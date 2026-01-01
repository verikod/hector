# Agents

Agents are the core of Hector. Each agent has an LLM, tools, and instructions that define its behavior.

## Basic Agent Configuration

### Minimal Agent

```yaml
version: "2"

llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}

agents:
  assistant:
    llm: default
```

This creates an agent at `http://localhost:8080/agents/assistant`.

### Complete Agent

```yaml
agents:
  assistant:
    name: Customer Support Assistant
    description: Helps customers with product questions
    llm: default
    tools: [search, text_editor]
    streaming: true
    visibility: public
    instruction: |
      You are a customer support assistant for ACME Corp.
      Use the search tool to find relevant documentation.
      Be helpful, concise, and professional.
```

## Agent Properties

### Basic Properties

**name**: Display name shown in UI and agent card

```yaml
agents:
  assistant:
    name: Research Assistant  # Human-readable name
```

**description**: Explains the agent's purpose

```yaml
agents:
  assistant:
    description: Researches topics and synthesizes information
```

**llm**: References an LLM configuration

```yaml
llms:
  fast:
    provider: openai
    model: gpt-4o-mini

  powerful:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}

agents:
  assistant:
    llm: powerful
```

**tools**: List of available tools

```yaml
agents:
  assistant:
    tools:
      - search        # Document search
      - text_editor   # View and edit files
      - bash          # Execute shell commands
```

> [!NOTE]
> **Implicit Tool Assignment**: When `tools:` is omitted, the agent gets access to **all enabled tools**. Use `tools: []` (empty list) for no tools.

### Instructions

**instruction**: System prompt that defines behavior

```yaml
agents:
  assistant:
    instruction: |
      You are a helpful assistant.

      When searching documents:
      - Use the search tool first
      - Cite sources in your response

      When writing code:
      - Include comments
      - Follow best practices
```

Supports template placeholders:

```yaml
agents:
  assistant:
    instruction: |
      You are {role?}.
      User: {user:name}
      Project: {app:project_name}
```

**global_instruction**: Applies to all agents in a multi-agent system

```yaml
agents:
  coordinator:
    global_instruction: |
      All agents must:
      - Cite sources
      - Use UTC timestamps
      - Format code blocks with language tags
```

### Prompt Configuration

For advanced prompt control:

```yaml
agents:
  assistant:
    prompt:
      role: Senior Software Engineer
      guidance: |
        Focus on:
        - Code quality
        - Performance
        - Security best practices
```

## Streaming

Enable token-by-token streaming:

```yaml
agents:
  assistant:
    streaming: true  # Default: false
```

With streaming enabled:

- Responses stream as they generate
- Users see output immediately
- Lower perceived latency

## Agent Visibility

Control discoverability and access:

```yaml
agents:
  # Public agent (default)
  public-assistant:
    visibility: public  # Visible in discovery, HTTP accessible

  # Internal agent
  internal-analyst:
    visibility: internal  # Only visible when authenticated

  # Private agent
  private-helper:
    visibility: private  # Not exposed via HTTP, internal use only
```

Visibility levels:

- **public** (default): Visible in agent discovery, accessible via HTTP
- **internal**: Visible only to authenticated users, requires authentication
- **private**: Hidden from discovery, not accessible via HTTP (for sub-agents/tools)

## Automations

Agents can be triggered automatically and send notifications to external services.

**Schedule Trigger** - Run on a cron schedule:

```yaml
agents:
  daily-report:
    trigger:
      type: schedule
      cron: "0 9 * * *"  # Daily at 9am
```

**Webhook Trigger** - Run when receiving HTTP requests:

```yaml
agents:
  order-processor:
    trigger:
      type: webhook
      secret: ${WEBHOOK_SECRET}
```

**Notifications** - Notify external services on events:

```yaml
agents:
  order-processor:
    notifications:
      - id: slack
        url: ${SLACK_WEBHOOK_URL}
        events: [task.completed, task.failed]
```

See the [Automations Guide](automations.md) for complete documentation on triggers, webhook configuration, and notification setup.

## Input and Output Modes

Specify supported MIME types:

```yaml
agents:
  analyst:
    input_modes:
      - text/plain
      - application/json
      - image/png
    output_modes:
      - text/plain
      - application/json
```

Common modes:

