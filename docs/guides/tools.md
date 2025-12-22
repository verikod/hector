# Tools

Tools extend agent capabilities by enabling actions like file operations, web requests, and external integrations.

## Tool Types

Hector supports three tool types:

1. **Function Tools**: Built-in Go functions
2. **MCP Tools**: Model Context Protocol servers
3. **Command Tools**: Shell command execution

## Built-in Tools

Enable all built-in tools:

```bash
hector serve --model gpt-4o --tools
```

Or specific tools:

```bash
hector serve --model gpt-4o --tools read_file,write_file,grep_search
```

### Available Built-in Tools

**File Operations**

- `read_file` - Read file contents with line numbers and ranges
- `write_file` - Create or overwrite files (requires approval)
- `search_replace` - Replace exact text in files (requires approval)
- `apply_patch` - Apply patches with context validation (requires approval)
- `grep_search` - Search files using regex patterns

**Command Execution**

- `execute_command` - Execute shell commands with sandboxing (requires approval)

**Web & Network**

- `web_request` - Make HTTP requests to external APIs (requires approval)

**Task Management**

- `todo_write` - Create and manage task lists

### Built-in Handler Names

When configuring function tools in YAML, use these handler names:

| Handler Name | Description | Default Approval |
|--------------|-------------|------------------|
| `read_file` | Read file contents | No |
| `write_file` | Write/create files | Yes |
| `search_replace` | Find and replace in files | Yes |
| `apply_patch` | Apply unified diff patches | Yes |
| `grep_search` | Regex search in files | No |
| `execute_command` | Run shell commands | Yes |
| `web_request` | HTTP requests | Yes |
| `todo_write` | Task list management | No |
| `search` | Document search (RAG) | No |

Example usage in config:

```yaml
tools:
  my_reader:
    type: function
    handler: read_file  # Must match a handler name above
    enabled: true
```

### Tool Approval

Tools have smart approval defaults:

**Require Approval** (HITL - Human in the Loop):

- `write_file` - File modification
- `search_replace` - File editing
- `apply_patch` - Code changes
- `execute_command` - Command execution
- `web_request` - External requests

**No Approval**:

- `read_file` - Read-only
- `grep_search` - Read-only
- `todo_write` - Safe operation
- `search` - Document search

Override defaults:

```bash
# Enable approval for specific tools
hector serve --model gpt-4o --tools --approve-tools read_file,grep_search

# Disable approval for specific tools
hector serve --model gpt-4o --tools --no-approve-tools write_file,execute_command
```

## Configuration File

### Function Tools

Configure built-in tools:

```yaml
tools:
  # Read file tool (read-only, no approval)
  read_file:
    type: function
    handler: read_file
    enabled: true

  # Write file tool (requires approval)
  write_file:
    type: function
    handler: write_file
    enabled: true
    require_approval: true
    approval_prompt: "Allow writing to {file}?"

  # Command execution (sandboxed)
  execute_command:
    type: command
    enabled: true
    working_directory: ./
    max_execution_time: 30s
    allowed_commands:
      - ls
      - cat
      - grep
      - git
    require_approval: true

agents:
  assistant:
    llm: default
    tools: [read_file, write_file, execute_command]
```

### MCP Tools

Connect to MCP servers for external tools.

#### HTTP/SSE Transport

```yaml
tools:
  weather:
    type: mcp
    url: http://localhost:8000/mcp
    transport: sse  # Auto-detected from URL

agents:
  assistant:
    tools: [weather]
```

#### Streamable HTTP Transport

```yaml
tools:
  docling:
    type: mcp
    url: http://localhost:8080
    transport: streamable-http

agents:
  assistant:
    tools: [docling]
```

#### STDIO Transport

Launch MCP server as subprocess:

```yaml
tools:
  filesystem:
    type: mcp
    transport: stdio
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-filesystem"
      - /path/to/allowed/directory
    env:
      NODE_ENV: production

agents:
  assistant:
    tools: [filesystem]
```

#### Choosing Transport Type

| Transport | Use When | Pros | Cons |
|-----------|----------|------|------|
| `sse` | Remote HTTP server, real-time | Simple setup, firewall-friendly | Requires running server |
| `streamable-http` | Modern HTTP/2 servers | Efficient streaming, bidirectional | Newer, less common |
| `stdio` | Local tools, no network | No network needed, secure | Process per connection |

**Decision guide:**

