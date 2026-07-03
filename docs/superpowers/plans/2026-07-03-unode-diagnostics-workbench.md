# unode Diagnostics Workbench Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local standalone workbench that loads `.env`, sends direct unode and ALRouter diagnostic API requests, shows request/response panes, and persists masked request history across browser refreshes.

**Architecture:** A dependency-light Node.js tool lives under `tools/unode-diagnostics/`. The Node server serves static browser assets, reads the repo `.env`, proxies diagnostic requests, masks secrets, appends JSONL history, and exposes simple JSON APIs. The browser UI is vanilla HTML/CSS/JS with a three-pane Workbench layout: history, request editor, response viewer.

**Tech Stack:** Node.js ESM, built-in `http`, `fs/promises`, `node:test`, browser `fetch`, vanilla CSS/JS.

---

## File Structure

- Modify: `.gitignore` — ignore visual companion output and local diagnostics runtime history.
- Create: `tools/unode-diagnostics/package.json` — scripts for start/test.
- Create: `tools/unode-diagnostics/README.md` — local usage and safety notes.
- Create: `tools/unode-diagnostics/server.mjs` — HTTP server and route wiring.
- Create: `tools/unode-diagnostics/lib/env.mjs` — `.env` parsing and display-safe config.
- Create: `tools/unode-diagnostics/lib/masking.mjs` — secret masking for headers/body/records.
- Create: `tools/unode-diagnostics/lib/presets.mjs` — four approved diagnostic presets.
- Create: `tools/unode-diagnostics/lib/summary.mjs` — response signal extraction and classification.
- Create: `tools/unode-diagnostics/lib/history.mjs` — append-only JSONL history store.
- Create: `tools/unode-diagnostics/lib/http-client.mjs` — outbound request preparation and execution.
- Create: `tools/unode-diagnostics/public/index.html` — Workbench shell.
- Create: `tools/unode-diagnostics/public/styles.css` — dense diagnostic UI styling.
- Create: `tools/unode-diagnostics/public/app.js` — Workbench interactions.
- Create: `tools/unode-diagnostics/test/core.test.mjs` — env, masking, presets, summary, history unit tests.

Runtime files are created under `tools/unode-diagnostics/data/` and must not be committed.

---

### Task 1: Scaffold Local Tool And Ignore Runtime Files

**Files:**
- Modify: `.gitignore`
- Create: `tools/unode-diagnostics/package.json`
- Create: `tools/unode-diagnostics/README.md`

- [ ] **Step 1: Add runtime ignore rules**

Append these lines to `.gitignore`:

```gitignore
.superpowers/
tools/unode-diagnostics/data/
```

- [ ] **Step 2: Create package manifest**

Create `tools/unode-diagnostics/package.json`:

```json
{
  "name": "unode-diagnostics",
  "private": true,
  "type": "module",
  "scripts": {
    "start": "node server.mjs",
    "test": "node --test test/*.test.mjs"
  },
  "engines": {
    "node": ">=20"
  }
}
```

- [ ] **Step 3: Create usage README**

Create `tools/unode-diagnostics/README.md`:

````markdown
# unode Diagnostics Workbench

Local-only diagnostic UI for comparing direct unode requests and ALRouter routed requests.

## Run

```bash
cd tools/unode-diagnostics
npm test
npm start
```

Open the printed local URL.

## Safety

- The server reads the repo root `.env`.
- Request execution uses real keys only in server memory.
- History is written to `tools/unode-diagnostics/data/history.jsonl`.
- Stored history masks authentication headers and token-like body fields.
- Do not paste customer prompts or confidential customer data into diagnostics.
````

- [ ] **Step 4: Verify scaffold**

Run:

```bash
cd tools/unode-diagnostics && node -e "const pkg=JSON.parse(require('node:fs').readFileSync('package.json','utf8')); if (pkg.scripts.start !== 'node server.mjs') process.exit(1); if (pkg.scripts.test !== 'node --test test/*.test.mjs') process.exit(1)"
```

Expected: exit code 0.

- [ ] **Step 5: Commit**

```bash
git add .gitignore tools/unode-diagnostics/package.json tools/unode-diagnostics/README.md
git commit -m "chore: scaffold unode diagnostics tool"
```

---

### Task 2: Env Loading And Secret Masking

**Files:**
- Create: `tools/unode-diagnostics/lib/env.mjs`
- Create: `tools/unode-diagnostics/lib/masking.mjs`
- Create: `tools/unode-diagnostics/test/core.test.mjs`

- [ ] **Step 1: Write failing env and masking tests**

Create `tools/unode-diagnostics/test/core.test.mjs`:

```js
import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
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

    const config = await loadEnvConfig(dir);

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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: FAIL with module not found for `../lib/env.mjs` or missing exports.

- [ ] **Step 3: Implement `masking.mjs`**

Create `tools/unode-diagnostics/lib/masking.mjs`:

```js
const SECRET_KEY_NAMES = new Set([
  'authorization',
  'x-api-key',
  'api-key',
  'x-goog-api-key',
  'new-api-user',
  'cookie',
  'set-cookie',
  'api_key',
  'access_token',
  'token',
  'key'
]);

function looksSecretKey(key) {
  const normalized = String(key).toLowerCase();
  return SECRET_KEY_NAMES.has(normalized) || normalized.includes('token') || normalized.includes('api_key');
}

export function maskValue(value) {
  if (value == null) return value;
  const raw = String(value);
  const bearerPrefix = raw.match(/^Bearer\s+/i)?.[0] ?? '';
  const token = bearerPrefix ? raw.slice(bearerPrefix.length) : raw;
  if (token.length <= 8) return `${bearerPrefix}***`;
  return `${bearerPrefix}${token.slice(0, 4)}...${token.slice(-4)}`;
}

export function maskSecrets(value, parentKey = '') {
  if (Array.isArray(value)) {
    return value.map((item) => maskSecrets(item, parentKey));
  }
  if (value && typeof value === 'object') {
    const out = {};
    for (const [key, child] of Object.entries(value)) {
      out[key] = looksSecretKey(key) ? maskValue(child) : maskSecrets(child, key);
    }
    return out;
  }
  return looksSecretKey(parentKey) ? maskValue(value) : value;
}
```

- [ ] **Step 4: Implement `env.mjs`**

Create `tools/unode-diagnostics/lib/env.mjs`:

```js
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

export async function loadEnvConfig(repoRoot = process.cwd()) {
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
    const value = parsed[key] ?? process.env[key] ?? '';
    if (!value) missing.push(key);
    values[key] = value;
    secrets[key] = value;
    display[key] = key.includes('KEY') || key.includes('TOKEN') ? maskValue(value) : value;
  }

  return { envPath, values, display, secrets, missing };
}
```

- [ ] **Step 5: Verify tests pass**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: PASS for the three tests.

- [ ] **Step 6: Commit**

```bash
git add tools/unode-diagnostics/lib/env.mjs tools/unode-diagnostics/lib/masking.mjs tools/unode-diagnostics/test/core.test.mjs
git commit -m "feat(diagnostics): load env and mask secrets"
```

---

### Task 3: Presets And Response Summary

**Files:**
- Create: `tools/unode-diagnostics/lib/presets.mjs`
- Create: `tools/unode-diagnostics/lib/summary.mjs`
- Modify: `tools/unode-diagnostics/test/core.test.mjs`

- [ ] **Step 1: Add failing preset and summary tests**

Append to `tools/unode-diagnostics/test/core.test.mjs`:

```js
import { buildPresets } from '../lib/presets.mjs';
import { summarizeExchange } from '../lib/summary.mjs';

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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: FAIL with module not found for `presets.mjs` or `summary.mjs`.

- [ ] **Step 3: Implement `presets.mjs`**

Create `tools/unode-diagnostics/lib/presets.mjs`:

```js
const prompt = '웹 검색 도구를 사용해 2026년 7월 2일 한국 주요 뉴스 1개를 출처 URL과 함께 한 문장으로 답해줘.';

export function buildPresets() {
  return [
    {
      id: 'direct-messages-web-search',
      label: 'Direct unode /v1/messages + web_search_20250305',
      target: 'unode',
      method: 'POST',
      baseUrl: '${UNODE_BASE_URL}',
      path: '/v1/messages',
      headers: {
        'x-api-key': '${UNODE_RELAY_API_KEY}',
        'anthropic-version': '2023-06-01',
        'content-type': 'application/json'
      },
      body: {
        model: 'claude-sonnet-4-6',
        max_tokens: 360,
        tools: [{ type: 'web_search_20250305', name: 'web_search', max_uses: 1 }],
        messages: [{ role: 'user', content: prompt }]
      }
    },
    {
      id: 'direct-chat-web-search-options',
      label: 'Direct unode /v1/chat/completions + web_search_options',
      target: 'unode',
      method: 'POST',
      baseUrl: '${UNODE_BASE_URL}',
      path: '/v1/chat/completions',
      headers: {
        authorization: 'Bearer ${UNODE_RELAY_API_KEY}',
        'content-type': 'application/json'
      },
      body: {
        model: 'claude-sonnet-4-6',
        max_tokens: 360,
        web_search_options: { search_context_size: 'low' },
        messages: [{ role: 'user', content: prompt }]
      }
    },
    {
      id: 'alrouter-messages-forced-channel',
      label: 'ALRouter /v1/messages forced channel',
      target: 'alrouter',
      method: 'POST',
      baseUrl: '${NEW_API_BASE_URL}',
      path: '/v1/messages',
      headers: {
        'x-api-key': '${NEW_API_RELAY_API_KEY}-${NEW_API_CHANNEL_ID}',
        'anthropic-version': '2023-06-01',
        'content-type': 'application/json'
      },
      body: {
        model: 'claude-sonnet-4-6',
        max_tokens: 360,
        tools: [{ type: 'web_search_20250305', name: 'web_search', max_uses: 1 }],
        messages: [{ role: 'user', content: prompt }]
      }
    },
    {
      id: 'alrouter-chat-web-search-options',
      label: 'ALRouter /v1/chat/completions forced channel + web_search_options',
      target: 'alrouter',
      method: 'POST',
      baseUrl: '${NEW_API_BASE_URL}',
      path: '/v1/chat/completions',
      headers: {
        authorization: 'Bearer ${NEW_API_RELAY_API_KEY}-${NEW_API_CHANNEL_ID}',
        'content-type': 'application/json'
      },
      body: {
        model: 'claude-sonnet-4-6',
        max_tokens: 360,
        web_search_options: { search_context_size: 'low' },
        messages: [{ role: 'user', content: prompt }]
      }
    }
  ];
}
```

