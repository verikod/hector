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

package remoteagent

import (
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"

	"github.com/kadirpekel/hector/pkg/agent"
)

// Config configures a remote A2A agent.
type Config struct {
	// Name is the local name for this remote agent.
	// Required.
	Name string

	// DisplayName is the human-readable name (optional).
	DisplayName string

	// Description describes what this remote agent does.
	Description string

	// URL is the base URL of the remote A2A server.
	// Can be used instead of AgentCard/AgentCardSource.
	// Example: "http://localhost:9000"
	URL string

	// AgentCard provides the agent card directly.
	// Takes precedence over URL and AgentCardSource.
	AgentCard *a2a.AgentCard

	// AgentCardSource is a URL or file path to resolve the agent card.
	// Used if AgentCard is not provided.
	// Example: "http://localhost:9000/.well-known/agent-card.json" or "./agent-card.json"
	AgentCardSource string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string

	// Timeout is the request timeout. Default: 5m.
	Timeout time.Duration

	// Streaming controls whether to use SSE streaming (message/stream) or
	// blocking mode (message/send). Default: true (streaming enabled).
	Streaming bool

	// MessageSendConfig is attached to every message sent to the remote agent.
	MessageSendConfig *a2a.MessageSendConfig
}

// a2aAgent is the internal implementation of a remote A2A agent.
type a2aAgent struct {
	cfg          Config
	resolvedCard *a2a.AgentCard
}

// NewA2A creates a remote A2A agent.
//
// Remote A2A agents communicate with agents running in different processes
// or on different hosts using the A2A (Agent-to-Agent) protocol.
//
// The remote agent can be:
//   - Used as a sub-agent for transfer patterns
//   - Wrapped as a tool using agenttool.New()
//   - Part of workflow agents (sequential, parallel, loop)
//
// Example:
//
//	// From URL (agent card resolved automatically)
//	agent, _ := remoteagent.NewA2A(remoteagent.Config{
//	    Name:        "remote_helper",
//	    Description: "A remote helper agent",
//	    URL:         "http://localhost:9000",
//	})
//
//	// From explicit agent card source
//	agent, _ := remoteagent.NewA2A(remoteagent.Config{
//	    Name:            "remote_helper",
//	    Description:     "A remote helper agent",
//	    AgentCardSource: "http://localhost:9000/.well-known/agent-card.json",
//	})
func NewA2A(cfg Config) (agent.Agent, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if cfg.URL == "" && cfg.AgentCard == nil && cfg.AgentCardSource == "" {
		return nil, fmt.Errorf("one of URL, AgentCard, or AgentCardSource must be provided")
	}

	// Set defaults
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute // Default: 5 minutes for agents with tools
	}

	// If URL provided but no AgentCardSource, use URL directly
	// The agentcard resolver will handle .well-known/agent-card.json appending
	if cfg.URL != "" && cfg.AgentCardSource == "" && cfg.AgentCard == nil {
		cfg.AgentCardSource = cfg.URL
	}

	remoteAgent := &a2aAgent{
		cfg:          cfg,
		resolvedCard: cfg.AgentCard,
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
			return remoteAgent.run(ctx)
		},
		AgentType: agent.TypeRemoteAgent,
	})
}

func (a *a2aAgent) run(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Resolve agent card if not already resolved
		card, err := a.resolveAgentCard(ctx)
		if err != nil {
			yield(a.errorEvent(ctx, fmt.Errorf("agent card resolution failed: %w", err)), nil)
			return
		}
		a.resolvedCard = card

		// Create HTTP client with configured timeout
		// The a2a-go library defaults to 5 seconds which is too short for agents with tools
		httpClient := &http.Client{
			Timeout: a.cfg.Timeout,
		}

		// Create A2A client with custom HTTP client
		client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithJSONRPCTransport(httpClient))
		if err != nil {
			yield(a.errorEvent(ctx, fmt.Errorf("client creation failed: %w", err)), nil)
			return
		}
		defer func() { _ = client.Destroy() }()

		// Build message from context
		msg := a.buildMessage(ctx)
		if len(msg.Parts) == 0 {
			// No content to send, yield empty event
			yield(a.newEvent(ctx), nil)
			return
		}

		// Send message and stream response
		req := &a2a.MessageSendParams{
			Message: msg,
			Config:  a.cfg.MessageSendConfig,
		}

		// Determine if we can use streaming:
		// - Config must enable streaming
		// - Agent Card must advertise streaming capability
		useStreaming := a.cfg.Streaming && card.Capabilities.Streaming

		if useStreaming {
			// SSE streaming: message/stream endpoint
			for a2aEvent, err := range client.SendStreamingMessage(ctx, req) {
				if err != nil {
					yield(a.errorEvent(ctx, err), nil)
					return
				}

				event := a.convertEvent(ctx, a2aEvent)
				if event == nil {
					continue
				}

				if !yield(event, nil) {
					break
				}
			}
		} else {
			// Blocking mode: message/send endpoint
			result, err := client.SendMessage(ctx, req)
			if err != nil {
				yield(a.errorEvent(ctx, err), nil)
				return
			}

			// Convert result to event - SendMessage returns Task or Message
			switch r := result.(type) {
			case *a2a.Task:
				event := a.convertEvent(ctx, r)
				if event != nil {
					yield(event, nil)
				}
			case *a2a.Message:
				event := a.newEvent(ctx)
				event.Message = r
				yield(event, nil)
			}
		}
	}
}

