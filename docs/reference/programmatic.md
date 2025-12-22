# Programmatic API Reference

Complete Go API reference for the `github.com/verikod/hector` package. This reference covers all programmatic APIs for building agents, tools, runners, and RAG systems in Go.

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

## High-Level API (`pkg`)

The `pkg` package provides the simplest way to build and run Hector agents. It combines configuration loading with programmatic customization.

### Initialization

Initialize a Hector instance using options or a config file.

```go
// From config file
h, err := pkg.FromConfig("config.yaml")

// Programmatic with options
h, err := pkg.New(
    pkg.WithOpenAI(openai.Config{APIKey: "sk-..."}),
    pkg.WithAgentName("researcher"),
    pkg.WithInstruction("You are a researcher."),
    pkg.WithMCPTool("weather", "http://localhost:8080"),
)
```

**Methods:**

| Method | Description |
|--------|-------------|
| `New(opts ...Option)` | Creates a new instance programmatically |
| `FromConfig(path string)` | Loads instance from YAML config |
| `FromConfigWithContext(ctx, path)` | Loads from config with context |

### Configuration Options

Options for `pkg.New()`:

| Option | Description |
|--------|-------------|
| `WithOpenAI(cfg)` | Configures OpenAI LLM |
| `WithAnthropic(cfg)` | Configures Anthropic LLM |
| `WithGemini(cfg)` | Configures Gemini LLM |
| `WithOllama(cfg)` | Configures Ollama LLM |
| `WithLLM(llm)` | Uses a custom LLM instance |
| `WithAgentName(name)` | Sets the default agent's name |
| `WithInstruction(text)` | Sets the default agent's system instruction |
| `WithMCPTool(name, url)` | Adds an MCP toolset (SSE) |
| `WithMCPCommand(name, cmd...)` | Adds an MCP toolset (Stdio) |
| `WithToolset(ts)` | Adds a custom toolset |
| `WithSubAgents(name, agents...)` | Adds sub-agents to an agent (Transfer) |
| `WithAgentTools(name, agents...)` | Adds agents as tools to an agent (Delegation) |
| `WithDirectTools(name, tools...)` | Adds custom tools to an agent |

### Agent Helpers

Convenience functions for creating agents without the builder.

| Function | Description |
|----------|-------------|
| `NewAgent(cfg)` | Creates an LLM agent |
| `NewSequentialAgent(cfg)` | Creates a sequential workflow agent |
| `NewParallelAgent(cfg)` | Creates a parallel workflow agent |
| `NewLoopAgent(cfg)` | Creates a loop workflow agent |
| `NewRemoteAgent(cfg)` | Creates a remote agent client |
| `AgentAsTool(agent)` | Wraps an agent as a callable tool |
| `FindAgent(root, name)` | Finds an agent in the hierarchy |
| `ListAgents(root)` | Returns all agents in the hierarchy |

## Builder Package (`pkg/builder`)

### LLM Builder

Constructs LLM instances from providers.

```go
import "github.com/verikod/hector/pkg/builder"

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
| `APIKey(key string)` | Sets the API key directly |
| `APIKeyFromEnv(envVar string)` | Sets API key from environment variable |
| `BaseURL(url string)` | Sets a custom API base URL |
| `Temperature(temp float64)` | Sets sampling temperature (0.0-2.0) |
| `MaxTokens(n int)` | Sets max tokens to generate |
| `Timeout(duration time.Duration)` | Sets request timeout |
| `MaxToolOutputLength(n int)` | Sets max length for tool outputs |
| `MaxRetries(n int)` | Sets max retry attempts |
| `EnableThinking(enable bool)` | Enables thinking/reasoning mode |
| `ThinkingBudget(tokens int)` | Sets token budget for thinking |
| `Build() (model.LLM, error)` | Finalizes and returns the LLM |
| `MustBuild() model.LLM` | Panics on error |
| `LLMFromConfig(cfg *config.LLMConfig)` | Creates builder from config struct |

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
| `WithTool(t tool.Tool)` | Adds a single tool |
| `WithTools(tools ...tool.Tool)` | Adds multiple tools |
| `WithToolset(ts tool.Toolset)` | Adds a toolset |
| `WithToolsets(ts ...tool.Toolset)` | Adds multiple toolsets |
| `WithSubAgent(agent agent.Agent)` | Adds a sub-agent (transfer pattern) |
| `WithSubAgents(agents ...agent.Agent)` | Adds multiple sub-agents |
| `WithWorkingMemory(strategy)` | Sets working memory strategy |
| `WithReasoning(config *llmagent.ReasoningConfig)` | Configures reasoning loop |
| `WithReasoningBuilder(rb *ReasoningBuilder)` | Sets reasoning from builder |
| `EnableStreaming(enable bool)` | Enables/disables streaming responses |
| `DisallowTransferToParent(bool)` | Prevents delegation to parent |
| `DisallowTransferToPeers(bool)` | Prevents delegation to siblings |
| `WithOutputKey(key string)` | Saves output to session state |
| `WithInputSchema(schema map[string]any)` | Validates input when used as tool |
| `WithOutputSchema(schema map[string]any)` | Enforces structured output format |
| `WithBeforeAgentCallback(cb)` | Callback before agent starts |
| `WithAfterAgentCallback(cb)` | Callback after agent completes |
| `WithBeforeModelCallback(cb)` | Callback before LLM call |
| `WithAfterModelCallback(cb)` | Callback after LLM call |
| `WithBeforeToolCallback(cb)` | Callback before tool execution |
| `WithAfterToolCallback(cb)` | Callback after tool execution |
| `Build() (agent.Agent, error)` | Finalizes the agent |
| `MustBuild() agent.Agent` | Panics on error |

### Tool Builders

#### Function Tool (Typed)

Creates tools from Go functions with automatic JSON schema generation from struct tags.

```go
import "github.com/verikod/hector/pkg/tool"

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
| `WithIndexService(svc runner.IndexService)` | Sets index service for search |
| `WithMemoryIndex(idx memory.IndexService)` | Sets up memory indexing |
| `WithCheckpointManager(mgr)` | Sets checkpoint manager |
| `Build() (*runner.Runner, error)` | Finalizes the runner |
| `MustBuild() *runner.Runner` | Panics on error |

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

