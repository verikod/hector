# Architecture Reference

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

**Responsibilities:**

- Build LLM providers from configuration
- Create embedders for semantic search
- Initialize tools and toolsets
- Construct agents with dependencies
- Manage RAG document stores
- Setup session and memory services
- Coordinate hot reload

**Lifecycle:**
1. Load configuration
2. Create LLM providers
3. Initialize embedders
4. Build tools and toolsets
5. Create agents
6. Setup persistence (sessions, tasks)
7. Start observability

**Dependency Graph:**

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

### Server

HTTP/gRPC server exposing A2A protocol endpoints:

**Endpoints:**

| Endpoint | Description |
|----------|-------------|
| `/.well-known/agent-card.json` | Default agent card |
| `/agents` | Multi-agent discovery (Hector extension) |
| `/agents/{name}` | Agent card (GET) / JSON-RPC (POST) |
| `/health` | Health check and auth discovery |
| `/metrics` | Prometheus metrics (when enabled) |
| `/api/schema` | JSON Schema for configuration |
| `/api/config` | Config read/write (studio mode) |
| `/api/tasks/.../cancel` | Cancel tool execution |

**Transports:**

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

**Storage Backends:**

| Backend | Persistence | Use Case |
|---------|-------------|----------|
| In-memory | Ephemeral | Development |
| SQL | Persistent | Production |

**Responsibilities:**

- Store messages
- Manage session state
- Track artifacts
- Persist across restarts

**Architecture:**
```
Session Service (SOURCE OF TRUTH)
    │
    ├─ Messages (conversation history)
    ├─ State (key-value pairs)
    └─ Artifacts (files, images)
```

### Memory Index

Searchable index over conversation history:

**Index Types:**

| Type | Description | Requirements |
|------|-------------|--------------|
| Keyword | Simple word matching | None |
| Vector | Semantic similarity | Embedder required |

**Use Cases:**

- Search past conversations
- Find relevant context
- Knowledge retrieval

**Architecture:**
```
Session Service → Index Service → Vector Provider
     (data)          (search)        (storage)
```

Index can be rebuilt from session data.

### Checkpoint Manager

Execution state checkpointing for recovery:

**Strategies:**

| Strategy | When | Description |
|----------|------|-------------|
| Event | Tool execution, LLM calls | Checkpoint at specific events |
| Interval | Every N iterations | Checkpoint at regular intervals |
| Hybrid | Both | Events and intervals combined |

**Configuration:**

```yaml
storage:
  checkpoint:
    enabled: true
    strategy: hybrid
    after_tools: true
    before_llm: true
    interval: 5  # every 5 iterations
```

**Storage:**

- Checkpoints stored in session service
- Auto-cleanup of expired checkpoints

**Recovery:**

- Auto-resume on startup
- Manual recovery via API
- HITL approval for sensitive tasks

**Recovery Configuration:**

```yaml
storage:
  checkpoint:
    recovery:
      auto_resume: true
      auto_resume_hitl: false
      timeout: 3600  # 1h (in seconds)
```

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

**Metrics (exposed at `/metrics`):**

| Metric | Type | Description |
|--------|------|-------------|
| `hector_llm_requests_total` | Counter | Total LLM requests |
| `hector_llm_tokens_total` | Counter | Total tokens used |
| `hector_tool_calls_total` | Counter | Total tool calls |
| `hector_agent_requests_total` | Counter | Total agent requests |

**Traces (sent to OTLP endpoint):**

```
Invocation Span
  ├─ Agent Span
  │   ├─ LLM Span
  │   ├─ Tool Span
  │   └─ Database Span
  └─ ...
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
   ├─> Validation
          └─ Schema check

   ├─> SKILL.md Detection
          └─ Auto-configure instruction & tools

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

**What Reloads:**

- LLM configurations
- Agent definitions
- Tool configurations
- RAG document stores
- Embedder settings

**What Doesn't Reload:**

- Active sessions (preserved)
- Session service (retained)
- Index service (retained)
- Server port/TLS (requires restart)

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

**Stateless Design:**

- No in-process state
- All state in database
- Instances interchangeable

### Resource Efficiency

| Metric | Value |
|--------|-------|
| Binary Size | 30MB (stripped) |
| Memory Footprint | ~50-100MB baseline |
| Startup Time | <100ms |
| Goroutines | Efficient concurrency |

**Comparison to Python frameworks:**

- 10-20x less memory
- 20-100x faster startup
- Single binary deployment

## Performance Characteristics

**Request Latency:**

| Component | Latency |
|-----------|---------|
| Hector Overhead | <10ms (routing, parsing) |
| LLM Call | 500ms - 10s (dominates) |
| Tool Execution | 10ms - 1s (varies) |
| Database Query | 1-10ms (local) |

**Throughput:**

| Scenario | Requests/sec |
|----------|--------------|
| Non-LLM bottleneck | 100-1000 req/s |
| LLM-limited | ~10-50 req/s (per provider) |
| Horizontal scaling | Linear with instances |

**Resource Usage:**

| Resource | Notes |
|----------|-------|
| CPU | Low baseline, spikes during LLM |
| Memory | ~50MB + sessions + vector data |
| Network | LLM API dominant |
| Disk | SQLite or logs only |

## Design Principles

1. **Config-First**: Agents defined declaratively, not in code
2. **A2A-Native**: Full A2A v1.0 (DRAFT) protocol compliance
3. **Production-Ready**: Observability, security, persistence built-in
4. **Resource Efficient**: Go implementation, minimal footprint
5. **Extensible**: Programmatic API, custom tools, custom LLMs
6. **Stateless**: Horizontal scaling via shared database
7. **Standards-Based**: OpenTelemetry, Prometheus, JWKS

## Build Phases

Runtime builds components in dependency order:

| Phase | Components |
|-------|------------|
| 1 | Observability (tracing & metrics) |
| 2 | Session Service (data persistence) |
| 3 | LLM Providers (language models) |
| 4 | Embedders (semantic embeddings) |
| 5 | Vector Stores (vector databases) |
| 6 | Toolsets (tools for agents) |
| 7 | Document Stores (RAG sources) |
| 8 | Index Service (search capability) |
| 9 | Agents (configured agents) |

## Supported Providers

### LLM Providers

| Provider | Models |
|----------|--------|
| OpenAI | gpt-4o, gpt-4-turbo, gpt-4o-mini, etc. |
| Anthropic | claude-sonnet-4, claude-opus-4, etc. |
| Google Gemini | gemini-2.5-pro, etc. |
| Ollama | Local models |

### Embedders

| Provider | Models |
|----------|--------|
| OpenAI | text-embedding-3-small, text-embedding-3-large |
| Ollama | Local embedding models |
| Cohere | embed-english-v3.0, etc. |

### Vector Stores

| Provider | Type |
|----------|------|
| Chromem | Embedded, file-based |
| Qdrant | Production vector database |
| Chroma | Open-source embedding database |
| Pinecone | Managed service |
| Weaviate | Open-source vector database |
| Milvus | Distributed vector database |

### Tool Types

| Type | Description |
|------|-------------|
| Function | Built-in Go functions (text_editor, bash, etc.) |
| MCP | Model Context Protocol servers |
| Command | Shell command execution |
| Search | Web search tools |

### Document Sources

| Type | Description |
|------|-------------|
| Directory | Local files |
| SQL | Database query results |
| URLs | Web pages |
| S3 | Cloud storage |
