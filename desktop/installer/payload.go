package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// payload holds app.tar.gz: the built desktop app, laid out exactly as it
// should appear in the install folder. package.sh writes it before building
// the wizard; the README keeps the folder (and the embed) valid without it.
//
//go:embed all:payload
var payload embed.FS

const payloadName = "payload/app.tar.gz"

// appArchive returns the embedded app, or nil in a preview build.
func appArchive() []byte {
	data, err := payload.ReadFile(payloadName)
	if err != nil || len(data) == 0 {
		return nil
	}
	return data
}

// archiveInfo returns the unpacked size and the top-level entries of a
// .tar.gz (the files and folders that land directly in the install folder).
func archiveInfo(data []byte) (size int64, top []string, err error) {
	seen := map[string]bool{}
	err = walkArchive(data, func(h *tar.Header, _ io.Reader) error {
		size += h.Size
		first := strings.SplitN(strings.TrimPrefix(path.Clean(h.Name), "./"), "/", 2)[0]
		if first != "" && first != "." && !seen[first] {
			seen[first] = true
			top = append(top, first)
		}
		return nil
	})
	return size, top, err
}

func walkArchive(data []byte, fn func(h *tar.Header, r io.Reader) error) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

// extract unpacks a .tar.gz into dir. Every file is written next to its
// final name and renamed into place, so a file that is in use (the app
// still running on Windows) fails cleanly instead of being half-written.
// progress gets the bytes written so far.
func extract(data []byte, dir string, progress func(done int64)) error {
	var done int64
	return walkArchive(data, func(h *tar.Header, r io.Reader) error {
		target, err := safeJoin(dir, h.Name)
		if err != nil {
			return err
		}
		mode := os.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			return os.MkdirAll(target, 0o755)
		case tar.TypeSymlink:
			// Only links that stay inside the archive (macOS frameworks use them).
			if _, err := safeJoin(filepath.Dir(target), h.Linkname); err != nil {
				return fmt.Errorf("archive link %s points outside the app", h.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(h.Linkname, target)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if mode == 0 {
				mode = 0o644
			}
			tmp := target + ".asg-new"
			f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			n, err := io.Copy(f, &countingReader{r: r, done: &done, progress: progress})
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err == nil && n != h.Size {
				err = io.ErrUnexpectedEOF
			}
			if err == nil {
				err = os.Rename(tmp, target)
			}
			if err != nil {
				_ = os.Remove(tmp)
				return inUse(target, err)
			}
			return nil
		}
		return nil // other entry types (hard links, devices) aren't used
	})
}

// inUse turns "access denied" on an existing file into advice.
func inUse(target string, err error) error {
	if _, statErr := os.Stat(target); statErr == nil && errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("can't replace %s — is ASG still running? Close it and try again", filepath.Base(target))
	}
	return err
}

// safeJoin joins an archive name to dir and refuses names that escape it.
func safeJoin(dir, name string) (string, error) {
	n := strings.ReplaceAll(name, "\\", "/")
	bad := strings.HasPrefix(n, "/") || filepath.IsAbs(name) || filepath.VolumeName(name) != ""
	for _, part := range strings.Split(n, "/") {
		bad = bad || part == ".."
	}
	if bad {
		return "", fmt.Errorf("archive entry %q escapes the install folder", name)
	}
	return filepath.Join(dir, filepath.FromSlash(n)), nil
}

type countingReader struct {
	r        io.Reader
	done     *int64
	progress func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	*c.done += int64(n)
	if c.progress != nil && n > 0 {
		c.progress(*c.done)
	}
	return n, err
}
