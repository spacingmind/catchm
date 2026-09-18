// Package meeting implements the per-meeting context store: each meeting
// is a directory containing an append-only JSONL live transcript and a
// markdown document with YAML frontmatter. Files are the source of truth.
package meeting

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Utterance is one segment of transcribed (and optionally translated)
// speech, appended to the live transcript as it is produced.
type Utterance struct {
	Seq        int64       `json:"seq"`
	At         time.Time   `json:"at"`
	Speaker    string      `json:"speaker,omitempty"`
	Text       string      `json:"text"`
	Translated string      `json:"translated,omitempty"`
	Lang       string      `json:"lang,omitempty"`
	Extra      interface{} `json:"extra,omitempty"`
}

// ActionItem is a task extracted from a meeting.
type ActionItem struct {
	Text   string `yaml:"text" json:"text"`
	Owner  string `yaml:"owner,omitempty" json:"owner,omitempty"`
	Due    string `yaml:"due,omitempty" json:"due,omitempty"`
	Status string `yaml:"status,omitempty" json:"status,omitempty"`
}

// Decision is a settled conclusion recorded in a meeting.
type Decision struct {
	Text  string `yaml:"text" json:"text"`
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
}

// Meeting is the frontmatter-level view of one meeting: the structured
// query surface stored in meeting.md.
type Meeting struct {
	SchemaVersion int               `yaml:"schema_version" json:"schema_version"`
	ID            string            `yaml:"id" json:"id"`
	Title         string            `yaml:"title" json:"title"`
	StartedAt     time.Time         `yaml:"started_at" json:"started_at"`
	EndedAt       *time.Time        `yaml:"ended_at,omitempty" json:"ended_at,omitempty"`
	Participants  []string          `yaml:"participants,omitempty" json:"participants,omitempty"`
	Langs         []string          `yaml:"langs,omitempty" json:"langs,omitempty"`
	Decisions     []Decision        `yaml:"decisions,omitempty" json:"decisions,omitempty"`
	ActionItems   []ActionItem      `yaml:"action_items,omitempty" json:"action_items,omitempty"`
	Terms         map[string]string `yaml:"terms,omitempty" json:"terms,omitempty"`
	Summary       string            `yaml:"summary,omitempty" json:"summary,omitempty"`
	Notes         []string          `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// SchemaVersion is the current frontmatter schema version.
const SchemaVersion = 1

// ErrNotFound is returned when a meeting ID has no directory on disk.
var ErrNotFound = errors.New("meeting not found")

// ErrNotActive is returned when an operation requires an active meeting.
var ErrNotActive = errors.New("meeting not active")

// Store manages meeting directories under a root dir (e.g. ~/meetings
// or a test temp dir).
type Store struct {
	root string
}

// NewStore returns a Store rooted at dir, creating it if needed.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create meetings dir: %w", err)
	}
	return &Store{root: dir}, nil
}

// Root returns the store's root directory.
func (s *Store) Root() string { return s.root }

func (s *Store) dir(id string) string {
	return filepath.Join(s.root, id)
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read failing is effectively untestable; fall back to time.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Start creates a new active meeting and writes its initial document.
func (s *Store) Start(title string, startedAt time.Time) (*Meeting, error) {
	m := &Meeting{
		SchemaVersion: SchemaVersion,
		ID:            newID(),
		Title:         title,
		StartedAt:     startedAt,
	}
	if err := os.MkdirAll(s.dir(m.ID), 0o700); err != nil {
		return nil, err
	}
	if err := s.WriteDoc(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Get loads a meeting's document from disk.
func (s *Store) Get(id string) (*Meeting, error) {
	raw, err := os.ReadFile(s.docPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m, err := parseDoc(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse meeting %s: %w", id, err)
	}
	return m, nil
}

// List returns all meetings, newest first.
func (s *Store) List() ([]*Meeting, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var ms []*Meeting
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.Get(e.Name())
		if err != nil {
			continue // skip unreadable dirs rather than fail the listing
		}
		ms = append(ms, m)
	}
	sortMeetings(ms)
	return ms, nil
}

// Active reports whether the meeting has not ended yet.
func (s *Store) Active(id string) (bool, error) {
	m, err := s.Get(id)
	if err != nil {
		return false, err
	}
	return m.EndedAt == nil, nil
}

// End marks the meeting ended and rewrites its document.
func (s *Store) End(id string, endedAt time.Time) error {
	m, err := s.Get(id)
	if err != nil {
		return err
	}
	if m.EndedAt != nil {
		return nil // idempotent
	}
	m.EndedAt = &endedAt
	return s.WriteDoc(m)
}

func sortMeetings(ms []*Meeting) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && ms[j].StartedAt.After(ms[j-1].StartedAt); j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

func trimSpace(s string) string { return strings.TrimFunc(s, isSpace) }
