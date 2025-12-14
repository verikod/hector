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

// Package tool defines interfaces for tools that agents can invoke.
//
// Tools are capabilities that allow agents to perform specific actions,
// such as searching the web, executing code, or calling external APIs.
//
// # Tool Interface Hierarchy
//
// The tool system uses a layered interface design inspired by ADK-Go
// but enhanced with Hector's streaming capabilities:
//
//	Tool (base)
//	  ├── CallableTool       - Simple synchronous execution (ADK-Go compatible)
//	  ├── StreamingTool      - Real-time incremental output (Hector extension)
//	  ├── IsLongRunning()    - Async operations (returns job ID, polls for completion)
//	  └── RequiresApproval() - HITL pattern (human approval before execution)
//
// # Execution Patterns
//
// 1. **Simple Tool** (CallableTool):
//   - Executes synchronously
//   - Returns final result
//   - ADK-Go compatible
//
// 2. **Streaming Tool** (StreamingTool):
//   - Yields incremental chunks during execution
//   - Great for: command output, sub-agent responses, progress updates
//   - Maps to A2A `artifact-update` with `append: true`
//
// 3. **HITL Tool** (RequiresApproval = true):
//   - Pauses execution before running
//   - Task transitions to `input_required` state
//   - Human approves or denies, task resumes or stops
//   - Maps to A2A `status-update` with `state: input_required`
//
// 4. **Async Tool** (IsLongRunning = true):
//   - Returns immediately with job ID
//   - Polls for completion (not yet implemented)
//   - No human intervention needed
//
// # Creating Tools
//
// Use the provided constructors for different tool types:
//
//	// Simple function tool (ADK-Go style)
//	tool := functiontool.New(myFunc)
//
//	// Streaming command tool (Hector extension)
//	tool := commandtool.New(commandtool.Config{...})
//
//	// MCP toolset (lazy connection)
//	toolset := mcptoolset.New(mcpConfig)
package tool

import (
	"context"
	"iter"

	"github.com/kadirpekel/hector/pkg/agent"
)

// Tool defines the base interface for a callable tool.
// This matches ADK-Go's tool.Tool interface for compatibility.
type Tool interface {
	// Name returns the unique name of the tool.
	Name() string

	// Description returns a human-readable description of what the tool does.
	// Used by LLMs to decide when to use this tool.
	Description() string

	// IsLongRunning indicates whether this tool is a long-running async operation.
	// Long-running tools return a job ID and are polled for completion.
	// NOTE: For HITL (human approval), use RequiresApproval() instead.
	IsLongRunning() bool

	// RequiresApproval indicates whether this tool needs human approval before execution.
	// When true:
	// - Tool execution is paused before running
	// - Task transitions to `input_required` state
	// - Human must approve or deny the operation
	// - Tool executes only after approval
	//
	// This is semantically different from IsLongRunning():
	// - RequiresApproval: needs human decision, executes instantly once approved
	// - IsLongRunning: async operation, no human needed, polls for completion
	RequiresApproval() bool
}

// CallableTool extends Tool with synchronous execution capability.
// This is the ADK-Go compatible interface - simple and straightforward.
type CallableTool interface {
	Tool

	// Call executes the tool with the given arguments.
	// Returns the result as a map and any error that occurred.
	// This is a blocking call that waits for completion.
	Call(ctx Context, args map[string]any) (map[string]any, error)

	// Schema returns the JSON schema for the tool's parameters.
	// Returns nil if the tool takes no parameters.
	Schema() map[string]any
}

// CancellableTool extends Tool with explicit cancellation capability.
// Tools implementing this interface can be individually cancelled
// during execution while the agent continues processing other work.
//
// This is complementary to context cancellation:
//   - Context cancellation: propagates from HTTP request, cascades to all operations
//   - CancellableTool: targeted cancellation of specific tool execution by callID
//
// Use cases:
//   - Cancelling a long-running command without aborting the entire agent
//   - Stopping a sub-agent call mid-execution
//   - Interrupting MCP tool calls
type CancellableTool interface {
	Tool

	// Cancel requests graceful termination of the tool execution
	// identified by callID. Returns true if cancellation was initiated.
	// The tool should clean up resources and return promptly.
	Cancel(callID string) bool

	// SupportsCancellation returns true if this tool type supports cancellation.
	// Tools may return false if current state prevents cancellation.
	SupportsCancellation() bool
}

// StreamingTool extends Tool with incremental output capability.
// This is a Hector extension for tools that produce real-time output.
//
// Use StreamingTool for:
// - Command execution (docker pull, npm install, etc.)
// - Sub-agent calls that should stream responses
// - Any operation where incremental feedback improves UX
//
// The streaming output maps to A2A `artifact-update` events with `append: true`,
// allowing the UI to display progress in real-time.
type StreamingTool interface {
	Tool

	// CallStreaming executes the tool and yields incremental results.
	// Each yielded Result represents a chunk of output.
	//
	// The iterator pattern (iter.Seq2) aligns with Go 1.23+ and ADK-Go's
	// streaming patterns, providing consistent semantics across the codebase.
	//
	// Example implementation:
	//
	//	func (t *CommandTool) CallStreaming(ctx Context, args map[string]any) iter.Seq2[*Result, error] {
	//	    return func(yield func(*Result, error) bool) {
	//	        // Start command...
	//	        for line := range outputLines {
	//	            if !yield(&Result{Content: line, Streaming: true}, nil) {
	//	                return // Client disconnected
	//	            }
	//	        }
	//	        // Final result
	//	        yield(&Result{Content: finalOutput, Streaming: false}, nil)
	//	    }
	//	}
	CallStreaming(ctx Context, args map[string]any) iter.Seq2[*Result, error]

	// Schema returns the JSON schema for the tool's parameters.
	Schema() map[string]any
}

