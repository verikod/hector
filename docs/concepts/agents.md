# Agents

Agents are autonomous entities that process user requests, invoke tools, and delegate to sub-agents.

## Agent Types

Hector supports multiple agent types for different use cases.

### LLM Agent

Powered by language models with tool calling and reasoning.

```go
// pkg/agent/llmagent
type Config struct {
    Name        string
    Model       model.LLM
    Instruction string
    Tools       []tool.Tool
    SubAgents   []agent.Agent
}
```

**Capabilities:**

- Natural language understanding
- Tool/function calling
- Chain-of-thought reasoning
- Sub-agent delegation
- Memory and context management

**Example:**

```yaml
agents:
  assistant:
    llm: gpt-4o
    instruction: You are a helpful assistant
    tools: [search, calculate]
    sub_agents: [specialist]
```

### Remote Agent

Proxy to external A2A-compliant agents.

```go
// pkg/agent/remoteagent
type Config struct {
    Name        string
    Description string
    URL         string  // Base URL of remote A2A server
}
```

**Usage:**

```yaml
agents:
  remote_helper:
    type: remote
    url: http://remote-server:8080
    description: Remote specialist agent
```

Fetches agent card from `/.well-known/agent-card.json` and forwards requests.

### Custom Agent

User-defined agent with custom execution logic.

```go
agent, err := agent.New(agent.Config{
    Name: "custom",
    Run: func(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
        return func(yield func(*agent.Event, error) bool) {
            // Custom logic
            event := agent.NewEvent(ctx.InvocationID())
            event.Message = &a2a.Message{
                Role: a2a.MessageRoleAgent,
                Parts: []a2a.Part{
                    a2a.TextPart{Text: "Custom response"},
                },
            }
            yield(event, nil)
        }
    },
})
```

## Agent Lifecycle

### Invocation Flow

