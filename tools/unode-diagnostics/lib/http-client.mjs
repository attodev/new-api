const ENV_TEMPLATE_PATTERN = /\$\{([A-Za-z_][A-Za-z0-9_]*)\}/g;

export function expandEnvTemplates(value, env = {}) {
  if (typeof value === 'string') {
    return value.replace(ENV_TEMPLATE_PATTERN, (_, name) => String(env[name] ?? ''));
  }
  if (Array.isArray(value)) {
    return value.map((item) => expandEnvTemplates(item, env));
  }
  if (value && typeof value === 'object') {
    const out = {};
    for (const [key, child] of Object.entries(value)) {
      out[key] = expandEnvTemplates(child, env);
    }
    return out;
  }
  return value;
}

function buildUrl(baseUrl = '', requestPath = '') {
  const pathText = String(requestPath ?? '').trim();
  if (/^https?:\/\//i.test(pathText)) return pathText;

  const baseText = String(baseUrl ?? '').trim();
  if (!baseText) return pathText;
  if (!pathText) return baseText;

  return `${baseText.replace(/\/+$/, '')}/${pathText.replace(/^\/+/, '')}`;
}

function normalizeHeaders(headers = {}) {
  const out = {};
  for (const [key, value] of Object.entries(headers ?? {})) {
    if (value == null) continue;
    out[key] = Array.isArray(value) ? value.map((item) => String(item)).join(', ') : String(value);
  }
  return out;
}

function bodyToText(body) {
  if (body == null) return undefined;
  if (typeof body === 'string') return body;
  return JSON.stringify(body);
}

function headersToObject(headers) {
  const out = {};
  if (!headers) return out;
  if (typeof headers.forEach === 'function') {
    headers.forEach((value, key) => {
      out[key] = value;
    });
    return out;
  }
  for (const [key, value] of Object.entries(headers)) {
    out[key] = Array.isArray(value) ? value.join(', ') : String(value);
  }
  return out;
}

function timeoutFromInput(input = {}) {
  const timeoutMs = Number(input.timeoutMs ?? input.timeout ?? 90000);
  return Number.isFinite(timeoutMs) && timeoutMs > 0 ? timeoutMs : 90000;
}

function parseJsonBody(bodyText) {
  if (!bodyText) return undefined;
  try {
    return JSON.parse(bodyText);
  } catch {
    return undefined;
  }
}

export function prepareOutboundRequest(input = {}, env = {}) {
  const expanded = expandEnvTemplates(input, env);
  const method = String(expanded.method ?? 'GET').toUpperCase();
  const headers = normalizeHeaders(expanded.headers);
  const body = expanded.body;
  const bodyText = bodyToText(body);

  return {
    method,
    url: buildUrl(expanded.baseUrl, expanded.path),
    headers,
    body,
    bodyText
  };
}

export async function sendDiagnosticRequest(input, env, fetchImpl = fetch) {
  const prepared = prepareOutboundRequest(input, env);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutFromInput(input));

  try {
    const init = {
      method: prepared.method,
      headers: prepared.headers,
      signal: controller.signal
    };

    if (prepared.method !== 'GET' && prepared.method !== 'HEAD' && prepared.bodyText !== undefined) {
      init.body = prepared.bodyText;
    }

    const response = await fetchImpl(prepared.url, init);
    const bodyText = await response.text();

    return {
      request: prepared,
      response: {
        status: response.status,
        statusText: response.statusText,
        headers: headersToObject(response.headers),
        bodyText,
        json: parseJsonBody(bodyText)
      }
    };
  } catch (error) {
    const name = error?.name || 'NetworkError';
    const message = error instanceof Error ? error.message : String(error);

    return {
      request: prepared,
      response: {
        status: 0,
        statusText: name,
        headers: {},
        bodyText: message,
        json: undefined
      },
      error: {
        name,
        message,
        timeout: name === 'AbortError'
      }
    };
  } finally {
    clearTimeout(timeout);
  }
}