// Result represents the output of a tool execution.
// Used by both CallableTool (single result) and StreamingTool (multiple results).
type Result struct {
	// Content is the output content, typically a string or structured data.
	Content any

	// Streaming indicates this is an intermediate chunk, not the final result.
	// When true: UI should append to existing output
	// When false: This is the final result
	Streaming bool

	// Error is set if an error occurred during execution.
	// Can be set on intermediate chunks (partial failure) or final result.
	Error string

	// Metadata contains optional additional data about this result.
	Metadata map[string]any
}

// Context provides the execution context for a tool.
type Context interface {
	agent.CallbackContext

	// FunctionCallID returns the unique ID of this tool invocation.
	FunctionCallID() string

	// Actions returns the event actions to modify state or request transfers.
	Actions() *agent.EventActions

	// SearchMemory searches the agent's memory for relevant information.
	SearchMemory(ctx context.Context, query string) (*agent.MemorySearchResponse, error)

	// Task returns the parent task for cascade cancellation registration.
	// Returns nil if no task is associated with this context.
	// Uses agent.CancellableTask to avoid circular imports.
	Task() agent.CancellableTask
}

// Toolset groups related tools and provides dynamic resolution.
// Toolsets enable lazy loading - tools are resolved only when needed.
type Toolset interface {
	// Name returns the name of this toolset.
	Name() string

	// Tools returns the available tools based on the current context.
	// This allows dynamic tool selection based on user, session, or other factors.
	Tools(ctx agent.ReadonlyContext) ([]Tool, error)
}

// Predicate determines whether a tool should be available to the LLM.
// Used for filtering tools based on context, permissions, etc.
type Predicate func(ctx agent.ReadonlyContext, tool Tool) bool

// StringPredicate creates a Predicate that allows only named tools.
func StringPredicate(allowedTools []string) Predicate {
	allowed := make(map[string]bool, len(allowedTools))
	for _, name := range allowedTools {
		allowed[name] = true
	}

	return func(ctx agent.ReadonlyContext, tool Tool) bool {
		return allowed[tool.Name()]
	}
}

// AllowAll returns a Predicate that allows all tools.
func AllowAll() Predicate {
	return func(ctx agent.ReadonlyContext, tool Tool) bool {
		return true
	}
}

// DenyAll returns a Predicate that denies all tools.
func DenyAll() Predicate {
	return func(ctx agent.ReadonlyContext, tool Tool) bool {
		return false
	}
}

// Combine combines multiple predicates with AND logic.
func Combine(predicates ...Predicate) Predicate {
	return func(ctx agent.ReadonlyContext, tool Tool) bool {
		for _, p := range predicates {
			if !p(ctx, tool) {
				return false
			}
		}
		return true
	}
}

// Or combines multiple predicates with OR logic.
func Or(predicates ...Predicate) Predicate {
	return func(ctx agent.ReadonlyContext, tool Tool) bool {
		for _, p := range predicates {
			if p(ctx, tool) {
				return true
			}
		}
		return false
	}
}

// Not negates a predicate.
func Not(p Predicate) Predicate {
	return func(ctx agent.ReadonlyContext, tool Tool) bool {
		return !p(ctx, tool)
	}
}

// Definition represents a tool definition for LLM function calling.
type Definition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToDefinition converts a tool to a Definition.
func ToDefinition(t Tool) Definition {
	def := Definition{
		Name:        t.Name(),
		Description: t.Description(),
	}

	// Get schema if available
	if ct, ok := t.(CallableTool); ok {
		def.Parameters = ct.Schema()
	} else if st, ok := t.(StreamingTool); ok {
		def.Parameters = st.Schema()
	}

	return def
}

// ToolCall represents an LLM's request to invoke a tool.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResult represents the result of a tool invocation.
// Used for building the conversation history.
type ToolResult struct {
	ToolCallID string
	Content    string
	Error      string
	Metadata   map[string]any
}

// RequestProcessor is an optional interface that tools can implement
// to modify the LLM request before it's sent.
//
// This follows the adk-go pattern where tools can inject additional
// context, modify system instructions, or add tool-specific configuration.
//
// Example use cases:
// - RAG tools adding retrieved context to the request
// - Authentication tools adding credentials
// - Context-aware tools modifying instructions based on state
type RequestProcessor interface {
	// ProcessRequest modifies the LLM request before sending.
	// Called during the preprocessing phase of the reasoning loop.
	ProcessRequest(ctx Context, req *Request) error
}

// Request is a simplified view of the LLM request for tool preprocessing.
// Tools can modify these fields to influence the LLM call.
type Request struct {
	// SystemInstruction can be appended to by tools
	SystemInstruction string

	// Messages is the conversation history (read-only recommended)
	Messages any

	// Config contains LLM configuration
	Config any

	// Metadata for tool-specific data
	Metadata map[string]any
}
