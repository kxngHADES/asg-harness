package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// Asker is implemented by front ends that can put a question to the user
// in the middle of a turn and wait for the answer.
type Asker interface {
	AskUser(ctx context.Context, question string, choices []string) (string, error)
}

// evKey carries the turn's Events through the context so tools that need
// the front end (ask_user) can reach it.
type evKey struct{}

func withEvents(ctx context.Context, ev Events) context.Context {
	return context.WithValue(ctx, evKey{}, ev)
}

func askerFrom(ctx context.Context) Asker {
	ev, _ := ctx.Value(evKey{}).(Events)
	for ev != nil {
		if a, ok := ev.(Asker); ok {
			return a
		}
		q, ok := ev.(quiet)
		if !ok {
			return nil
		}
		ev = q.Events
	}
	return nil
}

// NoUserAnswer is what the model is told when nobody can answer.
const NoUserAnswer = "No user is available to answer right now. Make the most reasonable assumption, state it clearly in your answer (and in notes.md if you keep working files), and continue."

// askTool lets the model stop at any moment and ask the user for missing
// information instead of guessing.
func askTool() *tools.Tool {
	return &tools.Tool{
		Spec: llm.ToolSpec{
			Name: "ask_user",
			Description: "Pause and ask the user a question, then continue with their answer. Use it whenever information you need is missing " +
				"and cannot be found in the project (requirements, credentials, scope, a decision only they can make). Ask one clear question at a time.",
			Parameters: map[string]any{"type": "object", "required": []string{"question"},
				"properties": map[string]any{
					"question": map[string]any{"type": "string", "description": "The question, with just enough context to answer it"},
					"choices": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
						"description": "Optional suggested answers; the user can still type their own"},
				}},
		},
		Source:   "builtin",
		Describe: func(a json.RawMessage) string { return oneLineClip(fieldOf(a, "question"), 200) },
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Question string   `json:"question"`
				Choices  []string `json:"choices"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			if strings.TrimSpace(a.Question) == "" {
				return "", errors.New("question is empty")
			}
			asker := askerFrom(ctx)
			if asker == nil {
				return NoUserAnswer, nil
			}
			ans, err := asker.AskUser(ctx, strings.TrimSpace(a.Question), a.Choices)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(ans) == "" {
				return "The user gave no answer. Make the most reasonable assumption, state it, and continue.", nil
			}
			return "The user answered: " + ans, nil
		},
	}
}

func fieldOf(raw json.RawMessage, key string) string {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}
