package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
)

func TestPriceFileExt(t *testing.T) {
	cases := []struct {
		name     string
		rawURL   string
		defExt   string
		expected string
	}{
		{"plain xml", "https://x.com/prices/company.xml", ".xml", ".xml"},
		{"plain json", "https://x.com/prices/company.json", ".json", ".json"},
		{"json with matrix params", "https://api.tradedoubler.com/1.0/products.json;page=1;pageSize=1000;fid=259170", ".json", ".json"},
		{"json with query string", "https://x.com/prices?file=company.json", ".json", ".json"},
		{"no extension", "https://x.com/prices/company", ".xml", ".xml"},
		{"empty url", "", ".xml", ".xml"},
		{"too long ext falls back", "https://x.com/a.b.c.def.ghijklmnopqr", ".xml", ".xml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := priceFileExt(tc.rawURL, tc.defExt)
			if got != tc.expected {
				t.Errorf("priceFileExt(%q, %q) = %q, want %q", tc.rawURL, tc.defExt, got, tc.expected)
			}
		})
	}
}

func TestDetectPriceFormat(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	cases := []struct {
		name     string
		content  string
		expected string
	}{
		{"json object", `{"offers":[]}`, "json"},
		{"json array", `[{"ean":"123"}]`, "json"},
		{"json with leading whitespace", "   \n\t {\"offers\":[]}", "json"},
		{"json with BOM", "\xEF\xBB\xBF{\"offers\":[]}", "json"},
		{"xml nokaut", `<?xml version="1.0"?><nokaut><offers/></nokaut>`, "nokaut"},
		{"xml with leading newline", "\n<?xml version=\"1.0\"?><root/>", "nokaut"},
		{"empty", "", ""},
		{"unknown", "hello world", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := write(tc.name+".tmp", tc.content)
			got := detectPriceFormat(p)
			if got != tc.expected {
				t.Errorf("detectPriceFormat(%q) = %q, want %q", tc.content, got, tc.expected)
			}
		})
	}

	// Missing file returns "".
	if got := detectPriceFormat(filepath.Join(dir, "missing.tmp")); got != "" {
		t.Errorf("detectPriceFormat(missing) = %q, want empty", got)
	}
}

func TestPriceSourceNormalize(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		expected string
	}{
		{"empty defaults to nokaut", "", "nokaut"},
		{"lowercases", "JSON", "json"},
		{"trims whitespace", "  nokaut  ", "nokaut"},
		{"xml alias maps to nokaut", "xml", "nokaut"},
		{"xml alias upper maps to nokaut", " XML ", "nokaut"},
		{"already nokaut", "nokaut", "nokaut"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &model.PriceSourceConfig{Format: tc.in}
			p.Normalize()
			if p.Format != tc.expected {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, p.Format, tc.expected)
			}
		})
	}

	// Nil receiver is safe.
	var nilP *model.PriceSourceConfig
	nilP.Normalize()
}
