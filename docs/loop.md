# Loop harness

`/loop` takes an **objective** and drives it to a verified finish. The model
doesn't decide what happens next; the harness does: it plans, works on one step
at a time, has every step checked, runs your tests, and keeps going until
everything passes.

```
objective ─► PLAN            read-only look around → numbered steps
             REVIEW PLAN ×4   the plan is checked before anything runs
             │
      ┌─► WORK on ONE step   short fresh context: objective, ✓/▶ checklist, notes
      │   REVIEW step        separate call judges it from tool results, not claims
      │      ├─ pass → next step
      │      ├─ fail → retry with the feedback (3×) → split into smaller steps → ask you
      │      └─ blocked → stops and asks you
      └──────┘
             VERIFY          your test/build command; failures become a "fix" step
             FINAL STEPS     the flavor's finale: security test, report, last screenshot
             REVIEW ×4       the whole result; if it changed anything, verify again
```

## Starting a loop

```text
/loop add a --json flag to the list command, with tests
```

or switch to loop mode (Shift+Tab, or `/mode loop`) and type the objective as a
normal message:

```text
 LOOP   software/work  qwen3.5:9b ▸ add a --json flag to the list command, with tests
```

Write objectives as an outcome you can check:

| Weak | Better |
|---|---|
| `improve the CLI` | `add a --json flag to list that prints the same fields as the table, with tests` |
| `security stuff` | `/cyber blue audit ./deploy for exposed secrets and weak TLS, fix what's safe to fix` |
| `fix CI` | `/devops pipeline make the test job pass on windows-latest` |

## Plan first, then loop

To read and approve the plan yourself before anything changes:

```text
/plan add a --json flag to the list command, with tests      ← read-only, returns numbered steps
                                                              (read it, ask for changes if needed)
/loop                                                         ← runs exactly that plan
```

`/loop` with no objective runs the plan you just made in plan mode and skips its
own planning stage.

## What you see

```text
  ⟳ LOOP  planning
  ⟳ LOOP  plan ready · 4 steps · verify: go build ./... && go test ./...
  ▶ 1. call the JSON printer from list when --json is set
  · 2. implement the JSON printer
  · 3. tests for --json
  · 4. document --json in README
  ⟳ LOOP  step 1/4: call the JSON printer from list when --json is set
  ◆ EDIT_FILE cmd/list.go
  ⟳ LOOP  ✓ step 1/4 — flag parsed and printer called; build passes
  …
  ⟳ LOOP  verifying: go build ./... && go test ./...
  ⟳ LOOP  plan passed — adding 1 final step(s)
  ⟳ LOOP  step 5/5: Security test: review every file changed in this run …
  ⟳ LOOP  final review of the whole result
  ◇ review 1/4 anticipation code             ✓ nothing unused
  …
```

## Controlling a loop

| You do | What happens |
|---|---|
| **Ctrl+C** | pauses the loop after the current call; nothing is lost |
| `/loop status` | objective, state, steps done, and the checklist |
| `/loop resume` | continues a paused or stopped loop, even after restarting `asg` |
| `/loop resume use port 8081` | continues and passes your answer or extra instructions along |
| `/loop verify go test ./...` | sets the command that proves the work |
| `/loop verify none` | no verification command |
| `/loop verify auto` | back to auto-detect |

When a step gets stuck (it failed 3 times, couldn't be split, or the model needs
something only you know), the loop **stops and asks you**. Answer with
`/loop resume <answer>`. Mid-step questions come up as
[`ask_user`](review-and-questions.md#ask_user) prompts that you answer in place.

## Verification

The check command is auto-detected from the project:

| Found | Runs |
|---|---|
| `go.mod` | `go build ./... && go test ./...` |
| `Cargo.toml` | `cargo test` |
| `package.json` with a real `test` script | `npm test` (else `npm run build` if there is one) |
| `pyproject.toml` / `setup.py` / `pytest.ini` / `setup.cfg` plus a `tests/` or `test/` folder | `python -m pytest -q` |
| `Makefile` with a `test:` target | `make test` |

`/config` shows what was detected. If verification fails, the failure output
becomes a new "fix" step (up to 3 times) before the loop gives up and asks you.

## The domain changes the loop

The current [domain/flavor](domains.md) shapes the whole run:

- The **plan** follows the flavor's phases. In software/work that means the
  caller before the function, then test → document → verify → security.
- The **step reviewer** enforces the flavor's rules. In software/work, a step
  that adds anticipation code **fails** and is retried.
- **Final steps** run after verification passes:

| Flavor | Final step |
|---|---|
| software/work | security test of every changed file |
| docs/update, docs/write | last screenshot pass |
| cyber/red, cyber/blue | full `report.md`: scope, every action, every finding with evidence and fixes |
| devops/review | pipeline review `report.md`: every file, findings by severity with fixes |

## Files

- The run is saved after every step in `~/.asg/loops/`, never in your repo, so
  `/loop resume` works across restarts.
- `plan.md`, `checklist.md` and `walkthrough.md` in the
  [work folder](work-folder.md) are kept up to date by the harness while it runs.

## Unattended runs

```sh
asg -loop -yolo -p "add a --json flag to list, with tests"
asg -domain software/work -loop -yolo -verify "go test ./..." -p "…"
asg -domain cyber/blue -loop -p "audit ./deploy"     # still asks before each tool
```

`-yolo` skips the approval gates for that run. Without it, every change is still
gated. Use it only in a checkout you're happy to have changed.

## Tuning (`~/.asg/config.json`)

```json
"loop": { "max_iterations": 30, "step_attempts": 3, "step_rounds": 15, "verify": "" }
```

| Key | Meaning |
|---|---|
| `max_iterations` | step attempts across the whole run before the loop stops |
| `step_attempts` | retries per step before splitting it |
| `step_rounds` | tool calls allowed within one step |
| `verify` | fixed check command (`""` = auto-detect) |

Next: [Self-review and questions](review-and-questions.md)
