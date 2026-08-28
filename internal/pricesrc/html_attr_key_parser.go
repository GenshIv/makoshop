package pricesrc

import (
	"html"
	"regexp"
	"strings"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
)

// HTMLAttrKeyParser extracts attributes from HTML descriptions.
// It finds raw key-value pairs (e.g. "Moc - 500 W") and maps them to
// attribute codes using the AttrDefRepo key index.
type HTMLAttrKeyParser struct {
	attrDefRepo *db.AttrDefRepo
}

// NewHTMLAttrKeyParser creates a new HTML attribute key parser.
func NewHTMLAttrKeyParser(attrDefRepo *db.AttrDefRepo) *HTMLAttrKeyParser {
	return &HTMLAttrKeyParser{
		attrDefRepo: attrDefRepo,
	}
}

// Parse extracts attributes from HTML content.
// Returns a map of attribute code -> value.
func (p *HTMLAttrKeyParser) Parse(htmlContent string) map[string][]string {
	if htmlContent == "" || p.attrDefRepo == nil {
		return nil
	}

	pairs := extractKeyValuePairs(htmlContent)
	if len(pairs) == 0 {
		return nil
	}

	attrs := make(map[string][]string)

	for _, pair := range pairs {
		ad, err := p.attrDefRepo.GetOrCreateByKey(pair.Key)
		if err != nil {
			continue
		}

		value := strings.TrimSpace(pair.Value)
		if value != "" {
			// Split by commas if present
			if strings.Contains(value, ",") {
				parts := strings.Split(value, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if part != "" {
						attrs[ad.Code] = append(attrs[ad.Code], part)
					}
				}
			} else {
				attrs[ad.Code] = append(attrs[ad.Code], value)
			}
		}
	}

	return attrs
}

// KeyValuePair represents a raw key-value pair from HTML.
type KeyValuePair struct {
	Key   string
	Value string
}

// extractKeyValuePairs finds key-value pairs in HTML content.
// Processes each <li> separately to extract text while preserving structure.
// Format: "Key - value" or "Key: value" in list items.
func extractKeyValuePairs(htmlContent string) []KeyValuePair {
	if htmlContent == "" {
		return nil
	}

	var pairs []KeyValuePair

	// Split into list items
	items := splitListItems(htmlContent)

	for _, item := range items {
		// Extract text from item (remove inline styles, keep content)
		text := extractTextFromListItem(item)
		if len(text) < 10 {
			continue
		}

		// Match "Key - value" or "Key: value" or "Key-value"
		match := regexp.MustCompile(`^([A-ZĄĆĘŁŃÓŚŹŻ][a-ząćęłńóśźż\s]{1,45})\s*[-:]\s*(.+)$`).FindStringSubmatch(text)
		if match == nil {
			continue
		}

		key := strings.TrimSpace(match[1])
		value := strings.TrimSpace(match[2])

		if len(key) < 2 || len(key) > 50 {
			continue
		}
		if len(value) < 3 || len(value) > 500 {
			continue
		}

		if isNoiseKey(key) {
			continue
		}

		pairs = append(pairs, KeyValuePair{Key: key, Value: value})
	}

	return pairs
}

// splitListItems splits HTML content into individual <li> items.
func splitListItems(htmlContent string) []string {
	var items []string

	// Normalize whitespace: replace newlines with spaces inside tags
	normalized := regexp.MustCompile(`(?s)<li[^>]*>(.*?)</li>`)
	matches := normalized.FindAllStringSubmatch(htmlContent, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			items = append(items, match[1])
		}
	}

	// If no list items found, split by newlines and paragraphs
	if len(items) == 0 {
		// Split by </p> tags
		parts := regexp.MustCompile(`<[/]?p[^>]*>`).Split(htmlContent, -1)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if len(part) > 0 {
				items = append(items, part)
			}
		}
	}

	return items
}

