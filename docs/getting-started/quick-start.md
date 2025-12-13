# Quick Start

Get an agent running in under 5 minutes using either zero-config mode (CLI flags) or a configuration file.

## Zero-Config Mode

Start an agent with a single command—no configuration file needed.

### Basic Agent

```bash
export OPENAI_API_KEY="sk-..."
hector serve --model gpt-4o
```

The server starts at `http://localhost:8080`. Test it:

```bash
curl -X POST http://localhost:8080/agents/assistant/message:send \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "parts": [{"text": "Hello!"}],
      "role": "user"
    }
  }'
```

### With Tools

Enable built-in tools:

```bash
hector serve --model gpt-4o --tools
```

Or specific tools:

```bash
hector serve --model gpt-4o --tools read_file,write_file,execute_command
```

### With RAG

Enable document search from a folder:

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --tools
```

Hector automatically:
- Creates an embedded vector database (chromem)
- Detects and configures an embedder
- Indexes documents (PDF, DOCX, XLSX supported natively)
- Adds search tool to your agent
- Watches for file changes and re-indexes

**With external vector database:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --vector-type qdrant \
  --vector-host localhost:6333 \
  --tools
```

**With Docling for advanced PDF/document parsing:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document \
  --tools
```

### With MCP

Connect to an MCP server for external tools:

```bash
hector serve \
  --model gpt-4o \
  --mcp-url http://localhost:8000/mcp
```

### With Persistence

Enable task and session persistence with SQLite:

```bash
hector serve \
  --model gpt-4o \
  --storage sqlite
```

Checkpoint/recovery is automatically enabled. Database is stored in `.hector/hector.db`.

For PostgreSQL or MySQL:

```bash
# PostgreSQL
hector serve \
  --model gpt-4o \
  --storage postgres \
  --storage-db "host=localhost port=5432 user=hector password=secret dbname=hector"

# MySQL
hector serve \
  --model gpt-4o \
  --storage mysql \
  --storage-db "hector:secret@tcp(localhost:3306)/hector"
```

### With Observability

Enable metrics and tracing:

```bash
hector serve \
  --model gpt-4o \
  --observe
```

Access:
- Metrics: `http://localhost:8080/metrics`
- Traces: sent to OTLP endpoint `localhost:4317`

### With Authentication

Enable JWT authentication (Zero-Config):

```bash
hector serve \
  --model gpt-4o \
  --auth-jwks-url https://auth.example.com/.well-known/jwks.json \
  --auth-issuer https://auth.example.com/ \
  --auth-audience my-api
```

This secures all endpoints by default. To make auth optional: `hector serve ... --no-auth-required`.

### Full Example

Combine all features:

```bash
export OPENAI_API_KEY="sk-..."

hector serve \
  --model gpt-4o \
  --tools \
  --docs-folder ./documents \
  --storage sqlite \
  --observe
```

## Config File Mode

Create a configuration file for repeatable deployments.

### Minimal Config

Create `config.yaml`:

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

Start the server:

```bash
export OPENAI_API_KEY="sk-..."
hector serve --config config.yaml
```

### Complete Config

Create `config.yaml`:

```yaml
version: "2"

llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
    temperature: 0.7
    max_tokens: 4096

tools:
  mcp:
    type: mcp
    url: ${MCP_URL}

agents:
  assistant:
    name: Assistant
    description: A helpful AI assistant
    llm: default
    tools: [mcp, search]
    streaming: true
    instruction: |
      You are a helpful AI assistant.
      Use available tools when needed.
      Be concise and accurate.

server:
  port: 8080
  cors:
    allowed_origins: ["*"]
```

Start:

```bash
export OPENAI_API_KEY="sk-..."
export MCP_URL="http://localhost:8000/mcp"
hector serve --config config.yaml
```

## Testing Your Agent

### Web UI

**Note:** The Web UI is only available when running in **Studio Mode** (`--studio`).

Open `http://localhost:8080` in your browser for an active visual dashboard.

### Agent Discovery

List available agents:

```bash
curl http://localhost:8080/agents
```

Get agent card:

```bash
curl http://localhost:8080/.well-known/agent-card.json
```

### Send Message

```bash
curl -X POST http://localhost:8080/agents/assistant/message:send \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "parts": [{"text": "What is the capital of France?"}],
      "role": "user"
    }
  }'
```

### Streaming

```bash
curl -X POST http://localhost:8080/agents/assistant/message:stream \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "message": {
      "parts": [{"text": "Tell me a story"}],
      "role": "user"
    }
  }'
```

## Studio Mode

Enable the config builder UI:

```bash
hector serve --model gpt-4o --studio
```

Access the studio at `http://localhost:8080`. Changes are saved to `.hector/config.yaml` and auto-reload.

> [!CAUTION]
> **Security Warning**: Studio Mode enables a full configuration editor and Web UI.
> **DO NOT** enable this in production (`--studio`) unless it is strictly internal or protected by authentication.

## Provider-Specific Examples

### Anthropic

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
hector serve --provider anthropic --model claude-sonnet-4-20250514
```

### Google Gemini

```bash
export GEMINI_API_KEY="..."
hector serve --provider gemini --model gemini-2.0-flash-exp
```

### Ollama (Local)

```bash
# Start Ollama first: ollama serve
hector serve --provider ollama --model llama3.3
```

## Environment Variables

Set API keys via environment:

- `OPENAI_API_KEY` - OpenAI API key
- `ANTHROPIC_API_KEY` - Anthropic API key
- `GEMINI_API_KEY` - Google Gemini API key
- `MCP_URL` - MCP server URL

## Next Steps

- [Configuration Guide](../guides/configuration.md) - Learn configuration in depth
- [Agents Guide](../guides/agents.md) - Configure advanced agent behavior
- [Deployment Guide](../guides/deployment.md) - Deploy to production
