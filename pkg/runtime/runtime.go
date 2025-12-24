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

// Package runtime builds and manages Hector agents from configuration.
//
// The runtime is the bridge between declarative config and live agents.
// It creates LLM providers, tools, and agents based on the configuration.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/agent/llmagent"
	"github.com/verikod/hector/pkg/agent/remoteagent"
	"github.com/verikod/hector/pkg/agent/runneragent"
	"github.com/verikod/hector/pkg/agent/workflowagent"
	"github.com/verikod/hector/pkg/auth"
	"github.com/verikod/hector/pkg/checkpoint"
	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/embedder"
	"github.com/verikod/hector/pkg/memory"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/observability"
	"github.com/verikod/hector/pkg/rag"
	"github.com/verikod/hector/pkg/runner"
	"github.com/verikod/hector/pkg/session"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/agenttool"
	"github.com/verikod/hector/pkg/tool/mcptoolset"
	"github.com/verikod/hector/pkg/tool/searchtool"
	"github.com/verikod/hector/pkg/trigger"
	"github.com/verikod/hector/pkg/vector"
)

// Runtime manages the lifecycle of Hector agents built from config.
type Runtime struct {
	cfg *config.Config

	mu            sync.RWMutex
	llms          map[string]model.LLM
	embedders     map[string]embedder.Embedder // Embedders for semantic search
	toolsets      map[string]tool.Toolset
	agents        map[string]agent.Agent
	sessions      session.Service        // SOURCE OF TRUTH for all data
	index         memory.IndexService    // SEARCH INDEX (can be rebuilt from sessions)
	checkpoint    *checkpoint.Manager    // Checkpoint/recovery manager
	dbPool        *config.DBPool         // Shared database pool for SQL backends
	observability *observability.Manager // Tracing and metrics

	// RAG/Document Store components
	vectorProviders map[string]vector.Provider    // Vector database providers
	documentStores  map[string]*rag.DocumentStore // Document stores for RAG

	// Trigger scheduler for scheduled agent invocations
	scheduler *trigger.Scheduler

	// Factory functions (injectable for testing)
	llmFactory      LLMFactory
	embedderFactory EmbedderFactory
	toolsetFactory  ToolsetFactory

	// Multi-agent injection (for programmatic API)
	subAgents   map[string][]agent.Agent // Sub-agents per agent name (Pattern 1: transfer)
	agentTools  map[string][]agent.Agent // Agents as tools per agent name (Pattern 2: delegation)
	directTools map[string][]tool.Tool   // Direct tools per agent name
}

// LLMFactory creates an LLM from config.
type LLMFactory func(cfg *config.LLMConfig) (model.LLM, error)

// EmbedderFactory creates an Embedder from config.
type EmbedderFactory func(cfg *config.EmbedderConfig) (embedder.Embedder, error)

// ToolsetFactory creates a Toolset from config.
type ToolsetFactory func(name string, cfg *config.ToolConfig) (tool.Toolset, error)

// Option configures the runtime.
type Option func(*Runtime)

// WithLLMFactory sets a custom LLM factory.
func WithLLMFactory(f LLMFactory) Option {
	return func(r *Runtime) {
		r.llmFactory = f
	}
}

// WithEmbedderFactory sets a custom embedder factory.
func WithEmbedderFactory(f EmbedderFactory) Option {
	return func(r *Runtime) {
		r.embedderFactory = f
	}
}

// WithToolsetFactory sets a custom toolset factory.
func WithToolsetFactory(f ToolsetFactory) Option {
	return func(r *Runtime) {
		r.toolsetFactory = f
	}
}

// WithSessionService sets a custom session service.
func WithSessionService(s session.Service) Option {
	return func(r *Runtime) {
		r.sessions = s
	}
}

// WithDBPool sets the shared database pool for SQL backends.
// Required when SQL persistence is configured without explicit services.
func WithDBPool(pool *config.DBPool) Option {
	return func(r *Runtime) {
		r.dbPool = pool
	}
}

// WithIndexService sets a custom index service.
func WithIndexService(idx memory.IndexService) Option {
	return func(r *Runtime) {
		r.index = idx
	}
}

// WithObservability sets a custom observability manager.
func WithObservability(obs *observability.Manager) Option {
	return func(r *Runtime) {
		r.observability = obs
	}
}

// WithCheckpointManager sets a custom checkpoint manager.
func WithCheckpointManager(mgr *checkpoint.Manager) Option {
	return func(r *Runtime) {
		r.checkpoint = mgr
	}
}

// WithSubAgents adds sub-agents for an agent (Pattern 1: transfer).
// Transfer tools are automatically created for each sub-agent.
func WithSubAgents(agentName string, subAgents []agent.Agent) Option {
	return func(r *Runtime) {
		if r.subAgents == nil {
			r.subAgents = make(map[string][]agent.Agent)
		}
		r.subAgents[agentName] = append(r.subAgents[agentName], subAgents...)
	}
}

// WithAgentTools adds agents as tools for an agent (Pattern 2: delegation).
// The parent agent can call these as tools and receive structured results.
func WithAgentTools(agentName string, agentTools []agent.Agent) Option {
	return func(r *Runtime) {
		if r.agentTools == nil {
			r.agentTools = make(map[string][]agent.Agent)
		}
		r.agentTools[agentName] = append(r.agentTools[agentName], agentTools...)
	}
}

// WithDirectTools adds tools directly for an agent.
func WithDirectTools(agentName string, tools []tool.Tool) Option {
	return func(r *Runtime) {
		if r.directTools == nil {
			r.directTools = make(map[string][]tool.Tool)
		}
		r.directTools[agentName] = append(r.directTools[agentName], tools...)
	}
}

// New creates a new Runtime from config.
func New(cfg *config.Config, opts ...Option) (*Runtime, error) {
	r := &Runtime{
		cfg:             cfg,
		llms:            make(map[string]model.LLM),
		embedders:       make(map[string]embedder.Embedder),
		toolsets:        make(map[string]tool.Toolset),
		agents:          make(map[string]agent.Agent),
		vectorProviders: make(map[string]vector.Provider),
		documentStores:  make(map[string]*rag.DocumentStore),
		llmFactory:      DefaultLLMFactory,
		embedderFactory: DefaultEmbedderFactory,
		toolsetFactory:  DefaultToolsetFactory,
	}

	for _, opt := range opts {
		opt(r)
	}

	// Initialize observability if configured and not provided via Option
	// Note: During Reload(), observability is always rebuilt from config (full reload)
	if r.observability == nil && cfg.Observability != nil {
		obs, err := observability.NewManager(context.Background(), cfg.Observability)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize observability: %w", err)
		}
		r.observability = obs
	}

	// Create session service from config if not provided
	if r.sessions == nil {
		sessionSvc, err := session.NewSessionServiceFromConfig(cfg, r.dbPool)
		if err != nil {
			return nil, fmt.Errorf("failed to create session service: %w", err)
		}
		r.sessions = sessionSvc
	}

	// Create checkpoint manager if configured and not provided via Option
	// Note: During Reload(), checkpoint manager is always rebuilt from config (full reload)
	if r.checkpoint == nil && cfg.Storage.Checkpoint != nil {
		cpCfg := r.buildCheckpointConfig(cfg.Storage.Checkpoint)
		r.checkpoint = checkpoint.NewManager(cpCfg, r.sessions)
		if cpCfg.IsEnabled() {
			slog.Info("Checkpoint manager enabled",
				"strategy", cpCfg.Strategy,
				"auto_resume", cpCfg.ShouldAutoResume())
		}
	}

	// Build LLMs first (needed by agents and RAG)
	if err := r.buildLLMs(); err != nil {
		return nil, fmt.Errorf("failed to build LLMs: %w", err)
	}

	// Build embedders (needed by index service and document stores)
	if err := r.buildEmbedders(); err != nil {
		return nil, fmt.Errorf("failed to build embedders: %w", err)
	}

	// Build vector providers (needed by document stores)
	if err := r.buildVectorProviders(); err != nil {
		return nil, fmt.Errorf("failed to build vector providers: %w", err)
	}

	// Build toolsets BEFORE document stores (MCP extractors need tool access)
	if err := r.buildToolsets(); err != nil {
		return nil, fmt.Errorf("failed to build toolsets: %w", err)
	}

	// Build document stores (needed by agents with RAG access)
	// Must be after toolsets so MCP extractors can access MCP tools
	if err := r.buildDocumentStores(); err != nil {
		return nil, fmt.Errorf("failed to build document stores: %w", err)
	}

	// Create index service from config if not provided
	// The index is built on top of session.Service (the source of truth)
	if r.index == nil {
		indexSvc, err := memory.NewIndexServiceFromConfig(cfg, r.embedders)
		if err != nil {
			return nil, fmt.Errorf("failed to create index service: %w", err)
		}
		r.index = indexSvc
	}

	// Build agents
	if err := r.buildAgents(); err != nil {
		return nil, fmt.Errorf("failed to build agents: %w", err)
	}

	// Initialize scheduler for triggered agents
	if err := r.initScheduler(); err != nil {
		return nil, fmt.Errorf("failed to initialize scheduler: %w", err)
	}

	return r, nil
}

