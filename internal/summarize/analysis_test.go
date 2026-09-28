package summarize

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

func seqSet(seqs ...int64) map[int64]bool {
	m := map[int64]bool{}
	for _, s := range seqs {
		m[s] = true
	}
	return m
}

func hasIssue(issues []string, substr string) bool {
	return slices.ContainsFunc(issues, func(s string) bool { return strings.Contains(s, substr) })
}

func TestValidateNormalizesKeysAndAliases(t *testing.T) {
	a := Analysis{
		Summary: "  s  ",
		People: []Person{
			{Key: "People:Long", Name: " Long ", Aliases: []string{"anh Long", "Long", " "}},
			{Name: "Minh", Speaker: "S2"},
			{Key: "person:long", Name: "Long N.", Speaker: "S1"},
			{Key: " ", Name: " "},
		},
		Entities: []Entity{
			{Name: "Acme", Kind: "Company"},
			{Name: "CatchM Cloud", Kind: "dự án", Evidence: []int64{7}},
			{Key: "product:catchm-cloud", Name: "CatchM Cloud"},
			{Name: "Lan", Kind: "human"},
			{Name: "Stand-up", Kind: "ritual"},
			{Key: "tool:Soniox API", Name: "Soniox"},
		},
		Topics: []Topic{
			{ID: "T1", Title: "Pricing page", Key: "Topic:Pricing Page"},
			{ID: "t2", Title: "Giá sản phẩm"},
			{ID: "t3", Title: "Pricing", Key: "topic:pricing-page", Evidence: []int64{5}},
			{ID: "t4", Title: "  "},
		},
	}
	issues := Validate(&a, seqSet(5, 7))

	if a.Summary != "s" || a.Version != AnalysisVersion {
		t.Fatalf("summary/version = %q/%d", a.Summary, a.Version)
	}
	if len(a.People) != 3 {
		t.Fatalf("people = %+v", a.People)
	}
	long := a.People[0]
	if long.Key != "person:long" || long.Name != "Long" || long.Speaker != "S1" ||
		!slices.Equal(long.Aliases, []string{"anh Long", "Long N."}) {
		t.Fatalf("merged person = %+v", long)
	}
	if a.People[1].Key != "person:minh" || a.People[2].Key != "person:lan" {
		t.Fatalf("derived/moved people = %+v", a.People[1:])
	}

	var kinds []string
	for _, e := range a.Entities {
		kinds = append(kinds, e.Key)
	}
	want := []string{"org:acme", "project:catchm-cloud", "product:catchm-cloud", "term:stand-up", "tool:soniox-api"}
	if !slices.Equal(kinds, want) {
		t.Fatalf("entity keys = %v, want %v", kinds, want)
	}

	if len(a.Topics) != 2 {
		t.Fatalf("topics = %+v", a.Topics)
	}
	if a.Topics[0].ID != "t1" || a.Topics[0].Key != "topic:pricing-page" || !slices.Equal(a.Topics[0].Evidence, []int64{5}) {
		t.Fatalf("merged topic = %+v", a.Topics[0])
	}
	if a.Topics[1].Key != "topic:gia-san-pham" {
		t.Fatalf("Vietnamese title key = %q", a.Topics[1].Key)
	}

	for _, want := range []string{
		`kind "company" mapped to "org"`,
		`kind "dự án" mapped to "project"`,
		`is a person, moved to people`,
		`kind "ritual" unknown, set to "term"`,
		`key "Topic:Pricing Page" normalized to "topic:pricing-page"`,
		`duplicate "topic:pricing-page" merged into topic "t1"`,
		`people[3]: no name or key`,
		`topics[3] ("t4"): empty title`,
	} {
		if !hasIssue(issues, want) {
			t.Errorf("missing issue %q in %q", want, issues)
		}
	}
	if !slices.Equal(a.Issues, issues) {
		t.Fatalf("a.Issues = %q, returned %q", a.Issues, issues)
	}
}

