package workflow

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"github.com/ncolesummers/open-research-agent/pkg/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// WorkerPoolConfig holds configuration for the worker pool
type WorkerPoolConfig struct {
	MaxWorkers    int           `json:"max_workers"`
	QueueSize     int           `json:"queue_size"`
	WorkerTimeout time.Duration `json:"worker_timeout"`
}

// WorkerPoolMetrics tracks worker pool performance
type WorkerPoolMetrics struct {
	tasksQueued      atomic.Int64
	tasksProcessing  atomic.Int64
	tasksCompleted   atomic.Int64
	tasksFailed      atomic.Int64
	activeWorkers    atomic.Int32
	queueDepth       atomic.Int32
	totalProcessTime atomic.Int64 // in nanoseconds
}

// WorkerPool manages concurrent research workers
type WorkerPool struct {
	config    *WorkerPoolConfig
	workers   []*Worker
	taskQueue chan *WorkerTask
	resultCh  chan *domain.ResearchResult
	errorCh   chan error

	// Synchronization
	wg     sync.WaitGroup
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	// State
	running atomic.Bool
	metrics *WorkerPoolMetrics

	// Dependencies
	llmClient domain.LLMClient
	tools     domain.ToolRegistry
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger

	// Components
	healthMonitor    *HealthMonitor
	metricsCollector *MetricsCollector
}

// WorkerTask represents a task to be processed by a worker
type WorkerTask struct {
	Task      *domain.ResearchTask
	State     *state.GraphState
	Context   context.Context
	StartTime time.Time
	Retries   int
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(
	cfg *WorkerPoolConfig,
	llmClient domain.LLMClient,
	tools domain.ToolRegistry,
	telemetry *observability.Telemetry,
) (*WorkerPool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 5 // Default to 5 workers
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 50 // Default queue size
	}
	if cfg.WorkerTimeout <= 0 {
		cfg.WorkerTimeout = 2 * time.Minute // Default timeout
	}
	if llmClient == nil {
		return nil, fmt.Errorf("llm client is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &WorkerPool{
		config:    cfg,
		workers:   make([]*Worker, 0, cfg.MaxWorkers),
		taskQueue: make(chan *WorkerTask, cfg.QueueSize),
		resultCh:  make(chan *domain.ResearchResult, cfg.QueueSize),
		errorCh:   make(chan error, cfg.QueueSize),
		ctx:       ctx,
		cancel:    cancel,
		metrics:   &WorkerPoolMetrics{},
		llmClient: llmClient,
		tools:     tools,
		telemetry: telemetry,
		logger:    observability.NewStructuredLogger("worker_pool"),
	}

	return pool, nil
}

// Start initializes and starts all workers
func (p *WorkerPool) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running.Load() {
		return fmt.Errorf("worker pool already running")
	}

	// Start span for pool startup
	if p.telemetry != nil {
		_, span := p.telemetry.StartSpan(ctx, "worker_pool.start",
			trace.WithAttributes(
				attribute.Int("max_workers", p.config.MaxWorkers),
				attribute.Int("queue_size", p.config.QueueSize),
			),
		)
		defer span.End()
	}

	// Create and start workers
	for i := 0; i < p.config.MaxWorkers; i++ {
		worker := NewWorker(
			fmt.Sprintf("worker-%d", i),
			p,
			p.llmClient,
			p.tools,
			p.telemetry,
		)
		p.workers = append(p.workers, worker)

		p.wg.Add(1)
		go p.runWorker(worker)
		p.metrics.activeWorkers.Add(1)
	}

	// Initialize and start health monitor
	p.healthMonitor = NewHealthMonitor(p, 10*time.Second)
	go p.healthMonitor.Start(ctx)

	// Initialize and start metrics collector
	if p.telemetry != nil {
		var err error
		p.metricsCollector, err = NewMetricsCollector(p, p.telemetry, 5*time.Second)
		if err != nil {
			p.logger.Warn(ctx, "Failed to create metrics collector",
				map[string]interface{}{"error": err.Error()})
		} else {
			go p.metricsCollector.Start(ctx)
		}
	}

	p.running.Store(true)

	if p.logger != nil {
		p.logger.Info(ctx, "Worker pool started",
			map[string]interface{}{
				"workers":    p.config.MaxWorkers,
				"queue_size": p.config.QueueSize,
			},
		)
	}

	return nil
}

// Stop gracefully shuts down the worker pool
func (p *WorkerPool) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running.Load() {
		return fmt.Errorf("worker pool not running")
	}

	// Start span for shutdown
	if p.telemetry != nil {
		_, span := p.telemetry.StartSpan(ctx, "worker_pool.stop")
		defer span.End()
	}

	p.running.Store(false)

	// Stop components
	if p.healthMonitor != nil {
		p.healthMonitor.Stop()
	}
	if p.metricsCollector != nil {
		p.metricsCollector.Stop()
	}

	// Signal cancellation
	p.cancel()

	// Close task queue to prevent new tasks
	close(p.taskQueue)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All workers finished gracefully
		if p.logger != nil {
			p.logger.Info(ctx, "Worker pool stopped gracefully")
		}
	case <-time.After(30 * time.Second):
		// Timeout waiting for workers
		if p.logger != nil {
			p.logger.Warn(ctx, "Worker pool stop timeout - forcing shutdown")
		}
	}

	// Close result and error channels
	close(p.resultCh)
	close(p.errorCh)

	return nil
}

