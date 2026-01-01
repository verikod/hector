# Configuration Reference

Hector uses YAML configuration files for declarative agent definition. This reference documents all available configuration options.

## Overview

```yaml
version: "2"
name: my-project
description: A helpful AI assistant

# Resource definitions
databases: {}       # SQL database connections
vector_stores: {}   # Vector database providers
llms: {}            # LLM providers
embedders: {}       # Embedding providers
tools: {}           # Tool configurations
agents: {}          # Agent definitions
document_stores: {} # RAG document stores
guardrails: {}      # Safety guardrails

# Settings
defaults: {}        # Default values for agents
server: {}          # Network and security
storage: {}         # Data persistence
rate_limiting: {}   # Rate limiting
observability: {}   # Tracing and metrics
logger: {}          # Logging
```

## Environment Variables

Hector supports environment variable interpolation:

```yaml
llms:
  default:
    api_key: ${OPENAI_API_KEY}        # Required - error if missing
    base_url: ${BASE_URL:default}     # Optional with default value
```

### `.env` Files

`.env` files are automatically loaded from:
1. The current directory
2. The config file's directory  
3. The user's home directory (`~/.env`)

### Hot Reload

Hector automatically watches for changes to `.env` files in the config file's directory. When the `.env` file is modified:

1. Environment variables are reloaded (new values overwrite existing ones)
2. The configuration is re-parsed with the updated values
3. Changes take effect immediately without requiring a restart

This enables workflows like:
- Rotating API keys without service interruption
- Updating configuration values on-the-fly via Hector Studio

> **Note**: Hot reload applies to variables interpolated in YAML (`${VAR}` syntax). Variables read directly at runtime (e.g., tool-specific defaults) are resolved when the configuration is parsed.

---

## LLMs

Configure LLM providers for agents.

```yaml
llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
    temperature: 0.7
    max_tokens: 4096
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provider` | string | auto-detect | `anthropic`, `openai`, `gemini`, `ollama` |
| `model` | string | per-provider | Model identifier |
| `api_key` | string | from env | API key (not required for Ollama) |
| `base_url` | string | per-provider | Custom API endpoint |
| `temperature` | float | `0.7` | Sampling temperature (0-2) |
| `max_tokens` | int | provider default | Maximum tokens to generate |
| `max_tool_output_length` | int | `0` (unlimited) | Truncate tool outputs |


### Extended Thinking (Claude)

```yaml
llms:
  claude:
    provider: anthropic
    model: claude-sonnet-4-20250514
    thinking:
      enabled: true
      budget_tokens: 1024
```

### Default Models

| Provider | Default Model |
|----------|---------------|
| `anthropic` | `claude-haiku-4-5` |
| `openai` | `gpt-4o` |
| `gemini` | `gemini-2.5-pro` |
| `ollama` | `qwen3` |

---

## Agents

Define AI agents with instructions, tools, and behavior.

```yaml
agents:
  assistant:
    name: AI Assistant
    description: A helpful coding assistant
    llm: default
    instruction: You are a helpful assistant.
    tools:
      - text_editor
      - grep_search
    streaming: true
```

### Core Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | - | Display name (human-readable) |
| `description` | string | auto-generated | Agent description |
| `type` | string | `llm` | `llm`, `sequential`, `parallel`, `loop`, `runner`, `remote` |
| `llm` | string | `default` | Reference to LLM config |
| `instruction` | string | - | System prompt |
| `tools` | []string | - | Tool names this agent can use |
| `streaming` | bool | `true` | Enable token-by-token streaming |
| `visibility` | string | `public` | `public`, `internal`, `private` |
| `skills` | []object | auto-generated | A2A skill definitions |
| `input_modes` | []string | `["text/plain"]` | Supported input MIME types |
| `output_modes` | []string | `["text/plain"]` | Supported output MIME types |
| `max_iterations` | int | - | Max iterations for loop agents |
| `trigger` | object | - | Trigger configuration (schedule or webhook) |
| `notifications` | []object | - | Outbound notification configurations |

### Trigger Configuration

Agents can be triggered by schedules or webhooks.

**Schedule Trigger:**
```yaml
agents:
  daily-report:
    trigger:
      type: schedule
      cron: "0 9 * * *"
      input: "Generate daily report"
```