- `text/plain` - Plain text
- `application/json` - JSON data
- `image/png`, `image/jpeg` - Images
- `audio/mpeg` - Audio

## Context Management

Manage conversation history to fit within LLM context limits:

### Buffer Window Strategy

Keep last N messages:

```yaml
agents:
  assistant:
    context:
      strategy: buffer_window
      window_size: 20  # Keep last 20 messages
```

### Token Window Strategy

Keep messages within token budget:

```yaml
agents:
  assistant:
    context:
      strategy: token_window
      budget: 8000         # Max tokens
      preserve_recent: 5   # Always keep last 5 messages
```

### Summary Buffer Strategy

Summarize old messages when exceeding budget:

```yaml
agents:
  assistant:
    context:
      strategy: summary_buffer
      budget: 8000           # Token budget
      threshold: 0.85        # Summarize at 85% usage
      target: 0.7            # Reduce to 70% after summarizing
      summarizer_llm: fast   # Use cheaper model for summarization
```

### No Strategy (Default)

Include all history (no filtering):

```yaml
agents:
  assistant:
    context:
      strategy: none  # Default: include all messages
```

## RAG Integration

### Auto-Injected Context

Automatically inject relevant documents into prompts:

```yaml
agents:
  assistant:
    document_stores: [docs, codebase]  # Access specific stores
    include_context: true               # Auto-inject relevant docs
    include_context_limit: 5            # Max 5 documents
    include_context_max_length: 500     # Max 500 chars per doc
```

When `include_context: true`:

- User messages are used to search document stores
- Top K relevant documents are retrieved
- Documents are injected into the system prompt
- Agent receives context automatically

### Manual Search

Let the agent decide when to search:

```yaml
agents:
  assistant:
    document_stores: [docs, codebase]
    tools: [search]  # Agent calls search explicitly
```

Agent uses `search` tool when needed:

- More control over when to search
- Can search multiple times
- Can refine search queries

### Scoped Access

Limit which document stores an agent can access:

```yaml
agents:
  # Access specific stores
  researcher:
    document_stores: [docs, codebase]

  # No RAG access
  restricted:
    document_stores: []

  # Access all stores (default)
  admin:
    # Omit document_stores for access to all
```

> [!NOTE]
> **Implicit Document Store Assignment**: When `document_stores:` is omitted, the agent gets access to **all document stores** and receives a `search` tool automatically. Use `document_stores: []` for no RAG access.

## Multi-Agent Patterns

### Sub-Agents (Pattern 1: Transfer Control)

Create specialized agents that handle specific tasks:

```yaml
agents:
  coordinator:
    llm: default
    sub_agents: [researcher, writer]
    instruction: |
      Route user requests to specialized agents:
      - Use transfer_to_researcher for research tasks
      - Use transfer_to_writer for content creation

  researcher:
    llm: default
    tools: [search]
    instruction: |
      Research topics thoroughly.
      Return findings to the coordinator.

  writer:
    llm: default
    tools: [text_editor]
    instruction: |
      Write high-quality content.
      Save to files when requested.
```

With `sub_agents`, transfer tools are auto-created:

- `transfer_to_researcher`
- `transfer_to_writer`

When called, control transfers to that agent.

### Agent Tools (Pattern 2: Callable Tools)

Use agents as tools that return results:

```yaml
agents:
  analyst:
    llm: default
    agent_tools: [sentiment_analyzer, data_processor]
    instruction: |
      Analyze user input using available tools:
      - sentiment_analyzer: Determines sentiment
      - data_processor: Processes raw data

  sentiment_analyzer:
    llm: default
    instruction: |
      Analyze sentiment and return: positive, negative, or neutral.

  data_processor:
    llm: default
    instruction: |
      Process and format data into structured output.
```

With `agent_tools`, tools are created for each agent:

- Agent maintains control
- Sub-agent processes input and returns result
- Result is structured data

### Choosing Between Patterns

| Aspect | `sub_agents` (Transfer) | `agent_tools` (Callable) |
|--------|------------------------|--------------------------|
| **Control Flow** | Transfers to sub-agent | Parent keeps control |
| **Response** | Sub-agent responds directly | Result returned to parent |
| **Use Case** | Routing to specialists | Parallel processing, analysis |
| **Conversation** | Sub-agent continues conversation | Parent synthesizes results |

