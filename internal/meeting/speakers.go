package meeting

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Speaker identity (docs/plans/active/speaker-identity.md). Engines label
// speakers per meeting ("1", "spk_1", "Speaker 1", "S1"); without labels
// the input an utterance came through still says whether it was the user
// (mic) or the other side of a call (system). A speaker key names one of
// those, and the frontmatter's speakers map gives keys confirmed names.

// Keys for utterances that have no engine label but a known source.
const (
	SpeakerMe     = "me"
	SpeakerOthers = "others"
)

// Guest is one person invited to the meeting, pasted from a calendar.
type Guest struct {
	Name  string `yaml:"name,omitempty" json:"name,omitempty"`
	Email string `yaml:"email,omitempty" json:"email,omitempty"`
}

// Limits on what a meeting stores.
const (
	MaxGuests      = 200
	MaxSpeakerName = 80
)

var errSpeakerName = fmt.Errorf("a speaker name has at most %d characters", MaxSpeakerName)

var labelNumber = regexp.MustCompile(`^(?i:speaker[ _]?|spk[_ ]?|s)?(\d{1,3})$`)

// SpeakerKey is the key an utterance's speaker is named under: the
// engine's label reduced to its number ("Speaker 2", "spk_2", "S2" → "2"),
// else SpeakerMe/SpeakerOthers from its source, else "".
func SpeakerKey(u Utterance) string {
	if l := strings.TrimSpace(u.Speaker); l != "" {
		if m := labelNumber.FindStringSubmatch(l); m != nil {
			if n := strings.TrimLeft(m[1], "0"); n != "" {
				return n
			}
			return "0"
		}
		return l
	}
	switch u.Source {
	case "mic":
		return SpeakerMe
	case "system":
		return SpeakerOthers
	}
	return ""
}

// SpeakerNamer turns speaker keys into the names people read. The
// default wording is English; exports in other languages swap it.
type SpeakerNamer struct {
	Names   map[string]string // confirmed names (Meeting.Speakers)
	Me      string            // the user's own name (config profile.name)
	Wording SpeakerWording
}

// SpeakerWording is how unnamed speakers read.
type SpeakerWording struct {
	Me      string // "Me"
	Others  string // "Others"
	Speaker string // "Speaker %s"
}

// EnglishWording is the default.
var EnglishWording = SpeakerWording{Me: "Me", Others: "Others", Speaker: "Speaker %s"}

// SetMe tells the store where the user's own name (config profile.name)
// comes from, so every reader names the "me" speaker the same way.
func (s *Store) SetMe(f func() string) { s.me.Store(&f) }

// Namer names m's speakers for any reader of this store.
func (s *Store) Namer(m *Meeting) SpeakerNamer {
	me := ""
	if f := s.me.Load(); f != nil {
		me = (*f)()
	}
	return m.Namer(me)
}

// Namer builds the meeting's namer; me is the user's profile name.
func (m *Meeting) Namer(me string) SpeakerNamer {
	return SpeakerNamer{Names: m.Speakers, Me: me, Wording: EnglishWording}
}

// Label is the name for key: confirmed, else the user's own name for
// SpeakerMe, else a generic label. "" for "".
func (n SpeakerNamer) Label(key string) string {
	if key == "" {
		return ""
	}
	if name := strings.TrimSpace(n.Names[key]); name != "" {
		return name
	}
	switch key {
	case SpeakerMe:
		if n.Me != "" {
			return n.Me
		}
		return n.Wording.Me
	case SpeakerOthers:
		return n.Wording.Others
	}
	if isDigits(key) {
		return fmt.Sprintf(n.Wording.Speaker, key)
	}
	return key
}

// Name is Label for an utterance.
func (n SpeakerNamer) Name(u Utterance) string { return n.Label(SpeakerKey(u)) }

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// SpeakerStat is one speaker key's share of a transcript.
type SpeakerStat struct {
	Key    string `json:"key"`
	Lines  int    `json:"lines"`
	Mic    int    `json:"mic,omitempty"`    // lines the microphone carried
	System int    `json:"system,omitempty"` // lines computer audio carried
	First  int64  `json:"first_seq"`        // where they first speak
}

