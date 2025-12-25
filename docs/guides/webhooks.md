# Webhooks & Notifications

Hector supports bidirectional integration with external services through **inbound webhooks** and **outbound notifications**.

- **Inbound webhooks** allow external services (Zapier, n8n, GitHub, Shopify) to trigger agents
- **Outbound notifications** let agents notify external services when tasks complete or fail

## Inbound Webhooks

Configure agents to be triggered by HTTP requests.

### Basic Webhook Trigger

```yaml
agents:
  order-processor:
    llm: default
    instruction: Process incoming orders
    trigger:
      type: webhook
```

This registers the agent at `POST /webhooks/order-processor`.

### Complete Webhook Configuration

```yaml
agents:
  github-handler:
    llm: default
    instruction: |
      Process GitHub events. Analyze the payload and respond appropriately.
    trigger:
      type: webhook
      path: /webhooks/github          # Custom path (default: /webhooks/{agent-name})
      methods: [POST]                  # Allowed HTTP methods (default: POST)
      secret: ${GITHUB_WEBHOOK_SECRET} # HMAC secret for signature verification
      signature_header: X-Hub-Signature-256  # Header containing signature
      
      webhook_input:
        template: |
          GitHub event received:
          Repository: {{ .body.repository.full_name }}
          Action: {{ .body.action }}
          Sender: {{ .body.sender.login }}
          
          Payload: {{ toJson .body }}
      
      response:
        mode: async                   # sync, async, or callback
        timeout: 30s                  # Timeout for sync mode
```

### Webhook Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | - | Set to `webhook` for webhook triggers |
| `path` | string | `/webhooks/{agent-name}` | Custom URL path |
| `methods` | []string | `["POST"]` | Allowed HTTP methods |
| `secret` | string | - | HMAC secret for signature verification |
| `signature_header` | string | `X-Webhook-Signature` | HTTP header containing HMAC signature |
| `input` | string | - | Static input (fallback if no template) |
| `enabled` | bool | `true` | Enable/disable the trigger |

### Signature Verification

When `secret` is configured, Hector validates incoming requests using HMAC-SHA256:

```yaml
trigger:
  type: webhook
  secret: ${WEBHOOK_SECRET}
  signature_header: X-Hub-Signature-256  # GitHub format
```

**Supported formats:**
- GitHub: `sha256=<hex>` in `X-Hub-Signature-256`
- Shopify: `X-Shopify-Hmac-Sha256`
- Generic: Raw hex signature

### Payload Transformation

Transform incoming webhook payloads into agent input using Go templates.

```yaml
webhook_input:
  template: |
    Process this order:
    Order ID: {{ .body.order.id }}
    Customer: {{ .body.customer.name }}
    Items: {{ toJson .body.items }}
```

**Available template data:**

| Variable | Description |
|----------|-------------|
| `.body` | Parsed JSON body as map |
| `.headers` | HTTP headers as map |
| `.query` | URL query parameters as map |
| `.fields` | Extracted fields (see below) |

**Template functions:**

| Function | Description |
|----------|-------------|
| `toJson` | Convert value to JSON string |
| `toJsonPretty` | Convert to pretty-printed JSON |
| `default` | Return default if value is nil/empty |
| `now` | Current time in RFC3339 format |

### Field Extraction

Extract specific fields for use in templates:

```yaml
webhook_input:
  extract_fields:
    - path: .body.order.id
      as: order_id
    - path: .body.customer.email
      as: email
  template: |
    Order {{ .fields.order_id }} from {{ .fields.email }}
```

### Response Modes

Control how webhooks respond to requests.

**Sync Mode (default)**: Wait for agent completion

```yaml
response:
  mode: sync
  timeout: 30s  # Max wait time
```

Response:
```json
{
  "status": "completed",
  "result": "Agent invocation completed successfully",
  "agent_name": "order-processor"
}
```

**Async Mode**: Return immediately with task ID

```yaml
response:
  mode: async
```

Response (HTTP 202):
```json
{
  "task_id": "webhook-order-processor-1703520000000000000",
  "status": "accepted",
  "agent_name": "order-processor"
}
```

**Callback Mode**: POST result to callback URL when done

```yaml
response:
  mode: callback
  callback_field: callback_url  # Field in payload containing callback URL
```

The webhook expects the request body to contain the callback URL:
```json
{
  "data": { ... },
  "callback_url": "https://example.com/webhook-callback"
}
```

---

## Outbound Notifications

Configure agents to notify external services when events occur.

### Basic Notification

```yaml
agents:
  order-processor:
    llm: default
    notifications:
      - id: zapier-webhook
        url: https://hooks.zapier.com/hooks/catch/123/abc
        events: [task.completed, task.failed]
```

### Complete Notification Configuration