func TestValidateDropsBadRefsAndFixesDue(t *testing.T) {
	a := Analysis{
		Summary: "s",
		Issues:  []string{"auto-corrected: from parsing"},
		People:  []Person{{Key: "person:long", Name: "Long", Aliases: []string{"anh Long"}}},
		Topics: []Topic{
			{ID: "t1", Title: "Release", Key: "topic:release", Evidence: []int64{1, 99, 1}},
			{ID: "t2", Title: "Hiring", Key: "topic:hiring"},
		},
		Decisions: []AnalysisDecision{
			{ID: "d1", Text: "Ship Friday", Topic: "t1", Evidence: []int64{2, 42}},
			{ID: "d1", Text: "Freeze scope", Topic: "t9"},
			{ID: "", Text: "Hire two", Topic: "topic:hiring"},
			{ID: "d4", Text: "Keep budget", Topic: "release"},
			{ID: "d5", Text: "  "},
		},
		Actions: []AnalysisAction{
			{ID: "a1", Text: "Write notes", Owner: "anh Long", Due: "2026-10-02", Evidence: []int64{3}},
			{ID: "a2", Text: "Call vendor", Owner: "person:bob-smith", Due: "thứ Sáu tuần sau"},
			{ID: "a3", Text: "Book room", Owner: "Mai", Due: "2026-02-30", DueText: "cuối tháng"},
			{ID: "a4", Text: "Send deck", Owner: "PERSON:Long", Due: "Friday"},
		},
	}
	issues := Validate(&a, seqSet(1, 2, 3))

	if !slices.Equal(a.Topics[0].Evidence, []int64{1}) || !slices.Equal(a.Decisions[0].Evidence, []int64{2}) {
		t.Fatalf("evidence not filtered: %v / %v", a.Topics[0].Evidence, a.Decisions[0].Evidence)
	}
	if a.Topics[1].Evidence == nil || a.Decisions[1].Evidence == nil {
		t.Fatal("empty evidence must be [] not null")
	}
	var ids, topics []string
	for _, d := range a.Decisions {
		ids = append(ids, d.ID)
		topics = append(topics, d.Topic)
	}
	if !slices.Equal(ids, []string{"d1", "d2", "d3", "d4"}) || !slices.Equal(topics, []string{"t1", "", "t2", "t1"}) {
		t.Fatalf("decision ids = %v, topics = %v", ids, topics)
	}

	acts := a.Actions
	if acts[0].Owner != "person:long" || acts[0].Due != "2026-10-02" || acts[0].DueText != "" {
		t.Fatalf("a1 = %+v", acts[0])
	}
	if acts[1].Owner != "bob smith" || acts[1].Due != "" || acts[1].DueText != "thứ Sáu tuần sau" {
		t.Fatalf("a2 = %+v", acts[1])
	}
	if acts[2].Owner != "Mai" || acts[2].Due != "" || acts[2].DueText != "cuối tháng" {
		t.Fatalf("a3 = %+v", acts[2])
	}
	if acts[3].Owner != "person:long" || acts[3].DueText != "Friday" {
		t.Fatalf("a4 = %+v", acts[3])
	}

	for _, want := range []string{
		`topics[0] evidence 99 (not a transcript line)`,
		`decisions[0] evidence 42`,
		`decisions[1] duplicate id "d1", renamed "d2"`,
		`decisions[1] topic "t9" (unknown topic)`,
		`decisions[2] missing id, set to "d3"`,
		`decisions[3] topic "release" resolved to "t1"`,
		`decisions[4]: empty text`,
		`actions[0] owner "anh Long" resolved to "person:long"`,
		`actions[1] owner reference "person:bob-smith" (not a listed person; kept as text "bob smith")`,
		`actions[1] due "thứ Sáu tuần sau" is not YYYY-MM-DD, moved to due_text`,
		`actions[2] owner reference "Mai"`,
		`actions[2] due "2026-02-30" (not YYYY-MM-DD; due_text "cuối tháng" kept)`,
	} {
		if !hasIssue(issues, want) {
			t.Errorf("missing issue %q in %q", want, issues)
		}
	}
	if a.Issues[0] != "auto-corrected: from parsing" || len(a.Issues) != len(issues)+1 {
		t.Fatalf("issues not appended to a.Issues: %q", a.Issues)
	}

	// Validating again finds nothing new to fix but the kept-as-text
	// owners, and changes nothing.
	before, _ := json.Marshal(a)
	again := Validate(&a, seqSet(1, 2, 3))
	a.Issues = a.Issues[:len(a.Issues)-len(again)]
	after, _ := json.Marshal(a)
	if string(before) != string(after) {
		t.Fatalf("second Validate changed the analysis:\n%s\n%s", before, after)
	}
	for _, is := range again {
		if !strings.Contains(is, "owner reference") {
			t.Fatalf("second Validate issue %q", is)
		}
	}
}

