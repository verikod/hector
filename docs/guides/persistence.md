# Persistence

Configure persistent storage for tasks, sessions, and checkpoints.

## Quick Start

### Zero-Config Persistence

Enable SQLite persistence:

```bash
hector serve --model gpt-5 --storage sqlite
```

This enables:

- Task persistence
- Session persistence
- Checkpoint/recovery (automatic)
- Database: `.hector/hector.db`

### With Postgres

```bash
hector serve \
  --model gpt-5 \
  --storage postgres \
  --storage-db "host=localhost port=5432 user=hector password=secret dbname=hector"
```

### With MySQL

```bash
hector serve \
  --model gpt-5 \
  --storage mysql \
  --storage-db "hector:secret@tcp(localhost:3306)/hector"
```

## Storage Backends

### In-Memory (Default)

No persistence, data lost on restart:

```yaml
server:
  tasks:
    backend: inmemory
  sessions:
    backend: inmemory
```

Fast but not durable. Use for development only.

### SQLite

Embedded database, single file:

```yaml
databases:
  main:
    driver: sqlite
    database: .hector/hector.db

server:
  tasks:
    backend: sql
    database: main
  sessions:
    backend: sql
    database: main
```

Best for:

- Single-instance deployments
- Development
- Edge deployments
- Low-medium traffic

### PostgreSQL

Production-grade relational database:

```yaml
databases:
  main:
    driver: postgres
    host: localhost
    port: 5432
    database: hector
    user: hector
    password: ${DB_PASSWORD}
    max_open_conns: 25
    max_idle_conns: 5
    conn_max_lifetime: 5m

server:
  tasks:
    backend: sql
    database: main
  sessions:
    backend: sql
    database: main
```

Best for:

- Production deployments
- High availability
- Horizontal scaling
- Multiple instances

### MySQL

Alternative relational database:

```yaml
databases:
  main:
    driver: mysql
    host: localhost
    port: 3306
    database: hector
    user: hector
    password: ${DB_PASSWORD}
    max_open_conns: 25
    max_idle_conns: 5

server:
  tasks:
    backend: sql
    database: main
  sessions:
    backend: sql
    database: main
```

## Database Configuration

### Connection Parameters

```yaml
databases:
  main:
    driver: postgres          # sqlite, postgres, mysql
    host: localhost           # Database host
    port: 5432                # Database port
    database: hector          # Database name
    user: hector              # Database user
    password: ${DB_PASSWORD}  # Password from environment

    # Connection pool settings
    max_open_conns: 25        # Max open connections
    max_idle_conns: 5         # Max idle connections
    conn_max_lifetime: 5m     # Max connection lifetime
    conn_max_idle_time: 1m    # Max idle time
```

### SQLite Options

```yaml
databases:
  main:
    driver: sqlite
    database: /var/lib/hector/hector.db  # Absolute path
    # Or relative path
    database: .hector/hector.db
```

SQLite automatically creates the database file if it doesn't exist.

### Connection Strings

#### PostgreSQL DSN

```yaml
databases:
  main:
    driver: postgres
    dsn: "host=localhost port=5432 dbname=hector user=hector password=secret sslmode=disable"
```

#### MySQL DSN

```yaml
databases:
  main:
    driver: mysql
    dsn: "hector:secret@tcp(localhost:3306)/hector?parseTime=true"
```

## Task Persistence

Tasks are A2A protocol executions:

```yaml
server:
  tasks:
    backend: sql      # or inmemory
    database: main    # references databases config
```

Persisted data:

- Task ID
- Agent name
- Input/output
- Status (pending, running, completed, failed)
- Created/updated timestamps

Query tasks:

```bash
curl http://localhost:8080/tasks
curl http://localhost:8080/tasks/{task_id}
```

## Session Persistence

Sessions store conversation history:

```yaml
server:
  sessions:
    backend: sql      # or inmemory
    database: main
```

Persisted data:

- Session ID
- Agent name
- Message history
- State variables
- Artifacts
- Created/updated timestamps

Sessions survive restarts. Resume conversations:

```bash
curl -X POST http://localhost:8080/agents/assistant/message:send \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "existing-session-id",
    "message": {
      "parts": [{"text": "Continue our conversation"}],
      "role": "user"
    }
  }'
```

## Checkpointing

Automatic checkpoint/recovery for long-running tasks:

```yaml
server:
  checkpoint:
    enabled: true
    strategy: hybrid        # event, interval, or hybrid
    after_tools: true       # Checkpoint after tool execution
    before_llm: true        # Checkpoint before LLM calls
    interval: 30s           # Interval for hybrid strategy
    recovery:
      auto_resume: true     # Auto-resume on startup
      auto_resume_hitl: false  # Require approval for HITL tasks
      timeout: 86400        # 24h checkpoint expiry
```

### Checkpoint Strategies

**event**: Checkpoint at specific events

```yaml
checkpoint:
  enabled: true
  strategy: event
  after_tools: true   # After each tool execution
  before_llm: true    # Before each LLM call
```

**interval**: Checkpoint at regular intervals

```yaml
checkpoint:
  enabled: true
  strategy: interval
  interval: 60s       # Every 60 seconds
```

**hybrid**: Both events and intervals

```yaml
checkpoint:
  enabled: true
  strategy: hybrid
  after_tools: true
  interval: 30s
```

### Recovery

Auto-resume tasks on restart:

```yaml
checkpoint:
  recovery:
    auto_resume: true         # Resume non-HITL tasks
    auto_resume_hitl: false   # Require approval for HITL
    timeout: 86400            # Expire checkpoints after 24h
```

When Hector restarts:

- Tasks with checkpoints are detected
- Non-HITL tasks resume automatically
- HITL tasks require user approval

## Mixed Backends

Use different backends for tasks and sessions:

```yaml
databases:
  task_db:
    driver: postgres
    host: postgres-tasks
    database: tasks

  session_db:
    driver: postgres
    host: postgres-sessions
    database: sessions

server:
  tasks:
    backend: sql
    database: task_db

  sessions:
    backend: sql
    database: session_db
```

Or mix SQL and in-memory:

```yaml
server:
  tasks:
    backend: sql       # Persist tasks
    database: main

  sessions:
    backend: inmemory  # Sessions in memory
```

## Database Migrations

Hector automatically creates required tables on startup:

**tasks table**:

- `id` - Task UUID
- `agent` - Agent name
- `input` - Input data (JSON)
- `output` - Output data (JSON)
- `status` - Task status
- `created_at` - Creation timestamp
- `updated_at` - Update timestamp

**sessions table**:

- `id` - Session UUID
- `agent` - Agent name
- `messages` - Message history (JSON)
- `state` - State variables (JSON)
- `artifacts` - File artifacts (JSON)
- `created_at` - Creation timestamp
- `updated_at` - Update timestamp

**checkpoints table**:

- `id` - Checkpoint UUID
- `task_id` - Task reference
- `state` - Execution state (JSON)
- `created_at` - Creation timestamp

## Production Setup

### PostgreSQL on Kubernetes

```yaml
# postgres-statefulset.yaml
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
        - name: PGDATA
          value: /var/lib/postgresql/data/pgdata
        volumeMounts:
        - name: postgres-storage
          mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
  - metadata:
      name: postgres-storage
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 20Gi
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
  clusterIP: None
```

Hector configuration:

```yaml
databases:
  main:
    driver: postgres
    host: postgres.hector.svc.cluster.local
    port: 5432
    database: hector
    user: hector
    password: ${DB_PASSWORD}
    max_open_conns: 25
    max_idle_conns: 5

server:
  tasks:
    backend: sql
    database: main
  sessions:
    backend: sql
    database: main
  checkpoint:
    enabled: true
    strategy: hybrid
```

### Connection Pooling

