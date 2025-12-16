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

// Package searchtool provides a search tool for agents to query document stores.
//
// The search tool enables agents to perform semantic search across configured
// document stores, supporting features like:
//   - Scoped access (agent can only search assigned stores)
//   - Multiple store search with result aggregation
//   - Rich result metadata including source attribution
//
// Derived from legacy pkg/tools/search.go
package searchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/verikod/hector/pkg/rag"
	"github.com/verikod/hector/pkg/tool"
)

// SearchTool allows agents to search document stores.
//
// Key features:
//   - Scoped to specific document stores per agent
//   - Aggregates results from multiple stores
//   - Returns rich metadata for source attribution
//
// Derived from legacy pkg/tools/search.go:SearchTool
type SearchTool struct {
	stores          map[string]*rag.DocumentStore
	availableStores []string // Store names this agent can access (empty = all)
	maxLimit        int
	defaultLimit    int
}

// Config configures the search tool.
type Config struct {
	// Stores maps store names to document stores.
	Stores map[string]*rag.DocumentStore

	// AvailableStores limits which stores this agent can search.
	// Empty means all stores are available.
	AvailableStores []string

	// MaxLimit is the maximum results per search (safety limit).
	// Default: 50
	MaxLimit int

	// DefaultLimit is the default results when limit not specified.
	// Default: 10
	DefaultLimit int
}

// New creates a new search tool.
func New(cfg Config) *SearchTool {
	if cfg.MaxLimit <= 0 {
		cfg.MaxLimit = 50
	}
	if cfg.DefaultLimit <= 0 {
		cfg.DefaultLimit = 10
	}
	if cfg.Stores == nil {
		cfg.Stores = make(map[string]*rag.DocumentStore)
	}

	t := &SearchTool{
		stores:          cfg.Stores,
		availableStores: cfg.AvailableStores,
		maxLimit:        cfg.MaxLimit,
		defaultLimit:    cfg.DefaultLimit,
	}

	return t
}

// Name returns the tool name.
func (t *SearchTool) Name() string {
	return "search"
}

// Description returns the tool description with current store stats.
func (t *SearchTool) Description() string {
	return t.buildDescription()
}

// IsLongRunning returns false - search is quick.
func (t *SearchTool) IsLongRunning() bool {
	return false
}

// RequiresApproval returns false - search is read-only.
func (t *SearchTool) RequiresApproval() bool {
	return false
}

// Schema returns the JSON schema for parameters.
func (t *SearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query to find relevant documents",
			},
			"stores": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"description": "Specific stores to search (empty searches all available stores)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum number of results to return (default: %d, max: %d)", t.defaultLimit, t.maxLimit),
			},
		},
		"required": []string{"query"},
	}
}

// Call executes the search.
func (t *SearchTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	// Parse arguments
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	// Parse limit
	limit := t.defaultLimit
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	} else if l, ok := args["limit"].(int); ok {
		limit = l
	}
	if limit <= 0 {
		limit = t.defaultLimit
	}
	if limit > t.maxLimit {
		limit = t.maxLimit
	}

	// Parse stores
	var requestedStores []string
	if stores, ok := args["stores"]; ok {
		switch v := stores.(type) {
		case []any:
			for _, s := range v {
				if str, ok := s.(string); ok {
					requestedStores = append(requestedStores, str)
				}
			}
		case []string:
			requestedStores = v
		case string:
			if v != "" {
				requestedStores = []string{v}
			}
		}
	}

	// Perform search
	response, err := t.performSearch(ctx, query, requestedStores, limit)
	if err != nil {
		return nil, err
	}

	// Return as map
	data, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}

// SearchResponse is returned to the agent.
type SearchResponse struct {
	Results    []SearchResult `json:"results"`
	Total      int            `json:"total"`
	Query      string         `json:"query"`
	Duration   string         `json:"duration"`
	StoresUsed []string       `json:"stores_used"`
}

