package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.3.0", "v0.2.0", true},
		{"v0.2.1", "0.2.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.1.9", "v0.2.0", false},
		{"v0.3.0", "dev", false},
		{"v0.3.0-rc1", "v0.2.0", true},
		{"garbage", "v0.2.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestDesktopAsset(t *testing.T) {
	for _, c := range [][3]string{
		{"windows", "amd64", "asg-desktop_windows_amd64.zip"},
		{"windows", "arm64", "asg-desktop_windows_arm64.zip"},
		{"darwin", "arm64", "asg-desktop_darwin_universal.zip"},
		{"linux", "amd64", "asg-desktop_linux_amd64.tar.gz"},
	} {
		if got := desktopAsset(c[0], c[1]); got != c[2] {
			t.Errorf("desktopAsset(%s, %s) = %s, want %s", c[0], c[1], got, c[2])
		}
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestFileFromArchives(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	writeZip(t, zp, map[string]string{"asg-desktop.exe": "new-exe", "README": "x"})
	if b, err := fileFromZip(zp, "asg-desktop.exe"); err != nil || string(b) != "new-exe" {
		t.Fatalf("zip: %q %v", b, err)
	}
	if _, err := fileFromZip(zp, "missing"); err == nil {
		t.Fatal("expected an error for a missing file")
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("new-bin")
	tw.WriteHeader(&tar.Header{Name: "asg-desktop_linux_amd64/", Typeflag: tar.TypeDir, Mode: 0o755})
	tw.WriteHeader(&tar.Header{Name: "asg-desktop_linux_amd64/asg-desktop", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))})
	tw.Write(body)
	tw.Close()
	gz.Close()
	tp := filepath.Join(dir, "a.tar.gz")
	os.WriteFile(tp, buf.Bytes(), 0o644)
	if b, err := fileFromTarGz(tp, "asg-desktop"); err != nil || string(b) != "new-bin" {
		t.Fatalf("tar: %q %v", b, err)
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "asg-desktop.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := replaceFile(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("got %q", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal(".new left behind")
	}
}

func TestUnzipAllRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "evil.zip")
	writeZip(t, zp, map[string]string{"../outside.txt": "x"})
	if err := unzipAll(zp, filepath.Join(dir, "out")); err == nil {
		t.Fatal("expected unzipAll to refuse a path outside the folder")
	}

	ok := filepath.Join(dir, "ok.zip")
	writeZip(t, ok, map[string]string{"ASG.app/Contents/MacOS/asg-desktop": "bin", "ASG.app/Contents/Info.plist": "plist"})
	out := filepath.Join(dir, "ok")
	if err := unzipAll(ok, out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "ASG.app", "Contents", "MacOS", "asg-desktop")); string(b) != "bin" {
		t.Fatalf("got %q", b)
	}
}

func TestTrayIcon(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(trayIconPNG(64)))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Fatalf("size %v", b)
	}
	ico := trayIcon("windows")
	var hdr [3]uint16
	binary.Read(bytes.NewReader(ico), binary.LittleEndian, &hdr)
	if hdr[0] != 0 || hdr[1] != 1 || hdr[2] != 3 {
		t.Fatalf("bad ico header %v", hdr)
	}
	// First entry's image must be a PNG at the offset it claims.
	off := binary.LittleEndian.Uint32(ico[6+12 : 6+16])
	if !bytes.HasPrefix(ico[off:], []byte("\x89PNG")) {
		t.Fatal("ico entry is not a PNG")
	}
}
