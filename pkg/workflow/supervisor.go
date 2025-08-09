package workflow

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"github.com/ncolesummers/open-research-agent/pkg/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TaskDistributor defines the interface for task distribution strategies
type TaskDistributor interface {
	Distribute(ctx context.Context, tasks []*domain.ResearchTask, workers []*Worker) ([]*TaskAssignment, error)
	GetStrategy() string
}

// TaskPrioritizer defines the interface for task prioritization
type TaskPrioritizer interface {
	Prioritize(tasks []*domain.ResearchTask) []*domain.ResearchTask
	CalculatePriority(task *domain.ResearchTask) float64
}

// ProgressMonitor tracks the progress of distributed tasks
type ProgressMonitor interface {
	TrackTask(taskID string, workerID string)
	UpdateTaskStatus(taskID string, status domain.TaskStatus)
	GetProgress() ProgressReport
	GetTaskLocation(taskID string) (workerID string, found bool)
}

// TaskAssignment represents a task assignment to a worker
type TaskAssignment struct {
	Task     *domain.ResearchTask
	WorkerID string
	Priority float64
	Deadline time.Time
}

// ProgressReport contains progress information
type ProgressReport struct {
	TotalTasks      int
	CompletedTasks  int
	InProgressTasks int
	PendingTasks    int
	FailedTasks     int
	AverageTime     time.Duration
	EstimatedTime   time.Duration
	WorkerLoads     map[string]int
}

// Supervisor manages task distribution and coordination
type Supervisor struct {
	pool        *WorkerPool
	distributor TaskDistributor
	prioritizer TaskPrioritizer
	monitor     ProgressMonitor
	telemetry   *observability.Telemetry
	logger      *observability.StructuredLogger

	// Synchronization
	mu sync.RWMutex

	// State
	taskQueue     []*domain.ResearchTask
	activeWorkers map[string]*Worker
	taskHistory   map[string]*TaskExecutionHistory

	// Configuration
	maxRetries       int
	taskTimeout      time.Duration
	rebalanceEnabled bool
	adaptiveStrategy bool
}

// TaskExecutionHistory tracks task execution history
type TaskExecutionHistory struct {
	TaskID     string
	Attempts   int
	LastWorker string
	StartTime  time.Time
	EndTime    *time.Time
	Status     domain.TaskStatus
	ErrorCount int
	LastError  error
}

// NewSupervisor creates a new supervisor instance
func NewSupervisor(
	pool *WorkerPool,
	telemetry *observability.Telemetry,
) *Supervisor {
	return &Supervisor{
		pool:             pool,
		distributor:      NewLoadBalancedDistributor(),
		prioritizer:      NewComplexityBasedPrioritizer(),
		monitor:          NewDefaultProgressMonitor(),
		telemetry:        telemetry,
		logger:           observability.NewStructuredLogger("supervisor"),
		activeWorkers:    make(map[string]*Worker),
		taskHistory:      make(map[string]*TaskExecutionHistory),
		maxRetries:       3,
		taskTimeout:      2 * time.Minute,
		rebalanceEnabled: true,
		adaptiveStrategy: true,
	}
}

