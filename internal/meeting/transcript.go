package meeting

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	transcriptFile      = "utterances.jsonl"
	batchTranscriptFile = "utterances.v2.jsonl"
	docFile             = "meeting.md"
)

// Append adds an utterance to the meeting's live transcript, assigning it
// the next sequence number.
func (s *Store) Append(id string, u Utterance) (Utterance, error) {
	if active, err := s.Active(id); err != nil {
		return u, err
	} else if !active {
		return u, ErrNotActive
	}
	seq, err := s.nextSeq(id)
	if err != nil {
		return u, err
	}
	u.Seq = seq
	if u.At.IsZero() {
		u.At = time.Now().UTC()
	}
	f, err := os.OpenFile(s.transcriptPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return u, err
	}
	defer f.Close()
	line, err := json.Marshal(u)
	if err != nil {
		return u, err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return u, err
	}
	return u, nil
}

// ReadSince returns utterances with sequence numbers strictly greater
// than cursor, plus the new cursor (the highest seq returned, or the old
// cursor when nothing is new). cursor < 0 means "from the beginning".
func (s *Store) ReadSince(id string, cursor int64) ([]Utterance, int64, error) {
	if _, err := os.Stat(s.transcriptPath(id)); os.IsNotExist(err) {
		if _, err := s.Get(id); err != nil {
			return nil, cursor, err
		}
		return nil, cursor, nil // active meeting, no utterances yet
	}
	f, err := os.Open(s.transcriptPath(id))
	if err != nil {
		return nil, cursor, err
	}
	defer f.Close()
	var out []Utterance
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := trimSpace(sc.Text())
		if line == "" {
			continue
		}
		var u Utterance
		if err := json.Unmarshal([]byte(line), &u); err != nil {
			return nil, cursor, fmt.Errorf("bad transcript line: %w", err)
		}
		if u.Seq > cursor {
			out = append(out, u)
			cursor = u.Seq
		}
	}
	if err := sc.Err(); err != nil {
		return nil, cursor, err
	}
	return out, cursor, nil
}

// WriteBatchTranscript overwrites the meeting's batch transcript
// (utterances.v2.jsonl) with us in full, assigning sequence numbers by
// position. Unlike Append, this is a full overwrite each call and never
// touches the append-only utterances.jsonl.
func (s *Store) WriteBatchTranscript(id string, us []Utterance) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	var b strings.Builder
	for i, u := range us {
		u.Seq = int64(i + 1)
		line, err := json.Marshal(u)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp := s.batchTranscriptPath(id) + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.batchTranscriptPath(id))
}

// ReadBatchTranscript reads the meeting's full batch transcript. It
// returns a nil slice, not an error, when no batch transcript has been
// written yet.
func (s *Store) ReadBatchTranscript(id string) ([]Utterance, error) {
	if _, err := os.Stat(s.batchTranscriptPath(id)); os.IsNotExist(err) {
		if _, err := s.Get(id); err != nil {
			return nil, err
		}
		return nil, nil
	}
	f, err := os.Open(s.batchTranscriptPath(id))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Utterance
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := trimSpace(sc.Text())
		if line == "" {
			continue
		}
		var u Utterance
		if err := json.Unmarshal([]byte(line), &u); err != nil {
			return nil, fmt.Errorf("bad batch transcript line: %w", err)
		}
		out = append(out, u)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) batchTranscriptPath(id string) string {
	return filepath.Join(s.dir(id), batchTranscriptFile)
}

func (s *Store) nextSeq(id string) (int64, error) {
	_, cursor, err := s.ReadSince(id, 0) // seqs are 1-based
	if err != nil {
		return 0, err
	}
	return cursor + 1, nil
}

func (s *Store) transcriptPath(id string) string {
	return filepath.Join(s.dir(id), transcriptFile)
}

func (s *Store) docPath(id string) string {
	return filepath.Join(s.dir(id), docFile)
}

// UpdateDoc loads the meeting document, applies fn, and writes it back.
// This is the seam enrichment jobs use to edit frontmatter.
func (s *Store) UpdateDoc(id string, fn func(*Meeting) error) error {
	m, err := s.Get(id)
	if err != nil {
		return err
	}
	if err := fn(m); err != nil {
		return err
	}
	m.ID = id // id is the directory; never let fn rename it
	return s.WriteDoc(m)
}
