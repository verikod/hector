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

package model

import (
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
	"github.com/verikod/hector/pkg/tool"
)

// StreamingAggregator aggregates partial streaming responses.
//
// This is aligned with ADK-Go's streamingResponseAggregator.
// It accumulates content from partial responses and generates:
//   - Partial responses for real-time UI updates (Partial=true)
//   - Aggregated response for session persistence (Partial=false)
//
// Usage:
//
//	aggregator := NewStreamingAggregator()
//	for chunk := range provider.Stream(ctx, req) {
//	    for resp, err := range aggregator.ProcessChunk(chunk) {
//	        yield(resp, err)
//	    }
//	}
//	if final := aggregator.Close(); final != nil {
//	    yield(final, nil)
//	}
type StreamingAggregator struct {
	text         string
	thinkingText string
	response     *Response
	role         a2a.MessageRole
	toolCalls    []tool.ToolCall
	usage        *Usage
	finishReason FinishReason

	// thinkingID is the unique identifier for the thinking block
	thinkingID string

	// thinkingSignature for providers that support verification
	thinkingSignature string
}

// NewStreamingAggregator creates a new streaming aggregator.
func NewStreamingAggregator() *StreamingAggregator {
	return &StreamingAggregator{
		role: a2a.MessageRoleAgent,
	}
}

// ProcessTextDelta processes a text delta chunk.
// Returns a partial response for the UI.
func (s *StreamingAggregator) ProcessTextDelta(text string) iter.Seq2[*Response, error] {
	return func(yield func(*Response, error) bool) {
		if text == "" {
			return
		}

		// Accumulate text
		s.text += text

		// Yield partial response for UI
		resp := &Response{
			Content: &Content{
				Parts: []a2a.Part{a2a.TextPart{Text: text}},
				Role:  s.role,
			},
			Partial: true,
		}
		s.response = resp

		yield(resp, nil)
	}
}

// ProcessThinkingDelta processes a thinking delta chunk.
// Returns a partial response with thinking metadata.
func (s *StreamingAggregator) ProcessThinkingDelta(thinking string) iter.Seq2[*Response, error] {
	return func(yield func(*Response, error) bool) {
		if thinking == "" {
			return
		}

		// Generate thinking ID on first delta
		if s.thinkingID == "" {
			s.thinkingID = "thinking_" + uuid.NewString()[:8]
		}

		// Accumulate thinking
		s.thinkingText += thinking

		// Yield partial response with thinking delta (for UI that shows thinking)
		resp := &Response{
			Content: &Content{
				Parts: []a2a.Part{}, // No text parts for thinking
				Role:  s.role,
			},
			Partial: true,
			Thinking: &ThinkingBlock{
				ID:      s.thinkingID,
				Content: thinking, // Delta only
			},
		}
		s.response = resp

		yield(resp, nil)
	}
}

// ProcessThinkingComplete processes a completed thinking block with signature.
func (s *StreamingAggregator) ProcessThinkingComplete(content, signature string) {
	// Generate ID if we don't have one yet (non-streamed thinking)
	if s.thinkingID == "" {
		s.thinkingID = "thinking_" + uuid.NewString()[:8]
	}
	s.thinkingText = content
	s.thinkingSignature = signature
}

// ThinkingText returns the accumulated thinking text.
func (s *StreamingAggregator) ThinkingText() string {
	return s.thinkingText
}

// ProcessToolCall processes a complete tool call.
// Returns a partial response with the tool call.
func (s *StreamingAggregator) ProcessToolCall(tc tool.ToolCall) iter.Seq2[*Response, error] {
	return func(yield func(*Response, error) bool) {
		// Accumulate tool calls
		s.toolCalls = append(s.toolCalls, tc)

		// Yield partial response with tool call
		resp := &Response{
			Content: &Content{
				Parts: []a2a.Part{
					a2a.DataPart{
						Data: map[string]any{
							"type":      "tool_use",
							"id":        tc.ID,
							"name":      tc.Name,
							"arguments": tc.Args,
						},
					},
				},
				Role: s.role,
			},
			Partial:   true,
			ToolCalls: []tool.ToolCall{tc},
		}
		s.response = resp

		yield(resp, nil)
	}
}

// SetUsage sets the usage statistics (typically from the done event).
func (s *StreamingAggregator) SetUsage(usage *Usage) {
	s.usage = usage
}

// SetFinishReason sets the finish reason.
func (s *StreamingAggregator) SetFinishReason(reason FinishReason) {
	s.finishReason = reason
}

// Close generates the final aggregated response.
// This should be called after all streaming chunks are processed.
// The returned response has Partial=false and is suitable for persistence.
func (s *StreamingAggregator) Close() *Response {
	return s.createAggregatedResponse()
}

func (s *StreamingAggregator) createAggregatedResponse() *Response {
	// Only create aggregated if we have accumulated content
	if s.text == "" && s.thinkingText == "" && len(s.toolCalls) == 0 {
		return nil
	}

	var parts []a2a.Part

	// Add text part if we accumulated text
	if s.text != "" {
		parts = append(parts, a2a.TextPart{Text: s.text})
	}

	// Build the aggregated response
	resp := &Response{
		Content: &Content{
			Parts: parts,
			Role:  s.role,
		},
		Partial:      false, // CRITICAL: This is the final aggregated response
		TurnComplete: true,
		ToolCalls:    s.toolCalls,
		Usage:        s.usage,
		FinishReason: s.finishReason,
	}

	// Add thinking block if we have one
	if s.thinkingText != "" {
		resp.Thinking = &ThinkingBlock{
			ID:        s.thinkingID,
			Content:   s.thinkingText,
			Signature: s.thinkingSignature,
		}
	}

	// Clear state
	s.clear()

	return resp
}

func (s *StreamingAggregator) clear() {
	s.text = ""
	s.thinkingText = ""
	s.thinkingID = ""
	s.thinkingSignature = ""
	s.response = nil
	s.toolCalls = nil
	s.usage = nil
	s.finishReason = ""
}
