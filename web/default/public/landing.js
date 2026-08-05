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
  calcUnit:       isKo ? (v) => `단가: $${v} / 1M`                                     : (v) => `$${v} / 1M`,
  calcEmpty:      isKo ? '모델과 토큰을 입력하고<br>‘+ 목록에 추가하기’를 눌러주세요.'      : 'Select a model and enter tokens,<br>then click ‘+ Add to list’.',
  calcRemove:     isKo ? '삭제'                                                         : 'Remove',
  calcNeedTokens: isKo ? '입력·출력 토큰을 입력해 주세요.'                                : 'Enter input/output tokens first.',
  calcItemDiscount: isKo ? (pct) => ` · ${pct}% 할인`                                   : (pct) => ` · ${pct}% off`,
  calcTotalList:     isKo ? (list) => `정가 ${list}`                                     : (list) => `List price ${list}`,
  calcTotalDiscount: isKo ? (list, save) => `<span class="ctd-gray">정가 <span class="ctd-list">${list}</span></span> · 할인 −${save}` : (list, save) => `<span class="ctd-gray">List <span class="ctd-list">${list}</span></span> · Save ${save}`,
  calcTotalList:  isKo ? (list) => `<span class="ctd-gray">정가 ${list}</span>`          : (list) => `<span class="ctd-gray">List price ${list}</span>`,
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
// AI cost calculator (cart-style): add model/token estimates to a list and
// see the running total. Model prices come from /api/pricing.
let _calcPricing = Object.create(null); // model_name → { input, output }  ($ / 1M)
let _calcGroupRatio = 1;
let _calcItems = [];
let _calcSeq = 0;
let _calcLastAddedId = null; // 렌더 1회에만 소비되는 "새로 추가됨" 표시

// full amount → "$1,234.56"; very large → compact "$1.2B" so the total never overflows
const _calcMoney = (v) =>
  "$" +
  (Math.abs(v) >= 1e6
    ? new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 2 }).format(v)
    : v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }));

const _calcUnitStr = (v) =>
  v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

// "모델 & 가격" 전체 가격표(popup.js renderPricingTable)와 동일한 제공사·티어 정렬 기준.
function _calcProvider(name) {
  if (name.startsWith("claude")) return "Anthropic";
  if (name.startsWith("gemini")) return "Google";
  if (name.startsWith("gpt") || name.startsWith("o1") || name.startsWith("o3") || name.startsWith("o4")) return "OpenAI";
  return "Other";
}
function _calcTier(name) {
  if (name.includes("haiku") || name.includes("flash-lite")) return 1;
  if (name.includes("sonnet") || (name.includes("flash") && !name.includes("lite"))) return 2;
  if (name.includes("opus") || name.includes("pro")) return 3;
  return 2;
}

async function loadCalcModels() {
  const sel = document.getElementById("calcModel");
  if (!sel) return;
  try {
    const res = await fetch("/api/pricing");
    const json = await res.json();
    _calcGroupRatio = 2;
    const models = json.data
      .map((model) => ({ ...model, model_name: String(model.model_name ?? "") }))
      .sort((a, b) => {
        const pa = _calcProvider(a.model_name), pb = _calcProvider(b.model_name);
        if (pa !== pb) return pa.localeCompare(pb);
        const ta = _calcTier(a.model_name), tb = _calcTier(b.model_name);
        if (ta !== tb) return ta - tb;
        return a.model_name.localeCompare(b.model_name);
      });
    models.forEach((m) => {
      // 전체 가격표의 discount_percent는 적용하지 않고 원래(정가) 금액으로 계산.
      // 로그인 사용자의 할인율은 addCalcItem에서 별도로 반영한다.
      _calcPricing[m.model_name] = {
        input: m.model_ratio * _calcGroupRatio,
        output: m.model_ratio * m.completion_ratio * _calcGroupRatio,
      };
    });
    let lastProvider = "";
    const nodes = [];
    models.forEach((m) => {
      const provider = _calcProvider(m.model_name);
      if (provider !== lastProvider) {
        const group = document.createElement("optgroup");
        group.label = provider;
        group.dataset.provider = provider;
        nodes.push(group);
        lastProvider = provider;
      }
      const option = document.createElement("option");
      option.value = m.model_name;
      const p = _calcPricing[m.model_name];
      option.textContent = `${m.model_name} ($${_calcUnitStr(p.input)} / $${_calcUnitStr(p.output)})`;
      nodes[nodes.length - 1].appendChild(option);
    });
    sel.replaceChildren(...nodes);
    updateCalcUnitLabels();
  } catch (e) {
    const errorOption = document.createElement("option");
    errorOption.value = "";
    errorOption.textContent = STRINGS.calcLoadError;
    sel.replaceChildren(errorOption);
  }
}
loadCalcModels();
// 초기(0개) 상태에서도 정가 라인을 노출해 하단이 비어 보이지 않도록 한다.
renderCalcCart();

