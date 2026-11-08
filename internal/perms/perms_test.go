package perms

import (
	"encoding/json"
	"reflect"
	"testing"
)

func bash(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestRulesFor(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"git status", []string{"bash:git"}},
		{"git add . && git commit -m x", []string{"bash:git"}},
		{"FOO=1 go test ./... | grep FAIL", []string{"bash:go", "bash:grep"}},
		{"cd sub; npm test", []string{"bash:cd", "bash:npm"}},
	}
	for _, c := range cases {
		got, err := RulesFor("bash", bash(c.cmd))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v %v, want %v", c.cmd, got, err, c.want)
		}
	}
	for _, cmd := range []string{"git log $(rm -rf /)", "echo `id`", "  "} {
		if _, err := RulesFor("bash", bash(cmd)); err != ErrNotRememberable {
			t.Errorf("%q should not be rememberable", cmd)
		}
	}
	if got, _ := RulesFor("edit_file", nil); !reflect.DeepEqual(got, []string{"edit_file"}) {
		t.Errorf("edit_file rules = %v", got)
	}
}

func TestStorePersists(t *testing.T) {
	dir := t.TempDir()
	proj, other := t.TempDir(), t.TempDir()
	s := Load(dir)
	if err := s.Add(proj, []string{"bash:git"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(proj, []string{"web_fetch"}, true); err != nil {
		t.Fatal(err)
	}

	s = Load(dir) // a new session
	if !s.Allowed(proj, []string{"bash:git"}) {
		t.Error("git should be allowed in its project after reload")
	}
	if s.Allowed(other, []string{"bash:git"}) {
		t.Error("project rule leaked to another project")
	}
	if !s.Allowed(other, []string{"web_fetch"}) {
		t.Error("global rule should apply everywhere")
	}
	if s.Allowed(proj, []string{"bash:git", "bash:rm"}) {
		t.Error("every rule must be granted")
	}
	if err := s.Remove(proj, "bash:git"); err != nil {
		t.Fatal(err)
	}
	if Load(dir).Allowed(proj, []string{"bash:git"}) {
		t.Error("removed rule still allowed")
	}
}
