package tools

import (
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/spacingmind/catchm/internal/meeting"
)

// MaxProjectNameLen caps a project name, in characters.
const MaxProjectNameLen = 80

// ErrProjectName is returned when a project name is empty or too long.
var ErrProjectName = errors.New("project name must be 1 to 80 characters")

// Project is one project derived from the meetings' project tags. There
// is no registry: a project exists while at least one meeting has it.
type Project struct {
	Name          string    `json:"name"`
	Meetings      int       `json:"meetings"`
	LastMeetingAt time.Time `json:"last_meeting_at"`
}

// ValidProjectName normalizes name and checks its length.
func ValidProjectName(name string) (string, error) {
	n := meeting.NormalizeProject(name)
	if n == "" || utf8.RuneCountInString(n) > MaxProjectNameLen {
		return "", ErrProjectName
	}
	return n, nil
}

// ListProjects groups meetings by project (case-insensitively), most
// recent meeting first. A project written with different case shows
// its most-used spelling; a tie goes to the newest meeting's spelling.
func (s *Surface) ListProjects() ([]Project, error) {
	ms, err := s.store.List()
	if err != nil {
		return nil, err
	}
	type group struct {
		p         Project
		spellings map[string]int
		best      int
	}
	groups := map[string]*group{}
	var order []string
	for _, m := range ms { // newest first
		name := meeting.ProjectOf(m.Tags)
		if name == "" {
			continue
		}
		key := meeting.ProjectKey(name)
		g := groups[key]
		if g == nil {
			g = &group{p: Project{Name: name}, spellings: map[string]int{}}
			groups[key] = g
			order = append(order, key)
		}
		g.p.Meetings++
		if m.StartedAt.After(g.p.LastMeetingAt) {
			g.p.LastMeetingAt = m.StartedAt
		}
		g.spellings[name]++
		if c := g.spellings[name]; c > g.best {
			g.best = c
			g.p.Name = name
		}
	}
	out := make([]Project, 0, len(order))
	for _, k := range order {
		out = append(out, groups[k].p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastMeetingAt.After(out[j].LastMeetingAt)
	})
	return out, nil
}

// RenameProject moves every meeting in project from to project to.
// Matching is case-insensitive; renaming onto an existing project
// merges the two. It returns how many meetings were rewritten.
func (s *Surface) RenameProject(from, to string) (int, error) {
	from, err := ValidProjectName(from)
	if err != nil {
		return 0, err
	}
	to, err = ValidProjectName(to)
	if err != nil {
		return 0, err
	}
	ms, err := s.store.List()
	if err != nil {
		return 0, err
	}
	key := meeting.ProjectKey(from)
	n := 0
	for _, m := range ms {
		if meeting.ProjectKey(meeting.ProjectOf(m.Tags)) != key {
			continue
		}
		if err := s.store.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
			mm.Tags = meeting.WithProject(mm.Tags, to)
			return nil
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
