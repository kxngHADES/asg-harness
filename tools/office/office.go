// Package office is the office_document tool: it writes Word (.docx),
// PowerPoint (.pptx) and Excel (.xlsx) files from Markdown or structured
// input. It is pure Go, so it works for every user with no Office, Python
// or other dependency installed.
package office

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/markdown"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// Slide is one PowerPoint slide.
type Slide struct {
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Bullets  []string `json:"bullets,omitempty"` // indent 2 spaces per level
}

// Sheet is one Excel worksheet; the first row is the header.
type Sheet struct {
	Name string  `json:"name"`
	Rows [][]any `json:"rows"`
}

// Request is the tool's input.
type Request struct {
	Path     string  `json:"path"`
	Title    string  `json:"title,omitempty"`
	Author   string  `json:"author,omitempty"`
	Markdown string  `json:"markdown,omitempty"`
	Slides   []Slide `json:"slides,omitempty"`
	Sheets   []Sheet `json:"sheets,omitempty"`
	CSV      string  `json:"csv,omitempty"`
}

// Formats lists the supported file extensions.
var Formats = []string{".docx", ".pptx", ".xlsx"}

// Tool returns the office_document tool.
func Tool() *tools.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return &tools.Tool{
		Spec: llm.ToolSpec{
			Name: "office_document",
			Description: "Create a Word (.docx), PowerPoint (.pptx) or Excel (.xlsx) file. The file extension of path picks the format. " +
				"Easiest: pass the content as Markdown. docx: a full document (headings, lists, tables, code, quotes; \\pagebreak for a page break). " +
				"pptx: each # or ## heading (or a --- line) starts a new slide and its bullets become the slide's bullets. " +
				"xlsx: every Markdown table becomes a sheet. Or pass slides / sheets / csv for exact control.",
			Parameters: map[string]any{"type": "object", "required": []string{"path"}, "properties": map[string]any{
				"path":     str("Output file path ending in .docx, .pptx or .xlsx"),
				"title":    str("Document title (docx: shown as the title; pptx: a title slide; all: file properties)"),
				"author":   str("Author for the file properties"),
				"markdown": str("Content as Markdown"),
				"slides": map[string]any{"type": "array", "description": "pptx slides", "items": map[string]any{
					"type": "object", "properties": map[string]any{
						"title":    str("Slide title"),
						"subtitle": str("Subtitle (title-style slide when there are no bullets)"),
						"bullets":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Bullet points; indent 2 spaces per sub-level"},
					}}},
				"sheets": map[string]any{"type": "array", "description": "xlsx sheets", "items": map[string]any{
					"type": "object", "properties": map[string]any{
						"name": str("Sheet name"),
						"rows": map[string]any{"type": "array", "description": "Rows of cells; first row is the header. Numbers stay numbers; text starting with = is a formula",
							"items": map[string]any{"type": "array", "items": map[string]any{}}},
					}}},
				"csv": str("CSV text for a one-sheet xlsx"),
			}},
		},
		NeedsApproval: true,
		Source:        "bundled",
		Describe: func(a json.RawMessage) string {
			var r Request
			_ = json.Unmarshal(a, &r)
			return r.Path
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var r Request
			if err := json.Unmarshal(raw, &r); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			return Write(r)
		},
	}
}

// Write creates the document described by r and returns a summary.
func Write(r Request) (string, error) {
	if strings.TrimSpace(r.Path) == "" {
		return "", errors.New("path is required")
	}
	ext := strings.ToLower(filepath.Ext(r.Path))
	var files []file
	var summary string
	switch ext {
	case ".docx":
		if strings.TrimSpace(r.Markdown) == "" && r.Title == "" {
			return "", errors.New("docx needs markdown content")
		}
		blocks := markdown.Parse(r.Markdown)
		files = docx(r.Title, r.Author, blocks)
		summary = fmt.Sprintf("%d blocks", len(blocks))
	case ".pptx":
		slides := r.Slides
		if len(slides) == 0 {
			slides = SlidesFromMarkdown(r.Markdown)
		}
		if r.Title != "" && (len(slides) == 0 || slides[0].Title != r.Title) {
			slides = append([]Slide{{Title: r.Title}}, slides...)
		}
		if len(slides) == 0 {
			return "", errors.New("pptx needs slides or markdown")
		}
		files = pptx(r.Title, r.Author, slides)
		summary = fmt.Sprintf("%d slides", len(slides))
	case ".xlsx":
		sheets := r.Sheets
		if len(sheets) == 0 && strings.TrimSpace(r.CSV) != "" {
			rows, err := csv.NewReader(strings.NewReader(r.CSV)).ReadAll()
			if err != nil {
				return "", fmt.Errorf("csv: %w", err)
			}
			sheets = []Sheet{{Name: "Sheet1", Rows: anyRows(rows)}}
		}
		if len(sheets) == 0 {
			for i, b := range markdown.Parse(r.Markdown) {
				if b.Kind == markdown.Table {
					sheets = append(sheets, Sheet{Name: fmt.Sprintf("Table %d", i+1), Rows: anyRows(plainRows(b.Rows))})
				}
			}
		}
		if len(sheets) == 0 {
			return "", errors.New("xlsx needs sheets, csv, or markdown containing a table")
		}
		files = xlsx(r.Title, r.Author, sheets)
		summary = fmt.Sprintf("%d sheet(s)", len(sheets))
	default:
		return "", fmt.Errorf("unsupported format %q — use one of %s", ext, strings.Join(Formats, ", "))
	}
	if err := writeZip(r.Path, files); err != nil {
		return "", err
	}
	info, _ := os.Stat(r.Path)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return fmt.Sprintf("wrote %s (%s, %d bytes)", r.Path, summary, size), nil
}

type file struct {
	name, body string
}

func writeZip(path string, files []file) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, fl := range files {
		w, err := zw.Create(fl.name)
		if err == nil {
			_, err = w.Write([]byte(fl.body))
		}
		if err != nil {
			zw.Close()
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// esc escapes text for XML and drops characters XML cannot hold.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r != 0xFFFE && r != 0xFFFF):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func coreProps(title, author string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + esc(title) + `</dc:title><dc:creator>` + esc(author) + `</dc:creator></cp:coreProperties>`
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

func anyRows(rows [][]string) [][]any {
	out := make([][]any, len(rows))
	for i, r := range rows {
		out[i] = make([]any, len(r))
		for j, c := range r {
			out[i][j] = c
		}
	}
	return out
}

func plainRows(rows [][]string) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = make([]string, len(r))
		for j, c := range r {
			out[i][j] = markdown.Plain(c)
		}
	}
	return out
}
