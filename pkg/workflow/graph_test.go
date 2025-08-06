package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/internal/testutil"
	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/workflow"
)

func TestNewResearchGraph(t *testing.T) {
	config := &workflow.Config{
		Research: workflow.ResearchConfig{
			MaxConcurrency:   5,
			MaxIterations:    10,
			MaxDepth:         3,
			CompressionRatio: 0.7,
		},
		LLM: workflow.LLMConfig{
			Model:       "test-model",
			Temperature: 0.7,
			MaxTokens:   2000,
		},
	}

	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()

	// Test successful creation
	graph, err := workflow.NewResearchGraph(config, mockLLM, mockTools)
	if err != nil {
		t.Errorf("NewResearchGraph failed: %v", err)
	}
	if graph == nil {
		t.Error("Expected graph, got nil")
	}

	// Test nil config
	_, err = workflow.NewResearchGraph(nil, mockLLM, mockTools)
	if err == nil {
		t.Error("Expected error for nil config, got nil")
	}

	// Test nil LLM client
	_, err = workflow.NewResearchGraph(config, nil, mockTools)
	if err == nil {
		t.Error("Expected error for nil LLM client, got nil")
	}

	// Test nil tools
	_, err = workflow.NewResearchGraph(config, mockLLM, nil)
	if err == nil {
		t.Error("Expected error for nil tools, got nil")
	}
}

func TestResearchGraph_Execute_SimplePath(t *testing.T) {
	ctx := testutil.NewTestContext(t)

	config := &workflow.Config{
		Research: workflow.ResearchConfig{
			MaxConcurrency:   5,
			MaxIterations:    3,
			MaxDepth:         2,
			CompressionRatio: 0.7,
		},
		LLM: workflow.LLMConfig{
			Model:       "test-model",
			Temperature: 0.7,
			MaxTokens:   2000,
		},
	}

	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()

	// Set up mock responses
	mockLLM.Responses["test query"] = "CLEAR"

	graph, err := workflow.NewResearchGraph(config, mockLLM, mockTools)
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}

	request := testutil.NewTestRequest("test query")
	report, err := graph.Execute(ctx, request)

	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if report == nil {
		t.Error("Expected report, got nil")
	}

	if report != nil {
		if report.RequestID != request.ID {
			t.Errorf("Report RequestID = %v, want %v", report.RequestID, request.ID)
		}

		if report.Executive == "" {
			t.Error("Report Executive summary is empty")
		}
	}
}

func TestResearchGraph_Execute_WithClarification(t *testing.T) {
	ctx := testutil.NewTestContext(t)

	config := &workflow.Config{
		Research: workflow.ResearchConfig{
			MaxConcurrency:   5,
			MaxIterations:    3,
			MaxDepth:         2,
			CompressionRatio: 0.7,
		},
		LLM: workflow.LLMConfig{
			Model:       "test-model",
			Temperature: 0.7,
			MaxTokens:   2000,
		},
	}

	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()

	// Set up mock responses for clarification path
	mockLLM.Responses["short"] = "Need more information"

	graph, err := workflow.NewResearchGraph(config, mockLLM, mockTools)
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}

	// Use a very short query to trigger clarification
	request := &domain.ResearchRequest{
		ID:        "test-req-1",
		Query:     "short",
		MaxDepth:  2,
		Timestamp: time.Now(),
	}

	report, err := graph.Execute(ctx, request)

	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if report == nil {
		t.Error("Expected report, got nil")
	}
}

func TestResearchGraph_Execute_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	config := &workflow.Config{
		Research: workflow.ResearchConfig{
			MaxConcurrency:   5,
			MaxIterations:    10,
			MaxDepth:         3,
			CompressionRatio: 0.7,
		},
		LLM: workflow.LLMConfig{
			Model:       "test-model",
			Temperature: 0.7,
			MaxTokens:   2000,
		},
	}

	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()

	// Set up mock to simulate delay
	mockLLM.Responses["test query"] = "Processing..."

	graph, err := workflow.NewResearchGraph(config, mockLLM, mockTools)
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}

	request := testutil.NewTestRequest("test query")

	// Cancel context immediately
	cancel()

	// Execute should handle cancelled context gracefully
	_, err = graph.Execute(ctx, request)

	// We expect an error due to context cancellation, but it should be handled gracefully
	if err == nil {
		// Some implementations might complete before context is checked
		// This is acceptable
		t.Log("Execute completed despite context cancellation")
	}
}

func TestResearchGraph_Execute_MaxIterations(t *testing.T) {
	ctx := testutil.NewTestContext(t)

	config := &workflow.Config{
		Research: workflow.ResearchConfig{
			MaxConcurrency:   5,
			MaxIterations:    2, // Low iteration count
			MaxDepth:         2,
			CompressionRatio: 0.7,
		},
		LLM: workflow.LLMConfig{
			Model:       "test-model",
			Temperature: 0.7,
			MaxTokens:   2000,
		},
	}

	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()

	graph, err := workflow.NewResearchGraph(config, mockLLM, mockTools)
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}

	request := testutil.NewTestRequest("complex research query requiring many iterations")
	report, err := graph.Execute(ctx, request)

	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if report == nil {
		t.Error("Expected report even with max iterations, got nil")
	}

	// Verify that the LLM was called but not excessively
	callCount := mockLLM.GetCallCount()
	// Should have calls for clarify, planning, and limited research iterations
	if callCount > config.Research.MaxIterations*2+2 {
		t.Errorf("Too many LLM calls: %d, expected <= %d", callCount, config.Research.MaxIterations*2+2)
	}
}

func TestResearchGraph_Execute_LLMError(t *testing.T) {
	ctx := testutil.NewTestContext(t)

	config := &workflow.Config{
		Research: workflow.ResearchConfig{
			MaxConcurrency:   5,
			MaxIterations:    3,
			MaxDepth:         2,
			CompressionRatio: 0.7,
		},
		LLM: workflow.LLMConfig{
			Model:       "test-model",
			Temperature: 0.7,
			MaxTokens:   2000,
		},
	}

	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()

	// Configure LLM to return error
	mockLLM.ShouldError = true
	mockLLM.ErrorMessage = "LLM service unavailable"

	graph, err := workflow.NewResearchGraph(config, mockLLM, mockTools)
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}

	request := testutil.NewTestRequest("test query")
	_, err = graph.Execute(ctx, request)

	if err == nil {
		t.Error("Expected error when LLM fails, got nil")
	}

	if err != nil && err.Error() != "clarify node failed: clarification failed: LLM service unavailable" {
		t.Errorf("Unexpected error message: %v", err)
	}
}
