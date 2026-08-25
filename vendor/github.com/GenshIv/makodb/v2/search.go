package makodb

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/GenshIv/intHache"
)

// Tokenize splits a text into unique lowercase alphanumeric tokens of length >= 2.
func Tokenize(text string) []string {
	// Manual parsing to avoid strings.FieldsFunc + strings.ToLower allocations.
	// Collect tokens as substrings (no extra allocations for token content).
	var tokens []string
	seen := make(map[key128]struct{})

	i := 0
	for i < len(text) {
		// Skip non-alphanumeric
		r, sz := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			i += sz
			continue
		}

		// Start of token
		start := i
		for i < len(text) {
			r, sz := utf8.DecodeRuneInString(text[i:])
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				break
			}
			i += sz
		}

		token := text[start:i]
		if utf8.RuneCountInString(token) < 2 {
			continue
		}

		// Lowercase in place using a small buffer
		tokenLower := toLowerASCII(token)

		// Dedup via intHache 128-bit hash
		h := intHache.SumString128(tokenLower)
		if _, exists := seen[h]; !exists {
			seen[h] = struct{}{}
			tokens = append(tokens, tokenLower)
		}
	}

	return tokens
}

// toLowerASCII converts a string to lowercase for ASCII letters.
// For non-ASCII, uses strings.ToLower as fallback.
// Optimized: first check if conversion is needed, then convert.
func toLowerASCII(s string) string {
	// Fast path: already lowercase ASCII or empty
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			goto convert
		}
		if c > 127 {
			return strings.ToLower(s)
		}
	}
	return s

convert:
	buf := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			buf[i] = c + ('a' - 'A')
		} else {
			buf[i] = c
		}
	}
	return string(buf)
}
