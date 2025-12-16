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

	"github.com/verikod/hector/pkg/observability"
)

// TransportType identifies the server transport.
type TransportType string

const (
	TransportJSONRPC TransportType = "json-rpc"
	TransportGRPC    TransportType = "grpc"
)

// ServerConfig configures the A2A server.
type ServerConfig struct {
	// Host to bind to.
	Host string `yaml:"host,omitempty"`

	// Port to listen on (HTTP/JSON-RPC).
	Port int `yaml:"port,omitempty"`

	// GRPCPort is the port for gRPC server (default: 50051).
	// Only used when Transport is "grpc".
	GRPCPort int `yaml:"grpc_port,omitempty"`

	// Transport protocol (json-rpc, grpc).
	Transport TransportType `yaml:"transport,omitempty"`

	// TLS configuration.
	TLS *TLSConfig `yaml:"tls,omitempty"`

	// CORS configuration.
	CORS *CORSConfig `yaml:"cors,omitempty"`

	// Auth configures JWT-based authentication.
	Auth *AuthConfig `yaml:"auth,omitempty"`

	// Tasks configures the task store for A2A task persistence.
	Tasks *TasksConfig `yaml:"tasks,omitempty"`

	// Sessions configures the session store for conversation persistence.
	Sessions *SessionsConfig `yaml:"sessions,omitempty"`

	// Memory configures the memory service for cross-session knowledge.
	Memory *MemoryConfig `yaml:"memory,omitempty"`

	// Observability configures tracing and metrics.
	Observability *observability.Config `yaml:"observability,omitempty"`

	// Checkpoint configures execution state checkpointing and recovery.
	Checkpoint *CheckpointConfig `yaml:"checkpoint,omitempty"`
}

// StorageBackend identifies a storage backend type.
type StorageBackend string

const (
	// StorageBackendInMemory uses in-memory storage (default).
	StorageBackendInMemory StorageBackend = "inmemory"

	// StorageBackendSQL uses SQL database for persistence.
	StorageBackendSQL StorageBackend = "sql"
)

// TasksConfig configures task storage.
type TasksConfig struct {
	// Backend specifies the storage backend: "inmemory" (default) or "sql".
	Backend StorageBackend `yaml:"backend,omitempty"`

	// Database is a reference to a database defined in the databases section.
	// Required when Backend is "sql".
	Database string `yaml:"database,omitempty"`
}

// SessionsConfig configures session storage.
type SessionsConfig struct {
	// Backend specifies the storage backend: "inmemory" (default) or "sql".
	Backend StorageBackend `yaml:"backend,omitempty"`

	// Database is a reference to a database defined in the databases section.
	// Required when Backend is "sql".
	Database string `yaml:"database,omitempty"`
}

// MemoryConfig configures the memory index service.
//
// Architecture (derived from legacy Hector):
//
//	┌─────────────────────────────────────────────────────────┐
//	│   session.Service (SQL) → SOURCE OF TRUTH               │
//	│   All conversation data is persisted here               │
//	├─────────────────────────────────────────────────────────┤
//	│   IndexService → SEARCH INDEX (can be rebuilt)          │
//	│   - keyword: Simple word matching (default)             │
//	│   - vector: Semantic similarity using embeddings        │
//	│                                                         │
//	│   vector.Provider → VECTOR STORAGE (reusable for RAG)   │
//	│   - chromem: Embedded (zero-config)                     │
//	│   - qdrant/chroma/etc: External (future)                │
//	└─────────────────────────────────────────────────────────┘
//
// Example:
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
type MemoryConfig struct {
	// Backend specifies the index backend.
	// Values:
	//   - "keyword" (default): Simple word matching, no embedder needed
	//   - "vector": Semantic vector search using embeddings
	Backend string `yaml:"backend,omitempty"`

	// Embedder references an embedder from the top-level embedders config.
	// Required when backend="vector".
	Embedder string `yaml:"embedder,omitempty"`

	// VectorProvider configures the vector storage backend.
	// Only used when backend="vector".
	// If not specified, defaults to chromem (embedded).
	VectorProvider *VectorProviderConfig `yaml:"vector_provider,omitempty"`
}

