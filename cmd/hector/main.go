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

// Command hector is the CLI for Hector pkg.
//
// Usage:
//
//	hector serve                                    # Uses .hector/config.yaml (creates if missing)
//	hector serve --config config.yaml               # Uses specified config file
//	hector serve --provider anthropic --model ...   # CLI flags seed initial config
//	hector info --config config.yaml --agent assistant
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"

	hector "github.com/verikod/hector"
	"github.com/verikod/hector/pkg/auth"
	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/runtime"
	"github.com/verikod/hector/pkg/server"
	"github.com/verikod/hector/pkg/session"
	"github.com/verikod/hector/pkg/task"
	"github.com/verikod/hector/pkg/utils"
)

// CLI defines the command-line interface.
type CLI struct {
	Version  VersionCmd  `cmd:"" help:"Show version information."`
	Serve    ServeCmd    `cmd:"" help:"Start the A2A server."`
	Info     InfoCmd     `cmd:"" help:"Show agent information."`
	Validate ValidateCmd `cmd:"" help:"Validate configuration file."`
	Schema   SchemaCmd   `cmd:"" help:"Generate JSON Schema for config builder."`

	Config    string `short:"c" help:"Path to config file." type:"path"`
	LogLevel  string `help:"Log level (debug, info, warn, error)." default:"info"`
	LogFile   string `help:"Log file path (empty = stderr)."`
	LogFormat string `help:"Log format (simple, verbose, or custom)." default:"simple"`
}

// VersionCmd shows version information.
type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	version := hector.Version // Check ldflags-injected version first

	// Fallback to module version (for go install)
	if version == "dev" || version == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "(devel)" && info.Main.Version != "" {
				version = info.Main.Version
			}
		}
	}

	fmt.Printf("Hector %s", version)
	if hector.GitCommit != "unknown" && hector.GitCommit != "" {
		fmt.Printf(" (%s)", hector.GitCommit)
	}
	if hector.BuildDate != "unknown" && hector.BuildDate != "" {
		fmt.Printf(" built %s", hector.BuildDate)
	}
	fmt.Println()
	return nil
}

