# Open Research Agent - Design Document

## Executive Summary

Open Research Agent is a Go-based AI research system built on the Eino framework, providing a vendor-neutral alternative to LangGraph-based research tools. It implements a supervisor-researcher pattern for parallel research execution, with initial support for Ollama as the LLM provider and a Bubble Tea CLI interface.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        CLI Layer                             │
│                    (Bubble Tea TUI)                          │
├─────────────────────────────────────────────────────────────┤
│                      API Gateway                             │
│                 (Future HTTP/WebSocket)                      │
├─────────────────────────────────────────────────────────────┤
│                 Observability Layer (OpenTelemetry)          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │ Tracing  │  │ Metrics  │  │ Logging  │  │ Baggage  │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
├─────────────────────────────────────────────────────────────┤
│                    Application Core                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │         Instrumented Workflow Engine (Eino)          │   │
│  │  ┌────────────┐  ┌────────────┐  ┌──────────────┐  │   │
│  │  │ Supervisor │  │ Researcher │  │ Report Gen   │  │   │
│  │  │   Node     │──│   Nodes    │──│    Node      │  │   │
│  │  └────────────┘  └────────────┘  └──────────────┘  │   │
│  └──────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────┤
│                     Domain Services                          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │Research  │  │  Tool    │  │  State   │  │ Config   │   │
│  │Orchestr. │  │ Registry │  │ Manager  │  │ Manager  │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
├─────────────────────────────────────────────────────────────┤
│                    Infrastructure Layer                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  Ollama  │  │  Search  │  │Document  │  │ Storage  │   │
│  │  Client  │  │   APIs   │  │ Loaders  │  │ Backend  │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
├─────────────────────────────────────────────────────────────┤
│                    Telemetry Backends                        │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  Jaeger  │  │Prometheus│  │   Loki   │  │  Grafana │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Domain Models

```go
// pkg/domain/models.go

type ResearchRequest struct {
    ID          string
    Query       string
    Context     string
    MaxDepth    int
    Timestamp   time.Time
}

type ResearchTask struct {
    ID              string
    Topic           string
    ParentID        string
    Status          TaskStatus
    Priority        int
    AssignedWorker  string
    Results         []ResearchResult
    CreatedAt       time.Time
    CompletedAt     *time.Time
}

type ResearchResult struct {
    ID          string
    TaskID      string
    Source      string
    Content     string
    Summary     string
    Confidence  float64
    Metadata    map[string]interface{}
    Timestamp   time.Time
}

type ResearchReport struct {
    ID              string
    RequestID       string
    Executive       string
    Sections        []ReportSection
    Findings        []Finding
    Sources         []Source
    GeneratedAt     time.Time
    TokensUsed      int
}

type TaskStatus string

const (
    TaskStatusPending    TaskStatus = "pending"
    TaskStatusInProgress TaskStatus = "in_progress"
    TaskStatusCompleted  TaskStatus = "completed"
    TaskStatusFailed     TaskStatus = "failed"
)
```

### 2. State Management

```go
// pkg/state/state.go

type GraphState struct {
    mu              sync.RWMutex
    Request         ResearchRequest
    Tasks           map[string]*ResearchTask
    Messages        []Message
    CurrentPhase    Phase
    Iterations      int
    MaxIterations   int
    Error           error
}

type Message struct {
    Role        string // "system", "user", "assistant", "tool"
    Content     string
    ToolCalls   []ToolCall
    Timestamp   time.Time
}

type Phase string

const (
    PhaseClarification  Phase = "clarification"
    PhasePlanning       Phase = "planning"
    PhaseResearch       Phase = "research"
    PhaseCompression    Phase = "compression"
    PhaseReporting      Phase = "reporting"
    PhaseComplete       Phase = "complete"
)

// Thread-safe state operations
func (s *GraphState) UpdateTask(taskID string, fn func(*ResearchTask)) error
func (s *GraphState) AddMessage(msg Message)
func (s *GraphState) SetPhase(phase Phase)
func (s *GraphState) GetSnapshot() GraphStateSnapshot
```

### 3. Workflow Engine (Eino Integration)