// VectorProviderConfig configures the vector storage backend.
//
// This is the unified configuration for all vector providers.
// The same provider can be used for both memory indexing and future RAG.
type VectorProviderConfig struct {
	// Type identifies which provider to use.
	// Values: "chromem" (default, embedded), "qdrant", "chroma", "pinecone", "milvus", "weaviate"
	Type string `yaml:"type,omitempty"`

	// Chromem configuration (used when Type="chromem").
	Chromem *ChromemProviderConfig `yaml:"chromem,omitempty"`

	// Future: External provider configurations
	// Qdrant   *QdrantProviderConfig   `yaml:"qdrant,omitempty"`
	// Chroma   *ChromaProviderConfig   `yaml:"chroma,omitempty"`
	// Pinecone *PineconeProviderConfig `yaml:"pinecone,omitempty"`
}

// ChromemProviderConfig configures the chromem-go embedded vector provider.
type ChromemProviderConfig struct {
	// PersistPath for file persistence (optional).
	// If empty, vectors are stored in memory only.
	// Default: .hector/chromem
	PersistPath string `yaml:"persist_path,omitempty"`

	// Compress enables gzip compression for persistence.
	// Reduces file size but increases CPU usage.
	Compress bool `yaml:"compress,omitempty"`
}

// SetDefaults applies default values to VectorProviderConfig.
func (c *VectorProviderConfig) SetDefaults() {
	if c.Type == "" {
		c.Type = "chromem"
	}
	if c.Type == "chromem" && c.Chromem == nil {
		c.Chromem = &ChromemProviderConfig{}
	}
}

// Validate checks VectorProviderConfig for errors.
func (c *VectorProviderConfig) Validate() error {
	switch c.Type {
	case "chromem", "":
		return nil
	case "qdrant", "chroma", "pinecone", "milvus", "weaviate":
		return fmt.Errorf("vector provider type %q is not yet implemented", c.Type)
	default:
		return fmt.Errorf("unknown vector provider type: %q", c.Type)
	}
}

// CheckpointConfig configures execution state checkpointing and recovery.
//
// Architecture (ported from legacy Hector):
//
//	Checkpoints capture the full state of an agent execution at strategic points.
//	This enables:
//	  - Fault tolerance: Resume after crashes
//	  - HITL workflows: Pause for human approval, resume later
//	  - Long-running tasks: Survive server restarts
//	  - Cost optimization: Don't re-execute completed work
//
// Example:
//
//	server:
//	  checkpoint:
//	    enabled: true
//	    strategy: hybrid
//	    interval: 5
//	    after_tools: true
//	    recovery:
//	      auto_resume: true
//	      timeout: 3600
type CheckpointConfig struct {
	// Enabled enables checkpointing.
	// Default: false
	Enabled *bool `yaml:"enabled,omitempty"`

	// Strategy determines when checkpoints are created.
	// Values: "event" (default), "interval", "hybrid"
	Strategy string `yaml:"strategy,omitempty"`

	// Interval specifies checkpoint frequency (every N iterations).
	// Only used when Strategy is "interval" or "hybrid".
	// Default: 0 (disabled)
	Interval int `yaml:"interval,omitempty"`

	// AfterTools checkpoints after tool executions complete.
	// Default: false
	AfterTools *bool `yaml:"after_tools,omitempty"`

	// BeforeLLM checkpoints before LLM API calls.
	// Default: false
	BeforeLLM *bool `yaml:"before_llm,omitempty"`

	// Recovery configures checkpoint recovery behavior.
	Recovery *CheckpointRecoveryConfig `yaml:"recovery,omitempty"`
}

