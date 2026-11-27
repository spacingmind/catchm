// Package tools defines the meeting tool surface: one typed implementation
// over the meeting store, consumed by the web UI handlers, in-process
// agent jobs (SDK-MCP), and an external MCP server later.
package tools

import (
	"strings"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

// ActionItemRef is an action item lifted out of its meeting for
// cross-meeting views: the item itself plus where it lives (meeting id,
// title) and its index inside that meeting's ActionItems slice, which is
// the address for status updates. The embedded meeting.ActionItem keeps
// text/owner/due/status at the top level of its JSON encoding.
type ActionItemRef struct {
	meeting.ActionItem
	MeetingID    string `json:"meeting_id"`
	MeetingTitle string `json:"meeting_title"`
	// MeetingProject is the meeting's project display name ("" = none),
	// so cross-meeting views can filter by project.
	MeetingProject string `json:"meeting_project,omitempty"`
	Index          int    `json:"index"`
}

// Surface is the tool surface over a meeting store.
type Surface struct {
	store *meeting.Store
}

// SetSelf tells the store where the user's profile comes from, for
// naming the user's speaker.
func (s *Surface) SetSelf(f func() meeting.Self) { s.store.SetSelf(f) }

// SetMeSpeaker records which speaker is the user ("" forgets it).
func (s *Surface) SetMeSpeaker(id, key string) error { return s.store.SetMeSpeaker(id, key) }

// Namer names a meeting's speakers: confirmed names, else the user's
// own name for "me", else generic labels.
func (s *Surface) Namer(m *meeting.Meeting) meeting.SpeakerNamer { return s.store.Namer(m) }

// SetSpeakerNames merges confirmed speaker names (a blank one removes it).
func (s *Surface) SetSpeakerNames(id string, names map[string]string) error {
	return s.store.SetSpeakerNames(id, names)
}

// SetGuests replaces a meeting's guest list.
func (s *Surface) SetGuests(id string, guests []meeting.Guest) error {
	return s.store.SetGuests(id, guests)
}

// New returns a Surface backed by store.
func New(store *meeting.Store) *Surface {
	return &Surface{store: store}
}

// Store returns the underlying meeting store, for callers (like the
// pipeline wiring in internal/server) that need direct store access
// beyond the tool surface.
func (s *Surface) Store() *meeting.Store { return s.store }

// ListMeetings returns all meetings, newest first.
func (s *Surface) ListMeetings() ([]*meeting.Meeting, error) {
	return s.store.List()
}

// GetMeeting returns one meeting's document.
func (s *Surface) GetMeeting(id string) (*meeting.Meeting, error) {
	return s.store.Get(id)
}

// ListActionItems flat-maps every meeting's action items into a single
// cross-meeting list, newest meeting first, in-document order within a
// meeting.
func (s *Surface) ListActionItems() ([]ActionItemRef, error) {
	ms, err := s.store.List()
	if err != nil {
		return nil, err
	}
	var out []ActionItemRef
	for _, m := range ms {
		project := meeting.ProjectOf(m.Tags)
		for i, a := range m.ActionItems {
			out = append(out, ActionItemRef{
				ActionItem:     a,
				MeetingID:      m.ID,
				MeetingTitle:   m.Title,
				MeetingProject: project,
				Index:          i,
			})
		}
	}
	return out, nil
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

// ReadBatchTranscript returns a meeting's full batch (refined) transcript.
func (s *Surface) ReadBatchTranscript(id string) ([]meeting.Utterance, error) {
	return s.store.ReadBatchTranscript(id)
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

// DeleteMeeting removes a meeting and its whole directory.
func (s *Surface) DeleteMeeting(id string) error {
	return s.store.Delete(id)
}

// RenameMeeting updates a meeting's title via UpdateDoc.
func (s *Surface) RenameMeeting(id, title string) error {
	return s.store.UpdateDoc(id, func(m *meeting.Meeting) error {
		m.Title = title
		return nil
	})
}

// SetMeetingTags replaces a meeting's tags via UpdateDoc.
func (s *Surface) SetMeetingTags(id string, tags []string) error {
	return s.store.UpdateDoc(id, func(m *meeting.Meeting) error {
		m.Tags = tags
		return nil
	})
}
