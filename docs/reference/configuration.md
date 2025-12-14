# Configuration

Hector's configuration system enables declarative agent definition with validation, defaults, and hot reload.

## Configuration Architecture

### Structure

```go
type Config struct {
    Version string

    LLMs           map[string]*LLMConfig
    Embedders      map[string]*EmbedderConfig
    Tools          map[string]*ToolConfig
    Agents         map[string]*AgentConfig
    VectorStores   map[string]*VectorStoreConfig
    DocumentStores map[string]*DocumentStoreConfig
    Databases      map[string]*DatabaseConfig

    Server ServerConfig
    Logger LoggerConfig
}
```

### Lifecycle

```
1. Load YAML file
2. Parse into Config struct
3. Interpolate environment variables
4. Apply defaults
5. Validate structure
6. Build runtime components
```

## Configuration Loading

### From File

```go
cfg, err := config.LoadFromFile("config.yaml")
```

**Process:**
1. Read YAML file
2. Unmarshal to Config struct
3. Apply SetDefaults()
4. Run Validate()

### From Zero-Config

```go
cfg := config.CreateZeroConfig(zeroConfigOpts)
```

**Generated from CLI flags:**

```bash
hector serve \
  --model gpt-4o \
  --tools \
  --docs-folder ./docs \
  --storage sqlite
```

Becomes:

```yaml
version: "2"

llms:
  default:
    provider: openai
    model: gpt-4o

agents:
  assistant:
    llm: default
    tools: [read_file, write_file, ...]

databases:
  _default:
    driver: sqlite
```

## Environment Variables

### Interpolation

```yaml
llms:
  default:
    api_key: ${OPENAI_API_KEY}

databases:
  main:
    password: ${DB_PASSWORD}
```

**Syntax:**

- `${VAR}`: Required (error if missing)
- `${VAR:default}`: Optional with default value

### .env Files

Automatically loaded:

```bash
# .env
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
DB_PASSWORD=secret
```

**Search paths:**
1. Current directory `.env`
2. Parent directories (up to 5 levels)
3. System environment variables

## Defaults

### Global Defaults

Applied automatically via `SetDefaults()`:

```go
func (c *Config) SetDefaults() {
    if c.Version == "" {
        c.Version = "2"
    }

    c.Server.SetDefaults()

    for _, llmCfg := range c.LLMs {
        llmCfg.SetDefaults()
    }

    for _, agentCfg := range c.Agents {
        agentCfg.SetDefaults()
    }
}
```

### Component Defaults

**LLM:**

```go
func (c *LLMConfig) SetDefaults() {
    if c.Temperature == nil {
        temp := 0.7
        c.Temperature = &temp
    }

    if c.MaxTokens == nil {
        tokens := 4096
        c.MaxTokens = &tokens
    }
}
```

**Agent:**

```go
func (c *AgentConfig) SetDefaults() {
    if c.Visibility == "" {
        c.Visibility = "public"
    }

    if c.InputModes == nil {
        c.InputModes = []string{"text/plain"}
    }

    if c.OutputModes == nil {
        c.OutputModes = []string{"text/plain"}
    }
}
```

**Server:**

```go
func (c *ServerConfig) SetDefaults() {
    if c.Host == "" {
        c.Host = "0.0.0.0"
    }

    if c.Port == 0 {
        c.Port = 8080
    }

    if c.Transport == "" {
        c.Transport = TransportJSONRPC
    }
}
```

## Validation

### Schema Validation

```go
func (c *Config) Validate() error {
    if c.Version != "2" {
        return errors.New("version must be '2'")
    }

    // Validate LLMs
    for name, llm := range c.LLMs {
        if err := llm.Validate(); err != nil {
            return fmt.Errorf("llm %q: %w", name, err)
        }
    }

    // Validate agents
    for name, agent := range c.Agents {
        if err := agent.Validate(); err != nil {
            return fmt.Errorf("agent %q: %w", name, err)
        }
    }

    return nil
}
```

### Component Validation

**LLM:**

```go
func (c *LLMConfig) Validate() error {
    if c.Provider == "" {
        return errors.New("provider is required")
    }

    if c.Model == "" {
        return errors.New("model is required")
    }

    if c.Provider != "ollama" && c.APIKey == "" {
        return errors.New("api_key is required")
    }

    return nil
}
```

**Agent:**

```go
func (c *AgentConfig) Validate() error {
    if c.LLM == "" && c.Type != "remote" {
        return errors.New("llm is required for non-remote agents")
    }

    // Validate tool references
    for _, toolName := range c.Tools {
        if !toolExists(toolName) {
            return fmt.Errorf("unknown tool: %s", toolName)
        }
    }

    return nil
}
```

### Reference Validation

Check that referenced components exist:

```yaml
agents:
  assistant:
    llm: default        # Must exist in llms
    tools:
      - search          # Must exist in tools
    document_stores:
      - knowledge       # Must exist in document_stores
```

Validation fails if references are broken.

## Hot Reload

### Watch Mode

```bash
hector serve --config config.yaml --watch
```

Monitors file for changes and reloads automatically.

### Reload Process

```
1. Detect file change
2. Load new config
3. Parse and validate
4. If valid:
   - Build new components
   - Atomic swap in runtime
   - Cleanup old components
5. If invalid:
   - Log error
   - Keep old config
```

### What Reloads

**Components:**

