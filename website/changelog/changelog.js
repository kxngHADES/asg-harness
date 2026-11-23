/* ASG live changelog reader. Vanilla JS, zero dependencies.
   Fetches CHANGELOG.md live from GitHub (main branch), parses release-please
   sections, caches locally for offline resilience, and auto-refreshes. */
(() => {
  const REPO = 'kxngHADES/asg-harness';
  const RAW_URL = `https://raw.githubusercontent.com/${REPO}/main/CHANGELOG.md`;
  const CACHE_KEY = 'asg-changelog-md-v1';
  const POLL_MS = 60 * 1000;

  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];

  const livePill = $('#live-pill');
  const liveText = $('#live-text');
  const refreshBtn = $('#refresh-btn');
  const latestBadge = $('#latest-badge');
  const filterInput = $('#filter-input');
  const versionRail = $('#version-rail');
  const releasesRoot = $('#releases-root');
  const loadingState = $('#loading-state');

  let releases = [];
  let activeCat = 'all';
  let query = '';
  let lastSyncedAt = null;

  function escHtml(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function classifySection(title) {
    const t = title.toLowerCase();
    if (t.includes('feature')) return 'features';
    if (t.includes('fix') || t.includes('bug')) return 'fixes';
    if (t.includes('doc')) return 'docs';
    return 'other';
  }

  function renderInline(text) {
    // Scope prefix like "**mobile:** rest"
    let scopeHtml = '';
    const scopeMatch = text.match(/^\*\*([^*:]+):\*\*\s*(.*)$/);
    if (scopeMatch) {
      scopeHtml = `<span class="scope">${escHtml(scopeMatch[1])}</span>`;
      text = scopeMatch[2];
    }

    const codeTokens = [];
    let s = text.replace(/`([^`]+)`/g, (_, c) => {
      const id = codeTokens.length;
      codeTokens.push(`<code>${escHtml(c)}</code>`);
      return `\u0000C${id}\u0000`;
    });

    s = escHtml(s);
    s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, (_, label, url) => (
      `<a href="${escHtml(url.replace(/&amp;/g, '&'))}" target="_blank" rel="noopener">${label}</a>`
    ));
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');

    return scopeHtml + s.replace(/\u0000C(\d+)\u0000/g, (_, i) => codeTokens[+i]);
  }

  function parseChangelog(md) {
    const lines = md.replace(/\r\n/g, '\n').split('\n');
    const out = [];
    let curRel = null;
    let curSec = null;

    for (const line of lines) {
      // ## [0.6.1](https://.../compare/...) (2026-11-22)  OR  ## 0.1.0 (2026-10-28)
      const relMatch = line.match(/^##\s+(?:\[([^\]]+)\]\(([^)]+)\)|(\d+\.\d+\.\d+[^\s]*))\s*(?:\(([^)]+)\))?/);
      if (relMatch) {
        const version = (relMatch[1] || relMatch[3] || '').trim();
        curRel = {
          version,
          id: `v${version.replace(/[^\w.-]/g, '')}`,
          compareUrl: relMatch[2] || '',
          date: (relMatch[4] || '').trim(),
          sections: [],
        };
        out.push(curRel);
        curSec = null;
        continue;
      }

      // Stop at trailing "## Changelog" footer note if present
      if (/^##\s+Changelog\s*$/i.test(line)) {
        curRel = null;
        curSec = null;
        continue;
      }

      const secMatch = line.match(/^###\s+(.+)$/);
      if (secMatch && curRel) {
        const title = secMatch[1].trim();
        curSec = { title, kind: classifySection(title), items: [] };
        curRel.sections.push(curSec);
        continue;
      }

      const itemMatch = line.match(/^\s*[-*]\s+(.+)$/);
      if (itemMatch && curRel) {
        if (!curSec) {
          curSec = { title: 'Changes', kind: 'other', items: [] };
          curRel.sections.push(curSec);
        }
        const raw = itemMatch[1].trim();
        // Deduplicate exact repeated lines inside a section
        if (!curSec.items.includes(raw)) {
          curSec.items.push(raw);
        }
      }
    }
    return out;
  }

  function render() {
    loadingState.hidden = true;
    const q = query.trim().toLowerCase();

    const filtered = releases.map((rel) => {
      const sections = rel.sections
        .filter((sec) => activeCat === 'all' || sec.kind === activeCat)
        .map((sec) => ({
          ...sec,
          items: sec.items.filter((it) => !q || it.toLowerCase().includes(q) || rel.version.toLowerCase().includes(q)),
        }))
        .filter((sec) => sec.items.length > 0);
      return { ...rel, sections };
    }).filter((rel) => rel.sections.length > 0);

    if (releases.length > 0) {
      latestBadge.textContent = `latest · v${releases[0].version}`;
      latestBadge.hidden = false;
    }

    versionRail.innerHTML = filtered.map((rel, i) => (
      `<li><a href="#${rel.id}" class="${i === 0 && activeCat === 'all' && !q ? 'is-latest' : ''}">` +
        `<span>v${escHtml(rel.version)}</span>` +
        (rel.date ? `<span class="rail__date">${escHtml(rel.date.slice(5))}</span>` : '') +
      `</a></li>`
    )).join('');

    if (!filtered.length) {
      releasesRoot.innerHTML =
        `<div class="state-card"><p class="mono">No changelog entries match "${escHtml(query || activeCat)}".</p></div>`;
      return;
    }

    releasesRoot.innerHTML = filtered.map((rel) => {
      const isLatest = releases[0] && rel.version === releases[0].version;
      const releaseUrl = `https://github.com/${REPO}/releases/tag/v${encodeURIComponent(rel.version)}`;
      return (
        `<article class="release${isLatest ? ' release--latest' : ''}" id="${rel.id}">` +
          `<header class="release__head">` +
            `<div class="release__ver-wrap">` +
              `<h2 class="release__ver"><a href="#${rel.id}">v${escHtml(rel.version)}</a></h2>` +
              (isLatest ? `<span class="tag-latest">Latest</span>` : '') +
            `</div>` +
            `<div class="release__meta mono">` +
              (rel.date ? `<time datetime="${escHtml(rel.date)}">${escHtml(rel.date)}</time>` : '') +
              `<a href="${releaseUrl}" target="_blank" rel="noopener">Release ↗</a>` +
              (rel.compareUrl ? `<a href="${escHtml(rel.compareUrl)}" target="_blank" rel="noopener">Diff ↗</a>` : '') +
            `</div>` +
          `</header>` +
          rel.sections.map((sec) => (
            `<section class="section section--${sec.kind}">` +
              `<h3 class="section__title"><span class="section__dot" aria-hidden="true"></span>${escHtml(sec.title)}</h3>` +
              `<ul class="entries">` +
                sec.items.map((it) => (
                  `<li class="entry"><span class="entry__bullet" aria-hidden="true">▸</span><span class="entry__body">${renderInline(it)}</span></li>`
                )).join('') +
              `</ul>` +
            `</section>`
          )).join('') +
        `</article>`
      );
    }).join('');
  }

  function setLiveState(state, text) {
    livePill.dataset.state = state;
    liveText.textContent = text;
  }

  function timeAgo(ts) {
    const s = Math.max(0, Math.floor((Date.now() - ts) / 1000));
    if (s < 10) return 'just now';
    if (s < 60) return `${s}s ago`;
    return `${Math.floor(s / 60)}m ago`;
  }

  async function fetchLive({ manual = false } = {}) {
    if (manual) refreshBtn.classList.add('is-spinning');
    setLiveState('syncing', 'Syncing with GitHub…');
    try {
      const bust = `${RAW_URL}?t=${Date.now()}`;
      const res = await fetch(bust, { cache: 'no-store' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const md = await res.text();
      releases = parseChangelog(md);
      lastSyncedAt = Date.now();
      try { localStorage.setItem(CACHE_KEY, JSON.stringify({ t: lastSyncedAt, md })); } catch (_) {}
      setLiveState('live', 'Live · synced just now');
      render();
    } catch (err) {
      if (releases.length > 0) {
        setLiveState('offline', 'Offline · showing cached changelog');
      } else {
        loadingState.hidden = true;
        setLiveState('offline', 'Could not reach GitHub');
        releasesRoot.innerHTML =
          `<div class="state-card">` +
            `<p class="mono">Could not fetch live CHANGELOG.md (${escHtml(String(err.message || err))}).</p>` +
            `<p><a href="https://github.com/${REPO}/blob/main/CHANGELOG.md" target="_blank" rel="noopener">Open CHANGELOG.md on GitHub ↗</a></p>` +
          `</div>`;
      }
    } finally {
      refreshBtn.classList.remove('is-spinning');
    }
  }

  // Hydrate immediately from localStorage if available, then refresh live.
  try {
    const cached = localStorage.getItem(CACHE_KEY);
    if (cached) {
      const c = JSON.parse(cached);
      if (c && typeof c.md === 'string') {
        releases = parseChangelog(c.md);
        lastSyncedAt = c.t || null;
        render();
      }
    }
  } catch (_) {}

  /* ───────── Controls ───────── */
  filterInput.addEventListener('input', () => {
    query = filterInput.value;
    render();
  });

  $$('#category-chips .chip').forEach((btn) => {
    btn.addEventListener('click', () => {
      activeCat = btn.dataset.cat;
      $$('#category-chips .chip').forEach((b) => b.setAttribute('aria-selected', b === btn));
      render();
    });
  });

  refreshBtn.addEventListener('click', () => fetchLive({ manual: true }));

  $('[data-toggle-theme]').addEventListener('click', () => {
    const root = document.documentElement;
    const isDark = root.dataset.theme === 'dark' || (!root.dataset.theme && matchMedia('(prefers-color-scheme: dark)').matches);
    const next = isDark ? 'light' : 'dark';
    root.dataset.theme = next;
    try { localStorage.setItem('asg-theme', next); } catch (_) {}
  });

  document.addEventListener('keydown', (e) => {
    const typing = /input|textarea|select/i.test(document.activeElement.tagName);
    if (e.key === '/' && !typing) {
      e.preventDefault();
      filterInput.focus();
    }
  });

  // Live updates: poll every 60s when visible, and update the "synced X ago" label.
  setInterval(() => {
    if (!document.hidden) fetchLive();
  }, POLL_MS);
  setInterval(() => {
    if (lastSyncedAt && livePill.dataset.state === 'live') {
      liveText.textContent = `Live · synced ${timeAgo(lastSyncedAt)}`;
    }
  }, 15000);
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) fetchLive();
  });

  fetchLive();
})();
