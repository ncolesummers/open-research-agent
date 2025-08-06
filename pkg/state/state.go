package state

import (
	"sync"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
)

// GraphState represents the complete state of a research workflow
type GraphState struct {
	mu            sync.RWMutex
	Request       domain.ResearchRequest          `json:"request"`
	Tasks         map[string]*domain.ResearchTask `json:"tasks"`
	Messages      []domain.Message                `json:"messages"`
	Results       []domain.ResearchResult         `json:"results"`
	CurrentPhase  domain.Phase                    `json:"current_phase"`
	Iterations    int                             `json:"iterations"`
	MaxIterations int                             `json:"max_iterations"`
	Error         error                           `json:"error,omitempty"`
	Context       domain.WorkflowContext          `json:"context"`
	Metadata      map[string]interface{}          `json:"metadata,omitempty"`
	CreatedAt     time.Time                       `json:"created_at"`
	UpdatedAt     time.Time                       `json:"updated_at"`

	// Runtime fields (not persisted)
	llmClient domain.LLMClient
	tools     domain.ToolRegistry
	config    *StateConfig
}

// StateConfig provides configuration for state management
type StateConfig struct {
	MaxIterations    int           `json:"max_iterations"`
	MaxTasks         int           `json:"max_tasks"`
	TaskTimeout      time.Duration `json:"task_timeout"`
	StateTimeout     time.Duration `json:"state_timeout"`
	EnableCheckpoint bool          `json:"enable_checkpoint"`
}

// NewGraphState creates a new graph state
func NewGraphState(request domain.ResearchRequest, config *StateConfig) *GraphState {
	if config == nil {
		config = DefaultStateConfig()
	}

	return &GraphState{
		Request:       request,
		Tasks:         make(map[string]*domain.ResearchTask),
		Messages:      []domain.Message{},
		Results:       []domain.ResearchResult{},
		CurrentPhase:  domain.PhaseClarification,
		Iterations:    0,
		MaxIterations: config.MaxIterations,
		Context: domain.WorkflowContext{
			RequestID: request.ID,
			Metadata:  make(map[string]interface{}),
		},
		Metadata:  make(map[string]interface{}),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		config:    config,
	}
}

// DefaultStateConfig returns default state configuration
func DefaultStateConfig() *StateConfig {
	return &StateConfig{
		MaxIterations:    10,
		MaxTasks:         50,
		TaskTimeout:      5 * time.Minute,
		StateTimeout:     30 * time.Minute,
		EnableCheckpoint: true,
	}
}

// Thread-safe state operations

// UpdateTask updates a task in the state
func (s *GraphState) UpdateTask(taskID string, fn func(*domain.ResearchTask)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.Tasks[taskID]
	if !exists {
		task = &domain.ResearchTask{
			ID:        taskID,
			Status:    domain.TaskStatusPending,
			CreatedAt: time.Now(),
		}
		s.Tasks[taskID] = task
	}

	fn(task)
	s.UpdatedAt = time.Now()
	return nil
}

// AddTask adds a new task to the state
func (s *GraphState) AddTask(task *domain.ResearchTask) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	s.Tasks[task.ID] = task
	s.UpdatedAt = time.Now()
}

// GetTask retrieves a task by ID
func (s *GraphState) GetTask(taskID string) (*domain.ResearchTask, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.Tasks[taskID]
	return task, exists
}

// GetNextPendingTask returns the next pending task
func (s *GraphState) GetNextPendingTask() *domain.ResearchTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var nextTask *domain.ResearchTask
	highestPriority := -1

	for _, task := range s.Tasks {
		if task.Status == domain.TaskStatusPending && task.Priority > highestPriority {
			nextTask = task
			highestPriority = task.Priority
		}
	}

	return nextTask
}

// AddMessage adds a message to the conversation history
func (s *GraphState) AddMessage(msg domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
}

// GetMessages returns all messages
func (s *GraphState) GetMessages() []domain.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messages := make([]domain.Message, len(s.Messages))
	copy(messages, s.Messages)
	return messages
}

// AddResult adds a research result
func (s *GraphState) AddResult(result domain.ResearchResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if result.Timestamp.IsZero() {
		result.Timestamp = time.Now()
	}
	s.Results = append(s.Results, result)
	s.UpdatedAt = time.Now()
}

