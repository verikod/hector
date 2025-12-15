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

package llmagent

import (
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"

	"github.com/kadirpekel/hector/pkg/agent"
	"github.com/kadirpekel/hector/pkg/model"
	"github.com/kadirpekel/hector/pkg/tool"
)

// State key prefixes for approval decisions (Issue #2: use constants instead of magic strings)
const (
	approvalStatePrefix     = "_approval:"      // Keyed by tool call ID
	approvalNameStatePrefix = "_approval_name:" // Keyed by tool name
)

// Flow implements the adk-go aligned core reasoning loop.
// Key principles from adk-go:
//  1. Outer loop continues until IsFinalResponse() returns true
//  2. Each runOneStep() handles: preprocess → LLM call → postprocess → tool execution
//  3. Events are yielded and persisted to session immediately
//  4. ContentsRequestProcessor reads from session on EVERY iteration
//  5. No manual message accumulation - session is the source of truth
type Flow struct {
	agent    *llmAgent
	pipeline *Pipeline

	// approvalDecisions stores approval decisions extracted from current user message
	// Key: tool_call_id or tool_name, Value: "approve" | "deny"
	approvalDecisions map[string]string

	// pendingDenialMessages stores denial tool result messages to inject into LLM request
	// These are added when user denies a tool and need to be in the conversation before LLM call
	pendingDenialMessages []*a2a.Message
}

// NewFlow creates a new flow for the given agent.
func NewFlow(a *llmAgent) *Flow {
	return &Flow{
		agent:             a,
		pipeline:          a.pipeline,
		approvalDecisions: make(map[string]string),
	}
}

// Run executes the reasoning loop following adk-go's pattern.
// The outer loop continues until we get a final response (no tool calls).
func (f *Flow) Run(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Extract approval decisions from current user message (legacy pattern)
		// This happens BEFORE tool execution, so decisions are available when needed
		f.extractApprovalDecisions(ctx)

		// If there are denial decisions, prepare denial messages to inject into LLM request
		// These are stored in pendingDenialMessages and injected in runOneStep
		f.preparePendingDenialMessages(ctx)

		// Execute any pending approved tools directly
		// This is critical: when user approves a tool, we must execute it immediately
		// rather than relying on the LLM to re-call it (which it won't)
		f.executePendingApprovedTools(ctx, yield)

		// Outer loop: continues until IsFinalResponse
		// This matches adk-go's Flow.Run pattern
		for iteration := 0; iteration < f.agent.reasoning.MaxIterations; iteration++ {
			// Check context cancellation at start of each iteration (Issue #6)
			// This prevents wasted CPU cycles when context is cancelled
			if ctx.Err() != nil {
				slog.Debug("Flow terminating due to context cancellation",
					"iteration", iteration,
					"error", ctx.Err())
				return
			}

			var lastEvent *agent.Event

			// Inner loop: run one step (LLM call + tool execution)
			for ev, err := range f.runOneStep(ctx) {
				if err != nil {
					yield(nil, err)
					return
				}

				// Forward the event first (yield to caller)
				if !yield(ev, nil) {
					return
				}

				lastEvent = ev
			}

			// Check termination conditions (adk-go pattern)
			if lastEvent == nil || lastEvent.IsFinalResponse() {
				slog.Debug("Flow terminating",
					"reason", "final_response",
					"iteration", iteration,
					"has_event", lastEvent != nil)
				return
			}

			// Safety check for partial responses
			if lastEvent.Partial {
				yield(nil, fmt.Errorf("unexpected partial event at end of step"))
				return
			}
		}

		// Safety limit exceeded
		yield(nil, fmt.Errorf("reasoning loop safety limit exceeded (%d iterations)", f.agent.reasoning.MaxIterations))
	}
}

// runOneStep executes one iteration: preprocess → LLM → postprocess → tools
// This matches adk-go's Flow.runOneStep pattern.
func (f *Flow) runOneStep(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// 1. Create empty request - processors will populate it
		// This is the adk-go pattern: req := &model.LLMRequest{Model: f.Model.Name()}
		req := &model.Request{}

		// 2. Preprocess: run all request processors
		// Key insight from adk-go: ContentsRequestProcessor reads from session.Events()
		// So we don't pass any messages - the processor builds them from session
		procCtx := newProcessorContext(ctx, f.agent)
		if err := f.pipeline.ProcessRequest(procCtx, req); err != nil {
			yield(nil, fmt.Errorf("preprocess failed: %w", err))
			return
		}

		// 2.5. Inject pending denial messages (if any)
		// These are tool result messages for denied tools that need to be in the conversation
		// BEFORE calling the LLM, so it sees the denial and doesn't retry.
		if len(f.pendingDenialMessages) > 0 {
			req.Messages = append(req.Messages, f.pendingDenialMessages...)
			f.pendingDenialMessages = nil // Clear after injection (only inject once)
		}

		// 3. Tool preprocessing (adk-go pattern)
		// Tools can modify the request before LLM call (e.g., RAG context injection)
		if err := f.toolPreprocess(ctx, req); err != nil {
			yield(nil, fmt.Errorf("tool preprocess failed: %w", err))
			return
		}

		// Check if invocation was ended during preprocessing
		if ctx.Ended() {
			return
		}

		// 3. Run before-model callbacks
		stateDelta := make(map[string]any)
		resp, err := f.callLLMWithCallbacks(ctx, req, stateDelta, yield)
		if err != nil {
			yield(nil, err)
			return
		}
		if resp == nil {
			return // Callback handled the response
		}

		// 4. Postprocess: run response processors
		if err := f.pipeline.ProcessResponse(procCtx, req, resp); err != nil {
			yield(nil, fmt.Errorf("postprocess failed: %w", err))
			return
		}

		// 5. Skip if no content and no error (adk-go pattern for code executor)
		// BUT don't skip if there are tool calls - we need to handle them
		if resp.Content == nil && resp.ErrorCode == "" && !resp.HasToolCalls() {
			return
		}

		// 6. Build and yield model response event
		modelEvent := f.buildModelResponseEvent(ctx, resp, stateDelta)
		if !yield(modelEvent, nil) {
			return
		}

		// 7. Handle function/tool calls
		if resp.HasToolCalls() {
			toolResponseEvent, err := f.handleToolCalls(ctx, resp, yield)
			if err != nil {
				yield(nil, err)
				return
			}
			if toolResponseEvent != nil {
				if !yield(toolResponseEvent, nil) {
					return
				}

				// Handle agent transfer (adk-go pattern)
				if toolResponseEvent.Actions.TransferToAgent != "" {
					f.handleAgentTransfer(ctx, toolResponseEvent.Actions.TransferToAgent, yield)
				}
			}
		}
	}
}

