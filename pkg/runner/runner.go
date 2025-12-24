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

// Package runner provides the orchestration layer for agent execution.
//
// The Runner manages agent execution within sessions, handling:
//   - Session creation and retrieval
//   - Agent selection based on session history
//   - Event streaming and persistence
//   - Parent map for agent tree navigation
package runner

import (
	"context"
	"fmt"
	"iter"
	"log/slog"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/memory"
	"github.com/verikod/hector/pkg/session"
)

// Config contains the configuration for creating a Runner.
type Config struct {
	// AppName identifies the application.
	AppName string

	// Agent is the root agent for execution.
	Agent agent.Agent

	// SessionService manages session lifecycle (SOURCE OF TRUTH).
	SessionService session.Service

	// ArtifactService manages artifact storage (optional).
	ArtifactService ArtifactService

	// IndexService provides semantic search over sessions (optional).
	// This is a SEARCH INDEX built on top of SessionService.
	// Can be rebuilt from SessionService at any time.
	IndexService IndexService

	// CheckpointManager handles execution state checkpointing (optional).
	// Enables fault tolerance and HITL workflow recovery.
	CheckpointManager CheckpointManager
}

// ArtifactService defines the interface for artifact storage.
type ArtifactService interface {
	agent.Artifacts
}

// IndexService defines the interface for semantic search over sessions.
//
// Architecture (derived from legacy Hector):
//   - SessionService: SOURCE OF TRUTH (stores all events in SQL)
//   - IndexService: SEARCH INDEX (can be rebuilt from SessionService)
//
// The index is populated after each turn and can be rebuilt on startup.
type IndexService interface {
	// Index adds session events to the search index.
	// Called after each turn (data already persisted to SessionService).
	Index(ctx context.Context, sess agent.Session) error

	// Search performs semantic similarity search.
	Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error)
}

// CheckpointManager defines the interface for checkpoint operations.
//
// Architecture (ported from legacy Hector):
//   - Checkpoints capture execution state at strategic points
//   - Enables fault tolerance and HITL workflow recovery
//   - Stored in session state under "pending_executions" key
type CheckpointManager interface {
	// IsEnabled returns whether checkpointing is enabled.
	IsEnabled() bool

	// ClearCheckpoint removes a checkpoint.
	ClearCheckpoint(ctx context.Context, appName, userID, sessionID, taskID string) error
}

// Runner orchestrates agent execution within sessions.
type Runner struct {
	appName           string
	rootAgent         agent.Agent
	sessionService    session.Service
	artifactService   ArtifactService
	indexService      IndexService
	checkpointManager CheckpointManager
	parents           ParentMap
}

// New creates a new Runner.
func New(cfg Config) (*Runner, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("root agent is required")
	}
	if cfg.SessionService == nil {
		return nil, fmt.Errorf("session service is required")
	}

	parents, err := BuildParentMap(cfg.Agent)
	if err != nil {
		return nil, fmt.Errorf("failed to build agent tree: %w", err)
	}

	return &Runner{
		appName:           cfg.AppName,
		rootAgent:         cfg.Agent,
		sessionService:    cfg.SessionService,
		artifactService:   cfg.ArtifactService,
		indexService:      cfg.IndexService,
		checkpointManager: cfg.CheckpointManager,
		parents:           parents,
	}, nil
}

// Run executes the agent for the given user input, yielding events.
// For each user message, it finds the proper agent within the agent tree
// to continue the conversation within the session.
func (r *Runner) Run(ctx context.Context, userID, sessionID string, content *agent.Content, cfg agent.RunConfig) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Get or create session
		sess, err := r.getOrCreateSession(ctx, userID, sessionID)
		if err != nil {
			yield(nil, err)
			return
		}

		// Find agent to run based on session history
		agentToRun := r.findAgentToRun(sess)

		// Deferred cleanup in reverse order (execute bottom-to-top):
		//
		// Architecture (derived from legacy Hector):
		//   1. clearTempState - Clean up temp keys
		//   2. indexSession - Build search index (data already in SessionService)
		//   3. checkAndSummarize - Working memory management

		// 1. Clear temp keys after invocation completes (adk-go pattern)
		defer r.clearTempState(sess)

		// 2. Index session for semantic search (data already persisted to SessionService)
		// This builds the SEARCH INDEX, not storage (SessionService is the source of truth)
		defer r.indexSession(ctx, sess)

		// 3. Check and perform summarization if needed (legacy hector pattern)
		defer r.checkAndSummarize(ctx, sess, agentToRun)

		// Create scoped memory adapter for this invocation
		// The adapter bridges IndexService to agent.Memory interface
		var mem agent.Memory
		if r.indexService != nil {
			mem = memory.NewAdapter(r.indexService, r.appName, userID)
		} else {
			mem = memory.NilMemory()
		}

		// Create invocation context
		invCtx := agent.NewInvocationContext(ctx, agent.InvocationContextParams{
			Agent:       agentToRun,
			Session:     sess,
			Artifacts:   r.artifactService,
			Memory:      mem,
			UserContent: content,
			RunConfig:   &cfg,
		})

		// Append user message to session
		if err := r.appendUserMessage(ctx, sess, content, invCtx.InvocationID()); err != nil {
			yield(nil, err)
			return
		}

		// Run agent and yield events
		for event, err := range agentToRun.Run(invCtx) {
			if err != nil {
				if !yield(event, err) {
					return
				}
				continue
			}

			// Persist non-partial events
			if !event.Partial {
				if err := r.sessionService.AppendEvent(ctx, sess, event); err != nil {
					yield(nil, fmt.Errorf("failed to persist event: %w", err))
					return
				}
				// Signal successful persistence (essential for async agents)
				if event.OnPersisted != nil {
					event.OnPersisted()
				}
			}

			if !yield(event, nil) {
				return
			}
		}
	}
}

