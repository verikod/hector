# Security

Secure Hector deployments with authentication, authorization, and tool sandboxing.

## Authentication

### JWT Authentication

Enable JWT with JWKS (JSON Web Key Set):

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://auth.yourdomain.com/.well-known/jwks.json
    issuer: https://auth.yourdomain.com/
    audience: your-api-identifier
    require_auth: true  # Default: true when enabled
    excluded_paths:  # Paths that don't require auth
      - /health
      - /.well-known/agent-card.json
```

**Required Fields:**

- `jwks_url`: URL to fetch JSON Web Key Set (public keys for JWT verification)
- `issuer`: Expected token issuer (`iss` claim)
- `audience`: Expected token audience (`aud` claim)

All three fields are required for JWT authentication to work. Incomplete configuration will be rejected.

**Optional Fields:**

- `require_auth` (default: `true`): When true, returns 401 for missing/invalid tokens
- `excluded_paths`: List of paths accessible without authentication
- `refresh_interval` (default: `15m`): How often to refresh the JWKS

Hector validates JWT tokens from the `Authorization: Bearer <token>` header using the public keys from the JWKS endpoint.

#### CLI Flags

Auth flags can be used to override or extend configuration:

```bash
# Example with auth flags
hector serve \
  --auth-jwks-url https://auth.yourdomain.com/.well-known/jwks.json \
  --auth-issuer https://auth.yourdomain.com/ \
  --auth-audience your-api-identifier \
  --provider anthropic \
  --model claude-sonnet-4

# Config mode with auth
hector serve --config agents.yaml \
  --auth-jwks-url https://auth.yourdomain.com/.well-known/jwks.json \
  --auth-issuer https://auth.yourdomain.com/ \
  --auth-audience your-api-identifier
```

**All three auth flags are required**. Providing only one or two will result in a warning and auth will be disabled:

```bash
# ❌ Incomplete - will warn and disable auth
hector serve --auth-jwks-url https://... --provider anthropic

# ✅ Complete - auth enabled
hector serve \
  --auth-jwks-url https://... \
  --auth-issuer https://... \
  --auth-audience your-api \
  --provider anthropic
```

To make auth optional (allow unauthenticated requests):

```bash
hector serve \
  --auth-jwks-url https://... \
  --auth-issuer https://... \
  --auth-audience your-api \
  --no-auth-required \
  --provider anthropic
```

#### Integration Examples

**Auth0:**

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://${AUTH0_DOMAIN}/.well-known/jwks.json
    issuer: https://${AUTH0_DOMAIN}/
    audience: ${AUTH0_AUDIENCE}
```

**Keycloak:**

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://keycloak.yourdomain.com/realms/your-realm/protocol/openid-connect/certs
    issuer: https://keycloak.yourdomain.com/realms/your-realm
    audience: hector-api
```

**AWS Cognito:**

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://cognito-idp.{region}.amazonaws.com/{userPoolId}/.well-known/jwks.json
    issuer: https://cognito-idp.{region}.amazonaws.com/{userPoolId}
    audience: {clientId}
```

### API Key Authentication

Use static API keys:

```yaml
server:
  auth:
    enabled: true
    api_keys:
      - ${API_KEY_1}
      - ${API_KEY_2}
```

Environment variables:

```bash
export API_KEY_1="key-abc123..."
export API_KEY_2="key-def456..."
```

Clients send: `Authorization: Bearer key-abc123...`

### Combined Authentication

Support both JWT and API keys:

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://auth.yourdomain.com/.well-known/jwks.json
    api_keys:
      - ${SERVICE_API_KEY}
```

Hector accepts either JWT tokens or API keys.

## Agent Visibility

Control agent discovery and access with visibility levels. **Note:** Visibility controls discovery, not access. Access control is managed by `server.auth.require_auth`.

```yaml
agents:
  # Public agent (default) - visible to all, auth controlled by server.auth.require_auth
  public_assistant:
    visibility: public
    # Visible in discovery to everyone
    # Access requires auth if server.auth.require_auth is true

  # Internal agent - visible only when authenticated
  internal_analyst:
    visibility: internal
    # Only visible in discovery when authenticated
    # Access requires authentication

  # Private agent - not exposed via HTTP
  private_helper:
    visibility: private
    # Hidden from discovery
    # Not accessible via HTTP (internal use only)
