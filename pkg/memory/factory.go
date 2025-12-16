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

package memory

import (
	"fmt"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/embedder"
	"github.com/verikod/hector/pkg/vector"
)

// NewIndexServiceFromConfig creates an IndexService based on configuration.
//
// Architecture (derived from legacy Hector):
//
//	┌─────────────────────────────────────────────────────────────┐
//	│   LAYER 3: IndexService (search index)                      │
//	│   - keyword: Simple word matching (default)                 │
//	│   - vector: Semantic search using embeddings                │
//	│   - CAN BE REBUILT from session.Service                     │
//	├─────────────────────────────────────────────────────────────┤
//	│   LAYER 2: session.Service (source of truth)                │
//	│   - SQL storage for all events                              │
//	│   - THIS IS THE SOURCE OF TRUTH                             │
//	├─────────────────────────────────────────────────────────────┤
//	│   LAYER 1: WorkingMemoryStrategy (context window)           │
//	│   - Ephemeral runtime cache                                 │
//	│   - Filters events for LLM context                          │
//	└─────────────────────────────────────────────────────────────┘
//
// Example config:
//
//	embedders:
//	  default:
//	    provider: openai
//	    model: text-embedding-3-small
//	    api_key: ${OPENAI_API_KEY}
//
//	server:
//	  memory:
//	    backend: vector
//	    embedder: default
//	    vector_provider:
//	      type: chromem
//	      chromem:
//	        persist_path: .hector/vectors
//	        compress: true
func NewIndexServiceFromConfig(cfg *config.Config, embedders map[string]embedder.Embedder) (IndexService, error) {
	// Check if memory config exists
	if cfg == nil || cfg.Server.Memory == nil {
		// Return keyword index as default
		return NewKeywordIndexService(), nil
	}

	memCfg := cfg.Server.Memory
	memCfg.SetDefaults()

	if err := memCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid memory config: %w", err)
	}

	switch {
	case memCfg.IsKeyword():
		return NewKeywordIndexService(), nil

	case memCfg.IsVector():
		// Get embedder reference
		emb, ok := embedders[memCfg.Embedder]
		if !ok {
			return nil, fmt.Errorf("embedder %q not found (referenced by server.memory)", memCfg.Embedder)
		}

		// Create vector provider
		provider, err := newVectorProviderFromConfig(memCfg.VectorProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to create vector provider: %w", err)
		}

		return NewVectorIndexService(VectorIndexConfig{
			Provider: provider,
			Embedder: emb,
		})

	default:
		return nil, fmt.Errorf("unknown memory backend: %s (supported: keyword, vector)", memCfg.Backend)
	}
}

// newVectorProviderFromConfig creates a vector.Provider from configuration.
func newVectorProviderFromConfig(cfg *config.VectorProviderConfig) (vector.Provider, error) {
	if cfg == nil {
		// Default to chromem with defaults
		return vector.NewChromemProvider(vector.ChromemConfig{})
	}

	cfg.SetDefaults()

	switch cfg.Type {
	case "chromem", "":
		chromemCfg := vector.ChromemConfig{}
		if cfg.Chromem != nil {
			chromemCfg.PersistPath = cfg.Chromem.PersistPath
			chromemCfg.Compress = cfg.Chromem.Compress
		}
		return vector.NewChromemProvider(chromemCfg)

	case "qdrant":
		return nil, fmt.Errorf("qdrant provider not yet implemented")

	case "chroma":
		return nil, fmt.Errorf("chroma provider not yet implemented")

	case "pinecone":
		return nil, fmt.Errorf("pinecone provider not yet implemented")

	case "milvus":
		return nil, fmt.Errorf("milvus provider not yet implemented")

	case "weaviate":
		return nil, fmt.Errorf("weaviate provider not yet implemented")

	default:
		return nil, fmt.Errorf("unknown vector provider type: %q", cfg.Type)
	}
}
