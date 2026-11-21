package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/llm"
)

// ScheduleView adds display fields to a schedule.
type ScheduleView struct {
	Schedule
	Label string `json:"label"`
}

func (a *App) ListSchedules() []ScheduleView {
	out := []ScheduleView{}
	for _, s := range a.store.Schedules() {
		out = append(out, ScheduleView{s, s.Describe()})
	}
	return out
}

func (a *App) SaveSchedule(s Schedule) (ScheduleView, error) {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	s.Name, s.Prompt = strings.TrimSpace(s.Name), strings.TrimSpace(s.Prompt)
	if s.Prompt == "" {
		return ScheduleView{}, errors.New("a schedule needs a prompt")
	}
	if s.Name == "" {
		s.Name = autoTitle(s.Prompt)
	}
	if _, ok := agent.ParseMode(s.Mode); !ok || s.Mode == "" {
		s.Mode = string(agent.ModeBuild)
	}
	s.NextRun = s.NextAfter(time.Now())
	list := a.store.Schedules()
	found := false
	for i := range list {
		if list[i].ID == s.ID {
			s.Created, s.LastRun, s.LastSession, s.LastStatus, s.Runs =
				list[i].Created, list[i].LastRun, list[i].LastSession, list[i].LastStatus, list[i].Runs
			list[i] = s
			found = true
		}
	}
	if !found {
		s.ID, s.Created = newID("c"), time.Now()
		list = append(list, s)
	}
	if err := a.store.SaveSchedules(list); err != nil {
		return ScheduleView{}, err
	}
	a.emit("asg:schedules", nil)
	return ScheduleView{s, s.Describe()}, nil
}

func (a *App) DeleteSchedule(id string) error {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	var keep []Schedule
	for _, s := range a.store.Schedules() {
		if s.ID != id {
			keep = append(keep, s)
		}
	}
	err := a.store.SaveSchedules(keep)
	a.emit("asg:schedules", nil)
	return err
}

// RunScheduleNow starts a run straight away and returns its session id.
func (a *App) RunScheduleNow(id string) (string, error) {
	for _, s := range a.store.Schedules() {
		if s.ID == id {
			return a.runSchedule(s)
		}
	}
	return "", errors.New("schedule not found")
}

func (a *App) startScheduler() {
	a.stopTick = make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-a.stopTick:
				return
			case now := <-t.C:
				a.tick(now)
			}
		}
	}()
}

// tick runs every enabled schedule that is due. A run missed while the
// app was closed runs once at the next tick, not once per missed slot.
func (a *App) tick(now time.Time) {
	var due []Schedule
	a.schedMu.Lock()
	list := a.store.Schedules()
	changed := false
	for i := range list {
		s := &list[i]
		if !s.Enabled {
			continue
		}
		if s.NextRun.IsZero() {
			s.NextRun, changed = s.NextAfter(now), true
			continue
		}
		if !now.Before(s.NextRun) {
			due = append(due, *s)
			s.NextRun, changed = s.NextAfter(now), true
		}
	}
	if changed {
		_ = a.store.SaveSchedules(list)
	}
	a.schedMu.Unlock()
	for _, s := range due {
		_, _ = a.runSchedule(s)
	}
	if len(due) > 0 {
		a.emit("asg:schedules", nil)
	}
}

func (a *App) runSchedule(s Schedule) (string, error) {
	meta, err := a.NewSession(NewSessionOpts{
		Kind: KindScheduled, ProjectID: s.ProjectID, Workdir: s.Workdir, Mode: s.Mode,
		Title: fmt.Sprintf("%s · %s", s.Name, time.Now().Format("Jan 2 15:04")),
	})
	if err != nil {
		a.recordRun(s.ID, "", "error: "+err.Error())
		return "", err
	}
	// NewSession turns a project chat into kind "project"; keep "scheduled".
	_ = a.updateMeta(meta.ID, func(f *SessionFile) { f.Kind, f.ScheduleID = KindScheduled, s.ID })
	ls, err := a.loadLive(meta.ID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	ls.mu.Lock()
	ls.running, ls.cancel = true, cancel
	ls.mu.Unlock()
	a.recordRun(s.ID, meta.ID, "running")
	a.emit("asg:toast", map[string]any{"kind": "scheduled", "text": "Schedule started: " + s.Name, "session": meta.ID})
	go func() {
		_, err := a.runTurn(ls, ctx, llm.Message{Content: s.Prompt}, true, s.AllowTools)
		status := "ok"
		if err != nil {
			status = "error: " + err.Error()
		}
		a.recordRun(s.ID, meta.ID, status)
		a.emit("asg:toast", map[string]any{"kind": "scheduled", "text": "Schedule finished: " + s.Name, "session": meta.ID})
	}()
	return meta.ID, nil
}

func (a *App) recordRun(id, session, status string) {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	list := a.store.Schedules()
	for i := range list {
		if list[i].ID == id {
			if status == "running" {
				list[i].LastRun = time.Now()
				list[i].Runs++
			}
			if session != "" {
				list[i].LastSession = session
			}
			list[i].LastStatus = clipStr(status, 200)
		}
	}
	_ = a.store.SaveSchedules(list)
	a.emit("asg:schedules", nil)
}
