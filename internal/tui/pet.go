package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/sprite"
)

const spriteRows = spriteH / 2

// showSprite reports whether the pixel mascot can be drawn: colour is on
// and the terminal is wide enough for it plus some text.
func showSprite() bool {
	return reset != "" && termWidth() >= spriteW+24
}

func animate() bool {
	return showSprite() && os.Getenv("ASG_NO_ANIMATION") == "" && term.IsTerminal(int(os.Stdout.Fd()))
}

// spriteBlock lays out sprite rows with text lines beside them, starting at
// the given row, each line cleared first and ending with "\n".
func spriteBlock(rows []string, info []string, infoTop int) string {
	var b strings.Builder
	for i, row := range rows {
		b.WriteString("\r\x1b[2K  " + row)
		if j := i - infoTop; j >= 0 && j < len(info) {
			b.WriteString("  " + info[j])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// petBanner draws the mascot with info lines beside it. On a capable
// terminal the crab scuttles in, blinks, and waves.
func petBanner(w io.Writer, info []string) {
	if !showSprite() {
		for _, l := range info {
			if l != "" {
				fmt.Fprintf(w, "  %s\n", l)
			}
		}
		return
	}
	top := (spriteRows - len(info)) / 2
	if animate() {
		fmt.Fprint(w, "\x1b[?25l")
		defer fmt.Fprint(w, "\x1b[?25h")
		for i, p := range sprite.Enter {
			if i > 0 {
				fmt.Fprintf(w, "\x1b[%dA", spriteRows)
			}
			fmt.Fprint(w, spriteBlock(renderSprite(sprite.Compose(p)), nil, 0))
			time.Sleep(70 * time.Millisecond)
		}
		fmt.Fprintf(w, "\x1b[%dA", spriteRows)
	}
	fmt.Fprint(w, spriteBlock(renderSprite(sprite.Compose(sprite.Rest)), info, top))
}

// workFrame draws one frame of the working animation with its status text.
func workFrame(i int, label string, elapsed time.Duration) string {
	rows := renderSprite(sprite.Compose(sprite.Work[i%len(sprite.Work)]))
	dots := strings.Repeat(".", i/3%4)
	text := []string{
		cyan + bold + clip(label, termWidth()-spriteW-8) + reset + cyan + dots + reset,
		fmt.Sprintf("%s%.1fs · ctrl+c to stop%s", slate, elapsed.Seconds(), reset),
	}
	var b strings.Builder
	b.WriteString(spriteBlock(rows, text, 3))
	fmt.Fprintf(&b, "\x1b[%dA\r", spriteRows)
	return b.String()
}

// petTrack is the one-line spinner used when the sprite can't be shown:
// a tiny crab walking back and forth, shell trailing behind.
func petTrack(i int) string {
	const cells, crab = 12, 5
	span := cells - crab
	pos := i / 2 % (2 * span)
	right := pos < span
	if !right {
		pos = 2*span - pos
	}
	eyes := "oo"
	if i%40 > 36 {
		eyes = "--"
	}
	body := lime + "@" + amber + "(" + eyes + ")"
	if !right {
		body = amber + "(" + eyes + ")" + lime + "@"
	}
	return slate + strings.Repeat("·", pos) + body + slate + strings.Repeat("·", span-pos) + reset
}