// DistributeTasks distributes tasks to workers intelligently
func (s *Supervisor) DistributeTasks(ctx context.Context, tasks []*domain.ResearchTask, state *state.GraphState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Start span for task distribution
	if s.telemetry != nil {
		_, span := s.telemetry.StartSpan(ctx, "supervisor.distribute",
			trace.WithAttributes(
				attribute.Int("task_count", len(tasks)),
				attribute.String("strategy", s.distributor.GetStrategy()),
			),
		)
		defer span.End()
	}

	// Store tasks in queue for later reference
	s.taskQueue = append(s.taskQueue, tasks...)
	
	// Prioritize tasks
	prioritizedTasks := s.prioritizer.Prioritize(tasks)

	// Get available workers from pool
	workers := s.pool.workers

	// Distribute tasks using the selected strategy
	assignments, err := s.distributor.Distribute(ctx, prioritizedTasks, workers)
	if err != nil {
		return fmt.Errorf("task distribution failed: %w", err)
	}

	// Submit assignments to worker pool
	for _, assignment := range assignments {
		// Track task assignment
		s.monitor.TrackTask(assignment.Task.ID, assignment.WorkerID)

		// Record task history
		s.taskHistory[assignment.Task.ID] = &TaskExecutionHistory{
			TaskID:     assignment.Task.ID,
			Attempts:   0,
			LastWorker: assignment.WorkerID,
			StartTime:  time.Now(),
			Status:     domain.TaskStatusPending,
		}

		// Submit to worker pool
		if err := s.pool.Submit(ctx, assignment.Task, state); err != nil {
			s.logger.Warn(ctx, "Failed to submit task",
				map[string]interface{}{
					"task_id":   assignment.Task.ID,
					"worker_id": assignment.WorkerID,
					"error":     err.Error(),
				},
			)
			// Continue with other tasks
		}
	}

	// Log distribution summary
	s.logger.Info(ctx, "Tasks distributed",
		map[string]interface{}{
			"total_tasks":  len(tasks),
			"assignments":  len(assignments),
			"strategy":     s.distributor.GetStrategy(),
			"worker_count": len(workers),
		},
	)

	return nil
}

// MonitorProgress monitors and reports task execution progress
func (s *Supervisor) MonitorProgress(ctx context.Context) ProgressReport {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.monitor.GetProgress()
}

// HandleTaskCompletion handles task completion events
func (s *Supervisor) HandleTaskCompletion(taskID string, result *domain.ResearchResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	history, exists := s.taskHistory[taskID]
	if !exists {
		return
	}

	now := time.Now()
	history.EndTime = &now

	if err != nil {
		history.ErrorCount++
		history.LastError = err
		history.Status = domain.TaskStatusFailed
		s.monitor.UpdateTaskStatus(taskID, domain.TaskStatusFailed)

		// Retry logic
		if history.Attempts < s.maxRetries {
			history.Attempts++
			// Re-queue for retry
			// This would typically trigger a re-submission to the pool
		}
	} else {
		history.Status = domain.TaskStatusCompleted
		s.monitor.UpdateTaskStatus(taskID, domain.TaskStatusCompleted)
	}
}

