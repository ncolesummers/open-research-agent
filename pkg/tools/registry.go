package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
)

// BasicRegistry is a simple implementation of ToolRegistry
type BasicRegistry struct {
	mu    sync.RWMutex
	tools map[string]domain.Tool
}

// NewBasicRegistry creates a new basic tool registry
func NewBasicRegistry() *BasicRegistry {
	return &BasicRegistry{
		tools: make(map[string]domain.Tool),
	}
}

// Register registers a new tool
func (r *BasicRegistry) Register(tool domain.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if tool == nil {
		return fmt.Errorf("tool cannot be nil")
	}

	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %s already registered", name)
	}

	r.tools[name] = tool
	return nil
}

// Get retrieves a tool by name
func (r *BasicRegistry) Get(name string) (domain.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("tool %s not found", name)
	}

	return tool, nil
}

// List returns all available tools
func (r *BasicRegistry) List() []domain.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]domain.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}

	return tools
}

// Execute executes a tool by name
func (r *BasicRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	tool, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	return tool.Execute(ctx, args)
}

// ThinkTool is a simple thinking/reasoning tool
type ThinkTool struct{}

// Name returns the tool name
func (t *ThinkTool) Name() string {
	return "think"
}

// Description returns the tool description
func (t *ThinkTool) Description() string {
	return "Think through a problem step by step"
}

// Execute executes the thinking process
func (t *ThinkTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok {
		return nil, fmt.Errorf("query is required")
	}

	// Simple implementation - in production, this would use LLM
	result := map[string]interface{}{
		"thought": fmt.Sprintf("Thinking about: %s", query),
		"steps": []string{
			"1. Understand the problem",
			"2. Break it down into components",
			"3. Analyze each component",
			"4. Synthesize findings",
		},
	}

	return result, nil
}

// Schema returns the tool's parameter schema
func (t *ThinkTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Type: "object",
		Properties: map[string]domain.SchemaProperty{
			"query": {
				Type:        "string",
				Description: "The problem or question to think about",
				Required:    true,
			},
		},
		Required: []string{"query"},
	}
}