- [ ] **Step 4: Implement `summary.mjs`**

Create `tools/unode-diagnostics/lib/summary.mjs`:

```js
function headerValue(headers, name) {
  const wanted = name.toLowerCase();
  for (const [key, value] of Object.entries(headers || {})) {
    if (key.toLowerCase() === wanted) return String(value);
  }
  return '';
}

function contentBlocks(json) {
  if (Array.isArray(json?.content)) return json.content;
  const choices = Array.isArray(json?.choices) ? json.choices : [];
  return choices.map((choice) => choice?.message).filter(Boolean);
}

function collectText(json, bodyText) {
  const texts = [];
  for (const block of contentBlocks(json)) {
    if (typeof block?.text === 'string') texts.push(block.text);
    if (typeof block?.content === 'string') texts.push(block.content);
  }
  if (bodyText) texts.push(bodyText);
  return texts.join('\n');
}

function collectToolErrorCodes(blocks) {
  const errors = [];
  for (const block of blocks) {
    const content = block?.content;
    if (content && typeof content === 'object' && content.error_code) errors.push(content.error_code);
    if (block?.error_code) errors.push(block.error_code);
  }
  return [...new Set(errors)];
}

export function summarizeExchange(record) {
  const response = record.response || {};
  const headers = response.headers || {};
  const json = response.json || {};
  const blocks = contentBlocks(json);
  const text = collectText(json, response.bodyText);
  const toolErrorCodes = collectToolErrorCodes(blocks);
  const webSearchRequests = Number(json?.usage?.server_tool_use?.web_search_requests || 0);
  const hasWebSearchText = /Claude Web Search called|web_search/i.test(text);
  const hasCcSessionId = Boolean(headerValue(headers, 'x-cc-session-id'));
  const hasUnavailableText = /웹 검색 도구를 사용할 수 없습니다|웹 검색 도구가 없어서|tool.*available|도구.*없/i.test(text);

  let classification = 'unknown';
  if (response.status >= 400 || record.error) {
    classification = 'request_error';
  } else if (toolErrorCodes.includes('too_many_requests')) {
    classification = 'tool_rate_limited';
  } else if (webSearchRequests > 0 || /Claude Web Search called 1 times/i.test(text)) {
    classification = 'web_search_used';
  } else if (hasCcSessionId || hasUnavailableText) {
    classification = 'claude_code_session_suspected';
  } else if (response.status) {
    classification = 'tool_not_used';
  }

  return {
    hasCcSessionId,
    requestId: headerValue(headers, 'x-oneapi-request-id') || headerValue(headers, 'x-newapi-request-id') || headerValue(headers, 'x-request-id'),
    upstreamRequestId: '',
    contentTypes: blocks.map((block) => block?.type || block?.role).filter(Boolean),
    toolErrorCodes,
    webSearchRequests,
    classification,
    hints: {
      hasWebSearchText,
      hasUnavailableText
    }
  };
}
```

- [ ] **Step 5: Verify tests pass**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: PASS for env, masking, presets, and summary tests.

- [ ] **Step 6: Commit**

```bash
git add tools/unode-diagnostics/lib/presets.mjs tools/unode-diagnostics/lib/summary.mjs tools/unode-diagnostics/test/core.test.mjs
git commit -m "feat(diagnostics): add presets and response summaries"
```

---

### Task 4: Append-Only History Store

**Files:**
- Create: `tools/unode-diagnostics/lib/history.mjs`
- Modify: `tools/unode-diagnostics/test/core.test.mjs`

- [ ] **Step 1: Add failing history tests**

Append to `tools/unode-diagnostics/test/core.test.mjs`:

```js
import { appendHistory, readHistory, readHistoryById } from '../lib/history.mjs';

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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: FAIL with module not found for `history.mjs`.

- [ ] **Step 3: Implement `history.mjs`**

Create `tools/unode-diagnostics/lib/history.mjs`:

```js
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
    id: newHistoryId(),
    createdAt: new Date().toISOString(),
    ...record
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
      records.push(JSON.parse(line));
    } catch {
      records.push({
        id: `corrupt_${records.length + 1}`,
        createdAt: new Date(0).toISOString(),
        presetId: 'corrupt-history-line',
        target: 'local',
        request: {},
        response: {},
        summary: { classification: 'request_error', warning: 'corrupt history line skipped' }
      });
    }
  }

  return records.sort((a, b) => String(b.createdAt).localeCompare(String(a.createdAt)));
}

export async function readHistoryById(filePath, id) {
  const records = await readHistory(filePath);
  return records.find((record) => record.id === id) || null;
}
```

- [ ] **Step 4: Verify tests pass**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tools/unode-diagnostics/lib/history.mjs tools/unode-diagnostics/test/core.test.mjs
git commit -m "feat(diagnostics): persist masked request history"
```

---

### Task 5: Outbound Request Preparation And Server APIs

**Files:**
- Create: `tools/unode-diagnostics/lib/http-client.mjs`
- Create: `tools/unode-diagnostics/server.mjs`
- Modify: `tools/unode-diagnostics/test/core.test.mjs`

- [ ] **Step 1: Add failing outbound request test**

Append to `tools/unode-diagnostics/test/core.test.mjs`:

```js
import { prepareOutboundRequest } from '../lib/http-client.mjs';

test('prepareOutboundRequest expands env placeholders and builds URL', () => {
  const prepared = prepareOutboundRequest({
    method: 'POST',
    baseUrl: '${NEW_API_BASE_URL}',
    path: '/v1/chat/completions',
    headers: {
      authorization: 'Bearer ${NEW_API_RELAY_API_KEY}-${NEW_API_CHANNEL_ID}',
      'content-type': 'application/json'
    },
    body: { model: 'claude-sonnet-4-6' }
  }, {
    NEW_API_BASE_URL: 'https://alrouter.ai/',
    NEW_API_RELAY_API_KEY: 'sk-router',
    NEW_API_CHANNEL_ID: '4'
  });

  assert.equal(prepared.url, 'https://alrouter.ai/v1/chat/completions');
  assert.equal(prepared.headers.authorization, 'Bearer sk-router-4');
  assert.equal(prepared.bodyText, '{"model":"claude-sonnet-4-6"}');
});
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: FAIL with module not found for `http-client.mjs`.

- [ ] **Step 3: Implement `http-client.mjs`**

Create `tools/unode-diagnostics/lib/http-client.mjs`:

```js
function expandTemplate(value, env) {
  if (typeof value === 'string') {
    return value.replace(/\$\{([A-Z0-9_]+)\}/g, (_, key) => env[key] || '');
  }
  if (Array.isArray(value)) return value.map((item) => expandTemplate(item, env));
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, expandTemplate(child, env)]));
  }
  return value;
}

function joinUrl(baseUrl, requestPath) {
  const base = String(baseUrl || '').replace(/\/+$/, '');
  const path = String(requestPath || '').startsWith('/') ? requestPath : `/${requestPath || ''}`;
  return `${base}${path}`;
}

export function prepareOutboundRequest(input, env) {
  const expanded = expandTemplate(input, env);
  const bodyText = typeof expanded.body === 'string' ? expanded.body : JSON.stringify(expanded.body || {});
  return {
    method: expanded.method || 'POST',
    url: joinUrl(expanded.baseUrl, expanded.path),
    headers: expanded.headers || {},
    body: expanded.body || {},
    bodyText
  };
}

export async function sendDiagnosticRequest(input, env, fetchImpl = fetch) {
  const prepared = prepareOutboundRequest(input, env);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), Number(input.timeoutMs || 90000));
  try {
    const response = await fetchImpl(prepared.url, {
      method: prepared.method,
      headers: prepared.headers,
      body: prepared.method.toUpperCase() === 'GET' ? undefined : prepared.bodyText,
      signal: controller.signal
    });
    const bodyText = await response.text();
    let json = null;
    try {
      json = JSON.parse(bodyText);
    } catch {
      json = null;
    }
    return {
      request: {
        method: prepared.method,
        url: prepared.url,
        headers: prepared.headers,
        body: prepared.body
      },
      response: {
        status: response.status,
        statusText: response.statusText,
        headers: Object.fromEntries(response.headers.entries()),
        bodyText,
        json
      }
    };
  } catch (error) {
    return {
      request: {
        method: prepared.method,
        url: prepared.url,
        headers: prepared.headers,
        body: prepared.body
      },
      response: {
        status: 0,
        statusText: 'NETWORK_ERROR',
        headers: {},
        bodyText: String(error.message || error),
        json: null
      },
      error: String(error.message || error)
    };
  } finally {
    clearTimeout(timeout);
  }
}
```

- [ ] **Step 4: Implement `server.mjs`**

Create `tools/unode-diagnostics/server.mjs`:

```js
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { loadEnvConfig } from './lib/env.mjs';
import { appendHistory, readHistory, readHistoryById } from './lib/history.mjs';
import { sendDiagnosticRequest } from './lib/http-client.mjs';
import { buildPresets } from './lib/presets.mjs';
import { summarizeExchange } from './lib/summary.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '../..');
const publicDir = path.join(__dirname, 'public');
const historyPath = path.join(__dirname, 'data/history.jsonl');