// UpdateResults updates multiple results at once
func (s *GraphState) UpdateResults(results []domain.ResearchResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, result := range results {
		if result.Timestamp.IsZero() {
			result.Timestamp = time.Now()
		}
		s.Results = append(s.Results, result)
	}
	s.UpdatedAt = time.Now()
}

// SetPhase sets the current workflow phase
func (s *GraphState) SetPhase(phase domain.Phase) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.CurrentPhase = phase
	s.UpdatedAt = time.Now()
}

// GetPhase returns the current phase
func (s *GraphState) GetPhase() domain.Phase {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.CurrentPhase
}

// IncrementIteration increments the iteration counter
func (s *GraphState) IncrementIteration() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Iterations++
	s.UpdatedAt = time.Now()
}

// SetError sets an error in the state
func (s *GraphState) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Error = err
	s.UpdatedAt = time.Now()
}

// GetError returns the current error
func (s *GraphState) GetError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Error
}

// GetSnapshot returns a snapshot of the current state
func (s *GraphState) GetSnapshot() GraphStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Deep copy tasks
	tasks := make(map[string]*domain.ResearchTask)
	for k, v := range s.Tasks {
		taskCopy := *v
		tasks[k] = &taskCopy
	}

	// Deep copy messages
	messages := make([]domain.Message, len(s.Messages))
	copy(messages, s.Messages)

	// Deep copy results
	results := make([]domain.ResearchResult, len(s.Results))
	copy(results, s.Results)

	return GraphStateSnapshot{
		Request:       s.Request,
		Tasks:         tasks,
		Messages:      messages,
		Results:       results,
		CurrentPhase:  s.CurrentPhase,
		Iterations:    s.Iterations,
		MaxIterations: s.MaxIterations,
		Error:         s.Error,
		Context:       s.Context,
		Metadata:      s.Metadata,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

// GraphStateSnapshot represents an immutable snapshot of the state
type GraphStateSnapshot struct {
	Request       domain.ResearchRequest          `json:"request"`
	Tasks         map[string]*domain.ResearchTask `json:"tasks"`
	Messages      []domain.Message                `json:"messages"`
	Results       []domain.ResearchResult         `json:"results"`
	CurrentPhase  domain.Phase                    `json:"current_phase"`
	Iterations    int                             `json:"iterations"`
	MaxIterations int                             `json:"max_iterations"`
	Error         error                           `json:"error,omitempty"`
	Context       domain.WorkflowContext          `json:"context"`
	Metadata      map[string]interface{}          `json:"metadata,omitempty"`
	CreatedAt     time.Time                       `json:"created_at"`
	UpdatedAt     time.Time                       `json:"updated_at"`
}

// Utility methods

// NeedsClarification determines if the request needs clarification
func (s *GraphState) NeedsClarification() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Simple heuristic - can be enhanced with LLM analysis
	return len(s.Request.Query) < 10 || s.Request.Query == ""
}

// ResearchComplete determines if research is complete
func (s *GraphState) ResearchComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if all tasks are completed or failed
	for _, task := range s.Tasks {
		if task.Status == domain.TaskStatusPending || task.Status == domain.TaskStatusInProgress {
			return false
		}
	}

	// Check if we have enough results
	return len(s.Results) >= 3 || s.Iterations >= s.MaxIterations
}

// GetTaskStats returns statistics about tasks
func (s *GraphState) GetTaskStats() domain.TaskSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := domain.TaskSummary{
		Total: len(s.Tasks),
	}

	for _, task := range s.Tasks {
		switch task.Status {
		case domain.TaskStatusPending:
			stats.Pending++
		case domain.TaskStatusInProgress:
			stats.InProgress++
		case domain.TaskStatusCompleted:
			stats.Completed++
		case domain.TaskStatusFailed:
			stats.Failed++
		}
	}

	return stats
}

// SetLLMClient sets the LLM client for the state
func (s *GraphState) SetLLMClient(client domain.LLMClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmClient = client
}

// GetLLMClient returns the LLM client
func (s *GraphState) GetLLMClient() domain.LLMClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.llmClient
}

// SetTools sets the tool registry for the state
func (s *GraphState) SetTools(tools domain.ToolRegistry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = tools
}

// GetTools returns the tool registry
func (s *GraphState) GetTools() domain.ToolRegistry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tools
}
