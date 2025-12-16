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

// Package openai provides an OpenAI LLM implementation using the Responses API.
//
// This implementation is strictly aligned with ADK-Go's model architecture:
//   - Uses OpenAI's new Responses API (/v1/responses)
//   - Unified GenerateContent method with stream boolean
//   - Returns iter.Seq2[*Response, error]
//   - Uses StreamingAggregator for streaming with Partial flag
//   - Proper handling of reasoning/thinking for o1/o3/o4/gpt-5 models
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-5"
	defaultTimeout = 120 * time.Second

	// Reasoning effort thresholds
	reasoningEffortLowThreshold    = 1024
	reasoningEffortMediumThreshold = 8192

	// Max image size (20MB for OpenAI)
	maxImageSize = 20 * 1024 * 1024
)

// SSE Event Types for Responses API
const (
	eventResponseCreated           = "response.created"
	eventOutputItemAdded           = "response.output_item.added"
	eventOutputItemDone            = "response.output_item.done"
	eventOutputTextDelta           = "response.output_text.delta"
	eventOutputTextDone            = "response.output_text.done"
	eventFunctionCallArgsDelta     = "response.function_call_arguments.delta"
	eventFunctionCallArgsDone      = "response.function_call_arguments.done"
	eventReasoningSummaryTextDelta = "response.reasoning_summary_text.delta"
	eventReasoningSummaryTextDone  = "response.reasoning_summary_text.done"
	eventReasoningSummaryPartDone  = "response.reasoning_summary_part.done"
	eventContentPartAdded          = "response.content_part.added"
	eventResponseCompleted         = "response.completed"
	eventResponseFailed            = "response.failed"
	eventError                     = "error"
)

// Config configures the OpenAI client.
type Config struct {
	APIKey              string
	Model               string
	MaxTokens           int
	Temperature         *float64
	BaseURL             string
	Timeout             time.Duration
	MaxRetries          int
	MaxToolOutputLength int
	EnableReasoning     bool
	ReasoningBudget     int // Maps to reasoning.effort: low/medium/high
}

// Option configures the OpenAI client.
type Option func(*Config)

// WithModel sets the model name.
func WithModel(model string) Option {
	return func(c *Config) {
		c.Model = model
	}
}

// WithMaxTokens sets the maximum output tokens.
func WithMaxTokens(maxTokens int) Option {
	return func(c *Config) {
		c.MaxTokens = maxTokens
	}
}

// WithTemperature sets the temperature.
func WithTemperature(temp float64) Option {
	return func(c *Config) {
		c.Temperature = &temp
	}
}

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) Option {
	return func(c *Config) {
		c.BaseURL = url
	}
}

// WithReasoning enables reasoning with the given budget.
func WithReasoning(budget int) Option {
	return func(c *Config) {
		c.EnableReasoning = true
		c.ReasoningBudget = budget
	}
}

// Client is an OpenAI LLM implementation using the Responses API.
// Implements model.LLM interface aligned with ADK-Go.
type Client struct {
	httpClient          *httpclient.Client
	apiKey              string
	baseURL             string
	modelName           string
	maxTokens           int
	maxToolOutputLength int
	temperature         *float64
	enableReasoning     bool
	reasoningBudget     int
}

// New creates a new OpenAI client.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	modelName := cfg.Model
	if modelName == "" {
		modelName = defaultModel
	}

	maxTokens := cfg.MaxTokens

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
		httpclient.WithHeaderParser(httpclient.ParseOpenAIHeaders),
	)

	reasoningBudget := cfg.ReasoningBudget
	if reasoningBudget == 0 {
		reasoningBudget = 8192 // Default to medium
	}

	return &Client{
		httpClient:          httpClient,
		apiKey:              cfg.APIKey,
		baseURL:             baseURL,
		modelName:           modelName,
		maxTokens:           maxTokens,
		maxToolOutputLength: cfg.MaxToolOutputLength,
		temperature:         cfg.Temperature,
		enableReasoning:     cfg.EnableReasoning,
		reasoningBudget:     reasoningBudget,
	}, nil
}

// Name returns the model identifier.
func (c *Client) Name() string {
	return c.modelName
}

