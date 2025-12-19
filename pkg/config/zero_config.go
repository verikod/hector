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
	"os"
	"path/filepath"
	"strings"

	"github.com/verikod/hector/pkg/observability"
	"github.com/verikod/hector/pkg/utils"
)

// ZeroConfig are CLI options for zero-config mode.
type ZeroConfig struct {
	// Provider type (anthropic, openai, gemini, ollama).
	Provider string

	// Model name.
	Model string

	// APIKey (usually from environment).
	APIKey string

	// BaseURL is a custom API base URL.
	BaseURL string

	// Temperature for generation.
	Temperature float64

	// MaxTokens for generation.
	MaxTokens int

	// Instruction is the system prompt.
	Instruction string

	// Role is the agent's role.
	Role string

	// MCPURL is an MCP server URL to connect to.
	MCPURL string

	// Tools enables built-in local tools.
	// Empty string or "all" enables all tools.
	// Comma-separated list enables specific tools (e.g., "read_file,write_file").
	Tools string

	// ApproveTools enables approval for specific tools (comma-separated).
	// Overrides smart defaults set by SetDefaults().
	ApproveTools string

	// NoApproveTools disables approval for specific tools (comma-separated).
	// Overrides smart defaults set by SetDefaults().
	NoApproveTools string

	// Thinking enables extended thinking for the LLM (like --tools enables tools).
	Thinking *bool

	// ThinkingBudget sets the token budget for thinking (default: 1024 if not specified).
	ThinkingBudget int

	// Streaming enables token-by-token streaming from the LLM (enabled by default).
	Streaming *bool

	// Port for the server.
	Port int

	// AgentName is the name of the agent.
	AgentName string

	// Storage specifies the storage backend: sqlite, postgres, mysql, or inmemory.
	// When set (and not "inmemory"), enables persistent storage for tasks, sessions,
	// and checkpointing.
	Storage string

	// StorageDB overrides the default database DSN/path for storage.
	// For SQLite: file path (default: ./.hector/hector.db)
	// For PostgreSQL/MySQL: connection string or individual params
	StorageDB string

	// Observe enables observability (metrics + OTLP tracing).
	// When enabled, exports traces to localhost:4317 and enables Prometheus metrics.
	Observe bool

	// DocsFolder is a path to documents for RAG.
	// When set, auto-creates a document store with the configured vector DB.
	DocsFolder string

	// VectorType specifies the vector database type.
	// Values: "chromem" (default, embedded), "qdrant", "chroma", "pinecone", "weaviate", "milvus"
	VectorType string

	// VectorHost is the host:port for external vector databases (qdrant, chroma, weaviate, milvus).
	// Example: "localhost:6333" for Qdrant
	VectorHost string

	// VectorAPIKey is the API key for vector databases that require authentication (pinecone).
	VectorAPIKey string

	// EmbedderProvider overrides the auto-detected embedder provider.
	// Values: "openai", "ollama", "cohere"
	// Auto-detection: Uses LLM provider if it has embeddings (openai), otherwise ollama.
	EmbedderProvider string

	// EmbedderModel overrides the auto-detected embedder model.
	// Auto-detection based on provider:
	//   - openai: text-embedding-3-small
	//   - ollama: nomic-embed-text
	//   - cohere: embed-english-v3.0
	EmbedderModel string

	// EmbedderURL overrides the embedder API base URL.
	// Useful for custom ollama endpoints or OpenAI-compatible APIs.
	EmbedderURL string

	// IncludeContext enables automatic RAG context injection.
	// When true, relevant context from document stores is automatically injected
	// into prompts without requiring the agent to call the search tool.
	IncludeContext *bool

	// RAGWatch enables file watching for auto re-indexing (default: true).
	RAGWatch *bool

	// MCPParserTool is the MCP tool name(s) for document parsing (e.g., Docling).
	// Comma-separated for fallback chain (e.g., "parse_document,docling_parse").
	MCPParserTool string

	// AuthJWKSURL is the URL to fetch JSON Web Key Set from for JWT auth.
	AuthJWKSURL string

	// AuthIssuer is the expected token issuer.
	AuthIssuer string

	// AuthAudience is the expected token audience.
	AuthAudience string

	// AuthRequired enforces authentication on all endpoints (default: true).
	AuthRequired *bool
}

