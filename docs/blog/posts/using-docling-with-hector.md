---
title: Using Docling with Hector for Advanced Document Parsing
description: Integrate Docling's powerful document parsing capabilities with Hector
date: 2025-11-19
tags:
  - RAG
  - Document Parsing
  - MCP
  - Docling
  - Tutorial
hide:
  - navigation
---

# Using Docling with Hector for Advanced Document Parsing

Enhance your RAG system with Docling's advanced document parsing capabilities. Parse PDFs, Word documents, PowerPoint presentations, and more with enterprise-grade accuracy.

**Time:** 10-15 minutes
**Difficulty:** Beginner

---

## What You'll Learn

- Understand Hector's MCP document parsing feature
- Set up Docling using Docker
- Configure Hector to use Docling for document parsing
- Parse complex documents (PDFs, DOCX, PPTX, XLSX, HTML)

---

## Hector's MCP Document Parsing

Hector's document stores support **MCP-based document parsing**, allowing you to use any MCP-compliant service to parse documents during indexing. This is configured via the `mcp_parsers` option in your document store configuration.

**Key benefits:**

- **Pluggable architecture** - Use any MCP service that can parse documents
- **Format flexibility** - Support formats beyond Hector's native parsers
- **Quality improvements** - Better parsing for complex layouts, tables, OCR
- **Fallback chains** - Configure multiple parsers with priority ordering

**Common use cases:**

- **Docling** - Advanced PDF/DOCX/PPTX parsing with layout detection
- **Custom parsers** - Domain-specific document processing
- **OCR services** - Scanned document text extraction
- **Audio/Video** - Transcription services via MCP

This tutorial uses **Docling** as an example, but the same pattern applies to any MCP-based parser.

---

## Why Docling?

**Docling** is a popular choice for MCP document parsing because it handles:

- **Complex layouts** - Tables, multi-column layouts, headers/footers
- **Multiple formats** - PDF, DOCX, PPTX, XLSX, HTML, and more
- **Structured extraction** - Preserves document structure and metadata
- **High accuracy** - Better than basic text extraction

**Perfect for RAG systems** where document quality directly impacts search results.

---

## Docker Setup

You can run Docling's MCP server in Docker using the `docling-serve` image, which includes the `docling-mcp-server` command.

### Step 1: Pull and Run Docling Container

**Using docling-serve (includes MCP server):**

```bash
# Pull the CPU-optimized image
docker pull ghcr.io/docling-project/docling-serve-cpu:latest

# Run the MCP server with streamable-http transport
# IMPORTANT: Mount your documents directory so Docling can access files
docker run -d \
  --name docling-mcp \
  -p 8000:8000 \
  -v "$(pwd)/test-docs:/docs:ro" \
  ghcr.io/docling-project/docling-serve-cpu:latest \
  /opt/app-root/bin/docling-mcp-server \
  --transport streamable-http \
  --host 0.0.0.0 \
  --port 8000
```

**Important:** The `-v "$(pwd)/test-docs:/docs:ro"` flag mounts your local `test-docs` directory into the container at `/docs` (read-only). When you use path mapping in Hector (`--docs-folder test-docs:/docs`), Hector will remap file paths to match the container mount point.

**Path Mapping:** Hector's path mapping feature (`local:remote` syntax) solves the Docker path mismatch problem:
1. Docker mount: `-v "$(pwd)/test-docs:/docs:ro"` (local `test-docs` → container `/docs`)
2. Hector flag: `--docs-folder test-docs:/docs` (tells Hector to remap paths to `/docs`)
3. Result: Hector sends `/docs/file.pdf` instead of `/Users/you/.../test-docs/file.pdf`

**Note:** If your documents are in a different location, adjust both the volume mount and the path mapping:

- Mount: `-v /path/to/your/documents:/docs:ro`
- Hector: `--docs-folder /path/to/your/documents:/docs`

**For GPU support:**

```bash
# Pull the GPU-enabled image
docker pull ghcr.io/docling-project/docling-serve-cu128:latest

# Run with GPU support
# IMPORTANT: Mount your documents directory
docker run -d \
  --name docling-mcp \
  --gpus all \
  -p 8000:8000 \
  -v "$(pwd)/test-docs:/docs:ro" \
  ghcr.io/docling-project/docling-serve-cu128:latest \
  /opt/app-root/bin/docling-mcp-server \
  --transport streamable-http \
  --host 0.0.0.0 \
  --port 8000
```

### Step 2: Verify Docling MCP Server is Running

Check the logs to confirm the server started:

```bash
docker logs docling-mcp

# You should see:
# INFO:     Uvicorn running on http://0.0.0.0:8000
# INFO:     StreamableHTTP session manager started
```

### Step 3: Configure Hector

**Quick Start (CLI) with Path Mapping:**

```bash
hector serve \
  --docs-folder test-docs:/docs \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document \
  --tools
```