func TestValidateNilSeqsDropsAllEvidenceAndFillsSlices(t *testing.T) {
	a := Analysis{Summary: "s", Decisions: []AnalysisDecision{{Text: "x", Evidence: []int64{1}}}}
	Validate(&a, nil)
	if len(a.Decisions[0].Evidence) != 0 {
		t.Fatalf("evidence = %v", a.Decisions[0].Evidence)
	}
	raw, _ := json.Marshal(a)
	for _, k := range []string{"topics", "decisions", "actions", "people", "entities", "flows", "issues"} {
		if strings.Contains(string(raw), `"`+k+`":null`) {
			t.Fatalf("%s is null: %s", k, raw)
		}
	}
}

func TestValidateFlows(t *testing.T) {
	a := Analysis{Summary: "s", Flows: []Flow{{
		Name: "Onboarding", Evidence: []int64{4, 8},
		Steps: []FlowStep{
			{ID: "s2", Order: 2, Text: "Verify email", Next: []FlowNext{{To: "s9"}}},
			{ID: "s1", Order: 1, Text: "Sign up", Next: []FlowNext{{To: "S2", Condition: " valid "}}},
			{Text: "Welcome"},
			{ID: "s4", Text: " "},
		},
	}, {Name: " "}}}
	issues := Validate(&a, seqSet(4))
	if len(a.Flows) != 1 || a.Flows[0].ID != "f1" || !slices.Equal(a.Flows[0].Evidence, []int64{4}) {
		t.Fatalf("flows = %+v", a.Flows)
	}
	steps := a.Flows[0].Steps
	if len(steps) != 3 || steps[0].ID != "s1" || steps[0].Next[0] != (FlowNext{To: "s2", Condition: "valid"}) ||
		steps[1].ID != "s2" || steps[1].Next != nil || steps[2].Order != 3 || steps[2].ID != "s3" {
		t.Fatalf("steps = %+v", steps)
	}
	if !hasIssue(issues, `next "s9" (unknown step)`) || !hasIssue(issues, "flows[1]: empty name") {
		t.Fatalf("issues = %q", issues)
	}
}

