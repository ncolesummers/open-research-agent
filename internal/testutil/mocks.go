package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
)

// MockLLMClient is a mock implementation of LLMClient for testing
type MockLLMClient struct {
	mu           sync.Mutex
	Responses    map[string]string
	CallCount    int
	LastMessages []domain.Message
	ShouldError  bool
	ErrorMessage string
	// ChatFunc allows custom chat behavior for tests
	ChatFunc func(ctx context.Context, messages []domain.Message, options domain.ChatOptions) (*domain.ChatResponse, error)
}

// NewMockLLMClient creates a new mock LLM client
func NewMockLLMClient() *MockLLMClient {
	return &MockLLMClient{
		Responses: make(map[string]string),
	}
}

// Chat implements domain.LLMClient
func (m *MockLLMClient) Chat(ctx context.Context, messages []domain.Message, options domain.ChatOptions) (*domain.ChatResponse, error) {
	// If ChatFunc is provided, use it without lock for concurrency testing
	if m.ChatFunc != nil {
		// Track call count atomically for concurrent calls
		m.mu.Lock()
		m.CallCount++
		m.LastMessages = messages
		m.mu.Unlock()
		return m.ChatFunc(ctx, messages, options)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.CallCount++
	m.LastMessages = messages

	if m.ShouldError {
		return nil, fmt.Errorf("%s", m.ErrorMessage)
	}

	// Return predefined response or default
	var content string
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if resp, ok := m.Responses[lastMsg.Content]; ok {
			content = resp
		} else if resp, ok := m.Responses["default"]; ok {
			content = resp
		} else {
			content = "Mock response"
		}
	}

	return &domain.ChatResponse{
		Content: content,
		Usage: domain.TokenUsage{
			PromptTokens:     50,
			CompletionTokens: 50,
			TotalTokens:      100,
		},
		FinishReason: "stop",
	}, nil
}

// Stream implements domain.LLMClient
func (m *MockLLMClient) Stream(ctx context.Context, messages []domain.Message, options domain.ChatOptions) (<-chan domain.ChatStreamResponse, error) {
	if m.ShouldError {
		return nil, fmt.Errorf("%s", m.ErrorMessage)
	}

	ch := make(chan domain.ChatStreamResponse, 1)
	go func() {
		defer close(ch)
		ch <- domain.ChatStreamResponse{
			Content: "Mock stream response",
			Done:    true,
		}
	}()
	return ch, nil
}

// Embed implements domain.LLMClient
func (m *MockLLMClient) Embed(ctx context.Context, text string) ([]float64, error) {
	if m.ShouldError {
		return nil, fmt.Errorf("%s", m.ErrorMessage)
	}
	// Return mock embeddings
	return []float64{0.1, 0.2, 0.3, 0.4, 0.5}, nil
}

// GetCallCount returns the number of Chat calls made
func (m *MockLLMClient) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.CallCount
}

// MockToolRegistry is a mock implementation of ToolRegistry
type MockToolRegistry struct {
	mu    sync.Mutex
	tools map[string]domain.Tool
}

// NewMockToolRegistry creates a new mock tool registry
func NewMockToolRegistry() *MockToolRegistry {
	return &MockToolRegistry{
		tools: make(map[string]domain.Tool),
	}
}

// Register implements domain.ToolRegistry
func (r *MockToolRegistry) Register(tool domain.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
	return nil
}

// Get implements domain.ToolRegistry
func (r *MockToolRegistry) Get(name string) (domain.Tool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tool, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool, nil
}

// List implements domain.ToolRegistry
func (r *MockToolRegistry) List() []domain.Tool {
	r.mu.Lock()
	defer r.mu.Unlock()
	tools := make([]domain.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// Execute implements domain.ToolRegistry
func (r *MockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	tool, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	return tool.Execute(ctx, args)
}

// MockTool is a mock implementation of Tool
type MockTool struct {
	ToolName        string
	ToolDescription string
	ExecuteFunc     func(context.Context, map[string]interface{}) (interface{}, error)
}

// Name implements domain.Tool
func (t *MockTool) Name() string {
	return t.ToolName
}

// Description implements domain.Tool
func (t *MockTool) Description() string {
	return t.ToolDescription
}

// Execute implements domain.Tool
func (t *MockTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if t.ExecuteFunc != nil {
		return t.ExecuteFunc(ctx, params)
	}
	return "mock result", nil
}

// Schema implements domain.Tool
func (t *MockTool) Schema() domain.ToolSchema {
	return domain.ToolSchema{
		Type:       "object",
		Properties: make(map[string]domain.SchemaProperty),
	}
}
