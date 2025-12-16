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

package llmagent

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/instruction"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/tool"
)

// RequestProcessor transforms an LLM request before it's sent to the model.
// Processors are called in order, each receiving the modified request from the previous.
// Return an error to abort the request pipeline.
type RequestProcessor func(ctx ProcessorContext, req *model.Request) error

// ResponseProcessor transforms an LLM response after it comes back from the model.
// Processors are called in order, each receiving the modified response from the previous.
// Return an error to abort the response pipeline.
type ResponseProcessor func(ctx ProcessorContext, req *model.Request, resp *model.Response) error

// ProcessorContext provides context for processors.
// It extends agent.InvocationContext with processor-specific functionality.
type ProcessorContext interface {
	agent.InvocationContext

	// Agent returns the LLM agent being processed.
	LLMAgent() *llmAgent

	// Tools returns all available tools for this agent.
	Tools() []tool.Tool

	// ToolDefinitions returns tool definitions for the LLM.
	ToolDefinitions() []tool.Definition
}

// processorContext implements ProcessorContext.
type processorContext struct {
	agent.InvocationContext
	llmAgent        *llmAgent
	tools           []tool.Tool
	toolDefinitions []tool.Definition
}

func newProcessorContext(ctx agent.InvocationContext, a *llmAgent) *processorContext {
	return &processorContext{
		InvocationContext: ctx,
		llmAgent:          a,
	}
}

func (c *processorContext) LLMAgent() *llmAgent {
	return c.llmAgent
}

func (c *processorContext) Tools() []tool.Tool {
	if c.tools == nil {
		c.tools = c.llmAgent.collectTools(c.InvocationContext)
	}
	return c.tools
}

func (c *processorContext) ToolDefinitions() []tool.Definition {
	if c.toolDefinitions == nil {
		c.toolDefinitions = c.llmAgent.collectToolDefinitions(c.InvocationContext)
	}
	return c.toolDefinitions
}

// Pipeline manages request and response processors.
type Pipeline struct {
	requestProcessors  []RequestProcessor
	responseProcessors []ResponseProcessor
}

// NewPipeline creates a new processor pipeline with default processors.
func NewPipeline() *Pipeline {
	return &Pipeline{
		requestProcessors:  DefaultRequestProcessors(),
		responseProcessors: DefaultResponseProcessors(),
	}
}

// NewCustomPipeline creates a pipeline with custom processors.
func NewCustomPipeline(reqProcessors []RequestProcessor, respProcessors []ResponseProcessor) *Pipeline {
	return &Pipeline{
		requestProcessors:  reqProcessors,
		responseProcessors: respProcessors,
	}
}

// ProcessRequest runs all request processors in order.
func (p *Pipeline) ProcessRequest(ctx ProcessorContext, req *model.Request) error {
	for i, processor := range p.requestProcessors {
		if err := processor(ctx, req); err != nil {
			return fmt.Errorf("request processor %d failed: %w", i, err)
		}
	}
	return nil
}

// ProcessResponse runs all response processors in order.
func (p *Pipeline) ProcessResponse(ctx ProcessorContext, req *model.Request, resp *model.Response) error {
	for i, processor := range p.responseProcessors {
		if err := processor(ctx, req, resp); err != nil {
			return fmt.Errorf("response processor %d failed: %w", i, err)
		}
	}
	return nil
}

// AddRequestProcessor appends a request processor to the pipeline.
func (p *Pipeline) AddRequestProcessor(processor RequestProcessor) {
	p.requestProcessors = append(p.requestProcessors, processor)
}

// AddResponseProcessor appends a response processor to the pipeline.
func (p *Pipeline) AddResponseProcessor(processor ResponseProcessor) {
	p.responseProcessors = append(p.responseProcessors, processor)
}

// PrependRequestProcessor adds a request processor at the beginning.
func (p *Pipeline) PrependRequestProcessor(processor RequestProcessor) {
	p.requestProcessors = append([]RequestProcessor{processor}, p.requestProcessors...)
}

// PrependResponseProcessor adds a response processor at the beginning.
func (p *Pipeline) PrependResponseProcessor(processor ResponseProcessor) {
	p.responseProcessors = append([]ResponseProcessor{processor}, p.responseProcessors...)
}

// ============================================================================
// Default Request Processors
// ============================================================================

