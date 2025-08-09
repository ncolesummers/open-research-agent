package workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
)

// WorkerMetrics tracks performance metrics for a research worker
type WorkerMetrics struct {
	workerID  string
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger

	// Counters
	tasksProcessed atomic.Int64
	tasksSucceeded atomic.Int64
	tasksFailed    atomic.Int64

	// Timing
	totalProcessTime atomic.Int64 // nanoseconds
	lastTaskTime     atomic.Int64 // unix timestamp

	// Confidence tracking
	confidenceSum   atomic.Uint64 // Store as fixed-point (multiplied by 1000)
	confidenceCount atomic.Int64

	// Tool usage
	toolUsageMu     sync.RWMutex
	toolInvocations map[string]int64
}

// ResearcherStats represents detailed metrics for a researcher worker
type ResearcherStats struct {
	WorkerID        string           `json:"worker_id"`
	TasksProcessed  int64            `json:"tasks_processed"`
	TasksSucceeded  int64            `json:"tasks_succeeded"`
	TasksFailed     int64            `json:"tasks_failed"`
	AvgProcessTime  time.Duration    `json:"avg_process_time"`
	LastTaskTime    time.Time        `json:"last_task_time"`
	AvgConfidence   float64          `json:"avg_confidence"`
	SuccessRate     float64          `json:"success_rate"`
	ToolInvocations map[string]int64 `json:"tool_invocations"`
}

// NewWorkerMetrics creates a new metrics tracker
func NewWorkerMetrics(workerID string, telemetry *observability.Telemetry, logger *observability.StructuredLogger) *WorkerMetrics {
	return &WorkerMetrics{
		workerID:        workerID,
		telemetry:       telemetry,
		logger:          logger,
		toolInvocations: make(map[string]int64),
	}
}

// RecordTaskStart records the start of a task
func (m *WorkerMetrics) RecordTaskStart(taskID string) time.Time {
	startTime := time.Now()

	if m.telemetry != nil {
		_ = m.telemetry.RecordMetric(context.Background(), "active_researchers", 1,
			attribute.String("worker_id", m.workerID),
		)
	}

	return startTime
}

// RecordTaskCompletion records task completion with metrics
func (m *WorkerMetrics) RecordTaskCompletion(taskID string, startTime time.Time, success bool, confidence float64) {
	duration := time.Since(startTime)

	// Update counters
	m.tasksProcessed.Add(1)
	if success {
		m.tasksSucceeded.Add(1)
		// Store confidence as fixed-point
		m.confidenceSum.Add(uint64(confidence * 1000))
		m.confidenceCount.Add(1)
	} else {
		m.tasksFailed.Add(1)
	}

	// Update timing
	m.totalProcessTime.Add(int64(duration))
	m.lastTaskTime.Store(time.Now().Unix())

	// Record telemetry metrics
	if m.telemetry != nil {
		status := "success"
		if !success {
			status = "error"
		}

		_ = m.telemetry.RecordMetric(context.Background(), "workflow_node_duration_seconds",
			duration.Seconds(),
			attribute.String("node_name", "researcher"),
			attribute.String("status", status),
			attribute.String("worker_id", m.workerID),
		)

		_ = m.telemetry.RecordMetric(context.Background(), "active_researchers", -1,
			attribute.String("worker_id", m.workerID),
		)

		if success {
			_ = m.telemetry.RecordMetric(context.Background(), "research_confidence_score",
				confidence,
				attribute.String("worker_id", m.workerID),
			)
		}
	}

	// Log metrics periodically (every 10 tasks)
	if m.tasksProcessed.Load()%10 == 0 {
		m.logger.Info(context.Background(), "Worker metrics checkpoint",
			map[string]interface{}{
				"worker_id":      m.workerID,
				"tasks":          m.tasksProcessed.Load(),
				"success_rate":   m.calculateSuccessRate(),
				"avg_confidence": m.calculateAvgConfidence(),
			},
		)
	}
}

// RecordToolInvocation records that a tool was used
func (m *WorkerMetrics) RecordToolInvocation(toolName string) {
	m.toolUsageMu.Lock()
	defer m.toolUsageMu.Unlock()

	m.toolInvocations[toolName]++

	if m.telemetry != nil {
		_ = m.telemetry.RecordMetric(context.Background(), "tool_invocations_total", 1,
			attribute.String("tool_name", toolName),
			attribute.String("worker_id", m.workerID),
		)
	}
}

// GetStats returns a snapshot of current metrics
func (m *WorkerMetrics) GetStats() ResearcherStats {
	processed := m.tasksProcessed.Load()
	succeeded := m.tasksSucceeded.Load()
	failed := m.tasksFailed.Load()
	totalTime := m.totalProcessTime.Load()

	var avgProcessTime time.Duration
	if processed > 0 {
		avgProcessTime = time.Duration(totalTime / processed)
	}

	var lastTask time.Time
	if lastUnix := m.lastTaskTime.Load(); lastUnix > 0 {
		lastTask = time.Unix(lastUnix, 0)
	}

	// Copy tool invocations
	m.toolUsageMu.RLock()
	tools := make(map[string]int64)
	for k, v := range m.toolInvocations {
		tools[k] = v
	}
	m.toolUsageMu.RUnlock()

	return ResearcherStats{
		WorkerID:        m.workerID,
		TasksProcessed:  processed,
		TasksSucceeded:  succeeded,
		TasksFailed:     failed,
		AvgProcessTime:  avgProcessTime,
		LastTaskTime:    lastTask,
		AvgConfidence:   m.calculateAvgConfidence(),
		SuccessRate:     m.calculateSuccessRate(),
		ToolInvocations: tools,
	}
}

// calculateSuccessRate calculates the success rate percentage
func (m *WorkerMetrics) calculateSuccessRate() float64 {
	processed := m.tasksProcessed.Load()
	if processed == 0 {
		return 0
	}
	succeeded := m.tasksSucceeded.Load()
	return float64(succeeded) / float64(processed) * 100
}

// calculateAvgConfidence calculates the average confidence score
func (m *WorkerMetrics) calculateAvgConfidence() float64 {
	count := m.confidenceCount.Load()
	if count == 0 {
		return 0
	}
	sum := m.confidenceSum.Load()
	return float64(sum) / float64(count) / 1000.0 // Convert from fixed-point
}

// Reset resets all metrics
func (m *WorkerMetrics) Reset() {
	m.tasksProcessed.Store(0)
	m.tasksSucceeded.Store(0)
	m.tasksFailed.Store(0)
	m.totalProcessTime.Store(0)
	m.lastTaskTime.Store(0)
	m.confidenceSum.Store(0)
	m.confidenceCount.Store(0)

	m.toolUsageMu.Lock()
	m.toolInvocations = make(map[string]int64)
	m.toolUsageMu.Unlock()
}
