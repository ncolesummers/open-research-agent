package workflow

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ResearcherPoolConfig holds configuration for the researcher pool
type ResearcherPoolConfig struct {
	MaxWorkers    int           `json:"max_workers"`
	QueueSize     int           `json:"queue_size"`
	WorkerTimeout time.Duration `json:"worker_timeout"`
}

// TaskTracking tracks an active task's state
type TaskTracking struct {
	TaskID    string
	WorkerID  string
	StartTime time.Time
	Topic     string
}

// ResearcherPool manages a pool of ResearcherWorkers
type ResearcherPool struct {
	config  *ResearcherPoolConfig
	workers []*ResearcherWorker

	// Channels - separated for tracking
	taskQueue          chan *domain.ResearchTask
	workerResultChan   chan *domain.ResearchResult // Workers write here
	consumerResultChan chan *domain.ResearchResult // Consumers read here
	errorChan          chan error

	// Task tracking and metrics
	taskTracker *PoolTaskTracker
	metrics     *PoolMetrics

	// Dependencies
	llmClient domain.LLMClient
	tools     domain.ToolRegistry
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger

	// Synchronization
	wg      sync.WaitGroup
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	running atomic.Bool
}

// NewResearcherPool creates a new researcher pool
func NewResearcherPool(
	cfg *ResearcherPoolConfig,
	llmClient domain.LLMClient,
	tools domain.ToolRegistry,
	telemetry *observability.Telemetry,
) (*ResearcherPool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 5 // Default
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 50 // Default
	}
	if cfg.WorkerTimeout <= 0 {
		cfg.WorkerTimeout = 2 * time.Minute
	}
	if llmClient == nil {
		return nil, fmt.Errorf("llm client is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create metrics and task tracker
	metrics := NewPoolMetrics()
	logger := observability.NewStructuredLogger("researcher_pool")
	taskTracker := NewPoolTaskTracker(metrics, telemetry, logger)

	pool := &ResearcherPool{
		config:             cfg,
		workers:            make([]*ResearcherWorker, 0, cfg.MaxWorkers),
		taskQueue:          make(chan *domain.ResearchTask, cfg.QueueSize),
		workerResultChan:   make(chan *domain.ResearchResult, cfg.QueueSize),
		consumerResultChan: make(chan *domain.ResearchResult, cfg.QueueSize),
		errorChan:          make(chan error, cfg.QueueSize),
		taskTracker:        taskTracker,
		metrics:            metrics,
		llmClient:          llmClient,
		tools:              tools,
		telemetry:          telemetry,
		logger:             logger,
		ctx:                ctx,
		cancel:             cancel,
	}

	return pool, nil
}

// Start initializes and starts all researcher workers
func (p *ResearcherPool) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running.Load() {
		return fmt.Errorf("researcher pool already running")
	}

	// Start telemetry span
	if p.telemetry != nil {
		_, span := p.telemetry.StartSpan(ctx, "researcher_pool.start",
			trace.WithAttributes(
				attribute.Int("max_workers", p.config.MaxWorkers),
				attribute.Int("queue_size", p.config.QueueSize),
			),
		)
		defer span.End()
	}

	// Create and start researcher workers
	for i := 0; i < p.config.MaxWorkers; i++ {
		worker := NewResearcherWorker(
			fmt.Sprintf("researcher-%d", i),
			p.llmClient,
			p.tools,
			p.taskQueue,
			p.workerResultChan, // Workers write to internal channel
			p.errorChan,
			p.telemetry,
			&p.wg,
		)

		// Set task tracking callbacks
		worker.SetTaskCallbacks(p.taskTracker.RegisterTaskStart, p.taskTracker.TrackTaskError)

		p.workers = append(p.workers, worker)
		worker.Start(ctx)
	}

	p.running.Store(true)

	p.logger.Info(ctx, "Researcher pool started",
		map[string]interface{}{
			"workers":    p.config.MaxWorkers,
			"queue_size": p.config.QueueSize,
		},
	)

	// Start result forwarder with tracking
	p.wg.Add(1)
	go p.forwardResults(ctx)

	// Start metrics collector
	go p.collectMetrics(ctx)

	return nil
}

