'use strict';
// ── Shared popup system (terms / privacy / pricing) ──
// Used by /, /en, /guide, /en/guide.
const popupIsKo = document.documentElement.lang === 'ko';

const POPUP_STRINGS = {
  popupTerms:      popupIsKo ? '이용약관'                    : 'Terms of Service',
  popupPrivacy:    popupIsKo ? '개인정보처리방침'              : 'Privacy Policy',
  popupPricing:    popupIsKo ? '전체 모델 및 가격표'           : 'Full Model & Pricing Table',
  popupTermsUrl:   popupIsKo ? '/legal/terms.html'           : '/legal/terms_en.html',
  popupPrivacyUrl: popupIsKo ? '/legal/privacy.html'         : '/legal/privacy_en.html',
  pricingTitleFn: popupIsKo
    ? (d) => `전체 모델 및 가격표 <span class="popup-date">(${d} 기준)</span>`
    : (d) => `Full Model & Pricing <span class="popup-date">(as of ${d})</span>`,
  pricingNote: popupIsKo
    ? '가격은 프로모션 적용 금액이며, 시기 및 모델에 따라 달라질 수 있습니다.'
    : 'Prices reflect promotional rates and may vary over time and by model.',
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

const MODAL_FOCUSABLE_SELECTOR = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'iframe',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function createAccessibleModal({ overlay, initialFocus, onClose }) {
  if (!overlay) return null;

  const modal = overlay.querySelector('.popup-modal');
  const backgroundState = new Map();
  let trigger = null;

  // The shared popup is rendered inside the footer template. Moving overlays to
  // the body allows every other top-level region to become inert while open.
  if (overlay.parentElement !== document.body) {
    document.body.appendChild(overlay);
  }

  function getFocusableElements() {
    return Array.from(modal.querySelectorAll(MODAL_FOCUSABLE_SELECTOR)).filter(
      (element) => element.getClientRects().length > 0,
    );
  }

  function setBackgroundInert(isInert) {
    if (isInert) {
      backgroundState.clear();
      Array.from(document.body.children).forEach((element) => {
        if (element === overlay) return;
        backgroundState.set(element, element.inert);
        element.inert = true;
      });
      return;
    }

    backgroundState.forEach((wasInert, element) => {
      if (element.isConnected) element.inert = wasInert;
    });
    backgroundState.clear();
  }

  function focusInitialElement() {
    const target = initialFocus?.() || getFocusableElements()[0] || modal;
    target.focus({ preventScroll: true });
  }

  function open(opener = document.activeElement) {
    if (overlay.classList.contains('open')) return;
    trigger = opener instanceof HTMLElement ? opener : null;
    overlay.classList.add('open');
    overlay.setAttribute('aria-hidden', 'false');
    document.body.style.overflow = 'hidden';
    setBackgroundInert(true);
    requestAnimationFrame(focusInitialElement);
  }

  function close() {
    if (!overlay.classList.contains('open')) return;
    overlay.classList.remove('open');
    overlay.setAttribute('aria-hidden', 'true');
    document.body.style.overflow = '';
    setBackgroundInert(false);

    const returnTarget = trigger;
    trigger = null;
    if (returnTarget?.isConnected) {
      requestAnimationFrame(() => returnTarget.focus({ preventScroll: true }));
    }
    onClose?.();
  }

  overlay.addEventListener('click', (event) => {
    if (event.target === overlay) close();
  });

  overlay.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
      return;
    }
    if (event.key !== 'Tab') return;

    const focusable = getFocusableElements();
    if (focusable.length === 0) {
      event.preventDefault();
      modal.focus({ preventScroll: true });
      return;
    }

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (!focusable.includes(document.activeElement)) {
      event.preventDefault();
      (event.shiftKey ? last : first).focus();
      return;
    }
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  return { open, close };
}

window.createAccessibleModal = createAccessibleModal;

(function wireLanguageSwitch() {
  const switcher = document.querySelector('.lang-switch');
  const trigger = switcher?.querySelector('.lang-switch-trigger') || switcher;
  if (!switcher || !trigger) return;

  function setOpen(isOpen) {
    switcher.classList.toggle('open', isOpen);
    trigger.setAttribute('aria-expanded', String(isOpen));
  }

  trigger.addEventListener('click', () => {
    setOpen(!switcher.classList.contains('open'));
  });
  switcher.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !switcher.classList.contains('open')) return;
    event.preventDefault();
    setOpen(false);
    trigger.focus();
  });
  document.addEventListener('click', (event) => {
    if (!switcher.contains(event.target)) setOpen(false);
  });
})();