- LLM configurations
- Agent definitions
- Tool configurations
- RAG document stores
- Embedder settings

**Preserved:**

- Active sessions
- Session data
- Index service
- Checkpoint state
- HTTP connections

### Reload Atomicity

```go
func (r *Runtime) Reload(newCfg *config.Config) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    // Validate first
    if err := newCfg.Validate(); err != nil {
        return err  // No changes
    }

    // Build new components
    newLLMs, err := buildLLMs(newCfg)
    if err != nil {
        return err  // Rollback
    }

    // Atomic swap
    oldLLMs := r.llms
    r.llms = newLLMs

    // Cleanup old
    for _, llm := range oldLLMs {
        llm.Close()
    }

    return nil
}
```

No partial updates. Either full reload or no change.

## Configuration Modes

### File-Based

```bash
hector serve --config production.yaml
```

Explicit YAML configuration file.

### Zero-Config

```bash
hector serve --model gpt-4o --tools
```

Generated configuration from CLI flags.

### Hybrid

```bash
hector serve --config base.yaml --model gpt-4-turbo
```

Override file config with CLI flags.

## Configuration Providers

### File Provider

Default YAML file loader:

```go
type FileProvider struct {
    path string
}

func (p *FileProvider) Load() (*Config, error) {
    data, err := os.ReadFile(p.path)
    return parseYAML(data)
}
```

### Environment Provider

Future: Load from environment variables:

```bash
export HECTOR_LLM_DEFAULT_MODEL=gpt-4o
export HECTOR_AGENT_ASSISTANT_INSTRUCTION="You are helpful"
```

### Remote Provider

Future: Load from configuration server:

```go
type RemoteProvider struct {
    url string
}

func (p *RemoteProvider) Load() (*Config, error) {
    resp, err := http.Get(p.url + "/config")
    return parseJSON(resp.Body)
}
```

## Configuration Schema

### JSON Schema Generation

```bash
hector schema > schema.json
```

Generates JSON Schema for IDE autocomplete.

### VSCode Integration

```json
{
  "yaml.schemas": {
    "./schema.json": "*.yaml"
  }
}
```

Enables:

- Autocomplete
- Validation
- Inline documentation
- Type checking

## Configuration Inheritance

### Base Configuration

```yaml
# base.yaml
llms:
  default:
    provider: openai
    temperature: 0.7

server:
  port: 8080
```

### Environment Overrides

```yaml
# production.yaml
extends: base.yaml

llms:
  default:
    model: gpt-4o  # Override
    max_tokens: 8192

server:
  port: 443
  auth:
    enabled: true
```

Merge strategy:

- Maps: Deep merge
- Arrays: Replace
- Scalars: Override

## Configuration Validation Modes

### Strict Mode

```bash
hector validate --config config.yaml --strict
```

Fails on:

- Unknown fields
- Deprecated options
- Missing recommended settings

### Lenient Mode (Default)

```bash
hector validate --config config.yaml
```

Warns on:

- Unknown fields (ignored)
- Deprecated options
- Suboptimal settings

## Configuration Best Practices

### Secrets Management

Never commit secrets:

```yaml
# ✅ Good
api_key: ${OPENAI_API_KEY}

# ❌ Bad
api_key: sk-proj-abc123...
```

### Environment-Specific Configs

Separate configs per environment:

```
configs/
  dev.yaml       # Development
  staging.yaml   # Staging
  production.yaml  # Production
```

### Shared Settings

Extract common configuration:

```yaml
# common.yaml
llms:
  default:
    temperature: 0.7
    max_tokens: 4096

# prod.yaml
extends: common.yaml
llms:
  default:
    model: gpt-4o
```

### Validation in CI

```bash
# .github/workflows/ci.yaml
- name: Validate Config
  run: hector validate --config config/production.yaml --strict
```

## Configuration Documentation

### Inline Comments

```yaml
llms:
  default:
    provider: openai
    model: gpt-4o
    # Temperature controls randomness (0.0-2.0)
    # Lower = more focused, Higher = more creative
    temperature: 0.7

    # Max tokens for completion
    # Includes prompt + response
    max_tokens: 4096
```

### Schema Documentation

JSON Schema includes descriptions:

```json
{
  "properties": {
    "temperature": {
      "type": "number",
      "minimum": 0.0,
      "maximum": 2.0,
      "default": 0.7,
      "description": "Controls randomness in generation"
    }
  }
}
```

Shown in IDE tooltips.

## Configuration Migration

### Version Upgrades

```bash
hector migrate --from v1 --to v2 --config old-config.yaml
```

Converts old config to new format.

### Migration Warnings

```
⚠ llm.temperature renamed to llm.generation.temperature
⚠ agent.context_strategy renamed to agent.working_memory
✓ Migrated 5 configurations
```

## Configuration Testing

### Validate

```bash
hector validate --config config.yaml
```

Checks syntax and references.

### Dry Run

```bash
hector serve --config config.yaml --dry-run
```

Validates and shows what would be created without starting.

### Config Diff

```bash
hector diff --config config-v1.yaml --config config-v2.yaml
```

Shows configuration changes.

## Next Steps

- [Architecture Reference](architecture.md) - How configuration is loaded into runtime
- [Programmatic API Reference](programmatic.md) - Configuration types in Go
- [Security Guide](../guides/security.md) - Securing configuration and secrets

