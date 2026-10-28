package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/config"
)

const voiceHelp = `  /voice                         show status and which voice will speak
  /voice on | off                toggle reading replies aloud
  /voice auto                    current model speaks if it can, else the fallback
  /voice ollama <model> [voice]  always use an Ollama speech model, e.g. /voice ollama phonon Juniper
  /voice openai [voice]          OpenAI TTS (uses the openai provider's key)
  /voice system [voice]          the OS voice (macOS say, espeak, Windows SAPI)
  /voice fallback <engine> …     voice used by auto when the model can't speak
  /voice pin <engine> …          pin a voice for the current chat model only
  /voice unpin                   remove the pin for the current chat model
  /voice models                  list installed Ollama speech models
  /voice voices [model]          list voices of an Ollama speech model
`

// parseSpec reads "<engine> [model] [voice…]" (model only for ollama).
func parseSpec(args []string) (config.VoiceSpec, error) {
	if len(args) == 0 {
		return config.VoiceSpec{}, fmt.Errorf("engine required: auto, ollama, openai or system")
	}
	spec := config.VoiceSpec{Engine: strings.ToLower(args[0])}
	rest := args[1:]
	switch spec.Engine {
	case "auto":
	case "ollama":
		if len(rest) == 0 {
			return spec, fmt.Errorf("usage: ollama <model> [voice] — see /voice models")
		}
		spec.Model, rest = rest[0], rest[1:]
	case "openai", "system":
	default:
		return spec, fmt.Errorf("unknown engine %q (auto, ollama, openai, system)", spec.Engine)
	}
	spec.Voice = strings.Join(rest, " ") // voice names may contain spaces
	return spec, nil
}

func describeSpec(s config.VoiceSpec) string {
	out := s.Engine
	if s.Model != "" {
		out += " " + s.Model
	}
	if s.Voice != "" {
		out += " (" + s.Voice + ")"
	}
	return out
}

func (r *REPL) voiceCommand(ctx context.Context, args []string) {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "":
		cfg := r.app.Config.Get()
		_, pc := cfg.Active()
		state := map[bool]string{true: green + "on" + reset, false: "off"}[cfg.Audio.Enabled]
		r.printf("  voice output: %s\n  default:      %s\n  fallback:     %s\n", state, describeSpec(cfg.Audio.VoiceSpec), describeSpec(cfg.Audio.Fallback))
		for m, s := range cfg.Audio.PerModel {
			r.printf("  pinned:       %s → %s\n", m, describeSpec(s))
		}
		opt := r.app.ResolveVoice(ctx)
		r.printf("  %snow:          %s → %s%s  %s(%s)%s\n", bold, pc.Model, opt.Label(), reset, dim, opt.Why, reset)
	case "help":
		r.printf("%s", voiceHelp)
	case "on", "off":
		on := sub == "on"
		r.update(func(c *config.Config) error { c.Audio.Enabled = on; return nil })
		if !on {
			r.app.Speaker.Stop()
		}
		r.printf("%svoice output %s%s\n", dim, sub, reset)
	case "models":
		sp := startSpinner("checking models")
		models, err := r.app.SpeechModels(ctx, "")
		sp.Stop()
		if err != nil {
			r.printf("%s%v%s\n", red, err, reset)
			return
		}
		if len(models) == 0 {
			r.printf("%sno Ollama speech models installed — try: ollama pull phonon%s\n", dim, reset)
			return
		}
		for _, m := range models {
			r.printf("  %s\n", m)
		}
	case "voices":
		model := strings.Join(args[1:], " ")
		if model == "" {
			cfg := r.app.Config.Get()
			for _, s := range []config.VoiceSpec{cfg.Audio.VoiceSpec, cfg.Audio.Fallback} {
				if s.Engine == "ollama" {
					model = s.Model
					break
				}
			}
		}
		if model == "" {
			r.printf("usage: /voice voices <ollama speech model>\n")
			return
		}
		voices, err := r.app.Voices(ctx, "", model)
		if err != nil {
			r.printf("%s%v%s\n", red, err, reset)
			return
		}
		r.printf("%s%s%s\n", dim, strings.Join(voices, " · "), reset)
	case "unpin":
		var model string
		r.update(func(c *config.Config) error {
			model = c.Providers[c.ActiveProvider].Model
			delete(c.Audio.PerModel, model)
			return nil
		})
		r.printf("%sunpinned voice for %s%s\n", dim, model, reset)
	case "fallback", "pin":
		spec, err := parseSpec(args[1:])
		if err == nil && spec.Engine == "auto" {
			err = fmt.Errorf("%s needs a concrete engine (ollama, openai or system)", sub)
		}
		if err != nil {
			r.printf("%s%v%s\n", red, err, reset)
			return
		}
		var model string
		r.update(func(c *config.Config) error {
			if sub == "fallback" {
				c.Audio.Fallback = spec
				return nil
			}
			model = c.Providers[c.ActiveProvider].Model
			if c.Audio.PerModel == nil {
				c.Audio.PerModel = map[string]config.VoiceSpec{}
			}
			c.Audio.PerModel[model] = spec
			return nil
		})
		if sub == "fallback" {
			r.printf("%sfallback voice → %s%s\n", dim, describeSpec(spec), reset)
		} else {
			r.printf("%s%s will be read by %s%s\n", dim, model, describeSpec(spec), reset)
		}
	default:
		spec, err := parseSpec(args)
		if err != nil {
			r.printf("%s%v%s\n%s", red, err, reset, voiceHelp)
			return
		}
		r.update(func(c *config.Config) error {
			rate := c.Audio.Rate
			c.Audio.VoiceSpec = spec
			c.Audio.Rate = rate
			c.Audio.Enabled = true
			return nil
		})
		opt := r.app.ResolveVoice(ctx)
		r.printf("%svoice → %s · now speaking with %s (%s)%s\n", dim, describeSpec(spec), opt.Label(), opt.Why, reset)
	}
}
