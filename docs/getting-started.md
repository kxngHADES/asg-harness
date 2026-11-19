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

When `asg` starts you see the crab banner. Below it, at the bottom of the
terminal, is the live input area:

```
  ASG  agent shell
  ollama/qwen3.5:9b  software/project
  E:\code\myapp
  type / for commands
────────────────────────────────────────────────────────────────────────────
   build  plan  loop  ask  │ software/project │ qwen3.5:9b · balanced: solid, tested code…
▸ ask anything · / for commands
  / menu • shift+tab mode • ctrl+t domain • ctrl+y yolo • ctrl+c stop • ctrl+v image
```

**The status line** (above the input) reads left to right:

| Part | Meaning |
|---|---|
| ` build  plan  loop  ask ` | every mode, the current one highlighted: build lime, plan cyan, loop amber, ask magenta |
| `software/project` | the domain/flavor, colour-coded per domain |
| `qwen3.5:9b` | the active model |
| ` ⚡ YOLO ` | tools run without asking (only while YOLO is on) |
| `▣ 2 img` | images attached to your next message (only when there are some) |
| `· balanced: …` | what the current mode / flavor will do |

It is part of the live screen, so Shift+Tab and Ctrl+T simply change it in
place. The line under the input lists the main shortcuts; it is replaced by a
hint when there is something to tell you (e.g. "press ctrl+c again to exit").

Everything ASG prints — your messages, replies, tool calls — goes into the
normal terminal scrollback, so you can scroll up, search and copy as usual.

## Typing

| You do | What happens |
|---|---|
| type and press **Enter** | sends the message |
| **Ctrl+J** / **Alt+Enter**, or end a line with `\` | adds a new line (multi-line message) |
| paste multi-line text | arrives as one message |
| **↑ / ↓** | recall earlier inputs |
| type `/` | opens the command menu |
| `@shot.png` or drag a file in | attaches that image |
| **Ctrl+V** | attaches the image on the clipboard |
| **Ctrl+C** while ASG works | stops the current turn (a loop pauses and can be resumed) |
| **Ctrl+C** with text typed | clears the input |
| **Ctrl+C** twice at an empty prompt | quits (so do **Ctrl+D**, `/exit`) |

Typos are forgiven: `//model` works like `/model`, and a bare `clear`, `exit`,
`quit` or `help` runs the command.

## The command menu

Type `/` and the menu opens under the input, filtered as you type:

```
▸ /mo
  ▸ /model     pick a model from a menu, or set one by name
    /mode      pick how ASG works (or press shift+tab to cycle)
    /models    list models available from the active provider
    ↑↓ move · tab complete · enter run · esc close  1/10
```

- **↑ / ↓** (or Ctrl+P / Ctrl+N) to move, **Tab** to complete, **Enter** to run,
  **Esc** to close; an exact match (`/mode`) always comes first
- commands that need an argument (like `/image <path>`) are inserted for you to
  finish; typing a space closes the menu so you can write arguments by hand
- installed skills appear in the same menu

## Pickers and questions

`/model`, `/provider`, `/mode`, `/domain`, `/memory` and `/permissions` open a
picker: **↑ / ↓** to move, **Enter** to choose, **/** to filter long lists, **Esc**
to close without changing anything. The permission **GATE** and ASG's questions
use the same kind of form; the gate also takes a single key — **y** run,
**a** always here, **g** always everywhere, **n** block, **ctrl+y** YOLO.

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
