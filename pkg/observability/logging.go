package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// LogLevel represents the severity of a log message
type LogLevel string

const (
	LogLevelDebug LogLevel = "DEBUG"
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
)

// logOutput is the destination for log entries. It's a variable to allow redirection in tests.
var logOutput io.Writer = os.Stdout

// SetLogOutput sets the output destination for the structured logger.
func SetLogOutput(w io.Writer) {
	logOutput = w
}

// StructuredLogger provides structured logging with trace correlation
type StructuredLogger struct {
	output    io.Writer
	component string
}

// NewStructuredLogger creates a new structured logger
func NewStructuredLogger(component string) *StructuredLogger {
	return &StructuredLogger{
		output:    logOutput,
		component: component,
	}
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp  string                 `json:"timestamp"`
	Severity   LogLevel               `json:"severity"`
	Component  string                 `json:"component"`
	Message    string                 `json:"message"`
	TraceID    string                 `json:"trace_id,omitempty"`
	SpanID     string                 `json:"span_id,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// extractTraceInfo extracts trace and span IDs from context
func extractTraceInfo(ctx context.Context) (traceID, spanID string) {
	span := trace.SpanFromContext(ctx)
	if span != nil {
		spanCtx := span.SpanContext()
		if spanCtx.IsValid() {
			traceID = spanCtx.TraceID().String()
			spanID = spanCtx.SpanID().String()
		}
	}
	return traceID, spanID
}

// log writes a structured log entry
func (l *StructuredLogger) log(ctx context.Context, level LogLevel, message string, attrs map[string]interface{}) {
	traceID, spanID := extractTraceInfo(ctx)

	entry := LogEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Severity:   level,
		Component:  l.component,
		Message:    message,
		TraceID:    traceID,
		SpanID:     spanID,
		Attributes: attrs,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback to simple logging if marshaling fails
		fmt.Fprintf(l.output, "[%s] %s: %s\n", level, l.component, message)
		return
	}

	fmt.Fprintln(l.output, string(data))
}

// Debug logs a debug message
func (l *StructuredLogger) Debug(ctx context.Context, message string, attrs ...map[string]interface{}) {
	var attributes map[string]interface{}
	if len(attrs) > 0 {
		attributes = attrs[0]
	}
	l.log(ctx, LogLevelDebug, message, attributes)
}

// Info logs an info message
func (l *StructuredLogger) Info(ctx context.Context, message string, attrs ...map[string]interface{}) {
	var attributes map[string]interface{}
	if len(attrs) > 0 {
		attributes = attrs[0]
	}
	l.log(ctx, LogLevelInfo, message, attributes)
}

// Warn logs a warning message
func (l *StructuredLogger) Warn(ctx context.Context, message string, attrs ...map[string]interface{}) {
	var attributes map[string]interface{}
	if len(attrs) > 0 {
		attributes = attrs[0]
	}
	l.log(ctx, LogLevelWarn, message, attributes)
}

// Error logs an error message
func (l *StructuredLogger) Error(ctx context.Context, message string, err error, attrs ...map[string]interface{}) {
	var attributes map[string]interface{}
	if len(attrs) > 0 {
		attributes = attrs[0]
	} else {
		attributes = make(map[string]interface{})
	}

	if err != nil {
		attributes["error"] = err.Error()
	}

	l.log(ctx, LogLevelError, message, attributes)
}

// WithComponent creates a new logger with a different component name
func (l *StructuredLogger) WithComponent(component string) *StructuredLogger {
	return &StructuredLogger{
		output:    l.output,
		component: component,
	}
}

// Logger interface for dependency injection
type Logger interface {
	Debug(ctx context.Context, message string, attrs ...map[string]interface{})
	Info(ctx context.Context, message string, attrs ...map[string]interface{})
	Warn(ctx context.Context, message string, attrs ...map[string]interface{})
	Error(ctx context.Context, message string, err error, attrs ...map[string]interface{})
}
