package workflow

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/internal/testutil"
	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"github.com/ncolesummers/open-research-agent/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisor_DistributeTasks(t *testing.T) {
	observability.SetLogOutput(io.Discard)
	tests := []struct {
		name          string
		tasks         []*domain.ResearchTask
		workerCount   int
		strategy      string
		expectedError bool
	}{
		{
			name: "distribute_single_task",
			tasks: []*domain.ResearchTask{
				{
					ID:       "task-1",
					Topic:    "Test Task",
					Priority: 1,
					Status:   domain.TaskStatusPending,
				},
			},
			workerCount:   3,
			strategy:      "load-balanced",
			expectedError: false,
		},
		{
			name: "distribute_multiple_tasks",
			tasks: []*domain.ResearchTask{
				{ID: "task-1", Topic: "Task 1", Priority: 1},
				{ID: "task-2", Topic: "Task 2", Priority: 2},
				{ID: "task-3", Topic: "Task 3", Priority: 3},
			},
			workerCount:   2,
			strategy:      "load-balanced",
			expectedError: false,
		},
		{
			name:          "no_workers_available",
			tasks:         []*domain.ResearchTask{{ID: "task-1"}},
			workerCount:   0,
			strategy:      "load-balanced",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()
			mockLLM := testutil.NewMockLLMClient()
			mockTools := testutil.NewMockToolRegistry()
			telemetry, _ := observability.NewTelemetry(&observability.TelemetryConfig{
				EnableTracing: false,
				EnableMetrics: false,
				EnableLogging: false,
			})

			// Create worker pool
			poolConfig := &WorkerPoolConfig{
				MaxWorkers:    tt.workerCount,
				QueueSize:     50,
				WorkerTimeout: 2 * time.Minute,
			}

			var pool *WorkerPool
			var err error
			if tt.workerCount > 0 {
				pool, err = NewWorkerPool(poolConfig, mockLLM, mockTools, telemetry)
				require.NoError(t, err)
				err = pool.Start(ctx)
				require.NoError(t, err)
				defer func() { assert.NoError(t, pool.Stop(ctx)) }()
			} else {
				// Create pool with no workers for error case
				pool = &WorkerPool{
					config:  poolConfig,
					workers: []*Worker{},
				}
			}

			// Create supervisor
			supervisor := NewSupervisor(pool, telemetry)

			// Create state
			request := domain.ResearchRequest{Query: "Test query"}
			stateConfig := &state.StateConfig{
				MaxIterations: 3,
				MaxTasks:      50,
			}
			state := state.NewGraphState(request, stateConfig)

			// Execute
			err = supervisor.DistributeTasks(ctx, tt.tasks, state)

			// Assert
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify tasks were tracked
				for _, task := range tt.tasks {
					_, found := supervisor.monitor.GetTaskLocation(task.ID)
					assert.True(t, found, "Task %s should be tracked", task.ID)
				}
			}
		})
	}
}

func TestLoadBalancedDistributor(t *testing.T) {
	ctx := context.Background()
	distributor := NewLoadBalancedDistributor()

	// Create workers
	workers := []*Worker{
		{id: "worker-1"},
		{id: "worker-2"},
		{id: "worker-3"},
	}

	// Create tasks
	tasks := []*domain.ResearchTask{
		{ID: "task-1", Priority: 1},
		{ID: "task-2", Priority: 2},
		{ID: "task-3", Priority: 1},
		{ID: "task-4", Priority: 3},
		{ID: "task-5", Priority: 2},
	}

	// Distribute tasks
	assignments, err := distributor.Distribute(ctx, tasks, workers)
	require.NoError(t, err)
	assert.Len(t, assignments, len(tasks))

	// Verify load balancing
	workerLoads := make(map[string]int)
	for _, assignment := range assignments {
		workerLoads[assignment.WorkerID]++
	}

	// Check that load is relatively balanced
	maxLoad := 0
	minLoad := len(tasks)
	for _, load := range workerLoads {
		if load > maxLoad {
			maxLoad = load
		}
		if load < minLoad {
			minLoad = load
		}
	}

	// Load difference should be at most 1
	assert.LessOrEqual(t, maxLoad-minLoad, 1, "Load should be balanced")
}

func TestRoundRobinDistributor(t *testing.T) {
	ctx := context.Background()
	distributor := NewRoundRobinDistributor()

	workers := []*Worker{
		{id: "worker-1"},
		{id: "worker-2"},
	}

	tasks := []*domain.ResearchTask{
		{ID: "task-1"},
		{ID: "task-2"},
		{ID: "task-3"},
		{ID: "task-4"},
	}

	assignments, err := distributor.Distribute(ctx, tasks, workers)
	require.NoError(t, err)
	assert.Len(t, assignments, len(tasks))

	// Verify round-robin distribution pattern
	// The pattern should alternate between workers
	workerCounts := make(map[string]int)
	for _, assignment := range assignments {
		workerCounts[assignment.WorkerID]++
	}

	// Each worker should get exactly 2 tasks
	assert.Equal(t, 2, workerCounts["worker-1"], "worker-1 should get 2 tasks")
	assert.Equal(t, 2, workerCounts["worker-2"], "worker-2 should get 2 tasks")
}

