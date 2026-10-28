// Package update replaces the running asg binary with the latest GitHub release.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Repo = "kxngHADES/asg-harness"

// Latest returns the newest release tag, e.g. "v0.2.0".
func Latest(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("no release found (%s)", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

// AssetName is the archive GoReleaser publishes for this platform.
func AssetName() string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("asg_%s_%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// Run updates the binary at os.Executable() to tag, verifying its checksum.
func Run(ctx context.Context, current string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tag, err := Latest(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimPrefix(tag, "v") == strings.TrimPrefix(current, "v") {
		return tag, nil
	}
	base := "https://github.com/" + Repo + "/releases/download/" + tag + "/"
	archive, err := get(ctx, base+AssetName())
	if err != nil {
		return "", err
	}
	sums, err := get(ctx, base+"checksums.txt")
	if err != nil {
		return "", err
	}
	if err := verify(archive, AssetName(), string(sums)); err != nil {
		return "", err
	}
	bin, err := extract(archive)
	if err != nil {
		return "", err
	}
	return tag, replaceSelf(bin)
}

func verify(data []byte, name, sums string) error {
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			if f[0] != want {
				return fmt.Errorf("checksum mismatch for %s", name)
			}
			return nil
		}
	}
	return fmt.Errorf("%s not listed in checksums.txt", name)
}

func extract(archive []byte) ([]byte, error) {
	want := "asg"
	if runtime.GOOS == "windows" {
		want = "asg.exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s not found in archive", want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("%s not found in archive", want)
		}
		if filepath.Base(h.Name) == want && h.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

// replaceSelf swaps in the new binary. Windows can't overwrite a running
// executable, so the old one is renamed aside first.
func replaceSelf(bin []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("write %s: %w (try again with sudo, or rerun the install script)", tmp, err)
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
