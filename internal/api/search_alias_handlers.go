package api

import (
	"net/http"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// --- Search Alias handlers ---

// HandleSearchAliasesList returns all search aliases (admin).
// GET /admin/search-aliases
func (h *Handlers) HandleSearchAliasesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	aliases, err := h.searchAliasRepo.ListAll()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if aliases == nil {
		aliases = []model.SearchAlias{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": aliases,
	})
}

// HandleSearchAliasGet returns a search alias by ID (admin).
// GET /admin/search-aliases/{id}
func (h *Handlers) HandleSearchAliasGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "search_alias_id")
	if !ok {
		return
	}

	a, err := h.searchAliasRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "search alias not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, a)
}

// HandleSearchAliasCreate creates a new search alias (admin).
// POST /admin/search-aliases
func (h *Handlers) HandleSearchAliasCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req model.SearchAlias
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if err := h.searchAliasRepo.Create(&req); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, req)
}

// HandleSearchAliasUpdate updates a search alias (admin).
// PATCH /admin/search-aliases/{id}
func (h *Handlers) HandleSearchAliasUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "search_alias_id")
	if !ok {
		return
	}

	var req model.SearchAlias
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	err := h.searchAliasRepo.Update(id, func(a *model.SearchAlias) {
		a.Slug = req.Slug
		a.Title = req.Title
		a.Description = req.Description
		a.SEOTitle = req.SEOTitle
		a.SEODescription = req.SEODescription
		a.OGImage = req.OGImage
		a.JSONLD = req.JSONLD
		a.SearchQuery = req.SearchQuery
		a.CategorySlug = req.CategorySlug
		a.PriceMin = req.PriceMin
		a.PriceMax = req.PriceMax
		a.AttrFilters = req.AttrFilters
		a.SortOrder = req.SortOrder
		a.IsActive = req.IsActive
	})
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	updated, _ := h.searchAliasRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

// HandleSearchAliasDelete deletes a search alias (admin).
// DELETE /admin/search-aliases/{id}
func (h *Handlers) HandleSearchAliasDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "search_alias_id")
	if !ok {
		return
	}

	if err := h.searchAliasRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// HandleSearchAliasBySlug returns a search alias by slug (public).
// GET /search-aliases/{slug}
func (h *Handlers) HandleSearchAliasBySlug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract slug from path: /search-aliases/{slug}
	path := r.URL.Path
	prefix := "/search-aliases/"
	slug := ""
	if len(path) > len(prefix) {
		slug = path[len(prefix):]
	}

	if slug == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "slug is required")
		return
	}

	a, err := h.searchAliasRepo.GetBySlug(slug)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "search alias not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, a)
}
