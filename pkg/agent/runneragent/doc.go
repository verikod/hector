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

// Package runneragent provides a deterministic tool execution agent.
//
// A runner agent executes its configured tools in sequence without LLM
// involvement. This enables pure automation pipelines that can be composed
// with LLM agents in workflows.
//
// Key characteristics:
//   - No LLM reasoning - tools execute deterministically in order
//   - Output piping - each tool's output becomes the next tool's input
//   - Composable - works with sequential/parallel/loop workflow agents
//   - A2A compatible - final output becomes an artifact/message
//
// Example configuration:
//
//	agents:
//	  data_fetcher:
//	    type: runner
//	    tools: [web_fetch]
//
//	  pipeline:
//	    type: sequential
//	    sub_agents: [data_fetcher, analyzer]
//
// Use cases:
//   - ETL pipelines (fetch → transform → save)
//   - CI/CD automation (bash → grep_search)
//   - Data preprocessing before LLM analysis
package runneragent