```

### Visibility Levels

**public** (default):

- Visible in agent discovery (`/agents`) to all users (authenticated or not)
- Accessible via HTTP endpoints
- **Auth enforcement:** Controlled by `server.auth.require_auth` setting
  - If `require_auth: true` → requires authentication
  - If `require_auth: false` → accessible without authentication

**internal**:

- Visible in discovery **only when authenticated**
- Hidden from unauthenticated users in discovery
- **Auth enforcement:** Always requires authentication regardless of `require_auth` setting

**private**:

- Hidden from discovery endpoint
- Not accessible via HTTP endpoints
- Only callable internally by other agents (sub-agents, agent tools)

### Visibility vs. Authentication

**Important distinction:**

- **Visibility** controls **who can see** the agent in discovery
- **Authentication** controls **who can access** the agent

Example with `require_auth: true`:

```yaml
server:
  auth:
    enabled: true
    require_auth: true  # All agents require auth by default

agents:
  assistant:
    visibility: public  # Visible in discovery to everyone
    # BUT: Still requires authentication to access (due to require_auth: true)

  admin:
    visibility: internal  # Only visible in discovery when authenticated
    # AND: Requires authentication to access
```

Example with `require_auth: false`:

```yaml
server:
  auth:
    enabled: true
    require_auth: false  # Auth is optional

agents:
  assistant:
    visibility: public  # Visible in discovery to everyone
    # AND: Accessible without authentication (require_auth: false)

  admin:
    visibility: internal  # Only visible when authenticated
    # AND: Always requires authentication (internal visibility enforces this)
```

### Example

```yaml
server:
  auth:
    enabled: true
    jwks_url: https://auth.company.com/.well-known/jwks.json
    issuer: https://auth.company.com/
    audience: company-api
    require_auth: true  # Require auth for all endpoints except excluded
    excluded_paths:
      - /health
      - /.well-known/agent-card.json

agents:
  # Customer-facing agent - visible to all, but requires auth to access
  customer_support:
    visibility: public  # Visible in discovery without auth
    instruction: Help customers with basic questions
    # Access requires authentication (due to require_auth: true)

  # Internal admin agent - visible and accessible only when authenticated
  admin_assistant:
    visibility: internal  # Only visible when authenticated
    tools: [bash, text_editor]
    instruction: Administrative tasks
    # Always requires authentication (internal visibility)

  # Backend helper - not exposed via HTTP
  data_processor:
    visibility: private  # Not accessible via HTTP
    instruction: Process data internally
    # Only callable by other agents internally
```

## Studio Mode Access Control

Studio mode allows editing agent configurations via `/api/config`. Access is controlled via CLI flags.

### Enabling Studio with RBAC

```bash
hector serve --config agents.yaml \
  --studio \
  --studio-roles admin,operator \
  --auth-jwks-url https://auth.company.com/.well-known/jwks.json \
  --auth-issuer https://auth.company.com/ \
  --auth-audience hector-api
```

| Flag | Default | Description |
|------|---------|-------------|
| `--studio` | off | Enable studio mode |
| `--studio-roles` | `operator` | Comma-separated roles allowed to access studio |

### How It Works

1. User's JWT token must contain a `role` claim
2. The role is matched against `--studio-roles`
3. If no match, access is denied (403 Forbidden)

### Server Config Immutability

> [!IMPORTANT]
> **Security: The `server:` block is immutable via studio API.**

POST to `/api/config` with a `server:` block returns 403:

```json
{
  "error": "Security: server configuration is immutable via studio API",
  "details": "Remove the 'server' block from your configuration..."
}
```

This prevents:
- Disabling authentication via config edit
- Modifying allowed roles
- Changing server ports or TLS settings

Server settings can only be changed via CLI flags or direct file editing.

## Tool Security

### Tool Approval (HITL)

Require human approval for sensitive tools:

```yaml
tools:
  text_editor:
    type: function
    handler: text_editor
    require_approval: true
    approval_prompt: "Allow file modification?"

  bash:
    type: command
    require_approval: true
    approval_prompt: "Execute: {command}?"
