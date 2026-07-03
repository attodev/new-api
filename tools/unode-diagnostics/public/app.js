import { displayValue, itemSearchText } from './ui-helpers.js';

const state = {
  config: null,
  presets: [],
  history: [],
  selectedHistoryId: ''
};

const ids = [
  'base-url',
  'body-json',
  'classification',
  'config-display',
  'env-status',
  'headers-json',
  'history-count',
  'history-filter',
  'history-list',
  'load-env',
  'method',
  'path',
  'preset-select',
  'reload-history',
  'request-error',
  'request-status',
  'response-body',
  'response-headers',
  'response-status',
  'send-request',
  'summary',
  'target',
  'workbench-status'
];

const els = Object.fromEntries(ids.map((id) => [id, document.getElementById(id)]));
const ALLOWED_METHODS = new Set(['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD']);

function pretty(value) {
  if (typeof value === 'string') return value;
  return JSON.stringify(value ?? {}, null, 2);
}

function setMessage(element, message = '') {
  element.textContent = message;
}

function setBusy(isBusy) {
  els['send-request'].disabled = isBusy;
  els['send-request'].textContent = isBusy ? 'Sending...' : 'Send API';
}

function safeClassName(value) {
  return String(value || 'neutral').replace(/[^a-z0-9_-]/gi, '_');
}

function setBadge(element, label, className = 'neutral') {
  element.textContent = label;
  element.className = `badge ${safeClassName(className)}`;
}

function clearChildren(element) {
  element.replaceChildren();
}

function appendEmptyState(element, text) {
  const node = document.createElement('div');
  node.className = 'empty-state';
  node.textContent = text;
  element.append(node);
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (!headers.has('content-type')) headers.set('content-type', 'application/json');

  const response = await fetch(path, {
    cache: 'no-store',
    ...options,
    headers
  });
  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = { message: text };
    }
  }

  if (!response.ok) {
    const message = payload?.message || payload?.error || `${response.status} ${response.statusText}`;
    throw new Error(message);
  }

  return payload ?? {};
}

function baseUrlForTarget(target) {
  const display = state.config?.display || {};
  if (target === 'alrouter') return display.NEW_API_BASE_URL || '';
  return display.UNODE_BASE_URL || '';
}

function applyPreset(preset) {
  if (!preset) return;
  els['preset-select'].value = preset.id;
  els.target.value = preset.target || 'unode';
  els.method.value = String(preset.method || 'POST').toUpperCase();
  els['base-url'].value = preset.baseUrl || baseUrlForTarget(preset.target);
  els.path.value = preset.path || '';
  els['headers-json'].value = pretty(preset.headers || {});
  els['body-json'].value = pretty(preset.body || {});
  state.selectedHistoryId = '';
  setMessage(els['request-error']);
  setMessage(els['request-status'], `Preset loaded: ${preset.label || preset.id}`);
  renderHistory();
}

function parseJsonEditor(element, label, mustBeObject = false) {
  const raw = element.value.trim();
  if (!raw) return {};

  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    throw new Error(`${label} JSON parse error: ${error.message}`);
  }

  if (mustBeObject && (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed))) {
    throw new Error(`${label} JSON must be an object`);
  }

  return parsed;
}

function readRequestForm() {
  const method = els.method.value.trim().toUpperCase() || 'POST';
  const baseUrl = els['base-url'].value.trim();
  const path = els.path.value.trim();

  if (!ALLOWED_METHODS.has(method)) throw new Error(`Method is not allowed: ${method}`);
  if (!baseUrl) throw new Error('Base URL is required');
  if (!path) throw new Error('Path is required');

  return {
    presetId: els['preset-select'].value,
    target: els.target.value,
    method,
    baseUrl,
    path,
    headers: parseJsonEditor(els['headers-json'], 'Headers', true),
    body: parseJsonEditor(els['body-json'], 'Body'),
    authMode: 'env'
  };
}

