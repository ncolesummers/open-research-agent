package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentedWorkflowNode wraps a workflow node with observability
func (t *Telemetry) InstrumentWorkflowNode(ctx context.Context, nodeName string, phase string, fn func(context.Context) error) error {
	// Start span for workflow node
	ctx, span := t.StartSpan(ctx, fmt.Sprintf("workflow.node.%s", nodeName),
		trace.WithAttributes(
			attribute.String("node.name", nodeName),
			attribute.String("phase", phase),
		),
	)
	defer span.End()

	// Record node start time
	startTime := time.Now()

	// Execute the node function
	err := fn(ctx)

	// Record duration and status
	duration := time.Since(startTime)
	status := "success"
	if err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("status", status),
		attribute.Float64("duration.seconds", duration.Seconds()),
	)

	return err
}

// InstrumentLLMCall wraps an LLM call with observability
func (t *Telemetry) InstrumentLLMCall(ctx context.Context, model string, fn func(context.Context) (promptTokens, completionTokens int, err error)) error {
	// Start span for LLM call
	ctx, span := t.StartSpan(ctx, "llm.chat",
		trace.WithAttributes(
			attribute.String("llm.model", model),
			attribute.String("llm.provider", "ollama"),
		),
	)
	defer span.End()

	// Record start time
	startTime := time.Now()

	// Execute the LLM call
	promptTokens, completionTokens, err := fn(ctx)

	// Record duration and status
	duration := time.Since(startTime)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
		// Set token usage attributes
		span.SetAttributes(
			attribute.Int("llm.prompt_tokens", promptTokens),
			attribute.Int("llm.completion_tokens", completionTokens),
			attribute.Int("llm.total_tokens", promptTokens+completionTokens),
		)
	}

	span.SetAttributes(
		attribute.Float64("duration.seconds", duration.Seconds()),
	)

	return err
}

// InstrumentToolExecution wraps a tool execution with observability
func (t *Telemetry) InstrumentToolExecution(ctx context.Context, toolName string, fn func(context.Context) error) error {
	// Start span for tool execution
	ctx, span := t.StartSpan(ctx, fmt.Sprintf("tool.%s", toolName),
		trace.WithAttributes(
			attribute.String("tool.name", toolName),
		),
	)
	defer span.End()

	// Record start time
	startTime := time.Now()

	// Execute the tool
	err := fn(ctx)

	// Record duration and status
	duration := time.Since(startTime)
	status := "success"
	if err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.SetAttributes(
		attribute.String("tool.status", status),
		attribute.Float64("tool.duration_seconds", duration.Seconds()),
	)

	return err
}

// StartResearchRequest starts a root span for a research request
func (t *Telemetry) StartResearchRequest(ctx context.Context, requestID, userID, query string) (context.Context, trace.Span) {
	ctx, span := t.StartSpan(ctx, "research.request",
		trace.WithAttributes(
			attribute.String("request.id", requestID),
			attribute.String("user.id", userID),
			attribute.Int("query.length", len(query)),
			attribute.String("model", "llama3.2"),
		),
	)

	// Add to baggage for propagation
	span.SetAttributes(
		attribute.String("request.id", requestID),
		attribute.String("research.type", determineResearchType(query)),
		attribute.String("complexity", estimateComplexity(query)),
	)

	return ctx, span
}

// Helper functions for classification
func determineResearchType(query string) string {
	// Simple classification logic - can be enhanced
	if len(query) < 50 {
		return "simple"
	} else if len(query) < 200 {
		return "moderate"
	}
	return "complex"
}

func estimateComplexity(query string) string {
	// Simple complexity estimation - can be enhanced
	if len(query) < 50 {
		return "low"
	} else if len(query) < 200 {
		return "medium"
	}
	return "high"
}
