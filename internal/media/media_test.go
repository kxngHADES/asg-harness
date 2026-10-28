package media

import (
	"os"
	"path/filepath"
	"testing"
)

// 1x1 transparent PNG.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func TestExtractDraggedPaths(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "my shot.png")
	os.WriteFile(p, png, 0o644)
	escaped := filepath.Join(dir, `my\ shot.png`)
	for _, in := range []string{"what is " + escaped, `what is "` + p + `"`, "what is @'" + p + "'"} {
		text, imgs, names := ExtractImages(in)
		if len(imgs) != 1 || text != "what is" || names[0] != "my shot.png" || imgs[0].MediaType != "image/png" {
			t.Errorf("ExtractImages(%q) = %q %d %v", in, text, len(imgs), names)
		}
	}
	if text, imgs, _ := ExtractImages("no images here.png"); len(imgs) != 0 || text != "no images here.png" {
		t.Error("non-existent path should be left alone")
	}
}
