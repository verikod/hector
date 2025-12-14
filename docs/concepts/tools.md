# Tools

Tools extend agent capabilities by allowing interaction with external systems, data sources, and services.

## Tool Architecture

### Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    Schema() map[string]any
    Execute(ctx Context, args map[string]any) (map[string]any, error)
}
```

**Components**

- **Name**: Unique identifier for the tool
- **Description**: Natural language explanation for LLMs
- **Schema**: JSON Schema for parameters
- **Execute**: Implementation logic

### Tool Context

```go
type Context interface {
    context.Context
    InvocationID() string
    AgentName() string
    UserID() string
    SessionID() string
    AppName() string
    State() ReadonlyState
}
```

Provides access to invocation data and session state.

## Tool Types

### Function Tools

Built-in Go functions exposed as tools.

```go
type FunctionTool struct {
    name        string
    description string
    schema      map[string]any
    handler     func(Context, map[string]any) (map[string]any, error)
}
```

**Examples:**

- `read_file`: Read file contents
- `write_file`: Write file to disk
- `execute_command`: Run shell commands
- `web_request`: HTTP requests

### MCP Tools

Model Context Protocol servers expose tools dynamically.

```yaml
tools:
  mcp_server:
    type: mcp
    url: http://localhost:3000
```

**Capabilities:**

- Dynamic tool discovery
- Remote tool execution
- Multiple tools per server
- Protocol-based communication

### Agent Tools

Agents wrapped as tools for delegation pattern.

```go
agentTool := agenttool.New(agenttool.Config{
    Agent:       helperAgent,
    Name:        "agent_call_helper",
    Description: "Delegates to helper agent",
})
```

**Use cases:**

- Specialized sub-tasks
- Result-oriented delegation
- Modular agent composition

### Control Tools

Special tools for agent flow control.

**Transfer Tools:**

```
transfer_to_{agent_name}
```

Automatically created for each sub-agent. Transfers conversation control.

**Exit Loop:**

```
exit_loop(reason: string)
```

Explicitly terminates reasoning loop.

**Escalate:**

```
escalate(reason: string)
```

Returns control to parent agent.

## Toolsets

Toolsets group related tools.

```go
type Toolset interface {
    Name() string
    ListTools(ctx context.Context) ([]Tool, error)
    GetTool(ctx context.Context, name string) (Tool, error)
    Execute(ctx Context, name string, args map[string]any) (map[string]any, error)
}
```

**Benefits:**

- Namespace isolation
- Dynamic tool resolution
- Lazy loading
- Resource management

### MCP Toolset

```go
type MCPToolset struct {
    client *mcpclient.Client
    tools  []Tool  // Cached from server
}
```

**Lifecycle:**
1. Connect to MCP server
2. Discover available tools
3. Cache tool schemas
4. Execute tool requests
5. Return results

### Built-in Toolset

```go
type BuiltinToolset struct {
    tools map[string]Tool
}
```

Provides core Hector functionality.

## Tool Execution

### Execution Flow

```
1. LLM generates tool call
   {
     "name": "search",
     "arguments": {"query": "weather"}
   }

2. Tool resolution
   toolset.GetTool(ctx, "search")

3. Approval check (if required)
   if tool.RequireApproval() {
       waitForApproval()
   }

4. Sandboxing (for commands)
   validateCommand(args)

5. Execution
   result, err := tool.Execute(ctx, args)

6. Result processing
   return result to LLM
```

### Execution Context

Tools receive read-only context:

```go
func (t *SearchTool) Execute(ctx tool.Context, args map[string]any) (map[string]any, error) {
    query := args["query"].(string)

    // Access session state
    prefs, _ := ctx.State().Get("user:search_prefs")

    // Execute search
    results := search(query, prefs)

    return map[string]any{
        "results": results,
    }, nil
}
```

### Error Handling

Tool errors returned to LLM:

```go
if err != nil {
    return nil, fmt.Errorf("search failed: %w", err)
}
```

LLM receives error message and can retry or adjust.

## Tool Security

### Approval Requirement (HITL)

```yaml
tools:
  write_file:
    type: function
    handler: write_file
    require_approval: true
    approval_prompt: "Allow writing to {file}?"