**When to use `sub_agents`:**

- User needs to interact with a specialist directly
- Task requires deep, focused conversation
- Routing based on user intent (support → sales → tech)

**When to use `agent_tools`:**

- Need results from multiple agents
- Parent must synthesize/combine outputs
- One-shot analysis or processing tasks

**Example decision:**

```yaml
# User asks "analyze my data and write a report"

# ❌ sub_agents: User would talk to analyst, then writer separately
# ✅ agent_tools: Coordinator calls both, synthesizes, returns unified report

agents:
  coordinator:
    agent_tools: [analyst, writer]  # Keeps control, combines results
    instruction: |
      1. Call analyst to analyze data
      2. Call writer to draft report
      3. Combine and return final report
```

### Error Handling

**Referenced agent doesn't exist:**

```
Error: agent "researcher" not found in sub_agents
```

Solution: Ensure all agents in `sub_agents` or `agent_tools` are defined in the config.

**Circular references:**

```
Error: circular agent reference detected: a → b → a
```

Solution: Agents cannot reference each other in a cycle. Use a coordinator pattern instead.

## Structured Output

Force agents to return JSON matching a schema:

```yaml
agents:
  classifier:
    llm: default
    structured_output:
      schema:
        type: object
        properties:
          category:
            type: string
            enum: [technical, sales, support]
          priority:
            type: string
            enum: [low, medium, high]
          confidence:
            type: number
            minimum: 0
            maximum: 1
        required: [category, priority, confidence]
```

Agent responses will match the schema:

```json
{
  "category": "technical",
  "priority": "high",
  "confidence": 0.92
}
```

## Skills (A2A Discovery)

Advertise agent capabilities for federation:

```yaml
agents:
  specialist:
    skills:
      - id: data-analysis
        name: Data Analysis
        description: Analyzes datasets and generates insights
        tags: [analytics, statistics]
        examples:
          - "Analyze this sales data"
          - "What trends do you see?"

      - id: visualization
        name: Data Visualization
        description: Creates charts and graphs
        tags: [charts, graphs]
        examples:
          - "Create a bar chart"
          - "Visualize this data"
```

Skills appear in agent card for A2A discovery.

## Remote Agents

Connect to external A2A agents:

```yaml
agents:
  external-specialist:
    type: remote
    url: https://external-service.com
    headers:
      Authorization: Bearer ${API_TOKEN}
    timeout: 30s
```

Auto-fetch agent card:

```yaml
agents:
  external-specialist:
    type: remote
    url: https://external-service.com
    agent_card_url: https://external-service.com/.well-known/agent.json
```

Or use local agent card:

```yaml
agents:
  external-specialist:
    type: remote
    url: https://external-service.com
    agent_card_file: ./cards/specialist.json
```

## Workflow Agents

### Sequential Agents

Run sub-agents in sequence:

```yaml
agents:
  pipeline:
    type: sequential
    sub_agents: [step1, step2, step3]

  step1:
    instruction: Process input data

  step2:
    instruction: Transform processed data

  step3:
    instruction: Generate final output
```

### Parallel Agents

Run sub-agents in parallel:

```yaml
agents:
  parallel-processor:
    type: parallel
    sub_agents: [analyzer1, analyzer2, analyzer3]

  analyzer1:
    instruction: Analyze from perspective 1

  analyzer2:
    instruction: Analyze from perspective 2

  analyzer3:
    instruction: Analyze from perspective 3
```

Results are aggregated and returned.

### Loop Agents

Run sub-agents repeatedly:

```yaml
agents:
  iterative-refiner:
    type: loop
    sub_agents: [refiner]
    max_iterations: 5

  refiner:
    instruction: |
      Refine the content.
      Escalate when satisfactory.
```

Loops until:

- Sub-agent escalates (signals completion)
- `max_iterations` reached

### Runner Agents

Execute tools in sequence without LLM reasoning:

```yaml
agents:
  data_fetcher:
    type: runner
    description: "Fetches and parses web content"
    tools: [web_fetch]
```

Runner agents:

- Execute tools deterministically in order
- Each tool's output becomes the next tool's input
- No LLM calls = cost-efficient and fast
- Composable with other workflow agents

**Hybrid Pipeline Example:**