// Submit adds a task to the worker pool queue
func (p *WorkerPool) Submit(ctx context.Context, task *domain.ResearchTask, state *state.GraphState) error {
	if !p.running.Load() {
		return fmt.Errorf("worker pool not running")
	}

	// Create worker task
	workerTask := &WorkerTask{
		Task:      task,
		State:     state,
		Context:   ctx,
		StartTime: time.Now(),
		Retries:   0,
	}

	// Update metrics
	p.metrics.tasksQueued.Add(1)
	p.metrics.queueDepth.Add(1)

	// Try to submit with timeout
	select {
	case p.taskQueue <- workerTask:
		if p.logger != nil {
			p.logger.Debug(ctx, "Task submitted to queue",
				map[string]interface{}{
					"task_id":     task.ID,
					"queue_depth": p.metrics.queueDepth.Load(),
				},
			)
		}
		return nil
	case <-time.After(5 * time.Second):
		p.metrics.tasksQueued.Add(-1)
		p.metrics.queueDepth.Add(-1)
		return fmt.Errorf("timeout submitting task to queue - queue may be full")
	case <-ctx.Done():
		p.metrics.tasksQueued.Add(-1)
		p.metrics.queueDepth.Add(-1)
		return ctx.Err()
	}
}

// GetResults returns the results channel
func (p *WorkerPool) GetResults() <-chan *domain.ResearchResult {
	return p.resultCh
}

// GetErrors returns the error channel
func (p *WorkerPool) GetErrors() <-chan error {
	return p.errorCh
}

// GetMetrics returns current worker pool metrics
func (p *WorkerPool) GetMetrics() WorkerPoolMetrics {
	return WorkerPoolMetrics{
		tasksQueued:      atomic.Int64{},
		tasksProcessing:  atomic.Int64{},
		tasksCompleted:   atomic.Int64{},
		tasksFailed:      atomic.Int64{},
		activeWorkers:    atomic.Int32{},
		queueDepth:       atomic.Int32{},
		totalProcessTime: atomic.Int64{},
	}
}

// runWorker executes the worker loop
func (p *WorkerPool) runWorker(worker *Worker) {
	defer p.wg.Done()
	defer p.metrics.activeWorkers.Add(-1)

	for {
		select {
		case task, ok := <-p.taskQueue:
			if !ok {
				// Queue closed, worker should exit
				if p.logger != nil {
					p.logger.Debug(p.ctx, "Worker exiting - queue closed",
						map[string]interface{}{"worker_id": worker.id},
					)
				}
				return
			}

			// Update metrics
			p.metrics.queueDepth.Add(-1)
			p.metrics.tasksProcessing.Add(1)

			// Process task with timeout
			taskCtx, cancel := context.WithTimeout(task.Context, p.config.WorkerTimeout)
			result, err := worker.ProcessTask(taskCtx, task)
			cancel()

			// Update metrics
			p.metrics.tasksProcessing.Add(-1)
			processingTime := time.Since(task.StartTime)
			p.metrics.totalProcessTime.Add(processingTime.Nanoseconds())

			if err != nil {
				p.metrics.tasksFailed.Add(1)
				// Handle error with retry logic
				if task.Retries < 3 {
					task.Retries++
					// Re-queue for retry with backoff
					go func() {
						time.Sleep(time.Duration(task.Retries) * time.Second)
						if err := p.Submit(task.Context, task.Task, task.State); err != nil {
							if p.logger != nil {
								p.logger.Error(p.ctx, "Failed to re-queue task for retry", err,
									map[string]interface{}{"task_id": task.Task.ID})
							}
						}
					}()
				} else {
					// Max retries exceeded
					select {
					case p.errorCh <- fmt.Errorf("task %s failed after %d retries: %w",
						task.Task.ID, task.Retries, err):
					default:
						// Error channel full, log it
						if p.logger != nil {
							p.logger.Error(p.ctx, "Failed to send error - channel full", err,
								map[string]interface{}{"task_id": task.Task.ID},
							)
						}
					}
				}
			} else {
				p.metrics.tasksCompleted.Add(1)
				// Send result
				select {
				case p.resultCh <- result:
				default:
					// Result channel full, log it
					if p.logger != nil {
						p.logger.Warn(p.ctx, "Result channel full - dropping result",
							map[string]interface{}{"task_id": task.Task.ID},
						)
					}
				}
			}

		case <-p.ctx.Done():
			// Pool is shutting down
			if p.logger != nil {
				p.logger.Debug(p.ctx, "Worker stopping - context cancelled",
					map[string]interface{}{"worker_id": worker.id},
				)
			}
			return
		}
	}
}

// GetHealthStatus returns the current health status via the health monitor
func (p *WorkerPool) GetHealthStatus() HealthStatus {
	if p.healthMonitor != nil {
		return p.healthMonitor.GetHealthStatus()
	}
	return HealthStatus{}
}

// GetMetricsSummary returns metrics summary via the metrics collector
func (p *WorkerPool) GetMetricsSummary() MetricsSummary {
	if p.metricsCollector != nil {
		return p.metricsCollector.GetMetricsSummary()
	}
	return MetricsSummary{}
}

// WaitForCompletion waits for all tasks to complete or timeout
func (p *WorkerPool) WaitForCompletion(ctx context.Context, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if p.metrics.queueDepth.Load() == 0 &&
				p.metrics.tasksProcessing.Load() == 0 {
				return nil
			}
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for tasks to complete")
		}
	}
}

// Note: CircuitBreaker, ResourceLimiter, HealthMonitor, and MetricsCollector
// have been extracted to separate files for better maintainability
