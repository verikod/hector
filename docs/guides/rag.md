# RAG (Retrieval-Augmented Generation)

RAG enhances agents with document search capabilities, enabling knowledge retrieval from your data sources.

## Quick Start

### Basic RAG

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --tools
```

This automatically:

- Creates embedded vector database (chromem)
- Configures embedder (auto-detected from LLM provider, or OpenAI/Ollama fallback)
- Indexes documents (including PDF, DOCX, XLSX via native parsers)
- Adds search tool
- Watches for file changes

### With Auto-Context

For automatic context injection (no need for agents to call the search tool):

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --include-context
```

When `--include-context` is enabled:
- Relevant documents are automatically retrieved based on user queries
- Context is injected into the system prompt before LLM calls
- The agent doesn't need to explicitly call the `search` tool

### Advanced Options

**With external vector database:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --vector-type qdrant \
  --vector-host localhost:6333 \
  --tools
```

**With custom embedder:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --embedder-provider ollama \
  --embedder-url http://localhost:11434 \
  --embedder-model nomic-embed-text \
  --tools
```

**With Docling for advanced document parsing:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document \
  --tools
```

**With Docker path mapping (for containerized MCP services):**

```bash
# Syntax: --docs-folder local_path:remote_path
hector serve \
  --model gpt-4o \
  --docs-folder ./documents:/docs \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document \
  --tools
```

The `local:remote` syntax maps local paths to container paths when using Docker-based MCP parsers like Docling.

### Config File RAG

```yaml
version: "2"

vector_stores:
  local:
    type: chromem
    persist_path: .hector/vectors
    compress: true

embedders:
  default:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}

document_stores:
  docs:
    source:
      type: directory
      path: ./documents
    vector_store: local
    embedder: default
    watch: true

agents:
  assistant:
    llm: default
    document_stores: [docs]
    tools: [search]
```

## Vector Stores

### Chromem (Embedded)

No external dependencies, persists to disk:

```yaml
vector_stores:
  local:
    type: chromem
    persist_path: .hector/vectors
    compress: true  # Gzip compression
```

### Qdrant

External vector database:

```yaml
vector_stores:
  qdrant:
    type: qdrant
    host: localhost
    port: 6333
    api_key: ${QDRANT_API_KEY}
    enable_tls: true
    collection: hector_docs
```

### Pinecone

Cloud vector database:

```yaml
vector_stores:
  pinecone:
    type: pinecone
    api_key: ${PINECONE_API_KEY}
    environment: us-east-1-aws
    index_name: hector-docs
```

### Weaviate

```yaml
vector_stores:
  weaviate:
    type: weaviate
    host: localhost
    port: 8080
    api_key: ${WEAVIATE_API_KEY}
```

### Milvus

```yaml
vector_stores:
  milvus:
    type: milvus
    host: localhost
    port: 19530
```

## Embedders

### OpenAI

```yaml
embedders:
  openai:
    provider: openai
    model: text-embedding-3-small  # or text-embedding-3-large
    api_key: ${OPENAI_API_KEY}
```

### Ollama (Local)

```yaml
embedders:
  ollama:
    provider: ollama
    model: nomic-embed-text  # or mxbai-embed-large
    base_url: http://localhost:11434
```

### Cohere

```yaml
embedders:
  cohere:
    provider: cohere
    model: embed-english-v3.0
    api_key: ${COHERE_API_KEY}
```

## Document Sources

### Directory Source

Index files from a folder:

```yaml
document_stores:
  docs:
    source:
      type: directory
      path: ./documents
      include:
        - "*.md"
        - "*.txt"
        - "*.pdf"
      exclude:
        - .git
        - node_modules
        - "*.tmp"
      max_file_size: 10485760  # 10MB
```

Default excludes: `.*`, `node_modules`, `__pycache__`, `vendor`, `.git`

### SQL Source

Index data from database:

```yaml
document_stores:
  knowledge_base:
    source:
      type: sql
      sql:
        database: main  # References databases config
        query: SELECT id, title, content FROM articles
        id_column: id
        content_columns: [title, content]
        metadata_columns: [category, author]
```

### API Source

Fetch documents from API:

```yaml
document_stores:
  external_docs:
    source:
      type: api
      api:
        url: https://api.example.com/documents
        method: GET
        headers:
          Authorization: Bearer ${API_TOKEN}
        response_path: documents
        id_field: id
        content_fields: [title, body]
        metadata_fields: [category, tags]
```

### Collection Source

Use existing vector collection:

```yaml
document_stores:
  existing:
    source:
      type: collection
      collection: pre_populated_collection
    vector_store: qdrant
    embedder: default
```

## Chunking Strategies

### Simple Chunking

Fixed-size chunks with overlap:

```yaml
document_stores:
  docs:
    chunking:
      strategy: simple
      size: 1000      # Characters per chunk
      overlap: 200    # Overlap between chunks
