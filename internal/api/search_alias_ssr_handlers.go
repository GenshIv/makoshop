package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
)

// HandleSearchAliasSSR handles /search/{slug} with SSR — renders HTML page
// with SEO meta tags and initial data for bots and users. Returns JSON for
// API requests (SPA navigation) based on Accept header.
func (h *Handlers) HandleSearchAliasSSR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path: /search/{slug}
	path := r.URL.Path
	prefix := "/search/"
	slug := ""
	if len(path) > len(prefix) {
		slug = path[len(prefix):]
	}

	if slug == "" {
		http.NotFound(w, r)
		return
	}

	// Get alias by slug
	alias, err := h.searchAliasRepo.GetBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// If client wants JSON (SPA navigation), return JSON response
	if !wantsHTML(r) {
		h.handleSearchAliasJSON(w, r, alias, slug)
		return
	}

	// Resolve category slug to ID if provided
	var catID int64
	if alias.CategorySlug != "" {
		parts := strings.Split(alias.CategorySlug, "/")
		resolvedCatID, err := h.findCategoryByPath(parts)
		if err == nil {
			catID = resolvedCatID
		}
	}

	// Parse pagination from request
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		p, _ := strconv.Atoi(pageStr)
		if p < 1 {
			p = 1
		}
		page = p
	}

	limit := 50
	limitParam := r.URL.Query().Get("limit")
	if limitParam == "" {
		limitParam = r.URL.Query().Get("per_page")
	}
	if limitParam != "" {
		l, _ := strconv.Atoi(limitParam)
		if l < 1 {
			l = 1
		}
		if l > 200 {
			l = 200
		}
		limit = l
	}

	// Build search params from alias + user query params
	params := db.EANPageListParams{
		Q:          alias.SearchQuery,
		CategoryID: catID,
		Sort:       alias.SortOrder,
		Page:       page,
		Limit:      limit,
	}

	if alias.AttrFilters != nil {
		params.AttrFilters = alias.AttrFilters
	} else {
		params.AttrFilters = make(map[string][]string)
	}

	if alias.PriceMin != nil {
		params.PriceMin = *alias.PriceMin
	}
	if alias.PriceMax != nil {
		params.PriceMax = *alias.PriceMax
	}

	// Track user-provided params separately for search_params_json
	userSearchParams := map[string]interface{}{}

	// Override with user query params if provided
	if sortParam := r.URL.Query().Get("sort"); sortParam != "" {
		params.Sort = sortParam
		userSearchParams["sort"] = sortParam
	}
	if priceMinStr := r.URL.Query().Get("price_min"); priceMinStr != "" {
		if pm, err := strconv.ParseFloat(priceMinStr, 64); err == nil {
			params.PriceMin = pm
			userSearchParams["price_min"] = pm
		}
	}
	if priceMaxStr := r.URL.Query().Get("price_max"); priceMaxStr != "" {
		if pm, err := strconv.ParseFloat(priceMaxStr, 64); err == nil {
			params.PriceMax = pm
			userSearchParams["price_max"] = pm
		}
	}

	// Parse attr filters from query params: attr.<code>=value1,value2 or attr_<code>=value1,value2
	for key, values := range r.URL.Query() {
		code := ""
		if strings.HasPrefix(key, "attr.") {
			code = strings.TrimPrefix(key, "attr.")
		} else if strings.HasPrefix(key, "attr_") {
			code = strings.TrimPrefix(key, "attr_")
		}
		if code != "" {
			params.AttrFilters[code] = values
			userSearchParams["attr_"+code] = values
		}
	}

	// Build category filter attributes (cached) — same as /shop endpoint
	var categoryAttrs []db.AttrItem
	if catID > 0 {
		categoryAttrs = h.GetCategoryAttrs(catID)
	}

	// Perform search using EANPageSearch (same as /shop endpoint)
	var respData db.EANListRespData
	if h.eanPageSearch != nil {
		searchResult, err := h.eanPageSearch.ListWithTurbo(params)
		if err == nil && searchResult != nil {
			respData = db.EANListRespData{
				Items:         searchResult.Items,
				Total:         searchResult.Total,
				Page:          searchResult.Page,
				Limit:         searchResult.Limit,
				CategoryAttrs: categoryAttrs,
			}
		}
	}

	// Add category info for breadcrumbs, filters, and UI (same as /shop endpoint)
	if catID > 0 {
		respData.CatID = catID
		if treePath, err := h.categoryRepo.GetTreePath(catID); err == nil && len(treePath) > 0 {
			respData.TreePath = treePath
			respData.SEOURL = "/search/" + slug // Keep search alias URL as canonical
		}
		if cat, err := h.categoryRepo.Get(catID); err == nil {
			respData.Category = *cat
			if subsJSON, err := h.categoryRepo.GetTreeByParentJSON(catID); err == nil && len(subsJSON) > 0 {
				respData.Subcategories = silentjson.RawMessage(subsJSON)
			}
		}
	}

	// Only pass user-provided search params to frontend (not alias defaults)
	if len(userSearchParams) > 0 {
		paramsJSON, _ := json.Marshal(userSearchParams)
		respData.SearchParamsJSON = string(paramsJSON)
	}

	// Build SEO title and description
	seoTitle := alias.SEOTitle
	if seoTitle == "" {
		seoTitle = alias.Title
	}
	seoDesc := alias.SEODescription
	if seoDesc == "" {
		seoDesc = alias.Description
	}

	// Use writeHTMLResponseEANList (same as /shop endpoint) for proper item serialization
	writeHTMLResponseEANList(w, r, seoTitle, h.siteBaseURL(), respData, h.seoSettings(), nil)
}

