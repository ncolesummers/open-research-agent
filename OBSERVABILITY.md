# Open Research Agent - Observability Architecture

## Overview

This document defines the observability strategy for Open Research Agent using OpenTelemetry (OTel) as the foundation. We adopt an Observability-Driven Development (ODD) approach where telemetry is not an afterthought but a core architectural component.

## Observability-Driven Development Principles

### 1. Design for Observability
- Every component emits structured telemetry
- Trace context propagates through all layers
- Metrics define SLIs before implementation
- Logs are structured and correlated with traces

### 2. Test Through Telemetry
- Unit tests verify telemetry emission
- Integration tests validate trace propagation
- Performance tests use metrics for assertions
- Debugging relies on distributed tracing

### 3. Deploy with Confidence
- Service Level Objectives (SLOs) defined upfront
- Alert on symptoms, not causes
- Progressive rollouts guided by metrics
- Incident response driven by traces

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Application Layer                         │
│  ┌──────────────────────────────────────────────────────┐   │
│  │            Instrumented Business Logic               │   │
│  │  • Traces for request flow                          │   │
│  │  • Metrics for performance/errors                   │   │
│  │  • Logs for detailed context                        │   │
│  │  • Baggage for research context                     │   │
│  └──────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────┤
│                 OpenTelemetry SDK Layer                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  Tracer  │  │  Meter   │  │  Logger  │  │ Propagator│   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
├─────────────────────────────────────────────────────────────┤
│                    Processors & Exporters                    │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  • Batch Processor (reduce overhead)                 │   │
│  │  • Resource Detection (auto-discover environment)    │   │
│  │  • Sampling (adaptive based on error rate)          │   │
│  │  • Multi-Protocol Export (OTLP/Jaeger/Prometheus)   │   │
│  └──────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────┤
│                    Telemetry Backends                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  Jaeger  │  │Prometheus│  │   Loki   │  │  Custom  │   │
│  │ (Traces) │  │(Metrics) │  │  (Logs)  │  │ Collector│   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Telemetry Schemas

### Research Request Trace

```
[Research Request Span]
├── [Clarification Span]
│   ├── [LLM Call Span]
│   └── [Response Parse Span]
├── [Planning Span]
│   ├── [Task Generation Span]
│   └── [Priority Assignment Span]
├── [Supervisor Span]
│   ├── [Task Distribution Span]
│   ├── [Researcher Span #1] (parallel)
│   │   ├── [Tool Selection Span]
│   │   ├── [Web Search Span]
│   │   ├── [LLM Analysis Span]
│   │   └── [Result Compression Span]
│   ├── [Researcher Span #2] (parallel)
│   │   └── ...
│   └── [Result Aggregation Span]
├── [Report Generation Span]
│   ├── [Content Synthesis Span]
│   └── [Formatting Span]
└── [Response Delivery Span]
```

### Key Metrics

```yaml
# Research Performance Metrics
research_request_duration_seconds:
  type: histogram
  description: Total time to complete research request
  labels: [status, complexity, model]
  buckets: [0.5, 1, 2, 5, 10, 30, 60, 120]

research_tasks_total:
  type: counter
  description: Number of research tasks created
  labels: [type, priority, status]

active_researchers:
  type: gauge
  description: Number of active researcher goroutines
  labels: [model]

# LLM Metrics
llm_request_duration_seconds:
  type: histogram
  description: LLM API call duration
  labels: [model, operation, provider]

llm_tokens_used_total:
  type: counter
  description: Total tokens consumed
  labels: [model, operation, type]

llm_errors_total:
  type: counter
  description: LLM API errors
  labels: [model, error_type, provider]

# Tool Metrics
tool_invocations_total:
  type: counter
  description: Tool usage count
  labels: [tool_name, success]

tool_duration_seconds:
  type: histogram
  description: Tool execution time
  labels: [tool_name]

# System Metrics
workflow_node_duration_seconds:
  type: histogram
  description: Duration of each workflow node
  labels: [node_name, status]

concurrent_requests:
  type: gauge
  description: Number of concurrent research requests

memory_usage_bytes:
  type: gauge
  description: Memory usage by component
  labels: [component]
```

