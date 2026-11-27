package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
)

func newTestStore(t *testing.T) *meeting.Store {
	t.Helper()
	s, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fixtureMeeting(t *testing.T, s *meeting.Store) *meeting.Meeting {
	t.Helper()
	m, err := s.Start("Q3 Roadmap Sync", time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC), meeting.StartOptions{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []meeting.Utterance{
		{Speaker: "alex", Text: "We freeze the scope by Friday."},
		{Speaker: "sam", Text: "I'll prepare the demo and send the timeline."},
	} {
		if _, err := s.Append(m.ID, u); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

const goodReply = `{"summary":"Synced on Q3.","participants":["alex","sam"],"decisions":[{"text":"Freeze scope by Friday","scope":""}],"action_items":[{"text":"Prepare demo","owner":"sam","due":""}]}`

func kindOf(err error) Kind {
	var se *Error
	if errors.As(err, &se) {
		return se.Kind
	}
	return ""
}

func TestParseExtraction(t *testing.T) {
	ex, err := parseExtraction(`Here is the JSON: {"summary":"s","participants":["alex"],"decisions":[{"text":"freeze scope"}],"action_items":[{"text":"demo","owner":"sam"}]} done.`)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Summary != "s" || len(ex.Participants) != 1 || ex.Decisions[0].Text != "freeze scope" || ex.ActionItems[0].Owner != "sam" {
		t.Fatalf("extraction = %+v", ex)
	}

	// Fenced output parses too.
	if _, err := parseExtraction("```json\n{\"summary\":\"s\"}\n```"); err != nil {
		t.Fatal(err)
	}

	if _, err := parseExtraction("no json here"); err == nil {
		t.Fatal("expected error for non-JSON result")
	}
}

func TestParseReplyCleansAndSanitizes(t *testing.T) {
	a, err := parseReply("<think>{maybe}</think>\n```json\n" +
		`{"summary":" s ","participants":["alex","sam"],"decisions":["ship it"],` +
		`"action_items":[{"task":"demo","assignee":"sam","deadline":"Friday","evidence":"[2], 3","topic":null}],` +
		`"topics":[{"id":"t1","name":"Scope","key":"topic:scope","evidence":[1.0,"4"]}],` +
		`"entities":[{"name":"CatchM","type":"company","evidence":7}]}` +
		"\n```")
	if err != nil {
		t.Fatal(err)
	}
	if a.Summary != "s" || len(a.People) != 2 || a.People[1].Name != "sam" || a.Decisions[0].Text != "ship it" {
		t.Fatalf("analysis = %+v", a)
	}
	act := a.Actions[0]
	if act.Text != "demo" || act.Owner != "sam" || act.Due != "Friday" || fmt.Sprint(act.Evidence) != "[2 3]" {
		t.Fatalf("action = %+v", act)
	}
	if a.Topics[0].Title != "Scope" || fmt.Sprint(a.Topics[0].Evidence) != "[1 4]" {
		t.Fatalf("topic = %+v", a.Topics[0])
	}
	if a.Entities[0].Kind != "company" || fmt.Sprint(a.Entities[0].Evidence) != "[7]" {
		t.Fatalf("entity = %+v", a.Entities[0])
	}
	if !strings.Contains(strings.Join(a.Issues, "\n"), `field "action_items" renamed "actions"`) {
		t.Fatalf("issues = %q", a.Issues)
	}
	if _, err := parseReply(`{"summary":"","people":[]}`); !errors.Is(err, errEmptySummary) {
		t.Fatalf("empty summary: err = %v", err)
	}
}

func TestApplyExtraction(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	ex := Extraction{
		Summary:      "synced on Q3",
		Participants: []string{"alex", "sam"},
		Decisions:    []meeting.Decision{{Text: "Freeze scope by Friday"}},
		ActionItems:  []meeting.ActionItem{{Text: "Prepare demo", Owner: "sam"}},
	}
	if err := s.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		applyExtraction(mm, ex)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "synced on Q3" ||
		len(got.Participants) != 2 ||
		len(got.Decisions) != 1 || got.Decisions[0].Text != "Freeze scope by Friday" ||
		len(got.ActionItems) != 1 || got.ActionItems[0].Owner != "sam" {
		t.Fatalf("meeting = %+v", got)
	}
}

func TestLoadInputPrefersBatchTranscript(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)

	in, err := LoadInput(s, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Q3 Roadmap Sync" || in.Language != "en" || !strings.Contains(in.Transcript, "[1] alex: We freeze the scope by Friday.\n") {
		t.Fatalf("live input = %+v", in)
	}

	if err := s.WriteBatchTranscript(m.ID, []meeting.Utterance{{Speaker: "S1", Text: "refined line"}}); err != nil {
		t.Fatal(err)
	}
	in, err = LoadInput(s, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if in.Transcript != "[1] Speaker 1: refined line\n" { // engine labels read as "Speaker N"
		t.Fatalf("batch input transcript = %q", in.Transcript)
	}

	if _, err := LoadInput(s, "nope"); !errors.Is(err, meeting.ErrNotFound) {
		t.Fatalf("unknown id: err = %v", err)
	}
}

type stubSummarizer struct {
	ex    Analysis
	err   error
	calls int
	in    Input
}

func (f *stubSummarizer) Summarize(_ context.Context, in Input) (Analysis, error) {
	f.calls++
	f.in = in
	return f.ex, f.err
}

func TestSummarizeMeetingSetsRefined(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	sum := &stubSummarizer{ex: Analysis{Summary: "done", People: []Person{{Name: "alex"}}}}
	if err := SummarizeMeeting(context.Background(), s, m.ID, sum); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(m.ID)
	if !got.Refined || got.Summary != "done" || sum.in.Title != "Q3 Roadmap Sync" {
		t.Fatalf("meeting = %+v, input = %+v", got, sum.in)
	}

	// Provider errors pass through and leave the meeting unrefined.
	m2 := fixtureMeeting(t, s)
	boom := failed("nope", nil)
	if err := SummarizeMeeting(context.Background(), s, m2.ID, &stubSummarizer{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := s.Get(m2.ID); got.Refined {
		t.Fatal("refined set despite a failed summary")
	}

	// No transcript text: refined without calling the provider.
	empty, err := s.Start("Silent", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubSummarizer{}
	if err := SummarizeMeeting(context.Background(), s, empty.ID, stub); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(empty.ID); !got.Refined || stub.calls != 0 {
		t.Fatalf("empty meeting: refined = %v, calls = %d", got.Refined, stub.calls)
	}
	if _, err := s.ReadAnalysis(empty.ID); !errors.Is(err, meeting.ErrNoAnalysis) {
		t.Fatalf("empty meeting got an analysis: err = %v", err)
	}
}

func TestNewSelectsProviderAndGuardsConsent(t *testing.T) {
	cfg := &config.Config{}
	cfg.Translate.Host = "http://127.0.0.1:11434"
	cfg.Summary.OllamaModel = "qwen3.5:9b"
	if s, err := New(cfg, CloudAuth{}); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*Ollama); !ok {
		t.Fatalf("default provider = %T, want *Ollama", s)
	}

	cfg.Summary.Provider = ProviderOpenAI
	if _, err := New(cfg, CloudAuth{}); kindOf(err) != KindNotReady {
		t.Fatalf("openai without base url: err = %v", err)
	}
	cfg.LLM = config.LLMConfig{Preset: config.LLMCustom, BaseURL: "http://x/v1", StrongModel: "m", APIKey: "k"}
	if s, err := New(cfg, CloudAuth{}); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*OpenAI); !ok {
		t.Fatalf("openai provider = %T", s)
	}

	cfg.Summary.Provider = ProviderCloud
	if _, err := New(cfg, CloudAuth{BaseURL: "http://x", Token: "tok"}); kindOf(err) != KindConsentRequired {
		t.Fatalf("cloud without consent: err = %v", err)
	}
	cfg.Summary.CloudConsent = true
	if _, err := New(cfg, CloudAuth{BaseURL: "http://x"}); kindOf(err) != KindNotReady {
		t.Fatalf("cloud logged out: err = %v", err)
	}
	if s, err := New(cfg, CloudAuth{BaseURL: "http://x", Token: "tok"}); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*Cloud); !ok {
		t.Fatalf("cloud provider = %T", s)
	}

	cfg.Summary.Provider = "bogus"
	if _, err := New(cfg, CloudAuth{}); kindOf(err) != KindNotReady {
		t.Fatalf("unknown provider: err = %v", err)
	}
}

func testInput() Input {
	return Input{Title: "Q3 Roadmap Sync", Language: "en", Transcript: "[1] alex: We freeze the scope by Friday.\n[2] sam: I'll prepare the demo.\n"}
}

func TestOllamaChat(t *testing.T) {
	var got ollamaChatRequest
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_ = json.Unmarshal(body, &raw)
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": goodReply},
			"done":    true,
		})
	}))
	defer srv.Close()

	ex, err := (&Ollama{Host: srv.URL, Model: "qwen3.5:9b"}).Summarize(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if ex.Summary != "Synced on Q3." || len(ex.Actions) != 1 || ex.Actions[0].Owner != "sam" {
		t.Fatalf("analysis = %+v", ex)
	}
	if got.Model != "qwen3.5:9b" || got.Stream || got.Options.Temperature != 0 || got.Options.NumCtx < ollamaMinCtx {
		t.Fatalf("request = %+v", got)
	}
	format, ok := raw["format"].(map[string]any)
	if !ok || format["type"] != "object" || format["properties"] == nil {
		t.Fatalf("format is not the JSON schema: %v", raw["format"])
	}
	if opts, _ := raw["options"].(map[string]any); opts["num_ctx"] == nil || opts["temperature"] == nil {
		t.Fatalf("options missing explicit num_ctx/temperature: %v", raw["options"])
	}
	if !strings.Contains(got.Messages[1].Content, "We freeze the scope by Friday.") {
		t.Fatalf("transcript not inlined: %q", got.Messages[1].Content)
	}
}