function json(res, status, value) {
  const body = JSON.stringify(value, null, 2);
  res.writeHead(status, {
    'content-type': 'application/json; charset=utf-8',
    'cache-control': 'no-store'
  });
  res.end(body);
}

async function readJson(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const raw = Buffer.concat(chunks).toString('utf8') || '{}';
  return JSON.parse(raw);
}

async function serveStatic(req, res) {
  const url = new URL(req.url, 'http://localhost');
  const pathname = url.pathname === '/' ? '/index.html' : url.pathname;
  const safePath = path.normalize(pathname).replace(/^(\.\.[/\\])+/, '');
  const filePath = path.join(publicDir, safePath);
  const ext = path.extname(filePath);
  const contentTypes = {
    '.html': 'text/html; charset=utf-8',
    '.css': 'text/css; charset=utf-8',
    '.js': 'text/javascript; charset=utf-8'
  };
  try {
    const body = await readFile(filePath);
    res.writeHead(200, { 'content-type': contentTypes[ext] || 'application/octet-stream' });
    res.end(body);
  } catch {
    res.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' });
    res.end('not found');
  }
}

async function handleApi(req, res) {
  const url = new URL(req.url, 'http://localhost');
  if (req.method === 'GET' && url.pathname === '/healthz') return json(res, 200, { ok: true });
  if (req.method === 'GET' && url.pathname === '/api/config') {
    const config = await loadEnvConfig(repoRoot);
    return json(res, 200, { display: config.display, missing: config.missing });
  }
  if (req.method === 'GET' && url.pathname === '/api/presets') {
    return json(res, 200, { presets: buildPresets() });
  }
  if (req.method === 'GET' && url.pathname === '/api/history') {
    return json(res, 200, { items: await readHistory(historyPath) });
  }
  if (req.method === 'GET' && url.pathname.startsWith('/api/history/')) {
    const id = decodeURIComponent(url.pathname.slice('/api/history/'.length));
    return json(res, 200, { item: await readHistoryById(historyPath, id) });
  }
  if (req.method === 'POST' && url.pathname === '/api/send') {
    const config = await loadEnvConfig(repoRoot);
    const input = await readJson(req);
    const exchange = await sendDiagnosticRequest(input, config.secrets);
    const summary = summarizeExchange(exchange);
    const stored = await appendHistory(historyPath, {
      presetId: input.presetId || '',
      target: input.target || '',
      request: exchange.request,
      response: exchange.response,
      summary,
      error: exchange.error || ''
    });
    return json(res, 200, { item: stored });
  }
  return json(res, 404, { error: 'not found' });
}

export function createDiagnosticsServer() {
  return createServer(async (req, res) => {
    try {
      if (req.url === '/healthz' || req.url.startsWith('/api/')) {
        await handleApi(req, res);
      } else {
        await serveStatic(req, res);
      }
    } catch (error) {
      json(res, 500, { error: String(error.message || error) });
    }
  });
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const port = Number(process.env.PORT || 5179);
  const host = process.env.HOST || '127.0.0.1';
  createDiagnosticsServer().listen(port, host, () => {
    console.log(`unode diagnostics: http://${host}:${port}`);
  });
}
```

- [ ] **Step 5: Verify tests pass**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add tools/unode-diagnostics/lib/http-client.mjs tools/unode-diagnostics/server.mjs tools/unode-diagnostics/test/core.test.mjs
git commit -m "feat(diagnostics): add local API server"
```

---

### Task 6: Workbench Browser UI

**Files:**
- Create: `tools/unode-diagnostics/public/index.html`
- Create: `tools/unode-diagnostics/public/styles.css`
- Create: `tools/unode-diagnostics/public/app.js`

- [ ] **Step 1: Create HTML shell**

Create `tools/unode-diagnostics/public/index.html`:

```html
<!doctype html>
<html lang="ko">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>unode Diagnostics Workbench</title>
    <link rel="stylesheet" href="/styles.css" />
  </head>
  <body>
    <header class="topbar">
      <div>
        <h1>unode Diagnostics Workbench</h1>
        <p>Direct unode and ALRouter routed request comparison</p>
      </div>
      <div class="actions">
        <button id="load-env">Load .env</button>
        <button id="send-request" class="primary">Send API</button>
      </div>
    </header>

    <main class="workbench">
      <aside class="history-pane">
        <div class="pane-title">
          <h2>History</h2>
          <button id="reload-history" title="Reload history">Reload</button>
        </div>
        <input id="history-filter" placeholder="Filter history" />
        <div id="history-list" class="history-list"></div>
      </aside>

      <section class="request-pane">
        <div class="pane-title">
          <h2>Request</h2>
          <span id="env-status" class="badge neutral">env not loaded</span>
        </div>
        <label>Preset</label>
        <select id="preset-select"></select>
        <div class="grid two">
          <label>Target<select id="target"><option value="unode">direct unode</option><option value="alrouter">ALRouter</option></select></label>
          <label>Method<input id="method" value="POST" /></label>
        </div>
        <label>Base URL<input id="base-url" /></label>
        <label>Path<input id="path" /></label>
        <label>Headers JSON<textarea id="headers-json" spellcheck="false"></textarea></label>
        <label>Body JSON<textarea id="body-json" spellcheck="false"></textarea></label>
        <div id="request-error" class="error"></div>
      </section>

      <section class="response-pane">
        <div class="pane-title">
          <h2>Response</h2>
          <span id="classification" class="badge neutral">none</span>
        </div>
        <div id="summary" class="summary-grid"></div>
        <label>Response Headers<textarea id="response-headers" readonly spellcheck="false"></textarea></label>
        <label>Response Body<textarea id="response-body" readonly spellcheck="false"></textarea></label>
      </section>
    </main>

    <script type="module" src="/app.js"></script>
  </body>
</html>
```

- [ ] **Step 2: Create CSS**

Create `tools/unode-diagnostics/public/styles.css`:

```css
:root {
  color-scheme: light;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  background: #f6f7f9;
  color: #17191f;
}

* { box-sizing: border-box; }
body { margin: 0; min-width: 1040px; }
button, input, select, textarea { font: inherit; }

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 20px;
  background: #ffffff;
  border-bottom: 1px solid #dfe3ea;
}

h1 { margin: 0; font-size: 20px; }
h2 { margin: 0; font-size: 15px; }
p { margin: 4px 0 0; color: #667085; }

.actions { display: flex; gap: 8px; }
button {
  border: 1px solid #c9d0dc;
  background: #fff;
  border-radius: 6px;
  padding: 8px 10px;
  cursor: pointer;
}
button.primary { background: #155eef; border-color: #155eef; color: #fff; }

.workbench {
  display: grid;
  grid-template-columns: 280px minmax(360px, 1fr) minmax(420px, 1.1fr);
  gap: 12px;
  padding: 12px;
  height: calc(100vh - 73px);
}

.history-pane, .request-pane, .response-pane {
  background: #ffffff;
  border: 1px solid #dfe3ea;
  border-radius: 8px;
  padding: 12px;
  overflow: auto;
}

.pane-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 12px;
}

label { display: block; font-size: 12px; font-weight: 650; color: #344054; margin-top: 10px; }
input, select, textarea {
  width: 100%;
  border: 1px solid #cfd6e2;
  border-radius: 6px;
  padding: 8px;
  margin-top: 5px;
  background: #fff;
}
textarea {
  min-height: 180px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.45;
  resize: vertical;
}

.grid.two { display: grid; grid-template-columns: 1fr 120px; gap: 8px; }
.badge {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  border-radius: 999px;
  padding: 2px 8px;
  font-size: 12px;
  background: #eef2f6;
  color: #344054;
}
.badge.web_search_used { background: #dcfae6; color: #067647; }
.badge.claude_code_session_suspected { background: #fef3c7; color: #92400e; }
.badge.tool_rate_limited, .badge.request_error { background: #fee4e2; color: #b42318; }
.badge.tool_not_used { background: #e0f2fe; color: #075985; }

.history-list { display: grid; gap: 8px; margin-top: 10px; }
.history-item {
  width: 100%;
  text-align: left;
  border: 1px solid #dfe3ea;
  border-radius: 6px;
  padding: 8px;
}
.history-item strong { display: block; font-size: 13px; }
.history-item span { display: block; color: #667085; font-size: 12px; margin-top: 3px; }

.summary-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin-bottom: 10px;
}
.summary-card {
  border: 1px solid #dfe3ea;
  border-radius: 6px;
  padding: 8px;
  min-height: 54px;
}
.summary-card span { display: block; color: #667085; font-size: 11px; }
.summary-card strong { display: block; margin-top: 4px; font-size: 13px; overflow-wrap: anywhere; }
.error { color: #b42318; min-height: 20px; margin-top: 8px; font-size: 13px; }
```

