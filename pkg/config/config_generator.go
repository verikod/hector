// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel
//
// Hector Configuration Generator
//
// This file implements the unified configuration generation system.
// Every `hector serve` invocation produces a configuration file.
// There is no distinction between "zero-config" and "config" modes.

package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CLIOptions represents the command-line options provided by the user.
// Only fields that are explicitly set will be included in the generated config.
type CLIOptions struct {
	// LLM options
	Provider    string
	Model       string
	APIKey      string
	BaseURL     string
	Temperature *float64 // Pointer to distinguish "not set" from "set to 0"
	MaxTokens   *int

	// Thinking options
	Thinking       *bool
	ThinkingBudget int

	// Streaming
	Streaming *bool

	// Agent options
	Instruction string
	Role        string

	// Tool options
	MCPURL         string
	Tools          string
	ApproveTools   string
	NoApproveTools string

	// Storage options
	Storage   string
	StorageDB string

	// Observability
	Observe bool

	// RAG options
	DocsFolder       string
	RAGWatch         *bool
	MCPParserTool    string
	VectorType       string
	VectorHost       string
	VectorAPIKey     string
	EmbedderProvider string
	EmbedderModel    string
	EmbedderURL      string
	IncludeContext   *bool

	// Server options
	Host string
	Port int

	// Auth options
	AuthJWKSURL  string
	AuthIssuer   string
	AuthAudience string
	AuthRequired *bool

	// Skill file (auto-detected)
	SkillFile string
}

// EnvVarInfo tracks environment variables that should be documented.
type EnvVarInfo struct {
	Name     string
	Required bool
	Value    string // Current value (for checking if set)
}

// GeneratorResult contains the output of config generation.
type GeneratorResult struct {
	ConfigYAML []byte
	EnvVars    []EnvVarInfo
	ConfigPath string
	SkillUsed  bool
	CreatedNew bool
}

// GenerateLeanConfig creates a minimal configuration from CLI options.
// Only explicitly provided values are included in the output.
func GenerateLeanConfig(opts CLIOptions, configPath string) (*GeneratorResult, error) {
	result := &GeneratorResult{
		ConfigPath: configPath,
		EnvVars:    []EnvVarInfo{},
	}

	// Build the lean config structure
	config := make(map[string]interface{})

	// 1. Handle LLM configuration
	llmConfig := buildLLMConfig(opts, &result.EnvVars)
	if len(llmConfig) > 0 {
		config["llms"] = map[string]interface{}{
			"default": llmConfig,
		}
	}

	// 2. Handle Agent configuration
	agentConfig := buildAgentConfig(opts)
	if len(agentConfig) > 0 {
		config["agents"] = map[string]interface{}{
			"assistant": agentConfig,
		}
	} else {
		// Always create at least a minimal agent
		config["agents"] = map[string]interface{}{
			"assistant": map[string]interface{}{
				"llm": "default",
			},
		}
	}

	// 3. Handle Tools
	toolsConfig := buildToolsConfig(opts, &result.EnvVars)
	if len(toolsConfig) > 0 {
		config["tools"] = toolsConfig
		// Add tools to agent
		if agents, ok := config["agents"].(map[string]interface{}); ok {
			if assistant, ok := agents["assistant"].(map[string]interface{}); ok {
				toolNames := make([]string, 0, len(toolsConfig))
				for name := range toolsConfig {
					toolNames = append(toolNames, name)
				}
				assistant["tools"] = toolNames
			}
		}
	}

	// 4. Handle Server configuration (only non-defaults)
	serverConfig := buildServerConfig(opts)
	if len(serverConfig) > 0 {
		config["server"] = serverConfig
	}

	// 5. Handle Storage (if explicitly requested)
	if opts.Storage != "" && opts.Storage != "inmemory" {
		storageConfig := buildStorageConfig(opts)
		if len(storageConfig) > 0 {
			config["storage"] = storageConfig
			// Also need database config
			dbConfig := buildDatabaseConfig(opts)
			if len(dbConfig) > 0 {
				config["databases"] = map[string]interface{}{
					"_default": dbConfig,
				}
			}
		}
	}

	// 6. Handle RAG/Document Stores (if explicitly requested)
	if opts.DocsFolder != "" {
		ragConfigs := buildRAGConfig(opts, &result.EnvVars)
		for k, v := range ragConfigs {
			config[k] = v
		}
	}

	// 7. Handle Observability (if explicitly requested)
	if opts.Observe {
		config["observability"] = map[string]interface{}{
			"tracing": map[string]interface{}{
				"enabled":  true,
				"exporter": "otlp",
				"endpoint": "localhost:4317",
			},
			"metrics": map[string]interface{}{
				"enabled": true,
			},
		}
	}

	// 8. Handle SKILL.md
	if opts.SkillFile != "" {
		result.SkillUsed = true
		// Add instruction_file to agent
		if agents, ok := config["agents"].(map[string]interface{}); ok {
			if assistant, ok := agents["assistant"].(map[string]interface{}); ok {
				// Calculate relative path from config location
				configDir := filepath.Dir(configPath)
				relPath, err := filepath.Rel(configDir, opts.SkillFile)
				if err != nil {
					relPath = opts.SkillFile
				}
				assistant["instruction_file"] = relPath
			}
		}
	}

	// Marshal to YAML with header
	var buf bytes.Buffer
	buf.WriteString("# Auto-generated by Hector\n")
	buf.WriteString("# Edit this file to customize your agent.\n")
	if result.SkillUsed {
		buf.WriteString("# System prompt is loaded from SKILL.md\n")
	}
	buf.WriteString("\n")

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(config); err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}
	encoder.Close()

	result.ConfigYAML = buf.Bytes()
	return result, nil
}

