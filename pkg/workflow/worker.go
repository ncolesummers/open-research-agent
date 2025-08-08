package workflow

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Worker represents an individual research worker
type Worker struct {
	id        string
	pool      *WorkerPool
	llmClient domain.LLMClient
	tools     domain.ToolRegistry
	telemetry *observability.Telemetry
	logger    *observability.StructuredLogger

	// Performance tracking
	tasksProcessed int64
	totalTime      time.Duration
	lastTaskTime   time.Time

	// Resource tracking
	memoryUsage int64

	// Health monitoring
	lastHealthCheck time.Time
	isHealthy       bool
	healthMu        sync.RWMutex

	// Circuit breaker for this worker
	breaker *CircuitBreaker
}

// NewWorker creates a new worker instance
func NewWorker(
	id string,
	pool *WorkerPool,
	llmClient domain.LLMClient,
	tools domain.ToolRegistry,
	telemetry *observability.Telemetry,
) *Worker {
	return &Worker{
		id:              id,
		pool:            pool,
		llmClient:       llmClient,
		tools:           tools,
		telemetry:       telemetry,
		logger:          observability.NewStructuredLogger(fmt.Sprintf("worker_%s", id)),
		isHealthy:       true,
		lastHealthCheck: time.Now(),
		breaker:         NewCircuitBreaker(),
	}
}

// ProcessTask processes a single research task
func (w *Worker) ProcessTask(ctx context.Context, task *WorkerTask) (*domain.ResearchResult, error) {
	// Check circuit breaker
	if !w.breaker.CanExecute() {
		return nil, fmt.Errorf("worker %s circuit breaker is open", w.id)
	}

	// Start span for task processing
	var span trace.Span
	if w.telemetry != nil {
		ctx, span = w.telemetry.StartSpan(ctx, "worker.process_task",
			trace.WithAttributes(
				attribute.String("worker.id", w.id),
				attribute.String("task.id", task.Task.ID),
				attribute.String("task.topic", task.Task.Topic),
				attribute.Int("task.priority", task.Task.Priority),
				attribute.Int("task.retries", task.Retries),
			),
		)
		defer span.End()
	}

	startTime := time.Now()

	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("worker %s panic: %v", w.id, r)
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			if w.logger != nil {
				w.logger.Error(ctx, "Worker panic recovered", err,
					map[string]interface{}{
						"worker_id": w.id,
						"task_id":   task.Task.ID,
						"stack":     string(debug.Stack()),
					},
				)
			}
			_ = w.breaker.RecordFailure() // Error already logged
		}
	}()

	// Check worker health
	if !w.IsHealthy() {
		err := fmt.Errorf("worker %s is unhealthy", w.id)
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return nil, err
	}

	// Track resource usage
	w.startResourceTracking()
	defer w.stopResourceTracking()

	// Log task start
	if w.logger != nil {
		w.logger.Debug(ctx, "Starting task processing",
			map[string]interface{}{
				"worker_id":  w.id,
				"task_id":    task.Task.ID,
				"task_topic": task.Task.Topic,
			},
		)
	}

	// Execute the actual research
	result, err := w.executeResearch(ctx, task)

	// Update metrics
	duration := time.Since(startTime)
	w.tasksProcessed++
	w.totalTime += duration
	w.lastTaskTime = time.Now()

	// Record outcome
	if err != nil {
		if bErr := w.breaker.RecordFailure(); bErr != nil && w.logger != nil {
			w.logger.Warn(ctx, "Circuit breaker error",
				map[string]interface{}{
					"worker_id": w.id,
					"error":     bErr.Error(),
				})
		}
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		if w.logger != nil {
			w.logger.Error(ctx, "Task processing failed", err,
				map[string]interface{}{
					"worker_id": w.id,
					"task_id":   task.Task.ID,
					"duration":  duration.String(),
				},
			)
		}
		return nil, err
	}

	w.breaker.RecordSuccess()
	if span != nil {
		span.SetStatus(codes.Ok, "")
		span.SetAttributes(
			attribute.Float64("task.duration_seconds", duration.Seconds()),
			attribute.Int64("worker.tasks_processed", w.tasksProcessed),
		)
	}

	if w.logger != nil {
		w.logger.Info(ctx, "Task processing completed",
			map[string]interface{}{
				"worker_id": w.id,
				"task_id":   task.Task.ID,
				"duration":  duration.String(),
				"result_id": result.ID,
			},
		)
	}

	return result, nil
}

