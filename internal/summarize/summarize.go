// Package summarize writes a meeting's summary, participants, decisions
// and action items with one structured-output LLM call per transcript
// chunk. Providers are pluggable: a local Ollama model (the default), the
// CatchM Cloud endpoint, or any OpenAI-compatible server. Go, not the
// model, applies the result to the meeting's frontmatter.
package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
)

// Provider names, as stored in config.SummaryConfig.Provider.
const (
	ProviderOllama = "ollama"
	ProviderCloud  = "catchm-cloud"
	ProviderOpenAI = "openai"
)

// Input is what a Summarizer sees of a meeting.
type Input struct {
	Title string
	// Language is a hint (e.g. "vi"); empty when unknown.
	Language   string
	Transcript string
}

// Extraction is the JSON shape every provider returns (the pluggable-
// summarizer plan's contract).
type Extraction struct {
	Summary      string               `json:"summary"`
	Participants []string             `json:"participants"`
	Decisions    []meeting.Decision   `json:"decisions"`
	ActionItems  []meeting.ActionItem `json:"action_items"`
}

// Summarizer turns one meeting's transcript into an Extraction. Errors
// are *Error values so callers can report their Kind.
type Summarizer interface {
	Summarize(ctx context.Context, in Input) (Extraction, error)
}

// Kind classifies a summary failure for the UI; the values are a stable
// wire contract (web/src/lib/api.ts mirrors them).
type Kind string

const (
	// KindNotReady: the provider isn't set up (Ollama down, model
	// missing, no base URL, not logged in to the cloud).
	KindNotReady Kind = "summary_not_ready"
	// KindConsentRequired: catchm-cloud without cloud_consent. Nothing
	// was sent.
	KindConsentRequired Kind = "consent_required"
	// KindQuotaExceeded: the cloud daily quota is used up; ResetAt says
	// when it refills.
	KindQuotaExceeded Kind = "quota_exceeded"
	// KindFailed: the model call or JSON parsing failed after a retry.
	KindFailed Kind = "summary_failed"
)