// DefaultRequestProcessors returns the standard request processor chain.
// Order matters - processors are executed sequentially.
func DefaultRequestProcessors() []RequestProcessor {
	return []RequestProcessor{
		ConfigRequestProcessor,        // 1. Apply generate config and output schema
		InstructionRequestProcessor,   // 2. Resolve instruction templates
		ToolsRequestProcessor,         // 3. Collect and add tool definitions
		ContentsRequestProcessor,      // 4. Build conversation history
		RAGContextRequestProcessor,    // 5. Inject RAG context (after contents)
		TransferToolsRequestProcessor, // 6. Add agent transfer tools
	}
}

// ConfigRequestProcessor applies the agent's generate config to the request.
func ConfigRequestProcessor(ctx ProcessorContext, req *model.Request) error {
	a := ctx.LLMAgent()
	if a == nil {
		return nil
	}

	// Deep copy generate config to prevent shared state between requests
	if a.generateConfig != nil {
		req.Config = a.generateConfig.Clone()
	}

	// Apply output schema if configured
	if a.outputSchema != nil {
		if req.Config == nil {
			req.Config = &model.GenerateConfig{}
		}
		req.Config.ResponseSchema = a.outputSchema
		req.Config.ResponseMIMEType = "application/json"
	}

	return nil
}

// InstructionRequestProcessor resolves instruction templates and sets system instruction.
func InstructionRequestProcessor(ctx ProcessorContext, req *model.Request) error {
	a := ctx.LLMAgent()
	if a == nil {
		return nil
	}

	var parts []string

	// Global instruction (from root agent in multi-agent setup)
	if a.globalInstructionProvider != nil {
		globalInst, err := a.globalInstructionProvider(ctx)
		if err != nil {
			return fmt.Errorf("global instruction provider: %w", err)
		}
		if globalInst != "" {
			parts = append(parts, globalInst)
		}
	} else if a.globalInstruction != "" {
		resolved, err := instruction.InjectState(ctx, a.globalInstruction)
		if err != nil {
			return fmt.Errorf("global instruction template: %w", err)
		}
		if resolved != "" {
			parts = append(parts, resolved)
		}
	}

	// Agent instruction
	if a.instructionProvider != nil {
		inst, err := a.instructionProvider(ctx)
		if err != nil {
			return fmt.Errorf("instruction provider: %w", err)
		}
		if inst != "" {
			parts = append(parts, inst)
		}
	} else if a.instruction != "" {
		resolved, err := instruction.InjectState(ctx, a.instruction)
		if err != nil {
			return fmt.Errorf("instruction template: %w", err)
		}
		if resolved != "" {
			parts = append(parts, resolved)
		}
	}

	// Completion instruction from reasoning config
	completionInst := a.buildCompletionInstruction()
	if completionInst != "" {
		parts = append(parts, completionInst)
	}

	req.SystemInstruction = joinInstructions(parts)
	return nil
}

// ToolsRequestProcessor collects tool definitions and adds them to the request.
func ToolsRequestProcessor(ctx ProcessorContext, req *model.Request) error {
	req.Tools = ctx.ToolDefinitions()
	return nil
}

// ContentsRequestProcessor builds the conversation history from session events.
// Following adk-go pattern: ALWAYS reads from session events on every LLM call.
// This is the source of truth for conversation history.
func ContentsRequestProcessor(ctx ProcessorContext, req *model.Request) error {
	a := ctx.LLMAgent()
	if a == nil {
		return nil
	}

	// ALWAYS build messages from session events (adk-go pattern)
	// The session contains all persisted events including tool calls/results
	req.Messages = a.buildMessages(ctx)

	slog.Debug("ContentsRequestProcessor: built messages from session",
		"message_count", len(req.Messages),
		"agent", a.Name())
	return nil
}

