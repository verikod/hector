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

package config

import "fmt"

// GuardrailsConfig contains guardrails configuration.
// Guardrails provide safety controls for agent inputs, outputs, and tool calls.
type GuardrailsConfig struct {
	// Enabled controls whether guardrails are active.
	Enabled bool `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,description=Whether guardrails are active,default=true"`

	// Input guardrail configurations.
	Input *InputGuardrailsConfig `yaml:"input,omitempty" json:"input,omitempty" jsonschema:"title=Input Guardrails,description=Input validation and sanitization settings"`

	// Output guardrail configurations.
	Output *OutputGuardrailsConfig `yaml:"output,omitempty" json:"output,omitempty" jsonschema:"title=Output Guardrails,description=Output filtering and redaction settings"`

	// Tool guardrail configurations.
	Tool *ToolGuardrailsConfig `yaml:"tool,omitempty" json:"tool,omitempty" jsonschema:"title=Tool Guardrails,description=Tool authorization settings"`
}

// InputGuardrailsConfig contains input guardrail settings.
type InputGuardrailsConfig struct {
	// ChainMode for input guardrails: "fail_fast" or "collect_all".
	ChainMode string `yaml:"chain_mode,omitempty" json:"chain_mode,omitempty" jsonschema:"title=Chain Mode,description=How to handle multiple guardrails,enum=fail_fast,enum=collect_all,default=fail_fast"`

	// Length validation settings.
	Length *LengthGuardrailConfig `yaml:"length,omitempty" json:"length,omitempty" jsonschema:"title=Length Validation,description=Input length constraints"`

	// Injection detection settings.
	Injection *InjectionGuardrailConfig `yaml:"injection,omitempty" json:"injection,omitempty" jsonschema:"title=Injection Detection,description=Prompt injection protection"`

	// Sanitization settings.
	Sanitizer *SanitizerGuardrailConfig `yaml:"sanitizer,omitempty" json:"sanitizer,omitempty" jsonschema:"title=Input Sanitization,description=Input cleaning and normalization"`

	// Pattern validation settings.
	Pattern *PatternGuardrailConfig `yaml:"pattern,omitempty" json:"pattern,omitempty" jsonschema:"title=Pattern Validation,description=Regex-based validation"`
}

// LengthGuardrailConfig configures input length validation.
type LengthGuardrailConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,default=true"`
	MinLength int    `yaml:"min_length,omitempty" json:"min_length,omitempty" jsonschema:"title=Minimum Length,default=1"`
	MaxLength int    `yaml:"max_length,omitempty" json:"max_length,omitempty" jsonschema:"title=Maximum Length,default=100000"`
	Action    string `yaml:"action,omitempty" json:"action,omitempty" jsonschema:"title=Action,enum=allow,enum=block,enum=warn,default=block"`
	Severity  string `yaml:"severity,omitempty" json:"severity,omitempty" jsonschema:"title=Severity,enum=low,enum=medium,enum=high,enum=critical,default=medium"`
}

// InjectionGuardrailConfig configures prompt injection detection.
type InjectionGuardrailConfig struct {
	Enabled       bool     `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,default=true"`
	Patterns      []string `yaml:"patterns,omitempty" json:"patterns,omitempty" jsonschema:"title=Custom Patterns,description=Additional regex patterns to detect"`
	CaseSensitive bool     `yaml:"case_sensitive,omitempty" json:"case_sensitive,omitempty" jsonschema:"title=Case Sensitive,default=false"`
	Action        string   `yaml:"action,omitempty" json:"action,omitempty" jsonschema:"title=Action,enum=allow,enum=block,enum=warn,default=block"`
	Severity      string   `yaml:"severity,omitempty" json:"severity,omitempty" jsonschema:"title=Severity,enum=low,enum=medium,enum=high,enum=critical,default=high"`
}

// SanitizerGuardrailConfig configures input sanitization.
type SanitizerGuardrailConfig struct {
	Enabled          bool `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,default=true"`
	TrimWhitespace   bool `yaml:"trim_whitespace,omitempty" json:"trim_whitespace,omitempty" jsonschema:"title=Trim Whitespace,default=true"`
	NormalizeUnicode bool `yaml:"normalize_unicode,omitempty" json:"normalize_unicode,omitempty" jsonschema:"title=Normalize Unicode,default=false"`
	MaxLength        int  `yaml:"max_length,omitempty" json:"max_length,omitempty" jsonschema:"title=Max Length,description=Truncate if exceeded (0=no limit)"`
	StripHTML        bool `yaml:"strip_html,omitempty" json:"strip_html,omitempty" jsonschema:"title=Strip HTML,default=false"`
}

