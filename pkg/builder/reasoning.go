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

package builder

import (
	"github.com/verikod/hector/pkg/agent/llmagent"
)

// ReasoningBuilder provides a fluent API for building reasoning configuration.
//
// Example:
//
//	reasoning := builder.NewReasoning().
//	    MaxIterations(100).
//	    EnableExitTool(true).
//	    EnableEscalateTool(true).
//	    CompletionInstruction("Call exit_loop when done.").
//	    Build()
type ReasoningBuilder struct {
	maxIterations         int
	enableExitTool        bool
	enableEscalateTool    bool
	completionInstruction string
}

// NewReasoning creates a new reasoning configuration builder.
//
// Example:
//
//	reasoning := builder.NewReasoning().
//	    MaxIterations(50).
//	    Build()
func NewReasoning() *ReasoningBuilder {
	return &ReasoningBuilder{
		maxIterations: 100, // Default safety limit
	}
}

// MaxIterations sets the maximum number of reasoning iterations.
// This is a SAFETY limit, not the primary termination condition.
// The loop terminates when semantic conditions are met (no tool calls, etc.)
//
// Default: 100 (high enough to not interfere with normal operation)
//
// Example:
//
//	builder.NewReasoning().MaxIterations(50)
func (b *ReasoningBuilder) MaxIterations(max int) *ReasoningBuilder {
	if max <= 0 {
		panic("max iterations must be positive")
	}
	b.maxIterations = max
	return b
}

// EnableExitTool adds the exit_loop tool for explicit termination.
// When enabled, the agent can call exit_loop to signal task completion.
//
// Example:
//
//	builder.NewReasoning().EnableExitTool(true)
func (b *ReasoningBuilder) EnableExitTool(enable bool) *ReasoningBuilder {
	b.enableExitTool = enable
	return b
}

// EnableEscalateTool adds the escalate tool for parent delegation.
// When enabled, the agent can escalate to a higher-level agent.
//
// Example:
//
//	builder.NewReasoning().EnableEscalateTool(true)
func (b *ReasoningBuilder) EnableEscalateTool(enable bool) *ReasoningBuilder {
	b.enableEscalateTool = enable
	return b
}

// CompletionInstruction sets a custom instruction appended to help
// the model know when to stop.
//
// Example:
//
//	builder.NewReasoning().CompletionInstruction("Call exit_loop when you have a final answer.")
func (b *ReasoningBuilder) CompletionInstruction(instruction string) *ReasoningBuilder {
	b.completionInstruction = instruction
	return b
}

// Build creates the reasoning configuration.
func (b *ReasoningBuilder) Build() *llmagent.ReasoningConfig {
	return &llmagent.ReasoningConfig{
		MaxIterations:         b.maxIterations,
		EnableExitTool:        b.enableExitTool,
		EnableEscalateTool:    b.enableEscalateTool,
		CompletionInstruction: b.completionInstruction,
	}
}