func TestCostBasedDistributor(t *testing.T) {
	ctx := context.Background()
	distributor := NewCostBasedDistributor()

	workers := []*Worker{
		{id: "worker-1"},
		{id: "worker-2"},
	}

	tasks := []*domain.ResearchTask{
		{ID: "task-1", Priority: 1},                     // High priority
		{ID: "task-2", Priority: 10},                    // Low priority
		{ID: "task-3", Priority: 2, ParentID: "task-1"}, // Child task
	}

	assignments, err := distributor.Distribute(ctx, tasks, workers)
	require.NoError(t, err)
	assert.Len(t, assignments, len(tasks))

	// Verify cost-based distribution
	// High priority task should have higher priority score
	var highPriorityAssignment *TaskAssignment
	for _, assignment := range assignments {
		if assignment.Task.ID == "task-1" {
			highPriorityAssignment = assignment
			break
		}
	}
	require.NotNil(t, highPriorityAssignment)
}

func TestComplexityBasedPrioritizer(t *testing.T) {
	prioritizer := NewComplexityBasedPrioritizer()

	now := time.Now()
	oldTime := now.Add(-10 * time.Minute)

	tasks := []*domain.ResearchTask{
		{ID: "task-1", Priority: 1, CreatedAt: now},                     // Critical, new
		{ID: "task-2", Priority: 5, CreatedAt: oldTime},                 // Medium, old
		{ID: "task-3", Priority: 10, CreatedAt: now},                    // Low, new
		{ID: "task-4", Priority: 1, ParentID: "task-1", CreatedAt: now}, // Critical child
	}

	prioritized := prioritizer.Prioritize(tasks)

	// Verify prioritization order
	assert.Equal(t, "task-1", prioritized[0].ID, "Critical task should be first")

	// Verify priority calculation
	criticalPriority := prioritizer.CalculatePriority(tasks[0])
	lowPriority := prioritizer.CalculatePriority(tasks[2])
	assert.Greater(t, criticalPriority, lowPriority, "Critical task should have higher priority")

	// Verify age factor
	oldTaskPriority := prioritizer.CalculatePriority(tasks[1])
	assert.Greater(t, oldTaskPriority, 1.0, "Old task should have boosted priority")
}

func TestProgressMonitor(t *testing.T) {
	monitor := NewDefaultProgressMonitor()

	// Track tasks
	monitor.TrackTask("task-1", "worker-1")
	monitor.TrackTask("task-2", "worker-1")
	monitor.TrackTask("task-3", "worker-2")

	// Verify initial state
	progress := monitor.GetProgress()
	assert.Equal(t, 3, progress.TotalTasks)
	assert.Equal(t, 3, progress.PendingTasks)
	assert.Equal(t, 2, progress.WorkerLoads["worker-1"])
	assert.Equal(t, 1, progress.WorkerLoads["worker-2"])

	// Update task status
	monitor.UpdateTaskStatus("task-1", domain.TaskStatusInProgress)
	progress = monitor.GetProgress()
	assert.Equal(t, 1, progress.InProgressTasks)
	assert.Equal(t, 2, progress.PendingTasks)

	// Complete a task
	monitor.UpdateTaskStatus("task-1", domain.TaskStatusCompleted)
	progress = monitor.GetProgress()
	assert.Equal(t, 1, progress.CompletedTasks)
	assert.Equal(t, 0, progress.InProgressTasks)
	assert.Equal(t, 1, progress.WorkerLoads["worker-1"])

	// Fail a task
	monitor.UpdateTaskStatus("task-2", domain.TaskStatusFailed)
	progress = monitor.GetProgress()
	assert.Equal(t, 1, progress.FailedTasks)
	assert.Equal(t, 0, progress.WorkerLoads["worker-1"])

	// Get task location
	workerID, found := monitor.GetTaskLocation("task-3")
	assert.True(t, found)
	assert.Equal(t, "worker-2", workerID)
}

