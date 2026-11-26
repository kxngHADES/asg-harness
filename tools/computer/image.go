package computer

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"

	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/tools/screenshot"
)

// Rect is a rectangle in screen coordinates (the space the OS uses for
// mouse input).
type Rect struct{ X, Y, W, H int }

func (r Rect) contains(x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

func (r Rect) intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X+r.W, o.X+o.W), min(r.Y+r.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

func (r Rect) empty() bool { return r.W <= 0 || r.H <= 0 }

// union is the bounding box of rects.
func union(rs []Rect) Rect {
	if len(rs) == 0 {
		return Rect{}
	}
	x0, y0, x1, y1 := rs[0].X, rs[0].Y, rs[0].X+rs[0].W, rs[0].Y+rs[0].H
	for _, r := range rs[1:] {
		x0, y0 = min(x0, r.X), min(y0, r.Y)
		x1, y1 = max(x1, r.X+r.W), max(y1, r.Y+r.H)
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// view maps between a screenshot the model sees and the screen: the model
// gives coordinates in the image, and they are scaled onto rect.
type view struct {
	Monitor    int // -1 is the whole desktop
	Rect       Rect
	ImgW, ImgH int
}

// newView sizes the image for rect so its longer side is at most maxSide.
// The size depends only on rect, so coordinates mean the same thing before
// and after a screenshot.
func newView(mon int, r Rect, maxSide int) view {
	scale := math.Min(1, float64(maxSide)/float64(max(r.W, r.H)))
	return view{Monitor: mon, Rect: r, ImgW: max(int(math.Round(float64(r.W)*scale)), 1), ImgH: max(int(math.Round(float64(r.H)*scale)), 1)}
}

// toScreen maps an image point to the screen.
func (v view) toScreen(x, y int) (int, int) {
	sx := v.Rect.X + int(math.Round(float64(x)*float64(v.Rect.W)/float64(v.ImgW)))
	sy := v.Rect.Y + int(math.Round(float64(y)*float64(v.Rect.H)/float64(v.ImgH)))
	return min(max(sx, v.Rect.X), v.Rect.X+v.Rect.W-1), min(max(sy, v.Rect.Y), v.Rect.Y+v.Rect.H-1)
}

// toImage maps a screen point into the image; ok is false when it is outside.
func (v view) toImage(sx, sy int) (x, y int, ok bool) {
	if !v.Rect.contains(sx, sy) {
		return 0, 0, false
	}
	x = int(float64(sx-v.Rect.X) * float64(v.ImgW) / float64(v.Rect.W))
	y = int(float64(sy-v.Rect.Y) * float64(v.ImgH) / float64(v.Rect.H))
	return x, y, true
}

// rectToImage maps a screen rectangle into image pixels.
func (v view) rectToImage(r Rect) image.Rectangle {
	r = r.intersect(v.Rect)
	if r.empty() {
		return image.Rectangle{}
	}
	sx, sy := float64(v.ImgW)/float64(v.Rect.W), float64(v.ImgH)/float64(v.Rect.H)
	return image.Rect(int(float64(r.X-v.Rect.X)*sx), int(float64(r.Y-v.Rect.Y)*sy),
		int(math.Ceil(float64(r.X-v.Rect.X+r.W)*sx)), int(math.Ceil(float64(r.Y-v.Rect.Y+r.H)*sy)))
}

// fitImage scales a capture to the view's size. Captures can be larger than
// the rect (HiDPI screens report points but capture pixels).
func fitImage(src image.Image, v view) *image.RGBA {
	b := src.Bounds()
	if b.Dx() == v.ImgW && b.Dy() == v.ImgH {
		if rgba, ok := src.(*image.RGBA); ok && b.Min == (image.Point{}) {
			return rgba
		}
		dst := image.NewRGBA(image.Rect(0, 0, v.ImgW, v.ImgH))
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	return screenshot.Shrink(src, v.ImgW, v.ImgH)
}

// blackout paints rectangles (excluded windows or monitors) black and labels
// nothing: the model only learns that something there is hidden.
func blackout(img *image.RGBA, rs []image.Rectangle) {
	for _, r := range rs {
		draw.Draw(img, r.Intersect(img.Bounds()), &image.Uniform{color.RGBA{20, 20, 20, 255}}, image.Point{}, draw.Src)
	}
}

// drawCursor marks the mouse position so the model can see where it is.
func drawCursor(img *image.RGBA, x, y int) {
	ring := color.RGBA{255, 64, 160, 255}
	for dy := -7; dy <= 7; dy++ {
		for dx := -7; dx <= 7; dx++ {
			d := dx*dx + dy*dy
			if d >= 30 && d <= 49 {
				img.Set(x+dx, y+dy, ring)
			}
		}
	}
	img.Set(x, y, ring)
}

// thumbSize is the grid used to tell whether the screen changed.
const thumbW, thumbH = 96, 54

// thumb is a tiny grayscale copy for change detection.
func thumb(img image.Image) []uint8 {
	small := screenshot.Shrink(img, thumbW, thumbH)
	out := make([]uint8, thumbW*thumbH)
	for y := 0; y < thumbH; y++ {
		for x := 0; x < thumbW; x++ {
			c := small.RGBAAt(x, y)
			out[y*thumbW+x] = uint8((299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000)
		}
	}
	return out
}

// changed returns the share of the screen (0–1) that differs between two thumbs.
func changed(a, b []uint8) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return -1
	}
	n := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d > 12 || d < -12 {
			n++
		}
	}
	return float64(n) / float64(len(a))
}

// encode turns the image into something every provider accepts.
func encode(img image.Image) (llm.Image, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return llm.Image{}, err
	}
	if buf.Len() < 3<<20 {
		return llm.Image{MediaType: "image/png", Data: buf.Bytes()}, nil
	}
	buf.Reset()
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return llm.Image{}, err
	}
	return llm.Image{MediaType: "image/jpeg", Data: buf.Bytes()}, nil
}

// imageRect is a rectangle in screenshot pixels.
type imageRect = image.Rectangle
