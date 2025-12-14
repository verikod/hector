# Memory

Hector's memory system manages conversation history, state, and semantic search across sessions.

## Architecture

**Two-Layer Design:**

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

## Session Service

Manages conversation sessions.

### Session Structure

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

**Components:**

- **ID**: Unique session identifier
- **AppName**: Application scope
- **UserID**: User scope
- **State**: Key-value store
- **Events**: Full conversation history
- **LastUpdateTime**: Modification timestamp

### Session Operations

**Create:**

```go
session, err := sessions.Create(ctx, &session.CreateRequest{
    AppName: "my-app",
    UserID:  "user-123",
    State:   map[string]any{"counter": 0},
})
```

**Get:**

```go
resp, err := sessions.Get(ctx, &session.GetRequest{
    AppName:   "my-app",
    UserID:    "user-123",
    SessionID: "session-456",
})
```

**Append Event:**

```go
err := sessions.AppendEvent(ctx, session, event)
```

**List:**

```go
resp, err := sessions.List(ctx, &session.ListRequest{
    AppName:  "my-app",
    UserID:   "user-123",
    PageSize: 50,
})
```

### Backends

**In-Memory:**

```yaml
server:
  sessions:
    backend: inmemory
```

Data lost on restart. Use for development.

**SQL:**

```yaml
server:
  sessions:
    backend: sql
    database: main
```

Persistent storage. Use for production.

**Supported databases:**

- SQLite (embedded)
- PostgreSQL (production)
- MySQL (alternative)

## State Management

Sessions have a scoped key-value store.

### State Scopes

```go
const (
    KeyPrefixApp  = "app:"   // Shared across all users/sessions
    KeyPrefixUser = "user:"  // Shared across user's sessions
    KeyPrefixTemp = "temp:"  // Cleared after invocation
)
```

### State Operations

**Get:**

```go
value, err := ctx.State().Get("counter")
```

**Set:**

```go
err := ctx.State().Set("counter", 42)
```

**Delete:**

```go
err := ctx.State().Delete("counter")
```

**Iterate:**

```go
for key, value := range ctx.State().All() {
    // Process key-value pairs
}
```

### Scoped Keys

**Session-level (default):**

```go
ctx.State().Set("last_search", "weather")
```

Persists within session only.

**User-level:**

```go
ctx.State().Set("user:preferences", map[string]any{
    "theme": "dark",
    "lang":  "en",
})
```

Shared across user's sessions.

**App-level:**

```go
ctx.State().Set("app:version", "2.0.0")
```

Shared globally across all users and sessions.

**Temporary:**

```go
ctx.State().Set("temp:processing", true)
```

Auto-cleared after invocation completes.

### Auto-Cleanup

Temp keys cleared automatically:

```go
// Before invocation
ctx.State().Set("temp:data", largeObject)

// After invocation completes
// temp:data is automatically deleted
```

## Events

Sessions store full conversation history.

### Event Structure

```go
type Event struct {
    ID           string
    InvocationID string
    Author       string         // Agent name or "user"
    Branch       string         // Agent hierarchy path
    Message      *a2a.Message
    Actions      Actions
    Timestamp    time.Time
    Metadata     map[string]any
}
```

### Event Access

**All events:**

```go
for event := range session.Events().All() {
    // Process event
}
```

**By index:**

```go
event := session.Events().At(5)
```

**Count:**

```go
count := session.Events().Len()
```

### Event Types

**User messages:**

```go
{
    "author": "user",
    "message": {
        "role": "user",
        "parts": [{"type": "text", "text": "Hello"}]
    }
}
```

**Agent responses:**

```go
{
    "author": "assistant",
    "message": {
        "role": "agent",
        "parts": [{"type": "text", "text": "Hi!"}]
    }
}
```

**Tool executions:**

```go
{
    "author": "assistant",
    "actions": {
        "tool_calls": [{
            "name": "search",
            "arguments": {"query": "weather"}
        }]
    }
}
```

## Index Service

Semantic search over conversation history.

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

Semantic similarity using embeddings.

### Indexing Flow

```
1. User sends message
2. Agent processes and responds
3. Session service saves events
4. Index service indexes events
```

**Code:**

