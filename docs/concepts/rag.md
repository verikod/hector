# RAG (Retrieval-Augmented Generation)

RAG enhances agents with external knowledge by retrieving relevant context from document stores.

## Architecture

```
User Query
    ↓
Context Provider (search documents)
    ↓
Retrieved Context
    ↓
Injected into System Message
    ↓
LLM Call (with context)
    ↓
Response
```

**Components:**

- **Document Store**: Indexed documents
- **Vector Store**: Similarity search
- **Embedder**: Text → vectors
- **Context Provider**: Query → relevant docs
- **Reranker**: Improve relevance (optional)

## Document Store

Central component managing document lifecycle.

### Structure

```go
type DocumentStore struct {
    name         string
    source       DocumentSource
    vectorStore  vector.Provider
    embedder     embedder.Embedder
    chunker      Chunker
    reranker     Reranker
}
```

### Lifecycle

```
1. Ingestion
   Source → Documents → Chunks → Embeddings → Vector Store

2. Query
   User Query → Embedding → Vector Search → Top K → Rerank → Results

3. Context Injection
   Results → Format → System Message → LLM
```

## Document Sources

### Directory Source

```yaml
document_stores:
  docs:
    source:
      type: directory
      path: ./docs
      patterns:
        - "**.md"
        - "**.txt"
      exclude:
        - "**/node_modules/**"
```

**Features:**

- Recursive directory scanning
- Pattern matching
- Auto-refresh on file changes

### SQL Source

```yaml
document_stores:
  knowledge:
    source:
      type: sql
      database: main
      query: "SELECT id, title, content FROM articles WHERE published = true"
      id_column: id
      content_column: content
      metadata_columns:
        - title
        - author
```

**Features:**

- Query-based document loading
- Incremental updates
- Metadata extraction

### URL Source

```yaml
document_stores:
  web_docs:
    source:
      type: url
      urls:
        - https://docs.example.com
        - https://blog.example.com
      crawler:
        max_depth: 3
        follow_links: true
```

**Features:**

- Web crawling
- Link following
- HTML → text extraction

### S3 Source

```yaml
document_stores:
  cloud_docs:
    source:
      type: s3
      bucket: my-documents
      prefix: knowledge/
      region: us-east-1
      access_key: ${AWS_ACCESS_KEY}
      secret_key: ${AWS_SECRET_KEY}
```

**Features:**

- Cloud storage integration
- Prefix filtering
- Automatic pagination

## Chunking

Documents split into smaller chunks for embedding.

### Strategies

**Fixed Size:**

```yaml
document_stores:
  docs:
    chunking:
      strategy: fixed_size
      chunk_size: 500
      chunk_overlap: 50
```

Splits at character count with overlap.

**Recursive:**

```yaml
document_stores:
  docs:
    chunking:
      strategy: recursive
      chunk_size: 1000
      separators:
        - "\n\n"
        - "\n"
        - ". "
        - " "
```

Tries separators in order, maintaining semantic boundaries.

**Markdown:**

```yaml
document_stores:
  docs:
    chunking:
      strategy: markdown
      chunk_size: 800
```

Respects markdown structure (headers, lists, code blocks).

**Semantic:**

```yaml
document_stores:
  docs:
    chunking:
      strategy: semantic
      embedder: default
      similarity_threshold: 0.7
```

Groups semantically similar sentences.

### Chunk Structure

```go
type Chunk struct {
    ID       string
    Content  string
    Metadata map[string]any
    Position ChunkPosition
}

type ChunkPosition struct {
    DocumentID string
    Index      int
    Start      int
    End        int
}
```

**Metadata inherited from document:**

- File path
- Title
- Author
- Timestamps
- Custom fields

## Vector Stores

Store and search document embeddings.

### Providers

**Chromem (Embedded):**

```yaml
vector_stores:
  local:
    type: chromem
    persist_path: .hector/vectors
```

File-based, embedded. No server required.

**Qdrant:**

```yaml
vector_stores:
  qdrant:
    type: qdrant
    url: http://localhost:6333
    collection: documents
```

