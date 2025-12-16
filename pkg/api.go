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

// Package pkg provides a config-first AI agent platform with a clean programmatic API.
//
// Hector can be used in three ways:
//
// # Config-First (Primary)
//
// Load agents from YAML configuration:
//
//	h, err := pkg.FromConfig("config.yaml")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer h.Close()
//
//	// Start A2A server
//	h.Serve(":8080")
//
// # Programmatic API (Simple)
//
// Build a single agent programmatically:
//
//	h, err := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithMCPTool("weather", "http://localhost:9000"),
//	    pkg.WithInstruction("You are a helpful assistant."),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer h.Close()
//
//	response, err := h.Generate(ctx, "How is the weather in Berlin?")
//
// # Multi-Agent Patterns
//
// Hector supports two multi-agent patterns aligned with adk-go:
//
// Pattern 1 - Transfer (sub-agents take over):
//
//	// Create specialized agents
//	researcher, _ := pkg.NewAgent(llmagent.Config{
//	    Name:        "researcher",
//	    Description: "Researches topics in depth",
//	    Model:       model,
//	})
//	writer, _ := pkg.NewAgent(llmagent.Config{
//	    Name:        "writer",
//	    Description: "Writes content based on research",
//	    Model:       model,
//	})
//
//	// Create parent with sub-agents (transfer tools auto-created)
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithSubAgents(researcher, writer),
//	)
//
// Pattern 2 - Delegation (parent maintains control):
//
//	// Create specialized agents
//	searchAgent, _ := pkg.NewAgent(llmagent.Config{
//	    Name:        "web_search",
//	    Description: "Searches the web for information",
//	    Model:       model,
//	})
//
//	// Create parent that uses agent as a tool
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithAgentTool(searchAgent),  // Parent calls searchAgent, gets results back
//	)
//
// Or use the helper function directly:
//
//	searchTool := pkg.AgentAsTool(searchAgent)
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithTool(searchTool),
//	)
package pkg

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/agent/llmagent"
	"github.com/verikod/hector/pkg/agent/remoteagent"
	"github.com/verikod/hector/pkg/agent/workflowagent"
	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/model/anthropic"
	"github.com/verikod/hector/pkg/model/gemini"
	"github.com/verikod/hector/pkg/model/ollama"
	"github.com/verikod/hector/pkg/model/openai"
	"github.com/verikod/hector/pkg/runner"
	"github.com/verikod/hector/pkg/runtime"
	"github.com/verikod/hector/pkg/server"
	"github.com/verikod/hector/pkg/session"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/agenttool"
	"github.com/verikod/hector/pkg/tool/controltool"
)

// ============================================================================
// Re-exports for convenience (users don't need to import multiple packages)
// ============================================================================

// Type aliases for clean API
type (
	// Agent is the core agent interface.
	Agent = agent.Agent

	// Tool is the core tool interface.
	Tool = tool.Tool

	// CallableTool is a tool that can be called synchronously.
	CallableTool = tool.CallableTool

	// Toolset groups related tools.
	Toolset = tool.Toolset

	// LLM is the LLM interface.
	LLM = model.LLM

	// Event represents an agent event.
	Event = agent.Event

	// AgentConfig is the configuration for an agent.
	AgentConfig = config.AgentConfig

	// ReasoningConfig configures the reasoning loop.
	ReasoningConfig = config.ReasoningConfig

	// LLMConfig is the configuration for an LLM.
	LLMConfig = config.LLMConfig

	// ToolConfig is the configuration for a tool.
	ToolConfig = config.ToolConfig

	// LLMAgentConfig is the configuration for an LLM agent.
	LLMAgentConfig = llmagent.Config

	// LLMAgentReasoningConfig configures the reasoning loop for LLM agents.
	LLMAgentReasoningConfig = llmagent.ReasoningConfig
)

// NewAgent creates a new LLM agent programmatically.
// This is the recommended way to create agents for multi-agent patterns.
//
// Example:
//
//	model, _ := openai.New(openai.Config{APIKey: key})
//	agent, _ := pkg.NewAgent(pkg.AgentOptions{
//	    Name:        "researcher",
//	    Description: "Researches topics",
//	    Model:       model,
//	    Tools:       []tool.Tool{searchTool},
//	})
func NewAgent(cfg llmagent.Config) (Agent, error) {
	return llmagent.New(cfg)
}

// ============================================================================
// Agent Hierarchy Navigation (adk-go aligned)
// ============================================================================

