// Package sprite holds the ASG mascot: a pixel-art hermit crab carrying its
// shell (ASG is a shell you carry into every project). Frames are composed
// from layers so every pose lines up. The terminal draws them with
// half-block characters; the dashboard and README use the animated SVG.
package sprite

import (
	"fmt"
	"strings"
)

type RGB struct{ R, G, B uint8 }

func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

var Palette = map[byte]RGB{
	'L': {0xe9, 0xff, 0x9c}, // shell highlight
	'S': {0xbd, 0xe8, 0x2c}, // shell
	'D': {0x7f, 0xa8, 0x14}, // shell shadow
	'P': {0x4a, 0x66, 0x08}, // spiral groove
	'C': {0xff, 0x7d, 0x52}, // crab
	'c': {0xd4, 0x4f, 0x2c}, // crab shade
	'W': {0xff, 0xff, 0xff}, // eye
	'E': {0x14, 0x16, 0x10}, // pupil
	'B': {0xff, 0xa8, 0xb8}, // blush
	'Y': {0xff, 0xe0, 0x4a}, // spark
	'y': {0xff, 0xf6, 0xb8}, // spark core
}

// W and H are the canvas size in pixels.
const W, H = 20, 14

// Grid is one frame; 0 is transparent, otherwise a Palette key.
type Grid [H][W]byte

type layer struct {
	x, y int
	rows []string // '.' is transparent
}

var (
	shellLayer = layer{0, 2, []string{
		"...LLLLL...",
		".LLLSSSSSS.",
		"LLSSPPPPSSS",
		"LSSPSSSSPSD",
		"LSPSSPPSPSD",
		"SSPSPSSPSSD",
		"SSPSPPPSSDD",
		"SSSPSSSSDD.",
		".SSSPPPDDD.",
		"..DDDDDDD..",
	}}
	bodyLayer = layer{7, 7, []string{
		"..CCCCCCC.",
		".CCCCCCCCC",
		"CCCCCCCCCC",
		"cCCCBCCCBC",
		".cccccccc.",
	}}
	stalkLayer = layer{11, 4, []string{
		"c..c",
		"c..c",
		"c..c",
	}}
	eyeLayers = map[string]layer{
		"open":  {10, 2, []string{"WW.WW", "WE.WE"}},
		"down":  {10, 2, []string{"WW.WW", "EW.EW"}},
		"blink": {10, 2, []string{".....", "cc.cc"}},
		"happy": {10, 2, []string{"WW.WW", "c..c."}},
	}
	clawLayers = map[string]layer{
		"down": {16, 6, []string{
			"....",
			"C.C.",
			"CCC.",
			".C..",
		}},
		"up": {16, 3, []string{
			"C.C.",
			"CCC.",
			".C..",
			".c..",
			".c..",
		}},
		"type": {16, 7, []string{
			".C.C",
			".CCC",
			"..C.",
		}},
	}
	legLayers = [2]layer{
		{8, 12, []string{".c.c..c.c", "c...c...c"}},
		{8, 12, []string{"c.c..c.c.", "..c...c.."}},
	}
)

// Pose describes one frame.
type Pose struct {
	DX    int    // horizontal shift (walking in)
	Bob   int    // 0 or 1: body and shell lowered a pixel
	Eyes  string // open, down, blink, happy
	Claw  string // down, up, type
	Legs  int    // leg phase
	Spark int    // 0 none, 1..3: sparkle phase above the shell (thinking)
}

// Compose renders a pose into a pixel grid.
func Compose(p Pose) Grid {
	var g Grid
	put := func(l layer, dy int) {
		for y, row := range l.rows {
			for x := 0; x < len(row); x++ {
				px, py := l.x+x+p.DX, l.y+y+dy
				if row[x] != '.' && px >= 0 && px < W && py >= 0 && py < H {
					g[py][px] = row[x]
				}
			}
		}
	}
	put(legLayers[p.Legs%2], 0)
	put(bodyLayer, p.Bob)
	put(stalkLayer, p.Bob)
	put(eyeLayers[p.Eyes], p.Bob)
	put(shellLayer, p.Bob)
	put(clawLayers[p.Claw], p.Bob)
	if p.Spark > 0 {
		for _, s := range [][3]int{{1, 0, 1}, {4, 1, 2}, {8, 0, 3}} {
			if s[2] == p.Spark {
				put(layer{s[0] - p.DX, s[1], []string{".Y.", "YyY", ".Y."}}, 0)
			} else if s[2] == p.Spark%3+1 {
				put(layer{s[0] + 1 - p.DX, s[1] + 1, []string{"Y"}}, 0)
			}
		}
	}
	return g
}

