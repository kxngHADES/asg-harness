// Command asg is an agentic terminal shell for Ollama, Anthropic and
// OpenAI-compatible models, with a browser dashboard.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"charm.land/log/v2"
	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/dashboard"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/media"
	"github.com/kxngHADES/asg-harness/internal/runtimes"
	"github.com/kxngHADES/asg-harness/internal/tools"
	"github.com/kxngHADES/asg-harness/internal/tui"
	"github.com/kxngHADES/asg-harness/internal/update"
)

// version is set at release time by GoReleaser (-ldflags -X main.version=…).
var version = "dev"

const usage = `asg — signal console for Ollama, Anthropic and OpenAI models

Usage:
  asg                        interactive terminal agent
  asg -p "prompt" [img...]   run one prompt and print the answer ("-" reads stdin)
  asg -loop -yolo -p "goal"  run an objective with the loop harness, unattended
  asg dashboard              web version: run the browser dashboard
  asg skill add <source>     import skills (path, URL, github:owner/repo[/path])
  asg skill list|rm <name>
  asg tool new <name> [--go] create a Python (or Go) tool in ~/.asg/tools
  asg tool add <source>      import tools (folder, github:owner/repo[/path], GitHub URL)
  asg tool setup <name>      install a tool's runtime and libraries now
  asg tool list              list the tools the model can call
  asg runtime [install [--go]]  show or install the managed uv / Python 3.12 / Go
  asg update                 update to the latest release
  asg version

Flags:
`

func main() {
	provider := flag.String("provider", "", "provider to use for this run (see /provider)")
	model := flag.String("model", "", "model to use for this run")
	prompt := flag.String("p", "", "run a single prompt non-interactively")
	yolo := flag.Bool("yolo", false, "auto-approve all tool calls for this run")
	mode := flag.String("mode", "build", "starting mode: build, plan, ask or loop")
	domain := flag.String("domain", "", "work domain and flavor, e.g. software/work, docs, cyber/blue, devops, pm/sprint")
	reviews := flag.Int("review", 0, "self-review passes after every answer (default from config, 4; -1 turns it off)")
	loop := flag.Bool("loop", false, "with -p: treat the prompt as an objective for the loop harness")
	verify := flag.String("verify", "", "with -loop: command that proves the work (\"none\" to skip; default auto-detect)")
	noOpen := flag.Bool("no-open", false, "with 'dashboard': don't open a browser")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage); flag.PrintDefaults() }
	flag.Parse()

	args := flag.Args()
	if *showVersion || (len(args) > 0 && args[0] == "version") {
		fmt.Println("asg", version)
		return
	}
	if len(args) > 0 && args[0] == "update" {
		fmt.Println("checking for updates…")
		tag, err := update.Run(context.Background(), version)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("asg is at %s\n", tag)
		return
	}

	store, err := config.Load()
	if err != nil {
		fatal(err)
	}
	app := agent.NewApp(store)

	// Per-run overrides are applied to the saved config so the dashboard
	// and terminal agree on what is active.
	if *provider != "" || *model != "" {
		err := store.Update(func(c *config.Config) error {
			if *provider != "" {
				if _, ok := c.Providers[*provider]; !ok {
					return fmt.Errorf("unknown provider %q", *provider)
				}
				c.ActiveProvider = *provider
			}
			if *model != "" {
				c.Providers[c.ActiveProvider].Model = *model
			}
			return nil
		})
		if err != nil {
			fatal(err)
		}
	}
	app.Yolo = *yolo
	startMode, ok := agent.ParseMode(*mode)
	if !ok {
		fatal(fmt.Errorf("unknown -mode %q (build, plan, ask or loop)", *mode))
	}
	if *loop {
		startMode = agent.ModeLoop
	}
	if *domain != "" || *reviews != 0 {
		name, flavor, _ := strings.Cut(*domain, "/")
		err := store.Update(func(c *config.Config) error {
			if *domain != "" {
				d, ok := agent.ParseDomain(name)
				if !ok {
					return fmt.Errorf("unknown -domain %q (software, docs, cyber, devops or pm)", name)
				}
				t := agent.Track{Domain: d}
				if flavor != "" {
					if t, ok = t.WithFlavor(flavor); !ok {
						return fmt.Errorf("%s has no flavor %q", name, flavor)
					}
				}
				c.Domain, c.Flavor = string(d), t.Flavor
			}
			if *reviews != 0 {
				c.Review.Passes, c.Review.MaxPasses = *reviews, 0
			}
			return nil
		})
		if err != nil {
			fatal(err)
		}
	}

	switch {
	case len(args) > 0 && args[0] == "dashboard":
		fatal(dashboard.Serve(app, !*noOpen))
	case len(args) > 0 && args[0] == "skill":
		skillCmd(app, args[1:])
	case len(args) > 0 && args[0] == "tool":
		toolCmd(app, args[1:])
	case len(args) > 0 && args[0] == "runtime":
		runtimeCmd(args[1:])
	case *prompt != "":
		oneShot(app, *prompt, args, startMode, *verify)
	default:
		r := tui.New(app)
		r.SetMode(startMode)
		if err := r.Run(); err != nil {
			fatal(err)
		}
	}
}