// callLLMWithCallbacks handles before/after callbacks and LLM call.
func (f *Flow) callLLMWithCallbacks(
	ctx agent.InvocationContext,
	req *model.Request,
	stateDelta map[string]any,
	yield func(*agent.Event, error) bool,
) (*model.Response, error) {
	// Run before-model callbacks
	for _, cb := range f.agent.beforeModelCallbacks {
		resp, err := cb(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("before-model callback failed: %w", err)
		}
		if resp != nil {
			return resp, nil // Callback provided response, skip LLM call
		}
	}

	// Call LLM
	var finalResp *model.Response
	for resp, err := range f.agent.model.GenerateContent(ctx, req, f.agent.enableStreaming) {
		// Run after-model callbacks
		callbackResp, callbackErr := f.runAfterModelCallbacks(ctx, resp, stateDelta, err)
		if callbackErr != nil {
			return nil, callbackErr
		}
		if callbackResp != nil {
			resp = callbackResp
		}

		if err != nil {
			return nil, fmt.Errorf("LLM generation failed: %w", err)
		}

		if resp == nil {
			continue
		}

		if resp.Partial {
			// Yield partial events for streaming UI
			event := f.buildPartialEvent(ctx, resp)
			if !yield(event, nil) {
				return nil, fmt.Errorf("streaming interrupted")
			}
		} else {
			finalResp = resp
		}
	}

	return finalResp, nil
}

// runAfterModelCallbacks runs after-model callbacks.
func (f *Flow) runAfterModelCallbacks(
	ctx agent.InvocationContext,
	resp *model.Response,
	stateDelta map[string]any,
	llmErr error,
) (*model.Response, error) {
	for _, cb := range f.agent.afterModelCallbacks {
		callbackResp, err := cb(ctx, resp, llmErr)
		if err != nil {
			return nil, fmt.Errorf("after-model callback failed: %w", err)
		}
		if callbackResp != nil {
			return callbackResp, nil
		}
	}
	return nil, nil
}

// buildModelResponseEvent creates an event from LLM response.
func (f *Flow) buildModelResponseEvent(ctx agent.InvocationContext, resp *model.Response, stateDelta map[string]any) *agent.Event {
	// Populate function call IDs if empty (adk-go pattern)
	// Some models don't return IDs, but we need them to match calls with results
	populateFunctionCallIDs(resp)

	event := agent.NewEvent(ctx.InvocationID())
	event.Author = f.agent.DisplayName()
	event.Branch = ctx.Branch()
	event.Partial = false
	event.Actions.StateDelta = stateDelta
	// Add agent_id for Frontend Highlighting (Canvas matches by ID)
	if event.CustomMetadata == nil {
		event.CustomMetadata = make(map[string]any)
	}
	event.CustomMetadata["agent_id"] = f.agent.Name()

	// Build message from response
	if resp.Content != nil {
		event.Message = a2a.NewMessage(a2a.MessageRoleAgent, resp.Content.Parts...)
	}

	// Add tool calls to event
	if len(resp.ToolCalls) > 0 {
		var parts []a2a.Part
		for _, tc := range resp.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, agent.ToolCallState{
				ID:     tc.ID,
				Name:   tc.Name,
				Args:   tc.Args,
				Status: "working",
			})
			parts = append(parts, a2a.DataPart{
				Data: map[string]any{
					"type":      "tool_use",
					"id":        tc.ID,
					"name":      tc.Name,
					"arguments": tc.Args,
				},
			})
		}

		// Prepend any text content
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if tp, ok := part.(a2a.TextPart); ok && tp.Text != "" {
					parts = append([]a2a.Part{part}, parts...)
				}
			}
		}
		event.Message = a2a.NewMessage(a2a.MessageRoleAgent, parts...)
	}

	// Add thinking state and save to CustomMetadata for multi-turn reconstruction
	// CRITICAL: Anthropic requires thinking signatures for multi-turn conversations
	// The signature must be preserved so reconstructMessageWithThinking() can rebuild
	if resp.Thinking != nil && resp.Thinking.Content != "" {
		thinkingID := resp.Thinking.ID
		if thinkingID == "" {
			thinkingID = "thinking_" + uuid.NewString()[:8]
		}
		event.Thinking = &agent.ThinkingState{
			ID:        thinkingID,
			Status:    "completed",
			Content:   resp.Thinking.Content,
			Signature: resp.Thinking.Signature,
			Type:      "default",
		}

		// Save to CustomMetadata for multi-turn thinking reconstruction
		if event.CustomMetadata == nil {
			event.CustomMetadata = make(map[string]any)
		}
		event.CustomMetadata["thinking"] = resp.Thinking.Content
		if resp.Thinking.Signature != "" {
			event.CustomMetadata["thinking_signature"] = resp.Thinking.Signature
		}
	}

	// OutputKey: Save agent output to session state if configured
	if f.agent.outputKey != "" && resp.Content != nil {
		if text := resp.TextContent(); text != "" {
			event.Actions.StateDelta[f.agent.outputKey] = text
		}
	}

	// NOTE: Don't set LongRunningToolIDs here. It's set in handleToolCalls
	// when we actually determine that approval is needed (via RequiresApproval()).
	// Setting it here would trigger HITL detection too early.

	return event
}

