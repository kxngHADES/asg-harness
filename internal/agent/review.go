package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/llm"
)

// Self-review: after the model answers, it goes back over its own work at
// least Review.Passes times (4 by default), each pass from a different
// angle (a "lens") so it is not the same question asked four times. A pass
// either confirms ("REVIEW OK: …") or fixes the problem and gives the
// corrected answer. When the last required pass still found something,
// more passes run, up to Review.MaxPasses.

// ReviewEvents is implemented by front ends that draw review progress.
type ReviewEvents interface {
	ReviewUpdate(pass, total int, lens string, ok bool, note string)
}

func reviewReport(ev Events, pass, total int, lens string, ok bool, note string) {
	if re, ok2 := ev.(ReviewEvents); ok2 {
		re.ReviewUpdate(pass, total, lens, ok, note)
		return
	}
	verdict := "fixed: " + note
	if ok {
		verdict = "ok"
	}
	ev.Notice(fmt.Sprintf("review %d/%d (%s) — %s", pass, total, lensName(lens), verdict))
}

// lensName is the short label before the " — " in a lens.
func lensName(lens string) string {
	name, _, _ := strings.Cut(lens, " — ")
	return name
}

var reReviewOK = regexp.MustCompile(`(?i)^[\s*_#>` + "`" + `]*review\s*(ok|passed|pass)\b[\s*_:\-–—]*(.*)`)

// reviewOK reports whether a pass found nothing, and its reason.
func reviewOK(reply string) (bool, string) {
	for _, line := range strings.Split(strings.TrimSpace(reply), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := reReviewOK.FindStringSubmatch(line); m != nil {
			return true, strings.Trim(strings.TrimSpace(m[2]), "*_")
		}
		return false, ""
	}
	return true, "" // an empty reply changes nothing
}

const reviewRules = `
## Self-review
You are re-checking your own work. Look only for real problems of the kind named in the review request.
Check against the actual files and tool results, not your memory — use tools to confirm when unsure.
If you find a real problem: fix it (with tools if it is in files), then give your corrected answer IN FULL.
If there is nothing to fix, reply with exactly one line: REVIEW OK: <what you checked>.
Never invent problems to look busy.`

type reviewResult struct {
	answer  string
	changed bool
	notes   []string // one line per pass
}

// selfReview runs review passes on the session's latest answer. The passes
// run in the same conversation, so the model sees what it did; afterwards
// the review exchange is folded out of the history so later turns stay
// short. preamble, when set, opens the first pass (used by the loop
// harness, whose reviewer has no conversation to look back on).
func (s *Session) selfReview(ctx context.Context, mode Mode, preamble, answer string, ev Events) reviewResult {
	cfg := s.app.Config.Get().Review
	res := reviewResult{answer: answer}
	if cfg.Passes <= 0 || strings.TrimSpace(answer) == "" && preamble == "" {
		return res
	}
	base := len(s.History)
	lenses := s.Track.Lenses(cfg.MaxPasses)
	rounds := max(s.app.Config.Get().MaxToolRounds/2, 4)
	lastOK := true
	for i := 0; i < cfg.MaxPasses; i++ {
		if i >= cfg.Passes && lastOK {
			break
		}
		total := cfg.Passes
		if i >= cfg.Passes {
			total = cfg.MaxPasses
		}
		var b strings.Builder
		if i == 0 && preamble != "" {
			b.WriteString(preamble + "\n\n")
		}
		fmt.Fprintf(&b, "[Self-review pass %d of at least %d]\nLens: %s\n\n", i+1, cfg.Passes, lenses[i])
		b.WriteString("Go back over your previous answer and the work behind it from this angle only. Fix real problems and give the corrected answer in full, or reply with one line: REVIEW OK: <what you checked>.")
		reply, err := s.send(ctx, llm.Message{Content: b.String()}, quiet{ev}, turnOpts{mode: mode, extra: reviewRules, maxRounds: rounds})
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			reviewReport(ev, i+1, total, lenses[i], true, "skipped: "+err.Error())
			res.notes = append(res.notes, fmt.Sprintf("pass %d (%s): skipped — %v", i+1, lensName(lenses[i]), err))
			continue
		}
		ok, why := reviewOK(reply)
		lastOK = ok
		if ok {
			if why == "" {
				why = "nothing to fix"
			}
			reviewReport(ev, i+1, total, lenses[i], true, why)
			res.notes = append(res.notes, fmt.Sprintf("pass %d (%s): ok — %s", i+1, lensName(lenses[i]), oneLineClip(why, 160)))
			continue
		}
		res.answer, res.changed = reply, true
		reviewReport(ev, i+1, total, lenses[i], false, oneLineClip(reply, 140))
		res.notes = append(res.notes, fmt.Sprintf("pass %d (%s): fixed — %s", i+1, lensName(lenses[i]), oneLineClip(reply, 160)))
	}

	// Fold the review out of the history: keep the turn as it was, with
	// the final answer in place of the first one.
	if len(s.History) > base {
		s.History = s.History[:base]
	}
	if res.changed && base > 0 && s.History[base-1].Role == llm.RoleAssistant && len(s.History[base-1].ToolCalls) == 0 {
		s.History[base-1].Content = res.answer + "\n\n(Revised during self-review.)"
	}
	if res.changed {
		ev.Text("\n" + res.answer + "\n")
	}
	return res
}
