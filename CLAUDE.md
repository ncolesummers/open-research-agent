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

This project practices Observability-Driven Development (ODD). Any code change Claude produces MUST consider telemetry impact.

### ODD Principles (apply these to every change)

1. Design for Observability – emit spans/metrics/logs as you implement logic (not afterward)
2. Test Through Telemetry – write/extend tests that assert spans + metrics for new behavior
3. Deploy with Confidence – telemetry changes are backwards-compatible & low cardinality

### Core Telemetry Components

| Concern | Mechanism               | Conventions                                                                                                     |
| ------- | ----------------------- | --------------------------------------------------------------------------------------------------------------- |
| Traces  | OpenTelemetry spans     | Root: `research.request`; Workflow nodes: `workflow.node.<name>`; LLM: `llm.chat`; Tools: `tool.<tool_name>`    |
| Metrics | OTel Meter / Prometheus | Names match `OBSERVABILITY.md` spec; use histograms for durations, counters for totals, up/down for concurrency |
| Logs    | Structured JSON         | Always include `trace_id`, `span_id`, `component`, minimal stable keys                                          |
| Context | OTel baggage            | Keys: `request.id`, `user.id`, `research.type`, `complexity`, `model`                                           |

### Required Span Attributes

| Span Type                 | Required Attributes                                                                          |
| ------------------------- | -------------------------------------------------------------------------------------------- |
| `research.request` (root) | `request.id`, `user.id`, `query.length`, `model`, `complexity`                               |
| `workflow.node.*`         | `node.name`, `phase`, `status`, `iteration`                                                  |
| `llm.chat`                | `llm.model`, `llm.provider`, `llm.temperature`, `llm.message_count`, token usage (post-call) |
| `tool.*`                  | `tool.name`, `tool.status`, `tool.duration_seconds`                                          |

If adding a new span category, document it in `OBSERVABILITY.md` and keep attribute cardinality bounded.

### Metrics (authoritative list lives in `OBSERVABILITY.md`)

Use existing instruments where possible. Only add a new metric if it answers a concrete question that none of the existing ones can.

Key instruments that SHOULD be touched (never silently removed):

- `research_request_duration_seconds` (histogram)
- `workflow_node_duration_seconds` (histogram)
- `llm_request_duration_seconds` (histogram)
- `llm_tokens_used_total` (counter)
- `tool_invocations_total` (counter)
- `tool_duration_seconds` (histogram)
- `active_researchers` / `active_research_requests` (up-down counter / gauge style)

Cardinality guardrails:

- Labels MUST be from controlled sets (e.g. `status` in {`success`,`error`})
- Do NOT include raw user input, task IDs, or long dynamic strings as metric labels

### Logging

Structured JSON only. When adding a log line:

- Include `component` (package or logical subsystem)
- Use consistent `severity` (one of DEBUG, INFO, WARN, ERROR)
- Prefer span attributes over duplicating data in logs—avoid redundancy
- Never log secrets, auth tokens, or full LLM raw prompts (summarize if needed)

### Adaptive Sampling

High-volume spans (e.g., fast internal nodes) should respect adaptive sampling logic. If you introduce _frequently executed_ spans, ensure they can be dropped without breaking semantic meaning of traces.

### Baggage Usage

Only put routing / classification context (low cardinality) into baggage. Do NOT put free-form text, prompts, or large IDs in baggage.

### Writing / Updating Tests for Telemetry

When adding or changing behavior:

1. Use `tracetest.SpanRecorder` to assert span presence + attributes
2. Use metric reader (test SDK) to confirm new/updated metric emission
3. For error paths, assert span status = Error and error metric increments
4. Avoid asserting exact timing values—assert presence & reasonable bounds

Minimal example pattern (pseudocode):

```go
spans := spanRecorder.Ended()
assertSpan(t, spans, spanAssert{
   Name: "workflow.node.planning",
   Attrs: map[string]any{"node.name": "planning", "status": "success"},
})

metrics := collect(metricReader)
assertHistogramObserved(metrics, "workflow_node_duration_seconds", labelSet{"node_name":"planning"})
```

### Adding New Workflow Nodes or Tools

You MUST:

1. Wrap execution with `Telemetry.InstrumentWorkflowNode` (or equivalent)
2. Provide deterministic `node.name`
3. Emit status attribute (`success` / `error`)
4. Record duration and increment relevant counters
5. Extend tests to cover normal + error scenarios

### Pull Requests Checklist (Claude MUST self-verify)

- [ ] All new logic instrumented (spans + metrics OR rationale documented)
- [ ] No high-cardinality labels introduced
- [ ] Span / metric names conform to conventions
- [ ] Tests assert telemetry for new behavior
- [ ] OBSERVABILITY.md updated if schemas changed
- [ ] No secrets or large payloads logged
- [ ] Error paths record span error + increment error counter

### Local Observability Stack (optional during dev)

If modifying telemetry semantics, run the full stack (compose file may be `docker-compose.observability.yml`) and visually confirm spans/metrics.

### Common Anti-Patterns (AVOID)

- Creating duplicate metrics for same concept (reuse labels instead)
- Logging full LLM prompts/token lists verbatim
- Adding user-controlled strings as label values
- Swallowing errors without span status update

When in doubt: prefer fewer, richer spans over many ultra-granular noisy spans.

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