// buildLLMConfig creates LLM configuration from CLI options.
func buildLLMConfig(opts CLIOptions, envVars *[]EnvVarInfo) map[string]interface{} {
	llm := make(map[string]interface{})

	// Provider (detect from env if not specified)
	provider := opts.Provider
	if provider == "" {
		provider = string(detectProviderFromEnv())
	}
	if provider != "" {
		llm["provider"] = provider
	}

	// Model
	if opts.Model != "" {
		llm["model"] = opts.Model
	}

	// API Key - use placeholder if from environment
	if opts.APIKey != "" {
		llm["api_key"] = opts.APIKey
	} else {
		// Check environment and use placeholder
		envVarName := getAPIKeyEnvVar(LLMProvider(provider))
		if envVarName != "" {
			if os.Getenv(envVarName) != "" {
				llm["api_key"] = fmt.Sprintf("${%s}", envVarName)
			}
			*envVars = append(*envVars, EnvVarInfo{
				Name:     envVarName,
				Required: LLMProvider(provider) != LLMProviderOllama,
				Value:    os.Getenv(envVarName),
			})
		}
	}

	// Base URL (only if explicitly set)
	if opts.BaseURL != "" {
		llm["base_url"] = opts.BaseURL
	}

	// Temperature (only if explicitly set)
	if opts.Temperature != nil {
		llm["temperature"] = *opts.Temperature
	}

	// Max tokens (only if explicitly set)
	if opts.MaxTokens != nil {
		llm["max_tokens"] = *opts.MaxTokens
	}

	// Thinking (only if explicitly enabled)
	if opts.Thinking != nil && *opts.Thinking {
		thinking := map[string]interface{}{
			"enabled": true,
		}
		if opts.ThinkingBudget > 0 {
			thinking["budget_tokens"] = opts.ThinkingBudget
		}
		llm["thinking"] = thinking
	}

	return llm
}

// buildAgentConfig creates agent configuration from CLI options.
func buildAgentConfig(opts CLIOptions) map[string]interface{} {
	agent := map[string]interface{}{
		"llm": "default",
	}

	// Instruction (only if explicitly set)
	if opts.Instruction != "" {
		agent["instruction"] = opts.Instruction
	}

	// Streaming (only if explicitly set)
	if opts.Streaming != nil {
		agent["streaming"] = *opts.Streaming
	}

	// Role
	if opts.Role != "" {
		agent["prompt"] = map[string]interface{}{
			"role": opts.Role,
		}
	}

	return agent
}

