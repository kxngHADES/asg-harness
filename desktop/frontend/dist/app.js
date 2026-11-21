"use strict";
/* ASG desktop UI. Plain JS: state in S, views rebuilt with template strings,
   streaming turns patched in place. Backend: window.go.main.App.* (Wails). */

// ---------- helpers ----------
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const api = () => window.go.main.App;
const rt = () => window.runtime;

async function call(name, ...args) {
  try {
    return await api()[name](...args);
  } catch (e) {
    toast(errText(e), "err");
    throw e;
  }
}
const errText = (e) => (typeof e === "string" ? e : e?.message || String(e));

function rel(iso) {
  if (!iso || iso.startsWith("0001")) return "";
  const d = new Date(iso), now = new Date();
  const s = (now - d) / 1000;
  if (s < 0) {
    const f = -s;
    if (f < 3600) return `in ${Math.max(1, Math.round(f / 60))} min`;
    if (f < 86400) return `in ${Math.round(f / 3600)} h`;
    return d.toLocaleDateString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" });
  }
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  if (s < 86400 * 7) return `${Math.floor(s / 86400)}d ago`;
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: d.getFullYear() === now.getFullYear() ? undefined : "numeric" });
}
function dayGroup(iso) {
  const d = new Date(iso), now = new Date();
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = (start - new Date(d.getFullYear(), d.getMonth(), d.getDate())) / 86400000;
  if (diff <= 0) return "Today";
  if (diff === 1) return "Yesterday";
  if (diff < 7) return "Previous 7 days";
  if (diff < 30) return "Previous 30 days";
  return d.toLocaleDateString(undefined, { month: "long", year: "numeric" });
}
const bytes = (n) => (n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : n < 1073741824 ? `${(n / 1048576).toFixed(1)} MB` : `${(n / 1073741824).toFixed(2)} GB`);
const base = (p) => String(p || "").split(/[\\/]/).filter(Boolean).pop() || p;
const b64 = (s) => btoa(unescape(encodeURIComponent(s)));
function shortPath(p) {
  if (!p) return "";
  const home = S.boot?.home;
  if (home && p.toLowerCase().startsWith(home.toLowerCase())) return "~" + p.slice(home.length);
  return p;
}

// ---------- state ----------
const S = {
  boot: null,
  sessions: [],
  projects: [],
  model: null,
  view: { name: "home" },
  history: [],
  live: {},        // session id -> { items, running, status, review, started }
  cur: null,       // SessionView of the open session
  panel: null,     // { type: "files"|"artifact", ... }
  drafts: {},      // composer key -> { text, images, skill }
  home: { kind: "chat", projectId: "", workdir: "", mode: "build" },
  collapsed: false,
  filter: { chats: "all", q: "", arts: "all", artQ: "" },
  artCache: {},
  skills: null,
  custTab: "model",
  setTab: "storage",
};

// ---------- boot ----------
window.addEventListener("DOMContentLoaded", async () => {
  try { S.collapsed = localStorage.getItem("asg.collapsed") === "1"; } catch {}
  await waitForBridge();
  const boot = await api().Bootstrap();
  S.boot = boot;
  document.documentElement.dataset.os = boot.os || "";
  S.sessions = boot.sessions || [];
  S.projects = boot.projects || [];
  S.model = boot.model;
  $("#hl").textContent = boot.highlight_css || "";
  applyTheme(boot.settings.theme);
  if (boot.data_fallback) toast("Your data folder isn't available (drive unplugged?). Using the default folder for now.", "err");

  rt().EventsOn("asg:turn", onTurn);
  rt().EventsOn("asg:sessions", debounce(refreshSessions, 120));
  rt().EventsOn("asg:projects", async () => { S.projects = await api().ListProjects(); renderSidebar(); if (["projects", "project"].includes(S.view.name)) renderView(); });
  rt().EventsOn("asg:artifacts", () => { if (S.view.name === "artifacts") renderView(); });
  rt().EventsOn("asg:schedules", () => { if (S.view.name === "schedules") renderView(); });
  rt().EventsOn("asg:model", (m) => { S.model = m; $$(".chip.model .ellip").forEach((el) => (el.textContent = modelLabel())); });
  rt().EventsOn("asg:toast", (t) => toast(t.text, "", t.session ? { label: "Open", fn: () => openSession(t.session) } : null));

  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => applyTheme(S.boot.settings.theme));
  document.addEventListener("keydown", onKey);
  document.addEventListener("click", onGlobalClick);
  $("#filepick").addEventListener("change", onFilePick);
  setInterval(tickWorking, 1000);
  render();
});

function waitForBridge() {
  return new Promise((res) => {
    const t = () => (window.go?.main?.App && window.runtime ? res() : setTimeout(t, 20));
    t();
  });
}

function debounce(fn, ms) {
  let t;
  return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}

function applyTheme(pref) {
  const dark = pref === "dark" || (pref !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}

async function refreshSessions() {
  S.sessions = await api().ListSessions();
  renderSidebar();
  if (["chats", "code", "project", "home"].includes(S.view.name)) renderView({ keepScroll: true });
  if (S.view.name === "session" && S.cur) {
    const m = S.sessions.find((x) => x.id === S.cur.meta.id);
    if (m) { S.cur.meta = m; renderTitlebar(); const h = $("#sess-head"); if (h) h.outerHTML = sessHeadHTML(); }
  }
}

// ---------- navigation ----------
function nav(name, extra = {}) {
  S.view = { name, ...extra };
  if (name !== "session") { S.cur = null; S.panel = null; }
  render();
}

async function openSession(id, opts = {}) {
  let v;
  try { v = await api().OpenSession(id); } catch (e) { toast(errText(e), "err"); return; }
  S.cur = v;
  S.view = { name: "session", id };
  const live = S.live[id];
  if (live && live.running) {
    // keep the live transcript; it has everything streamed so far
  } else {
    S.live[id] = { items: v.items, running: v.running, status: "", review: null, started: 0 };
  }
  if (!opts.keepPanel) S.panel = null;
  if (opts.panel) S.panel = opts.panel;
  render();
  scrollBottom(true);
}

function render() {
  const app = $("#app");
  app.classList.toggle("collapsed", S.collapsed);
  renderSidebar();
  renderTitlebar();
  renderView();
}

// ---------- sidebar ----------
function renderSidebar() {
  const sid = S.view.name === "session" ? S.view.id : "";
  const pinned = S.sessions.filter((s) => s.pinned);
  const recent = S.sessions.filter((s) => !s.pinned).slice(0, 40);
  const count = (k) => S.sessions.filter((s) => s.kind === k).length;
  const navItem = (name, ic, label, n) =>
    `<button class="nav-item ${S.view.name === name || (name === "projects" && S.view.name === "project") ? "on" : ""}" data-nav="${name}">${icon(ic)}<span>${label}</span>${n ? `<span class="count">${n}</span>` : ""}</button>`;
  const row = (s) => `
    <div class="recent-item ${s.id === sid ? "on" : ""}" data-open="${s.id}" role="button" tabindex="0" title="${esc(s.title || "Untitled")}">
      ${kindChip(s.kind)}
      <span class="t">${esc(s.title || "New " + (KINDS[s.kind]?.label || "chat").toLowerCase())}</span>
      ${S.live[s.id]?.running ? '<span class="live-dot" title="Working"></span>' : ""}
      <button class="more" data-sessmenu="${s.id}" title="More">${icon("more", "sm")}</button>
    </div>`;
  const initial = (S.boot?.user || "A").trim()[0]?.toUpperCase() || "A";
  $("#sidebar").innerHTML = `
    <div class="side-top">
      <div class="brand"><span class="brand-mark">AS</span>ASG</div>
      <span class="grow"></span>
      <button class="icon-btn" data-act="palette" title="Search (Ctrl+K)">${icon("search", "sm")}</button>
      <button class="icon-btn" data-act="toggle-side" title="Hide sidebar (Ctrl+B)">${icon("sidebar", "sm")}</button>
    </div>
    <div class="side-scroll">
      <button class="new-btn" data-act="new"><span class="plus">${icon("plus")}</span>New chat<kbd>Ctrl N</kbd></button>
      <nav class="nav">
        ${navItem("chats", "chats", "Chats", "")}
        ${navItem("projects", "folder", "Projects", S.projects.length || "")}
        ${navItem("code", "terminal", "Code", count("code") || "")}
        ${navItem("artifacts", "box", "Artifacts", "")}
        ${navItem("schedules", "clock", "Schedules", "")}
        ${navItem("customise", "sliders", "Customise", "")}
      </nav>
      ${pinned.length ? `<div class="side-label">Pinned</div><div class="recent">${pinned.map(row).join("")}</div>` : ""}
      <div class="side-label">Recents</div>
      <div class="recent">${recent.length ? recent.map(row).join("") : `<div style="padding:6px 10px;color:var(--faint);font-size:12.5px">Your chats will show up here.</div>`}</div>
    </div>
    <div class="side-foot">
      <button class="me" data-nav="settings">
        <span class="avatar">${esc(initial)}</span>
        <span class="who"><b>${esc(S.boot?.user || "You")}</b><span>${esc(shortPath(S.boot?.data_dir || ""))}</span></span>
      </button>
      <button class="icon-btn" data-nav="settings" title="Settings">${icon("settings", "sm")}</button>
    </div>`;
}

// ---------- titlebar ----------
function renderTitlebar() {
  let crumbs = "";
  const v = S.view;
  const titles = { chats: "Chats", projects: "Projects", code: "Code", artifacts: "Artifacts", schedules: "Schedules", customise: "Customise", settings: "Settings" };
  if (v.name === "session" && S.cur) {
    const m = S.cur.meta;
    const proj = m.project_id ? S.projects.find((p) => p.id === m.project_id) : null;
    crumbs = `${kindChip(m.kind)}${proj ? `<span>${esc(proj.name)}</span><span>/</span>` : ""}<b>${esc(m.title || "New " + KINDS[m.kind].label.toLowerCase())}</b>`;
  } else if (v.name === "project") {
    const p = S.projects.find((x) => x.id === v.id);
    crumbs = `<span>Projects</span><span>/</span><b>${esc(p?.name || "")}</b>`;
  } else if (titles[v.name]) crumbs = `<b>${titles[v.name]}</b>`;
  const sessionBtns = v.name === "session" && S.cur ? `
      <button class="icon-btn ${S.panel?.type === "files" ? "on" : ""}" data-act="toggle-files" title="Files and artifacts">${icon("panel", "sm")}</button>
      <button class="icon-btn" data-sessmenu="${S.cur.meta.id}" title="More">${icon("more", "sm")}</button>` : "";
  $("#titlebar").innerHTML = `
    ${S.collapsed ? `<button class="icon-btn" data-act="toggle-side" title="Show sidebar (Ctrl+B)">${icon("sidebar", "sm")}</button><button class="icon-btn" data-act="new" title="New chat">${icon("plus", "sm")}</button>` : ""}
    <div class="crumbs">${crumbs}</div>
    <span class="grow"></span>
    ${sessionBtns}
    <div class="winctl">
      <button data-win="min" title="Minimise">${icon("minus")}</button>
      <button data-win="max" title="Maximise">${icon("max")}</button>
      <button data-win="close" class="close" title="Close">${icon("x")}</button>
    </div>`;
  const t = S.cur?.meta?.title;
  try { rt().WindowSetTitle(v.name === "session" && t ? `${t} — ASG` : "ASG"); } catch {}
}

// ---------- views ----------
function renderView(opts = {}) {
  const el = $("#view");
  const prevScroll = opts.keepScroll ? $(".scroll", el)?.scrollTop : 0;
  const v = S.view.name;
  const fn = { home: viewHome, session: viewSession, chats: viewChats, projects: viewProjects, project: viewProject, code: viewCode, artifacts: viewArtifacts, schedules: viewSchedules, customise: viewCustomise, settings: viewSettings }[v] || viewHome;
  const out = fn(el);
  if (typeof out === "string") el.innerHTML = out;
  if (opts.keepScroll && $(".scroll", el)) $(".scroll", el).scrollTop = prevScroll;
  bindComposers();
  if (v === "project") fillProjectArtifacts(S.view.id);
}

function greeting() {
  const h = new Date().getHours();
  const part = h < 5 ? "Up late" : h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
  const name = (S.boot?.user || "").split(/[ ._]/)[0];
  return `${part}${name ? `, <em>${esc(name)}</em>` : ""}`;
}

function viewHome() {
  const k = S.home.kind;
  const hints = {
    chat: "A private sandbox folder is created for this chat. Files it makes are saved as artifacts.",
    code: S.home.workdir ? `Works in ${esc(shortPath(S.home.workdir))}` : "Pick the folder to work in. Coding sessions can be resumed any time from Code.",
    artifact: "Describe what to make — a page, document, diagram or dataset. It's saved to Artifacts.",
  };
  const sugg = {
    chat: [["message", "Explain a concept", "Walk me through how DNS resolution works, step by step."], ["file", "Draft something", "Draft a friendly project update email for my team."], ["search", "Research", "Compare SQLite, Postgres and DuckDB for a local analytics tool."]],
    code: [["terminal", "Find bugs", "Review this project for bugs and risky code. Start with the most important."], ["list", "Explain the codebase", "Give me a tour of this codebase: structure, entry points, how data flows."], ["check", "Add tests", "Find the least-tested important code and add tests for it."]],
    artifact: [["globe", "A web page", "Make a one-page landing site for a small coffee roaster, warm and modern."], ["shapes", "A diagram", "Draw an SVG architecture diagram of a typical web app with a queue and workers."], ["table", "A dataset", "Create a CSV of the 30 largest cities in Africa with country and population."]],
  }[k];
  const recent = S.sessions.slice(0, 4);
  return `<div class="scroll"><div class="home">
    <div class="crab">${S.boot?.crab_idle || ""}</div>
    <h1 class="greet">${greeting()}</h1>
    <div class="composer-wrap">${composerHTML("home")}</div>
    <div class="kind-picker">
      ${["chat", "code", "artifact"].map((x) => `<button class="kind-pick ${k === x ? "on" : ""}" data-homekind="${x}">${kindChip(x)}${KINDS[x].label}</button>`).join("")}
    </div>
    <div class="home-hint">${hints[k]}</div>
    <div class="suggest">${sugg.map(([ic, t, p]) => `<button data-suggest="${esc(p)}"><span class="kind k-${k}">${icon(ic)}</span><div><b>${t}</b><span>${esc(p)}</span></div></button>`).join("")}</div>
    ${recent.length ? `<div class="recent-strip"><h3>Jump back in</h3><div class="list">${recent.map(sessionRow).join("")}</div></div>` : ""}
  </div></div>`;
}

function sessionRow(s) {
  const proj = s.project_id ? S.projects.find((p) => p.id === s.project_id) : null;
  const sub = s.kind === "code" && s.workdir ? `${shortPath(s.workdir)}${s.branch ? " · " + s.branch : ""}` : s.preview || (s.last_error ? "⚠ " + s.last_error : "No messages yet");
  return `<button class="row-item" data-open="${s.id}">
    ${kindChip(s.kind, "lg")}
    <div class="t"><b>${s.pinned ? icon("pin", "xs") + " " : ""}${esc(s.title || "Untitled")}</b><span>${esc(sub)}</span></div>
    <div class="side">
      ${proj ? `<span class="tag pc-${proj.color}" style="color:var(--pc);background:color-mix(in srgb,var(--pc) 13%,transparent)">${icon("folder")}${esc(proj.name)}</span>` : ""}
      ${S.live[s.id]?.running ? '<span class="live-dot" style="width:7px;height:7px;border-radius:50%;background:var(--brand)"></span>' : ""}
      <span class="when">${rel(s.updated)}</span>
    </div>
  </button>`;
}

// ---------- composer ----------
function draft(key) {
  return (S.drafts[key] ||= { text: "", images: [], skill: "" });
}
function modelLabel() {
  const m = S.model;
  return m ? `${m.model || "no model"}` : "Model";
}
const MODES = { build: ["hammer", "Build", "Makes changes, behind permission gates"], plan: ["list", "Plan", "Reads and plans; changes nothing"], ask: ["eye", "Ask", "Answers questions, read-only"] };

function composerHTML(key) {
  const d = draft(key);
  let running = false, mode = "build", placeholder = "How can I help?";
  if (key === "home") {
    mode = S.home.mode;
    placeholder = { chat: "How can I help today?", code: "What should we build or fix?", artifact: "Describe the artifact to make…" }[S.home.kind];
  } else if (key.startsWith("project:")) {
    placeholder = "Start a chat in this project…";
    mode = S.home.mode;
  } else {
    const live = S.live[key];
    running = !!live?.running;
    mode = S.cur?.meta?.mode || "build";
    placeholder = running ? "Working… you can stop it with the button" : "Reply…";
  }
  const [mi, ml] = MODES[mode] || MODES.build;
  let ctxChips = "";
  if (key === "home") {
    const proj = S.projects.find((p) => p.id === S.home.projectId);
    if (S.home.kind === "code") {
      ctxChips += `<button class="chip ${S.home.workdir ? "set" : ""}" data-act="home-folder" title="${esc(S.home.workdir || "Choose a folder")}">${icon("folderOpen")}<span class="ellip">${esc(S.home.workdir ? base(S.home.workdir) : "Choose folder")}</span>${icon("chevDown", "xs")}</button>`;
    } else {
      ctxChips += `<button class="chip ${proj ? "set" : ""}" data-act="home-project">${proj ? `<span class="pdot pc-${proj.color}"></span>` : icon("folder")}<span class="ellip">${esc(proj ? proj.name : "No project")}</span>${icon("chevDown", "xs")}</button>`;
    }
  }
  return `<div class="composer" data-composer="${esc(key)}">
    ${d.images.length ? `<div class="thumbs">${d.images.map((im, i) => `<div class="thumb"><img src="data:${im.media_type};base64,${im.data}"><button data-rmimg="${i}" title="Remove">${icon("x")}</button></div>`).join("")}</div>` : ""}
    <textarea rows="1" placeholder="${esc(placeholder)}" spellcheck="true">${esc(d.text)}</textarea>
    <div class="row">
      <button class="chip icon" data-act="attach" title="Attach images (or paste)">${icon("clip")}</button>
      <button class="chip" data-act="mode" title="${esc(MODES[mode]?.[2] || "")}">${icon(mi)}<span>${ml}</span>${icon("chevDown", "xs")}</button>
      ${ctxChips}
      ${d.skill ? `<button class="chip skill" data-act="clear-skill" title="Remove skill">${icon("zap")}<span class="ellip">${esc(d.skill)}</span>${icon("x", "xs")}</button>` : ""}
      <span class="grow"></span>
      <button class="chip model" data-act="model" title="Model">${icon("cpu")}<span class="ellip">${esc(modelLabel())}</span></button>
      ${running ? `<button class="send stop" data-act="stop" title="Stop">${icon("stop", "fill")}</button>` : `<button class="send" data-act="send" title="Send (Enter)" ${d.text.trim() || d.images.length ? "" : "disabled"}>${icon("arrowUp")}</button>`}
    </div>
  </div>`;
}

function bindComposers() {
  $$(".composer:not([data-bound])").forEach((c) => {
    c.dataset.bound = "1";
    const key = c.dataset.composer;
    const ta = $("textarea", c);
    const d = draft(key);
    const fit = () => { ta.style.height = "auto"; ta.style.height = Math.min(ta.scrollHeight, 260) + "px"; };
    fit();
    ta.addEventListener("input", () => {
      d.text = ta.value;
      fit();
      const send = $(".send:not(.stop)", c);
      if (send) send.disabled = !(d.text.trim() || d.images.length);
      slashMenu(c, key);
    });
    ta.addEventListener("keydown", (e) => {
      const menu = $(".slash", c);
      if (menu && ["ArrowDown", "ArrowUp", "Enter", "Tab", "Escape"].includes(e.key)) {
        e.preventDefault();
        const opts = $$(".opt", menu);
        let i = opts.findIndex((o) => o.classList.contains("on"));
        if (e.key === "Escape") return menu.remove();
        if (e.key === "ArrowDown") i = (i + 1) % opts.length;
        else if (e.key === "ArrowUp") i = (i - 1 + opts.length) % opts.length;
        else { opts[Math.max(i, 0)]?.click(); return; }
        opts.forEach((o, j) => o.classList.toggle("on", j === i));
        return;
      }
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        submit(key);
      }
    });
    ta.addEventListener("paste", (e) => {
      const files = [...(e.clipboardData?.files || [])].filter((f) => f.type.startsWith("image/"));
      if (files.length) { e.preventDefault(); addImages(key, files); }
    });
    c.addEventListener("dragover", (e) => e.preventDefault());
    c.addEventListener("drop", (e) => {
      const files = [...(e.dataTransfer?.files || [])].filter((f) => f.type.startsWith("image/"));
      if (files.length) { e.preventDefault(); addImages(key, files); }
    });
  });
  const main = $(".composer textarea");
  if (main && !$("#overlay").innerHTML && (S.view.name === "home" || S.view.name === "session")) {
    main.focus();
    main.setSelectionRange(main.value.length, main.value.length);
  }
}

