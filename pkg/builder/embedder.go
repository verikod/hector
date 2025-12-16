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
	"github.com/verikod/hector/pkg/embedder"
)

// EmbedderBuilder provides a fluent API for building embedders.
//
// Embedders convert text to vector embeddings for semantic search.
// They're used by memory systems and RAG components.
//
// Example:
//
//	emb, err := builder.NewEmbedder("openai").
//	    Model("text-embedding-3-small").
//	    APIKeyFromEnv("OPENAI_API_KEY").
//	    Build()
type EmbedderBuilder struct {
	providerType string
	model        string
	apiKey       string
	baseURL      string
	dimension    int

	// Advanced options
	timeout         int // seconds
	batchSize       int
	encodingFormat  string // OpenAI: "float", "base64"
	user            string // OpenAI: end-user identifier
	inputType       string // Cohere: "search_document", "search_query", etc.
	outputDimension int    // Cohere v4+: 256, 512, 1024, 1536
	truncate        string // Cohere: "NONE", "START", "END"
}

// NewEmbedder creates a new embedder builder.
//
// Supported providers: "openai", "ollama", "cohere"
//
// Example:
//
//	emb, err := builder.NewEmbedder("openai").
//	    Model("text-embedding-3-small").
//	    APIKeyFromEnv("OPENAI_API_KEY").
//	    Build()
func NewEmbedder(providerType string) *EmbedderBuilder {
	b := &EmbedderBuilder{
		providerType: providerType,
	}

	// Set provider-specific defaults
	switch providerType {
	case "openai":
		b.model = "text-embedding-3-small"
		b.baseURL = "https://api.openai.com/v1"
		b.dimension = 1536
	case "ollama":
		b.model = "nomic-embed-text"
		b.baseURL = "http://localhost:11434"
		b.dimension = 768
	case "cohere":
		b.model = "embed-english-v3.0"
		b.dimension = 1024
	}

	return b
}

// Model sets the embedding model name.
//
// Example:
//
//	builder.NewEmbedder("openai").Model("text-embedding-3-large")
func (b *EmbedderBuilder) Model(model string) *EmbedderBuilder {
	b.model = model
	return b
}

// APIKey sets the API key directly.
//
// Example:
//
//	builder.NewEmbedder("openai").APIKey("sk-...")
func (b *EmbedderBuilder) APIKey(key string) *EmbedderBuilder {
	b.apiKey = key
	return b
}

// APIKeyFromEnv sets the API key from an environment variable.
//
// Example:
//
//	builder.NewEmbedder("openai").APIKeyFromEnv("OPENAI_API_KEY")
func (b *EmbedderBuilder) APIKeyFromEnv(envVar string) *EmbedderBuilder {
	b.apiKey = os.Getenv(envVar)
	return b
}

// BaseURL sets the API base URL.
//
// Example:
//
//	builder.NewEmbedder("openai").BaseURL("https://api.custom.com/v1")
func (b *EmbedderBuilder) BaseURL(url string) *EmbedderBuilder {
	b.baseURL = url
	return b
}

// Dimension sets the expected embedding dimension.
// This is usually auto-detected but can be overridden.
//
// Example:
//
//	builder.NewEmbedder("openai").Dimension(3072)
func (b *EmbedderBuilder) Dimension(dim int) *EmbedderBuilder {
	if dim <= 0 {
		panic("dimension must be positive")
	}
	b.dimension = dim
	return b
}

// Timeout sets the API request timeout in seconds.
//
// Example:
//
//	builder.NewEmbedder("openai").Timeout(60)
func (b *EmbedderBuilder) Timeout(seconds int) *EmbedderBuilder {
	b.timeout = seconds
	return b
}

// BatchSize sets the batch size for embedding requests.
//
// Example:
//
//	builder.NewEmbedder("openai").BatchSize(50)
func (b *EmbedderBuilder) BatchSize(size int) *EmbedderBuilder {
	b.batchSize = size
	return b
}

// EncodingFormat sets the encoding format for OpenAI API.
// Values: "float" (default), "base64"
//
// Example:
//
//	builder.NewEmbedder("openai").EncodingFormat("float")
func (b *EmbedderBuilder) EncodingFormat(format string) *EmbedderBuilder {
	b.encodingFormat = format
	return b
}

// User sets the end-user identifier for OpenAI API.
//
// Example:
//
//	builder.NewEmbedder("openai").User("user-123")
func (b *EmbedderBuilder) User(user string) *EmbedderBuilder {
	b.user = user
	return b
}