```

**Flow:**
1. LLM calls tool
2. Execution pauses
3. User receives approval request
4. User approves/denies
5. Execution continues or fails

### Command Sandboxing

```yaml
tools:
  execute_command:
    type: command
    working_directory: ./workspace
    deny_by_default: true
    allowed_commands:
      - ls
      - cat
      - grep
    max_execution_time: 30s
```

**Security layers:**

- Command whitelist
- Working directory restriction
- Execution timeout
- Output size limits

### Tool Visibility

```yaml
tools:
  internal_tool:
    enabled: true
    visibility: internal  # Requires authentication
```

Tools can be scoped to authenticated users.

## Tool Discovery

### Static Discovery

Tools configured in YAML:

```yaml
agents:
  assistant:
    tools:
      - read_file
      - write_file
      - search
```

Resolved at agent build time.

### Dynamic Discovery

MCP servers provide dynamic tools:

```go
// Connect to MCP server
client := mcpclient.New(url)

// Discover tools
tools, err := client.ListTools(ctx)

// Tools available without restart
```

### Implicit Resolution

Tools resolved by name at runtime:

```yaml
agents:
  assistant:
    tools:
      - read_file      # Built-in function tool
      - mcp_search     # From MCP server
      - custom_tool    # Custom tool
```

Runtime searches all toolsets for matching name.

## Tool Calling Protocol

### LLM Tool Call

```json
{
  "tool_calls": [
    {
      "id": "call_abc123",
      "type": "function",
      "function": {
        "name": "search",
        "arguments": "{\"query\":\"weather\"}"
      }
    }
  ]
}
```

### Tool Result

```json
{
  "tool_call_id": "call_abc123",
  "role": "tool",
  "content": "{\"results\":[{\"title\":\"Weather Forecast\",...}]}"
}
```

### Multi-Tool Calls

LLM can call multiple tools in parallel:

```json
{
  "tool_calls": [
    {"id": "call_1", "function": {"name": "search", "arguments": "..."}},
    {"id": "call_2", "function": {"name": "calculate", "arguments": "..."}}
  ]
}
```

All tools execute, results returned together.

## Tool Schemas

### Parameter Schema

```go
schema := map[string]any{
    "type": "object",
    "properties": map[string]any{
        "query": map[string]any{
            "type":        "string",
            "description": "Search query",
        },
        "max_results": map[string]any{
            "type":        "integer",
            "description": "Maximum number of results",
            "default":     10,
        },
    },
    "required": []string{"query"},
}
```

**Schema validation:**

- Type checking
- Required fields
- Default values
- Enum constraints

### Result Schema

Tools can define output schemas:

```go
resultSchema := map[string]any{
    "type": "object",
    "properties": map[string]any{
        "status": map[string]any{
            "type": "string",
            "enum": []string{"success", "error"},
        },
        "data": map[string]any{
            "type": "object",
        },
    },
}
```

Enables structured result parsing.

## Custom Tools

### Implementing Tool Interface

```go
type WeatherTool struct{}

func (t *WeatherTool) Name() string {
    return "get_weather"
}

func (t *WeatherTool) Description() string {
    return "Fetches current weather for a location"
}

func (t *WeatherTool) Schema() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "location": map[string]any{
                "type":        "string",
                "description": "City name or coordinates",
            },
        },
        "required": []string{"location"},
    }
}

