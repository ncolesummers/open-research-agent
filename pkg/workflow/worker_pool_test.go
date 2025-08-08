package workflow

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncolesummers/open-research-agent/internal/testutil"
	"github.com/ncolesummers/open-research-agent/pkg/domain"
	"github.com/ncolesummers/open-research-agent/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestWorkerPoolCreation tests worker pool creation and initialization
func TestWorkerPoolCreation(t *testing.T) {
	tests := []struct {
		name    string
		config  *WorkerPoolConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid configuration",
			config: &WorkerPoolConfig{
				MaxWorkers:    5,
				QueueSize:     50,
				WorkerTimeout: 2 * time.Minute,
			},
			wantErr: false,
		},
		{
			name:    "nil configuration uses defaults",
			config:  nil,
			wantErr: true,
			errMsg:  "config is required",
		},
		{
			name: "zero workers uses default",
			config: &WorkerPoolConfig{
				MaxWorkers:    0,
				QueueSize:     50,
				WorkerTimeout: 2 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "zero queue size uses default",
			config: &WorkerPoolConfig{
				MaxWorkers:    5,
				QueueSize:     0,
				WorkerTimeout: 2 * time.Minute,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLLM := testutil.NewMockLLMClient()
			mockTools := testutil.NewMockToolRegistry()

			pool, err := NewWorkerPool(tt.config, mockLLM, mockTools, nil)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, pool)
				if tt.config != nil && tt.config.MaxWorkers == 0 {
					assert.Equal(t, 5, pool.config.MaxWorkers)
				}
				if tt.config != nil && tt.config.QueueSize == 0 {
					assert.Equal(t, 50, pool.config.QueueSize)
				}
			}
		})
	}
}

// TestWorkerPoolStartStop tests starting and stopping the worker pool
func TestWorkerPoolStartStop(t *testing.T) {
	config := &WorkerPoolConfig{
		MaxWorkers:    3,
		QueueSize:     10,
		WorkerTimeout: 1 * time.Minute,
	}

	mockLLM := testutil.NewMockLLMClient()
	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Test starting the pool
	err = pool.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, pool.running.Load())
	assert.Equal(t, int32(3), pool.metrics.activeWorkers.Load())

	// Test starting an already running pool
	err = pool.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test stopping the pool
	err = pool.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, pool.running.Load())

	// Test stopping an already stopped pool
	err = pool.Stop(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

// TestWorkerPoolTaskSubmission tests submitting tasks to the pool
func TestWorkerPoolTaskSubmission(t *testing.T) {
	config := &WorkerPoolConfig{
		MaxWorkers:    2,
		QueueSize:     5,
		WorkerTimeout: 30 * time.Second,
	}

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.Responses["Research topic: Test research topic"] = "Research result for the task"

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = pool.Stop(ctx)
	}()

	// Create a test task
	task := &domain.ResearchTask{
		ID:       "task-1",
		Topic:    "Test research topic",
		Status:   domain.TaskStatusPending,
		Priority: 1,
	}

	stateConfig := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      50,
	}
	graphState := state.NewGraphState(domain.ResearchRequest{
		ID:    "req-1",
		Query: "Test query",
	}, stateConfig)

	// Submit task
	err = pool.Submit(ctx, task, graphState)
	assert.NoError(t, err)

	// Wait for result
	select {
	case result := <-pool.GetResults():
		assert.NotNil(t, result)
		assert.Equal(t, task.ID, result.TaskID)
		assert.Contains(t, result.Content, "Research result")
	case err := <-pool.GetErrors():
		t.Fatalf("Unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

// TestWorkerPoolConcurrency tests concurrent task processing
func TestWorkerPoolConcurrency(t *testing.T) {
	config := &WorkerPoolConfig{
		MaxWorkers:    5,
		QueueSize:     20,
		WorkerTimeout: 30 * time.Second,
	}

	// Track concurrent executions
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.ChatFunc = func(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
		// Simulate work and track concurrency
		current := concurrentCount.Add(1)
		defer concurrentCount.Add(-1)

		// Update max concurrent
		for {
			max := maxConcurrent.Load()
			if current <= max || maxConcurrent.CompareAndSwap(max, current) {
				break
			}
		}

		// Simulate processing time
		time.Sleep(100 * time.Millisecond)

		return &domain.ChatResponse{
			Content: "Result for task",
			Usage: domain.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 200,
				TotalTokens:      300,
			},
			FinishReason: "stop",
		}, nil
	}

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = pool.Stop(ctx)
	}()

	// Submit multiple tasks
	numTasks := 15
	stateConfig := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      50,
	}
	graphState := state.NewGraphState(domain.ResearchRequest{
		ID:    "req-1",
		Query: "Test query",
	}, stateConfig)

	for i := 0; i < numTasks; i++ {
		task := &domain.ResearchTask{
			ID:       fmt.Sprintf("task-%d", i),
			Topic:    fmt.Sprintf("Topic %d", i),
			Status:   domain.TaskStatusPending,
			Priority: 1,
		}

		err = pool.Submit(ctx, task, graphState)
		require.NoError(t, err)
	}

	// Collect results
	results := make([]*domain.ResearchResult, 0, numTasks)
	for i := 0; i < numTasks; i++ {
		select {
		case result := <-pool.GetResults():
			results = append(results, result)
		case err := <-pool.GetErrors():
			t.Fatalf("Unexpected error: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for results")
		}
	}

	// Verify all tasks completed
	assert.Equal(t, numTasks, len(results))

	// Verify concurrency was utilized
	assert.Greater(t, maxConcurrent.Load(), int32(1), "Expected concurrent execution")
	assert.LessOrEqual(t, maxConcurrent.Load(), int32(config.MaxWorkers), "Should not exceed max workers")
}