// Provider returns the provider type.
func (c *Client) Provider() model.Provider {
	return model.ProviderOpenAI
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.responsesURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Even on error, try to read the response body for better error messages
		if resp != nil {
			defer resp.Body.Close()
			bodyBytes, _ := io.ReadAll(resp.Body)
			if len(bodyBytes) > 0 {
				return nil, fmt.Errorf("request failed: %w - response: %s", err, string(bodyBytes))
			}
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp responsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.parseResponse(&apiResp)
}

// streamState holds state accumulated during SSE streaming.
type streamState struct {
	thinkingBlockID   string
	thinkingSignature string
	thinkingStreamed  bool
	functionCallID    string
	functionCallName  string
	functionCallArgs  strings.Builder
	totalTokens       int
	emittedCallIDs    map[string]bool
}

func newStreamState() *streamState {
	return &streamState{
		emittedCallIDs: make(map[string]bool),
	}
}

func (s *streamState) resetThinking() {
	s.thinkingBlockID = ""
	s.thinkingSignature = ""
}

func (s *streamState) resetFunctionCall() {
	s.functionCallID = ""
	s.functionCallName = ""
	s.functionCallArgs.Reset()
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

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.responsesURL(), bytes.NewReader(body))
		if err != nil {
			yield(nil, fmt.Errorf("failed to create request: %w", err))
			return
		}

		c.setHeaders(httpReq)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			// Even on error, try to read the response body for better error messages
			if resp != nil {
				defer resp.Body.Close()
				bodyBytes, _ := io.ReadAll(resp.Body)
				if len(bodyBytes) > 0 {
					yield(nil, fmt.Errorf("request failed: %w - response: %s", err, string(bodyBytes)))
					return
				}
			}
			yield(nil, fmt.Errorf("request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes)))
			return
		}

		// Parse SSE stream
		reader := bufio.NewReader(resp.Body)
		state := newStreamState()
		var currentEventType string

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					if state.totalTokens == 0 {
						slog.Warn("Stream closed with EOF but no content/tokens received")
					}
					break
				}
				yield(nil, fmt.Errorf("stream read error: %w", err))
				return
			}

			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}

			// Handle event type line
			if bytes.HasPrefix(line, []byte("event: ")) {
				currentEventType = string(bytes.TrimSpace(line[7:]))
				continue
			}

			// Handle data line
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}
			dataLine := line[6:]

			var streamEvent map[string]any
			if err := json.Unmarshal(dataLine, &streamEvent); err != nil {
				slog.Debug("Failed to parse streaming event", "error", err)
				currentEventType = ""
				continue
			}

			eventType := currentEventType
			if eventType == "" {
				eventType, _ = streamEvent["type"].(string)
			}
			currentEventType = ""

			// Process event through aggregator
			for resp, err := range c.processStreamEvent(streamEvent, eventType, state, aggregator) {
				if !yield(resp, err) {
					return
				}
			}
		}

		// Update aggregator with final state
		if state.totalTokens > 0 {
			aggregator.SetUsage(&model.Usage{
				TotalTokens: state.totalTokens,
			})
		}

		// Close aggregator to get final aggregated response
		if final := aggregator.Close(); final != nil {
			yield(final, nil)
		}
	}
}