// FindAgent searches for an agent by name in the agent tree.
// This provides the same functionality as ADK-Go's find_agent() method.
//
// Example:
//
//	root, _ := pkg.NewAgent(llmagent.Config{
//	    Name: "coordinator",
//	    SubAgents: []pkg.Agent{researcher, writer},
//	})
//
//	found := pkg.FindAgent(root, "researcher")
//	if found != nil {
//	    // Use the found agent
//	}
func FindAgent(root Agent, name string) Agent {
	return agent.FindAgent(root, name)
}

// FindAgentPath returns the path to an agent in the tree.
// The path is a slice of agent names from root to the target.
// Returns nil if the agent is not found.
func FindAgentPath(root Agent, name string) []string {
	return agent.FindAgentPath(root, name)
}

// WalkAgents visits all agents in the tree depth-first.
// The visitor function is called for each agent with its depth level.
//
// Example:
//
//	pkg.WalkAgents(root, func(ag pkg.Agent, depth int) bool {
//	    fmt.Printf("%s%s\n", strings.Repeat("  ", depth), ag.Name())
//	    return true // continue walking
//	})
func WalkAgents(root Agent, visitor func(Agent, int) bool) {
	agent.WalkAgents(root, visitor)
}

// ListAgents returns a flat list of all agents in the tree.
func ListAgents(root Agent) []Agent {
	return agent.ListAgents(root)
}

// AgentAsTool converts an agent to a tool (Pattern 2: delegation).
// The parent agent maintains control and receives structured results.
//
// Example:
//
//	searchTool := pkg.AgentAsTool(searchAgent)
//	parentAgent, _ := pkg.NewAgent(pkg.AgentOptions{
//	    Tools: []tool.Tool{searchTool},
//	})
func AgentAsTool(ag Agent) Tool {
	return agenttool.New(ag, nil)
}

// AgentAsToolWithConfig converts an agent to a tool with configuration.
func AgentAsToolWithConfig(ag Agent, cfg *agenttool.Config) Tool {
	return agenttool.New(ag, cfg)
}

// ExitLoopTool creates a tool for explicit loop termination.
func ExitLoopTool() Tool {
	return controltool.ExitLoop()
}

// EscalateTool creates a tool for escalating to parent agent.
func EscalateTool() Tool {
	return controltool.Escalate()
}

// TransferTool creates a tool for transferring to another agent.
func TransferTool(agentName, description string) Tool {
	return controltool.TransferTo(agentName, description)
}

// ============================================================================
// Workflow Agents (adk-go aligned)
// ============================================================================

// SequentialConfig is the configuration for a sequential agent.
type SequentialConfig = workflowagent.SequentialConfig

// ParallelConfig is the configuration for a parallel agent.
type ParallelConfig = workflowagent.ParallelConfig

// LoopConfig is the configuration for a loop agent.
type LoopConfig = workflowagent.LoopConfig

// NewSequentialAgent creates an agent that runs sub-agents once, in sequence.
//
// Use this when you want execution to occur in a fixed, strict order,
// such as a processing pipeline.
//
// Example:
//
//	stage1, _ := pkg.NewAgent(llmagent.Config{Name: "stage1", Model: model, ...})
//	stage2, _ := pkg.NewAgent(llmagent.Config{Name: "stage2", Model: model, ...})
//	stage3, _ := pkg.NewAgent(llmagent.Config{Name: "stage3", Model: model, ...})
//
//	pipeline, _ := pkg.NewSequentialAgent(pkg.SequentialConfig{
//	    Name:        "pipeline",
//	    Description: "Processes data through multiple stages",
//	    SubAgents:   []pkg.Agent{stage1, stage2, stage3},
//	})
func NewSequentialAgent(cfg SequentialConfig) (Agent, error) {
	return workflowagent.NewSequential(cfg)
}

// NewParallelAgent creates an agent that runs sub-agents simultaneously.
//
// All sub-agents receive the same input and run in parallel.
// Events from all sub-agents are yielded as they complete.
//
// Use this for:
//   - Running different algorithms simultaneously
//   - Generating multiple responses for review
//   - Getting diverse perspectives on a problem
//
// Example:
//
//	voter1, _ := pkg.NewAgent(llmagent.Config{Name: "voter1", Model: model, ...})
//	voter2, _ := pkg.NewAgent(llmagent.Config{Name: "voter2", Model: model, ...})
//	voter3, _ := pkg.NewAgent(llmagent.Config{Name: "voter3", Model: model, ...})
//
//	voters, _ := pkg.NewParallelAgent(pkg.ParallelConfig{
//	    Name:        "voters",
//	    Description: "Gets multiple perspectives",
//	    SubAgents:   []pkg.Agent{voter1, voter2, voter3},
//	})
func NewParallelAgent(cfg ParallelConfig) (Agent, error) {
	return workflowagent.NewParallel(cfg)
}

