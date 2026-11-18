# Permissions and YOLO

## Gates

Before a tool changes something or reaches out, ASG stops and shows a **GATE**:

```
  ▛▀ GATE ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  ▌ BASH requests permission
  ▌ go test ./...
  ▙▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  [y] run  [a] always go here  [g] everywhere  [ctrl+y] yolo  [n] block ▸
```

| Answer | Effect |
|---|---|
| `y` or **Enter** | run it this once |
| `a` | always allow this kind of call **in this project** (remembered) |
| `g` | always allow it **everywhere** (remembered) |
| **Ctrl+Y** | turn YOLO on **and** run this call |
| `n` | block it; the model is told and has to find another way |
| **Ctrl+C** | block it and stop the turn |

File edits show a red/green preview of what changes before you answer.

### What asks

| Asks | Doesn't ask |
|---|---|
| `bash`, `write_file`, `edit_file`, `web_fetch`, `office_document`, `screenshot`, script tools (unless `needs_approval: false`) | reading, listing and searching files, `ask_user`, memories, skills, and writes inside the [work folder](work-folder.md) |

### "Always" rules

For shell commands an "always" rule covers the **program**. Allowing
`git status` also allows `git log`, but `git status && rm -rf build` still asks
about `rm`. Commands that contain `$(…)` or backticks always ask.

```text
/permissions        list remembered rules; Enter on one revokes it
```

Rules are stored in `~/.asg/permissions.json`.

## YOLO

YOLO runs every tool **without asking**.

| Turn it on | |
|---|---|
| **Ctrl+Y** | toggle at the prompt |
| `/yolo` | same |
| Ctrl+Y at a GATE | turn on and approve the waiting call |
| `asg -yolo` | for that run |

While it's on, the prompt shows it:

```
  ⚡YOLO   BUILD  software/work  qwen3.5:9b ▸
```

YOLO lasts until you turn it off or quit, so it never carries over to the next
session by accident. Turning it off also clears a saved `"auto_approve": true`.

Use YOLO in a disposable checkout or a container, or with a loop whose objective
you trust. Don't use it when the model is fetching and acting on content you
haven't seen.

## Read-only modes

Plan and ask modes don't even *offer* the model a tool that changes your
project, so nothing can be changed there with or without YOLO.

Next: [Command reference](commands.md)