### Structured Logging Schema

```json
{
  "timestamp": "2024-01-15T10:30:45.123Z",
  "severity": "INFO",
  "trace_id": "7b8a9c0d1e2f3a4b5c6d7e8f9a0b1c2d",
  "span_id": "1a2b3c4d5e6f7890",
  "service": "research-agent",
  "component": "supervisor",
  "message": "Research task completed",
  "attributes": {
    "task_id": "task_123",
    "duration_ms": 2456,
    "tokens_used": 1523,
    "tool_calls": 3,
    "model": "llama3.2",
    "user_id": "user_456",
    "research_topic": "quantum computing applications"
  }
}
```

## Implementation Status

### ✅ Completed Implementations (Phase 1)

#### Telemetry Foundation (`pkg/observability/telemetry.go`)
- OpenTelemetry SDK setup with OTLP export
- Prometheus metrics export
- Trace propagation with W3C TraceContext
- Configurable sampling (ratio-based)
- Resource detection with service metadata

#### Metrics Collection (`pkg/observability/metrics.go`)
- Pre-created metric instruments for performance
- Research request metrics (duration, count, active)
- Task lifecycle metrics (created, started, completed, failed)
- LLM metrics (requests, tokens, duration)
- Tool execution metrics

#### Workflow Instrumentation (`pkg/workflow/graph.go`)
- All workflow nodes wrapped with spans
- Automatic metric recording for each node
- Error tracking and span status
- LLM call instrumentation within nodes
- Task creation and completion metrics

#### LLM Client Instrumentation (`pkg/llm/instrumented.go`)
- Full span coverage for Chat, Stream, and Embed operations
- Token usage tracking (prompt, completion, total)
- Error recording and classification
- Request duration histograms
- Model and provider attributes

#### Structured Logging (`pkg/observability/logging.go`)
- JSON structured logging with trace correlation
- Automatic trace/span ID extraction
- Component-based logging
- Log levels (DEBUG, INFO, WARN, ERROR)

#### Instrumentation Helpers (`pkg/observability/instrumentation.go`)
- `InstrumentWorkflowNode` - Wraps workflow nodes with telemetry
- `InstrumentLLMCall` - Wraps LLM calls with observability
- `InstrumentToolExecution` - Wraps tool executions
- `StartResearchRequest` - Root span creation with baggage

### 🚧 Pending Implementations (Phase 2)

These will be covered in Phase 2 user stories:

#### Tool Instrumentation
- Web Search Tool spans and metrics
- Think Tool reasoning chain tracing
- Summarize Tool compression metrics

#### Worker Pool Observability
- Worker lifecycle spans
- Task queue depth metrics
- Worker health monitoring
- Concurrent execution tracing

#### Advanced Features
- Adaptive sampling based on error rates
- Custom resource detection for Ollama
- Baggage propagation for research context
- Cost tracking for token usage

## Implementation

### 1. Instrumentation Middleware (Example from Current Implementation)

