// Package summarize writes a meeting's summary, participants, decisions
// and action items with one structured-output LLM call per transcript
// chunk. Providers are pluggable: a local Ollama model (the default), the
// CatchM Cloud endpoint, or any OpenAI-compatible server. Go, not the
// model, applies the result to the meeting's frontmatter.
package summarize

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

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
	Language string
	// Transcript is one "[seq] Speaker: text" line per utterance (see
	// FormatTranscript), so the model can cite seqs as evidence. Speakers
	// read as their confirmed names when the user gave them.
	Transcript string
	// Guests are the invited people's names (never emails), candidates
	// for unnamed speakers and owners.
	Guests []string
	// Me is the user's name and MeAliases what people call them, so the
	// model can tell when a line or a task is theirs. Empty when unset.
	Me        string
	MeAliases []string
}

// Extraction is the frontmatter's four-field core (and the CatchM Cloud
// Worker's reply shape): what Analysis.Extraction projects onto
// meeting.md.
type Extraction struct {
	Summary      string               `json:"summary"`
	Participants []string             `json:"participants"`
	Decisions    []meeting.Decision   `json:"decisions"`
	ActionItems  []meeting.ActionItem `json:"action_items"`
}

// Summarizer turns one meeting's transcript into an Analysis. Errors are
// *Error values so callers can report their Kind. The Analysis is not
// yet validated against the transcript's seqs (see Validate).
type Summarizer interface {
	Summarize(ctx context.Context, in Input) (Analysis, error)
}