Optimize database connections:

```yaml
databases:
  main:
    driver: postgres
    host: localhost
    port: 5432
    database: hector
    user: hector
    password: ${DB_PASSWORD}
    max_open_conns: 25        # Total connections
    max_idle_conns: 5         # Idle pool size
    conn_max_lifetime: 5m     # Max connection age
    conn_max_idle_time: 1m    # Max idle duration
```

Guidelines:

- `max_open_conns`: 25-50 for typical workloads
- `max_idle_conns`: ~20% of max_open_conns
- `conn_max_lifetime`: 5-15 minutes

### High Availability

Use PostgreSQL with replication:

```yaml
databases:
  # Primary for writes
  primary:
    driver: postgres
    host: postgres-primary
    port: 5432
    database: hector

  # Replica for reads
  replica:
    driver: postgres
    host: postgres-replica
    port: 5432
    database: hector

server:
  tasks:
    backend: sql
    database: primary

  sessions:
    backend: sql
    database: replica  # Read-only queries
```

## Backups

### PostgreSQL Backups

```bash
# Backup
pg_dump -h localhost -U hector hector > backup-$(date +%Y%m%d).sql

# Restore
psql -h localhost -U hector hector < backup-20250115.sql
```

Kubernetes CronJob:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: postgres-backup
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
              pg_dump -h postgres -U hector hector | \
              gzip > /backups/backup-$(date +%Y%m%d-%H%M%S).sql.gz
            volumeMounts:
            - name: backups
              mountPath: /backups
          restartPolicy: OnFailure
          volumes:
          - name: backups
            persistentVolumeClaim:
              claimName: backup-pvc
```

### SQLite Backups

```bash
# Backup (while running)
sqlite3 .hector/hector.db ".backup backup-$(date +%Y%m%d).db"

# Or copy file (while stopped)
cp .hector/hector.db backups/hector-$(date +%Y%m%d).db
```

## Monitoring

Monitor database performance:

```yaml
server:
  observability:
    metrics:
      enabled: true
```

Metrics:

- `hector_db_connections_open` - Open connections
- `hector_db_connections_idle` - Idle connections
- `hector_db_query_duration_seconds` - Query latency
- `hector_db_errors_total` - Database errors

Alert on:

- High connection usage
- Slow queries
- Database errors

## Best Practices

### Connection Limits

Match database capacity:

```yaml
# PostgreSQL max_connections = 100
# Hector instances: 4
# max_open_conns per instance: 25
# Total: 4 * 25 = 100 connections
databases:
  main:
    max_open_conns: 25
```

### Index Optimization

Add indexes for common queries:

```sql
-- Task queries by agent
CREATE INDEX idx_tasks_agent ON tasks(agent);

-- Task queries by status
CREATE INDEX idx_tasks_status ON tasks(status);

-- Session queries by agent
CREATE INDEX idx_sessions_agent ON sessions(agent);

-- Checkpoint queries by task
CREATE INDEX idx_checkpoints_task_id ON checkpoints(task_id);
```

### Data Retention

Clean up old data:

```sql
-- Delete old completed tasks
DELETE FROM tasks
WHERE status = 'completed'
AND updated_at < NOW() - INTERVAL '30 days';

-- Delete old sessions
DELETE FROM sessions
WHERE updated_at < NOW() - INTERVAL '90 days';

-- Delete expired checkpoints
DELETE FROM checkpoints
WHERE created_at < NOW() - INTERVAL '24 hours';
```

Automate with cron jobs or database triggers.

### Partition Large Tables

For high volume:

```sql
-- Partition tasks by month
CREATE TABLE tasks_2025_01 PARTITION OF tasks
FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');

CREATE TABLE tasks_2025_02 PARTITION OF tasks
FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');
```

## Next Steps

- [Deployment Guide](deployment.md) - Deploy with persistence
- [Observability Guide](observability.md) - Monitor database performance
- [Security Guide](security.md) - Secure database access