- **Remote server?** → Use `sse` or `streamable-http`
- **Local development?** → Use `stdio` (no server to manage)
- **Docker container?** → Use `sse` with port mapping
- **Need max security?** → Use `stdio` (no network exposure)

### Tool Filtering

Limit which tools are exposed from an MCP server:

```yaml
tools:
  mcp:
    type: mcp
    url: http://localhost:8000/mcp
    filter:
      - read_file      # Only expose read_file
      - list_directory # and list_directory
```

Without `filter`, all tools from the server are available.

## Tool Approval (HITL)

Human-in-the-Loop approval for sensitive operations.

### Enable Approval

```yaml
tools:
  write_file:
    type: function
    handler: write_file
    require_approval: true
    approval_prompt: "Allow writing to {file}?"

  execute_command:
    type: command
    require_approval: true
    approval_prompt: "Execute: {command}?"
```

### Approval Flow

When an agent calls a tool requiring approval:

1. Tool execution pauses
2. User receives approval request via webhook/UI
3. User approves or denies
4. Tool executes or returns error
5. Result returned to agent

### Approval Webhook

Configure approval webhook:

```yaml
server:
  approval:
    webhook_url: https://your-app.com/approve
    timeout: 300s  # Wait up to 5 minutes
```

Webhook receives:

```json
{
  "tool_name": "write_file",
  "parameters": {
    "path": "/app/config.yaml",
    "content": "..."
  },
  "agent": "assistant",
  "session_id": "sess_123"
}
```

Respond with:

```json
{
  "approved": true
}
```

## Command Tool Configuration

### Sandboxing

Restrict command execution:

```yaml
tools:
  execute_command:
    type: command
    working_directory: ./workspace
    max_execution_time: 30s
    allowed_commands:
      - git
      - npm
      - node
      - python
    denied_commands:
      - rm
      - dd
      - mkfs
    deny_by_default: false  # Allow unless denied
```

### Deny by Default

Strict whitelist mode:

```yaml
tools:
  execute_command:
    type: command
    deny_by_default: true  # Deny unless explicitly allowed
    allowed_commands:
      - ls
      - cat
      - grep
```

Only whitelisted commands can execute.

### Execution Timeout

Prevent long-running commands:

```yaml
tools:
  execute_command:
    type: command
    max_execution_time: 30s  # Kill after 30 seconds
```

Supports: `30s`, `1m`, `5m30s`, etc.

## MCP Integration Patterns

### Multiple MCP Servers

```yaml
tools:
  weather:
    type: mcp
    url: http://weather-service:8000/mcp

  database:
    type: mcp
    url: http://db-service:8000/mcp

  filesystem:
    type: mcp
    transport: stdio
    command: npx
    args: [-y, "@modelcontextprotocol/server-filesystem", /data]

agents:
  assistant:
    tools: [weather, database, filesystem]
```

### Authenticated MCP

Pass authentication via environment:

```yaml
tools:
  external_api:
    type: mcp
    url: https://api.example.com/mcp
    env:
      API_KEY: ${EXTERNAL_API_KEY}
```

For STDIO transport, env vars are passed to subprocess.

### Tool Name Conflicts

MCP tools are prefixed with server name:

```yaml
tools:
  server1:
    type: mcp
    url: http://server1:8000/mcp
    # Exposes: server1_read_file, server1_write_file

  server2:
    type: mcp
    url: http://server2:8000/mcp
    # Exposes: server2_read_file, server2_write_file
```

Tools become: `server1_read_file`, `server2_read_file`, etc.

## Agent Tool Selection

### Assign Tools to Agents

```yaml
tools:
  read_file:
    type: function
    handler: read_file

  write_file:
    type: function
    handler: write_file

  search:
    type: function
    handler: search

agents:
  # Reader agent: read-only tools
  reader:
    llm: default
    tools: [read_file, grep_search]

  # Writer agent: read and write tools
  writer:
    llm: default
    tools: [read_file, write_file, search_replace]

  # Analyst agent: read and search tools
  analyst:
    llm: default
    tools: [read_file, search, grep_search]
```

### All Available Tools

Omit `tools` to allow all configured tools:

```yaml
agents:
  admin:
    llm: default
    # tools not specified = all tools available
```

## Custom Tool Parameters

Define custom parameters schema:

```yaml
tools:
  custom_analyzer:
    type: function
    handler: custom_analyzer
    parameters:
      type: object
      properties:
        text:
          type: string
          description: Text to analyze
        mode:
          type: string
          enum: [simple, detailed, comprehensive]
        threshold:
          type: number
          minimum: 0
          maximum: 1
      required: [text, mode]
```

