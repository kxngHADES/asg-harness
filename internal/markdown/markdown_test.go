package markdown

import (
	"strings"
	"testing"
)

func TestParseBlocks(t *testing.T) {
	md := "# Title #\n\nPara line one\nline two\n\n- a\n  - b\n1. c\n\n```sh\necho hi\n```\n\n| h1 | h2 |\n|:--|--:|\n| x | y |\n\n> q1\n> q2\n\n---\n![logo](img/logo.png)\n"
	got := Parse(md)
	want := []Kind{Heading, Paragraph, Bullet, Bullet, Numbered, Code, Table, Quote, Rule, Image}
	if len(got) != len(want) {
		t.Fatalf("got %d blocks: %+v", len(got), got)
	}
	for i, k := range want {
		if got[i].Kind != k {
			t.Errorf("block %d: kind %d, want %d", i, got[i].Kind, k)
		}
	}
	if got[0].Text != "Title" || got[1].Text != "Para line one line two" || got[3].Level != 1 {
		t.Errorf("unexpected content: %+v", got[:4])
	}
	if got[5].Lang != "sh" || got[5].Text != "echo hi" || len(got[6].Rows) != 2 || got[7].Text != "q1 q2" || got[9].URL != "img/logo.png" {
		t.Errorf("unexpected content: %+v", got[5:])
	}
}

// Found by screenshotting tools/README.md: an indented continuation line
// broke the numbered list in two.
func TestListItemContinuationLines(t *testing.T) {
	got := Parse("1. Create a file\n   (and register it).\n2. Test it\n\nAfter.")
	if len(got) != 3 || got[0].Text != "Create a file (and register it)." || got[1].Kind != Numbered || got[2].Kind != Paragraph {
		t.Errorf("continuation should join the item: %+v", got)
	}
}

func TestInline(t *testing.T) {
	sp := Inline("a **b** *c* `d` [e](http://x) __f__")
	var b strings.Builder
	for _, s := range sp {
		switch {
		case s.Bold:
			b.WriteString("B(" + s.Text + ")")
		case s.Italic:
			b.WriteString("I(" + s.Text + ")")
		case s.Code:
			b.WriteString("C(" + s.Text + ")")
		case s.Link != "":
			b.WriteString("L(" + s.Text + "→" + s.Link + ")")
		default:
			b.WriteString(s.Text)
		}
	}
	if got := b.String(); got != "a B(b) I(c) C(d) L(e→http://x) B(f)" {
		t.Errorf("got %q", got)
	}
	if Plain("**x** `y`") != "x y" {
		t.Error("Plain should drop markup")
	}
}

func TestHTMLNestsListsAndEscapes(t *testing.T) {
	h := HTML(Parse("- a\n  - b\n- c <x>\n\n1. n"))
	want := "<ul>\n<li>a</li>\n<ul>\n<li>b</li>\n</ul>\n<li>c &lt;x&gt;</li>\n</ul>\n<ol>\n<li>n</li>\n</ol>\n"
	if h != want {
		t.Errorf("got\n%s\nwant\n%s", h, want)
	}
	if p := Page("t", h, "file:///C:/d/"); !strings.Contains(p, `<base href="file:///C:/d/">`) {
		t.Error("page should carry the base href")
	}
}
