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

func TestEnrichFinalPrompt(t *testing.T) {
	p := enrichFinalPrompt("abc123")
	for _, want := range []string{`"abc123"`, "get_meeting", "read_batch_transcript", `"decisions"`, `"action_items"`} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestReadBatchTranscriptTool(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	if err := s.WriteBatchTranscript(m.ID, []meeting.Utterance{{Text: "refined line"}}); err != nil {
		t.Fatal(err)
	}
	surface := tools.New(s)

	read := handleReadBatchTranscript(surface)
	res, err := read(context.Background(), map[string]any{"id": m.ID})
	if err != nil {
		t.Fatal(err)
	}
	var tr struct {
		Utterances []meeting.Utterance `json:"utterances"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &tr); err != nil {
		t.Fatal(err)
	}
	if len(tr.Utterances) != 1 || tr.Utterances[0].Text != "refined line" {
		t.Fatalf("utterances = %+v", tr.Utterances)
	}
}

// TestEnrichMeetingFinalSetsRefined runs the real `claude` CLI over a
// fixture meeting's batch transcript; skipped when the CLI is not on PATH.
func TestEnrichMeetingFinalSetsRefined(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not on PATH")
	}
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	if err := s.End(m.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteBatchTranscript(m.ID, []meeting.Utterance{
		{Speaker: "alex", Text: "We freeze the scope by Friday."},
		{Speaker: "sam", Text: "I'll prepare the demo and send the timeline."},
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := EnrichMeetingFinal(ctx, s, m.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Refined {
		t.Fatalf("Refined not set: %+v", got)
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
	t.Logf("enriched final: %+v", got)
}

func TestEnrichMeetingFinalUnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := EnrichMeetingFinal(context.Background(), s, "nope"); err != meeting.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
