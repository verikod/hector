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

package guardrails

import "context"

// Action represents what should happen after a guardrail check.
type Action string

const (
	// ActionAllow continues execution normally.
	ActionAllow Action = "allow"
	// ActionBlock stops execution and returns an error/message.
	ActionBlock Action = "block"
	// ActionModify continues with a modified value.
	ActionModify Action = "modify"
	// ActionWarn logs a warning but continues execution.
	ActionWarn Action = "warn"
)

// Severity indicates how critical a guardrail violation is.
type Severity string

const (
	// SeverityLow indicates a minor issue that may be logged.
	SeverityLow Severity = "low"
	// SeverityMedium indicates a notable issue that should be reviewed.
	SeverityMedium Severity = "medium"
	// SeverityHigh indicates a serious issue that may require action.
	SeverityHigh Severity = "high"
	// SeverityCritical indicates an issue that must be blocked.
	SeverityCritical Severity = "critical"
)

// Result represents the outcome of a guardrail check.
type Result struct {
	// Action to take based on the check result.
	Action Action `json:"action"`

	// Severity of the issue detected (if any).
	Severity Severity `json:"severity,omitempty"`

	// Reason provides a human-readable explanation.
	Reason string `json:"reason,omitempty"`

	// GuardrailName is the name of the guardrail that produced this result.
	GuardrailName string `json:"guardrail_name"`

	// Modified contains the modified value if Action == ActionModify.
	Modified any `json:"-"`

	// Details contains additional metadata about the check.
	Details map[string]any `json:"details,omitempty"`
}

// IsBlocking returns true if this result blocks execution.
func (r *Result) IsBlocking() bool {
	return r.Action == ActionBlock
}

// IsAllowed returns true if execution should continue.
func (r *Result) IsAllowed() bool {
	return r.Action == ActionAllow || r.Action == ActionWarn || r.Action == ActionModify
}

// Allow creates an allow result.
func Allow(guardrailName string) *Result {
	return &Result{
		Action:        ActionAllow,
		GuardrailName: guardrailName,
	}
}

// Block creates a blocking result.
func Block(guardrailName, reason string, severity Severity) *Result {
	return &Result{
		Action:        ActionBlock,
		Reason:        reason,
		Severity:      severity,
		GuardrailName: guardrailName,
	}
}

// Modify creates a result that modifies the input/output.
func Modify(guardrailName string, modified any, reason string) *Result {
	return &Result{
		Action:        ActionModify,
		Modified:      modified,
		Reason:        reason,
		GuardrailName: guardrailName,
	}
}

// Warn creates a warning result that allows execution to continue.
func Warn(guardrailName, reason string, severity Severity) *Result {
	return &Result{
		Action:        ActionWarn,
		Reason:        reason,
		Severity:      severity,
		GuardrailName: guardrailName,
	}
}

// InputGuardrail validates and potentially transforms user input.
//
// Implementations should be stateless and thread-safe.
type InputGuardrail interface {
	// Name returns the guardrail's unique identifier.
	Name() string

	// Check validates the input and returns a result.
	// If the result action is ActionModify, the Modified field contains
	// the transformed input string.
	Check(ctx context.Context, input string) (*Result, error)
}

// OutputGuardrail validates and potentially transforms LLM output.
//
// Implementations should be stateless and thread-safe.
type OutputGuardrail interface {
	// Name returns the guardrail's unique identifier.
	Name() string

	// Check validates the output and returns a result.
	// If the result action is ActionModify, the Modified field contains
	// the transformed output string.
	Check(ctx context.Context, output string) (*Result, error)
}

// ToolGuardrail validates tool calls before execution.
//
// Implementations should be stateless and thread-safe.
type ToolGuardrail interface {
	// Name returns the guardrail's unique identifier.
	Name() string

	// Check validates the tool call and returns a result.
	// If the result action is ActionModify, the Modified field contains
	// the transformed arguments map.
	Check(ctx context.Context, toolName string, args map[string]any) (*Result, error)
}