function renderConfig() {
  clearChildren(els['config-display']);
  const display = state.config?.display || {};
  const entries = Object.entries(display);
  if (!entries.length) {
    appendEmptyState(els['config-display'], 'Masked .env values will appear here.');
    return;
  }

  for (const [key, value] of entries) {
    const row = document.createElement('div');
    row.className = 'config-row';
    const name = document.createElement('strong');
    name.textContent = key;
    const val = document.createElement('span');
    val.textContent = displayValue(value, 'not set');
    row.append(name, val);
    els['config-display'].append(row);
  }
}

function renderPresets() {
  clearChildren(els['preset-select']);
  for (const preset of state.presets) {
    const option = document.createElement('option');
    option.value = preset.id;
    option.textContent = preset.label || preset.id;
    els['preset-select'].append(option);
  }
}

function summaryEntries(item) {
  const summary = item?.summary || {};
  const response = item?.response || {};
  return [
    ['HTTP status', displayValue(response.status)],
    ['request id', displayValue(summary.requestId)],
    ['upstream request id', displayValue(summary.upstreamRequestId)],
    ['x-cc-session-id', summary.hasCcSessionId ? 'present' : 'absent'],
    ['web search requests', displayValue(summary.webSearchRequests, '0')],
    ['tool errors', displayValue(summary.toolErrorCodes)],
    ['content types', displayValue(summary.contentTypes)],
    ['hints', displayValue(summary.hints)]
  ];
}

function renderSummary(item) {
  clearChildren(els.summary);
  const classification = item?.summary?.classification || 'none';
  setBadge(els.classification, classification, classification === 'none' ? 'neutral' : classification);

  for (const [label, value] of summaryEntries(item)) {
    const card = document.createElement('div');
    card.className = 'summary-card';
    const name = document.createElement('span');
    name.textContent = label;
    const content = document.createElement('strong');
    content.textContent = value;
    card.append(name, content);
    els.summary.append(card);
  }
}

function renderResponse(item) {
  renderSummary(item);
  const response = item?.response || {};
  els['response-status'].textContent = response.status ? `HTTP ${response.status}` : 'Waiting for response';
  els['response-headers'].value = pretty(response.headers || {});

  if (response.json !== undefined) {
    els['response-body'].value = pretty(response.json);
  } else if (response.bodyText !== undefined) {
    els['response-body'].value = String(response.bodyText);
  } else if (item?.error) {
    els['response-body'].value = pretty(item.error);
  } else {
    els['response-body'].value = '';
  }
}

function requestUrlParts(url) {
  try {
    const parsed = new URL(url);
    return {
      baseUrl: parsed.origin,
      path: `${parsed.pathname}${parsed.search}`
    };
  } catch {
    return {
      baseUrl: '',
      path: ''
    };
  }
}

function restoreHistoryItem(item) {
  if (!item) return;
  const request = item.request || {};
  const parts = requestUrlParts(request.url || '');

  state.selectedHistoryId = item.id || '';
  els.target.value = item.target || 'unode';
  els.method.value = String(request.method || 'POST').toUpperCase();
  els['base-url'].value = parts.baseUrl || baseUrlForTarget(item.target);
  els.path.value = parts.path || '';
  els['headers-json'].value = pretty(request.headers || {});
  els['body-json'].value = pretty(request.body || {});
  els['preset-select'].value = item.presetId || '';
  setMessage(els['request-error']);
  setMessage(els['request-status'], `History restored: ${item.id || 'selected item'}`);
  renderResponse(item);
  renderHistory();
}

