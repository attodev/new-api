'use strict';
const isKo = document.documentElement.lang === 'ko';

const STRINGS = {
  sending:        isKo ? '전송 중...'                                                  : 'Sending...',
  sent:           isKo ? '문의가 접수되었습니다. 빠른 시일 내에 연락드리겠습니다.'         : "Your inquiry has been received. We'll be in touch shortly.",
  error:          isKo ? '전송에 실패했습니다. 잠시 후 다시 시도해 주세요.'               : 'Something went wrong. Please try again later.',
  submitDefault:  isKo ? '문의하기 →'                                                  : 'Submit →',
  scaleTabLabel:  isKo ? '도입 규모 탭 전환'                                            : 'Team Size tab switch',
  // calc strings
  calcLoadError:  isKo ? '모델 로드 실패'                                               : 'Failed to load models',
  calcInputRow:   isKo ? (m) => `입력 토큰 (${m}M)`                                    : (m) => `Input Tokens (${m}M)`,
  calcOutputRow:  isKo ? (m) => `출력 토큰 (${m}M)`                                    : (m) => `Output Tokens (${m}M)`,
  calcInputRate:  isKo ? '단가 입력'                                                    : 'Input rate',
  calcOutputRate: isKo ? '단가 출력'                                                    : 'Output rate',
  // video filenames
  videoStd:       isKo ? '/videos/main/alrouter_ko.mp4'                                 : '/videos/main/alrouter_en.mp4',
  videoLite:      isKo ? '/videos/main/alrouter_ko_lite.mp4'                            : '/videos/main/alrouter_en_lite.mp4',
};