func (t *WeatherTool) Execute(ctx tool.Context, args map[string]any) (map[string]any, error) {
    location := args["location"].(string)

    // Fetch weather data
    weather, err := fetchWeather(location)
    if err != nil {
        return nil, err
    }

    return map[string]any{
        "temperature": weather.Temp,
        "conditions":  weather.Conditions,
        "humidity":    weather.Humidity,
    }, nil
}
```

### Registering Custom Tools

```go
runtime.New(cfg,
    runtime.WithDirectTools("assistant", []tool.Tool{
        &WeatherTool{},
        &StockPriceTool{},
    }),
)
```

## Tool Callbacks

### Before Tool

```go
beforeTool := func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
    // Log tool call
    log.Info("Tool called", "name", t.Name(), "args", args)

    // Validate arguments
    if err := validate(args); err != nil {
        return nil, err
    }

    // Return nil to continue
    return nil, nil
}
```

**Use cases:**

- Logging
- Validation
- Rate limiting
- Mock execution

### After Tool

```go
afterTool := func(ctx tool.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
    if err != nil {
        // Handle error
        log.Error("Tool failed", "name", t.Name(), "error", err)
        return retryOrFallback(ctx, args)
    }

    // Transform result
    return enhanceResult(result), nil
}
```

**Use cases:**

- Result transformation
- Error recovery
- Metrics collection
- Caching

## Tool Metrics

### Observability

Tools instrumented automatically:

```
hector_tool_calls_total{agent="assistant",tool="search",status="success"} 42
hector_tool_call_duration_seconds{agent="assistant",tool="search"} 1.234
```

### Tracing

Tool executions traced:

```
Agent Span
  └─ Tool Span (search)
       ├─ HTTP Request Span
       └─ Result Processing Span
```

## Tool Composition

### Chaining

Tools can call other tools:

```go
func (t *CompositeTool) Execute(ctx tool.Context, args map[string]any) (map[string]any, error) {
    // Call first tool
    result1, err := t.searchTool.Execute(ctx, searchArgs)

    // Use result in second tool
    result2, err := t.filterTool.Execute(ctx, map[string]any{
        "data": result1["results"],
    })

    return result2, nil
}
```

### Pipelines

Build tool pipelines:

```go
pipeline := &ToolPipeline{
    steps: []Tool{
        fetchTool,
        transformTool,
        analyzeTool,
    },
}
```

Each step processes previous step's output.

## Best Practices

### Tool Naming

Use clear, descriptive names:

```go
// ✅ Good
"search_web"
"calculate_statistics"
"fetch_user_profile"

// ❌ Bad
"do_search"
"calc"
"get_data"
```

### Error Messages

Provide actionable errors:

```go
// ✅ Good
return nil, fmt.Errorf("API rate limit exceeded. Retry after %s", retryAfter)

// ❌ Bad
return nil, errors.New("error")
```

### Schema Documentation

Document parameters clearly:

```go
"query": map[string]any{
    "type": "string",
    "description": "Search query (e.g., 'weather in Paris')",
    "minLength": 1,
    "maxLength": 200,
}
```

### Idempotency

Make tools idempotent when possible:

```go
func (t *CreateFileTool) Execute(ctx tool.Context, args map[string]any) (map[string]any, error) {
    path := args["path"].(string)

    // Check if exists
    if fileExists(path) {
        return map[string]any{"status": "already_exists"}, nil
    }

    // Create file
    return createFile(path, args["content"])
}
```

### Timeout Handling

Set reasonable timeouts:

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

result, err := externalAPI.Call(ctx, params)
```

### Tool Cancellation

Tools support cascade cancellation when a task is cancelled:

```go
func (t *LongRunningTool) Execute(ctx tool.Context, args map[string]any) (map[string]any, error) {
    // Register for cascade cancellation
    if taskObj := ctx.Task(); taskObj != nil {
        taskObj.RegisterExecution(&agent.ChildExecution{
            CallID: ctx.FunctionCallID(),
            Name:   t.Name(),
            Type:   "tool",
            Cancel: func() bool {
                // Cleanup logic
                return true
            },
        })
        defer taskObj.UnregisterExecution(ctx.FunctionCallID())
    }

    // Tool execution...
}
```

**API Endpoints:**

| Endpoint | Description |
|----------|-------------|
| A2A `tasks/cancel` | Cancels task + all child executions |
| `/api/tasks/{taskId}/toolCalls/{callId}/cancel` | Cancels specific tool |

When a task is cancelled, all registered child executions (tools and sub-agents) are automatically cancelled via cascade.

## Next Steps

- [RAG](rag.md) - Document stores and retrieval
- [Configuration](configuration.md) - Tool configuration details
- [Agents](agents.md) - Tool integration with agents