func TestBuildMindMap(t *testing.T) {
	a := Analysis{
		Topics: []Topic{{ID: "t1", Title: "Release"}, {ID: "t2", Title: "Hiring"}},
		Decisions: []AnalysisDecision{
			{ID: "d1", Text: "Ship Friday", Topic: "t1"},
			{ID: "d2", Text: "Freeze scope"},
		},
		Actions: []AnalysisAction{
			{ID: "a1", Text: "Write notes", Topic: "t1"},
			{ID: "a2", Text: "Post job", Topic: "t2"},
			{ID: "a3", Text: "Book room", Topic: "t9"},
		},
	}
	got := BuildMindMap(a, "Weekly sync")
	want := MindNode{Label: "Weekly sync", Children: []MindNode{
		{Label: "Release", Ref: "topic:t1", Children: []MindNode{
			{Label: "Ship Friday", Ref: "decision:d1"},
			{Label: "Write notes", Ref: "action:a1"},
		}},
		{Label: "Hiring", Ref: "topic:t2", Children: []MindNode{{Label: "Post job", Ref: "action:a2"}}},
		{Label: "Other", Children: []MindNode{
			{Label: "Freeze scope", Ref: "decision:d2"},
			{Label: "Book room", Ref: "action:a3"},
		}},
	}}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("mind map\n got %s\nwant %s", g, w)
	}

	// No unassigned items: no "Other" branch; no title: a default root.
	a.Decisions, a.Actions = a.Decisions[:1], a.Actions[:2]
	got = BuildMindMap(a, " ")
	if got.Label != "Meeting" || len(got.Children) != 2 {
		t.Fatalf("mind map = %+v", got)
	}
}

func TestExtractionProjectionKeepsStatuses(t *testing.T) {
	a := Analysis{
		Summary:   "sum",
		People:    []Person{{Key: "person:long", Name: "Long"}, {Key: "person:minh", Name: "Minh"}},
		Decisions: []AnalysisDecision{{ID: "d1", Text: "Ship", Scope: "Q3"}},
		Actions: []AnalysisAction{
			{ID: "a1", Text: "Write notes", Owner: "person:long", Due: "2026-10-02"},
			{ID: "a2", Text: "Call vendor", Owner: "Bob", DueText: "next Friday"},
			{ID: "a3", Text: "New task"},
		},
	}
	ex := a.Extraction()
	if ex.Summary != "sum" || !slices.Equal(ex.Participants, []string{"Long", "Minh"}) ||
		ex.Decisions[0] != (meeting.Decision{Text: "Ship", Scope: "Q3"}) {
		t.Fatalf("extraction = %+v", ex)
	}
	if ex.ActionItems[0] != (meeting.ActionItem{Text: "Write notes", Owner: "Long", Due: "2026-10-02"}) ||
		ex.ActionItems[1] != (meeting.ActionItem{Text: "Call vendor (due: next Friday)", Owner: "Bob"}) {
		t.Fatalf("action items = %+v", ex.ActionItems)
	}

	m := &meeting.Meeting{ActionItems: []meeting.ActionItem{
		{Text: "write notes.", Status: "done"},
		{Text: "Call vendor (due: next Friday)", Status: "in_progress"},
		{Text: "Dropped task", Status: "done"},
	}}
	applyExtraction(m, ex)
	var statuses []string
	for _, it := range m.ActionItems {
		statuses = append(statuses, it.Status)
	}
	if !slices.Equal(statuses, []string{"done", "in_progress", ""}) {
		t.Fatalf("statuses = %q", statuses)
	}
	if ex.ActionItems[0].Status != "" {
		t.Fatal("applyExtraction mutated the extraction")
	}
}

func TestLiftExtraction(t *testing.T) {
	a := liftExtraction(Extraction{
		Summary:      "s",
		Participants: []string{"Sam", "Alex", "sam"},
		Decisions:    []meeting.Decision{{Text: "Ship", Scope: "Q3"}},
		ActionItems: []meeting.ActionItem{
			{Text: "Demo", Owner: "sam", Due: "Friday"},
			{Text: "Notes", Owner: "Kim", Due: "2026-10-01"},
		},
	})
	if len(a.Topics) != 0 || len(a.People) != 2 || a.People[0].Key != "person:sam" || a.People[0].Name != "Sam" {
		t.Fatalf("lifted = %+v", a)
	}
	if a.Decisions[0].ID != "d1" || a.Decisions[0].Scope != "Q3" || len(a.Decisions[0].Evidence) != 0 {
		t.Fatalf("decision = %+v", a.Decisions[0])
	}
	if a.Actions[0].Owner != "person:sam" || a.Actions[0].DueText != "Friday" || a.Actions[0].Due != "" ||
		a.Actions[1].Owner != "Kim" || a.Actions[1].Due != "2026-10-01" {
		t.Fatalf("actions = %+v", a.Actions)
	}
	// A validated lift has nothing to fix but the unlisted owner.
	issues := Validate(&a, nil)
	if len(issues) != 1 || !strings.Contains(issues[0], `owner reference "Kim"`) {
		t.Fatalf("issues = %q", issues)
	}
	ex := a.Extraction()
	if ex.ActionItems[0].Owner != "Sam" || ex.ActionItems[0].Text != "Demo (due: Friday)" {
		t.Fatalf("round trip = %+v", ex.ActionItems)
	}
}

