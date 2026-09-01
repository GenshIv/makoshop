package api

import (
	"net/http"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// --- Branding: page decoration system ---
// Design: docs/BRANDING_SYSTEM_PLAN.md.
//
// Public read:  GET /branding/active
// Admin write:  /admin/branding/sets, /admin/branding/category-overrides

// HandleBrandingActive handles GET /branding/active (public).
// Returns only enabled sets plus all category overrides and the data
// version, so clients can detect stale caches.
func (h *Handlers) HandleBrandingActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	sets, err := h.brandingRepo.ListSets()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	enabled := make([]model.BrandSet, 0, len(sets))
	for _, s := range sets {
		if s.Enabled {
			enabled = append(enabled, s)
		}
	}
	overrides, err := h.brandingRepo.ListCatThemes()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if overrides == nil {
		overrides = []model.BrandCategoryTheme{}
	}
	httpres.WriteJSON(w, http.StatusOK, model.BrandingActivePayload{
		Version:           h.brandingRepo.GetVersion(),
		Sets:              enabled,
		CategoryOverrides: overrides,
	})
}

// --- Admin: brand sets ---

// HandleBrandingSetsList handles GET /admin/branding/sets (admin).
func (h *Handlers) HandleBrandingSetsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	sets, err := h.brandingRepo.ListSets()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if sets == nil {
		sets = []model.BrandSet{}
	}
	httpres.WriteJSON(w, http.StatusOK, sets)
}

// HandleBrandingSetCreate handles POST /admin/branding/sets (admin).
func (h *Handlers) HandleBrandingSetCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var s model.BrandSet
	if !httpres.ReadJSON(w, r, &s) {
		return
	}
	if err := h.brandingRepo.CreateSet(&s); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	_ = h.brandingRepo.BumpVersion()
	created, _ := h.brandingRepo.GetSet(s.ID)
	httpres.WriteJSON(w, http.StatusCreated, created)
}

// HandleBrandingSetGet handles GET /admin/branding/sets/{id} (admin).
func (h *Handlers) HandleBrandingSetGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "brand_set_id")
	if !ok {
		return
	}
	s, err := h.brandingRepo.GetSet(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, s)
}

// HandleBrandingSetUpdate handles PATCH /admin/branding/sets/{id} (admin).
// Full-replace semantics: the body carries the complete set state
// (name, description, priority, enabled, elements).
func (h *Handlers) HandleBrandingSetUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "brand_set_id")
	if !ok {
		return
	}
	var body model.BrandSet
	if !httpres.ReadJSON(w, r, &body) {
		return
	}
	if err := h.brandingRepo.UpdateSet(id, func(s *model.BrandSet) {
		s.Name = body.Name
		s.Description = body.Description
		s.Priority = body.Priority
		s.Enabled = body.Enabled
		s.Elements = body.Elements
	}); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	_ = h.brandingRepo.BumpVersion()
	updated, _ := h.brandingRepo.GetSet(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

// HandleBrandingSetDelete handles DELETE /admin/branding/sets/{id} (admin).
func (h *Handlers) HandleBrandingSetDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "brand_set_id")
	if !ok {
		return
	}
	if err := h.brandingRepo.DeleteSet(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	_ = h.brandingRepo.BumpVersion()
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Admin: category overrides ---

// HandleBrandingCatThemesList handles GET /admin/branding/category-overrides (admin).
func (h *Handlers) HandleBrandingCatThemesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	themes, err := h.brandingRepo.ListCatThemes()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if themes == nil {
		themes = []model.BrandCategoryTheme{}
	}
	httpres.WriteJSON(w, http.StatusOK, themes)
}

// HandleBrandingCatThemeUpsert handles POST /admin/branding/category-overrides (admin).
// Upsert by (category_id, slot).
func (h *Handlers) HandleBrandingCatThemeUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var t model.BrandCategoryTheme
	if !httpres.ReadJSON(w, r, &t) {
		return
	}
	if err := h.brandingRepo.UpsertCatTheme(&t); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	_ = h.brandingRepo.BumpVersion()
	saved, _ := h.brandingRepo.GetCatTheme(t.CategoryID, t.Slot)
	httpres.WriteJSON(w, http.StatusOK, saved)
}

// HandleBrandingCatThemeDelete handles
// DELETE /admin/branding/category-overrides?category_id=&slot= (admin).
func (h *Handlers) HandleBrandingCatThemeDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	catIDStr := r.URL.Query().Get("category_id")
	slot := model.BrandSlot(r.URL.Query().Get("slot"))
	if catIDStr == "" || !model.BrandSlotValid(slot) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "category_id and slot query params are required")
		return
	}
	catID, err := parseQueryInt64(catIDStr)
	if err != nil || catID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid category_id")
		return
	}
	if err := h.brandingRepo.DeleteCatTheme(catID, slot); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	_ = h.brandingRepo.BumpVersion()
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
