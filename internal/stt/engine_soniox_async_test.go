package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSoniox is an httptest-backed fake of the Soniox async REST API.
// It records requests, serves canned tokens, and can be told to fail a
// given path or delay completion for one poll.
type fakeSoniox struct {
	mu           sync.Mutex
	tokens       []sonioxAsyncToken
	authHeader   string
	uploadRef    string
	createBody   map[string]any
	createCount  int
	pollCount    int
	pendingPolls int // respond "processing" this many times first
	failPath     string
	deleteCalls  []string
	uploadBody   []byte
	now          time.Time
	srv          *httptest.Server
}

func newFakeSoniox(t *testing.T) *fakeSoniox {
	t.Helper()
	f := &fakeSoniox{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/files", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.authHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		f.uploadBody = body
		if m := regexp.MustCompile(`name="client_reference_id"\r\n\r\n([^\r]+)`).FindSubmatch(body); m != nil {
			f.uploadRef = string(m[1])
		}
		if f.failPath == "upload" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":"file-1","filename":"audio.wav","size":3,"created_at":%q,"client_reference_id":%q}`, f.now.Format(time.RFC3339), f.uploadRef)
	})
	mux.HandleFunc("POST /v1/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.createCount++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.createBody = body
		if f.failPath == "create" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":"tr-1","status":"queued","created_at":%q}`, f.now.Format(time.RFC3339))
	})
	mux.HandleFunc("GET /v1/transcriptions/tr-1", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pollCount++
		if f.failPath == "poll" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		status := "completed"
		if f.pollCount <= f.pendingPolls {
			status = "processing"
		}
		fmt.Fprintf(w, `{"id":"tr-1","status":%q,"created_at":%q}`, status, f.now.Format(time.RFC3339))
	})
	mux.HandleFunc("GET /v1/transcriptions/tr-1/transcript", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failPath == "fetch" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		toks := make([]map[string]any, len(f.tokens))
		for i, tok := range f.tokens {
			b, _ := json.Marshal(tok)
			var m map[string]any
			json.Unmarshal(b, &m)
			toks[i] = m
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "tr-1", "text": "", "tokens": toks})
	})
	mux.HandleFunc("DELETE /v1/transcriptions/tr-1", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.deleteCalls = append(f.deleteCalls, "transcription:tr-1")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1/files/file-1", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.deleteCalls = append(f.deleteCalls, "file:file-1")
		w.WriteHeader(http.StatusNoContent)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSoniox) deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleteCalls...)
}

func newTestAsyncEngine(f *fakeSoniox, t *testing.T) *SonioxAsyncEngine {
	t.Helper()
	return NewSonioxAsyncEngine("test-key", f.srv.URL,
		WithSonioxAsyncClient(f.srv.Client()),
		WithSonioxAsyncPollInterval(time.Millisecond),
		WithSonioxAsyncClock(func() time.Time { return f.now }, func(ctx context.Context, d time.Duration) error {
			return sleepContext(ctx, time.Millisecond)
		}),
	)
}

