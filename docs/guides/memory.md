# Memory & Sessions

Hector's memory system manages conversation history, state, and semantic search across sessions.

## Architecture

Hector uses a two-layer design:

```
Session Service (SOURCE OF TRUTH)
    │
    ├─ Messages (full conversation history)
    ├─ State (key-value store)
    └─ Artifacts (files, images)

Index Service (SEARCH INDEX)
    │
    └─ Built from session events
       (can be rebuilt at any time)
```

**Key principles:**

- Session service is the single source of truth
- Index service is a derived search index
- Index can be rebuilt from sessions
- No data loss if index corrupted

## Session Configuration

### In-Memory (Development)

```yaml
storage:
  sessions:
    backend: inmemory
```

Data lost on restart. Use for development.

### SQL (Production)

```yaml
storage:
  sessions:
    backend: sql
    database: main
```

Persistent storage across restarts.

**Supported databases:**

- SQLite (embedded)
- PostgreSQL (production)
- MySQL (alternative)

**Complete Example:**

```yaml
databases:
  main:
    driver: postgres
    host: localhost
    port: 5432
    database: hector
    user: ${DB_USER}
    password: ${DB_PASSWORD}

storage:
  sessions:
    backend: sql
    database: main
```

## State Management

Sessions include a scoped key-value store for persisting data.

### State Scopes

| Prefix | Scope | Lifecycle |
|--------|-------|-----------|
| (none) | Session | Persists within session only |
| `user:` | User | Shared across user's sessions |
| `app:` | Application | Global across all users |
| `temp:` | Temporary | Auto-cleared after invocation |

### Usage Examples

**Session-level state** (default):

```yaml
# Agent can store session-specific data
# Example: tracking conversation topic
```

The agent stores data like `last_search`, `current_topic` that persists only within the current session.

**User-level state**:

Store user preferences that persist across sessions:

```yaml
# Data stored with "user:" prefix
# Example: user:theme, user:language, user:preferences
```

**App-level state**:

Global data shared across all users:

```yaml
# Data stored with "app:" prefix
# Example: app:version, app:announcement
```

**Temporary state**:

Data that's automatically cleared after each invocation:

```yaml
# Data stored with "temp:" prefix
# Example: temp:processing, temp:intermediate_result
```

## Working Memory

Working memory manages the LLM context window by filtering conversation history.

### No Strategy (Default)

Include entire conversation history:

```yaml
agents:
  assistant:
    context:
      strategy: none  # Default
```

Best for: Short conversations where full context is needed.

### Buffer Window

Keep only the last N messages:

```yaml
agents:
  assistant:
    context:
      strategy: buffer_window
      window_size: 20  # Keep last 20 messages
```

Best for: Medium-length conversations, simple use cases.

### Token Window

Keep messages within a token budget:

```yaml
agents:
  assistant:
    context:
      strategy: token_window
      budget: 8000         # Max tokens
      preserve_recent: 5   # Always keep last 5 messages
```

Best for: Precise context control, cost optimization.

### Summary Buffer

Summarize old messages when exceeding budget:

```yaml
agents:
  assistant:
    context:
      strategy: summary_buffer
      budget: 8000           # Token budget
      threshold: 0.85        # Summarize at 85% usage
      target: 0.7            # Reduce to 70% after summarizing
      summarizer_llm: fast   # Use cheaper model for summarization
```

Best for: Long conversations where context matters.

**How it works:**

1. Conversation exceeds threshold (85% of budget)
2. Old messages are summarized using the summarizer LLM
3. Summary replaces the old messages
4. Context reduced to target level (70%)
5. Recent messages preserved in full

## Memory Index

Searchable index over conversation history enables cross-session recall.

### Keyword Index

Simple word matching, no embeddings required:

```yaml
storage:
  memory:
    backend: keyword
```

### Vector Index

Semantic similarity using embeddings:

```yaml
storage:
  memory:
    backend: vector
    embedder: default
```

Requires an embedder configuration:

