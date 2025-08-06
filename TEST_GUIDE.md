# Test Suite Guide

## Overview
The Open Research Agent test suite provides comprehensive testing for all components of the system.

## Running Tests

### Run All Tests
```bash
make test
```

### Run Unit Tests Only
```bash
make test-unit
```

### Run Integration Tests
```bash
make test-integration
```

### Generate Coverage Report
```bash
make coverage
```

### Check Coverage Threshold (70%)
```bash
make test-coverage-check
```

### Watch Mode (requires entr)
```bash
make test-watch
```

## Test Structure

### Unit Tests
- `pkg/domain/models_test.go` - Domain model tests
- `pkg/state/state_test.go` - State management tests  
- `pkg/workflow/graph_test.go` - Workflow orchestration tests
- `pkg/llm/ollama_test.go` - LLM client tests

### Test Helpers
- `internal/testutil/helpers.go` - Common test utilities
- `internal/testutil/mocks.go` - Mock implementations

### Test Configuration
- `configs/test.yaml` - Test environment configuration

## Coverage Goals
- Unit Test Coverage: 70% minimum
- Integration Test Coverage: 50% minimum
- Critical Path Coverage: 90% minimum

## Writing Tests

### Test Naming Convention
- Test functions: `TestFunctionName`
- Sub-tests: `TestFunctionName/SubTestName`
- Test files: `*_test.go`

### Test Patterns
```go
func TestExample(t *testing.T) {
    // Arrange
    ctx := testutil.NewTestContext(t)
    mock := testutil.NewMockLLMClient()
    
    // Act
    result, err := FunctionUnderTest(ctx, mock)
    
    // Assert
    testutil.AssertNoError(t, err, "unexpected error")
    testutil.AssertEqual(t, expected, result, "result mismatch")
}
```

### Mock Usage
```go
// Create mock LLM client
mockLLM := testutil.NewMockLLMClient()
mockLLM.Responses["query"] = "response"

// Create mock tool registry
mockTools := testutil.NewMockToolRegistry()
mockTools.Register(&testutil.MockTool{
    ToolName: "test-tool",
    ExecuteFunc: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        return "result", nil
    },
})
```

## CI/CD Integration
Tests are automatically run on:
- Pull requests
- Commits to main branch
- Release builds

## Debugging Tests
```bash
# Run tests with verbose output
go test -v ./...

# Run specific test
go test -v -run TestSpecificFunction ./pkg/...

# Debug with delve
dlv test ./pkg/domain
```

## Performance Testing
```bash
# Run benchmarks
make bench

# Profile tests
go test -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof
```