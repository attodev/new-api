import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { loadEnvConfig, parseDotenv } from '../lib/env.mjs';
import { appendHistory, readHistory, readHistoryById } from '../lib/history.mjs';
import { maskSecrets } from '../lib/masking.mjs';
import { buildPresets } from '../lib/presets.mjs';
import { summarizeExchange } from '../lib/summary.mjs';

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

test('history store appends and reads masked JSONL records newest first', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'unode-diag-history-'));
  try {
    const filePath = path.join(dir, 'history.jsonl');
    const first = await appendHistory(filePath, {
      presetId: 'first',
      target: 'unode',
      request: { headers: { Authorization: 'Bearer sk-first-123456' }, body: {} },
      response: { status: 200, headers: {}, bodyText: 'ok', json: {} },
      summary: { classification: 'tool_not_used' }
    });
    const second = await appendHistory(filePath, {
      presetId: 'second',
      target: 'alrouter',
      request: { headers: { 'x-api-key': 'sk-second-abcdef' }, body: {} },
      response: { status: 200, headers: {}, bodyText: 'ok', json: {} },
      summary: { classification: 'web_search_used' }
    });

    const history = await readHistory(filePath);
    assert.equal(history.length, 2);
    assert.equal(history[0].id, second.id);
    assert.equal(history[1].id, first.id);
    assert.equal(history[0].request.headers['x-api-key'], 'sk-s...cdef');

    const loaded = await readHistoryById(filePath, first.id);
    assert.equal(loaded.presetId, 'first');

    const raw = await readFile(filePath, 'utf8');
    assert.equal(raw.includes('sk-first-123456'), false);
    assert.equal(raw.includes('sk-second-abcdef'), false);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('history store scrubs token-shaped strings from ordinary text fields', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'unode-diag-history-'));
  try {
    const filePath = path.join(dir, 'history.jsonl');
    await appendHistory(filePath, {
      presetId: 'text-secrets',
      target: 'alrouter',
      request: {
        headers: {},
        body: {
          prompt: 'ordinary prompt mentioning sk-prompt-abcdef for reproduction'
        }
      },
      response: {
        status: 500,
        headers: {},
        bodyText: 'upstream returned Bearer sk-bodytext-123456 and sk-response-abcdef',
        json: { error: 'provider rejected sk-json-abcdef' }
      },
      summary: { classification: 'request_error' }
    });

    const raw = await readFile(filePath, 'utf8');
    assert.equal(raw.includes('ordinary prompt mentioning'), true);
    assert.equal(raw.includes('sk-prompt-abcdef'), false);
    assert.equal(raw.includes('sk-bodytext-123456'), false);
    assert.equal(raw.includes('sk-response-abcdef'), false);
    assert.equal(raw.includes('sk-json-abcdef'), false);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('readHistory returns empty list for missing files', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'unode-diag-history-'));
  try {
    const history = await readHistory(path.join(dir, 'missing.jsonl'));
    assert.deepEqual(history, []);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('readHistory includes request error sentinel for corrupt JSONL lines', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'unode-diag-history-'));
  try {
    const filePath = path.join(dir, 'history.jsonl');
    await writeFile(filePath, [
      JSON.stringify({
        id: 'valid',
        createdAt: '2026-07-03T00:00:00.000Z',
        presetId: 'valid',
        summary: { classification: 'tool_not_used' }
      }),
      '{not-valid-json'
    ].join('\n'));

    const history = await readHistory(filePath);
    const sentinel = history.find((record) => record.presetId === 'corrupt-history-line');
    assert.equal(sentinel.summary.classification, 'request_error');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('readHistoryById returns null for missing records', async () => {
  const dir = await mkdtemp(path.join(tmpdir(), 'unode-diag-history-'));
  try {
    const filePath = path.join(dir, 'history.jsonl');
    await appendHistory(filePath, {
      presetId: 'present',
      target: 'unode',
      request: { headers: {}, body: {} },
      response: { status: 200, headers: {}, bodyText: 'ok', json: {} },
      summary: { classification: 'tool_not_used' }
    });

    const loaded = await readHistoryById(filePath, 'missing');
    assert.equal(loaded, null);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('buildPresets returns four editable diagnostic presets', () => {
  const presets = buildPresets({
    UNODE_BASE_URL: 'https://www.unodetech.xyz',
    NEW_API_BASE_URL: 'https://alrouter.ai',
    NEW_API_RELAY_API_KEY: 'sk-router',
    NEW_API_CHANNEL_ID: '4'
  });

  assert.equal(presets.length, 4);
  assert.deepEqual(presets.map((preset) => preset.id), [
    'direct-messages-web-search',
    'direct-chat-web-search-options',
    'alrouter-messages-forced-channel',
    'alrouter-chat-web-search-options'
  ]);
  assert.equal(presets[0].path, '/v1/messages');
  assert.equal(presets[3].body.web_search_options.search_context_size, 'low');
  assert.equal(presets[3].headers.authorization, 'Bearer ${NEW_API_RELAY_API_KEY}-${NEW_API_CHANNEL_ID}');
});

test('summarizeExchange detects Claude Code session and web search usage', () => {
  const summary = summarizeExchange({
    response: {
      status: 200,
      headers: {
        'x-cc-session-id': 'session-1',
        'x-oneapi-request-id': 'rid-1'
      },
      json: {
        content: [
          { type: 'server_tool_use', name: 'web_search' },
          { type: 'bash_code_execution_tool_result', content: { error_code: 'too_many_requests' } },
          { type: 'text', text: 'Claude Web Search called 1 times, cost: 4500' }
        ],
        usage: {
          server_tool_use: {
            web_search_requests: 1
          }
        }
      },
      bodyText: 'Claude Web Search called 1 times, cost: 4500'
    }
  });

  assert.equal(summary.hasCcSessionId, true);
  assert.equal(summary.requestId, 'rid-1');
  assert.deepEqual(summary.contentTypes, ['server_tool_use', 'bash_code_execution_tool_result', 'text']);
  assert.deepEqual(summary.toolErrorCodes, ['too_many_requests']);
  assert.equal(summary.webSearchRequests, 1);
  assert.equal(summary.classification, 'tool_rate_limited');
});

test('summarizeExchange classifies plain tool-unavailable text as suspected Claude Code session', () => {
  const summary = summarizeExchange({
    response: {
      status: 200,
      headers: { 'x-cc-session-id': 'session-2' },
      json: {
        content: [{ type: 'text', text: '저는 웹 검색 도구를 사용할 수 없습니다.' }]
      },
      bodyText: '저는 웹 검색 도구를 사용할 수 없습니다.'
    }
  });

  assert.equal(summary.classification, 'claude_code_session_suspected');
});

test('summarizeExchange classifies request errors before tool and session signals', () => {
  const summary = summarizeExchange({
    response: {
      status: 500,
      headers: { 'x-cc-session-id': 'session-3' },
      json: {
        content: [
          { type: 'server_tool_use', name: 'web_search' },
          { type: 'tool_result', content: { error_code: 'too_many_requests' } }
        ],
        usage: {
          server_tool_use: {
            web_search_requests: 1
          }
        }
      },
      bodyText: 'Claude Web Search called 1 times, cost: 4500'
    }
  });

  assert.equal(summary.classification, 'request_error');
});

test('summarizeExchange classifies web search usage from usage counters', () => {
  const summary = summarizeExchange({
    response: {
      status: 200,
      headers: {},
      json: {
        content: [{ type: 'text', text: '검색 결과입니다.' }],
        usage: {
          server_tool_use: {
            web_search_requests: 2
          }
        }
      }
    }
  });

  assert.equal(summary.webSearchRequests, 2);
  assert.equal(summary.classification, 'web_search_used');
});

test('summarizeExchange classifies successful responses without tool signals as not used', () => {
  const summary = summarizeExchange({
    response: {
      status: 200,
      headers: {},
      json: {
        content: [{ type: 'text', text: '일반 응답입니다.' }]
      }
    }
  });

  assert.equal(summary.classification, 'tool_not_used');
});

test('summarizeExchange classifies records without status or signals as unknown', () => {
  const summary = summarizeExchange({
    response: {
      headers: {},
      json: {
        content: [{ type: 'text', text: '상태 코드가 없는 응답입니다.' }]
      }
    }
  });

  assert.equal(summary.classification, 'unknown');
});

test('summarizeExchange reads mixed-case request id headers', () => {
  const summary = summarizeExchange({
    response: {
      status: 200,
      headers: {
        'X-OneAPI-Request-ID': 'rid-mixed'
      },
      json: {
        content: [{ type: 'text', text: '일반 응답입니다.' }]
      }
    }
  });

  assert.equal(summary.requestId, 'rid-mixed');
});

test('summarizeExchange extracts OpenAI message content for classification', () => {
  const summary = summarizeExchange({
    response: {
      status: 200,
      headers: {},
      json: {
        choices: [
          {
            message: {
              role: 'assistant',
              content: 'I cannot use web search from this session.'
            }
          }
        ]
      }
    }
  });

  assert.deepEqual(summary.contentTypes, ['message_text']);
  assert.equal(summary.classification, 'claude_code_session_suspected');
});

test('summarizeExchange recursively extracts nested tool error codes', () => {
  const summary = summarizeExchange({
    response: {
      status: 200,
      headers: {},
      json: {
        content: [
          {
            type: 'tool_result',
            content: [
              {
                type: 'json',
                payload: {
                  details: {
                    error_code: 'too_many_requests'
                  }
                }
              }
            ]
          }
        ]
      }
    }
  });

  assert.deepEqual(summary.toolErrorCodes, ['too_many_requests']);
  assert.equal(summary.classification, 'tool_rate_limited');
});
