package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/media"
	"github.com/kxngHADES/asg-harness/internal/sprite"
)

// The Bubble Tea model. It owns the terminal while ASG runs; agent turns
// and slash commands run on a worker goroutine and reach the model only
// through messages posted by REPL methods (see AGENTS.md).

type (
	workerDoneMsg struct{ quit bool }
	busyMsg       struct {
		id    int64
		label string // "" stops spinner id
	}
	liveMsg   struct{ text string }
	animMsg   struct{}
	introMsg  struct{}
	bannerMsg struct{}
)

type model struct {
	r      *REPL
	width  int
	height int
	keys   keyMap
	input  textarea.Model
	help   help.Model

	working   bool // a worker goroutine is running a turn or command
	busyID    int64
	busyLabel string
	busyStart time.Time
	ticking   bool
	frame     int
	crab      crabMotion

	live string   // rendered block of the reply still being written
	form *formReq // open Huh form, if any

	menuSel   int
	history   []string
	histIdx   int
	lastCtrlC time.Time
	note      string // one-line hint under the input
	intro     int    // frame of the entrance animation, -1 when done
}

func newModel(r *REPL) *model {
	k := defaultKeys()
	ta := textarea.New()
	ta.KeyMap = inputKeys(k)
	ta.ShowLineNumbers = false
	ta.Placeholder = "ask anything · / for commands"
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 8
	ta.CharLimit = 0
	ta.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return limeBold("▸ ")
		}
		return slate("· ")
	})
	st := textarea.DefaultStyles(darkBackground)
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colSlate)
	st.Blurred.Placeholder = st.Focused.Placeholder
	st.Cursor.Color = colLime
	ta.SetStyles(st)
	ta.Focus()

	h := help.New()
	h.Styles = helpStyles()

	m := &model{r: r, keys: k, input: ta, help: h, crab: newCrab(float64(trackCells - crabCells)), intro: -1}
	if animate() && showSprite() {
		m.intro = 0
	}
	return m
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink}
	if m.intro == 0 {
		cmds = append(cmds, tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg { return introMsg{} }))
	} else {
		cmds = append(cmds, func() tea.Msg { return bannerMsg{} })
	}
	return tea.Batch(cmds...)
}

func (m *model) tick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tea.Tick(animEvery, func(time.Time) tea.Msg { return animMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		screenWidth.Store(int32(msg.Width))
		m.input.SetWidth(max(msg.Width-2, 10))
		m.help.SetWidth(msg.Width - 4)
		if m.form != nil {
			return m, m.updateForm(msg)
		}
		return m, nil

	case introMsg:
		if m.intro++; m.intro >= len(sprite.Enter) {
			m.intro = -1
			return m, tea.Println(m.r.bannerText())
		}
		return m, tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg { return introMsg{} })

	case bannerMsg:
		return m, tea.Println(m.r.bannerText())

	case busyMsg:
		if msg.label == "" {
			if msg.id == m.busyID {
				m.busyLabel = ""
			}
			return m, nil
		}
		m.busyID, m.busyLabel, m.busyStart = msg.id, msg.label, time.Now()
		return m, m.tick()

	case animMsg:
		m.ticking = false
		if m.busyLabel == "" {
			return m, nil
		}
		m.frame++
		m.crab.step()
		return m, m.tick()

	case liveMsg:
		m.live = msg.text
		return m, nil

	case formMsg:
		m.form = msg.req
		return m, m.form.form.Init()

	case workerDoneMsg:
		m.working, m.live, m.busyLabel = false, "", ""
		if msg.quit {
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.form != nil {
			return m, m.formKey(msg)
		}
		return m.onKey(msg)
	}

	var cmds []tea.Cmd
	if m.form != nil {
		cmds = append(cmds, m.updateForm(msg))
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, tea.Batch(append(cmds, cmd)...)
}

// --- forms ----------------------------------------------------------------

func (m *model) updateForm(msg tea.Msg) tea.Cmd {
	f, cmd := m.form.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form.form = ff
	}
	switch m.form.form.State {
	case huh.StateCompleted:
		m.finishForm(true)
	case huh.StateAborted:
		m.finishForm(false)
	}
	return cmd
}

func (m *model) finishForm(ok bool) {
	req := m.form
	m.form = nil
	req.done <- ok
}

func (m *model) formKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.form.onYolo != nil && key.Matches(msg, m.keys.Yolo) {
		m.form.onYolo()
		m.finishForm(true)
		return nil
	}
	if fn, ok := m.form.hotkeys[msg.String()]; ok {
		fn()
		m.finishForm(true)
		return nil
	}
	// Huh only quits on ctrl+c; esc closes the form too, unless the focused
	// field uses esc itself (e.g. to leave or clear a select's filter).
	if msg.String() == "esc" && !fieldUsesKey(m.form.form.GetFocusedField(), "esc") {
		m.finishForm(false)
		return nil
	}
	return m.updateForm(msg)
}

