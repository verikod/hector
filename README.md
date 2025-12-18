```
██╗  ██╗███████╗ ██████╗████████╗ ██████╗ ██████╗ 
██║  ██║██╔════╝██╔════╝╚══██╔══╝██╔═══██╗██╔══██╗
███████║█████╗  ██║        ██║   ██║   ██║██████╔╝
██╔══██║██╔══╝  ██║        ██║   ██║   ██║██╔══██╗
██║  ██║███████╗╚██████╗   ██║   ╚██████╔╝██║  ██║
╚═╝  ╚═╝╚══════╝ ╚═════╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝
```
[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![A2A Protocol](https://img.shields.io/badge/A2A%20v0.3.0-100%25%20compliant-brightgreen.svg)](https://a2a-protocol.org/latest/community/#a2a-integrations)
[![Documentation](https://img.shields.io/badge/docs-gohector.dev-blue.svg)](https://gohector.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/verikod/hector)](https://goreportcard.com/report/github.com/verikod/hector)

**Config-first A2A-Native Agent Platform**

Deploy observable, secure, and scalable AI agents with zero-config or YAML, plus a programmatic API.

**[Documentation](https://gohector.dev)** | [Quick Start](https://gohector.dev/getting-started/quick-start/) | [Configuration](https://gohector.dev/guides/configuration/)

---

## Hector Studio (Desktop)

The native desktop GUI for Hector. Manage your agents and workspaces with a rich visual interface.

- **Project**: [verikod/hector-studio](https://github.com/verikod/hector-studio)
- **Download**: [Latest Releases](https://github.com/verikod/hector-studio/releases) (macOS, Windows, Linux)

---

## Quick Start (zero-config)

```bash
go install github.com/verikod/hector/cmd/hector@latest
export OPENAI_API_KEY="sk-..."

hector serve --model gpt-4o --tools --studio
```

RAG in one command (with MCP parsing optional):
```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document
```

## Quick Start (config file)

```bash
cat > config.yaml <<'EOF'
version: "2"
llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
agents:
  assistant:
    llm: default
    tools: [search]
server:
  port: 8080
EOF

hector serve --config config.yaml --studio
```

## Highlights
- **Config-first & zero-config**: YAML for repeatability; flags for fast starts. JSON Schema available via `hector schema`.
- **Programmatic API**: Build agents in Go (`pkg/api.go`), including sub-agents and agent-as-tool patterns.
- **RAG**: Folder-based document stores, embedded vector search (chromem), native PDF/DOCX/XLSX parsers, optional MCP parsing (Docling).
- **Vector DBs**: Embedded chromem (default), or external (Qdrant, Pinecone, Weaviate, Milvus, Chroma).
- **Persistence**: Tasks and sessions can use in-memory or SQL backends (sqlite/postgres/mysql via DSN).
- **Observability**: Metrics endpoint and OTLP tracing options.
- **Checkpointing**: Optional checkpoint/recovery strategies.
- **Auth**: JWT/JWKS support at the server layer.
- **Guardrails**: Input validation, prompt injection detection, PII redaction, and tool authorization.
- **A2A-native**: Uses a2a-go types and JSON-RPC/gRPC endpoints.

## Documentation

- [Getting Started](https://gohector.dev/getting-started/quick-start/)
- [Configuration Guide](https://gohector.dev/guides/configuration/)
- [Guardrails Guide](https://gohector.dev/guides/guardrails/)
- [RAG Guide](https://gohector.dev/guides/rag/)
- [Tools Guide](https://gohector.dev/guides/tools/)
- [Core Concepts](https://gohector.dev/concepts/architecture/)
- [Blog & Tutorials](https://gohector.dev/blog/)

## License

AGPL-3.0 (see [LICENSE](LICENSE)).