// PatternGuardrailConfig configures pattern-based validation.
type PatternGuardrailConfig struct {
	Enabled       bool     `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,default=false"`
	AllowPatterns []string `yaml:"allow_patterns,omitempty" json:"allow_patterns,omitempty" jsonschema:"title=Allow Patterns,description=Patterns that input must match"`
	BlockPatterns []string `yaml:"block_patterns,omitempty" json:"block_patterns,omitempty" jsonschema:"title=Block Patterns,description=Patterns that input must NOT match"`
	Action        string   `yaml:"action,omitempty" json:"action,omitempty" jsonschema:"title=Action,enum=allow,enum=block,enum=warn,default=block"`
	Severity      string   `yaml:"severity,omitempty" json:"severity,omitempty" jsonschema:"title=Severity,enum=low,enum=medium,enum=high,enum=critical,default=medium"`
}

// OutputGuardrailsConfig contains output guardrail settings.
type OutputGuardrailsConfig struct {
	// ChainMode for output guardrails: "fail_fast" or "collect_all".
	ChainMode string `yaml:"chain_mode,omitempty" json:"chain_mode,omitempty" jsonschema:"title=Chain Mode,enum=fail_fast,enum=collect_all,default=fail_fast"`

	// PII detection/redaction settings.
	PII *PIIGuardrailConfig `yaml:"pii,omitempty" json:"pii,omitempty" jsonschema:"title=PII Detection,description=Detect and redact personally identifiable information"`

	// Content filtering settings.
	Content *ContentGuardrailConfig `yaml:"content,omitempty" json:"content,omitempty" jsonschema:"title=Content Filter,description=Block or warn about harmful content"`
}

// PIIGuardrailConfig configures PII detection and redaction.
type PIIGuardrailConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,default=true"`
	DetectEmail      bool   `yaml:"detect_email,omitempty" json:"detect_email,omitempty" jsonschema:"title=Detect Email,default=true"`
	DetectPhone      bool   `yaml:"detect_phone,omitempty" json:"detect_phone,omitempty" jsonschema:"title=Detect Phone,default=true"`
	DetectSSN        bool   `yaml:"detect_ssn,omitempty" json:"detect_ssn,omitempty" jsonschema:"title=Detect SSN,default=true"`
	DetectCreditCard bool   `yaml:"detect_credit_card,omitempty" json:"detect_credit_card,omitempty" jsonschema:"title=Detect Credit Card,default=true"`
	RedactMode       string `yaml:"redact_mode,omitempty" json:"redact_mode,omitempty" jsonschema:"title=Redact Mode,enum=mask,enum=remove,enum=hash,default=mask"`
	Action           string `yaml:"action,omitempty" json:"action,omitempty" jsonschema:"title=Action,enum=allow,enum=block,enum=modify,enum=warn,default=modify"`
	Severity         string `yaml:"severity,omitempty" json:"severity,omitempty" jsonschema:"title=Severity,enum=low,enum=medium,enum=high,enum=critical,default=high"`
}

// ContentGuardrailConfig configures content filtering.
type ContentGuardrailConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,default=false"`
	BlockedKeywords []string `yaml:"blocked_keywords,omitempty" json:"blocked_keywords,omitempty" jsonschema:"title=Blocked Keywords,description=Case-insensitive keywords to block"`
	BlockedPatterns []string `yaml:"blocked_patterns,omitempty" json:"blocked_patterns,omitempty" jsonschema:"title=Blocked Patterns,description=Regex patterns to block"`
	Action          string   `yaml:"action,omitempty" json:"action,omitempty" jsonschema:"title=Action,enum=allow,enum=block,enum=warn,default=block"`
	Severity        string   `yaml:"severity,omitempty" json:"severity,omitempty" jsonschema:"title=Severity,enum=low,enum=medium,enum=high,enum=critical,default=high"`
}

