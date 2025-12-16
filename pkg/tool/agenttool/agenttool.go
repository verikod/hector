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

// Package agenttool provides a tool that allows an agent to call another agent.
// This enables composition of agents (Pattern 2: agent-as-tool delegation).
//
// This follows adk-go's agenttool pattern exactly:
//   - Tool name matches agent name (not prefixed with "call_")
//   - Child agent runs in isolated session (no state bleeding)
//   - Parent state is copied to child (filtered for internal keys)
//
// Example:
//
//	searchAgent, _ := llmagent.New(llmagent.Config{...})
//	analysisAgent, _ := llmagent.New(llmagent.Config{...})
//
//	rootAgent, _ := llmagent.New(llmagent.Config{
//	    Tools: []tool.Tool{
//	        agenttool.New(searchAgent, nil),   // ✅ adk-go aligned pattern
//	        agenttool.New(analysisAgent, nil),
//	    },
//	})
package agenttool

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/session"
	"github.com/verikod/hector/pkg/tool"
)

// agentTool implements a tool that allows an agent to call another agent.
// Follows adk-go's agenttool pattern for isolated sub-agent execution.
type agentTool struct {
	agent             agent.Agent
	skipSummarization bool
}

// Config holds the configuration for an agent tool.
type Config struct {
	// SkipSummarization, if true, will cause the agent to skip summarization
	// after the sub-agent finishes execution.
	SkipSummarization bool
}

// New creates a new agent tool that wraps the given agent.
// This enables Pattern 2 (agent-as-tool delegation) where the parent agent
// maintains control and receives structured results from the child agent.
//
// If cfg is nil, default configuration is used (skipSummarization = false).
//
// The child agent runs in an ISOLATED session (adk-go pattern), meaning:
//   - Child has its own session state
//   - Parent state is copied at invocation time (excluding internal keys)
//   - State changes in child do NOT affect parent session
func New(ag agent.Agent, cfg *Config) tool.Tool {
	if ag == nil {
		return nil
	}

	skipSummarization := false
	if cfg != nil {
		skipSummarization = cfg.SkipSummarization
	}

	return &agentTool{
		agent:             ag,
		skipSummarization: skipSummarization,
	}
}

// Name returns the tool name, which is the agent name.
// This matches adk-go's convention (no "call_" prefix).
func (t *agentTool) Name() string {
	return t.agent.Name()
}

// Description returns a description of what this tool does.
// This matches adk-go's convention (just the agent description).
func (t *agentTool) Description() string {
	return t.agent.Description()
}

// IsLongRunning returns false - agent tools execute synchronously.
func (t *agentTool) IsLongRunning() bool {
	return false
}

// RequiresApproval returns false - agent tools don't need approval by default.
func (t *agentTool) RequiresApproval() bool {
	return false
}

// Schema returns the JSON schema for the tool's parameters.
// Uses "request" as the default parameter name for simplicity.
func (t *agentTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"request": map[string]any{
				"type":        "string",
				"description": "The task or request for the " + t.agent.Name() + " agent",
			},
		},
		"required": []string{"request"},
	}
}

// Call executes the agent in an isolated session and returns structured results.
// This follows adk-go's pattern of creating a new session for the sub-agent.
func (t *agentTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	// Extract request from input
	request, ok := args["request"].(string)
	if !ok {
		return nil, fmt.Errorf("request parameter must be a string")
	}

	// Set skip summarization if configured
	if t.skipSummarization {
		if actions := ctx.Actions(); actions != nil {
			actions.SkipSummarization = true
		}
	}

	// Get parent invocation context
	parentInvCtx := extractInvocationContext(ctx)
	if parentInvCtx == nil {
		return nil, fmt.Errorf("could not extract invocation context from tool context")
	}

	// Create isolated session for child agent (adk-go pattern)
	childSession, err := t.createIsolatedSession(parentInvCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to create isolated session: %w", err)
	}

	// Create isolated invocation context for child agent
	childCtx := agent.NewInvocationContext(
		parentInvCtx,
		agent.InvocationContextParams{
			Agent:       t.agent,
			Session:     childSession,
			Artifacts:   parentInvCtx.Artifacts(), // Share artifacts
			Memory:      parentInvCtx.Memory(),    // Share memory
			UserContent: agent.NewTextContent(request, "user"),
			RunConfig:   parentInvCtx.RunConfig(),
			Branch:      ctx.Branch() + "/" + t.agent.Name(),
		},
	)

	// Execute the child agent and collect results
	var output string
	var eventCount int

	for event, err := range t.agent.Run(childCtx) {
		if err != nil {
			return nil, fmt.Errorf("agent execution error: %w", err)
		}

		if event == nil {
			continue
		}

		// Count non-partial events
		if !event.Partial {
			eventCount++
		}

		// Extract text output
		if textContent := event.TextContent(); textContent != "" {
			output = textContent
		}
	}

	// Default message if no output
	if output == "" {
		output = fmt.Sprintf("Task completed by %s agent", t.agent.Name())
	}

	return map[string]any{
		"result":        output,
		"agent_name":    t.agent.Name(),
		"event_count":   eventCount,
		"invocation_id": childCtx.InvocationID(),
	}, nil
}