```go
// 1. Save to session (SOURCE OF TRUTH)
sessions.AppendEvent(ctx, session, event)

// 2. Index for search
index.Index(ctx, session)
```

### Search

**Keyword search:**

```go
resp, err := index.Search(ctx, &memory.SearchRequest{
    Query:   "weather forecast",
    AppName: "my-app",
    UserID:  "user-123",
})
```

Returns events with matching keywords.

**Semantic search:**

```go
resp, err := index.Search(ctx, &memory.SearchRequest{
    Query:   "What did I ask about the weather?",
    AppName: "my-app",
    UserID:  "user-123",
})
```

Returns events with semantic similarity (via embeddings).

### Search Results

```go
type SearchResult struct {
    SessionID string
    EventID   string
    Content   string
    Author    string
    Timestamp time.Time
    Score     float64       // Relevance score
    Metadata  map[string]any
}
```

Results ordered by score (highest first).

### Rebuild Index

Index can be rebuilt from sessions:

```go
err := index.Rebuild(ctx, sessions, "my-app", "user-123")
```

**Use cases:**

- Index corruption
- Format migration
- Initial population

**Process:**
1. Clear existing index entries
2. Load all sessions from session service
3. Index each session

## Working Memory

Manages LLM context window by filtering conversation history.

### Strategies

**All (No Filtering):**

```yaml
agents:
  assistant:
    working_memory:
      type: all
```

Include entire conversation history.

**Buffer Window (Recent N):**

```yaml
agents:
  assistant:
    working_memory:
      type: buffer_window
      max_messages: 20
```

Keep only last N messages. Fast and simple.

**Token Window (Token Budget):**

```yaml
agents:
  assistant:
    working_memory:
      type: token_window
      max_tokens: 4000
```

Keep messages within token budget. Accurate but slower.

**Summary Buffer (Compress Old):**

```yaml
agents:
  assistant:
    working_memory:
      type: summary_buffer
      max_messages: 50
      summary_threshold: 30
```

Summarize messages beyond threshold.

### Strategy Interface

```go
type WorkingMemoryStrategy interface {
    FilterEvents(events []*agent.Event) []*agent.Event
    CheckAndSummarize(ctx context.Context, events []*agent.Event) (*agent.Event, error)
}
```

**FilterEvents:** Called before LLM call to trim context.

**CheckAndSummarize:** Called after turn to compress history.

### Buffer Window Example

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

Keeps last N messages only.

### Summary Buffer Flow

```
1. Turn completes
2. Check if messages > threshold
3. If yes:
   - Summarize old messages (beyond max)
   - Create summary event
   - Save to session
4. Filter events for next LLM call
   - Recent messages: full text
   - Old messages: summary
```

## Cross-Session Memory

Recall information across conversations.

### Agent Search Tool

```yaml
agents:
  assistant:
    llm: gpt-4o
    search:
      enabled: true
      max_results: 5
```

Agent can search past conversations:

```
User: "What did I ask about yesterday?"

Agent calls search("yesterday's questions")
  → Finds past conversations
  → Uses context to answer
```

### Search Tool Schema

```json
{
  "name": "search_memory",
  "description": "Search past conversations",
  "parameters": {
    "query": {
      "type": "string",
      "description": "Search query"
    }
  }
}
```

### Implementation

```go
func searchTool(ctx tool.Context, args map[string]any) (map[string]any, error) {
    query := args["query"].(string)

    resp, err := index.Search(ctx, &memory.SearchRequest{
        Query:   query,
        AppName: ctx.AppName(),
        UserID:  ctx.UserID(),
    })

    return map[string]any{
        "results": resp.Results,
    }, nil
}
```

## Artifacts

Sessions can store generated files.

### Store Artifact

```go
ctx.Artifacts().Save(&agent.Artifact{
    Name:     "chart.png",
    MimeType: "image/png",
    Data:     imageBytes,
})
```

### List Artifacts

```go
for artifact := range ctx.Artifacts().All() {
    fmt.Println(artifact.Name)
}
```

### Retrieve Artifact

```go
artifact, err := ctx.Artifacts().Get("chart.png")
```

Artifacts persist with session.

## Persistence Model

### Session Tables (SQL)

**sessions:**

