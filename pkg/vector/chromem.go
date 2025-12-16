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

package vector

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/philippgille/chromem-go"

	"github.com/verikod/hector/pkg/utils"
)

// ChromemProvider implements Provider using chromem-go for embedded vector storage.
//
// This is the recommended provider for zero-config deployments as it requires
// no external services. It stores vectors in memory with optional file persistence.
//
// Features:
//   - Pure Go, no external dependencies
//   - Optional file persistence (gzip compressed)
//   - Cosine similarity search
//   - Metadata filtering
//
// Limitations:
//   - Single-process only (no distributed search)
//   - Memory-bound (all vectors in RAM)
//   - No hybrid search support
//
// For production at scale, consider Qdrant or other external providers.
type ChromemProvider struct {
	db          *chromem.DB
	persistPath string
	compress    bool
	mu          sync.RWMutex

	// collections caches collection references for performance
	collections map[string]*chromem.Collection

	// embeddingFunc is used for similarity search (identity function)
	// The actual embedding is done externally via the embedder package
	embeddingFunc chromem.EmbeddingFunc
}

// ChromemConfig configures the chromem provider.
type ChromemConfig struct {
	// PersistPath for file persistence (optional).
	// If empty, vectors are stored in memory only.
	// Directory will be created if it doesn't exist.
	PersistPath string `yaml:"persist_path,omitempty"`

	// Compress enables gzip compression for persistence.
	// Reduces file size but increases CPU usage.
	Compress bool `yaml:"compress,omitempty"`
}

// NewChromemProvider creates a new chromem-based vector provider.
func NewChromemProvider(cfg ChromemConfig) (*ChromemProvider, error) {
	var db *chromem.DB
	var err error

	if cfg.PersistPath != "" {
		// Ensure directory exists
		// Use centralized EnsureHectorDir if path contains .hector
		dir := cfg.PersistPath
		if filepath.Base(dir) == ".hector" || filepath.Base(filepath.Dir(dir)) == ".hector" {
			// Extract base path (parent of .hector)
			basePath := dir
			for filepath.Base(basePath) == ".hector" || filepath.Base(basePath) == "vectors" || filepath.Base(basePath) == "chromem" {
				basePath = filepath.Dir(basePath)
			}
			if basePath == "" || basePath == "." {
				basePath = "."
			}
			if _, err := utils.EnsureHectorDir(basePath); err != nil {
				return nil, fmt.Errorf("failed to create .hector directory: %w", err)
			}
			// Also ensure the full path exists (for subdirectories like .hector/vectors)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create persist directory: %w", err)
			}
		} else {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create persist directory: %w", err)
			}
		}

		// Try to load existing database
		// NewPersistentDB expects a directory path, not a file path
		db, err = chromem.NewPersistentDB(cfg.PersistPath, cfg.Compress)
		if err != nil {
			slog.Warn("Failed to create persistent vector database, using in-memory",
				"path", cfg.PersistPath,
				"error", err)
			db = chromem.NewDB()
		} else {
			slog.Info("Using persistent vector database", "path", cfg.PersistPath)
		}
	} else {
		db = chromem.NewDB()
		slog.Info("Created in-memory vector database (no persistence)")
	}

	// Identity embedding function - we receive pre-computed vectors
	identityEmbed := func(ctx context.Context, text string) ([]float32, error) {
		// This should not be called when using pre-computed vectors
		return nil, fmt.Errorf("embedding function called but vectors should be pre-computed")
	}

	return &ChromemProvider{
		db:            db,
		persistPath:   cfg.PersistPath,
		compress:      cfg.Compress,
		collections:   make(map[string]*chromem.Collection),
		embeddingFunc: identityEmbed,
	}, nil
}