**Webhook Trigger:**
```yaml
agents:
  github-handler:
    trigger:
      type: webhook
      path: /webhooks/github
      secret: ${GITHUB_WEBHOOK_SECRET}
      signature_header: X-Hub-Signature-256
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | - | Trigger type: `schedule` or `webhook` |
| `enabled` | bool | `true` | Enable/disable the trigger |
| `cron` | string | - | Cron expression (schedule only) |
| `timezone` | string | `UTC` | Timezone for cron (schedule only) |
| `input` | string | - | Static input for triggered runs |
| `path` | string | `/webhooks/{agent-name}` | URL path (webhook only) |
| `methods` | []string | `["POST"]` | Allowed HTTP methods (webhook only) |
| `secret` | string | - | HMAC secret for signature verification |
| `signature_header` | string | `X-Webhook-Signature` | Header containing signature |
| `webhook_input` | object | - | Payload transformation config |
| `response` | object | - | Webhook response behavior |

**Webhook Input Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `template` | string | - | Go template for payload transformation |
| `extract_fields` | []object | - | Fields to extract from payload |

**Webhook Response Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mode` | string | `sync` | `sync`, `async`, or `callback` |
| `timeout` | duration | `30s` | Timeout for sync mode |
| `callback_field` | string | `callback_url` | Field containing callback URL |

See the [Automations Guide](../guides/automations.md) for complete documentation.

### Notifications Configuration

Configure outbound notifications for agent events.

```yaml
agents:
  order-processor:
    notifications:
      - id: slack-alert
        url: ${SLACK_WEBHOOK_URL}
        events: [task.completed, task.failed]
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | required | Unique notification identifier |
| `type` | string | `webhook` | Notification type |
| `url` | string | required | Webhook endpoint URL |
| `enabled` | bool | `true` | Enable/disable notification |
| `events` | []string | required | Events: `task.started`, `task.completed`, `task.failed` |
| `headers` | map | - | Custom HTTP headers |
| `payload` | object | - | Custom payload template |
| `retry` | object | - | Retry configuration |

**Retry Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_attempts` | int | `3` | Maximum retry attempts |
| `initial_delay` | duration | `1s` | Initial retry delay |
| `max_delay` | duration | `30s` | Maximum retry delay |

See the [Automations Guide](../guides/automations.md) for complete documentation.

### Multi-Agent Patterns

**Pattern 1: Transfer Control (sub_agents)**
```yaml
agents:
  coordinator:
    instruction: Route requests to specialists.
    sub_agents:
      - researcher
      - writer
  
  researcher:
    instruction: You research topics...
  
  writer:
    instruction: You write content...
```

**Pattern 2: Tool Delegation (agent_tools)**
```yaml
agents:
  orchestrator:
    instruction: Orchestrate complex tasks.
    agent_tools:
      - web_search
      - data_analysis
  
  web_search:
    instruction: Search the web...
  
  data_analysis:
    instruction: Analyze data...
```

### Context Window Management

Control how conversation history fits within LLM context limits.

```yaml
agents:
  assistant:
    context:
      strategy: buffer_window
      window_size: 20
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `strategy` | string | `none` | `none`, `buffer_window`, `token_window`, `summary_buffer` |
| `window_size` | int | `20` | Messages to keep (buffer_window) |
| `budget` | int | `8000` | Token budget (token_window, summary_buffer) |
| `threshold` | float | `0.85` | Summarization trigger (summary_buffer) |
| `target` | float | `0.7` | Post-summarization target (summary_buffer) |
| `preserve_recent` | int | `5` | Recent messages to always keep (token_window) |
| `summarizer_llm` | string | agent's LLM | LLM for summarization |

### Reasoning Loop

Configure the agent's tool-use reasoning loop.

```yaml
agents:
  assistant:
    reasoning:
      max_iterations: 100
      enable_exit_tool: true
      enable_escalate_tool: false
      termination_conditions:
        - no_tool_calls
        - escalate
        - transfer
```

### RAG Integration

Connect agents to document stores for context-aware responses.

```yaml
agents:
  researcher:
    document_stores:
      - codebase
      - docs
    include_context: true
    include_context_limit: 5
    include_context_max_length: 500
```

### Structured Output

Force agents to return JSON matching a schema.

```yaml
agents:
  classifier:
    structured_output:
      strict: true
      name: classification
      schema:
        type: object
        properties:
          category:
            type: string
            enum: [bug, feature, question]
          priority:
            type: integer
            minimum: 1
            maximum: 5
        required: [category, priority]
