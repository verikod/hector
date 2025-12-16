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

// Package ollama provides an Ollama LLM implementation.
//
// This implementation is strictly aligned with ADK-Go's model architecture:
//   - Uses Ollama's Chat API (/api/chat)
//   - Unified GenerateContent method with stream boolean
//   - Returns iter.Seq2[*Response, error]
//   - Uses StreamingAggregator for streaming with Partial flag
//   - Proper handling of tool calls
//   - Support for thinking models via `think` parameter
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/httpclient"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/tool"
)

const (
	defaultBaseURL   = "http://localhost:11434"
	defaultModel     = "qwen3"
	defaultTimeout   = 300 * time.Second // Ollama can be slow for first request
	defaultKeepAlive = "5m"
)

// thinkingModels contains model patterns that support thinking.
var thinkingModels = []string{"qwen3", "deepseek-r1", "deepseek-v3", "gpt-oss"}

// excludedModels contains model patterns that don't support thinking despite matching patterns.
var excludedModels = []string{"qwen3-coder", "qwen2-coder"}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool { return &b }

// Config configures the Ollama client.
type Config struct {
	// BaseURL is the Ollama server URL (default: http://localhost:11434)
	BaseURL string

	// Model is the model name (e.g., "llama3.2", "mistral", "codellama")
	Model string

	// Temperature controls randomness (0-2)
	Temperature *float64

	// TopP for nucleus sampling
	TopP *float64

	// TopK for top-k sampling
	TopK *int

	// NumPredict limits the number of tokens to predict
	NumPredict *int

	// NumCtx sets the context window size
	NumCtx *int

	// Seed for reproducible outputs
	Seed *int

	// KeepAlive controls how long the model stays loaded (default: "5m")
	KeepAlive string

	// Timeout for HTTP requests
	Timeout time.Duration

	// MaxRetries for HTTP requests with retry/backoff
	MaxRetries int

	// EnableThinking enables thinking for supported models
	EnableThinking bool

	// MaxToolOutputLength limits the length of tool outputs.
	MaxToolOutputLength int
}

// Option configures the Ollama client.
type Option func(*Config)

// WithModel sets the model name.
func WithModel(model string) Option {
	return func(c *Config) {
		c.Model = model
	}
}

// WithBaseURL sets the Ollama server URL.
func WithBaseURL(url string) Option {
	return func(c *Config) {
		c.BaseURL = url
	}
}

// WithTemperature sets the temperature.
func WithTemperature(temp float64) Option {
	return func(c *Config) {
		c.Temperature = &temp
	}
}

// WithThinking enables thinking mode.
func WithThinking() Option {
	return func(c *Config) {
		c.EnableThinking = true
	}
}

// Client is an Ollama LLM implementation.
// Implements model.LLM interface aligned with ADK-Go.
type Client struct {
	httpClient          *httpclient.Client
	baseURL             string
	modelName           string
	temperature         *float64
	topP                *float64
	topK                *int
	numPredict          *int
	numCtx              *int
	seed                *int
	keepAlive           string
	enableThinking      bool
	maxToolOutputLength int
}

// New creates a new Ollama client.
func New(cfg Config) (*Client, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	modelName := cfg.Model
	if modelName == "" {
		modelName = defaultModel
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	keepAlive := cfg.KeepAlive
	if keepAlive == "" {
		keepAlive = defaultKeepAlive
	}

	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3 // Default retries for Ollama
	}

	// Use Hector's httpclient with retry/backoff for resilience
	hc := httpclient.New(
		httpclient.WithHTTPClient(&http.Client{Timeout: timeout}),
		httpclient.WithMaxRetries(maxRetries),
		httpclient.WithBaseDelay(2*time.Second),
	)

	return &Client{
		httpClient:          hc,
		baseURL:             baseURL,
		modelName:           modelName,
		temperature:         cfg.Temperature,
		topP:                cfg.TopP,
		topK:                cfg.TopK,
		numPredict:          cfg.NumPredict,
		numCtx:              cfg.NumCtx,
		seed:                cfg.Seed,
		keepAlive:           keepAlive,
		enableThinking:      cfg.EnableThinking,
		maxToolOutputLength: cfg.MaxToolOutputLength,
	}, nil
}

// Name returns the model identifier.
func (c *Client) Name() string {
	return c.modelName
}

