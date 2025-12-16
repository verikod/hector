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
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/utils"
)

// Default summary buffer settings (from legacy pkg/memory/summary_buffer.go)
const (
	DefaultSummaryBudget              = 8000 // Token budget before triggering summarization
	DefaultSummaryThreshold           = 0.85 // Percentage of budget that triggers summarization
	DefaultSummaryTarget              = 0.7  // Target percentage of budget after summarization
	DefaultMinMessagesBeforeSummary   = 20   // Minimum messages before allowing summarization
	DefaultMinMessagesToKeep          = 10   // Minimum recent messages to keep
	DefaultRecentMessageBudgetPercent = 0.8  // Percentage of target budget for recent messages
	SummaryPrefix                     = "Previous conversation summary: "
)

// Summarizer is the interface for conversation summarization.
// Implementations should use an LLM to summarize conversation history.
type Summarizer interface {
	// SummarizeConversation summarizes the given messages into a concise summary.
	SummarizeConversation(ctx context.Context, events []*agent.Event) (string, error)
}

// SummaryBufferStrategy implements token-based context window management
// with automatic summarization when the budget is exceeded.
//
// When the token count exceeds (budget * threshold), old messages are
// summarized and replaced with a summary message. Recent messages are
// preserved to maintain conversational continuity.
//
// Ported from pkg/memory/summary_buffer.go for use in v2.
type SummaryBufferStrategy struct {
	budget       int
	threshold    float64
	target       float64
	tokenCounter *utils.TokenCounter
	summarizer   Summarizer
	model        string
}

// SummaryBufferConfig holds configuration for the summary buffer strategy.
type SummaryBufferConfig struct {
	// Budget is the maximum number of tokens before summarization triggers.
	// Default: 8000
	Budget int

	// Threshold is the percentage of budget that triggers summarization.
	// When current tokens > budget * threshold, summarization occurs.
	// Default: 0.85 (85%)
	Threshold float64

	// Target is the percentage of budget to reduce to after summarization.
	// Recent messages are kept within budget * target.
	// Default: 0.7 (70%)
	Target float64

	// Model is the LLM model name for accurate token counting.
	// Required for accurate counting.
	Model string

	// Summarizer performs conversation summarization.
	// If nil, summarization is disabled (behaves like token_window).
	Summarizer Summarizer
}

// NewSummaryBufferStrategy creates a new summary buffer strategy.
func NewSummaryBufferStrategy(cfg SummaryBufferConfig) (*SummaryBufferStrategy, error) {
	budget := cfg.Budget
	if budget <= 0 {
		budget = DefaultSummaryBudget
	}

	threshold := cfg.Threshold
	if threshold <= 0 || threshold > 1 {
		threshold = DefaultSummaryThreshold
	}

	target := cfg.Target
	if target <= 0 || target > 1 {
		target = DefaultSummaryTarget
	}

	if cfg.Model == "" {
		return nil, fmt.Errorf("model is required for summary_buffer strategy")
	}

	tokenCounter, err := utils.NewTokenCounter(cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to create token counter: %w", err)
	}

	return &SummaryBufferStrategy{
		budget:       budget,
		threshold:    threshold,
		target:       target,
		tokenCounter: tokenCounter,
		summarizer:   cfg.Summarizer,
		model:        cfg.Model,
	}, nil
}

// Name returns the strategy name.
func (s *SummaryBufferStrategy) Name() string {
	return "summary_buffer"
}

// FilterEvents returns events that fit within the target token budget.
// It looks for existing summaries (checkpoint) and loads from there,
// or applies token-based filtering.
func (s *SummaryBufferStrategy) FilterEvents(events []*agent.Event) []*agent.Event {
	if len(events) == 0 {
		return events
	}

	// Look for existing summary (checkpoint) - start from there
	summaryIdx := s.findLastSummaryIndex(events)
	if summaryIdx >= 0 {
		// Start from summary
		events = events[summaryIdx:]
		slog.Debug("SummaryBufferStrategy: loading from checkpoint",
			"checkpoint_idx", summaryIdx,
			"events_after", len(events))
	}

	// Apply token-based filtering within target budget
	targetBudget := int(float64(s.budget) * s.target)
	return s.filterEventsWithinBudget(events, targetBudget)
}

// CheckAndSummarize checks if summarization should occur and performs it.
// Returns a summary event if summarization happened, nil otherwise.
func (s *SummaryBufferStrategy) CheckAndSummarize(ctx context.Context, events []*agent.Event) (*agent.Event, error) {
	if s.summarizer == nil {
		return nil, nil // Summarization disabled
	}

	if !s.shouldSummarize(events) {
		return nil, nil
	}

	return s.summarize(ctx, events)
}