func TestMergeAnalysesByKeyRemapsIDs(t *testing.T) {
	parts := []Analysis{
		{
			Summary: "one.",
			Topics:  []Topic{{ID: "t1", Title: "Pricing", Key: "topic:pricing", Evidence: []int64{1}}},
			People:  []Person{{Key: "person:long", Name: "Long"}},
			Entities: []Entity{
				{Key: "project:catchm", Name: "CatchM", Kind: "project", Evidence: []int64{2}},
			},
			Decisions: []AnalysisDecision{{ID: "d1", Text: "Raise price", Topic: "t1", Evidence: []int64{1}}},
			Issues:    []string{"auto-corrected: p1"},
		},
		{
			Summary: "two.",
			Topics: []Topic{
				{ID: "t1", Title: "Giá", Key: "Topic:Pricing", Evidence: []int64{8}},
				{ID: "t2", Title: "Hiring", Key: "topic:hiring", Evidence: []int64{9}},
			},
			People:   []Person{{Key: "person:long", Name: "anh Long", Speaker: "S1"}},
			Entities: []Entity{{Key: "project:catchm", Name: "CatchM", Kind: "project", Evidence: []int64{7}}},
			Decisions: []AnalysisDecision{
				{ID: "d1", Text: "raise price.", Topic: "t1", Scope: "EU", Evidence: []int64{8}},
				{ID: "d2", Text: "Hire two", Topic: "t2"},
			},
			Actions: []AnalysisAction{{ID: "a1", Text: "Post job", Topic: "Hiring", Owner: "person:long"}},
		},
	}
	m := mergeAnalyses(renumber(parts))
	if m.Summary != "one. two." || len(m.Topics) != 2 || m.Topics[1].Key != "topic:hiring" {
		t.Fatalf("merged = %+v", m)
	}
	if !slices.Equal(m.Topics[0].Evidence, []int64{1, 8}) || m.Topics[1].ID != "t2" {
		t.Fatalf("topics = %+v", m.Topics)
	}
	if len(m.Decisions) != 2 || m.Decisions[0].Scope != "EU" || !slices.Equal(m.Decisions[0].Evidence, []int64{1, 8}) ||
		m.Decisions[1].Topic != "t2" || m.Decisions[1].ID != "d2" {
		t.Fatalf("decisions = %+v", m.Decisions)
	}
	if m.Actions[0].Topic != "t2" || m.Actions[0].ID != "a1" {
		t.Fatalf("actions = %+v", m.Actions)
	}
	if len(m.People) != 1 || m.People[0].Speaker != "S1" || !slices.Equal(m.People[0].Aliases, []string{"anh Long"}) {
		t.Fatalf("people = %+v", m.People)
	}
	if len(m.Entities) != 1 || !slices.Equal(m.Entities[0].Evidence, []int64{2, 7}) {
		t.Fatalf("entities = %+v", m.Entities)
	}
	if !slices.Equal(m.Issues, []string{"auto-corrected: p1"}) {
		t.Fatalf("issues = %q", m.Issues)
	}
	// parts are not mutated.
	if parts[1].Topics[1].ID != "t2" || parts[0].Topics[0].Evidence[0] != 1 || len(parts[0].Topics[0].Evidence) != 1 {
		t.Fatalf("parts mutated: %+v", parts)
	}
}

