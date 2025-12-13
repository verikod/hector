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

// Package approvaltool provides a Human-in-the-Loop (HITL) tool example.
//
// This tool demonstrates the long-running/HITL pattern where:
// 1. Tool returns immediately with a pending status
// 2. Task transitions to `input_required` state
// 3. Human provides approval/rejection
// 4. Task resumes with the human's decision
//
// This is different from StreamingTool which produces incremental output
// but runs to completion without human intervention.
//
// Example usage:
//
//	tool := approvaltool.New(approvaltool.Config{
//	    Name:        "request_approval",
//	    Description: "Request human approval for sensitive operations",
//	})
//
// A2A Protocol Mapping:
//   - Initial call: Returns immediately with pending status
//   - Task state: Transitions to `input_required`
//   - A2A event: `status-update` with `state: input_required`
//   - Human response: Sent via `message/send` with approval decision
//   - Resume: Task continues with approval result
package approvaltool

import (
	"fmt"

	"github.com/kadirpekel/hector/pkg/tool"
)

// Config configures the approval tool.
type Config struct {
	// Name is the tool name (default: "request_approval")
	Name string

	// Description describes what the tool does
	Description string

	// RequiredFields specifies what information is needed for approval
	RequiredFields []string
}

// ApprovalTool implements a HITL approval workflow.
type ApprovalTool struct {
	name           string
	description    string
	requiredFields []string
}

// New creates a new approval tool.
func New(cfg Config) *ApprovalTool {
	name := cfg.Name
	if name == "" {
		name = "request_approval"
	}

	description := cfg.Description
	if description == "" {
		description = "Request human approval. Use this before taking high-risk actions or when you need explicit user confirmation. " +
			"Returns immediately with pending status. Task will pause until human responds."
	}

	return &ApprovalTool{
		name:           name,
		description:    description,
		requiredFields: cfg.RequiredFields,
	}
}

// Name returns the tool name.
func (t *ApprovalTool) Name() string {
	return t.name
}

// Description returns the tool description.
func (t *ApprovalTool) Description() string {
	return t.description
}

// IsLongRunning returns false - this is not an async tool.
func (t *ApprovalTool) IsLongRunning() bool {
	return false
}

// RequiresApproval returns true - this is a HITL tool.
// The difference from streaming:
// - Streaming: produces incremental output but runs to completion
// - RequiresApproval: pauses and waits for human input before execution
func (t *ApprovalTool) RequiresApproval() bool {
	return true // This triggers HITL flow
}

// Schema returns the JSON schema for the tool parameters.
func (t *ApprovalTool) Schema() map[string]any {
	properties := map[string]any{
		"action": map[string]any{
			"type":        "string",
			"description": "The action requiring approval",
		},
		"reason": map[string]any{
			"type":        "string",
			"description": "Why approval is needed",
		},
		"details": map[string]any{
			"type":        "object",
			"description": "Additional details for the approver",
		},
	}

	required := []string{"action", "reason"}

	// Add custom required fields
	for _, field := range t.requiredFields {
		if _, exists := properties[field]; !exists {
			properties[field] = map[string]any{
				"type":        "string",
				"description": fmt.Sprintf("Required field: %s", field),
			}
		}
		required = append(required, field)
	}

	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// Call executes the approval request.
// For HITL tools, this returns immediately with a pending status.
// The actual approval/rejection comes later via human input.
func (t *ApprovalTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	action, _ := args["action"].(string)
	reason, _ := args["reason"].(string)
	details, _ := args["details"].(map[string]any)

	// Mark this as requiring input via EventActions
	// This will cause the task to transition to `input_required` state
	if actions := ctx.Actions(); actions != nil {
		actions.RequireInput = true
		actions.InputPrompt = fmt.Sprintf("Approval required for: %s\nReason: %s", action, reason)
	}

	// Return pending status immediately
	// The task will pause here until human provides input
	return map[string]any{
		"status":  "pending",
		"message": fmt.Sprintf("Awaiting approval for: %s", action),
		"action":  action,
		"reason":  reason,
		"details": details,
		// The function_call_id is used to match the response
		"approval_id": ctx.FunctionCallID(),
	}, nil
}

// Ensure ApprovalTool implements CallableTool
var _ tool.CallableTool = (*ApprovalTool)(nil)
