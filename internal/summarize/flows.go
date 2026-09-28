package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spacingmind/catchm/internal/meeting"
)

// FlowFinder finds the processes (business flows, use cases) a meeting
// walks through: the on-demand flows pass, separate from Summarize so
// the summary schema stays small. a is the meeting's analysis, whose
// people and topics the flows reuse. Errors are *Error values; the flows
// are not yet validated (see ValidateFlows). Ollama and OpenAI implement
// it; CatchM Cloud doesn't yet.
type FlowFinder interface {
	FindFlows(ctx context.Context, in Input, a Analysis) ([]Flow, error)
}

// minFlowSteps is the fewest valid steps a kept flow has.
const minFlowSteps = 2

// errNoFlows is withRetry's user-facing message for flows calls.
const errNoFlows = "The model didn't return usable flows. Try again, or pick a different model."

// flowSchema is the flows reply's JSON schema, ordered and with minimal
// required fields like analysisSchema: flows of steps of next edges,
// nothing else nested.
func flowSchema(strict bool) ordered {
	str, seqs, list := schemaStr, schemaSeqs, schemaList
	obj := func(props ordered, required ...string) ordered { return schemaObj(strict, props, required...) }
	next := obj(ordered{{"to", str}, {"condition", str}}, "to")
	step := obj(ordered{
		{"id", str}, {"order", ordered{{"type", "integer"}}}, {"text", str}, {"actor", str},
		{"evidence", seqs}, {"next", list(next)},
	}, "id", "order", "text", "next")
	flow := obj(ordered{
		{"id", str}, {"name", str}, {"actor", str}, {"trigger", str}, {"evidence", seqs},
		{"steps", list(step)},
	}, "id", "name", "evidence", "steps")
	return obj(ordered{{"flows", list(flow)}}, "flows")
}

// flowsFormat is the flows call's reply format.
var flowsFormat = replyFormat{"meeting_flows", flowSchema(false), flowSchema(true)}

const flowsSystem = `You find the processes a meeting walks through (business flows, use
cases, procedures: how a refund is handled, how a customer is onboarded,
how a release goes out) and write them as JSON. Each transcript line
starts with its line number in brackets, e.g. "[12] Speaker 1: ...".
"evidence" lists the numbers of the lines that state the item; use only
numbers that appear in the transcript.

Respond with ONLY a JSON object — no markdown fences, no prose outside
the JSON — shaped like this:

{"flows": [{"id": "f1", "name": "short name of the process", "actor": "who runs it, or \"\"", "trigger": "what starts it, or \"\"", "evidence": [3, 4, 6], "steps": [
  {"id": "s1", "order": 1, "text": "what happens", "actor": "who does it, or \"\"", "evidence": [3], "next": [{"to": "s2", "condition": ""}]},
  {"id": "s2", "order": 2, "text": "a check with two outcomes", "actor": "", "evidence": [4], "next": [{"to": "s3", "condition": "if approved"}, {"to": "s4", "condition": "if rejected"}]},
  {"id": "s3", "order": 3, "text": "what happens when approved", "actor": "", "evidence": [5], "next": []},
  {"id": "s4", "order": 4, "text": "what happens when rejected", "actor": "", "evidence": [6], "next": []}]}]}

Rules:
- Don't invent flows. Only a process the meeting actually describes
  step by step is a flow; status updates, discussions, decisions and
  to-do lists are not. Most meetings describe no process: then answer
  {"flows": []}.
- Never invent steps: include only steps the transcript states, not
  ones such a process usually has.
- At most 5 flows, with 3-10 steps each. Leave out a process with
  fewer than 3 stated steps.
- "order" numbers a flow's steps 1, 2, 3, ... in the order they happen.
- "next" lists the steps that come directly after this one, by their id
  in the same flow. A plain sequence has one entry with condition "". A
  branch has one entry per outcome, each with its condition. The last
  step, and the end of each branch, has "next": [].
- Step ids are s1, s2, ... within each flow; flow ids are f1, f2, ...
- "actor": a listed person's key (e.g. "person:rosa") when that person
  does it; otherwise the role as said (e.g. "team lead"), or "".
- When a flow is one of the listed topics, name it in the topic's words.
- Names, texts and conditions stay in the meeting's language.`

