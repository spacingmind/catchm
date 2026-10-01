// Package export builds a whole-meeting document (title, meta, summary,
// decisions, action items, notes, transcript) rendered as text, Markdown
// or .docx. Headings follow the UI language.
package export

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/summarize"
)

// Lang is the UI language used for document headings.
type Lang string

const (
	En Lang = "en"
	Vi Lang = "vi"
)

func norm(l Lang) Lang {
	if l == Vi {
		return Vi
	}
	return En
}

type labels struct {
	Summary      string
	Decisions    string
	ActionItems  string
	Notes        string
	Transcript   string
	Participants string
	Project      string
	Tags         string
	Duration     string
	Done         string
	Owner        string
	Due          string
}

var enLabels = labels{
	Summary:      "Summary",
	Decisions:    "Decisions",
	ActionItems:  "Action items",
	Notes:        "Notes",
	Transcript:   "Transcript",
	Participants: "Participants",
	Project:      "Project",
	Tags:         "Tags",
	Duration:     "Duration",
	Done:         "done",
	Owner:        "Owner",
	Due:          "Due",
}

var viLabels = labels{
	Summary:      "Tóm tắt",
	Decisions:    "Quyết định",
	ActionItems:  "Việc cần làm",
	Notes:        "Ghi chú",
	Transcript:   "Bản chép lời",
	Participants: "Người tham dự",
	Project:      "Dự án",
	Tags:         "Thẻ",
	Duration:     "Thời lượng",
	Done:         "đã xong",
	Owner:        "Người thực hiện",
	Due:          "Hạn",
}

// MetaLine is one metadata row; Label is "" for the date line.
type MetaLine struct {
	Label string
	Value string
}

type Decision struct {
	Text  string
	Scope string
}

type Action struct {
	Text  string
	Owner string
	Due   string
	Done  bool
}

type Line struct {
	Offset     string
	Text       string
	Translated string
}

type Group struct {
	Speaker string
	Lines   []Line
}

// Document is a whole meeting ready for rendering.
type Document struct {
	Lang      Lang
	l         labels
	Title     string
	Meta      []MetaLine
	Summary   string
	Decisions []Decision
	Actions   []Action
	Notes     []string
	Groups    []Group
}

const (
	viDateLayout = "02/01/2006 15:04"
	enDateLayout = "Jan 2, 2006 3:04 PM"
)

// Build assembles a Document from a meeting, its people, and its
// transcript in the requested language.
func Build(m *meeting.Meeting, people []summarize.Person, us []meeting.Utterance, lang Lang) Document {
	lang = norm(lang)
	l := enLabels
	if lang == Vi {
		l = viLabels
	}

	layout := enDateLayout
	if lang == Vi {
		layout = viDateLayout
	}
	date := m.StartedAt.Format(layout)
	if m.EndedAt != nil {
		date += " – " + m.EndedAt.Format(layout)
	}
	var meta []MetaLine
	meta = append(meta, MetaLine{Value: date})
	if m.EndedAt != nil {
		meta = append(meta, MetaLine{Label: l.Duration, Value: durationString(m.EndedAt.Sub(m.StartedAt))})
	}
	if p := meeting.ProjectOf(m.Tags); p != "" {
		meta = append(meta, MetaLine{Label: l.Project, Value: p})
	}
	if tags := meeting.WithProject(m.Tags, ""); len(tags) > 0 {
		meta = append(meta, MetaLine{Label: l.Tags, Value: strings.Join(tags, ", ")})
	}
	// The meeting's own participant list; the analysis's people only
	// when the meeting has none.
	names := m.Participants
	if len(names) == 0 {
		for _, p := range people {
			if p.Name != "" {
				names = append(names, p.Name)
			}
		}
	}
	if len(names) > 0 {
		meta = append(meta, MetaLine{Label: l.Participants, Value: strings.Join(names, ", ")})
	}

	var decisions []Decision
	for _, d := range m.Decisions {
		decisions = append(decisions, Decision{Text: d.Text, Scope: d.Scope})
	}
	var actions []Action
	for _, a := range m.ActionItems {
		actions = append(actions, Action{Text: a.Text, Owner: a.Owner, Due: a.Due, Done: a.Status == "done"})
	}
	var notes []string
	for _, n := range m.Notes {
		if s := strings.TrimSpace(n); s != "" {
			notes = append(notes, s)
		}
	}

	speakerNames := map[string]string{}
	for _, p := range people {
		if p.Speaker != "" && p.Name != "" {
			speakerNames[p.Speaker] = p.Name
		}
	}
	var groups []Group
	var base time.Time
	for _, u := range us {
		text := strings.TrimSpace(u.Text)
		if text == "" {
			continue
		}
		if len(groups) == 0 {
			base = u.At
		}
		display := u.Speaker
		if name, ok := speakerNames[u.Speaker]; ok {
			display = name
		}
		if len(groups) == 0 || groups[len(groups)-1].Speaker != display {
			groups = append(groups, Group{Speaker: display})
		}
		g := &groups[len(groups)-1]
		g.Lines = append(g.Lines, Line{Offset: offsetString(u.At.Sub(base)), Text: text, Translated: strings.TrimSpace(u.Translated)})
	}

	return Document{
		Lang:      norm(lang),
		l:         l,
		Title:     m.Title,
		Meta:      meta,
		Summary:   strings.TrimSpace(m.Summary),
		Decisions: decisions,
		Actions:   actions,
		Notes:     notes,
		Groups:    groups,
	}
}

func offsetString(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h, rem := total/3600, total%3600
	m, s := rem/60, rem%60
	if h == 0 {
		return fmt.Sprintf("%02d:%02d", m, s)
	}
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

func durationString(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// Filename returns UTF-8 and ASCII-safe filenames for a meeting export
// with the given extension (e.g. "md", "docx").
func Filename(m *meeting.Meeting, ext string) (utf8Name, asciiName string) {
	date := m.StartedAt.Format("2006-01-02")
	return slug(m.Title, unicode.IsLetter, unicode.IsDigit) + "-" + date + "." + ext,
		slug(summarize.FoldDiacritics(strings.ToLower(m.Title)),
			func(r rune) bool { return r < unicode.MaxASCII && unicode.IsLetter(r) },
			func(r rune) bool { return r < unicode.MaxASCII && unicode.IsDigit(r) }) + "-" + date + "." + ext
}

func slug(title string, letter, digit func(rune) bool) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(title) {
		if letter(r) || digit(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "meeting"
	}
	return s
}