// describer is implemented by this package's providers, to fill
// GeneratedInfo.
type describer interface {
	describe() (provider, model string)
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
		base, _, strong := cfg.LLM.Resolved()
		if base == "" || strong == "" || cfg.LLM.APIKey == "" {
			return nil, notReady("Set up your AI provider (API key and model) in Settings → Summaries.", nil)
		}
		return &OpenAI{BaseURL: base, Model: strong, APIKey: cfg.LLM.APIKey,
			// Anthropic's OpenAI-compatible endpoint rejects a non-default
			// temperature on current Claude models.
			NoTemperature: cfg.LLM.Preset == config.LLMAnthropic}, nil
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

// Transcript sources, as recorded in GeneratedInfo.Transcript.
const (
	transcriptV2   = "v2"
	transcriptLive = "live"
)

// source is a meeting's summarizer input plus what refine needs to
// validate and label the result.
type source struct {
	in Input
	// seqs are the utterance seqs present in in.Transcript.
	seqs map[int64]bool
	// transcript is transcriptV2 or transcriptLive.
	transcript string
}

// LoadInput reads a meeting's title, language hint and transcript:
// the batch (v2) transcript when refinement has produced one, else the
// live one.
func LoadInput(store *meeting.Store, id string) (Input, error) {
	src, err := loadSource(store, id)
	return src.in, err
}

func loadSource(store *meeting.Store, id string) (source, error) {
	return loadSourceFrom(store, id, "")
}

// loadSourceFrom is loadSource, except that with want transcriptLive it
// reads the live transcript even when there is a batch one (so a pass
// over an analysis made from the live transcript cites the same seqs).
func loadSourceFrom(store *meeting.Store, id, want string) (source, error) {
	m, err := store.Get(id)
	if err != nil {
		return source{}, err
	}
	self := store.Self()
	var us []meeting.Utterance
	if want != transcriptLive {
		if us, err = store.ReadBatchTranscript(id); err != nil {
			return source{}, err
		}
	}
	kind := transcriptV2
	if len(us) == 0 {
		kind = transcriptLive
		if us, _, err = store.ReadSince(id, -1); err != nil {
			return source{}, err
		}
	}
	lang := m.Lang
	if lang == "" && len(m.Langs) > 0 {
		lang = m.Langs[0]
	}
	seqs := map[int64]bool{}
	for _, u := range us {
		if strings.TrimSpace(u.Text) != "" {
			seqs[u.Seq] = true
		}
	}
	return source{
		in: Input{
			Title: m.Title, Language: lang,
			Transcript: formatNamed(us, store.Namer(m).WithTranscript(us)),
			Guests:     meeting.GuestNames(m.Guests),
			Me:         self.Name, MeAliases: self.Aliases,
		},
		seqs:       seqs,
		transcript: kind,
	}, nil
}

// FormatTranscript renders utterances as the model's transcript: one
// "[seq] Speaker: text" line each ("[seq] text" without a speaker),
// skipping empty ones.
func FormatTranscript(us []meeting.Utterance) string {
	return formatNamed(us, (&meeting.Meeting{}).Namer(""))
}

// formatNamed is FormatTranscript with speakers named by namer.
func formatNamed(us []meeting.Utterance, namer meeting.SpeakerNamer) string {
	var b strings.Builder
	for _, u := range us {
		text := strings.TrimSpace(u.Text)
		if text == "" {
			continue
		}
		fmt.Fprintf(&b, "[%d] ", u.Seq)
		if name := namer.Name(u); name != "" {
			b.WriteString(name)
			b.WriteString(": ")
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}

// now is the clock for GeneratedInfo.At; tests replace it.
var now = func() time.Time { return time.Now().UTC() }

// SummarizeMeeting runs sum over the meeting's transcript, validates the
// Analysis against the transcript's seqs, writes it to analysis.json,
// keeping the flows of an earlier analysis.json, then syncs the
// frontmatter from it, marking the meeting Refined so
// callers can tell this final pass apart from a coarse live one. A
// meeting with no transcript text is marked Refined without calling the
// provider (and gets no analysis.json).
func SummarizeMeeting(ctx context.Context, store *meeting.Store, id string, sum Summarizer) error {
	src, err := loadSource(store, id)
	if err != nil {
		return err
	}
	var ex Extraction
	if strings.TrimSpace(src.in.Transcript) != "" {
		a, err := sum.Summarize(ctx, src.in)
		if err != nil {
			return err
		}
		// Flows come only from the on-demand flows pass: keep the earlier
		// ones (Validate drops their evidence the new transcript lacks).
		a.Flows = nil
		if prev, err := loadAnalysis(store, id); err == nil {
			a.Flows = prev.Flows
		}
		Validate(&a, src.seqs)
		a.Version = AnalysisVersion
		a.Generated = GeneratedInfo{
			At:           now().Format(time.RFC3339),
			Transcript:   src.transcript,
			AnalysisLang: src.in.Language,
		}
		if a.Generated.AnalysisLang == "" {
			a.Generated.AnalysisLang = "auto"
		}
		if d, ok := sum.(describer); ok {
			a.Generated.Provider, a.Generated.Model = d.describe()
		}
		if err := writeAnalysis(store, id, a); err != nil {
			return err
		}
		ex = a.Extraction()
	}
	return store.UpdateDoc(id, func(m *meeting.Meeting) error {
		applyExtraction(m, ex)
		m.Refined = true
		return nil
	})
}

// applyExtraction overwrites each field the extraction actually produced;
// absent fields leave the document unchanged. Action items keep the
// status of an existing item with the same (normalized) text, since
// status is user state.
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
		status := map[string]string{}
		for _, it := range m.ActionItems {
			if k := dedupeKey(it.Text); it.Status != "" && status[k] == "" {
				status[k] = it.Status
			}
		}
		items := make([]meeting.ActionItem, len(ex.ActionItems))
		for i, it := range ex.ActionItems {
			if it.Status == "" {
				it.Status = status[dedupeKey(it.Text)]
			}
			items[i] = it
		}
		m.ActionItems = items
	}
}

var (
	thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)
	isoDate    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// errEmptySummary marks a reply that parsed but carries no summary.
var errEmptySummary = errors.New("model reply has an empty summary")

// jsonObject strips reasoning blocks and ```json fences from a raw LLM
// reply, then slices from the first '{' to the last '}', tolerating
// surrounding prose.
func jsonObject(raw string) (string, error) {
	raw = stripFences(thinkBlock.ReplaceAllString(raw, ""))
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("no JSON object in model reply: %q", truncate(raw, 200))
	}
	return raw[start : end+1], nil
}

// parseReply parses an Ollama/OpenAI reply into an Analysis. The JSON is
// sanitized first (sanitizeReply), so a near-miss shape still parses;
// the fixes land in Issues. A reply with no summary is an error.
func parseReply(raw string) (Analysis, error) {
	obj, err := jsonObject(raw)
	if err != nil {
		return Analysis{}, err
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(obj), &generic); err != nil {
		return Analysis{}, fmt.Errorf("parse model JSON: %w", err)
	}
	issues := sanitizeReply(generic)
	clean, err := json.Marshal(generic)
	if err != nil {
		return Analysis{}, err
	}
	var a Analysis
	if err := json.Unmarshal(clean, &a); err != nil {
		return Analysis{}, fmt.Errorf("parse model JSON: %w", err)
	}
	a.Summary = strings.TrimSpace(a.Summary)
	if a.Summary == "" {
		return Analysis{}, errEmptySummary
	}
	a.Version = AnalysisVersion
	a.Issues = append(a.Issues, issues...)
	return a, nil
}

