package memory

import (
	"fmt"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/embedder"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/rag"
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
	if cfg == nil || cfg.Storage.Memory == nil {
		// Return keyword index as default
		return NewKeywordIndexService(), nil
	}

	memCfg := cfg.Storage.Memory
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
		// Map Memory-specific config (nested) to RAG generic config (flat)
		ragCfg := &config.VectorStoreConfig{
			Type: memCfg.VectorProvider.Type,
		}
		if memCfg.VectorProvider.Chromem != nil {
			ragCfg.PersistPath = memCfg.VectorProvider.Chromem.PersistPath
			ragCfg.Compress = memCfg.VectorProvider.Chromem.Compress
		}

		provider, err := rag.NewVectorProviderFromConfig(ragCfg)
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

// NewWorkingMemoryStrategyFromConfig creates a working memory strategy from configuration.
func NewWorkingMemoryStrategyFromConfig(cfg *config.ContextConfig, defaultModel string, llms map[string]model.LLM) (WorkingMemoryStrategy, error) {
	if cfg == nil {
		return NilWorkingMemory{}, nil
	}

	// Apply defaults
	cfg.SetDefaults()

	switch cfg.Strategy {
	case "none", "":
		return NilWorkingMemory{}, nil

	case "buffer_window":
		return NewBufferWindowStrategy(BufferWindowConfig{
			WindowSize: cfg.WindowSize,
		}), nil

	case "token_window":
		return NewTokenWindowStrategy(TokenWindowConfig{
			Budget:         cfg.Budget,
			PreserveRecent: cfg.PreserveRecent,
			Model:          defaultModel,
		})

	case "summary_buffer":
		// Get summarizer LLM
		summarizerLLMName := cfg.SummarizerLLM

		// Resolution logic for summarizer LLM:
		// 1. Configured SummarizerLLM
		// 2. Default LLM (if passed/known)
		// For now, if llms map is provided, we try to find one.
		var summarizerLLM model.LLM
		if summarizerLLMName != "" && llms != nil {
			var ok bool
			summarizerLLM, ok = llms[summarizerLLMName]
			if !ok {
				return nil, fmt.Errorf("summarizer LLM %q not found", summarizerLLMName)
			}
		}
		// If still nil, we can't create a summarizer unless we have a fallback?
		// Actually, `pkg/memory/summary_buffer.go` allows nil summarizer but then it disables summarization.

		// Detailed check:
		var summarizer Summarizer
		if summarizerLLM != nil {
			var err error
			summarizer, err = NewLLMSummarizer(LLMSummarizerConfig{
				LLM: summarizerLLM,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create summarizer: %w", err)
			}
		}

		return NewSummaryBufferStrategy(SummaryBufferConfig{
			Budget:     cfg.Budget,
			Threshold:  cfg.Threshold,
			Target:     cfg.Target,
			Model:      defaultModel,
			Summarizer: summarizer,
		})

	default:
		return nil, fmt.Errorf("unknown context strategy: %q", cfg.Strategy)
	}
}
