package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/summarize"
)

// stubBatchEngine answers fixed utterances and records the audio path
// and options it saw.
type stubBatchEngine struct {
	us       []meeting.Utterance
	lastPath atomic.Pointer[string]
	lastOpts atomic.Pointer[stt.Options]
}

func (e *stubBatchEngine) TranscribeFile(_ context.Context, path string, opts stt.Options) ([]meeting.Utterance, error) {
	e.lastPath.Store(&path)
	e.lastOpts.Store(&opts)
	return e.us, nil
}

// stubBatchEngineFor records which engine names each call used.
func stubBatchEngineFor(s *Server, us []meeting.Utterance) *[]string {
	var names []string
	s.newBatchEngine = func(_ *config.Config, engine string) (stt.BatchEngine, error) {
		names = append(names, engine)
		return &stubBatchEngine{us: us}, nil
	}
	return &names
}

// unreachableWhisper points Whisper at a closed server.
func unreachableWhisper(s *Server) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	s.cfg.WhisperURL = down.URL
}

func upWhisper(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	w := httptest.NewServer(http.NotFoundHandler()) // any response = reachable
	t.Cleanup(w.Close)
	s.cfg.WhisperURL = w.URL
	return w
}

// TestRefineAutoEngineMatrix covers plan decision 1's auto rules. The
// engine actually used is observed through the newBatchEngine seam
// (nil engine = "none": the seam is never invoked).
func TestRefineAutoEngineMatrix(t *testing.T) {
	cases := []struct {
		name       string
		meetingEng string
		sonioxKey  bool
		whisperUp  bool
		want       string
	}{
		{"soniox meeting with key", "soniox", true, false, "soniox"},
		{"whisper up", "", false, true, "whisper"},
		{"no whisper with key", "", true, false, "soniox"},
		{"nothing available", "", false, false, "none"},
		{"whisper up but soniox meeting with key", "soniox", true, true, "soniox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, store := newTestServer(t)
			m := refinableMeeting(t, s, store)
			stubSummarizer(t, s, &fakeSummarizer{ex: summarize.Analysis{Summary: "ok"}})
			if err := store.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
				mm.Engine = tc.meetingEng
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if tc.whisperUp {
				upWhisper(t, s)
			} else {
				unreachableWhisper(s)
			}
			s.cfg.Soniox.APIKey = ""
			if tc.sonioxKey {
				s.cfg.Soniox.APIKey = "sk-test"
			}
			names := stubBatchEngineFor(s, []meeting.Utterance{{Text: "final hello"}})

			w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if tc.want == "none" {
				if len(*names) != 0 {
					t.Fatalf("none still built engines: %v", *names)
				}
				if _, err := store.ReadBatchTranscript(m.ID); err == nil {
					us, _ := store.ReadBatchTranscript(m.ID)
					if len(us) != 0 {
						t.Fatalf("none wrote a batch transcript: %+v", us)
					}
				}
				got, _ := store.Get(m.ID)
				if got.RefinedWith != "" {
					t.Fatalf("refined_with = %q, want empty for none", got.RefinedWith)
				}
				return
			}
			if len(*names) != 1 || (*names)[0] != tc.want {
				t.Fatalf("engines = %v, want [%s]", *names, tc.want)
			}
			got, _ := store.Get(m.ID)
			if got.RefinedWith != tc.want {
				t.Fatalf("refined_with = %q, want %q", got.RefinedWith, tc.want)
			}
		})
	}
}

