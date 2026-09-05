package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/i18n"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
)

// browserAssetTags holds the <script>/<link> tags extracted from the built
// frontend index.html, so browser (non-bot) SSR pages reference the real
// production bundles. Defaults to the dev entry point when dist/ is absent.
//
// The tags are kept in sync with frontend rebuilds: refreshBrowserAssetTags
// re-extracts them whenever dist/index.html is modified on disk (mtime check),
// so SSR pages pick up a new frontend build without a server restart.
var (
	browserAssetTags = `<script type="module" src="/src/main.js"></script>`
	browserTagsMu    sync.RWMutex
	browserTagsMtime time.Time
)

var browserTagsRe = regexp.MustCompile(`<script[^>]*src=["'][^"']*["'][^>]*></script>|<link[^>]*href=["'][^"']*["'][^>]*>`)

// LoadBrowserAssetTags performs the initial extraction of production asset
// tags from frontend/dist/index.html. Called once at startup; afterwards
// refreshBrowserAssetTags (invoked from browserScriptEnd) keeps the tags in
// sync with frontend rebuilds. If the built index.html is missing, the dev
// fallback is kept so local development (Vite dev server) still works.
func LoadBrowserAssetTags() {
	refreshBrowserAssetTags()
}

// refreshBrowserAssetTags re-extracts the asset tags when dist/index.html is
// newer than the last extraction. Cheap in the common case (one os.Stat);
// safe for concurrent use.
func refreshBrowserAssetTags() {
	fi, err := os.Stat("frontend/dist/index.html")
	if err != nil {
		return
	}
	browserTagsMu.RLock()
	unchanged := !fi.ModTime().After(browserTagsMtime)
	browserTagsMu.RUnlock()
	if unchanged {
		return
	}
	data, err := os.ReadFile("frontend/dist/index.html")
	if err != nil {
		return
	}
	tags := browserTagsRe.FindAllString(string(data), -1)
	if len(tags) == 0 {
		return
	}
	browserTagsMu.Lock()
	browserAssetTags = strings.Join(tags, "\n  ")
	browserTagsMtime = fi.ModTime()
	browserTagsMu.Unlock()
}

// browserScriptEnd returns the closing HTML for browser SSR pages, using the
// production asset tags (or the dev fallback). It refreshes the tags first so
// a frontend rebuild is picked up without a server restart.
func browserScriptEnd() []byte {
	refreshBrowserAssetTags()
	browserTagsMu.RLock()
	tags := browserAssetTags
	browserTagsMu.RUnlock()
	return []byte(`</script>
  ` + tags + `
</body>
</html>`)
}

// --- Landing Page handlers ---

// HandleLandingPagesList returns all landing pages (admin).
// GET /admin/landings
func (h *Handlers) HandleLandingPagesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	lps, err := h.landingRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if lps == nil {
		lps = []model.LandingPage{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": lps,
	})
}

// HandleLandingPageGet returns a landing page by ID (admin).
// GET /admin/landings/{id}
func (h *Handlers) HandleLandingPageGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "landing_id")
	if !ok {
		return
	}

	lp, err := h.landingRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, lp)
}

// HandleLandingPageCreate creates a new landing page (admin).
// POST /admin/landings
func (h *Handlers) HandleLandingPageCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateLandingRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.EAN == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "ean is required")
		return
	}

	lp := &model.LandingPage{
		EAN:         req.EAN,
		Slug:        req.Slug,
		Title:       req.Title,
		Description: req.Description,
		Content:     req.Content,
		Images:      req.Images,
		IsActive:    req.IsActive,
		ProductIDs:  req.ProductIDs,
	}

	if err := h.landingRepo.Create(lp); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, lp)
}

// HandleLandingPageUpdate updates a landing page (admin).
// PUT /admin/landings/{id}
func (h *Handlers) HandleLandingPageUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "landing_id")
	if !ok {
		return
	}

	var req UpdateLandingRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if err := h.landingRepo.Update(id, func(lp *model.LandingPage) {
		if req.EAN != nil {
			lp.EAN = *req.EAN
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	lp, _ := h.landingRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, lp)
}

// HandleLandingPageDelete deletes a landing page (admin).
// DELETE /admin/landings/{id}
func (h *Handlers) HandleLandingPageDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "landing_id")
	if !ok {
		return
	}

	if err := h.landingRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleLandingPageBySlug returns a landing page by slug (public).
