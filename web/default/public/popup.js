'use strict';
// ── Shared popup system (terms / privacy / pricing) ──
// Used by /, /en, /guide, /en/guide.
const popupIsKo = document.documentElement.lang === 'ko';

const POPUP_STRINGS = {
  popupTerms:      popupIsKo ? '이용약관'                    : 'Terms of Service',
  popupPrivacy:    popupIsKo ? '개인정보처리방침'              : 'Privacy Policy',
  popupPricing:    popupIsKo ? '전체 모델 및 가격표'           : 'Full Model & Pricing Table',
  popupTermsUrl:   popupIsKo ? './legal/terms.html'          : './legal/terms_en.html',
  popupPrivacyUrl: popupIsKo ? './legal/privacy.html'        : './legal/privacy_en.html',
  pricingTitleFn: popupIsKo
    ? (d) => `전체 모델 및 가격표(${d} 기준, <span style="color:#fbbf24;">가격은 프로모션 적용 금액이며, 시기 및 모델에 따라 달라질 수 있습니다.</span>)`
    : (d) => `Full Model & Pricing (as of ${d}, <span style="color:#fbbf24;">Prices reflect promotional rates and may vary over time and by model.</span>)`,
  pricingLoading: popupIsKo ? '불러오는 중…'                 : 'Loading…',
  pricingError:   popupIsKo ? '가격 정보를 불러오지 못했습니다.' : 'Failed to load pricing data.',
  tableModel:     popupIsKo ? '모델명'                        : 'Model',
  tableInput:     popupIsKo ? '입력 단가 ($/M)'               : 'Input ($/M)',
  tableOutput:    popupIsKo ? '출력 단가 ($/M)'               : 'Output ($/M)',
};

const POPUP_CONTENTS = {
  terms:   { title: POPUP_STRINGS.popupTerms,   iframe: POPUP_STRINGS.popupTermsUrl },
  privacy: { title: POPUP_STRINGS.popupPrivacy, iframe: POPUP_STRINGS.popupPrivacyUrl },
  pricing: { title: POPUP_STRINGS.popupPricing, wide: true, dynamic: true },
};

function openPopup(title, key) {
  const overlay = document.getElementById('popupOverlay');
  const modal   = overlay.querySelector('.popup-modal');
  const body    = document.getElementById('popupBody');
  const item    = POPUP_CONTENTS[key] || {};

  document.getElementById('popupTitle').textContent = item.title || title;

  if (item.iframe) {
    modal.classList.add('wide');
    body.innerHTML = '<iframe class="popup-iframe" src="' + item.iframe + '"></iframe>';
  } else if (item.dynamic && key === 'pricing') {
    modal.classList.add('wide');
    renderPricingTable(body);
  } else if (item.wide) {
    modal.classList.add('wide');
    body.innerHTML = item.body || '';
  } else {
    modal.classList.remove('wide');
    body.innerHTML = item.body || '';
  }

  overlay.classList.add('open');
  document.body.style.overflow = 'hidden';
}

async function renderPricingTable(body) {
  const today = new Date();
  const dateStr = `${today.getFullYear()}/${String(today.getMonth()+1).padStart(2,'0')}/${String(today.getDate()).padStart(2,'0')}`;
  document.getElementById('popupTitle').innerHTML = POPUP_STRINGS.pricingTitleFn(dateStr);

  body.innerHTML = `<p style="color:#6b7280;font-size:13px;text-align:center;padding:24px 0;">${POPUP_STRINGS.pricingLoading}</p>`;

  try {
    const res  = await fetch('/api/pricing');
    const json = await res.json();
    const groupRatio = 2;

    function getProvider(name) {
      if (name.startsWith('claude'))  return 'Anthropic';
      if (name.startsWith('gemini'))  return 'Google';
      if (name.startsWith('gpt') || name.startsWith('o1') || name.startsWith('o3') || name.startsWith('o4')) return 'OpenAI';
      return 'Other';
    }

    function getTier(name) {
      if (name.includes('haiku') || name.includes('flash-lite')) return 1;
      if (name.includes('sonnet') || (name.includes('flash') && !name.includes('lite'))) return 2;
      if (name.includes('opus') || name.includes('pro')) return 3;
      return 2;
    }

    const sorted = [...json.data].sort((a, b) => {
      const pa = getProvider(a.model_name), pb = getProvider(b.model_name);
      if (pa !== pb) return pa.localeCompare(pb);
      const ta = getTier(a.model_name), tb = getTier(b.model_name);
      if (ta !== tb) return ta - tb;
      return a.model_name.localeCompare(b.model_name);
    });

    let lastProvider = '';
    const rows = sorted.map(m => {
      const discount = (m.discount_percent || 0) / 100;
      const inputPrice  = (m.model_ratio * groupRatio * (1 - discount)).toFixed(4);
      const outputPrice = (m.model_ratio * m.completion_ratio * groupRatio * (1 - discount)).toFixed(4);
      const provider = getProvider(m.model_name);
      const providerRow = provider !== lastProvider
        ? `<tr><td colspan="3" style="padding:12px 14px 4px;font-size:11px;font-weight:700;color:#0EA5E9;text-transform:uppercase;letter-spacing:0.08em;border-bottom:1px solid #374151;">${provider}</td></tr>`
        : '';
      lastProvider = provider;
      return providerRow + `
        <tr>
          <td style="padding:10px 14px;color:#f9fafb;font-size:13px;border-bottom:1px solid #1f2937;">${m.model_name}</td>
          <td style="padding:10px 14px;color:#7dd3fc;font-size:13px;text-align:right;border-bottom:1px solid #1f2937;">$${inputPrice}</td>
          <td style="padding:10px 14px;color:#7dd3fc;font-size:13px;text-align:right;border-bottom:1px solid #1f2937;">$${outputPrice}</td>
        </tr>`;
    }).join('');

    body.innerHTML = `
      <div style="padding:20px 24px 0;">
        <table style="width:100%;border-collapse:collapse;">
          <thead style="position:sticky;top:0;z-index:1;">
            <tr style="background:#1f2937;">
              <th style="padding:10px 14px;color:#9ca3af;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;text-align:left;border-bottom:1px solid #374151;">${POPUP_STRINGS.tableModel}</th>
              <th style="padding:10px 14px;color:#9ca3af;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;text-align:right;border-bottom:1px solid #374151;">${POPUP_STRINGS.tableInput}</th>
              <th style="padding:10px 14px;color:#9ca3af;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;text-align:right;border-bottom:1px solid #374151;">${POPUP_STRINGS.tableOutput}</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
      `;
  } catch (e) {
    body.innerHTML = `<p style="color:#f87171;font-size:13px;text-align:center;padding:24px 0;">${POPUP_STRINGS.pricingError}</p>`;
  }
}

function closePopup() {
  const overlay = document.getElementById('popupOverlay');
  overlay.classList.remove('open');
  overlay.querySelector('.popup-modal').classList.remove('wide');
  document.getElementById('popupBody').innerHTML = '';
  document.body.style.overflow = '';
}

document.getElementById('popupClose').addEventListener('click', closePopup);
document.getElementById('popupOverlay').addEventListener('click', function(e) {
  if (e.target === this) closePopup();
});
document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') closePopup();
});
