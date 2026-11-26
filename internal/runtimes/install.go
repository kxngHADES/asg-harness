package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/fetch"
)

// uvTarget is uv's release target triple for this platform. Linux uses the
// static musl build so it runs on any distribution.
func uvTarget(goos, goarch string) (target, ext string, err error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if arch == "" {
		return "", "", fmt.Errorf("no uv build for %s/%s", goos, goarch)
	}
	switch goos {
	case "windows":
		return arch + "-pc-windows-msvc", ".zip", nil
	case "darwin":
		return arch + "-apple-darwin", ".tar.gz", nil
	case "linux":
		return arch + "-unknown-linux-musl", ".tar.gz", nil
	}
	return "", "", fmt.Errorf("no uv build for %s/%s", goos, goarch)
}

// uvReleaseURL is the download folder of the uv release to install: the
// latest one, or ASG_UV_VERSION (e.g. 0.11.19) to pin it.
func uvReleaseURL() string {
	if v := strings.TrimPrefix(os.Getenv("ASG_UV_VERSION"), "v"); v != "" {
		return "https://github.com/astral-sh/uv/releases/download/" + v + "/"
	}
	return "https://github.com/astral-sh/uv/releases/latest/download/"
}

// installUV downloads uv's official release archive, checks it against the
// published SHA-256 and puts uv (and uvx) at dest.
func installUV(ctx context.Context, home, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	target, ext, err := uvTarget(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	asset := "uv-" + target + ext
	base := uvReleaseURL()
	sum, err := fetch.Get(ctx, base+asset+".sha256", 4096)
	if err != nil {
		return err
	}
	want := strings.Fields(string(sum))
	if len(want) == 0 {
		return fmt.Errorf("empty checksum file for %s", asset)
	}
	tmp, err := os.MkdirTemp(mkdir(home), "uv-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, asset)
	if err := fetch.File(ctx, base+asset, archive, want[0]); err != nil {
		return err
	}
	out := filepath.Join(tmp, "x")
	if err := extract(archive, out); err != nil {
		return err
	}
	// Windows zips hold uv.exe at the root, the tarballs uv-<target>/uv.
	found := false
	for _, name := range []string{exe("uv"), exe("uvx")} {
		src := findFile(out, name)
		if src == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		to := filepath.Join(filepath.Dir(dest), name)
		_ = os.Remove(to)
		if err := os.Rename(src, to); err != nil {
			return err
		}
		_ = os.Chmod(to, 0o755)
		found = found || name == exe("uv")
	}
	if !found {
		return fmt.Errorf("%s has no %s", asset, exe("uv"))
	}
	return nil
}

// goRelease is one entry of https://go.dev/dl/?mode=json.
type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		SHA256   string `json:"sha256"`
		Kind     string `json:"kind"`
	} `json:"files"`
}

// pickGo finds the archive for goos/goarch in the newest stable release.
func pickGo(releases []goRelease, goos, goarch string) (file, sha, version string, err error) {
	for _, r := range releases {
		if !r.Stable {
			continue
		}
		for _, f := range r.Files {
			if f.OS == goos && f.Arch == goarch && f.Kind == "archive" {
				return f.Filename, f.SHA256, r.Version, nil
			}
		}
	}
	return "", "", "", fmt.Errorf("go.dev has no Go archive for %s/%s", goos, goarch)
}

// installGo downloads the latest stable Go from go.dev into home/go.
func installGo(ctx context.Context, home string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	data, err := fetch.Get(ctx, "https://go.dev/dl/?mode=json", 1<<20)
	if err != nil {
		return err
	}
	var releases []goRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return fmt.Errorf("read go.dev release list: %w", err)
	}
	file, sha, _, err := pickGo(releases, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(mkdir(home), "go-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, file)
	if err := fetch.File(ctx, "https://go.dev/dl/"+file, archive, sha); err != nil {
		return err
	}
	out := filepath.Join(tmp, "x")
	if err := extract(archive, out); err != nil {
		return err
	}
	dest := filepath.Join(home, "go")
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(filepath.Join(out, "go"), dest)
}

func extract(archive, dest string) error {
	if strings.HasSuffix(archive, ".zip") {
		return fetch.ExtractZip(archive, dest)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	return fetch.ExtractTarGz(f, dest, 0)
}

func findFile(root, name string) string {
	var found string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == name {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func mkdir(dir string) string {
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:10]
}