// buildToolsConfig creates tools configuration from CLI options.
func buildToolsConfig(opts CLIOptions, envVars *[]EnvVarInfo) map[string]interface{} {
	tools := make(map[string]interface{})

	// Parse approval override lists
	approveList := parseCommaSeparated(opts.ApproveTools)
	noApproveList := parseCommaSeparated(opts.NoApproveTools)
	approveSet := make(map[string]bool)
	noApproveSet := make(map[string]bool)
	for _, name := range approveList {
		approveSet[name] = true
	}
	for _, name := range noApproveList {
		noApproveSet[name] = true
	}

	// MCP URL
	if opts.MCPURL != "" {
		tools["mcp"] = map[string]interface{}{
			"type": "mcp",
			"url":  opts.MCPURL,
		}
	} else if os.Getenv("MCP_URL") != "" {
		tools["mcp"] = map[string]interface{}{
			"type": "mcp",
			"url":  "${MCP_URL}",
		}
		*envVars = append(*envVars, EnvVarInfo{
			Name:  "MCP_URL",
			Value: os.Getenv("MCP_URL"),
		})
	}

	// Built-in tools
	if opts.Tools != "" {
		defaultTools := GetDefaultToolConfigs()
		enabledTools := parseToolsList(opts.Tools, defaultTools)
		for _, name := range enabledTools {
			if toolCfg, ok := defaultTools[name]; ok {
				toolMap := map[string]interface{}{
					"type": string(toolCfg.Type),
				}
				if toolCfg.Handler != "" {
					toolMap["handler"] = toolCfg.Handler
				}
				// Apply approval overrides
				if approveSet[name] {
					toolMap["require_approval"] = true
				} else if noApproveSet[name] {
					toolMap["require_approval"] = false
				}
				tools[name] = toolMap
			}
		}
	}

	return tools
}

// parseCommaSeparated parses a comma-separated string into a slice.
func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// buildServerConfig creates server configuration from CLI options.
func buildServerConfig(opts CLIOptions) map[string]interface{} {
	server := make(map[string]interface{})

	// Only include non-default values
	if opts.Host != "" && opts.Host != "0.0.0.0" {
		server["host"] = opts.Host
	}
	if opts.Port != 0 && opts.Port != 8080 {
		server["port"] = opts.Port
	}

	// Auth (only if explicitly configured)
	if opts.AuthJWKSURL != "" && opts.AuthIssuer != "" && opts.AuthAudience != "" {
		auth := map[string]interface{}{
			"enabled":  true,
			"jwks_url": opts.AuthJWKSURL,
			"issuer":   opts.AuthIssuer,
			"audience": opts.AuthAudience,
		}
		if opts.AuthRequired != nil {
			auth["require_auth"] = *opts.AuthRequired
		}
		server["auth"] = auth
	}

	return server
}

// buildStorageConfig creates storage configuration.
func buildStorageConfig(opts CLIOptions) map[string]interface{} {
	return map[string]interface{}{
		"tasks": map[string]interface{}{
			"backend":  "sql",
			"database": "_default",
		},
		"sessions": map[string]interface{}{
			"backend":  "sql",
			"database": "_default",
		},
	}
}

// buildDatabaseConfig creates database configuration.
func buildDatabaseConfig(opts CLIOptions) map[string]interface{} {
	driver := opts.Storage
	if driver == "sqlite3" {
		driver = "sqlite"
	}

	db := map[string]interface{}{
		"driver": driver,
	}

	if opts.StorageDB != "" {
		db["database"] = opts.StorageDB
	} else if driver == "sqlite" {
		db["database"] = ".hector/hector.db"
	}

	return db
}

// buildRAGConfig creates RAG-related configurations.
func buildRAGConfig(opts CLIOptions, envVars *[]EnvVarInfo) map[string]interface{} {
	result := make(map[string]interface{})

	// Vector store
	vectorType := opts.VectorType
	if vectorType == "" {
		vectorType = "chromem"
	}
	vectorStore := map[string]interface{}{
		"type": vectorType,
	}
	if vectorType == "chromem" {
		vectorStore["persist_path"] = ".hector/vectors"
	}
	if opts.VectorHost != "" {
		vectorStore["host"] = opts.VectorHost
	}
	result["vector_stores"] = map[string]interface{}{
		"_rag_vectors": vectorStore,
	}

	// Embedder
	embedderProvider := opts.EmbedderProvider
	if embedderProvider == "" {
		// Auto-detect
		if os.Getenv("OPENAI_API_KEY") != "" {
			embedderProvider = "openai"
		} else {
			embedderProvider = "ollama"
		}
	}
	embedder := map[string]interface{}{
		"provider": embedderProvider,
	}
	if opts.EmbedderModel != "" {
		embedder["model"] = opts.EmbedderModel
	}
	if embedderProvider == "openai" {
		embedder["api_key"] = "${OPENAI_API_KEY}"
		*envVars = append(*envVars, EnvVarInfo{
			Name:  "OPENAI_API_KEY",
			Value: os.Getenv("OPENAI_API_KEY"),
		})
	}
	result["embedders"] = map[string]interface{}{
		"_rag_embedder": embedder,
	}

	// Document store
	docStore := map[string]interface{}{
		"source": map[string]interface{}{
			"type": "directory",
			"path": opts.DocsFolder,
		},
		"vector_store": "_rag_vectors",
		"embedder":     "_rag_embedder",
	}
	if opts.RAGWatch != nil {
		docStore["watch"] = *opts.RAGWatch
	}
	result["document_stores"] = map[string]interface{}{
		"_rag_docs": docStore,
	}

	return result
}