- [ ] **Step 3: Create browser logic**

Create `tools/unode-diagnostics/public/app.js`:

```js
const state = {
  config: null,
  presets: [],
  history: []
};

const $ = (id) => document.getElementById(id);

function pretty(value) {
  return JSON.stringify(value ?? {}, null, 2);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { 'content-type': 'application/json' },
    ...options
  });
  if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
  return response.json();
}

function applyPreset(preset) {
  $('preset-select').value = preset.id;
  $('target').value = preset.target;
  $('method').value = preset.method;
  $('base-url').value = preset.baseUrl;
  $('path').value = preset.path;
  $('headers-json').value = pretty(preset.headers);
  $('body-json').value = pretty(preset.body);
  $('request-error').textContent = '';
}

function readRequestForm() {
  let headers;
  let body;
  try {
    headers = JSON.parse($('headers-json').value || '{}');
    body = JSON.parse($('body-json').value || '{}');
  } catch (error) {
    throw new Error(`JSON parse error: ${error.message}`);
  }
  return {
    presetId: $('preset-select').value,
    target: $('target').value,
    method: $('method').value.trim() || 'POST',
    baseUrl: $('base-url').value.trim(),
    path: $('path').value.trim(),
    headers,
    body,
    authMode: 'env'
  };
}

function renderSummary(item) {
  const summary = item?.summary || {};
  const response = item?.response || {};
  $('classification').textContent = summary.classification || 'none';
  $('classification').className = `badge ${summary.classification || 'neutral'}`;
  const entries = [
    ['HTTP status', response.status ?? ''],
    ['request id', summary.requestId || ''],
    ['x-cc-session-id', summary.hasCcSessionId ? 'present' : 'absent'],
    ['web search requests', summary.webSearchRequests ?? 0],
    ['tool errors', (summary.toolErrorCodes || []).join(', ')],
    ['content types', (summary.contentTypes || []).join(', ')]
  ];
  $('summary').innerHTML = entries.map(([label, value]) => `
    <div class="summary-card"><span>${label}</span><strong>${String(value || '-')}</strong></div>
  `).join('');
}

function renderResponse(item) {
  renderSummary(item);
  $('response-headers').value = pretty(item?.response?.headers || {});
  $('response-body').value = item?.response?.json ? pretty(item.response.json) : String(item?.response?.bodyText || '');
}

function restoreHistoryItem(item) {
  $('target').value = item.target || 'unode';
  $('method').value = item.request?.method || 'POST';
  const url = new URL(item.request?.url || 'http://localhost/');
  $('base-url').value = `${url.protocol}//${url.host}`;
  $('path').value = url.pathname;
  $('headers-json').value = pretty(item.request?.headers || {});
  $('body-json').value = pretty(item.request?.body || {});
  $('preset-select').value = item.presetId || '';
  renderResponse(item);
}

function renderHistory() {
  const filter = $('history-filter').value.toLowerCase();
  const items = state.history.filter((item) => JSON.stringify(item).toLowerCase().includes(filter));
  $('history-list').innerHTML = items.map((item) => `
    <button class="history-item" data-id="${item.id}">
      <strong>${item.summary?.classification || 'unknown'}</strong>
      <span>${item.presetId || item.target || 'manual'} · ${item.createdAt}</span>
      <span>${item.request?.url || ''}</span>
    </button>
  `).join('');
  for (const button of document.querySelectorAll('.history-item')) {
    button.addEventListener('click', () => {
      const item = state.history.find((entry) => entry.id === button.dataset.id);
      if (item) restoreHistoryItem(item);
    });
  }
}

async function loadConfig() {
  state.config = await api('/api/config');
  const missing = state.config.missing || [];
  $('env-status').textContent = missing.length ? `missing: ${missing.join(', ')}` : 'env loaded';
  $('env-status').className = `badge ${missing.length ? 'request_error' : 'web_search_used'}`;
}

async function loadPresets() {
  const data = await api('/api/presets');
  state.presets = data.presets || [];
  $('preset-select').innerHTML = state.presets.map((preset) => `<option value="${preset.id}">${preset.label}</option>`).join('');
  if (state.presets[0]) applyPreset(state.presets[0]);
}

async function loadHistory() {
  const data = await api('/api/history');
  state.history = data.items || [];
  renderHistory();
}

async function sendRequest() {
  $('request-error').textContent = '';
  try {
    const request = readRequestForm();
    const data = await api('/api/send', { method: 'POST', body: JSON.stringify(request) });
    renderResponse(data.item);
    await loadHistory();
  } catch (error) {
    $('request-error').textContent = error.message;
  }
}

