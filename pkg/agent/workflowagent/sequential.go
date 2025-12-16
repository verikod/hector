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
	"github.com/verikod/hector/pkg/agent"
)

// SequentialConfig defines the configuration for a SequentialAgent.
type SequentialConfig struct {
	// Name is the agent name.
	Name string

	// DisplayName is the human-readable name (optional).
	DisplayName string

	// Description describes what the agent does.
	Description string

	// SubAgents are the agents to run in sequence.
	SubAgents []agent.Agent
}

// NewSequential creates a SequentialAgent.
//
// SequentialAgent executes its sub-agents once, in the order they are listed.
// This is implemented as a LoopAgent with MaxIterations=1.
//
// Use SequentialAgent when you want execution to occur in a fixed, strict order,
// such as a processing pipeline.
//
// Example:
//
//	stage1, _ := llmagent.New(llmagent.Config{Name: "stage1", ...})
//	stage2, _ := llmagent.New(llmagent.Config{Name: "stage2", ...})
//	stage3, _ := llmagent.New(llmagent.Config{Name: "stage3", ...})
//
//	pipeline, _ := workflowagent.NewSequential(workflowagent.SequentialConfig{
//	    Name:        "pipeline",
//	    Description: "Processes data through multiple stages",
//	    SubAgents:   []agent.Agent{stage1, stage2, stage3},
//	})
func NewSequential(cfg SequentialConfig) (agent.Agent, error) {
	return NewLoop(LoopConfig{
		Name:          cfg.Name,
		DisplayName:   cfg.DisplayName,
		Description:   cfg.Description,
		SubAgents:     cfg.SubAgents,
		MaxIterations: 1, // Sequential = single iteration
		AgentType:     agent.TypeSequentialAgent,
	})
}
