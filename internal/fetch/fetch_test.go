package fetch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestParseGitHub(t *testing.T) {
	cases := []struct{ in, owner, repo, ref, sub string }{
		{"github:me/tools/py/x@v1", "me", "tools", "v1", "py/x"},
		{"me/tools", "me", "tools", "", ""},
		{"https://github.com/me/tools/tree/main/go", "me", "tools", "main", "go"},
	}
	for _, c := range cases {
		o, r, ref, sub, ok := ParseGitHub(c.in)
		if !ok || o != c.owner || r != c.repo || ref != c.ref || sub != c.sub {
			t.Errorf("ParseGitHub(%q) = %q %q %q %q %v", c.in, o, r, ref, sub, ok)
		}
	}
}

func TestExtractTarGzBlocksTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		mode int64
	}{{"ok/run", 0o755}, {"../escape", 0o644}, {"ok/data.txt", 0o644}} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: 2, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("hi"))
	}
	tw.Close()
	gz.Close()

	root := t.TempDir()
	dest := filepath.Join(root, "dest")
	if err := ExtractTarGz(&buf, dest, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
		t.Error("an entry escaped the destination")
	}
	if _, err := os.Stat(filepath.Join(dest, "ok", "data.txt")); err != nil {
		t.Error(err)
	}
}

func TestCopyDirSkipsEnvironments(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for _, p := range []string{"main.py", ".venv/lib/x", "__pycache__/a.pyc", "pkg/mod.py"} {
		full := filepath.Join(src, filepath.FromSlash(p))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("x"), 0o644)
	}
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]bool{"main.py": true, "pkg/mod.py": true, ".venv": false, "__pycache__": false} {
		_, err := os.Stat(filepath.Join(dst, filepath.FromSlash(p)))
		if (err == nil) != want {
			t.Errorf("%s copied=%v, want %v", p, err == nil, want)
		}
	}
}
