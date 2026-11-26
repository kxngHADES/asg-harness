package tui

import (
	"context"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/runtimes"
	itools "github.com/kxngHADES/asg-harness/internal/tools"
)

const toolUsage = "usage: /tool new <name> [python|go]  ·  /tool add <folder|github:owner/repo[/path]>  ·  /tool setup <name>"

// toolLang reads the optional language argument of /tool new.
func toolLang(args []string) string {
	if len(args) == 0 {
		return itools.LangPython
	}
	return strings.TrimLeft(strings.ToLower(args[0]), "-")
}

// toolCommand creates, imports and prepares script tools. It runs on the
// worker goroutine, so the slow first-time setup can block here.
func (r *REPL) toolCommand(ctx context.Context, args []string) {
	if len(args) < 2 {
		r.printf("%s", slate(toolUsage))
		return
	}
	dest := agent.ToolDirs()[1]
	// Runtime downloads and dependency installs report progress here.
	ctx = itools.WithProgress(ctx, func(m string) { r.printf("  %s", slate(m)) })
	switch args[0] {
	case "new", "create":
		dir, err := itools.ScaffoldTool(dest, args[1], toolLang(args[2:]))
		if err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
		r.app.ReloadTools()
		r.printf("  %s — edit tool.json and the code, then /tools to reload", green("✓ created "+dir))
	case "add", "import", "install":
		sp := r.startSpinner("importing " + args[1])
		got, err := itools.ImportTools(ctx, args[1], dest)
		sp.Stop()
		if err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
		r.app.ReloadTools()
		for _, t := range got {
			r.printf("%s %s  %s", green("✓"), cyan(t.Name), slate(langLabel(t.Lang)+" · "+t.Dir))
			r.prepareTool(ctx, t.Name, t.Dir)
		}
	case "setup", "prepare":
		t := r.app.Tools.Get(args[1])
		if t == nil || t.Source == "builtin" || t.Source == "bundled" {
			r.printf("%s", red("no script tool "+args[1]))
			return
		}
		r.prepareTool(ctx, args[1], t.Source)
	default:
		r.printf("%s", slate(toolUsage))
	}
}

// prepareTool installs a tool's runtime and libraries now, so its first
// call from the model is quick.
func (r *REPL) prepareTool(ctx context.Context, name, dir string) {
	if itools.ScriptToolLang(dir) == "" {
		return
	}
	sp := r.startSpinner("setting up " + name)
	err := itools.PrepareScriptTool(ctx, dir)
	sp.Stop()
	if err != nil {
		r.printf("  %s", amber("could not set up "+name+" yet (it retries on first run): "+oneLine(err.Error(), termWidth()-40)))
		return
	}
	r.printf("  %s", green("✓ "+name+" is ready"))
}

func langLabel(lang string) string {
	switch lang {
	case itools.LangPython:
		return "python"
	case itools.LangGo:
		return "go"
	}
	return "command"
}

// runtimeCommand shows the managed runtime, or installs it (/runtime install [go]).
func (r *REPL) runtimeCommand(ctx context.Context, args []string) {
	rt := runtimes.Default()
	if len(args) > 0 && args[0] == "install" {
		say := func(m string) { r.printf("  %s", slate(m)) }
		sp := r.startSpinner("installing uv and Python " + runtimes.PythonVersion)
		err := rt.EnsurePython(ctx, say)
		if err == nil && len(args) > 1 && strings.TrimLeft(args[1], "-") == "go" {
			_, err = rt.Go(ctx, say)
		}
		sp.Stop()
		if err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
	}
	st := rt.Status(ctx)
	val := func(s, missing string) string {
		if s == "" {
			return amber(missing)
		}
		return s
	}
	goLine := val(st.Go, "not installed — ASG installs it the first time a Go tool runs")
	if st.Go != "" && !st.GoManaged {
		goLine += slate("  (from PATH)")
	}
	r.println(strings.Join([]string{
		"  " + cyan("uv        ") + val(st.UV, "not installed — /runtime install"),
		"  " + cyan("python   ") + " " + val(st.Python, "not installed — /runtime install"),
		"  " + cyan("go       ") + " " + goLine,
		"  " + slate("runtime folder: "+st.Home),
	}, "\n"))
}
