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

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the YAML/JSON configuration for guardrails.
type Config struct {
	// Input guardrail configurations.
	Input InputConfig `json:"input" yaml:"input"`

	// Output guardrail configurations.
	Output OutputConfig `json:"output" yaml:"output"`

	// Tool guardrail configurations.
	Tool ToolConfig `json:"tool" yaml:"tool"`
}

// InputConfig contains input guardrail settings.
type InputConfig struct {
	// ChainMode for input guardrails.
	ChainMode ChainMode `json:"chain_mode" yaml:"chain_mode"`

	// Length validation settings.
	Length *LengthConfig `json:"length,omitempty" yaml:"length,omitempty"`

	// Injection detection settings.
	Injection *InjectionConfig `json:"injection,omitempty" yaml:"injection,omitempty"`

	// Sanitization settings.
	Sanitizer *SanitizerConfig `json:"sanitizer,omitempty" yaml:"sanitizer,omitempty"`
}

// LengthConfig configures input length validation.
type LengthConfig struct {
	Enabled   bool     `json:"enabled" yaml:"enabled"`
	MinLength int      `json:"min_length" yaml:"min_length"`
	MaxLength int      `json:"max_length" yaml:"max_length"`
	Action    Action   `json:"action" yaml:"action"`
	Severity  Severity `json:"severity" yaml:"severity"`
}

// InjectionConfig configures prompt injection detection.
type InjectionConfig struct {
	Enabled       bool     `json:"enabled" yaml:"enabled"`
	Patterns      []string `json:"patterns,omitempty" yaml:"patterns,omitempty"`
	CaseSensitive bool     `json:"case_sensitive" yaml:"case_sensitive"`
	Action        Action   `json:"action" yaml:"action"`
	Severity      Severity `json:"severity" yaml:"severity"`
}

// SanitizerConfig configures input sanitization.
type SanitizerConfig struct {
	Enabled          bool `json:"enabled" yaml:"enabled"`
	TrimWhitespace   bool `json:"trim_whitespace" yaml:"trim_whitespace"`
	NormalizeUnicode bool `json:"normalize_unicode" yaml:"normalize_unicode"`
	MaxLength        int  `json:"max_length" yaml:"max_length"`
	StripHTML        bool `json:"strip_html" yaml:"strip_html"`
}

// OutputConfig contains output guardrail settings.
type OutputConfig struct {
	// ChainMode for output guardrails.
	ChainMode ChainMode `json:"chain_mode" yaml:"chain_mode"`

	// PII detection/redaction settings.
	PII *PIIConfig `json:"pii,omitempty" yaml:"pii,omitempty"`

	// Content filtering settings.
	Content *ContentConfig `json:"content,omitempty" yaml:"content,omitempty"`
}

// PIIConfig configures PII detection and redaction.
type PIIConfig struct {
	Enabled          bool       `json:"enabled" yaml:"enabled"`
	DetectEmail      bool       `json:"detect_email" yaml:"detect_email"`
	DetectPhone      bool       `json:"detect_phone" yaml:"detect_phone"`
	DetectSSN        bool       `json:"detect_ssn" yaml:"detect_ssn"`
	DetectCreditCard bool       `json:"detect_credit_card" yaml:"detect_credit_card"`
	RedactMode       RedactMode `json:"redact_mode" yaml:"redact_mode"`
	Action           Action     `json:"action" yaml:"action"`
	Severity         Severity   `json:"severity" yaml:"severity"`
}

// RedactMode determines how PII is redacted.
type RedactMode string

const (
	// RedactModeMask replaces PII with asterisks.
	RedactModeMask RedactMode = "mask"
	// RedactModeRemove removes PII entirely.
	RedactModeRemove RedactMode = "remove"
	// RedactModeHash replaces PII with a hash.
	RedactModeHash RedactMode = "hash"
)

// ContentConfig configures content filtering.
type ContentConfig struct {
	Enabled         bool     `json:"enabled" yaml:"enabled"`
	BlockedKeywords []string `json:"blocked_keywords,omitempty" yaml:"blocked_keywords,omitempty"`
	BlockedPatterns []string `json:"blocked_patterns,omitempty" yaml:"blocked_patterns,omitempty"`
	Action          Action   `json:"action" yaml:"action"`
	Severity        Severity `json:"severity" yaml:"severity"`
}

// ToolConfig contains tool guardrail settings.
type ToolConfig struct {
	// ChainMode for tool guardrails.
	ChainMode ChainMode `json:"chain_mode" yaml:"chain_mode"`

	// Authorization settings.
	Authorization *AuthorizationConfig `json:"authorization,omitempty" yaml:"authorization,omitempty"`
}