// ServeCmd starts the A2A server.
type ServeCmd struct {
	// LLM options (used to seed initial config if missing)
	Provider       string  `help:"LLM provider (anthropic, openai, gemini, ollama)."`
	Model          string  `help:"Model name."`
	APIKey         string  `name:"api-key" help:"API key (defaults to environment variable)."`
	BaseURL        string  `name:"base-url" help:"Custom API base URL."`
	Temperature    float64 `help:"Temperature for generation (0.0-2.0)."`
	MaxTokens      int     `name:"max-tokens" help:"Max tokens for generation."`
	Instruction    string  `help:"System instruction for the agent."`
	Role           string  `help:"Agent role."`
	MCPURL         string  `name:"mcp-url" help:"MCP server URL."`
	Tools          string  `help:"Enable built-in local tools. Empty string or 'all' enables all tools. Comma-separated list enables specific tools (e.g., 'text_editor')."`
	ApproveTools   string  `name:"approve-tools" help:"Enable approval for specific tools (comma-separated, e.g., bash,text_editor). Overrides smart defaults." placeholder:"TOOL1,TOOL2"`
	NoApproveTools string  `name:"no-approve-tools" help:"Disable approval for specific tools (comma-separated, e.g., text_editor). Overrides smart defaults." placeholder:"TOOL1,TOOL2"`
	Thinking       *bool   `help:"Enable thinking at API level (like --tools enables tools)." negatable:""`
	ThinkingBudget int     `name:"thinking-budget" help:"Token budget for thinking (default: 1024, must be < max-tokens)." default:"0"`
	Stream         *bool   `default:"true" negatable:"" help:"Enable streaming responses (use --no-stream to disable)"`

	// Storage options (enables task and session persistence)
	Storage   string `name:"storage" help:"Storage backend: sqlite, postgres, mysql (default: inmemory). Also enables checkpointing." placeholder:"BACKEND"`
	StorageDB string `name:"storage-db" help:"Storage database path/DSN (default: .hector/hector.db for sqlite)." placeholder:"PATH"`

	// Observability options
	Observe bool `help:"Enable observability (metrics + OTLP tracing to localhost:4317)."`

	// RAG options (document search)
	DocsFolder    string `name:"docs-folder" help:"Folder containing documents for RAG." type:"path" placeholder:"PATH"`
	RAGWatch      *bool  `name:"rag-watch" default:"true" negatable:"" help:"Watch docs folder for changes and auto-reindex (enabled by default)."`
	MCPParserTool string `name:"mcp-parser-tool" help:"MCP tool name(s) for document parsing (e.g., 'convert_document_into_docling_document'). Comma-separated for fallback chain." placeholder:"TOOL_NAME"`

	// Vector database options
	VectorType   string `name:"vector-type" help:"Vector database type: chromem (default), qdrant, chroma, pinecone, weaviate, milvus." placeholder:"TYPE"`
	VectorHost   string `name:"vector-host" help:"Vector database host:port (for qdrant, chroma, weaviate, milvus)." placeholder:"HOST:PORT"`
	VectorAPIKey string `name:"vector-api-key" help:"Vector database API key (for pinecone, authenticated qdrant)." placeholder:"KEY"`

	// Embedder options
	EmbedderProvider string `name:"embedder-provider" help:"Embedder provider: openai, ollama, cohere (auto-detected: openai if available, else ollama)." placeholder:"PROVIDER"`
	EmbedderModel    string `name:"embedder-model" help:"Embedder model (auto-detected from provider)." placeholder:"MODEL"`
	EmbedderURL      string `name:"embedder-url" help:"Embedder API base URL (for custom ollama/OpenAI-compatible endpoints)." placeholder:"URL"`
	IncludeContext   *bool  `name:"include-context" help:"Automatically inject RAG context into prompts (no need to call search tool)." negatable:""`

	// Studio mode (config editing)
	Studio      bool   `help:"Enable studio mode: config builder UI + auto-reload on save."`
	StudioRoles string `name:"studio-roles" help:"Comma-separated roles allowed to access studio (default: operator)." placeholder:"ROLE1,ROLE2"`

	// Server options
	Host      string `help:"Host to bind to." default:"0.0.0.0"`
	Port      int    `help:"Port to listen on." default:"8080"`
	Watch     bool   `help:"Watch config file for changes (auto-enabled with --studio)."`
	Ephemeral bool   `help:"Run without saving config to disk (for Docker/CI)."`

	// Auth options
	AuthJWKSURL  string `name:"auth-jwks-url" env:"AUTH0_JWKS_URL" help:"JWKS URL for JWT authentication." placeholder:"URL"`
	AuthIssuer   string `name:"auth-issuer" env:"AUTH0_ISSUER" help:"JWT issuer." placeholder:"ISSUER"`
	AuthAudience string `name:"auth-audience" env:"AUTH0_AUDIENCE" help:"JWT audience." placeholder:"AUDIENCE"`
	AuthClientID string `name:"auth-client-id" env:"AUTH0_CLIENT_ID" help:"Public Client ID for frontend app." placeholder:"CLIENT_ID"`
	AuthRequired *bool  `name:"auth-required" help:"Require authentication for all endpoints (default: true)." negatable:""`
}

