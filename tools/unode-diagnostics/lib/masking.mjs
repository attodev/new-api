const SECRET_KEY_NAMES = new Set([
  'authorization',
  'x_api_key',
  'api_key',
  'x_goog_api_key',
  'new_api_user',
  'cookie',
  'set_cookie',
  'access_token',
  'refresh_token',
  'client_secret',
  'token',
  'tokens',
  'key'
]);

function looksSecretKey(key) {
  const normalized = String(key)
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '');
  return SECRET_KEY_NAMES.has(normalized);
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