### Embedder Builder

Constructs embedders for vector embedding generation.

```go
embedder := builder.NewEmbedder("openai"). // "openai", "ollama", "cohere"
    Model("text-embedding-3-small").
    APIKeyFromEnv("OPENAI_API_KEY").
    MustBuild()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewEmbedder(provider string)` | Starts building an embedder |
| `Model(name string)` | Sets the embedding model |
| `APIKey(key string)` | Sets the API key directly |
| `APIKeyFromEnv(envVar string)` | Sets API key from environment |
| `BaseURL(url string)` | Sets custom API base URL |
| `Dimension(dim int)` | Sets expected embedding dimension |
| `Timeout(seconds int)` | Sets API request timeout |
| `BatchSize(size int)` | Sets batch size for requests |
| `EncodingFormat(format string)` | Sets encoding format (OpenAI) |
| `InputType(type string)` | Sets input type (Cohere v3+) |
| `OutputDimension(dim int)` | Sets output dimension (Cohere v4+) |
| `Truncate(strategy string)` | Sets truncation strategy (Cohere) |
| `Build() (embedder.Embedder, error)` | Finalizes the embedder |
| `MustBuild() embedder.Embedder` | Panics on error |
| `EmbedderFromConfig(cfg)` | Creates builder from config |

### Vector Provider Builder

Constructs vector database providers for embedding storage.

```go
// Local embedded provider
provider := builder.NewVectorProvider("chromem").
    PersistPath(".hector/vectors").
    Compress(true).
    MustBuild()

// Cloud provider
provider := builder.NewVectorProvider("qdrant").
    Host("qdrant.example.com").
    Port(6334).
    APIKey("qdr-...").
    UseTLS(true).
    MustBuild()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewVectorProvider(type string)` | Starts building (chromem, qdrant, chroma, pinecone, milvus, weaviate) |
| `PersistPath(path string)` | Sets file path for chromem |
| `Compress(bool)` | Enables compression (chromem) |
| `Host(host string)` | Sets server host |
| `Port(port int)` | Sets server port |
| `APIKey(key string)` | Sets API key for cloud providers |
| `UseTLS(bool)` | Enables TLS connections |
| `IndexName(name string)` | Sets index name (Pinecone) |
| `Build() (vector.Provider, error)` | Finalizes the provider |
| `MustBuild() vector.Provider` | Panics on error |

### Document Store Builder

Constructs RAG document stores for indexing and search.