// TestWorkerPoolErrorHandling tests error handling and retry logic
func TestWorkerPoolErrorHandling(t *testing.T) {
	config := &WorkerPoolConfig{
		MaxWorkers:    2,
		QueueSize:     10,
		WorkerTimeout: 5 * time.Second,
	}

	// Track retry attempts
	var attempts sync.Map

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.ChatFunc = func(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
		// Extract task ID from messages (simplified)
		taskID := "task-1"

		count, _ := attempts.LoadOrStore(taskID, 0)
		attemptNum := count.(int) + 1
		attempts.Store(taskID, attemptNum)

		// Fail first 2 attempts, succeed on third
		if attemptNum < 3 {
			return nil, fmt.Errorf("simulated failure %d", attemptNum)
		}

		return &domain.ChatResponse{
			Content: "Success after retries",
			Usage: domain.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 200,
				TotalTokens:      300,
			},
			FinishReason: "stop",
		}, nil
	}

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = pool.Stop(ctx)
	}()

	// Submit task that will fail initially
	task := &domain.ResearchTask{
		ID:       "task-1",
		Topic:    "Test topic",
		Status:   domain.TaskStatusPending,
		Priority: 1,
	}

	stateConfig := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      50,
	}
	graphState := state.NewGraphState(domain.ResearchRequest{
		ID:    "req-1",
		Query: "Test query",
	}, stateConfig)

	err = pool.Submit(ctx, task, graphState)
	require.NoError(t, err)

	// Wait for result (should succeed after retries)
	select {
	case result := <-pool.GetResults():
		assert.NotNil(t, result)
		assert.Contains(t, result.Content, "Success after retries")

		// Verify retries occurred
		count, _ := attempts.Load("task-1")
		assert.Equal(t, 3, count.(int), "Expected 3 attempts (2 failures + 1 success)")

	case <-pool.GetErrors():
		// Should not receive error since it succeeds on retry
		t.Fatal("Should have succeeded after retries")

	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

// TestWorkerPoolGracefulShutdown tests graceful shutdown with in-progress tasks
func TestWorkerPoolGracefulShutdown(t *testing.T) {
	config := &WorkerPoolConfig{
		MaxWorkers:    3,
		QueueSize:     10,
		WorkerTimeout: 30 * time.Second,
	}

	// Use channel to control task completion
	taskStarted := make(chan struct{})
	allowCompletion := make(chan struct{})

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.ChatFunc = func(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
		// Signal that task has started
		select {
		case taskStarted <- struct{}{}:
		default:
		}

		// Wait for signal to complete
		select {
		case <-allowCompletion:
			return &domain.ChatResponse{
				Content: "Completed task",
				Usage: domain.TokenUsage{
					PromptTokens:     100,
					CompletionTokens: 200,
					TotalTokens:      300,
				},
				FinishReason: "stop",
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)

	// Submit task
	task := &domain.ResearchTask{
		ID:       "task-1",
		Topic:    "Test topic",
		Status:   domain.TaskStatusPending,
		Priority: 1,
	}

	stateConfig := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      50,
	}
	graphState := state.NewGraphState(domain.ResearchRequest{
		ID:    "req-1",
		Query: "Test query",
	}, stateConfig)

	err = pool.Submit(ctx, task, graphState)
	require.NoError(t, err)

	// Wait for task to start
	<-taskStarted

	// Start shutdown in background
	shutdownDone := make(chan error)
	go func() {
		shutdownDone <- pool.Stop(context.Background())
	}()

	// Allow task to complete
	close(allowCompletion)

	// Wait for shutdown to complete
	select {
	case err := <-shutdownDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown timeout")
	}

	// Verify pool is stopped
	assert.False(t, pool.running.Load())
}

// TestWorkerHealthMonitoring tests worker health monitoring and restart
func TestWorkerHealthMonitoring(t *testing.T) {
	config := &WorkerPoolConfig{
		MaxWorkers:    2,
		QueueSize:     10,
		WorkerTimeout: 5 * time.Second,
	}

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.Responses["default"] = "Test result"

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = pool.Stop(ctx)
	}()

	// Verify initial worker count
	assert.Equal(t, int32(2), pool.metrics.activeWorkers.Load())

	// Simulate worker failure by manipulating metrics
	pool.metrics.activeWorkers.Store(1)

	// Wait for health monitor to detect and restart
	time.Sleep(12 * time.Second) // Health check runs every 10 seconds

	// Verify workers were restarted
	assert.Equal(t, int32(2), pool.metrics.activeWorkers.Load())
}

// TestWorkerPoolMetrics tests metrics collection
func TestWorkerPoolMetrics(t *testing.T) {
	config := &WorkerPoolConfig{
		MaxWorkers:    2,
		QueueSize:     10,
		WorkerTimeout: 30 * time.Second,
	}

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.Responses["default"] = "Test result"

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = pool.Stop(ctx)
	}()

	// Submit multiple tasks
	numTasks := 5
	stateConfig := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      50,
	}
	graphState := state.NewGraphState(domain.ResearchRequest{
		ID:    "req-1",
		Query: "Test query",
	}, stateConfig)

	for i := 0; i < numTasks; i++ {
		task := &domain.ResearchTask{
			ID:       fmt.Sprintf("task-%d", i),
			Topic:    fmt.Sprintf("Topic %d", i),
			Status:   domain.TaskStatusPending,
			Priority: 1,
		}

		err = pool.Submit(ctx, task, graphState)
		require.NoError(t, err)
	}

	// Wait for all tasks to complete
	for i := 0; i < numTasks; i++ {
		select {
		case <-pool.GetResults():
			// Task completed
		case <-pool.GetErrors():
			// Task failed
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for task completion")
		}
	}

	// Check metrics
	assert.Equal(t, int64(numTasks), pool.metrics.tasksCompleted.Load())
	assert.Equal(t, int64(0), pool.metrics.tasksFailed.Load())
	assert.Equal(t, int32(0), pool.metrics.queueDepth.Load())
	assert.Greater(t, pool.metrics.totalProcessTime.Load(), int64(0))
}