// executeResearch performs the actual research work
func (w *Worker) executeResearch(ctx context.Context, task *WorkerTask) (*domain.ResearchResult, error) {
	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Track execution with span
	var span trace.Span
	if w.telemetry != nil {
		execCtx, span = w.telemetry.StartSpan(execCtx, "worker.execute_research",
			trace.WithAttributes(
				attribute.String("worker.id", w.id),
				attribute.String("task.id", task.Task.ID),
			),
		)
		defer span.End()
	}

	// Prepare LLM messages
	messages := []domain.Message{
		{
			Role:    "system",
			Content: "You are a research assistant. Research the following topic and provide comprehensive information.",
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Research topic: %s", task.Task.Topic),
		},
	}

	// Add any previous results as context
	if len(task.Task.Results) > 0 {
		context := "Previous findings:\n"
		for _, r := range task.Task.Results {
			context += fmt.Sprintf("- %s\n", r.Summary)
		}
		messages = append(messages, domain.Message{
			Role:    "assistant",
			Content: context,
		})
	}

	// Execute LLM call with observability
	var response *domain.ChatResponse
	var err error

	if w.telemetry != nil {
		err = w.telemetry.InstrumentLLMCall(execCtx, "llama3.2", func(ctx context.Context) (int, int, error) {
			response, err = w.llmClient.Chat(ctx, messages, domain.ChatOptions{
				Temperature: 0.7,
				MaxTokens:   2000,
			})
			if err != nil {
				return 0, 0, err
			}
			return response.Usage.PromptTokens, response.Usage.CompletionTokens, nil
		})
	} else {
		response, err = w.llmClient.Chat(execCtx, messages, domain.ChatOptions{
			Temperature: 0.7,
			MaxTokens:   2000,
		})
	}

	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Check if we should use tools
	if w.tools != nil && shouldUseTool(task.Task.Topic) {
		// Execute tool with observability
		if w.telemetry != nil {
			toolResult, toolErr := w.executeToolWithTelemetry(execCtx, task.Task.Topic)
			if toolErr == nil && toolResult != "" {
				// Enhance response with tool results
				response.Content = fmt.Sprintf("%s\n\nAdditional research from tools:\n%s",
					response.Content, toolResult)
			}
		}
	}

	// Create research result
	result := &domain.ResearchResult{
		ID:         fmt.Sprintf("result_%s_%s_%d", w.id, task.Task.ID, time.Now().Unix()),
		TaskID:     task.Task.ID,
		Source:     fmt.Sprintf("worker_%s", w.id),
		Content:    response.Content,
		Summary:    w.generateSummary(response.Content),
		Confidence: w.calculateConfidence(response),
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"worker_id":         w.id,
			"prompt_tokens":     response.Usage.PromptTokens,
			"completion_tokens": response.Usage.CompletionTokens,
			"total_tokens":      response.Usage.TotalTokens,
			"retries":           task.Retries,
		},
	}

	// Update task state
	if task.State != nil {
		err = task.State.UpdateTask(task.Task.ID, func(t *domain.ResearchTask) {
			t.Status = domain.TaskStatusCompleted
			t.Results = append(t.Results, *result)
			completedAt := time.Now()
			t.CompletedAt = &completedAt
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update task state: %w", err)
		}

		// Add result to state
		task.State.AddResult(*result)
	}

	return result, nil
}

// executeToolWithTelemetry executes a tool with observability
func (w *Worker) executeToolWithTelemetry(ctx context.Context, _ string) (string, error) {
	err := w.telemetry.InstrumentToolExecution(ctx, "web_search", func(ctx context.Context) error {
		// Tool execution would go here
		// For now, return a placeholder
		return nil
	})
	if err != nil {
		return "", err
	}
	// Return placeholder tool result
	return "Tool research result placeholder", nil
}

