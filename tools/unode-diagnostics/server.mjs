import { createReadStream } from 'node:fs';
import { stat } from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { loadEnvConfig } from './lib/env.mjs';
import { appendHistory, readHistory, readHistoryById } from './lib/history.mjs';
import { expandEnvTemplates, prepareOutboundRequest, sendDiagnosticRequest } from './lib/http-client.mjs';
import { buildPresets } from './lib/presets.mjs';
import { summarizeExchange } from './lib/summary.mjs';

const moduleDir = path.dirname(fileURLToPath(import.meta.url));
const defaultRepoRoot = path.resolve(moduleDir, '../..');
const defaultPublicDir = path.join(moduleDir, 'public');
const defaultHistoryPath = path.join(moduleDir, 'data/history.jsonl');
const ALLOWED_SEND_METHODS = new Set(['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD']);
const SENSITIVE_ENV_KEY_PATTERN = /(KEY|TOKEN|SECRET|PASSWORD)/i;

const MIME_TYPES = new Map([
  ['.css', 'text/css; charset=utf-8'],
  ['.html', 'text/html; charset=utf-8'],
  ['.ico', 'image/x-icon'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.json', 'application/json; charset=utf-8'],
  ['.map', 'application/json; charset=utf-8'],
  ['.png', 'image/png'],
  ['.svg', 'image/svg+xml; charset=utf-8'],
  ['.txt', 'text/plain; charset=utf-8'],
  ['.webp', 'image/webp']
]);

function sendJson(res, statusCode, payload) {
  const body = JSON.stringify(payload);
  res.writeHead(statusCode, {
    'content-type': 'application/json; charset=utf-8',
    'cache-control': 'no-store',
    'content-length': Buffer.byteLength(body)
  });
  res.end(body);
}

function sendMethodNotAllowed(res, methods) {
  res.writeHead(405, {
    allow: methods.join(', '),
    'cache-control': 'no-store'
  });
  res.end();
}

function requestError(statusCode, errorCode, message) {
  const error = new Error(message);
  error.statusCode = statusCode;
  error.errorCode = errorCode;
  return error;
}

function isPlainObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

async function readJsonBody(req) {
  const chunks = [];
  let size = 0;
  const maxBytes = 1024 * 1024;

  for await (const chunk of req) {
    size += chunk.length;
    if (size > maxBytes) {
      const error = new Error('request body too large');
      error.statusCode = 413;
      throw error;
    }
    chunks.push(chunk);
  }

  const bodyText = Buffer.concat(chunks).toString('utf8');
  if (!bodyText.trim()) return {};

  try {
    return JSON.parse(bodyText);
  } catch {
    const error = new Error('invalid JSON request body');
    error.statusCode = 400;
    throw error;
  }
}

function isLocalHostname(hostname) {
  const normalized = hostname.toLowerCase();
  return normalized === 'localhost' || normalized === '127.0.0.1' || normalized === '::1' || normalized === '[::1]';
}

function allowsLocalOrigin(req) {
  const origin = req.headers.origin || req.headers.referer;
  if (!origin) return true;

  try {
    const originUrl = new URL(origin);
    return isLocalHostname(originUrl.hostname) && (!req.headers.host || originUrl.host === req.headers.host);
  } catch {
    return false;
  }
}

function resolveStaticPath(publicDir, pathname) {
  let decodedPathname;
  try {
    decodedPathname = decodeURIComponent(pathname);
  } catch {
    return null;
  }

  const relativePath = decodedPathname === '/' ? 'index.html' : decodedPathname.replace(/^\/+/, '');
  const publicRoot = path.resolve(publicDir);
  const filePath = path.resolve(publicRoot, relativePath);
  if (filePath !== publicRoot && !filePath.startsWith(`${publicRoot}${path.sep}`)) return null;
  return filePath;
}

async function sendStatic(req, res, publicDir, pathname) {
  if (req.method !== 'GET' && req.method !== 'HEAD') return false;

  const filePath = resolveStaticPath(publicDir, pathname);
  if (!filePath) {
    sendJson(res, 403, { error: 'forbidden' });
    return true;
  }

  let fileStat;
  try {
    fileStat = await stat(filePath);
  } catch (error) {
    if (error.code === 'ENOENT' || error.code === 'ENOTDIR') return false;
    throw error;
  }
  if (!fileStat.isFile()) return false;

  res.writeHead(200, {
    'content-type': MIME_TYPES.get(path.extname(filePath).toLowerCase()) ?? 'application/octet-stream',
    'content-length': fileStat.size
  });
  if (req.method === 'HEAD') {
    res.end();
    return true;
  }

  await new Promise((resolve, reject) => {
    createReadStream(filePath)
      .on('error', reject)
      .on('end', resolve)
      .pipe(res);
  });
  return true;
}

function routeHistoryId(pathname) {
  if (!pathname.startsWith('/api/history/')) return null;
  const rawId = pathname.slice('/api/history/'.length);
  if (!rawId) return null;
  try {
    return decodeURIComponent(rawId);
  } catch {
    return rawId;
  }
}

function historyRecordFromExchange(input, exchange, summary) {
  return {
    presetId: input?.presetId ?? '',
    target: input?.target ?? '',
    request: {
      method: exchange.request.method,
      url: exchange.request.url,
      headers: exchange.request.headers,
      body: exchange.request.body
    },
    response: exchange.response,
    error: exchange.error,
    summary
  };
}

function allowedOriginsFromEnv(env) {
  const origins = new Set();
  for (const key of ['UNODE_BASE_URL', 'NEW_API_BASE_URL']) {
    const value = String(env[key] ?? '').trim();
    if (!value) continue;
    try {
      origins.add(new URL(value).origin);
    } catch {
      continue;
    }
  }
  return origins;
}

function assertAllowedAbsoluteUrl(value, allowedOrigins, fieldName) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw requestError(400, 'invalid_request', `${fieldName} is invalid`);
  }
  if (!allowedOrigins.has(url.origin)) {
    throw requestError(400, 'invalid_request', `${fieldName} origin is not allowed`);
  }
}

