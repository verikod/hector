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

package builder

import (
	"fmt"

	"github.com/verikod/hector/pkg/embedder"
	"github.com/verikod/hector/pkg/rag"
	"github.com/verikod/hector/pkg/vector"
)

// DocumentStoreBuilder provides a fluent API for building RAG document stores.
//
// Document stores index documents from various sources for semantic search.
// They combine:
//   - Data source: Where documents come from
//   - Search engine: How to index and search (embeddings + vector store)
//   - Chunking: How to split documents
//
// Example:
//
//	store, err := builder.NewDocumentStore("docs").
//	    FromDirectory("./documents").
//	    WithVectorProvider(vectorProvider).
//	    WithEmbedder(embedder).
//	    EnableWatching(true).
//	    Build()
type DocumentStoreBuilder struct {
	name        string
	description string
	collection  string

	// Source configuration
	sourceType      string
	sourcePath      string
	includePatterns []string
	excludePatterns []string
	maxFileSize     int64

	// Search engine components
	vectorProvider vector.Provider
	embedder       embedder.Embedder

	// Chunking options
	chunkSize    int
	chunkOverlap int

	// Index options
	watchEnabled        bool
	incrementalIndexing bool
	enableCheckpoints   bool
	enableProgress      bool
	maxConcurrent       int

	// Search options
	defaultTopK      int
	defaultThreshold float32
	enableHyDE       bool
	enableRerank     bool
	enableMultiQuery bool
}

// NewDocumentStore creates a new document store builder.
//
// Example:
//
//	store, err := builder.NewDocumentStore("my-docs").
//	    FromDirectory("./docs").
//	    WithVectorProvider(provider).
//	    WithEmbedder(emb).
//	    Build()
func NewDocumentStore(name string) *DocumentStoreBuilder {
	if name == "" {
		panic("document store name cannot be empty")
	}
	return &DocumentStoreBuilder{
		name:                name,
		collection:          name,
		chunkSize:           512,
		chunkOverlap:        50,
		incrementalIndexing: true,
		enableCheckpoints:   true,
		enableProgress:      true,
		maxConcurrent:       0, // Use default (NumCPU)
		defaultTopK:         10,
		defaultThreshold:    0.0,
	}
}

// Description sets the store description.
//
// Example:
//
//	builder.NewDocumentStore("docs").Description("Product documentation")
func (b *DocumentStoreBuilder) Description(desc string) *DocumentStoreBuilder {
	b.description = desc
	return b
}

// Collection sets the vector collection name.
//
// Example:
//
//	builder.NewDocumentStore("docs").Collection("documents")
func (b *DocumentStoreBuilder) Collection(collection string) *DocumentStoreBuilder {
	b.collection = collection
	return b
}

// FromDirectory configures a directory source.
//
// Example:
//
//	builder.NewDocumentStore("docs").FromDirectory("./documents")
func (b *DocumentStoreBuilder) FromDirectory(path string) *DocumentStoreBuilder {
	b.sourceType = "directory"
	b.sourcePath = path
	return b
}

// IncludePatterns sets glob patterns for file inclusion.
//
// Example:
//
//	builder.NewDocumentStore("docs").IncludePatterns("*.md", "*.txt")
func (b *DocumentStoreBuilder) IncludePatterns(patterns ...string) *DocumentStoreBuilder {
	b.includePatterns = patterns
	return b
}

// ExcludePatterns sets glob patterns for file exclusion.
//
// Example:
//
//	builder.NewDocumentStore("docs").ExcludePatterns("*.tmp", "node_modules/**")
func (b *DocumentStoreBuilder) ExcludePatterns(patterns ...string) *DocumentStoreBuilder {
	b.excludePatterns = patterns
	return b
}

// MaxFileSize sets the maximum file size to process (in bytes).
// Files larger than this will be skipped. 0 means no limit.
//
// Example:
//
//	builder.NewDocumentStore("docs").MaxFileSize(10 * 1024 * 1024) // 10MB
func (b *DocumentStoreBuilder) MaxFileSize(size int64) *DocumentStoreBuilder {
	if size < 0 {
		panic("max file size must be non-negative")
	}
	b.maxFileSize = size
	return b
}

