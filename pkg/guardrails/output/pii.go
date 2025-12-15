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

package output

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/kadirpekel/hector/pkg/guardrails"
)

// PII patterns for detection.
var (
	// Email pattern
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

	// Phone patterns (US formats)
	phonePattern = regexp.MustCompile(`(?:\+1[-.\s]?)?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}`)

	// SSN pattern (US)
	ssnPattern = regexp.MustCompile(`\b[0-9]{3}[-\s]?[0-9]{2}[-\s]?[0-9]{4}\b`)

	// Credit card patterns (major providers)
	creditCardPattern = regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`)
)

// PIIRedactor detects and optionally redacts personally identifiable information.
type PIIRedactor struct {
	detectEmail      bool
	detectPhone      bool
	detectSSN        bool
	detectCreditCard bool
	redactMode       guardrails.RedactMode
	action           guardrails.Action
	severity         guardrails.Severity
}

// NewPIIRedactor creates a new PII redactor with default settings.
func NewPIIRedactor() *PIIRedactor {
	return &PIIRedactor{
		detectEmail:      true,
		detectPhone:      true,
		detectSSN:        true,
		detectCreditCard: true,
		redactMode:       guardrails.RedactModeMask,
		action:           guardrails.ActionModify,
		severity:         guardrails.SeverityHigh,
	}
}

// DetectEmail enables/disables email detection.
func (r *PIIRedactor) DetectEmail(detect bool) *PIIRedactor {
	r.detectEmail = detect
	return r
}

// DetectPhone enables/disables phone number detection.
func (r *PIIRedactor) DetectPhone(detect bool) *PIIRedactor {
	r.detectPhone = detect
	return r
}

// DetectSSN enables/disables SSN detection.
func (r *PIIRedactor) DetectSSN(detect bool) *PIIRedactor {
	r.detectSSN = detect
	return r
}

// DetectCreditCard enables/disables credit card detection.
func (r *PIIRedactor) DetectCreditCard(detect bool) *PIIRedactor {
	r.detectCreditCard = detect
	return r
}

// RedactMode sets how PII is redacted.
func (r *PIIRedactor) RedactMode(mode guardrails.RedactMode) *PIIRedactor {
	r.redactMode = mode
	return r
}

// WithAction sets the action to take on detection.
func (r *PIIRedactor) WithAction(action guardrails.Action) *PIIRedactor {
	r.action = action
	return r
}

// WithSeverity sets the severity of detections.
func (r *PIIRedactor) WithSeverity(severity guardrails.Severity) *PIIRedactor {
	r.severity = severity
	return r
}

// Name returns the guardrail name.
func (r *PIIRedactor) Name() string {
	return "pii_redactor"
}

// PIIMatch represents a detected PII instance.
type PIIMatch struct {
	Type    string
	Value   string
	StartAt int
	EndAt   int
}

// Check detects and optionally redacts PII in the output.
func (r *PIIRedactor) Check(_ context.Context, output string) (*guardrails.Result, error) {
	var matches []PIIMatch

	// Detect emails
	if r.detectEmail {
		for _, loc := range emailPattern.FindAllStringIndex(output, -1) {
			matches = append(matches, PIIMatch{
				Type:    "email",
				Value:   output[loc[0]:loc[1]],
				StartAt: loc[0],
				EndAt:   loc[1],
			})
		}
	}

	// Detect phone numbers
	if r.detectPhone {
		for _, loc := range phonePattern.FindAllStringIndex(output, -1) {
			matches = append(matches, PIIMatch{
				Type:    "phone",
				Value:   output[loc[0]:loc[1]],
				StartAt: loc[0],
				EndAt:   loc[1],
			})
		}
	}

	// Detect SSNs
	if r.detectSSN {
		for _, loc := range ssnPattern.FindAllStringIndex(output, -1) {
			matches = append(matches, PIIMatch{
				Type:    "ssn",
				Value:   output[loc[0]:loc[1]],
				StartAt: loc[0],
				EndAt:   loc[1],
			})
		}
	}

	// Detect credit cards
	if r.detectCreditCard {
		for _, loc := range creditCardPattern.FindAllStringIndex(output, -1) {
			matches = append(matches, PIIMatch{
				Type:    "credit_card",
				Value:   output[loc[0]:loc[1]],
				StartAt: loc[0],
				EndAt:   loc[1],
			})
		}
	}

	// No PII found
	if len(matches) == 0 {
		return guardrails.Allow(r.Name()), nil
	}

	// Count by type for details
	piiCounts := make(map[string]int)
	for _, m := range matches {
		piiCounts[m.Type]++
	}

	// If action is Block, don't redact
	if r.action == guardrails.ActionBlock {
		return &guardrails.Result{
			Action:        guardrails.ActionBlock,
			Severity:      r.severity,
			Reason:        "PII detected in output",
			GuardrailName: r.Name(),
			Details: map[string]any{
				"pii_types":  piiCounts,
				"pii_count":  len(matches),
			},
		}, nil
	}

	// Redact PII
	redacted := r.redact(output, matches)

	return &guardrails.Result{
		Action:        guardrails.ActionModify,
		Severity:      r.severity,
		Reason:        "PII redacted from output",
		Modified:      redacted,
		GuardrailName: r.Name(),
		Details: map[string]any{
			"pii_types":  piiCounts,
			"pii_count":  len(matches),
		},
	}, nil
}

// redact replaces PII with appropriate redaction.
func (r *PIIRedactor) redact(text string, matches []PIIMatch) string {
	// Sort matches by position (descending) to replace from end to start
	// This preserves positions of earlier matches
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		replacement := r.getRedaction(m)
		text = text[:m.StartAt] + replacement + text[m.EndAt:]
	}
	return text
}

// getRedaction returns the redaction string for a match.
func (r *PIIRedactor) getRedaction(m PIIMatch) string {
	switch r.redactMode {
	case guardrails.RedactModeRemove:
		return ""

	case guardrails.RedactModeHash:
		hash := sha256.Sum256([]byte(m.Value))
		return "[" + m.Type + ":" + hex.EncodeToString(hash[:8]) + "]"

	case guardrails.RedactModeMask:
		fallthrough
	default:
		// Mask with asterisks, preserving length indication
		return "[" + strings.ToUpper(m.Type) + "_REDACTED]"
	}
}

// Ensure interface compliance.
var _ guardrails.OutputGuardrail = (*PIIRedactor)(nil)