async function slashMenu(c, key) {
  const ta = $("textarea", c);
  const m = /^\/([\w.-]*)$/.exec(ta.value);
  $(".slash", c)?.remove();
  if (!m) return;
  S.skills ||= await api().ListSkills();
  const q = m[1].toLowerCase();
  const list = S.skills.filter((s) => s.name.toLowerCase().includes(q)).slice(0, 12);
  const box = document.createElement("div");
  box.className = "slash";
  box.innerHTML = list.length
    ? `<div class="menu-lbl" style="font-size:11px;font-weight:600;color:var(--faint);padding:4px 10px">Skills</div>` +
      list.map((s, i) => `<button class="opt ${i === 0 ? "on" : ""}" data-skill="${esc(s.name)}">${icon("zap", "sm")}<div><b>/${esc(s.name)}</b><span>${esc(s.description)}</span></div></button>`).join("")
    : `<div style="padding:10px;color:var(--muted);font-size:12.5px">No skills match. Add skills in Customise → Skills.</div>`;
  c.appendChild(box);
  box.addEventListener("mousedown", (e) => e.preventDefault());
  $$(".opt", box).forEach((o) => o.addEventListener("click", () => {
    const d = draft(key);
    d.skill = o.dataset.skill;
    d.text = "";
    rerenderComposer(key);
  }));
}

function rerenderComposer(key) {
  const c = $(`.composer[data-composer="${CSS.escape(key)}"]`);
  if (!c) return;
  c.outerHTML = composerHTML(key);
  bindComposers();
}

function addImages(key, files) {
  const d = draft(key);
  Promise.all(files.map((f) => new Promise((res) => {
    const r = new FileReader();
    r.onload = () => res({ media_type: f.type, data: String(r.result).split(",")[1] });
    r.readAsDataURL(f);
  }))).then((imgs) => { d.images.push(...imgs); rerenderComposer(key); });
}

let pickKey = null;
function onFilePick(e) {
  if (pickKey) addImages(pickKey, [...e.target.files]);
  e.target.value = "";
}

async function submit(key) {
  const d = draft(key);
  const text = d.text.trim();
  if (!text && !d.images.length) return;
  let sid = key;
  if (key === "home" || key.startsWith("project:")) {
    const projectId = key === "home" ? (S.home.kind === "code" ? "" : S.home.projectId) : key.slice(8);
    const kind = key === "home" ? S.home.kind : "project";
    if (kind === "code" && !S.home.workdir) {
      const dir = await pickFolder("Choose the folder to work in");
      if (!dir) return;
      S.home.workdir = dir;
    }
    let meta;
    try {
      meta = await api().NewSession({ kind, project_id: projectId, workdir: kind === "code" ? S.home.workdir : "", mode: S.home.mode, title: "" });
    } catch (e) { toast(errText(e), "err"); return; }
    sid = meta.id;
    S.sessions.unshift(meta);
    S.drafts[sid] = { text: "", images: [], skill: "" };
    const msg = { text, images: d.images, skill: d.skill };
    S.drafts[key] = { text: "", images: [], skill: "" };
    await openSession(sid);
    return send(sid, msg);
  }
  const msg = { text, images: d.images, skill: d.skill };
  S.drafts[key] = { text: "", images: [], skill: "" };
  return send(sid, msg);
}

async function send(sid, msg) {
  const live = (S.live[sid] ||= { items: [], running: false });
  const shown = msg.skill ? `/${msg.skill} ${msg.text}` : msg.text;
  live.items.push({ type: "user", text: shown, images: msg.images.map((i) => `data:${i.media_type};base64,${i.data}`) });
  live.running = true;
  live.started = Date.now();
  live.status = "";
  live.review = null;
  if (S.view.name === "session" && S.view.id === sid) { renderSessionBody(); scrollBottom(true); }
  renderSidebar();
  try {
    await api().Send(sid, msg.text, msg.images, msg.skill || "");
  } catch (e) {
    live.running = false;
    live.items.push({ type: "notice", error: true, text: errText(e) });
    if (S.view.name === "session" && S.view.id === sid) renderSessionBody();
    renderSidebar();
  }
}

// ---------- live turn events ----------
function onTurn(ev) {
  const sid = ev.session;
  const live = (S.live[sid] ||= { items: [], running: true, started: Date.now() });
  const items = live.items;
  const last = items[items.length - 1];
  const visible = S.view.name === "session" && S.view.id === sid;
  let full = false;
  switch (ev.type) {
    case "text":
      if (last && last.type === "assistant" && last.pending) last.text += ev.text;
      else { items.push({ type: "assistant", text: ev.text, pending: true }); full = true; }
      if (visible) streamPatch(sid);
      live.status = "";
      break;
    case "tool_start":
      if (last?.pending) { last.pending = false; last.html = null; }
      items.push({ type: "tool", tool: { id: ev.id, name: ev.name, summary: ev.summary, args: ev.args, done: false } });
      full = true;
      break;
    case "tool_end": {
      const t = [...items].reverse().find((i) => i.type === "tool" && i.tool.id === ev.id && !i.tool.done);
      if (t) Object.assign(t.tool, { result: ev.result, error: ev.error, done: true });
      else items.push({ type: "tool", tool: { id: ev.id, name: ev.name, summary: "", result: ev.result, error: ev.error, done: true } });
      full = true;
      break;
    }
    case "artifact":
      items.push({ type: "artifact", artifact: ev.artifact });
      if (S.cur && S.cur.meta.id === sid) {
        S.cur.artifacts = [...(S.cur.artifacts || []).filter((a) => a.id !== ev.artifact.id), ev.artifact];
        if (!S.panel || S.panel.type === "artifact") openArtifactPanel(ev.artifact.id);
      }
      full = true;
      break;
    case "notice":
      items.push({ type: "notice", text: ev.text });
      full = true;
      break;
    case "status":
      live.status = ev.text;
      break;
    case "review":
      live.review = ev;
      live.status = `Reviewing ${ev.pass}/${ev.total} · ${String(ev.lens).split(" — ")[0]}`;
      break;
    case "approval":
      items.push({ type: "approval", ...ev, state: "waiting" });
      full = true;
      notifyAttention(sid, "needs your permission");
      break;
    case "approval_done": {
      const a = items.find((i) => i.type === "approval" && i.id === ev.id);
      if (a && a.state === "waiting") a.state = "done";
      full = true;
      break;
    }
    case "ask":
      items.push({ type: "ask", ...ev, state: "waiting" });
      full = true;
      notifyAttention(sid, "has a question");
      break;
    case "ask_done": {
      const a = items.find((i) => i.type === "ask" && i.id === ev.id);
      if (a) { a.state = "done"; if (ev.answer !== undefined) a.answer = ev.answer; }
      full = true;
      break;
    }
    case "error":
      live.running = false;
      if (last?.pending) last.pending = false;
      items.push({ type: "notice", error: true, text: ev.text });
      full = true;
      renderSidebar();
      break;
    case "done":
      live.running = false;
      live.status = "";
      live.lastStats = ev;
      renderSidebar();
      if (visible) { reloadCurrent(); return; }
      openSessionQuiet(sid);
      return;
  }
  if (visible && full) { renderSessionBody(); scrollBottom(); }
}

