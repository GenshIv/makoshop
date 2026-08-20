package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/i18n"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
)

// --- Landing Page handlers ---

// HandleLandingPagesList returns all landing pages (admin).
// GET /admin/landings
func (h *Handlers) HandleLandingPagesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	lps, err := h.landingRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if lps == nil {
		lps = []model.LandingPage{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": lps,
	})
}

// HandleLandingPageGet returns a landing page by ID (admin).
// GET /admin/landings/{id}
func (h *Handlers) HandleLandingPageGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "landing_id")
	if !ok {
		return
	}

	lp, err := h.landingRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	writeJSON(w, http.StatusOK, lp)
}

// HandleLandingPageCreate creates a new landing page (admin).
// POST /admin/landings
func (h *Handlers) HandleLandingPageCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateLandingRequest
	if !readJSON(w, r, &req) {
		return
	}

	if req.SCU == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "scu is required")
		return
	}

	lp := &model.LandingPage{
		SCU:         req.SCU,
		Slug:        req.Slug,
		Title:       req.Title,
		Description: req.Description,
		Content:     req.Content,
		Images:      req.Images,
		IsActive:    req.IsActive,
		ProductIDs:  req.ProductIDs,
	}

	if err := h.landingRepo.Create(lp); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, lp)
}

// HandleLandingPageUpdate updates a landing page (admin).
// PUT /admin/landings/{id}
func (h *Handlers) HandleLandingPageUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "landing_id")
	if !ok {
		return
	}

	var req UpdateLandingRequest
	if !readJSON(w, r, &req) {
		return
	}

	if err := h.landingRepo.Update(id, func(lp *model.LandingPage) {
		if req.SCU != nil {
			lp.SCU = *req.SCU
		}
		if req.Slug != nil {
			lp.Slug = *req.Slug
		}
		if req.Title != nil {
			lp.Title = *req.Title
		}
		if req.Description != nil {
			lp.Description = *req.Description
		}
		if req.Content != nil {
			lp.Content = *req.Content
		}
		if req.Images != nil {
			lp.Images = *req.Images
		}
		if req.IsActive != nil {
			lp.IsActive = *req.IsActive
		}
		if req.ProductIDs != nil {
			lp.ProductIDs = *req.ProductIDs
		}
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	lp, _ := h.landingRepo.Get(id)
	writeJSON(w, http.StatusOK, lp)
}

// HandleLandingPageDelete deletes a landing page (admin).
// DELETE /admin/landings/{id}
func (h *Handlers) HandleLandingPageDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "landing_id")
	if !ok {
		return
	}

	if err := h.landingRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleLandingPageBySlug returns a landing page by slug (public).
// GET /landing/{slug}
func (h *Handlers) HandleLandingPageBySlug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract slug from path: /landing/{slug}
	path := r.URL.Path
	prefix := "/landing/"
	if !strings.HasPrefix(path, prefix) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	slug := strings.TrimPrefix(path, prefix)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing slug")
		return
	}

	lp, err := h.landingRepo.GetBySlug(slug)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	// Get products with this SCU
	products, _ := h.turboSearch.GetProductsBySCU(lp.SCU)
	if products == nil {
		products = []model.Product{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"landing":  lp,
		"products": products,
	})
}

// HandleLandingPageBySCU returns a landing page by SCU (public).
// GET /landing/scu/{scu}
func (h *Handlers) HandleLandingPageBySCU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract SCU from path: /landing/scu/{scu}
	path := r.URL.Path
	prefix := "/landing/scu/"
	if !strings.HasPrefix(path, prefix) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	scu := strings.TrimPrefix(path, prefix)
	if scu == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing scu")
		return
	}

	lp, err := h.landingRepo.GetBySCU(scu)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	// Get products with this SCU
	products, _ := h.turboSearch.GetProductsBySCU(lp.SCU)
	if products == nil {
		products = []model.Product{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"landing":  lp,
		"products": products,
	})
}

