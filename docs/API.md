# API Reference

## Authentication Overview

| Endpoints | Required credential |
|-----------|---------------------|
| `POST /v1/messages`, `/health`, `/ready`, `/v1/capabilities`, `/v1/discovery` | None |
| `GET /v1/messages...` | Agent API key or admin key |
| `/v1/inbox/...` | Agent API key (own inbox only) |
| `/v1/admin/...` | Admin key |

The table describes the default setup. With `AMTP_AUTH_REQUIRED=true` the gateway-wide auth middleware additionally applies to every endpoint.

- **Agent API key**: `Authorization: Bearer <agent-api-key>`, returned once when the agent is registered.
- **Admin key**: `X-Admin-Key` header (name configurable via `AMTP_ADMIN_API_KEY_HEADER`), loaded from the admin key file. Until an admin key file is configured, every `/v1/admin` request is refused with `401 ADMIN_AUTH_NOT_CONFIGURED`. See [CONFIGURATION.md](CONFIGURATION.md).

## Core Messaging

### Send Message

```http
POST /v1/messages
Content-Type: application/json

{
  "sender": "agent@sender.com",
  "recipients": ["agent@receiver.com"],
  "subject": "Test Message",
  "schema": "agntcy:test.message.v1",
  "payload": {
    "text": "Hello, World!"
  }
}
```

### Query Message Status

```http
GET /v1/messages/{message_id}/status
```

### List Messages

```http
GET /v1/messages
```

### Get Message Details

```http
GET /v1/messages/{message_id}
```

### Message Query Authentication

`GET /v1/messages`, `GET /v1/messages/{message_id}` and
`GET /v1/messages/{message_id}/status` are **not public**. Each requires one of:

- an **agent API key** (`Authorization: Bearer <agent-api-key>`): the
  caller is scoped to messages the agent sent or received; or
- the **gateway admin key** (`X-Admin-Key` header): the admin may inspect
  any message.

`POST /v1/messages` remains **public by design** (AMTP is a federated
protocol where remote senders have no local key). Practical consequence: a
client that submits a message but holds no registered local agent key cannot
poll the delivery status of its own message. It must present a registered
agent's key or the admin key to do so.

## Local Agent Management

**Authentication**: All agent management endpoints require admin authentication.

### Register Local Agent

```http
POST /v1/admin/agents
Content-Type: application/json

{
  "address": "agent@localhost",
  "delivery_mode": "push",
  "push_target": "http://agent-service:8080/webhook",
  "headers": {
    "Authorization": "Bearer token",
    "X-Agent-ID": "agent-service"
  }
}
```

### List Local Agents

```http
GET /v1/admin/agents
```

### Unregister Local Agent

```http
DELETE /v1/admin/agents/{agent_address}
```

### Rotate Agent API Key

```http
POST /v1/admin/agents/{agent_address}/rotate-key
```

Returns the new `api_key`; the previous key stops working.

## Inbox Management (Pull Mode)

### Get Inbox Messages

```http
GET /v1/inbox/{recipient}
Authorization: Bearer {agent_api_key}
```

**Security**: Requires the agent's API key. Each agent can only access their own inbox.

### Acknowledge Message

```http
DELETE /v1/inbox/{recipient}/{message_id}
Authorization: Bearer {agent_api_key}
```

**Security**: Requires the agent's API key. Each agent can only acknowledge their own messages.

## Discovery

### Discover Domain Capabilities

```http
GET /v1/capabilities/{domain}
```

### Agent Discovery

```http
GET /v1/discovery/agents
GET /v1/discovery/agents/{domain}
```

Discover registered agents for this domain (or a specific domain). Supports filtering by delivery mode and active status.

## Health & Metrics

### Health Check

```http
GET /health
GET /ready
```

**Health Check (`/health`)**, liveness probe:
- Verifies that all core components are initialized
- Returns HTTP 200 if healthy, HTTP 503 if unhealthy
- Checks: router, message processor, agent registry, discovery service, schema manager

