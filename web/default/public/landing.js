'use strict';
const isKo = document.documentElement.lang === 'ko';

const STRINGS = {
  sending:        isKo ? '전송 중...'                                                  : 'Sending...',
  sent:           isKo ? '문의가 접수되었습니다. 빠른 시일 내에 연락드리겠습니다.'         : "Your inquiry has been received. We'll be in touch shortly.",
  error:          isKo ? '전송에 실패했습니다. 잠시 후 다시 시도해 주세요.'               : 'Something went wrong. Please try again later.',
  submitDefault:  isKo ? '문의하기 →'                                                  : 'Submit →',
  scaleTabLabel:  isKo ? '도입 규모 탭 전환'                                            : 'Team Size tab switch',
  popupTerms:     isKo ? '이용약관'                                                     : 'Terms of Service',
  popupPrivacy:   isKo ? '개인정보처리방침'                                              : 'Privacy Policy',
  popupPricing:   isKo ? '전체 모델 및 가격표'                                           : 'Full Model & Pricing Table',
  popupTermsUrl:  isKo ? './terms.html'                                                 : './terms_en.html',
  popupPrivacyUrl:isKo ? './privacy.html'                                               : './privacy_en.html',
  // renderPricingTable strings
  pricingTitleFn: isKo
    ? (d) => `전체 모델 및 가격표(${d} 기준, <span style="color:#fbbf24;">단가는 현재 시점 기준이며 추후 변동될 수 있음</span>)`
    : (d) => `Full Model & Pricing (as of ${d}, <span style="color:#fbbf24;">prices are current and subject to change</span>)`,
  pricingLoading: isKo ? '불러오는 중…'                                                 : 'Loading…',
  pricingError:   isKo ? '가격 정보를 불러오지 못했습니다.'                               : 'Failed to load pricing data.',
  tableModel:     isKo ? '모델명'                                                        : 'Model',
  tableInput:     isKo ? '입력 단가 ($/M)'                                              : 'Input ($/M)',
  tableOutput:    isKo ? '출력 단가 ($/M)'                                              : 'Output ($/M)',
  // calc strings
  calcLoadError:  isKo ? '모델 로드 실패'                                               : 'Failed to load models',
  calcInputRow:   isKo ? (m) => `입력 토큰 (${m}M)`                                    : (m) => `Input Tokens (${m}M)`,
  calcOutputRow:  isKo ? (m) => `출력 토큰 (${m}M)`                                    : (m) => `Output Tokens (${m}M)`,
  calcInputRate:  isKo ? '단가 입력'                                                    : 'Input rate',
  calcOutputRate: isKo ? '단가 출력'                                                    : 'Output rate',
  // video filenames
  videoStd:       isKo ? 'alrouter_ko.mp4'                                              : 'alrouter_en.mp4',
  videoLite:      isKo ? 'alrouter_ko_lite.mp4'                                         : 'alrouter_en_lite.mp4',
  posterStd:      isKo ? './alrouter_ko_poster.png'                                     : './alrouter_en_poster.png',
  posterLite:     isKo ? './alrouter_ko_lite_poster.png'                                : './alrouter_en_lite_poster.png',
  ctaMap:         isKo ? { 'alrouter_ko.mp4': 18.1, 'alrouter_ko_lite.mp4': 19.5 }
                       : { 'alrouter_en.mp4': 17.3, 'alrouter_en_lite.mp4': 21.6 },
};

