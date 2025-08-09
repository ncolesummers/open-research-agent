package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/observability"
	"github.com/ncolesummers/open-research-agent/pkg/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ResearchGraph represents the main workflow graph
type ResearchGraph struct {
	config     *Config
	llmClient  domain.LLMClient
	tools      domain.ToolRegistry
	telemetry  *observability.Telemetry
	metrics    *observability.Metrics
	logger     *observability.StructuredLogger
	workerPool *WorkerPool
	supervisor *Supervisor
}

// Config holds the configuration for the workflow
type Config struct {
	Research ResearchConfig `json:"research"`
	LLM      LLMConfig      `json:"llm"`
}

// ResearchConfig contains research-specific configuration
type ResearchConfig struct {
	MaxConcurrency   int     `json:"max_concurrency"`
	MaxIterations    int     `json:"max_iterations"`
	MaxDepth         int     `json:"max_depth"`
	CompressionRatio float64 `json:"compression_ratio"`
	Timeout          string  `json:"timeout"`
}

// LLMConfig contains LLM configuration
type LLMConfig struct {
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
}

// NewResearchGraph creates a new research workflow graph
func NewResearchGraph(cfg *Config, llmClient domain.LLMClient, tools domain.ToolRegistry) (*ResearchGraph, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if llmClient == nil {
		return nil, fmt.Errorf("llm client is required")
	}
	if tools == nil {
		return nil, fmt.Errorf("tools registry is required")
	}

	rg := &ResearchGraph{
		config:    cfg,
		llmClient: llmClient,
		tools:     tools,
	}

	return rg, nil
}

// NewResearchGraphWithTelemetry creates a new research workflow graph with observability
func NewResearchGraphWithTelemetry(cfg *Config, llmClient domain.LLMClient, tools domain.ToolRegistry, telemetry *observability.Telemetry) (*ResearchGraph, error) {
	rg, err := NewResearchGraph(cfg, llmClient, tools)
	if err != nil {
		return nil, err
	}

	if telemetry != nil {
		rg.telemetry = telemetry
		// Initialize metrics
		metrics, err := observability.NewMetrics(telemetry.Meter())
		if err != nil {
			return nil, fmt.Errorf("failed to create metrics: %w", err)
		}
		rg.metrics = metrics
		rg.logger = observability.NewStructuredLogger("research_graph")
	}

	// Initialize worker pool if concurrency is enabled
	if cfg.Research.MaxConcurrency > 1 {
		poolConfig := &WorkerPoolConfig{
			MaxWorkers:    cfg.Research.MaxConcurrency,
			QueueSize:     50, // Default queue size
			WorkerTimeout: 2 * time.Minute,
		}

		pool, err := NewWorkerPool(poolConfig, llmClient, tools, telemetry)
		if err != nil {
			return nil, fmt.Errorf("failed to create worker pool: %w", err)
		}
		rg.workerPool = pool

		// Create supervisor for intelligent task distribution
		rg.supervisor = NewSupervisor(pool, telemetry)
	}

	return rg, nil
}