// ── Contact form ──
(function () {
  const overlay = document.getElementById("contactOverlay");
  const form = document.getElementById("contactForm");
  const result = document.getElementById("contactResult");
  if (!overlay || !form || !result) return;

  const contactDialog = window.createAccessibleModal({
    overlay,
    initialFocus: () => form.querySelector('[name="org"]'),
  });

  window.openContact = (opener) => contactDialog.open(opener);

  function closeContact() {
    contactDialog.close();
  }
  document
    .getElementById("contactClose")
    .addEventListener("click", closeContact);
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

// ── Popup system (openPopup/closePopup/renderPricingTable) now lives in popup.js ──

// ── Video version check ──
(function () {
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
  // The template always starts on std. The requested initial variant is
  // committed below only after its poster has loaded and decoded.
  window._videoVer = "std";
  window._initialVideoVer = ver;

  function _updateDots(v) {
    const s = document.getElementById("dotStd");
    const l = document.getElementById("dotLite");
    if (s) s.classList.toggle("active", v === "std");
    if (l) l.classList.toggle("active", v === "lite");
  }
  _updateDots("std");
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
  const posterLayer = document.getElementById('animPosterLayer');
  const posterImages = {
    std: document.getElementById('animPosterStd'),
    lite: document.getElementById('animPosterLite'),
  };
  const playOverlay = document.getElementById('animPlayOverlay');
  const hoverOverlay= document.getElementById('animHoverOverlay');
  const pauseState  = document.getElementById('animPauseState');
  const resumeState = document.getElementById('animResumeState');
  const endControls = document.getElementById('animEndControls');
  const replayBtn   = document.getElementById('animReplayBtn');
  const animWrap    = document.getElementById('anim');
  let switchRequestId = 0;
  let _touchStartX = 0, _touchStartY = 0, _isSwiping = false;

  function renderPlaybackToggle(isPaused) {
    const unavailable = playOverlay.style.display !== 'none' || video.ended;
    hoverOverlay.hidden = unavailable;
    if (unavailable) return;

    pauseState.style.display = isPaused ? 'none' : 'flex';
    resumeState.style.display = isPaused ? 'flex' : 'none';
    hoverOverlay.classList.toggle('is-paused', isPaused);
    hoverOverlay.setAttribute(
      'aria-label',
      isPaused ? hoverOverlay.dataset.labelResume : hoverOverlay.dataset.labelPause,
    );
  }

  function syncPlaybackToggle() {
    renderPlaybackToggle(video.paused);
  }

  function playVideo() {
    // play 이벤트를 기다리는 동안 이전 일시정지 UI가 깜빡이지 않도록
    // 클릭 즉시 재생 중 상태를 먼저 표시한다.
    renderPlaybackToggle(false);
    if (video.readyState === HTMLMediaElement.HAVE_NOTHING) video.load();
    const playPromise = video.play();
    if (playPromise && typeof playPromise.catch === 'function') {
      playPromise.catch(syncPlaybackToggle);
    }
  }

  function loadAndDecodePoster(img) {
    const loaded = img.complete
      ? (img.naturalWidth > 0
          ? Promise.resolve()
          : Promise.reject(new Error('Poster failed to load')))
      : new Promise((resolve, reject) => {
          img.addEventListener('load', resolve, { once: true });
          img.addEventListener('error', () => reject(new Error('Poster failed to load')), { once: true });
        });

    return loaded.then(async () => {
      if (typeof img.decode === 'function') await img.decode();
      if (img.naturalWidth === 0) throw new Error('Poster failed to decode');
      return img;
    });
  }

  // Only the visible standard poster is requested at navigation time. The
  // alternate poster is attached on user intent or during browser idle time,
  // while the cached decode promise keeps switching atomic.
  const posterReady = {
    std: loadAndDecodePoster(posterImages.std),
  };
  posterReady.std.catch(() => {});

  function ensurePosterReady(ver) {
    if (posterReady[ver]) return posterReady[ver];

    const img = posterImages[ver];
    if (!img) return Promise.reject(new Error('Unknown poster version'));
    if (!img.getAttribute('src')) {
      if (img.dataset.srcset) img.srcset = img.dataset.srcset;
      if (img.dataset.sizes) img.sizes = img.dataset.sizes;
      img.src = img.dataset.src;
    }
    posterReady[ver] = loadAndDecodePoster(img);
    posterReady[ver].catch(() => {});
    return posterReady[ver];
  }

  playOverlay.addEventListener('click', () => {
    if (_isSwiping) return;
    // Playing the current video cancels a poster switch that is still loading.
    switchRequestId += 1;
    sessionStorage.setItem('videoVersion', window._videoVer);
    playOverlay.style.display = 'none';
    posterLayer.classList.add('is-hidden');
    video.currentTime = 0;
    playVideo();
  });

  hoverOverlay.addEventListener('click', () => {
    if (_isSwiping || video.ended) return;
    if (video.paused) {
      playVideo();
    } else {
      video.pause();
    }
  });

  video.addEventListener('play', syncPlaybackToggle);
  video.addEventListener('pause', syncPlaybackToggle);

  video.addEventListener('ended', () => {
    syncPlaybackToggle();
    endControls.style.display = 'flex';
    replayBtn.style.display = 'flex';
  });

  replayBtn.addEventListener('click', () => {
    endControls.style.display = 'none';
    replayBtn.style.display = 'none';
    video.currentTime = 0;
    playVideo();
  });

  // Mobile swipe to switch video (std ↔ lite)
  animWrap.addEventListener('touchstart', (e) => {
    const nextVer = window._videoVer === 'std' ? 'lite' : 'std';
    ensurePosterReady(nextVer).catch(() => {});
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

  function _doSwitch(newVer) {
    window._videoVer = newVer;
    sessionStorage.setItem('videoVersion', newVer);
    endControls.style.display = 'none';
    replayBtn.style.display = 'none';
    hoverOverlay.hidden = true;
    hoverOverlay.classList.remove('is-paused');
    playOverlay.style.display = '';
    Object.entries(posterImages).forEach(([ver, img]) => {
      img.classList.toggle('active', ver === newVer);
    });
    posterLayer.classList.remove('is-hidden');
    video.pause();
    video.src = newVer === 'lite' ? STRINGS.videoLite : STRINGS.videoStd;
    if (window._updateDots) window._updateDots(newVer);
  }

  window.switchVideo = async function(newVer, swipeDir) {
    if (!posterImages[newVer]) return;

    // Increment before the same-version check so selecting the current dot can
    // cancel an older pending switch to the other version.
    const requestId = ++switchRequestId;
    if (window._videoVer === newVer) return;

    try {
      await ensurePosterReady(newVer);
    } catch (error) {
      console.warn('Video poster could not be prepared; keeping the current poster.', error);
      return;
    }
    if (requestId !== switchRequestId) return;

    if (swipeDir) {
      // 스와이프: 기존 슬라이드 애니메이션
      _doSwitch(newVer);
      animWrap.classList.remove('swipe-left', 'swipe-right');
      void animWrap.offsetWidth;
      animWrap.classList.add('swipe-' + swipeDir);
      animWrap.addEventListener('animationend', () => {
        animWrap.classList.remove('swipe-left', 'swipe-right');
      }, { once: true });
    } else {
      // 도트 클릭: std→lite 는 왼쪽, lite→std 는 오른쪽
      const dir = newVer === 'lite' ? 'left' : 'right';
      animWrap.classList.remove('swipe-left', 'swipe-right');
      void animWrap.offsetWidth;
      _doSwitch(newVer);
      animWrap.classList.add('swipe-' + dir);
      animWrap.addEventListener('animationend', () => {
        animWrap.classList.remove('swipe-left', 'swipe-right');
      }, { once: true });
    }
  };

  Object.entries({ std: document.getElementById('dotStd'), lite: document.getElementById('dotLite') })
    .forEach(([ver, dot]) => {
      if (!dot) return;
      ['pointerenter', 'focus', 'touchstart'].forEach((eventName) => {
        dot.addEventListener(eventName, () => ensurePosterReady(ver).catch(() => {}), { passive: true });
      });
    });

  const warmAlternatePoster = () => {
    const initialVer = window._initialVideoVer;
    if (initialVer !== window._videoVer) {
      window.switchVideo(initialVer);
      return;
    }
    const alternateVer = initialVer === 'std' ? 'lite' : 'std';
    ensurePosterReady(alternateVer).catch(() => {});
  };
  if (typeof window.requestIdleCallback === 'function') {
    window.requestIdleCallback(warmAlternatePoster, { timeout: 2000 });
  } else {
    window.setTimeout(warmAlternatePoster, 1200);
  }

  document.addEventListener('visibilitychange', () => {
    if (document.hidden && !video.paused) video.pause();
  });

  syncPlaybackToggle();
})();

// ── Calculator pricing ──
// TODO: 모델 목록 및 가격은 추후 API에서 가져올 예정
let _calcPricing = Object.create(null); // model_name → { input, output }
let _calcGroupRatio = 1;

async function loadCalcModels() {
  const sel = document.getElementById("calcModel");
  try {
    const res = await fetch("/api/pricing");
    const json = await res.json();
    _calcGroupRatio = 2;
    const models = json.data.map((model) => ({
      ...model,
      model_name: String(model.model_name ?? ""),
    }));
    models.forEach((m) => {
      const d = (m.discount_percent || 0) / 100;
      _calcPricing[m.model_name] = {
        input: m.model_ratio * _calcGroupRatio * (1 - d),
        output: m.model_ratio * m.completion_ratio * _calcGroupRatio * (1 - d),
      };
    });
    const options = models.map((m) => {
      const option = document.createElement("option");
      option.value = m.model_name;
      option.textContent = m.model_name;
      return option;
    });
    sel.replaceChildren(...options);
  } catch (e) {
    const errorOption = document.createElement("option");
    errorOption.value = "";
    errorOption.textContent = STRINGS.calcLoadError;
    sel.replaceChildren(errorOption);
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
  // 아주 큰 토큰 수를 입력해도 결과 박스를 벗어나지 않도록, 일정 금액 이상이면 축약 표기($1.2B)로 전환
  const fmtMoney = (v, decimals) =>
    "$" +
    (Math.abs(v) >= 1e6
      ? new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 2 }).format(v)
      : v.toFixed(decimals));
  const fmt = (v) => fmtMoney(v, 4);
  document.getElementById("calcHint").style.display = "none";
  document.getElementById("calcLabel").style.display = "";
  const priceEl = document.getElementById("calcPrice");
  priceEl.style.display = "";
  priceEl.textContent = fmtMoney(total, 2);
  document.getElementById("calcBreakdown").innerHTML =
    `<div class="calc-breakdown-row"><span>${STRINGS.calcInputRow(inputM)}</span><span>${fmt(inputCost)}</span></div>` +
    `<div class="calc-breakdown-row"><span>${STRINGS.calcOutputRow(outputM)}</span><span>${fmt(outputCost)}</span></div>` +
    `<div class="calc-breakdown-row" style="display:none;margin-top:4px;padding-top:4px;border-top:1px solid #374151;color:#9ca3af"><span>${STRINGS.calcInputRate}</span><span>$${prices.input.toFixed(4)}/M</span></div>` +
    `<div class="calc-breakdown-row" style="display:none;color:#9ca3af"><span>${STRINGS.calcOutputRate}</span><span>$${prices.output.toFixed(4)}/M</span></div>`;
}

// ── Lucide icon init ──
if (window.lucide && typeof window.lucide.createIcons === 'function') {
  window.lucide.createIcons();
}

// ── Scroll reveal ──
(function () {
  const SELECTORS = [
    // 섹션 헤더 (tag + h2 + sub 묶음을 wrapper로)
    '#about .section-tag', '#about h2', '#about .section-sub',
    '.solution-inner .section-tag', '.solution-inner h2', '.solution-inner .section-sub',
    '.feature-section .section-tag', '.feature-section h2', '.feature-section > .feature-grid > *',
    '.integration-inner .section-tag', '.integration-inner h2', '.integration-inner .section-sub',
    '.integration-inner .provider-logos', '.integration-inner .modality-tags',
    '.pricing-inner .section-tag', '.pricing-inner .pricing-title', '.pricing-inner .section-sub',
    '.pricing-inner .price-cards-outer', '.pricing-inner .pricing-cta',
    '.problem-card',
    '.gov-point',
    '.cta-box',
  ];

  const els = SELECTORS.flatMap(sel => [...document.querySelectorAll(sel)]);
  // 중복 제거
  const unique = [...new Set(els)];

  unique.forEach(el => el.classList.add('reveal'));

  const io = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        entry.target.classList.add('visible');
        io.unobserve(entry.target);
      }
    });
  }, { threshold: 0.12 });

  unique.forEach(el => io.observe(el));
})();

// ── GNB scroll shadow ──
(function () {
  const gnb = document.querySelector('.gnb');
  if (!gnb) return;
  window.addEventListener('scroll', () => {
    gnb.classList.toggle('gnb--scrolled', window.scrollY > 50);
  }, { passive: true });
})();

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
