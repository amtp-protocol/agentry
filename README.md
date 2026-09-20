# Agentry

Orchestration and memory for multi-agent systems, implementing the Agent Message Transfer Protocol (AMTP) v1.0.

## Overview

Agentry provides intelligent workflow orchestration and organizational memory for multi-agent AI systems. When your AI planner generates a dynamic workflow at runtime, Agentry coordinates execution, shares context across agents, and accumulates decision traces that make your agents smarter over time.

## Features

### Protocol & Messaging
- **Universal Addressing** - `agent@domain` format with DNS-based discovery
- **Federated Architecture** - Decentralized communication across organizational domains
- **Dual Delivery Modes** - Pull-based inbox storage or push-based webhook delivery
- **At-Least-Once Delivery** - Guaranteed reliability with idempotency

### Infrastructure
- **Kubernetes Integration** - Workflow-aware scheduling on container orchestration
- **Schema Validation** - AGNTCY framework support for structured data
- **Security** - TLS 1.3, API key authentication, and access control
- **Observability** - Health checks, metrics, and structured logging


### Workflow Orchestration
- **Dynamic Execution** - AI planners generate workflows; Agentry executes them (parallel, sequential, conditional)
- **Automatic Context Sharing** - Workflow context injected seamlessly across agents
- **Context Engineering** - Token-aware context selection, summarization, and relevance filtering
- **Smart Routing** - Capability-based agent discovery without hardcoded addresses
- **Framework Agnostic** - Works with LangGraph, CrewAI, or any custom agents

### Context Graph (Organizational Memory)
- **Decision Traces** - Capture what was decided, why, and what alternatives were rejected
- **Precedent Queries** - Agents query past decisions to inform current choices
- **Cross-Agent Visibility** - Traces shared across agents and workflows
- **Federation** - Query traces across organizational boundaries via AMTP
- **Semantic Handoffs** - Preserve intent, reasoning, and constraints across agent handoffs
> **See [ContextGraph.md](./docs/ContextGraph.md) for full documentation**

## Why Agentry?

### The Problem with Current Multi-Agent Systems

**Kubernetes** orchestrates containers brilliantly but has no understanding of agent workflows.

**Kubeflow** only supports static DAGs - can't handle workflows generated dynamically by AI planners.

**Agent Frameworks** (LangGraph, CrewAI) lock you into single-framework ecosystems and require manual context passing.

**A2A Protocol** defines agent communication but lacks workflow orchestration - it's a wire protocol, not an execution engine.

**Every agent framework** treats each workflow as starting from scratch - no memory of past decisions, no precedents, no organizational learning.

### Agentry's Solution

When your AI planner generates a workflow like:
> "Research market data in parallel across 3 agents, then synthesize results, but only if confidence > 0.8"

**Agentry:**
1. ✅ Understands the workflow structure (parallel → conditional → synthesis)
2. ✅ Coordinates agent execution with proper dependencies
3. ✅ Automatically shares context (research results) with synthesis agent
4. ✅ Manages state, timeouts, and retry logic
5. ✅ Integrates with Kubernetes for actual execution

**You get:** Workflow intelligence that Kubernetes lacks + Infrastructure orchestration that agent frameworks don't provide + Organizational memory that compounds over time.

### Key Differentiators

| Capability | Agentry | Kubernetes | Kubeflow | LangGraph/CrewAI | A2A |
|------------|---------|------------|----------|------------------|-----|
| Dynamic AI-generated workflows | ✅ | ❌ | ❌ | ✅ | ❌ |
| Automatic context sharing | ✅ | ❌ | ❌ | Manual | ❌ |
| Organizational memory (context graph) | ✅ | ❌ | ❌ | ❌ | ❌ |
| Framework agnostic | ✅ | ✅ | ✅ | ❌ | ✅ |
| Infrastructure orchestration | ✅ | ✅ | ✅ | ❌ | ❌ |
| Workflow-aware scheduling | ✅ | ❌ | ❌ | ❌ | ❌ |
| Cross-org federation | ✅ | ❌ | ❌ | ❌ | ✅ |

## Quick Start

Requires Go 1.21 or later.

```bash
git clone https://github.com/amtp-protocol/agentry.git
cd agentry
make build

# The admin API is refused until an admin key file is configured
openssl rand -hex 32 > admin.keys
export ADMIN_KEY=$(cat admin.keys)

AMTP_TLS_ENABLED=false AMTP_SERVER_ADDRESS=:8080 AMTP_DOMAIN=localhost \
  ./build/agentry -admin-key-file admin.keys
```

