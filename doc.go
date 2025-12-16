// Package hector provides a pure A2A-native declarative AI agent platform.
//
// Hector allows you to build powerful AI agents using pure YAML configuration,
// without writing any code. It implements the A2A (Agent-to-Agent) protocol
// for interoperability and provides multi-agent orchestration capabilities.
//
// # Quick Start
//
// Install Hector:
//
//	go install github.com/verikod/hector/cmd/hector@latest
//
// Create a simple agent configuration:
//
//	yaml
//	agents:
//	  assistant:
//	    name: "My Assistant"
//	    llm: "gpt-4o"
//	    prompt:
//	      system_role: "You are a helpful assistant"
//
//	llms:
//	  gpt-4o:
//	    type: "openai"
//	    model: "gpt-4o-mini"
//	    api_key: "${OPENAI_API_KEY}"
//
// Start the server:
//
//	hector serve --config my-agent.yaml
//
// # Using as Go Library
//
// Import the main package for convenience:
//
//	import "github.com/verikod/hector/pkg"
//
// Or import specific packages:
//
//	import (
//	    "github.com/verikod/hector/pkg/agent"
//	    "github.com/verikod/hector/pkg/a2a/pb"
//	    "github.com/verikod/hector/pkg/config"
//	)
//
// # Key Features
//
//   - **Declarative YAML**: Define complete agents without code
//   - **A2A Protocol**: Industry-standard agent communication
//   - **Multi-Agent Orchestration**: LLM-driven delegation
//   - **External Agent Integration**: Connect to remote A2A agents
//   - **Built-in Tools**: Search, file ops, commands, todos
//   - **RAG Support**: Semantic search with document stores
//   - **Plugin System**: Extend with custom LLMs, databases, tools
//
// # Architecture
//
// Hector follows a pure A2A architecture:
//
//	User/Client → A2A Server → Agent Registry → Agents (Native/External)
//
// All communication uses the A2A protocol, ensuring interoperability
// with other A2A-compliant systems.
//
// # Alpha Status
//
// Hector is currently in alpha development. APIs may change, and some
// features are experimental. We welcome feedback and contributions!
//
// # Documentation
//
// For complete documentation, see:
//   - [README](https://github.com/verikod/hector/blob/main/README.md)
//   - [API Reference](https://godoc.org/github.com/verikod/hector)
//   - [Configuration Guide](https://github.com/verikod/hector/blob/main/docs/CONFIGURATION.md)
//
// # License
//
// AGPL-3.0 - See LICENSE.md for details.
package hector
