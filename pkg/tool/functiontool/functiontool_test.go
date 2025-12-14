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

package functiontool_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kadirpekel/hector/pkg/agent"
	"github.com/kadirpekel/hector/pkg/tool"
	"github.com/kadirpekel/hector/pkg/tool/functiontool"
)

// mockContext implements tool.Context for testing
type mockContext struct{}

// tool.Context methods
func (m *mockContext) FunctionCallID() string { return "test-call-id" }
func (m *mockContext) Actions() *agent.EventActions {
	return &agent.EventActions{StateDelta: make(map[string]any)}
}
func (m *mockContext) SearchMemory(ctx context.Context, query string) (*agent.MemorySearchResponse, error) {
	return nil, nil
}

// agent.CallbackContext methods
func (m *mockContext) Artifacts() agent.Artifacts { return nil }
func (m *mockContext) State() agent.State         { return nil }

// agent.ReadonlyContext methods
func (m *mockContext) InvocationID() string               { return "test-invocation" }
func (m *mockContext) AgentName() string                  { return "test-agent" }
func (m *mockContext) UserContent() *agent.Content        { return nil }
func (m *mockContext) ReadonlyState() agent.ReadonlyState { return nil }
func (m *mockContext) UserID() string                     { return "test-user" }
func (m *mockContext) AppName() string                    { return "test-app" }
func (m *mockContext) SessionID() string                  { return "test-session" }
func (m *mockContext) Branch() string                     { return "" }

// context.Context methods
func (m *mockContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (m *mockContext) Done() <-chan struct{}       { return nil }
func (m *mockContext) Err() error                  { return nil }
func (m *mockContext) Value(key any) any           { return nil }
func (m *mockContext) Task() agent.CancellableTask { return nil }

// TestNew_SimpleArgs tests basic function tool creation
func TestNew_SimpleArgs(t *testing.T) {
	type SimpleArgs struct {
		Name string `json:"name" jsonschema:"required,description=User name"`
		Age  int    `json:"age,omitempty" jsonschema:"description=User age,minimum=0,maximum=150"`
	}

	greetTool, err := functiontool.New(
		functiontool.Config{
			Name:        "greet",
			Description: "Greet a user",
		},
		func(ctx tool.Context, args SimpleArgs) (map[string]any, error) {
			return map[string]any{
				"greeting": fmt.Sprintf("Hello, %s! Age: %d", args.Name, args.Age),
			}, nil
		},
	)

	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	// Verify tool.Tool interface
	if greetTool.Name() != "greet" {
		t.Errorf("Expected name 'greet', got %q", greetTool.Name())
	}
	if greetTool.Description() != "Greet a user" {
		t.Errorf("Expected description 'Greet a user', got %q", greetTool.Description())
	}
	if greetTool.IsLongRunning() {
		t.Error("Expected IsLongRunning=false")
	}

	// Verify schema generation
	schema := greetTool.Schema()
	if schema == nil {
		t.Fatal("Schema is nil")
	}

	// Check type
	if schema["type"] != "object" {
		t.Errorf("Expected type 'object', got %v", schema["type"])
	}

	// Check properties exist
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Properties not found or wrong type")
	}

	if _, ok := props["name"]; !ok {
		t.Error("Property 'name' not found in schema")
	}
	if _, ok := props["age"]; !ok {
		t.Error("Property 'age' not found in schema")
	}

	// Check required fields
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("Required field not found or wrong type")
	}

	foundName := false
	for _, r := range required {
		if r == "name" {
			foundName = true
		}
	}
	if !foundName {
		t.Error("'name' should be in required fields")
	}
}

// TestCall_ValidArgs tests calling the function with valid arguments
func TestCall_ValidArgs(t *testing.T) {
	type MathArgs struct {
		A int `json:"a" jsonschema:"required,description=First number"`
		B int `json:"b" jsonschema:"required,description=Second number"`
	}

	addTool, err := functiontool.New(
		functiontool.Config{
			Name:        "add",
			Description: "Add two numbers",
		},
		func(ctx tool.Context, args MathArgs) (map[string]any, error) {
			return map[string]any{
				"result": args.A + args.B,
			}, nil
		},
	)

	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	// Call with valid args
	result, err := addTool.Call(&mockContext{}, map[string]any{
		"a": 5,
		"b": 3,
	})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result["result"] != 8 {
		t.Errorf("Expected result 8, got %v", result["result"])
	}
}

// TestCall_InvalidArgs tests error handling for invalid arguments
func TestCall_InvalidArgs(t *testing.T) {
	type StrictArgs struct {
		Name string `json:"name" jsonschema:"required"`
	}

	strictTool, err := functiontool.New(
		functiontool.Config{
			Name:        "strict",
			Description: "Requires name",
		},
		func(ctx tool.Context, args StrictArgs) (map[string]any, error) {
			return map[string]any{"name": args.Name}, nil
		},
	)

	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	// Call with missing required field (should still work, but name will be empty)
	result, err := strictTool.Call(&mockContext{}, map[string]any{})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Go doesn't enforce required at runtime (that's LLM's job)
	if result["name"] != "" {
		t.Errorf("Expected empty name, got %v", result["name"])
	}
}

