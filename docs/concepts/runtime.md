# Runtime

The Runtime manages Hector's component lifecycle, building agents from configuration and handling hot reload.

## Overview

**Runtime responsibilities:**

- Build LLM providers from configuration
- Create embedders for semantic search
- Initialize tools and toolsets
- Construct agents with dependencies
- Manage RAG document stores
- Setup session and memory services
- Coordinate hot reload

## Architecture

```go
type Runtime struct {
    cfg *config.Config

    llms          map[string]model.LLM
    embedders     map[string]embedder.Embedder
    toolsets      map[string]tool.Toolset
    agents        map[string]agent.Agent

    sessions      session.Service       // SOURCE OF TRUTH
    index         memory.IndexService   // SEARCH INDEX
    checkpoint    *checkpoint.Manager
    observability *observability.Manager

    vectorProviders map[string]vector.Provider
    documentStores  map[string]*rag.DocumentStore
}
```

**Data Flow:**

```
Configuration → Runtime → Components → Agents
```

**Persistence Model:**

```
Session Service (SOURCE OF TRUTH)
    │
    ├─ Messages (conversation history)
    ├─ State (key-value store)
    └─ Artifacts (files)

Index Service (SEARCH INDEX)
    │
    └─ Built from session events
       (can be rebuilt at any time)
```

## Initialization

### Creation

```go
runtime, err := runtime.New(cfg,
    runtime.WithSessionService(sessionSvc),
    runtime.WithObservability(obs),
)
```

### Build Phases

Runtime builds components in dependency order:

```
1. Observability  → Tracing & metrics
2. Session Service → Data persistence
3. LLM Providers   → Language models
4. Embedders       → Semantic embeddings
5. Vector Stores   → Vector databases
6. Toolsets        → Tools for agents
7. Document Stores → RAG sources
8. Index Service   → Search capability
9. Agents          → Configured agents
```

### Dependency Graph

```
Agents
  ├─> LLMs
  ├─> Tools
  │    └─> Toolsets
  │         └─> MCP Servers (optional)
  ├─> Document Stores
  │    ├─> Vector Providers
  │    ├─> Embedders
  │    └─> LLMs (for query processing)
  └─> Sub-agents (recursive)

Session Service ← Sessions, Checkpoints
Index Service   ← Embedders
```

## Component Creation

### LLM Providers

```go
func (r *Runtime) buildLLMs() error {
    for name, cfg := range r.cfg.LLMs {
        llm, err := r.llmFactory(cfg)
        r.llms[name] = llm
    }
}
```

**Factory function:**

```go
type LLMFactory func(cfg *config.LLMConfig) (model.LLM, error)
```

**Supported providers:**

- OpenAI (gpt-4o, gpt-4-turbo, etc.)
- Anthropic (claude-sonnet-4, claude-opus-4, etc.)
- Google Gemini (gemini-2.0-flash, etc.)
- Ollama (local models)

### Embedders

```go
func (r *Runtime) buildEmbedders() error {
    for name, cfg := range r.cfg.Embedders {
        emb, err := r.embedderFactory(cfg)
        r.embedders[name] = emb
    }
}
```

**Providers:**

- OpenAI (text-embedding-3-small, text-embedding-3-large)
- Ollama (local embedding models)
- Cohere (embed-english-v3.0, etc.)

### Toolsets

```go
func (r *Runtime) buildToolsets() error {
    for name, cfg := range r.cfg.Tools {
        if !cfg.IsEnabled() {
            continue
        }
        ts, err := r.toolsetFactory(name, cfg)
        r.toolsets[name] = ts
    }
}
```

**Tool types:**

- **Function**: Built-in Go functions (read_file, execute_command, etc.)
- **MCP**: Model Context Protocol servers
- **Command**: Shell command execution
- **Search**: Web search tools

### Vector Providers

```go
func (r *Runtime) buildVectorProviders() error {
    for name, cfg := range r.cfg.VectorStores {
        provider, err := rag.NewVectorProviderFromConfig(cfg)
        r.vectorProviders[name] = provider
    }
}
```

**Providers:**

- Chromem (embedded, file-based)
- Qdrant (production vector database)
- Pinecone (managed service)
- Weaviate (open-source vector database)
- Milvus (distributed vector database)

### Document Stores

```go
func (r *Runtime) buildDocumentStores() error {
    deps := &rag.FactoryDeps{
        VectorProviders: r.vectorProviders,
        Embedders:       r.embedders,
        LLMs:            r.llms,
    }

    for name, cfg := range r.cfg.DocumentStores {
        store, err := rag.NewDocumentStoreFromConfig(name, cfg, deps)
        r.documentStores[name] = store
    }
}
```

**Source types:**

- Directory (local files)
- SQL database (query results)
- URLs (web pages)
- S3 (cloud storage)

### Agents