// SpeakerStats lists the transcript's speaker keys in order of first
// appearance.
func SpeakerStats(us []Utterance) []SpeakerStat {
	idx := map[string]int{}
	var out []SpeakerStat
	for _, u := range us {
		k := SpeakerKey(u)
		if k == "" || strings.TrimSpace(u.Text) == "" {
			continue
		}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, SpeakerStat{Key: k, First: u.Seq})
		}
		out[i].Lines++
		switch u.Source {
		case "mic":
			out[i].Mic++
		case "system":
			out[i].System++
		}
	}
	return out
}

// CleanSpeakerNames normalizes a speakers map: trimmed keys and names,
// blank names dropped (that un-names a speaker), nil when empty.
func CleanSpeakerNames(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.Join(strings.Fields(v), " ")
		if k == "" || v == "" {
			continue
		}
		if utf8.RuneCountInString(v) > MaxSpeakerName {
			return nil, errSpeakerName
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SetSpeakerNames merges names into the meeting's speakers map: a blank
// name removes that key.
func (s *Store) SetSpeakerNames(id string, names map[string]string) error {
	return s.UpdateDoc(id, func(m *Meeting) error {
		merged := map[string]string{}
		for k, v := range m.Speakers {
			merged[k] = v
		}
		for k, v := range names {
			merged[strings.TrimSpace(k)] = v
		}
		clean, err := CleanSpeakerNames(merged)
		if err != nil {
			return err
		}
		m.Speakers = clean
		return nil
	})
}

// SetGuests replaces the meeting's guest list (normalized, deduplicated).
func (s *Store) SetGuests(id string, guests []Guest) error {
	clean, err := CleanGuests(guests)
	if err != nil {
		return err
	}
	return s.UpdateDoc(id, func(m *Meeting) error {
		m.Guests = clean
		return nil
	})
}

// ErrTooManyGuests is returned for a list over MaxGuests.
var ErrTooManyGuests = fmt.Errorf("a meeting has at most %d guests", MaxGuests)

// CleanGuests trims names, lowercases emails, drops empty entries and
// invalid emails' email part, and merges duplicates (same email, or same
// name when neither has an email). A later entry fills in a missing
// name.
func CleanGuests(in []Guest) ([]Guest, error) {
	var out []Guest
	byEmail := map[string]int{}
	byName := map[string]int{}
	for _, g := range in {
		g.Name = strings.Join(strings.Fields(g.Name), " ")
		g.Email = strings.ToLower(strings.TrimSpace(g.Email))
		if g.Email != "" && !validEmail(g.Email) {
			g.Email = ""
		}
		if utf8.RuneCountInString(g.Name) > MaxSpeakerName {
			return nil, errSpeakerName
		}
		if g.Name == "" && g.Email == "" {
			continue
		}
		if g.Email != "" {
			if i, ok := byEmail[g.Email]; ok {
				if out[i].Name == "" {
					out[i].Name = g.Name
				}
				continue
			}
		} else if i, ok := byName[strings.ToLower(g.Name)]; ok && out[i].Email == "" {
			continue
		}
		if g.Email != "" {
			byEmail[g.Email] = len(out)
		}
		if g.Name != "" {
			byName[strings.ToLower(g.Name)] = len(out)
		}
		out = append(out, g)
	}
	if len(out) > MaxGuests {
		return nil, ErrTooManyGuests
	}
	return out, nil
}

func validEmail(e string) bool {
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e && strings.Contains(e[strings.LastIndex(e, "@"):], ".")
}

var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-']+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// Calendar extras that aren't part of a name.
	guestNoise = regexp.MustCompile(`(?i)\s*[\(\[]?\b(organi[sz]er|optional|required|host|guest|accepted|declined|tentative|awaiting|maybe|yes|no|chủ trì|không bắt buộc|người tổ chức)\b[\)\]]?\s*`)
)

// ParseGuests reads a pasted guest list: "Name <email>" entries split by
// new lines and commas (Google Calendar, mail clients) or semicolons
// (Outlook, whose names may be "Last, First"), bare emails, or plain
// names. A guest with an email and no name gets a name guessed from the
// address ("an.nguyen@" → "An Nguyen"), which the user can fix.
func ParseGuests(text string) []Guest {
	semicolons := strings.Contains(text, ";")
	var out []Guest
	prevNameOnly := false // the last entry was a name with no email
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' || r == ';' }) {
		parts := []string{line}
		if !semicolons {
			parts = splitGuestLine(line)
		}
		for _, part := range parts {
			g, guessed, ok := parseGuest(part)
			if !ok {
				continue
			}
			// A calendar's guest panel copies as a name line, then an
			// email line: one person.
			if prevNameOnly && guessed {
				out[len(out)-1].Email = g.Email
				prevNameOnly = false
				continue
			}
			prevNameOnly = g.Email == ""
			out = append(out, g)
		}
	}
	clean, _ := CleanGuests(out)
	if len(clean) > MaxGuests {
		clean = clean[:MaxGuests]
	}
	return clean
}

