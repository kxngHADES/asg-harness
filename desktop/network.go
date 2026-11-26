package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultNetworkPort is the LAN port the desktop app listens on for mobile
	// clients (HTTP JSON/SSE API + UDP discovery).
	DefaultNetworkPort = 7789
	discoverProbe      = "ASG_DISCOVER"
)

// networkBridge exposes the live desktop App to mobile clients on the local
// network so chats, streaming turns, approvals, and sessions stay in sync.
type networkBridge struct {
	app  *App
	addr string
	port int

	srv *http.Server
	udp *net.UDPConn

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// allowedRemoteMethods restricts which exported App methods a network client
// may invoke via POST /api/call/{name}. Native OS dialogs and QuitApp stay local.
var allowedRemoteMethods = map[string]bool{
	"Bootstrap":            true,
	"ListSessions":         true,
	"NewSession":           true,
	"OpenSession":          true,
	"Send":                 true,
	"Stop":                 true,
	"Approve":              true,
	"Answer":               true,
	"RenameSession":        true,
	"PinSession":           true,
	"SetSessionMode":       true,
	"MoveSessionToProject": true,
	"DeleteSession":        true,
	"ListProjects":         true,
	"ListArtifacts":        true,
	"GetModel":             true,
	"SetModel":             true,
	"ListModels":           true,
	"RenderMarkdown":       true,
	"ListDomains":          true,
	"SetSessionTrack":      true,
	"SetDefaultTrack":      true,
	// The phone can watch and stop computer use, but not change its settings.
	"ComputerInfo":   true,
	"ComputerPause":  true,
	"ComputerResume": true,
	"ComputerStop":   true,
}

type networkInfo struct {
	App       string   `json:"app"`
	Hostname  string   `json:"hostname"`
	OS        string   `json:"os"`
	Version   string   `json:"version"`
	Port      int      `json:"port"`
	Provider  string   `json:"provider"`
	Model     string   `json:"model"`
	Addresses []string `json:"addresses"`
}

func newNetworkBridge(app *App, addr string) *networkBridge {
	if addr == "" {
		addr = fmt.Sprintf(":%d", DefaultNetworkPort)
	}
	return &networkBridge{
		app:     app,
		addr:    addr,
		clients: map[chan []byte]struct{}{},
	}
}

func (n *networkBridge) start() error {
	ln, err := net.Listen("tcp", n.addr)
	if err != nil {
		return err
	}
	if ta, ok := ln.Addr().(*net.TCPAddr); ok {
		n.port = ta.Port
	}
	n.srv = &http.Server{
		Handler:           n.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = n.srv.Serve(ln)
	}()
	n.startUDP()
	return nil
}

func (n *networkBridge) stop() {
	if n.udp != nil {
		_ = n.udp.Close()
	}
	if n.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = n.srv.Shutdown(ctx)
	}
}

func (n *networkBridge) startUDP() {
	if n.port == 0 {
		return
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: n.port})
	if err != nil {
		return
	}
	n.udp = conn
	go func() {
		buf := make([]byte, 512)
		for {
			k, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if strings.TrimSpace(string(buf[:k])) != discoverProbe {
				continue
			}
			b, err := json.Marshal(n.info())
			if err == nil {
				_, _ = conn.WriteToUDP(b, raddr)
			}
		}
	}()
}

func (n *networkBridge) broadcast(name string, data any) {
	b, err := json.Marshal(map[string]any{"name": name, "data": data})
	if err != nil {
		return
	}
	n.mu.Lock()
	for ch := range n.clients {
		select {
		case ch <- b:
		default:
		}
	}
	n.mu.Unlock()
}

func (n *networkBridge) info() networkInfo {
	host, _ := os.Hostname()
	mi := n.app.modelInfo()
	return networkInfo{
		App:       "asg-desktop",
		Hostname:  host,
		OS:        osName(),
		Version:   version,
		Port:      n.port,
		Provider:  mi.Provider,
		Model:     mi.Model,
		Addresses: localIPv4s(n.port),
	}
}

func (n *networkBridge) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(n.info())
	})
	mux.HandleFunc("GET /api/events", n.handleEvents)
	mux.HandleFunc("POST /api/call/{name}", n.handleCall)
	return withCORS(mux)
}

func (n *networkBridge) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan []byte, 256)
	n.mu.Lock()
	n.clients[ch] = struct{}{}
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.clients, ch)
		n.mu.Unlock()
	}()

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case b := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

func (n *networkBridge) handleCall(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !allowedRemoteMethods[name] {
		http.Error(w, fmt.Sprintf("method %q is not available over the network", name), http.StatusForbidden)
		return
	}
	out, err := invokeRemote(n.app, name, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func invokeRemote(app *App, name string, r *http.Request) ([]byte, error) {
	m := reflect.ValueOf(app).MethodByName(name)
	if !m.IsValid() {
		return nil, fmt.Errorf("no method %s", name)
	}
	var raw []json.RawMessage
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil && err.Error() != "EOF" {
			return nil, err
		}
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
	default:
		if len(values) > 1 {
			return json.Marshal(values)
		}
	}
	return nil, errors.New("unreachable")
}

func localIPv4s(port int) []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, fmt.Sprintf("%s:%d", ip.String(), port))
		}
	}
	return out
}

func osName() string {
	return os.Getenv("OS")
}
