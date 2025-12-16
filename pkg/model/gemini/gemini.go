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

// Package gemini implements the model.LLM interface for Google Gemini models.
//
// This implementation is strictly aligned with ADK-Go's gemini provider:
//   - Uses the official google.golang.org/genai SDK
//   - Implements unified GenerateContent with streaming aggregator
//   - Proper handling of partial/aggregated responses
package gemini

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/genai"

	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/tool"
)

// Config contains configuration for the Gemini model.
type Config struct {
	// APIKey is the Google AI API key.
	APIKey string

	// Model is the model name (e.g., "gemini-2.0-flash", "gemini-1.5-pro").
	Model string

	// MaxTokens limits the response length.
	MaxTokens int

	// Temperature controls randomness (0-2).
	Temperature float64

	// TopP controls nucleus sampling.
	TopP float64

	// TopK controls top-k sampling.
	TopK int

	// MaxToolOutputLength limits the length of tool outputs.
	MaxToolOutputLength int
}

// geminiModel implements model.LLM for Gemini.
type geminiModel struct {
	client *genai.Client
	name   string
	config Config
}

// New creates a new Gemini model instance.
func New(cfg Config) (model.LLM, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	if cfg.Model == "" {
		cfg.Model = "gemini-2.0-flash"
	}

	// Use context.Background() for initialization - constructors shouldn't require context
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &geminiModel{
		client: client,
		name:   cfg.Model,
		config: cfg,
	}, nil
}

// Name returns the model identifier.
func (m *geminiModel) Name() string {
	return m.name
}

// Provider returns the provider type.
func (m *geminiModel) Provider() model.Provider {
	return model.ProviderGemini
}

// GenerateContent produces responses for the given request.
// Aligned with ADK-Go's GenerateContent signature and behavior.
func (m *geminiModel) GenerateContent(ctx context.Context, req *model.Request, stream bool) iter.Seq2[*model.Response, error] {
	if stream {
		return m.generateStream(ctx, req)
	}

	return func(yield func(*model.Response, error) bool) {
		resp, err := m.generate(ctx, req)
		yield(resp, err)
	}
}

// Close releases resources.
func (m *geminiModel) Close() error {
	return nil
}

// generate performs non-streaming generation.
func (m *geminiModel) generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	contents, systemInstruction := m.buildRequest(req)
	config := m.buildConfig(req.Config, systemInstruction, req.Tools)

	genResp, err := m.client.Models.GenerateContent(ctx, m.name, contents, config)
	if err != nil {
		return nil, fmt.Errorf("Gemini generation failed: %w", err)
	}

	return m.parseResponse(genResp)
}

// generateStream performs streaming generation with aggregator.
// This is the ADK-Go aligned streaming pattern.
func (m *geminiModel) generateStream(ctx context.Context, req *model.Request) iter.Seq2[*model.Response, error] {
	aggregator := model.NewStreamingAggregator()
	state := &geminiStreamState{
		emittedCallIDs: make(map[string]bool),
	}

	return func(yield func(*model.Response, error) bool) {
		contents, systemInstruction := m.buildRequest(req)
		config := m.buildConfig(req.Config, systemInstruction, req.Tools)

		// Stream from Gemini
		for genResp, err := range m.client.Models.GenerateContentStream(ctx, m.name, contents, config) {
			if err != nil {
				yield(nil, fmt.Errorf("Gemini streaming error: %w", err))
				return
			}

			// Process streaming chunk through aggregator
			for resp, err := range m.processStreamChunk(aggregator, genResp, state) {
				if !yield(resp, err) {
					return // Consumer stopped
				}
			}
		}

		// Close any open thinking block at end of stream (aligned with legacy v1 implementation)
		if state.wasInThinkingBlock && aggregator.ThinkingText() != "" {
			aggregator.ProcessThinkingComplete(aggregator.ThinkingText(), "")
		}

		// Close aggregator to get final aggregated response
		if final := aggregator.Close(); final != nil {
			yield(final, nil)
		}
	}
}

// geminiStreamState holds state accumulated during Gemini streaming.
// Aligned with ADK-Go's pattern of tracking emitted call IDs at provider level.
type geminiStreamState struct {
	emittedCallIDs     map[string]bool
	wasInThinkingBlock bool // Track if we were processing thinking in previous chunk
}

