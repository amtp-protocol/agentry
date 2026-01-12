# Context Graph

The Context Graph is Agentry's organizational memory system - a queryable store of decision traces that enables agents to learn from past workflows.

## Why Context Graph?

Without organizational memory:
- Each workflow starts from scratch
- Agents repeat the same mistakes
- Precedents live in human heads, not systems
- No learning across agents or workflows

With Context Graph:
- Agents query past decisions before acting
- Precedents compound over time
- Cross-agent, cross-workflow learning
- Federated memory across organizations

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           AGENT                                     │
│  (decides what to write, what to query, interprets results)         │
└─────────────────────────────────────────────────────────────────────┘
        │                                           ▲
        │ 1. Write trace                           │ 4. Return raw data
        │ 2. Query traces                          │    (agent interprets)
        │ 3. Link traces                           │
        ▼                                           │
┌─────────────────────────────────────────────────────────────────────┐
│                     AGENTRY (Infrastructure)                        │
│                                                                     │
│  - Stores traces (no interpretation)                                │
│  - Queries by metadata (exact match, not semantic)                  │
│  - Maintains links (agent tells it what to link)                    │
│  - Returns raw data (agent decides what's relevant)                 │
│                                                                     │
│  Does NOT: parse content, determine similarity, decide relevance    │
└─────────────────────────────────────────────────────────────────────┘
```

Agentry provides **storage and query infrastructure**. Agents provide **understanding and interpretation**.

### Separation of Concerns

| Agentry Does | Agent Does |
|--------------|------------|
| Store traces | Decide what to store |
| Query by metadata | Interpret query results |
| Maintain links | Decide what to link |
| Enforce permissions | Determine relevance |
| Federate across domains | Understand content |

## Core Concepts

### Decision Trace

A record of what was decided, why, and what context informed the decision.

```json
{
  "id": "trace-uuid",
  "workflow_id": "workflow-uuid",
  "agent": "sales-agent@acme.com",
  "trace_type": "decision",
  "entities": [
    {"type": "customer", "id": "acme-123"},
    {"type": "deal", "id": "deal-456"}
  ],
  "tags": ["discount", "enterprise", "renewal"],
  "payload": {
    "decision": "approve_27_percent",
    "reasoning": "Based on precedent X and Y...",
    "alternatives_rejected": ["30%: exceeds policy", "20%: customer walked before"]
  },
  "outcome": "approved"
}
```

### Trace Links

Relationships between traces that form the graph.

| Link Type | Meaning |
|-----------|---------|
| `based_on_precedent` | This decision referenced a past decision |
| `supersedes` | This decision overrides a previous one |
| `led_to` | This decision caused a subsequent decision |
| `approved_by` | Links decision to approval trace |

### Visibility Levels

| Level | Who Can Access |
|-------|----------------|
| `private` | Only the creating agent |
| `workflow` | All agents in the same workflow |
| `domain` | All agents in the same domain |
| `public` | Any agent (with policy approval) |

## API Reference

### Trace Management

```http
# Create a decision trace
POST /v1/traces
{
  "workflow_id": "uuid",
  "agent": "sales-agent@acme.com",
  "trace_type": "decision",
  "entities": [{"type": "customer", "id": "acme-123"}],
  "tags": ["discount", "enterprise"],
  "visibility": "domain",
  "payload": { ... }
}

# Get a trace
GET /v1/traces/{trace_id}

# Update a trace (add outcome, feedback)
PATCH /v1/traces/{trace_id}
{
  "outcome": "approved",
  "payload": { ... }
}

# Delete a trace
DELETE /v1/traces/{trace_id}
```

### Trace Queries

```http
# Query by metadata (exact match, not semantic)
POST /v1/traces/query
{
  "filters": {
    "agent": "sales-agent@acme.com",
    "trace_type": "decision",
    "tags": ["discount", "enterprise"],
    "outcome": "approved",
    "entities": [{"type": "customer", "id": "acme-123"}],
    "created_after": "2025-01-01T00:00:00Z",
    "created_before": "2025-06-01T00:00:00Z"
  },
  "pagination": {
    "limit": 50,
    "offset": 0
  }
}

# Get traces for a workflow
GET /v1/workflows/{workflow_id}/traces

# Get traces for an entity
GET /v1/entities/{type}/{id}/traces
```

### Trace Linking

```http
# Create a link
POST /v1/traces/{trace_id}/links
{
  "target_trace_id": "uuid",
  "link_type": "based_on_precedent"
}

# Get links for a trace
GET /v1/traces/{trace_id}/links?direction=outbound

# Traverse the graph (follow links)
GET /v1/traces/{trace_id}/chain?link_types=based_on_precedent&max_depth=5

# Delete a link
DELETE /v1/traces/{trace_id}/links/{link_id}
```

### Admin Operations

```http
# Get trace statistics
GET /v1/admin/traces/stats?group_by=agent

# Prune old traces
DELETE /v1/admin/traces?created_before=2024-01-01T00:00:00Z
```

## Permissions

Traces use policy-based access control, extending Agentry's existing policy engine.

### Policy Configuration

```yaml
policies:
  # Default: domain isolation
  - name: domain-isolation
    effect: allow
    actions: [read, write, link]
    principal: "*.${trace.owner_domain}"
    resource: "traces/*"

  # Role-based access
  - name: sales-discount-access
    effect: allow
    actions: [read]
    principal: "sales-*@acme.com"
    conditions:
      tags_contain: ["discount"]

  # Cross-domain sharing
  - name: partner-access
    effect: allow
    actions: [read]
    principal: "*@partner.com"
    conditions:
      tags_contain: ["partner-visible"]

  # Workflow participants
  - name: workflow-access
    effect: allow
    actions: [read, link]
    principal: "@workflow:${trace.workflow_id}"
```

### Permission Evaluation

```
Agent calls API
       │
       ▼
1. Authenticate (API key → agent@domain)
       │
       ▼
2. Check trace ACL (if explicit override exists)
       │
       ▼
3. Evaluate policies (priority order, first match wins)
       │
       ▼
4. Default: deny
```

## Semantic Handoffs

Context Graph enhances agent handoffs beyond simple message passing.

### The Problem

Most orchestration treats handoffs as simple message passing:

```
Agent A output → JSON blob → Agent B input
```

Agent B loses:
- Why A made that choice
- What alternatives A rejected
- What assumptions A made
- The original intent
- A's uncertainty/confidence

### Structured Handoff Protocol

```yaml
handoff:
  # What was decided
  output:
    top_vendors: ["AWS", "Azure", "GCP"]

  # Why (reasoning chain)
  reasoning:
    - "Started with 12 vendors from Gartner quadrant"
    - "Filtered 8 for SOC2 compliance requirement"
    - "Ranked by TCO estimate"

  # Original intent (preserved from start)
  intent: "Find best vendor for cloud migration"

  # Constraints that must carry forward
  constraints:
    - compliance: "SOC2 required"
    - timeline: "6 months"

  # What I'm uncertain about
  uncertainty:
    - field: "azure_pricing"
      confidence: 0.6
      reason: "Pricing model unclear"

  # What I rejected and why
  rejected:
    - option: "Oracle"
      reason: "Failed SOC2 requirement"

  # Assumptions I made
  assumptions:
    - "Budget is flexible"
    - "Multi-cloud is acceptable"
```

### Intent Threading

Agentry preserves the original intent across all handoffs:

```
┌─────────────────────────────────────────────────────────────┐
│                    WORKFLOW INTENT                          │
│  "Find best vendor for cloud migration"                     │
│                                                             │
│  Automatically injected into every agent's context          │
│  Agents can refine but not lose original intent             │
└─────────────────────────────────────────────────────────────┘
          │              │              │
          ▼              ▼              ▼
      Agent A        Agent B        Agent C
     (Research)    (Negotiate)     (Contract)
```

## Context Compression

For long workflows, context accumulates and can exceed limits. Agentry manages this through tiered storage.

### Compression Tiers

| Tier | Content | Use Case |
|------|---------|----------|
| **Hot** | Full detail | Last N steps |
| **Warm** | Compressed summaries | Older steps |
| **Cold** | References only | Archived, fetch on demand |

### Configuration

```yaml
context:
  limits:
    max_tokens: 8000
    hot_window: 2        # Last N handoffs at full detail

  compression:
    # Option A: Dedicated compression agent
    compressor: "summarizer-agent@acme.com"

    # Option B: External service
    # compressor: "https://api.acme.com/compress"

    # Option C: Built-in (structured extraction only)
    # compressor: "builtin:extract-structured"

  preserve_always:
    - intent
    - semantic_anchors
    - constraints
    - active_uncertainties

  compress_aggressively:
    - rejected_options
    - intermediate_reasoning
    - raw_data
```

### On-Demand Expansion

Agents can request full context for compressed sections:

```http
# Agent receives compressed context with reference
{
  "step_3": {
    "compressed": "Evaluated 12 vendors, filtered to 3 based on compliance",
    "full_ref": "ctx://W-123/step-3/full"
  }
}

# Agent requests expansion
GET /v1/context/W-123/step-3/full
```

## Storage Schema

### Database Tables

```sql
-- Core trace table
CREATE TABLE traces (
    id              UUID PRIMARY KEY,
    workflow_id     UUID,
    message_id      UUID,
    owner_agent     VARCHAR(255) NOT NULL,
    owner_domain    VARCHAR(255) NOT NULL,
    trace_type      VARCHAR(50) NOT NULL,
    visibility      VARCHAR(20) DEFAULT 'domain',
    outcome         VARCHAR(50),
    tags            TEXT[],
    payload         JSONB,
    acl             JSONB,          -- optional explicit overrides
    created_at      TIMESTAMP,
    updated_at      TIMESTAMP
);

-- Entity references (for querying)
CREATE TABLE trace_entities (
    trace_id        UUID REFERENCES traces(id),
    entity_type     VARCHAR(100),
    entity_id       VARCHAR(255),
    PRIMARY KEY (trace_id, entity_type, entity_id)
);

-- Links between traces
CREATE TABLE trace_links (
    id              UUID PRIMARY KEY,
    source_trace_id UUID REFERENCES traces(id),
    target_trace_id UUID REFERENCES traces(id),
    link_type       VARCHAR(50),
    created_at      TIMESTAMP
);

-- Indexes
CREATE INDEX idx_traces_owner_domain ON traces(owner_domain);
CREATE INDEX idx_traces_workflow ON traces(workflow_id);
CREATE INDEX idx_traces_type ON traces(trace_type);
CREATE INDEX idx_traces_tags ON traces USING GIN(tags);
CREATE INDEX idx_trace_entities_lookup ON trace_entities(entity_type, entity_id);
```

## Value Proposition

### For Agent Developers

| Without Context Graph | With Context Graph |
|-----------------------|-------------------|
| Build your own trace storage | Standard API, ready to use |
| No cross-agent visibility | Query any agent's traces (with permission) |
| Manual context passing | Structured handoffs with reasoning |
| Each workflow isolated | Cross-workflow precedent search |

### For Organizations

| Without Context Graph | With Context Graph |
|-----------------------|-------------------|
| Knowledge in human heads | Knowledge in queryable store |
| Agents repeat mistakes | Learn from past failures |
| No audit trail for decisions | Full decision lineage |
| Siloed agent systems | Federated organizational memory |

### Real-World Example

**Without Context Graph:**
```
Workflow: Handle discount request for Acme Corp

Agent receives: "Acme wants 30% discount"
Agent thinks: "I have no idea what's normal here"
Agent outputs: "Recommend 15% based on general policy"
Human overrides: "Actually we always give enterprise 25%"

Next month, same situation, agent still doesn't know.
```

**With Context Graph:**
```
Workflow: Handle discount request for Acme Corp

1. Agent queries: "discount decisions + enterprise + similar ARR"
2. Graph returns:
   - "BigCo renewal: 25% approved, VP exception"
   - "MegaCorp renewal: 28% approved, precedent: BigCo"
3. Agent outputs: "Recommend 27% discount, citing precedents"
4. Decision stored with links to precedents
5. Next similar case automatically gets these precedents
```

## Integration with AMTP

Context Graph extends AMTP with trace-aware messaging:

### Trace References in Messages

```json
{
  "sender": "sales-agent@acme.com",
  "recipients": ["approval-agent@acme.com"],
  "payload": {
    "action": "request_discount_approval",
    "amount": "27%"
  },
  "trace_refs": [
    "trace://acme.com/T-456",
    "trace://acme.com/T-789"
  ]
}
```

### Federated Trace Queries

Traces can be queried across organizational boundaries using the same federation mechanism as messages:

```http
# Query partner's traces (requires cross-domain policy)
POST /v1/traces/query
{
  "filters": {
    "domains": ["partner.com"],
    "tags": ["partner-visible", "integration"]
  }
}
```
