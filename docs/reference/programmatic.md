# Programmatic API Reference

Complete Go API reference for the `github.com/kadirpekel/hector` package. This reference covers all programmatic APIs for building agents, tools, runners, and RAG systems in Go.

## Package Overview

| Package | Description |
|---------|-------------|
| `pkg/builder` | Fluent builder API for constructing components |
| `pkg/agent` | Agent types, interfaces, and execution contexts |
| `pkg/tool` | Tool interface and execution |
| `pkg/model` | LLM interface abstraction |
| `pkg/session` | Session and state management |
| `pkg/memory` | Memory index and search |
| `pkg/runner` | Session execution and orchestration |
| `pkg/runtime` | Runtime lifecycle management |
| `pkg/config` | Configuration structures |
| `pkg/rag` | Document stores and embeddings |
| `pkg` | Top-level convenience functions |

## Builder Package (`pkg/builder`)

### LLM Builder

Constructs LLM instances from providers.

```go
import "github.com/kadirpekel/hector/pkg/builder"

llm := builder.NewLLM("openai"). // "openai", "anthropic", "gemini", "ollama"
    Model("gpt-4o").
    APIKey(os.Getenv("OPENAI_API_KEY")).
    Temperature(0.7).
    MaxTokens(4096).
    BaseURL("https://api.openai.com/v1"). // optional custom endpoint
    MustBuild() // or Build() for error handling
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewLLM(provider string)` | Starts building an LLM for the specified provider |
| `Model(name string)` | Sets the model name |
| `APIKey(key string)` | Sets the API key |
| `BaseURL(url string)` | Sets a custom API base URL |
| `Temperature(temp float64)` | Sets sampling temperature (0.0-2.0) |
| `MaxTokens(n int)` | Sets max tokens to generate |
| `Build() (model.LLM, error)` | Finalizes and returns the LLM |
| `MustBuild() model.LLM` | Panics on error |

### Agent Builder

Constructs intelligent agents with LLMs, tools, and sub-agents.

```go
agent, err := builder.NewAgent("assistant").
    WithName("My Assistant").
    WithDescription("A helpful AI assistant").
    WithLLM(llm).
    WithInstruction("You are a helpful assistant.").
    WithTools(tool1, tool2).
    WithSubAgents(specialist1, specialist2). // Transfer pattern
    WithReasoning(reasoningConfig).
    EnableStreaming(true).
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewAgent(id string)` | Starts building an agent with the given ID |
| `WithName(name string)` | Sets the human-readable display name |
| `WithDescription(desc string)` | Sets the agent description |
| `WithLLM(llm model.LLM)` | Sets the LLM instance |
| `WithInstruction(inst string)` | Sets the system instruction |
| `WithTools(tools ...tool.Tool)` | Adds tools to the agent |
| `WithTool(t tool.Tool)` | Adds a single tool |
| `WithSubAgents(agents ...agent.Agent)` | Adds sub-agents (creates transfer tools) |
| `WithReasoning(config *config.ReasoningConfig)` | Configures reasoning loop |
| `EnableStreaming(enable bool)` | Enables/disables streaming responses |
| `Build() (agent.Agent, error)` | Finalizes the agent |

### Tool Builders

#### Function Tool (Typed)

Creates tools from Go functions with automatic JSON schema generation from struct tags.

```go
import "github.com/kadirpekel/hector/pkg/tool"

type WeatherArgs struct {
    City string `json:"city" jsonschema:"required,description=The city to check weather for"`
    Unit string `json:"unit" jsonschema:"description=Temperature unit (celsius/fahrenheit),default=celsius"`
}

weatherTool, err := builder.FunctionTool(
    "get_weather",
    "Get current weather for a city",
    func(ctx tool.Context, args WeatherArgs) (map[string]any, error) {
        // Implementation
        return map[string]any{
            "city": args.City,
            "temp": 22,
            "unit": args.Unit,
            "conditions": "Sunny",
        }, nil
    },
)

// Or use MustFunctionTool to panic on error
tool := builder.MustFunctionTool("name", "description", handler)
```

**JSON Schema Tags:**

| Tag | Description |
|-----|-------------|
| `json:"fieldname"` | JSON field name |
| `jsonschema:"required"` | Field is required |
| `jsonschema:"description=..."` | Field description |
| `jsonschema:"default=..."` | Default value |
| `jsonschema:"enum=a,b,c"` | Allowed values |

