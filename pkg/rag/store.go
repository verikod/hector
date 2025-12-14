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
	"sync"
	"sync/atomic"
	"time"
)

// DocumentStore manages document indexing and search.
//
// It combines:
//   - DataSource: Where documents come from
//   - ContentExtractor: How to extract text from documents
//   - SearchEngine: How to index and search
//   - File watching: Automatic re-indexing on changes
//   - Concurrent indexing with configurable worker pool
//   - Retry logic for transient failures
//   - Checkpoint/resume for interrupted indexing
//   - Progress tracking with ETA
//
// Direct port from legacy pkg/context/document_store.go
type DocumentStore struct {
	name       string
	source     DataSource
	extractor  *ExtractorRegistry
	engine     *SearchEngine
	chunker    Chunker
	collection string
	config     DocumentStoreConfig
	sourcePath string // Base path for checkpoints/progress (from directory source)

	// Options
	watchEnabled        bool
	incrementalIndexing bool
	indexedDocs         map[string]time.Time
	watchCancel         context.CancelFunc

	// File watching (for directory sources)
	watcher *FileWatcher

	// Concurrency control (from legacy indexingSemaphore)
	maxConcurrentIndexing int
	retryer               *Retryer

	// Progress tracking and checkpoints (from legacy)
	progressTracker   *ProgressTracker
	checkpointManager *IndexCheckpointManager

	// Metrics tracking
	metrics *IndexMetrics

	mu sync.RWMutex
}

// DocumentStoreConfig configures a document store.
type DocumentStoreConfig struct {
	// Name identifies this store.
	Name string

	// Description describes the store (used by SearchTool).
	Description string

	// Source provides documents.
	Source DataSource

	// SearchEngine for indexing and search.
	SearchEngine *SearchEngine

	// Chunker for splitting documents (optional, defaults to engine's chunker).
	Chunker Chunker

	// Collection name (optional, defaults to store name).
	Collection string

	// SourcePath is the base path for checkpoints (auto-detected from directory source).
	SourcePath string

	// Watch enables file watching for automatic re-indexing.
	Watch bool

	// IncrementalIndexing only re-indexes changed documents.
	IncrementalIndexing bool

	// EnableCheckpoints enables resume capability for interrupted indexing.
	// Checkpoints are saved to .hector/checkpoints/ in the source path.
	// Default: true for directory sources
	EnableCheckpoints bool

	// EnableProgress enables progress display during indexing.
	// Default: true
	EnableProgress bool

	// Search configuration for advanced features.
	Search *SearchOptions

	// MaxConcurrentIndexing limits parallel document processing (default: NumCPU).
	// Set to 1 for sequential indexing (legacy behavior).
	MaxConcurrentIndexing int

	// RetryConfig for transient failure handling (optional).
	RetryConfig *RetryConfig
}

// NewDocumentStore creates a new document store.
func NewDocumentStore(cfg DocumentStoreConfig) (*DocumentStore, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if cfg.Source == nil {
		return nil, fmt.Errorf("source is required")
	}
	if cfg.SearchEngine == nil {
		return nil, fmt.Errorf("search engine is required")
	}

	collection := cfg.Collection
	if collection == "" {
		collection = cfg.Name
	}

	chunker := cfg.Chunker
	if chunker == nil {
		chunker = NewSimpleChunker(DefaultChunkerConfig())
	}

	// Configure concurrency (default: NumCPU, like legacy indexingSemaphore)
	maxConcurrent := cfg.MaxConcurrentIndexing
	if maxConcurrent <= 0 {
		maxConcurrent = runtime.NumCPU()
	}

	// Configure retry
	var retryer *Retryer
	if cfg.RetryConfig != nil {
		retryer = NewRetryer(*cfg.RetryConfig)
	} else {
		retryer = NewRetryer(DefaultRetryConfig())
	}

	// Determine source path for checkpoints/progress (from directory source)
	sourcePath := cfg.SourcePath
	if sourcePath == "" {
		if dirSource, ok := cfg.Source.(*DirectorySource); ok {
			sourcePath = dirSource.GetBasePath()
		}
	}

	// Initialize checkpoint manager (like legacy)
	// Default: enabled for directory sources
	enableCheckpoints := cfg.EnableCheckpoints
	if !enableCheckpoints && sourcePath != "" && cfg.Source.Type() == "directory" {
		enableCheckpoints = true // Default enabled for directory sources
	}
	checkpointMgr := NewIndexCheckpointManager(cfg.Name, sourcePath, enableCheckpoints)

	// Initialize progress tracker (like legacy)
	enableProgress := cfg.EnableProgress
	if !enableProgress && sourcePath != "" {
		enableProgress = true // Default enabled
	}
	progressTracker := NewProgressTracker(enableProgress, false) // verbose=false by default

	return &DocumentStore{
		name:                  cfg.Name,
		source:                cfg.Source,
		extractor:             NewExtractorRegistry(),
		engine:                cfg.SearchEngine,
		chunker:               chunker,
		collection:            collection,
		config:                cfg,
		sourcePath:            sourcePath,
		watchEnabled:          cfg.Watch,
		incrementalIndexing:   cfg.IncrementalIndexing,
		indexedDocs:           make(map[string]time.Time),
		maxConcurrentIndexing: maxConcurrent,
		retryer:               retryer,
		progressTracker:       progressTracker,
		checkpointManager:     checkpointMgr,
		metrics:               NewIndexMetrics(cfg.Name),
	}, nil
}