```go
store, err := builder.NewDocumentStore("knowledge_base").
    FromDirectory("./documents").
    IncludePatterns("*.md", "*.txt").
    ExcludePatterns("*.tmp").
    WithEmbedder(embedder).
    WithVectorProvider(vectorProvider).
    ChunkSize(1000).
    ChunkOverlap(100).
    EnableWatching(true).
    EnableIncremental(true).
    Build()

// Index documents
store.Index(ctx)
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewDocumentStore(name string)` | Starts building a document store |
| `Description(desc string)` | Sets store description |
| `Collection(name string)` | Sets vector collection name |
| `FromDirectory(path string)` | Configures directory source |
| `IncludePatterns(patterns ...)` | Sets glob patterns for inclusion |
| `ExcludePatterns(patterns ...)` | Sets glob patterns for exclusion |
| `MaxFileSize(bytes int64)` | Sets max file size to process |
| `WithVectorProvider(p)` | Sets vector database |
| `WithEmbedder(e)` | Sets embedding provider |
| `ChunkSize(size int)` | Sets chunk size |
| `ChunkOverlap(overlap int)` | Sets chunk overlap |
| `EnableWatching(bool)` | Enables file watching |
| `EnableIncremental(bool)` | Enables incremental indexing |
| `EnableCheckpoints(bool)` | Enables checkpoint/resume |
| `EnableProgress(bool)` | Enables progress display |
| `MaxConcurrent(n int)` | Sets max concurrent workers |
| `DefaultTopK(k int)` | Sets default search results |
| `DefaultThreshold(score float32)` | Sets default similarity threshold |
| `EnableHyDE(bool)` | Enables Hypothetical Document Embeddings |
| `Build() (*rag.DocumentStore, error)` | Finalizes the store |
| `MustBuild()` | Panics on error |

### MCP Builder

Constructs MCP (Model Context Protocol) toolsets.

```go
// SSE transport
toolset, _ := builder.NewMCP("weather").
    URL("http://localhost:9000").
    Transport("sse").
    Build()

// Stdio transport
toolset, _ := builder.NewMCP("filesystem").
    Command("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp").
    Env(map[string]string{"DEBUG": "1"}).
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewMCP(name string)` | Starts building an MCP toolset |
| `URL(url string)` | Sets server URL (SSE/HTTP) |
| `Command(cmd, args...)` | Sets command for stdio transport |
| `Transport(type string)` | Sets transport type (sse, stdio, streamable-http) |
| `Filter(tools ...string)` | Limits exposed tools |
| `Env(map[string]string)` | Sets environment variables (stdio) |
| `Build() (*mcptoolset.Toolset, error)` | Finalizes the toolset |
| `MustBuild()` | Panics on error |
| `MCPFromConfig(name, cfg)` | Creates builder from config |

### Toolset Builder

Wraps multiple tools into a toolset.

```go
toolset := builder.NewToolset("my-tools").
    WithTool(tool1).
    WithTools(tool2, tool3).
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewToolset(name string)` | Starts building a toolset |
| `WithTool(t tool.Tool)` | Adds a single tool |
| `WithTools(tools ...tool.Tool)` | Adds multiple tools |
| `Build() tool.Toolset` | Finalizes the toolset |

### Working Memory Builder

Constructs working memory strategies.

```go
// Buffer window - simple sliding window
strategy, _ := builder.NewWorkingMemory("buffer_window").
    WindowSize(30).
    Build()

// Token window - token-based management
strategy, _ := builder.NewWorkingMemory("token_window").
    ModelName("gpt-4o").
    Budget(8000).
    PreserveRecent(5).
    Build()

// Summary buffer - summarization-based (requires LLM)
strategy, _ := builder.NewWorkingMemory("summary_buffer").
    Budget(8000).
    Threshold(0.85).
    Target(0.6).
    WithLLM(llm).
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewWorkingMemory(strategy string)` | Starts building (buffer_window, token_window, summary_buffer) |
| `WindowSize(size int)` | Sets window size (buffer_window) |
| `ModelName(name string)` | Sets model for token counting |
| `Budget(tokens int)` | Sets token budget |
| `Threshold(ratio float64)` | Sets summarization trigger threshold |
| `Target(ratio float64)` | Sets target after summarization |
| `PreserveRecent(count int)` | Sets minimum recent messages to keep |
| `WithLLM(llm model.LLM)` | Sets LLM for summarization |
| `Build() (memory.WorkingMemoryStrategy, error)` | Finalizes the strategy |
| `MustBuild()` | Panics on error |

### Server Builder

Constructs A2A servers for agent exposure.

```go
srv, _ := builder.NewServer().
    WithRunner(myRunner).
    Address(":8080").
    EnableUI(true).
    EnableStreaming(true).
    BuildServer()

srv.ListenAndServe()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewServer()` | Starts building a server |
| `WithRunner(r *runner.Runner)` | Sets the runner |
| `Address(addr string)` | Sets listen address |
| `EnableUI(bool)` | Enables built-in web UI |
| `EnableStreaming(bool)` | Enables streaming responses |
| `Build() (http.Handler, error)` | Builds HTTP handler |
| `BuildServer() (*Server, error)` | Builds complete server |
| `MustBuildServer() *Server` | Panics on error |

### Auth Builder

Constructs authentication configuration.

