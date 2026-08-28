package pricesrc

import (
	"strings"
	"unicode"
)

// HDWRAttrParser parses attributes from HDWR.pl description text.
// Format: Find "Specyfikacja urządzenia", then parse attributes.
// Values can contain multiple items separated by commas.
type HDWRAttrParser struct {
	specMarker string
}

// NewHDWRAttrParser creates a new HDWRAttrParser.
func NewHDWRAttrParser() *HDWRAttrParser {
	return &HDWRAttrParser{
		specMarker: "Specyfikacja urządzenia",
	}
}

// Parse extracts attributes from the description text.
// Returns a map where each key can have multiple values.
func (p *HDWRAttrParser) Parse(description string) map[string][]string {
	attrs := make(map[string][]string)

	// Find the spec section marker
	specIdx := strings.Index(description, p.specMarker)
	if specIdx == -1 {
		return attrs
	}

	// Get text after the marker
	specText := description[specIdx+len(p.specMarker):]

	// Parse attributes from spec text
	p.parseSpecText(specText, attrs)

	return attrs
}

// parseSpecText parses attribute key-value pairs from spec text.
func (p *HDWRAttrParser) parseSpecText(text string, attrs map[string][]string) {
	runes := []rune(text)
	n := len(runes)

	// Find all "Key:" patterns (capital letter followed by colon)
	type attrPos struct {
		keyStart int
		colonIdx int
	}

	var attrPositions []attrPos

	i := 0
	for i < n {
		// Find next capital letter
		nextCap := p.findNextCapital(runes, i)
		if nextCap == -1 {
			break
		}

		// Check if this capital letter is the start of a word (not in the middle)
		if nextCap > 0 && isLetterOrDigit(runes[nextCap-1]) {
			i = nextCap + 1
			continue
		}

		// Find the next capital letter after this one
		nextNextCap := p.findNextCapital(runes, nextCap+1)

		// Search for colon between nextCap and nextNextCap
		colonIdx := -1
		searchEnd := n
		if nextNextCap != -1 {
			searchEnd = nextNextCap
		}

		for j := nextCap; j < searchEnd; j++ {
			if runes[j] == ':' {
				colonIdx = j
				break
			}
		}

		// If no colon found before next capital letter, attributes have ended
		if colonIdx == -1 {
			break
		}

		// Record this attribute position
		attrPositions = append(attrPositions, attrPos{
			keyStart: nextCap,
			colonIdx: colonIdx,
		})

		// Continue from after the colon
		i = colonIdx + 1
	}

	// Now extract key-value pairs
	for idx, pos := range attrPositions {
		// Key is from keyStart to colonIdx
		key := strings.TrimSpace(string(runes[pos.keyStart:pos.colonIdx]))

		// Value is from after colon to the next attribute's keyStart (or end of text)
		valueStart := pos.colonIdx + 1
		valueEnd := n
		if idx+1 < len(attrPositions) {
			valueEnd = attrPositions[idx+1].keyStart
		}

		value := strings.TrimSpace(string(runes[valueStart:valueEnd]))

		// Store if key and value are valid
		if key != "" && value != "" && len(key) < 100 && len(value) < 300 {
			// Check if value contains commas - split into multiple values
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
	}
}

// findNextCapital finds the index of the next capital letter starting from pos.
func (p *HDWRAttrParser) findNextCapital(runes []rune, pos int) int {
	for i := pos; i < len(runes); i++ {
		if isCapital(runes[i]) {
			return i
		}
	}
	return -1
}

// isCapital checks if a rune is a capital letter.
func isCapital(r rune) bool {
	return unicode.IsUpper(r)
}

// isLetter checks if a rune is a letter.
func isLetter(r rune) bool {
	return unicode.IsLetter(r)
}

// isLetterOrDigit checks if a rune is a letter or digit.
func isLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
