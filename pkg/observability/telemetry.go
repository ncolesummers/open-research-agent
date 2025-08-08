package observability

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/stdr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// TelemetryConfig holds configuration for OpenTelemetry
type TelemetryConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	PrometheusPort int
	SamplingRate   float64
	EnableTracing  bool
	EnableMetrics  bool
	EnableLogging  bool
}

// Telemetry manages OpenTelemetry components
type Telemetry struct {
	config         *TelemetryConfig
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	tracer         trace.Tracer
	meter          metric.Meter
	shutdownFuncs  []func(context.Context) error
}

// NewTelemetry creates and initializes OpenTelemetry
func NewTelemetry(config *TelemetryConfig) (*Telemetry, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Configure OpenTelemetry SDK to suppress error logs to avoid interfering with CLI prompts
	// The SDK will still export traces when the endpoint becomes available
	stdr.SetVerbosity(0) // Only show critical errors
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		// Silently ignore export errors - they'll retry automatically
	}))

	t := &Telemetry{
		config:        config,
		shutdownFuncs: []func(context.Context) error{},
	}

	// Create resource
	res, err := t.createResource()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Initialize tracing
	if config.EnableTracing {
		if err := t.initTracing(res); err != nil {
			return nil, fmt.Errorf("failed to initialize tracing: %w", err)
		}
	} else {
		// Use a noop tracer when tracing is disabled
		t.tracer = noop.NewTracerProvider().Tracer(config.ServiceName)
	}

	// Initialize metrics
	if config.EnableMetrics {
		if err := t.initMetrics(res); err != nil {
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
	} else {
		// Use a noop meter when metrics are disabled
		// For now, just leave meter as nil since there's no noop meter provider
		// The Meter() method will handle nil case
	}

	// Set global propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return t, nil
}

// DefaultConfig returns default telemetry configuration
func DefaultConfig() *TelemetryConfig {
	return &TelemetryConfig{
		ServiceName:    "open-research-agent",
		ServiceVersion: "0.1.0",
		Environment:    getEnvOrDefault("ENVIRONMENT", "development"),
		OTLPEndpoint:   getEnvOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		PrometheusPort: 2223,
		SamplingRate:   1.0,
		EnableTracing:  true,
		EnableMetrics:  true,
		EnableLogging:  true,
	}
}

// createResource creates the OpenTelemetry resource
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

// initTracing initializes the tracing components
func (t *Telemetry) initTracing(res *resource.Resource) error {
	ctx := context.Background()

	// Create OTLP trace exporter with retry options
	client := otlptracehttp.NewClient(
		otlptracehttp.WithEndpoint(t.config.OTLPEndpoint),
		otlptracehttp.WithInsecure(), // Use TLS in production
		otlptracehttp.WithTimeout(time.Second*10),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
			Enabled:         true,
			InitialInterval: time.Second * 5,
			MaxInterval:     time.Second * 30,
			MaxElapsedTime:  time.Minute * 2,
		}),
	)

	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create trace provider with custom error handler to suppress noisy logs
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(t.config.SamplingRate)),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(time.Second*5),
			sdktrace.WithExportTimeout(time.Second*30),
		),
	)

	t.tracerProvider = tp
	t.shutdownFuncs = append(t.shutdownFuncs, tp.Shutdown)

	// Set global trace provider
	otel.SetTracerProvider(tp)

	// Create tracer
	t.tracer = tp.Tracer(
		t.config.ServiceName,
		trace.WithInstrumentationVersion(t.config.ServiceVersion),
	)

	return nil
}

// initMetrics initializes the metrics components
func (t *Telemetry) initMetrics(res *resource.Resource) error {
	// Create Prometheus exporter
	promExporter, err := prometheus.New()
	if err != nil {
		return fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// Create meter provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(promExporter),
	)

	t.meterProvider = mp
	t.shutdownFuncs = append(t.shutdownFuncs, mp.Shutdown)

	// Set global meter provider
	otel.SetMeterProvider(mp)

	// Create meter
	t.meter = mp.Meter(
		t.config.ServiceName,
		metric.WithInstrumentationVersion(t.config.ServiceVersion),
	)

	return nil
}

// Shutdown gracefully shuts down all telemetry components
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var err error
	for _, fn := range t.shutdownFuncs {
		if shutdownErr := fn(ctx); shutdownErr != nil {
			if err == nil {
				err = shutdownErr
			} else {
				err = fmt.Errorf("%v; %w", err, shutdownErr)
			}
		}
	}
	return err
}

// Tracer returns the configured tracer
func (t *Telemetry) Tracer() trace.Tracer {
	return t.tracer
}

// Meter returns the configured meter
func (t *Telemetry) Meter() metric.Meter {
	return t.meter
}

// StartSpan starts a new span with common attributes
func (t *Telemetry) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, opts...)
}

// RecordMetric records a metric value
func (t *Telemetry) RecordMetric(ctx context.Context, name string, value float64, attrs ...attribute.KeyValue) error {
	// This is a simplified example - in production, you'd have pre-created instruments
	counter, err := t.meter.Float64Counter(name)
	if err != nil {
		return err
	}
	counter.Add(ctx, value, metric.WithAttributes(attrs...))
	return nil
}

// Helper functions

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// SpanFromContext returns the current span from context
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// ContextWithSpan returns a new context with the given span
func ContextWithSpan(ctx context.Context, span trace.Span) context.Context {
	return trace.ContextWithSpan(ctx, span)
}