func TestOllamaNumCtxCoversInput(t *testing.T) {
	user := strings.Repeat("word ", 20000) // ~25k estimated tokens
	n := ollamaNumCtx(extractSystem, user)
	if n < estimateTokens(user)+ollamaReplyTokens || n%1024 != 0 {
		t.Fatalf("num_ctx = %d for ~%d input tokens", n, estimateTokens(user))
	}
}

func TestOllamaErrorsAreClassified(t *testing.T) {
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"model \"qwen3.5:9b\" not found, try pulling it first"}`)
	}))
	defer missing.Close()
	_, err := (&Ollama{Host: missing.URL, Model: "qwen3.5:9b"}).Summarize(context.Background(), testInput())
	if kindOf(err) != KindNotReady {
		t.Fatalf("missing model: err = %v", err)
	}

	missing.Close()
	_, err = (&Ollama{Host: missing.URL, Model: "qwen3.5:9b"}).Summarize(context.Background(), testInput())
	if kindOf(err) != KindNotReady {
		t.Fatalf("ollama down: err = %v", err)
	}
}

// TestRetryOnceThenFail: an invalid reply is retried exactly once; a
// second invalid reply is summary_failed, a valid one succeeds.
func TestRetryOnceThenFail(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replies []string
		wantErr bool
	}{
		{"fails twice", []string{"not json", `{"summary":""}`}, true},
		{"recovers", []string{"not json", goodReply}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := int(calls.Add(1)) - 1
				reply := tc.replies[min(n, len(tc.replies)-1)]
				json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": reply}})
			}))
			defer srv.Close()
			_, err := (&Ollama{Host: srv.URL, Model: "m"}).Summarize(context.Background(), testInput())
			if calls.Load() != 2 {
				t.Fatalf("calls = %d, want 2", calls.Load())
			}
			if tc.wantErr && kindOf(err) != KindFailed {
				t.Fatalf("err = %v, want summary_failed", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestOpenAIFallsBackFromJSONSchema: a server rejecting json_schema and
// then json_object is retried with the next response_format, down to
// none; the fenced reply still parses.
func TestOpenAIFallsBackFromJSONSchema(t *testing.T) {
	var formats []string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		rf, _ := req["response_format"].(map[string]any)
		typ, _ := rf["type"].(string)
		formats = append(formats, typ)
		switch typ {
		case "json_schema":
			js, _ := rf["json_schema"].(map[string]any)
			if js["name"] != "meeting_extraction" || js["strict"] != true || js["schema"] == nil {
				t.Errorf("json_schema = %v", js)
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"response_format type json_schema is not supported"}}`)
		case "json_object":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Unsupported value: 'response_format.type'"}}`)
		default:
			json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "```json\n" + goodReply + "\n```"}},
			}})
		}
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL + "/v1/", Model: "gpt", APIKey: "sk-x"}
	ex, err := o.Summarize(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if ex.Summary != "Synced on Q3." {
		t.Fatalf("extraction = %+v", ex)
	}
	if strings.Join(formats, ",") != "json_schema,json_object," {
		t.Fatalf("formats tried = %q", formats)
	}
	if auth != "Bearer sk-x" {
		t.Fatalf("authorization = %q", auth)
	}

	// The accepted level sticks for later calls.
	formats = nil
	if _, err := o.Summarize(context.Background(), testInput()); err != nil {
		t.Fatal(err)
	}
	if len(formats) != 1 || formats[0] != "" {
		t.Fatalf("second run re-probed: %q", formats)
	}
}

