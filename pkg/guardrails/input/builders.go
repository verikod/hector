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
	"github.com/verikod/hector/pkg/guardrails"
)

// DefaultBuilders returns the standard input guardrail builders.
// Use with Config.BuildInputChain(input.DefaultBuilders()).
func DefaultBuilders() guardrails.InputChainBuilders {
	return guardrails.InputChainBuilders{
		LengthValidator:   BuildLengthValidator,
		InjectionDetector: BuildInjectionDetector,
		Sanitizer:         BuildSanitizer,
	}
}

// BuildLengthValidator creates a LengthValidator from config.
func BuildLengthValidator(cfg *guardrails.LengthConfig) guardrails.InputGuardrail {
	v := NewLengthValidator(cfg.MinLength, cfg.MaxLength)
	if cfg.Action != "" {
		v.WithAction(cfg.Action)
	}
	if cfg.Severity != "" {
		v.WithSeverity(cfg.Severity)
	}
	return v
}

// BuildInjectionDetector creates an InjectionDetector from config.
func BuildInjectionDetector(cfg *guardrails.InjectionConfig) guardrails.InputGuardrail {
	d := NewInjectionDetector()
	if len(cfg.Patterns) > 0 {
		d.WithPatterns(cfg.Patterns)
	}
	d.CaseSensitive(cfg.CaseSensitive)
	if cfg.Action != "" {
		d.WithAction(cfg.Action)
	}
	if cfg.Severity != "" {
		d.WithSeverity(cfg.Severity)
	}
	return d
}

// BuildSanitizer creates a Sanitizer from config.
func BuildSanitizer(cfg *guardrails.SanitizerConfig) guardrails.InputGuardrail {
	s := NewSanitizer()
	s.TrimWhitespace(cfg.TrimWhitespace)
	s.NormalizeUnicode(cfg.NormalizeUnicode)
	s.StripHTML(cfg.StripHTML)
	if cfg.MaxLength > 0 {
		s.MaxLength(cfg.MaxLength)
	}
	return s
}
