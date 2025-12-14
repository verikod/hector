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

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/kadirpekel/hector/pkg/agent"
	"github.com/kadirpekel/hector/pkg/agent/llmagent"
	"github.com/kadirpekel/hector/pkg/agent/remoteagent"
	"github.com/kadirpekel/hector/pkg/agent/workflowagent"
	"github.com/kadirpekel/hector/pkg/auth"
	"github.com/kadirpekel/hector/pkg/checkpoint"
	"github.com/kadirpekel/hector/pkg/config"
	"github.com/kadirpekel/hector/pkg/embedder"
	"github.com/kadirpekel/hector/pkg/memory"
	"github.com/kadirpekel/hector/pkg/model"
	"github.com/kadirpekel/hector/pkg/observability"
	"github.com/kadirpekel/hector/pkg/rag"
	"github.com/kadirpekel/hector/pkg/runner"
	"github.com/kadirpekel/hector/pkg/session"
	"github.com/kadirpekel/hector/pkg/tool"
	"github.com/kadirpekel/hector/pkg/tool/agenttool"
	"github.com/kadirpekel/hector/pkg/tool/mcptoolset"
	"github.com/kadirpekel/hector/pkg/tool/searchtool"
	"github.com/kadirpekel/hector/pkg/vector"
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

	// Initialize observability if configured and not provided
	if r.observability == nil && cfg.Server.Observability != nil {
		obs, err := observability.NewManager(context.Background(), cfg.Server.Observability)
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

	// Create checkpoint manager if configured and not provided
	if r.checkpoint == nil && cfg.Server.Checkpoint != nil {
		cpCfg := &checkpoint.Config{
			Enabled:    cfg.Server.Checkpoint.Enabled,
			Strategy:   checkpoint.Strategy(cfg.Server.Checkpoint.Strategy),
			Interval:   cfg.Server.Checkpoint.Interval,
			AfterTools: cfg.Server.Checkpoint.AfterTools,
			BeforeLLM:  cfg.Server.Checkpoint.BeforeLLM,
		}
		if cfg.Server.Checkpoint.Recovery != nil {
			cpCfg.Recovery = &checkpoint.RecoveryConfig{
				AutoResume:     cfg.Server.Checkpoint.Recovery.AutoResume,
				AutoResumeHITL: cfg.Server.Checkpoint.Recovery.AutoResumeHITL,
				Timeout:        cfg.Server.Checkpoint.Recovery.Timeout,
			}
		}
		cpCfg.SetDefaults()
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

	return r, nil
}

// buildLLMs creates LLM instances from config.
func (r *Runtime) buildLLMs() error {
	for name, cfg := range r.cfg.LLMs {
		if cfg == nil {
			continue
		}

		llm, err := r.llmFactory(cfg)
		if err != nil {
			return fmt.Errorf("llm %q: %w", name, err)
		}

		r.llms[name] = llm
		slog.Debug("Created LLM", "name", name, "provider", cfg.Provider, "model", cfg.Model)
	}

	return nil
}

// buildEmbedders creates Embedder instances from config.
func (r *Runtime) buildEmbedders() error {
	for name, cfg := range r.cfg.Embedders {
		if cfg == nil {
			continue
		}

		emb, err := r.embedderFactory(cfg)
		if err != nil {
			return fmt.Errorf("embedder %q: %w", name, err)
		}

		r.embedders[name] = emb
		slog.Debug("Created embedder", "name", name, "provider", cfg.Provider, "model", cfg.Model)
	}

	return nil
}

// buildVectorProviders creates vector provider instances from config.
func (r *Runtime) buildVectorProviders() error {
	for name, cfg := range r.cfg.VectorStores {
		if cfg == nil {
			continue
		}

		provider, err := rag.NewVectorProviderFromConfig(cfg)
		if err != nil {
			return fmt.Errorf("vector_store %q: %w", name, err)
		}

		r.vectorProviders[name] = provider
		slog.Debug("Created vector provider", "name", name, "type", cfg.Type)
	}

	return nil
}

// buildDocumentStores creates document store instances from config.
func (r *Runtime) buildDocumentStores() error {
	if len(r.cfg.DocumentStores) == 0 {
		return nil
	}

	// Create ToolCaller adapter for MCP extractors
	// This wraps the runtime's toolsets to provide MCP tool access
	var toolCaller rag.ToolCaller
	if len(r.toolsets) > 0 {
		toolCaller = &toolCallerAdapter{toolsets: r.toolsets}
	}

	// Build RAG factory dependencies
	deps := &rag.FactoryDeps{
		DBPool:          r.dbPool,
		VectorProviders: r.vectorProviders,
		Embedders:       r.embedders,
		LLMs:            r.llms,
		ToolCaller:      toolCaller,
		Config:          r.cfg,
	}

	for name, cfg := range r.cfg.DocumentStores {
		if cfg == nil {
			continue
		}

		store, err := rag.NewDocumentStoreFromConfig(name, cfg, deps)
		if err != nil {
			return fmt.Errorf("document_store %q: %w", name, err)
		}

		r.documentStores[name] = store
		slog.Debug("Created document store",
			"name", name,
			"source_type", cfg.Source.Type,
			"vector_store", cfg.VectorStore,
			"embedder", cfg.Embedder)
	}

	return nil
}

// buildToolsets creates toolset instances from config.
func (r *Runtime) buildToolsets() error {
	for name, cfg := range r.cfg.Tools {
		if cfg == nil || !cfg.IsEnabled() {
			continue
		}

		ts, err := r.toolsetFactory(name, cfg)
		if err != nil {
			return fmt.Errorf("tool %q: %w", name, err)
		}

		r.toolsets[name] = ts
		slog.Debug("Created toolset", "name", name, "type", cfg.Type)
	}

	return nil
}

// buildAgents creates agent instances from config.
// Uses a multi-pass approach to handle dependencies:
// 1. First pass: Create LLM and remote agents (no sub-agent dependencies)
// 2. Second pass: Create workflow agents (depend on sub-agents)
// 3. Third pass: Wire up multi-agent relationships and rebuild
func (r *Runtime) buildAgents() error {
	// Iterative Build: Unified pass for all agents (LLM, Remote, Workflow)
	// handling dependencies automatically via topological iterative resolve.

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
					if _, exists := r.agents[subName]; !exists {
						ready = false
						break
					}
				}
			}
			// LLM/Remote agents have no "agent" dependencies for *creation*
			// (LLMs are pre-built, and tool/agent-tool wiring happens in later passes)

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
					subAgents = append(subAgents, r.agents[subName])
				}
				ag, err = r.createWorkflowAgent(name, cfg, subAgents)
			} else if isRemoteAgentType(cfg.Type) {
				ag, err = r.createRemoteAgent(name, cfg)
			} else {
				// LLM Agent
				llm, ok := r.llms[cfg.LLM]
				if !ok {
					return fmt.Errorf("agent %q: llm %q not found", name, cfg.LLM)
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
							return fmt.Errorf("agent %q: %w", name, err)
						}
						agentToolsets = append(agentToolsets, ts)
					}
				}

				ag, err = r.createLLMAgent(name, cfg, llm, agentToolsets)
			}

			if err != nil {
				return fmt.Errorf("failed to build agent %q: %w", name, err)
			}

			r.agents[name] = ag
			slog.Debug("Created agent", "name", name, "type", cfg.Type)
			progress = true
		}

		if !progress && len(nextPending) > 0 {
			// Cycle detected or missing dependency
			var missing []string
			for _, p := range nextPending {
				missing = append(missing, p.Name)
			}
			return fmt.Errorf("failed to build agents: dependency cycle or missing dependencies for: %v", missing)
		}

		pending = nextPending
	}

	// Pass 3: Wire up multi-agent relationships from config for LLM agents
	for name, cfg := range r.cfg.Agents {
		if cfg == nil || isWorkflowAgentType(cfg.Type) {
			continue
		}

		// Resolve sub_agents from config (Pattern 1: transfer)
		for _, subName := range cfg.SubAgents {
			subAgent, ok := r.agents[subName]
			if !ok {
				return fmt.Errorf("agent %q: sub_agent %q not found", name, subName)
			}
			if r.subAgents == nil {
				r.subAgents = make(map[string][]agent.Agent)
			}
			r.subAgents[name] = append(r.subAgents[name], subAgent)
		}

		// Resolve agent_tools from config (Pattern 2: delegation)
		for _, agToolName := range cfg.AgentTools {
			agentAsToolAgent, ok := r.agents[agToolName]
			if !ok {
				return fmt.Errorf("agent %q: agent_tool %q not found", name, agToolName)
			}
			if r.agentTools == nil {
				r.agentTools = make(map[string][]agent.Agent)
			}
			r.agentTools[name] = append(r.agentTools[name], agentAsToolAgent)
		}
	}

	// Pass 4: Rebuild LLM agents that have multi-agent links from config
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
		llm, ok := r.llms[cfg.LLM]
		if !ok {
			return fmt.Errorf("agent %q: llm %q not found", name, cfg.LLM)
		}

		// Collect toolsets
		// Consistent assignment pattern: nil/omitted = all enabled toolsets, [] = none, [...] = scoped
		var agentToolsets []tool.Toolset
		if cfg.Tools == nil {
			// Tools omitted (nil) - use all enabled toolsets (permissive default)
			for toolName, ts := range r.toolsets {
				// Only include enabled toolsets (buildToolsets already filters disabled, but double-check)
				if toolCfg, ok := r.cfg.Tools[toolName]; ok && toolCfg != nil && !toolCfg.IsEnabled() {
					continue
				}
				agentToolsets = append(agentToolsets, ts)
			}
		} else if len(cfg.Tools) == 0 {
			// Tools explicitly empty ([]) - no toolsets (explicit restriction)
			agentToolsets = []tool.Toolset{}
		} else {
			// Use explicitly listed toolsets
			for _, toolName := range cfg.Tools {
				ts, err := r.resolveToolset(toolName)
				if err != nil {
					return fmt.Errorf("agent %q: %w", name, err)
				}
				agentToolsets = append(agentToolsets, ts)
			}
		}

		// Rebuild with multi-agent links
		ag, err := r.createLLMAgent(name, cfg, llm, agentToolsets)
		if err != nil {
			return fmt.Errorf("agent %q: %w", name, err)
		}

		r.agents[name] = ag
		slog.Debug("Rebuilt agent with multi-agent links", "name", name,
			"sub_agents", cfg.SubAgents, "agent_tools", cfg.AgentTools)
	}

	return nil
}

