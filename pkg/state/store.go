package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
)

// MemoryStore is an in-memory implementation of StateStore
type MemoryStore struct {
	mu     sync.RWMutex
	states map[string]*GraphState
}

// NewMemoryStore creates a new in-memory state store
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		states: make(map[string]*GraphState),
	}
}

// Save saves the current state
func (m *MemoryStore) Save(ctx context.Context, state *GraphState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state.Request.ID == "" {
		return fmt.Errorf("request ID is required")
	}

	// Create a deep copy to avoid concurrent modification issues
	snapshot := state.GetSnapshot()
	copiedState := &GraphState{
		Request:       snapshot.Request,
		Tasks:         snapshot.Tasks,
		Messages:      snapshot.Messages,
		Results:       snapshot.Results,
		CurrentPhase:  snapshot.CurrentPhase,
		Iterations:    snapshot.Iterations,
		MaxIterations: snapshot.MaxIterations,
		Error:         snapshot.Error,
		Context:       snapshot.Context,
		Metadata:      snapshot.Metadata,
		CreatedAt:     snapshot.CreatedAt,
		UpdatedAt:     time.Now(),
	}

	m.states[state.Request.ID] = copiedState
	return nil
}

// Load loads state by request ID
func (m *MemoryStore) Load(ctx context.Context, requestID string) (*GraphState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.states[requestID]
	if !exists {
		return nil, fmt.Errorf("state not found for request ID: %s", requestID)
	}

	// Return a copy to avoid concurrent modification
	snapshot := state.GetSnapshot()
	return &GraphState{
		Request:       snapshot.Request,
		Tasks:         snapshot.Tasks,
		Messages:      snapshot.Messages,
		Results:       snapshot.Results,
		CurrentPhase:  snapshot.CurrentPhase,
		Iterations:    snapshot.Iterations,
		MaxIterations: snapshot.MaxIterations,
		Error:         snapshot.Error,
		Context:       snapshot.Context,
		Metadata:      snapshot.Metadata,
		CreatedAt:     snapshot.CreatedAt,
		UpdatedAt:     snapshot.UpdatedAt,
	}, nil
}

// Update updates specific fields in the state
func (m *MemoryStore) Update(ctx context.Context, requestID string, updates map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.states[requestID]
	if !exists {
		return fmt.Errorf("state not found for request ID: %s", requestID)
	}

	// Apply updates
	for key, value := range updates {
		switch key {
		case "phase":
			if phase, ok := value.(domain.Phase); ok {
				state.CurrentPhase = phase
			}
		case "iterations":
			if iterations, ok := value.(int); ok {
				state.Iterations = iterations
			}
		case "error":
			if err, ok := value.(error); ok {
				state.Error = err
			}
		case "metadata":
			if metadata, ok := value.(map[string]interface{}); ok {
				for k, v := range metadata {
					state.Metadata[k] = v
				}
			}
		}
	}

	state.UpdatedAt = time.Now()
	return nil
}

// Delete removes state
func (m *MemoryStore) Delete(ctx context.Context, requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.states, requestID)
	return nil
}

// List lists states with filtering
func (m *MemoryStore) List(ctx context.Context, filter domain.StateFilter) ([]*GraphState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*GraphState

	for _, state := range m.states {
		// Apply filters
		if len(filter.RequestIDs) > 0 {
			found := false
			for _, id := range filter.RequestIDs {
				if state.Request.ID == id {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if len(filter.Phase) > 0 {
			found := false
			for _, phase := range filter.Phase {
				if state.CurrentPhase == phase {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if filter.StartTime != nil && state.CreatedAt.Before(*filter.StartTime) {
			continue
		}

		if filter.EndTime != nil && state.CreatedAt.After(*filter.EndTime) {
			continue
		}

		// Create a copy of the state
		snapshot := state.GetSnapshot()
		copiedState := &GraphState{
			Request:       snapshot.Request,
			Tasks:         snapshot.Tasks,
			Messages:      snapshot.Messages,
			Results:       snapshot.Results,
			CurrentPhase:  snapshot.CurrentPhase,
			Iterations:    snapshot.Iterations,
			MaxIterations: snapshot.MaxIterations,
			Error:         snapshot.Error,
			Context:       snapshot.Context,
			Metadata:      snapshot.Metadata,
			CreatedAt:     snapshot.CreatedAt,
			UpdatedAt:     snapshot.UpdatedAt,
		}

		results = append(results, copiedState)
	}

	return results, nil
}

// FileStore is a file-based implementation of StateStore
type FileStore struct {
	mu      sync.RWMutex
	baseDir string
}

// NewFileStore creates a new file-based state store
func NewFileStore(baseDir string) *FileStore {
	return &FileStore{
		baseDir: baseDir,
	}
}

// Save saves the state to a file
func (f *FileStore) Save(ctx context.Context, state *GraphState) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if state.Request.ID == "" {
		return fmt.Errorf("request ID is required")
	}

	// Convert state to JSON
	data, err := json.MarshalIndent(state.GetSnapshot(), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Save to file
	filename := fmt.Sprintf("%s/%s.json", f.baseDir, state.Request.ID)
	// In a real implementation, we would write to file here
	// For now, this is a placeholder
	_ = filename
	_ = data

	return nil
}

// Load loads state from a file
func (f *FileStore) Load(ctx context.Context, requestID string) (*GraphState, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// In a real implementation, we would read from file here
	// For now, return not found
	return nil, fmt.Errorf("state not found for request ID: %s", requestID)
}

// Update updates specific fields in the state file
func (f *FileStore) Update(ctx context.Context, requestID string, updates map[string]interface{}) error {
	// Load, update, and save
	state, err := f.Load(ctx, requestID)
	if err != nil {
		return err
	}

	// Apply updates (similar to MemoryStore)
	for key, value := range updates {
		switch key {
		case "phase":
			if phase, ok := value.(domain.Phase); ok {
				state.SetPhase(phase)
			}
		case "iterations":
			if iterations, ok := value.(int); ok {
				state.Iterations = iterations
			}
		}
	}

	return f.Save(ctx, state)
}

// Delete removes the state file
func (f *FileStore) Delete(ctx context.Context, requestID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// In a real implementation, we would delete the file here
	return nil
}

// List lists states from files
func (f *FileStore) List(ctx context.Context, filter domain.StateFilter) ([]*GraphState, error) {
	// In a real implementation, we would read directory and filter files
	return []*GraphState{}, nil
}
