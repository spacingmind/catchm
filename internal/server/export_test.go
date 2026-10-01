package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

func newExportMeeting(t *testing.T, s *Server, store *meeting.Store) *meeting.Meeting {
	t.Helper()
	m, err := store.Start("Họp dự án Q3", time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(m.ID, meeting.Utterance{Speaker: "Speaker 1", Text: "Chào mọi người."}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestHandleExportFormats(t *testing.T) {
	s, store := newTestServer(t)
	m := newExportMeeting(t, s, store)

	cases := []struct {
		format, contentType string
	}{
		{"txt", "text/plain; charset=utf-8"},
		{"md", "text/markdown; charset=utf-8"},
		{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	}
	for _, c := range cases {
		w := do(t, s, http.MethodGet, "/api/meetings/"+m.ID+"/export?format="+c.format, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s", c.format, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); got != c.contentType {
			t.Fatalf("%s: Content-Type = %q, want %q", c.format, got, c.contentType)
		}
		cd := w.Header().Get("Content-Disposition")
		if !strings.Contains(cd, "filename*=UTF-8''") {
			t.Fatalf("%s: Content-Disposition = %q", c.format, cd)
		}
		if !strings.Contains(cd, "hop-du-an-q3") {
			t.Fatalf("%s: Content-Disposition missing ascii name: %q", c.format, cd)
		}
		switch c.format {
		case "txt":
			if !strings.Contains(w.Body.String(), "Họp dự án Q3") {
				t.Fatalf("txt body missing title: %q", w.Body.String())
			}
		case "docx":
			if !strings.HasPrefix(w.Body.String(), "PK") {
				t.Fatalf("docx body does not start with PK")
			}
		}
	}
}

func TestHandleExportLang(t *testing.T) {
	s, store := newTestServer(t)
	m := newExportMeeting(t, s, store)

	w := do(t, s, http.MethodGet, "/api/meetings/"+m.ID+"/export?format=txt&lang=vi", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Bản chép lời") {
		t.Fatalf("vi txt missing heading; got:\n%s", w.Body.String())
	}
}

func TestHandleExportBadFormat(t *testing.T) {
	s, store := newTestServer(t)
	m := newExportMeeting(t, s, store)

	w := do(t, s, http.MethodGet, "/api/meetings/"+m.ID+"/export?format=pdf", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
}

func TestHandleExportNotFound(t *testing.T) {
	s, _ := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/meetings/nope/export?format=txt", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "meeting_not_found") {
		t.Fatalf("body missing meeting_not_found: %s", w.Body.String())
	}
}
