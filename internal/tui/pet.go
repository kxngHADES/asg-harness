package tui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/harmonica"

	"github.com/kxngHADES/asg-harness/internal/sprite"
)

// showSprite reports whether the pixel mascot can be drawn: enough colour
// and a terminal wide enough for it plus some text.
func showSprite() bool {
	return richColor && termWidth() >= spriteW+24
}

// animate reports whether to animate at all (ASG_NO_ANIMATION turns it off).
func animate() bool { return os.Getenv("ASG_NO_ANIMATION") == "" }

const animEvery = 110 * time.Millisecond

// crabMotion moves the crab back and forth on a Harmonica spring, so it
// eases out, overshoots a little and settles before turning round.
type crabMotion struct {
	spring   harmonica.Spring
	pos, vel float64
	target   float64
	span     float64
}

func newCrab(span float64) crabMotion {
	return crabMotion{
		spring: harmonica.NewSpring(harmonica.FPS(int(time.Second/animEvery)), 3.2, 0.45),
		target: span,
		span:   span,
	}
}

func (c *crabMotion) step() {
	c.pos, c.vel = c.spring.Update(c.pos, c.vel, c.target)
	if math.Abs(c.pos-c.target) < 0.35 && math.Abs(c.vel) < 0.6 {
		if c.target == 0 {
			c.target = c.span
		} else {
			c.target = 0
		}
	}
}

// cell is the crab's position clamped to whole cells inside its track.
func (c *crabMotion) cell() int {
	return int(math.Round(math.Max(0, math.Min(c.span, c.pos))))
}

func (c *crabMotion) right() bool { return c.target > 0 }

// bannerText is the welcome block printed when asg starts: the resting
// crab with the info lines beside it, or just the lines without colour.
func bannerText(info []string) string {
	if !showSprite() {
		var out []string
		for _, l := range info {
			if l != "" {
				out = append(out, "  "+l)
			}
		}
		return strings.Join(out, "\n")
	}
	top := (spriteRows - len(info)) / 2
	return spriteBlock(renderSprite(sprite.Compose(sprite.Rest)), info, top, 0)
}

// introFrame is one frame of the crab scuttling in.
func introFrame(i int) string {
	return spriteBlock(renderSprite(sprite.Compose(sprite.Enter[i])), nil, 0, 0)
}

// workView draws the working animation with its status text.
func workView(frame int, crab *crabMotion, label string, elapsed time.Duration) string {
	dots := strings.Repeat(".", frame/3%4)
	status := slate(fmt.Sprintf("%.1fs · ctrl+c to stop", elapsed.Seconds()))
	if !showSprite() || !animate() {
		return "  " + petTrack(frame, crab.cell(), crab.right()) + " " + cyanBold(label) + cyan(dots) + "  " + status
	}
	rows := renderSprite(sprite.Compose(sprite.Work[frame%len(sprite.Work)]))
	text := []string{cyanBold(clip(label, termWidth()-spriteW-14)) + cyan(dots), status}
	return spriteBlock(rows, text, 3, crab.cell())
}

const trackCells, crabCells = 12, 5

// petTrack is the one-line crab used when the sprite can't be shown:
// a tiny crab walking along a track, shell trailing behind.
func petTrack(frame, pos int, right bool) string {
	span := trackCells - crabCells
	pos = max(0, min(pos, span))
	eyes := "oo"
	if frame%40 > 36 {
		eyes = "--"
	}
	body := lime("@") + amber("("+eyes+")")
	if !right {
		body = amber("("+eyes+")") + lime("@")
	}
	return slate(strings.Repeat("·", pos)) + body + slate(strings.Repeat("·", span-pos))
}
