package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
)

// mustParseJSONLDBlock parses a JSON-LD block into a map and fails the test on
// invalid JSON.
func mustParseJSONLDBlock(t *testing.T, block string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(block), &m); err != nil {
		t.Fatalf("block is not valid JSON: %v\nblock: %s", err, block)
	}
	return m
}

func TestBuildSiteJSONLDBlocks(t *testing.T) {
	s := &model.SEOSettings{
		Enabled:           true,
		OrgName:           "HDWR",
		OrgLegalName:      "HDWR Sp. z o.o.",
		OrgLogo:           "https://hdwr.pl/logo.svg",
		OrgPhone:          "+48-456-456-001",
		OrgEmail:          "biuro@hdwr.pl",
		OrgStreet:         "ul. Dmowskiego 28",
		OrgCity:           "Środa Wielkopolska",
		OrgPostalCode:     "63-000",
		OrgCountry:        "PL",
		OrgSameAs:         []string{"https://facebook.com/hdwr", "https://instagram.com/hdwr"},
		SiteName:          "HDWR - Sprzęt",
		SearchURLTemplate: "/shop?q={search_term_string}",
		StoreName:         "HDWR Store",
		StoreLogo:         "https://hdwr.pl/logo.jpg",
	}
	baseURL := "https://hdwr.pl"
	blocks := buildSiteJSONLDBlocks(s, baseURL, "hdwr.pl")

	// Expect 3 blocks: Organization, WebSite, OnlineStore.
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	org := mustParseJSONLDBlock(t, blocks[0])
	if org["@type"] != "Organization" {
		t.Errorf("block 0 @type = %v, want Organization", org["@type"])
	}
	if org["name"] != "HDWR" {
		t.Errorf("org name = %v, want HDWR", org["name"])
	}
	if org["legalName"] != "HDWR Sp. z o.o." {
		t.Errorf("org legalName = %v", org["legalName"])
	}
	cp, _ := org["contactPoint"].(map[string]any)
	if cp == nil || cp["telephone"] != "+48-456-456-001" {
		t.Errorf("org contactPoint.telephone missing or wrong: %v", cp)
	}
	addr, _ := org["address"].(map[string]any)
	if addr == nil || addr["addressCountry"] != "PL" {
		t.Errorf("org address.addressCountry missing or wrong: %v", addr)
	}
	sameAs, _ := org["sameAs"].([]any)
	if len(sameAs) != 2 {
		t.Errorf("org sameAs len = %d, want 2", len(sameAs))
	}

	website := mustParseJSONLDBlock(t, blocks[1])
	if website["@type"] != "WebSite" {
		t.Errorf("block 1 @type = %v, want WebSite", website["@type"])
	}
	pa, _ := website["potentialAction"].(map[string]any)
	if pa == nil || pa["@type"] != "SearchAction" {
		t.Fatalf("website potentialAction missing: %v", pa)
	}
	target, _ := pa["target"].(map[string]any)
	if target == nil || target["urlTemplate"] != "https://hdwr.pl/shop?q={search_term_string}" {
		t.Errorf("searchAction urlTemplate = %v", target)
	}

	store := mustParseJSONLDBlock(t, blocks[2])
	if store["@type"] != "OnlineStore" {
		t.Errorf("block 2 @type = %v, want OnlineStore", store["@type"])
	}
	if store["name"] != "HDWR Store" {
		t.Errorf("store name = %v, want HDWR Store", store["name"])
	}
}

func TestBuildSiteJSONLDBlocksDisabled(t *testing.T) {
	s := &model.SEOSettings{Enabled: false, OrgName: "X"}
	blocks := buildSiteJSONLDBlocks(s, "https://x.pl", "x.pl")
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks when disabled, got %d", len(blocks))
	}
}

func TestBuildSiteJSONLDBlocksMinimal(t *testing.T) {
	// Only org name set: should still emit Organization + WebSite + OnlineStore
	// (with fallbacks), and omit empty optional fields.
	s := &model.SEOSettings{Enabled: true, OrgName: "ACME"}
	blocks := buildSiteJSONLDBlocks(s, "https://acme.pl", "acme.pl")
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	org := mustParseJSONLDBlock(t, blocks[0])
	if _, has := org["contactPoint"]; has {
		t.Errorf("contactPoint should be omitted when no phone/email")
	}
	if _, has := org["address"]; has {
		t.Errorf("address should be omitted when empty")
	}
	if _, has := org["sameAs"]; has {
		t.Errorf("sameAs should be omitted when empty")
	}
	// WebSite should still have a SearchAction with the default template.
	website := mustParseJSONLDBlock(t, blocks[1])
	pa, _ := website["potentialAction"].(map[string]any)
	if pa == nil {
		t.Errorf("website should have default SearchAction")
	}
}

