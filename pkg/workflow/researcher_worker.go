package workflow

import (
	"context"
	"fmt"
	"sync"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
)

// ResearcherWorker represents a concurrent researcher that processes tasks
type ResearcherWorker struct {
	id         string
	llmClient  domain.LLMClient
	tools      domain.ToolRegistry
	taskChan   <-chan *domain.ResearchTask
	resultChan chan<- *domain.ResearchResult
	errorChan  chan<- error
	telemetry  *observability.Telemetry
	logger     *observability.StructuredLogger

	// Components
	executor  *ResearchExecutor
	health    *WorkerHealthManager
	metrics   *WorkerMetrics
	processor *TaskProcessor

	// Synchronization
	shutdown chan struct{}
	wg       *sync.WaitGroup

	// Task tracking callbacks
	onTaskStart func(task *domain.ResearchTask, workerID string)
	onTaskError func(taskID string, err error)
}

// NewResearcherWorker creates a new researcher worker with all components
func NewResearcherWorker(
	id string,
	llmClient domain.LLMClient,
	tools domain.ToolRegistry,
	taskChan <-chan *domain.ResearchTask,
	resultChan chan<- *domain.ResearchResult,
	errorChan chan<- error,
	telemetry *observability.Telemetry,
	wg *sync.WaitGroup,
) *ResearcherWorker {
	logger := observability.NewStructuredLogger(fmt.Sprintf("researcher_%s", id))

	// Create components
	executor := NewResearchExecutor(llmClient, tools, telemetry, logger)
	health := NewWorkerHealthManager(id, logger)
	metrics := NewWorkerMetrics(id, telemetry, logger)
	processor := NewTaskProcessor(id, executor, health, metrics, resultChan, errorChan, telemetry)

	return &ResearcherWorker{
		id:         id,
		llmClient:  llmClient,
		tools:      tools,
		taskChan:   taskChan,
		resultChan: resultChan,
		errorChan:  errorChan,
		telemetry:  telemetry,
		logger:     logger,
		executor:   executor,
		health:     health,
		metrics:    metrics,
		processor:  processor,
		shutdown:   make(chan struct{}),
		wg:         wg,
	}
}

// SetTaskCallbacks sets the task tracking callbacks
func (w *ResearcherWorker) SetTaskCallbacks(onStart func(task *domain.ResearchTask, workerID string), onError func(taskID string, err error)) {
	w.onTaskStart = onStart
	w.onTaskError = onError
}

// Start begins the worker's execution loop
func (w *ResearcherWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)

	// Start health monitoring
	w.health.StartHealthMonitoring(ctx)
}

// Stop gracefully shuts down the worker
func (w *ResearcherWorker) Stop() {
	close(w.shutdown)
}

// run is the main execution loop
func (w *ResearcherWorker) run(ctx context.Context) {
	defer w.wg.Done()

	w.logger.Info(ctx, "Researcher worker started",
		map[string]interface{}{
			"worker_id": w.id,
		},
	)

	for {
		select {
		case task, ok := <-w.taskChan:
			if !ok {
				w.logger.Info(ctx, "Task channel closed, shutting down",
					map[string]interface{}{
						"worker_id": w.id,
					},
				)
				return
			}
			if task != nil {
				// Notify task start if callback is set
				if w.onTaskStart != nil {
					w.onTaskStart(task, w.id)
				}
				w.processor.ProcessTask(ctx, task)
			}

		case <-ctx.Done():
			w.logger.Info(ctx, "Context cancelled, shutting down",
				map[string]interface{}{
					"worker_id": w.id,
				},
			)
			return

		case <-w.shutdown:
			w.logger.Info(ctx, "Shutdown signal received",
				map[string]interface{}{
					"worker_id": w.id,
				},
			)
			return
		}
	}
}

// GetMetrics returns the worker's current metrics
func (w *ResearcherWorker) GetMetrics() ResearcherStats {
	return w.metrics.GetStats()
}

// IsHealthy returns the worker's health status
func (w *ResearcherWorker) IsHealthy() bool {
	return w.health.IsHealthy()
}

// GetHealthStatus returns detailed health information
func (w *ResearcherWorker) GetHealthStatus() map[string]interface{} {
	status := w.health.GetHealthStatus()
	stats := w.metrics.GetStats()

	// Combine health and metrics data
	status["metrics"] = map[string]interface{}{
		"tasks_processed":  stats.TasksProcessed,
		"success_rate":     stats.SuccessRate,
		"avg_confidence":   stats.AvgConfidence,
		"avg_process_time": stats.AvgProcessTime.String(),
	}

	return status
}

// GetID returns the worker's ID
func (w *ResearcherWorker) GetID() string {
	return w.id
}