// CheckpointRecoveryConfig configures checkpoint recovery behavior.
type CheckpointRecoveryConfig struct {
	// AutoResume enables automatic recovery on startup.
	// Default: false
	AutoResume *bool `yaml:"auto_resume,omitempty"`

	// AutoResumeHITL enables automatic recovery for INPUT_REQUIRED tasks.
	// When false, INPUT_REQUIRED tasks wait for explicit user action.
	// Default: false
	AutoResumeHITL *bool `yaml:"auto_resume_hitl,omitempty"`

	// Timeout is the maximum age (in seconds) for a checkpoint to be recoverable.
	// Checkpoints older than this are considered expired.
	// Default: 3600 (1 hour)
	Timeout int `yaml:"timeout,omitempty"`
}

// SetDefaults applies default values for CheckpointConfig.
func (c *CheckpointConfig) SetDefaults() {
	if c.Enabled == nil {
		enabled := false
		c.Enabled = &enabled
	}
	if c.Strategy == "" {
		c.Strategy = "event"
	}
	if c.AfterTools == nil {
		afterTools := false
		c.AfterTools = &afterTools
	}
	if c.BeforeLLM == nil {
		beforeLLM := false
		c.BeforeLLM = &beforeLLM
	}
	if c.Recovery == nil {
		c.Recovery = &CheckpointRecoveryConfig{}
	}
	c.Recovery.SetDefaults()
}

// SetDefaults applies default values for CheckpointRecoveryConfig.
func (c *CheckpointRecoveryConfig) SetDefaults() {
	if c.AutoResume == nil {
		autoResume := false
		c.AutoResume = &autoResume
	}
	if c.AutoResumeHITL == nil {
		autoResumeHITL := false
		c.AutoResumeHITL = &autoResumeHITL
	}
	if c.Timeout == 0 {
		c.Timeout = 3600 // 1 hour
	}
}

// Validate checks the CheckpointConfig.
func (c *CheckpointConfig) Validate() error {
	if c.Strategy != "" && c.Strategy != "event" && c.Strategy != "interval" && c.Strategy != "hybrid" {
		return fmt.Errorf("invalid strategy %q (valid: event, interval, hybrid)", c.Strategy)
	}
	if c.Interval < 0 {
		return fmt.Errorf("interval must be non-negative")
	}
	if c.Recovery != nil {
		if err := c.Recovery.Validate(); err != nil {
			return fmt.Errorf("recovery: %w", err)
		}
	}
	return nil
}

// Validate checks the CheckpointRecoveryConfig.
func (c *CheckpointRecoveryConfig) Validate() error {
	if c.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative")
	}
	return nil
}

// IsEnabled returns whether checkpointing is enabled.
func (c *CheckpointConfig) IsEnabled() bool {
	return c != nil && c.Enabled != nil && *c.Enabled
}

// ShouldCheckpointAfterTools returns whether to checkpoint after tool execution.
func (c *CheckpointConfig) ShouldCheckpointAfterTools() bool {
	return c.IsEnabled() && c.AfterTools != nil && *c.AfterTools
}

// ShouldCheckpointBeforeLLM returns whether to checkpoint before LLM calls.
func (c *CheckpointConfig) ShouldCheckpointBeforeLLM() bool {
	return c.IsEnabled() && c.BeforeLLM != nil && *c.BeforeLLM
}

// ShouldAutoResume returns whether to auto-resume on startup.
func (c *CheckpointConfig) ShouldAutoResume() bool {
	return c.IsEnabled() && c.Recovery != nil && c.Recovery.AutoResume != nil && *c.Recovery.AutoResume
}

// TLSConfig configures TLS.
type TLSConfig struct {
	// Enabled turns on TLS.
	Enabled *bool `yaml:"enabled,omitempty"`

	// CertFile is the path to the certificate.
	CertFile string `yaml:"cert_file,omitempty"`

	// KeyFile is the path to the private key.
	KeyFile string `yaml:"key_file,omitempty"`
}

// CORSConfig configures CORS.
type CORSConfig struct {
	// AllowedOrigins is a list of allowed origins.
	AllowedOrigins []string `yaml:"allowed_origins,omitempty"`

	// AllowedMethods is a list of allowed HTTP methods.
	AllowedMethods []string `yaml:"allowed_methods,omitempty"`

	// AllowedHeaders is a list of allowed headers.
	AllowedHeaders []string `yaml:"allowed_headers,omitempty"`

	// AllowCredentials allows credentials.
	AllowCredentials *bool `yaml:"allow_credentials,omitempty"`
}

