// Package tui is the interactive terminal front end.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/dashboard"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/media"
)

const keyCtrlV = 22

type attachment struct {
	name string
	img  llm.Image
}

type REPL struct {
	app     *agent.App
	sess    *agent.Session
	t       *term.Terminal
	in      *inputQueue
	out     io.Writer // where menus draw
	fd      int
	menuOn  bool // '/' on an empty main prompt opens the command menu
	gating  bool // a permission gate is waiting for an answer
	pending []attachment

	mu        sync.Mutex
	cancel    context.CancelFunc
	lastReply string
	lastEOF   time.Time

	md   *mdStream
	spin *spinner
}

func New(app *agent.App) *REPL {
	r := &REPL{app: app, sess: app.NewSession(), fd: int(os.Stdin.Fd()), in: &inputQueue{r: os.Stdin}, out: os.Stdout}
	rw := struct {
		io.Reader
		io.Writer
	}{r.in, os.Stdout}
	r.t = term.NewTerminal(rw, "")
	r.t.AutoCompleteCallback = r.onKey
	return r
}

type command struct {
	name, args, help string
}

// needsArg reports whether a command is useless without arguments, so the
// '/' menu inserts it for editing instead of running it.
func (c command) needsArg() bool { return strings.HasPrefix(c.args, "<") }

var commands = []command{
	{"/help", "", "show this help"},
	{"/model", "[name]", "pick a model from a menu, or set one by name"},
	{"/provider", "[name]", "pick a provider (ollama, anthropic, openai, ...) and then its model"},
	{"/mode", "[build|plan|loop|ask]", "pick how ASG works (or press shift+tab to cycle)"},
	{"/plan", "[request]", "plan mode: investigate read-only and write a step-by-step plan"},
	{"/build", "[request]", "build mode: make changes (the default)"},
	{"/ask", "[question]", "ask mode: read-only questions about your code"},
	{"/loop", "[objective] | resume [answer] | status | verify <cmd>", "run an objective plan → do → verify → review until done"},
	{"/dashboard", "", "open the browser dashboard (chat, config, skills)"},
	{"/init", "", "write an ASG.md describing this project for future sessions"},
	{"/memory", "[note]", "browse saved memories, or ask ASG to remember something"},
	{"/permissions", "", `review and revoke remembered "always allow" answers`},
	{"/paste", "", "attach an image from the clipboard (or press Ctrl+V)"},
	{"/image", "<path>", "attach an image file (you can also drag files into the terminal)"},
	{"/models", "", "list models available from the active provider"},
	{"/voice", "[on|off|auto|…]", "speak replies aloud and choose who speaks (/voice help)"},
	{"/speak", "", "read the last reply aloud"},
	{"/skills", "", "list installed skills"},
	{"/skill", "add <src> | rm <name> | show <name>", "import a skill (path, URL, github:owner/repo[/path]) or remove one"},
	{"/tools", "", "list tools the model can call"},
	{"/yolo", "", "toggle YOLO: run tools without asking (or press ctrl+y)"},
	{"/config", "", "show config file location and active settings"},
	{"/usage", "", "show token usage for this session"},
	{"/clear", "", "start a new conversation"},
	{"/exit", "", "quit"},
}

func (r *REPL) printf(format string, a ...any) { fmt.Printf(format, a...) }

func (r *REPL) banner() {
	cfg := r.app.Config.Get()
	name, pc := cfg.Active()
	model := ""
	if pc != nil {
		model = pc.Model
	}
	cwd, _ := os.Getwd()
	project := ""
	if _, err := os.Stat("ASG.md"); err != nil {
		project = slate + " · no ASG.md, try " + lime + "/init" + reset
	}
	fmt.Println()
	petBanner(os.Stdout, []string{
		onLime + " ASG " + reset + " " + bold + "agent shell" + reset,
		slate + name + reset + "/" + lime + model + reset,
		slate + oneLine(cwd, 40) + reset + project,
		"type " + lime + "/" + reset + " for commands",
	})
	r.printf("  %s%s%s\n", slate, strings.Repeat("━", min(termWidth()-4, 60)), reset)
	r.printf("  %s/%s menu  %s·%s  %sshift+tab%s mode  %s·%s  %sctrl+y%s yolo  %s·%s  %sctrl+v%s paste image  %s·%s  %sctrl+c%s stop\n\n",
		lime, reset, slate, reset, lime, reset, slate, reset, lime, reset, slate, reset, lime, reset, slate, reset, lime, reset)
}

