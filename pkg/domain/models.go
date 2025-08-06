package domain

import (
	"time"
)

// TaskStatus represents the current state of a research task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
)

// Phase represents the current phase of the research workflow
type Phase string

const (
	PhaseStart         Phase = "start"
	PhaseClarification Phase = "clarification"
	PhasePlanning      Phase = "planning"
	PhaseResearch      Phase = "research"
	PhaseCompression   Phase = "compression"
	PhaseReporting     Phase = "reporting"
	PhaseComplete      Phase = "complete"
)

// ResearchRequest represents an incoming research request
type ResearchRequest struct {
	ID        string                 `json:"id"`
	Query     string                 `json:"query"`
	Context   string                 `json:"context,omitempty"`
	MaxDepth  int                    `json:"max_depth,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ResearchTask represents a specific research task to be executed
type ResearchTask struct {
	ID             string                 `json:"id"`
	Topic          string                 `json:"topic"`
	ParentID       string                 `json:"parent_id,omitempty"`
	Status         TaskStatus             `json:"status"`
	Priority       int                    `json:"priority"`
	AssignedWorker string                 `json:"assigned_worker,omitempty"`
	Results        []ResearchResult       `json:"results,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ResearchResult represents the outcome of a research task
type ResearchResult struct {
	ID         string                 `json:"id"`
	TaskID     string                 `json:"task_id"`
	Source     string                 `json:"source"`
	Content    string                 `json:"content"`
	Summary    string                 `json:"summary,omitempty"`
	Confidence float64                `json:"confidence"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

// ResearchReport represents the final research report
type ResearchReport struct {
	ID          string                 `json:"id"`
	RequestID   string                 `json:"request_id"`
	Executive   string                 `json:"executive_summary"`
	Sections    []ReportSection        `json:"sections"`
	Findings    []Finding              `json:"findings"`
	Sources     []Source               `json:"sources"`
	GeneratedAt time.Time              `json:"generated_at"`
	TokensUsed  int                    `json:"tokens_used"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ReportSection represents a section in the research report
type ReportSection struct {
	ID       string                 `json:"id"`
	Title    string                 `json:"title"`
	Content  string                 `json:"content"`
	Order    int                    `json:"order"`
	Level    int                    `json:"level"` // Heading level (1-6)
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Finding represents a key finding from the research
type Finding struct {
	ID         string                 `json:"id"`
	Title      string                 `json:"title"`
	Content    string                 `json:"content"`
	Confidence float64                `json:"confidence"`
	Sources    []string               `json:"source_ids"` // References to Source IDs
	Tags       []string               `json:"tags,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Source represents a source of information used in the research
type Source struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"` // "web", "document", "api", etc.
	Title       string                 `json:"title"`
	URL         string                 `json:"url,omitempty"`
	Author      string                 `json:"author,omitempty"`
	PublishedAt *time.Time             `json:"published_at,omitempty"`
	AccessedAt  time.Time              `json:"accessed_at"`
	Relevance   float64                `json:"relevance"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Message represents a message in the research workflow
type Message struct {
	Role      string                 `json:"role"` // "system", "user", "assistant", "tool"
	Content   string                 `json:"content"`
	ToolCalls []ToolCall             `json:"tool_calls,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ToolCall represents a tool invocation
type ToolCall struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Args     map[string]interface{} `json:"arguments"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Duration time.Duration          `json:"duration,omitempty"`
}

// WorkflowContext represents context passed through the workflow
type WorkflowContext struct {
	RequestID string                 `json:"request_id"`
	TraceID   string                 `json:"trace_id"`
	SpanID    string                 `json:"span_id"`
	Baggage   map[string]string      `json:"baggage,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}
