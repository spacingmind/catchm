package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/summarize"
)

// flowsMeeting creates a meeting with a two-line live transcript and an
// analysis.json made from it.
func flowsMeeting(t *testing.T, store *meeting.Store) *meeting.Meeting {
	t.Helper()
	m, err := store.Start("Refund walkthrough", time.Now().UTC(), meeting.StartOptions{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"The customer submits the refund form.", "Then the agent checks the order."} {
		if _, err := store.Append(m.ID, meeting.Utterance{Speaker: "Rosa", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(summarize.Analysis{
		Version:   1,
		Generated: summarize.GeneratedInfo{Transcript: "live"},
		Summary:   "How refunds work.",
		Topics:    []summarize.Topic{{ID: "t1", Title: "Refunds", Key: "topic:refunds", Evidence: []int64{1}}},
		People:    []summarize.Person{{Key: "person:rosa", Name: "Rosa", Speaker: "Rosa"}},
	})
	if err := store.WriteAnalysis(m.ID, raw); err != nil {
		t.Fatal(err)
	}
	return m
}

// fakeOllamaFlows is an Ollama with qwen3.5:9b installed whose chat
// replies are reply; it counts chat calls.
func fakeOllamaFlows(t *testing.T, s *Server, reply string) *atomic.Int32 {
	t.Helper()
	var chats atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"qwen3.5:9b"}]}`)
			return
		}
		chats.Add(1)
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 2 || !strings.Contains(req.Messages[1].Content, "[2] Rosa: Then the agent checks the order.") ||
			!strings.Contains(req.Messages[1].Content, "person:rosa") {
			t.Errorf("flows request = %+v", req)
		}
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": reply}})
	}))
	t.Cleanup(srv.Close)
	s.cfg.Translate.Host = srv.URL
	s.cfg.Summary.OllamaModel = "qwen3.5:9b"
	return &chats
}

type flowsBody struct {
	Analysis summarize.Analysis `json:"analysis"`
	MindMap  summarize.MindNode `json:"mindmap"`
}

func TestFindFlowsEndpoint(t *testing.T) {
	s, store := newTestServer(t)
	m := flowsMeeting(t, store)
	fakeOllamaFlows(t, s, `{"flows":[{"id":"f1","name":"Refund handling","actor":"Rosa","evidence":[1,2,9],"steps":[
		{"id":"s1","order":1,"text":"Submit the refund form","evidence":[1],"next":[{"to":"s2","condition":""},{"to":"s7"}]},
		{"id":"s2","order":2,"text":"Check the order","actor":"agent","evidence":[2],"next":[]}]}]}`)

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/flows", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var v flowsBody
	decode(t, w, &v)
	f := v.Analysis.Flows
	if len(f) != 1 || f[0].Actor != "person:rosa" || len(f[0].Evidence) != 2 || len(f[0].Steps) != 2 ||
		len(f[0].Steps[0].Next) != 1 || f[0].Steps[1].Actor != "agent" {
		t.Fatalf("flows = %+v", f)
	}
	if v.Analysis.Summary != "How refunds work." || v.MindMap.Label != m.Title || v.MindMap.Children[0].Ref != "topic:t1" {
		t.Fatalf("analysis = %+v, mindmap = %+v", v.Analysis, v.MindMap)
	}
	// Stored: GET /analysis serves the same flows.
	w = do(t, s, "GET", "/api/meetings/"+m.ID+"/analysis", "")
	var got flowsBody
	decode(t, w, &got)
	if len(got.Analysis.Flows) != 1 || got.Analysis.Flows[0].Name != "Refund handling" {
		t.Fatalf("stored flows = %+v", got.Analysis.Flows)
	}
}

func TestFindFlowsEndpointNoFlows(t *testing.T) {
	s, store := newTestServer(t)
	m := flowsMeeting(t, store)
	fakeOllamaFlows(t, s, `{"flows": []}`)

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/flows", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"flows":[]`) {
		t.Fatalf("flows not an empty array: %s", w.Body.String())
	}
}

