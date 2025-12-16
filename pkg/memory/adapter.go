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

	"github.com/verikod/hector/pkg/agent"
)

// SearchableService is implemented by services that support Search.
// IndexService implements this interface.
type SearchableService interface {
	Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error)
}

// Adapter wraps an IndexService to implement agent.Memory.
//
// The adapter is scoped to a specific user/app context, allowing the
// Search method to automatically scope queries without requiring
// callers to provide user/app info on every call.
//
// Architecture note:
//   - IndexService is a SEARCH INDEX (not storage)
//   - Session data is stored in session.Service (the source of truth)
//   - The adapter exposes search capabilities to agents
//
// Usage:
//
//	indexSvc := memory.NewKeywordIndexService()
//	adapter := memory.NewAdapter(indexSvc, "my-app", "user-123")
//
//	// Now use adapter as agent.Memory
//	results, err := adapter.Search(ctx, "what is the user's favorite color?")
type Adapter struct {
	svc     SearchableService
	appName string
	userID  string
}

// NewAdapter creates an agent.Memory adapter for the given SearchableService.
//
// The appName and userID scope all operations to a specific user context.
// This is typically created per-invocation using session metadata.
func NewAdapter(svc SearchableService, appName, userID string) *Adapter {
	return &Adapter{
		svc:     svc,
		appName: appName,
		userID:  userID,
	}
}

// AddSession is a no-op for the adapter.
//
// Session indexing is handled by the runner calling IndexService.Index()
// after each turn. The adapter only exposes search capabilities to agents.
func (a *Adapter) AddSession(ctx context.Context, session agent.Session) error {
	// No-op: indexing is handled by runner.indexSession()
	// Session data is already persisted via session.Service
	return nil
}

// Search returns memory entries relevant to the given query.
//
// The search is automatically scoped to the adapter's appName and userID.
// Results are converted from memory.SearchResult to agent.MemoryResult.
func (a *Adapter) Search(ctx context.Context, query string) (*agent.MemorySearchResponse, error) {
	if a.svc == nil {
		return &agent.MemorySearchResponse{}, nil
	}

	resp, err := a.svc.Search(ctx, &SearchRequest{
		Query:   query,
		AppName: a.appName,
		UserID:  a.userID,
	})
	if err != nil {
		return nil, err
	}

	// Convert results
	results := make([]agent.MemoryResult, len(resp.Results))
	for i, r := range resp.Results {
		// Enrich metadata with memory-specific fields
		metadata := r.Metadata
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["session_id"] = r.SessionID
		metadata["event_id"] = r.EventID
		metadata["author"] = r.Author
		metadata["timestamp"] = r.Timestamp

		results[i] = agent.MemoryResult{
			Content:  r.Content,
			Score:    r.Score,
			Metadata: metadata,
		}
	}

	return &agent.MemorySearchResponse{Results: results}, nil
}

// NilMemory returns a no-op memory implementation.
//
// Use this when memory is not configured to avoid nil checks.
// All operations succeed but do nothing.
func NilMemory() agent.Memory {
	return nilMemory{}
}

type nilMemory struct{}

func (nilMemory) AddSession(context.Context, agent.Session) error {
	return nil
}

func (nilMemory) Search(context.Context, string) (*agent.MemorySearchResponse, error) {
	return &agent.MemorySearchResponse{}, nil
}

// Compile-time interface checks
var (
	_ agent.Memory = (*Adapter)(nil)
	_ agent.Memory = nilMemory{}
)
