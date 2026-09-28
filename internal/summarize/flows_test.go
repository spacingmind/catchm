package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spacingmind/catchm/internal/meeting"
)

func TestValidateFlowsRules(t *testing.T) {
	people := []Person{{Key: "person:rosa", Name: "Rosa", Speaker: "S1"}}
	in := []Flow{
		{ID: "f1", Name: " Refund handling ", Actor: "rosa", Evidence: []int64{3, 99}, Steps: []FlowStep{
			{ID: "a", Order: 30, Text: "Approve", Evidence: []int64{6}},
			{ID: "b", Order: 10, Text: "Submit the form", Actor: "S1", Evidence: []int64{3, 42},
				Next: []FlowNext{{To: "20"}, {To: "zz"}, {To: "b"}}},
			{ID: "c", Order: 20, Text: "Check the order", Actor: "person:omar",
				Next: []FlowNext{{To: "a", Condition: "under $50"}, {To: "A", Condition: "under $50"}}},
		}},
		// Same id: renamed. No edges: linked in order. No flow evidence:
		// taken from its steps.
		{ID: "f1", Name: "Escalation", Steps: []FlowStep{
			{Order: 1, Text: "Escalate", Evidence: []int64{7}},
			{Order: 2, Text: "Lead decides", Evidence: []int64{8}},
		}},
		// One valid step: dropped.
		{Name: "Too short", Steps: []FlowStep{{Text: "Only step"}, {Text: " "}}},
	}
	flows, issues := ValidateFlows(in, people, seqSet(3, 6, 7, 8))
	if len(flows) != 2 {
		t.Fatalf("flows = %+v", flows)
	}
	f := flows[0]
	if f.ID != "f1" || f.Name != "Refund handling" || f.Actor != "person:rosa" || !slices.Equal(f.Evidence, []int64{3}) {
		t.Fatalf("flow = %+v", f)
	}
	var ids []string
	for i, s := range f.Steps {
		ids = append(ids, s.ID)
		if s.Order != i+1 {
			t.Fatalf("step %s order = %d, want %d", s.ID, s.Order, i+1)
		}
	}
	if strings.Join(ids, ",") != "b,c,a" {
		t.Fatalf("step order = %v", ids)
	}
	b, c, a := f.Steps[0], f.Steps[1], f.Steps[2]
	if !slices.Equal(b.Next, []FlowNext{{To: "c"}}) || b.Actor != "person:rosa" || !slices.Equal(b.Evidence, []int64{3}) {
		t.Fatalf("step b = %+v", b)
	}
	if !slices.Equal(c.Next, []FlowNext{{To: "a", Condition: "under $50"}}) || c.Actor != "omar" || c.Evidence == nil {
		t.Fatalf("step c = %+v", c)
	}
	if a.Next != nil {
		t.Fatalf("step a = %+v", a)
	}
	esc := flows[1]
	if esc.ID != "f2" || !slices.Equal(esc.Evidence, []int64{7, 8}) ||
		!slices.Equal(esc.Steps[0].Next, []FlowNext{{To: "s2"}}) || esc.Steps[1].Next != nil {
		t.Fatalf("escalation = %+v", esc)
	}
	for _, want := range []string{
		`flows[0].steps[b] next "20" resolved to "c"`,
		`flows[0].steps[b] next "zz" (unknown step)`,
		`flows[0].steps[b] next "b" (unknown step)`,
		`flows[0].steps[c] next "A" (duplicate edge)`,
		`flows[0] steps renumbered 1..3`,
		`flows[0] evidence 99`,
		`flows[0].steps[1] evidence 42`,
		`flows[0].steps[2] actor reference "person:omar"`,
		`flows[1] duplicate id "f1", renamed "f2"`,
		`flows[1] has no next edges, steps linked in order`,
		`flows[1] ("Escalation") missing evidence, set from its steps'`,
		`flows[2] ("Too short"): 1 valid step(s), fewer than 2`,
	} {
		if !hasIssue(issues, want) {
			t.Errorf("missing issue %q in %q", want, issues)
		}
	}

	// Nothing in, nothing out, and never nil.
	if flows, issues := ValidateFlows(nil, nil, nil); flows == nil || len(flows) != 0 || issues == nil {
		t.Fatalf("empty: %v %v", flows, issues)
	}
}