// splitGuestLine splits on commas outside <…> and quotes, so
// "Nguyen, An" <an@x.com> stays whole.
func splitGuestLine(line string) []string {
	var parts []string
	var cur strings.Builder
	depth, quoted := 0, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case r == ',' && depth == 0 && !quoted:
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	return append(parts, cur.String())
}

// parseGuest reads one entry; guessed reports an email alone (any name
// came from the address).
func parseGuest(s string) (g Guest, guessed, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Guest{}, false, false
	}
	email := emailPattern.FindString(s)
	name := s
	if email != "" {
		name = strings.Replace(name, email, "", 1)
	}
	name = strings.NewReplacer("<", " ", ">", " ", "\"", " ", "mailto:", " ").Replace(name)
	name = guestNoise.ReplaceAllString(name, " ")
	name = strings.Trim(strings.Join(strings.Fields(name), " "), " -–—:·•,()[]")
	if !hasLetter(name) {
		name = ""
	}
	if email == "" && name == "" {
		return Guest{}, false, false
	}
	if name == "" {
		return Guest{Name: nameFromEmail(email), Email: email}, true, true
	}
	return Guest{Name: name, Email: email}, false, true
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// nameFromEmail guesses "An Nguyen" from "an.nguyen@acme.com"; "" when
// the local part doesn't look like a name (digits, one letter, role
// addresses).
func nameFromEmail(email string) string {
	local := email
	if i := strings.IndexByte(email, '@'); i > 0 {
		local = email[:i]
	}
	if i := strings.IndexByte(local, '+'); i > 0 {
		local = local[:i]
	}
	words := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	if len(words) == 0 {
		return ""
	}
	for _, w := range words {
		if !hasLetter(w) || strings.IndexFunc(w, unicode.IsDigit) >= 0 {
			return ""
		}
	}
	if len(words) == 1 && roleAddress[strings.ToLower(words[0])] {
		return ""
	}
	for i, w := range words {
		r, size := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + strings.ToLower(w[size:])
	}
	return strings.Join(words, " ")
}

var roleAddress = map[string]bool{
	"info": true, "admin": true, "support": true, "team": true, "hello": true, "contact": true,
	"sales": true, "hr": true, "office": true, "noreply": true, "calendar": true, "meet": true,
}

// GuestNames lists the guests' names (for prompts and context terms):
// never their emails.
func GuestNames(gs []Guest) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range gs {
		if g.Name != "" && !seen[strings.ToLower(g.Name)] {
			seen[strings.ToLower(g.Name)] = true
			out = append(out, g.Name)
		}
	}
	return out
}