```

### Remote Agents

Connect to external A2A agents.

```yaml
agents:
  external:
    type: remote
    url: http://localhost:9000
    agent_card_url: http://localhost:9000/.well-known/agent-card.json
    agent_card_file: ./agent-card.json  # Alternative: local file
    headers:
      Authorization: "Bearer ${TOKEN}"
    timeout: "30s"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | - | Base URL of remote A2A server |
| `agent_card_url` | string | auto from url | URL to fetch agent card |
| `agent_card_file` | string | - | Local path to agent card JSON |
| `headers` | map | - | Custom HTTP headers |
| `timeout` | string | `30s` | Request timeout |

### Prompt Configuration

Advanced prompt configuration options.

```yaml
agents:
  assistant:
    prompt:
      system_prompt: "Complete system prompt override"
      role: "Senior Developer"
      guidance: "Focus on code quality."
    global_instruction: "Always respond in English."
```

---

## Tools

Define tools agents can use.

### MCP Tools

```yaml
tools:
  weather:
    type: mcp
    url: http://localhost:8081
    transport: sse
    filter:
      - get_weather
      - get_forecast
```

**Stdio Transport:**
```yaml
tools:
  filesystem:
    type: mcp
    transport: stdio
    command: npx
    args: ["@modelcontextprotocol/server-filesystem"]
    env:
      HOME: /home/user
```

### Function Tools

```yaml
tools:
  custom_search:
    type: function
    handler: grep_search
    description: Search for patterns in files
```

### Command Tools

```yaml
tools:
  shell:
    type: command
    working_directory: ./
    max_execution_time: 30s
    allowed_commands: [git, npm, python]
    denied_commands: [rm, sudo]
    deny_by_default: false
    require_approval: true
    approval_prompt: "Execute this command?"
```

### Tool Fields

**Common Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | `mcp` | `mcp`, `function`, `command` |
| `enabled` | bool | `true` | Whether tool is active |
| `description` | string | - | Tool description |
| `require_approval` | bool | varies | Require human approval (HITL) |
| `approval_prompt` | string | - | Message shown for approval |

**MCP Tool Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | - | MCP server URL |
| `transport` | string | auto-detect | `stdio`, `sse`, `streamable-http` |
| `command` | string | - | Command for stdio transport |
| `args` | []string | - | Arguments for stdio command |
| `env` | map | - | Environment variables for stdio |
| `filter` | []string | - | Limit exposed tools |

**Function Tool Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `handler` | string | required | Function handler name |
| `parameters` | object | - | Parameters schema |

**Command Tool Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `working_directory` | string | `./` | Working directory |
| `max_execution_time` | string | - | Timeout (e.g., `30s`) |
| `allowed_commands` | []string | - | Command whitelist |
| `denied_commands` | []string | - | Command blacklist |
| `deny_by_default` | bool | `false` | Require explicit whitelist |

---

## Databases

Configure SQL database connections.

```yaml
databases:
  main:
    driver: postgres
    host: localhost
    port: 5432
    database: hector
    username: ${DB_USER}
    password: ${DB_PASSWORD}
    ssl_mode: disable
    max_conns: 25
    max_idle: 5
```

**SQLite:**
```yaml
databases:
  local:
    driver: sqlite
    database: .hector/hector.db
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `driver` | string | required | `postgres`, `mysql`, `sqlite` |
| `host` | string | - | Database host (not for SQLite) |
| `port` | int | per-driver | Database port |
| `database` | string | required | Database name or SQLite file path |
| `username` | string | - | Database user |
| `password` | string | - | Database password |
| `ssl_mode` | string | `disable` | PostgreSQL SSL mode |
| `max_conns` | int | `25` | Maximum open connections |
| `max_idle` | int | `5` | Maximum idle connections |

---

## Embedders

Configure embedding providers for semantic search.

```yaml
embedders:
  default:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    dimension: 1536
    timeout: 30
    batch_size: 100
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provider` | string | `ollama` | `openai`, `ollama`, `cohere` |
| `model` | string | per-provider | Embedding model |
| `api_key` | string | - | API key (OpenAI, Cohere) |
| `base_url` | string | per-provider | API endpoint |
| `dimension` | int | auto-detect | Embedding dimension |
| `timeout` | int | `30` | Request timeout (seconds) |
| `batch_size` | int | `100` | Batch embedding size |

**Cohere-specific:**
```yaml
embedders:
  cohere:
    provider: cohere
    model: embed-english-v3.0
    input_type: search_document
    output_dimension: 1024
    truncate: END