func TestOpenAIErrorsAreClassified(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   Kind
	}{
		{http.StatusUnauthorized, KindNotReady},
		{http.StatusBadRequest, KindFailed}, // not a response_format complaint
		{http.StatusInternalServerError, KindFailed},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			fmt.Fprint(w, `{"error":{"message":"bad model"}}`)
		}))
		_, err := (&OpenAI{BaseURL: srv.URL, Model: "m"}).Summarize(context.Background(), testInput())
		srv.Close()
		if kindOf(err) != tc.want {
			t.Fatalf("status %d: err = %v, want %s", tc.status, err, tc.want)
		}
	}
}

func TestCloudSummarize(t *testing.T) {
	var got cloudRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/summarize":
			json.NewDecoder(r.Body).Decode(&got)
			fmt.Fprint(w, strings.TrimSuffix(goodReply, "}")+`,"remaining_today":4}`)
		case "/v1/summarize/quota":
			fmt.Fprint(w, `{"remaining_today":4,"limit":5,"reset_at":"2026-09-29T00:00:00Z"}`)
		default:
			t.Errorf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}
	in := testInput()
	in.Guests = []string{"Bao Tran", "x@y.co", strings.Repeat("a", cloudMaxGuestChars+1)}
	ex, err := c.Summarize(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Guests) != 1 || got.Guests[0] != "Bao Tran" {
		t.Errorf("guests sent = %v", got.Guests)
	}
	if ex.Summary != "Synced on Q3." || got.Title != "Q3 Roadmap Sync" || got.Language != "en" || !strings.Contains(got.Transcript, "freeze") {
		t.Fatalf("extraction = %+v, request = %+v", ex, got)
	}
	q, err := c.Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if q != (Quota{RemainingToday: 4, Limit: 5, ResetAt: "2026-09-29T00:00:00Z"}) {
		t.Fatalf("quota = %+v", q)
	}
}

func TestCloudQuotaExceededCarriesResetAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"quota_exceeded","reset_at":"2026-09-29T00:00:00Z"}`)
	}))
	defer srv.Close()
	_, err := (&Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}).Summarize(context.Background(), testInput())
	var se *Error
	if !errors.As(err, &se) || se.Kind != KindQuotaExceeded || se.ResetAt != "2026-09-29T00:00:00Z" {
		t.Fatalf("err = %#v", err)
	}
}

func TestCloudErrorsAreClassified(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   Kind
	}{
		{http.StatusUnauthorized, KindNotReady},
		{http.StatusRequestEntityTooLarge, KindFailed},
		{http.StatusServiceUnavailable, KindFailed},
		{http.StatusInternalServerError, KindFailed},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			fmt.Fprint(w, `{"error":"ai_unavailable"}`)
		}))
		_, err := (&Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}).Summarize(context.Background(), testInput())
		srv.Close()
		if kindOf(err) != tc.want {
			t.Fatalf("status %d: err = %v, want %s", tc.status, err, tc.want)
		}
	}
}

func TestCloudWithoutConsentSendsNothing(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	_, err := (&Cloud{BaseURL: srv.URL, Token: "tok"}).Summarize(context.Background(), testInput())
	if kindOf(err) != KindConsentRequired || hits.Load() != 0 {
		t.Fatalf("err = %v, hits = %d", err, hits.Load())
	}
}

func TestSplitTranscript(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("speaker %d: line number %d of the meeting\n", i, i)
	}
	transcript := strings.Join(lines, "")
	chunks := splitTranscript(transcript, 200, runeUTF16)
	if len(chunks) < 2 || strings.Join(chunks, "") != transcript {
		t.Fatalf("chunks lose or reorder text: %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 200 || !strings.HasSuffix(c, "\n") {
			t.Fatalf("chunk over budget or split mid-line: %q", c)
		}
	}

	// One over-budget line is cut at spaces, never losing text.
	long := strings.Repeat("abc ", 100)
	pieces := splitTranscript(long, 50, runeUTF16)
	if strings.Join(pieces, "") != long {
		t.Fatal("long line split loses text")
	}
	for _, p := range pieces {
		if len(p) > 50 {
			t.Fatalf("piece over budget: %d", len(p))
		}
	}

	// CJK text is costed per character, so it chunks sooner.
	if estimateTokens(strings.Repeat("会", 100)) < 100 || estimateTokens(strings.Repeat("a", 100)) > 30 {
		t.Fatal("token heuristic off")
	}
}

// TestChunkAndMerge: a transcript over the per-call budget is summarized
// per chunk, then merged by a final call whose duplicates are removed.
func TestChunkAndMerge(t *testing.T) {
	var extracts, merges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		reply := `{"summary":"part","participants":["alex"],"decisions":[{"text":"Freeze scope","scope":""}],"action_items":[{"text":"Prepare demo","owner":"","due":""}]}`
		if req.Messages[0].Content == mergeSystem {
			merges.Add(1)
			if !strings.Contains(req.Messages[1].Content, `"summary": "part"`) {
				t.Errorf("merge input lacks the partial extractions: %q", req.Messages[1].Content)
			}
			reply = `{"summary":"whole meeting","participants":["sam"],"decisions":[{"text":"Freeze scope","scope":""},{"text":"freeze scope.","scope":"Q3"}],"action_items":[{"text":"Prepare demo","owner":"sam","due":""}]}`
		} else {
			extracts.Add(1)
			if !strings.Contains(req.Messages[1].Content, "This is part ") {
				t.Errorf("chunk prompt lacks its part number")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": reply}})
	}))
	defer srv.Close()

	var b strings.Builder
	for b.Len() < (ollamaBudgetTokens*4)*3 {
		b.WriteString("alex: we keep talking about the scope freeze and the demo timeline.\n")
	}
	in := testInput()
	in.Transcript = b.String()

	ex, err := (&Ollama{Host: srv.URL, Model: "m"}).Summarize(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if extracts.Load() < 3 || merges.Load() != 1 {
		t.Fatalf("extract calls = %d, merge calls = %d", extracts.Load(), merges.Load())
	}
	if ex.Summary != "whole meeting" || len(ex.Decisions) != 1 || len(ex.Actions) != 1 {
		t.Fatalf("merged = %+v", ex)
	}
	if len(ex.People) != 2 {
		t.Fatalf("people the merge dropped not restored: %v", ex.People)
	}
}

func TestCloudChunksMergeWithoutExtraCall(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/summarize/quota" {
			fmt.Fprint(w, `{"remaining_today":5,"limit":5,"reset_at":"2026-09-29T00:00:00Z"}`)
			return
		}
		n := calls.Add(1)
		var req cloudRequest
		json.NewDecoder(r.Body).Decode(&req)
		if costOf(req.Transcript, runeUTF16) > cloudChunkChars {
			t.Errorf("chunk of %d chars over the cloud cap", len(req.Transcript))
		}
		fmt.Fprintf(w, `{"summary":"part %d.","participants":["alex"],"decisions":[{"text":"Freeze scope"}],"action_items":[{"text":"Prepare demo","owner":"sam"}],"remaining_today":3,"provider":"workers-ai"}`, n)
	}))
	defer srv.Close()

	in := testInput()
	in.Transcript = longCloudTranscript(2)
	ex, err := (&Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}).Summarize(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("calls = %d, want exactly one per chunk", n)
	}
	if !strings.HasPrefix(ex.Summary, "part 1.") || len(ex.Decisions) != 1 || len(ex.Actions) != 1 || len(ex.People) != 1 ||
		ex.Actions[0].Owner != "sam" || len(ex.Topics) != 0 {
		t.Fatalf("merged = %+v", ex)
	}
}

// longCloudTranscript is just over chunks-1 cloud chunks long, so it
// splits into exactly chunks pieces.
func longCloudTranscript(chunks int) string {
	line := "alex: talking about the scope freeze and the timeline.\n"
	return strings.Repeat(line, (chunks-1)*cloudChunkChars/len(line)+10)
}

func TestCloudQuotaTooLowForChunksFailsEarly(t *testing.T) {
	var summarizeCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/summarize/quota" {
			fmt.Fprint(w, `{"remaining_today":1,"limit":5,"reset_at":"2026-09-29T00:00:00Z"}`)
			return
		}
		summarizeCalls.Add(1)
	}))
	defer srv.Close()
	in := testInput()
	in.Transcript = longCloudTranscript(2)
	_, err := (&Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}).Summarize(context.Background(), in)
	var se *Error
	if !errors.As(err, &se) || se.Kind != KindQuotaExceeded || se.ResetAt != "2026-09-29T00:00:00Z" {
		t.Fatalf("err = %#v", err)
	}
	if summarizeCalls.Load() != 0 {
		t.Fatal("spent quota on a summary that couldn't finish")
	}
}

func TestCloudRateLimitIsNotQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"too many attempts"}`)
	}))
	defer srv.Close()
	_, err := (&Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}).Summarize(context.Background(), testInput())
	if kindOf(err) != KindFailed {
		t.Fatalf("err = %v, want summary_failed", err)
	}
}