func skillCmd(app *agent.App, args []string) {
	if len(args) == 0 || args[0] == "list" {
		for _, s := range app.Skills.List() {
			fmt.Printf("%-24s %s\n", s.Name, s.Description)
		}
		return
	}
	if len(args) < 2 {
		fatal(fmt.Errorf("usage: asg skill add <source> | rm <name>"))
	}
	switch args[0] {
	case "add", "import", "install":
		got, err := app.Skills.Import(context.Background(), args[1])
		if err != nil {
			fatal(err)
		}
		for _, s := range got {
			fmt.Printf("installed %s → %s\n", s.Name, s.Dir)
		}
	case "rm", "remove":
		if err := app.Skills.Remove(args[1]); err != nil {
			fatal(err)
		}
		fmt.Println("removed", args[1])
	default:
		fatal(fmt.Errorf("unknown skill command %q", args[0]))
	}
}

func toolCmd(app *agent.App, args []string) {
	if len(args) == 0 || args[0] == "list" {
		for _, t := range app.Tools.All() {
			fmt.Printf("%-18s %-8s %s\n", t.Spec.Name, sourceLabel(t.Source), t.Spec.Description)
		}
		fmt.Printf("\nscript tool folders: %s\n", strings.Join(agent.ToolDirs(), ", "))
		return
	}
	const toolUsage = "usage: asg tool new <name> [--python|--go] | add <source> | setup <name> | list"
	if len(args) < 2 {
		fatal(errors.New(toolUsage))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx = tools.WithProgress(ctx, func(m string) { logger.Info(m) })
	switch args[0] {
	case "new", "create":
		lang := tools.LangPython
		for _, a := range args[2:] {
			lang = strings.TrimLeft(a, "-")
		}
		dir, err := tools.ScaffoldTool(agent.ToolDirs()[1], args[1], lang)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("created %s — edit tool.json and the code (see tools/README.md)\n", dir)
	case "add", "import", "install":
		got, err := tools.ImportTools(ctx, args[1], agent.ToolDirs()[1])
		if err != nil {
			fatal(err)
		}
		for _, t := range got {
			fmt.Printf("installed %s → %s\n", t.Name, t.Dir)
			// Install the tool's runtime and libraries now, so its first
			// call doesn't have to.
			if err := tools.PrepareScriptTool(ctx, t.Dir); err != nil {
				logger.Warn("could not set up "+t.Name+" yet; it will retry on first run", "err", err)
			}
		}
	case "setup", "prepare":
		t := app.Tools.Get(args[1])
		if t == nil || t.Source == "builtin" || t.Source == "bundled" {
			fatal(fmt.Errorf("no script tool %q", args[1]))
		}
		if err := tools.PrepareScriptTool(ctx, t.Source); err != nil {
			fatal(err)
		}
		fmt.Println(args[1], "is ready")
	default:
		fatal(errors.New(toolUsage))
	}
}

// runtimeCmd shows or installs the managed runtime script tools run on.
func runtimeCmd(args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rt := runtimes.Default()
	if len(args) > 0 && args[0] == "install" {
		say := func(m string) { logger.Info(m) }
		if err := rt.EnsurePython(ctx, say); err != nil {
			fatal(err)
		}
		if len(args) > 1 && strings.TrimLeft(args[1], "-") == "go" {
			if _, err := rt.Go(ctx, say); err != nil {
				fatal(err)
			}
		}
	} else if len(args) > 0 {
		fatal(errors.New("usage: asg runtime [install [--go]]"))
	}
	st := rt.Status(ctx)
	orNone := func(s string) string {
		if s == "" {
			return "not installed"
		}
		return s
	}
	fmt.Printf("runtime folder  %s\n", st.Home)
	fmt.Printf("uv              %s\n", orNone(st.UV))
	fmt.Printf("python %s     %s\n", runtimes.PythonVersion, orNone(st.Python))
	goLine := orNone(st.Go)
	if st.Go != "" && !st.GoManaged {
		goLine += " (from PATH)"
	} else if st.Go == "" {
		goLine += " (installed on first use of a Go tool)"
	}
	fmt.Printf("go              %s\n", goLine)
}

func sourceLabel(src string) string {
	if src == "builtin" || src == "bundled" {
		return src
	}
	return "script"
}

// printEvents streams a one-shot run to stdout; tools needing approval are
// denied unless -yolo is set.
type printEvents struct{}

// AskUser lets ask_user reach a person when -p runs in a terminal.
func (printEvents) AskUser(ctx context.Context, q string, choices []string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return agent.NoUserAnswer, nil
	}
	fmt.Fprintf(os.Stderr, "\n? %s\n", q)
	for i, c := range choices {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, c)
	}
	fmt.Fprint(os.Stderr, "> ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	line = strings.TrimSpace(line)
	if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(choices) {
		line = choices[n-1]
	}
	return line, ctx.Err()
}