```go
auth := builder.NewAuth().
    JWKSURL("https://auth.example.com/.well-known/jwks.json").
    Issuer("https://auth.example.com").
    Audience("hector-api").
    RequireAuth(true).
    ExcludedPaths("/health", "/ready").
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewAuth()` | Starts building auth config |
| `Enabled(bool)` | Enables/disables authentication |
| `JWKSURL(url string)` | Sets JWKS URL for token validation |
| `Issuer(iss string)` | Sets expected JWT issuer |
| `Audience(aud string)` | Sets expected JWT audience |
| `RefreshInterval(duration)` | Sets JWKS refresh interval |
| `RequireAuth(bool)` | Makes authentication mandatory |
| `ExcludedPaths(paths ...)` | Sets paths excluded from auth |
| `AddExcludedPath(path string)` | Adds a single excluded path |
| `Build() *config.AuthConfig` | Finalizes the config |

### Credentials Builder

Constructs credentials for remote agents/services.

```go
// Bearer token
creds := builder.NewCredentials().
    Type("bearer").
    Token("my-token").
    Build()

// API key
creds := builder.NewCredentials().
    Type("api_key").
    APIKey("my-key").
    APIKeyHeader("X-API-Key").
    Build()

// Basic auth
creds := builder.NewCredentials().
    Type("basic").
    Username("user").
    Password("pass").
    Build()
```

**Methods:**

| Method | Description |
|--------|-------------|
| `NewCredentials()` | Starts building credentials |
| `Type(type string)` | Sets type (bearer, api_key, basic) |
| `Token(token string)` | Sets bearer token |
| `APIKey(key string)` | Sets API key |
| `APIKeyHeader(header string)` | Sets API key header name |
| `Username(user string)` | Sets basic auth username |
| `Password(pass string)` | Sets basic auth password |
| `Build() *config.CredentialsConfig` | Finalizes the config |

## Agent Package (`pkg/agent`)

### Agent Interface

```go
type Agent interface {
    Name() string           // Unique identifier within agent tree
    DisplayName() string    // Human-readable name for UI
    Description() string    // Used by LLMs for delegation decisions
    Run(ctx InvocationContext) iter.Seq2[*Event, error]
    SubAgents() []Agent     // Child agents for delegation
    Type() AgentType        // "custom", "llm", "sequential", "parallel", "loop", "remote"
}
```

**Agent Types:**

| Type | Description |
|------|-------------|
| `TypeCustomAgent` | Custom logic agents (`agent.New`) |
| `TypeLLMAgent` | LLM-based agents (`llmagent.New`) |
| `TypeSequentialAgent` | Runs sub-agents in sequence |
| `TypeParallelAgent` | Runs sub-agents in parallel |
| `TypeLoopAgent` | Loops until condition met |
| `TypeRemoteAgent` | Remote A2A agents |

### Checkpointable Interface

Optional interface for agents supporting state checkpointing for fault tolerance and HITL recovery.

```go
type Checkpointable interface {
    CaptureCheckpointState() (map[string]any, error)  // Capture current state
    RestoreCheckpointState(state map[string]any) error // Restore from checkpoint
}
```

### Invocation Context

Provides access to invocation data and services.

```go
type InvocationContext interface {
    ReadonlyContext
    
    // Current agent
    Agent() Agent
    
    // Session access  
    Session() Session
    Memory() Memory
    
    // Runtime configuration
    RunConfig() *RunConfig
    
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
    context.Context  // Embeds Go context
    
    InvocationID() string
    AgentName() string
    UserContent() *Content
    ReadonlyState() ReadonlyState
    UserID() string
    AppName() string
    SessionID() string
    Branch() string  // Agent hierarchy path
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
    ID            string
    Timestamp     time.Time
    InvocationID  string
    Branch        string        // Agent hierarchy path
    Author        string        // Display name (or "user"/"system")
    AgentID       string        // Unique agent identifier
    Message       *a2a.Message
    Partial       bool          // Streaming chunk
    TurnComplete  bool          // Final event of turn
    Interrupted   bool          // Forcibly stopped
    ErrorCode     string        // Machine-readable error
    ErrorMessage  string        // Human-readable error
    Thinking      *ThinkingState
    ToolCalls     []ToolCallState
    ToolResults   []ToolResultState
    Actions       EventActions
    LongRunningToolIDs []string
    CustomMetadata     map[string]any
}

type EventActions struct {
    StateDelta        map[string]any
    ArtifactDelta     map[string]int64
    SkipSummarization bool
    TransferToAgent   string
    Escalate          bool
    RequireInput      bool    // HITL pattern
    InputPrompt       string  // Message for human
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
| `WithDBPool(pool)` | Shared database pool for SQL backends |
| `WithIndexService(idx)` | Custom memory index service |
| `WithCheckpointManager(mgr)` | Custom checkpoint manager |

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
import "github.com/verikod/hector/pkg"

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
    "github.com/verikod/hector/pkg"
    "github.com/verikod/hector/pkg/builder"
    "github.com/verikod/hector/pkg/agent"
    "github.com/verikod/hector/pkg/tool"
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
