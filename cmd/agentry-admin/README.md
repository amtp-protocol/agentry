# AMTP Admin Tool

The `agentry-admin` tool is a command-line interface for managing schemas, local agents, and inbox operations in the AMTP Gateway. It provides a comprehensive set of commands for schema registration, validation, registry administration, agent management, and message inbox operations.

## Installation

Build the admin tool from the project root:

```bash
make build-admin
```

The binary will be created at `build/agentry-admin`.

## Usage

```bash
agentry-admin [global-flags] <command> [args]
```

## Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--gateway-url <url>` | Gateway URL to connect to | `http://localhost:8080` |
| `-v, --verbose` | Enable verbose output for debugging | `false` |

## Commands

### Schema Management

#### `schema register`

Register a new schema with the gateway.

**Usage:**
```bash
agentry-admin schema register <schema-id> [flags]
```

**Flags:**
- `-f, --file <file>` - Schema definition file (required)
- `--force` - Overwrite existing schema if it already exists

**Examples:**
```bash
# Register a new schema
agentry-admin schema register agntcy:commerce.order.v1 -f order-schema.json

# Register with long flag format
agentry-admin schema register agntcy:commerce.order.v1 --file order-schema.json

# Force overwrite existing schema
agentry-admin schema register agntcy:commerce.order.v1 -f order-schema.json --force

# Register to remote gateway
agentry-admin --gateway-url http://gateway.example.com:8080 schema register agntcy:commerce.order.v1 -f order-schema.json
```

#### `schema list`

List all registered schemas in the gateway.

**Usage:**
```bash
agentry-admin schema list
```

**Examples:**
```bash
# List all schemas
agentry-admin schema list

# List with verbose output
agentry-admin --verbose schema list
```

#### `schema get`

Retrieve and display a specific schema definition.

**Usage:**
```bash
agentry-admin schema get <schema-id>
```

**Examples:**
```bash
# Get a specific schema
agentry-admin schema get agntcy:commerce.order.v1

# Get schema from remote gateway
agentry-admin --gateway-url http://gateway.example.com:8080 schema get agntcy:commerce.order.v1
```

#### `schema delete`

Delete a schema from the gateway.

**Usage:**
```bash
agentry-admin schema delete <schema-id>
```

**Examples:**
```bash
# Delete a schema
agentry-admin schema delete agntcy:commerce.order.v1

# Delete with verbose output
agentry-admin --verbose schema delete agntcy:commerce.order.v1
```

#### `schema validate`

Validate a JSON payload against a registered schema.

**Usage:**
```bash
agentry-admin schema validate <schema-id> [flags]
```

**Flags:**
- `-f, --file <file>` - Payload file to validate (required)

**Examples:**
```bash
# Validate a payload
agentry-admin schema validate agntcy:commerce.order.v1 -f order-payload.json

# Validate with long flag format
agentry-admin schema validate agntcy:commerce.order.v1 --file order-payload.json

# Validate with verbose output
agentry-admin --verbose schema validate agntcy:commerce.order.v1 -f order-payload.json
```

#### `schema stats`

Display schema registry statistics and information.

**Usage:**
```bash
agentry-admin schema stats
```

**Examples:**
```bash
# Show schema registry statistics
agentry-admin schema stats

# Show stats with verbose output
agentry-admin --verbose schema stats
```

### Agent Management

The AMTP gateway supports local agents with two delivery modes: **pull** (inbox-based) and **push** (webhook-based). Agents can be registered, configured, and managed through the admin tool.

#### `agent register`

Register a local agent with the gateway.

**Usage:**
```bash
agentry-admin agent register <address> [flags]
```

**Flags:**
- `--mode <mode>` - Delivery mode: 'push' or 'pull' (default: pull)
- `--target <url>` - Push target URL (required for push mode)
- `--header <key=value>` - Custom header (can be used multiple times)
- `--schema <schema-id>` - Supported schema in format agntcy:domain.entity.version or agntcy:domain.* (can be used multiple times)

**Examples:**
```bash
# Register agent with name
agentry-admin agent register user --mode pull
agentry-admin agent register api-service --mode push --target http://api:8080/webhook
agentry-admin agent register purchase-bot --mode pull

# Register agent with custom headers
agentry-admin agent register api-service --mode push --target http://api:8080/webhook \
  --header "Authorization=Bearer secret-token" \
  --header "X-Agent-ID=api-service"

# Register agent with supported schemas
agentry-admin agent register sales --mode pull \
  --schema "agntcy:commerce.*" \
  --schema "agntcy:crm.lead.v1"

# Register agent with both schemas and headers
agentry-admin agent register api-service --mode push --target http://api:8080/webhook \
  --header "Authorization=Bearer secret-token" \
  --schema "agntcy:commerce.order.v1" \
  --schema "agntcy:commerce.product.v1"

# Register to remote gateway
agentry-admin --gateway-url http://gateway.example.com:8080 agent register user --mode pull
```

