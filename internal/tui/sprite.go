package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/sprite"
)

// Terminals can't show SVG everywhere, so the mascot is drawn with
// half-block characters: each cell is two pixels, the top one as the "▀"
// foreground and the bottom one as the background.

const spriteW, spriteH = sprite.W, sprite.H

var trueColor = func() bool {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	tp := os.Getenv("TERM_PROGRAM")
	return ct == "truecolor" || ct == "24bit" || os.Getenv("WT_SESSION") != "" ||
		tp == "iTerm.app" || tp == "WezTerm" || tp == "vscode" || tp == "ghostty" ||
		strings.Contains(os.Getenv("TERM"), "kitty")
}()

func colorCode(c sprite.RGB, bg bool) string {
	base := 38
	if bg {
		base = 48
	}
	if trueColor {
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", base, c.R, c.G, c.B)
	}
	q := func(v uint8) int { // nearest step of the xterm 6×6×6 cube
		switch {
		case v < 48:
			return 0
		case v < 115:
			return 1
		}
		return int(v-35) / 40
	}
	return fmt.Sprintf("\x1b[%d;5;%dm", base, 16+36*q(c.R)+6*q(c.G)+q(c.B))
}

// renderSprite turns a pixel grid into terminal rows using half blocks.
func renderSprite(g sprite.Grid) []string {
	rows := make([]string, 0, spriteH/2)
	for y := 0; y < spriteH; y += 2 {
		var b strings.Builder
		for x := 0; x < spriteW; x++ {
			top, bot := g[y][x], g[y+1][x]
			switch {
			case top == 0 && bot == 0:
				b.WriteByte(' ')
				continue
			case bot == 0:
				b.WriteString(colorCode(sprite.Palette[top], false) + "▀")
			case top == 0:
				b.WriteString(colorCode(sprite.Palette[bot], false) + "▄")
			default:
				b.WriteString(colorCode(sprite.Palette[top], false) + colorCode(sprite.Palette[bot], true) + "▀")
			}
			b.WriteString("\x1b[0m")
		}
		rows = append(rows, b.String())
	}
	return rows
}