$('load-env').addEventListener('click', loadConfig);
$('reload-history').addEventListener('click', loadHistory);
$('send-request').addEventListener('click', sendRequest);
$('preset-select').addEventListener('change', () => {
  const preset = state.presets.find((item) => item.id === $('preset-select').value);
  if (preset) applyPreset(preset);
});
$('history-filter').addEventListener('input', renderHistory);

await loadConfig();
await loadPresets();
await loadHistory();
```

- [ ] **Step 4: Smoke start server**

Run:

```bash
cd tools/unode-diagnostics && npm start
```

Expected: prints `unode diagnostics: http://127.0.0.1:5179`.

- [ ] **Step 5: Manual UI smoke**

Open `http://127.0.0.1:5179` and verify:

- `.env` status badge appears.
- Four presets are visible.
- Selecting a preset updates target/path/header/body.
- Response pane starts empty.
- History pane loads without error.

- [ ] **Step 6: Commit**

```bash
git add tools/unode-diagnostics/public/index.html tools/unode-diagnostics/public/styles.css tools/unode-diagnostics/public/app.js
git commit -m "feat(diagnostics): add browser workbench UI"
```

---

### Task 7: End-To-End Verification With Local And Live Requests

**Files:**
- Modify: `tools/unode-diagnostics/README.md`

- [ ] **Step 1: Run unit tests**

Run:

```bash
cd tools/unode-diagnostics && npm test
```

Expected: all tests pass.

- [ ] **Step 2: Verify server health**

Run server:

```bash
cd tools/unode-diagnostics && npm start
```

In another terminal:

```bash
curl -s http://127.0.0.1:5179/healthz
```

Expected:

```json
{
  "ok": true
}
```

- [ ] **Step 3: Verify config endpoint does not expose secret values**

Run:

```bash
curl -s http://127.0.0.1:5179/api/config
```

Expected:

- `display.UNODE_RELAY_API_KEY` is masked.
- `display.NEW_API_RELAY_API_KEY` is masked.
- Response does not contain the full value of `UNODE_RELAY_API_KEY` from `.env`.
- Response does not contain the full value of `NEW_API_RELAY_API_KEY` from `.env`.

- [ ] **Step 4: Verify live preset execution in browser**

Open `http://127.0.0.1:5179` and run each preset once:

- Direct unode `/v1/messages` + `web_search_20250305`
- Direct unode `/v1/chat/completions` + `web_search_options`
- ALRouter `/v1/messages` forced channel
- ALRouter `/v1/chat/completions` forced channel

Expected for each request:

- A new history item appears.
- Response pane shows HTTP status and raw response body.
- Summary cards show request id when present.
- Classification is one of `web_search_used`, `claude_code_session_suspected`, `tool_rate_limited`, `tool_not_used`, `request_error`, or `unknown`.

- [ ] **Step 5: Verify history survives refresh**

Refresh the browser.

Expected:

- Previously sent history items are still visible.
- Clicking a history item restores request headers/body and response headers/body.

- [ ] **Step 6: Verify history file has no full secrets**

Run:

```bash
grep -n "sk-" tools/unode-diagnostics/data/history.jsonl || true
```

Expected: only masked fragments such as `sk-r...cdef` or `Bearer sk-u...3456` appear. No full key from `.env` appears.

- [ ] **Step 7: Add README verification section**

Append to `tools/unode-diagnostics/README.md`:

````markdown
## Verification

```bash
cd tools/unode-diagnostics
npm test
npm start
```

Then open `http://127.0.0.1:5179`.

Run the four presets once and confirm:

- response summary is populated
- history persists after browser refresh
- `tools/unode-diagnostics/data/history.jsonl` contains masked auth values only
````

- [ ] **Step 8: Commit**

```bash
git add tools/unode-diagnostics/README.md
git commit -m "docs(diagnostics): document verification flow"
```

---

## Final Verification Checklist

- [ ] `cd tools/unode-diagnostics && npm test` passes.
- [ ] `cd tools/unode-diagnostics && npm start` starts a local server.
- [ ] Browser opens `http://127.0.0.1:5179`.
- [ ] `.env` values populate presets without showing full keys in stored history.
- [ ] Four presets can be sent.
- [ ] Response pane shows headers/body and classification.
- [ ] History persists after refresh.
- [ ] Clicking history restores prior request/response panes.
- [ ] `git status --short` shows only intended implementation files, with no committed runtime history.

## Spec Coverage Review

- Goals 1-2 are covered by Tasks 5 and 6: local server, `.env` config endpoint, browser UI.
- Preset and free editing goal is covered by Tasks 3 and 6.
- File DB persistence is covered by Task 4 and Task 7.
- Secret masking is covered by Task 2, Task 4, and Task 7.
- Automatic response summary is covered by Task 3.
- Local-only standalone scope is covered by Task 1 and Task 5.
