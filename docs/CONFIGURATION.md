# Configuration

Agentry is configured through command line flags, environment variables, and an optional YAML file. Agents and schemas are managed at runtime with the [admin tool](#admin-tool). See [config/config.example.yaml](../config/config.example.yaml) for a complete file example.

## Command Line Options

```bash
./build/agentry [OPTIONS]

Options:
  -config string
        Path to configuration file (YAML) - optional
  -admin-key-file string
        Path to admin API key file - required for the /v1/admin API
```

> **Note**: The gateway refuses every `/v1/admin` request with `401
> ADMIN_AUTH_NOT_CONFIGURED` until an admin key file is configured. Those
> endpoints register agents and hand back plaintext API keys, so they are
> never served anonymously. When `auth.require_auth` is enabled the gateway
> goes further and refuses to start without `auth.admin_key_file`, so the
> misconfiguration surfaces at deployment rather than as 401s later.

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

## Configuration Priority

From highest to lowest:

1. Command line flags (`-admin-key-file`)
2. Environment variables
3. Configuration file (if specified with `-config`)
4. Default values

If no `-config` flag is provided, the gateway uses default values combined with any environment variable overrides. The configuration file is completely optional.

## Environment Variables

### Server
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_SERVER_ADDRESS` | `:8443` | Server bind address |
| `AMTP_DOMAIN` | `localhost` | Gateway domain |
| `AMTP_READ_TIMEOUT` | `30s` | HTTP read timeout |
| `AMTP_WRITE_TIMEOUT` | `30s` | HTTP write timeout |
| `AMTP_IDLE_TIMEOUT` | `120s` | HTTP idle timeout |

### TLS
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_TLS_ENABLED` | `true` | Enable/disable TLS |
| `AMTP_TLS_CERT_FILE` | - | Path to TLS certificate file |
| `AMTP_TLS_KEY_FILE` | - | Path to TLS private key file |
| `AMTP_TLS_MIN_VERSION` | `1.3` | Minimum TLS version (1.2, 1.3) |

### DNS Discovery
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_DNS_CACHE_TTL` | `5m` | DNS cache TTL duration |
| `AMTP_DNS_TIMEOUT` | `5s` | DNS query timeout |
| `AMTP_DNS_MOCK_MODE` | `false` | Enable mock DNS for testing |
| `AMTP_DNS_ALLOW_HTTP` | `false` | Allow HTTP gateway URLs ⚠️ **Development only** |
| `AMTP_DNS_MOCK_RECORDS` | - | Custom mock DNS records (JSON format) |

> ⚠️ **Security Note**: Never enable `AMTP_DNS_ALLOW_HTTP=true` in production as it allows insecure HTTP gateway URLs. See [LOCAL_TESTING.md](LOCAL_TESTING.md) for how mock DNS and HTTP gateways are used in development.

### Message Processing
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_MESSAGE_MAX_SIZE` | `10485760` | Max message size in bytes (10MB) |
| `AMTP_MESSAGE_VALIDATION_ENABLED` | `true` | Enable message validation |
| `AMTP_IDEMPOTENCY_TTL` | `168h` | Idempotency cache TTL (7 days) |

### Authentication
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_AUTH_REQUIRED` | `false` | Require authentication |
| `AMTP_AUTH_API_KEY_HEADER` | `X-API-Key` | API key header name |
| `AMTP_ADMIN_KEY_FILE` | - | Path to admin API key file, required for the `/v1/admin` API (can also be set via `-admin-key-file` flag) |
| `AMTP_ADMIN_API_KEY_HEADER` | `X-Admin-Key` | Header name for admin API authentication |
| `AMTP_AUTH_API_KEY_SALT` | - | Salt for API key hashing |

### Logging
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `AMTP_LOG_FORMAT` | `json` | Log format (json, text) |

### Storage
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_STORAGE_TYPE` | `memory` | Storage type (database, memory) |
| `AMTP_STORAGE_DATABASE_DRIVER` | - | Database driver (pgx, cloudsqlpostgres, ...) |
| `AMTP_STORAGE_DATABASE_CONNECTION_STRING` | - | Database connection string |
| `AMTP_STORAGE_DATABASE_MAX_CONNS` | - | Max database connections |
| `AMTP_STORAGE_DATABASE_MAX_IDLE_TIME` | - | Max idle time for database connections (seconds) |

### Metrics
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_METRICS_ENABLED` | `false` | Enable JSON metrics collection and `/metrics` endpoint |

### Domain Signatures
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_SIGNATURE_PRIVATE_KEY_FILE` | - | Path to the PEM domain-signing private key. Empty disables outbound signing. |
| `AMTP_SIGNATURE_KEY_ID` | `k1` | Key selector (DNS label) used as the signature `keyid` |
| `AMTP_SIGNATURE_VERIFY_POLICY` | `flag` | Policy for remote messages without a valid signature: `accept`, `flag`, or `reject` |

Generate a key pair and print the DNS TXT record to publish:

```bash
agentry-admin keygen --domain example.com --key-id k1 --out private.pem
# Owner: k1._amtpkey.example.com.
# TXT: v=amtpkey1;alg=ES256;p=...
```

The private key file is read once at startup; a missing or invalid key fails startup when configured. See [API.md](API.md) for the verification semantics of each policy.

### Schema
| Variable | Default | Description |
|----------|---------|-------------|
| `AMTP_SCHEMA_REGISTRY_TYPE` | - | Schema registry type (set to `local` or `database` to enable) |
| `AMTP_SCHEMA_REGISTRY_PATH` | - | Path to local schema registry directory (when type is `local`) |
| `AMTP_SCHEMA_USE_LOCAL_REGISTRY` | `false` | (Deprecated) Enable local schema registry. Use `AMTP_SCHEMA_REGISTRY_TYPE=local` instead. |

See [SCHEMA.md](SCHEMA.md) for the schema framework itself.

## Admin Tool

Agents and schemas are runtime state, managed through the `/v1/admin` API rather than the settings above. The `agentry-admin` tool (`make build-admin`) wraps that API. Agent and schema commands need the same admin key the gateway was started with, passed via `--admin-key-file`; the tool reads the file as a single key. Use `--gateway-url` when the gateway is not at `http://localhost:8080`.

```bash
# Agent management (API keys are generated automatically)
./build/agentry-admin --admin-key-file admin.keys agent register user --mode pull
./build/agentry-admin --admin-key-file admin.keys agent register api-service --mode push --target http://api:8080/webhook
./build/agentry-admin --admin-key-file admin.keys agent list
./build/agentry-admin --admin-key-file admin.keys agent unregister user

# Schema management
./build/agentry-admin --admin-key-file admin.keys schema register agntcy:test.v1 -f schema.json
./build/agentry-admin --admin-key-file admin.keys schema list
./build/agentry-admin --admin-key-file admin.keys schema get agntcy:test.v1
./build/agentry-admin --admin-key-file admin.keys schema delete agntcy:test.v1
./build/agentry-admin --admin-key-file admin.keys schema validate agntcy:test.v1 -f payload.json
./build/agentry-admin --admin-key-file admin.keys schema stats

# Inbox management (uses the agent's own API key, not the admin key)
./build/agentry-admin inbox get user@localhost --key your-api-key
./build/agentry-admin inbox get user@localhost --key-file user.key
./build/agentry-admin inbox ack user@localhost message-id-123 --key your-api-key
```

For complete documentation, see [cmd/agentry-admin/README.md](../cmd/agentry-admin/README.md).

## Production Example

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
export AMTP_ADMIN_KEY_FILE="/etc/agentry/admin.keys"  # Required for admin API access
export AMTP_AUTH_API_KEY_SALT="your_salt"  # Optional: for api_key hash salt

# Logging
export AMTP_LOG_LEVEL=info
export AMTP_LOG_FORMAT=json

# Storage configuration
export AMTP_STORAGE_TYPE=database
export AMTP_STORAGE_DATABASE_DRIVER=pgx
export AMTP_STORAGE_DATABASE_CONNECTION_STRING="host=db.example.com port=5432 user=USER password=PASSWORD dbname=agentry"
export AMTP_STORAGE_DATABASE_MAX_CONNS=100
export AMTP_STORAGE_DATABASE_MAX_IDLE_TIME=300

# Metrics (optional - enable for monitoring)
export AMTP_METRICS_ENABLED=true

# Schema management (optional - enable for schema validation)
export AMTP_SCHEMA_REGISTRY_TYPE=database
```

For systemd, Docker, and Kubernetes setups, see [DEPLOYMENT.md](DEPLOYMENT.md).

## Development Example

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
# Alternative: export AMTP_SCHEMA_REGISTRY_TYPE=database for database registry
```

> 📝 The development script `./scripts/local-dev.sh` sets these variables for you.