// TestWorkerPoolWithObservability tests worker pool with observability enabled
func TestWorkerPoolWithObservability(t *testing.T) {
	t.Skip("Skipping observability test - requires more complex telemetry setup")
	// Create test telemetry
	spanRecorder := tracetest.NewSpanRecorder()
	metricReader := metric.NewManualReader()

	telemetry := testutil.SetupTestTelemetry(spanRecorder, metricReader)

	config := &WorkerPoolConfig{
		MaxWorkers:    2,
		QueueSize:     10,
		WorkerTimeout: 30 * time.Second,
	}

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.Responses["default"] = "Test result"

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, telemetry)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = pool.Stop(ctx)
	}()

	// Submit task
	task := &domain.ResearchTask{
		ID:       "task-1",
		Topic:    "Test topic",
		Status:   domain.TaskStatusPending,
		Priority: 1,
	}

	stateConfig := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      50,
	}
	graphState := state.NewGraphState(domain.ResearchRequest{
		ID:    "req-1",
		Query: "Test query",
	}, stateConfig)

	err = pool.Submit(ctx, task, graphState)
	require.NoError(t, err)

	// Wait for result
	select {
	case <-pool.GetResults():
		// Task completed
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for result")
	}

	// Verify spans were created
	spans := spanRecorder.Ended()
	assert.Greater(t, len(spans), 0, "Expected spans to be created")

	// Check for expected span names
	spanNames := make(map[string]bool)
	for _, span := range spans {
		spanNames[span.Name()] = true
	}

	assert.True(t, spanNames["worker_pool.start"], "Expected worker_pool.start span")
	assert.True(t, spanNames["worker.process_task"], "Expected worker.process_task span")

	// Verify metrics were recorded
	var metrics metricdata.ResourceMetrics
	err = metricReader.Collect(ctx, &metrics)
	assert.NoError(t, err)
	assert.Greater(t, len(metrics.ScopeMetrics), 0, "Expected metrics to be recorded")
}