// parseExtraction unmarshals a four-field (cloud) reply, tolerating
// surrounding prose or fences.
func parseExtraction(raw string) (Extraction, error) {
	obj, err := jsonObject(raw)
	if err != nil {
		return Extraction{}, err
	}
	var ex Extraction
	if err := json.Unmarshal([]byte(obj), &ex); err != nil {
		return Extraction{}, fmt.Errorf("parse model JSON: %w", err)
	}
	return ex, nil
}

// parseCloudReply parses and normalizes a CatchM Cloud reply, then lifts
// it into an Analysis.
func parseCloudReply(raw string) (Analysis, error) {
	ex, err := parseExtraction(raw)
	if err != nil {
		return Analysis{}, err
	}
	ex = normalize(ex)
	if ex.Summary == "" {
		return Analysis{}, errEmptySummary
	}
	return liftExtraction(ex), nil
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

// normalize trims every field of a four-field extraction and drops
// empty entries and duplicate participants.
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
		if a.Text != "" {
			out.ActionItems = append(out.ActionItems, a)
		}
	}
	return out
}

// liftExtraction turns a four-field extraction into an Analysis with no
// topics and no evidence: participants become people, owners that name
// a participant become person keys, and a due that isn't YYYY-MM-DD
// becomes due_text.
func liftExtraction(ex Extraction) Analysis {
	a := Analysis{Version: AnalysisVersion, Summary: ex.Summary}
	who := map[string]string{}
	for _, name := range ex.Participants {
		key := "person:" + slug(name)
		if key == "person:" || hasPerson(a.People, key) {
			continue
		}
		who[nameKey(name)] = key
		a.People = append(a.People, Person{Key: key, Name: name})
	}
	for i, d := range ex.Decisions {
		a.Decisions = append(a.Decisions, AnalysisDecision{
			ID: fmt.Sprintf("d%d", i+1), Text: d.Text, Scope: d.Scope, Evidence: []int64{},
		})
	}
	for i, it := range ex.ActionItems {
		act := AnalysisAction{ID: fmt.Sprintf("a%d", i+1), Text: it.Text, Owner: it.Owner, Due: it.Due, Evidence: []int64{}}
		if key, ok := who[nameKey(it.Owner)]; ok {
			act.Owner = key
		}
		if act.Due != "" && !validDate(act.Due) {
			act.Due, act.DueText = "", act.Due
		}
		a.Actions = append(a.Actions, act)
	}
	return a
}

// errNoSummary is withRetry's user-facing message for summary calls.
const errNoSummary = "The model didn't return a usable summary. Try again, or pick a different model."

// withRetry calls call and parses its reply, retrying once when the
// reply isn't usable; a second unusable reply is a summary_failed error
// with message msg. Provider errors from call are returned as-is (they
// are already classified).
func withRetry[T any](call func() (string, error), parse func(string) (T, error), msg string) (T, error) {
	var zero T
	var lastErr error
	for range 2 {
		raw, err := call()
		if err != nil {
			return zero, err
		}
		v, err := parse(raw)
		if err == nil {
			return v, nil
		}
		lastErr = err
	}
	return zero, failed(msg, lastErr)
}

// renumber gives every topic, decision and action across parts a unique
// id (t1.., d1.., a1.. in order), rewriting each part's topic
// references, so partial analyses can be shown to a merge call side by
// side.
func renumber(parts []Analysis) []Analysis {
	out := make([]Analysis, len(parts))
	var nt, nd, na int
	for i, p := range parts {
		remap := map[string]string{}
		topics := make([]Topic, len(p.Topics))
		for j, t := range p.Topics {
			nt++
			id := fmt.Sprintf("t%d", nt)
			if k := trimIDs(t.ID); k != "" && remap[k] == "" {
				remap[k] = id
			}
			t.ID = id
			topics[j] = t
		}
		ref := func(r string) string {
			if id, ok := remap[trimIDs(r)]; ok {
				return id
			}
			return r // a key or title; Validate resolves it
		}
		decisions := make([]AnalysisDecision, len(p.Decisions))
		for j, d := range p.Decisions {
			nd++
			d.ID, d.Topic = fmt.Sprintf("d%d", nd), ref(d.Topic)
			decisions[j] = d
		}
		actions := make([]AnalysisAction, len(p.Actions))
		for j, act := range p.Actions {
			na++
			act.ID, act.Topic = fmt.Sprintf("a%d", na), ref(act.Topic)
			actions[j] = act
		}
		p.Topics, p.Decisions, p.Actions = topics, decisions, actions
		out[i] = p
	}
	return out
}

