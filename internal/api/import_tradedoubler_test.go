package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
)

func TestIsTradedoublerURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://api.tradedoubler.com/1.0/products.json;page=1;pageSize=100;fid=1?token=x", true},
		{"https://api.tradedoubler.com/1.0/products.json?page=1&pageSize=100", true},
		{"https://example.com/1.0/products.json", false},
		{"https://api.tradedoubler.com/1.0/companies.json", false},
		// A paginated feed on a non-Tradedoubler host (e.g. a proxy/mock) is
		// also walked page by page.
		{"http://127.0.0.1:9292/1.0/products.json?page=1&fid=1", true},
		{"", false},
	}
	for _, c := range cases {
		if got := isTradedoublerURL(c.url); got != c.want {
			t.Errorf("isTradedoublerURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestNormalizeTradedoublerURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			"https://api.tradedoubler.com/1.0/products.json;page=1;pageSize=100;fid=123?token=ABC",
			"https://api.tradedoubler.com/1.0/products.json?page=1&pageSize=100&fid=123&token=ABC",
		},
		{
			// No query string, only matrix params.
			"https://api.tradedoubler.com/1.0/products.json;page=2;fid=9",
			"https://api.tradedoubler.com/1.0/products.json?page=2&fid=9",
		},
		{
			// Standard URL (no matrix params) is unchanged.
			"https://api.tradedoubler.com/1.0/products.json?page=1&token=ABC",
			"https://api.tradedoubler.com/1.0/products.json?page=1&token=ABC",
		},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeTradedoublerURL(c.in); got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTradedoublerPageParams(t *testing.T) {
	// Semicolon (matrix) parameters.
	page, pageSize, base, err := tradedoublerPageParams("https://api.tradedoubler.com/1.0/products.json;page=3;pageSize=100;fid=256284?token=ABC")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if page != 3 {
		t.Errorf("page = %d, want 3", page)
	}
	if pageSize != 100 {
		t.Errorf("pageSize = %d, want 100", pageSize)
	}
	if base.Get("page") != "" {
		t.Errorf("base should not contain page, got %q", base.Get("page"))
	}
	if base.Get("fid") != "256284" {
		t.Errorf("fid = %q, want 256284", base.Get("fid"))
	}
	if base.Get("token") != "ABC" {
		t.Errorf("token = %q, want ABC", base.Get("token"))
	}

	// No pageSize => default to the API maximum (1000).
	_, ps, _, err := tradedoublerPageParams("https://api.tradedoubler.com/1.0/products.json?page=1;fid=1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ps != tradedoublerMaxPageSize {
		t.Errorf("default pageSize = %d, want %d", ps, tradedoublerMaxPageSize)
	}

	// pageSize above the cap is clamped to 1000.
	_, ps2, _, err := tradedoublerPageParams("https://api.tradedoubler.com/1.0/products.json?page=1;pageSize=5000")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ps2 != tradedoublerMaxPageSize {
		t.Errorf("clamped pageSize = %d, want %d", ps2, tradedoublerMaxPageSize)
	}
}

func TestTradedoublerPageURL(t *testing.T) {
	raw := "https://api.tradedoubler.com/1.0/products.json;page=1;pageSize=100;fid=256284?token=ABC"
	_, _, base, err := tradedoublerPageParams(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	u, err := tradedoublerPageURL(raw, base, 7)
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	if got := urlQueryValue(t, u, "page"); got != "7" {
		t.Errorf("page = %q, want 7 (url=%s)", got, u)
	}
	if got := urlQueryValue(t, u, "fid"); got != "256284" {
		t.Errorf("fid = %q, want 256284", got)
	}
	if got := urlQueryValue(t, u, "token"); got != "ABC" {
		t.Errorf("token = %q, want ABC", got)
	}
}

func urlQueryValue(t *testing.T, rawURL, key string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return u.Query().Get(key)
}

func TestTradedoublerPrice(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"9900", 99.0, true},    // integer => smallest unit
		{"12345", 123.45, true}, // integer => smallest unit
		{"99.5", 99.5, true},    // fractional => already decimal
		{"0", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, c := range cases {
		var n json.Number
		n = json.Number(c.in)
		got, ok := tradedoublerPrice(n)
		if ok != c.ok {
			t.Errorf("tradedoublerPrice(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("tradedoublerPrice(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestConvertTradedoublerProduct(t *testing.T) {
	cache := map[string]*model.AttrDef{}
	newKeys := map[string]struct{}{}
	tp := tradedoublerProduct{
		ID:            42,
		Name:          "TimeLok 300",
		Description:   "Czytnik czasu pracy",
		Brand:         "HDWR",
		EAN:           "5907614665029",
		Price:         json.Number("37900"),
		PriceCurrency: "PLN",
		ProductURL:    "https://shop.example.com/p/42",
		Stock:         5,
		Images:        []tradedoublerImage{{URL: "https://img.example.com/1.jpg"}},
		Categories:    []tradedoublerCategory{{Name: "Rejestratory"}},
	}
	p, skip := convertTradedoublerProduct(tp, 7, "testco", "TestCo", "PLN", cache, newKeys)
	if skip {
		t.Fatal("should not skip")
	}
	if p.EAN != "5907614665029" {
		t.Errorf("EAN = %q", p.EAN)
	}
	if p.Price != 379.0 {
		t.Errorf("Price = %v, want 379.0", p.Price)
	}
	if p.Currency != "PLN" {
		t.Errorf("Currency = %q", p.Currency)
	}
	if p.Brand != "HDWR" {
		t.Errorf("Brand = %q", p.Brand)
	}
	if p.StockQty != 5 {
		t.Errorf("StockQty = %d, want 5", p.StockQty)
	}
	if len(p.Images) != 1 || p.Images[0] != "https://img.example.com/1.jpg" {
		t.Errorf("Images = %v", p.Images)
	}
	if p.Name != "TimeLok 300 — TestCo" {
		t.Errorf("Name = %q", p.Name)
	}
	// Brand and category attributes present.
	foundBrand, foundCat := false, false
	for _, a := range p.Attributes {
		if a.Key == "brand" && a.Value == "HDWR" {
			foundBrand = true
		}
		if a.Key == "category" && a.Value == "Rejestratory" {
			foundCat = true
		}
	}
	if !foundBrand {
		t.Errorf("brand attribute missing: %v", p.Attributes)
	}
	if !foundCat {
		t.Errorf("category attribute missing: %v", p.Attributes)
	}
	if _, ok := newKeys["brand"]; !ok {
		t.Errorf("newKeys should contain brand")
	}
}

// TestDownloadTradedoublerPagination simulates a paginated Tradedoubler API and
// verifies the importer walks every page and collects all products.
func TestDownloadTradedoublerPagination(t *testing.T) {
	// Serve 3 pages of 2 products each (6 total), with meta.totalPages=3.
	const perPage = 2
	const total = 6
	mux := http.NewServeMux()
	mux.HandleFunc("/products.json", func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if v := r.URL.Query().Get("page"); v != "" {
			fmt.Sscanf(v, "%d", &page)
		}
		if fid := r.URL.Query().Get("fid"); fid != "256284" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"errors":[{"code":"PF_392","message":"not connected"}]}`)
			return
		}
		start := (page - 1) * perPage
		end := start + perPage
		if end > total {
			end = total
		}
		var products []map[string]any
		for i := start; i < end; i++ {
			products = append(products, map[string]any{
				"id":            i + 1,
				"name":          fmt.Sprintf("Product %d", i+1),
				"ean":           fmt.Sprintf("590000000000%d", i+1),
				"price":         (i + 1) * 1000, // smallest unit
				"priceCurrency": "PLN",
				"brand":         "BrandX",
			})
		}
		resp := map[string]any{
			"products": products,
			"meta": map[string]any{
				"page":       page,
				"pageSize":   perPage,
				"totalItems": total,
				"totalPages": 3,
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Build a URL pointing at the test server (tradedoubler host not required
	// for downloadTradedoubler; it only needs a valid URL with page params).
	rawURL := srv.URL + "/products.json?page=1&pageSize=2&fid=256284&token=ABC"
	client := tradedoublerClient()
	got, err := downloadTradedoubler(client, rawURL, 0)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(got) != total {
		t.Fatalf("got %d products, want %d", len(got), total)
	}
	// Verify the first and last product names.
	if got[0].Name != "Product 1" {
		t.Errorf("first = %q, want Product 1", got[0].Name)
	}
	if got[total-1].Name != "Product 6" {
		t.Errorf("last = %q, want Product 6", got[total-1].Name)
	}
}

// TestDownloadTradedoublerLimit verifies the limit stops the walk early.
func TestDownloadTradedoublerLimit(t *testing.T) {
	const perPage = 2
	const total = 6
	mux := http.NewServeMux()
	mux.HandleFunc("/products.json", func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if v := r.URL.Query().Get("page"); v != "" {
			fmt.Sscanf(v, "%d", &page)
		}
		start := (page - 1) * perPage
		end := start + perPage
		if end > total {
			end = total
		}
		var products []map[string]any
		for i := start; i < end; i++ {
			products = append(products, map[string]any{
				"id":    i + 1,
				"name":  fmt.Sprintf("Product %d", i+1),
				"price": 1000,
			})
		}
		resp := map[string]any{
			"products": products,
			"meta":     map[string]any{"page": page, "totalPages": 3, "totalItems": total},
		}
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rawURL := srv.URL + "/products.json?page=1&pageSize=2&fid=1&token=ABC"
	client := tradedoublerClient()
	got, err := downloadTradedoubler(client, rawURL, 3)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d products, want 3 (limit)", len(got))
	}
}
