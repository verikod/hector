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
//	hector serve --config config.yaml
//	hector serve --provider anthropic --model claude-sonnet-4-20250514
//	hector info --config config.yaml --agent assistant
package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"
	"gopkg.in/yaml.v3"

	"github.com/kadirpekel/hector/pkg/auth"
	"github.com/kadirpekel/hector/pkg/config"
	"github.com/kadirpekel/hector/pkg/runtime"
	"github.com/kadirpekel/hector/pkg/server"
	"github.com/kadirpekel/hector/pkg/session"
	"github.com/kadirpekel/hector/pkg/task"
	"github.com/kadirpekel/hector/pkg/utils"
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

// marshalYAMLWithIndent marshals a value to YAML with explicit 2-space indentation.
func marshalYAMLWithIndent(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2) // Explicitly set 2-space indentation
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// VersionCmd shows version information.
type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "(devel)" && info.Main.Version != "" {
			version = info.Main.Version
		}
	}
	fmt.Printf("Hector pkg version %s\n", version)
	return nil
}

// ServeCmd starts the A2A server.
type ServeCmd struct {
	// Zero-config options
	Provider       string  `help:"LLM provider (anthropic, openai, gemini, ollama)."`
	Model          string  `help:"Model name."`
	APIKey         string  `name:"api-key" help:"API key (defaults to environment variable)."`
	BaseURL        string  `name:"base-url" help:"Custom API base URL."`
	Temperature    float64 `help:"Temperature for generation." default:"0.7"`
	MaxTokens      int     `name:"max-tokens" help:"Max tokens for generation." default:"4096"`
	Instruction    string  `help:"System instruction for the agent."`
	Role           string  `help:"Agent role."`
	MCPURL         string  `name:"mcp-url" help:"MCP server URL."`
	Tools          string  `help:"Enable built-in local tools. Empty string or 'all' enables all tools. Comma-separated list enables specific tools (e.g., 'read_file,write_file')."`
	ApproveTools   string  `name:"approve-tools" help:"Enable approval for specific tools (comma-separated, e.g., execute_command,write_file). Overrides smart defaults." placeholder:"TOOL1,TOOL2"`
	NoApproveTools string  `name:"no-approve-tools" help:"Disable approval for specific tools (comma-separated, e.g., write_file). Overrides smart defaults." placeholder:"TOOL1,TOOL2"`
	Thinking       *bool   `help:"Enable thinking at API level (like --tools enables tools)." negatable:""`
	ThinkingBudget int     `name:"thinking-budget" help:"Token budget for thinking (default: 1024, must be < max-tokens)." default:"0"`
	Stream         *bool   `default:"true" negatable:"" help:"Enable streaming responses (use --no-stream to disable)"`

	// Storage options (enables task and session persistence)
	Storage   string `name:"storage" help:"Storage backend: sqlite, postgres, mysql (default: inmemory). Also enables checkpointing." placeholder:"BACKEND"`
	StorageDB string `name:"storage-db" help:"Storage database path/DSN (default: .hector/hector.db for sqlite)." placeholder:"PATH"`

	// Observability options
	Observe bool `help:"Enable observability (metrics + OTLP tracing to localhost:4317)."`

	// RAG options (zero-config document search)
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

	// Studio mode (dev/edit mode)
	Studio bool `help:"Enable studio mode: config builder UI + auto-reload on save."`

	// Server options
	Port  int  `help:"Port to listen on." default:"8080"`
	Watch bool `help:"Watch config file for changes (auto-enabled with --studio)."`

	// Auth options
	AuthJWKSURL  string `name:"auth-jwks-url" help:"JWKS URL for JWT authentication." placeholder:"URL"`
	AuthIssuer   string `name:"auth-issuer" help:"JWT issuer." placeholder:"ISSUER"`
	AuthAudience string `name:"auth-audience" help:"JWT audience." placeholder:"AUDIENCE"`
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

	// Determine config path
	configPath := cli.Config
	if configPath == "" && !c.isZeroConfig() {
		// Use unified default config path
		configPath = utils.DefaultConfigPath()
	}

	// Load configuration
	cfg, loader, configPathUsed, err := c.loadConfig(ctx, configPath, c.Studio)
	if err != nil {
		return err
	}
	if loader != nil {
		defer loader.Close()
	}

	// Warn about incomplete auth configuration
	if cfg.Server.Auth != nil && !cfg.Server.Auth.IsEnabled() {
		if cfg.Server.Auth.JWKSURL != "" || cfg.Server.Auth.Issuer != "" || cfg.Server.Auth.Audience != "" {
			slog.Warn("Incomplete auth configuration detected",
				"jwks_url", cfg.Server.Auth.JWKSURL != "",
				"issuer", cfg.Server.Auth.Issuer != "",
				"audience", cfg.Server.Auth.Audience != "",
				"status", "Authentication is DISABLED - all three fields (jwks_url, issuer, audience) are required")
		}
	}

	// Override port if explicitly specified
	if c.Port != 0 && c.Port != 8080 {
		cfg.Server.Port = c.Port
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
		slog.Info("Task persistence enabled", "backend", cfg.Server.Tasks.Backend, "database", cfg.Server.Tasks.Database)
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
		// In studio mode, we need a save path for config updates
		savePath := configPathUsed
		if savePath == "" {
			// Zero-config in studio: use default save path
			savePath = utils.DefaultConfigPath()

			// Create initial config file from current zero-config state
			if err := c.saveConfigToFile(cfg, savePath); err != nil {
				slog.Warn("Failed to create initial config file", "error", err)
			} else {
				slog.Info("Created initial config from zero-config", "path", savePath)

				// Create a FileProvider + Loader to enable file watching
				// This is critical: zero-config returns nil loader, but we need
				// a loader with FileProvider to watch for config changes in studio mode
				_ = config.LoadDotEnvForConfig(savePath)
				_, newLoader, err := config.LoadConfigFile(ctx, savePath)
				if err != nil {
					slog.Warn("Failed to create loader for watching", "error", err)
				} else {
					// Close old loader if any (should be nil in zero-config)
					if loader != nil {
						loader.Close()
					}
					loader = newLoader
					configPathUsed = savePath
					slog.Debug("Created file loader for config watching", "path", savePath)
				}
			}
		}

		srv.SetStudioMode(savePath)
		c.Watch = true // Auto-enable watch

		if configPathUsed == "" {
			slog.Info("Studio mode with zero-config base", "save_path", savePath)
		} else {
			slog.Info("Studio mode enabled", "config_file", savePath)
		}
	}

	// Start config watching if enabled (auto-enabled by --studio)
	if c.Watch && loader != nil {
		// Register hot-reload callback
		reloadCallback := func(newCfg *config.Config) {
			slog.Info("Config file changed, reloading...")

			// Reload runtime
			if err := rt.Reload(newCfg); err != nil {
				slog.Error("Failed to reload runtime", "error", err)
				return
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
			slog.Info("✅ Hot reload complete", "agents", len(newExecutors))
		}

		// Create new loader with onChange callback
		provider := loader.Provider()
		loader = config.NewLoader(provider, config.WithOnChange(reloadCallback))

		// Start watching
		go func() {
			if err := loader.Watch(ctx); err != nil && ctx.Err() == nil {
				slog.Error("Config watch error", "error", err)
			}
		}()
	}

	// Print startup info
	greenColor := "\033[38;2;16;185;129m"
	resetColor := "\033[0m"
	fmt.Printf("\n%s🚀 Hector pkg server ready!%s\n", greenColor, resetColor)
	fmt.Printf("   Web UI:      http://%s\n", srv.Address())
	fmt.Printf("   Agent Card:  http://%s/.well-known/agent-card.json\n", srv.Address())
	fmt.Printf("   Discovery:   http://%s/agents\n", srv.Address())
	fmt.Printf("   Health:      http://%s/health\n", srv.Address())
	if cfg.Server.Transport == config.TransportGRPC {
		fmt.Printf("   gRPC:        %s\n", srv.GRPCAddress())
	}

	// Show storage persistence status
	if cfg.Server.Tasks != nil && cfg.Server.Tasks.IsSQL() {
		dbName := cfg.Server.Tasks.Database
		if dbCfg, ok := cfg.Databases[dbName]; ok {
			fmt.Printf("   Storage:     %s (%s)\n", dbCfg.Driver, dbCfg.Database)
			fmt.Printf("   - Tasks:     persistent\n")
			if cfg.Server.Sessions != nil && cfg.Server.Sessions.IsSQL() {
				fmt.Printf("   - Sessions:  persistent\n")
			} else {
				fmt.Printf("   - Sessions:  in-memory\n")
			}
			if cfg.Server.Checkpoint != nil && cfg.Server.Checkpoint.IsEnabled() {
				fmt.Printf("   - Checkpoint: enabled (%s)\n", cfg.Server.Checkpoint.Strategy)
			}
		}
	} else {
		fmt.Printf("   Storage:     in-memory (not persisted)\n")
	}

	// Show observability status
	if cfg.Server.Observability != nil {
		if cfg.Server.Observability.Tracing.Enabled {
			fmt.Printf("   Tracing:     %s (%s)\n", cfg.Server.Observability.Tracing.Exporter, cfg.Server.Observability.Tracing.Endpoint)
		}
		if cfg.Server.Observability.Metrics.Enabled {
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

	// Start server (blocks until context is cancelled)
	return srv.Start(ctx)
}

// isZeroConfig checks if we're using zero-config mode (CLI flags instead of file).
func (c *ServeCmd) isZeroConfig() bool {
	return c.Provider != "" || c.Model != "" || c.MCPURL != "" ||
		c.Tools != "" || c.DocsFolder != "" || c.Storage != "" ||
		c.AuthJWKSURL != "" || c.AuthIssuer != "" || c.AuthAudience != ""
}

// loadConfig loads configuration from file or creates zero-config.
// Returns: (config, loader, pathUsed, error)
// pathUsed is empty string for zero-config mode.
func (c *ServeCmd) loadConfig(ctx context.Context, configPath string, isStudioMode bool) (*config.Config, *config.Loader, string, error) {
	if configPath != "" {
		// Check if file exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			// File doesn't exist - create minimal config from zero-config defaults
			slog.Info("Config file not found, creating from defaults", "path", configPath)

			if err := c.createMinimalConfig(configPath); err != nil {
				return nil, nil, "", fmt.Errorf("failed to create config: %w", err)
			}
		}

		_ = config.LoadDotEnvForConfig(configPath)
		cfg, loader, err := config.LoadConfigFile(ctx, configPath)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to load config: %w", err)
		}
		slog.Info("Loaded configuration", "path", configPath)
		return cfg, loader, configPath, nil
	}

	// Zero-config mode
	slog.Info("Using zero-config mode")

	// Handle streaming default
	streaming := c.Stream
	if streaming == nil {
		streaming = config.BoolPtr(true)
	}

	cfg := config.CreateZeroConfig(config.ZeroConfig{
		Provider:       c.Provider,
		Model:          c.Model,
		APIKey:         c.APIKey,
		BaseURL:        c.BaseURL,
		Temperature:    c.Temperature,
		MaxTokens:      c.MaxTokens,
		Instruction:    c.Instruction,
		Role:           c.Role,
		MCPURL:         c.MCPURL,
		Tools:          c.Tools,
		ApproveTools:   c.ApproveTools,
		NoApproveTools: c.NoApproveTools,
		Thinking:       c.Thinking,
		ThinkingBudget: c.ThinkingBudget,
		Streaming:      streaming,
		Storage:        c.Storage,
		StorageDB:      c.StorageDB,
		Observe:        c.Observe,
		Port:           c.Port,
		// RAG options
		DocsFolder:       c.DocsFolder,
		RAGWatch:         c.RAGWatch,
		MCPParserTool:    c.MCPParserTool,
		VectorType:       c.VectorType,
		VectorHost:       c.VectorHost,
		VectorAPIKey:     c.VectorAPIKey,
		EmbedderProvider: c.EmbedderProvider,
		EmbedderModel:    c.EmbedderModel,
		EmbedderURL:      c.EmbedderURL,
		// Auth options
		AuthJWKSURL:  c.AuthJWKSURL,
		AuthIssuer:   c.AuthIssuer,
		AuthAudience: c.AuthAudience,
		AuthRequired: c.AuthRequired,
	})

	if isStudioMode {
		slog.Info("💡 Studio with zero-config: edits will be saved to " + utils.DefaultConfigPath())
	}

	// Log enabled features
	if c.Tools != "" {
		if c.Tools == "all" || strings.TrimSpace(c.Tools) == "" {
			slog.Info("All built-in local tools enabled")
		} else {
			slog.Info("Selected built-in local tools enabled", "tools", c.Tools)
		}
	}
	if c.Storage != "" && c.Storage != "inmemory" {
		dbInfo := c.StorageDB
		if dbInfo == "" {
			switch c.Storage {
			case "sqlite", "sqlite3":
				dbInfo = utils.DefaultDatabasePath()
			case "postgres":
				dbInfo = "localhost:5432/hector"
			case "mysql":
				dbInfo = "localhost:3306/hector"
			}
		}
		slog.Info("Persistent storage enabled", "backend", c.Storage, "database", dbInfo)
		slog.Info("Checkpointing auto-enabled", "strategy", "hybrid")
	}
	if c.Observe {
		slog.Info("Observability enabled", "tracing", "otlp://localhost:4317", "metrics", "prometheus")
	}
	if c.DocsFolder != "" {
		// Determine what will be used (for logging purposes)
		vectorType := c.VectorType
		if vectorType == "" {
			vectorType = "chromem"
		}
		embedderProvider := c.EmbedderProvider
		if embedderProvider == "" {
			embedderProvider = "(auto-detected)"
		}
		embedderModel := c.EmbedderModel
		if embedderModel == "" {
			embedderModel = "(auto-detected)"
		}
		watchEnabled := c.RAGWatch == nil || *c.RAGWatch
		slog.Info("RAG enabled",
			"docs_folder", c.DocsFolder,
			"vector_db", vectorType,
			"embedder", embedderProvider+"/"+embedderModel,
			"watch", watchEnabled)
		if c.MCPParserTool != "" {
			slog.Info("MCP document parsing enabled", "tools", c.MCPParserTool)
		}
	}

	return cfg, nil, "", nil
}

// createMinimalConfig creates a minimal viable config using secure templates.
// Uses environment variable references instead of expanded values to avoid exposing secrets.
func (c *ServeCmd) createMinimalConfig(path string) error {
	// Ensure .hector directory exists
	if _, err := utils.EnsureHectorDir("."); err != nil {
		return err
	}

	// SECURITY: Use secure template with env var placeholders instead of
	// CreateZeroConfig which expands env vars and would write API keys to disk
	template := config.StudioConfigTemplate()

	if err := os.WriteFile(path, []byte(template), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	slog.Info("✅ Created minimal config from template", "path", path)
	return nil
}

// saveConfigToFile saves a config to a YAML file.
func (c *ServeCmd) saveConfigToFile(cfg *config.Config, path string) error {
	// Ensure directory exists
	if _, err := utils.EnsureHectorDir("."); err != nil {
		return err
	}

	// Serialize to YAML with 2-space indentation
	yamlData, err := marshalYAMLWithIndent(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, yamlData, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
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