Production vector database with persistence.

**Pinecone:**

```yaml
vector_stores:
  pinecone:
    type: pinecone
    api_key: ${PINECONE_API_KEY}
    environment: us-east1-gcp
    index_name: knowledge
```

Managed cloud service.

**Weaviate:**

```yaml
vector_stores:
  weaviate:
    type: weaviate
    url: http://localhost:8080
    class_name: Documents
```

Open-source with hybrid search.

**Milvus:**

```yaml
vector_stores:
  milvus:
    type: milvus
    host: localhost
    port: 19530
    collection: docs
```

Distributed vector database.

### Operations

**Index:**

```go
err := vectorStore.Index(ctx, []vector.Document{
    {
        ID:        "doc-1",
        Vector:    embedding,
        Content:   "document text",
        Metadata:  metadata,
    },
})
```

**Search:**

```go
results, err := vectorStore.Search(ctx, &vector.SearchRequest{
    Vector:    queryEmbedding,
    TopK:      10,
    Filter:    metadata_filter,
})
```

## Embedders

Convert text to vector embeddings.

### Providers

**OpenAI:**

```yaml
embedders:
  openai:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    dimensions: 1536
```

**Models:**

- `text-embedding-3-small`: 1536 dims, fast
- `text-embedding-3-large`: 3072 dims, accurate

**Ollama:**

```yaml
embedders:
  ollama:
    provider: ollama
    model: nomic-embed-text
    base_url: http://localhost:11434
```

Local embedding models.

**Cohere:**

```yaml
embedders:
  cohere:
    provider: cohere
    model: embed-english-v3.0
    api_key: ${COHERE_API_KEY}
```

Multilingual embeddings.

### Embedding Process

```go
// 1. Text → Embedder
embedding, err := embedder.Embed(ctx, "document text")

// 2. Vector (float64 slice)
// [0.123, -0.456, 0.789, ...]

// 3. Store in vector database
vectorStore.Index(ctx, Document{
    ID:     "doc-1",
    Vector: embedding,
})
```

## Search

### Vector Search

```yaml
document_stores:
  docs:
    search:
      top_k: 5
      score_threshold: 0.7
```

**Flow:**
1. Query → Embedding
2. Vector similarity search
3. Top K results
4. Filter by score threshold

### Hybrid Search

```yaml
document_stores:
  docs:
    search:
      hybrid:
        enabled: true
        keyword_weight: 0.3
        semantic_weight: 0.7
```

Combines keyword matching + semantic similarity.

### Metadata Filtering

```yaml
document_stores:
  docs:
    search:
      top_k: 5
      metadata_filter:
        category: "technical"
        published: true
```

Pre-filter documents before vector search.

## Reranking

Improves relevance of search results.

### Cross-Encoder Reranking

```yaml
document_stores:
  docs:
    reranking:
      enabled: true
      model: cross-encoder/ms-marco-MiniLM-L-6-v2
      top_n: 3
```

**Process:**
1. Vector search returns top_k (e.g., 10)
2. Reranker scores query-document pairs
3. Return top_n (e.g., 3) by reranker score

### LLM-based Reranking

```yaml
document_stores:
  docs:
    reranking:
      enabled: true
      llm: gpt-4o-mini
      prompt: "Rate relevance of document to query"
      top_n: 3
```

LLM evaluates relevance.

## Context Provider

Bridges RAG with agents.

### Implementation

```go
type ContextProvider func(ctx agent.ReadonlyContext, query string) (string, error)
```

**Flow:**

```
1. Agent invocation
2. Extract user query
3. Call context provider
   contextProvider(ctx, query)
4. Search document store
5. Format results
6. Return context string
7. Inject into system message
```

### Usage in Agent

```yaml
agents:
  assistant:
    llm: gpt-4o
    document_stores:
      - knowledge_base
```

Agent automatically searches document stores for relevant context.

### Context Formatting

Results formatted for LLM:

```
Relevant context from knowledge base:

Document 1: Getting Started Guide
Content: Hector is an agent framework...
Source: docs/getting-started.md

Document 2: Configuration Reference
Content: Configure agents in YAML...
Source: docs/reference/config.md
```

## MCP Document Parsing

Use MCP tools for advanced document processing.

### Configuration

```yaml
document_stores:
  complex_docs:
    source:
      type: directory
      path: ./documents
    mcp_parser:
      enabled: true
      server: docling_mcp
      tool: parse_document
```

### Supported Formats

**With MCP (via Docling):**

- PDF (multi-column, tables, images)
- DOCX (Word documents)
- PPTX (PowerPoint)
- HTML (structured extraction)

**Built-in:**

- Plain text
- Markdown
- JSON
- CSV

### MCP Parser Flow

```
1. Document detected (e.g., PDF)
2. Call MCP tool: parse_document(file_path)
3. MCP returns structured content
4. Extract text, tables, metadata
5. Chunk and embed
6. Index in vector store
```

## Ingestion Pipeline

### Full Pipeline

```
Document Source
    ↓
Document Loading
    ↓
MCP Parsing (optional)
    ↓
Chunking
    ↓
Embedding Generation
    ↓
Vector Indexing
    ↓
Metadata Storage
```

### Incremental Updates

**Directory watching:**

```go
// Auto-detect file changes
watcher.OnChange(func(file string) {
    // Re-index changed document
    store.IndexDocument(file)
})
```

**SQL polling:**

```sql
SELECT * FROM documents
WHERE updated_at > last_sync_time
```

Only index new/updated documents.

### Batch Processing

```go
// Batch embed for efficiency
chunks := []Chunk{...}

embeddings, err := embedder.EmbedBatch(ctx, chunks)

vectorStore.IndexBatch(ctx, embeddings)
```

Reduces API calls and improves throughput.

## Performance

### Embedding Caching

```go
type CachedEmbedder struct {
    embedder embedder.Embedder
    cache    *lru.Cache
}

func (e *CachedEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
    if cached, ok := e.cache.Get(text); ok {
        return cached.([]float64), nil
    }

    embedding, err := e.embedder.Embed(ctx, text)
    e.cache.Add(text, embedding)
    return embedding, err
}
```

Avoids redundant embedding calls.

### Vector Index Optimization

**HNSW parameters (Qdrant, Weaviate):**

```yaml
vector_stores:
  qdrant:
    type: qdrant
    hnsw_config:
      m: 16                # Number of connections
      ef_construct: 100    # Build-time search depth
```

Balance accuracy vs. speed.

### Chunk Size Tuning

```yaml
# Smaller chunks
chunking:
  chunk_size: 300
  # + More precise retrieval
  # - More chunks to index

# Larger chunks
chunking:
  chunk_size: 1000
  # + Fewer chunks
  # - Less precise retrieval
```

Test with your data to find optimal size.

## Best Practices

### Chunk Overlap

Use overlap to avoid context loss:

```yaml
chunking:
  chunk_size: 500
  chunk_overlap: 50   # 10% overlap
```

### Metadata Enrichment

Add useful metadata:

```go
metadata := map[string]any{
    "source":     filepath,
    "timestamp":  modTime,
    "category":   detectCategory(content),
    "author":     extractAuthor(content),
}
```

Enables better filtering and attribution.

### Query Expansion

Expand user queries for better recall:

```go
func expandQuery(query string) []string {
    return []string{
        query,
        addSynonyms(query),
        reformulate(query),
    }
}
```

Search with multiple query variations.

### Result Diversity

Avoid duplicate content:

```go
func deduplicateResults(results []SearchResult) []SearchResult {
    seen := make(map[string]bool)
    var unique []SearchResult

    for _, r := range results {
        key := r.DocumentID
        if !seen[key] {
            seen[key] = true
            unique = append(unique, r)
        }
    }

    return unique
}
```

## Next Steps

- [Configuration](configuration.md) - RAG configuration details
- [Agents](agents.md) - Integrating RAG with agents
- [Tools](tools.md) - MCP tools for document parsing
