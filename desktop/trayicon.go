package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"

	"github.com/kxngHADES/asg-harness/internal/sprite"
)

// trayIconPNG draws the crab, centred on a transparent size×size square
// (size should be at least sprite.W).
func trayIconPNG(size int) []byte {
	g := sprite.Compose(sprite.Idle[0])
	scale := size / sprite.W
	if scale < 1 {
		scale = 1
	}
	ox, oy := (size-sprite.W*scale)/2, (size-sprite.H*scale)/2
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < sprite.H; y++ {
		for x := 0; x < sprite.W; x++ {
			c, ok := sprite.Palette[g[y][x]]
			if g[y][x] == 0 || !ok {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetNRGBA(ox+x*scale+dx, oy+y*scale+dy, color.NRGBA{c.R, c.G, c.B, 255})
				}
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// icoFromPNGs wraps PNG images (each at most 256 px) in an .ico container,
// which Windows needs for the tray icon.
func icoFromPNGs(sizes []int, pngs [][]byte) []byte {
	var b bytes.Buffer
	n := len(pngs)
	_ = binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(n)})
	offset := uint32(6 + 16*n)
	for i, p := range pngs {
		dim := byte(sizes[i])
		if sizes[i] >= 256 {
			dim = 0
		}
		b.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&b, binary.LittleEndian, [2]uint16{1, 32})
		_ = binary.Write(&b, binary.LittleEndian, [2]uint32{uint32(len(p)), offset})
		offset += uint32(len(p))
	}
	for _, p := range pngs {
		b.Write(p)
	}
	return b.Bytes()
}

// trayIcon is the icon in the format the platform's tray wants.
func trayIcon(goos string) []byte {
	if goos == "windows" {
		sizes := []int{20, 40, 64} // the crab is 20 px wide; Windows scales to fit
		pngs := make([][]byte, len(sizes))
		for i, s := range sizes {
			pngs[i] = trayIconPNG(s)
		}
		return icoFromPNGs(sizes, pngs)
	}
	return trayIconPNG(64)
}