// indexSession adds the session to the search index for semantic retrieval.
//
// Architecture (derived from legacy Hector):
//   - SessionService is the SOURCE OF TRUTH (data already persisted)
//   - IndexService is a SEARCH INDEX (can be rebuilt from SessionService)
//
// This is called after each turn to keep the index updated.
// The index can be rebuilt from SessionService if corrupted.
func (r *Runner) indexSession(ctx context.Context, sess session.Session) {
	if r.indexService == nil {
		return
	}

	if err := r.indexService.Index(ctx, sess); err != nil {
		slog.Warn("Failed to index session",
			"session_id", sess.ID(),
			"error", err)
	}
}

// checkAndSummarize checks if summarization should occur based on the agent's
// working memory strategy. If summarization is needed, it performs the summarization
// and persists the summary event to the session.
//
// This follows the legacy Hector pattern where MemoryService.AddBatchToHistory
// would call workingMemory.CheckAndSummarize after each turn.
func (r *Runner) checkAndSummarize(ctx context.Context, sess session.Session, ag agent.Agent) {
	// Check if agent implements WorkingMemoryProvider
	wmProvider, ok := ag.(memory.WorkingMemoryProvider)
	if !ok {
		return // Agent doesn't support working memory
	}

	strategy := wmProvider.WorkingMemory()
	if strategy == nil {
		return // No working memory strategy configured
	}

	// Collect all events from session
	var events []*agent.Event
	for ev := range sess.Events().All() {
		events = append(events, ev)
	}

	// Check and perform summarization
	summaryEvent, err := strategy.CheckAndSummarize(ctx, events)
	if err != nil {
		slog.Warn("Summarization check failed",
			"session_id", sess.ID(),
			"strategy", strategy.Name(),
			"error", err)
		return
	}

	// Persist summary event if created
	if summaryEvent != nil {
		if err := r.sessionService.AppendEvent(ctx, sess, summaryEvent); err != nil {
			slog.Error("Failed to persist summary event",
				"session_id", sess.ID(),
				"error", err)
			return
		}
		slog.Info("Summarization completed and persisted",
			"session_id", sess.ID(),
			"strategy", strategy.Name())
	}
}

// clearTempState removes all temp: prefixed keys from session state.
// This follows adk-go's pattern where temporary state is discarded after each invocation.
func (r *Runner) clearTempState(sess session.Session) {
	state := sess.State()
	if clearable, ok := state.(agent.TempClearable); ok {
		clearable.ClearTempKeys()
	}
}

// FindAgent searches for an agent by name in the runner's agent tree.
// This provides the same functionality as ADK-Go's find_agent() method.
//
// Example:
//
//	runner, _ := runner.New(runner.Config{Agent: coordinator})
//	researcher := runner.FindAgent("researcher")
func (r *Runner) FindAgent(name string) agent.Agent {
	return agent.FindAgent(r.rootAgent, name)
}

// ListAgents returns all agents in the runner's agent tree.
func (r *Runner) ListAgents() []agent.Agent {
	return agent.ListAgents(r.rootAgent)
}