// Provider returns the provider type.
func (c *Client) Provider() model.Provider {
	return model.ProviderOllama
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.parseResponse(&apiResp), nil
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

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			yield(nil, fmt.Errorf("failed to create request: %w", err))
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes)))
			return
		}

		// Parse streaming JSON objects
		reader := bufio.NewReader(resp.Body)
		var finalUsage *model.Usage

		// State for accumulating tool calls by index (for parallel tool calls)
		state := &ollamaStreamState{
			toolCalls: make(map[int]*tool.ToolCall),
		}

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				yield(nil, fmt.Errorf("stream read error: %w", err))
				return
			}

			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			var chunk chatResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				continue // Skip malformed chunks
			}

			// Check for API errors in stream chunk
			if chunk.Error != "" {
				yield(nil, fmt.Errorf("Ollama API error: %s", chunk.Error))
				return
			}

			// Process streaming chunk through aggregator
			for resp, err := range c.processStreamChunk(&chunk, aggregator, state) {
				if !yield(resp, err) {
					return
				}
			}

			// Capture final usage from done response
			if chunk.Done {
				finalUsage = &model.Usage{
					PromptTokens:     chunk.PromptEvalCount,
					CompletionTokens: chunk.EvalCount,
					TotalTokens:      chunk.PromptEvalCount + chunk.EvalCount,
				}

				// Process accumulated tool calls in index order
				for resp, err := range c.processAccumulatedToolCalls(state, aggregator) {
					if !yield(resp, err) {
						return
					}
				}
			}
		}

		// Update aggregator with final usage
		if finalUsage != nil {
			aggregator.SetUsage(finalUsage)
		}

		// Close aggregator to get final aggregated response
		if final := aggregator.Close(); final != nil {
			yield(final, nil)
		}
	}
}

// ollamaStreamState holds state accumulated during streaming.
type ollamaStreamState struct {
	toolCalls map[int]*tool.ToolCall // Index-based map for parallel tool calls
}

// processStreamChunk processes a single streaming chunk through the aggregator.
func (c *Client) processStreamChunk(chunk *chatResponse, agg *model.StreamingAggregator, state *ollamaStreamState) iter.Seq2[*model.Response, error] {
	return func(yield func(*model.Response, error) bool) {
		if chunk.Message == nil {
			return
		}

		// Handle thinking content
		if chunk.Message.Thinking != "" {
			for resp, err := range agg.ProcessThinkingDelta(chunk.Message.Thinking) {
				if !yield(resp, err) {
					return
				}
			}
		}

		// Handle text content
		if chunk.Message.Content != "" {
			for resp, err := range agg.ProcessTextDelta(chunk.Message.Content) {
				if !yield(resp, err) {
					return
				}
			}
		}

		// Handle tool calls - accumulate by index for parallel tool calls
		if len(chunk.Message.ToolCalls) > 0 {
			for _, tc := range chunk.Message.ToolCalls {
				if tc.Function == nil {
					continue
				}

				// Use Ollama's index field if available, otherwise use map size
				idx := tc.Function.Index
				if idx < 0 {
					idx = len(state.toolCalls)
				}

				// Accumulate or merge tool call by index
				if existing, exists := state.toolCalls[idx]; exists {
					// Merge: update arguments if provided (for streaming arguments)
					if len(tc.Function.Arguments) > 0 {
						if existing.Args == nil {
							existing.Args = make(map[string]any)
						}
						for k, v := range tc.Function.Arguments {
							existing.Args[k] = v
						}
					}
				} else {
					// Create new tool call entry
					args := tc.Function.Arguments
					if args == nil {
						args = make(map[string]any)
					}
					state.toolCalls[idx] = &tool.ToolCall{
						ID:   fmt.Sprintf("call_%d_%s", idx, tc.Function.Name),
						Name: tc.Function.Name,
						Args: args,
					}
				}
			}
		}

		// Set finish reason on done
		if chunk.Done {
			reason := model.FinishReasonStop
			if chunk.DoneReason == "length" {
				reason = model.FinishReasonLength
			}
			agg.SetFinishReason(reason)
		}
	}
}

// processAccumulatedToolCalls processes accumulated tool calls in index order.
func (c *Client) processAccumulatedToolCalls(state *ollamaStreamState, agg *model.StreamingAggregator) iter.Seq2[*model.Response, error] {
	return func(yield func(*model.Response, error) bool) {
		// Process tool calls in index order
		maxIdx := -1
		for idx := range state.toolCalls {
			if idx > maxIdx {
				maxIdx = idx
			}
		}

		for i := 0; i <= maxIdx; i++ {
			if tc, exists := state.toolCalls[i]; exists {
				for resp, err := range agg.ProcessToolCall(*tc) {
					if !yield(resp, err) {
						return
					}
				}
			}
		}
	}
}

// isThinkingCapableModel checks if a model name indicates it supports thinking.
// This matches patterns from the legacy implementation for auto-detection.
func isThinkingCapableModel(modelName string) bool {
	modelLower := strings.ToLower(modelName)

	// Check exclusions first - models that don't support thinking despite matching patterns
	for _, excluded := range excludedModels {
		if strings.Contains(modelLower, excluded) {
			return false
		}
	}

	// Check if model matches thinking-capable patterns
	for _, pattern := range thinkingModels {
		if strings.Contains(modelLower, pattern) {
			return true
		}
	}
	return false
}

