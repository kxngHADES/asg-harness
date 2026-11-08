package tui

import (
	"io"
	"strings"
	"testing"

	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

// keystrokes delivers one key per Read, as a terminal does: a single byte,
// or a whole escape sequence such as an arrow key.
type keystrokes struct{ keys string }

func (k *keystrokes) Read(p []byte) (int, error) {
	if k.keys == "" {
		return 0, io.EOF
	}
	n := 1
	if strings.HasPrefix(k.keys, "\x1b[") && len(k.keys) >= 3 {
		n = 3
	}
	n = copy(p, k.keys[:n])
	k.keys = k.keys[n:]
	return n, nil
}

// typeInto feeds keys one byte at a time (like a person typing) through the
// real line editor with the '/' menu hooked up, and returns the line read.
func typeInto(t *testing.T, keys string) string {
	t.Helper()
	t.Setenv("ASG_HOME", t.TempDir())
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := &REPL{app: agent.NewApp(store), in: &inputQueue{r: &keystrokes{keys: keys}}, out: io.Discard}
	r.t = term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{r.in, io.Discard}, "")
	r.t.AutoCompleteCallback = r.onKey
	r.menuOn = true
	line, err := r.t.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	return line
}

func TestSlashMenuInLineEditor(t *testing.T) {
	cases := []struct{ name, keys, want string }{
		{"filter and enter runs the command", "/dash\r", "/dashboard"},
		{"arrow to a command", "/\x1b[B\r", "/model"},
		{"commands needing args are inserted for editing", "/ima\rshot.png\r", "/image shot.png"},
		{"space hands back to normal typing", "/model llama3\r", "/model llama3"},
		{"esc keeps the typed text", "/prov\x1b r\r", "/prov r"},
		{"backspace on empty closes the menu", "/\x7fhello\r", "hello"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := typeInto(t, c.keys); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