**Readiness Check (`/ready`)**, readiness probe:
- Verifies that all dependencies are functional and ready to serve requests
- Returns HTTP 200 if ready, HTTP 503 if not ready
- Tests actual functionality of agent registry, schema manager, and other services

**Example Responses:**

```json
// GET /health - Healthy
{
  "status": "healthy",
  "healthy": true,
  "timestamp": "2024-01-15T10:30:00Z",
  "version": "1.0",
  "components": {
    "router": "healthy",
    "message_processor": "healthy",
    "agent_registry": "healthy",
    "discovery_service": "healthy",
    "schema_manager": "healthy"
  }
}

// GET /ready - Ready
{
  "status": "ready",
  "ready": true,
  "timestamp": "2024-01-15T10:30:00Z",
  "version": "1.0",
  "dependencies": {
    "agent_registry": "ready",
    "schema_manager": "ready",
    "discovery_service": "ready",
    "message_processor": "ready",
    "validator": "ready"
  }
}
```

### Metrics (optional)

```http
GET /metrics
```

Available when `AMTP_METRICS_ENABLED=true`:
- Exposes JSON metrics for monitoring
- Includes HTTP request metrics, message processing metrics, and system metrics
- Secured by the same authentication as other endpoints

## Schema Management

**Authentication**: All schema management endpoints require admin authentication. See [SCHEMA.md](SCHEMA.md) for the schema framework.

### Register Schema

```http
POST /v1/admin/schemas
Content-Type: application/json

{
  "id": "agntcy:test.message.v1",
  "definition": {
    "type": "object",
    "properties": {
      "text": {"type": "string"},
      "timestamp": {"type": "string", "format": "date-time"}
    },
    "required": ["text"]
  }
}
```

### List Schemas

```http
GET /v1/admin/schemas
GET /v1/admin/schemas?pattern=agntcy:test.*
```

### Get Schema

```http
GET /v1/admin/schemas/{schema_id}
```

### Update Schema

```http
PUT /v1/admin/schemas/{schema_id}
Content-Type: application/json

{
  "definition": {
    "type": "object",
    "properties": {
      "text": {"type": "string"},
      "timestamp": {"type": "string", "format": "date-time"},
      "priority": {"type": "integer", "minimum": 1, "maximum": 5}
    },
    "required": ["text"]
  }
}
```

### Delete Schema

```http
DELETE /v1/admin/schemas/{schema_id}
```

### Validate Payload Against Schema

```http
POST /v1/admin/schemas/{schema_id}/validate
Content-Type: application/json

{
  "payload": {
    "text": "Hello, World!",
    "timestamp": "2024-01-15T10:30:00Z"
  }
}
```

### Get Schema Statistics

```http
GET /v1/admin/schemas/stats
```

Returns statistics about the schema registry including total schema count, schemas by domain, and schemas by entity type.

## Security

### Agent Inbox Protection

The gateway implements API key-based access control for agent inboxes:

- **Automatic Key Generation**: Each registered agent receives a unique, cryptographically secure API key
- **Agent Isolation**: Agents can only access their own inbox using their specific API key
- **Secure Authentication**: API keys use 256-bit entropy with constant-time comparison to prevent timing attacks
- **Access Tracking**: Last access timestamps are recorded for audit purposes

### API Key Management

```bash
# Register agent (API key returned in response)
curl -X POST http://localhost:8080/v1/admin/agents \
  -H "Content-Type: application/json" \
  -H "X-Admin-Key: your-admin-key" \
  -d '{"address": "user", "delivery_mode": "pull"}'

# Access inbox with API key
curl -H "Authorization: Bearer your-api-key" \
     http://localhost:8080/v1/inbox/user@localhost

# Using admin tool with key file
echo "your-api-key" > user.key
./build/agentry-admin inbox get user@localhost --key-file user.key
```

**⚠️ Security Best Practices:**
- Store API keys securely (environment variables, key files with restricted permissions)
- Never log or expose API keys in plain text
- Rotate API keys periodically using the admin tool
- Use HTTPS in production to protect API keys in transit
