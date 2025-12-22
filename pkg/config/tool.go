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

// ToolType identifies the tool type.
type ToolType string

const (
	// ToolTypeMCP is an MCP (Model Context Protocol) tool.
	ToolTypeMCP ToolType = "mcp"

	// ToolTypeFunction is a built-in function tool.
	ToolTypeFunction ToolType = "function"

	// ToolTypeCommand is a built-in command execution tool.
	ToolTypeCommand ToolType = "command"
)

// ToolConfig configures a tool.
type ToolConfig struct {
	// Type of tool (mcp, function, command).
	Type ToolType `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"title=Tool Type,description=Type of tool,enum=mcp,enum=function,enum=command,default=mcp"`

	// Enabled controls whether the tool is active.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty" jsonschema:"title=Enabled,description=Whether the tool is active,default=true"`

	// Description of the tool.
	Description string `yaml:"description,omitempty" json:"description,omitempty" jsonschema:"title=Description,description=What this tool does"`

	// MCP-specific configuration
	// URL is the MCP server URL (for type: mcp).
	URL string `yaml:"url,omitempty" json:"url,omitempty" jsonschema:"title=MCP URL,description=MCP server URL (for type=mcp)"`

	// Transport specifies the MCP transport (stdio, sse, streamable-http).
	Transport string `yaml:"transport,omitempty" json:"transport,omitempty" jsonschema:"title=Transport,description=MCP transport type,enum=stdio,enum=sse,enum=streamable-http"`

	// Command for MCP stdio transport (not to be confused with CommandTool).
	Command string `yaml:"command,omitempty" json:"command,omitempty" jsonschema:"title=Command,description=Command to execute MCP server (for type=mcp stdio)"`

	// Args for MCP stdio transport.
	Args []string `yaml:"args,omitempty" json:"args,omitempty" jsonschema:"title=Args,description=Arguments for MCP stdio transport"`

	// Env for MCP stdio transport.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty" jsonschema:"title=Environment Variables,description=Environment variables for MCP stdio transport"`

	// Filter limits which tools are exposed from an MCP server.
	Filter []string `yaml:"filter,omitempty" json:"filter,omitempty" jsonschema:"title=Filter,description=Limit which tools are exposed from MCP server"`

	// Function-specific configuration
	// Handler is the function name (for type: function).
	Handler string `yaml:"handler,omitempty" json:"handler,omitempty" jsonschema:"title=Handler,description=Function name (for type=function)"`

	// Parameters schema (for type: function).
	Parameters map[string]any `yaml:"parameters,omitempty" json:"parameters,omitempty" jsonschema:"title=Parameters,description=Parameters schema (for type=function)"`

	// Command tool configuration (for type: command)
	// AllowedCommands is a whitelist of allowed base commands.
	AllowedCommands []string `yaml:"allowed_commands,omitempty" json:"allowed_commands,omitempty" jsonschema:"title=Allowed Commands,description=Whitelist of allowed base commands"`

	// DeniedCommands is a blacklist of denied base commands.
	DeniedCommands []string `yaml:"denied_commands,omitempty" json:"denied_commands,omitempty" jsonschema:"title=Denied Commands,description=Blacklist of denied base commands"`

	// WorkingDirectory for command execution.
	WorkingDirectory string `yaml:"working_directory,omitempty" json:"working_directory,omitempty" jsonschema:"title=Working Directory,description=Working directory for command execution"`

	// MaxExecutionTime limits command execution duration.
	MaxExecutionTime string `yaml:"max_execution_time,omitempty" json:"max_execution_time,omitempty" jsonschema:"title=Max Execution Time,description=Maximum command execution duration"`

	// DenyByDefault requires explicit allowed_commands whitelist.
	DenyByDefault *bool `yaml:"deny_by_default,omitempty" json:"deny_by_default,omitempty" jsonschema:"title=Deny By Default,description=Require explicit allowed_commands whitelist,default=false"`

	// HITL (Human-in-the-Loop) settings
	// RequireApproval requires user approval before execution.
	RequireApproval *bool `yaml:"require_approval,omitempty" json:"require_approval,omitempty" jsonschema:"title=Requires Approval (HITL),description=Whether this tool requires human approval,default=false"`

	// ApprovalPrompt is the message shown when requesting approval.
	ApprovalPrompt string `yaml:"approval_prompt,omitempty" json:"approval_prompt,omitempty" jsonschema:"title=Approval Prompt,description=Message shown when requesting approval"`
}

