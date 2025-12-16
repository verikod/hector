package llmagent_test

import (
	"testing"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/agent/llmagent"
	"github.com/verikod/hector/pkg/model/openai"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/agenttool"
)

// TestAgentTool_FactoryPattern tests the adk-go aligned pattern using agenttool.New().
// This is the recommended way to convert agents to tools.
func TestAgentTool_FactoryPattern(t *testing.T) {
	model, err := openai.New(openai.Config{APIKey: "test"})
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	// Create child agents - New() returns agent.Agent
	webSearchAgent, err := llmagent.New(llmagent.Config{
		Name:        "web_search",
		Description: "Searches the web",
		Model:       model,
	})
	if err != nil {
		t.Fatalf("Failed to create web_search agent: %v", err)
	}

	dataAnalysisAgent, err := llmagent.New(llmagent.Config{
		Name:        "data_analysis",
		Description: "Analyzes data",
		Model:       model,
	})
	if err != nil {
		t.Fatalf("Failed to create data_analysis agent: %v", err)
	}

	// ✅ adk-go aligned: Use agenttool.New() factory function
	searchTool := agenttool.New(webSearchAgent, nil)
	analysisTool := agenttool.New(dataAnalysisAgent, nil)

	// Verify the results are tool.Tool
	var _ tool.Tool = searchTool
	var _ tool.Tool = analysisTool

	// Create parent agent with child agents as tools
	researcher, err := llmagent.New(llmagent.Config{
		Name:        "researcher",
		Description: "Conducts comprehensive research",
		Model:       model,
		Tools: []tool.Tool{
			searchTool,
			analysisTool,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create researcher: %v", err)
	}

	// Verify agent.Agent interface
	var _ agent.Agent = researcher

	if researcher == nil {
		t.Fatal("Researcher is nil")
	}
}

// TestAgentTool_InlineUsage tests inline usage pattern (most common).
func TestAgentTool_InlineUsage(t *testing.T) {
	model, err := openai.New(openai.Config{APIKey: "test"})
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	// Create child agents
	agent1, _ := llmagent.New(llmagent.Config{
		Name:  "agent1",
		Model: model,
	})
	agent2, _ := llmagent.New(llmagent.Config{
		Name:  "agent2",
		Model: model,
	})
	agent3, _ := llmagent.New(llmagent.Config{
		Name:  "agent3",
		Model: model,
	})

	// ✅ Inline usage - most common pattern (like adk-go)
	parent, err := llmagent.New(llmagent.Config{
		Name:  "parent",
		Model: model,
		Tools: []tool.Tool{
			agenttool.New(agent1, nil),
			agenttool.New(agent2, nil),
			agenttool.New(agent3, nil),
		},
	})
	if err != nil {
		t.Fatalf("Failed to create parent: %v", err)
	}

	if parent == nil {
		t.Fatal("Parent is nil")
	}
}

// TestAgentTool_WithConfig tests the skip summarization config.
func TestAgentTool_WithConfig(t *testing.T) {
	model, err := openai.New(openai.Config{APIKey: "test"})
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	childAgent, _ := llmagent.New(llmagent.Config{
		Name:  "child",
		Model: model,
	})

	// Create with config
	agTool := agenttool.New(childAgent, &agenttool.Config{
		SkipSummarization: true,
	})

	if agTool == nil {
		t.Fatal("Tool is nil")
	}

	// Verify name follows adk-go pattern (agent name, no prefix)
	if agTool.Name() != "child" {
		t.Errorf("Expected name 'child', got %q", agTool.Name())
	}
}

// TestAgentTool_NilAgent tests that nil agent returns nil tool.
func TestAgentTool_NilAgent(t *testing.T) {
	agTool := agenttool.New(nil, nil)
	if agTool != nil {
		t.Error("Expected nil tool for nil agent")
	}
}

// TestAgentTool_ToolInterface verifies the tool implements CallableTool.
func TestAgentTool_ToolInterface(t *testing.T) {
	model, err := openai.New(openai.Config{APIKey: "test"})
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	childAgent, _ := llmagent.New(llmagent.Config{
		Name:        "child",
		Description: "A child agent",
		Model:       model,
	})

	agTool := agenttool.New(childAgent, nil)

	// Verify tool.Tool interface
	var _ tool.Tool = agTool

	// Verify tool.CallableTool interface
	callableTool, ok := agTool.(tool.CallableTool)
	if !ok {
		t.Fatal("Tool does not implement CallableTool")
	}

	// Verify schema
	schema := callableTool.Schema()
	if schema == nil {
		t.Error("Schema is nil")
	}

	// Verify name and description (adk-go pattern: just agent name, no prefix)
	if agTool.Name() != "child" {
		t.Errorf("Expected name 'child', got %q", agTool.Name())
	}

	if agTool.Description() == "" {
		t.Error("Description is empty")
	}

	if agTool.IsLongRunning() {
		t.Error("Agent tool should not be long-running")
	}
}
