# ASG.md

ASG ("agent shell") is a single-binary agentic coding assistant in Go. It talks
to Ollama, Anthropic and OpenAI-compatible models, runs tools (shell, files, web,
office documents, screenshots, script tools) behind permission gates, and comes
with an interactive terminal UI and a browser dashboard that share one config.

**Touching the terminal UI? Read [AGENTS.md](AGENTS.md) first** — it lists the
Charm libraries the TUI is built on and the rules for using them.

## Commands

```sh
go build ./...                      # build everything
go run ./cmd/asg                    # interactive TUI in the current folder
go run ./cmd/asg -p "question"      # one-shot, plain output (no TUI)
go run ./cmd/asg dashboard          # browser dashboard
go test ./...                       # all tests
go test ./internal/tui -run TestSlashMenu   # a single test
go test -race ./...                 # CI runs this too
gofmt -l . && go vet ./...          # lint (make lint)
go mod tidy                         # CI fails if go.mod / go.sum are not tidy
```

`make build | test | lint | run | dashboard | snapshot` wrap the same commands.
Go version comes from `go.mod` (CI uses `go-version-file`).

## Architecture

- `cmd/asg` — flags and subcommands (`dashboard`, `skill`, `tool`, `update`,
  `version`), the one-shot `-p` path (`printEvents`, diagnostics via `charm.land/log`
  on stderr) and the start of the TUI.
- `internal/agent` — the core: `App` (config, tools, skills, memory, perms),
  `Session` (history, model ↔ tool loop, `Send`), modes (build / plan / loop / ask),
  domains and flavors (`workflow.go`), the loop harness (`loop.go`), self-review
  (`review.go`), `ask_user`, the per-request work folder. UIs implement the
  `agent.Events` interface (plus optional `Asker`, `LoopEvents`, `ReviewEvents`).
- `internal/tui` — the terminal UI: a Bubble Tea program (`app.go`) with a
  worker goroutine for turns and commands (`repl.go`), Huh forms (`form.go`),
  Glamour markdown (`mdrender.go`), Lip Gloss theme (`theme.go`). See AGENTS.md.
- `internal/llm` — provider-neutral types and the Ollama / OpenAI / Anthropic
  streaming adapters.
- `internal/tools` — built-in tools and external script tools (`tools/README.md`).
- `internal/dashboard` — HTTP server plus the embedded web UI.
- `desktop/` — the desktop app (separate Go module, Wails v2, plain HTML/CSS/JS UI in
  `desktop/frontend/dist`): persisted sessions, projects, coding sessions, artifacts,
  schedules. See [docs/desktop.md](docs/desktop.md). Build/test with `make desktop` /
  `make desktop-test`; `go test ./...` at the root does not include it. CI's `desktop` job
  tests and packages it on Windows/macOS/Linux, and the release's `publish-desktop` job
  attaches `asg-desktop_*` archives built by `desktop/build/package.sh`.
- `internal/proc` — `proc.Hide` keeps child processes from flashing a console window when
  the parent is the GUI app; use it for every `exec.Command` a tool runs.
- `internal/config` — `~/.asg/config.json` (override the folder with `ASG_HOME`).
- `internal/skills/builtin/<name>/SKILL.md` — skills embedded in the binary and listed
  after the user's (a user skill with the same name wins; built-ins can't be removed).
- `internal/perms`, `internal/memory`, `internal/skills`, `internal/media`,
  `internal/audio`, `internal/sprite` (pixel crab; `UPDATE_SPRITES=1 go test
  ./internal/sprite/` refreshes the SVGs in `docs/`), `internal/markdown` (a small
  parser used by the office writer and screenshot tool — the TUI uses Glamour).
- `tools/office`, `tools/screenshot` — bundled tools.
- `docs/` — user docs; keep them in sync with what the user sees.

## Conventions

- Plain standard-library Go plus the Charm stack for the TUI; avoid new
  dependencies unless they clearly pay for themselves.
- Errors are returned and shown to the user, never panicked on. In the TUI,
  print them with the theme's `red(...)`.
- Tests sit next to the code (`*_test.go`), table-driven, standard `testing`
  only. Tests that need config set `ASG_HOME` to `t.TempDir()`.
- Commits and PR titles use Conventional Commits (`feat:`, `fix:`, `docs:`,
  `ci:`, `chore:` …); `pr-title.yml` enforces it and release-please derives the
  version from it (feat → minor, fix → patch).
- Work happens on branches off `main` (or `dev` for integration); open a PR.

## Gotchas

- Development happens on Windows as well as Unix: keep paths portable, and test
  the TUI in Windows Terminal. CI runs on Linux, macOS and Windows.
- Never write to stdout while the TUI runs — Bubble Tea owns it. Use
  `ASG_DEBUG=<file>` for diagnostics.
- `asg -p` output must stay pipeable: answers on stdout, everything else on stderr.
- `.asg/work/` (per-request work folders) is git-ignored; don't commit it.
- Plan and ask modes must never be offered tools that change the project.