```
User Message → Runner → Agent Selection → Agent Execution → Response

┌─────────────────────── Invocation ──────────────────────────┐
│                                                              │
│  ┌──────────── Agent Call 1 ────────────┐                   │
│  │ ┌─ Step 1 ─┐ ┌─ Step 2 ─┐           │                   │
│  │ │ LLM Call │ │Tool Exec │           │                   │
│  │ └──────────┘ └──────────┘           │                   │
│  └────────────────────────────────────┘                    │
│                                                              │
│  ┌────── Agent Call 2 ──────┐                              │
│  │ (Transfer to sub-agent)  │                              │
│  └──────────────────────────┘                              │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

**Phases:**
1. **Selection**: Runner selects agent based on delegation or initial routing
2. **Before Callbacks**: Pre-execution hooks
3. **Execution**: Agent runs (LLM calls, tool execution, etc.)
4. **After Callbacks**: Post-execution hooks
5. **Completion**: Event yielded, control returns to runner

### Agent States

Agents are stateless. State is managed in:

- **Session State**: Key-value store persisted across invocations
- **Events**: Full conversation history
- **Working Memory**: Filtered context window
- **Execution State**: Checkpointed for recovery (HITL)

## Context Management

### Invocation Context

Provides access to invocation data and services:

```go
type InvocationContext interface {
    // Identity
    InvocationID() string
    AgentName() string

    // Input
    UserContent() *Content

    // State
    State() State
    ReadonlyState() ReadonlyState

    // Services
    Session() Session
    Memory() Memory
    Artifacts() Artifacts

    // Control
    EndInvocation()
    Ended() bool
}
```

**Usage in Custom Agent:**

```go
Run: func(ctx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
    return func(yield func(*agent.Event, error) bool) {
        // Read state
        value, _ := ctx.State().Get("counter")

        // Update state
        ctx.State().Set("counter", value.(int) + 1)

        // Access user input
        user := ctx.UserContent()

        // End invocation early
        if someCondition {
            ctx.EndInvocation()
        }
    }
}
```

### Readonly Context

Safe to pass to tools and callbacks (no mutation):

```go
type ReadonlyContext interface {
    InvocationID() string
    AgentName() string
    UserContent() *Content
    ReadonlyState() ReadonlyState
    UserID() string
    SessionID() string
}
```

### Callback Context

Allows state modification in callbacks:

```go
type CallbackContext interface {
    ReadonlyContext
    State() State
    Artifacts() Artifacts
}
```

## Event System

Agents communicate via events.

### Event Structure

```go
type Event struct {
    ID           string
    InvocationID string
    Author       string      // Agent name
    Branch       string      // Agent hierarchy path
    Message      *a2a.Message
    Partial      bool        // Streaming chunk
    Actions      Actions
}
```

**Event Types:**

- **Message Event**: Contains agent response
- **Action Event**: Tool calls, transfers
- **Partial Event**: Streaming text chunk
- **Control Event**: End invocation, error

### Example Event

```go
event := agent.NewEvent(ctx.InvocationID())
event.Author = "assistant"
event.Branch = "/assistant"
event.Message = &a2a.Message{
    Role: a2a.MessageRoleAgent,
    Parts: []a2a.Part{
        a2a.TextPart{Text: "Hello!"},
    },
}
```

### Actions

Events can contain actions:

```go
type Actions struct {
    ToolCalls  []ToolCall      // Tool invocations
    Transfer   *Transfer       // Delegation to another agent
    StateDelta map[string]any  // State changes
}
```

**Tool Call Example:**

```json
{
  "tool_calls": [
    {
      "id": "call_123",
      "name": "search",
      "arguments": {"query": "weather"}
    }
  ]
}
```

**Transfer Example:**

```json
{
  "transfer": {
    "target": "specialist",
    "reason": "Requires domain expertise"
  }
}
```

## Multi-Agent Patterns

### Delegation (Sub-Agents)

Parent agent delegates to sub-agent via `transfer` tool:

```yaml
agents:
  coordinator:
    llm: gpt-4o
    instruction: |
      You coordinate tasks.
      Use transfer_to_specialist for complex queries.
    sub_agents:
      - specialist

  specialist:
    llm: claude-sonnet-4
    instruction: You are a domain specialist
```

**Flow:**

```
User → Coordinator
         ↓ transfer_to_specialist
       Specialist → Response
```

Coordinator can transfer control to specialist. Specialist continues the conversation.

### Agent Tools

Use agents as tools (returns result to parent):

```yaml
agents:
  main:
    llm: gpt-4o
    tools:
      - agent_call_researcher  # Agent as tool

  researcher:
    llm: claude-sonnet-4
    instruction: Research topics and return findings
```

**Flow:**

```
User → Main Agent
         ↓ call agent_tool
       Researcher (executes, returns)
         ↓ result
       Main Agent → Final Response
```

Researcher executes as a tool and returns result. Main agent continues processing.

### Sequential Workflow

Execute agents in sequence:

```yaml
agents:
  workflow:
    type: workflow
    mode: sequential
    steps:
      - agent: researcher
      - agent: writer
      - agent: editor
```

**Flow:**

```
User Input
  ↓
Researcher (research phase)
  ↓
Writer (write draft)
  ↓
Editor (review and finalize)
  ↓
Final Output
```

### Parallel Workflow

Execute agents concurrently:

```yaml
agents:
  workflow:
    type: workflow
    mode: parallel
    agents:
      - fact_checker
      - sentiment_analyzer
      - summarizer
```

All agents execute simultaneously. Results aggregated.

## Reasoning Loops

LLM agents use chain-of-thought reasoning with semantic termination.

### Default Behavior

```go
type ReasoningConfig struct {
    MaxIterations int  // Safety limit: 100 (default)
}
```

**Termination Conditions:**

- No tool calls in response
- Explicit final answer
- Max iterations reached (safety)

### Example Loop

```
Iteration 1:
  User: "What's the weather in Paris?"
  LLM: "I need to search for Paris weather" → [tool: search]
  Tool: "Sunny, 22°C"

