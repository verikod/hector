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
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/model"
)

// Reranker re-ranks search results using an LLM.
//
// Reranking improves search quality by:
//   - Using deeper semantic understanding than vector similarity
//   - Evaluating actual relevance to the query
//   - Considering context that embeddings might miss
//
// Trade-offs:
//   - Adds latency (100-500ms per search)
//   - Incurs LLM API costs
//   - Only practical for small result sets (10-20 items)
//
// Derived from legacy pkg/context/reranking/reranker.go
type Reranker struct {
	llm        model.LLM
	maxResults int
}

// RerankConfig configures the reranker.
type RerankResult struct {
	// Results are the reranked search results.
	Results []SearchResult

	// Rankings contains the LLM's ranking decisions.
	Rankings []RankingDecision
}

// RankingDecision represents the LLM's ranking for a single result.
type RankingDecision struct {
	// Index is the original result index.
	Index int `json:"index"`

	// Relevance is the LLM-assigned relevance score (1-10).
	Relevance int `json:"relevance"`

	// Reason explains why this ranking was assigned.
	Reason string `json:"reason,omitempty"`
}

// NewReranker creates a new reranker.
func NewReranker(llm model.LLM, maxResults int) *Reranker {
	if maxResults <= 0 {
		maxResults = 20
	}
	return &Reranker{
		llm:        llm,
		maxResults: maxResults,
	}
}

// Rerank re-orders results based on LLM assessment.
//
// The process:
//  1. Format results and query for the LLM
//  2. Ask LLM to rank results by relevance
//  3. Parse LLM response and reorder results
//  4. Assign new scores based on ranking position
//
// After reranking:
//   - Scores are position-based (1st=1.0, 2nd=0.95, etc.)
//   - Original vector similarity scores are replaced
func (r *Reranker) Rerank(ctx context.Context, query string, results []SearchResult) (*RerankResult, error) {
	if r.llm == nil {
		return nil, fmt.Errorf("LLM is required for reranking")
	}

	if len(results) == 0 {
		return &RerankResult{Results: results}, nil
	}

	// Limit results to rerank
	toRerank := results
	if len(toRerank) > r.maxResults {
		toRerank = toRerank[:r.maxResults]
	}

	// Build prompt
	prompt := r.buildRerankPrompt(query, toRerank)

	temp := 0.0
	request := &model.Request{
		Messages: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: prompt}),
		},
		Config: &model.GenerateConfig{
			Temperature: &temp, // Deterministic ranking
		},
	}

	var response string
	for resp, err := range r.llm.GenerateContent(ctx, request, false) {
		if err != nil {
			slog.Warn("Reranking failed, returning original order", "error", err)
			return &RerankResult{Results: results}, nil
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if tp, ok := part.(a2a.TextPart); ok {
					response += tp.Text
				}
			}
		}
	}

	// Parse rankings
	rankings, err := r.parseRankings(response, len(toRerank))
	if err != nil {
		slog.Warn("Failed to parse rankings, returning original order", "error", err)
		return &RerankResult{Results: results}, nil
	}

	// Reorder results
	reranked := r.applyRankings(toRerank, rankings)

	// Append any results that weren't reranked
	if len(results) > r.maxResults {
		reranked = append(reranked, results[r.maxResults:]...)
	}

	slog.Debug("Reranked search results",
		"query", query,
		"original_count", len(results),
		"reranked_count", len(toRerank))

	return &RerankResult{
		Results:  reranked,
		Rankings: rankings,
	}, nil
}

// buildRerankPrompt creates the prompt for reranking.
func (r *Reranker) buildRerankPrompt(query string, results []SearchResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(`Given the query: "%s"

Rank the following documents by their relevance to the query.
For each document, provide a relevance score from 1-10 (10 being most relevant).

Documents:
`, sanitizeInput(query)))

	for i, result := range results {
		content := truncateString(result.Content, 500)
		sb.WriteString(fmt.Sprintf("\n[%d] %s\n", i, content))
	}

	sb.WriteString(`

Respond with a JSON array of rankings, ordered from most to least relevant:
[{"index": 0, "relevance": 9, "reason": "directly answers the query"}, ...]

Only include the JSON array, no other text.`)

	return sb.String()
}

// parseRankings extracts ranking decisions from LLM response.
func (r *Reranker) parseRankings(response string, numResults int) ([]RankingDecision, error) {
	// Find JSON array in response
	start := strings.Index(response, "[")
	end := strings.LastIndex(response, "]")
	if start == -1 || end == -1 || start >= end {
		return nil, fmt.Errorf("no JSON array found in response")
	}

	jsonStr := response[start : end+1]

	var rankings []RankingDecision
	if err := json.Unmarshal([]byte(jsonStr), &rankings); err != nil {
		return nil, fmt.Errorf("failed to parse rankings JSON: %w", err)
	}

	// Validate rankings
	seen := make(map[int]bool)
	var valid []RankingDecision
	for _, ranking := range rankings {
		if ranking.Index >= 0 && ranking.Index < numResults && !seen[ranking.Index] {
			seen[ranking.Index] = true
			valid = append(valid, ranking)
		}
	}

	// Add any missing indices with low relevance
	for i := 0; i < numResults; i++ {
		if !seen[i] {
			valid = append(valid, RankingDecision{
				Index:     i,
				Relevance: 1,
			})
		}
	}

	// Sort by relevance (highest first)
	sort.Slice(valid, func(i, j int) bool {
		return valid[i].Relevance > valid[j].Relevance
	})

	return valid, nil
}

// applyRankings reorders results based on rankings.
func (r *Reranker) applyRankings(results []SearchResult, rankings []RankingDecision) []SearchResult {
	reranked := make([]SearchResult, len(rankings))

	for i, ranking := range rankings {
		if ranking.Index < len(results) {
			reranked[i] = results[ranking.Index]
			// Update score based on position (1st=1.0, 2nd=0.95, etc.)
			reranked[i].Score = 1.0 - float32(i)*0.05
			if reranked[i].Score < 0.1 {
				reranked[i].Score = 0.1
			}
		}
	}

	return reranked
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// NilReranker returns results unchanged.
type NilReranker struct{}

func (NilReranker) Rerank(ctx context.Context, query string, results []SearchResult) (*RerankResult, error) {
	return &RerankResult{Results: results}, nil
}