func TestBuildProductJSONLDBlocks(t *testing.T) {
	s := &model.SEOSettings{Enabled: true, OrgName: "HDWR", PriceValidDays: 30}
	baseURL := "https://hdwr.pl"
	ep := &model.EANPage{
		EAN:          "5907614665029",
		Title:        "TimeLok 300",
		Description:  "Czytnik czasu pracy",
		Brand:        "HDWR",
		MinPrice:     379,
		Currency:     "PLN",
		ProductCount: 2,
		Images:       []string{"https://hdwr.pl/img1.webp", "https://hdwr.pl/img2.webp"},
		SeoURL:       "/shop/Rejestratory/TimeLok-300",
	}
	products := []model.Product{
		{SKU: "TimeLok-300NWEPBI", AvgRating: 4.5, ReviewCount: 2},
	}
	treePath := []string{"Rejestratory"}
	treePathFull := []db.CategoryTreeNode{{Name: "Rejestratory czasu pracy", Slug: "Rejestratory"}}

	blocks := buildProductJSONLDBlocks(s, baseURL, ep, products, nil, treePath, treePathFull)
	// Expect 2 blocks: Product, BreadcrumbList. (OnlineStore is a site-level
	// block, emitted separately by buildSiteJSONLDBlocks.)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d: %v", len(blocks), blocks)
	}

	p := mustParseJSONLDBlock(t, blocks[0])
	if p["@type"] != "Product" {
		t.Errorf("block 0 @type = %v, want Product", p["@type"])
	}
	if p["name"] != "TimeLok 300" {
		t.Errorf("product name = %v", p["name"])
	}
	if p["sku"] != "TimeLok-300NWEPBI" {
		t.Errorf("product sku = %v", p["sku"])
	}
	if p["gtin13"] != "5907614665029" {
		t.Errorf("product gtin13 = %v", p["gtin13"])
	}
	brand, _ := p["brand"].(map[string]any)
	if brand == nil || brand["name"] != "HDWR" {
		t.Errorf("product brand = %v", brand)
	}
	offers, _ := p["offers"].(map[string]any)
	if offers == nil {
		t.Fatalf("product offers missing")
	}
	if offers["price"] != 379.0 {
		t.Errorf("offer price = %v, want 379", offers["price"])
	}
	if offers["priceCurrency"] != "PLN" {
		t.Errorf("offer currency = %v", offers["priceCurrency"])
	}
	if offers["availability"] != "https://schema.org/InStock" {
		t.Errorf("offer availability = %v", offers["availability"])
	}
	if _, has := offers["priceValidUntil"]; !has {
		t.Errorf("offer priceValidUntil missing")
	}
	images, _ := p["image"].([]any)
	if len(images) != 2 {
		t.Errorf("product image len = %d, want 2", len(images))
	}
	rating, _ := p["aggregateRating"].(map[string]any)
	if rating == nil {
		t.Fatalf("product aggregateRating missing")
	}
	if rc, _ := rating["reviewCount"].(float64); rc != 2 {
		t.Errorf("aggregateRating reviewCount = %v, want 2", rating["reviewCount"])
	}

	// BreadcrumbList: Home + 1 category + product = 3 items.
	bc := mustParseJSONLDBlock(t, blocks[1])
	if bc["@type"] != "BreadcrumbList" {
		t.Errorf("block 1 @type = %v, want BreadcrumbList", bc["@type"])
	}
	items, _ := bc["itemListElement"].([]any)
	if len(items) != 3 {
		t.Fatalf("breadcrumb items = %d, want 3", len(items))
	}
	first := items[0].(map[string]any)
	if first["position"] != 1.0 {
		t.Errorf("first crumb position = %v, want 1", first["position"])
	}
	last := items[2].(map[string]any)
	lastItem, _ := last["item"].(map[string]any)
	if lastItem == nil || lastItem["name"] != "TimeLok 300" {
		t.Errorf("last crumb should be the product: %v", last)
	}
}

