# FunctionTool - Type-Safe Tool Creation for Hector v2

FunctionTool provides a convenient way to create tools from typed Go functions, following ADK-Go patterns. It reduces boilerplate, provides compile-time type safety, and automatically generates JSON schemas from struct tags.

## Overview

**FunctionTool is syntactic sugar over CallableTool** - it generates a CallableTool implementation from a typed function, similar to:
- **Go (explicit)**: `functiontool.New(cfg, func)`
- **Python (implicit)**: `tools=[func]` (auto-wrapped)
- **Java (explicit)**: `FunctionTool.create(class, method)`

## When to Use FunctionTool

✅ **Use FunctionTool for:**
- Simple, stateless tools
- ≤ 5 parameters
- No streaming output
- Static schema
- Straightforward error handling

❌ **Use Direct CallableTool for:**
- Streaming tools (CallStreaming)
- Complex internal state
- Dynamic schemas
- HITL workflows
- Complex error patterns

## Basic Usage

```go
package main

import (
    "github.com/verikod/hector/pkg/tool"
    "github.com/verikod/hector/pkg/tool/functiontool"
)

// Define args struct with jsonschema tags
type GetWeatherArgs struct {
    City  string `json:"city" jsonschema:"required,description=City name"`
    Units string `json:"units,omitempty" jsonschema:"description=Temperature units,default=celsius,enum=celsius|fahrenheit"`
}

func main() {
    // Create tool from function
    weatherTool, err := functiontool.New(
        functiontool.Config{
            Name:        "get_weather",
            Description: "Get current weather for a city",
        },
        func(ctx tool.Context, args GetWeatherArgs) (map[string]any, error) {
            // Implementation
            return map[string]any{
                "city":      args.City,
                "temp":      22,
                "condition": "sunny",
                "units":     args.Units,
            }, nil
        },
    )
    
    // Use in agent
    agent, _ := llmagent.New(llmagent.Config{
        Tools: []tool.Tool{weatherTool},
    })
}
```

## Struct Tag Reference

### JSON Tags
- `json:"name"` - Parameter name (required)
- `json:",omitempty"` - Optional parameter (not required)

### JSONSchema Tags
- `jsonschema:"required"` - Mark as required explicitly
- `jsonschema:"description=..."` - Parameter description for LLM
- `jsonschema:"default=..."` - Default value
- `jsonschema:"enum=val1|val2"` - Allowed values
- `jsonschema:"minimum=N,maximum=M"` - Numeric constraints
- `jsonschema:"minLength=N,maxLength=M"` - String length constraints

## Examples

### Simple Parameters

```go
type CalculateArgs struct {
    A int `json:"a" jsonschema:"required,description=First number"`
    B int `json:"b" jsonschema:"required,description=Second number"`
}

calcTool, _ := functiontool.New(
    functiontool.Config{
        Name:        "calculate",
        Description: "Add two numbers",
    },
    func(ctx tool.Context, args CalculateArgs) (map[string]any, error) {
        return map[string]any{"result": args.A + args.B}, nil
    },
)
```

### Complex Parameters

```go
type SearchArgs struct {
    Query     string   `json:"query" jsonschema:"required,description=Search query"`
    Languages []string `json:"languages,omitempty" jsonschema:"description=Language filters"`
    MaxCount  int      `json:"max_count,omitempty" jsonschema:"description=Max results,default=10,minimum=1,maximum=100"`
    Type      string   `json:"type,omitempty" jsonschema:"description=Search type,enum=semantic|keyword"`
}

searchTool, _ := functiontool.New(
    functiontool.Config{
        Name:        "search",
        Description: "Search documents with filters",
    },
    func(ctx tool.Context, args SearchArgs) (map[string]any, error) {
        // Implementation with array and enum parameters
        return map[string]any{
            "results": []string{"doc1", "doc2"},
            "count":   2,
        }, nil
    },
)
```

### Custom Validation

```go
type FileArgs struct {
    Path    string `json:"path" jsonschema:"required,description=File path"`
    Content string `json:"content" jsonschema:"required,description=File content"`
}

fileTool, _ := functiontool.NewWithValidation(
    functiontool.Config{
        Name:        "create_file",
        Description: "Create a new file",
    },
    func(ctx tool.Context, args FileArgs) (map[string]any, error) {
        // Implementation
        return map[string]any{"path": args.Path}, nil
    },
    func(args FileArgs) error {
        // Custom validation beyond what tags can express
        if strings.Contains(args.Path, "..") {
            return fmt.Errorf("path traversal not allowed")
        }
        if len(args.Content) > 1000000 {
            return fmt.Errorf("content too large")
        }
        return nil
    },
)
```

