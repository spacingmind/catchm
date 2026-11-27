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
	Seq        int64     `json:"seq"`
	At         time.Time `json:"at"`
	Speaker    string    `json:"speaker,omitempty"`
	Text       string    `json:"text"`
	Translated string    `json:"translated,omitempty"`
	Lang       string    `json:"lang,omitempty"`
	// End is when the utterance stopped, for engines that report it
	// (Soniox async); nil when unknown. Same clock as At.
	End *time.Time `json:"end,omitempty"`
	// Source is the input that carried the utterance in a meeting that
	// recorded both: "mic" (the user) or "system" (the other side of the
	// call). Empty when unknown or unclear.
	Source string      `json:"source,omitempty"`
	Extra  interface{} `json:"extra,omitempty"`
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
	Engine        string            `yaml:"engine,omitempty" json:"engine,omitempty"`
	AudioSource   string            `yaml:"audio_source,omitempty" json:"audio_source,omitempty"`
	Lang          string            `yaml:"lang,omitempty" json:"lang,omitempty"`
	TranslateTo   string            `yaml:"translate_to,omitempty" json:"translate_to,omitempty"`
	TwoWay        bool              `yaml:"two_way,omitempty" json:"two_way,omitempty"`
	Tags          []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Decisions     []Decision        `yaml:"decisions,omitempty" json:"decisions,omitempty"`
	ActionItems   []ActionItem      `yaml:"action_items,omitempty" json:"action_items,omitempty"`
	Terms         map[string]string `yaml:"terms,omitempty" json:"terms,omitempty"`
	Summary       string            `yaml:"summary,omitempty" json:"summary,omitempty"`
	Notes         []string          `yaml:"notes,omitempty" json:"notes,omitempty"`
	Refined       bool              `yaml:"refined,omitempty" json:"refined,omitempty"`
	RefinedWith   string            `yaml:"refined_with,omitempty" json:"refined_with,omitempty"`
	Imported      bool              `yaml:"imported,omitempty" json:"imported,omitempty"`
}

// SchemaVersion is the current frontmatter schema version.
const SchemaVersion = 1

// ErrNotFound is returned when a meeting ID has no directory on disk.
var ErrNotFound = errors.New("meeting not found")

// ErrNotActive is returned when an operation requires an active meeting.
var ErrNotActive = errors.New("meeting not active")

// ErrActionItemIndex is returned when an index doesn't address an
// existing entry in a meeting's ActionItems (e.g. a stale client-side
// index after the underlying list changed).
var ErrActionItemIndex = errors.New("action item index out of range")

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

// dir resolves id to its meeting directory under root. ids ultimately
// come from external callers (HTTP path segments, MCP tool arguments),
// not just Store.Start's own generated newID() values, so an id
// containing a path separator or ".." must never be allowed to resolve
// outside root — e.g. via Store.Delete's os.RemoveAll. An invalid id
// resolves to a path guaranteed not to exist, so every caller's
// existing not-found handling covers it with no other code changes.
// invalidIDSentinel is where an unsafe id resolves to: a name real ids
// (hex-encoded newID() output) can never produce, so it behaves exactly
// like an ordinary unknown id (a plain, valid path that simply doesn't
// exist) rather than an OS-level error from an unrepresentable path.
const invalidIDSentinel = ".invalid-id"

func (s *Store) dir(id string) string {
	if !validID(id) {
		return filepath.Join(s.root, invalidIDSentinel)
	}
	return filepath.Join(s.root, id)
}

func validID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read failing is effectively untestable; fall back to time.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// StartOptions carries per-meeting STT settings recorded in frontmatter.
type StartOptions struct {
	Engine      string
	Lang        string
	TranslateTo string
	TwoWay      bool
	Tags        []string
}

// Start creates a new active meeting and writes its initial document.
func (s *Store) Start(title string, startedAt time.Time, opts StartOptions) (*Meeting, error) {
	m := &Meeting{
		SchemaVersion: SchemaVersion,
		ID:            newID(),
		Title:         title,
		StartedAt:     startedAt,
		Engine:        opts.Engine,
		Lang:          opts.Lang,
		TranslateTo:   opts.TranslateTo,
		TwoWay:        opts.TwoWay,
		Tags:          opts.Tags,
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

// Delete removes the meeting's whole directory. It returns ErrNotFound
// when no such meeting exists, so callers can distinguish "deleted"
// from "already gone" (RemoveAll alone would silently no-op).
func (s *Store) Delete(id string) error {
	if _, err := os.Stat(s.dir(id)); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return os.RemoveAll(s.dir(id))
}

// SetActionItemStatus updates one action item's status in place. An
// out-of-range index returns ErrActionItemIndex and leaves the meeting
// document untouched.
func (s *Store) SetActionItemStatus(meetingID string, index int, status string) error {
	return s.UpdateDoc(meetingID, func(m *Meeting) error {
		if index < 0 || index >= len(m.ActionItems) {
			return fmt.Errorf("%w: %d (meeting %q has %d action items)",
				ErrActionItemIndex, index, meetingID, len(m.ActionItems))
		}
		m.ActionItems[index].Status = status
		return nil
	})
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
