# Agentry

A federated, asynchronous communication gateway implementing the Agent Message Transfer Protocol (AMTP) v1.0.

## Overview

Agentry provides reliable agent-to-agent communication across organizational boundaries with native support for structured data, multi-agent coordination, and guaranteed delivery semantics.

## Features

### Core Capabilities
- **Universal Addressing**: `agent@domain` format with DNS-based discovery
- **Message Validation**: Comprehensive AMTP message structure validation
- **Protocol Compliance**: Full AMTP v1.0 specification implementation
- **Federated Architecture**: Decentralized communication across domains
- **Local Agent Management**: Register and manage local agents with pull/push delivery modes
- **Schema Integration**: AGNTCY framework support for structured data
- **Multi-Agent Coordination**: Workflow management and orchestration
- **Security**: TLS 1.3, digital signatures, and access control
- **Reliability**: At-least-once delivery with idempotency guarantees

### Technical Features
- **HTTP/HTTPS Transport**: Modern REST API with TLS 1.3 support
- **DNS Discovery**: Automatic capability discovery via DNS TXT records
- **Message Types**: Support for simple, schema-validated, and coordinated messages
- **Dual Delivery Modes**: Pull-based inbox storage and push-based webhook delivery
- **Agent Registry**: Dynamic registration and management of local agents
- **Attachments**: External file reference handling
- **Admin Tools**: Command-line interface for agent and schema management
- **Inbox Security**: API key-based access control for agent inboxes
- **Monitoring**: Health checks, metrics, and structured logging
- **Development Tools**: Local testing, Docker support, comprehensive CI/CD

## Quick Start

### Prerequisites
- Go 1.21 or later
- Docker (optional)

### Installation

```bash
# Clone the repository
git clone https://github.com/amtp-protocol/agentry.git
cd agentry

# Install dependencies
go mod download

# Build the binary
make build

# Run the gateway (uses default configuration)
./build/agentry

# Or run with custom configuration
./build/agentry -config config/config.example.yaml
```

### Local Development

For local testing and development:

```bash
# Option 1: Use development script (easiest)
./local-dev.sh

# Option 2: Manual configuration
AMTP_TLS_ENABLED=false AMTP_SERVER_ADDRESS=:8080 AMTP_DOMAIN=localhost ./build/agentry

# Option 3: Docker development
docker-compose -f docker-compose.dev.yml up --build
```

The server will be available at `http://localhost:8080` with debug logging enabled.

> **📖 For comprehensive local testing guide, see [docs/LOCAL_TESTING.md](docs/TESTING.md)**

#### Testing the API

Use localhost and test domains for local development:

```bash
# Health check
curl http://localhost:8080/health

# Register a local agent for pull mode (returns API key)
# Use just the agent name - domain will be auto-added
curl -X POST http://localhost:8080/v1/admin/agents \
  -H "Content-Type: application/json" \
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
curl -H "Authorization: Bearer Kx7vR9wQ2mP8sL3nF6jH4tY1uE5oA9cB2dG8hK0mN7pS4vW6xZ3q" \
     http://localhost:8080/v1/inbox/user@localhost

# List registered agents
curl http://localhost:8080/v1/admin/agents

# Send to test domain (will fail gracefully for testing)
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test@localhost",
    "recipients": ["user@test.com"],
    "subject": "Test Discovery Failure",
    "payload": {"message": "Testing graceful failure"}
  }'

# Check message status (replace MESSAGE_ID)
curl http://localhost:8080/v1/messages/MESSAGE_ID/status
```

**⚠️ Important**: Use `localhost`, `test.com`, or `example.com` domains for local testing. Avoid real domains like `gmail.com` as they will fail DNS discovery.

#### Troubleshooting

**Common Issues:**
- **"TLS cert and key files are required"**: Set `AMTP_TLS_ENABLED=false`
- **"bind: address already in use"**: Change port with `AMTP_SERVER_ADDRESS=:8081`
- **"connection refused"**: Ensure server is running on correct port

**Debug Mode:**
```bash
export AMTP_LOG_LEVEL=debug
export AMTP_LOG_FORMAT=text
```

### Configuration

#### Command Line Options

The Agentry gateway accepts the following optional command line flags:

```bash
./build/agentry [OPTIONS]

Options:
  -config string
        Path to configuration file (YAML) - optional
  -admin-key-file string
        Path to admin API key file - optional
```

**Examples:**

```bash
# Run with default configuration (environment variables only)
./build/agentry

# Run with custom config file
./build/agentry -config /path/to/config.yaml

# Run with admin key file for admin API access
./build/agentry -admin-key-file /path/to/admin.keys

# Run with both config file and admin key file
./build/agentry -config /path/to/config.yaml -admin-key-file /path/to/admin.keys
```

