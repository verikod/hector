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

// Package functiontool provides a convenient way to create tools from typed Go functions.
// This follows the ADK-Go pattern for FunctionTool, providing compile-time type safety
// and automatic schema generation from struct tags.
//
// FunctionTool is syntactic sugar over the CallableTool interface - it generates
// a CallableTool implementation from a typed function, reducing boilerplate and
// improving type safety.
//
// # Basic Usage
//
//	type GetWeatherArgs struct {
//	    City  string `json:"city" jsonschema:"required,description=City name"`
//	    Units string `json:"units,omitempty" jsonschema:"description=Temperature units,default=celsius,enum=celsius|fahrenheit"`
//	}
//
//	weatherTool, err := functiontool.New(
//	    functiontool.Config{
//	        Name:        "get_weather",
//	        Description: "Get current weather for a city",
//	    },
//	    func(ctx tool.Context, args GetWeatherArgs) (map[string]any, error) {
//	        // Implementation
//	        return map[string]any{"temp": 22, "condition": "sunny"}, nil
//	    },
//	)
//
// # When to Use FunctionTool
//
// Use FunctionTool for simple, stateless tools with:
//   - ≤ 5 parameters
//   - No internal state
//   - No streaming output
//   - Static schema
//   - Straightforward error handling
//
// For complex tools (streaming, dynamic schema, stateful), implement CallableTool directly.
//
// # ADK-Go Alignment
//
// This implementation follows ADK-Go patterns:
//   - Go (explicit): functiontool.New(cfg, func)
//   - Python (implicit): tools=[func] (auto-wrapped)
//   - Java (explicit): FunctionTool.create(class, method)
//
// Go uses struct tags for schema generation, similar to Java's @Schema annotation.
package functiontool

import (
	"fmt"

	"github.com/verikod/hector/pkg/tool"
)

// Config defines the configuration for a function tool.
type Config struct {
	// Name is the unique identifier for this tool (required).
	Name string

	// Description explains what the tool does (required).
	// This is shown to the LLM to help it decide when to use the tool.
	Description string
}

// New creates a CallableTool from a typed function.
// This is the primary way to create function tools in Hector v2.
//
// The function signature must be:
//
//	func(tool.Context, Args) (map[string]any, error)
//
// Where Args is a struct with json and jsonschema tags defining the parameters.
//
// Example:
//
//	type SearchArgs struct {
//	    Query string `json:"query" jsonschema:"required,description=Search query"`
//	    Limit int    `json:"limit,omitempty" jsonschema:"description=Max results,default=10"`
//	}
//
//	searchTool, err := functiontool.New(
//	    functiontool.Config{Name: "search", Description: "Search documents"},
//	    func(ctx tool.Context, args SearchArgs) (map[string]any, error) {
//	        // Implementation
//	    },
//	)
func New[Args any](cfg Config, fn func(tool.Context, Args) (map[string]any, error)) (tool.CallableTool, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	// Generate schema from Args type
	schema, err := generateSchema[Args]()
	if err != nil {
		return nil, fmt.Errorf("failed to generate schema for %s: %w", cfg.Name, err)
	}

	return &functionTool[Args]{
		config: cfg,
		fn:     fn,
		schema: schema,
	}, nil
}

// NewWithValidation creates a CallableTool with custom argument validation.
// The validation function is called before the main function, allowing you to
// implement complex validation logic beyond what struct tags can express.
//
// Example:
//
//	functiontool.NewWithValidation(
//	    cfg,
//	    myFunction,
//	    func(args MyArgs) error {
//	        if strings.Contains(args.Path, "..") {
//	            return fmt.Errorf("path traversal not allowed")
//	        }
//	        return nil
//	    },
//	)
func NewWithValidation[Args any](
	cfg Config,
	fn func(tool.Context, Args) (map[string]any, error),
	validate func(Args) error,
) (tool.CallableTool, error) {
	baseTool, err := New(cfg, fn)
	if err != nil {
		return nil, err
	}

	return &functionToolWithValidation[Args]{
		functionTool: baseTool.(*functionTool[Args]),
		validate:     validate,
	}, nil
}

// functionTool implements tool.CallableTool by wrapping a typed function.
type functionTool[Args any] struct {
	config Config
	fn     func(tool.Context, Args) (map[string]any, error)
	schema map[string]any
}

// Name returns the tool name.
func (t *functionTool[Args]) Name() string {
	return t.config.Name
}

// Description returns the tool description.
func (t *functionTool[Args]) Description() string {
	return t.config.Description
}

// IsLongRunning returns false (function tools are synchronous).
func (t *functionTool[Args]) IsLongRunning() bool {
	return false
}

// RequiresApproval returns false (function tools don't need approval by default).
// For HITL tools, implement CallableTool directly or wrap with approval.
func (t *functionTool[Args]) RequiresApproval() bool {
	return false
}

// Schema returns the JSON schema for tool parameters.
func (t *functionTool[Args]) Schema() map[string]any {
	return t.schema
}

// Call executes the function with typed arguments.
func (t *functionTool[Args]) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	// Convert map to typed struct
	var typedArgs Args
	if err := mapToStruct(args, &typedArgs); err != nil {
		return nil, fmt.Errorf("invalid arguments for %s: %w", t.config.Name, err)
	}

	// Call function with typed args
	result, err := t.fn(ctx, typedArgs)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// functionToolWithValidation wraps a function tool with custom validation.
type functionToolWithValidation[Args any] struct {
	*functionTool[Args]
	validate func(Args) error
}

// Call executes validation before calling the function.
func (t *functionToolWithValidation[Args]) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	// Convert map to typed struct
	var typedArgs Args
	if err := mapToStruct(args, &typedArgs); err != nil {
		return nil, fmt.Errorf("invalid arguments for %s: %w", t.config.Name, err)
	}

	// Run custom validation
	if err := t.validate(typedArgs); err != nil {
		return nil, fmt.Errorf("validation failed for %s: %w", t.config.Name, err)
	}

	// Call function with validated args
	result, err := t.fn(ctx, typedArgs)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// validateConfig checks that the configuration is valid.
func validateConfig(cfg Config) error {
	if cfg.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if cfg.Description == "" {
		return fmt.Errorf("tool description is required")
	}
	return nil
}

// Verify interface compliance at compile time
var _ tool.CallableTool = (*functionTool[struct{}])(nil)
var _ tool.CallableTool = (*functionToolWithValidation[struct{}])(nil)
