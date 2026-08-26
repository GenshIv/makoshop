package pricesrc

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Offer is a single parsed offer from a price file (company price list).
type Offer struct {
	ID           string
	Name         string
	Description  string
	URL          string
	Image        string
	Price        string // raw price string (may use Polish comma)
	Category     string
	ShopCategory string
	Producer     string
	Availability string // raw availability value
	Shipping     string
	Props        map[string]string // named <property name="...">
	AnonProps    []string          // anonymous <property> values in order
}

// NokautParser streams offers from a Nokaut-format XML price file.
type NokautParser struct {
	// Config is optional; parsing is format-agnostic. Field mapping is done
	// by the caller using PriceSourceConfig.
	Config interface{}
}

// NewNokautParser creates a NokautParser.
func NewNokautParser() *NokautParser {
	return &NokautParser{}
}

// Parse streams offers from r, calling fn for each offer. It is memory
// efficient (token-based) and suitable for very large files.
// Returns the number of offers processed and any fatal error.
func (p *NokautParser) Parse(r io.Reader, fn func(Offer) error) (int, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		// Assume UTF-8; Nokaut files declare utf-8.
		return input, nil
	}

	count := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("xml token: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "offer" {
			offer, err := p.readOffer(dec)
			if err != nil {
				if err == io.EOF {
					break
				}
				return count, fmt.Errorf("read offer: %w", err)
			}
			if err := fn(offer); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

// readOffer reads the children of an <offer> element until its end tag.
func (p *NokautParser) readOffer(dec *xml.Decoder) (Offer, error) {
	var offer Offer
	offer.Props = make(map[string]string)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return offer, io.EOF
		}
		if err != nil {
			return offer, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "id":
				offer.ID = readText(dec)
			case "name":
				offer.Name = readText(dec)
			case "description":
				offer.Description = readText(dec)
			case "url":
				offer.URL = readText(dec)
			case "image":
				offer.Image = readText(dec)
			case "price":
				offer.Price = readText(dec)
			case "category":
				offer.Category = readText(dec)
			case "shopcategory":
				offer.ShopCategory = readText(dec)
			case "producer":
				offer.Producer = readText(dec)
			case "availability":
				offer.Availability = readText(dec)
			case "shipping":
				offer.Shipping = readText(dec)
			case "property":
				name := ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						name = a.Value
						break
					}
				}
				val := readText(dec)
				if name != "" {
					offer.Props[name] = val
				} else {
					offer.AnonProps = append(offer.AnonProps, val)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "offer" {
				return offer, nil
			}
		}
	}
}

// readText reads the character content of an element (assumes no nested tags).
func readText(dec *xml.Decoder) string {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return sb.String()
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			return sb.String()
		}
	}
}

// ParsePrice parses a price string that may use Polish decimal comma.
// Examples: "799", "139,9", "1.234,56", "1234.56".
func ParsePrice(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Remove thousands separators and non-breaking spaces.
	s = strings.ReplaceAll(s, "\u00a0", "")
	s = strings.ReplaceAll(s, " ", "")

	lastDot := strings.LastIndex(s, ".")
	lastComma := strings.LastIndex(s, ",")

	if lastDot >= 0 && lastComma >= 0 {
		if lastComma > lastDot {
			// "1.234,56" (EU) -> comma is last, so it's the decimal point
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			// "1,234.56" (US) -> dot is last, so it's the decimal point
			s = strings.ReplaceAll(s, ",", "")
		}
	} else if lastComma >= 0 {
		afterComma := s[lastComma+1:]
		if len(afterComma) <= 2 {
			// "139,9" -> decimal comma
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			// "1,234" -> thousands comma
			s = strings.ReplaceAll(s, ",", "")
		}
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// NormalizeName normalizes a product name for uniqueness comparison.
// Lowercases, trims, and collapses whitespace.
func NormalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// ExtractEAN extracts a clean EAN from a raw value.
// Handles multiple EANs separated by ";", commas, or whitespace (takes the first).
// Returns only digits; empty string if no valid EAN found.
func ExtractEAN(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Take the first candidate.
	for _, sep := range []string{";", ",", " ", "/"} {
		if i := strings.Index(raw, sep); i >= 0 {
			raw = raw[:i]
		}
	}
	raw = strings.TrimSpace(raw)
	// Keep only digits.
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	e := b.String()
	// Valid EAN lengths: 8, 12, 13, 14. Otherwise return empty (or as-is?).
	if len(e) == 8 || len(e) == 12 || len(e) == 13 || len(e) == 14 {
		return e
	}
	// If it looks like an EAN (all digits, reasonable length), keep it.
	if len(e) >= 6 && len(e) <= 20 {
		return e
	}
	return ""
}
