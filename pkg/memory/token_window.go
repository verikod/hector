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
	"log/slog"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/utils"
)

// Default token window settings
const (
	DefaultTokenBudget    = 8000 // Token budget for history
	DefaultPreserveRecent = 5    // Minimum messages to always keep
)

// TokenWindowStrategy implements token-based context window management.
// It keeps events that fit within the token budget, working backwards from
// the most recent events (which are typically most relevant).
//
// This is a simplified version of SummaryBufferStrategy that focuses only
// on truncation without summarization.
type TokenWindowStrategy struct {
	budget         int
	preserveRecent int
	tokenCounter   *utils.TokenCounter
	model          string
}

// TokenWindowConfig holds configuration for the token window strategy.
type TokenWindowConfig struct {
	// Budget is the maximum number of tokens for conversation history.
	// Default: 8000
	Budget int

	// PreserveRecent is the minimum number of recent messages to always keep,
	// even if they exceed the budget slightly.
	// Default: 5
	PreserveRecent int

	// Model is the LLM model name for accurate token counting.
	// Required for accurate counting; falls back to estimation if not provided.
	Model string
}

// NewTokenWindowStrategy creates a new token window strategy.
func NewTokenWindowStrategy(cfg TokenWindowConfig) (*TokenWindowStrategy, error) {
	budget := cfg.Budget
	if budget <= 0 {
		budget = DefaultTokenBudget
	}

	preserveRecent := cfg.PreserveRecent
	if preserveRecent <= 0 {
		preserveRecent = DefaultPreserveRecent
	}

	// Create token counter if model is provided
	var tokenCounter *utils.TokenCounter
	if cfg.Model != "" {
		var err error
		tokenCounter, err = utils.NewTokenCounter(cfg.Model)
		if err != nil {
			slog.Warn("Failed to create token counter, using estimation",
				"model", cfg.Model,
				"error", err)
		}
	}

	return &TokenWindowStrategy{
		budget:         budget,
		preserveRecent: preserveRecent,
		tokenCounter:   tokenCounter,
		model:          cfg.Model,
	}, nil
}

// Name returns the strategy name.
func (s *TokenWindowStrategy) Name() string {
	return "token_window"
}

// FilterEvents returns events that fit within the token budget.
// It preserves at least preserveRecent messages and works backwards
// from the most recent events.
func (s *TokenWindowStrategy) FilterEvents(events []*agent.Event) []*agent.Event {
	if len(events) == 0 {
		return events
	}

	// Always keep at least preserveRecent messages
	if len(events) <= s.preserveRecent {
		return events
	}

	// Convert events to messages for token counting
	messages := make([]utils.Message, 0, len(events))
	for _, ev := range events {
		if ev.Message == nil {
			continue
		}
		messages = append(messages, utils.Message{
			Role:    ev.Author,
			Content: extractTextFromMessage(ev.Message),
		})
	}

	// Calculate which messages fit within budget
	var fitted []utils.Message
	if s.tokenCounter != nil {
		fitted = s.tokenCounter.FitWithinLimit(messages, s.budget)
	} else {
		// Fallback: estimate tokens (~4 chars per token)
		fitted = s.estimateFitWithinLimit(messages, s.budget)
	}

	// Ensure we keep at least preserveRecent
	minKeep := s.preserveRecent
	if len(events) < minKeep {
		minKeep = len(events)
	}
	if len(fitted) < minKeep {
		// Return last minKeep events
		return events[len(events)-minKeep:]
	}

	// Return events corresponding to fitted messages
	startIdx := len(events) - len(fitted)
	if startIdx < 0 {
		startIdx = 0
	}

	slog.Debug("TokenWindowStrategy filtered events",
		"total_events", len(events),
		"kept_events", len(events)-startIdx,
		"budget", s.budget)

	return events[startIdx:]
}

// CheckAndSummarize always returns nil (token window doesn't summarize).
// For summarization support, use SummaryBufferStrategy.
func (s *TokenWindowStrategy) CheckAndSummarize(ctx context.Context, events []*agent.Event) (*agent.Event, error) {
	return nil, nil
}

// Budget returns the configured token budget.
func (s *TokenWindowStrategy) Budget() int {
	return s.budget
}

// estimateFitWithinLimit is a fallback when token counter is not available.
func (s *TokenWindowStrategy) estimateFitWithinLimit(messages []utils.Message, maxTokens int) []utils.Message {
	if len(messages) == 0 {
		return messages
	}

	fitted := []utils.Message{}
	currentTokens := 0

	// Work backwards from most recent
	for i := len(messages) - 1; i >= 0; i-- {
		// Estimate: ~4 characters per token
		msgTokens := len(messages[i].Content)/4 + len(messages[i].Role)/4 + 3
		if currentTokens+msgTokens > maxTokens {
			break
		}
		fitted = append([]utils.Message{messages[i]}, fitted...)
		currentTokens += msgTokens
	}

	return fitted
}

// extractTextFromMessage extracts text content from an a2a message.
func extractTextFromMessage(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}

	var text string
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case a2a.TextPart:
			text += p.Text
		case *a2a.TextPart:
			text += p.Text
		}
	}
	return text
}

// Ensure TokenWindowStrategy implements WorkingMemoryStrategy.
var _ WorkingMemoryStrategy = (*TokenWindowStrategy)(nil)
