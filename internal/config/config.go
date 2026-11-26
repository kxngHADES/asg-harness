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
	// NumCtx is the context window requested from Ollama (num_ctx). Ollama's
	// own default is small enough that system prompt + tools + a file or
	// two get silently truncated, which wrecks small models.
	// 0 uses DefaultNumCtx; a negative value leaves it to Ollama.
	NumCtx int `json:"num_ctx,omitempty"`
}

// LoopConfig tunes the loop harness (/loop): plan → execute → verify →
// evaluate, one small step at a time.
type LoopConfig struct {
	// MaxIterations caps step attempts across the whole run.
	MaxIterations int `json:"max_iterations"`
	// StepAttempts is how often one step is retried before it is split into
	// smaller steps (and, if that fails too, handed back to the user).
	StepAttempts int `json:"step_attempts"`
	// StepRounds caps tool rounds inside one step.
	StepRounds int `json:"step_rounds"`
	// Verify is the command that proves the work (e.g. "go test ./...").
	// Empty auto-detects from the project; "none" disables verification.
	Verify string `json:"verify,omitempty"`
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

// ReviewConfig sets how often the model goes back over its own answer.
type ReviewConfig struct {
	// Passes is the minimum number of self-review passes after every answer
	// (default 4). A negative value turns self-review off.
	Passes int `json:"passes"`
	// MaxPasses caps the passes when the last one still found problems.
	MaxPasses int `json:"max_passes"`
}

type Config struct {
	ActiveProvider string                     `json:"active_provider"`
	Providers      map[string]*ProviderConfig `json:"providers"`
	SystemPrompt   string                     `json:"system_prompt,omitempty"`
	AutoApprove    bool                       `json:"auto_approve"`
	// MaxToolRounds caps tool rounds in one chat turn; 0 means no limit
	// (the default): the model uses tools until it is done or you press Ctrl+C.
	MaxToolRounds int          `json:"max_tool_rounds,omitempty"`
	DashboardPort int          `json:"dashboard_port"`
	Temperature   *float64     `json:"temperature,omitempty"`
	Audio         AudioConfig  `json:"audio"`
	Loop          LoopConfig   `json:"loop"`
	Review        ReviewConfig `json:"review"`
	// MemoryDir is where memory notes live. Empty keeps them in the harness
	// home (~/.asg/memory and ~/.asg/projects/<p>/memory).
	MemoryDir string `json:"memory_dir,omitempty"`
	// Domain and Flavor are the default work track (e.g. software / work).
	Domain string `json:"domain,omitempty"`
	Flavor string `json:"flavor,omitempty"`
	// Computer configures live computer use (the computer tool).
	Computer ComputerConfig `json:"computer"`
}

// ComputerConfig configures computer use: ASG seeing the screen and moving
// the real mouse and keyboard. It is off until the user turns it on.
type ComputerConfig struct {
	Enabled bool `json:"enabled"`
	// Monitor is the monitor used when a call names none: 0 is the primary
	// monitor, 1… the others in the order list_monitors shows them, and -1
	// the whole desktop across every monitor.
	Monitor int `json:"monitor"`
	// MaxImageSide caps the longer side of screenshots sent to the model
	// (default 1280). Coordinates the model gives are in that image.
	MaxImageSide int `json:"max_image_side,omitempty"`
	// NoAutoScreenshot stops ASG from taking a screenshot after every mouse
	// or keyboard action so the model can verify it.
	NoAutoScreenshot bool `json:"no_auto_screenshot,omitempty"`
	// VisionProvider / VisionModel pick a separate perception model: it looks
	// at each screenshot and describes it in text (with coordinates) for the
	// chat model, which then only plans the actions. Empty sends the image
	// to the chat model itself. VisionProvider defaults to the active one.
	VisionProvider string `json:"vision_provider,omitempty"`
	VisionModel    string `json:"vision_model,omitempty"`
	// Approval is the permission level:
	//   "actions" (default) — looking is free, every mouse/keyboard action asks
	//   "every"             — every call asks, screenshots too
	//   "auto"              — nothing asks except sensitive actions
	// Sensitive actions (sending, deleting, closing, anything the model flags)
	// always ask, even in YOLO mode.
	Approval string `json:"approval,omitempty"`
	// MaxActions caps mouse/keyboard actions in one turn (default 200).
	MaxActions int `json:"max_actions,omitempty"`
	// SessionMinutes ends control this long after a turn's first action
	// (default 30).
	SessionMinutes int `json:"session_minutes,omitempty"`
	// MoveMillis is how long a visible mouse move takes (default 250; -1 jumps).
	MoveMillis int `json:"move_ms,omitempty"`
	// ExcludedApps are app (process) names or window-title words ASG must not
	// see or touch: their windows are blacked out of screenshots and input
	// into them is refused.
	ExcludedApps []string `json:"excluded_apps,omitempty"`
	// ExcludedMonitors are monitor numbers (as in Monitor) never captured or used.
	ExcludedMonitors []int `json:"excluded_monitors,omitempty"`
}

// Computer approval levels.
const (
	ComputerApproveActions = "actions"
	ComputerApproveEvery   = "every"
	ComputerApproveAuto    = "auto"
)

// DefaultNumCtx is the Ollama context window used when num_ctx is unset.
const DefaultNumCtx = 16384

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
	// 25 was the old default cap, written into every saved config. Treat it
	// as unset so existing users get unlimited tool use too.
	if c.MaxToolRounds < 0 || c.MaxToolRounds == 25 {
		c.MaxToolRounds = 0
	}
	if c.Loop.MaxIterations <= 0 {
		c.Loop.MaxIterations = 30
	}
	if c.Loop.StepAttempts <= 0 {
		c.Loop.StepAttempts = 3
	}
	if c.Loop.StepRounds <= 0 {
		c.Loop.StepRounds = 15
	}
	if c.Review.Passes == 0 {
		c.Review.Passes = 4
	}
	if c.Review.Passes > 0 && c.Review.MaxPasses < c.Review.Passes {
		c.Review.MaxPasses = c.Review.Passes + 2
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
	c.Computer.normalize()
}

func (c *ComputerConfig) normalize() {
	if c.MaxImageSide <= 0 {
		c.MaxImageSide = 1280
	}
	c.MaxImageSide = min(max(c.MaxImageSide, 640), 2560)
	switch c.Approval {
	case ComputerApproveActions, ComputerApproveEvery, ComputerApproveAuto:
	default:
		c.Approval = ComputerApproveActions
	}
	if c.MaxActions <= 0 {
		c.MaxActions = 200
	}
	if c.SessionMinutes <= 0 {
		c.SessionMinutes = 30
	}
	if c.MoveMillis == 0 {
		c.MoveMillis = 250
	}
	if c.Monitor < -1 {
		c.Monitor = 0
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