function updateCalcUnitLabels() {
  const sel = document.getElementById("calcModel");
  const p = sel && _calcPricing[sel.value];
  const inEl = document.getElementById("calcInUnit");
  const outEl = document.getElementById("calcOutUnit");
  if (inEl) inEl.textContent = p ? STRINGS.calcUnit(_calcUnitStr(p.input)) : "";
  if (outEl) outEl.textContent = p ? STRINGS.calcUnit(_calcUnitStr(p.output)) : "";
}

// 할인율 뱃지(0/5/10/15/20)를 클릭하면 직접입력 인풋에 값을 채우고 활성 상태를 표시.
function setCalcDiscount(v, el) {
  const input = document.getElementById("calcDiscount");
  if (input) input.value = v;
  document
    .querySelectorAll(".calc-disc-badge")
    .forEach((b) => b.classList.remove("active"));
  if (el) el.classList.add("active");
}

// 직접입력 시: 정수(0~100)만 허용하고, 프리셋과 값이 일치하는 뱃지만 활성화(없으면 전부 해제).
function onCalcDiscountInput(input) {
  let v = input.value.replace(/[^\d]/g, ""); // 숫자만 (음수·소수점·문자 차단)
  if (v !== "") {
    let n = parseInt(v, 10);
    if (n > 100) n = 100;
    v = String(n);
  }
  if (v !== input.value) input.value = v;
  const val = v === "" ? null : parseFloat(v);
  document.querySelectorAll(".calc-disc-badge").forEach((b) => {
    b.classList.toggle("active", val !== null && parseFloat(b.dataset.disc) === val);
  });
}

// 입력값이 비어 있을 때 살짝 안내 + 인풋 강조.
function calcHintNeedTokens() {
  const hint = document.getElementById("calcHint");
  if (hint) hint.textContent = STRINGS.calcNeedTokens;
  ["calcInputTokens", "calcOutputTokens"].forEach((id) => {
    const el = document.getElementById(id);
    if (!el || _calcPositive(id) > 0) return; // 0 초과인 정상 필드는 강조하지 않음
    el.classList.add("calc-input-invalid");
    setTimeout(() => el.classList.remove("calc-input-invalid"), 1200);
  });
}

// 토큰 입력 최댓값(placeholder "0.1~100,000"과 동일한 단위: 백만 토큰).
const CALC_MAX_TOKENS_M = 100000;

// 토큰 입력 정제: 입력 순간부터 숫자와 소수점 하나만 허용한다.
// -, +, e, 공백, 문자 등은 애초에 들어가지 못하게 즉시 제거(음수 불가 → 값은 항상 0 이상).
// 최댓값을 넘으면 즉시 상한으로 클램프.
function sanitizeCalcNum(el) {
  let v = el.value.replace(/[^\d.]/g, ""); // 숫자·점만 남김
  const firstDot = v.indexOf(".");
  if (firstDot !== -1) {
    // 첫 소수점만 유지, 이후의 점은 제거
    v = v.slice(0, firstDot + 1) + v.slice(firstDot + 1).replace(/\./g, "");
  }
  const n = parseFloat(v);
  if (Number.isFinite(n) && n > CALC_MAX_TOKENS_M) {
    v = String(CALC_MAX_TOKENS_M);
  }
  if (v !== el.value) el.value = v;
}

// 안전 파싱: 유한한 양수만 인정, 그 외(음수·NaN·문자열)는 0.
function _calcPositive(id) {
  const v = parseFloat(document.getElementById(id).value);
  return Number.isFinite(v) && v > 0 ? v : 0;
}

function addCalcItem() {
  const model = document.getElementById("calcModel").value;
  const p = _calcPricing[model];
  if (!p) return;
  const inputM = _calcPositive("calcInputTokens");
  const outputM = _calcPositive("calcOutputTokens");
  // 입력·출력 모두 0 초과여야 유효한 견적. 한쪽이라도 0/빈값이면 차단.
  if (inputM <= 0 || outputM <= 0) {
    calcHintNeedTokens();
    return;
  }
  const hint = document.getElementById("calcHint");
  if (hint) hint.textContent = "";
  // 로그인 시 노출되는 할인율(뱃지/직접입력)이 있으면 반영 (없으면 정가)
  const discountEl = document.getElementById("calcDiscount");
  let discountPct = discountEl ? parseFloat(discountEl.value) || 0 : 0;
  discountPct = Math.min(100, Math.max(0, discountPct));
  const listCost = inputM * p.input + outputM * p.output;
  const cost = listCost * (1 - discountPct / 100);
  // 기존 항목들이 아래로 밀리는 것도 부드럽게 보이도록, 삽입 전 위치를 기록해둔다.
  const prevTops = {};
  document.querySelectorAll(".calc-cart-item").forEach((el) => {
    prevTops[el.dataset.id] = el.getBoundingClientRect().top;
  });
  // 새로 추가한 항목이 맨 위에 보이도록 앞에 삽입한다.
  _calcItems.unshift({ id: ++_calcSeq, model, inputM, outputM, listCost, cost, discount: discountPct });
  _calcLastAddedId = _calcSeq; // 방금 추가된 항목에만 등장 애니메이션 적용
  renderCalcCart();
  _calcFlipShift(prevTops);
}