// Execute runs the research workflow
func (rg *ResearchGraph) Execute(ctx context.Context, request *domain.ResearchRequest) (*domain.ResearchReport, error) {
	// Start root span for research request
	if rg.telemetry != nil {
		var span trace.Span
		ctx, span = rg.telemetry.StartResearchRequest(ctx, request.ID, "user", request.Query)
		defer span.End()

		// Record research request metric
		if rg.metrics != nil {
			rg.metrics.RecordResearchRequest(ctx, "api")
			defer func(start time.Time) {
				rg.metrics.RecordResearchComplete(ctx, time.Since(start), "complete")
			}(time.Now())
		}
	}

	// Create initial state
	stateConfig := &state.StateConfig{
		MaxIterations: rg.config.Research.MaxIterations,
		MaxTasks:      50,
	}

	graphState := state.NewGraphState(*request, stateConfig)
	graphState.SetLLMClient(rg.llmClient)
	graphState.SetTools(rg.tools)

	// Start worker pool if available
	if rg.workerPool != nil {
		if err := rg.workerPool.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to start worker pool: %w", err)
		}
		defer func() {
			if err := rg.workerPool.Stop(ctx); err != nil {
				if rg.logger != nil {
					rg.logger.Warn(ctx, "Failed to stop worker pool",
						map[string]interface{}{"error": err.Error()})
				}
			}
		}()
	}

	// Execute workflow steps in sequence (simplified for Phase 1)
	// In Phase 2, we'll integrate with Eino's proper graph execution

	// Step 1: Start
	if err := rg.startNode(ctx, graphState); err != nil {
		return nil, fmt.Errorf("start node failed: %w", err)
	}

	// Step 2: Clarify
	if err := rg.clarifyNode(ctx, graphState); err != nil {
		return nil, fmt.Errorf("clarify node failed: %w", err)
	}

	// Check if clarification needed
	if graphState.GetPhase() == domain.PhaseComplete {
		return rg.extractReport(graphState), nil
	}

	// Step 3: Planning
	if err := rg.planningNode(ctx, graphState); err != nil {
		return nil, fmt.Errorf("planning node failed: %w", err)
	}

	// Step 4: Research loop (supervisor/researcher)
	for i := 0; i < rg.config.Research.MaxIterations; i++ {
		if err := rg.supervisorNode(ctx, graphState); err != nil {
			return nil, fmt.Errorf("supervisor node failed: %w", err)
		}

		if graphState.GetPhase() == domain.PhaseCompression {
			break
		}

		if err := rg.researcherNode(ctx, graphState); err != nil {
			return nil, fmt.Errorf("researcher node failed: %w", err)
		}
	}

	// Step 5: Compress
	if err := rg.compressionNode(ctx, graphState); err != nil {
		return nil, fmt.Errorf("compression node failed: %w", err)
	}

	// Step 6: Generate report
	if err := rg.reportGenerationNode(ctx, graphState); err != nil {
		return nil, fmt.Errorf("report generation failed: %w", err)
	}

	// Step 7: End
	if err := rg.endNode(ctx, graphState); err != nil {
		return nil, fmt.Errorf("end node failed: %w", err)
	}

	// Extract report from final state
	report := rg.extractReport(graphState)
	return report, nil
}

// Node implementations

func (rg *ResearchGraph) startNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "start", string(domain.PhaseStart), func(ctx context.Context) error {
			return rg.startNodeImpl(ctx, state)
		})
	}
	return rg.startNodeImpl(ctx, state)
}

func (rg *ResearchGraph) startNodeImpl(ctx context.Context, state *state.GraphState) error {
	// Initialize the workflow
	state.SetPhase(domain.PhaseStart)
	state.AddMessage(domain.Message{
		Role:    "system",
		Content: "Starting research workflow for query: " + state.Request.Query,
	})
	return nil
}

func (rg *ResearchGraph) clarifyNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "clarify", string(domain.PhaseClarification), func(ctx context.Context) error {
			return rg.clarifyNodeImpl(ctx, state)
		})
	}
	return rg.clarifyNodeImpl(ctx, state)
}

func (rg *ResearchGraph) clarifyNodeImpl(ctx context.Context, state *state.GraphState) error {
	// Check if clarification is needed
	messages := []domain.Message{
		{
			Role:    "system",
			Content: clarifySystemPrompt,
		},
		{
			Role:    "user",
			Content: state.Request.Query,
		},
	}

	var response *domain.ChatResponse
	var err error

	// Instrument LLM call if telemetry is available
	if rg.telemetry != nil {
		err = rg.telemetry.InstrumentLLMCall(ctx, rg.config.LLM.Model, func(ctx context.Context) (int, int, error) {
			response, err = state.GetLLMClient().Chat(ctx, messages, domain.ChatOptions{
				Temperature: 0.3,
				MaxTokens:   500,
			})
			if err != nil {
				return 0, 0, err
			}
			return response.Usage.PromptTokens, response.Usage.CompletionTokens, nil
		})
	} else {
		response, err = state.GetLLMClient().Chat(ctx, messages, domain.ChatOptions{
			Temperature: 0.3,
			MaxTokens:   500,
		})
	}

	if err != nil {
		return fmt.Errorf("clarification failed: %w", err)
	}

	state.AddMessage(domain.Message{
		Role:    "assistant",
		Content: response.Content,
	})

	// Simple check - in production, parse LLM response properly
	if len(state.Request.Query) < 10 {
		state.SetPhase(domain.PhaseComplete)
	} else {
		state.SetPhase(domain.PhasePlanning)
	}

	return nil
}

