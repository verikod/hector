# Persistence

Configure persistent storage for tasks, sessions, and checkpoints.

## Quick Start

### Basic Persistence

Enable SQLite persistence:

```bash
hector serve --model gpt-4o --storage sqlite
```

This enables:

- Task persistence
- Session persistence
- Checkpoint/recovery (automatic)
- Database: `.hector/hector.db`

### With Postgres

```bash
hector serve \
  --model gpt-4o \
  --storage postgres \
  --storage-db "host=localhost port=5432 user=hector password=secret dbname=hector"
```

### With MySQL

```bash
hector serve \
  --model gpt-4o \
  --storage mysql \
  --storage-db "hector:secret@tcp(localhost:3306)/hector"
```

## Storage Backends

### In-Memory (Default)

No persistence, data lost on restart:

```yaml
storage:
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

storage:
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

storage:
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

storage:
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
storage:
  tasks:
    backend: sql      # or inmemory
    database: main    # references databases config
```

Persisted data:

- Task ID
- Context ID (Agent)
- Status (pending, running, completed, failed)
- History (Messages, including input/output)
- Artifacts
- Metadata
- Created/updated timestamps

Query tasks:

```bash
curl http://localhost:8080/tasks
curl http://localhost:8080/tasks/{task_id}
```

## Session Persistence

Sessions store conversation history:

```yaml
storage:
  sessions:
    backend: sql      # or inmemory
    database: main
```

Persisted data:

- Session ID
- App Name / User ID
- Event Log (Messages, Tool Calls)
- State Variables
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

## Vector Memory Persistence

Persist memory indices (embeddings) to disk using the embedded vector store (`chromem`).

```yaml
storage:
  memory:
    backend: vector
    embedder: default       # Reference to embedders config
    vector_provider:
      type: chromem         # Embedded vector store (default)
      chromem:
        persist_path: .hector/vectors  # Directory for vector data
        compress: true      # Gzip compression
```

Supported providers:

- **chromem**: Embedded, file-based (Go native).
- **qdrant**, **chroma**, **pgvector**: External (coming soon).

## Checkpointing

Automatic checkpoint/recovery for long-running tasks:

```yaml
storage:
  checkpoint:
    enabled: true
    strategy: hybrid        # event, interval, or hybrid
    after_tools: true       # Checkpoint after tool execution
    before_llm: true        # Checkpoint before LLM calls
    interval: 5           # Interval for hybrid strategy
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
  interval: 10       # Every 60 seconds
```

**hybrid**: Both events and intervals

```yaml
checkpoint:
  enabled: true
  strategy: hybrid
  after_tools: true
  interval: 5
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

storage:
  tasks:
    backend: sql
    database: task_db

  sessions:
    backend: sql
    database: session_db
```

Or mix SQL and in-memory:

```yaml
storage:
  tasks:
    backend: sql       # Persist tasks
    database: main

  sessions:
    backend: inmemory  # Sessions in memory
```

### Schemas

Hector automatically manages database migrations. The following tables are created:

**`a2a_tasks`** (Tasks)
Stores A2A protocol task executions.

- `id` (PK): Task UUID
- `context_id`: Agent/Context identifier
- `status_json`: Task status (pending, running, completed, failed)
- `history_json`: Execution history (messages)
- `artifacts_json`: Generated artifacts
- `metadata_json`: Custom metadata
- `created_at`: Creation timestamp
- `updated_at`: Update timestamp

**`sessions`** (Session Header)
Stores session metadata and current state.

- `app_name` (PK): Application namespace
- `user_id` (PK): User identifier
- `id` (PK): Session UUID
- `state_json`: Session-scoped state variables
- `created_at`: Creation timestamp
- `updated_at`: Update timestamp

**`session_events`** (Event Log)
Stores the append-only history of all session events (messages, tool calls, state changes).

- `id`: Event UUID
- `session_id`: Reference to session
- `sequence_num`: Ordering within session
- `type`: Event type (message, tool, error, etc.)
- `content_json`: Message content
- `state_delta_json`: State changes applied by this event
- `created_at`: Event timestamp

**`app_states` / `user_states`**
Stores state variables scoped to the app or user (cross-session memory).

> [!NOTE]
> Checkpoints are stored as JSON blobs within the `session_events` or `state_json`, not as a separate table.


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

storage:
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

storage:
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
observability:
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
-- Task queries by context/agent
CREATE INDEX idx_tasks_context ON a2a_tasks(context_id);

-- Task queries by status (requires JSON extraction index in some DBs)
-- Postgres example:
CREATE INDEX idx_tasks_status ON a2a_tasks((status_json->>'status'));

-- Session queries by user
CREATE INDEX idx_sessions_user ON sessions(user_id);

```

### Data Retention

Clean up old data:

```sql
-- Delete old completed tasks
DELETE FROM a2a_tasks
WHERE status_json LIKE '%completed%'
AND updated_at < NOW() - INTERVAL '30 days';

-- Delete old sessions (cascades to events)
DELETE FROM sessions
WHERE updated_at < NOW() - INTERVAL '90 days';
```

Automate with cron jobs or database triggers.

### Partition Large Tables

For high volume:

```sql
-- Partition tasks by month
CREATE TABLE tasks_2025_01 PARTITION OF a2a_tasks
FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');

CREATE TABLE tasks_2025_02 PARTITION OF a2a_tasks
FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');
```