#### `agent list`

List all registered local agents.

**Usage:**
```bash
agentry-admin agent list
```

**Examples:**
```bash
# List all agents
agentry-admin agent list

# List with verbose output
agentry-admin --verbose agent list
```

#### `agent unregister`

Remove a local agent from the gateway.

**Usage:**
```bash
agentry-admin agent unregister <address>
```

**Examples:**
```bash
# Unregister an agent
agentry-admin agent unregister test2

# Unregister with verbose output
agentry-admin --verbose agent unregister api
```

### Inbox Management

For agents using **pull mode**, messages are stored in local inboxes. The admin tool provides commands to retrieve and acknowledge messages.

#### `inbox get`

Retrieve messages from an agent's inbox.

**Usage:**
```bash
agentry-admin inbox get <recipient>
```

**Examples:**
```bash
# Get messages for a recipient
agentry-admin inbox get test2@localhost

# Get messages from remote gateway
agentry-admin --gateway-url http://gateway.example.com:8080 inbox get user@domain.com

# Get messages with verbose output
agentry-admin --verbose inbox get test2@localhost
```

#### `inbox ack`

Acknowledge and remove a message from an agent's inbox.

**Usage:**
```bash
agentry-admin inbox ack <recipient> <message-id>
```

**Examples:**
```bash
# Acknowledge a specific message
agentry-admin inbox ack test2@localhost 0198d94c-164d-7a9d-9ff3-f1d0c8cfa87e

# Acknowledge message on remote gateway
agentry-admin --gateway-url http://gateway.example.com:8080 inbox ack user@domain.com message-id-123

# Acknowledge with verbose output
agentry-admin --verbose inbox ack test2@localhost message-id-456
```

## Agent Concepts

### Delivery Modes

The AMTP gateway supports two delivery modes for local agents:

#### Pull Mode (Inbox-based)
- **Default mode** when no mode is specified
- Messages are stored in the gateway's local inbox
- Agents periodically check for new messages using `inbox get`
- Messages must be acknowledged using `inbox ack` after processing
- **Use cases**: Batch processing, offline agents, reliable delivery
- **Advantages**: Persistent storage, no webhook infrastructure needed
- **Disadvantages**: Requires polling, higher latency

#### Push Mode (Webhook-based)
- Messages are immediately delivered via HTTP POST to a webhook URL
- Requires `--target` URL and optionally custom `--header` values
- Real-time delivery with immediate processing
- **Use cases**: Real-time applications, online services, event-driven systems
- **Advantages**: Immediate delivery, event-driven processing
- **Disadvantages**: Requires webhook infrastructure, less reliable if webhook is down

### Agent Schema Support

Agents can declare which schemas they support during registration. This enables:
- **Smart routing**: Messages are delivered to agents that can handle the specific schema
- **Dynamic capabilities**: Gateway advertises schemas based on registered agents
- **Schema validation**: Gateway validates messages before routing to agents

**Schema Declaration:**
- Use `--schema` flag multiple times to declare multiple supported schemas
- Supports exact matches: `agntcy:commerce.order.v1`
- Supports wildcards: `agntcy:commerce.*` (matches all commerce schemas)
- If no schemas declared, agent accepts all schemas (backward compatibility)

**Examples:**
```bash
# Agent that handles all commerce schemas
agentry-admin agent register sales --schema "agntcy:commerce.*"

# Agent that handles specific schemas
agentry-admin agent register crm --schema "agntcy:crm.lead.v1" --schema "agntcy:crm.contact.v1"

# Agent that handles mixed schemas
agentry-admin agent register api --schema "agntcy:commerce.*" --schema "agntcy:auth.user.v1"
```

### Agent Address Format

Agent addresses follow the standard email format:
```
<local-part>@<domain>
```

**Examples:**
- `user@localhost` - Local user agent
- `api@localhost` - API service agent  
- `orders@commerce.example.com` - Remote domain agent

### Message Flow

1. **Message Sent**: External system sends message to gateway
2. **Agent Lookup**: Gateway checks if recipient has registered agent
3. **Delivery Decision**:
   - **Registered Agent**: Use configured delivery mode (push/pull)
   - **Unregistered Agent**: Default to pull mode
4. **Delivery Execution**:
   - **Pull Mode**: Store in inbox, agent polls for messages
   - **Push Mode**: HTTP POST to webhook URL immediately

## Schema Management Architecture

The AMTP Gateway uses a two-tier schema architecture:

### Schema Registry
- **Purpose**: Stores actual JSON Schema definitions for message validation
- **Management**: Use `agentry-admin schema register/list/get/delete` commands
- **Content**: JSON Schema documents that define message payload structure

### Agent Schema Support
- **Purpose**: Declares which schema IDs each agent can handle
- **Management**: Use `--schema` flag during `agentry-admin agent register`
- **Content**: Schema identifiers (not definitions) that agents support

### Relationship
1. **Register schema definitions** in the registry: `agentry-admin schema register agntcy:commerce.order.v1 -f schema.json`
2. **Agents declare support** for schema IDs: `agentry-admin agent register sales --schema "agntcy:commerce.*"`
3. **Gateway validates** incoming messages against registry definitions
4. **Gateway routes** messages to agents that declared support for the schema
5. **Capability discovery** returns schemas supported by registered agents

## Schema Identifier Format

Schema identifiers follow the AGNTCY format:
```
agntcy:<domain>.<entity>.<version>
```

**Examples:**
- `agntcy:commerce.order.v1`
- `agntcy:auth.user.v2`
- `agntcy:messaging.notification.v1`

## Schema File Format

Schema files must be valid JSON Schema documents. Example:

```json
{
  "type": "object",
  "title": "Order Schema",
  "description": "Schema for commerce orders",
  "properties": {
    "id": {
      "type": "string",
      "description": "Unique order identifier"
    },
    "customer_id": {
      "type": "string",
      "description": "Customer identifier"
    },
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "product_id": {
            "type": "string"
          },
          "quantity": {
            "type": "integer",
            "minimum": 1
          },
          "price": {
            "type": "number",
            "minimum": 0
          }
        },
        "required": ["product_id", "quantity", "price"]
      }
    },
    "total": {
      "type": "number",
      "minimum": 0
    }
  },
  "required": ["id", "customer_id", "items", "total"]
}
```

## Error Handling

The tool provides detailed error messages for common issues:

### Schema Registration Errors
- **File not found**: Check that the schema file path is correct
- **Invalid JSON**: Ensure the schema file contains valid JSON
- **Schema already exists**: Use `--force` flag to overwrite existing schemas
- **Network errors**: Check gateway URL and connectivity

### Validation Errors
- **Schema not found**: Ensure the schema ID is registered
- **Invalid payload**: The tool will display specific validation errors
- **File not found**: Check that the payload file path is correct

### Network Errors
- **Connection refused**: Ensure the gateway is running
- **Timeout**: Check network connectivity and gateway responsiveness
- **Authentication errors**: Verify gateway configuration

## Configuration

### Gateway URL

The default gateway URL is `http://localhost:8080`. You can override this using:

1. **Command-line flag** (highest priority):
   ```bash
   agentry-admin --gateway-url http://gateway.example.com:8080 schema list
   ```

2. **Environment variable**:
   ```bash
   export AMTP_GATEWAY_URL=http://gateway.example.com:8080
   agentry-admin schema list
   ```

### Verbose Output

Enable verbose output for debugging:

```bash
# Short flag
agentry-admin -v schema register agntcy:test.v1 -f schema.json

# Long flag
agentry-admin --verbose schema register agntcy:test.v1 -f schema.json
```

Verbose output includes:
- HTTP request details (method, URL)
- Request and response bodies
- HTTP status codes
- Timing information

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error (invalid arguments, file not found, etc.) |
| 2 | Network error (connection failed, timeout) |
| 3 | API error (4xx/5xx HTTP responses) |

## Examples Workflow

### Complete Schema Management Workflow

```bash
# 1. Initialize registry (if needed)
agentry-admin registry init

# 2. Register a schema
agentry-admin schema register agntcy:commerce.order.v1 -f order-schema.json

# 3. List all schemas to verify
agentry-admin schema list

# 4. Get the schema definition
agentry-admin schema get agntcy:commerce.order.v1

# 5. Validate a payload against the schema
agentry-admin schema validate agntcy:commerce.order.v1 -f order-payload.json

# 6. Check registry statistics
agentry-admin registry stats

# 7. Update schema (force overwrite)
agentry-admin schema register agntcy:commerce.order.v1 -f updated-order-schema.json --force

# 8. Delete schema when no longer needed
agentry-admin schema delete agntcy:commerce.order.v1
```

### Complete Agent Management Workflow