```go
func (r *Runtime) buildAgents() error {
    for name, cfg := range r.cfg.Agents {
        agent, err := r.createAgent(name, cfg)
        r.agents[name] = agent
    }
}
```

**Agent creation flow:**

```
1. Resolve LLM reference
2. Gather toolsets by name
3. Build sub-agents (recursive)
4. Create agent instance
   - LLM agent
   - Remote agent
   - Workflow agent
5. Register in agent map
```

## Programmatic API

Inject custom components:

### Custom Sub-Agents

```go
runtime.New(cfg,
    runtime.WithSubAgents("coordinator", []agent.Agent{
        customSpecialist,
    }),
)
```

Sub-agents appear as transfer tools:

```
transfer_to_customSpecialist
```

### Custom Agent Tools

```go
runtime.New(cfg,
    runtime.WithAgentTools("main", []agent.Agent{
        helperAgent,
    }),
)
```

Agents appear as callable tools:

```
agent_call_helperAgent
```

### Direct Tools

```go
runtime.New(cfg,
    runtime.WithDirectTools("assistant", []tool.Tool{
        &MyCustomTool{},
    }),
)
```

### Custom Factories

Replace default factories:

```go
runtime.New(cfg,
    runtime.WithLLMFactory(func(cfg *config.LLMConfig) (model.LLM, error) {
        return &CustomLLM{}, nil
    }),
    runtime.WithEmbedderFactory(customEmbedderFactory),
    runtime.WithToolsetFactory(customToolsetFactory),
)
```

## Session Service

Source of truth for all agent data.

### Backends

**In-Memory (Default):**

```go
sessionSvc := session.NewInMemoryService()
```

Data lost on restart. Use for development.

**SQL (Persistent):**

```go
sessionSvc, err := session.NewSQLService(db)
```

Data persists across restarts. Use for production.

### Session Data

**Messages:**

- Full conversation history
- User and agent messages
- Tool calls and results

**State:**

- Key-value store
- Persisted across invocations
- Temp keys auto-cleared

**Artifacts:**

- Generated files
- Images
- Binary data

## Index Service

Searchable index over conversation history.

### Index Types

**Keyword:**

```yaml
server:
  index:
    type: keyword
```

Simple word matching. No embeddings required.

**Vector:**

```yaml
server:
  index:
    type: vector
    embedder: default
```

Semantic similarity search using embeddings.

### Rebuild from Sessions

Index can be rebuilt from session data:

```go
// Sessions are SOURCE OF TRUTH
sessions.Save(message)

// Index built from sessions
index.Index(message)

// If index corrupted
index.Rebuild(sessions.GetAll())
```

## Observability

Integrated tracing and metrics.

### Initialization

```go
obs, err := observability.NewManager(ctx, cfg.Server.Observability)

runtime.New(cfg,
    runtime.WithObservability(obs),
)
```

### Instrumentation

Runtime automatically instruments:

- LLM calls (tokens, latency)
- Tool executions (duration, status)
- Agent invocations (path, timing)
- Database queries (query, duration)

### Metrics

Exposed at `/metrics`:

```
hector_llm_requests_total
hector_llm_tokens_total
hector_tool_calls_total
hector_agent_requests_total
```

### Traces

Sent to OTLP endpoint:

```
Invocation Span
  ├─ Agent Span
  │   ├─ LLM Span
  │   ├─ Tool Span
  │   └─ Database Span
  └─ ...
```

## Hot Reload

Runtime supports live configuration reload.

### Reload Process

```go
func (r *Runtime) Reload(newCfg *config.Config) error {
    // 1. Validate new config
    newCfg.Validate()

    // 2. Build new components
    newLLMs := buildLLMs(newCfg)
    newEmbedders := buildEmbedders(newCfg)
    newToolsets := buildToolsets(newCfg)
    newAgents := buildAgents(newCfg)

    // 3. Atomic swap (preserve sessions)
    r.llms = newLLMs
    r.embedders = newEmbedders
    r.toolsets = newToolsets
    r.agents = newAgents

    // 4. Cleanup old components
    cleanup(oldLLMs, oldEmbedders, oldToolsets)
}
```

### What Reloads

- LLM configurations
- Agent definitions
- Tool configurations
- RAG document stores
- Embedder settings

### What Doesn't Reload

- Active sessions (preserved)
- Session service (retained)
- Index service (retained)
- Server port/TLS (requires restart)

### Rollback on Error

```go
if err := buildNewComponents(); err != nil {
    r.cfg = oldCfg  // Rollback
    cleanup(newComponents)
    return err
}
```

Configuration validation failure preserves old runtime.

## Checkpoint Manager

Manages execution state for recovery.

### Initialization

```go
cpMgr := checkpoint.NewManager(cfg, sessionSvc)

runtime.New(cfg,
    runtime.WithCheckpointManager(cpMgr),
)
```

