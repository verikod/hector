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
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/verikod/hector/pkg/guardrails"
)

// Sanitizer cleans and normalizes user input.
type Sanitizer struct {
	trimWhitespace   bool
	normalizeUnicode bool
	maxLength        int
	stripHTML        bool
}

// NewSanitizer creates a new input sanitizer with sensible defaults.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		trimWhitespace:   true,
		normalizeUnicode: false,
		maxLength:        0, // No limit by default
		stripHTML:        false,
	}
}

// TrimWhitespace enables/disables whitespace trimming.
func (s *Sanitizer) TrimWhitespace(trim bool) *Sanitizer {
	s.trimWhitespace = trim
	return s
}

// NormalizeUnicode enables/disables Unicode normalization (NFC).
func (s *Sanitizer) NormalizeUnicode(normalize bool) *Sanitizer {
	s.normalizeUnicode = normalize
	return s
}

// MaxLength sets the maximum length (truncates if exceeded).
func (s *Sanitizer) MaxLength(length int) *Sanitizer {
	s.maxLength = length
	return s
}

// StripHTML enables/disables HTML tag stripping.
func (s *Sanitizer) StripHTML(strip bool) *Sanitizer {
	s.stripHTML = strip
	return s
}

// Name returns the guardrail name.
func (s *Sanitizer) Name() string {
	return "input_sanitizer"
}

// htmlTagPattern matches HTML tags.
var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// Check sanitizes the input and returns a modified result if changes were made.
func (s *Sanitizer) Check(_ context.Context, input string) (*guardrails.Result, error) {
	result := input
	modified := false

	// Trim whitespace
	if s.trimWhitespace {
		trimmed := strings.TrimSpace(result)
		if trimmed != result {
			result = trimmed
			modified = true
		}
	}

	// Normalize Unicode (NFC form)
	if s.normalizeUnicode {
		normalized := norm.NFC.String(result)
		if normalized != result {
			result = normalized
			modified = true
		}
	}

	// Strip HTML tags
	if s.stripHTML {
		stripped := htmlTagPattern.ReplaceAllString(result, "")
		if stripped != result {
			result = stripped
			modified = true
		}
	}

	// Remove control characters (except newlines and tabs)
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return -1 // Remove
		}
		return r
	}, result)
	if cleaned != result {
		result = cleaned
		modified = true
	}

	// Truncate if too long
	if s.maxLength > 0 && len(result) > s.maxLength {
		result = result[:s.maxLength]
		modified = true
	}

	if modified {
		return guardrails.Modify(s.Name(), result, "input sanitized"), nil
	}

	return guardrails.Allow(s.Name()), nil
}

// Ensure interface compliance.
var _ guardrails.InputGuardrail = (*Sanitizer)(nil)
