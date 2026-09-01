package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

// Tradedoubler API 1.0 paginated product import.
//
// Tradedoubler price lists are served from a JSON API that is paginated:
//
//	https://api.tradedoubler.com/1.0/products.json;page=1;pageSize=100;fid=123?token=...
//
// Parameters are joined with ";" (matrix parameters) in the canonical form,
// but the API accepts "&" as well. The response for a page is:
//
//	{
//	  "products": [ {id, name, description, brand, ean, price, priceCurrency,
//	                    productUrl, images[], categories[], ...} ],
//	  "meta": { "page": 1, "pageSize": 100, "totalItems": N, "totalPages": M }
//	}
//
// `price` is expressed in the smallest currency unit (e.g. grosze for PLN,
// cents for USD), so 9900 == 99.00.
//
// The importer walks every page (starting from the page encoded in the URL,
// default 1) until `meta.totalPages` is exhausted, capping the page size at
// tradedoublerMaxPageSize (1000, the API maximum) and the total number of
// products at the import limit.

const (
	tradedoublerMaxPageSize = 1000 // API maximum products per request
	tradedoublerDefaultPage = 1
)

// tradedoublerMeta mirrors the pagination metadata of a Tradedoubler response.
type tradedoublerMeta struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

// tradedoublerPage is the top-level shape of one API response.
type tradedoublerPage struct {
	Products []tradedoublerProduct `json:"products"`
	Meta     tradedoublerMeta      `json:"meta"`
}

// tradedoublerProduct is a single product as returned by the API. Field names
// use the documented Tradedoubler names; the parser also tolerates common
// aliases so a slightly different feed still imports.
type tradedoublerProduct struct {
	ID               int64                  `json:"id"`
	Name             string                 `json:"name"`
	Title            string                 `json:"title"` // alias
	Description      string                 `json:"description"`
	ShortDescription string                 `json:"shortDescription"`
	Brand            string                 `json:"brand"`
	BrandName        string                 `json:"brandName"` // alias
	EAN              string                 `json:"ean"`
	GTIN             string                 `json:"gtin"` // alias
	Barcode          string                 `json:"barcode"`
	Price            json.Number            `json:"price"`
	PriceCurrency    string                 `json:"priceCurrency"`
	Currency         string                 `json:"currency"` // alias
	ProductURL       string                 `json:"productUrl"`
	URL              string                 `json:"url"` // alias
	Link             string                 `json:"link"`
	Stock            int                    `json:"stock"`
	Images           []tradedoublerImage    `json:"images"`
	Categories       []tradedoublerCategory `json:"categories"`
}

// tradedoublerImage is an image entry (object form) or a bare URL (string form).
type tradedoublerImage struct {
	URL string `json:"url"`
}

