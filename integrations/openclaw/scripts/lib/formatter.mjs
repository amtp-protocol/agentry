/**
 * Human-readable output formatters for AMTP CLI.
 * OpenClaw AI reads stdout, so output is structured plain text.
 */

function ts(isoString) {
  if (!isoString) return '';
  const d = new Date(isoString);
  return d.toLocaleString();
}

/**
 * Format inbox response as a numbered message list.
 */
export function formatInbox(response) {
  if (!response?.messages?.length) {
    return 'Inbox is empty.';
  }

  const lines = [`Inbox for ${response.recipient} (${response.count} message${response.count !== 1 ? 's' : ''}):\n`];

  for (let i = 0; i < response.messages.length; i++) {
    const m = response.messages[i];
    lines.push(`  ${i + 1}. [${m.message_id}]`);
    lines.push(`     From:    ${m.sender}`);
    if (m.subject) lines.push(`     Subject: ${m.subject}`);
    lines.push(`     Time:    ${ts(m.timestamp)}`);
    if (m.payload !== undefined && m.payload !== null) {
      const payloadStr = typeof m.payload === 'string' ? m.payload : JSON.stringify(m.payload);
      lines.push(`     Payload: ${payloadStr}`);
    }
    lines.push('');
  }

  return lines.join('\n');
}

/**
 * Format agent discovery response as a list.
 */
export function formatAgents(response) {
  const agents = Array.isArray(response) ? response : response?.agents || [];
  if (!agents.length) {
    return 'No agents found.';
  }

  const lines = [`Discovered ${agents.length} agent${agents.length !== 1 ? 's' : ''}:\n`];

  for (const a of agents) {
    const addr = a.address || a.name || 'unknown';
    const mode = a.delivery_mode || '?';
    const schemas = a.supported_schemas?.length
      ? ` schemas=[${a.supported_schemas.join(', ')}]`
      : '';
    lines.push(`  - ${addr}  (${mode})${schemas}`);
  }

  return lines.join('\n');
}

/**
 * Format message delivery status.
 */
export function formatStatus(response) {
  const lines = [`Message ${response.message_id || response.id || '?'}:`];
  lines.push(`  Status: ${response.status || 'unknown'}`);

  const recipients = response.recipients || [];
  if (recipients.length) {
    lines.push('  Recipients:');
    for (const r of recipients) {
      let detail = `    - ${r.address}: ${r.status}`;
      if (r.delivery_mode) detail += ` (${r.delivery_mode})`;
      if (r.acknowledged) detail += ' [ACK]';
      if (r.error_message) detail += ` error: ${r.error_message}`;
      lines.push(detail);
    }
  }

  return lines.join('\n');
}

/**
 * Format a single sent message response.
 */
export function formatMessage(response) {
  const lines = [];
  lines.push(`Message sent successfully.`);
  lines.push(`  ID:     ${response.message_id}`);
  lines.push(`  Status: ${response.status}`);

  const recipients = response.recipients || [];
  if (recipients.length) {
    for (const r of recipients) {
      lines.push(`  -> ${r.address}: ${r.status}`);
    }
  }

  return lines.join('\n');
}
