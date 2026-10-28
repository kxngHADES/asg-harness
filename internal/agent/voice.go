package agent

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kxngHADES/asg-harness/internal/audio"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
)

// ModelInfo returns cached Ollama capabilities for a model.
func (a *App) ModelInfo(ctx context.Context, pc *config.ProviderConfig, model string) (llm.ModelInfo, error) {
	key := pc.BaseURL + "|" + model
	a.capMu.Lock()
	if info, ok := a.caps[key]; ok {
		a.capMu.Unlock()
		return info, nil
	}
	a.capMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	o := &llm.Ollama{BaseURL: strings.TrimRight(pc.BaseURL, "/"), Client: http.DefaultClient}
	info, err := o.Show(ctx, model)
	if err != nil {
		return info, err
	}
	a.capMu.Lock()
	if a.caps == nil {
		a.caps = map[string]llm.ModelInfo{}
	}
	a.caps[key] = info
	a.capMu.Unlock()
	return info, nil
}

// ResolveVoice decides who reads replies aloud for the active chat model:
// a per-model pin wins; "auto" uses the chat model itself when Ollama says
// it can speak, else the fallback.
func (a *App) ResolveVoice(ctx context.Context) audio.Options {
	cfg := a.Config.Get()
	provName, pc := cfg.Active()
	chatModel := ""
	if pc != nil {
		chatModel = pc.Model
	}
	spec := cfg.Audio.VoiceSpec
	why := "default voice"
	if pin, ok := cfg.Audio.PerModel[chatModel]; ok && pin.Engine != "" {
		spec, why = pin, "pinned for "+chatModel
	}
	if spec.Engine == "auto" || spec.Engine == "" {
		if pc != nil && pc.Type == "ollama" {
			if info, err := a.ModelInfo(ctx, pc, chatModel); err == nil && info.Has("speech") {
				spec = config.VoiceSpec{Engine: "ollama", Model: chatModel, Voice: spec.Voice, Provider: provName}
				why = chatModel + " speaks for itself"
			}
		}
		if spec.Engine == "auto" || spec.Engine == "" {
			spec, why = cfg.Audio.Fallback, chatModel+" can't speak — using fallback"
		}
	}
	opt := a.voiceOptions(cfg, spec)
	opt.Why = why
	return opt
}

func (a *App) voiceOptions(cfg config.Config, spec config.VoiceSpec) audio.Options {
	opt := audio.Options{Engine: spec.Engine, Model: spec.Model, Voice: spec.Voice, Rate: cfg.Audio.Rate}
	if opt.Engine == "" || opt.Engine == "auto" {
		opt.Engine = "system"
	}
	if opt.Engine == "system" {
		return opt
	}
	pc := a.speechProvider(cfg, spec)
	switch {
	case pc == nil && opt.Engine == "ollama":
		opt.BaseURL = "http://localhost:11434/v1"
	case pc == nil:
		opt.BaseURL = "https://api.openai.com/v1"
	case pc.Type == "ollama":
		opt.BaseURL, opt.APIKey = strings.TrimRight(pc.BaseURL, "/")+"/v1", pc.APIKey
	default:
		opt.BaseURL, opt.APIKey = pc.BaseURL, pc.APIKey
	}
	return opt
}

// speechProvider finds the provider entry that hosts the speech model.
func (a *App) speechProvider(cfg config.Config, spec config.VoiceSpec) *config.ProviderConfig {
	if pc := cfg.Providers[spec.Provider]; pc != nil {
		return pc
	}
	want := spec.Engine
	if _, active := cfg.Active(); active != nil && active.Type == want {
		return active
	}
	if want == "openai" {
		if pc := cfg.Providers["openai"]; pc != nil {
			return pc
		}
	}
	for _, n := range cfg.ProviderNames() {
		if cfg.Providers[n].Type == want {
			return cfg.Providers[n]
		}
	}
	return nil
}

// ollamaProvider returns the first configured Ollama provider.
func (a *App) ollamaProvider(cfg config.Config, name string) (*config.ProviderConfig, error) {
	if pc := cfg.Providers[name]; pc != nil && pc.Type == "ollama" {
		return pc, nil
	}
	if pc := a.speechProvider(cfg, config.VoiceSpec{Engine: "ollama"}); pc != nil {
		return pc, nil
	}
	return nil, fmt.Errorf("no Ollama provider configured")
}

// SpeechModels lists installed Ollama models that can speak.
func (a *App) SpeechModels(ctx context.Context, provider string) ([]string, error) {
	pc, err := a.ollamaProvider(a.Config.Get(), provider)
	if err != nil {
		return nil, err
	}
	p, _ := llm.New(pc)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	models, err := p.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := []string{}
	for _, m := range models {
		if strings.HasSuffix(m, "-cloud") || strings.HasSuffix(m, ":cloud") {
			continue
		}
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			if info, err := a.ModelInfo(ctx, pc, m); err == nil && info.Has("speech") {
				mu.Lock()
				out = append(out, m)
				mu.Unlock()
			}
		}(m)
	}
	wg.Wait()
	sort.Strings(out)
	return out, nil
}

// Voices lists the voices an Ollama speech model offers.
func (a *App) Voices(ctx context.Context, provider, model string) ([]string, error) {
	pc, err := a.ollamaProvider(a.Config.Get(), provider)
	if err != nil {
		return nil, err
	}
	info, err := a.ModelInfo(ctx, pc, model)
	if err != nil {
		return nil, err
	}
	if info.Voices == nil {
		return []string{}, nil
	}
	return info.Voices, nil
}
