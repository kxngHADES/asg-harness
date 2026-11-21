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
carries the desktop app next to the terminal binaries.

### Setup wizard (recommended)

Download the setup file for your platform and run it. It isn't zipped:

| File | Platform | Notes |
|---|---|---|
| `asg-setup_windows_amd64.exe` / `_arm64.exe` | Windows 10/11 | Not code-signed: if SmartScreen says "Windows protected your PC", choose **More info → Run anyway**. |
| `asg-setup_darwin_universal.dmg` | macOS 11+ | Open the disk image, then **right-click → Open** on **ASG Setup** the first time (it has no Apple Developer ID). |
| `asg-setup_linux_amd64` / `_arm64` | Linux with GTK 3 + WebKitGTK 4.1 | `chmod +x asg-setup_linux_amd64 && ./asg-setup_linux_amd64` (browsers drop the executable bit). |

The wizard walks through:

1. **Location**: where the app goes. The defaults need no administrator rights:
   `%LOCALAPPDATA%\Programs\ASG`, `/Applications` (or `~/Applications`), `~/.local/share/asg-desktop`.
2. **Shortcuts**: a desktop shortcut, a Start menu / app menu entry, start at sign-in,
   `asg-desktop` on your `PATH` (Linux only), and the theme.
3. **Storage**: the **data folder** (sessions, artifacts, sandboxes) and the **memory folder**
   (the agent's notes). Both can be moved later in **Settings → Storage & sandbox**.
4. **Providers**: switch on Ollama, Anthropic, OpenAI or any OpenAI-compatible API (OpenRouter,
   Groq, LM Studio, …). **Test connection** lists the provider's models. Add more or change keys
   any time in **Customise → Providers**.
5. **Review** and install.

Running the wizard again with an existing install offers **Reinstall** (an in-place update that keeps
your data) or **Uninstall**. On Windows, ASG is also listed under **Settings → Apps → Installed
apps**, which runs `uninstall.exe` from the install folder. Uninstall removes the app, its
shortcuts and its start-up entry. It deletes sessions and memory only when you tick that box.
`config.json` is always kept, because the terminal app uses it too. What an install created is
recorded in `~/.asg/install.json`.

### Archives

Prefer to unpack it yourself? Use the archives instead:

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
# macOS 11+ (Xcode command line tools)
CGO_LDFLAGS="-framework UniformTypeIdentifiers" go build -tags desktop,production -ldflags "-s -w" -o asg-desktop .
# Linux (libgtk-3-dev + libwebkit2gtk-4.1-dev; drop ",webkit2_41" if you only have webkit2gtk-4.0)
go build -tags desktop,production,webkit2_41 -ldflags "-s -w" -o asg-desktop .
go test -tags desktop,production .   # same tags / CGO_LDFLAGS as the build on macOS and Linux
```

`make desktop` does the same from the repo root. `make desktop-package` builds the same
archive CI attaches to a release (`desktop/build/package.sh`): on Windows it adds the icon
and manifest with [go-winres](https://github.com/tc-hib/go-winres); on macOS it builds an
arm64 + amd64 universal `ASG.app`; on Linux a tarball with a `.desktop` file. The icon is
`desktop/build/appicon.png` (the crab, drawn from `internal/sprite`).

The setup wizard lives in `desktop/installer` (a second Wails app in the same module).
`package.sh` packs the built app into `installer/payload/app.tar.gz` and builds the wizard around
it, so every `asg-setup_*` file contains the matching app. `make setup-preview` builds it without a
payload: it walks through every screen and installs nothing. You can also open
`desktop/installer/frontend/index.html` from any local web server, where a built-in mock stands in
for the Go side (`?os=darwin`, `?existing=1` and `?mode=uninstall` show the other variants).

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

**Memory** (same page) shows where the agent's notes live: `~/.asg/memory` by default. **Choose…**
moves them to any folder (e.g. a synced one) by copying the notes over. The folder is saved as
`memory_dir` in `config.json`, so the `asg` terminal app uses it too.

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
