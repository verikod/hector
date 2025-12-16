// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel
//
// Licensed under the GNU Affero General Public License v3.0 (AGPL-3.0) (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.gnu.org/licenses/agpl-3.0.en.html
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rag

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/verikod/hector/pkg/embedder"
	"github.com/verikod/hector/pkg/vector"
)

// Query validation constants (from legacy).
const (
	// MinQueryLength is the minimum allowed query length.
	MinQueryLength = 2
	// MaxQueryLength is the maximum allowed query length.
	MaxQueryLength = 10000
)

// SearchEngine provides document indexing and semantic search.
//
// It combines:
//   - Document ingestion with chunking
//   - Vector similarity search
//   - Optional hybrid search (vector + keyword)
//   - Optional query enhancement (HyDE, multi-query)
//   - Optional reranking
//
// Derived from legacy pkg/context/search.go:SearchEngine
type SearchEngine struct {
	provider   vector.Provider
	embedder   embedder.Embedder
	chunker    Chunker
	config     SearchEngineConfig
	collection string

	// Optional enhancement components
	hyde       *HyDE
	reranker   *Reranker
	multiQuery *MultiQueryExpander

	mu sync.RWMutex
}

// SearchEngineConfig configures the search engine.
type SearchEngineConfig struct {
	// Provider for vector storage and search (required).
	Provider vector.Provider

	// Embedder for generating embeddings (required).
	Embedder embedder.Embedder

	// Chunker for splitting documents (optional, defaults to simple).
	Chunker Chunker

	// Collection name for storing documents (optional, defaults to "rag_documents").
	Collection string

	// DefaultTopK is the default number of results (default: 10).
	DefaultTopK int

	// DefaultThreshold filters results below this score (default: 0.0).
	DefaultThreshold float32

	// HyDE for hypothetical document embedding (optional).
	HyDE *HyDE

	// Reranker for LLM-based result reranking (optional).
	Reranker *Reranker

	// MultiQuery for query expansion (optional).
	MultiQuery *MultiQueryExpander
}

// NewSearchEngine creates a new search engine.
func NewSearchEngine(cfg SearchEngineConfig) (*SearchEngine, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("vector provider is required")
	}
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("embedder is required")
	}

	// Set defaults
	chunker := cfg.Chunker
	if chunker == nil {
		chunker = NewSimpleChunker(DefaultChunkerConfig())
	}

	collection := cfg.Collection
	if collection == "" {
		collection = "rag_documents"
	}

	if cfg.DefaultTopK <= 0 {
		cfg.DefaultTopK = 10
	}

	slog.Info("Created RAG search engine",
		"provider", cfg.Provider.Name(),
		"collection", collection,
		"chunker", chunker.Strategy(),
		"hyde_enabled", cfg.HyDE != nil,
		"reranker_enabled", cfg.Reranker != nil,
		"multiquery_enabled", cfg.MultiQuery != nil)

	return &SearchEngine{
		provider:   cfg.Provider,
		embedder:   cfg.Embedder,
		chunker:    chunker,
		config:     cfg,
		collection: collection,
		hyde:       cfg.HyDE,
		reranker:   cfg.Reranker,
		multiQuery: cfg.MultiQuery,
	}, nil
}

