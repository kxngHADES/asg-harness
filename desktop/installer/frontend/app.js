// ASG Setup — the wizard UI. Plain JS, no build step. The Go side is
// window.go.main.Setup (see setup.go / install.go); progress arrives as
// "setup:progress" / "setup:done" events.
"use strict";

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

// ---------- icons (24px grid, currentColor) ----------
const I = {
  arrow: '<path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>',
  back: '<path d="M19 12H5"/><path d="m11 18-6-6 6-6"/>',
  check: '<path d="m5 12.5 4.5 4.5L19 7.5"/>',
  x: '<path d="M6 6l12 12"/><path d="M18 6 6 18"/>',
  minus: '<path d="M5 12h14"/>',
  folder: '<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9l-.8-1.2A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2z"/>',
  folderOpen: '<path d="m6 14 1.5-2.9A2 2 0 0 1 9.2 10H20a2 2 0 0 1 1.9 2.5l-1.5 6A2 2 0 0 1 18.4 20H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.7.9l.8 1.2a2 2 0 0 0 1.7.9H18a2 2 0 0 1 2 2v2"/>',
  drive: '<rect x="2.5" y="13" width="19" height="7" rx="2"/><path d="M5.5 13 8 5h8l2.5 8"/><path d="M17 16.5h.01"/><path d="M14 16.5h.01"/>',
  brain: '<path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z"/><path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z"/><path d="M12 5v13"/>',
  monitor: '<rect x="2.5" y="4" width="19" height="13" rx="2"/><path d="M8 21h8"/><path d="M12 17v4"/>',
  grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  power: '<path d="M12 3v9"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/>',
  terminal: '<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="m7.5 9.5 3 2.5-3 2.5"/><path d="M13 15h3.5"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.9 4.9 1.4 1.4"/><path d="m17.7 17.7 1.4 1.4"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.3 17.7-1.4 1.4"/><path d="m19.1 4.9-1.4 1.4"/>',
  moon: '<path d="M20.5 14.5A8.5 8.5 0 0 1 9.5 3.5a8.5 8.5 0 1 0 11 11z"/>',
  key: '<circle cx="7.5" cy="15.5" r="4.5"/><path d="m10.7 12.3 9.8-9.8"/><path d="m17 6 3 3"/><path d="m14.5 8.5 2 2"/>',
  link: '<path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7"/><path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7"/>',
  cube: '<path d="M21 8 12 3 3 8v8l9 5 9-5z"/><path d="m3 8 9 5 9-5"/><path d="M12 13v8"/>',
  star: '<path d="m12 3 2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1-4.4-4.3 6.1-.9z"/>',
  plug: '<path d="M9 2v6"/><path d="M15 2v6"/><path d="M6 8h12v3a6 6 0 0 1-12 0z"/><path d="M12 17v5"/>',
  plus: '<path d="M12 5v14"/><path d="M5 12h14"/>',
  trash: '<path d="M4 7h16"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-12"/><path d="M9 7V4h6v3"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  eyeOff: '<path d="M3 3l18 18"/><path d="M10.6 5.1A10.5 10.5 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-2.6 3.4"/><path d="M6.6 6.6C3.7 8.4 2 12 2 12s3.5 7 10 7a9.7 9.7 0 0 0 5.4-1.6"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/>',
  info: '<circle cx="12" cy="12" r="9.5"/><path d="M12 11v5.5"/><path d="M12 7.5h.01"/>',
  alert: '<path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
  sliders: '<path d="M4 6h10"/><path d="M18 6h2"/><circle cx="16" cy="6" r="2"/><path d="M4 12h4"/><path d="M12 12h8"/><circle cx="10" cy="12" r="2"/><path d="M4 18h12"/><path d="M20 18h0"/><circle cx="18" cy="18" r="2"/>',
  shield: '<path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6z"/><path d="m9 12 2 2 4-4"/>',
  sparkles: '<path d="M10 3.5 11.6 8a2 2 0 0 0 1.3 1.3L17.5 11l-4.6 1.6a2 2 0 0 0-1.3 1.3L10 18.5 8.4 14a2 2 0 0 0-1.3-1.3L2.5 11l4.6-1.6A2 2 0 0 0 8.4 8z"/><path d="M19 3v4"/><path d="M21 5h-4"/>',
  download: '<path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/>',
  rocket: '<path d="M4.5 16.5c-1.5 1.3-2 5-2 5s3.7-.5 5-2c.7-.8.7-2.1-.1-2.9a2.2 2.2 0 0 0-2.9-.1z"/><path d="M12 15l-3-3a22 22 0 0 1 2-3.9A12.9 12.9 0 0 1 22 2c0 2.7-.8 7.5-6 11a22.4 22.4 0 0 1-4 2z"/><path d="M9 12H4s.6-3 2-4c1.6-1.1 5 0 5 0"/><path d="M12 15v5s3-.6 4-2c1.1-1.6 0-5 0-5"/>',
  refresh: '<path d="M21 12a9 9 0 1 1-2.6-6.4L21 8"/><path d="M21 3v5h-5"/>',
};
const icon = (n, extra = "") => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" ${extra}>${I[n] || ""}</svg>`;

// ---------- backend (with a mock so the page can be opened in a browser) ----------
const api = () => window.go?.main?.Setup || MOCK;
const on = (name, fn) => (window.runtime ? window.runtime.EventsOn(name, fn) : MOCK.listen(name, fn));

// ---------- state ----------
const FLOW = [
  { id: "welcome", label: "Welcome" },
  { id: "location", label: "Location" },
  { id: "shortcuts", label: "Shortcuts" },
  { id: "storage", label: "Storage" },
  { id: "providers", label: "Providers" },
  { id: "review", label: "Review" },
  { id: "install", label: "Install" },
];
const UFLOW = [
  { id: "confirm", label: "Confirm" },
  { id: "remove", label: "Remove" },
  { id: "bye", label: "Done" },
];

const S = {
  info: null,
  mode: "install",
  step: 0,
  opts: null,
  errors: {},
  tests: {},       // provider name → { busy, ok, models, error }
  showKey: {},
  steps: { install: [], uninstall: [] },
  run: { state: "idle", pct: 0, step: 0, detail: "", error: "" },
  removeData: false,
};

