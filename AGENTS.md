# Repository Guidelines

## Project Structure & Module Organization
- `cmd/ora/`: CLI entrypoint (build output in `bin/ora`).
- `pkg/`: Public packages — `workflow/`, `llm/`, `observability/`, `domain/`, `config/`, `tools/`, `api/`, `state/`.
- `internal/`: Private helpers — `research/`, `utils/`, `testutil/`.
- `configs/`: Runtime configs (`default.yaml`, `test.yaml`).
- `bin/`: Built binaries. Other docs: `DESIGN.md`, `OBSERVABILITY.md`, `TEST_GUIDE.md`.

## Build, Test, and Development Commands
- `make init`: One‑time setup (deps, tools, pull default Ollama model).
- `make build` / `make run`: Build binary to `bin/ora` / run it.
- `make dev`: Hot reload (requires `air`).
- `make test` | `make test-unit` | `make test-integration`: Run tests; integration tests match `-run Integration`.
- `make coverage` | `make test-coverage-check`: Generate HTML report / enforce 70% total.
- `make fmt` | `make lint` | `make vet`: Format, lint (golangci-lint), vet.
- `make docker-build` | `make docker-run`: Container build/run.
Example: `OLLAMA_BASE_URL=http://localhost:11434 make run`.

## Coding Style & Naming Conventions
- Language: Go 1.24. Use `gofmt -s` and `goimports` (run `make fmt`).
- Lint: `golangci-lint run` (via `make lint`). Avoid unused exports; prefer small, focused packages.
- Names: packages lowercase; files lowercase; exported identifiers UpperCamelCase; unexported lowerCamelCase.
- Config: prefer typed structs in `pkg/config`; default values live in `configs/default.yaml`.

## Testing Guidelines
- Frameworks: Go `testing` + `testify`. Helpers/mocks in `internal/testutil/`.
- Structure: unit tests near code (e.g., `pkg/workflow/graph_test.go`).
- Naming: files `*_test.go`; tests `TestXxx`; subtests `t.Run("Case")`.
- Coverage: target ≥70% unit; use `make test-coverage-check`. See `TEST_GUIDE.md` for patterns and examples.

## Commit & Pull Request Guidelines
- Commits: Prefer Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`). Use imperative mood and scope when useful.
- Branches: `feature/<slug>`, `fix/<slug>`, `chore/<slug>`.
- PRs: clear description, linked issue, test plan/outputs, and any telemetry notes (links to traces/metrics if applicable). Include config changes and migration notes.

## Security & Configuration Tips
- Local LLM: start/pull with `make ollama-start` and `make ollama-pull` (default model `llama3.2`).
- Runtime config: tune `configs/default.yaml`; env vars supported (see `make help`): `OLLAMA_BASE_URL`, `OLLAMA_MODEL`, `API_PORT`, `ENVIRONMENT`.
- Observability: OpenTelemetry tracing/metrics configurable via `observability.*` in config; see `OBSERVABILITY.md`.

