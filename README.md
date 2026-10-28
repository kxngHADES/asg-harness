<div align="center">

```
▄▀█ █▀ █▀▀
█▀█ ▄█ █▄█
```

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
  ▄▀█ █▀ █▀▀
  █▀█ ▄█ █▄█
   AGENT SHELL   ollama/gemma4:31b-cloud
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

gemma4:31b-cloud ▸ why is the build failing? [Image #1]
  ◆ BASH go build ./...
  ┊ internal/tui/repl.go:151:33: cannot call pointer method Active
  ▛▀ GATE ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  ▌ EDIT_FILE requests permission
  ▌ - if _, pc := r.app.Config.Get().Active(); pc != nil {
  ▌ + cfg := r.app.Config.Get()
  ▙▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  [y] run  [a] always edit_file  [n] block ▸ y
┃  FIXED 
┃ ▪ Active() has a pointer receiver, so I bound the config first.
  ── 6.2s · 4210↑ 188↓ tokens ──
```

## ◆ What it does

| | |
|---|---|
| **Tool calling** | bash, read / write / edit files, glob, grep, web fetch, skills. Anything that changes state goes through a **GATE** you approve. |
| **Images** | **Ctrl+V** pastes a clipboard image, drag a file into the terminal, or type `@shot.png`. You can also paste or drop images in the browser. |
| **Voice** | Replies are read aloud by the current model if it can speak, or by a speech model you choose (e.g. `phonon` as *Juniper*), the OS voice, or OpenAI TTS. |
| **Skills** | Import `SKILL.md` skills from GitHub, URLs, or folders. The model loads a skill when it's relevant. |
| **Web version** | A local dashboard to chat, approve tools, switch providers and models, edit API keys, and manage voices and skills. |
| **Any backend** | Ollama (native API) · Anthropic · OpenAI · OpenRouter · Groq · LM Studio · vLLM · anything OpenAI-compatible. |

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

A full interactive agent that reads and edits your code, runs commands behind approval gates, takes pasted images, and can speak its replies. If the folder has an `ASG.md`, `AGENTS.md` or `CLAUDE.md`, ASG adds it as project instructions.

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
```

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
    "ollama":     { "type": "ollama",    "base_url": "http://localhost:11434",        "model": "llama3.1:8b" },
    "anthropic":  { "type": "anthropic", "base_url": "https://api.anthropic.com",     "api_key": "sk-ant-…", "model": "claude-sonnet-5-5" },
    "openai":     { "type": "openai",    "base_url": "https://api.openai.com/v1",     "api_key": "sk-…",     "model": "gpt-5" },
    "openrouter": { "type": "openai",    "base_url": "https://openrouter.ai/api/v1",  "api_key": "sk-or-…",  "model": "…" }
  },
  "auto_approve": false,
  "audio": {
    "enabled": true,
    "engine": "auto",
    "fallback": { "engine": "ollama", "model": "phonon", "voice": "Juniper" },
    "per_model": { "nemotron-3.5-bolt:e4b": { "engine": "system", "voice": "Samantha" } }
  }
}
```

If a model rejects tool definitions, ASG switches to chat-only mode for it automatically.

---

## ▸ Commands

| Command | |
|---|---|
| `/paste` · **Ctrl+V** | attach a clipboard image |
| `/image <path>` | attach an image file (or drag it in, or `@path`) |
| `/model [name]` · `/models` | show / set / list models |
| `/provider [name]` | switch backend |
| `/voice …` · `/speak` | voice output: `/voice help` for everything |
| `/skills` · `/skill add\|rm\|show` | manage skills |
| `/<skill> <request>` | run a request with a skill |
| `/tools` · `/yolo` | list tools · toggle auto-approve |
| `/dashboard` | open the web version |
| `/clear` · `/usage` · `/config` · `/exit` | |

End a line with `\` for multi-line input. Pasted multi-line text arrives as one message. **Ctrl+C** stops a running turn, and pressing it twice at the prompt quits. **Tab** completes commands. Set `NO_COLOR=1` for plain output.

---

## ▸ Security

- **Gates.** `bash`, `write_file`, `edit_file` and `web_fetch` ask before running. **Always** remembers your answer for this session only, and `-yolo` lasts one run only.
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
