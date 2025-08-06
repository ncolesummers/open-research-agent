# Open Research Agent - Implementation Guide

## Quick Start Implementation

This guide provides concrete code examples for implementing the Open Research Agent using Go and Eino.

## Core Implementation Examples

### 1. Workflow Node Implementations

```go
// pkg/workflow/nodes.go

package workflow

import (
    "context"
    "fmt"
    "github.com/cloudwego/eino/components/model"
    "github.com/cloudwego/eino/flow/graph"
)

// ClarifyNode determines if user query needs clarification
func ClarifyNode(ctx context.Context, state *GraphState) error {
    messages := []model.Message{
        {Role: "system", Content: clarifySystemPrompt},
        {Role: "user", Content: state.Request.Query},
    }
    
    response, err := state.LLM.Chat(ctx, &model.ChatRequest{
        Messages: messages,
        Options: model.ChatOptions{
            Temperature: 0.3,
            MaxTokens:   500,
        },
    })
    if err != nil {
        return fmt.Errorf("clarification failed: %w", err)
    }
    
    // Parse response to determine if clarification needed
    if needsClarification(response.Content) {
        state.AddMessage(Message{
            Role:    "assistant",
            Content: response.Content,
        })
        state.SetPhase(PhaseComplete)
    } else {
        state.SetPhase(PhasePlanning)
    }
    
    return nil
}

// SupervisorNode orchestrates parallel research tasks
func SupervisorNode(ctx context.Context, state *GraphState) error {
    if state.Iterations >= state.MaxIterations {
        state.SetPhase(PhaseCompression)
        return nil
    }
    
    // Generate research tasks based on current state
    tasks := generateResearchTasks(state)
    
    // Execute tasks in parallel using goroutines
    results := make(chan *ResearchResult, len(tasks))
    errors := make(chan error, len(tasks))
    
    for _, task := range tasks {
        go func(t *ResearchTask) {
            researcher := NewResearcher(state.LLM, state.Tools)
            result, err := researcher.Execute(ctx, t)
            if err != nil {
                errors <- err
                return
            }
            results <- result
        }(task)
    }
    
    // Collect results with timeout
    var collectedResults []*ResearchResult
    for i := 0; i < len(tasks); i++ {
        select {
        case result := <-results:
            collectedResults = append(collectedResults, result)
        case err := <-errors:
            // Log error but continue with other results
            fmt.Printf("Research task failed: %v\n", err)
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    
    // Update state with results
    state.UpdateResults(collectedResults)
    state.Iterations++
    
    // Determine next phase
    if isResearchComplete(state) {
        state.SetPhase(PhaseCompression)
    } else {
        state.SetPhase(PhaseResearch)
    }
    
    return nil
}

// ResearcherNode performs individual research tasks
func ResearcherNode(ctx context.Context, state *GraphState) error {
    task := state.GetNextPendingTask()
    if task == nil {
        return nil
    }
    
    task.Status = TaskStatusInProgress
    
    // Research loop with tool usage
    for i := 0; i < 10; i++ { // Max 10 tool calls per task
        // Plan next action
        action, err := planNextAction(ctx, state.LLM, task)
        if err != nil {
            task.Status = TaskStatusFailed
            return err
        }
        
        // Execute tool
        if action.Tool != "" {
            result, err := state.Tools.Execute(ctx, action.Tool, action.Args)
            if err != nil {
                continue // Try different approach
            }
            
            task.Results = append(task.Results, ResearchResult{
                Source:  action.Tool,
                Content: fmt.Sprintf("%v", result),
            })
        }
        
        // Check if research is complete
        if isTaskComplete(task) {
            break
        }
    }
    
    task.Status = TaskStatusCompleted
    return nil
}
```

### 2. Eino Graph Setup

