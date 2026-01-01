# Instructions & Templating

Hector provides a powerful system for defining agent behavior through instructions (system prompts). This system supports dynamic content injection, scoped state access, and file embedding, allowing agents to adapt their behavior based on runtime context.

## Configuration

### Basic Instruction
The most common way to define an agent's behavior is the `instruction` field in the agent configuration.

```yaml
agents:
  researcher:
    name: "Web Researcher"
    instruction: "You are a detailed-oriented web researcher. Always cite your sources."
```

### Global Instruction
You can define a `global_instruction` on the root agent (or any agent acting as a root for sub-agents). This instruction is prepended to the agent's specific instruction, making it ideal for shared guidelines, voice, or safety rules.

```yaml
agents:
  root:
    global_instruction: "Responses must be in valid JSON format."
    agents:
      child1:
        instruction: "Analyze this data."
```

### Prompt Configuration
For more granular control, use the `prompt` configuration object. This allows you to separate the core role from specific guidance or override the system prompt entirely.

```yaml
agents:
  analyst:
    prompt:
      role: "Senior Data Analyst"
      guidance: "Focus on trends and outliers. Be concise."
      # system_prompt: "..." # (Optional) Completely overrides 'instruction'
```

Effectively, the final system prompt is constructed as: `Global Instruction + Role + Instruction + Guidance`.

## SKILL.md (Skill-based Configuration)

For a simpler, file-based approach to defining a single agent's behavior, Hector supports `SKILL.md`. This follows the [Agent Skills specification](https://agentskills.io) for portable skill definitions. If this file is present in your working directory, Hector will automatically use it to configure the default agent.

### Structure

`SKILL.md` consists of a YAML frontmatter block for configuration and a markdown body for the instruction.

```markdown
---
name: Data Analyst
description: Analyzes CSV files
allowed-tools: Read Edit Bash(git:*)
---
You are an expert data analyst.
Always visualize your findings.
```

### Frontmatter Reference

| Field | Description | Example |
| :--- | :--- | :--- |
| `name` | Agent name (display name) | `Data Analyst` |
| `description` | Brief description of the agent's purpose | `Analyzes data` |
| `allowed-tools` | Space-delimited tool hints (see below) | `Read Edit Bash(git:*)` |

### allowed-tools Mapping

Hector maps Agent Skills tool names to built-in tools:

| Agent Skills Name | Hector Tool | Notes |
| :--- | :--- | :--- |
| `Read`, `Edit`, `Write`, `Editor` | `text_editor` | All map to unified file tool |
| `Bash`, `Bash(*)`, `Bash(git:*)` | `bash` | Wildcard patterns are hints only |
| `Grep` | `grep_search` | (planned) |

> [!NOTE]
> The Agent Skills spec's `allowed-tools` field is marked as **experimental**. Hector treats these as tool selection hints, not granular permission restrictions. For fine-grained control (e.g., restricting `text_editor` to view-only, or `bash` to specific commands), use full `hector.yaml` configuration with `allowed_commands` and `require_approval`.

### Integration

When `hector serve` initializes:
1. It detects `SKILL.md`.
2. It parses the frontmatter to set the agent's name, description, and allowed tools.
3. It uses the markdown body as the agent's `instruction`.
4. It generates/updates `.hector/config.yaml` to reflect these settings.


## Dynamic Templating

Static text is often insufficient. Hector's instruction system allows you to inject dynamic values using placeholder syntax `{...}`. These placeholders are resolved at runtime just before the agent executes.

### Syntax Reference

| Placeholder | Scope | Description | Example |
| :--- | :--- | :--- | :--- |
| `{variable}` | Session | Value from the current session state. | `Hello {user_name}` |
| `{app:variable}` | App | Value from global application state. | `{app:environment}` |
| `{user:variable}` | User | Value from user-specific state. | `{user:preferences}` |
| `{temp:variable}` | Temp | Value from temporary (ephemeral) state. | `{temp:current_job}` |
| `{artifact.file}` | Artifact | Text content of a file in the artifact system. | `{artifact.code.go}` |
| `{var?}` | Optional | Returns empty string if missing (no error). | `Context: {extra_context?}` |

### Examples

**1. Personalization:**
```yaml
instruction: "You are helpful assistant for {user_name}. Current project: {app:project_name}."
```

**2. Injecting Code/Docs:**
If you have a file named `api_spec.json` loaded into the artifact system (e.g., via the `text_editor` tool or initial context), you can inject it directly:

```yaml
instruction: |
  You are an API expert.
  Here is the API specification you must follow:
  
  ```json
  {artifact.api_spec.json}
  ```
```

**3. Conditional Context:**
Use the `?` suffix to handle optional values gracefully without causing errors if the key is missing.

```yaml
instruction: |
  Analyze the following data.
  {additional_context?}
```

## Programmatic Control

For advanced use cases where YAML templating is not enough, you can generate instructions programmatically using Go. This is done by implementing an `InstructionProvider`.

### InstructionProvider

The `InstructionProvider` is a function that receives the full read-only context of the agent and returns a string.

```go
type InstructionProvider func(ctx agent.ReadonlyContext) (string, error)
```

**Usage:**

```go
import (
    "github.com/verikod/hector/pkg/agent"
    "github.com/verikod/hector/pkg/agent/llmagent"
)

func main() {
    myAgent, _ := llmagent.New(llmagent.Config{
        Name: "dynamic-agent",
        Model: myModel,
        
        // Dynamic instruction generation
        InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
            // Access state
            state := ctx.ReadonlyState()
            mode, _ := state.Get("mode")
            
            if mode == "expert" {
                return "You are an expert scientist. Use technical jargon.", nil
            }
            return "You are a helpful teacher. Explain simply.", nil
        },
    })
    
    // ...
}
```

### Precedence

When an `InstructionProvider` is set, it **takes precedence** over the static `Instruction` field in the configuration. However, `GlobalInstruction` is still applied if defined.

### GlobalInstructionProvider

Similar to `InstructionProvider`, you can dynamically generate global instructions:

```go
myAgent, _ := llmagent.New(llmagent.Config{
    Name: "dynamic-agent",
    Model: myModel,
    
    GlobalInstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
        // Dynamic global instructions for all sub-agents
        return "All responses must be under 500 words.", nil
    },
    
    InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
        return "You are a helpful assistant.", nil
    },
})
```



