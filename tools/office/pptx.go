package office

import (
	"fmt"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/markdown"
)

// Slide geometry (16:9) in EMU.
const (
	slideW = 12192000
	slideH = 6858000
	margin = 609600
)

// SlidesFromMarkdown turns Markdown into slides: each # / ## heading or
// --- rule starts a slide; list items become bullets and other text
// becomes plain lines.
func SlidesFromMarkdown(md string) []Slide {
	var slides []Slide
	cur := -1
	start := func(title string) {
		slides = append(slides, Slide{Title: title})
		cur = len(slides) - 1
	}
	add := func(line string) {
		if cur < 0 {
			start("")
		}
		slides[cur].Bullets = append(slides[cur].Bullets, line)
	}
	for _, b := range markdown.Parse(md) {
		switch b.Kind {
		case markdown.Heading:
			if b.Level <= 2 {
				start(markdown.Plain(b.Text))
			} else {
				add("§" + markdown.Plain(b.Text)) // sub-heading: bold line
			}
		case markdown.Rule, markdown.PageBreak:
			cur = -1
		case markdown.Bullet, markdown.Numbered:
			add(strings.Repeat("  ", b.Level) + markdown.Plain(b.Text))
		case markdown.Paragraph, markdown.Quote:
			if cur >= 0 && len(slides[cur].Bullets) == 0 && slides[cur].Subtitle == "" && b.Kind == markdown.Paragraph {
				slides[cur].Subtitle = markdown.Plain(b.Text)
				continue
			}
			add("¶" + markdown.Plain(b.Text)) // plain text, no bullet
		case markdown.Code:
			for _, l := range strings.Split(b.Text, "\n") {
				add("¶" + l)
			}
		case markdown.Table:
			for _, r := range b.Rows {
				add("¶" + markdown.Plain(strings.Join(r, "  |  ")))
			}
		}
	}
	// A slide that only has a subtitle and comes after content keeps the
	// subtitle as a plain line rather than turning into a title slide.
	for i := range slides {
		if i > 0 && len(slides[i].Bullets) == 0 && slides[i].Subtitle != "" {
			slides[i].Bullets = []string{"¶" + slides[i].Subtitle}
			slides[i].Subtitle = ""
		}
	}
	return slides
}