func (c *ServeCmd) Run(cli *CLI) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("Shutting down...")
		cancel()
	}()

	// Validate ephemeral mode restrictions
	if c.Ephemeral {
		if c.Studio {
			return fmt.Errorf("--ephemeral cannot be used with --studio (studio requires config file)")
		}
		if c.Watch {
			return fmt.Errorf("--ephemeral cannot be used with --watch (watch requires config file)")
		}
	}

	// Define overrides function to apply CLI flags
	// This ensures CLI flags take precedence over YAML, even on hot-reload
	overrideFn := func(cfg *config.Config) {
		slog.Info("Applying configuration overrides", "cli_port", c.Port, "config_port_before", cfg.Server.Port)
		if c.Host != "" && c.Host != "0.0.0.0" {
			cfg.Server.Host = c.Host
		}
		if c.Port != 0 && c.Port != 8080 {
			cfg.Server.Port = c.Port
		}
		slog.Info("Configuration overrides applied", "config_port_after", cfg.Server.Port)

		// Studio config from CLI
		if c.Studio {
			if cfg.Server.Studio == nil {
				cfg.Server.Studio = &config.StudioConfig{}
			}
			cfg.Server.Studio.Enabled = true
			if c.StudioRoles != "" {
				roles := strings.Split(c.StudioRoles, ",")
				for i, role := range roles {
					roles[i] = strings.TrimSpace(role)
				}
				cfg.Server.Studio.AllowedRoles = roles
			}
		}

		// Auth config from CLI
		if c.AuthJWKSURL != "" || c.AuthIssuer != "" || c.AuthAudience != "" || c.AuthClientID != "" {
			if cfg.Server.Auth == nil {
				cfg.Server.Auth = &config.AuthConfig{}
			}
			if c.AuthJWKSURL != "" {
				cfg.Server.Auth.JWKSURL = c.AuthJWKSURL
			}
			if c.AuthIssuer != "" {
				cfg.Server.Auth.Issuer = c.AuthIssuer
			}
			if c.AuthAudience != "" {
				cfg.Server.Auth.Audience = c.AuthAudience
			}
			if c.AuthClientID != "" {
				cfg.Server.Auth.ClientID = c.AuthClientID
			}
			if c.AuthRequired != nil {
				cfg.Server.Auth.RequireAuth = c.AuthRequired
			}

			// Enable auth if overrides present
			hasJWKS := cfg.Server.Auth.JWKSURL != ""
			hasIssuer := cfg.Server.Auth.Issuer != ""
			hasAudience := cfg.Server.Auth.Audience != ""
			if hasJWKS || (hasIssuer && hasAudience) { // Relaxed check here, validation handles strictness
				cfg.Server.Auth.Enabled = true
			}
		}
	}

	// Determine config path (always use a config file, unless ephemeral)
	configPath := cli.Config
	if configPath == "" {
		configPath = utils.DefaultConfigPath()
	}

	// Load configuration (always creates config file if missing, unless ephemeral)
	// Pass overrides to loader so they persist on hot-reload
	cfg, loader, configPath, err := c.loadConfig(ctx, configPath, overrideFn, c.Ephemeral)
	if err != nil {
		return err
	}
	if loader != nil {
		defer loader.Close()
	}

	// Auth CLI overrides moved to overrideFn above

	// Validate Auth configuration (post-CLI-flags)
	if cfg.Server.Auth != nil {
		// Apply defaults first (e.g. RefreshInterval) to ensure validation doesn't fail on defaults
		cfg.Server.Auth.SetDefaults()

		if cfg.Server.Auth.Enabled {
			if err := cfg.Server.Auth.Validate(); err != nil {
				// User explicitly enabled auth (via config or flags) but it's invalid.
				// Fail fast instead of silently disabling it, to prevent insecure deployments.
				return fmt.Errorf("invalid auth configuration: %w (check logs for details)", err)
			}
		}

		// Warn about incomplete/dormant auth configuration
		if !cfg.Server.Auth.IsEnabled() {
			if cfg.Server.Auth.JWKSURL != "" || cfg.Server.Auth.Issuer != "" || cfg.Server.Auth.Audience != "" {
				slog.Warn("Incomplete auth configuration detected",
					"jwks_url", cfg.Server.Auth.JWKSURL != "",
					"issuer", cfg.Server.Auth.Issuer != "",
					"audience", cfg.Server.Auth.Audience != "",
					"status", "Authentication is DISABLED - all three fields (jwks_url, issuer, audience) are required")
			}
		}
	}

	// Create shared database pool for SQLite to prevent "database is locked" errors.
	// Both TaskStore and SessionService share the same connection pool.
	dbPool := config.NewDBPool()
	defer dbPool.Close()

	// Create session service with shared pool
	sessionSvc, err := session.NewSessionServiceFromConfig(cfg, dbPool)
	if err != nil {
		return fmt.Errorf("failed to create session service: %w", err)
	}

	// Build runtime with session service
	rt, err := runtime.New(cfg, runtime.WithSessionService(sessionSvc))
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	// Create task service for cascade cancellation
	taskService := task.NewInMemoryService()

	// Create per-agent executors
	executors := make(map[string]*server.Executor)
	for _, agentName := range cfg.ListAgents() {
		runnerCfg, err := rt.RunnerConfig(agentName)
		if err != nil {
			return fmt.Errorf("failed to create runner config for agent %s: %w", agentName, err)
		}
		executors[agentName] = server.NewExecutor(server.ExecutorConfig{
			RunnerConfig: *runnerCfg,
			TaskService:  taskService,
		})
	}

	// Create TaskStore with shared pool
	var serverOpts []server.HTTPServerOption
	taskStore, err := task.NewTaskStoreFromConfig(cfg, dbPool)
	if err != nil {
		return fmt.Errorf("failed to create task store: %w", err)
	}
	if taskStore != nil {
		serverOpts = append(serverOpts, server.WithTaskStore(taskStore))
		slog.Info("Task persistence enabled", "backend", cfg.Storage.Tasks.Backend, "database", cfg.Storage.Tasks.Database)
	}

	// Add task service for cascade cancellation
	serverOpts = append(serverOpts, server.WithTaskService(taskService))

	// Initialize Auth Validator if enabled
	if cfg.Server.Auth != nil && cfg.Server.Auth.IsEnabled() {
		validator, err := auth.NewJWTValidator(auth.JWTValidatorConfig{
			JWKSURL:         cfg.Server.Auth.JWKSURL,
			Issuer:          cfg.Server.Auth.Issuer,
			Audience:        cfg.Server.Auth.Audience,
			RefreshInterval: cfg.Server.Auth.RefreshInterval,
		})
		if err != nil {
			return fmt.Errorf("failed to create JWT validator: %w", err)
		}
		defer validator.Close()
		serverOpts = append(serverOpts, server.WithAuthValidator(validator))
		slog.Info("JWT Authentication enabled", "issuer", cfg.Server.Auth.Issuer)
	}

	srv := server.NewHTTPServer(cfg, executors, serverOpts...)

	// Enable studio mode if requested
	if c.Studio {
		// With unified config, we always have a config file
		srv.SetStudioMode(configPath)
		slog.Info("Studio mode enabled", "config_file", configPath)
	}

	// Set up reload function for studio mode (sync API reload via POST /api/config)
	// This is independent of file watcher - API triggers reload synchronously
	if c.Studio && loader != nil {
		slog.Debug("Setting up studio mode synchronous reload")
		provider := loader.Provider()
		reloadLoader := config.NewLoader(provider, config.WithOverrides(overrideFn))

		// doReload performs the actual reload work, returning an error if it fails
		var lastEnvVars map[string]string

		doReload := func() error {
			// Load .env file (for hot reload of env vars)
			newEnv, _ := config.ReloadDotEnvForConfig(configPath)
			if newEnv != nil {
				// Cleanup removed env vars
				for k := range lastEnvVars {
					if _, exists := newEnv[k]; !exists {
						_ = os.Unsetenv(k)
					}
				}
				lastEnvVars = newEnv
			}

			// Load and validate new config
			newCfg, err := reloadLoader.Load(ctx)
			if err != nil {
				return err
			}

			slog.Info("Reloading configuration...", "agents", len(newCfg.Agents))

			// Reload runtime
			if err := rt.Reload(newCfg); err != nil {
				return fmt.Errorf("failed to reload runtime: %w", err)
			}

			// Rebuild executors for HTTP server
			newExecutors := make(map[string]*server.Executor)
			for _, agentName := range newCfg.ListAgents() {
				runnerCfg, err := rt.RunnerConfig(agentName)
				if err != nil {
					slog.Error("Failed to create runner config", "agent", agentName, "error", err)
					continue
				}
				newExecutors[agentName] = server.NewExecutor(server.ExecutorConfig{
					RunnerConfig: *runnerCfg,
					TaskService:  taskService,
				})
			}

			// Hot-swap executors
			srv.UpdateExecutors(newCfg, newExecutors)
			slog.Info("✅ Configuration applied", "agents", len(newExecutors))
			return nil
		}

		// Set reload function for sync API calls
		srv.SetReloadFunc(doReload)
	}

	// Start file watcher if explicitly requested (optional, for external file edits)
	// Note: --studio no longer auto-enables --watch since API handles sync reload
	if c.Watch && loader != nil {
		provider := loader.Provider()

		reloadCallback := func(newCfg *config.Config) {
			slog.Info("Config file changed externally, reloading...")
			// Trigger the same reload logic
			if err := rt.Reload(newCfg); err != nil {
				slog.Error("Failed to reload config", "error", err)
				return
			}

			newExecutors := make(map[string]*server.Executor)
			for _, agentName := range newCfg.ListAgents() {
				runnerCfg, err := rt.RunnerConfig(agentName)
				if err != nil {
					slog.Error("Failed to create runner config", "agent", agentName, "error", err)
					continue
				}
				newExecutors[agentName] = server.NewExecutor(server.ExecutorConfig{
					RunnerConfig: *runnerCfg,
					TaskService:  taskService,
				})
			}
			srv.UpdateExecutors(newCfg, newExecutors)
			slog.Info("✅ Hot reload complete (file watcher)", "agents", len(newExecutors))
		}

		watchLoader := config.NewLoader(provider, config.WithOnChange(reloadCallback), config.WithOverrides(overrideFn))

		go func() {
			if err := watchLoader.Watch(ctx); err != nil && ctx.Err() == nil {
				slog.Error("Config watch error", "error", err)
			}
		}()
	}

	// Print startup info
	greenColor := "\033[38;2;16;185;129m"
	resetColor := "\033[0m"
	fmt.Printf("\n%s🚀 Hector server ready!%s\n", greenColor, resetColor)
	fmt.Printf("   Web UI:      http://%s\n", srv.Address())
	fmt.Printf("   Agent Card:  http://%s/.well-known/agent-card.json\n", srv.Address())
	fmt.Printf("   Discovery:   http://%s/agents\n", srv.Address())
	fmt.Printf("   Health:      http://%s/health\n", srv.Address())
	if cfg.Server.Transport == config.TransportGRPC {
		fmt.Printf("   gRPC:        %s\n", srv.GRPCAddress())
	}

	// Show storage persistence status
	if cfg.Storage.Tasks != nil && cfg.Storage.Tasks.IsSQL() {
		dbName := cfg.Storage.Tasks.Database
		if dbCfg, ok := cfg.Databases[dbName]; ok {
			fmt.Printf("   Storage:     %s (%s)\n", dbCfg.Driver, dbCfg.Database)
			fmt.Printf("   - Tasks:     persistent\n")
			if cfg.Storage.Sessions != nil && cfg.Storage.Sessions.IsSQL() {
				fmt.Printf("   - Sessions:  persistent\n")
			} else {
				fmt.Printf("   - Sessions:  in-memory\n")
			}
			if cfg.Storage.Checkpoint != nil && cfg.Storage.Checkpoint.IsEnabled() {
				fmt.Printf("   - Checkpoint: enabled (%s)\n", cfg.Storage.Checkpoint.Strategy)
			}
		}
	} else {
		fmt.Printf("   Storage:     in-memory (not persisted)\n")
	}

	// Show observability status
	if cfg.Observability != nil {
		if cfg.Observability.Tracing.Enabled {
			fmt.Printf("   Tracing:     %s (%s)\n", cfg.Observability.Tracing.Exporter, cfg.Observability.Tracing.Endpoint)
		}
		if cfg.Observability.Metrics.Enabled {
			fmt.Printf("   Metrics:     http://%s/metrics\n", srv.Address())
		}
	}

	// Initialize and start RAG document stores
	if len(cfg.DocumentStores) > 0 {
		// Index document stores asynchronously (non-blocking startup)
		go func() {
			if err := rt.IndexDocumentStores(ctx); err != nil {
				slog.Warn("Failed to index document stores", "error", err)
			}
		}()

		// Start file watching for auto re-indexing
		if err := rt.StartDocumentStoreWatching(ctx); err != nil {
			slog.Warn("Failed to start document store watching", "error", err)
		}

		// Show RAG status
		for name, store := range cfg.DocumentStores {
			if store.Source != nil {
				watchStatus := "enabled"
				if !store.Watch {
					watchStatus = "disabled"
				}
				fmt.Printf("   RAG Store:   %s (%s, watch=%s)\n", name, store.Source.Type, watchStatus)
			}
		}
	}

	fmt.Println("\n   Agents (A2A JSON-RPC endpoints):")
	for _, name := range cfg.ListAgents() {
		fmt.Printf("     - http://%s/agents/%s\n", srv.Address(), name)
	}
	fmt.Println("\nPress Ctrl+C to stop")

	// Start trigger scheduler for scheduled agents
	rt.StartScheduler()

	// Start server (blocks until context is cancelled)
	return srv.Start(ctx)
}

