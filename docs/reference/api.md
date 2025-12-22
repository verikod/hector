# HTTP API Reference

Complete reference for Hector's HTTP API. The API consists of:

1. **Hector Extensions** - Health, discovery, studio, and management endpoints
2. **A2A Protocol** - Agent-to-Agent protocol endpoints (industry standard)

## Endpoint Summary

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check and auth discovery |
| `/metrics` | GET | Prometheus metrics (when enabled) |
| `/agents` | GET | Multi-agent discovery (Hector extension) |
| `/agents/{name}` | GET | Agent card |
| `/agents/{name}` | POST | JSON-RPC agent invocation (A2A) |
| `/.well-known/agent-card.json` | GET | Default agent card (A2A spec) |
| `/api/schema` | GET | JSON Schema for configuration |
| `/api/config` | GET/POST | Config read/write (studio mode) |
| `/api/tasks/{taskId}/toolCalls/{callId}/cancel` | POST | Cancel tool execution |

---

## Hector Extensions

### Health Check

Returns server status and auth discovery information.

```
GET /health
```

**Response:**

```json
{
  "status": "ok",
  "studio_mode": false,
  "auth": {
    "enabled": true,
    "type": "jwt",
    "issuer": "https://auth.example.com",
    "audience": "hector-api",
    "client_id": "hector-client"
  },
  "studio": {
    "enabled": true,
    "allowed_roles": ["admin", "operator"]
  }
}
```

The `auth` and `studio` fields are only present when those features are enabled.

### Multi-Agent Discovery

Returns all visible agents. Hector extension to A2A for multi-agent servers.

```
GET /agents
```

**Response:**

```json
{
  "agents": [
    {
      "name": "AI Assistant",
      "description": "A helpful assistant",
      "url": "http://localhost:8080/agents/assistant",
      "version": "2.0.0",
      "protocol_version": "1.0",
      "default_input_modes": ["text/plain"],
      "default_output_modes": ["text/plain"],
      "capabilities": {
        "streaming": true,
        "push_notifications": false,
        "state_transition_history": false
      },
      "preferred_transport": "jsonrpc",
      "skills": [...]
    }
  ],
  "total": 2
}
```

**Visibility Filtering:**

| Visibility | Behavior |
|------------|----------|
| `public` | Always visible |
| `internal` | Visible only when authenticated |
| `private` | Never visible (for internal use) |

### JSON Schema

Returns dynamically-generated JSON Schema for configuration validation.

```
GET /api/schema
```

**Response:**

```json
{
  "$id": "https://hector.dev/schemas/config.json",
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Hector Configuration Schema",
  "description": "Complete configuration schema for Hector v2",
  "type": "object",
  "properties": {
    "version": {...},
    "agents": {...},
    "llms": {...},
    "tools": {...}
  }
}
```

### Configuration (Studio Mode)

Read and write configuration. Requires studio mode enabled.

**Read Configuration:**

```
GET /api/config
```

**Response (YAML):**

```yaml
version: "2"
agents:
  assistant:
    llm: default
    instruction: You are a helpful assistant.
```

**Write Configuration:**

```
POST /api/config
Content-Type: application/yaml

version: "2"
agents:
  assistant:
    llm: default
    instruction: Updated instruction.
```

**Response (JSON):**

```json
{
  "status": "saved",
  "message": "Configuration saved. Reloading..."
}
```

**Access Control:**

1. Studio mode must be enabled (`--studio` flag or `server.studio.enabled: true`)
2. If auth is enabled, user must have an allowed role (default: `operator`)

> [!WARNING]
> **Security:** The `server` block (auth, studio, ports, TLS, CORS) cannot be modified via the API. These settings can only be changed by editing the config file directly.

### Prometheus Metrics

Returns Prometheus-format metrics when observability is enabled.

```
GET /metrics
```

**Response:**

```
# HELP hector_requests_total Total number of requests
# TYPE hector_requests_total counter
hector_requests_total{agent="assistant"} 42
```

Enable via:

```yaml
observability:
  metrics:
    enabled: true
    endpoint: /metrics  # default
```

### Cancel Tool Execution

Cancel a specific tool execution within a running task.

```
POST /api/tasks/{taskId}/toolCalls/{callId}/cancel
```

**Response:**

```json
{
  "cancelled": true,
  "task_id": "uuid-123",
  "call_id": "call-456"
}
```

---

## A2A Protocol Endpoints