// buildLLMsInto builds LLM instances from config into the provided map.
// Used by both New() and Reload() to avoid code duplication.
func (r *Runtime) buildLLMsInto(cfg *config.Config, llmsMap map[string]model.LLM) error {
	for name, llmCfg := range cfg.LLMs {
		if llmCfg == nil {
			continue
		}

		llm, err := r.llmFactory(llmCfg)
		if err != nil {
			return fmt.Errorf("llm %q: %w", name, err)
		}

		llmsMap[name] = llm
		slog.Debug("Created LLM", "name", name, "provider", llmCfg.Provider, "model", llmCfg.Model)
	}

	return nil
}

// buildLLMs creates LLM instances from config.
func (r *Runtime) buildLLMs() error {
	return r.buildLLMsInto(r.cfg, r.llms)
}

// buildEmbeddersInto builds Embedder instances from config into the provided map.
// Used by both New() and Reload() to avoid code duplication.
func (r *Runtime) buildEmbeddersInto(cfg *config.Config, embeddersMap map[string]embedder.Embedder) error {
	for name, embCfg := range cfg.Embedders {
		if embCfg == nil {
			continue
		}

		emb, err := r.embedderFactory(embCfg)
		if err != nil {
			return fmt.Errorf("embedder %q: %w", name, err)
		}

		embeddersMap[name] = emb
		slog.Debug("Created embedder", "name", name, "provider", embCfg.Provider, "model", embCfg.Model)
	}

	return nil
}

// buildEmbedders creates Embedder instances from config.
func (r *Runtime) buildEmbedders() error {
	return r.buildEmbeddersInto(r.cfg, r.embedders)
}

// buildVectorProvidersInto builds vector provider instances from config into the provided map.
// Used by both New() and Reload() to avoid code duplication.
func (r *Runtime) buildVectorProvidersInto(cfg *config.Config, providersMap map[string]vector.Provider) error {
	for name, vecCfg := range cfg.VectorStores {
		if vecCfg == nil {
			continue
		}

		provider, err := rag.NewVectorProviderFromConfig(vecCfg)
		if err != nil {
			return fmt.Errorf("vector_store %q: %w", name, err)
		}

		providersMap[name] = provider
		slog.Debug("Created vector provider", "name", name, "type", vecCfg.Type)
	}

	return nil
}

// buildVectorProviders creates vector provider instances from config.
func (r *Runtime) buildVectorProviders() error {
	return r.buildVectorProvidersInto(r.cfg, r.vectorProviders)
}

// buildDocumentStoresInto builds document store instances from config into the provided map.
// Used by both New() and Reload() to avoid code duplication.
func (r *Runtime) buildDocumentStoresInto(
	cfg *config.Config,
	storesMap map[string]*rag.DocumentStore,
	toolsetsMap map[string]tool.Toolset,
	vectorProvidersMap map[string]vector.Provider,
	embeddersMap map[string]embedder.Embedder,
	llmsMap map[string]model.LLM,
) error {
	if len(cfg.DocumentStores) == 0 {
		return nil
	}

	// Create ToolCaller adapter for MCP extractors
	// This wraps the toolsets to provide MCP tool access
	var toolCaller rag.ToolCaller
	if len(toolsetsMap) > 0 {
		toolCaller = &toolCallerAdapter{toolsets: toolsetsMap}
	}

	// Build RAG factory dependencies
	deps := &rag.FactoryDeps{
		DBPool:          r.dbPool,
		VectorProviders: vectorProvidersMap,
		Embedders:       embeddersMap,
		LLMs:            llmsMap,
		ToolCaller:      toolCaller,
		Config:          cfg,
	}

	for name, storeCfg := range cfg.DocumentStores {
		if storeCfg == nil {
			continue
		}

		store, err := rag.NewDocumentStoreFromConfig(name, storeCfg, deps)
		if err != nil {
			return fmt.Errorf("document_store %q: %w", name, err)
		}

		storesMap[name] = store
		slog.Debug("Created document store",
			"name", name,
			"source_type", storeCfg.Source.Type,
			"vector_store", storeCfg.VectorStore,
			"embedder", storeCfg.Embedder)
	}

	return nil
}

// buildDocumentStores creates document store instances from config.
func (r *Runtime) buildDocumentStores() error {
	return r.buildDocumentStoresInto(
		r.cfg,
		r.documentStores,
		r.toolsets,
		r.vectorProviders,
		r.embedders,
		r.llms,
	)
}

// buildToolsetsInto builds toolset instances from config into the provided map.
// Used by both New() and Reload() to avoid code duplication.
func (r *Runtime) buildToolsetsInto(cfg *config.Config, toolsetsMap map[string]tool.Toolset) error {
	for name, toolCfg := range cfg.Tools {
		if toolCfg == nil || !toolCfg.IsEnabled() {
			continue
		}

		ts, err := r.toolsetFactory(name, toolCfg)
		if err != nil {
			return fmt.Errorf("tool %q: %w", name, err)
		}

		toolsetsMap[name] = ts
		slog.Debug("Created toolset", "name", name, "type", toolCfg.Type)
	}

	return nil
}

// buildToolsets creates toolset instances from config.
func (r *Runtime) buildToolsets() error {
	return r.buildToolsetsInto(r.cfg, r.toolsets)
}