func TestCloud413RechunksToMaxChars(t *testing.T) {
	const maxChars = 1000
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/summarize/quota" {
			fmt.Fprint(w, `{"remaining_today":5,"limit":5,"reset_at":"x"}`)
			return
		}
		calls.Add(1)
		var req cloudRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Transcript) > maxChars {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			fmt.Fprintf(w, `{"error":"transcript too large","max_chars":%d}`, maxChars)
			return
		}
		fmt.Fprint(w, goodReply)
	}))
	defer srv.Close()
	in := testInput()
	in.Transcript = strings.Repeat("alex: a line about the plan.\n", 60) // ~1.8k chars
	ex, err := (&Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}).Summarize(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || ex.Summary == "" {
		t.Fatalf("calls = %d (want 1 rejected + 2 re-cut), ex = %+v", calls.Load(), ex)
	}

	// A 413 without a usable max_chars is summary_failed.
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		fmt.Fprint(w, `{"error":"transcript too large"}`)
	}))
	defer bare.Close()
	_, err = (&Cloud{BaseURL: bare.URL, Token: "tok", Consent: true}).Summarize(context.Background(), testInput())
	var se *Error
	if !errors.As(err, &se) || se.Kind != KindFailed {
		t.Fatalf("bare 413: err = %#v", err)
	}
}

