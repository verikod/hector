# Configuration

Hector always operates from a configuration file. If no config file exists, one is automatically generated from CLI flags and environment variables.

## How Configuration Works

1. **First run**: Hector generates `.hector/config.yaml` from CLI flags
2. **Subsequent runs**: Existing config is loaded (never overwritten)
3. **CLI flags**: Seed initial config or can be used to override specific values during runtime (overrides are transient unless saved).



## CLI Flags for Config Generation

When Hector generates a config file, these flags determine its contents:

**LLM Options:**

| Flag | Description | Example |
|------|-------------|---------|
| `--provider` | LLM provider | `openai`, `anthropic`, `ollama` |
| `--model` | Model name | `gpt-4o`, `claude-sonnet-4-20250514` |
| `--api-key` | API key (or use env var) | `sk-...` |
| `--base-url` | Custom API endpoint | `http://localhost:11434/v1` |
| `--temperature` | Sampling temperature | `0.7` |
| `--max-tokens` | Max response tokens | `4096` |
| `--thinking` | Enable extended thinking (Claude) | flag |
| `--thinking-budget` | Thinking token budget | `1024` |

**Tool Options:**

| Flag | Description | Example |
|------|-------------|---------|
| `--tools` | Enable all built-in tools | flag |
| `--tools` | Enable specific tools | `text_editor,grep_search,search` |
| `--mcp-url` | MCP server URL | `http://localhost:8000/mcp` |
| `--approve-tools` | Always require approval | `bash,text_editor` |
| `--no-approve-tools` | Never require approval | `grep_search,search` |

**RAG Options:**

| Flag | Description | Example |
|------|-------------|---------|
| `--docs-folder` | Documents folder for RAG | `./documents` |
| `--docs-folder` | With Docker path mapping | `./docs:/docs` |
| `--embedder-model` | Embedder model | `text-embedding-3-small` |
| `--embedder-provider` | Embedder provider | `openai`, `ollama`, `cohere` |
| `--embedder-url` | Custom embedder URL | `http://localhost:11434` |
| `--rag-watch` / `--no-rag-watch` | File watching | default: enabled |
| `--mcp-parser-tool` | MCP tool for document parsing | `convert_document_into_docling_document` |

**Vector Database Options:**

| Flag | Description | Example |
|------|-------------|---------|
| `--vector-type` | Vector DB type | `chromem`, `qdrant`, `chroma`, `pinecone`, `weaviate`, `milvus` |
| `--vector-host` | Vector DB host:port | `localhost:6333` |
| `--vector-api-key` | Vector DB API key | `your-api-key` |

**Persistence Options:**

| Flag | Description | Example |
|------|-------------|---------|
| `--storage` | Storage backend | `sqlite`, `postgres`, `mysql` |
| `--storage-db` | Database connection string | `host=localhost dbname=hector` |

**Server Options:**

| Flag | Description | Example |
|------|-------------|---------|
| `--port` | Server port | `8080` |
| `--host` | Server host | `0.0.0.0` |
| `--studio` | Enable studio mode | flag |
| `--observe` | Enable observability | flag |

**Example with multiple options:**

```bash
hector serve \
  --provider anthropic \
  --model claude-sonnet-4-20250514 \
  --docs-folder ./documents:/docs \
  --vector-type qdrant \
  --vector-host localhost:6333 \
  --embedder-provider openai \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document \
  --tools \
  --storage sqlite
```

## Configuration File

For repeatable deployments, use or edit the configuration file directly:

```bash
# Use default path (.hector/config.yaml)
hector serve

# Use custom path
hector serve --config my-config.yaml
```

Configuration file features:

- Production-ready and version-controlled
- Complete control over all settings
- Supports hot reload with `--watch`
- Required for multi-agent setups

## File Structure

### Minimal Configuration

```yaml
version: "2"

llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}

agents:
  assistant:
    llm: default

server:
  port: 8080
```

### Complete Structure