func (rg *ResearchGraph) planningNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "planning", string(domain.PhasePlanning), func(ctx context.Context) error {
			return rg.planningNodeImpl(ctx, state)
		})
	}
	return rg.planningNodeImpl(ctx, state)
}

func (rg *ResearchGraph) planningNodeImpl(ctx context.Context, state *state.GraphState) error {
	// Generate research plan
	messages := []domain.Message{
		{
			Role:    "system",
			Content: planningSystemPrompt,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Research query: %s\nContext: %s", state.Request.Query, state.Request.Context),
		},
	}

	var response *domain.ChatResponse
	var err error

	// Instrument LLM call if telemetry is available
	if rg.telemetry != nil {
		err = rg.telemetry.InstrumentLLMCall(ctx, rg.config.LLM.Model, func(ctx context.Context) (int, int, error) {
			response, err = state.GetLLMClient().Chat(ctx, messages, domain.ChatOptions{
				Temperature: 0.5,
				MaxTokens:   1000,
			})
			if err != nil {
				return 0, 0, err
			}
			return response.Usage.PromptTokens, response.Usage.CompletionTokens, nil
		})
	} else {
		response, err = state.GetLLMClient().Chat(ctx, messages, domain.ChatOptions{
			Temperature: 0.5,
			MaxTokens:   1000,
		})
	}

	if err != nil {
		return fmt.Errorf("planning failed: %w", err)
	}

	// Parse response and create tasks
	tasks := rg.parseTasksFromResponse(response.Content)
	for _, task := range tasks {
		state.AddTask(task)
		// Record task creation metric
		if rg.metrics != nil {
			rg.metrics.RecordTaskCreated(ctx, "research")
		}
	}

	state.SetPhase(domain.PhaseResearch)
	return nil
}

func (rg *ResearchGraph) supervisorNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "supervisor", string(domain.PhaseResearch), func(ctx context.Context) error {
			return rg.supervisorNodeImpl(ctx, state)
		})
	}
	return rg.supervisorNodeImpl(ctx, state)
}

func (rg *ResearchGraph) supervisorNodeImpl(ctx context.Context, state *state.GraphState) error {
	// Check if we need to continue research
	if state.Iterations >= state.MaxIterations {
		state.SetPhase(domain.PhaseCompression)
		return nil
	}

	// Check task completion
	stats := state.GetTaskStats()
	if stats.Pending == 0 && stats.InProgress == 0 {
		state.SetPhase(domain.PhaseCompression)
		return nil
	}

	// Use enhanced supervisor if available
	if rg.supervisor != nil && rg.workerPool != nil {
		// Get pending tasks
		pendingTasks := state.GetPendingTasks()

		// Distribute tasks using the supervisor's intelligent distribution
		if err := rg.supervisor.DistributeTasks(ctx, pendingTasks, state); err != nil {
			if rg.logger != nil {
				rg.logger.Error(ctx, "Supervisor task distribution failed", err,
					map[string]interface{}{
						"task_count": len(pendingTasks),
					})
			}
			return fmt.Errorf("supervisor distribution failed: %w", err)
		}

		// Monitor progress
		progress := rg.supervisor.MonitorProgress(ctx)
		if rg.logger != nil {
			rg.logger.Info(ctx, "Task distribution progress",
				map[string]interface{}{
					"total":       progress.TotalTasks,
					"completed":   progress.CompletedTasks,
					"in_progress": progress.InProgressTasks,
					"failed":      progress.FailedTasks,
				})
		}

		// Adapt strategy if needed
		rg.supervisor.AdaptStrategy(ctx)

		// Rebalance if needed
		if err := rg.supervisor.RebalanceTasks(ctx); err != nil {
			if rg.logger != nil {
				rg.logger.Warn(ctx, "Task rebalancing failed",
					map[string]interface{}{"error": err.Error()})
			}
		}
	} else if rg.workerPool != nil {
		// Fallback to simple distribution if supervisor not available
		pendingTasks := state.GetPendingTasks()
		for _, task := range pendingTasks {
			if err := rg.workerPool.Submit(ctx, task, state); err != nil {
				// Log error but continue with other tasks
				if rg.logger != nil {
					rg.logger.Warn(ctx, "Failed to submit task to worker pool",
						map[string]interface{}{
							"task_id": task.ID,
							"error":   err.Error(),
						},
					)
				}
			}
		}

		// Process results from worker pool
		go rg.processWorkerResults(ctx, state)

		// Wait for some tasks to complete before next iteration
		time.Sleep(100 * time.Millisecond)
	} else {
		// Sequential execution mode
		state.SetPhase(domain.PhaseResearch)
	}

	state.IncrementIteration()
	return nil
}

