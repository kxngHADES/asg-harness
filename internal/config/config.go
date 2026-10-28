// Package config loads and saves the harness configuration (~/.asg/config.json).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ProviderConfig describes one model backend. Type is one of "ollama",
// "openai" (any OpenAI-compatible API) or "anthropic".
type ProviderConfig struct {
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model"`
}

// VoiceSpec says who reads replies aloud.
//
// Engine is one of:
//   - "auto":   the current chat model, if Ollama reports it can speak;
//     otherwise the audio fallback
//   - "ollama": a specific Ollama speech model (e.g. model "phonon", voice "Juniper")
//   - "openai": an OpenAI-compatible /audio/speech endpoint
//   - "system": the OS voice (macOS say, espeak, Windows SAPI)
type VoiceSpec struct {
	Engine string `json:"engine"`
	Model  string `json:"model,omitempty"`
	Voice  string `json:"voice,omitempty"`
	// Provider names the provider entry supplying the base URL and API key.
	// Empty picks the first provider of the matching type.
	Provider string `json:"provider,omitempty"`
}

type AudioConfig struct {
	Enabled bool `json:"enabled"`
	VoiceSpec
	Rate int `json:"rate"` // words per minute for system TTS (0 = default)
	// Fallback is used when Engine is "auto" and the chat model can't speak.
	Fallback VoiceSpec `json:"fallback"`
	// PerModel pins a voice for specific chat models, keyed by model name.
	PerModel map[string]VoiceSpec `json:"per_model,omitempty"`
	// OpenAIProvider is the pre-VoiceSpec name for Provider; read for compatibility.
	OpenAIProvider string `json:"openai_provider,omitempty"`
}

type Config struct {
	ActiveProvider string                     `json:"active_provider"`
	Providers      map[string]*ProviderConfig `json:"providers"`
	SystemPrompt   string                     `json:"system_prompt,omitempty"`
	AutoApprove    bool                       `json:"auto_approve"`
	MaxToolRounds  int                        `json:"max_tool_rounds"`
	DashboardPort  int                        `json:"dashboard_port"`
	Temperature    *float64                   `json:"temperature,omitempty"`
	Audio          AudioConfig                `json:"audio"`
}

// Store guards a Config and persists it to disk.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *Config
}

// Dir returns the harness home directory, honouring $ASG_HOME.
func Dir() string {
	if d := os.Getenv("ASG_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".asg"
	}
	return filepath.Join(home, ".asg")
}

func Default() *Config {
	return &Config{
		ActiveProvider: "ollama",
		Providers: map[string]*ProviderConfig{
			"ollama":    {Type: "ollama", BaseURL: "http://localhost:11434", Model: "llama3.1:8b"},
			"anthropic": {Type: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-5-5"},
			"openai":    {Type: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-5"},
		},
		MaxToolRounds: 25,
		DashboardPort: 7777,
		Audio: AudioConfig{
			VoiceSpec: VoiceSpec{Engine: "auto"},
			Fallback:  VoiceSpec{Engine: "system"},
		},
	}
}

// Load reads the config file, creating it with defaults if missing.
func Load() (*Store, error) {
	path := filepath.Join(Dir(), "config.json")
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfg = Default()
		return s, s.save()
	}
	if err != nil {
		return nil, err
	}
	cfg := Default()
	cfg.Providers = map[string]*ProviderConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.normalize()
	s.cfg = cfg
	return s, nil
}

func (c *Config) normalize() {
	if c.Providers == nil {
		c.Providers = map[string]*ProviderConfig{}
	}
	if len(c.Providers) == 0 {
		c.Providers = Default().Providers
	}
	if _, ok := c.Providers[c.ActiveProvider]; !ok {
		for _, name := range sortedKeys(c.Providers) {
			c.ActiveProvider = name
			break
		}
	}
	if c.MaxToolRounds <= 0 {
		c.MaxToolRounds = 25
	}
	if c.DashboardPort == 0 {
		c.DashboardPort = 7777
	}
	if c.Audio.Engine == "" {
		c.Audio.Engine = "auto"
	}
	if c.Audio.Provider == "" && c.Audio.OpenAIProvider != "" {
		c.Audio.Provider = c.Audio.OpenAIProvider
	}
	c.Audio.OpenAIProvider = ""
	if c.Audio.Fallback.Engine == "" || c.Audio.Fallback.Engine == "auto" {
		c.Audio.Fallback.Engine = "system"
	}
}

func (s *Store) Path() string { return s.path }

// Get returns a deep copy of the current config.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.cfg)
}

// Update applies fn to the config and saves it.
func (s *Store) Update(fn func(c *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.cfg)
	if err := fn(&next); err != nil {
		return err
	}
	next.normalize()
	s.cfg = &next
	return s.save()
}

// Replace swaps in a whole new config (used by the dashboard).
func (s *Store) Replace(c Config) error {
	return s.Update(func(cur *Config) error { *cur = c; return nil })
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func clone(c *Config) Config {
	data, _ := json.Marshal(c)
	var out Config
	_ = json.Unmarshal(data, &out)
	return out
}

func sortedKeys(m map[string]*ProviderConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ProviderNames returns provider names sorted alphabetically.
func (c *Config) ProviderNames() []string { return sortedKeys(c.Providers) }

// Active returns the active provider config.
func (c *Config) Active() (string, *ProviderConfig) {
	return c.ActiveProvider, c.Providers[c.ActiveProvider]
}
