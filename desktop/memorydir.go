package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

// MemoryInfo says where memory notes live (Settings → Storage).
type MemoryInfo struct {
	Dir       string `json:"dir"`
	Default   string `json:"default"`
	IsDefault bool   `json:"is_default"`
	Notes     int    `json:"notes"`
}

func defaultMemoryDir() string { return filepath.Join(config.Dir(), "memory") }

func (a *App) Memory() MemoryInfo {
	dir := a.core.Memory.Root()
	return MemoryInfo{Dir: dir, Default: defaultMemoryDir(), IsDefault: samePath(dir, defaultMemoryDir()),
		Notes: len(a.core.Memory.List())}
}

// ChangeMemoryDir asks for a folder and keeps memory notes there from now
// on. Existing notes are copied over (a note already in the new folder
// wins); the old copies stay where they were.
func (a *App) ChangeMemoryDir() (MemoryInfo, error) {
	if a.anyRunning() {
		return a.Memory(), errors.New("wait for running sessions to finish first")
	}
	dst, err := a.PickFolder("Choose where ASG keeps its memory notes")
	if err != nil || dst == "" {
		return a.Memory(), err
	}
	return a.setMemoryDir(dst)
}

// ResetMemoryDir goes back to ~/.asg/memory, copying notes back the same way.
func (a *App) ResetMemoryDir() (MemoryInfo, error) {
	if a.anyRunning() {
		return a.Memory(), errors.New("wait for running sessions to finish first")
	}
	return a.setMemoryDir("")
}

func (a *App) setMemoryDir(dir string) (MemoryInfo, error) {
	old := a.core.Memory
	if dir != "" && samePath(dir, defaultMemoryDir()) {
		dir = ""
	}
	err := a.core.Config.Update(func(c *config.Config) error { c.MemoryDir = dir; return nil })
	if err != nil {
		return a.Memory(), err
	}
	next := agent.NewMemory(a.core.Config.Get())
	if err := copyNotes(old.Root(), next.Root()); err != nil {
		return a.Memory(), err
	}
	if err := copyProjectNotes(old.ProjectsDir(), next.ProjectsDir()); err != nil {
		return a.Memory(), err
	}
	a.core.Memory = next
	return a.Memory(), nil
}

// copyNotes copies the *.md files in src to dst without overwriting.
func copyNotes(src, dst string) error {
	if samePath(src, dst) {
		return nil
	}
	entries, err := os.ReadDir(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		target := filepath.Join(dst, e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(src, e.Name()), target); err != nil {
			return err
		}
	}
	return nil
}

// copyProjectNotes copies <src>/<project>/memory/*.md to the same place under dst.
func copyProjectNotes(src, dst string) error {
	if samePath(src, dst) {
		return nil
	}
	entries, err := os.ReadDir(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := copyNotes(filepath.Join(src, e.Name(), "memory"), filepath.Join(dst, e.Name(), "memory")); err != nil {
			return err
		}
	}
	return nil
}
