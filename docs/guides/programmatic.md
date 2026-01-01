# Programmatic Usage

Hector provides a fluent, type-safe API in the `pkg/builder` package for constructing agents, tools, and workflows in Go. This allows you to build completely code-defined agent systems without any YAML configuration.

## Basic Workflow

The typical pattern involves four steps:
1.  Build an **LLM**
2.  Create **Tools** (optional)
3.  Build an **Agent**
4.  Build a **Runner** to execute the agent

```go
import (
    "github.com/a2aproject/a2a-go/a2a"
    "github.com/verikod/hector/pkg/builder"
    "github.com/verikod/hector/pkg/agent"
)
```

## 1. Building an LLM

Use `builder.NewLLM` to configuring an LLM provider.

```go
llm := builder.NewLLM("openai").
    Model("gpt-4o").
    APIKey(os.Getenv("OPENAI_API_KEY")).
    Temperature(0.7).
    MustBuild() // or Build() to handle error
```

## 2. Creating Tools

The `builder` package makes it easy to create safe, typed function tools.

### Function Tools
Define a struct for arguments, and Hector handles the JSON schema generation automatically.

```go
type WeatherArgs struct {
    City string `json:"city" jsonschema:"required,description=The city to check"`
}

weatherTool := builder.MustFunctionTool(
    "get_weather",
    "Get current weather for a city",
    func(ctx tool.Context, args WeatherArgs) (map[string]any, error) {
        return map[string]any{
            "city": args.City, 
            "temp": 22, 
            "cond": "Sunny",
        }, nil
    },
)
```

## 3. Building an Agent

Use `builder.NewAgent` to assemble your agent with the LLM and tools.

```go
// Configure reasoning loop (optional)
reasoning := builder.NewReasoning().
    MaxIterations(10).
    Build()

// Build the agent
myAgent, err := builder.NewAgent("assistant").
    WithName("Helpful Assistant").
    WithDescription("A general purpose assistant").
    WithLLM(llm).
    WithInstruction("You are a helpful AI assistant.").
    WithTools(weatherTool).
    WithReasoning(reasoning).
    EnableStreaming(true).
    Build()
```

## 4. Running the Agent (Sessions)

The `Runner` manages conversation sessions and state.

```go
// Create a runner
r, _ := builder.NewRunner("my-app").
    WithAgent(myAgent).
    Build()

// Create input content
content := &agent.Content{
    Role:  "user",
    Parts: []a2a.Part{a2a.TextPart{Text: "What is the weather in Berlin?"}},
}

// Run (returns an iterator of events)
for event, err := range r.Run(ctx, "user-1", "session-1", content, agent.RunConfig{}) {
    if event.Message != nil {
        // Handle streaming response...
    }
}
```

## Multi-Agent Patterns

### Transfer (Sub-Agents)
The main agent can transfer control effectively to specialized sub-agents.

```go
researcher, _ := builder.NewAgent("researcher").WithLLM(llm).Build()
writer, _ := builder.NewAgent("writer").WithLLM(llm).Build()

// Parent agent with sub-agents
coordinator, _ := builder.NewAgent("coordinator").
    WithLLM(llm).
    WithSubAgents(researcher, writer). // Automatically creates transfer tools
    Build()
```

### Delegation (Agents as Tools)
The main agent calls another agent as if it were a function, getting the result back.

```go
import "github.com/verikod/hector/pkg"

// ... build researcher agent ...

manager, _ := builder.NewAgent("manager").
    WithLLM(llm).
    WithTool(pkg.AgentAsTool(researcher)). // Wrap agent as a tool
    Build()
```

## RAG Support

You can also build RAG components programmatically.

```go
// 1. Embedder
emb := builder.NewEmbedder("openai").MustBuild()

// 2. Vector DB
vec := builder.NewVectorProvider("chromem").PersistPath("./data").MustBuild()

// 3. Document Store
store, _ := builder.NewDocumentStore("knowledge_base").
    FromDirectory("./docs").
    WithEmbedder(emb).
    WithVectorProvider(vec).
    Build()

store.Index(ctx)
```
