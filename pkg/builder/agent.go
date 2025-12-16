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

package builder

import (
	"fmt"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/agent/llmagent"
	"github.com/verikod/hector/pkg/memory"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/tool"
)

// AgentBuilder provides a fluent API for building LLM agents.
//
// Example:
//
//	agent, err := builder.NewAgent("assistant").
//	    WithName("Assistant").
//	    WithDescription("A helpful AI assistant").
//	    WithLLM(llm).
//	    WithInstruction("You are a helpful assistant.").
//	    WithTools(tool1, tool2).
//	    Build()
type AgentBuilder struct {
	id          string
	name        string
	description string
	llm         model.LLM
	instruction string

	tools         []tool.Tool
	toolsets      []tool.Toolset
	subAgents     []agent.Agent
	workingMemory memory.WorkingMemoryStrategy
	reasoning     *llmagent.ReasoningConfig

	enableStreaming          bool
	disallowTransferToParent bool
	disallowTransferToPeers  bool
	outputKey                string
	inputSchema              map[string]any
	outputSchema             map[string]any

	beforeAgentCallbacks []agent.BeforeAgentCallback
	afterAgentCallbacks  []agent.AfterAgentCallback
	beforeModelCallbacks []llmagent.BeforeModelCallback
	afterModelCallbacks  []llmagent.AfterModelCallback
	beforeToolCallbacks  []llmagent.BeforeToolCallback
	afterToolCallbacks   []llmagent.AfterToolCallback
}

// NewAgent creates a new agent builder.
//
// The id must be unique within the agent tree.
//
// Example:
//
//	agent, err := builder.NewAgent("my-agent").
//	    WithLLM(llm).
//	    Build()
func NewAgent(id string) *AgentBuilder {
	if id == "" {
		panic("agent ID cannot be empty")
	}
	return &AgentBuilder{
		id:    id,
		tools: make([]tool.Tool, 0),
	}
}

// WithName sets the agent's display name.
//
// Example:
//
//	builder.NewAgent("my-agent").WithName("My Assistant")
func (b *AgentBuilder) WithName(name string) *AgentBuilder {
	b.name = name
	return b
}

// WithDescription sets the agent's description.
// The description helps LLMs decide when to delegate to this agent.
//
// Example:
//
//	builder.NewAgent("researcher").WithDescription("Researches topics in depth")
func (b *AgentBuilder) WithDescription(desc string) *AgentBuilder {
	b.description = desc
	return b
}

// WithLLM sets the LLM provider for the agent.
//
// Example:
//
//	llm, _ := builder.NewLLM("openai").Model("gpt-4o").Build()
//	builder.NewAgent("my-agent").WithLLM(llm)
func (b *AgentBuilder) WithLLM(llm model.LLM) *AgentBuilder {
	if llm == nil {
		panic("LLM cannot be nil")
	}
	b.llm = llm
	return b
}

// WithInstruction sets the system instruction for the agent.
// Supports template placeholders like {variable} resolved from state.
//
// Example:
//
//	builder.NewAgent("my-agent").WithInstruction("You are a helpful assistant.")
func (b *AgentBuilder) WithInstruction(instruction string) *AgentBuilder {
	b.instruction = instruction
	return b
}

// WithTool adds a single tool to the agent.
//
// Example:
//
//	builder.NewAgent("my-agent").WithTool(myTool)
func (b *AgentBuilder) WithTool(t tool.Tool) *AgentBuilder {
	if t == nil {
		panic("tool cannot be nil")
	}
	b.tools = append(b.tools, t)
	return b
}

// WithTools adds multiple tools to the agent.
//
// Example:
//
//	builder.NewAgent("my-agent").WithTools(tool1, tool2, tool3)
func (b *AgentBuilder) WithTools(tools ...tool.Tool) *AgentBuilder {
	for _, t := range tools {
		if t == nil {
			panic("tool cannot be nil")
		}
		b.tools = append(b.tools, t)
	}
	return b
}

// WithToolset adds a toolset to the agent.
//
// Example:
//
//	builder.NewAgent("my-agent").WithToolset(myToolset)
func (b *AgentBuilder) WithToolset(ts tool.Toolset) *AgentBuilder {
	if ts == nil {
		panic("toolset cannot be nil")
	}
	b.toolsets = append(b.toolsets, ts)
	return b
}

// WithToolsets adds multiple toolsets to the agent.
//
// Example:
//
//	builder.NewAgent("my-agent").WithToolsets(toolset1, toolset2)
func (b *AgentBuilder) WithToolsets(toolsets ...tool.Toolset) *AgentBuilder {
	for _, ts := range toolsets {
		if ts == nil {
			panic("toolset cannot be nil")
		}
		b.toolsets = append(b.toolsets, ts)
	}
	return b
}