// TestRefineExplicitEngineUnusable: an explicit engine that can't run
// fails fast with {error, kind:"summary_not_ready"} and 503, before the
// batch pass.
func TestRefineExplicitEngineUnusable(t *testing.T) {
	t.Run("soniox without key", func(t *testing.T) {
		s, store := newTestServer(t)
		m := refinableMeeting(t, s, store)
		stubSummarizer(t, s, &fakeSummarizer{})
		unreachableWhisper(s)
		s.cfg.Soniox.APIKey = ""
		names := stubBatchEngineFor(s, nil)
		w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", `{"engine":"soniox"}`)
		var v refineErrBody
		decode(t, w, &v)
		if w.Code != http.StatusServiceUnavailable || v.Kind != "summary_not_ready" ||
			!strings.Contains(v.Error, "No Soniox API key") {
			t.Fatalf("status = %d body = %+v", w.Code, v)
		}
		if len(*names) != 0 {
			t.Fatal("engine was built despite being unusable")
		}
	})
	t.Run("whisper down", func(t *testing.T) {
		s, store := newTestServer(t)
		m := refinableMeeting(t, s, store)
		stubSummarizer(t, s, &fakeSummarizer{})
		unreachableWhisper(s)
		s.cfg.Soniox.APIKey = "sk-test"
		names := stubBatchEngineFor(s, nil)
		w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", `{"engine":"whisper"}`)
		var v refineErrBody
		decode(t, w, &v)
		if w.Code != http.StatusServiceUnavailable || v.Kind != "summary_not_ready" ||
			!strings.Contains(v.Error, "Whisper server") {
			t.Fatalf("status = %d body = %+v", w.Code, v)
		}
		if len(*names) != 0 {
			t.Fatal("engine was built despite being unusable")
		}
	})
	t.Run("bad engine name", func(t *testing.T) {
		s, store := newTestServer(t)
		m := refinableMeeting(t, s, store)
		stubSummarizer(t, s, &fakeSummarizer{})
		w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", `{"engine":"nonsense"}`)
		// A malformed request, not an unready engine.
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
		}
	})
}

// TestRefineSetsRefinedWith: a successful re-transcription records the
// engine in the frontmatter and returns it in the response.
func TestRefineSetsRefinedWith(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	stubSummarizer(t, s, &fakeSummarizer{})
	upWhisper(t, s)
	stubBatchEngineFor(s, []meeting.Utterance{{Text: "x"}})

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", `{"engine":"whisper"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var v map[string]any
	decode(t, w, &v)
	if v["refined_with"] != "whisper" {
		t.Fatalf("refined_with = %v", v["refined_with"])
	}
	got, _ := store.Get(m.ID)
	if got.RefinedWith != "whisper" {
		t.Fatalf("frontmatter refined_with = %q", got.RefinedWith)
	}
}

// TestRefineExplicitNoneSummarizesLive: engine "none" skips
// re-transcription and still summarizes (from the live transcript).
func TestRefineExplicitNoneSummarizesLive(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store) // has a live "live hello" line
	stubSummarizer(t, s, &fakeSummarizer{})
	unreachableWhisper(s)
	names := stubBatchEngineFor(s, nil)

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", `{"engine":"none"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(*names) != 0 {
		t.Fatalf("none built engines: %v", *names)
	}
	got, _ := store.Get(m.ID)
	if !got.Refined || got.RefinedWith != "" {
		t.Fatalf("meeting = refined:%v refined_with:%q", got.Refined, got.RefinedWith)
	}
}

// TestRefineNoneWithoutWhisperDependency proves the summary readiness
// check no longer requires Whisper: with Whisper down, no Soniox key
// and a ready summarizer, engine auto → none and the request succeeds.
func TestRefineNoneWithoutWhisperDependency(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	stubSummarizer(t, s, &fakeSummarizer{})
	unreachableWhisper(s)
	names := stubBatchEngineFor(s, nil)

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(*names) != 0 {
		t.Fatalf("auto with nothing available built engines: %v", *names)
	}
}

// importRequest builds a multipart import body.
func importRequest(t *testing.T, filename string, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("fake audio bytes"))
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	return &body, mw.FormDataContentType()
}

