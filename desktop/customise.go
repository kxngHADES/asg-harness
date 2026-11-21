package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
)

const keyMask = "••••"

func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return keyMask
	}
	return keyMask + k[len(k)-4:]
}

// GetConfig returns the harness config with API keys masked.
func (a *App) GetConfig() config.Config {
	cfg := a.core.Config.Get()
	for _, p := range cfg.Providers {
		p.APIKey = maskKey(p.APIKey)
	}
	return cfg
}

// SaveConfig stores the config. Masked keys keep their old value.
func (a *App) SaveConfig(next config.Config) (config.Config, error) {
	if len(next.Providers) == 0 {
		return next, errors.New("at least one provider is required")
	}
	for name, p := range next.Providers {
		switch p.Type {
		case "ollama", "openai", "anthropic":
		default:
			return next, fmt.Errorf("provider %s: unknown type %q", name, p.Type)
		}
	}
	err := a.core.Config.Update(func(cur *config.Config) error {
		for name, p := range next.Providers {
			if strings.HasPrefix(p.APIKey, keyMask) {
				if old := cur.Providers[name]; old != nil {
					p.APIKey = old.APIKey
				} else {
					p.APIKey = ""
				}
			}
		}
		*cur = next
		return nil
	})
	if err != nil {
		return next, err
	}
	a.emit("asg:model", a.modelInfo())
	return a.GetConfig(), nil
}

func (a *App) GetModel() ModelInfo { return a.modelInfo() }

// SetModel makes provider active and, when model is set, picks its model.
func (a *App) SetModel(provider, model string) (ModelInfo, error) {
	err := a.core.Config.Update(func(c *config.Config) error {
		pc := c.Providers[provider]
		if pc == nil {
			return fmt.Errorf("unknown provider %q", provider)
		}
		c.ActiveProvider = provider
		if model != "" {
			pc.Model = model
		}
		return nil
	})
	mi := a.modelInfo()
	a.emit("asg:model", mi)
	return mi, err
}

// ListModels asks a provider which models it offers.
func (a *App) ListModels(provider string) ([]string, error) {
	cfg := a.core.Config.Get()
	pc := cfg.Providers[provider]
	if pc == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	p, err := llm.New(pc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := p.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(models)
	return models, nil
}

// ---------- skills ----------

type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Dir         string `json:"dir"`
	Source      string `json:"source,omitempty"`
	Body        string `json:"body,omitempty"`
	Builtin     bool   `json:"builtin,omitempty"`
}

func (a *App) ListSkills() []SkillInfo {
	out := []SkillInfo{}
	for _, s := range a.core.Skills.List() {
		out = append(out, SkillInfo{Name: s.Name, Description: s.Description, Dir: s.Dir, Source: s.Source, Builtin: s.Builtin})
	}
	return out
}

func (a *App) GetSkill(name string) (SkillInfo, error) {
	s := a.core.Skills.Get(name)
	if s == nil {
		return SkillInfo{}, fmt.Errorf("no skill %q", name)
	}
	return SkillInfo{Name: s.Name, Description: s.Description, Dir: s.Dir, Source: s.Source, Body: s.Body, Builtin: s.Builtin}, nil
}

func (a *App) CreateSkill(name, description, body string) error {
	_, err := a.core.Skills.Create(name, description, body)
	return err
}

func (a *App) ImportSkill(source string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	got, err := a.core.Skills.Import(ctx, strings.TrimSpace(source))
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	return names, err
}

func (a *App) DeleteSkill(name string) error { return a.core.Skills.Remove(name) }

// ---------- tools ----------

type ToolInfo struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	NeedsApproval bool   `json:"needs_approval"`
	Source        string `json:"source"`
}

func (a *App) ListTools() []ToolInfo {
	out := []ToolInfo{}
	for _, t := range a.core.Tools.All() {
		out = append(out, ToolInfo{t.Spec.Name, t.Spec.Description, t.NeedsApproval, t.Source})
	}
	return out
}

type ToolReload struct {
	Tools  []ToolInfo `json:"tools"`
	Errors []string   `json:"errors"`
}

// ReloadTools rescans the script-tool folders.
func (a *App) ReloadTools() ToolReload {
	out := ToolReload{Errors: []string{}}
	for _, err := range a.core.ReloadTools() {
		out.Errors = append(out.Errors, err.Error())
	}
	out.Tools = a.ListTools()
	return out
}

// ---------- memory ----------

type MemoryNote struct {
	Name    string    `json:"name"`
	Scope   string    `json:"scope"`
	Path    string    `json:"path"`
	Summary string    `json:"summary"`
	Body    string    `json:"body"`
	ModTime time.Time `json:"mod_time"`
}

func (a *App) ListMemory() []MemoryNote {
	out := []MemoryNote{}
	for _, n := range a.core.Memory.List() {
		out = append(out, MemoryNote{n.Name, n.Scope, n.Path, n.Summary, n.Body, n.ModTime})
	}
	return out
}

func (a *App) SaveMemory(scope, name, content string) error {
	_, err := a.core.Memory.Save(scope, name, content)
	return err
}

func (a *App) DeleteMemory(scope, name string) error { return a.core.Memory.Delete(scope, name) }
