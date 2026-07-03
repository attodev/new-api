function targetConfig(target, env = {}) {
  if (target === 'unode') {
    return {
      baseUrl: env.UNODE_BASE_URL,
      headers: {
        authorization: `Bearer ${env.UNODE_RELAY_API_KEY || ''}`,
        'x-api-key': env.UNODE_RELAY_API_KEY || ''
      }
    };
  }
  if (target === 'alrouter') {
    return {
      baseUrl: env.NEW_API_BASE_URL,
      headers: {
        authorization: `Bearer ${env.NEW_API_RELAY_API_KEY || ''}`,
        'x-api-key': env.NEW_API_RELAY_API_KEY || ''
      }
    };
  }
  return null;
}

function modelIdFromItem(item) {
  if (typeof item === 'string') return item;
  if (!item || typeof item !== 'object') return '';
  return String(item.id ?? item.name ?? item.model ?? '').trim();
}

function modelLabelFromItem(item, id) {
  if (typeof item === 'string') return item;
  if (!item || typeof item !== 'object') return id;
  return String(item.display_name ?? item.name ?? item.label ?? id).trim() || id;
}

function modelSourceArray(payload) {
  if (Array.isArray(payload)) return payload;
  if (Array.isArray(payload?.data)) return payload.data;
  if (Array.isArray(payload?.models)) return payload.models;
  return [];
}

export function normalizeModelList(payload) {
  const models = [];
  const seen = new Set();

  for (const item of modelSourceArray(payload)) {
    const id = modelIdFromItem(item);
    if (!id || seen.has(id)) continue;
    seen.add(id);
    const label = modelLabelFromItem(item, id);
    models.push({
      id,
      label,
      created: item && typeof item === 'object' ? item.created ?? item.created_at ?? '' : '',
      ownedBy: item && typeof item === 'object' ? item.owned_by ?? item.provider ?? '' : ''
    });
  }

  return models.sort((a, b) => a.id.localeCompare(b.id));
}

export function modelsRequestForTarget(target, env = {}) {
  const config = targetConfig(target, env);
  if (!config) {
    const error = new Error('target must be one of: unode, alrouter');
    error.statusCode = 400;
    error.errorCode = 'invalid_target';
    throw error;
  }
  if (!config.baseUrl) {
    const error = new Error(`base URL is not configured for target: ${target}`);
    error.statusCode = 400;
    error.errorCode = 'missing_target_base_url';
    throw error;
  }

  return {
    method: 'GET',
    url: `${String(config.baseUrl).replace(/\/+$/, '')}/v1/models`,
    headers: Object.fromEntries(Object.entries(config.headers).filter(([, value]) => value && value !== 'Bearer '))
  };
}

export async function fetchTargetModels(target, env = {}, fetchImpl = fetch) {
  const request = modelsRequestForTarget(target, env);
  const response = await fetchImpl(request.url, {
    method: request.method,
    headers: request.headers
  });
  const bodyText = await response.text();
  let json = null;
  try {
    json = bodyText ? JSON.parse(bodyText) : null;
  } catch {
    json = null;
  }

  if (!response.ok) {
    const error = new Error(`model list request failed with HTTP ${response.status}`);
    error.statusCode = 502;
    error.errorCode = 'model_list_failed';
    throw error;
  }

  return {
    target,
    url: request.url,
    status: response.status,
    models: normalizeModelList(json)
  };
}