func doImport(t *testing.T, s *Server, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/meetings/import", body)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestImportHappyPath(t *testing.T) {
	s, store := newTestServer(t)
	stubBatchEngineFor(s, []meeting.Utterance{{Text: "hello", Speaker: "1"}})
	s.cfg.Soniox.APIKey = "sk-test"
	unreachableWhisper(s) // auto falls through to soniox

	body, ct := importRequest(t, "standup.m4a", map[string]string{"title": "Weekly", "lang": "en"})
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var m meeting.Meeting
	decode(t, w, &m)
	if m.Title != "Weekly" || !m.Imported || m.EndedAt == nil || m.Lang != "en" || m.Engine != "" {
		t.Fatalf("meeting = %+v", m)
	}
	if m.RefinedWith != "soniox" {
		t.Fatalf("refined_with = %q", m.RefinedWith)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), m.ID, "audio.m4a")); err != nil {
		t.Fatalf("audio.m4a not stored: %v", err)
	}
	us, err := store.ReadBatchTranscript(m.ID)
	if err != nil || len(us) != 1 || us[0].Text != "hello" {
		t.Fatalf("batch transcript = %+v (%v)", us, err)
	}
}

func TestImportDefaultTitleAndTwoWay(t *testing.T) {
	s, _ := newTestServer(t)
	var opts atomic.Pointer[stt.Options]
	s.newBatchEngine = func(_ *config.Config, engine string) (stt.BatchEngine, error) {
		e := &stubBatchEngine{}
		return engineFunc(func(ctx context.Context, path string, o stt.Options) ([]meeting.Utterance, error) {
			opts.Store(&o)
			return e.TranscribeFile(ctx, path, o)
		}), nil
	}
	s.cfg.Soniox.APIKey = "sk-test"
	unreachableWhisper(s)

	body, ct := importRequest(t, "quarterly-review.mp3", map[string]string{
		"lang": "en", "translate_to": "vi", "two_way": "true"})
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var m meeting.Meeting
	decode(t, w, &m)
	if m.Title != "quarterly-review" {
		t.Fatalf("default title = %q", m.Title)
	}
	o := opts.Load()
	if o == nil || !o.TwoWay || o.LanguageB != "vi" || o.Lang != "en" {
		got := stt.Options{}
		if o != nil {
			got = *o
		}
		t.Fatalf("engine opts = %+v", got)
	}
}

type engineFunc func(context.Context, string, stt.Options) ([]meeting.Utterance, error)

func (f engineFunc) TranscribeFile(ctx context.Context, path string, opts stt.Options) ([]meeting.Utterance, error) {
	return f(ctx, path, opts)
}

func TestImportBadExtension(t *testing.T) {
	s, _ := newTestServer(t)
	body, ct := importRequest(t, "notes.txt", nil)
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var v map[string]any
	decode(t, w, &v)
	if !strings.Contains(v["error"].(string), "Unsupported file type") {
		t.Fatalf("error = %v", v["error"])
	}
}

func TestImportWhisperNonWavIs400(t *testing.T) {
	s, _ := newTestServer(t)
	upWhisper(t, s)
	s.cfg.Soniox.APIKey = ""
	body, ct := importRequest(t, "song.mp3", nil)
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var v map[string]any
	decode(t, w, &v)
	if !strings.Contains(v["error"].(string), "Soniox") {
		t.Fatalf("error = %v, want a Soniox suggestion", v["error"])
	}
}

// TestImportFailureKeepsMeeting: a transcription failure after the
// meeting was created keeps the meeting and answers {error, kind,
// meeting_id}.
func TestImportFailureKeepsMeeting(t *testing.T) {
	s, store := newTestServer(t)
	s.newBatchEngine = func(_ *config.Config, _ string) (stt.BatchEngine, error) {
		return engineFunc(func(context.Context, string, stt.Options) ([]meeting.Utterance, error) {
			return nil, os.ErrDeadlineExceeded
		}), nil
	}
	s.cfg.Soniox.APIKey = "sk-test"
	unreachableWhisper(s)

	body, ct := importRequest(t, "standup.m4a", nil)
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var v struct {
		Error     string `json:"error"`
		Kind      string `json:"kind"`
		MeetingID string `json:"meeting_id"`
	}
	decode(t, w, &v)
	if v.Kind != "summary_failed" || v.MeetingID == "" {
		t.Fatalf("body = %+v", v)
	}
	if _, err := store.Get(v.MeetingID); err != nil {
		t.Fatalf("meeting not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), v.MeetingID, "audio.m4a")); err != nil {
		t.Fatalf("audio not kept: %v", err)
	}
}

func TestImportSizeLimit(t *testing.T) {
	s, _ := newTestServer(t)
	old := importMaxBodyBytes
	importMaxBodyBytes = 512
	defer func() { importMaxBodyBytes = old }()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "big.wav")
	fw.Write(make([]byte, 4096))
	mw.Close()
	w := doImport(t, s, &body, mw.FormDataContentType())
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var v map[string]any
	decode(t, w, &v)
	if !strings.Contains(v["error"].(string), "1 GiB") {
		t.Fatalf("error = %v", v["error"])
	}
}