func writeAudio(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	meetingDir := filepath.Join(dir, "abc12345")
	if err := os.Mkdir(meetingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(meetingDir, "audio.wav")
	if err := os.WriteFile(p, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSonioxAsyncFullFlow(t *testing.T) {
	f := newFakeSoniox(t)
	f.tokens = []sonioxAsyncToken{
		{sonioxToken: sonioxToken{Text: "Hello", Speaker: "1", TranslationStatus: "original", Language: "en"}, StartMS: 0, EndMS: 400},
		{sonioxToken: sonioxToken{Text: " world", Speaker: "1", TranslationStatus: "original", Language: "en"}, StartMS: 450, EndMS: 800},
		{sonioxToken: sonioxToken{Text: "xin chao", TranslationStatus: "translation", Language: "vi"}, StartMS: 0, EndMS: 0},
	}
	f.pendingPolls = 1 // first poll: processing, second: completed
	e := newTestAsyncEngine(f, t)
	audio := writeAudio(t)

	us, err := e.TranscribeFile(context.Background(), audio, Options{
		Lang: "en", TranslateTo: "vi",
		Title: "Standup", ContextTerms: []string{"Alice", "CatchM"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if f.authHeader != "Bearer test-key" {
		t.Errorf("auth = %q", f.authHeader)
	}
	if !strings.Contains(f.uploadRef, "catchm-abc12345") {
		t.Errorf("upload client_reference_id = %q", f.uploadRef)
	}
	// upload multipart carries the reference id
	if !strings.Contains(string(f.uploadBody), "catchm-abc12345") {
		t.Error("upload body missing client_reference_id")
	}
	// create body
	if f.createBody["model"] != "stt-async-v5" {
		t.Errorf("model = %v", f.createBody["model"])
	}
	if f.createBody["file_id"] != "file-1" {
		t.Errorf("file_id = %v", f.createBody["file_id"])
	}
	if f.createBody["enable_speaker_diarization"] != true || f.createBody["enable_language_identification"] != true {
		t.Errorf("diarization/language id flags = %v/%v", f.createBody["enable_speaker_diarization"], f.createBody["enable_language_identification"])
	}
	if f.createBody["client_reference_id"] != "catchm-abc12345" {
		t.Errorf("client_reference_id = %v", f.createBody["client_reference_id"])
	}
	hints, _ := f.createBody["language_hints"].([]any)
	if len(hints) != 1 || hints[0] != "en" {
		t.Errorf("language_hints = %v", f.createBody["language_hints"])
	}
	tr, _ := f.createBody["translation"].(map[string]any)
	if tr == nil || tr["type"] != "one_way" || tr["target_language"] != "vi" {
		t.Errorf("translation = %v", f.createBody["translation"])
	}
	ctxObj, _ := f.createBody["context"].(map[string]any)
	if ctxObj == nil {
		t.Fatalf("context missing: %v", f.createBody["context"])
	}
	general, _ := ctxObj["general"].([]any)
	if len(general) != 1 {
		t.Fatalf("context.general = %v", general)
	}
	item, _ := general[0].(map[string]any)
	if item["key"] != "topic" || item["value"] != "Standup" {
		t.Errorf("context.general[0] = %v", item)
	}
	terms, _ := ctxObj["terms"].([]any)
	if len(terms) != 2 || terms[0] != "Alice" || terms[1] != "CatchM" {
		t.Errorf("context.terms = %v", terms)
	}
	if f.pollCount != 2 {
		t.Errorf("polls = %d, want 2 (one pending)", f.pollCount)
	}
	// utterances
	if len(us) != 1 {
		t.Fatalf("utterances = %d (%+v)", len(us), us)
	}
	u := us[0]
	if u.Text != "Hello world" || u.Translated != "xin chao" {
		t.Errorf("text/translated = %q / %q", u.Text, u.Translated)
	}
	if u.Speaker != "Speaker 1" {
		t.Errorf("speaker = %q", u.Speaker)
	}
	if u.Lang != "en" {
		t.Errorf("lang = %q", u.Lang)
	}
	if !u.At.Equal(time.Time{}.Add(0)) {
		t.Errorf("at = %v", u.At)
	}
	// cleanup happened
	ds := f.deletes()
	if len(ds) != 2 || ds[0] != "transcription:tr-1" || ds[1] != "file:file-1" {
		t.Errorf("deletes = %v", ds)
	}
}

func TestSonioxAsyncTwoWay(t *testing.T) {
	f := newFakeSoniox(t)
	e := newTestAsyncEngine(f, t)
	audio := writeAudio(t)
	_, err := e.TranscribeFile(context.Background(), audio, Options{Lang: "en", LanguageB: "vi", TwoWay: true, TranslateTo: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	hints, _ := f.createBody["language_hints"].([]any)
	if len(hints) != 2 || hints[0] != "en" || hints[1] != "vi" {
		t.Errorf("language_hints = %v", f.createBody["language_hints"])
	}
	tr, _ := f.createBody["translation"].(map[string]any)
	if tr == nil || tr["type"] != "two_way" || tr["language_a"] != "en" || tr["language_b"] != "vi" {
		t.Errorf("translation = %v", f.createBody["translation"])
	}
	if _, has := f.createBody["context"]; has {
		t.Error("context should be omitted when empty")
	}
}

func TestSonioxAsyncGrouping(t *testing.T) {
	tokens := []sonioxAsyncToken{
		// utterance 1: speaker 1, ends with "."
		{sonioxToken: sonioxToken{Text: "Hi there.", Speaker: "spk_1", TranslationStatus: "original", Language: "en"}, StartMS: 0, EndMS: 500},
		{sonioxToken: sonioxToken{Text: " hello", TranslationStatus: "translation", Language: "es"}, StartMS: 0, EndMS: 0},
		// utterance 2: new sentence, same speaker, big gap
		{sonioxToken: sonioxToken{Text: "Later", Speaker: "spk_1", TranslationStatus: "original", Language: "en"}, StartMS: 4000, EndMS: 4300},
		// utterance 3: speaker change
		{sonioxToken: sonioxToken{Text: "Bien", Speaker: "2", TranslationStatus: "original", Language: "es"}, StartMS: 4400, EndMS: 4900},
		{sonioxToken: sonioxToken{Text: " ok", Speaker: "2", TranslationStatus: "original", Language: "es"}, StartMS: 4901, EndMS: 5100},
	}
	us := groupTokens(tokens)
	if len(us) != 3 {
		t.Fatalf("utterances = %d (%+v)", len(us), us)
	}
	if us[0].Text != "Hi there." || us[0].Speaker != "Speaker 1" || us[0].Translated != " hello" {
		t.Errorf("u0 = %+v", us[0])
	}
	if us[1].Text != "Later" || us[1].Speaker != "Speaker 1" {
		t.Errorf("u1 = %+v", us[1])
	}
	if !us[1].At.Equal(time.Time{}.Add(4 * time.Second)) {
		t.Errorf("u1 at = %v", us[1].At)
	}
	if us[2].Text != "Bien ok" || us[2].Speaker != "Speaker 2" || us[2].Lang != "es" {
		t.Errorf("u2 = %+v", us[2])
	}
}

func TestSonioxAsyncDeleteOnError(t *testing.T) {
	for _, fail := range []string{"create", "poll", "fetch"} {
		t.Run(fail, func(t *testing.T) {
			f := newFakeSoniox(t)
			f.failPath = fail
			e := newTestAsyncEngine(f, t)
			audio := writeAudio(t)
			_, err := e.TranscribeFile(context.Background(), audio, Options{})
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Errorf("error leaks key: %v", err)
			}
			// The file was uploaded, so it must be deleted; the
			// transcription only exists past "create".
			ds := f.deletes()
			wantTr := fail != "create"
			gotTr := false
			gotFile := false
			for _, d := range ds {
				gotTr = gotTr || d == "transcription:tr-1"
				gotFile = gotFile || d == "file:file-1"
			}
			if gotTr != wantTr || !gotFile {
				t.Errorf("deletes = %v (want transcription=%v, file=true)", ds, wantTr)
			}
		})
	}
}

func TestSonioxAsyncDeleteOnCancel(t *testing.T) {
	f := newFakeSoniox(t)
	f.pendingPolls = 100 // never completes
	e := newTestAsyncEngine(f, t)
	audio := writeAudio(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := e.TranscribeFile(ctx, audio, Options{})
	if err == nil {
		t.Fatal("expected cancel error")
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(f.deletes()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	ds := f.deletes()
	if len(ds) != 2 {
		t.Fatalf("deletes after cancel = %v, want transcription and file", ds)
	}
}

func TestSonioxAsyncAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"status_code":401,"error_type":"unauthenticated","message":"Incorrect API key provided."}`)
	}))
	defer srv.Close()
	e := NewSonioxAsyncEngine("bad-key", srv.URL, WithSonioxAsyncClient(srv.Client()))
	_, err := e.TranscribeFile(context.Background(), writeAudio(t), Options{})
	if err == nil || err.Error() != "Soniox rejected the API key. Check it in Settings → Transcription." {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "bad-key") {
		t.Error("error leaks key")
	}
}

func TestSonioxAsyncDefaultBaseURL(t *testing.T) {
	e := NewSonioxAsyncEngine("k", "")
	if e.baseURL != "https://api.soniox.com" {
		t.Errorf("baseURL = %q", e.baseURL)
	}
}

func TestSonioxAsyncCleanupStale(t *testing.T) {
	old := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 9, 28, 11, 59, 0, 0, time.UTC)
	var deleted []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/v1/transcriptions" && r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"transcriptions":[
				{"id":"tr-old","status":"completed","created_at":%q,"client_reference_id":"catchm-x"},
				{"id":"tr-recent","status":"completed","created_at":%q,"client_reference_id":"catchm-y"},
				{"id":"tr-other","status":"completed","created_at":%q,"client_reference_id":"other"}
			],"next_page_cursor":null}`, old.Format(time.RFC3339), recent.Format(time.RFC3339), old.Format(time.RFC3339))
		case r.URL.Path == "/v1/files" && r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"files":[
				{"id":"f-old","filename":"catchm-a.wav","size":1,"created_at":%q,"client_reference_id":"catchm-a"},
				{"id":"f-recent","filename":"catchm-b.wav","size":1,"created_at":%q,"client_reference_id":""}
			],"next_page_cursor":null}`, old.Format(time.RFC3339), recent.Format(time.RFC3339))
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	e := NewSonioxAsyncEngine("k", srv.URL, WithSonioxAsyncClient(srv.Client()),
		WithSonioxAsyncClock(func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }, nil))
	if err := e.CleanupStale(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"/v1/transcriptions/tr-old", "/v1/files/f-old"}
	if len(deleted) != len(want) {
		t.Fatalf("deleted = %v, want %v", deleted, want)
	}
	for i := range want {
		if deleted[i] != want[i] {
			t.Fatalf("deleted = %v, want %v", deleted, want)
		}
	}
}