// RAGContextRequestProcessor injects relevant RAG context into the messages.
// This runs AFTER ContentsRequestProcessor to inject context based on user query.
func RAGContextRequestProcessor(ctx ProcessorContext, req *model.Request) error {
	a := ctx.LLMAgent()
	if a == nil || a.contextProvider == nil {
		return nil
	}

	// Extract the last user query from messages
	query := extractLastUserQuery(req.Messages)
	if query == "" {
		slog.Debug("RAGContextRequestProcessor: no user query found, skipping",
			"agent", a.Name())
		return nil
	}

	// Query the context provider
	ragContext, err := a.contextProvider(ctx, query)
	if err != nil {
		slog.Warn("RAGContextRequestProcessor: failed to get context",
			"agent", a.Name(),
			"error", err)
		return nil // Don't fail the request, just skip context injection
	}

	if ragContext == "" {
		slog.Debug("RAGContextRequestProcessor: no relevant context found",
			"agent", a.Name(),
			"query", query)
		return nil
	}

	// Inject context directly into SystemInstruction for stronger adherence
	// This ensures the model treats the context as authoritative knowledge
	if req.SystemInstruction != "" {
		req.SystemInstruction += "\n\n"
	}
	req.SystemInstruction += ragContext

	// Log preview for verification
	preview := ""
	if len(ragContext) > 200 {
		preview = ragContext[:200] + "..."
	} else {
		preview = ragContext
	}
	slog.Debug("RAGContextRequestProcessor: injected context",
		"agent", a.Name(),
		"query", query,
		"context_length", len(ragContext),
		"preview", preview)
	return nil
}

// extractLastUserQuery extracts the most recent user query from messages.
func extractLastUserQuery(messages []*a2a.Message) string {
	// Iterate backwards to find the last user message
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == a2a.MessageRoleUser {
			for _, part := range msg.Parts {
				if text, ok := part.(a2a.TextPart); ok && text.Text != "" {
					return text.Text
				}
			}
		}
	}
	return ""
}

// TransferToolsRequestProcessor adds agent transfer tools based on sub-agents.
func TransferToolsRequestProcessor(ctx ProcessorContext, req *model.Request) error {
	a := ctx.LLMAgent()
	if a == nil {
		return nil
	}

	// Get sub-agents that can be transferred to
	subAgents := a.SubAgents()
	if len(subAgents) == 0 {
		return nil
	}

	// Add transfer tools for each sub-agent
	for _, sub := range subAgents {
		transferTool := tool.Definition{
			Name:        "transfer_to_" + sub.Name(),
			Description: fmt.Sprintf("Transfer control to the %s agent. %s", sub.Name(), sub.Description()),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"request": map[string]any{
						"type":        "string",
						"description": "What you want the " + sub.Name() + " agent to do",
					},
				},
				"required": []string{"request"},
			},
		}
		req.Tools = append(req.Tools, transferTool)
	}

	return nil
}

// ============================================================================
// Default Response Processors
// ============================================================================

// DefaultResponseProcessors returns the standard response processor chain.
func DefaultResponseProcessors() []ResponseProcessor {
	return []ResponseProcessor{
		ThinkingResponseProcessor, // 1. Process thinking blocks
		LoggingResponseProcessor,  // 2. Log response details (debug)
	}
}

// ThinkingResponseProcessor processes thinking blocks in the response.
func ThinkingResponseProcessor(ctx ProcessorContext, req *model.Request, resp *model.Response) error {
	// Thinking is already handled in the response from the model
	// This processor can be extended to:
	// - Extract planning steps
	// - Validate thinking signatures for multi-turn
	// - Transform thinking format
	return nil
}

// LoggingResponseProcessor logs response details for debugging.
func LoggingResponseProcessor(ctx ProcessorContext, req *model.Request, resp *model.Response) error {
	if resp == nil {
		return nil
	}

	slog.Debug("LLM response processed",
		"agent", ctx.AgentName(),
		"finish_reason", resp.FinishReason,
		"has_tool_calls", resp.HasToolCalls(),
		"partial", resp.Partial,
	)

	return nil
}

// ============================================================================
// Custom Processor Helpers
// ============================================================================

// AuthRequestProcessor creates a processor that adds authentication tokens.
// The token is stored in Config.Metadata["Authorization"] for LLM implementations to use.
//
// Example usage:
//
//	authProcessor := AuthRequestProcessor(func(ctx agent.ReadonlyContext) (string, error) {
//	    // Get token from session state, environment, or secret manager
//	    token, _ := ctx.ReadonlyState().Get("api_token")
//	    return token.(string), nil
//	})
func AuthRequestProcessor(tokenProvider func(ctx agent.ReadonlyContext) (string, error)) RequestProcessor {
	return func(ctx ProcessorContext, req *model.Request) error {
		token, err := tokenProvider(ctx)
		if err != nil {
			return fmt.Errorf("failed to get auth token: %w", err)
		}

		if req.Config == nil {
			req.Config = &model.GenerateConfig{}
		}

		// Store token in metadata for LLM implementation to use
		if req.Config.Metadata == nil {
			req.Config.Metadata = make(map[string]string)
		}
		req.Config.Metadata["Authorization"] = "Bearer " + token

		return nil
	}
}

