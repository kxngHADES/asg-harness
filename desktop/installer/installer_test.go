package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/config"
)

type entry struct {
	name, body string
	dir        bool
	mode       int64
}

func makeArchive(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.dir {
			h.Typeflag, h.Size = tar.TypeDir, 0
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !e.dir {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractAndArchiveInfo(t *testing.T) {
	data := makeArchive(t, []entry{
		{name: "ASG.app/", dir: true},
		{name: "ASG.app/Contents/MacOS/asg-desktop", body: "binary", mode: 0o755},
		{name: "ASG.app/Contents/Info.plist", body: "<plist/>"},
		{name: "asg-desktop.png", body: "png"},
	})
	size, top, err := archiveInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len("binary")+len("<plist/>")+len("png")) {
		t.Errorf("size = %d", size)
	}
	if len(top) != 2 || top[0] != "ASG.app" || top[1] != "asg-desktop.png" {
		t.Errorf("top = %v", top)
	}

	dir := t.TempDir()
	var last int64
	if err := extract(data, dir, func(n int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	if last != size {
		t.Errorf("progress ended at %d, want %d", last, size)
	}
	bin := filepath.Join(dir, "ASG.app", "Contents", "MacOS", "asg-desktop")
	got, err := os.ReadFile(bin)
	if err != nil || string(got) != "binary" {
		t.Fatalf("read %s: %q %v", bin, got, err)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(bin); st.Mode().Perm()&0o100 == 0 {
			t.Errorf("executable bit lost: %v", st.Mode())
		}
	}
	// Extracting again replaces files in place (an update).
	if err := extract(data, dir, nil); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.asg-new")); len(matches) > 0 {
		t.Errorf("temporary files left behind: %v", matches)
	}
}

func TestExtractRefusesEscapes(t *testing.T) {
	for _, name := range []string{"../evil", "a/../../evil", "/etc/evil", `..\evil`} {
		data := makeArchive(t, []entry{{name: name, body: "x"}})
		if err := extract(data, t.TempDir(), nil); err == nil {
			t.Errorf("%q: extracted, want an error", name)
		}
	}
}

func TestApplyProviders(t *testing.T) {
	c := config.Default()
	c.Providers["anthropic"].APIKey = "sk-old"
	in := []ProviderInput{
		{Name: "ollama", Type: "ollama", BaseURL: "http://localhost:11434/", Model: "qwen3:14b", Enabled: true},
		{Name: "anthropic", Type: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-5-5", Enabled: true},
		{Name: "openai", Type: "openai", Enabled: false},
		{Name: "groq", Type: "openai", BaseURL: "https://api.groq.com/openai/v1", Model: "llama", APIKey: " gk ", Enabled: true},
	}
	if err := applyProviders(c, in, "anthropic"); err != nil {
		t.Fatal(err)
	}
	if c.Providers["openai"] != nil {
		t.Error("switched-off provider was kept")
	}
	if got := c.Providers["anthropic"].APIKey; got != "sk-old" {
		t.Errorf("empty key replaced the saved one: %q", got)
	}
	if got := c.Providers["groq"].APIKey; got != "gk" {
		t.Errorf("groq key = %q", got)
	}
	if got := c.Providers["ollama"].BaseURL; got != "http://localhost:11434" {
		t.Errorf("trailing slash kept: %q", got)
	}
	if c.ActiveProvider != "anthropic" {
		t.Errorf("active = %s", c.ActiveProvider)
	}

	// The active provider falls back to the first enabled one.
	c2 := config.Default()
	if err := applyProviders(c2, in, "openai"); err != nil {
		t.Fatal(err)
	}
	if c2.ActiveProvider != "ollama" {
		t.Errorf("fallback active = %s", c2.ActiveProvider)
	}
}

func TestApplyProvidersRejects(t *testing.T) {
	cases := map[string][]ProviderInput{
		"none on":  {{Name: "ollama", Type: "ollama", Enabled: false}},
		"bad name": {{Name: "My Provider", Type: "openai", BaseURL: "https://x", Model: "m", Enabled: true}},
		"bad type": {{Name: "x", Type: "gemini", BaseURL: "https://x", Model: "m", Enabled: true}},
		"bad url":  {{Name: "x", Type: "openai", BaseURL: "api.example.com", Model: "m", Enabled: true}},
		"no model": {{Name: "x", Type: "openai", BaseURL: "https://x", Enabled: true}},
	}
	for name, in := range cases {
		if err := applyProviders(config.Default(), in, ""); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestProviderInputsFreshMachine(t *testing.T) {
	got := providerInputs(*config.Default(), true)
	if len(got) != 3 {
		t.Fatalf("got %d providers", len(got))
	}
	for _, p := range got {
		if p.Enabled != (p.Name == "ollama") {
			t.Errorf("%s enabled = %v on a fresh machine", p.Name, p.Enabled)
		}
	}

	c := config.Default()
	c.Providers["groq"] = &config.ProviderConfig{Type: "openai", BaseURL: "https://api.groq.com/openai/v1", Model: "m", APIKey: "k"}
	delete(c.Providers, "openai")
	got = providerInputs(*c, false)
	byName := map[string]ProviderInput{}
	for _, p := range got {
		byName[p.Name] = p
	}
	if !byName["groq"].Enabled || !byName["groq"].HasKey {
		t.Errorf("custom provider: %+v", byName["groq"])
	}
	if byName["openai"].Enabled {
		t.Error("a provider missing from the config shows as on")
	}
}

func TestCheckPaths(t *testing.T) {
	dir := t.TempDir()
	ok := Paths{InstallDir: filepath.Join(dir, "ASG"), DataDir: filepath.Join(dir, "data", "new"), MemoryDir: dir}
	if bad := checkPaths(ok); len(bad) > 0 {
		t.Fatalf("good paths rejected: %v", bad)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	bad := checkPaths(Paths{InstallDir: file, DataDir: "relative/dir", MemoryDir: ""})
	for _, k := range []string{"install_dir", "data_dir", "memory_dir"} {
		if bad[k] == "" {
			t.Errorf("%s accepted", k)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("checking left files behind: %v", entries)
	}
}

func TestWithinAndSafeToDelete(t *testing.T) {
	root := t.TempDir()
	if !within(root, filepath.Join(root, "a", "b")) || within(root, filepath.Dir(root)) || within(root, root+"x") {
		t.Error("within is wrong")
	}
	home, _ := os.UserHomeDir()
	if safeToDelete(home) || safeToDelete(string(filepath.Separator)) || safeToDelete("relative") {
		t.Error("safeToDelete allows a dangerous target")
	}
	if !safeToDelete(filepath.Join(root, "data")) {
		t.Error("safeToDelete refuses a normal folder")
	}
}

func TestRecordAndDesktopSettingsRoundTrip(t *testing.T) {
	t.Setenv("ASG_HOME", t.TempDir())
	if loadRecord() != nil {
		t.Fatal("record exists before install")
	}
	r := &Record{Version: "v1.0.0", InstallDir: "/opt/asg", Files: []string{"/opt/asg/asg-desktop"}}
	if err := saveRecord(r); err != nil {
		t.Fatal(err)
	}
	if got := loadRecord(); got == nil || got.InstallDir != "/opt/asg" || len(got.Files) != 1 {
		t.Fatalf("loadRecord = %+v", got)
	}

	if err := os.WriteFile(filepath.Join(config.Dir(), "desktop.json"), []byte(`{"theme":"dark","future":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeDesktopSettings(map[string]any{"data_dir": "/data"}); err != nil {
		t.Fatal(err)
	}
	m := readDesktopSettings()
	if m["data_dir"] != "/data" || m["theme"] != "dark" || m["future"] == nil {
		t.Errorf("desktop.json merge lost keys: %v", m)
	}
}
