package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

func testServer(t *testing.T) *Server {
	t.Setenv("ASG_HOME", t.TempDir())
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	store.Update(func(c *config.Config) error { c.Providers["anthropic"].APIKey = "sk-ant-secret-1234"; return nil })
	return newServer(agent.NewApp(store))
}

func do(s *Server, method, path, host, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = host
	if token != "" {
		req.Header.Set("X-ASG-Token", token)
	}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}

func TestGuard(t *testing.T) {
	s := testServer(t)
	if c := do(s, "GET", "/api/state", "127.0.0.1:7777", "", "").Code; c != http.StatusUnauthorized {
		t.Errorf("no token: %d", c)
	}
	if c := do(s, "GET", "/api/state", "evil.example:7777", s.token, "").Code; c != http.StatusForbidden {
		t.Errorf("foreign host (DNS rebinding): %d", c)
	}
	if c := do(s, "GET", "/api/state", "localhost:7777", s.token, "").Code; c != 200 {
		t.Errorf("valid: %d", c)
	}
}

func TestKeysMaskedAndPreserved(t *testing.T) {
	s := testServer(t)
	rec := do(s, "GET", "/api/state", "127.0.0.1:1", s.token, "")
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("API key leaked to browser")
	}
	var st struct{ Config config.Config }
	json.Unmarshal(rec.Body.Bytes(), &st)
	st.Config.Providers["anthropic"].Model = "changed"
	body, _ := json.Marshal(st.Config)
	if c := do(s, "PUT", "/api/config", "127.0.0.1:1", s.token, string(body)).Code; c != 200 {
		t.Fatalf("put: %d", c)
	}
	got := s.app.Config.Get().Providers["anthropic"]
	if got.APIKey != "sk-ant-secret-1234" || got.Model != "changed" {
		t.Fatalf("masked key round-trip broke config: %+v", got)
	}
}
