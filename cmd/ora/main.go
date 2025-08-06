package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/config"
	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/llm"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"github.com/ncolesummers/open-research-agent/pkg/tools"
	"github.com/ncolesummers/open-research-agent/pkg/workflow"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	// Version information (set by build flags)
	Version   = "dev"
	BuildTime = "unknown"

	// Global telemetry instance
	telemetry *observability.Telemetry
	metrics   *observability.Metrics
	tracer    trace.Tracer
)

func main() {
	// Parse command line flags
	var (
		configPath = flag.String("config", "configs/default.yaml", "Path to configuration file")
		version    = flag.Bool("version", false, "Show version information")
		apiMode    = flag.Bool("api", false, "Run in API server mode")
		query      = flag.String("query", "", "Research query (for CLI mode)")
	)
	flag.Parse()

	// Show version if requested
	if *version {
		fmt.Printf("Open Research Agent\n")
		fmt.Printf("Version: %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		os.Exit(0)
	}

	// Load configuration
	cfg := config.LoadOrDefault(*configPath)

	// Initialize observability
	ctx := context.Background()
	if err := initObservability(ctx, cfg); err != nil {
		log.Fatalf("Failed to initialize observability: %v", err)
	}
	defer shutdownObservability(ctx)

	// Start main span
	ctx, span := tracer.Start(ctx, "main",
		trace.WithAttributes(
			attribute.String("version", Version),
			attribute.String("mode", getMode(*apiMode)),
		),
	)
	defer span.End()

	// Log startup
	log.Printf("Starting Open Research Agent v%s (built: %s)", Version, BuildTime)
	log.Printf("Configuration loaded from: %s", *configPath)

	// Initialize components with observability
	if err := run(ctx, cfg, *apiMode, *query); err != nil {
		span.RecordError(err)
		log.Fatalf("Application failed: %v", err)
	}
}

func initObservability(ctx context.Context, cfg *config.Config) error {
	// Initialize telemetry
	telConfig := &observability.TelemetryConfig{
		ServiceName:    "open-research-agent",
		ServiceVersion: Version,
		Environment:    getEnvironment(),
		OTLPEndpoint:   cfg.Observability.Tracing.Endpoint,
		PrometheusPort: cfg.Observability.Metrics.Port,
		SamplingRate:   cfg.Observability.Tracing.SamplingRate,
		EnableTracing:  cfg.Observability.Tracing.Enabled,
		EnableMetrics:  cfg.Observability.Metrics.Enabled,
		EnableLogging:  true,
	}

	var err error
	telemetry, err = observability.NewTelemetry(telConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	// Get tracer
	tracer = telemetry.Tracer()

	// Initialize metrics
	if cfg.Observability.Metrics.Enabled {
		metrics, err = observability.NewMetrics(telemetry.Meter())
		if err != nil {
			return fmt.Errorf("failed to initialize metrics: %w", err)
		}
	}

	log.Println("Observability initialized successfully")
	return nil
}

func shutdownObservability(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if telemetry != nil {
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down telemetry: %v", err)
		}
	}
}

func run(ctx context.Context, cfg *config.Config, apiMode bool, query string) error {
	// Create span for initialization
	ctx, span := tracer.Start(ctx, "initialize_components")
	defer span.End()

	// Initialize Ollama client with observability
	ollamaClient := llm.NewOllamaClient(
		cfg.Ollama.BaseURL,
		cfg.Ollama.Model,
		&llm.OllamaOptions{
			Temperature: cfg.Ollama.Temperature,
			MaxTokens:   cfg.Ollama.MaxTokens,
			TopP:        cfg.Ollama.TopP,
			TopK:        cfg.Ollama.TopK,
		},
	)

	// Check Ollama health
	healthCtx, healthSpan := tracer.Start(ctx, "ollama_health_check")
	if err := ollamaClient.CheckHealth(healthCtx); err != nil {
		healthSpan.RecordError(err)
		healthSpan.End()
		return fmt.Errorf("ollama health check failed: %w", err)
	}
	healthSpan.End()
	log.Println("Ollama connection established")

	// Initialize tool registry
	toolRegistry := createToolRegistry()

	// Create workflow config
	workflowConfig := &workflow.Config{
		Research: workflow.ResearchConfig{
			MaxConcurrency:   cfg.Research.MaxConcurrency,
			MaxIterations:    cfg.Research.MaxIterations,
			MaxDepth:         cfg.Research.MaxDepth,
			CompressionRatio: cfg.Research.CompressionRatio,
			Timeout:          cfg.Research.Timeout,
		},
		LLM: workflow.LLMConfig{
			Model:       cfg.Ollama.Model,
			Temperature: cfg.Ollama.Temperature,
			MaxTokens:   cfg.Ollama.MaxTokens,
		},
	}

	// Build research graph
	graph, err := workflow.NewResearchGraph(workflowConfig, ollamaClient, toolRegistry)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to build research graph: %w", err)
	}

	span.End()

	if apiMode {
		return runAPIServer(ctx, cfg, graph)
	} else {
		return runCLI(ctx, graph, query)
	}
}

