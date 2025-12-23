# Quick Start

Get an agent running in under 5 minutes. Hector always operates from a configuration file, which is automatically created if missing.

## Basic Usage

When you run `hector serve`, it checks for a configuration file. If none exists, it generates one based on your flags and environment variables.

### With Environment Variables

Start an agent with a single command:

### With Environment Variables

```bash
export OPENAI_API_KEY="sk-..."
hector serve
```

Hector automatically:

- Creates `.hector/config.yaml` if missing
- Detects your LLM provider from environment variables
- Starts a default agent

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

### With CLI Flags

CLI flags seed the initial configuration file:

```bash
hector serve --provider openai --model gpt-4o
```

The flags are used to generate `.hector/config.yaml`. On subsequent runs, the existing config is used (not overwritten) unless you manually edit it or delete it.

### With SKILL.md

If you have a `SKILL.md` file in your directory, Hector automatically detects it and configures the agent instruction from it.

```markdown
---
name: My Agent
description: A helpful assistant
allowed-tools: [search]
---
You are a helpful assistant.
```

```bash
hector serve
```

## With Tools

Enable built-in tools:

```bash
hector serve --model gpt-4o --tools all
```

Or specific tools:

```bash
hector serve --model gpt-4o --tools text_editor,grep_search,bash
```

## With RAG

Enable document search from a folder:

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --tools all
```

Hector automatically:

- Creates an embedded vector database (chromem)
- Detects and configures an embedder
- Indexes documents (PDF, DOCX, XLSX supported natively)
- Adds search tool to your agent
- Watches for file changes and re-indexes

**With automatic context injection (no search tool needed):**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --include-context
```

**With external vector database:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --vector-type qdrant \
  --vector-host localhost:6333 \
  --tools all
```

**With Docling for advanced PDF/document parsing:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document \
  --tools all
```

## With MCP

Connect to an MCP server for external tools:

```bash
hector serve \
  --model gpt-4o \
  --mcp-url http://localhost:8000/mcp
```

## With Persistence

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

## With Observability

Enable metrics and tracing:

```bash
hector serve \
  --model gpt-4o \
  --observe
```

Access:

- Metrics: `http://localhost:8080/metrics`
- Traces: sent to OTLP endpoint `localhost:4317`

## With Authentication

Enable JWT authentication:

```bash
hector serve \
  --model gpt-4o \
  --auth-jwks-url https://auth.example.com/.well-known/jwks.json \
  --auth-issuer https://auth.example.com/ \
  --auth-audience my-api
```

This secures all endpoints by default. To make auth optional: `hector serve ... --no-auth-required`.

## Full Example

Combine all features:

```bash
export OPENAI_API_KEY="sk-..."

hector serve \
  --model gpt-4o \
  --tools all \
  --docs-folder ./documents \
  --storage sqlite \
  --observe
```

## Using a Config File

For repeatable deployments, create or edit the configuration file directly.

### Minimal Config

Create `.hector/config.yaml`:

```yaml
llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}

agents:
  assistant:
    llm: default
```

Start the server:

```bash
export OPENAI_API_KEY="sk-..."
hector serve
```

Or specify a custom config path:

```bash
hector serve --config my-config.yaml
```

### Complete Config

```yaml
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

## Testing Your Agent

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

Enable the Studio API to allow Hector Studio (desktop app) to connect and manage configuration:

```bash
hector serve --studio
```

Launch **Hector Studio** and connect to your server.

> [!CAUTION]
> **Security Warning**: Studio Mode enables remote configuration editing.
> **DO NOT** enable this in production unless protected by authentication.

## Provider-Specific Examples

### Anthropic

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
hector serve --provider anthropic --model claude-haiku-4-5
```

### Google Gemini

```bash
export GEMINI_API_KEY="..."
hector serve --provider gemini --model gemini-2.5-pro
```

### Ollama (Local)

```bash
# Start Ollama first: ollama serve
hector serve --provider ollama --model qwen3
```

## Environment Variables

Set API keys via environment:

- `OPENAI_API_KEY` - OpenAI API key
- `ANTHROPIC_API_KEY` - Anthropic API key
- `GEMINI_API_KEY` - Google Gemini API key
- `TAVILY_API_KEY` - Tavily API key (for web_search tool)
- `MCP_URL` - MCP server URL

