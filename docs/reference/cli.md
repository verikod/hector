# CLI Reference

Complete reference for the `hector` command-line interface.

## How It Works

Hector always operates from a configuration file:

1. If config file exists → Load and use it
2. If config file is missing → Generate from CLI flags and environment variables
3. On subsequent runs → Use existing config (never overwritten)

The default config path is `.hector/config.yaml`. Use `--config` to specify a different location.

---

## Global Flags

Available for all commands:

| Flag | Default | Description |
|------|---------|-------------|
| `-c, --config` | `.hector/config.yaml` | Path to configuration file |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `--log-file` | stderr | Log file path (empty = stderr) |
| `--log-format` | `simple` | Log format: `simple`, `verbose` |
| `-h, --help` | - | Show help |

---

## Commands

### version

```bash
hector version
```

### serve

Start the A2A server.

```bash
hector serve [flags]
```

#### LLM Options

These flags seed the initial config when generating a new configuration file:

| Flag | Default | Description |
|------|---------|-------------|
| `--provider` | auto | LLM provider: `anthropic`, `openai`, `gemini`, `ollama` |
| `--model` | - | Model name (e.g., `gpt-4o`, `claude-haiku-4-5`) |
| `--api-key` | env var | API key (defaults to provider-specific env var) |
| `--base-url` | - | Custom API base URL |
| `--temperature` | - | Temperature for generation (0.0-2.0) |
| `--max-tokens` | - | Max tokens for generation |

#### Agent Options

| Flag | Default | Description |
|------|---------|-------------|
| `--instruction` | - | System instruction for the agent |
| `--role` | - | Agent role |

#### Tool Options

| Flag | Default | Description |
|------|---------|-------------|
| `--tools` | - | Enable built-in tools: `all` for all tools, or comma-separated list |
| `--mcp-url` | - | MCP server URL |
| `--approve-tools` | - | Require approval for specific tools (comma-separated) |
| `--no-approve-tools` | - | Disable approval for specific tools (comma-separated) |

#### Thinking (Extended Reasoning)

| Flag | Default | Description |
|------|---------|-------------|
| `--thinking` / `--no-thinking` | off | Enable thinking at API level |
| `--thinking-budget` | `0` | Token budget for thinking |

#### RAG Options

| Flag | Default | Description |
|------|---------|-------------|
| `--docs-folder` | - | Folder containing documents for RAG |
| `--rag-watch` / `--no-rag-watch` | on | Watch docs folder for changes |
| `--include-context` / `--no-include-context` | off | Auto-inject RAG context into prompts |
| `--mcp-parser-tool` | - | MCP tool name(s) for document parsing |
| `--vector-type` | `chromem` | Vector database type |
| `--vector-host` | - | Vector database host:port |
| `--vector-api-key` | - | Vector database API key |
| `--embedder-provider` | auto | Embedder provider: `openai`, `ollama`, `cohere` |
| `--embedder-model` | auto | Embedder model |
| `--embedder-url` | - | Embedder API base URL |

#### Storage Options

| Flag | Default | Description |
|------|---------|-------------|
| `--storage` | `inmemory` | Storage backend: `sqlite`, `postgres`, `mysql` |
| `--storage-db` | - | Database path/DSN |

#### Observability

| Flag | Default | Description |
|------|---------|-------------|
| `--observe` | off | Enable observability (metrics + OTLP tracing) |

#### Server Options

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `0.0.0.0` | Host to bind to |
| `--port` | `8080` | Port to listen on |
| `--stream` / `--no-stream` | on | Enable/disable streaming responses |
| `--watch` | off | Watch config file for changes |
| `--ephemeral` | off | Run without saving config to disk (for Docker/CI) |


#### Authentication

| Flag | Default | Description |
|------|---------|-------------|
| `--auth-jwks-url` | - | JWKS URL for JWT authentication |
| `--auth-issuer` | - | JWT issuer |
| `--auth-audience` | - | JWT audience |
| `--auth-client-id` | - | Public Client ID for frontend apps |
| `--auth-required` / `--no-auth-required` | on | Require authentication |

#### Studio Mode

Enable Hector Studio (desktop app) to connect and manage configuration:

```bash
# Enable studio mode
hector serve --studio

# With role-based access (requires auth)
hector serve --studio --studio-roles admin,operator
```

| Flag | Default | Description |
|------|---------|-------------|
| `--studio` | off | Enable studio mode: config builder UI + auto-reload |
| `--studio-roles` | `operator` | Comma-separated roles allowed to access studio |

---

### info

Show agent information.

```bash
hector info [<agent>] --config config.yaml
hector info --config config.yaml        # List all agents
hector info assistant --config config.yaml  # Show specific agent
```

### validate

Validate a configuration file.

```bash
hector validate <config> [flags]
hector validate config.yaml
hector validate config.yaml --format json
hector validate config.yaml --print-config
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--format` | `-f` | `compact` | Output format: `compact`, `verbose`, `json` |
| `--print-config` | `-p` | off | Print expanded config (with defaults and env vars resolved) |

### schema

Generate JSON Schema for configuration.

```bash
hector schema [flags]
hector schema > schema.json
hector schema --compact
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--compact` | `-c` | off | Compact JSON output (no indentation) |

---

## Built-in Tools

When you use `--tools`:

| Tool | Description | Requires Approval |
|------|-------------|-------------------|
| `text_editor` | View and modify files | **Yes** |
| `grep_search` | Search files with regex | No |
| `apply_patch` | Apply patches with context | **Yes** |
| `bash` | Execute shell commands | **Yes** |
| `web_search` | Search the internet (Tavily) | No |
| `web_fetch` | Fetch URL content | No |
| `web_request` | Make HTTP requests | **Yes** |
| `todo_write` | Manage task lists | No |

Override approval defaults:

```bash
# Disable approval for text_editor
hector serve --model gpt-4o --tools all --no-approve-tools text_editor

# Enable approval for grep_search
hector serve --model gpt-4o --tools all --approve-tools grep_search
```

---

## Examples

### First Run (generates config)

```bash
# Minimal: auto-generates config from environment
export OPENAI_API_KEY="sk-..."
hector serve

# With CLI flags (seeds initial config)
hector serve --model gpt-4o --tools all

# With RAG
hector serve --model gpt-4o --docs-folder ./documents --tools all

# With persistence
hector serve --model gpt-4o --tools all --storage sqlite
```

### Subsequent Runs (uses existing config)

```bash
# Just run - uses existing .hector/config.yaml
hector serve

# Specify custom config path
hector serve --config my-config.yaml
```

### With Authentication

```bash
hector serve \
  --auth-jwks-url https://auth.example.com/.well-known/jwks.json \
  --auth-issuer https://auth.example.com/ \
  --auth-audience hector-api
```

### Studio Mode

```bash
# Enable studio for config editing
hector serve --studio

# With role-based access control
hector serve \
  --studio \
  --studio-roles admin,operator \
  --auth-jwks-url https://auth.example.com/.well-known/jwks.json \
  --auth-issuer https://auth.example.com/ \
  --auth-audience hector-api
```

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENAI_API_KEY` | OpenAI API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `GEMINI_API_KEY` | Google Gemini API key |
| `COHERE_API_KEY` | Cohere API key |
| `MCP_URL` | MCP server URL |

Environment variables can be referenced in configuration files using `${VAR_NAME}` syntax.
