//go:build linux || darwin

package computer

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/proc"
)

// runCmd runs a helper program and returns its trimmed stdout.
func runCmd(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	proc.Hide(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %v %s", name, err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// decodePNGFile reads a PNG written by a capture program.
func decodePNGFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// tempPNG is a fresh path for a capture.
func tempPNG() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("asg-computer-%d.png", time.Now().UnixNano()))
}

// looksLikePathOrURL tells a file, folder or URL from an app name.
func looksLikePathOrURL(t string) bool {
	if strings.Contains(t, "://") || strings.HasPrefix(t, "/") || strings.HasPrefix(t, "~") || strings.HasPrefix(t, ".") {
		return true
	}
	_, err := os.Stat(t)
	return err == nil
}