const flow = () => (S.mode === "uninstall" ? UFLOW : FLOW);
const cur = () => flow()[S.step].id;

const PROV = {
  ollama: { label: "Ollama", logo: icon("cube"), cls: "l-ollama", blurb: "Open models on this machine. Free, private, no key.", url: "https://ollama.com/download", urlLabel: "Get Ollama" },
  anthropic: { label: "Anthropic", logo: "A", cls: "l-anthropic", blurb: "Claude models through the Anthropic API.", url: "https://console.anthropic.com/settings/keys", urlLabel: "Get a key" },
  openai: { label: "OpenAI", logo: icon("sparkles"), cls: "l-openai", blurb: "GPT models through the OpenAI API.", url: "https://platform.openai.com/api-keys", urlLabel: "Get a key" },
};
const PRESETS = [
  { name: "openrouter", label: "OpenRouter", base: "https://openrouter.ai/api/v1" },
  { name: "groq", label: "Groq", base: "https://api.groq.com/openai/v1" },
  { name: "lmstudio", label: "LM Studio", base: "http://localhost:1234/v1" },
  { name: "custom", label: "Other", base: "https://" },
];

// ---------- boot ----------
async function boot() {
  S.info = await api().Info();
  S.steps = await api().Steps();
  S.mode = S.info.mode;
  const i = S.info;
  document.documentElement.classList.add("os-" + i.os);
  S.opts = {
    install_dir: i.current.install_dir,
    data_dir: i.current.data_dir,
    memory_dir: i.current.memory_dir,
    desktop_shortcut: i.features.desktop_shortcut,
    menu_shortcut: i.features.menu_shortcut,
    launch_at_login: false,
    add_to_path: i.features.add_to_path,
    theme: i.theme || "system",
    providers: i.providers.map((p) => ({ ...p, api_key: "" })),
    active: i.active,
  };
  on("setup:progress", onProgress);
  on("setup:done", onDone);
  render();
}

// ---------- rendering ----------
function render(animate = true) {
  renderTitle();
  renderRail();
  const el = $("#screen");
  el.classList.toggle("static", !animate);
  el.innerHTML = SCREENS[cur()]();
  if (animate) el.scrollTop = 0;
  renderFoot();
  afterRender();
}

function renderTitle() {
  const i = S.info;
  const win = i.os === "darwin" ? "" : `<div class="winctl"><button data-win="min" title="Minimise">${icon("minus")}</button><button class="close" data-win="close" title="Close">${icon("x")}</button></div>`;
  $("#titlebar").innerHTML = `<span class="tag"><b>ASG</b> ${S.mode === "uninstall" ? "Uninstall" : "Setup"}</span><span class="pill">${esc(i.version)}</span>${i.preview ? '<span class="pill warn">preview</span>' : ""}<span class="grow"></span>${win}`;
}

function renderRail() {
  const steps = flow();
  const running = S.run.state === "running";
  const finished = S.run.state === "ok";
  const danger = S.mode === "uninstall";
  $("#rail").innerHTML = `
    <div class="brand">
      <div class="crab-wrap"><img src="${running ? "crab-working.svg" : "crab.svg"}" alt=""></div>
      <div><h1><span>ASG</span></h1><p>${danger ? "Sorry to see you go" : "The agent harness, on your desktop"}</p></div>
    </div>
    <ol class="steps"><span class="fill"></span>
      ${steps.map((s, k) => {
        const done = k < S.step || (finished && k === S.step);
        const clickable = !danger && k < S.step && S.run.state === "idle";
        return `<li class="${done ? "done" : ""} ${k === S.step && !done ? "on" : ""} ${clickable ? "clickable" : ""} ${danger ? "danger" : ""}" ${clickable ? `data-goto="${k}"` : ""}>
          <span class="node">${done ? icon("check") : String(k + 1).padStart(2, "0")}</span>${s.label}</li>`;
      }).join("")}
    </ol>
    <div class="rail-foot">
      <div class="row"><span class="dot"></span>${esc(i18nOS(S.info.os))} · ${esc(S.info.arch)}</div>
      <div style="margin-top:6px">Settings file<br><span class="mono">${esc(S.info.config_path)}</span></div>
    </div>`;
  requestAnimationFrame(() => {
    const lis = $$("#rail .steps li");
    const fill = $("#rail .fill");
    if (!lis.length || !fill) return;
    const idx = Math.min(S.step + (finished ? 0 : 0), lis.length - 1);
    const first = lis[0], at = lis[idx];
    fill.style.top = first.offsetTop + first.offsetHeight / 2 + "px";
    fill.style.height = at.offsetTop - first.offsetTop + "px";
  });
}

const i18nOS = (os) => ({ windows: "Windows", darwin: "macOS", linux: "Linux" })[os] || os;

function renderFoot() {
  const meta = (t) => `<span class="meta">${t}</span>`;
  const back = `<button class="btn ghost" data-act="back">${icon("back")}Back</button>`;
  const next = (label = "Continue") => `<button class="btn primary" data-act="next">${label}${icon("arrow", 'class="arrow"')}</button>`;
  const f = $("#foot");
  const id = cur();
  const pos = `Step <b>${S.step + 1}</b> of <b>${flow().length}</b>`;
  if (id === "welcome") {
    f.innerHTML = meta(S.info.payload_bytes ? `Needs about <b>${bytes(S.info.payload_bytes)}</b>` : `<b>${esc(S.info.version)}</b>`) + `<button class="btn ghost" data-act="quit">Cancel</button>` + next(S.info.existing ? "Reinstall" : "Get started");
  } else if (id === "review") {
    f.innerHTML = meta(pos) + back + `<button class="btn primary" data-act="install">${icon("download")}Install ASG</button>`;
  } else if (id === "install" || id === "remove") {
    const r = S.run;
    if (r.state === "running") f.innerHTML = meta(S.mode === "uninstall" ? "Removing ASG…" : "Installing — keep this window open") + `<button class="btn" disabled><span class="spin"></span>Working</button>`;
    else if (r.state === "fail") f.innerHTML = meta("Something went wrong") + (S.mode === "uninstall" ? `<button class="btn ghost" data-act="quit">Close</button>` : `<button class="btn ghost" data-act="goto-review">${icon("back")}Change settings</button>`) + `<button class="btn primary" data-act="retry">${icon("refresh")}Try again</button>`;
    else if (r.state === "ok" && S.mode === "install") f.innerHTML = meta("All done") + `<button class="btn ghost" data-act="quit">Close</button><button class="btn primary" data-act="launch">${icon("rocket")}Launch ASG</button>`;
    else f.innerHTML = meta("");
  } else if (id === "confirm") {
    f.innerHTML = meta(esc(S.info.version)) + (S.info.mode === "uninstall" ? `<button class="btn ghost" data-act="quit">Cancel</button>` : `<button class="btn ghost" data-act="to-install">${icon("back")}Back</button>`) + `<button class="btn danger" data-act="uninstall" ${S.info.existing ? "" : "disabled"}>${icon("trash")}Uninstall</button>`;
  } else if (id === "bye") {
    f.innerHTML = meta("") + `<button class="btn primary" data-act="quit">Close</button>`;
  } else {
    f.innerHTML = meta(pos) + back + next();
  }
}

