package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/tools"
)

func newTestServer(t *testing.T) (*Server, *meeting.Store) {
	t.Helper()
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(tools.New(store), &config.Config{}), store
}

// fakeEngine stands in for a real STT engine in HTTP-layer tests: no
// network, no audio hardware, one final result per pcm chunk received.
type fakeEngine struct{}

func (fakeEngine) SupportsDiarization() bool { return false }
func (fakeEngine) SupportsTranslation() bool { return false }

func (fakeEngine) Stream(_ context.Context, _ stt.Options) (chan<- stt.PCM, <-chan stt.Result, error) {
	pcmCh := make(chan stt.PCM)
	resultCh := make(chan stt.Result, 1)
	go func() {
		defer close(resultCh)
		for range pcmCh {
			select {
			case resultCh <- stt.Result{Text: "hi from fake engine", Final: true}:
			default:
			}
		}
	}()
	return pcmCh, resultCh, nil
}

func fixtureWAV(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.wav")
	w, err := audio.NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, 3200)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// withFakePipeline rigs s to build a fakeEngine and a file-backed capture
// source instead of touching real audio hardware or a real STT service.
func withFakePipeline(t *testing.T, s *Server) {
	t.Helper()
	fixture := fixtureWAV(t)
	s.newEngine = func(string, *config.Config) (stt.StreamEngine, error) { return fakeEngine{}, nil }
	s.newSource = func(string) (capture.Source, error) { return capture.NewFileSource(fixture, 800) }
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

func TestStartMeetingWithEngineWiresPipeline(t *testing.T) {
	s, store := newTestServer(t)
	withFakePipeline(t, s)

	w := do(t, s, "POST", "/api/meetings", `{"title":"Live Standup","engine":"whisper","lang":"en","translate_to":"vi"}`)
	var v map[string]any
	decode(t, w, &v)
	if w.Code != http.StatusOK {
		t.Fatalf("start: status = %d, body = %v", w.Code, v)
	}
	id, _ := v["id"].(string)
	if id == "" {
		t.Fatalf("start: no id in %v", v)
	}
	if v["engine"] != "whisper" || v["lang"] != "en" || v["translate_to"] != "vi" {
		t.Fatalf("engine/lang/translate_to not recorded: %v", v)
	}

	deadline := time.Now().Add(2 * time.Second)
	var us []meeting.Utterance
	for time.Now().Before(deadline) {
		var err error
		us, _, err = store.ReadSince(id, -1)
		if err != nil {
			t.Fatal(err)
		}
		if len(us) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(us) == 0 {
		t.Fatal("pipeline never appended an utterance")
	}
	if us[0].Text != "hi from fake engine" {
		t.Fatalf("utterance = %+v", us[0])
	}

	w = do(t, s, "POST", "/api/meetings/"+id+"/end", "")
	if w.Code != http.StatusOK {
		t.Fatalf("end: status = %d", w.Code)
	}
	s.mu.Lock()
	_, stillTracked := s.pipelines[id]
	s.mu.Unlock()
	if stillTracked {
		t.Fatal("pipeline still tracked after end")
	}

	audioPath := filepath.Join(store.Root(), id, "audio.wav")
	pcm, err := audio.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != 3200 {
		t.Fatalf("audio.wav has %d bytes, want 3200", len(pcm))
	}
}

func TestStartMeetingWithoutEngineHasNoPipeline(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "POST", "/api/meetings", `{"title":"Plain"}`)
	var v map[string]any
	decode(t, w, &v)
	if w.Code != http.StatusOK {
		t.Fatalf("start: status = %d, body = %v", w.Code, v)
	}
	if v["engine"] != nil && v["engine"] != "" {
		t.Fatalf("engine should be unset: %v", v["engine"])
	}
	s.mu.Lock()
	n := len(s.pipelines)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected no pipelines, got %d", n)
	}
}

func TestDevicesEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	w := do(t, s, "GET", "/api/devices", "")
	if w.Code != http.StatusOK {
		t.Fatalf("devices: status = %d", w.Code)
	}
	// Devices may be nil/empty in a headless test environment; the
	// endpoint must still respond with valid JSON.
	var devices []capture.Device
	decode(t, w, &devices)
}

func fakeWhisperBatchServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"segments": []map[string]any{
				{"start": 0.0, "end": 1.0, "text": "final hello"},
			},
		})
	}))
}

func TestRefineEndpoint(t *testing.T) {
	s, store := newTestServer(t)

	w := do(t, s, "POST", "/api/meetings/nope/refine", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: status = %d", w.Code)
	}

	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w = do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("active meeting: status = %d", w.Code)
	}

	if err := store.End(m.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	w = do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("no audio.wav: status = %d", w.Code)
	}

	audioPath := filepath.Join(store.Root(), m.ID, "audio.wav")
	aw, err := audio.NewWriter(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aw.Write(make([]byte, 1600)); err != nil {
		t.Fatal(err)
	}
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}

	whisper := fakeWhisperBatchServer(t)
	defer whisper.Close()
	s.cfg.WhisperURL = whisper.URL
	s.enrichFinal = func(_ context.Context, store *meeting.Store, id string) error {
		return store.UpdateDoc(id, func(mm *meeting.Meeting) error {
			mm.Refined = true
			mm.Summary = "fake enrichment"
			return nil
		})
	}

	w = do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	if w.Code != http.StatusOK {
		t.Fatalf("refine: status = %d, body = %s", w.Code, w.Body.String())
	}
	var v map[string]any
	decode(t, w, &v)
	if v["refined"] != true {
		t.Fatalf("refined not set in response: %v", v)
	}
	if v["summary"] != "fake enrichment" {
		t.Fatalf("summary not set in response: %v", v)
	}

	got, err := store.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "final hello" {
		t.Fatalf("batch transcript = %+v", got)
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
