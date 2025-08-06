package state_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/internal/testutil"
	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/state"
)

func TestNewGraphState(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	config := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      50,
	}

	s := state.NewGraphState(*req, config)

	if s.Request.Query != "test query" {
		t.Errorf("Request.Query = %v, want test query", s.Request.Query)
	}

	if s.MaxIterations != 10 {
		t.Errorf("MaxIterations = %v, want 10", s.MaxIterations)
	}

	if s.Iterations != 0 {
		t.Errorf("Iterations = %v, want 0", s.Iterations)
	}
}

func TestGraphState_SetAndGetPhase(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	// Test initial phase
	if s.GetPhase() != domain.PhaseClarification {
		t.Errorf("Initial phase = %v, want %v", s.GetPhase(), domain.PhaseClarification)
	}

	// Test setting phase
	s.SetPhase(domain.PhaseResearch)
	if s.GetPhase() != domain.PhaseResearch {
		t.Errorf("Phase after set = %v, want %v", s.GetPhase(), domain.PhaseResearch)
	}
}

func TestGraphState_IncrementIteration(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	initial := s.Iterations
	s.IncrementIteration()

	if s.Iterations != initial+1 {
		t.Errorf("Iterations after increment = %v, want %v", s.Iterations, initial+1)
	}
}

func TestGraphState_AddTask(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	task := testutil.NewTestTask("test topic")
	s.AddTask(task)

	retrieved, exists := s.GetTask(task.ID)
	if !exists {
		t.Error("Task not found after adding")
	}

	if retrieved.Topic != "test topic" {
		t.Errorf("Retrieved task topic = %v, want test topic", retrieved.Topic)
	}
}

func TestGraphState_UpdateTask(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	task := testutil.NewTestTask("test topic")
	s.AddTask(task)

	// Update task status
	err := s.UpdateTask(task.ID, func(t *domain.ResearchTask) {
		t.Status = domain.TaskStatusCompleted
		now := time.Now()
		t.CompletedAt = &now
	})
	if err != nil {
		t.Errorf("UpdateTask failed: %v", err)
	}

	updated, _ := s.GetTask(task.ID)
	if updated.Status != domain.TaskStatusCompleted {
		t.Errorf("Task status after update = %v, want %v", updated.Status, domain.TaskStatusCompleted)
	}

	if updated.CompletedAt == nil {
		t.Error("CompletedAt not set after update")
	}
}

func TestGraphState_GetNextPendingTask(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	// Add multiple tasks with different statuses
	task1 := &domain.ResearchTask{
		ID:       "task-1",
		Topic:    "topic-1",
		Status:   domain.TaskStatusCompleted,
		Priority: 1,
	}

	task2 := &domain.ResearchTask{
		ID:       "task-2",
		Topic:    "topic-2",
		Status:   domain.TaskStatusPending,
		Priority: 2, // Higher priority value = higher priority
	}

	task3 := &domain.ResearchTask{
		ID:       "task-3",
		Topic:    "topic-3",
		Status:   domain.TaskStatusPending,
		Priority: 1, // Lower priority value = lower priority
	}

	s.AddTask(task1)
	s.AddTask(task2)
	s.AddTask(task3)

	// Should return highest priority pending task (task-2 has priority 2)
	next := s.GetNextPendingTask()
	if next == nil {
		t.Fatal("Expected pending task, got nil")
		return // This return is never reached but helps the linter understand
	}

	if next.ID != "task-2" {
		t.Errorf("Next task ID = %v, want task-2", next.ID)
	}
}

func TestGraphState_GetTaskStats(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	// Add tasks with different statuses
	tasks := []*domain.ResearchTask{
		{ID: "1", Status: domain.TaskStatusPending},
		{ID: "2", Status: domain.TaskStatusPending},
		{ID: "3", Status: domain.TaskStatusInProgress},
		{ID: "4", Status: domain.TaskStatusCompleted},
		{ID: "5", Status: domain.TaskStatusFailed},
	}

	for _, task := range tasks {
		s.AddTask(task)
	}

	stats := s.GetTaskStats()

	if stats.Total != 5 {
		t.Errorf("Total tasks = %v, want 5", stats.Total)
	}

	if stats.Pending != 2 {
		t.Errorf("Pending tasks = %v, want 2", stats.Pending)
	}

	if stats.InProgress != 1 {
		t.Errorf("InProgress tasks = %v, want 1", stats.InProgress)
	}

	if stats.Completed != 1 {
		t.Errorf("Completed tasks = %v, want 1", stats.Completed)
	}

	if stats.Failed != 1 {
		t.Errorf("Failed tasks = %v, want 1", stats.Failed)
	}
}

func TestGraphState_AddMessage(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	msg := domain.Message{
		Role:    "user",
		Content: "test message",
	}

	s.AddMessage(msg)

	messages := s.Messages
	if len(messages) != 1 {
		t.Errorf("Messages count = %v, want 1", len(messages))
	}

	if messages[0].Content != "test message" {
		t.Errorf("Message content = %v, want test message", messages[0].Content)
	}
}

func TestGraphState_AddResult(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	result := *testutil.NewTestResult("task-1", "test content")
	s.AddResult(result)

	if len(s.Results) != 1 {
		t.Errorf("Results count = %v, want 1", len(s.Results))
	}

	if s.Results[0].Content != "test content" {
		t.Errorf("Result content = %v, want test content", s.Results[0].Content)
	}
}

func TestGraphState_ConcurrentAccess(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	var wg sync.WaitGroup
	numGoroutines := 10
	tasksPerGoroutine := 10

	// Concurrent task additions
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				task := &domain.ResearchTask{
					ID:     fmt.Sprintf("task-%d-%d", id, j),
					Topic:  fmt.Sprintf("topic-%d-%d", id, j),
					Status: domain.TaskStatusPending,
				}
				s.AddTask(task)
			}
		}(i)
	}

	// Concurrent task updates
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				taskID := fmt.Sprintf("task-%d-%d", id, j)
				err := s.UpdateTask(taskID, func(t *domain.ResearchTask) {
					t.Status = domain.TaskStatusCompleted
				})
				if err != nil {
					// Ignore errors in concurrent test - tasks might not exist
					continue
				}
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.GetTaskStats()
			_ = s.GetNextPendingTask()
			_ = s.GetPhase()
		}()
	}

	wg.Wait()

	// Verify all tasks were added
	stats := s.GetTaskStats()
	expectedTotal := numGoroutines * tasksPerGoroutine
	if stats.Total != expectedTotal {
		t.Errorf("Total tasks after concurrent operations = %v, want %v", stats.Total, expectedTotal)
	}
}

func TestGraphState_SetAndGetLLMClient(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	mockClient := testutil.NewMockLLMClient()
	s.SetLLMClient(mockClient)

	retrieved := s.GetLLMClient()
	if retrieved == nil {
		t.Error("LLM client not set properly")
	}
}

func TestGraphState_SetAndGetTools(t *testing.T) {
	req := testutil.NewTestRequest("test query")
	s := state.NewGraphState(*req, nil)

	mockRegistry := testutil.NewMockToolRegistry()
	s.SetTools(mockRegistry)

	retrieved := s.GetTools()
	if retrieved == nil {
		t.Error("Tools registry not set properly")
	}
}
