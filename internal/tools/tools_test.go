package tools

import (
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

func newSurface(t *testing.T) (*Surface, *meeting.Store) {
	t.Helper()
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store), store
}

func TestListGetSearch(t *testing.T) {
	s, store := newSurface(t)
	base := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)

	m1, err := store.Start("Q3 Roadmap Sync", base)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDoc(m1.ID, func(m *meeting.Meeting) error {
		m.Decisions = append(m.Decisions, meeting.Decision{Text: "Freeze scope by Friday"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start("Weekly Design Crit", base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	ms, err := s.ListMeetings()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Title != "Weekly Design Crit" {
		t.Fatalf("list = %+v", ms)
	}

	got, err := s.GetMeeting(m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Decisions) != 1 {
		t.Fatalf("decisions = %+v", got.Decisions)
	}

	hits, err := s.SearchMeetings("freeze")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != m1.ID {
		t.Fatalf("search hits = %+v", hits)
	}

	hits, err = s.SearchMeetings("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestReadLiveTranscriptAndAddNote(t *testing.T) {
	s, store := newSurface(t)
	m, err := store.Start("Standup", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(m.ID, meeting.Utterance{Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	us, cursor, err := s.ReadLiveTranscript(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 1 || us[0].Text != "hello" {
		t.Fatalf("utterances = %+v", us)
	}

	if err := s.AddNote(m.ID, "check the beta timeline"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMeeting(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "check the beta timeline" {
		t.Fatalf("notes = %+v", got.Notes)
	}

	// Note is searchable.
	hits, err := s.SearchMeetings("beta timeline")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	_ = cursor
}
