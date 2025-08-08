package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentedLLMClient wraps an LLM client with observability
type InstrumentedLLMClient struct {
	client    domain.LLMClient
	telemetry *observability.Telemetry
	metrics   *observability.Metrics
	model     string
}

// NewInstrumentedLLMClient creates a new instrumented LLM client
func NewInstrumentedLLMClient(client domain.LLMClient, telemetry *observability.Telemetry, model string) (*InstrumentedLLMClient, error) {
	if client == nil {
		return nil, fmt.Errorf("client is required")
	}
	if telemetry == nil {
		return nil, fmt.Errorf("telemetry is required")
	}

	metrics, err := observability.NewMetrics(telemetry.Meter())
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	return &InstrumentedLLMClient{
		client:    client,
		telemetry: telemetry,
		metrics:   metrics,
		model:     model,
	}, nil
}

// Chat performs an instrumented chat completion
func (c *InstrumentedLLMClient) Chat(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
	// Start span
	ctx, span := c.telemetry.StartSpan(ctx, "llm.chat",
		trace.WithAttributes(
			attribute.String("llm.model", c.model),
			attribute.String("llm.provider", "ollama"),
			attribute.Float64("llm.temperature", opts.Temperature),
			attribute.Int("llm.max_tokens", opts.MaxTokens),
			attribute.Int("llm.message_count", len(messages)),
		),
	)
	defer span.End()

	// Record start time for metrics
	startTime := time.Now()

	// Execute the actual LLM call
	response, err := c.client.Chat(ctx, messages, opts)

	// Record duration
	duration := time.Since(startTime)

	if err != nil {
		// Record error in span
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// Record success and token usage
	span.SetStatus(codes.Ok, "")
	span.SetAttributes(
		attribute.Int("llm.prompt_tokens", response.Usage.PromptTokens),
		attribute.Int("llm.completion_tokens", response.Usage.CompletionTokens),
		attribute.Int("llm.total_tokens", response.Usage.TotalTokens),
		attribute.String("llm.finish_reason", response.FinishReason),
	)

	// Record metrics
	c.metrics.RecordLLMRequest(ctx, c.model,
		int64(response.Usage.PromptTokens),
		int64(response.Usage.CompletionTokens),
		duration)

	return response, nil
}

// Stream performs an instrumented streaming chat completion
func (c *InstrumentedLLMClient) Stream(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (<-chan domain.ChatStreamResponse, error) {
	// Start span
	ctx, span := c.telemetry.StartSpan(ctx, "llm.stream",
		trace.WithAttributes(
			attribute.String("llm.model", c.model),
			attribute.String("llm.provider", "ollama"),
			attribute.Float64("llm.temperature", opts.Temperature),
			attribute.Int("llm.message_count", len(messages)),
		),
	)

	// Record start time
	startTime := time.Now()

	// Execute the actual stream call
	stream, err := c.client.Stream(ctx, messages, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}

	// Create wrapped stream channel
	wrappedStream := make(chan domain.ChatStreamResponse)

	// Start goroutine to wrap the stream
	go func() {
		defer close(wrappedStream)
		defer span.End()

		var totalPromptTokens, totalCompletionTokens int

		for response := range stream {
			// Forward the response
			wrappedStream <- response

			// Track token usage if available
			if response.Usage != nil {
				totalPromptTokens += response.Usage.PromptTokens
				totalCompletionTokens += response.Usage.CompletionTokens
			}

			// Check for errors
			if response.Error != nil {
				span.RecordError(response.Error)
				span.SetStatus(codes.Error, response.Error.Error())
				return
			}

			// Check if done
			if response.Done {
				span.SetStatus(codes.Ok, "")
				span.SetAttributes(
					attribute.Int("llm.prompt_tokens", totalPromptTokens),
					attribute.Int("llm.completion_tokens", totalCompletionTokens),
					attribute.Int("llm.total_tokens", totalPromptTokens+totalCompletionTokens),
				)

				// Record metrics
				duration := time.Since(startTime)
				c.metrics.RecordLLMRequest(ctx, c.model,
					int64(totalPromptTokens),
					int64(totalCompletionTokens),
					duration)
			}
		}
	}()

	return wrappedStream, nil
}

// Embed performs an instrumented embedding generation
func (c *InstrumentedLLMClient) Embed(ctx context.Context, text string) ([]float64, error) {
	// Start span
	ctx, span := c.telemetry.StartSpan(ctx, "llm.embed",
		trace.WithAttributes(
			attribute.String("llm.model", c.model),
			attribute.String("llm.provider", "ollama"),
			attribute.Int("llm.input_length", len(text)),
		),
	)
	defer span.End()

	// Execute the actual embed call
	embeddings, err := c.client.Embed(ctx, text)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	span.SetAttributes(
		attribute.Int("llm.embedding_dimensions", len(embeddings)),
	)

	return embeddings, nil
}