### Runner Builder

Constructs a runner to manage sessions and execute agents.

```go
runner, err := builder.NewRunner("my-app").
    WithAgent(agent).
    WithSessionService(sessionSvc). // optional custom session storage
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewRunner(appName string)` | Starts building a runner |
| `WithAgent(agent agent.Agent)` | Sets the root agent |
| `WithSessionService(svc session.Service)` | Sets custom session storage |
| `Build() (*runner.Runner, error)` | Finalizes the runner |

### Reasoning Builder

Configures the agent's reasoning loop behavior.

```go
cfg := builder.NewReasoning().
    MaxIterations(10).
    EnableExitTool(true).
    EnableEscalateTool(true).
    CompletionInstruction("Call exit_loop when done.").
    Build()

agent := builder.NewAgent("assistant").
    WithReasoning(cfg).
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `MaxIterations(n int)` | Maximum reasoning loop iterations |
| `EnableExitTool(bool)` | Enable explicit exit_loop tool |
| `EnableEscalateTool(bool)` | Enable escalate tool for parent delegation |
| `CompletionInstruction(string)` | Custom instruction for loop termination |

### RAG Builders

#### Embedder Builder

```go
embedder := builder.NewEmbedder("openai"). // "openai", "ollama", "cohere"
    Model("text-embedding-3-small").
    APIKey(os.Getenv("OPENAI_API_KEY")).
    MustBuild()
```

#### Vector Provider Builder

```go
vectorProvider := builder.NewVectorProvider("chromem"). // "chromem", "qdrant", etc.
    PersistPath("./data/vectors").
    MustBuild()
```

#### Document Store Builder

```go
store, err := builder.NewDocumentStore("knowledge_base").
    FromDirectory("./documents").
    WithEmbedder(embedder).
    WithVectorProvider(vectorProvider).
    Build()

// Index documents
store.Index(ctx)
```

## Agent Package (`pkg/agent`)

### Agent Interface

```go
type Agent interface {
    Name() string
    Run(ctx InvocationContext) iter.Seq2[*Event, error]
}
```

### Invocation Context

Provides access to invocation data and services.

```go
type InvocationContext interface {
    // Identity
    InvocationID() string
    AgentName() string

    // Input
    UserContent() *Content

    // State
    State() State
    ReadonlyState() ReadonlyState

    // Services
    Session() Session
    Memory() Memory
    Artifacts() Artifacts

    // Control
    EndInvocation()
    Ended() bool
}
```

**Usage in Custom Agent:**

```go
agent, err := agent.New(agent.Config{
    Name: "custom",
    Run: func(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
        return func(yield func(*agent.Event, error) bool) {
            // Read state
            value, _ := ctx.State().Get("counter")

            // Update state
            ctx.State().Set("counter", value.(int) + 1)

            // Access user input
            user := ctx.UserContent()

            // Create response
            event := agent.NewEvent(ctx.InvocationID())
            event.Message = &a2a.Message{
                Role: a2a.MessageRoleAgent,
                Parts: []a2a.Part{
                    a2a.TextPart{Text: "Hello!"},
                },
            }
            yield(event, nil)
        }
    },
})
```

### Readonly Context

Safe to pass to tools and callbacks (no state mutation).

```go
type ReadonlyContext interface {
    InvocationID() string
    AgentName() string
    UserContent() *Content
    ReadonlyState() ReadonlyState
    UserID() string
    SessionID() string
}
```

### Callback Context

Allows state modification in callbacks.

```go
type CallbackContext interface {
    ReadonlyContext
    State() State
    Artifacts() Artifacts
}
```

### Event Structure

```go
type Event struct {
    ID           string
    InvocationID string
    Author       string       // Agent name
    Branch       string       // Agent hierarchy path
    Message      *a2a.Message
    Partial      bool         // Streaming chunk
    Actions      Actions
}

type Actions struct {
    ToolCalls  []ToolCall       // Tool invocations
    Transfer   *Transfer        // Delegation to another agent
    StateDelta map[string]any   // State changes
}
```

### Content Structure

```go
type Content struct {
    Role  string
    Parts []a2a.Part
}
```

## Tool Package (`pkg/tool`)

Hector uses a layered tool interface hierarchy:

```
Tool (base)
  ├── CallableTool       - Simple synchronous execution
  ├── StreamingTool      - Real-time incremental output
  ├── CancellableTool    - Supports targeted cancellation
  ├── IsLongRunning()    - Async operations
  └── RequiresApproval() - HITL pattern
