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

package agent

import (
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
)

// Event author constants
const (
	// AuthorUser represents events authored by the user (human input).
	// Used when tool results or other events come from user interactions.
	AuthorUser = "user"

	// AuthorSystem represents events authored by the system.
	// Used for system-generated events like errors or notifications.
	AuthorSystem = "system"
)

// Event represents an interaction in an agent conversation.
// Events are yielded by Agent.Run() and translated to A2A events by the server.
type Event struct {
	// ID is the unique identifier for this event.
	ID string

	// Timestamp when the event was created.
	Timestamp time.Time

	// InvocationID links this event to its invocation.
	InvocationID string

	// Branch isolates conversation history for parallel agents.
	// Format: "agent_1.agent_2.agent_3" (parent chain).
	Branch string

	// Author is the display name of the agent (or "user"/"system").
	// This is used for UI rendering and attribution.
	Author string

	// AgentID is the unique identifier of the agent that produced this event.
	// Used for internal routing, logic, and state tracking.
	AgentID string

	// Message contains the A2A message content (text, files, data).
	// This maps directly to a2a.Message for seamless translation.
	Message *a2a.Message

	// Actions captures side effects (state changes, transfers, etc).
	Actions EventActions

	// LongRunningToolIDs identifies tools awaiting external completion.
	LongRunningToolIDs []string

	// Partial indicates this is a streaming chunk, not a complete event.
	Partial bool

	// TurnComplete indicates this is the final event of a turn.
	TurnComplete bool

	// Interrupted indicates this event was cut off due to cancellation or timeout.
	// When true, the response may be incomplete. This helps distinguish between:
	// - A complete response that ended naturally (Interrupted=false)
	// - A partial response that was forcibly stopped (Interrupted=true)
	Interrupted bool

	// ErrorCode is a machine-readable error identifier (e.g., "rate_limit", "timeout").
	ErrorCode string

	// ErrorMessage is a human-readable error description.
	ErrorMessage string

	// Thinking captures the model's reasoning process (for thinking-enabled models).
	// This is rendered as a collapsible ThinkingWidget in the UI.
	Thinking *ThinkingState

	// ToolCalls captures tool invocations in this event.
	// Each tool call is rendered as a ToolWidget in the UI.
	ToolCalls []ToolCallState

	// ToolResults captures tool execution results in this event.
	// These update the corresponding ToolWidget status.
	ToolResults []ToolResultState

	// CustomMetadata for application-specific data.
	CustomMetadata map[string]any

	// OnPersisted is a callback invoked after the event is persisted to the session.
	// Used by async agents (like ParallelAgent) to synchronize with the persistence layer.
	// This field is not serialized.
	OnPersisted func() `json:"-"`
}

// ThinkingState represents the model's reasoning process.
// Maps to ThinkingWidget in the UI with proper lifecycle animations.
type ThinkingState struct {
	// ID uniquely identifies this thinking block within a conversation.
	ID string `json:"id"`

	// Status indicates the lifecycle state: "active" | "completed".
	Status string `json:"status"`

	// Content is the thinking text (may stream incrementally).
	Content string `json:"content"`

	// Signature is used for multi-turn verification (e.g., Anthropic).
	Signature string `json:"signature,omitempty"`

	// Type categorizes the thinking: "default" | "planning" | "reflection" | "goal".
	Type string `json:"type,omitempty"`
}

// ToolCallState represents a tool invocation.
// Maps to ToolWidget in the UI with "working" status.
type ToolCallState struct {
	// ID uniquely identifies this tool call (matches LLM's tool_use ID).
	ID string `json:"id"`

	// Name is the tool being called.
	Name string `json:"name"`

	// Args are the arguments passed to the tool.
	Args map[string]any `json:"args"`

	// Status indicates lifecycle: "pending" | "working".
	Status string `json:"status"`
}

// ToolResultState represents a tool execution result.
// Updates the corresponding ToolWidget to "success" | "failed".
type ToolResultState struct {
	// ToolCallID links this result to its ToolCallState.
	ToolCallID string `json:"tool_call_id"`

	// Content is the tool's output.
	Content string `json:"content"`

	// Status indicates outcome: "success" | "failed".
	Status string `json:"status"`

	// IsError indicates if Content represents an error message.
	IsError bool `json:"is_error,omitempty"`
}

