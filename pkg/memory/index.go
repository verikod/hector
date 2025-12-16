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
	"time"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/session"
)

// SearchRequest represents a request for memory search.
type SearchRequest struct {
	// Query is the search query (natural language or keywords).
	Query string

	// UserID scopes the search to a specific user's memories.
	UserID string

	// AppName scopes the search to a specific application.
	AppName string
}

// SearchResponse represents the response from a memory search.
type SearchResponse struct {
	// Results contains the matching memory entries.
	Results []SearchResult
}

// SearchResult represents a single memory search result.
type SearchResult struct {
	// SessionID identifies which session this memory came from.
	SessionID string

	// EventID identifies the specific event within the session.
	EventID string

	// Content is the text content of the memory.
	Content string

	// Author identifies who created this content (agent name or "user").
	Author string

	// Timestamp indicates when this memory was created.
	Timestamp time.Time

	// Score represents the relevance score (higher is better).
	// For keyword search: number of matching words.
	// For semantic search: cosine similarity.
	Score float64

	// Metadata contains additional context about the memory.
	Metadata map[string]any
}

// Entry represents a memory entry stored in the index.
type Entry struct {
	SessionID string
	EventID   string
	AppName   string
	UserID    string
	Author    string
	Content   string
	Timestamp time.Time
	Words     map[string]struct{} // Pre-computed word index for keyword search
	Metadata  map[string]any
}

// IndexService provides semantic search over session data.
//
// This follows the legacy Hector pattern where:
//   - session.Service is the SOURCE OF TRUTH (stores all data in SQL)
//   - IndexService is a SEARCH INDEX (can be rebuilt from session.Service)
//
// The index is populated after each turn and can be rebuilt on startup
// if the index is corrupted or needs to be migrated.
//
// This architecture ensures:
//   - No data loss (SQL is the source of truth)
//   - Fast semantic search (vector index)
//   - Rebuild capability (index from session.Service)
//
// Derived from legacy pkg/memory/longterm_strategy.go:
//
//	type LongTermMemoryStrategy interface {
//	    Store(agentID, sessionID string, messages []*pb.Message) error
//	    Recall(agentID, sessionID, query string, limit int) ([]*pb.Message, error)
//	    Clear(agentID, sessionID string) error
//	    Name() string
//	}
type IndexService interface {
	// Index adds session events to the semantic search index.
	//
	// This is called after each turn completes. The session data is already
	// persisted in session.Service (SQL) - this method just builds the
	// search index for fast retrieval.
	//
	// Implementation note: This should be idempotent - calling Index
	// multiple times with the same session should produce the same result.
	Index(ctx context.Context, sess agent.Session) error

	// Search performs semantic similarity search over indexed sessions.
	//
	// The search is scoped to (app_name, user_id) to ensure isolation.
	// Returns results ordered by relevance score (highest first).
	//
	// For vector-based implementations, this uses cosine similarity.
	// For keyword-based implementations, this uses word matching.
	Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error)

	// Rebuild repopulates the entire index from session.Service.
	//
	// This is called:
	//   - On startup when index persistence is disabled
	//   - When the index file is corrupted
	//   - When migrating to a new index format
	//
	// The rebuild process:
	//   1. Clear existing index entries for (app_name, user_id)
	//   2. Load all sessions from session.Service
	//   3. Index each session
	//
	// This can be expensive for large datasets but ensures consistency.
	Rebuild(ctx context.Context, sessions session.Service, appName, userID string) error

	// Clear removes all index entries for a specific session.
	//
	// Called when a session is deleted from session.Service.
	Clear(ctx context.Context, appName, userID, sessionID string) error

	// Name returns the index implementation name (e.g., "chromem", "keyword").
	Name() string
}

// NilIndexService is a no-op implementation for when indexing is disabled.
type NilIndexService struct{}

func (NilIndexService) Index(ctx context.Context, sess agent.Session) error {
	return nil
}

func (NilIndexService) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	return &SearchResponse{Results: []SearchResult{}}, nil
}

func (NilIndexService) Rebuild(ctx context.Context, sessions session.Service, appName, userID string) error {
	return nil
}

func (NilIndexService) Clear(ctx context.Context, appName, userID, sessionID string) error {
	return nil
}

func (NilIndexService) Name() string {
	return "nil"
}

// Ensure NilIndexService implements IndexService.
var _ IndexService = NilIndexService{}
