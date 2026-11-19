# AGENTS.md

Instructions for any agent (ASG, Claude, Codex, Cursor, …) working in this repo.
Project overview, commands and architecture live in [ASG.md](ASG.md) — read it too.

## Before you touch the UI — read this first

The terminal UI (`internal/tui/`) is built on the [Charm](https://charm.land/#libs) stack.
**Always read this section before changing anything the user sees in the terminal**:
the interactive REPL, menus, pickers, prompts, permission gates, spinners, markdown
output, colours, or log output.

### Rule 1 — read the library's GitHub docs before you implement

Before using or changing any Charm API, open that library's GitHub README, its
`examples/` folder, and the pkg.go.dev page **for the version pinned in `go.mod`**.
Do not code from memory: the v2 APIs differ a lot from v1 (and from most blog posts
and training data). If the docs and this file disagree, the docs win — then fix this file.

| Library | Import path | Used for | Docs |
|---|---|---|---|
| Bubble Tea | `charm.land/bubbletea/v2` | The REPL program: model / update / view, key + paste + resize messages, `Println` into scrollback | [GitHub](https://github.com/charmbracelet/bubbletea) · [examples](https://github.com/charmbracelet/bubbletea/tree/main/examples) · [v2 upgrade guide](https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md) · [pkg.go.dev](https://pkg.go.dev/charm.land/bubbletea/v2) |
| Bubbles | `charm.land/bubbles/v2` | Components: `textarea` (prompt), `spinner`, `key` + `help` (key hints) | [GitHub](https://github.com/charmbracelet/bubbles) · [pkg.go.dev](https://pkg.go.dev/charm.land/bubbles/v2) |
| Lip Gloss | `charm.land/lipgloss/v2` | **All** colour, borders, badges, padding and layout | [GitHub](https://github.com/charmbracelet/lipgloss) · [examples](https://github.com/charmbracelet/lipgloss/tree/main/examples) · [pkg.go.dev](https://pkg.go.dev/charm.land/lipgloss/v2) |
| Huh | `charm.land/huh/v2` | Pickers and forms: model/provider/mode/domain menus, permission gate, `ask_user`, API-key input | [GitHub](https://github.com/charmbracelet/huh) · [examples](https://github.com/charmbracelet/huh/tree/main/examples) · [pkg.go.dev](https://pkg.go.dev/charm.land/huh/v2) |
| Glamour | `charm.land/glamour/v2` | Rendering the model's markdown replies | [GitHub](https://github.com/charmbracelet/glamour) · [styles](https://github.com/charmbracelet/glamour/tree/main/styles) · [pkg.go.dev](https://pkg.go.dev/charm.land/glamour/v2) |
| Log | `charm.land/log/v2` | CLI diagnostics on stderr and the `ASG_DEBUG` log file | [GitHub](https://github.com/charmbracelet/log) · [pkg.go.dev](https://pkg.go.dev/charm.land/log/v2) |
| Harmonica | `github.com/charmbracelet/harmonica` | Spring physics for animation (the working crab) | [GitHub](https://github.com/charmbracelet/harmonica) · [pkg.go.dev](https://pkg.go.dev/github.com/charmbracelet/harmonica) |
| Wish | `charm.land/wish/v2` | *Not used yet.* The candidate if ASG is ever served over SSH | [GitHub](https://github.com/charmbracelet/wish) |

Never import the old v1 paths (`github.com/charmbracelet/bubbletea`, `…/lipgloss`,
`…/bubbles`, `…/huh`, `…/glamour`, `…/log`). Harmonica is the one exception: it has
no v2 and still lives at `github.com/charmbracelet/harmonica`.

### Rule 2 — how the TUI is put together

- `internal/tui/app.go` — the Bubble Tea model. It owns the screen while ASG runs.
  The live area at the bottom holds: the in-progress reply, the working animation,
  any open picker / gate / question, the input box and the status strip.
  Finished output is pushed into the normal terminal scrollback with
  `Program.Println`, so history stays scrollable and copyable (no alt screen).
- Agent turns and slash commands run on a **worker goroutine**, never inside
  `Update`. They talk to the UI only through `REPL` methods that send messages
  to the program (`r.printf`, `r.choose`, `Approve`, `AskUser`, …). Anything that
  needs an answer from the user sends a request message carrying a reply channel
  and blocks on it; `Update` opens a Huh form and answers the channel when the form
  completes or is aborted.
- `internal/tui/theme.go` — the palette and every Lip Gloss style. Add new styles
  here; do not build ad-hoc styles inside views.
- `internal/tui/mdrender.go` — Glamour renderer with the ASG style. Streamed text is
  committed to scrollback one finished block at a time; only the block still being
  written is re-rendered in the live area.
- `internal/tui/form.go` — Huh theme and the form builders (pickers, gate, ask).

### Rule 3 — UI conventions

- **No raw ANSI and no `fmt.Print*` to stdout** while the program is running. Writing
  to stdout behind Bubble Tea's back corrupts the screen. Use `r.printf` / `r.println`
  (which route through the program) and Lip Gloss styles from `theme.go`.
- **Colours come from `theme.go` only.** The "signal console" palette: lime = ASG,
  cyan = tools, magenta = skills / questions, amber = gates and the loop, red = errors.
  Lip Gloss downsamples colours to what the terminal supports and respects
  `NO_COLOR`; don't add your own colour detection.
- **Width-aware.** Read the width from `tea.WindowSizeMsg` (kept on the model);
  clip or wrap with Lip Gloss (`Width`, `MaxWidth`) instead of counting bytes.
  Use `lipgloss.Width` for visible width — never `len()` on a styled string.
- **Keys** are declared once as `key.Binding`s in `keys.go` so the help line and
  the handlers can't drift apart. Keep the existing shortcuts working:
  `/` menu, `shift+tab` mode, `ctrl+t` domain, `ctrl+y` YOLO, `ctrl+v` image,
  `ctrl+c` stop / quit, `ctrl+j` or `alt+enter` newline.
- **Animation** is optional: honour `ASG_NO_ANIMATION` and drive frames with
  `tea.Tick` (never `time.Sleep` in `Update`). Use Harmonica springs for motion.
- **Non-interactive paths stay plain.** `asg -p`, `asg skill …`, `asg tool …` print
  plain text (stdout for answers, stderr for diagnostics via `charm.land/log`) so they
  can be piped. Don't start a Bubble Tea program there.
- Keep pure logic (filtering, formatting, command parsing) in plain functions with
  unit tests; `go test ./...` must pass. Use `teatest`-style message tests for the
  model only where they add real value.

### Rule 4 — after a UI change

1. `go build ./... && go vet ./... && go test ./...`
2. Run `go run ./cmd/asg` in a real terminal (Windows Terminal *and* a narrow window)
   and try: `/` menu filtering, `/model`, `shift+tab`, `ctrl+t`, a tool call that
   hits the permission gate, a multi-line paste, `ctrl+c` during a turn.
3. Update `docs/` and the README if you changed a key, command or what the user sees.
