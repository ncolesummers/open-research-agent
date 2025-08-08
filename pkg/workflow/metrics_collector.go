package workflow

import (
	"context"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/metric"
)

// MetricsCollector handles metrics collection for the worker pool
type MetricsCollector struct {
	pool      *WorkerPool
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger
	interval  time.Duration
	stopChan  chan struct{}
	
	// Metric instruments
	queueDepthGauge       metric.Int64ObservableGauge
	activeWorkersGauge    metric.Int64ObservableGauge
	tasksCompletedCounter metric.Int64Counter
	tasksFailedCounter    metric.Int64Counter
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(pool *WorkerPool, telemetry *observability.Telemetry, interval time.Duration) (*MetricsCollector, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	
	mc := &MetricsCollector{
		pool:      pool,
		telemetry: telemetry,
		logger:    observability.NewStructuredLogger("metrics_collector"),
		interval:  interval,
		stopChan:  make(chan struct{}),
	}
	
	if telemetry != nil {
		if err := mc.initializeInstruments(); err != nil {
			return nil, err
		}
	}
	
	return mc, nil
}

// initializeInstruments creates the metric instruments
func (mc *MetricsCollector) initializeInstruments() error {
	meter := mc.telemetry.Meter()
	
	// Create metric instruments
	queueDepthGauge, err := meter.Int64ObservableGauge(
		"worker_pool.queue_depth",
		metric.WithDescription("Current number of tasks in queue"),
	)
	if err != nil {
		return err
	}
	mc.queueDepthGauge = queueDepthGauge
	
	activeWorkersGauge, err := meter.Int64ObservableGauge(
		"worker_pool.active_workers",
		metric.WithDescription("Number of active workers"),
	)
	if err != nil {
		return err
	}
	mc.activeWorkersGauge = activeWorkersGauge
	
	tasksCompletedCounter, err := meter.Int64Counter(
		"worker_pool.tasks_completed",
		metric.WithDescription("Total tasks completed"),
	)
	if err != nil {
		return err
	}
	mc.tasksCompletedCounter = tasksCompletedCounter
	
	tasksFailedCounter, err := meter.Int64Counter(
		"worker_pool.tasks_failed",
		metric.WithDescription("Total tasks failed"),
	)
	if err != nil {
		return err
	}
	mc.tasksFailedCounter = tasksFailedCounter
	
	// Register callbacks for observable gauges
	_, err = meter.RegisterCallback(
		mc.observeMetrics,
		mc.queueDepthGauge,
		mc.activeWorkersGauge,
	)
	
	return err
}

// observeMetrics is the callback for observable gauges
func (mc *MetricsCollector) observeMetrics(ctx context.Context, o metric.Observer) error {
	o.ObserveInt64(mc.queueDepthGauge, int64(mc.pool.metrics.queueDepth.Load()))
	o.ObserveInt64(mc.activeWorkersGauge, int64(mc.pool.metrics.activeWorkers.Load()))
	return nil
}

// Start begins metrics collection
func (mc *MetricsCollector) Start(ctx context.Context) {
	if mc.telemetry == nil {
		return
	}
	
	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			mc.collectMetrics(ctx)
		case <-mc.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop stops the metrics collector
func (mc *MetricsCollector) Stop() {
	close(mc.stopChan)
}

// collectMetrics collects and records metrics
func (mc *MetricsCollector) collectMetrics(ctx context.Context) {
	// Record counters
	mc.tasksCompletedCounter.Add(ctx, mc.pool.metrics.tasksCompleted.Load())
	mc.tasksFailedCounter.Add(ctx, mc.pool.metrics.tasksFailed.Load())
	
	// Log metrics for debugging
	if mc.logger != nil {
		avgProcessTime := time.Duration(0)
		completed := mc.pool.metrics.tasksCompleted.Load()
		if completed > 0 {
			avgProcessTime = time.Duration(mc.pool.metrics.totalProcessTime.Load() / completed)
		}
		
		mc.logger.Debug(ctx, "Worker pool metrics",
			map[string]interface{}{
				"queue_depth":      mc.pool.metrics.queueDepth.Load(),
				"active_workers":   mc.pool.metrics.activeWorkers.Load(),
				"tasks_completed":  completed,
				"tasks_failed":     mc.pool.metrics.tasksFailed.Load(),
				"tasks_processing": mc.pool.metrics.tasksProcessing.Load(),
				"avg_process_time": avgProcessTime.String(),
			},
		)
	}
}

// GetMetricsSummary returns a summary of current metrics
func (mc *MetricsCollector) GetMetricsSummary() MetricsSummary {
	completed := mc.pool.metrics.tasksCompleted.Load()
	failed := mc.pool.metrics.tasksFailed.Load()
	totalTime := time.Duration(mc.pool.metrics.totalProcessTime.Load())
	
	avgTime := time.Duration(0)
	if completed > 0 {
		avgTime = totalTime / time.Duration(completed)
	}
	
	successRate := float64(0)
	total := completed + failed
	if total > 0 {
		successRate = float64(completed) / float64(total) * 100
	}
	
	return MetricsSummary{
		TasksQueued:           mc.pool.metrics.tasksQueued.Load(),
		TasksProcessing:       mc.pool.metrics.tasksProcessing.Load(),
		TasksCompleted:        completed,
		TasksFailed:           failed,
		ActiveWorkers:         int(mc.pool.metrics.activeWorkers.Load()),
		QueueDepth:            int(mc.pool.metrics.queueDepth.Load()),
		AverageProcessingTime: avgTime,
		SuccessRate:           successRate,
	}
}

// MetricsSummary provides a summary of worker pool metrics
type MetricsSummary struct {
	TasksQueued           int64
	TasksProcessing       int64
	TasksCompleted        int64
	TasksFailed           int64
	ActiveWorkers         int
	QueueDepth            int
	AverageProcessingTime time.Duration
	SuccessRate           float64
}