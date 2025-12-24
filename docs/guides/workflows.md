# Workflows

Hector provides workflow primitives that let you compose agents into complex pipelines. This guide covers the available patterns and how to combine them effectively.

## Overview

Workflow agents orchestrate other agents without their own LLM. They define *how* agents execute, not *what* they do.

| Type | Behavior | Use Case |
|------|----------|----------|
| `sequential` | Run agents in order | Pipelines, multi-step tasks |
| `parallel` | Run agents concurrently | Fan-out analysis, speed |
| `loop` | Run agents repeatedly | Iterative refinement |
| `runner` | Execute tools directly | Deterministic automation |
| `trigger` | Schedule-based execution | Cron jobs, automated reports |

## Sequential Workflows

Run sub-agents in strict order. Output from each step flows to the next.

```yaml
agents:
  content_pipeline:
    type: sequential
    sub_agents: [researcher, writer, editor]

  researcher:
    llm: default
    tools: [web_search]
    instruction: Research the topic thoroughly.

  writer:
    llm: default
    instruction: Write content based on the research.

  editor:
    llm: default
    instruction: Polish and improve the writing.
```

**Flow:**
```
User Input → researcher → writer → editor → Final Output
```

## Parallel Workflows

Run sub-agents concurrently and aggregate results.

```yaml
agents:
  multi_analysis:
    type: parallel
    sub_agents: [sentiment, topics, entities]

  sentiment:
    llm: default
    instruction: Analyze sentiment (positive/negative/neutral).

  topics:
    llm: default
    instruction: Extract main topics.

  entities:
    llm: default
    instruction: Identify named entities.
```

**Flow:**
```
                ┌─→ sentiment ─┐
User Input ─────┼─→ topics ────┼─→ Aggregated Output
                └─→ entities ──┘
```

## Loop Workflows

Run sub-agents repeatedly until completion or max iterations.

```yaml
agents:
  iterative_refiner:
    type: loop
    sub_agents: [critic, improver]
    max_iterations: 3

  critic:
    llm: default
    instruction: |
      Evaluate the content. If good enough, escalate.
      Otherwise, provide improvement suggestions.

  improver:
    llm: default
    instruction: Improve based on criticism.
```

**Termination conditions:**
- Sub-agent calls `escalate` (signals completion)
- `max_iterations` reached

## Runner Workflows

Execute tools in sequence without LLM reasoning. Fast and cost-effective.

```yaml
agents:
  data_fetcher:
    type: runner
    tools: [web_fetch]
```

**Key benefits:**
- No LLM calls = no cost for deterministic steps
- Milliseconds vs seconds latency
- Predictable, reproducible execution

## Schedule Triggers

Automate agents with cron-based scheduling. Agents run automatically without HTTP requests.

```yaml
agents:
  daily_report:
    type: sequential
    sub_agents: [data_fetcher, report_generator]
    trigger:
      type: schedule
      cron: "0 9 * * *"  # Daily at 9am
      timezone: "UTC"
      input: 'Generate the daily status report'
```

### Trigger Configuration

| Field | Description | Example |
|-------|-------------|---------|
| `type` | Trigger type | `schedule` |
| `cron` | Cron expression | `"0 9 * * *"` (daily 9am) |
| `timezone` | Timezone | `"UTC"`, `"America/New_York"` |
| `input` | Static input message | `"Run report"` |
| `enabled` | Enable/disable | `true` (default) |

### Common Cron Patterns

```yaml
# Every minute (testing)
cron: "* * * * *"

# Every hour
cron: "0 * * * *"

# Daily at 9am
cron: "0 9 * * *"

# Weekly on Monday at 8am
cron: "0 8 * * 1"

# First day of month at midnight
cron: "0 0 1 * *"
```

### Scheduled Workflow Example

```yaml
agents:
  # Scheduled monitoring pipeline
  health_check:
    type: sequential
    sub_agents: [monitor, alerter]
    trigger:
      type: schedule
      cron: "*/5 * * * *"  # Every 5 minutes
      input: 'Check system health'

  monitor:
    type: runner
    tools: [web_fetch]  # Check endpoints

  alerter:
    type: llm
    llm: default
    instruction: |
      Analyze the health check results.
      If issues detected, format an alert.
```

## Composing Workflows

The real power comes from combining primitives.

### Hybrid Pipeline (Runner + LLM)

Use runner for data prep, LLM for reasoning:

```yaml
agents:
  # Deterministic data fetching
  data_prep:
    type: runner
    tools: [web_fetch]

  # AI analysis
  analyzer:
    type: llm
    llm: default
    instruction: Analyze the data and provide insights.

  # Combined pipeline
  smart_pipeline:
    type: sequential
    sub_agents: [data_prep, analyzer]
```

**Cost comparison:**
```
Without runner: 3 LLM calls = $0.03
With runner:    1 LLM call  = $0.01
Savings: 66% per request
```

### Fan-out Analysis

Parallel analysis with sequential aggregation:

```yaml
agents:
  comprehensive_review:
    type: sequential
    sub_agents: [multi_review, synthesizer]

  multi_review:
    type: parallel
    sub_agents: [security_review, perf_review, style_review]

  security_review:
    llm: default
    instruction: Review for security issues.

  perf_review:
    llm: default
    instruction: Review for performance issues.

  style_review:
    llm: default
    instruction: Review for code style.

  synthesizer:
    llm: default
    instruction: Synthesize all reviews into a final report.
```

### Iterative Generation

Loop with parallel validation:

```yaml
agents:
  quality_content:
    type: loop
    sub_agents: [generator, validators]
    max_iterations: 3

  generator:
    llm: default
    instruction: Generate or improve content.

  validators:
    type: parallel
    sub_agents: [fact_checker, grammar_checker]

  fact_checker:
    llm: default
    instruction: Verify factual accuracy. Escalate if all correct.

  grammar_checker:
    llm: default
    instruction: Check grammar. Escalate if flawless.
```

## Real-World Examples

### Research Pipeline

```yaml
agents:
  research_assistant:
    type: sequential
    sub_agents: [searcher, fetcher, summarizer]

  searcher:
    type: llm
    llm: default
    tools: [web_search]
    instruction: Find relevant sources for the query.

  fetcher:
    type: runner
    tools: [web_fetch]

  summarizer:
    type: llm
    llm: default
    instruction: Synthesize findings into a comprehensive summary.
```

### Code Review Pipeline

```yaml
agents:
  code_reviewer:
    type: sequential
    sub_agents: [code_loader, reviewers, reporter]

  code_loader:
    type: runner
    tools: [grep_search]

  reviewers:
    type: parallel
    sub_agents: [security_bot, perf_bot, test_bot]

  security_bot:
    llm: default
    instruction: Check for security vulnerabilities.

  perf_bot:
    llm: default
    instruction: Identify performance concerns.

  test_bot:
    llm: default
    instruction: Suggest missing test cases.

  reporter:
    type: llm
    llm: default
    instruction: Compile findings into a review summary.
```

### Customer Support Triage

```yaml
agents:
  support_triage:
    type: sequential
    sub_agents: [classifier, router]

  classifier:
    type: llm
    llm: default
    structured_output:
      schema:
        type: object
        properties:
          category: { type: string, enum: [billing, technical, sales] }
          priority: { type: string, enum: [low, medium, high] }
        required: [category, priority]

  router:
    type: llm
    llm: default
    sub_agents: [billing_agent, tech_agent, sales_agent]
    instruction: Route to the appropriate specialist based on classification.
```

## Best Practices

### When to Use Each Primitive

| Pattern | Use When |
|---------|----------|
| Sequential | Steps depend on previous output |
| Parallel | Steps are independent |
| Loop | Quality requires iteration |
| Runner | No reasoning needed |

### Optimize for Cost

1. **Use runner for data ops** - Fetching, parsing, formatting
2. **Use parallel for analysis** - Faster than sequential
3. **Use cheaper models for loops** - Iterative steps add up
4. **Cache runner results** - Same input = same output

### Error Handling

```yaml
agents:
  robust_pipeline:
    type: sequential
    sub_agents: [primary, fallback]

  primary:
    llm: default
    instruction: |
      Process the request.
      If you cannot handle it, escalate with reason.

  fallback:
    llm: powerful  # More capable model
    instruction: Handle escalated requests.
```

### Keep Workflows Shallow

```yaml
# ✅ Good: Flat structure
pipeline:
  type: sequential
  sub_agents: [a, b, c, d]

# ❌ Avoid: Deep nesting
nested:
  type: sequential
  sub_agents:
    - step1:
        type: sequential
        sub_agents:
          - inner1:
              type: sequential
              sub_agents: [...]
```

## Summary

Hector's workflow primitives enable:

- **Sequential**: Step-by-step pipelines
- **Parallel**: Concurrent processing
- **Loop**: Iterative refinement
- **Runner**: LLM-free automation
- **Triggers**: Schedule-based automation

Combine them to build sophisticated AI workflows that are **fast**, **cost-effective**, and **maintainable**.
