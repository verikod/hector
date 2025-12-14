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

// Package anthropic provides an Anthropic Claude LLM implementation.
//
// This implementation is strictly aligned with ADK-Go's model architecture:
//   - Unified GenerateContent method with stream boolean
//   - Returns iter.Seq2[*Response, error]
//   - Uses StreamingAggregator for streaming with Partial flag
//   - Proper handling of thinking blocks with signatures
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/kadirpekel/hector/pkg/httpclient"
	"github.com/kadirpekel/hector/pkg/model"
	"github.com/kadirpekel/hector/pkg/tool"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	apiVersion       = "2023-06-01"
	betaThinking     = "interleaved-thinking-2025-05-14"
	defaultModel     = "claude-haiku-4-5"
	defaultMaxTokens = 4096
	defaultTimeout   = 120 * time.Second

	// Temperature when thinking is enabled (Anthropic requirement)
	thinkingTemperature = 1.0
)

// Config configures the Anthropic client.
type Config struct {
	APIKey              string
	Model               string
	MaxTokens           int
	Temperature         *float64
	BaseURL             string
	Timeout             time.Duration
	MaxRetries          int
	EnableThinking      bool
	ThinkingBudget      int
	MaxToolOutputLength int
}

// Client is an Anthropic LLM implementation.
// Implements model.LLM interface aligned with ADK-Go.
type Client struct {
	httpClient          *httpclient.Client
	apiKey              string
	baseURL             string
	model               string
	maxTokens           int
	maxToolOutputLength int
	temperature         *float64
	enableThinking      bool
	thinkingBudget      int
}

// New creates a new Anthropic client.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	modelName := cfg.Model
	if modelName == "" {
		modelName = defaultModel
	}

	maxTokens := cfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 5
	}

	httpClient := httpclient.New(
		httpclient.WithHTTPClient(&http.Client{Timeout: timeout}),
		httpclient.WithMaxRetries(maxRetries),
		httpclient.WithHeaderParser(httpclient.ParseAnthropicHeaders),
	)

	thinkingBudget := cfg.ThinkingBudget
	if thinkingBudget == 0 {
		thinkingBudget = 10000
	}

	return &Client{
		httpClient:          httpClient,
		apiKey:              cfg.APIKey,
		baseURL:             baseURL,
		model:               modelName,
		maxTokens:           maxTokens,
		maxToolOutputLength: cfg.MaxToolOutputLength,
		temperature:         cfg.Temperature,
		enableThinking:      cfg.EnableThinking,
		thinkingBudget:      thinkingBudget,
	}, nil
}

// Name returns the model identifier.
func (c *Client) Name() string {
	return c.model
}

// Provider returns the provider type.
func (c *Client) Provider() model.Provider {
	return model.ProviderAnthropic
}

// GenerateContent produces responses for the given request.
// This is the ADK-Go aligned interface.
//
// When stream=false:
//   - Yields exactly one Response with complete content, Partial=false
//
// When stream=true:
//   - Yields multiple partial Responses (Partial=true) for real-time UI updates
//   - Finally yields aggregated Response (Partial=false) for session persistence
func (c *Client) GenerateContent(ctx context.Context, req *model.Request, stream bool) iter.Seq2[*model.Response, error] {
	if stream {
		return c.generateStream(ctx, req)
	}

	return func(yield func(*model.Response, error) bool) {
		resp, err := c.generate(ctx, req)
		yield(resp, err)
	}
}

// Close releases resources.
func (c *Client) Close() error {
	return nil
}

// generate performs non-streaming generation.
func (c *Client) generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	apiReq := c.buildRequest(req, false)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.parseResponse(&apiResp), nil
}

// streamState holds state accumulated during SSE streaming.
type streamState struct {
	toolJSONBuffers    map[int]string
	toolCalls          map[int]*tool.ToolCall
	thinkingBuffers    map[int]string
	thinkingSignatures map[int]string
	usage              *model.Usage
	finishReason       model.FinishReason
}

func newStreamState() *streamState {
	return &streamState{
		toolJSONBuffers:    make(map[int]string),
		toolCalls:          make(map[int]*tool.ToolCall),
		thinkingBuffers:    make(map[int]string),
		thinkingSignatures: make(map[int]string),
		finishReason:       model.FinishReasonStop,
	}
}

