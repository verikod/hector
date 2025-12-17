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

// Package config provides configuration loading and management for Hector v2.
//
// Hector is config-first: agents, tools, and LLMs are defined in YAML and
// the runtime builds them automatically.
//
// Example config:
//
//	version: "2"
//	name: my-assistant
//
//	llms:
//	  default:
//	    provider: anthropic
//	    model: claude-sonnet-4-20250514
//	    api_key: ${ANTHROPIC_API_KEY}
//
//	tools:
//	  weather:
//	    type: mcp
//	    url: ${MCP_URL}
//
//	agents:
//	  assistant:
//	    llm: default
//	    tools: [weather]
//	    instruction: You are a helpful assistant.
//
//	server:
//	  port: 8080
package config

import (
	"fmt"
	"strings"
)

// Config is the root configuration structure.
type Config struct {
	// Version of the config schema (e.g., "2").
	Version string `yaml:"version,omitempty" json:"version,omitempty" jsonschema:"title=Config Version,description=Configuration schema version,default=2"`

	// Name of this configuration (for logging/display).
	Name string `yaml:"name,omitempty" json:"name,omitempty" jsonschema:"title=Project Name,description=Name of this configuration"`

	// Description of this configuration.
	Description string `yaml:"description,omitempty" json:"description,omitempty" jsonschema:"title=Description,description=Human-readable description"`

	// Databases defines available database connections.
	// These can be referenced by other components (e.g., server.tasks).
	Databases map[string]*DatabaseConfig `yaml:"databases,omitempty" json:"databases,omitempty" jsonschema:"title=Databases,description=Database connection configurations"`

	// VectorStores defines available vector database providers.
	// These can be referenced by memory, RAG, and document stores.
	VectorStores map[string]*VectorStoreConfig `yaml:"vector_stores,omitempty" json:"vector_stores,omitempty" jsonschema:"title=Vector Stores,description=Vector database provider configurations"`

	// LLMs defines available LLM providers.
	LLMs map[string]*LLMConfig `yaml:"llms,omitempty" json:"llms,omitempty" jsonschema:"title=LLMs,description=LLM provider configurations"`

	// Embedders defines available embedding providers for semantic search.
	Embedders map[string]*EmbedderConfig `yaml:"embedders,omitempty" json:"embedders,omitempty" jsonschema:"title=Embedders,description=Embedding provider configurations"`

	// Tools defines available tools.
	Tools map[string]*ToolConfig `yaml:"tools,omitempty" json:"tools,omitempty" jsonschema:"title=Tools,description=Tool configurations"`

	// Agents defines available agents.
	Agents map[string]*AgentConfig `yaml:"agents,omitempty" json:"agents,omitempty" jsonschema:"title=Agents,description=Agent configurations"`

	// DocumentStores defines available document stores for RAG.
	DocumentStores map[string]*DocumentStoreConfig `yaml:"document_stores,omitempty" json:"document_stores,omitempty" jsonschema:"title=Document Stores,description=Document store configurations for RAG"`

	// Server configures the A2A server.
	// Note: YAML serialization is disabled ("-") because server config is infrastructure-level
	// and should be managed via CLI flags/environment variables, not the application config file.
	Server ServerConfig `yaml:"-" json:"server,omitempty" jsonschema:"title=Server Configuration,description=A2A server settings"`

	// Logger configures logging behavior.
	Logger *LoggerConfig `yaml:"logger,omitempty" json:"logger,omitempty" jsonschema:"title=Logger Configuration,description=Logging behavior settings"`

	// RateLimiting configures rate limiting.
	RateLimiting *RateLimitConfig `yaml:"rate_limiting,omitempty" json:"rate_limiting,omitempty" jsonschema:"title=Rate Limiting,description=Rate limiting configuration"`

	// Guardrails defines named guardrail configurations.
	// Agents reference these by name via their "guardrails" field.
	Guardrails map[string]*GuardrailsConfig `yaml:"guardrails,omitempty" json:"guardrails,omitempty" jsonschema:"title=Guardrails,description=Named guardrail configurations"`

	// Defaults provides default values for agents.
	Defaults *DefaultsConfig `yaml:"defaults,omitempty" json:"defaults,omitempty" jsonschema:"title=Defaults,description=Default values for agents"`
}

