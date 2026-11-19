package tui

import (
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/kxngHADES/asg-harness/internal/sprite"
)

// Terminals can't show SVG everywhere, so the mascot is drawn with
// half-block characters: each cell is two pixels, the top one as the "▀"
// foreground and the bottom one as the background. Lip Gloss picks true
// colour or the nearest 256-colour match for the terminal.

const spriteW, spriteH = sprite.W, sprite.H

const spriteRows = spriteH / 2

var spriteCache sync.Map // sprite.Grid → []string

// renderSprite turns a pixel grid into terminal rows using half blocks.
func renderSprite(g sprite.Grid) []string {
	if v, ok := spriteCache.Load(g); ok {
		return v.([]string)
	}
	rows := make([]string, 0, spriteRows)
	for y := 0; y < spriteH; y += 2 {
		var b strings.Builder
		for x := 0; x < spriteW; x++ {
			top, bot := g[y][x], g[y+1][x]
			st := lipgloss.NewStyle()
			switch {
			case top == 0 && bot == 0:
				b.WriteByte(' ')
				continue
			case bot == 0:
				b.WriteString(st.Foreground(lipgloss.Color(sprite.Palette[top].Hex())).Render("▀"))
			case top == 0:
				b.WriteString(st.Foreground(lipgloss.Color(sprite.Palette[bot].Hex())).Render("▄"))
			default:
				b.WriteString(st.Foreground(lipgloss.Color(sprite.Palette[top].Hex())).
					Background(lipgloss.Color(sprite.Palette[bot].Hex())).Render("▀"))
			}
		}
		rows = append(rows, b.String())
	}
	spriteCache.Store(g, rows)
	return rows
}

// spriteBlock lays out sprite rows with text lines beside them, the text
// starting at row infoTop. pad shifts the sprite right by that many cells.
func spriteBlock(rows []string, info []string, infoTop, pad int) string {
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat(" ", 2+pad) + row)
		if j := i - infoTop; j >= 0 && j < len(info) {
			b.WriteString("  " + info[j])
		}
	}
	return b.String()
}