LLM receives schema and generates valid parameters.

## Tool Discovery

Tools are automatically discovered via:

1. **Built-in tools**: Registered in `GetDefaultToolConfigs()`
2. **MCP tools**: Fetched from MCP server on startup
3. **Custom tools**: Defined in configuration

View available tools:

```bash
hector info --config config.yaml --agent assistant
```

## Examples

### File Operations Agent

```yaml
tools:
  read_file:
    type: function
    handler: read_file

  write_file:
    type: function
    handler: write_file
    require_approval: true

  grep_search:
    type: function
    handler: grep_search

agents:
  file_manager:
    llm: default
    tools: [read_file, write_file, grep_search]
    instruction: |
      You help users manage files.
      Use read_file to view contents.
      Use grep_search to find patterns.
      Use write_file to create or update files.
      Always ask for confirmation before writing.
```

### MCP Document Parser

```yaml
tools:
  docling:
    type: mcp
    url: http://localhost:8080
    transport: streamable-http
    filter:
      - convert_document_into_docling_document

agents:
  document_processor:
    llm: default
    tools: [docling]
    instruction: |
      You process documents using Docling.
      Convert PDFs and other formats to structured text.
```

### Command Execution Agent

```yaml
tools:
  execute_command:
    type: command
    working_directory: ./workspace
    max_execution_time: 60s
    allowed_commands:
      - git
      - npm
      - node
      - python
      - pytest
    require_approval: true

agents:
  developer:
    llm: default
    tools: [execute_command, read_file, write_file]
    instruction: |
      You are a development assistant.
      Use execute_command to run tests, build, and deploy.
      Always explain what commands you're running.
```

### Multi-MCP Integration

```yaml
tools:
  weather:
    type: mcp
    url: http://weather:8000/mcp

  database:
    type: mcp
    url: http://database:8000/mcp
    filter:
      - query
      - insert
      - update

  filesystem:
    type: mcp
    transport: stdio
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-filesystem"
      - /data/workspace

agents:
  integration_agent:
    llm: default
    tools: [weather, database, filesystem]
    instruction: |
      You integrate multiple data sources:
      - Weather data via weather API
      - Database operations via database MCP
      - File operations via filesystem MCP
```

## Security Best Practices

### Minimal Permissions

Grant only necessary tools:

```yaml
# ✅ Good - Scoped permissions
agents:
  reader:
    tools: [read_file, grep_search]

# ❌ Bad - Excessive permissions
agents:
  reader:
    tools: [read_file, write_file, execute_command, web_request]
```

### Command Sandboxing

Use strict whitelists:

```yaml
# ✅ Good - Explicit whitelist
tools:
  execute_command:
    deny_by_default: true
    allowed_commands: [ls, cat, grep]

# ❌ Bad - Open access
tools:
  execute_command:
    deny_by_default: false
```

### Require Approval

Enable approval for sensitive operations:

```yaml
# ✅ Good - Approval required
tools:
  write_file:
    require_approval: true

  execute_command:
    require_approval: true

# ❌ Bad - Auto-execution
tools:
  write_file:
    require_approval: false
```

### Working Directory Limits

Restrict command execution scope:

```yaml
tools:
  execute_command:
    working_directory: ./safe-workspace  # Limit scope
    deny_by_default: true
```

## Troubleshooting

### Tool Not Found

Ensure tool is configured and assigned:

```yaml
tools:
  my_tool:
    type: function
    handler: my_tool

agents:
  assistant:
    tools: [my_tool]  # Must be listed
```

### MCP Connection Failed

Check MCP server status:

```bash
curl http://localhost:8000/mcp
```

Verify transport matches server type:

```yaml
tools:
  mcp:
    url: http://localhost:8000/mcp
    transport: sse  # Must match server transport
```

### Approval Timeout

Increase approval timeout:

```yaml
server:
  approval:
    timeout: 600s  # 10 minutes
```

### Command Denied

Add to whitelist:

```yaml
tools:
  execute_command:
    allowed_commands:
      - your_command  # Add here
```

## Next Steps

- [RAG Guide](rag.md) - Setup document stores and search
- [Security Guide](security.md) - Authentication and authorization
- [Programmatic API Reference](../reference/programmatic.md) - Custom tools and tool callbacks in Go