func runAPIServer(ctx context.Context, cfg *config.Config, graph *workflow.ResearchGraph) error {
	// TODO: Implement API server in Phase 5
	return fmt.Errorf("API mode not yet implemented")
}

func runCLI(ctx context.Context, graph *workflow.ResearchGraph, query string) error {
	// If no query provided, read from stdin
	if query == "" {
		fmt.Print("Enter your research query: ")
		_, err := fmt.Scanln(&query)
		if err != nil {
			return fmt.Errorf("failed to read query from stdin: %w", err)
		}
	}

	if query == "" {
		return fmt.Errorf("no research query provided")
	}

	// Create research request
	request := &domain.ResearchRequest{
		ID:        fmt.Sprintf("req_%d", time.Now().Unix()),
		Query:     query,
		MaxDepth:  3,
		Timestamp: time.Now(),
	}

	// Record metrics
	if metrics != nil {
		metrics.RecordResearchRequest(ctx, "cli")
	}

	// Start research span
	ctx, span := tracer.Start(ctx, "research_execution",
		trace.WithAttributes(
			attribute.String("request.id", request.ID),
			attribute.String("request.query", request.Query),
		),
	)
	defer span.End()

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		cancel()
	}()

	// Execute research
	startTime := time.Now()
	log.Printf("Starting research for: %s", query)

	report, err := graph.Execute(ctx, request)
	if err != nil {
		span.RecordError(err)
		if metrics != nil {
			metrics.RecordResearchComplete(ctx, time.Since(startTime), "failed")
		}
		return fmt.Errorf("research failed: %w", err)
	}

	// Record success metrics
	if metrics != nil {
		metrics.RecordResearchComplete(ctx, time.Since(startTime), "success")
	}

	// Output report
	fmt.Println("\n=== Research Report ===")
	fmt.Printf("Report ID: %s\n", report.ID)
	fmt.Printf("Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Printf("\nExecutive Summary:\n%s\n", report.Executive)

	if len(report.Findings) > 0 {
		fmt.Println("\nKey Findings:")
		for i, finding := range report.Findings {
			fmt.Printf("%d. %s\n", i+1, finding.Title)
			fmt.Printf("   %s\n", finding.Content)
			fmt.Printf("   Confidence: %.2f\n", finding.Confidence)
		}
	}

	fmt.Printf("\nTokens Used: %d\n", report.TokensUsed)
	fmt.Printf("Duration: %s\n", time.Since(startTime))

	return nil
}

func createToolRegistry() domain.ToolRegistry {
	// Create and populate tool registry
	registry := tools.NewBasicRegistry()

	// Register basic tools
	if err := registry.Register(&tools.ThinkTool{}); err != nil {
		return nil // Ignore registration errors for now
	}

	// TODO: Add more tools in Phase 2

	return registry
}

func getMode(apiMode bool) string {
	if apiMode {
		return "api"
	}
	return "cli"
}

func getEnvironment() string {
	if env := os.Getenv("ENVIRONMENT"); env != "" {
		return env
	}
	return "development"
}
