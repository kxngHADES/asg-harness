package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Anthropic talks to the Anthropic Messages API.
type Anthropic struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

const anthropicVersion = "2023-06-01"

func (a *Anthropic) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	hreq, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("x-api-key", a.APIKey)
	hreq.Header.Set("anthropic-version", anthropicVersion)
	return a.Client.Do(hreq)
}

// toAnthropic converts neutral messages, merging consecutive tool results
// into a single user turn as the API requires.
func toAnthropic(msgs []Message) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		var blocks []map[string]any
		role := "user"
		switch m.Role {
		case RoleAssistant:
			role = "assistant"
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": tc.Args})
			}
		case RoleTool:
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content})
		default:
			for _, img := range m.Images {
				blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{
					"type": "base64", "media_type": img.MediaType, "data": base64.StdEncoding.EncodeToString(img.Data),
				}})
			}
			if m.Content != "" || len(blocks) == 0 {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
		}
		if len(blocks) == 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1]["role"] == role {
			out[n-1]["content"] = append(out[n-1]["content"].([]map[string]any), blocks...)
			continue
		}
		out = append(out, map[string]any{"role": role, "content": blocks})
	}
	return out
}

func (a *Anthropic) Chat(ctx context.Context, req Request, onText func(string)) (*Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 16000
	}
	body := map[string]any{
		"model": req.Model, "max_tokens": maxTokens, "stream": true,
		"messages": toAnthropic(req.Messages),
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
		}
		body["tools"] = tools
	}
	data, _ := json.Marshal(body)
	resp, err := a.do(ctx, "POST", "/v1/messages", data)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic: %w", apiError(resp))
	}

	type block struct {
		kind, id, name string
		buf            strings.Builder
	}
	blocks := map[int]*block{}
	var order []int
	out := &Response{Message: Message{Role: RoleAssistant}}
	var text strings.Builder
	err = readSSE(resp.Body, func(event, payload string) error {
		var ev struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
				Text string `json:"text"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return nil
		}
		switch ev.Type {
		case "error":
			return fmt.Errorf("anthropic: %s", ev.Error.Message)
		case "message_start":
			out.Usage.InputTokens = ev.Message.Usage.InputTokens
		case "content_block_start":
			b := &block{kind: ev.ContentBlock.Type, id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
			blocks[ev.Index] = b
			order = append(order, ev.Index)
		case "content_block_delta":
			b := blocks[ev.Index]
			if b == nil {
				return nil
			}
			switch ev.Delta.Type {
			case "text_delta":
				text.WriteString(ev.Delta.Text)
				if onText != nil {
					onText(ev.Delta.Text)
				}
			case "input_json_delta":
				b.buf.WriteString(ev.Delta.PartialJSON)
			}
		case "message_delta":
			out.Usage.OutputTokens = ev.Usage.OutputTokens
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, i := range order {
		b := blocks[i]
		if b.kind == "tool_use" {
			out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{ID: b.id, Name: b.name, Args: rawArgs(b.buf.String())})
		}
	}
	out.Message.Content = text.String()
	return out, nil
}

func (a *Anthropic) ListModels(ctx context.Context) ([]string, error) {
	resp, err := a.do(ctx, "GET", "/v1/models?limit=100", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, apiError(resp)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	ids := []string{}
	for _, m := range list.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}
