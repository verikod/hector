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

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/model"
)

// HyDE implements Hypothetical Document Embeddings.
//
// Instead of searching with the query embedding directly, HyDE:
//  1. Uses an LLM to generate a hypothetical document that would answer the query
//  2. Embeds the hypothetical document
//  3. Uses that embedding for search
//
// This can significantly improve retrieval for questions, as the hypothetical
// document's embedding is closer to actual relevant documents than the
// query embedding.
//
// Paper: "Precise Zero-Shot Dense Retrieval without Relevance Labels"
// https://arxiv.org/abs/2212.10496
//
// Derived from legacy pkg/context/hyde.go
type HyDE struct {
	llm model.LLM
}

// NewHyDE creates a new HyDE processor.
func NewHyDE(llm model.LLM) *HyDE {
	return &HyDE{llm: llm}
}

// GenerateHypotheticalDocument generates a hypothetical document for the query.
func (h *HyDE) GenerateHypotheticalDocument(ctx context.Context, query string) (string, error) {
	if h.llm == nil {
		return "", fmt.Errorf("LLM is required for HyDE")
	}

	// Sanitize query to prevent prompt injection
	sanitizedQuery := sanitizeInput(query)

	prompt := fmt.Sprintf(`Write a concise, hypothetical document that would be highly relevant to answer the following query: "%s"

The document should:
- Be brief (1-2 paragraphs)
- Directly address the core of the query
- Sound like a real document excerpt
- Not mention that it's hypothetical

Document:`, sanitizedQuery)

	temp := 0.7
	maxTokens := 300
	request := &model.Request{
		Messages: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: prompt}),
		},
		Config: &model.GenerateConfig{
			Temperature:   &temp,
			MaxTokens:     &maxTokens,
			StopSequences: []string{"\n\nQuery:"}, // Stop if it starts a new query
		},
	}

	var result string
	for resp, err := range h.llm.GenerateContent(ctx, request, false) {
		if err != nil {
			return "", fmt.Errorf("failed to generate hypothetical document: %w", err)
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if tp, ok := part.(a2a.TextPart); ok {
					result += tp.Text
				}
			}
		}
	}

	if result == "" {
		return "", fmt.Errorf("LLM returned empty response")
	}

	slog.Debug("Generated hypothetical document",
		"query", query,
		"hypothetical_length", len(result))

	return result, nil
}

// EnhancedSearch performs HyDE-enhanced search.
//
// This is a convenience method that:
//  1. Generates a hypothetical document
//  2. Returns both the hypothetical doc and the original query
//
// The caller should embed the hypothetical doc instead of the query.
func (h *HyDE) EnhancedSearch(ctx context.Context, query string) (hypotheticalDoc string, err error) {
	return h.GenerateHypotheticalDocument(ctx, query)
}