// NewLoopAgent creates an agent that runs sub-agents repeatedly.
//
// Runs sub-agents in sequence for N iterations or until escalation.
// Set MaxIterations=0 for indefinite looping until escalation.
//
// Use this for iterative refinement workflows, such as:
//   - Code review and improvement cycles
//   - Content revision loops
//   - Optimization iterations
//
// Example:
//
//	reviewer, _ := pkg.NewAgent(llmagent.Config{Name: "reviewer", Model: model, ...})
//	improver, _ := pkg.NewAgent(llmagent.Config{Name: "improver", Model: model, ...})
//
//	refiner, _ := pkg.NewLoopAgent(pkg.LoopConfig{
//	    Name:          "refiner",
//	    Description:   "Iteratively refines output",
//	    SubAgents:     []pkg.Agent{reviewer, improver},
//	    MaxIterations: 3,
//	})
func NewLoopAgent(cfg LoopConfig) (Agent, error) {
	return workflowagent.NewLoop(cfg)
}

// ============================================================================
// Remote Agents (adk-go aligned)
// ============================================================================

// RemoteAgentConfig is the configuration for a remote A2A agent.
type RemoteAgentConfig = remoteagent.Config

// NewRemoteAgent creates a remote A2A agent.
//
// Remote agents communicate with agents running in different processes
// or on different hosts using the A2A (Agent-to-Agent) protocol.
//
// Remote agents can be:
//   - Used as sub-agents for transfer patterns
//   - Wrapped as tools using AgentAsTool()
//   - Part of workflow agents (sequential, parallel, loop)
//
// Example:
//
//	// From URL (agent card resolved automatically)
//	remoteHelper, _ := pkg.NewRemoteAgent(pkg.RemoteAgentConfig{
//	    Name:        "remote_helper",
//	    Description: "A remote helper agent",
//	    URL:         "http://localhost:9000",
//	})
//
//	// Use as sub-agent
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithSubAgents(remoteHelper),
//	)
//
//	// Use as tool
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithAgentTool(remoteHelper),
//	)
func NewRemoteAgent(cfg RemoteAgentConfig) (Agent, error) {
	return remoteagent.NewA2A(cfg)
}

// Hector is the main entry point for the Hector platform.
// It wraps a runtime.Runtime and provides a clean API.
type Hector struct {
	runtime *runtime.Runtime
	cfg     *config.Config

	// Builder state (only used during New())
	//nolint:unused // Reserved for future use
	builder *builder
}

// builder collects options during New() before creating runtime
type builder struct {
	cfg         *config.Config
	llmFactory  runtime.LLMFactory     // Override LLM factory
	toolFactory runtime.ToolsetFactory // Override toolset factory
	sessions    session.Service        // Custom session service

	// Multi-agent state
	subAgents   map[string][]agent.Agent // Sub-agents per agent name (Pattern 1: transfer)
	agentTools  map[string][]agent.Agent // Agents as tools per agent name (Pattern 2: delegation)
	directTools map[string][]tool.Tool   // Direct tools per agent name
}

// ensureDefaultAgent creates the default "assistant" agent if not exists.
func ensureDefaultAgent(b *builder) {
	if _, ok := b.cfg.Agents["assistant"]; !ok {
		b.cfg.Agents["assistant"] = &config.AgentConfig{
			Name: "assistant",
		}
	}
}

// Option configures a Hector instance.
type Option func(*builder) error

// FromConfig creates a Hector instance from a config file.
func FromConfig(path string) (*Hector, error) {
	return FromConfigWithContext(context.Background(), path)
}

// FromConfigWithContext creates a Hector instance from a config file with context.
func FromConfigWithContext(ctx context.Context, path string) (*Hector, error) {
	// Load .env from config directory
	_ = config.LoadDotEnvForConfig(path)

	cfg, _, err := config.LoadConfigFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	rt, err := runtime.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	return &Hector{
		runtime: rt,
		cfg:     cfg,
	}, nil
}

