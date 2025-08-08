package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestTimeout provides a standard timeout for test contexts
const TestTimeout = 5 * time.Second

// NewTestContext creates a context with standard test timeout
func NewTestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), TestTimeout)
	t.Cleanup(cancel)
	return ctx
}

// NewTestRequest creates a test research request
func NewTestRequest(query string) *domain.ResearchRequest {
	return &domain.ResearchRequest{
		ID:        "test-req-1",
		Query:     query,
		Context:   "test context",
		MaxDepth:  2,
		Timestamp: time.Now(),
	}
}

// NewTestTask creates a test research task
func NewTestTask(topic string) *domain.ResearchTask {
	return &domain.ResearchTask{
		ID:       "test-task-1",
		Topic:    topic,
		Status:   domain.TaskStatusPending,
		Priority: 1,
	}
}

// NewTestResult creates a test research result
func NewTestResult(taskID, content string) *domain.ResearchResult {
	return &domain.ResearchResult{
		ID:         "test-result-1",
		TaskID:     taskID,
		Source:     "test-source",
		Content:    content,
		Summary:    "Test summary",
		Confidence: 0.85,
		Timestamp:  time.Now(),
	}
}

// AssertEqual checks if two values are equal
func AssertEqual(t *testing.T, expected, actual interface{}, msg string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// AssertNoError checks if error is nil
func AssertNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", msg, err)
	}
}

// AssertError checks if error is not nil
func AssertError(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected error but got nil", msg)
	}
}

// SetupTestTelemetry creates test telemetry with span recorder and metric reader
func SetupTestTelemetry(spanRecorder *tracetest.SpanRecorder, metricReader metric.Reader) *observability.Telemetry {
	// Create trace provider with span recorder
	tracerProvider := trace.NewTracerProvider(
		trace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)
	
	// Create meter provider with metric reader
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metricReader),
	)
	otel.SetMeterProvider(meterProvider)
	
	// Create telemetry config
	config := &observability.TelemetryConfig{
		ServiceName:    "test-service",
		ServiceVersion: "test",
		Environment:    "test",
		EnableTracing:  true,
		EnableMetrics:  true,
		EnableLogging:  true,
		SamplingRate:   1.0,
	}
	
	// Create telemetry
	telemetry, _ := observability.NewTelemetry(config)
	return telemetry
}
