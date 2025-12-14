# Architecture

Hector's architecture is designed for production deployments with observability, security, and A2A-native federation.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP/gRPC Server                        │
│  ┌────────────┬────────────┬────────────┬──────────────┐   │
│  │ Discovery  │  A2A API   │  Metrics   │  Health      │   │
│  └────────────┴────────────┴────────────┴──────────────┘   │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                        Runtime                               │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Agent Registry  │  LLM Providers  │  Tool Registry  │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Session Service  │  Memory Index  │  Checkpoint Mgr │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────┬───────────────────────────────────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
┌───────▼────────┐ ┌──────▼──────┐ ┌───────▼────────┐
│  LLM Providers │ │ Vector DBs  │ │  Databases     │
│  - OpenAI      │ │ - Chromem   │ │  - SQLite      │
│  - Anthropic   │ │ - Qdrant    │ │  - Postgres    │
│  - Gemini      │ │ - Pinecone  │ │  - MySQL       │
│  - Ollama      │ └─────────────┘ └────────────────┘
└────────────────┘
```

## Core Components

### Runtime

The runtime is the central component that manages agent lifecycle:

**Responsibilities**:

- Build agents from configuration
- Create and manage LLM providers
- Initialize tool registries
- Setup session and memory services
- Handle hot reload

**Key Types** (pkg/runtime/runtime.go):
```go
type Runtime struct {
    cfg             *config.Config
    llms            map[string]model.LLM
    embedders       map[string]embedder.Embedder
    toolsets        map[string]tool.Toolset
    agents          map[string]agent.Agent
    sessions        session.Service
    index           memory.IndexService
    checkpoint      *checkpoint.Manager
    observability   *observability.Manager
    vectorProviders map[string]vector.Provider
    documentStores  map[string]*rag.DocumentStore
}
```

**Lifecycle**:
1. Load configuration
2. Create LLM providers
3. Initialize embedders
4. Build tools and toolsets
5. Create agents
6. Setup persistence (sessions, tasks)
7. Start observability

### Server

HTTP/gRPC server exposing A2A protocol endpoints:

**Endpoints**:

- `/.well-known/agent-card.json` - Agent card
- `/agents` - Agent discovery
- `/agents/{name}/message:send` - Send message
- `/agents/{name}/message:stream` - Stream message
- `/tasks` - Task management
- `/metrics` - Prometheus metrics
- `/health` - Health check

**Transports**:

- JSON-RPC over HTTP (default)
- gRPC (optional)

### Agents

Three agent types:

**LLM Agent** (pkg/agent/llmagent):

- LLM-powered reasoning
- Tool execution
- Multi-turn conversations
- Memory and context management

**Remote Agent** (pkg/agent/remoteagent):

- Proxy to external A2A services
- Fetches agent card
- Forwards requests
- Federation support

**Workflow Agent** (pkg/agent/workflowagent):

- Sequential execution
- Parallel execution
- Loop/iteration
- Orchestrates sub-agents

### Session Service

Manages conversation history and state:

**Storage Backends**:

- In-memory (default, ephemeral)
- SQL (persistent)

**Responsibilities**:

- Store messages
- Manage session state
- Track artifacts
- Persist across restarts

**Architecture**:
```
Session Service (SOURCE OF TRUTH)
    │
    ├─ Messages (conversation history)
    ├─ State (key-value pairs)
    └─ Artifacts (files, images)
```

### Memory Index

Searchable index over conversation history:

**Index Types**:

- **Keyword**: Simple word matching
- **Vector**: Semantic similarity with embeddings

**Use Cases**:

- Search past conversations
- Find relevant context
- Knowledge retrieval

**Architecture**:
```
Session Service → Index Service → Vector Provider
     (data)          (search)        (storage)
```

Index can be rebuilt from session data.

### Checkpoint Manager

Execution state checkpointing for recovery:

**Strategies**:

- **Event**: Checkpoint at specific events (tool execution, LLM calls)
- **Interval**: Checkpoint at regular intervals
- **Hybrid**: Both events and intervals

**Storage**:

- Checkpoints stored in session service
- Auto-cleanup of expired checkpoints

**Recovery**:

- Auto-resume on startup
- Manual recovery via API
- HITL approval for sensitive tasks

## Data Flow

### Message Flow

```
1. Client Request
   │
   ├─> HTTP Server
   │      │
   │      ├─> Authentication (if enabled)
   │      └─> Rate Limiting (if enabled)
   │
2. Runtime
   │
   ├─> Agent Resolution
   │      │
   │      └─> Visibility Check
   │