```yaml
version: "2"
name: my-project
description: Production agent deployment

# Database connections
databases:
  main:
    driver: postgres
    host: localhost
    port: 5432
    database: hector
    user: ${DB_USER}
    password: ${DB_PASSWORD}

# Vector stores for RAG
vector_stores:
  qdrant:
    type: qdrant
    host: localhost
    port: 6334

# LLM providers
llms:
  openai:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
    temperature: 0.7
    max_tokens: 4096

# Embedding providers
embedders:
  default:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}

# Tools
tools:
  mcp:
    type: mcp
    url: ${MCP_URL}

  search:
    type: function
    handler: search
    require_approval: false

# Agents
agents:
  assistant:
    name: Assistant
    description: A helpful AI assistant
    llm: openai
    tools: [mcp, search]
    streaming: true
    instruction: |
      You are a helpful AI assistant.
      Use tools when appropriate.

# Document stores for RAG
document_stores:
  docs:
    source:
      type: directory
      path: ./documents
      exclude: [.git, node_modules]
    chunking:
      strategy: simple
      size: 1000
      overlap: 200
    vector_store: qdrant
    embedder: default
    watch: true

# Server configuration (infrastructure - NOT modifiable via Studio API)
server:
  port: 8080
  transport: http
  cors:
    allowed_origins: ["*"]

# Storage configuration (persistence - modifiable via Studio API)
storage:
  tasks:
    backend: sql
    database: main
  sessions:
    backend: sql
    database: main
  checkpoint:
    enabled: true
    strategy: hybrid
    after_tools: true
    before_llm: true
    recovery:
      auto_resume: true
      auto_resume_hitl: false
      timeout: 3600

# Observability configuration (telemetry - modifiable via Studio API)
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: localhost:4317
  metrics:
    enabled: true
```

## Environment Variables

Reference environment variables using `${VAR_NAME}` syntax:

```yaml
llms:
  default:
    api_key: ${OPENAI_API_KEY}
    base_url: ${CUSTOM_BASE_URL}
```

Load variables from `.env` file:

```bash
# .env
OPENAI_API_KEY=sk-...
MCP_URL=http://localhost:8000/mcp
```

Hector automatically loads `.env` from:
1. Current directory (`./.env`)
2. Config file directory (`./.env` relative to config)
3. Home directory (`~/.hector/.env`)

## Configuration Validation

Validate configuration before deployment:

```bash
hector validate --config config.yaml
```

Generate JSON Schema for IDE autocomplete:

```bash
hector schema > schema.json
```

Use in VSCode (`.vscode/settings.json`):

```json
{
  "yaml.schemas": {
    "./schema.json": "config.yaml"
  }
}
```

## Ephemeral Mode

Run without generating a configuration file (useful for Docker/CI):

```bash
hector serve --ephemeral --model gpt-4o --tools all
```

In ephemeral mode:
- No `.hector/config.yaml` file is created
- Configuration is generated in-memory from CLI flags
- `.env` files are still loaded from current directory
- Cannot be used with `--studio` or `--watch`

This is ideal for:
- **Docker containers**: Run without persisting state
- **CI/CD pipelines**: Stateless test runs
- **Quick experiments**: Try configurations without polluting the filesystem

## Hot Reload

Enable hot reload to update configuration without restarting:

```bash
hector serve --config config.yaml --watch

```

When the config file or `.env` file changes:

- Environment variables are reloaded from `.env`
- Configuration is reloaded
- Runtime is updated
- Agents are rebuilt
- Server continues running
- Active sessions are preserved

Hot reload works for:

- Agent configuration changes
- Tool additions/removals
- LLM parameter updates
- Server settings (except port)
- Environment variables in `.env`


## Studio Mode

