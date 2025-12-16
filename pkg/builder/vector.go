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

	"github.com/verikod/hector/pkg/vector"
)

// VectorProviderBuilder provides a fluent API for building vector database providers.
//
// Vector providers store and search embeddings for RAG and memory systems.
//
// Example:
//
//	provider, err := builder.NewVectorProvider("chromem").
//	    PersistPath(".hector/vectors").
//	    Compress(true).
//	    Build()
type VectorProviderBuilder struct {
	providerType string

	// Chromem options
	persistPath string
	compress    bool

	// Qdrant options
	qdrantHost   string
	qdrantPort   int
	qdrantAPIKey string
	qdrantUseTLS bool

	// Chroma options
	chromaHost   string
	chromaPort   int
	chromaAPIKey string
	chromaUseTLS bool

	// Pinecone options
	pineconeAPIKey string
	pineconeIndex  string

	// Milvus options
	milvusHost   string
	milvusPort   int
	milvusAPIKey string
	milvusUseTLS bool

	// Weaviate options
	weaviateHost   string
	weaviatePort   int
	weaviateAPIKey string
	weaviateUseTLS bool
}

// NewVectorProvider creates a new vector provider builder.
//
// Supported providers: "chromem", "qdrant", "chroma", "pinecone", "milvus", "weaviate"
//
// Example:
//
//	// Local embedded provider (chromem)
//	provider, err := builder.NewVectorProvider("chromem").
//	    PersistPath(".hector/vectors").
//	    Build()
//
//	// Cloud provider (Qdrant)
//	provider, err := builder.NewVectorProvider("qdrant").
//	    Host("localhost").
//	    Port(6334).
//	    Build()
func NewVectorProvider(providerType string) *VectorProviderBuilder {
	b := &VectorProviderBuilder{
		providerType: providerType,
	}

	// Set provider-specific defaults
	switch providerType {
	case "chromem", "":
		b.persistPath = ".hector/vectors"
		b.compress = true
	case "qdrant":
		b.qdrantHost = "localhost"
		b.qdrantPort = 6334 // Qdrant gRPC port (REST is 6333)
	case "chroma":
		b.chromaHost = "localhost"
		b.chromaPort = 8000
	case "milvus":
		b.milvusHost = "localhost"
		b.milvusPort = 19530
	case "weaviate":
		b.weaviateHost = "localhost"
		b.weaviatePort = 8080
	}

	return b
}

// PersistPath sets the file path for persistent storage (chromem).
//
// Example:
//
//	builder.NewVectorProvider("chromem").PersistPath(".hector/vectors")
func (b *VectorProviderBuilder) PersistPath(path string) *VectorProviderBuilder {
	b.persistPath = path
	return b
}

// Compress enables/disables compression for persistent storage (chromem).
//
// Example:
//
//	builder.NewVectorProvider("chromem").Compress(true)
func (b *VectorProviderBuilder) Compress(compress bool) *VectorProviderBuilder {
	b.compress = compress
	return b
}

// Host sets the server host for remote providers.
//
// Example:
//
//	builder.NewVectorProvider("qdrant").Host("qdrant.example.com")
func (b *VectorProviderBuilder) Host(host string) *VectorProviderBuilder {
	b.qdrantHost = host
	b.chromaHost = host
	b.milvusHost = host
	b.weaviateHost = host
	return b
}

// Port sets the server port for remote providers.
//
// Example:
//
//	builder.NewVectorProvider("qdrant").Port(6333)
func (b *VectorProviderBuilder) Port(port int) *VectorProviderBuilder {
	if port <= 0 {
		panic("port must be positive")
	}
	b.qdrantPort = port
	b.chromaPort = port
	b.milvusPort = port
	b.weaviatePort = port
	return b
}

// APIKey sets the API key for cloud providers.
//
// Example:
//
//	builder.NewVectorProvider("pinecone").APIKey("pk-...")
func (b *VectorProviderBuilder) APIKey(key string) *VectorProviderBuilder {
	b.qdrantAPIKey = key
	b.pineconeAPIKey = key
	b.weaviateAPIKey = key
	b.milvusAPIKey = key
	b.chromaAPIKey = key
	return b
}

// UseTLS enables TLS for secure connections.
//
// Example:
//
//	builder.NewVectorProvider("qdrant").UseTLS(true)
func (b *VectorProviderBuilder) UseTLS(useTLS bool) *VectorProviderBuilder {
	b.qdrantUseTLS = useTLS
	b.milvusUseTLS = useTLS
	b.weaviateUseTLS = useTLS
	b.chromaUseTLS = useTLS
	return b
}

// IndexName sets the index name (Pinecone).
//
// Example:
//
//	builder.NewVectorProvider("pinecone").IndexName("my-index")
func (b *VectorProviderBuilder) IndexName(name string) *VectorProviderBuilder {
	b.pineconeIndex = name
	return b
}

// Build creates the vector provider.
//
// Returns an error if required parameters are missing or the provider is not implemented.
func (b *VectorProviderBuilder) Build() (vector.Provider, error) {
	switch b.providerType {
	case "chromem", "":
		return vector.NewChromemProvider(vector.ChromemConfig{
			PersistPath: b.persistPath,
			Compress:    b.compress,
		})

	case "qdrant":
		return vector.NewQdrantProvider(vector.QdrantConfig{
			Host:   b.qdrantHost,
			Port:   b.qdrantPort,
			APIKey: b.qdrantAPIKey,
			UseTLS: b.qdrantUseTLS,
		})

	case "chroma":
		return vector.NewChromaProvider(vector.ChromaConfig{
			Host:   b.chromaHost,
			Port:   b.chromaPort,
			APIKey: b.chromaAPIKey,
			UseTLS: b.chromaUseTLS,
		})

	case "pinecone":
		if b.pineconeAPIKey == "" {
			return nil, fmt.Errorf("API key is required for Pinecone")
		}
		return vector.NewPineconeProvider(vector.PineconeConfig{
			APIKey:    b.pineconeAPIKey,
			IndexName: b.pineconeIndex,
		})

	case "milvus":
		return vector.NewMilvusProvider(vector.MilvusConfig{
			Host:   b.milvusHost,
			Port:   b.milvusPort,
			APIKey: b.milvusAPIKey,
			UseTLS: b.milvusUseTLS,
		})

	case "weaviate":
		return vector.NewWeaviateProvider(vector.WeaviateConfig{
			Host:   b.weaviateHost,
			Port:   b.weaviatePort,
			APIKey: b.weaviateAPIKey,
			UseTLS: b.weaviateUseTLS,
		})

	default:
		return nil, fmt.Errorf("unknown vector provider: %s (supported: chromem, qdrant, chroma, pinecone, milvus, weaviate)", b.providerType)
	}
}

// MustBuild creates the vector provider or panics on error.
//
// Use this only when you're certain the configuration is valid.
func (b *VectorProviderBuilder) MustBuild() vector.Provider {
	provider, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build vector provider: %v", err))
	}
	return provider
}
