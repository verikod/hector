# CLI Reference

Complete reference for the `hector` command-line interface.

## Two Operating Modes

Hector has two **mutually exclusive** operating modes:

| Mode | Description | When to Use |
|------|-------------|-------------|
| **Config Mode** | Uses YAML configuration file | Production, complex setups, multi-agent |
| **Zero-Config Mode** | Uses CLI flags only | Quick testing, simple single-agent |

> [!IMPORTANT]
> You **cannot** mix these modes. Using `--config` with any zero-config flag will produce an error.

### Config Mode

```bash
hector serve --config agents.yaml
```

All agent, LLM, and tool configuration comes from the YAML file.

### Zero-Config Mode

```bash
hector serve --model gpt-5 --tools all
```

A single default agent is created from CLI flags. No YAML file needed.

---

## Global Flags

Available for all commands:

| Flag | Default | Description |
|------|---------|-------------|
| `-c, --config` | - | Path to configuration file (enables Config Mode) |
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

#### Zero-Config Flags

These flags are **only valid in Zero-Config Mode** (without `--config`):

**LLM Configuration:**

| Flag | Default | Description |
|------|---------|-------------|
| `--provider` | auto | LLM provider: `anthropic`, `openai`, `gemini`, `ollama` |
| `--model` | - | Model name (e.g., `gpt-4o`, `claude-haiku-4-5`) |
| `--api-key` | env var | API key (defaults to provider-specific env var) |
| `--base-url` | - | Custom API base URL |
| `--temperature` | `0.7` | Temperature for generation |
| `--max-tokens` | `4096` | Max tokens for generation |

**Agent Configuration:**

| Flag | Default | Description |
|------|---------|-------------|
| `--instruction` | - | System instruction for the agent |
| `--role` | - | Agent role |

**Tool Configuration:**

| Flag | Default | Description |
|------|---------|-------------|
| `--tools` | - | Enable built-in tools. Use `--tools all` or `--tools` for all tools, or `--tools read_file,write_file` for specific tools |
| `--mcp-url` | - | MCP server URL |
| `--approve-tools` | - | Require approval for specific tools (comma-separated) |
| `--no-approve-tools` | - | Disable approval for specific tools (comma-separated) |

**Thinking (Extended Reasoning):**

| Flag | Default | Description |
|------|---------|-------------|
| `--thinking` / `--no-thinking` | off | Enable thinking at API level |
| `--thinking-budget` | `0` | Token budget for thinking |

**RAG Configuration:**

| Flag | Default | Description |
|------|---------|-------------|
| `--docs-folder` | - | Folder containing documents for RAG |
| `--rag-watch` / `--no-rag-watch` | on | Watch docs folder for changes |
| `--include-context` / `--no-include-context` | off | Auto-inject RAG context into prompts (no need to call search) |
| `--mcp-parser-tool` | - | MCP tool name(s) for document parsing |
| `--vector-type` | `chromem` | Vector database type |
| `--vector-host` | - | Vector database host:port |
| `--vector-api-key` | - | Vector database API key |
| `--embedder-provider` | auto | Embedder provider: `openai`, `ollama`, `cohere` |
| `--embedder-model` | auto | Embedder model |
| `--embedder-url` | - | Embedder API base URL |

**Storage Configuration:**

| Flag | Default | Description |
|------|---------|-------------|
| `--storage` | `inmemory` | Storage backend: `sqlite`, `postgres`, `mysql` |
| `--storage-db` | - | Database path/DSN |

**Observability:**

| Flag | Default | Description |
|------|---------|-------------|
| `--observe` | off | Enable observability (metrics + OTLP tracing) |

#### Flags for Both Modes

These flags work with either Config Mode or Zero-Config Mode:

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | Port to listen on |
| `--stream` / `--no-stream` | on | Enable/disable streaming responses |
| `--watch` | off | Watch config file for changes (Config Mode only) |

**Authentication (works in both modes):**

| Flag | Default | Description |
|------|---------|-------------|
| `--auth-jwks-url` | - | JWKS URL for JWT authentication |
| `--auth-issuer` | - | JWT issuer |
| `--auth-audience` | - | JWT audience |
| `--auth-required` / `--no-auth-required` | on | Require authentication |

#### Studio Mode

> [!IMPORTANT]
> `--studio` **requires** `--config`. It cannot be used with Zero-Config Mode.

```bash
# Correct: Studio with config file
hector serve --config agents.yaml --studio

# With role-based access (requires auth)
hector serve --config agents.yaml --studio --studio-roles admin,operator

# Wrong: Studio without config (will error)
hector serve --model gpt-5 --studio  # ERROR!
```

| Flag | Default | Description |
|------|---------|-------------|
| `--studio` | off | Enable studio mode: config builder UI + auto-reload |
| `--studio-roles` | `operator` | Comma-separated roles allowed to access studio (when auth enabled) |
| `--host` | `0.0.0.0` | Host to bind to |

---

### info

Show agent information (requires `--config`).

```bash
hector info [<agent>] --config config.yaml
```

### validate

Validate a configuration file.

```bash
hector validate <config> [--format compact|verbose|json] [--print-config]
```

### schema

Generate JSON Schema for configuration.

```bash
hector schema [--compact]
```

---

## Built-in Tools

When you use `--tools` in Zero-Config Mode:

| Tool | Description | Requires Approval |
|------|-------------|-------------------|
| `read_file` | Read file contents | No |
| `write_file` | Create or overwrite files | **Yes** |
| `grep_search` | Search files with regex | No |
| `search_replace` | Find and replace in files | **Yes** |
| `apply_patch` | Apply patches with context | **Yes** |
| `execute_command` | Execute shell commands | **Yes** |
| `web_request` | Make HTTP requests | **Yes** |
| `todo_write` | Manage task lists | No |

Override approval defaults:

```bash
# Disable approval for write_file
hector serve --model gpt-5 --tools all --no-approve-tools write_file

# Enable approval for read_file
hector serve --model gpt-5 --tools all --approve-tools read_file
```

---

## Examples

### Zero-Config Mode

```bash
# Minimal: single agent
hector serve --model gpt-5

# With all tools
hector serve --model gpt-5 --tools all

# With specific tools
hector serve --model gpt-5 --tools read_file,grep_search

# With RAG (agent calls search tool)
hector serve --model gpt-5 --tools all --docs-folder ./documents

# With RAG and auto-context (context injected automatically)
hector serve --model gpt-5 --docs-folder ./documents --include-context

# With persistence
hector serve --model gpt-5 --tools all --storage sqlite
```

### Config Mode

```bash
# Basic
hector serve --config agents.yaml

# With studio (for editing config)
hector serve --config agents.yaml --studio

# With JWT authentication
hector serve --config agents.yaml \
  --auth-jwks-url https://auth.example.com/.well-known/jwks.json \
  --auth-issuer https://auth.example.com/ \
  --auth-audience hector-api

# Studio with role-based access control
hector serve --config agents.yaml \
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

Environment variables can also be referenced in configuration files using `${VAR_NAME}` syntax.