func (r *Runner) getOrCreateSession(ctx context.Context, userID, sessionID string) (session.Session, error) {
	resp, err := r.sessionService.Get(ctx, &session.GetRequest{
		AppName:   r.appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err == nil && resp != nil {
		return resp.Session, nil
	}

	// Create new session
	createResp, err := r.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   r.appName,
		UserID:    userID,
		SessionID: sessionID,
		State:     make(map[string]any),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return createResp.Session, nil
}

func (r *Runner) appendUserMessage(ctx context.Context, sess session.Session, content *agent.Content, invocationID string) error {
	if content == nil {
		return nil
	}

	event := agent.NewEvent(invocationID)
	event.Author = "user"
	event.Message = content.ToMessage()

	return r.sessionService.AppendEvent(ctx, sess, event)
}

// findAgentToRun determines which agent should handle the next request
// based on session history.
func (r *Runner) findAgentToRun(sess session.Session) agent.Agent {
	events := sess.Events()
	for i := events.Len() - 1; i >= 0; i-- {
		event := events.At(i)
		if event == nil || event.Author == "user" {
			continue
		}

		// Find agent by name in the tree
		subAgent := findAgentByName(r.rootAgent, event.Author)
		if subAgent == nil {
			slog.Debug("Event from unknown agent",
				"agent", event.Author,
				"event_id", event.ID)
			continue
		}

		// Skip sub-agents of workflow agents (sequential/parallel/loop).
		// Workflow agents should always run their full workflow on every invocation,
		// not resume from a sub-agent.
		if r.isWorkflowSubAgent(subAgent) {
			slog.Debug("Skipping workflow sub-agent, will use root instead",
				"agent", subAgent.Name())
			continue
		}

		// Check if transfer is allowed up the tree
		if r.isTransferableAcrossTree(subAgent) {
			return subAgent
		}
	}

	return r.rootAgent
}

// isWorkflowSubAgent checks if an agent is a direct child of a workflow agent.
// Workflow agents (sequential/parallel/loop) should always run from the root,
// never resume from their sub-agents.
func (r *Runner) isWorkflowSubAgent(ag agent.Agent) bool {
	// Get the parent agent
	parent := r.parents[ag.Name()]
	if parent == nil {
		return false
	}

	// Check if parent is a workflow agent
	t := parent.Type()
	if t == agent.TypeSequentialAgent ||
		t == agent.TypeParallelAgent ||
		t == agent.TypeLoopAgent ||
		t == agent.TypeRunnerAgent {
		slog.Debug("Parent is workflow agent, skipping sub-agent",
			"agent", ag.Name(),
			"parent", parent.Name(),
			"parent_type", t)
		return true
	}
	return false
}

// TransferRestrictable is implemented by agents that can restrict transfers.
type TransferRestrictable interface {
	DisallowTransferToParent() bool
	DisallowTransferToPeers() bool
}

func (r *Runner) isTransferableAcrossTree(ag agent.Agent) bool {
	// Walk up the parent chain checking for transfer restrictions
	for current := ag; current != nil; current = r.parents[current.Name()] {
		// Check if this agent restricts parent transfers
		if restrictable, ok := current.(TransferRestrictable); ok {
			if restrictable.DisallowTransferToParent() {
				slog.Debug("Transfer blocked by DisallowTransferToParent",
					"agent", current.Name())
				return false
			}
		}
	}
	return true
}

func findAgentByName(root agent.Agent, name string) agent.Agent {
	if root == nil {
		return nil
	}
	if root.Name() == name {
		return root
	}
	for _, sub := range root.SubAgents() {
		if found := findAgentByName(sub, name); found != nil {
			return found
		}
	}
	return nil
}

// ParentMap maps agent names to their parent agents.
type ParentMap map[string]agent.Agent

// BuildParentMap creates a parent map for the agent tree.
func BuildParentMap(root agent.Agent) (ParentMap, error) {
	parents := make(ParentMap)
	if err := buildParentMapRecursive(root, nil, parents); err != nil {
		return nil, err
	}
	return parents, nil
}

func buildParentMapRecursive(ag agent.Agent, parent agent.Agent, parents ParentMap) error {
	if ag == nil {
		return nil
	}

	// Check for cycles
	if _, exists := parents[ag.Name()]; exists {
		return fmt.Errorf("duplicate agent name in tree: %s", ag.Name())
	}

	parents[ag.Name()] = parent

	for _, sub := range ag.SubAgents() {
		if err := buildParentMapRecursive(sub, ag, parents); err != nil {
			return err
		}
	}

	return nil
}

// RootAgent returns the root agent.
func (r *Runner) RootAgent() agent.Agent {
	return r.rootAgent
}

// AppName returns the application name.
func (r *Runner) AppName() string {
	return r.appName
}

// CheckpointManager returns the checkpoint manager (may be nil).
func (r *Runner) CheckpointManager() CheckpointManager {
	return r.checkpointManager
}

// IsCheckpointEnabled returns whether checkpointing is enabled.
func (r *Runner) IsCheckpointEnabled() bool {
	return r.checkpointManager != nil && r.checkpointManager.IsEnabled()
}
