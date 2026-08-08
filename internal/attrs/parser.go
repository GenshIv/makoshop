package attrs

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ParsedAttrs represents normalized attributes map: code -> values.
type ParsedAttrs map[string][]string

// ParseTable parses a simple HTML table with key/value rows and returns normalized attributes.
// Expected structure: <table><tr><td>key</td><td>value</td></tr>...</table>
// Special handling:
//   - "Комплектация" and similar attributes with comma-separated items are split into
//     separate boolean attributes (e.g., "lyulka", "dokhdevik", "moskitnaya_setka").
//   - Values like "наклона спинки / 2 положения /, высоты подголовника / 8 положений /"
//     are split into separate attributes.
func ParseTable(html string) ParsedAttrs {
	if !strings.Contains(strings.ToLower(html), "<table") {
		return nil
	}

	attrs := make(ParsedAttrs)

	// Normalize whitespace in HTML to handle newlines between tags
	html = normalizeHTMLWhitespace(html)

	// Extract rows: <tr>...</tr>
	trRe := regexp.MustCompile(`<tr[^>]*>(.*?)</tr>`)
	rows := trRe.FindAllString(html, -1)

	for _, row := range rows {
		// Extract td cells
		tdRe := regexp.MustCompile(`<td[^>]*>(.*?)</td>`)
		cells := tdRe.FindAllString(row, -1)
		if len(cells) < 2 {
			continue
		}

		key := cleanCell(cells[0])
		value := cleanCell(cells[1])

		if key == "" || value == "" {
			continue
		}

		// Normalize key -> code
		code := slugify(key)
		if code == "" {
			continue
		}

		// Check if this is a "composite" attribute like "Комплектация"
		// where comma-separated items should become separate boolean attributes.
		if isCompositeAttribute(key) {
			items := splitCompositeValue(value)
			for _, item := range items {
				itemCode := slugify(item)
				if itemCode == "" {
					continue
				}
				// Use a prefixed code to avoid collisions, e.g., "komplektatsiya_lyulka"
				compositeCode := code + "_" + itemCode
				attrs.add(compositeCode, "да")
			}
			continue
		}

		// Check if value looks like multiple parameters combined with comma
		// e.g. "наклона спинки / 2 положения /, высоты подголовника / 8 положений /"
		if looksLikeMultipleParams(value) {
			subKeys, subValues := splitParamsFromValue(value)
			for i, sk := range subKeys {
				if i >= len(subValues) {
					break
				}
				sk = strings.TrimSpace(sk)
				sv := strings.TrimSpace(subValues[i])
				if sk == "" || sv == "" {
					continue
				}
				subCode := slugify(sk)
				if subCode == "" {
					continue
				}
				for _, v := range normalizeValues(sv) {
					attrs.add(subCode, v)
				}
			}
		} else {
			// Single key -> possibly multiple values (comma-separated options)
			for _, v := range normalizeValues(value) {
				attrs.add(code, v)
			}
		}
	}

	return attrs
}

// isCompositeAttribute checks if an attribute key represents a list of independent
// features that should be split into separate boolean attributes.
// Examples: "Комплектация", "В комплекте", "Аксессуары в комплекте".
func isCompositeAttribute(key string) bool {
	k := strings.ToLower(key)
	compositeKeys := []string{
		"комплектация",
		"в комплекте",
		"аксессуары в комплекте",
		"что в комплекте",
		"дополнительное оборудование",
		"входит в комплект",
	}
	for _, ck := range compositeKeys {
		if strings.Contains(k, ck) {
			return true
		}
	}
	return false
}

// splitCompositeValue splits a composite attribute value into individual items.
// Handles comma-separated lists and normalizes each item.
func splitCompositeValue(value string) []string {
	value = normalizeSpaces(value)
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	// Split by comma
	parts := strings.Split(value, ",")
	var items []string
	for _, p := range parts {
		item := normalizeSingleValue(p)
		item = strings.TrimSpace(item)
		if item != "" && len(item) < 100 { // Sanity check: items should be short
			items = append(items, item)
		}
	}
	return items
}

