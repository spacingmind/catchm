package meeting

import (
	"errors"
	"path/filepath"
	"sort"
)

// ErrNoAudio is returned when a meeting directory has no recorded
// audio file (audio.wav, or an imported audio.<ext>).
var ErrNoAudio = errors.New("meeting has no audio recording")

// AudioPath returns the path of meeting id's audio file: the first
// match of audio.* in the meeting directory (sorted, so audio.wav wins
// when several exist — the pipeline always records that name, and
// imports write audio.<ext>). ErrNoAudio when none exists.
func (s *Store) AudioPath(id string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(s.dir(id), "audio.*"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", ErrNoAudio
	}
	sort.Strings(matches)
	for _, m := range matches {
		if filepath.Ext(m) == ".wav" {
			return m, nil
		}
	}
	return matches[0], nil
}