func TestSchemaOrderAndStrictness(t *testing.T) {
	raw, err := json.Marshal(analysisFormat.schema)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if i, j := strings.Index(s, `"decisions":{"type":"array","items":{"type":"object","properties":{"id"`), strings.Index(s, `"actions"`); i < 0 || j < i {
		t.Fatalf("schema not in declaration order: %s", s)
	}
	var loose, strict struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"items"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	json.Unmarshal(raw, &loose)
	sraw, _ := json.Marshal(analysisFormat.strict)
	json.Unmarshal(sraw, &strict)
	for name, p := range strict.Properties {
		if name == "summary" {
			continue
		}
		if len(p.Items.Required) != len(p.Items.Properties) {
			t.Errorf("strict %s requires %v of %d properties", name, p.Items.Required, len(p.Items.Properties))
		}
	}
	if slices.Contains(loose.Properties["actions"].Items.Required, "due_text") ||
		slices.Contains(loose.Properties["people"].Items.Required, "aliases") ||
		!slices.Contains(loose.Properties["decisions"].Items.Required, "evidence") {
		t.Fatalf("loose required = %+v", loose.Properties)
	}
}

// richReply is what a model following the analysis prompt answers for
// the fixture meeting (seqs 1 and 2), with the usual small-model slips:
// an uppercase key, a bad seq, an owner by name, a relative due.
const richReply = `{
 "summary": "The team froze Q3 scope and planned the demo.",
 "people": [{"name": "alex", "key": "person:alex", "speaker": "alex"},
            {"name": "sam", "key": "Person:Sam", "speaker": "sam", "aliases": []}],
 "topics": [{"id": "t1", "title": "Scope freeze", "key": "topic:scope-freeze", "evidence": [1]},
            {"id": "t2", "title": "Demo", "key": "topic:demo", "evidence": [2, 77]}],
 "decisions": [{"id": "d1", "text": "Freeze the scope by Friday", "scope": "", "topic": "t1", "evidence": [1]}],
 "actions": [{"id": "a1", "text": "Prepare the demo", "owner": "sam", "due": "Friday", "due_text": "", "topic": "t2", "evidence": [2]},
             {"id": "a2", "text": "Send the timeline", "owner": "person:sam", "due": "", "topic": "t2", "evidence": [2]}],
 "entities": [{"name": "Q3 roadmap", "kind": "project", "key": "project:q3-roadmap", "evidence": [1]}]
}`

func readAnalysis(t *testing.T, s *meeting.Store, id string) Analysis {
	t.Helper()
	raw, err := s.ReadAnalysis(id)
	if err != nil {
		t.Fatal(err)
	}
	var a Analysis
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

// checkRichAnalysis asserts the fixture meeting's analysis.json and
// frontmatter after summarizing it with richReply.
func checkRichAnalysis(t *testing.T, s *meeting.Store, id, provider, model string) {
	t.Helper()
	a := readAnalysis(t, s, id)
	want := GeneratedInfo{At: "2026-09-28T12:00:00Z", Provider: provider, Model: model, Transcript: "live", AnalysisLang: "en"}
	if a.Version != 1 || a.Generated != want {
		t.Fatalf("version/generated = %d %+v", a.Version, a.Generated)
	}
	if a.People[1].Key != "person:sam" || !slices.Equal(a.Topics[1].Evidence, []int64{2}) ||
		a.Actions[0].Owner != "person:sam" || a.Actions[0].DueText != "Friday" || a.Actions[1].Topic != "t2" {
		t.Fatalf("analysis not validated: %+v", a)
	}
	if !hasIssue(a.Issues, "evidence 77") || !hasIssue(a.Issues, `due "Friday"`) {
		t.Fatalf("issues = %q", a.Issues)
	}
	mm := BuildMindMap(a, "Q3 Roadmap Sync")
	if len(mm.Children) != 2 || len(mm.Children[1].Children) != 2 {
		t.Fatalf("mind map = %+v", mm)
	}
	m, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Refined || m.Summary != a.Summary || !slices.Equal(m.Participants, []string{"alex", "sam"}) ||
		len(m.ActionItems) != 2 || m.ActionItems[0] != (meeting.ActionItem{Text: "Prepare the demo (due: Friday)", Owner: "sam"}) {
		t.Fatalf("frontmatter = %+v", m)
	}
}

func fixClock(t *testing.T) {
	t.Helper()
	prev := now
	now = func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = prev })
}