// NewEvent creates a new event with generated ID and current timestamp.
func NewEvent(invocationID string) *Event {
	return &Event{
		ID:           uuid.NewString(),
		Timestamp:    time.Now(),
		InvocationID: invocationID,
		Actions:      EventActions{StateDelta: make(map[string]any)},
	}
}

// EventActions represents side effects attached to an event.
type EventActions struct {
	// StateDelta contains key-value changes to session state.
	StateDelta map[string]any

	// ArtifactDelta tracks artifact updates (filename -> version).
	ArtifactDelta map[string]int64

	// SkipSummarization prevents LLM summarization of tool responses.
	SkipSummarization bool

	// TransferToAgent requests control transfer to another agent.
	TransferToAgent string

	// Escalate requests escalation to a higher-level agent.
	Escalate bool

	// RequireInput signals that human input is required (HITL pattern).
	// When true, the task transitions to `input_required` state.
	// This is used by long-running tools that need approval or additional info.
	RequireInput bool

	// InputPrompt is the message shown to the human when RequireInput is true.
	// Should explain what input is needed and why.
	InputPrompt string
}

// IsFinalResponse returns whether this event is a final response.
// Multiple events can be final when multiple agents participate in one invocation.
//
// An event is NOT final if it:
// - Contains function/tool calls (awaiting execution)
// - Contains function/tool responses (awaiting LLM summarization)
// - Is a partial/streaming event
func (e *Event) IsFinalResponse() bool {
	// SkipSummarization or long-running tools are explicitly final
	if e.Actions.SkipSummarization || len(e.LongRunningToolIDs) > 0 {
		return true
	}

	// Partial events are never final
	if e.Partial {
		return false
	}

	// Events with tool calls or results are not final
	if e.HasToolCalls() || e.HasToolResults() {
		return false
	}

	return true
}

// HasToolCalls returns true if this event contains tool call requests.
// Tool calls indicate the LLM wants to execute tools before providing a final response.
func (e *Event) HasToolCalls() bool {
	// Check ToolCalls field first (preferred)
	if len(e.ToolCalls) > 0 {
		return true
	}

	// Fall back to checking message parts (A2A protocol)
	return hasPartOfType(e.Message, "tool_use")
}

// HasToolResults returns true if this event contains tool execution results.
// Tool results indicate tools have been executed and the LLM needs to process them.
func (e *Event) HasToolResults() bool {
	// Check ToolResults field first (preferred)
	if len(e.ToolResults) > 0 {
		return true
	}

	// Fall back to checking message parts (A2A protocol)
	return hasPartOfType(e.Message, "tool_result")
}

// hasPartOfType checks if a message contains a DataPart with the specified type.
func hasPartOfType(msg *a2a.Message, partType string) bool {
	if msg == nil {
		return false
	}

	for _, part := range msg.Parts {
		if dp, ok := part.(a2a.DataPart); ok {
			if typeVal, hasType := dp.Data["type"].(string); hasType && typeVal == partType {
				return true
			}
		}
	}
	return false
}

// TextContent extracts text content from the event's message.
func (e *Event) TextContent() string {
	if e.Message == nil {
		return ""
	}

	var text string
	for _, part := range e.Message.Parts {
		if tp, ok := part.(a2a.TextPart); ok {
			text += tp.Text
		}
	}
	return text
}

// Content is a convenience type for building message content.
type Content struct {
	Parts []a2a.Part
	Role  a2a.MessageRole
}

// NewTextContent creates content with a text part.
func NewTextContent(text string, role a2a.MessageRole) *Content {
	return &Content{
		Parts: []a2a.Part{a2a.TextPart{Text: text}},
		Role:  role,
	}
}

// ToMessage converts Content to an a2a.Message.
func (c *Content) ToMessage() *a2a.Message {
	if c == nil {
		return nil
	}
	return a2a.NewMessage(c.Role, c.Parts...)
}

// AddPart appends a part to the content.
func (c *Content) AddPart(part a2a.Part) {
	c.Parts = append(c.Parts, part)
}

// AddText appends a text part to the content.
func (c *Content) AddText(text string) {
	c.Parts = append(c.Parts, a2a.TextPart{Text: text})
}
