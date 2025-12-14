# A2A Protocol

Hector implements the Agent-to-Agent (A2A) protocol v0.3.0 for standardized agent communication and federation.

## Protocol Overview

**A2A (Agent-to-Agent)** is an open protocol enabling:

- Standardized agent discovery
- Cross-platform agent communication
- Agent federation across organizations
- Interoperability between agent frameworks

**Hector's A2A implementation:**

- Full v0.3.0 compliance
- Both server (expose agents) and client (call remote agents)
- JSON-RPC over HTTP (default) or gRPC transport
- Agent cards for discovery
- Task-based execution model
- Streaming responses

## Agent Cards

Agent cards are JSON documents describing agent capabilities.

### Server-Level Agent Card

Per A2A spec, servers expose a well-known endpoint:

```
GET /.well-known/agent-card.json
```

Returns the default (first) agent's card:

```json
{
  "name": "Customer Support Assistant",
  "description": "AI agent for customer support",
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
    "url": "https://github.com/kadirpekel/hector"
  },
  "skills": [
    {
      "id": "support",
      "name": "Customer Support",
      "description": "Answer customer questions",
      "tags": ["support", "customer-service"]
    }
  ]
}
```

### Per-Agent Cards

Each agent has its own card endpoint:

```
GET /agents/{agent_name}
GET /agents/{agent_name}/.well-known/agent-card.json
```

Both return the same agent card JSON.

### Card Structure

```go
type AgentCard struct {
    Name               string
    Description        string
    URL                string
    Version            string
    ProtocolVersion    string
    DefaultInputModes  []string
    DefaultOutputModes []string
    Skills             []AgentSkill
    Capabilities       AgentCapabilities
    PreferredTransport string
    Provider           *AgentProvider
    SecuritySchemes    NamedSecuritySchemes
    Security           []SecurityRequirements
}
```

**Skills** describe what the agent can do:

```go
type AgentSkill struct {
    ID          string
    Name        string
    Description string
    Tags        []string
    Examples    []string
}
```

**Capabilities** indicate features:

```go
type AgentCapabilities struct {
    Streaming              bool
    PushNotifications      bool
    StateTransitionHistory bool
}
```

## Discovery

Hector extends A2A with multi-agent discovery:

```
GET /agents
```

Returns all visible agents:

```json
{
  "agents": [
    {
      "name": "Assistant",
      "url": "http://localhost:8080/agents/assistant",
      ...
    },
    {
      "name": "Specialist",
      "url": "http://localhost:8080/agents/specialist",
      ...
    }
  ],
  "total": 2
}
```

Respects agent visibility:

- `public`: Always visible
- `internal`: Visible only when authenticated
- `private`: Hidden from discovery

## Message Protocol

### Message Structure

A2A messages have a role and content parts:

```go
type Message struct {
    Role  MessageRole
    Parts []Part
}
```

**Roles:**

- `user`: User input
- `agent`: Agent response
- `system`: System messages

**Parts** (polymorphic content):

```go
// Text content
type TextPart struct {
    Text string
}

// File attachment
type FilePart struct {
    Name     string
    MimeType string
    Data     []byte  // Or URL
}

// Structured data
type DataPart struct {
    Data     any
    MimeType string
}
```

### Example Message

```json
{
  "role": "user",
  "parts": [
    {
      "type": "text",
      "text": "Analyze this image"
    },
    {
      "type": "file",
      "name": "chart.png",
      "mime_type": "image/png",
      "url": "https://example.com/chart.png"
    }
  ]
}
```

## Task Execution

A2A uses a task-based model for agent invocations.

### Task States

```
submitted → working → completed
                  ↘ failed
                  ↘ cancelled
                  ↘ input_required
```

**States:**

- `submitted`: Task created, not started
- `working`: Agent processing
- `completed`: Successfully finished
- `failed`: Error occurred
- `cancelled`: User/system cancelled
- `input_required`: Waiting for human input (HITL)

### Task Lifecycle

**1. Submit Task:**

```http
POST /agents/assistant
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "message:send",
  "params": {
    "message": {
      "role": "user",
      "parts": [
        {"type": "text", "text": "Hello"}
      ]
    }
  }
}
```