// The summary sees confirmed speaker names and the guests' names, never
// their emails.
func TestLoadInputNamesAndGuests(t *testing.T) {
	s := newTestStore(t)
	s.SetMe(func() string { return "Minh" })
	m, err := s.Start("Sync", time.Now(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []meeting.Utterance{{Speaker: "1", Text: "hi"}, {Speaker: "2", Text: "yo"}, {Source: "mic", Text: "me here"}} {
		if _, err := s.Append(m.ID, u); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetSpeakerNames(m.ID, map[string]string{"2": "Bao Tran"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGuests(m.ID, []meeting.Guest{{Name: "Bao Tran", Email: "bao@acme.com"}, {Email: "carol@acme.com"}}); err != nil {
		t.Fatal(err)
	}
	in, err := LoadInput(s, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[1] Speaker 1: hi\n[2] Bao Tran: yo\n[3] Minh: me here\n"; in.Transcript != want {
		t.Errorf("transcript = %q", in.Transcript)
	}
	if len(in.Guests) != 1 || in.Guests[0] != "Bao Tran" {
		t.Errorf("guests = %v", in.Guests)
	}
	prompt := extractUser(in, 1, 1)
	if !strings.Contains(prompt, "Invited: Bao Tran.") || strings.Contains(prompt, "@") {
		t.Errorf("prompt = %q", prompt)
	}
}
