#!/usr/bin/env node

/**
 * AMTP CLI — OpenClaw skill entrypoint.
 *
 * Usage:
 *   amtp.mjs setup --name <name>
 *   amtp.mjs send --to <addr> --subject <s> [--payload <json> | --text <t>]
 *   amtp.mjs inbox
 *   amtp.mjs ack <message-id>
 *   amtp.mjs discover [domain]
 *   amtp.mjs status <message-id>
 *   amtp.mjs unregister
 *   amtp.mjs whoami
 */

import { loadConfig, saveConfig, CONFIG_FILE } from './lib/config.mjs';
import {
  registerAgent,
  unregisterAgent,
  sendMessage,
  getInbox,
  ackMessage,
  getMessageStatus,
  discoverAgents,
  AmtpError,
} from './lib/client.mjs';
import {
  formatInbox,
  formatAgents,
  formatStatus,
  formatMessage,
} from './lib/formatter.mjs';

// ── Argument parsing ──────────────────────────────────────────────

function parseArgs(argv) {
  const args = argv.slice(2);
  const command = args[0] || '';
  const positional = [];
  const flags = {};

  for (let i = 1; i < args.length; i++) {
    if (args[i].startsWith('--')) {
      const key = args[i].slice(2);
      const next = args[i + 1];
      if (next && !next.startsWith('--')) {
        flags[key] = next;
        i++;
      } else {
        flags[key] = true;
      }
    } else {
      positional.push(args[i]);
    }
  }

  return { command, positional, flags };
}

// ── Helpers ───────────────────────────────────────────────────────

function die(msg) {
  console.error(`Error: ${msg}`);
  process.exit(1);
}

function requireConfig(cfg, ...fields) {
  for (const f of fields) {
    if (!cfg[f]) {
      const hint = f === 'gatewayUrl'
        ? 'Set AMTP_GATEWAY_URL env var or run setup first.'
        : f === 'adminKey'
          ? 'Set AMTP_ADMIN_KEY env var.'
          : f === 'apiKey'
            ? 'Run "amtp setup" first, or set AMTP_API_KEY.'
            : f === 'agentAddress'
              ? 'Run "amtp setup" first.'
              : `Set ${f}.`;
      die(`Missing config: ${f}. ${hint}`);
    }
  }
}

// ── Commands ──────────────────────────────────────────────────────

async function cmdSetup(cfg, { flags }) {
  const name = flags.name;
  if (!name) die('--name is required. Usage: amtp setup --name <agent-name>');

  requireConfig(cfg, 'gatewayUrl', 'adminKey');

  // Validate name format
  if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}[a-zA-Z0-9]$/.test(name) && !/^[a-zA-Z0-9]$/.test(name)) {
    die('Invalid agent name. Use letters, numbers, hyphens, underscores, dots (1-64 chars). Cannot start/end with -, _, or .');
  }

  console.log(`Registering agent "${name}" on ${cfg.gatewayUrl}...`);
  const result = await registerAgent(cfg, name);

  // The response is the agent object with a one-time plain-text api_key
  const agentAddress = result.address;
  const apiKey = result.api_key;

  if (!apiKey) {
    die('Registration succeeded but no API key was returned. The agent may already exist.');
  }

  await saveConfig({
    gatewayUrl: cfg.gatewayUrl,
    agentName: name,
    agentAddress,
    apiKey,
  });

  console.log(`Agent registered successfully.`);
  console.log(`  Address: ${agentAddress}`);
  console.log(`  Config saved to: ${CONFIG_FILE}`);
  console.log(`\nYou can now send and receive messages.`);
}

async function cmdSend(cfg, { flags }) {
  requireConfig(cfg, 'gatewayUrl', 'agentAddress');

  const to = flags.to;
  if (!to) die('--to is required. Usage: amtp send --to <address> --subject <s> [--text <t> | --payload <json>]');

  const subject = flags.subject || '';

  let payload;
  if (flags.payload) {
    try {
      payload = JSON.parse(flags.payload);
    } catch {
      die('--payload must be valid JSON.');
    }
  } else if (flags.text) {
    payload = { text: flags.text };
  }

  const result = await sendMessage(cfg, { to, subject, payload });
  console.log(formatMessage(result));
}

