// Package tools defines the meeting tool surface: one typed implementation
// over the meeting store, consumed by the web UI handlers, in-process
// agent jobs (SDK-MCP), and an external MCP server later.
package tools

import (
	"strings"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

// Surface is the tool surface over a meeting store.
type Surface struct {
	store *meeting.Store
}

// New returns a Surface backed by store.
func New(store *meeting.Store) *Surface {
	return &Surface{store: store}
}

// ListMeetings returns all meetings, newest first.
func (s *Surface) ListMeetings() ([]*meeting.Meeting, error) {
	return s.store.List()
}

// GetMeeting returns one meeting's document.
func (s *Surface) GetMeeting(id string) (*meeting.Meeting, error) {
	return s.store.Get(id)
}

// SearchMeetings returns meetings whose title, summary, notes, decisions,
// or action items contain the query (case-insensitive substring; the FTS5
// index lands in a later milestone with the same signature).
func (s *Surface) SearchMeetings(query string) ([]*meeting.Meeting, error) {
	ms, err := s.store.List()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ms, nil
	}
	var out []*meeting.Meeting
	for _, m := range ms {
		if meetingMatches(m, q) {
			out = append(out, m)
		}
	}
	return out, nil
}

func meetingMatches(m *meeting.Meeting, q string) bool {
	if strings.Contains(strings.ToLower(m.Title), q) ||
		strings.Contains(strings.ToLower(m.Summary), q) {
		return true
	}
	for _, n := range m.Notes {
		if strings.Contains(strings.ToLower(n), q) {
			return true
		}
	}
	for _, d := range m.Decisions {
		if strings.Contains(strings.ToLower(d.Text), q) {
			return true
		}
	}
	for _, a := range m.ActionItems {
		if strings.Contains(strings.ToLower(a.Text), q) {
			return true
		}
	}
	return false
}

// ReadLiveTranscript returns new utterances for a meeting given a cursor.
// cursor < 0 means "from the beginning".
func (s *Surface) ReadLiveTranscript(id string, cursor int64) ([]meeting.Utterance, int64, error) {
	return s.store.ReadSince(id, cursor)
}

// AddNote appends a timestamped note to a meeting's document.
func (s *Surface) AddNote(id, note string) error {
	return s.store.UpdateDoc(id, func(m *meeting.Meeting) error {
		m.Notes = append(m.Notes, note)
		return nil
	})
}

// StartMeeting creates a new active meeting.
func (s *Surface) StartMeeting(title string) (*meeting.Meeting, error) {
	return s.store.Start(title, time.Now().UTC(), meeting.StartOptions{})
}

// StartMeetingWithOptions creates a new active meeting with STT engine/lang
// settings recorded in its frontmatter.
func (s *Surface) StartMeetingWithOptions(title string, opts meeting.StartOptions) (*meeting.Meeting, error) {
	return s.store.Start(title, time.Now().UTC(), opts)
}

// EndMeeting marks a meeting ended.
func (s *Surface) EndMeeting(id string) error {
	return s.store.End(id, time.Now().UTC())
}
