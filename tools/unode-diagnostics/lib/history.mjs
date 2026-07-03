import { appendFile, mkdir, readFile } from 'node:fs/promises';
import path from 'node:path';

import { maskSecrets, maskValue } from './masking.mjs';

const SK_TOKEN_SOURCE = 'sk-[A-Za-z0-9][A-Za-z0-9_-]{7,}';
const BEARER_SK_TOKEN_PATTERN = new RegExp(
  `(^|[^A-Za-z0-9_-])(Bearer\\s+)(${SK_TOKEN_SOURCE})(?=$|[^A-Za-z0-9_-])`,
  'gi'
);
const STANDALONE_SK_TOKEN_PATTERN = new RegExp(
  `(^|[^A-Za-z0-9_-])(${SK_TOKEN_SOURCE})(?=$|[^A-Za-z0-9_-])`,
  'g'
);

function newHistoryId() {
  const stamp = new Date().toISOString().replace(/[-:.TZ]/g, '').slice(0, 14);
  const suffix = Math.random().toString(36).slice(2, 8);
  return `diag_${stamp}_${suffix}`;
}

function scrubSecretString(value) {
  return value
    .replace(BEARER_SK_TOKEN_PATTERN, (_, boundary, bearerPrefix, token) => {
      return `${boundary}${maskValue(`${bearerPrefix}${token}`)}`;
    })
    .replace(STANDALONE_SK_TOKEN_PATTERN, (_, boundary, token) => {
      return `${boundary}${maskValue(token)}`;
    });
}

function knownSecretValues(knownSecrets) {
  return [...new Set(knownSecrets.map((secret) => String(secret)).filter(Boolean))]
    .sort((a, b) => b.length - a.length);
}

function redactKnownSecretString(value, knownSecrets) {
  let redacted = value;
  for (const secret of knownSecrets) {
    redacted = redacted.split(secret).join(maskValue(secret));
  }
  return redacted;
}

function scrubSecretStringValue(value, knownSecrets) {
  return scrubSecretString(redactKnownSecretString(value, knownSecrets));
}

function scrubSecretStrings(value, knownSecrets = []) {
  if (Array.isArray(value)) {
    return value.map((item) => scrubSecretStrings(item, knownSecrets));
  }
  if (value && typeof value === 'object') {
    const out = {};
    for (const [key, child] of Object.entries(value)) {
      out[scrubSecretStringValue(key, knownSecrets)] = scrubSecretStrings(child, knownSecrets);
    }
    return out;
  }
  if (typeof value === 'string') {
    return scrubSecretStringValue(value, knownSecrets);
  }
  return value;
}

export async function appendHistory(filePath, record, knownSecrets = []) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const stored = scrubSecretStrings(maskSecrets({
    ...record,
    id: newHistoryId(),
    createdAt: new Date().toISOString()
  }), knownSecretValues(knownSecrets));
  await appendFile(filePath, `${JSON.stringify(stored)}\n`, 'utf8');
  return stored;
}

export async function readHistory(filePath) {
  let content = '';
  try {
    content = await readFile(filePath, 'utf8');
  } catch (error) {
    if (error.code === 'ENOENT') return [];
    throw error;
  }

  const records = [];
  for (const line of content.split(/\r?\n/)) {
    if (!line.trim()) continue;
    try {
      records.push({ lineNumber: records.length + 1, record: JSON.parse(line) });
    } catch {
      records.push({
        lineNumber: records.length + 1,
        record: {
          id: `corrupt_${records.length + 1}`,
          createdAt: new Date(0).toISOString(),
          presetId: 'corrupt-history-line',
          target: 'local',
          request: {},
          response: {},
          summary: { classification: 'request_error', warning: 'corrupt history line skipped' }
        }
      });
    }
  }

  return records
    .sort((a, b) => {
      const createdAtOrder = String(b.record.createdAt).localeCompare(String(a.record.createdAt));
      return createdAtOrder || b.lineNumber - a.lineNumber;
    })
    .map(({ record }) => record);
}

export async function readHistoryById(filePath, id) {
  const records = await readHistory(filePath);
  return records.find((record) => record.id === id) || null;
}