// DefaultsConfig provides default values for agent configurations.
type DefaultsConfig struct {
	// LLM is the default LLM reference for agents.
	LLM string `yaml:"llm,omitempty" json:"llm,omitempty" jsonschema:"title=Default LLM,description=Default LLM reference for agents"`
}

// SetDefaults applies default values to the config.
func (c *Config) SetDefaults() {
	// Initialize maps if nil
	if c.Databases == nil {
		c.Databases = make(map[string]*DatabaseConfig)
	}
	if c.VectorStores == nil {
		c.VectorStores = make(map[string]*VectorStoreConfig)
	}
	if c.LLMs == nil {
		c.LLMs = make(map[string]*LLMConfig)
	}
	if c.Embedders == nil {
		c.Embedders = make(map[string]*EmbedderConfig)
	}
	if c.Tools == nil {
		c.Tools = make(map[string]*ToolConfig)
	}
	if c.Agents == nil {
		c.Agents = make(map[string]*AgentConfig)
	}
	if c.DocumentStores == nil {
		c.DocumentStores = make(map[string]*DocumentStoreConfig)
	}

	// Create default LLM if none defined
	if len(c.LLMs) == 0 {
		c.LLMs["default"] = &LLMConfig{}
	}

	// Create default agent if none defined
	if len(c.Agents) == 0 {
		c.Agents["assistant"] = &AgentConfig{}
	}

	// Apply defaults to databases
	for name, db := range c.Databases {
		if db != nil {
			db.SetDefaults()
		} else {
			c.Databases[name] = &DatabaseConfig{}
			c.Databases[name].SetDefaults()
		}
	}

	// Apply defaults to vector stores
	for name, vs := range c.VectorStores {
		if vs != nil {
			vs.SetDefaults()
		} else {
			c.VectorStores[name] = &VectorStoreConfig{}
			c.VectorStores[name].SetDefaults()
		}
	}

	// Apply defaults to document stores
	for name, ds := range c.DocumentStores {
		if ds != nil {
			ds.SetDefaults()
		} else {
			c.DocumentStores[name] = &DocumentStoreConfig{}
			c.DocumentStores[name].SetDefaults()
		}
	}

	// Apply defaults to each component
	for name, llm := range c.LLMs {
		if llm != nil {
			llm.SetDefaults()
		} else {
			c.LLMs[name] = &LLMConfig{}
			c.LLMs[name].SetDefaults()
		}
	}

	for name, tool := range c.Tools {
		if tool != nil {
			tool.SetDefaults()
		} else {
			c.Tools[name] = &ToolConfig{}
			c.Tools[name].SetDefaults()
		}
	}

	for name, agent := range c.Agents {
		if agent != nil {
			agent.SetDefaults(c.Defaults)
		} else {
			c.Agents[name] = &AgentConfig{}
			c.Agents[name].SetDefaults(c.Defaults)
		}
	}

	c.Server.SetDefaults()

	// Apply defaults to rate limiting
	if c.RateLimiting != nil {
		c.RateLimiting.SetDefaults()
	}

	// Initialize guardrails map if nil
	if c.Guardrails == nil {
		c.Guardrails = make(map[string]*GuardrailsConfig)
	}

	// Apply defaults to each guardrails config
	for name, g := range c.Guardrails {
		if g != nil {
			g.SetDefaults()
		} else {
			c.Guardrails[name] = &GuardrailsConfig{}
			c.Guardrails[name].SetDefaults()
		}
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	var errs []string

	// Validate Databases
	for name, db := range c.Databases {
		if db == nil {
			continue
		}
		if err := db.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("database %q: %v", name, err))
		}
	}

	// Validate VectorStores
	for name, vs := range c.VectorStores {
		if vs == nil {
			continue
		}
		if err := vs.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("vector_store %q: %v", name, err))
		}
	}

	// Validate LLMs
	for name, llm := range c.LLMs {
		if llm == nil {
			continue
		}
		if err := llm.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("llm %q: %v", name, err))
		}
	}

	// Validate Tools
	for name, tool := range c.Tools {
		if tool == nil {
			continue
		}
		if err := tool.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("tool %q: %v", name, err))
		}
	}

	// Validate Agents
	for name, agent := range c.Agents {
		if agent == nil {
			continue
		}
		if err := agent.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("agent %q: %v", name, err))
		}
	}

	// Validate DocumentStores
	for name, ds := range c.DocumentStores {
		if ds == nil {
			continue
		}
		if err := ds.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("document_store %q: %v", name, err))
		}
	}

	// Validate Server
	if err := c.Server.Validate(); err != nil {
		errs = append(errs, fmt.Sprintf("server: %v", err))
	}

	// Validate Logger
	if c.Logger != nil {
		if err := c.Logger.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("logger: %v", err))
		}
	}

	// Validate RateLimiting
	if c.RateLimiting != nil {
		if err := c.RateLimiting.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("rate_limiting: %v", err))
		}
	}

	// Validate Guardrails
	for name, g := range c.Guardrails {
		if g == nil {
			continue
		}
		if err := g.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("guardrails %q: %v", name, err))
		}
	}

	// Validate references
	if err := c.validateReferences(); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// validateReferences checks that all references are valid.
