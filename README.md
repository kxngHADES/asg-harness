<div align="center">

<img src="docs/asg-crab.svg" width="160" alt="ASG, a pixel hermit crab carrying a lime spiral shell">&nbsp;&nbsp;&nbsp;<img src="docs/asg-crab-working.svg" width="160" alt="ASG at work: tapping its claws with sparks of thought">

### a signal console for local & hosted models

**ASG** is an agentic shell for your terminal and browser. It runs [Ollama](https://ollama.com) models first,<br>
and switches to Anthropic or any OpenAI-compatible API in one click.

[![ci](https://github.com/kxngHADES/asg-harness/actions/workflows/ci.yml/badge.svg)](https://github.com/kxngHADES/asg-harness/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/kxngHADES/asg-harness?color=c6f432&labelColor=121310)](https://github.com/kxngHADES/asg-harness/releases/latest)
[![go](https://img.shields.io/github/go-mod/go-version/kxngHADES/asg-harness?color=4de1ff&labelColor=121310)](go.mod)
![platforms](https://img.shields.io/badge/macOS%20·%20Linux%20·%20Windows-amd64%20%2B%20arm64-ff5fd8?labelColor=121310)

[Install](#-install) · [Modes](#-modes) · [Voice](#-voice) · [Skills](#-skills) · [Providers](#-providers) · [Commands](#-commands) · [Releases](#-development--releases)

</div>

---

```text
  [pixel crab, 20×7]     ASG  agent shell
                         ollama/gemma4:31b-cloud
                         ~/code/asg-harness
                         type / for commands
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

gemma4:31b-cloud ▸ why is the build failing? [Image #1]
  ◆ BASH go build ./...
  ┊ internal/tui/repl.go:151:33: cannot call pointer method Active
  ▛▀ GATE ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  ▌ EDIT_FILE requests permission
  ▌ - if _, pc := r.app.Config.Get().Active(); pc != nil {
  ▌ + cfg := r.app.Config.Get()
  ▙▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  [y] run  [a] always edit_file here  [g] everywhere  [n] block ▸ y
┃  FIXED 
┃ ▪ Active() has a pointer receiver, so I bound the config first.
  ── 6.2s · 4210↑ 188↓ tokens ──
```

## ◆ What it does

| | |
|---|---|
| **Tool calling** | bash, read / write / edit files, glob, grep, web fetch, skills. Anything that changes state goes through a **GATE** you approve. |
| **Modes** | **Shift+Tab** cycles *build* → *plan* → *loop* → *ask*. **Ctrl+Y** toggles YOLO (no approval prompts). |
| **Loop harness** | `/loop <objective>` plans, works one small step at a time, has each step reviewed, runs your tests, and repeats until done. Built to make 7–14B local models (e.g. Qwen3.5 9B) finish real tasks. |
| **Images** | **Ctrl+V** pastes a clipboard image, drag a file into the terminal, or type `@shot.png`. You can also paste or drop images in the browser. |
| **Voice** | Replies are read aloud by the current model if it can speak, or by a speech model you choose (e.g. `phonon` as *Juniper*), the OS voice, or OpenAI TTS. |
| **Skills** | Import `SKILL.md` skills from GitHub, URLs, or folders. The model loads a skill when it's relevant. |
| **Web version** | A local dashboard to chat, approve tools, switch providers and models, edit API keys, and manage voices and skills. |
| **Any backend** | Ollama (native API) · Anthropic · OpenAI · OpenRouter · Groq · LM Studio · vLLM · anything OpenAI-compatible. |

**New here?** The [terminal guide](docs/README.md) covers every key and command, with examples: switching modes, switching domains and flavors (e.g. `software/project` → `software/work`), loops, tools and config.

---

## ▸ Install

> You need [Ollama](https://ollama.com/download) for local models (`ollama pull llama3.1`). For hosted models you only need an API key.

<details open>
<summary><b>macOS / Linux</b> (bash · zsh · fish · sh)</summary>

```sh
curl -fsSL https://raw.githubusercontent.com/kxngHADES/asg-harness/main/install.sh | sh
```

The script picks your OS/CPU, verifies the SHA-256 checksum, and installs to `/usr/local/bin` (or `~/.local/bin`). If that folder isn't on your `PATH`, it prints the exact line to add for bash, zsh and fish.

Options: `ASG_VERSION=v0.2.0` pins a release · `ASG_INSTALL_DIR=~/bin` picks the location.

</details>

<details open>
<summary><b>Windows</b> (PowerShell · Windows Terminal · cmd)</summary>

```powershell
irm https://raw.githubusercontent.com/kxngHADES/asg-harness/main/install.ps1 | iex
```

Installs `asg.exe` to `%LOCALAPPDATA%\Programs\asg` and adds it to your user `PATH`. Open a new terminal (PowerShell, cmd, or Windows Terminal) and run `asg`.

</details>

<details>
<summary><b>With Go</b> (any OS)</summary>

```sh
go install github.com/kxngHADES/asg-harness/cmd/asg@latest
```

</details>

<details>
<summary><b>From source</b></summary>

```sh
git clone https://github.com/kxngHADES/asg-harness && cd asg-harness
make install          # builds and installs to ~/.local/bin/asg
# or: go build -o asg ./cmd/asg
```

</details>

**Update anytime** with `asg update`. It downloads the latest release, checks its checksum, and swaps itself in place. Check your version with `asg version`.

---

## ▸ Modes

### 1 · Terminal coding agent

```sh
cd your-project
asg
```

A full interactive agent that reads and edits your code, runs commands behind approval gates, takes pasted images, and can speak its replies. If the folder has an `ASG.md`, `AGENTS.md` or `CLAUDE.md`, ASG adds it as project instructions. Run `/init` and ASG explores the project and writes (or improves) an `ASG.md` for future sessions.

**Memory.** ASG keeps Markdown notes between sessions. Tell it something lasting ("always write tests with pytest") and it saves a note with its `memory_save` tool, or use `/memory <note>` to ask directly. Notes live in `~/.asg/memory/` (every project) and `~/.asg/projects/<project>/memory/` (this folder only), one `.md` file each, so you can edit them by hand. `/memory` lets you browse and delete them.

**Remembered permissions.** At a GATE, `[a]` always allows that kind of call in this project and `[g]` allows it everywhere, and both carry over to future sessions. For shell commands the rule covers the program, so allowing `git status` also allows `git log`, but `git status && rm -rf build` still asks about `rm`. Commands containing `$(…)` or backticks always ask. Review or revoke rules with `/permissions` (stored in `~/.asg/permissions.json`).

```sh
asg -provider anthropic -model claude-opus-5-5   # switch backend (saved as default)
asg -yolo                                        # skip approval gates for this run only
```

### 2 · Web version

```sh
asg dashboard              # opens http://127.0.0.1:7777 in your browser
asg dashboard -no-open     # just print the URL
```

| View | |
|---|---|
| **Chat** | Streaming transcript, inline tool approvals, paste or drop images, run with a skill, read replies aloud |
| **Providers** | Add Ollama / Anthropic / OpenAI / OpenRouter / Groq / LM Studio from presets, paste API keys, **Test** the connection, pick models |
| **Settings** | System prompt, auto-approve, tool-round limit, temperature, and who speaks (voice) |
| **Skills** | Import from GitHub/URL, upload a `.md`, write one, view or remove |
| **Tools** | What the model can call and which tools need approval |

### 3 · Both at once

Type `/dashboard` inside the terminal agent. The browser and terminal share one config, so switching provider, model, or voice in either one takes effect in both immediately. Each keeps its own conversation.

### 4 · One-shot & scripting

```sh
asg -p "summarise this repo in 5 bullets"
asg -p "what's wrong in this screenshot?" error.png
git diff | asg -p -                                # "-" reads the prompt from stdin
asg -yolo -p "run the tests and fix what fails"    # one-shot mode denies gated tools unless -yolo
asg -loop -yolo -p "add a --json flag to list, with tests"   # unattended loop run; exits 2 unless it finishes
```

---

## ▸ Working modes

Press **Shift+Tab** to cycle modes, or use `/mode`. The prompt always shows the mode as a fixed-width badge followed by the domain, e.g. `BUILD software/work qwen3.5:9b ▸`, and a status line above it shows every mode with the current one highlighted. Repeated Shift+Tab / Ctrl+T presses redraw that one line in place instead of printing a new line each time.

| Mode | Tools | Use it to |
|---|---|---|
| **build** | all, behind gates | make changes — the default |
| **plan** | read-only | investigate and get a numbered plan; nothing is changed. Afterwards `/loop` runs the plan step by step, or switch to build and the plan stays in view |
| **loop** | all, behind gates | each message is an objective for the loop harness (below) |
| **ask** | read-only | questions about the code, answered from the actual files |

`/plan <request>`, `/build <request>`, `/ask <question>` switch and send in one go. `asg -mode plan` starts in a mode.

**YOLO.** **Ctrl+Y** (or `/yolo`) toggles running tools without asking; the prompt shows `⚡YOLO` while it is on. At a GATE, Ctrl+Y turns YOLO on *and* approves the waiting call. YOLO lasts until you toggle it off or quit, so it never sticks around by accident.

## ▸ Domains

Modes say *how* ASG works; domains say *what kind of work* it does. Each domain has sub-modes (flavors), and each flavor brings **the loop the model must follow**, hard rules, and the angles it reviews its own work from. Press **Ctrl+T** to cycle domains, `/domain` for a menu, or use a shortcut that switches and sends in one go: `/cyber blue audit the nginx config`. The choice is saved as your default; `asg -domain software/work` sets it from the command line.

| Domain | Flavors | The loop |
|---|---|---|
| **software** | `project` *(default)* · `work` · `hobby` | **work** is strict: only code this exact build uses — no anticipation code; write the *caller* before the function; then test → document → verify (fix the root cause and loop until everything passes) → security test |
| **docs** | `update` · `write` | screenshot the rendered docs → decide changes from the image → edit → screenshot again and compare, until it renders right |
| **cyber** | `red` · `blue` | red: scope → recon → enumerate → analyse → validate → report. blue: inventory → threat model → detect → harden → respond → report. Confirms scope and authorization first; ends with a report of **everything it did and found** |
| **devops** | `review` · `pipeline` | inventory every pipeline/infra file → correctness → security (permissions, pinning, secrets, injection) → speed & reliability → findings with exact fixes |
| **pm** | `plan` · `sprint` · `status` | scope → breakdown → sequence → estimate → risks → communicate, reviewed from four planning angles (dependencies, estimates, risks, acceptance) |

In a loop run the plan follows the flavor's phases, the step reviewer enforces its rules (e.g. FAIL on anticipation code), and once everything passes verification the flavor's **final steps** run — the security test for software/work, the full report for cyber and devops review, a last screenshot pass for docs.

## ▸ Self-review

After every answer — in every mode and domain — ASG goes back over its own work **at least 4 times**, each pass from a different angle (the domain's lenses first, then logic, completeness, edge cases, evidence). A pass either confirms (`REVIEW OK`) or fixes the problem and gives the corrected answer; if the last required pass still found something, more passes run (up to `max_passes`). You see one line per pass. Loop runs review their plan 4 times before starting and the finished result 4 times at the end. `/review 6` changes the minimum, `/review off` turns it off, `asg -review N` sets it per run.

## ▸ Asking you

When information is missing that it can't find in the project — requirements, credentials, scope, a decision only you can make — the model calls `ask_user` at any moment: the turn pauses, shows the question (with numbered suggestions when it has them), and continues with your answer. It works in every mode, including loop runs and `asg -p` in a terminal.

## ▸ Work folder

In build mode and loop runs ASG keeps resources you can read while it works, in `.asg/work/<date>-<task>/` in your project:

| File | Written by | What's in it |
|---|---|---|
| `plan.md` | model (loop: harness) | goal, approach, steps — written first |
| `checklist.md` | model (loop: harness) | `- [ ]` / `- [x]` per step, ticked as it goes |
| `walkthrough.md` | model (loop: harness) | what changed, where, why, how to check it |
| `notes.md` | model | decisions, assumptions, open questions |
| `report.md` | model | cyber and devops: everything done and found |
| `activity.md` | harness | every tool call with time, summary and result — complete even if the model forgets |

Writes inside the work folder never ask for permission. `/work` shows the folder and the checklist. The folder is only created once something is written, so a chat turn leaves nothing behind. Add `.asg/work/` to `.gitignore` if you don't want to commit it.

## ▸ Tools

Besides the built-in file, shell and web tools, ASG ships **bundled tools** from the repo's [`tools/`](tools) folder, compiled in for every user:

- **`office_document`** — writes Word `.docx`, PowerPoint `.pptx` and Excel `.xlsx` from Markdown (or exact slides / sheets / CSV). Pure Go: no Office, Python or other install needed.
- **`screenshot`** — renders a URL, `.html` or `.md` file in headless Chrome/Edge/Chromium (or captures the screen) and shows the image to the model. Docs mode is built on it. Needs a vision-capable model; `ASG_BROWSER` points at a browser if it isn't found.

Anyone can add **script tools** without recompiling: a folder with a `tool.json` and a script in any language, in `~/.asg/tools/` (you) or `.asg/tools/` (the project). `asg tool new <name>` scaffolds one, `/tools` lists everything and reloads script tools. See [tools/README.md](tools/README.md).

## ▸ Loop harness

Small models fall over on long tasks: context fills up, they lose the thread, claim success without checking, or repeat the same call. The loop harness gives the model *operational discipline* instead — the harness, not the model, decides what happens next:

```
objective ─► PLAN (read-only look around → numbered steps)
             │
             ▼
      ┌─► WORK on ONE step   fresh short context: objective, ✓/▶ checklist, notes from earlier steps
      │      │
      │   REVIEW             separate no-tools call judges the step from tool results, not claims
      │      ├─ pass → next step
      │      ├─ fail → retry with the reviewer's feedback (3×) → split into smaller steps → ask you
      │      └─ BLOCKED: question → stops and asks you
      └──────┘
             ▼  all steps done
         VERIFY              runs your test/build command; failures become a "fix" step (up to 3×)
             ▼
         FINAL STEPS         the domain's finale: security test (software/work), report (cyber, devops), …
         REVIEW ×4           the whole result from four angles; if it changed anything, verify again
```

The plan itself is reviewed 4 times before the first step runs, and `plan.md`, `checklist.md` and `walkthrough.md` in the work folder are kept in sync by the harness.

```text
/loop add a --json flag to the list command, with tests    start
/loop                    run the plan you just made in plan mode
/loop status             checklist and state of the saved run
/loop resume [answer]    continue after Ctrl+C, a question, or a restart
/loop verify <cmd>       set the check command (auto · none)
```

The check command is auto-detected (`go test`, `cargo test`, `npm test`, `pytest`, `make test`) or set with `/loop verify`. Runs are saved after every step in `~/.asg/loops/`, never in your repo. Every tool call still goes through the normal gates unless YOLO is on.

**Small-model helpers** that apply in every mode: tool calls written out as text (`<tool_call>{…}</tool_call>`, a JSON block) are recovered and run; a fourth identical tool call in one turn is refused with a nudge to change course; and Ollama gets a 16K context window (`num_ctx`) instead of its small default, which otherwise silently truncates the system prompt and tool output.

---

## ▸ Voice

Turn it on with `/voice on` (terminal) or the **Voice** toggle (browser). Then choose **who speaks**:

| Mode | What happens |
|---|---|
| `auto` *(default)* | If the current chat model has Ollama's `speech` capability, it reads its own replies. Otherwise the **fallback** speaks. |
| `ollama <model> [voice]` | Always use a specific Ollama speech model, e.g. **phonon** as **Juniper**. |
| `openai [voice]` | OpenAI TTS (uses your `openai` provider key). |
| `system [voice]` | The OS voice: macOS `say`, Linux `espeak-ng`, Windows SAPI. |

Example: gemma4 has no voice of its own, so let phonon speak for it:

```text
/voice auto
/voice fallback ollama phonon Juniper     # models that can't speak use phonon (Juniper)
/model gemma4:31b-cloud
/voice                                    # → now: gemma4:31b-cloud → ollama phonon (Juniper)
```

Or **pin** a voice to one model, which beats both the default and the fallback:

```text
/model nemotron-3.5-bolt:e4b
/voice pin ollama phonon Captain Silas Blackwater
```

`/voice models` lists your installed Ollama speech models, and `/voice voices phonon` lists a model's voices. Long replies are split into sentences, so speech starts almost at once and the next chunk is generated while the current one plays.

> Note: a model's `audio` capability (e.g. nemotron) means it can *hear* audio input. Only models with `speech` can talk.

---

## ▸ Skills

```sh
asg skill add github:anthropics/skills/skills/pdf              # one skill from a repo folder
asg skill add github:owner/repo                                # every SKILL.md in a repo
asg skill add https://github.com/owner/repo/tree/main/skills/x # GitHub URL (branch/tag ok)
asg skill add https://example.com/SKILL.md                     # a single file
asg skill add ~/my-skills                                      # local folder
asg skill list · asg skill rm pdf
```

Skills go to `~/.asg/skills/`. ASG also loads project skills from `./.asg/skills/`. The model sees each skill's name and description and loads the full instructions when it needs them, or you can run one directly with `/<skill-name> your request`. Set `GITHUB_TOKEN` for higher GitHub rate limits.

---

## ▸ Providers

Manage providers in the dashboard, or edit `~/.asg/config.json` (`$ASG_HOME` overrides the location):

```json
{
  "active_provider": "ollama",
  "providers": {
    "ollama":     { "type": "ollama",    "base_url": "http://localhost:11434",        "model": "qwen3.5:9b", "num_ctx": 16384 },
    "anthropic":  { "type": "anthropic", "base_url": "https://api.anthropic.com",     "api_key": "sk-ant-…", "model": "claude-sonnet-5-5" },
    "openai":     { "type": "openai",    "base_url": "https://api.openai.com/v1",     "api_key": "sk-…",     "model": "gpt-5" },
    "openrouter": { "type": "openai",    "base_url": "https://openrouter.ai/api/v1",  "api_key": "sk-or-…",  "model": "…" }
  },
  "auto_approve": false,
  "loop": { "max_iterations": 30, "step_attempts": 3, "step_rounds": 15, "verify": "" },
  "review": { "passes": 4, "max_passes": 6 },
  "domain": "software", "flavor": "work",
  "audio": {
    "enabled": true,
    "engine": "auto",
    "fallback": { "engine": "ollama", "model": "phonon", "voice": "Juniper" },
    "per_model": { "nemotron-3.5-bolt:e4b": { "engine": "system", "voice": "Samantha" } }
  }
}
```

There is no limit on how many tools the model uses in one turn; it keeps going until it's done or you press Ctrl+C. Set `"max_tool_rounds": N` only if you want a cap. If a model rejects tool definitions, ASG switches to chat-only mode for it automatically. `num_ctx` is the Ollama context window: unset means 16384, a negative value leaves Ollama's default. Lower it if a model no longer fits in VRAM; raise it for long files.

---

## ▸ Commands

Press **/** on an empty prompt to open the command menu: type to filter, **↑↓** to move, **Enter** to run, **Esc** to close. Commands that need an argument are inserted for you to finish, and typing a space keeps writing by hand.

| Command | |
|---|---|
| `/mode` · **Shift+Tab** | pick or cycle the working mode (build, plan, loop, ask) |
| `/domain` · **Ctrl+T** | pick or cycle the domain (software, docs, cyber, devops, pm) and its flavor |
| `/software` · `/docs` · `/cyber` · `/devops` · `/pm [flavor] [text]` | switch domain (and flavor), and send the text in it |
| `/plan` · `/build` · `/ask [text]` | switch mode, and send the text in it |
| `/loop …` | run an objective with the loop harness (see *Loop harness*) |
| `/review [n\|off]` | self-review passes after every answer (default 4) |
| `/work` | show the work folder: plan, checklist, walkthrough, activity |
| `/provider [name]` | pick a provider from a menu (asks for a missing API key), then its model |
| `/model [name]` · `/models` | pick a model from a menu, set one by name, or list them |
| `/init` | write or improve `ASG.md` for this project |
| `/memory [note]` | browse and delete memories, or ask ASG to remember something |
| `/permissions` | review and revoke remembered "always allow" answers |
| `/paste` · **Ctrl+V** | attach a clipboard image |
| `/image <path>` | attach an image file (or drag it in, or `@path`) |
| `/voice …` · `/speak` | voice output: `/voice help` for everything |
| `/skills` · `/skill add\|rm\|show` | manage skills |
| `/<skill> <request>` | run a request with a skill |
| `/tools` · `/tool new <name>` | list (and reload) tools · scaffold a script tool |
| `/yolo` · **Ctrl+Y** | toggle YOLO (run tools without asking) |
| `/dashboard` | open the web version |
| `/clear` · `/usage` · `/config` · `/exit` | |

End a line with `\` for multi-line input. Pasted multi-line text arrives as one message. **Ctrl+C** stops a running turn, and pressing it twice at the prompt quits. **Tab** completes commands. Set `NO_COLOR=1` for plain output, or `ASG_NO_ANIMATION=1` to keep the colours but skip the animations.

**The crab.** ASG's mascot is pixel art drawn with half-block characters (two pixels per cell, true colour or 256 colours). It scuttles in when you start `asg`, and while ASG is thinking or running a tool it works away under your prompt, tapping its claws with sparks of thought, then gets out of the way when the reply streams in. Narrow terminals and `ASG_NO_ANIMATION` get a one-line crab instead. The dashboard shows the same sprite as an animated SVG that switches to its working animation while a reply is in progress. The art lives in `internal/sprite`; after changing it, run `UPDATE_SPRITES=1 go test ./internal/sprite/` to refresh the SVGs in `docs/`.

---

## ▸ Security

- **Gates.** `bash`, `write_file`, `edit_file`, `web_fetch`, `office_document`, `screenshot` and script tools (unless their `tool.json` says otherwise) ask before running; writes inside the build's own work folder don't. `[a]`/`[g]` remember your answer per program or tool (see *Remembered permissions*) until you revoke it with `/permissions`. `-yolo` and Ctrl+Y last until you turn them off or quit; turning YOLO off also clears a saved `auto_approve`. Plan and ask modes don't offer the model any tool that changes your project.
- **Dashboard.** It listens on **127.0.0.1 only**, and each run gets a random token passed in the URL fragment. Every API call must carry that token, and requests with a non-localhost `Host` are rejected (DNS-rebinding protection). API keys are masked before they reach the browser.
- **Updates.** Releases and `asg update` are verified against `checksums.txt`.

---

## ▸ Development & releases

```sh
make test        # go test ./...
make lint        # gofmt + go vet
make run         # build and start the terminal agent
make dashboard   # build and start the web version
```

```
cmd/asg               CLI entry point
internal/agent        model ↔ tool loop, sessions, voice resolution
internal/llm          provider-neutral types + Ollama / OpenAI / Anthropic adapters
internal/tools        built-in tools
internal/perms        remembered "always allow" rules
internal/memory       Markdown notes kept between sessions
internal/sprite       the pixel-art crab: frames, animations, SVG export
internal/skills       SKILL.md loading and importing
internal/media        images: files, drag-and-drop paths, clipboard
internal/audio        speech: OS voice, Ollama / OpenAI speech models
internal/tui          terminal UI
internal/dashboard    HTTP server + embedded web UI
internal/update       self-update
```

**CI/CD.** Every push and PR runs tests on macOS, Linux and Windows, plus gofmt, race detection, shellcheck, and a full GoReleaser dry run.

**Autodeploy.** Commits use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, …):

1. Every push to `main` updates a **release PR** with the next version and the [CHANGELOG](CHANGELOG.md) ([release-please](https://github.com/googleapis/release-please)).
2. Merging that PR tags the release, and GoReleaser builds macOS / Linux / Windows binaries (amd64 + arm64) and attaches them.
3. The install scripts and `asg update` pick up the new version right away.