// Config returns the store configuration.
func (s *DocumentStore) Config() DocumentStoreConfig {
	return s.config
}

// Name returns the store name.
func (s *DocumentStore) Name() string {
	return s.name
}

// Collection returns the collection name.
func (s *DocumentStore) Collection() string {
	return s.collection
}

// Index indexes all documents from the source with concurrent processing.
//
// Uses channel-based DiscoverDocuments from legacy architecture with
// worker pool for concurrent indexing (like legacy indexingSemaphore).
// Supports checkpoint/resume for interrupted indexing.
//
// Direct port from legacy pkg/context/document_store_indexing.go
func (s *DocumentStore) Index(ctx context.Context) error {
	slog.Info("Starting document indexing",
		"store", s.name,
		"workers", s.maxConcurrentIndexing)
	startTime := time.Now()

	// Reset metrics
	s.metrics.Reset()
	s.metrics.SetStartTime(startTime)

	// Try to load checkpoint (like legacy)
	var checkpoint *IndexCheckpoint
	if s.source.Type() == "directory" {
		var err error
		checkpoint, err = s.checkpointManager.LoadCheckpoint()
		if checkpoint != nil && err == nil {
			processedCount := len(checkpoint.ProcessedFiles)
			if processedCount > 0 {
				slog.Info("Loaded index state from checkpoint",
					"info", s.checkpointManager.FormatCheckpointInfo(checkpoint))
			}
		}
	}

	// Discover documents from source using channels
	docChan, errChan := s.source.DiscoverDocuments(ctx)

	// Worker pool for concurrent indexing (like legacy indexingSemaphore)
	semaphore := make(chan struct{}, s.maxConcurrentIndexing)
	var wg sync.WaitGroup
	var indexErrors []error
	var errorsMu sync.Mutex

	// Track counts atomically for concurrent updates
	var indexed, skipped, total, errors int64

	// Track found documents for cleanup (like legacy foundDocs)
	foundDocs := make(map[string]bool)
	var foundDocsMu sync.Mutex

	// Start progress tracker
	s.progressTracker.Start()
	defer func() {
		s.progressTracker.Stop()
		// Save final checkpoint
		if s.source.Type() == "directory" {
			_ = s.checkpointManager.SaveCheckpoint()
		}
	}()

	// Error collector goroutine
	go func() {
		for err := range errChan {
			if err != nil {
				errorsMu.Lock()
				indexErrors = append(indexErrors, err)
				errorsMu.Unlock()
				atomic.AddInt64(&errors, 1)
				slog.Warn("Error during document discovery", "error", err)
			}
		}
	}()

	// Process documents concurrently
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()

		case doc, ok := <-docChan:
			if !ok {
				// Channel closed, wait for workers to finish
				wg.Wait()

				elapsed := time.Since(startTime)
				s.metrics.SetEndTime(time.Now())

				finalTotal := atomic.LoadInt64(&total)
				finalIndexed := atomic.LoadInt64(&indexed)
				finalSkipped := atomic.LoadInt64(&skipped)
				finalErrors := atomic.LoadInt64(&errors)

				// Clean up deleted files (like legacy cleanupDeletedFiles)
				if s.source.Type() == "directory" && s.incrementalIndexing {
					s.cleanupDeletedFiles(ctx, foundDocs)
				}

				// Checkpoint is preserved for incremental indexing persistence

				slog.Info("Document indexing complete",
					"store", s.name,
					"total", finalTotal,
					"indexed", finalIndexed,
					"skipped", finalSkipped,
					"errors", finalErrors,
					"elapsed", elapsed,
					"docs_per_sec", float64(finalIndexed)/elapsed.Seconds())

				return nil
			}

			atomic.AddInt64(&total, 1)
			s.metrics.IncrementTotal()

			// Track found documents for cleanup
			foundDocsMu.Lock()
			foundDocs[doc.ID] = true
			foundDocsMu.Unlock()

			// Check if document should be indexed (from filter)
			shouldIndex := true
			if si, ok := doc.Metadata["should_index"].(bool); ok {
				shouldIndex = si
			}
			if !shouldIndex {
				atomic.AddInt64(&skipped, 1)
				s.metrics.IncrementSkipped()
				s.progressTracker.IncrementSkipped()
				s.progressTracker.IncrementProcessed()
				continue
			}

			// Get modification time for checkpoint/incremental checks
			modTime := time.Time{}
			fileSize := int64(0)
			if mt, ok := doc.Metadata["last_modified"].(int64); ok {
				modTime = time.Unix(mt, 0)
			}
			if sz, ok := doc.Metadata["size"].(int64); ok {
				fileSize = sz
			}

			// Check checkpoint for already-processed files (like legacy)
			if s.source.Type() == "directory" && checkpoint != nil {
				if !s.checkpointManager.ShouldProcessFile(doc.ID, fileSize, modTime) {
					atomic.AddInt64(&skipped, 1)
					s.metrics.IncrementSkipped()
					s.progressTracker.IncrementSkipped()
					s.progressTracker.IncrementProcessed()
					continue
				}
			}

			// Check if document needs re-indexing (incremental)
			if s.incrementalIndexing && checkpoint == nil {
				s.mu.RLock()
				lastIndexed, exists := s.indexedDocs[doc.ID]
				s.mu.RUnlock()

				if exists && !modTime.After(lastIndexed) {
					atomic.AddInt64(&skipped, 1)
					s.metrics.IncrementSkipped()
					s.progressTracker.IncrementSkipped()
					s.progressTracker.IncrementProcessed()
					continue
				}
			}

			// Acquire semaphore (block if at max concurrency)
			semaphore <- struct{}{}
			wg.Add(1)

			// Process document in goroutine
			go func(doc Document, fileSize int64, modTime time.Time) {
				defer func() {
					<-semaphore // Release semaphore
					wg.Done()
				}()

				// Update progress with current file
				s.progressTracker.SetCurrentFile(doc.ID)

				// Index with retry
				err := s.retryer.Do(ctx, "index_document", func() error {
					return s.indexSingleDocument(ctx, doc)
				})

				if err != nil {
					atomic.AddInt64(&errors, 1)
					s.metrics.IncrementErrors()
					s.progressTracker.IncrementFailed()
					s.progressTracker.IncrementProcessed()
					// Record failed file in checkpoint
					s.checkpointManager.RecordFile(doc.ID, fileSize, modTime, "failed")
					slog.Warn("Failed to index document",
						"document", doc.ID,
						"error", err)
					return
				}

				atomic.AddInt64(&indexed, 1)
				s.metrics.IncrementIndexed()
				s.progressTracker.IncrementIndexed()
				s.progressTracker.IncrementProcessed()

				// Track indexed document
				s.mu.Lock()
				s.indexedDocs[doc.ID] = time.Now()
				s.mu.Unlock()

				// Record in checkpoint (like legacy)
				s.checkpointManager.RecordFile(doc.ID, fileSize, modTime, "indexed")

				// Periodically save checkpoint (handled internally by manager)
				_ = s.checkpointManager.SaveCheckpoint()
			}(doc, fileSize, modTime)
		}
	}
}

