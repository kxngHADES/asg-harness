package runtimes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUVTarget(t *testing.T) {
	cases := map[string]string{
		"windows/amd64": "x86_64-pc-windows-msvc.zip",
		"windows/arm64": "aarch64-pc-windows-msvc.zip",
		"darwin/arm64":  "aarch64-apple-darwin.tar.gz",
		"linux/amd64":   "x86_64-unknown-linux-musl.tar.gz",
	}
	for in, want := range cases {
		osys, arch, _ := strings.Cut(in, "/")
		target, ext, err := uvTarget(osys, arch)
		if err != nil || target+ext != want {
			t.Errorf("%s: got %s%s %v, want %s", in, target, ext, err, want)
		}
	}
	if _, _, err := uvTarget("plan9", "386"); err == nil {
		t.Error("unsupported platforms should fail")
	}
}

func TestPickGo(t *testing.T) {
	rel := []goRelease{{Version: "go9.9rc1", Stable: false}, {Version: "go1.30.1", Stable: true}}
	rel[0].Files = append(rel[0].Files, struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		SHA256   string `json:"sha256"`
		Kind     string `json:"kind"`
	}{"go9.9rc1.linux-amd64.tar.gz", "linux", "amd64", "aa", "archive"})
	rel[1].Files = append(rel[1].Files, rel[0].Files[0], rel[0].Files[0])
	rel[1].Files[0].Filename, rel[1].Files[0].Kind = "go1.30.1.linux-amd64.pkg", "installer"
	rel[1].Files[1].Filename = "go1.30.1.linux-amd64.tar.gz"
	file, _, v, err := pickGo(rel, "linux", "amd64")
	if err != nil || file != "go1.30.1.linux-amd64.tar.gz" || v != "go1.30.1" {
		t.Errorf("got %s %s %v", file, v, err)
	}
	if _, _, _, err := pickGo(rel, "windows", "amd64"); err == nil {
		t.Error("missing platform should fail")
	}
}

func TestNeedsBootstrap(t *testing.T) {
	t.Setenv("ASG_UV", "")
	m := New(t.TempDir())
	if !m.NeedsBootstrap("v1") {
		t.Fatal("fresh install needs a bootstrap")
	}
	// Pretend uv and a Python are installed and v1 was bootstrapped.
	for _, p := range []string{m.UVPath(), filepath.Join(m.PythonDir(), "cpython-3.12", "python")} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o755)
	}
	_ = os.WriteFile(m.statePath(), []byte(`{"bootstrapped_version":"v1"}`), 0o644)
	if m.NeedsBootstrap("v1") {
		t.Error("same version with everything installed needs nothing")
	}
	if !m.NeedsBootstrap("v2") {
		t.Error("an app update should bootstrap again")
	}
	_ = os.Remove(m.UVPath())
	if !m.NeedsBootstrap("v1") {
		t.Error("a missing uv should bootstrap again")
	}
}

func TestEnvAndPaths(t *testing.T) {
	m := New(t.TempDir())
	t.Setenv("VIRTUAL_ENV", "/somewhere")
	env := strings.Join(m.Env(), "\n")
	if strings.Contains(env, "VIRTUAL_ENV=") {
		t.Error("an activated virtualenv must not leak into tools")
	}
	if !strings.Contains(env, "UV_PYTHON_INSTALL_DIR="+m.PythonDir()) || !strings.Contains(env, "UV_PYTHON_PREFERENCE=only-managed") {
		t.Error("uv must use the managed Python folder")
	}
	if m.EnvDir("t", "/a") == m.EnvDir("t", "/b") {
		t.Error("tools in different folders must not share an environment")
	}
	got := prependPath([]string{"A=1", "Path=x"}, "gobin")
	if got[1] != "Path=gobin"+string(os.PathListSeparator)+"x" {
		t.Errorf("prependPath: %v", got)
	}
}
