package main

// In-app updates: the app compares its version with the latest GitHub
// release and, only when the user says so, downloads the matching desktop
// archive, checks it against its .sha256, swaps it in place of the running
// app and restarts. Nothing is installed without the user choosing to.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kxngHADES/asg-harness/internal/update"
)

// waitPIDFlag tells a freshly started copy to wait for the old one to exit
// before it takes the single-instance lock.
const waitPIDFlag = "--asg-wait-pid="

// UpdateInfo is the result of an update check.
type UpdateInfo struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	Dev        bool   `json:"dev"`         // a development build: nothing to compare against
	Notes      string `json:"notes"`       // release notes, Markdown
	NotesHTML  string `json:"notes_html"`  // rendered
	URL        string `json:"url"`         // the release page
	Published  string `json:"published"`   // RFC 3339
	Asset      string `json:"asset"`       // archive for this platform
	Size       int64  `json:"size"`        // bytes
	CanInstall bool   `json:"can_install"` // false: the user has to download it themselves
	Reason     string `json:"reason"`      // why CanInstall is false
	Skipped    bool   `json:"skipped"`     // the user chose to skip this version
	Checked    string `json:"checked"`     // when this check ran
}

type ghRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// updater holds the state of checks and installs between calls.
type updater struct {
	mu         sync.Mutex
	last       *UpdateInfo
	release    *ghRelease
	installing bool
	notified   string // version the user was last told about
}

// desktopAsset is the archive package.sh publishes for this platform.
func desktopAsset(goos, goarch string) string {
	switch goos {
	case "windows":
		return "asg-desktop_windows_" + goarch + ".zip"
	case "darwin":
		return "asg-desktop_darwin_universal.zip"
	default:
		return "asg-desktop_" + goos + "_" + goarch + ".tar.gz"
	}
}

// parseVersion reads "v1.2.3" (a "-rc1" style suffix is ignored).
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newerVersion reports whether latest is newer than current.
func newerVersion(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func fetchRelease(ctx context.Context) (*ghRelease, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+update.Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "asg-desktop")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("GitHub is rate-limiting update checks; try again in a few minutes")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("no release found (%s)", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// installTarget is what an update replaces: the .app bundle on macOS, the
// executable elsewhere.
func installTarget() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		app := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // ASG.app/Contents/MacOS/asg-desktop
		if !strings.HasSuffix(app, ".app") {
			return "", errors.New("this copy isn't inside an .app bundle")
		}
		return app, nil
	}
	return exe, nil
}

// writable reports whether files can be created next to path.
func writable(path string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".asg-update-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// CheckUpdate asks GitHub for the latest release. It never installs anything.
func (a *App) CheckUpdate() (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info := UpdateInfo{Current: version, Checked: time.Now().Format(time.RFC3339)}
	rel, err := fetchRelease(ctx)
	if err != nil {
		return info, err
	}
	info.Latest = rel.TagName
	info.URL = rel.HTMLURL
	info.Notes = strings.TrimSpace(rel.Body)
	if info.Notes != "" {
		info.NotesHTML = RenderMarkdown(info.Notes)
	}
	info.Published = rel.PublishedAt
	info.Asset = desktopAsset(runtime.GOOS, runtime.GOARCH)
	if _, ok := parseVersion(version); !ok {
		info.Dev = true
	} else {
		info.Available = newerVersion(rel.TagName, version)
	}
	a.setMu.Lock()
	info.Skipped = info.Available && a.settings.SkippedVersion == rel.TagName
	a.setMu.Unlock()

	hasAsset, hasSum := false, false
	for _, as := range rel.Assets {
		if as.Name == info.Asset {
			hasAsset, info.Size = true, as.Size
		}
		if as.Name == info.Asset+".sha256" {
			hasSum = true
		}
	}
	switch target, terr := installTarget(); {
	case info.Dev:
		info.Reason = "This is a development build, so it can't update itself."
	case !hasAsset || !hasSum:
		info.Reason = "This release has no desktop build for your system yet."
	case terr != nil:
		info.Reason = "ASG can't update this copy in place (" + terr.Error() + ")."
	case writable(target) != nil:
		info.Reason = "ASG isn't allowed to write to " + filepath.Dir(target) + ". Run the setup wizard to update."
	default:
		info.CanInstall = true
	}

	a.upd.mu.Lock()
	a.upd.last, a.upd.release = &info, rel
	a.upd.mu.Unlock()
	a.trayUpdate(info)
	return info, nil
}

// SetUpdatePrefs saves the automatic-check switch.
func (a *App) SetUpdatePrefs(autoCheck bool) error {
	a.setMu.Lock()
	a.settings.SkipUpdateCheck = !autoCheck
	s := a.settings
	a.setMu.Unlock()
	return saveSettings(s)
}

// SkipUpdate stops reminding about one version ("" clears it).
func (a *App) SkipUpdate(tag string) error {
	a.setMu.Lock()
	a.settings.SkippedVersion = tag
	s := a.settings
	a.setMu.Unlock()
	return saveSettings(s)
}

// startUpdateChecks checks shortly after start and then twice a day, and
// tells the UI when a new version is out. The user decides what to do.
func (a *App) startUpdateChecks() {
	go func() {
		t := time.NewTimer(10 * time.Second)
		for {
			select {
			case <-t.C:
			case <-a.stopUpd:
				t.Stop()
				return
			}
			a.setMu.Lock()
			skip := a.settings.SkipUpdateCheck
			a.setMu.Unlock()
			if !skip {
				if info, err := a.CheckUpdate(); err == nil && info.Available && !info.Skipped {
					a.upd.mu.Lock()
					fresh := a.upd.notified != info.Latest
					a.upd.notified = info.Latest
					a.upd.mu.Unlock()
					if fresh {
						a.emit("asg:update-available", info)
					}
				}
			}
			t.Reset(12 * time.Hour)
		}
	}()
}

// progressReader reports download progress to the UI at most every 150 ms.
type progressReader struct {
	r     io.Reader
	h     hash.Hash
	done  int64
	total int64
	last  time.Time
	emit  func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.h.Write(b[:n])
		p.done += int64(n)
		if time.Since(p.last) > 150*time.Millisecond || err == io.EOF {
			p.last = time.Now()
			p.emit(p.done, p.total)
		}
	}
	return n, err
}