// RebalanceTasks rebalances tasks across workers if needed
func (s *Supervisor) RebalanceTasks(ctx context.Context) error {
	if !s.rebalanceEnabled {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	progress := s.monitor.GetProgress()

	// Check for worker load imbalance
	var maxLoad, minLoad int
	for _, load := range progress.WorkerLoads {
		if load > maxLoad {
			maxLoad = load
		}
		if minLoad == 0 || load < minLoad {
			minLoad = load
		}
	}

	// Rebalance if significant imbalance detected
	imbalanceThreshold := 3
	if maxLoad-minLoad > imbalanceThreshold {
		s.logger.Info(ctx, "Rebalancing tasks",
			map[string]interface{}{
				"max_load": maxLoad,
				"min_load": minLoad,
			},
		)
		
		// Find overloaded and underloaded workers
		overloadedWorkers := make([]string, 0)
		underloadedWorkers := make([]string, 0)
		avgLoad := progress.TotalTasks / len(progress.WorkerLoads)
		
		for workerID, load := range progress.WorkerLoads {
			if load > avgLoad+1 {
				overloadedWorkers = append(overloadedWorkers, workerID)
			} else if load < avgLoad {
				underloadedWorkers = append(underloadedWorkers, workerID)
			}
		}
		
		// Rebalance by redistributing pending tasks
		// Note: We can't move in-progress tasks, only redistribute pending ones
		if len(overloadedWorkers) > 0 && len(underloadedWorkers) > 0 {
			// Get pending tasks from overloaded workers
			tasksToRedistribute := make([]*domain.ResearchTask, 0)
			
			for taskID, taskInfo := range s.taskHistory {
				if taskInfo.Status == domain.TaskStatusPending {
					// Check if task is assigned to overloaded worker
					for _, overloadedWorker := range overloadedWorkers {
						if taskInfo.LastWorker == overloadedWorker {
							// Find the actual task
							if task := s.findTaskByID(taskID); task != nil {
								tasksToRedistribute = append(tasksToRedistribute, task)
							}
							break
						}
					}
				}
			}
			
			// Redistribute tasks to underloaded workers
			if len(tasksToRedistribute) > 0 {
				s.logger.Info(ctx, "Redistributing tasks",
					map[string]interface{}{
						"task_count":          len(tasksToRedistribute),
						"underloaded_workers": len(underloadedWorkers),
					},
				)
				
				// Update task assignments
				for i, task := range tasksToRedistribute {
					newWorker := underloadedWorkers[i%len(underloadedWorkers)]
					if history, exists := s.taskHistory[task.ID]; exists {
						history.LastWorker = newWorker
						s.monitor.UpdateTaskStatus(task.ID, domain.TaskStatusPending)
						s.monitor.TrackTask(task.ID, newWorker)
					}
				}
			}
		}
	}

	return nil
}

// AdaptStrategy adapts the distribution strategy based on performance metrics
func (s *Supervisor) AdaptStrategy(ctx context.Context) {
	if !s.adaptiveStrategy {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Analyze performance metrics
	progress := s.monitor.GetProgress()

	// Calculate efficiency metrics
	completionRate := float64(progress.CompletedTasks) / float64(progress.TotalTasks)
	failureRate := float64(progress.FailedTasks) / float64(progress.TotalTasks)

	// Switch strategy based on performance
	if failureRate > 0.2 {
		// High failure rate - switch to more conservative strategy
		s.distributor = NewRoundRobinDistributor()
		s.logger.Info(ctx, "Switched to round-robin distribution due to high failure rate",
			map[string]interface{}{"failure_rate": failureRate},
		)
	} else if completionRate < 0.5 && progress.InProgressTasks > len(s.pool.workers)*2 {
		// Low completion with high queue - switch to cost-based distribution
		s.distributor = NewCostBasedDistributor()
		s.logger.Info(ctx, "Switched to cost-based distribution for better throughput",
			map[string]interface{}{"completion_rate": completionRate},
		)
	}
}

// GetTaskHistory returns the execution history for a task
func (s *Supervisor) GetTaskHistory(taskID string) (*TaskExecutionHistory, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history, exists := s.taskHistory[taskID]
	return history, exists
}

// findTaskByID finds a task in the task queue by ID
func (s *Supervisor) findTaskByID(taskID string) *domain.ResearchTask {
	// This is a helper method - in production, tasks would be stored
	// in a more efficient data structure for lookup
	for _, task := range s.taskQueue {
		if task.ID == taskID {
			return task
		}
	}
	return nil
}

// LoadBalancedDistributor implements load-balanced task distribution
type LoadBalancedDistributor struct {
	workerLoads map[string]*atomic.Int32
}

func NewLoadBalancedDistributor() *LoadBalancedDistributor {
	return &LoadBalancedDistributor{
		workerLoads: make(map[string]*atomic.Int32),
	}
}

func (d *LoadBalancedDistributor) Distribute(ctx context.Context, tasks []*domain.ResearchTask, workers []*Worker) ([]*TaskAssignment, error) {
	if len(workers) == 0 {
		return nil, fmt.Errorf("no workers available")
	}

	assignments := make([]*TaskAssignment, 0, len(tasks))

	// Initialize worker loads if needed
	for _, worker := range workers {
		if _, exists := d.workerLoads[worker.id]; !exists {
			d.workerLoads[worker.id] = &atomic.Int32{}
		}
	}

	// Distribute tasks to least loaded workers
	for _, task := range tasks {
		// Find worker with minimum load
		var selectedWorker *Worker
		minLoad := int32(^uint32(0) >> 1) // Max int32

		for _, worker := range workers {
			load := d.workerLoads[worker.id].Load()
			if load < minLoad {
				minLoad = load
				selectedWorker = worker
			}
		}

		if selectedWorker != nil {
			assignments = append(assignments, &TaskAssignment{
				Task:     task,
				WorkerID: selectedWorker.id,
				Priority: 1.0,
				Deadline: time.Now().Add(2 * time.Minute),
			})
			d.workerLoads[selectedWorker.id].Add(1)
		}
	}

	return assignments, nil
}

func (d *LoadBalancedDistributor) GetStrategy() string {
	return "load-balanced"
}

// RoundRobinDistributor implements round-robin task distribution
type RoundRobinDistributor struct {
	nextWorkerIndex atomic.Int32
}

func NewRoundRobinDistributor() *RoundRobinDistributor {
	return &RoundRobinDistributor{}
}

func (d *RoundRobinDistributor) Distribute(ctx context.Context, tasks []*domain.ResearchTask, workers []*Worker) ([]*TaskAssignment, error) {
	if len(workers) == 0 {
		return nil, fmt.Errorf("no workers available")
	}

	assignments := make([]*TaskAssignment, 0, len(tasks))

	for _, task := range tasks {
		workerIndex := int(d.nextWorkerIndex.Add(1)) % len(workers)
		assignments = append(assignments, &TaskAssignment{
			Task:     task,
			WorkerID: workers[workerIndex].id,
			Priority: 1.0,
			Deadline: time.Now().Add(2 * time.Minute),
		})
	}

	return assignments, nil
}

func (d *RoundRobinDistributor) GetStrategy() string {
	return "round-robin"
}

// CostBasedDistributor implements cost-based task distribution
type CostBasedDistributor struct {
	taskCosts   map[string]float64
	workerCosts map[string]float64
	mu          sync.RWMutex
}

func NewCostBasedDistributor() *CostBasedDistributor {
	return &CostBasedDistributor{
		taskCosts:   make(map[string]float64),
		workerCosts: make(map[string]float64),
	}
}

func (d *CostBasedDistributor) Distribute(ctx context.Context, tasks []*domain.ResearchTask, workers []*Worker) ([]*TaskAssignment, error) {
	if len(workers) == 0 {
		return nil, fmt.Errorf("no workers available")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	assignments := make([]*TaskAssignment, 0, len(tasks))

	// Calculate cost for each task-worker pair
	type costPair struct {
		task   *domain.ResearchTask
		worker *Worker
		cost   float64
	}

	pairs := make([]costPair, 0, len(tasks)*len(workers))

	for _, task := range tasks {
		taskCost := d.estimateTaskCost(task)
		for _, worker := range workers {
			workerCost := d.getWorkerCost(worker.id)
			totalCost := taskCost + workerCost
			pairs = append(pairs, costPair{
				task:   task,
				worker: worker,
				cost:   totalCost,
			})
		}
	}

	// Sort by cost (ascending)
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].cost < pairs[j].cost
	})

	// Assign tasks to minimize total cost
	assignedTasks := make(map[string]bool)
	assignedWorkers := make(map[string]int)

	for _, pair := range pairs {
		if assignedTasks[pair.task.ID] {
			continue
		}

		// Check worker capacity
		if assignedWorkers[pair.worker.id] >= 3 { // Max 3 tasks per worker
			continue
		}

		assignments = append(assignments, &TaskAssignment{
			Task:     pair.task,
			WorkerID: pair.worker.id,
			Priority: 1.0 / pair.cost, // Higher priority for lower cost
			Deadline: time.Now().Add(2 * time.Minute),
		})

		assignedTasks[pair.task.ID] = true
		assignedWorkers[pair.worker.id]++
		d.workerCosts[pair.worker.id] = pair.cost
	}

	return assignments, nil
}