async function cmdInbox(cfg) {
  requireConfig(cfg, 'gatewayUrl', 'agentAddress', 'apiKey');

  const result = await getInbox(cfg);
  console.log(formatInbox(result));
}

async function cmdAck(cfg, { positional }) {
  const messageId = positional[0];
  if (!messageId) die('Message ID is required. Usage: amtp ack <message-id>');

  requireConfig(cfg, 'gatewayUrl', 'agentAddress', 'apiKey');

  await ackMessage(cfg, messageId);
  console.log(`Message ${messageId} acknowledged.`);
}

async function cmdDiscover(cfg, { positional }) {
  requireConfig(cfg, 'gatewayUrl');

  const domain = positional[0];
  const result = await discoverAgents(cfg, domain);
  console.log(formatAgents(result));
}

async function cmdStatus(cfg, { positional }) {
  const messageId = positional[0];
  if (!messageId) die('Message ID is required. Usage: amtp status <message-id>');

  requireConfig(cfg, 'gatewayUrl');

  const result = await getMessageStatus(cfg, messageId);
  console.log(formatStatus(result));
}

async function cmdUnregister(cfg) {
  requireConfig(cfg, 'gatewayUrl', 'adminKey', 'agentName');

  console.log(`Unregistering agent "${cfg.agentName}"...`);
  await unregisterAgent(cfg, cfg.agentName);
  console.log(`Agent "${cfg.agentName}" removed from gateway.`);
}

async function cmdWhoami(cfg) {
  const show = (label, val) => console.log(`  ${label.padEnd(16)} ${val || '(not set)'}`);

  console.log('AMTP Configuration:');
  show('Gateway URL:', cfg.gatewayUrl);
  show('Agent Name:', cfg.agentName);
  show('Agent Address:', cfg.agentAddress);
  show('API Key:', cfg.apiKey ? cfg.apiKey.slice(0, 8) + '...' : '');
  show('Admin Key:', cfg.adminKey ? cfg.adminKey.slice(0, 8) + '...' : '');
  show('Config File:', CONFIG_FILE);
}

function printHelp() {
  console.log(`AMTP CLI — Agent Message Transfer Protocol

Usage: amtp.mjs <command> [options]

Commands:
  setup --name <name>          Register as an AMTP agent, save config
  send --to <addr> --subject <s> [--payload <json> | --text <t>]
                               Send a message to another agent
  inbox                        Check inbox for messages
  ack <message-id>             Acknowledge/remove a message
  discover [domain]            List agents on the network
  status <message-id>          Check delivery status of a message
  unregister                   Remove agent from gateway
  whoami                       Show current configuration

Environment:
  AMTP_GATEWAY_URL             Gateway URL (required)
  AMTP_ADMIN_KEY               Admin API key (for setup/unregister)
  AMTP_AGENT_NAME              Agent name override
  AMTP_API_KEY                 Agent API key override`);
}

// ── Main ──────────────────────────────────────────────────────────

async function main() {
  const parsed = parseArgs(process.argv);

  if (!parsed.command || parsed.command === 'help' || parsed.flags?.help) {
    printHelp();
    return;
  }

  const cfg = await loadConfig();

  const commands = {
    setup: cmdSetup,
    send: cmdSend,
    inbox: cmdInbox,
    ack: cmdAck,
    discover: cmdDiscover,
    status: cmdStatus,
    unregister: cmdUnregister,
    whoami: cmdWhoami,
  };

  const handler = commands[parsed.command];
  if (!handler) {
    die(`Unknown command: "${parsed.command}". Run "amtp.mjs help" for usage.`);
  }

  try {
    await handler(cfg, parsed);
  } catch (err) {
    if (err instanceof AmtpError) {
      console.error(`Error (${err.code}): ${err.message}`);
      if (err.details) console.error(`  Details: ${JSON.stringify(err.details)}`);
      process.exit(1);
    }
    if (err.cause?.code === 'ECONNREFUSED') {
      die(`Cannot connect to gateway at ${cfg.gatewayUrl}. Is Agentry running?`);
    }
    throw err;
  }
}

main();
