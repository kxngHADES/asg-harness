// Package markdown is a small Markdown block and inline parser. It covers
// what models actually write — headings, paragraphs, bullet and numbered
// lists, code blocks, tables, quotes, rules and images — and is shared by
// the office document writer and the screenshot renderer.
package markdown

import (
	"html"
	"regexp"
	"strings"
)

type Kind int

const (
	Heading Kind = iota
	Paragraph
	Bullet
	Numbered
	Code
	Table
	Quote
	Rule
	PageBreak
	Image
)

// Block is one block-level element.
type Block struct {
	Kind  Kind
	Level int    // heading level 1–6, or list nesting depth from 0
	Text  string // inline Markdown (code: raw text; image: alt text)
	Lang  string // code block language
	Rows  [][]string
	URL   string // image source
}

var (
	reHeading  = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	reBullet   = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	reNumbered = regexp.MustCompile(`^(\s*)\d+[.)]\s+(.*)$`)
	reRule     = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	reTableSep = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
	reImage    = regexp.MustCompile(`^\s*!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)\s*$`)
)

// Parse splits Markdown into blocks.
func Parse(src string) []Block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []Block
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, Block{Kind: Paragraph, Text: strings.Join(para, " ")})
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			flush()
		case strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~"):
			flush()
			fence := trim[:3]
			lang := strings.TrimSpace(trim[3:])
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence); i++ {
				code = append(code, lines[i])
			}
			out = append(out, Block{Kind: Code, Text: strings.Join(code, "\n"), Lang: lang})
		case trim == `\pagebreak` || trim == "<!-- pagebreak -->":
			flush()
			out = append(out, Block{Kind: PageBreak})
		case reHeading.MatchString(trim):
			flush()
			m := reHeading.FindStringSubmatch(trim)
			out = append(out, Block{Kind: Heading, Level: len(m[1]), Text: m[2]})
		case reRule.MatchString(trim):
			flush()
			out = append(out, Block{Kind: Rule})
		case reImage.MatchString(trim):
			flush()
			m := reImage.FindStringSubmatch(trim)
			out = append(out, Block{Kind: Image, Text: m[1], URL: m[2]})
		case strings.HasPrefix(trim, "|") && i+1 < len(lines) && reTableSep.MatchString(lines[i+1]):
			flush()
			rows := [][]string{splitRow(trim)}
			for i += 2; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, splitRow(strings.TrimSpace(lines[i])))
			}
			i--
			out = append(out, Block{Kind: Table, Rows: rows})
		case strings.HasPrefix(trim, ">"):
			flush()
			var q []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">"); i++ {
				q = append(q, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")))
			}
			i--
			out = append(out, Block{Kind: Quote, Text: strings.Join(q, " ")})
		case reBullet.MatchString(line):
			flush()
			m := reBullet.FindStringSubmatch(line)
			out = append(out, Block{Kind: Bullet, Level: indent(m[1]), Text: m[2]})
		case reNumbered.MatchString(line):
			flush()
			m := reNumbered.FindStringSubmatch(line)
			out = append(out, Block{Kind: Numbered, Level: indent(m[1]), Text: m[2]})
		case len(para) == 0 && len(out) > 0 && (line[0] == ' ' || line[0] == '\t') &&
			(out[len(out)-1].Kind == Bullet || out[len(out)-1].Kind == Numbered):
			// An indented line under a list item continues that item.
			out[len(out)-1].Text += " " + trim
		default:
			para = append(para, trim)
		}
	}
	flush()
	return out
}