// loadConfig ensures configuration exists and loads it.
// If no config file exists, one is generated from CLI options.
// In ephemeral mode, config is generated in-memory without writing to disk.
// Returns: (config, loader, pathUsed, error)
func (c *ServeCmd) loadConfig(ctx context.Context, configPath string, overrideFn func(*config.Config), ephemeral bool) (*config.Config, *config.Loader, string, error) {
	// Build CLI options
	opts := config.CLIOptions{
		Provider:         c.Provider,
		Model:            c.Model,
		APIKey:           c.APIKey,
		BaseURL:          c.BaseURL,
		Instruction:      c.Instruction,
		Role:             c.Role,
		MCPURL:           c.MCPURL,
		Tools:            c.Tools,
		ApproveTools:     c.ApproveTools,
		NoApproveTools:   c.NoApproveTools,
		Thinking:         c.Thinking,
		ThinkingBudget:   c.ThinkingBudget,
		Streaming:        c.Stream,
		Storage:          c.Storage,
		StorageDB:        c.StorageDB,
		Observe:          c.Observe,
		DocsFolder:       c.DocsFolder,
		RAGWatch:         c.RAGWatch,
		MCPParserTool:    c.MCPParserTool,
		VectorType:       c.VectorType,
		VectorHost:       c.VectorHost,
		VectorAPIKey:     c.VectorAPIKey,
		EmbedderProvider: c.EmbedderProvider,
		EmbedderModel:    c.EmbedderModel,
		EmbedderURL:      c.EmbedderURL,
		IncludeContext:   c.IncludeContext,
		Host:             c.Host,
		Port:             c.Port,
		AuthJWKSURL:      c.AuthJWKSURL,
		AuthIssuer:       c.AuthIssuer,
		AuthAudience:     c.AuthAudience,
		AuthRequired:     c.AuthRequired,
	}

	// Handle temperature
	if c.Temperature > 0 {
		opts.Temperature = &c.Temperature
	}

	// Handle max tokens
	if c.MaxTokens > 0 {
		opts.MaxTokens = &c.MaxTokens
	}

	// Ephemeral mode: generate config in-memory without writing to disk
	if ephemeral {
		slog.Info("🚀 Running in ephemeral mode (no config file written)")

		// Load .env file from current directory
		_ = config.LoadDotEnv()

		// Generate config in-memory
		result, err := config.GenerateLeanConfig(opts, "")
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to generate ephemeral config: %w", err)
		}

		// Parse YAML into config struct
		cfg, err := config.ParseConfigBytes(result.ConfigYAML)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to parse ephemeral config: %w", err)
		}

		// Apply overrides
		if overrideFn != nil {
			overrideFn(cfg)
		}

		// No loader in ephemeral mode (no file watching)
		return cfg, nil, "(ephemeral)", nil
	}

	// Normal mode: ensure config exists (creates if missing)
	result, err := config.EnsureConfigExists(opts, configPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to ensure config exists: %w", err)
	}

	// Log config status
	if result.CreatedNew {
		slog.Info("✅ Created configuration", "path", configPath)
		if result.SkillUsed {
			slog.Info("   Using SKILL.md for agent instructions")
		}
	} else {
		slog.Info("📁 Using existing configuration", "path", configPath)
	}

	// Load .env file
	_ = config.LoadDotEnvForConfig(configPath)

	// Load and parse configuration
	// Apply overrides for CLI precedence
	cfg, loader, err := config.LoadConfigFile(ctx, configPath, config.WithOverrides(overrideFn))
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to load config: %w", err)
	}

	return cfg, loader, configPath, nil
}

