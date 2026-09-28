package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/catchm/internal/summarize"
)

// stubSummarizer answers each meeting by title.
type stubSummarizer struct {
	byTitle map[string]func() (summarize.Analysis, error)
	inputs  []summarize.Input
}

func (s *stubSummarizer) Summarize(_ context.Context, in summarize.Input) (summarize.Analysis, error) {
	s.inputs = append(s.inputs, in)
	return s.byTitle[in.Title]()
}

func TestRun(t *testing.T) {
	ok := standup()
	ok.Title = "ok"
	bad := standup()
	bad.Title = "bad-json"
	down := standup()
	down.Title = "down"
	sum := &stubSummarizer{byTitle: map[string]func() (summarize.Analysis, error){
		"ok": func() (summarize.Analysis, error) {
			a := perfectStandup()
			a.Decisions[0].Evidence = []int64{1, 42} // Validate drops 42
			return a, nil
		},
		"bad-json": func() (summarize.Analysis, error) {
			return summarize.Analysis{}, &summarize.Error{Kind: summarize.KindFailed, Message: "no usable summary"}
		},
		"down": func() (summarize.Analysis, error) {
			return summarize.Analysis{}, &summarize.Error{Kind: summarize.KindNotReady, Message: "Ollama isn't running"}
		},
	}}
	var seen []string
	rs := Run(context.Background(), sum, []NamedGold{{"ok", ok}, {"bad", bad}, {"down", down}},
		func(name string) { seen = append(seen, name) })

	if strings.Join(seen, ",") != "ok,bad,down" || len(rs) != 3 {
		t.Fatalf("progress %v, %d results", seen, len(rs))
	}
	if in := sum.inputs[0]; in.Language != "en" || !strings.Contains(in.Transcript, "[3] Priya: I'll write the flag rollout plan") {
		t.Errorf("input = %+v, want seq-prefixed transcript", in)
	}

	r := rs[0]
	if r.Report == nil || r.Error != "" || r.Report.JSONValid != 1 || r.Issues == 0 {
		t.Fatalf("ok result = %+v", r)
	}
	want(t, "ok evidence_valid", r.Report.EvidenceValid, 1)

	r = rs[1]
	if r.Report == nil || r.Report.JSONValid != 0 || r.Error == "" {
		t.Fatalf("bad-json result = %+v, want scored with json_valid 0", r)
	}
	want(t, "bad-json decision_recall", r.Report.DecisionRecall, 0)
	wantNil(t, "bad-json decision_precision", r.Report.DecisionPrecision)

	if r = rs[2]; r.Report != nil || !strings.Contains(r.Error, "Ollama isn't running") {
		t.Fatalf("down result = %+v, want not evaluated", r)
	}

	out := NewOutput("ollama", "m", rs)
	if out.Evaluated != 2 || out.Aggregate == nil || out.Aggregate.JSONValid != 0.5 {
		t.Fatalf("output = %+v", out)
	}
	var tbl bytes.Buffer
	if err := out.WriteTable(&tbl); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"DEC_R", "MEAN (2/3)", "not evaluated", "errors:", "down: Ollama isn't running"} {
		if !strings.Contains(tbl.String(), s) {
			t.Errorf("table missing %q:\n%s", s, tbl.String())
		}
	}
	var js bytes.Buffer
	if err := out.WriteJSON(&js); err != nil {
		t.Fatal(err)
	}
	var back Output
	if err := json.Unmarshal(js.Bytes(), &back); err != nil || back.Evaluated != 2 || len(back.Files) != 3 {
		t.Fatalf("json round trip: %v %+v", err, back)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sum := &stubSummarizer{}
	rs := Run(ctx, sum, []NamedGold{{"a", standup()}}, nil)
	if len(sum.inputs) != 0 || rs[0].Report != nil || rs[0].Error == "" {
		t.Fatalf("cancelled run = %+v", rs)
	}
}
