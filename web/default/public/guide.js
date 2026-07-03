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

  // 프로바이더 탭 전환 (그룹 내부에서만)
  document.querySelectorAll('.guide-tab-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var group = btn.closest('.guide-group');
      if (!group) return;
      group.querySelectorAll('.guide-tab-btn').forEach(function (b) {
        b.setAttribute('aria-selected', 'false');
      });
      group.querySelectorAll('.guide-panel').forEach(function (p) {
        p.classList.remove('active');
      });
      btn.setAttribute('aria-selected', 'true');
      var target = document.getElementById('guide-panel-' + btn.dataset.tab);
      if (target) target.classList.add('active');
    });
  });
})();
