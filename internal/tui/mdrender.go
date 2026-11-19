package tui

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

// Replies are rendered with Glamour using an ASG style: the stock dark or
// light style with the signal-console palette for headings, code and links,
// and no document margin (the gutter provides the indent).

func sp(s string) *string { return &s }
func up(u uint) *uint     { return &u }

func asgStyle() gansi.StyleConfig {
	st := styles.DarkStyleConfig
	text := "252"
	if !darkBackground {
		st = styles.LightStyleConfig
		text = "235"
	}
	lime, cyan, magenta, slate := "#AFFF00", "#00D7FF", "#FF5FFF", "#8A8A8A"
	if !darkBackground {
		lime, cyan, magenta = "#5F8700", "#0087AF", "#AF00AF"
	}
	st.Document.Margin = up(0)
	st.Document.BlockPrefix = ""
	st.Document.BlockSuffix = ""
	st.Document.Color = sp(text)
	st.Heading.Color = sp(lime)
	st.H1.Color = sp("#000000")
	st.H1.BackgroundColor = sp(lime)
	for _, h := range []*gansi.StyleBlock{&st.H2, &st.H3, &st.H4, &st.H5, &st.H6} {
		h.Color = sp(lime)
	}
	st.Code.Color = sp(cyan)
	st.Code.BackgroundColor = nil
	st.Code.Prefix, st.Code.Suffix = "", "" // the stock padding is for a background we don't use
	st.CodeBlock.Margin = up(1)
	st.Link.Color = sp(cyan)
	st.LinkText.Color = sp(cyan)
	st.Strong.Color = nil
	st.Emph.Color = nil
	st.BlockQuote.Color = sp(slate)
	st.BlockQuote.IndentToken = sp("▏ ")
	st.Item.BlockPrefix = "▪ "
	st.Enumeration.Color = sp(lime)
	st.Task.Ticked = "✓ "
	st.Task.Unticked = "□ "
	st.HorizontalRule.Color = sp(slate)
	st.Image.Color = sp(magenta)
	return st
}

var md struct {
	sync.Mutex
	width int
	tr    *glamour.TermRenderer
}

// renderMarkdown renders src to fit width columns. If Glamour fails the
// source is returned as is — a reply is never lost to a renderer error.
func renderMarkdown(src string, width int) string {
	width = max(width, 20)
	md.Lock()
	defer md.Unlock()
	if md.tr == nil || md.width != width {
		tr, err := glamour.NewTermRenderer(glamour.WithStyles(asgStyle()), glamour.WithWordWrap(width), glamour.WithEmoji())
		if err != nil {
			return src
		}
		md.tr, md.width = tr, width
	}
	out, err := md.tr.Render(src)
	if err != nil {
		return src
	}
	return trimBlank(out, width)
}

// trailingPad matches spaces at the end of a line, even when Glamour has
// wrapped them in styling codes.
var trailingPad = regexp.MustCompile(`(?:[ \t]|\x1b\[[0-9;]*m)+$`)

// trimBlank drops leading and trailing blank lines and the padding Glamour
// adds to fill the wrap width, and clips any line wider than width (code
// blocks are not wrapped), so the gutter never ends up on a wrapped row.
func trimBlank(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = trailingPad.ReplaceAllString(l, "") + "\x1b[0m"
		if ansi.StringWidth(l) > width {
			l = ansi.Truncate(l, width, "…")
		}
		lines[i] = l
	}
	for len(lines) > 0 && strings.TrimSpace(stripANSI(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(stripANSI(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// splitBlock cuts the first finished Markdown block off s: text up to a
// blank line outside a code fence, or up to and including a closing fence.
// It only looks at complete lines, so a block is never cut mid-sentence.
func splitBlock(s string) (block, rest string, ok bool) {
	inFence := false
	pos := 0
	for {
		i := strings.IndexByte(s[pos:], '\n')
		if i < 0 {
			return "", s, false
		}
		line := strings.TrimSpace(s[pos : pos+i])
		next := pos + i + 1
		switch {
		case strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~"):
			if inFence {
				return s[:next], s[next:], true
			}
			inFence = true
		case !inFence && line == "" && strings.TrimSpace(s[:pos]) != "":
			return s[:pos], s[next:], true
		}
		pos = next
	}
}

// mdStream turns streamed reply text into rendered output. Finished blocks
// are rendered once and printed into the scrollback; only the block still
// being written is re-rendered, in the live area above the input.
type mdStream struct {
	r       *REPL
	mu      sync.Mutex
	pending string
	blocks  int
	last    time.Time
	timer   *time.Timer
}

func (r *REPL) newMDStream() *mdStream { return &mdStream{r: r} }

const liveEvery = 60 * time.Millisecond

func (m *mdStream) Write(delta string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending += delta
	for {
		blk, rest, ok := splitBlock(m.pending)
		if !ok {
			break
		}
		m.commit(blk)
		m.pending = rest
	}
	m.showLive()
}

func (m *mdStream) width() int { return termWidth() - 4 }

func (m *mdStream) commit(blk string) {
	if strings.TrimSpace(blk) == "" {
		return
	}
	out := gutterLines(renderMarkdown(blk, m.width()))
	if m.blocks > 0 {
		out = gutter + "\n" + out
	}
	m.blocks++
	m.r.println(out)
}

// showLive re-renders the unfinished block, at most every liveEvery; a
// trailing timer makes sure the last change is shown.
func (m *mdStream) showLive() {
	if time.Since(m.last) < liveEvery {
		if m.timer == nil {
			m.timer = time.AfterFunc(liveEvery, func() {
				m.mu.Lock()
				defer m.mu.Unlock()
				m.timer = nil
				m.showLive()
			})
		}
		return
	}
	m.last = time.Now()
	text := ""
	if strings.TrimSpace(m.pending) != "" {
		text = gutterLines(renderMarkdown(m.pending, m.width()))
	}
	m.r.post(liveMsg{text: text})
}

// Flush prints whatever is left and clears the live area.
func (m *mdStream) Flush() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.commit(m.pending)
	m.pending = ""
	m.r.post(liveMsg{})
}