// buildRequest creates an API request from model.Request.
func (c *Client) buildRequest(req *model.Request, stream bool) *chatRequest {
	// Check if thinking is explicitly enabled via config
	thinkingEnabled := c.enableThinking || (req.Config != nil && req.Config.EnableThinking)

	apiReq := &chatRequest{
		Model:     c.modelName,
		Stream:    stream,
		KeepAlive: c.keepAlive,
	}

	// For thinking-capable models, we must explicitly control the think parameter
	// because Ollama enables thinking by default for these models (e.g., qwen3).
	// If thinking is not explicitly enabled in config, send Think=false to disable it.
	if isThinkingCapableModel(c.modelName) {
		apiReq.Think = &thinkingEnabled
	} else if thinkingEnabled {
		// For non-thinking models, only set Think=true if explicitly requested
		apiReq.Think = boolPtr(true)
	}

	// Build options
	options := make(map[string]any)

	if c.temperature != nil {
		options["temperature"] = *c.temperature
	} else if req.Config != nil && req.Config.Temperature != nil {
		options["temperature"] = *req.Config.Temperature
	}

	if c.topP != nil {
		options["top_p"] = *c.topP
	} else if req.Config != nil && req.Config.TopP != nil {
		options["top_p"] = *req.Config.TopP
	}

	if c.topK != nil {
		options["top_k"] = *c.topK
	} else if req.Config != nil && req.Config.TopK != nil {
		options["top_k"] = int(*req.Config.TopK)
	}

	if c.numPredict != nil {
		options["num_predict"] = *c.numPredict
	} else if req.Config != nil && req.Config.MaxTokens != nil {
		options["num_predict"] = *req.Config.MaxTokens
	}

	if c.numCtx != nil {
		options["num_ctx"] = *c.numCtx
	}

	if c.seed != nil {
		options["seed"] = *c.seed
	}

	if req.Config != nil && len(req.Config.StopSequences) > 0 {
		options["stop"] = req.Config.StopSequences
	}

	if len(options) > 0 {
		apiReq.Options = options
	}

	// Handle structured output
	if req.Config != nil && req.Config.ResponseSchema != nil {
		apiReq.Format = req.Config.ResponseSchema
	} else if req.Config != nil && req.Config.ResponseMIMEType == "application/json" {
		apiReq.Format = "json"
	}

	// Convert messages
	for _, msg := range req.Messages {
		if msg == nil {
			continue
		}

		ollamaMsg := c.convertMessage(msg)
		if ollamaMsg != nil {
			apiReq.Messages = append(apiReq.Messages, ollamaMsg)
		}
	}

	// Add system instruction as first message if present
	if req.SystemInstruction != "" {
		systemMsg := &chatMessage{
			Role:    "system",
			Content: req.SystemInstruction,
		}
		apiReq.Messages = append([]*chatMessage{systemMsg}, apiReq.Messages...)
	}

	// Convert tools
	if len(req.Tools) > 0 {
		apiReq.Tools = c.convertTools(req.Tools)
		apiReq.ToolChoice = "auto"
	}

	return apiReq
}

// convertMessage converts an a2a.Message to Ollama format.
func (c *Client) convertMessage(msg *a2a.Message) *chatMessage {
	role := "user"
	if msg.Role == a2a.MessageRoleAgent {
		role = "assistant"
	}

	ollamaMsg := &chatMessage{
		Role: role,
	}

	var textParts []string
	var images []string

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case a2a.TextPart:
			if p.Text != "" {
				textParts = append(textParts, p.Text)
			}

		case a2a.FilePart:
			// Handle images for multimodal models
			switch f := p.File.(type) {
			case a2a.FileBytes:
				if strings.HasPrefix(f.MimeType, "image/") {
					images = append(images, base64.StdEncoding.EncodeToString([]byte(f.Bytes)))
				}
			}

		case a2a.DataPart:
			// Handle tool calls and results
			if dataType, ok := p.Data["type"].(string); ok {
				switch dataType {
				case "tool_use":
					// Assistant tool call
					if name, ok := p.Data["name"].(string); ok {
						args, _ := p.Data["arguments"].(map[string]any)
						ollamaMsg.ToolCalls = append(ollamaMsg.ToolCalls, &toolCall{
							Function: &functionCall{
								Name:      name,
								Arguments: args,
							},
						})
					}
				case "tool_result":
					// Tool result - change role to "tool"
					ollamaMsg.Role = "tool"
					if content, ok := p.Data["content"].(string); ok {
						// Safety Truncation
						if c.maxToolOutputLength > 0 && len(content) > c.maxToolOutputLength {
							content = content[:c.maxToolOutputLength] + fmt.Sprintf("\n... [TRUNCATED by client: output length %d exceeded safety limit]", len(content))
						}
						textParts = append(textParts, content)
					}
					if toolName, ok := p.Data["tool_name"].(string); ok {
						ollamaMsg.ToolName = toolName
					}
				}
			}
		}
	}

	if len(textParts) > 0 {
		ollamaMsg.Content = strings.Join(textParts, "\n")
	}

	if len(images) > 0 {
		ollamaMsg.Images = images
	}

	// Don't return empty messages (unless it's a tool call)
	if ollamaMsg.Content == "" && len(ollamaMsg.ToolCalls) == 0 && len(ollamaMsg.Images) == 0 {
		return nil
	}

	return ollamaMsg
}