// New creates a Hector instance programmatically.
// All options are converted to config and run through the same runtime as FromConfig.
func New(opts ...Option) (*Hector, error) {
	// Initialize builder with default config
	b := &builder{
		cfg: &config.Config{
			Version: "2",
			Name:    "hector",
			LLMs:    make(map[string]*config.LLMConfig),
			Tools:   make(map[string]*config.ToolConfig),
			Agents:  make(map[string]*config.AgentConfig),
		},
	}
	b.cfg.Server.SetDefaults()

	// Apply options
	for _, opt := range opts {
		if err := opt(b); err != nil {
			return nil, err
		}
	}

	// Ensure we have at least one LLM
	if len(b.cfg.LLMs) == 0 {
		return nil, fmt.Errorf("LLM is required: use WithAnthropic, WithOpenAI, etc.")
	}

	// Create default agent if none defined
	if len(b.cfg.Agents) == 0 {
		b.cfg.Agents["assistant"] = &config.AgentConfig{
			Name:        "assistant",
			Description: "Hector AI Assistant",
			LLM:         "default",
		}
	}

	// Link agent to first LLM if not set
	for _, agent := range b.cfg.Agents {
		if agent.LLM == "" {
			for name := range b.cfg.LLMs {
				agent.LLM = name
				break
			}
		}
	}

	// Process config (apply defaults, validate)
	b.cfg.SetDefaults()
	if err := b.cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	cfg := b.cfg

	// Build runtime options
	var rtOpts []runtime.Option
	if b.llmFactory != nil {
		rtOpts = append(rtOpts, runtime.WithLLMFactory(b.llmFactory))
	}
	if b.toolFactory != nil {
		rtOpts = append(rtOpts, runtime.WithToolsetFactory(b.toolFactory))
	}
	if b.sessions != nil {
		rtOpts = append(rtOpts, runtime.WithSessionService(b.sessions))
	}

	// Add multi-agent options
	for agentName, subs := range b.subAgents {
		rtOpts = append(rtOpts, runtime.WithSubAgents(agentName, subs))
	}
	for agentName, agTools := range b.agentTools {
		rtOpts = append(rtOpts, runtime.WithAgentTools(agentName, agTools))
	}
	for agentName, tools := range b.directTools {
		rtOpts = append(rtOpts, runtime.WithDirectTools(agentName, tools))
	}

	// Create runtime (same path as FromConfig!)
	rt, err := runtime.New(cfg, rtOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	return &Hector{
		runtime: rt,
		cfg:     cfg,
	}, nil
}

// ============================================================================
// LLM Options
// ============================================================================

// WithAnthropic configures Anthropic as the LLM provider.
func WithAnthropic(acfg anthropic.Config) Option {
	return func(b *builder) error {
		cfg := &config.LLMConfig{
			Provider: config.LLMProviderAnthropic,
			APIKey:   acfg.APIKey,
		}
		cfg.SetDefaults()

		if acfg.Model != "" {
			cfg.Model = acfg.Model
		}
		if acfg.MaxTokens > 0 {
			cfg.MaxTokens = acfg.MaxTokens
		}
		if acfg.Temperature != nil {
			cfg.Temperature = acfg.Temperature
		}
		if acfg.BaseURL != "" {
			cfg.BaseURL = acfg.BaseURL
		}
		if acfg.EnableThinking {
			cfg.Thinking = &config.ThinkingConfig{Enabled: config.BoolPtr(true), BudgetTokens: acfg.ThinkingBudget}
		}

		b.cfg.LLMs["default"] = cfg
		return nil
	}
}

// WithOpenAI configures OpenAI as the LLM provider.
func WithOpenAI(ocfg openai.Config) Option {
	return func(b *builder) error {
		cfg := &config.LLMConfig{
			Provider: config.LLMProviderOpenAI,
			APIKey:   ocfg.APIKey,
		}
		cfg.SetDefaults()

		if ocfg.Model != "" {
			cfg.Model = ocfg.Model
		}
		if ocfg.MaxTokens > 0 {
			cfg.MaxTokens = ocfg.MaxTokens
		}
		if ocfg.Temperature != nil {
			cfg.Temperature = ocfg.Temperature
		}
		if ocfg.BaseURL != "" {
			cfg.BaseURL = ocfg.BaseURL
		}
		if ocfg.EnableReasoning {
			cfg.Thinking = &config.ThinkingConfig{Enabled: config.BoolPtr(true), BudgetTokens: ocfg.ReasoningBudget}
		}

		b.cfg.LLMs["default"] = cfg
		return nil
	}
}

// WithGemini configures Gemini as the LLM provider.
func WithGemini(gcfg gemini.Config) Option {
	return func(b *builder) error {
		cfg := &config.LLMConfig{
			Provider: config.LLMProviderGemini,
			APIKey:   gcfg.APIKey,
		}
		cfg.SetDefaults()

		if gcfg.Model != "" {
			cfg.Model = gcfg.Model
		}
		if gcfg.MaxTokens > 0 {
			cfg.MaxTokens = gcfg.MaxTokens
		}
		if gcfg.Temperature > 0 {
			cfg.Temperature = &gcfg.Temperature
		}

		b.cfg.LLMs["default"] = cfg
		return nil
	}
}

// WithOllama configures Ollama as the LLM provider.
// Ollama runs locally and doesn't require an API key.
func WithOllama(ocfg ollama.Config) Option {
	return func(b *builder) error {
		cfg := &config.LLMConfig{
			Provider: config.LLMProviderOllama,
		}
		cfg.SetDefaults()

		if ocfg.Model != "" {
			cfg.Model = ocfg.Model
		}
		if ocfg.BaseURL != "" {
			cfg.BaseURL = ocfg.BaseURL
		}
		if ocfg.Temperature != nil {
			cfg.Temperature = ocfg.Temperature
		}
		if ocfg.NumPredict != nil {
			cfg.MaxTokens = *ocfg.NumPredict
		}
		if ocfg.EnableThinking {
			cfg.Thinking = &config.ThinkingConfig{Enabled: config.BoolPtr(true)}
		}

		b.cfg.LLMs["default"] = cfg
		return nil
	}
}

// WithLLM configures a custom LLM instance.
// This bypasses the factory and uses the provided LLM directly.
func WithLLM(llm model.LLM) Option {
	return func(b *builder) error {
		// Create a placeholder config
		b.cfg.LLMs["default"] = &config.LLMConfig{
			Provider: "custom",
			Model:    llm.Name(),
		}
		// Override factory to return this LLM
		b.llmFactory = func(cfg *config.LLMConfig) (model.LLM, error) {
			return llm, nil
		}
		return nil
	}
}

// WithLLMConfig adds an LLM from config.
func WithLLMConfig(name string, cfg *config.LLMConfig) Option {
	return func(b *builder) error {
		cfg.SetDefaults()
		b.cfg.LLMs[name] = cfg
		return nil
	}
}

// ============================================================================
// Tool Options
// ============================================================================

// WithMCPTool adds an MCP toolset using SSE transport.
func WithMCPTool(name, url string, filter ...string) Option {
	return func(b *builder) error {
		b.cfg.Tools[name] = &config.ToolConfig{
			Type:      config.ToolTypeMCP,
			URL:       url,
			Transport: "sse",
			Filter:    filter,
		}
		// Link to default agent
		if agent, ok := b.cfg.Agents["assistant"]; ok {
			agent.Tools = append(agent.Tools, name)
		}
		return nil
	}
}

// WithMCPToolHTTP adds an MCP toolset using streamable-http transport.
// Use this for Composio and other HTTP-based MCP servers.
func WithMCPToolHTTP(name, url string, filter ...string) Option {
	return func(b *builder) error {
		b.cfg.Tools[name] = &config.ToolConfig{
			Type:      config.ToolTypeMCP,
			URL:       url,
			Transport: "streamable-http",
			Filter:    filter,
		}
		// Link to default agent
		if agent, ok := b.cfg.Agents["assistant"]; ok {
			agent.Tools = append(agent.Tools, name)
		}
		return nil
	}
}

// WithMCPCommand adds an MCP toolset using stdio transport.
func WithMCPCommand(name, command string, args ...string) Option {
	return func(b *builder) error {
		b.cfg.Tools[name] = &config.ToolConfig{
			Type:      config.ToolTypeMCP,
			Command:   command,
			Args:      args,
			Transport: "stdio",
		}
		// Link to default agent
		if agent, ok := b.cfg.Agents["assistant"]; ok {
			agent.Tools = append(agent.Tools, name)
		}
		return nil
	}
}

// WithToolset adds a custom toolset.
func WithToolset(ts tool.Toolset) Option {
	return func(b *builder) error {
		name := ts.Name()
		b.cfg.Tools[name] = &config.ToolConfig{
			Type: "custom",
		}
		// Override factory to return this toolset
		originalFactory := b.toolFactory
		b.toolFactory = func(n string, cfg *config.ToolConfig) (tool.Toolset, error) {
			if n == name {
				return ts, nil
			}
			if originalFactory != nil {
				return originalFactory(n, cfg)
			}
			return runtime.DefaultToolsetFactory(n, cfg)
		}
		// Link to default agent
		if agent, ok := b.cfg.Agents["assistant"]; ok {
			agent.Tools = append(agent.Tools, name)
		}
		return nil
	}
}

// WithToolConfig adds a tool from config.
func WithToolConfig(name string, cfg *config.ToolConfig) Option {
	return func(b *builder) error {
		cfg.SetDefaults()
		b.cfg.Tools[name] = cfg
		return nil
	}
}

// ============================================================================
// Agent Options
// ============================================================================

// WithInstruction sets the system instruction for the default agent.
func WithInstruction(instruction string) Option {
	return func(b *builder) error {
		if _, ok := b.cfg.Agents["assistant"]; !ok {
			b.cfg.Agents["assistant"] = &config.AgentConfig{
				Name: "assistant",
			}
		}
		b.cfg.Agents["assistant"].Instruction = instruction
		return nil
	}
}

// WithAgentName sets the default agent name.
func WithAgentName(name string) Option {
	return func(b *builder) error {
		// Rename default agent
		if agent, ok := b.cfg.Agents["assistant"]; ok {
			delete(b.cfg.Agents, "assistant")
			agent.Name = name
			b.cfg.Agents[name] = agent
		}
		return nil
	}
}

// WithAgent adds a custom agent configuration.
func WithAgent(name string, cfg *config.AgentConfig) Option {
	return func(b *builder) error {
		cfg.SetDefaults(nil)
		b.cfg.Agents[name] = cfg
		return nil
	}
}

// WithReasoning configures the chain-of-thought reasoning loop.
// This controls how the agent iterates through tool calls and responses.
//
// Example:
//
//	pkg.WithReasoning(&config.ReasoningConfig{
//	    MaxIterations:      50,
//	    EnableExitTool:     true,
//	    EnableEscalateTool: true,
//	})
func WithReasoning(cfg *config.ReasoningConfig) Option {
	return func(b *builder) error {
		ensureDefaultAgent(b)
		cfg.SetDefaults()
		b.cfg.Agents["assistant"].Reasoning = cfg
		return nil
	}
}

// WithControlTools enables control flow tools for explicit loop termination.
// When enabled, the agent can call exit_loop to signal completion or
// escalate to delegate to a parent agent.
//
// Example:
//
//	pkg.WithControlTools(true, true)  // Enable both exit and escalate
func WithControlTools(enableExit, enableEscalate bool) Option {
	return func(b *builder) error {
		ensureDefaultAgent(b)
		if b.cfg.Agents["assistant"].Reasoning == nil {
			b.cfg.Agents["assistant"].Reasoning = &config.ReasoningConfig{}
		}
		b.cfg.Agents["assistant"].Reasoning.EnableExitTool = config.BoolPtr(enableExit)
		b.cfg.Agents["assistant"].Reasoning.EnableEscalateTool = config.BoolPtr(enableEscalate)
		return nil
	}
}

// WithStreaming enables token-by-token streaming for the default agent.
func WithStreaming(enabled bool) Option {
	return func(b *builder) error {
		ensureDefaultAgent(b)
		b.cfg.Agents["assistant"].Streaming = config.BoolPtr(enabled)
		return nil
	}
}

// ============================================================================
// Multi-Agent Options (Pattern 1: Transfer, Pattern 2: Delegation)
// ============================================================================

// WithSubAgents adds sub-agents for automatic transfer (Pattern 1).
// Transfer tools are automatically created for each sub-agent, allowing
// the parent agent to hand off control when needed.
//
// This follows the adk-go transfer pattern where the parent agent
// stops and the sub-agent takes over completely.
//
// Example:
//
//	researcher, _ := llmagent.New(llmagent.Config{Name: "researcher", ...})
//	writer, _ := llmagent.New(llmagent.Config{Name: "writer", ...})
//
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithSubAgents(researcher, writer),
//	)
//
// The agent will have transfer_to_researcher and transfer_to_writer tools.
func WithSubAgents(agents ...agent.Agent) Option {
	return func(b *builder) error {
		ensureDefaultAgent(b)

		// Store sub-agents to be linked during runtime creation
		if b.subAgents == nil {
			b.subAgents = make(map[string][]agent.Agent)
		}
		b.subAgents["assistant"] = append(b.subAgents["assistant"], agents...)
		return nil
	}
}

// WithAgentTool adds an agent as a callable tool (Pattern 2: delegation).
// The parent agent maintains control and receives structured results.
//
// This follows the adk-go agenttool pattern where the sub-agent runs
// in an isolated session and returns results to the parent.
//
// Example:
//
//	searchAgent, _ := llmagent.New(llmagent.Config{
//	    Name:        "web_search",
//	    Description: "Searches the web for information",
//	    Model:       model,
//	})
//
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithAgentTool(searchAgent),  // Parent can call web_search tool
//	)
func WithAgentTool(ag agent.Agent) Option {
	return func(b *builder) error {
		ensureDefaultAgent(b)

		// Store agent tools to be linked during runtime creation
		if b.agentTools == nil {
			b.agentTools = make(map[string][]agent.Agent)
		}
		b.agentTools["assistant"] = append(b.agentTools["assistant"], ag)
		return nil
	}
}

// WithAgentTools adds multiple agents as callable tools.
// Convenience wrapper for adding multiple agent tools at once.
//
// Example:
//
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithAgentTools(searchAgent, analysisAgent, writerAgent),
//	)
func WithAgentTools(agents ...agent.Agent) Option {
	return func(b *builder) error {
		for _, ag := range agents {
			if err := WithAgentTool(ag)(b); err != nil {
				return err
			}
		}
		return nil
	}
}

// ============================================================================
// Direct Tool Options
// ============================================================================

// WithTool adds a single tool directly.
// Use this when you have a tool.Tool implementation ready to use.
//
// Example:
//
//	myTool := &MyCustomTool{}
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithTool(myTool),
//	)
func WithTool(t tool.Tool) Option {
	return func(b *builder) error {
		ensureDefaultAgent(b)

		// Store direct tools to be linked during runtime creation
		if b.directTools == nil {
			b.directTools = make(map[string][]tool.Tool)
		}
		b.directTools["assistant"] = append(b.directTools["assistant"], t)
		return nil
	}
}

// WithTools adds multiple tools directly.
// Convenience wrapper for adding multiple tools at once.
//
// Example:
//
//	h, _ := pkg.New(
//	    pkg.WithOpenAI(openai.Config{APIKey: key}),
//	    pkg.WithTools(tool1, tool2, tool3),
//	)
func WithTools(tools ...tool.Tool) Option {
	return func(b *builder) error {
		for _, t := range tools {
			if err := WithTool(t)(b); err != nil {
				return err
			}
		}
		return nil
	}
}

// ============================================================================
// Session Options
// ============================================================================

// WithSessionService sets a custom session service.
func WithSessionService(s session.Service) Option {
	return func(b *builder) error {
		b.sessions = s
		return nil
	}
}

// ============================================================================
// Execution Methods
// ============================================================================

// Generate produces a response for the given input.
// This is a convenience method for simple single-turn interactions.
func (h *Hector) Generate(ctx context.Context, input string) (string, error) {
	var result string

	for event, err := range h.Run(ctx, input) {
		if err != nil {
			return "", err
		}
		if event.Message != nil {
			for _, part := range event.Message.Parts {
				if tp, ok := part.(a2a.TextPart); ok {
					result += tp.Text
				}
			}
		}
	}

	return result, nil
}

// GenerateStream produces a streaming response.
func (h *Hector) GenerateStream(ctx context.Context, input string) iter.Seq2[*agent.Event, error] {
	return h.Run(ctx, input)
}

// Run executes the agent and streams events.
func (h *Hector) Run(ctx context.Context, input string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Get default agent
		ag, ok := h.runtime.DefaultAgent()
		if !ok {
			yield(nil, fmt.Errorf("no agent configured"))
			return
		}

		// Create runner
		r, err := runner.New(runner.Config{
			AppName:        h.cfg.Name,
			Agent:          ag,
			SessionService: h.runtime.SessionService(),
		})
		if err != nil {
			yield(nil, fmt.Errorf("failed to create runner: %w", err))
			return
		}

		// Create user content
		content := &agent.Content{
			Role:  a2a.MessageRoleUser,
			Parts: []a2a.Part{a2a.TextPart{Text: input}},
		}

		// Run agent
		for event, err := range r.Run(ctx, "default-user", "default-session", content, agent.RunConfig{}) {
			if !yield(event, err) {
				return
			}
		}
	}
}