// constructAgentGraph builds all agents from the current config and provided dependencies.
// It returns the agents map and auxiliary relationship maps (subAgents, agentTools).
// It re-implements the robust multi-pass logic from buildAgents.
func (r *Runtime) constructAgentGraph(llms map[string]model.LLM) (
	map[string]agent.Agent,
	map[string][]agent.Agent,
	map[string][]agent.Agent,
	error,
) {
	agents := make(map[string]agent.Agent)
	subAgentsMap := make(map[string][]agent.Agent)
	agentToolsMap := make(map[string][]agent.Agent)

	// Dependency tracking for topological build
	type pendingAgent struct {
		Name   string
		Config *config.AgentConfig
	}

	var pending []pendingAgent
	for name, cfg := range r.cfg.Agents {
		if cfg == nil {
			continue
		}
		pending = append(pending, pendingAgent{Name: name, Config: cfg})
	}

	// Loop until all pending agents are built
	for len(pending) > 0 {
		var nextPending []pendingAgent
		progress := false

		for _, p := range pending {
			name := p.Name
			cfg := p.Config

			// Check dependencies based on type
			ready := true

			// Workflow agents depend on SubAgents
			if isWorkflowAgentType(cfg.Type) {
				for _, subName := range cfg.SubAgents {
					if _, exists := agents[subName]; !exists {
						ready = false
						break
					}
				}
			}
			// LLM/Remote agents: dependencies are resolved in later passes or via maps

			if !ready {
				nextPending = append(nextPending, p)
				continue
			}

			// Build the agent
			var ag agent.Agent
			var err error

			if isWorkflowAgentType(cfg.Type) {
				// Resolve sub-agents (we know they exist now)
				var subAgents []agent.Agent
				for _, subName := range cfg.SubAgents {
					subAgents = append(subAgents, agents[subName])
				}
				ag, err = r.createWorkflowAgent(name, cfg, subAgents)
			} else if isRemoteAgentType(cfg.Type) {
				ag, err = r.createRemoteAgent(name, cfg)
			} else {
				// LLM Agent
				llm, ok := llms[cfg.LLM]
				if !ok {
					return nil, nil, nil, fmt.Errorf("agent %q: llm %q not found", name, cfg.LLM)
				}

				// Collect toolsets
				var agentToolsets []tool.Toolset
				if cfg.Tools == nil {
					for toolName, ts := range r.toolsets {
						if toolCfg, ok := r.cfg.Tools[toolName]; ok && toolCfg != nil && !toolCfg.IsEnabled() {
							continue
						}
						agentToolsets = append(agentToolsets, ts)
					}
				} else if len(cfg.Tools) == 0 {
					agentToolsets = []tool.Toolset{}
				} else {
					for _, toolName := range cfg.Tools {
						ts, err := r.resolveToolset(toolName)
						if err != nil {
							return nil, nil, nil, fmt.Errorf("agent %q: %w", name, err)
						}
						agentToolsets = append(agentToolsets, ts)
					}
				}

				// Create base agent without multi-agent relationships.
				// Relationships are wired up in Pass 3-4, then agents are rebuilt in Pass 4.
				ag, err = r.createLLMAgent(name, cfg, llm, agentToolsets, nil, nil)
			}

			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to build agent %q: %w", name, err)
			}

			agents[name] = ag
			progress = true
		}

		if !progress && len(nextPending) > 0 {
			var missing []string
			for _, p := range nextPending {
				missing = append(missing, p.Name)
			}
			return nil, nil, nil, fmt.Errorf("dependency cycle or missing dependencies: %v", missing)
		}

		pending = nextPending
	}

	// Pass 3: Wire up multi-agent relationships
	for name, cfg := range r.cfg.Agents {
		if cfg == nil || isWorkflowAgentType(cfg.Type) {
			continue
		}

		for _, subName := range cfg.SubAgents {
			subAgent, ok := agents[subName]
			if !ok {
				return nil, nil, nil, fmt.Errorf("agent %q: sub_agent %q not found", name, subName)
			}
			subAgentsMap[name] = append(subAgentsMap[name], subAgent)
		}

		for _, agToolName := range cfg.AgentTools {
			agentAsToolAgent, ok := agents[agToolName]
			if !ok {
				return nil, nil, nil, fmt.Errorf("agent %q: agent_tool %q not found", name, agToolName)
			}
			agentToolsMap[name] = append(agentToolsMap[name], agentAsToolAgent)
		}
	}

	// Pass 4: Rebuild LLM agents with relationships
	for name, cfg := range r.cfg.Agents {
		if cfg == nil || isWorkflowAgentType(cfg.Type) {
			continue
		}

		// Only rebuild if agent has config-based multi-agent links
		hasConfigSubAgents := len(cfg.SubAgents) > 0
		hasConfigAgentTools := len(cfg.AgentTools) > 0

		if !hasConfigSubAgents && !hasConfigAgentTools {
			continue
		}

		// Get LLM for this agent
		llm, ok := llms[cfg.LLM]
		if !ok {
			return nil, nil, nil, fmt.Errorf("agent %q: llm %q not found", name, cfg.LLM)
		}

		// Collect toolsets
		var agentToolsets []tool.Toolset
		if cfg.Tools == nil {
			for toolName, ts := range r.toolsets {
				if toolCfg, ok := r.cfg.Tools[toolName]; ok && toolCfg != nil && !toolCfg.IsEnabled() {
					continue
				}
				agentToolsets = append(agentToolsets, ts)
			}
		} else if len(cfg.Tools) == 0 {
			agentToolsets = []tool.Toolset{}
		} else {
			for _, toolName := range cfg.Tools {
				ts, err := r.resolveToolset(toolName)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("agent %q: %w", name, err)
				}
				agentToolsets = append(agentToolsets, ts)
			}
		}

		ag, err := r.createLLMAgent(name, cfg, llm, agentToolsets, subAgentsMap[name], agentToolsMap[name])
		if err != nil {
			return nil, nil, nil, fmt.Errorf("agent %q: %w", name, err)
		}

		agents[name] = ag
	}

	return agents, subAgentsMap, agentToolsMap, nil
}

// buildAgents creates agent instances from config.
func (r *Runtime) buildAgents() error {
	agents, subAgentsMap, agentToolsMap, err := r.constructAgentGraph(r.llms)
	if err != nil {
		return err
	}

	r.agents = agents
	r.subAgents = subAgentsMap
	r.agentTools = agentToolsMap

	slog.Info("Agents built successfully", "count", len(r.agents))
	return nil
}

// resolveToolset finds a toolset by name, or implicitly via MCP.
func (r *Runtime) resolveToolset(toolName string) (tool.Toolset, error) {
	if ts, ok := r.toolsets[toolName]; ok {
		return ts, nil
	}

	// Debug info collection
	var debugInfo []string

	// Try implicit MCP tools
	// Sort toolsets for deterministic resolution order
	var toolsetNames []string
	for name, ts := range r.toolsets {
		if _, ok := ts.(*mcptoolset.Toolset); ok {
			toolsetNames = append(toolsetNames, name)
		} else {
			debugInfo = append(debugInfo, fmt.Sprintf("Skip %s: type %T not MCP", name, ts))
		}
	}
	sort.Strings(toolsetNames)

	for _, name := range toolsetNames {
		ts := r.toolsets[name]
		if mcpTS, ok := ts.(*mcptoolset.Toolset); ok {
			// Check config for this toolset
			toolCfg := r.cfg.Tools[mcpTS.Name()]
			if toolCfg == nil {
				debugInfo = append(debugInfo, fmt.Sprintf("Skip %s: config missing", name))
				continue
			}
			if toolCfg.Type != config.ToolTypeMCP {
				debugInfo = append(debugInfo, fmt.Sprintf("Skip %s: type %s not MCP", name, toolCfg.Type))
				continue
			}

			allowed := false
			if len(toolCfg.Filter) > 0 {
				for _, allowedTool := range toolCfg.Filter {
					if allowedTool == toolName {
						allowed = true
						break
					}
				}
				if !allowed {
					debugInfo = append(debugInfo, fmt.Sprintf("Skip %s: tool %s filtered out (filter: %v)", name, toolName, toolCfg.Filter))
				}
			} else {
				// No filter = promiscuous (allows any tool)
				allowed = true
			}

			if allowed {
				return mcpTS.WithFilter([]string{toolName}), nil
			}
		}
	}

	return nil, fmt.Errorf("tool %q not found (checked: %v)", toolName, debugInfo)
}

// isWorkflowAgentType returns true if the type is a workflow agent type.
func isWorkflowAgentType(t string) bool {
	switch t {
	case "sequential", "parallel", "loop", "runner":
		return true
	default:
		return false
	}
}

// isRemoteAgentType returns true if the type is a remote agent type.
func isRemoteAgentType(t string) bool {
	return t == "remote"
}

