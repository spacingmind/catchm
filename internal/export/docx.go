package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"strings"
)

// Docx renders the document as a .docx file.
func (d Document) Docx() ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)

	files := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", relsXML},
		{"word/_rels/document.xml.rels", docRelsXML},
		{"word/styles.xml", d.stylesXML()},
		{"word/document.xml", d.documentXML()},
	}
	for _, f := range files {
		w, err := z.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`

const relsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const docRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

const ( // shared style/run fragments
	fontXML = `<w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/>`
)

func (d Document) stylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr>` + fontXML + `<w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="48"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="240"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="160"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Translation"><w:name w:val="Translation"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="360"/></w:pPr><w:rPr><w:i/><w:color w:val="666666"/></w:rPr></w:style>
</w:styles>`
}

func (d Document) documentXML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	b.WriteString(para("Title", d.Title))
	for _, m := range d.Meta {
		if m.Label == "" {
			b.WriteString(para("Normal", m.Value))
			continue
		}
		b.WriteString(para("Normal", m.Label+": "+m.Value))
	}

	if d.Summary != "" {
		b.WriteString(para("Heading1", d.l.Summary))
		for _, line := range strings.Split(d.Summary, "\n") {
			if line != "" {
				b.WriteString(para("Normal", line))
			}
		}
	}
	if len(d.Decisions) > 0 {
		b.WriteString(para("Heading1", d.l.Decisions))
		for _, dec := range d.Decisions {
			text := "• " + dec.Text
			if dec.Scope != "" {
				text += " (" + dec.Scope + ")"
			}
			b.WriteString(para("Normal", text))
		}
	}
	if len(d.Actions) > 0 {
		b.WriteString(para("Heading1", d.l.ActionItems))
		for _, a := range d.Actions {
			text := "• " + a.Text
			if a.Owner != "" {
				text += " — " + d.l.Owner + ": " + a.Owner
			}
			if a.Due != "" {
				text += " — " + d.l.Due + ": " + a.Due
			}
			if a.Done {
				text += " (" + d.l.Done + ")"
			}
			b.WriteString(para("Normal", text))
		}
	}
	if len(d.Notes) > 0 {
		b.WriteString(para("Heading1", d.l.Notes))
		for _, n := range d.Notes {
			b.WriteString(para("Normal", "• "+n))
		}
	}
	if len(d.Groups) > 0 {
		b.WriteString(para("Heading1", d.l.Transcript))
		for _, g := range d.Groups {
			if g.Speaker != "" {
				b.WriteString(para("Heading2", g.Speaker))
			}
			for _, ln := range g.Lines {
				b.WriteString(para("Normal", "["+ln.Offset+"] "+ln.Text))
				if ln.Translated != "" {
					b.WriteString(para("Translation", ln.Translated))
				}
			}
		}
	}

	b.WriteString(`<w:sectPr/></w:body></w:document>`)
	return b.String()
}

// para emits one paragraph with an optional style and a single run.
func para(style, text string) string {
	pPr := ""
	if style != "" {
		pPr = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return `<w:p>` + pPr + `<w:r>` + runProps[style] + `<w:t xml:space="preserve">` + escapeXMLText(text) + `</w:t></w:r></w:p>`
}

// runProps repeats each style's character formatting on the run itself:
// macOS Quick Look, TextEdit and Pages ignore styles.xml entirely, so
// without this every heading would render as plain body text there.
var runProps = map[string]string{
	"Title":       `<w:rPr><w:b/><w:sz w:val="48"/></w:rPr>`,
	"Heading1":    `<w:rPr><w:b/><w:sz w:val="32"/></w:rPr>`,
	"Heading2":    `<w:rPr><w:b/><w:sz w:val="24"/></w:rPr>`,
	"Translation": `<w:rPr><w:i/><w:color w:val="666666"/></w:rPr>`,
}

// escapeXMLText strips characters invalid in XML 1.0 (control chars
// other than \t \n \r, which transcripts can contain) and escapes the
// rest for use in element content.
func escapeXMLText(s string) string {
	var clean strings.Builder
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		clean.WriteRune(r)
	}
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(clean.String())); err != nil {
		return ""
	}
	return escaped.String()
}