// CreateZeroConfig creates a Config from CLI options.
// This enables "zero config" mode where users can run Hector
// without a config file by providing CLI flags.
//
// IMPORTANT DESIGN PRINCIPLE: Do NOT duplicate defaults that are already handled by SetDefaults().
// Only set values here if:
//  1. They are explicitly provided via CLI flags (non-zero/non-empty)
//  2. They are zero-config specific overrides (different from config file defaults)
//
// Examples:
//   - ✅ Set streaming=true (zero-config override, different from config file default=false)
//   - ✅ Set thinking.Enabled=true when --thinking flag is used (explicit enablement)
//   - ❌ Do NOT set thinking.BudgetTokens=1024 (SetDefaults() already handles this)
//   - ❌ Do NOT set temperature=0.7 (SetDefaults() already handles this)
//   - ❌ Do NOT set maxTokens=4096 (SetDefaults() already handles this)
//
// After this function, cfg.SetDefaults() is called to apply all standard defaults.
func CreateZeroConfig(opts ZeroConfig) *Config {
	// Detect provider from environment if not specified
	provider := LLMProvider(opts.Provider)
	if provider == "" {
		provider = detectProviderFromEnv()
	}

	// Get API key
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = getAPIKeyFromEnv(provider)
	}

	// Check for MCP URL from environment
	mcpURL := opts.MCPURL
	if mcpURL == "" {
		mcpURL = os.Getenv("MCP_URL")
	}

	// Agent name
	// NOTE: This is a zero-config specific default, not handled by SetDefaults()
	// SetDefaults() doesn't set agent names - they come from the map key in config files
	agentName := opts.AgentName
	if agentName == "" {
		agentName = "assistant" // Zero-config default agent name
	}

	// Create LLM config
	llmConfig := &LLMConfig{
		Provider: provider,
		APIKey:   apiKey,
	}

	if opts.Model != "" {
		llmConfig.Model = opts.Model
	}

	if opts.BaseURL != "" {
		llmConfig.BaseURL = opts.BaseURL
	}

	// Set temperature only if explicitly provided (> 0)
	// NOTE: Do NOT set default here - SetDefaults() handles it (defaults to 0.7)
	// Only set if user explicitly provided a value via CLI
	if opts.Temperature > 0 {
		llmConfig.Temperature = &opts.Temperature
	}

	// Set max tokens only if explicitly provided (> 0)
	// NOTE: Do NOT set default here - SetDefaults() handles it (defaults to 4096)
	// Only set if user explicitly provided a value via CLI
	if opts.MaxTokens > 0 {
		llmConfig.MaxTokens = opts.MaxTokens
	}

	// Set thinking configuration if enabled
	// NOTE: Do NOT set default values here that are already handled by SetDefaults().
	// Only set values that are explicitly provided or are zero-config specific overrides.
	// - Thinking.Enabled: Set to true when --thinking flag is used (explicit enablement)
	// - Thinking.BudgetTokens: Only set if explicitly provided (> 0), otherwise let SetDefaults() handle it
	if BoolValue(opts.Thinking, false) {
		llmConfig.Thinking = &ThinkingConfig{
			Enabled: BoolPtr(true), // Explicit enablement when --thinking flag is used
		}
		// Only set budget if explicitly provided via CLI
		// If 0, SetDefaults() will set it to 1024 (see LLMConfig.SetDefaults())
		if opts.ThinkingBudget > 0 {
			llmConfig.Thinking.BudgetTokens = opts.ThinkingBudget
		}
	}

	// Create agent config
	// NOTE: Zero-config specific override: streaming defaults to true (enabled) in zero-config mode.
	// This is different from config file mode where streaming defaults to false.
	// SetDefaults() will NOT override this because it only sets defaults if the field is nil.
	streaming := opts.Streaming
	if streaming == nil {
		streaming = BoolPtr(true) // Zero-config specific: enable streaming by default
	}
	agentConfig := &AgentConfig{
		Name:        agentName,
		LLM:         "default", // SetDefaults() will handle if empty
		Instruction: opts.Instruction,
		Streaming:   streaming, // Zero-config override: true by default
	}

	if opts.Role != "" {
		agentConfig.Prompt = &PromptConfig{
			Role: opts.Role,
		}
	}

	// Create config
	cfg := &Config{
		Name:      "Zero Config Mode",
		Databases: make(map[string]*DatabaseConfig),
		LLMs: map[string]*LLMConfig{
			"default": llmConfig,
		},
		Agents: map[string]*AgentConfig{
			agentName: agentConfig,
		},
		Tools: make(map[string]*ToolConfig),
		Server: ServerConfig{
			// Port: Only set if explicitly provided (> 0)
			// NOTE: Do NOT set default here - SetDefaults() handles it (defaults to 8080)
			Port: opts.Port,
		},
	}

	// Configure persistent storage if specified
	// Supported backends: sqlite, postgres, mysql (inmemory is default, no persistence)
	if opts.Storage != "" && opts.Storage != "inmemory" {
		storageBackend := opts.Storage
		storageDB := opts.StorageDB
		// Get default database config for the specified backend
		dbConfig := DefaultDatabaseConfig(storageBackend)
		if dbConfig != nil {
			// Override with custom DSN/path if provided
			if storageDB != "" {
				if storageBackend == "sqlite" || storageBackend == "sqlite3" {
					dbConfig.Database = storageDB
				} else {
					// For postgres/mysql, StorageDB can be a DSN or just override database name
					dbConfig.Database = storageDB
				}
			}

			// Ensure .hector directory exists for SQLite
			if dbConfig.Driver == "sqlite" {
				dir := filepath.Dir(dbConfig.Database)
				if dir != "" && dir != "." {
					// Use centralized EnsureHectorDir if path contains .hector
					if filepath.Base(dir) == ".hector" {
						basePath := filepath.Dir(dir)
						if basePath == "" || basePath == "." {
							basePath = "."
						}
						_, _ = utils.EnsureHectorDir(basePath)
					} else {
						_ = os.MkdirAll(dir, 0755)
					}
				}
			}

			// Add database config and enable persistence for tasks, sessions, and memory
			cfg.Databases["_default"] = dbConfig
			cfg.Storage.Tasks = &TasksConfig{
				Backend:  StorageBackendSQL,
				Database: "_default",
			}
			cfg.Storage.Sessions = &SessionsConfig{
				Backend:  StorageBackendSQL,
				Database: "_default",
			}
			// Memory index defaults to keyword (no embedder needed)
			// Users can configure vector index with embedder in config file
			cfg.Storage.Memory = &MemoryConfig{
				Backend: "keyword",
			}

			// Auto-enable checkpointing when storage is enabled
			// Checkpoints are stored in session state, so they benefit from persistence
			cfg.Storage.Checkpoint = &CheckpointConfig{
				Enabled:    BoolPtr(true),
				Strategy:   "hybrid", // Safe default: event + interval
				AfterTools: BoolPtr(true),
				BeforeLLM:  BoolPtr(true),
				Recovery: &CheckpointRecoveryConfig{
					AutoResume:     BoolPtr(true),  // Auto-resume non-HITL tasks on startup
					AutoResumeHITL: BoolPtr(false), // Require user approval for HITL
					Timeout:        86400,          // 24h expiry for checkpoints (in seconds)
				},
			}
		}
	}

	// Configure observability if enabled
	// Exports traces to OTLP endpoint and enables Prometheus metrics
	if opts.Observe {
		cfg.Storage.Observability = &observability.Config{
			Tracing: observability.TracingConfig{
				Enabled:      true,
				Exporter:     "otlp",
				Endpoint:     "localhost:4317",
				SamplingRate: 1.0, // Sample all traces in zero-config mode
			},
			Metrics: observability.MetricsConfig{
				Enabled: true,
			},
		}
	}

	// Configure RAG if docs folder is specified
	// Creates embedded chromem vector store, auto-detected embedder, and document store
	if opts.DocsFolder != "" {
		expandDocsFolder(cfg, agentConfig, opts)
	}

	// Add MCP tool if URL provided
	// NOTE: ToolConfig defaults (Enabled, etc.) are handled by ToolConfig.SetDefaults()
	// Only set values that are explicitly provided here
	if mcpURL != "" {
		cfg.Tools["mcp"] = &ToolConfig{
			Type: ToolTypeMCP,
			URL:  mcpURL,
			// Enabled, Transport, etc. will be set by ToolConfig.SetDefaults()
		}
		agentConfig.Tools = append(agentConfig.Tools, "mcp")
	}

	// Add default local tools if enabled
	// Empty string or "all" enables all tools
	// Comma-separated list enables specific tools
	if opts.Tools != "" {
		defaultTools := GetDefaultToolConfigs()
		enabledTools := parseToolsList(opts.Tools, defaultTools)

		for _, name := range enabledTools {
			if toolCfg, ok := defaultTools[name]; ok {
				cfg.Tools[name] = toolCfg
				agentConfig.Tools = append(agentConfig.Tools, name)
			}
		}
	}

	// Configure Authentication if JWKS URL is provided
	// All three fields (JWKSURL, Issuer, Audience) are required for JWT auth.
	// Incomplete configs are skipped here; user warning is handled in main.go.
	if opts.AuthJWKSURL != "" {
		if opts.AuthIssuer == "" || opts.AuthAudience == "" {
			// Incomplete auth config: create partial AuthConfig that will fail IsEnabled() check.
			// This allows main.go to detect and warn about the incomplete configuration.
			cfg.Server.Auth = &AuthConfig{
				Enabled:  false, // Mark as disabled due to incomplete config
				JWKSURL:  opts.AuthJWKSURL,
				Issuer:   opts.AuthIssuer,
				Audience: opts.AuthAudience,
			}
		} else {
			// Complete auth config: all three required fields are present
			// Default auth required to true if not specified
			requireAuth := true
			if opts.AuthRequired != nil {
				requireAuth = *opts.AuthRequired
			}

			cfg.Server.Auth = &AuthConfig{
				Enabled:     true,
				JWKSURL:     opts.AuthJWKSURL,
				Issuer:      opts.AuthIssuer,
				Audience:    opts.AuthAudience,
				RequireAuth: &requireAuth,
				// Zero-config secure default: Only exclude health and agent card.
				// This ensures /agents endpoints are protected by default.
				ExcludedPaths: []string{"/health", "/.well-known/agent-card.json"},
			}
		}
	}

	// Apply defaults
	// IMPORTANT: SetDefaults() is called AFTER setting zero-config specific values.
	// SetDefaults() will:
	// - Set defaults for fields that are nil/empty (not explicitly set)
	// - NOT override fields that are already set (like our streaming=true override)
	// - Handle all standard defaults (temperature, max_tokens, etc.)
	cfg.SetDefaults()

	// Apply tool approval overrides AFTER SetDefaults()
	// This allows CLI flags to override the smart defaults set by SetDefaults()
	ApplyToolApprovalOverrides(cfg, opts.ApproveTools, opts.NoApproveTools)

	return cfg
}