function notifyAttention(sid, what) {
  if (S.view.name === "session" && S.view.id === sid) return;
  const s = S.sessions.find((x) => x.id === sid);
  toast(`“${s?.title || "A session"}” ${what}`, "", { label: "Open", fn: () => openSession(sid) });
}

async function reloadCurrent() {
  const sid = S.view.id;
  try {
    const v = await api().OpenSession(sid);
    if (S.view.name !== "session" || S.view.id !== sid) return;
    S.cur = v;
    S.live[sid] = { items: v.items, running: v.running, status: "", started: 0, lastStats: S.live[sid]?.lastStats };
    renderTitlebar();
    renderSessionBody();
    if (S.panel?.type === "files") renderPanel();
    scrollBottom();
  } catch {}
}
async function openSessionQuiet(sid) {
  try {
    const v = await api().OpenSession(sid);
    S.live[sid] = { items: v.items, running: v.running, status: "", started: 0 };
  } catch {}
}

let streamTimer = null;
function streamPatch(sid) {
  if (streamTimer) return;
  streamTimer = setTimeout(async () => {
    streamTimer = null;
    const live = S.live[sid];
    const item = live?.items[live.items.length - 1];
    const el = $("#stream");
    if (!item || item.type !== "assistant" || !item.pending) return;
    if (!el) { renderSessionBody(); scrollBottom(); return; }
    const html = await api().RenderMarkdown(item.text);
    item.html = html;
    const cur = $("#stream");
    if (cur) cur.innerHTML = html;
    scrollBottom();
  }, 90);
}

function scrollBottom(force) {
  const sc = $("#tscroll");
  if (!sc) return;
  const near = sc.scrollHeight - sc.scrollTop - sc.clientHeight < 180;
  if (force || near) requestAnimationFrame(() => (sc.scrollTop = sc.scrollHeight));
}

function tickWorking() {
  if (S.view.name !== "session") return;
  const live = S.live[S.view.id];
  const el = $(".working .el");
  if (live?.running && el && live.started) el.textContent = fmtElapsed((Date.now() - live.started) / 1000);
}
const fmtElapsed = (s) => (s < 60 ? `${Math.floor(s)}s` : `${Math.floor(s / 60)}m ${String(Math.floor(s % 60)).padStart(2, "0")}s`);

// ---------- session view ----------
function viewSession(el) {
  if (!S.cur) return `<div class="pane"></div>`;
  el.innerHTML = `<div class="session">
    <div class="chat-col">
      <div class="scroll" id="tscroll"><div class="transcript" id="transcript"></div></div>
      <div class="dock"><div class="composer-wrap">${composerHTML(S.cur.meta.id)}</div><div class="composer-foot">${footNote()}</div></div>
    </div>
    <div id="panel-slot" style="display:contents"></div>
  </div>`;
  renderSessionBody(true);
  renderPanel();
}

function footNote() {
  const m = S.cur.meta;
  if (S.cur.sandbox) return `${icon("shield", "xs")} Sandbox · files stay in this chat's own folder`;
  if (m.kind === "code") return `Working in ${esc(shortPath(S.cur.workdir))} · tools that change things ask first`;
  return `Working in ${esc(shortPath(S.cur.workdir))}`;
}

function sessHeadHTML() {
  const m = S.cur.meta;
  const proj = S.cur.project || (m.project_id ? S.projects.find((p) => p.id === m.project_id) : null);
  const bits = [`<span>${KINDS[m.kind]?.label || "Chat"}</span>`];
  if (proj) bits.push(`<span class="pc-${proj.color}" style="color:var(--pc)">${icon("folder")}${esc(proj.name)}</span>`);
  if (m.kind === "code" || !S.cur.sandbox) bits.push(`<span class="path" data-reveal="${esc(S.cur.workdir)}" title="Open folder">${icon("folderOpen")}${esc(shortPath(S.cur.workdir))}</span>`);
  if (m.branch) bits.push(`<span class="branch">${icon("branch")}${esc(m.branch)}</span>`);
  bits.push(`<span>${icon(MODES[m.mode]?.[0] || "hammer")}${MODES[m.mode]?.[1] || "Build"}</span>`);
  if (m.created) bits.push(`<span>${icon("clock")}${new Date(m.created).toLocaleDateString(undefined, { month: "short", day: "numeric" })}</span>`);
  return `<div class="sess-head" id="sess-head">${kindChip(m.kind, "lg")}<div style="min-width:0"><h2 data-rename="${m.id}" title="Double-click to rename">${esc(m.title || "New " + (KINDS[m.kind]?.label || "chat").toLowerCase())}</h2><div class="meta">${bits.join("")}</div></div></div>`;
}

function renderSessionBody() {
  const tr = $("#transcript");
  if (!tr || !S.cur) return;
  const sid = S.cur.meta.id;
  const live = S.live[sid] || { items: [] };
  const items = live.items || [];
  let html = sessHeadHTML();
  if (!items.length) {
    const m = S.cur.meta;
    const blurb = {
      chat: "Ask anything. This chat has its own sandbox folder, so anything it creates is kept here and in Artifacts.",
      code: "Describe the change, bug or question. Switch to Plan to think it through first, or Ask for read-only answers.",
      artifact: "Describe what to make. The file appears in the panel on the right and in your Artifacts library.",
      project: "This chat follows the project's instructions and works in the project's folder.",
      scheduled: "This run was started by a schedule.",
    }[m.kind];
    html += `<div class="empty-sess">${kindChip(m.kind, "xl")}<h3>${esc(m.title || "Fresh start")}</h3><p>${blurb}</p></div>`;
  }
  const arts = S.cur.artifacts || [];
  const shownArts = new Set();
  let toolGroup = [];
  const flush = () => {
    if (!toolGroup.length) return;
    html += `<div class="tools">${toolGroup.map(toolHTML).join("")}</div>`;
    for (const t of toolGroup) {
      for (const a of matchArtifacts(t, arts)) if (!shownArts.has(a.id)) { shownArts.add(a.id); html += artCardHTML(a); }
    }
    toolGroup = [];
  };
  items.forEach((it, i) => {
    if (it.type === "tool") { toolGroup.push(it.tool); return; }
    flush();
    switch (it.type) {
      case "user":
        html += `<div class="msg user"><div class="bubble">${it.images?.length ? `<div class="imgs">${it.images.map((s) => `<img src="${s}">`).join("")}</div>` : ""}${esc(it.text)}</div></div>`;
        break;
      case "assistant":
        html += `<div class="msg assistant"><span class="who">AS</span><div class="body md" ${it.pending ? 'id="stream"' : ""}>${it.html || (it.pending ? `<p>${esc(it.text)}</p>` : esc(it.text))}</div></div>`;
        if (!it.html && !it.pending) lazyMd(it, i);
        break;
      case "notice":
        html += `<div class="notice ${it.error ? "err" : ""}">${icon(it.error ? "alert" : "info", "sm")}<div>${esc(it.text)}${it.images?.length ? `<div class="imgs">${it.images.map((s) => `<img src="${s}">`).join("")}</div>` : ""}</div></div>`;
        break;
      case "approval":
        html += gateHTML(sid, it);
        break;
      case "ask":
        html += askHTML(sid, it);
        break;
      case "artifact":
        if (!shownArts.has(it.artifact.id)) { shownArts.add(it.artifact.id); html += artCardHTML(it.artifact); }
        break;
    }
  });
  flush();
  if (live.running) {
    const waiting = items.some((i) => (i.type === "approval" || i.type === "ask") && i.state === "waiting");
    const status = waiting ? "Waiting for you" : live.status || (items[items.length - 1]?.type === "tool" ? "Running tools" : "Thinking");
    html += `<div class="working"><span class="crab-w">${S.boot?.crab_work || ""}</span><span class="${live.review ? "rv" : ""}">${esc(status)}</span><span class="dots"><span></span><span></span><span></span></span><span class="el">${live.started ? fmtElapsed((Date.now() - live.started) / 1000) : ""}</span></div>`;
  }
  tr.innerHTML = html;
  // composer reflects running state
  const c = $(`.composer[data-composer="${CSS.escape(sid)}"]`);
  if (c) {
    const isStop = !!$(".send.stop", c);
    if (isStop !== !!live.running) rerenderComposer(sid);
  }
}

function lazyMd(it) {
  api().RenderMarkdown(it.text).then((h) => { it.html = h; renderSessionBody(); });
}

function matchArtifacts(tool, arts) {
  if (!["write_file", "office_document"].includes(tool.name) || !tool.done || tool.error) return [];
  let p = "";
  try { p = JSON.parse(tool.args || "{}").path || ""; } catch {}
  if (!p) p = tool.summary || "";
  const b = base(p).toLowerCase();
  return arts.filter((a) => base(a.source_path).toLowerCase() === b);
}

function toolHTML(t) {
  const st = !t.done ? `<span class="st"><span class="spin"></span></span>` : t.error ? `<span class="st err">${icon("x", "xs")}</span>` : `<span class="st ok">${icon("check", "xs")}</span>`;
  let args = t.args || "";
  try { args = JSON.stringify(JSON.parse(args), null, 2); } catch {}
  return `<details class="tool">
    <summary>${icon("chevRight", "xs chev")}${st}<span class="tname">${esc(t.name)}</span><span class="tsum">${esc(t.summary || "")}</span></summary>
    <div class="tbody">${args && args !== "{}" ? `<div class="lbl">INPUT</div><pre>${esc(args)}</pre>` : ""}${t.done ? `<div class="lbl">${t.error ? "ERROR" : "RESULT"}</div><pre>${esc(t.result || "(no output)")}</pre>` : ""}</div>
  </details>`;
}

function gateHTML(sid, it) {
  let args = it.args || "";
  try { args = JSON.stringify(JSON.parse(args), null, 2); } catch {}
  const done = it.state !== "waiting";
  return `<div class="card-gate ${done ? "done" : ""}">
    <div class="h">${icon("shield")}<span>Allow <code style="font-family:var(--mono)">${esc(it.name)}</code>?</span></div>
    <pre>${esc(it.summary || args)}</pre>
    ${done ? `<div class="result">${esc(it.decision ? { allow: "Allowed once", always: "Always allowed here", always_global: "Always allowed everywhere", deny: "Denied" }[it.decision] : "Answered")}</div>` : `
    <div class="acts">
      <button class="btn primary sm" data-gate="${it.id}" data-d="allow">${icon("check")}Allow once</button>
      ${it.scope ? `<button class="btn sm" data-gate="${it.id}" data-d="always" title="Remember for this folder">Always allow ${esc(it.scope)} here</button>
      <button class="btn sm ghost" data-gate="${it.id}" data-d="always_global" title="Remember everywhere">…everywhere</button>` : ""}
      <button class="btn sm danger" data-gate="${it.id}" data-d="deny">Deny</button>
    </div>`}
  </div>`;
}

function askHTML(sid, it) {
  const done = it.state !== "waiting";
  return `<div class="card-gate ask ${done ? "done" : ""}">
    <div class="h">${icon("help")}<span>Question</span></div>
    <p class="q">${esc(it.question)}</p>
    ${done ? `<div class="result">${it.answer ? "You answered: " + esc(it.answer) : "Answered"}</div>` : `
    ${it.choices?.length ? `<div class="acts">${it.choices.map((c) => `<button class="btn sm" data-ask="${it.id}" data-a="${esc(c)}">${esc(c)}</button>`).join("")}</div>` : ""}
    <div class="ans"><input class="input" id="ask-${it.id}" placeholder="Type an answer…"><button class="btn primary sm" data-askinput="${it.id}">Answer</button></div>`}
  </div>`;
}

function artCardHTML(a) {
  const k = ART_KINDS[a.kind] || ART_KINDS.text;
  return `<button class="art-card" data-art="${a.id}">${`<span class="kind lg k-artifact">${icon(k.icon)}</span>`}<div class="t"><b>${esc(a.title)}</b><span>${k.label}${a.versions > 1 ? ` · v${a.versions}` : ""} · ${bytes(a.size)}</span></div>${icon("chevRight", "sm")}</button>`;
}

// ---------- side panel ----------
function renderPanel() {
  const slot = $("#panel-slot");
  if (!slot) return;
  if (!S.panel) { slot.innerHTML = ""; return; }
  if (S.panel.type === "artifact") { slot.innerHTML = `<aside class="panel">${artifactViewerHTML(S.panel)}</aside>`; bindViewer(slot); return; }
  // files panel
  const files = S.cur?.work_files || [];
  const arts = S.cur?.artifacts || [];
  const tab = S.panel.tab || (files.length ? files[0].name : "artifacts");
  const cur = files.find((f) => f.name === tab);
  slot.innerHTML = `<aside class="panel narrow">
    <div class="panel-head"><span class="kind k-${S.cur.meta.kind}">${icon("panel")}</span><div class="t"><b>Session files</b><span>${files.length ? "Working files and artifacts" : "Artifacts from this session"}</span></div><button class="icon-btn" data-act="close-panel">${icon("x", "sm")}</button></div>
    <div class="panel-tabs">${files.map((f) => `<button class="${f.name === tab ? "on" : ""}" data-ptab="${f.name}">${esc(f.name.replace(".md", ""))}</button>`).join("")}<button class="${tab === "artifacts" ? "on" : ""}" data-ptab="artifacts">artifacts${arts.length ? ` (${arts.length})` : ""}</button></div>
    <div class="panel-body">${tab === "artifacts" || !cur
      ? arts.length ? `<div class="panel-list">${arts.map((a) => `<button class="row" data-art="${a.id}"><span class="kind k-artifact">${icon((ART_KINDS[a.kind] || ART_KINDS.text).icon)}</span><div class="t"><b>${esc(a.title)}</b><span>${(ART_KINDS[a.kind] || ART_KINDS.text).label} · ${rel(a.updated)}</span></div></button>`).join("")}</div>`
        : `<div class="panel-empty">${icon("box", "lg")}<p>Files this session writes — pages, documents, images — are kept here.</p></div>`
      : `<div class="md selectable">${cur.html}</div>`}</div>
    ${cur && tab !== "artifacts" ? `<div class="panel-actions"><button class="btn sm" data-openpath="${esc(cur.path)}">${icon("external")}Open</button><button class="btn sm ghost" data-reveal="${esc(cur.path)}">${icon("folderOpen")}Show in folder</button></div>` : ""}
  </aside>`;
}