```

When an agent calls an approval-required tool:
1. Execution pauses
2. User receives approval request
3. User approves or denies
4. Tool executes or returns error

### Command Sandboxing

Restrict command execution:

```yaml
tools:
  bash:
    type: command
    working_directory: ./workspace
    max_execution_time: 30s
    allowed_commands:
      - git
      - npm
      - python
      - pytest
    denied_commands:
      - rm
      - dd
      - sudo
    deny_by_default: false
```

**Whitelist Mode** (recommended):

```yaml
tools:
  bash:
    type: command
    deny_by_default: true  # Deny all except allowed
    allowed_commands:
      - ls
      - cat
      - grep
```

Only whitelisted commands can execute.

### Working Directory Restriction

Limit command scope:

```yaml
tools:
  bash:
    type: command
    working_directory: ./safe-workspace
    # Commands execute only in this directory
```

### Execution Timeout

Prevent long-running commands:

```yaml
tools:
  bash:
    type: command
    max_execution_time: 30s  # Kill after 30 seconds
```

## Secret Management

### Environment Variables

Never commit secrets to configuration:

```yaml
# ✅ Good - Environment variable
llms:
  default:
    api_key: ${OPENAI_API_KEY}

# ❌ Bad - Hardcoded secret
llms:
  default:
    api_key: sk-proj-abc123...
```

### .env Files

Store secrets in `.env` (add to `.gitignore`):

```bash
# .env
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
DATABASE_PASSWORD=secret
```

Hector automatically loads `.env` files.

### Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: hector-secrets
  namespace: hector
type: Opaque
stringData:
  OPENAI_API_KEY: sk-...
  ANTHROPIC_API_KEY: sk-ant-...
  DATABASE_PASSWORD: secret
```

Reference in deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: hector
        envFrom:
        - secretRef:
            name: hector-secrets
