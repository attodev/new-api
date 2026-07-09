/* ── AlRouter 사용 가이드 페이지 스크립트 ── */
(function () {
  // lucide 아이콘 렌더
  if (window.lucide && typeof window.lucide.createIcons === 'function') {
    window.lucide.createIcons();
  }

  // 모바일 햄버거 메뉴 (landing.js와 동일 동작)
  var hamburger = document.querySelector('.gnb-hamburger');
  var gnbMenu = document.querySelector('.gnb-menu');
  if (hamburger && gnbMenu) {
    hamburger.addEventListener('click', function () {
      var isOpen = gnbMenu.classList.toggle('open');
      hamburger.classList.toggle('open', isOpen);
      hamburger.setAttribute('aria-expanded', String(isOpen));
    });
  }

  // 가이드 영상 하나(재생 오버레이 + 종료 시 CTA/다시보기)에 필요한 동작을 전부 연결.
  // 매뉴얼 설정 패널과 CC Switch 패널 둘 다 동일한 구조라 id 접미사만 바꿔 재사용한다.
  function wireGuideVideo(suffix, ctaLeadSeconds) {
    var guideVideo = document.getElementById('guideVideo' + suffix);
    if (!guideVideo) return;

    // 일시정지 상태에서 큰 재생 버튼을 표시
    // — 재생 중에는 오버레이를 완전히 숨겨 네이티브 컨트롤(탐색바 등) 클릭을 가리지 않도록 함
    // — 영상이 끝난 상태(ended)에서는 이 오버레이 대신 CTA 옆의 "다시 보기" 버튼을 노출하므로 표시하지 않음
    var guidePlayOverlay = document.getElementById('guidePlayOverlay' + suffix);
    if (guidePlayOverlay) {
      var renderGuidePlayOverlay = function () {
        if (guideVideo.ended) {
          guidePlayOverlay.classList.remove('visible');
        } else if (guideVideo.paused) {
          guidePlayOverlay.classList.add('visible');
        } else {
          guidePlayOverlay.classList.remove('visible');
        }
      };

      guideVideo.addEventListener('play', renderGuidePlayOverlay);
      guideVideo.addEventListener('pause', renderGuidePlayOverlay);
      guideVideo.addEventListener('ended', renderGuidePlayOverlay);
      guidePlayOverlay.addEventListener('click', function () {
        guideVideo.play();
      });
      renderGuidePlayOverlay();
    }

    // 영상 마지막에 체험하기 CTA 노출 (랜딩 페이지 히어로 영상과 동일한 동작)
    var guideCtaWrap = document.getElementById('guideCtaWrap' + suffix);
    var guideCtaBtn = document.getElementById('guideCtaBtn' + suffix);
    var guideReplayBtn = document.getElementById('guideReplayBtn' + suffix);
    if (guideCtaWrap && guideCtaBtn) {
      var CTA_LEAD_SECONDS = ctaLeadSeconds || 5;
      guideVideo.addEventListener('timeupdate', function () {
        var showFrom = guideVideo.duration - CTA_LEAD_SECONDS;
        if (guideVideo.currentTime >= showFrom && guideCtaWrap.style.display === 'none') {
          guideCtaWrap.style.display = 'flex';
          setTimeout(function () { guideCtaBtn.classList.add('active'); }, 50);
        } else if (guideVideo.currentTime < showFrom && guideCtaWrap.style.display !== 'none') {
          guideCtaBtn.classList.remove('active');
          guideCtaWrap.style.display = 'none';
        }
      });
      guideVideo.addEventListener('play', function () {
        if (guideVideo.currentTime < guideVideo.duration - CTA_LEAD_SECONDS) {
          guideCtaBtn.classList.remove('active');
          guideCtaWrap.style.display = 'none';
        }
      });

      if (guideReplayBtn) {
        // 다시보기 버튼은 실제로 영상이 끝난(ended) 상태일 때만 노출.
        // 끝난 뒤 재생바를 앞/뒤로 당기면 ended가 풀리므로 그때마다 다시 감춘다.
        var syncReplayBtn = function () {
          guideReplayBtn.style.display = guideVideo.ended ? 'flex' : 'none';
        };
        guideVideo.addEventListener('ended', syncReplayBtn);
        guideVideo.addEventListener('seeking', syncReplayBtn);
        guideVideo.addEventListener('play', syncReplayBtn);
        guideReplayBtn.addEventListener('click', function () {
          guideReplayBtn.style.display = 'none';
          guideCtaBtn.classList.remove('active');
          guideCtaWrap.style.display = 'none';
          guideVideo.currentTime = 0;
          guideVideo.play();
        });
      }
    }
  }

  wireGuideVideo('');
  wireGuideVideo('Cc', 7);

  // 그룹 토글 (수동 설정 / CC Switch)
  // 선택한 탭과 스크롤 위치를 sessionStorage에 저장해, 새로고침이나 언어 전환(같은 탭 내 이동) 후에도 유지되도록 함
  var GROUP_STORAGE_KEY = 'guideActiveGroup';
  var SCROLL_STORAGE_KEY = 'guideScrollY';

  function activateGroup(group, opts) {
    var btn = document.querySelector('.guide-group-btn[data-group="' + group + '"]');
    var target = document.getElementById('guide-group-' + group);
    if (!btn || !target) return;
    document.querySelectorAll('.guide-group-btn').forEach(function (b) {
      b.setAttribute('aria-selected', 'false');
    });
    // 비활성화되는 그룹의 영상은 화면에서만 숨겨질 뿐 재생은 계속되므로, 탭을 떠날 때 처음 상태로 되돌린다
    document.querySelectorAll('.guide-group.active').forEach(function (g) {
      if (g === target) return;
      g.querySelectorAll('video').forEach(function (v) {
        v.pause();
        v.currentTime = 0;
      });
    });
    document.querySelectorAll('.guide-group').forEach(function (g) {
      g.classList.remove('active');
    });
    btn.setAttribute('aria-selected', 'true');
    target.classList.add('active');
    if (!opts || !opts.skipSave) {
      sessionStorage.setItem(GROUP_STORAGE_KEY, group);
    }
  }

  document.querySelectorAll('.guide-group-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      activateGroup(btn.dataset.group);
    });
  });

  var savedGroup = sessionStorage.getItem(GROUP_STORAGE_KEY);
  if (savedGroup) {
    activateGroup(savedGroup, { skipSave: true });
  }

  // 스크롤 위치 복원: 탭 전환으로 레이아웃이 바뀐 뒤에 적용해야 정확하므로 그룹 복원 이후에 실행
  var savedScrollY = sessionStorage.getItem(SCROLL_STORAGE_KEY);
  if (savedScrollY !== null) {
    requestAnimationFrame(function () {
      window.scrollTo(0, parseInt(savedScrollY, 10));
    });
  }
  window.addEventListener('beforeunload', function () {
    sessionStorage.setItem(SCROLL_STORAGE_KEY, String(window.scrollY));
  });

  // 단계별 화면 캡처: 파일이 아직 없으면(404) 조용히 숨기고,
  // 로드되면 보여준다. (사용자가 캡처 파일을 나중에 채워 넣는 구조)
  document.querySelectorAll('.guide-step-shot').forEach(function (img) {
    if (img.complete && img.naturalWidth > 0) {
      img.classList.add('is-loaded');
      return;
    }
    img.addEventListener('load', function () {
      img.classList.add('is-loaded');
    });
    img.addEventListener('error', function () {
      img.classList.remove('is-loaded');
      img.style.display = 'none';
    });
  });

  // 스크린샷 확대 보기 (라이트박스) — 클릭해서 확대, 화살표로 다음/이전 이미지 넘기기
  var lightbox = document.getElementById('shotLightbox');
  if (lightbox) {
    var lightboxImg = document.getElementById('shotLightboxImg');
    var lightboxCaption = document.getElementById('shotLightboxCaption');
    var lightboxPrev = document.getElementById('shotLightboxPrev');
    var lightboxNext = document.getElementById('shotLightboxNext');
    var lightboxClose = document.getElementById('shotLightboxClose');
    var lightboxDots = document.getElementById('shotLightboxDots');
    var currentIndex = -1;
    var currentShots = [];

    // 현재 열려 있는 그룹(수동 설정 / CC Switch) 안의 이미지끼리만 순서대로 이동하도록 스코프를 제한
    function shotList(img) {
      var scope = (img && img.closest('.guide-group')) || document.querySelector('.guide-group.active') || document;
      return Array.prototype.slice.call(scope.querySelectorAll('.guide-step-shot.is-loaded'));
    }

    function shotCaption(img) {
      var fig = img.closest('figure');
      var figcaption = fig && fig.querySelector('figcaption');
      return figcaption ? figcaption.textContent : (img.alt || '');
    }

    function renderDots() {
      if (!lightboxDots) return;
      lightboxDots.innerHTML = '';
      if (currentShots.length <= 1) return;
      currentShots.forEach(function (shot, i) {
        var dot = document.createElement('button');
        dot.className = 'shot-lightbox-dot' + (i === currentIndex ? ' active' : '');
        dot.setAttribute('aria-label', 'Go to image ' + (i + 1));
        dot.addEventListener('click', function () { showShot(i); });
        lightboxDots.appendChild(dot);
      });
    }

    function showShot(index) {
      if (!currentShots.length) return;
      // 순서대로만 이동 가능(양 끝에서 반대편으로 넘어가는 순환 없음)
      currentIndex = Math.min(Math.max(index, 0), currentShots.length - 1);
      var img = currentShots[currentIndex];
      lightboxImg.src = img.src;
      lightboxImg.alt = img.alt;
      lightboxCaption.textContent = shotCaption(img);
      lightboxPrev.disabled = currentIndex <= 0;
      lightboxNext.disabled = currentIndex >= currentShots.length - 1;
      renderDots();
    }

    function openLightbox(img) {
      currentShots = shotList(img);
      var index = currentShots.indexOf(img);
      if (index === -1) return;
      showShot(index);
      lightbox.classList.add('open');
      document.body.style.overflow = 'hidden';
    }

    function closeLightbox() {
      lightbox.classList.remove('open');
      document.body.style.overflow = '';
    }

    document.querySelectorAll('.guide-step-shot').forEach(function (img) {
      img.addEventListener('click', function () {
        if (img.classList.contains('is-loaded')) openLightbox(img);
      });
    });

    lightboxClose.addEventListener('click', closeLightbox);
    lightboxPrev.addEventListener('click', function () { showShot(currentIndex - 1); });
    lightboxNext.addEventListener('click', function () { showShot(currentIndex + 1); });
    lightbox.addEventListener('click', function (e) {
      if (e.target === lightbox) closeLightbox();
    });
    document.addEventListener('keydown', function (e) {
      if (!lightbox.classList.contains('open')) return;
      if (e.key === 'Escape') closeLightbox();
      else if (e.key === 'ArrowLeft') showShot(currentIndex - 1);
      else if (e.key === 'ArrowRight') showShot(currentIndex + 1);
    });
  }
})();