// InstallUpdate downloads the latest release, verifies it, replaces this
// copy of the app and restarts into the new one. Only called when the user
// confirms in the UI.
func (a *App) InstallUpdate() error {
	a.upd.mu.Lock()
	if a.upd.installing {
		a.upd.mu.Unlock()
		return errors.New("an update is already being installed")
	}
	info, rel := a.upd.last, a.upd.release
	if info == nil || rel == nil {
		a.upd.mu.Unlock()
		return errors.New("check for updates first")
	}
	if !info.Available || !info.CanInstall {
		a.upd.mu.Unlock()
		if info.Reason != "" {
			return errors.New(info.Reason)
		}
		return errors.New("you're already on the latest version")
	}
	a.upd.installing = true
	a.upd.mu.Unlock()
	defer func() {
		a.upd.mu.Lock()
		a.upd.installing = false
		a.upd.mu.Unlock()
	}()
	if a.anyRunning() {
		return errors.New("a session is still running: finish or stop it, then update")
	}

	stage := func(s string, extra map[string]any) {
		m := map[string]any{"stage": s}
		for k, v := range extra {
			m[k] = v
		}
		a.emit("asg:update-progress", m)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var url, sumURL string
	for _, as := range rel.Assets {
		switch as.Name {
		case info.Asset:
			url = as.URL
		case info.Asset + ".sha256":
			sumURL = as.URL
		}
	}
	stage("download", map[string]any{"done": 0, "total": info.Size})
	want, err := fetchChecksum(ctx, sumURL)
	if err != nil {
		return err
	}
	archive, err := os.CreateTemp("", "asg-update-*"+filepath.Ext(info.Asset))
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	got, err := download(ctx, url, archive, info.Size, func(done, total int64) {
		stage("download", map[string]any{"done": done, "total": total})
	})
	archive.Close()
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return errors.New("the download doesn't match its checksum; nothing was changed")
	}

	stage("install", nil)
	target, err := installTarget()
	if err != nil {
		return err
	}
	if err := applyUpdate(archive.Name(), target); err != nil {
		return err
	}
	a.LogJS("updated to " + rel.TagName)

	stage("restart", nil)
	if err := relaunch(target); err != nil {
		return fmt.Errorf("updated to %s, but couldn't restart: %w. Open ASG again", rel.TagName, err)
	}
	time.AfterFunc(300*time.Millisecond, a.quitApp)
	return nil
}

func fetchChecksum(ctx context.Context, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 || len(f[0]) != 64 {
		return "", errors.New("checksum file is malformed")
	}
	return f[0], nil
}