const keyCtrlY = 25

// modeColor gives each mode a colour of its own in the prompt badge.
func modeColor(m agent.Mode) string {
	switch m {
	case agent.ModePlan:
		return cyan
	case agent.ModeAsk:
		return magenta
	case agent.ModeLoop:
		return amber
	}
	return lime
}

// yolo reports whether tool calls currently run without asking.
func (r *REPL) yolo() bool { return r.app.Yolo || r.app.Config.Get().AutoApprove }

// toggleYolo flips YOLO for this run. Turning it off also clears a saved
// auto_approve so "off" always means off.
func (r *REPL) toggleYolo() bool {
	if r.yolo() {
		r.app.Yolo = false
		if r.app.Config.Get().AutoApprove {
			r.update(func(c *config.Config) error { c.AutoApprove = false; return nil })
		}
		return false
	}
	r.app.Yolo = true
	return true
}

func (r *REPL) yoloLine(on bool) string {
	if on {
		return fmt.Sprintf("  %s%s⚡ YOLO ON %s %stools run without asking until you press ctrl+y again%s\n", bold, onAmber, reset, amber, reset)
	}
	return fmt.Sprintf("  %sYOLO off — tool calls ask first%s\n", slate, reset)
}

func (r *REPL) setMode(m agent.Mode) {
	r.sess.Mode = m
}

// SetMode picks the mode the session starts in (the -mode flag).
func (r *REPL) SetMode(m agent.Mode) { r.setMode(m) }

func (r *REPL) modeLine() string {
	m := r.mode()
	extra := ""
	if m == agent.ModeLoop && r.sess.Plan != "" {
		extra = fmt.Sprintf(" %s· /loop runs the plan you just made%s", slate, reset)
	}
	return fmt.Sprintf("  %s%s %s mode %s %s%s%s%s\n", bold, onColor(modeColor(m)), strings.ToUpper(string(m)), reset, slate, m.Describe(), reset, extra)
}

func (r *REPL) mode() agent.Mode {
	if r.sess == nil || r.sess.Mode == "" {
		return agent.ModeBuild
	}
	return r.sess.Mode
}

// Run starts the read-eval-print loop.
func (r *REPL) Run() error {
	if !term.IsTerminal(r.fd) {
		return errors.New("stdin is not a terminal; use -p for one-shot prompts")
	}
	r.banner()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		for range sig {
			r.mu.Lock()
			if r.cancel != nil {
				r.cancel()
			}
			r.mu.Unlock()
			r.app.Speaker.Stop()
		}
	}()
	for {
		line, err := r.readInput()
		if err == io.EOF {
			if time.Since(r.lastEOF) < 2*time.Second {
				r.printf("%sbye%s\n", dim, reset)
				return nil
			}
			r.lastEOF = time.Now()
			r.printf("%s(press Ctrl+C again to exit)%s\n", dim, reset)
			continue
		}
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" && len(r.pending) == 0 {
			continue
		}
		line = normalizeCommand(line)
		if strings.HasPrefix(line, "/") && !media.IsImagePath(strings.Fields(line)[0]) {
			if quit := r.command(line); quit {
				return nil
			}
			continue
		}
		r.send(line)
	}
}

// bareCommands are words that, typed alone, almost certainly mean the
// command rather than a message to the model.
var bareCommands = map[string]bool{"clear": true, "exit": true, "quit": true, "help": true}

// normalizeCommand forgives "//model" typos and bare "clear"/"exit"/….
func normalizeCommand(line string) string {
	if strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "///") {
		return line[1:]
	}
	if bareCommands[strings.ToLower(line)] {
		return "/" + strings.ToLower(line)
	}
	return line
}

func (r *REPL) prompt() string {
	model := ""
	cfg := r.app.Config.Get()
	if _, pc := cfg.Active(); pc != nil {
		model = pc.Model
	}
	if len(model) > 22 {
		model = model[:21] + "…"
	}
	p := slate + model + " " + reset + lime + bold + "▸ " + reset
	if m := r.mode(); m != agent.ModeBuild {
		p = modeColor(m) + bold + string(m) + reset + " " + p
	}
	if r.yolo() {
		p = amber + bold + "⚡yolo" + reset + " " + p
	}
	if n := len(r.pending); n > 0 {
		p = fmt.Sprintf("%s▣ %d img%s %s", magenta, n, reset, p)
	}
	return p
}