// HandleLandingPageProducts returns products for a landing page (public).
// GET /landing/{slug}/products
func (h *Handlers) HandleLandingPageProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract slug from path: /landing/{slug}/products
	path := r.URL.Path
	prefix := "/landing/"
	if !strings.HasPrefix(path, prefix) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "products" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	slug := parts[0]
	if slug == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing slug")
		return
	}

	lp, err := h.landingRepo.GetBySlug(slug)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	// Get products with this SCU
	products, _ := h.turboSearch.GetProductsBySCU(lp.SCU)
	if products == nil {
		products = []model.Product{}
	}

	q := r.URL.Query().Get("q")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")
	sortStr := r.URL.Query().Get("sort")

	page := 1
	if pageStr != "" {
		p, _ := strconv.Atoi(pageStr)
		if p < 1 {
			p = 1
		}
		page = p
	}

	limit := 50
	if limitStr != "" {
		l, _ := strconv.Atoi(limitStr)
		if l < 1 {
			l = 1
		}
		if l > 200 {
			l = 200
		}
		limit = l
	}

	// Apply filters to products
	var filtered []model.Product
	for _, p := range products {
		if q != "" {
			qLower := strings.ToLower(q)
			if !strings.Contains(strings.ToLower(p.Name), qLower) &&
				!strings.Contains(strings.ToLower(p.Description), qLower) {
				continue
			}
		}
		filtered = append(filtered, p)
	}

	// Sort
	switch sortStr {
	case "price_asc":
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Price < filtered[j].Price
		})
	case "price_desc":
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Price > filtered[j].Price
		})
	}

	total := int64(len(filtered))

	// Pagination
	start := (page - 1) * limit
	end := start + limit
	if start > len(filtered) {
		filtered = []model.Product{}
	} else if end > len(filtered) {
		filtered = filtered[start:]
	} else {
		filtered = filtered[start:end]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": filtered,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// --- SCUPage handlers (SEO pages) ---

// HandleSCUPageByPath handles SEO page requests.
// GET /shop — all SCU pages (root catalog)
// GET /shop/{category_tree} — SCU pages in category (category catalog)
// GET /shop/{category_tree}/{slug} — single SCU page
// Example: /shop/elektronika/telefony/samsung-galaxy-s7
// Priority: category first, then SCU page, then redirect to referer/home.
func (h *Handlers) HandleSCUPageByPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract path after /shop/
	path := r.URL.Path
	prefix := "/shop/"
	rest := ""
	if strings.HasPrefix(path, prefix) {
		rest = strings.TrimPrefix(path, prefix)
	} else if path == "/shop" {
		rest = ""
	}

	// Split and filter empty parts in one pass to reduce allocs.
	cleanParts := splitNonEmpty(rest, '/')

	if len(cleanParts) == 0 {
		// /shop — root catalog (all SCU pages)
		h.handleSCUPageCatalog(w, r, 0)
		return
	}

	// Step 1: Try SCU page by last part as slug
	// If it's a single part, it might be an SCU page slug (e.g. /shop/samsung-galaxy-s7)
	// or a category slug (e.g. /shop/elektronika).
	// We check SCU page first if it's a single part to allow redirects.
	if len(cleanParts) == 1 {
		slug := cleanParts[0]
		sp, err := h.scuPageRepo.GetBySlug(slug)
		if err == nil {
			canonical := "/shop/" + sp.Slug
			if sp.CategoryID != 0 {
				treePath, tpErr := h.categoryRepo.GetTreePath(sp.CategoryID)
				if tpErr == nil && len(treePath) > 0 {
					canonical = "/shop/" + strings.Join(treePath, "/") + "/" + sp.Slug
				}
			}
			// currentPath := "/shop/" + strings.Join(cleanParts, "/")
			if path != canonical {
				http.Redirect(w, r, canonical, http.StatusMovedPermanently)
				return
			}
			h.writeSCUPageResponse(w, r, sp)
			return
		}
	}

	// Step 2: Try to find category by full path
	catID, err := h.findCategoryByPath(cleanParts)
	if err == nil {
		h.handleSCUPageCatalog(w, r, catID)
		return
	}

	// Step 3: Try SCU page by last part as slug (for deep paths that didn't match category)
	slug := cleanParts[len(cleanParts)-1]
	sp, err := h.scuPageRepo.GetBySlug(slug)
	if err == nil {
		// Build canonical URL for this SCUPage
		canonical := "/shop/" + sp.Slug
		if sp.CategoryID != 0 {
			treePath, tpErr := h.categoryRepo.GetTreePath(sp.CategoryID)
			if tpErr == nil && len(treePath) > 0 {
				canonical = "/shop/" + strings.Join(treePath, "/") + "/" + sp.Slug
			}
		}
		// If current path differs from canonical, redirect 301
		currentPath := "/shop/" + strings.Join(cleanParts, "/")
		if currentPath != canonical {
			http.Redirect(w, r, canonical, http.StatusMovedPermanently)
			return
		}
		// Exact match — render page
		h.writeSCUPageResponse(w, r, sp)
		return
	}

	// Step 3: Not found — return 404
	writeError(w, http.StatusNotFound, "NOT_FOUND", "page not found")
}

var scuListRespRegistry = silentjson.BuildRegistry(reflect.TypeOf(db.SCUListRespData{}))

