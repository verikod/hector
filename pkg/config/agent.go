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

package config

import "fmt"

// AgentConfig configures an agent.
type AgentConfig struct {
	// Name is the display name of the agent (human-readable).
	// The actual unique identifier is the map key in the agents section.
	Name string `yaml:"name,omitempty" json:"name,omitempty" jsonschema:"title=Agent Display Name,description=Human-readable display name for the agent (e.g. 'AI Assistant')"`

	// Description describes what the agent does.
	Description string `yaml:"description,omitempty" json:"description,omitempty" jsonschema:"title=Description,description=Human-readable description of agent's purpose"`

	// Visibility controls agent discovery and access.
	// Values:
	//   - "public" (default): Visible in discovery, accessible via HTTP (if auth enabled, requires auth)
	//   - "internal": Visible in discovery ONLY if authenticated, accessible via HTTP (requires auth)
	//   - "private": Hidden from discovery, NOT accessible via HTTP (internal calls only)
	Visibility string `yaml:"visibility,omitempty" json:"visibility,omitempty" jsonschema:"title=Visibility,description=Controls agent discovery and access,enum=public,enum=internal,enum=private,default=public"`

	// LLM references a configured LLM by name.
	LLM string `yaml:"llm,omitempty" json:"llm,omitempty" jsonschema:"title=LLM Reference,description=References a configured LLM by name,default=default"`

	// Tools lists tool names this agent can use.
	Tools []string `yaml:"tools,omitempty" json:"tools,omitempty" jsonschema:"title=Tools,description=List of tool names this agent can use"`

	// SubAgents lists agent names that can receive transferred control (Pattern 1).
	// Transfer tools are automatically created for each sub-agent.
	// The parent agent can call "transfer_to_<name>" to hand off control.
	//
	// Example:
	//   agents:
	//     coordinator:
	//       sub_agents: [researcher, writer]
	//     researcher:
	//       instruction: "You research topics..."
	//     writer:
	//       instruction: "You write content..."
	SubAgents []string `yaml:"sub_agents,omitempty" json:"sub_agents,omitempty" jsonschema:"title=Sub-Agents,description=Child agents that can receive transferred control"`

	// AgentTools lists agent names to use as callable tools (Pattern 2).
	// The parent agent maintains control and receives structured results.
	// The tool name will be the agent name (e.g., "web_search").
	//
	// Example:
	//   agents:
	//     researcher:
	//       agent_tools: [web_search, data_analysis]
	//     web_search:
	//       instruction: "You search the web..."
	//     data_analysis:
	//       instruction: "You analyze data..."
	AgentTools []string `yaml:"agent_tools,omitempty" json:"agent_tools,omitempty" jsonschema:"title=Agent Tools,description=Agent names to use as callable tools"`

	// Instruction is the system prompt for the agent.
	// Supports template placeholders:
	//   {variable}           - session state
	//   {app:variable}       - app-scoped state
	//   {user:variable}      - user-scoped state
	//   {temp:variable}      - temp-scoped state
	//   {artifact.filename}  - artifact content
	//   {variable?}          - optional (empty if not found)
	Instruction string `yaml:"instruction,omitempty" json:"instruction,omitempty" jsonschema:"title=System Instruction,description=System prompt that defines agent behavior"`

	// InstructionFile is a path to a file containing the system instruction.
	// If provided, the content of this file is read and used as Instruction.
	// Supports SKILL.md files which contain frontmatter + markdown body.
	// The file path is relative to the config file location.
	InstructionFile string `yaml:"instruction_file,omitempty" json:"instruction_file,omitempty" jsonschema:"title=Instruction File,description=Path to a file containing the system instruction"`

	// GlobalInstruction applies to all agents in the tree (root only).
	// Supports the same template placeholders as Instruction.
	GlobalInstruction string `yaml:"global_instruction,omitempty" json:"global_instruction,omitempty" jsonschema:"title=Global Instruction,description=Instruction applied to all agents in the tree"`

	// Reasoning configures the chain-of-thought reasoning loop.
	Reasoning *ReasoningConfig `yaml:"reasoning,omitempty" json:"reasoning,omitempty" jsonschema:"title=Reasoning Configuration,description=Chain-of-thought reasoning loop settings"`

	// Context configures working memory / context window management.
	// Controls how conversation history is managed to fit within LLM limits.
	Context *ContextConfig `yaml:"context,omitempty" json:"context,omitempty" jsonschema:"title=Context Configuration,description=Working memory and context window settings"`

	// Guardrails references a named guardrails configuration.
	// Controls input validation, output filtering, and tool authorization.
	Guardrails string `yaml:"guardrails,omitempty" json:"guardrails,omitempty" jsonschema:"title=Guardrails Reference,description=References a named guardrails configuration"`

	// Prompt provides detailed prompt configuration.
	Prompt *PromptConfig `yaml:"prompt,omitempty" json:"prompt,omitempty" jsonschema:"title=Prompt Configuration,description=Detailed prompt configuration"`

	// Skills describes agent capabilities for A2A discovery.
	Skills []SkillConfig `yaml:"skills,omitempty" json:"skills,omitempty" jsonschema:"title=Skills,description=Agent capabilities for A2A discovery"`

	// InputModes are supported input MIME types.
	InputModes []string `yaml:"input_modes,omitempty" json:"input_modes,omitempty" jsonschema:"title=Input Modes,description=Supported input MIME types"`

	// OutputModes are supported output MIME types.
	OutputModes []string `yaml:"output_modes,omitempty" json:"output_modes,omitempty" jsonschema:"title=Output Modes,description=Supported output MIME types"`

	// Streaming enables token-by-token streaming from the LLM.
	Streaming *bool `yaml:"streaming,omitempty" json:"streaming,omitempty" jsonschema:"title=Enable Streaming,description=Token-by-token streaming from LLM,default=false"`

	// DocumentStores lists document store names this agent can search.
	// Controls scoped access to RAG document stores.
	// Values:
	//   - nil/omitted: Agent can search ALL document stores
	//   - []: Agent has NO document store access
	//   - [stores...]: Agent can only search listed stores
	//
	// When document stores are available:
	//   - A "search" tool is automatically added to the agent
	//   - If IncludeContext=true, relevant context is auto-injected
	//
	// Example:
	//   agents:
	//     researcher:
	//       document_stores: [codebase, docs]  # Scoped to these stores
	//     admin:
	//       document_stores: []                # No RAG access
	//     default:
	//       # omitted = access all stores
	DocumentStores *[]string `yaml:"document_stores,omitempty" json:"document_stores,omitempty" jsonschema:"title=Document Stores,description=Document stores accessible to this agent"`

	// IncludeContext enables automatic context injection from RAG.
	// When true, relevant document chunks are automatically included
	// in the system prompt based on the user's message.
	// Requires DocumentStores access.
	// Default: false
	IncludeContext *bool `yaml:"include_context,omitempty" json:"include_context,omitempty" jsonschema:"title=Include Context,description=Automatically inject RAG context,default=false"`

	// IncludeContextLimit sets the maximum number of documents to include.
	// Only used when IncludeContext=true.
	// Default: 5
	IncludeContextLimit *int `yaml:"include_context_limit,omitempty" json:"include_context_limit,omitempty" jsonschema:"title=Include Context Limit,description=Maximum number of documents to include,minimum=1,default=5"`

	// IncludeContextMaxLength sets the maximum content length per document (chars).
	// Longer content is truncated.
	// Only used when IncludeContext=true.
	// Default: 500
	IncludeContextMaxLength *int `yaml:"include_context_max_length,omitempty" json:"include_context_max_length,omitempty" jsonschema:"title=Include Context Max Length,description=Maximum content length per document (chars),minimum=1,default=500"`

	// StructuredOutput configures JSON schema response format.
	// When set, the LLM will return responses matching the specified schema.
	//
	// Example:
	//   structured_output:
	//     schema:
	//       type: object
	//       properties:
	//         sentiment:
	//           type: string
	//           enum: ["positive", "negative", "neutral"]
	//         confidence:
	//           type: number
	//       required: ["sentiment", "confidence"]
	StructuredOutput *StructuredOutputConfig `yaml:"structured_output,omitempty" json:"structured_output,omitempty" jsonschema:"title=Structured Output,description=JSON schema response format configuration"`

	// Type specifies the agent type.
	// Values:
	//   - "llm" (default): LLM-powered agent
	//   - "sequential": Runs sub-agents in sequence
	//   - "parallel": Runs sub-agents in parallel
	//   - "loop": Runs sub-agents repeatedly
	//   - "remote": Remote A2A agent
	//   - "runner": Executes tools in sequence without LLM
	Type string `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"title=Agent Type,description=Type of agent,enum=llm,enum=sequential,enum=parallel,enum=loop,enum=remote,enum=runner,default=llm"`

	// MaxIterations is the maximum iterations for loop agents.
	// Only used when Type="loop". If 0, loops until escalation.
	MaxIterations uint `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty" jsonschema:"title=Max Iterations,description=Maximum iterations for loop agents,minimum=0"`

	// Trigger configures automatic agent invocation on a schedule.
	// When configured, the agent will be invoked automatically based on the trigger.
	Trigger *TriggerConfig `yaml:"trigger,omitempty" json:"trigger,omitempty" jsonschema:"title=Trigger,description=Automatic invocation trigger (e.g. cron schedule)"`

	// === Remote Agent Configuration (Type="remote") ===

	// URL is the base URL of the remote A2A server.
	// Used when Type="remote".
	// Example: "http://localhost:9000"
	URL string `yaml:"url,omitempty" json:"url,omitempty" jsonschema:"title=Remote URL,description=Base URL of remote A2A server"`

	// AgentCardURL is the URL to fetch the agent card from.
	// If not provided and URL is set, defaults to "{URL}/.well-known/agent-card.json".
	AgentCardURL string `yaml:"agent_card_url,omitempty" json:"agent_card_url,omitempty" jsonschema:"title=Agent Card URL,description=URL to fetch agent card from"`

	// AgentCardFile is a local file path to the agent card JSON.
	// Takes precedence over AgentCardURL.
	AgentCardFile string `yaml:"agent_card_file,omitempty" json:"agent_card_file,omitempty" jsonschema:"title=Agent Card File,description=Local file path to agent card JSON"`

	// Headers are custom HTTP headers for remote agent requests.
	// Useful for authentication.
	// Example:
	//   headers:
	//     Authorization: "Bearer ${API_TOKEN}"
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty" jsonschema:"title=HTTP Headers,description=Custom headers for remote requests"`

	// Timeout is the request timeout for remote agents.
	// Default: "30s"
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty" jsonschema:"title=Timeout,description=Request timeout,default=30s"`
}