```

Best for: General text, documentation

### Semantic Chunking

Chunk by semantic boundaries:

```yaml
document_stores:
  docs:
    chunking:
      strategy: semantic
      size: 1000
      overlap: 100
```

Best for: Natural language content

### Sentence Chunking

Chunk by sentences:

```yaml
document_stores:
  docs:
    chunking:
      strategy: sentence
      sentences_per_chunk: 5
```

Best for: Precise retrieval, Q&A

## Search Configuration

### Basic Search

```yaml
document_stores:
  docs:
    search:
      top_k: 10           # Return top 10 results
      threshold: 0.5      # Minimum similarity score
```

### Advanced Search

```yaml
document_stores:
  docs:
    search:
      top_k: 10
      threshold: 0.5
      rerank: true        # Enable reranking
      rerank_top_k: 3     # Return top 3 after reranking
```

Reranking improves relevance by rescoring initial results.

## Watch Mode

Auto-reindex on file changes:

```yaml
document_stores:
  docs:
    watch: true                  # Enable file watching
    incremental_indexing: true   # Only reindex changed files
```

When files change:

- Added files: indexed immediately
- Modified files: re-indexed
- Deleted files: removed from index

## Indexing Configuration

Control indexing behavior:

```yaml
document_stores:
  docs:
    indexing:
      max_concurrent: 8        # Parallel workers
      retry:
        max_retries: 3         # Retry failed documents
        base_delay: 1s         # Delay between retries
        max_delay: 30s
```

## Index Management

### Clearing the Index

To clear and rebuild the index, delete the vector store data:

**For chromem (embedded):**

```bash
rm -rf .hector/vectors
hector serve --config config.yaml  # Reindexes on startup
```

**For external vector stores (Qdrant, Pinecone, etc.):**

Delete the collection via the vector store's API or UI, then restart Hector.

### Forcing Reindex

To force reindexing without deleting data, restart with `--force-reindex`:

```bash
hector serve --config config.yaml --force-reindex
```

Or modify any file in the source directory (triggers incremental reindex if `watch: true`).

### Index Status

Check indexing status via logs:

```
INFO  Indexing started: 150 documents
INFO  Indexing progress: 75/150 (50%)
INFO  Indexing complete: 148 indexed, 2 failed
```

Failed documents are logged with errors for debugging.



## Document Parsing

Hector supports multiple document parsers with automatic fallback:

| Parser | Priority | Formats | When Used |
|--------|----------|---------|-----------|
| **MCP Parser** (e.g., Docling) | 8 | PDF, DOCX, PPTX, XLSX, HTML | When `--mcp-parser-tool` configured |
| **Native Parsers** | 5 | PDF, DOCX, XLSX | Built-in, always available |
| **Text Extractor** | 1 | Plain text, code files | Fallback for text-based files |

### Native Document Parsers

Hector includes built-in parsers for common document formats:

**Supported formats:**

- **PDF** - Text extraction with page markers
- **DOCX** - Word document content extraction
- **XLSX** - Excel spreadsheet with cell references (max 1000 cells/sheet)

Native parsers work automatically for ~70% of documents. For complex layouts, tables, or scanned documents, use MCP parsers like Docling.

**Default include patterns:**

- Text/code: `*.md`, `*.txt`, `*.rst`, `*.go`, `*.py`, `*.js`, `*.ts`, `*.json`, `*.yaml`, etc.
- Binary documents: `*.pdf`, `*.docx`, `*.xlsx`

### MCP Document Parsing

For advanced parsing (OCR, complex layouts, tables), use MCP tools like Docling:

```yaml
tools:
  docling:
    type: mcp
    url: http://localhost:8000/mcp
    transport: streamable-http

document_stores:
  docs:
    source:
      type: directory
      path: ./documents
    mcp_parsers:
      tool_names:
        - convert_document_into_docling_document
      extensions:
        - .pdf
        - .docx
        - .pptx
        - .xlsx
      priority: 8  # Higher than native (5)
      path_prefix: "/docs"  # For Docker path mapping
```

**With Docling:**

```bash
hector serve \
  --model gpt-4o \
  --docs-folder ./documents \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document
```

**With Docker path mapping:**

When Docling runs in Docker, use path mapping to remap local paths:

```bash
# Docker mount: -v $(pwd)/documents:/docs:ro
hector serve \
  --model gpt-4o \
  --docs-folder ./documents:/docs \
  --mcp-url http://localhost:8000/mcp \
  --mcp-parser-tool convert_document_into_docling_document
```

The `local:remote` syntax ensures paths are correctly translated for the container.

See [Using Docling with Hector](../blog/posts/using-docling-with-hector.md) for a complete tutorial.

## Agent Integration

### Manual Search

Agent calls search tool explicitly:

```yaml
agents:
  assistant:
    llm: default
    document_stores: [docs]  # Access to docs store
    tools: [search]          # Search tool
    instruction: |
      Use the search tool to find relevant information.
      Always cite sources in your responses.