// ContentFilterRequestProcessor creates a processor that filters/transforms content.
func ContentFilterRequestProcessor(filter func(content string) string) RequestProcessor {
	return func(ctx ProcessorContext, req *model.Request) error {
		// Filter system instruction
		if req.SystemInstruction != "" {
			req.SystemInstruction = filter(req.SystemInstruction)
		}

		// Filter message content
		for _, msg := range req.Messages {
			for i, part := range msg.Parts {
				if tp, ok := part.(a2a.TextPart); ok {
					msg.Parts[i] = a2a.TextPart{Text: filter(tp.Text)}
				}
			}
		}

		return nil
	}
}

// ValidationResponseProcessor creates a processor that validates responses.
func ValidationResponseProcessor(validator func(resp *model.Response) error) ResponseProcessor {
	return func(ctx ProcessorContext, req *model.Request, resp *model.Response) error {
		return validator(resp)
	}
}

// ============================================================================
// Utility Functions
// ============================================================================

// ============================================================================
// Content Processor (Message History Processing)
// ============================================================================

// ContentProcessor handles message history processing for LLM context.
// It handles tool call/result pairing, foreign agent message conversion, and auth filtering.
//
// The processor is model-aware: different providers have different requirements
// for how tool calls and results should be formatted:
//   - OpenAI: Tool results are separate function_call_output items
//   - Anthropic/Gemini: Tool results must be paired with tool_use in same message
type ContentProcessor struct {
	agentName string
	provider  model.Provider
}

// NewContentProcessor creates a new content processor for the given agent and provider.
func NewContentProcessor(agentName string, provider model.Provider) *ContentProcessor {
	return &ContentProcessor{
		agentName: agentName,
		provider:  provider,
	}
}

// Process handles tool call/result formatting for LLM protocols.
//
// This method is model-aware:
//   - OpenAI/Ollama: Returns messages as-is (tool results are separate function_call_output items)
//   - Anthropic: Returns messages as-is (tool_use in assistant msg, tool_result in next user msg)
//   - Gemini: May need special handling (currently returns as-is)
//
// The Flow already creates the correct message structure for all providers:
//   - Assistant message with tool_use blocks
//   - User message with tool_result blocks
//
// This processor primarily exists for future model-specific transformations
// and to filter out any malformed messages.
func (p *ContentProcessor) Process(messages []*a2a.Message) []*a2a.Message {
	// All providers now use the standard format that Flow creates:
	// - tool_use in assistant message
	// - tool_result in subsequent user message
	// No transformation needed
	return messages
}

// pairToolCallsWithResults pairs tool_use with tool_result for Anthropic/Gemini.
//
//nolint:unused // Reserved for future use
func (p *ContentProcessor) pairToolCallsWithResults(messages []*a2a.Message) []*a2a.Message {
	// Collect tool results by ID for pairing
	toolCallToResult := make(map[string]a2a.Part)
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if dp, ok := part.(a2a.DataPart); ok {
				if typeVal, _ := dp.Data["type"].(string); typeVal == "tool_result" {
					if toolCallID, _ := dp.Data["tool_call_id"].(string); toolCallID != "" {
						toolCallToResult[toolCallID] = part
					}
				}
			}
		}
	}

	// Rebuild messages with proper tool call/result pairing
	var result []*a2a.Message
	for _, msg := range messages {
		var newParts []a2a.Part
		for _, part := range msg.Parts {
			// Skip standalone tool results (they'll be paired with tool calls)
			if dp, ok := part.(a2a.DataPart); ok {
				if typeVal, _ := dp.Data["type"].(string); typeVal == "tool_result" {
					continue
				}

				// For tool calls, immediately add the result if available
				if typeVal, _ := dp.Data["type"].(string); typeVal == "tool_use" {
					newParts = append(newParts, part)
					if toolID, _ := dp.Data["id"].(string); toolID != "" {
						if resultPart, ok := toolCallToResult[toolID]; ok {
							newParts = append(newParts, resultPart)
						}
					}
					continue
				}
			}
			newParts = append(newParts, part)
		}

		if len(newParts) > 0 {
			result = append(result, a2a.NewMessage(msg.Role, newParts...))
		}
	}

	return result
}