```

### Tool Interface (Base)

```go
type Tool interface {
    Name() string
    Description() string
    IsLongRunning() bool
    RequiresApproval() bool
}
```

### CallableTool Interface

Extends Tool with synchronous execution:

```go
type CallableTool interface {
    Tool
    Call(ctx Context, args map[string]any) (map[string]any, error)
    Schema() map[string]any
}
```

### StreamingTool Interface

For tools that produce incremental output:

```go
type StreamingTool interface {
    Tool
    CallStreaming(ctx Context, args map[string]any) iter.Seq2[*Result, error]
    Schema() map[string]any
}
```

### Tool Context

```go
type Context interface {
    agent.CallbackContext
    
    FunctionCallID() string
    Actions() *agent.EventActions
    SearchMemory(ctx context.Context, query string) (*agent.MemorySearchResponse, error)
    Task() agent.CancellableTask
}
```

### Custom CallableTool Implementation

```go
type WeatherTool struct{}

func (t *WeatherTool) Name() string {
    return "get_weather"
}

func (t *WeatherTool) Description() string {
    return "Fetches current weather for a location"
}

func (t *WeatherTool) IsLongRunning() bool {
    return false
}

func (t *WeatherTool) RequiresApproval() bool {
    return false
}

func (t *WeatherTool) Schema() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "city": map[string]any{
                "type":        "string",
                "description": "City name",
            },
        },
        "required": []string{"city"},
    }
}

func (t *WeatherTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
    city := args["city"].(string)
    // Implementation...
    return map[string]any{
        "temperature": 22,
        "conditions":  "Sunny",
    }, nil
}
```

### Custom StreamingTool Implementation

For tools that produce incremental output (e.g., command execution):

```go
func (t *CommandTool) CallStreaming(ctx tool.Context, args map[string]any) iter.Seq2[*tool.Result, error] {
    return func(yield func(*tool.Result, error) bool) {
        // Execute command and stream output...
        for line := range outputLines {
            if !yield(&tool.Result{Content: line, Streaming: true}, nil) {
                return // Client disconnected
            }
        }
        // Final result
        yield(&tool.Result{Content: finalOutput, Streaming: false}, nil)
    }
}
```

## Session Package (`pkg/session`)

### Session Interface

```go
type Session interface {
    ID() string
    AppName() string
    UserID() string
    State() State
    Events() Events
    LastUpdateTime() time.Time
}
```

### State Interface

```go
type State interface {
    Get(key string) (any, error)
    Set(key string, value any) error
    Delete(key string) error
    All() iter.Seq2[string, any]
}
```

**State Scopes (Key Prefixes):**

| Prefix | Scope | Lifecycle |
|--------|-------|-----------|
| (none) | Session | Persists within session |
| `user:` | User | Shared across user's sessions |
| `app:` | Application | Global across all users |
| `temp:` | Temporary | Cleared after invocation |

```go
// Session-specific
ctx.State().Set("last_search", "weather")

// User-level preferences
ctx.State().Set("user:theme", "dark")

// App-level configuration
ctx.State().Set("app:version", "2.0.0")

// Temporary (auto-cleared)
ctx.State().Set("temp:processing", true)
```

### Events Interface

```go
type Events interface {
    All() iter.Seq[*agent.Event]
    At(index int) *agent.Event
    Len() int
}
```

### Session Service

```go
type Service interface {
    Get(ctx context.Context, req *GetRequest) (*GetResponse, error)
    Create(ctx context.Context, req *CreateRequest) (*CreateResponse, error)
    AppendEvent(ctx context.Context, session Session, event *agent.Event) error
    List(ctx context.Context, req *ListRequest) (*ListResponse, error)
    Delete(ctx context.Context, req *DeleteRequest) error
}
```

**Backends:**

```go
// In-memory (development)
svc := session.InMemoryService()

// SQL (production)
svc, err := session.NewSQLSessionService(db, "sqlite") // or "postgres", "mysql"
```

## Memory Package (`pkg/memory`)

### Index Service

```go
type IndexService interface {
    Index(ctx context.Context, sess agent.Session) error
    Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error)
    Rebuild(ctx context.Context, sessions session.Service, appName, userID string) error
    Clear(ctx context.Context, appName, userID, sessionID string) error
    Name() string
}
```

### Search Request/Response

```go
type SearchRequest struct {
    Query   string
    UserID  string
    AppName string
}