func pptx(title, author string, slides []Slide) []file {
	var types, ids, rels strings.Builder
	var files []file
	for i, s := range slides {
		n := i + 1
		fmt.Fprintf(&types, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, n)
		fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="rId%d"/>`, 255+n, n+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, n+1, n)
		files = append(files,
			file{fmt.Sprintf("ppt/slides/slide%d.xml", n), slideXML(s)},
			file{fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
				`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`},
		)
	}
	themeID := len(slides) + 2
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/>`, themeID)

	return append(files,
		file{"[Content_Types].xml", xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>` +
			`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>` +
			`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>` +
			`<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` +
			`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
			types.String() + `</Types>`},
		file{"_rels/.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
			`</Relationships>`},
		file{"ppt/presentation.xml", xmlHeader + `<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" saveSubsetFonts="1">` +
			`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>` +
			`<p:sldIdLst>` + ids.String() + `</p:sldIdLst>` +
			fmt.Sprintf(`<p:sldSz cx="%d" cy="%d"/><p:notesSz cx="6858000" cy="9144000"/>`, slideW, slideH) +
			`</p:presentation>`},
		file{"ppt/_rels/presentation.xml.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>` +
			rels.String() + `</Relationships>`},
		file{"ppt/slideMasters/slideMaster1.xml", xmlHeader + `<p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">` +
			`<p:cSld><p:bg><p:bgRef idx="1001"><a:schemeClr val="bg1"/></p:bgRef></p:bg>` + emptyTree + `</p:cSld>` +
			`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
			`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`},
		file{"ppt/slideMasters/_rels/slideMaster1.xml.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>` +
			`</Relationships>`},
		file{"ppt/slideLayouts/slideLayout1.xml", xmlHeader + `<p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="blank" preserve="1">` +
			`<p:cSld name="Blank">` + emptyTree + `</p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`},
		file{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`},
		file{"ppt/theme/theme1.xml", themeXML},
		file{"docProps/core.xml", coreProps(title, author)},
	)
}

const emptyTree = `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
	`<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr></p:spTree>`

// textBox is a positioned shape holding paragraphs.
func textBox(id int, name string, x, y, w, h int, anchor, paras string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>`+
		`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>`+
		`<p:txBody><a:bodyPr wrap="square" lIns="91440" tIns="45720" rIns="91440" bIns="45720" anchor="%s"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
		id, name, x, y, w, h, anchor, paras)
}

func run(text string, size int, bold bool, color string) string {
	b := ""
	if bold {
		b = ` b="1"`
	}
	return fmt.Sprintf(`<a:r><a:rPr lang="en-US" sz="%d"%s dirty="0"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Calibri"/></a:rPr><a:t>%s</a:t></a:r>`,
		size, b, color, esc(text))
}

func slideXML(s Slide) string {
	var shapes strings.Builder
	titleOnly := len(s.Bullets) == 0
	if titleOnly {
		// Title slide: big centred title with the subtitle under it.
		shapes.WriteString(textBox(2, "Title", margin, slideH/2-1371600, slideW-2*margin, 1371600, "b",
			`<a:p><a:pPr algn="ctr"/>`+run(s.Title, 4400, true, "1F3864")+`</a:p>`))
		if s.Subtitle != "" {
			shapes.WriteString(textBox(3, "Subtitle", margin, slideH/2+91440, slideW-2*margin, 1143000, "t",
				`<a:p><a:pPr algn="ctr"/>`+run(s.Subtitle, 2400, false, "595959")+`</a:p>`))
		}
	} else {
		top := margin / 2
		shapes.WriteString(textBox(2, "Title", margin, top, slideW-2*margin, 1005840, "b",
			`<a:p>`+run(s.Title, 3200, true, "1F3864")+`</a:p>`))
		bodyTop := top + 1005840 + 182880
		if s.Subtitle != "" {
			shapes.WriteString(textBox(3, "Subtitle", margin, bodyTop, slideW-2*margin, 457200, "t",
				`<a:p>`+run(s.Subtitle, 1800, false, "595959")+`</a:p>`))
			bodyTop += 457200
		}
		var paras strings.Builder
		size := 2000
		if len(s.Bullets) > 8 {
			size = 1600
		}
		for _, line := range s.Bullets {
			trimmed := strings.TrimLeft(line, " ")
			level := min((len(line)-len(trimmed))/2, 4)
			switch {
			case strings.HasPrefix(trimmed, "¶"):
				fmt.Fprintf(&paras, `<a:p><a:pPr marL="0" indent="0"><a:buNone/></a:pPr>%s</a:p>`, run(strings.TrimPrefix(trimmed, "¶"), size, false, "262626"))
			case strings.HasPrefix(trimmed, "§"):
				fmt.Fprintf(&paras, `<a:p><a:pPr marL="0" indent="0"><a:spcBef><a:spcPts val="600"/></a:spcBef><a:buNone/></a:pPr>%s</a:p>`, run(strings.TrimPrefix(trimmed, "§"), size+200, true, "2F5496"))
			default:
				trimmed = strings.TrimLeft(trimmed, "-*• ")
				fmt.Fprintf(&paras, `<a:p><a:pPr marL="%d" lvl="%d" indent="-285750"><a:spcBef><a:spcPts val="600"/></a:spcBef><a:buFont typeface="Arial"/><a:buChar char="•"/></a:pPr>%s</a:p>`,
					342900+level*342900, level, run(trimmed, size-level*200, false, "262626"))
			}
		}
		shapes.WriteString(textBox(4, "Body", margin, bodyTop, slideW-2*margin, slideH-bodyTop-margin/2, "t", paras.String()))
	}
	tree := strings.Replace(emptyTree, `</p:spTree>`, shapes.String()+`</p:spTree>`, 1)
	return xmlHeader + `<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">` +
		`<p:cSld>` + tree + `</p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

const themeXML = xmlHeader + `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="ASG"><a:themeElements>` +
	`<a:clrScheme name="ASG"><a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F3864"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4>` +
	`<a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6><a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme>` +
	`<a:fontScheme name="ASG"><a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
	`<a:fmtScheme name="ASG"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst>` +
	`<a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="19050"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst>` +
	`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
	`<a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme>` +
	`</a:themeElements><a:objectDefaults/><a:extraClrSchemeLst/></a:theme>`