```

---

## Vector Stores

Configure vector databases for document storage.

```yaml
vector_stores:
  local:
    type: chromem
    persist_path: .hector/vectors
    compress: false
  
  production:
    type: qdrant
    host: qdrant.example.com
    port: 6333
    api_key: ${QDRANT_API_KEY}
    enable_tls: true
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | `chromem` | `chromem`, `qdrant`, `pinecone`, `weaviate`, `milvus`, `chroma` |
| `host` | string | - | Server host (external stores) |
| `port` | int | per-type | Server port |
| `api_key` | string | - | API key |
| `enable_tls` | bool | `false` | Enable TLS |
| `persist_path` | string | - | File path (chromem) |
| `compress` | bool | `false` | Enable compression (chromem) |
| `collection` | string | - | Default collection name |

---

## Document Stores

Configure RAG document sources.

```yaml
document_stores:
  codebase:
    source:
      type: directory
      path: ./src
      include: ["*.go", "*.ts", "*.py"]
      exclude: ["vendor", "node_modules"]
      max_file_size: 10485760
    
    chunking:
      strategy: simple
      size: 1000
      overlap: 0
    
    vector_store: local
    embedder: default
    watch: true
    incremental_indexing: true
    
    search:
      top_k: 10
      threshold: 0.0
      enable_hyde: false
      enable_rerank: false
    
    indexing:
      max_concurrent: 8
      retry:
        max_retries: 3
        base_delay: 1s
        max_delay: 30s
```

### Source Types

**Directory:**
```yaml
source:
  type: directory
  path: ./docs
  include: ["*.md", "*.txt"]
  exclude: [".git", "node_modules"]
```

**SQL:**
```yaml
source:
  type: sql
  sql:
    database: main
    tables:
      - table: articles
        columns: [title, content]
        id_column: id
        updated_column: updated_at
```

**API:**
```yaml
source:
  type: api
  api:
    url: https://api.example.com/documents
    headers:
      Authorization: "Bearer ${TOKEN}"
    id_field: id
    content_field: body
```

### Chunking Strategies

| Strategy | Description |
|----------|-------------|
| `simple` | Split at fixed character intervals |
| `overlapping` | Split with overlap between chunks |
| `semantic` | Split at semantic boundaries |

### Document Store Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `source` | object | required | Document source configuration |
| `chunking` | object | - | Chunking configuration |
| `vector_store` | string | - | Vector store reference |
| `embedder` | string | - | Embedder reference |
| `collection` | string | - | Collection name override |
| `watch` | bool | `false` | Enable file watching |
| `incremental_indexing` | bool | `false` | Only re-index changed documents |
| `search` | object | - | Search behavior configuration |
| `indexing` | object | - | Indexing behavior configuration |
| `mcp_parsers` | object | - | MCP-based document parsing |

### Chunking Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `strategy` | string | `simple` | `simple`, `overlapping`, `semantic` |
| `size` | int | `1000` | Target chunk size (chars) |
| `overlap` | int | `0` | Overlap between chunks |
| `min_size` | int | `100` | Minimum chunk size |
| `max_size` | int | `2000` | Maximum chunk size |
| `preserve_words` | bool | `true` | Avoid splitting mid-word |

### Search Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `top_k` | int | `10` | Number of results |
| `threshold` | float | `0.0` | Minimum similarity score |
| `enable_hyde` | bool | `false` | Enable HyDE query expansion |
| `hyde_llm` | string | - | LLM for HyDE (required if enabled) |
| `enable_rerank` | bool | `false` | Enable LLM reranking |
| `rerank_llm` | string | - | LLM for reranking |
| `rerank_max_results` | int | `20` | Max candidates for reranking |
| `enable_multi_query` | bool | `false` | Enable query expansion |
| `multi_query_llm` | string | - | LLM for multi-query |
| `multi_query_count` | int | `3` | Number of query variants |

---

## Storage

Configure where data is persisted.

```yaml
storage:
  tasks:
    backend: sql
    database: main
  
  sessions:
    backend: sql
    database: main
  
  memory:
    backend: vector
    embedder: default
    vector_provider:
      type: chromem
      chromem:
        persist_path: .hector/chromem
  
  checkpoint:
    enabled: true
    strategy: hybrid
    after_tools: true
    recovery:
      auto_resume: true
      auto_resume_hitl: false
      timeout: 3600
```

### Tasks & Sessions

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `backend` | string | `inmemory` | `inmemory`, `sql` |
| `database` | string | - | Database reference (when `sql`) |