type SearchResponse struct {
    Results []SearchResult
}

type SearchResult struct {
    SessionID string
    EventID   string
    Content   string
    Author    string
    Timestamp time.Time
    Score     float64
    Metadata  map[string]any
}
```

## Runner Package (`pkg/runner`)

### Runner

Manages agent execution across sessions.

```go
type Runner struct {
    // ...
}

func (r *Runner) Run(
    ctx context.Context,
    userID string,
    sessionID string,
    content *agent.Content,
    config RunConfig,
) iter.Seq2[*agent.Event, error]
```

**Usage:**

```go
runner, _ := builder.NewRunner("my-app").
    WithAgent(myAgent).
    Build()

content := &agent.Content{
    Role:  "user",
    Parts: []a2a.Part{a2a.TextPart{Text: "Hello!"}},
}

for event, err := range runner.Run(ctx, "user-1", "session-1", content, agent.RunConfig{}) {
    if err != nil {
        log.Fatal(err)
    }
    if event.Message != nil {
        fmt.Println(event.Message.Parts)
    }
}
```

## Runtime Package (`pkg/runtime`)

### Runtime Creation

```go
runtime, err := runtime.New(cfg,
    runtime.WithSessionService(sessionSvc),
    runtime.WithObservability(obs),
)
```

### Runtime Options

| Option | Description |
|--------|-------------|
| `WithSessionService(svc)` | Custom session storage |
| `WithObservability(obs)` | Observability manager |
| `WithSubAgents(agentName, agents)` | Inject sub-agents |
| `WithAgentTools(agentName, agents)` | Inject agents as tools |
| `WithDirectTools(agentName, tools)` | Inject custom tools |
| `WithLLMFactory(factory)` | Custom LLM factory |
| `WithEmbedderFactory(factory)` | Custom embedder factory |
| `WithToolsetFactory(factory)` | Custom toolset factory |

**Custom Tool Injection:**

```go
runtime.New(cfg,
    runtime.WithDirectTools("assistant", []tool.Tool{
        &MyCustomTool{},
    }),
)
```

**Custom Sub-Agents:**

```go
runtime.New(cfg,
    runtime.WithSubAgents("coordinator", []agent.Agent{
        customSpecialist,
    }),
)
```

**Custom Factory:**

```go
runtime.New(cfg,
    runtime.WithLLMFactory(func(cfg *config.LLMConfig) (model.LLM, error) {
        if cfg.Provider == "custom" {
            return &CustomLLM{}, nil
        }
        return DefaultLLMFactory(cfg)
    }),
)
```

### Runtime Reload

```go
err := runtime.Reload(newConfig)
```

Atomically swaps components while preserving active sessions.

## Top-Level Package (`pkg`)

### AgentAsTool

Wraps an agent to be used as a tool (delegation pattern).

```go
import "github.com/kadirpekel/hector/pkg"

researcherAgent, _ := builder.NewAgent("researcher").
    WithLLM(llm).
    WithInstruction("Research topics thoroughly").
    Build()

managerAgent, _ := builder.NewAgent("manager").
    WithLLM(llm).
    WithTool(pkg.AgentAsTool(researcherAgent)).
    Build()
```

## Callbacks

### Agent Callbacks

**Before Agent:**

```go
type BeforeAgentCallback func(CallbackContext) (*a2a.Message, error)

beforeAgent := func(ctx agent.CallbackContext) (*a2a.Message, error) {
    // Validate, inject context, or skip execution
    if !authorized {
        return &a2a.Message{
            Role: a2a.MessageRoleAgent,
            Parts: []a2a.Part{
                a2a.TextPart{Text: "Unauthorized"},
            },
        }, nil // Skip agent execution
    }
    return nil, nil // Continue
}
```

**After Agent:**

```go
type AfterAgentCallback func(CallbackContext) (*a2a.Message, error)
```

### Model Callbacks

**Before Model:**

```go
type BeforeModelCallback func(ctx CallbackContext, req *model.Request) (*model.Response, error)
```

**After Model:**

```go
type AfterModelCallback func(ctx CallbackContext, resp *model.Response, err error) (*model.Response, error)
```

### Tool Callbacks

**Before Tool:**

```go
type BeforeToolCallback func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error)

