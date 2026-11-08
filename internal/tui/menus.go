package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/memory"
	"github.com/kxngHADES/asg-harness/internal/perms"
)

// slashMenu runs inside the line editor when '/' is typed on an empty
// prompt. It returns the new line contents for the editor.
func (r *REPL) slashMenu() (string, int, bool) {
	var items []menuItem
	needsArg := map[string]bool{}
	for _, c := range commands {
		items = append(items, menuItem{Label: c.name, Detail: c.help, Value: c.name})
		needsArg[c.name] = c.needsArg()
	}
	for _, s := range r.app.Skills.List() {
		items = append(items, menuItem{Label: "/" + s.Name, Detail: s.Description, Hint: "skill", Value: "/" + s.Name})
		needsArg["/"+s.Name] = true // skills take a request
	}
	p := &picker{items: items, slash: true, prompt: r.prompt(), rows: min(8, len(items))}
	it, how := p.run(r.in, r.out)
	p.clear(r.out)
	fmt.Fprint(r.out, r.prompt())

	line := "/" + p.filter
	switch how {
	case pickChose:
		if needsArg[it.Value] {
			return it.Value + " ", len(it.Value) + 1, true
		}
		r.in.unread([]byte{'\r'}) // run it straight away
		return it.Value, len(it.Value), true
	case pickTyped:
		if !strings.HasSuffix(line, " ") {
			r.in.unread([]byte{'\r'}) // Enter with no match: submit as typed
		}
		return line, len(line), true
	}
	if p.filter == "" {
		return "", 0, true
	}
	return line, len(line), true
}

// choose shows a standalone menu and returns the picked item, or nil.
func (r *REPL) choose(title string, items []menuItem, sel int) *menuItem {
	if len(items) == 0 {
		return nil
	}
	old, err := term.MakeRaw(r.fd)
	if err != nil {
		return nil
	}
	defer term.Restore(r.fd, old)
	p := &picker{items: items, title: title, sel: sel, rows: min(10, len(items))}
	it, how := p.run(r.in, r.out)
	p.clear(r.out)
	if how != pickChose {
		return nil
	}
	return it
}

func (r *REPL) pickProvider(ctx context.Context) {
	cfg := r.app.Config.Get()
	var items []menuItem
	sel := 0
	for i, n := range cfg.ProviderNames() {
		pc := cfg.Providers[n]
		it := menuItem{Label: n, Detail: pc.Type + " · " + pc.Model, Value: n, Mark: n == cfg.ActiveProvider}
		if pc.Type != "ollama" && pc.APIKey == "" {
			it.Hint = "no API key"
		}
		if it.Mark {
			sel = i
		}
		items = append(items, it)
	}
	items = append(items, menuItem{Label: "+ add or edit providers…", Detail: "opens the dashboard", Value: "\x00dashboard"})
	it := r.choose("provider", items, sel)
	if it == nil {
		r.printf("  %sprovider unchanged (%s)%s\n", slate, cfg.ActiveProvider, reset)
		return
	}
	if it.Value == "\x00dashboard" {
		r.command("/dashboard")
		return
	}
	name := it.Value
	if pc := cfg.Providers[name]; pc.Type != "ollama" && pc.APIKey == "" {
		key := r.readSecret(fmt.Sprintf("  %s%s needs an API key%s — paste it (hidden, enter to skip) ▸ ", amber, name, reset))
		if key != "" {
			r.update(func(c *config.Config) error { c.Providers[name].APIKey = key; return nil })
			r.printf("  %s✓ key saved to %s%s\n", green, r.app.Config.Path(), reset)
		}
	}
	r.update(func(c *config.Config) error { c.ActiveProvider = name; return nil })
	r.printf("  %sprovider → %s%s\n", dim, name, reset)
	r.pickModel(ctx)
}

func (r *REPL) pickModel(ctx context.Context) {
	p, cur, err := r.app.Provider()
	cfg := r.app.Config.Get()
	name, _ := cfg.Active()
	var models []string
	if err == nil {
		lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		sp := startSpinner("loading " + name + " models")
		models, err = p.ListModels(lctx)
		sp.Stop()
		cancel()
	}
	if err != nil || len(models) == 0 {
		if err == nil {
			err = fmt.Errorf("no models found")
		}
		r.printf("  %scouldn't list models: %v%s\n  %smodel stays %s — set one with /model <name>%s\n", yellow, err, reset, slate, cur, reset)
		return
	}
	var items []menuItem
	sel, found := 0, false
	for i, m := range models {
		items = append(items, menuItem{Label: m, Value: m, Mark: m == cur})
		if m == cur {
			sel, found = i, true
		}
	}
	if !found && cur != "" {
		items = append([]menuItem{{Label: cur, Value: cur, Mark: true, Hint: "current"}}, items...)
	}
	it := r.choose("model · "+name, items, sel)
	if it == nil {
		r.printf("  %smodel stays %s%s\n", slate, cur, reset)
		return
	}
	r.update(func(c *config.Config) error { c.Providers[c.ActiveProvider].Model = it.Value; return nil })
	r.printf("  %smodel → %s%s%s\n", dim, bold, it.Value, reset)
}