```go
// pkg/workflow/graph.go

type ResearchGraph struct {
    graph       *eino.Graph
    config      *Config
    llmClient   *OllamaClient
    tools       ToolRegistry
}

func NewResearchGraph(cfg *Config) (*ResearchGraph, error) {
    g := eino.NewGraph()
    
    // Define nodes
    g.AddNode("clarify", ClarifyNode)
    g.AddNode("plan", PlanningNode)
    g.AddNode("supervisor", SupervisorNode)
    g.AddNode("researcher", ResearcherNode)
    g.AddNode("compress", CompressionNode)
    g.AddNode("report", ReportGenerationNode)
    
    // Define edges with conditions
    g.AddConditionalEdge("clarify", 
        func(state *GraphState) string {
            if state.NeedsClarification() {
                return "end"
            }
            return "plan"
        })
    
    g.AddEdge("plan", "supervisor")
    g.AddConditionalEdge("supervisor",
        func(state *GraphState) string {
            if state.ResearchComplete() {
                return "compress"
            }
            return "researcher"
        })
    
    g.AddEdge("researcher", "supervisor")
    g.AddEdge("compress", "report")
    g.AddEdge("report", "end")
    
    return &ResearchGraph{graph: g}, nil
}

// Node implementations
func ClarifyNode(ctx context.Context, state *GraphState) error
func PlanningNode(ctx context.Context, state *GraphState) error
func SupervisorNode(ctx context.Context, state *GraphState) error
func ResearcherNode(ctx context.Context, state *GraphState) error
func CompressionNode(ctx context.Context, state *GraphState) error
func ReportGenerationNode(ctx context.Context, state *GraphState) error
```

### 4. Supervisor-Researcher Pattern

```go
// pkg/workflow/supervisor.go

type Supervisor struct {
    maxConcurrency  int
    taskQueue       chan *ResearchTask
    workerPool      *WorkerPool
}

func (s *Supervisor) Execute(ctx context.Context, state *GraphState) error {
    // Analyze research brief
    brief := s.analyzeBrief(state.Request)
    
    // Generate research tasks
    tasks := s.generateTasks(brief)
    
    // Distribute tasks to researchers
    results := s.distributeWork(ctx, tasks)
    
    // Aggregate results
    state.UpdateResults(results)
    
    return nil
}

// pkg/workflow/researcher.go

type Researcher struct {
    id          string
    llmClient   LLMClient
    tools       []Tool
}

func (r *Researcher) Execute(ctx context.Context, task *ResearchTask) (*ResearchResult, error) {
    // Research loop with tool usage
    for i := 0; i < maxIterations; i++ {
        // Think about next action
        action := r.planNextAction(task)
        
        // Execute tool
        result := r.executeTool(action)
        
        // Check if complete
        if r.isComplete(result) {
            break
        }
    }
    
    // Compress findings
    return r.compressFindings(task)
}
```

### 5. Ollama Integration

```go
// pkg/llm/ollama.go

type OllamaClient struct {
    baseURL     string
    model       string
    httpClient  *http.Client
    options     OllamaOptions
}

type OllamaOptions struct {
    Temperature     float64
    MaxTokens       int
    TopP            float64
    SystemPrompt    string
}

func NewOllamaClient(baseURL, model string) *OllamaClient {
    return &OllamaClient{
        baseURL:    baseURL,
        model:      model,
        httpClient: &http.Client{Timeout: 2 * time.Minute},
    }
}

func (c *OllamaClient) Complete(ctx context.Context, messages []Message) (*CompletionResponse, error)
func (c *OllamaClient) Stream(ctx context.Context, messages []Message) (<-chan StreamResponse, error)
func (c *OllamaClient) Embed(ctx context.Context, text string) ([]float64, error)

// Eino ChatModel implementation
func (c *OllamaClient) Chat(ctx context.Context, req *eino.ChatRequest) (*eino.ChatResponse, error)
```

### 6. Tool System

```go
// pkg/tools/registry.go

type Tool interface {
    Name() string
    Description() string
    Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
    Schema() ToolSchema
}

type ToolRegistry struct {
    tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
    registry := &ToolRegistry{
        tools: make(map[string]Tool),
    }
    
    // Register default tools
    registry.Register(NewWebSearchTool())
    registry.Register(NewThinkTool())
    registry.Register(NewSummarizeTool())
    
    return registry
}

// pkg/tools/search.go

type WebSearchTool struct {
    client SearchClient
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    query := args["query"].(string)
    results := t.client.Search(ctx, query)
    return t.formatResults(results), nil
}
```

### 7. CLI Interface (Bubble Tea)