// ConvertForeignAgentMessage converts messages from other agents to user context.
// In multi-agent setups, foreign agent messages need to be presented as user context.
func (p *ContentProcessor) ConvertForeignAgentMessage(msg *a2a.Message, author string) *a2a.Message {
	// Only convert messages from other agents (not self, not user)
	if author == p.agentName || author == agent.AuthorUser {
		return msg
	}

	// Helper to safely get string from map
	getString := func(m map[string]any, key string) string {
		if v, ok := m[key].(string); ok {
			return v
		}
		return ""
	}

	// Convert to user role with agent attribution
	// We flatten everything to text to ensure the model treats it as context
	// and to avoid protocol errors (e.g. tool calls inside user messages)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Message from agent '%s']:\n", author))

	for _, part := range msg.Parts {
		switch pt := part.(type) {
		case a2a.TextPart:
			sb.WriteString(pt.Text)
			sb.WriteString("\n")
		case a2a.DataPart:
			typeVal := getString(pt.Data, "type")
			switch typeVal {
			case "tool_use":
				name := getString(pt.Data, "name")
				args := pt.Data["arguments"]
				// Simple JSON representation of args
				argsJson := "{}"
				if args != nil {
					argsJson = fmt.Sprintf("%v", args)
				}
				sb.WriteString(fmt.Sprintf("Called tool %q with parameters: %s\n", name, argsJson))
			case "tool_result":
				name := getString(pt.Data, "tool_name")
				content := pt.Data["content"]
				contentStr := ""
				if s, ok := content.(string); ok {
					contentStr = s
				} else {
					contentStr = fmt.Sprintf("%v", content)
				}
				sb.WriteString(fmt.Sprintf("Tool %q returned result: %s\n", name, contentStr))
			default:
				// Generic data part
				sb.WriteString(fmt.Sprintf("Data: %v\n", pt.Data))
			}
		default:
			// Fallback for unknown parts
			sb.WriteString(fmt.Sprintf("%v\n", part))
		}
	}

	return a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: sb.String()})
}

// SanitizeMessage removes empty parts from a message, helpful for some models (e.g. Gemini).
func (p *ContentProcessor) SanitizeMessage(msg *a2a.Message) *a2a.Message {
	if msg == nil || len(msg.Parts) == 0 {
		return msg
	}

	var cleanParts []a2a.Part
	for _, part := range msg.Parts {
		if part == nil {
			continue
		}
		if tp, ok := part.(a2a.TextPart); ok && tp.Text == "" {
			continue
		}
		cleanParts = append(cleanParts, part)
	}

	if len(cleanParts) == len(msg.Parts) {
		return msg
	}
	return a2a.NewMessage(msg.Role, cleanParts...)
}

// RearrangeEventsForLatestFunctionResponse handles async function responses.
// If the latest event is a function response, it searches backward for the matching
// call, removes all intervening events, and ensures proper call/response pairing.
//
// This follows adk-go's rearrangeEventsForLatestFunctionResponse pattern for
// handling long-running/async tools where responses may arrive out of order.
func (p *ContentProcessor) RearrangeEventsForLatestFunctionResponse(events []*agent.Event) ([]*agent.Event, error) {
	if len(events) < 2 {
		return events, nil
	}

	lastEvent := events[len(events)-1]
	lastResponses := p.listFunctionResponsesFromEvent(lastEvent)

	// No need to process if the latest event is not a function response
	if len(lastResponses) == 0 {
		return events, nil
	}

	// Create response ID set
	responseIDs := make(map[string]struct{})
	for _, res := range lastResponses {
		responseIDs[res.ID] = struct{}{}
	}

	// Check if it's already in the correct position
	prevEvent := events[len(events)-2]
	prevCalls := p.listFunctionCallsFromEvent(prevEvent)
	if len(prevCalls) > 0 {
		for _, call := range prevCalls {
			if _, found := responseIDs[call.ID]; found {
				// Already matched - nothing to do
				return events, nil
			}
		}
	}

	// Search backward for matching function call event
	functionCallEventIdx := -1
	var allCallIDsFromMatchingEvent map[string]struct{}

	for idx := len(events) - 2; idx >= 0; idx-- {
		event := events[idx]
		calls := p.listFunctionCallsFromEvent(event)

		if len(calls) > 0 {
			for _, call := range calls {
				if _, found := responseIDs[call.ID]; found {
					functionCallEventIdx = idx
					allCallIDsFromMatchingEvent = make(map[string]struct{})
					for _, c := range calls {
						allCallIDsFromMatchingEvent[c.ID] = struct{}{}
					}
					break
				}
			}
			if functionCallEventIdx != -1 {
				break
			}
		}
	}

	if functionCallEventIdx == -1 {
		return nil, fmt.Errorf("no function call event found for function responses")
	}

	// Collect response events between call and last response
	var responseEventsToMerge []*agent.Event
	for i := functionCallEventIdx + 1; i < len(events)-1; i++ {
		event := events[i]
		responses := p.listFunctionResponsesFromEvent(event)
		if len(responses) == 0 {
			continue
		}

		isRelated := false
		for _, res := range responses {
			if _, exists := allCallIDsFromMatchingEvent[res.ID]; exists {
				isRelated = true
				break
			}
		}

		if isRelated {
			responseEventsToMerge = append(responseEventsToMerge, event)
		}
	}

	responseEventsToMerge = append(responseEventsToMerge, events[len(events)-1])

	// Build result: events up to and including call, then merged response
	resultEvents := events[:functionCallEventIdx+1]
	mergedEvent := p.mergeFunctionResponseEvents(responseEventsToMerge)
	resultEvents = append(resultEvents, mergedEvent)

	return resultEvents, nil
}