// processStreamEvent processes a single SSE event through the aggregator.
func (c *Client) processStreamEvent(
	event map[string]any,
	eventType string,
	state *streamState,
	agg *model.StreamingAggregator,
) iter.Seq2[*model.Response, error] {
	return func(yield func(*model.Response, error) bool) {
		switch eventType {
		case eventResponseCreated:
			// Response started - no action needed

		case eventOutputItemAdded:
			item, ok := event["item"].(map[string]any)
			if !ok {
				return
			}
			itemType, _ := item["type"].(string)

			switch itemType {
			case "reasoning":
				if id, ok := item["id"].(string); ok {
					state.thinkingBlockID = id
				}
				state.thinkingStreamed = false

			case "function_call":
				if callID, ok := item["call_id"].(string); ok {
					state.functionCallID = callID
				} else if id, ok := item["id"].(string); ok {
					state.functionCallID = id
				}
				if name, ok := item["name"].(string); ok {
					state.functionCallName = name
				}
				state.functionCallArgs.Reset()
			}

		case eventOutputItemDone:
			item, ok := event["item"].(map[string]any)
			if !ok {
				return
			}
			itemType, _ := item["type"].(string)

			switch itemType {
			case "reasoning":
				// Extract encrypted content signature
				if encryptedContent, ok := item["encrypted_content"].(map[string]any); ok {
					if data, ok := encryptedContent["data"].(string); ok {
						state.thinkingSignature = data
					}
				}

				// Only emit if not already streamed via deltas
				if !state.thinkingStreamed {
					if summary, ok := item["summary"].([]any); ok {
						for _, summaryItem := range summary {
							if itemMap, ok := summaryItem.(map[string]any); ok {
								textType, _ := itemMap["type"].(string)
								if textType == "summary_text" {
									if text, ok := itemMap["text"].(string); ok && text != "" {
										for resp, err := range agg.ProcessThinkingDelta(text) {
											if !yield(resp, err) {
												return
											}
										}
									}
								}
							}
						}
					}
					agg.ProcessThinkingComplete("", state.thinkingSignature)
				}
				state.resetThinking()

			case "function_call":
				// Function call completed via output_item.done
				callID := ""
				if cid, ok := item["call_id"].(string); ok {
					callID = cid
				} else if id, ok := item["id"].(string); ok {
					callID = id
				}
				name, _ := item["name"].(string)
				argsStr, _ := item["arguments"].(string)

				if callID != "" && name != "" && !state.emittedCallIDs[callID] {
					var args map[string]any
					if argsStr != "" {
						if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
							args = make(map[string]any)
						}
					} else {
						args = make(map[string]any)
					}

					state.emittedCallIDs[callID] = true
					tc := tool.ToolCall{
						ID:   callID,
						Name: name,
						Args: args,
					}
					for resp, err := range agg.ProcessToolCall(tc) {
						if !yield(resp, err) {
							return
						}
					}
				}
				state.resetFunctionCall()
			}

		case eventOutputTextDelta:
			// Text streaming - extract delta text
			var deltaText string
			if delta, ok := event["delta"].(string); ok && delta != "" {
				deltaText = delta
			} else if deltaObj, ok := event["delta"].(map[string]any); ok {
				if text, ok := deltaObj["text"].(string); ok {
					deltaText = text
				}
			} else if text, ok := event["text"].(string); ok && text != "" {
				deltaText = text
			}

			if deltaText != "" {
				for resp, err := range agg.ProcessTextDelta(deltaText) {
					if !yield(resp, err) {
						return
					}
				}
			}

		case eventFunctionCallArgsDelta:
			// Streaming function call arguments
			if delta, ok := event["delta"].(string); ok && delta != "" {
				state.functionCallArgs.WriteString(delta)
			}

		case eventFunctionCallArgsDone:
			// Function call arguments complete
			if state.functionCallID != "" && state.functionCallName != "" && !state.emittedCallIDs[state.functionCallID] {
				var args map[string]any
				argsStr := state.functionCallArgs.String()
				if argsStr != "" {
					if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
						args = make(map[string]any)
					}
				} else {
					args = make(map[string]any)
				}

				state.emittedCallIDs[state.functionCallID] = true
				tc := tool.ToolCall{
					ID:   state.functionCallID,
					Name: state.functionCallName,
					Args: args,
				}
				for resp, err := range agg.ProcessToolCall(tc) {
					if !yield(resp, err) {
						return
					}
				}
				state.resetFunctionCall()
			}

		case eventReasoningSummaryTextDelta:
			// Reasoning/thinking content streaming
			if delta, ok := event["delta"].(string); ok && delta != "" {
				state.thinkingStreamed = true
				for resp, err := range agg.ProcessThinkingDelta(delta) {
					if !yield(resp, err) {
						return
					}
				}
			}

		case eventReasoningSummaryTextDone, eventReasoningSummaryPartDone:
			// Reasoning complete
			if state.thinkingBlockID != "" {
				state.thinkingStreamed = true
				agg.ProcessThinkingComplete("", state.thinkingSignature)
				state.resetThinking()
			}

		case eventResponseCompleted:
			// Extract usage
			if response, ok := event["response"].(map[string]any); ok {
				if usage, ok := response["usage"].(map[string]any); ok {
					if total, ok := usage["total_tokens"].(float64); ok {
						state.totalTokens = int(total)
					}
				}
			}

		case eventResponseFailed:
			if response, ok := event["response"].(map[string]any); ok {
				if status, ok := response["status"].(string); ok && status == "failed" {
					if lastError, ok := response["last_error"].(map[string]any); ok {
						code, _ := lastError["code"].(string)
						msg, _ := lastError["message"].(string)
						yield(nil, fmt.Errorf("API response failed: %s: %s", code, msg))
						return
					}
				}
			}

		case eventError:
			if errorObj, ok := event["error"].(map[string]any); ok {
				code, _ := errorObj["code"].(string)
				msg, _ := errorObj["message"].(string)
				yield(nil, fmt.Errorf("API stream error: %s: %s", code, msg))
				return
			}
		}
	}
}