// generateStableFunctionCallID creates a stable ID for a function call based on name and args.
// This ensures the same function call (same name + args) gets the same ID even if
// Gemini sends it multiple times with empty IDs in different streaming chunks.
// Aligned with ADK-Go's PopulateClientFunctionCallID but uses stable hashing for deduplication.
func generateStableFunctionCallID(name string, args map[string]any) string {
	// Create a deterministic hash from name + args
	// This ensures same call = same ID across chunks
	data := map[string]any{
		"name": name,
		"args": args,
	}
	jsonBytes, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonBytes)
	// Use first 16 bytes of hash as UUID-like identifier
	return fmt.Sprintf("hector-%x", hash[:16])
}

// processStreamChunk processes a streaming chunk through the aggregator.
// Aligned with ADK-Go's ProcessResponse pattern.
// Deduplicates tool calls at provider level (like OpenAI) before passing to aggregator.
func (m *geminiModel) processStreamChunk(agg *model.StreamingAggregator, genResp *genai.GenerateContentResponse, state *geminiStreamState) iter.Seq2[*model.Response, error] {
	return func(yield func(*model.Response, error) bool) {
		if len(genResp.Candidates) == 0 {
			return
		}

		candidate := genResp.Candidates[0]

		// Set finish reason if present
		if candidate.FinishReason != "" {
			agg.SetFinishReason(mapFinishReason(candidate.FinishReason))
		}

		// Set usage if present
		if genResp.UsageMetadata != nil {
			agg.SetUsage(&model.Usage{
				PromptTokens:     int(genResp.UsageMetadata.PromptTokenCount),
				CompletionTokens: int(genResp.UsageMetadata.CandidatesTokenCount),
				TotalTokens:      int(genResp.UsageMetadata.TotalTokenCount),
			})
		}

		// Process content parts
		if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
			return
		}

		for _, part := range candidate.Content.Parts {
			// Check for thought signature (indicates thinking block completion)
			if len(part.ThoughtSignature) > 0 {
				// Process thinking complete with signature
				agg.ProcessThinkingComplete(agg.ThinkingText(), string(part.ThoughtSignature))
				state.wasInThinkingBlock = false
			}

			// Text content
			if part.Text != "" {
				if part.Thought {
					// Thinking delta
					state.wasInThinkingBlock = true
					for resp, err := range agg.ProcessThinkingDelta(part.Text) {
						if !yield(resp, err) {
							return
						}
					}
				} else {
					// Regular text delta - if we were in thinking mode, complete the thinking block
					// (transition from thinking to regular text indicates thinking is complete)
					if state.wasInThinkingBlock && agg.ThinkingText() != "" {
						agg.ProcessThinkingComplete(agg.ThinkingText(), "")
						state.wasInThinkingBlock = false
					}
					// Regular text delta
					for resp, err := range agg.ProcessTextDelta(part.Text) {
						if !yield(resp, err) {
							return
						}
					}
				}
			}

			// Function call
			// NOTE: If duplicates appear, they may come from:
			// 1. Gemini sending same FunctionCall part multiple times in one response
			// 2. Our code processing issues
			// Adding deduplication by ID to handle case 1, but this needs verification
			if part.FunctionCall != nil {
				// Transition from thinking to tool call - close thinking block first
				// (aligned with legacy v1 implementation)
				if state.wasInThinkingBlock && agg.ThinkingText() != "" {
					agg.ProcessThinkingComplete(agg.ThinkingText(), "")
					state.wasInThinkingBlock = false
				}

				callID := part.FunctionCall.ID

				// Generate ID if missing (aligned with ADK-Go's PopulateClientFunctionCallID)
				if callID == "" {
					callID = generateStableFunctionCallID(part.FunctionCall.Name, part.FunctionCall.Args)
				}

				// Deduplicate: Skip if we've already processed this exact call ID in this stream
				// This handles the case where Gemini might send the same FunctionCall part
				// multiple times in different chunks (needs verification if this actually happens)
				if state.emittedCallIDs[callID] {
					continue
				}

				// Mark as emitted
				state.emittedCallIDs[callID] = true

				tc := tool.ToolCall{
					ID:   callID,
					Name: part.FunctionCall.Name,
					Args: part.FunctionCall.Args,
				}
				for resp, err := range agg.ProcessToolCall(tc) {
					if !yield(resp, err) {
						return
					}
				}
			}
		}
	}
}

// buildRequest converts Hector request to Gemini format.
func (m *geminiModel) buildRequest(req *model.Request) ([]*genai.Content, *genai.Content) {
	var contents []*genai.Content
	var systemInstruction *genai.Content

	// Handle system instruction
	if req.SystemInstruction != "" {
		systemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.SystemInstruction}},
			Role:  "user", // System instruction uses user role
		}
	}

	// Convert messages to contents
	for _, msg := range req.Messages {
		content := m.messageToContent(msg)
		if content != nil {
			contents = append(contents, content)
		}
	}

	return contents, systemInstruction
}

