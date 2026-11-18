package office

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// openPackage reads every part of an OOXML file, checks each XML part is
// well-formed, and checks that every relationship target exists and every
// part has a content type.
func openPackage(t *testing.T, file string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		parts[f.Name] = string(data)
		dec := xml.NewDecoder(strings.NewReader(string(data)))
		for {
			if _, err := dec.Token(); err != nil {
				if !errors.Is(err, io.EOF) {
					t.Errorf("%s: malformed XML: %v", f.Name, err)
				}
				break
			}
		}
	}
	types := parts["[Content_Types].xml"]
	reTarget := regexp.MustCompile(`Target="([^"]+)"`)
	for name, body := range parts {
		if !strings.HasSuffix(name, ".rels") {
			if !strings.HasSuffix(name, ".xml") || name == "[Content_Types].xml" {
				continue
			}
			if !strings.Contains(types, `PartName="/`+name+`"`) {
				t.Errorf("%s has no content type override", name)
			}
			continue
		}
		// _rels/x.xml.rels describes x.xml; targets are relative to x's folder.
		base := path.Dir(path.Dir(name))
		for _, m := range reTarget.FindAllStringSubmatch(body, -1) {
			target := path.Clean(path.Join(base, m[1]))
			if _, ok := parts[target]; !ok {
				t.Errorf("%s points at missing part %s", name, target)
			}
		}
	}
	return parts
}

func TestDocxFromMarkdown(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "report.docx")
	md := "# Findings\n\nSome **bold** and *italic* & `code` <tags>.\n\n" +
		"- one\n  - nested\n- two\n\n1. first\n   - detail\n2. second\n\nText between.\n\n1. again\n\n" +
		"| Name | Score |\n|---|---|\n| a | 1 |\n\n```go\nfunc main() {}\n```\n\n> quoted\n\n\\pagebreak\n\nEnd."
	res, err := Write(Request{Path: out, Title: "Pen test report", Author: "ASG", Markdown: md})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "wrote") {
		t.Errorf("summary: %q", res)
	}
	p := openPackage(t, out)
	doc := p["word/document.xml"]
	for _, want := range []string{"Pen test report", `w:val="Heading1"`, "<w:b/>", "&amp; ", "&lt;tags&gt;", "<w:tbl>", "Consolas", `w:type="page"`, `w:val="Quote"`} {
		if !strings.Contains(doc, want) && !strings.Contains(p["word/styles.xml"], want) {
			t.Errorf("document lacks %q", want)
		}
	}
	// The second numbered list restarts: two numbering instances, and the
	// sub-bullet under "first" did not split the first list.
	if n := strings.Count(p["word/numbering.xml"], "<w:startOverride"); n != 2 {
		t.Errorf("want 2 numbered lists, got %d", n)
	}
	if !strings.Contains(p["docProps/core.xml"], "Pen test report") {
		t.Error("title missing from file properties")
	}
}

func TestPptxFromMarkdownAndSlides(t *testing.T) {
	dir := t.TempDir()
	md := "# Q3 Plan\nRoadmap and risks\n\n## Goals\n- Ship v2\n  - beta in July\n- Hire 2\n\n## Risks\nBudget is tight.\n\n---\n\n## Next\n1. Approve"
	out := filepath.Join(dir, "deck.pptx")
	res, err := Write(Request{Path: out, Markdown: md})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "4 slides") {
		t.Errorf("want 4 slides: %s", res)
	}
	p := openPackage(t, out)
	if !strings.Contains(p["ppt/slides/slide1.xml"], "Roadmap and risks") || !strings.Contains(p["ppt/slides/slide1.xml"], `algn="ctr"`) {
		t.Error("first slide should be a title slide with its subtitle")
	}
	s2 := p["ppt/slides/slide2.xml"]
	if !strings.Contains(s2, "Ship v2") || !strings.Contains(s2, `lvl="1"`) || !strings.Contains(s2, "<a:buChar") {
		t.Error("slide 2 should have nested bullets")
	}
	if !strings.Contains(p["ppt/slides/slide3.xml"], "<a:buNone/>") {
		t.Error("plain paragraphs should not get bullets")
	}

	out2 := filepath.Join(dir, "exact.pptx")
	if _, err := Write(Request{Path: out2, Title: "Deck", Slides: []Slide{{Title: "A", Bullets: []string{"x", "  y"}}}}); err != nil {
		t.Fatal(err)
	}
	p2 := openPackage(t, out2)
	if len(p2) == 0 || !strings.Contains(p2["ppt/presentation.xml"], `<p:sldId id="257"`) {
		t.Error("title slide plus one slide expected")
	}
}

func TestXlsxCellsAndSources(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "data.xlsx")
	var rows [][]any
	_ = json.Unmarshal([]byte(`[["Item","Qty","Code","Total"],["Apples",3,"007","=B2*2"],["Pears","4.5",true,null]]`), &rows)
	if _, err := Write(Request{Path: out, Sheets: []Sheet{{Name: "Stock: [Q3]", Rows: rows}, {Name: "Stock: [Q3]"}}}); err != nil {
		t.Fatal(err)
	}
	p := openPackage(t, out)
	s1 := p["xl/worksheets/sheet1.xml"]
	for _, want := range []string{
		`<c r="B2"><v>3</v></c>`,                            // JSON number
		`<c r="B3"><v>4.5</v></c>`,                          // numeric text becomes a number
		`t="inlineStr"><is><t xml:space="preserve">007</t>`, // leading zeros stay text
		`<c r="D2"><f>B2*2</f></c>`,                         // formula
		`<c r="C3" t="b"><v>1</v></c>`,                      // bool
		`s="1"`,                                             // bold header
		`state="frozen"`,
	} {
		if !strings.Contains(s1, want) {
			t.Errorf("sheet lacks %s", want)
		}
	}
	wb := p["xl/workbook.xml"]
	if !strings.Contains(wb, `name="Stock- -Q3-"`) || !strings.Contains(wb, `name="Stock- -Q3- (2)"`) {
		t.Errorf("sheet names should be cleaned and unique: %s", wb)
	}

	csvOut := filepath.Join(dir, "csv.xlsx")
	if _, err := Write(Request{Path: csvOut, CSV: "a,b\n1,2\n"}); err != nil {
		t.Fatal(err)
	}
	mdOut := filepath.Join(dir, "md.xlsx")
	if res, err := Write(Request{Path: mdOut, Markdown: "| x | y |\n|---|---|\n| **1** | 2 |\n"}); err != nil || !strings.Contains(res, "1 sheet") {
		t.Fatalf("markdown table → sheet: %s %v", res, err)
	}
	if s := openPackage(t, mdOut)["xl/worksheets/sheet1.xml"]; !strings.Contains(s, `<c r="A2"><v>1</v></c>`) {
		t.Error("markdown markup should be stripped from cells")
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Request{
		{Path: ""},
		{Path: filepath.Join(dir, "x.pdf"), Markdown: "hi"},
		{Path: filepath.Join(dir, "x.xlsx"), Markdown: "no table"},
		{Path: filepath.Join(dir, "x.docx")},
	} {
		if _, err := Write(r); err == nil {
			t.Errorf("%+v should fail", r)
		}
	}
}

func TestEscDropsInvalidXMLChars(t *testing.T) {
	if got := esc("a\x00b\x1bc<&>\"\t"); got != "abc&lt;&amp;&gt;&quot;\t" {
		t.Errorf("esc = %q", got)
	}
}
