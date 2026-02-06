import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { homedir } from 'node:os';

const CONFIG_FILE = join(homedir(), '.amtp-config.json');

/**
 * Load config with priority: env vars > ~/.amtp-config.json > defaults.
 */
export async function loadConfig() {
  let fileConfig = {};
  try {
    const raw = await readFile(CONFIG_FILE, 'utf-8');
    fileConfig = JSON.parse(raw);
  } catch {
    // No config file yet — that's fine
  }

  return {
    gatewayUrl: process.env.AMTP_GATEWAY_URL || fileConfig.gatewayUrl || '',
    agentName: process.env.AMTP_AGENT_NAME || fileConfig.agentName || '',
    agentAddress: fileConfig.agentAddress || '',
    apiKey: process.env.AMTP_API_KEY || fileConfig.apiKey || '',
    adminKey: process.env.AMTP_ADMIN_KEY || fileConfig.adminKey || '',
  };
}

/**
 * Save config to ~/.amtp-config.json. Merges with existing values.
 */
export async function saveConfig(config) {
  let existing = {};
  try {
    const raw = await readFile(CONFIG_FILE, 'utf-8');
    existing = JSON.parse(raw);
  } catch {
    // Starting fresh
  }

  const merged = { ...existing, ...config };
  await writeFile(CONFIG_FILE, JSON.stringify(merged, null, 2) + '\n', 'utf-8');
  return merged;
}

export { CONFIG_FILE };