// InputType sets the input type for Cohere v3+ models.
// Values: "search_document", "search_query", "classification", "clustering"
//
// Example:
//
//	builder.NewEmbedder("cohere").InputType("search_document")
func (b *EmbedderBuilder) InputType(inputType string) *EmbedderBuilder {
	b.inputType = inputType
	return b
}

// OutputDimension sets the output dimension for Cohere v4+ models.
// Values: 256, 512, 1024, 1536
//
// Example:
//
//	builder.NewEmbedder("cohere").OutputDimension(1024)
func (b *EmbedderBuilder) OutputDimension(dim int) *EmbedderBuilder {
	b.outputDimension = dim
	return b
}

// Truncate sets the truncation strategy for Cohere API.
// Values: "NONE", "START", "END" (default: "END")
//
// Example:
//
//	builder.NewEmbedder("cohere").Truncate("END")
func (b *EmbedderBuilder) Truncate(truncate string) *EmbedderBuilder {
	b.truncate = truncate
	return b
}

// Build creates the embedder.
//
// Returns an error if required parameters are missing or invalid.
func (b *EmbedderBuilder) Build() (embedder.Embedder, error) {
	if b.model == "" {
		return nil, fmt.Errorf("model is required")
	}

	// Try to get API key from environment if not set
	if b.apiKey == "" {
		switch b.providerType {
		case "openai":
			b.apiKey = os.Getenv("OPENAI_API_KEY")
		case "cohere":
			b.apiKey = os.Getenv("COHERE_API_KEY")
		case "ollama":
			// Ollama doesn't require API key
		}
	}

	// Convert timeout to duration
	var timeout time.Duration
	if b.timeout > 0 {
		timeout = time.Duration(b.timeout) * time.Second
	}

	switch b.providerType {
	case "openai":
		return embedder.NewOpenAIEmbedder(embedder.OpenAIConfig{
			APIKey:         b.apiKey,
			Model:          b.model,
			BaseURL:        b.baseURL,
			Dimension:      b.dimension,
			Timeout:        timeout,
			BatchSize:      b.batchSize,
			EncodingFormat: b.encodingFormat,
			User:           b.user,
		})

	case "ollama":
		return embedder.NewOllamaEmbedder(embedder.OllamaConfig{
			Model:     b.model,
			BaseURL:   b.baseURL,
			Dimension: b.dimension,
			Timeout:   timeout,
		})

	case "cohere":
		cfg := embedder.CohereConfig{
			APIKey:    b.apiKey,
			Model:     b.model,
			BaseURL:   b.baseURL,
			Dimension: b.dimension,
			Timeout:   timeout,
			BatchSize: b.batchSize,
			InputType: b.inputType,
			Truncate:  b.truncate,
		}
		if b.outputDimension > 0 {
			cfg.OutputDimension = &b.outputDimension
		}
		return embedder.NewCohereEmbedder(cfg)

	default:
		return nil, fmt.Errorf("unknown embedder provider: %s (supported: openai, ollama, cohere)", b.providerType)
	}
}

// MustBuild creates the embedder or panics on error.
//
// Use this only when you're certain the configuration is valid.
func (b *EmbedderBuilder) MustBuild() embedder.Embedder {
	emb, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build embedder: %v", err))
	}
	return emb
}

// EmbedderFromConfig creates an EmbedderBuilder from a config.EmbedderConfig.
// This allows the configuration system to use the builder as its foundation.
//
// Example:
//
//	cfg := &config.EmbedderConfig{Provider: "openai", Model: "text-embedding-3-small"}
//	emb, err := builder.EmbedderFromConfig(cfg).Build()
func EmbedderFromConfig(cfg *config.EmbedderConfig) *EmbedderBuilder {
	if cfg == nil {
		return NewEmbedder("")
	}

	// Apply defaults and validation
	cfg.SetDefaults()

	b := NewEmbedder(cfg.Provider)
	b.model = cfg.Model
	b.apiKey = cfg.APIKey

	if cfg.BaseURL != "" {
		b.baseURL = cfg.BaseURL
	}
	if cfg.Dimension > 0 {
		b.dimension = cfg.Dimension
	}
	if cfg.Timeout > 0 {
		b.timeout = cfg.Timeout
	}
	if cfg.BatchSize > 0 {
		b.batchSize = cfg.BatchSize
	}
	if cfg.EncodingFormat != "" {
		b.encodingFormat = cfg.EncodingFormat
	}
	if cfg.User != "" {
		b.user = cfg.User
	}
	if cfg.InputType != "" {
		b.inputType = cfg.InputType
	}
	if cfg.OutputDimension > 0 {
		b.outputDimension = cfg.OutputDimension
	}
	if cfg.Truncate != "" {
		b.truncate = cfg.Truncate
	}

	return b
}