In another terminal (with `ADMIN_KEY` exported), register an agent, send it a message, and read its inbox:

```bash
# Register a pull-mode agent; the response contains its api_key
curl -X POST http://localhost:8080/v1/admin/agents \
  -H "Content-Type: application/json" \
  -H "X-Admin-Key: $ADMIN_KEY" \
  -d '{"address": "user", "delivery_mode": "pull"}'

curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "test@localhost",
    "recipients": ["user@localhost"],
    "subject": "Local Test Message",
    "payload": {"message": "Hello localhost!"}
  }'

curl -H "Authorization: Bearer <api_key>" \
     http://localhost:8080/v1/inbox/user@localhost
```

See [Getting Started](docs/GETTING_STARTED.md) for the full walkthrough, Docker usage, DNS setup, and troubleshooting.

## Documentation

| Guide | Contents |
|-------|----------|
| [Getting Started](docs/GETTING_STARTED.md) | Build, run locally, first message, Docker, DNS record, troubleshooting |
| [Configuration](docs/CONFIGURATION.md) | Command line flags, all `AMTP_*` environment variables, admin tool usage, production and development examples |
| [API Reference](docs/API.md) | Messaging, agent management, inbox, discovery, health, schema endpoints, API key security |
| [Deployment](docs/DEPLOYMENT.md) | Single and multi-domain deployments, Docker, Kubernetes, monitoring |
| [Domain Management](docs/DOMAIN_MANAGEMENT.md) | Domains, DNS records, and agent addressing |
| [Schema Framework](docs/SCHEMA.md) | AGNTCY schema registry and validation |
| [Context Graph](docs/ContextGraph.md) | Decision traces, precedent queries, federation |
| [Development](docs/DEVELOPMENT.md) | Building, tests, code quality, contributing |
| [Testing](docs/TESTING.md) | Test structure, [local DNS testing](docs/LOCAL_TESTING.md), [Docker simulation](docs/DOCKER_TESTING.md) |
| [Roadmap](docs/ROADMAP.md) | Development timeline and feature planning |

## Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                         AMTP Gateway                              │
├─────────────────┬───────────────────┬─────────────────────────────┤
│  HTTP Server    │  Message Queue    │    Protocol Bridge          │
│  - Receive      │  - Persistence    │    - AMTP ↔ SMTP            │
│  - Send         │  - Retry Logic    │    - Schema Conversion      │
│  - Status API   │  - DLQ            │    - Format Translation     │
├─────────────────┼───────────────────┼─────────────────────────────┤
│  DNS Resolver   │  Schema Engine    │    Coordination Engine      │
│  - Discovery    │  - Validation     │    - Workflow State         │
│  - Caching      │  - AGNTCY API     │    - Multi-Agent Logic      │
├─────────────────┼───────────────────┼─────────────────────────────┤
│  Context Graph  │  Context Manager  │    Delivery Engine          │
│  - Trace Store  │  - Workflow Ctx   │    - Push/Pull Modes        │
│  - Precedent API│  - Handoff Schema │    - Webhook HTTP           │
│  - Graph Links  │  - Compression    │    - Local Inbox            │
├─────────────────┼───────────────────┼─────────────────────────────┤
│  Auth Manager   │  Policy Engine    │    Monitoring               │
│  - TLS Certs    │  - Access Rules   │    - Metrics                │
│  - API Keys     │  - Trace Perms    │    - Logging                │
└─────────────────┴───────────────────┴─────────────────────────────┘
```

## Protocol Specification

This implementation follows the [AMTP Protocol Specification v1.0](https://github.com/amtp-protocol/amtp).

Key features:
- Universal addressing using `agent@domain` format
- Transparent protocol upgrade with SMTP bridging
- At-least-once delivery with idempotency guarantees
- Local agent management with pull/push delivery modes
- Standard schema integration via AGNTCY framework
- Multi-agent workflow coordination
- Federated architecture with DNS-based discovery

## Contributing

Contributions are welcome. See [Development](docs/DEVELOPMENT.md#contributing) for the workflow and guidelines; run `make ci` before submitting PRs.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Support

- 📖 [Documentation](./docs/)
- 🐛 [Issue Tracker](https://github.com/amtp-protocol/agentry/issues)
- 💬 [Discussions](https://github.com/amtp-protocol/agentry/discussions)

## Acknowledgments

- Built with [Gin](https://github.com/gin-gonic/gin) HTTP framework
- Follows [AGNTCY](https://agntcy.org) schema standards
- Implements federated messaging patterns inspired by email protocols
