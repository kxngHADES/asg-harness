package agent

import (
	"reflect"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/tools"
)

func TestParseSteps(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"numbered with prose", "Sure! Plan:\n\n1. Add the flag in `main.go`\n2) Write a test\n\nRisks: none.", []string{"Add the flag in `main.go`", "Write a test"}},
		{"nested detail", "1. **Edit config**\n   - add field Loop\n   - default 30\n2. Wire it up", []string{"Edit config — add field Loop; default 30", "Wire it up"}},
		{"nested numbers are detail", "1. Outer\n   1. inner a\n   2. inner b\n2. Second", []string{"Outer — inner a; inner b", "Second"}},
		{"checklist", "- [ ] first\n- [x] second", []string{"first", "second"}},
		{"step labels", "Step 1: look\nStep 2: change", []string{"look", "change"}},
		{"prose only", "I think we should just change the file.", nil},
		{"leaked tool calls are not steps", "1. list_dir{path:<|\"|>.<|\"|>}\n2. read_file{path:\"a.py\"}\n3. Add subtract() to calc.py", []string{"Add subtract() to calc.py"}},
		{"braces in a real step are fine", "1. Return {} from handler() in api.go when empty", []string{"Return {} from handler() in api.go when empty"}},
	}
	for _, c := range cases {
		if got := ParseSteps(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestRecoverToolCalls(t *testing.T) {
	byName := map[string]*tools.Tool{"read_file": {}, "bash": {}}
	cases := []struct {
		name, in, tool, args string
	}{
		{"qwen tag", "Let me look.\n<tool_call>\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}\n</tool_call>", "read_file", `{"path": "a.go"}`},
		{"fenced", "```json\n{\"name\":\"bash\",\"parameters\":{\"command\":\"ls\"}}\n```", "bash", `{"command":"ls"}`},
		{"bare openai shape", `{"function":{"name":"bash","arguments":"{\"command\":\"pwd\"}"}}`, "bash", `{"command":"pwd"}`},
		{"gemma syntax", "call:read_file{path:<|\"|>calc.py<|\"|>,limit:5}", "read_file", `{"path":"calc.py","limit":5}`},
		{"unknown tool", `{"name":"rm_rf","arguments":{}}`, "", ""},
		{"json inside prose is ignored", "You can call it like {\"name\":\"bash\",\"arguments\":{}} if you want.", "", ""},
	}
	for _, c := range cases {
		got := recoverToolCalls(c.in, byName)
		if c.tool == "" {
			if len(got) != 0 {
				t.Errorf("%s: expected nothing, got %+v", c.name, got)
			}
			continue
		}
		if len(got) != 1 || got[0].Name != c.tool || canonicalArgs(got[0].Args) != canonicalArgs([]byte(c.args)) {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}

func TestCanonicalArgs(t *testing.T) {
	if canonicalArgs([]byte(`{"b":1, "a":"x"}`)) != canonicalArgs([]byte(`{"a":"x","b":1}`)) {
		t.Error("key order should not matter")
	}
}

func TestModes(t *testing.T) {
	m := ModeBuild
	seen := map[Mode]bool{}
	for range Modes {
		seen[m] = true
		m = m.Next()
	}
	if m != ModeBuild || len(seen) != len(Modes) {
		t.Errorf("Next should cycle through every mode and back, got %v", seen)
	}
	if Mode("").Next() != ModePlan {
		t.Error("the zero mode is build")
	}
	reg := tools.Builtins()
	for _, tl := range reg.All() {
		if ModePlan.allows(tl) == tl.NeedsApproval && tl.Spec.Name != "web_fetch" {
			t.Errorf("plan mode allows(%s) = %v", tl.Spec.Name, ModePlan.allows(tl))
		}
		if !ModeBuild.allows(tl) {
			t.Errorf("build mode must allow %s", tl.Spec.Name)
		}
	}
	if p, ok := ParseMode("Auto"); !ok || p != ModeLoop {
		t.Error("alias auto → loop")
	}
}

func TestBlockedQuestion(t *testing.T) {
	if q := blockedQuestion("did some work\n**BLOCKED:** need the API key"); q != "need the API key" {
		t.Errorf("got %q", q)
	}
	if q := blockedQuestion("all good, nothing blocked here"); q != "" {
		t.Errorf("got %q", q)
	}
}

func TestDetectVerify(t *testing.T) {
	dir := t.TempDir()
	if got := DetectVerify(dir); got != "" {
		t.Errorf("empty dir: %q", got)
	}
	write := func(name, body string) {
		if err := osWrite(dir, name, body); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1","build":"tsc"}}`)
	if got := DetectVerify(dir); got != "npm run build" {
		t.Errorf("placeholder npm test should be skipped, got %q", got)
	}
	write("go.mod", "module x")
	if got := DetectVerify(dir); got != "go build ./... && go test ./..." {
		t.Errorf("go: %q", got)
	}
}
