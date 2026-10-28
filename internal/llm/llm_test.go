package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicMergesToolResults(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi", Images: []Image{{MediaType: "image/png", Data: []byte{1}}}},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Name: "a", Args: json.RawMessage(`{}`)}, {ID: "2", Name: "b", Args: json.RawMessage(`{}`)}}},
		{Role: RoleTool, ToolCallID: "1", Content: "r1"},
		{Role: RoleTool, ToolCallID: "2", Content: "r2"},
	}
	out := toAnthropic(msgs)
	if len(out) != 3 {
		t.Fatalf("want 3 turns, got %d", len(out))
	}
	if blocks := out[2]["content"].([]map[string]any); len(blocks) != 2 || blocks[1]["tool_use_id"] != "2" {
		t.Fatalf("tool results not merged: %v", out[2])
	}
	if first := out[0]["content"].([]map[string]any); first[0]["type"] != "image" {
		t.Fatalf("image block missing: %v", first)
	}
}

func TestReadSSE(t *testing.T) {
	var got []string
	readSSE(strings.NewReader("event: a\ndata: 1\n\ndata: 2\ndata: 3\n\n"), func(e, d string) error {
		got = append(got, e+"="+d)
		return nil
	})
	if strings.Join(got, ",") != "a=1,=2\n3" {
		t.Fatalf("got %q", got)
	}
}

func TestRawArgs(t *testing.T) {
	if string(rawArgs("")) != "{}" || string(rawArgs("{bad")) != "{}" || string(rawArgs(`{"a":1}`)) != `{"a":1}` {
		t.Fatal("rawArgs")
	}
}
