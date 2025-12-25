// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel

package runtime

import (
	"context"
	"iter"
	"sync"
	"testing"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/model"
	"github.com/verikod/hector/pkg/session"
)

// mockLLM implements model.LLM for testing
type mockLLM struct {
	name   string
	closed bool
	mu     sync.Mutex
}

func (m *mockLLM) Name() string {
	return m.name
}

func (m *mockLLM) Provider() model.Provider {
	return model.ProviderOpenAI
}

func (m *mockLLM) GenerateContent(ctx context.Context, req *model.Request, stream bool) iter.Seq2[*model.Response, error] {
	return func(yield func(*model.Response, error) bool) {
		yield(&model.Response{}, nil)
	}
}

func (m *mockLLM) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockLLM) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// minimalConfig creates a minimal valid config for testing
func minimalConfig() *config.Config {
	cfg := &config.Config{
		Name:    "test-app",
		Version: "1.0.0",
		LLMs: map[string]*config.LLMConfig{
			"default": {
				Provider: "openai",
				Model:    "gpt-4o",
				APIKey:   "test-api-key", // Required for validation
			},
		},
		Agents: map[string]*config.AgentConfig{
			"assistant": {
				LLM:         "default",
				Description: "A test assistant",
			},
		},
		Storage: config.StorageConfig{},
		Server:  config.ServerConfig{},
	}
	cfg.SetDefaults()
	return cfg
}

// TestRuntime_Cleanup tests that Close() cleans up resources
func TestRuntime_Cleanup(t *testing.T) {
	cfg := minimalConfig()

	// Track created LLMs
	var createdLLMs []*mockLLM
	var mu sync.Mutex
	trackingFactory := func(cfg *config.LLMConfig) (model.LLM, error) {
		llm := &mockLLM{name: cfg.Model}
		mu.Lock()
		createdLLMs = append(createdLLMs, llm)
		mu.Unlock()
		return llm, nil
	}

	rt, err := NewBuilder().
		WithConfig(cfg).
		WithLLMFactory(trackingFactory).
		WithSessionService(session.InMemoryService()).
		Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// One LLM should be created initially
	mu.Lock()
	initialCount := len(createdLLMs)
	originalLLM := createdLLMs[0]
	mu.Unlock()
	if initialCount != 1 {
		t.Fatalf("Expected 1 LLM, got %d", initialCount)
	}

	// Close runtime
	if err := rt.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Original LLM should be closed
	if !originalLLM.IsClosed() {
		t.Error("LLM should be closed after Runtime.Close()")
	}
}