```go
// pkg/observability/middleware.go

package observability

import (
    "context"
    "time"
    
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
    "go.opentelemetry.io/otel/codes"
)

type Telemetry struct {
    tracer  trace.Tracer
    meter   metric.Meter
    
    // Pre-created metrics for performance
    requestDuration   metric.Float64Histogram
    activeRequests    metric.Int64UpDownCounter
    tokensUsed        metric.Int64Counter
    errorCount        metric.Int64Counter
}

func NewTelemetry(serviceName string) (*Telemetry, error) {
    tracer := otel.Tracer(serviceName)
    meter := otel.Meter(serviceName)
    
    requestDuration, err := meter.Float64Histogram(
        "research_request_duration_seconds",
        metric.WithDescription("Duration of research requests"),
        metric.WithUnit("s"),
    )
    if err != nil {
        return nil, err
    }
    
    activeRequests, err := meter.Int64UpDownCounter(
        "active_research_requests",
        metric.WithDescription("Number of active research requests"),
    )
    if err != nil {
        return nil, err
    }
    
    tokensUsed, err := meter.Int64Counter(
        "llm_tokens_used_total",
        metric.WithDescription("Total LLM tokens consumed"),
    )
    if err != nil {
        return nil, err
    }
    
    errorCount, err := meter.Int64Counter(
        "research_errors_total",
        metric.WithDescription("Total number of errors"),
    )
    if err != nil {
        return nil, err
    }
    
    return &Telemetry{
        tracer:          tracer,
        meter:           meter,
        requestDuration: requestDuration,
        activeRequests:  activeRequests,
        tokensUsed:      tokensUsed,
        errorCount:      errorCount,
    }, nil
}

// InstrumentWorkflowNode wraps Eino nodes with telemetry
func (t *Telemetry) InstrumentWorkflowNode(
    nodeName string,
    nodeFunc func(context.Context, *GraphState) error,
) func(context.Context, *GraphState) error {
    
    return func(ctx context.Context, state *GraphState) error {
        // Start span
        ctx, span := t.tracer.Start(ctx, "workflow.node."+nodeName,
            trace.WithAttributes(
                attribute.String("node.name", nodeName),
                attribute.String("request.id", state.Request.ID),
                attribute.String("phase", string(state.CurrentPhase)),
            ),
        )
        defer span.End()
        
        // Record active requests
        t.activeRequests.Add(ctx, 1,
            metric.WithAttributes(
                attribute.String("node", nodeName),
            ),
        )
        defer t.activeRequests.Add(ctx, -1,
            metric.WithAttributes(
                attribute.String("node", nodeName),
            ),
        )
        
        // Execute node
        start := time.Now()
        err := nodeFunc(ctx, state)
        duration := time.Since(start).Seconds()
        
        // Record metrics
        status := "success"
        if err != nil {
            status = "error"
            span.RecordError(err)
            span.SetStatus(codes.Error, err.Error())
            t.errorCount.Add(ctx, 1,
                metric.WithAttributes(
                    attribute.String("node", nodeName),
                    attribute.String("error_type", classifyError(err)),
                ),
            )
        }
        
        t.requestDuration.Record(ctx, duration,
            metric.WithAttributes(
                attribute.String("node", nodeName),
                attribute.String("status", status),
            ),
        )
        
        // Add node-specific attributes
        span.SetAttributes(
            attribute.Float64("duration_seconds", duration),
            attribute.String("status", status),
            attribute.Int("iteration", state.Iterations),
        )
        
        return err
    }
}
```

### 2. LLM Client Instrumentation (Actual Implementation)

```go
// pkg/llm/instrumented.go

package llm

import (
    "context"
    "time"
    
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"
)

type InstrumentedOllamaClient struct {
    *OllamaClient
    tracer trace.Tracer
    meter  metric.Meter
}

func (c *InstrumentedOllamaClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
    // Start span with detailed attributes
    ctx, span := c.tracer.Start(ctx, "llm.chat",
        trace.WithAttributes(
            attribute.String("llm.model", c.model),
            attribute.String("llm.provider", "ollama"),
            attribute.Int("llm.max_tokens", req.MaxTokens),
            attribute.Float64("llm.temperature", req.Temperature),
            attribute.Int("llm.message_count", len(req.Messages)),
        ),
    )
    defer span.End()
    
    // Add request to span events
    span.AddEvent("request_sent",
        trace.WithAttributes(
            attribute.String("first_message_role", req.Messages[0].Role),
        ),
    )
    
    start := time.Now()
    resp, err := c.OllamaClient.Chat(ctx, req)
    duration := time.Since(start)
    
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        
        // Record error metric
        c.meter.RecordBatch(ctx,
            attribute.String("model", c.model),
            attribute.String("error_type", classifyLLMError(err)),
        )
        return nil, err
    }
    
    // Record success metrics and span attributes
    span.SetAttributes(
        attribute.Int("llm.prompt_tokens", resp.Usage.PromptTokens),
        attribute.Int("llm.completion_tokens", resp.Usage.CompletionTokens),
        attribute.Int("llm.total_tokens", resp.Usage.TotalTokens),
        attribute.Float64("llm.duration_seconds", duration.Seconds()),
    )
    
    // Record metrics
    c.recordMetrics(ctx, resp.Usage, duration)
    
    return resp, nil
}

func (c *InstrumentedOllamaClient) recordMetrics(ctx context.Context, usage Usage, duration time.Duration) {
    attrs := []attribute.KeyValue{
        attribute.String("model", c.model),
        attribute.String("provider", "ollama"),
    }
    
    // Token metrics
    c.meter.RecordBatch(ctx, attrs,
        c.tokensPrompt.Measurement(int64(usage.PromptTokens)),
        c.tokensCompletion.Measurement(int64(usage.CompletionTokens)),
        c.tokensTotal.Measurement(int64(usage.TotalTokens)),
    )
    
    // Performance metrics
    c.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
    
    // Cost tracking (if applicable)
    estimatedCost := calculateCost(c.model, usage.TotalTokens)
    c.costTotal.Add(ctx, estimatedCost, metric.WithAttributes(attrs...))
}
```

