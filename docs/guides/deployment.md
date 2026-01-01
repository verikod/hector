# Deployment

Deploy Hector to production environments with Kubernetes, Docker, or as a standalone binary.

## Binary Deployment

### SystemD Service (Linux)

Create `/etc/systemd/system/hector.service`:

```ini
[Unit]
Description=Hector AI Agent Platform
After=network.target

[Service]
Type=simple
User=hector
Group=hector
WorkingDirectory=/opt/hector
Environment="PATH=/usr/local/bin:/usr/bin:/bin"
EnvironmentFile=/etc/hector/environment
ExecStart=/usr/local/bin/hector serve --config /etc/hector/config.yaml
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=hector

[Install]
WantedBy=multi-user.target
```

Environment file `/etc/hector/environment`:

```bash
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

Deploy:

```bash
# Create user
sudo useradd -r -s /bin/false hector

# Install binary
sudo cp hector /usr/local/bin/
sudo chmod +x /usr/local/bin/hector

# Setup directories
sudo mkdir -p /etc/hector /opt/hector
sudo cp config.yaml /etc/hector/
sudo chown -R hector:hector /opt/hector

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable hector
sudo systemctl start hector

# Check status
sudo systemctl status hector
```

### LaunchD Service (macOS)

Create `~/Library/LaunchAgents/com.hector.agent.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.hector.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/hector</string>
        <string>serve</string>
        <string>--config</string>
        <string>/Users/yourusername/.hector/config.yaml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Users/yourusername/.hector/hector.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/yourusername/.hector/hector-error.log</string>
</dict>
</plist>
```

Deploy:

```bash
launchctl load ~/Library/LaunchAgents/com.hector.agent.plist
launchctl start com.hector.agent
```

## Docker Deployment

### Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  hector:
    image: ghcr.io/verikod/hector:latest
    ports:
      - "8080:8080"
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}
    volumes:
      - ./config.yaml:/app/config.yaml
      - hector-data:/app/.hector
    command: hector serve --config /app/config.yaml
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 5
      timeout: 10s
      retries: 3

  postgres:
    image: postgres:16-alpine
    environment:
      - POSTGRES_USER=hector
      - POSTGRES_PASSWORD=secret
      - POSTGRES_DB=hector
    volumes:
      - postgres-data:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  hector-data:
  postgres-data:
```

Configuration `config.yaml`:

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

databases:
  main:
    driver: postgres
    host: postgres
    port: 5432
    database: hector
    user: hector
    password: secret

server:
  port: 8080

storage:
  tasks:
    backend: sql
    database: main
  sessions:
    backend: sql
    database: main
```

Deploy:

```bash
export OPENAI_API_KEY="sk-..."
docker-compose up -d
```

### Docker Standalone

```bash
docker run -d \
  --name hector \
  -p 8080:8080 \
  -e OPENAI_API_KEY="sk-..." \
  -v $(pwd)/config.yaml:/app/config.yaml \
  --restart unless-stopped \
  ghcr.io/verikod/hector:latest \
  hector serve --config /app/config.yaml
```

## Kubernetes Deployment

### Basic Deployment

Create `k8s/deployment.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: hector

---
apiVersion: v1
kind: Secret
metadata:
  name: hector-secrets
  namespace: hector
type: Opaque
stringData:
  OPENAI_API_KEY: sk-...
  ANTHROPIC_API_KEY: sk-ant-...

---
apiVersion: v1
kind: ConfigMap
metadata:
  name: hector-config
  namespace: hector
data:
  config.yaml: |
    version: "2"

    llms:
      default:
        provider: openai
        model: gpt-4o
        api_key: ${OPENAI_API_KEY}

    agents:
      assistant:
        llm: default
        tools: [search]
        streaming: true

    server:
      port: 8080

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hector
  namespace: hector
spec:
  replicas: 3
  selector:
    matchLabels:
      app: hector
  template:
    metadata:
      labels:
        app: hector
    spec:
      containers:
      - name: hector
        image: ghcr.io/verikod/hector:latest
        ports:
        - containerPort: 8080
          name: http
        args: ["serve", "--config", "/app/config.yaml"]
        envFrom:
        - secretRef:
            name: hector-secrets
        volumeMounts:
        - name: config
          mountPath: /app/config.yaml
          subPath: config.yaml
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
      volumes:
      - name: config
        configMap:
          name: hector-config

