package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// When ASG_TEST_SCRIPT is set the test binary acts as a script tool: it
// echoes its JSON input, or fails when asked to.
func TestMain(m *testing.M) {
	if os.Getenv("ASG_TEST_SCRIPT") == "1" {
		data, _ := io.ReadAll(os.Stdin)
		var a map[string]any
		_ = json.Unmarshal(data, &a)
		if a["fail"] == true {
			fmt.Fprintln(os.Stderr, "boom")
			os.Exit(3)
		}
		fmt.Printf("got %v from %s", a["text"], os.Getenv("ASG_TOOL_DIR"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func writeManifest(t *testing.T, dir, name, body string) string {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "tool.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestScriptToolsLoadAndRun(t *testing.T) {
	t.Setenv("ASG_TEST_SCRIPT", "1")
	exe, _ := json.Marshal(os.Args[0])
	project, user := t.TempDir(), t.TempDir()
	tdir := writeManifest(t, project, "echo", `{"name":"echo","description":"echo it","command":[`+string(exe)+`],"needs_approval":false}`)
	writeManifest(t, user, "echo", `{"name":"echo","description":"shadowed","command":["nope"]}`)
	writeManifest(t, user, "broken", `{"name":"bad name!","command":["x"]}`)
	writeManifest(t, user, "safe-by-default", `{"name":"other","command":["x"]}`)

	got, errs := LoadScriptTools(project, user, filepath.Join(user, "missing"))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "bad name!") {
		t.Errorf("want one error for the bad name, got %v", errs)
	}
	if len(got) != 2 || got[0].Spec.Name != "echo" || got[0].Spec.Description != "echo it" {
		t.Fatalf("project tool should win: %+v", got)
	}
	if got[0].NeedsApproval || !got[1].NeedsApproval {
		t.Error("needs_approval should default to true and honour false")
	}
	out, err := got[0].Run(context.Background(), json.RawMessage(`{"text":"hi"}`))
	abs, _ := filepath.Abs(tdir)
	if err != nil || out != "got hi from "+abs {
		t.Errorf("run: %q %v", out, err)
	}
	if _, err := got[0].Run(context.Background(), json.RawMessage(`{"fail":true}`)); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a failing script should report stderr, got %v", err)
	}
}

func TestScaffoldScriptTool(t *testing.T) {
	dir := t.TempDir()
	d, err := ScaffoldScriptTool(dir, "my_tool")
	if err != nil {
		t.Fatal(err)
	}
	tool, err := LoadScriptTool(d)
	if err != nil || tool.Spec.Name != "my_tool" {
		t.Fatalf("scaffold should load: %v", err)
	}
	if _, err := ScaffoldScriptTool(dir, "my_tool"); err == nil {
		t.Error("scaffolding over an existing tool should fail")
	}
	if _, err := ScaffoldScriptTool(dir, "../evil"); err == nil {
		t.Error("names with path parts must be rejected")
	}
}
