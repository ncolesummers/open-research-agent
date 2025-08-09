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

// ResearcherPool manages a pool of ResearcherWorkers
type ResearcherPool struct {
	config  *ResearcherPoolConfig
	workers []*ResearcherWorker

	// Channels
	taskQueue  chan *domain.ResearchTask
	resultChan chan *domain.ResearchResult
	errorChan  chan error

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

	// Metrics
	tasksQueued     atomic.Int64
	tasksProcessing atomic.Int64
	tasksCompleted  atomic.Int64
	tasksFailed     atomic.Int64
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

	pool := &ResearcherPool{
		config:     cfg,
		workers:    make([]*ResearcherWorker, 0, cfg.MaxWorkers),
		taskQueue:  make(chan *domain.ResearchTask, cfg.QueueSize),
		resultChan: make(chan *domain.ResearchResult, cfg.QueueSize),
		errorChan:  make(chan error, cfg.QueueSize),
		llmClient:  llmClient,
		tools:      tools,
		telemetry:  telemetry,
		logger:     observability.NewStructuredLogger("researcher_pool"),
		ctx:        ctx,
		cancel:     cancel,
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
			p.resultChan,
			p.errorChan,
			p.telemetry,
			&p.wg,
		)
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

	// Cancel context
	p.cancel()

	// Close channels
	close(p.resultChan)
	close(p.errorChan)

	return nil
}

// SubmitTask submits a research task to the pool
func (p *ResearcherPool) SubmitTask(ctx context.Context, task *domain.ResearchTask) error {
	if !p.running.Load() {
		return fmt.Errorf("researcher pool not running")
	}

	// Update metrics
	p.tasksQueued.Add(1)

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
		p.tasksQueued.Add(-1)
		return fmt.Errorf("timeout submitting task - queue full")
	case <-ctx.Done():
		p.tasksQueued.Add(-1)
		return ctx.Err()
	}
}

// GetResults returns the result channel
func (p *ResearcherPool) GetResults() <-chan *domain.ResearchResult {
	return p.resultChan
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
			stats := p.GetStats()
			p.logger.Debug(ctx, "Pool metrics",
				map[string]interface{}{
					"tasks_queued":     stats["tasks_queued"],
					"tasks_completed":  stats["tasks_completed"],
					"tasks_failed":     stats["tasks_failed"],
					"queue_depth":      stats["queue_depth"],
				},
			)
		case <-ctx.Done():
			return
		}
	}
}

// GetStats returns pool statistics
func (p *ResearcherPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := map[string]interface{}{
		"running":          p.running.Load(),
		"workers":          len(p.workers),
		"tasks_queued":     p.tasksQueued.Load(),
		"tasks_processing": p.tasksProcessing.Load(),
		"tasks_completed":  p.tasksCompleted.Load(),
		"tasks_failed":     p.tasksFailed.Load(),
		"queue_depth":      len(p.taskQueue),
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