// indexSingleDocument extracts and indexes a single document.
func (s *DocumentStore) indexSingleDocument(ctx context.Context, doc Document) error {
	// Extract content
	extracted, err := s.extractor.Extract(ctx, doc)
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Update document with extracted content
	if extracted.Content != "" {
		doc.Content = extracted.Content
	}
	if extracted.Title != "" && doc.Title == "" {
		doc.Title = extracted.Title
	}
	if doc.Metadata == nil {
		doc.Metadata = make(map[string]any)
	}
	if extracted.ExtractorName != "" {
		doc.Metadata["extractor"] = extracted.ExtractorName
	}
	doc.Metadata["collection"] = s.collection

	// Index document
	if err := s.engine.IngestDocument(ctx, doc); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	return nil
}

// Search searches for documents.
func (s *DocumentStore) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	startTime := time.Now()

	// Apply store defaults
	if req.Collection == "" {
		req.Collection = s.collection
	}

	// Apply search options from config if not set in request
	if req.Options == nil && s.config.Search != nil {
		req.Options = s.config.Search
	}

	result, err := s.engine.Search(ctx, req)

	// Record metrics
	s.metrics.RecordSearch(time.Since(startTime))

	return result, err
}

// SearchWithFilter searches with metadata filtering.
func (s *DocumentStore) SearchWithFilter(ctx context.Context, query string, topK int, filter map[string]any) (*SearchResponse, error) {
	req := SearchRequest{
		Query:      query,
		Collection: s.collection,
		TopK:       topK,
		Filter:     filter,
	}

	// Apply search options from config
	if s.config.Search != nil {
		req.Options = s.config.Search
	}

	return s.engine.Search(ctx, req)
}