// getAPIKeyEnvVar returns the environment variable name for an LLM provider.
func getAPIKeyEnvVar(provider LLMProvider) string {
	switch provider {
	case LLMProviderOpenAI:
		return "OPENAI_API_KEY"
	case LLMProviderAnthropic:
		return "ANTHROPIC_API_KEY"
	case LLMProviderGemini:
		return "GEMINI_API_KEY"
	case LLMProviderDeepSeek:
		return "DEEPSEEK_API_KEY"
	case LLMProviderGroq:
		return "GROQ_API_KEY"
	case LLMProviderMistral:
		return "MISTRAL_API_KEY"
	case LLMProviderCohere:
		return "COHERE_API_KEY"
	default:
		return ""
	}
}

// GenerateEnvExample creates a .env.example file content.
func GenerateEnvExample(envVars []EnvVarInfo) []byte {
	if len(envVars) == 0 {
		return nil
	}

	var buf bytes.Buffer
	buf.WriteString("# Auto-generated by Hector\n")
	buf.WriteString("# Copy this file to .env and fill in the values\n\n")

	seen := make(map[string]bool)
	for _, env := range envVars {
		if seen[env.Name] {
			continue
		}
		seen[env.Name] = true

		if env.Required {
			buf.WriteString(fmt.Sprintf("%s=\n", env.Name))
		} else {
			buf.WriteString(fmt.Sprintf("# %s=\n", env.Name))
		}
	}

	return buf.Bytes()
}

// EnsureConfigExists ensures a configuration file exists, creating one if needed.
// Returns the path to the config file and whether it was created.
func EnsureConfigExists(opts CLIOptions, configPath string) (*GeneratorResult, error) {
	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		// Config exists, don't overwrite
		return &GeneratorResult{
			ConfigPath: configPath,
			CreatedNew: false,
		}, nil
	}

	// Check for SKILL.md in workspace root
	if opts.SkillFile == "" {
		// Try to detect SKILL.md
		configDir := filepath.Dir(configPath)
		workspaceRoot := configDir
		if filepath.Base(configDir) == ".hector" {
			workspaceRoot = filepath.Dir(configDir)
		}
		skillPath := filepath.Join(workspaceRoot, "SKILL.md")
		if _, err := os.Stat(skillPath); err == nil {
			opts.SkillFile = skillPath
		}
	}

	// Generate lean config
	result, err := GenerateLeanConfig(opts, configPath)
	if err != nil {
		return nil, err
	}
	result.CreatedNew = true

	// Ensure directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write config file
	if err := os.WriteFile(configPath, result.ConfigYAML, 0644); err != nil {
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	// Generate .env.example if .env doesn't exist
	workspaceRoot := filepath.Dir(configPath)
	if filepath.Base(filepath.Dir(configPath)) == ".hector" {
		workspaceRoot = filepath.Dir(filepath.Dir(configPath))
	}
	envPath := filepath.Join(workspaceRoot, ".env")
	envExamplePath := filepath.Join(workspaceRoot, ".env.example")

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if len(result.EnvVars) > 0 {
			envContent := GenerateEnvExample(result.EnvVars)
			if envContent != nil {
				// Only create .env.example if it doesn't exist
				if _, err := os.Stat(envExamplePath); os.IsNotExist(err) {
					// We ignore the error here as it's just an example file and not critical
					_ = os.WriteFile(envExamplePath, envContent, 0644)
				}
			}
		}
	}

	return result, nil
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