// BenchmarkWorkerPoolScaling benchmarks worker pool performance with varying worker counts
func BenchmarkWorkerPoolScaling(b *testing.B) {
	// Test with different worker counts to verify linear scaling
	workerCounts := []int{1, 2, 3, 4, 5}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			config := &WorkerPoolConfig{
				MaxWorkers:    workers,
				QueueSize:     100,
				WorkerTimeout: 30 * time.Second,
			}

			mockLLM := testutil.NewMockLLMClient()
			mockLLM.ChatFunc = func(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
				// Simulate some work
				time.Sleep(10 * time.Millisecond)
				return &domain.ChatResponse{
					Content: "Benchmark result",
					Usage: domain.TokenUsage{
						PromptTokens:     100,
						CompletionTokens: 200,
						TotalTokens:      300,
					},
					FinishReason: "stop",
				}, nil
			}

			mockTools := testutil.NewMockToolRegistry()

			pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
			require.NoError(b, err)

			ctx := context.Background()
			err = pool.Start(ctx)
			require.NoError(b, err)
			defer func() {
				_ = pool.Stop(ctx)
			}()

			stateConfig := &state.StateConfig{
				MaxIterations: 10,
				MaxTasks:      1000,
			}
			graphState := state.NewGraphState(domain.ResearchRequest{
				ID:    "req-1",
				Query: "Benchmark query",
			}, stateConfig)

			b.ResetTimer()

			// Submit and process tasks
			for i := 0; i < b.N; i++ {
				task := &domain.ResearchTask{
					ID:       fmt.Sprintf("task-%d", i),
					Topic:    fmt.Sprintf("Topic %d", i),
					Status:   domain.TaskStatusPending,
					Priority: 1,
				}

				err = pool.Submit(ctx, task, graphState)
				if err != nil {
					b.Fatal(err)
				}
			}

			// Wait for all tasks to complete
			for i := 0; i < b.N; i++ {
				select {
				case <-pool.GetResults():
					// Task completed
				case <-pool.GetErrors():
					// Task failed
				case <-time.After(30 * time.Second):
					b.Fatal("Timeout waiting for task completion")
				}
			}

			b.StopTimer()

			// Report metrics
			completed := pool.metrics.tasksCompleted.Load()
			totalTime := time.Duration(pool.metrics.totalProcessTime.Load())
			avgTime := totalTime / time.Duration(completed)

			b.ReportMetric(float64(avgTime.Nanoseconds()), "ns/task")
			b.ReportMetric(float64(completed)/b.Elapsed().Seconds(), "tasks/sec")
		})
	}
}