### 3. Tool Instrumentation (To Be Implemented in Phase 2)

```go
// pkg/tools/instrumented_registry.go (PHASE 2)

package tools

import (
    "context"
    "fmt"
    "time"
    
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"
)

type InstrumentedToolRegistry struct {
    *ToolRegistry
    tracer trace.Tracer
    meter  metric.Meter
}

func (r *InstrumentedToolRegistry) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
    // Create span for tool execution
    ctx, span := r.tracer.Start(ctx, fmt.Sprintf("tool.%s", toolName),
        trace.WithAttributes(
            attribute.String("tool.name", toolName),
            attribute.String("tool.args", fmt.Sprintf("%v", args)),
        ),
    )
    defer span.End()
    
    // Get tool
    tool, exists := r.tools[toolName]
    if !exists {
        err := fmt.Errorf("tool %s not found", toolName)
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return nil, err
    }
    
    // Record tool invocation
    r.toolInvocations.Add(ctx, 1,
        metric.WithAttributes(
            attribute.String("tool", toolName),
        ),
    )
    
    // Execute with timeout tracking
    start := time.Now()
    result, err := tool.Execute(ctx, args)
    duration := time.Since(start)
    
    // Record outcome
    status := "success"
    if err != nil {
        status = "error"
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }
    
    // Set span attributes
    span.SetAttributes(
        attribute.String("tool.status", status),
        attribute.Float64("tool.duration_seconds", duration.Seconds()),
    )
    
    // Record metrics
    r.toolDuration.Record(ctx, duration.Seconds(),
        metric.WithAttributes(
            attribute.String("tool", toolName),
            attribute.String("status", status),
        ),
    )
    
    return result, err
}
```

### 4. Baggage for Research Context (Partially Implemented)

```go
// pkg/observability/instrumentation.go - Current Implementation
func (t *Telemetry) StartResearchRequest(ctx context.Context, requestID, userID, query string) (context.Context, trace.Span) {
    ctx, span := t.StartSpan(ctx, "research.request",
        trace.WithAttributes(
            attribute.String("request.id", requestID),
            attribute.String("user.id", userID),
            attribute.Int("query.length", len(query)),
            attribute.String("model", "llama3.2"),
        ),
    )
    // Baggage propagation to be enhanced in Phase 2
    return ctx, span
}

// Full baggage implementation coming in Phase 2:
// pkg/observability/baggage.go (PHASE 2)

package observability

import (
    "context"
    
    "go.opentelemetry.io/otel/baggage"
)

// ResearchContext propagates through the entire request
type ResearchContext struct {
    RequestID    string
    UserID       string
    ResearchType string
    Complexity   string
    Model        string
}

func WithResearchContext(ctx context.Context, rc ResearchContext) context.Context {
    bag, _ := baggage.Parse(fmt.Sprintf(
        "request.id=%s,user.id=%s,research.type=%s,complexity=%s,model=%s",
        rc.RequestID, rc.UserID, rc.ResearchType, rc.Complexity, rc.Model,
    ))
    return baggage.ContextWithBaggage(ctx, bag)
}

func GetResearchContext(ctx context.Context) ResearchContext {
    bag := baggage.FromContext(ctx)
    return ResearchContext{
        RequestID:    bag.Member("request.id").Value(),
        UserID:       bag.Member("user.id").Value(),
        ResearchType: bag.Member("research.type").Value(),
        Complexity:   bag.Member("complexity").Value(),
        Model:        bag.Member("model").Value(),
    }
}
```

