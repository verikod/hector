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

// StudioConfigTemplate returns a secure, minimal config template for studio mode.
// Uses environment variable references instead of expanded values to avoid exposing secrets.
// Auto-detects which provider is available and generates appropriate template.
func StudioConfigTemplate() string {
	// Use existing detectProviderFromEnv to avoid code duplication
	provider := detectProviderFromEnv()

	switch provider {
	case LLMProviderAnthropic:
		return anthropicTemplate
	case LLMProviderOpenAI:
		return openaiTemplate
	case LLMProviderGemini:
		return geminiTemplate
	case LLMProviderOllama:
		return ollamaTemplate
	default:
		// Fallback to Ollama template (free, local, no API key needed)
		return ollamaTemplate
	}
}

// Template strings with env var placeholders for security

const anthropicTemplate = `# Hector Configuration - Anthropic/Claude
# Secure template with environment variable references
# See: https://docs.hector.dev/guides/configuration

name: "My Hector Setup"

llms:
  default:
    provider: anthropic
    api_key: ${ANTHROPIC_API_KEY}
    model: claude-haiku-4-5
    temperature: 0.7
    max_tokens: 4096

agents:
  assistant:
    name: "AI Assistant"
    llm: default
    description: "General-purpose AI assistant"
    instruction: "You are a helpful AI assistant."
    streaming: true
`

const openaiTemplate = `# Hector Configuration - OpenAI
# Secure template with environment variable references
# See: https://docs.hector.dev/guides/configuration

name: "My Hector Setup"

llms:
  default:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-5
    temperature: 0.7
    max_tokens: 4096

agents:
  assistant:
    name: "AI Assistant"
    llm: default
    description: "General-purpose AI assistant"
    instruction: "You are a helpful AI assistant."
    streaming: true
`

const geminiTemplate = `# Hector Configuration - Google Gemini
# Secure template with environment variable references
# See: https://docs.hector.dev/guides/configuration

name: "My Hector Setup"

llms:
  default:
    provider: gemini
    api_key: ${GEMINI_API_KEY}
    model: gemini-2.5-pro
    temperature: 0.7
    max_tokens: 4096

agents:
  assistant:
    name: "AI Assistant"
    llm: default
    description: "General-purpose AI assistant"
    instruction: "You are a helpful AI assistant."
    streaming: true
`

const ollamaTemplate = `# Hector Configuration - Ollama (Local)
# No API key needed for local Ollama
# See: https://docs.hector.dev/guides/configuration

name: "My Hector Setup"

llms:
  default:
    provider: ollama
    base_url: http://localhost:11434
    model: qwen3
    temperature: 0.7
    max_tokens: 4096

agents:
  assistant:
    name: "AI Assistant"
    llm: default
    description: "General-purpose AI assistant"
    instruction: "You are a helpful AI assistant."
    streaming: true
`
