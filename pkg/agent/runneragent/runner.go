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

package runneragent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/tool"
)

// Config defines the configuration for a runner agent.
type Config struct {
	// Name is the agent's unique identifier.
	Name string

	// DisplayName is the human-readable name (optional).
	DisplayName string

	// Description describes what the agent does.
	Description string

	// Tools are the tools to execute in sequence.
	// Each tool's output becomes the next tool's input.
	Tools []tool.Tool
}

// New creates a runner agent that executes tools in sequence without LLM.
//
// Each tool receives the previous tool's output as its input parameter.
// The first tool receives the user's message. The final tool's output
// becomes the agent's response.
//
// Example:
//
//	runner, err := runneragent.New(runneragent.Config{
//	    Name:        "data_fetcher",
//	    Description: "Fetches and parses data",
//	    Tools:       []tool.Tool{fetchTool, parseTool},
//	})
func New(cfg Config) (agent.Agent, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("runner agent name is required")
	}

	if len(cfg.Tools) == 0 {
		return nil, fmt.Errorf("runner agent requires at least one tool")
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
		AgentType:   agent.TypeRunnerAgent,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
			return runTools(ctx, cfg.Tools)
		},
	})
}

// runTools executes tools in sequence, piping output to input.
func runTools(ctx agent.InvocationContext, tools []tool.Tool) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Start with user content as initial input
		currentInput := parseInput(ctx.UserContent())

		for _, t := range tools {
			// Check context cancellation
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			// Execute tool
			callableTool, ok := t.(tool.CallableTool)
			if !ok {
				yield(nil, fmt.Errorf("tool %s is not callable", t.Name()))
				return
			}

			// Create a minimal tool context adapter
			toolCtx := &runnerToolContext{
				InvocationContext: ctx,
			}

			// Execute tool with current input
			result, err := callableTool.Call(toolCtx, currentInput)
			if err != nil {
				yield(nil, fmt.Errorf("tool %s failed: %w", t.Name(), err))
				return
			}

			// Tool output becomes next tool's input
			currentInput = result

			// Yield a tool result event for observability
			event := agent.NewEvent(ctx.InvocationID())
			event.Branch = ctx.Branch()
			// Use message with tool result info
			event.Message = &a2a.Message{
				Role: a2a.MessageRoleAgent,
				Parts: []a2a.Part{
					a2a.TextPart{Text: fmt.Sprintf("[Tool: %s] %s", t.Name(), formatResult(result))},
				},
			}
			event.Partial = true // Mark as intermediate
			if !yield(event, nil) {
				return
			}
		}

		// Final output becomes the agent's response
		finalEvent := agent.NewEvent(ctx.InvocationID())
		finalEvent.Branch = ctx.Branch()
		finalEvent.Message = &a2a.Message{
			Role: a2a.MessageRoleAgent,
			Parts: []a2a.Part{
				a2a.TextPart{Text: formatResult(currentInput)},
			},
		}
		yield(finalEvent, nil)
	}
}

// runnerToolContext is a minimal tool.Context implementation for runner agents.
type runnerToolContext struct {
	agent.InvocationContext
}

// FunctionCallID returns a placeholder ID for runner tool calls.
func (c *runnerToolContext) FunctionCallID() string {
	return "runner-" + c.InvocationID()
}

// Actions returns empty actions (runner doesn't use state/artifact deltas).
func (c *runnerToolContext) Actions() *agent.EventActions {
	return &agent.EventActions{}
}

// SearchMemory returns nil (runner doesn't use memory search).
func (c *runnerToolContext) SearchMemory(_ context.Context, _ string) (*agent.MemorySearchResponse, error) {
	return nil, nil
}

// Task returns nil (runner doesn't use task cancellation).
func (c *runnerToolContext) Task() agent.CancellableTask {
	return nil
}

// parseInput converts user content to a map for tool input.
func parseInput(content *agent.Content) map[string]any {
	if content == nil {
		return map[string]any{"input": ""}
	}

	// Extract text from content parts
	text := ""
	for _, part := range content.Parts {
		if textPart, ok := part.(a2a.TextPart); ok {
			text += textPart.Text
		}
	}

	// Try to parse as JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		return parsed
	}

	// Fallback: wrap as "input" field
	return map[string]any{
		"input": text,
	}
}

// formatResult converts a map to a string for output.
func formatResult(result map[string]any) string {
	if result == nil {
		return ""
	}

	// Try to format as JSON
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("%v", result)
	}
	return string(data)
}
