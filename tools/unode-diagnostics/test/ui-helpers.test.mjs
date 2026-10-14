import assert from 'node:assert/strict';
import test from 'node:test';

import { baseUrlForTarget, displayValue, itemSearchText, modelOptionLabel, summaryHintTokens } from '../public/ui-helpers.js';

test('summaryHintTokens preserves array hint text for history filtering', () => {
  const tokens = summaryHintTokens([
    'request id: rid-1',
    'tool returned too_many_requests',
    'web search requests: 2'
  ]);

  assert.deepEqual(tokens, [
    'request id: rid-1',
    'tool returned too_many_requests',
    'web search requests: 2'
  ]);
});

test('summaryHintTokens supports object hint maps without leaking false entries', () => {
  const tokens = summaryHintTokens({
    hasWebSearchText: true,
    hasUnavailableText: false,
    note: 'manual override'
  });

  assert.deepEqual(tokens, ['hasWebSearchText', 'note manual override']);
});

test('itemSearchText includes real hint content from array and object records', () => {
  const arrayRecord = {
    id: 'diag-1',
    presetId: 'direct-messages-web-search',
    request: { method: 'POST', url: 'https://example.test/v1/messages' },
    response: { status: 200 },
    summary: {
      classification: 'tool_rate_limited',
      hints: ['request id: rid-1', 'tool returned too_many_requests', 'web search requests: 2']
    }
  };
  const objectRecord = {
    id: 'diag-2',
    summary: { hints: { hasUnavailableText: true, note: 'web search unavailable' } }
  };

  assert.match(itemSearchText(arrayRecord), /web search requests: 2/);
  assert.match(itemSearchText(arrayRecord), /too_many_requests/);
  assert.match(itemSearchText(objectRecord), /hasunavailabletext/);
  assert.match(itemSearchText(objectRecord), /web search unavailable/);
});

test('displayValue renders arrays and object hint maps as readable labels', () => {
  assert.equal(displayValue(['request id: rid-1', 'web search requests: 2']), 'request id: rid-1, web search requests: 2');
  assert.equal(displayValue({ hasWebSearchText: true, note: 'manual' }), 'hasWebSearchText, note: manual');
  assert.equal(displayValue({ hasWebSearchText: false }), '-');
});

test('baseUrlForTarget returns the URL matching the selected target', () => {
  const display = {
    UNODE_BASE_URL: 'https://www.unodetech.xyz',
    NEW_API_BASE_URL: 'https://alrouter.ai'
  };

  assert.equal(baseUrlForTarget('unode', display), 'https://www.unodetech.xyz');
  assert.equal(baseUrlForTarget('alrouter', display), 'https://alrouter.ai');
  assert.equal(baseUrlForTarget('unknown', display), 'https://www.unodetech.xyz');
});

test('modelOptionLabel includes display names when available', () => {
  assert.equal(modelOptionLabel({ id: 'claude-sonnet-4-20250514', label: 'Claude Sonnet 4' }), 'claude-sonnet-4-20250514 - Claude Sonnet 4');
  assert.equal(modelOptionLabel({ id: 'gpt-4.1', label: 'gpt-4.1' }), 'gpt-4.1');
});