func (d *CostBasedDistributor) estimateTaskCost(task *domain.ResearchTask) float64 {
	// Estimate based on task complexity
	baseCost := 1.0
	// Higher priority tasks have higher cost (priority is int, lower values = higher priority)
	if task.Priority <= 2 {
		baseCost *= 3.0
	} else if task.Priority <= 5 {
		baseCost *= 2.0
	}
	// Use parent ID as a proxy for depth
	if task.ParentID != "" {
		baseCost *= 1.5
	}
	return baseCost
}

func (d *CostBasedDistributor) getWorkerCost(workerID string) float64 {
	if cost, exists := d.workerCosts[workerID]; exists {
		return cost
	}
	return 1.0
}

func (d *CostBasedDistributor) GetStrategy() string {
	return "cost-based"
}

// ComplexityBasedPrioritizer prioritizes tasks based on complexity
type ComplexityBasedPrioritizer struct{}

func NewComplexityBasedPrioritizer() *ComplexityBasedPrioritizer {
	return &ComplexityBasedPrioritizer{}
}

func (p *ComplexityBasedPrioritizer) Prioritize(tasks []*domain.ResearchTask) []*domain.ResearchTask {
	prioritized := make([]*domain.ResearchTask, len(tasks))
	copy(prioritized, tasks)

	sort.Slice(prioritized, func(i, j int) bool {
		return p.CalculatePriority(prioritized[i]) > p.CalculatePriority(prioritized[j])
	})

	return prioritized
}