// buildPartialEvent creates a partial streaming event.
// This is called during streaming for each delta (text, thinking, tool calls).
// The event metadata is used by the UI to create and update widgets.
func (f *Flow) buildPartialEvent(ctx agent.InvocationContext, resp *model.Response) *agent.Event {
	event := agent.NewEvent(ctx.InvocationID())
	event.Author = f.agent.DisplayName()
	event.Branch = ctx.Branch()
	event.Partial = true
	// Add agent_id for Frontend Highlighting
	if event.CustomMetadata == nil {
		event.CustomMetadata = make(map[string]any)
	}
	event.CustomMetadata["agent_id"] = f.agent.Name()

	// Collect parts for the message
	var parts []a2a.Part

	// Add thinking content as a Part (legacy pattern: thinking streams as Parts)
	// This ensures thinking appears in artifact.parts for proper UI ordering
	if resp.Thinking != nil && resp.Thinking.Content != "" {
		thinkingID := resp.Thinking.ID
		if thinkingID == "" {
			thinkingID = "thinking_" + uuid.NewString()[:8]
		}

		// Create thinking Part with type marker so UI can identify it
		parts = append(parts, a2a.DataPart{
			Data: map[string]any{
				"type":    "thinking",
				"id":      thinkingID,
				"content": resp.Thinking.Content,
				"status":  "active",
			},
		})

		// Also set event.Thinking for metadata
		event.Thinking = &agent.ThinkingState{
			ID:      thinkingID,
			Status:  "active",
			Content: resp.Thinking.Content,
			Type:    "default",
		}
	}

	// Add text content Parts and tool call Parts
	// NOTE: Tool calls are already included in Content.Parts by the aggregator.
	// We deduplicate tool_use parts here to handle cases where Gemini might send
	// the same FunctionCall part multiple times in Content.Parts (needs verification).
	if resp.Content != nil {
		// Track tool call parts we've already added to prevent duplicates
		// Key: tool call ID (or name+args hash if ID is empty)
		seenToolCallParts := make(map[string]bool)

		for _, part := range resp.Content.Parts {
			// Check if this is a duplicate tool_use part
			shouldSkip := false
			if dp, ok := part.(a2a.DataPart); ok {
				if data, ok := dp.Data["type"].(string); ok && data == "tool_use" {
					id := getString(dp.Data, "id")
					name := getString(dp.Data, "name")
					args := getMap(dp.Data, "arguments")

					// Create deduplication key: use ID if present, otherwise name+args
					// Use stable JSON serialization for args to ensure deterministic keys (Issue #8)
					dedupKey := id
					if dedupKey == "" {
						// For empty IDs, use name+args as key with stable JSON serialization
						argsJSON, _ := json.Marshal(args) // Marshal sorts map keys alphabetically
						dedupKey = fmt.Sprintf("%s|%s", name, string(argsJSON))
					}

					// Skip if we've already seen this exact tool call part
					if seenToolCallParts[dedupKey] {
						shouldSkip = true
					} else {
						seenToolCallParts[dedupKey] = true

						// Extract for event metadata
						event.ToolCalls = append(event.ToolCalls, agent.ToolCallState{
							ID:     id,
							Name:   name,
							Args:   args,
							Status: "working",
						})
					}
				}
			}

			// Add the part only if not a duplicate tool call
			if !shouldSkip {
				parts = append(parts, part)
			}
		}
	}

	// If tool calls weren't in Content.Parts (legacy/non-streaming case), add them
	// This handles cases where ToolCalls exist but aren't in Content.Parts yet
	if len(resp.ToolCalls) > 0 && len(event.ToolCalls) == 0 {
		for _, tc := range resp.ToolCalls {
			parts = append(parts, a2a.DataPart{
				Data: map[string]any{
					"type":      "tool_use",
					"id":        tc.ID,
					"name":      tc.Name,
					"arguments": tc.Args,
				},
			})

			event.ToolCalls = append(event.ToolCalls, agent.ToolCallState{
				ID:     tc.ID,
				Name:   tc.Name,
				Args:   tc.Args,
				Status: "working",
			})
		}
	}

	// Create message with all parts
	if len(parts) > 0 {
		event.Message = a2a.NewMessage(a2a.MessageRoleAgent, parts...)
	}

	return event
}