// responsesURL returns the URL for the OpenAI Responses API.
func (c *Client) responsesURL() string {
	if strings.HasSuffix(c.baseURL, "/v1") {
		return c.baseURL + "/responses"
	}
	return c.baseURL + "/responses"
}

// setHeaders sets the required HTTP headers.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

// buildRequest creates an API request from model.Request.
func (c *Client) buildRequest(req *model.Request, stream bool) *responsesRequest {
	enableReasoning := c.enableReasoning || (req.Config != nil && req.Config.EnableThinking)

	apiReq := &responsesRequest{
		Model:  c.modelName,
		Stream: stream,
	}

	// Set max output tokens
	if c.maxTokens > 0 {
		apiReq.MaxOutputTokens = &c.maxTokens
	}

	// Set temperature (only for non-reasoning models)
	if !enableReasoning && !c.isReasoningModel() {
		if c.temperature != nil {
			apiReq.Temperature = c.temperature
		}
	}

	// Enable reasoning for supported models
	if enableReasoning && c.isReasoningModel() {
		budget := c.reasoningBudget
		if req.Config != nil && req.Config.ThinkingBudget > 0 {
			budget = req.Config.ThinkingBudget
		}
		effort := c.mapBudgetToEffort(budget)

		apiReq.Reasoning = &reasoningConfig{
			Effort:  effort,
			Summary: "auto",
		}
		// Request encrypted content for multi-turn reasoning
		apiReq.Include = []string{"reasoning.encrypted_content"}
	}

	// Set system instruction
	if req.SystemInstruction != "" {
		apiReq.Instructions = req.SystemInstruction
	}

	// Convert messages to input items
	inputItems := c.convertMessages(req.Messages)
	if len(inputItems) > 0 {
		apiReq.Input = inputItems
	}

	// Convert tools
	if len(req.Tools) > 0 {
		apiReq.Tools = c.convertTools(req.Tools)
		apiReq.ToolChoice = "auto"
	}

	// Handle structured output
	if req.Config != nil && req.Config.ResponseSchema != nil {
		// Use config values or defaults
		schemaName := req.Config.ResponseSchemaName
		if schemaName == "" {
			schemaName = "response"
		}
		strict := true
		if req.Config.ResponseSchemaStrict != nil {
			strict = *req.Config.ResponseSchemaStrict
		}

		apiReq.Text = &textFormat{
			Format: &jsonSchemaFormat{
				Type:   "json_schema",
				Name:   schemaName,
				Strict: strict,
				Schema: req.Config.ResponseSchema,
			},
		}
	}

	return apiReq
}

