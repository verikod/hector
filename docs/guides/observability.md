# Observability

Monitor Hector with Prometheus metrics and OpenTelemetry tracing.

## Quick Start

Enable observability via CLI:

```bash
hector serve --model gpt-4o --observe
```

This enables:

- Prometheus metrics at `/metrics`
- OTLP tracing to `localhost:4317`

## Metrics

### Enable Metrics

CLI:

```bash
hector serve --model gpt-4o --observe
```

Configuration file:

```yaml
observability:
  metrics:
    enabled: true
```

### Access Metrics

Metrics endpoint:

```
http://localhost:8080/metrics
```

Example output:

```
# HELP hector_llm_requests_total Total LLM requests
# TYPE hector_llm_requests_total counter
hector_llm_requests_total{agent="assistant",provider="openai",model="gpt-4o"} 42

# HELP hector_llm_request_duration_seconds LLM request duration
# TYPE hector_llm_request_duration_seconds histogram
hector_llm_request_duration_seconds_bucket{agent="assistant",le="0.5"} 10
hector_llm_request_duration_seconds_bucket{agent="assistant",le="1"} 25
hector_llm_request_duration_seconds_bucket{agent="assistant",le="5"} 40
hector_llm_request_duration_seconds_sum{agent="assistant"} 89.4
hector_llm_request_duration_seconds_count{agent="assistant"} 42

# HELP hector_llm_tokens_total Total tokens used
# TYPE hector_llm_tokens_total counter
hector_llm_tokens_total{agent="assistant",type="prompt"} 12500
hector_llm_tokens_total{agent="assistant",type="completion"} 8200
```

### Available Metrics

**Request Metrics**

- `hector_llm_requests_total` - Total LLM requests (counter)
- `hector_llm_request_duration_seconds` - LLM latency (histogram)
- `hector_agent_requests_total` - Total agent requests (counter)
- `hector_agent_request_duration_seconds` - Agent latency (histogram)

**Token Metrics**

- `hector_llm_tokens_total` - Token usage (counter)
  - Labels: `agent`, `type` (prompt/completion)

**Tool Metrics**

- `hector_tool_calls_total` - Tool invocations (counter)
  - Labels: `agent`, `tool`, `status` (success/error)
- `hector_tool_call_duration_seconds` - Tool execution time (histogram)

**Error Metrics**

- `hector_errors_total` - Total errors (counter)
  - Labels: `component`, `error_type`

### Custom Labels

Add constant labels to all metrics:

```yaml
observability:
  metrics:
    enabled: true
    const_labels:
        environment: production
        region: us-east-1
        team: ai-platform
```

Metrics include these labels:

```
hector_llm_requests_total{environment="production",region="us-east-1",...} 42
```

### Metric Namespace

Customize metric prefix:

```yaml
observability:
  metrics:
    enabled: true
    namespace: mycompany      # Prefix: mycompany_
    subsystem: agents         # Becomes: mycompany_agents_
```

Metrics:

- `mycompany_agents_llm_requests_total`
- `mycompany_agents_llm_tokens_total`

### Custom Endpoint

Change metrics path:

```yaml
observability:
  metrics:
    enabled: true
    endpoint: /custom-metrics
```

Access at: `http://localhost:8080/custom-metrics`

## Tracing

### Enable Tracing

CLI (OTLP to localhost):

```bash
hector serve --model gpt-4o --observe
```

Configuration file:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: localhost:4317
```

### Exporters

#### OTLP (OpenTelemetry Protocol)

Default exporter for Jaeger, Tempo, etc:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: localhost:4317  # gRPC
    insecure: true
```

OTLP over HTTP:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: localhost:4318  # HTTP
    insecure: true
```

#### Jaeger

Direct Jaeger exporter:

```yaml
observability:
  tracing:
    enabled: true
    exporter: jaeger
    endpoint: http://localhost:14268/api/traces
```

#### Zipkin

```yaml
observability:
  tracing:
    enabled: true
    exporter: zipkin
    endpoint: http://localhost:9411/api/v2/spans
```

#### Stdout (Debug)

Print traces to console:

```yaml
observability:
  tracing:
    enabled: true
    exporter: stdout
```

### Sampling

Control what fraction of traces are sampled:

```yaml
observability:
  tracing:
    enabled: true
    sampling_rate: 1.0   # Sample all traces
```

Sampling rates:

- `1.0` - All traces (100%)
- `0.1` - 10% of traces
- `0.01` - 1% of traces

Use lower rates in high-traffic production:

```yaml
# Development: sample all
sampling_rate: 1.0

# Production: sample 10%
sampling_rate: 0.1
```

### Service Identification

Identify your service in traces:

```yaml
observability:
  tracing:
    enabled: true
    service_name: hector-production
    service_version: 2.0.0
```

### Trace Context

Traces include:

**Agent Spans**

- Agent name
- Request/response
- Duration

**LLM Spans**

- Provider (openai, anthropic)
- Model (gpt-4o, claude-sonnet-4)
- Token counts
- Latency

**Tool Spans**

- Tool name
- Parameters
- Result
- Duration

**Database Spans**

- Operation (query, insert, update)
- Table
- Duration

### Payload Capture

Capture full LLM requests/responses (debug only):

```yaml
observability:
  tracing:
    enabled: true
    capture_payloads: true