// WithSubAgent adds a sub-agent for task delegation (Pattern 1: Transfer).
// Transfer tools are automatically created for each sub-agent.
//
// Example:
//
//	builder.NewAgent("coordinator").WithSubAgent(researcher)
func (b *AgentBuilder) WithSubAgent(ag agent.Agent) *AgentBuilder {
	if ag == nil {
		panic("sub-agent cannot be nil")
	}
	b.subAgents = append(b.subAgents, ag)
	return b
}

// WithSubAgents adds multiple sub-agents.
//
// Example:
//
//	builder.NewAgent("coordinator").WithSubAgents(researcher, writer)
func (b *AgentBuilder) WithSubAgents(agents ...agent.Agent) *AgentBuilder {
	for _, ag := range agents {
		if ag == nil {
			panic("sub-agent cannot be nil")
		}
		b.subAgents = append(b.subAgents, ag)
	}
	return b
}

// WithWorkingMemory sets the working memory strategy.
//
// Example:
//
//	strategy, _ := builder.NewWorkingMemory("summary_buffer").WithLLM(llm).Build()
//	builder.NewAgent("my-agent").WithWorkingMemory(strategy)
func (b *AgentBuilder) WithWorkingMemory(strategy memory.WorkingMemoryStrategy) *AgentBuilder {
	b.workingMemory = strategy
	return b
}

// WithReasoning sets the reasoning configuration.
//
// Example:
//
//	reasoning := builder.NewReasoning().MaxIterations(50).Build()
//	builder.NewAgent("my-agent").WithReasoning(reasoning)
func (b *AgentBuilder) WithReasoning(reasoning *llmagent.ReasoningConfig) *AgentBuilder {
	b.reasoning = reasoning
	return b
}

// WithReasoningBuilder sets reasoning from a builder (convenience method).
//
// Example:
//
//	builder.NewAgent("my-agent").WithReasoningBuilder(
//	    builder.NewReasoning().MaxIterations(50),
//	)
func (b *AgentBuilder) WithReasoningBuilder(rb *ReasoningBuilder) *AgentBuilder {
	if rb == nil {
		panic("reasoning builder cannot be nil")
	}
	b.reasoning = rb.Build()
	return b
}

// EnableStreaming enables token-by-token streaming.
//
// Example:
//
//	builder.NewAgent("my-agent").EnableStreaming(true)
func (b *AgentBuilder) EnableStreaming(enable bool) *AgentBuilder {
	b.enableStreaming = enable
	return b
}

// DisallowTransferToParent prevents delegation to parent agent.
//
// Example:
//
//	builder.NewAgent("my-agent").DisallowTransferToParent(true)
func (b *AgentBuilder) DisallowTransferToParent(disallow bool) *AgentBuilder {
	b.disallowTransferToParent = disallow
	return b
}

// DisallowTransferToPeers prevents delegation to sibling agents.
//
// Example:
//
//	builder.NewAgent("my-agent").DisallowTransferToPeers(true)
func (b *AgentBuilder) DisallowTransferToPeers(disallow bool) *AgentBuilder {
	b.disallowTransferToPeers = disallow
	return b
}

// WithOutputKey saves agent output to session state under this key.
//
// Example:
//
//	builder.NewAgent("researcher").WithOutputKey("research_results")
func (b *AgentBuilder) WithOutputKey(key string) *AgentBuilder {
	b.outputKey = key
	return b
}

// WithInputSchema validates input when agent is used as a tool.
//
// Example:
//
//	builder.NewAgent("my-agent").WithInputSchema(map[string]any{
//	    "type": "object",
//	    "properties": map[string]any{
//	        "query": map[string]any{"type": "string"},
//	    },
//	})
func (b *AgentBuilder) WithInputSchema(schema map[string]any) *AgentBuilder {
	b.inputSchema = schema
	return b
}

// WithOutputSchema enforces structured output format.
//
// Example:
//
//	builder.NewAgent("my-agent").WithOutputSchema(map[string]any{
//	    "type": "object",
//	    "properties": map[string]any{
//	        "answer": map[string]any{"type": "string"},
//	    },
//	})
func (b *AgentBuilder) WithOutputSchema(schema map[string]any) *AgentBuilder {
	b.outputSchema = schema
	return b
}

