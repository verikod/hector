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

package builder

import (
	"fmt"
	"os"
	"time"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/model/anthropic"
	"github.com/verikod/hector/pkg/model/gemini"
	"github.com/verikod/hector/pkg/model/ollama"
	"github.com/verikod/hector/pkg/model/openai"
)

// LLMBuilder provides a fluent API for building LLM providers.
//
// Example:
//
//	llm, err := builder.NewLLM("openai").
//	    Model("gpt-4o-mini").
//	    APIKeyFromEnv("OPENAI_API_KEY").
//	    Temperature(0.7).
//	    MaxTokens(4000).
//	    Build()
type LLMBuilder struct {
	providerType        string
	model               string
	apiKey              string
	baseURL             string
	temperature         *float64
	maxTokens           int
	timeout             time.Duration
	maxRetries          int
	enableThinking      bool
	thinkingBudget      int
	maxToolOutputLength int
}

// NewLLM creates a new LLM builder.
//
// Supported providers: "openai", "anthropic", "gemini", "ollama"
//
// Example:
//
//	llm, err := builder.NewLLM("anthropic").
//	    Model("claude-sonnet-4-20250514").
//	    APIKeyFromEnv("ANTHROPIC_API_KEY").
//	    Build()
func NewLLM(providerType string) *LLMBuilder {
	b := &LLMBuilder{
		providerType: providerType,
		maxRetries:   3,
		timeout:      120 * time.Second,
	}

	// Set provider-specific defaults
	switch providerType {
	case "openai":
		b.model = "gpt-4o-mini"
		b.baseURL = "https://api.openai.com/v1"
	case "anthropic":
		b.model = "claude-sonnet-4-20250514"
		b.baseURL = "https://api.anthropic.com"
	case "gemini":
		b.model = "gemini-2.0-flash"
	case "ollama":
		b.model = "qwen3"
		b.baseURL = "http://localhost:11434"
	}

	return b
}

// Model sets the model name.
//
// Example:
//
//	builder.NewLLM("openai").Model("gpt-4o")
func (b *LLMBuilder) Model(model string) *LLMBuilder {
	b.model = model
	return b
}

// APIKey sets the API key directly.
//
// Example:
//
//	builder.NewLLM("openai").APIKey("sk-...")
func (b *LLMBuilder) APIKey(key string) *LLMBuilder {
	b.apiKey = key
	return b
}

// APIKeyFromEnv sets the API key from an environment variable.
//
// Example:
//
//	builder.NewLLM("openai").APIKeyFromEnv("OPENAI_API_KEY")
func (b *LLMBuilder) APIKeyFromEnv(envVar string) *LLMBuilder {
	b.apiKey = os.Getenv(envVar)
	return b
}

// BaseURL sets the API base URL.
//
// Example:
//
//	builder.NewLLM("openai").BaseURL("https://api.custom.com/v1")
func (b *LLMBuilder) BaseURL(url string) *LLMBuilder {
	b.baseURL = url
	return b
}

// Temperature sets the sampling temperature (0.0 to 2.0).
//
// Example:
//
//	builder.NewLLM("openai").Temperature(0.7)
func (b *LLMBuilder) Temperature(temp float64) *LLMBuilder {
	if temp < 0 || temp > 2 {
		panic("temperature must be between 0 and 2")
	}
	b.temperature = &temp
	return b
}

// MaxTokens sets the maximum output tokens.
//
// Example:
//
//	builder.NewLLM("openai").MaxTokens(4000)
func (b *LLMBuilder) MaxTokens(max int) *LLMBuilder {
	if max < 0 {
		panic("max tokens must be non-negative")
	}
	b.maxTokens = max
	return b
}

// Timeout sets the request timeout.
//
// Example:
//
//	builder.NewLLM("openai").Timeout(2 * time.Minute)
func (b *LLMBuilder) Timeout(timeout time.Duration) *LLMBuilder {
	b.timeout = timeout
	return b
}

// MaxToolOutputLength sets the maximum length for tool outputs.
//
// Example:
//
//	builder.NewLLM("openai").MaxToolOutputLength(50000)
func (b *LLMBuilder) MaxToolOutputLength(max int) *LLMBuilder {
	if max < 0 {
		panic("max tool output length must be non-negative")
	}
	b.maxToolOutputLength = max
	return b
}

// MaxRetries sets the maximum number of retries.
//
// Example:
//
//	builder.NewLLM("openai").MaxRetries(5)
func (b *LLMBuilder) MaxRetries(max int) *LLMBuilder {
	if max < 0 {
		panic("max retries must be non-negative")
	}
	b.maxRetries = max
	return b
}