// SetDefaults applies default values.
func (c *ToolConfig) SetDefaults() {
	if c.Type == "" {
		c.Type = ToolTypeMCP
	}

	if c.Enabled == nil {
		c.Enabled = BoolPtr(true)
	}

	if c.Type == ToolTypeMCP && c.Transport == "" {
		// Auto-detect transport from URL
		if c.URL != "" {
			c.Transport = "sse" // Default for URL-based
		} else if c.Command != "" {
			c.Transport = "stdio"
		}
	}

	// Smart approval defaults based on tool type
	if c.RequireApproval == nil {
		switch c.Type {
		case ToolTypeCommand:
			// Command tools: require approval by default for safety
			c.RequireApproval = BoolPtr(true)
		case ToolTypeFunction:
			// Function tools: set approval based on handler name
			switch c.Handler {
			case "text_editor":
				// text_editor can do viewing and editing.
				// For safety, require approval by default for the whole tool,
				// or we could enhance this to only require approval for write ops (complex).
				// For now, treat as high risk if it can edit.
				c.RequireApproval = BoolPtr(true)
			case "apply_patch":
				// File modification tools: require approval (high risk)
				c.RequireApproval = BoolPtr(true)
			case "web_request":
				// External requests: require approval (high risk)
				c.RequireApproval = BoolPtr(true)
			case "web_fetch", "web_search", "grep_search", "todo_write":
				// Read-only or safe operations: no approval needed
				c.RequireApproval = BoolPtr(false)
			default:
				// Unknown function tools: default to requiring approval for safety
				c.RequireApproval = BoolPtr(true)
			}
		default:
			// Other tool types: no approval by default
			c.RequireApproval = BoolPtr(false)
		}
	}
}

// Validate checks the tool configuration.
func (c *ToolConfig) Validate() error {
	validTypes := []ToolType{ToolTypeMCP, ToolTypeFunction, ToolTypeCommand}
	isValid := false
	for _, t := range validTypes {
		if c.Type == t {
			isValid = true
			break
		}
	}
	if !isValid {
		return fmt.Errorf("invalid tool type %q (valid: mcp, function, command)", c.Type)
	}

	if c.Type == ToolTypeMCP {
		if c.URL == "" && c.Command == "" {
			return fmt.Errorf("mcp tool requires url or command")
		}
	}

	if c.Type == ToolTypeFunction {
		if c.Handler == "" {
			return fmt.Errorf("function tool requires handler")
		}
	}

	// Command tools validation is lenient - defaults are applied

	return nil
}

// IsEnabled returns whether the tool is enabled.
func (c *ToolConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// NeedsApproval returns whether the tool requires approval.
func (c *ToolConfig) NeedsApproval() bool {
	return c.RequireApproval != nil && *c.RequireApproval
}

// GetDefaultToolConfigs returns default local tool configurations.
// These are the built-in tools that can be enabled with --tools flag.
// Tools marked with RequireApproval=true use HITL (Human-in-the-Loop) pattern
// and require user approval before execution.
func GetDefaultToolConfigs() map[string]*ToolConfig {
	return map[string]*ToolConfig{
		// Command execution tool - smart defaults set in SetDefaults()
		"bash": {
			Type:             ToolTypeCommand,
			Enabled:          BoolPtr(true),
			Description:      "Execute shell commands with security restrictions. Use for running scripts, build tools, package managers, etc.",
			WorkingDirectory: "./",
			MaxExecutionTime: "30s",
			// Note: Approval defaults are set in SetDefaults() based on sandboxing
			// Users can override via --approve-tools or --no-approve-tools flags
		},

		// File operation tools
		"text_editor": {
			Type:        ToolTypeFunction,
			Handler:     "text_editor",
			Enabled:     BoolPtr(true),
			Description: "View and modify files. Supports view, create, str_replace, insert, and undo_edit commands. Use this tool for all file operations.",
			// Note: Approval defaults are set in SetDefaults() - modifications require approval
		},
		"apply_patch": {
			Type:        ToolTypeFunction,
			Handler:     "apply_patch",
			Enabled:     BoolPtr(true),
			Description: "Apply a patch to a file by finding and replacing text with surrounding context. More robust than search_replace for code edits. Validates context before applying changes.",
			// Note: Approval defaults are set in SetDefaults() - requires approval by default
		},
		"grep_search": {
			Type:        ToolTypeFunction,
			Handler:     "grep_search",
			Enabled:     BoolPtr(true),
			Description: "Search for patterns across files using regex. Use to find code references, function definitions, or text patterns.",
			// Safe operation - no approval needed
		},

		// Web and network tools
		"web_search": {
			Type:        ToolTypeFunction,
			Handler:     "web_search",
			Enabled:     BoolPtr(true),
			Description: "Search the internet for information. Returns relevant results with summaries. Use this to find up-to-date information, news, or answers to questions not in your training data.",
			// Safe read-only operation - no approval needed
		},
		"web_fetch": {
			Type:        ToolTypeFunction,
			Handler:     "web_fetch",
			Enabled:     BoolPtr(true),
			Description: "Fetch the content of a URL. Returns the Page Content. Use this to read documentation, news, or any public web page.",
			// Safe read-only operation - no approval needed
		},
		"web_request": {
			Type:        ToolTypeFunction,
			Handler:     "web_request",
			Enabled:     BoolPtr(true),
			Description: "Make HTTP requests to external APIs or services. Supports GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS methods.",
			// Note: Approval defaults are set in SetDefaults() - requires approval by default
		},

		// Task management tools
		"todo_write": {
			Type:        ToolTypeFunction,
			Handler:     "todo_write",
			Enabled:     BoolPtr(true),
			Description: "Create and manage a structured task list for tracking progress. Use for complex multi-step tasks (3+ steps) to demonstrate thoroughness. Always provide complete todo items (id, content, status) for all todos in the list.",
			// Safe operation - no approval needed
		},
	}
}