// WithBeforeAgentCallback adds a callback that runs before the agent starts.
//
// Example:
//
//	builder.NewAgent("my-agent").WithBeforeAgentCallback(func(ctx agent.CallbackContext) (*a2a.Content, error) {
//	    // Pre-processing logic
//	    return nil, nil
//	})
func (b *AgentBuilder) WithBeforeAgentCallback(cb agent.BeforeAgentCallback) *AgentBuilder {
	b.beforeAgentCallbacks = append(b.beforeAgentCallbacks, cb)
	return b
}

// WithAfterAgentCallback adds a callback that runs after the agent completes.
//
// Example:
//
//	builder.NewAgent("my-agent").WithAfterAgentCallback(func(ctx agent.CallbackContext) (*a2a.Content, error) {
//	    // Post-processing logic
//	    return nil, nil
//	})
func (b *AgentBuilder) WithAfterAgentCallback(cb agent.AfterAgentCallback) *AgentBuilder {
	b.afterAgentCallbacks = append(b.afterAgentCallbacks, cb)
	return b
}

// WithBeforeModelCallback adds a callback that runs before each LLM call.
//
// Example:
//
//	builder.NewAgent("my-agent").WithBeforeModelCallback(func(ctx agent.CallbackContext, req *model.Request) (*model.Response, error) {
//	    // Modify request or short-circuit
//	    return nil, nil
//	})
func (b *AgentBuilder) WithBeforeModelCallback(cb llmagent.BeforeModelCallback) *AgentBuilder {
	b.beforeModelCallbacks = append(b.beforeModelCallbacks, cb)
	return b
}

// WithAfterModelCallback adds a callback that runs after each LLM call.
//
// Example:
//
//	builder.NewAgent("my-agent").WithAfterModelCallback(func(ctx agent.CallbackContext, resp *model.Response, err error) (*model.Response, error) {
//	    // Modify response or handle error
//	    return resp, err
//	})
func (b *AgentBuilder) WithAfterModelCallback(cb llmagent.AfterModelCallback) *AgentBuilder {
	b.afterModelCallbacks = append(b.afterModelCallbacks, cb)
	return b
}

// WithBeforeToolCallback adds a callback that runs before tool execution.
//
// Example:
//
//	builder.NewAgent("my-agent").WithBeforeToolCallback(func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
//	    // Modify args or short-circuit
//	    return nil, nil
//	})
func (b *AgentBuilder) WithBeforeToolCallback(cb llmagent.BeforeToolCallback) *AgentBuilder {
	b.beforeToolCallbacks = append(b.beforeToolCallbacks, cb)
	return b
}

// WithAfterToolCallback adds a callback that runs after tool execution.
//
// Example:
//
//	builder.NewAgent("my-agent").WithAfterToolCallback(func(ctx tool.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
//	    // Modify result or handle error
//	    return result, err
//	})
func (b *AgentBuilder) WithAfterToolCallback(cb llmagent.AfterToolCallback) *AgentBuilder {
	b.afterToolCallbacks = append(b.afterToolCallbacks, cb)
	return b
}

// Build creates the agent.
//
// Returns an error if required parameters are missing.
func (b *AgentBuilder) Build() (agent.Agent, error) {
	if b.llm == nil {
		return nil, fmt.Errorf("LLM is required: use WithLLM()")
	}

	name := b.name
	if name == "" {
		name = b.id
	}

	cfg := llmagent.Config{
		Name:                     name,
		Description:              b.description,
		Model:                    b.llm,
		Instruction:              b.instruction,
		EnableStreaming:          b.enableStreaming,
		Tools:                    b.tools,
		Toolsets:                 b.toolsets,
		SubAgents:                b.subAgents,
		WorkingMemory:            b.workingMemory,
		Reasoning:                b.reasoning,
		DisallowTransferToParent: b.disallowTransferToParent,
		DisallowTransferToPeers:  b.disallowTransferToPeers,
		OutputKey:                b.outputKey,
		InputSchema:              b.inputSchema,
		OutputSchema:             b.outputSchema,
		BeforeAgentCallbacks:     b.beforeAgentCallbacks,
		AfterAgentCallbacks:      b.afterAgentCallbacks,
		BeforeModelCallbacks:     b.beforeModelCallbacks,
		AfterModelCallbacks:      b.afterModelCallbacks,
		BeforeToolCallbacks:      b.beforeToolCallbacks,
		AfterToolCallbacks:       b.afterToolCallbacks,
	}

	return llmagent.New(cfg)
}

// MustBuild creates the agent or panics on error.
//
// Use this only when you're certain the configuration is valid.
func (b *AgentBuilder) MustBuild() agent.Agent {
	ag, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build agent: %v", err))
	}
	return ag
}