**Configuration Priority (highest to lowest):**
1. Command line flags (`-admin-key-file`)
2. Environment variables
3. Configuration file (if specified with `-config`)
4. Default values

> **Note**: If no `-config` flag is provided, the gateway will use default configuration values combined with any environment variable overrides. The configuration file is completely optional.

#### Environment Variables

##### Server Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_SERVER_ADDRESS` | `:8443` | Server bind address |
| `AMTP_DOMAIN` | `localhost` | Gateway domain |
| `AMTP_READ_TIMEOUT` | `30s` | HTTP read timeout |
| `AMTP_WRITE_TIMEOUT` | `30s` | HTTP write timeout |
| `AMTP_IDLE_TIMEOUT` | `120s` | HTTP idle timeout |

##### TLS Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_TLS_ENABLED` | `true` | Enable/disable TLS |
| `AMTP_TLS_CERT_FILE` | - | Path to TLS certificate file |
| `AMTP_TLS_KEY_FILE` | - | Path to TLS private key file |
| `AMTP_TLS_MIN_VERSION` | `1.3` | Minimum TLS version (1.2, 1.3) |

##### DNS Discovery Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_DNS_CACHE_TTL` | `5m` | DNS cache TTL duration |
| `AMTP_DNS_TIMEOUT` | `5s` | DNS query timeout |
| `AMTP_DNS_MOCK_MODE` | `false` | Enable mock DNS for testing |
| `AMTP_DNS_ALLOW_HTTP` | `false` | Allow HTTP gateway URLs ⚠️ **Development only** |
| `AMTP_DNS_MOCK_RECORDS` | - | Custom mock DNS records (JSON format) |

##### Message Processing Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_MESSAGE_MAX_SIZE` | `10485760` | Max message size in bytes (10MB) |
| `AMTP_MESSAGE_VALIDATION_ENABLED` | `true` | Enable message validation |
| `AMTP_IDEMPOTENCY_TTL` | `168h` | Idempotency cache TTL (7 days) |

##### Authentication Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_AUTH_REQUIRED` | `false` | Require authentication |
| `AMTP_AUTH_API_KEY_HEADER` | `X-API-Key` | API key header name |
| `AMTP_ADMIN_KEY_FILE` | - | Path to admin API key file (can also be set via `-admin-key-file` flag) |
| `AMTP_ADMIN_API_KEY_HEADER` | `X-Admin-Key` | Header name for admin API authentication |

##### Logging Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `AMTP_LOG_FORMAT` | `json` | Log format (json, text) |

##### Metrics Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_METRICS_ENABLED` | `false` | Enable JSON metrics collection and `/metrics` endpoint |

##### Schema Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_SCHEMA_REGISTRY_TYPE` | - | Schema registry type (set to `local` to enable) |
| `AMTP_SCHEMA_REGISTRY_PATH` | - | Path to local schema registry directory |
| `AMTP_SCHEMA_USE_LOCAL_REGISTRY` | `false` | Enable local schema registry (alternative to setting type) |

> ⚠️ **Security Note**: Variables marked with ⚠️ should only be used in development environments. Never enable `AMTP_DNS_ALLOW_HTTP=true` in production as it allows insecure HTTP gateway URLs.

#### Production Configuration

```bash
# Server configuration
export AMTP_SERVER_ADDRESS=":8443"
export AMTP_DOMAIN="your-domain.com"
export AMTP_READ_TIMEOUT="30s"
export AMTP_WRITE_TIMEOUT="30s"
export AMTP_IDLE_TIMEOUT="120s"

# TLS configuration
export AMTP_TLS_ENABLED=true
export AMTP_TLS_CERT_FILE="/path/to/cert.pem"
export AMTP_TLS_KEY_FILE="/path/to/key.pem"
export AMTP_TLS_MIN_VERSION="1.3"

# DNS configuration
export AMTP_DNS_CACHE_TTL="5m"
export AMTP_DNS_TIMEOUT="5s"
# Note: AMTP_DNS_ALLOW_HTTP should remain false (default) for security

# Message configuration
export AMTP_MESSAGE_MAX_SIZE=10485760  # 10MB
export AMTP_MESSAGE_VALIDATION_ENABLED=true
export AMTP_IDEMPOTENCY_TTL="168h"  # 7 days

# Authentication configuration
export AMTP_AUTH_REQUIRED=false
export AMTP_AUTH_API_KEY_HEADER="X-API-Key"
export AMTP_ADMIN_KEY_FILE="/etc/agentry/admin.keys"  # Optional: for admin API access

# Logging
export AMTP_LOG_LEVEL=info
export AMTP_LOG_FORMAT=json

# Metrics (optional - enable for monitoring)
export AMTP_METRICS_ENABLED=true

# Schema management (optional - enable for schema validation)
export AMTP_SCHEMA_REGISTRY_TYPE=local
export AMTP_SCHEMA_REGISTRY_PATH="/var/lib/agentry/schemas"
```

