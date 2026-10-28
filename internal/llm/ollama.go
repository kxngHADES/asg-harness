package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Ollama talks to the native Ollama /api/chat endpoint.
type Ollama struct {
	BaseURL string
	Client  *http.Client
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaMsg struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
}

func (o *Ollama) Chat(ctx context.Context, req Request, onText func(string)) (*Response, error) {
	var msgs []ollamaMsg
	if req.System != "" {
		msgs = append(msgs, ollamaMsg{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		om := ollamaMsg{Role: string(m.Role), Content: m.Content, ToolName: m.ToolName}
		for _, img := range m.Images {
			om.Images = append(om.Images, base64.StdEncoding.EncodeToString(img.Data))
		}
		for _, tc := range m.ToolCalls {
			var c ollamaToolCall
			c.Function.Name = tc.Name
			c.Function.Arguments = tc.Args
			om.ToolCalls = append(om.ToolCalls, c)
		}
		msgs = append(msgs, om)
	}
	body := map[string]any{"model": req.Model, "messages": msgs, "stream": true}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": t})
		}
		body["tools"] = tools
	}
	opts := map[string]any{}
	if req.Temperature != nil {
		opts["temperature"] = *req.Temperature
	}
	if len(opts) > 0 {
		body["options"] = opts
	}
	data, _ := json.Marshal(body)
	hreq, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := o.Client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w (is `ollama serve` running?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama: %w", apiError(resp))
	}

	out := &Response{Message: Message{Role: RoleAssistant}}
	var text strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Message         ollamaMsg `json:"message"`
			Done            bool      `json:"done"`
			Error           string    `json:"error"`
			PromptEvalCount int       `json:"prompt_eval_count"`
			EvalCount       int       `json:"eval_count"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		if chunk.Error != "" {
			return nil, fmt.Errorf("ollama: %s", chunk.Error)
		}
		if c := chunk.Message.Content; c != "" {
			text.WriteString(c)
			if onText != nil {
				onText(c)
			}
		}
		for _, tc := range chunk.Message.ToolCalls {
			args := tc.Function.Arguments
			// Some models return arguments as a JSON-encoded string.
			var s string
			if json.Unmarshal(args, &s) == nil {
				args = rawArgs(s)
			}
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage("{}")
			}
			out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{ID: newID(), Name: tc.Function.Name, Args: args})
		}
		if chunk.Done {
			out.Usage = Usage{InputTokens: chunk.PromptEvalCount, OutputTokens: chunk.EvalCount}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out.Message.Content = text.String()
	return out, nil
}

func (o *Ollama) ListModels(ctx context.Context) ([]string, error) {
	hreq, err := http.NewRequestWithContext(ctx, "GET", o.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.Client.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, apiError(resp)
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	names := []string{}
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// ModelInfo is the subset of /api/show the harness uses.
type ModelInfo struct {
	Capabilities []string `json:"capabilities"`
	Voices       []string `json:"voices"`
}

// Has reports whether the model advertises a capability such as "speech".
func (m ModelInfo) Has(capability string) bool {
	for _, c := range m.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// Show returns a model's capabilities and (for speech models) voices.
func (o *Ollama) Show(ctx context.Context, model string) (ModelInfo, error) {
	var info ModelInfo
	body, _ := json.Marshal(map[string]string{"model": model})
	hreq, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return info, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := o.Client.Do(hreq)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return info, apiError(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&info)
	return info, err
}
