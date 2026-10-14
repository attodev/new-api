const DIAGNOSTIC_PROMPT =
  '웹 검색 도구를 사용해 2026년 7월 2일 한국 주요 뉴스 1개를 출처 URL과 함께 한 문장으로 답해줘.';

function messagesBody() {
  return {
    model: 'claude-sonnet-4-20250514',
    max_tokens: 512,
    tools: [
      {
        type: 'web_search_20250305',
        name: 'web_search'
      }
    ],
    messages: [
      {
        role: 'user',
        content: DIAGNOSTIC_PROMPT
      }
    ]
  };
}

function chatBody() {
  return {
    model: 'claude-sonnet-4-20250514',
    messages: [
      {
        role: 'user',
        content: DIAGNOSTIC_PROMPT
      }
    ],
    web_search_options: {
      search_context_size: 'low'
    }
  };
}

export function buildPresets(env = {}) {
  return [
    {
      id: 'direct-messages-web-search',
      label: 'Direct Messages Web Search',
      target: 'unode',
      baseUrl: env.UNODE_BASE_URL ?? '',
      method: 'POST',
      path: '/v1/messages',
      headers: {
        'content-type': 'application/json',
        'anthropic-version': '2023-06-01',
        'x-api-key': '${UNODE_RELAY_API_KEY}'
      },
      body: messagesBody()
    },
    {
      id: 'direct-chat-web-search-options',
      label: 'Direct Chat Web Search Options',
      target: 'unode',
      baseUrl: env.UNODE_BASE_URL ?? '',
      method: 'POST',
      path: '/v1/chat/completions',
      headers: {
        'content-type': 'application/json',
        authorization: 'Bearer ${UNODE_RELAY_API_KEY}'
      },
      body: chatBody()
    },
    {
      id: 'alrouter-messages-forced-channel',
      label: 'ALRouter Messages Forced Channel',
      target: 'alrouter',
      baseUrl: env.NEW_API_BASE_URL ?? '',
      method: 'POST',
      path: '/v1/messages',
      headers: {
        'content-type': 'application/json',
        'anthropic-version': '2023-06-01',
        'x-api-key': '${NEW_API_RELAY_API_KEY}-${NEW_API_CHANNEL_ID}'
      },
      body: messagesBody()
    },
    {
      id: 'alrouter-chat-web-search-options',
      label: 'ALRouter Chat Web Search Options',
      target: 'alrouter',
      baseUrl: env.NEW_API_BASE_URL ?? '',
      method: 'POST',
      path: '/v1/chat/completions',
      headers: {
        'content-type': 'application/json',
        authorization: 'Bearer ${NEW_API_RELAY_API_KEY}-${NEW_API_CHANNEL_ID}'
      },
      body: chatBody()
    }
  ];
}
