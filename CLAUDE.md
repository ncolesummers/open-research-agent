# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Open Research Agent is a Go-based AI research system implementing a supervisor-researcher pattern for parallel research execution. It uses OpenTelemetry for observability and supports Ollama as the LLM provider.

## Key Architecture Components

### Core Domain Design
- **Workflow Engine**: Graph-based orchestration in `pkg/workflow/graph.go` using a state machine pattern with phases (start, clarification, planning, research, synthesis, complete)
- **State Management**: Thread-safe state management in `pkg/state/` with GraphState containing tasks, results, and messages
- **LLM Integration**: Abstracted LLM interface in `pkg/domain/interfaces.go` with Ollama implementation in `pkg/llm/ollama.go`
- **Tool System**: Registry pattern for tools in `pkg/tools/registry.go` with extensible Tool interface

### Phase-Based Research Flow
1. **Start** → Initial state setup
2. **Clarification** → Determine if query needs refinement
3. **Planning** → Generate research tasks
4. **Research** → Execute tasks in parallel (up to MaxConcurrency)
5. **Synthesis** → Compile results into report
6. **Complete** → Final state with report generation

## Essential Commands

### Development
```bash
# Build the application
make build

# Run with hot reload (requires air)
make dev

# Run with specific config
./bin/ora -config configs/default.yaml

# Run with CLI query
./bin/ora -query "Your research question here"
```

### Testing
```bash
# Run all tests with coverage
make test

# Run unit tests only (faster, no integration tests)
make test-unit

# Run specific test
go test -v -run TestResearchGraph_Execute ./pkg/workflow/...

# Check coverage threshold (70%)
make test-coverage-check

# Generate HTML coverage report
make coverage
```

### Code Quality
```bash
# Format code and imports
make fmt

# Run linter (golangci-lint)
make lint

# Run go vet
make vet
```

### Ollama Setup
```bash
# Start Ollama server
make ollama-start

# Pull default model (llama3.2)
make ollama-pull
```

## Configuration

The system uses YAML configuration files in `configs/`:
- `default.yaml` - Production settings with observability enabled
- `test.yaml` - Test settings with observability disabled

Key configuration sections:
- **ollama**: LLM settings (model, temperature, max_tokens)
- **research**: Workflow parameters (max_iterations, max_concurrency, max_depth)
- **observability**: OpenTelemetry settings for tracing, metrics, logging
- **tools**: Configuration for web_search, think, summarize tools

## Testing Strategy

### Test Utilities
- **Mocks**: `internal/testutil/mocks.go` provides MockLLMClient, MockToolRegistry, MockTool
- **Helpers**: `internal/testutil/helpers.go` provides test object creation and assertions
- **Table-driven tests**: Domain models use table-driven testing patterns

### Test Coverage Areas
- **Domain Models**: Validation and state transitions
- **State Management**: Thread-safe concurrent access testing
- **Workflow**: Graph execution, error handling, context cancellation
- **LLM Client**: HTTP mocking with httptest for Ollama API

## Error Handling Patterns

The codebase uses wrapped errors with context:
```go
return fmt.Errorf("clarify node failed: %w", err)
```

LLM errors propagate through the workflow with proper error chains for debugging.

## Observability

OpenTelemetry integration provides:
- **Tracing**: Distributed tracing with OTLP export to Jaeger
- **Metrics**: Prometheus metrics on port 2223
- **Structured Logging**: JSON formatted logs with context propagation

Key metrics tracked:
- Research request latency
- Task execution duration
- LLM token usage
- Workflow phase transitions

## Development Workflow

1. **Feature Development**:
   - Create/modify domain models in `pkg/domain/`
   - Implement business logic in appropriate package
   - Add workflow nodes if needed in `pkg/workflow/`
   - Write tests alongside implementation

2. **Testing**:
   - Unit tests go in same package with `_test.go` suffix
   - Use mocks from `internal/testutil/` for dependencies
   - Run `make test-unit` frequently during development
   - Ensure 70% coverage before committing

3. **Integration**:
   - Test with local Ollama instance
   - Use `configs/test.yaml` for isolated testing
   - Monitor with observability tools when enabled

## Common Troubleshooting

### Ollama Connection Issues
- Ensure Ollama is running: `ollama serve`
- Check base URL in config matches Ollama endpoint
- Default is `http://localhost:11434`

### Test Failures
- Mock LLM responses are configured in test setup
- Check `MockLLMClient.Responses` map for expected queries
- Context cancellation tests may pass if execution completes quickly

### Workflow Execution
- Max iterations limit prevents infinite loops
- Each phase has specific transition logic
- State mutations are thread-safe with mutex protection

## Code Style Guidelines

- Use table-driven tests for comprehensive test coverage
- Wrap errors with context using `fmt.Errorf` with `%w` verb
- Keep interfaces small and focused (domain.LLMClient, domain.Tool)
- Use mutex for thread-safe operations in shared state
- Prefer composition over inheritance in struct design