// GET /landing/{slug}
func (h *Handlers) HandleLandingPageBySlug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract slug from path: /landing/{slug}
	path := r.URL.Path
	prefix := "/landing/"
	if !strings.HasPrefix(path, prefix) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	slug := strings.TrimPrefix(path, prefix)
	if slug == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing slug")
		return
	}

	lp, err := h.landingRepo.GetBySlug(slug)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	// Get products with this EAN
	products, _ := h.turboSearch.GetProductsByEAN(lp.EAN)
	if products == nil {
		products = []model.Product{}
	}
	h.enrichProductsWithCompanyNames(products)

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"landing":  lp,
		"products": products,
	})
}

// HandleLandingPageByEAN returns a landing page by EAN (public).
// GET /landing/ean/{ean}
func (h *Handlers) HandleLandingPageByEAN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract EAN from path: /landing/ean/{ean}
	path := r.URL.Path
	prefix := "/landing/ean/"
	if !strings.HasPrefix(path, prefix) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	ean := strings.TrimPrefix(path, prefix)
	if ean == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing ean")
		return
	}

	lp, err := h.landingRepo.GetByEAN(ean)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	// Get products with this EAN
	products, _ := h.turboSearch.GetProductsByEAN(lp.EAN)
	if products == nil {
		products = []model.Product{}
	}
	h.enrichProductsWithCompanyNames(products)

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"landing":  lp,
		"products": products,
	})
}

// HandleLandingPageProducts returns products for a landing page (public).
// GET /landing/{slug}/products
func (h *Handlers) HandleLandingPageProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract slug from path: /landing/{slug}/products
	path := r.URL.Path
	prefix := "/landing/"
	if !strings.HasPrefix(path, prefix) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "products" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	slug := parts[0]
	if slug == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing slug")
		return
	}

	lp, err := h.landingRepo.GetBySlug(slug)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "landing page not found")
		return
	}

	// Get products with this EAN
	products, _ := h.turboSearch.GetProductsByEAN(lp.EAN)
	if products == nil {
		products = []model.Product{}
	}
	h.enrichProductsWithCompanyNames(products)

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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": filtered,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// --- EANPage handlers (SEO pages) ---

// HandleEANPageByPath handles SEO page requests.
// GET /shop — all EAN pages (root catalog)
// GET /shop/{category_tree} — EAN pages in category (category catalog)
// GET /shop/{category_tree}/{slug} — single EAN page
// Example: /shop/elektronika/telefony/samsung-galaxy-s7
// Priority: category first, then EAN page, then redirect to referer/home.
func (h *Handlers) HandleEANPageByPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Canonicalize trailing slash: /shop/x/y/ -> /shop/x/y (301). Without this,
	// both forms serve identical content as duplicate URLs. This handler only
	// serves /shop and /shop/... paths, so any trailing slash is safe to strip.
	if len(r.URL.Path) > 1 && strings.HasSuffix(r.URL.Path, "/") {
		target := strings.TrimSuffix(r.URL.Path, "/")
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
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
		// /shop — root catalog (all EAN pages)
		h.handleEANPageCatalog(w, r, 0)
		return
	}

	// Step 1: Try EAN page by last part as EAN code first
	// If it's a single part, it might be an EAN code (e.g. /shop/5901234123457)
	if len(cleanParts) == 1 {
		part := cleanParts[0]
		if sp, err := h.eanPageRepo.GetByEAN(part); err == nil {
			canonical := "/shop/" + sp.Slug
			if sp.CategoryID != 0 {
				treePath, tpErr := h.categoryRepo.GetTreePath(sp.CategoryID)
				if tpErr == nil && len(treePath) > 0 {
					canonical = "/shop/" + strings.Join(treePath, "/") + "/" + sp.Slug
				}
			}
			if path != canonical {
				http.Redirect(w, r, canonical, http.StatusMovedPermanently)
				return
			}
			h.writeEANPageResponse(w, r, sp)
			return
		}
	}

	// Step 1b: Try EAN page by last part as slug
	// If it's a single part, it might be an EAN page slug (e.g. /shop/samsung-galaxy-s7)
	// or a category slug (e.g. /shop/elektronika).
	// We check EAN page first if it's a single part to allow redirects.
	if len(cleanParts) == 1 {
		slug := cleanParts[0]
		sp, err := h.eanPageRepo.GetBySlug(slug)
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
			h.writeEANPageResponse(w, r, sp)
			return
		}
	}

	// Step 2: Try to find category by full path
	catID, err := h.findCategoryByPath(cleanParts)
	if err == nil {
		h.handleEANPageCatalog(w, r, catID)
		return
	}

	// Step 3: Try EAN page by last part as EAN code or slug (for deep paths that didn't match category)
	part := cleanParts[len(cleanParts)-1]
	var sp *model.EANPage

	// Try EAN first
	sp, err = h.eanPageRepo.GetByEAN(part)
	if err != nil {
		// Fallback to slug
		sp, err = h.eanPageRepo.GetBySlug(part)
	}
	if err == nil {
		// Build canonical URL for this EANPage
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
		h.writeEANPageResponse(w, r, sp)
		return
	}

	// Step 3: Not found — return 404
	httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "page not found")
}