// readInput reads one logical input, joining bracketed pastes and
// backslash-continued lines.
func (r *REPL) readInput() (string, error) {
	old, err := term.MakeRaw(r.fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(r.fd, old)
	r.t.SetBracketedPasteMode(true)
	defer r.t.SetBracketedPasteMode(false)

	var lines []string
	r.t.SetPrompt(r.prompt())
	defer func() { r.menuOn = false }()
	for {
		r.menuOn = len(lines) == 0
		line, err := r.t.ReadLine()
		if err == term.ErrPasteIndicator {
			lines = append(lines, line)
			r.t.SetPrompt(dim + "… " + reset)
			continue
		}
		if err != nil {
			return "", err
		}
		if strings.HasSuffix(line, "\\") {
			lines = append(lines, strings.TrimSuffix(line, "\\"))
			r.t.SetPrompt(dim + "… " + reset)
			continue
		}
		lines = append(lines, line)
		return strings.Join(lines, "\n"), nil
	}
}

// onKey handles Ctrl+V image paste and Tab completion of slash commands.
func (r *REPL) onKey(line string, pos int, key rune) (string, int, bool) {
	switch key {
	case keyShiftTab:
		if r.gating {
			return line, pos, true
		}
		r.setMode(r.mode().Next())
		r.t.SetPrompt(r.prompt())
		fmt.Fprint(r.t, r.modeLine()) // writing redraws the prompt and line
		return line, pos, true
	case keyCtrlY:
		on := r.toggleYolo()
		if r.gating {
			fmt.Fprint(r.t, r.yoloLine(on)) // keep the gate's own prompt
			if !on {
				return line, pos, true
			}
			// At a permission gate, ctrl+y also approves the waiting call.
			r.in.unread([]byte{'\r'})
			return "y", 1, true
		}
		r.t.SetPrompt(r.prompt())
		fmt.Fprint(r.t, r.yoloLine(on))
		return line, pos, true
	case '/':
		if r.menuOn && line == "" {
			return r.slashMenu()
		}
	case keyCtrlV:
		img, err := media.ClipboardImage()
		if err != nil {
			fmt.Fprintf(r.t, "%s%s%s\n", yellow, err, reset)
			return "", 0, false
		}
		r.pending = append(r.pending, attachment{name: "clipboard", img: img})
		r.t.SetPrompt(r.prompt())
		tag := fmt.Sprintf("[Image #%d] ", len(r.pending))
		return line[:pos] + tag + line[pos:], pos + len(tag), true
	case '\t':
		if !strings.HasPrefix(line, "/") || strings.Contains(line, " ") {
			return "", 0, false
		}
		var matches []string
		for _, c := range r.commandNames() {
			if strings.HasPrefix(c, line) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 0 {
			return "", 0, false
		}
		if len(matches) == 1 {
			return matches[0] + " ", len(matches[0]) + 1, true
		}
		fmt.Fprintf(r.t, "%s%s%s\n", dim, strings.Join(matches, "  "), reset)
		p := commonPrefix(matches)
		return p, len(p), true
	}
	return "", 0, false
}

func (r *REPL) commandNames() []string {
	var names []string
	for _, c := range commands {
		names = append(names, c.name)
	}
	for _, s := range r.app.Skills.List() {
		names = append(names, "/"+s.Name)
	}
	sort.Strings(names)
	return names
}

func commonPrefix(ss []string) string {
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (r *REPL) send(text string) {
	text, imgs, names := media.ExtractImages(text)
	for i, img := range imgs {
		r.pending = append(r.pending, attachment{name: names[i], img: img})
	}
	msg := llm.Message{Content: text}
	for _, a := range r.pending {
		msg.Images = append(msg.Images, a.img)
	}
	if len(r.pending) > 0 {
		var ns []string
		for _, a := range r.pending {
			ns = append(ns, a.name)
		}
		r.printf("  %s▣ attached %s%s\n", magenta, strings.Join(ns, ", "), reset)
	}
	r.pending = nil
	if strings.TrimSpace(msg.Content) == "" && len(msg.Images) > 0 {
		msg.Content = "Describe this image."
	}
	if r.mode() == agent.ModeLoop {
		if len(msg.Images) > 0 {
			r.printf("  %s! images are not passed to loop runs — describe what matters in the objective%s\n", amber, reset)
		}
		r.loop(msg.Content, agent.LoopOptions{})
		return
	}
	r.run(msg)
}

// loop runs the loop harness on an objective (or resumes a saved run).
func (r *REPL) loop(objective string, opt agent.LoopOptions) {
	r.app.Speaker.Stop()
	fmt.Println()
	r.md = newMDStream()
	r.spin = startSpinner("starting loop")
	start := time.Now()
	_, err := r.withCancel(func(ctx context.Context) (string, error) {
		_, err := r.sess.RunLoop(ctx, objective, opt, r)
		return "", err
	})
	r.spin.Stop()
	r.md.Flush()
	switch {
	case errors.Is(err, context.Canceled):
		r.printf("  %s■ loop paused%s %s— /loop resume picks up where it stopped%s\n\n", amber, reset, slate, reset)
		return
	case err != nil:
		r.printf("  %s✕ %v%s\n\n", red, err, reset)
		return
	}
	u := r.sess.Usage
	r.printf("  %s── loop %.0fs · %d↓ tokens generated ──%s\n\n", slate, time.Since(start).Seconds(), u.OutputTokens, reset)
}

// withCancel runs fn with a context that Ctrl+C cancels.
func (r *REPL) withCancel(fn func(ctx context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
		cancel()
	}()
	return fn(ctx)
}

func (r *REPL) run(msg llm.Message) {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
		cancel()
	}()

	r.app.Speaker.Stop()
	fmt.Println()
	r.md = newMDStream()
	r.spin = startSpinner("thinking")
	start := time.Now()
	reply, err := r.sess.Send(ctx, msg, r)
	r.spin.Stop()
	r.md.Flush()
	switch {
	case errors.Is(err, context.Canceled):
		r.printf("  %s■ stopped%s\n\n", amber, reset)
		return
	case err != nil:
		r.printf("  %s✕ %v%s\n\n", red, err, reset)
		return
	}
	u := r.sess.Usage
	r.printf("  %s── %.1fs · %d↑ %d↓ tokens ──%s\n", slate, time.Since(start).Seconds(), u.InputTokens, u.OutputTokens, reset)
	if r.mode() == agent.ModePlan && r.sess.Plan != "" && reply == r.sess.Plan {
		r.printf("  %splan saved · %s/loop%s%s runs it step by step with checks · %sshift+tab%s%s to build mode works through it with you%s\n",
			slate, lime, reset, slate, lime, reset, slate, reset)
	}
	r.printf("\n")
	r.lastReply = reply
	if r.app.Config.Get().Audio.Enabled {
		r.speak(reply)
	}
}

func (r *REPL) speak(text string) {
	done := r.app.Speaker.Speak(text, r.app.ResolveVoice(context.Background()))
	go func() {
		if err := <-done; err != nil {
			fmt.Fprintf(r.t, "%saudio: %v%s\n", yellow, err, reset)
		}
	}()
}

// --- agent.Events -------------------------------------------------------

func (r *REPL) Text(delta string) {
	r.spin.Stop()
	r.md.Write(delta)
}

func (r *REPL) ToolStart(call llm.ToolCall, summary string) {
	r.spin.Stop()
	r.md.Flush()
	r.printf("  %s◆ %s%s %s%s%s\n", cyan, strings.ToUpper(call.Name), reset, slate, oneLine(summary, termWidth()-len(call.Name)-8), reset)
	r.spin = startSpinner("running " + call.Name)
}

func (r *REPL) ToolEnd(call llm.ToolCall, result string, isErr bool) {
	r.spin.Stop()
	color, mark := slate, "┊"
	if isErr {
		color, mark = red, "✕"
	}
	for _, l := range strings.Split(preview(result, 4), "\n") {
		r.printf("  %s%s %s%s\n", color, mark, l, reset)
	}
	r.md = newMDStream()
	r.spin = startSpinner("thinking")
}

func (r *REPL) Notice(msg string) {
	r.spin.Stop()
	r.md.Flush()
	r.printf("  %s! %s%s\n", amber, msg, reset)
}

// LoopUpdate draws loop harness progress (agent.LoopEvents).
func (r *REPL) LoopUpdate(st *agent.LoopState, msg string) {
	r.spin.Stop()
	r.md.Flush()
	color := amber
	switch {
	case strings.HasPrefix(msg, "✓"):
		color = green
	case strings.HasPrefix(msg, "✗"):
		color = red
	}
	if strings.HasPrefix(msg, "step ") {
		r.printf("\n")
	}
	r.printf("  %s%s⟳ LOOP %s %s%s%s\n", bold, onAmber, reset, color, msg, reset)
	if strings.HasPrefix(msg, "plan ready") || strings.HasPrefix(msg, "split ") || st.Status != agent.LoopRunning {
		r.printChecklist(st)
	}
	r.md = newMDStream()
	r.spin = startSpinner("working")
}

func (r *REPL) printChecklist(st *agent.LoopState) {
	for _, l := range strings.Split(strings.TrimRight(st.Checklist(), "\n"), "\n") {
		c := slate
		switch {
		case strings.HasPrefix(l, "✓"):
			c = green
		case strings.HasPrefix(l, "▶"):
			c = amber
		case strings.HasPrefix(l, "✗"):
			c = red
		}
		r.printf("    %s%s%s\n", c, oneLine(l, termWidth()-6), reset)
	}
}

func (r *REPL) Approve(ctx context.Context, call llm.ToolCall, gate agent.Gate) agent.Decision {
	summary := gate.Summary
	r.spin.Stop()
	r.md.Flush()
	bar := strings.Repeat("▀", min(termWidth()-14, 48))
	r.printf("\n  %s▛▀ GATE %s%s\n", amber, bar, reset)
	r.printf("  %s▌%s %s%s%s requests permission\n", amber, reset, bold, strings.ToUpper(call.Name), reset)
	detail := summary
	if call.Name == "write_file" || call.Name == "edit_file" {
		detail = describeEdit(call)
	}
	for _, l := range strings.Split(strings.TrimRight(detail, "\n"), "\n") {
		r.printf("  %s▌%s %s\n", amber, reset, l)
	}
	r.printf("  %s▙%s%s\n", amber, strings.Repeat("▄", len([]rune(bar))+7), reset)

	old, err := term.MakeRaw(r.fd)
	if err != nil {
		return agent.Deny
	}
	defer term.Restore(r.fd, old)
	r.gating = true
	defer func() { r.gating = false }()
	opts := fmt.Sprintf("  %s[y]%s run  ", lime, reset)
	if gate.Scope != "" {
		opts += fmt.Sprintf("%s[a]%s always %s%s%s here  %s[g]%s everywhere  ", lime, reset, bold, gate.Scope, reset, lime, reset)
	}
	opts += fmt.Sprintf("%s[ctrl+y]%s yolo  ", amber, reset)
	r.t.SetPrompt(opts + fmt.Sprintf("%s[n]%s block %s▸%s ", red, reset, amber, reset))
	for {
		ans, err := r.t.ReadLine()
		if err != nil {
			r.mu.Lock()
			if r.cancel != nil {
				r.cancel()
			}
			r.mu.Unlock()
			return agent.Deny
		}
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "y", "yes", "":
			return agent.Allow
		case "a", "always":
			if gate.Scope != "" {
				r.printf("  %sremembered: %s is always allowed in this project (/permissions to undo)%s\n", slate, gate.Scope, reset)
				return agent.AlwaysAllow
			}
		case "g", "global", "everywhere":
			if gate.Scope != "" {
				r.printf("  %sremembered: %s is always allowed everywhere (/permissions to undo)%s\n", slate, gate.Scope, reset)
				return agent.AlwaysAllowGlobal
			}
		case "n", "no":
			return agent.Deny
		}
	}
}

func describeEdit(call llm.ToolCall) string {
	var a map[string]any
	_ = json.Unmarshal(call.Args, &a)
	var b strings.Builder
	fmt.Fprintf(&b, "%v\n", a["path"])
	if old, ok := a["old_string"].(string); ok {
		for _, l := range strings.Split(preview(old, 6), "\n") {
			fmt.Fprintf(&b, "%s- %s%s\n", red, strings.TrimSpace(l), reset)
		}
		nw, _ := a["new_string"].(string)
		for _, l := range strings.Split(preview(nw, 6), "\n") {
			fmt.Fprintf(&b, "%s+ %s%s\n", green, strings.TrimSpace(l), reset)
		}
	} else if c, ok := a["content"].(string); ok {
		for _, l := range strings.Split(preview(c, 8), "\n") {
			fmt.Fprintf(&b, "%s+ %s%s\n", green, strings.TrimSpace(l), reset)
		}
	}
	return b.String()
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 10 && len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

// --- slash commands -----------------------------------------------------

func (r *REPL) command(line string) (quit bool) {
	fields := strings.Fields(line)
	cmd, args := fields[0], fields[1:]
	rest := strings.TrimSpace(strings.TrimPrefix(line, cmd))
	ctx := context.Background()

	switch cmd {
	case "/exit", "/quit":
		return true
	case "/help":
		r.printf("\n  %s COMMANDS %s\n", onLime, reset)
		for _, c := range commands {
			r.printf("  %s%-13s%s %-36s %s%s%s\n", lime, c.name, reset, c.args, slate, c.help, reset)
		}
		if sk := r.app.Skills.List(); len(sk) > 0 {
			r.printf("\n  %s SKILLS %s %s/<name> [request]%s\n", onLime, reset, slate, reset)
			for _, s := range sk {
				r.printf("  %s/%-20s%s %s%s%s\n", magenta, s.Name, reset, dim, oneLine(s.Description, termWidth()-26), reset)
			}
		}
		r.printf("\n%sTips: end a line with \\ for multi-line input · paste multi-line text freely · @path/to/img.png attaches an image%s\n\n", dim, reset)
	case "/clear":
		r.sess.Clear()
		r.pending = nil
		r.printf("%sconversation cleared%s\n", dim, reset)
	case "/paste":
		img, err := media.ClipboardImage()
		if err != nil {
			r.printf("%s%v%s\n", yellow, err, reset)
			return
		}
		r.pending = append(r.pending, attachment{name: "clipboard", img: img})
		r.printf("  %s▣ image attached (%s, %d KB) — type your message%s\n", magenta, img.MediaType, len(img.Data)/1024, reset)
	case "/image":
		_, imgs, names := media.ExtractImages(rest)
		if len(imgs) == 0 {
			r.printf("%sno image found at %q%s\n", yellow, rest, reset)
			return
		}
		for i, img := range imgs {
			r.pending = append(r.pending, attachment{name: names[i], img: img})
		}
		r.printf("  %s▣ attached %s%s\n", magenta, strings.Join(names, ", "), reset)
	case "/model":
		if len(args) == 0 {
			r.pickModel(ctx)
			return
		}
		r.update(func(c *config.Config) error { c.Providers[c.ActiveProvider].Model = rest; return nil })
		r.printf("%smodel → %s%s\n", dim, rest, reset)
	case "/models":
		p, cur, err := r.app.Provider()
		if err == nil {
			var models []string
			if models, err = p.ListModels(ctx); err == nil {
				for _, m := range models {
					mark := "  "
					if m == cur {
						mark = green + "● " + reset
					}
					r.printf("%s%s\n", mark, m)
				}
			}
		}
		if err != nil {
			r.printf("%s%v%s\n", red, err, reset)
		}
	case "/provider", "/providers":
		cfg := r.app.Config.Get()
		if len(args) == 0 {
			r.pickProvider(ctx)
			return
		}
		if _, ok := cfg.Providers[args[0]]; !ok {
			r.printf("%sno provider %q — add it in /dashboard or %s%s\n", red, args[0], r.app.Config.Path(), reset)
			return
		}
		r.update(func(c *config.Config) error { c.ActiveProvider = args[0]; return nil })
		r.printf("%sprovider → %s (%s)%s\n", dim, args[0], r.app.Config.Get().Providers[args[0]].Model, reset)
	case "/init":
		r.initProject()
	case "/memory", "/memories":
		if rest != "" {
			r.send("Remember this for future sessions (save it with memory_save, choosing a fitting name and scope): " + rest)
			return
		}
		r.memoryMenu()
	case "/permissions", "/perms":
		r.permissionsMenu()
	case "/voice":
		r.voiceCommand(ctx, args)
	case "/speak":
		if r.lastReply == "" {
			r.printf("%snothing to speak yet%s\n", dim, reset)
			return
		}
		r.speak(r.lastReply)
	case "/skills":
		list := r.app.Skills.List()
		if len(list) == 0 {
			r.printf("%sno skills installed — try /skill add github:anthropics/skills/skills/pdf%s\n", dim, reset)
			return
		}
		for _, s := range list {
			r.printf("  %s%-16s%s %s%s%s\n", magenta, s.Name, reset, dim, oneLine(s.Description, termWidth()-22), reset)
		}
	case "/skill":
		r.skillCommand(ctx, args)
	case "/tools":
		for _, t := range r.app.Tools.All() {
			flag := ""
			if t.NeedsApproval {
				flag = yellow + " (asks)" + reset
			}
			r.printf("  %s%-11s%s%s %s%s%s\n", cyan, t.Spec.Name, reset, flag, dim, t.Spec.Description, reset)
		}
	case "/yolo":
		r.printf("%s", r.yoloLine(r.toggleYolo()))
	case "/mode", "/modes":
		if len(args) == 0 {
			r.pickMode()
			return
		}
		m, ok := agent.ParseMode(args[0])
		if !ok {
			r.printf("%sunknown mode %q — build, plan, loop or ask%s\n", yellow, args[0], reset)
			return
		}
		r.modeCommand(m, strings.TrimSpace(strings.TrimPrefix(rest, args[0])))
	case "/plan":
		r.modeCommand(agent.ModePlan, rest)
	case "/build":
		r.modeCommand(agent.ModeBuild, rest)
	case "/ask":
		r.modeCommand(agent.ModeAsk, rest)
	case "/loop":
		r.loopCommand(args, rest)
	case "/dashboard":
		url, err := dashboard.Ensure(r.app)
		if err != nil {
			r.printf("%s%v%s\n", red, err, reset)
			return
		}
		r.printf("%sdashboard running at %s%s\n", dim, url, reset)
		dashboard.OpenBrowser(url)
	case "/config":
		cfg := r.app.Config.Get()
		name, pc := cfg.Active()
		verify := cfg.Loop.Verify
		if verify == "" {
			cwd, _ := os.Getwd()
			verify = "auto (nothing detected)"
			if d := agent.DetectVerify(cwd); d != "" {
				verify = "auto → " + d
			}
		}
		r.printf("  file:       %s\n  provider:   %s (%s @ %s)\n  model:      %s\n  mode:       %s\n  yolo:       %v (saved auto_approve: %v)\n  loop:       %d iterations · %d attempts/step · %d tool rounds/step · verify: %s\n  voice:      %v (%s)\n  skills dir: %s\n  memory:     %s · %s\n  permissions: %s\n",
			r.app.Config.Path(), name, pc.Type, pc.BaseURL, pc.Model, r.mode(), r.yolo(), cfg.AutoApprove,
			cfg.Loop.MaxIterations, cfg.Loop.StepAttempts, cfg.Loop.StepRounds, verify,
			cfg.Audio.Enabled, cfg.Audio.Engine, r.app.Skills.InstallDir(),
			r.app.Memory.Dir("user"), r.app.Memory.Dir("project"), r.app.Perms.Path())
	case "/usage":
		u := r.sess.Usage
		r.printf("  last context: %d tokens · generated: %d tokens · messages: %d\n", u.InputTokens, u.OutputTokens, len(r.sess.History))
	default:
		name := strings.TrimPrefix(cmd, "/")
		if sk := r.app.Skills.Get(name); sk != nil {
			req := rest
			if req == "" {
				req = "Follow the skill's instructions."
			}
			r.printf("  %s✦ skill %s%s\n", magenta, sk.Name, reset)
			r.send(agent.SkillPrompt(sk) + "\n\n---\nUser request: " + req)
			return
		}
		r.printf("%sunknown command %s — /help lists commands%s\n", yellow, cmd, reset)
	}
	return false
}

// modeCommand switches mode and, given a request, sends it in that mode.
func (r *REPL) modeCommand(m agent.Mode, request string) {
	if r.mode() != m {
		r.setMode(m)
		r.printf("%s", r.modeLine())
	}
	if request != "" {
		r.send(request)
	}
}

func (r *REPL) pickMode() {
	var items []menuItem
	sel := 0
	for i, m := range agent.Modes {
		items = append(items, menuItem{Label: string(m), Detail: m.Describe(), Mark: m == r.mode(), Value: string(m)})
		if m == r.mode() {
			sel = i
		}
	}
	if it := r.choose("mode", items, sel); it != nil {
		m, _ := agent.ParseMode(it.Value)
		r.modeCommand(m, "")
	}
}

func (r *REPL) loopCommand(args []string, rest string) {
	cwd, _ := os.Getwd()
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "":
		if r.sess.Plan != "" {
			steps := agent.ParseSteps(r.sess.Plan)
			r.printf("  %srunning the plan from plan mode (%d steps)%s\n", slate, len(steps), reset)
			r.loop(r.sess.PlanGoal, agent.LoopOptions{Steps: steps})
			r.sess.Plan, r.sess.PlanGoal = "", ""
			return
		}
		r.modeCommand(agent.ModeLoop, "")
		r.printf("  %stype the objective, e.g. \"add a --json flag to the list command, with tests\"%s\n", slate, reset)
	case "resume", "continue":
		r.loop("", agent.LoopOptions{Resume: true, Notes: strings.TrimSpace(strings.TrimPrefix(rest, args[0]))})
	case "status":
		st, err := agent.LoadLoop(cwd)
		if err != nil {
			r.printf("%s%v%s\n", dim, err, reset)
			return
		}
		done, total := st.Progress()
		r.printf("  %s%s⟳ LOOP %s %s%s%s · %d/%d steps · %d iterations · updated %s\n  %sobjective:%s %s\n",
			bold, onAmber, reset, amber, st.Status, reset, done, total, st.Iteration, st.Updated.Format("Jan 2 15:04"), slate, reset, st.Objective)
		if st.Outcome != "" {
			r.printf("  %s%s%s\n", slate, st.Outcome, reset)
		}
		r.printChecklist(st)
	case "verify":
		cmd := strings.TrimSpace(strings.TrimPrefix(rest, args[0]))
		if cmd == "" {
			r.printf("usage: /loop verify <command>  ·  /loop verify none  ·  /loop verify auto\n")
			return
		}
		if cmd == "auto" {
			cmd = ""
		}
		r.update(func(c *config.Config) error { c.Loop.Verify = cmd; return nil })
		if cmd == "" {
			cmd = "auto-detect (" + agent.DetectVerify(cwd) + ")"
		}
		r.printf("%sloop verification → %s%s\n", dim, cmd, reset)
	default:
		r.loop(rest, agent.LoopOptions{})
	}
}

func (r *REPL) skillCommand(ctx context.Context, args []string) {
	if len(args) < 2 {
		r.printf("usage: /skill add <path|url|github:owner/repo[/path][@ref]>  ·  /skill rm <name>  ·  /skill show <name>\n")
		return
	}
	switch args[0] {
	case "add", "import", "install":
		sp := startSpinner("importing " + args[1])
		got, err := r.app.Skills.Import(ctx, args[1])
		sp.Stop()
		if err != nil {
			r.printf("%s%v%s\n", red, err, reset)
			return
		}
		for _, s := range got {
			r.printf("%s✓%s %s%s%s  %s%s%s\n", green, reset, magenta, s.Name, reset, dim, s.Description, reset)
		}
	case "rm", "remove", "delete":
		if err := r.app.Skills.Remove(args[1]); err != nil {
			r.printf("%s%v%s\n", red, err, reset)
			return
		}
		r.printf("%sremoved %s%s\n", dim, args[1], reset)
	case "show":
		sk := r.app.Skills.Get(args[1])
		if sk == nil {
			r.printf("%sno skill %q%s\n", red, args[1], reset)
			return
		}
		r.printf("%s%s%s — %s\n%s%s%s\n%s\n", bold, sk.Name, reset, sk.Description, dim, sk.Dir, reset, sk.Body)
	default:
		r.printf("unknown /skill subcommand %q\n", args[0])
	}
}

func (r *REPL) update(fn func(c *config.Config) error) {
	if err := r.app.Config.Update(fn); err != nil {
		r.printf("%s%v%s\n", red, err, reset)
	}
}
