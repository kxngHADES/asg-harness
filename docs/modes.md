# Modes

A **mode** decides *how* ASG works on your next message: whether it may change
files, whether it only plans, or whether it drives a whole objective to
completion. It is independent of the [domain](domains.md), which decides *what
kind of work* it is.

| Mode | Badge | Tools | What your next message does |
|---|---|---|---|
| **build** | ` BUILD ` lime | all, behind [gates](permissions.md) | reads, edits and runs commands — the default |
| **plan** | ` PLAN ` cyan | read-only | investigates and writes a numbered plan; changes nothing |
| **loop** | ` LOOP ` amber | all, behind gates | is an *objective*: plan → do → verify → review, step by step until done ([loop](loop.md)) |
| **ask** | ` ASK ` magenta | read-only | answers questions about your code from the actual files |

"Read-only" means the model is not even offered `bash`, `write_file` or
`edit_file`. It can still read files, search, fetch web pages, take
[screenshots](tools.md#screenshot), save memories and [ask you
questions](review-and-questions.md#ask_user).

## Switching modes

There are four ways; pick whichever is fastest for you.

### 1. Shift+Tab — cycle

Press **Shift+Tab** at the prompt. The order is build → plan → loop → ask →
build. The status line above the prompt is redrawn in place, so you can tap
through quickly and watch the highlight move:

```
   build  plan  loop  ask  │ software/work · strict: only code this build needs, caller first, …
 BUILD  software/work  qwen3.5:9b ▸

            ⇧ Shift+Tab

   build [plan] loop  ask  │ software/work · read-only: investigates and writes a step-by-step plan, changes nothing
 PLAN   software/work  qwen3.5:9b ▸
```

Whatever you had already typed on the line stays there.

### 2. `/mode` — menu

```text
/mode
```

opens a menu with each mode and its description; ↑/↓ and Enter pick one, Esc
cancels. `/mode plan` (or `/mode p`) switches directly.

Aliases accepted by `/mode` and `asg -mode`:

| Mode | Also accepts |
|---|---|
| build | `b`, `code`, `act` |
| plan | `p` |
| loop | `l`, `auto` |
| ask | `a`, `chat`, `explain` |

### 3. Switch and send in one go

Put the request after the mode command:

```text
/plan how should we add rate limiting to the API?
/build add the rate limiter from the plan
/ask where is the session token validated?
/loop add a --json flag to the list command, with tests
```

ASG switches mode **and** sends the request, so you don't need two steps. The
mode stays switched afterwards.

### 4. From the command line

```sh
asg -mode plan
asg -mode ask
asg -loop -p "add a --json flag to list, with tests"     # one-shot loop run
```

`-mode` applies to that run only; ASG starts in build otherwise.

## Each mode in practice

### build

```text
 BUILD  software/project  qwen3.5:9b ▸ rename the `cfg` package to `config` and fix the imports
  ◆ GREP cfg\.
  ◆ EDIT_FILE cmd/asg/main.go
  ▛▀ GATE ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  ▌ EDIT_FILE requests permission
  ▌ cmd/asg/main.go
  ▌ - "github.com/me/app/cfg"
  ▌ + "github.com/me/app/config"
  ▙▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  [y] run  [a] always edit_file here  [g] everywhere  [ctrl+y] yolo  [n] block ▸ y
```

- Every change goes through a [GATE](permissions.md) unless YOLO is on.
- The [work folder](work-folder.md) (`plan.md`, `checklist.md`,
  `walkthrough.md`, …) is created on the first request.
- After the answer, ASG [reviews its own work](review-and-questions.md) at least
  4 times.

### plan

```text
 PLAN   software/work  qwen3.5:9b ▸ how should we add caching to the search endpoint?
  ◆ READ_FILE internal/search/handler.go
  ◆ GREP Search\(
  ┃ ## Plan
  ┃ 1. Add a `searchCache` call in `handler.Search` (caller first) …
  ┃ 2. Implement `searchCache` as an LRU keyed by query + page …
  ┃ 3. Tests: cache hit, miss, eviction …
  ┃ 4. Document the new `-cache-size` flag …
```

Nothing is changed. Then either:

- `/loop` — runs **exactly this plan** step by step with verification (the loop
  skips its own planning stage), or
- `/build` (or Shift+Tab to build) and say "go ahead" — the plan stays in the
  conversation and build mode follows it.

### loop

In loop mode every message you send is an objective for the [loop
harness](loop.md):

```text
 LOOP   software/work  qwen3.5:9b ▸ add a --json flag to the list command, with tests
  ⟳ LOOP  planning
  ⟳ LOOP  plan ready · 4 steps · verify: go build ./... && go test ./...
  ⟳ LOOP  step 1/4: wire --json into the list command (caller first)
  …
```

Ctrl+C pauses a loop; `/loop resume` continues it.

### ask

```text
 ASK    software/project  qwen3.5:9b ▸ what happens when the config file is missing?
  ◆ READ_FILE internal/config/config.go
  ┃ `Load` returns the defaults and writes nothing until you change a setting …
```

Answers come from the files it reads, not from guesses. Use ask mode when you
want an explanation and want to be sure nothing is touched.

## Mode × domain

The mode and the [domain](domains.md) combine freely. A few useful pairings:

| You want to… | Mode | Domain |
|---|---|---|
| ship a feature with strict, verified rules | loop | software/work |
| hack on a side project quickly | build | software/hobby |
| scope a pen test before touching anything | plan | cyber/red |
| harden a server config and get a report | loop | cyber/blue |
| audit your GitHub Actions without changes | ask | devops/review |
| fix a broken pipeline | build | devops/pipeline |
| break an epic into a sprint | plan | pm/sprint |
| fix docs that render badly | build | docs/update |

```text
/plan
/cyber red
scope a test of staging.example.com — what would you check first?
```

Next: [Domains](domains.md)
