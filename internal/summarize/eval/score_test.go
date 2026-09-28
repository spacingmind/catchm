package eval

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/catchm/internal/summarize"
)

func standup() Gold {
	return Gold{
		Title: "Standup",
		Lang:  "en",
		Transcript: []GoldUtterance{
			{Seq: 1, Speaker: "Dana", Text: "We ship the onboarding redesign behind a feature flag."},
			{Seq: 2, Speaker: "Dana", Text: "And we drop the tablet layout from this sprint."},
			{Seq: 3, Speaker: "Priya", Text: "I'll write the flag rollout plan by Thursday."},
			{Seq: 4, Speaker: "Marcus", Text: "I'll merge the settings crash fix tomorrow."},
			{Seq: 5, Speaker: "Jo", Text: "Nice weather today."},
		},
		Decisions: []string{
			"Ship the onboarding redesign behind a feature flag",
			"Drop the tablet layout from this sprint",
		},
		Actions: []GoldAction{
			{Text: "Write the flag rollout plan", Owner: "Priya", Due: "2026-10-08"},
			{Text: "Merge the settings crash fix", Owner: "Marcus", Due: "2026-10-07"},
		},
	}
}

func perfectStandup() summarize.Analysis {
	return summarize.Analysis{
		Decisions: []summarize.AnalysisDecision{
			{ID: "d1", Text: "Ship the onboarding redesign behind a feature flag", Evidence: []int64{1}},
			{ID: "d2", Text: "Drop the tablet layout from this sprint", Evidence: []int64{2}},
		},
		Actions: []summarize.AnalysisAction{
			{ID: "a1", Text: "Write the flag rollout plan", Owner: "person:priya", Due: "2026-10-08", Evidence: []int64{3}},
			{ID: "a2", Text: "Merge the settings crash fix", Owner: "Marcus", Due: "2026-10-07", Evidence: []int64{4}},
		},
		People: []summarize.Person{{Key: "person:priya", Name: "Priya"}, {Key: "person:marcus", Name: "Marcus"}},
	}
}

func want(t *testing.T, name string, got *float64, w float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %.2f", name, w)
		return
	}
	if math.Abs(*got-w) > 1e-9 {
		t.Errorf("%s = %.4f, want %.2f", name, *got, w)
	}
}

func wantNil(t *testing.T, name string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %.4f, want nil (undefined)", name, *got)
	}
}

func TestScorePerfect(t *testing.T) {
	r := Score(standup(), perfectStandup())
	if r.JSONValid != 1 || r.Unsupported != 0 {
		t.Fatalf("json_valid=%v unsupported=%v", r.JSONValid, r.Unsupported)
	}
	for name, v := range map[string]*float64{
		"decision_recall": r.DecisionRecall, "decision_precision": r.DecisionPrecision,
		"action_recall": r.ActionRecall, "action_precision": r.ActionPrecision,
		"owner_accuracy": r.OwnerAccuracy, "due_accuracy": r.DueAccuracy,
		"evidence_valid": r.EvidenceValid,
	} {
		want(t, name, v, 1)
	}
}

func TestScorePartial(t *testing.T) {
	a := summarize.Analysis{
		// A paraphrase of the first decision; the second is missed.
		Decisions: []summarize.AnalysisDecision{
			{Text: "The team decided to ship the redesigned onboarding behind a feature flag.", Evidence: []int64{1}},
		},
		Actions: []summarize.AnalysisAction{
			// Matched, right owner, wrong date.
			{Text: "Priya writes the rollout plan for the flag", Owner: "Priya", Due: "2026-10-09", Evidence: []int64{3}},
			// Matched, no owner, no date.
			{Text: "Merge the settings crash fix", Evidence: []int64{4}},
		},
	}
	r := Score(standup(), a)
	want(t, "decision_recall", r.DecisionRecall, 0.5)
	want(t, "decision_precision", r.DecisionPrecision, 1)
	want(t, "action_recall", r.ActionRecall, 1)
	want(t, "owner_accuracy", r.OwnerAccuracy, 0.5)
	want(t, "due_accuracy", r.DueAccuracy, 0)
	want(t, "evidence_valid", r.EvidenceValid, 1)
}