Hector implements [A2A Protocol](https://a2a-protocol.org/latest/specification/) v1.0 (DRAFT).

### A2A Compliance

| Operation | JSON-RPC Method | Status |
|-----------|-----------------|--------|
| Send Message | `message/send` | ✅ Supported |
| Send Streaming Message | `message/stream` | ✅ Supported |
| Get Task | `tasks/get` | ✅ Supported (via TaskStore) |
| List Tasks | `tasks/list` | ✅ Supported (via TaskStore) |
| Cancel Task | `tasks/cancel` | ✅ Supported |
| Subscribe to Task | `tasks/subscribe` | ✅ Supported (SSE streaming) |
| Push Notifications | `tasks/pushNotificationConfig/*` | ❌ Not supported |

**Capabilities advertised in Agent Card:**

```json
{
  "capabilities": {
    "streaming": true,
    "push_notifications": false,
    "state_transition_history": false
  }
}
```

### Default Agent Card

Per A2A spec, servers expose a well-known endpoint for the default agent.

```
GET /.well-known/agent-card.json
```

**Response:**

```json
{
  "name": "AI Assistant",
  "description": "A helpful assistant",
  "url": "http://localhost:8080/agents/assistant",
  "version": "2.0.0",
  "protocol_version": "1.0",
  "default_input_modes": ["text/plain"],
  "default_output_modes": ["text/plain"],
  "capabilities": {
    "streaming": true,
    "push_notifications": false,
    "state_transition_history": false
  },
  "preferred_transport": "jsonrpc",
  "provider": {
    "org": "Hector",
    "url": "https://github.com/verikod/hector"
  },
  "skills": [
    {
      "id": "general",
      "name": "General Assistant",
      "description": "Answers questions and helps with tasks",
      "tags": ["general", "assistant"]
    }
  ],
  "security_schemes": {
    "BearerAuth": {
      "type": "http",
      "scheme": "bearer",
      "bearer_format": "JWT"
    }
  },
  "security": [{"BearerAuth": []}]
}
```

### Per-Agent Card

Each agent has its own card endpoint:

```
GET /agents/{name}
GET /agents/{name}/.well-known/agent-card.json
```

Both return the same agent card JSON.

### JSON-RPC Agent Invocation

Send messages to agents using JSON-RPC 2.0.

```
POST /agents/{name}
Content-Type: application/json
```

#### message:send (Blocking)

Wait for complete response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "message:send",
  "params": {
    "message": {
      "role": "user",
      "parts": [{"type": "text", "text": "Hello"}]
    }
  }
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "task_id": "uuid-123",
    "context_id": "ctx-456",
    "status": {
      "state": "completed",
      "message": {
        "role": "agent",
        "parts": [{"type": "text", "text": "Hello! How can I help?"}]
      }
    }
  }
}
```

#### message:stream (Streaming)

Stream response chunks via SSE:

```bash
curl -X POST http://localhost:8080/agents/assistant \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "message:stream",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"type": "text", "text": "Hello"}]
      }
    }
  }'
```

**SSE Stream:**

```
data: {"type":"task.status_update","task_id":"uuid-123","status":{"state":"submitted"}}

data: {"type":"task.status_update","task_id":"uuid-123","status":{"state":"working"}}

data: {"type":"task.status_update","task_id":"uuid-123","status":{"state":"working","message":{"role":"agent","parts":[{"type":"text","text":"Hello!"}]}}}

data: {"type":"task.status_update","task_id":"uuid-123","status":{"state":"completed","message":{"role":"agent","parts":[{"type":"text","text":"Hello! How can I help?"}]}}}
```

### Message Parts

A2A supports multi-modal messages via parts:

**Text:**
```json
{"type": "text", "text": "Hello world"}
```

**File (URL):**
```json
{
  "type": "file",
  "name": "document.pdf",
  "mime_type": "application/pdf",
  "url": "https://example.com/document.pdf"
}
```

**File (Inline):**
```json
{
  "type": "file",
  "name": "image.png",
  "mime_type": "image/png",
  "data": "base64-encoded-data"
}
```

**Structured Data:**
```json
{
  "type": "data",
  "mime_type": "application/json",
  "data": {"key": "value"}
}
```

### Task States

| State | Description |
|-------|-------------|
| `submitted` | Task created, not started |
| `working` | Agent processing |
| `completed` | Successfully finished |
| `failed` | Error occurred |
| `cancelled` | User/system cancelled |
| `input_required` | Waiting for human input (HITL) |

### Error Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32600,
    "message": "Invalid request",
    "data": {"detail": "Missing required field: message"}
  }
}
```

**Standard JSON-RPC Error Codes:**

| Code | Message |
|------|---------|
| `-32700` | Parse error |
| `-32600` | Invalid request |
| `-32601` | Method not found |
| `-32602` | Invalid params |
| `-32603` | Internal error |

---

## Authentication

### JWT Bearer Authentication

When auth is enabled, include the token in requests:

```http
Authorization: Bearer <jwt-token>
```

**Excluded Paths (no auth required):**

- `/health`
- `/.well-known/agent-card.json`
- `/agents` (for discovery)
- `/agents/` (visibility handles per-agent auth)

Agent visibility (`public`, `internal`, `private`) handles per-agent access control.

### Auth Discovery

Clients can discover auth requirements from the health endpoint:

```bash
curl http://localhost:8080/health
```

```json
{
  "auth": {
    "enabled": true,
    "type": "jwt",
    "issuer": "https://auth.example.com",
    "audience": "hector-api",
    "client_id": "hector-client"
  }
}
```

---

## Transport Options

### JSON-RPC (Default)

JSON-RPC 2.0 over HTTP/HTTPS.

```yaml
server:
  transport: jsonrpc  # Default
```

### gRPC (Optional)

Protocol Buffers over HTTP/2.

```yaml
server:
  transport: grpc
  grpc_port: 9090
```

---

## Examples

### Basic Chat Request

```bash
curl -X POST http://localhost:8080/agents/assistant \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "message:send",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"type": "text", "text": "What is 2+2?"}]
      }
    }
  }'
```

### Authenticated Request

```bash
curl -X POST http://localhost:8080/agents/assistant \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "message:stream",
    "params": {
      "message": {
        "role": "user",
        "parts": [{"type": "text", "text": "Hello"}]
      }
    }
  }'
```

### Multi-Modal Request

```bash
curl -X POST http://localhost:8080/agents/assistant \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "message:send",
    "params": {
      "message": {
        "role": "user",
        "parts": [
          {"type": "text", "text": "Describe this image"},
          {"type": "file", "name": "photo.jpg", "mime_type": "image/jpeg", "url": "https://example.com/photo.jpg"}
        ]
      }
    }
  }'
```

---

## CORS

Default permissive CORS for development:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
```

Configure in YAML:

```yaml
server:
  cors:
    allowed_origins:
      - "https://app.example.com"
    allow_credentials: true
```
