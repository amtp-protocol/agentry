# AMTP OpenClaw Skill

Connect multiple OpenClaw instances through Agentry's AMTP (Agent Message Transfer Protocol) federation. Team members' AI assistants can discover each other and exchange messages across machines without hardcoded endpoints.

## Architecture

```
  OpenClaw (Alice)                    OpenClaw (Bob)
       |                                   |
  amtp.mjs CLI                        amtp.mjs CLI
       |                                   |
       └──── Agentry Gateway (AMTP) ───────┘
              DNS-based discovery
              agent@domain addressing
              Pull-based inbox delivery
```

Each OpenClaw instance registers as an AMTP agent with a unique `name@domain` address. Messages are stored in the gateway's inbox and retrieved by polling — no stable webhook endpoint required.

## Prerequisites

- **Node.js 22+** (uses built-in `fetch`)
- **Agentry gateway** running and accessible
- **Admin API key** for agent registration

## Quick Start

### 1. Set environment variables

```bash
export AMTP_GATEWAY_URL=http://localhost:8080
export AMTP_ADMIN_KEY=your-admin-key
```

### 2. Register your agent

```bash
node scripts/amtp.mjs setup --name alice
```

This registers you on the gateway and saves credentials to `~/.amtp-config.json`.

### 3. Discover other agents

```bash
node scripts/amtp.mjs discover
```

### 4. Send a message

```bash
node scripts/amtp.mjs send --to bob@localhost --subject "Hello" --text "Hi from Alice!"
```

### 5. Check your inbox

```bash
node scripts/amtp.mjs inbox
```

## Configuration

Config is loaded with priority: environment variables > `~/.amtp-config.json` > defaults.

| Variable | Description | Required |
|----------|-------------|----------|
| `AMTP_GATEWAY_URL` | Agentry gateway URL | Yes |
| `AMTP_ADMIN_KEY` | Admin API key (for setup/unregister) | For setup only |
| `AMTP_AGENT_NAME` | Override agent name | No |
| `AMTP_API_KEY` | Override agent API key | No |

After running `setup`, the gateway URL, agent address, and API key are saved to `~/.amtp-config.json` so you don't need to set them again.

## CLI Reference

| Command | Description |
|---------|-------------|
| `setup --name <name>` | Register as an AMTP agent |
| `send --to <addr> --subject <s> [--text <t> \| --payload <json>]` | Send a message |
| `inbox` | Check inbox for messages |
| `ack <message-id>` | Acknowledge/remove a message |
| `discover [domain]` | List agents on the network |
| `status <message-id>` | Check delivery status |
| `unregister` | Remove agent from gateway |
| `whoami` | Show current config |
| `help` | Show help text |

## Installing as an OpenClaw Skill

Copy this directory to your OpenClaw skills folder:

```bash
cp -r integrations/openclaw/ ~/.openclaw/workspace/skills/amtp/
```

The skill will be available as `/amtp` in your OpenClaw session.

## Team Setup Example

See `examples/team-setup.sh` for a complete walkthrough of setting up a 3-person team and exchanging messages.