// convertTools converts tool definitions to Ollama format.
func (c *Client) convertTools(tools []tool.Definition) []*apiTool {
	result := make([]*apiTool, len(tools))
	for i, t := range tools {
		result[i] = &apiTool{
			Type: "function",
			Function: &functionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}
	return result
}

// parseResponse converts API response to model.Response.
func (c *Client) parseResponse(resp *chatResponse) *model.Response {
	result := &model.Response{
		Partial:      false,
		TurnComplete: true,
		FinishReason: model.FinishReasonStop,
	}

	// Map done reason
	if resp.DoneReason == "length" {
		result.FinishReason = model.FinishReasonLength
	}

	// Build content
	var parts []a2a.Part

	if resp.Message != nil {
		// Handle thinking content
		if resp.Message.Thinking != "" {
			result.Thinking = &model.ThinkingBlock{
				Content: resp.Message.Thinking,
			}
		}

		// Handle text content
		if resp.Message.Content != "" {
			parts = append(parts, a2a.TextPart{Text: resp.Message.Content})
		}

		// Handle tool calls
		if len(resp.Message.ToolCalls) > 0 {
			for i, tc := range resp.Message.ToolCalls {
				if tc.Function == nil {
					continue
				}
				toolCall := tool.ToolCall{
					ID:   fmt.Sprintf("call_%d", i),
					Name: tc.Function.Name,
					Args: tc.Function.Arguments,
				}
				result.ToolCalls = append(result.ToolCalls, toolCall)
				parts = append(parts, a2a.DataPart{
					Data: map[string]any{
						"type":      "tool_use",
						"id":        toolCall.ID,
						"name":      toolCall.Name,
						"arguments": toolCall.Args,
					},
				})
			}
			result.FinishReason = model.FinishReasonToolCalls
		}
	}

	if len(parts) > 0 {
		result.Content = &model.Content{
			Parts: parts,
			Role:  a2a.MessageRoleAgent,
		}
	}

	// Parse usage
	if resp.PromptEvalCount > 0 || resp.EvalCount > 0 {
		result.Usage = &model.Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		}
	}

	return result
}

// API types

type chatRequest struct {
	Model      string         `json:"model"`
	Messages   []*chatMessage `json:"messages"`
	Tools      []*apiTool     `json:"tools,omitempty"`
	ToolChoice string         `json:"tool_choice,omitempty"`
	Format     any            `json:"format,omitempty"` // "json" or JSON schema
	Options    map[string]any `json:"options,omitempty"`
	Stream     bool           `json:"stream"`
	KeepAlive  string         `json:"keep_alive,omitempty"`
	Think      *bool          `json:"think,omitempty"` // For thinking models - pointer to allow nil (omit) vs explicit false
}

type chatMessage struct {
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	Images    []string    `json:"images,omitempty"`
	ToolCalls []*toolCall `json:"tool_calls,omitempty"`
	ToolName  string      `json:"tool_name,omitempty"`
	Thinking  string      `json:"thinking,omitempty"`
}

type toolCall struct {
	Function *functionCall `json:"function,omitempty"`
}

type functionCall struct {
	Index     int            `json:"index,omitempty"` // Index for parallel tool calls
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type apiTool struct {
	Type     string       `json:"type"`
	Function *functionDef `json:"function"`
}

type functionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatResponse struct {
	Model              string       `json:"model"`
	CreatedAt          string       `json:"created_at"`
	Message            *chatMessage `json:"message,omitempty"`
	Done               bool         `json:"done"`
	DoneReason         string       `json:"done_reason,omitempty"`
	Error              string       `json:"error,omitempty"`
	TotalDuration      int64        `json:"total_duration,omitempty"`
	LoadDuration       int64        `json:"load_duration,omitempty"`
	PromptEvalCount    int          `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64        `json:"prompt_eval_duration,omitempty"`
	EvalCount          int          `json:"eval_count,omitempty"`
	EvalDuration       int64        `json:"eval_duration,omitempty"`
}

// Ensure Client implements model.LLM
var _ model.LLM = (*Client)(nil)
