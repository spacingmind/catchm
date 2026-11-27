package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// field is one property of an ordered JSON object.
type field struct {
	name  string
	value any
}

// ordered is a JSON object that keeps its keys in declaration order.
// Ollama's grammar emits properties in schema order, so the schema lists
// what the model should write first (ids and text before evidence).
type ordered []field

func (o ordered) MarshalJSON() ([]byte, error) {
	buf := []byte{'{'}
	for i, f := range o {
		if i > 0 {
			buf = append(buf, ',')
		}
		k, err := json.Marshal(f.name)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		buf = append(append(append(buf, k...), ':'), v...)
	}
	return append(buf, '}'), nil
}

// schemaObj is a JSON-schema object with props in order. Strict
// (OpenAI's json_schema strict mode) requires every property; otherwise
// only required, which keeps small models' grammar light. Either way
// extra properties are forbidden.
func schemaObj(strict bool, props ordered, required ...string) ordered {
	if strict {
		required = make([]string, 0, len(props))
		for _, p := range props {
			required = append(required, p.name)
		}
	}
	return ordered{{"type", "object"}, {"properties", props}, {"required", required}, {"additionalProperties", false}}
}

func schemaList(item ordered) ordered { return ordered{{"type", "array"}, {"items", item}} }

var (
	schemaStr  = ordered{{"type", "string"}}
	schemaSeqs = ordered{{"type", "array"}, {"items", ordered{{"type", "integer"}}}}
)

// analysisSchema is the reply's JSON schema: flat arrays of flat
// objects, where only the fields the analysis can't do without are
// required unless strict (see schemaObj), and "" stands for "not
// stated".
func analysisSchema(strict bool) ordered {
	str, seqs, list := schemaStr, schemaSeqs, schemaList
	obj := func(props ordered, required ...string) ordered { return schemaObj(strict, props, required...) }
	kinds := []string{"project", "product", "org", "tool", "term", "place"}
	return obj(ordered{
		{"summary", str},
		{"people", list(obj(ordered{
			{"name", str}, {"key", str}, {"speaker", str},
			{"aliases", ordered{{"type", "array"}, {"items", str}}},
		}, "name", "key"))},
		{"topics", list(obj(ordered{
			{"id", str}, {"title", str}, {"key", str}, {"evidence", seqs},
		}, "id", "title", "key", "evidence"))},
		{"decisions", list(obj(ordered{
			{"id", str}, {"text", str}, {"scope", str}, {"topic", str}, {"evidence", seqs},
		}, "id", "text", "topic", "evidence"))},
		{"actions", list(obj(ordered{
			{"id", str}, {"text", str}, {"owner", str}, {"due", str}, {"due_text", str},
			{"topic", str}, {"evidence", seqs},
		}, "id", "text", "owner", "due", "topic", "evidence"))},
		{"entities", list(obj(ordered{
			{"name", str}, {"kind", ordered{{"type", "string"}, {"enum", kinds}}}, {"key", str},
			{"evidence", seqs},
		}, "name", "kind", "key"))},
	}, "summary", "people", "topics", "decisions", "actions", "entities")
}

// replyFormat is the JSON schema one kind of call's reply follows:
// schema is Ollama's format, strict OpenAI's json_schema (strict mode),
// named name.
type replyFormat struct {
	name   string
	schema ordered
	strict ordered
}

// analysisFormat is the summary call's reply format.
var analysisFormat = replyFormat{"meeting_extraction", analysisSchema(false), analysisSchema(true)}

const shapeInstructions = `Respond with ONLY a JSON object — no markdown fences, no prose outside
the JSON — with these keys:

{
  "summary": "2-3 sentence summary of the meeting",
  "people": [{"name": "name as said", "key": "person:english-slug", "speaker": "their transcript speaker label, or \"\"", "aliases": ["other names used for them"]}],
  "topics": [{"id": "t1", "title": "short topic title", "key": "topic:english-slug", "evidence": [3, 7]}],
  "decisions": [{"id": "d1", "text": "what was decided", "scope": "optional context, or \"\"", "topic": "t1", "evidence": [8]}],
  "actions": [{"id": "a1", "text": "task", "owner": "the owner's person key, or \"\"", "due": "YYYY-MM-DD if an exact date is stated, or \"\"", "due_text": "the deadline as said (e.g. \"next Friday\"), or \"\"", "topic": "t1", "evidence": [9]}],
  "entities": [{"name": "name as said", "kind": "project|product|org|tool|term|place", "key": "project:english-slug", "evidence": [4]}]
}

Rules:
- people: the speakers, and anyone who owns an action item.
- topics: the main things discussed (1-8). Every decision and action
  names the id of its topic in "topic", or "" when none fits.
- Keys are lowercase canonical ENGLISH slugs with a type prefix (person:,
  topic:, project:, product:, org:, tool:, term:, place:), even when the
  meeting is not in English: "topic:pricing-page", "person:long". Use the
  same key for the same thing every time.
- The summary, titles, texts and names stay in the meeting's language.
- Only include decisions and action items the transcript actually
  states; never invent them. Never invent an owner or a date: use ""
  when none is stated.
- Use [] for empty arrays and "" for unknown values.`