## Architecture

FunctionTool generates a CallableTool implementation:

```
┌─────────────────────────┐
│   tool.Tool Interface   │
└───────────┬─────────────┘
            │
┌───────────▼─────────────┐
│ tool.CallableTool       │
│  - Call()               │
│  - Schema()             │
└───────────┬─────────────┘
            │
┌───────────▼─────────────┐
│ functionTool[Args]      │  ← Generated by functiontool.New()
│  - Type-safe args       │
│  - Auto schema          │
│  - Map → Struct         │
└─────────────────────────┘
```

## Type Safety

FunctionTool provides compile-time type safety through generics:

```go
// Define typed args
type MyArgs struct {
    Value string `json:"value"`
}

// Function receives typed struct (not map[string]any)
func myFunction(ctx tool.Context, args MyArgs) (map[string]any, error) {
    // args.Value is string (compile-time checked)
    return map[string]any{"result": args.Value}, nil
}

// Type parameter inferred from function signature
tool, _ := functiontool.New(cfg, myFunction)
```

## Schema Generation

Schemas are automatically generated using the `invopop/jsonschema` library:

```go
type Args struct {
    Name string `json:"name" jsonschema:"required,description=User name"`
    Age  int    `json:"age,omitempty" jsonschema:"minimum=0,maximum=150"`
}

// Auto-generates:
// {
//   "type": "object",
//   "properties": {
//     "name": {"type": "string", "description": "User name"},
//     "age": {"type": "integer", "minimum": 0, "maximum": 150}
//   },
//   "required": ["name"]
// }
```

## Error Handling

Errors are propagated from your function:

```go
func risky(ctx tool.Context, args MyArgs) (map[string]any, error) {
    if args.Value == "" {
        return nil, fmt.Errorf("value is required")  // LLM sees this error
    }
    return map[string]any{"ok": true}, nil
}
```

## Comparison with Direct CallableTool

**FunctionTool (120 lines → 40 lines):**
```go
type ReadFileArgs struct {
    Path string `json:"path" jsonschema:"required,description=File path"`
}

readTool, _ := functiontool.New(
    functiontool.Config{Name: "read_file", Description: "Read file"},
    func(ctx tool.Context, args ReadFileArgs) (map[string]any, error) {
        content, _ := os.ReadFile(args.Path)
        return map[string]any{"content": string(content)}, nil
    },
)
```

**Direct CallableTool (full control):**
```go
type ReadFileTool struct {
    config Config
}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read file" }
func (t *ReadFileTool) IsLongRunning() bool { return false }
func (t *ReadFileTool) Schema() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "path": map[string]any{"type": "string", "description": "File path"},
        },
        "required": []string{"path"},
    }
}
func (t *ReadFileTool) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
    path := args["path"].(string)
    content, _ := os.ReadFile(path)
    return map[string]any{"content": string(content)}, nil
}
```

## Best Practices

1. **Keep it simple**: Use FunctionTool for tools with < 5 parameters
2. **Use descriptive tags**: The LLM reads these descriptions
3. **Validate early**: Use custom validation for complex constraints
4. **Return structured data**: Use map[string]any with clear keys
5. **Document enums**: List allowed values in description + enum tag
6. **Test with real LLM**: Verify the LLM understands your tool

## Testing

```go
func TestMyTool(t *testing.T) {
    tool, err := functiontool.New(cfg, myFunc)
    if err != nil {
        t.Fatal(err)
    }
    
    // Test schema
    schema := tool.(tool.CallableTool).Schema()
    // Assert schema properties...
    
    // Test execution
    result, err := tool.(tool.CallableTool).Call(mockCtx, args)
    // Assert results...
}
```

## ADK-Go Alignment

This implementation follows ADK-Go patterns:

| Language | Pattern | Example |
|----------|---------|---------|
| Python | Implicit wrapping | `tools=[my_function]` |
| Go | Explicit wrapping | `functiontool.New(cfg, myFunc)` |
| Java | Explicit wrapping | `FunctionTool.create(class, "method")` |

All use struct tags / annotations for schema generation.

## See Also

- [tool.CallableTool](../tool.go) - Base interface
- [tool.StreamingTool](../tool.go) - For streaming output
- [agenttool](../agenttool/) - Agent-as-tool delegation
- [commandtool](../commandtool/) - Command execution tool
