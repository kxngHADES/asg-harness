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
	fd      int
	pending []attachment

	mu        sync.Mutex
	cancel    context.CancelFunc
	lastReply string
	lastEOF   time.Time

	md   *mdStream
	spin *spinner
}

func New(app *agent.App) *REPL {
	r := &REPL{app: app, sess: app.NewSession(), fd: int(os.Stdin.Fd())}
	rw := struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}
	r.t = term.NewTerminal(rw, "")
	r.t.AutoCompleteCallback = r.onKey
	return r
}

type command struct {
	name, args, help string
}

var commands = []command{
	{"/help", "", "show this help"},
	{"/paste", "", "attach an image from the clipboard (or press Ctrl+V)"},
	{"/image", "<path>", "attach an image file (you can also drag files into the terminal)"},
	{"/model", "[name]", "show or set the model for the active provider"},
	{"/models", "", "list models available from the active provider"},
	{"/provider", "[name]", "show or switch provider (ollama, anthropic, openai, ...)"},
	{"/voice", "[on|off|auto|…]", "speak replies aloud and choose who speaks (/voice help)"},
	{"/speak", "", "read the last reply aloud"},
	{"/skills", "", "list installed skills"},
	{"/skill", "add <src> | rm <name> | show <name>", "import a skill (path, URL, github:owner/repo[/path]) or remove one"},
	{"/tools", "", "list tools the model can call"},
	{"/yolo", "", "toggle auto-approval of tool calls"},
	{"/dashboard", "", "open the browser dashboard (chat, config, skills)"},
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
	fmt.Println()
	fmt.Print(wordmark())
	r.printf("  %s AGENT SHELL %s  %s%s%s/%s%s%s\n", onLime, reset, slate, name, reset, lime, model, reset)
	r.printf("  %s%s%s\n", slate, cwd, reset)
	r.printf("  %s%s%s\n", slate, strings.Repeat("━", min(termWidth()-4, 60)), reset)
	r.printf("  %s/help%s commands  %s·%s  %sctrl+v%s paste image  %s·%s  %sctrl+c%s stop  %s·%s  %s/dashboard%s web ui\n\n",
		lime, reset, slate, reset, lime, reset, slate, reset, lime, reset, slate, reset, lime, reset)
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
	for {
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
	r.run(msg)
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
	r.printf("  %s── %.1fs · %d↑ %d↓ tokens ──%s\n\n", slate, time.Since(start).Seconds(), u.InputTokens, u.OutputTokens, reset)
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

func (r *REPL) Approve(ctx context.Context, call llm.ToolCall, summary string) agent.Decision {
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
	r.t.SetPrompt(fmt.Sprintf("  %s[y]%s run  %s[a]%s always %s  %s[n]%s block %s▸%s ", lime, reset, lime, reset, call.Name, red, reset, amber, reset))
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
			return agent.AlwaysAllow
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
			r.printf("  %s%-10s%s %-36s %s%s%s\n", lime, c.name, reset, c.args, slate, c.help, reset)
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
		cfg := r.app.Config.Get()
		name, pc := cfg.Active()
		if len(args) == 0 {
			r.printf("%s · %s%s%s\n", name, bold, pc.Model, reset)
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
			for _, n := range cfg.ProviderNames() {
				p := cfg.Providers[n]
				mark := "  "
				if n == cfg.ActiveProvider {
					mark = green + "● " + reset
				}
				key := ""
				if p.Type != "ollama" && p.APIKey == "" {
					key = yellow + " (no API key — set it in /dashboard)" + reset
				}
				r.printf("%s%-12s %s%s · %s%s%s\n", mark, n, dim, p.Type, p.Model, reset, key)
			}
			return
		}
		if _, ok := cfg.Providers[args[0]]; !ok {
			r.printf("%sno provider %q — add it in /dashboard or %s%s\n", red, args[0], r.app.Config.Path(), reset)
			return
		}
		r.update(func(c *config.Config) error { c.ActiveProvider = args[0]; return nil })
		r.printf("%sprovider → %s (%s)%s\n", dim, args[0], r.app.Config.Get().Providers[args[0]].Model, reset)
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
		on := !r.app.Config.Get().AutoApprove
		r.update(func(c *config.Config) error { c.AutoApprove = on; return nil })
		r.printf("%sauto-approve %s%s\n", yellow, map[bool]string{true: "ON — tools run without asking", false: "off"}[on], reset)
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
		r.printf("  file:       %s\n  provider:   %s (%s @ %s)\n  model:      %s\n  auto-approve: %v\n  voice:      %v (%s)\n  skills dir: %s\n",
			r.app.Config.Path(), name, pc.Type, pc.BaseURL, pc.Model, cfg.AutoApprove, cfg.Audio.Enabled, cfg.Audio.Engine, r.app.Skills.InstallDir())
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