async function openArtifactPanel(id) {
  let v = S.artCache[id];
  try { v = await api().GetArtifact(id); S.artCache[id] = v; } catch (e) { if (!v) return toast(errText(e), "err"); }
  if (S.view.name === "session") {
    S.panel = { type: "artifact", id, view: v, tab: "preview" };
    renderPanel();
    renderTitlebar();
  } else {
    openViewer(v);
  }
}

function artifactViewerHTML(p, inModal) {
  const v = p.view;
  const a = v.meta;
  const k = ART_KINDS[a.kind] || ART_KINDS.text;
  const hasSource = ["html", "svg", "markdown"].includes(a.kind);
  let body = "";
  if (p.tab === "source" && hasSource) body = `<div class="selectable">${v.source_html || ""}</div>`;
  else if (a.kind === "html") body = `<iframe sandbox="allow-scripts allow-forms allow-modals allow-popups" srcdoc="${esc(v.text)}"></iframe>`;
  else if (a.kind === "svg") body = `<div class="imgview"><img src="data:image/svg+xml;base64,${b64(v.text)}"></div>`;
  else if (a.kind === "image") body = `<div class="imgview"><img src="${v.data}"></div>`;
  else if (a.kind === "pdf") body = `<iframe src="${v.data}" style="background:transparent"></iframe>`;
  else if (a.kind === "markdown") body = `<div class="md selectable">${v.html}</div>`;
  else if (v.html) body = `<div class="selectable">${v.html}</div>`;
  else body = `<div class="panel-empty">${icon(k.icon, "lg")}<p><b>${esc(a.title)}</b><br>This file type opens in its own app.</p><button class="btn primary sm" data-artact="open" data-id="${a.id}">${icon("external")}Open ${esc(a.ext.toUpperCase())}</button></div>`;
  return `
    <div class="panel-head"><span class="kind lg k-artifact">${icon(k.icon)}</span><div class="t"><b class="selectable">${esc(a.title)}</b><span>${k.label}${a.versions > 1 ? ` · version ${a.versions}` : ""} · ${bytes(a.size)} · ${rel(a.updated)}</span></div>
      ${hasSource ? `<div class="seg"><button class="${p.tab !== "source" ? "on" : ""}" data-vtab="preview">${icon("eye")}Preview</button><button class="${p.tab === "source" ? "on" : ""}" data-vtab="source">${icon("fileCode")}Source</button></div>` : ""}
      <button class="icon-btn" data-act="${inModal ? "close-overlay" : "close-panel"}" title="Close">${icon("x", "sm")}</button></div>
    <div class="panel-body">${body}</div>
    <div class="panel-actions">
      <button class="btn sm" data-artact="open" data-id="${a.id}">${icon("external")}Open</button>
      <button class="btn sm" data-artact="export" data-id="${a.id}">${icon("download")}Save as…</button>
      <button class="btn sm ghost" data-artact="reveal" data-id="${a.id}">${icon("folderOpen")}Show in folder</button>
      ${inModal ? `<button class="btn sm ghost" data-artact="session" data-id="${a.id}" data-sid="${a.session_id}">${kindChip(a.session_kind)}Go to session</button>` : ""}
      <span style="flex:1"></span>
      <button class="btn sm danger" data-artact="delete" data-id="${a.id}">${icon("trash")}</button>
    </div>`;
}

function bindViewer(root) {
  $$("[data-vtab]", root).forEach((b) => b.addEventListener("click", async () => {
    const p = root.closest(".viewer") ? S.viewer : S.panel;
    p.tab = b.dataset.vtab;
    if (p.tab === "source" && !p.view.source_html) {
      const ext = p.view.meta.kind === "markdown" ? "md" : p.view.meta.ext;
      p.view.source_html = await api().RenderMarkdown("```" + ext + "\n" + p.view.text + "\n```");
    }
    if (root.closest(".viewer")) { $(".viewer").innerHTML = artifactViewerHTML(p, true); bindViewer($(".viewer")); }
    else renderPanel();
  }));
}

function openViewer(v) {
  S.viewer = { view: v, tab: "preview" };
  $("#overlay").innerHTML = `<div class="scrim" data-scrim><div class="viewer">${artifactViewerHTML(S.viewer, true)}</div></div>`;
  bindViewer($(".viewer"));
}

async function artifactAction(act, id, sid) {
  if (act === "open") return call("OpenArtifact", id);
  if (act === "reveal") return call("RevealArtifact", id);
  if (act === "export") { const p = await call("ExportArtifact", id); if (p) toast(`Saved to ${p}`); return; }
  if (act === "session") { closeOverlay(); return openSession(sid); }
  if (act === "delete") {
    if (!(await confirmBox("Delete this artifact?", "The saved copy is removed from your library. The original file (if it still exists) is not touched.", "Delete"))) return;
    await call("DeleteArtifact", id);
    delete S.artCache[id];
    if (S.panel?.id === id) { S.panel = null; renderPanel(); }
    if (S.cur) S.cur.artifacts = (S.cur.artifacts || []).filter((a) => a.id !== id);
    closeOverlay();
    if (S.view.name === "artifacts") renderView();
    else if (S.view.name === "session") renderSessionBody();
  }
}

// ---------- chats page ----------
function viewChats() {
  const f = S.filter;
  const q = f.q.toLowerCase();
  const list = S.sessions.filter((s) => (f.chats === "all" || s.kind === f.chats) && (!q || (s.title + " " + (s.preview || "")).toLowerCase().includes(q)));
  const groups = {};
  for (const s of list) (groups[s.pinned ? "Pinned" : dayGroup(s.updated)] ||= []).push(s);
  const tabs = [["all", "All", "chats"], ["chat", "Chats", "message"], ["code", "Code", "terminal"], ["artifact", "Artifacts", "sparkles"], ["project", "Projects", "folder"], ["scheduled", "Scheduled", "clock"]];
  return `<div class="scroll"><div class="page">
    <div class="page-head"><div><h1>Chats</h1><p>${S.sessions.length} sessions · everything you've worked on, in one place</p></div><div class="actions"><button class="btn primary" data-act="new">${icon("plus")}New chat</button></div></div>
    <div class="filters">
      <label class="search">${icon("search", "sm")}<input id="chat-q" placeholder="Search your chats…" value="${esc(f.q)}"></label>
      <div class="seg">${tabs.map(([k, l, ic]) => `<button class="${f.chats === k ? "on" : ""}" data-chatfilter="${k}">${icon(ic)}${l}</button>`).join("")}</div>
    </div>
    ${list.length ? Object.entries(groups).map(([g, items]) => `<div class="group-label">${g}</div><div class="list">${items.map(sessionRow).join("")}</div>`).join("") : emptyHTML("chat", q ? "Nothing matches" : "No chats yet", q ? "Try a different search." : "Start a conversation and it'll be saved here.", q ? "" : `<button class="btn primary" data-act="new">${icon("plus")}New chat</button>`)}
  </div></div>`;
}

function emptyHTML(kind, title, text, actions = "") {
  return `<div class="empty">${kindChip(kind, "xl")}<h3>${title}</h3><p>${text}</p>${actions}</div>`;
}

// ---------- projects ----------
function viewProjects() {
  const count = (id) => S.sessions.filter((s) => s.project_id === id).length;
  return `<div class="scroll"><div class="page">
    <div class="page-head"><div><h1>Projects</h1><p>Group chats around shared instructions and a folder of files.</p></div><div class="actions"><button class="btn primary" data-act="new-project">${icon("plus")}New project</button></div></div>
    <div class="grid">
      ${S.projects.map((p) => `<button class="card proj-card pc-${p.color}" data-project="${p.id}">
        <span class="bar"></span>
        <div class="inline" style="gap:10px"><span class="proj-hero"><span class="badge" style="width:38px;height:38px;border-radius:11px">${icon("folder")}</span></span><h3>${esc(p.name)}</h3></div>
        <p>${esc(p.description || p.instructions || "No description")}</p>
        <div class="foot">${icon("chats", "xs")}${count(p.id)} chats${p.folder ? ` · ${icon("folderOpen", "xs")}${esc(base(p.folder))}` : ""}<span style="margin-left:auto">${rel(p.updated)}</span></div>
      </button>`).join("")}
      <button class="card new" data-act="new-project">${icon("plus", "lg")}<span>New project</span></button>
    </div>
  </div></div>`;
}