**Key Points:**

- The `test-docs:/docs` syntax maps your local `test-docs` folder to `/docs` inside the Docker container
- This matches the volume mount: `-v "$(pwd)/test-docs:/docs:ro"`
- Hector remaps paths before sending to Docling (e.g., `/Users/you/.../test-docs/file.pdf` → `/docs/file.pdf`)

**Important:** For streamable-http transport, use the `/mcp` endpoint: `http://localhost:8000/mcp` (not just the base URL).

**Using Configuration File:**

Create `configs/docling-docker.yaml`:

```yaml
global:
  a2a_server:
    host: "0.0.0.0"
    port: 8080

llms:
  gpt-4o:
    type: "openai"
    model: "gpt-4o-mini"
    api_key: "${OPENAI_API_KEY}"
    temperature: 0.7
    max_tokens: 4000

vector_stores:
  qdrant-db:
    type: "qdrant"
    host: "localhost"
    port: 6334

embedders:
  ollama-embedder:
    type: "ollama"
    model: "nomic-embed-text"
    host: "http://localhost:11434"

tools:
  # Docling MCP tool - provides document parsing capabilities
  docling:
    type: "mcp"
    enabled: true
    internal: true  # Not visible to agents (used only for document parsing)
    server_url: "http://localhost:8000/mcp"  # Include /mcp endpoint for streamable-http transport
    description: "Docling - Advanced document parsing and conversion"

document_stores:
  knowledge_base:
    path: "./test-docs"
    source: "directory"
    # Configure MCP parsers to use Docling for document parsing
    mcp_parsers:
      tool_names:
        - "convert_document_into_docling_document"
      extensions:
        - ".pdf"
        - ".docx"
        - ".pptx"
        - ".xlsx"
        - ".html"
      priority: 8  # Higher than native parsers, so MCP is preferred
      path_prefix: "/docs"  # Remap paths for Docker container (matches -v ./test-docs:/docs)

agents:
  docling_assistant:
    name: "Docling Assistant"
    description: "Assistant with advanced document parsing via Docling"
    llm: "gpt-4o"
    vector_store: "qdrant-db"
    embedder: "ollama-embedder"
    document_stores: ["knowledge_base"]
    prompt:
      system_prompt: |
        You are a helpful assistant with access to documents parsed using Docling.
        Documents are parsed with high accuracy, preserving structure and metadata.
```

Run Hector:

```bash
hector serve --config configs/docling-docker.yaml
```

---

## Local Setup (Alternative)

For local development, you can run Docling without Docker:

```bash
# Install and run Docling MCP server
pip install docling
uvx --from docling-mcp docling-mcp-server --transport streamable-http

# Configure Hector (no path mapping needed for local setup)
hector serve \
  --docs-folder test-docs \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document \
  --tools
```

---

## Testing the Integration

### Step 1: Add Test Documents

Place some documents in your `test-docs` folder:

```bash
mkdir -p test-docs
# Add PDFs, DOCX files, etc.
cp your-document.pdf test-docs/
cp your-presentation.pptx test-docs/
```

### Step 2: Start Hector

```bash
hector serve --config configs/docling-docker.yaml
```

### Step 3: Test Document Parsing

```bash
# Chat with the agent
hector chat --config configs/docling-docker.yaml --agent docling_assistant

# Or call directly
hector call --config configs/docling-docker.yaml \
  --agent docling_assistant \
  "What information is in the documents?"
```

The agent will use Docling to parse documents and answer questions based on the extracted content.

---

## Understanding the Configuration

### MCP Parser Configuration

```yaml
document_stores:
  knowledge_base:
    mcp_parsers:
      tool_names: 
        - "convert_document_into_docling_document"
      extensions: 
        - ".pdf"
        - ".docx"
        - ".pptx"
        - ".xlsx"
        - ".html"
      priority: 8
```

**Key settings:**

- **`tool_names`**: The MCP tool to use for parsing (Docling's tool name)
- **`extensions`**: File types to parse with Docling
- **`priority`**: Higher priority means Docling is tried before native parsers

### Internal Tools

```yaml
tools:
  docling:
    type: "mcp"
    internal: true  # Hide from agents, available for document stores
```

Setting `internal: true` means:

- ✅ Available for document parsing
- ✅ Not visible to agents (keeps tool list clean)
- ✅ Used automatically by document stores

---

## Supported Document Formats

Docling supports parsing:

- **PDF** - Complex layouts, tables, multi-column
- **DOCX** - Word documents with formatting
- **PPTX** - PowerPoint presentations
- **XLSX** - Excel spreadsheets
- **HTML** - Web pages and HTML documents
- **And more** - Check Docling documentation for full list

---


---

**About Hector**: Hector is a production-grade A2A-native agent platform designed for enterprise deployments. Learn more at [gohector.dev](https://gohector.dev).