// ---------- screens ----------
const head = (kicker, title, lede, red) => `<div class="kicker ${red ? "red" : ""}">${kicker}</div><h2 class="title">${title}</h2>${lede ? `<p class="lede">${lede}</p>` : ""}`;

const SCREENS = {
  welcome() {
    const i = S.info;
    const req = { windows: "Uses Microsoft Edge WebView2, which Windows 10 and 11 already include. No administrator rights needed for the default folder.", darwin: "Needs macOS 11 or later. Works on Apple Silicon and Intel.", linux: "Needs GTK 3 and WebKitGTK 4.1 — on Debian/Ubuntu: <code>sudo apt install libwebkit2gtk-4.1-0</code>." }[i.os] || "";
    return `${head("Welcome", "Let's put <em>ASG</em> on this machine.", "A few quick choices — where it lives, where your sessions and memory go, and which models it talks to. About a minute, start to finish.")}
      ${i.existing ? `<div class="existing"><div class="ic">${icon("shield")}</div><div class="t"><b>ASG ${esc(i.existing.version)} is already installed</b><span>${esc(i.existing.install_dir)}</span></div><button class="btn sm" data-act="to-uninstall">${icon("trash")}Uninstall</button></div>` : ""}
      <div class="tiles">
        <div class="tile c-lime"><div class="ic">${icon("folderOpen")}</div><b>Your folders</b><span>Pick where the app, your sessions and memory notes are kept.</span></div>
        <div class="tile c-cyan"><div class="ic">${icon("plug")}</div><b>Your models</b><span>Local Ollama, Anthropic, OpenAI or any OpenAI-compatible API.</span></div>
        <div class="tile c-mag"><div class="ic">${icon("sliders")}</div><b>Change it later</b><span>Every choice here can be changed in ASG's Customise and Settings.</span></div>
      </div>
      ${i.preview ? `<div class="notice amber">${icon("alert")}<div class="grow"><b>Preview build.</b> There's no app inside this build, so Install only walks through the steps — nothing on your computer is changed.</div></div>` : ""}
      ${req ? `<div class="notice">${icon("info")}<div class="grow">${req}</div></div>` : ""}`;
  },

  location() {
    const o = S.opts, i = S.info;
    return `${head("Location", "Where should ASG live?", "The app goes in one folder. The default needs no administrator rights.")}
      <div class="folder-card">
        <div class="head"><div class="ic" style="background:var(--lime-soft);color:var(--lime)">${icon("download")}</div><div><b>Install folder</b><span>${i.os === "darwin" ? "ASG.app is placed here." : "The ASG program files are placed here."}</span></div></div>
        ${pathField("install_dir", "Install folder", o.install_dir, i.defaults.install_dir)}
      </div>
      <div class="notice cyan">${icon("info")}<div class="grow">Updating? Point this at your current install folder and the app is replaced in place — your sessions and settings aren't touched.</div></div>`;
  },

  shortcuts() {
    const o = S.opts, f = S.info.features;
    const opt = (key, ic, title, sub) => `<button class="opt ${o[key] ? "on" : ""}" data-toggle="${key}"><span class="ic">${icon(ic)}</span><span class="t"><b>${title}</b><span>${sub}</span></span><span class="switch"></span></button>`;
    const themes = [
      ["system", "monitor", "System", ["#121211", "#f7f6f1", "#c6f432"]],
      ["dark", "moon", "Dark", ["#121211", "#20201d", "#c6f432"]],
      ["light", "sun", "Light", ["#ecebe4", "#ffffff", "#c6f432"]],
    ];
    return `${head("Shortcuts", "How do you want to open it?", "Choose where ASG shows up. You can pin it to the taskbar or dock yourself later.")}
      <div class="opts">
        ${f.desktop_shortcut ? opt("desktop_shortcut", "monitor", "Desktop shortcut", "An ASG icon on your desktop.") : ""}
        ${f.menu_shortcut ? opt("menu_shortcut", "grid", `${f.menu_label[0].toUpperCase() + f.menu_label.slice(1)} entry`, `Find ASG in the ${esc(f.menu_label)}.`) : ""}
        ${f.launch_at_login ? opt("launch_at_login", "power", "Start when I sign in", "ASG opens when you sign in, so your schedules keep running.") : ""}
        ${f.add_to_path ? opt("add_to_path", "terminal", "Add to PATH", "Run <code>asg-desktop</code> from a terminal (~/.local/bin).") : ""}
      </div>
      <div class="section-h">Theme</div>
      <div class="themes">${themes.map(([k, ic, label, c]) => `<button class="theme ${o.theme === k ? "on" : ""}" data-theme="${k}"><div class="pv"><i style="flex:1.2;background:${c[0]}"></i><i style="flex:3;background:${c[1]};position:relative"></i><i style="width:14px;background:${c[2]}"></i></div><b>${icon(ic)}${label}</b></button>`).join("")}</div>`;
  },

  storage() {
    const o = S.opts, d = S.info.defaults, sep = S.info.os === "windows" ? "\\" : "/";
    return `${head("Storage", "Where should your work be kept?", "Two folders, both plain files you own. Put them on another drive or a synced folder if you like.")}
      <div class="folder-card">
        <div class="head"><div class="ic" style="background:var(--cyan-soft);color:var(--cyan)">${icon("drive")}</div><div><b>Data folder</b><span>Sessions you can resume, artifacts the agent made, project files and each chat's private sandbox.</span></div></div>
        ${pathField("data_dir", "Data folder", o.data_dir, d.data_dir)}
        <div class="tree"><i>${esc(shortPath(o.data_dir))}${sep}</i><br>├─ sessions${sep} &nbsp;&nbsp;full chat history<br>├─ artifacts${sep} &nbsp;pages, docs, images — every version<br>└─ sandbox${sep} &nbsp;&nbsp;&nbsp;where plain chats run commands</div>
      </div>
      <div class="folder-card">
        <div class="head"><div class="ic" style="background:var(--magenta-soft);color:var(--magenta)">${icon("brain")}</div><div><b>Memory folder</b><span>Notes the agent keeps between sessions — your preferences and project facts, one Markdown file each. Shared with the <code>asg</code> terminal app.</span></div></div>
        ${pathField("memory_dir", "Memory folder", o.memory_dir, d.memory_dir)}
      </div>
      <div class="notice lime">${icon("sliders")}<div class="grow">Change your mind later? <b>Settings → Storage &amp; sandbox</b> in ASG can move both folders at any time.</div></div>`;
  },

  providers() {
    return `${head("Providers", "Which models should ASG use?", "Switch on at least one. Keys stay on this computer, in your ASG settings file.")}
      <div class="notice lime">${icon("sparkles")}<div class="grow"><b>This isn't final.</b> You can add more providers, switch models or change keys any time in ASG under <b>Customise → Providers</b>.</div></div>
      <div class="provs" id="provs">${S.opts.providers.map(provCard).join("")}</div>
      <div class="presets"><span class="lbl">Add an OpenAI-compatible provider</span>${PRESETS.map((p) => `<button class="preset" data-preset="${p.name}">${icon("plus")}${p.label}</button>`).join("")}</div>
      ${S.errors.providers ? `<div class="notice red">${icon("alert")}<div class="grow">${esc(S.errors.providers)}</div></div>` : ""}`;
  },

  review() {
    const o = S.opts, f = S.info.features;
    const li = (onOff, label) => `<li class="${onOff ? "" : "off"}">${icon(onOff ? "check" : "x")}${label}</li>`;
    const provs = o.providers.filter((p) => p.enabled);
    return `${head("Review", "Ready when you are.", "Here's what setup will do. Jump back to change anything.")}
      <div class="review">
        <div class="rv wide"><div class="h">${icon("download")}Install to<a data-goto="1">Edit</a></div><div class="v">${esc(o.install_dir)}</div></div>
        <div class="rv"><div class="h">${icon("drive")}Data<a data-goto="3">Edit</a></div><div class="v">${esc(o.data_dir)}</div></div>
        <div class="rv"><div class="h">${icon("brain")}Memory<a data-goto="3">Edit</a></div><div class="v">${esc(o.memory_dir)}</div></div>
        <div class="rv"><div class="h">${icon("monitor")}Shortcuts<a data-goto="2">Edit</a></div><ul>
          ${f.desktop_shortcut ? li(o.desktop_shortcut, "Desktop") : ""}
          ${f.menu_shortcut ? li(o.menu_shortcut, esc(f.menu_label)) : ""}
          ${f.launch_at_login ? li(o.launch_at_login, "Start at sign-in") : ""}
          ${f.add_to_path ? li(o.add_to_path, "PATH") : ""}
          <li>${icon(o.theme === "light" ? "sun" : o.theme === "dark" ? "moon" : "monitor")}${o.theme[0].toUpperCase() + o.theme.slice(1)} theme</li>
        </ul></div>
        <div class="rv"><div class="h">${icon("plug")}Providers<a data-goto="4">Edit</a></div><ul>
          ${provs.map((p) => `<li>${icon(p.name === o.active ? "star" : "check")}${esc(provLabel(p))} · <span class="mono">${esc(p.model)}</span></li>`).join("")}
        </ul></div>
      </div>`;
  },

  install() {
    const r = S.run;
    if (r.state === "ok") return doneScreen();
    const steps = S.steps.install;
    return `${head(r.state === "fail" ? "Install failed" : "Installing", r.state === "fail" ? "That didn't work." : "Setting things up…", r.state === "fail" ? "Nothing you chose is lost — fix the problem below and try again." : "This only takes a moment.", r.state === "fail")}
      ${r.state === "fail" ? `<div class="errbox">${esc(r.error)}</div>` : ""}
      ${runView(steps)}`;
  },

  confirm() {
    const e = S.info.existing;
    return `${head("Uninstall", "Remove ASG from this computer?", e ? "The app, its shortcuts and its start-up entry are removed." : "No ASG install was found for this user.", true)}
      ${e ? `<div class="existing" style="border-color:rgba(255,107,94,.3);background:linear-gradient(100deg,rgba(255,107,94,.08),rgba(22,24,20,.6) 70%)"><div class="ic" style="background:var(--red-soft);color:var(--red)">${icon("trash")}</div><div class="t"><b>ASG ${esc(e.version)}</b><span>${esc(e.install_dir)}</span></div></div>` : ""}
      <div class="folder-card">
        <button class="check red ${S.removeData ? "on" : ""}" data-act="toggle-data"><span class="box">${icon("check")}</span><span><b style="color:var(--text)">Also delete my sessions, artifacts and memory notes</b></span></button>
        <p class="hint" style="margin:8px 0 0 30px;font-size:12.5px;color:var(--muted)">This can't be undone. Your provider settings (<code>config.json</code>) are kept either way — the <code>asg</code> terminal app uses them too.</p>
      </div>
      ${S.info.preview && S.info.mode !== "uninstall" ? `<div class="notice amber">${icon("alert")}<div class="grow"><b>Preview build</b> — uninstall only walks through the steps.</div></div>` : ""}`;
  },

  remove() {
    const r = S.run;
    return `${head(r.state === "fail" ? "Uninstall failed" : "Uninstalling", r.state === "fail" ? "Some files couldn't be removed." : "Removing ASG…", "", true)}
      ${r.state === "fail" ? `<div class="errbox">${esc(r.error)}</div>` : ""}
      ${runView(S.steps.uninstall)}`;
  },

  bye() {
    return `<div class="done-hero"><div class="badge-ok">${icon("check")}</div><div>${head("Uninstalled", "ASG has been removed.", S.removeData ? "The app and your data are gone." : "Your sessions and memory notes are still where you left them, in case you come back.")}</div></div>
      <div class="notice">${icon("info")}<div class="grow">Changed your mind? Run the setup again any time — it picks up your old settings.</div></div>`;
  },
};