---
apiVersion: v1
kind: Service
metadata:
  name: hector
  namespace: hector
spec:
  selector:
    app: hector
  ports:
  - port: 80
    targetPort: 8080
    name: http
  type: LoadBalancer
```

Deploy:

```bash
kubectl apply -f k8s/deployment.yaml
```

### With Postgres Persistence

Add to `k8s/deployment.yaml`:

```yaml
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-pvc
  namespace: hector
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi

---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: hector
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: postgres:16-alpine
        ports:
        - containerPort: 5432
        env:
        - name: POSTGRES_DB
          value: hector
        - name: POSTGRES_USER
          value: hector
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: hector-secrets
              key: DB_PASSWORD
        volumeMounts:
        - name: postgres-storage
          mountPath: /var/lib/postgresql/data
      volumes:
      - name: postgres-storage
        persistentVolumeClaim:
          claimName: postgres-pvc

---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: hector
spec:
  selector:
    app: postgres
  ports:
  - port: 5432
    targetPort: 5432
  clusterIP: None
```

Update ConfigMap with database config:

```yaml
databases:
  main:
    driver: postgres
    host: postgres.hector.svc.cluster.local
    port: 5432
    database: hector
    user: hector
    password: ${DB_PASSWORD}

storage:
  tasks:
    backend: sql
    database: main
  sessions:
    backend: sql
    database: main
```

### Horizontal Pod Autoscaling

Create `k8s/hpa.yaml`:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: hector
  namespace: hector
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: hector
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

### Ingress

Create `k8s/ingress.yaml`:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: hector
  namespace: hector
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - agents.yourdomain.com
    secretName: hector-tls
  rules:
  - host: agents.yourdomain.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: hector
            port:
              number: 80
```

## Health Checks

Hector exposes `/health` endpoint:

```bash
curl http://localhost:8080/health
```

Response:

```json
{
  "status": "healthy",
  "version": "2.0.0"
}
```

Use for:

- Load balancer health checks
- Kubernetes liveness/readiness probes
- Monitoring systems

## Graceful Shutdown

Hector handles `SIGTERM` and `SIGINT` gracefully:

- Stops accepting new requests
- Completes in-flight requests
- Closes database connections
- Shuts down cleanly

Shutdown timeout is 30 seconds by default.

## Observability

### Metrics

Enable Prometheus metrics:

```yaml
observability:
  metrics:
    enabled: true
```

Metrics endpoint: `http://localhost:8080/metrics`

Available metrics:

- `hector_llm_requests_total` - Total LLM requests
- `hector_llm_request_duration_seconds` - LLM latency
- `hector_llm_tokens_total` - Token usage
- `hector_agent_requests_total` - Agent requests
- `hector_tool_calls_total` - Tool invocations

Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'hector'
    static_configs:
      - targets: ['hector.hector.svc.cluster.local:8080']
    metrics_path: /metrics
```

### Tracing

Enable OpenTelemetry tracing:

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: jaeger-collector.observability.svc.cluster.local:4317
    sampling_rate: 1.0
```

Traces include:

- Agent request spans
- LLM call spans
- Tool execution spans
- Database operation spans

View traces in Jaeger, Zipkin, or other OTLP-compatible systems.

## Security

### Authentication

Enable JWT authentication:

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://auth.yourdomain.com/.well-known/jwks.json
```

Or use API keys:

```yaml
server:
  auth:
    enabled: true
    api_keys:
      - ${API_KEY_1}
      - ${API_KEY_2}