function knownSecretsFromEnv(env) {
  return Object.entries(env)
    .filter(([key, value]) => SENSITIVE_ENV_KEY_PATTERN.test(key) && value)
    .map(([, value]) => String(value));
}

function validateSendInput(input, env) {
  if (!isPlainObject(input)) {
    throw requestError(400, 'invalid_request', 'request body must be a JSON object');
  }

  if (input.method !== undefined && typeof input.method !== 'string') {
    throw requestError(400, 'invalid_request', 'method must be a string');
  }
  const method = String(input.method ?? 'GET').toUpperCase();
  if (!ALLOWED_SEND_METHODS.has(method)) {
    throw requestError(400, 'invalid_request', 'method is not allowed');
  }

  if (input.headers !== undefined && !isPlainObject(input.headers)) {
    throw requestError(400, 'invalid_request', 'headers must be an object');
  }
  if (typeof input.baseUrl !== 'string' || !input.baseUrl.trim()) {
    throw requestError(400, 'invalid_request', 'baseUrl is required');
  }
  if (typeof input.path !== 'string' || !input.path.trim()) {
    throw requestError(400, 'invalid_request', 'path is required');
  }

  const allowedOrigins = allowedOriginsFromEnv(env);
  if (allowedOrigins.size === 0) {
    throw requestError(400, 'invalid_request', 'no allowed outbound origins are configured');
  }

  const expandedBaseUrl = expandEnvTemplates(input.baseUrl, env).trim();
  const expandedPath = expandEnvTemplates(input.path, env).trim();
  assertAllowedAbsoluteUrl(expandedBaseUrl, allowedOrigins, 'baseUrl');
  if (/^https?:\/\//i.test(expandedPath)) {
    assertAllowedAbsoluteUrl(expandedPath, allowedOrigins, 'path');
  }

  let preparedUrl;
  try {
    preparedUrl = new URL(prepareOutboundRequest(input, env).url);
  } catch {
    throw requestError(400, 'invalid_request', 'outbound URL is invalid');
  }

  if (!allowedOrigins.has(preparedUrl.origin)) {
    throw requestError(400, 'invalid_request', 'outbound URL origin is not allowed');
  }
}

