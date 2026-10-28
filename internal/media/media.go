// Package media handles image attachments: loading files, detecting pasted
// or dragged-in image paths, and reading images from the system clipboard.
package media

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/llm"
)

var imageExt = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp",
}

const maxImage = 20 << 20

// LoadImage reads an image file from disk.
func LoadImage(path string) (llm.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return llm.Image{}, err
	}
	return FromBytes(data)
}

// FromBytes sniffs the media type of raw image data.
func FromBytes(data []byte) (llm.Image, error) {
	if len(data) > maxImage {
		return llm.Image{}, fmt.Errorf("image too large (%d MB)", len(data)>>20)
	}
	mt := http.DetectContentType(data)
	if !strings.HasPrefix(mt, "image/") {
		return llm.Image{}, fmt.Errorf("not an image (%s)", mt)
	}
	return llm.Image{MediaType: mt, Data: data}, nil
}

// IsImagePath reports whether p looks like an existing image file.
func IsImagePath(p string) bool {
	if _, ok := imageExt[strings.ToLower(filepath.Ext(p))]; !ok {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// ExtractImages finds image paths in user input (as produced by dragging a
// file into the terminal or typing @path) and returns the remaining text and
// the loaded images. Paths may be quoted or have backslash-escaped spaces.
func ExtractImages(input string) (string, []llm.Image, []string) {
	var images []llm.Image
	var names []string
	var kept []string
	for _, tok := range splitShell(input) {
		p := strings.TrimPrefix(tok, "@")
		p = strings.TrimPrefix(p, "file://")
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[2:])
			}
		}
		if IsImagePath(p) {
			if img, err := LoadImage(p); err == nil {
				images = append(images, img)
				names = append(names, filepath.Base(p))
				continue
			}
		}
		kept = append(kept, tok)
	}
	if len(images) == 0 {
		return input, nil, nil
	}
	return strings.Join(kept, " "), images, names
}

// splitShell splits on whitespace, honouring quotes and backslash escapes.
func splitShell(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	inTok := false
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\\' && i+1 < len(runes) && runtime.GOOS != "windows":
			i++
			cur.WriteRune(runes[i])
			inTok = true
		case r == '"' || r == '\'':
			quote = r
			inTok = true
		case r == ' ' || r == '\t' || r == '\n':
			if inTok {
				out = append(out, cur.String())
				cur.Reset()
				inTok = false
			}
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	if inTok {
		out = append(out, cur.String())
	}
	return out
}

// ClipboardImage reads an image from the system clipboard.
func ClipboardImage() (llm.Image, error) {
	tmp, err := os.CreateTemp("", "asg-clip-*.png")
	if err != nil {
		return llm.Image{}, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	var data []byte
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("pngpaste"); err == nil {
			if exec.Command("pngpaste", tmp.Name()).Run() == nil {
				data, _ = os.ReadFile(tmp.Name())
			}
		}
		if len(data) == 0 {
			script := fmt.Sprintf(`try
	set png to (the clipboard as «class PNGf»)
on error
	return "none"
end try
set f to open for access POSIX file %q with write permission
set eof f to 0
write png to f
close access f
return "ok"`, tmp.Name())
			out, err := exec.Command("osascript", "-e", script).Output()
			if err != nil || strings.TrimSpace(string(out)) != "ok" {
				return llm.Image{}, fmt.Errorf("no image on the clipboard")
			}
			data, _ = os.ReadFile(tmp.Name())
		}
	case "windows":
		ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; $i=[Windows.Forms.Clipboard]::GetImage(); if ($i) { $i.Save('%s') }`, tmp.Name())
		_ = exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps).Run()
		data, _ = os.ReadFile(tmp.Name())
	default:
		for _, c := range [][]string{{"wl-paste", "--type", "image/png"}, {"xclip", "-selection", "clipboard", "-t", "image/png", "-o"}} {
			if _, err := exec.LookPath(c[0]); err != nil {
				continue
			}
			if out, err := exec.Command(c[0], c[1:]...).Output(); err == nil && len(out) > 0 {
				data = out
				break
			}
		}
	}
	if len(data) == 0 {
		return llm.Image{}, fmt.Errorf("no image on the clipboard")
	}
	return FromBytes(data)
}