// download streams url into w and returns its SHA-256.
func download(ctx context.Context, url string, w io.Writer, size int64, progress func(done, total int64)) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: %s", resp.Status)
	}
	if resp.ContentLength > 0 {
		size = resp.ContentLength
	}
	pr := &progressReader{r: io.LimitReader(resp.Body, 500<<20), h: sha256.New(), total: size, emit: progress}
	if _, err := io.Copy(w, pr); err != nil {
		return "", err
	}
	progress(pr.done, pr.total)
	return hex.EncodeToString(pr.h.Sum(nil)), nil
}

// applyUpdate unpacks archive and puts it in place of target.
func applyUpdate(archive, target string) error {
	if runtime.GOOS == "darwin" {
		return replaceBundle(archive, target)
	}
	name := filepath.Base(target)
	var bin []byte
	var err error
	if strings.HasSuffix(archive, ".zip") {
		bin, err = fileFromZip(archive, name)
	} else {
		bin, err = fileFromTarGz(archive, name)
	}
	if err != nil {
		return err
	}
	return replaceFile(target, bin)
}

func fileFromZip(archive, name string) ([]byte, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) == name && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 500<<20))
		}
	}
	return nil, fmt.Errorf("%s not found in the update", name)
}

func fileFromTarGz(archive, name string) ([]byte, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("%s not found in the update", name)
		}
		if filepath.Base(h.Name) == name && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, 500<<20))
		}
	}
}

// replaceFile swaps in a new executable. Windows can't overwrite a running
// executable but can rename it, so the old one is moved aside and removed on
// the next start (cleanupOldUpdate).
func replaceFile(exe string, bin []byte) error {
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Remove(old)
	}
	return nil
}

// replaceBundle unpacks ASG.app next to the installed one and swaps them.
func replaceBundle(archive, app string) error {
	stage, err := os.MkdirTemp(filepath.Dir(app), ".asg-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := unzipAll(archive, stage); err != nil {
		return err
	}
	fresh := filepath.Join(stage, "ASG.app")
	if _, err := os.Stat(filepath.Join(fresh, "Contents", "MacOS", "asg-desktop")); err != nil {
		return errors.New("the update doesn't contain ASG.app")
	}
	old := app + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(app, old); err != nil {
		return err
	}
	if err := os.Rename(fresh, app); err != nil {
		_ = os.Rename(old, app)
		return err
	}
	return os.RemoveAll(old)
}

// unzipAll extracts every entry under dir, refusing paths that escape it.
func unzipAll(archive, dir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	root := filepath.Clean(dir) + string(os.PathSeparator)
	for _, f := range zr.File {
		dst := filepath.Join(dir, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(dst+string(os.PathSeparator), root) {
			return fmt.Errorf("unsafe path in update: %s", f.Name)
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		case mode&os.ModeSymlink != 0:
			rc, err := f.Open()
			if err != nil {
				return err
			}
			link, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return err
			}
			l := string(link)
			if filepath.IsAbs(l) || !strings.HasPrefix(filepath.Join(filepath.Dir(dst), l)+string(os.PathSeparator), root) {
				return fmt.Errorf("unsafe link in update: %s", f.Name)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(l, dst); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		perm := mode.Perm()
		if perm == 0 {
			perm = 0o644
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 500<<20))
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// relaunch starts the updated app, which waits for this process to exit.
func relaunch(target string) error {
	arg := waitPIDFlag + strconv.Itoa(os.Getpid())
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-n", target, "--args", arg)
	} else {
		cmd = exec.Command(target, arg)
	}
	cmd.Dir = filepath.Dir(target)
	return cmd.Start()
}

// afterUpdate runs before the UI starts: it waits for the copy that started
// this one to exit (so the single-instance lock is free) and removes what an
// update left behind.
func afterUpdate(args []string) {
	for _, a := range args {
		if pid, err := strconv.Atoi(strings.TrimPrefix(a, waitPIDFlag)); strings.HasPrefix(a, waitPIDFlag) && err == nil && pid > 0 {
			waitForExit(pid, 20*time.Second)
		}
	}
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			_ = os.Remove(exe + ".old")
		}
	}
}

// quitApp quits for real, skipping close-to-tray.
func (a *App) quitApp() {
	a.quitting.Store(true)
	if a.ctx != nil {
		wruntime.Quit(a.ctx)
	}
}