func (printEvents) Text(d string) { fmt.Print(d) }
func (printEvents) ToolStart(c llm.ToolCall, s string) {
	logger.Info("tool", "name", c.Name, "args", s)
}
func (printEvents) ToolEnd(llm.ToolCall, string, bool) {}
func (printEvents) Notice(m string)                    { logger.Warn(m) }
func (printEvents) Approve(context.Context, llm.ToolCall, agent.Gate) agent.Decision {
	logger.Warn("tool denied (run with -yolo to allow tools in -p mode)")
	return agent.Deny
}
func (printEvents) LoopUpdate(st *agent.LoopState, msg string) {
	fmt.Fprintf(os.Stderr, "\n⟳ %s\n", msg)
	if strings.HasPrefix(msg, "plan ready") || st.Status != agent.LoopRunning {
		fmt.Fprint(os.Stderr, st.Checklist())
	}
}

func oneShot(app *agent.App, prompt string, imgs []string, mode agent.Mode, verify string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	msg := llm.Message{Content: prompt}
	for _, p := range imgs {
		img, err := media.LoadImage(p)
		if err != nil {
			fatal(err)
		}
		msg.Images = append(msg.Images, img)
	}
	if strings.TrimSpace(prompt) == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal(err)
		}
		msg.Content = string(data)
	}
	sess := app.NewSession()
	sess.Mode = mode
	if mode == agent.ModeLoop {
		st, err := sess.RunLoop(ctx, msg.Content, agent.LoopOptions{Verify: verify}, printEvents{})
		if err != nil {
			fatal(err)
		}
		fmt.Println("\n" + agent.LoopSummary(st))
		if st.Status != agent.LoopDone {
			os.Exit(2)
		}
		return
	}
	if _, err := sess.Send(ctx, msg, printEvents{}); err != nil {
		fatal(err)
	}
	fmt.Println()
}

// logger writes diagnostics to stderr so answers on stdout stay pipeable.
var logger = log.NewWithOptions(os.Stderr, log.Options{Prefix: "asg"})

func fatal(err error) {
	if err == nil {
		return
	}
	logger.Error(err)
	os.Exit(1)
}
