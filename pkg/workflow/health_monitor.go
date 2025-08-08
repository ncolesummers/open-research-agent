package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/observability"
)

// HealthMonitor monitors worker pool health and handles recovery
type HealthMonitor struct {
	pool      *WorkerPool
	logger    *observability.StructuredLogger
	interval  time.Duration
	stopChan  chan struct{}
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(pool *WorkerPool, interval time.Duration) *HealthMonitor {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	
	return &HealthMonitor{
		pool:     pool,
		logger:   observability.NewStructuredLogger("health_monitor"),
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

// Start begins health monitoring
func (hm *HealthMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(hm.interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			hm.checkHealth(ctx)
		case <-hm.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop stops the health monitor
func (hm *HealthMonitor) Stop() {
	close(hm.stopChan)
}

// checkHealth performs health check and recovery
func (hm *HealthMonitor) checkHealth(ctx context.Context) {
	hm.pool.mu.RLock()
	activeWorkers := hm.pool.metrics.activeWorkers.Load()
	expectedWorkers := int32(hm.pool.config.MaxWorkers)
	isRunning := hm.pool.running.Load()
	hm.pool.mu.RUnlock()
	
	if activeWorkers < expectedWorkers && isRunning {
		// Some workers have died, restart them
		diff := expectedWorkers - activeWorkers
		if hm.logger != nil {
			hm.logger.Warn(ctx, "Restarting failed workers",
				map[string]interface{}{
					"active":     activeWorkers,
					"expected":   expectedWorkers,
					"restarting": diff,
				},
			)
		}
		
		hm.restartWorkers(ctx, int(diff))
	}
	
	// Check individual worker health
	hm.checkWorkerHealth(ctx)
}

// restartWorkers restarts the specified number of workers
func (hm *HealthMonitor) restartWorkers(ctx context.Context, count int) {
	hm.pool.mu.Lock()
	defer hm.pool.mu.Unlock()
	
	for i := 0; i < count; i++ {
		worker := NewWorker(
			fmt.Sprintf("worker-restart-%d-%d", time.Now().Unix(), i),
			hm.pool,
			hm.pool.llmClient,
			hm.pool.tools,
			hm.pool.telemetry,
		)
		hm.pool.workers = append(hm.pool.workers, worker)
		hm.pool.wg.Add(1)
		go hm.pool.runWorker(worker)
		hm.pool.metrics.activeWorkers.Add(1)
	}
}

// checkWorkerHealth checks the health of individual workers
func (hm *HealthMonitor) checkWorkerHealth(ctx context.Context) {
	hm.pool.mu.RLock()
	workers := make([]*Worker, len(hm.pool.workers))
	copy(workers, hm.pool.workers)
	hm.pool.mu.RUnlock()
	
	for _, worker := range workers {
		if !worker.IsHealthy() {
			if hm.logger != nil {
				hm.logger.Warn(ctx, "Unhealthy worker detected",
					map[string]interface{}{
						"worker_id": worker.id,
					},
				)
			}
			// Could implement worker replacement here if needed
		}
	}
}

// GetHealthStatus returns the current health status
func (hm *HealthMonitor) GetHealthStatus() HealthStatus {
	hm.pool.mu.RLock()
	defer hm.pool.mu.RUnlock()
	
	activeWorkers := hm.pool.metrics.activeWorkers.Load()
	expectedWorkers := int32(hm.pool.config.MaxWorkers)
	
	status := HealthStatus{
		Healthy:         activeWorkers == expectedWorkers,
		ActiveWorkers:   int(activeWorkers),
		ExpectedWorkers: int(expectedWorkers),
		QueueDepth:      int(hm.pool.metrics.queueDepth.Load()),
		TasksCompleted:  hm.pool.metrics.tasksCompleted.Load(),
		TasksFailed:     hm.pool.metrics.tasksFailed.Load(),
	}
	
	// Calculate average processing time
	if status.TasksCompleted > 0 {
		totalTime := time.Duration(hm.pool.metrics.totalProcessTime.Load())
		status.AverageProcessingTime = totalTime / time.Duration(status.TasksCompleted)
	}
	
	return status
}

// HealthStatus represents the health status of the worker pool
type HealthStatus struct {
	Healthy               bool
	ActiveWorkers         int
	ExpectedWorkers       int
	QueueDepth            int
	TasksCompleted        int64
	TasksFailed           int64
	AverageProcessingTime time.Duration
}