// SetDefaults applies default values.
func (c *ServerConfig) SetDefaults() {
	if c.Host == "" {
		c.Host = "0.0.0.0"
	}

	if c.Port == 0 {
		c.Port = 8080
	}

	if c.GRPCPort == 0 {
		c.GRPCPort = 50051
	}

	if c.Transport == "" {
		c.Transport = TransportJSONRPC
	}

	// Default CORS for development
	if c.CORS == nil {
		c.CORS = &CORSConfig{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST", "OPTIONS"},
			AllowedHeaders: []string{"Content-Type", "Authorization"},
		}
	}

	// Apply auth defaults if configured
	if c.Auth != nil {
		c.Auth.SetDefaults()
	}

	// Apply task defaults if configured
	if c.Tasks != nil {
		c.Tasks.SetDefaults()
	}

	// Apply session defaults if configured
	if c.Sessions != nil {
		c.Sessions.SetDefaults()
	}

	// Apply memory defaults if configured
	if c.Memory != nil {
		c.Memory.SetDefaults()
	}

	// Apply observability defaults if configured
	if c.Observability != nil {
		c.Observability.SetDefaults()
	}

	// Apply checkpoint defaults if configured
	if c.Checkpoint != nil {
		c.Checkpoint.SetDefaults()
	}
}

// Validate checks the server configuration.
func (c *ServerConfig) Validate() error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d", c.Port)
	}

	if c.GRPCPort < 0 || c.GRPCPort > 65535 {
		return fmt.Errorf("invalid grpc_port %d", c.GRPCPort)
	}

	if c.Transport != TransportJSONRPC && c.Transport != TransportGRPC {
		return fmt.Errorf("invalid transport %q (valid: json-rpc, grpc)", c.Transport)
	}

	if c.TLS != nil && BoolValue(c.TLS.Enabled, false) {
		if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
			return fmt.Errorf("tls requires cert_file and key_file")
		}
	}

	// Validate auth config
	if c.Auth != nil {
		if err := c.Auth.Validate(); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	// Validate tasks config
	if c.Tasks != nil {
		if err := c.Tasks.Validate(); err != nil {
			return fmt.Errorf("tasks: %w", err)
		}
	}

	// Validate sessions config
	if c.Sessions != nil {
		if err := c.Sessions.Validate(); err != nil {
			return fmt.Errorf("sessions: %w", err)
		}
	}

	// Validate memory config
	if c.Memory != nil {
		if err := c.Memory.Validate(); err != nil {
			return fmt.Errorf("memory: %w", err)
		}
	}

	// Validate observability config
	if c.Observability != nil {
		if err := c.Observability.Validate(); err != nil {
			return fmt.Errorf("observability: %w", err)
		}
	}

	// Validate checkpoint config
	if c.Checkpoint != nil {
		if err := c.Checkpoint.Validate(); err != nil {
			return fmt.Errorf("checkpoint: %w", err)
		}
	}

	return nil
}

// SetDefaults applies default values for TasksConfig.
func (c *TasksConfig) SetDefaults() {
	if c.Backend == "" {
		c.Backend = StorageBackendInMemory
	}
}

// DefaultDatabaseConfig returns a DatabaseConfig with sane defaults for the given driver.
// This is used for zero-config mode where users just specify a backend.
func DefaultDatabaseConfig(driver string) *DatabaseConfig {
	cfg := &DatabaseConfig{
		Driver:   driver,
		MaxConns: 25,
		MaxIdle:  5,
	}

	switch driver {
	case "sqlite", "sqlite3":
		cfg.Driver = "sqlite"
		cfg.Database = "./.hector/hector.db"
	case "postgres":
		cfg.Host = "localhost"
		cfg.Port = 5432
		cfg.Database = "hector"
		cfg.SSLMode = "disable"
	case "mysql":
		cfg.Host = "localhost"
		cfg.Port = 3306
		cfg.Database = "hector"
	}

	return cfg
}

