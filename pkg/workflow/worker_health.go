package workflow

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/observability"
)

// WorkerHealthManager manages health monitoring and circuit breaker for a worker
type WorkerHealthManager struct {
	workerID string
	breaker  *CircuitBreaker
	logger   *observability.StructuredLogger

	// Health state
	healthMu         sync.RWMutex
	isHealthy        bool
	lastCheck        time.Time
	lastTaskTime     time.Time
	consecutiveFails atomic.Int32

	// Resource tracking
	memoryUsage   atomic.Int64
	memoryLimit   int64
	checkInterval time.Duration
}

// NewWorkerHealthManager creates a new health manager
func NewWorkerHealthManager(workerID string, logger *observability.StructuredLogger) *WorkerHealthManager {
	return &WorkerHealthManager{
		workerID:      workerID,
		breaker:       NewCircuitBreaker(),
		logger:        logger,
		isHealthy:     true,
		lastCheck:     time.Now(),
		memoryLimit:   100 * 1024 * 1024, // 100MB default
		checkInterval: 30 * time.Second,
	}
}

// IsHealthy returns the current health status of the worker
func (h *WorkerHealthManager) IsHealthy() bool {
	h.healthMu.RLock()
	defer h.healthMu.RUnlock()

	// Check circuit breaker
	if !h.breaker.CanExecute() {
		return false
	}

	// Check if health check is stale
	if time.Since(h.lastCheck) > h.checkInterval*2 {
		return false
	}

	// Check memory usage
	if h.memoryUsage.Load() > h.memoryLimit {
		return false
	}

	// Check consecutive failures
	if h.consecutiveFails.Load() > 5 {
		return false
	}

	return h.isHealthy
}

// CanExecute checks if the worker can execute tasks
func (h *WorkerHealthManager) CanExecute() bool {
	if !h.IsHealthy() {
		return false
	}
	return h.breaker.CanExecute()
}

// RecordSuccess records a successful task execution
func (h *WorkerHealthManager) RecordSuccess() {
	h.breaker.RecordSuccess()
	h.consecutiveFails.Store(0)

	h.healthMu.Lock()
	h.lastTaskTime = time.Now()
	h.healthMu.Unlock()
}

// RecordFailure records a failed task execution
func (h *WorkerHealthManager) RecordFailure() error {
	err := h.breaker.RecordFailure()
	h.consecutiveFails.Add(1)

	if h.consecutiveFails.Load() > 3 {
		h.logger.Warn(context.Background(), "Multiple consecutive failures",
			map[string]interface{}{
				"worker_id": h.workerID,
				"failures":  h.consecutiveFails.Load(),
			},
		)
	}

	return err
}

// PerformHealthCheck performs a health check and updates status
func (h *WorkerHealthManager) PerformHealthCheck(ctx context.Context) {
	h.healthMu.Lock()
	defer h.healthMu.Unlock()

	h.lastCheck = time.Now()

	// Check memory
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	h.memoryUsage.Store(int64(m.Alloc))

	// Determine health status
	healthy := true

	// Check if worker has been idle too long
	if !h.lastTaskTime.IsZero() && time.Since(h.lastTaskTime) > 5*time.Minute {
		h.logger.Debug(ctx, "Worker idle",
			map[string]interface{}{
				"worker_id": h.workerID,
				"idle_time": time.Since(h.lastTaskTime).String(),
			},
		)
	}

	// Check memory threshold
	if m.Alloc > uint64(h.memoryLimit) {
		healthy = false
		h.logger.Warn(ctx, "Memory limit exceeded",
			map[string]interface{}{
				"worker_id":    h.workerID,
				"memory_bytes": m.Alloc,
				"limit_bytes":  h.memoryLimit,
			},
		)
	}

	// Check circuit breaker state
	if !h.breaker.CanExecute() {
		healthy = false
		h.logger.Warn(ctx, "Circuit breaker open",
			map[string]interface{}{
				"worker_id": h.workerID,
			},
		)
	}

	h.isHealthy = healthy
}

// StartResourceTracking begins tracking resource usage
func (h *WorkerHealthManager) StartResourceTracking() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	h.memoryUsage.Store(int64(m.Alloc))
}

// StopResourceTracking stops tracking resource usage and logs if needed
func (h *WorkerHealthManager) StopResourceTracking() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	memoryDelta := int64(m.Alloc) - h.memoryUsage.Load()
	if memoryDelta > 10*1024*1024 { // More than 10MB increase
		h.logger.Debug(context.Background(), "Significant memory increase",
			map[string]interface{}{
				"worker_id":    h.workerID,
				"memory_delta": memoryDelta,
			},
		)
	}

	h.memoryUsage.Store(int64(m.Alloc))
}

// GetLastTaskTime returns the time of the last processed task
func (h *WorkerHealthManager) GetLastTaskTime() time.Time {
	h.healthMu.RLock()
	defer h.healthMu.RUnlock()
	return h.lastTaskTime
}

// Reset resets the health manager state
func (h *WorkerHealthManager) Reset() {
	h.healthMu.Lock()
	defer h.healthMu.Unlock()

	h.isHealthy = true
	h.lastCheck = time.Now()
	h.consecutiveFails.Store(0)
	h.breaker = NewCircuitBreaker()
}

// StartHealthMonitoring starts periodic health checks
func (h *WorkerHealthManager) StartHealthMonitoring(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(h.checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				h.PerformHealthCheck(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// GetHealthStatus returns detailed health status
func (h *WorkerHealthManager) GetHealthStatus() map[string]interface{} {
	h.healthMu.RLock()
	defer h.healthMu.RUnlock()

	return map[string]interface{}{
		"worker_id":         h.workerID,
		"is_healthy":        h.isHealthy,
		"breaker_open":      !h.breaker.CanExecute(),
		"last_check":        h.lastCheck,
		"last_task":         h.lastTaskTime,
		"consecutive_fails": h.consecutiveFails.Load(),
		"memory_usage":      h.memoryUsage.Load(),
		"memory_limit":      h.memoryLimit,
	}
}
