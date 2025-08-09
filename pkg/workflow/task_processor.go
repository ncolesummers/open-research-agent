package workflow

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TaskProcessor manages the task processing lifecycle
type TaskProcessor struct {
	workerID  string
	executor  *ResearchExecutor
	health    *WorkerHealthManager
	metrics   *WorkerMetrics
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger

	// Channels for results and errors
	resultChan chan<- *domain.ResearchResult
	errorChan  chan<- error
}

// NewTaskProcessor creates a new task processor
func NewTaskProcessor(
	workerID string,
	executor *ResearchExecutor,
	health *WorkerHealthManager,
	metrics *WorkerMetrics,
	resultChan chan<- *domain.ResearchResult,
	errorChan chan<- error,
	telemetry *observability.Telemetry,
) *TaskProcessor {
	return &TaskProcessor{
		workerID:   workerID,
		executor:   executor,
		health:     health,
		metrics:    metrics,
		resultChan: resultChan,
		errorChan:  errorChan,
		telemetry:  telemetry,
		logger:     observability.NewStructuredLogger(fmt.Sprintf("task_processor_%s", workerID)),
	}
}

// ProcessTask processes a research task with full lifecycle management
func (p *TaskProcessor) ProcessTask(ctx context.Context, task *domain.ResearchTask) {
	// Create task context with timeout
	taskCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Start observability span
	var span trace.Span
	if p.telemetry != nil {
		taskCtx, span = p.telemetry.StartSpan(taskCtx, "task_processor.process",
			trace.WithAttributes(
				attribute.String("worker.id", p.workerID),
				attribute.String("task.id", task.ID),
				attribute.String("task.topic", task.Topic),
				attribute.Int("task.priority", task.Priority),
			),
		)
		defer span.End()
	}

	// Check health before processing
	if !p.health.CanExecute() {
		err := fmt.Errorf("worker %s cannot execute: health check failed", p.workerID)
		p.handleError(taskCtx, err, task, span)
		return
	}

	// Record task start
	startTime := p.metrics.RecordTaskStart(task.ID)
	p.health.StartResourceTracking()
	defer p.health.StopResourceTracking()

	// Add panic recovery
	defer p.recoverFromPanic(taskCtx, task)

	// Log task start
	p.logger.Debug(taskCtx, "Starting task processing",
		map[string]interface{}{
			"worker_id":  p.workerID,
			"task_id":    task.ID,
			"task_topic": task.Topic,
		},
	)

	// Execute the research
	result, err := p.executeWithRetry(taskCtx, task)

	// Process the result
	if err != nil {
		p.handleTaskFailure(taskCtx, task, err, span, startTime)
	} else {
		p.handleTaskSuccess(taskCtx, task, result, span, startTime)
	}
}

// executeWithRetry executes the research with retry logic
func (p *TaskProcessor) executeWithRetry(ctx context.Context, task *domain.ResearchTask) (*domain.ResearchResult, error) {
	maxRetries := 2
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt) * 2 * time.Second
			p.logger.Debug(ctx, "Retrying task execution",
				map[string]interface{}{
					"worker_id": p.workerID,
					"task_id":   task.ID,
					"attempt":   attempt,
					"backoff":   backoff.String(),
				},
			)
			time.Sleep(backoff)
		}

		result, err := p.executor.ExecuteResearch(ctx, task)
		if err == nil {
			return result, nil
		}

		lastErr = err
		p.logger.Warn(ctx, "Task execution attempt failed",
			map[string]interface{}{
				"worker_id": p.workerID,
				"task_id":   task.ID,
				"attempt":   attempt,
				"error":     err.Error(),
			},
		)
	}

	return nil, fmt.Errorf("task failed after %d retries: %w", maxRetries, lastErr)
}

// handleTaskSuccess handles successful task completion
func (p *TaskProcessor) handleTaskSuccess(ctx context.Context, task *domain.ResearchTask, result *domain.ResearchResult, span trace.Span, startTime time.Time) {
	// Update metrics
	p.metrics.RecordTaskCompletion(task.ID, startTime, true, result.Confidence)
	p.health.RecordSuccess()

	// Update span
	if span != nil {
		span.SetStatus(codes.Ok, "Task completed successfully")
		span.SetAttributes(
			attribute.Float64("result.confidence", result.Confidence),
			attribute.String("result.id", result.ID),
		)
	}

	// Send result
	select {
	case p.resultChan <- result:
		p.logger.Info(ctx, "Task completed successfully",
			map[string]interface{}{
				"worker_id":  p.workerID,
				"task_id":    task.ID,
				"result_id":  result.ID,
				"confidence": result.Confidence,
				"duration":   time.Since(startTime).String(),
			},
		)
	case <-ctx.Done():
		p.logger.Warn(ctx, "Failed to send result: context cancelled",
			map[string]interface{}{
				"worker_id": p.workerID,
				"task_id":   task.ID,
			},
		)
	}
}

// handleTaskFailure handles task execution failure
func (p *TaskProcessor) handleTaskFailure(ctx context.Context, task *domain.ResearchTask, err error, span trace.Span, startTime time.Time) {
	// Update metrics
	p.metrics.RecordTaskCompletion(task.ID, startTime, false, 0)
	_ = p.health.RecordFailure()

	// Handle error
	p.handleError(ctx, err, task, span)
}

// handleError handles and reports errors
func (p *TaskProcessor) handleError(ctx context.Context, err error, task *domain.ResearchTask, span trace.Span) {
	if span != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	p.logger.Error(ctx, "Task processing failed", err,
		map[string]interface{}{
			"worker_id":  p.workerID,
			"task_id":    task.ID,
			"task_topic": task.Topic,
		},
	)

	// Send error through error channel
	select {
	case p.errorChan <- fmt.Errorf("worker %s failed task %s: %w", p.workerID, task.ID, err):
	case <-ctx.Done():
		// Context cancelled, don't block
	default:
		// Error channel might be full
		p.logger.Warn(ctx, "Failed to send error: channel might be full",
			map[string]interface{}{
				"worker_id": p.workerID,
				"task_id":   task.ID,
			},
		)
	}
}

// recoverFromPanic recovers from panics during task processing
func (p *TaskProcessor) recoverFromPanic(ctx context.Context, task *domain.ResearchTask) {
	if r := recover(); r != nil {
		err := fmt.Errorf("task processor panic: %v", r)
		p.logger.Error(ctx, "Panic recovered", err,
			map[string]interface{}{
				"worker_id": p.workerID,
				"task_id":   task.ID,
				"stack":     string(debug.Stack()),
			},
		)

		// Record failure
		_ = p.health.RecordFailure()

		// Try to send error
		select {
		case p.errorChan <- err:
		default:
			// Don't block if error channel is full
		}
	}
}
