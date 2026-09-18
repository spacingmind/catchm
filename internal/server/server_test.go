package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

func newTestServer(t *testing.T) (*Server, *meeting.Store) {
	t.Helper()
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(tools.New(store)), store
}

func do(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
}

func TestHealth(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "GET", "/healthz", "")
	var v map[string]any
	decode(t, w, &v)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if v["status"] != "ok" {
		t.Fatalf("status field = %v", v["status"])
	}
}

func TestMeetingLifecycleOverHTTP(t *testing.T) {
	s, store := newTestServer(t)

	w := do(t, s, "POST", "/api/meetings", `{"title":"Q3 Roadmap Sync"}`)
	var v map[string]any
	decode(t, w, &v)
	if w.Code != http.StatusOK {
		t.Fatalf("start: status = %d, body = %v", w.Code, v)
	}
	id, _ := v["id"].(string)
	if id == "" {
		t.Fatalf("start: no id in %v", v)
	}

	w = do(t, s, "GET", "/api/meetings", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d", w.Code)
	}
	var ms []*meeting.Meeting
	if err := json.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Fatalf("list: decode: %v", err)
	}
	if len(ms) != 1 || ms[0].ID != id {
		t.Fatalf("list = %+v", ms)
	}

	w = do(t, s, "GET", "/api/meetings/"+id, "")
	v = map[string]any{}
	decode(t, w, &v)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status = %d", w.Code)
	}
	if v["title"] != "Q3 Roadmap Sync" {
		t.Fatalf("get: title = %v", v["title"])
	}

	w = do(t, s, "POST", "/api/meetings/"+id+"/end", "")
	if w.Code != http.StatusOK {
		t.Fatalf("end: status = %d", w.Code)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.EndedAt == nil {
		t.Fatal("end: meeting not ended on disk")
	}
}

func TestTranscriptWithCursor(t *testing.T) {
	s, store := newTestServer(t)
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second"} {
		if _, err := store.Append(m.ID, meeting.Utterance{Text: text}); err != nil {
			t.Fatal(err)
		}
	}

	w := do(t, s, "GET", "/api/meetings/"+m.ID+"/transcript", "")
	if w.Code != http.StatusOK {
		t.Fatalf("transcript: status = %d", w.Code)
	}
	var tr0 struct {
		Utterances []meeting.Utterance `json:"utterances"`
		Cursor     int64               `json:"cursor"`
	}
	decode(t, w, &tr0)
	if len(tr0.Utterances) != 2 {
		t.Fatalf("transcript: len = %d", len(tr0.Utterances))
	}
	if tr0.Cursor != 2 {
		t.Fatalf("transcript: cursor = %d", tr0.Cursor)
	}

	if _, err := store.Append(m.ID, meeting.Utterance{Text: "third"}); err != nil {
		t.Fatal(err)
	}
	w = do(t, s, "GET", "/api/meetings/"+m.ID+"/transcript?cursor=2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("delta: status = %d", w.Code)
	}
	var tr struct {
		Utterances []meeting.Utterance `json:"utterances"`
		Cursor     int64               `json:"cursor"`
	}
	decode(t, w, &tr)
	if len(tr.Utterances) != 1 || tr.Utterances[0].Text != "third" {
		t.Fatalf("delta = %+v", tr.Utterances)
	}
	if tr.Cursor != 3 {
		t.Fatalf("delta: cursor = %d", tr.Cursor)
	}
}

func TestAddNote(t *testing.T) {
	s, store := newTestServer(t)
	m, err := store.Start("Planning", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/notes", `{"note":"check the beta timeline"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add note: status = %d", w.Code)
	}
	got, err := store.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "check the beta timeline" {
		t.Fatalf("notes = %+v", got.Notes)
	}
}

func TestUnknownMeetingIs404(t *testing.T) {
	s, _ := newTestServer(t)
	for _, tc := range []struct {
		method, target string
	}{
		{"GET", "/api/meetings/nope"},
		{"GET", "/api/meetings/nope/transcript"},
		{"POST", "/api/meetings/nope/notes"},
		{"POST", "/api/meetings/nope/end"},
	} {
		w := do(t, s, tc.method, tc.target, `{"note":"x"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: status = %d, want 404", tc.method, tc.target, w.Code)
		}
	}
}
