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

package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/embedder"
	"github.com/verikod/hector/pkg/session"
	"github.com/verikod/hector/pkg/vector"
)

// VectorIndexService provides semantic vector search using the vector.Provider abstraction.
//
// This implementation uses the unified vector.Provider interface, allowing
// different backends (chromem-go, Qdrant, etc.) to be used interchangeably.
//
// Architecture:
//
//	session.Service (SQL) → SOURCE OF TRUTH
//	     ↓
//	VectorIndexService → SEARCH INDEX
//	     │
//	     ├── vector.Provider (chromem, qdrant, etc.)
//	     └── embedder.Embedder (OpenAI, Ollama)
type VectorIndexService struct {
	provider       vector.Provider
	embedder       embedder.Embedder
	collectionName string
}

// VectorIndexConfig configures the vector index service.
type VectorIndexConfig struct {
	// Provider for vector storage and search (required).
	Provider vector.Provider

	// Embedder for generating vector embeddings (required).
	Embedder embedder.Embedder

	// CollectionName for storing memory entries (optional).
	// Default: "hector_memory"
	CollectionName string
}

// NewVectorIndexService creates a new vector-based index service.
func NewVectorIndexService(cfg VectorIndexConfig) (*VectorIndexService, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("vector provider is required")
	}
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("embedder is required for vector index")
	}

	collectionName := cfg.CollectionName
	if collectionName == "" {
		collectionName = "hector_memory"
	}

	slog.Info("Created vector index service",
		"provider", cfg.Provider.Name(),
		"collection", collectionName)

	return &VectorIndexService{
		provider:       cfg.Provider,
		embedder:       cfg.Embedder,
		collectionName: collectionName,
	}, nil
}

// Index adds session events to the vector index.
func (s *VectorIndexService) Index(ctx context.Context, sess agent.Session) error {
	if sess == nil {
		return nil
	}

	indexed := 0
	for ev := range sess.Events().All() {
		if ev.Message == nil {
			continue
		}

		text := extractTextFromA2AMessage(ev.Message)
		if text == "" {
			continue
		}

		// Generate embedding
		embedding, err := s.embedder.Embed(ctx, text)
		if err != nil {
			slog.Warn("Failed to embed event",
				"event_id", ev.ID,
				"error", err)
			continue
		}

		// Document ID is composite to allow updates
		docID := fmt.Sprintf("%s:%s:%s", sess.AppName(), sess.ID(), ev.ID)

		// Prepare metadata
		metadata := map[string]any{
			"app_name":   sess.AppName(),
			"user_id":    sess.UserID(),
			"session_id": sess.ID(),
			"event_id":   ev.ID,
			"author":     ev.Author,
			"content":    text,
			"timestamp":  time.Now().Format(time.RFC3339),
		}

		// Upsert to vector store
		if err := s.provider.Upsert(ctx, s.collectionName, docID, embedding, metadata); err != nil {
			slog.Warn("Failed to upsert event to vector store",
				"event_id", ev.ID,
				"error", err)
			continue
		}

		indexed++
	}

	if indexed > 0 {
		slog.Debug("Indexed session in vector index",
			"session_id", sess.ID(),
			"documents", indexed)
	}

	return nil
}

// Search performs semantic similarity search.
func (s *VectorIndexService) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if req.Query == "" {
		return &SearchResponse{Results: []SearchResult{}}, nil
	}

	// Generate query embedding
	queryEmbedding, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Build metadata filter for user scoping
	filter := map[string]any{
		"app_name": req.AppName,
		"user_id":  req.UserID,
	}

	// Query vector store
	results, err := s.provider.SearchWithFilter(ctx, s.collectionName, queryEmbedding, 10, filter)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert to SearchResponse
	var searchResults []SearchResult
	for _, r := range results {
		content := ""
		if c, ok := r.Metadata["content"].(string); ok {
			content = c
		} else {
			content = r.Content
		}

		sessionID := ""
		if sid, ok := r.Metadata["session_id"].(string); ok {
			sessionID = sid
		}

		eventID := ""
		if eid, ok := r.Metadata["event_id"].(string); ok {
			eventID = eid
		}

		author := ""
		if a, ok := r.Metadata["author"].(string); ok {
			author = a
		}

		searchResults = append(searchResults, SearchResult{
			SessionID: sessionID,
			EventID:   eventID,
			Content:   content,
			Author:    author,
			Score:     float64(r.Score),
			Metadata:  r.Metadata,
		})
	}

	slog.Debug("Vector search completed",
		"query", req.Query,
		"results", len(searchResults))

	return &SearchResponse{Results: searchResults}, nil
}

// Rebuild repopulates the index from session.Service.
func (s *VectorIndexService) Rebuild(ctx context.Context, sessions session.Service, appName, userID string) error {
	if sessions == nil {
		return nil
	}

	slog.Info("Rebuilding vector index from session.Service",
		"app_name", appName,
		"user_id", userID)

	// Clear existing entries for this user
	if err := s.provider.DeleteByFilter(ctx, s.collectionName, map[string]any{
		"app_name": appName,
		"user_id":  userID,
	}); err != nil {
		slog.Warn("Failed to clear existing entries during rebuild", "error", err)
	}

	// List all sessions for this user
	resp, err := sessions.List(ctx, &session.ListRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	// Index each session
	indexed := 0
	for _, sess := range resp.Sessions {
		if err := s.Index(ctx, sess); err != nil {
			slog.Warn("Failed to index session during rebuild",
				"session_id", sess.ID(),
				"error", err)
			continue
		}
		indexed++
	}

	slog.Info("Vector index rebuild complete",
		"app_name", appName,
		"user_id", userID,
		"sessions_indexed", indexed)

	return nil
}

// Clear removes index entries for a specific session.
func (s *VectorIndexService) Clear(ctx context.Context, appName, userID, sessionID string) error {
	filter := map[string]any{
		"app_name":   appName,
		"user_id":    userID,
		"session_id": sessionID,
	}

	if err := s.provider.DeleteByFilter(ctx, s.collectionName, filter); err != nil {
		return fmt.Errorf("failed to clear session from index: %w", err)
	}

	slog.Debug("Cleared session from vector index", "session_id", sessionID)
	return nil
}

// Name returns the index implementation name.
func (s *VectorIndexService) Name() string {
	return "vector"
}

// Close releases resources.
func (s *VectorIndexService) Close() error {
	// Provider is managed externally (by runtime)
	return nil
}

// Ensure VectorIndexService implements IndexService.
var _ IndexService = (*VectorIndexService)(nil)
