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
	"context"
	"log/slog"
	"maps"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"

	"github.com/kadirpekel/hector/pkg/agent"
)

// Metadata keys for A2A events
const (
	metaKeyTaskID    = "hector:task_id"
	metaKeyContextID = "hector:context_id"
	metaKeyEscalate  = "hector:escalate"
	metaKeyTransfer  = "hector:transfer_to_agent"
)

// invocationMeta contains metadata for an invocation.
type invocationMeta struct {
	userID    string
	sessionID string
	eventMeta map[string]any
}

func toInvocationMeta(reqCtx *a2asrv.RequestContext) invocationMeta {
	meta := invocationMeta{
		eventMeta: make(map[string]any),
	}

	// Use the context ID provided by a2asrv - this matches the Task's ContextID
	// and ensures session persistence works correctly across server restarts.
	// a2asrv either uses the client-provided context_id or generates one,
	// and stores it in the task for continuations.
	meta.sessionID = reqCtx.ContextID
	slog.Debug("Using a2asrv context as session", "sessionID", meta.sessionID, "taskID", string(reqCtx.TaskID))

	// Include taskId in event metadata for frontend cancellation support
	if reqCtx.TaskID != "" {
		meta.eventMeta["taskId"] = string(reqCtx.TaskID)
	}

	// Extract user ID from message metadata
	if reqCtx.Message != nil && reqCtx.Message.Metadata != nil {
		if uid, ok := reqCtx.Message.Metadata["user_id"].(string); ok {
			meta.userID = uid
		}
	}

	// Default user ID
	if meta.userID == "" {
		meta.userID = "default"
	}

	return meta
}

// eventProcessor translates Hector events to A2A events.
type eventProcessor struct {
	reqCtx *a2asrv.RequestContext
	meta   invocationMeta

	// terminalActions accumulates actions for the terminal event
	terminalActions agent.EventActions

	// responseID is created once first artifact is sent
	responseID a2a.ArtifactID

	// terminalEvents holds potential terminal events by state
	terminalEvents map[a2a.TaskState]*a2a.TaskStatusUpdateEvent
}

func newEventProcessor(reqCtx *a2asrv.RequestContext, meta invocationMeta) *eventProcessor {
	return &eventProcessor{
		reqCtx:         reqCtx,
		meta:           meta,
		terminalEvents: make(map[a2a.TaskState]*a2a.TaskStatusUpdateEvent),
	}
}

func (p *eventProcessor) process(ctx context.Context, event *agent.Event) (*a2a.TaskArtifactUpdateEvent, error) {
	if event == nil {
		return nil, nil
	}

	p.updateTerminalActions(event)

	eventMeta := p.makeEventMeta(event)

	// Check for errors
	// TODO: Check for error indicators in message when event.Message != nil
	_ = event.Message

	// Check for HITL input required - two signals:
	// 1. Event.LongRunningToolIDs (ADK-Go compatible)
	// 2. Event.Actions.RequireInput (Hector extension)
	if len(event.LongRunningToolIDs) > 0 || event.Actions.RequireInput {
		slog.Debug("HITL input required detected",
			"longRunningToolIDs", len(event.LongRunningToolIDs),
			"requireInput", event.Actions.RequireInput,
			"inputPrompt", event.Actions.InputPrompt)
		// Build status message with prompt if available
		var statusMsg *a2a.Message
		if event.Actions.InputPrompt != "" {
			statusMsg = a2a.NewMessageForTask(
				a2a.MessageRoleAgent,
				p.reqCtx,
				a2a.TextPart{Text: event.Actions.InputPrompt},
			)
		}

		ev := a2a.NewStatusUpdateEvent(p.reqCtx, a2a.TaskStateInputRequired, statusMsg)
		ev.Final = true

		// Add HITL metadata for UI
		if ev.Metadata == nil {
			ev.Metadata = make(map[string]any)
		}
		ev.Metadata["input_required"] = true
		if len(event.LongRunningToolIDs) > 0 {
			// Convert []string to []any for A2A Metadata compatibility
			toolIDs := make([]any, len(event.LongRunningToolIDs))
			for i, id := range event.LongRunningToolIDs {
				toolIDs[i] = id
			}
			ev.Metadata["long_running_tool_ids"] = toolIDs
		}
		if event.Actions.InputPrompt != "" {
			ev.Metadata["input_prompt"] = event.Actions.InputPrompt
		}

		p.terminalEvents[a2a.TaskStateInputRequired] = ev
	}

	// Determine if we have content to emit
	// Events can have: message parts, thinking, tool calls, or tool results
	hasParts := event.Message != nil && len(event.Message.Parts) > 0
	hasThinking := event.Thinking != nil && event.Thinking.Content != ""
	hasToolCalls := len(event.ToolCalls) > 0
	hasToolResults := len(event.ToolResults) > 0

	// If no content at all, skip
	if !hasParts && !hasThinking && !hasToolCalls && !hasToolResults {
		return nil, nil
	}

	// Get parts (may be empty for thinking-only events)
	var parts []a2a.Part
	if event.Message != nil {
		parts = event.Message.Parts
	}

	// Create or update artifact
	var result *a2a.TaskArtifactUpdateEvent
	if p.responseID == "" {
		result = a2a.NewArtifactEvent(p.reqCtx, parts...)
		p.responseID = result.Artifact.ID
	} else {
		result = a2a.NewArtifactUpdateEvent(p.reqCtx, p.responseID, parts...)
	}

	// Always include metadata for contextual blocks (thinking, tools)
	if len(eventMeta) > 0 {
		result.Metadata = eventMeta
	}

	return result, nil
}

