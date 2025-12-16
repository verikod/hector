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

// Package checkpoint provides execution state capture and recovery.
//
// # Architecture (ported from legacy Hector)
//
// ExecutionState captures the full state of an agent execution at a point in time.
// This enables:
//   - Fault tolerance: Resume after crashes
//   - HITL workflows: Pause for human approval, resume later
//   - Long-running tasks: Survive server restarts
//   - Cost optimization: Don't re-execute completed work
//
// The checkpoint system is built on top of session.Service - checkpoints are stored
// in session state (under "pending_executions" key) and can be recovered on startup.
//
// # Multi-Agent Scope
//
// Checkpoints capture the state of the CURRENTLY EXECUTING agent only, not the
// entire agent tree. This is intentional and matches legacy Hector behavior.
//
// Why single-agent scope is sufficient:
//
//  1. Session events are the source of truth - All agent interactions are persisted
//     to session.Service, providing complete conversation history across all agents.
//
//  2. Runner determines correct agent - On recovery, runner.findAgentToRun() uses
//     session event history to determine which agent should resume.
//
//  3. Context is preserved - The LLM sees full conversation history via session
//     events when the agent resumes.
//
// # Recovery Flow
//
//	┌─────────────────────────────────────────────────────────────────────────┐
//	│   CHECKPOINT CREATION                                                    │
//	│   ───────────────────                                                    │
//	│   User → Coordinator → Researcher (tool approval needed)                 │
//	│                            ↓                                             │
//	│                   CHECKPOINT: AgentName="researcher"                     │
//	│                              AgentState={iteration: 1, ...}              │
//	│                              PendingToolCall={requires_approval: true}   │
//	├─────────────────────────────────────────────────────────────────────────┤
//	│   RECOVERY                                                               │
//	│   ────────                                                               │
//	│   1. Load checkpoint → AgentName="researcher"                            │
//	│   2. Load session → Events from all agents (full history)                │
//	│   3. Runner.findAgentToRun() → Returns "researcher"                      │
//	│   4. Resume researcher with full context                                 │
//	└─────────────────────────────────────────────────────────────────────────┘
//
// # Workflow Agent Support
//
// For workflow agents (sequential, parallel, loop), additional state is captured:
//   - WorkflowType: The workflow pattern being executed
//   - WorkflowStage: Current stage in sequential workflows
//   - LoopIteration: Current iteration in loop workflows
//
// Parallel workflows have limited checkpoint support because multiple agents
// may be executing concurrently. In this case, recovery starts the parallel
// workflow from the beginning.
//
// # Integration
//
// Agents can implement the agent.Checkpointable interface to provide custom
// state capture/restore logic. The checkpoint hooks in manager.go provide
// integration points for the runner.
package checkpoint

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/verikod/hector/pkg/agent"
)

// Phase represents the execution phase when checkpoint was created.
type Phase string

const (
	// PhaseInitialized - Agent execution just started.
	PhaseInitialized Phase = "initialized"

	// PhasePreLLM - Before LLM call.
	PhasePreLLM Phase = "pre_llm"

	// PhasePostLLM - After LLM response received.
	PhasePostLLM Phase = "post_llm"

	// PhaseToolExecution - During tool execution.
	PhaseToolExecution Phase = "tool_execution"

	// PhasePostTool - After tool execution completed.
	PhasePostTool Phase = "post_tool"

	// PhaseIterationEnd - End of an agent loop iteration.
	PhaseIterationEnd Phase = "iteration_end"

	// PhaseToolApproval - Waiting for HITL tool approval.
	PhaseToolApproval Phase = "tool_approval"

	// PhaseError - Checkpoint created due to error.
	PhaseError Phase = "error"
)

// Type represents why the checkpoint was created.
type Type string

const (
	// TypeEvent - Event-driven (tool approval, error, etc.).
	TypeEvent Type = "event"

	// TypeInterval - Interval-based (every N iterations).
	TypeInterval Type = "interval"

	// TypeManual - Manual pause requested.
	TypeManual Type = "manual"

	// TypeError - Error recovery checkpoint.
	TypeError Type = "error"
)

// State represents the full execution state at a checkpoint.
//
// This captures everything needed to resume agent execution:
//   - Task and session identifiers
//   - The original user query
//   - Agent execution state (messages, iteration count, etc.)
//   - Pending tool calls awaiting approval
//   - Checkpoint metadata (phase, type, time)
type State struct {
	// Core identifiers
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	AppName   string `json:"app_name"`

	// Original user input
	Query string `json:"query"`

	// Agent state snapshot
	AgentName      string              `json:"agent_name"`
	AgentState     *AgentStateSnapshot `json:"agent_state,omitempty"`
	InvocationID   string              `json:"invocation_id"`
	LastEventIndex int                 `json:"last_event_index"` // Index of last processed event

	// Pending tool call (for HITL approval)
	PendingToolCall *PendingToolCall `json:"pending_tool_call,omitempty"`

	// Checkpoint metadata
	Phase          Phase     `json:"phase"`
	CheckpointType Type      `json:"checkpoint_type"`
	CheckpointTime time.Time `json:"checkpoint_time"`

	// Error information (if Phase == PhaseError)
	Error string `json:"error,omitempty"`
}