#### Development Configuration

For local development and testing:

```bash
# Server configuration (HTTP for local development)
export AMTP_SERVER_ADDRESS=":8080"
export AMTP_DOMAIN="localhost"
export AMTP_TLS_ENABLED=false

# DNS configuration (enable mock DNS and HTTP for testing)
export AMTP_DNS_MOCK_MODE=true
export AMTP_DNS_ALLOW_HTTP=true

# Message configuration
export AMTP_MESSAGE_VALIDATION_ENABLED=true

# Authentication (disabled for easier testing)
export AMTP_AUTH_REQUIRED=false

# Logging (verbose for development)
export AMTP_LOG_LEVEL=debug
export AMTP_LOG_FORMAT=text

# Schema management (optional - enable for schema validation)
export AMTP_SCHEMA_REGISTRY_TYPE=local
export AMTP_SCHEMA_REGISTRY_PATH="/tmp/schemas"
# Alternative: export AMTP_SCHEMA_USE_LOCAL_REGISTRY=true
```

> 📝 **Development Note**: The development script `./scripts/local-dev.sh` automatically sets these variables for you.

### Docker

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

## API Reference

### Core Messaging

#### Send Message

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

#### Query Message Status

```http
GET /v1/messages/{message_id}/status
```

#### List Messages

```http
GET /v1/messages
```

#### Get Message Details

```http
GET /v1/messages/{message_id}
```

### Local Agent Management

**Authentication**: All agent management endpoints require admin authentication.

#### Register Local Agent

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

#### List Local Agents

```http
GET /v1/admin/agents
```

#### Unregister Local Agent

```http
DELETE /v1/admin/agents/{agent_address}
```

### Inbox Management (Pull Mode)

#### Get Inbox Messages

```http
GET /v1/inbox/{recipient}
Authorization: Bearer {agent_api_key}
```

**Security**: Requires the agent's API key. Each agent can only access their own inbox.

#### Acknowledge Message

```http
DELETE /v1/inbox/{recipient}/{message_id}
Authorization: Bearer {agent_api_key}
```

**Security**: Requires the agent's API key. Each agent can only acknowledge their own messages.

### Discovery & Health

#### Discover Domain Capabilities

```http
GET /v1/capabilities/{domain}
```

#### Health Check

```http
GET /health
GET /ready
```

#### Metrics (optional)

```http
GET /metrics
```

**Metrics Endpoint** - Available when `AMTP_METRICS_ENABLED=true`:
- Exposes JSON metrics for monitoring
- Includes HTTP request metrics, message processing metrics, and system metrics
- Secured by the same authentication as other endpoints

**Health Check (`/health`)** - Liveness Probe:
- Verifies that all core components are initialized
- Returns HTTP 200 if healthy, HTTP 503 if unhealthy
- Checks: router, message processor, agent registry, discovery service, schema manager

**Readiness Check (`/ready`)** - Readiness Probe:
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

### Schema Management

**Authentication**: All schema management endpoints require admin authentication.

#### Register Schema

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

#### List Schemas

```http
GET /v1/admin/schemas
GET /v1/admin/schemas?pattern=agntcy:test.*
```

#### Get Schema

```http
GET /v1/admin/schemas/{schema_id}
```

#### Update Schema

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

#### Delete Schema

```http
DELETE /v1/admin/schemas/{schema_id}
```

#### Validate Payload Against Schema

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

#### Get Schema Statistics

```http
GET /v1/admin/schemas/stats
```

Returns statistics about the schema registry including total schema count, schemas by domain, and schemas by entity type.

### Discovery Endpoints

#### Agent Discovery

```http
GET /v1/discovery/agents
GET /v1/discovery/agents/{domain}
```

Discover registered agents for this domain (or a specific domain). Supports filtering by delivery mode and active status.

## Security

### Agent Inbox Protection

The AMTP Gateway implements API key-based access control for agent inboxes:

- **Automatic Key Generation**: Each registered agent receives a unique, cryptographically secure API key
- **Agent Isolation**: Agents can only access their own inbox using their specific API key
- **Secure Authentication**: API keys use 256-bit entropy with constant-time comparison to prevent timing attacks
- **Access Tracking**: Last access timestamps are recorded for audit purposes