// messageToContent converts an a2a.Message to genai.Content.
func (m *geminiModel) messageToContent(msg *a2a.Message) *genai.Content {
	if msg == nil {
		return nil
	}

	var parts []*genai.Part

	for _, p := range msg.Parts {
		switch part := p.(type) {
		case a2a.TextPart:
			parts = append(parts, &genai.Part{Text: part.Text})

		case a2a.DataPart:
			// Handle tool calls/results in data parts
			if data, ok := part.Data["type"].(string); ok {
				switch data {
				case "tool_use":
					// Function call
					if name, ok := part.Data["name"].(string); ok {
						args, _ := part.Data["arguments"].(map[string]any)
						id, _ := part.Data["id"].(string)
						parts = append(parts, &genai.Part{
							FunctionCall: &genai.FunctionCall{
								ID:   id,
								Name: name,
								Args: args,
							},
						})
					}
				case "tool_result":
					// Function response
					// Our format: tool_call_id, tool_name, content (string)
					// Gemini expects: ID, Name, Response (map)
					name, _ := part.Data["tool_name"].(string)
					id, _ := part.Data["tool_call_id"].(string)

					// Convert content string to response map
					var response map[string]any
					if content, ok := part.Data["content"].(string); ok {
						// Safety Truncation
						if m.config.MaxToolOutputLength > 0 && len(content) > m.config.MaxToolOutputLength {
							content = content[:m.config.MaxToolOutputLength] + fmt.Sprintf("\n... [TRUNCATED by client: output length %d exceeded safety limit]", len(content))
						}
						response = map[string]any{"result": content}
					} else if result, ok := part.Data["result"].(map[string]any); ok {
						response = result
					}

					if name != "" || id != "" {
						parts = append(parts, &genai.Part{
							FunctionResponse: &genai.FunctionResponse{
								ID:       id,
								Name:     name,
								Response: response,
							},
						})
					}
				}
			}

		case a2a.FilePart:
			// Handle file parts (images, etc.)
			switch f := part.File.(type) {
			case a2a.FileBytes:
				parts = append(parts, &genai.Part{
					InlineData: &genai.Blob{
						MIMEType: f.MimeType,
						Data:     []byte(f.Bytes),
					},
				})
			case a2a.FileURI:
				parts = append(parts, &genai.Part{
					FileData: &genai.FileData{
						MIMEType: f.MimeType,
						FileURI:  f.URI,
					},
				})
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}

	role := "user"
	if msg.Role == a2a.MessageRoleAgent {
		role = "model"
	}

	return &genai.Content{
		Parts: parts,
		Role:  role,
	}
}

// buildConfig creates Gemini generation config.
func (m *geminiModel) buildConfig(cfg *model.GenerateConfig, systemInstruction *genai.Content, tools []tool.Definition) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
	}

	// Apply config overrides
	if cfg != nil {
		if cfg.Temperature != nil {
			config.Temperature = genai.Ptr(float32(*cfg.Temperature))
		}
		if cfg.MaxTokens != nil {
			config.MaxOutputTokens = int32(*cfg.MaxTokens)
		}
		if cfg.TopP != nil {
			config.TopP = genai.Ptr(float32(*cfg.TopP))
		}
		if cfg.TopK != nil {
			config.TopK = genai.Ptr(float32(*cfg.TopK))
		}
		if len(cfg.StopSequences) > 0 {
			config.StopSequences = cfg.StopSequences
		}
		if cfg.ResponseMIMEType != "" {
			config.ResponseMIMEType = cfg.ResponseMIMEType
		}
		// Handle ResponseSchema for structured output
		if cfg.ResponseSchema != nil {
			config.ResponseSchema = toGenaiSchema(cfg.ResponseSchema)
			// Ensure MIME type is set for structured output
			if config.ResponseMIMEType == "" {
				config.ResponseMIMEType = "application/json"
			}
		}
		// Enable thinking if configured
		if cfg.EnableThinking {
			thinkingConfig := &genai.ThinkingConfig{
				IncludeThoughts: true,
			}
			if cfg.ThinkingBudget > 0 {
				budget := int32(cfg.ThinkingBudget)
				thinkingConfig.ThinkingBudget = &budget
			}
			config.ThinkingConfig = thinkingConfig
		}
		// TODO: Handle ResponseSchema for structured output
	}

	// Apply defaults from model config
	if config.Temperature == nil && m.config.Temperature > 0 {
		config.Temperature = genai.Ptr(float32(m.config.Temperature))
	}
	if config.MaxOutputTokens == 0 && m.config.MaxTokens > 0 {
		config.MaxOutputTokens = int32(m.config.MaxTokens)
	}

	// Add tools
	if len(tools) > 0 {
		config.Tools = m.buildTools(tools)
	}

	return config
}

