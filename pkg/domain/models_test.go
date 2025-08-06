package domain_test

import (
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
)

func TestTaskStatus(t *testing.T) {
	tests := []struct {
		name   string
		status domain.TaskStatus
		want   string
	}{
		{"Pending", domain.TaskStatusPending, "pending"},
		{"InProgress", domain.TaskStatusInProgress, "in_progress"},
		{"Completed", domain.TaskStatusCompleted, "completed"},
		{"Failed", domain.TaskStatusFailed, "failed"},
	}

	for _, tt := range tests {
		// capture range variable
		// to avoid closure issues in the loop
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.status); got != tt.want {
				t.Errorf("TaskStatus = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPhase(t *testing.T) {
	tests := []struct {
		name  string
		phase domain.Phase
		want  string
	}{
		{"Start", domain.PhaseStart, "start"},
		{"Clarification", domain.PhaseClarification, "clarification"},
		{"Planning", domain.PhasePlanning, "planning"},
		{"Research", domain.PhaseResearch, "research"},
		{"Compression", domain.PhaseCompression, "compression"},
		{"Reporting", domain.PhaseReporting, "reporting"},
		{"Complete", domain.PhaseComplete, "complete"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.phase); got != tt.want {
				t.Errorf("Phase = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResearchRequest(t *testing.T) {
	now := time.Now()
	req := domain.ResearchRequest{
		ID:        "req-123",
		Query:     "Test query",
		Context:   "Test context",
		MaxDepth:  3,
		Timestamp: now,
	}

	if req.ID != "req-123" {
		t.Errorf("ID = %v, want req-123", req.ID)
	}

	if req.Query != "Test query" {
		t.Errorf("Query = %v, want Test query", req.Query)
	}

	if req.MaxDepth != 3 {
		t.Errorf("MaxDepth = %v, want 3", req.MaxDepth)
	}

	if !req.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", req.Timestamp, now)
	}
}

func TestResearchTask(t *testing.T) {
	now := time.Now()
	task := domain.ResearchTask{
		ID:          "task-123",
		Topic:       "Test topic",
		Status:      domain.TaskStatusPending,
		Priority:    1,
		Results:     []domain.ResearchResult{},
		CreatedAt:   now,
		CompletedAt: &now,
	}

	if task.ID != "task-123" {
		t.Errorf("ID = %v, want task-123", task.ID)
	}

	if task.Status != domain.TaskStatusPending {
		t.Errorf("Status = %v, want pending", task.Status)
	}

	if task.Priority != 1 {
		t.Errorf("Priority = %v, want 1", task.Priority)
	}

	if task.CompletedAt == nil || !task.CompletedAt.Equal(now) {
		t.Errorf("CompletedAt = %v, want %v", task.CompletedAt, now)
	}
}

func TestResearchResult(t *testing.T) {
	now := time.Now()
	result := domain.ResearchResult{
		ID:         "result-123",
		TaskID:     "task-123",
		Source:     "test-source",
		Content:    "Test content",
		Summary:    "Test summary",
		Confidence: 0.95,
		Metadata: map[string]interface{}{
			"key": "value",
		},
		Timestamp: now,
	}

	if result.ID != "result-123" {
		t.Errorf("ID = %v, want result-123", result.ID)
	}

	if result.TaskID != "task-123" {
		t.Errorf("TaskID = %v, want task-123", result.TaskID)
	}

	if result.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", result.Confidence)
	}

	if val, ok := result.Metadata["key"]; !ok || val != "value" {
		t.Errorf("Metadata[key] = %v, want value", val)
	}
}

func TestResearchReport(t *testing.T) {
	now := time.Now()
	report := domain.ResearchReport{
		ID:        "report-123",
		RequestID: "req-123",
		Executive: "Executive summary",
		Sections: []domain.ReportSection{
			{
				Title:   "Section 1",
				Content: "Content 1",
				Order:   1,
			},
		},
		Findings: []domain.Finding{
			{
				ID:         "finding-1",
				Title:      "Finding 1",
				Content:    "Finding content",
				Confidence: 0.9,
			},
		},
		Sources: []domain.Source{
			{
				ID:    "source-1",
				Title: "Source 1",
				Type:  "web",
				URL:   "https://example.com",
			},
		},
		GeneratedAt: now,
		TokensUsed:  1000,
	}

	if report.ID != "report-123" {
		t.Errorf("ID = %v, want report-123", report.ID)
	}

	if len(report.Sections) != 1 || report.Sections[0].Title != "Section 1" {
		t.Errorf("Sections incorrect, got %v", report.Sections)
	}

	if len(report.Findings) != 1 || report.Findings[0].Confidence != 0.9 {
		t.Errorf("Findings incorrect, got %v", report.Findings)
	}

	if len(report.Sources) != 1 || report.Sources[0].Type != "web" {
		t.Errorf("Sources incorrect, got %v", report.Sources)
	}

	if report.TokensUsed != 1000 {
		t.Errorf("TokensUsed = %v, want 1000", report.TokensUsed)
	}
}

func TestMessage(t *testing.T) {
	msg := domain.Message{
		Role:    "user",
		Content: "Test message",
	}

	if msg.Role != "user" {
		t.Errorf("Role = %v, want user", msg.Role)
	}

	if msg.Content != "Test message" {
		t.Errorf("Content = %v, want Test message", msg.Content)
	}
}

func TestChatOptions(t *testing.T) {
	opts := domain.ChatOptions{
		Temperature: 0.7,
		MaxTokens:   2000,
		TopP:        0.9,
		TopK:        40,
	}

	if opts.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", opts.Temperature)
	}

	if opts.MaxTokens != 2000 {
		t.Errorf("MaxTokens = %v, want 2000", opts.MaxTokens)
	}

	if opts.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", opts.TopP)
	}

	if opts.TopK != 40 {
		t.Errorf("TopK = %v, want 40", opts.TopK)
	}
}

func TestChatResponse(t *testing.T) {
	resp := domain.ChatResponse{
		Content: "Response content",
		Usage: domain.TokenUsage{
			PromptTokens:     200,
			CompletionTokens: 300,
			TotalTokens:      500,
		},
	}

	if resp.Content != "Response content" {
		t.Errorf("Content = %v, want Response content", resp.Content)
	}

	if resp.Usage.TotalTokens != 500 {
		t.Errorf("TotalTokens = %v, want 500", resp.Usage.TotalTokens)
	}

	if resp.Usage.PromptTokens != 200 {
		t.Errorf("PromptTokens = %v, want 200", resp.Usage.PromptTokens)
	}
}