```yaml
embedders:
  default:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}

storage:
  memory:
    backend: vector
    embedder: default
```

## Cross-Session Memory

Enable agents to recall information from past conversations.

### Search Tool

```yaml
agents:
  assistant:
    llm: gpt-4o
    search:
      enabled: true
      max_results: 5
```

With search enabled, the agent can access past conversations:

```
User: "What did I ask about yesterday?"

Agent searches past conversations
  → Finds relevant history
  → Uses context to answer
```

## Artifacts

Sessions can store generated files.

### Artifact Storage

Artifacts are binary files (images, documents, etc.) that persist with the session:

- Images generated by the agent
- Documents created during conversation
- Any binary data the agent produces

### Usage

Agents can save and retrieve artifacts during execution. Artifacts are stored in the session and persist with it.

## Session Lifecycle

### Session Creation

Sessions are created automatically when a user starts a conversation:

1. User sends first message
2. Session created with unique ID
3. State initialized (empty or with defaults)
4. Events collection started

### During Conversation

Each invocation follows this flow:

1. Load session
2. Filter events (working memory strategy)
3. Run agent
4. Save new events
5. Index events (for search)
6. Summarize if needed
7. Clear temporary state

### Session Cleanup

Sessions can be deleted when no longer needed:

- Manually via API
- Automatically after expiration (configurable)
- On user request

## Best Practices

### Choosing Working Memory Strategy

**Short conversations** (customer support, Q&A):

```yaml
context:
  strategy: buffer_window
  window_size: 20
```

**Long conversations** (research, analysis):

```yaml
context:
  strategy: summary_buffer
  budget: 8000
  threshold: 0.85
  target: 0.7
```

**Cost-sensitive applications**:

```yaml
context:
  strategy: token_window
  budget: 4000
  preserve_recent: 3
```

### State Scoping

Use appropriate prefixes for data:

- **No prefix**: Session-specific data (current topic, last action)
- **user:**: User preferences (theme, language, settings)
- **app:**: Global configuration (version, announcements)
- **temp:**: Processing data (intermediate results)

### Index Configuration

**Development**: Use keyword index (simpler, no embeddings needed)

```yaml
storage:
  memory:
    backend: keyword
```

**Production**: Use vector index for semantic search

```yaml
storage:
  memory:
    backend: vector
    embedder: default
```

### Database Selection

| Use Case | Database | Configuration |
|----------|----------|---------------|
| Development | In-memory | `backend: inmemory` |
| Single server | SQLite | `driver: sqlite` |
| Production | PostgreSQL | `driver: postgres` |
| Alternative | MySQL | `driver: mysql` |

## Examples

### Basic Session Persistence

```yaml
databases:
  main:
    driver: sqlite
    path: .hector/hector.db

storage:
  sessions:
    backend: sql
    database: main
```

### Production Configuration

```yaml
databases:
  main:
    driver: postgres
    host: ${DB_HOST}
    port: 5432
    database: hector
    user: ${DB_USER}
    password: ${DB_PASSWORD}

embedders:
  default:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}

storage:
  sessions:
    backend: sql
    database: main

  index:
    type: vector
    embedder: default

agents:
  assistant:
    llm: default
    context:
      strategy: summary_buffer
      budget: 8000
      threshold: 0.85
      target: 0.7
```

### Multi-Agent with Shared Memory

```yaml
agents:
  coordinator:
    llm: default
    sub_agents: [researcher, writer]
    context:
      strategy: buffer_window
      window_size: 30

  researcher:
    llm: default
    tools: [search]
    context:
      strategy: buffer_window
      window_size: 20

  writer:
    llm: default
    tools: [write_file]
    context:
      strategy: buffer_window
      window_size: 20
```

All agents share the same session when working together.

## Next Steps

- [Agents Guide](agents.md) - Configure agent memory settings
- [Persistence Guide](persistence.md) - Database configuration details
- [Programmatic API Reference](../reference/programmatic.md) - Session APIs in Go
