package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// schema is the Extraction JSON schema sent as Ollama's format and
// OpenAI's json_schema. Every property is required and extra ones are
// forbidden, as OpenAI's strict mode demands; "" stands for "not stated".
var schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"summary":      map[string]any{"type": "string"},
		"participants": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"decisions": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":  map[string]any{"type": "string"},
				"scope": map[string]any{"type": "string"},
			},
			"required":             []string{"text", "scope"},
			"additionalProperties": false,
		}},
		"action_items": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":  map[string]any{"type": "string"},
				"owner": map[string]any{"type": "string"},
				"due":   map[string]any{"type": "string"},
			},
			"required":             []string{"text", "owner", "due"},
			"additionalProperties": false,
		}},
	},
	"required":             []string{"summary", "participants", "decisions", "action_items"},
	"additionalProperties": false,
}

const shapeInstructions = `Respond with ONLY a JSON object — no markdown fences, no prose outside
the JSON — with exactly these keys:

{
  "summary": "2-3 sentence summary of the meeting",
  "participants": ["speaker names mentioned in the transcript"],
  "decisions": [{"text": "what was decided", "scope": "optional context, or \"\""}],
  "action_items": [{"text": "task", "owner": "name if stated, or \"\"", "due": "YYYY-MM-DD if an exact date is stated, or \"\""}]
}

Use [] for empty arrays and "" for unknown values. Only include decisions
and action items the transcript actually states; never invent them.`

const extractSystem = `You read meeting transcripts and extract their structure as JSON.

` + shapeInstructions

const mergeSystem = `You combine partial extractions of ONE meeting, each taken from a
consecutive part of its transcript, into a single extraction as JSON.
Write one summary for the whole meeting. Merge duplicate participants,
decisions and action items (the same point worded differently counts as a
duplicate), keeping the most specific owner, due date and scope.

` + shapeInstructions

// languageLine tells the model which language to write in.
func languageLine(lang string) string {
	if lang == "" {
		return "Write the summary, decisions and action items in the transcript's language."
	}
	return fmt.Sprintf("The meeting language is %q. Write the summary, decisions and action items in that language.", lang)
}

func extractUser(in Input, part, parts int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Meeting title: %s\n%s\n\n", in.Title, languageLine(in.Language))
	if parts > 1 {
		fmt.Fprintf(&b, "This is part %d of %d of the transcript; extract only what this part says.\n\n", part, parts)
	}
	b.WriteString("Transcript:\n")
	b.WriteString(in.Transcript)
	return b.String()
}

func mergeUser(in Input, partsJSON []byte) string {
	return fmt.Sprintf("Meeting title: %s\n%s\n\nPartial extractions, in transcript order:\n%s",
		in.Title, languageLine(in.Language), partsJSON)
}

// completer is one LLM call: system and user prompt in, raw reply out.
// Errors are already classified *Error values.
type completer interface {
	complete(ctx context.Context, system, user string) (string, error)
}

// promptOverheadTokens reserves room in each chunk for the system
// prompt, title and instructions around the transcript.
const promptOverheadTokens = 800

// runLLM summarizes in through c: one call when the transcript fits
// budgetTokens, else one call per chunk followed by a merge pass.
func runLLM(ctx context.Context, c completer, budgetTokens int, in Input) (Extraction, error) {
	chunks := splitTokens(in.Transcript, budgetTokens-promptOverheadTokens)
	parts := make([]Extraction, 0, len(chunks))
	for i, chunk := range chunks {
		part := in
		part.Transcript = chunk
		user := extractUser(part, i+1, len(chunks))
		ex, err := withRetry(func() (string, error) { return c.complete(ctx, extractSystem, user) })
		if err != nil {
			return Extraction{}, err
		}
		parts = append(parts, ex)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return mergeLLM(ctx, c, budgetTokens, in, parts)
}

// mergeLLM asks the model to merge the per-chunk extractions, falling
// back to the deterministic merge when they are too big for one call or
// the model's merge is unusable. Participants the model dropped are
// restored, and the result is deduplicated either way.
func mergeLLM(ctx context.Context, c completer, budgetTokens int, in Input, parts []Extraction) (Extraction, error) {
	fallback := mergeExtractions(parts)
	raw, err := json.MarshalIndent(parts, "", " ")
	if err != nil || estimateTokens(string(raw)) > budgetTokens-promptOverheadTokens {
		return fallback, nil
	}
	user := mergeUser(in, raw)
	ex, err := withRetry(func() (string, error) { return c.complete(ctx, mergeSystem, user) })
	if err != nil {
		var se *Error
		if errors.As(err, &se) && se.Kind == KindFailed {
			return fallback, nil
		}
		return Extraction{}, err
	}
	ex.Participants = append(ex.Participants, fallback.Participants...)
	return dedupe(normalize(ex)), nil
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
