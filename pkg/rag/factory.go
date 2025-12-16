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
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/embedder"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/vector"
)

// FactoryDeps provides dependencies for creating RAG components.
type FactoryDeps struct {
	// DBPool provides database connections.
	DBPool *config.DBPool

	// VectorProviders maps provider names to instances.
	VectorProviders map[string]vector.Provider

	// Embedders maps embedder names to instances.
	Embedders map[string]embedder.Embedder

	// LLMs maps LLM names to instances.
	LLMs map[string]model.LLM

	// ToolCaller provides access to MCP tools for document parsing.
	// Optional - only needed if MCPParsers is configured.
	ToolCaller ToolCaller

	// Config is the root configuration.
	Config *config.Config
}

// NewDataSourceFromConfig creates a data source from configuration.
func NewDataSourceFromConfig(cfg *config.DocumentSourceConfig, deps *FactoryDeps) (DataSource, error) {
	if cfg == nil {
		return NilDataSource{}, nil
	}

	switch cfg.Type {
	case "directory":
		dirCfg := DefaultDirectorySourceConfig(cfg.Path)
		if len(cfg.Include) > 0 {
			dirCfg.Include = cfg.Include
		}
		if len(cfg.Exclude) > 0 {
			dirCfg.Exclude = append(dirCfg.Exclude, cfg.Exclude...)
		}
		if cfg.MaxFileSize > 0 {
			dirCfg.MaxFileSize = cfg.MaxFileSize
		}
		return NewDirectorySourceFromConfig(dirCfg)

	case "sql":
		if cfg.SQL == nil {
			return nil, fmt.Errorf("sql config is required for sql source")
		}
		return newSQLSourceFromConfig(cfg.SQL, deps)

	case "api":
		if cfg.API == nil {
			return nil, fmt.Errorf("api config is required for api source")
		}
		return newAPISourceFromConfig(cfg.API)

	case "collection":
		// Collection source - references an existing pre-populated collection
		collection := cfg.Collection
		if collection == "" {
			return nil, fmt.Errorf("collection name is required for collection source")
		}
		return NewCollectionSource(collection), nil

	default:
		return nil, fmt.Errorf("unknown data source type: %q", cfg.Type)
	}
}

// newAPISourceFromConfig creates an API source from configuration.
func newAPISourceFromConfig(cfg *config.APISourceConfig) (*APISource, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("url is required for api source")
	}

	// Build endpoint config from the simplified config
	endpoint := APIEndpointConfig{
		Path:         "", // Base URL already includes path
		Method:       "GET",
		IDField:      cfg.IDField,
		ContentField: cfg.ContentField,
		Headers:      cfg.Headers,
	}

	// Build auth config if headers contain auth
	var auth *APIAuthConfig
	if token, ok := cfg.Headers["Authorization"]; ok {
		if len(token) > 7 && token[:7] == "Bearer " {
			auth = &APIAuthConfig{
				Type:  "bearer",
				Token: token[7:],
			}
			delete(cfg.Headers, "Authorization")
		}
	}

	return NewAPISource(cfg.URL, []APIEndpointConfig{endpoint}, auth), nil
}

