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

package guardrails

import (
	"github.com/a2aproject/a2a-go/a2a"

	"github.com/kadirpekel/hector/pkg/agent"
	"github.com/kadirpekel/hector/pkg/agent/llmagent"
	"github.com/kadirpekel/hector/pkg/model"
	"github.com/kadirpekel/hector/pkg/tool"
)

// ToBeforeAgentCallback converts an input guardrail chain to BeforeAgentCallback.
// If the chain blocks, returns a message with the block reason.
func ToBeforeAgentCallback(chain *InputChain) agent.BeforeAgentCallback {
	return func(ctx agent.CallbackContext) (*a2a.Message, error) {
		// Get user input from context
		userContent := ctx.UserContent()
		if userContent == nil {
			return nil, nil
		}

		// Extract text from user content (a2a.Message)
		var inputText string
		for _, part := range userContent.Parts {
			if tp, ok := part.(a2a.TextPart); ok {
				inputText += tp.Text
			}
		}

		if inputText == "" {
			return nil, nil
		}

		// Run the guardrail chain
		result, err := chain.Check(ctx, inputText)
		if err != nil {
			return nil, err
		}

		if result.IsBlocking() {
			// Return a message to short-circuit the agent
			return a2a.NewMessage(a2a.MessageRoleAgent,
				a2a.TextPart{Text: "I cannot process this request. " + result.Reason},
			), nil
		}

		// If input was modified, we'd need to modify the context
		// For now, modifications are logged but the original is used
		// TODO: Support input modification via context

		return nil, nil
	}
}

// ToAfterModelCallback converts an output guardrail chain to AfterModelCallback.
// If the chain modifies output, the modified response is returned.
func ToAfterModelCallback(chain *OutputChain) llmagent.AfterModelCallback {
	return func(ctx agent.CallbackContext, resp *model.Response, respErr error) (*model.Response, error) {
		// Pass through errors
		if respErr != nil {
			return resp, respErr
		}

		// Skip if no response or no content
		if resp == nil || resp.Content == nil {
			return resp, nil
		}

		// Extract text content
		var outputText string
		for _, part := range resp.Content.Parts {
			if tp, ok := part.(a2a.TextPart); ok {
				outputText += tp.Text
			}
		}

		if outputText == "" {
			return resp, nil
		}

		// Run the guardrail chain
		result, err := chain.Check(ctx, outputText)
		if err != nil {
			return nil, err
		}

		if result.IsBlocking() {
			// Return a safe response
			return &model.Response{
				Content: &model.Content{
					Parts: []a2a.Part{a2a.TextPart{Text: "I cannot provide this response. " + result.Reason}},
					Role:  a2a.MessageRoleAgent,
				},
			}, nil
		}

		// Handle modified output
		if result.Action == ActionModify {
			if modified, ok := result.Modified.(string); ok {
				// Create a new response with modified text
				newResponse := *resp
				newResponse.Content = &model.Content{
					Parts: []a2a.Part{a2a.TextPart{Text: modified}},
					Role:  resp.Content.Role,
				}
				return &newResponse, nil
			}
		}

		return resp, nil
	}
}

// ToBeforeToolCallback converts a tool guardrail chain to BeforeToolCallback.
// If the chain blocks, returns an error result.
func ToBeforeToolCallback(chain *ToolChain) llmagent.BeforeToolCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		// Run the guardrail chain
		result, err := chain.Check(ctx, t.Name(), args)
		if err != nil {
			return nil, err
		}

		if result.IsBlocking() {
			// Return error in the result
			return map[string]any{
				"error": result.Reason,
			}, nil
		}

		// Handle modified args
		if result.Action == ActionModify {
			if modified, ok := result.Modified.(map[string]any); ok {
				return modified, nil
			}
		}

		// Continue with original args
		return nil, nil
	}
}
