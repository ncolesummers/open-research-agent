package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ResearchExecutor handles the core research logic and tool chain orchestration
type ResearchExecutor struct {
	llmClient domain.LLMClient
	tools     domain.ToolRegistry
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger
}

// NewResearchExecutor creates a new research executor
func NewResearchExecutor(
	llmClient domain.LLMClient,
	tools domain.ToolRegistry,
	telemetry *observability.Telemetry,
	logger *observability.StructuredLogger,
) *ResearchExecutor {
	return &ResearchExecutor{
		llmClient: llmClient,
		tools:     tools,
		telemetry: telemetry,
		logger:    logger,
	}
}

// ExecuteResearch performs the complete research pipeline for a task
func (e *ResearchExecutor) ExecuteResearch(ctx context.Context, task *domain.ResearchTask) (*domain.ResearchResult, error) {
	// Execute tool chain: search → think → summarize
	searchResults, toolsUsed := e.executeWebSearch(ctx, task.Topic)

	analysis, err := e.performAnalysis(ctx, task, searchResults)
	if err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}

	summary := e.generateSummary(ctx, analysis, task.ID)
	if summary == "" {
		summary = e.createBasicSummary(analysis)
	}

	confidence := e.calculateConfidence(searchResults, analysis, summary, toolsUsed)

	return &domain.ResearchResult{
		ID:         fmt.Sprintf("result_%s_%d", task.ID, time.Now().Unix()),
		TaskID:     task.ID,
		Source:     "research_executor",
		Content:    analysis,
		Summary:    summary,
		Confidence: confidence,
		Metadata: map[string]interface{}{
			"has_search_results": searchResults != "",
			"tools_used":         toolsUsed,
		},
		Timestamp: time.Now(),
	}, nil
}

// executeWebSearch performs web search using the web_search tool if available
func (e *ResearchExecutor) executeWebSearch(ctx context.Context, topic string) (string, []string) {
	if e.tools == nil {
		return "", nil
	}

	searchTool, err := e.tools.Get("web_search")
	if err != nil {
		e.logger.Debug(ctx, "Web search tool not available",
			map[string]interface{}{"error": err.Error()},
		)
		return "", nil
	}

	var span trace.Span
	if e.telemetry != nil {
		ctx, span = e.telemetry.StartSpan(ctx, "tool.web_search",
			trace.WithAttributes(
				attribute.String("tool.name", "web_search"),
				attribute.String("query", topic),
			),
		)
		defer span.End()
	}

	result, err := searchTool.Execute(ctx, map[string]interface{}{
		"query":       topic,
		"max_results": 5,
	})

	if err != nil {
		e.logger.Warn(ctx, "Web search failed",
			map[string]interface{}{"error": err.Error()},
		)
		return "", nil
	}

	return fmt.Sprintf("%v", result), []string{"web_search"}
}

// performAnalysis uses the LLM to analyze the topic with search results
func (e *ResearchExecutor) performAnalysis(ctx context.Context, task *domain.ResearchTask, searchResults string) (string, error) {
	messages := e.buildAnalysisPrompt(task, searchResults)

	var response *domain.ChatResponse
	var err error

	if e.telemetry != nil {
		err = e.telemetry.InstrumentLLMCall(ctx, "llama3.2", func(ctx context.Context) (int, int, error) {
			response, err = e.llmClient.Chat(ctx, messages, domain.ChatOptions{
				Temperature: 0.7,
				MaxTokens:   2000,
			})
			if err != nil {
				return 0, 0, err
			}
			return response.Usage.PromptTokens, response.Usage.CompletionTokens, nil
		})
	} else {
		response, err = e.llmClient.Chat(ctx, messages, domain.ChatOptions{
			Temperature: 0.7,
			MaxTokens:   2000,
		})
	}

	if err != nil {
		return "", err
	}

	return response.Content, nil
}

// generateSummary uses the summarize tool if available
func (e *ResearchExecutor) generateSummary(ctx context.Context, content string, taskID string) string {
	if e.tools == nil {
		return ""
	}

	summarizeTool, err := e.tools.Get("summarize")
	if err != nil {
		return ""
	}

	var span trace.Span
	if e.telemetry != nil {
		ctx, span = e.telemetry.StartSpan(ctx, "tool.summarize",
			trace.WithAttributes(
				attribute.String("tool.name", "summarize"),
				attribute.String("task.id", taskID),
			),
		)
		defer span.End()
	}

	result, err := summarizeTool.Execute(ctx, map[string]interface{}{
		"content":    content,
		"max_length": 500,
	})

	if err != nil {
		e.logger.Debug(ctx, "Summarize tool failed",
			map[string]interface{}{"error": err.Error()},
		)
		return ""
	}

	return fmt.Sprintf("%v", result)
}

// buildAnalysisPrompt constructs the prompt for LLM analysis
func (e *ResearchExecutor) buildAnalysisPrompt(task *domain.ResearchTask, searchResults string) []domain.Message {
	systemPrompt := `You are an expert research assistant. Provide comprehensive, accurate, and well-structured research.
Focus on: key facts, important context, current trends, reliable sources, and clear insights.`

	messages := []domain.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Research Topic: %s", task.Topic)},
	}

	if searchResults != "" {
		messages = append(messages, domain.Message{
			Role:    "system",
			Content: fmt.Sprintf("Web search results:\n%s", searchResults),
		})
	}

	if len(task.Results) > 0 {
		var previous strings.Builder
		previous.WriteString("Previous findings:\n")
		for _, r := range task.Results {
			fmt.Fprintf(&previous, "- %s (confidence: %.2f)\n", r.Summary, r.Confidence)
		}
		messages = append(messages, domain.Message{
			Role:    "system",
			Content: previous.String(),
		})
	}

	return messages
}

// createBasicSummary creates a simple summary when summarize tool is unavailable
func (e *ResearchExecutor) createBasicSummary(content string) string {
	lines := strings.Split(content, "\n")
	var summary []string

	for i, line := range lines {
		if i >= 3 {
			break
		}
		line = strings.TrimSpace(line)
		if line != "" {
			summary = append(summary, line)
		}
	}

	result := strings.Join(summary, " ")
	if len(result) > 500 {
		result = result[:497] + "..."
	}

	return result
}

// calculateConfidence calculates a confidence score based on various factors
func (e *ResearchExecutor) calculateConfidence(searchResults, analysis, summary string, toolsUsed []string) float64 {
	confidence := 0.5 // Base confidence

	if searchResults != "" {
		confidence += 0.2
	}

	if len(analysis) > 500 {
		confidence += 0.1
	}

	if len(summary) > 100 {
		confidence += 0.1
	}

	if len(toolsUsed) > 0 {
		confidence += 0.05 * float64(len(toolsUsed))
	}

	// Check for structured content
	if strings.Contains(analysis, "1.") || strings.Contains(analysis, "•") {
		confidence += 0.05
	}

	// Cap at 0.95
	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}
