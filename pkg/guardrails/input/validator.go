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

package input

import (
	"context"
	"fmt"
	"regexp"

	"github.com/verikod/hector/pkg/guardrails"
)

// LengthValidator validates input length.
type LengthValidator struct {
	minLength int
	maxLength int
	action    guardrails.Action
	severity  guardrails.Severity
}

// NewLengthValidator creates a new length validator.
func NewLengthValidator(minLength, maxLength int) *LengthValidator {
	return &LengthValidator{
		minLength: minLength,
		maxLength: maxLength,
		action:    guardrails.ActionBlock,
		severity:  guardrails.SeverityMedium,
	}
}

// WithAction sets the action to take on violation.
func (v *LengthValidator) WithAction(action guardrails.Action) *LengthValidator {
	v.action = action
	return v
}

// WithSeverity sets the severity of violations.
func (v *LengthValidator) WithSeverity(severity guardrails.Severity) *LengthValidator {
	v.severity = severity
	return v
}

// Name returns the guardrail name.
func (v *LengthValidator) Name() string {
	return "length_validator"
}

// Check validates the input length.
func (v *LengthValidator) Check(_ context.Context, input string) (*guardrails.Result, error) {
	length := len(input)

	if v.minLength > 0 && length < v.minLength {
		return &guardrails.Result{
			Action:        v.action,
			Severity:      v.severity,
			Reason:        fmt.Sprintf("input too short: %d chars (minimum: %d)", length, v.minLength),
			GuardrailName: v.Name(),
			Details: map[string]any{
				"length":     length,
				"min_length": v.minLength,
			},
		}, nil
	}

	if v.maxLength > 0 && length > v.maxLength {
		return &guardrails.Result{
			Action:        v.action,
			Severity:      v.severity,
			Reason:        fmt.Sprintf("input too long: %d chars (maximum: %d)", length, v.maxLength),
			GuardrailName: v.Name(),
			Details: map[string]any{
				"length":     length,
				"max_length": v.maxLength,
			},
		}, nil
	}

	return guardrails.Allow(v.Name()), nil
}

// PatternValidator validates input against regex patterns.
type PatternValidator struct {
	allowPatterns []*regexp.Regexp
	blockPatterns []*regexp.Regexp
	action        guardrails.Action
	severity      guardrails.Severity
}

// NewPatternValidator creates a new pattern validator.
func NewPatternValidator() *PatternValidator {
	return &PatternValidator{
		action:   guardrails.ActionBlock,
		severity: guardrails.SeverityMedium,
	}
}

// AllowPatterns sets patterns that input must match (at least one).
func (v *PatternValidator) AllowPatterns(patterns ...string) *PatternValidator {
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			v.allowPatterns = append(v.allowPatterns, re)
		}
	}
	return v
}

// BlockPatterns sets patterns that input must NOT match.
func (v *PatternValidator) BlockPatterns(patterns ...string) *PatternValidator {
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			v.blockPatterns = append(v.blockPatterns, re)
		}
	}
	return v
}

// WithAction sets the action to take on violation.
func (v *PatternValidator) WithAction(action guardrails.Action) *PatternValidator {
	v.action = action
	return v
}

// WithSeverity sets the severity of violations.
func (v *PatternValidator) WithSeverity(severity guardrails.Severity) *PatternValidator {
	v.severity = severity
	return v
}

// Name returns the guardrail name.
func (v *PatternValidator) Name() string {
	return "pattern_validator"
}

// Check validates the input against patterns.
func (v *PatternValidator) Check(_ context.Context, input string) (*guardrails.Result, error) {
	// Check block patterns first
	for _, re := range v.blockPatterns {
		if re.MatchString(input) {
			return &guardrails.Result{
				Action:        v.action,
				Severity:      v.severity,
				Reason:        fmt.Sprintf("input matches blocked pattern: %s", re.String()),
				GuardrailName: v.Name(),
				Details: map[string]any{
					"matched_pattern": re.String(),
					"pattern_type":    "block",
				},
			}, nil
		}
	}

	// Check allow patterns (if any defined, at least one must match)
	if len(v.allowPatterns) > 0 {
		matched := false
		for _, re := range v.allowPatterns {
			if re.MatchString(input) {
				matched = true
				break
			}
		}
		if !matched {
			return &guardrails.Result{
				Action:        v.action,
				Severity:      v.severity,
				Reason:        "input does not match any allowed pattern",
				GuardrailName: v.Name(),
				Details: map[string]any{
					"pattern_type": "allow",
				},
			}, nil
		}
	}

	return guardrails.Allow(v.Name()), nil
}

// Ensure interface compliance.
var (
	_ guardrails.InputGuardrail = (*LengthValidator)(nil)
	_ guardrails.InputGuardrail = (*PatternValidator)(nil)
)