func fieldUsesKey(f huh.Field, k string) bool {
	if f == nil {
		return false
	}
	if s, ok := f.(interface{ GetFiltering() bool }); ok && s.GetFiltering() {
		return true
	}
	for _, b := range f.KeyBinds() {
		if !b.Enabled() {
			continue
		}
		for _, bk := range b.Keys() {
			if bk == k {
				return true
			}
		}
	}
	return false
}

// --- keys -----------------------------------------------------------------

func (m *model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.r
	if !key.Matches(msg, m.keys.Stop) {
		m.lastCtrlC = time.Time{}
	}
	vis, menuOpen := m.menu()

	switch {
	case key.Matches(msg, m.keys.Stop):
		if m.working {
			r.interrupt()
			m.note = "stopping…"
			return m, nil
		}
		if m.input.Value() != "" {
			m.input.Reset()
			return m, nil
		}
		if time.Since(m.lastCtrlC) < 2*time.Second {
			return m, tea.Quit
		}
		m.lastCtrlC = time.Now()
		m.note = "press ctrl+c again to exit"
		return m, nil

	case key.Matches(msg, m.keys.Exit) && m.input.Value() == "" && !m.working:
		return m, tea.Quit

	case key.Matches(msg, m.keys.Mode):
		if !m.working {
			r.setMode(r.mode().Next())
		}
		return m, nil

	case key.Matches(msg, m.keys.Domain):
		if !m.working {
			r.cycleDomain()
		}
		return m, nil

	case key.Matches(msg, m.keys.Yolo):
		return m, tea.Println(r.yoloLine(r.toggleYolo()))

	case key.Matches(msg, m.keys.Image):
		img, err := media.ClipboardImage()
		if err != nil {
			// No image on the clipboard: fall back to pasting text.
			return m, tea.ReadClipboard
		}
		n := r.addPending(attachment{name: "clipboard", img: img})
		m.input.InsertString(fmt.Sprintf("[Image #%d] ", n))
		return m, nil

	case menuOpen && key.Matches(msg, m.keys.Up):
		if m.menuSel--; m.menuSel < 0 {
			m.menuSel = len(vis) - 1
		}
		return m, nil

	case menuOpen && key.Matches(msg, m.keys.Down):
		if m.menuSel++; m.menuSel >= len(vis) {
			m.menuSel = 0
		}
		return m, nil

	case menuOpen && key.Matches(msg, m.keys.Complete):
		if len(vis) > 0 {
			v := vis[m.menuSel].Value
			if r.menuNeedsArg(v) {
				v += " "
			}
			m.input.SetValue(v)
			m.input.CursorEnd()
		}
		return m, nil

	case menuOpen && key.Matches(msg, m.keys.CloseMenu):
		m.input.Reset()
		return m, nil

	case menuOpen && key.Matches(msg, m.keys.Submit):
		if len(vis) == 0 {
			return m, m.submit(m.input.Value())
		}
		v := vis[m.menuSel].Value
		if r.menuNeedsArg(v) && v != strings.TrimSpace(m.input.Value()) {
			m.input.SetValue(v + " ")
			m.input.CursorEnd()
			return m, nil
		}
		return m, m.submit(v)

	case key.Matches(msg, m.keys.Submit):
		val := m.input.Value()
		if strings.HasSuffix(val, "\\") { // a trailing \ continues on the next line
			m.input.SetValue(strings.TrimSuffix(val, "\\") + "\n")
			m.input.CursorEnd()
			return m, nil
		}
		return m, m.submit(val)

	case !menuOpen && key.Matches(msg, m.keys.Up) && m.input.Line() == 0 && len(m.history) > 0:
		if m.histIdx > 0 {
			m.histIdx--
			m.input.SetValue(m.history[m.histIdx])
			m.input.CursorEnd()
		}
		return m, nil

	case !menuOpen && key.Matches(msg, m.keys.Down) && m.input.Line() == m.input.LineCount()-1 && m.histIdx < len(m.history):
		m.histIdx++
		if m.histIdx == len(m.history) {
			m.input.Reset()
		} else {
			m.input.SetValue(m.history[m.histIdx])
			m.input.CursorEnd()
		}
		return m, nil
	}

	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.menuSel, m.note = 0, ""
	}
	return m, cmd
}

// menu returns the '/' menu items when it is open.
func (m *model) menu() ([]menuItem, bool) {
	f, ok := slashFilter(m.input.Value())
	if !ok || m.form != nil {
		return nil, false
	}
	vis := filterItems(m.r.slashItems(), f)
	if m.menuSel >= len(vis) {
		m.menuSel = max(len(vis)-1, 0)
	}
	return vis, true
}

