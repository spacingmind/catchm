// Package eval scores a summarize.Analysis against a hand-written gold
// file, so providers, models and prompts can be compared on the same
// meetings before any default changes (meeting-insights plan, decision
// 10). The committed gold set in testdata/ is synthetic; real
// transcripts belong in the gitignored eval-local/ directory.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spacingmind/catchm/internal/meeting"
)

// Gold is one meeting's transcript and the decisions and actions a good
// summary must find in it.
type Gold struct {
	Title      string          `json:"title"`
	Lang       string          `json:"lang"`
	Transcript []GoldUtterance `json:"transcript"`
	Decisions  []string        `json:"decisions"`
	Actions    []GoldAction    `json:"actions"`
}

// GoldUtterance is one transcript line.
type GoldUtterance struct {
	Seq     int64  `json:"seq"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

// GoldAction is an expected action item. Owner is a person's name as
// spoken; Due is YYYY-MM-DD. Either is empty when the meeting doesn't
// state it, and then it isn't scored.
type GoldAction struct {
	Text  string `json:"text"`
	Owner string `json:"owner,omitempty"`
	Due   string `json:"due,omitempty"`
}

// NamedGold is a Gold with the file name it was loaded from.
type NamedGold struct {
	Name string
	Gold Gold
}

// Load reads every *.json file in dir as a Gold, sorted by file name.
// Unknown fields, an empty transcript and duplicate seqs are errors, so
// a typo in a gold file can't silently weaken the score.
func Load(dir string) ([]NamedGold, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no gold files (*.json) in %s", dir)
	}
	out := make([]NamedGold, 0, len(paths))
	for _, p := range paths {
		g, err := loadFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		out = append(out, NamedGold{Name: strings.TrimSuffix(filepath.Base(p), ".json"), Gold: g})
	}
	return out, nil
}

func loadFile(path string) (Gold, error) {
	f, err := os.Open(path)
	if err != nil {
		return Gold{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var g Gold
	if err := dec.Decode(&g); err != nil {
		return Gold{}, err
	}
	if len(g.Transcript) == 0 {
		return Gold{}, fmt.Errorf("empty transcript")
	}
	seen := map[int64]bool{}
	for _, u := range g.Transcript {
		if seen[u.Seq] {
			return Gold{}, fmt.Errorf("duplicate seq %d", u.Seq)
		}
		seen[u.Seq] = true
	}
	for _, a := range g.Actions {
		if a.Due != "" && !isoDate(a.Due) {
			return Gold{}, fmt.Errorf("action %q: due %q is not YYYY-MM-DD", a.Text, a.Due)
		}
	}
	return g, nil
}

// Seqs is the set of transcript seqs, as summarize.Validate wants it.
func (g Gold) Seqs() map[int64]bool {
	m := make(map[int64]bool, len(g.Transcript))
	for _, u := range g.Transcript {
		m[u.Seq] = true
	}
	return m
}

// Utterances converts the transcript to the store's utterance type.
func (g Gold) Utterances() []meeting.Utterance {
	us := make([]meeting.Utterance, len(g.Transcript))
	for i, u := range g.Transcript {
		us[i] = meeting.Utterance{Seq: u.Seq, Speaker: u.Speaker, Text: u.Text, Lang: g.Lang}
	}
	return us
}

func isoDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, r := range s {
		if i != 4 && i != 7 && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