// PromptConfig provides detailed prompt configuration.
type PromptConfig struct {
	// SystemPrompt is the full system prompt (overrides Instruction).
	SystemPrompt string `yaml:"system_prompt,omitempty" json:"system_prompt,omitempty" jsonschema:"title=System Prompt,description=Full system prompt (overrides Instruction)"`

	// Role defines the agent's role.
	Role string `yaml:"role,omitempty" json:"role,omitempty" jsonschema:"title=Role,description=Agent's role"`

	// Guidance provides additional instructions.
	Guidance string `yaml:"guidance,omitempty" json:"guidance,omitempty" jsonschema:"title=Guidance,description=Additional instructions"`
}

// SkillConfig describes an agent skill for A2A discovery.
type SkillConfig struct {
	// ID is a unique identifier for the skill.
	ID string `yaml:"id,omitempty" json:"id,omitempty" jsonschema:"title=Skill ID,description=Unique identifier for the skill"`

	// Name is the display name.
	Name string `yaml:"name,omitempty" json:"name,omitempty" jsonschema:"title=Skill Name,description=Display name"`

	// Description explains what the skill does.
	Description string `yaml:"description,omitempty" json:"description,omitempty" jsonschema:"title=Skill Description,description=What this skill does"`

	// Tags for categorization.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty" jsonschema:"title=Tags,description=Tags for categorization"`

	// Examples of prompts this skill handles.
	Examples []string `yaml:"examples,omitempty" json:"examples,omitempty" jsonschema:"title=Examples,description=Example prompts this skill handles"`
}