**2. Status Updates:**

Server sends task status updates via streaming:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "type": "task.status_update",
    "task_id": "uuid-123",
    "status": {
      "state": "working",
      "timestamp": "2025-01-15T10:00:00Z"
    }
  }
}
```

**3. Progress Events:**

Streaming message chunks:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "type": "task.status_update",
    "task_id": "uuid-123",
    "status": {
      "state": "working",
      "message": {
        "role": "agent",
        "parts": [
          {"type": "text", "text": "Hello! How"}
        ]
      }
    }
  }
}
```

**4. Completion:**

Final status with complete response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "type": "task.status_update",
    "task_id": "uuid-123",
    "status": {
      "state": "completed",
      "message": {
        "role": "agent",
        "parts": [
          {"type": "text", "text": "Hello! How can I help you?"}
        ]
      }
    }
  }
}
```

## Transport Protocols

### JSON-RPC (Default)

JSON-RPC 2.0 over HTTP/HTTPS:

```yaml
server:
  transport: jsonrpc  # Default
```

**Endpoints:**

- `POST /agents/{name}` - JSON-RPC requests

**Methods:**

- `message:send` - Send message, get complete response
- `message:stream` - Send message, stream response

### gRPC (Optional)

Protocol Buffers over HTTP/2:

```yaml
server:
  transport: grpc
  grpc_port: 9090
```

gRPC service definitions from a2a-go library.

## Request/Response Patterns

### Non-Streaming Request

Complete response in single reply:

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
        "parts": [{"type": "text", "text": "Hello"}]
      }
    }
  }'
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "task_id": "uuid-123",
    "status": {
      "state": "completed",
      "message": {
        "role": "agent",
        "parts": [
          {"type": "text", "text": "Hello! How can I help?"}
        ]
      }
    }
  }
}
```

### Streaming Request

Server-Sent Events (SSE) stream:

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

Stream response:

```
data: {"type":"task.status_update","status":{"state":"submitted"}}

data: {"type":"task.status_update","status":{"state":"working"}}

data: {"type":"task.status_update","status":{"state":"working","message":{"role":"agent","parts":[{"type":"text","text":"Hello"}]}}}

data: {"type":"task.status_update","status":{"state":"working","message":{"role":"agent","parts":[{"type":"text","text":"Hello! How"}]}}}

data: {"type":"task.status_update","status":{"state":"completed","message":{"role":"agent","parts":[{"type":"text","text":"Hello! How can I help?"}]}}}
```

## Authentication

A2A supports security schemes in agent cards.

### JWT Authentication

Agent card declares security:

```json
{
  "security_schemes": {
    "BearerAuth": {
      "type": "http",
      "scheme": "bearer",
      "bearer_format": "JWT",
      "description": "JWT Bearer token authentication"
    }
  },
  "security": [
    {"BearerAuth": []}
  ]
}
```

Clients send token:

```http
POST /agents/assistant
Authorization: Bearer <jwt-token>
```

Configuration:

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://auth.example.com/.well-known/jwks.json
```

## Remote Agents

Hector can call remote A2A agents.

### Configuration

```yaml
agents:
  remote_helper:
    type: remote
    url: http://remote-server:8080
    description: Remote helper agent
```

Or explicit agent card:

```yaml
agents:
  remote_helper:
    type: remote
    agent_card_source: http://remote-server:8080/.well-known/agent-card.json
```

### Implementation

```go
// pkg/agent/remoteagent/a2a.go

// Create client from agent card
client, err := a2aclient.NewFromCard(ctx, card)

// Send message
req := &a2a.MessageSendParams{
    Message: msg,
}

// Stream response
for event, err := range client.SendStreamingMessage(ctx, req) {
    // Process event
}
```

### Usage Patterns

**As Sub-Agent:**

```yaml
agents:
  coordinator:
    llm: default
    sub_agents:
      - remote_specialist
```

**As Agent Tool:**

```yaml
agents:
  main:
    llm: default
    tools:
      - agent_call_remote_specialist
```

**In Workflows:**

```yaml
agents:
  workflow:
    type: workflow
    steps:
      - agent: local_agent
      - agent: remote_agent
