/* ASG docs reader. Vanilla JS, zero dependencies.
   Reads window.ASG_DOCS (generated from docs/*.md) and renders pages,
   table of contents, prev/next pager, and full-text "/" search. */
(() => {
  const REPO = 'kxngHADES/asg-harness';
  const DOCS = Array.isArray(window.ASG_DOCS) ? window.ASG_DOCS : [];
  const BY_SLUG = Object.fromEntries(DOCS.map((d) => [d.slug, d]));
  const BY_FILE = Object.fromEntries(DOCS.map((d) => [d.file.toLowerCase(), d]));
  const ACCENTS = {
    lime: 'var(--lime)',
    cyan: 'var(--cyan)',
    magenta: 'var(--magenta)',
    coral: 'var(--coral)',
  };

  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];

  /* ───────── Utilities ───────── */
  function escHtml(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function slugify(text, seen) {
    const base = text
      .replace(/`([^`]+)`/g, '$1')
      .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
      .replace(/[*_~]/g, '')
      .toLowerCase()
      .trim()
      .replace(/[^\w\s-]/g, '')
      .replace(/\s+/g, '-');
    const key = base || 'section';
    const n = seen.get(key) || 0;
    seen.set(key, n + 1);
    return n ? `${key}-${n}` : key;
  }

  function resolveHref(rawHref) {
    if (/^https?:\/\//i.test(rawHref) || rawHref.startsWith('mailto:')) {
      return { href: rawHref, external: true };
    }
    if (rawHref.startsWith('#')) {
      return { href: `#${currentSlug}/${rawHref.slice(1)}`, external: false };
    }
    if (rawHref.startsWith('../')) {
      const rel = rawHref.replace(/^\.\.\//, '');
      return { href: `https://github.com/${REPO}/blob/main/${rel}`, external: true };
    }
    const [filePart, hashPart] = rawHref.split('#');
    const doc = BY_FILE[filePart.toLowerCase()];
    if (doc) {
      return {
        href: hashPart ? `#${doc.slug}/${hashPart}` : `#${doc.slug}`,
        external: false,
      };
    }
    return { href: rawHref, external: false };
  }

  /* ───────── Markdown → HTML ───────── */
  function renderInline(src) {
    // Protect inline code spans first so formatting inside backticks is untouched.
    const tokens = [];
    let s = src.replace(/`([^`]+)`/g, (_, code) => {
      const id = tokens.length;
      tokens.push(`<code>${escHtml(code)}</code>`);
      return `\u0000CODE${id}\u0000`;
    });

    s = escHtml(s);

    // Links: [label](url)
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, label, url) => {
      const { href, external } = resolveHref(url.replace(/&amp;/g, '&'));
      const attrs = external ? ' target="_blank" rel="noopener"' : '';
      return `<a href="${escHtml(href)}"${attrs}>${label}</a>`;
    });

    // Strong + emphasis + strikethrough
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/__([^_]+)__/g, '<strong>$1</strong>');
    s = s.replace(/(^|[^\w])\*([^*\n]+)\*(?!\w)/g, '$1<em>$2</em>');
    s = s.replace(/(^|[^\w])_([^_\n]+)_(?!\w)/g, '$1<em>$2</em>');
    s = s.replace(/~~([^~]+)~~/g, '<del>$1</del>');

    return s.replace(/\u0000CODE(\d+)\u0000/g, (_, i) => tokens[+i]);
  }

  function splitTableRow(line) {
    const trimmed = line.trim().replace(/^\|/, '').replace(/\|$/, '');
    const cells = [];
    let cur = '', inCode = false;
    for (let i = 0; i < trimmed.length; i++) {
      const ch = trimmed[i];
      if (ch === '`') inCode = !inCode;
      if (ch === '\\' && trimmed[i + 1] === '|') { cur += '|'; i++; continue; }
      if (ch === '|' && !inCode) { cells.push(cur.trim()); cur = ''; continue; }
      cur += ch;
    }
    cells.push(cur.trim());
    return cells;
  }

  function isTableSep(line) {
    return /^\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?$/.test(line.trim());
  }

  function parseMarkdown(md) {
    const lines = md.replace(/\r\n/g, '\n').split('\n');
    const out = [];
    const headings = [];
    const seenIds = new Map();
    let i = 0;

    function isBlockStart(idx) {
      const l = lines[idx];
      if (!l || !l.trim()) return true;
      if (/^```/.test(l)) return true;
      if (/^#{1,4}\s+/.test(l)) return true;
      if (/^>\s?/.test(l)) return true;
      if (/^([-*_])\1{2,}\s*$/.test(l.trim())) return true;
      if (/^\s*([-*+]|\d+\.)\s+/.test(l)) return true;
      if (l.includes('|') && idx + 1 < lines.length && isTableSep(lines[idx + 1])) return true;
      return false;
    }

    while (i < lines.length) {
      const line = lines[i];

      // Blank line
      if (!line.trim()) { i++; continue; }

      // Fenced code block
      const fence = line.match(/^```(\S*)/);
      if (fence) {
        const lang = fence[1] || '';
        const buf = [];
        i++;
        while (i < lines.length && !/^```\s*$/.test(lines[i])) {
          buf.push(lines[i]);
          i++;
        }
        if (i < lines.length) i++;
        const codeText = buf.join('\n');
        const label = lang || 'text';
        out.push(
          `<div class="codeblock">` +
            `<div class="codeblock__bar"><span class="codeblock__lang">${escHtml(label)}</span>` +
            `<button class="codeblock__copy" type="button">Copy</button></div>` +
            `<pre><code>${escHtml(codeText)}</code></pre>` +
          `</div>`
        );
        continue;
      }

      // Headings
      const h = line.match(/^(#{1,4})\s+(.+?)\s*#*$/);
      if (h) {
        const level = h[1].length;
        const rawText = h[2].trim();
        const id = slugify(rawText, seenIds);
        if (level === 2 || level === 3) {
          headings.push({
            level,
            id,
            text: rawText.replace(/`([^`]+)`/g, '$1').replace(/\[([^\]]+)\]\([^)]+\)/g, '$1'),
          });
        }
        const anchor = level >= 2
          ? `<a class="h-anchor" href="#${currentSlug}/${id}" aria-label="Link to ${escHtml(rawText)}">#</a>`
          : '';
        out.push(`<h${level} id="${id}">${renderInline(rawText)}${anchor}</h${level}>`);
        i++;
        continue;
      }

      // Horizontal rule
      if (/^([-*_])\1{2,}\s*$/.test(line.trim())) {
        out.push('<hr>');
        i++;
        continue;
      }

      // Blockquote
      if (/^>\s?/.test(line)) {
        const q = [];
        while (i < lines.length && /^>\s?/.test(lines[i])) {
          q.push(lines[i].replace(/^>\s?/, ''));
          i++;
        }
        out.push(`<blockquote>${parseMarkdown(q.join('\n')).html}</blockquote>`);
        continue;
      }

      // Table
      if (line.includes('|') && i + 1 < lines.length && isTableSep(lines[i + 1])) {
        const headers = splitTableRow(line);
        i += 2;
        const rows = [];
        while (i < lines.length && lines[i].trim() && lines[i].includes('|')) {
          rows.push(splitTableRow(lines[i]));
          i++;
        }
        const thead = `<thead><tr>${headers.map((c) => `<th>${renderInline(c)}</th>`).join('')}</tr></thead>`;
        const tbody = `<tbody>${rows.map((r) => `<tr>${headers.map((_, ci) => `<td>${renderInline(r[ci] || '')}</td>`).join('')}</tr>`).join('')}</tbody>`;
        out.push(`<div class="table-wrap"><table>${thead}${tbody}</table></div>`);
        continue;
      }

      // Lists (unordered & ordered, supports continuation lines)
      const listMatch = line.match(/^(\s*)([-*+]|\d+\.)\s+(.*)$/);
      if (listMatch) {
        const ordered = /\d+\./.test(listMatch[2]);
        const tag = ordered ? 'ol' : 'ul';
        const items = [];
        while (i < lines.length) {
          const m = lines[i].match(/^(\s*)([-*+]|\d+\.)\s+(.*)$/);
          if (!m) break;
          const isOrd = /\d+\./.test(m[2]);
          if (isOrd !== ordered) break;
          const parts = [m[3]];
          i++;
          while (i < lines.length && lines[i].trim() && !/^\s*([-*+]|\d+\.)\s+/.test(lines[i]) && !isBlockStart(i)) {
            parts.push(lines[i].trim());
            i++;
          }
          items.push(`<li>${renderInline(parts.join(' '))}</li>`);
        }
        out.push(`<${tag}>${items.join('')}</${tag}>`);
        continue;
      }

      // Paragraph
      const para = [line.trim()];
      i++;
      while (i < lines.length && !isBlockStart(i)) {
        para.push(lines[i].trim());
        i++;
      }
      out.push(`<p>${renderInline(para.join(' '))}</p>`);
    }

    return { html: out.join('\n'), headings };
  }

  /* ───────── Build sidebar navigation ───────── */
  const navEl = $('#doc-nav');
  function buildNav() {
    const groups = [];
    const seen = new Map();
    for (const d of DOCS) {
      if (!seen.has(d.group)) {
        const g = { name: d.group, items: [] };
        seen.set(d.group, g);
        groups.push(g);
      }
      seen.get(d.group).items.push(d);
    }
    navEl.innerHTML = groups.map((g) => (
      `<section class="nav__group">` +
        `<h2 class="nav__group-title">${escHtml(g.name)}</h2>` +
        `<ul class="nav__list">` +
          g.items.map((d) => (
            `<li><a class="nav__link" href="#${d.slug}" data-slug="${d.slug}" style="--dot:${ACCENTS[d.accent] || 'var(--lime)'}">` +
              `<span class="nav__dot" aria-hidden="true"></span><span>${escHtml(d.title)}</span>` +
            `</a></li>`
          )).join('') +
        `</ul>` +
      `</section>`
    )).join('');
  }

  /* ───────── Render current doc page ───────── */
  let currentSlug = DOCS[0]?.slug || 'overview';
  let tocObserver = null;

  function parseRoute() {
    const raw = location.hash.replace(/^#/, '');
    if (!raw) return { slug: DOCS[0]?.slug || 'overview', anchor: '' };
    const slash = raw.indexOf('/');
    const slug = slash === -1 ? raw : raw.slice(0, slash);
    const anchor = slash === -1 ? '' : raw.slice(slash + 1);
    if (BY_SLUG[slug]) return { slug, anchor };
    return { slug: DOCS[0]?.slug || 'overview', anchor: '' };
  }

  function renderPage({ slug, anchor }) {
    const doc = BY_SLUG[slug] || DOCS[0];
    if (!doc) return;
    currentSlug = doc.slug;

    document.title = `${doc.title} · ASG Docs`;
    document.documentElement.style.setProperty('--page-accent', ACCENTS[doc.accent] || 'var(--lime)');

    $('#doc-group').textContent = doc.group;
    $('#doc-edit').href = `https://github.com/${REPO}/blob/main/docs/${doc.file}`;

    const { html, headings } = parseMarkdown(doc.md);
    const article = $('#doc-article');
    article.innerHTML = html;

    // Update active sidebar link
    $$('.nav__link', navEl).forEach((a) => {
      if (a.dataset.slug === doc.slug) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    });

    // Build TOC
    const tocList = $('#doc-toc');
    if (headings.length) {
      tocList.innerHTML = headings.map((h) => (
        `<li class="toc__item toc__item--h${h.level}">` +
          `<a href="#${doc.slug}/${h.id}" data-toc="${h.id}">${escHtml(h.text)}</a>` +
        `</li>`
      )).join('');
    } else {
      tocList.innerHTML = '';
    }
    setupTocSpy(headings);

    // Build prev/next pager
    const idx = DOCS.findIndex((d) => d.slug === doc.slug);
    const prev = idx > 0 ? DOCS[idx - 1] : null;
    const next = idx >= 0 && idx < DOCS.length - 1 ? DOCS[idx + 1] : null;
    $('#doc-pager').innerHTML =
      (prev ? `<a class="pager__card" href="#${prev.slug}"><span class="pager__dir">← Previous</span><span class="pager__title">${escHtml(prev.title)}</span></a>` : '') +
      (next ? `<a class="pager__card pager__card--next" href="#${next.slug}"><span class="pager__dir">Next →</span><span class="pager__title">${escHtml(next.title)}</span></a>` : '');

    // Wire copy buttons
    $$('.codeblock__copy', article).forEach((btn) => {
      btn.addEventListener('click', async () => {
        const text = btn.closest('.codeblock').querySelector('code').textContent;
        const ok = await copyText(text);
        btn.textContent = ok ? 'Copied' : 'Failed';
        btn.classList.toggle('is-done', ok);
        toast(ok ? 'Copied code block' : 'Could not copy');
        setTimeout(() => { btn.textContent = 'Copy'; btn.classList.remove('is-done'); }, 1600);
      });
    });

    closeMobileNav();

    if (anchor) {
      const target = document.getElementById(anchor);
      if (target) {
        target.scrollIntoView({ behavior: 'auto' });
        return;
      }
    }
    window.scrollTo({ top: 0, behavior: 'auto' });
  }

  function setupTocSpy(headings) {
    if (tocObserver) tocObserver.disconnect();
    if (!headings.length) return;
    const links = new Map($$('[data-toc]').map((a) => [a.dataset.toc, a]));
    const setActive = (id) => {
      links.forEach((a, k) => a.classList.toggle('is-active', k === id));
    };
    setActive(headings[0].id);
    tocObserver = new IntersectionObserver((entries) => {
      const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
      if (visible.length) setActive(visible[0].target.id);
    }, { rootMargin: '-80px 0px -65% 0px', threshold: 0 });
    headings.forEach((h) => {
      const el = document.getElementById(h.id);
      if (el) tocObserver.observe(el);
    });
  }

  /* ───────── Clipboard & Toast ───────── */
  const toastEl = $('.toast');
  let toastTimer;
  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toastEl.classList.remove('show'), 2000);
  }
  async function copyText(text) {
    try { await navigator.clipboard.writeText(text); return true; }
    catch (_) {
      const ta = Object.assign(document.createElement('textarea'), { value: text });
      document.body.append(ta); ta.select();
      const ok = document.execCommand('copy'); ta.remove(); return ok;
    }
  }

  /* ───────── Mobile navigation drawer ───────── */
  const sidebar = $('#sidebar');
  const scrim = $('.scrim');
  const navBtn = $('[data-toggle-nav]');
  function openMobileNav() {
    sidebar.classList.add('is-open');
    scrim.hidden = false;
    navBtn.setAttribute('aria-expanded', 'true');
  }
  function closeMobileNav() {
    sidebar.classList.remove('is-open');
    scrim.hidden = true;
    navBtn.setAttribute('aria-expanded', 'false');
  }
  navBtn.addEventListener('click', () => (sidebar.classList.contains('is-open') ? closeMobileNav() : openMobileNav()));
  scrim.addEventListener('click', closeMobileNav);

  /* ───────── Theme toggle ───────── */
  $('[data-toggle-theme]').addEventListener('click', () => {
    const root = document.documentElement;
    const isDark = root.dataset.theme === 'dark' || (!root.dataset.theme && matchMedia('(prefers-color-scheme: dark)').matches);
    const next = isDark ? 'light' : 'dark';
    root.dataset.theme = next;
    try { localStorage.setItem('asg-theme', next); } catch (_) {}
  });

  /* ───────── Full-text "/" search palette ───────── */
  // Build lightweight search index (pages + headings + plain text).
  const INDEX = DOCS.map((d) => {
    const plain = d.md
      .replace(/```[\s\S]*?```/g, (m) => m.replace(/```[^\n]*\n?/g, ' '))
      .replace(/[#*_`|[\]()>]/g, ' ')
      .replace(/\s+/g, ' ')
      .trim();
    const seen = new Map();
    const sections = [];
    for (const line of d.md.split('\n')) {
      const m = line.match(/^(#{2,3})\s+(.+?)\s*#*$/);
      if (m) {
        const text = m[2].replace(/`([^`]+)`/g, '$1').trim();
        sections.push({ text, id: slugify(m[2], seen) });
      }
    }
    return { doc: d, plain, plainLower: plain.toLowerCase(), sections };
  });

  const dlg = $('#search-dialog');
  const input = $('#search-input');
  const list = $('#search-list');
  let results = [], sel = 0;

  function searchDocs(q) {
    if (!q) {
      return DOCS.map((d) => ({
        title: d.title,
        tag: d.group,
        desc: d.summary,
        href: `#${d.slug}`,
      }));
    }
    const terms = q.toLowerCase().split(/\s+/).filter(Boolean);
    const hits = [];
    for (const entry of INDEX) {
      const d = entry.doc;
      const titleLower = d.title.toLowerCase();
      const summaryLower = d.summary.toLowerCase();

      // Match page title / summary
      let score = 0;
      if (terms.every((t) => titleLower.includes(t))) score += 100;
      else if (terms.every((t) => (titleLower + ' ' + summaryLower).includes(t))) score += 60;

      // Match headings
      for (const sec of entry.sections) {
        const sl = sec.text.toLowerCase();
        if (terms.every((t) => sl.includes(t))) {
          hits.push({
            score: 85,
            title: `${d.title} › ${sec.text}`,
            tag: 'Section',
            desc: d.summary,
            href: `#${d.slug}/${sec.id}`,
          });
        }
      }

      // Match body text
      if (terms.every((t) => entry.plainLower.includes(t))) {
        const pos = entry.plainLower.indexOf(terms[0]);
        const start = Math.max(0, pos - 40);
        const snippet = (start > 0 ? '…' : '') + entry.plain.slice(start, start + 140) + (start + 140 < entry.plain.length ? '…' : '');
        hits.push({
          score: score || 40,
          title: d.title,
          tag: d.group,
          desc: score >= 60 ? d.summary : snippet,
          href: `#${d.slug}`,
        });
      }
    }
    hits.sort((a, b) => b.score - a.score);
    return hits.slice(0, 14);
  }

  function renderSearch() {
    const q = input.value.trim();
    results = searchDocs(q);
    sel = Math.min(sel, Math.max(0, results.length - 1));
    list.innerHTML = results.length
      ? results.map((r, i) => (
          `<li class="palette__item" role="option" id="sopt-${i}" aria-selected="${i === sel}">` +
            `<div class="palette__item-top"><b>${escHtml(r.title)}</b><span class="palette__item-tag">${escHtml(r.tag)}</span></div>` +
            `<div class="palette__item-desc">${escHtml(r.desc)}</div>` +
          `</li>`
        )).join('')
      : `<li class="palette__empty mono">No docs match "${escHtml(q)}". Try "loop", "permissions", or "ollama".</li>`;
    input.setAttribute('aria-activedescendant', results.length ? `sopt-${sel}` : '');
    if (results.length) list.children[sel].scrollIntoView({ block: 'nearest' });
  }

  function openSearch() {
    if (dlg.open) return;
    input.value = ''; sel = 0; renderSearch();
    dlg.showModal(); input.focus();
  }

  function goResult(i) {
    const r = results[i];
    if (!r) return;
    dlg.close();
    if (location.hash === r.href) renderPage(parseRoute());
    else location.hash = r.href;
  }

  $('[data-open-search]').addEventListener('click', openSearch);
  $('[data-close-search]').addEventListener('click', () => dlg.close());
  dlg.addEventListener('click', (e) => { if (e.target === dlg) dlg.close(); });
  input.addEventListener('input', () => { sel = 0; renderSearch(); });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); sel = (sel + 1) % Math.max(1, results.length); renderSearch(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); sel = (sel - 1 + results.length) % Math.max(1, results.length); renderSearch(); }
    else if (e.key === 'Enter') { e.preventDefault(); goResult(sel); }
  });
  list.addEventListener('pointermove', (e) => {
    const li = e.target.closest('.palette__item'); if (!li) return;
    const i = [...list.children].indexOf(li);
    if (i !== sel) { sel = i; renderSearch(); }
  });
  list.addEventListener('click', (e) => {
    const li = e.target.closest('.palette__item'); if (!li) return;
    goResult([...list.children].indexOf(li));
  });
  document.addEventListener('keydown', (e) => {
    const typing = /input|textarea|select/i.test(document.activeElement.tagName) || document.activeElement.isContentEditable;
    if ((e.key === '/' && !typing) || (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey))) {
      e.preventDefault();
      openSearch();
    } else if (e.key === 'Escape' && sidebar.classList.contains('is-open')) {
      closeMobileNav();
    }
  });

  /* ───────── Init ───────── */
  buildNav();
  window.addEventListener('hashchange', () => renderPage(parseRoute()));
  renderPage(parseRoute());
})();
