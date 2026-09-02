package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
)

// This file builds schema.org JSON-LD blocks for SEO. Each block is a
// self-contained JSON string (one <script type="application/ld+json"> per
// block, matching how search engines expect multiple structured-data objects).
//
// Site-level blocks (Organization, WebSite, OnlineStore) are emitted on every
// landing page. Product-level blocks (Product, BreadcrumbList, OnlineStore)
// are emitted on EAN/product pages.

// jsonldBlock marshals a value into a JSON-LD block string. Returns "" on
// error (the caller simply omits the block).
func jsonldBlock(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// siteNameOrDefault returns the configured site name or falls back to the name
// derived from the base URL host.
func siteNameOrDefault(s *model.SEOSettings, fallback string) string {
	if n := strings.TrimSpace(s.SiteName); n != "" {
		return n
	}
	return fallback
}

// buildSiteJSONLDBlocks returns the site-level JSON-LD blocks: Organization,
// WebSite (with SearchAction) and OnlineStore. Empty slices are omitted.
func buildSiteJSONLDBlocks(s *model.SEOSettings, baseURL, fallbackSiteName string) []string {
	if s == nil || !s.Enabled {
		return nil
	}
	var blocks []string

	// --- Organization ---
	org := map[string]any{
		"@context": "https://schema.org",
		"@type":    "Organization",
		"name":     firstNonEmpty(s.OrgName, fallbackSiteName),
		"url":      baseURL,
	}
	if s.OrgLegalName != "" {
		org["legalName"] = s.OrgLegalName
	}
	if logo := absURL(baseURL, s.OrgLogo); logo != "" {
		org["logo"] = logo
	}
	if s.OrgPhone != "" || s.OrgEmail != "" {
		cp := map[string]any{"@type": "ContactPoint", "contactType": "customer service"}
		if s.OrgPhone != "" {
			cp["telephone"] = s.OrgPhone
		}
		if s.OrgEmail != "" {
			cp["email"] = s.OrgEmail
		}
		org["contactPoint"] = cp
	}
	if s.OrgStreet != "" || s.OrgCity != "" || s.OrgPostalCode != "" || s.OrgCountry != "" {
		addr := map[string]any{"@type": "PostalAddress"}
		if s.OrgStreet != "" {
			addr["streetAddress"] = s.OrgStreet
		}
		if s.OrgCity != "" {
			addr["addressLocality"] = s.OrgCity
		}
		if s.OrgPostalCode != "" {
			addr["postalCode"] = s.OrgPostalCode
		}
		if s.OrgCountry != "" {
			addr["addressCountry"] = s.OrgCountry
		}
		org["address"] = addr
	}
	if sameAs := cleanSameAs(s.OrgSameAs); len(sameAs) > 0 {
		org["sameAs"] = sameAs
	}
	if b := jsonldBlock(org); b != "" {
		blocks = append(blocks, b)
	}

	// --- WebSite (+ SearchAction) ---
	site := map[string]any{
		"@context": "https://schema.org",
		"@type":    "WebSite",
		"name":     siteNameOrDefault(s, fallbackSiteName),
		"url":      baseURL,
	}
	tmpl := strings.TrimSpace(s.SearchURLTemplate)
	if tmpl == "" {
		tmpl = model.SEODefaultSearch
	}
	if strings.Contains(tmpl, "{search_term_string}") {
		site["potentialAction"] = map[string]any{
			"@type":       "SearchAction",
			"target":      map[string]any{"@type": "EntryPoint", "urlTemplate": baseURL + tmpl},
			"query-input": "required name=search_term_string",
		}
	}
	if b := jsonldBlock(site); b != "" {
		blocks = append(blocks, b)
	}

	// --- OnlineStore ---
	blocks = append(blocks, onlineStoreBlock(s, baseURL, fallbackSiteName)...)

	return blocks
}

// onlineStoreBlock returns the OnlineStore JSON-LD block (a single-element
// slice, or nil when there is nothing meaningful to emit).
func onlineStoreBlock(s *model.SEOSettings, baseURL, fallbackSiteName string) []string {
	if s == nil || !s.Enabled {
		return nil
	}
	name := firstNonEmpty(s.StoreName, s.OrgName, fallbackSiteName)
	logo := absURL(baseURL, firstNonEmpty(s.StoreLogo, s.OrgLogo))
	sameAs := cleanSameAs(s.StoreSameAs)
	if len(sameAs) == 0 {
		sameAs = cleanSameAs(s.OrgSameAs)
	}
	if name == "" {
		return nil
	}
	store := map[string]any{
		"@context": "https://schema.org",
		"@type":    "OnlineStore",
		"name":     name,
		"url":      baseURL,
	}
	if logo != "" {
		store["logo"] = logo
	}
	if len(sameAs) > 0 {
		store["sameAs"] = sameAs
	}
	if b := jsonldBlock(store); b != "" {
		return []string{b}
	}
	return nil
}

// buildProductJSONLDBlocks returns the product-page JSON-LD blocks: Product,
// BreadcrumbList and OnlineStore.
//
// reviews are the approved reviews for the products sharing this EAN; they
// drive the `review` array and (preferably) `aggregateRating`.
func buildProductJSONLDBlocks(s *model.SEOSettings, baseURL string, ep *model.EANPage, products []model.Product, reviews []model.Review, treePath []string, treePathFull []db.CategoryTreeNode) []string {
	if s == nil || !s.Enabled || ep == nil {
		return nil
	}
	var blocks []string

	url := baseURL + ep.SeoURL
	if url == baseURL {
		url = baseURL + "/shop"
	}

	// --- Product ---
	p := map[string]any{
		"@context": "https://schema.org",
		"@type":    "Product",
		"@id":      url,
		"name":     ep.Title,
	}

	// Description: always present (EAN page → first product → title).
	if desc := firstNonEmpty(ep.Description, productDescription(products), ep.Title); desc != "" {
		p["description"] = desc
	}

	// sku / mpn: prefer the first product's EAN, fall back to the EAN.
	sku := ""
	if len(products) > 0 {
		sku = products[0].EAN
	}
	if sku == "" {
		sku = ep.EAN
	}
	if sku != "" {
		p["sku"] = sku
		p["mpn"] = sku
	}

	// Brand (global identifier #1).
	if ep.Brand != "" {
		p["brand"] = map[string]any{"@type": "Brand", "name": ep.Brand}
	}

	// Global identifier #2: EAN as productID + GTIN.
	if ep.EAN != "" {
		p["productID"] = map[string]any{
			"@type": "ProductIdentifier",
			"name":  "EAN",
			"value": ep.EAN,
		}
	}
	if gtin := normalizeGTIN(ep.EAN); gtin != "" {
		p["gtin"] = gtin
		p["gtin13"] = gtin
	}

	if seller := firstNonEmpty(s.OrgName, siteNameOrDefault(s, siteNameFromBaseURL(baseURL))); seller != "" {
		p["manufacturer"] = map[string]any{"@type": "Organization", "name": seller}
	}
	if len(ep.Images) > 0 {
		p["image"] = ep.Images
	}

	// Offers
	if ep.MinPrice > 0 {
		currency := ep.Currency
		if currency == "" {
			currency = firstNonEmpty(s.DefaultCurrency, "RUB")
		}
		avail := "https://schema.org/InStock"
		if ep.ProductCount <= 0 {
			avail = "https://schema.org/OutOfStock"
		}
		offer := map[string]any{
			"@type":         "Offer",
			"url":           url,
			"price":         ep.MinPrice,
			"priceCurrency": currency,
			"availability":  avail,
			"itemCondition": "https://schema.org/NewCondition",
		}
		if days := priceValidDays(s); days > 0 {
			offer["priceValidUntil"] = time.Now().AddDate(0, 0, days).Format("2006-01-02")
		}
		if seller := firstNonEmpty(s.OrgName, siteNameOrDefault(s, siteNameFromBaseURL(baseURL))); seller != "" {
			offer["seller"] = map[string]any{"@type": "Organization", "name": seller}
		}
		// Shipping details (configurable).
		if s.ShippingEnabled {
			sd := map[string]any{
				"@type": "OfferShippingDetails",
				"shippingRate": map[string]any{
					"@type":    "MonetaryAmount",
					"value":    s.ShippingCost,
					"currency": currency,
				},
			}
			if s.ShippingMinDays > 0 || s.ShippingMaxDays > 0 {
				dt := map[string]any{"@type": "QuantitativeValue", "unitCode": "DAY"}
				if s.ShippingMinDays > 0 {
					dt["minValue"] = s.ShippingMinDays
				}
				if s.ShippingMaxDays > 0 {
					dt["maxValue"] = s.ShippingMaxDays
				}
				sd["deliveryTime"] = dt
			}
			if dest := firstNonEmpty(s.ShippingDestination, s.OrgCountry); dest != "" {
				sd["shippingDestination"] = map[string]any{"@type": "Place", "name": dest}
			}
			offer["shippingDetails"] = sd
		}
		p["offers"] = offer
	}

	// Merchant return policy (configurable).
	if s.ReturnPolicyEnabled {
		rp := map[string]any{
			"@type":                "MerchantReturnPolicy",
			"returnPolicyCategory": "https://schema.org/MerchantReturnFiniteReturnWindow",
		}
		if country := firstNonEmpty(s.ReturnPolicyCountry, s.OrgCountry); country != "" {
			rp["applicableCountry"] = country
		}
		if s.ReturnPolicyText != "" {
			rp["merchantReturnPolicy"] = s.ReturnPolicyText
		}
		if s.ReturnPolicyDays > 0 {
			rp["returnPeriod"] = map[string]any{
				"@type":    "QuantitativeValue",
				"value":    s.ReturnPolicyDays,
				"unitCode": "DAY",
			}
		}
		p["hasMerchantReturnPolicy"] = rp
	}

	// Aggregate rating: prefer actual reviews, fall back to precomputed
	// product ratings.
	if len(reviews) > 0 {
		var sum int
		for _, rv := range reviews {
			sum += rv.Rating
		}
		avg := float64(sum) / float64(len(reviews))
		p["aggregateRating"] = map[string]any{
			"@type":       "AggregateRating",
			"ratingValue": fmt.Sprintf("%.1f", avg),
			"reviewCount": len(reviews),
			"bestRating":  5,
			"worstRating": 1,
		}
	} else if rating, count := aggregateRating(products); count > 0 {
		p["aggregateRating"] = map[string]any{
			"@type":       "AggregateRating",
			"ratingValue": fmt.Sprintf("%.1f", rating),
			"reviewCount": count,
			"bestRating":  5,
			"worstRating": 1,
		}
	}

	// Individual reviews (schema.org Review).
	if len(reviews) > 0 {
		revs := make([]any, 0, len(reviews))
		for _, rv := range reviews {
			r := map[string]any{
				"@type":  "Review",
				"author": map[string]any{"@type": "Person", "name": "Customer"},
				"reviewRating": map[string]any{
					"@type":       "Rating",
					"ratingValue": rv.Rating,
					"bestRating":  5,
					"worstRating": 1,
				},
			}
			if rv.Comment != "" {
				r["reviewBody"] = rv.Comment
			}
			if rv.CreatedAt > 0 {
				r["datePublished"] = time.Unix(rv.CreatedAt, 0).UTC().Format("2006-01-02")
			}
			revs = append(revs, r)
		}
		p["review"] = revs
	}

	if b := jsonldBlock(p); b != "" {
		blocks = append(blocks, b)
	}

	// --- BreadcrumbList ---
	if crumbs := buildBreadcrumbList(baseURL, ep.Title, treePath, treePathFull); crumbs != "" {
		blocks = append(blocks, crumbs)
	}

	// NOTE: OnlineStore is a site-level entity and is already emitted by
	// buildSiteJSONLDBlocks on every page (including product pages), so it is
	// NOT repeated here to avoid a duplicate block.

	return blocks
}

// productDescription returns the first non-empty product description.
func productDescription(products []model.Product) string {
	for _, p := range products {
		if strings.TrimSpace(p.Description) != "" {
			return p.Description
		}
	}
	return ""
}

// buildBreadcrumbList returns the BreadcrumbList JSON-LD block for a product
// page: Home -> each category in the path -> the product.
func buildBreadcrumbList(baseURL, productName string, treePath []string, treePathFull []db.CategoryTreeNode) string {
	items := []any{
		map[string]any{
			"@type":    "ListItem",
			"position": 1,
			"item":     map[string]any{"@id": baseURL, "name": "Home"},
		},
	}
	pos := 2
	for i, slug := range treePath {
		// Category display name: prefer the localized/primary name from the
		// full tree node when available, else the slug.
		name := ""
		if i < len(treePathFull) {
			name = firstNonEmpty(treePathFull[i].Name, treePathFull[i].NameEn, treePathFull[i].NamePl)
		}
		if name == "" {
			name = slug
		}
		catURL := baseURL + "/shop/" + strings.Join(treePath[:i+1], "/")
		items = append(items, map[string]any{
			"@type":    "ListItem",
			"position": pos,
			"item":     map[string]any{"@id": catURL, "name": name},
		})
		pos++
	}
	// Product (last crumb, no @id — it is the current page).
	items = append(items, map[string]any{
		"@type":    "ListItem",
		"position": pos,
		"item":     map[string]any{"name": productName},
	})

	bc := map[string]any{
		"@context":        "https://schema.org",
		"@type":           "BreadcrumbList",
		"name":            "Breadcrumbs",
		"itemListElement": items,
	}
	return jsonldBlock(bc)
}

// aggregateRating computes a weighted average rating and total review count
// across the products sharing an EAN. Returns (0,0) when there are no reviews.
func aggregateRating(products []model.Product) (float64, int) {
	var weighted float64
	var total int
	for _, p := range products {
		if p.ReviewCount <= 0 || p.AvgRating <= 0 {
			continue
		}
		weighted += p.AvgRating * float64(p.ReviewCount)
		total += p.ReviewCount
	}
	if total == 0 {
		return 0, 0
	}
	return weighted / float64(total), total
}

// normalizeGTIN returns the EAN as a 13-digit GTIN when it is a valid numeric
// 13-char code, else "" (so the field is omitted for non-GTIN identifiers).
func normalizeGTIN(ean string) string {
	e := strings.TrimSpace(ean)
	if len(e) != 13 {
		return ""
	}
	for _, c := range e {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return e
}

// priceValidDays returns the configured priceValidUntil horizon (days),
// defaulting to SEODefaultValid when unset.
func priceValidDays(s *model.SEOSettings) int {
	if s == nil {
		return model.SEODefaultValid
	}
	if s.PriceValidDays > 0 {
		return s.PriceValidDays
	}
	return model.SEODefaultValid
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// absURL converts a possibly-relative URL (e.g. an uploaded asset
// "/uploads/seo/logo.png") into an absolute URL using baseURL. Absolute
// http(s) URLs are returned unchanged; empty strings stay empty. schema.org
// expects absolute URLs for logo/image fields.
func absURL(baseURL, u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if strings.HasPrefix(u, "/") {
		return strings.TrimSuffix(baseURL, "/") + u
	}
	return baseURL + "/" + u
}

// cleanSameAs trims and de-duplicates a list of URLs, dropping empties.
func cleanSameAs(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}