Iteration 2:
  LLM: "The weather in Paris is sunny with 22°C" → (no tools)
  ✓ Loop terminates (no tool calls)
```

### Custom Termination

Add exit tool:

```yaml
agents:
  assistant:
    llm: gpt-4o
    reasoning:
      enable_exit_tool: true
```

Agent can call `exit_loop` to terminate explicitly.

### Escalation

Enable escalate tool for parent delegation:

```yaml
agents:
  specialist:
    llm: claude-sonnet-4
    reasoning:
      enable_escalate_tool: true
```

Agent can call `escalate` to delegate back to parent.

## Callbacks

Callbacks customize agent behavior at specific points.

### Before Agent

Runs before agent starts:

```go
BeforeAgentCallback func(CallbackContext) (*a2a.Message, error)
```

**Use Cases:**

- Validate input
- Inject context
- Skip execution

**Example:**

```go
beforeAgent := func(ctx agent.CallbackContext) (*a2a.Message, error) {
    // Check user permission
    if !authorized {
        return &a2a.Message{
            Role: a2a.MessageRoleAgent,
            Parts: []a2a.Part{
                a2a.TextPart{Text: "Unauthorized"},
            },
        }, nil  // Skip agent execution
    }
    return nil, nil  // Continue
}
```

### After Agent

Runs after agent completes:

```go
AfterAgentCallback func(CallbackContext) (*a2a.Message, error)
```

**Use Cases:**

- Post-process response
- Log results
- Update state

### Before Model

Runs before each LLM call:

```go
BeforeModelCallback func(ctx CallbackContext, req *model.Request) (*model.Response, error)
```

**Use Cases:**

- Modify prompt
- Cache responses
- Skip LLM call

### After Model

Runs after each LLM call:

```go
AfterModelCallback func(ctx CallbackContext, resp *model.Response, err error) (*model.Response, error)
```

**Use Cases:**

- Filter responses
- Error handling
- Logging

### Before Tool

Runs before tool execution:

```go
BeforeToolCallback func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error)
```

**Use Cases:**

- Validate arguments
- Mock tool execution
- Rate limiting

### After Tool

Runs after tool execution:

```go
AfterToolCallback func(ctx tool.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error)
```

**Use Cases:**

- Transform results
- Error recovery
- Metrics

## Tool Integration

Agents access tools during reasoning loops.

### Built-in Tools

```yaml
agents:
  assistant:
    llm: gpt-4o
    tools:
      - read_file
      - write_file
      - execute_command
      - web_request
```

### Custom Tools

```go
type MyTool struct{}

func (t *MyTool) Name() string { return "my_tool" }
func (t *MyTool) Description() string { return "Does something" }
func (t *MyTool) Schema() map[string]any { return schema }
func (t *MyTool) Execute(ctx tool.Context, args map[string]any) (map[string]any, error) {
    // Custom logic
    return result, nil
}
```

Register with agent:

```go
agent, _ := llmagent.New(llmagent.Config{
    Name: "assistant",
    Tools: []tool.Tool{&MyTool{}},
})
```

### Control Tools

Special tools for agent control flow:

**transfer_to_{agent}**:

- Delegates to sub-agent
- Transfers conversation control
- Automatically created for each sub-agent

**exit_loop**:

- Terminates reasoning loop explicitly
- Enabled via `reasoning.enable_exit_tool`

**escalate**:

- Returns control to parent agent
- Enabled via `reasoning.enable_escalate_tool`

## Working Memory

Manages conversation history to fit LLM context limits.

### Strategies

**All (No Filtering)**:

```yaml
agents:
  assistant:
    working_memory: all
```

Include entire conversation history.

**Recent (Sliding Window)**:

```yaml
agents:
  assistant:
    working_memory:
      type: recent
      max_messages: 20
```

Keep only last N messages.

**Summarize (Compress Old)**:

```yaml
agents:
  assistant:
    working_memory:
      type: summarize
      max_messages: 50
      summary_threshold: 30
