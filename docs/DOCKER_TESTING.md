# AMTP Gateway Docker Simulation

The `docker/` directory contains Docker Compose configuration and the `scripts` directory contains scripts for simulating multiple AMTP gateway domains with full schema functionality locally.

## Quick Start

### 1. Run the Full Test Suite

```bash
# Start services and run comprehensive tests
./scripts/test-simulation-docker.sh start
```

This will:
- Build and start DNS server with AMTP TXT records (no schemas in DNS)
- Build and start 3 AMTP gateways (company-a.local, company-b.local, partner.local)
- Test basic connectivity
- Test DNS TXT record discovery (verifying schemas are NOT in DNS)
- Register test schemas on all gateways
- Register agents with schema support (demonstrating agent-centric schema model)
- Test gateway capabilities (showing schemas from agents, not DNS)
- Test agent discovery with schema information
- Test inter-gateway communication with schema validation
- Test schema validation scenarios (success and failure cases)
- Test invalid schema scenarios

### 2. Optional: Setup Host Domain Access

The DNS server provides TXT records for AMTP discovery, but to access gateways by domain name from your host machine, you need to add entries to `/etc/hosts`:

```bash
# Add domain name resolution to /etc/hosts (requires sudo)
./scripts/setup-hosts.sh add

# Now you can use domain names instead of localhost:port
curl http://company-a.local:8080/health        # Instead of localhost:8080
curl http://company-b.local:8081/health        # Instead of localhost:8081  
curl http://partner.local:8082/health          # Instead of localhost:8082
```

**Without setup-hosts.sh, use:**
```bash
curl http://localhost:8080/health               # Company A
curl http://localhost:8081/health               # Company B
curl http://localhost:8082/health               # Partner
```

## Available Commands

### Test Script Commands

```bash
./scripts/test-simulation-docker.sh start             # Full comprehensive test suite
./scripts/test-simulation-docker.sh stop              # Stop and cleanup
./scripts/test-simulation-docker.sh logs              # Show service logs
./scripts/test-simulation-docker.sh status            # Show service status
./scripts/test-simulation-docker.sh test-connectivity # Run connectivity tests only
./scripts/test-simulation-docker.sh test-discovery    # Run discovery tests only
./scripts/test-simulation-docker.sh test-schemas      # Run schema validation tests only
```

### Hosts Setup Commands

```bash
./scripts/setup-hosts.sh add     # Add domains to /etc/hosts
./scripts/setup-hosts.sh remove  # Remove domains from /etc/hosts
./scripts/setup-hosts.sh show    # Show current domains
```

### Manual Docker Compose Commands

```bash
# Start services
docker-compose -f docker/docker-compose.schema-test.yml up -d

# View logs
docker-compose -f docker/docker-compose.schema-test.yml logs -f

# Stop services
docker-compose -f docker/docker-compose.schema-test.yml down -v
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Docker Network: amtp-network                │
│                        Subnet: 172.20.0.0/16                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────┐  ← DNS Server with AMTP TXT records       │
│  │   DNS Server    │                                            │
│  │   (CoreDNS)     │                                            │
│  │  172.20.0.10    │                                            │
│  └─────────────────┘                                            │
│           │                                                     │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐ │
│  │  Company A      │  │  Company B      │  │  Partner        │ │
│  │  Gateway        │  │  Gateway        │  │  Gateway        │ │
│  │                 │  │                 │  │                 │ │
│  │ company-a.local │  │ company-b.local │  │ partner.local   │ │
│  │ Port: 8080      │  │ Port: 8080      │  │ Port: 8080      │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘ │
│           │                     │                     │         │
│           └─────────────────────┼─────────────────────┘         │
│                                 │                               │
│                    ┌─────────────────┐                         │
│                    │  Test Client    │                         │
│                    │  Container      │                         │
│                    │                 │                         │
│                    │ curl + dig      │                         │
│                    └─────────────────┘                         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
         │                    │                    │        │
    Host:8080            Host:8081           Host:8082  Host:1053 (DNS)
```

## Domain Resolution

### DNS TXT Records
The DNS server provides AMTP discovery records (schemas are NOT in DNS):
- `_amtp.company-a.local` → `"v=amtp1;gateway=http://company-a.local:8080;features=agent-discovery,schema-validation"`
- `_amtp.company-b.local` → `"v=amtp1;gateway=http://company-b.local:8080;features=agent-discovery,schema-validation"`
- `_amtp.partner.local` → `"v=amtp1;gateway=http://partner.local:8080;features=agent-discovery,schema-validation"`

