import { readFile } from 'node:fs/promises';
import path from 'node:path';

import { maskValue } from './masking.mjs';

export const REQUIRED_ENV = [
  'UNODE_BASE_URL',
  'UNODE_RELAY_API_KEY',
  'NEW_API_BASE_URL',
  'NEW_API_RELAY_API_KEY',
  'NEW_API_CHANNEL_ID',
  'NEW_API_ADMIN_ACCESS_TOKEN',
  'NEW_API_ADMIN_USER_ID'
];

export function parseDotenv(content) {
  const env = {};
  for (const line of content.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const index = trimmed.indexOf('=');
    if (index < 0) continue;
    const key = trimmed.slice(0, index).trim();
    let value = trimmed.slice(index + 1).trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }
    env[key] = value;
  }
  return env;
}

export async function loadEnvConfig(repoRoot = process.cwd(), envSource = process.env) {
  const envPath = path.join(repoRoot, '.env');
  let parsed = {};
  try {
    parsed = parseDotenv(await readFile(envPath, 'utf8'));
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }

  const values = {};
  const display = {};
  const secrets = {};
  const missing = [];

  for (const key of REQUIRED_ENV) {
    const value = parsed[key] ?? envSource[key] ?? '';
    if (!value) missing.push(key);
    values[key] = value;
    secrets[key] = value;
    display[key] = key.includes('KEY') || key.includes('TOKEN') ? maskValue(value) : value;
  }

  return { envPath, values, display, secrets, missing };
}
