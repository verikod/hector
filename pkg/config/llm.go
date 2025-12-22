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

import (
	"fmt"
	"os"
)

// LLMProvider identifies the LLM provider type.
type LLMProvider string

const (
	LLMProviderAnthropic LLMProvider = "anthropic"
	LLMProviderOpenAI    LLMProvider = "openai"
	LLMProviderGemini    LLMProvider = "gemini"
	LLMProviderOllama    LLMProvider = "ollama"
	LLMProviderDeepSeek  LLMProvider = "deepseek"
	LLMProviderGroq      LLMProvider = "groq"
	LLMProviderMistral   LLMProvider = "mistral"
	LLMProviderCohere    LLMProvider = "cohere"
)

// LLMConfig configures an LLM provider.
type LLMConfig struct {
	// Provider type (anthropic, openai, gemini, ollama).
	Provider LLMProvider `yaml:"provider,omitempty" json:"provider,omitempty" jsonschema:"title=Provider,description=LLM provider,enum=anthropic,enum=openai,enum=gemini,enum=ollama,default=anthropic"`

	// Model name (e.g., "claude-sonnet-4-20250514", "gpt-4o").
	Model string `yaml:"model,omitempty" json:"model,omitempty" jsonschema:"title=Model,description=Model identifier"`

	// APIKey for authentication. Supports ${VAR} expansion.
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty" jsonschema:"title=API Key,description=API key for authentication (use ${ENV_VAR})"`

	// BaseURL overrides the default API endpoint.
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty" jsonschema:"title=Base URL,description=Custom base URL for API endpoint"`

	// Temperature for generation (0.0 - 1.0).
	Temperature *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty" jsonschema:"title=Temperature,description=Sampling temperature,minimum=0,maximum=2,default=0.7"`

	// MaxTokens limits response length.
	MaxTokens int `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty" jsonschema:"title=Max Tokens,description=Maximum tokens to generate,minimum=1,default=4096"`

	// MaxToolOutputLength limits the length of tool outputs to prevent context overflow.
	// 0 means unlimited.
	MaxToolOutputLength int `yaml:"max_tool_output_length,omitempty" json:"max_tool_output_length,omitempty" jsonschema:"title=Max Tool Output Length,description=Maximum output length for tools tokens to avoid context length error,minimum=0,default=0"`

	// Thinking enables extended thinking (Claude).
	Thinking *ThinkingConfig `yaml:"thinking,omitempty" json:"thinking,omitempty" jsonschema:"title=Thinking Configuration,description=Extended thinking configuration (Claude)"`
}

// ThinkingConfig configures extended thinking (Claude).
type ThinkingConfig struct {
	// Enabled turns on extended thinking.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty" jsonschema:"title=Enabled,description=Enable extended thinking,default=true"`

	// BudgetTokens is the token budget for thinking.
	BudgetTokens int `yaml:"budget_tokens,omitempty" json:"budget_tokens,omitempty" jsonschema:"title=Budget Tokens,description=Token budget for thinking,minimum=1,default=1024"`
}

// SetDefaults applies default values.
func (c *LLMConfig) SetDefaults() {
	// Auto-detect provider from environment if not set
	if c.Provider == "" {
		c.Provider = detectProviderFromEnv()
	}

	// Set default model per provider
	if c.Model == "" {
		switch c.Provider {
		case LLMProviderAnthropic:
			c.Model = "claude-haiku-4-5"
		case LLMProviderOpenAI:
			c.Model = "gpt-5"
		case LLMProviderGemini:
			c.Model = "gemini-2.5-pro"
		case LLMProviderOllama:
			c.Model = "qwen3"
		}
	}

	// Get API key from environment if not set
	if c.APIKey == "" {
		c.APIKey = getAPIKeyFromEnv(c.Provider)
	}

	// Default temperature
	if c.Temperature == nil {
		temp := 0.7
		c.Temperature = &temp
	}

	// Default max tokens
	// NOTE: We do NOT set a default here anymore.
	// 0 means "use provider default" (which is often "infinity" for OpenAI).
	// Providers that require a max token limit (like Anthropic) handles 0 in their own New() method.

	// Default thinking config
	if c.Thinking != nil {
		if c.Thinking.Enabled == nil {
			c.Thinking.Enabled = BoolPtr(true)
		}
		if c.Thinking.BudgetTokens == 0 {
			c.Thinking.BudgetTokens = 1024
		}
	}
}

// Validate checks the LLM configuration.
func (c *LLMConfig) Validate() error {
	validProviders := map[LLMProvider]bool{
		LLMProviderAnthropic: true,
		LLMProviderOpenAI:    true,
		LLMProviderGemini:    true,
		LLMProviderOllama:    true,
	}

	if c.Provider != "" && !validProviders[c.Provider] {
		return fmt.Errorf("invalid provider %q (valid: anthropic, openai, gemini, ollama)", c.Provider)
	}

	// Ollama doesn't require API key
	if c.Provider != LLMProviderOllama && c.APIKey == "" {
		return fmt.Errorf("api_key is required for provider %q", c.Provider)
	}

	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		return fmt.Errorf("temperature must be between 0 and 2")
	}

	return nil
}

// detectProviderFromEnv detects provider based on available API keys.
func detectProviderFromEnv() LLMProvider {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return LLMProviderAnthropic
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return LLMProviderOpenAI
	}
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		return LLMProviderGemini
	}
	// Default to Ollama (no API key required)
	return LLMProviderOllama
}

// getAPIKeyFromEnv gets the API key for a provider from environment.
func getAPIKeyFromEnv(provider LLMProvider) string {
	switch provider {
	case LLMProviderAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY")
	case LLMProviderOpenAI:
		return os.Getenv("OPENAI_API_KEY")
	case LLMProviderGemini:
		if key := os.Getenv("GEMINI_API_KEY"); key != "" {
			return key
		}
		return os.Getenv("GOOGLE_API_KEY")
	case LLMProviderOllama:
		return "" // Ollama doesn't need API key
	default:
		return ""
	}
}
