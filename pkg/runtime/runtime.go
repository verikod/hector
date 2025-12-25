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
	"strings"
	"sync"
	"time"

	"github.com/verikod/hector/pkg/agent"
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
}

// LLMFactory creates an LLM from config.
type LLMFactory func(cfg *config.LLMConfig) (model.LLM, error)

// EmbedderFactory creates an Embedder from config.
type EmbedderFactory func(cfg *config.EmbedderConfig) (embedder.Embedder, error)

// ToolsetFactory creates a Toolset from config.
type ToolsetFactory func(name string, cfg *config.ToolConfig) (tool.Toolset, error)

// New creates a new Runtime from config.
// This is a convenience function that delegates to Builder.
// For programmatic configuration, use NewBuilder() directly.
func New(cfg *config.Config) (*Runtime, error) {
	return NewBuilder().WithConfig(cfg).Build()
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