// AuthorizationConfig configures tool authorization.
type AuthorizationConfig struct {
	Enabled      bool     `json:"enabled" yaml:"enabled"`
	AllowedTools []string `json:"allowed_tools,omitempty" yaml:"allowed_tools,omitempty"`
	BlockedTools []string `json:"blocked_tools,omitempty" yaml:"blocked_tools,omitempty"`
	Action       Action   `json:"action" yaml:"action"`
	Severity     Severity `json:"severity" yaml:"severity"`
}

// LoadConfig loads a guardrails configuration from a YAML file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfig saves a guardrails configuration to a YAML file.
func SaveConfig(config *Config, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() *Config {
	return &Config{
		Input: InputConfig{
			ChainMode: ChainModeFailFast,
			Length: &LengthConfig{
				Enabled:   true,
				MinLength: 1,
				MaxLength: 100000,
				Action:    ActionBlock,
				Severity:  SeverityMedium,
			},
			Injection: &InjectionConfig{
				Enabled:       true,
				CaseSensitive: false,
				Action:        ActionBlock,
				Severity:      SeverityHigh,
			},
			Sanitizer: &SanitizerConfig{
				Enabled:        true,
				TrimWhitespace: true,
			},
		},
		Output: OutputConfig{
			ChainMode: ChainModeFailFast,
			PII: &PIIConfig{
				Enabled:          true,
				DetectEmail:      true,
				DetectPhone:      true,
				DetectSSN:        true,
				DetectCreditCard: true,
				RedactMode:       RedactModeMask,
				Action:           ActionModify,
				Severity:         SeverityHigh,
			},
		},
		Tool: ToolConfig{
			ChainMode: ChainModeFailFast,
		},
	}
}

// BuildInputChain creates an InputChain from the configuration.
// Import the input package separately: import "github.com/kadirpekel/hector/pkg/guardrails/input"
func (c *Config) BuildInputChain(builders InputChainBuilders) *InputChain {
	var guardrails []InputGuardrail

	// Add length validator
	if c.Input.Length != nil && c.Input.Length.Enabled && builders.LengthValidator != nil {
		guardrails = append(guardrails, builders.LengthValidator(c.Input.Length))
	}

	// Add injection detector
	if c.Input.Injection != nil && c.Input.Injection.Enabled && builders.InjectionDetector != nil {
		guardrails = append(guardrails, builders.InjectionDetector(c.Input.Injection))
	}

	// Add sanitizer
	if c.Input.Sanitizer != nil && c.Input.Sanitizer.Enabled && builders.Sanitizer != nil {
		guardrails = append(guardrails, builders.Sanitizer(c.Input.Sanitizer))
	}

	chain := NewInputChain(guardrails...)
	if c.Input.ChainMode != "" {
		chain.WithMode(c.Input.ChainMode)
	}
	return chain
}

// BuildOutputChain creates an OutputChain from the configuration.
func (c *Config) BuildOutputChain(builders OutputChainBuilders) *OutputChain {
	var guardrails []OutputGuardrail

	// Add PII redactor
	if c.Output.PII != nil && c.Output.PII.Enabled && builders.PIIRedactor != nil {
		guardrails = append(guardrails, builders.PIIRedactor(c.Output.PII))
	}

	// Add content filter
	if c.Output.Content != nil && c.Output.Content.Enabled && builders.ContentFilter != nil {
		guardrails = append(guardrails, builders.ContentFilter(c.Output.Content))
	}

	chain := NewOutputChain(guardrails...)
	if c.Output.ChainMode != "" {
		chain.WithMode(c.Output.ChainMode)
	}
	return chain
}

// BuildToolChain creates a ToolChain from the configuration.
func (c *Config) BuildToolChain(builders ToolChainBuilders) *ToolChain {
	var guardrails []ToolGuardrail

	// Add authorizer
	if c.Tool.Authorization != nil && c.Tool.Authorization.Enabled && builders.Authorizer != nil {
		guardrails = append(guardrails, builders.Authorizer(c.Tool.Authorization))
	}

	chain := NewToolChain(guardrails...)
	if c.Tool.ChainMode != "" {
		chain.WithMode(c.Tool.ChainMode)
	}
	return chain
}

// InputChainBuilders provides factory functions to create input guardrails from config.
// This decouples config from the input package to avoid circular imports.
type InputChainBuilders struct {
	LengthValidator   func(*LengthConfig) InputGuardrail
	InjectionDetector func(*InjectionConfig) InputGuardrail
	Sanitizer         func(*SanitizerConfig) InputGuardrail
}

// OutputChainBuilders provides factory functions to create output guardrails from config.
type OutputChainBuilders struct {
	PIIRedactor   func(*PIIConfig) OutputGuardrail
	ContentFilter func(*ContentConfig) OutputGuardrail
}

// ToolChainBuilders provides factory functions to create tool guardrails from config.
type ToolChainBuilders struct {
	Authorizer func(*AuthorizationConfig) ToolGuardrail
}

