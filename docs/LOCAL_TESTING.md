# Local DNS Testing Guide

This guide explains how to test DNS TXT record discovery locally using the built-in mock DNS functionality and configurable HTTP support.

## Overview

The AMTP Gateway supports **mock DNS mode** and **configurable HTTP support** for local development, which simulates DNS TXT record responses and allows HTTP gateway URLs without requiring actual DNS configuration.

## Quick Start

### Option 1: Use Development Script (Recommended)

The simplest way to test DNS TXT records locally:

```bash
# Use the development script (mock DNS + HTTP enabled by default)
./local-dev.sh
```

This automatically enables:
- ✅ Mock DNS mode (`AMTP_DNS_MOCK_MODE=true`)
- ✅ HTTP gateway support (`AMTP_DNS_ALLOW_HTTP=true`)
- ✅ Local testing configuration

### Option 2: Manual Configuration

```bash
# Enable mock DNS and HTTP support manually
export AMTP_DNS_MOCK_MODE=true
export AMTP_DNS_ALLOW_HTTP=true
export AMTP_TLS_ENABLED=false
./build/agentry
```

## Configuration Options

### Environment Variables

| Variable | Description | Default | Notes |
|----------|-------------|---------|-------|
| `AMTP_DNS_MOCK_MODE` | Enable mock DNS mode | `false` | Simulates DNS TXT records |
| `AMTP_DNS_ALLOW_HTTP` | Allow HTTP gateway URLs | `false` | **Security**: Only enable for development |
| `AMTP_DNS_MOCK_RECORDS` | Custom mock records (JSON) | See below | Override default test records |

### YAML Configuration

```yaml
dns:
  mock_mode: true              # Enable mock DNS
  allow_http: true             # Allow HTTP gateways (dev only)
  mock_records:
    localhost: "v=amtp1;gateway=http://localhost:8080;schemas=agntcy:test.*"
    custom.local: "v=amtp1;gateway=http://localhost:9000;auth=apikey"
```

## Security Design

### HTTP Gateway Validation

By default, AMTP **requires HTTPS** for all gateway URLs to ensure security:

```bash
# Default behavior (AMTP_DNS_ALLOW_HTTP=false)
# ❌ HTTP gateways are rejected
curl -X POST http://localhost:8080/v1/messages \
  -d '{"sender":"test@localhost","recipients":["user@localhost"]}'
# Result: "gateway URL must use HTTPS"
```

### Development Mode

When explicitly enabled, HTTP is allowed for local testing:

```bash
# Development mode (AMTP_DNS_ALLOW_HTTP=true)  
# ✅ HTTP gateways are accepted
export AMTP_DNS_ALLOW_HTTP=true
curl -X POST http://localhost:8080/v1/messages \
  -d '{"sender":"test@localhost","recipients":["user@localhost"]}'
# Result: Message processed successfully
```

**⚠️ Security Warning**: Only enable `AMTP_DNS_ALLOW_HTTP=true` in development environments. Production should always use HTTPS.

## Mock DNS Records

### Default Mock Records

When `AMTP_DNS_MOCK_MODE=true`, these DNS TXT records are simulated:

| Domain | Mock TXT Record |
|--------|----------------|
| `localhost` | `v=amtp1;gateway=http://localhost:8080;schemas=agntcy:test.*,agntcy:dev.*` |
| `test.local` | `v=amtp1;gateway=http://localhost:8080;schemas=agntcy:test.*` |
| `dev.local` | `v=amtp1;gateway=http://localhost:8080;schemas=agntcy:dev.*` |
| `example.com` | `v=amtp1;gateway=http://localhost:8080;schemas=agntcy:example.*` |

### Custom Mock Records

```bash
export AMTP_DNS_MOCK_RECORDS='{
  "localhost": "v=amtp1;gateway=http://localhost:8080;schemas=agntcy:test.*",
  "mytest.com": "v=amtp1;gateway=http://localhost:9000;schemas=agntcy:custom.*",
  "secure.local": "v=amtp1;gateway=https://localhost:8443;auth=cert;max-size=5242880"
}'
```

## Testing Scenarios

### Scenario 1: Default Security (HTTP Disabled)

Test that HTTP gateways are rejected by default:

```bash
# Start without HTTP allowance
AMTP_DNS_MOCK_MODE=true AMTP_TLS_ENABLED=false ./build/agentry

# This should fail with "gateway URL must use HTTPS"
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test@localhost",
    "recipients": ["user@localhost"],
    "subject": "Security Test",
    "payload": {"test": "http_disabled"}
  }'
```

### Scenario 2: Development Mode (HTTP Enabled)

Test that HTTP gateways work when explicitly enabled:

```bash
# Start with HTTP allowance for development
AMTP_DNS_MOCK_MODE=true AMTP_DNS_ALLOW_HTTP=true AMTP_TLS_ENABLED=false ./build/agentry

# This should succeed
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test@localhost",
    "recipients": ["user@localhost"],
    "subject": "Development Test",
    "payload": {"test": "http_enabled"}
  }'
```

### Scenario 3: Mixed HTTP/HTTPS Testing

Test both HTTP and HTTPS gateways in the same environment:

```bash
export AMTP_DNS_MOCK_RECORDS='{
  "http.local": "v=amtp1;gateway=http://localhost:8080;schemas=agntcy:test.*",
  "https.local": "v=amtp1;gateway=https://localhost:8443;schemas=agntcy:test.*"
}'
export AMTP_DNS_ALLOW_HTTP=true

# Test HTTP gateway (works)
curl -X POST http://localhost:8080/v1/messages \
  -d '{"sender":"test@localhost","recipients":["user@http.local"],"subject":"HTTP Test"}'

# Test HTTPS gateway (also works)  
curl -X POST http://localhost:8080/v1/messages \
  -d '{"sender":"test@localhost","recipients":["user@https.local"],"subject":"HTTPS Test"}'
```

### Scenario 4: Schema Validation with Mock DNS

```bash
# Test schema support patterns
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test@localhost",
    "recipients": ["user@localhost"],
    "schema": "agntcy:test.order.v1",
    "subject": "Schema Test",
    "payload": {"order_id": "12345"}
  }'
```

## Discovery Testing

### Test Discovery Endpoints

```bash
# Test mock DNS discovery
curl http://localhost:8080/v1/capabilities/localhost | jq .
curl http://localhost:8080/v1/capabilities/test.local | jq .
curl http://localhost:8080/v1/capabilities/dev.local | jq .

# Test non-existent domain (should fail)
curl http://localhost:8080/v1/capabilities/nonexistent.com | jq .
```

### Verify Mock Records

```bash
# Check that localhost has the expected mock record
curl -s http://localhost:8080/v1/capabilities/localhost | jq .
# Expected output:
# {
#   "version": "1.0", 
#   "gateway": "http://localhost:8080",
#   "schemas": ["agntcy:test.*", "agntcy:dev.*"],
#   "discovered_at": "...",
#   "ttl": "5m0s"
# }
```

## Debugging

### Enable Debug Logging

```bash
export AMTP_LOG_LEVEL=debug
export AMTP_DNS_MOCK_MODE=true
export AMTP_DNS_ALLOW_HTTP=true
./build/agentry
```

### Check Configuration in Logs

Look for these log messages:
```
DEBUG config: DNS configuration loaded mock_mode=true allow_http=true
DEBUG dns.mock: Using mock DNS record domain=localhost record="v=amtp1;gateway=http://localhost:8080"
DEBUG processing.delivery: Gateway URL validation passed url="http://localhost:8080" allow_http=true
```

### Common Debugging Commands

```bash
# 1. Test server health
curl http://localhost:8080/health

# 2. Test discovery
curl http://localhost:8080/v1/capabilities/localhost

# 3. Test HTTP validation
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"sender":"test@localhost","recipients":["user@localhost"],"subject":"Debug"}'

# 4. Check environment variables
echo "Mock mode: $AMTP_DNS_MOCK_MODE"
echo "Allow HTTP: $AMTP_DNS_ALLOW_HTTP"
```

## Production vs Development

| Aspect | Production | Development |
|--------|-----------|-------------|
| **DNS Discovery** | Real DNS TXT records | Mock DNS records |
| **Gateway URLs** | HTTPS required | HTTP allowed if configured |
| **Security** | Full security enforced | Relaxed for testing |
| **Configuration** | `allow_http: false` | `allow_http: true` |

## Best Practices

### Development Workflow

1. **Always start with default security** - Test that HTTP is rejected by default
2. **Explicitly enable HTTP** - Only enable `AMTP_DNS_ALLOW_HTTP=true` when needed
3. **Use consistent test domains** - Stick to localhost, test.local, dev.local
4. **Test both HTTP and HTTPS** - Verify both protocols work when enabled
5. **Document security implications** - Make it clear that HTTP is for development only

### Security Checklist

- [ ] ✅ HTTP gateways are **disabled by default**
- [ ] ✅ HTTP is only enabled via explicit configuration
- [ ] ✅ Production configs never set `allow_http: true`
- [ ] ✅ Development environments clearly marked as such
- [ ] ✅ Mock DNS is only used for testing

### Configuration Examples

#### ❌ Bad - Never in Production
```yaml
dns:
  allow_http: true  # NEVER do this in production
```

#### ✅ Good - Development Only
```yaml
# config.local.yaml - for development only
dns:
  mock_mode: true
  allow_http: true  # OK for local development
```

#### ✅ Good - Production
```yaml
# config.yaml - production configuration  
dns:
  mock_mode: false   # Use real DNS
  allow_http: false  # Require HTTPS (default)
```

## Troubleshooting

### Problem: HTTP URLs rejected even with mock DNS
**Solution**: Check that `AMTP_DNS_ALLOW_HTTP=true` is set
```bash
echo $AMTP_DNS_ALLOW_HTTP  # Should be 'true'
```

### Problem: Mock DNS not working
**Solution**: Verify mock mode is enabled
```bash
echo $AMTP_DNS_MOCK_MODE   # Should be 'true'
curl http://localhost:8080/v1/capabilities/localhost
```

### Problem: Gateway validation still failing
**Solution**: Check both mock DNS and HTTP allowance are enabled
```bash
# Both should be true for local testing
echo "Mock: $AMTP_DNS_MOCK_MODE, HTTP: $AMTP_DNS_ALLOW_HTTP"
```

### Problem: Configuration not loading
**Solution**: Restart the gateway after changing environment variables
```bash
pkill agentry
./local-dev.sh  # or your custom configuration
```

This configurable approach provides secure defaults while enabling flexible local development without compromising production security.