function doneScreen() {
  const active = S.opts.providers.find((p) => p.name === S.opts.active && p.enabled) || S.opts.providers.find((p) => p.enabled);
  return `<div class="done-hero"><div class="badge-ok">${icon("check")}</div><div>${head("Installed", "ASG is ready.", `Launch it and start a chat${active ? ` with <b>${esc(provLabel(active))}</b>` : ""}.`)}</div></div>
    <div class="tiles">
      <div class="tile c-lime"><div class="ic">${icon("sparkles")}</div><b>New chat</b><span><span class="kbd">Ctrl N</span> starts one. Pick Chat, Code or Artifact.</span></div>
      <div class="tile c-cyan"><div class="ic">${icon("plug")}</div><b>More providers</b><span>Add or change them any time in <b>Customise → Providers</b>.</span></div>
      <div class="tile c-mag"><div class="ic">${icon("drive")}</div><b>Move your data</b><span><b>Settings → Storage</b> moves sessions and memory.</span></div>
    </div>
    ${S.info.preview ? `<div class="notice amber">${icon("alert")}<div class="grow"><b>Preview build</b> — nothing was installed.</div></div>` : ""}`;
}

function runView(steps) {
  const r = S.run, C = 2 * Math.PI * 98;
  return `<div class="run">
      <div class="ring ${r.state === "fail" ? "fail" : r.state === "ok" ? "ok" : ""}">
        <svg class="r" viewBox="0 0 230 230">
          <defs><linearGradient id="rg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#e3ff8a"/><stop offset=".55" stop-color="#c6f432"/><stop offset="1" stop-color="#5fe3f5"/></linearGradient></defs>
          <circle class="ticks" cx="115" cy="115" r="112" fill="none" stroke-width="1" stroke-dasharray="1 8.6"/>
          <circle class="track" cx="115" cy="115" r="98" fill="none" stroke-width="10"/>
          <circle class="bar" cx="115" cy="115" r="98" fill="none" stroke-width="10" stroke-dasharray="${C}" stroke-dashoffset="${C * (1 - r.pct / 100)}"/>
          ${r.state === "running" ? `<g class="orbit"><circle cx="115" cy="5" r="3" fill="#5fe3f5"/></g>` : ""}
        </svg>
        <div class="center"><div><img src="${r.state === "running" ? "crab-working.svg" : "crab.svg"}" alt=""><div class="pct"><span id="pct">${Math.round(r.pct)}</span><small>%</small></div></div></div>
      </div>
      <div>
        <ul class="checklist">${steps.map((s, k) => `<li class="${stepClass(k)}"><span class="b">${k < r.step || r.state === "ok" ? icon("check") : r.state === "fail" && k === r.step ? icon("x") : ""}</span>${esc(s)}</li>`).join("")}</ul>
        <div class="detail" id="detail">${esc(r.detail || (r.state === "fail" ? "stopped" : "starting…"))}</div>
      </div>
    </div>`;
}