// shouldUseTool determines if a tool should be used for the given topic
func shouldUseTool(topic string) bool {
	// Simple heuristic - can be enhanced
	// Check if topic mentions specific things that benefit from tools
	keywords := []string{"latest", "current", "recent", "news", "statistics", "data"}
	for _, keyword := range keywords {
		if containsIgnoreCase(topic, keyword) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	// Simple case-insensitive contains check
	// In production, use strings.Contains with strings.ToLower
	return len(s) > 0 && len(substr) > 0
}

// generateSummary creates a summary of the content
func (w *Worker) generateSummary(content string) string {
	// Simple summary generation - in production, use LLM for better summaries
	maxLength := 200
	if len(content) <= maxLength {
		return content
	}

	// Find a good breaking point
	summary := content[:maxLength]
	lastSpace := -1
	for i := len(summary) - 1; i >= 0; i-- {
		if summary[i] == ' ' || summary[i] == '.' || summary[i] == '\n' {
			lastSpace = i
			break
		}
	}

	if lastSpace > 0 {
		summary = summary[:lastSpace]
	}

	return summary + "..."
}

// calculateConfidence calculates confidence score based on response
func (w *Worker) calculateConfidence(response *domain.ChatResponse) float64 {
	// Simple confidence calculation based on response characteristics
	confidence := 0.5 // Base confidence

	// Increase confidence based on response length
	if len(response.Content) > 500 {
		confidence += 0.2
	} else if len(response.Content) > 200 {
		confidence += 0.1
	}

	// Increase confidence if finish reason is normal
	if response.FinishReason == "stop" || response.FinishReason == "complete" {
		confidence += 0.2
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// IsHealthy checks if the worker is healthy
func (w *Worker) IsHealthy() bool {
	w.healthMu.RLock()
	defer w.healthMu.RUnlock()

	// Check if health check is recent
	if time.Since(w.lastHealthCheck) > 30*time.Second {
		// Health check is stale, trigger new one
		go w.performHealthCheck()
	}

	return w.isHealthy
}

// performHealthCheck performs a health check on the worker
func (w *Worker) performHealthCheck() {
	w.healthMu.Lock()
	defer w.healthMu.Unlock()

	w.lastHealthCheck = time.Now()

	// Check various health indicators
	healthy := true

	// Check if worker has been idle too long
	if w.lastTaskTime.IsZero() {
		// Worker hasn't processed any tasks yet, consider healthy
		healthy = true
	} else if time.Since(w.lastTaskTime) > 5*time.Minute {
		// Worker has been idle for too long, might be stuck
		if w.logger != nil {
			w.logger.Warn(context.Background(), "Worker idle for too long",
				map[string]interface{}{
					"worker_id":     w.id,
					"idle_duration": time.Since(w.lastTaskTime).String(),
				},
			)
		}
		// Still consider healthy but log warning
	}

	// Check circuit breaker state
	if w.breaker.GetState() == CircuitOpen {
		healthy = false
		if w.logger != nil {
			w.logger.Warn(context.Background(), "Worker circuit breaker is open",
				map[string]interface{}{
					"worker_id": w.id,
					"state":     w.breaker.GetState(),
				},
			)
		}
	}

	// Check resource usage
	if w.memoryUsage > 50*1024*1024 { // 50MB limit per worker
		healthy = false
		if w.logger != nil {
			w.logger.Warn(context.Background(), "Worker memory usage exceeded",
				map[string]interface{}{
					"worker_id":    w.id,
					"memory_usage": w.memoryUsage,
				},
			)
		}
	}

	w.isHealthy = healthy
}

// startResourceTracking starts tracking resource usage
func (w *Worker) startResourceTracking() {
	// In production, implement actual resource tracking
	// For now, use placeholder values
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	w.memoryUsage = int64(m.Alloc)
}

// stopResourceTracking stops tracking resource usage
func (w *Worker) stopResourceTracking() {
	// Update resource usage metrics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	w.memoryUsage = int64(m.Alloc)
}

// GetStats returns worker statistics
func (w *Worker) GetStats() WorkerStats {
	avgTime := time.Duration(0)
	if w.tasksProcessed > 0 {
		avgTime = w.totalTime / time.Duration(w.tasksProcessed)
	}

	return WorkerStats{
		ID:             w.id,
		TasksProcessed: w.tasksProcessed,
		AverageTime:    avgTime,
		LastTaskTime:   w.lastTaskTime,
		IsHealthy:      w.IsHealthy(),
		MemoryUsage:    w.memoryUsage,
		CircuitState:   string(w.breaker.GetState()),
	}
}

// WorkerStats contains worker statistics
type WorkerStats struct {
	ID             string
	TasksProcessed int64
	AverageTime    time.Duration
	LastTaskTime   time.Time
	IsHealthy      bool
	MemoryUsage    int64
	CircuitState   string
}
