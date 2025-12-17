# Hector Studio

Rich desktop environment for developing, testing, and managing Hector agents.

## Overview

**Hector Studio** is a standalone desktop application that connects to your running Hector server to provide a visual development environment.

It features:

- **Visual Config Editor**: Edit YAML configurations with schema validation and autocomplete.
- **Chat Interface**: Test your agents with a rich chat UI.
- **Real-Time Validation**: Immediate feedback on configuration errors.
- **Multiple Servers**: Manage and switch between multiple local or remote Hector instances.

## Enabling Studio Access

To connect Hector Studio to your server, you must start `hector` with the `--studio` flag. This enables the necessary API endpoints.

> [!IMPORTANT]
> Studio mode **requires** a configuration file. It cannot be used with zero-config mode.

```bash
# Start server with studio API enabled
hector serve --config agents.yaml --studio
```

## Security & Authentication

> [!CAUTION]
> **Security Warning**: Studio Mode enables remote configuration editing.
> **DO NOT** enable this in production unless protected by authentication.

If you are running Hector remotely, you should secure the Studio API using JWT authentication and role-based access control (RBAC).

```bash
hector serve --config agents.yaml \
  --studio \
  --studio-roles admin,operator \
  --auth-jwks-url https://auth.company.com/.well-known/jwks.json \
  --auth-issuer https://auth.company.com/ \
  --auth-audience hector-api
```

This ensures only authorized users can connect via Hector Studio.

## Connecting

1. Launch **Hector Studio**.
2. Click **Add Server**.
3. Enter your server URL (e.g., `http://localhost:8080`).
4. If authentication is enabled, you will be prompted to log in.

## Features

### Configuration Editor
Edit your `config.yaml` with confidence. The Studio provides validation and schema support, ensuring your configuration is correct before it's applied.

### Chat & Testing
Interact with your agents directly. The chat interface supports streaming responses, tool execution visualization, and rich markdown rendering.

### Watch Mode
When you save changes in Hector Studio, they are automatically applied to the running server (if the server supports hot-reload).

## Troubleshooting

### Connection Failed
- Ensure `hector serve` is running.
- Verify `--studio` flag is present.
- Check network connectivity and port accessibility.

### Login Issues
- Verify `auth-issuer` and `auth-client-id` settings match your identity provider.
- Ensure your user has one of the roles specified in `--studio-roles`.
