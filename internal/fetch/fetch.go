// Package fetch downloads and unpacks things ASG installs from the network
// or the local disk: skills, script tools and the tool runtimes. Everything
// here guards against archive entries that try to escape their folder.
package fetch

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var reGitHub = regexp.MustCompile(`^(?:https?://)?github\.com/([^/]+)/([^/#?]+?)(?:\.git)?(?:/(?:tree|blob)/([^/]+)(?:/(.*))?)?/?$`)

// ParseGitHub recognises github:owner/repo[/path][@ref], the owner/repo
// shorthand and https://github.com/owner/repo[/tree/ref/path] URLs.
func ParseGitHub(src string) (owner, repo, ref, sub string, ok bool) {
	if strings.HasPrefix(src, "github:") || (!strings.Contains(src, "://") && !strings.HasPrefix(src, "github.com") && strings.Count(src, "/") >= 1 && !strings.HasPrefix(src, ".") && !strings.HasPrefix(src, "/")) {
		s := strings.TrimPrefix(src, "github:")
		if i := strings.LastIndex(s, "@"); i > 0 {
			ref = s[i+1:]
			s = s[:i]
		}
		parts := strings.SplitN(s, "/", 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", "", "", "", false
		}
		owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
		if len(parts) == 3 {
			sub = parts[2]
		}
		return owner, repo, ref, sub, true
	}
	if mm := reGitHub.FindStringSubmatch(src); mm != nil {
		return mm[1], mm[2], mm[3], mm[4], true
	}
	return "", "", "", "", false
}

// GitHub downloads owner/repo at ref (HEAD, main or master when empty) into
// a new temporary folder. It returns the repository root inside it; the
// caller removes tmp when done.
func GitHub(ctx context.Context, owner, repo, ref string) (root, tmp string, err error) {
	tmp, err = os.MkdirTemp("", "asg-fetch-*")
	if err != nil {
		return "", "", err
	}
	refs := []string{ref}
	if ref == "" {
		refs = []string{"HEAD", "main", "master"}
	}
	var lastErr error
	for _, r := range refs {
		url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, r)
		if lastErr = DownloadTarGz(ctx, url, tmp); lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		os.RemoveAll(tmp)
		return "", "", fmt.Errorf("download github.com/%s/%s: %w", owner, repo, lastErr)
	}
	// The archive contains a single top-level folder: repo-ref/
	root = tmp
	if entries, _ := os.ReadDir(tmp); len(entries) == 1 && entries[0].IsDir() {
		root = filepath.Join(tmp, entries[0].Name())
	}
	return root, tmp, nil
}

// DownloadTarGz streams a .tar.gz from url into dest.
func DownloadTarGz(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" && strings.Contains(url, "github.com/") {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s", resp.Status)
	}
	return ExtractTarGz(resp.Body, dest, 50<<20)
}

// ExtractTarGz unpacks a gzipped tar stream into dest, keeping executable
// bits. Files larger than maxFile bytes are skipped (0 = no limit).
func ExtractTarGz(r io.Reader, dest string, maxFile int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, ok := inside(dest, hdr.Name)
		if !ok {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if maxFile > 0 && hdr.Size > maxFile {
				continue
			}
			if err := writeFile(target, tr, fileMode(hdr.FileInfo().Mode())); err != nil {
				return err
			}
		}
	}
}

// ExtractZip unpacks the zip file at path into dest.
func ExtractZip(path, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target, ok := inside(dest, f.Name)
		if !ok {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, fileMode(f.Mode()))
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// inside joins name onto dest and reports whether the result stays inside
// dest (archive entries like ../../x are dropped).
func inside(dest, name string) (string, bool) {
	target := filepath.Join(dest, filepath.FromSlash(name))
	d, t := Abs(dest), Abs(target)
	return target, t != d && strings.HasPrefix(t, d+string(os.PathSeparator))
}

func fileMode(m fs.FileMode) fs.FileMode {
	if m.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func writeFile(target string, r io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Get fetches url and returns at most limit bytes of the body.
func Get(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// File downloads url to path and checks its SHA-256 when wantSHA is set.
func File(ctx context.Context, url, path, wantSHA string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	if wantSHA != "" {
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA) {
			os.Remove(path)
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, wantSHA)
		}
	}
	return nil
}

// skipDirs are never copied: VCS data, caches and virtual environments that
// belong to the machine they were made on.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true}

// CopyDir copies the regular files under src into dst.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if rel != "." && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		return writeFile(target, in, fileMode(info.Mode()))
	})
}

// Abs is filepath.Abs that falls back to p.
func Abs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// Expand turns a leading ~/ into the home folder.
func Expand(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
