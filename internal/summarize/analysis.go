package summarize

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spacingmind/catchm/internal/meeting"
)

// AnalysisVersion is analysis.json's schema version.
const AnalysisVersion = 1

// Analysis is a meeting's analysis.json (v1): the canonical, richer
// summary output. The frontmatter's summary, participants, decisions and
// action items are projected from it (Extraction).
type Analysis struct {
	Version   int                `json:"version"`
	Generated GeneratedInfo      `json:"generated"`
	Summary   string             `json:"summary"`
	Topics    []Topic            `json:"topics"`
	Decisions []AnalysisDecision `json:"decisions"`
	Actions   []AnalysisAction   `json:"actions"`
	People    []Person           `json:"people"`
	Entities  []Entity           `json:"entities"`
	Flows     []Flow             `json:"flows"`
	Issues    []string           `json:"issues"`
}

// GeneratedInfo records how an Analysis was produced.
type GeneratedInfo struct {
	// At is an RFC3339 time.
	At       string `json:"at,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Transcript is the transcript read: "v2" (batch) or "live".
	Transcript string `json:"transcript,omitempty"`
	// AnalysisLang is the meeting's language hint, or "auto".
	AnalysisLang string `json:"analysis_lang,omitempty"`
}

// Topic is one thing the meeting discussed. Key is a canonical English
// slug ("topic:pricing-page") that links meetings in any language.
type Topic struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Key      string  `json:"key"`
	Evidence []int64 `json:"evidence"`
}

// AnalysisDecision is a decision with its topic id and evidence seqs.
type AnalysisDecision struct {
	ID       string  `json:"id"`
	Text     string  `json:"text"`
	Topic    string  `json:"topic,omitempty"`
	Scope    string  `json:"scope,omitempty"`
	Evidence []int64 `json:"evidence"`
}

// AnalysisAction is an action item. Owner is a Person key when the owner
// is a listed person, else the owner's name as plain text. Due is set
// only for a YYYY-MM-DD date; any other stated deadline is DueText.
type AnalysisAction struct {
	ID       string  `json:"id"`
	Text     string  `json:"text"`
	Topic    string  `json:"topic,omitempty"`
	Owner    string  `json:"owner,omitempty"`
	Due      string  `json:"due,omitempty"`
	DueText  string  `json:"due_text,omitempty"`
	Evidence []int64 `json:"evidence"`
}

// Person is someone in the meeting; Key is "person:<slug>".
type Person struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	// Speaker is the transcript's speaker label, when known.
	Speaker string `json:"speaker,omitempty"`
}

// Entity is a named thing the meeting mentions; Key is "<kind>:<slug>".
type Entity struct {
	Key string `json:"key"`
	// Name is the display name, in the meeting's language.
	Name string `json:"name"`
	// Kind is one of project, product, org, tool, term, place.
	Kind     string  `json:"kind"`
	Evidence []int64 `json:"evidence"`
}

// Flow is a process the meeting walked through (a separate, on-demand
// pass; empty until it runs).
type Flow struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Actor    string     `json:"actor,omitempty"`
	Trigger  string     `json:"trigger,omitempty"`
	Evidence []int64    `json:"evidence"`
	Steps    []FlowStep `json:"steps"`
}

// FlowStep is one step of a Flow, in explicit Order.
type FlowStep struct {
	ID    string     `json:"id"`
	Order int        `json:"order"`
	Text  string     `json:"text"`
	Actor string     `json:"actor,omitempty"`
	Next  []FlowNext `json:"next,omitempty"`
}

// FlowNext is an edge to another step of the same flow, optionally
// conditional (a branch).
type FlowNext struct {
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

// MindNode is one node of the derived mind map. Ref is "topic:<id>",
// "decision:<id>" or "action:<id>"; it is empty for the root and for the
// "Other" branch.
type MindNode struct {
	Label    string     `json:"label"`
	Ref      string     `json:"ref,omitempty"`
	Children []MindNode `json:"children,omitempty"`
}

// otherBranch labels the mind-map branch for items with no topic.
const otherBranch = "Other"

// BuildMindMap derives the mind map: the meeting at the root, one branch
// per topic, and each topic's decisions then actions under it. Items
// without a (known) topic go under an "Other" branch, added only when
// there are some.
func BuildMindMap(a Analysis, title string) MindNode {
	root := MindNode{Label: strings.TrimSpace(title)}
	if root.Label == "" {
		root.Label = "Meeting"
	}
	branch := map[string]int{}
	for _, t := range a.Topics {
		if t.ID == "" {
			continue
		}
		if _, dup := branch[t.ID]; dup {
			continue
		}
		branch[t.ID] = len(root.Children)
		root.Children = append(root.Children, MindNode{Label: t.Title, Ref: "topic:" + t.ID})
	}
	var other []MindNode
	add := func(topic string, n MindNode) {
		if i, ok := branch[topic]; ok {
			root.Children[i].Children = append(root.Children[i].Children, n)
			return
		}
		other = append(other, n)
	}
	for _, d := range a.Decisions {
		add(d.Topic, MindNode{Label: d.Text, Ref: "decision:" + d.ID})
	}
	for _, act := range a.Actions {
		add(act.Topic, MindNode{Label: act.Text, Ref: "action:" + act.ID})
	}
	if len(other) > 0 {
		root.Children = append(root.Children, MindNode{Label: otherBranch, Children: other})
	}
	return root
}

// Extraction projects a onto the frontmatter fields. Owners that are
// person keys become the person's name; a DueText deadline is appended
// to the item's text, as the frontmatter has no field for it. Status is
// left empty: applyExtraction carries existing statuses over by text.
func (a Analysis) Extraction() Extraction {
	ex := Extraction{Summary: a.Summary}
	names := map[string]string{}
	for _, p := range a.People {
		if p.Name == "" {
			continue
		}
		names[p.Key] = p.Name
		ex.Participants = append(ex.Participants, p.Name)
	}
	for _, d := range a.Decisions {
		ex.Decisions = append(ex.Decisions, meeting.Decision{Text: d.Text, Scope: d.Scope})
	}
	for _, act := range a.Actions {
		owner := act.Owner
		if n, ok := names[owner]; ok {
			owner = n
		}
		text := act.Text
		if act.Due == "" && act.DueText != "" {
			text = fmt.Sprintf("%s (due: %s)", text, act.DueText)
		}
		ex.ActionItems = append(ex.ActionItems, meeting.ActionItem{Text: text, Owner: owner, Due: act.Due})
	}
	return ex
}

// Entity kinds, as stored in Entity.Kind and key prefixes.
var entityKinds = map[string]bool{
	"project": true, "product": true, "org": true, "tool": true, "term": true, "place": true,
}

// typeAliases maps the key prefixes and kinds a model may write
// (lowercased, diacritics folded, '_'/'-' as spaces) to canonical types:
// person, topic, or an entity kind.
var typeAliases = map[string]string{
	"person": "person", "people": "person", "human": "person", "speaker": "person",
	"participant": "person", "attendee": "person", "member": "person", "user": "person",
	"nguoi": "person", "thanh vien": "person",

	"topic": "topic", "theme": "topic", "subject": "topic", "tag": "topic", "category": "topic",
	"chu de": "topic",

	"project": "project", "initiative": "project", "du an": "project",

	"product": "product", "app": "product", "application": "product", "feature": "product",
	"san pham": "product",

	"org": "org", "organization": "org", "organisation": "org", "company": "org",
	"team": "org", "client": "org", "customer": "org", "vendor": "org", "partner": "org",
	"cong ty": "org", "to chuc": "org", "khach hang": "org", "doi tac": "org",

	"tool": "tool", "software": "tool", "service": "tool", "platform": "tool",
	"library": "tool", "framework": "tool", "system": "tool", "cong cu": "tool", "he thong": "tool",

	"term": "term", "concept": "term", "glossary": "term", "acronym": "term",
	"jargon": "term", "thuat ngu": "term", "khai niem": "term",

	"place": "place", "location": "place", "city": "place", "country": "place",
	"office": "place", "dia diem": "place", "noi": "place",
}

// canonicalType maps s through typeAliases; "" when unknown.
func canonicalType(s string) string {
	s = strings.NewReplacer("_", " ", "-", " ").Replace(foldString(strings.ToLower(s)))
	return typeAliases[strings.Join(strings.Fields(s), " ")]
}

// splitKey splits a "type:slug" key into its canonical type ("" when the
// prefix is missing or unknown) and slugged body. With an unknown prefix
// the whole key is the body.
func splitKey(key string) (typ, body string) {
	key = strings.TrimSpace(key)
	if i := strings.IndexByte(key, ':'); i >= 0 {
		if t := canonicalType(key[:i]); t != "" {
			return t, slug(key[i+1:])
		}
	}
	return "", slug(key)
}

// slug lowercases s, folds Vietnamese/Latin diacritics, and joins its
// letter and digit runs with '-'.
func slug(s string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(s) {
		r = foldRune(r)
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			gap = true
			continue
		}
		if gap && b.Len() > 0 {
			b.WriteByte('-')
		}
		gap = false
		b.WriteRune(r)
	}
	return b.String()
}

// diacriticFolds lists, per ASCII base letter, the lowercase accented
// forms folded onto it (Vietnamese plus common Latin-1).
var diacriticFolds = map[rune]string{
	'a': "àáảãạăằắẳẵặâầấẩẫậäå",
	'e': "èéẻẽẹêềếểễệë",
	'i': "ìíỉĩịîï",
	'o': "òóỏõọôồốổỗộơờớởỡợöø",
	'u': "ùúủũụưừứửữựûü",
	'y': "ỳýỷỹỵÿ",
	'd': "đ",
	'c': "ç",
	'n': "ñ",
}

var foldTable = func() map[rune]rune {
	t := map[rune]rune{}
	for base, forms := range diacriticFolds {
		for _, r := range forms {
			t[r] = base
		}
	}
	return t
}()

func foldRune(r rune) rune {
	if f, ok := foldTable[r]; ok {
		return f
	}
	return r
}

func foldString(s string) string { return strings.Map(foldRune, s) }

// nameKey is the case-, space- and diacritic-insensitive form used to
// match names, aliases and titles.
func nameKey(s string) string {
	return strings.Join(strings.Fields(foldString(strings.ToLower(s))), " ")
}

// validDate reports whether s is a real YYYY-MM-DD date.
func validDate(s string) bool {
	if !isoDate.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// Validate is the tolerant validator: it repairs what it can in a and
// drops what it can't, instead of rejecting the whole analysis. It:
//   - trims every string and drops items left empty;
//   - normalizes keys to lowercase "type:slug", mapping alias types
//     (company→org, people→person, "dự án"→project, ...);
//   - dedupes ids (and topics, people and entities by key);
//   - drops evidence seqs not in validSeqs (a nil map accepts none);
//   - resolves topic references by id, key or title and person
//     references by key, name, alias or speaker, dropping unknown ones
//     (an unknown owner is kept as plain text);
//   - keeps due only as a YYYY-MM-DD date, else moves it to due_text.
//
// Each fix is logged; the log is returned and appended to a.Issues.
func Validate(a *Analysis, validSeqs map[int64]bool) []string {
	v := &validator{seqs: validSeqs}
	if a.Version == 0 {
		a.Version = AnalysisVersion
	}
	a.Summary = strings.TrimSpace(a.Summary)
	a.People = v.people(a.People)
	a.Entities = v.entities(a.Entities, &a.People)
	a.Topics = v.topics(a.Topics)
	a.Decisions = v.decisions(a.Decisions)
	a.Actions = v.actions(a.Actions, a.People)
	a.Flows = v.flows(a.Flows)
	if a.Issues == nil {
		a.Issues = []string{}
	}
	a.Issues = append(a.Issues, v.issues...)
	if v.issues == nil {
		return []string{}
	}
	return v.issues
}

type validator struct {
	seqs   map[int64]bool
	issues []string
	// topic lookups, filled by topics: id, key and title → final id.
	topicByID, topicByKey, topicByTitle map[string]string
}

func (v *validator) fixed(format string, args ...any) {
	v.issues = append(v.issues, "auto-corrected: "+fmt.Sprintf(format, args...))
}

func (v *validator) dropped(format string, args ...any) {
	v.issues = append(v.issues, "dropped: "+fmt.Sprintf(format, args...))
}

// evidence keeps the seqs of in that exist, once each, in order.
func (v *validator) evidence(where string, in []int64) []int64 {
	out := []int64{}
	seen := map[int64]bool{}
	var bad []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		if !v.seqs[s] {
			bad = append(bad, strconv.FormatInt(s, 10))
			continue
		}
		out = append(out, s)
	}
	if len(bad) > 0 {
		v.dropped("%s evidence %s (not a transcript line)", where, strings.Join(bad, ", "))
	}
	return out
}

func unionSeqs(a, b []int64) []int64 {
	seen := map[int64]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			a = append(a, s)
		}
	}
	return a
}

// assignID returns id when it is non-empty and free, else the next free
// "<prefix>N", logging the change. It marks the result taken.
func (v *validator) assignID(where, id, prefix string, taken map[string]bool) string {
	if id != "" && !taken[id] {
		taken[id] = true
		return id
	}
	n := 1
	for taken[prefix+strconv.Itoa(n)] {
		n++
	}
	next := prefix + strconv.Itoa(n)
	taken[next] = true
	if id == "" {
		v.fixed("%s missing id, set to %q", where, next)
	} else {
		v.fixed("%s duplicate id %q, renamed %q", where, id, next)
	}
	return next
}

func trimIDs(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func (v *validator) people(in []Person) []Person {
	out := []Person{}
	byKey := map[string]int{}
	for i, p := range in {
		where := fmt.Sprintf("people[%d]", i)
		p.Name, p.Speaker = strings.TrimSpace(p.Name), strings.TrimSpace(p.Speaker)
		orig := strings.TrimSpace(p.Key)
		_, body := splitKey(orig)
		if body == "" {
			body = slug(p.Name)
		}
		if body == "" {
			v.dropped("%s: no name or key", where)
			continue
		}
		if p.Name == "" {
			p.Name = strings.ReplaceAll(body, "-", " ")
			v.fixed("%s missing name, set to %q", where, p.Name)
		}
		p.Key = "person:" + body
		if orig == "" {
			v.fixed("%s (%q) missing key, set to %q", where, p.Name, p.Key)
		} else if orig != p.Key {
			v.fixed("%s key %q normalized to %q", where, orig, p.Key)
		}
		p.Aliases = cleanAliases(p.Aliases, p.Name)
		if j, ok := byKey[p.Key]; ok {
			kept := &out[j]
			kept.Aliases = cleanAliases(append(append(kept.Aliases, p.Name), p.Aliases...), kept.Name)
			if kept.Speaker == "" {
				kept.Speaker = p.Speaker
			}
			v.fixed("%s duplicate %q merged into the earlier entry", where, p.Key)
			continue
		}
		byKey[p.Key] = len(out)
		out = append(out, p)
	}
	return out
}

// cleanAliases trims and dedupes aliases, dropping empties and the name.
func cleanAliases(in []string, name string) []string {
	var out []string
	seen := map[string]bool{nameKey(name): true}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[nameKey(s)] {
			continue
		}
		seen[nameKey(s)] = true
		out = append(out, s)
	}
	return out
}

// entities normalizes kinds and keys; entities whose kind means a person
// move into people (when not already listed).
func (v *validator) entities(in []Entity, people *[]Person) []Entity {
	out := []Entity{}
	byKey := map[string]int{}
	for i, e := range in {
		where := fmt.Sprintf("entities[%d]", i)
		e.Name = strings.TrimSpace(e.Name)
		origKey, origKind := strings.TrimSpace(e.Key), strings.ToLower(strings.TrimSpace(e.Kind))
		keyType, body := splitKey(origKey)
		if body == "" {
			body = slug(e.Name)
		}
		if body == "" {
			v.dropped("%s: no name or key", where)
			continue
		}
		if e.Name == "" {
			e.Name = strings.ReplaceAll(body, "-", " ")
			v.fixed("%s missing name, set to %q", where, e.Name)
		}
		kind := canonicalType(origKind)
		if kind == "" && keyType != "" {
			kind = keyType
		}
		if kind == "person" {
			key := "person:" + body
			if !hasPerson(*people, key) {
				*people = append(*people, Person{Key: key, Name: e.Name})
			}
			v.fixed("%s (%q) is a person, moved to people as %q", where, e.Name, key)
			continue
		}
		switch {
		case !entityKinds[kind]:
			kind = "term"
			v.fixed("%s (%q) kind %q unknown, set to \"term\"", where, e.Name, origKind)
		case kind != origKind:
			v.fixed("%s (%q) kind %q mapped to %q", where, e.Name, origKind, kind)
		}
		e.Kind = kind
		e.Key = kind + ":" + body
		if origKey != "" && origKey != e.Key {
			v.fixed("%s key %q normalized to %q", where, origKey, e.Key)
		} else if origKey == "" {
			v.fixed("%s (%q) missing key, set to %q", where, e.Name, e.Key)
		}
		e.Evidence = v.evidence(where, e.Evidence)
		if j, ok := byKey[e.Key]; ok {
			out[j].Evidence = unionSeqs(out[j].Evidence, e.Evidence)
			v.fixed("%s duplicate %q merged into the earlier entry", where, e.Key)
			continue
		}
		byKey[e.Key] = len(out)
		out = append(out, e)
	}
	return out
}

func hasPerson(people []Person, key string) bool {
	for _, p := range people {
		if p.Key == key {
			return true
		}
	}
	return false
}

func (v *validator) topics(in []Topic) []Topic {
	out := []Topic{}
	v.topicByID, v.topicByKey, v.topicByTitle = map[string]string{}, map[string]string{}, map[string]string{}
	byKey := map[string]int{}
	taken := map[string]bool{}
	for i, t := range in {
		where := fmt.Sprintf("topics[%d]", i)
		t.Title = strings.TrimSpace(t.Title)
		id := trimIDs(t.ID)
		if t.Title == "" {
			v.dropped("%s (%q): empty title", where, id)
			continue
		}
		orig := strings.TrimSpace(t.Key)
		_, body := splitKey(orig)
		if body == "" {
			body = slug(t.Title)
		}
		if body == "" {
			body = slug(id)
		}
		t.Key = "topic:" + body
		if orig == "" {
			v.fixed("%s (%q) missing key, set to %q", where, t.Title, t.Key)
		} else if orig != t.Key {
			v.fixed("%s key %q normalized to %q", where, orig, t.Key)
		}
		t.Evidence = v.evidence(where, t.Evidence)
		if j, ok := byKey[t.Key]; ok {
			out[j].Evidence = unionSeqs(out[j].Evidence, t.Evidence)
			if id != "" {
				if _, known := v.topicByID[id]; !known {
					v.topicByID[id] = out[j].ID
				}
			}
			v.fixed("%s duplicate %q merged into topic %q", where, t.Key, out[j].ID)
			continue
		}
		t.ID = v.assignID(where, id, "t", taken)
		if id == "" {
			id = t.ID
		}
		if _, known := v.topicByID[id]; !known {
			v.topicByID[id] = t.ID
		}
		v.topicByID[t.ID] = t.ID
		v.topicByKey[t.Key] = t.ID
		if k := nameKey(t.Title); v.topicByTitle[k] == "" {
			v.topicByTitle[k] = t.ID
		}
		byKey[t.Key] = len(out)
		out = append(out, t)
	}
	return out
}

// topicRef resolves a decision's or action's topic reference.
func (v *validator) topicRef(where, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if id, ok := v.topicByID[strings.ToLower(ref)]; ok {
		if id != ref {
			v.fixed("%s topic %q resolved to %q", where, ref, id)
		}
		return id
	}
	if typ, body := splitKey(ref); body != "" && (typ == "topic" || typ == "") {
		if id, ok := v.topicByKey["topic:"+body]; ok {
			v.fixed("%s topic %q resolved to %q", where, ref, id)
			return id
		}
	}
	if id, ok := v.topicByTitle[nameKey(ref)]; ok {
		v.fixed("%s topic %q resolved to %q", where, ref, id)
		return id
	}
	v.dropped("%s topic %q (unknown topic)", where, ref)
	return ""
}

func (v *validator) decisions(in []AnalysisDecision) []AnalysisDecision {
	out := []AnalysisDecision{}
	taken := map[string]bool{}
	for i, d := range in {
		where := fmt.Sprintf("decisions[%d]", i)
		d.Text, d.Scope = strings.TrimSpace(d.Text), strings.TrimSpace(d.Scope)
		if d.Text == "" {
			v.dropped("%s: empty text", where)
			continue
		}
		d.ID = v.assignID(where, trimIDs(d.ID), "d", taken)
		d.Topic = v.topicRef(where, d.Topic)
		d.Evidence = v.evidence(where, d.Evidence)
		out = append(out, d)
	}
	return out
}

func (v *validator) actions(in []AnalysisAction, people []Person) []AnalysisAction {
	out := []AnalysisAction{}
	taken := map[string]bool{}
	who := personIndex(people)
	for i, act := range in {
		where := fmt.Sprintf("actions[%d]", i)
		act.Text = strings.TrimSpace(act.Text)
		if act.Text == "" {
			v.dropped("%s: empty text", where)
			continue
		}
		act.ID = v.assignID(where, trimIDs(act.ID), "a", taken)
		act.Topic = v.topicRef(where, act.Topic)
		act.Owner = v.owner(where, strings.TrimSpace(act.Owner), who)
		act.Due, act.DueText = strings.TrimSpace(act.Due), strings.TrimSpace(act.DueText)
		if act.Due != "" && !validDate(act.Due) {
			if act.DueText == "" {
				act.DueText = act.Due
				v.fixed("%s due %q is not YYYY-MM-DD, moved to due_text", where, act.Due)
			} else {
				v.dropped("%s due %q (not YYYY-MM-DD; due_text %q kept)", where, act.Due, act.DueText)
			}
			act.Due = ""
		}
		act.Evidence = v.evidence(where, act.Evidence)
		out = append(out, act)
	}
	return out
}

// personIndex maps each person's key, slugged key body, name, aliases
// and speaker label (all nameKey'd) to the person's key.
func personIndex(people []Person) map[string]string {
	who := map[string]string{}
	put := func(k, key string) {
		if k = nameKey(k); k != "" && who[k] == "" {
			who[k] = key
		}
	}
	for _, p := range people {
		put(p.Key, p.Key)
	}
	for _, p := range people {
		put(strings.TrimPrefix(p.Key, "person:"), p.Key)
		put(p.Name, p.Key)
		put(p.Speaker, p.Key)
		for _, al := range p.Aliases {
			put(al, p.Key)
		}
	}
	return who
}

// owner resolves an action's owner to a person key; an unknown owner is
// kept as plain text (a key's slug as words).
func (v *validator) owner(where, owner string, who map[string]string) string {
	if owner == "" {
		return ""
	}
	if key, ok := who[nameKey(owner)]; ok {
		if key != owner {
			v.fixed("%s owner %q resolved to %q", where, owner, key)
		}
		return key
	}
	typ, body := splitKey(owner)
	if typ == "person" || (typ == "" && strings.Contains(owner, ":")) {
		if key, ok := who[body]; ok {
			v.fixed("%s owner %q resolved to %q", where, owner, key)
			return key
		}
		text := strings.ReplaceAll(body, "-", " ")
		v.dropped("%s owner reference %q (not a listed person; kept as text %q)", where, owner, text)
		return text
	}
	v.dropped("%s owner reference %q (not a listed person; kept as text)", where, owner)
	return owner
}

func (v *validator) flows(in []Flow) []Flow {
	out := []Flow{}
	taken := map[string]bool{}
	for i, f := range in {
		where := fmt.Sprintf("flows[%d]", i)
		f.Name, f.Actor, f.Trigger = strings.TrimSpace(f.Name), strings.TrimSpace(f.Actor), strings.TrimSpace(f.Trigger)
		if f.Name == "" {
			v.dropped("%s: empty name", where)
			continue
		}
		f.ID = v.assignID(where, trimIDs(f.ID), "f", taken)
		f.Evidence = v.evidence(where, f.Evidence)
		f.Steps = v.steps(where, f.Steps)
		out = append(out, f)
	}
	return out
}

func (v *validator) steps(flow string, in []FlowStep) []FlowStep {
	out := []FlowStep{}
	taken := map[string]bool{}
	for i, s := range in {
		where := fmt.Sprintf("%s.steps[%d]", flow, i)
		s.Text, s.Actor = strings.TrimSpace(s.Text), strings.TrimSpace(s.Actor)
		if s.Text == "" {
			v.dropped("%s: empty text", where)
			continue
		}
		s.ID = v.assignID(where, trimIDs(s.ID), "s", taken)
		if s.Order <= 0 {
			s.Order = len(out) + 1
			v.fixed("%s missing order, set to %d", where, s.Order)
		}
		out = append(out, s)
	}
	for i := range out {
		var next []FlowNext
		for _, n := range out[i].Next {
			n.To, n.Condition = trimIDs(n.To), strings.TrimSpace(n.Condition)
			if !taken[n.To] || n.To == out[i].ID {
				v.dropped("%s.steps[%s] next %q (unknown step)", flow, out[i].ID, n.To)
				continue
			}
			next = append(next, n)
		}
		out[i].Next = next
	}
	// Stable insertion sort by order: steps are few.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Order < out[j-1].Order; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