// newSQLSourceFromConfig creates a SQL source using DBPool.
func newSQLSourceFromConfig(cfg *config.SQLSourceConfig, deps *FactoryDeps) (*SQLSource, error) {
	if deps.DBPool == nil {
		return nil, fmt.Errorf("DBPool is required for SQL source")
	}
	if deps.Config == nil {
		return nil, fmt.Errorf("Config is required for SQL source")
	}

	// Get database config by name
	dbCfg, ok := deps.Config.Databases[cfg.Database]
	if !ok {
		return nil, fmt.Errorf("database %q not found in configuration", cfg.Database)
	}

	// Get connection from pool
	conn, err := deps.DBPool.Get(dbCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	// Convert config table configs to SQLTableConfig
	var tables []SQLTableConfig
	for _, t := range cfg.Tables {
		tables = append(tables, SQLTableConfig{
			Table:           t.Table,
			Columns:         t.Columns,
			IDColumn:        t.IDColumn,
			UpdatedColumn:   t.UpdatedColumn,
			WhereClause:     t.WhereClause,
			MetadataColumns: t.MetadataColumns,
		})
	}

	return NewSQLSource(SQLSourceOptions{
		DB:      conn,
		Driver:  dbCfg.Driver,
		Tables:  tables,
		MaxRows: 10000,
	})
}

// NewChunkerFromConfig creates a chunker from configuration.
func NewChunkerFromConfig(cfg *config.ChunkingConfig) (Chunker, error) {
	if cfg == nil {
		return NewSimpleChunker(DefaultChunkerConfig()), nil
	}

	ragCfg := ChunkerConfig{
		Strategy:      ChunkerStrategy(cfg.Strategy),
		Size:          cfg.Size,
		Overlap:       cfg.Overlap,
		MinSize:       cfg.MinSize,
		MaxSize:       cfg.MaxSize,
		PreserveWords: cfg.PreserveWords != nil && *cfg.PreserveWords,
	}

	return NewChunker(ragCfg)
}

// NewSearchEngineFromConfig creates a search engine from configuration.
// collectionName is used as the default if storeCfg.Collection is empty.
func NewSearchEngineFromConfig(
	storeCfg *config.DocumentStoreConfig,
	deps *FactoryDeps,
	collectionName string,
) (*SearchEngine, error) {
	// Get vector provider
	var provider vector.Provider
	if storeCfg.VectorStore != "" {
		var ok bool
		provider, ok = deps.VectorProviders[storeCfg.VectorStore]
		if !ok {
			return nil, fmt.Errorf("vector store %q not found", storeCfg.VectorStore)
		}
		slog.Debug("Using configured vector provider",
			"name", storeCfg.VectorStore,
			"provider", provider.Name())
	} else {
		// Use first available provider or create default chromem
		for _, p := range deps.VectorProviders {
			provider = p
			break
		}
		if provider == nil {
			// Create default embedded provider
			slog.Debug("Creating default chromem provider (no provider configured)")
			var err error
			provider, err = vector.NewChromemProvider(vector.ChromemConfig{})
			if err != nil {
				return nil, fmt.Errorf("failed to create default vector provider: %w", err)
			}
		}
	}

	// Get embedder
	var emb embedder.Embedder
	if storeCfg.Embedder != "" {
		var ok bool
		emb, ok = deps.Embedders[storeCfg.Embedder]
		if !ok {
			return nil, fmt.Errorf("embedder %q not found", storeCfg.Embedder)
		}
	} else {
		// Use first available embedder
		for _, e := range deps.Embedders {
			emb = e
			break
		}
		if emb == nil {
			return nil, fmt.Errorf("no embedder available")
		}
	}

	// Create chunker
	chunker, err := NewChunkerFromConfig(storeCfg.Chunking)
	if err != nil {
		return nil, fmt.Errorf("failed to create chunker: %w", err)
	}

	// Create optional HyDE
	var hyde *HyDE
	if storeCfg.Search != nil && storeCfg.Search.EnableHyDE && storeCfg.Search.HyDELLM != "" {
		llm, ok := deps.LLMs[storeCfg.Search.HyDELLM]
		if !ok {
			return nil, fmt.Errorf("HyDE LLM %q not found", storeCfg.Search.HyDELLM)
		}
		hyde = NewHyDE(llm)
	}

	// Create optional reranker
	var reranker *Reranker
	if storeCfg.Search != nil && storeCfg.Search.EnableRerank && storeCfg.Search.RerankLLM != "" {
		llm, ok := deps.LLMs[storeCfg.Search.RerankLLM]
		if !ok {
			return nil, fmt.Errorf("rerank LLM %q not found", storeCfg.Search.RerankLLM)
		}
		maxResults := 20
		if storeCfg.Search.RerankMaxResults > 0 {
			maxResults = storeCfg.Search.RerankMaxResults
		}
		reranker = NewReranker(llm, maxResults)
	}

	// Create optional multi-query
	var multiQuery *MultiQueryExpander
	if storeCfg.Search != nil && storeCfg.Search.EnableMultiQuery && storeCfg.Search.MultiQueryLLM != "" {
		llm, ok := deps.LLMs[storeCfg.Search.MultiQueryLLM]
		if !ok {
			return nil, fmt.Errorf("multi-query LLM %q not found", storeCfg.Search.MultiQueryLLM)
		}
		numQueries := 3
		if storeCfg.Search.MultiQueryCount > 0 {
			numQueries = storeCfg.Search.MultiQueryCount
		}
		multiQuery = NewMultiQueryExpander(llm, numQueries)
	}

	// Determine collection name
	collection := storeCfg.Collection
	if collection == "" {
		collection = collectionName
	}
	if collection == "" {
		collection = "rag_documents"
	}

	// Set default top-k
	defaultTopK := 10
	if storeCfg.Search != nil && storeCfg.Search.TopK > 0 {
		defaultTopK = storeCfg.Search.TopK
	}

	return NewSearchEngine(SearchEngineConfig{
		Provider:         provider,
		Embedder:         emb,
		Chunker:          chunker,
		Collection:       collection,
		DefaultTopK:      defaultTopK,
		DefaultThreshold: storeCfg.Search.Threshold,
		HyDE:             hyde,
		Reranker:         reranker,
		MultiQuery:       multiQuery,
	})
}

// NewDocumentStoreFromConfig creates a document store from configuration.
func NewDocumentStoreFromConfig(
	name string,
	storeCfg *config.DocumentStoreConfig,
	deps *FactoryDeps,
) (*DocumentStore, error) {
	// Determine collection name first (used by both SearchEngine and DocumentStore)
	collection := storeCfg.Collection
	if collection == "" {
		collection = name
	}

	// Create data source
	source, err := NewDataSourceFromConfig(storeCfg.Source, deps)
	if err != nil {
		return nil, fmt.Errorf("failed to create data source: %w", err)
	}

	// Create search engine with the same collection name
	engine, err := NewSearchEngineFromConfig(storeCfg, deps, collection)
	if err != nil {
		source.Close()
		return nil, fmt.Errorf("failed to create search engine: %w", err)
	}

	// Create chunker
	chunker, err := NewChunkerFromConfig(storeCfg.Chunking)
	if err != nil {
		source.Close()
		return nil, fmt.Errorf("failed to create chunker: %w", err)
	}

	// Build internal config
	internalCfg := DocumentStoreConfig{
		Name:                name,
		Source:              source,
		SearchEngine:        engine,
		Chunker:             chunker,
		Collection:          collection,
		Watch:               storeCfg.Watch,
		IncrementalIndexing: storeCfg.IncrementalIndexing,
	}

	// Wire through indexing config if present
	if storeCfg.Indexing != nil {
		internalCfg.MaxConcurrentIndexing = storeCfg.Indexing.MaxConcurrent

		if storeCfg.Indexing.Retry != nil {
			internalCfg.RetryConfig = &RetryConfig{
				MaxRetries:   storeCfg.Indexing.Retry.MaxRetries,
				BaseDelay:    storeCfg.Indexing.Retry.BaseDelay.Duration(),
				MaxDelay:     storeCfg.Indexing.Retry.MaxDelay.Duration(),
				JitterFactor: storeCfg.Indexing.Retry.Jitter,
			}
		}
	}

	store, err := NewDocumentStore(internalCfg)
	if err != nil {
		return nil, err
	}

	// Register MCP extractor if configured
	if storeCfg.MCPParsers != nil && len(storeCfg.MCPParsers.ToolNames) > 0 {
		if deps.ToolCaller == nil {
			return nil, fmt.Errorf("MCP parser requires ToolCaller (MCP tool must be configured)")
		}

		// Determine priority
		// Default: 8 (higher than native BinaryExtractor at 5)
		// If PreferNative=true: 4 (lower than native, MCP used as fallback)
		priority := 8
		if storeCfg.MCPParsers.Priority != nil {
			priority = *storeCfg.MCPParsers.Priority
		}
		if storeCfg.MCPParsers.PreferNative != nil && *storeCfg.MCPParsers.PreferNative {
			// Lower priority so native parsers are tried first
			priority = 4
		}

		// Get source path for path remapping (containerized MCP services)
		localBasePath := ""
		if storeCfg.Source != nil && storeCfg.Source.Type == "directory" {
			localBasePath = storeCfg.Source.Path
		}

		mcpExtractor, err := NewMCPExtractor(MCPExtractorConfig{
			ToolCaller:      deps.ToolCaller,
			ParserToolNames: storeCfg.MCPParsers.ToolNames,
			SupportedExts:   storeCfg.MCPParsers.Extensions,
			Priority:        priority,
			LocalBasePath:   localBasePath,
			PathPrefix:      storeCfg.MCPParsers.PathPrefix,
		})
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("failed to create MCP extractor: %w", err)
		}

		store.RegisterExtractor(mcpExtractor)
		slog.Debug("Registered MCP extractor",
			"store", name,
			"tools", storeCfg.MCPParsers.ToolNames,
			"extensions", storeCfg.MCPParsers.Extensions,
			"priority", priority,
			"prefer_native", storeCfg.MCPParsers.PreferNative != nil && *storeCfg.MCPParsers.PreferNative)
	}

	return store, nil
}

