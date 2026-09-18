package meeting

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStartEndMeeting(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	m, err := s.Start("Q3 Roadmap Sync", start, StartOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Acceptance: meeting dir contains meeting.md (+ transcript later).
	if _, err := os.Stat(filepath.Join(s.Root(), m.ID, "meeting.md")); err != nil {
		t.Fatalf("meeting.md not written: %v", err)
	}

	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Q3 Roadmap Sync" || got.ID != m.ID {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", got.SchemaVersion, SchemaVersion)
	}

	active, err := s.Active(m.ID)
	if err != nil || !active {
		t.Fatalf("meeting should be active: %v %v", active, err)
	}

	end := start.Add(48 * time.Minute)
	if err := s.End(m.ID, end); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(end) {
		t.Fatalf("ended_at = %v, want %v", got.EndedAt, end)
	}

	// End is idempotent.
	if err := s.End(m.ID, end.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(m.ID)
	if !got.EndedAt.Equal(end) {
		t.Fatalf("end was not idempotent: %v", got.EndedAt)
	}
}

func TestAppendAndReadSinceCursor(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Standup", time.Now().UTC(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"first", "second", "third"}
	for i, text := range want {
		u, err := s.Append(m.ID, Utterance{Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if u.Seq != int64(i+1) {
			t.Fatalf("seq = %d, want %d", u.Seq, i+1)
		}
	}

	// Read all.
	all, cursor, err := s.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || cursor != 3 {
		t.Fatalf("all: len=%d cursor=%d", len(all), cursor)
	}

	// Cursor-based read returns only new utterances.
	u4, err := s.Append(m.ID, Utterance{Text: "fourth"})
	if err != nil {
		t.Fatal(err)
	}
	delta, cursor, err := s.ReadSince(m.ID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) != 1 || delta[0].Seq != u4.Seq || delta[0].Text != "fourth" {
		t.Fatalf("delta = %+v", delta)
	}
	if cursor != 4 {
		t.Fatalf("cursor = %d, want 4", cursor)
	}

	// No new data: same cursor, empty delta.
	delta, cursor2, err := s.ReadSince(m.ID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) != 0 || cursor2 != cursor {
		t.Fatalf("expected empty delta, got %+v cursor=%d", delta, cursor2)
	}

	// Ended meetings reject appends.
	if err := s.End(m.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(m.ID, Utterance{Text: "late"}); err != ErrNotActive {
		t.Fatalf("append after end: err = %v, want ErrNotActive", err)
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	ids := make([]string, 3)
	for i, title := range []string{"old", "newer", "newest"} {
		m, err := s.Start(title, base.Add(time.Duration(i)*time.Hour), StartOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = m.ID
	}
	ms, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 {
		t.Fatalf("len = %d, want 3", len(ms))
	}
	if ms[0].Title != "newest" || ms[2].Title != "old" {
		t.Fatalf("order = %s, %s, %s", ms[0].Title, ms[1].Title, ms[2].Title)
	}
}

// TestEngineSelectionRecorded verifies the acceptance criterion that a
// meeting started with a given engine/lang records it in frontmatter, both
// via the parsed Meeting and in the raw meeting.md on disk.
func TestEngineSelectionRecorded(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Standup", time.Now().UTC(), StartOptions{
		Engine:      "soniox",
		Lang:        "en",
		TranslateTo: "vi",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Engine != "soniox" || got.Lang != "en" || got.TranslateTo != "vi" {
		t.Fatalf("engine/lang not recorded: %+v", got)
	}

	raw, err := os.ReadFile(filepath.Join(s.Root(), m.ID, "meeting.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"engine: soniox", "lang: en", "translate_to: vi"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("meeting.md missing %q:\n%s", want, raw)
		}
	}
}

func TestWriteReadBatchTranscript(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Standup", time.Now().UTC(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(m.ID, Utterance{Text: "live one"}); err != nil {
		t.Fatal(err)
	}

	want := []Utterance{
		{Text: "hello", At: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)},
		{Text: "world", At: time.Date(2026, 9, 17, 10, 0, 5, 0, time.UTC)},
	}
	if err := s.WriteBatchTranscript(m.ID, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "hello" || got[1].Text != "world" {
		t.Fatalf("batch transcript = %+v", got)
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("seqs = %d, %d", got[0].Seq, got[1].Seq)
	}

	// The live transcript is untouched by a batch write.
	live, _, err := s.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].Text != "live one" {
		t.Fatalf("live transcript mutated: %+v", live)
	}

	// A second write fully overwrites the first.
	if err := s.WriteBatchTranscript(m.ID, []Utterance{{Text: "only this now"}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "only this now" {
		t.Fatalf("overwrite failed: %+v", got)
	}
}

func TestReadBatchTranscriptEmptyBeforeWrite(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Standup", time.Now().UTC(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("batch transcript = %+v, want empty", got)
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Get("nope"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateDocPreservesID(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Planning", time.Now().UTC(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateDoc(m.ID, func(mm *Meeting) error {
		mm.ID = "hijack"
		mm.Decisions = append(mm.Decisions, Decision{Text: "Ship on Friday"})
		mm.ActionItems = append(mm.ActionItems, ActionItem{Text: "Prepare demo", Owner: "alex"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != m.ID {
		t.Fatalf("id changed to %q", got.ID)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].Text != "Ship on Friday" {
		t.Fatalf("decisions = %+v", got.Decisions)
	}
	if len(got.ActionItems) != 1 || got.ActionItems[0].Owner != "alex" {
		t.Fatalf("action items = %+v", got.ActionItems)
	}
}

func TestDeleteMeeting(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Planning", time.Now().UTC(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(m.ID, Utterance{Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), m.ID)); !os.IsNotExist(err) {
		t.Fatalf("meeting dir still exists: %v", err)
	}
	if _, err := s.Get(m.ID); err != ErrNotFound {
		t.Fatalf("get after delete: err = %v, want ErrNotFound", err)
	}
	ms, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("list after delete = %+v", ms)
	}

	// A second delete, and an unknown id, both return ErrNotFound.
	if err := s.Delete(m.ID); err != ErrNotFound {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}
	if err := s.Delete("nope"); err != ErrNotFound {
		t.Fatalf("unknown id: err = %v, want ErrNotFound", err)
	}
}

// TestStoreRejectsPathTraversalIDs proves an id containing a path
// separator or ".." can never resolve outside the store root: it must
// behave exactly like an unknown id (ErrNotFound), never touching
// anything above root. A sibling directory outside root stands in for
// something an attacker-controlled id might otherwise reach.
func TestStoreRejectsPathTraversalIDs(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "meetings")
	s, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	canary := filepath.Join(parent, "canary")
	if err := os.MkdirAll(canary, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canary, "file.txt"), []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"../canary", "..", "../../canary", "sub/dir", `sub\dir`} {
		if err := s.Delete(id); err != ErrNotFound {
			t.Errorf("Delete(%q) = %v, want ErrNotFound", id, err)
		}
		if _, err := s.Get(id); err != ErrNotFound {
			t.Errorf("Get(%q) = %v, want ErrNotFound", id, err)
		}
	}

	if _, err := os.Stat(filepath.Join(canary, "file.txt")); err != nil {
		t.Fatalf("canary file outside root was affected: %v", err)
	}
}

func TestSetActionItemStatusBoundsChecked(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Planning", time.Now().UTC(), StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDoc(m.ID, func(mm *Meeting) error {
		mm.ActionItems = append(mm.ActionItems,
			ActionItem{Text: "Prepare demo", Owner: "alex"},
			ActionItem{Text: "Send notes", Owner: "sam", Status: "done"},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A valid index updates the status and persists.
	if err := s.SetActionItemStatus(m.ID, 0, "in progress"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActionItems[0].Status != "in progress" {
		t.Fatalf("status = %q, want %q", got.ActionItems[0].Status, "in progress")
	}
	if got.ActionItems[1].Status != "done" {
		t.Fatalf("sibling status changed: %+v", got.ActionItems)
	}

	// Out-of-range indices error (no panic) and leave the doc untouched.
	before, err := os.ReadFile(filepath.Join(s.Root(), m.ID, "meeting.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{2, -1} {
		err := s.SetActionItemStatus(m.ID, index, "done")
		if !errors.Is(err, ErrActionItemIndex) {
			t.Fatalf("index %d: err = %v, want ErrActionItemIndex", index, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(s.Root(), m.ID, "meeting.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("meeting.md changed after a failed SetActionItemStatus call")
	}
	got, err = s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ActionItems) != 2 || got.ActionItems[0].Status != "in progress" {
		t.Fatalf("action items corrupted: %+v", got.ActionItems)
	}

	// Unknown meeting is still ErrNotFound.
	if err := s.SetActionItemStatus("nope", 0, "done"); err != ErrNotFound {
		t.Fatalf("unknown meeting: err = %v, want ErrNotFound", err)
	}
}

// TestTwoWayMeetingOptionRecorded verifies the two-way representation: a
// meeting started with TwoWay records lang/translate_to as the two
// conversation languages (language A/B) plus two_way: true, and all three
// round-trip through Get both parsed and in the raw frontmatter.
func TestTwoWayMeetingOptionRecorded(t *testing.T) {
	s := newTestStore(t)
	m, err := s.Start("Bilingual 1:1", time.Now().UTC(), StartOptions{
		Lang:        "en",
		TranslateTo: "vi",
		TwoWay:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TwoWay || got.Lang != "en" || got.TranslateTo != "vi" {
		t.Fatalf("two-way options not recorded: %+v", got)
	}

	raw, err := os.ReadFile(filepath.Join(s.Root(), m.ID, "meeting.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lang: en", "translate_to: vi", "two_way: true"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("meeting.md missing %q:\n%s", want, raw)
		}
	}
}