// WithVectorProvider sets the vector database provider.
//
// Example:
//
//	provider, _ := builder.NewVectorProvider("chromem").Build()
//	builder.NewDocumentStore("docs").WithVectorProvider(provider)
func (b *DocumentStoreBuilder) WithVectorProvider(provider vector.Provider) *DocumentStoreBuilder {
	if provider == nil {
		panic("vector provider cannot be nil")
	}
	b.vectorProvider = provider
	return b
}

// WithEmbedder sets the embedding provider.
//
// Example:
//
//	emb, _ := builder.NewEmbedder("openai").Build()
//	builder.NewDocumentStore("docs").WithEmbedder(emb)
func (b *DocumentStoreBuilder) WithEmbedder(emb embedder.Embedder) *DocumentStoreBuilder {
	if emb == nil {
		panic("embedder cannot be nil")
	}
	b.embedder = emb
	return b
}

// ChunkSize sets the size of document chunks.
//
// Example:
//
//	builder.NewDocumentStore("docs").ChunkSize(1000)
func (b *DocumentStoreBuilder) ChunkSize(size int) *DocumentStoreBuilder {
	if size <= 0 {
		panic("chunk size must be positive")
	}
	b.chunkSize = size
	return b
}

// ChunkOverlap sets the overlap between chunks.
//
// Example:
//
//	builder.NewDocumentStore("docs").ChunkOverlap(100)
func (b *DocumentStoreBuilder) ChunkOverlap(overlap int) *DocumentStoreBuilder {
	if overlap < 0 {
		panic("chunk overlap must be non-negative")
	}
	b.chunkOverlap = overlap
	return b
}

// EnableWatching enables file watching for automatic re-indexing.
//
// Example:
//
//	builder.NewDocumentStore("docs").EnableWatching(true)
func (b *DocumentStoreBuilder) EnableWatching(enabled bool) *DocumentStoreBuilder {
	b.watchEnabled = enabled
	return b
}

// EnableIncremental enables incremental indexing (only changed files).
//
// Example:
//
//	builder.NewDocumentStore("docs").EnableIncremental(true)
func (b *DocumentStoreBuilder) EnableIncremental(enabled bool) *DocumentStoreBuilder {
	b.incrementalIndexing = enabled
	return b
}

// EnableCheckpoints enables checkpoint/resume for interrupted indexing.
//
// Example:
//
//	builder.NewDocumentStore("docs").EnableCheckpoints(true)
func (b *DocumentStoreBuilder) EnableCheckpoints(enabled bool) *DocumentStoreBuilder {
	b.enableCheckpoints = enabled
	return b
}

// EnableProgress enables progress display during indexing.
//
// Example:
//
//	builder.NewDocumentStore("docs").EnableProgress(true)
func (b *DocumentStoreBuilder) EnableProgress(enabled bool) *DocumentStoreBuilder {
	b.enableProgress = enabled
	return b
}

// MaxConcurrent sets the maximum concurrent indexing workers.
// Default is NumCPU.
//
// Example:
//
//	builder.NewDocumentStore("docs").MaxConcurrent(4)
func (b *DocumentStoreBuilder) MaxConcurrent(max int) *DocumentStoreBuilder {
	if max < 0 {
		panic("max concurrent must be non-negative")
	}
	b.maxConcurrent = max
	return b
}

// DefaultTopK sets the default number of results to return in searches.
//
// Example:
//
//	builder.NewDocumentStore("docs").DefaultTopK(10)
func (b *DocumentStoreBuilder) DefaultTopK(k int) *DocumentStoreBuilder {
	if k <= 0 {
		panic("top k must be positive")
	}
	b.defaultTopK = k
	return b
}

// DefaultThreshold sets the minimum similarity score for search results.
//
// Example:
//
//	builder.NewDocumentStore("docs").DefaultThreshold(0.5)
func (b *DocumentStoreBuilder) DefaultThreshold(score float32) *DocumentStoreBuilder {
	if score < 0 || score > 1 {
		panic("threshold must be between 0 and 1")
	}
	b.defaultThreshold = score
	return b
}