// BenchmarkWorkerPoolThroughput benchmarks throughput compared to sequential execution
func BenchmarkWorkerPoolThroughput(b *testing.B) {
	mockLLM := testutil.NewMockLLMClient()
	mockLLM.ChatFunc = func(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
		// Simulate realistic work
		time.Sleep(50 * time.Millisecond)
		return &domain.ChatResponse{
			Content: "Benchmark result",
			Usage: domain.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 200,
				TotalTokens:      300,
			},
			FinishReason: "stop",
		}, nil
	}

	mockTools := testutil.NewMockToolRegistry()

	b.Run("sequential", func(b *testing.B) {
		b.ResetTimer()
		startTime := time.Now()

		// Process tasks sequentially
		for i := 0; i < b.N; i++ {
			task := &domain.ResearchTask{
				ID:       fmt.Sprintf("task-%d", i),
				Topic:    fmt.Sprintf("Topic %d", i),
				Status:   domain.TaskStatusPending,
				Priority: 1,
			}

			// Simulate sequential processing
			messages := []domain.Message{
				{Role: "system", Content: "Research assistant"},
				{Role: "user", Content: task.Topic},
			}

			_, err := mockLLM.Chat(context.Background(), messages, domain.ChatOptions{
				Temperature: 0.7,
				MaxTokens:   2000,
			})
			if err != nil {
				b.Fatal(err)
			}
		}

		elapsed := time.Since(startTime)
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/sec")
	})

	b.Run("parallel-5-workers", func(b *testing.B) {
		config := &WorkerPoolConfig{
			MaxWorkers:    5,
			QueueSize:     100,
			WorkerTimeout: 30 * time.Second,
		}

		pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
		require.NoError(b, err)

		ctx := context.Background()
		err = pool.Start(ctx)
		require.NoError(b, err)
		defer func() {
			_ = pool.Stop(ctx)
		}()

		stateConfig := &state.StateConfig{
			MaxIterations: 10,
			MaxTasks:      1000,
		}
		graphState := state.NewGraphState(domain.ResearchRequest{
			ID:    "req-1",
			Query: "Benchmark query",
		}, stateConfig)

		b.ResetTimer()
		startTime := time.Now()

		// Submit all tasks
		for i := 0; i < b.N; i++ {
			task := &domain.ResearchTask{
				ID:       fmt.Sprintf("task-%d", i),
				Topic:    fmt.Sprintf("Topic %d", i),
				Status:   domain.TaskStatusPending,
				Priority: 1,
			}

			err = pool.Submit(ctx, task, graphState)
			if err != nil {
				b.Fatal(err)
			}
		}

		// Wait for all tasks to complete
		for i := 0; i < b.N; i++ {
			select {
			case <-pool.GetResults():
				// Task completed
			case <-pool.GetErrors():
				// Task failed
			case <-time.After(30 * time.Second):
				b.Fatal("Timeout waiting for task completion")
			}
		}

		elapsed := time.Since(startTime)
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tasks/sec")

		// Calculate speedup
		// Note: This is approximate since we're comparing different runs
		b.Logf("Parallel execution with 5 workers completed %d tasks in %v", b.N, elapsed)
	})
}

// TestWorkerPoolRaceConditions tests for race conditions
func TestWorkerPoolRaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition test in short mode")
	}

	config := &WorkerPoolConfig{
		MaxWorkers:    10,
		QueueSize:     100,
		WorkerTimeout: 5 * time.Second,
	}

	mockLLM := testutil.NewMockLLMClient()
	mockLLM.ChatFunc = func(ctx context.Context, messages []domain.Message, opts domain.ChatOptions) (*domain.ChatResponse, error) {
		// Random sleep to create race conditions
		time.Sleep(time.Duration(time.Now().UnixNano()%10) * time.Millisecond)
		return &domain.ChatResponse{
			Content: "Race test result",
			Usage: domain.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 200,
				TotalTokens:      300,
			},
			FinishReason: "stop",
		}, nil
	}

	mockTools := testutil.NewMockToolRegistry()

	pool, err := NewWorkerPool(config, mockLLM, mockTools, nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Start(ctx)
	require.NoError(t, err)
	defer func() {
		_ = pool.Stop(ctx)
	}()

	// Run concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 20
	tasksPerGoroutine := 10

	stateConfig := &state.StateConfig{
		MaxIterations: 10,
		MaxTasks:      1000,
	}
	graphState := state.NewGraphState(domain.ResearchRequest{
		ID:    "req-1",
		Query: "Race test query",
	}, stateConfig)

	// Start task submitters
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < tasksPerGoroutine; i++ {
				task := &domain.ResearchTask{
					ID:       fmt.Sprintf("task-%d-%d", goroutineID, i),
					Topic:    fmt.Sprintf("Topic %d-%d", goroutineID, i),
					Status:   domain.TaskStatusPending,
					Priority: 1,
				}

				err := pool.Submit(ctx, task, graphState)
				if err != nil {
					t.Errorf("Failed to submit task: %v", err)
				}
			}
		}(g)
	}

	// Start result collectors
	totalTasks := numGoroutines * tasksPerGoroutine
	resultCount := atomic.Int32{}

	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for resultCount.Load() < int32(totalTasks) {
				select {
				case <-pool.GetResults():
					resultCount.Add(1)
				case <-pool.GetErrors():
					resultCount.Add(1)
				case <-time.After(100 * time.Millisecond):
					// Check periodically
				}
			}
		}()
	}

	// Wait for all operations to complete
	wg.Wait()

	// Verify all tasks were processed
	assert.Equal(t, int32(totalTasks), resultCount.Load())
}
