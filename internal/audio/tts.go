// Package audio speaks assistant replies aloud, either with the OS speech
// engine or with a speech model behind an OpenAI-compatible /audio/speech
// endpoint (Ollama speech models such as phonon, or OpenAI TTS).
package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Options is a fully resolved voice.
type Options struct {
	Engine  string // "system", "ollama" or "openai"
	BaseURL string // OpenAI-compatible base URL, e.g. http://localhost:11434/v1
	APIKey  string
	Model   string
	Voice   string
	Rate    int // words per minute (system engine only)
	// Why explains how this voice was chosen, for display.
	Why string
}

// Label describes the voice for humans, e.g. "ollama phonon (Juniper)".
func (o Options) Label() string {
	s := o.Engine
	if o.Model != "" {
		s += " " + o.Model
	}
	if o.Voice != "" {
		s += " (" + o.Voice + ")"
	}
	return s
}

// Speaker plays one reply at a time; a new Speak interrupts the last.
type Speaker struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

var (
	reCode     = regexp.MustCompile("(?s)```.*?```")
	reInline   = regexp.MustCompile("`([^`]*)`")
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reMarkup   = regexp.MustCompile(`[*_#>|~]+`)
	reSpaces   = regexp.MustCompile(`\s+`)
	reSentence = regexp.MustCompile(`[^.!?…]+[.!?…]+["')\]]*\s*|[^.!?…]+$`)
)

// Clean turns markdown into something pleasant to read aloud.
func Clean(text string) string {
	text = reCode.ReplaceAllString(text, " (code omitted) ")
	text = reInline.ReplaceAllString(text, "$1")
	text = reLink.ReplaceAllString(text, "$1")
	text = reMarkup.ReplaceAllString(text, " ")
	return strings.TrimSpace(reSpaces.ReplaceAllString(text, " "))
}

// Chunks splits text at sentence boundaries into pieces of at most max
// characters. The first chunk is kept short so speech starts quickly.
func Chunks(text string, max int) []string {
	var out []string
	var cur strings.Builder
	limit := max / 2
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
			limit = max
		}
		cur.Reset()
	}
	for _, sent := range reSentence.FindAllString(text, -1) {
		if cur.Len() > 0 && cur.Len()+len(sent) > limit {
			flush()
		}
		for len(sent) > max { // one very long sentence: split on a space
			cut := strings.LastIndex(sent[:max], " ")
			if cut <= 0 {
				cut = max
			}
			cur.WriteString(sent[:cut])
			flush()
			sent = sent[cut:]
		}
		cur.WriteString(sent)
	}
	flush()
	return out
}

// Stop interrupts any speech in progress.
func (s *Speaker) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// Speak starts speaking text in the background. The channel receives the
// first error, or nil when finished or interrupted.
func (s *Speaker) Speak(text string, opt Options) <-chan error {
	done := make(chan error, 1)
	text = Clean(text)
	if text == "" {
		done <- nil
		return done
	}
	s.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	go func() {
		var err error
		if opt.Engine == "system" {
			err = speakSystem(ctx, text, opt)
		} else {
			err = speakHTTP(ctx, text, opt)
		}
		if ctx.Err() != nil {
			err = nil
		}
		done <- err
	}()
	return done
}

// speakHTTP synthesizes chunk n+1 while chunk n plays.
func speakHTTP(ctx context.Context, text string, opt Options) error {
	type clip struct {
		data []byte
		err  error
	}
	clips := make(chan clip, 2)
	go func() {
		defer close(clips)
		for _, c := range Chunks(text, 400) {
			data, _, err := Synthesize(ctx, c, opt)
			select {
			case clips <- clip{data, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for c := range clips {
		if c.err != nil {
			return c.err
		}
		if err := playBytes(ctx, c.data); err != nil {
			return err
		}
	}
	return nil
}

func speakSystem(ctx context.Context, text string, opt Options) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		args := []string{}
		if opt.Voice != "" {
			args = append(args, "-v", opt.Voice)
		}
		if opt.Rate > 0 {
			args = append(args, "-r", strconv.Itoa(opt.Rate))
		}
		cmd = exec.CommandContext(ctx, "say", args...)
		cmd.Stdin = strings.NewReader(text)
	case "windows":
		ps := "Add-Type -AssemblyName System.Speech; $s = New-Object System.Speech.Synthesis.SpeechSynthesizer; $s.Speak([Console]::In.ReadToEnd())"
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
		cmd.Stdin = strings.NewReader(text)
	default:
		for _, bin := range []string{"espeak-ng", "espeak", "spd-say"} {
			if _, err := exec.LookPath(bin); err == nil {
				args := []string{}
				if opt.Voice != "" && bin != "spd-say" {
					args = append(args, "-v", opt.Voice)
				}
				if opt.Rate > 0 && bin != "spd-say" {
					args = append(args, "-s", strconv.Itoa(opt.Rate))
				}
				cmd = exec.CommandContext(ctx, bin, append(args, text)...)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no speech engine found (install espeak-ng, or pick an Ollama speech model with /voice)")
		}
	}
	return cmd.Run()
}

// Synthesize fetches audio for text from an OpenAI-compatible
// /audio/speech endpoint and returns the bytes and their content type.
func Synthesize(ctx context.Context, text string, opt Options) ([]byte, string, error) {
	model, voice := opt.Model, opt.Voice
	if opt.Engine == "openai" {
		if model == "" {
			model = "gpt-4o-mini-tts"
		}
		if voice == "" {
			voice = "alloy"
		}
	}
	if model == "" {
		return nil, "", fmt.Errorf("no speech model set (try /voice ollama phonon Juniper)")
	}
	base := strings.TrimRight(opt.BaseURL, "/")
	payload := map[string]any{"model": model, "input": text, "response_format": "wav"}
	if voice != "" {
		payload["voice"] = voice
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if opt.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+opt.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("speech: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("speech (%s): %s: %s", model, resp.Status, strings.TrimSpace(string(data)))
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	return data, ct, nil
}

func playBytes(ctx context.Context, data []byte) error {
	ext := ".wav"
	if !bytes.HasPrefix(data, []byte("RIFF")) {
		ext = ".mp3"
	}
	f, err := os.CreateTemp("", "asg-tts-*"+ext)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	f.Write(data)
	f.Close()
	return play(ctx, f.Name(), ext)
}

func play(ctx context.Context, path, ext string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"afplay", path}}
	case "windows":
		if ext == ".wav" {
			candidates = [][]string{{"powershell", "-NoProfile", "-Command", fmt.Sprintf("(New-Object Media.SoundPlayer '%s').PlaySync()", path)}}
		}
		candidates = append(candidates, []string{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path})
	default:
		if ext == ".wav" {
			candidates = [][]string{{"paplay", path}, {"aplay", "-q", path}}
		} else {
			candidates = [][]string{{"mpg123", "-q", path}}
		}
		candidates = append(candidates, []string{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path}, []string{"mpv", "--really-quiet", path})
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err == nil {
			return exec.CommandContext(ctx, c[0], c[1:]...).Run()
		}
	}
	return fmt.Errorf("no audio player found (install pulseaudio-utils, alsa-utils or ffmpeg)")
}

// SystemVoices lists voices for the OS speech engine (macOS only).
func SystemVoices() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.Command("say", "-v", "?").Output()
	if err != nil {
		return nil
	}
	var voices []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Voice names can contain spaces; the locale column looks like en_US.
		var name []string
		for _, f := range fields {
			if strings.Contains(f, "_") && len(f) <= 7 {
				break
			}
			name = append(name, f)
		}
		voices = append(voices, strings.Join(name, " "))
	}
	return voices
}