function stepClass(k) {
  const r = S.run;
  if (r.state === "ok" || k < r.step) return "done";
  if (k === r.step) return r.state === "fail" ? "fail" : r.state === "running" ? "on" : "";
  return "";
}

function pathField(key, label, value, def) {
  const err = S.errors[key];
  const isDef = samePath(value, def);
  return `<div class="field">
      <div class="path">
        <div class="input ${err ? "bad" : ""}">${icon("folder")}<input class="mono" data-path="${key}" value="${esc(value)}" spellcheck="false" aria-label="${esc(label)}"></div>
        <button class="btn" data-pick="${key}">${icon("folderOpen")}Browse…</button>
        ${isDef ? "" : `<button class="btn ghost" data-default="${key}" title="Use ${esc(def)}">Default</button>`}
      </div>
      ${err ? `<div class="err">${icon("alert")}<span>${esc(err)}</span></div>` : `<div class="hint">${isDef ? "Default location." : "Custom location."}</div>`}
    </div>`;
}

function provLabel(p) {
  if (PROV[p.name]) return PROV[p.name].label;
  const pre = PRESETS.find((x) => x.name === p.name.replace(/-\d+$/, ""));
  return pre && pre.name !== "custom" ? pre.label : p.name;
}

function provCard(p, idx) {
  const meta = PROV[p.name] || { label: provLabel(p), logo: icon("plug"), cls: "l-custom", blurb: "OpenAI-compatible endpoint." };
  const t = S.tests[p.name] || {};
  const std = !!PROV[p.name];
  const active = S.opts.active === p.name;
  const needsKey = p.type !== "ollama";
  const status = t.busy ? `<span class="status"><span class="spin"></span>Connecting…</span>`
    : t.ok ? `<span class="status ok">${icon("check")}Connected · ${t.models.length} model${t.models.length === 1 ? "" : "s"}</span>`
    : t.error ? `<span class="status bad" title="${esc(t.error)}">${icon("alert")}${esc(t.error.length > 90 ? t.error.slice(0, 90) + "…" : t.error)}</span>` : "";
  const chip = !p.enabled ? "" : active ? `<span class="chip lime">default</span>` : p.has_key ? `<span class="chip cyan">key saved</span>` : "";
  return `<div class="prov ${p.enabled ? "on" : ""} ${active ? "active" : ""}" data-prov="${idx}">
      <div class="top" data-act="prov-toggle">
        <div class="logo ${meta.cls}">${meta.logo}</div>
        <div class="t"><b>${esc(meta.label)}${chip}</b><span>${esc(meta.blurb)}</span></div>
        ${std ? "" : `<button class="remove" data-act="prov-remove" title="Remove">${icon("trash")}</button>`}
        <span class="switch ${p.enabled ? "on" : ""}"></span>
      </div>
      <div class="body"><div><div class="inner">
        ${std ? "" : `<div class="field"><label>Name</label><div class="input"><input data-pf="name" value="${esc(p.name)}" spellcheck="false"></div></div>`}
        <div class="field ${std ? "full" : ""}"><label>Address</label><div class="input">${icon("link")}<input class="mono" data-pf="base_url" value="${esc(p.base_url)}" spellcheck="false"></div></div>
        ${needsKey ? `<div class="field"><label>API key ${p.has_key ? '<span class="badge">saved</span>' : ""}</label><div class="input">${icon("key")}<input class="mono" data-pf="api_key" type="${S.showKey[p.name] ? "text" : "password"}" value="${esc(p.api_key)}" placeholder="${p.has_key ? "Saved key is kept — type to replace" : p.name === "anthropic" ? "sk-ant-…" : "sk-…"}" spellcheck="false" autocomplete="off"><button class="eye" data-act="eye" title="Show / hide">${icon(S.showKey[p.name] ? "eyeOff" : "eye")}</button></div></div>` : ""}
        <div class="field"><label>Model</label><div class="input">${icon("cube")}<input class="mono" data-pf="model" value="${esc(p.model)}" list="ml-${idx}" placeholder="Test the connection to list models" spellcheck="false"></div><datalist id="ml-${idx}">${(t.models || []).map((m) => `<option value="${esc(m)}">`).join("")}</datalist></div>
        <div class="row">
          <button class="btn sm" data-act="prov-test" ${t.busy ? "disabled" : ""}>${icon("plug")}Test connection</button>
          <button class="star ${active ? "on" : ""}" data-act="prov-default">${icon("star")}${active ? "Default" : "Make default"}</button>
          ${meta.url ? `<a data-url="${meta.url}">${esc(meta.urlLabel)} ↗</a>` : ""}
          ${status}
        </div>
      </div></div></div>
    </div>`;
}