```go
// cmd/ora/main.go

type Model struct {
    state           AppState
    input           textinput.Model
    viewport        viewport.Model
    progress        progress.Model
    currentRequest  *ResearchRequest
    results         []ResearchResult
    err             error
}

type AppState int

const (
    StateInput AppState = iota
    StateProcessing
    StateResults
    StateError
)

func (m Model) Init() tea.Cmd {
    return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "enter":
            if m.state == StateInput {
                return m, m.startResearch()
            }
        case "ctrl+c", "q":
            return m, tea.Quit
        }
        
    case ResearchStartedMsg:
        m.state = StateProcessing
        return m, m.trackProgress()
        
    case ResearchProgressMsg:
        m.progress.SetPercent(msg.Percent)
        return m, m.trackProgress()
        
    case ResearchCompleteMsg:
        m.state = StateResults
        m.results = msg.Results
        return m, nil
        
    case errMsg:
        m.state = StateError
        m.err = msg
        return m, nil
    }
    
    return m, nil
}

func (m Model) View() string {
    switch m.state {
    case StateInput:
        return m.renderInput()
    case StateProcessing:
        return m.renderProgress()
    case StateResults:
        return m.renderResults()
    case StateError:
        return m.renderError()
    }
    return ""
}
```

### 8. API Gateway (Future Frontend Support)

```go
// pkg/api/server.go

type Server struct {
    research    *ResearchService
    router      *gin.Engine
    upgrader    websocket.Upgrader
}

func NewServer(research *ResearchService) *Server {
    s := &Server{
        research: research,
        router:   gin.New(),
    }
    
    s.setupRoutes()
    return s
}

func (s *Server) setupRoutes() {
    // REST endpoints
    s.router.POST("/api/research", s.handleCreateResearch)
    s.router.GET("/api/research/:id", s.handleGetResearch)
    s.router.GET("/api/research/:id/status", s.handleGetStatus)
    
    // WebSocket for real-time updates
    s.router.GET("/ws", s.handleWebSocket)
}

// pkg/api/handlers.go

func (s *Server) handleCreateResearch(c *gin.Context) {
    var req CreateResearchRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    
    research, err := s.research.Create(c.Request.Context(), req)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    
    c.JSON(201, research)
}

func (s *Server) handleWebSocket(c *gin.Context) {
    conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        return
    }
    
    client := NewWSClient(conn, s.research)
    go client.HandleMessages()
}
```

## Configuration Management

```go
// pkg/config/config.go

type Config struct {
    Ollama      OllamaConfig      `yaml:"ollama"`
    Research    ResearchConfig    `yaml:"research"`
    Tools       ToolsConfig       `yaml:"tools"`
    Storage     StorageConfig     `yaml:"storage"`
    API         APIConfig         `yaml:"api"`
}

type OllamaConfig struct {
    BaseURL         string  `yaml:"base_url"`
    Model           string  `yaml:"model"`
    Temperature     float64 `yaml:"temperature"`
    MaxTokens       int     `yaml:"max_tokens"`
}

type ResearchConfig struct {
    MaxConcurrency      int `yaml:"max_concurrency"`
    MaxIterations       int `yaml:"max_iterations"`
    MaxDepth            int `yaml:"max_depth"`
    CompressionRatio    float64 `yaml:"compression_ratio"`
}

func Load(path string) (*Config, error) {
    // Load from file with environment variable override support
}
```

## Directory Structure

```
open-research-agent/
├── cmd/
│   └── ora/
│       └── main.go              # CLI entry point
├── pkg/
│   ├── domain/
│   │   ├── models.go            # Core domain models
│   │   └── interfaces.go        # Domain interfaces
│   ├── workflow/
│   │   ├── graph.go             # Eino graph setup
│   │   ├── nodes.go             # Node implementations
│   │   ├── supervisor.go        # Supervisor logic
│   │   └── researcher.go        # Researcher logic
│   ├── state/
│   │   ├── state.go             # State management
│   │   └── store.go             # State persistence
│   ├── llm/
│   │   ├── ollama.go            # Ollama client
│   │   ├── instrumented.go      # Instrumented LLM client
│   │   └── prompts.go           # Prompt templates
│   ├── tools/
│   │   ├── registry.go          # Tool registry
│   │   ├── instrumented.go      # Instrumented tools
│   │   ├── search.go            # Search tools
│   │   └── think.go             # Thinking tool
│   ├── observability/
│   │   ├── telemetry.go         # OpenTelemetry setup
│   │   ├── middleware.go        # Instrumentation middleware
│   │   ├── metrics.go           # Metric definitions
│   │   ├── tracing.go           # Tracing utilities
│   │   ├── logging.go           # Structured logging
│   │   ├── baggage.go           # Context propagation
│   │   ├── sampling.go          # Adaptive sampling
│   │   └── resource.go          # Resource detection
│   ├── api/
│   │   ├── server.go            # HTTP/WS server
│   │   ├── handlers.go          # Request handlers
│   │   └── websocket.go         # WebSocket support
│   ├── cli/
│   │   ├── app.go               # Bubble Tea app
│   │   ├── views.go             # UI components
│   │   └── styles.go            # Terminal styles
│   └── config/
│       └── config.go            # Configuration
├── configs/
│   ├── default.yaml             # Default configuration
│   ├── otel-collector.yaml      # OTel collector config
│   ├── prometheus.yml           # Prometheus config
│   ├── alerts.yaml              # Alert rules
│   └── slo.yaml                 # SLO definitions
├── deployments/
│   ├── docker-compose.yml       # Main deployment
│   ├── docker-compose.obs.yml   # Observability stack
│   └── k8s/                     # Kubernetes manifests
├── dashboards/
│   ├── grafana/                 # Grafana dashboards
│   └── queries/                 # Common queries
├── internal/
│   └── utils/                   # Internal utilities
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
└── README.md
```