### 5. Adaptive Sampling (To Be Implemented in Phase 2)

```go
// pkg/observability/sampling.go (PHASE 2)

package observability

import (
    "go.opentelemetry.io/otel/sdk/trace"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type AdaptiveSampler struct {
    errorRate      float64
    normalRate     float64
    errorCounter   int64
    totalCounter   int64
}

func NewAdaptiveSampler(normalRate, errorRate float64) *AdaptiveSampler {
    return &AdaptiveSampler{
        normalRate: normalRate,
        errorRate:  errorRate,
    }
}

func (s *AdaptiveSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
    // Always sample if there's an error
    for _, attr := range parameters.Attributes {
        if attr.Key == "error" && attr.Value.AsBool() {
            return sdktrace.SamplingResult{
                Decision: sdktrace.RecordAndSample,
            }
        }
    }
    
    // Sample based on current error rate
    currentErrorRate := float64(s.errorCounter) / float64(s.totalCounter)
    
    samplingRate := s.normalRate
    if currentErrorRate > 0.01 { // If error rate > 1%
        samplingRate = s.errorRate // Increase sampling
    }
    
    // Probabilistic sampling
    if rand.Float64() < samplingRate {
        return sdktrace.SamplingResult{
            Decision: sdktrace.RecordAndSample,
        }
    }
    
    return sdktrace.SamplingResult{
        Decision: sdktrace.Drop,
    }
}

func (s *AdaptiveSampler) Description() string {
    return "AdaptiveSampler"
}
```

### 6. Custom Resource Detection (Partially Implemented)

```go
// pkg/observability/telemetry.go - Current Implementation
func (t *Telemetry) createResource() (*resource.Resource, error) {
    hostname, _ := os.Hostname()
    return resource.Merge(
        resource.Default(),
        resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName(t.config.ServiceName),
            semconv.ServiceVersion(t.config.ServiceVersion),
            semconv.DeploymentEnvironment(t.config.Environment),
            attribute.String("host.name", hostname),
            attribute.String("service.namespace", "research"),
        ),
    )
}

// Enhanced resource detection coming in Phase 2:
// pkg/observability/resource.go (PHASE 2)

package observability

import (
    "context"
    "os"
    
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

func NewResource(ctx context.Context) (*resource.Resource, error) {
    return resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName("open-research-agent"),
            semconv.ServiceVersion(getVersion()),
            semconv.ServiceInstanceID(getInstanceID()),
            semconv.DeploymentEnvironment(getEnvironment()),
        ),
        resource.WithHost(),
        resource.WithContainer(),
        resource.WithOS(),
        resource.WithProcess(),
        resource.WithTelemetrySDK(),
        resource.WithAttributes(
            attribute.String("ollama.model", os.Getenv("OLLAMA_MODEL")),
            attribute.String("ollama.url", os.Getenv("OLLAMA_URL")),
        ),
    )
}
```

### 7. Observability-Driven Tests