// InfoCmd shows agent information.
type InfoCmd struct {
	Agent string `arg:"" optional:"" help:"Agent name to show info for."`
}

func (c *InfoCmd) Run(cli *CLI) error {
	ctx := context.Background()

	if cli.Config == "" {
		return fmt.Errorf("--config is required for info command")
	}

	_ = config.LoadDotEnvForConfig(cli.Config)
	cfg, loader, err := config.LoadConfigFile(ctx, cli.Config)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	defer loader.Close()

	if c.Agent == "" {
		fmt.Println("Available agents:")
		for _, name := range cfg.ListAgents() {
			agent, _ := cfg.GetAgent(name)
			desc := agent.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Printf("  - %s: %s\n", name, desc)
		}
		return nil
	}

	agent, ok := cfg.GetAgent(c.Agent)
	if !ok {
		return fmt.Errorf("agent %q not found", c.Agent)
	}

	fmt.Printf("\nAgent: %s\n", c.Agent)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Name:        %s\n", agent.GetDisplayName())
	if agent.Description != "" {
		fmt.Printf("Description: %s\n", agent.Description)
	}
	fmt.Printf("LLM:         %s\n", agent.LLM)
	if len(agent.Tools) > 0 {
		fmt.Printf("Tools:       %v\n", agent.Tools)
	}
	if len(agent.InputModes) > 0 {
		fmt.Printf("Input:       %v\n", agent.InputModes)
	}
	if len(agent.OutputModes) > 0 {
		fmt.Printf("Output:      %v\n", agent.OutputModes)
	}

	return nil
}

