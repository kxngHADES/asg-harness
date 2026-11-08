package tui

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// inputQueue sits between stdin and the line editor so menus can read
// keys themselves and push bytes back (e.g. an Enter to submit a choice).
type inputQueue struct {
	mu  sync.Mutex
	buf []byte
	r   io.Reader
}

func (q *inputQueue) Read(p []byte) (int, error) {
	q.mu.Lock()
	if len(q.buf) > 0 {
		n := copy(p, q.buf)
		q.buf = q.buf[n:]
		q.mu.Unlock()
		return n, nil
	}
	q.mu.Unlock()
	return q.r.Read(p)
}

// unread puts bytes back in front of the queue.
func (q *inputQueue) unread(b []byte) {
	q.mu.Lock()
	q.buf = append(append([]byte(nil), b...), q.buf...)
	q.mu.Unlock()
}

type keyKind int

const (
	kRune keyKind = iota
	kUp
	kDown
	kEnter
	kTab
	kEsc
	kBack
	kCancel
	kOther
)

type key struct {
	kind keyKind
	r    rune
}

func (q *inputQueue) readKey() (key, error) {
	buf := make([]byte, 64)
	n, err := q.Read(buf)
	if err != nil {
		return key{}, err
	}
	k, used := parseKey(buf[:n])
	if used < n {
		q.unread(buf[used:n])
	}
	return k, nil
}

// parseKey decodes the first key in b and reports how many bytes it used.
func parseKey(b []byte) (key, int) {
	switch b[0] {
	case 0x1b:
		if len(b) >= 3 && (b[1] == '[' || b[1] == 'O') {
			switch b[2] {
			case 'A':
				return key{kind: kUp}, 3
			case 'B':
				return key{kind: kDown}, 3
			}
			i := 2 // skip any other CSI sequence up to its final byte
			for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
				i++
			}
			return key{kind: kOther}, min(i+1, len(b))
		}
		return key{kind: kEsc}, 1
	case '\r':
		if len(b) > 1 && b[1] == '\n' {
			return key{kind: kEnter}, 2
		}
		return key{kind: kEnter}, 1
	case '\n':
		return key{kind: kEnter}, 1
	case '\t':
		return key{kind: kTab}, 1
	case 0x7f, 0x08:
		return key{kind: kBack}, 1
	case 0x03, 0x04:
		return key{kind: kCancel}, 1
	case 0x10: // ctrl+p
		return key{kind: kUp}, 1
	case 0x0e: // ctrl+n
		return key{kind: kDown}, 1
	}
	r, size := utf8.DecodeRune(b)
	if r < 0x20 || r == utf8.RuneError {
		return key{kind: kOther}, max(size, 1)
	}
	return key{kind: kRune, r: r}, size
}

type menuItem struct {
	Label  string // main text, e.g. "/model" or a model name
	Detail string // dimmed description
	Hint   string // short right-hand tag, e.g. "no key"
	Mark   bool   // the current choice (●)
	Value  string
}

// picker is a filterable list drawn under the cursor line. In slash mode
// the line above the list is the prompt plus "/filter", so it reads as a
// dropdown attached to what the user is typing.
type picker struct {
	items  []menuItem
	filter string
	sel    int
	top    int
	rows   int
	slash  bool
	title  string // header in standalone mode
	prompt string // prompt text in slash mode
	drawn  bool
}

const (
	pickChose = iota
	pickTyped
	pickCancel
)

func (p *picker) visible() []menuItem {
	f := strings.ToLower(strings.TrimPrefix(p.filter, "/"))
	if f == "" {
		return p.items
	}
	var prefix, sub []menuItem
	for _, it := range p.items {
		l := strings.ToLower(strings.TrimPrefix(it.Label, "/"))
		switch {
		case strings.HasPrefix(l, f):
			prefix = append(prefix, it)
		case strings.Contains(l, f) || strings.Contains(strings.ToLower(it.Detail), f):
			sub = append(sub, it)
		}
	}
	return append(prefix, sub...)
}

func (p *picker) header() string {
	if p.slash {
		return p.prompt + "/" + p.filter
	}
	return "  " + onLime + " " + strings.ToUpper(p.title) + " " + reset + " " + p.filter
}