// AgentStateSnapshot captures the state of an LLM agent during execution.
//
// Multi-Agent Scope (ported from legacy Hector):
//
//	Checkpoints capture the state of the CURRENTLY EXECUTING agent only.
//	This is intentional - the full multi-agent history is preserved in
//	session events (the source of truth). On recovery:
//	  1. Checkpoint tells us which agent was active
//	  2. Session events provide full conversation history
//	  3. Runner.findAgentToRun() routes to the correct agent
//
// For workflow agents (sequential, parallel, loop), the WorkflowState
// fields track progress within the workflow.
type AgentStateSnapshot struct {
	// Iteration tracking
	Iteration   int `json:"iteration"`
	TotalTokens int `json:"total_tokens"`

	// Conversation state (from legacy ReasoningStateSnapshot)
	History     []*agent.Event `json:"history,omitempty"`
	LastEvent   *agent.Event   `json:"last_event,omitempty"`
	CurrentTurn []*agent.Event `json:"current_turn,omitempty"` // Messages in current turn

	// Response accumulation
	AccumulatedResponse string `json:"accumulated_response,omitempty"`
	FinalResponseAdded  bool   `json:"final_response_added"` // Response complete flag

	// Tool execution tracking
	PendingToolCalls        []*ToolCallSnapshot `json:"pending_tool_calls,omitempty"`
	FirstIterationToolCalls []*ToolCallSnapshot `json:"first_iteration_tool_calls,omitempty"` // For agentic loop

	// Multi-agent context (from legacy SubAgents field)
	SubAgents   []string `json:"sub_agents,omitempty"`   // Available sub-agents (for transfer)
	ParentAgent string   `json:"parent_agent,omitempty"` // Who invoked this agent
	Branch      string   `json:"branch,omitempty"`       // Agent branch path (e.g., "root.coordinator.researcher")

	// Workflow state (for sequential/parallel/loop agents)
	WorkflowType      string `json:"workflow_type,omitempty"`       // "sequential", "parallel", "loop"
	WorkflowStage     int    `json:"workflow_stage,omitempty"`      // Current stage index in sequential
	LoopIteration     int    `json:"loop_iteration,omitempty"`      // Current loop iteration
	LoopMaxIterations int    `json:"loop_max_iterations,omitempty"` // Max loop iterations

	// Agent-specific state (for custom agents)
	Custom map[string]any `json:"custom,omitempty"`
}

// PendingToolCall represents a tool call awaiting execution or approval.
type PendingToolCall struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Arguments        map[string]any `json:"arguments,omitempty"`
	RequiresApproval bool           `json:"requires_approval"`
}

// ToolCallSnapshot captures the state of a tool call in progress.
type ToolCallSnapshot struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	Completed bool           `json:"completed"`
}

// Serialize converts the State to JSON bytes.
func (s *State) Serialize() ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("cannot serialize nil state")
	}
	return json.Marshal(s)
}

// Deserialize reconstructs a State from JSON bytes.
func Deserialize(data []byte) (*State, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("cannot deserialize empty data")
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint state: %w", err)
	}

	return &state, nil
}

// NewState creates a new checkpoint State with required fields.
func NewState(taskID, sessionID, userID, appName, query, agentName, invocationID string) *State {
	return &State{
		TaskID:         taskID,
		SessionID:      sessionID,
		UserID:         userID,
		AppName:        appName,
		Query:          query,
		AgentName:      agentName,
		InvocationID:   invocationID,
		Phase:          PhaseInitialized,
		CheckpointType: TypeEvent,
		CheckpointTime: time.Now(),
	}
}

// WithPhase sets the checkpoint phase.
func (s *State) WithPhase(phase Phase) *State {
	s.Phase = phase
	s.CheckpointTime = time.Now()
	return s
}

// WithType sets the checkpoint type.
func (s *State) WithType(t Type) *State {
	s.CheckpointType = t
	return s
}

// WithAgentState sets the agent state snapshot.
func (s *State) WithAgentState(as *AgentStateSnapshot) *State {
	s.AgentState = as
	return s
}

// WithPendingToolCall sets a pending tool call.
func (s *State) WithPendingToolCall(tc *PendingToolCall) *State {
	s.PendingToolCall = tc
	return s
}

// WithError sets the error message.
func (s *State) WithError(err error) *State {
	if err != nil {
		s.Error = err.Error()
		s.Phase = PhaseError
		s.CheckpointType = TypeError
	}
	return s
}

// WithLastEventIndex sets the index of the last processed event.
func (s *State) WithLastEventIndex(idx int) *State {
	s.LastEventIndex = idx
	return s
}

// IsExpired checks if the checkpoint has expired based on the timeout.
func (s *State) IsExpired(timeout time.Duration) bool {
	if s.CheckpointTime.IsZero() {
		return false // No timestamp, assume valid
	}
	if timeout <= 0 {
		return false // No timeout configured
	}
	return time.Since(s.CheckpointTime) > timeout
}

// IsRecoverable returns true if the checkpoint can be recovered.
func (s *State) IsRecoverable() bool {
	// Can't recover from completed or canceled states
	if s.Phase == "" {
		return false
	}
	return true
}

// NeedsUserInput returns true if the checkpoint is waiting for user input.
func (s *State) NeedsUserInput() bool {
	return s.Phase == PhaseToolApproval && s.PendingToolCall != nil && s.PendingToolCall.RequiresApproval
}