function afterRender() {
  const first = $("#screen input[data-path]");
  if (first && cur() === "location") first.focus({ preventScroll: true });
}

// ---------- progress ----------
function onProgress(p) {
  S.run.pct = Math.max(S.run.pct, p.pct);
  S.run.step = p.step;
  S.run.detail = p.detail || p.label;
  patchRun();
}

function onDone(d) {
  if (d.ok) {
    S.run.state = "ok";
    S.run.pct = 100;
    if (S.mode === "uninstall") S.step = 2;
    render();
    if (S.mode === "install") confetti();
  } else {
    S.run.state = "fail";
    S.run.error = d.error || "Unknown error";
    render(false);
  }
}

// Update the progress view in place so the ring animates smoothly.
function patchRun() {
  const C = 2 * Math.PI * 98;
  const bar = $(".ring .bar");
  if (!bar) return render(false);
  bar.style.strokeDashoffset = C * (1 - S.run.pct / 100);
  $("#pct").textContent = Math.round(S.run.pct);
  $("#detail").textContent = S.run.detail || "";
  $$(".checklist li").forEach((li, k) => {
    const cls = stepClass(k);
    if (li.className !== cls) {
      li.className = cls;
      $(".b", li).innerHTML = cls === "done" ? icon("check") : "";
    }
  });
}

// ---------- actions ----------
async function next() {
  const id = cur();
  if (id === "location" && !(await checkPaths(["install_dir"]))) return;
  if (id === "storage" && !(await checkPaths(["data_dir", "memory_dir"]))) return;
  if (id === "providers" && !checkProviders()) return;
  S.step = Math.min(S.step + 1, flow().length - 1);
  render();
}

function back() {
  if (S.step > 0) { S.step--; render(); }
}

async function checkPaths(keys) {
  const bad = await api().CheckPaths({ install_dir: S.opts.install_dir, data_dir: S.opts.data_dir, memory_dir: S.opts.memory_dir });
  S.errors = {};
  keys.forEach((k) => { if (bad[k]) S.errors[k] = bad[k]; });
  if (Object.keys(S.errors).length) { render(false); return false; }
  return true;
}

function checkProviders() {
  const on = S.opts.providers.filter((p) => p.enabled);
  let err = "";
  if (!on.length) err = "Switch on at least one provider — local Ollama is a good start.";
  for (const p of on) {
    if (err) break;
    if (!/^https?:\/\/.+/.test(p.base_url.trim())) err = `${provLabel(p)}: the address must start with http:// or https://.`;
    else if (!p.model.trim()) err = `${provLabel(p)}: pick a model (Test connection lists them).`;
    else if (!/^[a-z0-9][a-z0-9_-]{0,39}$/.test(p.name)) err = `"${p.name}": use lowercase letters, digits, - or _ for the name.`;
  }
  const names = on.map((p) => p.name);
  if (!err && new Set(names).size !== names.length) err = "Two providers have the same name.";
  S.errors.providers = err;
  if (err) { render(false); return false; }
  if (!on.some((p) => p.name === S.opts.active)) S.opts.active = on[0].name;
  return true;
}

async function startInstall() {
  S.run = { state: "running", pct: 0, step: 0, detail: "", error: "" };
  S.step = FLOW.length - 1;
  render();
  try {
    await api().Install({ ...S.opts, providers: S.opts.providers.map((p) => ({ ...p, name: p.name.trim().toLowerCase() })) });
  } catch (e) {
    onDone({ ok: false, error: errText(e) });
  }
}

async function startUninstall() {
  S.run = { state: "running", pct: 0, step: 0, detail: "", error: "" };
  S.step = 1;
  render();
  try {
    await api().Uninstall(S.removeData);
  } catch (e) {
    onDone({ ok: false, error: errText(e) });
  }
}

async function testProvider(idx) {
  const p = S.opts.providers[idx];
  S.tests[p.name] = { busy: true };
  rerenderProv(idx);
  const r = await api().TestProvider(p);
  S.tests[p.name] = { ok: r.ok, models: r.models || [], error: r.error };
  if (r.ok && r.models?.length && !r.models.includes(p.model)) {
    // Keep the user's model when the provider lists it; otherwise suggest the first.
    if (!p.model) p.model = r.models[0];
  }
  rerenderProv(idx);
}