### Strategies

**Event:**

```yaml
server:
  checkpoint:
    strategy: event
    after_tools: true
    before_llm: true
```

Checkpoint at specific events (tool execution, LLM calls).

**Interval:**

```yaml
server:
  checkpoint:
    strategy: interval
    interval: 30s
```

Checkpoint every N seconds.

**Hybrid:**

```yaml
server:
  checkpoint:
    strategy: hybrid
    after_tools: true
    interval: 30s
```

Both events and intervals.

### Recovery

Auto-resume on restart:

```yaml
server:
  checkpoint:
    recovery:
      auto_resume: true
      auto_resume_hitl: false
      timeout: 86400  # 24h
```

**Flow:**

```
1. Server restarts
2. Checkpoint manager queries sessions
3. Find incomplete tasks with checkpoints
4. Resume non-HITL tasks automatically
5. Await approval for HITL tasks
```

## Lifecycle Management

### Startup

```go
// 1. Create runtime
runtime, err := runtime.New(cfg)

// 2. Create server executors
executors := make(map[string]*server.Executor)
for name, agent := range runtime.Agents() {
    executors[name] = server.NewExecutor(agent, runtime.Runner())
}

// 3. Start HTTP server
httpServer := server.NewHTTPServer(cfg, executors)
httpServer.Start(ctx)
```

### Shutdown

```go
// 1. Stop HTTP server
httpServer.Shutdown(ctx)

// 2. Close runtime components
runtime.Close()
  ├─ Close LLMs
  ├─ Close embedders
  ├─ Close toolsets
  ├─ Shutdown observability
  └─ (Sessions/index preserved)
```

### Update (Hot Reload)

```go
// 1. Watch config file
watcher.OnChange(func(newCfg *config.Config) {
    // 2. Reload runtime
    runtime.Reload(newCfg)

    // 3. Update HTTP server executors
    executors := rebuildExecutors(runtime)
    httpServer.UpdateExecutors(cfg, executors)
})
```

## Factory Patterns

### Default Factories

```go
func DefaultLLMFactory(cfg *config.LLMConfig) (model.LLM, error) {
    switch cfg.Provider {
    case "openai":
        return openai.New(cfg)
    case "anthropic":
        return anthropic.New(cfg)
    case "gemini":
        return gemini.New(cfg)
    case "ollama":
        return ollama.New(cfg)
    }
}
```

### Custom Factories

```go
customFactory := func(cfg *config.LLMConfig) (model.LLM, error) {
    if cfg.Provider == "custom" {
        return &CustomLLM{
            apiKey: cfg.APIKey,
            model:  cfg.Model,
        }, nil
    }
    return DefaultLLMFactory(cfg)
}

runtime.New(cfg,
    runtime.WithLLMFactory(customFactory),
)
```

## Concurrency

Runtime uses read-write locks for safe hot reload:

```go
func (r *Runtime) Agents() map[string]agent.Agent {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.agents
}

func (r *Runtime) Reload(cfg *config.Config) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    // Atomic component swap
}
```

**Safe patterns:**

- Read operations: RLock (concurrent)
- Reload operations: Lock (exclusive)
- Component swaps: Atomic

## Configuration Validation

Runtime validates configuration before building:

```go
func (r *Runtime) Reload(newCfg *config.Config) error {
    // Validate first
    if err := newCfg.Validate(); err != nil {
        return err  // No changes made
    }

    // Build components
    ...
}
```

**Validation checks:**

- Required fields present
- Referenced components exist
- Type compatibility
- Schema conformance

## Error Handling

### Build Errors

```go
if err := runtime.buildLLMs(); err != nil {
    return fmt.Errorf("failed to build LLMs: %w", err)
}
```

Errors propagate with context. Partial builds are cleaned up.

### Reload Errors

```go
if err := buildNewAgents(); err != nil {
    cleanup(newLLMs, newEmbedders, newToolsets)
    r.cfg = oldCfg  // Rollback
    return err
}
```

Failed reloads preserve old runtime state.

## Best Practices

### Factory Injection

Use factories for testability:

```go
// Test with mock factory
testRuntime := runtime.New(cfg,
    runtime.WithLLMFactory(mockLLMFactory),
)
```

### Session Persistence

Use SQL backends in production:

```yaml
server:
  sessions:
    backend: sql
    database: main
```

### Observability

Enable in production:

```yaml
server:
  observability:
    tracing:
      enabled: true
      endpoint: jaeger:4317
    metrics:
      enabled: true
```

### Hot Reload

Enable watch mode for development:

```bash
hector serve --config config.yaml --watch
```

## Next Steps

- [Memory](memory.md) - Session and index architecture
- [Tools](tools.md) - Tool system details
- [Configuration](configuration.md) - Configuration internals
