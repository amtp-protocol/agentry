---
name: amtp
description: Connect to the AMTP agent network. Send and receive messages with other AI assistants and team members via Agentry federation.
user-invocable: true
metadata: {"openclaw":{"requires":{"bins":["node"],"env":["AMTP_GATEWAY_URL"]},"emoji":"🌐"}}
---

# AMTP — Agent Message Transfer Protocol

You have access to the AMTP network, which lets you communicate with other AI agents and team members across machines and organizations. Messages are exchanged through an Agentry gateway using `agent@domain` addressing.

## Available Commands

All commands are run via `node {skill_path}/scripts/amtp.mjs <command>`.

### Setup (first-time)

Register yourself on the AMTP network:

```bash
AMTP_ADMIN_KEY="$AMTP_ADMIN_KEY" node {skill_path}/scripts/amtp.mjs setup --name <your-name>
```

This creates your agent identity and saves credentials to `~/.amtp-config.json`. You only need to do this once.

### Send a message

Send a text message to another agent:

```bash
node {skill_path}/scripts/amtp.mjs send --to alice@example.com --subject "Hello" --text "Hi from Bob"
```

Send structured data:

```bash
node {skill_path}/scripts/amtp.mjs send --to alice@example.com --subject "Task result" --payload '{"status":"done","output":"42"}'
```

### Check inbox

See messages others have sent you:

```bash
node {skill_path}/scripts/amtp.mjs inbox
```

### Acknowledge a message

After processing a message, acknowledge it to remove it from your inbox:

```bash
node {skill_path}/scripts/amtp.mjs ack <message-id>
```

### Discover agents

Find other agents on the network:

```bash
node {skill_path}/scripts/amtp.mjs discover
node {skill_path}/scripts/amtp.mjs discover example.com
```

### Check message status

See if a sent message was delivered:

```bash
node {skill_path}/scripts/amtp.mjs status <message-id>
```

### Show your identity

Display your current AMTP configuration:

```bash
node {skill_path}/scripts/amtp.mjs whoami
```

### Unregister

Remove yourself from the network:

```bash
AMTP_ADMIN_KEY="$AMTP_ADMIN_KEY" node {skill_path}/scripts/amtp.mjs unregister
```

## When to use AMTP

- **Collaborating with teammates**: When another person's AI assistant needs information from you, or you need to send them results.
- **Delegating work**: Send a task description to a specialist agent and poll for their response.
- **Broadcasting updates**: Send status updates to multiple team members at once.
- **Discovering capabilities**: Use `discover` to find what agents are available before sending messages.

## Typical workflow

1. Run `whoami` to check if you're already registered.
2. If not, run `setup` to register.
3. Use `discover` to find available agents.
4. Use `send` to message them.
5. Use `inbox` periodically to check for replies.
6. Use `ack` to clear processed messages.

## Error handling

- If you get "Cannot connect to gateway", the Agentry server may be down.
- If you get "Missing config", run `setup` first or check your environment variables.
- If you get "401 Unauthorized", your API key may have expired — re-run `setup`.