// ContextConfig configures working memory / context window management.
// This controls how conversation history is managed to fit within LLM context limits.
// Ported from legacy pkg/memory patterns for use in v2.
type ContextConfig struct {
	// Strategy determines how context window is managed.
	// Values:
	//   - "none": No filtering (include all history)
	//   - "buffer_window": Keep last N messages (simple, fast)
	//   - "token_window": Keep messages within token budget (accurate)
	//   - "summary_buffer": Summarize old messages when exceeding budget
	// Default: "none" (for backwards compatibility)
	Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty" jsonschema:"title=Strategy,description=Context window management strategy,enum=none,enum=buffer_window,enum=token_window,enum=summary_buffer,default=none"`

	// WindowSize is the number of messages to keep for buffer_window strategy.
	// Only used when Strategy="buffer_window".
	// Default: 20
	WindowSize int `yaml:"window_size,omitempty" json:"window_size,omitempty" jsonschema:"title=Window Size,description=Number of messages to keep for buffer_window strategy,minimum=1,default=20"`

	// Budget is the token budget for token_window and summary_buffer strategies.
	// Only used when Strategy="token_window" or "summary_buffer".
	// Default: 8000
	Budget int `yaml:"budget,omitempty" json:"budget,omitempty" jsonschema:"title=Token Budget,description=Token budget for token_window and summary_buffer strategies,minimum=1,default=8000"`

	// Threshold is the percentage of budget that triggers summarization.
	// Only used when Strategy="summary_buffer".
	// When current tokens > budget * threshold, summarization occurs.
	// Default: 0.85 (85%)
	Threshold float64 `yaml:"threshold,omitempty" json:"threshold,omitempty" jsonschema:"title=Threshold,description=Percentage of budget that triggers summarization,minimum=0,maximum=1,default=0.85"`

	// Target is the percentage of budget to reduce to after summarization.
	// Only used when Strategy="summary_buffer".
	// Default: 0.7 (70%)
	Target float64 `yaml:"target,omitempty" json:"target,omitempty" jsonschema:"title=Target,description=Percentage of budget to reduce to after summarization,minimum=0,maximum=1,default=0.7"`

	// PreserveRecent is the minimum number of recent messages to always keep.
	// Only used when Strategy="token_window".
	// Default: 5
	PreserveRecent int `yaml:"preserve_recent,omitempty" json:"preserve_recent,omitempty" jsonschema:"title=Preserve Recent,description=Minimum number of recent messages to always keep,minimum=0,default=5"`

	// SummarizerLLM references an LLM from the global llms config to use for summarization.
	// Only used when Strategy="summary_buffer".
	// If empty, uses the same LLM as the agent.
	// Example: "gpt-4o-mini" (for cheaper summarization)
	SummarizerLLM string `yaml:"summarizer_llm,omitempty" json:"summarizer_llm,omitempty" jsonschema:"title=Summarizer LLM,description=LLM reference for summarization (uses agent LLM if empty)"`
}

