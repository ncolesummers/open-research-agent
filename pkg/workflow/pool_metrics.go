package workflow

import (
	"sync/atomic"
	"time"
)

// PoolMetrics manages all metrics for the researcher pool
type PoolMetrics struct {
	// Task counts
	tasksQueued     atomic.Int64
	tasksProcessing atomic.Int64
	tasksCompleted  atomic.Int64
	tasksFailed     atomic.Int64

	// Duration tracking
	totalTaskDuration  atomic.Int64 // nanoseconds
	lastCompletionTime atomic.Int64 // unix timestamp
}

// NewPoolMetrics creates a new metrics manager
func NewPoolMetrics() *PoolMetrics {
	return &PoolMetrics{}
}

// IncrementTasksQueued increments the queued task count
func (m *PoolMetrics) IncrementTasksQueued() {
	m.tasksQueued.Add(1)
}

// DecrementTasksQueued decrements the queued task count
func (m *PoolMetrics) DecrementTasksQueued() {
	m.tasksQueued.Add(-1)
}

// IncrementTasksProcessing increments the processing task count
func (m *PoolMetrics) IncrementTasksProcessing() {
	m.tasksProcessing.Add(1)
}

// DecrementTasksProcessing decrements the processing task count
func (m *PoolMetrics) DecrementTasksProcessing() {
	m.tasksProcessing.Add(-1)
}

// IncrementTasksCompleted increments the completed task count
func (m *PoolMetrics) IncrementTasksCompleted() {
	m.tasksCompleted.Add(1)
}

// IncrementTasksFailed increments the failed task count
func (m *PoolMetrics) IncrementTasksFailed() {
	m.tasksFailed.Add(1)
}

// AddTaskDuration adds a task duration to the total
func (m *PoolMetrics) AddTaskDuration(duration time.Duration) {
	m.totalTaskDuration.Add(int64(duration))
}

// UpdateLastCompletionTime updates the last completion timestamp
func (m *PoolMetrics) UpdateLastCompletionTime(t time.Time) {
	m.lastCompletionTime.Store(t.Unix())
}

// GetTasksQueued returns the number of queued tasks
func (m *PoolMetrics) GetTasksQueued() int64 {
	return m.tasksQueued.Load()
}

// GetTasksProcessing returns the number of processing tasks
func (m *PoolMetrics) GetTasksProcessing() int64 {
	return m.tasksProcessing.Load()
}

// GetTasksCompleted returns the number of completed tasks
func (m *PoolMetrics) GetTasksCompleted() int64 {
	return m.tasksCompleted.Load()
}

// GetTasksFailed returns the number of failed tasks
func (m *PoolMetrics) GetTasksFailed() int64 {
	return m.tasksFailed.Load()
}

// GetAverageTaskDuration returns the average task duration in seconds
func (m *PoolMetrics) GetAverageTaskDuration() float64 {
	completed := m.tasksCompleted.Load()
	failed := m.tasksFailed.Load()
	totalTasks := completed + failed

	if totalTasks == 0 {
		return 0
	}

	totalDuration := m.totalTaskDuration.Load()
	return float64(totalDuration) / float64(totalTasks) / 1e9 // Convert to seconds
}

// GetLastCompletionTime returns the last task completion time
func (m *PoolMetrics) GetLastCompletionTime() time.Time {
	unix := m.lastCompletionTime.Load()
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

// GetSnapshot returns a snapshot of all metrics
func (m *PoolMetrics) GetSnapshot() map[string]interface{} {
	return map[string]interface{}{
		"tasks_queued":      m.GetTasksQueued(),
		"tasks_processing":  m.GetTasksProcessing(),
		"tasks_completed":   m.GetTasksCompleted(),
		"tasks_failed":      m.GetTasksFailed(),
		"avg_task_duration": m.GetAverageTaskDuration(),
		"last_completion":   m.GetLastCompletionTime().Format(time.RFC3339),
	}
}
