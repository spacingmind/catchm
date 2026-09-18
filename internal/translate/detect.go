package translate

import (
	"strings"
	"unicode"
)

// scriptEntry describes the rune set one language is written with: a
// match predicate over lowercased runes, plus the approximate size of
// that set. The size is only used to break ties in favor of the more
// constrained script (plain ASCII text is more likely English than
// Vietnamese-with-diacritics-omitted).
type scriptEntry struct {
	match func(rune) bool
	size  int
}

func isASCIILetter(r rune) bool {
	return r < 128 && unicode.IsLetter(r)
}

// Vietnamese uses the full ASCII Latin alphabet plus diacritic letters
// concentrated in Latin-1 Supplement (à á â ã ...), parts of Latin
// Extended-A (ă đ ê ô ơ ư), and Latin Extended Additional
// (U+1E00–U+1EFF: ả ẳ ẹ ế ộ ứ ...).
func isVietnameseLetter(r rune) bool {
	if isASCIILetter(r) {
		return true
	}
	switch {
	case r >= 0x00C0 && r <= 0x00FF, // À..ÿ
		r >= 0x0100 && r <= 0x017F, // Ā..ſ (contains ă đ ê ô ơ ư)
		r >= 0x1E00 && r <= 0x1EFF: // Ḁ..ỿ (most precomposed Vietnamese)
		return true
	}
	return false
}

// scripts maps a language code to the runes characteristic of it. Only
// the languages this heuristic can actually distinguish are listed; any
// other code has no entry, and a pair with an unknown code falls back to
// the first language.
var scripts = map[string]scriptEntry{
	"en": {match: isASCIILetter, size: 52},
	"vi": {match: isVietnameseLetter, size: 260},
}

// DetectLanguage is a best-effort heuristic, not an NLP feature: given
// text and exactly two candidate language codes, it picks the one whose
// script better explains the text. It compares runes found in one
// language's script but not the other's (for en/vi: Vietnamese
// diacritics vs plain ASCII), and on a tie prefers the more constrained
// script. Languages outside the scripts table (or identical/empty codes)
// resolve to the first candidate, so callers get a usable answer even
// when detection can't tell the pair apart.
func DetectLanguage(text, langA, langB string) string {
	langA, langB = strings.ToLower(strings.TrimSpace(langA)), strings.ToLower(strings.TrimSpace(langB))
	if langA == "" {
		return langB
	}
	if langB == "" || langA == langB {
		return langA
	}
	sa, oka := scripts[langA]
	sb, okb := scripts[langB]
	if !oka || !okb {
		return langA
	}

	var onlyA, onlyB int
	for _, r := range text {
		r = unicode.ToLower(r)
		ma, mb := sa.match(r), sb.match(r)
		if ma && !mb {
			onlyA++
		}
		if mb && !ma {
			onlyB++
		}
	}
	switch {
	case onlyB > onlyA:
		return langB
	case onlyA > onlyB:
		return langA
	case sa.size <= sb.size:
		return langA
	default:
		return langB
	}
}