func (p *eventProcessor) makeTerminalEvents() []a2a.Event {
	result := make([]a2a.Event, 0, 2)

	// Close artifact stream if we sent any artifacts
	if p.responseID != "" {
		ev := a2a.NewArtifactUpdateEvent(p.reqCtx, p.responseID)
		ev.LastChunk = true
		result = append(result, ev)
	}

	// Check for failure or input required (in priority order)
	for _, state := range []a2a.TaskState{a2a.TaskStateFailed, a2a.TaskStateInputRequired} {
		if ev, ok := p.terminalEvents[state]; ok {
			ev.Metadata = p.setActionsMeta(ev.Metadata)
			result = append(result, ev)
			return result
		}
	}

	// Default: completed
	ev := a2a.NewStatusUpdateEvent(p.reqCtx, a2a.TaskStateCompleted, nil)
	ev.Final = true
	ev.Metadata = p.setActionsMeta(maps.Clone(p.meta.eventMeta))
	result = append(result, ev)

	return result
}

func (p *eventProcessor) makeFailedEvent(cause error, event *agent.Event) *a2a.TaskStatusUpdateEvent {
	meta := p.meta.eventMeta
	if event != nil {
		meta = p.makeEventMeta(event)
	}
	return toFailedStatusEvent(p.reqCtx, cause, meta)
}

func (p *eventProcessor) updateTerminalActions(event *agent.Event) {
	p.terminalActions.Escalate = p.terminalActions.Escalate || event.Actions.Escalate
	if event.Actions.TransferToAgent != "" {
		p.terminalActions.TransferToAgent = event.Actions.TransferToAgent
	}
}

func (p *eventProcessor) makeEventMeta(event *agent.Event) map[string]any {
	meta := maps.Clone(p.meta.eventMeta)
	if meta == nil {
		meta = make(map[string]any)
	}

	meta["event_id"] = event.ID
	meta["author"] = event.Author
	if event.Branch != "" {
		meta["branch"] = event.Branch
	}
	// ADK-Go aligned: Pass partial flag to UI for duplicate detection
	// UI should track streamed content and skip final if it matches
	meta["partial"] = event.Partial

	// Add invocation ID for stable widget identification (Fixes duplication issues)
	if event.InvocationID != "" {
		meta["invocation_id"] = event.InvocationID
	}

	// Contextual Blocks - These enable rich UI rendering with proper lifecycle
	// Each block type maps to a specific widget in the UI

	// Thinking block - renders as ThinkingWidget with auto-expand/collapse
	if event.Thinking != nil {
		meta["thinking"] = map[string]any{
			"id":      event.Thinking.ID,
			"status":  event.Thinking.Status,
			"content": event.Thinking.Content,
			"type":    event.Thinking.Type,
		}
		// Include signature for multi-turn verification (Anthropic requirement)
		if event.Thinking.Signature != "" {
			meta["thinking"].(map[string]any)["signature"] = event.Thinking.Signature
		}
	}

	// Tool calls - render as ToolWidget with "working" status
	if len(event.ToolCalls) > 0 {
		toolCalls := make([]map[string]any, len(event.ToolCalls))
		for i, tc := range event.ToolCalls {
			toolCalls[i] = map[string]any{
				"id":     tc.ID,
				"name":   tc.Name,
				"args":   tc.Args,
				"status": tc.Status,
			}
		}
		meta["tool_calls"] = toolCalls
	}

	// Tool results - update ToolWidget to "success"/"failed"
	if len(event.ToolResults) > 0 {
		toolResults := make([]map[string]any, len(event.ToolResults))
		for i, tr := range event.ToolResults {
			toolResults[i] = map[string]any{
				"tool_call_id": tr.ToolCallID,
				"content":      tr.Content,
				"status":       tr.Status,
				"is_error":     tr.IsError,
			}
		}
		meta["tool_results"] = toolResults
	}

	return meta
}

func (p *eventProcessor) setActionsMeta(meta map[string]any) map[string]any {
	if meta == nil {
		meta = make(map[string]any)
	}

	if p.terminalActions.Escalate {
		meta[metaKeyEscalate] = true
	}
	if p.terminalActions.TransferToAgent != "" {
		meta[metaKeyTransfer] = p.terminalActions.TransferToAgent
	}

	return meta
}

func toFailedStatusEvent(reqCtx *a2asrv.RequestContext, cause error, meta map[string]any) *a2a.TaskStatusUpdateEvent {
	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, reqCtx, a2a.TextPart{Text: cause.Error()})
	ev := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateFailed, msg)
	ev.Metadata = meta
	ev.Final = true
	return ev
}
