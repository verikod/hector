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

// Example programmatic demonstrates the comprehensive programmatic API of Hector.
//
// This example shows how to build a complete agentic system without any YAML
// configuration, using only Go code. It demonstrates:
//
//   - Building LLMs with fluent API
//   - Creating custom function tools
//   - Building agents with reasoning
//   - Multi-agent patterns (sub-agents and agent tools)
//   - Running agents with sessions
//   - Setting up RAG with document stores
//
// Prerequisites:
//   - Set OPENAI_API_KEY environment variable
//
// Run:
//
//	go run ./pkg/examples/programmatic
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/builder"
	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/tool"
)

// WeatherArgs defines the arguments for the weather tool.
type WeatherArgs struct {
	City  string `json:"city" jsonschema:"required,description=The city to get weather for"`
	Units string `json:"units,omitempty" jsonschema:"description=Temperature units (celsius or fahrenheit),default=celsius"`
}

// CalculatorArgs defines the arguments for the calculator tool.
type CalculatorArgs struct {
	Expression string `json:"expression" jsonschema:"required,description=Mathematical expression to evaluate"`
}

func main() {
	// Load .env file if present
	config.LoadDotEnv()

	// Check for API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// =========================================================================
	// Step 1: Build LLM using fluent builder API
	// =========================================================================
	fmt.Println("🔧 Building LLM...")

	llm := builder.NewLLM("openai").
		Model("gpt-4o-mini").
		APIKey(apiKey).
		Temperature(0.7).
		MaxTokens(4000).
		MustBuild()

	defer llm.Close()
	fmt.Printf("✅ Built LLM: %s\n", llm.Name())

	// =========================================================================
	// Step 2: Create custom function tools
	// =========================================================================
	fmt.Println("\n🔧 Creating custom tools...")

	// Weather tool using typed function
	weatherTool, err := builder.FunctionTool(
		"get_weather",
		"Get current weather for a city. Returns temperature and conditions.",
		func(ctx tool.Context, args WeatherArgs) (map[string]any, error) {
			// Simulated weather data
			units := args.Units
			if units == "" {
				units = "celsius"
			}
			temp := 22.0
			if units == "fahrenheit" {
				temp = 72.0
			}
			return map[string]any{
				"city":        args.City,
				"temperature": temp,
				"units":       units,
				"conditions":  "sunny",
				"humidity":    65,
			}, nil
		},
	)
	if err != nil {
		log.Fatalf("Failed to create weather tool: %v", err)
	}

	// Calculator tool
	calculatorTool := builder.MustFunctionTool(
		"calculate",
		"Evaluate a mathematical expression. Supports +, -, *, /, and parentheses.",
		func(ctx tool.Context, args CalculatorArgs) (map[string]any, error) {
			// Simple calculator simulation
			return map[string]any{
				"expression": args.Expression,
				"result":     42, // Simplified - real implementation would evaluate
				"note":       "Expression evaluated successfully",
			}, nil
		},
	)

	fmt.Printf("✅ Created tools: %s, %s\n", weatherTool.Name(), calculatorTool.Name())

	// =========================================================================
	// Step 3: Build agent with tools and reasoning
	// =========================================================================
	fmt.Println("\n🔧 Building agent...")

	// Configure reasoning
	reasoning := builder.NewReasoning().
		MaxIterations(50).
		EnableExitTool(true).
		EnableEscalateTool(false).
		CompletionInstruction("When you have answered the user's question, call exit_loop.").
		Build()

	// Build the agent
	assistant, err := builder.NewAgent("assistant").
		WithName("Hector Assistant").
		WithDescription("A helpful AI assistant with weather and calculation capabilities").
		WithLLM(llm).
		WithInstruction(`You are a helpful assistant that can check weather and perform calculations.

When the user asks about weather, use the get_weather tool to get current conditions.
When the user asks for calculations, use the calculate tool.

Always be friendly and concise in your responses.`).
		WithTools(weatherTool, calculatorTool).
		WithReasoning(reasoning).
		EnableStreaming(true).
		Build()
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	fmt.Printf("✅ Built agent: %s\n", assistant.Name())

	// =========================================================================
	// Step 4: Build runner for session management
	// =========================================================================
	fmt.Println("\n🔧 Building runner...")

	r, err := builder.NewRunner("programmatic-example").
		WithAgent(assistant).
		Build()
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}

	fmt.Printf("✅ Built runner for app: %s\n", "programmatic-example")

	// =========================================================================
	// Step 5: Run the agent with a user query
	// =========================================================================
	fmt.Println("\n📝 Running agent with query...")

	ctx := context.Background()
	userMessage := "What's the weather like in Berlin today? Also, what's 15 * 7?"

	fmt.Printf("\n💬 User: %s\n", userMessage)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Create user content
	content := &agent.Content{
		Role:  a2a.MessageRoleUser,
		Parts: []a2a.Part{a2a.TextPart{Text: userMessage}},
	}

	// Run the agent
	var response string
	for event, err := range r.Run(ctx, "user1", "session1", content, agent.RunConfig{}) {
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		if event == nil {
			continue
		}

		// Handle different event types
		if event.Message != nil {
			for _, part := range event.Message.Parts {
				switch p := part.(type) {
				case a2a.TextPart:
					if event.Partial {
						// Streaming chunk
						fmt.Print(p.Text)
					} else {
						// Final message
						response = p.Text
					}
				}
			}
		}

		// Log state changes (indicates tool execution progress)
		if len(event.Actions.StateDelta) > 0 {
			fmt.Printf("\n📊 State updated\n")
		}

		// Log transfers
		if event.Actions.TransferToAgent != "" {
			fmt.Printf("\n🔄 Transferring to: %s\n", event.Actions.TransferToAgent)
		}
	}

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if response != "" {
		fmt.Printf("\n🤖 Final Response:\n%s\n", response)
	}

	// =========================================================================
	// Step 6: Demonstrate multi-agent pattern (commented for brevity)
	// =========================================================================
	fmt.Println("\n📚 Multi-Agent Example (code shown, not executed):")
	fmt.Print(`
// Create specialized agents
researcher, _ := builder.NewAgent("researcher").
    WithDescription("Researches topics in depth").
    WithLLM(llm).
    Build()

writer, _ := builder.NewAgent("writer").
    WithDescription("Writes content based on research").
    WithLLM(llm).
    Build()

// Pattern 1: Sub-agents (transfer pattern)
// Parent creates transfer tools automatically
coordinator, _ := builder.NewAgent("coordinator").
    WithDescription("Coordinates research and writing").
    WithLLM(llm).
    WithSubAgents(researcher, writer).
    Build()

// Pattern 2: Agent as tool (delegation pattern)
// Parent maintains control, gets results back
orchestrator, _ := builder.NewAgent("orchestrator").
    WithLLM(llm).
    WithTool(pkg.AgentAsTool(researcher)).
    WithTool(pkg.AgentAsTool(writer)).
    Build()
`)

	// =========================================================================
	// Step 7: Demonstrate RAG setup (commented for brevity)
	// =========================================================================
	fmt.Println("\n📚 RAG Setup Example (code shown, not executed):")
	fmt.Print(`
// Build embedder
emb := builder.NewEmbedder("openai").
    Model("text-embedding-3-small").
    MustBuild()

// Build vector store
vecStore := builder.NewVectorProvider("chromem").
    PersistPath(".hector/vectors").
    MustBuild()

// Build document store
docStore, _ := builder.NewDocumentStore("docs").
    FromDirectory("./documents").
    IncludePatterns("*.md", "*.txt").
    WithVectorProvider(vecStore).
    WithEmbedder(emb).
    ChunkSize(512).
    EnableWatching(true).
    Build()

// Index documents
docStore.Index(ctx)

// Search
results, _ := docStore.Search(ctx, rag.SearchRequest{
    Query: "How does authentication work?",
    TopK:  5,
})
`)

	fmt.Println("\n✅ Example completed successfully!")
}