// RearrangeEventsForFunctionResponsesInHistory reorganizes entire event history
// to ensure every function call is immediately followed by its response.
//
// This follows adk-go's rearrangeEventsForFunctionResponsesInHistory pattern.
func (p *ContentProcessor) RearrangeEventsForFunctionResponsesInHistory(events []*agent.Event) ([]*agent.Event, error) {
	if len(events) < 2 {
		return events, nil
	}

	// Map call IDs to their response event indices
	callIDToResponseEventIndex := make(map[string]int)
	for i, event := range events {
		responses := p.listFunctionResponsesFromEvent(event)
		for _, res := range responses {
			callIDToResponseEventIndex[res.ID] = i
		}
	}

	// Rebuild event list
	var resultEvents []*agent.Event
	for _, event := range events {
		// Skip response events - they'll be handled with their calls
		if len(p.listFunctionResponsesFromEvent(event)) > 0 {
			continue
		}

		calls := p.listFunctionCallsFromEvent(event)
		if len(calls) == 0 {
			// Regular event - just append
			resultEvents = append(resultEvents, event)
		} else {
			// Function call event - append it and find responses
			resultEvents = append(resultEvents, event)

			// Find unique response event indices
			responseEventIndices := make(map[int]struct{})
			for _, call := range calls {
				if idx, found := callIDToResponseEventIndex[call.ID]; found {
					responseEventIndices[idx] = struct{}{}
				}
			}

			if len(responseEventIndices) == 0 {
				continue
			}

			if len(responseEventIndices) == 1 {
				for idx := range responseEventIndices {
					resultEvents = append(resultEvents, events[idx])
				}
			} else {
				// Multiple response events - merge them
				var eventsToMerge []*agent.Event
				for idx := range responseEventIndices {
					eventsToMerge = append(eventsToMerge, events[idx])
				}
				mergedEvent := p.mergeFunctionResponseEvents(eventsToMerge)
				resultEvents = append(resultEvents, mergedEvent)
			}
		}
	}

	return resultEvents, nil
}

// FunctionCallInfo represents a function call extracted from an event.
type FunctionCallInfo struct {
	ID   string
	Name string
	Args map[string]any
}

// FunctionResponseInfo represents a function response extracted from an event.
type FunctionResponseInfo struct {
	ID      string
	Name    string
	Content string
}

// listFunctionCallsFromEvent extracts function calls from an event.
func (p *ContentProcessor) listFunctionCallsFromEvent(e *agent.Event) []FunctionCallInfo {
	var calls []FunctionCallInfo

	// Check ToolCalls field
	for _, tc := range e.ToolCalls {
		calls = append(calls, FunctionCallInfo{
			ID:   tc.ID,
			Name: tc.Name,
			Args: tc.Args,
		})
	}

	// Also check message parts
	if e.Message != nil {
		for _, part := range e.Message.Parts {
			if dp, ok := part.(a2a.DataPart); ok {
				if typeVal, _ := dp.Data["type"].(string); typeVal == "tool_use" {
					calls = append(calls, FunctionCallInfo{
						ID:   getString(dp.Data, "id"),
						Name: getString(dp.Data, "name"),
						Args: getMap(dp.Data, "arguments"),
					})
				}
			}
		}
	}

	return calls
}

