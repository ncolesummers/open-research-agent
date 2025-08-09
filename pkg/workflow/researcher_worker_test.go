package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/internal/testutil"
	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResearcherWorker_ProcessTask(t *testing.T) {
	tests := []struct {
		name           string
		task           *domain.ResearchTask
		llmResponse    string
		expectedResult bool
		expectedError  bool
	}{
		{
			name: "successful research task",
			task: &domain.ResearchTask{
				ID:       "task-1",
				Topic:    "test research topic",
				Status:   domain.TaskStatusPending,
				Priority: 1,
			},
			llmResponse:    "This is a comprehensive research response about the topic.",
			expectedResult: true,
			expectedError:  false,
		},
		{
			name: "task with previous results",
			task: &domain.ResearchTask{
				ID:       "task-2",
				Topic:    "continued research",
				Status:   domain.TaskStatusPending,
				Priority: 2,
				Results: []domain.ResearchResult{
					{
						ID:         "prev-1",
						Summary:    "Previous finding",
						Confidence: 0.7,
					},
				},
			},
			llmResponse:    "Building on previous findings with new insights.",
			expectedResult: true,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Create channels
			taskChan := make(chan *domain.ResearchTask, 1)
			resultChan := make(chan *domain.ResearchResult, 1)
			errorChan := make(chan error, 1)

			// Create mock LLM client
			mockLLM := testutil.NewMockLLMClient()
			// Set default response to handle any prompt
			mockLLM.Responses["default"] = tt.llmResponse

			// Create mock tool registry
			mockTools := testutil.NewMockToolRegistry()

			// Create worker
			var wg sync.WaitGroup
			worker := NewResearcherWorker(
				"test-worker",
				mockLLM,
				mockTools,
				taskChan,
				resultChan,
				errorChan,
				nil, // telemetry
				&wg,
			)

			// Start worker
			worker.Start(ctx)

			// Submit task
			taskChan <- tt.task

			// Wait for result or error
			var result *domain.ResearchResult
			var err error

			select {
			case result = <-resultChan:
				assert.True(t, tt.expectedResult, "expected result but got none")
				assert.NotNil(t, result)
				assert.Equal(t, tt.task.ID, result.TaskID)
				assert.Contains(t, result.Content, tt.llmResponse)
				assert.Greater(t, result.Confidence, 0.0)
			case err = <-errorChan:
				if !tt.expectedError {
					t.Errorf("unexpected error: %v", err)
				}
			case <-time.After(2 * time.Second):
				if tt.expectedResult {
					t.Error("timeout waiting for result")
				}
			}

			// Stop worker
			worker.Stop()
			close(taskChan)

			// Wait for worker to finish
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Worker stopped successfully
			case <-time.After(1 * time.Second):
				t.Error("timeout waiting for worker to stop")
			}
		})
	}
}

func TestResearcherWorker_HealthAndMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Setup
	taskChan := make(chan *domain.ResearchTask, 1)
	resultChan := make(chan *domain.ResearchResult, 1)
	errorChan := make(chan error, 1)

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.Responses["default"] = "Research response"

	var wg sync.WaitGroup
	worker := NewResearcherWorker(
		"health-test-worker",
		mockLLM,
		nil, // tools
		taskChan,
		resultChan,
		errorChan,
		nil, // telemetry
		&wg,
	)

	// Start worker
	worker.Start(ctx)

	// Check initial health
	assert.True(t, worker.IsHealthy())

	// Process a task
	task := &domain.ResearchTask{
		ID:       "health-task-1",
		Topic:    "test topic",
		Status:   domain.TaskStatusPending,
		Priority: 1,
	}
	taskChan <- task

	// Wait for result
	select {
	case <-resultChan:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	// Check metrics
	metrics := worker.GetMetrics()
	assert.Equal(t, int64(1), metrics.TasksProcessed)
	assert.Equal(t, int64(1), metrics.TasksSucceeded)
	assert.Equal(t, int64(0), metrics.TasksFailed)
	assert.Greater(t, metrics.AvgConfidence, 0.0)

	// Check health status
	healthStatus := worker.GetHealthStatus()
	assert.NotNil(t, healthStatus)
	assert.Equal(t, "health-test-worker", healthStatus["worker_id"])
	assert.True(t, healthStatus["is_healthy"].(bool))

	// Stop worker
	worker.Stop()
	close(taskChan)
	wg.Wait()
}