// shouldSummarize checks if summarization should be triggered.
func (s *SummaryBufferStrategy) shouldSummarize(events []*agent.Event) bool {
	if len(events) < DefaultMinMessagesBeforeSummary {
		return false
	}

	currentTokens := s.countEventsTokens(events)
	thresholdTokens := int(float64(s.budget) * s.threshold)

	return currentTokens > thresholdTokens
}

// summarize performs summarization of old events.
func (s *SummaryBufferStrategy) summarize(ctx context.Context, events []*agent.Event) (*agent.Event, error) {
	targetTokens := int(float64(s.budget) * s.target)

	// Select recent messages to keep
	recentEvents := s.selectRecentEventsWithMinimum(events, targetTokens)
	oldEvents := events[:len(events)-len(recentEvents)]

	if len(oldEvents) == 0 {
		return nil, nil // Nothing to summarize
	}

	slog.Info("Summarizing events",
		"total", len(events),
		"old", len(oldEvents),
		"keeping_recent", len(recentEvents))

	// Perform summarization
	summary, err := s.summarizer.SummarizeConversation(ctx, oldEvents)
	if err != nil {
		return nil, fmt.Errorf("summarization failed: %w", err)
	}

	// Create summary event
	summaryEvent := &agent.Event{
		ID:     uuid.NewString(),
		Author: "system",
		Message: a2a.NewMessage(a2a.MessageRoleUser,
			a2a.TextPart{Text: SummaryPrefix + summary}),
	}

	slog.Info("Summarization complete",
		"summarized_events", len(oldEvents),
		"summary_length", len(summary))

	return summaryEvent, nil
}

// filterEventsWithinBudget returns events that fit within the token budget.
func (s *SummaryBufferStrategy) filterEventsWithinBudget(events []*agent.Event, budget int) []*agent.Event {
	if len(events) == 0 {
		return events
	}

	// Work backwards from most recent
	var selected []*agent.Event
	currentTokens := 0

	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		evTokens := s.countEventTokens(ev)

		if currentTokens+evTokens <= budget {
			selected = append([]*agent.Event{ev}, selected...)
			currentTokens += evTokens
		} else {
			break
		}
	}

	// Ensure we keep at least MinMessagesToKeep
	if len(selected) < DefaultMinMessagesToKeep && len(events) >= DefaultMinMessagesToKeep {
		return events[len(events)-DefaultMinMessagesToKeep:]
	}
	if len(selected) < len(events) && len(selected) < DefaultMinMessagesToKeep {
		return events
	}

	return selected
}

// selectRecentEventsWithMinimum selects recent events within budget, with minimum guarantee.
func (s *SummaryBufferStrategy) selectRecentEventsWithMinimum(events []*agent.Event, targetTokens int) []*agent.Event {
	if len(events) == 0 {
		return events
	}

	minEvents := DefaultMinMessagesToKeep
	if len(events) < minEvents {
		return events
	}

	recentBudget := int(float64(targetTokens) * DefaultRecentMessageBudgetPercent)
	recentEvents := s.filterEventsWithinBudget(events, recentBudget)

	if len(recentEvents) < minEvents {
		startIdx := len(events) - minEvents
		if startIdx < 0 {
			startIdx = 0
		}
		return events[startIdx:]
	}

	return recentEvents
}

// findLastSummaryIndex finds the index of the last summary event.
func (s *SummaryBufferStrategy) findLastSummaryIndex(events []*agent.Event) int {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Message == nil {
			continue
		}

		text := extractTextFromMessage(ev.Message)
		if strings.HasPrefix(text, SummaryPrefix) ||
			strings.HasPrefix(text, "Conversation summary:") {
			return i
		}
	}
	return -1
}

// countEventsTokens counts total tokens for all events.
func (s *SummaryBufferStrategy) countEventsTokens(events []*agent.Event) int {
	total := 0
	for _, ev := range events {
		total += s.countEventTokens(ev)
	}
	return total
}

// countEventTokens counts tokens for a single event.
func (s *SummaryBufferStrategy) countEventTokens(ev *agent.Event) int {
	if ev == nil || ev.Message == nil {
		return 0
	}

	text := extractTextFromMessage(ev.Message)
	messages := []utils.Message{{Role: ev.Author, Content: text}}
	return s.tokenCounter.CountMessages(messages)
}

// Budget returns the configured token budget.
func (s *SummaryBufferStrategy) Budget() int {
	return s.budget
}

// Threshold returns the configured threshold.
func (s *SummaryBufferStrategy) Threshold() float64 {
	return s.threshold
}

// Target returns the configured target.
func (s *SummaryBufferStrategy) Target() float64 {
	return s.target
}

// Ensure SummaryBufferStrategy implements WorkingMemoryStrategy.
var _ WorkingMemoryStrategy = (*SummaryBufferStrategy)(nil)