// handleToolCalls executes tool calls and returns a merged response event.
// This follows adk-go's handleFunctionCalls pattern.
//
// Supports three tool execution patterns:
//  1. Regular tools (CallableTool): Execute immediately, return result
//  2. Streaming tools (StreamingTool): Yield partial events, return final result
//  3. HITL tools (RequiresApproval): Return pending status, pause for human approval
func (f *Flow) handleToolCalls(ctx agent.InvocationContext, resp *model.Response, yield func(*agent.Event, error) bool) (*agent.Event, error) {
	if len(resp.ToolCalls) == 0 {
		return nil, nil
	}

	var toolResultParts []a2a.Part
	var toolResults []agent.ToolResultState
	var longRunningToolIDs []string
	var requiresInput bool
	var inputPrompt string
	mergedActions := &agent.EventActions{StateDelta: make(map[string]any)}

	for _, tc := range resp.ToolCalls {
		t := f.agent.findTool(ctx, tc.Name)

		var resultStr string
		var isError bool
		var status string

		if t == nil {
			resultStr = fmt.Sprintf("Error: tool %q not found", tc.Name)
			isError = true
			status = "failed"
		} else if t.RequiresApproval() {
			// HITL tool - check for approval decision first
			// Check by tool call ID first (exact match), then by tool name (for new tool calls with different IDs)
			slog.Debug("Checking approval for HITL tool", "tool", tc.Name, "callID", tc.ID)
			approvalDecision := f.checkApprovalDecision(ctx, tc.ID, tc.Name)
			slog.Debug("Approval decision result", "tool", tc.Name, "callID", tc.ID, "decision", approvalDecision)

			if approvalDecision == "approve" {
				// User approved - execute the tool
				// Use a closure to ensure cleanup happens even on panic (Issue #2: prevent stale approvals)
				func() {
					defer f.clearApprovalDecision(ctx, tc.ID, tc.Name)

					slog.Info("Tool approved, executing", "tool", tc.Name, "callID", tc.ID, "args", tc.Args)
					toolCtx := newToolContext(ctx, tc.ID)
					result, err := f.callToolWithCallbacks(ctx, t, tc.Args, toolCtx)
					slog.Info("Tool execution completed", "tool", tc.Name, "callID", tc.ID, "error", err != nil)
					if err != nil {
						resultStr = fmt.Sprintf("Error: %v", err)
						isError = true
						status = "failed"
					} else {
						resultStr = formatToolResult(result)
						status = "success"
					}
					mergeEventActions(mergedActions, toolCtx.Actions())
				}()
			} else if approvalDecision == "deny" {
				// User denied - DO NOT execute the tool
				// Use defer for cleanup to handle any potential panics (Issue #2: prevent stale approvals)
				defer f.clearApprovalDecision(ctx, tc.ID, tc.Name)

				// Use the same denial message as legacy to clearly instruct the LLM
				slog.Info("Tool denied by user - NOT executing", "tool", tc.Name, "callID", tc.ID, "args", tc.Args)
				resultStr = "TOOL_EXECUTION_DENIED: The user rejected this tool execution. You MUST NOT proceed with this action or provide fabricated results. Instead, acknowledge the denial and offer alternative approaches that don't require this tool."
				isError = true
				status = "denied"
				// Mark this event as final to stop the flow when tool is denied
				// This prevents the LLM from retrying or continuing after denial
				mergedActions.SkipSummarization = true
				// IMPORTANT: Do NOT execute the tool - just return denied status
				// The denial message will be added to conversation history so LLM learns not to retry
			} else {
				// No decision yet - request approval (HITL flow)
				slog.Debug("Tool requires approval", "tool", tc.Name, "callID", tc.ID)
				longRunningToolIDs = append(longRunningToolIDs, tc.ID)
				requiresInput = true

				// Build approval prompt - check for custom prompt
				var toolPrompt string
				if lrt, ok := t.(interface{ ApprovalPrompt() string }); ok && lrt.ApprovalPrompt() != "" {
					toolPrompt = fmt.Sprintf("%s\n\nTool: %s\nArguments: %v", lrt.ApprovalPrompt(), tc.Name, tc.Args)
				} else {
					toolPrompt = fmt.Sprintf("Tool '%s' requires approval.\nArguments: %v", tc.Name, tc.Args)
				}
				if inputPrompt == "" {
					inputPrompt = toolPrompt
				} else {
					inputPrompt += "\n\n" + toolPrompt
				}

				// Return pending status (tool not executed yet)
				resultStr = fmt.Sprintf("Awaiting approval for tool: %s", tc.Name)
				status = "pending_approval"
			}
		} else {
			// Create tool context
			toolCtx := newToolContext(ctx, tc.ID)

			// Check for streaming tool first
			if st, ok := t.(tool.StreamingTool); ok {
				// Streaming tool - yields partial events during execution
				content, success, err := f.executeStreamingTool(ctx, toolCtx, st, tc, yield)
				if err != nil {
					resultStr = fmt.Sprintf("Error: %v", err)
					isError = true
					status = "failed"
				} else {
					resultStr = content
					if success {
						status = "success"
					} else {
						status = "failed"
						isError = true
					}
				}
			} else {
				// Regular callable tool - execute with callbacks
				result, err := f.callToolWithCallbacks(ctx, t, tc.Args, toolCtx)
				if err != nil {
					resultStr = fmt.Sprintf("Error: %v", err)
					isError = true
					status = "failed"
				} else {
					resultStr = formatToolResult(result)
					status = "success"
				}
			}

			// Merge actions from tool context
			mergeEventActions(mergedActions, toolCtx.Actions())
		}

		// Track tool result for UI
		toolResults = append(toolResults, agent.ToolResultState{
			ToolCallID: tc.ID,
			Content:    resultStr,
			Status:     status,
			IsError:    isError,
		})

		// Build tool result part
		toolResultParts = append(toolResultParts, a2a.DataPart{
			Data: map[string]any{
				"type":              "tool_result",
				"tool_call_id":      tc.ID,
				"tool_name":         tc.Name,
				"content":           resultStr,
				"is_error":          isError,
				"requires_approval": t != nil && t.RequiresApproval(),
			},
		})
	}

	// Build merged tool response event (adk-go pattern)
	event := agent.NewEvent(ctx.InvocationID())
	event.Author = f.agent.DisplayName()
	event.Branch = ctx.Branch()
	event.Partial = false
	// Add agent_id for Frontend Highlighting
	if event.CustomMetadata == nil {
		event.CustomMetadata = make(map[string]any)
	}
	event.CustomMetadata["agent_id"] = f.agent.Name()
	event.ToolResults = toolResults
	event.Message = a2a.NewMessage(a2a.MessageRoleUser, toolResultParts...)
	event.Actions = *mergedActions

	slog.Debug("handleToolCalls created event", "agent", f.agent.Name(), "tool_results", len(toolResults))

	// Set HITL signals if any long-running tools
	if len(longRunningToolIDs) > 0 {
		event.LongRunningToolIDs = longRunningToolIDs
	}
	if requiresInput {
		event.Actions.RequireInput = true
		event.Actions.InputPrompt = inputPrompt
	}

	return event, nil
}

