package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule runs a prompt on a timer while the app is open. Each run is a
// new session (kind "scheduled") so its history and artifacts are kept.
type Schedule struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	ProjectID string `json:"project_id,omitempty"`
	// Workdir runs the prompt in a folder (like a coding session); empty
	// uses the project's folder, or a fresh sandbox.
	Workdir string `json:"workdir,omitempty"`
	Mode    string `json:"mode"` // build | plan | ask
	// Every is minutes | hourly | daily | weekdays | weekly.
	Every    string `json:"every"`
	Interval int    `json:"interval,omitempty"` // minutes, for "minutes"
	At       string `json:"at,omitempty"`       // "HH:MM" for daily, weekdays, weekly
	Weekday  int    `json:"weekday,omitempty"`  // 0 = Sunday, for weekly
	Enabled  bool   `json:"enabled"`
	// AllowTools approves every tool call without asking. Off by default:
	// nobody is there to answer a permission gate, so gated calls are denied.
	AllowTools  bool      `json:"allow_tools,omitempty"`
	Created     time.Time `json:"created"`
	LastRun     time.Time `json:"last_run,omitempty"`
	NextRun     time.Time `json:"next_run,omitempty"`
	LastSession string    `json:"last_session,omitempty"`
	LastStatus  string    `json:"last_status,omitempty"`
	Runs        int       `json:"runs,omitempty"`
}

// parseClock reads "HH:MM"; anything else is 09:00.
func parseClock(s string) (int, int) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 9, 0
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 9, 0
	}
	return hh, mm
}

// NextAfter returns the first run time strictly after t.
func (s Schedule) NextAfter(t time.Time) time.Time {
	hh, mm := parseClock(s.At)
	at := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, t.Location())
	}
	switch s.Every {
	case "minutes":
		n := s.Interval
		if n < 5 {
			n = 5
		}
		return t.Truncate(time.Minute).Add(time.Duration(n) * time.Minute)
	case "hourly":
		return t.Truncate(time.Hour).Add(time.Hour)
	case "weekdays":
		for i := 0; i < 8; i++ {
			c := at(t.AddDate(0, 0, i))
			if c.After(t) && c.Weekday() != time.Saturday && c.Weekday() != time.Sunday {
				return c
			}
		}
	case "weekly":
		wd := time.Weekday(((s.Weekday % 7) + 7) % 7)
		for i := 0; i < 8; i++ {
			c := at(t.AddDate(0, 0, i))
			if c.After(t) && c.Weekday() == wd {
				return c
			}
		}
	}
	// daily (and the fallback)
	c := at(t)
	if !c.After(t) {
		c = at(t.AddDate(0, 0, 1))
	}
	return c
}

// Describe is a short human label, e.g. "Weekdays at 08:30".
func (s Schedule) Describe() string {
	hh, mm := parseClock(s.At)
	clock := fmt.Sprintf("%02d:%02d", hh, mm)
	switch s.Every {
	case "minutes":
		n := s.Interval
		if n < 5 {
			n = 5
		}
		return fmt.Sprintf("Every %d minutes", n)
	case "hourly":
		return "Every hour"
	case "weekdays":
		return "Weekdays at " + clock
	case "weekly":
		return fmt.Sprintf("%ss at %s", time.Weekday(((s.Weekday%7)+7)%7), clock)
	}
	return "Daily at " + clock
}