// listFunctionResponsesFromEvent extracts function responses from an event.
func (p *ContentProcessor) listFunctionResponsesFromEvent(e *agent.Event) []FunctionResponseInfo {
	var responses []FunctionResponseInfo

	// Check ToolResults field
	for _, tr := range e.ToolResults {
		responses = append(responses, FunctionResponseInfo{
			ID:      tr.ToolCallID,
			Content: tr.Content,
		})
	}

	// Also check message parts
	if e.Message != nil {
		for _, part := range e.Message.Parts {
			if dp, ok := part.(a2a.DataPart); ok {
				if typeVal, _ := dp.Data["type"].(string); typeVal == "tool_result" {
					responses = append(responses, FunctionResponseInfo{
						ID:      getString(dp.Data, "tool_call_id"),
						Name:    getString(dp.Data, "tool_name"),
						Content: getString(dp.Data, "content"),
					})
				}
			}
		}
	}

	return responses
}

// mergeFunctionResponseEvents merges multiple function response events into one.
func (p *ContentProcessor) mergeFunctionResponseEvents(events []*agent.Event) *agent.Event {
	if len(events) == 0 {
		return nil
	}
	if len(events) == 1 {
		return events[0]
	}

	// Use first event as base
	merged := *events[0]
	merged.ToolResults = append([]agent.ToolResultState{}, events[0].ToolResults...)

	// Merge tool results from subsequent events
	seenIDs := make(map[string]bool)
	for _, tr := range merged.ToolResults {
		seenIDs[tr.ToolCallID] = true
	}

	for _, event := range events[1:] {
		for _, tr := range event.ToolResults {
			if !seenIDs[tr.ToolCallID] {
				merged.ToolResults = append(merged.ToolResults, tr)
				seenIDs[tr.ToolCallID] = true
			}
		}

		// Merge actions
		if event.Actions.SkipSummarization {
			merged.Actions.SkipSummarization = true
		}
		if event.Actions.TransferToAgent != "" {
			merged.Actions.TransferToAgent = event.Actions.TransferToAgent
		}
		if event.Actions.Escalate {
			merged.Actions.Escalate = true
		}
		for k, v := range event.Actions.StateDelta {
			if merged.Actions.StateDelta == nil {
				merged.Actions.StateDelta = make(map[string]any)
			}
			merged.Actions.StateDelta[k] = v
		}
	}

	return &merged
}

// getString safely extracts a string from a map.
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getMap safely extracts a map from a map.
func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

// FilterAuthEvents removes authentication-related events from the history.
func (p *ContentProcessor) FilterAuthEvents(messages []*a2a.Message) []*a2a.Message {
	var result []*a2a.Message
	for _, msg := range messages {
		// Filter out auth-related and tool approval parts
		var filteredParts []a2a.Part
		for _, part := range msg.Parts {
			if dp, ok := part.(a2a.DataPart); ok {
				typeVal, _ := dp.Data["type"].(string)
				// Skip auth events and tool approval messages (HITL approval/denial decisions)
				// Tool approval messages are metadata for the flow, not conversation content
				if typeVal == "auth" || typeVal == "auth_response" || typeVal == "tool_approval" {
					continue
				}
			}
			filteredParts = append(filteredParts, part)
		}

		if len(filteredParts) > 0 {
			result = append(result, a2a.NewMessage(msg.Role, filteredParts...))
		}
	}
	return result
}

// ============================================================================
// Utility Functions
// ============================================================================

// collectTools returns all tools available to the agent.
func (a *llmAgent) collectTools(ctx agent.InvocationContext) []tool.Tool {
	var tools []tool.Tool

	// Control tools
	tools = append(tools, a.getControlTools()...)

	// Static tools
	tools = append(tools, a.tools...)

	// Toolset tools
	for _, ts := range a.toolsets {
		tsTools, err := ts.Tools(ctx)
		if err != nil {
			slog.Warn("Toolset failed to provide tools",
				"toolset", ts.Name(),
				"agent", a.Name(),
				"error", err)
			continue
		}
		tools = append(tools, tsTools...)
	}

	return tools
}
