// Command asg is an agentic terminal shell for Ollama, Anthropic and
// OpenAI-compatible models, with a browser dashboard.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/dashboard"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/media"
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

	switch {
	case len(args) > 0 && args[0] == "dashboard":
		fatal(dashboard.Serve(app, !*noOpen))
	case len(args) > 0 && args[0] == "skill":
		skillCmd(app, args[1:])
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

// printEvents streams a one-shot run to stdout; tools needing approval are
// denied unless -yolo is set.
type printEvents struct{}

func (printEvents) Text(d string) { fmt.Print(d) }
func (printEvents) ToolStart(c llm.ToolCall, s string) {
	fmt.Fprintf(os.Stderr, "\n⏺ %s(%s)\n", c.Name, s)
}
func (printEvents) ToolEnd(llm.ToolCall, string, bool) {}
func (printEvents) Notice(m string)                    { fmt.Fprintln(os.Stderr, "!", m) }
func (printEvents) Approve(context.Context, llm.ToolCall, agent.Gate) agent.Decision {
	fmt.Fprintln(os.Stderr, "! tool denied (run with -yolo to allow tools in -p mode)")
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

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "asg:", err)
	os.Exit(1)
}
