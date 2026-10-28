package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/config"
)

// fakeOllama answers /api/show: "talker" can speak, "gemma" can't.
func fakeOllama(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model string }
		json.NewDecoder(r.Body).Decode(&req)
		caps := []string{"completion"}
		if req.Model == "talker" {
			caps = append(caps, "speech")
		}
		json.NewEncoder(w).Encode(map[string]any{"capabilities": caps})
	}))
}

func newTestApp(t *testing.T, base, model string) *App {
	t.Setenv("ASG_HOME", t.TempDir())
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	store.Update(func(c *config.Config) error {
		c.ActiveProvider = "ollama"
		c.Providers["ollama"].BaseURL = base
		c.Providers["ollama"].Model = model
		c.Audio.Fallback = config.VoiceSpec{Engine: "ollama", Model: "phonon", Voice: "Juniper"}
		return nil
	})
	return NewApp(store)
}

func TestResolveVoice(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()
	ctx := context.Background()

	// A model that can speak reads its own replies.
	app := newTestApp(t, srv.URL, "talker")
	if o := app.ResolveVoice(ctx); o.Engine != "ollama" || o.Model != "talker" || o.BaseURL != srv.URL+"/v1" {
		t.Fatalf("talker: %+v", o)
	}

	// A model that can't falls back to phonon/Juniper.
	app = newTestApp(t, srv.URL, "gemma")
	if o := app.ResolveVoice(ctx); o.Model != "phonon" || o.Voice != "Juniper" {
		t.Fatalf("gemma: %+v", o)
	}

	// A pin beats both.
	app.Config.Update(func(c *config.Config) error {
		c.Audio.PerModel = map[string]config.VoiceSpec{"gemma": {Engine: "system", Voice: "Samantha"}}
		return nil
	})
	if o := app.ResolveVoice(ctx); o.Engine != "system" || o.Voice != "Samantha" {
		t.Fatalf("pinned: %+v", o)
	}
}

func TestLegacyAudioConfigMigrates(t *testing.T) {
	c := config.Default()
	c.Audio.OpenAIProvider = "myopenai"
	c.Audio.Engine = "openai"
	data, _ := json.Marshal(c)
	var back config.Config
	json.Unmarshal(data, &back)
	t.Setenv("ASG_HOME", t.TempDir())
	store, _ := config.Load()
	store.Replace(back)
	if got := store.Get().Audio.Provider; got != "myopenai" {
		t.Fatalf("provider = %q", got)
	}
}
