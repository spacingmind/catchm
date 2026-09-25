package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/stt"
)

// fakeInputStream is a capture.Source the tests fully control.
type fakeInputStream struct {
	ch     chan stt.PCM
	mu     sync.Mutex
	closed bool
}

func newFakeInputStream() *fakeInputStream {
	return &fakeInputStream{ch: make(chan stt.PCM, 64)}
}

func (f *fakeInputStream) Chunks() <-chan stt.PCM { return f.ch }
func (f *fakeInputStream) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.ch)
	}
	return nil
}

func TestAudioSourcesEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	res := do(t, s, http.MethodGet, "/api/audio/sources", "")
	if res.Code != http.StatusOK {
		t.Fatalf("code = %d", res.Code)
	}
	var body struct {
		MicDevices []map[string]any `json:"mic_devices"`
		System     struct {
			Supported bool   `json:"supported"`
			Reason    string `json:"reason"`
		} `json:"system"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.System.Reason == "" && !body.System.Supported {
		t.Fatalf("unsupported platform must carry a reason: %+v", body.System)
	}
	// mic_devices is present (possibly empty on a headless runner).
	if body.MicDevices == nil {
		body.MicDevices = []map[string]any{}
	}
	// /api/devices still works.
	resOld := do(t, s, http.MethodGet, "/api/devices", "")
	if resOld.Code != http.StatusOK {
		t.Fatalf("legacy /api/devices broken: %d", resOld.Code)
	}
}

// Boot a live pipeline over fake sources so the endpoints have
// something real to poke: fake engine + mixer over one fake input.
func startPipelinedMeeting(t *testing.T, s *Server) string {
	t.Helper()
	s.newEngine = func(name string, cfg *config.Config) (stt.StreamEngine, error) {
		return &fakeEngine{}, nil
	}
	mic := newFakeInputStream()
	s.newSource = func(device string) (capture.Source, error) {
		return mic, nil
	}
	res := do(t, s, http.MethodPost, "/api/meetings", `{"title":"Src switch","engine":"whisper","source":"mic"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("start meeting: %d %s", res.Code, res.Body.String())
	}
	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func TestSetMeetingSourceErrors(t *testing.T) {
	s, _ := newTestServer(t)

	// No live pipeline → 404.
	res := do(t, s, http.MethodPost, "/api/meetings/zzz/source", `{"source":"system"}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("no-pipeline code = %d, want 404", res.Code)
	}

	id := startPipelinedMeeting(t, s)

	// Bad source → 400.
	res = do(t, s, http.MethodPost, "/api/meetings/"+id+"/source", `{"source":"nope"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("bad source code = %d, want 400", res.Code)
	}

	// System unsupported on linux → 422 with the platform reason, and
	// the OLD mic input keeps running (a subsequent levels call still
	// answers for mic).
	if ok, _ := capture.SystemAudioSupported(); !ok {
		res = do(t, s, http.MethodPost, "/api/meetings/"+id+"/source", `{"source":"system"}`)
		if res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("unsupported system code = %d, want 422", res.Code)
		}
		if !strings.Contains(res.Body.String(), "system audio") {
			t.Fatalf("422 body: %s", res.Body.String())
		}
		lv := do(t, s, http.MethodGet, "/api/meetings/"+id+"/levels", "")
		if lv.Code != http.StatusOK || !strings.Contains(lv.Body.String(), `"mic"`) {
			t.Fatalf("old input lost after failed switch: %d %s", lv.Code, lv.Body.String())
		}
	}
}

func TestSetMeetingSourceSuccessRecordsTimeline(t *testing.T) {
	s, store := newTestServer(t)
	id := startPipelinedMeeting(t, s)

	// Switch mic → mic (different device): newSource opens fine.
	res := do(t, s, http.MethodPost, "/api/meetings/"+id+"/source", `{"source":"mic","device":""}`)
	if res.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Source string `json:"source"`
		Device string `json:"device"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Source != "mic" {
		t.Fatalf("source = %q", body.Source)
	}

	// The meeting records audio_source and a timestamped note.
	m, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.AudioSource != "mic" {
		t.Fatalf("audio_source = %q", m.AudioSource)
	}
	found := false
	for _, n := range m.Notes {
		if strings.Contains(n, "audio source switched to mic at ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("switch note missing: %v", m.Notes)
	}

	// GET /api/meetings/{id} includes audio_source.
	res = do(t, s, http.MethodGet, "/api/meetings/"+id, "")
	var got struct {
		AudioSource string `json:"audio_source"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AudioSource != "mic" {
		t.Fatalf("GET audio_source = %q", got.AudioSource)
	}
}

func TestMeetingLevelsShape(t *testing.T) {
	s, _ := newTestServer(t)
	id := startPipelinedMeeting(t, s)

	res := do(t, s, http.MethodGet, "/api/meetings/"+id+"/levels", "")
	if res.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["mic"]; !ok {
		t.Fatalf("active mic input missing from levels: %v", body)
	}
	if _, ok := body["system"]; ok {
		t.Fatalf("inactive system input present in levels: %v", body)
	}
	mic := body["mic"].(map[string]any)
	for _, k := range []string{"rms", "peak", "dropped_samples"} {
		if _, ok := mic[k]; !ok {
			t.Fatalf("mic levels missing %q: %v", k, mic)
		}
	}
	if _, ok := body["dropped_ticks"]; !ok {
		t.Fatalf("dropped_ticks missing: %v", body)
	}

	// 404 without a pipeline.
	res = do(t, s, http.MethodGet, "/api/meetings/zzz/levels", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("no-pipeline levels code = %d, want 404", res.Code)
	}
}

// countingSource is a fake input that counts Close calls.
type countingSource struct {
	ch         chan stt.PCM
	closeCount atomic.Int64
}

func (c *countingSource) Chunks() <-chan stt.PCM { return c.ch }
func (c *countingSource) Close() error {
	c.closeCount.Add(1)
	return nil
}

// Ownership protocol: every source opened across start → switch →
// switch → end gets closed exactly once (start's source when displaced
// by switch 1, switch 1's when displaced by switch 2, switch 2's at
// pipeline stop).
func TestOwnershipEverySourceClosedExactlyOnce(t *testing.T) {
	s, _ := newTestServer(t)
	s.newEngine = func(name string, cfg *config.Config) (stt.StreamEngine, error) {
		return &fakeEngine{}, nil
	}

	var mu sync.Mutex
	opened := []*countingSource{}
	newSrc := func() capture.Source {
		src := &countingSource{ch: make(chan stt.PCM, 8)}
		mu.Lock()
		opened = append(opened, src)
		mu.Unlock()
		return src
	}
	s.newSource = func(device string) (capture.Source, error) { return newSrc(), nil }

	res := do(t, s, http.MethodPost, "/api/meetings", `{"title":"Own","engine":"whisper","source":"mic"}`)
	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &m); err != nil || m.ID == "" {
		t.Fatalf("start failed: %d %s", res.Code, res.Body.String())
	}

	// Two in-place switches (mic → mic → mic).
	for i := 0; i < 2; i++ {
		res := do(t, s, http.MethodPost, "/api/meetings/"+m.ID+"/source", `{"source":"mic"}`)
		if res.Code != http.StatusOK {
			t.Fatalf("switch %d: %d %s", i, res.Code, res.Body.String())
		}
		// Give the mixer a beat to finish the swap cleanup.
		time.Sleep(20 * time.Millisecond)
	}

	// End the meeting (stops the pipeline).
	res = do(t, s, http.MethodPost, "/api/meetings/"+m.ID+"/end", "")
	if res.Code != http.StatusOK {
		t.Fatalf("end: %d %s", res.Code, res.Body.String())
	}
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 3 {
		t.Fatalf("expected 3 sources opened (start + 2 switches), got %d", len(opened))
	}
	for i, src := range opened {
		if n := src.closeCount.Load(); n != 1 {
			t.Fatalf("source %d closed %d times, want exactly 1", i, n)
		}
	}
}

