package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

// AskUser pauses a running turn to put the model's question to the user
// (agent.Asker). Choices are numbered; the user can pick one by number or
// type any answer.
func (r *REPL) AskUser(ctx context.Context, question string, choices []string) (string, error) {
	r.spin.Stop()
	r.md.Flush()
	bar := strings.Repeat("─", max(min(termWidth()-24, 48), 8))
	r.printf("\n  %s╭─%s %s%s ? ASG NEEDS YOUR INPUT %s %s%s%s\n", magenta, reset, bold, onMagenta, reset, magenta, bar, reset)
	for _, l := range strings.Split(strings.TrimSpace(question), "\n") {
		r.printf("  %s│%s %s\n", magenta, reset, l)
	}
	for i, c := range choices {
		r.printf("  %s│%s   %s%d%s %s\n", magenta, reset, lime, i+1, reset, c)
	}
	r.printf("  %s╰─%s%s\n", magenta, bar, reset)

	old, err := term.MakeRaw(r.fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(r.fd, old)
	r.gating = true
	defer func() { r.gating = false }()
	hint := "type your answer"
	if len(choices) > 0 {
		hint = fmt.Sprintf("1–%d or type your own", len(choices))
	}
	r.t.SetPrompt(fmt.Sprintf("  %s%s%s %s▸%s ", slate, hint, reset, magenta, reset))
	ans, err := r.t.ReadLine()
	if err != nil {
		r.mu.Lock()
		if r.cancel != nil {
			r.cancel()
		}
		r.mu.Unlock()
		return "", errors.New("the user cancelled")
	}
	ans = strings.TrimSpace(ans)
	if n, err := strconv.Atoi(ans); err == nil && n >= 1 && n <= len(choices) {
		ans = choices[n-1]
	}
	r.spin = startSpinner("thinking")
	return ans, ctx.Err()
}

// ReviewUpdate draws one self-review pass (agent.ReviewEvents).
func (r *REPL) ReviewUpdate(pass, total int, lens string, ok bool, note string) {
	r.spin.Stop()
	r.md.Flush()
	name, _, _ := strings.Cut(lens, " — ")
	mark, color := "✓", green
	if !ok {
		mark, color = "↻", amber
	}
	label := fmt.Sprintf("review %d/%d", pass, total)
	r.printf("  %s◇ %s%s %s%-28s%s %s%s %s%s\n", slate, label, reset, slate, clip(name, 28), reset,
		color, mark, oneLine(note, max(termWidth()-52, 20)), reset)
	r.md = newMDStream()
	r.spin = startSpinner("reviewing")
}

func (r *REPL) reviewCommand(args []string) {
	cfg := r.app.Config.Get().Review
	if len(args) == 0 {
		state := fmt.Sprintf("%d passes minimum, up to %d when the last one still finds problems", cfg.Passes, cfg.MaxPasses)
		if cfg.Passes < 0 {
			state = "off"
		}
		r.printf("  self-review: %s\n  %s/review <n>%s sets the minimum · %s/review off%s turns it off\n", state, lime, reset, lime, reset)
		return
	}
	n := -1
	if a := strings.ToLower(args[0]); a != "off" && a != "0" {
		v, err := strconv.Atoi(a)
		if err != nil || v < 1 || v > 12 {
			r.printf("%s/review takes a number from 1 to 12, or off%s\n", yellow, reset)
			return
		}
		n = v
	}
	r.update(func(c *config.Config) error {
		c.Review.Passes, c.Review.MaxPasses = n, 0 // max is re-derived from passes
		return nil
	})
	if n < 0 {
		r.printf("  %sself-review off%s\n", slate, reset)
		return
	}
	r.printf("  %sself-review: %d passes after every answer%s\n", slate, n, reset)
}

func (r *REPL) workCommand() {
	w := r.sess.Work
	if !w.Exists() {
		r.printf("  %sno work folder yet — build mode and /loop create one under %s/ on the first request%s\n", slate, agent.WorkRoot, reset)
		return
	}
	r.printf("  %s%s WORK %s %s\n", bold, onLime, reset, w.Dir)
	entries, _ := os.ReadDir(w.Dir)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		r.printf("    %s%-18s%s %s%6d bytes · %s%s\n", lime, e.Name(), reset, slate, info.Size(), info.ModTime().Format("15:04:05"), reset)
	}
	if cl, err := os.ReadFile(filepath.Join(w.Dir, "checklist.md")); err == nil {
		r.printf("\n%s\n", preview(string(cl), 14))
	}
}
