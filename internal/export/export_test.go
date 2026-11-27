package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/summarize"
)

// fixtureDoc builds a fully populated document in the given language.
func fixtureDoc(t *testing.T, lang Lang) Document {
	t.Helper()
	start := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	m := &meeting.Meeting{
		Title:        "Họp dự án Q3",
		StartedAt:    start,
		EndedAt:      &end,
		Tags:         []string{"project:catchm", "planning"},
		Participants: []string{"An", "Bình"},
		Summary:      "Wrapped up Q3 scope.",
		Decisions: []meeting.Decision{
			{Text: "Ship on Friday", Scope: "team"},
			{Text: "Freeze dependencies"},
		},
		ActionItems: []meeting.ActionItem{
			{Text: "Prepare demo", Owner: "An", Due: "2026-09-19", Status: "open"},
			{Text: "Send notes", Status: "done"},
		},
		Notes: []string{"Check budget", "  "},
	}
	us := []meeting.Utterance{
		{At: start, Speaker: "Speaker 1", Text: "Chào mọi người."},
		{At: start.Add(30 * time.Second), Speaker: "Speaker 1", Text: "Hello everyone.", Translated: "translation"},
		{At: start.Add(time.Hour + 5*time.Minute), Speaker: "Speaker 2", Text: "Bắt đầu nhé."},
	}
	people := []summarize.Person{{Name: "An", Speaker: "Speaker 1"}}
	return Build(m, m.Namer(""), people, us, lang)
}

func TestRenderContainsExpectedContent(t *testing.T) {
	cases := []struct {
		lang     Lang
		summary  string
		done     string // text-format done marker
		duration string
		project  string
		tags     string
	}{
		{En, "Summary", "(done)", "Duration: 1h 30m", "Project: catchm", "Tags: planning"},
		{Vi, "Tóm tắt", "(đã xong)", "Thời lượng: 1h 30m", "Dự án: catchm", "Thẻ: planning"},
	}
	for _, c := range cases {
		d := fixtureDoc(t, c.lang)
		text := d.Text()
		md := d.Markdown()
		if !strings.Contains(text, c.done) {
			t.Fatalf("%s Text: missing %q; got:\n%s", c.lang, c.done, text)
		}
		if !strings.Contains(md, "- [x] ") {
			t.Fatalf("%s Markdown: missing - [x]; got:\n%s", c.lang, md)
		}
		for _, out := range []struct{ name, body string }{{"Text", text}, {"Markdown", md}} {
			for _, want := range []string{
				c.summary,
				"An",
				"[00:30]",
				"[1:05:00]",
				c.duration,
				c.project,
				c.tags,
				"An, Bình",
			} {
				if !strings.Contains(out.body, want) {
					t.Fatalf("%s %s: missing %q; got:\n%s", c.lang, out.name, want, out.body)
				}
			}
			if strings.Contains(out.body, "Speaker 1") {
				t.Fatalf("%s %s: raw speaker label leaked; got:\n%s", c.lang, out.name, out.body)
			}
		}
		if !strings.Contains(md, "*translation*") {
			t.Fatalf("%s Markdown: missing *translation*; got:\n%s", c.lang, md)
		}
	}
}

func TestEmptyMeetingHasNoSections(t *testing.T) {
	m := &meeting.Meeting{Title: "Empty", StartedAt: time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)}
	for _, lang := range []Lang{En, Vi} {
		d := Build(m, m.Namer(""), nil, nil, lang)
		for _, out := range []struct{ name, body string }{{"Text", d.Text()}, {"Markdown", d.Markdown()}} {
			doc, err := d.Docx()
			if err != nil {
				t.Fatalf("Docx: %v", err)
			}
			_ = doc
			for _, heading := range []string{d.l.Summary, d.l.Decisions, d.l.ActionItems, d.l.Notes, d.l.Transcript} {
				if strings.Contains(out.body, heading) {
					t.Fatalf("%s %s: unexpected section %q; got:\n%s", lang, out.name, heading, out.body)
				}
			}
		}
		docXML := Build(m, m.Namer(""), nil, nil, En).documentXML()
		for _, heading := range []string{"Summary", "Decisions", "Action items", "Notes", "Transcript"} {
			if strings.Contains(docXML, ">"+heading+"<") {
				t.Fatalf("docx: unexpected section %q; got:\n%s", heading, docXML)
			}
		}
	}
}

