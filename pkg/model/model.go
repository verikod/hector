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

// Package model defines the LLM interface for Hector v2.
//
// This package is strictly aligned with ADK-Go's model architecture:
//   - Unified GenerateContent method with stream boolean parameter
//   - Returns iter.Seq2[*Response, error] for both streaming and non-streaming
//   - Streaming uses Partial flag to distinguish chunks from aggregated response
//   - Aggregator pattern for accumulating streaming text
package model

import (
	"context"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/tool"
)

// LLM is the interface for language models.
//
// This interface is aligned with ADK-Go's model.LLM interface.
// Key design principles:
//   - Single GenerateContent method handles both streaming and non-streaming
//   - Returns iter.Seq2 which yields one or more Response objects
//   - For non-streaming: yields exactly one Response
//   - For streaming: yields multiple partial Responses (Partial=true), then final aggregated (Partial=false)
type LLM interface {
	// Name returns the model identifier.
	Name() string

	// Provider returns the provider type (e.g., "openai", "anthropic", "gemini").
	// Used for model-specific message formatting and content processing.
	Provider() Provider

	// GenerateContent produces responses for the given request.
	//
	// When stream=false:
	//   - Yields exactly one Response with complete content
	//   - Response.Partial will be false
	//
	// When stream=true:
	//   - Yields multiple partial Responses with Partial=true
	//   - Finally yields aggregated Response with Partial=false and full content
	//   - The aggregated response is for session persistence
	//   - Partial responses are for real-time UI updates
	GenerateContent(ctx context.Context, req *Request, stream bool) iter.Seq2[*Response, error]

	// Close releases any resources held by the LLM.
	Close() error
}

// Provider identifies the LLM provider.
// Used for model-specific message formatting and content processing.
type Provider string

const (
	// ProviderOpenAI represents OpenAI models (GPT-4, etc.)
	// Tool results are separate function_call_output items.
	ProviderOpenAI Provider = "openai"

	// ProviderAnthropic represents Anthropic models (Claude)
	// Tool results must be paired with tool_use in the same message.
	ProviderAnthropic Provider = "anthropic"

	// ProviderGemini represents Google Gemini models
	// Similar to Anthropic - tool results paired with function calls.
	ProviderGemini Provider = "gemini"

	// ProviderOllama represents Ollama local models
	// Follows OpenAI-compatible format.
	ProviderOllama Provider = "ollama"

	// ProviderUnknown for unrecognized providers.
	ProviderUnknown Provider = "unknown"
)

// Request contains the input for an LLM call.
type Request struct {
	// Messages is the conversation history.
	Messages []*a2a.Message

	// Tools available for the model to call.
	Tools []tool.Definition

	// Config contains generation configuration.
	Config *GenerateConfig

	// SystemInstruction is prepended to the conversation.
	SystemInstruction string
}

// GenerateConfig contains configuration for generation.
type GenerateConfig struct {
	// Temperature controls randomness (0-2).
	Temperature *float64

	// MaxTokens limits the response length.
	MaxTokens *int

	// TopP controls nucleus sampling.
	TopP *float64

	// TopK controls top-k sampling.
	TopK *int

	// StopSequences terminates generation.
	StopSequences []string

	// ResponseMIMEType for structured output (e.g., "application/json").
	ResponseMIMEType string

	// ResponseSchema for structured output.
	ResponseSchema map[string]any

	// ResponseSchemaName identifies the schema for providers that require it.
	// Used by OpenAI's json_schema format.
	// Default: "response"
	ResponseSchemaName string

	// ResponseSchemaStrict enables strict schema validation.
	// When true, the LLM is constrained to only output valid schema conforming JSON.
	// Default: true (nil means true)
	ResponseSchemaStrict *bool

	// EnableThinking enables extended thinking (model-specific).
	EnableThinking bool

	// ThinkingBudget limits thinking tokens (model-specific).
	ThinkingBudget int

	// Metadata contains additional key-value pairs for LLM providers.
	// Used for authentication tokens, custom headers, etc.
	Metadata map[string]string
}

