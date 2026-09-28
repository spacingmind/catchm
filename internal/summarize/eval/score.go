package eval

import (
	"sort"
	"strings"
	"unicode"

	"github.com/spacingmind/catchm/internal/summarize"
)

// Report is one meeting's scores (or, from Aggregate, their means). A
// nil metric is undefined for that meeting, e.g. decision_recall when
// the gold has no decisions, and is skipped by Aggregate.
type Report struct {
	// JSONValid is 1 when the provider returned a usable Analysis, 0
	// when it failed to (the runner sets 0; Score always sets 1).
	JSONValid float64 `json:"json_valid"`
	// DecisionRecall: gold decisions matched by a predicted one.
	DecisionRecall *float64 `json:"decision_recall"`
	// DecisionPrecision: predicted decisions that match a gold one;
	// 1 when nothing was predicted.
	DecisionPrecision *float64 `json:"decision_precision"`
	// ActionRecall: gold actions matched by a predicted one.
	ActionRecall *float64 `json:"action_recall"`
	// ActionPrecision: predicted actions that match a gold one; 1 when
	// nothing was predicted. Beyond the plan's list, so an invented
	// action (the no-decisions meeting) shows up somewhere.
	ActionPrecision *float64 `json:"action_precision"`
	// OwnerAccuracy: matched actions whose gold has an owner and whose
	// predicted owner names the same person.
	OwnerAccuracy *float64 `json:"owner_accuracy"`
	// DueAccuracy: matched actions whose gold has a due date and whose
	// predicted due is that date.
	DueAccuracy *float64 `json:"due_accuracy"`
	// EvidenceValid: predicted decisions and actions whose evidence is
	// non-empty, cites only existing seqs, and whose cited utterances
	// share at least one content word with the item's text.
	EvidenceValid *float64 `json:"evidence_valid"`
	// Unsupported: predicted decisions and actions with no evidence.
	Unsupported float64 `json:"unsupported"`
	// FlowsOK is 1 when the flows pass found exactly the gold's
	// expect_flows flows (after validation), 0 when it found another
	// number or failed; nil when the gold sets no expectation or the
	// provider has no flows pass.
	FlowsOK *float64 `json:"flows_ok"`
}

// Score compares a against g. Items match when their token-set
// similarity (see similarity) is at least matchThreshold; each gold
// and predicted item matches at most once, best pairs first.
func Score(g Gold, a summarize.Analysis) Report {
	r := Report{JSONValid: 1}

	predD := make([]string, len(a.Decisions))
	for i, d := range a.Decisions {
		predD[i] = d.Text
	}
	dm := match(g.Decisions, predD)
	r.DecisionRecall = ratio(len(dm), len(g.Decisions))
	r.DecisionPrecision = precision(len(dm), len(predD))

	goldA := make([]string, len(g.Actions))
	for i, x := range g.Actions {
		goldA[i] = x.Text
	}
	predA := make([]string, len(a.Actions))
	for i, x := range a.Actions {
		predA[i] = x.Text
	}
	am := match(goldA, predA)
	r.ActionRecall = ratio(len(am), len(goldA))
	r.ActionPrecision = precision(len(am), len(predA))

	var owners, ownersOK, dues, duesOK int
	for gi, pi := range am {
		ga, pa := g.Actions[gi], a.Actions[pi]
		if ga.Owner != "" {
			owners++
			if sameOwner(ga.Owner, pa.Owner, a.People) {
				ownersOK++
			}
		}
		if ga.Due != "" {
			dues++
			if strings.TrimSpace(pa.Due) == ga.Due {
				duesOK++
			}
		}
	}
	r.OwnerAccuracy = ratio(ownersOK, owners)
	r.DueAccuracy = ratio(duesOK, dues)

	byseq := make(map[int64]string, len(g.Transcript))
	for _, u := range g.Transcript {
		byseq[u.Seq] = u.Text
	}
	var items, valid, unsupported int
	check := func(text string, ev []int64) {
		items++
		switch {
		case len(ev) == 0:
			unsupported++
		case evidenceSupports(text, ev, byseq):
			valid++
		}
	}
	for _, d := range a.Decisions {
		check(d.Text, d.Evidence)
	}
	for _, x := range a.Actions {
		check(x.Text, x.Evidence)
	}
	r.EvidenceValid = ratio(valid, items)
	r.Unsupported = float64(unsupported)
	return r
}

