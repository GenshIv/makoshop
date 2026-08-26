package pricesrc

import (
	"os"
	"testing"
)

func TestParseNokaut(t *testing.T) {
	// Test on a small real file if available.
	files := []string{
		"../../prices/RoboJet/bafe89f4-3bd5-4a79-87c6-e36408d80bcb.xml",
		"../../prices/Nexus/44da43cf-dda4-450c-b578-12b249b15ea4.xml",
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Logf("skip %s: %v", f, err)
			continue
		}
		file, err := os.Open(f)
		if err != nil {
			t.Fatalf("open %s: %v", f, err)
		}
		p := NewNokautParser()
		count, err := p.Parse(file, func(o Offer) error {
			// Just validate structure on first offer.
			if o.Name == "" {
				t.Errorf("offer with empty name in %s", f)
			}
			return nil
		})
		file.Close()
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		t.Logf("%s: parsed %d offers", f, count)
		if count == 0 {
			t.Errorf("expected offers in %s", f)
		}
	}
}

func TestParsePrice(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"799", 799},
		{"139,9", 139.9},
		{"1.234,56", 1234.56},
		{"1234.56", 1234.56},
		{"1,234", 1234},
		{"28,99", 28.99},
		{"", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		got := ParsePrice(c.in)
		if got != c.want {
			t.Errorf("ParsePrice(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestExtractEAN(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"5902560337471", "5902560337471"},
		{"4016803055914; 4016803055921", "4016803055914"},
		{"9788324050611", "9788324050611"},
		{"", ""},
		{"not-an-ean", ""},
		{"123456789012", "123456789012"}, // 12-digit UPC
	}
	for _, c := range cases {
		got := ExtractEAN(c.in)
		if got != c.want {
			t.Errorf("ExtractEAN(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"  Hello   World  ", "hello world"},
		{"ABC\tDEF", "abc def"},
		{"", ""},
	}
	for _, c := range cases {
		got := NormalizeName(c.in)
		if got != c.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