```go
// pkg/workflow/graph_builder.go

package workflow

import (
    "github.com/cloudwego/eino/flow/graph"
    "github.com/cloudwego/eino/flow/option"
)

func BuildResearchGraph(cfg *Config) (*graph.Graph[*GraphState], error) {
    // Create new graph with state type
    g := graph.New[*GraphState]()
    
    // Add nodes with their functions
    g.AddNode("start", graph.NodeFunc[*GraphState](StartNode))
    g.AddNode("clarify", graph.NodeFunc[*GraphState](ClarifyNode))
    g.AddNode("plan", graph.NodeFunc[*GraphState](PlanningNode))
    g.AddNode("supervisor", graph.NodeFunc[*GraphState](SupervisorNode))
    g.AddNode("researcher", graph.NodeFunc[*GraphState](ResearcherNode))
    g.AddNode("compress", graph.NodeFunc[*GraphState](CompressionNode))
    g.AddNode("report", graph.NodeFunc[*GraphState](ReportGenerationNode))
    g.AddNode("end", graph.NodeFunc[*GraphState](EndNode))
    
    // Add edges with conditions
    g.AddEdge("start", "clarify")
    
    // Conditional edge from clarify
    g.AddConditionalEdge("clarify", func(state *GraphState) string {
        if state.CurrentPhase == PhaseComplete {
            return "end"
        }
        return "plan"
    })
    
    g.AddEdge("plan", "supervisor")
    
    // Conditional edge from supervisor
    g.AddConditionalEdge("supervisor", func(state *GraphState) string {
        switch state.CurrentPhase {
        case PhaseCompression:
            return "compress"
        case PhaseResearch:
            return "researcher"
        default:
            return "end"
        }
    })
    
    g.AddEdge("researcher", "supervisor")
    g.AddEdge("compress", "report")
    g.AddEdge("report", "end")
    
    // Compile the graph
    compiled, err := g.Compile(
        option.WithMaxConcurrency(cfg.Research.MaxConcurrency),
        option.WithTimeout(cfg.Research.Timeout),
    )
    
    return compiled, err
}
```

### 3. Ollama Integration with Eino

```go
// pkg/llm/ollama_eino.go

package llm

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    
    "github.com/cloudwego/eino/components/model"
    "github.com/cloudwego/eino/schema"
)

type OllamaEinoClient struct {
    baseURL    string
    model      string
    httpClient *http.Client
}

// Implement Eino's ChatModel interface
func (c *OllamaEinoClient) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
    // Convert Eino messages to Ollama format
    ollamaReq := OllamaRequest{
        Model:    c.model,
        Messages: convertMessages(req.Messages),
        Options: map[string]interface{}{
            "temperature": req.Options.Temperature,
            "num_predict": req.Options.MaxTokens,
        },
    }
    
    // Make HTTP request to Ollama
    body, err := json.Marshal(ollamaReq)
    if err != nil {
        return nil, err
    }
    
    httpReq, err := http.NewRequestWithContext(
        ctx,
        "POST",
        fmt.Sprintf("%s/api/chat", c.baseURL),
        bytes.NewReader(body),
    )
    if err != nil {
        return nil, err
    }
    
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    // Parse response
    var ollamaResp OllamaResponse
    if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
        return nil, err
    }
    
    return &model.ChatResponse{
        Content: ollamaResp.Message.Content,
        Usage: model.Usage{
            PromptTokens:     ollamaResp.PromptEvalCount,
            CompletionTokens: ollamaResp.EvalCount,
        },
    }, nil
}

// Stream implements streaming chat for Eino
func (c *OllamaEinoClient) Stream(ctx context.Context, req *model.ChatRequest) (<-chan model.ChatStreamResponse, error) {
    stream := make(chan model.ChatStreamResponse)
    
    go func() {
        defer close(stream)
        
        // Similar to Chat but with streaming enabled
        ollamaReq := OllamaRequest{
            Model:    c.model,
            Messages: convertMessages(req.Messages),
            Stream:   true,
        }
        
        // Make streaming request and send chunks through channel
        // Implementation details...
    }()
    
    return stream, nil
}
```

### 4. Bubble Tea CLI Implementation