func TestParseFlows(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      int
		wantErr   bool
	}{
		{"object", `{"flows":[{"name":"x","steps":[{"text":"a"},{"text":"b"}]}]}`, 1, false},
		{"empty", `{"flows": []}`, 0, false},
		{"null", `{"flows": null}`, 0, false},
		{"fenced alias", "```json\n{\"processes\":[{\"title\":\"x\",\"steps\":[\"a\",\"b\"]}]}\n```", 1, false},
		{"bare array", `[{"name":"x","steps":[]}]`, 1, false},
		{"no flows key", `{"summary":"s"}`, 0, true},
		{"not json", `no flows here`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flows, err := parseFlows(tc.raw)
			if (err != nil) != tc.wantErr || (err == nil && (flows == nil || len(flows) != tc.want)) {
				t.Fatalf("flows = %+v, err = %v", flows, err)
			}
		})
	}
	// Aliases, bare-string steps and numeric next targets are sanitized.
	flows, err := parseFlows(`{"flows":[{"process":"Refunds","steps":[{"step":"Submit","order":"1","next":[2]},"Check"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	s := flows[0].Steps
	if flows[0].Name != "Refunds" || len(s) != 2 || s[0].Text != "Submit" || s[0].Order != 1 ||
		s[0].Next[0].To != "2" || s[1].Text != "Check" {
		t.Fatalf("flows = %+v", flows)
	}
}

const flowsReply = `{"flows":[{"id":"f1","name":"Scope freeze","actor":"person:alex","trigger":"","evidence":[1],"steps":[
 {"id":"s1","order":1,"text":"Freeze the scope","actor":"alex","evidence":[1],"next":[{"to":"s2","condition":""}]},
 {"id":"s2","order":2,"text":"Prepare the demo","actor":"sam","evidence":[2],"next":[]}]}]}`

// flowsAnalysis is the context a flows call gets in these tests.
var flowsAnalysis = Analysis{
	People: []Person{{Key: "person:alex", Name: "alex"}, {Key: "person:sam", Name: "sam", Aliases: []string{"Sammy"}}},
	Topics: []Topic{{ID: "t1", Title: "Scope freeze"}},
}

func TestOllamaFindFlows(t *testing.T) {
	for _, tc := range []struct {
		reply string
		want  int
	}{{flowsReply, 1}, {`{"flows": []}`, 0}} {
		var req ollamaChatRequest
		var raw map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body json.RawMessage
			json.NewDecoder(r.Body).Decode(&body)
			_ = json.Unmarshal(body, &req)
			_ = json.Unmarshal(body, &raw)
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": tc.reply}})
		}))
		flows, err := (&Ollama{Host: srv.URL, Model: "qwen3.5:9b"}).FindFlows(context.Background(), testInput(), flowsAnalysis)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if flows == nil || len(flows) != tc.want {
			t.Fatalf("flows = %+v", flows)
		}
		if tc.want == 1 && (len(flows[0].Steps) != 2 || flows[0].Steps[0].Next[0].To != "s2") {
			t.Fatalf("flow = %+v", flows[0])
		}
		format, _ := raw["format"].(map[string]any)
		props, _ := format["properties"].(map[string]any)
		if props["flows"] == nil || props["summary"] != nil {
			t.Fatalf("format is not the flows schema: %v", raw["format"])
		}
		if req.Messages[0].Content != flowsSystem {
			t.Fatalf("system prompt = %q", req.Messages[0].Content)
		}
		user := req.Messages[1].Content
		for _, want := range []string{"- person:sam: sam (also Sammy)", "- t1: Scope freeze", "[1] alex: We freeze the scope by Friday."} {
			if !strings.Contains(user, want) {
				t.Fatalf("prompt lacks %q: %q", want, user)
			}
		}
	}
}

func TestOpenAIFindFlows(t *testing.T) {
	for _, tc := range []struct {
		reply string
		want  int
	}{{"```json\n" + flowsReply + "\n```", 1}, {`{"flows":[]}`, 0}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			rf, _ := req["response_format"].(map[string]any)
			js, _ := rf["json_schema"].(map[string]any)
			sch, _ := js["schema"].(map[string]any)
			props, _ := sch["properties"].(map[string]any)
			if js["name"] != "meeting_flows" || js["strict"] != true || props["flows"] == nil {
				t.Errorf("json_schema = %v", js)
			}
			json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
				{"message": map[string]string{"content": tc.reply}},
			}})
		}))
		flows, err := (&OpenAI{BaseURL: srv.URL, Model: "llama-3.3"}).FindFlows(context.Background(), testInput(), flowsAnalysis)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if flows == nil || len(flows) != tc.want {
			t.Fatalf("flows = %+v", flows)
		}
	}
}

// TestFlowSchemaStrictness: the loose schema requires only what a flow
// can't do without; the strict one requires every property.
func TestFlowSchemaStrictness(t *testing.T) {
	var loose, strict struct {
		Properties struct {
			Flows struct {
				Items struct {
					Required   []string `json:"required"`
					Properties struct {
						Steps struct {
							Items struct {
								Required []string `json:"required"`
							} `json:"items"`
						} `json:"steps"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"flows"`
		} `json:"properties"`
	}
	raw, _ := json.Marshal(flowsFormat.schema)
	sraw, _ := json.Marshal(flowsFormat.strict)
	if err := json.Unmarshal(raw, &loose); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sraw, &strict); err != nil {
		t.Fatal(err)
	}
	lf, sf := loose.Properties.Flows.Items, strict.Properties.Flows.Items
	if strings.Join(lf.Required, ",") != "id,name,evidence,steps" ||
		strings.Join(lf.Properties.Steps.Items.Required, ",") != "id,order,text,next" {
		t.Fatalf("loose required = %v / %v", lf.Required, lf.Properties.Steps.Items.Required)
	}
	if strings.Join(sf.Required, ",") != "id,name,actor,trigger,evidence,steps" ||
		strings.Join(sf.Properties.Steps.Items.Required, ",") != "id,order,text,actor,evidence,next" {
		t.Fatalf("strict required = %v / %v", sf.Required, sf.Properties.Steps.Items.Required)
	}
	if !strings.Contains(string(raw), `{"type":"object","properties":{"id":`) {
		t.Fatalf("schema not in declaration order: %s", raw)
	}
}

func TestFindFlowsRetriesOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reply := `{"summary": "not flows"}`
		if calls.Add(1) > 1 {
			reply = flowsReply
		}
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": reply}})
	}))
	defer srv.Close()
	flows, err := (&Ollama{Host: srv.URL, Model: "m"}).FindFlows(context.Background(), testInput(), Analysis{})
	if err != nil || len(flows) != 1 || calls.Load() != 2 {
		t.Fatalf("flows = %+v, err = %v, calls = %d", flows, err, calls.Load())
	}

	calls.Store(-10) // every reply invalid
	_, err = (&Ollama{Host: srv.URL, Model: "m"}).FindFlows(context.Background(), testInput(), Analysis{})
	var se *Error
	if !errors.As(err, &se) || se.Kind != KindFailed || se.Message != errNoFlows {
		t.Fatalf("err = %v", err)
	}
}

// TestFindFlowsChunksAndDedupes: an over-budget transcript is searched
// per chunk; flows found in several chunks under the same name are kept
// once (the one with the most steps), with unique ids.
func TestFindFlowsChunksAndDedupes(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !strings.Contains(req.Messages[1].Content, "This is part ") {
			t.Errorf("chunk prompt lacks its part number")
		}
		reply := `{"flows":[{"id":"f1","name":"Refund handling","steps":[{"id":"s1","order":1,"text":"a","next":[]},{"id":"s2","order":2,"text":"b","next":[]}]}]}`
		if calls.Add(1) == 2 {
			reply = `{"flows":[{"id":"f1","name":"refund  Handling.","steps":[{"id":"s1","order":1,"text":"a","next":[]},{"id":"s2","order":2,"text":"b","next":[]},{"id":"s3","order":3,"text":"c","next":[]}]},` +
				`{"id":"f2","name":"Escalation","steps":[{"id":"s1","order":1,"text":"x","next":[]},{"id":"s2","order":2,"text":"y","next":[]}]}]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": reply}})
	}))
	defer srv.Close()

	var b strings.Builder
	for b.Len() < (ollamaBudgetTokens*4)*3 {
		b.WriteString("[1] rosa: the refund goes to the team lead when it is over fifty dollars.\n")
	}
	in := testInput()
	in.Transcript = b.String()
	flows, err := (&Ollama{Host: srv.URL, Model: "m"}).FindFlows(context.Background(), in, Analysis{})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() < 3 || len(flows) != 2 || flows[0].ID != "f1" || len(flows[0].Steps) != 3 ||
		flows[1].ID != "f2" || flows[1].Name != "Escalation" {
		t.Fatalf("calls = %d, flows = %+v", calls.Load(), flows)
	}
}

