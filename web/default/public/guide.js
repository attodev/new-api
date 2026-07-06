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
})();