3. Agent Execution
   │
   ├─> Session Load (from session service)
   ├─> Context Preparation
   ├─> LLM Call
   │      │
   │      ├─> Tool Execution (if tool calls)
   │      │      │
   │      │      ├─> Approval Check (HITL)
   │      │      └─> Tool Result
   │      │
   │      └─> Response Generation
   │
4. Persistence
   │
   ├─> Session Update
   ├─> Memory Index Update
   └─> Checkpoint Save (if enabled)
   │
5. Response
   │
   └─> Client (streaming or complete)
```

### RAG Flow

```
1. Document Ingestion
   │
   ├─> Document Source (directory, SQL, API)
   ├─> MCP Parser (optional, for PDF/DOCX)
   ├─> Chunking Strategy
   ├─> Embedding Generation
   └─> Vector Storage

2. Query Processing
   │
   ├─> User Message
   ├─> Embedding Generation
   ├─> Vector Search
   ├─> Reranking (optional)
   └─> Top K Results

3. Context Injection
   │
   ├─> Retrieved Documents
   ├─> Format as Context
   └─> Inject into System Prompt
```

## Component Interactions

### Agent + Tools

```
Agent
  │
  ├─ Calls Tool
  │     │
  │     ├─> Approval Check (if required)
  │     ├─> Tool Execution
  │     │      │
  │     │      ├─ Built-in Function
  │     │      ├─ MCP Server Call
  │     │      └─ Command Execution
  │     │
  │     └─> Result
  │
  └─ Processes Result
```

### Multi-Agent Patterns

**Pattern 1: Transfer (Sub-Agents)**
```
Coordinator Agent
  │
  ├─ Calls transfer_to_specialist
  │
  └─> Control Transferred
        │
        Specialist Agent
          │
          └─ Continues Conversation
```

**Pattern 2: Delegation (Agent Tools)**
```
Parent Agent
  │
  ├─ Calls agent_tool
  │
  └─> Tool Execution
        │
        Agent Tool
          │
          ├─ Executes Task
          └─ Returns Result
        │
  Parent Agent
    │
    └─ Processes Result
```

### Observability Integration

```
Every Request
  │
  ├─> Start Trace Span
  │      │
  │      ├─ Agent Span
  │      │     │
  │      │     ├─ LLM Span
  │      │     │    └─ Record: tokens, latency
  │      │     │
  │      │     ├─ Tool Span
  │      │     │    └─ Record: duration, result
  │      │     │
  │      │     └─ Database Span
  │      │          └─ Record: query, duration
  │      │
  │      └─> End Trace Span
  │
  └─> Update Metrics
         │
         ├─ Counters (requests, tokens, errors)
         ├─ Histograms (latency)
         └─ Gauges (active sessions)
```

## Configuration System

### Configuration Loading

```
1. Load Phase
   │
   ├─> File Provider (YAML)
   │      └─ Load from disk
   │
   ├─> Environment Variables
   │      └─ Interpolate ${VAR}
   │
   └─> Validation
          └─ Schema check

2. Runtime Phase
   │
   ├─> Create Components
   │      │
   │      ├─ LLM Providers
   │      ├─ Embedders
   │      ├─ Tools
   │      └─ Agents
   │
   └─> Watch Mode (optional)
          │
          └─> Hot Reload on Change
```

### Hot Reload

```
Config File Change
  │
  ├─> Detect Change (file watcher)
  │
  ├─> Load New Config
  │      │
  │      └─> Validation
  │
  ├─> Reload Runtime
  │      │
  │      ├─ Rebuild LLMs
  │      ├─ Rebuild Tools
  │      └─ Rebuild Agents
  │
  └─> Swap Components
         │
         └─> Active sessions preserved
```

## Persistence Architecture

### Three-Layer Storage

```
┌──────────────────────────────────────────────┐
│  Application Layer (Runtime)                  │
└────────────┬─────────────────────────────────┘
             │
┌────────────▼─────────────────────────────────┐
│  Session Service (SOURCE OF TRUTH)            │
│  - Messages                                   │
│  - State                                      │
│  - Artifacts                                  │
│  Backend: InMemory or SQL                     │
└────────────┬─────────────────────────────────┘
             │