// parseDocsFolder parses the docs-folder path which may contain a remote path mapping.
// Syntax: "local_path" or "local_path:remote_path"
//
// Examples:
//   - "./test-docs" -> localPath="./test-docs", remotePath=""
//   - "./test-docs:/docs" -> localPath="./test-docs", remotePath="/docs"
//
// The remote path is used for Docker-based MCP services where the local directory
// is mounted at a different path inside the container.
func parseDocsFolder(docsFolder string) (localPath string, remotePath string) {
	// Check for colon separator (path mapping syntax)
	// On Windows, avoid splitting drive letters like "C:\path"
	if idx := strings.LastIndex(docsFolder, ":"); idx > 0 {
		// Check if this is a Windows drive letter (single letter before colon)
		if idx == 1 && len(docsFolder) > 2 && (docsFolder[0] >= 'A' && docsFolder[0] <= 'Z' || docsFolder[0] >= 'a' && docsFolder[0] <= 'z') {
			// Windows drive letter, no path mapping
			return docsFolder, ""
		}
		// Split on the last colon for path mapping
		localPath = docsFolder[:idx]
		remotePath = docsFolder[idx+1:]
		return localPath, remotePath
	}
	return docsFolder, ""
}

// expandDocsFolder auto-configures RAG from a docs folder path.
//
// Creates:
// - Vector store (chromem by default, or external like qdrant)
// - Embedder (auto-detected from LLM provider or explicit)
// - Document store with directory source
// - Search tool for the agent
//
// Supports path mapping syntax for Docker compatibility:
//   - "./test-docs:/docs" maps local ./test-docs to /docs in container
//
// This mirrors legacy pkg/config/config.go:expandDocsFolder()
func expandDocsFolder(cfg *Config, agentConfig *AgentConfig, opts ZeroConfig) {
	// Parse docs folder for path mapping (local:remote syntax)
	localPath, remotePath := parseDocsFolder(opts.DocsFolder)
	// Determine embedder provider
	// Priority: explicit > LLM provider has embeddings > ollama fallback
	embedderProvider := opts.EmbedderProvider
	if embedderProvider == "" {
		embedderProvider = detectEmbedderProvider(cfg)
	}

	// Determine embedder model
	embedderModel := opts.EmbedderModel
	if embedderModel == "" {
		embedderModel = detectEmbedderModelForProvider(embedderProvider)
	}

	// Create embedder config
	if cfg.Embedders == nil {
		cfg.Embedders = make(map[string]*EmbedderConfig)
	}

	// Get API key for embedder provider
	embedderAPIKey := ""
	if embedderProvider == "openai" {
		embedderAPIKey = os.Getenv("OPENAI_API_KEY")
	} else if embedderProvider == "cohere" {
		embedderAPIKey = os.Getenv("COHERE_API_KEY")
	}

	embedderConfig := &EmbedderConfig{
		Provider: embedderProvider,
		Model:    embedderModel,
		APIKey:   embedderAPIKey,
	}

	// Apply custom embedder URL if specified
	if opts.EmbedderURL != "" {
		embedderConfig.BaseURL = opts.EmbedderURL
	}

	cfg.Embedders["_rag_embedder"] = embedderConfig

	// Create vector store config
	if cfg.VectorStores == nil {
		cfg.VectorStores = make(map[string]*VectorStoreConfig)
	}

	vectorConfig := createVectorStoreConfig(opts)
	cfg.VectorStores["_rag_vectors"] = vectorConfig

	// Create document store config
	if cfg.DocumentStores == nil {
		cfg.DocumentStores = make(map[string]*DocumentStoreConfig)
	}

	// Determine if watching is enabled (default: true)
	watchEnabled := BoolValue(opts.RAGWatch, true)

	docStoreConfig := &DocumentStoreConfig{
		Source: &DocumentSourceConfig{
			Type: "directory",
			Path: localPath, // Use parsed local path (supports local:remote syntax)
			// Default exclusions for common non-document folders
			Exclude: []string{".git", "node_modules", "__pycache__", ".hector", "vendor"},
		},
		Chunking: &ChunkingConfig{
			Strategy: "simple", // Simple for zero-config (fast, predictable)
			Size:     1000,
			Overlap:  200,
		},
		VectorStore:         "_rag_vectors",
		Embedder:            "_rag_embedder",
		Watch:               watchEnabled,
		IncrementalIndexing: true, // Only re-index changed files
		Indexing:            &IndexingConfig{
			// Use defaults (NumCPU workers, standard retry)
		},
		Search: &DocumentSearchConfig{
			TopK:      10,
			Threshold: 0.0, // No threshold filtering - return all topK results
		},
	}

	// Configure MCP parser if specified (e.g., Docling)
	if opts.MCPParserTool != "" {
		toolNames := parseCommaSeparatedList(opts.MCPParserTool)
		if len(toolNames) > 0 {
			docStoreConfig.MCPParsers = &MCPParserConfig{
				ToolNames:  toolNames,
				Extensions: []string{".pdf", ".docx", ".pptx", ".xlsx", ".html"},
				PathPrefix: remotePath, // For Docker: maps local paths to container paths
			}
			docStoreConfig.MCPParsers.SetDefaults()
		}
	}

	cfg.DocumentStores["_rag_docs"] = docStoreConfig

	// Assign document store to agent
	// Note: The runtime will automatically create a search tool for agents with document stores
	ragDocs := []string{"_rag_docs"}
	agentConfig.DocumentStores = &ragDocs

	// Enable automatic context injection if requested
	// When enabled, RAG context is injected into prompts automatically
	// without requiring the agent to call the search tool explicitly
	if BoolValue(opts.IncludeContext, false) {
		agentConfig.IncludeContext = BoolPtr(true)
	}
}

