// Package slug provides URL-friendly slug generation utilities.
package slug

import (
	"strings"
)

var translitMap = map[rune]string{
	'А': "a", 'Б': "b", 'В': "v", 'Г': "g", 'Д': "d", 'Е': "e", 'Ё': "e", 'Ж': "zh",
	'З': "z", 'И': "i", 'Й': "y", 'К': "k", 'Л': "l", 'М': "m", 'Н': "n", 'О': "o",
	'П': "p", 'Р': "r", 'С': "s", 'Т': "t", 'У': "u", 'Ф': "f", 'Х': "kh", 'Ц': "ts",
	'Ч': "ch", 'Ш': "sh", 'Щ': "shch", 'Ъ': "", 'Ы': "y", 'Ь': "", 'Э': "e", 'Ю': "yu", 'Я': "ya",
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// Slug creates a URL-friendly slug with Cyrillic transliteration.
// Letters are lowercased, spaces/hyphens/underscores become hyphens,
// consecutive hyphens are collapsed, leading/trailing hyphens are trimmed.
func Slug(s string) string {
	var result strings.Builder
	for _, r := range s {
		if t, ok := translitMap[r]; ok {
			result.WriteString(t)
		} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			result.WriteString("-")
		}
	}

	slug := result.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")

	return strings.ToLower(slug)
}

// SlugFromNameEn generates a URL-safe slug from an English (Latin) name.
// The input is lowercased, spaces become hyphens, and any character
// that is not a-z, 0-9 or '-' is dropped. Consecutive hyphens are
// collapsed and leading/trailing hyphens are trimmed.
func SlugFromNameEn(name string) string {
	s := strings.ToLower(name)
	// Replace non-alnum / spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	s = b.String()
	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	// Trim leading/trailing hyphens
	return strings.Trim(s, "-")
}

// SlugKeepCase creates a URL-friendly slug preserving letter case.
// Spaces and hyphens become hyphens, all other non-alphanumeric
// characters are dropped. Consecutive hyphens are collapsed and
// leading/trailing hyphens are trimmed.
func SlugKeepCase(s string) string {
	// Simple slug: replace spaces with hyphens, remove special chars
	result := []rune{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result = append(result, r)
		} else if r == ' ' || r == '-' {
			result = append(result, '-')
		}
	}
	// Collapse multiple hyphens
	collapsed := []rune{}
	for i, r := range result {
		if r == '-' && i > 0 && result[i-1] == '-' {
			continue
		}
		collapsed = append(collapsed, r)
	}
	// Trim leading/trailing hyphens
	start, end := 0, len(collapsed)
	for start < end && collapsed[start] == '-' {
		start++
	}
	for end > start && collapsed[end-1] == '-' {
		end--
	}
	return string(collapsed[start:end])
}
