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

package memory_test

import (
	"testing"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/memory"
)

func TestMemoryConfig_SetDefaults(t *testing.T) {
	cfg := &config.MemoryConfig{}
	cfg.SetDefaults()

	if cfg.Backend != "keyword" {
		t.Errorf("expected default backend %q, got %q", "keyword", cfg.Backend)
	}
}

func TestMemoryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.MemoryConfig
		wantErr bool
	}{
		{
			name:    "valid keyword (default)",
			cfg:     &config.MemoryConfig{Backend: "keyword"},
			wantErr: false,
		},
		{
			name:    "valid vector with embedder",
			cfg:     &config.MemoryConfig{Backend: "vector", Embedder: "default"},
			wantErr: false,
		},
		{
			name:    "invalid backend",
			cfg:     &config.MemoryConfig{Backend: "invalid"},
			wantErr: true,
		},
		{
			name:    "vector without embedder",
			cfg:     &config.MemoryConfig{Backend: "vector"},
			wantErr: true,
		},
		{
			name: "valid vector with vector provider",
			cfg: &config.MemoryConfig{
				Backend:  "vector",
				Embedder: "default",
				VectorProvider: &config.VectorProviderConfig{
					Type: "chromem",
					Chromem: &config.ChromemProviderConfig{
						PersistPath: ".hector/vectors",
						Compress:    true,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryConfig_IsKeyword(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.MemoryConfig
		want bool
	}{
		{name: "nil config", cfg: nil, want: true},
		{name: "empty backend", cfg: &config.MemoryConfig{}, want: true},
		{name: "explicit keyword", cfg: &config.MemoryConfig{Backend: "keyword"}, want: true},
		{name: "vector backend", cfg: &config.MemoryConfig{Backend: "vector"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsKeyword(); got != tt.want {
				t.Errorf("IsKeyword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryConfig_IsVector(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.MemoryConfig
		want bool
	}{
		{name: "nil config", cfg: nil, want: false},
		{name: "empty backend", cfg: &config.MemoryConfig{}, want: false},
		{name: "keyword backend", cfg: &config.MemoryConfig{Backend: "keyword"}, want: false},
		{name: "vector backend", cfg: &config.MemoryConfig{Backend: "vector"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsVector(); got != tt.want {
				t.Errorf("IsVector() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewIndexServiceFromConfig_Keyword(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Memory: &config.MemoryConfig{
				Backend: "keyword",
			},
		},
	}

	svc, err := memory.NewIndexServiceFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.Name() != "keyword" {
		t.Errorf("expected keyword service, got %s", svc.Name())
	}
}

func TestNewIndexServiceFromConfig_NilConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Memory: nil,
		},
	}

	svc, err := memory.NewIndexServiceFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.Name() != "keyword" {
		t.Errorf("expected keyword service, got %s", svc.Name())
	}
}

func TestNewIndexServiceFromConfig_VectorWithoutEmbedder(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Memory: &config.MemoryConfig{
				Backend:  "vector",
				Embedder: "missing",
			},
		},
	}

	_, err := memory.NewIndexServiceFromConfig(cfg, nil)
	if err == nil {
		t.Error("expected error for missing embedder reference")
	}
}

// Note: Vector index tests with actual embedders require API keys
// and should be covered by integration tests.
