import { appendFile, mkdir, readFile } from 'node:fs/promises';
import path from 'node:path';

import { maskSecrets } from './masking.mjs';

function newHistoryId() {
  const stamp = new Date().toISOString().replace(/[-:.TZ]/g, '').slice(0, 14);
  const suffix = Math.random().toString(36).slice(2, 8);
  return `diag_${stamp}_${suffix}`;
}

export async function appendHistory(filePath, record) {
  await mkdir(path.dirname(filePath), { recursive: true });
  const stored = maskSecrets({
    ...record,
    id: newHistoryId(),
    createdAt: new Date().toISOString()
  });
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