func TestScoreHallucinated(t *testing.T) {
	g := Gold{
		Title:      "Coffee",
		Transcript: []GoldUtterance{{Seq: 1, Text: "How was the trip to the mountains?"}, {Seq: 2, Text: "Cold but beautiful."}},
	}
	a := summarize.Analysis{
		Decisions: []summarize.AnalysisDecision{{Text: "Plan another trip to the mountains", Evidence: []int64{1}}},
	}
	r := Score(g, a)
	wantNil(t, "decision_recall", r.DecisionRecall)
	want(t, "decision_precision", r.DecisionPrecision, 0)
	wantNil(t, "action_recall", r.ActionRecall)
	want(t, "action_precision", r.ActionPrecision, 1)
	wantNil(t, "owner_accuracy", r.OwnerAccuracy)
	wantNil(t, "due_accuracy", r.DueAccuracy)

	// Saying nothing about a meeting with nothing to say is perfect.
	r = Score(g, summarize.Analysis{Summary: "A catch-up about a trip."})
	want(t, "decision_precision", r.DecisionPrecision, 1)
	want(t, "action_precision", r.ActionPrecision, 1)
	wantNil(t, "evidence_valid", r.EvidenceValid)
}

func TestScoreBadEvidence(t *testing.T) {
	a := perfectStandup()
	a.Decisions[0].Evidence = []int64{1, 99} // 99 isn't a transcript line
	a.Decisions[1].Evidence = []int64{5}     // unrelated line
	a.Actions[0].Evidence = nil              // unsupported
	r := Score(standup(), a)
	want(t, "evidence_valid", r.EvidenceValid, 0.25)
	if r.Unsupported != 1 {
		t.Errorf("unsupported = %v, want 1", r.Unsupported)
	}
	// Evidence doesn't affect matching.
	want(t, "decision_recall", r.DecisionRecall, 1)
}

func TestScoreOwnerViaAlias(t *testing.T) {
	g := Gold{
		Transcript: []GoldUtterance{{Seq: 1, Text: "Anh Hùng hoàn thành API thanh toán trước thứ Sáu."}},
		Actions:    []GoldAction{{Text: "Hoàn thành API thanh toán", Owner: "Hùng"}},
	}
	people := []summarize.Person{
		{Key: "person:hung", Name: "Hung", Aliases: []string{"anh Hùng"}},
		{Key: "person:lan", Name: "Lan"},
	}
	for _, tc := range []struct {
		owner string
		ok    bool
	}{
		{"person:hung", true}, // key → alias
		{"anh Hùng", true},    // alias itself (honorific dropped)
		{"HÙNG", true},        // case-insensitive
		{"Hung", true},        // name → alias
		{"person:lan", false},
		{"", false},
	} {
		a := summarize.Analysis{
			Actions: []summarize.AnalysisAction{{Text: "Hoàn thành API thanh toán", Owner: tc.owner, Evidence: []int64{1}}},
			People:  people,
		}
		w := 0.0
		if tc.ok {
			w = 1
		}
		want(t, "owner_accuracy("+tc.owner+")", Score(g, a).OwnerAccuracy, w)
	}
}

func TestScoreVietnamese(t *testing.T) {
	g := Gold{
		Lang: "vi",
		Transcript: []GoldUtterance{
			{Seq: 1, Speaker: "Lan", Text: "Vậy mình hoãn tính năng xuất PDF sang sprint 15 nhé."},
			{Seq: 2, Speaker: "Thảo", Text: "Em sẽ gửi bản thiết kế màn hình thanh toán vào thứ Tư."},
		},
		Decisions: []string{"Hoãn tính năng xuất PDF sang sprint 15"},
		Actions:   []GoldAction{{Text: "Gửi bản thiết kế màn hình thanh toán", Owner: "Thảo", Due: "2026-10-07"}},
	}
	a := summarize.Analysis{
		Decisions: []summarize.AnalysisDecision{
			{Text: "Nhóm quyết định hoãn xuất PDF sang sprint 15.", Evidence: []int64{1}},
		},
		Actions: []summarize.AnalysisAction{
			{Text: "Thảo gửi thiết kế màn hình thanh toán", Owner: "person:thao", Due: "2026-10-07", Evidence: []int64{2}},
		},
		People: []summarize.Person{{Key: "person:thao", Name: "Thảo"}},
	}
	r := Score(g, a)
	want(t, "decision_recall", r.DecisionRecall, 1)
	want(t, "action_recall", r.ActionRecall, 1)
	want(t, "owner_accuracy", r.OwnerAccuracy, 1)
	want(t, "due_accuracy", r.DueAccuracy, 1)
	want(t, "evidence_valid", r.EvidenceValid, 1)

	// Diacritics are kept: "bàn" (table) is not "bán" (sell).
	if s := similarity(tokenSet("bán hàng online"), tokenSet("bàn làm việc")); s != 0 {
		t.Errorf("similarity across diacritics = %v, want 0", s)
	}
}

