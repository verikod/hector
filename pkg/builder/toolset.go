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
	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/mcptoolset"
)

// MCPBuilder provides a fluent API for building MCP toolsets.
//
// MCP (Model Context Protocol) toolsets connect to external MCP servers
// to provide tools dynamically.
//
// Example:
//
//	toolset, err := builder.NewMCP("weather").
//	    URL("http://localhost:9000").
//	    Transport("sse").
//	    Build()
type MCPBuilder struct {
	name      string
	url       string
	command   string
	args      []string
	transport string
	filter    []string
	env       map[string]string
}

// NewMCP creates a new MCP toolset builder.
//
// Example:
//
//	// SSE transport
//	toolset, _ := builder.NewMCP("weather").
//	    URL("http://localhost:9000").
//	    Build()
//
//	// Stdio transport
//	toolset, _ := builder.NewMCP("filesystem").
//	    Command("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp").
//	    Build()
func NewMCP(name string) *MCPBuilder {
	if name == "" {
		panic("MCP toolset name cannot be empty")
	}
	return &MCPBuilder{
		name:      name,
		transport: "sse", // Default
	}
}

// URL sets the server URL for SSE or HTTP transports.
//
// Example:
//
//	builder.NewMCP("weather").URL("http://localhost:9000")
func (b *MCPBuilder) URL(url string) *MCPBuilder {
	b.url = url
	return b
}

// Command sets the command and arguments for stdio transport.
//
// Example:
//
//	builder.NewMCP("fs").Command("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp")
func (b *MCPBuilder) Command(cmd string, args ...string) *MCPBuilder {
	b.command = cmd
	b.args = args
	b.transport = "stdio"
	return b
}

// Transport sets the transport type: "sse", "stdio", or "streamable-http".
//
// Example:
//
//	builder.NewMCP("weather").Transport("streamable-http")
func (b *MCPBuilder) Transport(transport string) *MCPBuilder {
	b.transport = transport
	return b
}

// Filter limits which tools from the MCP server are exposed.
//
// Example:
//
//	builder.NewMCP("weather").Filter("get_weather", "get_forecast")
func (b *MCPBuilder) Filter(tools ...string) *MCPBuilder {
	b.filter = tools
	return b
}

// Env sets environment variables for stdio transport.
//
// Example:
//
//	builder.NewMCP("fs").Env(map[string]string{"DEBUG": "1"})
func (b *MCPBuilder) Env(env map[string]string) *MCPBuilder {
	b.env = env
	return b
}

// Build creates the MCP toolset.
//
// Returns an error if required parameters are missing.
func (b *MCPBuilder) Build() (*mcptoolset.Toolset, error) {
	cfg := mcptoolset.Config{
		Name:   b.name,
		Filter: b.filter,
	}

	switch b.transport {
	case "sse", "streamable-http":
		if b.url == "" {
			return nil, fmt.Errorf("URL is required for %s transport", b.transport)
		}
		cfg.URL = b.url
		cfg.Transport = b.transport

	case "stdio":
		if b.command == "" {
			return nil, fmt.Errorf("command is required for stdio transport")
		}
		cfg.Command = b.command
		cfg.Args = b.args
		cfg.Env = b.env
		cfg.Transport = "stdio"

	default:
		return nil, fmt.Errorf("unknown transport: %s (supported: sse, stdio, streamable-http)", b.transport)
	}

	return mcptoolset.New(cfg)
}

// MustBuild creates the MCP toolset or panics on error.
//
// Use this only when you're certain the configuration is valid.
func (b *MCPBuilder) MustBuild() *mcptoolset.Toolset {
	ts, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build MCP toolset: %v", err))
	}
	return ts
}

// MCPFromConfig creates an MCPBuilder from a config.ToolConfig.
// This allows the configuration system to use the builder as its foundation.
//
// Example:
//
//	cfg := &config.ToolConfig{Type: "mcp", URL: "http://localhost:9000", Transport: "sse"}
//	ts, err := builder.MCPFromConfig("weather", cfg).Build()
func MCPFromConfig(name string, cfg *config.ToolConfig) *MCPBuilder {
	if cfg == nil {
		return NewMCP(name)
	}

	b := NewMCP(name)
	b.url = cfg.URL
	b.command = cfg.Command
	b.args = cfg.Args
	b.env = cfg.Env
	b.filter = cfg.Filter

	if cfg.Transport != "" {
		b.transport = cfg.Transport
	}

	return b
}

// ToolsetBuilder wraps multiple tools into a toolset.
//
// Example:
//
//	toolset := builder.NewToolset("my-tools").
//	    WithTool(tool1).
//	    WithTool(tool2).
//	    Build()
type ToolsetBuilder struct {
	name  string
	tools []tool.Tool
}

// NewToolset creates a new toolset builder.
//
// Example:
//
//	toolset := builder.NewToolset("custom-tools").
//	    WithTool(myTool).
//	    Build()
func NewToolset(name string) *ToolsetBuilder {
	if name == "" {
		panic("toolset name cannot be empty")
	}
	return &ToolsetBuilder{
		name:  name,
		tools: make([]tool.Tool, 0),
	}
}

// WithTool adds a tool to the toolset.
//
// Example:
//
//	builder.NewToolset("tools").WithTool(myTool)
func (b *ToolsetBuilder) WithTool(t tool.Tool) *ToolsetBuilder {
	if t == nil {
		panic("tool cannot be nil")
	}
	b.tools = append(b.tools, t)
	return b
}

// WithTools adds multiple tools to the toolset.
//
// Example:
//
//	builder.NewToolset("tools").WithTools(tool1, tool2, tool3)
func (b *ToolsetBuilder) WithTools(tools ...tool.Tool) *ToolsetBuilder {
	for _, t := range tools {
		if t == nil {
			panic("tool cannot be nil")
		}
		b.tools = append(b.tools, t)
	}
	return b
}

// Build creates the toolset.
func (b *ToolsetBuilder) Build() tool.Toolset {
	return &staticToolset{
		name:  b.name,
		tools: b.tools,
	}
}

// staticToolset is a simple toolset that returns a fixed set of tools.
type staticToolset struct {
	name  string
	tools []tool.Tool
}

func (ts *staticToolset) Name() string {
	return ts.name
}

func (ts *staticToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	return ts.tools, nil
}

// Ensure staticToolset implements tool.Toolset.
var _ tool.Toolset = (*staticToolset)(nil)