// handleSearchAliasJSON returns JSON response for SPA navigation requests.
func (h *Handlers) handleSearchAliasJSON(w http.ResponseWriter, r *http.Request, alias *model.SearchAlias, slug string) {
	// Resolve category slug to ID if provided
	var catID int64
	if alias.CategorySlug != "" {
		parts := strings.Split(alias.CategorySlug, "/")
		resolvedCatID, err := h.findCategoryByPath(parts)
		if err == nil {
			catID = resolvedCatID
		}
	}

	// Parse pagination from request
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		p, _ := strconv.Atoi(pageStr)
		if p < 1 {
			p = 1
		}
		page = p
	}

	limit := 50
	limitParam := r.URL.Query().Get("limit")
	if limitParam == "" {
		limitParam = r.URL.Query().Get("per_page")
	}
	if limitParam != "" {
		l, _ := strconv.Atoi(limitParam)
		if l < 1 {
			l = 1
		}
		if l > 200 {
			l = 200
		}
		limit = l
	}

	// Build search params from alias + user query params
	params := db.EANPageListParams{
		Q:          alias.SearchQuery,
		CategoryID: catID,
		Sort:       alias.SortOrder,
		Page:       page,
		Limit:      limit,
	}

	if alias.AttrFilters != nil {
		params.AttrFilters = alias.AttrFilters
	} else {
		params.AttrFilters = make(map[string][]string)
	}

	if alias.PriceMin != nil {
		params.PriceMin = *alias.PriceMin
	}
	if alias.PriceMax != nil {
		params.PriceMax = *alias.PriceMax
	}

	// Override with user query params if provided
	if sortParam := r.URL.Query().Get("sort"); sortParam != "" {
		params.Sort = sortParam
	}
	if priceMinStr := r.URL.Query().Get("price_min"); priceMinStr != "" {
		if pm, err := strconv.ParseFloat(priceMinStr, 64); err == nil {
			params.PriceMin = pm
		}
	}
	if priceMaxStr := r.URL.Query().Get("price_max"); priceMaxStr != "" {
		if pm, err := strconv.ParseFloat(priceMaxStr, 64); err == nil {
			params.PriceMax = pm
		}
	}

	// Parse attr filters from query params: attr.<code>=value1,value2 or attr_<code>=value1,value2
	for key, values := range r.URL.Query() {
		code := ""
		if strings.HasPrefix(key, "attr.") {
			code = strings.TrimPrefix(key, "attr.")
		} else if strings.HasPrefix(key, "attr_") {
			code = strings.TrimPrefix(key, "attr_")
		}
		// Handle bracket notation for arrays (axios default): attr.code[] -> attr.code
		code = strings.TrimSuffix(code, "[]")
		if code != "" {
			params.AttrFilters[code] = values
		}
	}

	// Build category filter attributes (cached) — same as /shop endpoint
	var categoryAttrs []db.AttrItem
	if catID > 0 {
		categoryAttrs = h.GetCategoryAttrs(catID)
	}

	// Perform search
	var respData db.EANListRespData
	if h.eanPageSearch != nil {
		searchResult, err := h.eanPageSearch.ListWithTurbo(params)
		if err == nil && searchResult != nil {
			respData = db.EANListRespData{
				Items:         searchResult.Items,
				Total:         searchResult.Total,
				Page:          searchResult.Page,
				Limit:         searchResult.Limit,
				CategoryAttrs: categoryAttrs,
			}
		}
	}

	// Return JSON response using proper serialization (same as /shop endpoint)
	writeJSONEANList(w, r, http.StatusOK, respData)
}