var eanListRespRegistry = silentjson.BuildRegistry(reflect.TypeOf(db.EANListRespData{}))

// handleEANPageCatalog returns a paginated list of EAN pages for a category.
func (h *Handlers) handleEANPageCatalog(w http.ResponseWriter, r *http.Request, catID int64) {
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

	// Use EANPageSearch for catalog listing
	if h.eanPageSearch != nil {
		params := db.EANPageListParams{
			Q:           q,
			CategoryID:  catID,
			AttrFilters: attrFilters,
			PriceMin:    priceMin,
			PriceMax:    priceMax,
			Sort:        sort,
			Page:        page,
			Limit:       limit,
		}
		result, err := h.eanPageSearch.ListWithTurbo(params)
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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

		respData := db.EANListRespData{
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
				// Canonical URL for the category listing page.
				respData.SEOURL = "/shop/" + strings.Join(treePath, "/")
			}
			// Include full category object (with descriptions and images)
			if cat, err := h.categoryRepo.Get(catID); err == nil {
				respData.Category = *cat
				// Add subcategories as precomputed JSON (no struct->json)
				if subsJSON, err := h.categoryRepo.GetTreeByParentJSON(catID); err == nil && len(subsJSON) > 0 {
					respData.Subcategories = silentjson.RawMessage(subsJSON)
				}
			}
		} else {
			// Root catalog: canonical is /shop
			respData.SEOURL = "/shop"
		}

		if wantsHTML(r) {
			writeHTMLResponseEANList(w, r, i18n.T("ui.catalog_title"), h.siteBaseURL(), respData, h.seoSettings(), nil)
			return
		}
		writeJSONEANList(w, r, http.StatusOK, respData)
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSONEANList(w, r, http.StatusOK, *result)
}

// writeEANPageResponse writes the EAN page with its products.
// enrichProductsWithCompanyNames fills Product.CompanyName from the company
// table so the frontend can show the real company name instead of a fallback.
func (h *Handlers) enrichProductsWithCompanyNames(products []model.Product) {
	if len(products) == 0 || h.companyRepo == nil {
		return
	}
	seen := make(map[int64]string, len(products))
	for i := range products {
		cid := products[i].CompanyID
		if cid == 0 || products[i].CompanyName != "" {
			continue
		}
		if name, ok := seen[cid]; ok {
			products[i].CompanyName = name
			continue
		}
		name := ""
		if c, err := h.companyRepo.Get(cid); err == nil && c != nil {
			name = c.Name
		}
		seen[cid] = name
		products[i].CompanyName = name
	}
}