// executeStreamingTool executes a streaming tool and yields partial events.
// Each chunk from the tool triggers a partial event to update the UI in real-time.
func (f *Flow) executeStreamingTool(
	ctx agent.InvocationContext,
	toolCtx tool.Context,
	st tool.StreamingTool,
	tc tool.ToolCall,
	yield func(*agent.Event, error) bool,
) (string, bool, error) {
	startTime := time.Now()

	var accumulated string
	var finalResult *tool.Result
	var execError error

	// Run before-tool callbacks
	for _, cb := range f.agent.beforeToolCallbacks {
		result, err := cb(toolCtx, st, tc.Args)
		if err != nil {
			return "", false, fmt.Errorf("before-tool callback failed: %w", err)
		}
		if result != nil {
			// Callback provided result, skip tool execution
			duration := time.Since(startTime)
			if f.agent.metricsRecorder != nil {
				f.agent.metricsRecorder.RecordToolCall(st.Name(), duration)
			}
			return formatToolResult(result), true, nil
		}
	}

	// Iterate over streaming results
	for result, err := range st.CallStreaming(toolCtx, tc.Args) {
		if err != nil {
			execError = err
			break
		}

		if result == nil {
			continue
		}

		// Handle streaming chunks (intermediate results)
		if result.Streaming {
			content := fmt.Sprintf("%v", result.Content)
			accumulated += content

			slog.Debug("Streaming tool chunk",
				"tool", st.Name(),
				"chunk_size", len(content),
				"accumulated_size", len(accumulated))

			// Yield partial event for real-time UI update
			event := agent.NewEvent(ctx.InvocationID())
			event.Author = f.agent.DisplayName()
			event.Branch = ctx.Branch()
			event.Partial = true // Partial - for UI only, not persisted
			// Add agent_id for Frontend Highlighting
			if event.CustomMetadata == nil {
				event.CustomMetadata = make(map[string]any)
			}
			event.CustomMetadata["agent_id"] = f.agent.Name()

			// Update tool result with accumulated content
			// This is sent to UI via metadata.tool_results for streaming updates
			event.ToolResults = []agent.ToolResultState{{
				ToolCallID: tc.ID,
				Content:    accumulated,
				Status:     "working",
				IsError:    false,
			}}

			slog.Debug("Yielding streaming tool event",
				"tool", st.Name(),
				"partial", true,
				"content_len", len(accumulated))

			if !yield(event, nil) {
				return accumulated, false, fmt.Errorf("streaming interrupted")
			}
		} else {
			// Final result
			finalResult = result
		}
	}

	// Determine final content and status
	var finalContent string
	var success bool

	if execError != nil {
		finalContent = fmt.Sprintf("Error: %v", execError)
		success = false
	} else if finalResult != nil {
		finalContent = fmt.Sprintf("%v", finalResult.Content)
		success = finalResult.Error == ""
		if finalResult.Error != "" {
			finalContent = fmt.Sprintf("Error: %s\n%s", finalResult.Error, finalContent)
		}
	} else {
		// No final result, use accumulated content
		finalContent = accumulated
		success = true
	}

	duration := time.Since(startTime)

	// Record metrics
	if f.agent.metricsRecorder != nil {
		f.agent.metricsRecorder.RecordToolCall(st.Name(), duration)
		if execError != nil {
			f.agent.metricsRecorder.RecordToolError(st.Name(), "execution_error")
		}
	}

	// Run after-tool callbacks
	resultMap := map[string]any{"content": finalContent}
	for _, cb := range f.agent.afterToolCallbacks {
		callbackResult, err := cb(toolCtx, st, tc.Args, resultMap, nil)
		if err != nil {
			return "", false, fmt.Errorf("after-tool callback failed: %w", err)
		}
		if callbackResult != nil {
			finalContent = formatToolResult(callbackResult)
		}
	}

	return finalContent, success, nil
}

// callToolWithCallbacks executes a tool with before/after callbacks.
func (f *Flow) callToolWithCallbacks(
	ctx agent.InvocationContext,
	t tool.Tool,
	args map[string]any,
	toolCtx tool.Context,
) (map[string]any, error) {
	startTime := time.Now()

	// Run before-tool callbacks
	for _, cb := range f.agent.beforeToolCallbacks {
		result, err := cb(toolCtx, t, args)
		if err != nil {
			return nil, fmt.Errorf("before-tool callback failed: %w", err)
		}
		if result != nil {
			// Callback provided result, skip tool execution
			// Still record metrics for callback execution
			duration := time.Since(startTime)
			if f.agent.metricsRecorder != nil {
				f.agent.metricsRecorder.RecordToolCall(t.Name(), duration)
			}
			return result, nil
		}
	}

	// Execute tool
	var result map[string]any
	var toolErr error

	if callable, ok := t.(tool.CallableTool); ok {
		result, toolErr = callable.Call(toolCtx, args)
	} else if streaming, ok := t.(tool.StreamingTool); ok {
		// Handle streaming tools by collecting all results
		var finalResult *tool.Result
		for res, err := range streaming.CallStreaming(toolCtx, args) {
			if err != nil {
				toolErr = err
				break
			}
			// Keep track of the final result (non-streaming)
			if res != nil && !res.Streaming {
				finalResult = res
			}
		}
		if finalResult != nil {
			result = map[string]any{
				"content":  finalResult.Content,
				"metadata": finalResult.Metadata,
			}
			if finalResult.Error != "" {
				result["error"] = finalResult.Error
			}
		}
	} else {
		return nil, fmt.Errorf("tool %q is not callable", t.Name())
	}

	duration := time.Since(startTime)

	// Record metrics
	if f.agent.metricsRecorder != nil {
		f.agent.metricsRecorder.RecordToolCall(t.Name(), duration)
		if toolErr != nil {
			f.agent.metricsRecorder.RecordToolError(t.Name(), "execution_error")
		}
	}

	// Run after-tool callbacks
	for _, cb := range f.agent.afterToolCallbacks {
		callbackResult, err := cb(toolCtx, t, args, result, toolErr)
		if err != nil {
			return nil, fmt.Errorf("after-tool callback failed: %w", err)
		}
		if callbackResult != nil {
			result = callbackResult
		}
	}

	return result, toolErr
}