// SetDefaults applies default values to ContextConfig.
func (c *ContextConfig) SetDefaults() {
	// Strategy defaults to "none" for backwards compatibility
	if c.Strategy == "" {
		c.Strategy = "none"
	}

	switch c.Strategy {
	case "buffer_window":
		if c.WindowSize <= 0 {
			c.WindowSize = 20
		}
	case "token_window":
		if c.Budget <= 0 {
			c.Budget = 8000
		}
		if c.PreserveRecent <= 0 {
			c.PreserveRecent = 5
		}
	case "summary_buffer":
		if c.Budget <= 0 {
			c.Budget = 8000
		}
		if c.Threshold <= 0 || c.Threshold > 1 {
			c.Threshold = 0.85
		}
		if c.Target <= 0 || c.Target > 1 {
			c.Target = 0.7
		}
	}
}

// Validate checks the context configuration.
func (c *ContextConfig) Validate() error {
	validStrategies := map[string]bool{
		"":               true,
		"none":           true,
		"buffer_window":  true,
		"token_window":   true,
		"summary_buffer": true,
	}

	if !validStrategies[c.Strategy] {
		return fmt.Errorf("invalid context strategy %q (valid: none, buffer_window, token_window, summary_buffer)", c.Strategy)
	}

	if c.WindowSize < 0 {
		return fmt.Errorf("window_size must be non-negative")
	}

	if c.Budget < 0 {
		return fmt.Errorf("budget must be non-negative")
	}

	if c.Threshold < 0 || c.Threshold > 1 {
		return fmt.Errorf("threshold must be between 0 and 1")
	}

	if c.Target < 0 || c.Target > 1 {
		return fmt.Errorf("target must be between 0 and 1")
	}

	if c.PreserveRecent < 0 {
		return fmt.Errorf("preserve_recent must be non-negative")
	}

	return nil
}

