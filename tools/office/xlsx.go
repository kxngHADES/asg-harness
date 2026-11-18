package office

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// xlsx builds a SpreadsheetML package. Cells use inline strings, so no
// shared-string table is needed. The first row of each sheet is a bold,
// frozen header.
func xlsx(title, author string, sheets []Sheet) []file {
	var types, wbSheets, wbRels strings.Builder
	var files []file
	used := map[string]bool{}
	for i, sh := range sheets {
		n := i + 1
		name := sheetName(sh.Name, n, used)
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(name), n, n)
		fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		files = append(files, file{fmt.Sprintf("xl/worksheets/sheet%d.xml", n), sheetXML(sh.Rows)})
	}
	stylesID := len(sheets) + 1
	fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, stylesID)

	return append(files,
		file{"[Content_Types].xml", xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
			types.String() + `</Types>`},
		file{"_rels/.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
			`</Relationships>`},
		file{"xl/workbook.xml", xmlHeader + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<bookViews><workbookView/></bookViews><sheets>` + wbSheets.String() + `</sheets></workbook>`},
		file{"xl/_rels/workbook.xml.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + wbRels.String() + `</Relationships>`},
		file{"xl/styles.xml", xlsxStyles},
		file{"docProps/core.xml", coreProps(title, author)},
	)
}

// sheetName makes a valid, unique Excel sheet name (≤31 chars, no []:*?/\).
func sheetName(name string, n int, used map[string]bool) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Trim(name, "'")
	if name == "" {
		name = fmt.Sprintf("Sheet%d", n)
	}
	if utf8.RuneCountInString(name) > 31 {
		name = string([]rune(name)[:31])
	}
	base := name
	for i := 2; used[strings.ToLower(name)]; i++ {
		suffix := fmt.Sprintf(" (%d)", i)
		r := []rune(base)
		if len(r)+len(suffix) > 31 {
			r = r[:31-len(suffix)]
		}
		name = string(r) + suffix
	}
	used[strings.ToLower(name)] = true
	return name
}

func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func sheetXML(rows [][]any) string {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	widths := make([]int, cols)
	var data strings.Builder
	for ri, row := range rows {
		fmt.Fprintf(&data, `<row r="%d">`, ri+1)
		for ci, v := range row {
			ref := fmt.Sprintf("%s%d", colName(ci), ri+1)
			style := ""
			if ri == 0 {
				style = ` s="1"`
			}
			text, cell := cellXML(ref, style, v)
			widths[ci] = max(widths[ci], utf8.RuneCountInString(text))
			data.WriteString(cell)
		}
		data.WriteString(`</row>`)
	}
	var b strings.Builder
	b.WriteString(xmlHeader + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	if len(rows) > 1 {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	}
	if cols > 0 {
		b.WriteString(`<cols>`)
		for i, w := range widths {
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, i+1, i+1, min(max(w+2, 8), 60))
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>` + data.String() + `</sheetData></worksheet>`)
	return b.String()
}

// cellXML renders one cell and returns its display text (for widths).
func cellXML(ref, style string, v any) (string, string) {
	switch x := v.(type) {
	case nil:
		return "", ""
	case bool:
		n, text := "0", "FALSE"
		if x {
			n, text = "1", "TRUE"
		}
		return text, fmt.Sprintf(`<c r="%s"%s t="b"><v>%s</v></c>`, ref, style, n)
	case float64:
		s := strconv.FormatFloat(x, 'f', -1, 64)
		return s, fmt.Sprintf(`<c r="%s"%s><v>%s</v></c>`, ref, style, s)
	case int:
		s := strconv.Itoa(x)
		return s, fmt.Sprintf(`<c r="%s"%s><v>%s</v></c>`, ref, style, s)
	}
	s := fmt.Sprint(v)
	trim := strings.TrimSpace(s)
	if strings.HasPrefix(trim, "=") && len(trim) > 1 {
		return trim, fmt.Sprintf(`<c r="%s"%s><f>%s</f></c>`, ref, style, esc(trim[1:]))
	}
	// Numbers written as text ("42", "3.5") are stored as numbers, but not
	// ones whose leading zeros matter ("007") or that are too long.
	if f, err := strconv.ParseFloat(trim, 64); err == nil && len(trim) < 16 && !(len(trim) > 1 && trim[0] == '0' && trim[1] != '.') {
		n := strconv.FormatFloat(f, 'f', -1, 64)
		return trim, fmt.Sprintf(`<c r="%s"%s><v>%s</v></c>`, ref, style, n)
	}
	return s, fmt.Sprintf(`<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, esc(s))
}

const xlsxStyles = xmlHeader + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFF2F2F2"/><bgColor indexed="64"/></patternFill></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/></cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`
