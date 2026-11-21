# ASG desktop

A desktop app around the same agent as the `asg` terminal app: chats you can come back
to, projects, coding sessions you can resume, a library of everything the agent made,
and prompts that run on a schedule. It reads the same `~/.asg/config.json`, skills,
tools, memory and permissions as the terminal app.

It is a separate Go module in [`desktop/`](../desktop) built with
[Wails v2](https://wails.io) (Go backend + the system WebView2/WebKit). The UI is plain
HTML, CSS and JS in `desktop/frontend/dist` — there is no Node build step.

## Download

Every release on the [releases page](https://github.com/kxngHADES/asg-harness/releases/latest)
carries the desktop app next to the terminal binaries:

| File | Platform | How to install |
|---|---|---|
| `asg-desktop_windows_amd64.zip` / `_arm64.zip` | Windows 10/11 | Unzip and run `asg-desktop.exe`. The build isn't code-signed, so SmartScreen may say "Windows protected your PC": **More info → Run anyway**. Uses the WebView2 runtime that Windows ships with. |
| `asg-desktop_darwin_universal.zip` | macOS 11+ (Apple Silicon and Intel) | Unzip and drag **ASG.app** to Applications. It has no Apple Developer ID, so the first time **right-click → Open** (or run `xattr -dr com.apple.quarantine /Applications/ASG.app`). |
| `asg-desktop_linux_amd64.tar.gz` / `_arm64.tar.gz` | Linux with GTK 3 | Needs `libwebkit2gtk-4.1-0` and `libgtk-3-0` (`sudo apt install libwebkit2gtk-4.1-0` on Debian/Ubuntu 22.04+). Unpack, put `asg-desktop` on your `PATH`, and copy `asg-desktop.desktop` / `asg-desktop.png` to `~/.local/share/applications` / `~/.local/share/icons` for a menu entry. |

Each file has a `.sha256` next to it. The app reads the same `~/.asg/config.json` as `asg`,
so set up a provider in either one.

On macOS the window uses the native traffic-light buttons; on Windows and Linux the app
draws its own minimise / maximise / close buttons.

## Build and run

```sh
cd desktop
# Windows (needs the WebView2 runtime, which Windows 10/11 ship with)
go build -tags desktop,production -ldflags "-H windowsgui -s -w" -o asg-desktop.exe .
# macOS / Linux (Linux needs libgtk-3 and libwebkit2gtk-4.0/4.1 dev packages)
go build -tags desktop,production -ldflags "-s -w" -o asg-desktop .
go test -tags desktop,production .
```

`make desktop` does the same from the repo root. `make desktop-package` builds the same
archive CI attaches to a release (`desktop/build/package.sh`): on Windows it adds the icon
and manifest with [go-winres](https://github.com/tc-hib/go-winres); on macOS it builds an
arm64 + amd64 universal `ASG.app`; on Linux a tarball with a `.desktop` file. The icon is
`desktop/build/appicon.png` (the crab, drawn from `internal/sprite`).

For UI work, `go run -tags devserve .` serves the same UI on `http://127.0.0.1:7788` with
the real backend behind it, so you can use the browser's dev tools. Folder pickers become
a browser prompt there. Set `ASG_HOME` to a scratch folder to keep test data apart.

## What's in it

| Area | What it does |
|---|---|
| **New chat** | One composer with three kinds: **Chat**, **Code** (pick a folder) and **Artifact**. Optional project. Mode (Build / Plan / Ask), model, image attachments (paste or drop), and `/skill` to run a skill. |
| **Chats** | Every session, grouped by day, filterable by kind, searchable. Pin, rename, move to a project or delete from the `…` menu. |
| **Projects** | Instructions added to every chat in the project, an optional folder to work in (otherwise a shared private folder), the project's chats and artifacts. |
| **Code** | Coding sessions grouped by folder with the current git branch. **Resume** reopens the full history; the agent keeps its plan and working files (`plan.md`, `checklist.md`, …) which show in the session's side panel. |
| **Artifacts** | Every file a session wrote (`write_file`, `office_document`) is snapshotted, so it survives the original being changed or deleted. Writing the same file again adds a version. Pages and SVGs preview live (HTML runs in a sandboxed frame), Markdown renders, code is highlighted; Office files open in their own app. Coding sessions only keep documents and images, not every source file. |
| **Schedules** | A prompt that runs every N minutes, hourly, daily, on weekdays or weekly. Each run is its own session (clock icon). Runs only while the app is open; a run missed while it was closed runs once on the next start. Nobody can answer a permission prompt during a scheduled run, so gated tools are skipped unless you turn on **Allow tools**. |
| **Customise** | Providers and models, global instructions, skills (create / import / remove; the built-in `/design-first-dev` and `/frontend-design` are tagged **Built-in**), tools, memory notes, and behaviour (auto-approve, tool rounds, self-review passes, temperature). |
| **Settings** | Theme, and **Storage & sandbox** (below). |

Session icons: speech bubble = chat, terminal = code, sparkles = artifact, folder = project
chat, clock = scheduled run.

Keys: `Ctrl+N` new chat · `Ctrl+K` search and jump · `Ctrl+B` sidebar · `Enter` send ·
`Shift+Enter` new line · `Esc` close.

## Storage and the sandbox

Everything the app keeps lives in one **data folder** (default `~/.asg/desktop`):

```
sessions/<id>.json        full history (resumable) + <id>.meta.json for listings
artifacts/<id>/           snapshot of the file + artifact.json
sandbox/<session id>/     each plain chat's private working folder
projects/<id>/files/      shared folder for a project without its own folder
projects.json  schedules.json  desktop.log
```

**Settings → Storage & sandbox** can:

- **Move…** everything to a new, empty folder — e.g. on an external drive — then delete the
  old copy.
- **Use a different folder** that already holds ASG data (say, the same drive on another PC),
  without moving anything.
- **Reset** to the default folder.

The chosen folder is remembered in `~/.asg/desktop.json`. If it isn't there at start-up
(drive unplugged), the app falls back to the default folder for that run and tells you.

A chat that isn't tied to a folder works in its own sandbox folder. Writing files inside
it never asks for permission; shell commands, web access and anything outside the sandbox
still go through the normal permission prompt. Coding sessions and projects with a folder
work in that folder with the usual prompts.

## Limits

- One turn runs at a time. Tools resolve paths against the process working directory, so
  the app switches into each session's folder for its turn; a second session waits (it
  shows "Waiting for another session to finish…").
- Sessions started in the terminal app aren't listed — the terminal doesn't save them.
