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
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
)

// Agent is the base interface which all agents must implement.
//
// Agents are created with constructors to ensure correct initialization:
//   - agent.New for custom agents
//   - llmagent.New for LLM-based agents
//   - remoteagent.NewA2A for remote A2A agents
//   - workflowagents for sequential/parallel/loop patterns
type Agent interface {
	// Name returns the unique identifier for this agent within the agent tree.
	// Agent name cannot be "user" as it's reserved for end-user input.
	Name() string

	// DisplayName returns the human-readable name of the agent.
	// Used for UI attribution.
	DisplayName() string

	// Description returns a human-readable description of the agent's capability.
	// Used by LLMs to determine whether to delegate control to this agent.
	Description() string

	// Run executes the agent and yields events.
	// The iterator pattern allows clean streaming of results.
	Run(InvocationContext) iter.Seq2[*Event, error]

	// SubAgents returns child agents that this agent can delegate to.
	SubAgents() []Agent

	// Type returns the agent type for introspection.
	// Used by the runner to determine workflow vs delegation semantics.
	Type() AgentType
}

// Checkpointable is an optional interface for agents that support state checkpointing.
//
// Agents implementing this interface can have their execution state captured
// for fault tolerance and HITL workflow recovery.
//
// Implementation Note (ported from legacy Hector):
//
//	Checkpoints capture the state of the CURRENTLY EXECUTING agent only.
//	The full multi-agent history is preserved in session events (the source
//	of truth). On recovery:
//	  1. Checkpoint identifies which agent was active
//	  2. Session events provide full conversation history
//	  3. Runner.findAgentToRun() routes to the correct agent
//
// Not all agents need to implement this - only agents with internal state
// that cannot be reconstructed from session events alone.
type Checkpointable interface {
	Agent

	// CaptureCheckpointState returns the agent's current execution state.
	// Called by the checkpoint manager at strategic points (pre-LLM, post-tool, etc.)
	CaptureCheckpointState() (map[string]any, error)

	// RestoreCheckpointState restores the agent's execution state from a checkpoint.
	// Called by the recovery manager when resuming from a checkpoint.
	RestoreCheckpointState(state map[string]any) error
}

// Config is the configuration for creating a new custom Agent.
type Config struct {
	// Name must be a non-empty string, unique within the agent tree.
	Name string

	// DisplayName is the human-readable name (optional).
	DisplayName string

	// Description of the agent's capability (used for delegation decisions).
	Description string

	// SubAgents are child agents this agent can delegate tasks to.
	SubAgents []Agent

	// BeforeAgentCallbacks are called before the agent starts its run.
	// If any returns non-nil content or error, agent run is skipped.
	BeforeAgentCallbacks []BeforeAgentCallback

	// Run defines the agent's execution logic.
	Run func(InvocationContext) iter.Seq2[*Event, error]

	// AgentType allows specifying the agent type (optional).
	// If empty, defaults to TypeCustomAgent.
	AgentType AgentType

	// AfterAgentCallbacks are called after the agent completes its run.
	AfterAgentCallbacks []AfterAgentCallback
}

// BeforeAgentCallback is called before the agent starts.
// If it returns non-nil content or error, agent run is skipped.
type BeforeAgentCallback func(CallbackContext) (*a2a.Message, error)

// AfterAgentCallback is called after the agent completes.
// If it returns non-nil content or error, a new event is created.
type AfterAgentCallback func(CallbackContext) (*a2a.Message, error)

// AgentType identifies the kind of agent for introspection.
type AgentType string

const (
	TypeCustomAgent     AgentType = "custom"
	TypeLLMAgent        AgentType = "llm"
	TypeSequentialAgent AgentType = "sequential"
	TypeParallelAgent   AgentType = "parallel"
	TypeLoopAgent       AgentType = "loop"
	TypeRemoteAgent     AgentType = "remote"
	TypeRunnerAgent     AgentType = "runner"
)

// baseAgent implements the Agent interface with common functionality.
type baseAgent struct {
	name        string
	displayName string
	description string
	subAgents   []Agent
	agentType   AgentType

	beforeAgentCallbacks []BeforeAgentCallback
	run                  func(InvocationContext) iter.Seq2[*Event, error]
	afterAgentCallbacks  []AfterAgentCallback
}

// New creates an Agent with custom logic defined by the Run function.
func New(cfg Config) (Agent, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if cfg.Name == "user" {
		return nil, fmt.Errorf("agent name cannot be 'user' (reserved)")
	}
	if cfg.Run == nil {
		return nil, fmt.Errorf("agent Run function is required")
	}

	// Check for duplicate sub-agents
	seen := make(map[string]bool)
	for _, sub := range cfg.SubAgents {
		if seen[sub.Name()] {
			return nil, fmt.Errorf("duplicate sub-agent: %s", sub.Name())
		}
		seen[sub.Name()] = true
	}

	// Default agent type
	agentType := cfg.AgentType
	if agentType == "" {
		agentType = TypeCustomAgent
	}

	displayName := cfg.DisplayName
	if displayName == "" {
		displayName = cfg.Name
	}

	return &baseAgent{
		name:                 cfg.Name,
		displayName:          displayName,
		description:          cfg.Description,
		subAgents:            cfg.SubAgents,
		agentType:            agentType,
		beforeAgentCallbacks: cfg.BeforeAgentCallbacks,
		run:                  cfg.Run,
		afterAgentCallbacks:  cfg.AfterAgentCallbacks,
	}, nil
}

// Type returns the agent type for introspection.
func (a *baseAgent) Type() AgentType {
	return a.agentType
}

