package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/spacingmind/catchm/internal/translate"
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

// noopTranslator stands in for a real translate.Translator in HTTP-layer
// tests: no Ollama, no network.
type noopTranslator struct{}

func (noopTranslator) Translate(_ context.Context, text, _ string) (string, error) { return text, nil }

// countingNewTranslator rigs s.newTranslator to build a noopTranslator
// while recording the model it was asked for, so tests can assert whether
// a translator was constructed at all.
func countingNewTranslator(s *Server) *[]string {
	var models []string
	s.newTranslator = func(_ *config.Config, model string) (translate.Translator, error) {
		models = append(models, model)
		return noopTranslator{}, nil
	}
	return &models
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

// TestStartPipelineOnlyConstructsTranslatorWhenRequested proves the live
// translator seam is only invoked when a meeting actually asks for
// translation, so meetings that don't never introduce an Ollama dependency.
func TestStartPipelineOnlyConstructsTranslatorWhenRequested(t *testing.T) {
	s, _ := newTestServer(t)
	withFakePipeline(t, s)
	s.cfg.Translate.LiveModel = "translategemma:4b"
	models := countingNewTranslator(s)

	w := do(t, s, "POST", "/api/meetings", `{"title":"No Translate","engine":"whisper"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("start: status = %d", w.Code)
	}
	if len(*models) != 0 {
		t.Fatalf("translator constructed without translate_to: %v", *models)
	}

	w = do(t, s, "POST", "/api/meetings", `{"title":"With Translate","engine":"whisper","translate_to":"vi"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("start: status = %d", w.Code)
	}
	if len(*models) != 1 || (*models)[0] != "translategemma:4b" {
		t.Fatalf("translator not constructed with translate_to: %v", *models)
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

// TestHandleRefineOnlyConstructsTranslatorWhenRequested proves the batch
// translator seam is only invoked for meetings that asked for translation.
func TestHandleRefineOnlyConstructsTranslatorWhenRequested(t *testing.T) {
	s, store := newTestServer(t)
	s.cfg.Translate.BatchModel = "translategemma:12b"
	models := countingNewTranslator(s)

	whisper := fakeWhisperBatchServer(t)
	defer whisper.Close()
	s.cfg.WhisperURL = whisper.URL
	s.enrichFinal = func(_ context.Context, _ *meeting.Store, _ string) error { return nil }

	refine := func(translateTo string) {
		m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{TranslateTo: translateTo})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.End(m.ID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		aw, err := audio.NewWriter(filepath.Join(store.Root(), m.ID, "audio.wav"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := aw.Write(make([]byte, 1600)); err != nil {
			t.Fatal(err)
		}
		if err := aw.Close(); err != nil {
			t.Fatal(err)
		}
		w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
		if w.Code != http.StatusOK {
			t.Fatalf("refine: status = %d, body = %s", w.Code, w.Body.String())
		}
	}

	refine("")
	if len(*models) != 0 {
		t.Fatalf("translator constructed without translate_to: %v", *models)
	}

	refine("vi")
	if len(*models) != 1 || (*models)[0] != "translategemma:12b" {
		t.Fatalf("translator not constructed with translate_to: %v", *models)
	}
}

func fakeOllamaTagsServer(t *testing.T, installed ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		models := make([]map[string]any, len(installed))
		for i, name := range installed {
			models[i] = map[string]any{"name": name}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"models": models})
	}))
}

func TestTranslateStatusEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.Translate.LiveModel = "translategemma:4b"
	s.cfg.Translate.BatchModel = "translategemma:12b"

	ollama := fakeOllamaTagsServer(t, "translategemma:4b")
	defer ollama.Close()
	s.cfg.Translate.Host = ollama.URL

	w := do(t, s, "GET", "/api/translate/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var v struct {
		OllamaReachable bool `json:"ollama_reachable"`
		LiveModel       struct {
			Name      string `json:"name"`
			Installed bool   `json:"installed"`
		} `json:"live_model"`
		BatchModel struct {
			Name      string `json:"name"`
			Installed bool   `json:"installed"`
		} `json:"batch_model"`
	}
	decode(t, w, &v)
	if !v.OllamaReachable {
		t.Fatal("ollama_reachable = false, want true")
	}
	if !v.LiveModel.Installed {
		t.Fatal("live_model.installed = false, want true")
	}
	if v.BatchModel.Installed {
		t.Fatal("batch_model.installed = true, want false")
	}

	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	s.cfg.Translate.Host = unreachable.URL
	unreachable.Close()

	w = do(t, s, "GET", "/api/translate/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("unreachable: status = %d, want 200", w.Code)
	}
	decode(t, w, &v)
	if v.OllamaReachable {
		t.Fatal("ollama_reachable = true, want false")
	}
	if v.LiveModel.Installed || v.BatchModel.Installed {
		t.Fatalf("expected both installed = false when unreachable, got live=%v batch=%v", v.LiveModel.Installed, v.BatchModel.Installed)
	}
}

func TestTranslatePullEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.Translate.LiveModel = "translategemma:4b"

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["model"] != "translategemma:4b" {
			t.Fatalf("model = %v", req["model"])
		}
		fmt.Fprintln(w, `{"status":"downloading 50%"}`)
		fmt.Fprintln(w, `{"status":"success"}`)
	}))
	defer ollama.Close()
	s.cfg.Translate.Host = ollama.URL

	w := do(t, s, "POST", "/api/translate/pull", `{"target":"live"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content-type = %q", ct)
	}
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("body lines = %v", lines)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode line: %v", err)
	}
	if first["status"] != "downloading 50%" {
		t.Fatalf("first line = %v", first)
	}

	w = do(t, s, "POST", "/api/translate/pull", `{"target":"nonsense"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid target: status = %d, want 400", w.Code)
	}
}

func TestTranslateTestEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	countingNewTranslator(s)

	w := do(t, s, "POST", "/api/translate/test", `{"text":"hello","target_lang":"vi"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var v map[string]any
	decode(t, w, &v)
	if v["translated"] != "hello" {
		t.Fatalf("translated = %v", v["translated"])
	}

	w = do(t, s, "POST", "/api/translate/test", `{"target_lang":"vi"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing text: status = %d, want 400", w.Code)
	}
	w = do(t, s, "POST", "/api/translate/test", `{"text":"hello"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing target_lang: status = %d, want 400", w.Code)
	}

	s.newTranslator = func(_ *config.Config, _ string) (translate.Translator, error) {
		return failingTranslator{}, nil
	}
	w = do(t, s, "POST", "/api/translate/test", `{"text":"hello","target_lang":"vi"}`)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("translator error: status = %d, want 502", w.Code)
	}
}

type failingTranslator struct{}

func (failingTranslator) Translate(context.Context, string, string) (string, error) {
	return "", errors.New("boom")
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

// TestConfigEndpointGetSanitizesKey proves GET /api/config never leaks
// the raw Soniox API key: with a key configured, the response contains
// no trace of it and reports soniox_api_key_set: true instead.
func TestConfigEndpointGetSanitizesKey(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.Soniox.APIKey = "sk-top-secret"

	w := do(t, s, "GET", "/api/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "sk-top-secret") {
		t.Fatalf("response leaks the api key: %s", w.Body.String())
	}
	var v struct {
		Engine          string `json:"engine"`
		WhisperURL      string `json:"whisper_url"`
		SonioxAPIKeySet bool   `json:"soniox_api_key_set"`
		Soniox          struct {
			URL string `json:"url"`
		} `json:"soniox"`
		Translate struct {
			Host       string `json:"host"`
			LiveModel  string `json:"live_model"`
			BatchModel string `json:"batch_model"`
		} `json:"translate"`
	}
	decode(t, w, &v)
	if !v.SonioxAPIKeySet {
		t.Fatal("soniox_api_key_set = false, want true")
	}
	if v.WhisperURL != s.cfg.WhisperURL || v.Soniox.URL != s.cfg.Soniox.URL {
		t.Fatalf("sanitized fields not mirrored: %+v", v)
	}
}

// TestConfigEndpointPutPreservesKeyWhenOmitted is the milestone's main
// correctness trap: a PUT without soniox_api_key (or with it explicitly
// empty) must not wipe a previously stored key — neither in the live
// config nor in the file the next GET would reload from.
func TestConfigEndpointPutPreservesKeyWhenOmitted(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, _ := newTestServer(t)

	w := do(t, s, "PUT", "/api/config", `{"engine":"soniox","soniox_api_key":"sk-keep-me","soniox":{"url":"wss://api.soniox.com/transcribe-websocket"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("first put: status = %d, body = %s", w.Code, w.Body.String())
	}

	for _, body := range []string{
		`{"engine":"whisper","whisper_url":"http://127.0.0.1:8080"}`,
		`{"engine":"whisper","whisper_url":"http://127.0.0.1:8080","soniox_api_key":""}`,
	} {
		w = do(t, s, "PUT", "/api/config", body)
		if w.Code != http.StatusOK {
			t.Fatalf("second put %s: status = %d, body = %s", body, w.Code, w.Body.String())
		}
		w = do(t, s, "GET", "/api/config", "")
		var v struct {
			SonioxAPIKeySet bool `json:"soniox_api_key_set"`
		}
		decode(t, w, &v)
		if !v.SonioxAPIKeySet {
			t.Fatalf("put %s wiped the stored key", body)
		}
	}

	s.mu.Lock()
	key := s.cfg.Soniox.APIKey
	s.mu.Unlock()
	if key != "sk-keep-me" {
		t.Fatalf("stored key = %q, want %q", key, "sk-keep-me")
	}
	onDisk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.Soniox.APIKey != "sk-keep-me" {
		t.Fatalf("key on disk = %q, want %q", onDisk.Soniox.APIKey, "sk-keep-me")
	}
}

// TestConfigEndpointPutUpdatesFields proves a PUT's non-secret fields
// round-trip through the store into a subsequent GET (and that the saved
// file, not just the in-memory copy, carries them).
func TestConfigEndpointPutUpdatesFields(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, _ := newTestServer(t)

	w := do(t, s, "PUT", "/api/config", `{
		"engine": "soniox",
		"whisper_url": "http://localhost:9000",
		"soniox": {"url": "wss://stt.example.com/ws"},
		"translate": {"host": "http://localhost:11434", "live_model": "m-live", "batch_model": "m-batch"}
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put: status = %d, body = %s", w.Code, w.Body.String())
	}

	w = do(t, s, "GET", "/api/config", "")
	var v struct {
		Engine     string `json:"engine"`
		WhisperURL string `json:"whisper_url"`
		Soniox     struct {
			URL string `json:"url"`
		} `json:"soniox"`
		Translate struct {
			Host       string `json:"host"`
			LiveModel  string `json:"live_model"`
			BatchModel string `json:"batch_model"`
		} `json:"translate"`
	}
	decode(t, w, &v)
	if v.Engine != "soniox" || v.WhisperURL != "http://localhost:9000" || v.Soniox.URL != "wss://stt.example.com/ws" {
		t.Fatalf("updated fields not reflected: %+v", v)
	}
	if v.Translate.Host != "http://localhost:11434" || v.Translate.LiveModel != "m-live" || v.Translate.BatchModel != "m-batch" {
		t.Fatalf("translate fields not reflected: %+v", v.Translate)
	}
}

// TestConfigEndpointPutRejectsUnknownEngine keeps the cheap validation
// at the boundary: engine must be a known name when non-empty.
func TestConfigEndpointPutRejectsUnknownEngine(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, _ := newTestServer(t)
	w := do(t, s, "PUT", "/api/config", `{"engine":"nonsense"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