// Stop gracefully shuts down the researcher pool
func (p *ResearcherPool) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running.Load() {
		return fmt.Errorf("researcher pool not running")
	}

	p.logger.Info(ctx, "Stopping researcher pool")

	// Stop accepting new tasks
	p.running.Store(false)

	// Close task queue to signal workers to stop
	close(p.taskQueue)

	// Stop all workers
	for _, worker := range p.workers {
		worker.Stop()
	}

	// Cancel context to stop forwardResults goroutine
	p.cancel()

	// Close worker result channel to unblock forwardResults
	close(p.workerResultChan)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info(ctx, "All researchers stopped gracefully")
	case <-time.After(30 * time.Second):
		p.logger.Warn(ctx, "Timeout waiting for researchers to stop")
	}

	// Close remaining channels
	close(p.consumerResultChan)
	close(p.errorChan)

	return nil
}

// forwardResults forwards results from workers to consumers while tracking metrics
func (p *ResearcherPool) forwardResults(ctx context.Context) {
	defer p.wg.Done()

	for {
		select {
		case result, ok := <-p.workerResultChan:
			if !ok {
				// Channel closed, stop forwarding
				return
			}

			// Track completion metrics
			p.taskTracker.TrackTaskCompletion(result)

			// Forward to consumers
			select {
			case p.consumerResultChan <- result:
				// Successfully forwarded
			case <-ctx.Done():
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// SubmitTask submits a research task to the pool
func (p *ResearcherPool) SubmitTask(ctx context.Context, task *domain.ResearchTask) error {
	if !p.running.Load() {
		return fmt.Errorf("researcher pool not running")
	}

	// Update metrics
	p.metrics.IncrementTasksQueued()

	// Submit with timeout
	select {
	case p.taskQueue <- task:
		p.logger.Debug(ctx, "Task submitted",
			map[string]interface{}{
				"task_id":    task.ID,
				"task_topic": task.Topic,
				"queue_size": len(p.taskQueue),
			},
		)
		return nil
	case <-time.After(5 * time.Second):
		p.metrics.DecrementTasksQueued()
		return fmt.Errorf("timeout submitting task - queue full")
	case <-ctx.Done():
		p.metrics.DecrementTasksQueued()
		return ctx.Err()
	}
}

// GetResults returns the result channel for consumers
func (p *ResearcherPool) GetResults() <-chan *domain.ResearchResult {
	return p.consumerResultChan
}

// GetErrors returns the error channel
func (p *ResearcherPool) GetErrors() <-chan error {
	return p.errorChan
}

// collectMetrics collects metrics from completed tasks
func (p *ResearcherPool) collectMetrics(ctx context.Context) {
	// Note: This is a simplified metrics collector
	// In production, you might want to track more detailed metrics
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Log basic metrics
			metricSnapshot := p.metrics.GetSnapshot()
			p.logger.Debug(ctx, "Pool metrics", metricSnapshot)
		case <-ctx.Done():
			return
		}
	}
}

// GetStats returns pool statistics
func (p *ResearcherPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Get metrics snapshot
	metricsSnapshot := p.metrics.GetSnapshot()

	// Get active tasks
	activeTasks := p.taskTracker.GetActiveTasks()

	stats := map[string]interface{}{
		"running":           p.running.Load(),
		"workers":           len(p.workers),
		"tasks_queued":      metricsSnapshot["tasks_queued"],
		"tasks_processing":  metricsSnapshot["tasks_processing"],
		"tasks_completed":   metricsSnapshot["tasks_completed"],
		"tasks_failed":      metricsSnapshot["tasks_failed"],
		"queue_depth":       len(p.taskQueue),
		"avg_task_duration": metricsSnapshot["avg_task_duration"],
		"active_tasks":      activeTasks,
		"last_completion":   metricsSnapshot["last_completion"],
	}

	// Add worker stats
	var workerStats []map[string]interface{}
	for _, worker := range p.workers {
		metrics := worker.GetMetrics()
		workerStats = append(workerStats, map[string]interface{}{
			"id":              worker.GetID(),
			"healthy":         worker.IsHealthy(),
			"tasks_processed": metrics.TasksProcessed,
			"success_rate":    metrics.SuccessRate,
			"avg_confidence":  metrics.AvgConfidence,
		})
	}
	stats["workers"] = workerStats

	return stats
}

// GetHealthStatus returns health status of all workers
func (p *ResearcherPool) GetHealthStatus() []map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var health []map[string]interface{}
	for _, worker := range p.workers {
		health = append(health, worker.GetHealthStatus())
	}
	return health
}
