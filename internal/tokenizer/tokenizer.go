package tokenizer

import (
	"strings"
	"unicode"

	"github.com/GenshIv/makoshop/internal/model"
)

// Stop words (Russian + common non-topical words)
var stopWords = map[string]bool{
	"для": true, "с": true, "и": true, "в": true, "на": true, "из": true,
	"по": true, "к": true, "у": true, "о": true, "от": true, "до": true,
	"без": true, "через": true, "при": true, "под": true, "над": true,
	"между": true, "за": true, "перед": true, "после": true, "вокруг": true,
	"но": true, "а": true, "или": true, "если": true, "что": true, "который": true,
	"этот": true, "этих": true, "этим": true,
	"новый": true, "новые": true, "новое": true, "новом": true,
	"лучший": true, "лучшие": true, "хороший": true, "хорошие": true,
	"качественный": true, "качественные": true,
	"оригинальный": true, "оригинальные": true,
	"бренд": true, "фирма": true, "производитель": true,
	"модель": true, "модели": true,
	"товар": true, "товары": true,
	"цена": true, "цены": true,
	"купить": true, "продажа": true, "продажи": true,
	"в наличии": true, "наличие": true,
	"шт": true, "штука": true, "штуки": true, "штук": true,
	"комплект": true, "комплекты": true, "набор": true, "наборы": true,
	"упаковка": true, "упаковки": true, "box": true, "pack": true,
	"set": true, "kit": true, "plus": true, "pro": true, "max": true,
	"mini": true, "micro": true, "ultra": true, "super": true, "mega": true,
	"hyper": true, "giga": true, "tera": true,
	"все": true, "всех": true,
	// Polish stop words
	"i": true, "z": true, "do": true, "na": true, "w": true, "o": true,
	"a": true, "ale": true, "oraz": true, "dla": true, "przez": true,
	"przy": true, "pod": true, "nad": true, "między": true, "za": true,
	"przed": true, "po": true, "bez": true, "jako": true, "że": true,
	"który": true, "ta": true, "to": true, "ten": true, "te": true,
}

// TokenInfo holds token data
type TokenInfo struct {
	Hash uint64
	Word string
}

// Tokenize splits text into tokens (hashes + words)
func Tokenize(text string) []TokenInfo {
	if text == "" {
		return nil
	}

	// Normalize: lowercase, replace non-letters with spaces
	var normalized strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(unicode.ToLower(r))
		} else {
			normalized.WriteRune(' ')
		}
	}

	words := strings.Fields(normalized.String())
	var tokens []TokenInfo
	seen := make(map[uint64]bool)

	for _, word := range words {
		// Skip short words
		if len(word) < 4 {
			continue
		}

		// Skip stop words
		if stopWords[word] {
			continue
		}

		// Stem: remove common Russian endings
		stem := stemWord(word)

		// Hash
		hash := fnv64(stem)

		if !seen[hash] {
			seen[hash] = true
			tokens = append(tokens, TokenInfo{
				Hash: hash,
				Word: stem,
			})
		}
	}

	return tokens
}

