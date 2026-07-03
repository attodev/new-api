import assert from 'node:assert/strict';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { loadEnvConfig, parseDotenv } from '../lib/env.mjs';
import { maskSecrets } from '../lib/masking.mjs';

test('parseDotenv handles comments, quotes, and equals signs', () => {
  const env = parseDotenv(`
# comment
UNODE_BASE_URL=https://www.unodetech.xyz
NEW_API_RELAY_API_KEY="sk-a=b=c"
EMPTY=
`);

  assert.equal(env.UNODE_BASE_URL, 'https://www.unodetech.xyz');
  assert.equal(env.NEW_API_RELAY_API_KEY, 'sk-a=b=c');
  assert.equal(env.EMPTY, '');
});

test('loadEnvConfig returns masked display values and missing names', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'unode-diag-env-'));
  try {
    await writeFile(path.join(dir, '.env'), [
      'UNODE_BASE_URL=https://www.unodetech.xyz',
      'UNODE_RELAY_API_KEY=sk-unode-secret-123456',
      'NEW_API_BASE_URL=https://alrouter.ai',
      'NEW_API_RELAY_API_KEY=sk-router-secret-abcdef',
      'NEW_API_CHANNEL_ID=4'
    ].join('\n'));

    const config = await loadEnvConfig(dir, {});

    assert.equal(config.values.UNODE_BASE_URL, 'https://www.unodetech.xyz');
    assert.equal(config.display.UNODE_RELAY_API_KEY, 'sk-u...3456');
    assert.equal(config.display.NEW_API_RELAY_API_KEY, 'sk-r...cdef');
    assert.ok(config.missing.includes('NEW_API_ADMIN_ACCESS_TOKEN'));
    assert.ok(config.missing.includes('NEW_API_ADMIN_USER_ID'));
    assert.equal(config.secrets.UNODE_RELAY_API_KEY, 'sk-unode-secret-123456');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('loadEnvConfig falls back to injected env source', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'unode-diag-env-'));
  try {
    await writeFile(path.join(dir, '.env'), [
      'UNODE_BASE_URL=https://www.unodetech.xyz',
      'UNODE_RELAY_API_KEY=sk-unode-secret-123456',
      'NEW_API_BASE_URL=https://alrouter.ai',
      'NEW_API_RELAY_API_KEY=sk-router-secret-abcdef',
      'NEW_API_CHANNEL_ID=4'
    ].join('\n'));

    const config = await loadEnvConfig(dir, {
      NEW_API_ADMIN_ACCESS_TOKEN: 'admin-access-secret-123456',
      NEW_API_ADMIN_USER_ID: '99'
    });

    assert.equal(config.values.NEW_API_ADMIN_ACCESS_TOKEN, 'admin-access-secret-123456');
    assert.equal(config.display.NEW_API_ADMIN_ACCESS_TOKEN, 'admi...3456');
    assert.equal(config.values.NEW_API_ADMIN_USER_ID, '99');
    assert.ok(!config.missing.includes('NEW_API_ADMIN_ACCESS_TOKEN'));
    assert.ok(!config.missing.includes('NEW_API_ADMIN_USER_ID'));
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('maskSecrets masks auth headers and token-like body fields', () => {
  const masked = maskSecrets({
    request: {
      headers: {
        Authorization: 'Bearer sk-secret-123456',
        'x-api-key': 'sk-key-abcdef',
        'content-type': 'application/json'
      },
      body: {
        api_key: 'sk-body-123456',
        nested: {
          access_token: 'access-secret-abcdef',
          regular: 'visible'
        }
      }
    }
  });

  assert.equal(masked.request.headers.Authorization, 'Bearer sk-s...3456');
  assert.equal(masked.request.headers['x-api-key'], 'sk-k...cdef');
  assert.equal(masked.request.headers['content-type'], 'application/json');
  assert.equal(masked.request.body.api_key, 'sk-b...3456');
  assert.equal(masked.request.body.nested.access_token, 'acce...cdef');
  assert.equal(masked.request.body.nested.regular, 'visible');
});

test('maskSecrets preserves arrays and objects under secret-like keys', () => {
  const masked = maskSecrets({
    tokens: ['sk-secret-123456', 'sk-secret-abcdef'],
    'set-cookie': ['session-secret-123456', 'refresh-secret-abcdef'],
    token: {
      primary: 'sk-secret-123456',
      nested: {
        refresh: 'refresh-secret-abcdef'
      }
    }
  });

  assert.deepEqual(masked.tokens, ['sk-s...3456', 'sk-s...cdef']);
  assert.deepEqual(masked['set-cookie'], ['sess...3456', 'refr...cdef']);
  assert.deepEqual(masked.token, {
    primary: 'sk-s...3456',
    nested: {
      refresh: 'refr...cdef'
    }
  });
});

test('maskSecrets leaves token counters visible but masks common secret key shapes', () => {
  const masked = maskSecrets({
    usage: {
      prompt_tokens: 11,
      completion_tokens: 22,
      total_tokens: 33,
      max_tokens: 44
    },
    credentials: {
      apiKey: 'sk-camel-123456',
      accessToken: 'access-camel-abcdef',
      refreshToken: 'refresh-camel-abcdef',
      client_secret: 'client-secret-123456',
      tokens: ['sk-secret-123456', 'sk-secret-abcdef']
    }
  });

  assert.deepEqual(masked.usage, {
    prompt_tokens: 11,
    completion_tokens: 22,
    total_tokens: 33,
    max_tokens: 44
  });
  assert.equal(masked.credentials.apiKey, 'sk-c...3456');
  assert.equal(masked.credentials.accessToken, 'acce...cdef');
  assert.equal(masked.credentials.refreshToken, 'refr...cdef');
  assert.equal(masked.credentials.client_secret, 'clie...3456');
  assert.deepEqual(masked.credentials.tokens, ['sk-s...3456', 'sk-s...cdef']);
});