// resolveToolset finds a toolset by name, or implicitly via MCP.
func (r *Runtime) resolveToolset(toolName string) (tool.Toolset, error) {
	if ts, ok := r.toolsets[toolName]; ok {
		return ts, nil
	}

	// Try implicit MCP tools
	// Sort toolsets for deterministic resolution order
	var toolsetNames []string
	for name, ts := range r.toolsets {
		if _, ok := ts.(*mcptoolset.Toolset); ok {
			toolsetNames = append(toolsetNames, name)
		}
	}
	sort.Strings(toolsetNames)

	for _, name := range toolsetNames {
		ts := r.toolsets[name]
		if mcpTS, ok := ts.(*mcptoolset.Toolset); ok {
			// Check config for this toolset
			toolCfg := r.cfg.Tools[mcpTS.Name()]
			if toolCfg == nil || toolCfg.Type != config.ToolTypeMCP {
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
			} else {
				// No filter = promiscuous (allows any tool)
				allowed = true
			}

			if allowed {
				return mcpTS.WithFilter([]string{toolName}), nil
			}
		}
	}

	return nil, fmt.Errorf("tool %q not found", toolName)
}

// isWorkflowAgentType returns true if the type is a workflow agent type.
func isWorkflowAgentType(t string) bool {
	switch t {
	case "sequential", "parallel", "loop":
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

	// Configure streaming if enabled
	var msgSendCfg *a2a.MessageSendConfig
	if cfg.Streaming != nil && *cfg.Streaming {
		blocking := false
		msgSendCfg = &a2a.MessageSendConfig{
			Blocking: &blocking,
		}
	}

	return remoteagent.NewA2A(remoteagent.Config{
		Name:              name,
		Description:       cfg.Description,
		URL:               cfg.URL,
		AgentCardSource:   agentCardSource,
		Headers:           cfg.Headers,
		Timeout:           timeout,
		MessageSendConfig: msgSendCfg,
	})
}

// createLLMAgent creates an LLM agent from config.
func (r *Runtime) createLLMAgent(name string, cfg *config.AgentConfig, llm model.LLM, toolsets []tool.Toolset) (agent.Agent, error) {
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
	if agentTools, ok := r.agentTools[name]; ok {
		for _, ag := range agentTools {
			tools = append(tools, agenttool.New(ag, nil))
		}
	}

	// Collect sub-agents (Pattern 1: transfer)
	var subAgents []agent.Agent
	if subs, ok := r.subAgents[name]; ok {
		subAgents = append(subAgents, subs...)
	}

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

	return llmagent.New(llmagent.Config{
		Name:            name,
		Description:     cfg.Description,
		Model:           llm,
		Instruction:     instruction,
		Toolsets:        toolsets,
		Tools:           tools,
		SubAgents:       subAgents,
		EnableStreaming: config.BoolValue(cfg.Streaming, false),
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

// Close shuts down the runtime and releases resources.
func (r *Runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error

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

// Reload rebuilds the runtime with new config (hot-reload).
// Sessions and memory are preserved, only LLMs/agents/tools are rebuilt.
func (r *Runtime) Reload(newCfg *config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	slog.Info("Reloading configuration...")

	// 1. Validate new config
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// 2. Build new components
	oldCfg := r.cfg
	r.cfg = newCfg

	// Build new LLMs
	newLLMs := make(map[string]model.LLM)
	for name, cfg := range newCfg.LLMs {
		if cfg == nil {
			continue
		}
		llm, err := r.llmFactory(cfg)
		if err != nil {
			r.cfg = oldCfg // Rollback
			return fmt.Errorf("llm %q: %w", name, err)
		}
		newLLMs[name] = llm
	}

	// Build new embedders
	newEmbedders := make(map[string]embedder.Embedder)
	for name, cfg := range newCfg.Embedders {
		if cfg == nil {
			continue
		}
		emb, err := r.embedderFactory(cfg)
		if err != nil {
			// Cleanup newly created LLMs
			for _, llm := range newLLMs {
				llm.Close()
			}
			r.cfg = oldCfg // Rollback
			return fmt.Errorf("embedder %q: %w", name, err)
		}
		newEmbedders[name] = emb
	}

	// Build new toolsets
	newToolsets := make(map[string]tool.Toolset)
	for name, cfg := range newCfg.Tools {
		if cfg == nil || !cfg.IsEnabled() {
			continue
		}
		ts, err := r.toolsetFactory(name, cfg)
		if err != nil {
			// Cleanup
			for _, llm := range newLLMs {
				llm.Close()
			}
			for _, emb := range newEmbedders {
				if closer, ok := emb.(interface{ Close() error }); ok {
					closer.Close()
				}
			}
			r.cfg = oldCfg // Rollback
			return fmt.Errorf("tool %q: %w", name, err)
		}
		newToolsets[name] = ts
	}

	// Update toolsets temporarily for agent building
	oldToolsets := r.toolsets
	r.toolsets = newToolsets

	// Build new agents (this needs toolsets in place)
	newAgents := make(map[string]agent.Agent)
	if err := r.buildAgentsInto(newAgents, newLLMs); err != nil {
		// Cleanup
		for _, llm := range newLLMs {
			llm.Close()
		}
		for _, emb := range newEmbedders {
			if closer, ok := emb.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
		for _, ts := range newToolsets {
			if closer, ok := ts.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
		r.cfg = oldCfg // Rollback
		r.toolsets = oldToolsets
		return fmt.Errorf("failed to build agents: %w", err)
	}

	// 3. Atomic swap (preserve sessions/memory)
	oldLLMs := r.llms
	oldEmbedders := r.embedders
	// oldToolsets already saved above

	r.llms = newLLMs
	r.embedders = newEmbedders
	r.agents = newAgents

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
		for _, ts := range oldToolsets {
			if closer, ok := ts.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
		slog.Debug("Old resources cleaned up")
	}()

	slog.Info("✅ Configuration reloaded",
		"llms", len(newLLMs),
		"agents", len(newAgents),
		"tools", len(newToolsets))
	return nil
}

// buildAgentsInto is a helper that builds agents into a provided map.
// Used by Reload to build new agents without modifying r.agents until swap.
func (r *Runtime) buildAgentsInto(agentsMap map[string]agent.Agent, llmsMap map[string]model.LLM) error {
	// Pass 1: Create LLM and remote agents first
	for name, cfg := range r.cfg.Agents {
		if cfg == nil {
			continue
		}

		if isWorkflowAgentType(cfg.Type) {
			continue
		}

		if isRemoteAgentType(cfg.Type) {
			ag, err := r.createRemoteAgent(name, cfg)
			if err != nil {
				return fmt.Errorf("remote agent %q: %w", name, err)
			}
			agentsMap[name] = ag
			continue
		}

		llm, ok := llmsMap[cfg.LLM]
		if !ok {
			return fmt.Errorf("agent %q: llm %q not found", name, cfg.LLM)
		}

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
				ts, ok := r.toolsets[toolName]
				if !ok {
					return fmt.Errorf("agent %q: tool %q not found", name, toolName)
				}
				agentToolsets = append(agentToolsets, ts)
			}
		}

		ag, err := r.createLLMAgent(name, cfg, llm, agentToolsets)
		if err != nil {
			return fmt.Errorf("agent %q: %w", name, err)
		}

		agentsMap[name] = ag
	}

	// Pass 2: Create workflow agents
	for name, cfg := range r.cfg.Agents {
		if cfg == nil || !isWorkflowAgentType(cfg.Type) {
			continue
		}

		var subAgents []agent.Agent
		for _, subName := range cfg.SubAgents {
			subAgent, ok := agentsMap[subName]
			if !ok {
				return fmt.Errorf("workflow agent %q: sub_agent %q not found", name, subName)
			}
			subAgents = append(subAgents, subAgent)
		}

		ag, err := r.createWorkflowAgent(name, cfg, subAgents)
		if err != nil {
			return fmt.Errorf("workflow agent %q: %w", name, err)
		}

		agentsMap[name] = ag
	}

	// Pass 3 & 4: Wire up multi-agent relationships (similar to buildAgents)
	// Reset sub-agent maps for new config
	r.subAgents = make(map[string][]agent.Agent)
	r.agentTools = make(map[string][]agent.Agent)

	for name, cfg := range r.cfg.Agents {
		if cfg == nil || isWorkflowAgentType(cfg.Type) {
			continue
		}

		for _, subName := range cfg.SubAgents {
			subAgent, ok := agentsMap[subName]
			if !ok {
				return fmt.Errorf("agent %q: sub_agent %q not found", name, subName)
			}
			r.subAgents[name] = append(r.subAgents[name], subAgent)
		}

		for _, agToolName := range cfg.AgentTools {
			agentAsToolAgent, ok := agentsMap[agToolName]
			if !ok {
				return fmt.Errorf("agent %q: agent_tool %q not found", name, agToolName)
			}
			r.agentTools[name] = append(r.agentTools[name], agentAsToolAgent)
		}
	}

	// Rebuild agents with multi-agent links
	for name, cfg := range r.cfg.Agents {
		if cfg == nil || isWorkflowAgentType(cfg.Type) {
			continue
		}

		hasConfigSubAgents := len(cfg.SubAgents) > 0
		hasConfigAgentTools := len(cfg.AgentTools) > 0

		if !hasConfigSubAgents && !hasConfigAgentTools {
			continue
		}

		llm, ok := llmsMap[cfg.LLM]
		if !ok {
			return fmt.Errorf("agent %q: llm %q not found", name, cfg.LLM)
		}

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
				ts, ok := r.toolsets[toolName]
				if !ok {
					return fmt.Errorf("agent %q: tool %q not found", name, toolName)
				}
				agentToolsets = append(agentToolsets, ts)
			}
		}

		ag, err := r.createLLMAgent(name, cfg, llm, agentToolsets)
		if err != nil {
			return fmt.Errorf("agent %q: %w", name, err)
		}

		agentsMap[name] = ag
	}

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