func (a *a2aAgent) resolveAgentCard(ctx agent.InvocationContext) (*a2a.AgentCard, error) {
	// Return cached card if available
	if a.resolvedCard != nil {
		return a.resolvedCard, nil
	}

	source := a.cfg.AgentCardSource

	// Resolve from URL
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		card, err := agentcard.DefaultResolver.Resolve(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch agent card from %s: %w", source, err)
		}
		return card, nil
	}

	// Resolve from file
	fileBytes, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent card from %q: %w", source, err)
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(fileBytes, &card); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent card: %w", err)
	}

	return &card, nil
}

func (a *a2aAgent) buildMessage(ctx agent.InvocationContext) *a2a.Message {
	// Get user content from context
	userContent := ctx.UserContent()
	if userContent == nil {
		return a2a.NewMessage(a2a.MessageRoleUser)
	}

	// Convert to A2A message
	msg := a2a.NewMessage(a2a.MessageRoleUser, userContent.Parts...)

	// Set ContextID from session to maintain conversation history on remote server
	// This ensures the remote server groups all messages from this session together
	session := ctx.Session()
	if session != nil {
		msg.ContextID = session.ID()
	}

	return msg
}

func (a *a2aAgent) newEvent(ctx agent.InvocationContext) *agent.Event {
	return &agent.Event{
		InvocationID: ctx.InvocationID(),
		Branch:       ctx.Branch(),
	}
}

func (a *a2aAgent) errorEvent(ctx agent.InvocationContext, err error) *agent.Event {
	event := a.newEvent(ctx)
	// Create error message so it's displayed to the user
	event.Message = a2a.NewMessage(
		a2a.MessageRoleAgent,
		a2a.TextPart{Text: "Remote agent error: " + err.Error()},
	)
	event.CustomMetadata = map[string]any{
		"_hector_remote_error": err.Error(),
		"error":                true,
	}
	return event
}

func (a *a2aAgent) convertEvent(ctx agent.InvocationContext, a2aEvent a2a.Event) *agent.Event {
	// Convert A2A event to Hector event
	event := a.newEvent(ctx)

	switch e := a2aEvent.(type) {
	case *a2a.TaskStatusUpdateEvent:
		// Task status updates
		event.CustomMetadata = map[string]any{
			"_hector_task_status": e.Status.State,
		}
		if e.Status.Message != nil {
			event.Message = e.Status.Message
		}
		event.Partial = e.Status.State == a2a.TaskStateWorking

	case *a2a.TaskArtifactUpdateEvent:
		// Artifact updates - extract message parts from artifact
		if e.Artifact != nil && len(e.Artifact.Parts) > 0 {
			event.Message = a2a.NewMessage(a2a.MessageRoleAgent, e.Artifact.Parts...)
		}
		event.Partial = !e.LastChunk

	case *a2a.Task:
		// Task object from message/send response.
		// Response content can be in:
		// - Task.History (ADK puts response as last agent message)
		// - Task.Artifacts (Hector puts response in artifacts)

		hasMessage := false

		// First, try to get response from Artifacts (Hector pattern)
		// Now that server side disables streaming for blocking requests,
		// artifacts will contain only the final response, clean of chunks.
		if len(e.Artifacts) > 0 {
			var allParts []a2a.Part
			for _, artifact := range e.Artifacts {
				allParts = append(allParts, artifact.Parts...)
			}
			if len(allParts) > 0 {
				event.Message = a2a.NewMessage(a2a.MessageRoleAgent, allParts...)
				hasMessage = true
			}
		}

		// Fallback: try to get response from History (ADK pattern)
		if !hasMessage && len(e.History) > 0 {
			lastMsg := e.History[len(e.History)-1]
			// Check if the last message is from the agent
			if lastMsg.Role == a2a.MessageRoleAgent {
				event.Message = lastMsg
				hasMessage = true
			}
		}

		event.CustomMetadata = map[string]any{
			"_hector_task_id": e.ID,
		}

		// Task with completed status means final response
		event.Partial = e.Status.State != a2a.TaskStateCompleted

		// Only set _hector_task_status if we don't have a message
		// (to avoid triggering status-only event path)
		if !hasMessage {
			event.CustomMetadata["_hector_task_status"] = e.Status.State
		}

	default:
		// Unknown event type, skip
		return nil
	}

	return event
}
