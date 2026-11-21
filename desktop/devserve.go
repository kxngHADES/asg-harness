//go:build devserve

// Development server: serves the desktop UI to a normal browser with the
// real Go backend behind it, so the frontend can be debugged with browser
// dev tools. It binds to 127.0.0.1 only.
//
//	go run -tags devserve . [-addr 127.0.0.1:7788]
//
// Calls to window.go.main.App.X(...) become POST /call/X with a JSON array
// of arguments; backend events stream over /events (server-sent events).
// Native dialogs are replaced by a browser prompt.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

const shim = `<script>
(() => {
  const call = async (name, args) => {
    const r = await fetch("/call/" + name, { method: "POST", body: JSON.stringify(args) });
    const t = await r.text();
    if (!r.ok) throw t;
    return t ? JSON.parse(t) : null;
  };
  const App = new Proxy({}, { get: (_, name) => (...args) => call(name, args) });
  window.go = { main: { App } };
  const handlers = {};
  const es = new EventSource("/events");
  es.onmessage = (m) => { const d = JSON.parse(m.data); (handlers[d.name] || []).forEach((f) => f(d.data)); };
  const noop = () => {};
  window.runtime = {
    EventsOn: (n, f) => { (handlers[n] ||= []).push(f); return () => {}; },
    WindowMinimise: noop, WindowToggleMaximise: noop, Quit: noop, WindowSetTitle: (t) => (document.title = t),
    BrowserOpenURL: (u) => window.open(u, "_blank"),
  };
})();
</script>`

func main() {
	addr := flag.String("addr", "127.0.0.1:7788", "listen address (keep it on 127.0.0.1)")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	app := NewApp(agent.NewApp(cfg))

	var mu sync.Mutex
	clients := map[chan []byte]bool{}
	app.emitHook = func(name string, data any) {
		b, _ := json.Marshal(map[string]any{"name": name, "data": data})
		mu.Lock()
		for c := range clients {
			select {
			case c <- b:
			default:
			}
		}
		mu.Unlock()
	}
	// The browser answers folder picks through /pick (see the prompt below).
	picks := make(chan string)
	app.pickHook = func(title string) (string, error) {
		app.emit("dev:pick", title)
		p := <-picks
		if p == "" {
			return "", nil
		}
		return p, nil
	}
	app.startup(nil)

	dist, _ := fs.Sub(assets, "frontend/dist")
	files := http.FileServer(http.FS(dist))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data, _ := fs.ReadFile(dist, "index.html")
		extra := shim + `<script>window.addEventListener("DOMContentLoaded",()=>runtime.EventsOn("dev:pick",async(t)=>{const p=prompt(t+"\n(full folder path)")||"";fetch("/pick",{method:"POST",body:p});}));</script>`
		data = bytes.Replace(data, []byte("<head>"), []byte("<head>"+extra), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
	mux.Handle("GET /", files)
	mux.HandleFunc("POST /pick", func(w http.ResponseWriter, r *http.Request) {
		b := new(bytes.Buffer)
		b.ReadFrom(r.Body)
		select {
		case picks <- strings.TrimSpace(b.String()):
		default:
		}
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		f, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		c := make(chan []byte, 256)
		mu.Lock()
		clients[c] = true
		mu.Unlock()
		defer func() { mu.Lock(); delete(clients, c); mu.Unlock() }()
		f.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-c:
				fmt.Fprintf(w, "data: %s\n\n", b)
				f.Flush()
			}
		}
	})
	mux.HandleFunc("POST /call/{name}", func(w http.ResponseWriter, r *http.Request) {
		out, err := invoke(app, r.PathValue("name"), r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	})
	fmt.Printf("ASG desktop UI (dev): http://%s\n", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// invoke calls an exported App method with JSON arguments, the way the
// Wails bridge does.
func invoke(app *App, name string, r *http.Request) ([]byte, error) {
	m := reflect.ValueOf(app).MethodByName(name)
	if !m.IsValid() {
		return nil, fmt.Errorf("no method %s", name)
	}
	var raw []json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, err
	}
	t := m.Type()
	if len(raw) != t.NumIn() {
		return nil, fmt.Errorf("%s takes %d arguments, got %d", name, t.NumIn(), len(raw))
	}
	args := make([]reflect.Value, t.NumIn())
	for i := range args {
		v := reflect.New(t.In(i))
		if err := json.Unmarshal(raw[i], v.Interface()); err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		args[i] = v.Elem()
	}
	res := m.Call(args)
	var values []any
	for _, v := range res {
		if v.Type() == reflect.TypeOf((*error)(nil)).Elem() {
			if !v.IsNil() {
				return nil, v.Interface().(error)
			}
			continue
		}
		values = append(values, v.Interface())
	}
	switch len(values) {
	case 0:
		return []byte("null"), nil
	case 1:
		return json.Marshal(values[0])
	}
	if len(values) > 1 {
		return json.Marshal(values)
	}
	return nil, errors.New("unreachable")
}