func (rg *ResearchGraph) researcherNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "researcher", string(domain.PhaseResearch), func(ctx context.Context) error {
			return rg.researcherNodeImpl(ctx, state)
		})
	}
	return rg.researcherNodeImpl(ctx, state)
}

func (rg *ResearchGraph) researcherNodeImpl(ctx context.Context, state *state.GraphState) error {
	// If worker pool is active, skip sequential researcher execution
	if rg.workerPool != nil {
		// Workers are handling the research tasks in parallel
		return nil
	}

	// Sequential execution mode (Phase 1 compatibility)
	// Get next pending task
	task := state.GetNextPendingTask()
	if task == nil {
		return nil
	}

	// Record task started metric
	if rg.metrics != nil {
		rg.metrics.RecordTaskStarted(ctx)
	}
	taskStartTime := time.Now()

	// Update task status
	err := state.UpdateTask(task.ID, func(t *domain.ResearchTask) {
		t.Status = domain.TaskStatusInProgress
	})
	if err != nil {
		return fmt.Errorf("failed to update task status to in progress: %w", err)
	}

	// Execute research for this task (with span)
	var result *domain.ResearchResult
	if rg.telemetry != nil {
		ctx, span := rg.telemetry.StartSpan(ctx, "research.execute",
			trace.WithAttributes(
				attribute.String("task.id", task.ID),
				attribute.String("task.topic", task.Topic),
			),
		)
		defer span.End()
		result, err = rg.executeResearch(ctx, state, task)
	} else {
		result, err = rg.executeResearch(ctx, state, task)
	}

	if err != nil {
		updateErr := state.UpdateTask(task.ID, func(t *domain.ResearchTask) {
			t.Status = domain.TaskStatusFailed
		})
		// Record task failure metric
		if rg.metrics != nil {
			rg.metrics.RecordTaskComplete(ctx, time.Since(taskStartTime), "failed")
		}
		if updateErr != nil {
			return fmt.Errorf("research failed for task %s and failed to update status: %w (original error: %v)", task.ID, updateErr, err)
		}
		return fmt.Errorf("research failed for task %s: %w", task.ID, err)
	}

	// Update task with results
	err = state.UpdateTask(task.ID, func(t *domain.ResearchTask) {
		t.Status = domain.TaskStatusCompleted
		t.Results = append(t.Results, *result)
		completedAt := time.Now()
		t.CompletedAt = &completedAt
	})
	// Record task completion metric
	if rg.metrics != nil {
		rg.metrics.RecordTaskComplete(ctx, time.Since(taskStartTime), "success")
	}
	if err != nil {
		return fmt.Errorf("failed to update task with results: %w", err)
	}

	state.AddResult(*result)
	return nil
}

func (rg *ResearchGraph) compressionNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "compression", string(domain.PhaseCompression), func(ctx context.Context) error {
			return rg.compressionNodeImpl(ctx, state)
		})
	}
	return rg.compressionNodeImpl(ctx, state)
}

func (rg *ResearchGraph) compressionNodeImpl(ctx context.Context, state *state.GraphState) error {
	// Compress and synthesize research results
	state.SetPhase(domain.PhaseReporting)

	// In a real implementation, this would compress findings
	// For now, just transition to reporting
	return nil
}

func (rg *ResearchGraph) reportGenerationNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "report", string(domain.PhaseReporting), func(ctx context.Context) error {
			return rg.reportGenerationNodeImpl(ctx, state)
		})
	}
	return rg.reportGenerationNodeImpl(ctx, state)
}

func (rg *ResearchGraph) reportGenerationNodeImpl(ctx context.Context, state *state.GraphState) error {
	// Generate final report
	state.SetPhase(domain.PhaseComplete)

	// In a real implementation, this would generate a comprehensive report
	// For now, just mark as complete
	return nil
}

