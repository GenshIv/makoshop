package pricesrc

import (
	"regexp"
	"strings"

	"github.com/GenshIv/makoshop/internal/model"
)

// HTMLAttrParser extracts attributes from HTML descriptions using configured rules
type HTMLAttrParser struct {
	rules    []model.HTMLAttrRule
	compiled map[string]*regexp.Regexp
}

// NewHTMLAttrParser creates a new HTML attribute parser
func NewHTMLAttrParser(rules []model.HTMLAttrRule) *HTMLAttrParser {
	p := &HTMLAttrParser{
		rules:    rules,
		compiled: make(map[string]*regexp.Regexp),
	}

	// Compile regex patterns
	for _, rule := range rules {
		if rule.Pattern != "" {
			re, err := regexp.Compile(rule.Pattern)
			if err == nil {
				p.compiled[rule.Code] = re
			}
		}
	}

	return p
}

// Parse extracts attributes from HTML content
func (p *HTMLAttrParser) Parse(htmlContent string) map[string]string {
	if htmlContent == "" || len(p.rules) == 0 {
		return nil
	}

	// Strip HTML tags to get plain text for regex matching
	text := stripHTML(htmlContent)

	attrs := make(map[string]string)

	for _, rule := range p.rules {
		re, ok := p.compiled[rule.Code]
		if !ok {
			continue
		}

		match := re.FindStringSubmatch(text)
		if match == nil {
			continue
		}

		// Get the capture group
		groupIdx := rule.Group
		if groupIdx <= 0 || groupIdx >= len(match) {
			groupIdx = 1
			if groupIdx >= len(match) {
				continue
			}
		}

		value := strings.TrimSpace(match[groupIdx])

		// Apply transform
		value = p.transform(value, rule.Transform)

		if value != "" {
			attrs[rule.Code] = value
		}
	}

	return attrs
}

// transform applies the configured transformation to the value
func (p *HTMLAttrParser) transform(value, transform string) string {
	switch transform {
	case "lowercase":
		return strings.ToLower(value)
	case "uppercase":
		return strings.ToUpper(value)
	case "trim":
		return strings.TrimSpace(value)
	case "clean_html":
		return stripHTML(value)
	default:
		return value
	}
}

// stripHTML removes HTML tags from content
func stripHTML(html string) string {
	return StripHTMLEntities(html)
}

// StripHTMLEntities removes HTML tags and entities from content, returning plain text
func StripHTMLEntities(html string) string {
	if html == "" {
		return ""
	}

	// Replace HTML entities
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = strings.ReplaceAll(html, "&#39;", "'")

	// Remove HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	html = re.ReplaceAllString(html, " ")

	// Clean up whitespace
	html = regexp.MustCompile(`\s+`).ReplaceAllString(html, " ")

	return strings.TrimSpace(html)
}