// StartWatching starts watching for document changes.
//
// Direct port from legacy pkg/context/document_store.go
func (s *DocumentStore) StartWatching(ctx context.Context) error {
	if !s.watchEnabled {
		return nil
	}

	// Only directory sources support watching
	if s.source.Type() != "directory" {
		slog.Warn("File watching only supported for directory sources",
			"store", s.name,
			"source_type", s.source.Type())
		return nil
	}

	s.mu.Lock()
	if s.watchCancel != nil {
		s.mu.Unlock()
		return fmt.Errorf("already watching")
	}

	watchCtx, cancel := context.WithCancel(ctx)
	s.watchCancel = cancel

	// Get the base path from the directory source
	dirSource, ok := s.source.(*DirectorySource)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("source is not a DirectorySource")
	}

	// Create file watcher
	watcher, err := NewFileWatcher(FileWatcherConfig{
		BasePath: dirSource.GetBasePath(),
		Filter:   dirSource.GetFilter(),
	})
	if err != nil {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	s.watcher = watcher
	s.mu.Unlock()

	// Start watching
	events, err := watcher.Start(watchCtx)
	if err != nil {
		s.mu.Lock()
		s.watcher = nil
		s.watchCancel = nil
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("failed to start file watcher: %w", err)
	}

	// Process events
	go s.processEvents(watchCtx, events)

	slog.Info("Started watching for document changes", "store", s.name)
	return nil
}

// processEvents handles document change events.
func (s *DocumentStore) processEvents(ctx context.Context, events <-chan DocumentEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			s.handleEvent(ctx, event)
		}
	}
}

// handleEvent processes a single document event.
func (s *DocumentStore) handleEvent(ctx context.Context, event DocumentEvent) {
	switch event.Type {
	case DocumentEventCreate, DocumentEventUpdate:
		// Extract and index
		doc := event.Document
		extracted, err := s.extractor.Extract(ctx, doc)
		if err != nil {
			slog.Warn("Failed to extract content on change",
				"document", doc.ID,
				"error", err)
			return
		}

		if extracted.Content != "" {
			doc.Content = extracted.Content
		}
		if doc.Metadata == nil {
			doc.Metadata = make(map[string]any)
		}
		doc.Metadata["collection"] = s.collection

		if err := s.engine.IngestDocument(ctx, doc); err != nil {
			slog.Warn("Failed to index document on change",
				"document", doc.ID,
				"error", err)
			return
		}

		s.mu.Lock()
		s.indexedDocs[doc.ID] = time.Now()
		s.mu.Unlock()

		slog.Debug("Indexed document on change",
			"document", doc.ID,
			"event", event.Type)

	case DocumentEventDelete:
		if err := s.engine.DeleteDocument(ctx, event.Document.ID); err != nil {
			slog.Warn("Failed to delete document on change",
				"document", event.Document.ID,
				"error", err)
			return
		}

		s.mu.Lock()
		delete(s.indexedDocs, event.Document.ID)
		s.mu.Unlock()

		slog.Debug("Deleted document on change", "document", event.Document.ID)

	case DocumentEventError:
		slog.Warn("Document source error", "error", event.Error)
	}
}

// StopWatching stops watching for changes.
func (s *DocumentStore) StopWatching() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.watchCancel != nil {
		s.watchCancel()
		s.watchCancel = nil
	}

	if s.watcher != nil {
		if err := s.watcher.Stop(); err != nil {
			slog.Warn("Error stopping file watcher", "store", s.name, "error", err)
		}
		s.watcher = nil
	}

	slog.Info("Stopped watching for document changes", "store", s.name)
}

// Clear removes all indexed documents.
func (s *DocumentStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	s.indexedDocs = make(map[string]time.Time)
	s.mu.Unlock()

	return s.engine.Clear(ctx)
}

// RegisterExtractor adds a custom content extractor.
func (s *DocumentStore) RegisterExtractor(e ContentExtractor) {
	s.extractor.Register(e)
}