// EnableThinking enables thinking/reasoning mode.
// Supported by Anthropic (extended thinking) and OpenAI (o-series reasoning).
//
// Example:
//
//	builder.NewLLM("anthropic").
//	    Model("claude-sonnet-4-20250514").
//	    EnableThinking(true).
//	    ThinkingBudget(10000)
func (b *LLMBuilder) EnableThinking(enable bool) *LLMBuilder {
	b.enableThinking = enable
	return b
}

// ThinkingBudget sets the token budget for thinking.
//
// Example:
//
//	builder.NewLLM("anthropic").ThinkingBudget(10000)
func (b *LLMBuilder) ThinkingBudget(budget int) *LLMBuilder {
	b.thinkingBudget = budget
	return b
}

// Build creates the LLM provider.
//
// Returns an error if required parameters are missing or invalid.
func (b *LLMBuilder) Build() (model.LLM, error) {
	if b.model == "" {
		return nil, fmt.Errorf("model is required")
	}

	// Try to get API key from environment if not set
	if b.apiKey == "" {
		switch b.providerType {
		case "openai":
			b.apiKey = os.Getenv("OPENAI_API_KEY")
		case "anthropic":
			b.apiKey = os.Getenv("ANTHROPIC_API_KEY")
		case "gemini":
			b.apiKey = os.Getenv("GEMINI_API_KEY")
		case "ollama":
			// Ollama doesn't require API key
		}
	}

	switch b.providerType {
	case "openai":
		cfg := openai.Config{
			APIKey:      b.apiKey,
			Model:       b.model,
			MaxTokens:   b.maxTokens,
			Temperature: b.temperature,
			BaseURL:     b.baseURL,
			Timeout:     b.timeout,
			MaxRetries:  b.maxRetries,
		}
		if b.enableThinking {
			cfg.EnableReasoning = true
			cfg.ReasoningBudget = b.thinkingBudget
		}
		return openai.New(cfg)

	case "anthropic":
		cfg := anthropic.Config{
			APIKey:      b.apiKey,
			Model:       b.model,
			MaxTokens:   b.maxTokens,
			Temperature: b.temperature,
			BaseURL:     b.baseURL,
			Timeout:     b.timeout,
			MaxRetries:  b.maxRetries,
		}
		if b.enableThinking {
			cfg.EnableThinking = true
			cfg.ThinkingBudget = b.thinkingBudget
		}
		return anthropic.New(cfg)

	case "gemini":
		var temp float64
		if b.temperature != nil {
			temp = *b.temperature
		}
		return gemini.New(gemini.Config{
			APIKey:              b.apiKey,
			Model:               b.model,
			MaxTokens:           b.maxTokens,
			Temperature:         temp,
			MaxToolOutputLength: b.maxToolOutputLength,
		})

	case "ollama":
		cfg := ollama.Config{
			Model:       b.model,
			BaseURL:     b.baseURL,
			Temperature: b.temperature,
		}
		if b.maxTokens > 0 {
			cfg.NumPredict = &b.maxTokens
		}
		if b.enableThinking {
			cfg.EnableThinking = true
		}
		cfg.MaxToolOutputLength = b.maxToolOutputLength
		return ollama.New(cfg)

	default:
		return nil, fmt.Errorf("unknown provider type: %s (supported: openai, anthropic, gemini, ollama)", b.providerType)
	}
}

// MustBuild creates the LLM provider or panics on error.
//
// Use this only when you're certain the configuration is valid.
func (b *LLMBuilder) MustBuild() model.LLM {
	llm, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build LLM: %v", err))
	}
	return llm
}

// LLMFromConfig creates an LLMBuilder from a config.LLMConfig.
// This allows the configuration system to use the builder as its foundation.
//
// Example:
//
//	cfg := &config.LLMConfig{Provider: "openai", Model: "gpt-4o", APIKey: "sk-..."}
//	llm, err := builder.LLMFromConfig(cfg).Build()
func LLMFromConfig(cfg *config.LLMConfig) *LLMBuilder {
	if cfg == nil {
		return NewLLM("")
	}

	b := NewLLM(string(cfg.Provider))
	b.model = cfg.Model
	b.apiKey = cfg.APIKey
	b.maxTokens = cfg.MaxTokens
	b.temperature = cfg.Temperature
	b.maxToolOutputLength = cfg.MaxToolOutputLength

	if cfg.BaseURL != "" {
		b.baseURL = cfg.BaseURL
	}

	if cfg.Thinking != nil && config.BoolValue(cfg.Thinking.Enabled, false) {
		b.enableThinking = true
		b.thinkingBudget = cfg.Thinking.BudgetTokens
	}

	return b
}