// ToolGuardrailsConfig contains tool guardrail settings.
type ToolGuardrailsConfig struct {
	// ChainMode for tool guardrails: "fail_fast" or "collect_all".
	ChainMode string `yaml:"chain_mode,omitempty" json:"chain_mode,omitempty" jsonschema:"title=Chain Mode,enum=fail_fast,enum=collect_all,default=fail_fast"`

	// Authorization settings.
	Authorization *AuthorizationGuardrailConfig `yaml:"authorization,omitempty" json:"authorization,omitempty" jsonschema:"title=Tool Authorization,description=Control which tools can be called"`
}

// AuthorizationGuardrailConfig configures tool authorization.
type AuthorizationGuardrailConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled" jsonschema:"title=Enabled,default=false"`
	AllowedTools []string `yaml:"allowed_tools,omitempty" json:"allowed_tools,omitempty" jsonschema:"title=Allowed Tools,description=Whitelist of allowed tools (glob patterns)"`
	BlockedTools []string `yaml:"blocked_tools,omitempty" json:"blocked_tools,omitempty" jsonschema:"title=Blocked Tools,description=Blacklist of blocked tools (glob patterns)"`
	Action       string   `yaml:"action,omitempty" json:"action,omitempty" jsonschema:"title=Action,enum=allow,enum=block,enum=warn,default=block"`
	Severity     string   `yaml:"severity,omitempty" json:"severity,omitempty" jsonschema:"title=Severity,enum=low,enum=medium,enum=high,enum=critical,default=high"`
}

// SetDefaults applies default values to the guardrails config.
func (c *GuardrailsConfig) SetDefaults() {
	if c.Input == nil {
		c.Input = &InputGuardrailsConfig{}
	}
	if c.Input.ChainMode == "" {
		c.Input.ChainMode = "fail_fast"
	}

	if c.Output == nil {
		c.Output = &OutputGuardrailsConfig{}
	}
	if c.Output.ChainMode == "" {
		c.Output.ChainMode = "fail_fast"
	}

	if c.Tool == nil {
		c.Tool = &ToolGuardrailsConfig{}
	}
	if c.Tool.ChainMode == "" {
		c.Tool.ChainMode = "fail_fast"
	}
}

// Validate checks the guardrails configuration for errors.
func (c *GuardrailsConfig) Validate() error {
	// Validate chain modes
	validModes := map[string]bool{"fail_fast": true, "collect_all": true, "": true}

	if c.Input != nil && !validModes[c.Input.ChainMode] {
		return fmt.Errorf("invalid input chain_mode: %q", c.Input.ChainMode)
	}
	if c.Output != nil && !validModes[c.Output.ChainMode] {
		return fmt.Errorf("invalid output chain_mode: %q", c.Output.ChainMode)
	}
	if c.Tool != nil && !validModes[c.Tool.ChainMode] {
		return fmt.Errorf("invalid tool chain_mode: %q", c.Tool.ChainMode)
	}

	return nil
}

// DefaultGuardrailsConfig returns sensible default guardrails configuration.
func DefaultGuardrailsConfig() *GuardrailsConfig {
	return &GuardrailsConfig{
		Enabled: true,
		Input: &InputGuardrailsConfig{
			ChainMode: "fail_fast",
			Length: &LengthGuardrailConfig{
				Enabled:   true,
				MinLength: 1,
				MaxLength: 100000,
				Action:    "block",
				Severity:  "medium",
			},
			Injection: &InjectionGuardrailConfig{
				Enabled:       true,
				CaseSensitive: false,
				Action:        "block",
				Severity:      "high",
			},
			Sanitizer: &SanitizerGuardrailConfig{
				Enabled:        true,
				TrimWhitespace: true,
			},
		},
		Output: &OutputGuardrailsConfig{
			ChainMode: "fail_fast",
			PII: &PIIGuardrailConfig{
				Enabled:          true,
				DetectEmail:      true,
				DetectPhone:      true,
				DetectSSN:        true,
				DetectCreditCard: true,
				RedactMode:       "mask",
				Action:           "modify",
				Severity:         "high",
			},
		},
		Tool: &ToolGuardrailsConfig{
			ChainMode: "fail_fast",
		},
	}
}