// TestNewWithValidation tests custom validation
func TestNewWithValidation(t *testing.T) {
	type PathArgs struct {
		Path string `json:"path" jsonschema:"required,description=File path"`
	}

	validateTool, err := functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "read_file",
			Description: "Read a file",
		},
		func(ctx tool.Context, args PathArgs) (map[string]any, error) {
			return map[string]any{"path": args.Path}, nil
		},
		func(args PathArgs) error {
			if strings.Contains(args.Path, "..") {
				return fmt.Errorf("path traversal not allowed")
			}
			return nil
		},
	)

	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	// Valid path
	result, err := validateTool.Call(&mockContext{}, map[string]any{
		"path": "/safe/path/file.txt",
	})
	if err != nil {
		t.Errorf("Valid path rejected: %v", err)
	}
	if result["path"] != "/safe/path/file.txt" {
		t.Errorf("Unexpected result: %v", result)
	}

	// Invalid path (path traversal)
	_, err = validateTool.Call(&mockContext{}, map[string]any{
		"path": "../../../etc/passwd",
	})
	if err == nil {
		t.Error("Expected validation error for path traversal")
	}
	if !strings.Contains(err.Error(), "path traversal not allowed") {
		t.Errorf("Expected path traversal error, got: %v", err)
	}
}

// TestNew_ComplexTypes tests schema generation for complex types
func TestNew_ComplexTypes(t *testing.T) {
	type ComplexArgs struct {
		Query     string   `json:"query" jsonschema:"required,description=Search query"`
		Languages []string `json:"languages,omitempty" jsonschema:"description=Language filters"`
		MaxCount  int      `json:"max_count,omitempty" jsonschema:"description=Max results,default=10,minimum=1,maximum=100"`
		Type      string   `json:"type,omitempty" jsonschema:"description=Search type,enum=semantic|keyword"`
	}

	complexTool, err := functiontool.New(
		functiontool.Config{
			Name:        "search",
			Description: "Search with filters",
		},
		func(ctx tool.Context, args ComplexArgs) (map[string]any, error) {
			return map[string]any{
				"query":     args.Query,
				"languages": args.Languages,
				"max_count": args.MaxCount,
				"type":      args.Type,
			}, nil
		},
	)

	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	schema := complexTool.Schema()
	props := schema["properties"].(map[string]any)

	// Check array type (languages)
	langProp := props["languages"].(map[string]any)
	if langProp["type"] != "array" {
		t.Errorf("Expected languages type 'array', got %v", langProp["type"])
	}

	// Check numeric constraints (max_count)
	maxCountProp := props["max_count"].(map[string]any)
	if maxCountProp["minimum"] != float64(1) {
		t.Errorf("Expected minimum 1, got %v", maxCountProp["minimum"])
	}
	if maxCountProp["maximum"] != float64(100) {
		t.Errorf("Expected maximum 100, got %v", maxCountProp["maximum"])
	}
}

// TestNew_InvalidConfig tests config validation
func TestNew_InvalidConfig(t *testing.T) {
	type DummyArgs struct {
		Value string `json:"value"`
	}

	// Missing name
	_, err := functiontool.New(
		functiontool.Config{
			Description: "No name",
		},
		func(ctx tool.Context, args DummyArgs) (map[string]any, error) {
			return nil, nil
		},
	)
	if err == nil {
		t.Error("Expected error for missing name")
	}

	// Missing description
	_, err = functiontool.New(
		functiontool.Config{
			Name: "no_description",
		},
		func(ctx tool.Context, args DummyArgs) (map[string]any, error) {
			return nil, nil
		},
	)
	if err == nil {
		t.Error("Expected error for missing description")
	}
}

// TestCall_FunctionError tests error propagation from function
func TestCall_FunctionError(t *testing.T) {
	type ErrorArgs struct {
		ShouldFail bool `json:"should_fail"`
	}

	errorTool, err := functiontool.New(
		functiontool.Config{
			Name:        "error_test",
			Description: "Tests error handling",
		},
		func(ctx tool.Context, args ErrorArgs) (map[string]any, error) {
			if args.ShouldFail {
				return nil, fmt.Errorf("intentional error")
			}
			return map[string]any{"success": true}, nil
		},
	)

	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	// Success case
	result, err := errorTool.Call(&mockContext{}, map[string]any{
		"should_fail": false,
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result["success"] != true {
		t.Error("Expected success")
	}

	// Error case
	_, err = errorTool.Call(&mockContext{}, map[string]any{
		"should_fail": true,
	})
	if err == nil {
		t.Error("Expected error from function")
	}
	if !strings.Contains(err.Error(), "intentional error") {
		t.Errorf("Expected 'intentional error', got: %v", err)
	}
}

// TestCall_TypeConversion tests type conversion in mapToStruct
func TestCall_TypeConversion(t *testing.T) {
	type NumericArgs struct {
		IntVal    int     `json:"int_val"`
		FloatVal  float64 `json:"float_val"`
		BoolVal   bool    `json:"bool_val"`
		StringVal string  `json:"string_val"`
	}

	numericTool, err := functiontool.New(
		functiontool.Config{
			Name:        "numeric",
			Description: "Tests type conversion",
		},
		func(ctx tool.Context, args NumericArgs) (map[string]any, error) {
			return map[string]any{
				"int":    args.IntVal,
				"float":  args.FloatVal,
				"bool":   args.BoolVal,
				"string": args.StringVal,
			}, nil
		},
	)

	if err != nil {
		t.Fatalf("Failed to create tool: %v", err)
	}

	// Test with numeric types (JSON unmarshaling converts numbers to float64)
	result, err := numericTool.Call(&mockContext{}, map[string]any{
		"int_val":    42.0, // JSON number
		"float_val":  3.14,
		"bool_val":   true,
		"string_val": "hello",
	})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result["int"] != 42 {
		t.Errorf("Expected int 42, got %v", result["int"])
	}
	if result["float"] != 3.14 {
		t.Errorf("Expected float 3.14, got %v", result["float"])
	}
	if result["bool"] != true {
		t.Errorf("Expected bool true, got %v", result["bool"])
	}
	if result["string"] != "hello" {
		t.Errorf("Expected string 'hello', got %v", result["string"])
	}
}