```

Warning: Produces large spans. Use only for debugging.

### Authentication Headers

Send headers with trace exports:

```yaml
observability:
  tracing:
    enabled: true
    endpoint: traces.example.com:4317
    headers:
        Authorization: Bearer ${TRACE_API_KEY}
        X-Custom-Header: value
```

## Prometheus Setup

### Scrape Configuration

Add Hector to Prometheus:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: hector
    static_configs:
      - targets:
          - localhost:8080
    metrics_path: /metrics
    scrape_interval: 15s
```

### Kubernetes Service Monitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: hector
  namespace: hector
spec:
  selector:
    matchLabels:
      app: hector
  endpoints:
  - port: http
    path: /metrics
    interval: 15s
```

## Jaeger Setup

### Local Jaeger

Run Jaeger locally:

```bash
docker run -d \
  --name jaeger \
  -e COLLECTOR_OTLP_ENABLED=true \
  -p 4317:4317 \
  -p 4318:4318 \
  -p 16686:16686 \
  jaegertracing/all-in-one:latest
```

Configure Hector:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: localhost:4317
```

View traces: `http://localhost:16686`

### Kubernetes Jaeger

Deploy Jaeger Operator:

```bash
kubectl create namespace observability
kubectl apply -f https://github.com/jaegertracing/jaeger-operator/releases/latest/download/jaeger-operator.yaml -n observability
```

Create Jaeger instance:

```yaml
apiVersion: jaegertracing.io/v1
kind: Jaeger
metadata:
  name: jaeger
  namespace: observability
spec:
  strategy: production
  storage:
    type: elasticsearch
    options:
      es:
        server-urls: http://elasticsearch:9200
```

Configure Hector:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: jaeger-collector.observability.svc.cluster.local:4317
```

## Grafana Dashboards

### Metrics Dashboard

Create dashboard for Hector metrics:

```json
{
  "dashboard": {
    "title": "Hector Metrics",
    "panels": [
      {
        "title": "Request Rate",
        "targets": [
          {
            "expr": "rate(hector_llm_requests_total[5m])"
          }
        ]
      },
      {
        "title": "Latency (p95)",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, rate(hector_llm_request_duration_seconds_bucket[5m]))"
          }
        ]
      },
      {
        "title": "Token Usage",
        "targets": [
          {
            "expr": "rate(hector_llm_tokens_total[1h])"
          }
        ]
      }
    ]
  }
}
```

### Trace Visualization

Connect Grafana to Jaeger:

1. Add Jaeger data source
2. Set URL: `http://jaeger-query:16686`
3. Explore traces in Grafana

## Alerting

### Prometheus Alerting Rules

```yaml
groups:
  - name: hector_alerts
    rules:
      - alert: HighErrorRate
        expr: rate(hector_errors_total[5m]) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Hector error rate above 10%"

      - alert: HighLatency
        expr: histogram_quantile(0.95, rate(hector_llm_request_duration_seconds_bucket[5m])) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "95th percentile latency above 10s"

      - alert: HighTokenUsage
        expr: rate(hector_llm_tokens_total[1h]) > 1000000
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "Token usage exceeds 1M/hour"

      - alert: ToolFailureRate
        expr: rate(hector_tool_calls_total{status="error"}[5m]) / rate(hector_tool_calls_total[5m]) > 0.2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Tool failure rate above 20%"
```

## Production Configuration

### Development

Sample all traces, enable all metrics:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: localhost:4317
    sampling_rate: 1.0
    capture_payloads: true
  metrics:
    enabled: true
```

### Production

Lower sampling, essential metrics:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: otlp-collector.observability.svc.cluster.local:4317
    sampling_rate: 0.1  # Sample 10%
    service_name: hector-production
    service_version: 2.0.0
    headers:
      Authorization: Bearer ${OTLP_API_KEY}
  metrics:
    enabled: true
    const_labels:
      environment: production
      cluster: us-east-1
```

## Examples

### Complete Observability Stack

```yaml
# Hector config
server:
  port: 8080

observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: jaeger-collector:4317
    sampling_rate: 1.0
  metrics:
    enabled: true
    const_labels:
      environment: staging
```

```yaml
# docker-compose.yml
version: '3.8'

services:
  hector:
    image: ghcr.io/verikod/hector:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}

  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686"  # UI
      - "4317:4317"    # OTLP gRPC
    environment:
      - COLLECTOR_OTLP_ENABLED=true

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    command:
      - --config.file=/etc/prometheus/prometheus.yml

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
```

Access:

- Hector: `http://localhost:8080`
- Metrics: `http://localhost:8080/metrics`
- Jaeger: `http://localhost:16686`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000`

## Best Practices

### Sampling Strategy

```yaml
# ✅ Good - Adaptive sampling
production:
  sampling_rate: 0.1  # 10% for normal traffic

development:
  sampling_rate: 1.0  # 100% for debugging
```

### Label Cardinality

Avoid high-cardinality labels:

```yaml
# ❌ Bad - User ID in labels (unbounded)
const_labels:
  user_id: ${USER_ID}

# ✅ Good - Environment in labels (bounded)
const_labels:
  environment: production
  region: us-east-1
```

### Payload Capture

Use only for debugging:

```yaml
# ✅ Good - Disabled in production
production:
  capture_payloads: false

# ✅ Good - Enabled for debugging
development:
  capture_payloads: true
```