const extractSystem = `You read meeting transcripts and extract their structure as JSON.
Each transcript line starts with its line number in brackets, e.g.
"[12] Speaker 1: ...". "evidence" lists the numbers of the lines that
state the item; use only numbers that appear in the transcript.

` + shapeInstructions

const mergeSystem = `You combine partial analyses of ONE meeting, each taken from a
consecutive part of its transcript, into a single analysis as JSON.
Write one summary for the whole meeting. Merge topics, people and
entities that are the same thing (the same key, or the same thing worded
differently), and point each decision and action at its merged topic's
id. Merge duplicate decisions and action items (the same point worded
differently counts as a duplicate), keeping the most specific owner, due
date and scope, and the union of their evidence. "evidence" numbers are
transcript line numbers: keep them as given, never add new ones.

` + shapeInstructions

// languageLine tells the model which language to write in.
func languageLine(lang string) string {
	if lang == "" {
		return "Write the summary, titles, decisions and action items in the transcript's language; keys stay English."
	}
	return fmt.Sprintf("The meeting language is %q. Write the summary, titles, decisions and action items in that language; keys stay English.", lang)
}

func extractUser(in Input, part, parts int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Meeting title: %s\n%s\n\n", in.Title, languageLine(in.Language))
	if len(in.Guests) > 0 {
		fmt.Fprintf(&b, "Invited: %s. Only use these names for a speaker or an owner when the transcript supports it.\n\n", strings.Join(in.Guests, ", "))
	}
	if line := meLine(in); line != "" {
		b.WriteString(line + "\n\n")
	}
	if parts > 1 {
		fmt.Fprintf(&b, "This is part %d of %d of the transcript; extract only what this part says.\n\n", part, parts)
	}
	b.WriteString("Transcript:\n")
	b.WriteString(in.Transcript)
	return b.String()
}

func mergeUser(in Input, partsJSON []byte) string {
	return fmt.Sprintf("Meeting title: %s\n%s\n\nPartial analyses, in transcript order:\n%s",
		in.Title, languageLine(in.Language), partsJSON)
}

// completer is one LLM call: system and user prompt in, raw reply out.
// Errors are already classified *Error values.
type completer interface {
	complete(ctx context.Context, f replyFormat, system, user string) (string, error)
}

// promptOverheadTokens reserves room in each chunk for the system
// prompt, title and instructions around the transcript.
const promptOverheadTokens = 1500