func (p *ComplexityBasedPrioritizer) CalculatePriority(task *domain.ResearchTask) float64 {
	priority := 1.0

	// Factor in task priority (lower int value = higher priority)
	if task.Priority <= 1 {
		priority *= 4.0 // Critical
	} else if task.Priority <= 3 {
		priority *= 2.0 // High
	} else if task.Priority <= 5 {
		priority *= 1.5 // Medium
	}

	// Factor in depth (use parent ID as proxy - tasks without parents have higher priority)
	if task.ParentID != "" {
		priority *= 0.8 // Reduce priority for child tasks
	}

	// Factor in creation time (older tasks get slightly higher priority)
	age := time.Since(task.CreatedAt)
	if age > 5*time.Minute {
		priority *= 1.2
	}

	return priority
}

// DefaultProgressMonitor provides basic progress monitoring
type DefaultProgressMonitor struct {
	tasks       map[string]*TaskProgress
	workerLoads map[string]int
	mu          sync.RWMutex
}

type TaskProgress struct {
	TaskID    string
	WorkerID  string
	Status    domain.TaskStatus
	StartTime time.Time
	EndTime   *time.Time
}

func NewDefaultProgressMonitor() *DefaultProgressMonitor {
	return &DefaultProgressMonitor{
		tasks:       make(map[string]*TaskProgress),
		workerLoads: make(map[string]int),
	}
}

func (m *DefaultProgressMonitor) TrackTask(taskID string, workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tasks[taskID] = &TaskProgress{
		TaskID:    taskID,
		WorkerID:  workerID,
		Status:    domain.TaskStatusPending,
		StartTime: time.Now(),
	}
	m.workerLoads[workerID]++
}

func (m *DefaultProgressMonitor) UpdateTaskStatus(taskID string, status domain.TaskStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if progress, exists := m.tasks[taskID]; exists {
		progress.Status = status
		if status == domain.TaskStatusCompleted || status == domain.TaskStatusFailed {
			now := time.Now()
			progress.EndTime = &now
			if m.workerLoads[progress.WorkerID] > 0 {
				m.workerLoads[progress.WorkerID]--
			}
		}
	}
}

func (m *DefaultProgressMonitor) GetProgress() ProgressReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report := ProgressReport{
		WorkerLoads: make(map[string]int),
	}

	var totalTime time.Duration
	completedCount := 0

	for _, progress := range m.tasks {
		report.TotalTasks++

		switch progress.Status {
		case domain.TaskStatusCompleted:
			report.CompletedTasks++
			if progress.EndTime != nil {
				totalTime += progress.EndTime.Sub(progress.StartTime)
				completedCount++
			}
		case domain.TaskStatusInProgress:
			report.InProgressTasks++
		case domain.TaskStatusPending:
			report.PendingTasks++
		case domain.TaskStatusFailed:
			report.FailedTasks++
		}
	}

	// Calculate average time
	if completedCount > 0 {
		report.AverageTime = totalTime / time.Duration(completedCount)
		// Estimate remaining time
		remainingTasks := report.PendingTasks + report.InProgressTasks
		report.EstimatedTime = report.AverageTime * time.Duration(remainingTasks)
	}

	// Copy worker loads
	for workerID, load := range m.workerLoads {
		report.WorkerLoads[workerID] = load
	}

	return report
}

func (m *DefaultProgressMonitor) GetTaskLocation(taskID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if progress, exists := m.tasks[taskID]; exists {
		return progress.WorkerID, true
	}
	return "", false
}