// flowFinderFunc adapts a function to FlowFinder.
type flowFinderFunc func(context.Context, Input, Analysis) ([]Flow, error)

func (f flowFinderFunc) FindFlows(ctx context.Context, in Input, a Analysis) ([]Flow, error) {
	return f(ctx, in, a)
}

// TestFindMeetingFlowsAndResummary: the flows pass needs an analysis,
// stores validated flows in analysis.json, and a later summary keeps
// them, dropping evidence the transcript no longer has.
func TestFindMeetingFlowsAndResummary(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	ff := flowFinderFunc(func(_ context.Context, in Input, a Analysis) ([]Flow, error) {
		if !strings.Contains(in.Transcript, "[2] sam: ") || len(a.People) != 1 {
			t.Errorf("input = %+v, analysis = %+v", in, a)
		}
		return []Flow{{Name: "Scope freeze", Evidence: []int64{1, 2, 50}, Steps: []FlowStep{
			{Order: 1, Text: "Freeze the scope", Actor: "Alex", Evidence: []int64{1}},
			{Order: 2, Text: "Prepare the demo", Evidence: []int64{2}},
		}}}, nil
	})
	if _, err := FindMeetingFlows(context.Background(), s, m.ID, ff); !errors.Is(err, meeting.ErrNoAnalysis) {
		t.Fatalf("no analysis: err = %v", err)
	}

	sum := &stubSummarizer{ex: Analysis{Summary: "done", People: []Person{{Name: "alex"}}}}
	if err := SummarizeMeeting(context.Background(), s, m.ID, sum); err != nil {
		t.Fatal(err)
	}
	a, err := FindMeetingFlows(context.Background(), s, m.ID, ff)
	if err != nil {
		t.Fatal(err)
	}
	stored := readAnalysis(t, s, m.ID)
	for _, got := range []Analysis{a, stored} {
		if len(got.Flows) != 1 || got.Flows[0].ID != "f1" || !slices.Equal(got.Flows[0].Evidence, []int64{1, 2}) ||
			got.Flows[0].Steps[0].Actor != "person:alex" || !slices.Equal(got.Flows[0].Steps[0].Next, []FlowNext{{To: "s2"}}) ||
			got.Summary != "done" || !hasIssue(got.Issues, "flows[0] evidence 50") {
			t.Fatalf("analysis = %+v", got)
		}
	}

	// A second pass replaces the flows and their issues.
	none := flowFinderFunc(func(context.Context, Input, Analysis) ([]Flow, error) { return nil, nil })
	a, err = FindMeetingFlows(context.Background(), s, m.ID, none)
	if err != nil || a.Flows == nil || len(a.Flows) != 0 || hasIssue(a.Issues, "flows[") {
		t.Fatalf("empty pass: analysis = %+v, err = %v", a, err)
	}
	if _, err := FindMeetingFlows(context.Background(), s, m.ID, ff); err != nil {
		t.Fatal(err)
	}

	// Re-summarizing keeps the flows, even when the summary reply has
	// some of its own; evidence no longer in the transcript is dropped.
	if err := s.WriteBatchTranscript(m.ID, []meeting.Utterance{{Seq: 1, Speaker: "alex", Text: "We freeze the scope."}}); err != nil {
		t.Fatal(err)
	}
	sum.ex.Flows = []Flow{{Name: "Invented", Steps: []FlowStep{{Text: "a"}, {Text: "b"}}}}
	if err := SummarizeMeeting(context.Background(), s, m.ID, sum); err != nil {
		t.Fatal(err)
	}
	got := readAnalysis(t, s, m.ID)
	if len(got.Flows) != 1 || got.Flows[0].Name != "Scope freeze" || !slices.Equal(got.Flows[0].Evidence, []int64{1}) ||
		len(got.Flows[0].Steps[1].Evidence) != 0 || got.Generated.Transcript != "v2" {
		t.Fatalf("after re-summary: %+v", got)
	}
}

// TestFindMeetingFlowsReadsTheAnalysisTranscript: an analysis made from
// the live transcript is searched in the live transcript, even once a
// batch one exists.
func TestFindMeetingFlowsReadsTheAnalysisTranscript(t *testing.T) {
	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	if err := SummarizeMeeting(context.Background(), s, m.ID, &stubSummarizer{ex: Analysis{Summary: "done"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteBatchTranscript(m.ID, []meeting.Utterance{{Seq: 1, Text: "batch line"}}); err != nil {
		t.Fatal(err)
	}
	var transcript string
	ff := flowFinderFunc(func(_ context.Context, in Input, _ Analysis) ([]Flow, error) {
		transcript = in.Transcript
		return nil, nil
	})
	if _, err := FindMeetingFlows(context.Background(), s, m.ID, ff); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transcript, "We freeze the scope by Friday.") || strings.Contains(transcript, "batch line") {
		t.Fatalf("transcript = %q", transcript)
	}
}