// Close stops watching and releases resources.
func (s *DocumentStore) Close() error {
	s.StopWatching()
	return s.source.Close()
}

// Stats returns indexing statistics.
func (s *DocumentStore) Stats() DocumentStoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metricsSnapshot := s.metrics.Snapshot()

	return DocumentStoreStats{
		Name:          s.name,
		Collection:    s.collection,
		IndexedCount:  len(s.indexedDocs),
		WatchEnabled:  s.watchEnabled,
		SourceType:    s.source.Type(),
		TotalDocs:     metricsSnapshot.TotalDocs,
		SkippedDocs:   metricsSnapshot.SkippedDocs,
		ErrorDocs:     metricsSnapshot.ErrorDocs,
		DocsPerSecond: metricsSnapshot.DocsPerSecond,
		SearchCount:   metricsSnapshot.SearchCount,
	}
}

// Metrics returns detailed indexing metrics.
func (s *DocumentStore) Metrics() IndexMetricsSnapshot {
	return s.metrics.Snapshot()
}

// DocumentStoreStats contains store statistics.
type DocumentStoreStats struct {
	Name          string  `json:"name"`
	Collection    string  `json:"collection"`
	IndexedCount  int     `json:"indexed_count"`
	WatchEnabled  bool    `json:"watch_enabled"`
	SourceType    string  `json:"source_type"`
	TotalDocs     int64   `json:"total_docs"`
	SkippedDocs   int64   `json:"skipped_docs"`
	ErrorDocs     int64   `json:"error_docs"`
	DocsPerSecond float64 `json:"docs_per_second"`
	SearchCount   int64   `json:"search_count"`
}

// GetDocument retrieves a specific document by ID.
//
// Direct port from legacy pkg/context/document_store.go
func (s *DocumentStore) GetDocument(ctx context.Context, id string) (*SearchResult, error) {
	// Search for the specific document
	results, err := s.engine.Search(ctx, SearchRequest{
		Query:      id,
		Collection: s.collection,
		TopK:       1,
		Filter:     map[string]any{"id": id},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	if len(results.Results) == 0 {
		return nil, nil
	}

	return &results.Results[0], nil
}

// RefreshDocument re-indexes a single document by path.
//
// Direct port from legacy pkg/context/document_store.go
func (s *DocumentStore) RefreshDocument(ctx context.Context, docID string) error {
	// For directory sources, discover the specific document
	if dirSource, ok := s.source.(*DirectorySource); ok {
		// Get the base path and find the file
		basePath := dirSource.GetBasePath()
		fullPath := basePath + "/" + docID

		// Create a document from the file
		doc := Document{
			ID:         docID,
			SourcePath: fullPath,
			Metadata:   make(map[string]any),
		}

		// Extract and index
		extracted, err := s.extractor.Extract(ctx, doc)
		if err != nil {
			return fmt.Errorf("failed to extract content: %w", err)
		}

		if extracted.Content != "" {
			doc.Content = extracted.Content
		}
		if extracted.Title != "" && doc.Title == "" {
			doc.Title = extracted.Title
		}
		doc.Metadata["collection"] = s.collection

		if err := s.engine.IngestDocument(ctx, doc); err != nil {
			return fmt.Errorf("failed to re-index document: %w", err)
		}

		// Update indexed timestamp
		s.mu.Lock()
		s.indexedDocs[docID] = time.Now()
		s.mu.Unlock()

		slog.Info("Refreshed document", "store", s.name, "document", docID)
		return nil
	}

	return fmt.Errorf("RefreshDocument only supported for directory sources")
}

// GetSearchEngine returns the underlying search engine.
//
// Direct port from legacy pkg/context/document_store.go
func (s *DocumentStore) GetSearchEngine() *SearchEngine {
	return s.engine
}

// cleanupDeletedFiles removes indexed documents that no longer exist in the source.
//
// Direct port from legacy pkg/context/document_store.go (cleanupDeletedFiles)
func (s *DocumentStore) cleanupDeletedFiles(ctx context.Context, foundDocs map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deletedCount := 0
	for docID := range s.indexedDocs {
		if !foundDocs[docID] {
			// Document no longer exists in source, delete from index
			if err := s.engine.DeleteDocument(ctx, docID); err != nil {
				slog.Warn("Failed to delete stale document",
					"document", docID,
					"error", err)
				continue
			}
			delete(s.indexedDocs, docID)
			deletedCount++
		}
	}

	if deletedCount > 0 {
		slog.Info("Cleaned up deleted files",
			"store", s.name,
			"deleted_count", deletedCount)
	}
}
