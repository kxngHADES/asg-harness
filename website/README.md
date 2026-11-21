# ASG website

The download page for ASG. Plain HTML, CSS and JavaScript: no build step, no framework, no trackers.

```
website/
├── index.html      page structure and copy
├── styles.css      design tokens (top of file), neumorphic surfaces, layout, motion
├── js/main.js      OS detection, downloads, release info, "/" menu, scroll motion
├── js/shell.js     the 3D hermit-crab shell in the header (canvas, no WebGL library)
└── assets/crab.svg the ASG crab (copied from docs/asg-crab.svg)
```

## How downloads stay current

Every download link points at GitHub's stable redirect:

```
https://github.com/kxngHADES/asg-harness/releases/latest/download/<file>
```

GitHub sends that to the newest release's file, so a new release needs no change here. The
release workflow publishes the setup wizard under fixed names (`asg-setup_windows_<arch>.exe`,
`asg-setup_darwin_universal.dmg`, `asg-setup_linux_<arch>`). **If those names change, update
`setupFile()` in `js/main.js` and the links in `index.html`.**

`main.js` reads the visitor's OS (and CPU, via `navigator.userAgentData` where the browser
supports it) to pick the main button's file and preselect the install tab. The GitHub API call is
only used for the version number, release date and file sizes. It's cached for 10 minutes per tab,
and if it fails (offline, or the 60-requests-an-hour limit) the links still work.

## Run locally

```sh
cd website
python -m http.server 8765    # then open http://localhost:8765
```

## Deploy

Any static host works. For GitHub Pages: Settings → Pages → Source: GitHub Actions, then use the
"Static HTML" starter workflow with `path: website`.

## Design notes

- Palette: lime `#c6f432` (ASG), cyan `#4de1ff` (tools), magenta `#ff5fd8` (skills), coral
  `#ff7d52`, on night `#0d100e` and clay `#e1e6de`. No gold or amber.
- Type: Anybody (display, its width axis is animated), Hanken Grotesk (body), Martian Mono (code).
- Press `/` (or Ctrl/⌘ + K) anywhere for the command menu, same key as in the app.
- Reduced motion stops the shell, the kinetic type and the parallax. Data saver stops the shell.
  The shell also pauses when it's off screen or the tab is hidden.
