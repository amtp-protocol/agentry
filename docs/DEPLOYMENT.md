# AMTP Gateway Deployment Guide

This guide covers deploying AMTP gateways in production environments, with a focus on the "one gateway = one domain" architecture.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Domain Configuration](#domain-configuration)
3. [Single Domain Deployment](#single-domain-deployment)
4. [Multi-Domain Deployment](#multi-domain-deployment)
5. [Docker Deployments](#docker-deployments)
6. [Kubernetes Deployments](#kubernetes-deployments)
7. [DNS Configuration](#dns-configuration)
8. [Security Considerations](#security-considerations)
9. [Monitoring and Observability](#monitoring-and-observability)
10. [Troubleshooting](#troubleshooting)

## Architecture Overview

Each AMTP gateway instance manages exactly **one domain**. This design provides:

- **Clear Ownership**: Each domain has a dedicated gateway
- **Security Isolation**: No cross-domain agent registration
- **Independent Scaling**: Scale each domain independently
- **Simplified Management**: Clear responsibility boundaries

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Gateway A     │    │   Gateway B     │    │   Gateway C     │
│ company-a.com   │    │ subsidiary.com  │    │  partner.com    │
│                 │    │                 │    │                 │
│ Agents:         │    │ Agents:         │    │ Agents:         │
│ • sales@...     │    │ • hr@...        │    │ • api@...       │
│ • support@...   │    │ • finance@...   │    │ • webhook@...   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Domain Configuration

### Environment Variables (Recommended)

```bash
# Required: The domain this gateway manages
export AMTP_DOMAIN="company-a.com"

# Optional: Server address (default: :8443)
export AMTP_SERVER_ADDRESS=":8443"

# TLS Configuration
export AMTP_TLS_ENABLED=true
export AMTP_TLS_CERT_FILE="/etc/ssl/certs/company-a.com.crt"
export AMTP_TLS_KEY_FILE="/etc/ssl/private/company-a.com.key"

# Authentication
export AMTP_AUTH_REQUIRED=true
export AMTP_AUTH_ADMIN_KEY_FILE="/etc/ssl/admin/admin.key"
```

### YAML Configuration

```yaml
# config/production.yaml
server:
  address: ":8443"
  domain: "company-a.com"  # ← This is the managed domain
  read_timeout: "30s"
  write_timeout: "30s"
  idle_timeout: "120s"

tls:
  enabled: true
  cert_file: "/etc/ssl/certs/company-a.com.crt"
  key_file: "/etc/ssl/private/company-a.com.key"
  min_version: "1.3"

auth:
  require_auth: true
  admin_key_file: "/etc/ssl/admin/admin.key"

logging:
  level: "info"
  format: "json"
```

### Domain Validation

The gateway validates domain configuration at startup:

- ✅ **Valid**: `company-a.com`, `api.example.org`, `localhost`
- ❌ **Invalid**: `invalid_domain`, `domain-.com`, `toolongdomainname...`

## Single Domain Deployment

### Systemd Service

```ini
# /etc/systemd/system/amtp-gateway.service
[Unit]
Description=AMTP Gateway for company-a.com
After=network.target

[Service]
Type=simple
User=amtp
Group=amtp
WorkingDirectory=/opt/amtp
ExecStart=/opt/amtp/agentry -config /etc/amtp/config.yaml
Environment=AMTP_DOMAIN=company-a.com
Environment=AMTP_TLS_CERT_FILE=/etc/ssl/certs/company-a.com.crt
Environment=AMTP_TLS_KEY_FILE=/etc/ssl/private/company-a.com.key
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Direct Binary Execution

```bash
# Production deployment
AMTP_DOMAIN="company-a.com" \
AMTP_SERVER_ADDRESS=":8443" \
AMTP_TLS_ENABLED=true \
AMTP_TLS_CERT_FILE="/etc/ssl/certs/company-a.com.crt" \
AMTP_TLS_KEY_FILE="/etc/ssl/private/company-a.com.key" \
AMTP_AUTH_REQUIRED=true \
AMTP_AUTH_ADMIN_KEY_FILE="/etc/ssl/admin/admin.key" \
./agentry
```

## Multi-Domain Deployment

### Separate Processes

```bash
# Terminal 1: Gateway for company-a.com
AMTP_DOMAIN="company-a.com" \
AMTP_SERVER_ADDRESS=":8443" \
./agentry &

# Terminal 2: Gateway for subsidiary.com  
AMTP_DOMAIN="subsidiary.com" \
AMTP_SERVER_ADDRESS=":8444" \
./agentry &

# Terminal 3: Gateway for partner.com
AMTP_DOMAIN="partner.com" \
AMTP_SERVER_ADDRESS=":8445" \
./agentry &
```

### Systemd Services

```bash
# Create separate service files for each domain
sudo cp amtp-gateway.service amtp-gateway-company-a.service
sudo cp amtp-gateway.service amtp-gateway-subsidiary.service
sudo cp amtp-gateway.service amtp-gateway-partner.service

# Edit each service file with appropriate domain and port
sudo systemctl daemon-reload
sudo systemctl enable amtp-gateway-company-a
sudo systemctl enable amtp-gateway-subsidiary
sudo systemctl enable amtp-gateway-partner

sudo systemctl start amtp-gateway-company-a
sudo systemctl start amtp-gateway-subsidiary
sudo systemctl start amtp-gateway-partner
```

## Docker Deployments

### Single Domain Container

```bash
# Build the image
docker build -t amtp-gateway -f docker/Dockerfile .

# Run for company-a.com
docker run -d \
  --name amtp-gateway-company-a \
  -e AMTP_DOMAIN="company-a.com" \
  -e AMTP_SERVER_ADDRESS=":8443" \
  -e AMTP_TLS_ENABLED=true \
  -e AMTP_TLS_CERT_FILE=/etc/ssl/certs/company-a.com.crt \
  -e AMTP_TLS_KEY_FILE=/etc/ssl/private/company-a.com.key \
  -v /path/to/certs:/etc/ssl/certs:ro \
  -v /path/to/keys:/etc/ssl/private:ro \
  -p 8443:8443 \
  amtp-gateway
```

### Multi-Domain with Docker Compose

```bash
# Production deployment
docker-compose -f docker/docker-compose.multi-domain.yml up -d

# Local development
docker-compose -f docker/docker-compose.local-dev.yml up -d
```

The compose files include:
- Multiple gateway instances (one per domain)
- Nginx reverse proxy for domain-based routing
- Prometheus monitoring
- Grafana dashboards

## Kubernetes Deployments

### Single Domain Deployment

```yaml
# k8s/company-a-gateway.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: amtp-gateway-company-a
  namespace: amtp
spec:
  replicas: 3
  selector:
    matchLabels:
      app: amtp-gateway
      domain: company-a.com
  template:
    metadata:
      labels:
        app: amtp-gateway
        domain: company-a.com
    spec:
      containers:
      - name: gateway
        image: amtp-gateway:latest
        env:
        - name: AMTP_DOMAIN
          value: "company-a.com"
        - name: AMTP_SERVER_ADDRESS
          value: ":8443"
        - name: AMTP_TLS_ENABLED
          value: "true"
        - name: AMTP_TLS_CERT_FILE
          value: "/etc/ssl/certs/tls.crt"
        - name: AMTP_TLS_KEY_FILE
          value: "/etc/ssl/private/tls.key"
        ports:
        - containerPort: 8443
          name: https
        volumeMounts:
        - name: tls-certs
          mountPath: /etc/ssl/certs
          readOnly: true
        - name: tls-keys
          mountPath: /etc/ssl/private
          readOnly: true
        livenessProbe:
          httpGet:
            path: /health
            port: 8443
            scheme: HTTPS
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8443
            scheme: HTTPS
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: tls-certs
        secret:
          secretName: company-a-tls-cert
      - name: tls-keys
        secret:
          secretName: company-a-tls-key
---
apiVersion: v1
kind: Service
metadata:
  name: amtp-gateway-company-a
  namespace: amtp
spec:
  selector:
    app: amtp-gateway
    domain: company-a.com
  ports:
  - port: 443
    targetPort: 8443
    name: https
  type: LoadBalancer
```

### Multi-Domain with Ingress

```yaml
# k8s/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: amtp-gateways
  namespace: amtp
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
  - hosts:
    - company-a.com
    secretName: company-a-tls
  - hosts:
    - subsidiary.com
    secretName: subsidiary-tls
  - hosts:
    - partner.com
    secretName: partner-tls
  rules:
  - host: company-a.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: amtp-gateway-company-a
            port:
              number: 443
  - host: subsidiary.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: amtp-gateway-subsidiary
            port:
              number: 443
  - host: partner.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: amtp-gateway-partner
            port:
              number: 443
```

## DNS Configuration

Each domain needs DNS records to advertise AMTP capabilities:

```dns
# DNS TXT records for each domain
_amtp.company-a.com.    IN TXT "v=amtp1;gateway=https://company-a.com:443"
_amtp.subsidiary.com.   IN TXT "v=amtp1;gateway=https://subsidiary.com:443"
_amtp.partner.com.      IN TXT "v=amtp1;gateway=https://partner.com:443"

# A records pointing to gateway servers
company-a.com.          IN A    203.0.113.10
subsidiary.com.         IN A    203.0.113.11
partner.com.            IN A    203.0.113.12
```

## Security Considerations

### TLS Configuration

- **Always use TLS in production** (`AMTP_TLS_ENABLED=true`)
- **Use strong cipher suites** (TLS 1.3 preferred)
- **Regular certificate rotation**
- **Proper certificate validation**

### Authentication

- **Enable admin authentication** (`AMTP_AUTH_REQUIRED=true`)
- **Secure admin key storage**
- **Regular key rotation**
- **Principle of least privilege**

### Network Security

- **Firewall rules** (only allow necessary ports)
- **Network segmentation**
- **DDoS protection**
- **Rate limiting**

### Domain Validation

The gateway automatically validates:
- Domain format compliance
- Length restrictions
- Character validation
- Reserved name checking

## Monitoring and Observability

### Health Checks

```bash
# Health endpoint
curl -f https://company-a.com/health

# Readiness endpoint
curl -f https://company-a.com/ready

# Metrics endpoint
curl https://company-a.com/metrics
```

### Prometheus Metrics

Key metrics to monitor:
- `amtp_messages_total` - Total messages processed
- `amtp_messages_in_flight` - Current active messages
- `amtp_delivery_duration_seconds` - Message delivery latency
- `amtp_errors_total` - Error counts by type

### Logging

Configure structured logging:

```yaml
logging:
  level: "info"      # debug, info, warn, error
  format: "json"     # json, text
```

### Grafana Dashboards

The deployment includes pre-configured Grafana dashboards for:
- Message throughput and latency
- Error rates and types
- Gateway health and availability
- Resource utilization

## Troubleshooting

### Common Issues

#### Domain Configuration Errors

```bash
# Check domain validation
AMTP_DOMAIN="invalid_domain" ./agentry
# Error: invalid server domain: invalid domain format: invalid_domain
```

#### TLS Certificate Issues

```bash
# Verify certificate
openssl x509 -in /etc/ssl/certs/company-a.com.crt -text -noout

# Check certificate-key pair
openssl rsa -in /etc/ssl/private/company-a.com.key -check
```

#### Agent Registration Issues

```bash
# Register agent (name only)
agentry-admin agent register sales --mode pull

# Check agent status
agentry-admin agent list
```

#### DNS Discovery Issues

```bash
# Test DNS TXT record
dig TXT _amtp.company-a.com

# Test gateway health
curl https://company-a.com/health

# Test agent discovery
curl https://company-a.com/v1/discovery/agents
```

### Debug Mode

Enable debug logging for troubleshooting:

```bash
AMTP_LOGGING_LEVEL=debug ./agentry
```

### Log Analysis

```bash
# Filter error logs
journalctl -u amtp-gateway-company-a | grep ERROR

# Monitor real-time logs
journalctl -u amtp-gateway-company-a -f
```

## Best Practices

1. **One Gateway Per Domain**: Never configure multiple domains in one gateway
2. **Secure by Default**: Always use TLS and authentication in production
3. **Monitor Everything**: Set up comprehensive monitoring and alerting
4. **Regular Updates**: Keep gateways updated with security patches
5. **Backup Configuration**: Version control your configuration files
6. **Test Deployments**: Use staging environments for testing changes
7. **Document Everything**: Maintain clear deployment documentation

## Example Production Setup

Here's a complete example for a production deployment:

```bash
#!/bin/bash
# deploy-company-a.sh

# Set domain
export AMTP_DOMAIN="company-a.com"

# TLS Configuration
export AMTP_TLS_ENABLED=true
export AMTP_TLS_CERT_FILE="/etc/ssl/certs/company-a.com.crt"
export AMTP_TLS_KEY_FILE="/etc/ssl/private/company-a.com.key"

# Security
export AMTP_AUTH_REQUIRED=true
export AMTP_AUTH_ADMIN_KEY_FILE="/etc/ssl/admin/company-a.key"

# Performance
export AMTP_SERVER_ADDRESS=":8443"
export AMTP_MESSAGE_MAX_SIZE=10485760  # 10MB

# Logging
export AMTP_LOGGING_LEVEL=info
export AMTP_LOGGING_FORMAT=json

# Start gateway
./agentry -config /etc/amtp/production.yaml
```

This deployment guide provides everything needed to deploy AMTP gateways in production with proper domain management, security, and monitoring.
