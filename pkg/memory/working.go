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

// WorkingMemoryStrategy defines the interface for context window management.
// Different strategies can implement different approaches:
//   - buffer_window: Keep last N messages (simple, fast)
//   - token_window: Keep messages within token budget (accurate)
//   - summary_buffer: Summarize old messages when exceeding budget (compact)
//
// Ported from pkg/memory/types.go for use in v2.
//
// NOTE: Future optimization opportunity - session loading could be checkpoint-aware
// to avoid loading all events for strategies like summary_buffer. The session.GetRequest
// already supports NumRecentEvents for this purpose. See pkg/memory/summary_buffer.go
// LoadState for the legacy approach.
type WorkingMemoryStrategy interface {
	// Name returns the strategy identifier.
	Name() string

	// FilterEvents applies the strategy to filter/truncate events for context window.
	// This is called before building messages for the LLM.
	// Returns the filtered events that should be included in the context.
	FilterEvents(events []*agent.Event) []*agent.Event

	// CheckAndSummarize checks if summarization should occur and performs it if needed.
	// This is called after a turn completes (when events are persisted).
	// Returns a summary event to persist (if any), or nil if no summarization needed.
	// This is optional - strategies like buffer_window return nil.
	CheckAndSummarize(ctx context.Context, events []*agent.Event) (*agent.Event, error)
}

// NilWorkingMemory is a no-op strategy that returns all events unchanged.
// Used when no working memory strategy is configured.
type NilWorkingMemory struct{}

// Name returns the strategy name.
func (NilWorkingMemory) Name() string {
	return "none"
}

// FilterEvents returns all events unchanged.
func (NilWorkingMemory) FilterEvents(events []*agent.Event) []*agent.Event {
	return events
}

// CheckAndSummarize always returns nil (no summarization).
func (NilWorkingMemory) CheckAndSummarize(ctx context.Context, events []*agent.Event) (*agent.Event, error) {
	return nil, nil
}

// Ensure NilWorkingMemory implements WorkingMemoryStrategy.
var _ WorkingMemoryStrategy = NilWorkingMemory{}

// WorkingMemoryProvider is implemented by agents that have a working memory strategy.
// This allows the runner to access the strategy for post-turn summarization.
type WorkingMemoryProvider interface {
	// WorkingMemory returns the agent's working memory strategy.
	// Returns nil if no strategy is configured.
	WorkingMemory() WorkingMemoryStrategy
}
