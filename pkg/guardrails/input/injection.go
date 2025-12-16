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
	"regexp"
	"strings"

	"github.com/verikod/hector/pkg/guardrails"
)

// DefaultInjectionPatterns contains common prompt injection patterns.
var DefaultInjectionPatterns = []string{
	// Direct instruction override attempts
	`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`,
	`(?i)disregard\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`,
	`(?i)forget\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`,

	// Role/identity manipulation
	`(?i)you\s+are\s+now\s+`,
	`(?i)pretend\s+(to\s+be|you\s+are)\s+`,
	`(?i)act\s+as\s+(if\s+you\s+are\s+)?`,
	`(?i)roleplay\s+as\s+`,

	// System/assistant impersonation
	`(?i)^system\s*:`,
	`(?i)^assistant\s*:`,
	`(?i)\[system\]`,
	`(?i)\[assistant\]`,

	// Jailbreak attempts
	`(?i)jailbreak`,
	`(?i)dan\s+mode`,
	`(?i)developer\s+mode`,

	// Hidden instruction markers
	`(?i)<\s*system\s*>`,
	`(?i)<\s*/\s*system\s*>`,
	`(?i)\[\[system\]\]`,

	// Base64 encoded content (potential hidden instructions)
	`(?i)base64\s*:`,
}

// InjectionDetector detects prompt injection attempts.
type InjectionDetector struct {
	patterns      []*regexp.Regexp
	caseSensitive bool
	action        guardrails.Action
	severity      guardrails.Severity
}

// NewInjectionDetector creates a new injection detector with default patterns.
func NewInjectionDetector() *InjectionDetector {
	d := &InjectionDetector{
		caseSensitive: false,
		action:        guardrails.ActionBlock,
		severity:      guardrails.SeverityHigh,
	}

	// Compile default patterns
	for _, pattern := range DefaultInjectionPatterns {
		if re, err := regexp.Compile(pattern); err == nil {
			d.patterns = append(d.patterns, re)
		}
	}

	return d
}

// WithPatterns adds custom patterns (replaces defaults if called).
func (d *InjectionDetector) WithPatterns(patterns []string) *InjectionDetector {
	d.patterns = nil
	for _, pattern := range patterns {
		flags := ""
		if !d.caseSensitive {
			flags = "(?i)"
		}
		if re, err := regexp.Compile(flags + pattern); err == nil {
			d.patterns = append(d.patterns, re)
		}
	}
	return d
}

// AddPatterns adds additional patterns to the existing ones.
func (d *InjectionDetector) AddPatterns(patterns ...string) *InjectionDetector {
	for _, pattern := range patterns {
		flags := ""
		if !d.caseSensitive {
			flags = "(?i)"
		}
		if re, err := regexp.Compile(flags + pattern); err == nil {
			d.patterns = append(d.patterns, re)
		}
	}
	return d
}

// CaseSensitive sets whether pattern matching is case-sensitive.
func (d *InjectionDetector) CaseSensitive(sensitive bool) *InjectionDetector {
	d.caseSensitive = sensitive
	return d
}

// WithAction sets the action to take on detection.
func (d *InjectionDetector) WithAction(action guardrails.Action) *InjectionDetector {
	d.action = action
	return d
}

// WithSeverity sets the severity of detections.
func (d *InjectionDetector) WithSeverity(severity guardrails.Severity) *InjectionDetector {
	d.severity = severity
	return d
}

// Name returns the guardrail name.
func (d *InjectionDetector) Name() string {
	return "injection_detector"
}

// Check detects prompt injection in the input.
func (d *InjectionDetector) Check(_ context.Context, input string) (*guardrails.Result, error) {
	// Normalize input for checking
	normalized := strings.TrimSpace(input)

	for _, re := range d.patterns {
		if matches := re.FindStringSubmatch(normalized); len(matches) > 0 {
			return &guardrails.Result{
				Action:        d.action,
				Severity:      d.severity,
				Reason:        "potential prompt injection detected",
				GuardrailName: d.Name(),
				Details: map[string]any{
					"matched_pattern": re.String(),
					"matched_text":    matches[0],
				},
			}, nil
		}
	}

	return guardrails.Allow(d.Name()), nil
}

// Ensure interface compliance.
var _ guardrails.InputGuardrail = (*InjectionDetector)(nil)
