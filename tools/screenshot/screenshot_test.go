package screenshot

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownTargetIsRenderedToHTML(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "guide.md")
	if err := os.WriteFile(md, []byte("# Guide\n\n- step\n\n![x](shot.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := pageURL(md, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "file:///") || !strings.HasSuffix(u, ".html") {
		t.Fatalf("want a file URL to rendered HTML, got %s", u)
	}
	html := filepath.FromSlash(strings.TrimPrefix(u, "file://"))
	if len(html) > 2 && html[0] == filepath.Separator && html[2] == ':' {
		html = html[1:] // \C:\… → C:\…
	}
	data, err := os.ReadFile(html)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<h1>Guide</h1>") || !strings.Contains(string(data), "<base href=\"file:///") {
		t.Errorf("rendered page missing content or base:\n%s", data)
	}
	if u, _ := pageURL("http://localhost:3000/docs", ""); u != "http://localhost:3000/docs" {
		t.Errorf("URLs pass through, got %s", u)
	}
	if _, err := pageURL(filepath.Join(dir, "missing.md"), ""); err == nil {
		t.Error("a missing file should be an error")
	}
}

func TestLargeScreenshotsAreShrunk(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3600, 200))
	for x := 0; x < 3600; x++ {
		src.Set(x, 0, color.RGBA{255, 0, 0, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	img, w, h, err := load(p)
	if err != nil {
		t.Fatal(err)
	}
	if w != maxSide || h != 100 || img.MediaType != "image/png" {
		t.Errorf("want %dx100 png, got %dx%d %s", maxSide, w, h, img.MediaType)
	}
}
