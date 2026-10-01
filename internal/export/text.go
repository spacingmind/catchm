package export

import (
	"strings"
	"unicode/utf8"
)

// Text renders the document as plain text.
func (d Document) Text() string {
	var b strings.Builder

	b.WriteString(d.Title + "\n")
	b.WriteString(strings.Repeat("=", utf8.RuneCountInString(d.Title)) + "\n")

	for _, m := range d.Meta {
		if m.Label == "" {
			b.WriteString("\n" + m.Value)
			continue
		}
		b.WriteString("\n" + m.Label + ": " + m.Value)
	}

	d.writeTextSections(&b)
	b.WriteString("\n")
	return b.String()
}

func (d Document) writeTextSections(b *strings.Builder) {
	if d.Summary != "" {
		b.WriteString("\n\n" + d.l.Summary + "\n" + strings.Repeat("-", utf8.RuneCountInString(d.l.Summary)) + "\n\n" + d.Summary)
	}
	if len(d.Decisions) > 0 {
		b.WriteString("\n\n" + d.l.Decisions + "\n" + strings.Repeat("-", utf8.RuneCountInString(d.l.Decisions)) + "\n")
		for _, dec := range d.Decisions {
			line := "- " + dec.Text
			if dec.Scope != "" {
				line += " (" + dec.Scope + ")"
			}
			b.WriteString("\n" + line)
		}
	}
	if len(d.Actions) > 0 {
		b.WriteString("\n\n" + d.l.ActionItems + "\n" + strings.Repeat("-", utf8.RuneCountInString(d.l.ActionItems)) + "\n")
		for _, a := range d.Actions {
			line := "- " + a.Text
			if a.Owner != "" {
				line += " — " + d.l.Owner + ": " + a.Owner
			}
			if a.Due != "" {
				line += " — " + d.l.Due + ": " + a.Due
			}
			if a.Done {
				line += " (" + d.l.Done + ")"
			}
			b.WriteString("\n" + line)
		}
	}
	if len(d.Notes) > 0 {
		b.WriteString("\n\n" + d.l.Notes + "\n" + strings.Repeat("-", utf8.RuneCountInString(d.l.Notes)) + "\n")
		for _, n := range d.Notes {
			b.WriteString("\n- " + n)
		}
	}
	if len(d.Groups) > 0 {
		b.WriteString("\n\n" + d.l.Transcript + "\n" + strings.Repeat("-", utf8.RuneCountInString(d.l.Transcript)) + "\n")
		for i, g := range d.Groups {
			if i > 0 {
				b.WriteString("\n")
			}
			if g.Speaker != "" {
				b.WriteString("\n" + g.Speaker + "\n")
			}
			for _, ln := range g.Lines {
				b.WriteString("\n[" + ln.Offset + "] " + ln.Text)
				if ln.Translated != "" {
					b.WriteString("\n  " + ln.Translated)
				}
			}
		}
	}
}

// Markdown renders the document as Markdown.
func (d Document) Markdown() string {
	var b strings.Builder

	b.WriteString("# " + escapeMD(d.Title) + "\n")

	last := len(d.Meta) - 1
	for i, m := range d.Meta {
		line := "\n"
		if m.Label != "" {
			line += escapeMD(m.Label) + ": "
		}
		line += escapeMD(m.Value)
		if i != last {
			line += "  "
		}
		b.WriteString(line)
	}

	d.writeMarkdownSections(&b)
	b.WriteString("\n")
	return b.String()
}

func (d Document) writeMarkdownSections(b *strings.Builder) {
	if d.Summary != "" {
		b.WriteString("\n\n## " + d.l.Summary + "\n\n" + escapeMD(d.Summary))
	}
	if len(d.Decisions) > 0 {
		b.WriteString("\n\n## " + d.l.Decisions + "\n")
		for _, dec := range d.Decisions {
			line := "\n- " + escapeMD(dec.Text)
			if dec.Scope != "" {
				line += " (" + escapeMD(dec.Scope) + ")"
			}
			b.WriteString(line)
		}
	}
	if len(d.Actions) > 0 {
		b.WriteString("\n\n## " + d.l.ActionItems + "\n")
		for _, a := range d.Actions {
			box := " "
			if a.Done {
				box = "x"
			}
			line := "\n- [" + box + "] " + escapeMD(a.Text)
			if a.Owner != "" {
				line += " — **" + d.l.Owner + ":** " + escapeMD(a.Owner)
			}
			if a.Due != "" {
				line += " — **" + d.l.Due + ":** " + escapeMD(a.Due)
			}
			b.WriteString(line)
		}
	}
	if len(d.Notes) > 0 {
		b.WriteString("\n\n## " + d.l.Notes + "\n")
		for _, n := range d.Notes {
			b.WriteString("\n- " + escapeMD(n))
		}
	}
	if len(d.Groups) > 0 {
		b.WriteString("\n\n## " + d.l.Transcript + "\n")
		for i, g := range d.Groups {
			if i > 0 {
				b.WriteString("\n")
			}
			if g.Speaker != "" {
				b.WriteString("\n**" + escapeMD(g.Speaker) + "**\n")
			}
			// One paragraph per speaker turn, every line a hard break, so
			// a translation never runs into the next line.
			var rows []string
			for _, ln := range g.Lines {
				rows = append(rows, "["+ln.Offset+"] "+escapeMD(ln.Text))
				if ln.Translated != "" {
					rows = append(rows, "*"+escapeMD(ln.Translated)+"*")
				}
			}
			b.WriteString("\n" + strings.Join(rows, "  \n"))
		}
	}
}

// escapeMD escapes user text so a line can't be mistaken for a Markdown
// block-level construct.
func escapeMD(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "#"),
			strings.HasPrefix(line, "-"),
			strings.HasPrefix(line, "*"),
			strings.HasPrefix(line, "+"),
			strings.HasPrefix(line, ">"):
			lines[i] = "\\" + line
		default:
			if n := numberPrefixEnd(line); n > 0 {
				lines[i] = line[:n-1] + "\\" + line[n-1:]
			}
		}
	}
	return strings.Join(lines, "\n")
}

// numberPrefixEnd returns the index just past a leading "12." or "12)"
// (including the punctuation), or 0 if the line doesn't start with one.
func numberPrefixEnd(line string) int {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(line) {
		return 0
	}
	if line[i] == '.' || line[i] == ')' {
		return i + 1
	}
	return 0
}