func TestFindFlowsEndpointNeedsAnalysis(t *testing.T) {
	s, store := newTestServer(t)
	chats := fakeOllamaFlows(t, s, `{"flows": []}`)
	m, err := store.Start("No summary", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/flows", "")
	var v refineErrBody
	decode(t, w, &v)
	if w.Code != http.StatusConflict || v.Error != "Create a summary first." || v.Kind != "" || chats.Load() != 0 {
		t.Fatalf("status = %d, body = %+v, chats = %d", w.Code, v, chats.Load())
	}
	if w := do(t, s, "POST", "/api/meetings/nope/flows", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown meeting: status = %d", w.Code)
	}
}

// TestFindFlowsEndpointNotReady: CatchM Cloud has no flows pass, and a
// down Ollama is caught by the readiness check; neither sends anything.
func TestFindFlowsEndpointNotReady(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*testing.T, *Server)
		reason string
	}{
		{"catchm cloud", func(t *testing.T, s *Server) {
			var hits atomic.Int32
			cloud := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
			t.Cleanup(func() {
				cloud.Close()
				if hits.Load() != 0 {
					t.Errorf("cloud called %d times", hits.Load())
				}
			})
			s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(cloud.URL) }
			s.cloud = &cloudState{email: "a@b.c", sessionToken: "tok"}
			s.cfg.Summary.Provider = summarize.ProviderCloud
			s.cfg.Summary.CloudConsent = true
		}, "Finding flows isn't available with CatchM Cloud yet. Use your own AI provider (or Ollama) in Settings → Summaries."},
		{"ollama down", func(t *testing.T, s *Server) {
			down := httptest.NewServer(http.NotFoundHandler())
			down.Close()
			s.cfg.Translate.Host = down.URL
			s.cfg.Summary.OllamaModel = "qwen3.5:9b"
		}, "Ollama isn't running. Install or start Ollama, then check again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store := newTestServer(t)
			m := flowsMeeting(t, store)
			tc.setup(t, s)
			w := do(t, s, "POST", "/api/meetings/"+m.ID+"/flows", "")
			var v refineErrBody
			decode(t, w, &v)
			if w.Code != http.StatusServiceUnavailable || v.Kind != "summary_not_ready" || v.Error != tc.reason {
				t.Fatalf("status = %d, body = %+v", w.Code, v)
			}
		})
	}
}

// TestFindFlowsSurviveRefine: refining (and so re-summarizing) a meeting
// keeps the flows found earlier.
func TestFindFlowsSurviveRefine(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	upWhisper(t, s)
	stubBatchEngineFor(s, []meeting.Utterance{{Speaker: "S1", Text: "Submit the form, then check the order."}})
	sum := &flowsSummarizer{fakeSummarizer: fakeSummarizer{ex: summarize.Analysis{Summary: "Refunds."}}, flows: []summarize.Flow{{
		Name: "Refund handling", Evidence: []int64{1},
		Steps: []summarize.FlowStep{{Text: "Submit the form"}, {Text: "Check the order"}},
	}}}
	stubSummarizer(t, s, sum)
	if w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", ""); w.Code != http.StatusOK {
		t.Fatalf("refine: status = %d, body = %s", w.Code, w.Body.String())
	}
	if w := do(t, s, "POST", "/api/meetings/"+m.ID+"/flows", ""); w.Code != http.StatusOK {
		t.Fatalf("flows: status = %d, body = %s", w.Code, w.Body.String())
	}
	sum.ex.Summary = "Refunds, again."
	if w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", ""); w.Code != http.StatusOK {
		t.Fatalf("second refine: status = %d, body = %s", w.Code, w.Body.String())
	}
	w := do(t, s, "GET", "/api/meetings/"+m.ID+"/analysis", "")
	var v flowsBody
	decode(t, w, &v)
	if v.Analysis.Summary != "Refunds, again." || len(v.Analysis.Flows) != 1 || v.Analysis.Flows[0].Name != "Refund handling" {
		t.Fatalf("analysis = %+v", v.Analysis)
	}
}

// flowsSummarizer is a fakeSummarizer that also finds flows.
type flowsSummarizer struct {
	fakeSummarizer
	flows []summarize.Flow
}

func (f *flowsSummarizer) FindFlows(ctx context.Context, _ summarize.Input, _ summarize.Analysis) ([]summarize.Flow, error) {
	return f.flows, ctx.Err()
}

// The meeting page reads the transcript an analysis's evidence refers
// to: ?source=v2 serves the batch transcript, "best" prefers it and
// falls back to live, and the response says which one it served.
func TestTranscriptSource(t *testing.T) {
	s, store := newTestServer(t)
	m, err := store.Start("Sources", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"live one", "live two"} {
		if _, err := store.Append(m.ID, meeting.Utterance{Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	type page struct {
		Utterances []meeting.Utterance `json:"utterances"`
		Source     string              `json:"source"`
	}
	get := func(q string) (int, page) {
		t.Helper()
		w := do(t, s, "GET", "/api/meetings/"+m.ID+"/transcript?cursor=-1"+q, "")
		var p page
		if w.Code == http.StatusOK {
			decode(t, w, &p)
		}
		return w.Code, p
	}
	if code, p := get("&source=best"); code != 200 || p.Source != "live" || len(p.Utterances) != 2 {
		t.Fatalf("best without v2 = %d %+v", code, p)
	}
	if code, p := get("&source=v2"); code != 200 || p.Source != "v2" || len(p.Utterances) != 0 {
		t.Fatalf("v2 before refine = %d %+v", code, p)
	}
	if err := store.WriteBatchTranscript(m.ID, []meeting.Utterance{{Text: "refined"}}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"&source=v2", "&source=best"} {
		if code, p := get(q); code != 200 || p.Source != "v2" || len(p.Utterances) != 1 || p.Utterances[0].Text != "refined" {
			t.Fatalf("%s = %d %+v", q, code, p)
		}
	}
	if code, p := get(""); code != 200 || p.Source != "live" || len(p.Utterances) != 2 {
		t.Fatalf("default = %d %+v", code, p)
	}
	if code, _ := get("&source=nope"); code != http.StatusBadRequest {
		t.Fatalf("bad source = %d", code)
	}
}
