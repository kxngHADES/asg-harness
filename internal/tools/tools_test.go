package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobToRegexp(t *testing.T) {
	re, _ := globToRegexp("**/*.{go,md}")
	for _, p := range []string{"main.go", "a/b/c.md"} {
		if !re.MatchString(p) {
			t.Errorf("%s should match", p)
		}
	}
	if re.MatchString("a/b.txt") {
		t.Error("b.txt should not match")
	}
}

// Small local models often quote numbers and booleans.
func TestQuotedArgs(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	os.WriteFile(f, []byte("x\nx\nline3\n"), 0o644)

	out, err := readTool().Run(context.Background(), json.RawMessage(`{"path":"`+f+`","offset":"3","limit":"1"}`))
	if err != nil || !strings.Contains(out, "line3") || strings.Contains(out, "\tx") {
		t.Fatalf("read: %q %v", out, err)
	}
	_, err = editTool().Run(context.Background(), json.RawMessage(`{"path":"`+f+`","old_string":"x","new_string":"y","replace_all":"true"}`))
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(f); string(data) != "y\ny\nline3\n" {
		t.Fatalf("edit result %q", data)
	}
	if _, err := bashTool().Run(context.Background(), json.RawMessage(`{"command":"echo hi","timeout":"5"}`)); err != nil {
		t.Fatalf("bash with quoted timeout: %v", err)
	}
}

func TestEditRequiresUnique(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(f, []byte("a a"), 0o644)
	if _, err := editTool().Run(context.Background(), json.RawMessage(`{"path":"`+f+`","old_string":"a","new_string":"b"}`)); err == nil {
		t.Fatal("expected ambiguity error")
	}
}
