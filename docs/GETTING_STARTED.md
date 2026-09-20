# Getting Started

This guide walks through running a local gateway and exchanging a first message.

## Prerequisites

- Go 1.21 or later
- Docker (optional)

## Build

```bash
git clone https://github.com/amtp-protocol/agentry.git
cd agentry
go mod download
make build
```

## Run Locally

The admin API (`/v1/admin`) is refused until an admin key file is configured, so create one first. The file holds one key per line; empty lines and `#` comments are ignored.

```bash
openssl rand -hex 32 > admin.keys
chmod 600 admin.keys
export ADMIN_KEY=$(cat admin.keys)

AMTP_TLS_ENABLED=false AMTP_SERVER_ADDRESS=:8080 AMTP_DOMAIN=localhost \
  ./build/agentry -admin-key-file admin.keys
```

Alternatives:

```bash
# Development script (debug logging, mock DNS and HTTP gateways enabled).
# It does not set an admin key, so pass one through the environment.
AMTP_ADMIN_KEY_FILE=$PWD/admin.keys ./scripts/local-dev.sh

# Docker development environment
docker-compose -f docker/docker-compose.dev.yml up --build
```

The server is available at `http://localhost:8080`. All settings are described in [CONFIGURATION.md](CONFIGURATION.md). For mock DNS and the other local testing scenarios, see [LOCAL_TESTING.md](LOCAL_TESTING.md).

## Send Your First Message

```bash
# Health check
curl http://localhost:8080/health

# Register a local agent for pull mode (returns API key)
# Use just the agent name - domain will be auto-added
curl -X POST http://localhost:8080/v1/admin/agents \
  -H "Content-Type: application/json" \
  -H "X-Admin-Key: $ADMIN_KEY" \
  -d '{
    "address": "user",
    "delivery_mode": "pull"
  }'

# Response includes API key for secure inbox access:
# {
#   "message": "Agent registered successfully",
#   "agent": {
#     "address": "user@localhost",
#     "delivery_mode": "pull",
#     "api_key": "Kx7vR9wQ2mP8sL3nF6jH4tY1uE5oA9cB2dG8hK0mN7pS4vW6xZ3q"
#   }
# }
export AGENT_KEY=<api_key from the response>

# Send a local message
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test@localhost",
    "recipients": ["user@localhost"],
    "subject": "Local Test Message",
    "payload": {"message": "Hello localhost!"}
  }'

# Check inbox for received messages (requires API key)
curl -H "Authorization: Bearer $AGENT_KEY" \
     http://localhost:8080/v1/inbox/user@localhost

# List registered agents
curl -H "X-Admin-Key: $ADMIN_KEY" http://localhost:8080/v1/admin/agents

# Send to test domain (will fail gracefully for testing)
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test@localhost",
    "recipients": ["user@test.com"],
    "subject": "Test Discovery Failure",
    "payload": {"message": "Testing graceful failure"}
  }'

# Check message status (replace MESSAGE_ID; requires the agent API key or
# the admin key, see "Message Query Authentication" in API.md)
curl -H "Authorization: Bearer $AGENT_KEY" \
     http://localhost:8080/v1/messages/MESSAGE_ID/status
```

**⚠️ Important**: Use `localhost`, `test.com`, or `example.com` domains for local testing. Avoid real domains like `gmail.com` as they will fail DNS discovery.

The full endpoint list is in [API.md](API.md).

## Docker

```bash
# Build Docker image
make docker-build

# Run with Docker
docker run -p 8443:8443 \
  -e AMTP_DOMAIN=your-domain.com \
  -e AMTP_TLS_ENABLED=false \
  agentry:latest

# Run with Docker including schema management
docker run -p 8443:8443 \
  -e AMTP_DOMAIN=your-domain.com \
  -e AMTP_TLS_ENABLED=false \
  -e AMTP_SCHEMA_REGISTRY_TYPE=local \
  -e AMTP_SCHEMA_REGISTRY_PATH=/app/schemas \
  -v $(pwd)/schemas:/app/schemas \
  agentry:latest

# Run with custom config file and admin keys
docker run -p 8443:8443 \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/keys:/app/keys \
  agentry:latest -config /app/config/production.yaml -admin-key-file /app/keys/admin.keys
```

For production deployments (systemd, multi-domain, Kubernetes), see [DEPLOYMENT.md](DEPLOYMENT.md).

## DNS Setup

To enable AMTP for your domain, add a DNS TXT record:

```dns
_amtp.yourdomain.com. IN TXT "v=amtp1;gateway=https://amtp.yourdomain.com:443"
```

See [DOMAIN_MANAGEMENT.md](DOMAIN_MANAGEMENT.md) for details.

## Troubleshooting

- **"TLS cert and key files are required"**: Set `AMTP_TLS_ENABLED=false`
- **"bind: address already in use"**: Change port with `AMTP_SERVER_ADDRESS=:8081`
- **"connection refused"**: Ensure server is running on correct port
- **`401 ADMIN_AUTH_NOT_CONFIGURED`**: Start the gateway with `-admin-key-file` or `AMTP_ADMIN_KEY_FILE`

Debug mode:

```bash
export AMTP_LOG_LEVEL=debug
export AMTP_LOG_FORMAT=text
```
