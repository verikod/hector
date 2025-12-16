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

// Package controltool provides control flow tools for agent reasoning loops.
//
// These tools allow agents to explicitly control the reasoning loop:
//   - exit_loop: Signal task completion and exit the loop
//   - escalate: Escalate to a parent agent when stuck or needing help
//   - transfer_to: Transfer control to another agent
//
// Following adk-go patterns, these tools work by setting EventActions flags
// that are checked by the termination conditions in the reasoning loop.
package controltool

import (
	"github.com/verikod/hector/pkg/tool"
)

// ExitLoop creates a tool that allows the agent to explicitly exit the reasoning loop.
// When called, it sets SkipSummarization=true which triggers the skip_summarization
// termination condition.
//
// Usage in YAML config:
//
//	tools:
//	  - exit_loop
//
// Usage in instruction:
//
//	Call `exit_loop` when your task is complete and you have a final answer.
func ExitLoop() tool.CallableTool {
	return &exitLoopTool{}
}

type exitLoopTool struct{}

func (t *exitLoopTool) Name() string {
	return "exit_loop"
}

func (t *exitLoopTool) Description() string {
	return "Exits the reasoning loop. Call this when your task is complete and you have a final answer to provide."
}

func (t *exitLoopTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *exitLoopTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	ctx.Actions().SkipSummarization = true
	return map[string]any{
		"status":  "completed",
		"message": "Task marked as complete. Exiting reasoning loop.",
	}, nil
}

func (t *exitLoopTool) IsLongRunning() bool {
	return false
}

func (t *exitLoopTool) RequiresApproval() bool {
	return false
}

// Escalate creates a tool that allows the agent to escalate to a parent agent.
// When called, it sets Escalate=true and SkipSummarization=true which triggers
// the escalate termination condition.
//
// Usage in YAML config:
//
//	tools:
//	  - escalate
//
// Usage in instruction:
//
//	Call `escalate` if you need help, are stuck, or the task is outside your capabilities.
func Escalate() tool.CallableTool {
	return &escalateTool{}
}

type escalateTool struct{}

func (t *escalateTool) Name() string {
	return "escalate"
}

func (t *escalateTool) Description() string {
	return "Escalates to a higher-level agent. Call this when you need help, are stuck, or the task is outside your capabilities."
}

func (t *escalateTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reason": map[string]any{
				"type":        "string",
				"description": "Why you are escalating (what help you need or what you're stuck on)",
			},
		},
		"required": []string{"reason"},
	}
}

func (t *escalateTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	reason, _ := args["reason"].(string)
	if reason == "" {
		reason = "No reason provided"
	}

	ctx.Actions().Escalate = true
	ctx.Actions().SkipSummarization = true

	return map[string]any{
		"status":    "escalated",
		"reason":    reason,
		"message":   "Escalating to parent agent.",
		"escalated": true,
	}, nil
}

func (t *escalateTool) IsLongRunning() bool {
	return false
}

func (t *escalateTool) RequiresApproval() bool {
	return false
}

// TransferTo creates a tool that transfers control to a specific agent.
// When called, it sets TransferToAgent and SkipSummarization which triggers
// the transfer termination condition.
//
// Parameters:
//   - agentName: The name of the agent to transfer to
//   - description: Description of what this agent does (for LLM context)
//
// Usage in YAML config (typically auto-generated for sub-agents):
//
//	tools:
//	  - transfer_to_researcher
//
// Usage in instruction:
//
//	Transfer to the researcher agent for information gathering tasks.
func TransferTo(agentName, description string) tool.CallableTool {
	return &transferTool{
		agentName:   agentName,
		description: description,
	}
}

type transferTool struct {
	agentName   string
	description string
}

func (t *transferTool) Name() string {
	return "transfer_to_" + t.agentName
}

func (t *transferTool) Description() string {
	if t.description != "" {
		return t.description
	}
	return "Transfers control to the " + t.agentName + " agent."
}

func (t *transferTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"request": map[string]any{
				"type":        "string",
				"description": "What you want the " + t.agentName + " agent to do",
			},
		},
		"required": []string{"request"},
	}
}

func (t *transferTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	request, _ := args["request"].(string)

	ctx.Actions().TransferToAgent = t.agentName
	ctx.Actions().SkipSummarization = true

	return map[string]any{
		"status":         "transferred",
		"transferred_to": t.agentName,
		"request":        request,
		"message":        "Transferring to " + t.agentName + " agent.",
	}, nil
}

func (t *transferTool) IsLongRunning() bool {
	return false
}

func (t *transferTool) RequiresApproval() bool {
	return false
}

// Verify interface compliance
var (
	_ tool.CallableTool = (*exitLoopTool)(nil)
	_ tool.CallableTool = (*escalateTool)(nil)
	_ tool.CallableTool = (*transferTool)(nil)
)