func TestResearcherPool_ConcurrentExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create mock LLM client
	mockLLM := testutil.NewMockLLMClient()
	// Add generic response for any query
	mockLLM.Responses["default"] = "Generic research response for testing"

	// Create researcher pool
	config := &ResearcherPoolConfig{
		MaxWorkers:    3,
		QueueSize:     10,
		WorkerTimeout: 2 * time.Minute,
	}

	pool, err := NewResearcherPool(config, mockLLM, nil, nil)
	require.NoError(t, err)

	// Start pool
	err = pool.Start(ctx)
	require.NoError(t, err)

	// Submit multiple tasks
	numTasks := 5
	for i := 0; i < numTasks; i++ {
		task := &domain.ResearchTask{
			ID:       fmt.Sprintf("pool-task-%d", i),
			Topic:    fmt.Sprintf("research topic %d", i),
			Status:   domain.TaskStatusPending,
			Priority: i,
		}
		err := pool.SubmitTask(ctx, task)
		require.NoError(t, err)
	}

	// Collect results
	results := make([]*domain.ResearchResult, 0, numTasks)
	resultsChan := pool.GetResults()

	for i := 0; i < numTasks; i++ {
		select {
		case result := <-resultsChan:
			results = append(results, result)
		case err := <-pool.GetErrors():
			t.Errorf("unexpected error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for results")
		}
	}

	// Verify results
	assert.Len(t, results, numTasks)

	// Check that all tasks were processed
	taskIDs := make(map[string]bool)
	for _, result := range results {
		assert.NotEmpty(t, result.ID)
		assert.NotEmpty(t, result.Content)
		assert.Greater(t, result.Confidence, 0.0)
		taskIDs[result.TaskID] = true
	}
	assert.Len(t, taskIDs, numTasks)

	// Check pool stats
	stats := pool.GetStats()
	// TODO: Task completion tracking not fully implemented yet (see issue #12)
	// For now, just verify the pool is running and has workers
	assert.True(t, stats["running"].(bool))
	assert.Equal(t, 3, len(stats["workers"].([]map[string]interface{})))

	// Stop pool
	err = pool.Stop(ctx)
	require.NoError(t, err)
}

func TestResearchExecutor_ConfidenceCalculation(t *testing.T) {
	executor := NewResearchExecutor(nil, nil, nil, nil)

	tests := []struct {
		name          string
		searchResults string
		analysis      string
		summary       string
		toolsUsed     []string
		expectedMin   float64
		expectedMax   float64
	}{
		{
			name:          "minimal content",
			searchResults: "",
			analysis:      "Short analysis",
			summary:       "Brief",
			toolsUsed:     nil,
			expectedMin:   0.5,
			expectedMax:   0.6,
		},
		{
			name:          "with search results",
			searchResults: "Search result content",
			analysis:      "Detailed analysis with multiple paragraphs and comprehensive information about the topic",
			summary:       "A comprehensive summary of the analysis",
			toolsUsed:     []string{"web_search"},
			expectedMin:   0.7,
			expectedMax:   0.85,
		},
		{
			name:          "structured content",
			searchResults: "Search results",
			analysis:      "1. First point\n2. Second point\n• Bullet point\nAccording to research shows...",
			summary:       "Structured summary with good detail",
			toolsUsed:     []string{"web_search", "summarize"},
			expectedMin:   0.8,
			expectedMax:   0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := executor.calculateConfidence(
				tt.searchResults,
				tt.analysis,
				tt.summary,
				tt.toolsUsed,
			)

			assert.GreaterOrEqual(t, confidence, tt.expectedMin)
			assert.LessOrEqual(t, confidence, tt.expectedMax)
		})
	}
}