```

## Federation

A2A enables agent federation across organizations.

### Scenario: Cross-Organization Agents

**Organization A:**

```yaml
# config.yaml
agents:
  legal_expert:
    llm: default
    visibility: public
    description: Legal expertise agent

server:
  auth:
    enabled: true
    jwks_url: https://auth.org-a.com/.well-known/jwks.json
```

**Organization B:**

```yaml
# config.yaml
agents:
  coordinator:
    llm: default
    tools: [research, analyze]
    sub_agents:
      - org_a_legal  # Remote agent from Org A

  org_a_legal:
    type: remote
    url: https://agents.org-a.com/agents/legal_expert
    headers:
      Authorization: Bearer ${ORG_A_API_TOKEN}
```

Coordinator agent can now delegate to Org A's legal expert.

## Multi-Modal Support

A2A supports multi-modal content via Parts.

### Text + Image

```json
{
  "role": "user",
  "parts": [
    {"type": "text", "text": "What's in this image?"},
    {
      "type": "file",
      "name": "photo.jpg",
      "mime_type": "image/jpeg",
      "url": "https://example.com/photo.jpg"
    }
  ]
}
```

### Structured Data

```json
{
  "role": "user",
  "parts": [
    {"type": "text", "text": "Analyze this data"},
    {
      "type": "data",
      "mime_type": "application/json",
      "data": {
        "sales": [100, 200, 300],
        "region": "US"
      }
    }
  ]
}
```

## Error Handling

### Task Failures

Failed tasks return error in status:

```json
{
  "task_id": "uuid-123",
  "status": {
    "state": "failed",
    "timestamp": "2025-01-15T10:00:00Z",
    "error": {
      "code": "llm_error",
      "message": "API rate limit exceeded"
    }
  }
}
```

### JSON-RPC Errors

Standard JSON-RPC error responses:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32600,
    "message": "Invalid request",
    "data": {
      "detail": "Missing required field: message"
    }
  }
}
```

## Human-in-the-Loop (HITL)

Tasks can pause for human input.

### Input Required State

```json
{
  "task_id": "uuid-123",
  "status": {
    "state": "input_required",
    "message": {
      "role": "agent",
      "parts": [
        {"type": "text", "text": "Approve command execution: rm -rf /data?"}
      ]
    }
  },
  "input_requirement": {
    "type": "tool_approval",
    "options": [
      {"id": "approve", "label": "Approve"},
      {"id": "deny", "label": "Deny"}
    ]
  }
}
```

### Provide Input

```json
{
  "jsonrpc": "2.0",
  "method": "task:input",
  "params": {
    "task_id": "uuid-123",
    "option_id": "approve"
  }
}
```

Task resumes with selected option.

## Implementation Details

### Server Components

**HTTP Server** (pkg/server/http.go):

- Routing to agents
- Agent card serving
- Authentication middleware
- CORS handling

**Executor** (pkg/server/executor.go):

- Implements a2a.Executor interface
- Bridges A2A to Hector agents
- Task state management
- Event streaming

**Task Store** (pkg/task/store.go):

- Persistent task storage
- SQL or in-memory backends

### Client Components

**Remote Agent** (pkg/agent/remoteagent/a2a.go):

- Fetches agent cards
- Creates A2A clients
- Streams remote responses
- Error handling

### A2A Library

Hector uses `github.com/a2aproject/a2a-go`:

- Core A2A types (Message, AgentCard, Task)
- JSON-RPC handlers
- gRPC handlers
- Client implementation
- Agent card resolution

## Compliance

Hector's A2A implementation:

**Compliant:**

- ✅ Agent card discovery
- ✅ JSON-RPC transport
- ✅ Task-based execution
- ✅ Streaming responses
- ✅ Security schemes (JWT)
- ✅ Multi-modal messages

**Extensions:**

- Multi-agent discovery endpoint (`/agents`)
- Agent visibility levels
- Custom task states (input_required)
- Hot reload of agent configurations

## Next Steps

- [Architecture Reference](architecture.md) - System architecture
- [Programmatic API Reference](programmatic.md) - Agent types and implementation
- [Security Guide](../guides/security.md) - Authentication setup