// handleAgentTransfer handles transfer to another agent.
func (f *Flow) handleAgentTransfer(ctx agent.InvocationContext, agentName string, yield func(*agent.Event, error) bool) {
	// Find the target agent in sub-agents
	var targetAgent agent.Agent
	for _, sub := range f.agent.SubAgents() {
		if sub.Name() == agentName {
			targetAgent = sub
			break
		}
	}

	if targetAgent == nil {
		yield(nil, fmt.Errorf("transfer target agent not found: %s", agentName))
		return
	}

	// Run the target agent and forward events
	for ev, err := range targetAgent.Run(ctx) {
		if !yield(ev, err) || err != nil {
			return
		}
	}
}

// findLongRunningToolIDs returns IDs of long-running tool calls.
//
//nolint:unused // Reserved for future use
func (f *Flow) findApprovalRequiredToolIDs(resp *model.Response) []string {
	var ids []string
	for _, tc := range resp.ToolCalls {
		t := f.agent.findTool(nil, tc.Name)
		if t != nil && t.RequiresApproval() {
			ids = append(ids, tc.ID)
		}
	}
	return ids
}

// formatToolResult converts tool result to string.
func formatToolResult(result map[string]any) string {
	if result == nil {
		return ""
	}

	// Extract content field if present (from streaming tools)
	if content, ok := result["content"]; ok {
		switch v := content.(type) {
		case string:
			// Trim whitespace and ensure we have valid content
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				return "(no output)"
			}
			return trimmed
		default:
			return fmt.Sprintf("%v", v)
		}
	}

	// Fallback: simple string conversion
	return fmt.Sprintf("%v", result)
}

// extractApprovalDecisions extracts approval decisions from current user message and stores them in session state.
// This follows the legacy pattern where decisions are extracted BEFORE tool execution and stored in context/state.
// We store in session state (not Flow state) because Flow instances are recreated per request.
func (f *Flow) extractApprovalDecisions(ctx agent.InvocationContext) {
	f.approvalDecisions = make(map[string]string)

	userContent := ctx.UserContent()
	if userContent == nil || len(userContent.Parts) == 0 {
		return
	}

	state := ctx.Session().State()
	if state == nil {
		return
	}

	for _, part := range userContent.Parts {
		if dp, ok := part.(a2a.DataPart); ok {
			if partType, ok := dp.Data["type"].(string); ok && partType == "tool_approval" {
				decision, _ := dp.Data["decision"].(string)
				toolCallID, _ := dp.Data["tool_call_id"].(string)
				toolName, _ := dp.Data["tool_name"].(string)

				if decision == "approve" || decision == "deny" {
					// Store in Flow state for immediate use
					if toolCallID != "" {
						f.approvalDecisions["id:"+toolCallID] = decision
					}
					if toolName != "" {
						f.approvalDecisions["name:"+toolName] = decision
					}

					// Also store in session state for persistence across Flow instances
					if toolCallID != "" {
						_ = state.Set(approvalStatePrefix+toolCallID, decision)
					}
					if toolName != "" {
						_ = state.Set(approvalNameStatePrefix+toolName, decision)
					}

					slog.Debug("Extracted approval decision", "tool", toolName, "callID", toolCallID, "decision", decision)
				}
			}
		}
	}
}

// preparePendingDenialMessages finds pending tool calls from previous requests that were denied
// and prepares denial tool result messages to inject into the LLM request.
// These messages are stored in pendingDenialMessages and injected in runOneStep.
// This matches legacy behavior where denied tools get tool results added to the conversation.
func (f *Flow) preparePendingDenialMessages(ctx agent.InvocationContext) {
	f.pendingDenialMessages = nil

	// Check if there are any denial decisions
	hasDenial := false
	for _, decision := range f.approvalDecisions {
		if decision == "deny" {
			hasDenial = true
			break
		}
	}
	if !hasDenial {
		return
	}

	session := ctx.Session()
	if session == nil {
		return
	}

	events := session.Events()
	if events == nil {
		return
	}

	var deniedToolResultParts []a2a.Part

	// Find tool calls in agent messages that match denied decisions
	for i := events.Len() - 1; i >= 0; i-- {
		event := events.At(i)
		if event == nil || event.Message == nil || event.Message.Role != a2a.MessageRoleAgent {
			continue
		}

		for _, part := range event.Message.Parts {
			dp, ok := part.(a2a.DataPart)
			if !ok {
				continue
			}
			partType, _ := dp.Data["type"].(string)
			if partType != "tool_use" {
				continue
			}

			toolCallID, _ := dp.Data["id"].(string)
			toolName, _ := dp.Data["name"].(string)

			// Check if this tool call was denied
			if f.checkApprovalDecision(ctx, toolCallID, toolName) != "deny" {
				continue
			}

			// Check if a final result already exists (skip pending_approval results)
			if f.hasFinalToolResult(events, i, toolCallID) {
				continue
			}

			// Create denial result with legacy message
			deniedToolResultParts = append(deniedToolResultParts, a2a.DataPart{
				Data: map[string]any{
					"type":         "tool_result",
					"tool_call_id": toolCallID,
					"tool_name":    toolName,
					"content":      "TOOL_EXECUTION_DENIED: The user rejected this tool execution. You MUST NOT proceed with this action or provide fabricated results. Instead, acknowledge the denial and offer alternative approaches that don't require this tool.",
					"is_error":     true,
				},
			})
			slog.Info("Prepared denial for tool", "tool", toolName, "callID", toolCallID)
		}
	}

	if len(deniedToolResultParts) > 0 {
		f.pendingDenialMessages = append(f.pendingDenialMessages, a2a.NewMessage(a2a.MessageRoleUser, deniedToolResultParts...))
	}
}

