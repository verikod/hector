package openai

import (
	"encoding/json"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/verikod/hector/pkg/model"
)

func TestReproConversationConversion(t *testing.T) {
	// 1. Create client
	cfg := Config{
		APIKey: "sk-test",
		Model:  "gpt-4",
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// 2. Construct history: User -> Agent(ToolCall) -> User(ToolResult)
	messages := []*a2a.Message{
		// User message
		a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Find eggs"}),

		// Agent tool call
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.DataPart{
			Data: map[string]any{
				"type":      "tool_use",
				"id":        "call_123",
				"name":      "search",
				"arguments": map[string]any{"query": "eggs"},
			},
		}),

		// Tool result
		a2a.NewMessage(a2a.MessageRoleUser, a2a.DataPart{
			Data: map[string]any{
				"type":         "tool_result",
				"tool_call_id": "call_123",
				"content":      "Eggs usage found",
			},
		}),
	}

	// 3. Create request
	req := &model.Request{
		Messages: messages,
	}

	// 4. Force build request (using private method via internal test)
	apiReq := client.buildRequest(req, false)

	// 5. Inspect input items
	t.Logf("Input items count: %d", len(apiReq.Input.([]inputItem)))

	inputs, ok := apiReq.Input.([]inputItem)
	if !ok {
		t.Fatalf("Input is not []inputItem")
	}

	// Expect 3 items: message(user), function_call(search), function_call_output(result)
	// Note: User msg is index 0. Agent msg (index 1) has no text, so just function_call.
	// Tool result msg (index 2) -> function_call_output.

	if len(inputs) != 3 {
		js, _ := json.MarshalIndent(inputs, "", "  ")
		t.Errorf("Expected 3 input items, got %d:\n%s", len(inputs), string(js))
	}

	for i, item := range inputs {
		js, _ := json.Marshal(item)
		t.Logf("Item %d: %s", i, string(js))
	}

	// Check item types
	if inputs[0].Type != "message" {
		t.Errorf("Item 0 type mismatch: %s", inputs[0].Type)
	}
	if inputs[1].Type != "function_call" {
		t.Errorf("Item 1 type mismatch: %s", inputs[1].Type)
	}
	if inputs[2].Type != "function_call_output" {
		t.Errorf("Item 2 type mismatch: %s", inputs[2].Type)
	}

	// Check linking
	if inputs[1].CallID != "call_123" {
		t.Errorf("Item 1 CallID mismatch: %s", inputs[1].CallID)
	}
	if inputs[2].CallID != "call_123" {
		t.Errorf("Item 2 CallID mismatch: %s", inputs[2].CallID)
	}

	// Check output content
	if inputs[2].Output == nil || *inputs[2].Output != "Eggs usage found" {
		t.Errorf("Item 2 Output mismatch")
	}
}

func TestMaxTokensOmitted(t *testing.T) {
	// 1. Create client with NO MaxTokens set (should remain 0)
	cfg := Config{
		APIKey: "sk-test",
		Model:  "gpt-4",
		// MaxTokens: 0 (implicit)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// 2. Create dummy request
	req := &model.Request{
		Messages: []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello"}),
		},
	}

	// 3. Build API request
	apiReq := client.buildRequest(req, false)

	// 4. Verify MaxOutputTokens is nil
	if apiReq.MaxOutputTokens != nil {
		t.Errorf("Expected MaxOutputTokens to be nil (unlimited), got %d", *apiReq.MaxOutputTokens)
	}
}
