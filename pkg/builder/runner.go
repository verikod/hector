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

package builder

import (
	"fmt"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/memory"
	"github.com/verikod/hector/pkg/runner"
	"github.com/verikod/hector/pkg/session"
)

// RunnerBuilder provides a fluent API for building runners.
//
// Runners orchestrate agent execution within sessions, handling:
//   - Session creation and retrieval
//   - Agent selection based on session history
//   - Event streaming and persistence
//
// Example:
//
//	r, err := builder.NewRunner("my-app").
//	    WithAgent(myAgent).
//	    WithSessionService(session.InMemoryService()).
//	    Build()
type RunnerBuilder struct {
	appName           string
	agent             agent.Agent
	sessionService    session.Service
	indexService      runner.IndexService
	checkpointManager runner.CheckpointManager
}

// NewRunner creates a new runner builder.
//
// Example:
//
//	r, err := builder.NewRunner("my-app").
//	    WithAgent(myAgent).
//	    Build()
func NewRunner(appName string) *RunnerBuilder {
	if appName == "" {
		panic("app name cannot be empty")
	}
	return &RunnerBuilder{
		appName: appName,
	}
}

// WithAgent sets the root agent for execution.
//
// Example:
//
//	builder.NewRunner("app").WithAgent(myAgent)
func (b *RunnerBuilder) WithAgent(ag agent.Agent) *RunnerBuilder {
	if ag == nil {
		panic("agent cannot be nil")
	}
	b.agent = ag
	return b
}

// WithSessionService sets the session service for persistence.
// If not set, an in-memory service will be used.
//
// Example:
//
//	builder.NewRunner("app").WithSessionService(session.InMemoryService())
func (b *RunnerBuilder) WithSessionService(svc session.Service) *RunnerBuilder {
	b.sessionService = svc
	return b
}

// WithIndexService sets the index service for semantic search.
//
// Example:
//
//	builder.NewRunner("app").WithIndexService(indexSvc)
func (b *RunnerBuilder) WithIndexService(svc runner.IndexService) *RunnerBuilder {
	b.indexService = svc
	return b
}

// WithMemoryIndex sets up memory indexing with the provided index service.
// This is a convenience method that wraps memory.IndexService.
//
// Example:
//
//	indexSvc := memory.NewKeywordIndexService()
//	builder.NewRunner("app").WithMemoryIndex(indexSvc)
func (b *RunnerBuilder) WithMemoryIndex(idx memory.IndexService) *RunnerBuilder {
	b.indexService = idx
	return b
}

// WithCheckpointManager sets the checkpoint manager for fault tolerance.
//
// Example:
//
//	builder.NewRunner("app").WithCheckpointManager(checkpointMgr)
func (b *RunnerBuilder) WithCheckpointManager(mgr runner.CheckpointManager) *RunnerBuilder {
	b.checkpointManager = mgr
	return b
}

// Build creates the runner.
//
// Returns an error if required parameters are missing.
func (b *RunnerBuilder) Build() (*runner.Runner, error) {
	if b.agent == nil {
		return nil, fmt.Errorf("agent is required: use WithAgent()")
	}

	// Use in-memory session service if not provided
	sessionSvc := b.sessionService
	if sessionSvc == nil {
		sessionSvc = session.InMemoryService()
	}

	return runner.New(runner.Config{
		AppName:           b.appName,
		Agent:             b.agent,
		SessionService:    sessionSvc,
		IndexService:      b.indexService,
		CheckpointManager: b.checkpointManager,
	})
}

// MustBuild creates the runner or panics on error.
//
// Use this only when you're certain the configuration is valid.
func (b *RunnerBuilder) MustBuild() *runner.Runner {
	r, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build runner: %v", err))
	}
	return r
}
