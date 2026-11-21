package main

import (
	"bytes"
	"html"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// Markdown is rendered in Go with goldmark. Raw HTML in model output is
// not passed through (goldmark's default), so replies can't inject markup
// into the app. Fenced code is highlighted with chroma using CSS classes,
// so one stylesheet per theme covers every block.

var (
	mdOnce sync.Once
	md     goldmark.Markdown
)

func markdown() goldmark.Markdown {
	mdOnce.Do(func() {
		md = goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions(
				renderer.WithNodeRenderers(util.Prioritized(&codeRenderer{}, 100)),
			),
		)
	})
	return md
}

// RenderMarkdown turns markdown into safe HTML.
func RenderMarkdown(src string) string {
	var buf bytes.Buffer
	if err := markdown().Convert([]byte(src), &buf); err != nil {
		return "<pre>" + html.EscapeString(src) + "</pre>"
	}
	return buf.String()
}

type codeRenderer struct{}

func (r *codeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.render)
	reg.Register(ast.KindCodeBlock, r.render)
}

func (r *codeRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var code strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		code.Write(seg.Value(source))
	}
	lang := ""
	if fc, ok := node.(*ast.FencedCodeBlock); ok {
		lang = string(fc.Language(source))
	}
	w.WriteString(highlight(code.String(), lang))
	return ast.WalkSkipChildren, nil
}

var formatter = chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true))

func highlight(code, lang string) string {
	label := lang
	if label == "" {
		label = "text"
	}
	var body bytes.Buffer
	lexer := lexers.Get(lang)
	if lexer == nil && lang == "" {
		lexer = lexers.Analyse(code)
	}
	ok := false
	if lexer != nil {
		lexer = chroma.Coalesce(lexer)
		if it, err := lexer.Tokenise(nil, code); err == nil {
			ok = formatter.Format(&body, styles.Fallback, it) == nil
		}
	}
	if !ok {
		body.Reset()
		body.WriteString(html.EscapeString(code))
	}
	return `<div class="codeblock"><div class="codeblock-head"><span>` + html.EscapeString(label) +
		`</span><button class="copy" type="button" data-copy>Copy</button></div><pre class="chroma"><code>` +
		body.String() + `</code></pre></div>`
}

// HighlightCSS returns chroma styles for both themes, scoped by data-theme.
func HighlightCSS() string {
	var b strings.Builder
	write := func(style, scope string) {
		st := styles.Get(style)
		var buf bytes.Buffer
		_ = formatter.WriteCSS(&buf, st)
		for _, line := range strings.Split(buf.String(), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Drop the background so blocks use the app's own surface.
			if strings.Contains(line, ".bg ") || strings.Contains(line, ".chroma {") {
				continue
			}
			if i := strings.Index(line, "*/"); i >= 0 && strings.HasPrefix(line, "/*") {
				line = strings.TrimSpace(line[i+2:])
			}
			b.WriteString(scope + " " + line + "\n")
		}
	}
	write("github", `[data-theme="light"]`)
	write("github-dark", `[data-theme="dark"]`)
	return b.String()
}
