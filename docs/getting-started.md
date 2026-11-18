# Getting started

## Start it

```sh
asg                          # interactive terminal agent in the current folder
asg -provider anthropic      # switch provider (saved as your default)
asg -model qwen3.5:9b        # switch model (saved as your default)
asg -mode plan               # start in plan mode (this run only)
asg -domain software/work    # software domain, work flavor (saved as your default)
```

Run `asg` **from the project folder** you want to work on — the working
directory is what the model reads, edits and runs commands in. Project
instructions are read from `ASG.md` (or `AGENTS.md` / `CLAUDE.md`) in that
folder; `/init` writes an `ASG.md` for you.

## Reading the screen

When `asg` starts you see the crab banner, then a key strip and the status line:

```
  ASG  agent shell
  ollama/qwen3.5:9b  software/project
  E:\code\myapp
  type / for commands
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  / menu  ·  shift+tab mode  ·  ctrl+t domain  ·  ctrl+y yolo  ·  ctrl+v image  ·  ctrl+c stop

   build  plan  loop  ask  │ software/project · balanced: solid, tested code for your own or a team project
 BUILD  software/project  qwen3.5:9b ▸ _
```

**The status line** (above the prompt) shows every mode with the current one
highlighted, then the domain/flavor and a one-line description of what it will
do. When you press Shift+Tab or Ctrl+T it is redrawn **in place** — it doesn't
add a new line per key press.

**The prompt** reads left to right:

| Part | Meaning |
|---|---|
| `▣ 2 img` | images attached to your next message (only when there are some) |
| ` ⚡YOLO ` | tools run without asking (only while YOLO is on) |
| ` BUILD ` | the mode, colour-coded: build lime, plan cyan, loop amber, ask magenta |
| `software/project` | the domain/flavor, colour-coded per domain |
| `qwen3.5:9b` | the active model |

## Typing

| You do | What happens |
|---|---|
| type and press **Enter** | sends the message |
| end a line with `\` | continues on the next line (multi-line message) |
| paste multi-line text | arrives as one message |
| type `/` on an empty prompt | opens the command menu |
| **Tab** after `/mo` | completes the command (`/mode`, `/model`, …) |
| `@shot.png` or drag a file in | attaches that image |
| **Ctrl+V** | attaches the image on the clipboard |
| **Ctrl+C** while ASG works | stops the current turn (a loop pauses and can be resumed) |
| **Ctrl+C** twice at the prompt | quits (so does `/exit`) |

Typos are forgiven: `//model` works like `/model`, and a bare `clear`, `exit`,
`quit` or `help` runs the command.

## The command menu

Press `/` on an empty prompt:

```
 BUILD  software/project  qwen3.5:9b ▸ /mo
  ▸ /mode      pick how ASG works (or press shift+tab to cycle)
    /model     pick a model from a menu, or set one by name
    /models    list models available from the active provider
    ↑↓ move · type to filter · enter select · esc cancel
```

- type to filter, **↑ / ↓** (or Ctrl+P / Ctrl+N) to move, **Enter** to run,
  **Esc** to close
- commands that need an argument (like `/image <path>`) are inserted for you to
  finish; typing a space keeps writing by hand
- installed skills appear in the same menu

## While ASG works

```
  ◆ READ_FILE internal/list.go
  ┊      1	package main
  ┊      2	…
  ◆ EDIT_FILE internal/list.go
  ┃ Added the --json flag and a test for it.
  ◇ review 1/4 anticipation code            ✓ nothing unused
  ◇ review 2/4 caller-first and wiring      ✓ flag is read in main
  ◇ review 3/4 tests and verification       ↻ re-ran go test, fixed a failing case
  ◇ review 4/4 security                     ✓ no new input paths
  ── 41.2s · 9120↑ 1804↓ tokens ──
  work folder: .asg/work/2026-11-18-add-a-json-flag/ (plan · checklist · walkthrough · activity — /work)
```

- `◆ TOOL` lines are tool calls, with a preview of the result under them
- `┃` is the model's reply
- `◇ review n/4` lines are the self-review passes ([details](review-and-questions.md))
- an amber **GATE** box means a tool wants permission ([details](permissions.md))
- a magenta **ASG NEEDS YOUR INPUT** box means the model is asking you something

## Everyday commands

```text
/help                 every command
/model                pick a model from a menu (or /model qwen3.5:9b)
/provider             pick a provider, then its model
/clear                start a new conversation
/usage                tokens used so far
/config               where the config lives and what is active
/init                 write ASG.md describing this project
/memory               browse saved memories (or /memory <note> to save one)
/dashboard            open the browser version on the same config
```

Next: [Modes](modes.md) · [Domains](domains.md)