```go
// cmd/ora/tui.go

package main

import (
    "fmt"
    "strings"
    
    "github.com/charmbracelet/bubbles/progress"
    "github.com/charmbracelet/bubbles/spinner"
    "github.com/charmbracelet/bubbles/textarea"
    "github.com/charmbracelet/bubbles/viewport"
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

type model struct {
    state       ViewState
    input       textarea.Model
    viewport    viewport.Model
    spinner     spinner.Model
    progress    progress.Model
    graph       *ResearchGraph
    research    *ResearchRequest
    report      string
    err         error
    width       int
    height      int
}

type ViewState int

const (
    ViewInput ViewState = iota
    ViewProcessing
    ViewReport
)

// Messages
type researchStartMsg struct{}
type researchProgressMsg float64
type researchCompleteMsg string
type errMsg error

func initialModel() model {
    ta := textarea.New()
    ta.Placeholder = "Enter your research question..."
    ta.Focus()
    ta.CharLimit = 500
    ta.SetHeight(5)
    
    sp := spinner.New()
    sp.Spinner = spinner.Dot
    sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
    
    prog := progress.New(progress.WithDefaultGradient())
    
    return model{
        state:    ViewInput,
        input:    ta,
        spinner:  sp,
        progress: prog,
    }
}

func (m model) Init() tea.Cmd {
    return tea.Batch(
        textarea.Blink,
        m.spinner.Tick,
    )
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd
    
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        m.viewport.Width = msg.Width
        m.viewport.Height = msg.Height - 10
        
    case tea.KeyMsg:
        switch msg.Type {
        case tea.KeyCtrlC, tea.KeyEsc:
            return m, tea.Quit
            
        case tea.KeyEnter:
            if m.state == ViewInput && !m.input.Value() == "" {
                m.research = &ResearchRequest{
                    Query: m.input.Value(),
                }
                m.state = ViewProcessing
                return m, m.startResearch
            }
        }
        
    case researchStartMsg:
        cmds = append(cmds, m.spinner.Tick)
        
    case researchProgressMsg:
        cmd := m.progress.SetPercent(float64(msg))
        cmds = append(cmds, cmd)
        
    case researchCompleteMsg:
        m.state = ViewReport
        m.report = string(msg)
        m.viewport.SetContent(m.report)
        
    case errMsg:
        m.err = msg
        m.state = ViewReport
        m.viewport.SetContent(fmt.Sprintf("Error: %v", msg))
        
    case spinner.TickMsg:
        var cmd tea.Cmd
        m.spinner, cmd = m.spinner.Update(msg)
        cmds = append(cmds, cmd)
    }
    
    // Update active component
    switch m.state {
    case ViewInput:
        var cmd tea.Cmd
        m.input, cmd = m.input.Update(msg)
        cmds = append(cmds, cmd)
        
    case ViewReport:
        var cmd tea.Cmd
        m.viewport, cmd = m.viewport.Update(msg)
        cmds = append(cmds, cmd)
    }
    
    return m, tea.Batch(cmds...)
}

func (m model) View() string {
    header := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("205")).
        Render("🔍 Open Research Agent")
    
    switch m.state {
    case ViewInput:
        return fmt.Sprintf(
            "%s\n\n%s\n\n%s",
            header,
            m.input.View(),
            "(Press Enter to start research, Ctrl+C to quit)",
        )
        
    case ViewProcessing:
        return fmt.Sprintf(
            "%s\n\n%s Researching...\n\n%s\n\n%s",
            header,
            m.spinner.View(),
            m.progress.View(),
            "Please wait while I research your question...",
        )
        
    case ViewReport:
        return fmt.Sprintf(
            "%s\n\n%s\n\n%s",
            header,
            m.viewport.View(),
            "(Use arrow keys to scroll, q to quit)",
        )
    }
    
    return ""
}

func (m model) startResearch() tea.Msg {
    // Run research in background
    go func() {
        // Execute research graph
        result, err := m.graph.Execute(context.Background(), m.research)
        if err != nil {
            program.Send(errMsg(err))
            return
        }
        
        program.Send(researchCompleteMsg(result.Report))
    }()
    
    return researchStartMsg{}
}
```

### 5. Tool Implementation Example