func TestSummarizeMeetingWithOllamaWritesAnalysis(t *testing.T) {
	fixClock(t)
	var prompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		prompt = req.Messages[1].Content
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": richReply}})
	}))
	defer srv.Close()

	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	if err := SummarizeMeeting(context.Background(), s, m.ID, &Ollama{Host: srv.URL, Model: "qwen3.5:9b"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "[1] alex: We freeze the scope by Friday.\n[2] sam: ") {
		t.Fatalf("transcript lines lack seqs: %q", prompt)
	}
	checkRichAnalysis(t, s, m.ID, ProviderOllama, "qwen3.5:9b")

	// A later summary keeps the user's statuses.
	if err := s.SetActionItemStatus(m.ID, 1, "done"); err != nil {
		t.Fatal(err)
	}
	if err := SummarizeMeeting(context.Background(), s, m.ID, &Ollama{Host: srv.URL, Model: "qwen3.5:9b"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(m.ID); got.ActionItems[1].Status != "done" {
		t.Fatalf("status lost on re-summary: %+v", got.ActionItems)
	}
}

func TestSummarizeMeetingWithOpenAIWritesAnalysis(t *testing.T) {
	fixClock(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		rf, _ := req["response_format"].(map[string]any)
		js, _ := rf["json_schema"].(map[string]any)
		sch, _ := js["schema"].(map[string]any)
		props, _ := sch["properties"].(map[string]any)
		if props["topics"] == nil || props["entities"] == nil {
			t.Errorf("json_schema lacks the analysis fields: %v", sch)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"content": "```json\n" + richReply + "\n```"}},
		}})
	}))
	defer srv.Close()

	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	if err := SummarizeMeeting(context.Background(), s, m.ID, &OpenAI{BaseURL: srv.URL, Model: "llama-3.3"}); err != nil {
		t.Fatal(err)
	}
	checkRichAnalysis(t, s, m.ID, ProviderOpenAI, "llama-3.3")
}

func TestSummarizeMeetingWithCloudLiftsReply(t *testing.T) {
	fixClock(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.TrimSuffix(goodReply, "}")+`,"remaining_today":4}`)
	}))
	defer srv.Close()

	s := newTestStore(t)
	m := fixtureMeeting(t, s)
	if err := SummarizeMeeting(context.Background(), s, m.ID, &Cloud{BaseURL: srv.URL, Token: "tok", Consent: true}); err != nil {
		t.Fatal(err)
	}
	a := readAnalysis(t, s, m.ID)
	if a.Generated.Provider != ProviderCloud || len(a.Topics) != 0 || len(a.People) != 2 ||
		a.Actions[0].Owner != "person:sam" || len(a.Actions[0].Evidence) != 0 || len(a.Issues) != 0 {
		t.Fatalf("lifted analysis = %+v", a)
	}
	if mm := BuildMindMap(a, m.Title); len(mm.Children) != 1 || mm.Children[0].Label != "Other" {
		t.Fatalf("mind map = %+v", mm)
	}
	got, _ := s.Get(m.ID)
	if got.Summary != "Synced on Q3." || got.ActionItems[0] != (meeting.ActionItem{Text: "Prepare demo", Owner: "sam"}) {
		t.Fatalf("frontmatter = %+v", got)
	}
}