// SearchResult represents a single search result.
type SearchResult struct {
	DocumentID string         `json:"document_id"`
	StoreName  string         `json:"store_name"`
	Content    string         `json:"content"`
	Score      float32        `json:"score"`
	ChunkIndex int            `json:"chunk_index,omitempty"`
	SourcePath string         `json:"source_path,omitempty"`
	Title      string         `json:"title,omitempty"`
	StartLine  int            `json:"start_line,omitempty"`
	EndLine    int            `json:"end_line,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// performSearch executes the search across stores.
func (t *SearchTool) performSearch(ctx context.Context, query string, requestedStores []string, limit int) (*SearchResponse, error) {
	start := time.Now()

	// Get stores to search
	storesToSearch := t.getStoresToSearch(requestedStores)
	if len(storesToSearch) == 0 {
		return nil, fmt.Errorf("no document stores available")
	}

	// Search each store
	var allResults []SearchResult
	var storesUsed []string

	for storeName, store := range storesToSearch {
		// Add timeout per store
		searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		results, err := store.Search(searchCtx, rag.SearchRequest{
			Query: query,
			TopK:  limit,
		})
		cancel()

		if err != nil {
			slog.Warn("Search failed for store",
				"store", storeName,
				"error", err)
			continue
		}

		storesUsed = append(storesUsed, storeName)

		// Convert results
		for _, r := range results.Results {
			result := SearchResult{
				DocumentID: r.DocumentID,
				StoreName:  storeName,
				Content:    r.Content,
				Score:      r.Score,
				ChunkIndex: r.ChunkIndex,
				Metadata:   r.Metadata,
			}

			// Extract common metadata fields
			if r.Metadata != nil {
				if path, ok := r.Metadata["source_path"].(string); ok {
					result.SourcePath = path
				}
				if title, ok := r.Metadata["title"].(string); ok {
					result.Title = title
				}
				if startLine, ok := r.Metadata["start_line"].(int); ok {
					result.StartLine = startLine
				} else if startLine, ok := r.Metadata["start_line"].(float64); ok {
					result.StartLine = int(startLine)
				}
				if endLine, ok := r.Metadata["end_line"].(int); ok {
					result.EndLine = endLine
				} else if endLine, ok := r.Metadata["end_line"].(float64); ok {
					result.EndLine = int(endLine)
				}
			}

			allResults = append(allResults, result)
		}
	}

	// Sort by score (highest first)
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	// Apply limit
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	return &SearchResponse{
		Results:    allResults,
		Total:      len(allResults),
		Query:      query,
		Duration:   time.Since(start).String(),
		StoresUsed: storesUsed,
	}, nil
}

// getStoresToSearch returns the stores to search based on configuration and request.
func (t *SearchTool) getStoresToSearch(requestedStores []string) map[string]*rag.DocumentStore {
	result := make(map[string]*rag.DocumentStore)

	// Get available stores for this agent
	availableStores := t.getAvailableStores()

	// If specific stores requested, filter to those
	if len(requestedStores) > 0 {
		for _, name := range requestedStores {
			if store, ok := availableStores[name]; ok {
				result[name] = store
			}
		}
	} else {
		// Search all available stores
		result = availableStores
	}

	return result
}

// getAvailableStores returns stores this agent can access.
func (t *SearchTool) getAvailableStores() map[string]*rag.DocumentStore {
	// If no restrictions, return all stores
	if len(t.availableStores) == 0 {
		return t.stores
	}

	// Filter to allowed stores
	result := make(map[string]*rag.DocumentStore)
	for _, name := range t.availableStores {
		if store, ok := t.stores[name]; ok {
			result[name] = store
		}
	}
	return result
}

// buildDescription creates descriptions for available stores.
func (t *SearchTool) buildDescription() string {
	base := "Search knowledgeable document stores (RAG). Use this to find information, concepts, or code snippets when you don't know the exact file location."

	stores := t.getAvailableStores()
	if len(stores) == 0 {
		return base
	}

	var descriptions []string
	for name, store := range stores {
		stats := store.Stats()
		desc := fmt.Sprintf("- %s: %s source, %d documents indexed",
			name, stats.SourceType, stats.IndexedCount)
		descriptions = append(descriptions, desc)
	}

	sort.Strings(descriptions)
	return base + "\n\nAvailable stores:\n" + strings.Join(descriptions, "\n")
}

// RegisterStore adds a document store.
func (t *SearchTool) RegisterStore(name string, store *rag.DocumentStore) {
	if t.stores == nil {
		t.stores = make(map[string]*rag.DocumentStore)
	}
	t.stores[name] = store
}

// Ensure SearchTool implements tool.CallableTool interface.
var _ tool.CallableTool = (*SearchTool)(nil)
