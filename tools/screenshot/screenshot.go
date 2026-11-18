// Package screenshot is the screenshot tool: it renders a web page, an
// HTML file or a Markdown file in a headless browser (or captures the
// whole screen) and hands the image to the model, so documentation work
// can be judged by how it actually looks.
package screenshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/markdown"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// Args is the tool's input.
type Args struct {
	Target string `json:"target"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Tool returns the screenshot tool.
func Tool() *tools.Tool {
	return &tools.Tool{
		Spec: llm.ToolSpec{
			Name: "screenshot",
			Description: "Take a screenshot and look at it. target is a URL (e.g. http://localhost:3000/docs), a local .md or .html file " +
				"(Markdown is rendered like GitHub would), or \"screen\" for the whole screen. Use it to see how documentation or a page " +
				"really renders before and after you change it. Needs a vision-capable model.",
			Parameters: map[string]any{"type": "object", "required": []string{"target"}, "properties": map[string]any{
				"target": map[string]any{"type": "string", "description": `URL, path to a .md/.html file, or "screen"`},
				"width":  map[string]any{"type": "integer", "description": "Viewport width in pixels (default 1280)"},
				"height": map[string]any{"type": "integer", "description": "Viewport height in pixels (default 1600; raise it to see more of a long page, max 8000)"},
			}},
		},
		NeedsApproval: true,
		Source:        "bundled",
		Describe: func(a json.RawMessage) string {
			var x Args
			_ = json.Unmarshal(a, &x)
			return x.Target
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			text, _, err := run(ctx, raw)
			return text, err
		},
		RunMedia: run,
	}
}

func run(ctx context.Context, raw json.RawMessage) (string, []llm.Image, error) {
	var a Args
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", nil, fmt.Errorf("invalid arguments: %w", err)
	}
	a.Target = strings.TrimSpace(a.Target)
	if a.Target == "" {
		return "", nil, errors.New("target is required")
	}
	if a.Width <= 0 {
		a.Width = 1280
	}
	if a.Height <= 0 {
		a.Height = 1600
	}
	a.Width, a.Height = min(max(a.Width, 320), 3840), min(max(a.Height, 240), 8000)

	dir := filepath.Join(os.TempDir(), "asg-screenshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	out := filepath.Join(dir, fmt.Sprintf("shot-%d.png", time.Now().UnixNano()))
	what := a.Target
	var err error
	if strings.EqualFold(a.Target, "screen") {
		err = captureScreen(ctx, out)
	} else {
		var u string
		u, err = pageURL(a.Target, dir)
		if err == nil {
			err = capturePage(ctx, u, out, a.Width, a.Height)
			what = fmt.Sprintf("%s at %dx%d", a.Target, a.Width, a.Height)
		}
	}
	if err != nil {
		return "", nil, err
	}
	img, w, h, err := load(out)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Screenshot of %s (%dx%d image, saved to %s). The image is attached — look at it before deciding what to change.", what, w, h, out),
		[]llm.Image{img}, nil
}

// pageURL turns a target into a URL a browser can open, rendering Markdown
// to a temporary HTML page first.
func pageURL(target, tmp string) (string, error) {
	if u, err := url.Parse(target); err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "file") {
		return target, nil
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("%s: %w (give a URL, a .md/.html file, or \"screen\")", target, err)
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".md", ".markdown":
		base := fileURL(filepath.Dir(abs)) + "/"
		page := markdown.Page(filepath.Base(abs), markdown.HTML(markdown.Parse(string(data))), base)
		html := filepath.Join(tmp, fmt.Sprintf("render-%d.html", time.Now().UnixNano()))
		if err := os.WriteFile(html, []byte(page), 0o600); err != nil {
			return "", err
		}
		return fileURL(html), nil
	}
	return fileURL(abs), nil
}

func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/x → /C:/x
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// Browser finds a Chromium-based browser for headless screenshots.
// $ASG_BROWSER overrides the search.
func Browser() (string, error) {
	if b := os.Getenv("ASG_BROWSER"); b != "" {
		return b, nil
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "msedge", "microsoft-edge", "microsoft-edge-stable", "brave-browser", "brave"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(root, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(root, `BraveSoftware\Brave-Browser\Application\brave.exe`),
				filepath.Join(root, `Chromium\Application\chrome.exe`))
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("no Chrome, Edge, Chromium or Brave found for headless screenshots — install one or set ASG_BROWSER to its path")
}

func capturePage(ctx context.Context, u, out string, w, h int) error {
	browser, err := Browser()
	if err != nil {
		return err
	}
	profile, err := os.MkdirTemp("", "asg-browser-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, browser,
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
		"--allow-file-access-from-files", "--user-data-dir="+profile, "--virtual-time-budget=4000",
		fmt.Sprintf("--window-size=%d,%d", w, h), "--screenshot="+out, u)
	msg, err := cmd.CombinedOutput()
	if _, serr := os.Stat(out); serr == nil {
		return nil // browsers often exit non-zero after writing the file
	}
	if ctx.Err() != nil {
		return fmt.Errorf("browser timed out rendering %s", u)
	}
	return fmt.Errorf("browser could not screenshot %s: %v %s", u, err, strings.TrimSpace(string(msg)))
}

func captureScreen(ctx context.Context, out string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var attempts [][]string
	switch runtime.GOOS {
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms,System.Drawing; ` +
			`$b=[System.Windows.Forms.SystemInformation]::VirtualScreen; ` +
			`$bmp=New-Object System.Drawing.Bitmap $b.Width,$b.Height; ` +
			`$g=[System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.Left,$b.Top,0,0,$bmp.Size); ` +
			`$bmp.Save($env:ASG_SHOT,[System.Drawing.Imaging.ImageFormat]::Png)`
		attempts = [][]string{{"powershell", "-NoProfile", "-NonInteractive", "-Command", ps}}
	case "darwin":
		attempts = [][]string{{"screencapture", "-x", out}}
	default:
		attempts = [][]string{{"grim", out}, {"gnome-screenshot", "-f", out}, {"spectacle", "-b", "-n", "-o", out}, {"import", "-window", "root", out}, {"scrot", out}}
	}
	var errs []string
	for _, a := range attempts {
		if _, err := exec.LookPath(a[0]); err != nil {
			continue
		}
		cmd := exec.CommandContext(ctx, a[0], a[1:]...)
		cmd.Env = append(os.Environ(), "ASG_SHOT="+out)
		msg, err := cmd.CombinedOutput()
		if _, serr := os.Stat(out); serr == nil {
			return nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v %s", a[0], err, strings.TrimSpace(string(msg))))
	}
	if len(errs) == 0 {
		return errors.New("no screen capture program found (Linux: install grim, gnome-screenshot, imagemagick or scrot)")
	}
	return errors.New("screen capture failed: " + strings.Join(errs, "; "))
}

