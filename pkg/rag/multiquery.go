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
	"strings"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/model"
)

// MultiQueryExpander generates multiple query variants for better recall.
//
// Multi-query retrieval improves recall by:
//   - Generating alternative phrasings of the query
//   - Searching with each variant
//   - Combining and deduplicating results
//
// This helps when:
//   - Queries are ambiguous
//   - Relevant documents use different terminology
//   - Users don't know exact terms used in documents
//
// Derived from legacy pkg/context/multi_query.go
type MultiQueryExpander struct {
	llm        model.LLM
	numQueries int
}

// NewMultiQueryExpander creates a new multi-query expander.
func NewMultiQueryExpander(llm model.LLM, numQueries int) *MultiQueryExpander {
	if numQueries <= 0 {
		numQueries = 3
	}
	return &MultiQueryExpander{
		llm:        llm,
		numQueries: numQueries,
	}
}

// ExpandQuery generates multiple query variants.
func (m *MultiQueryExpander) ExpandQuery(ctx context.Context, query string) ([]string, error) {
	if m.llm == nil {
		return []string{query}, fmt.Errorf("LLM is required for multi-query expansion")
	}

	sanitizedQuery := sanitizeInput(query)

	prompt := fmt.Sprintf(`Generate %d alternative versions of the following search query. 
Each alternative should:
- Search for the same information but with different wording
- Use synonyms or related terms
- Rephrase the question from different angles

Original query: "%s"

Respond with only the alternative queries, one per line, without numbering or bullets.`, m.numQueries, sanitizedQuery)

	temp := 0.7
	maxTokens := 200
	request := &model.Request{
		Messages: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: prompt}),
		},
		Config: &model.GenerateConfig{
			Temperature: &temp, // Allow creativity
			MaxTokens:   &maxTokens,
		},
	}

	var response string
	for resp, err := range m.llm.GenerateContent(ctx, request, false) {
		if err != nil {
			slog.Warn("Multi-query expansion failed", "error", err)
			return []string{query}, nil
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if tp, ok := part.(a2a.TextPart); ok {
					response += tp.Text
				}
			}
		}
	}

	// Parse response into queries
	queries := m.parseQueries(response, query)

	slog.Debug("Expanded query",
		"original", query,
		"variants", len(queries))

	return queries, nil
}

// parseQueries extracts query variants from LLM response.
func (m *MultiQueryExpander) parseQueries(response, original string) []string {
	// Always include the original query
	queries := []string{original}
	seen := map[string]bool{strings.ToLower(original): true}

	// Parse lines from response
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		// Clean up the line
		query := strings.TrimSpace(line)

		// Remove common prefixes
		prefixes := []string{"-", "•", "*", "1.", "2.", "3.", "4.", "5."}
		for _, prefix := range prefixes {
			query = strings.TrimPrefix(query, prefix)
		}
		query = strings.TrimSpace(query)

		// Remove quotes
		query = strings.Trim(query, `"'`)

		// Skip empty or duplicate queries
		if query == "" {
			continue
		}
		if seen[strings.ToLower(query)] {
			continue
		}

		queries = append(queries, query)
		seen[strings.ToLower(query)] = true

		// Limit to requested number
		if len(queries) >= m.numQueries+1 { // +1 for original
			break
		}
	}

	return queries
}

// CombineResults merges results from multiple queries.
//
// Deduplicates by document ID and keeps the highest score for each.
func CombineResults(resultSets [][]SearchResult) []SearchResult {
	if len(resultSets) == 0 {
		return nil
	}

	// Track best score for each unique result
	bestScores := make(map[string]SearchResult)

	for _, results := range resultSets {
		for _, result := range results {
			key := result.ID
			if existing, ok := bestScores[key]; !ok || result.Score > existing.Score {
				bestScores[key] = result
			}
		}
	}

	// Convert to slice and sort by score
	combined := make([]SearchResult, 0, len(bestScores))
	for _, result := range bestScores {
		combined = append(combined, result)
	}

	// Sort by score (highest first)
	sortResultsByScore(combined)

	return combined
}

// sortResultsByScore sorts results by score in descending order.
func sortResultsByScore(results []SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// NilMultiQueryExpander returns the original query unchanged.
type NilMultiQueryExpander struct{}

func (NilMultiQueryExpander) ExpandQuery(ctx context.Context, query string) ([]string, error) {
	return []string{query}, nil
}