```

Summarize messages beyond threshold.

## Context Providers (RAG)

Inject relevant context into conversations.

### Configuration

```yaml
agents:
  assistant:
    llm: gpt-4o
    document_stores:
      - knowledge_base
```

### Implementation

```go
type ContextProvider func(ctx agent.ReadonlyContext, query string) (string, error)
```

**Flow:**

```
User Query
  ↓
Context Provider (search, retrieve)
  ↓
Relevant Context
  ↓
Injected into System Message
  ↓
LLM Call
```

## Checkpointing

Agents can save execution state for recovery.

### Checkpointable Interface

```go
type Checkpointable interface {
    Agent
    CaptureCheckpointState() (map[string]any, error)
    RestoreCheckpointState(state map[string]any) error
}
```

### Use Cases

**Long-Running Tasks:**

- Save state periodically
- Resume on crash/restart

**HITL Workflows:**

- Pause for approval
- Resume after input

### Example

```go
// Agent implements Checkpointable
func (a *myAgent) CaptureCheckpointState() (map[string]any, error) {
    return map[string]any{
        "iteration": a.iteration,
        "partial_result": a.result,
    }, nil
}

func (a *myAgent) RestoreCheckpointState(state map[string]any) error {
    a.iteration = state["iteration"].(int)
    a.result = state["partial_result"].(string)
    return nil
}
```

## Agent Hierarchy

Agents form trees with parent-child relationships.

### Navigation

```go
// Find agent by name
specialist := agent.FindAgent(root, "specialist")

// Get path to agent
path := agent.FindAgentPath(root, "specialist")
// path = ["team_a", "specialist"]

// Walk all agents
agent.WalkAgents(root, func(ag agent.Agent, depth int) bool {
    fmt.Printf("%s%s\n", strings.Repeat("  ", depth), ag.Name())
    return true  // continue
})
```

### Branch Tracking

Events include branch path:

```go
event.Branch = "/coordinator/specialist"
```

Tracks which agent in hierarchy created the event.

## Agent Visibility

Controls agent access via A2A protocol.

```yaml
agents:
  public_agent:
    visibility: public      # Accessible to all

  internal_agent:
    visibility: internal    # Requires authentication

  private_agent:
    visibility: private     # Only callable by other agents
```

**Levels:**

- `public`: HTTP accessible, visible in discovery
- `internal`: HTTP accessible with auth, visible when authenticated
- `private`: Not exposed via HTTP, internal use only

## Structured Output

Enforce output schemas:

```yaml
agents:
  data_extractor:
    llm: gpt-4o
    output_schema:
      type: object
      properties:
        name:
          type: string
        age:
          type: integer
      required: [name, age]
```

LLM must return JSON matching schema.

## Best Practices

### Single Responsibility

Each agent should have a clear, focused purpose:

```yaml
# ✅ Good - Focused agents
agents:
  researcher:
    instruction: Research topics and gather facts

  writer:
    instruction: Write articles from research

# ❌ Bad - Doing too much
agents:
  do_everything:
    instruction: Research, write, edit, format, publish
```

### Proper Delegation

Use sub-agents for specialization:

```yaml
agents:
  coordinator:
    sub_agents:
      - legal_expert
      - financial_expert
      - technical_expert
```

### Tool Minimalism

Only provide necessary tools:

```yaml
# ✅ Good - Minimal tools
agents:
  reader:
    tools: [read_file, search]

# ❌ Bad - Excessive permissions
agents:
  reader:
    tools: [read_file, write_file, execute_command, web_request]
```

### Context Window Management

Use working memory for long conversations:

```yaml
agents:
  chatbot:
    working_memory:
      type: recent
      max_messages: 50
```

## Next Steps

- [Runtime](runtime.md) - Runtime execution model
- [Tools](tools.md) - Tool system details
- [Memory](memory.md) - Memory and state management