// NewVectorProviderFromConfig creates a vector provider from configuration.
func NewVectorProviderFromConfig(cfg *config.VectorStoreConfig) (vector.Provider, error) {
	if cfg == nil {
		// Default to embedded chromem
		return vector.NewChromemProvider(vector.ChromemConfig{})
	}

	switch cfg.Type {
	case "chromem":
		return vector.NewChromemProvider(vector.ChromemConfig{
			PersistPath: cfg.PersistPath,
			Compress:    cfg.Compress,
		})

	case "qdrant":
		useTLS := false
		if cfg.EnableTLS != nil {
			useTLS = *cfg.EnableTLS
		}
		port := cfg.Port
		if port == 0 {
			port = 6334 // Qdrant gRPC port
		}
		return vector.NewQdrantProvider(vector.QdrantConfig{
			Host:   cfg.Host,
			Port:   port,
			APIKey: cfg.APIKey,
			UseTLS: useTLS,
		})

	case "pinecone":
		return vector.NewPineconeProvider(vector.PineconeConfig{
			APIKey:      cfg.APIKey,
			Host:        cfg.Host,
			IndexName:   cfg.IndexName,
			Environment: cfg.Environment,
		})

	case "weaviate":
		useTLS := false
		if cfg.EnableTLS != nil {
			useTLS = *cfg.EnableTLS
		}
		port := cfg.Port
		if port == 0 {
			port = 8080
		}
		return vector.NewWeaviateProvider(vector.WeaviateConfig{
			Host:   cfg.Host,
			Port:   port,
			APIKey: cfg.APIKey,
			UseTLS: useTLS,
		})

	case "milvus":
		useTLS := false
		if cfg.EnableTLS != nil {
			useTLS = *cfg.EnableTLS
		}
		port := cfg.Port
		if port == 0 {
			port = 19530
		}
		return vector.NewMilvusProvider(vector.MilvusConfig{
			Host:   cfg.Host,
			Port:   port,
			APIKey: cfg.APIKey,
			UseTLS: useTLS,
		})

	case "chroma":
		useTLS := false
		if cfg.EnableTLS != nil {
			useTLS = *cfg.EnableTLS
		}
		port := cfg.Port
		if port == 0 {
			port = 8000
		}
		return vector.NewChromaProvider(vector.ChromaConfig{
			Host:   cfg.Host,
			Port:   port,
			APIKey: cfg.APIKey,
			UseTLS: useTLS,
		})

	default:
		return nil, fmt.Errorf("unsupported vector store type: %s", cfg.Type)
	}
}

// DBPoolAdapter wraps config.DBPool to provide sql.DB connections.
type DBPoolAdapter struct {
	pool *config.DBPool
	cfg  *config.Config
}

// NewDBPoolAdapter creates an adapter for the DBPool.
func NewDBPoolAdapter(pool *config.DBPool, cfg *config.Config) *DBPoolAdapter {
	return &DBPoolAdapter{pool: pool, cfg: cfg}
}

// Get returns a database connection for the given database name.
func (a *DBPoolAdapter) Get(name string) (*sql.DB, string, error) {
	dbCfg, ok := a.cfg.Databases[name]
	if !ok {
		return nil, "", fmt.Errorf("database %q not found", name)
	}
	conn, err := a.pool.Get(dbCfg)
	if err != nil {
		return nil, "", err
	}
	return conn, dbCfg.Driver, nil
}