// convertMessages converts a2a.Message to OpenAI input items.
func (c *Client) convertMessages(messages []*a2a.Message) []inputItem {
	var items []inputItem

	for _, msg := range messages {
		if msg == nil {
			continue
		}

		// Check for tool results
		toolResults := c.extractToolResults(msg)
		if len(toolResults) > 0 {
			for _, tr := range toolResults {
				// Safety Truncation for massive tool outputs (prevents context_length_exceeded)
				content := tr.Content
				if c.maxToolOutputLength > 0 && len(content) > c.maxToolOutputLength {
					content = content[:c.maxToolOutputLength] + fmt.Sprintf("\n... [TRUNCATED by client: output length %d exceeded safety limit]", len(tr.Content))
				}

				items = append(items, inputItem{
					Type:   "function_call_output",
					CallID: tr.ToolCallID,
					Output: &content,
				})
			}
			continue
		}

		// Check for tool calls in agent messages
		toolCalls := c.extractToolCalls(msg)
		if msg.Role == a2a.MessageRoleAgent && len(toolCalls) > 0 {
			// Add text content first if any
			textContent := c.extractText(msg)
			if textContent != "" {
				items = append(items, inputItem{
					Type:    "message",
					Role:    "assistant",
					Content: []map[string]any{{"type": "output_text", "text": textContent}},
				})
			}

			// Add each tool call as a separate item
			for _, tc := range toolCalls {
				argsJSON, _ := json.Marshal(tc.Args)
				items = append(items, inputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: string(argsJSON),
				})
			}
			continue
		}

		// Regular message
		role := "user"
		if msg.Role == a2a.MessageRoleAgent {
			role = "assistant"
		}

		content := c.extractContent(msg, role)
		if len(content) > 0 {
			items = append(items, inputItem{
				Type:    "message",
				Role:    role,
				Content: content,
			})
		}
	}

	return items
}

// extractContent extracts content parts from a message.
func (c *Client) extractContent(msg *a2a.Message, role string) []map[string]any {
	var parts []map[string]any

	// Determine text content type based on role
	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case a2a.TextPart:
			if p.Text != "" {
				parts = append(parts, map[string]any{
					"type": textType,
					"text": p.Text,
				})
			}

		case a2a.FilePart:
			// Handle file/image content
			switch f := p.File.(type) {
			case a2a.FileBytes:
				if strings.HasPrefix(f.MimeType, "image/") && len(f.Bytes) <= maxImageSize {
					base64Data := base64.StdEncoding.EncodeToString([]byte(f.Bytes))
					url := fmt.Sprintf("data:%s;base64,%s", f.MimeType, base64Data)
					parts = append(parts, map[string]any{
						"type":      "input_image",
						"image_url": url,
					})
				}
			case a2a.FileURI:
				if strings.HasPrefix(f.MimeType, "image/") {
					parts = append(parts, map[string]any{
						"type":      "input_image",
						"image_url": f.URI,
					})
				}
			}
		}
	}

	return parts
}

// extractText extracts text content from a message.
func (c *Client) extractText(msg *a2a.Message) string {
	var text strings.Builder
	for _, part := range msg.Parts {
		if tp, ok := part.(a2a.TextPart); ok && tp.Text != "" {
			text.WriteString(tp.Text)
		}
	}
	return text.String()
}

// extractToolCalls extracts tool calls from a message.
func (c *Client) extractToolCalls(msg *a2a.Message) []tool.ToolCall {
	var calls []tool.ToolCall
	for _, part := range msg.Parts {
		if dp, ok := part.(a2a.DataPart); ok {
			if dataType, ok := dp.Data["type"].(string); ok && dataType == "tool_use" {
				tc := tool.ToolCall{
					ID: getString(dp.Data, "id"),
				}
				if name, ok := dp.Data["name"].(string); ok {
					tc.Name = name
				}
				if args, ok := dp.Data["arguments"].(map[string]any); ok {
					tc.Args = args
				}
				calls = append(calls, tc)
			}
		}
	}
	return calls
}

// extractToolResults extracts tool results from a message.
func (c *Client) extractToolResults(msg *a2a.Message) []tool.ToolResult {
	var results []tool.ToolResult
	for _, part := range msg.Parts {
		if dp, ok := part.(a2a.DataPart); ok {
			if dataType, ok := dp.Data["type"].(string); ok && dataType == "tool_result" {
				tr := tool.ToolResult{
					ToolCallID: getString(dp.Data, "tool_call_id"),
					Content:    getString(dp.Data, "content"),
				}
				results = append(results, tr)
			}
		}
	}

	if len(results) == 0 && len(msg.Parts) > 0 {
		// Debug log for missed parts
		for i, part := range msg.Parts {
			if dp, ok := part.(a2a.DataPart); ok {
				slog.Debug("Inspecting DataPart", "idx", i, "data", dp.Data)
			}
		}
	}
	return results
}

// convertTools converts tool definitions to OpenAI format.
func (c *Client) convertTools(tools []tool.Definition) []apiTool {
	result := make([]apiTool, len(tools))
	for i, t := range tools {
		result[i] = apiTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			Strict:      false,
		}
	}
	return result
}