// generateStream performs streaming generation with aggregator.
// This is the ADK-Go aligned streaming pattern.
func (c *Client) generateStream(ctx context.Context, req *model.Request) iter.Seq2[*model.Response, error] {
	aggregator := model.NewStreamingAggregator()

	return func(yield func(*model.Response, error) bool) {
		apiReq := c.buildRequest(req, true)

		body, err := json.Marshal(apiReq)
		if err != nil {
			yield(nil, fmt.Errorf("failed to marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			yield(nil, fmt.Errorf("failed to create request: %w", err))
			return
		}

		c.setHeaders(httpReq)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body)))
			return
		}

		// Parse SSE stream
		reader := bufio.NewReader(resp.Body)
		state := newStreamState()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				yield(nil, fmt.Errorf("stream read error: %w", err))
				return
			}

			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var event streamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			// Process event through aggregator
			for resp, err := range c.processStreamEvent(&event, state, aggregator) {
				if !yield(resp, err) {
					return
				}
			}
		}

		// Update aggregator with final state
		if state.usage != nil {
			aggregator.SetUsage(state.usage)
		}
		aggregator.SetFinishReason(state.finishReason)

		// Close aggregator to get final aggregated response
		// This is the Partial=false response for session persistence
		if final := aggregator.Close(); final != nil {
			yield(final, nil)
		}
	}
}

// processStreamEvent processes a single SSE event through the aggregator.
// Returns partial responses for real-time UI updates.
func (c *Client) processStreamEvent(event *streamEvent, state *streamState, agg *model.StreamingAggregator) iter.Seq2[*model.Response, error] {
	return func(yield func(*model.Response, error) bool) {
		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil {
				switch event.ContentBlock.Type {
				case "tool_use":
					state.toolCalls[event.Index] = &tool.ToolCall{
						ID:   event.ContentBlock.ID,
						Name: event.ContentBlock.Name,
					}
					state.toolJSONBuffers[event.Index] = ""
				case "thinking":
					state.thinkingBuffers[event.Index] = ""
					state.thinkingSignatures[event.Index] = ""
				}
			}

		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta":
					// Process text through aggregator - yields partial response
					for resp, err := range agg.ProcessTextDelta(event.Delta.Text) {
						if !yield(resp, err) {
							return
						}
					}
				case "thinking_delta":
					// Accumulate thinking
					state.thinkingBuffers[event.Index] += event.Delta.Thinking
					// Process thinking through aggregator
					for resp, err := range agg.ProcessThinkingDelta(event.Delta.Thinking) {
						if !yield(resp, err) {
							return
						}
					}
				case "input_json_delta":
					state.toolJSONBuffers[event.Index] += event.Delta.PartialJSON
				case "signature_delta":
					state.thinkingSignatures[event.Index] += event.Delta.Signature
				}
			}

		case "content_block_stop":
			// Handle tool call completion
			if tc, ok := state.toolCalls[event.Index]; ok {
				if jsonStr, ok := state.toolJSONBuffers[event.Index]; ok && jsonStr != "" {
					var args map[string]any
					_ = json.Unmarshal([]byte(jsonStr), &args)
					tc.Args = args
				}
				// Process tool call through aggregator
				for resp, err := range agg.ProcessToolCall(*tc) {
					if !yield(resp, err) {
						return
					}
				}
			}

			// Handle thinking block completion
			if thinkingContent, ok := state.thinkingBuffers[event.Index]; ok && thinkingContent != "" {
				signature := state.thinkingSignatures[event.Index]
				agg.ProcessThinkingComplete(thinkingContent, signature)
			}

		case "message_delta":
			if event.Delta != nil {
				if event.Delta.StopReason != "" {
					switch event.Delta.StopReason {
					case "tool_use":
						state.finishReason = model.FinishReasonToolCalls
					case "max_tokens":
						state.finishReason = model.FinishReasonLength
					default:
						state.finishReason = model.FinishReasonStop
					}
				}
			}
			if event.Usage != nil {
				state.usage = &model.Usage{
					PromptTokens:     event.Usage.InputTokens,
					CompletionTokens: event.Usage.OutputTokens,
					TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
				}
			}
		}
	}
}

// setHeaders sets the required HTTP headers.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	if c.enableThinking {
		req.Header.Set("anthropic-beta", betaThinking)
	}
}

