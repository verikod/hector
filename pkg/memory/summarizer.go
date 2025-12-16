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
	"strings"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/model"
)

// Default summarization prompt
const defaultSummarizationPrompt = `You are a conversation summarizer. Your task is to create a concise summary of the conversation history that preserves the key information, decisions made, and context needed for continuing the conversation.

Guidelines:
- Focus on key facts, decisions, and context
- Preserve important details like names, dates, numbers
- Keep the summary concise but comprehensive
- Write in a neutral, factual tone
- Do not add information not present in the conversation

Conversation to summarize:
%s

Please provide a concise summary:`

// LLMSummarizer implements the Summarizer interface using an LLM.
type LLMSummarizer struct {
	llm    model.LLM
	prompt string
}

// LLMSummarizerConfig configures the LLM summarizer.
type LLMSummarizerConfig struct {
	// LLM is the language model to use for summarization.
	LLM model.LLM

	// Prompt is a custom summarization prompt template.
	// Use %s as placeholder for the conversation text.
	// If empty, uses the default prompt.
	Prompt string
}

// NewLLMSummarizer creates a new LLM-based summarizer.
func NewLLMSummarizer(cfg LLMSummarizerConfig) (*LLMSummarizer, error) {
	if cfg.LLM == nil {
		return nil, fmt.Errorf("LLM is required for summarization")
	}

	prompt := cfg.Prompt
	if prompt == "" {
		prompt = defaultSummarizationPrompt
	}

	return &LLMSummarizer{
		llm:    cfg.LLM,
		prompt: prompt,
	}, nil
}

// SummarizeConversation summarizes the given events into a concise summary.
func (s *LLMSummarizer) SummarizeConversation(ctx context.Context, events []*agent.Event) (string, error) {
	if len(events) == 0 {
		return "", nil
	}

	// Build conversation text from events
	var conversation strings.Builder
	for _, ev := range events {
		if ev.Message == nil {
			continue
		}

		role := ev.Author
		if role == "" {
			role = "unknown"
		}

		text := extractTextFromA2AMessage(ev.Message)
		if text != "" {
			conversation.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, text))
		}
	}

	if conversation.Len() == 0 {
		return "", nil
	}

	// Build the summarization prompt
	fullPrompt := fmt.Sprintf(s.prompt, conversation.String())

	// Create request for LLM
	req := &model.Request{
		Messages: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: fullPrompt}),
		},
	}

	// Call LLM (non-streaming)
	var summary string
	for resp, err := range s.llm.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", fmt.Errorf("summarization failed: %w", err)
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if tp, ok := part.(a2a.TextPart); ok {
					summary += tp.Text
				}
			}
		}
	}

	return strings.TrimSpace(summary), nil
}

// Ensure LLMSummarizer implements Summarizer.
var _ Summarizer = (*LLMSummarizer)(nil)