// ── Contact form ──
(function () {
  const overlay = document.getElementById("contactOverlay");
  const form = document.getElementById("contactForm");
  const result = document.getElementById("contactResult");

  function closeContact() {
    overlay.classList.remove("open");
  }
  document
    .getElementById("contactClose")
    .addEventListener("click", closeContact);
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) closeContact();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeContact();
  });
  new MutationObserver(() => {
    document.body.style.overflow = overlay.classList.contains("open")
      ? "hidden"
      : "";
  }).observe(overlay, { attributes: true, attributeFilter: ["class"] });

  // 도입 규모 탭 전환
  document
    .querySelectorAll('input[name="scale_type"]')
    .forEach((radio) => {
      radio.addEventListener("change", () => {
        const isToken = radio.value === "token";
        document.getElementById("scaleHeadField").style.display = isToken
          ? "none"
          : "";
        document.getElementById("scaleTokenField").style.display = isToken
          ? ""
          : "none";
        document.querySelector('[name="headcount"]').required = !isToken;
      });
    });

  form.addEventListener("submit", async function (e) {
    e.preventDefault();
    const btn = form.querySelector(".cf-submit");
    btn.disabled = true;
    btn.textContent = STRINGS.sending;
    result.style.display = "none";

    const fd = new FormData(form);
    const data = Object.fromEntries(fd);
    data.features = fd.getAll("features");
    // 토큰 방식일 때 headcount 제거, 인원수 방식일 때 token 필드 제거
    if (data.scale_type === "token") {
      delete data.headcount;
    } else {
      delete data.input_tokens;
      delete data.output_tokens;
    }

    try {
      const res = await fetch("/api/contact", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(data),
      });
      if (!res.ok) throw new Error();
      result.className = "cf-result cf-success";
      result.textContent = STRINGS.sent;
      result.style.display = "block";
      form.reset();
    } catch {
      result.className = "cf-result cf-error";
      result.textContent = STRINGS.error;
      result.style.display = "block";
    } finally {
      btn.disabled = false;
      btn.textContent = STRINGS.submitDefault;
    }
  });
})();