// flowsContext lists a's people and topics for the flows prompt, so
// actors reuse the people keys and flow names the topics' wording.
func flowsContext(a Analysis) string {
	var b strings.Builder
	if len(a.People) > 0 {
		b.WriteString("People (key: name):\n")
		for _, p := range a.People {
			fmt.Fprintf(&b, "- %s: %s", p.Key, p.Name)
			if len(p.Aliases) > 0 {
				fmt.Fprintf(&b, " (also %s)", strings.Join(p.Aliases, ", "))
			}
			if p.Speaker != "" {
				fmt.Fprintf(&b, " [speaker %s]", p.Speaker)
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	if len(a.Topics) > 0 {
		b.WriteString("Topics (id: title):\n")
		for _, t := range a.Topics {
			fmt.Fprintf(&b, "- %s: %s\n", t.ID, t.Title)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func flowsUser(in Input, known, chunk string, part, parts int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Meeting title: %s\n", in.Title)
	if in.Language == "" {
		b.WriteString("Write names, texts and conditions in the transcript's language; keys stay as listed.\n\n")
	} else {
		fmt.Fprintf(&b, "The meeting language is %q. Write names, texts and conditions in that language; keys stay as listed.\n\n", in.Language)
	}
	b.WriteString(known)
	if parts > 1 {
		fmt.Fprintf(&b, "This is part %d of %d of the transcript; find only the flows this part describes.\n\n", part, parts)
	}
	b.WriteString("Transcript:\n")
	b.WriteString(chunk)
	return b.String()
}

// runFlows finds flows in in through c: one call when the transcript
// fits budgetTokens, else one call per chunk, their flows concatenated
// (see concatFlows).
func runFlows(ctx context.Context, c completer, budgetTokens int, in Input, a Analysis) ([]Flow, error) {
	known := flowsContext(a)
	budget := max(budgetTokens-promptOverheadTokens-estimateTokens(known), budgetTokens/2)
	chunks := splitTokens(in.Transcript, budget)
	parts := make([][]Flow, 0, len(chunks))
	for i, chunk := range chunks {
		user := flowsUser(in, known, chunk, i+1, len(chunks))
		flows, err := withRetry(func() (string, error) { return c.complete(ctx, flowsFormat, flowsSystem, user) }, parseFlows, errNoFlows)
		if err != nil {
			return nil, err
		}
		parts = append(parts, flows)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return concatFlows(parts), nil
}

// concatFlows joins per-chunk flows in order, deduping by normalized
// name: of two flows with the same name the one with more steps is
// kept, with both's evidence. Flow ids are renumbered f1.. so they are
// unique across chunks.
func concatFlows(parts [][]Flow) []Flow {
	out := []Flow{}
	at := map[string]int{}
	for _, flows := range parts {
		for _, f := range flows {
			k := dedupeKey(nameKey(f.Name))
			if k == "" {
				continue
			}
			if i, ok := at[k]; ok {
				kept := &out[i]
				if len(f.Steps) > len(kept.Steps) {
					f.ID, f.Evidence = kept.ID, unionSeqs(kept.Evidence, f.Evidence)
					*kept = f
				} else {
					kept.Evidence = unionSeqs(kept.Evidence, f.Evidence)
				}
				continue
			}
			at[k] = len(out)
			f.ID, f.Evidence = fmt.Sprintf("f%d", len(out)+1), unionSeqs(nil, f.Evidence)
			out = append(out, f)
		}
	}
	return out
}

// flowsAliases are the top-level keys a model may use for "flows".
var flowsAliases = []string{"flows", "processes", "workflows", "use_cases", "usecases"}

// parseFlows parses a flows reply. It is tolerant like parseReply: a
// bare array stands for the flows, alias keys and field names are
// accepted, and the JSON is sanitized before decoding. A reply with no
// flows key at all is an error; {"flows": []} (or null) is no flows.
func parseFlows(raw string) ([]Flow, error) {
	raw = stripFences(thinkBlock.ReplaceAllString(raw, ""))
	var generic map[string]any
	if strings.HasPrefix(raw, "[") {
		var list []any
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return nil, fmt.Errorf("parse model JSON: %w", err)
		}
		generic = map[string]any{"flows": list}
	} else {
		obj, err := jsonObject(raw)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(obj), &generic); err != nil {
			return nil, fmt.Errorf("parse model JSON: %w", err)
		}
	}
	found := false
	for _, k := range flowsAliases {
		v, ok := generic[k]
		if !ok {
			continue
		}
		found = true
		delete(generic, k)
		if v != nil {
			generic["flows"] = v
			break
		}
	}
	if !found {
		return nil, errors.New("model reply has no flows field")
	}
	if _, ok := generic["flows"]; !ok {
		return []Flow{}, nil
	}
	sanitizeReply(generic)
	clean, err := json.Marshal(map[string]any{"flows": generic["flows"]})
	if err != nil {
		return nil, err
	}
	var out struct {
		Flows []Flow `json:"flows"`
	}
	if err := json.Unmarshal(clean, &out); err != nil {
		return nil, fmt.Errorf("parse model JSON: %w", err)
	}
	if out.Flows == nil {
		out.Flows = []Flow{}
	}
	return out.Flows, nil
}

// loadAnalysis reads and decodes a meeting's analysis.json.
func loadAnalysis(store *meeting.Store, id string) (Analysis, error) {
	raw, err := store.ReadAnalysis(id)
	if err != nil {
		return Analysis{}, err
	}
	var a Analysis
	if err := json.Unmarshal(raw, &a); err != nil {
		return Analysis{}, fmt.Errorf("read %s: %w", meeting.AnalysisFile, err)
	}
	return a, nil
}

func writeAnalysis(store *meeting.Store, id string, a Analysis) error {
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteAnalysis(id, append(raw, '\n'))
}

// FindMeetingFlows runs the flows pass over the meeting's transcript
// (the one its analysis was made from), validates the flows, and stores
// them in analysis.json in place of the earlier ones. It needs an
// analysis: without one it returns meeting.ErrNoAnalysis. It returns the
// updated analysis; no flows is a valid result.
func FindMeetingFlows(ctx context.Context, store *meeting.Store, id string, ff FlowFinder) (Analysis, error) {
	a, err := loadAnalysis(store, id)
	if err != nil {
		return Analysis{}, err
	}
	src, err := loadSourceFrom(store, id, a.Generated.Transcript)
	if err != nil {
		return Analysis{}, err
	}
	var flows []Flow
	if strings.TrimSpace(src.in.Transcript) != "" {
		if flows, err = ff.FindFlows(ctx, src.in, a); err != nil {
			return Analysis{}, err
		}
	}
	// A summary written while the model ran wins over the copy read
	// above; only the flows are replaced.
	if a, err = loadAnalysis(store, id); err != nil {
		return Analysis{}, err
	}
	flows, issues := ValidateFlows(flows, a.People, src.seqs)
	a.Flows = flows
	a.Issues = append(withoutFlowIssues(a.Issues), issues...)
	if err := writeAnalysis(store, id, a); err != nil {
		return Analysis{}, err
	}
	return a, nil
}

// withoutFlowIssues drops the issues an earlier validation logged about
// flows, which a new flows pass replaces.
func withoutFlowIssues(issues []string) []string {
	out := []string{}
	for _, s := range issues {
		rest := strings.TrimPrefix(strings.TrimPrefix(s, "auto-corrected: "), "dropped: ")
		if !strings.HasPrefix(rest, "flows[") {
			out = append(out, s)
		}
	}
	return out
}

func (o *Ollama) FindFlows(ctx context.Context, in Input, a Analysis) ([]Flow, error) {
	if o.Host == "" || o.Model == "" {
		return nil, notReady("Pick an Ollama model in Settings → Summaries.", nil)
	}
	return runFlows(ctx, o, ollamaBudgetTokens, in, a)
}

func (o *OpenAI) FindFlows(ctx context.Context, in Input, a Analysis) ([]Flow, error) {
	if o.BaseURL == "" || o.Model == "" {
		return nil, notReady("Add your Cloudflare Account ID and API token in Settings → Summaries.", nil)
	}
	return runFlows(ctx, o, openaiBudgetTokens, in, a)
}