## Implementation Phases

### Phase 1: Core Foundation (Week 1) ✅ COMPLETED
- [x] Domain models and interfaces
- [x] Basic graph structure (sequential, ready for Eino)
- [x] Ollama client implementation
- [x] State management system

### Phase 2: Workflow Engine (Week 2)
- [ ] Supervisor node implementation
- [ ] Researcher node implementation
- [ ] Parallel task execution
- [ ] Tool registry and basic tools

### Phase 3: CLI Interface (Week 3)
- [ ] Bubble Tea UI setup
- [ ] Interactive research flow
- [ ] Progress tracking
- [ ] Result visualization

### Phase 4: Enhancement & Testing (Week 4)
- [ ] Search API integration
- [ ] Compression and reporting
- [ ] Unit and integration tests
- [ ] Performance optimization

### Phase 5: API Layer (Future)
- [ ] REST API endpoints
- [ ] WebSocket support
- [ ] Frontend integration hooks
- [ ] Authentication/authorization

## Key Design Decisions

### 1. Clean Architecture
- **Domain-centric**: Core business logic independent of frameworks
- **Dependency injection**: Interfaces for all external dependencies
- **Testability**: All components unit testable in isolation
- **Observability-first**: Every component instrumented from inception

### 2. Concurrency Model
- **Go channels**: For task distribution and result aggregation
- **Context propagation**: For cancellation, timeout, and trace context
- **Worker pools**: For controlled parallel research execution
- **Trace correlation**: Parent-child span relationships across goroutines

### 3. State Management
- **Thread-safe**: Mutex-protected state mutations
- **Event sourcing ready**: State changes as events for future persistence
- **Snapshot capability**: For checkpoint/resume functionality
- **State metrics**: Gauge metrics for state transitions and sizes

### 4. Extensibility Points
- **Tool plugins**: Easy addition of new research tools with automatic instrumentation
- **LLM providers**: Interface-based for future provider support with metrics
- **Storage backends**: Pluggable persistence layer with performance tracking
- **UI frontends**: API-first design for multiple clients with trace propagation

### 5. Observability Strategy
- **OpenTelemetry native**: Built-in tracing, metrics, and logging
- **Adaptive sampling**: Intelligent sampling based on error rates
- **SLI/SLO driven**: Service level indicators defined upfront
- **Cost tracking**: Token usage and API call metrics for budget control

## Performance Considerations

1. **Parallel Research**: Utilize Go's concurrency for parallel task execution
2. **Streaming Responses**: Stream LLM responses to CLI for better UX
3. **Caching**: Cache search results and embeddings
4. **Resource Pooling**: Connection pools for HTTP clients
5. **Graceful Degradation**: Fallback strategies for service failures

## Security Considerations

1. **API Key Management**: Secure storage in environment/config
2. **Input Validation**: Sanitize all user inputs
3. **Rate Limiting**: Prevent abuse of LLM and search APIs
4. **Audit Logging**: Track all research requests and results
5. **Data Privacy**: Option to disable persistence of sensitive queries

## Testing Strategy

1. **Unit Tests**: 80% coverage for core logic
2. **Integration Tests**: Workflow end-to-end tests
3. **Mock Services**: Mock Ollama and search APIs for testing
4. **Performance Tests**: Benchmark parallel execution
5. **CLI Tests**: Terminal UI interaction tests

## Deployment

1. **Single Binary**: Cross-compiled for multiple platforms
2. **Docker Support**: Containerized deployment option
3. **Configuration**: Environment variables or config file
4. **Monitoring**: OpenTelemetry instrumentation ready
5. **Logging**: Structured logging with levels

## Future Enhancements

1. **Multi-model Support**: Add OpenAI, Anthropic, Google providers
2. **RAG Integration**: Vector database for context enhancement
3. **Web UI**: React/Vue frontend using API
4. **Collaborative Research**: Multi-user research sessions
5. **Plugin System**: Dynamic tool loading
6. **Research Templates**: Pre-configured research workflows
7. **Export Formats**: PDF, Markdown, DOCX report generation