// RunWithSession executes the agent with a specific session.
func (h *Hector) RunWithSession(ctx context.Context, userID, sessionID, input string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		ag, ok := h.runtime.DefaultAgent()
		if !ok {
			yield(nil, fmt.Errorf("no agent configured"))
			return
		}

		r, err := runner.New(runner.Config{
			AppName:        h.cfg.Name,
			Agent:          ag,
			SessionService: h.runtime.SessionService(),
		})
		if err != nil {
			yield(nil, fmt.Errorf("failed to create runner: %w", err))
			return
		}

		content := &agent.Content{
			Role:  a2a.MessageRoleUser,
			Parts: []a2a.Part{a2a.TextPart{Text: input}},
		}

		for event, err := range r.Run(ctx, userID, sessionID, content, agent.RunConfig{}) {
			if !yield(event, err) {
				return
			}
		}
	}
}

// ============================================================================
// Server Methods
// ============================================================================

// Serve starts the A2A server.
func (h *Hector) Serve(addr string) error {
	cfg, err := h.runtime.DefaultRunnerConfig()
	if err != nil {
		return fmt.Errorf("no agent configured: %w", err)
	}

	executor := server.NewExecutor(server.ExecutorConfig{
		RunnerConfig: *cfg,
	})

	handler := a2asrv.NewHandler(executor)
	httpHandler := a2asrv.NewJSONRPCHandler(handler)

	mux := http.NewServeMux()
	mux.Handle("/", httpHandler)

	slog.Info("Starting Hector server", "address", addr)
	return http.ListenAndServe(addr, mux)
}

