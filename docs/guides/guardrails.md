# Guardrails

Guardrails provide safety controls for AI agents, protecting against prompt injection, PII exposure, and unauthorized tool usage.

## Overview

Guardrails validate and transform data at three points:

- **Input**: Before user messages reach the agent
- **Output**: Before agent responses reach the user  
- **Tool**: Before tool calls are executed

Define guardrails once, assign to agents by reference:

```yaml
guardrails:
  strict:
    input:
      injection:
        enabled: true
    output:
      pii:
        enabled: true

agents:
  assistant:
    guardrails: strict
```

## Input Guardrails

Input guardrails validate and sanitize user messages before they reach the LLM.

### Length Validation

Prevent excessively long or empty inputs:

```yaml
guardrails:
  production:
    input:
      length:
        enabled: true
        min_length: 1        # Require at least 1 character
        max_length: 100000   # Cap at 100K characters
        action: block        # Block if violated
        severity: medium
```

### Prompt Injection Detection

Detect and block attempts to manipulate agent behavior:

```yaml
guardrails:
  production:
    input:
      injection:
        enabled: true
        case_sensitive: false
        action: block
        severity: high
        # Optional: add custom patterns
        patterns:
          - "ignore previous instructions"
          - "you are now"
          - "system prompt"
```

Default patterns detect common injection techniques:

- Instruction overrides ("ignore all instructions")
- Role manipulation ("pretend you are")
- System prompt impersonation ("system:")
- Jailbreak attempts ("developer mode")
- Hidden instruction markers (base64, XML tags)

### Input Sanitization

Clean and normalize user input:

```yaml
guardrails:
  production:
    input:
      sanitizer:
        enabled: true
        trim_whitespace: true      # Remove leading/trailing spaces
        normalize_unicode: true    # Normalize Unicode to NFC form
        strip_html: true           # Remove HTML tags
        max_length: 50000          # Truncate if exceeded (0=no limit)
```

## Output Guardrails

Output guardrails filter and transform agent responses before they reach the user.

### PII Detection and Redaction

Automatically detect and redact personally identifiable information:

```yaml
guardrails:
  production:
    output:
      pii:
        enabled: true
        detect_email: true       # john@example.com → [EMAIL REDACTED]
        detect_phone: true       # 555-123-4567 → [PHONE REDACTED]
        detect_ssn: true         # 123-45-6789 → [SSN REDACTED]
        detect_credit_card: true # 4111...1111 → [CC REDACTED]
        redact_mode: mask        # mask, remove, or hash
        action: modify           # Modify output (vs block entirely)
        severity: high
```

Redaction modes:

- **mask**: Replace with `[TYPE REDACTED]` (default)
- **remove**: Remove PII entirely
- **hash**: Replace with SHA-256 hash (reversible lookup)

### Content Filtering

Block outputs containing harmful or prohibited content:

```yaml
guardrails:
  production:
    output:
      content:
        enabled: true
        blocked_keywords:
          - "password"
          - "secret_key"
          - "api_token"
        blocked_patterns:
          - "sk-[a-zA-Z0-9]{48}"    # OpenAI API keys
          - "ghp_[a-zA-Z0-9]{36}"   # GitHub tokens
        action: block
        severity: high
```

## Tool Guardrails

Tool guardrails control which tools can be invoked and with what arguments.

### Tool Authorization

Whitelist or blacklist specific tools:

```yaml
guardrails:
  strict:
    tool:
      authorization:
        enabled: true
        # Whitelist: only these tools are allowed
        allowed_tools:
          - search
          - grep_search
        # Blacklist: these tools are never allowed  
        blocked_tools:
          - bash
          - web_request
        action: block
        severity: high
```

Use glob patterns for wildcards:

```yaml
guardrails:
  limited:
    tool:
      authorization:
        enabled: true
        allowed_tools:
          - "read_*"    # Allow all read operations
          - "search"
        blocked_tools:
          - "write_*"   # Block all write operations
          - "delete_*"
```

## Chain Modes

Multiple guardrails run in sequence. Choose how violations are handled:

### Fail Fast (Default)

Stop at first violation:

```yaml
guardrails:
  production:
    input:
      chain_mode: fail_fast  # Stop at first violation
      injection:
        enabled: true
      length:
        enabled: true
```

### Collect All

Gather all violations before returning:

```yaml
guardrails:
  audit:
    input:
      chain_mode: collect_all  # Check all, report all violations
      injection:
        enabled: true
      length:
        enabled: true
      pattern:
        enabled: true
```