// StructuredOutputConfig configures JSON schema response format.
// This enables the LLM to return structured data matching a specific schema.
//
// Ported from legacy pkg/config/types.go for v2 compatibility.
//
// Provider Support:
//   - OpenAI: Uses text.format.json_schema (strict mode)
//   - Gemini: Uses ResponseMIMEType + ResponseSchema
//   - Anthropic: Uses tool_use pattern for structured output
//   - Ollama: Uses format field with schema
type StructuredOutputConfig struct {
	// Schema is the JSON schema the response must conform to.
	// Uses standard JSON Schema format.
	//
	// Example:
	//   schema:
	//     type: object
	//     properties:
	//       name: { type: string }
	//       age: { type: integer }
	//     required: ["name"]
	Schema map[string]interface{} `yaml:"schema,omitempty" json:"schema,omitempty" jsonschema:"title=Schema,description=JSON schema the response must conform to"`

	// Strict enables strict schema validation.
	// When true, the LLM is constrained to only output valid schema conforming JSON.
	// Default: true
	Strict *bool `yaml:"strict,omitempty" json:"strict,omitempty" jsonschema:"title=Strict,description=Enable strict schema validation,default=true"`

	// Name is an optional name for the schema (used by some providers).
	// Default: "response"
	Name string `yaml:"name,omitempty" json:"name,omitempty" jsonschema:"title=Schema Name,description=Optional name for the schema,default=response"`
}

// SetDefaults applies default values to StructuredOutputConfig.
func (c *StructuredOutputConfig) SetDefaults() {
	if c.Strict == nil {
		c.Strict = BoolPtr(true)
	}
	if c.Name == "" {
		c.Name = "response"
	}
}

// Validate checks the structured output configuration.
func (c *StructuredOutputConfig) Validate() error {
	if c.Schema == nil {
		return fmt.Errorf("schema is required for structured output")
	}
	return nil
}

// IsStrict returns whether strict mode is enabled.
func (c *StructuredOutputConfig) IsStrict() bool {
	return c.Strict == nil || *c.Strict
}

// ReasoningConfig configures the chain-of-thought reasoning loop.
// This follows adk-go patterns for semantic loop termination rather than
// arbitrary iteration limits.
type ReasoningConfig struct {
	// MaxIterations is a SAFETY limit, not the primary termination condition.
	// The loop terminates when semantic conditions are met (no tool calls, etc.)
	// This is only a fallback to prevent runaway loops.
	// Default: 100 (high enough to not interfere with normal operation)
	MaxIterations int `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty" jsonschema:"title=Max Iterations,description=Safety limit for reasoning loop iterations,minimum=1,default=100"`

	// EnableExitTool adds the exit_loop tool for explicit termination.
	// When true, the agent can call exit_loop to signal task completion.
	EnableExitTool *bool `yaml:"enable_exit_tool,omitempty" json:"enable_exit_tool,omitempty" jsonschema:"title=Enable Exit Tool,description=Add exit_loop tool for explicit termination,default=false"`

	// EnableEscalateTool adds the escalate tool for parent delegation.
	// When true, the agent can escalate to a higher-level agent.
	EnableEscalateTool *bool `yaml:"enable_escalate_tool,omitempty" json:"enable_escalate_tool,omitempty" jsonschema:"title=Enable Escalate Tool,description=Add escalate tool for parent delegation,default=false"`

	// TerminationConditions lists which conditions terminate the loop.
	// Built-in conditions:
	//   - "no_tool_calls"      - model doesn't request tools (default)
	//   - "escalate"           - agent escalates to parent
	//   - "transfer"           - agent transfers to another agent
	//   - "skip_summarization" - explicit completion signal
	//   - "input_required"     - HITL pause
	// Custom conditions can be added programmatically.
	// Default: all built-in conditions enabled
	TerminationConditions []string `yaml:"termination_conditions,omitempty" json:"termination_conditions,omitempty" jsonschema:"title=Termination Conditions,description=Conditions that terminate the reasoning loop"`

	// CompletionInstruction is appended to help the model know when to stop.
	// If empty and EnableExitTool/EnableEscalateTool are set, a default
	// completion instruction is generated.
	CompletionInstruction string `yaml:"completion_instruction,omitempty" json:"completion_instruction,omitempty" jsonschema:"title=Completion Instruction,description=Instruction appended to help model know when to stop"`
}

// SetDefaults applies default values to ReasoningConfig.
func (c *ReasoningConfig) SetDefaults() {
	if c.MaxIterations == 0 {
		c.MaxIterations = 100 // Safety limit, not primary control
	}

	// Default termination conditions
	if len(c.TerminationConditions) == 0 {
		c.TerminationConditions = []string{
			"no_tool_calls",
			"escalate",
			"transfer",
			"skip_summarization",
			"input_required",
		}
	}

	// Default control tools to false if not set
	if c.EnableExitTool == nil {
		c.EnableExitTool = BoolPtr(false)
	}
	if c.EnableEscalateTool == nil {
		c.EnableEscalateTool = BoolPtr(false)
	}
}