func indent(ws string) int {
	n := strings.Count(ws, "\t")*4 + strings.Count(ws, " ")
	return min(n/2, 5)
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// Span is a run of inline text with one style.
type Span struct {
	Text               string
	Bold, Italic, Code bool
	Link               string
}

var reInline = regexp.MustCompile("`([^`]+)`|\\*\\*([^*]+)\\*\\*|__([^_]+)__|\\*([^*\\s][^*]*)\\*|\\[([^\\]]+)\\]\\(([^)\\s]+)[^)]*\\)")

// Inline splits inline Markdown into styled spans.
func Inline(s string) []Span {
	var out []Span
	last := 0
	for _, m := range reInline.FindAllStringSubmatchIndex(s, -1) {
		if m[0] > last {
			out = append(out, Span{Text: s[last:m[0]]})
		}
		switch {
		case m[2] >= 0:
			out = append(out, Span{Text: s[m[2]:m[3]], Code: true})
		case m[4] >= 0:
			out = append(out, Span{Text: s[m[4]:m[5]], Bold: true})
		case m[6] >= 0:
			out = append(out, Span{Text: s[m[6]:m[7]], Bold: true})
		case m[8] >= 0:
			out = append(out, Span{Text: s[m[8]:m[9]], Italic: true})
		case m[10] >= 0:
			out = append(out, Span{Text: s[m[10]:m[11]], Link: s[m[12]:m[13]]})
		}
		last = m[1]
	}
	if last < len(s) {
		out = append(out, Span{Text: s[last:]})
	}
	return out
}

// Plain returns inline Markdown with the markup removed.
func Plain(s string) string {
	var b strings.Builder
	for _, sp := range Inline(s) {
		b.WriteString(sp.Text)
	}
	return b.String()
}

func inlineHTML(s string) string {
	var b strings.Builder
	for _, sp := range Inline(s) {
		t := html.EscapeString(sp.Text)
		switch {
		case sp.Code:
			t = "<code>" + t + "</code>"
		case sp.Bold:
			t = "<strong>" + t + "</strong>"
		case sp.Italic:
			t = "<em>" + t + "</em>"
		case sp.Link != "":
			t = `<a href="` + html.EscapeString(sp.Link) + `">` + t + "</a>"
		}
		b.WriteString(t)
	}
	return b.String()
}

// HTML renders blocks as an HTML fragment.
func HTML(blocks []Block) string {
	var b strings.Builder
	var lists []string // open list tags, innermost last
	closeLists := func(depth int) {
		for len(lists) > depth {
			b.WriteString("</" + lists[len(lists)-1] + ">\n")
			lists = lists[:len(lists)-1]
		}
	}
	for _, bl := range blocks {
		if bl.Kind != Bullet && bl.Kind != Numbered {
			closeLists(0)
		}
		switch bl.Kind {
		case Heading:
			lv := string(rune('0' + bl.Level))
			b.WriteString("<h" + lv + ">" + inlineHTML(bl.Text) + "</h" + lv + ">\n")
		case Paragraph:
			b.WriteString("<p>" + inlineHTML(bl.Text) + "</p>\n")
		case Bullet, Numbered:
			tag := "ul"
			if bl.Kind == Numbered {
				tag = "ol"
			}
			closeLists(bl.Level + 1)
			if len(lists) == bl.Level+1 && lists[bl.Level] != tag {
				closeLists(bl.Level)
			}
			for len(lists) < bl.Level+1 {
				b.WriteString("<" + tag + ">\n")
				lists = append(lists, tag)
			}
			b.WriteString("<li>" + inlineHTML(bl.Text) + "</li>\n")
		case Code:
			b.WriteString("<pre><code>" + html.EscapeString(bl.Text) + "</code></pre>\n")
		case Table:
			b.WriteString("<table>\n")
			for i, row := range bl.Rows {
				cell := "td"
				if i == 0 {
					cell = "th"
				}
				b.WriteString("<tr>")
				for _, c := range row {
					b.WriteString("<" + cell + ">" + inlineHTML(c) + "</" + cell + ">")
				}
				b.WriteString("</tr>\n")
			}
			b.WriteString("</table>\n")
		case Quote:
			b.WriteString("<blockquote>" + inlineHTML(bl.Text) + "</blockquote>\n")
		case Rule, PageBreak:
			b.WriteString("<hr>\n")
		case Image:
			b.WriteString(`<p><img src="` + html.EscapeString(bl.URL) + `" alt="` + html.EscapeString(bl.Text) + `"></p>` + "\n")
		}
	}
	closeLists(0)
	return b.String()
}

// Page wraps rendered Markdown in a standalone, GitHub-like HTML page.
// base, when set, is where relative links and images resolve from.
func Page(title, body, base string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\">")
	if base != "" {
		b.WriteString(`<base href="` + html.EscapeString(base) + `">`)
	}
	b.WriteString("<title>" + html.EscapeString(title) + "</title><style>" + pageCSS + "</style></head><body><main>\n")
	b.WriteString(body)
	b.WriteString("</main></body></html>\n")
	return b.String()
}

const pageCSS = `body{margin:0;background:#fff;color:#1f2328;font:16px/1.6 -apple-system,"Segoe UI",Helvetica,Arial,sans-serif}
main{max-width:880px;margin:0 auto;padding:32px}
h1,h2{border-bottom:1px solid #d1d9e0;padding-bottom:.3em}
h1,h2,h3,h4{margin:1.4em 0 .6em;line-height:1.25}
code{background:#eff1f3;padding:.2em .4em;border-radius:6px;font:85% ui-monospace,Consolas,monospace}
pre{background:#f6f8fa;padding:16px;border-radius:6px;overflow:auto}
pre code{background:none;padding:0}
table{border-collapse:collapse;margin:1em 0}
th,td{border:1px solid #d1d9e0;padding:6px 13px}
th{background:#f6f8fa}
blockquote{margin:0;padding:0 1em;color:#59636e;border-left:.25em solid #d1d9e0}
img{max-width:100%}
a{color:#0969da}`