// Clone creates a deep copy of the GenerateConfig.
// This is important for processor pipelines to avoid shared state between requests.
func (c *GenerateConfig) Clone() *GenerateConfig {
	if c == nil {
		return nil
	}

	// Shallow copy first
	clone := *c

	// Deep copy Temperature (pointer)
	if c.Temperature != nil {
		temp := *c.Temperature
		clone.Temperature = &temp
	}

	// Deep copy MaxTokens (pointer)
	if c.MaxTokens != nil {
		maxTok := *c.MaxTokens
		clone.MaxTokens = &maxTok
	}

	// Deep copy TopP (pointer)
	if c.TopP != nil {
		topP := *c.TopP
		clone.TopP = &topP
	}

	// Deep copy TopK (pointer)
	if c.TopK != nil {
		topK := *c.TopK
		clone.TopK = &topK
	}

	// Deep copy StopSequences (slice)
	if c.StopSequences != nil {
		clone.StopSequences = make([]string, len(c.StopSequences))
		copy(clone.StopSequences, c.StopSequences)
	}

	// Deep copy ResponseSchema (map)
	if c.ResponseSchema != nil {
		clone.ResponseSchema = deepCopyMap(c.ResponseSchema)
	}

	// Deep copy ResponseSchemaStrict (pointer)
	if c.ResponseSchemaStrict != nil {
		strict := *c.ResponseSchemaStrict
		clone.ResponseSchemaStrict = &strict
	}

	// Deep copy Metadata (map)
	if c.Metadata != nil {
		clone.Metadata = make(map[string]string, len(c.Metadata))
		for k, v := range c.Metadata {
			clone.Metadata[k] = v
		}
	}

	return &clone
}

// deepCopyMap creates a deep copy of a map[string]any.
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = deepCopyMap(val)
		case []any:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice creates a deep copy of a []any.
func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}

	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = deepCopyMap(val)
		case []any:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// Response contains the result of an LLM call.
//
// This is aligned with ADK-Go's LLMResponse structure.
type Response struct {
	// Content is the generated content (text, tool calls, etc.)
	Content *Content

	// Partial indicates whether this is a streaming chunk (true) or final response (false).
	// In streaming mode:
	//   - Partial=true: This is a delta chunk for real-time display
	//   - Partial=false: This is the aggregated final response for persistence
	Partial bool

	// TurnComplete indicates whether the model has finished its turn.
	TurnComplete bool

	// ToolCalls requested by the model.
	ToolCalls []tool.ToolCall

	// Usage statistics.
	Usage *Usage

	// Thinking contains the model's reasoning (if enabled).
	Thinking *ThinkingBlock

	// FinishReason indicates why generation stopped.
	FinishReason FinishReason

	// ErrorCode for provider-specific errors.
	ErrorCode string

	// ErrorMessage for provider-specific error messages.
	ErrorMessage string
}

// Content represents the content of a response.
type Content struct {
	// Parts contains the content parts (text, data, files).
	Parts []a2a.Part

	// Role identifies the sender (agent/user).
	Role a2a.MessageRole
}

// Usage contains token usage statistics.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ThinkingTokens   int
}

// ThinkingBlock contains the model's reasoning.
type ThinkingBlock struct {
	// ID uniquely identifies this thinking block in the conversation.
	// Generated by the LLM provider or assigned during aggregation.
	ID string

	// Content is the thinking/reasoning text.
	Content string

	// Signature is used for multi-turn verification (e.g., Anthropic).
	Signature string
}

// FinishReason indicates why generation stopped.
type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonLength    FinishReason = "length"
	FinishReasonToolCalls FinishReason = "tool_calls"
	FinishReasonContent   FinishReason = "content_filter"
	FinishReasonError     FinishReason = "error"
)

// TextContent extracts text from a response.
func (r *Response) TextContent() string {
	if r == nil || r.Content == nil {
		return ""
	}

	var text string
	for _, part := range r.Content.Parts {
		if tp, ok := part.(a2a.TextPart); ok {
			text += tp.Text
		}
	}
	return text
}

// HasToolCalls returns whether the response contains tool calls.
func (r *Response) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// ToMessage converts a Response to an a2a.Message.
func (r *Response) ToMessage() *a2a.Message {
	if r == nil || r.Content == nil {
		return nil
	}
	return a2a.NewMessage(r.Content.Role, r.Content.Parts...)
}
