package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

const ownAnswer = "\x00own"

// AskUser pauses a running turn to put the model's question to the user
// (agent.Asker): the question is printed into the scrollback, then a Huh
// form offers the model's choices plus "something else", or a text input.
func (r *REPL) AskUser(ctx context.Context, question string, choices []string) (string, error) {
	r.spin.Stop()
	r.md.Flush()
	r.println("\n" + askBox.Render(magentaBold("? ASG NEEDS YOUR INPUT")+"\n"+strings.TrimSpace(question)))

	pick, typed := ownAnswer, ""
	var groups []*huh.Group
	if len(choices) > 0 {
		opts := make([]huh.Option[string], 0, len(choices)+1)
		for _, c := range choices {
			opts = append(opts, huh.NewOption(c, c))
		}
		opts = append(opts, huh.NewOption("✎ something else…", ownAnswer))
		pick = choices[0]
		groups = append(groups, huh.NewGroup(
			huh.NewSelect[string]().Title("Your answer").Options(opts...).Value(&pick),
		))
	}
	groups = append(groups, huh.NewGroup(
		huh.NewInput().Title("Your answer").Placeholder("type your answer").Value(&typed),
	).WithHideFunc(func() bool { return pick != ownAnswer }))

	if !r.runForm(huh.NewForm(groups...)) {
		r.interrupt()
		return "", errors.New("the user cancelled")
	}
	ans := pick
	if pick == ownAnswer {
		ans = strings.TrimSpace(typed)
		if n, err := strconv.Atoi(ans); err == nil && n >= 1 && n <= len(choices) {
			ans = choices[n-1]
		}
	}
	r.printf("  %s %s", magenta("↳"), ans)
	r.spin = r.startSpinner("thinking")
	return ans, ctx.Err()
}

// ReviewUpdate draws one self-review pass (agent.ReviewEvents).
func (r *REPL) ReviewUpdate(pass, total int, lens string, ok bool, note string) {
	r.spin.Stop()
	r.md.Flush()
	name, _, _ := strings.Cut(lens, " — ")
	mark, paint := "✓", green
	if !ok {
		mark, paint = "↻", amber
	}
	label := fmt.Sprintf("review %d/%d", pass, total)
	r.printf("  %s %s %s", slate("◇ "+label), slate(fmt.Sprintf("%-28s", clip(name, 28))),
		paint(mark+" "+oneLine(note, max(termWidth()-52, 20))))
	r.md = r.newMDStream()
	r.spin = r.startSpinner("reviewing")
}

func (r *REPL) reviewCommand(args []string) {
	cfg := r.app.Config.Get().Review
	if len(args) == 0 {
		state := fmt.Sprintf("%d passes minimum, up to %d when the last one still finds problems", cfg.Passes, cfg.MaxPasses)
		if cfg.Passes < 0 {
			state = "off"
		}
		r.printf("  self-review: %s\n  %s sets the minimum · %s turns it off", state, lime("/review <n>"), lime("/review off"))
		return
	}
	n := -1
	if a := strings.ToLower(args[0]); a != "off" && a != "0" {
		v, err := strconv.Atoi(a)
		if err != nil || v < 1 || v > 12 {
			r.printf("%s", yellow("/review takes a number from 1 to 12, or off"))
			return
		}
		n = v
	}
	r.update(func(c *config.Config) error {
		c.Review.Passes, c.Review.MaxPasses = n, 0 // max is re-derived from passes
		return nil
	})
	if n < 0 {
		r.printf("  %s", slate("self-review off"))
		return
	}
	r.printf("  %s", slate(fmt.Sprintf("self-review: %d passes after every answer", n)))
}

func (r *REPL) workCommand() {
	w := r.sess.Work
	if !w.Exists() {
		r.printf("  %s", slate("no work folder yet — build mode and /loop create one under "+agent.WorkRoot+"/ on the first request"))
		return
	}
	out := []string{"  " + badge(colLime, "WORK") + " " + w.Dir}
	entries, _ := os.ReadDir(w.Dir)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, fmt.Sprintf("    %s %s", lime(fmt.Sprintf("%-18s", e.Name())), slate(fmt.Sprintf("%6d bytes · %s", info.Size(), info.ModTime().Format("15:04:05")))))
	}
	if cl, err := os.ReadFile(filepath.Join(w.Dir, "checklist.md")); err == nil {
		out = append(out, "", preview(string(cl), 14))
	}
	r.println(strings.Join(out, "\n"))
}
