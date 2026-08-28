package pricesrc

import (
	"regexp"
	"strings"
)

// ZabudowaAttrParser parses attributes from Zabudowa AGD description text.
type ZabudowaAttrParser struct {
	// keyPattern matches lines like "Key: value" or "Key [unit]: value"
	keyPattern *regexp.Regexp
}

// NewZabudowaAttrParser creates a new ZabudowaAttrParser.
func NewZabudowaAttrParser() *ZabudowaAttrParser {
	return &ZabudowaAttrParser{
		// Match lines with "key: value" pattern
		// Key can contain letters, numbers, spaces, parentheses, brackets, Polish characters
		keyPattern: regexp.MustCompile(`^([A-Za-zĄĆĘŁŃÓŚŹŻąćęłńóśźż0-9\s\(\)\[\]\/\-\*°Ø%]+?)\s*:\s*(.+)$`),
	}
}

// Parse extracts attributes from the description text.
// Returns a map of normalized attribute names to values (supports multiple values per key).
func (p *ZabudowaAttrParser) Parse(description string) map[string][]string {
	attrs := make(map[string][]string)

	lines := strings.Split(description, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// Try to match key: value pattern
		matches := p.keyPattern.FindStringSubmatch(line)
		if matches != nil {
			key := strings.TrimSpace(matches[1])
			value := strings.TrimSpace(matches[2])

			// Skip if key is too long (probably not an attribute)
			if len(key) > 80 {
				continue
			}

			// Skip if value is too long (probably not an attribute)
			if len(value) > 200 {
				continue
			}

			// Skip if key looks like a section header (all caps, no colon in original)
			if isSectionHeader(key) {
				continue
			}

			// Normalize the key
			normKey := p.normalizeKey(key)
			if normKey == "" {
				continue
			}

			// Store the attribute (split by commas if present)
			p.addValue(attrs, normKey, value)
			continue
		}

		// Try to match key\nvalue pattern (key on one line, value on next)
		if i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])

			// Check if current line looks like a key (short, no punctuation at end)
			if p.looksLikeKey(line) && p.looksLikeValue(nextLine) {
				key := line
				value := nextLine

				// Skip if key is too long (probably not an attribute)
				if len(key) > 80 {
					continue
				}

				// Skip if value is too long (probably not an attribute)
				if len(value) > 200 {
					continue
				}

				// Skip if key looks like a section header
				if isSectionHeader(key) {
					continue
				}

				// Normalize the key
				normKey := p.normalizeKey(key)
				if normKey == "" {
					continue
				}

				// Store the attribute (split by commas if present)
				p.addValue(attrs, normKey, value)

				// Skip the value line
				i++
			}
		}
	}

	return attrs
}

// addValue adds a value to the attributes map, splitting by commas if present.
func (p *ZabudowaAttrParser) addValue(attrs map[string][]string, key, value string) {
	if strings.Contains(value, ",") {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				attrs[key] = append(attrs[key], part)
			}
		}
	} else {
		attrs[key] = append(attrs[key], value)
	}
}

// looksLikeKey checks if a line looks like an attribute key.
func (p *ZabudowaAttrParser) looksLikeKey(line string) bool {
	// Key should be short
	if len(line) > 50 {
		return false
	}

	// Key should not end with punctuation (except for some cases)
	if strings.HasSuffix(line, ".") || strings.HasSuffix(line, ",") || strings.HasSuffix(line, "!") || strings.HasSuffix(line, "?") {
		return false
	}

	// Key should not contain too many spaces
	if strings.Count(line, " ") > 5 {
		return false
	}

	// Key should contain letters
	hasLetters := false
	for _, r := range line {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 'ą' && r <= 'ż') || (r >= 'Ą' && r <= 'Ż') {
			hasLetters = true
			break
		}
	}

	return hasLetters
}

// looksLikeValue checks if a line looks like an attribute value.
func (p *ZabudowaAttrParser) looksLikeValue(line string) bool {
	// Value should not be empty
	if line == "" {
		return false
	}

	// Value should not be too long
	if len(line) > 150 {
		return false
	}

	// Value should not look like a section header
	if isSectionHeader(line) {
		return false
	}

	return true
}

// normalizeKey normalizes an attribute key for storage.
func (p *ZabudowaAttrParser) normalizeKey(key string) string {
	// Remove common unit patterns from key
	key = strings.ReplaceAll(key, " [mm]", "")
	key = strings.ReplaceAll(key, " [W]", "")
	key = strings.ReplaceAll(key, " [V]", "")
	key = strings.ReplaceAll(key, " [kg]", "")
	key = strings.ReplaceAll(key, " [m]", "")
	key = strings.ReplaceAll(key, " [°C]", "")
	key = strings.ReplaceAll(key, " [dB]", "")
	key = strings.ReplaceAll(key, " [l]", "")
	key = strings.ReplaceAll(key, " [L]", "")
	key = strings.ReplaceAll(key, " [h]", "")
	key = strings.ReplaceAll(key, " [min]", "")

	// Trim whitespace
	key = strings.TrimSpace(key)

	return key
}

// isSectionHeader checks if a key looks like a section header.
func isSectionHeader(key string) bool {
	// Section headers are usually all caps and short
	if len(key) > 50 {
		return false
	}

	// Check if all characters are uppercase (or Polish uppercase)
	allCaps := true
	for _, r := range key {
		if r >= 'a' && r <= 'z' {
			allCaps = false
			break
		}
		if r >= 'ą' && r <= 'ż' {
			allCaps = false
			break
		}
	}

	// If all caps and short, it's probably a section header
	return allCaps && len(key) < 30
}
