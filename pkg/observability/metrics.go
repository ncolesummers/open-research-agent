package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds all application metrics
type Metrics struct {
	meter metric.Meter

	// Counters
	researchRequestsTotal metric.Int64Counter
	tasksCreatedTotal     metric.Int64Counter
	tasksCompletedTotal   metric.Int64Counter
	tasksFailedTotal      metric.Int64Counter
	llmRequestsTotal      metric.Int64Counter
	llmTokensUsedTotal    metric.Int64Counter
	toolExecutionsTotal   metric.Int64Counter

	// Histograms
	researchDuration      metric.Float64Histogram
	taskDuration          metric.Float64Histogram
	llmRequestDuration    metric.Float64Histogram
	toolExecutionDuration metric.Float64Histogram

	// Gauges (using async instruments)
	activeResearchRequests metric.Int64ObservableGauge
	activeTasks            metric.Int64ObservableGauge
	queuedTasks            metric.Int64ObservableGauge

	// Values for gauges (would be updated by application)
	activeResearchCount int64
	activeTaskCount     int64
	queuedTaskCount     int64
}

// NewMetrics creates and initializes all metrics
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	m := &Metrics{
		meter: meter,
	}

	var err error

	// Initialize counters
	m.researchRequestsTotal, err = meter.Int64Counter(
		"research_requests_total",
		metric.WithDescription("Total number of research requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	m.tasksCreatedTotal, err = meter.Int64Counter(
		"tasks_created_total",
		metric.WithDescription("Total number of tasks created"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	m.tasksCompletedTotal, err = meter.Int64Counter(
		"tasks_completed_total",
		metric.WithDescription("Total number of tasks completed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	m.tasksFailedTotal, err = meter.Int64Counter(
		"tasks_failed_total",
		metric.WithDescription("Total number of tasks failed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	m.llmRequestsTotal, err = meter.Int64Counter(
		"llm_requests_total",
		metric.WithDescription("Total number of LLM requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	m.llmTokensUsedTotal, err = meter.Int64Counter(
		"llm_tokens_used_total",
		metric.WithDescription("Total number of LLM tokens used"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	m.toolExecutionsTotal, err = meter.Int64Counter(
		"tool_executions_total",
		metric.WithDescription("Total number of tool executions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	// Initialize histograms
	m.researchDuration, err = meter.Float64Histogram(
		"research_duration_seconds",
		metric.WithDescription("Duration of research requests in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	m.taskDuration, err = meter.Float64Histogram(
		"task_duration_seconds",
		metric.WithDescription("Duration of task execution in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	m.llmRequestDuration, err = meter.Float64Histogram(
		"llm_request_duration_seconds",
		metric.WithDescription("Duration of LLM requests in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	m.toolExecutionDuration, err = meter.Float64Histogram(
		"tool_execution_duration_seconds",
		metric.WithDescription("Duration of tool executions in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	// Initialize gauges
	m.activeResearchRequests, err = meter.Int64ObservableGauge(
		"active_research_requests",
		metric.WithDescription("Number of active research requests"),
		metric.WithUnit("1"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.activeResearchCount)
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}

	m.activeTasks, err = meter.Int64ObservableGauge(
		"active_tasks",
		metric.WithDescription("Number of active tasks"),
		metric.WithUnit("1"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.activeTaskCount)
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}

	m.queuedTasks, err = meter.Int64ObservableGauge(
		"queued_tasks",
		metric.WithDescription("Number of queued tasks"),
		metric.WithUnit("1"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.queuedTaskCount)
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// RecordResearchRequest records a new research request
func (m *Metrics) RecordResearchRequest(ctx context.Context, requestType string) {
	m.researchRequestsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("type", requestType),
		),
	)
	m.activeResearchCount++
}

// RecordResearchComplete records completion of a research request
func (m *Metrics) RecordResearchComplete(ctx context.Context, duration time.Duration, status string) {
	m.researchDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String("status", status),
		),
	)
	m.activeResearchCount--
}

// RecordTaskCreated records creation of a new task
func (m *Metrics) RecordTaskCreated(ctx context.Context, taskType string) {
	m.tasksCreatedTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("type", taskType),
		),
	)
	m.queuedTaskCount++
}

// RecordTaskStarted records start of task execution
func (m *Metrics) RecordTaskStarted(ctx context.Context) {
	m.queuedTaskCount--
	m.activeTaskCount++
}

// RecordTaskComplete records completion of a task
func (m *Metrics) RecordTaskComplete(ctx context.Context, duration time.Duration, status string) {
	if status == "success" {
		m.tasksCompletedTotal.Add(ctx, 1)
	} else {
		m.tasksFailedTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("reason", status),
			),
		)
	}

	m.taskDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String("status", status),
		),
	)

	m.activeTaskCount--
}

// RecordLLMRequest records an LLM request
func (m *Metrics) RecordLLMRequest(ctx context.Context, model string, promptTokens, completionTokens int64, duration time.Duration) {
	m.llmRequestsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("model", model),
		),
	)

	m.llmTokensUsedTotal.Add(ctx, promptTokens+completionTokens,
		metric.WithAttributes(
			attribute.String("model", model),
			attribute.String("type", "total"),
		),
	)

	m.llmRequestDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String("model", model),
		),
	)
}

// RecordToolExecution records a tool execution
func (m *Metrics) RecordToolExecution(ctx context.Context, toolName string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}

	m.toolExecutionsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("tool", toolName),
			attribute.String("status", status),
		),
	)

	m.toolExecutionDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			attribute.String("tool", toolName),
			attribute.String("status", status),
		),
	)
}

// GetActiveResearchCount returns the current number of active research requests
func (m *Metrics) GetActiveResearchCount() int64 {
	return m.activeResearchCount
}

// GetActiveTaskCount returns the current number of active tasks
func (m *Metrics) GetActiveTaskCount() int64 {
	return m.activeTaskCount
}

// GetQueuedTaskCount returns the current number of queued tasks
func (m *Metrics) GetQueuedTaskCount() int64 {
	return m.queuedTaskCount
}