// buildTools converts Hector tool definitions to Gemini tools.
func (m *geminiModel) buildTools(tools []tool.Definition) []*genai.Tool {
	var genaiTools []*genai.Tool

	for _, t := range tools {
		genaiTool := &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  toGenaiSchema(t.Parameters),
				},
			},
		}
		genaiTools = append(genaiTools, genaiTool)
	}

	return genaiTools
}

// toGenaiSchema converts a JSON schema to Gemini schema.
func toGenaiSchema(schema map[string]any) *genai.Schema {
	if schema == nil {
		return nil
	}

	// Convert JSON schema to genai.Schema
	s := &genai.Schema{}

	if t, ok := schema["type"].(string); ok {
		s.Type = genai.Type(t)
	}
	if desc, ok := schema["description"].(string); ok {
		s.Description = desc
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		s.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				s.Properties[name] = toGenaiSchema(propMap)
			}
		}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, r := range required {
			if rs, ok := r.(string); ok {
				s.Required = append(s.Required, rs)
			}
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		s.Items = toGenaiSchema(items)
	}
	if enum, ok := schema["enum"].([]any); ok {
		for _, e := range enum {
			if es, ok := e.(string); ok {
				s.Enum = append(s.Enum, es)
			}
		}
	}

	return s
}

// parseResponse converts Gemini response to Hector response.
func (m *geminiModel) parseResponse(genResp *genai.GenerateContentResponse) (*model.Response, error) {
	if len(genResp.Candidates) == 0 {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	candidate := genResp.Candidates[0]

	resp := &model.Response{
		Partial:      false,
		TurnComplete: true,
		FinishReason: mapFinishReason(candidate.FinishReason),
	}

	// Parse content
	if candidate.Content != nil {
		var parts []a2a.Part
		var toolCalls []tool.ToolCall
		var thinkingParts []string
		var thoughtSignature string

		for _, part := range candidate.Content.Parts {
			// Extract thought signature if present (required for function calling continuity)
			// See: https://ai.google.dev/gemini-api/docs/thought-signatures
			if len(part.ThoughtSignature) > 0 {
				thoughtSignature = string(part.ThoughtSignature)
			}

			if part.Text != "" {
				if part.Thought {
					// Thinking content - accumulate for thinking block
					thinkingParts = append(thinkingParts, part.Text)
				} else {
					// Regular text content
					parts = append(parts, a2a.TextPart{Text: part.Text})
				}
			}
			if part.FunctionCall != nil {
				tc := tool.ToolCall{
					ID:   part.FunctionCall.ID,
					Name: part.FunctionCall.Name,
					Args: part.FunctionCall.Args,
				}
				toolCalls = append(toolCalls, tc)
				parts = append(parts, a2a.DataPart{
					Data: map[string]any{
						"type":      "tool_use",
						"id":        tc.ID,
						"name":      tc.Name,
						"arguments": tc.Args,
					},
				})
			}
		}

		role := a2a.MessageRoleAgent
		if candidate.Content.Role == "user" {
			role = a2a.MessageRoleUser
		}

		resp.Content = &model.Content{
			Parts: parts,
			Role:  role,
		}
		resp.ToolCalls = toolCalls

		// Set thinking block if we have thinking content (aligned with legacy v1 implementation)
		if len(thinkingParts) > 0 {
			thinkingContent := ""
			for _, tp := range thinkingParts {
				thinkingContent += tp
			}
			resp.Thinking = &model.ThinkingBlock{
				Content:   thinkingContent,
				Signature: thoughtSignature, // Gemini thought signature for multi-turn conversations
			}
		}
	}

	// Parse usage
	if genResp.UsageMetadata != nil {
		resp.Usage = &model.Usage{
			PromptTokens:     int(genResp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(genResp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(genResp.UsageMetadata.TotalTokenCount),
		}
	}

	return resp, nil
}

// mapFinishReason converts Gemini finish reason to Hector finish reason.
func mapFinishReason(reason genai.FinishReason) model.FinishReason {
	switch reason {
	case genai.FinishReasonStop:
		return model.FinishReasonStop
	case genai.FinishReasonMaxTokens:
		return model.FinishReasonLength
	case genai.FinishReasonSafety:
		return model.FinishReasonContent
	default:
		return model.FinishReasonStop
	}
}

// Ensure geminiModel implements model.LLM
var _ model.LLM = (*geminiModel)(nil)
