package meeting

import (
	"errors"
	"os"
	"path/filepath"
)

// AnalysisFile is the per-meeting analysis sidecar next to meeting.md:
// the summary's richer output (topics, evidence, canonical keys), owned
// by internal/summarize. The frontmatter is projected from it.
const AnalysisFile = "analysis.json"

// ErrNoAnalysis is returned by ReadAnalysis when the meeting exists but
// has no analysis.json yet (e.g. it was summarized before the sidecar
// existed, or never).
var ErrNoAnalysis = errors.New("meeting has no analysis")

// WriteAnalysis atomically replaces the meeting's analysis.json with
// data (a tmp file, then rename).
func (s *Store) WriteAnalysis(id string, data []byte) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	tmp := s.analysisPath(id) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.analysisPath(id))
}

// ReadAnalysis returns the meeting's analysis.json: ErrNotFound for an
// unknown meeting, ErrNoAnalysis when it has none.
func (s *Store) ReadAnalysis(id string) ([]byte, error) {
	data, err := os.ReadFile(s.analysisPath(id))
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	return nil, ErrNoAnalysis
}

func (s *Store) analysisPath(id string) string {
	return filepath.Join(s.dir(id), AnalysisFile)
}