// readSecret reads a line without echo.
func (r *REPL) readSecret(prompt string) string {
	old, err := term.MakeRaw(r.fd)
	if err != nil {
		return ""
	}
	defer term.Restore(r.fd, old)
	s, err := r.t.ReadPassword(prompt)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func (r *REPL) memoryMenu() {
	notes := r.app.Memory.List()
	if len(notes) == 0 {
		r.printf("  %sno memories yet. Tell ASG a lasting preference (\"always use pytest\") or run /memory <note>.%s\n  %sstored in %s and %s%s\n",
			slate, reset, dim, r.app.Memory.Dir(memory.User), r.app.Memory.Dir(memory.Project), reset)
		return
	}
	var items []menuItem
	for i, n := range notes {
		items = append(items, menuItem{Label: n.Name, Detail: n.Summary, Hint: n.Scope, Value: fmt.Sprint(i)})
	}
	it := r.choose("memory", items, 0)
	if it == nil {
		return
	}
	i, _ := strconv.Atoi(it.Value)
	n := notes[i]
	r.printf("\n  %s %s %s %s(%s)%s\n", onLime, n.Name, reset, slate, n.Scope, reset)
	md := newMDStream()
	md.Write(n.Body + "\n")
	md.Flush()
	r.printf("  %s%s%s\n\n", dim, n.Path, reset)
	act := r.choose("action", []menuItem{
		{Label: "keep", Value: "keep"},
		{Label: "delete", Detail: "forget this note", Value: "delete"},
	}, 0)
	if act != nil && act.Value == "delete" {
		if err := r.app.Memory.Delete(n.Scope, n.Name); err != nil {
			r.printf("%s%v%s\n", red, err, reset)
			return
		}
		r.printf("  %sforgot %s%s\n", dim, n.Name, reset)
	}
}

func (r *REPL) permissionsMenu() {
	cwd, _ := os.Getwd()
	global, local := r.app.Perms.List(cwd)
	if len(global)+len(local) == 0 {
		r.printf("  %snothing remembered yet. Answer a GATE with [a] (this project) or [g] (everywhere).%s\n", slate, reset)
		return
	}
	var items []menuItem
	for _, rule := range local {
		items = append(items, menuItem{Label: ruleLabel(rule), Detail: "this project", Value: rule})
	}
	for _, rule := range global {
		items = append(items, menuItem{Label: ruleLabel(rule), Detail: "everywhere", Value: rule})
	}
	it := r.choose("always allowed · enter revokes", items, 0)
	if it == nil {
		return
	}
	if err := r.app.Perms.Remove(cwd, it.Value); err != nil {
		r.printf("%s%v%s\n", red, err, reset)
		return
	}
	r.printf("  %srevoked %s — ASG will ask again%s\n", dim, ruleLabel(it.Value), reset)
}

func ruleLabel(rule string) string {
	if strings.HasPrefix(rule, "bash:") {
		return perms.Describe([]string{rule}) + " (shell)"
	}
	return rule
}

const initPrompt = `Set up ASG.md for this project. ASG.md sits in the project root and is loaded into your instructions at the start of every future session here, so it should hold what a new agent needs to work in this codebase effectively.

%s

Steps:
1. Explore before writing: list the root, read the README, build files (package.json, go.mod, pyproject.toml, Cargo.toml, Makefile, …), CI config, and a few core source files. Also read any AGENTS.md, CLAUDE.md, .cursorrules, .cursor/rules or .github/copilot-instructions.md and carry over what is still accurate.
2. Write ASG.md with write_file. Include only what you verified:
   - one or two sentences on what the project is
   - exact commands to build, run, test (including a single test) and lint
   - the architecture: main directories/packages and how they fit together
   - conventions that are not obvious from reading one file (error handling, naming, test framework, commit style)
   - gotchas and things to avoid
3. Keep it short (well under 150 lines), use Markdown headings and bullets, and do not list every file or pad it with generic advice.
4. Finish with a two-line summary of what you wrote.`

func (r *REPL) initProject() {
	state := "There is no ASG.md yet; create it."
	if data, err := os.ReadFile("ASG.md"); err == nil {
		state = "ASG.md already exists. Read it first and improve it: fix anything outdated and fill gaps, keeping what is still right.\n\nCurrent ASG.md:\n" + string(data)
	}
	r.printf("  %s✦ init%s %swriting ASG.md for this project%s\n", magenta, reset, slate, reset)
	r.send(fmt.Sprintf(initPrompt, state))
}
