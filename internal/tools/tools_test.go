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

	m1, err := store.Start("Q3 Roadmap Sync", base, meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDoc(m1.ID, func(m *meeting.Meeting) error {
		m.Decisions = append(m.Decisions, meeting.Decision{Text: "Freeze scope by Friday"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start("Weekly Design Crit", base.Add(time.Hour), meeting.StartOptions{}); err != nil {
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
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{})
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

func TestReadBatchTranscript(t *testing.T) {
	s, store := newSurface(t)
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteBatchTranscript(m.ID, []meeting.Utterance{{Text: "refined line"}}); err != nil {
		t.Fatal(err)
	}
	us, err := s.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 1 || us[0].Text != "refined line" {
		t.Fatalf("utterances = %+v", us)
	}
}

func TestListActionItemsAcrossMeetings(t *testing.T) {
	s, store := newSurface(t)
	base := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)

	m1, err := store.Start("Q3 Roadmap Sync", base, meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDoc(m1.ID, func(m *meeting.Meeting) error {
		m.ActionItems = append(m.ActionItems,
			meeting.ActionItem{Text: "Freeze scope", Owner: "alex"},
			meeting.ActionItem{Text: "Send recap", Owner: "sam", Status: "done"},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m2, err := store.Start("Weekly Design Crit", base.Add(time.Hour), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDoc(m2.ID, func(m *meeting.Meeting) error {
		m.ActionItems = append(m.ActionItems, meeting.ActionItem{Text: "Redraw dashboard"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A meeting with no action items contributes nothing.
	if _, err := store.Start("Silent sync", base.Add(2*time.Hour), meeting.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	items, err := s.ListActionItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(items), items)
	}
	// Store.List is newest-first, so m2's item leads.
	want := []struct {
		meetingID    string
		meetingTitle string
		index        int
		text         string
	}{
		{m2.ID, "Weekly Design Crit", 0, "Redraw dashboard"},
		{m1.ID, "Q3 Roadmap Sync", 0, "Freeze scope"},
		{m1.ID, "Q3 Roadmap Sync", 1, "Send recap"},
	}
	for i, w := range want {
		got := items[i]
		if got.MeetingID != w.meetingID || got.MeetingTitle != w.meetingTitle ||
			got.Index != w.index || got.Text != w.text {
			t.Fatalf("items[%d] = %+v, want %+v", i, got, w)
		}
	}
	if items[1].Owner != "alex" || items[2].Status != "done" {
		t.Fatalf("action item fields lost: %+v", items)
	}
}