// match pairs gold with predicted texts, greedily from the most similar
// pair down, and returns gold index → predicted index.
func match(gold, pred []string) map[int]int {
	gs := make([]map[string]bool, len(gold))
	for i, s := range gold {
		gs[i] = tokenSet(s)
	}
	ps := make([]map[string]bool, len(pred))
	for i, s := range pred {
		ps[i] = tokenSet(s)
	}
	type pair struct {
		g, p int
		sim  float64
	}
	var pairs []pair
	for gi := range gs {
		for pi := range ps {
			if s := similarity(gs[gi], ps[pi]); s >= matchThreshold {
				pairs = append(pairs, pair{gi, pi, s})
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].sim > pairs[j].sim })
	out := map[int]int{}
	used := map[int]bool{}
	for _, p := range pairs {
		if _, ok := out[p.g]; ok || used[p.p] {
			continue
		}
		out[p.g] = p.p
		used[p.p] = true
	}
	return out
}

// evidenceSupports reports whether every cited seq exists and the cited
// utterances share a content word with text.
func evidenceSupports(text string, ev []int64, byseq map[int64]string) bool {
	want := tokenSet(text)
	shared := false
	for _, seq := range ev {
		u, ok := byseq[seq]
		if !ok {
			return false
		}
		if !shared && intersect(want, tokenSet(u)) > 0 {
			shared = true
		}
	}
	return shared
}

// sameOwner reports whether the predicted owner names the gold owner:
// directly, or through the key, name or an alias of the analysis person
// it refers to. Names compare case-insensitively, ignoring honorifics
// ("chị Lan" is "Lan") and a "person:" key prefix.
func sameOwner(gold, pred string, people []summarize.Person) bool {
	if strings.TrimSpace(pred) == "" {
		return false
	}
	names := []string{pred}
	for _, p := range people {
		forms := append([]string{p.Key, p.Name}, p.Aliases...)
		for _, f := range forms {
			if nameKey(f) == nameKey(pred) {
				names = append(names, forms...)
				break
			}
		}
	}
	want := nameKey(gold)
	for _, n := range names {
		if nameKey(n) == want {
			return true
		}
	}
	return false
}

// honorifics are titles that precede a name without being part of it.
var honorifics = map[string]bool{
	"anh": true, "chị": true, "em": true, "cô": true, "chú": true, "bác": true,
	"ông": true, "bà": true, "mr": true, "mrs": true, "ms": true, "dr": true,
}

func nameKey(s string) string {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "person:")
	var ws []string
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	}) {
		if !honorifics[w] {
			ws = append(ws, w)
		}
	}
	if len(ws) == 0 {
		return s
	}
	return strings.Join(ws, " ")
}

func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	v := float64(n) / float64(d)
	return &v
}

// precision is ratio, except that predicting nothing makes no false
// claim and scores 1.
func precision(n, d int) *float64 {
	if d == 0 {
		v := 1.0
		return &v
	}
	return ratio(n, d)
}

// Aggregate averages reports metric by metric, skipping each metric's
// undefined (nil) values; a metric undefined everywhere stays nil.
func Aggregate(rs []Report) Report {
	var out Report
	if len(rs) == 0 {
		return out
	}
	mean := func(get func(Report) *float64) *float64 {
		var sum float64
		n := 0
		for _, r := range rs {
			if v := get(r); v != nil {
				sum += *v
				n++
			}
		}
		if n == 0 {
			return nil
		}
		v := sum / float64(n)
		return &v
	}
	var jv, un float64
	for _, r := range rs {
		jv += r.JSONValid
		un += r.Unsupported
	}
	out.JSONValid = jv / float64(len(rs))
	out.Unsupported = un / float64(len(rs))
	out.DecisionRecall = mean(func(r Report) *float64 { return r.DecisionRecall })
	out.DecisionPrecision = mean(func(r Report) *float64 { return r.DecisionPrecision })
	out.ActionRecall = mean(func(r Report) *float64 { return r.ActionRecall })
	out.ActionPrecision = mean(func(r Report) *float64 { return r.ActionPrecision })
	out.OwnerAccuracy = mean(func(r Report) *float64 { return r.OwnerAccuracy })
	out.DueAccuracy = mean(func(r Report) *float64 { return r.DueAccuracy })
	out.EvidenceValid = mean(func(r Report) *float64 { return r.EvidenceValid })
	out.FlowsOK = mean(func(r Report) *float64 { return r.FlowsOK })
	return out
}