function viewProject() {
  const p = S.projects.find((x) => x.id === S.view.id);
  if (!p) return viewProjects();
  const chats = S.sessions.filter((s) => s.project_id === p.id);
  const key = "project:" + p.id;
  return `<div class="scroll"><div class="page">
    <div class="proj-hero pc-${p.color}"><span class="badge">${icon("folder")}</span><div style="flex:1;min-width:0"><h1>${esc(p.name)}</h1><p>${esc(p.description || "")}</p></div>
      <button class="btn" data-act="edit-project" data-id="${p.id}">${icon("pencil")}Edit</button></div>
    <div class="proj-layout">
      <div>
        <div class="composer-wrap" style="margin-bottom:22px">${composerHTML(key)}</div>
        <div class="group-label" style="margin-top:0">Chats in this project</div>
        ${chats.length ? `<div class="list">${chats.map(sessionRow).join("")}</div>` : `<div class="empty" style="padding:34px">${kindChip("project", "lg")}<p style="margin:10px 0 0">No chats yet. Start one above — it will follow the project's instructions.</p></div>`}
      </div>
      <div>
        <div class="side-box"><h4>${icon("book", "sm")}Instructions<button class="btn sm ghost" data-act="edit-project" data-id="${p.id}">Edit</button></h4><div class="txt selectable">${esc(p.instructions || "None yet. Instructions are added to every chat in this project.")}</div></div>
        <div class="side-box"><h4>${icon("folderOpen", "sm")}Folder</h4>${p.folder ? `<div class="path selectable" data-reveal="${esc(p.folder)}" style="cursor:pointer">${esc(shortPath(p.folder))}</div>` : `<div class="txt">Chats share a private project folder. Pick a folder to work on real files.</div>`}</div>
        <div class="side-box"><h4>${icon("box", "sm")}Artifacts</h4><div id="proj-arts" class="txt">Loading…</div></div>
      </div>
    </div>
  </div></div>`;
}

async function fillProjectArtifacts(pid) {
  const arts = (await api().ListArtifacts()).filter((a) => a.project_id === pid);
  const el = $("#proj-arts");
  if (!el) return;
  el.innerHTML = arts.length ? arts.slice(0, 8).map((a) => `<button class="row-item" style="padding:6px 4px;border:0" data-art="${a.id}"><span class="kind k-artifact">${icon((ART_KINDS[a.kind] || ART_KINDS.text).icon)}</span><div class="t"><b style="font-size:13px">${esc(a.title)}</b></div></button>`).join("") : "Files made in this project show up here.";
}

function projectForm(p = {}) {
  const colors = ["amber", "rose", "sky", "lime", "violet", "teal"];
  let color = p.color || "amber";
  let folder = p.folder || "";
  modal({
    title: p.id ? "Edit project" : "New project",
    body: `
      <div class="field"><label>Name</label><input class="input" id="pf-name" value="${esc(p.name || "")}" placeholder="e.g. Thesis, Client site, Home lab"></div>
      <div class="field"><label>Description</label><input class="input" id="pf-desc" value="${esc(p.description || "")}" placeholder="What is this project about?"></div>
      <div class="field"><label>Instructions</label><textarea class="input" id="pf-ins" rows="5" placeholder="How should ASG work in this project? Tone, conventions, things to remember…">${esc(p.instructions || "")}</textarea><span class="hint">Added to every chat in this project.</span></div>
      <div class="field"><label>Folder</label><div class="inline"><input class="input mono" id="pf-folder" value="${esc(folder)}" placeholder="Optional — chats share a private folder otherwise" readonly><button class="btn" id="pf-pick">${icon("folderOpen")}Choose</button><button class="btn ghost" id="pf-clear" title="Clear">${icon("x")}</button></div></div>
      <div class="field"><label>Colour</label><div class="colors">${colors.map((c) => `<button class="pc-${c} ${c === color ? "on" : ""}" data-color="${c}"></button>`).join("")}</div></div>`,
    actions: [
      ...(p.id ? [{ label: "Delete", cls: "danger", fn: async () => { if (await confirmBox("Delete this project?", "Its chats are kept and moved out of the project.", "Delete")) { await call("DeleteProject", p.id); nav("projects"); return true; } return false; } }] : []),
      { label: "Cancel" },
      { label: p.id ? "Save" : "Create project", cls: "primary", fn: async () => {
        const out = await call("SaveProject", { ...p, name: $("#pf-name").value, description: $("#pf-desc").value, instructions: $("#pf-ins").value, folder: $("#pf-folder").value, color });
        S.projects = await api().ListProjects();
        nav("project", { id: out.id });
        return true;
      } },
    ],
    onOpen: (m) => {
      $("#pf-name", m).focus();
      $$("[data-color]", m).forEach((b) => b.addEventListener("click", () => { color = b.dataset.color; $$("[data-color]", m).forEach((x) => x.classList.toggle("on", x === b)); }));
      $("#pf-pick", m).addEventListener("click", async () => { const d = await pickFolder("Project folder"); if (d) $("#pf-folder", m).value = d; });
      $("#pf-clear", m).addEventListener("click", () => ($("#pf-folder", m).value = ""));
    },
  });
}

// ---------- code ----------
function viewCode(el) {
  el.innerHTML = `<div class="scroll"><div class="page">
    <div class="page-head"><div><h1>Code</h1><p>Coding sessions, grouped by folder. Pick one up exactly where you left off.</p></div><div class="actions"><button class="btn primary" data-act="new-code">${icon("plus")}New coding session</button></div></div>
    <div id="code-list"><div class="panel-empty">Loading…</div></div>
  </div></div>`;
  api().CodeFolders().then((folders) => {
    const box = $("#code-list");
    if (!box) return;
    box.innerHTML = folders.length ? folders.map((f) => `
      <div class="folder-card">
        <div class="fh">${kindChip("code", "lg")}<div class="t"><b>${esc(f.name)}</b><span title="${esc(f.path)}">${esc(shortPath(f.path))}</span></div>
          ${f.branch ? `<span class="branch">${icon("branch")}${esc(f.branch)}</span>` : ""}
          ${f.exists ? `<button class="btn sm" data-newcode="${esc(f.path)}">${icon("plus")}New session</button><button class="icon-btn" data-reveal="${esc(f.path)}" title="Open folder">${icon("folderOpen", "sm")}</button>` : `<span class="pill warn">Folder missing</span>`}
        </div>
        ${f.sessions.map((s) => `<button class="row-item" data-open="${s.id}">
          <span class="kind k-code" style="background:transparent">${icon(S.live[s.id]?.running ? "play" : "history")}</span>
          <div class="t"><b>${esc(s.title || "Untitled session")}</b><span>${esc(s.preview || (s.last_error ? "⚠ " + s.last_error : "No messages yet"))}</span></div>
          <div class="side"><span class="pill">${MODES[s.mode]?.[1] || "Build"}</span><span class="pill">${s.messages} msgs</span><span class="when">${rel(s.updated)}</span><span class="btn sm">${icon("play")}Resume</span></div>
        </button>`).join("")}
      </div>`).join("") : emptyHTML("code", "No coding sessions yet", "Point ASG at a folder — a repo, a script collection, anything — and start building.", `<button class="btn primary" data-act="new-code">${icon("folderOpen")}Choose a folder</button>`);
  });
}

async function newCodeSession(dir) {
  dir ||= await pickFolder("Choose the folder to work in");
  if (!dir) return;
  try {
    const meta = await api().NewSession({ kind: "code", workdir: dir, mode: "build", project_id: "", title: "" });
    S.sessions.unshift(meta);
    openSession(meta.id);
  } catch (e) { toast(errText(e), "err"); }
}

// ---------- artifacts ----------
function viewArtifacts(el) {
  el.innerHTML = `<div class="scroll"><div class="page">
    <div class="page-head"><div><h1>Artifacts</h1><p>Every page, document, image and file your sessions have made — saved copies, safe even if the original moves.</p></div></div>
    <div class="filters">
      <label class="search">${icon("search", "sm")}<input id="art-q" placeholder="Search artifacts…" value="${esc(S.filter.artQ)}"></label>
      <div class="seg">${[["all", "All", "box"], ["html", "Pages", "globe"], ["docs", "Docs", "file"], ["image", "Images", "image"], ["code", "Code", "fileCode"], ["data", "Data", "table"]].map(([k, l, ic]) => `<button class="${S.filter.arts === k ? "on" : ""}" data-artfilter="${k}">${icon(ic)}${l}</button>`).join("")}</div>
    </div>
    <div id="art-grid"><div class="panel-empty">Loading…</div></div>
  </div></div>`;
  api().ListArtifacts().then((all) => {
    const q = S.filter.artQ.toLowerCase();
    const f = S.filter.arts;
    const match = (a) => f === "all" || (f === "docs" ? ["markdown", "document", "pdf", "text"].includes(a.kind) : f === "image" ? ["image", "svg"].includes(a.kind) : a.kind === f);
    const list = all.filter((a) => match(a) && (!q || (a.title + " " + a.session_title).toLowerCase().includes(q)));
    const box = $("#art-grid");
    if (!box) return;
    box.innerHTML = list.length ? `<div class="grid">${list.map((a) => {
      const k = ART_KINDS[a.kind] || ART_KINDS.text;
      return `<button class="card art-tile" data-art="${a.id}">
        <div class="thumbnail" data-thumb="${a.id}" data-kind="${a.kind}"><span class="kind xl k-artifact">${icon(k.icon)}</span></div>
        <div class="info"><b>${esc(a.title)}</b><span>${kindChip(a.session_kind)}${esc(a.session_title || "")}</span><span style="color:var(--faint)">${k.label} · ${bytes(a.size)} · ${rel(a.updated)}</span></div>
      </button>`;
    }).join("")}</div>` : emptyHTML("artifact", q || f !== "all" ? "Nothing matches" : "No artifacts yet", "Start an Artifact session, or ask any chat to make a page, document or diagram. Files it writes are kept here.", `<button class="btn primary" data-act="new-artifact">${icon("sparkles")}Make an artifact</button>`);
    lazyThumbs();
  });
}

let thumbObs;
function lazyThumbs() {
  thumbObs?.disconnect();
  thumbObs = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      thumbObs.unobserve(e.target);
      fillThumb(e.target);
    }
  }, { rootMargin: "200px" });
  $$("[data-thumb]").forEach((el) => thumbObs.observe(el));
}
async function fillThumb(el) {
  const id = el.dataset.thumb, kind = el.dataset.kind;
  if (!["image", "svg", "html", "markdown", "code", "data", "text"].includes(kind)) return;
  let v = S.artCache[id];
  if (!v) { try { v = await api().GetArtifact(id); S.artCache[id] = v; } catch { return; } }
  if (kind === "image") el.innerHTML = `<img src="${v.data}" alt="">`;
  else if (kind === "svg") el.innerHTML = `<img src="data:image/svg+xml;base64,${b64(v.text)}" alt="" style="object-fit:contain;padding:14px">`;
  else if (kind === "html") el.innerHTML = `<iframe sandbox="" srcdoc="${esc(v.text)}" tabindex="-1" loading="lazy"></iframe>`;
  else el.insertAdjacentHTML("afterbegin", `<div class="snip">${esc((v.text || "").slice(0, 900))}</div>`);
}

// ---------- schedules ----------
function viewSchedules(el) {
  el.innerHTML = `<div class="scroll"><div class="page">
    <div class="page-head"><div><h1>Schedules</h1><p>Run a prompt on a timer — daily briefings, weekly reports, routine checks. Each run is saved as its own session.</p></div><div class="actions"><button class="btn primary" data-act="new-schedule">${icon("plus")}New schedule</button></div></div>
    <div class="warnbox" style="background:var(--k-scheduled-bg);color:var(--k-scheduled)">${icon("info", "sm")}<span>Schedules run while ASG is open (it can sit minimised). A run that was missed while the app was closed runs once when it next opens.</span></div>
    <div id="sched-list"><div class="panel-empty">Loading…</div></div>
  </div></div>`;
  api().ListSchedules().then((list) => {
    const box = $("#sched-list");
    if (!box) return;
    box.innerHTML = list.length ? list.map((s) => {
      const proj = S.projects.find((p) => p.id === s.project_id);
      return `<div class="sched">
        <button class="switch ${s.enabled ? "on" : ""}" data-schedtoggle="${s.id}" title="${s.enabled ? "On" : "Off"}"></button>
        ${kindChip("scheduled", "lg")}
        <div class="t"><b>${esc(s.name)}</b><div class="p">${esc(s.prompt)}</div>
          <div class="m"><span>${icon("calendar")}${esc(s.label)}</span>${s.enabled && s.next_run ? `<span>${icon("clock")}Next ${rel(s.next_run)}</span>` : ""}${s.last_run && !s.last_run.startsWith("0001") ? `<span>${icon("history")}Last ${rel(s.last_run)}${s.last_status && s.last_status !== "ok" ? " · " + esc(s.last_status) : ""}</span>` : ""}${proj ? `<span>${icon("folder")}${esc(proj.name)}</span>` : ""}${s.workdir ? `<span>${icon("folderOpen")}${esc(base(s.workdir))}</span>` : ""}${s.allow_tools ? `<span style="color:var(--warn)">${icon("shield")}Tools allowed</span>` : ""}</div></div>
        <div class="inline">
          ${s.last_session ? `<button class="btn sm ghost" data-open="${s.last_session}">${icon("history")}Last run</button>` : ""}
          <button class="btn sm" data-schedrun="${s.id}">${icon("play")}Run now</button>
          <button class="icon-btn" data-schededit="${s.id}" title="Edit">${icon("pencil", "sm")}</button>
        </div>
      </div>`;
    }).join("") : emptyHTML("scheduled", "No schedules yet", "Have ASG check something, summarise something or build a report on a timer.", `<button class="btn primary" data-act="new-schedule">${icon("plus")}New schedule</button>`);
    S.schedules = list;
  });
}

function scheduleForm(s = {}) {
  const v = { every: "daily", at: "09:00", interval: 30, weekday: 1, mode: "build", enabled: true, ...s };
  let workdir = v.workdir || "";
  const days = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
  modal({
    title: s.id ? "Edit schedule" : "New schedule",
    wide: true,
    body: `
      <div class="field"><label>Name</label><input class="input" id="sf-name" value="${esc(v.name || "")}" placeholder="Morning briefing"></div>
      <div class="field"><label>Prompt</label><textarea class="input" id="sf-prompt" rows="4" placeholder="What should ASG do each time?">${esc(v.prompt || "")}</textarea></div>
      <div class="row2">
        <div class="field"><label>Repeat</label><select class="input" id="sf-every">${[["minutes", "Every few minutes"], ["hourly", "Every hour"], ["daily", "Every day"], ["weekdays", "Weekdays"], ["weekly", "Every week"]].map(([k, l]) => `<option value="${k}" ${v.every === k ? "selected" : ""}>${l}</option>`).join("")}</select></div>
        <div class="field" id="sf-when"></div>
      </div>
      <div class="row2">
        <div class="field"><label>Project</label><select class="input" id="sf-proj"><option value="">None</option>${S.projects.map((p) => `<option value="${p.id}" ${v.project_id === p.id ? "selected" : ""}>${esc(p.name)}</option>`).join("")}</select></div>
        <div class="field"><label>Mode</label><select class="input" id="sf-mode">${Object.entries(MODES).map(([k, [, l, d]]) => `<option value="${k}" ${v.mode === k ? "selected" : ""}>${l} — ${d}</option>`).join("")}</select></div>
      </div>
      <div class="field"><label>Folder</label><div class="inline"><input class="input mono" id="sf-dir" value="${esc(workdir)}" placeholder="Optional — a fresh sandbox (or the project's folder) is used otherwise" readonly><button class="btn" id="sf-pick">${icon("folderOpen")}Choose</button><button class="btn ghost" id="sf-clear">${icon("x")}</button></div></div>
      <div class="setting"><div class="t"><b>Allow tools without asking</b><span>Nobody is there to answer permission prompts, so gated tools (shell, writing outside the sandbox, web) are skipped unless this is on.</span></div><button class="switch ${v.allow_tools ? "on" : ""}" id="sf-allow"></button></div>
      <div class="setting"><div class="t"><b>Enabled</b><span>Turn off to pause without deleting.</span></div><button class="switch ${v.enabled ? "on" : ""}" id="sf-enabled"></button></div>`,
    actions: [
      ...(s.id ? [{ label: "Delete", cls: "danger", fn: async () => { if (await confirmBox("Delete this schedule?", "Past runs stay in your chats.", "Delete")) { await call("DeleteSchedule", s.id); renderView(); return true; } return false; } }] : []),
      { label: "Cancel" },
      { label: s.id ? "Save" : "Create schedule", cls: "primary", fn: async () => {
        const g = (id) => $(id).value;
        await call("SaveSchedule", { ...s, name: g("#sf-name"), prompt: g("#sf-prompt"), every: g("#sf-every"), at: $("#sf-at")?.value || v.at, interval: +($("#sf-int")?.value || v.interval), weekday: +($("#sf-wd")?.value ?? v.weekday), project_id: g("#sf-proj"), mode: g("#sf-mode"), workdir: g("#sf-dir"), allow_tools: $("#sf-allow").classList.contains("on"), enabled: $("#sf-enabled").classList.contains("on") });
        renderView();
        return true;
      } },
    ],
    onOpen: (m) => {
      const when = () => {
        const e = $("#sf-every", m).value;
        $("#sf-when", m).innerHTML = e === "minutes" ? `<label>Every</label><div class="inline"><input class="input" type="number" min="5" id="sf-int" value="${v.interval || 30}"><span style="color:var(--muted)">minutes</span></div>`
          : e === "hourly" ? `<label>&nbsp;</label><span class="hint" style="padding:9px 0">On the hour</span>`
          : e === "weekly" ? `<label>On</label><div class="inline"><select class="input" id="sf-wd">${days.map((d, i) => `<option value="${i}" ${+v.weekday === i ? "selected" : ""}>${d}</option>`).join("")}</select><input class="input" type="time" id="sf-at" value="${esc(v.at)}"></div>`
          : `<label>At</label><input class="input" type="time" id="sf-at" value="${esc(v.at)}">`;
      };
      when();
      $("#sf-every", m).addEventListener("change", when);
      $$(".switch", m).forEach((sw) => sw.addEventListener("click", () => sw.classList.toggle("on")));
      $("#sf-pick", m).addEventListener("click", async () => { const d = await pickFolder("Folder for this schedule"); if (d) $("#sf-dir", m).value = d; });
      $("#sf-clear", m).addEventListener("click", () => ($("#sf-dir", m).value = ""));
      $("#sf-name", m).focus();
    },
  });
}

// ---------- customise ----------
function viewCustomise(el) {
  const tabs = [["model", "cpu", "Models"], ["instructions", "book", "Instructions"], ["skills", "zap", "Skills"], ["tools", "wrench", "Tools"], ["memory", "bulb", "Memory"], ["behaviour", "sliders", "Behaviour"]];
  el.innerHTML = `<div class="scroll"><div class="page">
    <div class="page-head"><div><h1>Customise</h1><p>Shape how ASG thinks and works — shared with the terminal app.</p></div></div>
    <div class="split"><nav class="subnav">${tabs.map(([k, ic, l]) => `<button class="${S.custTab === k ? "on" : ""}" data-custtab="${k}">${icon(ic)}${l}</button>`).join("")}</nav><div id="cust-body"><div class="panel-empty">Loading…</div></div></div>
  </div></div>`;
  renderCustBody();
}

async function renderCustBody() {
  const box = $("#cust-body");
  if (!box) return;
  const t = S.custTab;
  if (t === "model" || t === "instructions" || t === "behaviour") {
    const cfg = await api().GetConfig();
    S.cfg = cfg;
    S.cfgPath ||= (await api().Storage()).config;
    if (t === "model") {
      const names = Object.keys(cfg.providers).sort();
      box.innerHTML = `<div class="section"><h3>Models & providers</h3><p>The active provider answers every chat. API keys are stored in ${esc(shortPath(S.cfgPath || "config.json"))} and never shown in full.</p>
        ${names.map((n) => { const p = cfg.providers[n]; return `<div class="prov ${cfg.active_provider === n ? "active" : ""}" data-prov="${esc(n)}">
          <div class="ph"><b>${esc(n)}</b><span class="pill">${esc(p.type)}</span>${cfg.active_provider === n ? `<span class="pill ok">Active</span>` : `<button class="btn sm ghost" data-activate="${esc(n)}">Make active</button>`}<span style="flex:1"></span><button class="icon-btn" data-rmprov="${esc(n)}" title="Remove">${icon("trash", "sm")}</button></div>
          <div class="row2"><div class="field"><label>Model</label><div class="inline"><input class="input mono" data-k="model" value="${esc(p.model)}"><button class="btn" data-pickmodel="${esc(n)}" title="List models">${icon("list")}</button></div></div>
          <div class="field"><label>Base URL</label><input class="input mono" data-k="base_url" value="${esc(p.base_url)}"></div></div>
          <div class="row2"><div class="field"><label>API key</label><input class="input mono" type="password" data-k="api_key" value="${esc(p.api_key || "")}" placeholder="${p.type === "ollama" ? "Not needed for local Ollama" : "Paste a key"}"></div>
          ${p.type === "ollama" ? `<div class="field"><label>Context window</label><input class="input mono" type="number" data-k="num_ctx" value="${p.num_ctx || ""}" placeholder="16384"></div>` : ""}</div>
        </div>`; }).join("")}
        <div class="inline" style="margin-top:6px"><button class="btn" data-act="add-provider">${icon("plus")}Add provider</button><span style="flex:1"></span><button class="btn primary" data-act="save-providers">Save</button></div></div>`;
    } else if (t === "instructions") {
      box.innerHTML = `<div class="section"><h3>Instructions for every chat</h3><p>Added to the system prompt everywhere — desktop and terminal. Project instructions are added on top of these.</p>
        <textarea class="input" id="sys-prompt" rows="12" placeholder="e.g. I'm a Go developer on Windows. Prefer concise answers with code first.">${esc(cfg.system_prompt || "")}</textarea>
        <div class="inline" style="margin-top:12px;justify-content:flex-end"><button class="btn primary" data-act="save-instructions">Save</button></div></div>`;
    } else {
      box.innerHTML = `<div class="section"><h3>Behaviour</h3><p>How far ASG goes on its own.</p>
        <div class="setting"><div class="t"><b>Approve every tool automatically</b><span>Skips all permission prompts. Only turn this on if you trust every folder you work in.</span></div><button class="switch ${cfg.auto_approve ? "on" : ""}" id="bh-auto"></button></div>
        <div class="setting"><div class="t"><b>Tool rounds per turn</b><span>How many rounds of tool calls one reply may use before stopping.</span></div><input class="input" style="width:90px" type="number" min="1" id="bh-rounds" value="${cfg.max_tool_rounds}"></div>
        <div class="setting"><div class="t"><b>Self-review passes</b><span>ASG re-checks each answer this many times from different angles (-1 turns it off).</span></div><input class="input" style="width:90px" type="number" min="-1" id="bh-review" value="${cfg.review?.passes ?? 4}"></div>
        <div class="setting"><div class="t"><b>Temperature</b><span>Blank uses the model's default.</span></div><input class="input" style="width:90px" type="number" step="0.1" min="0" max="2" id="bh-temp" value="${cfg.temperature ?? ""}"></div>
        <div class="inline" style="margin-top:12px;justify-content:flex-end"><button class="btn primary" data-act="save-behaviour">Save</button></div></div>`;
      $("#bh-auto").addEventListener("click", (e) => e.currentTarget.classList.toggle("on"));
    }
    return;
  }
  if (t === "skills") {
    const skills = (S.skills = await api().ListSkills());
    box.innerHTML = `<div class="section"><h3>Skills</h3><p>Reusable instructions ASG loads when a task needs them. Type <code>/</code> in any message to run one directly.</p>
      <div class="inline" style="margin-bottom:14px"><button class="btn" data-act="import-skill">${icon("download")}Import</button><button class="btn" data-act="new-skill">${icon("plus")}New skill</button></div>
      ${skills.length ? `<div class="list-box">${skills.map((s) => `<div class="li"><span class="kind k-artifact">${icon("zap")}</span><div class="t"><b>/${esc(s.name)}${s.builtin ? ` <span class="pill" title="Ships with ASG. Create a skill with the same name to replace it.">Built-in</span>` : ""}</b><span>${esc(s.description)}</span></div><button class="btn sm ghost" data-viewskill="${esc(s.name)}">View</button>${s.builtin ? "" : `<button class="icon-btn" data-rmskill="${esc(s.name)}" title="Remove">${icon("trash", "sm")}</button>`}</div>`).join("")}</div>` : `<div class="panel-empty">No skills installed yet.</div>`}</div>`;
    return;
  }
  if (t === "tools") {
    const tools = await api().ListTools();
    box.innerHTML = `<div class="section"><h3>Tools</h3><p>What ASG can do. Tools marked <span class="pill warn">asks first</span> wait for your permission unless you've allowed them.</p>
      <div class="inline" style="margin-bottom:14px"><button class="btn" data-act="reload-tools">${icon("refresh")}Reload script tools</button></div>
      <div class="list-box">${tools.map((x) => `<div class="li"><span class="kind k-code">${icon("wrench")}</span><div class="t"><b>${esc(x.name)}</b><span>${esc(x.description)}</span></div>${x.needs_approval ? `<span class="pill warn">asks first</span>` : `<span class="pill ok">safe</span>`}<span class="pill">${esc(x.source)}</span></div>`).join("")}</div></div>`;
    return;
  }
  if (t === "memory") {
    const notes = await api().ListMemory();
    box.innerHTML = `<div class="section"><h3>Memory</h3><p>Notes ASG keeps between sessions. It saves these itself when you tell it something worth remembering; you can add or remove them here too.</p>
      <div class="inline" style="margin-bottom:14px"><button class="btn" data-act="new-memory">${icon("plus")}Add note</button></div>
      ${notes.length ? `<div class="list-box">${notes.map((n) => `<div class="li"><span class="kind k-chat">${icon("bulb")}</span><div class="t"><b>${esc(n.name)}</b><span>${esc(n.summary)}</span></div><span class="pill">${esc(n.scope)}</span><button class="btn sm ghost" data-viewmem="${esc(n.scope)}|${esc(n.name)}">View</button><button class="icon-btn" data-rmmem="${esc(n.scope)}|${esc(n.name)}" title="Delete">${icon("trash", "sm")}</button></div>`).join("")}</div>` : `<div class="panel-empty">No memories yet.</div>`}</div>`;
    S.notes = notes;
  }
}

function collectProviders() {
  const cfg = S.cfg;
  $$("[data-prov]").forEach((el) => {
    const p = cfg.providers[el.dataset.prov];
    $$("[data-k]", el).forEach((inp) => {
      const k = inp.dataset.k;
      p[k] = k === "num_ctx" ? (+inp.value || 0) : inp.value.trim();
    });
  });
  return cfg;
}

async function pickModelFor(name) {
  collectProviders();
  const m = modal({ title: `Models on ${name}`, body: `<div class="panel-empty">Asking ${esc(name)} for its models…</div>`, actions: [{ label: "Close" }] });
  let models;
  try { models = await api().ListModels(name); } catch (e) { $(".modal-body", m).innerHTML = `<div class="warnbox">${icon("alert", "sm")}${esc(errText(e))}</div><p style="color:var(--muted);font-size:13px">Save the provider first if you just changed its URL or key.</p>`; return; }
  $(".modal-body", m).innerHTML = `<label class="search" style="margin-bottom:10px">${icon("search", "sm")}<input id="mq" placeholder="Filter…"></label><div class="list-box" id="ml" style="max-height:380px;overflow:auto"></div>`;
  const draw = () => {
    const q = $("#mq", m).value.toLowerCase();
    $("#ml", m).innerHTML = models.filter((x) => x.toLowerCase().includes(q)).map((x) => `<button class="li" style="width:100%;text-align:left" data-m="${esc(x)}"><div class="t"><b>${esc(x)}</b></div></button>`).join("") || `<div class="panel-empty">No models.</div>`;
    $$("[data-m]", m).forEach((b) => b.addEventListener("click", () => {
      const inp = $(`[data-prov="${CSS.escape(name)}"] [data-k="model"]`);
      if (inp) inp.value = b.dataset.m;
      closeOverlay();
    }));
  };
  $("#mq", m).addEventListener("input", draw);
  $("#mq", m).focus();
  draw();
}

// model chip in the composer: switch provider / model quickly
async function modelMenu(anchor) {
  const cfg = await api().GetConfig();
  const names = Object.keys(cfg.providers).sort();
  menu(anchor, [
    { label: "Provider", header: true },
    ...names.map((n) => ({ icon: "cpu", label: n, sub: cfg.providers[n].model, check: cfg.active_provider === n, fn: async () => { S.model = await call("SetModel", n, ""); refreshModelChips(); } })),
    { sep: true },
    { icon: "list", label: "Choose a model…", fn: () => chooseModel(cfg.active_provider) },
    { icon: "sliders", label: "Manage providers", fn: () => { S.custTab = "model"; nav("customise"); } },
  ], "up");
}
function refreshModelChips() { $$(".chip.model .ellip").forEach((el) => (el.textContent = modelLabel())); }

async function chooseModel(provider) {
  const m = modal({ title: `Choose a model · ${provider}`, body: `<div class="panel-empty">Loading models…</div>`, actions: [{ label: "Close" }] });
  let models;
  try { models = await api().ListModels(provider); } catch (e) { $(".modal-body", m).innerHTML = `<div class="warnbox">${icon("alert", "sm")}${esc(errText(e))}</div>`; return; }
  $(".modal-body", m).innerHTML = `<label class="search" style="margin-bottom:10px">${icon("search", "sm")}<input id="mq" placeholder="Filter models…"></label><div class="list-box" id="ml" style="max-height:400px;overflow:auto"></div>`;
  const draw = () => {
    const q = $("#mq", m).value.toLowerCase();
    $("#ml", m).innerHTML = models.filter((x) => x.toLowerCase().includes(q)).map((x) => `<button class="li" style="width:100%;text-align:left" data-m="${esc(x)}">${icon(x === S.model?.model ? "check" : "cpu", "sm")}<div class="t"><b>${esc(x)}</b></div></button>`).join("") || `<div class="panel-empty">No models.</div>`;
    $$("[data-m]", m).forEach((b) => b.addEventListener("click", async () => { S.model = await call("SetModel", provider, b.dataset.m); refreshModelChips(); closeOverlay(); toast(`Now using ${b.dataset.m}`); }));
  };
  $("#mq", m).addEventListener("input", draw);
  $("#mq", m).focus();
  draw();
}

// ---------- settings ----------
function viewSettings(el) {
  const tabs = [["storage", "drive", "Storage & sandbox"], ["appearance", "sun", "Appearance"], ["about", "info", "About"]];
  el.innerHTML = `<div class="scroll"><div class="page">
    <div class="page-head"><div><h1>Settings</h1><p>Where things live and how the app looks.</p></div></div>
    <div class="split"><nav class="subnav">${tabs.map(([k, ic, l]) => `<button class="${S.setTab === k ? "on" : ""}" data-settab="${k}">${icon(ic)}${l}</button>`).join("")}</nav><div id="set-body"></div></div>
  </div></div>`;
  renderSetBody();
}

async function renderSetBody() {
  const box = $("#set-body");
  if (!box) return;
  if (S.setTab === "appearance") {
    const cur = S.boot.settings.theme;
    const sw = (a, b, c) => `<span class="sw"><i style="background:${a}"></i><i style="background:${b}"></i><i style="background:${c}"></i></span>`;
    box.innerHTML = `<div class="section"><h3>Theme</h3><p>Follow Windows, or pick one.</p><div class="theme-pick">
      <button class="${cur === "system" ? "on" : ""}" data-theme-set="system">${sw("#121211", "#f7f6f1", "#c6f432")}<span>${icon("monitor", "sm")} System</span></button>
      <button class="${cur === "light" ? "on" : ""}" data-theme-set="light">${sw("#ecebe4", "#ffffff", "#c6f432")}<span>${icon("sun", "sm")} Light</span></button>
      <button class="${cur === "dark" ? "on" : ""}" data-theme-set="dark">${sw("#121211", "#20201d", "#c6f432")}<span>${icon("moon", "sm")} Dark</span></button>
    </div></div>
    <div class="section"><h3>Session icons</h3><p>Every session wears its kind, so you can tell them apart at a glance.</p>
      <div class="list-box">${Object.entries(KINDS).map(([k, v]) => `<div class="li" style="align-items:center">${kindChip(k)}<div class="t"><b style="font-family:var(--font)">${v.label}</b><span>${{ chat: "General chat in a private sandbox folder", code: "Works in a folder you pick; resumable from Code", artifact: "Made to produce a file — page, doc, diagram, data", project: "A chat inside a project, with its instructions", scheduled: "Started automatically by a schedule" }[k]}</span></div></div>`).join("")}</div></div>`;
    return;
  }
  if (S.setTab === "about") {
    box.innerHTML = `<div class="section"><h3>ASG desktop <span class="pill" style="font:500 11.5px var(--mono);vertical-align:middle">${esc(S.boot.version || "dev")}</span></h3><p>The same agent as the <code>asg</code> terminal app, with sessions, projects, artifacts and schedules.</p>
      <div class="setting"><div class="t"><b>Shared config</b><span class="selectable" style="font-family:var(--mono)">${esc((await api().Storage()).config)}</span></div></div>
      <div class="setting"><div class="t"><b>Keyboard</b><span>Ctrl+N new chat · Ctrl+K search · Ctrl+B sidebar · Enter send · Shift+Enter new line · Esc close</span></div></div></div>`;
    return;
  }
  const st = await api().Storage();
  const isDefault = st.is_default;
  box.innerHTML = `<div class="section"><h3>Where your sessions live</h3><p>Sessions, artifacts, project files and each chat's sandbox are kept in one folder. Put it on an external drive or a synced folder if you like.</p>
    ${S.boot.data_fallback ? `<div class="warnbox">${icon("alert", "sm")}<span>Your chosen folder <b>${esc(S.boot.settings.data_dir)}</b> isn't available right now, so ASG is using the default folder. Plug the drive in and restart ASG.</span></div>` : ""}
    <div class="storage-path">${icon("drive", "lg")}<div class="t"><b class="selectable">${esc(st.data_dir)}</b><span>${isDefault ? "Default location" : "Custom location"}</span></div><button class="btn sm" data-reveal-dir="${esc(st.data_dir)}">${icon("folderOpen")}Open</button></div>
    <div class="stats"><div class="stat"><b>${st.sessions}</b><span>sessions</span></div><div class="stat"><b>${st.artifacts}</b><span>artifacts</span></div><div class="stat"><b>${bytes(st.bytes)}</b><span>on disk</span></div></div>
    <div class="setting"><div class="t"><b>Move everything to another folder</b><span>Copies your sessions, artifacts and sandboxes to a new, empty folder (e.g. on an external drive), then removes the old copy.</span></div><button class="btn primary" data-act="move-data">${icon("drive")}Move…</button></div>
    <div class="setting"><div class="t"><b>Use a different folder</b><span>Switch to a folder that already holds ASG data — say, a drive you used on another PC. Nothing is moved.</span></div><button class="btn" data-act="use-data">${icon("folderOpen")}Choose…</button></div>
    ${isDefault ? "" : `<div class="setting"><div class="t"><b>Back to the default folder</b><span>${esc(shortPath(st.default))} — nothing is moved.</span></div><button class="btn ghost" data-act="reset-data">Reset</button></div>`}
  </div>
  <div class="section"><h3>Sandbox</h3><p>Chats that aren't tied to a folder get their own sandbox: a private folder inside the data folder above. Shell commands, files and downloads land there, never in your real projects. Coding sessions and projects with a folder work in that folder instead, and anything that changes files or runs commands asks first.</p>
    <div class="setting"><div class="t"><b>Sandbox folders</b><span class="selectable" style="font-family:var(--mono)">${esc(st.data_dir)}${S.boot.os === "windows" ? "\\" : "/"}sandbox</span></div><button class="btn sm" data-reveal-dir="${esc(st.data_dir + (S.boot.os === "windows" ? "\\" : "/") + "sandbox")}">${icon("folderOpen")}Open</button></div>
  </div>`;
}

async function changeData(move) {
  if (move && !(await confirmBox("Move your ASG data?", "Pick an empty folder. Everything is copied there and then removed from the current folder. Running sessions must be finished first.", "Choose folder"))) return;
  try {
    const dir = move ? await api().ChangeDataDir(true) : await api().ChangeDataDir(false);
    if (!dir) return;
    S.boot = await api().Bootstrap();
    S.sessions = S.boot.sessions; S.projects = S.boot.projects; S.live = {}; S.artCache = {};
    toast(move ? `Moved to ${dir}` : `Now using ${dir}`);
    render();
  } catch (e) { toast(errText(e), "err"); }
}

// ---------- overlays: modal, confirm, menu, palette, toast ----------
function modal({ title, body, actions = [], wide, onOpen }) {
  const ov = $("#overlay");
  ov.innerHTML = `<div class="scrim" data-scrim><div class="modal ${wide ? "wide" : ""}" role="dialog"><div class="modal-head"><h2>${esc(title)}</h2><button class="icon-btn" data-act="close-overlay">${icon("x", "sm")}</button></div><div class="modal-body">${body}</div><div class="modal-foot">${actions.map((a, i) => `<button class="btn ${a.cls || ""}" data-mact="${i}" ${a.cls === "danger" ? 'style="margin-right:auto"' : ""}>${esc(a.label)}</button>`).join("")}</div></div></div>`;
  const m = $(".modal", ov);
  $$("[data-mact]", m).forEach((b) => b.addEventListener("click", async () => {
    const a = actions[+b.dataset.mact];
    if (!a.fn) return closeOverlay();
    b.disabled = true;
    try { const ok = await a.fn(m); if (ok !== false) closeOverlay(); } catch { } finally { b.disabled = false; }
  }));
  onOpen?.(m);
  return m;
}

function confirmBox(title, text, okLabel = "OK") {
  return new Promise((res) => {
    const host = document.createElement("div");
    host.innerHTML = `<div class="scrim" style="z-index:80"><div class="modal" style="width:min(420px,100%)"><div class="modal-head"><h2>${esc(title)}</h2></div><div class="modal-body"><p style="margin:0;color:var(--muted)">${esc(text)}</p></div><div class="modal-foot"><button class="btn" data-c="0">Cancel</button><button class="btn ${/delete|remove/i.test(okLabel) ? "danger" : "primary"}" data-c="1">${esc(okLabel)}</button></div></div></div>`;
    document.body.appendChild(host);
    host.addEventListener("click", (e) => {
      const c = e.target.closest("[data-c]");
      if (!c && !e.target.classList.contains("scrim")) return;
      host.remove();
      res(c?.dataset.c === "1");
    });
  });
}

function closeOverlay() { $("#overlay").innerHTML = ""; S.viewer = null; }

let menuEl = null;
function menu(anchor, items, dir = "down") {
  closeMenu();
  menuEl = document.createElement("div");
  menuEl.className = "menu";
  menuEl.innerHTML = items.map((it, i) => it.sep ? `<div class="sep"></div>` : it.header ? `<div class="lbl">${esc(it.label)}</div>` : `<button data-mi="${i}" class="${it.danger ? "danger" : ""}">${it.html || (it.icon ? icon(it.icon, "sm") : "")}<span>${esc(it.label)}</span>${it.check ? `<span class="check">${icon("check", "sm")}</span>` : it.sub ? `<span class="sub">${esc(it.sub)}</span>` : ""}</button>`).join("");
  document.body.appendChild(menuEl);
  const r = anchor.getBoundingClientRect();
  const mw = menuEl.offsetWidth, mh = menuEl.offsetHeight;
  let x = Math.min(r.left, innerWidth - mw - 8);
  let y = dir === "up" ? r.top - mh - 6 : r.bottom + 6;
  if (y + mh > innerHeight - 8) y = r.top - mh - 6;
  if (y < 8) y = r.bottom + 6;
  menuEl.style.left = Math.max(8, x) + "px";
  menuEl.style.top = Math.max(8, y) + "px";
  menuEl.addEventListener("click", (e) => {
    const b = e.target.closest("[data-mi]");
    if (!b) return;
    const it = items[+b.dataset.mi];
    closeMenu();
    it.fn?.();
  });
}
function closeMenu() { menuEl?.remove(); menuEl = null; }

function sessionMenu(anchor, id) {
  const s = S.sessions.find((x) => x.id === id) || S.cur?.meta;
  if (!s) return;
  const projItems = S.projects.map((p) => ({ html: `<span class="pdot pc-${p.color}"></span>`, label: p.name, check: s.project_id === p.id, fn: () => call("MoveSessionToProject", id, s.project_id === p.id ? "" : p.id).then(refreshSessions) }));
  menu(anchor, [
    { icon: "pencil", label: "Rename", fn: () => renameSession(id) },
    { icon: "pin", label: s.pinned ? "Unpin" : "Pin to top", fn: () => call("PinSession", id, !s.pinned) },
    ...(s.kind !== "code" && projItems.length ? [{ sep: true }, { label: "Move to project", header: true }, ...projItems] : []),
    { sep: true },
    { icon: "folderOpen", label: "Open its folder", fn: async () => { const v = await api().OpenSession(id); call("OpenPath", v.workdir); } },
    { icon: "trash", label: "Delete", danger: true, fn: async () => {
      if (!(await confirmBox("Delete this session?", s.workdir ? "Its history is removed. Your project folder is not touched." : "Its history and sandbox folder are removed. Saved artifacts stay in your library.", "Delete"))) return;
      await call("DeleteSession", id);
      delete S.live[id];
      if (S.view.name === "session" && S.view.id === id) nav("home");
    } },
  ]);
}

function renameSession(id) {
  const s = S.sessions.find((x) => x.id === id) || S.cur?.meta;
  modal({
    title: "Rename",
    body: `<input class="input" id="rn" value="${esc(s?.title || "")}">`,
    actions: [{ label: "Cancel" }, { label: "Save", cls: "primary", fn: async () => { await call("RenameSession", id, $("#rn").value); return true; } }],
    onOpen: (m) => { const i = $("#rn", m); i.focus(); i.select(); i.addEventListener("keydown", (e) => e.key === "Enter" && $('[data-mact="1"]', m).click()); },
  });
}

function palette() {
  const ov = $("#overlay");
  ov.innerHTML = `<div class="scrim" data-scrim><div class="palette"><label class="search">${icon("search")}<input id="pq" placeholder="Search sessions, or jump to…"></label><div class="res" id="pr"></div></div></div>`;
  const actions = [["home", "plus", "New chat"], ["chats", "chats", "Chats"], ["projects", "folder", "Projects"], ["code", "terminal", "Code"], ["artifacts", "box", "Artifacts"], ["schedules", "clock", "Schedules"], ["customise", "sliders", "Customise"], ["settings", "settings", "Settings"]];
  let sel = 0, results = [];
  const draw = () => {
    const q = $("#pq").value.toLowerCase().trim();
    const acts = actions.filter(([, , l]) => !q || l.toLowerCase().includes(q)).map(([n, ic, l]) => ({ html: `<span class="kind" style="background:var(--hover)">${icon(ic)}</span><span class="t">${l}</span><span class="when">Go to</span>`, fn: () => nav(n) }));
    const ss = S.sessions.filter((s) => !q || (s.title + " " + (s.preview || "") + " " + (s.workdir || "")).toLowerCase().includes(q)).slice(0, 30).map((s) => ({ html: `${kindChip(s.kind)}<span class="t">${esc(s.title || "Untitled")}</span><span class="when">${rel(s.updated)}</span>`, fn: () => openSession(s.id) }));
    results = q ? [...ss, ...acts] : [...acts.slice(0, 1), ...ss.slice(0, 12), ...acts.slice(1)];
    sel = Math.min(sel, results.length - 1);
    $("#pr").innerHTML = results.map((r, i) => `<button class="${i === sel ? "on" : ""}" data-pi="${i}">${r.html}</button>`).join("") || `<div class="panel-empty">No matches</div>`;
  };
  const go = (i) => { const r = results[i]; closeOverlay(); r?.fn(); };
  $("#pq").addEventListener("input", () => { sel = 0; draw(); });
  $("#pq").addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { e.preventDefault(); sel = Math.min(sel + 1, results.length - 1); draw(); $("#pr .on")?.scrollIntoView({ block: "nearest" }); }
    if (e.key === "ArrowUp") { e.preventDefault(); sel = Math.max(sel - 1, 0); draw(); $("#pr .on")?.scrollIntoView({ block: "nearest" }); }
    if (e.key === "Enter") { e.preventDefault(); go(sel); }
  });
  $("#pr").addEventListener("click", (e) => { const b = e.target.closest("[data-pi]"); if (b) go(+b.dataset.pi); });
  draw();
  $("#pq").focus();
}

function toast(text, kind = "", action = null) {
  const el = document.createElement("div");
  el.className = `toast ${kind}`;
  el.innerHTML = `${icon(kind === "err" ? "alert" : "check", "sm")}<span>${esc(text)}</span>`;
  if (action) {
    const b = document.createElement("button");
    b.textContent = action.label;
    b.onclick = () => { el.remove(); action.fn(); };
    el.appendChild(b);
  }
  $("#toasts").appendChild(el);
  setTimeout(() => { el.style.transition = "opacity .3s"; el.style.opacity = "0"; setTimeout(() => el.remove(), 320); }, kind === "err" ? 7000 : 4200);
}

async function pickFolder(title) {
  try { return await api().PickFolder(title); } catch (e) { toast(errText(e), "err"); return ""; }
}

// ---------- events ----------
function onKey(e) {
  const mod = e.ctrlKey || e.metaKey;
  if (mod && e.key.toLowerCase() === "n") { e.preventDefault(); closeOverlay(); nav("home"); }
  else if (mod && e.key.toLowerCase() === "k") { e.preventDefault(); palette(); }
  else if (mod && e.key.toLowerCase() === "b") { e.preventDefault(); toggleSide(); }
  else if (e.key === "Escape") {
    if (menuEl) return closeMenu();
    if ($("#overlay").innerHTML) return closeOverlay();
    if (S.panel) { S.panel = null; renderPanel(); renderTitlebar(); }
  }
}

function toggleSide() {
  S.collapsed = !S.collapsed;
  try { localStorage.setItem("asg.collapsed", S.collapsed ? "1" : "0"); } catch {}
  $("#app").classList.toggle("collapsed", S.collapsed);
  renderTitlebar();
}

async function onGlobalClick(e) {
  const t = e.target;
  if (menuEl && !t.closest(".menu")) closeMenu();
  const q = (sel) => t.closest(sel);
  let el;

  // links inside markdown open in the browser
  if ((el = q(".md a, .panel-body a")) && el.getAttribute("href")) { e.preventDefault(); api().OpenURL(el.getAttribute("href")); return; }
  if ((el = q("[data-copy]"))) {
    const code = el.closest(".codeblock")?.querySelector("pre")?.innerText || "";
    navigator.clipboard.writeText(code).then(() => { el.textContent = "Copied"; setTimeout(() => (el.textContent = "Copy"), 1400); });
    return;
  }
  if ((el = q("[data-scrim]")) && t === el) return closeOverlay();
  if ((el = q("[data-win]"))) {
    const w = el.dataset.win;
    if (w === "min") rt().WindowMinimise(); else if (w === "max") rt().WindowToggleMaximise(); else rt().Quit();
    return;
  }
  if ((el = q("[data-sessmenu]"))) { e.stopPropagation(); return sessionMenu(el, el.dataset.sessmenu); }
  if ((el = q("[data-open]"))) return openSession(el.dataset.open);
  if ((el = q("[data-nav]"))) return nav(el.dataset.nav);
  if ((el = q("[data-project]"))) return nav("project", { id: el.dataset.project });
  if ((el = q("[data-art]"))) return openArtifactPanel(el.dataset.art);
  if ((el = q("[data-artact]"))) return artifactAction(el.dataset.artact, el.dataset.id, el.dataset.sid);
  if ((el = q("[data-reveal]"))) return call("OpenPath", el.dataset.reveal);
  if ((el = q("[data-reveal-dir]"))) return call("OpenPath", el.dataset.revealDir);
  if ((el = q("[data-openpath]"))) return call("OpenPath", el.dataset.openpath);
  if ((el = q("[data-homekind]"))) { S.home.kind = el.dataset.homekind; return renderView(); }
  if ((el = q("[data-suggest]"))) { draft("home").text = el.dataset.suggest; rerenderComposer("home"); return; }
  if ((el = q("[data-rmimg]"))) { const key = el.closest(".composer").dataset.composer; draft(key).images.splice(+el.dataset.rmimg, 1); return rerenderComposer(key); }
  if ((el = q("[data-chatfilter]"))) { S.filter.chats = el.dataset.chatfilter; return renderView(); }
  if ((el = q("[data-artfilter]"))) { S.filter.arts = el.dataset.artfilter; return renderView(); }
  if ((el = q("[data-ptab]"))) { S.panel.tab = el.dataset.ptab; return renderPanel(); }
  if ((el = q("[data-gate]"))) {
    const sid = S.view.id, id = el.dataset.gate, d = el.dataset.d;
    const it = S.live[sid]?.items.find((i) => i.type === "approval" && i.id === id);
    if (it) { it.state = "done"; it.decision = d; }
    renderSessionBody();
    return call("Approve", sid, id, d);
  }
  if ((el = q("[data-ask]")) || (el = q("[data-askinput]"))) {
    const sid = S.view.id;
    const id = el.dataset.ask || el.dataset.askinput;
    const ans = el.dataset.ask ? el.dataset.a : $(`#ask-${CSS.escape(id)}`).value;
    const it = S.live[sid]?.items.find((i) => i.type === "ask" && i.id === id);
    if (it) { it.state = "done"; it.answer = ans; }
    renderSessionBody();
    return call("Answer", sid, id, ans);
  }
  if ((el = q("[data-newcode]"))) return newCodeSession(el.dataset.newcode);
  if ((el = q("[data-schedtoggle]"))) {
    const s = S.schedules.find((x) => x.id === el.dataset.schedtoggle);
    el.classList.toggle("on");
    await call("SaveSchedule", { ...s, enabled: !s.enabled });
    return renderView();
  }
  if ((el = q("[data-schedrun]"))) { const sid = await call("RunScheduleNow", el.dataset.schedrun); toast("Started", "", { label: "Watch", fn: () => openSession(sid) }); return; }
  if ((el = q("[data-schededit]"))) return scheduleForm(S.schedules.find((x) => x.id === el.dataset.schededit));
  if ((el = q("[data-custtab]"))) { S.custTab = el.dataset.custtab; $$("[data-custtab]").forEach((b) => b.classList.toggle("on", b === el)); return renderCustBody(); }
  if ((el = q("[data-settab]"))) { S.setTab = el.dataset.settab; $$("[data-settab]").forEach((b) => b.classList.toggle("on", b === el)); return renderSetBody(); }
  if ((el = q("[data-theme-set]"))) { const th = el.dataset.themeSet; await call("SetTheme", th); S.boot.settings.theme = th; applyTheme(th); return renderSetBody(); }
  if ((el = q("[data-activate]"))) { const cfg = collectProviders(); cfg.active_provider = el.dataset.activate; S.cfg = await call("SaveConfig", cfg); S.model = await api().GetModel(); return renderCustBody(); }
  if ((el = q("[data-rmprov]"))) { const cfg = collectProviders(); if (Object.keys(cfg.providers).length < 2) return toast("Keep at least one provider", "err"); if (!(await confirmBox(`Remove ${el.dataset.rmprov}?`, "Its settings and key are deleted from the config.", "Remove"))) return; delete cfg.providers[el.dataset.rmprov]; S.cfg = await call("SaveConfig", cfg); return renderCustBody(); }
  if ((el = q("[data-pickmodel]"))) return pickModelFor(el.dataset.pickmodel);
  if ((el = q("[data-viewskill]"))) { const s = await call("GetSkill", el.dataset.viewskill); const html = await api().RenderMarkdown(s.body); modal({ title: "/" + s.name, wide: true, body: `<p style="color:var(--muted);margin-top:0">${esc(s.description)}</p><div class="md selectable" style="max-height:50vh;overflow:auto">${html}</div><p class="hint" style="font-size:12px;color:var(--faint)">${s.builtin ? "Built-in: ships with ASG. Create a skill with the same name to replace it." : esc(s.dir)}</p>`, actions: [{ label: "Close" }] }); return; }
  if ((el = q("[data-rmskill]"))) { if (await confirmBox(`Remove /${el.dataset.rmskill}?`, "The skill's folder is deleted.", "Remove")) { await call("DeleteSkill", el.dataset.rmskill); S.skills = null; renderCustBody(); } return; }
  if ((el = q("[data-viewmem]"))) { const [sc, n] = el.dataset.viewmem.split("|"); const note = S.notes.find((x) => x.scope === sc && x.name === n); const html = await api().RenderMarkdown(note?.body || ""); modal({ title: n, body: `<div class="md selectable">${html}</div>`, actions: [{ label: "Close" }] }); return; }
  if ((el = q("[data-rmmem]"))) { const [sc, n] = el.dataset.rmmem.split("|"); if (await confirmBox(`Forget “${n}”?`, "ASG won't see this note any more.", "Delete")) { await call("DeleteMemory", sc, n); renderCustBody(); } return; }
  if ((el = q("[data-rename]")) && e.detail === 2) return renameSession(el.dataset.rename);

  const act = q("[data-act]")?.dataset.act;
  if (!act) return;
  const actEl = q("[data-act]");
  const key = actEl.closest(".composer")?.dataset.composer;
  switch (act) {
    case "new": closeOverlay(); S.home.kind = "chat"; return nav("home");
    case "new-artifact": S.home.kind = "artifact"; return nav("home");
    case "palette": return palette();
    case "toggle-side": return toggleSide();
    case "close-overlay": return closeOverlay();
    case "close-panel": S.panel = null; renderPanel(); return renderTitlebar();
    case "toggle-files":
      if (S.panel?.type === "files") S.panel = null;
      else { const v = await api().OpenSession(S.cur.meta.id); S.cur.work_files = v.work_files; S.cur.artifacts = v.artifacts; S.panel = { type: "files" }; }
      renderPanel();
      return renderTitlebar();
    case "send": return submit(key);
    case "stop": return api().Stop(key);
    case "attach": pickKey = key; return $("#filepick").click();
    case "clear-skill": draft(key).skill = ""; return rerenderComposer(key);
    case "mode": return menu(actEl, Object.entries(MODES).map(([k, [ic, l, d]]) => ({ icon: ic, label: l, sub: d, fn: async () => {
      if (key === "home" || key.startsWith("project:")) S.home.mode = k;
      else { await call("SetSessionMode", key, k); S.cur.meta.mode = k; const h = $("#sess-head"); if (h) h.outerHTML = sessHeadHTML(); }
      rerenderComposer(key);
    } })), "up");
    case "model": return modelMenu(actEl);
    case "home-project": return menu(actEl, [
      { icon: "x", label: "No project", check: !S.home.projectId, fn: () => { S.home.projectId = ""; rerenderComposer("home"); } },
      ...S.projects.map((p) => ({ html: `<span class="pdot pc-${p.color}"></span>`, label: p.name, check: S.home.projectId === p.id, fn: () => { S.home.projectId = p.id; rerenderComposer("home"); } })),
      { sep: true }, { icon: "plus", label: "New project…", fn: () => projectForm() },
    ], "up");
    case "home-folder": {
      const folders = [...new Set(S.sessions.filter((s) => s.kind === "code" && s.workdir).map((s) => s.workdir))].slice(0, 8);
      return menu(actEl, [
        ...(folders.length ? [{ label: "Recent folders", header: true }, ...folders.map((f) => ({ icon: "folder", label: base(f), sub: shortPath(f), check: S.home.workdir === f, fn: () => { S.home.workdir = f; renderView(); } })), { sep: true }] : []),
        { icon: "folderOpen", label: "Choose a folder…", fn: async () => { const d = await pickFolder("Choose the folder to work in"); if (d) { S.home.workdir = d; renderView(); } } },
      ], "up");
    }
    case "new-project": return projectForm();
    case "edit-project": return projectForm(S.projects.find((p) => p.id === actEl.dataset.id));
    case "new-code": return newCodeSession();
    case "new-schedule": return scheduleForm();
    case "add-provider": return modal({
      title: "Add provider",
      body: `<div class="field"><label>Name</label><input class="input" id="np-name" placeholder="e.g. openrouter, lmstudio, work-anthropic"></div>
        <div class="field"><label>Type</label><select class="input" id="np-type"><option value="openai">OpenAI-compatible (OpenAI, OpenRouter, LM Studio, vLLM, Groq…)</option><option value="anthropic">Anthropic</option><option value="ollama">Ollama</option></select></div>
        <div class="field"><label>Base URL</label><input class="input mono" id="np-url" placeholder="https://api.openai.com/v1"></div>`,
      actions: [{ label: "Cancel" }, { label: "Add", cls: "primary", fn: async () => {
        const n = $("#np-name").value.trim(); const ty = $("#np-type").value;
        if (!n) { toast("Give it a name", "err"); return false; }
        const cfg = collectProviders();
        cfg.providers[n] = { type: ty, base_url: $("#np-url").value.trim() || { openai: "https://api.openai.com/v1", anthropic: "https://api.anthropic.com", ollama: "http://localhost:11434" }[ty], model: "", api_key: "" };
        S.cfg = await call("SaveConfig", cfg);
        renderCustBody();
        return true;
      } }],
    });
    case "save-providers": S.cfg = await call("SaveConfig", collectProviders()); S.model = await api().GetModel(); toast("Saved"); return renderCustBody();
    case "save-instructions": { const cfg = await api().GetConfig(); cfg.system_prompt = $("#sys-prompt").value; await call("SaveConfig", cfg); return toast("Instructions saved"); }
    case "save-behaviour": {
      const cfg = await api().GetConfig();
      cfg.auto_approve = $("#bh-auto").classList.contains("on");
      cfg.max_tool_rounds = +$("#bh-rounds").value || 25;
      cfg.review = { ...(cfg.review || {}), passes: +$("#bh-review").value };
      const temp = $("#bh-temp").value;
      cfg.temperature = temp === "" ? null : +temp;
      await call("SaveConfig", cfg);
      return toast("Saved");
    }
    case "new-skill": return modal({
      title: "New skill", wide: true,
      body: `<div class="field"><label>Name</label><input class="input mono" id="sk-name" placeholder="release-notes"></div>
        <div class="field"><label>When to use it</label><input class="input" id="sk-desc" placeholder="Write release notes from the git log since the last tag"></div>
        <div class="field"><label>Instructions (Markdown)</label><textarea class="input mono" id="sk-body" rows="12" placeholder="# Release notes&#10;1. Run git log …"></textarea></div>`,
      actions: [{ label: "Cancel" }, { label: "Create", cls: "primary", fn: async () => { await call("CreateSkill", $("#sk-name").value, $("#sk-desc").value, $("#sk-body").value); S.skills = null; renderCustBody(); return true; } }],
    });
    case "import-skill": return modal({
      title: "Import skills",
      body: `<div class="field"><label>Source</label><input class="input mono" id="sk-src" placeholder="github.com/owner/repo, a URL to SKILL.md, or a local folder"><span class="hint">Every skill found is installed.</span></div>`,
      actions: [{ label: "Cancel" }, { label: "Import", cls: "primary", fn: async () => { const got = await call("ImportSkill", $("#sk-src").value); toast(`Imported ${got?.length || 0} skill(s)`); S.skills = null; renderCustBody(); return true; } }],
    });
    case "reload-tools": { const { errors: errs } = await api().ReloadTools(); if (errs?.length) toast(errs.join("; "), "err"); else toast("Tools reloaded"); return renderCustBody(); }
    case "new-memory": return modal({
      title: "Add memory",
      body: `<div class="row2"><div class="field"><label>Name</label><input class="input" id="mm-name" placeholder="Preferred stack"></div><div class="field"><label>Scope</label><select class="input" id="mm-scope"><option value="user">Everywhere</option><option value="project">Current folder</option></select></div></div>
        <div class="field"><label>Note</label><textarea class="input" id="mm-body" rows="6" placeholder="I use Go 1.26 and deploy with Docker."></textarea></div>`,
      actions: [{ label: "Cancel" }, { label: "Save", cls: "primary", fn: async () => { await call("SaveMemory", $("#mm-scope").value, $("#mm-name").value, $("#mm-body").value); renderCustBody(); return true; } }],
    });
    case "move-data": return changeData(true);
    case "use-data": return changeData(false);
    case "reset-data": { await call("ResetDataDir"); S.boot = await api().Bootstrap(); S.sessions = S.boot.sessions; S.projects = S.boot.projects; S.live = {}; toast("Using the default folder"); return render(); }
  }
}

