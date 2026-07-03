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
  const shouldMask = looksSecretKey(parentKey);
  if (Array.isArray(value)) {
    return value.map((item) => maskSecrets(item, parentKey));
  }
  if (value && typeof value === 'object') {
    const out = {};
    for (const [key, child] of Object.entries(value)) {
      out[key] = maskSecrets(child, shouldMask ? parentKey : key);
    }
    return out;
  }
  return shouldMask ? maskValue(value) : value;
}