```yaml
agents:
  order-processor:
    llm: default
    notifications:
      - id: slack-webhook
        type: webhook
        url: https://hooks.slack.com/services/T00/B00/XXX
        enabled: true
        events:
          - task.completed
          - task.failed
          - task.started
        headers:
          Authorization: "Bearer ${SLACK_TOKEN}"
        payload:
          template: |
            {
              "text": "Agent {{ .AgentName }} {{ .Status }}",
              "attachments": [{
                "color": "{{ if eq .Status \"completed\" }}good{{ else }}danger{{ end }}",
                "fields": [
                  {"title": "Task ID", "value": "{{ .TaskID }}", "short": true},
                  {"title": "Event", "value": "{{ .Type }}", "short": true}
                ]
              }]
            }
        retry:
          max_attempts: 3
          initial_delay: 1s
          max_delay: 30s
```

### Notification Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | required | Unique identifier for this notification |
| `type` | string | `webhook` | Notification type (currently only `webhook`) |
| `url` | string | required | Webhook endpoint URL |
| `enabled` | bool | `true` | Enable/disable this notification |
| `events` | []string | required | Events that trigger notification |
| `headers` | map | - | Custom HTTP headers |
| `payload` | object | - | Custom payload configuration |
| `retry` | object | - | Retry configuration |

### Notification Events

| Event | Description |
|-------|-------------|
| `task.started` | Fired when agent begins processing |
| `task.completed` | Fired when agent completes successfully |
| `task.failed` | Fired when agent encounters an error |

### Default Payload

Without custom template, notifications send this JSON:

```json
{
  "type": "task.completed",
  "agent_name": "order-processor",
  "task_id": "abc-123",
  "status": "completed",
  "result": "Order processed successfully",
  "timestamp": "2024-12-25T20:00:00Z"
}
```

### Custom Payload Template

Use Go templates to customize the payload:

```yaml
payload:
  template: |
    {
      "event": "{{ .Type }}",
      "agent": "{{ .AgentName }}",
      "success": {{ if eq .Status "completed" }}true{{ else }}false{{ end }},
      "details": {{ toJson . }}
    }
```

**Available template data:**

| Field | Type | Description |
|-------|------|-------------|
| `.Type` | string | Event type (e.g., `task.completed`) |
| `.AgentName` | string | Name of the agent |
| `.TaskID` | string | Unique task identifier |
| `.Status` | string | `started`, `completed`, or `failed` |
| `.Result` | string | Completion result (on success) |
| `.Error` | string | Error message (on failure) |
| `.Timestamp` | time | Event timestamp |
| `.Metadata` | map | Additional metadata |

### Retry Configuration

Configure automatic retries with exponential backoff:

```yaml
retry:
  max_attempts: 3       # Number of retry attempts (default: 3)
  initial_delay: 1s     # Initial delay before first retry (default: 1s)
  max_delay: 30s        # Maximum delay between retries (default: 30s)
```

Retries use exponential backoff with the delays specified.

---

## Integration Examples

### GitHub Webhooks

```yaml
agents:
  github-handler:
    llm: default
    instruction: |
      You process GitHub webhook events.
      For push events, summarize the commits.
      For PR events, analyze the changes.
    trigger:
      type: webhook
      path: /webhooks/github
      secret: ${GITHUB_WEBHOOK_SECRET}
      signature_header: X-Hub-Signature-256
      webhook_input:
        template: |
          GitHub {{ .headers.X-GitHub-Event }} event:
          Repository: {{ .body.repository.full_name }}
          {{ toJsonPretty .body }}
```

### Shopify Order Processing

```yaml
agents:
  shopify-orders:
    llm: default
    instruction: Process incoming Shopify orders
    trigger:
      type: webhook
      path: /webhooks/shopify/orders
      secret: ${SHOPIFY_WEBHOOK_SECRET}
      signature_header: X-Shopify-Hmac-Sha256
      response:
        mode: async
    notifications:
      - id: order-complete
        url: https://hooks.zapier.com/hooks/catch/123/abc
        events: [task.completed]
        payload:
          template: |
            {"order_id": "{{ .TaskID }}", "status": "{{ .Status }}"}
```

### Zapier Integration

**Inbound (Zapier triggers Hector):**
```yaml
trigger:
  type: webhook
  response:
    mode: callback
    callback_field: callback_url
```

**Outbound (Hector notifies Zapier):**
```yaml
notifications:
  - id: zapier
    url: https://hooks.zapier.com/hooks/catch/123/abc
    events: [task.completed, task.failed]
```

### Slack Notifications

```yaml
notifications:
  - id: slack-alerts
    url: ${SLACK_WEBHOOK_URL}
    events: [task.failed]
    payload:
      template: |
        {
          "text": ":warning: Agent {{ .AgentName }} failed",
          "attachments": [{
            "color": "danger",
            "text": "Error: {{ .Error }}"
          }]
        }
```

---

## Security Best Practices

1. **Always use signature verification** for inbound webhooks:
   ```yaml
   trigger:
     type: webhook
     secret: ${WEBHOOK_SECRET}  # Store in .env
   ```

2. **Use environment variables** for sensitive URLs and tokens:
   ```yaml
   notifications:
     - id: slack
       url: ${SLACK_WEBHOOK_URL}
       headers:
         Authorization: "Bearer ${SLACK_TOKEN}"
   ```

3. **Enable authentication** on the Hector server for production:
   ```yaml
   server:
     auth:
       enabled: true
       jwks_url: https://auth.example.com/.well-known/jwks.json
   ```