// submit echoes the input into the scrollback and hands it to the worker.
func (m *model) submit(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" && !m.r.hasPending() {
		return nil
	}
	if m.working {
		m.note = "still working — ctrl+c stops this turn"
		return nil
	}
	m.input.Reset()
	m.menuSel, m.note = 0, ""
	if t := strings.TrimSpace(text); t != "" && (len(m.history) == 0 || m.history[len(m.history)-1] != t) {
		m.history = append(m.history, t)
	}
	m.histIdx = len(m.history)
	m.working = true
	r := m.r
	return tea.Sequence(
		tea.Println(echoLine(text)),
		func() tea.Msg { return workerDoneMsg{quit: r.handle(text)} },
	)
}

func echoLine(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range lines {
		pre := "  "
		if i == 0 {
			pre = "▸ "
		}
		lines[i] = echoStyle.Render(pre) + l
	}
	return "\n" + strings.Join(lines, "\n")
}

// --- view -----------------------------------------------------------------

func (m *model) View() tea.View {
	if m.intro >= 0 {
		return tea.NewView(introFrame(m.intro))
	}
	var parts []string
	if m.live != "" {
		room := max(m.height-m.chromeHeight(), 3)
		parts = append(parts, lastLines(m.live, room))
	}
	if m.busyLabel != "" && m.form == nil {
		parts = append(parts, workView(m.frame, &m.crab, m.busyLabel, time.Since(m.busyStart)))
	}
	if m.form != nil {
		parts = append(parts, m.form.form.View())
	} else {
		parts = append(parts, rule(m.width), m.r.statusStrip(m.width), m.input.View())
		if vis, ok := m.menu(); ok {
			parts = append(parts, renderMenu(vis, m.menuSel, m.width))
		} else {
			parts = append(parts, m.footer())
		}
	}
	v := tea.NewView(strings.Join(parts, "\n"))
	v.WindowTitle = "asg · " + string(m.r.mode()) + " · " + m.r.track().String()
	return v
}

// chromeHeight estimates the rows the input area and animation take, so the
// live reply never pushes them off screen.
func (m *model) chromeHeight() int {
	h := 4 + m.input.Height()
	if m.busyLabel != "" {
		h += spriteRows
	}
	return h
}

func (m *model) footer() string {
	left := "  " + m.help.ShortHelpView(m.keys.ShortHelp())
	if m.note != "" {
		left = "  " + amber(m.note)
	}
	return left
}

// --- the worker side ---------------------------------------------------------

// outbox delivers messages from worker goroutines to the program in order,
// without ever blocking the sender — so code reachable from Update (such as
// a config error printed by toggleYolo) can post safely.
type outbox struct {
	mu     sync.Mutex
	cond   *sync.Cond
	q      []tea.Msg
	closed bool
}

func newOutbox(p *tea.Program) *outbox {
	o := &outbox{}
	o.cond = sync.NewCond(&o.mu)
	go func() {
		for {
			o.mu.Lock()
			for len(o.q) == 0 && !o.closed {
				o.cond.Wait()
			}
			if len(o.q) == 0 {
				o.mu.Unlock()
				return
			}
			msg := o.q[0]
			o.q = o.q[1:]
			o.mu.Unlock()
			p.Send(msg)
		}
	}()
	return o
}

func (o *outbox) post(msg tea.Msg) {
	o.mu.Lock()
	if !o.closed {
		o.q = append(o.q, msg)
		o.cond.Signal()
	}
	o.mu.Unlock()
}

func (o *outbox) close() {
	o.mu.Lock()
	o.closed = true
	o.q = nil
	o.cond.Signal()
	o.mu.Unlock()
}

// bannerText is the welcome block printed once at start.
func (r *REPL) bannerText() string {
	cfg := r.app.Config.Get()
	name, pc := cfg.Active()
	model := ""
	if pc != nil {
		model = pc.Model
	}
	cwd := workingDir()
	project := ""
	if !fileExists("ASG.md") {
		project = slate(" · no ASG.md, try ") + lime("/init")
	}
	t := r.track()
	return "\n" + bannerText([]string{
		badge(colLime, "ASG") + " " + bold("agent shell"),
		slate(name+"/") + lime(model) + "  " + domainStyle(t.Domain).Render(t.String()),
		slate(oneLine(cwd, 40)) + project,
		"type " + lime("/") + " for commands",
	}) + "\n"
}

// modeTab is the mode as a badge when active, dim text otherwise.
func modeTab(m agent.Mode, active bool) string {
	if active {
		return badge(modeColor(m), string(m))
	}
	return slate(" " + string(m) + " ")
}
