package main

import (
	"fmt"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

// Domains are the kinds of work ASG does (software, docs, cyber, devops,
// pm), each with flavors — the same tracks the terminal switches with
// ctrl+t and /domain.

type FlavorInfo struct {
	Name     string `json:"name"`
	Describe string `json:"describe"`
}

type DomainInfo struct {
	Domain   string       `json:"domain"`
	Label    string       `json:"label"`
	Describe string       `json:"describe"`
	Flavors  []FlavorInfo `json:"flavors"`
}

// ListDomains returns every domain and its flavors, default flavor first.
func (a *App) ListDomains() []DomainInfo {
	var out []DomainInfo
	for _, w := range agent.Workflows {
		d := DomainInfo{Domain: string(w.Domain), Label: w.Label, Describe: w.Describe}
		for _, f := range w.Flavors {
			d.Flavors = append(d.Flavors, FlavorInfo{Name: f.Name, Describe: f.Describe})
		}
		out = append(out, d)
	}
	return out
}

// resolveTrack turns a domain/flavor pair into a valid track. An empty or
// unknown domain falls back to the saved default (shared with the
// terminal); an unknown flavor to the domain's default flavor.
func (a *App) resolveTrack(domain, flavor string) agent.Track {
	d, ok := agent.ParseDomain(domain)
	if !ok {
		cfg := a.core.Config.Get()
		if d, ok = agent.ParseDomain(cfg.Domain); !ok {
			d = agent.DomainSoftware
		}
		if flavor == "" {
			flavor = cfg.Flavor
		}
	}
	t := agent.Track{Domain: d, Flavor: agent.WorkflowFor(d).Flavors[0].Name}
	if ft, ok := t.WithFlavor(flavor); ok {
		t = ft
	}
	return t
}

// DefaultTrack is the track new chats start in.
func (a *App) DefaultTrack() agent.Track { return a.resolveTrack("", "") }

// SetDefaultTrack saves the track new chats start in (the terminal uses it too).
func (a *App) SetDefaultTrack(domain, flavor string) (agent.Track, error) {
	if _, ok := agent.ParseDomain(domain); !ok {
		return agent.Track{}, fmt.Errorf("unknown domain %q", domain)
	}
	t := a.resolveTrack(domain, flavor)
	err := a.core.Config.Update(func(c *config.Config) error {
		c.Domain, c.Flavor = string(t.Domain), t.Flavor
		return nil
	})
	return t, err
}

// SetSessionTrack switches a session's domain and flavor. The next turn
// follows the new domain's loop (and a new domain gets a fresh work folder).
func (a *App) SetSessionTrack(id, domain, flavor string) (SessionMeta, error) {
	if _, ok := agent.ParseDomain(domain); !ok {
		return SessionMeta{}, fmt.Errorf("unknown domain %q", domain)
	}
	t := a.resolveTrack(domain, flavor)
	var meta SessionMeta
	err := a.updateMeta(id, func(f *SessionFile) {
		f.Domain, f.Flavor = string(t.Domain), t.Flavor
		meta = f.SessionMeta
	})
	return meta, err
}

// applyTrack puts the session file's track on the live agent session.
// Callers hold ls.mu (or own ls exclusively).
func (a *App) applyTrack(ls *liveSession) {
	t := a.resolveTrack(ls.file.Domain, ls.file.Flavor)
	if ls.sess.Track == t {
		return
	}
	if ls.sess.Track.Domain != t.Domain {
		ls.sess.Work = nil // a new kind of work gets its own work folder
	}
	ls.sess.Track = t
}