// executePendingApprovedTools finds tool calls that were pending approval and are now approved,
// executes them directly, and yields the results as events.
// Returns true if any tools were executed (caller should continue with normal flow after).
func (f *Flow) executePendingApprovedTools(ctx agent.InvocationContext, yield func(*agent.Event, error) bool) bool {
	// Check if there are any approval decisions
	hasApproval := false
	for _, decision := range f.approvalDecisions {
		if decision == "approve" {
			hasApproval = true
			break
		}
	}
	if !hasApproval {
		return false
	}

	session := ctx.Session()
	if session == nil {
		return false
	}

	events := session.Events()
	if events == nil {
		return false
	}

	// Find pending tool calls that are now approved
	type pendingTool struct {
		toolCallID string
		toolName   string
		args       map[string]any
		eventIndex int
	}
	var pendingTools []pendingTool

	for i := events.Len() - 1; i >= 0; i-- {
		event := events.At(i)
		if event == nil || event.Message == nil || event.Message.Role != a2a.MessageRoleAgent {
			continue
		}

		for _, part := range event.Message.Parts {
			dp, ok := part.(a2a.DataPart)
			if !ok {
				continue
			}
			partType, _ := dp.Data["type"].(string)
			if partType != "tool_use" {
				continue
			}

			toolCallID, _ := dp.Data["id"].(string)
			toolName, _ := dp.Data["name"].(string)
			args, _ := dp.Data["arguments"].(map[string]any)

			// Check if this tool call is now approved
			if f.checkApprovalDecision(ctx, toolCallID, toolName) != "approve" {
				continue
			}

			// Check if already has a final result
			if f.hasFinalToolResult(events, i, toolCallID) {
				continue
			}

			pendingTools = append(pendingTools, pendingTool{
				toolCallID: toolCallID,
				toolName:   toolName,
				args:       args,
				eventIndex: i,
			})
		}
	}

	if len(pendingTools) == 0 {
		return false
	}

	// Execute each pending approved tool
	for _, pt := range pendingTools {
		t := f.agent.findTool(ctx, pt.toolName)
		if t == nil {
			slog.Warn("Pending approved tool not found", "tool", pt.toolName)
			continue
		}

		slog.Info("Executing pending approved tool", "tool", pt.toolName, "callID", pt.toolCallID)

		// Create tool context
		toolCtx := newToolContext(ctx, pt.toolCallID)

		// Cleanup approval decision using defer to handle panics (Issue #2: prevent stale approvals)
		defer f.clearApprovalDecision(ctx, pt.toolCallID, pt.toolName)

		// Execute the tool (with streaming support)
		var resultStr string
		var isError bool
		var status string

		// Check for streaming tool first
		if st, ok := t.(tool.StreamingTool); ok {
			// Streaming tool - yields partial events during execution
			var success bool
			var err error
			resultStr, success, err = f.executeStreamingTool(ctx, toolCtx, st, tool.ToolCall{
				ID:   pt.toolCallID,
				Name: pt.toolName,
				Args: pt.args,
			}, yield)
			if err != nil {
				resultStr = fmt.Sprintf("Error: %v", err)
				isError = true
				status = "failed"
			} else {
				status = "success"
				if !success {
					status = "failed"
					isError = true
				}
			}
		} else {
			// Regular callable tool - execute with callbacks
			result, err := f.callToolWithCallbacks(ctx, t, pt.args, toolCtx)
			if err != nil {
				resultStr = fmt.Sprintf("Error: %v", err)
				isError = true
				status = "failed"
			} else {
				resultStr = formatToolResult(result)
				status = "success"
			}
		}

		slog.Info("Pending approved tool executed", "tool", pt.toolName, "callID", pt.toolCallID, "status", status, "result", resultStr)

		// Build and yield the tool result event
		event := agent.NewEvent(ctx.InvocationID())
		// Tool results should appear as user messages so the LLM can pair them
		// with the prior assistant tool_use call (Anthropic/OpenAI pattern).
		event.Author = agent.AuthorUser
		event.Branch = ctx.Branch()
		event.Partial = false
		if toolCtx.Actions() != nil {
			event.Actions = *toolCtx.Actions()
		}

		// Add tool result to message as a user role
		event.Message = a2a.NewMessage(a2a.MessageRoleUser, a2a.DataPart{
			Data: map[string]any{
				"type":              "tool_result",
				"tool_call_id":      pt.toolCallID,
				"tool_name":         pt.toolName,
				"content":           resultStr,
				"is_error":          isError,
				"requires_approval": false,
			},
		})

		// Add tool result state
		event.ToolResults = []agent.ToolResultState{{
			ToolCallID: pt.toolCallID,
			Content:    resultStr,
			Status:     status,
		}}

		// Yield the event (runner will persist to session)
		if !yield(event, nil) {
			return true
		}
	}

	return true
}