// parseResponse converts API response to model.Response.
func (c *Client) parseResponse(resp *responsesResponse) (*model.Response, error) {
	if resp.Error != nil {
		return nil, fmt.Errorf("API error: %s", resp.Error.Message)
	}

	if resp.Status != "completed" {
		msg := fmt.Sprintf("response incomplete: status=%s", resp.Status)
		if resp.IncompleteDetails != nil {
			msg += fmt.Sprintf(", reason=%s", resp.IncompleteDetails.Reason)
		}
		return nil, fmt.Errorf("%s", msg)
	}

	if len(resp.Output) == 0 {
		return nil, fmt.Errorf("no output items in response")
	}

	result := &model.Response{
		Partial:      false,
		TurnComplete: true,
		Usage: &model.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
		FinishReason: model.FinishReasonStop,
	}

	// Extract thinking from reasoning summary
	if resp.Reasoning != nil && resp.Reasoning.Summary != nil {
		thinkingContent := *resp.Reasoning.Summary
		if thinkingContent != "" {
			result.Thinking = &model.ThinkingBlock{
				Content: thinkingContent,
			}
		}
	}

	// Parse output items
	var parts []a2a.Part
	for _, outputItem := range resp.Output {
		switch outputItem.Type {
		case "message":
			text := c.extractTextFromOutput(outputItem)
			if text != "" {
				parts = append(parts, a2a.TextPart{Text: text})
			}

		case "function_call":
			tc, err := c.parseFunctionCall(outputItem)
			if err != nil {
				slog.Warn("Failed to parse function call", "error", err)
				continue
			}
			if tc != nil {
				result.ToolCalls = append(result.ToolCalls, *tc)
				parts = append(parts, a2a.DataPart{
					Data: map[string]any{
						"type":      "tool_use",
						"id":        tc.ID,
						"name":      tc.Name,
						"arguments": tc.Args,
					},
				})
				result.FinishReason = model.FinishReasonToolCalls
			}

		case "reasoning":
			thinkingContent := c.extractReasoningFromOutput(outputItem)
			if thinkingContent != "" {
				encryptedSig := ""
				if len(outputItem.EncryptedContent) > 0 {
					// EncryptedContent can be either a string or an object
					// Try to parse as object first, fall back to string
					var ec encryptedContent
					if json.Unmarshal(outputItem.EncryptedContent, &ec) == nil && ec.Data != "" {
						encryptedSig = ec.Data
					} else {
						// Try as plain string
						var str string
						if json.Unmarshal(outputItem.EncryptedContent, &str) == nil {
							encryptedSig = str
						}
					}
				}
				result.Thinking = &model.ThinkingBlock{
					Content:   thinkingContent,
					Signature: encryptedSig,
				}
			}
		}
	}

	if len(parts) > 0 {
		result.Content = &model.Content{
			Parts: parts,
			Role:  a2a.MessageRoleAgent,
		}
	}

	return result, nil
}

// extractTextFromOutput extracts text from a message output item.
func (c *Client) extractTextFromOutput(item outputItem) string {
	if item.Content == nil {
		return ""
	}

	contentArray, ok := item.Content.([]any)
	if !ok {
		return ""
	}

	var text strings.Builder
	for _, part := range contentArray {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		partType, _ := partMap["type"].(string)
		if partType == "output_text" {
			if t, ok := partMap["text"].(string); ok {
				text.WriteString(t)
			}
		}
	}

	return text.String()
}

// parseFunctionCall parses a function_call output item.
func (c *Client) parseFunctionCall(item outputItem) (*tool.ToolCall, error) {
	if item.Name == "" {
		return nil, fmt.Errorf("function_call name is empty")
	}

	var args map[string]any
	if item.Arguments != "" {
		if err := json.Unmarshal([]byte(item.Arguments), &args); err != nil {
			return nil, fmt.Errorf("failed to parse function arguments: %w", err)
		}
	} else {
		args = make(map[string]any)
	}

	callID := item.CallID
	if callID == "" {
		callID = item.ID
	}

	return &tool.ToolCall{
		ID:   callID,
		Name: item.Name,
		Args: args,
	}, nil
}

