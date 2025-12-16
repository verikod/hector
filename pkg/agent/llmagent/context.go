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
	"context"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/tool"
)

// toolContext implements tool.Context for tool execution.
type toolContext struct {
	agent.CallbackContext
	functionCallID string
	actions        *agent.EventActions
	invCtx         agent.InvocationContext
}

func newToolContext(invCtx agent.InvocationContext, functionCallID string) *toolContext {
	return &toolContext{
		CallbackContext: newCallbackContextFromInvocation(invCtx),
		functionCallID:  functionCallID,
		actions:         &agent.EventActions{StateDelta: make(map[string]any)},
		invCtx:          invCtx,
	}
}

func (c *toolContext) FunctionCallID() string {
	return c.functionCallID
}

func (c *toolContext) Actions() *agent.EventActions {
	return c.actions
}

func (c *toolContext) SearchMemory(ctx context.Context, query string) (*agent.MemorySearchResponse, error) {
	memory := c.invCtx.Memory()
	if memory == nil {
		return &agent.MemorySearchResponse{}, nil
	}
	return memory.Search(ctx, query)
}

// InvocationContext returns the underlying InvocationContext.
// This is used by agenttool to create child invocation contexts.
func (c *toolContext) InvocationContext() agent.InvocationContext {
	return c.invCtx
}

// Task returns the parent task for cascade cancellation registration.
// Returns nil if no task is associated with this context.
func (c *toolContext) Task() agent.CancellableTask {
	if c.invCtx == nil {
		return nil
	}
	// Get task from RunConfig
	runConfig := c.invCtx.RunConfig()
	if runConfig == nil || runConfig.Task == nil {
		return nil
	}
	// Return the task directly - no adapter needed since both use agent.CancellableTask
	return runConfig.Task
}

// callbackContextAdapter adapts InvocationContext to CallbackContext.
type callbackContextAdapter struct {
	context.Context
	invCtx agent.InvocationContext
}

func newCallbackContextFromInvocation(invCtx agent.InvocationContext) agent.CallbackContext {
	return &callbackContextAdapter{
		Context: invCtx,
		invCtx:  invCtx,
	}
}

func (c *callbackContextAdapter) InvocationID() string {
	return c.invCtx.InvocationID()
}

func (c *callbackContextAdapter) AgentName() string {
	return c.invCtx.Agent().Name()
}

func (c *callbackContextAdapter) UserContent() *agent.Content {
	return c.invCtx.UserContent()
}

func (c *callbackContextAdapter) Branch() string {
	return c.invCtx.Branch()
}

func (c *callbackContextAdapter) UserID() string {
	session := c.invCtx.Session()
	if session != nil {
		return session.UserID()
	}
	return ""
}

func (c *callbackContextAdapter) AppName() string {
	session := c.invCtx.Session()
	if session != nil {
		return session.AppName()
	}
	return ""
}

func (c *callbackContextAdapter) SessionID() string {
	session := c.invCtx.Session()
	if session != nil {
		return session.ID()
	}
	return ""
}

func (c *callbackContextAdapter) ReadonlyState() agent.ReadonlyState {
	session := c.invCtx.Session()
	if session != nil {
		return session.State()
	}
	return nil
}

func (c *callbackContextAdapter) Artifacts() agent.Artifacts {
	return c.invCtx.Artifacts()
}

func (c *callbackContextAdapter) State() agent.State {
	session := c.invCtx.Session()
	if session != nil {
		return session.State()
	}
	return nil
}

var (
	_ tool.Context          = (*toolContext)(nil)
	_ agent.CallbackContext = (*callbackContextAdapter)(nil)
)
