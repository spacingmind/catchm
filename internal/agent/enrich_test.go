package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

func newTestStore(t *testing.T) *meeting.Store {
	t.Helper()
	s, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fixtureMeeting(t *testing.T, s *meeting.Store) *meeting.Meeting {
	t.Helper()
	m, err := s.Start("Q3 Roadmap Sync", time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []meeting.Utterance{
		{Speaker: "alex", Text: "We freeze the scope by Friday."},
		{Speaker: "sam", Text: "I'll prepare the demo and send the timeline."},
	} {
		if _, err := s.Append(m.ID, u); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func TestEnrichPrompt(t *testing.T) {
	p := enrichPrompt("abc123")
	for _, want := range []string{`"abc123"`, "get_meeting", "read_live_transcript", "cursor -1", `"decisions"`, `"action_items"`} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestParseExtraction(t *testing.T) {
	ex, err := parseExtraction(`Here is the JSON: {"summary":"s","participants":["alex"],"decisions":[{"text":"freeze scope"}],"action_items":[{"text":"demo","owner":"sam"}]} done.`)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Summary != "s" || len(ex.Participants) != 1 || ex.Decisions[0].Text != "freeze scope" || ex.ActionItems[0].Owner != "sam" {
		t.Fatalf("extraction = %+v", ex)
	}

	// Fenced output parses too.
	if _, err := parseExtraction("```json\n{\"summary\":\"s\"}\n```"); err != nil {
		t.Fatal(err)
	}

	if _, err := parseExtraction("no json here"); err == nil {
		t.Fatal("expected error for non-JSON result")
	}
}

func TestApplyExtraction(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	ex := extraction{
		Summary:      "synced on Q3",
		Participants: []string{"alex", "sam"},
		Decisions:    []meeting.Decision{{Text: "Freeze scope by Friday"}},
		ActionItems:  []meeting.ActionItem{{Text: "Prepare demo", Owner: "sam"}},
	}
	if err := s.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		applyExtraction(mm, ex)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "synced on Q3" ||
		len(got.Participants) != 2 ||
		len(got.Decisions) != 1 || got.Decisions[0].Text != "Freeze scope by Friday" ||
		len(got.ActionItems) != 1 || got.ActionItems[0].Owner != "sam" {
		t.Fatalf("meeting = %+v", got)
	}
}

func TestMcpTools(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	surface := tools.New(s)

	get := handleGetMeeting(surface)
	res, err := get(context.Background(), map[string]any{"id": m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get_meeting errored: %+v", res)
	}
	var doc meeting.Meeting
	if err := json.Unmarshal([]byte(res.Content[0].Text), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Q3 Roadmap Sync" {
		t.Fatalf("title = %q", doc.Title)
	}

	if _, err := get(context.Background(), map[string]any{"id": "nope"}); err == nil {
		t.Fatal("expected error for unknown meeting")
	}

	read := handleReadLiveTranscript(surface)
	res, err = read(context.Background(), map[string]any{"id": m.ID, "cursor": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	var tr struct {
		Utterances []meeting.Utterance `json:"utterances"`
		Cursor     int64               `json:"cursor"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &tr); err != nil {
		t.Fatal(err)
	}
	if len(tr.Utterances) != 1 || tr.Utterances[0].Seq != 2 || tr.Cursor != 2 {
		t.Fatalf("delta = %+v cursor = %d", tr.Utterances, tr.Cursor)
	}

	// String-typed cursor is tolerated (models occasionally quote numbers).
	res, err = read(context.Background(), map[string]any{"id": m.ID, "cursor": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content[0].Text, `"cursor":2`) {
		t.Fatalf("string cursor result = %s", res.Content[0].Text)
	}
}

// TestEnrichMeetingIntegration runs the real `claude` CLI over a fixture
// meeting; skipped when the CLI is not on PATH.
func TestEnrichMeetingIntegration(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not on PATH")
	}
	s := newTestStore(t)
	m := fixtureMeeting(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := EnrichMeeting(ctx, s, m.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Decisions) == 0 {
		t.Fatalf("no decisions extracted: %+v", got)
	}
	if len(got.ActionItems) == 0 {
		t.Fatalf("no action items extracted: %+v", got)
	}
	if got.Summary == "" || len(got.Participants) == 0 {
		t.Fatalf("summary/participants not extracted: %+v", got)
	}
	t.Logf("enriched: %+v", got)
}

func TestEnrichMeetingUnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := EnrichMeeting(context.Background(), s, "nope"); err != meeting.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