// handleSCUPageCatalog returns a paginated list of SCU pages for a category.
func (h *Handlers) handleSCUPageCatalog(w http.ResponseWriter, r *http.Request, catID int64) {
	ctx := r.Context()

	q := r.URL.Query().Get("q")
	sort := r.URL.Query().Get("sort")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")
	priceMinStr := r.URL.Query().Get("price_min")
	priceMaxStr := r.URL.Query().Get("price_max")

	var priceMin, priceMax float64
	if priceMinStr != "" {
		priceMin, _ = strconv.ParseFloat(priceMinStr, 64)
	}
	if priceMaxStr != "" {
		priceMax, _ = strconv.ParseFloat(priceMaxStr, 64)
	}

	page := 1
	if pageStr != "" {
		p, _ := strconv.Atoi(pageStr)
		if p < 1 {
			p = 1
		}
		page = p
	}

	limit := 50
	if limitStr != "" {
		l, _ := strconv.Atoi(limitStr)
		if l < 1 {
			l = 1
		}
		if l > 200 {
			l = 200
		}
		limit = l
	}

	// Parse attribute filters: attr.{code}=value1,value2 or attr.{code}[]=value1,value2
	attrFilters := make(map[string][]string)
	query := r.URL.Query()
	for key, values := range query {
		if len(key) > 5 && key[:5] == "attr." {
			code := key[5:]
			// Remove trailing [] if present (axios array encoding)
			if len(code) >= 2 && code[len(code)-2:] == "[]" {
				code = code[:len(code)-2]
			}
			if code != "" {
				attrFilters[code] = values
			}
		}
	}

	// Build category filter attributes (cached)
	categoryAttrs := h.GetCategoryAttrs(catID)

	// Use SCUPageSearch for catalog listing
	if h.scuPageSearch != nil {
		params := db.SCUPageListParams{
			Q:           q,
			CategoryID:  catID,
			AttrFilters: attrFilters,
			PriceMin:    priceMin,
			PriceMax:    priceMax,
			Sort:        sort,
			Page:        page,
			Limit:       limit,
		}
		result, err := h.scuPageSearch.ListWithTurbo(params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		// Check if client disconnected after search
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Add seo_url to each item via text replacement (no unmarshal)
		// Pre-cache tree paths for categories that appear in results.
		//items := make([]silentjson.RawMessage, 0, len(result.Items))
		//treePathCache := make(map[int64][]string, 16)
		//for _, raw := range result.Items {
		//	items = append(items, injectSeoURLCached(raw, h.categoryRepo, treePathCache))
		//}

		respData := db.SCUListRespData{
			Items:         result.Items,
			Total:         result.Total,
			Page:          result.Page,
			Limit:         result.Limit,
			CategoryAttrs: categoryAttrs,
		}

		// Add category info for breadcrumbs and UI
		if catID > 0 {
			respData.CatID = catID
			if treePath, err := h.categoryRepo.GetTreePath(catID); err == nil && len(treePath) > 0 {
				respData.TreePath = treePath
			}
			// Include full category object (with descriptions and images)
			if cat, err := h.categoryRepo.Get(catID); err == nil {
				respData.Category = *cat
				// Add subcategories as precomputed JSON (no struct->json)
				if subsJSON, err := h.categoryRepo.GetTreeByParentJSON(catID); err == nil && len(subsJSON) > 0 {
					respData.Subcategories = silentjson.RawMessage(subsJSON)
				}
			}
		}

		if wantsHTML(r) {
			writeHTMLResponseSCUList(w, r, i18n.T("ui.catalog_title"), respData)
			return
		}
		writeJSONSCUList(w, r, http.StatusOK, respData)
		return
	}

	// Fallback: use productRepo.ListWithFacets
	productParams := db.ListParams{
		Q:          q,
		CategoryID: catID,
		Sort:       sort,
		Page:       page,
		Limit:      limit,
	}
	result, err := h.productRepo.ListWithFacets(productParams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSONSCUList(w, r, http.StatusOK, *result)
}

// writeSCUPageResponse writes the SCU page with its products.
func (h *Handlers) writeSCUPageResponse(w http.ResponseWriter, r *http.Request, sp *model.SCUPage) {
	ctx := r.Context()

	// Get products with this SCU via turbo index "scu:{scu}"
	products, err := h.turboSearch.GetProductsBySCU(sp.SCU)
	if err != nil || products == nil {
		products = []model.Product{}
	}

	// Check if client disconnected after product lookup
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Build category tree path for SEO and breadcrumbs
	var treePath []string
	var treePathFull []db.CategoryTreeNode
	if sp.CategoryID != 0 {
		treePath, _ = h.categoryRepo.GetTreePath(sp.CategoryID)
		treePathFull, _ = h.categoryRepo.GetTreePathFull(sp.CategoryID)
	}

	seoURL := buildSEOURL(sp, treePath)

	// writeJSONSCUList
	respData := db.SCUListRespData{
		SCUPage:      sp,
		Products:     products,
		TreePath:     treePath,
		TreePathFull: treePathFull,
		SEOURL:       seoURL,
		CatID:        sp.CategoryID,
	}

	if sp.CategoryID != 0 {
		if cat, err := h.categoryRepo.Get(sp.CategoryID); err == nil {
			respData.Category = *cat
			// Add subcategories as precomputed JSON (no struct->json)
			if subsJSON, err := h.categoryRepo.GetTreeByParentJSON(sp.CategoryID); err == nil && len(subsJSON) > 0 {
				respData.Subcategories = silentjson.RawMessage(subsJSON)
			}
		}
	}

	title := sp.Title + " — MakoShop"
	if wantsHTML(r) {
		writeHTMLResponseSCUList(w, r, title, respData)
		return
	}
	writeJSONSCUList(w, r, http.StatusOK, respData)
}

// isBot checks if the request is from a search engine bot
func isBot(r *http.Request) bool {
	ua := strings.ToLower(r.UserAgent())
	bots := []string{
		"googlebot",
		"yandexbot",
		"bingbot",
		"duckduckbot",
		"baiduspider",
		"facebookexternalhit",
		"twitterbot",
		"slackbot",
		"whatsapp",
		"telegrambot",
	}
	for _, b := range bots {
		if strings.Contains(ua, b) {
			return true
		}
	}
	return false
}

// wantsHTML checks if client wants HTML (browser) vs JSON (API/bots)
func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return true // default to HTML for browsers
	}
	// If explicitly wants JSON, return false
	if strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html") {
		return false
	}
	return true
}

// writeHTMLResponse writes an HTML page with embedded data for SSR
// For bots: full SSR with inline content. For browsers: minimal HTML + JS.
func writeHTMLResponseSCUList(w http.ResponseWriter, r *http.Request, title string, data db.SCUListRespData) {
	ctx := r.Context()
	headOnly := r.Method == http.MethodHead

	// For bots: need jsonData for SSR content. For browsers: write directly to avoid copy.
	var jsonData []byte
	if isBot(r) {
		jsonData = marshalWithPool(&data, scuListRespRegistry)
		// Check if client disconnected after marshaling
		select {
		case <-ctx.Done():
			return
		default:
		}
	}

	// Base URL
	baseURL := "https://makoshop.com"

	// Extract SEO fields
	seoURL := ""
	desc := "MakoShop — маркетплейс товаров по лучшим ценам от проверенных поставщиков."
	image := ""

	canonicalTag := ""
	if seoURL != "" {
		canonicalTag = `  <link rel="canonical" href="` + baseURL + seoURL + `">
`
	}

	ogTags := ""
	if image != "" {
		ogTags = `  <meta property="og:image" content="` + html.EscapeString(image) + `">
`
	}

	if isBot(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Full SSR for bots: inline content + SEO tags
		bodyContent := renderSSRContent(jsonData)

		// Check if client disconnected after SSR rendering
		select {
		case <-ctx.Done():
			return
		default:
		}

		w.WriteHeader(http.StatusOK)
		if headOnly {
			return
		}

		ogURL := baseURL + seoURL
		if ogURL == baseURL {
			ogURL = baseURL + "/shop"
		}
		w.Write(htmlBotHead)
		writeEscapedString(w, title)
		w.Write(htmlBotTitleEnd)
		writeEscapedString(w, desc)
		w.Write(htmlBotDescEnd)
		if canonicalTag != "" {
			w.Write(stringToBytes(canonicalTag))
		}
		if ogTags != "" {
			w.Write(stringToBytes(ogTags))
		}
		w.Write(htmlBotOGStart)
		writeEscapedString(w, title)
		w.Write(htmlBotOGTitleEnd)
		writeEscapedString(w, desc)
		w.Write(htmlBotOGDescEnd)
		w.Write(htmlBotOGType)
		w.Write(stringToBytes(ogURL))
		w.Write(htmlBotOGURLEnd)
		w.Write(htmlBotBodyStart)
		if bodyContent != "" {
			w.Write(stringToBytes(bodyContent))
		}
		w.Write(htmlBotScriptStart)
		writeSafeJSON(w, jsonData)
		w.Write(htmlBotScriptEnd)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if headOnly {
		return
	}
	// Minimal HTML for browsers (SPA) — write JSON directly to avoid copy
	w.Write(htmlHead)
	writeEscapedString(w, title)
	w.Write(htmlTitleEnd)
	if canonicalTag != "" {
		w.Write(stringToBytes(canonicalTag))
	}
	w.Write(htmlBodyStart)
	writeSafeJSONWithPool(w, &data, scuListRespRegistry)
	w.Write(htmlScriptEnd)
}

// writeSafeJSONWithPool marshals data and writes escaped JSON directly to w.
// Optimized for hot paths: no extra copy, escapes '<' inline.
func writeSafeJSONWithPool(w http.ResponseWriter, data *db.SCUListRespData, reg *silentjson.Registry) {
	bufPtr := jsonBufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]
	buf = silentjson.Marshal(data, reg, buf)

	// Note: we can't easily set Content-Length here because writeSafeJSON
	// might increase the length by escaping '<'.
	writeSafeJSON(w, buf)
	*bufPtr = buf[:64*1024]
	jsonBufPool.Put(bufPtr)
}