// printBanner prints a colored ASCII banner using hector-green (#10b981)
func printBanner() {
	// Check if stdout is a terminal
	if fileInfo, err := os.Stdout.Stat(); err == nil {
		if (fileInfo.Mode() & os.ModeCharDevice) == 0 {
			// Not a terminal, skip banner
			return
		}
	} else {
		return
	}

	// Green color: #10b981 = RGB(16, 185, 129)
	// Use ANSI RGB color mode: \033[38;2;R;G;Bm
	greenColor := "\033[38;2;16;185;129m"
	resetColor := "\033[0m"

	banner := `
██╗  ██╗███████╗ ██████╗████████╗ ██████╗ ██████╗ 
██║  ██║██╔════╝██╔════╝╚══██╔══╝██╔═══██╗██╔══██╗
███████║█████╗  ██║        ██║   ██║   ██║██████╔╝
██╔══██║██╔══╝  ██║        ██║   ██║   ██║██╔══██╗
██║  ██║███████╗╚██████╗   ██║   ╚██████╔╝██║  ██║
╚═╝  ╚═╝╚══════╝ ╚═════╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝
`
	fmt.Printf("%s%s%s\n", greenColor, banner, resetColor)
}

// shouldSkipBanner checks if command should skip banner
// In pkg, "info", "validate", and "schema" commands skip banner (they're informational, not server)
func shouldSkipBanner(args []string) bool {
	if len(args) < 2 {
		return false
	}

	// Check for informational commands
	for _, arg := range args {
		// Skip program name and flags, look for commands
		if arg == "info" || arg == "validate" || arg == "schema" {
			return true
		}
	}
	return false
}

func main() {
	// Skip banner for informational commands (info, validate)
	if !shouldSkipBanner(os.Args) {
		printBanner()
	}

	// Validate mutual exclusivity of --config and zero-config flags BEFORE kong parsing.
	// This provides clear error messages when users mix config file and zero-config modes.
	// Ported from legacy pkg/cli/mutual_exclusivity.go
	if !ShouldSkipValidation(os.Args) {
		if err := ValidateConfigMutualExclusivity(os.Args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		// Validate studio mode requires config file
		if err := ValidateStudioMode(os.Args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	_ = config.LoadDotEnv()

	cli := CLI{}
	ctx := kong.Parse(&cli,
		kong.Name("hector"),
		kong.Description("Hector pkg - Config-first AI Agent Platform"),
		kong.UsageOnError(),
	)

	// Initialize logger with CLI flags/env vars (before config loading)
	// Config file logger settings will be applied later if no CLI/env overrides
	_, _, _, cleanup, err := initLoggerFromCLI(cli.LogLevel, cli.LogFile, cli.LogFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	if cleanup != nil {
		defer cleanup()
	}

	err = ctx.Run(&cli)
	ctx.FatalIfErrorf(err)
}
