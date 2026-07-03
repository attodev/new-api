function headerValue(headers, name) {
  if (!headers) return '';
  if (typeof headers.get === 'function') {
    return headers.get(name) ?? headers.get(name.toLowerCase()) ?? '';
  }

  const target = name.toLowerCase();
  for (const [key, value] of Object.entries(headers)) {
    if (key.toLowerCase() !== target) continue;
    if (Array.isArray(value)) return value.join(', ');
    return value == null ? '' : String(value);
  }
  return '';
}

function normalizeStatus(status) {
  if (typeof status === 'number') return status;
  if (typeof status === 'string' && status.trim()) {
    const parsed = Number(status);
    if (Number.isFinite(parsed)) return parsed;
  }
  return status;
}

function responseBlocks(json) {
  if (!json || typeof json !== 'object') return [];
  if (Array.isArray(json.content)) return json.content;

  if (Array.isArray(json.choices)) {
    const blocks = [];
    for (const choice of json.choices) {
      const message = choice?.message ?? choice?.delta;
      if (!message || typeof message !== 'object') continue;

      if (Array.isArray(message.content)) {
        blocks.push(...message.content);
      } else if (typeof message.content === 'string') {
        blocks.push({ type: 'message_text', text: message.content });
      }

      if (typeof message.refusal === 'string') {
        blocks.push({ type: 'message_refusal', text: message.refusal });
      }

      if (Array.isArray(message.tool_calls)) {
        for (const toolCall of message.tool_calls) {
          blocks.push({
            type: toolCall.type ?? 'tool_call',
            name: toolCall.function?.name ?? toolCall.name,
            content: toolCall.function?.arguments ?? toolCall.content
          });
        }
      }
    }
    return blocks;
  }

  return [];
}

function appendBlockText(block, texts) {
  if (!block || typeof block !== 'object') return;
  if (typeof block.text === 'string') texts.push(block.text);
  if (typeof block.content === 'string') texts.push(block.content);
  if (Array.isArray(block.content)) {
    for (const child of block.content) {
      appendBlockText(typeof child === 'object' ? child : { content: child }, texts);
    }
  } else if (block.content && typeof block.content === 'object') {
    appendBlockText(block.content, texts);
  }
}

function unique(values) {
  return [...new Set(values.filter(Boolean))];
}

function collectErrorCodes(value, codes) {
  if (Array.isArray(value)) {
    for (const item of value) {
      collectErrorCodes(item, codes);
    }
    return;
  }

  if (!value || typeof value !== 'object') return;

  for (const [key, child] of Object.entries(value)) {
    if (key === 'error_code' && typeof child === 'string') {
      codes.push(child);
      continue;
    }
    collectErrorCodes(child, codes);
  }
}

function toolErrorCodes(blocks) {
  const codes = [];
  for (const block of blocks) {
    collectErrorCodes(block, codes);
  }
  return unique(codes);
}

function usageWebSearchRequests(json) {
  const usage = json?.usage;
  if (!usage || typeof usage !== 'object') return 0;

  const candidates = [
    usage.web_search_requests,
    usage.server_tool_use?.web_search_requests,
    usage.server_tool_use?.web_search?.requests
  ];

  for (const candidate of candidates) {
    const count = Number(candidate);
    if (Number.isFinite(count) && count > 0) return count;
  }
  return 0;
}

function textWebSearchRequests(text) {
  const match = text.match(/Claude Web Search called\s+(\d+)\s+times/i);
  if (!match) return 0;
  const count = Number(match[1]);
  return Number.isFinite(count) ? count : 0;
}

function hasUnavailableToolText(text) {
  return (
    /웹\s*검색\s*도구.*사용할\s*수\s*없/i.test(text) ||
    /web\s+search.*(?:unavailable|not available|cannot|can't|do not have|don't have|no access)/i.test(text) ||
    /(?:cannot|can't|do not have|don't have|no access).*web\s+search/i.test(text)
  );
}

function classify({ status, hasRecordError, codes, webSearchRequests, allText, hasCcSessionId }) {
  if ((typeof status === 'number' && status >= 400) || hasRecordError) return 'request_error';
  if (codes.includes('too_many_requests')) return 'tool_rate_limited';
  if (webSearchRequests > 0 || /Claude Web Search called 1 times/i.test(allText)) return 'web_search_used';
  if (hasCcSessionId || hasUnavailableToolText(allText)) return 'claude_code_session_suspected';
  if (status != null) return 'tool_not_used';
  return 'unknown';
}

function buildHints({ status, hasCcSessionId, requestId, upstreamRequestId, codes, webSearchRequests, allText }) {
  const hints = [];
  if (typeof status === 'number' && status >= 400) hints.push(`HTTP ${status} response`);
  if (hasCcSessionId) hints.push('Claude Code session header present');
  if (requestId) hints.push(`request id: ${requestId}`);
  if (upstreamRequestId) hints.push(`upstream request id: ${upstreamRequestId}`);
  if (codes.includes('too_many_requests')) hints.push('tool returned too_many_requests');
  if (webSearchRequests > 0) hints.push(`web search requests: ${webSearchRequests}`);
  if (hasUnavailableToolText(allText)) hints.push('response text says web search is unavailable');
  return hints;
}

export function summarizeExchange(record = {}) {
  const response = record.response ?? {};
  const headers = response.headers ?? {};
  const json = response.json;
  const status = normalizeStatus(response.status);
  const blocks = responseBlocks(json);
  const texts = [];

  for (const block of blocks) {
    appendBlockText(block, texts);
  }
  if (typeof response.bodyText === 'string') texts.push(response.bodyText);

  const allText = texts.join('\n');
  const hasCcSessionId = Boolean(headerValue(headers, 'x-cc-session-id'));
  const gatewayRequestId =
    headerValue(headers, 'x-oneapi-request-id') || headerValue(headers, 'x-newapi-request-id');
  const genericRequestId = headerValue(headers, 'x-request-id');
  const requestId = gatewayRequestId || genericRequestId;
  const upstreamRequestId =
    headerValue(headers, 'x-oneapi-upstream-request-id') ||
    headerValue(headers, 'x-upstream-request-id') ||
    headerValue(headers, 'anthropic-request-id') ||
    headerValue(headers, 'openai-request-id') ||
    (gatewayRequestId ? genericRequestId : '');
  const contentTypes = blocks.map((block) => block?.type).filter(Boolean);
  const codes = toolErrorCodes(blocks);
  const webSearchRequests = Math.max(
    usageWebSearchRequests(json),
    textWebSearchRequests(allText),
    blocks.filter((block) => block?.type === 'server_tool_use' && block?.name === 'web_search').length
  );
  const classification = classify({
    status,
    hasRecordError: Boolean(record.error),
    codes,
    webSearchRequests,
    allText,
    hasCcSessionId
  });

  return {
    hasCcSessionId,
    requestId,
    upstreamRequestId,
    contentTypes,
    toolErrorCodes: codes,
    webSearchRequests,
    classification,
    hints: buildHints({
      status,
      hasCcSessionId,
      requestId,
      upstreamRequestId,
      codes,
      webSearchRequests,
      allText
    })
  };
}