function rerenderProv(idx) {
  const el = $(`.prov[data-prov="${idx}"]`);
  if (!el) return;
  const tmp = document.createElement("div");
  tmp.innerHTML = provCard(S.opts.providers[idx], idx);
  el.replaceWith(tmp.firstElementChild);
}

function addPreset(name) {
  const pre = PRESETS.find((p) => p.name === name);
  let n = pre.name, k = 2;
  while (S.opts.providers.some((p) => p.name === n)) n = `${pre.name}-${k++}`;
  S.opts.providers.push({ name: n, type: "openai", base_url: pre.base, api_key: "", model: "", enabled: true, has_key: false });
  render(false);
  const el = $(`.prov[data-prov="${S.opts.providers.length - 1}"]`);
  el?.scrollIntoView({ behavior: reduced ? "auto" : "smooth", block: "center" });
  $("input[data-pf='api_key'], input[data-pf='model']", el)?.focus({ preventScroll: true });
}

async function pick(key) {
  const titles = { install_dir: "Choose where to install ASG", data_dir: "Choose where ASG keeps sessions and artifacts", memory_dir: "Choose where ASG keeps memory notes" };
  const dir = await api().PickFolder(titles[key], S.opts[key]);
  if (!dir) return;
  // Picking a parent like "Programs" for the app: put it in an ASG subfolder.
  S.opts[key] = key === "install_dir" && S.info.os !== "darwin" && !/asg/i.test(dir.split(/[\\/]/).pop()) ? joinPath(dir, "ASG") : dir;
  delete S.errors[key];
  render(false);
}

// ---------- events ----------
document.addEventListener("click", async (e) => {
  const t = e.target.closest("[data-act],[data-goto],[data-toggle],[data-theme],[data-pick],[data-default],[data-preset],[data-url],[data-win]");
  if (!t) return;
  if (t.dataset.win) return t.dataset.win === "min" ? window.runtime?.WindowMinimise() : api().Quit();
  if (t.dataset.goto !== undefined) { S.step = +t.dataset.goto; return render(); }
  if (t.dataset.url) return api().OpenURL(t.dataset.url);
  if (t.dataset.toggle) {
    const k = t.dataset.toggle;
    S.opts[k] = !S.opts[k];
    t.classList.toggle("on", S.opts[k]);
    return;
  }
  if (t.dataset.theme) {
    S.opts.theme = t.dataset.theme;
    $$(".theme").forEach((b) => b.classList.toggle("on", b.dataset.theme === S.opts.theme));
    return;
  }
  if (t.dataset.pick) return pick(t.dataset.pick);
  if (t.dataset.default) { S.opts[t.dataset.default] = S.info.defaults[t.dataset.default]; delete S.errors[t.dataset.default]; return render(false); }
  if (t.dataset.preset) return addPreset(t.dataset.preset);

  const card = t.closest(".prov");
  const idx = card ? +card.dataset.prov : -1;
  const p = idx >= 0 ? S.opts.providers[idx] : null;
  switch (t.dataset.act) {
    case "next": return next();
    case "back": return back();
    case "quit": return api().Quit();
    case "install": return startInstall();
    case "retry": return S.mode === "uninstall" ? startUninstall() : startInstall();
    case "goto-review": S.run.state = "idle"; S.step = FLOW.length - 2; return render();
    case "launch": try { await api().Launch(); } catch (err) { toast(errText(err), true); } return;
    case "to-uninstall": S.mode = "uninstall"; S.step = 0; return render();
    case "to-install": S.mode = "install"; S.step = 0; return render();
    case "uninstall": return startUninstall();
    case "toggle-data": S.removeData = !S.removeData; t.classList.toggle("on", S.removeData); return;
    case "prov-toggle":
      if (e.target.closest("button.remove")) return;
      p.enabled = !p.enabled;
      if (p.enabled && !S.opts.providers.some((x) => x.enabled && x.name === S.opts.active)) S.opts.active = p.name;
      S.errors.providers = "";
      return rerenderProv(idx);
    case "prov-remove":
      S.opts.providers.splice(idx, 1);
      if (S.opts.active === p.name) S.opts.active = S.opts.providers.find((x) => x.enabled)?.name || "";
      return render(false);
    case "prov-default":
      S.opts.active = p.name;
      S.opts.providers.forEach((_, k) => rerenderProv(k));
      return;
    case "prov-test": return testProvider(idx);
    case "eye": S.showKey[p.name] = !S.showKey[p.name]; return rerenderProv(idx);
  }
});

document.addEventListener("input", (e) => {
  const t = e.target;
  if (t.dataset.path) {
    S.opts[t.dataset.path] = t.value;
    if (S.errors[t.dataset.path]) { delete S.errors[t.dataset.path]; t.closest(".input").classList.remove("bad"); }
  }
  if (t.dataset.pf) {
    const idx = +t.closest(".prov").dataset.prov;
    const p = S.opts.providers[idx];
    if (t.dataset.pf === "name") {
      const old = p.name;
      p.name = t.value.trim().toLowerCase();
      if (S.opts.active === old) S.opts.active = p.name;
      if (S.tests[old]) { S.tests[p.name] = S.tests[old]; delete S.tests[old]; }
    } else p[t.dataset.pf] = t.value;
  }
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.isComposing && !e.target.closest("button")) {
    const primary = $("#foot .btn.primary:not([disabled]), #foot .btn.danger:not([disabled])");
    if (primary && cur() !== "providers") { e.preventDefault(); primary.click(); }
  } else if (e.key === "Escape" && S.run.state !== "running" && !e.target.closest("input")) {
    if (S.step > 0 && cur() !== "install" && cur() !== "bye") back();
  }
});

