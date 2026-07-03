export function displayValue(value, fallback = '-') {
  if (value == null || value === '') return fallback;
  if (Array.isArray(value)) {
    const items = value.map((item) => displayValue(item, '')).filter(Boolean);
    return items.length ? items.join(', ') : fallback;
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value)
      .filter(([, child]) => Boolean(child))
      .map(([key, child]) => (typeof child === 'string' ? `${key}: ${child}` : key));
    return entries.length ? entries.join(', ') : fallback;
  }
  return String(value);
}

export function summaryHintTokens(hints) {
  if (Array.isArray(hints)) {
    return hints.map((hint) => String(hint)).filter(Boolean);
  }
  if (hints && typeof hints === 'object') {
    return Object.entries(hints)
      .filter(([, value]) => Boolean(value))
      .map(([key, value]) => (typeof value === 'string' ? `${key} ${value}` : key));
  }
  if (typeof hints === 'string' && hints) return [hints];
  return [];
}

export function itemSearchText(item = {}) {
  return [
    item.id,
    item.createdAt,
    item.presetId,
    item.target,
    item.request?.method,
    item.request?.url,
    item.response?.status,
    item.summary?.classification,
    ...summaryHintTokens(item.summary?.hints)
  ].join(' ').toLowerCase();
}

export function baseUrlForTarget(target, display = {}) {
  if (target === 'alrouter') return display.NEW_API_BASE_URL || '';
  return display.UNODE_BASE_URL || '';
}