// stemWord removes common Russian endings
func stemWord(word string) string {
	// Russian plural endings
	for _, ending := range []string{
		"ые", "ые", "их", "ым", "ые", "ые",
		"ые", "ых", "ым", "ие", "ых", "ий",
		"ые", "ые", "ых", "ые", "ые",
		"ов", "ев", "ей", "ам", "ом", "ем",
		"ом", "ом", "ми", "ами", "ыми", "ими",
		"ый", "ая", "ое", "ое", "ий", "яя",
		"ее", "ий", "ий", "ая", "ое", "ое",
		"ий", "ий", "ий", "ый", "ая", "ое",
		"ое", "ий", "ий", "ий", "ий",
		"ость", "ение", "ание", "ение", "ание",
		"еть", "ать", "ить", "еть", "ать",
		"ний", "ния", "ние", "ний", "ния",
		"ний", "ний", "ний", "ний",
		"ный", "ная", "ное", "ные", "ный",
		"ная", "ное", "ные", "ный",
		"ский", "ская", "ское", "ские",
		"ской", "скую", "скому", "ском",
		"ском", "ской", "ским", "ским",
		"ской", "ским", "ском", "ском",
		"ской", "ским",
		"нный", "нная", "нное", "нные",
		"нный", "нная", "нное", "нные",
		"нный", "нная", "нное", "нные",
		"ющий", "щая", "щее", "щие",
		"щего", "щую", "щему", "щем",
		"щем", "щей", "щим", "щим",
		"щей", "щим", "щем", "щем",
		"щей", "щим",
		"вший", "вшая", "вшее", "вшие",
		"вшего", "вшую", "вшему", "вшем",
		"вшем", "вшей", "вшим", "вшим",
		"вшей", "вшим", "вшем", "вшем",
		"вшей", "вшим",
		"вший", "вший", "вший", "вший",
		"вший", "вший", "вший", "вший",
	} {
		if strings.HasSuffix(word, ending) && len(word)-len(ending) >= 3 {
			return word[:len(word)-len(ending)]
		}
	}

	return word
}

// fnv64 is FNV-1a 64-bit hash
func fnv64(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	var hash uint64 = offset64
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= prime64
	}
	return hash
}

// TokenizeForCount returns token counts (for building category token index)
func TokenizeForCount(text string) map[uint64]int {
	tokens := make(map[uint64]int)
	for _, t := range Tokenize(text) {
		tokens[t.Hash]++
	}
	return tokens
}

// CountTokenOverlap counts overlapping tokens between two sets
func CountTokenOverlap(tokens1, tokens2 []uint64) int {
	set := make(map[uint64]bool, len(tokens1))
	for _, t := range tokens1 {
		set[t] = true
	}
	count := 0
	for _, t := range tokens2 {
		if set[t] {
			count++
		}
	}
	return count
}

// Stem returns the stem of a single word.
func Stem(word string) string {
	if len(word) < 4 {
		return word
	}
	if stopWords[strings.ToLower(word)] {
		return ""
	}
	return stemWord(strings.ToLower(word))
}

// CachedCategoryTokens holds pre-loaded token index for a category.
// Used for fast batch auto-catalogization without repeated DB reads.
type CachedCategoryTokens struct {
	ID       int64
	IsActive bool
	Tokens   []any
}

// BuildEANTokensFullText combines all text fields of an EAN page for tokenization.
// Used for better matching during catalogization.
func BuildEANTokensFullText(title, description, content string, attrs []model.KeyValue) string {
	var sb strings.Builder
	if title != "" {
		sb.WriteString(title)
		sb.WriteString(" ")
	}
	if description != "" {
		sb.WriteString(description)
		sb.WriteString(" ")
	}
	if content != "" {
		sb.WriteString(content)
		sb.WriteString(" ")
	}
	for _, kv := range attrs {
		if kv.Value != "" {
			sb.WriteString(kv.Value)
			sb.WriteString(" ")
		}
	}
	return strings.TrimSpace(sb.String())
}

// CatalogizeNameFromCache determines the best category for a product name using
// pre-loaded category tokens. Returns category ID or 0 if no match.
func CatalogizeNameFromCache(name string, cached []CachedCategoryTokens) int64 {
	productTokens := Tokenize(name)
	if len(productTokens) == 0 {
		return 0
	}

	productHashes := make([]uint64, len(productTokens))
	for i, t := range productTokens {
		productHashes[i] = t.Hash
	}

	var bestCatID int64
	var bestScore int

	for _, ct := range cached {
		// Build set for this category
		catSet := make(map[uint64]bool, len(ct.Tokens))
		//for _, h := range ct.Tokens {
		//	catSet[h] = true
		//}

		score := 0
		for _, h := range productHashes {
			if catSet[h] {
				score++
			}
		}

		if score > bestScore {
			bestScore = score
			bestCatID = ct.ID
		}
	}

	return bestCatID
}
