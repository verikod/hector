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

// Package guardrails provides composable safety controls for Hector agents.
//
// Guardrails can be applied at multiple interception points:
//   - Input: Validate and sanitize user input before agent processing
//   - Output: Filter and redact LLM responses before returning to users
//   - Tool: Authorize and validate tool calls before execution
//
// # Architecture
//
// Guardrails integrate with Hector's existing callback system:
//   - InputGuardrail -> BeforeAgentCallback
//   - OutputGuardrail -> AfterModelCallback
//   - ToolGuardrail -> BeforeToolCallback
//
// # Usage
//
// Create guardrails and chain them together:
//
//	chain := guardrails.NewInputChain(
//	    input.NewLengthValidator(10, 10000),
//	    input.NewInjectionDetector(),
//	    input.NewSanitizer(),
//	)
//
//	agent, _ := builder.NewAgent("secure-agent").
//	    WithLLM(llm).
//	    WithInputGuardrails(chain.Guardrails()...).
//	    Build()
//
// # Configuration
//
// Guardrails can be configured programmatically or via YAML:
//
//	config, _ := guardrails.LoadConfig("guardrails.yaml")
//	chain := config.BuildInputChain()
package guardrails