// normalizeHTMLWhitespace collapses newlines and extra spaces between HTML tags
// so regex patterns work reliably.
func normalizeHTMLWhitespace(html string) string {
	// Replace newlines with space
	s := strings.ReplaceAll(html, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	// Collapse multiple spaces
	re := regexp.MustCompile(`\s+`)
	return re.ReplaceAllString(s, " ")
}

func (a ParsedAttrs) add(code, value string) {
	if value == "" {
		return
	}
	if _, ok := a[code]; !ok {
		a[code] = []string{}
	}
	// Avoid duplicates
	for _, existing := range a[code] {
		if existing == value {
			return
		}
	}
	a[code] = append(a[code], value)
}

// cleanCell extracts text from a td cell, stripping HTML tags and normalizing spaces.
func cleanCell(html string) string {
	// Remove HTML tags
	re := regexp.MustCompile(`<[^>]+>`)
	text := re.ReplaceAllString(html, " ")
	// Normalize spaces
	text = normalizeSpaces(text)
	return strings.TrimSpace(text)
}

// normalizeSpaces replaces non-breaking spaces and multiple spaces with single space.
func normalizeSpaces(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\u2002", " ")
	s = strings.ReplaceAll(s, "\u2003", " ")
	s = strings.ReplaceAll(s, "\u202f", " ")
	re := regexp.MustCompile(`\s+`)
	return re.ReplaceAllString(s, " ")
}

// normalizeValues splits a value into individual normalized values.
// Handles comma-separated options and slash-enclosed notes.
func normalizeValues(raw string) []string {
	raw = normalizeSpaces(raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// If value contains comma and looks like a list of options, split by comma
	if strings.Contains(raw, ",") && looksLikeList(raw) {
		parts := strings.Split(raw, ",")
		var result []string
		for _, p := range parts {
			p = normalizeSingleValue(p)
			if p != "" {
				result = append(result, p)
			}
		}
		return result
	}

	// Single value
	return []string{normalizeSingleValue(raw)}
}

// normalizeSingleValue cleans up a single attribute value.
func normalizeSingleValue(v string) string {
	v = normalizeSpaces(v)
	// Normalize dashes: "–", "—", "−" -> "-"
	v = strings.ReplaceAll(v, "–", "-")
	v = strings.ReplaceAll(v, "—", "-")
	v = strings.ReplaceAll(v, "−", "-")
	// Clean up slash-enclosed notes: "да / 4 положения /" -> "да"
	v = cleanSlashNotes(v)
	v = strings.TrimSpace(v)
	return v
}

// cleanSlashNotes removes trailing slash-enclosed notes like " / 4 положения /" from values.
func cleanSlashNotes(v string) string {
	// Pattern: " / ... /" at the end or after a clear value
	re := regexp.MustCompile(`\s*/\s*.*?/\s*$`)
	return re.ReplaceAllString(v, "")
}

// looksLikeList checks if a string looks like a comma-separated list of options.
func looksLikeList(s string) bool {
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return false
	}
	// Check if parts are relatively short (likely options, not a long description)
	for _, p := range parts {
		if len(strings.TrimSpace(p)) > 80 {
			return false
		}
	}
	return true
}

// looksLikeMultipleParams checks if a value contains multiple parameters in format:
// "param1 / value1 /, param2 / value2 /" or similar patterns.
func looksLikeMultipleParams(value string) bool {
	if !strings.Contains(value, ",") {
		return false
	}
	parts := strings.Split(value, ",")
	if len(parts) < 2 {
		return false
	}
	// Check if most parts contain slash pattern (param / value /)
	slashCount := 0
	for _, p := range parts {
		if strings.Contains(p, "/") {
			slashCount++
		}
	}
	return slashCount >= len(parts)-1
}

// splitParamsFromValue splits a value like "наклона спинки / 2 положения /, высоты подголовника / 8 положений /"
// into separate key/value pairs.
func splitParamsFromValue(value string) ([]string, []string) {
	parts := strings.Split(value, ",")

	var keys []string
	var values []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Pattern: "param name / value /" or "param name / value"
		if strings.Contains(part, "/") {
			segments := strings.SplitN(part, "/", 3)
			keyName := strings.TrimSpace(segments[0])
			var val string
			if len(segments) >= 2 {
				val = strings.TrimSpace(segments[1])
			}
			if keyName != "" {
				keys = append(keys, keyName)
				values = append(values, val)
			}
		} else {
			// No slash, treat as single value
			keys = append(keys, "")
			values = append(values, part)
		}
	}

	return keys, values
}