func TestBuildProductJSONLDBlocksNoPrice(t *testing.T) {
	s := &model.SEOSettings{Enabled: true, OrgName: "ACME"}
	ep := &model.EANPage{EAN: "123", Title: "No price", ProductCount: 0, SeoURL: "/shop/x"}
	blocks := buildProductJSONLDBlocks(s, "https://x.pl", ep, nil, nil, nil, nil)
	// Product block still emitted (without offers), plus BreadcrumbList, plus OnlineStore.
	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks, got %d", len(blocks))
	}
	p := mustParseJSONLDBlock(t, blocks[0])
	if _, has := p["offers"]; has {
		t.Errorf("offers should be omitted when no price")
	}
	if _, has := p["aggregateRating"]; has {
		t.Errorf("aggregateRating should be omitted when no reviews")
	}
}

func TestNormalizeGTIN(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"5907614665029", "5907614665029"},
		{"12345", ""},
		{"590761466502A", ""},
		{"  5907614665029  ", "5907614665029"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeGTIN(c.in); got != c.want {
			t.Errorf("normalizeGTIN(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAggregateRating(t *testing.T) {
	products := []model.Product{
		{AvgRating: 4.0, ReviewCount: 2},
		{AvgRating: 5.0, ReviewCount: 2},
		{AvgRating: 0, ReviewCount: 0},
	}
	rating, count := aggregateRating(products)
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}
	if rating < 4.49 || rating > 4.51 {
		t.Errorf("rating = %v, want ~4.5", rating)
	}

	// No reviews.
	if r, c := aggregateRating([]model.Product{{AvgRating: 0, ReviewCount: 0}}); r != 0 || c != 0 {
		t.Errorf("expected (0,0) for no reviews, got (%v,%d)", r, c)
	}
}

func TestCleanSameAs(t *testing.T) {
	in := []string{"https://a.com", "  ", "https://a.com", "https://b.com", ""}
	out := cleanSameAs(in)
	if len(out) != 2 {
		t.Fatalf("cleanSameAs len = %d, want 2: %v", len(out), out)
	}
	if out[0] != "https://a.com" || out[1] != "https://b.com" {
		t.Errorf("cleanSameAs = %v", out)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty = %q, want empty", got)
	}
}

func TestAbsURL(t *testing.T) {
	base := "https://hdwr.pl"
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"/uploads/seo/logo.png", "https://hdwr.pl/uploads/seo/logo.png"},
		{"https://example.com/logo.svg", "https://example.com/logo.svg"},
		{"http://example.com/logo.svg", "http://example.com/logo.svg"},
		{"relative/logo.png", "https://hdwr.pl/relative/logo.png"},
	}
	for _, c := range cases {
		if got := absURL(base, c.in); got != c.want {
			t.Errorf("absURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestProductBlockURL ensures the product @id and offer url use the canonical
// product URL (baseURL + seoURL).
func TestProductBlockURL(t *testing.T) {
	s := &model.SEOSettings{Enabled: true, OrgName: "ACME"}
	ep := &model.EANPage{EAN: "123", Title: "X", MinPrice: 10, Currency: "PLN", ProductCount: 1, SeoURL: "/shop/cat/prod"}
	blocks := buildProductJSONLDBlocks(s, "https://acme.pl", ep, nil, nil, nil, nil)
	p := mustParseJSONLDBlock(t, blocks[0])
	if p["@id"] != "https://acme.pl/shop/cat/prod" {
		t.Errorf("product @id = %v", p["@id"])
	}
	offers := p["offers"].(map[string]any)
	if offers["url"] != "https://acme.pl/shop/cat/prod" {
		t.Errorf("offer url = %v", offers["url"])
	}
}

// TestWriteHTMLResponseEANListInjectsJSONLD verifies the end-to-end injection:
// the HTML <head> contains the site-level blocks (Organization, WebSite,
// OnlineStore) on every page and the product-level blocks (Product,
// BreadcrumbList, OnlineStore) on EAN pages.
func TestWriteHTMLResponseEANListInjectsJSONLD(t *testing.T) {
	seo := &model.SEOSettings{
		Enabled:           true,
		OrgName:           "HDWR",
		OrgLegalName:      "HDWR Sp. z o.o.",
		OrgPhone:          "+48-456-456-001",
		OrgEmail:          "biuro@hdwr.pl",
		OrgStreet:         "ul. Dmowskiego 28",
		OrgCity:           "Środa Wielkopolska",
		OrgPostalCode:     "63-000",
		OrgCountry:        "PL",
		OrgSameAs:         []string{"https://facebook.com/hdwr"},
		SiteName:          "HDWR - Sprzęt",
		SearchURLTemplate: "/shop?q={search_term_string}",
		StoreName:         "HDWR Store",
		PriceValidDays:    30,
	}
	ep := &model.EANPage{
		EAN:          "5907614665029",
		Title:        "TimeLok 300",
		Description:  "Czytnik czasu pracy",
		Brand:        "HDWR",
		MinPrice:     379,
		Currency:     "PLN",
		ProductCount: 1,
		Images:       []string{"https://hdwr.pl/img1.webp"},
		SeoURL:       "/shop/Rejestratory/TimeLok-300",
	}
	data := db.EANListRespData{
		EANPage:      ep,
		Products:     []model.Product{{SKU: "TimeLok-300NWEPBI", AvgRating: 4.5, ReviewCount: 2}},
		TreePath:     []string{"Rejestratory"},
		TreePathFull: []db.CategoryTreeNode{{Name: "Rejestratory czasu pracy", Slug: "Rejestratory"}},
		SEOURL:       "/shop/Rejestratory/TimeLok-300",
		CatID:        1,
		Total:        1,
		Page:         1,
		Limit:        50,
	}

	// Bot request (full SSR head with JSON-LD).
	req := httptest.NewRequest(http.MethodGet, "/shop/Rejestratory/TimeLok-300", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	writeHTMLResponseEANList(w, req, "TimeLok 300", "https://hdwr.pl", data, seo, nil)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Count JSON-LD blocks.
	n := strings.Count(body, `<script type="application/ld+json">`)
	if n < 5 {
		t.Fatalf("expected at least 5 JSON-LD blocks (Org+WebSite+OnlineStore+Product+Breadcrumb), got %d\nbody head:\n%s", n, firstN(body, 2000))
	}

	for _, want := range []string{
		`"@type":"Organization"`,
		`"@type":"WebSite"`,
		`"SearchAction"`,
		`"@type":"OnlineStore"`,
		`"@type":"Product"`,
		`"@type":"BreadcrumbList"`,
		`"gtin13":"5907614665029"`,
		`"priceCurrency":"PLN"`,
		`"aggregateRating"`,
		`"HDWR Sp. z o.o."`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("HTML missing expected JSON-LD fragment: %s", want)
		}
	}
}

// TestWriteHTMLResponseEANListNoProductPage verifies site-level blocks are
// present on a non-product (catalog) page and no Product block is emitted.
func TestWriteHTMLResponseEANListNoProductPage(t *testing.T) {
	seo := &model.SEOSettings{Enabled: true, OrgName: "ACME"}
	data := db.EANListRespData{} // no EANPage -> catalog/home page

	req := httptest.NewRequest(http.MethodGet, "/shop", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1)")
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	writeHTMLResponseEANList(w, req, "Catalog", "https://acme.pl", data, seo, nil)

	body := w.Body.String()
	// Site-level: Organization + WebSite + OnlineStore = 3 blocks.
	n := strings.Count(body, `<script type="application/ld+json">`)
	if n != 3 {
		t.Fatalf("expected 3 site-level blocks, got %d", n)
	}
	if strings.Contains(body, `"@type":"Product"`) {
		t.Errorf("Product block should not be present on a catalog page")
	}
}

// firstN returns the first n bytes of s (for error messages).
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestProductBlockCompleteFields verifies all the schema.org fields a rich
// results validator expects: description, global identifier (gtin/productID/
// brand), aggregateRating, review, hasMerchantReturnPolicy and
// offers.shippingDetails.
func TestProductBlockCompleteFields(t *testing.T) {
	s := &model.SEOSettings{
		Enabled:             true,
		OrgName:             "HDWR",
		OrgCountry:          "PL",
		ReturnPolicyEnabled: true,
		ReturnPolicyText:    "Возврат в течение 14 дней с момента получения.",
		ReturnPolicyDays:    14,
		ShippingEnabled:     true,
		ShippingCost:        0,
		ShippingMinDays:     1,
		ShippingMaxDays:     3,
		ShippingDestination: "PL",
	}
	ep := &model.EANPage{
		EAN:          "5907614665029",
		Title:        "TimeLok 300",
		Brand:        "HDWR",
		MinPrice:     379,
		Currency:     "PLN",
		ProductCount: 1,
		SeoURL:       "/shop/Rejestratory/TimeLok-300",
	}
	reviews := []model.Review{
		{Rating: 5, Comment: "Great product!", CreatedAt: 1700000000},
		{Rating: 4, Comment: "Good value.", CreatedAt: 1700100000},
	}

	blocks := buildProductJSONLDBlocks(s, "https://hdwr.pl", ep, nil, reviews, nil, nil)
	if len(blocks) == 0 {
		t.Fatal("no blocks")
	}
	p := mustParseJSONLDBlock(t, blocks[0])

	// description (fallback to title since ep.Description and products empty).
	if p["description"] != "TimeLok 300" {
		t.Errorf("description = %v, want title fallback", p["description"])
	}
	// Global identifier.
	if p["gtin"] != "5907614665029" || p["gtin13"] != "5907614665029" {
		t.Errorf("gtin/gtin13 missing: %v / %v", p["gtin"], p["gtin13"])
	}
	pid, _ := p["productID"].(map[string]any)
	if pid == nil || pid["value"] != "5907614665029" {
		t.Errorf("productID missing or wrong: %v", p["productID"])
	}
	if b, _ := p["brand"].(map[string]any); b == nil || b["name"] != "HDWR" {
		t.Errorf("brand missing: %v", p["brand"])
	}
	// aggregateRating from reviews (avg of 5 and 4 = 4.5, count 2).
	ar, _ := p["aggregateRating"].(map[string]any)
	if ar == nil {
		t.Fatalf("aggregateRating missing")
	}
	if rc, _ := ar["reviewCount"].(float64); rc != 2 {
		t.Errorf("aggregateRating reviewCount = %v, want 2", ar["reviewCount"])
	}
	// review array.
	revs, _ := p["review"].([]any)
	if len(revs) != 2 {
		t.Fatalf("review len = %d, want 2", len(revs))
	}
	firstRev := revs[0].(map[string]any)
	if firstRev["@type"] != "Review" {
		t.Errorf("review[0] @type = %v", firstRev["@type"])
	}
	// hasMerchantReturnPolicy.
	rp, _ := p["hasMerchantReturnPolicy"].(map[string]any)
	if rp == nil {
		t.Fatalf("hasMerchantReturnPolicy missing")
	}
	if rp["applicableCountry"] != "PL" {
		t.Errorf("return policy applicableCountry = %v, want PL", rp["applicableCountry"])
	}
	if rpDays, ok := rp["returnPeriod"].(map[string]any); !ok || rpDays["value"] != 14.0 {
		t.Errorf("return policy returnPeriod = %v", rp["returnPeriod"])
	}
	// offers.shippingDetails.
	offers, _ := p["offers"].(map[string]any)
	if offers == nil {
		t.Fatalf("offers missing")
	}
	sd, _ := offers["shippingDetails"].(map[string]any)
	if sd == nil {
		t.Fatalf("offers.shippingDetails missing")
	}
	if rate, ok := sd["shippingRate"].(map[string]any); !ok || rate["currency"] != "PLN" {
		t.Errorf("shippingDetails.shippingRate = %v", sd["shippingRate"])
	}
	if dest, ok := sd["shippingDestination"].(map[string]any); !ok || dest["name"] != "PL" {
		t.Errorf("shippingDetails.shippingDestination = %v", sd["shippingDestination"])
	}
}

// TestProductBlockNoReviewsNoConfig ensures optional blocks are omitted when
// there are no reviews and return/shipping policy is disabled.
func TestProductBlockNoReviewsNoConfig(t *testing.T) {
	s := &model.SEOSettings{Enabled: true, OrgName: "ACME"}
	ep := &model.EANPage{EAN: "123", Title: "X", MinPrice: 10, Currency: "PLN", ProductCount: 1, SeoURL: "/shop/x"}
	blocks := buildProductJSONLDBlocks(s, "https://acme.pl", ep, nil, nil, nil, nil)
	p := mustParseJSONLDBlock(t, blocks[0])
	if _, has := p["aggregateRating"]; has {
		t.Errorf("aggregateRating should be omitted without reviews")
	}
	if _, has := p["review"]; has {
		t.Errorf("review should be omitted without reviews")
	}
	if _, has := p["hasMerchantReturnPolicy"]; has {
		t.Errorf("hasMerchantReturnPolicy should be omitted when disabled")
	}
	offers := p["offers"].(map[string]any)
	if _, has := offers["shippingDetails"]; has {
		t.Errorf("shippingDetails should be omitted when disabled")
	}
	// description should still be present (title fallback).
	if p["description"] != "X" {
		t.Errorf("description fallback = %v, want X", p["description"])
	}
}