Enable configuration API for external tools like [Hector Studio](https://github.com/verikod/hector-studio):

```bash
hector serve --studio
```

> [!NOTE]
> Studio mode automatically enables `--watch` for hot-reload.

Studio mode enables:

- `/api/config` endpoint for reading/writing configuration
- `/api/schema` endpoint for JSON Schema
- Real-time validation on config changes
- Automatic hot reload on config save

Use with Hector Studio or any HTTP client to manage configuration remotely.

## Editing Configuration

### Using Studio Mode

Enable Studio to edit configuration via UI:

```bash
hector serve --studio
```

Then connect with Hector Studio desktop app or use the API:

```bash
# Fetch current config
curl http://localhost:8080/api/config

# Update config
curl -X PUT http://localhost:8080/api/config -d @new-config.yaml
```

### Manual Editing

Edit `.hector/config.yaml` directly. If using `--watch`, changes apply automatically.
  --tools \
  --storage sqlite
```

## Default Values

Hector applies smart defaults when values are omitted:

### LLM Defaults

```yaml
llms:
  default:
    provider: openai  # Required
    model: gpt-4o     # Required
    # Defaults:
    temperature: 0.7
    max_tokens: 4096
    streaming: false
```

### Agent Defaults

```yaml
agents:
  assistant:
    llm: default  # Required
    # Defaults:
    name: assistant
    streaming: false
    tools: []
```

### Server/Storage Defaults

```yaml
server:
  # Defaults:
  port: 8080
  transport: http

storage:
  # Defaults (in-memory, no persistence):
  tasks:
    backend: inmemory
  sessions:
    backend: inmemory
```

## Configuration Organization

### Single File

Simple deployments use one file:

```yaml
# config.yaml
version: "2"
llms: {...}
agents: {...}
```

### Environment-Specific

Separate configs per environment:

```bash
configs/
├── development.yaml
├── staging.yaml
└── production.yaml
```

Deploy with:

```bash
hector serve --config configs/production.yaml
```

### Shared Configuration

Use environment variables for environment-specific values:

```yaml
# config.yaml (shared)
llms:
  default:
    provider: openai
    model: ${LLM_MODEL}
    api_key: ${OPENAI_API_KEY}

server:
  port: ${PORT}
```

```bash
# .env.development
LLM_MODEL=gpt-4o-mini
PORT=8080

# .env.production
LLM_MODEL=gpt-4o
PORT=8080
```

## Configuration Defaults

Set global defaults for agents:

```yaml
defaults:
  llm: default

agents:
  assistant:
    # Inherits llm: default
    tools: [search]

  analyst:
    # Inherits llm: default
    tools: [search, text_editor]
```

## Best Practices

### Version Control

Commit configuration files:

```bash
git add config.yaml
git commit -m "Update agent configuration"
```

Never commit secrets—use environment variables:

```yaml
# ✅ Good
api_key: ${OPENAI_API_KEY}

# ❌ Bad
api_key: sk-proj-abc123...
```

### Configuration Testing

Test configuration changes locally before deploying:

```bash
# Validate
hector validate --config config.yaml

# Test locally
hector serve --config config.yaml

# Deploy
kubectl apply -f deployment.yaml
```

### Minimal Configuration

Only specify non-default values:

```yaml
# ✅ Concise
llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}

# ❌ Verbose
llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
    temperature: 0.7      # default
    max_tokens: 4096      # default
    streaming: false      # default
```

### Documentation

Document your configuration:

```yaml
version: "2"
name: customer-support
description: |
  Customer support agent system with:
  - RAG over documentation
  - Ticket creation via MCP
  - Persistent sessions

llms:
  # Production LLM for customer-facing responses
  production:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
```

## Common Patterns

### Multi-Agent System

```yaml
llms:
  fast:
    provider: openai
    model: gpt-4o-mini

  powerful:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}

agents:
  router:
    llm: fast
    tools: [agent_call]
    instruction: Route requests to specialized agents

  specialist:
    llm: powerful
    tools: [search, text_editor]
    instruction: Handle complex queries with tools
```

### Development vs Production

```yaml
llms:
  default:
    provider: openai
    model: ${LLM_MODEL}  # gpt-4o-mini (dev), gpt-4o (prod)
    api_key: ${OPENAI_API_KEY}

observability:
  tracing:
    enabled: ${TRACING_ENABLED}  # false (dev), true (prod)
    endpoint: ${OTLP_ENDPOINT}
  metrics:
    enabled: ${METRICS_ENABLED}  # false (dev), true (prod)
```






## Checkpoint Recovery

Configure how the agent saves state and recovers from failures.

### Strategies

*   **`event` (Default)**: Saves a checkpoint after every agent turn (Post-LLM). Recommended for most use cases as it provides the safest recovery point.
*   **`interval`**: Saves a checkpoint every `N` iterations. In v2, this adds to the base event strategy.
*   **`hybrid`**: Explicit combination of event and interval strategies.

### Recovery Options

*   **`auto_resume`**: Automatically reloads the last checkpoint on server startup.
*   **`auto_resume_hitl`**: Automatically resumes tasks that were paused waiting for human input (e.g., tool approval). 
    *   **Enabled**: The agent checks the session history for approval decisions and proceeds without waiting.
    *   **Disabled**: The agent remains paused until explicitly nudged.