// Validate checks the tasks configuration.
func (c *TasksConfig) Validate() error {
	// Validate backend
	if c.Backend != "" && c.Backend != StorageBackendInMemory && c.Backend != StorageBackendSQL {
		return fmt.Errorf("invalid backend %q (valid: inmemory, sql)", c.Backend)
	}

	// If backend is SQL, database reference is required
	if c.Backend == StorageBackendSQL && c.Database == "" {
		return fmt.Errorf("database reference is required when backend is sql")
	}

	// If database is set, backend should be SQL
	if c.Database != "" && c.Backend != StorageBackendSQL {
		return fmt.Errorf("database reference requires backend to be sql")
	}

	return nil
}

// IsInMemory returns true if using in-memory task storage.
func (c *TasksConfig) IsInMemory() bool {
	return c == nil || c.Backend == "" || c.Backend == StorageBackendInMemory
}

// IsSQL returns true if using SQL task storage.
func (c *TasksConfig) IsSQL() bool {
	return c != nil && c.Backend == StorageBackendSQL
}

// SetDefaults applies default values for SessionsConfig.
func (c *SessionsConfig) SetDefaults() {
	if c.Backend == "" {
		c.Backend = StorageBackendInMemory
	}
}

// Validate checks the sessions configuration.
func (c *SessionsConfig) Validate() error {
	// Validate backend
	if c.Backend != "" && c.Backend != StorageBackendInMemory && c.Backend != StorageBackendSQL {
		return fmt.Errorf("invalid backend %q (valid: inmemory, sql)", c.Backend)
	}

	// If backend is SQL, database reference is required
	if c.Backend == StorageBackendSQL && c.Database == "" {
		return fmt.Errorf("database reference is required when backend is sql")
	}

	// If database is set, backend should be SQL
	if c.Database != "" && c.Backend != StorageBackendSQL {
		return fmt.Errorf("database reference requires backend to be sql")
	}

	return nil
}

// IsInMemory returns true if using in-memory session storage.
func (c *SessionsConfig) IsInMemory() bool {
	return c == nil || c.Backend == "" || c.Backend == StorageBackendInMemory
}

// IsSQL returns true if using SQL session storage.
func (c *SessionsConfig) IsSQL() bool {
	return c != nil && c.Backend == StorageBackendSQL
}

// SetDefaults applies default values for MemoryConfig.
func (c *MemoryConfig) SetDefaults() {
	if c.Backend == "" {
		c.Backend = "keyword"
	}

	// Set vector provider defaults
	if c.IsVector() && c.VectorProvider == nil {
		c.VectorProvider = &VectorProviderConfig{}
	}
	if c.VectorProvider != nil {
		c.VectorProvider.SetDefaults()
	}
}

// Validate checks the memory configuration.
func (c *MemoryConfig) Validate() error {
	validBackends := map[string]bool{
		"":        true,
		"keyword": true,
		"vector":  true,
	}

	if !validBackends[c.Backend] {
		return fmt.Errorf("invalid backend %q (valid: keyword, vector)", c.Backend)
	}

	// Vector backend requires an embedder reference
	if c.IsVector() && c.Embedder == "" {
		return fmt.Errorf("embedder reference is required when backend is vector")
	}

	// Validate vector provider config
	if c.VectorProvider != nil {
		if err := c.VectorProvider.Validate(); err != nil {
			return fmt.Errorf("vector_provider: %w", err)
		}
	}

	return nil
}

// IsKeyword returns true if using keyword-based search index (default).
func (c *MemoryConfig) IsKeyword() bool {
	return c == nil || c.Backend == "" || c.Backend == "keyword"
}

// IsVector returns true if using vector-based semantic search index.
func (c *MemoryConfig) IsVector() bool {
	return c != nil && c.Backend == "vector"
}

// Address returns the HTTP server address.
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// GRPCAddress returns the gRPC server address.
func (c *ServerConfig) GRPCAddress() string {
	return fmt.Sprintf("%s:%d", c.Host, c.GRPCPort)
}