// ---------- helpers ----------
const errText = (e) => (typeof e === "string" ? e : e?.message || String(e));
const bytes = (n) => (n >= 1 << 20 ? (n / (1 << 20)).toFixed(0) + " MB" : Math.max(1, Math.round(n / 1024)) + " KB");
function samePath(a, b) {
  const n = (s) => String(s || "").replace(/[\\/]+$/, "");
  return S.info.os === "windows" ? n(a).toLowerCase() === n(b).toLowerCase() : n(a) === n(b);
}
function joinPath(a, b) {
  const sep = S.info.os === "windows" ? "\\" : "/";
  return a.replace(/[\\/]+$/, "") + sep + b;
}
function shortPath(p) {
  const h = S.info.home_dir;
  if (h && p.toLowerCase().startsWith(h.toLowerCase())) return "~" + p.slice(h.length);
  return p;
}
let toastTimer;
function toast(msg, err) {
  const t = $("#toast");
  t.textContent = msg;
  t.className = "toast show" + (err ? " err" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (t.className = "toast"), 4200);
}

// Pixel confetti in the crab's palette.
function confetti() {
  if (reduced) return;
  const cv = $("#confetti"), ctx = cv.getContext("2d"), dpr = devicePixelRatio || 1;
  cv.width = innerWidth * dpr; cv.height = innerHeight * dpr;
  ctx.scale(dpr, dpr);
  const colors = ["#c6f432", "#e3ff8a", "#5fe3f5", "#ff6bd5", "#ffbf4d", "#ffffff"];
  const ox = innerWidth * 0.62, oy = innerHeight * 0.38;
  const parts = Array.from({ length: 150 }, () => {
    const a = Math.random() * Math.PI * 2, v = 4 + Math.random() * 9;
    return { x: ox, y: oy, vx: Math.cos(a) * v, vy: Math.sin(a) * v - 5, s: 3 + Math.floor(Math.random() * 3) * 2, c: colors[(Math.random() * colors.length) | 0], life: 1 };
  });
  let frame = 0;
  (function tick() {
    ctx.clearRect(0, 0, innerWidth, innerHeight);
    let alive = 0;
    for (const p of parts) {
      p.vy += 0.32; p.vx *= 0.985; p.vy *= 0.985;
      p.x += p.vx; p.y += p.vy; p.life -= 0.008;
      if (p.life <= 0 || p.y > innerHeight + 20) continue;
      alive++;
      ctx.globalAlpha = Math.min(1, p.life * 1.5);
      ctx.fillStyle = p.c;
      ctx.fillRect(Math.round(p.x), Math.round(p.y), p.s, p.s);
    }
    if (alive && frame++ < 400) requestAnimationFrame(tick);
    else ctx.clearRect(0, 0, innerWidth, innerHeight);
  })();
}

// ---------- browser mock (no Wails): lets the UI be opened as a page ----------
const MOCK = (() => {
  const L = {};
  const emit = (n, d) => (L[n] || []).forEach((f) => f(d));
  const run = (steps, then) => {
    let i = 0, k = 0;
    const t = setInterval(() => {
      emit("setup:progress", { step: i, label: steps[i], detail: `${steps[i]}…`, pct: ((i + k / 8) / steps.length) * 100 });
      if (++k > 8) { k = 0; i++; }
      if (i >= steps.length) { clearInterval(t); then(); }
    }, 70);
  };
  const steps = { install: ["Checking the folder", "Copying ASG", "Creating shortcuts", "Setting up storage", "Saving providers", "Finishing up"], uninstall: ["Removing shortcuts", "Removing the app", "Cleaning up"] };
  const params = new URLSearchParams(location.search);
  return {
    listen: (n, f) => (L[n] ||= []).push(f),
    Info: async () => ({
      version: "v0.3.0", os: params.get("os") || "windows", arch: "amd64", mode: params.get("mode") || "install", preview: true, payload_bytes: 23_800_000,
      defaults: { install_dir: "C:\\Users\\you\\AppData\\Local\\Programs\\ASG", data_dir: "C:\\Users\\you\\.asg\\desktop", memory_dir: "C:\\Users\\you\\.asg\\memory" },
      current: { install_dir: "C:\\Users\\you\\AppData\\Local\\Programs\\ASG", data_dir: "C:\\Users\\you\\.asg\\desktop", memory_dir: "C:\\Users\\you\\.asg\\memory" },
      features: { desktop_shortcut: true, menu_shortcut: true, menu_label: "Start menu", launch_at_login: true, add_to_path: false },
      existing: params.get("existing") ? { version: "v0.2.0", install_dir: "C:\\Users\\you\\AppData\\Local\\Programs\\ASG" } : null,
      providers: [
        { name: "ollama", type: "ollama", base_url: "http://localhost:11434", model: "llama3.1:8b", enabled: true, has_key: false },
        { name: "anthropic", type: "anthropic", base_url: "https://api.anthropic.com", model: "claude-sonnet-5-5", enabled: false, has_key: false },
        { name: "openai", type: "openai", base_url: "https://api.openai.com/v1", model: "gpt-5", enabled: false, has_key: false },
      ],
      active: "ollama", theme: "system", config_path: "C:\\Users\\you\\.asg\\config.json", home_dir: "C:\\Users\\you",
    }),
    Steps: async () => steps,
    CheckPaths: async (p) => (p.install_dir.startsWith("C:\\Windows") ? { install_dir: "ASG can't write here (permission denied). Pick a folder in your user profile, or run setup as administrator." } : {}),
    PickFolder: async (_t, start) => start,
    TestProvider: async (p) => new Promise((r) => setTimeout(() => r(p.type === "ollama" ? { ok: true, models: ["llama3.1:8b", "qwen3:14b", "gpt-oss:20b"] } : p.api_key ? { ok: true, models: ["model-a", "model-b"] } : { ok: false, error: "Add an API key first." }), 700)),
    Install: async () => run(steps.install, () => emit("setup:done", { ok: true })),
    Uninstall: async () => run(steps.uninstall, () => emit("setup:done", { ok: true })),
    Launch: async () => toast("Would launch ASG"),
    OpenURL: (u) => window.open(u, "_blank"),
    Quit: () => toast("Would close setup"),
  };
})();

boot().catch((e) => {
  document.body.insertAdjacentHTML("beforeend", `<pre style="position:fixed;bottom:10px;left:10px;color:#ff6b5e;font:12px monospace">${esc(errText(e))}</pre>`);
});