// tradedoublerCategory is a category entry.
type tradedoublerCategory struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// isTradedoublerURL reports whether the import URL points at a Tradedoubler
// products API (the paginated JSON endpoint we walk page by page). It matches
// the canonical tradedoubler.com host, and also any paginated products API
// (a URL carrying a "page" parameter) so self-hosted / proxied feeds import
// the same way.
func isTradedoublerURL(rawURL string) bool {
	u, err := url.Parse(normalizeTradedoublerURL(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasSuffix(host, "tradedoubler.com") && strings.Contains(u.Path, "products") {
		return true
	}
	// A "page" parameter signals a paginated feed; walk it page by page.
	return u.Query().Has("page")
}

// normalizeTradedoublerURL converts a Tradedoubler URL that uses ";" matrix
// parameters (optionally followed by a "?" query string) into a standard URL
// with all parameters in the query string, so url.Parse/url.Query understand
// them.
//
//	.../products.json;page=1;pageSize=100;fid=123?token=ABC
//	  => .../products.json?page=1&pageSize=100&fid=123&token=ABC
//
// URLs without matrix parameters are returned unchanged.
func normalizeTradedoublerURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	// Split off the query string (everything after the first "?").
	beforeQ, afterQ, hasQ := strings.Cut(rawURL, "?")
	// Split the path from the matrix parameters (everything after the first ";").
	purePath, matrixParams, hasMatrix := strings.Cut(beforeQ, ";")
	if !hasMatrix {
		return rawURL
	}
	params := strings.ReplaceAll(matrixParams, ";", "&")
	var b strings.Builder
	b.WriteString(purePath)
	b.WriteByte('?')
	b.WriteString(params)
	if hasQ && afterQ != "" {
		b.WriteByte('&')
		b.WriteString(afterQ)
	}
	return b.String()
}

// tradedoublerPageParams parses a Tradedoubler URL (which may use ";" or "&"
// as the parameter separator) into its query values. It returns the page
// number (default 1) and page size (default 1000, capped at 1000) plus the
// canonical query without the page parameter (so the caller can re-append
// page=N for each request).
func tradedoublerPageParams(rawURL string) (page, pageSize int, baseQuery url.Values, err error) {
	normalized := normalizeTradedoublerURL(rawURL)
	u, perr := url.Parse(normalized)
	if perr != nil {
		return 0, 0, nil, fmt.Errorf("parse url: %w", perr)
	}
	q := u.Query()
	page = tradedoublerDefaultPage
	if v := q.Get("page"); v != "" {
		if n, aerr := strconv.Atoi(v); aerr == nil && n > 0 {
			page = n
		}
	}
	// Default to the API maximum (1000) when the URL does not specify a page
	// size, so we fetch as much as allowed per request. An explicit pageSize is
	// respected (and capped at the API maximum).
	pageSize = tradedoublerMaxPageSize
	if v := firstNonEmptyQuery(q, "pageSize", "page_size", "limit"); v != "" {
		if n, aerr := strconv.Atoi(v); aerr == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > tradedoublerMaxPageSize {
		pageSize = tradedoublerMaxPageSize
	}
	// Build the base query without the page parameter (we set it per request).
	base := url.Values{}
	for k, vs := range q {
		if k == "page" {
			continue
		}
		for _, v := range vs {
			base.Add(k, v)
		}
	}
	return page, pageSize, base, nil
}

// firstNonEmptyQuery returns the first non-empty value among the given keys.
func firstNonEmptyQuery(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
}

// tradedoublerPageURL builds the URL for a specific page from the original
// URL's scheme/host/path and the base query (without page), appending page=N.
func tradedoublerPageURL(rawURL string, baseQuery url.Values, page int) (string, error) {
	normalized := normalizeTradedoublerURL(rawURL)
	u, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	q := baseQuery
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// downloadTradedoubler fetches every page of the Tradedoubler feed starting at
// the page encoded in the URL (default 1) and returns all products. The page
// size is taken from the URL (default 1000, the API maximum). It stops when a
// page returns fewer products than the page size, when meta.totalPages is
// reached, or when limit products have been collected (limit<=0 = unlimited).
func downloadTradedoubler(client *http.Client, rawURL string, limit int) ([]tradedoublerProduct, error) {
	page, pageSize, baseQuery, err := tradedoublerPageParams(rawURL)
	if err != nil {
		return nil, err
	}

	var all []tradedoublerProduct
	current := page
	for {
		reqURL, uerr := tradedoublerPageURL(rawURL, baseQuery, current)
		if uerr != nil {
			return all, uerr
		}
		fmt.Printf("[IMPORT-TRADEDoubler] Fetching page %d (%d products so far)\n", current, len(all))
		var pageData tradedoublerPage
		if ferr := fetchJSON(client, reqURL, &pageData); ferr != nil {
			return all, fmt.Errorf("page %d: %w", current, ferr)
		}

		got := len(pageData.Products)
		if got == 0 {
			fmt.Printf("[IMPORT-TRADEDoubler] Page %d empty, stopping\n", current)
			break
		}
		all = append(all, pageData.Products...)
		fmt.Printf("[IMPORT-TRADEDoubler] Page %d: %d products (total %d)\n", current, got, len(all))

		if limit > 0 && len(all) >= limit {
			all = all[:limit]
			break
		}

		// Decide whether there is a next page.
		next := false
		if pageData.Meta.TotalPages > 0 {
			next = current < pageData.Meta.TotalPages
		} else if pageData.Meta.TotalItems > 0 {
			next = len(all) < pageData.Meta.TotalItems
		} else {
			// No metadata: keep going while the page was full.
			next = got >= pageSize
		}
		if !next {
			break
		}
		current++
	}
	return all, nil
}

// fetchJSON performs a GET and decodes the JSON body into dst.
func fetchJSON(client *http.Client, reqURL string, dst interface{}) error {
	resp, err := client.Get(reqURL)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("unexpected status: %s (body: %s)", resp.Status, string(body))
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// parseTradedoublerProducts converts Tradedoubler API products into
// model.Product values, ready for the shared import phases. It returns the
// products and the names (parallel slices) plus the count of skipped items.
func parseTradedoublerProducts(tps []tradedoublerProduct, companyID int64, companyName, currency string,
	attrDefCache map[string]*model.AttrDef, newAttrKeys map[string]struct{}, limit int) (products []*model.Product, names []string, skipped int) {

	for _, tp := range tps {
		if limit > 0 && len(products) >= limit {
			break
		}
		p, skip := convertTradedoublerProduct(tp, companyID, companyName, currency, attrDefCache, newAttrKeys)
		if skip {
			skipped++
			continue
		}
		if p == nil {
			skipped++
			continue
		}
		products = append(products, p)
		names = append(names, p.Name)
	}
	return products, names, skipped
}

// convertTradedoublerProduct maps a single Tradedoubler product to a
// model.Product. The second return value is true when the item should be
// skipped (no name, no usable price, etc.).
func convertTradedoublerProduct(tp tradedoublerProduct, companyID int64, companyName, currency string,
	attrDefCache map[string]*model.AttrDef, newAttrKeys map[string]struct{}) (*model.Product, bool) {

	name := strings.TrimSpace(firstNonEmptyStr(tp.Name, tp.Title))
	if name == "" {
		return nil, true
	}

	// Price: Tradedoubler reports it in the smallest currency unit (integer).
	// A value with a fractional part is already a decimal price.
	price, ok := tradedoublerPrice(tp.Price)
	if !ok || price <= 0 {
		return nil, true
	}
	cur := strings.TrimSpace(firstNonEmptyStr(tp.PriceCurrency, tp.Currency, currency))
	if cur == "" {
		cur = "PLN"
	}

	// EAN: prefer an explicit EAN/GTIN/barcode; fall back to the product id.
	ean := strings.TrimSpace(firstNonEmptyStr(tp.EAN, tp.GTIN, tp.Barcode))
	if ean == "" && tp.ID > 0 {
		ean = strconv.FormatInt(tp.ID, 10)
	}

	// SKU: use the EAN (the stable identifier) so re-imports match in place.
	sku := ean

	// Name with company suffix (matches the other importers' convention).
	fullName := name
	if companyName != "" {
		fullName = name + " — " + companyName
	}

	// Description: prefer the long description, fall back to the short one.
	description := strings.TrimSpace(firstNonEmptyStr(tp.Description, tp.ShortDescription))

	// Images: first usable image URL.
	var images []string
	if len(tp.Images) > 0 {
		if u := strings.TrimSpace(tp.Images[0].URL); u != "" {
			images = []string{u}
		}
	}

	// Brand is a first-class attribute (created in Phase 0 via newAttrKeys).
	brand := strings.TrimSpace(firstNonEmptyStr(tp.Brand, tp.BrandName))
	var attrs []model.KeyValue
	if brand != "" {
		attrs = append(attrs, model.KeyValue{Key: "brand", Value: brand})
		newAttrKeys["brand"] = struct{}{}
	}
	// Category names as attributes for catalog filtering.
	if len(tp.Categories) > 0 {
		var catNames []string
		for _, c := range tp.Categories {
			if n := strings.TrimSpace(c.Name); n != "" {
				catNames = append(catNames, n)
			}
		}
		if len(catNames) > 0 {
			attrs = append(attrs, model.KeyValue{Key: "category", Value: strings.Join(catNames, " > ")})
			newAttrKeys["category"] = struct{}{}
		}
	}

	productURL := strings.TrimSpace(firstNonEmptyStr(tp.ProductURL, tp.URL, tp.Link))

	p := &model.Product{
		SKU:         sku,
		EAN:         ean,
		Name:        fullName,
		Description: description,
		CompanyID:   companyID,
		Brand:       brand,
		Price:       price,
		Currency:    cur,
		StockQty:    int64(tp.Stock),
		Status:      model.ProductStatusActive,
		ProductURL:  productURL,
		Images:      images,
		Attributes:  attrs,
		SEO: model.ProductSEO{
			Title: fmt.Sprintf("%s — MakoShop", fullName),
		},
	}
	return p, false
}

// tradedoublerPrice interprets a Tradedoubler price. The API reports prices in
// the smallest currency unit as integers (9900 == 99.00). A value that already
// has a fractional part is treated as a decimal price.
func tradedoublerPrice(raw json.Number) (float64, bool) {
	s := strings.TrimSpace(raw.String())
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if v <= 0 {
		return 0, false
	}
	// Integer value => smallest currency unit; convert to major unit.
	if v == float64(int64(v)) {
		v = v / 100.0
	}
	return v, true
}

// firstNonEmptyStr returns the first non-empty (after trim) string.
func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// tradedoublerClient builds the HTTP client used for the API (generous overall
// timeout for large feeds, fast failure if the server never responds).
func tradedoublerClient() *http.Client {
	return &http.Client{
		Timeout:   15 * time.Minute,
		Transport: &http.Transport{ResponseHeaderTimeout: 60 * time.Second},
	}
}
