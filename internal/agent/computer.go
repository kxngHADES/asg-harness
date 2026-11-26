package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/tools"
	"github.com/kxngHADES/asg-harness/tools/computer"
)

// computerTool is live computer use, connected to the app's config and,
// when one is configured, a separate vision model for perception.
func (a *App) computerTool() *tools.Tool {
	return computer.Tool(computer.Host{
		Config:   func() config.ComputerConfig { return a.Config.Get().Computer },
		Perceive: a.perceive,
	})
}

// perceive asks the configured vision model to describe a screenshot, so a
// chat model that can't see (or a cheaper one) can still plan actions.
func (a *App) perceive(ctx context.Context, img llm.Image, w, h int, focus string) (string, error) {
	cfg := a.Config.Get()
	cc := cfg.Computer
	name := cc.VisionProvider
	if name == "" {
		name = cfg.ActiveProvider
	}
	base := cfg.Providers[name]
	if base == nil {
		return "", fmt.Errorf("vision provider %q is not configured", name)
	}
	pc := *base
	pc.Model = cc.VisionModel
	p, err := llm.New(&pc)
	if err != nil {
		return "", err
	}
	prompt := fmt.Sprintf(`You are the eyes of an agent that controls this computer. The screenshot is %d×%d pixels; (0,0) is the top-left.
1. Say which app and window are active and what state the screen is in (dialogs, errors, loading, selected items).
2. List every visible interactive element as "- (x, y) kind \"label\"", where (x, y) is the CENTRE of the element in this image's pixels. Kinds: button, link, field, checkbox, tab, menu, icon, list item, text.
3. Quote any important text exactly.
Be precise about coordinates; don't guess at things you can't see.`, w, h)
	if f := strings.TrimSpace(focus); f != "" {
		prompt += "\nThe agent is looking for: " + f + " — say clearly whether it is visible and where."
	}
	resp, err := p.Chat(ctx, llm.Request{Model: pc.Model, Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt, Images: []llm.Image{img}}}, NumCtx: numCtx(&pc)}, nil)
	if err != nil {
		return "", err
	}
	if resp == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return "", errors.New("the vision model returned nothing")
	}
	return resp.Message.Content, nil
}

// computerInstructions is the system prompt section while computer use is on.
func computerInstructions(cfg config.Config) string {
	if !cfg.Computer.Enabled {
		return ""
	}
	return `

## Computer use
You can see and control the user's computer with the computer tool (real mouse and keyboard). Use it for tasks that need a GUI; prefer files, shell and web tools when they can do the job.
Loop: screenshot → one action → check the screenshot that comes back → next. If a click did nothing, look again before retrying; don't repeat blind.
Mark anything that sends, submits, buys, deletes or can't be undone with sensitive=true. If control is paused or stopped, stop acting and follow the tool's message.`
}