// getCollection gets or creates a collection.
func (p *ChromemProvider) getCollection(ctx context.Context, name string) (*chromem.Collection, error) {
	p.mu.RLock()
	if col, ok := p.collections[name]; ok {
		p.mu.RUnlock()
		return col, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if col, ok := p.collections[name]; ok {
		return col, nil
	}

	// Create or get collection
	col, err := p.db.GetOrCreateCollection(name, nil, p.embeddingFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to get/create collection %q: %w", name, err)
	}

	p.collections[name] = col
	slog.Debug("ChromemProvider getCollection: created new collection",
		"name", name)
	return col, nil
}

// Upsert adds or updates a document with its vector embedding.
func (p *ChromemProvider) Upsert(ctx context.Context, collection string, id string, vector []float32, metadata map[string]any) error {
	col, err := p.getCollection(ctx, collection)
	if err != nil {
		return err
	}

	countBefore := col.Count()

	// Convert metadata to string map (chromem requirement)
	strMetadata := make(map[string]string, len(metadata))
	for k, v := range metadata {
		strMetadata[k] = fmt.Sprint(v)
	}

	// Extract content from metadata if present
	content := ""
	if c, ok := metadata["content"].(string); ok {
		content = c
	}

	doc := chromem.Document{
		ID:        id,
		Content:   content,
		Metadata:  strMetadata,
		Embedding: vector,
	}

	// AddDocument with pre-computed embedding
	if err := col.AddDocuments(ctx, []chromem.Document{doc}, runtime.NumCPU()); err != nil {
		return fmt.Errorf("failed to upsert document: %w", err)
	}

	countAfter := col.Count()
	if countAfter > countBefore {
		slog.Debug("ChromemProvider Upsert: document added",
			"collection", collection,
			"doc_id", id,
			"count", countAfter)
	}

	// Persist if enabled
	if err := p.persist(); err != nil {
		slog.Warn("Failed to persist after upsert", "error", err)
	}

	return nil
}

// Search finds the most similar vectors in a collection.
func (p *ChromemProvider) Search(ctx context.Context, collection string, vector []float32, topK int) ([]Result, error) {
	return p.SearchWithFilter(ctx, collection, vector, topK, nil)
}

// SearchWithFilter combines vector similarity with metadata filtering.
func (p *ChromemProvider) SearchWithFilter(ctx context.Context, collection string, vector []float32, topK int, filter map[string]any) ([]Result, error) {
	col, err := p.getCollection(ctx, collection)
	if err != nil {
		return nil, err
	}

	// Cap topK to collection count (chromem requires nResults <= document count)
	docCount := col.Count()
	slog.Info("ChromemProvider search",
		"collection", collection,
		"doc_count", docCount,
		"requested_topK", topK)
	if docCount == 0 {
		slog.Warn("ChromemProvider search: collection is empty - no documents to search",
			"collection", collection)
		return []Result{}, nil
	}
	if topK > docCount {
		topK = docCount
	}

	// Convert filter to string map
	var whereFilter map[string]string
	if len(filter) > 0 {
		whereFilter = make(map[string]string, len(filter))
		for k, v := range filter {
			whereFilter[k] = fmt.Sprint(v)
		}
	}

	// Query using pre-computed vector
	// chromem.QueryWithEmbedding would be ideal but Query with empty text works
	// We need to use the embedding directly
	results, err := col.QueryEmbedding(ctx, vector, topK, whereFilter, nil)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert to our Result type
	out := make([]Result, 0, len(results))
	for _, r := range results {
		metadata := make(map[string]any, len(r.Metadata))
		for k, v := range r.Metadata {
			metadata[k] = v
		}

		out = append(out, Result{
			ID:       r.ID,
			Score:    r.Similarity,
			Content:  r.Content,
			Metadata: metadata,
		})
	}

	return out, nil
}

// Delete removes a document from a collection by ID.
func (p *ChromemProvider) Delete(ctx context.Context, collection string, id string) error {
	col, err := p.getCollection(ctx, collection)
	if err != nil {
		return err
	}

	if err := col.Delete(ctx, nil, nil, id); err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}

	if err := p.persist(); err != nil {
		slog.Warn("Failed to persist after delete", "error", err)
	}

	return nil
}

// DeleteByFilter removes all documents matching the filter.
func (p *ChromemProvider) DeleteByFilter(ctx context.Context, collection string, filter map[string]any) error {
	col, err := p.getCollection(ctx, collection)
	if err != nil {
		return err
	}

	// Convert filter to string map
	whereFilter := make(map[string]string, len(filter))
	for k, v := range filter {
		whereFilter[k] = fmt.Sprint(v)
	}

	if err := col.Delete(ctx, whereFilter, nil); err != nil {
		return fmt.Errorf("failed to delete by filter: %w", err)
	}

	if err := p.persist(); err != nil {
		slog.Warn("Failed to persist after delete", "error", err)
	}

	return nil
}

// CreateCollection creates a new collection.
// chromem-go creates collections implicitly, so this is a no-op.
func (p *ChromemProvider) CreateCollection(ctx context.Context, collection string, vectorDimension int) error {
	_, err := p.getCollection(ctx, collection)
	return err
}

// DeleteCollection removes a collection and all its documents.
func (p *ChromemProvider) DeleteCollection(ctx context.Context, collection string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.db.DeleteCollection(collection); err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}

	delete(p.collections, collection)

	if err := p.persist(); err != nil {
		slog.Warn("Failed to persist after collection delete", "error", err)
	}

	return nil
}

// Name returns the provider name.
func (p *ChromemProvider) Name() string {
	return "chromem"
}

// Close persists the database and releases resources.
func (p *ChromemProvider) Close() error {
	return p.persist()
}

// persist is a no-op since NewPersistentDB handles persistence automatically.
// Kept for API compatibility.
func (p *ChromemProvider) persist() error {
	// NewPersistentDB handles persistence automatically on each write operation.
	// No manual export needed.
	return nil
}

// Ensure ChromemProvider implements Provider.
var _ Provider = (*ChromemProvider)(nil)