func (a *baseAgent) Name() string {
	return a.name
}

func (a *baseAgent) DisplayName() string {
	return a.displayName
}

func (a *baseAgent) Description() string {
	return a.description
}

func (a *baseAgent) SubAgents() []Agent {
	return a.subAgents
}

func (a *baseAgent) Run(ctx InvocationContext) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		// Run before-agent callbacks
		event, err := a.runBeforeCallbacks(ctx)
		if event != nil || err != nil {
			yield(event, err)
			return
		}

		if ctx.Ended() {
			return
		}

		// Execute agent logic
		for event, err := range a.run(ctx) {
			if event != nil && event.Author == "" {
				event.Author = a.displayName
				event.AgentID = a.name
			}
			if !yield(event, err) {
				return
			}
		}

		if ctx.Ended() {
			return
		}

		// Run after-agent callbacks
		event, err = a.runAfterCallbacks(ctx)
		if event != nil || err != nil {
			yield(event, err)
		}
	}
}

func (a *baseAgent) runBeforeCallbacks(ctx InvocationContext) (*Event, error) {
	cbCtx := newCallbackContext(ctx)

	for _, cb := range a.beforeAgentCallbacks {
		msg, err := cb(cbCtx)
		if err != nil {
			return nil, fmt.Errorf("before-agent callback failed: %w", err)
		}
		if msg != nil {
			event := NewEvent(ctx.InvocationID())
			event.Message = msg
			event.Author = a.displayName
			event.AgentID = a.name
			event.Branch = ctx.Branch()
			event.Actions = *cbCtx.actions
			ctx.EndInvocation()
			return event, nil
		}
	}

	// Return state delta event if modified
	if len(cbCtx.actions.StateDelta) > 0 {
		event := NewEvent(ctx.InvocationID())
		event.Author = a.displayName
		event.AgentID = a.name
		event.Branch = ctx.Branch()
		event.Actions = *cbCtx.actions
		return event, nil
	}

	return nil, nil
}

func (a *baseAgent) runAfterCallbacks(ctx InvocationContext) (*Event, error) {
	cbCtx := newCallbackContext(ctx)

	for _, cb := range a.afterAgentCallbacks {
		msg, err := cb(cbCtx)
		if err != nil {
			return nil, fmt.Errorf("after-agent callback failed: %w", err)
		}
		if msg != nil {
			event := NewEvent(ctx.InvocationID())
			event.Message = msg
			event.Author = a.displayName
			event.AgentID = a.name
			event.Branch = ctx.Branch()
			event.Actions = *cbCtx.actions
			return event, nil
		}
	}

	// Return state delta event if modified
	if len(cbCtx.actions.StateDelta) > 0 {
		event := NewEvent(ctx.InvocationID())
		event.Author = a.displayName
		event.AgentID = a.name
		event.Branch = ctx.Branch()
		event.Actions = *cbCtx.actions
		return event, nil
	}

	return nil, nil
}

// ============================================================================
// Agent Hierarchy Navigation (adk-go alignment)
// ============================================================================

// FindAgent searches for an agent by name in the agent tree.
// It performs a depth-first search starting from the root agent.
//
// This provides the same functionality as ADK-Go's find_agent() method.
//
// Example:
//
//	root, _ := llmagent.New(llmagent.Config{
//	    Name: "coordinator",
//	    SubAgents: []agent.Agent{researcher, writer},
//	})
//
//	// Find a descendant by name
//	found := agent.FindAgent(root, "researcher")
//	if found != nil {
//	    // Use the found agent
//	}
func FindAgent(root Agent, name string) Agent {
	if root == nil {
		return nil
	}
	if root.Name() == name {
		return root
	}
	for _, sub := range root.SubAgents() {
		if found := FindAgent(sub, name); found != nil {
			return found
		}
	}
	return nil
}

// FindAgentPath returns the path to an agent in the tree.
// The path is a slice of agent names from root to the target (exclusive of root).
// Returns nil if the agent is not found.
//
// Example:
//
//	// For a tree: coordinator -> team_a -> specialist
//	path := agent.FindAgentPath(coordinator, "specialist")
//	// path = ["team_a", "specialist"]
func FindAgentPath(root Agent, name string) []string {
	if root == nil {
		return nil
	}
	if root.Name() == name {
		return []string{} // Found at root
	}
	for _, sub := range root.SubAgents() {
		if path := FindAgentPath(sub, name); path != nil {
			return append([]string{sub.Name()}, path...)
		}
	}
	return nil
}

// WalkAgents visits all agents in the tree depth-first.
// The visitor function is called for each agent with its depth level.
// If the visitor returns false, the walk stops.
//
// Example:
//
//	agent.WalkAgents(root, func(ag Agent, depth int) bool {
//	    fmt.Printf("%s%s\n", strings.Repeat("  ", depth), ag.Name())
//	    return true // continue walking
//	})
func WalkAgents(root Agent, visitor func(Agent, int) bool) {
	walkAgents(root, 0, visitor)
}

func walkAgents(ag Agent, depth int, visitor func(Agent, int) bool) bool {
	if ag == nil {
		return true
	}
	if !visitor(ag, depth) {
		return false
	}
	for _, sub := range ag.SubAgents() {
		if !walkAgents(sub, depth+1, visitor) {
			return false
		}
	}
	return true
}

// ListAgents returns a flat list of all agents in the tree.
// The root agent is included first, followed by descendants depth-first.
func ListAgents(root Agent) []Agent {
	var agents []Agent
	WalkAgents(root, func(ag Agent, _ int) bool {
		agents = append(agents, ag)
		return true
	})
	return agents
}
