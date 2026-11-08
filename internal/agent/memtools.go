package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/memory"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// memoryTools let the model keep notes between sessions. They only touch
// files under ~/.asg, so they run without a permission gate.
func memoryTools(m *memory.Manager) []*tools.Tool {
	type args struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Scope   string `json:"scope"`
	}
	scope := map[string]any{"type": "string", "enum": []string{memory.User, memory.Project},
		"description": `"user" applies in every project (default); "project" only in this directory`}
	describe := func(raw json.RawMessage) string {
		var a args
		_ = json.Unmarshal(raw, &a)
		if a.Scope == "" {
			a.Scope = memory.User
		}
		return fmt.Sprintf("%s (%s)", memory.Slug(a.Name), a.Scope)
	}
	return []*tools.Tool{
		{
			Spec: llm.ToolSpec{
				Name:        "memory_save",
				Description: "Save or replace a persistent Markdown note that will be shown to you in future sessions. Use for lasting user preferences, conventions and facts.",
				Parameters: map[string]any{"type": "object", "required": []string{"name", "content"},
					"properties": map[string]any{
						"name":    map[string]any{"type": "string", "description": "Short kebab-case name, e.g. testing-framework"},
						"content": map[string]any{"type": "string", "description": "Markdown body: the fact and, if useful, why"},
						"scope":   scope,
					}},
			},
			Describe: describe,
			Run: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a args
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				p, err := m.Save(a.Scope, a.Name, a.Content)
				if err != nil {
					return "", err
				}
				return "saved " + p, nil
			},
		},
		{
			Spec: llm.ToolSpec{
				Name:        "memory_delete",
				Description: "Delete a persistent note that is wrong or no longer wanted.",
				Parameters: map[string]any{"type": "object", "required": []string{"name"},
					"properties": map[string]any{
						"name":  map[string]any{"type": "string", "description": "Note name"},
						"scope": scope,
					}},
			},
			Describe: describe,
			Run: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a args
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				if err := m.Delete(a.Scope, a.Name); err != nil {
					return "", err
				}
				return "deleted " + memory.Slug(a.Name), nil
			},
		},
	}
}