func TestImportEndsAtLastUtterance(t *testing.T) {
	s, _ := newTestServer(t)
	// TranscribeFile returns offsets from the zero time; Refine shifts
	// them onto started_at, so the last line sits 90s into the meeting.
	stubBatchEngineFor(s, []meeting.Utterance{
		{Text: "first", At: time.Time{}.Add(2 * time.Second)},
		{Text: "last", At: time.Time{}.Add(90 * time.Second)},
	})
	s.cfg.Soniox.APIKey = "sk-test"
	unreachableWhisper(s)

	body, ct := importRequest(t, "call.m4a", nil)
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var m meeting.Meeting
	decode(t, w, &m)
	if m.EndedAt == nil || m.EndedAt.Sub(m.StartedAt) != 90*time.Second {
		t.Fatalf("span = started %v ended %v, want 90s", m.StartedAt, m.EndedAt)
	}
}

func TestImportRejectsSecondFile(t *testing.T) {
	s, store := newTestServer(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, name := range []string{"a.wav", "b.wav"} {
		fw, err := mw.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte("fake"))
	}
	mw.Close()
	w := doImport(t, s, &body, mw.FormDataContentType())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	// Neither upload may linger in the temp dir.
	entries, _ := os.ReadDir(filepath.Join(store.Root(), ".import-tmp"))
	if len(entries) != 0 {
		t.Fatalf("leftover temp files: %d", len(entries))
	}
}

// A Whisper port that accepts but never answers (seen with WSL port
// forwarding) must not hang the import: the probe is time-bounded.
func TestImportWhisperProbeIsBounded(t *testing.T) {
	s, _ := newTestServer(t)
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer hang.Close()
	defer close(release)
	s.cfg.WhisperURL = hang.URL
	orig := refineEngineWhisperReachable
	refineEngineWhisperReachable = stt.WhisperReachable
	defer func() { refineEngineWhisperReachable = orig }()

	body, ct := importRequest(t, "call.wav", nil)
	start := time.Now()
	w := doImport(t, s, body, ct)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("import took %v; the Whisper probe isn't bounded", d)
	}
}

// Improve with Soniox (summarize "if_ready") must still re-transcribe
// when summaries aren't set up, reporting why the summary was skipped.
func TestRefineIfReadySkipsUnreadySummary(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	s.newSummarizer = func(*config.Config, summarize.CloudAuth) (summarize.Summarizer, error) {
		return nil, &summarize.Error{Kind: summarize.KindNotReady, Message: "Ollama isn't running."}
	}
	s.cfg.Soniox.APIKey = "sk-test"
	names := stubBatchEngineFor(s, []meeting.Utterance{{Text: "better text"}})

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", `{"engine":"soniox","summarize":"if_ready"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var v struct {
		RefinedWith    string `json:"refined_with"`
		SummarySkipped string `json:"summary_skipped"`
	}
	decode(t, w, &v)
	if v.RefinedWith != "soniox" || v.SummarySkipped != "Ollama isn't running." || len(*names) != 1 {
		t.Fatalf("got %+v, engines built %v", v, *names)
	}
	if us, err := store.ReadBatchTranscript(m.ID); err != nil || len(us) != 1 || us[0].Text != "better text" {
		t.Fatalf("batch transcript = %+v (%v)", us, err)
	}

	// Without if_ready, refine stays strict.
	w = do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", `{"engine":"soniox"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("strict refine status = %d, want 503", w.Code)
	}
}