// FLIP: 삭제로 아래 항목들이 순간이동하듯 튀지 않도록, 이전 위치에서 새 위치로 부드럽게 슬라이드.
function _calcFlipShift(prevTops) {
  document.querySelectorAll(".calc-cart-item").forEach((el) => {
    const prevTop = prevTops[el.dataset.id];
    if (prevTop == null) return;
    const delta = prevTop - el.getBoundingClientRect().top;
    if (Math.abs(delta) < 0.5) return;
    el.style.transition = "none";
    el.style.transform = `translateY(${delta}px)`;
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        el.style.transition = "transform 0.22s ease-out";
        el.style.transform = "";
        el.addEventListener("transitionend", () => { el.style.transition = ""; }, { once: true });
      });
    });
  });
}

function removeCalcItem(id) {
  // 삭제 애니메이션(fade-out) 재생 후 실제로 목록에서 제거한다.
  const row = document.querySelector(`.calc-cart-item[data-id="${id}"]`);
  if (row) {
    row.classList.remove("cci-new"); // 등장 애니메이션과 겹치지 않도록 먼저 해제
    row.classList.add("cci-removing");
    setTimeout(() => {
      const prevTops = {};
      document.querySelectorAll(".calc-cart-item").forEach((el) => {
        if (el.dataset.id !== String(id)) prevTops[el.dataset.id] = el.getBoundingClientRect().top;
      });
      _calcItems = _calcItems.filter((it) => it.id !== id);
      renderCalcCart();
      _calcFlipShift(prevTops);
    }, 220);
  } else {
    _calcItems = _calcItems.filter((it) => it.id !== id);
    renderCalcCart();
  }
}

function clearCalcItems() {
  const rows = [...document.querySelectorAll(".calc-cart-item")];
  if (rows.length === 0) {
    _calcItems = [];
    renderCalcCart();
    return;
  }
  // 한 번에 사라지지 않고 위에서부터 순서대로 하나씩 fade-out 되도록 지연을 준다.
  const STAGGER_MS = 50;
  const FADE_MS = 220;
  rows.forEach((row, i) => {
    setTimeout(() => {
      row.classList.remove("cci-new");
      row.classList.add("cci-removing");
    }, i * STAGGER_MS);
  });
  setTimeout(() => {
    _calcItems = [];
    renderCalcCart();
  }, (rows.length - 1) * STAGGER_MS + FADE_MS);
}

function renderCalcCart() {
  const list = document.getElementById("calcItemList");
  const totalEl = document.getElementById("calcTotal");
  const countEl = document.getElementById("calcItemCount");
  const discEl = document.getElementById("calcTotalDiscount");
  if (!list) return;
  if (countEl) countEl.textContent = _calcItems.length;

  if (_calcItems.length === 0) {
    list.innerHTML = `<div class="calc-cart-empty">${STRINGS.calcEmpty}</div>`;
    if (totalEl) totalEl.textContent = "$0.00";
    // 비어 있어도 정가 라인을 그대로 노출해 아래 공간이 허전하지 않도록 한다.
    if (discEl) discEl.innerHTML = STRINGS.calcTotalList(_calcMoney(0));
    return;
  }

  let totalCost = 0;
  let totalList = 0;
  list.innerHTML = "";
  _calcItems.forEach((it) => {
    totalCost += it.cost;
    totalList += it.listCost != null ? it.listCost : it.cost;
    // inputM/outputM/discount 는 정제된 숫자라 innerHTML 삽입이 안전(모델명만 textContent).
    const subDisc =
      it.discount > 0
        ? `<span class="cci-disc">${STRINGS.calcItemDiscount(it.discount)}</span>`
        : "";
    const row = document.createElement("div");
    row.className = "calc-cart-item" + (it.id === _calcLastAddedId ? " cci-new" : "");
    row.dataset.id = it.id;
    row.innerHTML =
      `<div><div class="cci-name"></div><div class="cci-sub">In: ${it.inputM}M / Out: ${it.outputM}M${subDisc}</div></div>` +
      `<div class="cci-right"><span class="cci-cost">${_calcMoney(it.cost)}</span>` +
      `<button class="cci-remove" type="button" aria-label="${STRINGS.calcRemove}" onclick="removeCalcItem(${it.id})">✕</button></div>`;
    row.querySelector(".cci-name").textContent = it.model; // set as text to avoid HTML injection
    list.appendChild(row);
  });
  _calcLastAddedId = null; // 이번 렌더에서 소비 완료
  if (totalEl) totalEl.textContent = _calcMoney(totalCost);
  if (discEl) {
    const saved = totalList - totalCost;
    // 정가는 항상 노출하고, 할인이 있으면 옆에 할인액을 덧붙인다.
    discEl.innerHTML =
      saved > 0.005
        ? STRINGS.calcTotalDiscount(_calcMoney(totalList), _calcMoney(saved))
        : STRINGS.calcTotalList(_calcMoney(totalList));
  }
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