var (
	htmlBotHead = []byte(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>`)
	htmlBotTitleEnd = []byte(`</title>
  <meta name="description" content="`)
	htmlBotDescEnd = []byte(`">
`)
	htmlBotOGStart    = []byte(`  <meta property="og:title" content="`)
	htmlBotOGTitleEnd = []byte(`">
  <meta property="og:description" content="`)
	htmlBotOGDescEnd = []byte(`">
  <meta property="og:type" content="website">
  <meta property="og:url" content="`)
	htmlBotOGType = []byte(`">
</head>
<body>
  <div id="app">`)
	htmlBotOGURLEnd = []byte(`">
</head>
<body>
  <div id="app">`)
	htmlBotBodyStart = []byte(`</div>
  <script>window.__INITIAL_DATA__=`)
	htmlBotScriptStart = []byte(`</div>
  <script>window.__INITIAL_DATA__=`)
	htmlBotScriptEnd = []byte(`</script>
</body>
</html>`)

	htmlHead = []byte(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>`)
	htmlTitleEnd = []byte(`</title>
`)
	htmlBodyStart = []byte(`</head>
<body>
  <div id="app"></div>
  <script>window.__INITIAL_DATA__=`)
	htmlScriptEnd = []byte(`</script>
  <script type="module" src="/src/main.js"></script>
</body>
</html>`)
)

func writeEscapedString(w http.ResponseWriter, s string) {
	if s == "" {
		return
	}
	last := 0
	for i := 0; i < len(s); i++ {
		var esc []byte
		switch s[i] {
		case '&':
			esc = htmlAmp
		case '<':
			esc = htmlLT
		case '>':
			esc = htmlGT
		case '"':
			esc = htmlQuote
		case '\\':
			esc = htmlBS
		default:
			continue
		}
		if i > last {
			_, _ = w.Write(stringToBytes(s[last:i]))
		}
		_, _ = w.Write(esc)
		last = i + 1
	}
	if last < len(s) {
		_, _ = w.Write(stringToBytes(s[last:]))
	}
}

var (
	htmlAmp   = []byte("&amp;")
	htmlLT    = []byte("&lt;")
	htmlGT    = []byte("&gt;")
	htmlQuote = []byte("&quot;")
	htmlBS    = []byte("\\")
)

// writeHTMLResponse writes an HTML page with embedded data for SSR
// For bots: full SSR with inline content. For browsers: minimal HTML + JS.
func writeHTMLResponse(w http.ResponseWriter, r *http.Request, title string, data map[string]interface{}) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		return
	}

	jsonData, _ := json.Marshal(data)
	safeJSON := strings.ReplaceAll(string(jsonData), "<", "\\u003c")

	// Base URL
	baseURL := "https://makoshop.com"

	// Extract SEO fields
	seoURL := ""
	desc := "MakoShop — маркетплейс товаров по лучшим ценам от проверенных поставщиков."
	image := ""

	if page, ok := data["page"].(*model.SCUPage); ok {
		seoURL = data["seo_url"].(string)
		if page.Description != "" {
			desc = page.Description
		}
		if len(page.Images) > 0 {
			image = page.Images[0]
		}
	} else if seoU, ok := data["seo_url"].(string); ok {
		seoURL = seoU
	}

	canonicalTag := ""
	if seoURL != "" {
		canonicalTag = `  <link rel="canonical" href="` + baseURL + seoURL + `">
`
	}

	ogTags := ""
	if image != "" {
		ogTags = `  <meta property="og:image" content="` + html.EscapeString(image) + `">
`
	}

	if isBot(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Full SSR for bots: inline content + SEO tags
		bodyContent := renderSSRContent(jsonData)

		w.WriteHeader(http.StatusOK)

		ogURL := baseURL + seoURL
		if ogURL == baseURL {
			ogURL = baseURL + "/shop"
		}
		w.Write(stringToBytes(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>`))
		w.Write(stringToBytes(html.EscapeString(title)))

		w.Write(stringToBytes(`</title>
  <meta name="description" content="`))
		w.Write(stringToBytes(html.EscapeString(desc)))
		w.Write(stringToBytes(`">
`))
		w.Write(stringToBytes(canonicalTag))
		w.Write(stringToBytes(ogTags))
		w.Write(stringToBytes(`  <meta property="og:title" content="`))
		w.Write(stringToBytes(html.EscapeString(title)))
		w.Write(stringToBytes(`">
  <meta property="og:description" content="`))
		w.Write(stringToBytes(html.EscapeString(desc)))
		w.Write(stringToBytes(`">
  <meta property="og:type" content="website">
  <meta property="og:url" content="`))
		w.Write(stringToBytes(ogURL))
		w.Write(stringToBytes(`">
</head>
<body>
  <div id="app">`))
		w.Write(stringToBytes(bodyContent))
		w.Write(stringToBytes(`</div>
  <script>window.__INITIAL_DATA__=`))
		w.Write(stringToBytes(safeJSON))
		w.Write(stringToBytes(`</script>
</body>
</html>`))

		//w.Header().Set("Content-Type", "text/html; charset=utf-8")
		//w.WriteHeader(http.StatusOK)
		//w.Write(stringToBytes(htmlStr))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// Minimal HTML for browsers (SPA)
	w.Write(stringToBytes(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>`))
	w.Write(stringToBytes(html.EscapeString(title)))
	w.Write(stringToBytes(`</title>
`))
	w.Write(stringToBytes(canonicalTag))
	w.Write(stringToBytes(`</head>
<body>
  <div id="app"></div>
  <script>window.__INITIAL_DATA__=`))
	w.Write(stringToBytes(safeJSON))
	w.Write(stringToBytes(`</script>
  <script type="module" src="/src/main.js"></script>
</body>
</html>`))
	//w.Write(stringToBytes(htmlStr))
}

func stringToBytes(s string) []byte {
	return *((*[]byte)(unsafe.Pointer(&s)))
}

// splitNonEmpty splits s by sep, returning only non-empty parts.
// Avoids the extra alloc of strings.Split + loop.
func splitNonEmpty(s string, sep rune) []string {
	if s == "" {
		return nil
	}
	count := 0
	start := 0
	for i := 0; i < len(s); i++ {
		if rune(s[i]) == sep {
			if i > start {
				count++
			}
			start = i + 1
		}
	}
	if start < len(s) {
		count++
	}
	if count == 0 {
		return nil
	}
	result := make([]string, 0, count)
	start = 0
	for i := 0; i < len(s); i++ {
		if rune(s[i]) == sep {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

// writeSafeJSON writes JSON to w, escaping '<' as '\u003c' to prevent
// script injection in HTML context. Streams directly without allocating
// a full copy (avoids bytes.ReplaceAll).
var safeLT = []byte("\\u003c")

func writeSafeJSON(w interface{ Write([]byte) (int, error) }, data []byte) {
	last := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '<' {
			if i > last {
				_, _ = w.Write(data[last:i])
			}
			_, _ = w.Write(safeLT)
			last = i + 1
		}
	}
	if last < len(data) {
		_, _ = w.Write(data[last:])
	}
}

// declineRussian declines a number into correct Russian form (1/2-4/5-20, 21, etc).
// singular: 1, 21, 31...
// plural: 2-4, 22-24, 32-34...
// many: 5-20, 25-30, 35-40...
func declineRussian(n int, singular, plural, many string) string {
	lastTwo := n % 100
	lastOne := n % 10
	if lastTwo >= 11 && lastTwo <= 19 {
		return many
	}
	switch lastOne {
	case 1:
		return singular
	case 2, 3, 4:
		return plural
	default:
		return many
	}
}

// renderSSRContent generates inline HTML for bots from JSON data.
func renderSSRContent(jsonData []byte) string {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return ""
	}

	var buf strings.Builder

	catalogLabel := i18n.T("ui.catalog_label")
	brandLabel := i18n.T("ui.brand_label")

	// Check if this is a catalog page (has "items")
	if raw, ok := data["items"]; ok {
		var items []map[string]interface{}
		if err := json.Unmarshal(raw, &items); err == nil && len(items) > 0 {
			buf.WriteString(`<div class="ssr-catalog"><h1>`)
			buf.WriteString(html.EscapeString(catalogLabel))
			buf.WriteString(`</h1><div class="ssr-grid">`)
			for _, m := range items {
				buf.WriteString(renderSSRProductCard(m, brandLabel))
			}
			buf.WriteString(`</div></div>`)
			return buf.String()
		}
	}

	// Check if this is a single SCUPage (has "page")
	if raw, ok := data["page"]; ok {
		var page model.SCUPage
		if err := json.Unmarshal(raw, &page); err == nil {
			var products []model.Product
			if rawProds, ok := data["products"]; ok {
				json.Unmarshal(rawProds, &products)
			}
			buf.WriteString(`<div class="ssr-scupage">`)
			buf.WriteString(fmt.Sprintf(`<h1>%s</h1>`, html.EscapeString(page.Title)))
			if page.Description != "" {
				buf.WriteString(fmt.Sprintf(`<p class="ssr-desc">%s</p>`, html.EscapeString(page.Description)))
			}
			if page.Brand != "" {
				buf.WriteString(fmt.Sprintf(`<p>%s: %s</p>`, html.EscapeString(brandLabel), html.EscapeString(page.Brand)))
			}
			if page.MinPrice > 0 {
				buf.WriteString(fmt.Sprintf(`<p class="ssr-price">%.2f %s</p>`, page.MinPrice, page.Currency))
			}
			if len(page.Images) > 0 {
				buf.WriteString(fmt.Sprintf(`<img src="%s" alt="%s" class="ssr-image">`,
					html.EscapeString(page.Images[0]), html.EscapeString(page.Title)))
			}
			if len(products) > 0 {
				buf.WriteString(`<h2>Предложения поставщиков</h2><table class="ssr-table"><thead><tr><th>Поставщик</th><th>Цена</th><th>Наличие</th></tr></thead><tbody>`)
				for _, p := range products {
					buf.WriteString(`<tr>`)
					buf.WriteString(fmt.Sprintf(`<td>%s</td>`, html.EscapeString(p.Name)))
					buf.WriteString(fmt.Sprintf(`<td>%.2f %s</td>`, p.Price, p.Currency))
					if p.StockQty > 0 {
						buf.WriteString(`<td>В наличии</td>`)
					} else {
						buf.WriteString(`<td>Нет в наличии</td>`)
					}
					buf.WriteString(`</tr>`)
				}
				buf.WriteString(`</tbody></table>`)
			}
			buf.WriteString(`</div>`)
			return buf.String()
		}
	}

	return ""
}

// renderSSRProductCard renders a single product card for catalog SSR.
func renderSSRProductCard(m map[string]interface{}, brandLabel string) string {
	title, _ := m["title"].(string)
	slug, _ := m["slug"].(string)
	seoURL, _ := m["seo_url"].(string)
	if seoURL == "" && slug != "" {
		seoURL = "/shop/" + slug
	}
	minPrice, _ := m["min_price"].(float64)
	currency, _ := m["currency"].(string)
	brand, _ := m["brand"].(string)
	productCount, _ := m["product_count"].(int)
	images, _ := m["images"].([]string)

	imgSrc := ""
	if len(images) > 0 {
		imgSrc = images[0]
	}

	buf := strings.Builder{}
	buf.WriteString(`<div class="ssr-card"><a href="`)
	buf.WriteString(html.EscapeString(seoURL))
	buf.WriteString(`">`)
	if imgSrc != "" {
		buf.WriteString(fmt.Sprintf(`<img src="%s" alt="%s">`, html.EscapeString(imgSrc), html.EscapeString(title)))
	}
	buf.WriteString(fmt.Sprintf(`<h2>%s</h2>`, html.EscapeString(title)))
	if brand != "" {
		buf.WriteString(fmt.Sprintf(`<p>%s: %s</p>`, html.EscapeString(brandLabel), html.EscapeString(brand)))
	}
	buf.WriteString(fmt.Sprintf(`<p class="ssr-price">%.2f %s</p>`, minPrice, currency))
	if productCount > 1 {
		lang := i18n.Current()
		offersText := declineRussian(productCount,
			i18n.T("ui.offer_count_singular"),
			i18n.T("ui.offer_count_plural"),
			i18n.T("ui.offer_count_many"),
		)
		if lang == "en" {
			// English doesn't need complex declension; just plural
			offersText = i18n.T("ui.offer_count_plural")
		}
		buf.WriteString(fmt.Sprintf(`<p>%d %s</p>`, productCount, offersText))
	}
	buf.WriteString(`</a></div>`)
	return buf.String()
}

// findCategoryByPath finds a category ID by its slug path [slug1, slug2, ...].
func (h *Handlers) findCategoryByPath(slugs []string) (int64, error) {
	if len(slugs) == 0 {
		return 0, fmt.Errorf("empty path")
	}

	// Try O(1) lookup by path hash first
	cat, err := h.categoryRepo.GetByPath(slugs)
	if err == nil && cat != nil {
		if !cat.IsActive {
			return 0, fmt.Errorf("category not found") // hide inactive
		}
		return cat.ID, nil
	}

	// Fallback: walk slug by slug using GetBySlug (O(1) each)
	var currentID *int64
	var lastCat *model.Category
	for i, slug := range slugs {
		cat, err := h.categoryRepo.GetBySlug(slug)
		if err != nil || cat == nil {
			return 0, fmt.Errorf("category not found in path: %s", slug)
		}

		if i == 0 {
			currentID = &cat.ID
			lastCat = cat
		} else if currentID != nil && cat.ParentID != nil && *cat.ParentID == *currentID {
			currentID = &cat.ID
			lastCat = cat
		} else {
			return 0, fmt.Errorf("category not found in path: %s", slug)
		}
	}

	if lastCat != nil && !lastCat.IsActive {
		return 0, fmt.Errorf("category not found")
	}

	if currentID == nil {
		return 0, fmt.Errorf("path resolution failed")
	}
	return *currentID, nil
}

// buildSEOURL builds the SEO URL for a SCU page.
func buildSEOURL(sp *model.SCUPage, treePath []string) string {
	parts := append(treePath, sp.Slug)
	return "/shop/" + strings.Join(parts, "/")
}

type SeoSlugSCU struct {
	Slug       string `json:"slug"`
	CategoryID int64  `json:"category_id"`
}

// --- Request types ---

type CreateLandingRequest struct {
	SCU         string   `json:"scu"`
	Slug        string   `json:"slug,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Content     string   `json:"content,omitempty"`
	Images      []string `json:"images,omitempty"`
	IsActive    bool     `json:"is_active"`
	ProductIDs  []int64  `json:"product_ids,omitempty"`
}

type UpdateLandingRequest struct {
	SCU         *string   `json:"scu,omitempty"`
	Slug        *string   `json:"slug,omitempty"`
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	Content     *string   `json:"content,omitempty"`
	Images      *[]string `json:"images,omitempty"`
	IsActive    *bool     `json:"is_active,omitempty"`
	ProductIDs  *[]int64  `json:"product_ids,omitempty"`
}

// HandleAdminRebuildSCUPages rebuilds all SCU pages from existing products.
// POST /admin/rebuild-scupages
// Uses the same logic as import: BatchUpsertFromProducts for consistent seo_url generation.
func (h *Handlers) HandleAdminRebuildSCUPages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	fmt.Println("[REBUILD-SCUPAGES] Starting SCU page rebuild...")
	startTime := time.Now()

	// Get all product IDs
	nextIDData, _ := h.productRepo.Store().DB().TurboRawRead("state:next_id:product")
	var maxID int64
	if len(nextIDData) > 0 {
		_, _ = fmt.Sscanf(string(nextIDData), "%d", &maxID)
	}
	fmt.Printf("[REBUILD-SCUPAGES] Max product ID: %d\n", maxID)

	var allIDs []uint64
	for id := int64(1); id < maxID; id++ {
		_, err := h.productRepo.Get(id)
		if err == nil {
			allIDs = append(allIDs, uint64(id))
		}
		if len(allIDs)%50000 == 0 && len(allIDs) > 0 {
			fmt.Printf("[REBUILD-SCUPAGES] Collected %d product IDs\n", len(allIDs))
		}
	}
	fmt.Printf("[REBUILD-SCUPAGES] Total products: %d\n", len(allIDs))

	if len(allIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no_products"})
		return
	}

	// Read all products into memory (same as import)
	fmt.Printf("[REBUILD-SCUPAGES] Reading %d products...\n", len(allIDs))
	readStart := time.Now()
	var allProducts []*model.Product
	const readBatchSize = 10000
	for i := 0; i < len(allIDs); i += readBatchSize {
		end := i + readBatchSize
		if end > len(allIDs) {
			end = len(allIDs)
		}
		for _, docID := range allIDs[i:end] {
			p, err := h.productRepo.Get(int64(docID))
			if err != nil {
				continue
			}
			allProducts = append(allProducts, p)
		}
		if (i+end)/2%100000 == 0 {
			fmt.Printf("[REBUILD-SCUPAGES] Read %d/%d products\n", (i+end)/2, len(allIDs))
		}
	}
	fmt.Printf("[REBUILD-SCUPAGES] Read %d products in %v\n", len(allProducts), time.Since(readStart))

	if len(allProducts) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no_products_with_scu"})
		return
	}

	// Phase 1: Batch upsert SCU pages (uses same logic as import, including seo_url)
	fmt.Printf("[REBUILD-SCUPAGES] Phase 1: BatchUpsertFromProducts %d products...\n", len(allProducts))
	phase1Start := time.Now()
	_ = h.scuPageRepo.BatchUpsertFromProducts(allProducts)
	fmt.Printf("[REBUILD-SCUPAGES] Phase 1: SCU pages upserted in %v\n", time.Since(phase1Start))

	// Phase 2: Index all products (creates scu:{scu} indexes for SCU→Products lookup)
	fmt.Printf("[REBUILD-SCUPAGES] Phase 2: Indexing %d products...\n", len(allProducts))
	phase2Start := time.Now()
	if h.productRepo.TurboSearch() != nil {
		// Clear global product list first to avoid duplicates
		_ = h.productRepo.Store().DB().TurboClearIndex(db.TurboKeyProductList)

		const idxBatchSize = 10000
		for i := 0; i < len(allProducts); i += idxBatchSize {
			end := i + idxBatchSize
			if end > len(allProducts) {
				end = len(allProducts)
			}
			if err := h.productRepo.TurboSearch().IndexProductBatch(allProducts[i:end]); err != nil {
				fmt.Printf("[REBUILD-SCUPAGES] WARN: index product batch: %v\n", err)
			}
			if (i+end)/2%100000 == 0 {
				fmt.Printf("[REBUILD-SCUPAGES] Indexed %d/%d products\n", (i+end)/2, len(allProducts))
			}
		}
	}
	fmt.Printf("[REBUILD-SCUPAGES] Phase 2: Products indexed in %v\n", time.Since(phase2Start))

	// Phase 3: Index all SCU pages
	fmt.Println("[REBUILD-SCUPAGES] Phase 3: Indexing SCU pages...")
	phase3Start := time.Now()
	if h.scuPageSearch != nil {
		allSCUs, _ := h.scuPageRepo.ListAll()
		scuPtrs := make([]*model.SCUPage, len(allSCUs))
		for i := range allSCUs {
			scuPtrs[i] = &allSCUs[i]
		}
		if err := h.scuPageSearch.IndexSCUPageBatch(scuPtrs); err != nil {
			fmt.Printf("[REBUILD-SCUPAGES] WARN: index SCU pages: %v\n", err)
		}
	}
	fmt.Printf("[REBUILD-SCUPAGES] Phase 3: SCU pages indexed in %v\n", time.Since(phase3Start))

	// Phase 4: Build sort indexes for SCU pages
	fmt.Println("[REBUILD-SCUPAGES] Phase 4: Building SCU page sort indexes...")
	phase4Start := time.Now()
	if h.scuPageSearch != nil {
		if err := h.scuPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[REBUILD-SCUPAGES] WARN: build sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[REBUILD-SCUPAGES] Phase 4: Sort indexes built in %v\n", time.Since(phase4Start))

	// Phase 5: Rebuild product sort indexes
	fmt.Println("[REBUILD-SCUPAGES] Phase 5: Rebuilding product sort indexes...")
	phase5Start := time.Now()
	if h.productRepo.TurboSearch() != nil {
		if err := h.productRepo.TurboSearch().BuildSortIndexes(); err != nil {
			fmt.Printf("[REBUILD-SCUPAGES] WARN: rebuild product sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[REBUILD-SCUPAGES] Phase 5: Product sort indexes rebuilt in %v\n", time.Since(phase5Start))

	// Phase 6: Recalculate SCU page product counts
	fmt.Println("[REBUILD-SCUPAGES] Phase 6: Recalculating SCU page product counts...")
	phase6Start := time.Now()
	if err := h.scuPageRepo.RecalculateProductCounts(); err != nil {
		fmt.Printf("[REBUILD-SCUPAGES] WARN: recalculate product counts: %v\n", err)
	}
	fmt.Printf("[REBUILD-SCUPAGES] Phase 6: Product counts recalculated in %v\n", time.Since(phase6Start))

	elapsed := time.Since(startTime)
	fmt.Printf("[REBUILD-SCUPAGES] Completed in %v\n", elapsed)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "completed",
		"products": len(allProducts),
		"elapsed":  elapsed.String(),
	})
}

// HandleAdminRebuildSCUPageSortIndexes rebuilds sort indexes for SCU pages.
func (h *Handlers) HandleAdminRebuildSCUPageSortIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if h.scuPageSearch == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "scupage search not initialized")
		return
	}

	fmt.Println("[REBUILD-SCUPAGE-SORT-INDEXES] Starting...")
	startTime := time.Now()

	if err := h.scuPageSearch.BuildSortIndexes(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[REBUILD-SCUPAGE-SORT-INDEXES] Completed in %v\n", elapsed)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "completed",
		"elapsed": elapsed.String(),
	})
}

// HandleAdminRebuildProductCounts recalculates ProductCount for all SCU pages.
// POST /admin/rebuild-product-counts
func (h *Handlers) HandleAdminRebuildProductCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if h.scuPageRepo == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "scupage repo not initialized")
		return
	}

	fmt.Println("[REBUILD-PRODUCT-COUNTS] Starting...")
	startTime := time.Now()

	if err := h.scuPageRepo.RecalculateProductCounts(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[REBUILD-PRODUCT-COUNTS] Completed in %v\n", elapsed)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "completed",
		"elapsed": elapsed.String(),
	})
}

// HandleAdminRebuildSCUPageIndexes indexes all SCU pages into SCUPageSearch.
func (h *Handlers) HandleAdminRebuildSCUPageIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if h.scuPageSearch == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "scupage search not initialized")
		return
	}

	fmt.Println("[REBUILD-SCUPAGE-INDEXES] Starting...")
	startTime := time.Now()

	// Get all SCU pages
	all, err := h.scuPageRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Use batch indexing for speed
	allPtrs := make([]*model.SCUPage, len(all))
	for i := range all {
		allPtrs[i] = &all[i]
	}
	if err := h.scuPageSearch.IndexSCUPageBatch(allPtrs); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	indexed := len(all)
	errors := 0

	// Build sort indexes ONCE after all pages are indexed
	if err := h.scuPageSearch.BuildSortIndexes(); err != nil {
		fmt.Printf("[REBUILD-SCUPAGE-INDEXES] WARN: build sort indexes: %v\n", err)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[REBUILD-SCUPAGE-INDEXES] Completed: %d indexed, %d errors in %v\n", indexed, errors, elapsed)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "completed",
		"total":   len(all),
		"indexed": indexed,
		"errors":  errors,
		"elapsed": elapsed.String(),
	})
}

// HandleAdminRebuildCategorySlugs rebuilds slugs for all categories.
func (h *Handlers) HandleAdminRebuildCategorySlugs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	fmt.Println("[REBUILD-CAT-SLUGS] Starting...")
	startTime := time.Now()

	cats, err := h.categoryRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	updated := 0
	for _, c := range cats {
		nameEn := c.NameEn
		if nameEn == "" {
			nameEn = c.NameRu
		}
		if c.Slug == "" || c.Slug == nameEn {
			newSlug := toSlugLocal(nameEn)
			if err := h.categoryRepo.Update(c.ID, func(cat *model.Category) {
				cat.Slug = newSlug
			}); err != nil {
				fmt.Printf("[REBUILD-CAT-SLUGS] WARN: update category %d: %v\n", c.ID, err)
				continue
			}
			updated++
		}
	}

	elapsed := time.Since(startTime)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"updated": updated,
		"elapsed": elapsed.String(),
	})
}

// HandleAdminRebuildCategoryIndexes rebuilds all category turbo indexes.
// POST /admin/rebuild-category-indexes?force=1 — force rebuild from docs
func (h *Handlers) HandleAdminRebuildCategoryIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	force := r.URL.Query().Get("force") == "1"

	fmt.Printf("[REBUILD-CAT-INDEXES] Starting (force=%v)\n", force)
	startTime := time.Now()

	var err error
	if force {
		err = h.categoryRepo.RebuildIndexesFromDocs()
	} else {
		err = h.categoryRepo.RebuildAllIndexes()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	elapsed := time.Since(startTime)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"elapsed": elapsed.String(),
	})
}

// HandleAdminRebuildAttrDefIndexes rebuilds attrdef cat_codes indexes.
func (h *Handlers) HandleAdminRebuildAttrDefIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	fmt.Println("[REBUILD-ATTRDEF-INDEXES] Starting...")
	startTime := time.Now()

	if err := h.attrDefRepo.RebuildCatCodesIndex(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	elapsed := time.Since(startTime)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"elapsed": elapsed.String(),
	})
}

// toSlugLocal creates a URL-friendly slug with Cyrillic transliteration.
func toSlugLocal(s string) string {
	translitMap := map[rune]string{
		'А': "a", 'Б': "b", 'В': "v", 'Г': "g", 'Д': "d", 'Е': "e", 'Ё': "e", 'Ж': "zh",
		'З': "z", 'И': "i", 'Й': "y", 'К': "k", 'Л': "l", 'М': "m", 'Н': "n", 'О': "o",
		'П': "p", 'Р': "r", 'С': "s", 'Т': "t", 'У': "u", 'Ф': "f", 'Х': "kh", 'Ц': "ts",
		'Ч': "ch", 'Ш': "sh", 'Щ': "shch", 'Ъ': "", 'Ы': "y", 'Ь': "", 'Э': "e", 'Ю': "yu", 'Я': "ya",
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
		'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
		'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
		'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	}

	var result strings.Builder
	for _, r := range s {
		if t, ok := translitMap[r]; ok {
			result.WriteString(t)
		} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			result.WriteString("-")
		}
	}

	slug := result.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")

	return strings.ToLower(slug)
}

// limitStrings truncates a slice to at most maxLen elements.
func limitStrings(s []string, maxLen int) []string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