function renderHistory() {
  clearChildren(els['history-list']);
  const filter = els['history-filter'].value.trim().toLowerCase();
  const items = state.history.filter((item) => !filter || itemSearchText(item).includes(filter));
  els['history-count'].textContent = `${items.length} of ${state.history.length} records`;

  if (!items.length) {
    appendEmptyState(els['history-list'], filter ? 'No matching history.' : 'No history yet.');
    return;
  }

  for (const item of items) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = item.id === state.selectedHistoryId ? 'history-item active' : 'history-item';
    button.dataset.id = item.id || '';

    const title = document.createElement('div');
    title.className = 'history-item-title';
    const classification = document.createElement('strong');
    classification.textContent = item.summary?.classification || 'unknown';
    const status = document.createElement('span');
    status.textContent = displayValue(item.response?.status);
    title.append(classification, status);

    const preset = document.createElement('span');
    preset.textContent = `${item.presetId || item.target || 'manual'} · ${item.createdAt || ''}`;
    const url = document.createElement('span');
    url.textContent = item.request?.url || '';

    button.append(title, preset, url);
    button.addEventListener('click', () => loadHistoryItem(item.id));
    els['history-list'].append(button);
  }
}

async function loadConfig() {
  setMessage(els['workbench-status'], 'Loading local configuration...');
  const config = await api('/api/config');
  state.config = config;
  const missing = config.missing || [];
  if (missing.length) {
    setBadge(els['env-status'], `missing ${missing.length}`, 'request_error');
    setMessage(els['workbench-status'], `Missing env: ${missing.join(', ')}`);
  } else {
    setBadge(els['env-status'], 'env loaded', 'web_search_used');
    setMessage(els['workbench-status'], 'Masked local configuration loaded');
  }
  renderConfig();

  const preset = state.presets.find((item) => item.id === els['preset-select'].value);
  if (preset && !els['base-url'].value.trim()) {
    els['base-url'].value = baseUrlForTarget(preset.target);
  }
}

async function loadPresets() {
  const data = await api('/api/presets');
  state.presets = data.presets || [];
  renderPresets();
  applyPreset(state.presets[0]);
}

async function loadHistory() {
  const data = await api('/api/history');
  state.history = data.items || [];
  renderHistory();
}

async function loadHistoryItem(id) {
  if (!id) return;
  setMessage(els['request-status'], `Loading history ${id}...`);
  try {
    const data = await api(`/api/history/${encodeURIComponent(id)}`);
    if (!data.item) throw new Error(`History item not found: ${id}`);
    restoreHistoryItem(data.item);
  } catch (error) {
    setMessage(els['request-error'], error.message);
  }
}

async function sendRequest() {
  setMessage(els['request-error']);
  let request;
  try {
    request = readRequestForm();
  } catch (error) {
    setMessage(els['request-error'], error.message);
    return;
  }

  setBusy(true);
  setMessage(els['request-status'], 'Sending request...');
  try {
    const data = await api('/api/send', {
      method: 'POST',
      body: JSON.stringify(request)
    });
    state.selectedHistoryId = data.item?.id || '';
    setMessage(els['request-status'], `Stored history: ${state.selectedHistoryId || 'sent request'}`);
    renderResponse(data.item);
    await loadHistory();
  } catch (error) {
    setMessage(els['request-error'], error.message);
    setMessage(els['request-status'], 'Request failed');
  } finally {
    setBusy(false);
  }
}

function wireEvents() {
  els['load-env'].addEventListener('click', () => {
    loadConfig().catch((error) => {
      setBadge(els['env-status'], 'env error', 'request_error');
      setMessage(els['request-error'], error.message);
    });
  });
  els['reload-history'].addEventListener('click', () => {
    loadHistory().catch((error) => setMessage(els['request-error'], error.message));
  });
  els['send-request'].addEventListener('click', sendRequest);
  els['preset-select'].addEventListener('change', () => {
    const preset = state.presets.find((item) => item.id === els['preset-select'].value);
    applyPreset(preset);
  });
  els.target.addEventListener('change', () => {
    if (!els['base-url'].value.trim()) els['base-url'].value = baseUrlForTarget(els.target.value);
  });
  els['history-filter'].addEventListener('input', renderHistory);
}

async function init() {
  wireEvents();
  renderConfig();
  renderSummary(null);
  try {
    await loadConfig();
    await loadPresets();
    await loadHistory();
  } catch (error) {
    setBadge(els['env-status'], 'startup error', 'request_error');
    setMessage(els['request-error'], error.message);
    setMessage(els['workbench-status'], 'Startup API call failed');
  }
}

init();