### Memory

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `backend` | string | `keyword` | `keyword`, `vector` |
| `embedder` | string | - | Embedder reference (when `vector`) |

### Checkpoint

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable checkpointing |
| `strategy` | string | `event` | `event`, `interval`, `hybrid` |
| `interval` | int | `0` | Checkpoint every N iterations |
| `after_tools` | bool | `false` | Checkpoint after tool execution |
| `before_llm` | bool | `false` | Checkpoint before LLM calls |

### Checkpoint Recovery

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `auto_resume` | bool | `false` | Auto-recover on startup |
| `auto_resume_hitl` | bool | `false` | Auto-resume INPUT_REQUIRED tasks |
| `timeout` | int | `3600` | Max checkpoint age (seconds) |

---

## Server

Configure network and security settings.

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  transport: json-rpc
  
  tls:
    enabled: true
    cert_file: /path/to/cert.pem
    key_file: /path/to/key.pem
  
  cors:
    allowed_origins: ["https://app.example.com"]
    allowed_methods: ["GET", "POST", "OPTIONS"]
    allowed_headers: ["Content-Type", "Authorization"]
  
  auth:
    enabled: true
    jwks_url: https://auth.example.com/.well-known/jwks.json
    issuer: https://auth.example.com
    audience: hector-api
    excluded_paths:
      - /health
      - /.well-known/agent-card.json
  
  studio:
    enabled: true
    allowed_roles: [operator, admin]
```

### Server Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `host` | string | `0.0.0.0` | Bind address |
| `port` | int | `8080` | HTTP port |
| `grpc_port` | int | `50051` | gRPC port |
| `transport` | string | `json-rpc` | `json-rpc`, `grpc` |

### Auth Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable JWT authentication |
| `jwks_url` | string | required | JWKS endpoint |
| `issuer` | string | required | Expected token issuer |
| `audience` | string | required | Expected token audience |
| `client_id` | string | - | Public client ID for frontend apps |
| `refresh_interval` | duration | `15m` | JWKS refresh interval |
| `require_auth` | bool | `true` | Reject unauthenticated requests |
| `excluded_paths` | []string | `["/health", "/.well-known/agent-card.json"]` | Paths that don't require auth |

### Studio Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable studio mode endpoints |
| `allowed_roles` | []string | `["operator", "admin"]` | JWT roles that can access studio |
| `config_path` | string | original file | Where config is saved |

---

## Rate Limiting

Configure request rate limits.

```yaml
rate_limiting:
  enabled: true
  scope: session
  backend: memory
  limits:
    - type: token
      window: hour
      limit: 100000
    - type: count
      window: minute
      limit: 60
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable rate limiting |
| `scope` | string | `session` | `session`, `user` |
| `backend` | string | `memory` | `memory`, `sql` |
| `sql_database` | string | - | Database reference (when `sql`) |

### Limit Rules

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `token`, `count` |
| `window` | string | `minute`, `hour`, `day`, `week`, `month` |
| `limit` | int | Maximum allowed in window |

---

## Guardrails

Configure safety controls for agents.

```yaml
guardrails:
  strict:
    enabled: true
    input:
      chain_mode: fail_fast
      length:
        enabled: true
        max_length: 100000
        action: block
      injection:
        enabled: true
        action: block
      sanitizer:
        enabled: true
        trim_whitespace: true
    output:
      pii:
        enabled: true
        detect_email: true
        detect_phone: true
        redact_mode: mask
        action: modify
    tool:
      authorization:
        enabled: true
        allowed_tools: ["read_*"]
        blocked_tools: ["*_delete"]
```

Reference guardrails in agents:
```yaml
agents:
  assistant:
    guardrails: strict
```

---

## Defaults

Set default values for agents.

```yaml
defaults:
  llm: default
```

---

## Logger

Configure logging behavior.

```yaml
logger:
  level: info
  file: hector.log
  format: simple
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `level` | string | `info` | `debug`, `info`, `warn`, `error` |
| `file` | string | stderr | Log file path |
| `format` | string | `simple` | `simple`, `verbose` |

---

## Hot Reload

Enable automatic configuration reload:

```bash
hector serve --config config.yaml --watch
```

Changes to the config file are automatically detected and applied without restart. Active sessions are preserved.

---

## Validation

Validate configuration before deployment:

```bash
hector validate --config config.yaml
```

Generate JSON Schema for IDE autocomplete:

```bash
hector schema > schema.json
```

---