// maxSide keeps images within what vision models accept.
const maxSide = 1800

// load reads the PNG, shrinking it when it is larger than models accept.
func load(path string) (llm.Image, int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return llm.Image{}, 0, 0, err
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return llm.Image{}, 0, 0, fmt.Errorf("read screenshot: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= 8000 && len(data) < 4<<20 {
		return llm.Image{MediaType: "image/png", Data: data}, w, h, nil
	}
	scale := min(float64(maxSide)/float64(w), 8000/float64(h), 1)
	small := shrink(src, max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, small); err != nil {
		return llm.Image{}, 0, 0, err
	}
	mt := "image/png"
	if buf.Len() >= 4<<20 {
		buf.Reset()
		if err := jpeg.Encode(&buf, small, &jpeg.Options{Quality: 80}); err != nil {
			return llm.Image{}, 0, 0, err
		}
		mt = "image/jpeg"
	}
	sb := small.Bounds()
	return llm.Image{MediaType: mt, Data: buf.Bytes()}, sb.Dx(), sb.Dy(), nil
}

// shrink scales src down to w×h by averaging the source pixels that fall
// into each destination pixel.
func shrink(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		y0, y1 := b.Min.Y+y*sh/h, b.Min.Y+max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := b.Min.X+x*sw/w, b.Min.X+max((x+1)*sw/w, x*sw/w+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			dst.Set(x, y, color.RGBA64{uint16(r / n), uint16(g / n), uint16(bl / n), uint16(a / n)})
		}
	}
	return dst
}