// Animations, as pose sequences played at a fixed tick.
var (
	// Enter: scuttle in from the left, blink, wave.
	Enter = func() []Pose {
		var out []Pose
		for i := 0; i < 10; i++ {
			out = append(out, Pose{DX: -(10 - i) * 2, Eyes: "open", Claw: "down", Legs: i, Bob: i % 2})
		}
		return append(out,
			Pose{Eyes: "open", Claw: "down"},
			Pose{Eyes: "blink", Claw: "down"},
			Pose{Eyes: "open", Claw: "down"},
			Pose{Eyes: "happy", Claw: "up"},
			Pose{Eyes: "happy", Claw: "down"},
			Pose{Eyes: "happy", Claw: "up"},
		)
	}()
	// Rest is the pose left on screen after the entrance.
	Rest = Pose{Eyes: "happy", Claw: "up"}
	// Idle: breathing, blinking, an occasional wave.
	Idle = func() []Pose {
		var out []Pose
		for i := 0; i < 24; i++ {
			p := Pose{Eyes: "open", Claw: "down", Bob: i / 6 % 2}
			switch {
			case i == 9:
				p.Eyes = "blink"
			case i >= 16 && i < 22:
				p.Eyes, p.Claw = "happy", []string{"up", "down"}[i%2]
			}
			out = append(out, p)
		}
		return out
	}()
	// Work: eyes on the job, claws tapping, sparks of thought, and a glance
	// up now and then.
	Work = func() []Pose {
		var out []Pose
		for i := 0; i < 24; i++ {
			p := Pose{Eyes: "down", Claw: "type", Bob: i / 2 % 2, Spark: i/2%3 + 1, Legs: i / 4}
			if i%2 == 1 {
				p.Claw = "down"
			}
			switch i {
			case 10, 11:
				p.Eyes = "open"
			case 20:
				p.Eyes = "blink"
			}
			out = append(out, p)
		}
		return out
	}()
)

// SVG renders an animation as a self-contained animated SVG: one group per
// frame, shown in turn by a CSS animation. Runs of equal pixels are merged
// into single rects to keep it small. scale is the pixel size.
func SVG(frames []Pose, frameMS, scale int) string {
	n := len(frames)
	total := n * frameMS
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" shape-rendering="crispEdges" role="img" aria-label="ASG hermit crab">`,
		W, H, W*scale, H*scale)
	if n > 1 {
		fmt.Fprintf(&b, `<style>g{visibility:hidden;animation:f %dms steps(1,end) infinite}@keyframes f{0%%{visibility:visible}%.4f%%{visibility:hidden}}@media (prefers-reduced-motion:reduce){g{animation:none}g:first-of-type{visibility:visible}}</style>`,
			total, 100/float64(n))
	}
	for i, p := range frames {
		g := Compose(p)
		if n > 1 {
			fmt.Fprintf(&b, `<g style="animation-delay:%dms">`, i*frameMS-total)
		} else {
			b.WriteString(`<g>`)
		}
		paths := map[byte]*strings.Builder{}
		var order []byte
		for y := 0; y < H; y++ {
			for x := 0; x < W; {
				k := g[y][x]
				run := 1
				for x+run < W && g[y][x+run] == k {
					run++
				}
				if k != 0 {
					if paths[k] == nil {
						paths[k] = &strings.Builder{}
						order = append(order, k)
					}
					fmt.Fprintf(paths[k], "M%d %dh%dv1h-%dz", x, y, run, run)
				}
				x += run
			}
		}
		for _, k := range order {
			fmt.Fprintf(&b, `<path fill="%s" d="%s"/>`, Palette[k].Hex(), paths[k].String())
		}
		b.WriteString(`</g>`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}
