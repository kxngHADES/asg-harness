// Package llm contains a provider-neutral chat model and adapters for
// Ollama, OpenAI-compatible APIs and Anthropic.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/config"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Image struct {
	MediaType string `json:"media_type"` // e.g. image/png
	Data      []byte `json:"data"`
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Images     []Image    `json:"images,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
}

// ToolSpec is a JSON-schema described tool offered to the model.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	Temperature *float64
	MaxTokens   int
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type Response struct {
	Message Message
	Usage   Usage
}

// Provider streams a chat completion. onText is called with each text delta.
type Provider interface {
	Chat(ctx context.Context, req Request, onText func(string)) (*Response, error)
	ListModels(ctx context.Context) ([]string, error)
}

// New builds a provider from config.
func New(pc *config.ProviderConfig) (Provider, error) {
	if pc == nil {
		return nil, fmt.Errorf("no provider configured")
	}
	client := &http.Client{Timeout: 0} // streaming; rely on context for cancellation
	base := strings.TrimRight(pc.BaseURL, "/")
	switch pc.Type {
	case "ollama":
		if base == "" {
			base = "http://localhost:11434"
		}
		return &Ollama{BaseURL: base, Client: client}, nil
	case "openai":
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		return &OpenAI{BaseURL: base, APIKey: pc.APIKey, Client: client}, nil
	case "anthropic":
		if base == "" {
			base = "https://api.anthropic.com"
		}
		return &Anthropic{BaseURL: base, APIKey: pc.APIKey, Client: client}, nil
	}
	return nil, fmt.Errorf("unknown provider type %q (want ollama, openai or anthropic)", pc.Type)
}

func newID() string {
	return fmt.Sprintf("call_%d", time.Now().UnixNano())
}

func apiError(resp *http.Response) error {
	var buf strings.Builder
	b := make([]byte, 4096)
	n, _ := resp.Body.Read(b)
	buf.Write(b[:n])
	return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(buf.String()))
}

// rawArgs makes sure tool arguments are a valid JSON object.
func rawArgs(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" || !json.Valid([]byte(s)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}
