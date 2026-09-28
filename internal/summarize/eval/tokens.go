package eval

import (
	"strings"
	"unicode"
)

// matchThreshold is the token-set similarity at which a predicted item
// counts as the gold item.
const matchThreshold = 0.5

// stopwords are dropped before comparing texts: English and Vietnamese
// function words, pronouns and honorifics, plus the verbs models wrap
// items in ("the team decided to ...", "chốt là ..."). Vietnamese is
// written one syllable per token, so entries are single syllables.
var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		a an the and or but of to in on at for by with from as is are was were be been
		will would shall should can could may might must do does did it its this that
		these those there here we our us you your they them their he she his her i me my
		so if then than also just about into up out over all any some not no yes ok okay
		team decided decide agreed agree going let lets s t
		và hoặc là của có cho với các những một thì được sẽ đã đang này đó kia
		em anh chị mình chúng ta tôi bạn họ ông bà cô chú bác
		ạ nhé nha nhỉ ừ vâng dạ rồi lại cũng để vào ở trong khi nào gì vậy thế
		chốt thống nhất quyết
	`) {
		m[w] = true
	}
	return m
}()

// tokens lowercases s, turns punctuation and symbols into spaces, and
// returns its content words, singularized. Diacritics are kept: in
// Vietnamese they distinguish words ("bán" sell, "bàn" table).
func tokens(s string) []string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, s)
	var out []string
	for _, w := range strings.Fields(s) {
		if !stopwords[w] {
			out = append(out, singular(w))
		}
	}
	return out
}

// singular strips a plain English plural "s" ("refunds" → "refund").
// Vietnamese syllables never end in "s", so they pass through.
func singular(w string) string {
	if len(w) <= 3 || !strings.HasSuffix(w, "s") || strings.HasSuffix(w, "ss") {
		return w
	}
	for _, r := range w {
		if r < 'a' || r > 'z' {
			return w
		}
	}
	return w[:len(w)-1]
}

func tokenSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range tokens(s) {
		m[w] = true
	}
	return m
}

// similarity is the Dice overlap of two token sets:
// 2·|A∩B| / (|A|+|B|). Unlike an overlap over the smaller set, a
// one-word prediction can't match every gold item that contains it.
func similarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	return 2 * float64(intersect(a, b)) / float64(len(a)+len(b))
}

func intersect(a, b map[string]bool) int {
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return n
}