beforeTool := func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
    log.Info("Tool called", "name", t.Name(), "args", args)
    
    // Validate arguments
    if err := validate(args); err != nil {
        return nil, err
    }
    
    // Modify arguments
    args["timestamp"] = time.Now()
    return args, nil
}
```

**After Tool:**

```go
type AfterToolCallback func(ctx tool.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error)
```

## Working Memory Strategies

### Strategy Interface

```go
type WorkingMemoryStrategy interface {
    FilterEvents(events []*agent.Event) []*agent.Event
    CheckAndSummarize(ctx context.Context, events []*agent.Event) (*agent.Event, error)
}
```

### Built-in Strategies

**All (No Filtering):**

```go
// Include entire conversation history
```

**Buffer Window:**

```go
type BufferWindow struct {
    MaxMessages int
}

func (b *BufferWindow) FilterEvents(events []*agent.Event) []*agent.Event {
    if len(events) <= b.MaxMessages {
        return events
    }
    return events[len(events)-b.MaxMessages:]
}
```

**Token Window:**

```go
// Keep messages within token budget
```

**Summary Buffer:**

```go
// Summarize old messages when exceeding threshold
```

## Artifacts

The `Artifacts` interface provides artifact storage operations:

```go
type Artifacts interface {
    Save(ctx context.Context, name string, part a2a.Part) (*ArtifactSaveResponse, error)
    List(ctx context.Context) (*ArtifactListResponse, error)
    Load(ctx context.Context, name string) (*ArtifactLoadResponse, error)
    LoadVersion(ctx context.Context, name string, version int) (*ArtifactLoadResponse, error)
}
```

**Usage:**

```go
// Save artifact (a2a.Part can be FilePart, DataPart, etc.)
resp, err := ctx.Artifacts().Save(ctx, "chart.png", a2a.FilePart{
    Name:     "chart.png",
    MimeType: "image/png",
    Bytes:    imageBytes,
})

// List artifacts
list, err := ctx.Artifacts().List(ctx)
for _, info := range list.Artifacts {
    fmt.Println(info.Name, info.Version)
}

// Load artifact
artifact, err := ctx.Artifacts().Load(ctx, "chart.png")
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/a2aproject/a2a-go/a2a"
    "github.com/kadirpekel/hector/pkg"
    "github.com/kadirpekel/hector/pkg/builder"
    "github.com/kadirpekel/hector/pkg/agent"
    "github.com/kadirpekel/hector/pkg/tool"
)

type WeatherArgs struct {
    City string `json:"city" jsonschema:"required,description=City name"`
}

func main() {
    ctx := context.Background()

    // 1. Build LLM
    llm := builder.NewLLM("openai").
        Model("gpt-4o").
        APIKey(os.Getenv("OPENAI_API_KEY")).
        MustBuild()

    // 2. Create tools
    weatherTool := builder.MustFunctionTool(
        "get_weather",
        "Get weather for a city",
        func(ctx tool.Context, args WeatherArgs) (map[string]any, error) {
            return map[string]any{"temp": 22, "conditions": "Sunny"}, nil
        },
    )

    // 3. Create sub-agent
    researcher, _ := builder.NewAgent("researcher").
        WithLLM(llm).
        WithInstruction("Research topics thoroughly").
        Build()

    // 4. Build main agent with sub-agent as tool
    assistant, _ := builder.NewAgent("assistant").
        WithName("AI Assistant").
        WithLLM(llm).
        WithTools(weatherTool).
        WithTool(pkg.AgentAsTool(researcher)).
        WithInstruction("You are a helpful assistant.").
        EnableStreaming(true).
        Build()

    // 5. Create runner
    runner, _ := builder.NewRunner("my-app").
        WithAgent(assistant).
        Build()

    // 6. Run conversation
    content := &agent.Content{
        Role:  "user",
        Parts: []a2a.Part{a2a.TextPart{Text: "What's the weather in Berlin?"}},
    }

    for event, err := range runner.Run(ctx, "user-1", "session-1", content, agent.RunConfig{}) {
        if err != nil {
            panic(err)
        }
        if event.Message != nil {
            for _, part := range event.Message.Parts {
                if text, ok := part.(a2a.TextPart); ok {
                    fmt.Print(text.Text)
                }
            }
        }
    }
}
```