// runLLM summarizes in through c: one call when the transcript fits
// budgetTokens, else one call per chunk followed by a merge pass.
func runLLM(ctx context.Context, c completer, budgetTokens int, in Input) (Analysis, error) {
	chunks := splitTokens(in.Transcript, budgetTokens-promptOverheadTokens)
	parts := make([]Analysis, 0, len(chunks))
	for i, chunk := range chunks {
		part := in
		part.Transcript = chunk
		user := extractUser(part, i+1, len(chunks))
		a, err := withRetry(func() (string, error) { return c.complete(ctx, analysisFormat, extractSystem, user) }, parseReply, errNoSummary)
		if err != nil {
			return Analysis{}, err
		}
		parts = append(parts, a)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return mergeLLM(ctx, c, budgetTokens, in, renumber(parts))
}

// mergeLLM asks the model to merge the per-chunk analyses (whose ids are
// already unique across parts), falling back to the deterministic merge
// when they are too big for one call or the model's merge is unusable.
// People and entities the model dropped are restored, the parts' issues
// kept, and the result is deduplicated either way.
func mergeLLM(ctx context.Context, c completer, budgetTokens int, in Input, parts []Analysis) (Analysis, error) {
	fallback := mergeAnalyses(parts)
	raw, err := json.MarshalIndent(partsForMerge(parts), "", " ")
	if err != nil || estimateTokens(string(raw)) > budgetTokens-promptOverheadTokens {
		return fallback, nil
	}
	user := mergeUser(in, raw)
	a, err := withRetry(func() (string, error) { return c.complete(ctx, analysisFormat, mergeSystem, user) }, parseReply, errNoSummary)
	if err != nil {
		var se *Error
		if errors.As(err, &se) && se.Kind == KindFailed {
			return fallback, nil
		}
		return Analysis{}, err
	}
	a.Issues = append(fallback.Issues, a.Issues...)
	return mergeAnalyses([]Analysis{a, {People: fallback.People, Entities: fallback.Entities}}), nil
}

// partsForMerge strips the fields a merge call doesn't need to see.
func partsForMerge(parts []Analysis) []map[string]any {
	out := make([]map[string]any, len(parts))
	for i, p := range parts {
		out[i] = map[string]any{
			"summary": p.Summary, "people": p.People, "topics": p.Topics,
			"decisions": p.Decisions, "actions": p.Actions, "entities": p.Entities,
		}
	}
	return out
}

// runeCost weighs one rune; a string's cost is the sum over its runes,
// so chunk sizes can be tracked incrementally.
type runeCost func(r rune) int

// quarterTokens approximates a rune's share of a token without a
// tokenizer, in quarter-tokens: ~4 ASCII characters per token, ~2 for
// other alphabetic scripts (accented Latin, Cyrillic, ...), and one per
// CJK character. It errs high so chunks stay under the model's context.
func quarterTokens(r rune) int {
	switch {
	case r < utf8.RuneSelf:
		return 1
	case r >= 0x2E80:
		return 4
	default:
		return 2
	}
}

func costOf(s string, rc runeCost) int {
	n := 0
	for _, r := range s {
		n += rc(r)
	}
	return n
}

// estimateTokens approximates s's token count (see quarterTokens).
func estimateTokens(s string) int { return costOf(s, quarterTokens)/4 + 1 }

// splitTokens cuts transcript into chunks of at most budgetTokens
// estimated tokens.
func splitTokens(transcript string, budgetTokens int) []string {
	return splitTranscript(transcript, budgetTokens*4, quarterTokens)
}

// splitTranscript cuts transcript into chunks whose cost stays within
// budget, breaking between lines (and inside a line only when that one
// line alone is over budget, at a space when there is one).
func splitTranscript(transcript string, budget int, rc runeCost) []string {
	if budget < 4 {
		budget = 4
	}
	if costOf(transcript, rc) <= budget {
		return []string{transcript}
	}
	var chunks []string
	var cur strings.Builder
	curCost := 0
	flush := func() {
		if cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
			curCost = 0
		}
	}
	for _, line := range strings.SplitAfter(transcript, "\n") {
		c := costOf(line, rc)
		if c > budget {
			flush()
			chunks = append(chunks, splitLine(line, budget, rc)...)
			continue
		}
		if curCost+c > budget {
			flush()
		}
		cur.WriteString(line)
		curCost += c
	}
	flush()
	return chunks
}

func splitLine(line string, budget int, rc runeCost) []string {
	var out []string
	start, c, lastSpace := 0, 0, -1
	for i, r := range line {
		w := rc(r)
		if c+w > budget {
			cut := i
			if lastSpace > start {
				cut = lastSpace + 1
			}
			out = append(out, line[start:cut])
			start, c, lastSpace = cut, costOf(line[cut:i], rc), -1
		}
		if r == ' ' {
			lastSpace = i
		}
		c += w
	}
	if start < len(line) {
		out = append(out, line[start:])
	}
	return out
}

// meLine tells the model who recorded the meeting, so "I'll send it" on
// their lines, or a task given to one of their aliases, is theirs.
func meLine(in Input) string {
	if in.Me == "" && len(in.MeAliases) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Recorded by: ")
	if in.Me != "" {
		b.WriteString(in.Me)
	} else {
		b.WriteString(in.MeAliases[0])
	}
	if aliases := otherThan(in.MeAliases, in.Me); len(aliases) > 0 {
		fmt.Fprintf(&b, " (also called %s)", strings.Join(aliases, ", "))
	}
	b.WriteString(". Use that name for them, including as an action item owner.")
	return b.String()
}

func otherThan(names []string, name string) []string {
	var out []string
	for _, n := range names {
		if !strings.EqualFold(n, name) {
			out = append(out, n)
		}
	}
	return out
}
