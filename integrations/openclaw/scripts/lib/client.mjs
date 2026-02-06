/**
 * HTTP client for the Agentry AMTP API.
 * Zero dependencies — uses Node 22+ built-in fetch.
 */

class AmtpError extends Error {
  constructor(statusCode, body) {
    const msg = body?.error?.message || `HTTP ${statusCode}`;
    super(msg);
    this.name = 'AmtpError';
    this.statusCode = statusCode;
    this.code = body?.error?.code || 'UNKNOWN';
    this.details = body?.error?.details || null;
  }
}

async function request(url, options = {}) {
  const res = await fetch(url, options);
  const contentType = res.headers.get('content-type') || '';

  if (!res.ok) {
    let body = {};
    if (contentType.includes('application/json')) {
      try { body = await res.json(); } catch { /* ignore parse errors */ }
    }
    throw new AmtpError(res.status, body);
  }

  if (res.status === 204 || !contentType.includes('application/json')) {
    const text = await res.text();
    return text || null;
  }

  return res.json();
}

function adminHeaders(cfg) {
  return {
    'Content-Type': 'application/json',
    'X-Admin-Key': cfg.adminKey,
  };
}

function agentHeaders(cfg) {
  return {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${cfg.apiKey}`,
  };
}

/**
 * Register a new agent on the gateway.
 * POST /v1/admin/agents
 * Body: { address: "<bare name>", delivery_mode: "pull" }
 * Returns: agent object with plain-text api_key (one-time).
 */
export async function registerAgent(cfg, name) {
  return request(`${cfg.gatewayUrl}/v1/admin/agents`, {
    method: 'POST',
    headers: adminHeaders(cfg),
    body: JSON.stringify({
      address: name,
      delivery_mode: 'pull',
    }),
  });
}

/**
 * Unregister an agent from the gateway.
 * DELETE /v1/admin/agents/:name
 */
export async function unregisterAgent(cfg, name) {
  return request(`${cfg.gatewayUrl}/v1/admin/agents/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    headers: adminHeaders(cfg),
  });
}

/**
 * Send a message to one or more recipients.
 * POST /v1/messages
 */
export async function sendMessage(cfg, { to, subject, payload }) {
  const recipients = Array.isArray(to) ? to : [to];
  const body = {
    sender: cfg.agentAddress,
    recipients,
  };
  if (subject) body.subject = subject;
  if (payload !== undefined) body.payload = payload;

  return request(`${cfg.gatewayUrl}/v1/messages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

/**
 * Get inbox messages for the current agent.
 * GET /v1/inbox/:address
 */
export async function getInbox(cfg) {
  return request(`${cfg.gatewayUrl}/v1/inbox/${encodeURIComponent(cfg.agentAddress)}`, {
    method: 'GET',
    headers: agentHeaders(cfg),
  });
}

/**
 * Acknowledge (remove) a message from the inbox.
 * DELETE /v1/inbox/:address/:messageId
 */
export async function ackMessage(cfg, messageId) {
  return request(
    `${cfg.gatewayUrl}/v1/inbox/${encodeURIComponent(cfg.agentAddress)}/${encodeURIComponent(messageId)}`,
    {
      method: 'DELETE',
      headers: agentHeaders(cfg),
    },
  );
}

/**
 * Check delivery status of a sent message.
 * GET /v1/messages/:id/status
 */
export async function getMessageStatus(cfg, messageId) {
  return request(`${cfg.gatewayUrl}/v1/messages/${encodeURIComponent(messageId)}/status`, {
    method: 'GET',
    headers: { 'Content-Type': 'application/json' },
  });
}

/**
 * Discover agents on the network.
 * GET /v1/discovery/agents or GET /v1/discovery/agents/:domain
 */
export async function discoverAgents(cfg, domain) {
  const path = domain
    ? `/v1/discovery/agents/${encodeURIComponent(domain)}`
    : '/v1/discovery/agents';
  return request(`${cfg.gatewayUrl}${path}`, {
    method: 'GET',
    headers: { 'Content-Type': 'application/json' },
  });
}

export { AmtpError };