- `id` - Session UUID
- `app_name` - Application scope
- `user_id` - User scope
- `state` - State JSON
- `created_at` - Creation timestamp
- `updated_at` - Update timestamp

**events:**

- `id` - Event UUID
- `session_id` - Session reference
- `invocation_id` - Invocation ID
- `author` - Agent name or "user"
- `branch` - Agent hierarchy path
- `message` - Message JSON
- `actions` - Actions JSON
- `timestamp` - Event timestamp
- `metadata` - Metadata JSON

**artifacts:**

- `id` - Artifact UUID
- `session_id` - Session reference
- `name` - File name
- `mime_type` - Content type
- `data` - Binary data
- `created_at` - Upload timestamp

### Index Storage

**Keyword index:**

- In-memory hash maps
- No persistence required

**Vector index:**

- Chromem: File-based persistence
- Qdrant: Native persistence
- Pinecone: Cloud-managed
- Weaviate: Native persistence

## Memory Lifecycle

### Initialization

```go
// 1. Create session service
sessions, err := session.NewSessionServiceFromConfig(cfg)

// 2. Create index service
index, err := memory.NewIndexServiceFromConfig(cfg, embedders)

// 3. Rebuild index (if needed)
if !index.IsPersisted() {
    index.Rebuild(ctx, sessions, "app", "user")
}
```

### Per-Invocation

```
1. Load session
   sessions.Get(ctx, req)

2. Filter events (working memory)
   strategy.FilterEvents(session.Events())

3. Run agent
   agent.Run(ctx)

4. Save events
   sessions.AppendEvent(ctx, session, event)

5. Index events
   index.Index(ctx, session)

6. Summarize (if needed)
   strategy.CheckAndSummarize(ctx, events)

7. Clear temp state
   state.ClearTempKeys()
```

### Cleanup

**Delete session:**

```go
// 1. Delete from session service
sessions.Delete(ctx, req)

// 2. Clear from index
index.Clear(ctx, appName, userID, sessionID)
```

## Best Practices

### Working Memory Selection

**Short conversations:**

```yaml
working_memory:
  type: all
```

No filtering needed.

**Long conversations:**

```yaml
working_memory:
  type: buffer_window
  max_messages: 50
```

Keep recent context only.

**Very long conversations:**

```yaml
working_memory:
  type: summary_buffer
  max_messages: 100
  summary_threshold: 50
```

Compress old messages.

### State Scoping

Use appropriate prefixes:

```go
// ✅ Good - Session-specific
ctx.State().Set("current_topic", "weather")

// ✅ Good - User preferences
ctx.State().Set("user:theme", "dark")

// ✅ Good - App config
ctx.State().Set("app:version", "2.0.0")

// ✅ Good - Temporary data
ctx.State().Set("temp:large_result", data)
```

### Index Rebuild

Schedule periodic rebuilds:

```bash
# Rebuild index from sessions
hector index rebuild --app my-app --user user-123
```

Or rebuild on startup if index file missing.

### Search Scope

Always scope searches to user:

```go
// ✅ Good - User-scoped
index.Search(ctx, &memory.SearchRequest{
    Query:   query,
    AppName: appName,
    UserID:  userID,  // User isolation
})

// ❌ Bad - No user scope
index.Search(ctx, &memory.SearchRequest{
    Query: query,
    // Missing AppName and UserID
})
```

## Performance

### Session Loading

**Partial loading:**

```go
sessions.Get(ctx, &session.GetRequest{
    SessionID:       sessionID,
    NumRecentEvents: 100,  // Load last 100 only
})
```

Faster for large sessions with working memory.

**Time filtering:**

```go
sessions.Get(ctx, &session.GetRequest{
    SessionID: sessionID,
    After:     time.Now().Add(-24 * time.Hour),  // Last 24h
})
```

### Index Performance

**Keyword:**

- Fast indexing
- Fast search
- No embeddings required

**Vector:**

- Slower indexing (embeddings)
- Semantic search
- Higher memory usage

### Caching

Session service can cache frequently accessed sessions:

```go
type CachedSessionService struct {
    backend session.Service
    cache   *lru.Cache
}
```

Reduces database queries.

## Next Steps

- [Tools](tools.md) - Tool system architecture
- [RAG](rag.md) - Document stores and context injection
- [Configuration](configuration.md) - Memory configuration options
