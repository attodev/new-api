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

  // 가이드 영상 마지막에 체험하기 CTA 노출 (랜딩 페이지 히어로 영상과 동일한 동작)
  var guideVideo = document.getElementById('guideVideo');
  var guideCtaWrap = document.getElementById('guideCtaWrap');
  var guideCtaBtn = document.getElementById('guideCtaBtn');
  if (guideVideo && guideCtaWrap && guideCtaBtn) {
    var CTA_LEAD_SECONDS = 5;
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
  }

  // 그룹 토글 (수동 설정 / CC Switch)
  document.querySelectorAll('.guide-group-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      document.querySelectorAll('.guide-group-btn').forEach(function (b) {
        b.setAttribute('aria-selected', 'false');
      });
      document.querySelectorAll('.guide-group').forEach(function (g) {
        g.classList.remove('active');
      });
      btn.setAttribute('aria-selected', 'true');
      var target = document.getElementById('guide-group-' + btn.dataset.group);
      if (target) target.classList.add('active');
    });
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
    var currentIndex = -1;

    function shotList() {
      return Array.prototype.slice.call(document.querySelectorAll('.guide-step-shot.is-loaded'));
    }

    function shotCaption(img) {
      var fig = img.closest('figure');
      var figcaption = fig && fig.querySelector('figcaption');
      return figcaption ? figcaption.textContent : (img.alt || '');
    }

    function showShot(index) {
      var shots = shotList();
      if (!shots.length) return;
      currentIndex = Math.min(Math.max(index, 0), shots.length - 1);
      var img = shots[currentIndex];
      lightboxImg.src = img.src;
      lightboxImg.alt = img.alt;
      lightboxCaption.textContent = shotCaption(img);
      lightboxPrev.disabled = currentIndex <= 0;
      lightboxNext.disabled = currentIndex >= shots.length - 1;
    }

    function openLightbox(img) {
      var shots = shotList();
      var index = shots.indexOf(img);
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