func (c *Config) validateReferences() error {
	var errs []string

	for agentName, agent := range c.Agents {
		if agent == nil {
			continue
		}

		// Check LLM reference
		if agent.LLM != "" {
			if _, ok := c.LLMs[agent.LLM]; !ok {
				errs = append(errs, fmt.Sprintf("agent %q references undefined llm %q", agentName, agent.LLM))
			}
		}

		// Check tool references
		for _, toolName := range agent.Tools {
			if _, ok := c.Tools[toolName]; ok {
				// Tool is explicitly defined
				continue
			}

			// Check if provided by an MCP toolset
			found := false
			hasPromiscuousMCP := false

			for _, toolCfg := range c.Tools {
				if toolCfg == nil || toolCfg.Type != ToolTypeMCP {
					continue
				}

				if len(toolCfg.Filter) > 0 {
					// Check if allowed by filter
					for _, allowedTool := range toolCfg.Filter {
						if allowedTool == toolName {
							found = true
							break
						}
					}
				} else {
					// No filter = promiscuous MCP server (allows any tool)
					hasPromiscuousMCP = true
				}

				if found {
					break
				}
			}

			if !found && !hasPromiscuousMCP {
				errs = append(errs, fmt.Sprintf("agent %q references undefined tool %q", agentName, toolName))
			}
		}

		// Check sub-agent references
		for _, subAgentName := range agent.SubAgents {
			if _, ok := c.Agents[subAgentName]; !ok {
				errs = append(errs, fmt.Sprintf("agent %q references undefined sub-agent %q", agentName, subAgentName))
			}
		}

		// Check agent-tool references
		for _, toolAgentName := range agent.AgentTools {
			if _, ok := c.Agents[toolAgentName]; !ok {
				errs = append(errs, fmt.Sprintf("agent %q references undefined agent_tool %q", agentName, toolAgentName))
			}
		}

		// Check guardrails reference
		if agent.Guardrails != "" {
			if _, ok := c.Guardrails[agent.Guardrails]; !ok {
				errs = append(errs, fmt.Sprintf("agent %q references undefined guardrails %q", agentName, agent.Guardrails))
			}
		}

		// Check document store references
		if agent.DocumentStores != nil {
			for _, storeName := range *agent.DocumentStores {
				if _, ok := c.DocumentStores[storeName]; !ok {
					errs = append(errs, fmt.Sprintf("agent %q references undefined document_store %q", agentName, storeName))
				}
			}
		}
	}

	// Check document store references
	for storeName, store := range c.DocumentStores {
		if store == nil {
			continue
		}

		// Check vector_store reference
		if store.VectorStore != "" {
			if _, ok := c.VectorStores[store.VectorStore]; !ok {
				errs = append(errs, fmt.Sprintf("document_store %q references undefined vector_store %q", storeName, store.VectorStore))
			}
		}

		// Check embedder reference
		if store.Embedder != "" {
			if _, ok := c.Embedders[store.Embedder]; !ok {
				errs = append(errs, fmt.Sprintf("document_store %q references undefined embedder %q", storeName, store.Embedder))
			}
		}

		// Check SQL database reference
		if store.Source != nil && store.Source.SQL != nil && store.Source.SQL.Database != "" {
			if _, ok := c.Databases[store.Source.SQL.Database]; !ok {
				errs = append(errs, fmt.Sprintf("document_store %q references undefined database %q", storeName, store.Source.SQL.Database))
			}
		}

		// Check search LLM references
		if store.Search != nil {
			if store.Search.HyDELLM != "" {
				if _, ok := c.LLMs[store.Search.HyDELLM]; !ok {
					errs = append(errs, fmt.Sprintf("document_store %q references undefined llm %q for HyDE", storeName, store.Search.HyDELLM))
				}
			}
			if store.Search.RerankLLM != "" {
				if _, ok := c.LLMs[store.Search.RerankLLM]; !ok {
					errs = append(errs, fmt.Sprintf("document_store %q references undefined llm %q for reranking", storeName, store.Search.RerankLLM))
				}
			}
			if store.Search.MultiQueryLLM != "" {
				if _, ok := c.LLMs[store.Search.MultiQueryLLM]; !ok {
					errs = append(errs, fmt.Sprintf("document_store %q references undefined llm %q for multi-query", storeName, store.Search.MultiQueryLLM))
				}
			}
		}
	}

	// Check server.tasks database reference
	if c.Server.Tasks != nil && c.Server.Tasks.Database != "" {
		if _, ok := c.Databases[c.Server.Tasks.Database]; !ok {
			errs = append(errs, fmt.Sprintf("server.tasks references undefined database %q", c.Server.Tasks.Database))
		}
	}

	// Check server.sessions database reference
	if c.Server.Sessions != nil && c.Server.Sessions.Database != "" {
		if _, ok := c.Databases[c.Server.Sessions.Database]; !ok {
			errs = append(errs, fmt.Sprintf("server.sessions references undefined database %q", c.Server.Sessions.Database))
		}
	}

	// Check rate_limiting database reference
	if c.RateLimiting != nil && c.RateLimiting.Backend == "sql" && c.RateLimiting.SQLDatabase != "" {
		if _, ok := c.Databases[c.RateLimiting.SQLDatabase]; !ok {
			errs = append(errs, fmt.Sprintf("rate_limiting references undefined database %q", c.RateLimiting.SQLDatabase))
		}
	}

	// Check server.memory embedder reference
	if c.Server.Memory != nil && c.Server.Memory.Embedder != "" {
		if _, ok := c.Embedders[c.Server.Memory.Embedder]; !ok {
			errs = append(errs, fmt.Sprintf("server.memory references undefined embedder %q", c.Server.Memory.Embedder))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("reference errors:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// GetAgent returns the agent config by name.
func (c *Config) GetAgent(name string) (*AgentConfig, bool) {
	agent, ok := c.Agents[name]
	return agent, ok
}

// GetLLM returns the LLM config by name.
func (c *Config) GetLLM(name string) (*LLMConfig, bool) {
	llm, ok := c.LLMs[name]
	return llm, ok
}

// GetTool returns the tool config by name.
func (c *Config) GetTool(name string) (*ToolConfig, bool) {
	tool, ok := c.Tools[name]
	return tool, ok
}

// ListAgents returns the names of all configured agents.
func (c *Config) ListAgents() []string {
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	return names
}

// GetDatabase returns the database config by name.
func (c *Config) GetDatabase(name string) (*DatabaseConfig, bool) {
	db, ok := c.Databases[name]
	return db, ok
}

// GetGuardrails returns the guardrails config by name.
func (c *Config) GetGuardrails(name string) (*GuardrailsConfig, bool) {
	g, ok := c.Guardrails[name]
	return g, ok
}
