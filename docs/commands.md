# Command reference

## Keys

| Key | Where | Does |
|---|---|---|
| **Enter** | prompt | send |
| `\` at line end | prompt | continue on the next line |
| **/** | empty prompt | open the command menu |
| **Tab** | after `/xx` | complete the command |
| **↑ / ↓** | prompt | message history |
| **↑ / ↓**, Ctrl+P / Ctrl+N | menus | move |
| **Esc** | menus | close |
| **Shift+Tab** | prompt | next [mode](modes.md): build → plan → loop → ask |
| **Ctrl+T** | prompt | next [domain](domains.md): software → docs → cyber → devops → pm |
| **Ctrl+Y** | prompt or GATE | toggle [YOLO](permissions.md#yolo) |
| **Ctrl+V** | prompt | attach the clipboard image |
| **Ctrl+C** | while working | stop the turn (pauses a loop) |
| **Ctrl+C** ×2 | prompt | quit |

## Modes

| Command | Does |
|---|---|
| `/mode` | menu |
| `/mode build\|plan\|loop\|ask` | switch |
| `/build [request]` | switch to build (and send) |
| `/plan [request]` | switch to plan (and send) |
| `/ask [question]` | switch to ask (and send) |

## Domains

| Command | Does |
|---|---|
| `/domain` | two-step menu: domain, then flavor |
| `/domain <domain> [flavor] [request]` | switch (and send) |
| `/software [project\|work\|hobby] [request]` | software |
| `/docs [update\|write] [request]` | docs |
| `/cyber [red\|blue] [request]` | cyber |
| `/devops [review\|pipeline] [request]` | devops |
| `/pm [plan\|sprint\|status] [request]` | project manager |

## Loop

| Command | Does |
|---|---|
| `/loop <objective>` | start a loop run |
| `/loop` | run the plan from plan mode (or switch to loop mode) |
| `/loop status` | state and checklist of the saved run |
| `/loop resume [answer]` | continue after Ctrl+C, a question, or a restart |
| `/loop verify <cmd>` | set the check command (`auto`, `none`) |

## Review and work

| Command | Does |
|---|---|
| `/review` | show self-review setting |
| `/review <1–12>` | minimum passes after every answer |
| `/review off` | turn it off |
| `/work` | show the work folder and checklist |

## Models and providers

| Command | Does |
|---|---|
| `/model` | pick a model from a menu |
| `/model <name>` | set the model |
| `/models` | list the provider's models |
| `/provider` | pick a provider, then its model |
| `/provider <name>` | switch provider |

## Images

| Command | Does |
|---|---|
| `/paste` | attach the clipboard image (same as Ctrl+V) |
| `/image <path>` | attach an image file |
| `@path/to/img.png` in a message | attach inline |

## Tools, skills, memory, permissions

| Command | Does |
|---|---|
| `/tools` | list tools (reloads script tools) |
| `/tool new <name>` | create a script tool in `~/.asg/tools` |
| `/skills` | list installed skills |
| `/skill add <src>` | import a skill (path, URL, `github:owner/repo[/path]`) |
| `/skill rm <name>`, `/skill show <name>` | remove / show a skill |
| `/<skill-name> [request]` | run a skill |

Two skills ship with ASG: `/design-first-dev` (design diagrams first, wait for approval,
build skeleton-first, then tests, docs and a security check) and `/frontend-design`
(distinctive visual direction for UI work). They can't be removed, but a skill of your
own with the same name in `~/.asg/skills` replaces the built-in one.
| `/memory` | browse memories |
| `/memory <note>` | ask ASG to remember something |
| `/permissions` | review and revoke "always allow" rules |
| `/yolo` | toggle YOLO |

## Voice

| Command | Does |
|---|---|
| `/voice on\|off` | speak replies aloud |
| `/voice help` | every voice option |
| `/speak` | read the last reply aloud |

## Session

| Command | Does |
|---|---|
| `/init` | write `ASG.md` describing this project |
| `/clear` | new conversation |
| `/usage` | tokens used |
| `/config` | config file, provider, mode, track, review, loop settings |
| `/dashboard` | open the browser dashboard |
| `/help` | every command |
| `/exit`, `/quit` | quit |
