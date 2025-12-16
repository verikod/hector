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

package server

import (
	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
)

// toHectorContent converts an A2A message to Hector content.
func toHectorContent(msg *a2a.Message) (*agent.Content, error) {
	if msg == nil {
		return nil, nil
	}

	content := &agent.Content{
		Parts: msg.Parts,
		Role:  toHectorRole(msg.Role),
	}

	return content, nil
}

// toHectorRole converts A2A message role to Hector role.
func toHectorRole(role a2a.MessageRole) a2a.MessageRole {
	// A2A roles map directly
	return role
}

// ApprovalResponse represents an approval decision from the user.
type ApprovalResponse struct {
	// Decision is "approve" or "deny"
	Decision string
	// ToolCallID is the ID of the tool call being approved/denied
	ToolCallID string
	// TaskID is the task this approval is for
	TaskID string
}

// ExtractApprovalResponse checks if a message contains an approval response.
// Returns nil if the message is not an approval response.
//
// Approval responses can be:
// 1. A DataPart with type: "tool_approval"
// 2. A TextPart with "approve" or "deny" (for simple approvals)
func ExtractApprovalResponse(msg *a2a.Message) *ApprovalResponse {
	if msg == nil || len(msg.Parts) == 0 {
		return nil
	}

	for _, part := range msg.Parts {
		// Check for structured approval (DataPart)
		if dp, ok := part.(a2a.DataPart); ok {
			if partType, ok := dp.Data["type"].(string); ok && partType == "tool_approval" {
				decision, _ := dp.Data["decision"].(string)
				toolCallID, _ := dp.Data["tool_call_id"].(string)
				taskID, _ := dp.Data["task_id"].(string)
				if decision != "" {
					return &ApprovalResponse{
						Decision:   decision,
						ToolCallID: toolCallID,
						TaskID:     taskID,
					}
				}
			}
		}

		// Check for simple text approval
		if tp, ok := part.(a2a.TextPart); ok {
			text := tp.Text
			if text == "approve" || text == "approved" {
				return &ApprovalResponse{Decision: "approve"}
			}
			if text == "deny" || text == "denied" || text == "reject" {
				return &ApprovalResponse{Decision: "deny"}
			}
		}
	}

	return nil
}