// createWorkflowAgent creates a workflow agent from config.
func (r *Runtime) createWorkflowAgent(name string, cfg *config.AgentConfig, subAgents []agent.Agent) (agent.Agent, error) {
	switch cfg.Type {
	case "sequential":
		return workflowagent.NewSequential(workflowagent.SequentialConfig{
			Name:        name,
			Description: cfg.Description,
			SubAgents:   subAgents,
		})
	case "parallel":
		return workflowagent.NewParallel(workflowagent.ParallelConfig{
			Name:        name,
			Description: cfg.Description,
			SubAgents:   subAgents,
		})
	case "loop":
		return workflowagent.NewLoop(workflowagent.LoopConfig{
			Name:          name,
			Description:   cfg.Description,
			SubAgents:     subAgents,
			MaxIterations: cfg.MaxIterations,
		})
	case "runner":
		// Runner agents execute tools directly without LLM
		// Collect tools for the runner
		var tools []tool.Tool
		for _, toolName := range cfg.Tools {
			ts, err := r.resolveToolset(toolName)
			if err != nil {
				return nil, fmt.Errorf("runner agent %q: %w", name, err)
			}
			// Get tools from toolset
			resolvedTools, err := ts.Tools(nil)
			if err != nil {
				return nil, fmt.Errorf("runner agent %q: failed to resolve tools from %q: %w", name, toolName, err)
			}
			tools = append(tools, resolvedTools...)
		}
		return runneragent.New(runneragent.Config{
			Name:        name,
			Description: cfg.Description,
			Tools:       tools,
		})
	default:
		return nil, fmt.Errorf("unknown workflow agent type: %s", cfg.Type)
	}
}

// createRemoteAgent creates a remote A2A agent from config.
func (r *Runtime) createRemoteAgent(name string, cfg *config.AgentConfig) (agent.Agent, error) {
	// Determine agent card source
	var agentCardSource string
	if cfg.AgentCardFile != "" {
		agentCardSource = cfg.AgentCardFile
	} else if cfg.AgentCardURL != "" {
		agentCardSource = cfg.AgentCardURL
	}

	// Parse timeout
	var timeout time.Duration
	if cfg.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout %q: %w", cfg.Timeout, err)
		}
	}

	// Streaming mode:
	// - true (default): Uses message/stream (SSE), supports token-by-token streaming
	// - false: Uses message/send (blocking), complete response at once
	//
	// Note: Google ADK's to_a2a() adapter does NOT support message/stream.
	// For Hector-to-Hector, both endpoints work correctly.

	// Determine streaming mode - default to true if not specified
	streaming := true
	if cfg.Streaming != nil {
		streaming = *cfg.Streaming
	}

	return remoteagent.NewA2A(remoteagent.Config{
		Name:            name,
		Description:     cfg.Description,
		URL:             cfg.URL,
		AgentCardSource: agentCardSource,
		Headers:         cfg.Headers,
		Timeout:         timeout,
		Streaming:       streaming,
	})
}

// createLLMAgent creates an LLM agent from config.
func (r *Runtime) createLLMAgent(name string, cfg *config.AgentConfig, llm model.LLM, toolsets []tool.Toolset, subAgents []agent.Agent, agentTools []agent.Agent) (agent.Agent, error) {
	// Collect direct tools (injected via WithTool/WithTools)
	var tools []tool.Tool
	if directTools, ok := r.directTools[name]; ok {
		tools = append(tools, directTools...)
	}

	// Add search tool if agent has document store access
	if searchTool := r.createSearchToolForAgent(name, cfg); searchTool != nil {
		tools = append(tools, searchTool)
		slog.Debug("Added search tool for agent", "agent", name)
	}

	// Convert agent tools to tools (Pattern 2: delegation)
	for _, ag := range agentTools {
		tools = append(tools, agenttool.New(ag, nil))
	}

	// Collect sub-agents (Pattern 1: transfer) - subAgents arg already contains them

	// Build reasoning config
	var reasoning *llmagent.ReasoningConfig
	if cfg.Reasoning != nil {
		reasoning = &llmagent.ReasoningConfig{
			MaxIterations:         cfg.Reasoning.MaxIterations,
			EnableExitTool:        config.BoolValue(cfg.Reasoning.EnableExitTool, false),
			EnableEscalateTool:    config.BoolValue(cfg.Reasoning.EnableEscalateTool, false),
			CompletionInstruction: cfg.Reasoning.CompletionInstruction,
		}
	}

	// Build generate config from LLM config (for thinking, etc.)
	var generateConfig *model.GenerateConfig
	if llmCfg, ok := r.cfg.LLMs[cfg.LLM]; ok && llmCfg != nil {
		if llmCfg.Thinking != nil && config.BoolValue(llmCfg.Thinking.Enabled, false) {
			generateConfig = &model.GenerateConfig{
				EnableThinking: true,
			}
			if llmCfg.Thinking.BudgetTokens > 0 {
				generateConfig.ThinkingBudget = llmCfg.Thinking.BudgetTokens
			}
		}
	}

	// Add structured output config if specified
	if cfg.StructuredOutput != nil && cfg.StructuredOutput.Schema != nil {
		if generateConfig == nil {
			generateConfig = &model.GenerateConfig{}
		}
		generateConfig.ResponseMIMEType = "application/json"
		generateConfig.ResponseSchema = cfg.StructuredOutput.Schema
		generateConfig.ResponseSchemaName = cfg.StructuredOutput.Name
		generateConfig.ResponseSchemaStrict = cfg.StructuredOutput.Strict
		slog.Debug("Structured output enabled for agent",
			"agent", name,
			"schema_name", cfg.StructuredOutput.Name,
			"strict", cfg.StructuredOutput.IsStrict())
	}

	// Build working memory strategy from context config
	var workingMemory memory.WorkingMemoryStrategy
	if cfg.Context != nil {
		// Get model name for token counting
		modelName := ""
		if llmCfg, ok := r.cfg.LLMs[cfg.LLM]; ok && llmCfg != nil {
			modelName = llmCfg.Model
		}

		// Resolve summarizer LLM for summary_buffer strategy
		var summarizerLLM model.LLM
		if cfg.Context.Strategy == "summary_buffer" {
			if cfg.Context.SummarizerLLM != "" {
				// Use explicitly configured summarizer LLM
				summarizerLLM = r.llms[cfg.Context.SummarizerLLM]
				if summarizerLLM == nil {
					slog.Warn("Summarizer LLM not found, summarization disabled",
						"summarizer_llm", cfg.Context.SummarizerLLM,
						"agent", name)
				}
			} else {
				// Use the same LLM as the agent
				summarizerLLM = llm
			}
		}

		var err error
		workingMemory, err = DefaultWorkingMemoryFactory(WorkingMemoryFactoryOptions{
			Config:        cfg.Context,
			ModelName:     modelName,
			SummarizerLLM: summarizerLLM,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create working memory strategy: %w", err)
		}
	}

	// Build RAG context provider if IncludeContext is enabled
	var contextProvider llmagent.ContextProvider
	if config.BoolValue(cfg.IncludeContext, false) {
		contextProvider = r.createContextProviderForAgent(name, cfg)
		if contextProvider != nil {
			slog.Debug("RAG context injection enabled for agent", "agent", name)
		}
	}

	// Get metrics recorder from observability manager
	var metricsRecorder observability.Recorder
	if r.observability != nil {
		metricsRecorder = r.observability.Metrics()
	}

	// Update instruction with RAG context hint if enabled
	instruction := cfg.GetSystemPrompt()
	if contextProvider != nil {
		hint := "\n\nYou have access to relevant context from valid document stores, which will be automatically provided at the start of the conversation. Please prioritize using this context to answer questions. Do NOT use the 'search' tool if the answer is present in the provided context. Only use the search tool if the provided context is irrelevant or insufficient."
		instruction += hint
		slog.Debug("Appended RAG context hint to system instruction", "agent", name)
	}

	// Determine display name (prefer explicit Name, fallback to map key ID)
	// This ensures the agent Author name matches the UI Canvas label (which uses cfg.Name)
	// BUT we keep Name as 'name' (ID) for safe tool naming.
	displayName := name
	if cfg.Name != "" {
		displayName = cfg.Name
	}

	return llmagent.New(llmagent.Config{
		Name:            name,
		DisplayName:     displayName,
		Description:     cfg.Description,
		Model:           llm,
		Instruction:     instruction,
		Toolsets:        toolsets,
		Tools:           tools,
		SubAgents:       subAgents,
		EnableStreaming: config.BoolValue(cfg.Streaming, true),
		Reasoning:       reasoning,
		GenerateConfig:  generateConfig,
		WorkingMemory:   workingMemory,
		ContextProvider: contextProvider,
		MetricsRecorder: metricsRecorder,
	})
}

// GetAgent returns an agent by name.
func (r *Runtime) GetAgent(name string) (agent.Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ag, ok := r.agents[name]
	return ag, ok
}

// GetLLM returns an LLM by name.
func (r *Runtime) GetLLM(name string) (model.LLM, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	llm, ok := r.llms[name]
	return llm, ok
}

// ListAgents returns all agent names.
func (r *Runtime) ListAgents() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	return names
}