```

### HashiCorp Vault

Use Vault for secret management:

```bash
# Retrieve secrets from Vault
export OPENAI_API_KEY=$(vault kv get -field=api_key secret/hector/openai)
```

Or use Vault Agent for injection.

## Network Security

### TLS/HTTPS

Terminate TLS at reverse proxy:

```nginx
# nginx.conf
server {
    listen 443 ssl http2;
    server_name agents.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/agents.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/agents.yourdomain.com/privkey.pem;

    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

### Kubernetes Network Policies

Restrict pod communication:

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
  # Allow database
  - to:
    - podSelector:
        matchLabels:
          app: postgres
    ports:
    - protocol: TCP
      port: 5432
  # Allow HTTPS for LLM APIs
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443
  # Allow DNS
  - to:
    - namespaceSelector: {}
      podSelector:
        matchLabels:
          k8s-app: kube-dns
    ports:
    - protocol: UDP
      port: 53
```

### CORS Configuration

Control allowed origins:

```yaml
server:
  cors:
    allowed_origins:
      - https://app.yourdomain.com
      - https://dashboard.yourdomain.com
    allowed_methods:
      - GET
      - POST
      - OPTIONS
    allowed_headers:
      - Authorization
      - Content-Type
    max_age: 86400
```

Wildcard (development only):

```yaml
server:
  cors:
    allowed_origins:
      - "*"
```

## Rate Limiting

Prevent abuse with rate limiting:

```yaml
server:
  rate_limiting:
    enabled: true
    requests_per_minute: 60
    burst: 10
```

Per-user rate limiting (with auth):

```yaml
server:
  rate_limiting:
    enabled: true
    requests_per_minute: 100
    burst: 20
    per_user: true  # Separate limits per authenticated user
```

## Audit Logging

Enable structured logging for auditing:

```yaml
logger:
  level: info
  format: json  # Structured logs for parsing
```

Logs include:

- Authentication attempts
- Agent requests
- Tool executions
- Errors

Example log:

```json
{
  "timestamp": "2025-01-15T10:30:00Z",
  "level": "info",
  "component": "auth",
  "action": "authenticate",
  "user": "user@example.com",
  "success": true
}
```

## Security Best Practices

### Principle of Least Privilege

Grant minimal permissions:

```yaml
# ✅ Good - Minimal tools
agents:
  reader:
    tools: [grep_search]

# ❌ Bad - Excessive permissions
agents:
  reader:
    tools: [text_editor, bash, web_request]
```

### Agent Isolation

Separate agents by trust level:

```yaml
agents:
  # Untrusted: public-facing, limited tools
  public_assistant:
    visibility: public
    tools: [search]

  # Trusted: internal, more tools
  internal_assistant:
    visibility: internal
    tools: [search, text_editor, grep_search]

  # Privileged: admin only, all tools
  admin_assistant:
    visibility: internal
    tools: [bash, text_editor, web_request]
```

### Tool Whitelisting

Use explicit whitelists:

```yaml
# ✅ Good - Explicit whitelist
tools:
  bash:
    deny_by_default: true
    allowed_commands: [ls, cat, grep]

# ❌ Bad - Blacklist (incomplete)
tools:
  bash:
    deny_by_default: false
    denied_commands: [rm]  # Many dangerous commands not listed
```

### Secrets Rotation

Rotate secrets regularly:

```bash
# Generate new API key
new_key=$(generate_api_key)

# Update Kubernetes secret
kubectl create secret generic hector-secrets \
  --from-literal=API_KEY=$new_key \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart pods to pick up new secret
kubectl rollout restart deployment/hector -n hector
```

### Input Validation

Validate inputs in custom tools:

```go
func ValidatePath(path string) error {
    // Prevent path traversal
    if strings.Contains(path, "..") {
        return errors.New("path traversal not allowed")
    }
    // Restrict to workspace
    if !strings.HasPrefix(path, "/workspace/") {
        return errors.New("path must be within workspace")
    }
    return nil
}
```

## Production Security Checklist

- [ ] Enable authentication (JWT or API keys)
- [ ] Use agent visibility controls
- [ ] Require approval for destructive tools
- [ ] Enable command sandboxing with whitelists
- [ ] Store secrets in environment variables or vault
- [ ] Use TLS/HTTPS (via reverse proxy)
- [ ] Configure network policies (Kubernetes)
- [ ] Set up CORS restrictions
- [ ] Enable rate limiting
- [ ] Use structured logging for auditing
- [ ] Implement secrets rotation
- [ ] Regular security updates
- [ ] Monitor for suspicious activity

## Example: Secure Production Setup

```yaml
# config.yaml
version: "2"

llms:
  default:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}  # From Kubernetes secret

tools:
  bash:
    type: command
    working_directory: ./workspace
    max_execution_time: 30s
    deny_by_default: true
    allowed_commands: [git, npm, python, pytest]
    require_approval: true

  text_editor:
    type: function
    handler: text_editor
    require_approval: true

  grep_search:
    type: function
    handler: grep_search
    # No approval needed for read-only

agents:
  # Public agent: minimal tools
  public_assistant:
    visibility: public
    llm: default
    tools: [search]

  # Internal agent: more tools
  internal_assistant:
    visibility: internal
    llm: default
    tools: [search, grep_search, text_editor]
    document_stores: [internal_docs]

  # Admin agent: full access
  admin_assistant:
    visibility: internal
    llm: default
    tools: [bash, text_editor, grep_search]

server:
  port: 8080
  auth:
    enabled: true
    jwks_url: https://auth.company.com/.well-known/jwks.json
    issuer: https://auth.company.com/
    audience: company-api
    require_auth: true
    excluded_paths:
      - /health
      - /.well-known/agent-card.json
  cors:
    allowed_origins:
      - https://app.company.com
  rate_limiting:
    enabled: true
    requests_per_minute: 100
    per_user: true
  observability:
    metrics:
      enabled: true
    tracing:
      enabled: true
      endpoint: jaeger-collector:4317

logger:
  level: info
  format: json
```