// ── Popup system ──
const POPUP_CONTENTS = {
  terms:   { title: STRINGS.popupTerms,    iframe: STRINGS.popupTermsUrl },
  privacy: { title: STRINGS.popupPrivacy,  iframe: STRINGS.popupPrivacyUrl },
  pricing: { title: STRINGS.popupPricing,  wide: true, dynamic: true },
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
  document.getElementById('popupTitle').innerHTML = STRINGS.pricingTitleFn(dateStr);

  body.innerHTML = `<p style="color:#6b7280;font-size:13px;text-align:center;padding:24px 0;">${STRINGS.pricingLoading}</p>`;

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
              <th style="padding:10px 14px;color:#9ca3af;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;text-align:left;border-bottom:1px solid #374151;">${STRINGS.tableModel}</th>
              <th style="padding:10px 14px;color:#9ca3af;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;text-align:right;border-bottom:1px solid #374151;">${STRINGS.tableInput}</th>
              <th style="padding:10px 14px;color:#9ca3af;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.06em;text-align:right;border-bottom:1px solid #374151;">${STRINGS.tableOutput}</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
      `;
  } catch (e) {
    body.innerHTML = `<p style="color:#f87171;font-size:13px;text-align:center;padding:24px 0;">${STRINGS.pricingError}</p>`;
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

// ── Video version check ──
(function () {
  const video = document.getElementById("animVideo");
  const prev = sessionStorage.getItem("videoVersion");
  const ver =
    prev === "std"
      ? "lite"
      : prev === "lite"
        ? "std"
        : Math.random() < 0.5
          ? "std"
          : "lite";
  sessionStorage.setItem("videoVersion", ver);
  window._videoVer = ver;
  video.src = ver === "lite" ? STRINGS.videoLite : STRINGS.videoStd;
  video.load();

  function _updateDots(v) {
    const s = document.getElementById("dotStd");
    const l = document.getElementById("dotLite");
    if (s) s.classList.toggle("active", v === "std");
    if (l) l.classList.toggle("active", v === "lite");
  }
  _updateDots(ver);
  window._updateDots = _updateDots;

  // 언어 전환 링크에 스크롤 위치 저장
  document.querySelectorAll(".lang-dropdown a").forEach((a) => {
    a.addEventListener("click", () => {
      sessionStorage.setItem("scrollY", window.scrollY);
    });
  });

  // 복원
  const savedY = sessionStorage.getItem("scrollY");
  if (savedY) {
    sessionStorage.removeItem("scrollY");
    window.addEventListener("load", () =>
      window.scrollTo(0, parseInt(savedY, 10)),
    );
  }
})();

// ── Video play/pause controls ──
(function() {
  const video       = document.getElementById('animVideo');
  const playOverlay = document.getElementById('animPlayOverlay');
  const hoverOverlay= document.getElementById('animHoverOverlay');
  const pauseState  = document.getElementById('animPauseState');
  const resumeState = document.getElementById('animResumeState');
  const ctaWrap     = document.getElementById('animCtaWrap');
  const ctaBtn      = document.getElementById('animCtaBtn');
  const replayBtn   = document.getElementById('animReplayBtn');
  const animWrap    = document.getElementById('anim');
  const CTA_MAP = STRINGS.ctaMap;
  let CTA_TIME = CTA_MAP[video.src.split('/').pop()] ?? 18.1;

  playOverlay.addEventListener('click', () => {
    playOverlay.style.display = 'none';
    video.currentTime = 0;
    video.play();
  });

  video.addEventListener('timeupdate', () => {
    if (video.currentTime >= CTA_TIME && ctaWrap.style.display === 'none') {
      ctaWrap.style.display = 'flex';
      setTimeout(() => ctaBtn.classList.add('active'), 50);
    }
  });

  video.addEventListener('ended', () => {
    hoverOverlay.style.display = 'none';
    replayBtn.style.display = 'flex';
  });

  replayBtn.addEventListener('click', () => {
    ctaBtn.classList.remove('active');
    ctaWrap.style.display = 'none';
    replayBtn.style.display = 'none';
    video.currentTime = 0;
    video.play();
  });

  const isTouch = () => window.matchMedia('(hover: none) and (pointer: coarse)').matches;

  animWrap.addEventListener('mouseenter', () => {
    if (isTouch()) return;
    if (playOverlay.style.display !== 'none') return;
    if (video.ended) return;
    pauseState.style.display  = video.paused ? 'none' : 'flex';
    resumeState.style.display = video.paused ? 'flex' : 'none';
    hoverOverlay.style.display = 'flex';
  });
  animWrap.addEventListener('mouseleave', () => {
    if (isTouch()) return;
    hoverOverlay.style.display = 'none';
  });

  // Mobile: tap to show controls, auto-hide after 2.5s
  let _hideControlsTimer = null;
  animWrap.addEventListener('click', () => {
    if (!isTouch()) return;
    if (_isSwiping) return;
    if (playOverlay.style.display !== 'none') return; // play overlay handles this tap
    if (video.ended) return;
    clearTimeout(_hideControlsTimer);
    if (hoverOverlay.style.display === 'flex') {
      hoverOverlay.style.display = 'none';
      return;
    }
    pauseState.style.display  = video.paused ? 'none' : 'flex';
    resumeState.style.display = video.paused ? 'flex' : 'none';
    hoverOverlay.style.display = 'flex';
    _hideControlsTimer = setTimeout(() => {
      hoverOverlay.style.display = 'none';
    }, 2500);
  });

  // Mobile swipe to switch video (std ↔ lite)
  let _touchStartX = 0, _touchStartY = 0, _isSwiping = false;
  animWrap.addEventListener('touchstart', (e) => {
    _touchStartX = e.touches[0].clientX;
    _touchStartY = e.touches[0].clientY;
    _isSwiping = false;
  }, { passive: true });
  animWrap.addEventListener('touchmove', (e) => {
    const dx = Math.abs(e.touches[0].clientX - _touchStartX);
    const dy = Math.abs(e.touches[0].clientY - _touchStartY);
    if (dx > dy && dx > 8) {
      _isSwiping = true;
      e.preventDefault(); // prevent iOS from hijacking as vertical scroll
    }
  }, { passive: false });
  animWrap.addEventListener('touchend', (e) => {
    if (!_isSwiping) return;
    const dx = e.changedTouches[0].clientX - _touchStartX;
    if (Math.abs(dx) < 30) return;
    const swipeDir = dx < 0 ? 'left' : 'right';
    const next = swipeDir === 'left'
      ? (window._videoVer === 'std' ? 'lite' : 'std')
      : (window._videoVer === 'lite' ? 'std' : 'lite');
    window.switchVideo(next, swipeDir);
  }, { passive: true });

  window.switchVideo = function(newVer, swipeDir) {
    if (window._videoVer === newVer) return;
    window._videoVer = newVer;
    sessionStorage.setItem('videoVersion', newVer);
    ctaBtn.classList.remove('active');
    ctaWrap.style.display = 'none';
    replayBtn.style.display = 'none';
    hoverOverlay.style.display = 'none';
    playOverlay.style.display = '';
    video.pause();
    video.src = newVer === 'lite' ? STRINGS.videoLite : STRINGS.videoStd;
    video.poster = newVer === 'lite' ? STRINGS.posterLite : STRINGS.posterStd;
    video.load();
    CTA_TIME = CTA_MAP[video.src.split('/').pop()] ?? 18.1;
    if (window._updateDots) window._updateDots(newVer);
    if (swipeDir) {
      animWrap.classList.remove('swipe-left', 'swipe-right');
      void animWrap.offsetWidth; // reflow to restart animation
      animWrap.classList.add('swipe-' + swipeDir);
      animWrap.addEventListener('animationend', () => {
        animWrap.classList.remove('swipe-left', 'swipe-right');
      }, { once: true });
    }
  };

  pauseState.addEventListener('click', () => {
    video.pause();
    pauseState.style.display  = 'none';
    resumeState.style.display = 'flex';
  });

  resumeState.addEventListener('click', () => {
    video.play();
    hoverOverlay.style.display = 'none';
  });
})();

// ── Calculator pricing ──
// TODO: 모델 목록 및 가격은 추후 API에서 가져올 예정
let _calcPricing = {}; // model_name → { input, output }
let _calcGroupRatio = 1;

async function loadCalcModels() {
  const sel = document.getElementById("calcModel");
  try {
    const res = await fetch("/api/pricing");
    const json = await res.json();
    _calcGroupRatio = 2;
    json.data.forEach((m) => {
      const d = (m.discount_percent || 0) / 100;
      _calcPricing[m.model_name] = {
        input: m.model_ratio * _calcGroupRatio * (1 - d),
        output: m.model_ratio * m.completion_ratio * _calcGroupRatio * (1 - d),
      };
    });
    sel.innerHTML = json.data
      .map((m) => `<option value="${m.model_name}">${m.model_name}</option>`)
      .join("");
  } catch (e) {
    sel.innerHTML = `<option value="">${STRINGS.calcLoadError}</option>`;
  }
}
loadCalcModels();

function runCalc() {
  const model = document.getElementById("calcModel").value;
  const inputM =
    parseFloat(document.getElementById("calcInputTokens").value) || 0;
  const outputM =
    parseFloat(document.getElementById("calcOutputTokens").value) || 0;
  const prices = _calcPricing[model] || { input: 0, output: 0 };
  const inputCost = inputM * prices.input;
  const outputCost = outputM * prices.output;
  const total = inputCost + outputCost;
  const fmt = (v) => "$" + v.toFixed(4);
  document.getElementById("calcHint").style.display = "none";
  document.getElementById("calcLabel").style.display = "";
  const priceEl = document.getElementById("calcPrice");
  priceEl.style.display = "";
  priceEl.textContent = "$" + total.toFixed(2);
  document.getElementById("calcBreakdown").innerHTML =
    `<div class="calc-breakdown-row"><span>${STRINGS.calcInputRow(inputM)}</span><span>${fmt(inputCost)}</span></div>` +
    `<div class="calc-breakdown-row"><span>${STRINGS.calcOutputRow(outputM)}</span><span>${fmt(outputCost)}</span></div>` +
    `<div class="calc-breakdown-row" style="display:none;margin-top:4px;padding-top:4px;border-top:1px solid #374151;color:#9ca3af"><span>${STRINGS.calcInputRate}</span><span>$${prices.input.toFixed(4)}/M</span></div>` +
    `<div class="calc-breakdown-row" style="display:none;color:#9ca3af"><span>${STRINGS.calcOutputRate}</span><span>$${prices.output.toFixed(4)}/M</span></div>`;
}

// ── Lucide icon init + lang-switch click handler ──
lucide.createIcons();
document.addEventListener("click", function (e) {
  document.querySelectorAll(".lang-switch.open").forEach(function (el) {
    if (!el.contains(e.target)) el.classList.remove("open");
  });
});

// ── Hamburger menu ──
(function () {
  const hamburger = document.querySelector('.gnb-hamburger');
  const gnbMenu   = document.querySelector('.gnb-menu');
  if (!hamburger || !gnbMenu) return;
  hamburger.addEventListener('click', function () {
    const isOpen = gnbMenu.classList.toggle('open');
    hamburger.classList.toggle('open', isOpen);
    hamburger.setAttribute('aria-expanded', String(isOpen));
  });
  // 메뉴 외부 클릭 시 닫기
  document.addEventListener('click', function (e) {
    if (!e.target.closest('.gnb')) {
      gnbMenu.classList.remove('open');
      hamburger.classList.remove('open');
      hamburger.setAttribute('aria-expanded', 'false');
    }
  });
})();
