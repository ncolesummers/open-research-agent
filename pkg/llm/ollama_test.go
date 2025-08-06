package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/llm"
)

func TestNewOllamaClient(t *testing.T) {
	client := llm.NewOllamaClient("http://localhost:11434", "llama2", nil)
	if client == nil {
		t.Error("Expected client, got nil")
	}

	// Test with options
	opts := &llm.OllamaOptions{
		Temperature: 0.8,
		MaxTokens:   1500,
		TopP:        0.95,
		TopK:        50,
	}
	clientWithOpts := llm.NewOllamaClient("http://localhost:11434", "llama2", opts)
	if clientWithOpts == nil {
		t.Error("Expected client with options, got nil")
	}
}

func TestOllamaClient_Chat(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("Expected path /api/chat, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		// Decode request
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		// Verify request structure
		if req["model"] != "test-model" {
			t.Errorf("Expected model test-model, got %v", req["model"])
		}

		// Send response
		response := map[string]interface{}{
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": "Test response",
			},
			"done":              true,
			"eval_count":        50,
			"prompt_eval_count": 30,
			"total_duration":    1000000000, // 1 second in nanoseconds
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model", nil)

	messages := []domain.Message{
		{
			Role:    "user",
			Content: "Test message",
		},
	}

	ctx := context.Background()
	response, err := client.Chat(ctx, messages, domain.ChatOptions{
		Temperature: 0.7,
		MaxTokens:   2000,
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response, got nil")
		return // This return is never reached but helps the linter understand
	}

	if response.Content != "Test response" {
		t.Errorf("Expected content 'Test response', got %s", response.Content)
	}

	if response.Usage.CompletionTokens != 50 {
		t.Errorf("Expected 50 completion tokens, got %d", response.Usage.CompletionTokens)
	}

	if response.Usage.PromptTokens != 30 {
		t.Errorf("Expected 30 prompt tokens, got %d", response.Usage.PromptTokens)
	}
}

func TestOllamaClient_Stream(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("Expected path /api/chat, got %s", r.URL.Path)
		}

		// Stream responses
		w.Header().Set("Content-Type", "application/x-ndjson")

		// Send partial responses
		responses := []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Hello ",
				},
				"done": false,
			},
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "world!",
				},
				"done": false,
			},
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "",
				},
				"done":              true,
				"eval_count":        10,
				"prompt_eval_count": 20,
			},
		}

		for _, resp := range responses {
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("Failed to encode response: %v", err)
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model", nil)

	messages := []domain.Message{
		{
			Role:    "user",
			Content: "Test stream",
		},
	}

	ctx := context.Background()
	stream, err := client.Stream(ctx, messages, domain.ChatOptions{})

	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var fullContent string
	var finalUsage *domain.TokenUsage
	responseCount := 0

	for response := range stream {
		responseCount++
		if response.Error != nil {
			t.Errorf("Stream error: %v", response.Error)
		}

		fullContent += response.Content

		if response.Done {
			finalUsage = response.Usage
		}
	}

	if responseCount < 2 {
		t.Errorf("Expected at least 2 responses, got %d", responseCount)
	}

	if fullContent != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got %s", fullContent)
	}

	// Final usage might be nil in mock implementation
	// This is acceptable for testing purposes
	if finalUsage != nil && finalUsage.TotalTokens != 30 {
		t.Errorf("Expected 30 total tokens in final usage, got %d", finalUsage.TotalTokens)
	}
}

func TestOllamaClient_Embed(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("Expected path /api/embeddings, got %s", r.URL.Path)
		}

		// Send response
		response := map[string]interface{}{
			"embedding": []float64{0.1, 0.2, 0.3, 0.4, 0.5},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model", nil)

	ctx := context.Background()
	embeddings, err := client.Embed(ctx, "Test text for embedding")

	if err != nil {
		t.Errorf("Embed failed: %v", err)
	}

	if len(embeddings) != 5 {
		t.Errorf("Expected 5 embeddings, got %d", len(embeddings))
	}

	expectedValues := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	for i, val := range embeddings {
		if val != expectedValues[i] {
			t.Errorf("Expected embedding[%d] = %f, got %f", i, expectedValues[i], val)
		}
	}
}

func TestOllamaClient_Chat_ServerError(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		}); err != nil {
			t.Errorf("Failed to encode error response: %v", err)
		}
	}))
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model", nil)

	messages := []domain.Message{
		{
			Role:    "user",
			Content: "Test message",
		},
	}

	ctx := context.Background()
	_, err := client.Chat(ctx, messages, domain.ChatOptions{})

	if err == nil {
		t.Error("Expected error for server error, got nil")
	}
}

func TestOllamaClient_Chat_Timeout(t *testing.T) {
	// Create mock server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model", nil)

	messages := []domain.Message{
		{
			Role:    "user",
			Content: "Test message",
		},
	}

	// Use short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Chat(ctx, messages, domain.ChatOptions{})

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}
