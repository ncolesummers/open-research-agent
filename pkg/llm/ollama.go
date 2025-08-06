package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
)

// OllamaClient implements the LLMClient interface for Ollama
type OllamaClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
	options    OllamaOptions
}

// OllamaOptions configures the Ollama client
type OllamaOptions struct {
	Temperature  float64       `json:"temperature"`
	MaxTokens    int           `json:"max_tokens"`
	TopP         float64       `json:"top_p"`
	TopK         int           `json:"top_k"`
	SystemPrompt string        `json:"system_prompt"`
	Timeout      time.Duration `json:"timeout"`
}

// OllamaRequest represents a request to the Ollama API
type OllamaRequest struct {
	Model    string                 `json:"model"`
	Messages []OllamaMessage        `json:"messages"`
	Options  map[string]interface{} `json:"options,omitempty"`
	Stream   bool                   `json:"stream"`
}

// OllamaMessage represents a message in the Ollama format
type OllamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OllamaResponse represents a response from the Ollama API
type OllamaResponse struct {
	Message         OllamaMessage `json:"message"`
	Done            bool          `json:"done"`
	TotalDuration   int64         `json:"total_duration"`
	LoadDuration    int64         `json:"load_duration"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
	EvalDuration    int64         `json:"eval_duration"`
}

// OllamaStreamResponse represents a streaming response chunk
type OllamaStreamResponse struct {
	Message OllamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(baseURL, model string, options *OllamaOptions) *OllamaClient {
	if options == nil {
		options = &OllamaOptions{
			Temperature: 0.7,
			MaxTokens:   4096,
			TopP:        0.9,
			Timeout:     2 * time.Minute,
		}
	}

	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: options.Timeout,
		},
		options: *options,
	}
}

// Chat performs a chat completion
func (c *OllamaClient) Chat(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
	// Convert domain messages to Ollama format
	ollamaMessages := c.convertMessages(messages)

	// Build request
	req := OllamaRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Options:  c.buildOptions(opts),
		Stream:   false,
	}

	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("%s/api/chat", c.baseURL),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to domain response
	response := &domain.ChatResponse{
		Content: ollamaResp.Message.Content,
		Usage: domain.TokenUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
		FinishReason: c.determineFinishReason(ollamaResp),
	}

	return response, nil
}

// Stream performs a streaming chat completion
func (c *OllamaClient) Stream(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (<-chan domain.ChatStreamResponse, error) {
	stream := make(chan domain.ChatStreamResponse)

	go func() {
		defer close(stream)

		// Convert messages
		ollamaMessages := c.convertMessages(messages)

		// Build request
		req := OllamaRequest{
			Model:    c.model,
			Messages: ollamaMessages,
			Options:  c.buildOptions(opts),
			Stream:   true,
		}

		// Marshal request
		body, err := json.Marshal(req)
		if err != nil {
			stream <- domain.ChatStreamResponse{
				Error: fmt.Errorf("failed to marshal request: %w", err),
				Done:  true,
			}
			return
		}

		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(
			ctx,
			"POST",
			fmt.Sprintf("%s/api/chat", c.baseURL),
			bytes.NewReader(body),
		)
		if err != nil {
			stream <- domain.ChatStreamResponse{
				Error: fmt.Errorf("failed to create request: %w", err),
				Done:  true,
			}
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")

		// Send request
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			stream <- domain.ChatStreamResponse{
				Error: fmt.Errorf("request failed: %w", err),
				Done:  true,
			}
			return
		}
		defer resp.Body.Close()

		// Check status code
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			stream <- domain.ChatStreamResponse{
				Error: fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body)),
				Done:  true,
			}
			return
		}

		// Read streaming response
		decoder := json.NewDecoder(resp.Body)
		for {
			var chunk OllamaStreamResponse
			if err := decoder.Decode(&chunk); err != nil {
				if err == io.EOF {
					break
				}
				stream <- domain.ChatStreamResponse{
					Error: fmt.Errorf("failed to decode chunk: %w", err),
					Done:  true,
				}
				return
			}

			stream <- domain.ChatStreamResponse{
				Content: chunk.Message.Content,
				Done:    chunk.Done,
			}

			if chunk.Done {
				break
			}
		}
	}()

	return stream, nil
}

// Embed generates embeddings for text
func (c *OllamaClient) Embed(ctx context.Context, text string) ([]float64, error) {
	// Build request
	reqBody := map[string]interface{}{
		"model":  c.model,
		"prompt": text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("%s/api/embeddings", c.baseURL),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var embedResp struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return embedResp.Embedding, nil
}

// Helper methods

func (c *OllamaClient) convertMessages(messages []domain.Message) []OllamaMessage {
	ollamaMessages := make([]OllamaMessage, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = OllamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}
	return ollamaMessages
}

func (c *OllamaClient) buildOptions(opts domain.ChatOptions) map[string]interface{} {
	options := make(map[string]interface{})

	// Use provided options or fall back to defaults
	if opts.Temperature > 0 {
		options["temperature"] = opts.Temperature
	} else {
		options["temperature"] = c.options.Temperature
	}

	if opts.MaxTokens > 0 {
		options["num_predict"] = opts.MaxTokens
	} else {
		options["num_predict"] = c.options.MaxTokens
	}

	if opts.TopP > 0 {
		options["top_p"] = opts.TopP
	} else if c.options.TopP > 0 {
		options["top_p"] = c.options.TopP
	}

	if opts.TopK > 0 {
		options["top_k"] = opts.TopK
	} else if c.options.TopK > 0 {
		options["top_k"] = c.options.TopK
	}

	if len(opts.Stop) > 0 {
		options["stop"] = opts.Stop
	}

	return options
}

func (c *OllamaClient) determineFinishReason(resp OllamaResponse) string {
	if resp.Done {
		return "stop"
	}
	// Ollama doesn't provide detailed finish reasons, so we default to "stop"
	return "stop"
}

// CheckHealth verifies the Ollama service is accessible
func (c *OllamaClient) CheckHealth(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"GET",
		fmt.Sprintf("%s/api/tags", c.baseURL),
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama service unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// ListModels returns available models from Ollama
func (c *OllamaClient) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"GET",
		fmt.Sprintf("%s/api/tags", c.baseURL),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	var modelsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]string, len(modelsResp.Models))
	for i, model := range modelsResp.Models {
		models[i] = model.Name
	}

	return models, nil
}