```

### TLS/HTTPS

Terminate TLS at:

- **Load balancer** (recommended): AWS ALB, nginx
- **Ingress controller**: Kubernetes Ingress with cert-manager
- **Reverse proxy**: nginx, Caddy

Hector serves HTTP. Use reverse proxy for TLS.

Example nginx config:

```nginx
server {
    listen 443 ssl http2;
    server_name agents.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/agents.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/agents.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Network Policies (Kubernetes)

Restrict traffic:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: hector
  namespace: hector
spec:
  podSelector:
    matchLabels:
      app: hector
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
    ports:
    - protocol: TCP
      port: 8080
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: postgres
    ports:
    - protocol: TCP
      port: 5432
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443  # Allow HTTPS for LLM APIs
```

## Configuration Management

### Kubernetes ConfigMaps

Hot reload with ConfigMap changes:

```bash
# Update config
kubectl edit configmap hector-config -n hector

# Trigger reload (if using --watch)
# Or restart pods
kubectl rollout restart deployment/hector -n hector
```

### External Configuration

Use distributed configuration stores:

```yaml
# config.yaml with remote config
config:
  provider: consul
  address: consul.service.consul:8500
  key: hector/production/config
```

Supported providers:

- File (default)
- Consul
- etcd
- ZooKeeper (via future extension)

## Backups

### Database Backups

Backup Postgres (tasks, sessions, checkpoints):

```bash
# Backup
pg_dump -h postgres.hector.svc.cluster.local -U hector hector > backup.sql

# Restore
psql -h postgres.hector.svc.cluster.local -U hector hector < backup.sql
```

Kubernetes CronJob for automated backups:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: hector-backup
  namespace: hector
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:16-alpine
            env:
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef:
                  name: hector-secrets
                  key: DB_PASSWORD
            command:
            - /bin/sh
            - -c
            - |
              pg_dump -h postgres.hector.svc.cluster.local -U hector hector | \
              gzip > /backups/hector-$(date +%Y%m%d-%H%M%S).sql.gz
            volumeMounts:
            - name: backups
              mountPath: /backups
          restartPolicy: OnFailure
          volumes:
          - name: backups
            persistentVolumeClaim:
              claimName: backup-pvc
```

### Configuration Backups

Store configuration in Git:

```bash
git add config.yaml k8s/
git commit -m "Production config $(date +%Y-%m-%d)"
git push
```

## Monitoring

### Key Metrics to Monitor

- **Request rate**: `hector_agent_requests_total`
- **Error rate**: `hector_agent_requests_total{status="error"}`
- **Latency**: `hector_agent_request_duration_seconds`
- **Token usage**: `hector_llm_tokens_total`
- **Pod health**: Kubernetes liveness/readiness

### Alerting Rules

Prometheus alerting rules:

```yaml
groups:
- name: hector
  rules:
  - alert: HighErrorRate
    expr: rate(hector_agent_requests_total{status="error"}[5m]) > 0.1
    for: 5m
    annotations:
      summary: "High error rate in Hector"

  - alert: HighLatency
    expr: histogram_quantile(0.95, rate(hector_agent_request_duration_seconds_bucket[5m])) > 10
    for: 5m
    annotations:
      summary: "95th percentile latency > 10s"

  - alert: HighTokenUsage
    expr: rate(hector_llm_tokens_total[1h]) > 1000000
    for: 1h
    annotations:
      summary: "Token usage exceeds 1M/hour"
```

## Scaling

### Vertical Scaling

Increase resources for individual pods:

```



### Vertical Scaling

Increase resources for individual pods:

```yaml
resources:
  requests:
    memory: "256Mi"  # Up from 128Mi
    cpu: "200m"      # Up from 100m
  limits:
    memory: "1Gi"    # Up from 512Mi
    cpu: "1000m"     # Up from 500m
```

### Horizontal Scaling

Add more replicas:

```bash
kubectl scale deployment hector --replicas=5 -n hector
```

Or use HPA (shown above).

### Database Scaling

For high load:

- Use connection pooling (PgBouncer)
- Read replicas for sessions
- Partition large tables

## Best Practices

### Resource Limits

Set appropriate limits:

```yaml
resources:
  requests:
    memory: "128Mi"  # Minimum needed
    cpu: "100m"
  limits:
    memory: "512Mi"  # Maximum allowed
    cpu: "500m"
```

### Multi-Region Deployment

Deploy to multiple regions:

- Reduce latency for global users
- Improve availability
- Use GeoDNS for routing

### Secrets Management

Use proper secret management:

- Kubernetes Secrets
- HashiCorp Vault
- AWS Secrets Manager
- Azure Key Vault

Never commit secrets to Git.

### Logging

Structure logs for easy parsing:

```yaml
logger:
  level: info
  format: json
```

Send logs to centralized logging:

- ELK Stack
- Loki
- CloudWatch Logs

