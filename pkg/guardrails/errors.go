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

import "fmt"

// GuardrailError represents an error from a guardrail check.
type GuardrailError struct {
	GuardrailName string
	Reason        string
	Severity      Severity
	Details       map[string]any
}

func (e *GuardrailError) Error() string {
	return fmt.Sprintf("guardrail %q blocked: %s (severity: %s)", e.GuardrailName, e.Reason, e.Severity)
}

// NewGuardrailError creates a new GuardrailError from a Result.
func NewGuardrailError(result *Result) *GuardrailError {
	return &GuardrailError{
		GuardrailName: result.GuardrailName,
		Reason:        result.Reason,
		Severity:      result.Severity,
		Details:       result.Details,
	}
}

// ChainError represents multiple errors from a guardrail chain.
type ChainError struct {
	Errors []*GuardrailError
}

func (e *ChainError) Error() string {
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("%d guardrails blocked execution", len(e.Errors))
}

// Add adds an error to the chain.
func (e *ChainError) Add(err *GuardrailError) {
	e.Errors = append(e.Errors, err)
}

// HasErrors returns true if any errors were collected.
func (e *ChainError) HasErrors() bool {
	return len(e.Errors) > 0
}