┌────────────▼─────────────────────────────────┐
│  Storage Layer                                │
│  - SQLite (embedded)                          │
│  - PostgreSQL (production)                    │
│  - MySQL (alternative)                        │
└───────────────────────────────────────────────┘
```

### Checkpoint/Recovery

```
Execution State
  │
  ├─> Event Trigger (tool execution, LLM call)
  │      │
  │      └─> Create Checkpoint
  │            │
  │            ├─ Serialize State
  │            └─ Save to Session
  │
  ├─> Interval Trigger (30s)
  │      │
  │      └─> Create Checkpoint
  │
  └─> Recovery (on restart)
         │
         ├─> Load Checkpoints
         ├─> Filter Expired
         ├─> Auto-Resume (non-HITL)
         └─> Await Approval (HITL)
```

## Security Architecture

### Authentication Flow

```
Request
  │
  ├─> Extract Token (Authorization header)
  │
  ├─> Validate Token
  │      │
  │      ├─ JWT: Verify signature with JWKS
  │      └─ API Key: Compare with configured keys
  │
  ├─> Extract Claims
  │      │
  │      └─ User ID, Email, Roles
  │
  └─> Attach to Context
```

### Authorization

```
Agent Request
  │
  ├─> Check Agent Visibility
  │      │
  │      ├─ Public: Allow (with auth if enabled)
  │      ├─ Internal: Require auth
  │      └─ Private: Deny HTTP access
  │
  └─> Check User Permissions (future)
```

### Tool Security

```
Tool Call
  │
  ├─> Check Approval Requirement
  │      │
  │      ├─ Required: Pause & Request Approval
  │      └─ Not Required: Continue
  │
  ├─> Check Sandboxing (commands)
  │      │
  │      ├─ Whitelist Check
  │      ├─ Blacklist Check
  │      └─ Working Directory Check
  │
  └─> Execute Tool
```

## Scalability

### Horizontal Scaling

```
Load Balancer
  │
  ├─> Hector Instance 1
  ├─> Hector Instance 2
  └─> Hector Instance 3
       │
       └─> Shared Database (PostgreSQL)
              │
              ├─ Sessions (shared state)
              └─ Tasks (distributed)
```

**Stateless Design**:

- No in-process state
- All state in database
- Instances interchangeable

### Resource Efficiency

**Binary Size**: 30MB (stripped)
**Memory Footprint**: ~50-100MB baseline
**Startup Time**: <100ms
**Goroutines**: Efficient concurrency

Compare to Python frameworks:

- 10-20x less memory
- 20-100x faster startup
- Single binary deployment

## Extension Points

### Programmatic API

```go
// Custom agent with programmatic API
agent := hector.NewAgent("custom").
    WithLLM(llm).
    WithTools(tools).
    Build()

// Combine with config-based agents
runtime.NewRuntimeBuilder().
    WithConfig(cfg).
    WithAgent(agent).
    Start()
```

### Custom Tools

```go
// Custom tool implementation
type MyTool struct {}

func (t *MyTool) Name() string { return "my_tool" }
func (t *MyTool) Execute(ctx context.Context, input string) (string, error) {
    // Custom logic
    return result, nil
}

// Register with runtime
runtime.WithDirectTools("agent-name", []tool.Tool{&MyTool{}})
```

### Custom LLM Providers

```go
// Custom LLM implementation
type CustomLLM struct {}

func (l *CustomLLM) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
    // Custom LLM logic
    return response, nil
}

// Register factory
runtime.New(cfg, runtime.WithLLMFactory(func(cfg *config.LLMConfig) (model.LLM, error) {
    return &CustomLLM{}, nil
}))
```

## Performance Characteristics

**Request Latency**:

- Overhead: <10ms (routing, parsing)
- LLM: 500ms - 10s (dominates)
- Tools: 10ms - 1s (varies)
- Database: 1-10ms (local queries)

**Throughput**:

- Single instance: 100-1000 req/s (non-LLM bottleneck)
- LLM-limited: ~10-50 req/s (per LLM provider)
- Horizontal scaling: Linear with instances

**Resource Usage**:

- CPU: Low baseline, spikes during LLM
- Memory: ~50MB + sessions + vector data
- Network: LLM API dominant
- Disk: SQLite or logs only

## Design Principles

1. **Config-First**: Agents defined declaratively, not in code
2. **A2A-Native**: Full A2A v0.3.0 protocol compliance
3. **Production-Ready**: Observability, security, persistence built-in
4. **Resource Efficient**: Go implementation, minimal footprint
5. **Extensible**: Programmatic API, custom tools, custom LLMs
6. **Stateless**: Horizontal scaling via shared database
7. **Standards-Based**: OpenTelemetry, Prometheus, JWKS

## Next Steps

- [Runtime Concepts](runtime.md) - Deep dive into runtime
- [Agent Concepts](agents.md) - Agent architecture
- [A2A Protocol](a2a-protocol.md) - Protocol implementation