```yaml
agents:
  # Step 1: Fetch data (no LLM needed)
  fetcher:
    type: runner
    tools: [web_fetch]

  # Step 2: Analyze data (LLM reasoning)
  analyzer:
    type: llm
    llm: default
    instruction: Analyze the content and provide insights

  # Combined pipeline
  research_pipeline:
    type: sequential
    sub_agents: [fetcher, analyzer]
```

Use cases:

- **ETL pipelines**: fetch → transform → save
- **Data preprocessing**: fetch data before LLM analysis
- **Automation**: deterministic tool chains within AI workflows
- **Cost optimization**: skip LLM for predictable operations


## Examples

### Research Assistant

```yaml
agents:
  researcher:
    name: Research Assistant
    llm: default
    tools: [search, text_editor]
    document_stores: [research-docs]
    streaming: true
    instruction: |
      You are a research assistant.

      Process:
      1. Search for relevant documents
      2. Synthesize information
      3. Cite sources
      4. Save findings to files when requested
```

### Customer Support

```yaml
agents:
  support:
    name: Customer Support
    llm: default
    tools: [search]
    document_stores: [kb]
    include_context: true
    include_context_limit: 3
    context:
      strategy: summary_buffer
      budget: 4000
    instruction: |
      You are a customer support agent.
      Help customers using the knowledge base.
      Be friendly and professional.
```

### Multi-Agent Coordinator

```yaml
agents:
  coordinator:
    name: Task Coordinator
    llm: default
    sub_agents: [researcher, analyst, writer]
    instruction: |
      Coordinate tasks among specialists:
      - Researcher: Gathers information
      - Analyst: Analyzes data
      - Writer: Creates content

      Route tasks to the appropriate specialist.

  researcher:
    llm: default
    tools: [search]
    visibility: private
    instruction: Research topics and gather information

  analyst:
    llm: default
    agent_tools: [data-processor]
    visibility: private
    instruction: Analyze data and provide insights

  writer:
    llm: default
    tools: [text_editor]
    visibility: private
    instruction: Create polished content
```

## Guardrails

Assign safety controls to agents to protect against prompt injection, PII exposure, and unauthorized tool usage.

### Basic Usage

Reference a named guardrails configuration:

```yaml
guardrails:
  strict:
    input:
      injection:
        enabled: true
    output:
      pii:
        enabled: true
        redact_mode: mask

agents:
  assistant:
    llm: default
    guardrails: strict  # Reference to guardrails config
```

### Different Guardrails per Agent

Use different protection levels for different agents:

```yaml
guardrails:
  strict:
    input:
      injection:
        enabled: true
      sanitizer:
        enabled: true
    output:
      pii:
        enabled: true

  relaxed:
    input:
      sanitizer:
        enabled: true

agents:
  # Public-facing: maximum protection
  customer_support:
    guardrails: strict
    visibility: public

  # Internal: less restrictive
  admin_tool:
    guardrails: relaxed
    visibility: internal

  # Private helper: no guardrails
  data_processor:
    visibility: private
    # No guardrails reference
```

See the [Guardrails Guide](guardrails.md) for complete configuration options.

## Best Practices

### Instruction Design

Be specific and actionable:

```yaml
# ✅ Good
instruction: |
  You are a code reviewer.

  Review process:
  1. Check for security issues
  2. Verify error handling
  3. Assess code clarity
  4. Suggest improvements

  Format responses as:
  - Issues: [list]
  - Suggestions: [list]

# ❌ Bad
instruction: Review code
```

### Tool Selection

Provide necessary tools only:

```yaml
# ✅ Good - Scoped tools
agents:
  researcher:
    tools: [search, grep_search]

# ❌ Bad - Unnecessary tools
agents:
  researcher:
    tools: [search, text_editor, bash]
```

### Context Management

Choose appropriate strategy for your use case:

```yaml
# Short conversations: buffer_window
agents:
  chatbot:
    context:
      strategy: buffer_window
      window_size: 20

# Long conversations: summary_buffer
agents:
  assistant:
    context:
      strategy: summary_buffer
      budget: 8000
```

### Visibility

Use visibility for security:

```yaml
agents:
  # Public-facing
  customer-support:
    visibility: public

  # Internal tools
  admin-assistant:
    visibility: internal

  # Sub-agents/helpers
  data-processor:
    visibility: private
```