// createVectorStoreConfig creates a vector store config based on zero-config options.
func createVectorStoreConfig(opts ZeroConfig) *VectorStoreConfig {
	vectorType := opts.VectorType
	if vectorType == "" {
		vectorType = "chromem" // Default to embedded
	}

	config := &VectorStoreConfig{
		Type: vectorType,
	}

	switch vectorType {
	case "chromem":
		// Embedded vector store with persistence
		config.PersistPath = ".hector/vectors"
		config.Compress = true

	case "qdrant":
		// External Qdrant
		if opts.VectorHost != "" {
			config.Host = opts.VectorHost
		} else {
			config.Host = "localhost"
			config.Port = 6334 // Qdrant gRPC port (REST is 6333)
		}
		if opts.VectorAPIKey != "" {
			config.APIKey = opts.VectorAPIKey
		}

	case "chroma":
		// External Chroma
		if opts.VectorHost != "" {
			config.Host = opts.VectorHost
		} else {
			config.Host = "localhost"
			config.Port = 8000 // Chroma default
		}

	case "weaviate":
		// External Weaviate
		if opts.VectorHost != "" {
			config.Host = opts.VectorHost
		} else {
			config.Host = "localhost"
			config.Port = 8080 // Weaviate default
		}
		if opts.VectorAPIKey != "" {
			config.APIKey = opts.VectorAPIKey
		}

	case "milvus":
		// External Milvus
		if opts.VectorHost != "" {
			config.Host = opts.VectorHost
		} else {
			config.Host = "localhost"
			config.Port = 19530 // Milvus default
		}

	case "pinecone":
		// Pinecone (cloud)
		if opts.VectorAPIKey != "" {
			config.APIKey = opts.VectorAPIKey
		} else {
			config.APIKey = os.Getenv("PINECONE_API_KEY")
		}
		// VectorHost can contain index name for Pinecone
		if opts.VectorHost != "" {
			config.IndexName = opts.VectorHost
		}
	}

	return config
}