### Inside Docker Network
- Containers can reach each other using domain names:
  - `http://company-a.local:8080`
  - `http://company-b.local:8080`
  - `http://partner.local:8080`
- DNS queries are resolved by the custom DNS server at `172.20.0.10`

### From Host Machine (after setup-hosts.sh)
- Access via localhost with different ports:
  - `http://company-a.local:8080` → `localhost:8080`
  - `http://company-b.local:8081` → `localhost:8081`
  - `http://partner.local:8082` → `localhost:8082`
- Test DNS from host: `dig @localhost -p 1053 _amtp.company-a.local TXT`

## Testing Scenarios

### 1. Basic Discovery
Each gateway can be discovered via DNS TXT records and provides health endpoints:

```bash
# Test gateway health
curl http://company-a.local:8080/health

# Test agent discovery
curl http://company-a.local:8080/v1/discovery/agents
```

### 2. Schema Registration
Register schemas on gateways:

```bash
curl -X POST http://company-a.local:8080/v1/admin/schemas \
  -H "Content-Type: application/json" \
  -d '{
    "id": "agntcy:commerce.order.v1",
    "definition": {
      "$schema": "http://json-schema.org/draft-07/schema#",
      "title": "Commerce Order",
      "type": "object",
      "properties": {
        "order_id": {"type": "string"},
        "total_amount": {"type": "number", "minimum": 0}
      },
      "required": ["order_id", "total_amount"]
    }
  }'
```

### 3. Agent Registration with Schema Support
Register agents with their supported schemas:

```bash
curl -X POST http://company-a.local:8080/v1/admin/agents \
  -H "Content-Type: application/json" \
  -d '{
    "address": "sales", 
    "delivery_mode": "pull",
    "supported_schemas": ["agntcy:commerce.*", "agntcy:finance.payment.*"]
  }'
```

### 4. Enhanced Discovery
After registering agents, discovery includes agent information with schemas:

```bash
curl http://company-a.local:8080/v1/discovery/agents | jq .agents
```

### 5. Inter-Gateway Discovery
Gateways can discover each other's capabilities:

```bash
curl http://company-a.local:8080/v1/capabilities/company-b.local
```

### 6. Message Sending with Schema Validation
Send messages between domains with schema validation:

```bash
curl -X POST http://company-a.local:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "sales@company-a.local",
    "recipients": ["payment-processor@company-b.local"],
    "subject": "Payment Request",
    "schema": "agntcy:finance.payment.v1",
    "payload": {
      "payment_id": "PAY-12345",
      "amount": 99.99,
      "currency": "USD",
      "payment_method": "credit_card",
      "status": "pending"
    }
  }'
```

## Troubleshooting

### Services Not Starting
```bash
# Check service status
./scripts/test-simulation-docker.sh status

# View logs
./scripts/test-simulation-docker.sh logs

# Rebuild and restart
docker-compose -f docker/docker-compose.schema-test.yml down -v
docker-compose -f docker/docker-compose.schema-test.yml up -d --build
```

### Domain Resolution Issues
```bash
# Test from inside network
docker exec amtp-test-client nslookup company-a.local

# Check network configuration
docker network inspect amtp-network
```

### Port Conflicts
If ports 8080-8082 are in use, modify the Docker Compose file:

```yaml
ports:
  - "9080:8080"  # Change host port
```

## Cleanup

```bash
# Stop services and remove containers/networks
./scripts/test-simulation-docker.sh stop

# Remove host entries
./scripts/setup-hosts.sh remove

# Remove Docker images (optional)
docker rmi $(docker images -q amtp-gateway)
```

## Files

- `docker-compose.schema-test.yml` - Main Docker Compose configuration with schema support
- `../scripts/test-simulation-docker.sh` - Comprehensive test script (domain + schema functionality)
- `../scripts/setup-hosts.sh` - Host file management script
- `README-domain-simulation.md` - This documentation

## Schema Management

The simulation uses **file-based schema management** with schemas stored in `/tmp/amtp-schemas/` during testing. The test script automatically:
1. Creates `/tmp/amtp-schemas/` directory
2. Populates it with sample schema files
3. Configures gateways to use file-based schema registry
4. Cleans up temporary files on exit