### API Key Management

```bash
# Register agent (API key returned in response)
curl -X POST http://localhost:8080/v1/admin/agents \
  -H "Content-Type: application/json" \
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

## DNS Configuration

To enable AMTP for your domain, add a DNS TXT record:

```dns
_amtp.yourdomain.com. IN TXT "v=amtp1;gateway=https://amtp.yourdomain.com:443"
```

## Development

### Building

```bash
# Build gateway for current platform
make build

# Build admin tool
make build-admin

# Build both gateway and admin tool
make build-all

# Build for specific platform
make build-linux
make build-darwin
make build-windows
```

### Admin Tool

The `agentry-admin` tool provides command-line management for agents, schemas, and inbox operations:

```bash
# Build admin tool
make build-admin

# Agent management (API keys are generated automatically)
./build/agentry-admin agent register user --mode pull
./build/agentry-admin agent register api-service --mode push --target http://api:8080/webhook
./build/agentry-admin agent list
./build/agentry-admin agent unregister user

# Inbox management (requires API key for security)
./build/agentry-admin inbox get user@localhost --key your-api-key
./build/agentry-admin inbox get user@localhost --key-file user.key
./build/agentry-admin inbox ack user@localhost message-id-123 --key your-api-key

# Schema management
./build/agentry-admin schema register agntcy:test.v1 -f schema.json
./build/agentry-admin schema list
./build/agentry-admin schema get agntcy:test.v1
./build/agentry-admin schema delete agntcy:test.v1
./build/agentry-admin schema validate agntcy:test.v1 -f payload.json
./build/agentry-admin schema stats
```

For complete documentation, see [cmd/agentry-admin/README.md](cmd/agentry-admin/README.md).

### Testing

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Run benchmarks
make benchmark
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run security scan
make security-scan

# Run all checks
make ci
```

### Development Environment Setup

```bash
# Setup development environment
make setup

# Run in development mode
make dev
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    AMTP Gateway                             │
├─────────────────┬─────────────────┬─────────────────────────┤
│  HTTP Server    │  Message Queue  │    Protocol Bridge     │
│  - Receive      │  - Persistence  │    - AMTP ↔ SMTP       │
│  - Send         │  - Retry Logic  │    - Schema Conversion  │
│  - Status API   │  - DLQ          │    - Format Translation │
├─────────────────┼─────────────────┼─────────────────────────┤
│  DNS Resolver   │  Schema Engine  │    Coordination Engine  │
│  - Discovery    │  - Validation   │    - Workflow State     │
│  - Caching      │  - AGNTCY API   │    - Multi-Agent Logic  │
├─────────────────┼─────────────────┼─────────────────────────┤
│  Agent Registry │  Delivery Engine│    Local Inbox         │
│  - Registration │  - Push Mode    │    - Pull Mode Storage  │
│  - Configuration│  - Webhook HTTP │    - Message Queuing    │
│  - Management   │  - Headers      │    - Acknowledgment     │
├─────────────────┼─────────────────┼─────────────────────────┤
│  Auth Manager   │  Policy Engine  │    Monitoring          │
│  - TLS Certs    │  - Access Rules │    - Metrics           │
│  - API Keys     │  - Rate Limits  │    - Logging           │
└─────────────────┴─────────────────┴─────────────────────────┘
```

## Protocol Specification

This implementation follows the [AMTP Protocol Specification v1.0](./amtp_protocol_spec.md).

Key features:
- Universal addressing using `agent@domain` format
- Transparent protocol upgrade with SMTP bridging
- At-least-once delivery with idempotency guarantees
- Local agent management with pull/push delivery modes
- Standard schema integration via AGNTCY framework
- Multi-agent workflow coordination
- Federated architecture with DNS-based discovery

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Guidelines

- Follow Go best practices and idioms
- Write comprehensive tests for new features
- Update documentation for API changes
- Run `make ci` before submitting PRs
- Follow the existing code style and patterns

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Roadmap

See [ROADMAP.md](./ROADMAP.md) for detailed development timeline and feature planning.

## Support

- 📖 [Documentation](./docs/)
- 🐛 [Issue Tracker](https://github.com/amtp-protocol/agentry/issues)
- 💬 [Discussions](https://github.com/amtp-protocol/agentry/discussions)

## Acknowledgments

- Built with [Gin](https://github.com/gin-gonic/gin) HTTP framework
- Follows [AGNTCY](https://agntcy.org) schema standards
- Implements federated messaging patterns inspired by email protocols