// EnableHyDE enables Hypothetical Document Embeddings for better search.
//
// Example:
//
//	builder.NewDocumentStore("docs").EnableHyDE(true)
func (b *DocumentStoreBuilder) EnableHyDE(enabled bool) *DocumentStoreBuilder {
	b.enableHyDE = enabled
	return b
}

// EnableRerank enables LLM-based result reranking.
//
// Example:
//
//	builder.NewDocumentStore("docs").EnableRerank(true)
func (b *DocumentStoreBuilder) EnableRerank(enabled bool) *DocumentStoreBuilder {
	b.enableRerank = enabled
	return b
}

// EnableMultiQuery enables query expansion for better recall.
//
// Example:
//
//	builder.NewDocumentStore("docs").EnableMultiQuery(true)
func (b *DocumentStoreBuilder) EnableMultiQuery(enabled bool) *DocumentStoreBuilder {
	b.enableMultiQuery = enabled
	return b
}

// Build creates the document store.
//
// Returns an error if required parameters are missing.
func (b *DocumentStoreBuilder) Build() (*rag.DocumentStore, error) {
	// Validate required parameters
	if b.vectorProvider == nil {
		return nil, fmt.Errorf("vector provider is required: use WithVectorProvider()")
	}
	if b.embedder == nil {
		return nil, fmt.Errorf("embedder is required: use WithEmbedder()")
	}
	if b.sourceType == "" || b.sourcePath == "" {
		return nil, fmt.Errorf("source is required: use FromDirectory()")
	}

	// Create source
	var source rag.DataSource
	var err error

	switch b.sourceType {
	case "directory":
		source, err = rag.NewDirectorySourceFromConfig(rag.DirectorySourceConfig{
			Path:        b.sourcePath,
			Include:     b.includePatterns,
			Exclude:     b.excludePatterns,
			MaxFileSize: b.maxFileSize,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create directory source: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported source type: %s", b.sourceType)
	}

	// Create chunker
	chunker := rag.NewSimpleChunker(rag.ChunkerConfig{
		Size:    b.chunkSize,
		Overlap: b.chunkOverlap,
	})

	// Create search engine
	engineCfg := rag.SearchEngineConfig{
		Provider:         b.vectorProvider,
		Embedder:         b.embedder,
		Chunker:          chunker,
		Collection:       b.collection,
		DefaultTopK:      b.defaultTopK,
		DefaultThreshold: b.defaultThreshold,
	}
	engine, err := rag.NewSearchEngine(engineCfg)
	if err != nil {
		source.Close()
		return nil, fmt.Errorf("failed to create search engine: %w", err)
	}

	// Build search options (for advanced features)
	var searchOpts *rag.SearchOptions
	if b.enableHyDE || b.enableRerank || b.enableMultiQuery {
		searchOpts = &rag.SearchOptions{
			EnableHyDE:       b.enableHyDE,
			EnableRerank:     b.enableRerank,
			EnableMultiQuery: b.enableMultiQuery,
		}
	}

	// Create document store
	store, err := rag.NewDocumentStore(rag.DocumentStoreConfig{
		Name:                  b.name,
		Description:           b.description,
		Source:                source,
		SearchEngine:          engine,
		Chunker:               chunker,
		Collection:            b.collection,
		SourcePath:            b.sourcePath,
		Watch:                 b.watchEnabled,
		IncrementalIndexing:   b.incrementalIndexing,
		EnableCheckpoints:     b.enableCheckpoints,
		EnableProgress:        b.enableProgress,
		MaxConcurrentIndexing: b.maxConcurrent,
		Search:                searchOpts,
	})
	if err != nil {
		engine.Close()
		source.Close()
		return nil, fmt.Errorf("failed to create document store: %w", err)
	}

	return store, nil
}

// MustBuild creates the document store or panics on error.
//
// Use this only when you're certain the configuration is valid.
func (b *DocumentStoreBuilder) MustBuild() *rag.DocumentStore {
	store, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build document store: %v", err))
	}
	return store
}