// detectEmbedderProvider auto-detects the best embedder provider based on LLM provider.
// Logic: If LLM provider has embeddings (openai), use it. Otherwise check for API keys,
// then fallback to local ollama.
func detectEmbedderProvider(cfg *Config) string {
	// Check if we have an LLM config to infer from
	if cfg.LLMs != nil {
		if llmCfg, ok := cfg.LLMs["default"]; ok {
			switch llmCfg.Provider {
			case LLMProviderOpenAI:
				// OpenAI has embeddings - use it
				return "openai"
			case LLMProviderOllama:
				// Ollama has embeddings - use it
				return "ollama"
			case LLMProviderAnthropic, LLMProviderGemini:
				// These don't have embeddings, check for OpenAI API key first
				if os.Getenv("OPENAI_API_KEY") != "" {
					return "openai"
				}
				// Check for Cohere API key
				if os.Getenv("COHERE_API_KEY") != "" {
					return "cohere"
				}
				// Fallback to local ollama
				return "ollama"
			}
		}
	}

	// No LLM config, check for API keys
	if os.Getenv("OPENAI_API_KEY") != "" {
		return "openai"
	}
	if os.Getenv("COHERE_API_KEY") != "" {
		return "cohere"
	}

	// Final fallback: Ollama (works locally without API key)
	return "ollama"
}

