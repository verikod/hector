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

package workflowagent

import (
	"iter"

	"github.com/kadirpekel/hector/pkg/agent"
)

// LoopConfig defines the configuration for a LoopAgent.
type LoopConfig struct {
	// Name is the agent name.
	Name string

	// DisplayName is the human-readable name (optional).
	DisplayName string

	// Description describes what the agent does.
	Description string

	// SubAgents are the agents to run in each iteration.
	SubAgents []agent.Agent

	// AgentType overrides the agent type. Defaults to TypeLoopAgent.
	AgentType agent.AgentType

	// MaxIterations is the maximum number of iterations.
	// If 0, runs indefinitely until any sub-agent escalates.
	MaxIterations uint
}

// NewLoop creates a LoopAgent.
//
// LoopAgent repeatedly runs its sub-agents in sequence for a specified number
// of iterations or until a termination condition is met (escalate action).
//
// Use LoopAgent when your workflow involves repetition or iterative
// refinement, such as revising code or refining outputs.
//
// Example:
//
//	reviewer, _ := llmagent.New(llmagent.Config{...})
//	improver, _ := llmagent.New(llmagent.Config{...})
//
//	refiner, _ := workflowagent.NewLoop(workflowagent.LoopConfig{
//	    Name:          "refiner",
//	    Description:   "Iteratively refines output",
//	    SubAgents:     []agent.Agent{reviewer, improver},
//	    MaxIterations: 3,
//	})
func NewLoop(cfg LoopConfig) (agent.Agent, error) {
	maxIterations := cfg.MaxIterations

	agentType := cfg.AgentType
	if agentType == "" {
		agentType = agent.TypeLoopAgent
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
		SubAgents:   cfg.SubAgents,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
			return runLoop(ctx, maxIterations)
		},
		AgentType: agentType,
	})

}

func runLoop(ctx agent.InvocationContext, maxIterations uint) iter.Seq2[*agent.Event, error] {
	count := maxIterations

	return func(yield func(*agent.Event, error) bool) {
		for {
			shouldExit := false

			for _, subAgent := range ctx.Agent().SubAgents() {
				// Create sub-context for the sub-agent
				subCtx := agent.NewInvocationContext(ctx, agent.InvocationContextParams{
					Agent:       subAgent,
					Session:     ctx.Session(),
					Artifacts:   ctx.Artifacts(),
					Memory:      ctx.Memory(),
					UserContent: ctx.UserContent(),
					RunConfig:   ctx.RunConfig(),
					Branch:      ctx.Branch(), // Share branch so sub-agents see each other's events
				})

				for event, err := range subAgent.Run(subCtx) {
					if !yield(event, err) {
						return
					}

					// Check if sub-agent escalated
					if event != nil && event.Actions.Escalate {
						shouldExit = true
					}
				}

				if shouldExit {
					return
				}
			}

			// Handle iteration count
			if count > 0 {
				count--
				if count == 0 {
					return
				}
			}
		}
	}
}