func TestEscapeMD(t *testing.T) {
	cases := []struct{ in, want string }{
		{"# x", `\# x`},
		{"- x", `\- x`},
		{"1. x", `1\. x`},
		{"a # b", "a # b"},
		{"first\n# second\n2. third", "first\n\\# second\n2\\. third"},
	}
	for _, c := range cases {
		if got := escapeMD(c.in); got != c.want {
			t.Fatalf("escapeMD(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDocx(t *testing.T) {
	d := fixtureDoc(t, En)
	d.Title = "<&>"
	d.Summary = "has \x01 control"
	raw, err := d.Docx()
	if err != nil {
		t.Fatalf("Docx: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	want := map[string]bool{
		"[Content_Types].xml":          false,
		"_rels/.rels":                  false,
		"word/_rels/document.xml.rels": false,
		"word/styles.xml":              false,
		"word/document.xml":            false,
	}
	var docXML string
	for _, f := range zr.File {
		seen, ok := want[f.Name]
		if !ok {
			t.Fatalf("unexpected zip part %q", f.Name)
		}
		if seen {
			t.Fatalf("duplicate zip part %q", f.Name)
		}
		want[f.Name] = true
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %q: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %q: %v", f.Name, err)
		}
		if f.Name == "word/document.xml" {
			docXML = string(body)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing zip part %q", name)
		}
	}

	dec := xml.NewDecoder(strings.NewReader(docXML))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("xml decode: %v\ndoc:\n%s", err, docXML)
		}
	}
	if !strings.Contains(docXML, "&lt;&amp;&gt;") {
		t.Fatalf("title not escaped; got:\n%s", docXML)
	}
	if strings.Contains(docXML, "\x01") {
		t.Fatalf("control char survived; got:\n%s", docXML)
	}
}

func TestFilename(t *testing.T) {
	m := &meeting.Meeting{Title: "Họp dự án Q3", StartedAt: time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)}
	utf8Name, asciiName := Filename(m, "md")
	if utf8Name != "họp-dự-án-q3-2026-09-17.md" {
		t.Fatalf("utf8Name = %q", utf8Name)
	}
	if asciiName != "hop-du-an-q3-2026-09-17.md" {
		t.Fatalf("asciiName = %q", asciiName)
	}
	bang := &meeting.Meeting{Title: "!!!", StartedAt: m.StartedAt}
	if u, a := Filename(bang, "md"); u != "meeting-2026-09-17.md" || a != "meeting-2026-09-17.md" {
		t.Fatalf("Filename(!) = %q, %q", u, a)
	}
}

func TestOffsetAndDurationStrings(t *testing.T) {
	offsets := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00"},
		{59 * time.Second, "00:59"},
		{59*time.Minute + 59*time.Second, "59:59"},
		{time.Hour, "1:00:00"},
		{-5 * time.Second, "00:00"},
	}
	for _, c := range offsets {
		if got := offsetString(c.d); got != c.want {
			t.Fatalf("offsetString(%v) = %q, want %q", c.d, got, c.want)
		}
	}
	durations := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{45 * time.Minute, "45m"},
		{90 * time.Minute, "1h 30m"},
	}
	for _, c := range durations {
		if got := durationString(c.d); got != c.want {
			t.Fatalf("durationString(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// Confirmed names beat the analysis's guesses; unnamed speakers read in
// the export's language.
func TestExportSpeakerNames(t *testing.T) {
	start := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	m := &meeting.Meeting{Title: "Sync", StartedAt: start, Speakers: map[string]string{"1": "An Nguyen"}}
	us := []meeting.Utterance{
		{At: start, Speaker: "1", Text: "a"},
		{At: start.Add(time.Second), Speaker: "2", Text: "b"},
		{At: start.Add(2 * time.Second), Speaker: "3", Text: "c"},
		{At: start.Add(3 * time.Second), Source: "mic", Text: "d"},
	}
	people := []summarize.Person{{Name: "Guess One", Speaker: "Speaker 1"}, {Name: "Bao", Speaker: "Speaker 2"}}
	var got []string
	for _, g := range Build(m, m.Namer("Minh"), people, us, Vi).Groups {
		got = append(got, g.Speaker)
	}
	if want := "An Nguyen,Bao,Người nói 3,Minh"; strings.Join(got, ",") != want {
		t.Errorf("speakers = %v, want %s", got, want)
	}
}