(function wireProfileMenu() {
  const menu = document.querySelector('.profile-menu');
  const trigger = menu?.querySelector('.profile-menu-trigger');
  if (!menu || !trigger) return;

  function setOpen(isOpen) {
    menu.classList.toggle('open', isOpen);
    trigger.setAttribute('aria-expanded', String(isOpen));
  }

  trigger.addEventListener('click', () => {
    setOpen(!menu.classList.contains('open'));
  });
  menu.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !menu.classList.contains('open')) return;
    event.preventDefault();
    setOpen(false);
    trigger.focus();
  });
  document.addEventListener('click', (event) => {
    if (!menu.contains(event.target)) setOpen(false);
  });

  const logoutBtn = menu.querySelector('.pd-logout');
  if (logoutBtn) {
    logoutBtn.addEventListener('click', async () => {
      logoutBtn.disabled = true;
      try {
        await fetch('/api/user/logout', { method: 'GET', credentials: 'same-origin' });
      } catch (e) {
        /* 로그아웃 요청 실패해도 새로고침으로 상태 재확인 */
      }
      window.location.reload();
    });
  }
})();

const popupOverlay = document.getElementById('popupOverlay');
function resetPopupContent() {
  popupOverlay.querySelector('.popup-modal').classList.remove('wide', 'pricing');
  document.getElementById('popupBody').replaceChildren();
}

const popupDialog = createAccessibleModal({
  overlay: popupOverlay,
  initialFocus: () => document.getElementById('popupTitle'),
  onClose: resetPopupContent,
});

function openPopup(title, key) {
  const overlay = popupOverlay;
  const modal   = overlay.querySelector('.popup-modal');
  const body    = document.getElementById('popupBody');
  const item    = POPUP_CONTENTS[key] || {};
  const popupTitle = document.getElementById('popupTitle');
  const popupNote = document.getElementById('popupNote');

  modal.classList.toggle('pricing', key === 'pricing');
  popupTitle.textContent = item.title || title;
  popupNote.textContent = '';
  popupNote.style.display = 'none';

  if (item.iframe) {
    modal.classList.add('wide');
    const iframe = document.createElement('iframe');
    iframe.className = 'popup-iframe';
    iframe.src = item.iframe;
    iframe.title = item.title || title;
    iframe.addEventListener('load', () => {
      iframe.contentDocument?.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') closePopup();
      });
    });
    body.replaceChildren(iframe);
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

  popupDialog.open();
}

async function renderPricingTable(body) {
  const today = new Date();
  const dateStr = `${today.getFullYear()}/${String(today.getMonth()+1).padStart(2,'0')}/${String(today.getDate()).padStart(2,'0')}`;
  document.getElementById('popupTitle').innerHTML = POPUP_STRINGS.pricingTitleFn(dateStr);
  const popupNote = document.getElementById('popupNote');
  popupNote.textContent = POPUP_STRINGS.pricingNote;
  popupNote.style.display = '';

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

    const sorted = json.data.map((model) => ({
      ...model,
      model_name: String(model.model_name ?? ''),
    })).sort((a, b) => {
      const pa = getProvider(a.model_name), pb = getProvider(b.model_name);
      if (pa !== pb) return pa.localeCompare(pb);
      const ta = getTier(a.model_name), tb = getTier(b.model_name);
      if (ta !== tb) return ta - tb;
      return a.model_name.localeCompare(b.model_name);
    });

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
          <tbody></tbody>
        </table>
      </div>
    `;

    const tableBody = body.querySelector('tbody');
    let lastProvider = '';
    sorted.forEach((m) => {
      const discount = (m.discount_percent || 0) / 100;
      const inputPrice  = (m.model_ratio * groupRatio * (1 - discount)).toFixed(4);
      const outputPrice = (m.model_ratio * m.completion_ratio * groupRatio * (1 - discount)).toFixed(4);
      const provider = getProvider(m.model_name);

      if (provider !== lastProvider) {
        const providerRow = document.createElement('tr');
        const providerCell = document.createElement('td');
        providerCell.colSpan = 3;
        providerCell.style.cssText = 'padding:12px 14px 4px;font-size:11px;font-weight:700;color:#0EA5E9;text-transform:uppercase;letter-spacing:0.08em;border-bottom:1px solid #374151;';
        providerCell.textContent = provider;
        providerRow.appendChild(providerCell);
        tableBody.appendChild(providerRow);
      }
      lastProvider = provider;

      const row = document.createElement('tr');
      const modelCell = document.createElement('td');
      modelCell.style.cssText = 'padding:10px 14px;color:#f9fafb;font-size:13px;border-bottom:1px solid #1f2937;';
      modelCell.textContent = m.model_name;

      const inputCell = document.createElement('td');
      inputCell.style.cssText = 'padding:10px 14px;color:#7dd3fc;font-size:13px;text-align:right;border-bottom:1px solid #1f2937;';
      inputCell.textContent = `$${inputPrice}`;

      const outputCell = document.createElement('td');
      outputCell.style.cssText = 'padding:10px 14px;color:#7dd3fc;font-size:13px;text-align:right;border-bottom:1px solid #1f2937;';
      outputCell.textContent = `$${outputPrice}`;

      row.append(modelCell, inputCell, outputCell);
      tableBody.appendChild(row);
    });
  } catch (e) {
    body.innerHTML = `<p style="color:#f87171;font-size:13px;text-align:center;padding:24px 0;">${POPUP_STRINGS.pricingError}</p>`;
  }
}

function closePopup() {
  popupDialog.close();
}

document.getElementById('popupClose').addEventListener('click', closePopup);