// extractTextFromListItem extracts plain text from a list item.
// Removes inline styles and HTML tags, but preserves the content.
func extractTextFromListItem(item string) string {
	// Remove inline style attributes (keep other attributes)
	reStyle := regexp.MustCompile(`\s*style="[^"]*"`)
	text := reStyle.ReplaceAllString(item, "")

	// Remove HTML tags
	reTags := regexp.MustCompile(`<[^>]*>`)
	text = reTags.ReplaceAllString(text, " ")

	// Unescape HTML entities
	text = html.UnescapeString(text)

	// Clean whitespace
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

// isNoiseKey checks if a key looks like noise (generic headings, book descriptions).
func isNoiseKey(key string) bool {
	keyLower := strings.ToLower(strings.TrimSpace(key))

	// Generic headings that describe product groups, not individual attributes
	genericHeadings := []string{
		"najważniejsze cechy",
		"najwazniejsze cechy",
		"najważniesze cechy",
		"opis produktu",
		"opis",
		"specyfikacja",
		"specyfikacja produktu",
		"specyfiakacja produktu",
		"charakterystyka",
		"cechy produktu",
		"cechy produkut",
		"cechy produtu",
		"cechy",
		"dane techniczne",
		"informacje",
		"dodatkowe informacje",
		"uwagi",
		"uwaga",
		"zawartość",
		"w zestawie",
		"zestaw zawiera",
		"zawartość pudełka",
		"zawartość teczki",
		"w skład pakietu wchodzą",
		"zestaw",
		"fantastyczny zestaw zawierający",
		"zestaw zawiera propozycje zabawnych masek",
		"zestaw zawiera trzy arkusze naklejek",
		"zestaw zawiera trzy arkusze",
		"zestaw zawiera trzy książki",
		"zestaw zawiera dwie książki",
		"zestaw zawiera trzy",
		"zestaw zawiera dwa",
		"zestaw mieszanek do wypieku chleba",
		"zestaw do gry w badmintona zawartość",
	}

	for _, pattern := range genericHeadings {
		if keyLower == pattern {
			return true
		}
	}

	// Book/literature noise patterns (substring match)
	bookNoise := []string{
		"książka", "opowiada", "bohater", "bohaterka", "historia", "autor", "wydawnictwo",
		"przewodnik", "podróż", "miasto", "kraj", "region", "góry", "rzeka",
		"miłość", "życie", "świat", "marzenia", "przyszłość", "przeszłość",
		"dziecko", "rodzina", "dom", "szkoła", "nauka", "edukacja",
		"zdrowie", "choroba", "lekarz", "leczenie", "dieta", "odżywianie",
		"sport", "trening", "ćwiczenie", "fitness", "siłownia",
		"piękno", "moda", "styl", "ubranie", "buty", "dodatki",
		"gotowanie", "kuchnia", "przepis", "smak", "jedzenie",
		"muzyka", "film", "serial", "gra", "zabawa", "rozrywka",
		"religia", "wiara", "bóg", "modlitwa", "kościół",
		"polityka", "władza", "demokracja", "wolność", "prawo",
		"kocham", "serce", "uczucia", "siła",
		"o książce", "krótko o", "w tej książce", "w tej niesamowitej",
		"książka opowiada", "książka omawia",
		"kolorowanka", "magiczne wodne",
		"mini album", "album fotograficzny",
		"piórnik", "teczka", "zeszyt", "kołozeszyt",
		"blok techniczny", "blok z papierem",
		"kartka na urodzinki",
		"serwetki papierowe",
		"klipy biurowe", "klipy do papieru",
		"torebka ozdobna",
		"napisz i zetrzyj",
		"farby wodno", "farba szkolna",
		"pędzle", "pastele", "akwarele",
		"kredki", "żelopisy",
		"taśma korekcyjna",
		"korektor",
		"długopis", "zakreślacze", "temperówka",
		"naklejki", "bibuła", "krepina",
		"druciki", "miedziany drucik",
		"okładka na dokumenty",
		"koperty",
		"kalendarz", "terminarz",
		"słownik", "podręczny słownik",
		"plansze do kolorowania",
		"zestaw do gry",
		"grzechotka",
		"materiały na zajęcia",
		"pomoc edukacyjno",
		"praca z zeszytem",
		"wspomnienia mają", "słowa mają", "prawda ma",
		"biblia jest", "klasyczne baśnie",
		"epicka opowieść", "romans wszech czasów",
		"podczas wojny", "podczas lektury",
		"pierwszy album", "piąty album",
		"czasy ostateczne",
		"wyścigowy potwór",
		"zapnijcie pasy",
		"czytanie jest", "odwaga to",
		"człowiek składa się",
		"druciki kreatywne",
		"czas na", "czas na zegar",
		"mocniej i dłużej",
		"na mapie", "jeśli czasem",
		"samochody osobowe",
		"dwie talie",
		"wysokiej jakości materiały",
	}

	for _, pattern := range bookNoise {
		if strings.Contains(keyLower, pattern) {
			return true
		}
	}

	return false
}

// ParseToModelAttributes converts the parsed map to model.KeyValue slice.
func (p *HTMLAttrKeyParser) ParseToModelAttributes(htmlContent string) []model.KeyValue {
	attrs := p.Parse(htmlContent)
	if len(attrs) == 0 {
		return nil
	}

	result := make([]model.KeyValue, 0, len(attrs))
	for code, values := range attrs {
		for _, value := range values {
			result = append(result, model.KeyValue{Key: code, Value: value})
		}
	}

	return result
}

// CleanHTMLDescription sanitizes HTML for safe display on the website.
// Removes inline styles, script tags, and event handlers.
// Preserves structure: ul, li, p, strong, span, br, etc.
func CleanHTMLDescription(htmlContent string) string {
	if htmlContent == "" {
		return ""
	}

	clean := htmlContent

	// Remove script tags and their content (Go regexp doesn't support lookahead)
	for {
		start := strings.Index(clean, "<script")
		if start == -1 {
			break
		}
		end := strings.Index(clean[start:], "</script>")
		if end == -1 {
			clean = clean[:start]
			break
		}
		clean = clean[:start] + clean[start+end+len("</script>"):]
	}

	// Remove event handlers (onclick, onload, etc.)
	reEvents := regexp.MustCompile(`\s+on\w+="[^"]*"`)
	clean = reEvents.ReplaceAllString(clean, "")
	reEventsSingle := regexp.MustCompile(`\s+on\w+='[^']*'`)
	clean = reEventsSingle.ReplaceAllString(clean, "")
	reEventsNoQuotes := regexp.MustCompile(`\s+on\w+=\w+`)
	clean = reEventsNoQuotes.ReplaceAllString(clean, "")

	// Remove inline style attributes (Tailwind prose will handle styling)
	reStyle := regexp.MustCompile(`\s+style="[^"]*"`)
	clean = reStyle.ReplaceAllString(clean, "")

	// Unescape HTML entities
	clean = html.UnescapeString(clean)

	return strings.TrimSpace(clean)
}

// TruncateHTML safely truncates HTML to maxLen characters without breaking tags.
// Returns truncated HTML with all open tags properly closed.
func TruncateHTML(htmlContent string, maxLen int) string {
	if len(htmlContent) <= maxLen {
		return htmlContent
	}

	truncated := htmlContent[:maxLen]

	// Find the last complete tag (ending with >)
	lastTagEnd := strings.LastIndex(truncated, ">")
	if lastTagEnd == -1 {
		// No tags found, just truncate
		return truncated
	}

	// Truncate at the last complete tag
	truncated = truncated[:lastTagEnd+1]

	// Track open tags that need to be closed
	openTags := []string{}
	tagRe := regexp.MustCompile(`<(\w+)(?:\s[^>]*)?>|</(\w+)?>`)

	for _, match := range tagRe.FindAllStringSubmatch(truncated, -1) {
		if match[1] != "" {
			// Opening tag (not self-closing)
			tag := strings.ToLower(match[1])
			// Skip self-closing tags
			if tag != "br" && tag != "hr" && tag != "img" && tag != "input" &&
				tag != "meta" && tag != "link" && tag != "area" && tag != "base" &&
				tag != "col" && tag != "embed" && tag != "param" && tag != "source" &&
				tag != "track" && tag != "wbr" {
				openTags = append(openTags, tag)
			}
		} else if match[2] != "" {
			// Closing tag - remove from stack
			tag := strings.ToLower(match[2])
			for i := len(openTags) - 1; i >= 0; i-- {
				if openTags[i] == tag {
					openTags = openTags[:i]
					break
				}
			}
		}
	}

	// Close remaining open tags in reverse order
	for i := len(openTags) - 1; i >= 0; i-- {
		truncated += "</" + openTags[i] + ">"
	}

	return truncated
}
