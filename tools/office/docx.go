package office

import (
	"fmt"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/markdown"
)

// docx builds a WordprocessingML package from Markdown blocks.
func docx(title, author string, blocks []markdown.Block) []file {
	var body strings.Builder
	if title != "" {
		body.WriteString(para("Title", runs(title, false)))
	}
	numbered := 0 // numbering instances used by numbered lists (ids start at 2)
	inNumbered := false
	for _, b := range blocks {
		// Sub-bullets under a numbered item don't end the numbered list.
		if b.Kind != markdown.Numbered && !(b.Kind == markdown.Bullet && b.Level > 0) {
			inNumbered = false
		}
		switch b.Kind {
		case markdown.Heading:
			body.WriteString(para(fmt.Sprintf("Heading%d", min(b.Level, 4)), runs(b.Text, false)))
		case markdown.Paragraph:
			body.WriteString(para("", runs(b.Text, false)))
		case markdown.Bullet:
			body.WriteString(listPara(1, b.Level, runs(b.Text, false)))
		case markdown.Numbered:
			if !inNumbered && b.Level == 0 {
				numbered++
				inNumbered = true
			}
			body.WriteString(listPara(1+max(numbered, 1), b.Level, runs(b.Text, false)))
		case markdown.Code:
			var r strings.Builder
			for i, line := range strings.Split(b.Text, "\n") {
				if i > 0 {
					r.WriteString(`<w:r><w:br/></w:r>`)
				}
				r.WriteString(`<w:r><w:t xml:space="preserve">` + esc(line) + `</w:t></w:r>`)
			}
			body.WriteString(para("Code", r.String()))
		case markdown.Quote:
			body.WriteString(para("Quote", runs(b.Text, false)))
		case markdown.Table:
			body.WriteString(table(b.Rows))
		case markdown.Rule:
			body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="AAAAAA"/></w:pBdr></w:pPr></w:p>`)
		case markdown.PageBreak:
			body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		case markdown.Image:
			body.WriteString(para("", runs("[image: "+b.Text+" — "+b.URL+"]", false)))
		}
	}

	doc := xmlHeader + `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
		body.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>`

	return []file{
		{"[Content_Types].xml", xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
			`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
			`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
			`</Types>`},
		{"_rels/.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
			`</Relationships>`},
		{"word/_rels/document.xml.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` +
			`</Relationships>`},
		{"word/document.xml", doc},
		{"word/styles.xml", docxStyles},
		{"word/numbering.xml", numberingXML(max(numbered, 1))},
		{"docProps/core.xml", coreProps(title, author)},
	}
}

func para(style, content string) string {
	pPr := ""
	if style != "" {
		pPr = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return `<w:p>` + pPr + content + `</w:p>`
}

func listPara(numID, level int, content string) string {
	return fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr></w:pPr>%s</w:p>`,
		min(level, 2), numID, content)
}

// runs renders inline Markdown as Word runs.
func runs(text string, bold bool) string {
	var b strings.Builder
	for _, sp := range markdown.Inline(text) {
		rPr := ""
		if sp.Bold || bold {
			rPr += `<w:b/>`
		}
		if sp.Italic {
			rPr += `<w:i/>`
		}
		if sp.Code {
			rPr += `<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:shd w:val="clear" w:color="auto" w:fill="EFF1F3"/>`
		}
		if sp.Link != "" {
			rPr += `<w:color w:val="0563C1"/><w:u w:val="single"/>`
		}
		if rPr != "" {
			rPr = `<w:rPr>` + rPr + `</w:rPr>`
		}
		b.WriteString(`<w:r>` + rPr + `<w:t xml:space="preserve">` + esc(sp.Text) + `</w:t></w:r>`)
	}
	return b.String()
}

func table(rows [][]string) string {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblBorders>`)
	for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + side + ` w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/>`)
	}
	b.WriteString(`</w:tblBorders><w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/></w:tblPr><w:tblGrid>`)
	w := 9026 / cols
	for i := 0; i < cols; i++ {
		fmt.Fprintf(&b, `<w:gridCol w:w="%d"/>`, w)
	}
	b.WriteString(`</w:tblGrid>`)
	for i, r := range rows {
		b.WriteString(`<w:tr>`)
		if i == 0 {
			b.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for c := 0; c < cols; c++ {
			text := ""
			if c < len(r) {
				text = r[c]
			}
			shade := ""
			if i == 0 {
				shade = `<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`
			}
			fmt.Fprintf(&b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s<w:vAlign w:val="center"/></w:tcPr><w:p><w:pPr><w:spacing w:before="60" w:after="60"/><w:ind w:left="60"/></w:pPr>%s</w:p></w:tc>`, w, shade, runs(text, i == 0))
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl><w:p/>`)
	return b.String()
}

// numberingXML defines numId 1 as bullets and numIds 2..n+1 as numbered
// lists that each restart at 1.
func numberingXML(numberedLists int) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	bullets := []string{"•", "◦", "▪"}
	b.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for l := 0; l < 3; l++ {
		fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
			l, bullets[l], 720*(l+1))
	}
	b.WriteString(`</w:abstractNum><w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>`)
	formats := []string{"decimal", "lowerLetter", "lowerRoman"}
	for l := 0; l < 3; l++ {
		fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%%%d."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
			l, formats[l], l+1, 720*(l+1))
	}
	b.WriteString(`</w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`)
	for i := 0; i < numberedLists; i++ {
		fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`, i+2)
	}
	b.WriteString(`</w:numbering>`)
	return b.String()
}

const docxStyles = xmlHeader + `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="en-US"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="264" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="240"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri Light" w:hAnsi="Calibri Light"/><w:sz w:val="56"/><w:szCs w:val="56"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:color w:val="1F3864"/><w:sz w:val="36"/><w:szCs w:val="36"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="80"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:color w:val="2F5496"/><w:sz w:val="30"/><w:szCs w:val="30"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="60"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:color w:val="2F5496"/><w:sz w:val="26"/><w:szCs w:val="26"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val="3"/></w:pPr><w:rPr><w:b/><w:i/><w:color w:val="2F5496"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="60"/><w:contextualSpacing/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F6F8FA"/><w:spacing w:after="160" w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="BFBFBF"/></w:pBdr><w:ind w:left="360"/></w:pPr><w:rPr><w:i/><w:color w:val="595959"/></w:rPr></w:style>` +
	`</w:styles>`
