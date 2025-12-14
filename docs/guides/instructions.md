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
If you have a file named `api_spec.json` loaded into the artifact system (e.g., via the `read_file` tool or initial context), you can inject it directly:

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
    "github.com/kadirpekel/hector/pkg/agent"
    "github.com/kadirpekel/hector/pkg/agent/llmagent"
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