// Error is a classified summary failure. Message is plain, user-facing
// text; Err is the underlying cause, for logs.
type Error struct {
	Kind    Kind
	Message string
	// ResetAt is an RFC3339 time, set only for KindQuotaExceeded.
	ResetAt string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func notReady(msg string, err error) *Error {
	return &Error{Kind: KindNotReady, Message: msg, Err: err}
}

func failed(msg string, err error) *Error {
	return &Error{Kind: KindFailed, Message: msg, Err: err}
}

// CloudAuth is the in-memory cloud-sync session a catchm-cloud
// summarizer borrows; Token is empty when logged out.
type CloudAuth struct {
	BaseURL string
	Token   string
}

// ProviderOf returns cfg's summary provider, defaulting to Ollama.
func ProviderOf(cfg *config.Config) string {
	if cfg.Summary.Provider == "" {
		return ProviderOllama
	}
	return cfg.Summary.Provider
}

// New builds the Summarizer cfg selects. It only checks configuration
// (it never touches the network), so a catchm-cloud setup without
// consent fails here, before any transcript could be sent.
func New(cfg *config.Config, cloud CloudAuth) (Summarizer, error) {
	switch ProviderOf(cfg) {
	case ProviderOllama:
		return &Ollama{Host: cfg.Translate.Host, Model: cfg.Summary.OllamaModel}, nil
	case ProviderOpenAI:
		o := cfg.Summary.OpenAI
		if o.BaseURL == "" || o.Model == "" {
			return nil, notReady("Add the server address and model name in Settings → Summaries.", nil)
		}
		return &OpenAI{BaseURL: o.BaseURL, Model: o.Model, APIKey: o.APIKey}, nil
	case ProviderCloud:
		if !cfg.Summary.CloudConsent {
			return nil, consentRequired()
		}
		if cloud.Token == "" {
			return nil, notReady("Log in to CatchM Cloud in Settings → Cloud sync to use cloud summaries.", nil)
		}
		return &Cloud{BaseURL: cloud.BaseURL, Token: cloud.Token, Consent: true}, nil
	default:
		return nil, notReady(fmt.Sprintf("Unknown summary provider %q. Pick one in Settings → Summaries.", cfg.Summary.Provider), nil)
	}
}

func consentRequired() *Error {
	return &Error{Kind: KindConsentRequired, Message: "CatchM Cloud needs your permission before it can receive this transcript."}
}

// LoadInput reads a meeting's title, language hint and transcript:
// the batch (v2) transcript when refinement has produced one, else the
// live one.
func LoadInput(store *meeting.Store, id string) (Input, error) {
	m, err := store.Get(id)
	if err != nil {
		return Input{}, err
	}
	us, err := store.ReadBatchTranscript(id)
	if err != nil {
		return Input{}, err
	}
	if len(us) == 0 {
		if us, _, err = store.ReadSince(id, -1); err != nil {
			return Input{}, err
		}
	}
	lang := m.Lang
	if lang == "" && len(m.Langs) > 0 {
		lang = m.Langs[0]
	}
	return Input{Title: m.Title, Language: lang, Transcript: formatTranscript(us)}, nil
}

func formatTranscript(us []meeting.Utterance) string {
	var b strings.Builder
	for _, u := range us {
		text := strings.TrimSpace(u.Text)
		if text == "" {
			continue
		}
		if u.Speaker != "" {
			b.WriteString(u.Speaker)
			b.WriteString(": ")
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}

// SummarizeMeeting runs sum over the meeting's transcript and applies
// the result to its frontmatter, marking it Refined so callers can tell
// this final pass apart from a coarse live one. A meeting with no
// transcript text is marked Refined without calling the provider.
func SummarizeMeeting(ctx context.Context, store *meeting.Store, id string, sum Summarizer) error {
	in, err := LoadInput(store, id)
	if err != nil {
		return err
	}
	var ex Extraction
	if strings.TrimSpace(in.Transcript) != "" {
		if ex, err = sum.Summarize(ctx, in); err != nil {
			return err
		}
	}
	return store.UpdateDoc(id, func(m *meeting.Meeting) error {
		applyExtraction(m, ex)
		m.Refined = true
		return nil
	})
}

// parseExtraction unmarshals a model reply, tolerating surrounding
// prose or fences by slicing from the first '{' to the last '}'.
func parseExtraction(raw string) (Extraction, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return Extraction{}, fmt.Errorf("no JSON object in model reply: %q", truncate(raw, 200))
	}
	var ex Extraction
	if err := json.Unmarshal([]byte(raw[start:end+1]), &ex); err != nil {
		return Extraction{}, fmt.Errorf("parse model JSON: %w", err)
	}
	return ex, nil
}

// applyExtraction overwrites each field the extraction actually produced;
// absent fields leave the document unchanged.
func applyExtraction(m *meeting.Meeting, ex Extraction) {
	if ex.Summary != "" {
		m.Summary = ex.Summary
	}
	if len(ex.Participants) > 0 {
		m.Participants = ex.Participants
	}
	if len(ex.Decisions) > 0 {
		m.Decisions = ex.Decisions
	}
	if len(ex.ActionItems) > 0 {
		m.ActionItems = ex.ActionItems
	}
}

var (
	thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)
	isoDate    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// errEmptySummary marks a reply that parsed but carries no summary.
var errEmptySummary = errors.New("model reply has an empty summary")

// parseReply is parseExtraction plus the cleanup a raw LLM reply needs:
// reasoning blocks and ```json fences are stripped, and the result is
// validated and normalized.
func parseReply(raw string) (Extraction, error) {
	raw = thinkBlock.ReplaceAllString(raw, "")
	raw = stripFences(raw)
	ex, err := parseExtraction(raw)
	if err != nil {
		return Extraction{}, err
	}
	ex = normalize(ex)
	if ex.Summary == "" {
		return Extraction{}, errEmptySummary
	}
	return ex, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:] // drop the info string ("json")
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// normalize trims every field, drops empty entries and duplicate
// participants, and keeps due only when it is a YYYY-MM-DD date (any
// other stated deadline stays visible in the item's text).
func normalize(ex Extraction) Extraction {
	out := Extraction{Summary: strings.TrimSpace(ex.Summary)}
	seen := map[string]bool{}
	for _, p := range ex.Participants {
		p = strings.TrimSpace(p)
		if p == "" || seen[dedupeKey(p)] {
			continue
		}
		seen[dedupeKey(p)] = true
		out.Participants = append(out.Participants, p)
	}
	for _, d := range ex.Decisions {
		d.Text, d.Scope = strings.TrimSpace(d.Text), strings.TrimSpace(d.Scope)
		if d.Text != "" {
			out.Decisions = append(out.Decisions, d)
		}
	}
	for _, a := range ex.ActionItems {
		a.Text, a.Owner, a.Due = strings.TrimSpace(a.Text), strings.TrimSpace(a.Owner), strings.TrimSpace(a.Due)
		a.Status = ""
		if a.Text == "" {
			continue
		}
		if a.Due != "" && !isoDate.MatchString(a.Due) {
			a.Text = fmt.Sprintf("%s (due: %s)", a.Text, a.Due)
			a.Due = ""
		}
		out.ActionItems = append(out.ActionItems, a)
	}
	return out
}

// withRetry calls call and parses its reply, retrying once when the
// reply isn't a valid extraction. Provider errors from call are
// returned as-is (they are already classified).
func withRetry(call func() (string, error)) (Extraction, error) {
	var lastErr error
	for range 2 {
		raw, err := call()
		if err != nil {
			return Extraction{}, err
		}
		ex, err := parseReply(raw)
		if err == nil {
			return ex, nil
		}
		lastErr = err
	}
	return Extraction{}, failed("The model didn't return a usable summary. Try again, or pick a different model.", lastErr)
}

// mergeExtractions combines per-chunk extractions deterministically:
// summaries joined in order, participants unioned, decisions and action
// items deduplicated by their normalized text.
func mergeExtractions(parts []Extraction) Extraction {
	var summaries []string
	var all Extraction
	for _, p := range parts {
		if p.Summary != "" {
			summaries = append(summaries, p.Summary)
		}
		all.Participants = append(all.Participants, p.Participants...)
		all.Decisions = append(all.Decisions, p.Decisions...)
		all.ActionItems = append(all.ActionItems, p.ActionItems...)
	}
	all.Summary = strings.Join(summaries, " ")
	return dedupe(normalize(all))
}

// dedupe drops decisions and action items whose normalized text repeats
// an earlier one, filling a kept item's missing owner/due/scope from
// its duplicates.
func dedupe(ex Extraction) Extraction {
	var decisions []meeting.Decision
	di := map[string]int{}
	for _, d := range ex.Decisions {
		k := dedupeKey(d.Text)
		if i, ok := di[k]; ok {
			if decisions[i].Scope == "" {
				decisions[i].Scope = d.Scope
			}
			continue
		}
		di[k] = len(decisions)
		decisions = append(decisions, d)
	}
	var items []meeting.ActionItem
	ai := map[string]int{}
	for _, a := range ex.ActionItems {
		k := dedupeKey(a.Text)
		if i, ok := ai[k]; ok {
			if items[i].Owner == "" {
				items[i].Owner = a.Owner
			}
			if items[i].Due == "" {
				items[i].Due = a.Due
			}
			continue
		}
		ai[k] = len(items)
		items = append(items, a)
	}
	ex.Decisions, ex.ActionItems = decisions, items
	return ex
}

func dedupeKey(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	return strings.TrimRight(s, ".!;, ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