```

### Auto-Injected Context

Automatically inject relevant context:

```yaml
agents:
  assistant:
    llm: default
    document_stores: [docs]
    include_context: true            # Auto-inject
    include_context_limit: 5         # Max 5 documents
    include_context_max_length: 500  # Max 500 chars per doc
```

When enabled:

- User message triggers search
- Top K documents retrieved
- Injected into system prompt
- Agent receives context automatically

### Scoped Access

Limit document store access per agent:

```yaml
document_stores:
  internal_docs:
    source:
      type: directory
      path: ./internal

  public_docs:
    source:
      type: directory
      path: ./public

agents:
  # Public agent: public docs only
  public_assistant:
    document_stores: [public_docs]

  # Internal agent: all docs
  internal_assistant:
    document_stores: [internal_docs, public_docs]

  # No RAG access
  restricted:
    document_stores: []
```

## Multi-Store Configuration

Configure multiple document stores:

```yaml
vector_stores:
  chromem:
    type: chromem
    persist_path: .hector/vectors

embedders:
  default:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}

document_stores:
  codebase:
    source:
      type: directory
      path: ./src
      include: ["*.go", "*.ts", "*.py"]
    chunking:
      strategy: simple
      size: 1500
    vector_store: chromem
    embedder: default

  documentation:
    source:
      type: directory
      path: ./docs
      include: ["*.md"]
    chunking:
      strategy: simple
      size: 1000
    vector_store: chromem
    embedder: default

  knowledge_base:
    source:
      type: sql
      sql:
        database: main
        query: SELECT * FROM articles
        id_column: id
        content_columns: [title, content]
    vector_store: chromem
    embedder: default

agents:
  assistant:
    document_stores: [codebase, documentation, knowledge_base]
    tools: [search]
```

## Examples

### Documentation Assistant

```yaml
vector_stores:
  local:
    type: chromem
    persist_path: .hector/vectors

embedders:
  default:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}

document_stores:
  docs:
    source:
      type: directory
      path: ./documentation
      include: ["*.md", "*.txt"]
    chunking:
      strategy: simple
      size: 1000
      overlap: 200
    vector_store: local
    embedder: default
    watch: true
    search:
      top_k: 5
      threshold: 0.6

agents:
  docs_assistant:
    llm: default
    document_stores: [docs]
    tools: [search]
    instruction: |
      You help users find information in documentation.
      Always search before answering questions.
      Cite document sources in responses.
```

### Code Search Agent

```yaml
document_stores:
  codebase:
    source:
      type: directory
      path: ./src
      include:
        - "*.go"
        - "*.ts"
        - "*.py"
        - "*.java"
    chunking:
      strategy: simple
      size: 1500  # Larger chunks for code
      overlap: 300
    vector_store: local
    embedder: default
    watch: true

agents:
  code_assistant:
    llm: default
    document_stores: [codebase]
    tools: [search, text_editor]
    instruction: |
      You help developers understand the codebase.
      Use search to find relevant code.
      Use text_editor view to view complete files.
```

### Multi-Source RAG

```yaml
document_stores:
  docs:
    source:
      type: directory
      path: ./docs

  database:
    source:
      type: sql
      sql:
        database: main
        query: SELECT id, title, content FROM kb_articles
        id_column: id
        content_columns: [title, content]

  api:
    source:
      type: api
      api:
        url: https://api.example.com/docs
        response_path: data
        id_field: id
        content_fields: [content]

agents:
  research_assistant:
    document_stores: [docs, database, api]
    tools: [search]
    instruction: |
      Search across all available knowledge sources.
      Synthesize information from multiple sources.
```

## Performance Optimization

### Embedding Cache

Reuse embeddings for unchanged documents:

```yaml
document_stores:
  docs:
    incremental_indexing: true  # Only reindex changed files
```

### Parallel Indexing

Index multiple files concurrently:

```yaml
document_stores:
  docs:
    indexing:
      max_concurrent: 16  # Increase for faster indexing
```

### Collection Persistence

Use persistent vector stores:

```yaml
vector_stores:
  qdrant:
    type: qdrant
    host: localhost
    port: 6333
    collection: my_docs  # Persistent collection
```

## Best Practices

### Chunk Size

Choose appropriate chunk sizes:

```yaml
# Small chunks (500-800): Precise retrieval, Q&A
chunking:
  size: 500

# Medium chunks (1000-1500): General purpose
chunking:
  size: 1000

# Large chunks (2000-3000): Context-rich, code
chunking:
  size: 2000
```

### Overlap

Use overlap to preserve context across chunks:

```yaml
chunking:
  size: 1000
  overlap: 200  # 20% overlap
```

### File Filters

Exclude non-content files:

```yaml
source:
  exclude:
    - .git
    - node_modules
    - __pycache__
    - "*.log"
    - "*.tmp"
```

### Search Threshold

Balance precision vs recall:

```yaml
# High precision, may miss some results
search:
  threshold: 0.7

# Balanced
search:
  threshold: 0.5

# High recall, may include irrelevant results  
search:
  threshold: 0.3
```

