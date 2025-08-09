package workflow

import (
	"context"
	"sync"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
)

// PoolTaskTracker manages task tracking for the researcher pool
type PoolTaskTracker struct {
	activeTasks map[string]*TaskTracking
	mu          sync.RWMutex

	// Metrics updater interface
	metrics PoolMetricsUpdater

	// Dependencies
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger
}

// PoolMetricsUpdater interface for updating pool metrics
type PoolMetricsUpdater interface {
	IncrementTasksProcessing()
	DecrementTasksProcessing()
	IncrementTasksCompleted()
	IncrementTasksFailed()
	DecrementTasksQueued()
	AddTaskDuration(duration time.Duration)
	UpdateLastCompletionTime(t time.Time)
}

// NewPoolTaskTracker creates a new task tracker
func NewPoolTaskTracker(metrics PoolMetricsUpdater, telemetry *observability.Telemetry, logger *observability.StructuredLogger) *PoolTaskTracker {
	return &PoolTaskTracker{
		activeTasks: make(map[string]*TaskTracking),
		metrics:     metrics,
		telemetry:   telemetry,
		logger:      logger,
	}
}

// RegisterTaskStart registers when a task starts processing
func (t *PoolTaskTracker) RegisterTaskStart(task *domain.ResearchTask, workerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.activeTasks[task.ID] = &TaskTracking{
		TaskID:    task.ID,
		WorkerID:  workerID,
		StartTime: time.Now(),
		Topic:     task.Topic,
	}

	t.metrics.DecrementTasksQueued()
	t.metrics.IncrementTasksProcessing()
}

// TrackTaskCompletion updates metrics when a task completes successfully
func (t *PoolTaskTracker) TrackTaskCompletion(result *domain.ResearchResult) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Find and remove the active task
	if tracking, exists := t.activeTasks[result.TaskID]; exists {
		// Calculate duration
		duration := time.Since(tracking.StartTime)
		t.metrics.AddTaskDuration(duration)

		// Update completion metrics
		t.metrics.DecrementTasksProcessing()
		t.metrics.IncrementTasksCompleted()

		// Update last completion time
		t.metrics.UpdateLastCompletionTime(time.Now())

		// Log completion
		t.logger.Debug(context.Background(), "Task completed",
			map[string]interface{}{
				"task_id":    result.TaskID,
				"worker_id":  tracking.WorkerID,
				"duration":   duration.Seconds(),
				"confidence": result.Confidence,
			},
		)

		// Record telemetry if available
		if t.telemetry != nil {
			_ = t.telemetry.RecordMetric(context.Background(), "task_completion_duration_seconds",
				duration.Seconds(),
				attribute.String("status", "success"),
				attribute.String("worker_id", tracking.WorkerID),
			)
		}

		// Remove from active tasks
		delete(t.activeTasks, result.TaskID)
	}
}

// TrackTaskError updates metrics when a task fails
func (t *PoolTaskTracker) TrackTaskError(taskID string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Find and remove the active task
	if tracking, exists := t.activeTasks[taskID]; exists {
		// Calculate duration
		duration := time.Since(tracking.StartTime)

		// Update failure metrics
		t.metrics.DecrementTasksProcessing()
		t.metrics.IncrementTasksFailed()

		// Log failure
		t.logger.Warn(context.Background(), "Task failed",
			map[string]interface{}{
				"task_id":   taskID,
				"worker_id": tracking.WorkerID,
				"duration":  duration.Seconds(),
				"error":     err.Error(),
			},
		)

		// Record telemetry if available
		if t.telemetry != nil {
			_ = t.telemetry.RecordMetric(context.Background(), "task_completion_duration_seconds",
				duration.Seconds(),
				attribute.String("status", "error"),
				attribute.String("worker_id", tracking.WorkerID),
			)
		}

		// Remove from active tasks
		delete(t.activeTasks, taskID)
	}
}

// GetActiveTasks returns a snapshot of currently active tasks
func (t *PoolTaskTracker) GetActiveTasks() []map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	activeTasks := make([]map[string]interface{}, 0, len(t.activeTasks))
	for _, tracking := range t.activeTasks {
		activeTasks = append(activeTasks, map[string]interface{}{
			"task_id":   tracking.TaskID,
			"worker_id": tracking.WorkerID,
			"topic":     tracking.Topic,
			"duration":  time.Since(tracking.StartTime).Seconds(),
		})
	}

	return activeTasks
}