Use `collect_all` for comprehensive auditing or when you want complete violation reports.

## Assigning Guardrails to Agents

Define guardrails globally, reference by name in agents:

```yaml
guardrails:
  # Strict guardrails for public-facing agents
  strict:
    enabled: true
    input:
      injection:
        enabled: true
      sanitizer:
        enabled: true
    output:
      pii:
        enabled: true
        redact_mode: mask
    tool:
      authorization:
        enabled: true
        blocked_tools:
          - bash

  # Relaxed for internal tools
  relaxed:
    enabled: true
    input:
      sanitizer:
        enabled: true
        trim_whitespace: true

agents:
  # Public agent uses strict guardrails
  customer_support:
    llm: default
    guardrails: strict
    visibility: public

  # Internal tool uses relaxed guardrails  
  admin_assistant:
    llm: default
    guardrails: relaxed
    visibility: internal

  # No guardrails for private helper
  data_processor:
    llm: default
    visibility: private
    # No guardrails reference = no guardrails
```

## Actions and Severity

Each guardrail can specify an action and severity level:

**Actions:**

- **allow**: Permit the request (used for "all clear" results)
- **block**: Reject the request entirely
- **modify**: Transform the content (e.g., redact PII)
- **warn**: Log warning but allow request

**Severity:**

- **low**: Minor issue
- **medium**: Notable issue
- **high**: Serious issue
- **critical**: Severe issue requiring immediate attention

```yaml
guardrails:
  production:
    input:
      injection:
        enabled: true
        action: block      # Reject injection attempts
        severity: critical # Log as critical
      length:
        enabled: true
        action: warn       # Warn but allow
        severity: low
```

## Example Configurations

### Production Web Application

```yaml
guardrails:
  production:
    enabled: true
    input:
      chain_mode: fail_fast
      length:
        enabled: true
        max_length: 50000
        action: block
      injection:
        enabled: true
        action: block
        severity: critical
      sanitizer:
        enabled: true
        trim_whitespace: true
        strip_html: true
    output:
      chain_mode: fail_fast
      pii:
        enabled: true
        detect_email: true
        detect_phone: true
        detect_ssn: true
        detect_credit_card: true
        redact_mode: mask
      content:
        enabled: true
        blocked_keywords:
          - "internal_api_key"
          - "database_password"
    tool:
      chain_mode: fail_fast
      authorization:
        enabled: true
        blocked_tools:
          - bash
          - web_request

agents:
  assistant:
    llm: default
    guardrails: production
    tools: [search, text_editor, grep_search]
```

### Strict Privacy Example

```yaml
guardrails:
  privacy_strict:
    enabled: true
    input:
      injection:
        enabled: true
        action: block
    output:
      pii:
        enabled: true
        detect_email: true
        detect_phone: true
        detect_ssn: true
        redact_mode: remove  # Remove PII entirely
        action: modify
        severity: critical

agents:
  assistant:
    llm: default
    guardrails: privacy_strict
```

### Development Environment

```yaml
guardrails:
  development:
    enabled: true
    input:
      length:
        enabled: true
        max_length: 100000
      sanitizer:
        enabled: true
        trim_whitespace: true
    # No PII redaction in dev
    # No tool restrictions in dev

agents:
  dev_assistant:
    llm: default
    guardrails: development
    tools: [search, text_editor, grep_search, bash]
```

## Best Practices

### Layer Your Defenses

Use multiple guardrails together:

```yaml
guardrails:
  defense_in_depth:
    input:
      # Layer 1: Sanitize
      sanitizer:
        enabled: true
      # Layer 2: Validate length
      length:
        enabled: true
      # Layer 3: Detect injection
      injection:
        enabled: true
    output:
      # Layer 4: Redact PII
      pii:
        enabled: true
      # Layer 5: Filter content
      content:
        enabled: true
```

### Match Guardrails to Agent Purpose

```yaml
agents:
  # Customer-facing: maximum protection
  support_agent:
    guardrails: strict
    visibility: public

  # Internal: balanced protection
  research_agent:
    guardrails: standard
    visibility: internal

  # Backend: minimal overhead
  data_pipeline:
    guardrails: minimal
    visibility: private
```

### Monitor and Iterate

Enable logging to track guardrail violations:

```yaml
logger:
  level: info
  format: json  # Structured logs for analysis
```

Review logs to:

- Identify false positives (adjust patterns)
- Detect attack attempts
- Fine-tune severity levels



