package workflow

import "sync"

// ResourceLimiter tracks and enforces resource limits for workers
type ResourceLimiter struct {
	mu              sync.RWMutex
	memoryLimit     int64   // bytes
	cpuLimit        float64 // percentage (0-100)
	currentMemory   int64
	currentCPU      float64
}

// NewResourceLimiter creates a new resource limiter with specified limits
func NewResourceLimiter(memoryLimit int64, cpuLimit float64) *ResourceLimiter {
	return &ResourceLimiter{
		memoryLimit: memoryLimit,
		cpuLimit:    cpuLimit,
	}
}

// CanAllocate checks if resources can be allocated
func (rl *ResourceLimiter) CanAllocate(memory int64, cpu float64) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	if rl.currentMemory+memory > rl.memoryLimit {
		return false
	}
	
	if rl.currentCPU+cpu > rl.cpuLimit {
		return false
	}
	
	return true
}

// Allocate allocates resources
func (rl *ResourceLimiter) Allocate(memory int64, cpu float64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	// Double-check with lock held
	if rl.currentMemory+memory > rl.memoryLimit {
		return false
	}
	
	if rl.currentCPU+cpu > rl.cpuLimit {
		return false
	}
	
	rl.currentMemory += memory
	rl.currentCPU += cpu
	return true
}

// Release releases allocated resources
func (rl *ResourceLimiter) Release(memory int64, cpu float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	rl.currentMemory -= memory
	rl.currentCPU -= cpu
	
	// Ensure we don't go negative
	if rl.currentMemory < 0 {
		rl.currentMemory = 0
	}
	if rl.currentCPU < 0 {
		rl.currentCPU = 0
	}
}

// GetUsage returns current resource usage
func (rl *ResourceLimiter) GetUsage() (memory int64, cpu float64) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.currentMemory, rl.currentCPU
}

// GetLimits returns resource limits
func (rl *ResourceLimiter) GetLimits() (memoryLimit int64, cpuLimit float64) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.memoryLimit, rl.cpuLimit
}

// Reset resets current usage to zero
func (rl *ResourceLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.currentMemory = 0
	rl.currentCPU = 0
}