// BuildCompletionInstruction generates instruction text based on config.
// Returns empty string if no control tools are enabled and no custom instruction.
func (c *ReasoningConfig) BuildCompletionInstruction() string {
	if c.CompletionInstruction != "" {
		return c.CompletionInstruction
	}

	var parts []string

	if BoolValue(c.EnableExitTool, false) {
		parts = append(parts, "- Call `exit_loop` when your task is complete and you have a final answer")
	}
	if BoolValue(c.EnableEscalateTool, false) {
		parts = append(parts, "- Call `escalate` if you need help, are stuck, or the task is outside your capabilities")
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Completion Guidelines\n" + joinStrings(parts, "\n")
}

// joinStrings joins strings with a separator.
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

// isWorkflowAgent returns true if the agent type is a workflow orchestrator
// that doesn't need its own LLM (sequential, parallel, loop, runner).
func isWorkflowAgent(agentType string) bool {
	switch agentType {
	case "sequential", "parallel", "loop", "runner":
		return true
	default:
		return false
	}
}

// SetDefaults applies default values.
func (c *AgentConfig) SetDefaults(defaults *DefaultsConfig) {
	// Apply global defaults
	if defaults != nil {
		if c.LLM == "" && defaults.LLM != "" {
			c.LLM = defaults.LLM
		}
	}

	// If still no LLM, use "default" (but only for agent types that need an LLM)
	// Workflow agents (sequential, parallel, loop) don't need an LLM
	if c.LLM == "" && !isWorkflowAgent(c.Type) {
		c.LLM = "default"
	}

	// Default description (A2A spec required)
	if c.Description == "" {
		if c.Name != "" {
			c.Description = "A helpful AI agent: " + c.Name
		} else {
			c.Description = "A helpful AI assistant"
		}
	}

	// Default input/output modes (A2A spec required)
	if len(c.InputModes) == 0 {
		c.InputModes = []string{"text/plain"}
	}
	if len(c.OutputModes) == 0 {
		c.OutputModes = []string{"text/plain"}
	}

	// Default visibility
	if c.Visibility == "" {
		c.Visibility = "public"
	}

	// Generate default skill if none provided (A2A spec required)
	if len(c.Skills) == 0 {
		c.Skills = []SkillConfig{{
			ID:          "default",
			Name:        c.GetDisplayName(),
			Description: c.Description,
			Tags:        []string{"general", "assistant"},
		}}
	}

	// Apply reasoning config defaults
	if c.Reasoning != nil {
		c.Reasoning.SetDefaults()
	}

	// Apply context config defaults
	if c.Context != nil {
		c.Context.SetDefaults()
	}

	// Apply structured output config defaults
	if c.StructuredOutput != nil {
		c.StructuredOutput.SetDefaults()
	}

	// Apply IncludeContext defaults (matches legacy PromptConfig.SetDefaults)
	if c.IncludeContext == nil {
		c.IncludeContext = BoolPtr(false)
	}
	if c.IncludeContextMaxLength == nil {
		c.IncludeContextMaxLength = IntPtr(500)
	}

	// Default streaming to true if not set (modern UX expectation)
	// Note: Zero-config mode sets this explicitly before SetDefaults is called
	if c.Streaming == nil {
		c.Streaming = BoolPtr(true)
	}
}

// Validate checks the agent configuration.
func (c *AgentConfig) Validate() error {
	// Validate structured output config
	if c.StructuredOutput != nil {
		if err := c.StructuredOutput.Validate(); err != nil {
			return fmt.Errorf("structured_output: %w", err)
		}
	}

	// Validate visibility
	switch c.Visibility {
	case "", "public", "internal", "private":
		// valid
	default:
		return fmt.Errorf("invalid visibility %q (must be public, internal, or private)", c.Visibility)
	}

	// Validate context config
	if c.Context != nil {
		if err := c.Context.Validate(); err != nil {
			return fmt.Errorf("context: %w", err)
		}
	}

	// LLM reference is validated at Config level
	return nil
}

// GetSystemPrompt returns the system prompt to use.
func (c *AgentConfig) GetSystemPrompt() string {
	if c.Prompt != nil && c.Prompt.SystemPrompt != "" {
		return c.Prompt.SystemPrompt
	}
	return c.Instruction
}

// GetDisplayName returns the name to display.
func (c *AgentConfig) GetDisplayName() string {
	if c.Name != "" {
		return c.Name
	}
	return "Assistant"
}