func TestSupervisor_HandleTaskCompletion(t *testing.T) {
	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()
	telemetry, _ := observability.NewTelemetry(&observability.TelemetryConfig{
		EnableTracing: false,
		EnableMetrics: false,
		EnableLogging: false,
	})

	poolConfig := &WorkerPoolConfig{
		MaxWorkers:    3,
		QueueSize:     50,
		WorkerTimeout: 2 * time.Minute,
	}

	pool, err := NewWorkerPool(poolConfig, mockLLM, mockTools, telemetry)
	require.NoError(t, err)

	supervisor := NewSupervisor(pool, telemetry)

	// Add task to history
	taskID := "test-task"
	supervisor.taskHistory[taskID] = &TaskExecutionHistory{
		TaskID:     taskID,
		Attempts:   0,
		LastWorker: "worker-1",
		StartTime:  time.Now(),
		Status:     domain.TaskStatusInProgress,
	}

	// Track in monitor
	supervisor.monitor.TrackTask(taskID, "worker-1")
	supervisor.monitor.UpdateTaskStatus(taskID, domain.TaskStatusInProgress)

	// Test successful completion
	result := &domain.ResearchResult{
		TaskID:  taskID,
		Content: "Test result",
	}
	supervisor.HandleTaskCompletion(taskID, result, nil)

	history, exists := supervisor.GetTaskHistory(taskID)
	assert.True(t, exists)
	assert.Equal(t, domain.TaskStatusCompleted, history.Status)
	assert.NotNil(t, history.EndTime)
	assert.Equal(t, 0, history.ErrorCount)

	// Test failure with retry
	failTaskID := "fail-task"
	supervisor.taskHistory[failTaskID] = &TaskExecutionHistory{
		TaskID:     failTaskID,
		Attempts:   1,
		LastWorker: "worker-2",
		StartTime:  time.Now(),
		Status:     domain.TaskStatusInProgress,
	}

	testErr := assert.AnError
	supervisor.HandleTaskCompletion(failTaskID, nil, testErr)

	failHistory, exists := supervisor.GetTaskHistory(failTaskID)
	assert.True(t, exists)
	assert.Equal(t, domain.TaskStatusFailed, failHistory.Status)
	assert.Equal(t, 1, failHistory.ErrorCount)
	assert.Equal(t, testErr, failHistory.LastError)
}

func TestSupervisor_AdaptStrategy(t *testing.T) {
	ctx := context.Background()
	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()
	telemetry, _ := observability.NewTelemetry(&observability.TelemetryConfig{
		EnableTracing: false,
		EnableMetrics: false,
		EnableLogging: false,
	})

	poolConfig := &WorkerPoolConfig{
		MaxWorkers:    3,
		QueueSize:     50,
		WorkerTimeout: 2 * time.Minute,
	}

	pool, err := NewWorkerPool(poolConfig, mockLLM, mockTools, telemetry)
	require.NoError(t, err)

	supervisor := NewSupervisor(pool, telemetry)
	supervisor.adaptiveStrategy = true

	// Set up monitor with high failure rate
	monitor := NewDefaultProgressMonitor()
	for i := 0; i < 10; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		monitor.TrackTask(taskID, "worker-1")
		if i < 3 {
			monitor.UpdateTaskStatus(taskID, domain.TaskStatusFailed)
		} else if i < 5 {
			monitor.UpdateTaskStatus(taskID, domain.TaskStatusCompleted)
		}
	}
	supervisor.monitor = monitor

	// Initial strategy should be load-balanced
	assert.Equal(t, "load-balanced", supervisor.distributor.GetStrategy())

	// Adapt strategy based on high failure rate
	supervisor.AdaptStrategy(ctx)

	// Should switch to round-robin due to high failure rate
	assert.Equal(t, "round-robin", supervisor.distributor.GetStrategy())
}

func TestSupervisor_ConcurrentTaskDistribution(t *testing.T) {
	ctx := context.Background()
	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()
	telemetry, _ := observability.NewTelemetry(&observability.TelemetryConfig{
		EnableTracing: false,
		EnableMetrics: false,
		EnableLogging: false,
	})

	poolConfig := &WorkerPoolConfig{
		MaxWorkers:    5,
		QueueSize:     100,
		WorkerTimeout: 2 * time.Minute,
	}

	pool, err := NewWorkerPool(poolConfig, mockLLM, mockTools, telemetry)
	require.NoError(t, err)
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() { assert.NoError(t, pool.Stop(ctx)) }()

	supervisor := NewSupervisor(pool, telemetry)

	// Create state
	request := domain.ResearchRequest{Query: "Test query"}
	stateConfig := &state.StateConfig{
		MaxIterations: 3,
		MaxTasks:      50,
	}
	state := state.NewGraphState(request, stateConfig)

	// Create many tasks
	var tasks []*domain.ResearchTask
	for i := 0; i < 20; i++ {
		tasks = append(tasks, &domain.ResearchTask{
			ID:       fmt.Sprintf("task-%d", i),
			Topic:    fmt.Sprintf("Topic %d", i),
			Priority: (i % 5) + 1,
			Status:   domain.TaskStatusPending,
		})
	}

	// Distribute tasks concurrently
	var wg sync.WaitGroup
	errors := make([]error, 0)
	var mu sync.Mutex

	for i := 0; i < 4; i++ {
		wg.Add(1)
		batch := tasks[i*5 : (i+1)*5]
		go func(batch []*domain.ResearchTask) {
			defer wg.Done()
			if err := supervisor.DistributeTasks(ctx, batch, state); err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
			}
		}(batch)
	}

	wg.Wait()

	// Verify no errors
	assert.Empty(t, errors)

	// Verify all tasks were tracked
	progress := supervisor.MonitorProgress(ctx)
	assert.Equal(t, 20, progress.TotalTasks)
}
