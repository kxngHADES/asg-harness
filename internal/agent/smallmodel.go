package agent

// Helpers that make small local models (7–14B) more dependable. Each one
// covers a habit that otherwise derails a run: writing a tool call out as
// text, repeating the same call, or burying a plan in prose.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

var (
	reToolTag   = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*(?:</tool_call>|$)`)
	reCodeFence = regexp.MustCompile("(?s)```(?:json|tool_call|tool)?\\s*\\n?(.*?)```")
)

// recoverToolCalls finds tool calls that a model wrote as text (Qwen-style
// <tool_call>{…}</tool_call>, a fenced JSON block, or a bare JSON object)
// instead of using native tool calling. Only calls to tools in byName are
// returned, and fenced or bare JSON only counts when it is essentially the
// whole message, so a JSON example inside an explanation is left alone.
func recoverToolCalls(content string, byName map[string]*tools.Tool) []llm.ToolCall {
	var candidates []string
	for _, m := range reToolTag.FindAllStringSubmatch(content, -1) {
		candidates = append(candidates, m[1])
	}
	trimmed := strings.TrimSpace(content)
	if len(candidates) == 0 && strings.HasPrefix(trimmed, "```") {
		for _, m := range reCodeFence.FindAllStringSubmatch(trimmed, -1) {
			candidates = append(candidates, m[1])
		}
	}
	if len(candidates) == 0 && strings.HasPrefix(trimmed, "{") {
		candidates = append(candidates, trimmed)
	}
	if len(candidates) == 0 && strings.Contains(content, gemmaQuote) {
		candidates = gemmaCalls(content)
	}
	var calls []llm.ToolCall
	for i, c := range candidates {
		name, args, ok := parseCallJSON(c)
		if !ok || byName[name] == nil {
			continue
		}
		calls = append(calls, llm.ToolCall{ID: fmt.Sprintf("recovered_%d_%d", time.Now().UnixNano(), i), Name: name, Args: args})
	}
	return calls
}

// gemmaQuote is the string delimiter in Gemma's native tool-call syntax,
// e.g. call:read_file{path:<|"|>a.go<|"|>}, which leaks into text output.
const gemmaQuote = `<|"|>`

var (
	reGemmaCall = regexp.MustCompile(`(?:call:)?\b([a-z_][a-z0-9_]*)\{([^{}]*)\}`)
	reBareKey   = regexp.MustCompile(`(^|,)\s*([A-Za-z_][A-Za-z0-9_]*)\s*:`)
)

// gemmaCalls converts Gemma-syntax calls into {"name","arguments"} JSON.
func gemmaCalls(content string) []string {
	var out []string
	for _, m := range reGemmaCall.FindAllStringSubmatch(content, -1) {
		body := strings.ReplaceAll(m[2], gemmaQuote, `"`)
		body = reBareKey.ReplaceAllString(body, `$1"$2":`)
		out = append(out, fmt.Sprintf(`{"name":%q,"arguments":{%s}}`, m[1], body))
	}
	return out
}

// looksLikeToolCall reports text that is a leaked tool call, not a step.
func looksLikeToolCall(s string) bool {
	return strings.Contains(s, gemmaQuote) || strings.Contains(s, "<tool_call>") || reGemmaCall.MatchString(strings.TrimSpace(s)) && strings.HasSuffix(strings.TrimSpace(s), "}")
}

// parseCallJSON accepts the shapes models commonly use for a call:
// {"name", "arguments"|"parameters"|"args"|"input"} and
// {"function": {"name", "arguments"}}; arguments may be a JSON string.
func parseCallJSON(s string) (string, json.RawMessage, bool) {
	var v struct {
		Name       string          `json:"name"`
		Tool       string          `json:"tool"`
		Arguments  json.RawMessage `json:"arguments"`
		Parameters json.RawMessage `json:"parameters"`
		Args       json.RawMessage `json:"args"`
		Input      json.RawMessage `json:"input"`
		Function   *struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &v); err != nil {
		return "", nil, false
	}
	name, args := v.Name, v.Arguments
	if name == "" {
		name = v.Tool
	}
	if v.Function != nil {
		name, args = v.Function.Name, v.Function.Arguments
	}
	for _, alt := range []json.RawMessage{v.Parameters, v.Args, v.Input} {
		if len(args) == 0 {
			args = alt
		}
	}
	var str string
	if json.Unmarshal(args, &str) == nil {
		args = json.RawMessage(str)
	}
	if len(args) == 0 || !json.Valid(args) || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	return name, args, name != ""
}

// canonicalArgs renders arguments with sorted keys so equal calls compare
// equal however the model formatted them.
func canonicalArgs(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

var (
	reStep     = regexp.MustCompile(`^(\s*)(?:\d{1,2}[.)]|[-*]\s*\[[ xX]?\]|(?i:step)\s*\d{1,2}[:.)])\s+(.+)$`)
	reMDMarkup = regexp.MustCompile(`\*\*|__|^#+\s*`)
)

// maxSteps keeps plans short enough for a small model to hold in mind.
const maxSteps = 12

// ParseSteps pulls the top-level steps out of a plan written as a numbered
// list ("1. …", "2) …", "Step 3: …") or a checklist ("- [ ] …"). Indented
// lines under a step are kept as its detail. Plain prose yields nothing.
func ParseSteps(text string) []string {
	type step struct {
		indent int
		title  string
		detail []string
	}
	var steps []*step
	minIndent := -1
	var cur *step
	for _, line := range strings.Split(text, "\n") {
		if m := reStep.FindStringSubmatch(line); m != nil {
			indent := len(strings.ReplaceAll(m[1], "\t", "    "))
			if minIndent < 0 || indent < minIndent {
				minIndent = indent
			}
			if cur != nil && indent > cur.indent {
				cur.detail = append(cur.detail, cleanStep(m[2]))
				continue
			}
			cur = &step{indent: indent, title: cleanStep(m[2])}
			steps = append(steps, cur)
			continue
		}
		if cur != nil && strings.TrimSpace(line) != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			cur.detail = append(cur.detail, cleanStep(strings.TrimLeft(strings.TrimSpace(line), "-*• ")))
			continue
		}
		if strings.TrimSpace(line) != "" {
			cur = nil // prose ends the current step's detail
		}
	}
	var out []string
	for _, s := range steps {
		if s.indent != minIndent || s.title == "" || looksLikeToolCall(s.title) {
			continue
		}
		t := s.title
		if len(s.detail) > 0 {
			t += " — " + clip(strings.Join(s.detail, "; "), 300)
		}
		out = append(out, t)
		if len(out) == maxSteps {
			break
		}
	}
	return out
}

func cleanStep(s string) string {
	return strings.TrimSpace(reMDMarkup.ReplaceAllString(strings.TrimSpace(s), ""))
}