export function createDiagnosticsServer(options = {}) {
  const repoRoot = options.repoRoot ?? defaultRepoRoot;
  const publicDir = options.publicDir ?? defaultPublicDir;
  const historyPath = options.historyPath ?? defaultHistoryPath;
  const fetchImpl = options.fetchImpl ?? fetch;

  return http.createServer(async (req, res) => {
    try {
      const requestUrl = new URL(req.url ?? '/', 'http://127.0.0.1');
      const { pathname } = requestUrl;

      if (pathname === '/healthz') {
        if (req.method !== 'GET') return sendMethodNotAllowed(res, ['GET']);
        return sendJson(res, 200, { ok: true });
      }

      if (pathname === '/api/config') {
        if (req.method !== 'GET') return sendMethodNotAllowed(res, ['GET']);
        const { display, missing } = await loadEnvConfig(repoRoot);
        return sendJson(res, 200, { display, missing });
      }

      if (pathname === '/api/presets') {
        if (req.method !== 'GET') return sendMethodNotAllowed(res, ['GET']);
        return sendJson(res, 200, { presets: buildPresets() });
      }

      if (pathname === '/api/history') {
        if (req.method !== 'GET') return sendMethodNotAllowed(res, ['GET']);
        const items = await readHistory(historyPath);
        return sendJson(res, 200, { items });
      }

      const historyId = routeHistoryId(pathname);
      if (historyId) {
        if (req.method !== 'GET') return sendMethodNotAllowed(res, ['GET']);
        const item = await readHistoryById(historyPath, historyId);
        return sendJson(res, 200, { item });
      }

      if (pathname === '/api/send') {
        if (req.method !== 'POST') return sendMethodNotAllowed(res, ['POST']);
        if (!allowsLocalOrigin(req)) return sendJson(res, 403, { error: 'forbidden' });
        const input = await readJsonBody(req);
        const config = await loadEnvConfig(repoRoot);
        validateSendInput(input, config.secrets);
        const exchange = await sendDiagnosticRequest(input, config.secrets, fetchImpl);
        const summary = summarizeExchange(exchange);
        const stored = await appendHistory(
          historyPath,
          historyRecordFromExchange(input, exchange, summary),
          knownSecretsFromEnv(config.secrets)
        );
        return sendJson(res, 200, { item: stored });
      }

      if (pathname.startsWith('/api/')) {
        return sendJson(res, 404, { error: 'not_found' });
      }

      if (await sendStatic(req, res, publicDir, pathname)) return undefined;
      return sendJson(res, 404, { error: 'not_found' });
    } catch (error) {
      const statusCode = error.statusCode && Number.isInteger(error.statusCode) ? error.statusCode : 500;
      return sendJson(res, statusCode, {
        error: statusCode >= 500 ? 'internal_server_error' : error.errorCode ?? error.message,
        ...(statusCode >= 500 ? {} : { message: error.message })
      });
    }
  });
}

function isCliEntrypoint() {
  return Boolean(process.argv[1]) && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;
}

if (isCliEntrypoint()) {
  const host = process.env.UNODE_DIAGNOSTICS_HOST ?? process.env.HOST ?? '127.0.0.1';
  const port = Number(process.env.UNODE_DIAGNOSTICS_PORT ?? process.env.PORT ?? 5179);
  const server = createDiagnosticsServer();

  server.listen(port, host, () => {
    console.log(`unode diagnostics server listening at http://${host}:${port}`);
  });
}