// IngestDocument indexes a document for search.
//
// The document is:
//  1. Split into chunks using the configured chunker
//  2. Each chunk is embedded
//  3. Chunks are stored in the vector database
//
// Document ID should be stable across re-indexing to enable updates.
func (e *SearchEngine) IngestDocument(ctx context.Context, doc Document) error {
	if doc.ID == "" {
		return fmt.Errorf("document ID is required")
	}
	if doc.Content == "" {
		return nil // Skip empty documents
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Create chunk context from document metadata
	chunkCtx := &ChunkContext{
		FilePath: doc.SourcePath,
	}
	if lang, ok := doc.Metadata["language"].(string); ok {
		chunkCtx.Language = lang
	}

	// Split document into chunks
	chunks, err := e.chunker.Chunk(doc.Content, chunkCtx)
	if err != nil {
		return fmt.Errorf("failed to chunk document: %w", err)
	}

	if len(chunks) == 0 {
		return nil
	}

	// Index each chunk
	indexed := 0
	for _, chunk := range chunks {
		// Generate chunk ID
		chunkID := fmt.Sprintf("%s:chunk:%d", doc.ID, chunk.Index)

		// Generate embedding
		embedding, err := e.embedder.Embed(ctx, chunk.Content)
		if err != nil {
			slog.Warn("Failed to embed chunk",
				"document_id", doc.ID,
				"chunk_index", chunk.Index,
				"error", err)
			continue
		}

		// Prepare metadata
		metadata := make(map[string]any)
		for k, v := range doc.Metadata {
			metadata[k] = v
		}
		metadata["document_id"] = doc.ID
		metadata["chunk_index"] = chunk.Index
		metadata["chunk_total"] = chunk.Total
		metadata["start_line"] = chunk.StartLine
		metadata["end_line"] = chunk.EndLine
		metadata["content"] = chunk.Content
		if doc.Title != "" {
			metadata["title"] = doc.Title
		}
		if doc.SourcePath != "" {
			metadata["source_path"] = doc.SourcePath
		}
		if chunk.Context != nil {
			if chunk.Context.FunctionName != "" {
				metadata["function_name"] = chunk.Context.FunctionName
			}
			if chunk.Context.TypeName != "" {
				metadata["type_name"] = chunk.Context.TypeName
			}
		}

		// Upsert to vector store
		if err := e.provider.Upsert(ctx, e.collection, chunkID, embedding, metadata); err != nil {
			slog.Warn("Failed to upsert chunk",
				"document_id", doc.ID,
				"chunk_index", chunk.Index,
				"error", err)
			continue
		}

		indexed++
	}

	slog.Debug("Indexed document",
		"document_id", doc.ID,
		"chunks_total", len(chunks),
		"chunks_indexed", indexed)

	return nil
}

// IngestDocuments indexes multiple documents concurrently.
func (e *SearchEngine) IngestDocuments(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	// Use worker pool for concurrent indexing
	numWorkers := runtime.NumCPU()
	if numWorkers > len(docs) {
		numWorkers = len(docs)
	}

	docChan := make(chan Document, len(docs))
	errChan := make(chan error, len(docs))

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for doc := range docChan {
				if err := e.IngestDocument(ctx, doc); err != nil {
					errChan <- fmt.Errorf("failed to index %s: %w", doc.ID, err)
				}
			}
		}()
	}

	// Send documents to workers
	for _, doc := range docs {
		docChan <- doc
	}
	close(docChan)

	wg.Wait()
	close(errChan)

	// Collect errors
	var errs []string
	for err := range errChan {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("indexing errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

// Search finds documents matching the query.
func (e *SearchEngine) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	startTime := time.Now()

	req.SetDefaults()
	if req.TopK <= 0 {
		req.TopK = e.config.DefaultTopK
	}
	if req.Threshold <= 0 {
		req.Threshold = e.config.DefaultThreshold
	}

	// Process and validate query (from legacy processQuery + validateQuery)
	query := e.processQuery(req.Query)
	if err := e.validateQuery(query); err != nil {
		return &SearchResponse{Results: []SearchResult{}}, err
	}
	req.Query = query

	collection := req.Collection
	if collection == "" {
		collection = e.collection
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check if we should use multi-query expansion
	var queryExpansions []string
	if e.multiQuery != nil && req.Options != nil && req.Options.EnableMultiQuery {
		queries, err := e.multiQuery.ExpandQuery(ctx, req.Query)
		if err != nil {
			slog.Warn("Multi-query expansion failed", "error", err)
			queries = []string{req.Query}
		}
		queryExpansions = queries
	} else {
		queryExpansions = []string{req.Query}
	}

	// Search with each query variant
	var allResultSets [][]SearchResult
	for _, query := range queryExpansions {
		results, err := e.searchSingle(ctx, query, collection, req)
		if err != nil {
			slog.Warn("Search failed for query variant", "query", query, "error", err)
			continue
		}
		allResultSets = append(allResultSets, results)
	}

	// Combine results from all queries
	searchResults := CombineResults(allResultSets)

	// Apply reranking if enabled
	if e.reranker != nil && req.Options != nil && req.Options.EnableRerank && len(searchResults) > 0 {
		reranked, err := e.reranker.Rerank(ctx, req.Query, searchResults)
		if err != nil {
			slog.Warn("Reranking failed", "error", err)
		} else {
			searchResults = reranked.Results
		}
	}

	// Apply top-k limit after reranking
	if len(searchResults) > req.TopK {
		searchResults = searchResults[:req.TopK]
	}

	elapsed := time.Since(startTime)

	return &SearchResponse{
		Results:         searchResults,
		TotalMatches:    len(searchResults),
		SearchTimeMs:    elapsed.Milliseconds(),
		QueryExpansions: queryExpansions,
	}, nil
}

// searchSingle performs a single search query.
func (e *SearchEngine) searchSingle(ctx context.Context, query, collection string, req SearchRequest) ([]SearchResult, error) {
	// Determine what to embed (query or hypothetical doc)
	textToEmbed := query
	if e.hyde != nil && req.Options != nil && req.Options.EnableHyDE {
		hypothetical, err := e.hyde.GenerateHypotheticalDocument(ctx, query)
		if err != nil {
			slog.Warn("HyDE generation failed, using original query", "error", err)
		} else {
			textToEmbed = hypothetical
		}
	}

	// Generate embedding
	queryEmbedding, err := e.embedder.Embed(ctx, textToEmbed)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Search vector store (get more than topK for reranking)
	fetchK := req.TopK
	if e.reranker != nil && req.Options != nil && req.Options.EnableRerank {
		fetchK = req.TopK * 3 // Fetch more for reranking
		if fetchK > 100 {
			fetchK = 100
		}
	}

	var results []vector.Result
	if len(req.Filter) > 0 {
		results, err = e.provider.SearchWithFilter(ctx, collection, queryEmbedding, fetchK, req.Filter)
	} else {
		results, err = e.provider.Search(ctx, collection, queryEmbedding, fetchK)
	}
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert results
	searchResults := make([]SearchResult, 0, len(results))
	filteredCount := 0
	for _, r := range results {
		// Apply threshold filter
		if req.Threshold > 0 && r.Score < req.Threshold {
			filteredCount++
			continue
		}

		// Extract content from metadata
		content := r.Content
		if content == "" {
			if c, ok := r.Metadata["content"].(string); ok {
				content = c
			}
		}

		// Extract document ID and chunk index
		docID := ""
		if did, ok := r.Metadata["document_id"].(string); ok {
			docID = did
		}

		chunkIndex := 0
		if ci, ok := r.Metadata["chunk_index"].(int); ok {
			chunkIndex = ci
		} else if ci, ok := r.Metadata["chunk_index"].(float64); ok {
			chunkIndex = int(ci)
		}

		searchResults = append(searchResults, SearchResult{
			ID:         r.ID,
			Content:    content,
			Score:      r.Score,
			DocumentID: docID,
			ChunkIndex: chunkIndex,
			Metadata:   r.Metadata,
		})
	}

	// Log search results summary with scores
	if len(searchResults) > 0 {
		minScore := searchResults[len(searchResults)-1].Score
		maxScore := searchResults[0].Score
		slog.Debug("Search results",
			"query", req.Query,
			"returned", len(searchResults),
			"filtered_by_threshold", filteredCount,
			"threshold", req.Threshold,
			"score_range", fmt.Sprintf("%.3f-%.3f", minScore, maxScore))
	} else if filteredCount > 0 {
		slog.Debug("All results filtered by threshold",
			"query", req.Query,
			"filtered", filteredCount,
			"threshold", req.Threshold)
	}

	return searchResults, nil
}

// DeleteDocument removes a document and all its chunks from the index.
func (e *SearchEngine) DeleteDocument(ctx context.Context, documentID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Delete by document_id filter
	filter := map[string]any{
		"document_id": documentID,
	}

	if err := e.provider.DeleteByFilter(ctx, e.collection, filter); err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}

	slog.Debug("Deleted document from index", "document_id", documentID)
	return nil
}

// Clear removes all documents from the index.
func (e *SearchEngine) Clear(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.provider.DeleteCollection(ctx, e.collection); err != nil {
		return fmt.Errorf("failed to clear collection: %w", err)
	}

	slog.Info("Cleared RAG index", "collection", e.collection)
	return nil
}

// Collection returns the collection name.
func (e *SearchEngine) Collection() string {
	return e.collection
}

// Close releases resources.
func (e *SearchEngine) Close() error {
	// Provider is managed externally
	return nil
}

// validateQuery validates query parameters.
//
// Direct port from legacy pkg/context/search.go
func (e *SearchEngine) validateQuery(query string) error {
	if query == "" {
		return nil // Empty queries are allowed (return empty results)
	}
	if len(query) < MinQueryLength {
		return fmt.Errorf("query too short (min %d characters)", MinQueryLength)
	}
	if len(query) > MaxQueryLength {
		return fmt.Errorf("query too long (max %d characters)", MaxQueryLength)
	}
	return nil
}

// processQuery normalizes and cleans up a query string.
//
// Direct port from legacy pkg/context/search.go
func (e *SearchEngine) processQuery(query string) string {
	// Trim whitespace
	processed := strings.TrimSpace(query)

	// Normalize whitespace (collapse multiple spaces)
	processed = strings.Join(strings.Fields(processed), " ")

	return processed
}

// DeleteByFilter removes documents matching the filter.
//
// Direct port from legacy pkg/context/search.go
func (e *SearchEngine) DeleteByFilter(ctx context.Context, filter map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.provider.DeleteByFilter(ctx, e.collection, filter); err != nil {
		return fmt.Errorf("failed to delete by filter: %w", err)
	}
	return nil
}

// Status returns the current status of the search engine.
//
// Direct port from legacy pkg/context/search.go (GetStatus)
func (e *SearchEngine) Status() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status := map[string]any{
		"collection":      e.collection,
		"provider":        e.provider.Name(),
		"has_chunker":     e.chunker != nil,
		"has_hyde":        e.hyde != nil,
		"has_reranker":    e.reranker != nil,
		"has_multi_query": e.multiQuery != nil,
	}

	// Add config info
	status["config"] = map[string]any{
		"default_top_k":     e.config.DefaultTopK,
		"default_threshold": e.config.DefaultThreshold,
	}

	return status
}
