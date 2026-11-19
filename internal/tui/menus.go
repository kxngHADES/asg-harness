package tui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/memory"
	"github.com/kxngHADES/asg-harness/internal/perms"
)

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
	it := r.choose("Provider", items, sel)
	if it == nil {
		r.printf("  %s", slate("provider unchanged ("+cfg.ActiveProvider+")"))
		return
	}
	if it.Value == "\x00dashboard" {
		r.command("/dashboard")
		return
	}
	name := it.Value
	if pc := cfg.Providers[name]; pc.Type != "ollama" && pc.APIKey == "" {
		if key := r.readSecret(name + " needs an API key — paste it"); key != "" {
			r.update(func(c *config.Config) error { c.Providers[name].APIKey = key; return nil })
			r.printf("  %s", green("✓ key saved to "+r.app.Config.Path()))
		}
	}
	r.update(func(c *config.Config) error { c.ActiveProvider = name; return nil })
	r.printf("  %s", slate("provider → "+name))
	r.pickModel(ctx)
}

func (r *REPL) pickModel(ctx context.Context) {
	p, cur, err := r.app.Provider()
	cfg := r.app.Config.Get()
	name, _ := cfg.Active()
	var models []string
	if err == nil {
		lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		sp := r.startSpinner("loading " + name + " models")
		models, err = p.ListModels(lctx)
		sp.Stop()
		cancel()
	}
	if err != nil || len(models) == 0 {
		if err == nil {
			err = fmt.Errorf("no models found")
		}
		r.printf("  %s\n  %s", yellow(fmt.Sprintf("couldn't list models: %v", err)), slate("model stays "+cur+" — set one with /model <name>"))
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
	it := r.choose("Model · "+name, items, sel)
	if it == nil {
		r.printf("  %s", slate("model stays "+cur))
		return
	}
	r.update(func(c *config.Config) error { c.Providers[c.ActiveProvider].Model = it.Value; return nil })
	r.printf("  %s%s", slate("model → "), bold(it.Value))
}

func (r *REPL) memoryMenu() {
	notes := r.app.Memory.List()
	if len(notes) == 0 {
		r.printf("  %s\n  %s",
			slate(`no memories yet. Tell ASG a lasting preference ("always use pytest") or run /memory <note>.`),
			dim("stored in "+r.app.Memory.Dir(memory.User)+" and "+r.app.Memory.Dir(memory.Project)))
		return
	}
	var items []menuItem
	for i, n := range notes {
		items = append(items, menuItem{Label: n.Name, Detail: n.Summary, Hint: n.Scope, Value: fmt.Sprint(i)})
	}
	it := r.choose("Memory", items, 0)
	if it == nil {
		return
	}
	i, _ := strconv.Atoi(it.Value)
	n := notes[i]
	r.printf("\n  %s %s", badge(colLime, n.Name), slate("("+n.Scope+")"))
	r.println(gutterLines(renderMarkdown(n.Body, termWidth()-4)))
	r.printf("  %s\n", dim(n.Path))
	act := r.choose("Keep this memory?", []menuItem{
		{Label: "keep", Value: "keep"},
		{Label: "delete", Detail: "forget this note", Value: "delete"},
	}, 0)
	if act != nil && act.Value == "delete" {
		if err := r.app.Memory.Delete(n.Scope, n.Name); err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
		r.printf("  %s", slate("forgot "+n.Name))
	}
}

func (r *REPL) permissionsMenu() {
	cwd, _ := os.Getwd()
	global, local := r.app.Perms.List(cwd)
	if len(global)+len(local) == 0 {
		r.printf("  %s", slate("nothing remembered yet. Answer a GATE with [a] (this project) or [g] (everywhere)."))
		return
	}
	var items []menuItem
	for _, rule := range local {
		items = append(items, menuItem{Label: ruleLabel(rule), Detail: "this project", Value: rule})
	}
	for _, rule := range global {
		items = append(items, menuItem{Label: ruleLabel(rule), Detail: "everywhere", Value: rule})
	}
	it := r.choose("Always allowed · enter revokes", items, 0)
	if it == nil {
		return
	}
	if err := r.app.Perms.Remove(cwd, it.Value); err != nil {
		r.printf("%s", red(err.Error()))
		return
	}
	r.printf("  %s", slate("revoked "+ruleLabel(it.Value)+" — ASG will ask again"))
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
	r.printf("  %s %s", magenta("✦ init"), slate("writing ASG.md for this project"))
	r.send(fmt.Sprintf(initPrompt, state))
}
