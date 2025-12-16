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

// DefaultBufferWindowSize is the default number of events to keep.
const DefaultBufferWindowSize = 20

// BufferWindowStrategy implements a simple sliding window that keeps
// the last N events. This is the simplest and fastest strategy.
//
// Ported from pkg/memory/buffer_window.go for use in v2.
type BufferWindowStrategy struct {
	windowSize int
}

// BufferWindowConfig holds configuration for the buffer window strategy.
type BufferWindowConfig struct {
	// WindowSize is the maximum number of events to keep.
	// Default: 20
	WindowSize int
}

// NewBufferWindowStrategy creates a new buffer window strategy.
func NewBufferWindowStrategy(cfg BufferWindowConfig) *BufferWindowStrategy {
	windowSize := cfg.WindowSize
	if windowSize <= 0 {
		windowSize = DefaultBufferWindowSize
	}

	return &BufferWindowStrategy{
		windowSize: windowSize,
	}
}

// Name returns the strategy name.
func (s *BufferWindowStrategy) Name() string {
	return "buffer_window"
}

// FilterEvents returns the last windowSize events.
// If there are fewer events than windowSize, all events are returned.
func (s *BufferWindowStrategy) FilterEvents(events []*agent.Event) []*agent.Event {
	if len(events) <= s.windowSize {
		return events
	}
	return events[len(events)-s.windowSize:]
}

// CheckAndSummarize always returns nil (buffer window doesn't summarize).
func (s *BufferWindowStrategy) CheckAndSummarize(ctx context.Context, events []*agent.Event) (*agent.Event, error) {
	return nil, nil
}

// WindowSize returns the configured window size.
func (s *BufferWindowStrategy) WindowSize() int {
	return s.windowSize
}

// Ensure BufferWindowStrategy implements WorkingMemoryStrategy.
var _ WorkingMemoryStrategy = (*BufferWindowStrategy)(nil)