```bash
# 1. Register schemas first (if not already registered)
agentry-admin schema register agntcy:commerce.order.v1 -f order-schema.json
agentry-admin schema register agntcy:crm.lead.v1 -f lead-schema.json

# 2. Register agents with schema support
agentry-admin agent register sales@localhost --mode pull \
  --schema "agntcy:commerce.*" \
  --schema "agntcy:crm.lead.v1"

agentry-admin agent register api@localhost --mode push --target http://api:8080/webhook \
  --header "Authorization=Bearer secret-token" \
  --schema "agntcy:commerce.order.v1"

# 3. List all agents to verify registration
agentry-admin agent list

# 4. Send messages (using curl or other tools)
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test1@localhost",
    "recipients": ["test2@localhost"],
    "subject": "Test Message",
    "payload": {"message": "Hello test2!"}
  }'

# 5. Check inbox for pull mode agents
agentry-admin inbox get sales@localhost

# 6. Process and acknowledge messages
agentry-admin inbox ack sales@localhost message-id-123

# 7. Verify inbox is empty after acknowledgment
agentry-admin inbox get sales@localhost

# 8. Clean up agents when done
agentry-admin agent unregister sales@localhost
agentry-admin agent unregister api@localhost
```

### Agent Delivery Modes

#### Pull Mode (Inbox-based)
```bash
# Register for pull mode
agentry-admin agent register user@localhost --mode pull

# Check for new messages
agentry-admin inbox get user@localhost

# Process messages and acknowledge
agentry-admin inbox ack user@localhost message-id-456
```

#### Push Mode (Webhook-based)
```bash
# Register for push mode with webhook
agentry-admin agent register service@localhost --mode push \
  --target http://service:8080/amtp/webhook \
  --header "Authorization=Bearer webhook-token" \
  --header "X-Service-ID=message-processor"

# Messages will be automatically pushed to the webhook
# No inbox management needed for push mode
```

### Working with Remote Gateway

```bash
# Set gateway URL for all commands
export AMTP_GATEWAY_URL=http://production-gateway.example.com:8080

# Or use flag for individual commands
agentry-admin --gateway-url http://staging-gateway.example.com:8080 schema list
```

### Debugging Issues

```bash
# Enable verbose output to see HTTP details
agentry-admin --verbose schema register agntcy:test.v1 -f schema.json

# Check registry status
agentry-admin registry stats

# Validate connectivity
agentry-admin schema list
```

## Troubleshooting

### Common Issues

1. **"connection refused" error**
   - Ensure the AMTP Gateway is running
   - Verify the gateway URL is correct
   - Check firewall and network connectivity

2. **"schema already exists" error**
   - Use `--force` flag to overwrite existing schemas
   - Or delete the existing schema first

3. **"invalid JSON" error**
   - Validate your JSON schema file syntax
   - Use a JSON validator or formatter

4. **"file not found" error**
   - Check file paths are correct and files exist
   - Use absolute paths if relative paths don't work

5. **"validation failed" error**
   - Review the specific validation errors in the output
   - Ensure your payload matches the schema requirements

6. **"push target URL is required" error**
   - Ensure you provide `--target` flag when using `--mode push`
   - Verify the webhook URL is accessible and correct

7. **"message not found" error (inbox ack)**
   - Check that the message ID is correct and exists in the inbox
   - Use `inbox get` to list available messages first

8. **"no messages" in inbox**
   - Verify messages were sent to the correct recipient address
   - Check if the agent is registered for pull mode
   - Ensure messages haven't been acknowledged already

9. **Push delivery not working**
   - Verify the webhook endpoint is running and accessible
   - Check webhook logs for incoming requests
   - Ensure custom headers are correctly formatted (key=value)

### Getting Help

- Use `--help` flag for command usage: `agentry-admin --help`
- Use command-specific help: 
  - `agentry-admin schema register --help`
  - `agentry-admin agent register --help`
  - `agentry-admin inbox get --help`
- Enable verbose output for debugging: `agentry-admin --verbose <command>`

## API Endpoints

The admin tool communicates with the following gateway API endpoints:

### Schema Management
| Command | Method | Endpoint |
|---------|--------|----------|
| `schema register` | POST | `/v1/admin/schemas` |
| `schema list` | GET | `/v1/admin/schemas` |
| `schema get` | GET | `/v1/admin/schemas/{id}` |
| `schema delete` | DELETE | `/v1/admin/schemas/{id}` |
| `schema validate` | POST | `/v1/admin/schemas/{id}/validate` |
| `schema stats` | GET | `/v1/admin/schemas/stats` |

### Agent Management
| Command | Method | Endpoint |
|---------|--------|----------|
| `agent register` | POST | `/v1/admin/agents` |
| `agent list` | GET | `/v1/admin/agents` |
| `agent unregister` | DELETE | `/v1/admin/agents/{address}` |

### Inbox Management
| Command | Method | Endpoint |
|---------|--------|----------|
| `inbox get` | GET | `/v1/inbox/{recipient}` |
| `inbox ack` | DELETE | `/v1/inbox/{recipient}/{message-id}` |

All requests use JSON content type and expect JSON responses.
