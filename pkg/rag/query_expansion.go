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
	"strings"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/model"
)

// QueryExpander expands a single query into multiple query variations.
//
// Direct port from legacy pkg/context/query_expansion.go
type QueryExpander interface {
	// Expand generates multiple query variations from the original query.
	Expand(ctx context.Context, query string, numVariations int) ([]string, error)
}

// LLMQueryExpander uses an LLM to generate query variations.
//
// Direct port from legacy pkg/context/query_expansion.go
type LLMQueryExpander struct {
	llm model.LLM
}

// NewLLMQueryExpander creates a new LLM-based query expander.
func NewLLMQueryExpander(llm model.LLM) *LLMQueryExpander {
	return &LLMQueryExpander{
		llm: llm,
	}
}

// Expand implements the QueryExpander interface.
//
// Direct port from legacy pkg/context/query_expansion.go
func (e *LLMQueryExpander) Expand(ctx context.Context, query string, numVariations int) ([]string, error) {
	if numVariations <= 0 {
		numVariations = 3 // Default: generate 3 variations
	}
	if numVariations > 5 {
		numVariations = 5 // Cap at 5 variations to avoid too many API calls
	}

	// Sanitize query to prevent prompt injection
	sanitizedQuery := sanitizeInput(query)

	prompt := fmt.Sprintf(`Generate %d different query variations for the following search query. Each variation should:
1. Use different wording or phrasing
2. Focus on different aspects or perspectives
3. Be semantically similar but not identical
4. Be suitable for document retrieval

Original query: %s

Return only a JSON array of query strings, one per line, without any additional text or explanation.
Example format: ["query 1", "query 2", "query 3"]`, numVariations, sanitizedQuery)

	temp := 0.7
	maxTokens := 200
	request := &model.Request{
		Messages: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: prompt}),
		},
		Config: &model.GenerateConfig{
			Temperature: &temp,
			MaxTokens:   &maxTokens,
		},
	}

	var response string
	for resp, err := range e.llm.GenerateContent(ctx, request, false) {
		if err != nil {
			return nil, fmt.Errorf("failed to generate query variations: %w", err)
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if tp, ok := part.(a2a.TextPart); ok {
					response += tp.Text
				}
			}
		}
	}

	// Parse JSON array from response
	queries, err := parseQueryArray(response)
	if err != nil {
		// Fallback: try to extract queries manually
		queries = extractQueriesFromText(response)
	}

	// Ensure we have at least the original query
	if len(queries) == 0 {
		queries = []string{query}
	}

	// Limit to requested number
	if len(queries) > numVariations {
		queries = queries[:numVariations]
	}

	return queries, nil
}

// parseQueryArray parses a JSON array of query strings.
//
// Direct port from legacy pkg/context/query_expansion.go
func parseQueryArray(response string) ([]string, error) {
	// Find JSON array in response
	startIdx := -1
	endIdx := -1
	depth := 0

	for i, char := range response {
		if char == '[' {
			if startIdx == -1 {
				startIdx = i
			}
			depth++
		} else if char == ']' {
			depth--
			if depth == 0 && startIdx != -1 {
				endIdx = i + 1
				break
			}
		}
	}

	if startIdx == -1 || endIdx == -1 {
		return nil, fmt.Errorf("no JSON array found")
	}

	jsonStr := response[startIdx:endIdx]

	// Simple JSON parsing for string array
	// Remove brackets and quotes
	jsonStr = jsonStr[1 : len(jsonStr)-1] // Remove [ and ]

	var queries []string
	var current strings.Builder
	inQuotes := false
	escape := false

	for _, char := range jsonStr {
		if escape {
			current.WriteRune(char)
			escape = false
			continue
		}

		if char == '\\' {
			escape = true
			continue
		}

		if char == '"' {
			if inQuotes {
				// End of string
				queries = append(queries, current.String())
				current.Reset()
			}
			inQuotes = !inQuotes
			continue
		}

		if inQuotes {
			current.WriteRune(char)
		}
	}

	if len(queries) == 0 {
		return nil, fmt.Errorf("failed to parse queries")
	}

	return queries, nil
}

// extractQueriesFromText tries to extract queries from unstructured text.
//
// Direct port from legacy pkg/context/query_expansion.go
func extractQueriesFromText(response string) []string {
	var queries []string
	lines := strings.Split(response, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for quoted strings
		if strings.HasPrefix(line, `"`) && strings.HasSuffix(line, `"`) {
			query := line[1 : len(line)-1] // Remove quotes
			if len(query) > 0 {
				queries = append(queries, query)
			}
		} else if strings.HasPrefix(line, `'`) && strings.HasSuffix(line, `'`) {
			query := line[1 : len(line)-1] // Remove quotes
			if len(query) > 0 {
				queries = append(queries, query)
			}
		} else if len(line) > 10 && !strings.Contains(line, ":") {
			// Might be a query without quotes
			queries = append(queries, line)
		}
	}

	return queries
}

// NilQueryExpander returns the original query unchanged.
type NilQueryExpander struct{}

func (NilQueryExpander) Expand(ctx context.Context, query string, numVariations int) ([]string, error) {
	return []string{query}, nil
}

// Ensure implementations satisfy interface.
var _ QueryExpander = (*LLMQueryExpander)(nil)
var _ QueryExpander = NilQueryExpander{}
