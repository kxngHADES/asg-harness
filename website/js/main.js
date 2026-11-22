/* ASG website behaviour. Vanilla JS, no dependencies.
   - detects the visitor's OS/CPU and points the main button at the right setup file
   - every download link uses GitHub's /releases/latest/download/<file> redirect,
     so it always serves the newest release without this page changing
   - the GitHub API is only used to show the version number and file sizes */
(() => {
  const REPO = 'kxngHADES/asg-harness';
  const LATEST = `https://github.com/${REPO}/releases/latest/download/`;
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];

  /* ───────── OS detection ───────── */
  const OS_NAME = { windows: 'Windows', mac: 'macOS', linux: 'Linux' };
  const ICONS = {
    windows: $('[data-os="windows"] .os__icon svg')?.outerHTML,
    mac: $('[data-os="mac"] .os__icon svg')?.outerHTML,
    linux: $('[data-os="linux"] .os__icon svg')?.outerHTML,
  };
  function setupFile(os, arch) {
    if (os === 'windows') return `asg-setup_windows_${arch}.exe`;
    if (os === 'mac') return 'asg-setup_darwin_universal.dmg';
    if (os === 'linux') return `asg-setup_linux_${arch}`;
    return null;
  }
  function detect() {
    const ua = navigator.userAgent;
    const plat = ((navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || '').toLowerCase();
    let os = 'unknown';
    if (/iphone|ipad|ipod/i.test(ua) || (plat.includes('mac') && navigator.maxTouchPoints > 1)) os = 'ios';
    else if (/android/i.test(ua)) os = 'android';
    else if (plat.includes('win') || /windows/i.test(ua)) os = 'windows';
    else if (plat.includes('mac') || /mac os x/i.test(ua)) os = 'mac';
    else if (plat.includes('linux') || /linux|x11|cros/i.test(ua)) os = 'linux';
    const arch = /arm64|aarch64|armv8/i.test(ua) ? 'arm64' : 'amd64';
    return { os, arch };
  }
  const sys = detect();

  function applySystem() {
    const primary = $('[data-dl-primary]');
    const label = $('[data-dl-label]');
    const note = $('[data-dl-note]');
    const icon = $('[data-os-icon]');
    const file = setupFile(sys.os, sys.arch);
    $$('.os').forEach((c) => c.classList.toggle('is-mine', c.dataset.os === sys.os));

    // Point Windows/Linux cards at the right CPU build; offer the other one as the alt link.
    ['windows', 'linux'].forEach((os) => {
      const card = $(`.os[data-os="${os}"]`);
      if (!card || sys.os !== os) return;
      const mine = setupFile(os, sys.arch), other = setupFile(os, sys.arch === 'arm64' ? 'amd64' : 'arm64');
      $('[data-dl]', card).href = LATEST + mine;
      $('[data-file]', card).textContent = mine;
      const alt = $('[data-alt]', card);
      if (alt) { alt.href = LATEST + other; alt.textContent = sys.arch === 'arm64' ? 'x64 build' : 'ARM64 build'; }
    });

    if (file) {
      primary.href = LATEST + file;
      label.textContent = `Download for ${OS_NAME[sys.os]}`;
      if (icon && ICONS[sys.os]) icon.innerHTML = ICONS[sys.os];
      note.textContent = `${file} · straight from the latest GitHub release`;
      if (sys.os === 'linux') note.textContent += ' · chmod +x, then run it';
    } else {
      // Phones and unknown systems: ASG is a desktop/terminal app, so show the options.
      primary.href = '#download';
      label.textContent = 'Get ASG for your computer';
      note.textContent = sys.os === 'ios' || sys.os === 'android'
        ? 'ASG runs on macOS, Windows and Linux. Open this page on your computer to download.'
        : 'Pick your system below, or install from a terminal.';
    }
    // Preselect the matching install tab.
    selectTab(sys.os === 'windows' ? 'win' : 'unix', false);
  }

  async function refineArch() {
    try {
      if (!navigator.userAgentData || !navigator.userAgentData.getHighEntropyValues) return;
      const v = await navigator.userAgentData.getHighEntropyValues(['architecture', 'bitness']);
      if (v.architecture === 'arm' && v.bitness === '64') { sys.arch = 'arm64'; applySystem(); }
    } catch (_) { /* keep the guess */ }
  }

  /* ───────── Release info (version + sizes) ───────── */
  const fmtSize = (b) => (b > 1048576 ? `${(b / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(b / 1024))} KB`);
  function ago(iso) {
    const d = (Date.now() - new Date(iso).getTime()) / 86400000;
    if (d < 1) return 'today';
    if (d < 2) return 'yesterday';
    if (d < 30) return `${Math.floor(d)} days ago`;
    return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }
  async function loadRelease() {
    let rel = null;
    try {
      const cached = sessionStorage.getItem('asg-release');
      if (cached) {
        const c = JSON.parse(cached);
        if (Date.now() - c.t < 10 * 60 * 1000) rel = c.rel;
      }
    } catch (_) { /* storage blocked */ }
    if (!rel) {
      try {
        const r = await fetch(`https://api.github.com/repos/${REPO}/releases/latest`, { headers: { Accept: 'application/vnd.github+json' } });
        if (!r.ok) return;
        const j = await r.json();
        rel = { tag: j.tag_name, date: j.published_at, url: j.html_url, assets: (j.assets || []).map((a) => ({ name: a.name, size: a.size })) };
        try { sessionStorage.setItem('asg-release', JSON.stringify({ t: Date.now(), rel })); } catch (_) {}
      } catch (_) { return; } // offline or rate-limited: links still work, we just skip the details
    }
    const sizes = Object.fromEntries(rel.assets.map((a) => [a.name, a.size]));
    $$('[data-release-tag]').forEach((el) => (el.textContent = `${rel.tag} · ${ago(rel.date)}`));
    const line = $('[data-release-line]');
    if (line) line.innerHTML = `Latest is <strong>${rel.tag}</strong>, released ${ago(rel.date)}. Every button fetches it straight from GitHub. <a href="${rel.url}">Release notes</a>`;
    $$('.os').forEach((card) => {
      const f = $('[data-file]', card);
      if (f && sizes[f.textContent]) f.textContent = `${f.textContent} · ${fmtSize(sizes[f.textContent])}`;
    });
    $$('[data-more-list] a').forEach((a) => {
      const n = a.textContent.trim();
      if (sizes[n] && !a.querySelector('small')) a.insertAdjacentHTML('beforeend', `<small>${fmtSize(sizes[n])}</small>`);
    });
    const note = $('[data-dl-note]');
    const file = setupFile(sys.os, sys.arch);
    if (note && file && sizes[file]) note.textContent = `${rel.tag} · ${file} · ${fmtSize(sizes[file])}`;
  }

  /* ───────── Install tabs + copy ───────── */
  const tabs = $$('[role="tab"]');
  function selectTab(id, focus = true) {
    tabs.forEach((t) => {
      const on = t.dataset.tab === id;
      t.setAttribute('aria-selected', on);
      t.tabIndex = on ? 0 : -1;
      $('#' + t.getAttribute('aria-controls')).hidden = !on;
      if (on && focus) t.focus();
    });
  }
  tabs.forEach((t, i) => {
    t.addEventListener('click', () => selectTab(t.dataset.tab));
    t.addEventListener('keydown', (e) => {
      const d = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
      if (d) selectTab(tabs[(i + d + tabs.length) % tabs.length].dataset.tab);
    });
  });

  const toastEl = $('.toast');
  let toastT;
  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.classList.add('show');
    clearTimeout(toastT);
    toastT = setTimeout(() => toastEl.classList.remove('show'), 2200);
  }
  async function copyText(text) {
    try { await navigator.clipboard.writeText(text); return true; }
    catch (_) {
      const ta = Object.assign(document.createElement('textarea'), { value: text });
      document.body.append(ta); ta.select();
      const ok = document.execCommand('copy'); ta.remove(); return ok;
    }
  }
  $$('.copy').forEach((btn) => btn.addEventListener('click', async () => {
    const ok = await copyText($('code', btn.parentElement).textContent.trim());
    btn.textContent = ok ? 'Copied' : 'Press Ctrl+C';
    btn.classList.toggle('is-done', ok);
    setTimeout(() => { btn.textContent = 'Copy'; btn.classList.remove('is-done'); }, 1800);
  }));

  // Toast when a download starts (the link itself does the work).
  document.addEventListener('click', (e) => {
    const a = e.target.closest('a[href*="/releases/latest/download/"]');
    if (a) toast(`Downloading ${a.href.split('/').pop()}`);
  });

  /* ───────── Reveal on scroll ───────── */
  const io = new IntersectionObserver((ents) => ents.forEach((en) => {
    if (en.isIntersecting) { en.target.classList.add('in'); io.unobserve(en.target); }
  }), { threshold: 0.18, rootMargin: '0px 0px -6% 0px' });
  $$('.reveal').forEach((el) => io.observe(el));

  /* ───────── Loop diagram: light each step as the pulse passes ───────── */
  const steps = $$('.loop__steps li');
  const pulse = $('.loop__pulse');
  const loopStart = performance.now();
  // The pulse dwells on each node, and after Review it hops back to Work once (the retry arc).
  const route = [0, 1, 2, 1, 2, 3, 4];
  function loopTick(now) {
    const t = (Math.max(0, now - loopStart) / 900) % route.length;
    const k = Math.floor(t), f = t - k;
    const from = route[k], to = route[(k + 1) % route.length];
    const move = f < 0.55 ? 0 : (f - 0.55) / 0.45;           // rest, then glide
    const e = move < 0.5 ? 2 * move * move : 1 - (-2 * move + 2) ** 2 / 2;
    const wrap = to < from && from === 4;                      // end of run: jump to start
    const pos = wrap ? (move > 0 ? to : from) : from + (to - from) * e;
    pulse.setAttribute('cx', 100 + pos * 200);
    pulse.setAttribute('cy', 150 - (to < from && !wrap ? Math.sin(e * Math.PI) * 92 : 0));
    const hot = Math.round(pos);
    steps.forEach((s, i) => s.classList.toggle('is-hot', i === hot));
  }

  /* ───────── Scroll engine (one rAF loop for everything) ───────── */
  const hero = $('.hero');
  const reel = $('.reel'), track = $('.reel__track'), reelBar = $('.reel__progress');
  const modes = $$('.mode');
  const rows = $$('.kinetic__row');
  const parallax = $$('[data-speed]');
  const dock = $('.dock'), dockLabel = $('[data-dock-label]');
  const sections = $$('[data-section]');
  let lastY = scrollY, vel = 0, offset = 0, currentLabel = 'Intro';
  let loopVisible = false;
  new IntersectionObserver(([en]) => { loopVisible = en.isIntersecting; }).observe($('.loop'));

  function tick(now) {
    const y = scrollY, vh = innerHeight;
    const dy = y - lastY; lastY = y;
    vel += (dy - vel) * 0.12;

    // hero: progress feeds the shell + parallax on the title
    const hp = Math.min(1, Math.max(0, y / (hero.offsetHeight || 1)));
    window.dispatchEvent(new CustomEvent('asg:hero', { detail: hp }));

    // kinetic rows: drift + scroll velocity; width axis stretches with speed
    if (!reduce) {
      offset += 0.6 + Math.abs(vel) * 0.9;
      const wd = Math.max(60, Math.min(150, 100 + vel * 2.2));
      rows.forEach((r) => {
        const dir = +r.dataset.dir, half = r.scrollWidth / 2;
        const x = dir > 0 ? -(offset % half) : -half + (offset % half);
        r.style.transform = `translate3d(${x}px,0,0)`;
        r.style.setProperty('--w', wd.toFixed(1));
      });
    }

    // pinned horizontal reel (desktop only)
    if (innerWidth > 760 && reel) {
      const r = reel.getBoundingClientRect();
      const total = reel.offsetHeight - vh;
      const p = Math.min(1, Math.max(0, -r.top / (total || 1)));
      const maxX = Math.max(0, track.scrollWidth - innerWidth);
      track.style.transform = `translate3d(${-p * maxX}px,0,0)`;
      reelBar.style.setProperty('--p', p.toFixed(4));
      const active = Math.min(modes.length - 1, Math.round(p * (modes.length - 1)));
      modes.forEach((m, i) => m.classList.toggle('is-active', i === active));
    }

    // collage parallax
    if (!reduce) parallax.forEach((el) => {
      if (innerWidth < 860 && !el.classList.contains('blend__disc')) { el.style.removeProperty('--y'); return; }
      const r = el.getBoundingClientRect();
      if (r.bottom < -200 || r.top > vh + 200) return;
      const c = r.top + r.height / 2 - vh / 2;
      el.style.setProperty('--y', `${(-c * +el.dataset.speed).toFixed(1)}px`);
      if (el.classList.contains('blend__disc')) el.style.transform = `translate3d(0, ${(-c * +el.dataset.speed * 2).toFixed(1)}px, 0)`;
    });

    // dock: page progress + current section name; tuck away while scrolling down fast
    const max = document.documentElement.scrollHeight - vh;
    dock.style.setProperty('--sp', (y / (max || 1)).toFixed(4));
    let label = currentLabel;
    for (const s of sections) { if (s.getBoundingClientRect().top < vh * 0.45) label = s.dataset.section; }
    if (label !== currentLabel) {
      currentLabel = label; dockLabel.textContent = label;
      dockLabel.classList.remove('swap'); void dockLabel.offsetWidth; dockLabel.classList.add('swap');
    }
    dock.style.setProperty('--dy', vel > 14 ? '140%' : '0');

    if (loopVisible && !reduce) loopTick(now);
    requestAnimationFrame(tick);
  }

  /* ───────── Hero: timecode + RGB title follows the pointer ───────── */
  const title = $('.title'), tc = $('#tc');
  const t0 = performance.now();
  if (tc) setInterval(() => {
    const s = (performance.now() - t0) / 1000;
    const f = Math.floor((s % 1) * 24), ss = Math.floor(s) % 60, mm = Math.floor(s / 60) % 60, hh = Math.floor(s / 3600);
    tc.textContent = [hh, mm, ss, f].map((n) => String(n).padStart(2, '0')).join(':');
  }, 1000 / 24);
  if (!reduce) hero.addEventListener('pointermove', (e) => {
    const r = hero.getBoundingClientRect();
    title.style.setProperty('--px', ((e.clientX - r.left) / r.width - 0.5) * 2);
    title.style.setProperty('--py', ((e.clientY - r.top) / r.height - 0.5) * 2);
  });

  /* ───────── Magnetic buttons ───────── */
  if (!reduce && matchMedia('(pointer: fine)').matches) $$('.magnetic').forEach((b) => {
    b.addEventListener('pointermove', (e) => {
      const r = b.getBoundingClientRect();
      b.style.setProperty('--tx', `${(e.clientX - r.left - r.width / 2) * 0.25}px`);
      b.style.setProperty('--ty', `${(e.clientY - r.top - r.height / 2) * 0.35}px`);
    });
    b.addEventListener('pointerleave', () => { b.style.setProperty('--tx', '0px'); b.style.setProperty('--ty', '0px'); });
  });

  /* ───────── "/" command menu, like the one in the app ───────── */
  const dlg = $('#palette'), input = $('.palette__input'), list = $('.palette__list');
  const go = (hash) => () => { dlg.close(); document.querySelector(hash)?.scrollIntoView({ behavior: reduce ? 'auto' : 'smooth' }); };
  const ext = (url) => () => { dlg.close(); window.open(url, '_blank', 'noopener'); };
  function commands() {
    const file = setupFile(sys.os, sys.arch);
    const cmds = [];
    if (file) cmds.push({ t: 'download', d: `${file}`, c: 'var(--lime)', run: () => { dlg.close(); toast(`Downloading ${file}`); location.href = LATEST + file; } });
    cmds.push(
      { t: 'modes', d: 'build · plan · ask · loop', c: 'var(--cyan)', run: go('#modes') },
      { t: 'loop', d: 'how the harness works', c: 'var(--coral)', run: go('#loop') },
      { t: 'inside', d: 'gates, domains, memory, skills', c: 'var(--magenta)', run: go('#inside') },
      { t: 'all downloads', d: 'Windows · macOS · Linux', c: 'var(--lime)', run: go('#download') },
      { t: 'terminal', d: 'one-line install', c: 'var(--cyan)', run: go('#terminal') },
      { t: 'copy install', d: sys.os === 'windows' ? 'PowerShell command' : 'curl command', c: 'var(--magenta)', run: async () => {
        dlg.close();
        const pane = sys.os === 'windows' ? '#pane-win' : '#pane-unix';
        const ok = await copyText($(`${pane} code`).textContent.trim());
        toast(ok ? 'Install command copied' : 'Copy failed. Select the command below instead.');
        if (!ok) go('#terminal')();
      } },
      { t: 'docs', d: 'github.com · docs/', c: 'var(--cyan)', run: ext(`https://github.com/${REPO}/tree/main/docs`) },
      { t: 'github', d: REPO, c: 'var(--lime)', run: ext(`https://github.com/${REPO}`) },
      { t: 'top', d: 'back to the start', c: 'var(--magenta)', run: go('#top') },
    );
    return cmds;
  }
  let shown = [], sel = 0;
  function render() {
    const q = input.value.trim().toLowerCase();
    shown = commands().filter((c) => !q || c.t.includes(q) || c.d.toLowerCase().includes(q));
    sel = Math.min(sel, Math.max(0, shown.length - 1));
    list.innerHTML = shown.length
      ? shown.map((c, i) => `<li class="palette__item" role="option" id="opt-${i}" aria-selected="${i === sel}" style="--c:${c.c}"><b>${c.t}</b><span>${c.d}</span></li>`).join('')
      : `<li class="palette__empty mono">No command matches "${q.replace(/</g, '&lt;')}". Try "download" or "terminal".</li>`;
    input.setAttribute('aria-activedescendant', shown.length ? `opt-${sel}` : '');
    if (shown.length) list.children[sel].scrollIntoView({ block: 'nearest' });
  }
  function openPalette() {
    if (dlg.open) return;
    input.value = ''; sel = 0; render();
    dlg.showModal(); input.focus();
  }
  list.addEventListener('pointermove', (e) => {
    const li = e.target.closest('.palette__item'); if (!li) return;
    const i = [...list.children].indexOf(li);
    if (i !== sel) { sel = i; render(); }
  });
  list.addEventListener('click', (e) => {
    const li = e.target.closest('.palette__item'); if (!li) return;
    shown[[...list.children].indexOf(li)]?.run();
  });
  input.addEventListener('input', () => { sel = 0; render(); });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); sel = (sel + 1) % Math.max(1, shown.length); render(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); sel = (sel - 1 + shown.length) % Math.max(1, shown.length); render(); }
    else if (e.key === 'Enter') { e.preventDefault(); shown[sel]?.run(); }
  });
  dlg.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });
  $('[data-close-palette]').addEventListener('click', () => dlg.close());
  $('[data-open-palette]').addEventListener('click', openPalette);
  document.addEventListener('keydown', (e) => {
    const typing = /input|textarea|select/i.test(document.activeElement.tagName) || document.activeElement.isContentEditable;
    if ((e.key === '/' && !typing) || (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey))) { e.preventDefault(); openPalette(); }
  });

  /* ───────── Go ───────── */
  applySystem();
  refineArch();
  loadRelease();
  requestAnimationFrame(() => requestAnimationFrame(() => document.documentElement.classList.add('is-ready')));
  requestAnimationFrame(tick);
})();
