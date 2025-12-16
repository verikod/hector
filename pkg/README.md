# Hector

A fresh, clean implementation of Hector built natively on the [A2A (Agent-to-Agent) protocol](https://github.com/a2aproject/a2a-go).

## Overview

Hector is a complete rewrite that follows the architectural patterns from Google's ADK-Go, with clean interfaces and native A2A protocol support.

### Key Features

- **Native A2A**: Uses `github.com/a2aproject/a2a-go` directly, no custom protobuf
- **Interface-First**: All core components are defined as interfaces for testability
- **Iterator Pattern**: Uses Go 1.23+ `iter.Seq2` for clean event streaming
- **Context Hierarchy**: Clear separation of read-only vs mutable context
- **Lazy Loading**: Toolsets connect to external services on first use

## Package Structure

```
v2/
├── agent/                    # Core agent interfaces and types
│   ├── agent.go             # Agent interface and base implementation
│   ├── context.go           # Context hierarchy (Invocation/Readonly/Callback)
│   ├── event.go             # Event types for agent communication
│   └── llmagent/            # LLM-based agent implementation
│       ├── llmagent.go      # LLM agent with tool calling support
│       └── context.go       # Tool execution context
├── model/                    # LLM interface
│   └── model.go             # LLM abstraction for different providers
├── runner/                   # Agent orchestration
│   └── runner.go            # Manages agent execution within sessions
├── server/                   # A2A protocol server
│   ├── executor.go          # a2asrv.AgentExecutor implementation
│   ├── events.go            # Event translation (Hector ↔ A2A)
│   └── parts.go             # Part conversion utilities
├── session/                  # Session management
│   └── session.go           # Session interface and in-memory impl
├── tool/                     # Tool interfaces
│   └── tool.go              # Tool, Toolset, and predicate patterns
└── examples/                 # Usage examples
    └── quickstart/          # Simple echo agent example
```

## Quick Start

```go
package main

import (
    "github.com/verikod/hector/pkg/agent"
    "github.com/verikod/hector/pkg/agent/llmagent"
    "github.com/verikod/hector/pkg/runner"
    "github.com/verikod/hector/pkg/server"
    "github.com/verikod/hector/pkg/session"
)

func main() {
    // Create an LLM agent
    myAgent, _ := llmagent.New(llmagent.Config{
        Name:        "assistant",
        Model:       myLLM, // Your model implementation
        Instruction: "You are a helpful assistant.",
        Tools:       []tool.Tool{myTool},
    })

    // Set up the runner
    runnerCfg := runner.Config{
        AppName:        "my-app",
        Agent:          myAgent,
        SessionService: session.InMemoryService(),
    }

    // Create A2A server
    executor := server.NewExecutor(server.ExecutorConfig{
        RunnerConfig: runnerCfg,
    })

    // Serve via JSON-RPC
    handler := a2asrv.NewHandler(executor)
    http.Handle("/a2a", a2asrv.NewJSONRPCHandler(handler))
    http.ListenAndServe(":8080", nil)
}
```

## Architecture

### Context Hierarchy

```
InvocationContext (full access)
    └── CallbackContext (state modification)
            └── ReadonlyContext (safe for tools)
```

### Event Flow

```
User Message
    ↓
Runner.Run()
    ↓
Agent.Run() → yields Event(s)
    ↓
Session.AppendEvent()
    ↓
A2A Event Translation
    ↓
Client Response
```

## Migration from v1

The v2 package is designed to coexist with the legacy `pkg/` during migration:

1. **New features**: Implement in v2
2. **Existing features**: Gradually port to v2
3. **Legacy code**: Remains functional in `pkg/`

### Key Differences

| v1 (`pkg/`) | v2 |
|-------------|-----|
| Custom protobuf (`pb.Message`) | Native a2a-go types (`a2a.Message`) |
| Channel-based streaming | Iterator-based (`iter.Seq2`) |
| Monolithic Agent struct | Interface-based composition |
| Eager tool loading | Lazy toolset resolution |

## Contributing

When adding new features:

1. Define interfaces first
2. Implement concrete types
3. Add tests alongside implementation
4. Follow existing patterns from ADK-Go

## License

AGPL-3.0 (non-commercial); commercial licensing available per LICENSE.md