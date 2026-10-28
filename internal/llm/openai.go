package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// OpenAI talks to any OpenAI-compatible /chat/completions endpoint
// (OpenAI, OpenRouter, Groq, LM Studio, vLLM, Ollama's /v1, ...).
type OpenAI struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (o *OpenAI) Chat(ctx context.Context, req Request, onText func(string)) (*Response, error) {
	var msgs []map[string]any
	if req.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleTool:
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Content})
		case RoleAssistant:
			am := map[string]any{"role": "assistant", "content": m.Content}
			if len(m.ToolCalls) > 0 {
				var calls []map[string]any
				for _, tc := range m.ToolCalls {
					calls = append(calls, map[string]any{
						"id": tc.ID, "type": "function",
						"function": map[string]any{"name": tc.Name, "arguments": string(tc.Args)},
					})
				}
				am["tool_calls"] = calls
			}
			msgs = append(msgs, am)
		default:
			if len(m.Images) == 0 {
				msgs = append(msgs, map[string]any{"role": string(m.Role), "content": m.Content})
				continue
			}
			parts := []map[string]any{{"type": "text", "text": m.Content}}
			for _, img := range m.Images {
				url := "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			}
			msgs = append(msgs, map[string]any{"role": string(m.Role), "content": parts})
		}
	}
	body := map[string]any{
		"model": req.Model, "messages": msgs, "stream": true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": t})
		}
		body["tools"] = tools
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_completion_tokens"] = req.MaxTokens
	}
	data, _ := json.Marshal(body)
	hreq, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := o.Client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai: %w", apiError(resp))
	}

	type partial struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*partial{}
	var text strings.Builder
	out := &Response{Message: Message{Role: RoleAssistant}}
	err = readSSE(resp.Body, func(_, payload string) error {
		if payload == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil
		}
		if chunk.Error != nil {
			return fmt.Errorf("openai: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			out.Usage = Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				if onText != nil {
					onText(ch.Delta.Content)
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				p := calls[tc.Index]
				if p == nil {
					p = &partial{}
					calls[tc.Index] = p
				}
				if tc.ID != "" {
					p.id = tc.ID
				}
				if tc.Function.Name != "" {
					p.name += tc.Function.Name
				}
				p.args.WriteString(tc.Function.Arguments)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		p := calls[i]
		id := p.id
		if id == "" {
			id = newID()
		}
		out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{ID: id, Name: p.name, Args: rawArgs(p.args.String())})
	}
	out.Message.Content = text.String()
	return out, nil
}

func (o *OpenAI) ListModels(ctx context.Context) ([]string, error) {
	hreq, err := http.NewRequestWithContext(ctx, "GET", o.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if o.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := o.Client.Do(hreq)
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
	sort.Strings(ids)
	return ids, nil
}
