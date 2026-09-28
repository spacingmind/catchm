package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

type projectJSON struct {
	Name          string    `json:"name"`
	Meetings      int       `json:"meetings"`
	LastMeetingAt time.Time `json:"last_meeting_at"`
}

func seedMeeting(t *testing.T, store *meeting.Store, title string, at time.Time, tags ...string) *meeting.Meeting {
	t.Helper()
	m, err := store.Start(title, at, meeting.StartOptions{Tags: tags})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func listProjects(t *testing.T, s *Server) []projectJSON {
	t.Helper()
	w := do(t, s, "GET", "/api/projects", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list projects: status = %d body = %s", w.Code, w.Body.String())
	}
	var ps []projectJSON
	decode(t, w, &ps)
	return ps
}

func TestProjectsEndpointGroupsCaseInsensitively(t *testing.T) {
	s, store := newTestServer(t)
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	seedMeeting(t, store, "a", base, "project:Acme Cloud", "q3")
	seedMeeting(t, store, "b", base.Add(time.Hour), "project:acme  cloud")
	seedMeeting(t, store, "c", base.Add(2*time.Hour), "Project:Acme Cloud")
	seedMeeting(t, store, "d", base.Add(3*time.Hour), "project:Beta")
	seedMeeting(t, store, "e", base.Add(4*time.Hour), "q3")
	seedMeeting(t, store, "f", base.Add(-time.Hour), "project:Old")

	ps := listProjects(t, s)
	want := []projectJSON{
		{Name: "Beta", Meetings: 1, LastMeetingAt: base.Add(3 * time.Hour)},
		// Two meetings spell it "Acme Cloud", one "acme cloud".
		{Name: "Acme Cloud", Meetings: 3, LastMeetingAt: base.Add(2 * time.Hour)},
		{Name: "Old", Meetings: 1, LastMeetingAt: base.Add(-time.Hour)},
	}
	if len(ps) != len(want) {
		t.Fatalf("projects = %+v", ps)
	}
	for i := range want {
		if ps[i].Name != want[i].Name || ps[i].Meetings != want[i].Meetings || !ps[i].LastMeetingAt.Equal(want[i].LastMeetingAt) {
			t.Fatalf("projects[%d] = %+v, want %+v", i, ps[i], want[i])
		}
	}
}

func TestProjectsEndpointEmpty(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "GET", "/api/projects", "")
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("status = %d body = %q, want []", w.Code, w.Body.String())
	}
}

func TestRenameProjectAndMerge(t *testing.T) {
	s, store := newTestServer(t)
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	a := seedMeeting(t, store, "a", base, "client", "project:Alpha", "q3")
	b := seedMeeting(t, store, "b", base.Add(time.Hour), "project:alpha")
	c := seedMeeting(t, store, "c", base.Add(2*time.Hour), "project:Beta")
	d := seedMeeting(t, store, "d", base.Add(3*time.Hour), "other")

	w := do(t, s, "POST", "/api/projects/rename", `{"from":"ALPHA","to":"  Alpha   One "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: status = %d body = %s", w.Code, w.Body.String())
	}
	var res struct {
		Renamed int `json:"renamed"`
	}
	decode(t, w, &res)
	if res.Renamed != 2 {
		t.Fatalf("renamed = %d, want 2", res.Renamed)
	}
	got, _ := store.Get(a.ID)
	if want := []string{"project:Alpha One", "client", "q3"}; !reflect.DeepEqual(got.Tags, want) {
		t.Fatalf("tags after rename = %q, want %q", got.Tags, want)
	}
	got, _ = store.Get(b.ID)
	if want := []string{"project:Alpha One"}; !reflect.DeepEqual(got.Tags, want) {
		t.Fatalf("tags after rename = %q, want %q", got.Tags, want)
	}

	// Renaming onto an existing project merges them.
	w = do(t, s, "POST", "/api/projects/rename", `{"from":"Beta","to":"alpha one"}`)
	decode(t, w, &res)
	if w.Code != http.StatusOK || res.Renamed != 1 {
		t.Fatalf("merge: status = %d renamed = %d", w.Code, res.Renamed)
	}
	got, _ = store.Get(c.ID)
	if meeting.ProjectKey(meeting.ProjectOf(got.Tags)) != "alpha one" {
		t.Fatalf("merged tags = %q", got.Tags)
	}
	got, _ = store.Get(d.ID)
	if !reflect.DeepEqual(got.Tags, []string{"other"}) {
		t.Fatalf("unrelated meeting changed: %q", got.Tags)
	}
	ps := listProjects(t, s)
	if len(ps) != 1 || ps[0].Name != "Alpha One" || ps[0].Meetings != 3 {
		t.Fatalf("projects after merge = %+v", ps)
	}

	// Unknown project: nothing to rename.
	w = do(t, s, "POST", "/api/projects/rename", `{"from":"Nope","to":"X"}`)
	decode(t, w, &res)
	if w.Code != http.StatusOK || res.Renamed != 0 {
		t.Fatalf("unknown: status = %d renamed = %d", w.Code, res.Renamed)
	}
}

func TestRenameProjectValidates(t *testing.T) {
	s, _ := newTestServer(t)
	long := strings.Repeat("x", 81)
	for _, body := range []string{
		`{"from":"","to":"X"}`,
		`{"from":"X","to":"   "}`,
		`{"from":"X","to":"` + long + `"}`,
		`not json`,
	} {
		if w := do(t, s, "POST", "/api/projects/rename", body); w.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, w.Code)
		}
	}
	if w := do(t, s, "POST", "/api/projects/rename", `{"from":"X","to":"`+strings.Repeat("é", 80)+`"}`); w.Code != http.StatusOK {
		t.Errorf("80 characters: status = %d, want 200", w.Code)
	}
}

func TestStartMeetingWithProject(t *testing.T) {
	s, store := newTestServer(t)
	w := do(t, s, "POST", "/api/meetings", `{"title":"Kickoff","tags":["q3","project:Stale"],"project":" Acme  Cloud "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("start: status = %d body = %s", w.Code, w.Body.String())
	}
	var m meeting.Meeting
	decode(t, w, &m)
	got, err := store.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"project:Acme Cloud", "q3"}; !reflect.DeepEqual(got.Tags, want) {
		t.Fatalf("tags = %q, want %q", got.Tags, want)
	}

	w = do(t, s, "POST", "/api/meetings", `{"title":"Too long","project":"`+strings.Repeat("x", 81)+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("long project: status = %d, want 400", w.Code)
	}
}

func TestImportWithProject(t *testing.T) {
	s, store := newTestServer(t)
	stubBatchEngineFor(s, []meeting.Utterance{{Text: "hello"}})
	s.cfg.Soniox.APIKey = "sk-test"
	unreachableWhisper(s)

	body, ct := importRequest(t, "standup.m4a", map[string]string{"title": "Weekly", "project": "Acme"})
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var m meeting.Meeting
	decode(t, w, &m)
	got, err := store.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tags, []string{"project:Acme"}) {
		t.Fatalf("tags = %q", got.Tags)
	}
}

func TestActionItemsCarryProject(t *testing.T) {
	s, store := newTestServer(t)
	m := seedMeeting(t, store, "a", time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), "project:Acme")
	if err := store.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		mm.ActionItems = []meeting.ActionItem{{Text: "ship it"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := do(t, s, "GET", "/api/action-items", "")
	var items []struct {
		MeetingProject string `json:"meeting_project"`
	}
	decode(t, w, &items)
	if len(items) != 1 || items[0].MeetingProject != "Acme" {
		t.Fatalf("items = %+v", items)
	}
}