// ============================================================================
// Accessors
// ============================================================================

// Close releases all resources.
func (h *Hector) Close() error {
	return h.runtime.Close()
}

// Runtime returns the underlying runtime.
func (h *Hector) Runtime() *runtime.Runtime {
	return h.runtime
}

// Config returns the loaded configuration.
func (h *Hector) Config() *config.Config {
	return h.cfg
}

// Agent returns an agent by name.
func (h *Hector) Agent(name string) (agent.Agent, bool) {
	return h.runtime.GetAgent(name)
}

// DefaultAgent returns the default agent.
func (h *Hector) DefaultAgent() (agent.Agent, bool) {
	return h.runtime.DefaultAgent()
}

// SessionService returns the session service.
func (h *Hector) SessionService() session.Service {
	return h.runtime.SessionService()
}

// ============================================================================
// Builder Re-exports (for comprehensive programmatic API)
// ============================================================================
//
// For full programmatic control, use the builder package directly:
//
//	import "github.com/verikod/hector/pkg/builder"
//
// The builder package provides fluent APIs for:
//   - builder.NewAgent() - Build LLM agents
//   - builder.NewLLM() - Build LLM providers
//   - builder.NewEmbedder() - Build embedding providers
//   - builder.NewVectorProvider() - Build vector databases
//   - builder.NewDocumentStore() - Build RAG document stores
//   - builder.NewMCP() - Build MCP toolsets
//   - builder.NewToolset() - Wrap tools into toolsets
//   - builder.NewRunner() - Build agent runners
//   - builder.NewServer() - Build A2A servers
//   - builder.FunctionTool() - Create tools from Go functions
//   - builder.NewReasoning() - Configure reasoning loops
//   - builder.NewWorkingMemory() - Configure memory strategies
//
// Example using builder package for full control:
//
//	import "github.com/verikod/hector/pkg/builder"
//
//	// Build LLM
//	llm := builder.NewLLM("openai").
//	    Model("gpt-4o").
//	    APIKeyFromEnv("OPENAI_API_KEY").
//	    MustBuild()
//
//	// Build custom tool
//	type Args struct {
//	    Query string `json:"query" jsonschema:"required"`
//	}
//	searchTool, _ := builder.FunctionTool("search", "Search for information",
//	    func(ctx tool.Context, args Args) (map[string]any, error) {
//	        return map[string]any{"results": []string{"result1"}}, nil
//	    })
//
//	// Build agent
//	agent, _ := builder.NewAgent("assistant").
//	    WithLLM(llm).
//	    WithTool(searchTool).
//	    WithReasoning(builder.NewReasoning().MaxIterations(50).Build()).
//	    Build()
//
//	// Build runner
//	r, _ := builder.NewRunner("my-app").
//	    WithAgent(agent).
//	    Build()
//
//	// Run agent
//	for event, err := range r.Run(ctx, "user1", "session1", content, agent.RunConfig{}) {
//	    // Handle events
//	}
// ============================================================================