// SessionService returns the session service.
func (r *Runtime) SessionService() session.Service {
	return r.sessions
}

// IndexService returns the index service.
func (r *Runtime) IndexService() memory.IndexService {
	return r.index
}

// CheckpointManager returns the checkpoint manager.
// Returns nil if checkpointing is not configured.
func (r *Runtime) CheckpointManager() *checkpoint.Manager {
	return r.checkpoint
}

// Observability returns the observability manager.
// Returns nil if observability is not configured.
func (r *Runtime) Observability() *observability.Manager {
	return r.observability
}

// Tracer returns the tracer for distributed tracing.
// Returns nil if tracing is not enabled.
func (r *Runtime) Tracer() *observability.Tracer {
	if r.observability == nil {
		return nil
	}
	return r.observability.Tracer()
}

// Metrics returns the metrics recorder.
// Returns nil if metrics are not enabled.
func (r *Runtime) Metrics() *observability.Metrics {
	if r.observability == nil {
		return nil
	}
	return r.observability.Metrics()
}

// Config returns the runtime's configuration.
func (r *Runtime) Config() *config.Config {
	return r.cfg
}

// initScheduler creates the scheduler and registers all agent triggers.
func (r *Runtime) initScheduler() error {
	// Create invoker function that invokes agents directly
	invoker := func(ctx context.Context, agentName, input string) error {
		ag, ok := r.agents[agentName]
		if !ok {
			return fmt.Errorf("agent %q not found", agentName)
		}

		slog.Info("Trigger invoking agent",
			"agent", agentName,
			"input", input)

		// Create or get session for this trigger
		// Use a consistent session ID per agent to maintain context across triggers
		userID := "trigger"
		sessionID := fmt.Sprintf("trigger-%s", agentName)
		appName := r.cfg.Name

		// Try to get existing session
		var sess session.Session
		getResp, err := r.sessions.Get(ctx, &session.GetRequest{
			AppName:   appName,
			UserID:    userID,
			SessionID: sessionID,
		})
		if err == nil && getResp != nil {
			sess = getResp.Session
		} else {
			// Create new session
			createResp, err := r.sessions.Create(ctx, &session.CreateRequest{
				AppName:   appName,
				UserID:    userID,
				SessionID: sessionID,
			})
			if err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}
			sess = createResp.Session
		}

		// Create user content
		userContent := agent.NewTextContent(input, "user")

		// Create invocation context for triggered execution
		invCtx := agent.NewInvocationContext(ctx, agent.InvocationContextParams{
			Agent:       ag,
			Session:     sess,
			Branch:      "main",
			UserContent: userContent,
			RunConfig:   &agent.RunConfig{},
		})

		// Append user message to session BEFORE running agent (required for LLM agents)
		userEvent := agent.NewEvent(invCtx.InvocationID())
		userEvent.Author = "user"
		userEvent.Message = userContent.ToMessage()
		if err := r.sessions.AppendEvent(ctx, sess, userEvent); err != nil {
			return fmt.Errorf("failed to persist user message: %w", err)
		}

		// Execute agent and consume events
		for event, err := range ag.Run(invCtx) {
			if err != nil {
				return fmt.Errorf("invocation error: %w", err)
			}
			// Persist event to session via service
			if event != nil {
				if err := r.sessions.AppendEvent(ctx, sess, event); err != nil {
					slog.Warn("Failed to persist trigger event", "error", err)
				}
				if event.IsFinalResponse() {
					slog.Info("Trigger invocation completed",
						"agent", agentName,
						"response", event.TextContent())
				}
			}
		}
		return nil
	}

	// Create scheduler
	r.scheduler = trigger.NewScheduler(invoker)

	// Register all agent triggers
	for name, ag := range r.agents {
		cfg := r.cfg.Agents[name]
		if cfg == nil || cfg.Trigger == nil {
			continue
		}

		if err := r.scheduler.RegisterAgent(name, ag, cfg.Trigger); err != nil {
			return fmt.Errorf("failed to register trigger for agent %q: %w", name, err)
		}
	}

	return nil
}

// StartScheduler starts the trigger scheduler.
// Call this after the server is ready to receive requests.
func (r *Runtime) StartScheduler() {
	if r.scheduler != nil {
		r.scheduler.Start()
	}
}

// StopScheduler stops the trigger scheduler gracefully.
func (r *Runtime) StopScheduler() {
	if r.scheduler != nil {
		r.scheduler.Stop()
	}
}

// Close shuts down the runtime and releases resources.
func (r *Runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error

	// Stop scheduler first
	if r.scheduler != nil {
		r.scheduler.Stop()
	}

	// Shutdown observability first (flush traces/metrics)
	if r.observability != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.observability.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("observability: %w", err))
		}
	}

	// Close document stores
	for name, store := range r.documentStores {
		if err := store.Close(); err != nil {
			errs = append(errs, fmt.Errorf("document_store %q: %w", name, err))
		}
	}

	// Close vector providers
	for name, provider := range r.vectorProviders {
		if err := provider.Close(); err != nil {
			errs = append(errs, fmt.Errorf("vector_provider %q: %w", name, err))
		}
	}

	// Close LLMs
	for name, llm := range r.llms {
		if err := llm.Close(); err != nil {
			errs = append(errs, fmt.Errorf("llm %q: %w", name, err))
		}
	}

	// Close toolsets (if they implement io.Closer)
	for name, ts := range r.toolsets {
		if closer, ok := ts.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("toolset %q: %w", name, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing runtime: %v", errs)
	}

	return nil
}

// buildCheckpointConfig builds checkpoint config from storage checkpoint config.
// Shared helper for both New() and Reload() to avoid duplication.
func (r *Runtime) buildCheckpointConfig(cfg *config.CheckpointConfig) *checkpoint.Config {
	cpCfg := &checkpoint.Config{
		Enabled:    cfg.Enabled,
		Strategy:   checkpoint.Strategy(cfg.Strategy),
		Interval:   cfg.Interval,
		AfterTools: cfg.AfterTools,
		BeforeLLM:  cfg.BeforeLLM,
	}
	if cfg.Recovery != nil {
		cpCfg.Recovery = &checkpoint.RecoveryConfig{
			AutoResume:     cfg.Recovery.AutoResume,
			AutoResumeHITL: cfg.Recovery.AutoResumeHITL,
			Timeout:        cfg.Recovery.Timeout,
		}
	}
	cpCfg.SetDefaults()
	return cpCfg
}