// slugify converts a Russian (or any Unicode) string to a URL-safe slug.
// Uses transliteration for common Cyrillic characters.
func slugify(s string) string {
	s = strings.ToLower(s)
	// Normalize spaces first
	s = normalizeSpaces(s)
	// Transliterate common Cyrillic characters
	trans := map[rune]string{
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d",
		'е': "e", 'ё': "yo", 'ж': "zh", 'з': "z", 'и': "i",
		'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n",
		'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
		'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch",
		'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "",
		'э': "e", 'ю': "yu", 'я': "ya",
	}

	var sb strings.Builder
	for _, r := range s {
		if t, ok := trans[r]; ok {
			sb.WriteString(t)
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		} else if r == '-' || r == '_' || r == ' ' {
			sb.WriteString("-")
		} else {
			sb.WriteString("-")
		}
	}

	// Replace multiple dashes with single
	re := regexp.MustCompile(`-+`)
	slug := re.ReplaceAllString(sb.String(), "-")
	// Trim leading/trailing dashes
	slug = strings.Trim(slug, "-")

	return slug
}

// ToFlatMap converts ParsedAttrs to a flat map[string]string (first value for each code).
// Useful for storage when only one value per attribute is needed.
func (a ParsedAttrs) ToFlatMap() map[string]string {
	result := make(map[string]string)
	for code, values := range a {
		if len(values) > 0 {
			result[code] = values[0]
		}
	}
	return result
}

// ToDisplayMap converts ParsedAttrs to a map with human-readable names.
func (a ParsedAttrs) ToDisplayMap(displayNames map[string]string) map[string][]string {
	result := make(map[string][]string)
	for code, values := range a {
		name := code
		if dn, ok := displayNames[code]; ok {
			name = dn
		}
		result[name] = values
	}
	return result
}

// Merge combines two ParsedAttrs, with b taking precedence for conflicts.
func (a ParsedAttrs) Merge(b ParsedAttrs) ParsedAttrs {
	result := make(ParsedAttrs)
	// Copy all from a
	for code, values := range a {
		result[code] = append([]string{}, values...)
	}
	// Merge/override with b
	for code, values := range b {
		if _, ok := result[code]; !ok {
			result[code] = []string{}
		}
		for _, v := range values {
			result.add(code, v)
		}
	}
	return result
}

// Validate checks if attributes look reasonable and returns issues.
func (a ParsedAttrs) Validate() []string {
	var issues []string
	for code, values := range a {
		if code == "" {
			issues = append(issues, "empty attribute code")
			continue
		}
		if len(code) > 100 {
			issues = append(issues, fmt.Sprintf("code too long (%d): %s", len(code), code))
		}
		for _, v := range values {
			if v == "" {
				issues = append(issues, fmt.Sprintf("empty value for code %s", code))
			}
			if len(v) > 500 {
				issues = append(issues, fmt.Sprintf("value too long (%d) for code %s", len(v), code))
			}
		}
		if len(values) > 20 {
			issues = append(issues, fmt.Sprintf("too many values (%d) for code %s", len(values), code))
		}
	}
	return issues
}