// mergeAnalyses combines per-chunk analyses deterministically: summaries
// joined in order; topics, people and entities merged by normalized key
// (topic ids remapped); decisions and actions deduplicated by their
// normalized text, a kept item filling its missing fields and evidence
// from its duplicates. Ids are renumbered t1.., d1.., a1...
func mergeAnalyses(parts []Analysis) Analysis {
	var out Analysis
	out.Version = AnalysisVersion
	var summaries []string
	topicAt, personAt, entityAt := map[string]int{}, map[string]int{}, map[string]int{}
	decisionAt, actionAt := map[string]int{}, map[string]int{}
	for _, p := range parts {
		if s := strings.TrimSpace(p.Summary); s != "" {
			summaries = append(summaries, s)
		}
		remap := map[string]string{}
		for _, t := range p.Topics {
			_, body := splitKey(t.Key)
			if body == "" {
				body = slug(t.Title)
			}
			k := "topic:" + body
			origID := trimIDs(t.ID)
			var id string
			if i, ok := topicAt[k]; ok {
				out.Topics[i].Evidence = unionSeqs(out.Topics[i].Evidence, t.Evidence)
				id = out.Topics[i].ID
			} else {
				id = fmt.Sprintf("t%d", len(out.Topics)+1)
				topicAt[k] = len(out.Topics)
				t.ID, t.Evidence = id, unionSeqs(nil, t.Evidence)
				out.Topics = append(out.Topics, t)
			}
			for _, alias := range []string{origID, k, nameKey(t.Title)} {
				if alias != "" && remap[alias] == "" {
					remap[alias] = id
				}
			}
		}
		topicRef := func(r string) string {
			if r = strings.TrimSpace(r); r == "" {
				return ""
			}
			for _, alias := range []string{trimIDs(r), nameKey(r)} {
				if id, ok := remap[alias]; ok {
					return id
				}
			}
			if _, body := splitKey(r); body != "" {
				if id, ok := remap["topic:"+body]; ok {
					return id
				}
			}
			out.Issues = append(out.Issues, fmt.Sprintf("dropped: topic reference %q (unknown topic)", r))
			return ""
		}
		for _, person := range p.People {
			_, body := splitKey(person.Key)
			if body == "" {
				body = slug(person.Name)
			}
			k := "person:" + body
			if i, ok := personAt[k]; ok {
				kept := &out.People[i]
				kept.Aliases = cleanAliases(append(append(kept.Aliases, person.Name), person.Aliases...), kept.Name)
				if kept.Speaker == "" {
					kept.Speaker = person.Speaker
				}
				continue
			}
			personAt[k] = len(out.People)
			out.People = append(out.People, person)
		}
		for _, e := range p.Entities {
			k := strings.ToLower(strings.TrimSpace(e.Key))
			if k == "" {
				k = strings.ToLower(e.Kind) + ":" + slug(e.Name)
			}
			if i, ok := entityAt[k]; ok {
				out.Entities[i].Evidence = unionSeqs(out.Entities[i].Evidence, e.Evidence)
				continue
			}
			entityAt[k] = len(out.Entities)
			e.Evidence = unionSeqs(nil, e.Evidence)
			out.Entities = append(out.Entities, e)
		}
		for _, d := range p.Decisions {
			k := dedupeKey(d.Text)
			if k == "" {
				continue
			}
			d.Topic = topicRef(d.Topic)
			if i, ok := decisionAt[k]; ok {
				kept := &out.Decisions[i]
				kept.Scope = cmp.Or(kept.Scope, d.Scope)
				kept.Topic = cmp.Or(kept.Topic, d.Topic)
				kept.Evidence = unionSeqs(kept.Evidence, d.Evidence)
				continue
			}
			decisionAt[k] = len(out.Decisions)
			d.ID, d.Evidence = fmt.Sprintf("d%d", len(out.Decisions)+1), unionSeqs(nil, d.Evidence)
			out.Decisions = append(out.Decisions, d)
		}
		for _, act := range p.Actions {
			k := dedupeKey(act.Text)
			if k == "" {
				continue
			}
			act.Topic = topicRef(act.Topic)
			if i, ok := actionAt[k]; ok {
				kept := &out.Actions[i]
				kept.Owner = cmp.Or(kept.Owner, act.Owner)
				kept.Due = cmp.Or(kept.Due, act.Due)
				kept.DueText = cmp.Or(kept.DueText, act.DueText)
				kept.Topic = cmp.Or(kept.Topic, act.Topic)
				kept.Evidence = unionSeqs(kept.Evidence, act.Evidence)
				continue
			}
			actionAt[k] = len(out.Actions)
			act.ID, act.Evidence = fmt.Sprintf("a%d", len(out.Actions)+1), unionSeqs(nil, act.Evidence)
			out.Actions = append(out.Actions, act)
		}
		out.Issues = append(out.Issues, p.Issues...)
	}
	out.Summary = strings.Join(summaries, " ")
	return out
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