// buildRequest creates an API request from model.Request.
func (c *Client) buildRequest(req *model.Request, stream bool) *apiRequest {
	thinkingEnabled := c.enableThinking || (req.Config != nil && req.Config.EnableThinking)

	apiReq := &apiRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Stream:    stream,
	}

	// Set temperature
	if thinkingEnabled {
		apiReq.Temperature = thinkingTemperature
	} else if c.temperature != nil {
		apiReq.Temperature = *c.temperature
	}

	// Enable thinking if configured
	if thinkingEnabled {
		budget := c.thinkingBudget
		if req.Config != nil && req.Config.ThinkingBudget > 0 {
			budget = req.Config.ThinkingBudget
		}
		apiReq.Thinking = &thinkingSettings{
			Type:         "enabled",
			BudgetTokens: budget,
		}
	}

	// Set system instruction
	if req.SystemInstruction != "" {
		apiReq.System = req.SystemInstruction
	}

	// Convert messages
	for _, msg := range req.Messages {
		if msg == nil {
			continue
		}

		role := "user"
		if msg.Role == a2a.MessageRoleAgent {
			role = "assistant"
		}

		var content []apiContent
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case a2a.TextPart:
				content = append(content, apiContent{
					Type: "text",
					Text: p.Text,
				})
			case a2a.DataPart:
				data := p.Data
				if dataType, ok := data["type"].(string); ok {
					switch dataType {
					case "tool_use":
						var args map[string]any
						if a, ok := data["arguments"].(map[string]any); ok {
							args = a
						}
						content = append(content, apiContent{
							Type:  "tool_use",
							ID:    getString(data, "id"),
							Name:  getString(data, "name"),
							Input: args,
						})
						continue
					case "tool_result":
						toolCallID := getString(data, "tool_call_id")
						contentStr := getString(data, "content")
						// Ensure content is not empty (Anthropic rejects empty tool results)
						if contentStr == "" {
							contentStr = "(no output)"
						}
						// Ensure tool_use_id is not empty (required for Anthropic to match results to calls)
						if toolCallID == "" {
							slog.Warn("Anthropic: tool_result missing tool_call_id, skipping")
							continue
						}

						// Safety Truncation
						if c.maxToolOutputLength > 0 && len(contentStr) > c.maxToolOutputLength {
							contentStr = contentStr[:c.maxToolOutputLength] + fmt.Sprintf("\n... [TRUNCATED by client: output length %d exceeded safety limit]", len(contentStr))
						}

						content = append(content, apiContent{
							Type:      "tool_result",
							ToolUseID: toolCallID,
							Content:   contentStr,
						})
						continue
					case "thinking":
						content = append(content, apiContent{
							Type:      "thinking",
							Thinking:  getString(data, "thinking"),
							Signature: getString(data, "signature"),
						})
						continue
					}
				}
				jsonData, _ := json.Marshal(p.Data)
				content = append(content, apiContent{
					Type: "text",
					Text: string(jsonData),
				})
			}
		}

		if len(content) > 0 {
			apiReq.Messages = append(apiReq.Messages, apiMessage{
				Role:    role,
				Content: content,
			})
		}
	}

	// Convert tools
	for _, t := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, apiTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	return apiReq
}

// parseResponse converts API response to model.Response.
func (c *Client) parseResponse(resp *apiResponse) *model.Response {
	result := &model.Response{
		Partial:      false, // Non-streaming response is always complete
		TurnComplete: true,
		Usage: &model.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
		FinishReason: model.FinishReasonStop,
	}

	// Map stop reason
	switch resp.StopReason {
	case "tool_use":
		result.FinishReason = model.FinishReasonToolCalls
	case "max_tokens":
		result.FinishReason = model.FinishReasonLength
	}

	// Parse content
	var parts []a2a.Part
	for _, content := range resp.Content {
		switch content.Type {
		case "text":
			parts = append(parts, a2a.TextPart{Text: content.Text})
		case "thinking":
			result.Thinking = &model.ThinkingBlock{
				Content:   content.Thinking,
				Signature: content.Signature,
			}
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, tool.ToolCall{
				ID:   content.ID,
				Name: content.Name,
				Args: content.Input,
			})
		}
	}

	if len(parts) > 0 {
		result.Content = &model.Content{
			Parts: parts,
			Role:  a2a.MessageRoleAgent,
		}
	}

	return result
}

// getString safely extracts a string from a map.
// Handles various types that might be stored (string, number, etc.)
// and converts them to string representation.
func getString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}

	// Already a string
	if s, ok := v.(string); ok {
		return s
	}

	// Convert other types to string
	return fmt.Sprintf("%v", v)
}

// API types

type apiRequest struct {
	Model       string            `json:"model"`
	Messages    []apiMessage      `json:"messages"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature float64           `json:"temperature,omitempty"`
	Stream      bool              `json:"stream"`
	System      string            `json:"system,omitempty"`
	Tools       []apiTool         `json:"tools,omitempty"`
	Thinking    *thinkingSettings `json:"thinking,omitempty"`
}

type thinkingSettings struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type apiMessage struct {
	Role    string       `json:"role"`
	Content []apiContent `json:"content"`
}

type apiContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
	Signature string         `json:"signature,omitempty"`
}

type apiTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type apiResponse struct {
	ID         string       `json:"id"`
	Type       string       `json:"type"`
	Role       string       `json:"role"`
	Content    []apiContent `json:"content"`
	StopReason string       `json:"stop_reason"`
	Usage      apiUsage     `json:"usage"`
}

type apiUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type streamEvent struct {
	Type         string      `json:"type"`
	Index        int         `json:"index"`
	Delta        *apiDelta   `json:"delta,omitempty"`
	ContentBlock *apiContent `json:"content_block,omitempty"`
	Usage        *apiUsage   `json:"usage,omitempty"`
}

type apiDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

// Ensure Client implements model.LLM
var _ model.LLM = (*Client)(nil)