func (h *Handlers) writeEANPageResponse(w http.ResponseWriter, r *http.Request, sp *model.EANPage) {
	ctx := r.Context()

	// Get products with this EAN via turbo index "ean:{ean}"
	products, err := h.turboSearch.GetProductsByEAN(sp.EAN)
	if err != nil || products == nil {
		products = []model.Product{}
	}
	h.enrichProductsWithCompanyNames(products)

	// Show EAN page even if no products — frontend renders "not available"
	// (catalog filtering is handled by BuildSortIndexes excluding product_count == 0)

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

	// writeJSONEANList
	respData := db.EANListRespData{
		EANPage:      sp,
		Products:     products,
		TreePath:     treePath,
		TreePathFull: treePathFull,
		SEOURL:       seoURL,
		CatID:        sp.CategoryID,
		Total:        int64(len(products)),
		Page:         1,
		Limit:        50,
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

	title := sp.Title + " — " + h.siteName()
	if wantsHTML(r) {
		// Fetch approved reviews for the products sharing this EAN (for the
		// Product JSON-LD `review`/`aggregateRating` fields). Capped to avoid
		// excessive DB calls on pages with many products.
		reviews := h.fetchReviewsForProducts(products, 20)
		writeHTMLResponseEANList(w, r, title, h.siteBaseURL(), respData, h.seoSettings(), reviews)
		return
	}
	writeJSONEANList(w, r, http.StatusOK, respData)
}

// fetchReviewsForProducts collects approved reviews for the given products
// (capped at maxTotal) for structured data. Errors are ignored (reviews are
// best-effort; the page still renders without them).
func (h *Handlers) fetchReviewsForProducts(products []model.Product, maxTotal int) []model.Review {
	if h.reviewRepo == nil || len(products) == 0 || maxTotal <= 0 {
		return nil
	}
	var reviews []model.Review
	seen := make(map[int64]struct{})
	for _, p := range products {
		if len(reviews) >= maxTotal {
			break
		}
		if p.ID == 0 {
			continue
		}
		if _, ok := seen[p.ID]; ok {
			continue
		}
		seen[p.ID] = struct{}{}
		perProduct := maxTotal - len(reviews)
		if perProduct > 10 {
			perProduct = 10
		}
		if rvs, _, err := h.reviewRepo.ListByProduct(p.ID, 1, perProduct, string(model.ReviewStatusApproved)); err == nil {
			reviews = append(reviews, rvs...)
		}
	}
	return reviews
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
func writeHTMLResponseEANList(w http.ResponseWriter, r *http.Request, title string, baseURL string, data db.EANListRespData, seo *model.SEOSettings, reviews []model.Review) {
	ctx := r.Context()
	headOnly := r.Method == http.MethodHead

	// For bots: need jsonData for SSR content. For browsers: write directly to avoid copy.
	var jsonData []byte
	if isBot(r) {
		jsonData = marshalWithPool(&data, eanListRespRegistry)
		// Check if client disconnected after marshaling
		select {
		case <-ctx.Done():
			return
		default:
		}
	}

	// Extract SEO fields. seoURL drives the canonical link and og:url; it is
	// populated by the callers for both category listings and EAN pages.
	seoURL := data.SEOURL
	desc := siteNameFromBaseURL(baseURL) + " — онлайн-каталог товаров от проверенных продавцов. Всё, что вы ищете, — в одном месте."
	image := ""

	// JSON-LD structured data (SEO): site-level blocks (Organization, WebSite,
	// OnlineStore) on every page; product-level blocks (Product, BreadcrumbList,
	// OnlineStore) on EAN pages.
	fallbackName := siteNameFromBaseURL(baseURL)
	var ldBlocks []string
	if seo != nil && seo.Enabled {
		ldBlocks = append(ldBlocks, buildSiteJSONLDBlocks(seo, baseURL, fallbackName)...)
		if ep := data.EANPage; ep != nil {
			ldBlocks = append(ldBlocks, buildProductJSONLDBlocks(seo, baseURL, ep, data.Products, reviews, data.TreePath, data.TreePathFull)...)
		}
	}

	if ep := data.EANPage; ep != nil {
		if ep.Description != "" {
			desc = ep.Description
		}
		if len(ep.Images) > 0 {
			image = ep.Images[0]
		}
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

	jsonLDTag := ""
	for _, b := range ldBlocks {
		jsonLDTag += `  <script type="application/ld+json">` + b + `</script>
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
		if jsonLDTag != "" {
			w.Write(stringToBytes(jsonLDTag))
		}
		w.Write(htmlBotOGStart)
		writeEscapedString(w, title)
		w.Write(htmlBotOGTitleEnd)
		writeEscapedString(w, desc)
		w.Write(htmlBotOGDescEnd)
		w.Write(stringToBytes(ogURL))
		w.Write(htmlBotOGURLEnd)
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
	if jsonLDTag != "" {
		w.Write(stringToBytes(jsonLDTag))
	}
	w.Write(htmlBodyStart)
	writeSafeJSONWithPool(w, &data, eanListRespRegistry)
	w.Write(browserScriptEnd())
}

// siteNameFromBaseURL derives the human-readable site name from a base URL
// host (e.g. "https://wszyst.pl" -> "wszyst.pl"). Used by free functions that
// don't have access to *Handlers. Falls back to "MakoShop" in dev.
func siteNameFromBaseURL(baseURL string) string {
	if i := strings.Index(baseURL, "//"); i >= 0 {
		host := baseURL[i+2:]
		if j := strings.IndexAny(host, "/?"); j >= 0 {
			host = host[:j]
		}
		if host != "" && host != "localhost" && host != "localhost:5173" {
			return host
		}
	}
	return "MakoShop"
}

// writeSafeJSONWithPool marshals data and writes escaped JSON directly to w.
// Optimized for hot paths: no extra copy, escapes '<' inline.
func writeSafeJSONWithPool(w http.ResponseWriter, data *db.EANListRespData, reg *silentjson.Registry) {
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
  <meta name="mylead-verification" content="7bc06fe7286b5ce1f518b4c133a2c106">
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
	htmlBotOGURLEnd = []byte(`">
</head>
<body>
  <div id="app">`)
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
  <meta name="mylead-verification" content="7bc06fe7286b5ce1f518b4c133a2c106">
  <title>`)
	htmlTitleEnd = []byte(`</title>
`)
	htmlBodyStart = []byte(`</head>
<body>
  <div id="app"></div>
  <script>window.__INITIAL_DATA__=`)
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

	if page, ok := data["page"].(*model.EANPage); ok {
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
	w.Write(browserScriptEnd())
	//w.Write(stringToBytes(htmlStr))
}

func stringToBytes(s string) []byte {
	if s == "" {
		return nil
	}
	// Zero-copy string->[]byte. A string header is 16 bytes (ptr+len) but a
	// slice header is 24 bytes (ptr+len+cap), so reinterpreting &s as *[]byte
	// reads cap from adjacent garbage memory (latent out-of-bounds). Build the
	// slice explicitly with cap == len via unsafe.Slice instead.
	return unsafe.Slice(unsafe.StringData(s), len(s))
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

// writeSafeJSON writes JSON to w, escaping '<', '>', '/', '\n', '\r' for safe embedding in HTML <script>.
// Prevents script injection and broken JavaScript from newlines in data.
var (
	safeLT = []byte("\\u003c") // <
	safeGT = []byte("\\u003e") // >
	safeSL = []byte("\\u002f") // /
	safeNL = []byte("\\u000a") // \n
	safeCR = []byte("\\u000d") // \r
)

func writeSafeJSON(w interface{ Write([]byte) (int, error) }, data []byte) {
	last := 0
	for i := 0; i < len(data); i++ {
		c := data[i]
		var replacement []byte
		switch c {
		case '<':
			replacement = safeLT
		case '>':
			replacement = safeGT
		case '/':
			replacement = safeSL
		case '\n':
			replacement = safeNL
		case '\r':
			replacement = safeCR
		default:
			continue
		}
		if i > last {
			_, _ = w.Write(data[last:i])
		}
		_, _ = w.Write(replacement)
		last = i + 1
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

	// Check if this is a single EANPage (has "ean_page"). Note: the top-level
	// "page" field is the pagination number, not the object.
	if raw, ok := data["ean_page"]; ok {
		var page model.EANPage
		if err := json.Unmarshal(raw, &page); err == nil {
			var products []model.Product
			if rawProds, ok := data["products"]; ok {
				json.Unmarshal(rawProds, &products)
			}
			buf.WriteString(`<div class="ssr-eanpage">`)
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

// buildSEOURL builds the SEO URL for a EAN page.
func buildSEOURL(sp *model.EANPage, treePath []string) string {
	parts := append(treePath, sp.Slug)
	return "/shop/" + strings.Join(parts, "/")
}

type SeoSlugEAN struct {
	Slug       string `json:"slug"`
	CategoryID int64  `json:"category_id"`
}

// --- Request types ---

type CreateLandingRequest struct {
	EAN         string   `json:"ean"`
	Slug        string   `json:"slug,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Content     string   `json:"content,omitempty"`
	Images      []string `json:"images,omitempty"`
	IsActive    bool     `json:"is_active"`
	ProductIDs  []int64  `json:"product_ids,omitempty"`
}

type UpdateLandingRequest struct {
	EAN         *string   `json:"ean,omitempty"`
	Slug        *string   `json:"slug,omitempty"`
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	Content     *string   `json:"content,omitempty"`
	Images      *[]string `json:"images,omitempty"`
	IsActive    *bool     `json:"is_active,omitempty"`
	ProductIDs  *[]int64  `json:"product_ids,omitempty"`
}

// limitStrings truncates a slice to at most maxLen elements.
func limitStrings(s []string, maxLen int) []string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