// A failing newSource on switch: 422, old input stays live.
func TestSwitchFailureKeepsOldInput422(t *testing.T) {
	s, _ := newTestServer(t)
	s.newEngine = func(name string, cfg *config.Config) (stt.StreamEngine, error) {
		return &fakeEngine{}, nil
	}
	live := &countingSource{ch: make(chan stt.PCM, 8)}
	s.newSource = func(device string) (capture.Source, error) { return live, nil }
	res := do(t, s, http.MethodPost, "/api/meetings", `{"title":"Keep","engine":"whisper","source":"mic"}`)
	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &m); err != nil || m.ID == "" {
		t.Fatalf("start: %d %s", res.Code, res.Body.String())
	}

	// Now make opens fail.
	s.newSource = func(device string) (capture.Source, error) {
		return nil, errors.New("mic unplugged")
	}
	res = do(t, s, http.MethodPost, "/api/meetings/"+m.ID+"/source", `{"source":"mic"}`)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("switch code = %d, want 422 (body: %s)", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "mic unplugged") {
		t.Fatalf("422 body: %s", res.Body.String())
	}
	// Old input still installed and live.
	lv := do(t, s, http.MethodGet, "/api/meetings/"+m.ID+"/levels", "")
	if lv.Code != http.StatusOK || !strings.Contains(lv.Body.String(), `"mic"`) {
		t.Fatalf("old input lost after failed switch: %d %s", lv.Code, lv.Body.String())
	}
	if n := live.closeCount.Load(); n != 0 {
		t.Fatalf("live source closed %d times before meeting end", n)
	}
}