```go
// pkg/workflow/supervisor_test.go

package workflow

import (
    "context"
    "testing"
    
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
    "go.opentelemetry.io/otel/sdk/metric/metrictest"
)

func TestSupervisorNodeObservability(t *testing.T) {
    // Create test telemetry recorders
    spanRecorder := tracetest.NewSpanRecorder()
    metricReader := metrictest.NewReader()
    
    // Setup test telemetry
    tel := setupTestTelemetry(spanRecorder, metricReader)
    
    // Create instrumented node
    node := tel.InstrumentWorkflowNode("supervisor", SupervisorNode)
    
    // Execute node
    ctx := context.Background()
    state := &GraphState{
        Request: ResearchRequest{
            ID:    "test-123",
            Query: "test query",
        },
    }
    
    err := node(ctx, state)
    require.NoError(t, err)
    
    // Verify span was created with correct attributes
    spans := spanRecorder.Ended()
    require.Len(t, spans, 1)
    
    span := spans[0]
    assert.Equal(t, "workflow.node.supervisor", span.Name())
    assert.Equal(t, "test-123", span.Attributes()["request.id"])
    assert.Equal(t, "supervisor", span.Attributes()["node.name"])
    
    // Verify metrics were recorded
    metrics := metricReader.Collect()
    
    requestDuration := findMetric(metrics, "research_request_duration_seconds")
    assert.NotNil(t, requestDuration)
    assert.Equal(t, "supervisor", requestDuration.Attributes["node"])
    assert.Equal(t, "success", requestDuration.Attributes["status"])
    
    activeRequests := findMetric(metrics, "active_research_requests")
    assert.NotNil(t, activeRequests)
    assert.Equal(t, int64(0), activeRequests.Value) // Should be 0 after completion
}

func TestLLMClientObservability(t *testing.T) {
    // Create mock server with telemetry verification
    mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify trace context propagation
        traceParent := r.Header.Get("traceparent")
        assert.NotEmpty(t, traceParent)
        
        // Return mock response
        json.NewEncoder(w).Encode(ChatResponse{
            Content: "test response",
            Usage: Usage{
                PromptTokens:     10,
                CompletionTokens: 20,
                TotalTokens:      30,
            },
        })
    }))
    defer mockServer.Close()
    
    // Create instrumented client
    client := NewInstrumentedOllamaClient(mockServer.URL, "test-model")
    
    // Execute chat
    ctx := context.Background()
    resp, err := client.Chat(ctx, &ChatRequest{
        Messages: []Message{{Role: "user", Content: "test"}},
    })
    
    require.NoError(t, err)
    assert.Equal(t, "test response", resp.Content)
    
    // Verify telemetry
    verifyLLMTelemetry(t, spanRecorder, metricReader)
}
```

## Deployment Configuration

### Docker Compose with Full Observability Stack

```yaml
# docker-compose.observability.yml

version: '3.8'

services:
  ora:
    build: .
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
      - OTEL_SERVICE_NAME=open-research-agent
      - OTEL_TRACES_EXPORTER=otlp
      - OTEL_METRICS_EXPORTER=otlp
      - OTEL_LOGS_EXPORTER=otlp
    depends_on:
      - otel-collector
      - ollama

  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    command: ["--config=/etc/otel-collector-config.yaml"]
    volumes:
      - ./configs/otel-collector.yaml:/etc/otel-collector-config.yaml
    ports:
      - "4317:4317"   # OTLP gRPC
      - "4318:4318"   # OTLP HTTP
      - "8888:8888"   # Prometheus metrics

  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686" # Jaeger UI
      - "14250:14250" # gRPC

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./configs/prometheus.yml:/etc/prometheus/prometheus.yml
    ports:
      - "9090:9090"

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - ./configs/grafana/dashboards:/etc/grafana/provisioning/dashboards
      - ./configs/grafana/datasources:/etc/grafana/provisioning/datasources

  loki:
    image: grafana/loki:latest
    ports:
      - "3100:3100"
    command: -config.file=/etc/loki/local-config.yaml
```

### OpenTelemetry Collector Configuration

```yaml
# configs/otel-collector.yaml

receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
    timeout: 1s
    send_batch_size: 1024
  
  memory_limiter:
    check_interval: 1s
    limit_mib: 512
    spike_limit_mib: 128
  
  resource:
    attributes:
      - key: environment
        value: ${ENVIRONMENT}
        action: upsert
  
  tail_sampling:
    policies:
      - name: errors-policy
        type: status_code
        status_code: {status_codes: [ERROR]}
      - name: latency-policy
        type: latency
        latency: {threshold_ms: 5000}
      - name: probabilistic-policy
        type: probabilistic
        probabilistic: {sampling_percentage: 10}

exporters:
  jaeger:
    endpoint: jaeger:14250
    tls:
      insecure: true
  
  prometheus:
    endpoint: "0.0.0.0:8888"
    namespace: ora
    const_labels:
      environment: ${ENVIRONMENT}
  
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
  
  debug:
    verbosity: detailed

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch, resource, tail_sampling]
      exporters: [jaeger, debug]
    
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch, resource]
      exporters: [prometheus]
    
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch, resource]
      exporters: [loki, debug]
```

