package domain

import (
	"context"
	"time"
)

// ResearchService defines the main research service interface
type ResearchService interface {
	// Execute performs a complete research workflow
	Execute(ctx context.Context, request *ResearchRequest) (*ResearchReport, error)

	// GetStatus returns the current status of a research request
	GetStatus(ctx context.Context, requestID string) (*ResearchStatus, error)

	// Cancel cancels an ongoing research request
	Cancel(ctx context.Context, requestID string) error

	// List returns a list of research requests
	List(ctx context.Context, opts ListOptions) ([]*ResearchRequest, error)
}

// LLMClient defines the interface for language model interactions
type LLMClient interface {
	// Chat performs a chat completion
	Chat(ctx context.Context, messages []Message, opts ChatOptions) (*ChatResponse, error)

	// Stream performs a streaming chat completion
	Stream(ctx context.Context, messages []Message, opts ChatOptions) (<-chan ChatStreamResponse, error)

	// Embed generates embeddings for text
	Embed(ctx context.Context, text string) ([]float64, error)
}

// Tool defines the interface for research tools
type Tool interface {
	// Name returns the tool name
	Name() string

	// Description returns the tool description
	Description() string

	// Execute executes the tool with given arguments
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)

	// Schema returns the tool's parameter schema
	Schema() ToolSchema
}

// ToolRegistry manages available tools
type ToolRegistry interface {
	// Register registers a new tool
	Register(tool Tool) error

	// Get retrieves a tool by name
	Get(name string) (Tool, error)

	// List returns all available tools
	List() []Tool

	// Execute executes a tool by name
	Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error)
}

// StateStore manages workflow state persistence
type StateStore interface {
	// Save saves the current state
	Save(ctx context.Context, state *GraphState) error

	// Load loads state by request ID
	Load(ctx context.Context, requestID string) (*GraphState, error)

	// Update updates specific fields in the state
	Update(ctx context.Context, requestID string, updates map[string]interface{}) error

	// Delete removes state
	Delete(ctx context.Context, requestID string) error

	// List lists states with filtering
	List(ctx context.Context, filter StateFilter) ([]*GraphState, error)
}

// SearchClient defines the interface for web search
type SearchClient interface {
	// Search performs a web search
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
}

// DocumentLoader defines the interface for document loading
type DocumentLoader interface {
	// Load loads a document from URL or path
	Load(ctx context.Context, source string) (*Document, error)

	// Parse parses document content
	Parse(ctx context.Context, content []byte, mimeType string) (*Document, error)
}

// Supporting types for interfaces

// ResearchStatus represents the status of a research request
type ResearchStatus struct {
	RequestID    string      `json:"request_id"`
	Phase        Phase       `json:"phase"`
	Progress     float64     `json:"progress"` // 0.0 to 1.0
	TasksSummary TaskSummary `json:"tasks_summary"`
	Error        string      `json:"error,omitempty"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// TaskSummary provides a summary of task statuses
type TaskSummary struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
}

// ListOptions provides options for listing research requests
type ListOptions struct {
	Limit      int        `json:"limit,omitempty"`
	Offset     int        `json:"offset,omitempty"`
	OrderBy    string     `json:"order_by,omitempty"`
	Descending bool       `json:"descending,omitempty"`
	Filter     ListFilter `json:"filter,omitempty"`
}

// ListFilter provides filtering options
type ListFilter struct {
	Status    []TaskStatus `json:"status,omitempty"`
	StartTime *time.Time   `json:"start_time,omitempty"`
	EndTime   *time.Time   `json:"end_time,omitempty"`
	Query     string       `json:"query,omitempty"`
}

// ChatOptions provides options for chat completions
type ChatOptions struct {
	Model       string   `json:"model,omitempty"`
	Temperature float64  `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	TopP        float64  `json:"top_p,omitempty"`
	TopK        int      `json:"top_k,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// ChatResponse represents a chat completion response
type ChatResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        TokenUsage `json:"usage"`
	FinishReason string     `json:"finish_reason,omitempty"`
}

// ChatStreamResponse represents a streaming chat response chunk
type ChatStreamResponse struct {
	Content  string      `json:"content,omitempty"`
	ToolCall *ToolCall   `json:"tool_call,omitempty"`
	Usage    *TokenUsage `json:"usage,omitempty"`
	Done     bool        `json:"done"`
	Error    error       `json:"error,omitempty"`
}

// TokenUsage tracks token consumption
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ToolSchema defines the parameter schema for a tool
type ToolSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// SchemaProperty defines a property in a tool schema
type SchemaProperty struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Required    bool        `json:"required,omitempty"`
}

// SearchOptions provides options for web search
type SearchOptions struct {
	MaxResults int    `json:"max_results,omitempty"`
	Language   string `json:"language,omitempty"`
	Region     string `json:"region,omitempty"`
	TimeRange  string `json:"time_range,omitempty"`
	SafeSearch bool   `json:"safe_search,omitempty"`
}

// SearchResult represents a search result
type SearchResult struct {
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Snippet     string     `json:"snippet"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Source      string     `json:"source,omitempty"`
	Relevance   float64    `json:"relevance"`
}

// Document represents a loaded document
type Document struct {
	ID       string                 `json:"id"`
	Source   string                 `json:"source"`
	Title    string                 `json:"title,omitempty"`
	Content  string                 `json:"content"`
	MimeType string                 `json:"mime_type"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	LoadedAt time.Time              `json:"loaded_at"`
}

// StateFilter provides filtering options for state queries
type StateFilter struct {
	RequestIDs []string   `json:"request_ids,omitempty"`
	Phase      []Phase    `json:"phase,omitempty"`
	StartTime  *time.Time `json:"start_time,omitempty"`
	EndTime    *time.Time `json:"end_time,omitempty"`
}

// GraphState is forward declared here, full definition in state package
type GraphState struct {
	// This will be fully defined in pkg/state/state.go
	// Placeholder for interface compilation
}