func (p *picker) render(w io.Writer) {
	var b strings.Builder
	if !p.drawn { // make room so the list never scrolls the header away
		b.WriteString(strings.Repeat("\r\n", p.rows+1))
		fmt.Fprintf(&b, "\x1b[%dA", p.rows+1)
		p.drawn = true
	}
	vis := p.visible()
	if p.sel >= len(vis) {
		p.sel = len(vis) - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+p.rows {
		p.top = p.sel - p.rows + 1
	}
	width := termWidth() - 1
	labelW := 0
	for _, it := range vis {
		labelW = max(labelW, utf8.RuneCountInString(it.Label))
	}
	labelW = min(labelW, 32)

	b.WriteString("\r\x1b[2K" + p.header())
	lines := 0
	for i := p.top; i < len(vis) && i < p.top+p.rows; i++ {
		b.WriteString("\r\n\x1b[2K" + p.row(vis[i], i == p.sel, labelW, width))
		lines++
	}
	if len(vis) == 0 {
		b.WriteString("\r\n\x1b[2K    " + slate + "no matches" + reset)
		lines++
	}
	more := ""
	if len(vis) > p.rows {
		more = fmt.Sprintf("  %d/%d", p.sel+1, len(vis))
	}
	b.WriteString("\r\n\x1b[2K    " + slate + "↑↓ move · type to filter · enter select · esc cancel" + more + reset)
	lines++
	b.WriteString("\x1b[J")
	fmt.Fprintf(&b, "\x1b[%dA\r", lines)
	if col := visibleLen(p.header()); col > 0 {
		fmt.Fprintf(&b, "\x1b[%dC", col)
	}
	fmt.Fprint(w, b.String())
}

func (p *picker) row(it menuItem, selected bool, labelW, width int) string {
	mark := "  "
	if it.Mark {
		mark = green + "● " + reset
	}
	label := clip(it.Label, labelW)
	label += strings.Repeat(" ", labelW-utf8.RuneCountInString(label))
	pre := "  "
	if selected {
		pre = lime + "▸ " + reset
		label = lime + bold + label + reset
	} else if strings.HasPrefix(it.Label, "/") {
		label = lime + label + reset
	}
	room := width - 4 - 2 - labelW - 2 - utf8.RuneCountInString(it.Hint) - 2
	detail := ""
	if it.Detail != "" && room > 4 {
		detail = "  " + slate + clip(it.Detail, room) + reset
	}
	hint := ""
	if it.Hint != "" {
		hint = "  " + yellow + it.Hint + reset
	}
	return "  " + pre + mark + label + detail + hint
}

// run shows the picker until the user chooses, cancels, or (slash mode)
// types a space to keep writing arguments by hand.
func (p *picker) run(q *inputQueue, w io.Writer) (*menuItem, int) {
	for {
		p.render(w)
		k, err := q.readKey()
		if err != nil {
			return nil, pickCancel
		}
		vis := p.visible()
		switch k.kind {
		case kUp:
			if p.sel--; p.sel < 0 {
				p.sel = len(vis) - 1
			}
		case kDown:
			if p.sel++; p.sel >= len(vis) {
				p.sel = 0
			}
		case kEnter, kTab:
			if len(vis) > 0 {
				it := vis[p.sel]
				return &it, pickChose
			}
			if p.slash {
				return nil, pickTyped
			}
		case kEsc, kCancel:
			return nil, pickCancel
		case kBack:
			if p.filter == "" {
				if p.slash {
					return nil, pickCancel
				}
				continue
			}
			_, size := utf8.DecodeLastRuneInString(p.filter)
			p.filter = p.filter[:len(p.filter)-size]
			p.sel, p.top = 0, 0
		case kRune:
			if p.slash && k.r == ' ' {
				p.filter += " "
				return nil, pickTyped
			}
			p.filter += string(k.r)
			p.sel, p.top = 0, 0
		}
	}
}

// clear erases the picker, leaving the cursor at the start of its header line.
func (p *picker) clear(w io.Writer) {
	fmt.Fprint(w, "\r\x1b[J")
}

var reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func visibleLen(s string) int {
	return utf8.RuneCountInString(reANSI.ReplaceAllString(s, ""))
}

func clip(s string, n int) string {
	if n <= 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