```go
// pkg/tools/web_search.go

package tools

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
)

type WebSearchTool struct {
    httpClient *http.Client
    apiKey     string
    maxResults int
}

func NewWebSearchTool(apiKey string) *WebSearchTool {
    return &WebSearchTool{
        httpClient: &http.Client{},
        apiKey:     apiKey,
        maxResults: 5,
    }
}

func (t *WebSearchTool) Name() string {
    return "web_search"
}

func (t *WebSearchTool) Description() string {
    return "Search the web for current information"
}

func (t *WebSearchTool) Schema() ToolSchema {
    return ToolSchema{
        Type: "object",
        Properties: map[string]SchemaProperty{
            "query": {
                Type:        "string",
                Description: "Search query",
                Required:    true,
            },
            "max_results": {
                Type:        "integer",
                Description: "Maximum number of results",
                Default:     5,
            },
        },
    }
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    query, ok := args["query"].(string)
    if !ok {
        return nil, fmt.Errorf("query is required")
    }
    
    maxResults := t.maxResults
    if mr, ok := args["max_results"].(int); ok {
        maxResults = mr
    }
    
    // Use DuckDuckGo HTML API (no key required)
    searchURL := fmt.Sprintf(
        "https://html.duckduckgo.com/html/?q=%s",
        url.QueryEscape(query),
    )
    
    req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := t.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    // Parse HTML response and extract results
    results := parseSearchResults(resp.Body, maxResults)
    
    return results, nil
}
```

### 6. Main Entry Point

```go
// cmd/ora/main.go

package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    
    tea "github.com/charmbracelet/bubbletea"
    "github.com/your-org/ora/pkg/config"
    "github.com/your-org/ora/pkg/llm"
    "github.com/your-org/ora/pkg/workflow"
)

func main() {
    var (
        configPath = flag.String("config", "configs/default.yaml", "Path to config file")
        apiMode    = flag.Bool("api", false, "Run in API server mode")
        port       = flag.String("port", "8080", "API server port")
    )
    flag.Parse()
    
    // Load configuration
    cfg, err := config.Load(*configPath)
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    // Initialize components
    ollamaClient := llm.NewOllamaEinoClient(
        cfg.Ollama.BaseURL,
        cfg.Ollama.Model,
    )
    
    // Build research graph
    graph, err := workflow.BuildResearchGraph(cfg)
    if err != nil {
        log.Fatalf("Failed to build graph: %v", err)
    }
    
    if *apiMode {
        // Run API server
        runAPIServer(cfg, graph, *port)
    } else {
        // Run TUI
        runTUI(cfg, graph)
    }
}

func runTUI(cfg *config.Config, graph *workflow.ResearchGraph) {
    m := initialModel()
    m.graph = graph
    
    program = tea.NewProgram(m, tea.WithAltScreen())
    if _, err := program.Run(); err != nil {
        fmt.Printf("Error running program: %v", err)
        os.Exit(1)
    }
}

func runAPIServer(cfg *config.Config, graph *workflow.ResearchGraph, port string) {
    server := api.NewServer(graph)
    log.Printf("Starting API server on port %s", port)
    if err := server.Run(":" + port); err != nil {
        log.Fatalf("Server failed: %v", err)
    }
}
```

## Configuration File Example

```yaml
# configs/default.yaml

ollama:
  base_url: "http://localhost:11434"
  model: "llama3.2"
  temperature: 0.7
  max_tokens: 4096

research:
  max_concurrency: 5
  max_iterations: 6
  max_depth: 3
  compression_ratio: 0.3
  timeout: 5m

tools:
  web_search:
    enabled: true
    provider: "duckduckgo"
  
storage:
  type: "memory"
  path: "./data"

api:
  enabled: false
  port: 8080
  cors:
    allowed_origins: ["*"]
```

## Makefile

```makefile
# Makefile

.PHONY: build run test clean install-deps

BINARY_NAME=ora
BUILD_DIR=./bin
GO_FILES=$(shell find . -name '*.go' -type f)

build:
	@echo "Building Open Research Agent..."
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/ora

run: build
	@$(BUILD_DIR)/$(BINARY_NAME)

test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...

coverage: test
	@go tool cover -html=coverage.out

install-deps:
	@echo "Installing dependencies..."
	@go mod download
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

lint:
	@golangci-lint run ./...

clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out

docker-build:
	@docker build -t open-research-agent:latest .

docker-run:
	@docker run -it --rm open-research-agent:latest
```

## Getting Started

1. **Install dependencies**:
```bash
go mod init github.com/your-org/open-research-agent
go get github.com/cloudwego/eino
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/bubbles
```

2. **Start Ollama**:
```bash
ollama serve
ollama pull llama3.2
```

3. **Build and run**:
```bash
make build
./bin/ora
```

4. **Run with custom config**:
```bash
./bin/ora -config configs/custom.yaml
```

5. **Run in API mode**:
```bash
./bin/ora -api -port 8080
```

This implementation provides a solid foundation that can be extended with additional features as needed.