// cleanupBuiltComponents cleans up partially built components on error.
// This is a helper for Reload() error handling.
func (r *Runtime) cleanupBuiltComponents(
	llms map[string]model.LLM,
	embedders map[string]embedder.Embedder,
	vectorProviders map[string]vector.Provider,
	toolsets map[string]tool.Toolset,
	documentStores map[string]*rag.DocumentStore,
	index memory.IndexService,
	observability *observability.Manager,
) {
	for _, llm := range llms {
		llm.Close()
	}
	for _, emb := range embedders {
		if closer, ok := emb.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	for _, provider := range vectorProviders {
		if closer, ok := provider.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	for _, ts := range toolsets {
		if closer, ok := ts.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	for _, store := range documentStores {
		store.Close()
	}
	if index != nil {
		if closer, ok := index.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	if observability != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := observability.Shutdown(ctx); err != nil {
			slog.Warn("Failed to shutdown observability during cleanup", "error", err)
		}
		cancel()
	}
}

// Reload rebuilds the runtime with new config (hot-reload).
// Sessions and memory are preserved, but all components are rebuilt from config.
// This uses the same build functions as New() to ensure consistency.
func (r *Runtime) Reload(newCfg *config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	slog.Info("Reloading configuration...")

	// 1. Validate new config
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// 2. Build new components (same order as New(), using shared build functions)
	oldCfg := r.cfg
	r.cfg = newCfg

	// Build new LLMs first (needed by agents and RAG)
	newLLMs := make(map[string]model.LLM)
	if err := r.buildLLMsInto(newCfg, newLLMs); err != nil {
		r.cfg = oldCfg // Rollback
		return err
	}

	// Build new embedders (needed by index service and document stores)
	newEmbedders := make(map[string]embedder.Embedder)
	if err := r.buildEmbeddersInto(newCfg, newEmbedders); err != nil {
		r.cleanupBuiltComponents(newLLMs, nil, nil, nil, nil, nil, nil)
		r.cfg = oldCfg // Rollback
		return err
	}

	// Build new vector providers (needed by document stores)
	newVectorProviders := make(map[string]vector.Provider)
	if err := r.buildVectorProvidersInto(newCfg, newVectorProviders); err != nil {
		r.cleanupBuiltComponents(newLLMs, newEmbedders, nil, nil, nil, nil, nil)
		r.cfg = oldCfg // Rollback
		return err
	}

	// Build new toolsets BEFORE document stores (MCP extractors need tool access)
	newToolsets := make(map[string]tool.Toolset)
	if err := r.buildToolsetsInto(newCfg, newToolsets); err != nil {
		r.cleanupBuiltComponents(newLLMs, newEmbedders, newVectorProviders, nil, nil, nil, nil)
		r.cfg = oldCfg // Rollback
		return err
	}

	// Build new document stores (needed by agents with RAG access)
	// Must be after toolsets so MCP extractors can access MCP tools
	newDocumentStores := make(map[string]*rag.DocumentStore)
	if err := r.buildDocumentStoresInto(newCfg, newDocumentStores, newToolsets, newVectorProviders, newEmbedders, newLLMs); err != nil {
		r.cleanupBuiltComponents(newLLMs, newEmbedders, newVectorProviders, newToolsets, nil, nil, nil)
		r.cfg = oldCfg // Rollback
		return err
	}

	// Build new index service from config (FULL reload - always rebuild from config)
	// The index is built on top of session.Service (the source of truth)
	var newIndex memory.IndexService
	indexSvc, err := memory.NewIndexServiceFromConfig(newCfg, newEmbedders)
	if err != nil {
		r.cleanupBuiltComponents(newLLMs, newEmbedders, newVectorProviders, newToolsets, newDocumentStores, nil, nil)
		r.cfg = oldCfg // Rollback
		return fmt.Errorf("failed to create index service: %w", err)
	}
	newIndex = indexSvc

	// Temporarily swap toolsets for agent building (constructAgentGraph reads from r.toolsets)
	oldToolsets := r.toolsets
	r.toolsets = newToolsets

	// Build new agents (this needs toolsets in place)
	newAgents, newSubAgents, newAgentTools, err := r.constructAgentGraph(newLLMs)
	if err != nil {
		r.toolsets = oldToolsets // Restore before cleanup
		r.cleanupBuiltComponents(newLLMs, newEmbedders, newVectorProviders, newToolsets, newDocumentStores, newIndex, nil)
		r.cfg = oldCfg // Rollback
		return fmt.Errorf("failed to build agents: %w", err)
	}

	// Rebuild observability from config (FULL reload - always rebuild from config)
	var newObservability *observability.Manager
	if newCfg.Observability != nil {
		obs, err := observability.NewManager(context.Background(), newCfg.Observability)
		if err != nil {
			r.toolsets = oldToolsets // Restore before cleanup
			r.cleanupBuiltComponents(newLLMs, newEmbedders, newVectorProviders, newToolsets, newDocumentStores, newIndex, nil)
			r.cfg = oldCfg // Rollback
			return fmt.Errorf("failed to rebuild observability: %w", err)
		}
		newObservability = obs
	}

	// Rebuild checkpoint manager from config (FULL reload - always rebuild from config)
	// Note: Unlike New(), Reload() always rebuilds from config, ignoring Option-injected components
	var newCheckpoint *checkpoint.Manager
	if newCfg.Storage.Checkpoint != nil {
		cpCfg := r.buildCheckpointConfig(newCfg.Storage.Checkpoint)
		newCheckpoint = checkpoint.NewManager(cpCfg, r.sessions)
		if cpCfg.IsEnabled() {
			slog.Info("Checkpoint manager enabled",
				"strategy", cpCfg.Strategy,
				"auto_resume", cpCfg.ShouldAutoResume())
		}
	}

	// Rebuild scheduler (needs agents to be ready)
	// Stop old scheduler first
	wasSchedulerRunning := false
	if r.scheduler != nil {
		wasSchedulerRunning = true
		r.scheduler.Stop()
	}

	// Temporarily swap agents for scheduler initialization
	oldAgents := r.agents
	r.agents = newAgents
	if err := r.initScheduler(); err != nil {
		// Restore old agents and toolsets before cleanup
		r.agents = oldAgents
		r.toolsets = oldToolsets
		r.cleanupBuiltComponents(newLLMs, newEmbedders, newVectorProviders, newToolsets, newDocumentStores, newIndex, newObservability)
		r.cfg = oldCfg // Rollback
		return fmt.Errorf("failed to rebuild scheduler: %w", err)
	}

	// 3. Atomic swap (preserve sessions/memory)
	// Save old values for cleanup
	oldLLMs := r.llms
	oldEmbedders := r.embedders
	oldVectorProviders := r.vectorProviders
	oldDocumentStores := r.documentStores
	oldIndex := r.index
	oldObservability := r.observability
	// Note: checkpoint manager doesn't need cleanup - it's just a reference

	// Atomically swap all components together
	// Note: r.toolsets and r.agents are already set from temporary swaps above,
	// but we include them here for clarity and consistency of the atomic swap pattern
	r.llms = newLLMs
	r.embedders = newEmbedders
	r.vectorProviders = newVectorProviders
	r.toolsets = newToolsets // Already set at line 1355, but included for atomic swap clarity
	r.documentStores = newDocumentStores
	r.index = newIndex
	r.agents = newAgents // Already set at line 1402, but included for atomic swap clarity
	r.subAgents = newSubAgents
	r.agentTools = newAgentTools
	if newObservability != nil {
		r.observability = newObservability
	}
	if newCheckpoint != nil {
		r.checkpoint = newCheckpoint
	}

	// Restart scheduler if it was running
	if wasSchedulerRunning && r.scheduler != nil {
		r.scheduler.Start()
	}

	// 4. Cleanup old resources after grace period
	go func() {
		time.Sleep(5 * time.Second)
		for _, llm := range oldLLMs {
			llm.Close()
		}
		for _, emb := range oldEmbedders {
			if closer, ok := emb.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
		for _, provider := range oldVectorProviders {
			if closer, ok := provider.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
		for _, ts := range oldToolsets {
			if closer, ok := ts.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
		for _, store := range oldDocumentStores {
			store.Close()
		}
		if oldIndex != nil && oldIndex != r.index {
			if closer, ok := oldIndex.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
		if oldObservability != nil && oldObservability != r.observability {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := oldObservability.Shutdown(ctx); err != nil {
				slog.Warn("Failed to shutdown old observability during cleanup", "error", err)
			}
			cancel()
		}
		// Note: oldCheckpoint and oldScheduler don't need cleanup - they're just references
		slog.Debug("Old resources cleaned up")
	}()

	slog.Info("✅ Configuration fully reloaded",
		"llms", len(newLLMs),
		"embedders", len(newEmbedders),
		"vector_providers", len(newVectorProviders),
		"tools", len(newToolsets),
		"document_stores", len(newDocumentStores),
		"agents", len(newAgents))
	return nil
}

// DefaultAgent returns the first agent (primary/default agent).
func (r *Runtime) DefaultAgent() (agent.Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return the first agent (could be made configurable via server.default_agent)
	for _, ag := range r.agents {
		return ag, true
	}
	return nil, false
}

// RunnerConfig creates a runner.Config for the given agent.
func (r *Runtime) RunnerConfig(agentName string) (*runner.Config, error) {
	ag, ok := r.GetAgent(agentName)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", agentName)
	}

	return &runner.Config{
		AppName:           r.cfg.Name,
		Agent:             ag,
		SessionService:    r.sessions,
		IndexService:      r.index,      // memory.IndexService implements runner.IndexService
		CheckpointManager: r.checkpoint, // checkpoint.Manager implements runner.CheckpointManager
	}, nil
}

// DefaultRunnerConfig creates a runner.Config for the default agent.
func (r *Runtime) DefaultRunnerConfig() (*runner.Config, error) {
	ag, ok := r.DefaultAgent()
	if !ok {
		return nil, fmt.Errorf("no agents configured")
	}

	return &runner.Config{
		AppName:           r.cfg.Name,
		Agent:             ag,
		SessionService:    r.sessions,
		IndexService:      r.index,      // memory.IndexService implements runner.IndexService
		CheckpointManager: r.checkpoint, // checkpoint.Manager implements runner.CheckpointManager
	}, nil
}

// NewAuthValidator creates a JWT validator from the server auth config.
// Returns nil if authentication is not enabled.
func (r *Runtime) NewAuthValidator() (auth.TokenValidator, error) {
	if r.cfg.Server.Auth == nil || !r.cfg.Server.Auth.IsEnabled() {
		return nil, nil
	}

	validator, err := auth.NewValidatorFromConfig(r.cfg.Server.Auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth validator: %w", err)
	}

	if validator != nil {
		slog.Info("JWT authentication enabled",
			"jwks_url", r.cfg.Server.Auth.JWKSURL,
			"issuer", r.cfg.Server.Auth.Issuer,
			"audience", r.cfg.Server.Auth.Audience,
		)
	}

	return validator, nil
}

// DocumentStores returns all document stores.
func (r *Runtime) DocumentStores() map[string]*rag.DocumentStore {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.documentStores
}

// GetDocumentStore returns a document store by name.
func (r *Runtime) GetDocumentStore(name string) (*rag.DocumentStore, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	store, ok := r.documentStores[name]
	return store, ok
}

// IndexDocumentStores indexes all document stores.
// This should be called after runtime initialization to populate the indexes.
// It can be called asynchronously to avoid blocking startup.
func (r *Runtime) IndexDocumentStores(ctx context.Context) error {
	r.mu.RLock()
	stores := r.documentStores
	r.mu.RUnlock()

	if len(stores) == 0 {
		return nil
	}

	slog.Info("Indexing document stores", "count", len(stores))

	var errs []error
	for name, store := range stores {
		slog.Debug("Indexing document store", "name", name)
		if err := store.Index(ctx); err != nil {
			slog.Warn("Failed to index document store",
				"name", name,
				"error", err)
			errs = append(errs, fmt.Errorf("document_store %q: %w", name, err))
			continue
		}
		slog.Info("Indexed document store",
			"name", name,
			"stats", store.Stats())
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors indexing document stores: %v", errs)
	}

	return nil
}

// StartDocumentStoreWatching starts file watching for all document stores.
// This enables automatic re-indexing when documents change.
func (r *Runtime) StartDocumentStoreWatching(ctx context.Context) error {
	r.mu.RLock()
	stores := r.documentStores
	r.mu.RUnlock()

	for name, store := range stores {
		if err := store.StartWatching(ctx); err != nil {
			slog.Warn("Failed to start watching document store",
				"name", name,
				"error", err)
		}
	}

	return nil
}

// createSearchToolForAgent creates a search tool for an agent based on document store access.
// Returns nil if the agent has no document store access.
func (r *Runtime) createSearchToolForAgent(agentName string, cfg *config.AgentConfig) tool.Tool {
	// Check if there are any document stores
	if len(r.documentStores) == 0 {
		return nil
	}

	// Determine which stores this agent can access
	var availableStores []string
	if cfg.DocumentStores == nil {
		// nil = access all stores
		for name := range r.documentStores {
			availableStores = append(availableStores, name)
		}
	} else if len(*cfg.DocumentStores) == 0 {
		// [] = no access
		return nil
	} else {
		// [...] = scoped access
		availableStores = *cfg.DocumentStores
	}

	// Filter to stores that actually exist
	validStores := make(map[string]*rag.DocumentStore)
	for _, name := range availableStores {
		if store, ok := r.documentStores[name]; ok {
			validStores[name] = store
		}
	}

	if len(validStores) == 0 {
		return nil
	}

	return searchtool.New(searchtool.Config{
		Stores:          validStores,
		AvailableStores: availableStores,
		MaxLimit:        50,
		DefaultLimit:    10,
	})
}

// createContextProviderForAgent creates a RAG context provider for an agent.
// Returns nil if the agent has no document store access.
func (r *Runtime) createContextProviderForAgent(agentName string, cfg *config.AgentConfig) llmagent.ContextProvider {
	slog.Debug("createContextProviderForAgent called",
		"agent", agentName,
		"document_stores_count", len(r.documentStores),
		"agent_doc_stores", cfg.DocumentStores)

	// Check if there are any document stores
	if len(r.documentStores) == 0 {
		slog.Debug("No document stores available for context provider", "agent", agentName)
		return nil
	}

	// Determine which stores this agent can access
	var availableStores []string
	if cfg.DocumentStores == nil {
		// nil = access all stores
		for name := range r.documentStores {
			availableStores = append(availableStores, name)
		}
	} else if len(*cfg.DocumentStores) == 0 {
		// [] = no access
		return nil
	} else {
		// [...] = scoped access
		availableStores = *cfg.DocumentStores
	}

	// Filter to stores that actually exist
	var validStores []*rag.DocumentStore
	for _, name := range availableStores {
		if store, ok := r.documentStores[name]; ok {
			validStores = append(validStores, store)
		}
	}

	if len(validStores) == 0 {
		return nil
	}

	// Get context limits from config (matches legacy fallback logic)
	// Priority: include_context_limit > document_store.search.top_k > default(10)
	maxDocs := 10 // default matches legacy searchConfig.TopK default
	if cfg.IncludeContextLimit != nil && *cfg.IncludeContextLimit > 0 {
		// Explicit limit set
		maxDocs = *cfg.IncludeContextLimit
	} else {
		// Fallback to search.top_k from document store config (like legacy)
		for _, storeName := range availableStores {
			if storeCfg, ok := r.cfg.DocumentStores[storeName]; ok && storeCfg.Search != nil {
				if storeCfg.Search.TopK > 0 {
					maxDocs = storeCfg.Search.TopK
					break // Use first store's config
				}
			}
		}
	}

	maxContentLen := 500 // default matches legacy
	if cfg.IncludeContextMaxLength != nil && *cfg.IncludeContextMaxLength > 0 {
		maxContentLen = *cfg.IncludeContextMaxLength
	}

	// Return a context provider function that queries document stores
	return func(ctx agent.ReadonlyContext, query string) (string, error) {
		// ReadonlyContext embeds context.Context, so we can use it directly
		return r.searchRAGContext(ctx, validStores, query, maxDocs, maxContentLen)
	}
}

// ragSearchResult pairs a search result with its source store name and description.
// Mirrors legacy's approach of including store metadata in context.
type ragSearchResult struct {
	result           rag.SearchResult
	storeName        string
	storeDescription string
}

// searchRAGContext searches document stores and formats results as context.
// Follows legacy format: "[Data source: storeName (description)] content"
func (r *Runtime) searchRAGContext(ctx context.Context, stores []*rag.DocumentStore, query string, maxDocs, maxContentLen int) (string, error) {
	var allResults []ragSearchResult

	// Search all stores (like legacy SearchAllStores)
	for _, store := range stores {
		resp, err := store.Search(ctx, rag.SearchRequest{
			Query: query,
			TopK:  maxDocs,
		})
		if err != nil {
			slog.Warn("RAG context search failed for store",
				"store", store.Name(),
				"error", err)
			continue // Don't fail the whole search
		}
		// Tag results with store name and description (like legacy)
		storeDesc := r.buildStoreDescription(store.Name())
		for _, result := range resp.Results {
			allResults = append(allResults, ragSearchResult{
				result:           result,
				storeName:        store.Name(),
				storeDescription: storeDesc,
			})
		}
	}

	if len(allResults) == 0 {
		return "", nil
	}

	// Limit total results (like legacy: cap to maxDocs)
	if len(allResults) > maxDocs {
		allResults = allResults[:maxDocs]
	}

	// Format results as context (matches legacy format exactly)
	var contextBuilder strings.Builder
	contextBuilder.WriteString("IMPORTANT: The following information is retrieved from the knowledge base to answer the user's question. Use it as the primary source of truth:\n")

	for _, item := range allResults {
		content := item.result.Content
		// Truncate content if needed (like legacy)
		if len(content) > maxContentLen {
			content = content[:maxContentLen] + "..."
		}

		// Format: [Data source: storeName (description)] content (matches legacy exactly)
		if item.storeDescription != "" {
			contextBuilder.WriteString(fmt.Sprintf("[Data source: %s (%s)] %s\n", item.storeName, item.storeDescription, content))
		} else {
			contextBuilder.WriteString(fmt.Sprintf("[Data source: %s] %s\n", item.storeName, content))
		}
	}

	return contextBuilder.String(), nil
}

// buildStoreDescription creates a human-readable description from store config and status.
// Matches legacy BuildStoreDescription function.
func (r *Runtime) buildStoreDescription(storeName string) string {
	if storeName == "" {
		return ""
	}

	storeCfg, ok := r.cfg.DocumentStores[storeName]
	if !ok || storeCfg == nil {
		return ""
	}

	var descParts []string

	// Add source type if available (like legacy)
	if storeCfg.Source != nil && storeCfg.Source.Type != "" {
		descParts = append(descParts, fmt.Sprintf("source: %s", storeCfg.Source.Type))
	}

	// Add source path if available (like legacy)
	if storeCfg.Source != nil && storeCfg.Source.Path != "" {
		descParts = append(descParts, fmt.Sprintf("path: %s", storeCfg.Source.Path))
	}

	if len(descParts) == 0 {
		return ""
	}

	return strings.Join(descParts, ", ")
}

// toolCallerAdapter bridges tool.Toolset to rag.ToolCaller.
// This allows MCP extractors to call MCP tools via the runtime's toolsets.
//
// Architecture note: This adapter bridges v2's tool interface (Name/Description/Call)
// to rag's Tool interface (GetInfo/Execute) which MCPExtractor expects.
type toolCallerAdapter struct {
	toolsets map[string]tool.Toolset
}

// GetTool returns a tool by name from any registered toolset.
func (a *toolCallerAdapter) GetTool(name string) (rag.Tool, error) {
	// Search through all toolsets for the named tool
	// Note: We pass nil context since we don't have one at this point
	// MCP tools should work without context for discovery
	for _, ts := range a.toolsets {
		tools, err := ts.Tools(nil)
		if err != nil {
			continue // Skip toolsets that fail to enumerate
		}
		for _, t := range tools {
			if t.Name() == name {
				return &toolAdapter{tool: t}, nil
			}
		}
	}
	return nil, fmt.Errorf("tool %q not found", name)
}

// toolAdapter wraps tool.Tool to implement rag.Tool.
// Bridges v2's CallableTool interface to rag's Tool interface.
type toolAdapter struct {
	tool tool.Tool
}

// GetInfo returns information about the tool.
// Extracts parameters from the tool's JSON schema if available.
func (a *toolAdapter) GetInfo() rag.ToolInfo {
	info := rag.ToolInfo{
		Name:        a.tool.Name(),
		Description: a.tool.Description(),
		Parameters:  []rag.ToolParameter{},
	}

	// Extract parameters from schema if tool is callable
	callable, ok := a.tool.(tool.CallableTool)
	if !ok {
		return info
	}

	schema := callable.Schema()
	if schema == nil {
		return info
	}

	// Parse JSON Schema to extract parameters
	// Schema format: {"type": "object", "properties": {...}, "required": [...]}
	props, _ := schema["properties"].(map[string]any)
	required := make(map[string]bool)
	if reqList, ok := schema["required"].([]any); ok {
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				required[s] = true
			}
		}
	}

	for name, propRaw := range props {
		prop, ok := propRaw.(map[string]any)
		if !ok {
			continue
		}

		param := rag.ToolParameter{
			Name:        name,
			Required:    required[name],
			Type:        "string", // Default
			Description: "",
		}

		if t, ok := prop["type"].(string); ok {
			param.Type = t
		}
		if d, ok := prop["description"].(string); ok {
			param.Description = d
		}

		info.Parameters = append(info.Parameters, param)
	}

	return info
}

// Execute calls the tool with the given arguments.
// Converts v2's map[string]any result to rag's ToolResult struct.
func (a *toolAdapter) Execute(ctx context.Context, args map[string]any) (rag.ToolResult, error) {
	// Check if it's a callable tool
	callable, ok := a.tool.(tool.CallableTool)
	if !ok {
		return rag.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("tool %q is not callable", a.tool.Name()),
		}, fmt.Errorf("tool %q is not callable", a.tool.Name())
	}

	// Call the tool
	// Note: MCP tools in v2 don't require a full tool.Context for execution
	// They work with a nil context for simple operations like document parsing
	result, err := callable.Call(nil, args)
	if err != nil {
		return rag.ToolResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// Check for error in result (MCP convention)
	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		return rag.ToolResult{
			Success: false,
			Error:   errMsg,
		}, nil
	}

	// Extract content from result
	// MCP tools return: {"result": "..."} or {"results": [...]} or {"content": "..."}
	content := ""
	if c, ok := result["result"].(string); ok {
		content = c
	} else if c, ok := result["content"].(string); ok {
		content = c
	} else if c, ok := result["text"].(string); ok {
		content = c
	} else if results, ok := result["results"].([]any); ok && len(results) > 0 {
		// Multiple results - join them
		var parts []string
		for _, r := range results {
			if s, ok := r.(string); ok {
				parts = append(parts, s)
			}
		}
		content = strings.Join(parts, "\n\n")
	}

	// Extract metadata if present
	var metadata interface{}
	if m, ok := result["metadata"]; ok {
		metadata = m
	}

	return rag.ToolResult{
		Success:  true,
		Content:  content,
		Metadata: metadata,
	}, nil
}