// search inputs (delegated)
document.addEventListener("input", (e) => {
  if (e.target.id === "chat-q") {
    S.filter.q = e.target.value;
    const pos = e.target.selectionStart;
    renderView();
    const i = $("#chat-q"); i.focus(); i.setSelectionRange(pos, pos);
  } else if (e.target.id === "art-q") {
    S.filter.artQ = e.target.value;
    clearTimeout(window.__aq);
    window.__aq = setTimeout(() => { const pos = $("#art-q").selectionStart; renderView(); const i = $("#art-q"); i.focus(); i.setSelectionRange(pos, pos); }, 150);
  }
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && e.target.id?.startsWith("ask-")) { e.preventDefault(); $(`[data-askinput="${CSS.escape(e.target.id.slice(4))}"]`)?.click(); }
});
document.addEventListener("dblclick", (e) => {
  if (e.target.closest(".titlebar") && !e.target.closest("button")) rt().WindowToggleMaximise();
});

// report script errors to the backend log (desktop.log in the data folder)
window.addEventListener("error", (e) => { try { api().LogJS(`${e.message} @ ${e.filename}:${e.lineno}`); } catch {} });
window.addEventListener("unhandledrejection", (e) => { try { api().LogJS("promise: " + errText(e.reason)); } catch {} });
