package attrs

import (
	"regexp"
	"strings"
	"unicode"
)

// Code and value validity limits.
const (
	CodeMinLen    = 3
	CodeMaxLen    = 40
	CodeMaxWords  = 4
	ValueMaxRunes = 40
	ValueMaxWords = 6
)

// polishMap transliterates Polish diacritics to ASCII.
// Applied to both lowercase and uppercase forms.
var polishMap = map[rune]string{
	'ą': "a", 'ć': "c", 'ę': "e", 'ł': "l", 'ń': "n", 'ó': "o", 'ś': "s", 'ź': "z", 'ż': "z",
	'Ą': "A", 'Ć': "C", 'Ę': "E", 'Ł': "L", 'Ń': "N", 'Ó': "O", 'Ś': "S", 'Ź': "Z", 'Ż': "Z",
}

// PolishToASCII replaces Polish diacritics with their ASCII equivalents.
// All other runes are preserved.
func PolishToASCII(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if repl, ok := polishMap[r]; ok {
			sb.WriteString(repl)
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// NormalizeKey cleans a raw attribute key: trims, converts NBSP and other
// Unicode spaces to regular spaces, normalizes dash variants, collapses
// whitespace runs.
func NormalizeKey(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\u2002", " ")
	s = strings.ReplaceAll(s, "\u2003", " ")
	s = strings.ReplaceAll(s, "\u202f", " ")
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "—", "-")
	s = strings.ReplaceAll(s, "−", "-")
	s = normalizeSpaces(s)
	return s
}

var codeRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// ValidateCode checks the canonical code format:
//   - ^[a-z][a-z0-9]*(_[a-z0-9]+)*$ (starts with a letter, no leading digit)
//   - length 3..40
//   - at most 4 words
func ValidateCode(code string) bool {
	if len(code) < CodeMinLen || len(code) > CodeMaxLen {
		return false
	}
	if !codeRe.MatchString(code) {
		return false
	}
	if strings.Count(code, "_")+1 > CodeMaxWords {
		return false
	}
	return true
}

// CodeFromKey generates a canonical attribute code from a raw key
// (e.g. "Pojemność" -> "pojemnosc", "Kolor klawiszy" -> "kolor_klawiszy").
// Returns ok=false when the raw key is not a plausible attribute key
// (empty, a sentence, a value, starts with a digit, etc.).
func CodeFromKey(raw string) (string, bool) {
	key := NormalizeKey(raw)
	if key == "" {
		return "", false
	}

	// A colon inside a key means a glued "key: value" fragment.
	if strings.Contains(key, ":") {
		return "", false
	}

	// Must start with a letter (keys like "0.06 m 3" are values, not keys).
	first := []rune(key)[0]
	if !unicode.IsLetter(first) {
		return "", false
	}

	// Reject sentences: more than CodeMaxWords words is not an attribute name.
	words := strings.Fields(key)
	if len(words) > CodeMaxWords {
		return "", false
	}
	for _, w := range words {
		if len(w) > 20 {
			return "", false
		}
	}

	code := PolishToASCII(strings.ToLower(key))
	code = strings.ReplaceAll(code, " ", "_")
	code = strings.ReplaceAll(code, "-", "_")

	// Drop everything that is not [a-z0-9_].
	var sb strings.Builder
	sb.Grow(len(code))
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		}
	}
	code = sb.String()

	// Collapse and trim underscores.
	for strings.Contains(code, "__") {
		code = strings.ReplaceAll(code, "__", "_")
	}
	code = strings.Trim(code, "_")

	if !ValidateCode(code) {
		return "", false
	}
	return code, true
}

var gluedKeyRe = regexp.MustCompile(`[:]\s`)

// ValidValue checks whether a string is a plausible attribute value:
//   - 1..ValueMaxRunes runes
//   - at most ValueMaxWords words
//   - no glued "key: value" fragment (": ")
//   - contains at least one letter or digit
func ValidValue(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	runes := []rune(v)
	if len(runes) > ValueMaxRunes {
		return false
	}
	if len(strings.Fields(v)) > ValueMaxWords {
		return false
	}
	if gluedKeyRe.MatchString(v) {
		return false
	}
	hasAlnum := false
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasAlnum = true
			break
		}
	}
	return hasAlnum
}

// NormalizeValue cleans a single attribute value: trims, normalizes spaces
// and dash variants, removes trailing slash-enclosed notes.
func NormalizeValue(raw string) string {
	return normalizeSingleValue(raw)
}

// SplitValues normalizes a raw value and splits it into individual values.
// Comma-separated option lists are split; each part is validated and
// duplicates are removed.
func SplitValues(raw string) []string {
	raw = NormalizeValue(raw)
	if raw == "" {
		return nil
	}

	var parts []string
	if strings.Contains(raw, ",") && looksLikeList(raw) {
		for _, p := range strings.Split(raw, ",") {
			parts = append(parts, normalizeSingleValue(p))
		}
	} else {
		parts = []string{raw}
	}

	seen := make(map[string]bool, len(parts))
	var result []string
	for _, p := range parts {
		if !ValidValue(p) {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		result = append(result, p)
	}
	return result
}