## Grafana Dashboard

```json
{
  "dashboard": {
    "title": "Open Research Agent",
    "panels": [
      {
        "title": "Request Rate",
        "targets": [
          {
            "expr": "rate(ora_research_request_total[5m])"
          }
        ]
      },
      {
        "title": "Request Duration (p50, p95, p99)",
        "targets": [
          {
            "expr": "histogram_quantile(0.5, rate(ora_research_request_duration_seconds_bucket[5m]))",
            "legendFormat": "p50"
          },
          {
            "expr": "histogram_quantile(0.95, rate(ora_research_request_duration_seconds_bucket[5m]))",
            "legendFormat": "p95"
          },
          {
            "expr": "histogram_quantile(0.99, rate(ora_research_request_duration_seconds_bucket[5m]))",
            "legendFormat": "p99"
          }
        ]
      },
      {
        "title": "Token Usage",
        "targets": [
          {
            "expr": "rate(ora_llm_tokens_used_total[5m])"
          }
        ]
      },
      {
        "title": "Error Rate",
        "targets": [
          {
            "expr": "rate(ora_research_errors_total[5m])"
          }
        ]
      },
      {
        "title": "Active Researchers",
        "targets": [
          {
            "expr": "ora_active_researchers"
          }
        ]
      },
      {
        "title": "Tool Usage",
        "targets": [
          {
            "expr": "rate(ora_tool_invocations_total[5m]) by (tool_name)"
          }
        ]
      }
    ]
  }
}
```

## SLI/SLO Definitions

```yaml
# configs/slo.yaml

slos:
  - name: research_availability
    description: Research service availability
    sli:
      error_ratio:
        selector: 'service="open-research-agent"'
        error_query: 'ora_research_errors_total'
        total_query: 'ora_research_request_total'
    target: 99.9
    window: 30d

  - name: research_latency
    description: Research request latency
    sli:
      latency:
        selector: 'service="open-research-agent"'
        histogram: 'ora_research_request_duration_seconds'
        percentile: 95
    target: 10 # 95% of requests under 10 seconds
    window: 30d

  - name: llm_token_budget
    description: LLM token usage within budget
    sli:
      threshold:
        selector: 'service="open-research-agent"'
        metric: 'rate(ora_llm_tokens_used_total[1h])'
    target: 100000 # tokens per hour
    window: 24h
```

## Alerts

```yaml
# configs/alerts.yaml

groups:
  - name: ora_alerts
    rules:
      - alert: HighErrorRate
        expr: rate(ora_research_errors_total[5m]) > 0.01
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High error rate detected
          description: "Error rate is {{ $value }} errors per second"

      - alert: HighLatency
        expr: histogram_quantile(0.95, rate(ora_research_request_duration_seconds_bucket[5m])) > 30
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: High request latency
          description: "95th percentile latency is {{ $value }} seconds"

      - alert: LLMTokenBudgetExceeded
        expr: rate(ora_llm_tokens_used_total[1h]) > 100000
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: LLM token budget exceeded
          description: "Token usage is {{ $value }} tokens per hour"

      - alert: OllamaDown
        expr: up{job="ollama"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: Ollama service is down
          description: "Ollama has been down for more than 1 minute"
```

## Benefits of This Observability Architecture

1. **Complete Visibility**: Every component is instrumented from day one
2. **Debugging Power**: Distributed tracing shows exact request flow
3. **Performance Insights**: Metrics identify bottlenecks immediately
4. **Cost Control**: Token usage tracking prevents budget overruns
5. **Proactive Monitoring**: SLOs and alerts catch issues before users notice
6. **Development Velocity**: Telemetry-driven testing speeds up debugging
7. **Production Confidence**: Know exactly what's happening in production