// createIsolatedSession creates a new in-memory session for the child agent.
// This follows adk-go's pattern of session isolation for sub-agents.
// Parent state is copied but filtered for internal keys (e.g., "_adk" prefix).
func (t *agentTool) createIsolatedSession(parentCtx agent.InvocationContext) (session.Session, error) {
	// Create new session service for isolation
	sessionService := session.InMemoryService()

	// Copy parent state, filtering out internal keys
	parentState := make(map[string]any)
	if parentSession := parentCtx.Session(); parentSession != nil {
		for k, v := range parentSession.State().All() {
			// Filter out internal keys (adk-go pattern)
			if !strings.HasPrefix(k, "_adk") && !strings.HasPrefix(k, "_hector") {
				parentState[k] = v
			}
		}
	}

	// Create new session for child agent
	resp, err := sessionService.Create(context.Background(), &session.CreateRequest{
		AppName: t.agent.Name(),
		UserID:  parentCtx.Session().UserID(),
		State:   parentState,
	})
	if err != nil {
		return nil, err
	}

	return resp.Session, nil
}

// extractInvocationContext extracts the InvocationContext from a tool.Context.
// This is needed because tool.Context embeds CallbackContext, but we need
// access to the full InvocationContext for creating child contexts.
func extractInvocationContext(ctx tool.Context) agent.InvocationContext {
	// tool.Context embeds CallbackContext
	// Try to get InvocationContext if available
	if invCtx, ok := ctx.(agent.InvocationContext); ok {
		return invCtx
	}

	// The tool context implementation may have an invCtx field
	// This is a known pattern in the llmagent package
	type invCtxHolder interface {
		InvocationContext() agent.InvocationContext
	}
	if holder, ok := ctx.(invCtxHolder); ok {
		return holder.InvocationContext()
	}

	return nil
}

// CallStreaming executes the agent and yields incremental results in real-time.
// This provides live feedback from sub-agent execution (streaming text, thinking, tool calls).
func (t *agentTool) CallStreaming(ctx tool.Context, args map[string]any) iter.Seq2[*tool.Result, error] {
	return func(yield func(*tool.Result, error) bool) {
		// Extract request from input
		request, ok := args["request"].(string)
		if !ok {
			yield(nil, fmt.Errorf("request parameter must be a string"))
			return
		}

		// Set skip summarization if configured
		if t.skipSummarization {
			if actions := ctx.Actions(); actions != nil {
				actions.SkipSummarization = true
			}
		}

		// Get parent invocation context
		parentInvCtx := extractInvocationContext(ctx)
		if parentInvCtx == nil {
			yield(nil, fmt.Errorf("could not extract invocation context from tool context"))
			return
		}

		// Create isolated session for child agent (adk-go pattern)
		childSession, err := t.createIsolatedSession(parentInvCtx)
		if err != nil {
			yield(nil, fmt.Errorf("failed to create isolated session: %w", err))
			return
		}

		// Create isolated invocation context for child agent
		childCtx := agent.NewInvocationContext(
			parentInvCtx,
			agent.InvocationContextParams{
				Agent:       t.agent,
				Session:     childSession,
				Artifacts:   parentInvCtx.Artifacts(), // Share artifacts
				Memory:      parentInvCtx.Memory(),    // Share memory
				UserContent: agent.NewTextContent(request, "user"),
				RunConfig:   parentInvCtx.RunConfig(),
				Branch:      ctx.Branch() + "/" + t.agent.Name(),
			},
		)

		// Register sub-agent execution for cascade cancellation
		callID := ctx.FunctionCallID()
		if taskObj := ctx.Task(); taskObj != nil {
			taskObj.RegisterExecution(&agent.ChildExecution{
				CallID: callID,
				Name:   t.agent.Name(),
				Type:   "agent",
				Cancel: func() bool {
					// Sub-agent cancellation is signaled via context
					// The parent's context.Done() will propagate to child
					return true
				},
			})
			defer taskObj.UnregisterExecution(callID)
		}

		// Stream agent events as they occur
		var lastOutput string
		var eventCount int

		for event, err := range t.agent.Run(childCtx) {
			if err != nil {
				yield(nil, fmt.Errorf("agent execution error: %w", err))
				return
			}

			if event == nil {
				continue
			}

			// Count non-partial events
			if !event.Partial {
				eventCount++
			}

			// Extract and yield text output
			if textContent := event.TextContent(); textContent != "" {
				lastOutput = textContent

				// Yield incremental result for streaming (only non-partial events for now)
				if !event.Partial {
					result := &tool.Result{
						Content:   textContent,
						Streaming: true, // Mark as streaming chunk for UI
					}

					if !yield(result, nil) {
						return
					}
				}
			}
		}

		// Yield final result with complete metadata
		finalContent := lastOutput
		if finalContent == "" {
			finalContent = fmt.Sprintf("Task completed by %s agent", t.agent.Name())
		}

		finalResult := &tool.Result{
			Content:   finalContent,
			Streaming: false, // Mark as final result
		}

		yield(finalResult, nil)
	}
}

// Verify interface compliance
var _ tool.StreamingTool = (*agentTool)(nil)