func TestTokens(t *testing.T) {
	got := strings.Join(tokens("The team decided: ship v4.2, NOW! Chị Lan chốt là xuất PDF nhé."), " ")
	if want := "ship v4 2 now lan xuất pdf"; got != want {
		t.Errorf("tokens = %q, want %q", got, want)
	}
}

func TestAggregate(t *testing.T) {
	one, half := 1.0, 0.5
	agg := Aggregate([]Report{
		{JSONValid: 1, DecisionRecall: &one, OwnerAccuracy: &half, Unsupported: 2},
		{JSONValid: 0, DecisionRecall: &half, Unsupported: 0},
	})
	if agg.JSONValid != 0.5 || agg.Unsupported != 1 {
		t.Errorf("json_valid=%v unsupported=%v", agg.JSONValid, agg.Unsupported)
	}
	want(t, "decision_recall", agg.DecisionRecall, 0.75)
	want(t, "owner_accuracy", agg.OwnerAccuracy, 0.5) // nil skipped
	wantNil(t, "due_accuracy", agg.DueAccuracy)
	if got := Aggregate(nil); got.DecisionRecall != nil || got.JSONValid != 0 {
		t.Errorf("Aggregate(nil) = %+v", got)
	}
}

func TestLoadGoldSet(t *testing.T) {
	golds, err := Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(golds) != 5 {
		t.Fatalf("got %d gold files, want 5", len(golds))
	}
	langs := map[string]bool{}
	var noItems, withFlow bool
	for _, ng := range golds {
		g := ng.Gold
		langs[g.Lang] = true
		if n := len(g.Transcript); n < 15 || n > 40 {
			t.Errorf("%s: %d utterances, want 15-40", ng.Name, n)
		}
		if g.Title == "" || g.Lang == "" {
			t.Errorf("%s: missing title or lang", ng.Name)
		}
		if len(g.Decisions) == 0 && len(g.Actions) == 0 {
			noItems = true
		}
		if len(g.Decisions) == 1 && len(g.Actions) == 0 {
			withFlow = true
		}
		// Scoring the gold against itself, citing the best-supporting
		// line, must be perfect: every gold item is findable in its
		// transcript.
		r := Score(g, goldAsAnalysis(g))
		for name, v := range map[string]*float64{
			"decision_recall": r.DecisionRecall, "action_recall": r.ActionRecall,
			"owner_accuracy": r.OwnerAccuracy, "due_accuracy": r.DueAccuracy,
			"evidence_valid": r.EvidenceValid,
		} {
			if v != nil && *v != 1 {
				t.Errorf("%s: self-score %s = %.2f, want 1", ng.Name, name, *v)
			}
		}
	}
	if !langs["en"] || !langs["vi"] || !noItems || !withFlow {
		t.Errorf("gold set coverage: langs=%v no-items=%v process=%v", langs, noItems, withFlow)
	}
}

// goldAsAnalysis turns g's expected items into an Analysis, each citing
// the utterance sharing the most words with it.
func goldAsAnalysis(g Gold) summarize.Analysis {
	best := func(text string) []int64 {
		want := tokenSet(text)
		var seq int64 = -1
		top := 0
		for _, u := range g.Transcript {
			if n := intersect(want, tokenSet(u.Text)); n > top {
				seq, top = u.Seq, n
			}
		}
		if seq < 0 {
			return nil
		}
		return []int64{seq}
	}
	var a summarize.Analysis
	for _, d := range g.Decisions {
		a.Decisions = append(a.Decisions, summarize.AnalysisDecision{Text: d, Evidence: best(d)})
	}
	for _, x := range g.Actions {
		a.Actions = append(a.Actions, summarize.AnalysisAction{Text: x.Text, Owner: x.Owner, Due: x.Due, Evidence: best(x.Text)})
	}
	return a
}

func TestLoadRejectsBadGold(t *testing.T) {
	for name, body := range map[string]string{
		"unknown field": `{"title":"x","lang":"en","transcript":[{"seq":1,"text":"hi"}],"decisons":[]}`,
		"empty":         `{"title":"x","lang":"en","transcript":[]}`,
		"duplicate seq": `{"title":"x","lang":"en","transcript":[{"seq":1,"text":"a"},{"seq":1,"text":"b"}]}`,
		"bad due":       `{"title":"x","lang":"en","transcript":[{"seq":1,"text":"a"}],"actions":[{"text":"a","due":"Friday"}]}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "g.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Errorf("%s: Load succeeded, want an error", name)
		}
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("empty dir: Load succeeded, want an error")
	}
}