// detectEmbedderModelForProvider returns the default model for a given embedder provider.
func detectEmbedderModelForProvider(provider string) string {
	switch provider {
	case "openai":
		return "text-embedding-3-small"
	case "ollama":
		return "nomic-embed-text"
	case "cohere":
		return "embed-english-v3.0"
	default:
		return "nomic-embed-text"
	}
}

// parseToolsList parses a tools string into a list of enabled tool names.
// Empty string or "all" returns all available tools.
// Comma-separated list returns only the specified tools.
func parseToolsList(toolsStr string, availableTools map[string]*ToolConfig) []string {
	toolsStr = strings.TrimSpace(toolsStr)
	if toolsStr == "" || strings.ToLower(toolsStr) == "all" {
		// Return all available tools
		result := make([]string, 0, len(availableTools))
		for name := range availableTools {
			result = append(result, name)
		}
		return result
	}

	// Parse comma-separated list
	parts := strings.Split(toolsStr, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			// Validate tool exists
			if _, ok := availableTools[name]; ok {
				result = append(result, name)
			}
		}
	}
	return result
}

// ApplyToolApprovalOverrides applies CLI-specified approval overrides to tool configs.
// This should be called AFTER SetDefaults() to override the smart defaults set by SetDefaults().
func ApplyToolApprovalOverrides(cfg *Config, approveTools, noApproveTools string) {
	if approveTools == "" && noApproveTools == "" {
		return
	}

	// Parse comma-separated tool lists
	approveList := parseCommaSeparatedList(approveTools)
	noApproveList := parseCommaSeparatedList(noApproveTools)

	// Ensure tools map exists
	if cfg.Tools == nil {
		cfg.Tools = make(map[string]*ToolConfig)
	}

	// Apply approval overrides
	for _, toolName := range approveList {
		applyToolApprovalOverride(cfg, toolName, true)
	}

	// Apply no-approval overrides
	for _, toolName := range noApproveList {
		applyToolApprovalOverride(cfg, toolName, false)
	}
}

// applyToolApprovalOverride applies an approval override to a tool config.
// Creates the tool config if it doesn't exist, then sets RequireApproval.
func applyToolApprovalOverride(cfg *Config, toolName string, enable bool) {
	if cfg.Tools[toolName] == nil {
		// Create tool config if it doesn't exist
		defaultConfigs := GetDefaultToolConfigs()
		if defaultCfg, ok := defaultConfigs[toolName]; ok {
			cfg.Tools[toolName] = &ToolConfig{
				Type:    defaultCfg.Type,
				Handler: defaultCfg.Handler,
			}
			cfg.Tools[toolName].SetDefaults()
		} else {
			// Unknown tool, create minimal config
			cfg.Tools[toolName] = &ToolConfig{
				Type: ToolTypeFunction,
			}
			cfg.Tools[toolName].SetDefaults()
		}
	}
	cfg.Tools[toolName].RequireApproval = BoolPtr(enable)
}

// parseCommaSeparatedList parses a comma-separated string into a list of trimmed strings.
func parseCommaSeparatedList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
