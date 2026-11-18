# ASG terminal guide

How to drive `asg` from the terminal: every key, every command, and worked
examples for each way of working.

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Starting `asg`, reading the screen, the prompt, the keys you'll use every day |
| [Modes](modes.md) | **How** ASG works: build, plan, loop, ask — switching with Shift+Tab and `/mode` |
| [Domains](domains.md) | **What kind of work** ASG does: software, docs, cyber, devops, pm — and their sub-modes (software/project → software/work, cyber/red → cyber/blue, …) |
| [Loop harness](loop.md) | Running an objective step by step until it is verified: `/loop` |
| [Self-review and questions](review-and-questions.md) | The 4+ review passes after every answer, and how ASG stops to ask you things |
| [Work folder](work-folder.md) | The plan, checklist, walkthrough and activity log ASG writes while it works |
| [Tools](tools.md) | The tools the model can call, Word/PowerPoint/Excel output, screenshots, and adding your own tools |
| [Permissions and YOLO](permissions.md) | Approval gates, "always allow", and running without asking |
| [Command reference](commands.md) | Every slash command and key on one page |
| [Command line and config](cli-and-config.md) | `asg` flags, one-shot and unattended runs, `~/.asg/config.json` |

## The two ideas to know first

ASG has two independent switches. You set both, and the prompt always shows both:

```
 BUILD  software/work  qwen3.5:9b ▸
 ─────  ─────────────
  mode      domain/flavor
```

- **Mode** — *how* ASG works this turn: make changes (`build`), plan read-only
  (`plan`), run a whole objective step by step (`loop`), or answer questions
  (`ask`). Cycle with **Shift+Tab**.
- **Domain / flavor** — *what kind of work* it is, and therefore which loop and
  rules the model follows: `software/work`, `docs/update`, `cyber/blue`, … Cycle
  domains with **Ctrl+T**; pick a flavor with `/software work`, `/cyber blue`, ….

Any mode combines with any domain: `PLAN cyber/red` plans an engagement without
touching anything; `LOOP software/work` builds a feature end to end with the
strict work rules; `ASK devops/review` answers questions about your pipelines.

## A two-minute tour

```text
$ asg
 BUILD  software/project  qwen3.5:9b ▸ /software work           ← strict rules for a work codebase
 BUILD  software/work  qwen3.5:9b ▸ add a --json flag to `list`  ← builds it: caller first, tests, docs, verify, security
 BUILD  software/work  qwen3.5:9b ▸ <shift+tab>                  ← now in PLAN
 PLAN   software/work  qwen3.5:9b ▸ how should we add caching?   ← read-only, returns a numbered plan
 PLAN   software/work  qwen3.5:9b ▸ /loop                        ← runs that plan step by step, verified
 BUILD  software/work  qwen3.5:9b ▸ /cyber blue audit the nginx config in ./deploy
 BUILD  cyber/blue  qwen3.5:9b ▸ /work                           ← look at the report and checklist
```
