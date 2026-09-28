package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spacingmind/catchm/internal/summarize"
)

// Result is one gold file's outcome. Report is nil when the file
// couldn't be evaluated (the provider isn't set up, or the run was
// cancelled); a provider that returned no usable analysis is still
// evaluated, with json_valid 0 and zero recall.
type Result struct {
	Name   string  `json:"name"`
	Lang   string  `json:"lang"`
	Report *Report `json:"report"`
	// Issues is how many problems summarize.Validate logged and fixed.
	Issues int    `json:"validator_issues"`
	Error  string `json:"error,omitempty"`
}

// Run summarizes each gold transcript with sum, validates the analysis
// against the gold seqs and scores it; for gold files with expect_flows
// it also runs sum's flows pass, when sum has one. A failing file is recorded in
// its Result and the run moves on. progress, when non-nil, is called
// before each file.
func Run(ctx context.Context, sum summarize.Summarizer, golds []NamedGold, progress func(name string)) []Result {
	out := make([]Result, 0, len(golds))
	for _, ng := range golds {
		if progress != nil {
			progress(ng.Name)
		}
		out = append(out, runOne(ctx, sum, ng))
	}
	return out
}

func runOne(ctx context.Context, sum summarize.Summarizer, ng NamedGold) Result {
	res := Result{Name: ng.Name, Lang: ng.Gold.Lang}
	if err := ctx.Err(); err != nil {
		res.Error = err.Error()
		return res
	}
	a, err := sum.Summarize(ctx, Input(ng.Gold))
	if err != nil {
		res.Error = err.Error()
		var se *summarize.Error
		if errors.As(err, &se) && se.Kind == summarize.KindFailed && ctx.Err() == nil {
			r := Score(ng.Gold, summarize.Analysis{})
			r.JSONValid = 0
			// Nothing was claimed, but a failure shouldn't earn precision.
			r.DecisionPrecision, r.ActionPrecision = nil, nil
			res.Report = &r
		}
		return res
	}
	res.Issues = len(summarize.Validate(&a, ng.Gold.Seqs()))
	r := Score(ng.Gold, a)
	if ff, ok := sum.(summarize.FlowFinder); ok && ng.Gold.ExpectFlows != nil {
		hit := 0.0
		flows, err := ff.FindFlows(ctx, Input(ng.Gold), a)
		if err != nil {
			res.Error = "flows: " + err.Error()
		} else if flows, _ = summarize.ValidateFlows(flows, a.People, ng.Gold.Seqs()); len(flows) == *ng.Gold.ExpectFlows {
			hit = 1
		}
		r.FlowsOK = &hit
	}
	res.Report = &r
	return res
}

// Input is the summarize.Input the app would build for g's meeting.
func Input(g Gold) summarize.Input {
	return summarize.Input{
		Title:      g.Title,
		Language:   g.Lang,
		Transcript: summarize.FormatTranscript(g.Utterances()),
	}
}

// Reports returns the reports of the evaluated results.
func Reports(rs []Result) []Report {
	var out []Report
	for _, r := range rs {
		if r.Report != nil {
			out = append(out, *r.Report)
		}
	}
	return out
}

// Output is the machine-readable form of a run.
type Output struct {
	Provider  string   `json:"provider"`
	Model     string   `json:"model"`
	Files     []Result `json:"files"`
	Evaluated int      `json:"evaluated"`
	// Aggregate is the per-metric mean over evaluated files; nil when
	// none was.
	Aggregate *Report `json:"aggregate"`
}

// NewOutput bundles a run's results with their aggregate.
func NewOutput(provider, model string, rs []Result) Output {
	o := Output{Provider: provider, Model: model, Files: rs}
	if reps := Reports(rs); len(reps) > 0 {
		agg := Aggregate(reps)
		o.Evaluated, o.Aggregate = len(reps), &agg
	}
	return o
}

// WriteJSON writes o as indented JSON.
func (o Output) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(o)
}

// WriteTable writes one row per file, an aggregate row, then each
// file's error. Undefined metrics print as "-".
func (o Output) WriteTable(w io.Writer) error {
	fmt.Fprintf(w, "provider: %s  model: %s\n\n", o.Provider, o.Model)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILE\tLANG\tJSON\tDEC_R\tDEC_P\tACT_R\tACT_P\tOWNER\tDUE\tEVID\tFLOWS\tUNSUP\tISSUES")
	for _, r := range o.Files {
		if r.Report == nil {
			fmt.Fprintf(tw, "%s\t%s\tnot evaluated\n", r.Name, r.Lang)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", r.Name, r.Lang, row(*r.Report, "%.0f"), r.Issues)
	}
	if o.Aggregate != nil {
		fmt.Fprintf(tw, "MEAN (%d/%d)\t\t%s\t\n", o.Evaluated, len(o.Files), row(*o.Aggregate, "%.1f"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	header := "\nerrors:\n"
	for _, r := range o.Files {
		if r.Error == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s  %s: %s\n", header, r.Name, r.Error); err != nil {
			return err
		}
		header = ""
	}
	return nil
}

func row(r Report, unsupported string) string {
	m := func(v *float64) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprintf("%.2f", *v)
	}
	return fmt.Sprintf("%.2f\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t"+unsupported,
		r.JSONValid, m(r.DecisionRecall), m(r.DecisionPrecision), m(r.ActionRecall),
		m(r.ActionPrecision), m(r.OwnerAccuracy), m(r.DueAccuracy), m(r.EvidenceValid), m(r.FlowsOK), r.Unsupported)
}