func (rg *ResearchGraph) endNode(ctx context.Context, state *state.GraphState) error {
	if rg.telemetry != nil {
		return rg.telemetry.InstrumentWorkflowNode(ctx, "end", string(domain.PhaseComplete), func(ctx context.Context) error {
			return rg.endNodeImpl(ctx, state)
		})
	}
	return rg.endNodeImpl(ctx, state)
}

func (rg *ResearchGraph) endNodeImpl(ctx context.Context, state *state.GraphState) error {
	// Finalize the workflow
	state.SetPhase(domain.PhaseComplete)
	return nil
}

// Helper methods

func (rg *ResearchGraph) executeResearch(ctx context.Context, state *state.GraphState, task *domain.ResearchTask) (*domain.ResearchResult, error) {
	// Simple research execution
	// In production, this would use tools and multiple iterations

	messages := []domain.Message{
		{
			Role:    "system",
			Content: "You are a research assistant. Research the following topic and provide comprehensive information.",
		},
		{
			Role:    "user",
			Content: task.Topic,
		},
	}

	response, err := state.GetLLMClient().Chat(ctx, messages, domain.ChatOptions{
		Temperature: 0.7,
		MaxTokens:   2000,
	})
	if err != nil {
		return nil, err
	}

	result := &domain.ResearchResult{
		ID:         fmt.Sprintf("result_%s_%d", task.ID, time.Now().Unix()),
		TaskID:     task.ID,
		Source:     "llm_research",
		Content:    response.Content,
		Summary:    rg.generateSummary(response.Content),
		Confidence: 0.8,
		Timestamp:  time.Now(),
	}

	return result, nil
}

func (rg *ResearchGraph) parseTasksFromResponse(response string) []*domain.ResearchTask {
	// Simple task parsing - in production, use structured output
	tasks := []*domain.ResearchTask{
		{
			ID:       fmt.Sprintf("task_%d", time.Now().Unix()),
			Topic:    "Main research topic from query",
			Status:   domain.TaskStatusPending,
			Priority: 1,
		},
	}
	return tasks
}

func (rg *ResearchGraph) generateSummary(content string) string {
	// Simple summary - in production, use LLM for summarization
	if len(content) > 200 {
		return content[:200] + "..."
	}
	return content
}

func (rg *ResearchGraph) extractReport(state *state.GraphState) *domain.ResearchReport {
	// Extract report from final state
	report := &domain.ResearchReport{
		ID:          fmt.Sprintf("report_%s", state.Request.ID),
		RequestID:   state.Request.ID,
		Executive:   "Research completed successfully",
		Sections:    []domain.ReportSection{},
		Findings:    []domain.Finding{},
		Sources:     []domain.Source{},
		GeneratedAt: time.Now(),
	}

	// Add findings from results
	for _, result := range state.Results {
		finding := domain.Finding{
			ID:         result.ID,
			Title:      "Research Finding",
			Content:    result.Summary,
			Confidence: result.Confidence,
		}
		report.Findings = append(report.Findings, finding)
	}

	return report
}

// processWorkerResults processes results from the worker pool
func (rg *ResearchGraph) processWorkerResults(ctx context.Context, state *state.GraphState) {
	if rg.workerPool == nil {
		return
	}

	resultCh := rg.workerPool.GetResults()
	errorCh := rg.workerPool.GetErrors()

	for {
		select {
		case result := <-resultCh:
			if result != nil {
				// Result is already added to state by the worker
				if rg.logger != nil {
					rg.logger.Debug(ctx, "Received result from worker pool",
						map[string]interface{}{
							"task_id":   result.TaskID,
							"result_id": result.ID,
						},
					)
				}
			}
		case err := <-errorCh:
			if err != nil && rg.logger != nil {
				rg.logger.Error(ctx, "Worker pool error", err, nil)
			}
		case <-ctx.Done():
			return
		}
	}
}

// System prompts
const (
	clarifySystemPrompt = `You are a research assistant. Analyze the user's query and determine if it needs clarification.
If the query is clear and specific enough for research, respond with "CLEAR".
If clarification is needed, ask specific questions to better understand the research request.`

	planningSystemPrompt = `You are a research planner. Based on the user's query, create a research plan.
Break down the query into specific research tasks that can be executed in parallel.
Each task should focus on a specific aspect of the research question.`
)