// extractReasoningFromOutput extracts reasoning content from a reasoning output item.
func (c *Client) extractReasoningFromOutput(item outputItem) string {
	if len(item.Summary) == 0 {
		return ""
	}

	var text strings.Builder
	for _, summaryItem := range item.Summary {
		if summaryItem.Type == "summary_text" && summaryItem.Text != "" {
			text.WriteString(summaryItem.Text)
			text.WriteString("\n")
		}
	}

	return strings.TrimSpace(text.String())
}

// isReasoningModel checks if the current model supports reasoning.
func (c *Client) isReasoningModel() bool {
	modelLower := strings.ToLower(c.modelName)

	// Check exact matches
	if modelLower == "o1" || modelLower == "o3" || modelLower == "o4" || modelLower == "gpt-5" {
		return true
	}

	// Check prefixes
	reasoningPrefixes := []string{"o1-", "o3-", "o4-", "gpt-5-"}
	for _, prefix := range reasoningPrefixes {
		if strings.HasPrefix(modelLower, prefix) {
			return true
		}
	}

	return false
}

// mapBudgetToEffort maps thinking budget tokens to OpenAI reasoning effort.
func (c *Client) mapBudgetToEffort(budget int) string {
	if budget <= reasoningEffortLowThreshold {
		return "low"
	}
	if budget <= reasoningEffortMediumThreshold {
		return "medium"
	}
	return "high"
}

// getString safely extracts a string from a map.
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// API types

type responsesRequest struct {
	Model           string           `json:"model"`
	Input           any              `json:"input,omitempty"`
	Instructions    string           `json:"instructions,omitempty"`
	MaxOutputTokens *int             `json:"max_output_tokens,omitempty"`
	Temperature     *float64         `json:"temperature,omitempty"`
	Tools           []apiTool        `json:"tools,omitempty"`
	ToolChoice      any              `json:"tool_choice,omitempty"`
	Reasoning       *reasoningConfig `json:"reasoning,omitempty"`
	Include         []string         `json:"include,omitempty"`
	Stream          bool             `json:"stream,omitempty"`
	Text            *textFormat      `json:"text,omitempty"`
}

type reasoningConfig struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type textFormat struct {
	Format *jsonSchemaFormat `json:"format,omitempty"`
}

type jsonSchemaFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type inputItem struct {
	Type      string           `json:"type"`
	ID        string           `json:"id,omitempty"`
	Role      string           `json:"role,omitempty"`
	Content   []map[string]any `json:"content,omitempty"`
	CallID    string           `json:"call_id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Arguments string           `json:"arguments,omitempty"`
	Output    *string          `json:"output,omitempty"`
}

type apiTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type responsesResponse struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	CreatedAt         int64              `json:"created_at"`
	Status            string             `json:"status"`
	Error             *apiError          `json:"error,omitempty"`
	IncompleteDetails *incompleteDetails `json:"incomplete_details,omitempty"`
	Model             string             `json:"model"`
	Output            []outputItem       `json:"output"`
	Reasoning         *reasoningResponse `json:"reasoning,omitempty"`
	Usage             apiUsage           `json:"usage"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

type incompleteDetails struct {
	Reason string `json:"reason,omitempty"`
}

type outputItem struct {
	Type             string          `json:"type"`
	ID               string          `json:"id,omitempty"`
	Status           string          `json:"status,omitempty"`
	Role             string          `json:"role,omitempty"`
	Content          any             `json:"content,omitempty"`
	Summary          []summaryItem   `json:"summary,omitempty"`
	EncryptedContent json.RawMessage `json:"encrypted_content,omitempty"` // Can be string or encryptedContent object
	CallID           string          `json:"call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Arguments        string          `json:"arguments,omitempty"`
}

type summaryItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type encryptedContent struct {
	Type  string `json:"type"`
	Data  string `json:"data"`
	IV    string `json:"iv"`
	Tag   string `json:"tag"`
	KeyID string `json:"key_id"`
}

type reasoningResponse struct {
	Effort  *string `json:"effort,omitempty"`
	Summary *string `json:"summary,omitempty"`
}

type apiUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Ensure Client implements model.LLM
var _ model.LLM = (*Client)(nil)