// hasFinalToolResult checks if a final tool result (denied/success/failed) exists for a tool call.
func (f *Flow) hasFinalToolResult(events agent.Events, fromIndex int, toolCallID string) bool {
	for j := fromIndex + 1; j < events.Len(); j++ {
		event := events.At(j)
		if event == nil || event.Message == nil {
			continue
		}

		for _, part := range event.Message.Parts {
			dp, ok := part.(a2a.DataPart)
			if !ok {
				continue
			}
			if dp.Data["type"] != "tool_result" {
				continue
			}
			if dp.Data["tool_call_id"] != toolCallID {
				continue
			}

			// Check if this is a final result
			if event.ToolResults != nil {
				for _, tr := range event.ToolResults {
					if tr.ToolCallID == toolCallID {
						if tr.Status == "denied" || tr.Status == "success" || tr.Status == "failed" {
							return true
						}
					}
				}
			}
			if status, ok := dp.Data["status"].(string); ok {
				if status == "denied" || status == "success" || status == "failed" {
					return true
				}
			}
		}
	}
	return false
}

// checkApprovalDecision checks stored approval decisions.
// Returns "approve", "deny", or "" (no decision yet).
// Checks Flow state first (current request), then session state (persisted across requests).
func (f *Flow) checkApprovalDecision(ctx agent.InvocationContext, toolCallID string, toolName string) string {
	// Check Flow state first (decisions extracted by extractApprovalDecisions at start of Run)
	if toolCallID != "" {
		if decision, ok := f.approvalDecisions["id:"+toolCallID]; ok {
			return decision
		}
	}
	if toolName != "" {
		if decision, ok := f.approvalDecisions["name:"+toolName]; ok {
			return decision
		}
	}

	// Fallback: check session state (for decisions from previous requests)
	state := ctx.Session().State()
	if state != nil {
		if toolCallID != "" {
			if val, err := state.Get(approvalStatePrefix + toolCallID); err == nil {
				if decision, ok := val.(string); ok && (decision == "approve" || decision == "deny") {
					return decision
				}
			}
		}
		if toolName != "" {
			if val, err := state.Get(approvalNameStatePrefix + toolName); err == nil {
				if decision, ok := val.(string); ok && (decision == "approve" || decision == "deny") {
					return decision
				}
			}
		}
	}

	return ""
}

// clearApprovalDecision removes approval decisions from Flow state and session state
// after a tool has been executed or denied. This prevents stale approvals from
// accumulating and causing confusion if the same tool is called again later.
func (f *Flow) clearApprovalDecision(ctx agent.InvocationContext, toolCallID string, toolName string) {
	// Clear from Flow state
	if toolCallID != "" {
		delete(f.approvalDecisions, "id:"+toolCallID)
	}
	if toolName != "" {
		delete(f.approvalDecisions, "name:"+toolName)
	}

	// Clear from session state using Delete() for proper cleanup (Issue #10)
	state := ctx.Session().State()
	if state != nil {
		if toolCallID != "" {
			_ = state.Delete(approvalStatePrefix + toolCallID)
		}
		if toolName != "" {
			_ = state.Delete(approvalNameStatePrefix + toolName)
		}
	}
}

// toolPreprocess runs ProcessRequest on tools that implement RequestProcessor.
// This follows adk-go's toolPreprocess pattern, allowing tools to modify
// the LLM request before it's sent (e.g., RAG context injection).
func (f *Flow) toolPreprocess(ctx agent.InvocationContext, req *model.Request) error {
	tools := f.agent.collectTools(ctx)

	// Create a tool request view for processors
	toolReq := &tool.Request{
		SystemInstruction: req.SystemInstruction,
		Messages:          req.Messages,
		Config:            req.Config,
		Metadata:          make(map[string]any),
	}

	for _, t := range tools {
		processor, ok := t.(tool.RequestProcessor)
		if !ok {
			continue // Tool doesn't implement preprocessing
		}

		toolCtx := newToolContext(ctx, "")
		if err := processor.ProcessRequest(toolCtx, toolReq); err != nil {
			return fmt.Errorf("tool %q preprocessing failed: %w", t.Name(), err)
		}
	}

	// Apply any modifications back to the request
	// 1. System instruction changes (e.g., adding context)
	if toolReq.SystemInstruction != req.SystemInstruction {
		req.SystemInstruction = toolReq.SystemInstruction
	}

	// 2. Message changes (e.g., RAG context injection)
	// Tools can modify or add messages for context injection
	if msgs, ok := toolReq.Messages.([]*a2a.Message); ok {
		if len(msgs) != len(req.Messages) || !messagesEqual(msgs, req.Messages) {
			req.Messages = msgs
		}
	}

	// 3. Config changes
	if cfg, ok := toolReq.Config.(*model.GenerateConfig); ok && cfg != nil {
		req.Config = cfg
	}

	return nil
}

// messagesEqual checks if two message slices are equal.
func messagesEqual(a, b []*a2a.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeEventActions merges actions from tool execution (adk-go pattern).
func mergeEventActions(base, other *agent.EventActions) {
	if other == nil {
		return
	}
	if other.SkipSummarization {
		base.SkipSummarization = true
	}
	if other.TransferToAgent != "" {
		base.TransferToAgent = other.TransferToAgent
	}
	if other.Escalate {
		base.Escalate = true
	}
	for k, v := range other.StateDelta {
		base.StateDelta[k] = v
	}
}

// clientFunctionCallIDPrefix is used to identify client-generated function call IDs.
const clientFunctionCallIDPrefix = "hector-"

// populateFunctionCallIDs ensures all tool calls have IDs.
// Some models don't return function call IDs, but we need them to match
// calls with results in the conversation history.
// This follows adk-go's PopulateClientFunctionCallID pattern.
func populateFunctionCallIDs(resp *model.Response) {
	if resp == nil {
		return
	}

	for i := range resp.ToolCalls {
		if resp.ToolCalls[i].ID == "" {
			resp.ToolCalls[i].ID = clientFunctionCallIDPrefix + uuid.NewString()
		}
	}
}
