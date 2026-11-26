package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/runtimes"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// The desktop app owns the runtime script tools run on. On a new install
// and after every update it makes sure uv and Python 3.12 are present (in
// the background, so the window opens straight away). Go is installed only
// when a Go tool first needs it.

// RuntimeEvent is sent to the UI as "asg:runtime".
type RuntimeEvent struct {
	State string `json:"state"` // working | ready | error
	Text  string `json:"text"`
}

var runtimeBusy sync.Mutex

func (a *App) runtimeProgress(m string) {
	a.emit("asg:runtime", RuntimeEvent{State: "working", Text: m})
}

// bootstrapRuntime runs at startup. It only does work on a new install, after
// an update, or when uv / Python went missing. ASG_NO_RUNTIME_BOOTSTRAP=1
// turns it off (offline machines manage it with `asg runtime install`).
func (a *App) bootstrapRuntime() {
	if os.Getenv("ASG_NO_RUNTIME_BOOTSTRAP") == "1" {
		return
	}
	rt := runtimes.Default()
	if !rt.NeedsBootstrap(version) {
		return
	}
	if !runtimeBusy.TryLock() {
		return
	}
	defer runtimeBusy.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	a.runtimeProgress("Setting up Python " + runtimes.PythonVersion + " for tools…")
	if err := rt.Bootstrap(ctx, version, a.runtimeProgress); err != nil {
		a.emit("asg:runtime", RuntimeEvent{State: "error", Text: "Tool runtime setup failed: " + err.Error()})
		return
	}
	a.emit("asg:runtime", RuntimeEvent{State: "ready", Text: "Python " + runtimes.PythonVersion + " is ready for tools"})
}

// RuntimeStatus reports what the tool runtime has installed.
func (a *App) RuntimeStatus() runtimes.Status {
	return runtimes.Default().Status(context.Background())
}

// InstallRuntime installs (or repairs) uv and Python 3.12, and Go too when
// withGo is set.
func (a *App) InstallRuntime(withGo bool) (runtimes.Status, error) {
	runtimeBusy.Lock()
	defer runtimeBusy.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	rt := runtimes.Default()
	if err := rt.Bootstrap(ctx, version, a.runtimeProgress); err != nil {
		return rt.Status(ctx), err
	}
	if withGo {
		if _, err := rt.Go(ctx, a.runtimeProgress); err != nil {
			return rt.Status(ctx), err
		}
	}
	a.emit("asg:runtime", RuntimeEvent{State: "ready", Text: "Tool runtime is ready"})
	return rt.Status(ctx), nil
}

// NewTool scaffolds a Python or Go tool in ~/.asg/tools and returns its folder.
func (a *App) NewTool(name, lang string) (string, error) {
	dir, err := tools.ScaffoldTool(agent.ToolDirs()[1], strings.TrimSpace(name), lang)
	if err != nil {
		return "", err
	}
	a.core.ReloadTools()
	return dir, nil
}

// ImportTool installs the tools found at source (a folder or GitHub) and
// returns their names. Their libraries are installed by PrepareTool.
func (a *App) ImportTool(source string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	got, err := tools.ImportTools(ctx, source, agent.ToolDirs()[1])
	if err != nil {
		return nil, err
	}
	a.core.ReloadTools()
	names := []string{}
	for _, t := range got {
		names = append(names, t.Name)
	}
	return names, nil
}

// PrepareTool installs a script tool's runtime and libraries (or builds a
// Go tool) so its first call is quick. Progress arrives as asg:runtime.
func (a *App) PrepareTool(name string) error {
	t := a.core.Tools.Get(name)
	if t == nil || t.Source == "builtin" || t.Source == "bundled" {
		return fmt.Errorf("no script tool %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	ctx = tools.WithProgress(ctx, a.runtimeProgress)
	if err := tools.PrepareScriptTool(ctx, t.Source); err != nil {
		return err
	}
	a.emit("asg:runtime", RuntimeEvent{State: "ready", Text: name + " is ready"})
	return nil
}
