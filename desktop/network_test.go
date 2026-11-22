package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

func testDesktopApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("ASG_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	a := NewApp(agent.NewApp(cfg))
	if err := a.store.Init(); err != nil {
		t.Fatalf("store.Init: %v", err)
	}
	return a
}

func TestNetworkBridgeInfoAndRPC(t *testing.T) {
	a := testDesktopApp(t)
	nb := newNetworkBridge(a, "127.0.0.1:0")
	a.netBridge = nb
	ts := httptest.NewServer(nb.routes())
	defer ts.Close()

	// GET /api/info
	resp, err := http.Get(ts.URL + "/api/info")
	if err != nil {
		t.Fatalf("GET /api/info: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("info status = %d, want 200", resp.StatusCode)
	}
	var info networkInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if info.App != "asg-desktop" {
		t.Fatalf("info.App = %q, want asg-desktop", info.App)
	}

	// Disallowed method is rejected with 403.
	resp2, err := http.Post(ts.URL+"/api/call/QuitApp", "application/json", strings.NewReader("[]"))
	if err != nil {
		t.Fatalf("POST QuitApp: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("QuitApp status = %d, want 403", resp2.StatusCode)
	}

	// Allowed method: NewSession + ListSessions round-trip.
	body := `[{"kind":"chat","mode":"build","title":"From mobile"}]`
	resp3, err := http.Post(ts.URL+"/api/call/NewSession", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST NewSession: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("NewSession status = %d, want 200", resp3.StatusCode)
	}
	var created SessionMeta
	if err := json.NewDecoder(resp3.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.Title != "From mobile" || created.ID == "" {
		t.Fatalf("unexpected session: %+v", created)
	}

	resp4, err := http.Post(ts.URL+"/api/call/ListSessions", "application/json", bytes.NewReader([]byte("[]")))
	if err != nil {
		t.Fatalf("POST ListSessions: %v", err)
	}
	defer resp4.Body.Close()
	var list []SessionMeta
	if err := json.NewDecoder(resp4.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("ListSessions = %+v, want session %s", list, created.ID)
	}
}

func TestNetworkBridgeSSEBroadcast(t *testing.T) {
	a := testDesktopApp(t)
	nb := newNetworkBridge(a, "127.0.0.1:0")
	a.netBridge = nb
	ts := httptest.NewServer(nb.routes())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	// First line is ": connected"
	if !sc.Scan() || !strings.HasPrefix(sc.Text(), ": connected") {
		t.Fatalf("expected connected comment, got %q", sc.Text())
	}

	a.emit("asg:turn", map[string]any{"session": "s1", "type": "user", "text": "hello from mobile"})

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			var ev struct {
				Name string         `json:"name"`
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				t.Fatalf("unmarshal sse: %v", err)
			}
			if ev.Name == "asg:turn" && ev.Data["text"] == "hello from mobile" {
				return
			}
		}
	}
	t.Fatalf("did not receive broadcast event: %v", sc.Err())
}
