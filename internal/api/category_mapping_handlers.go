package api

import (
	"fmt"
	"net/http"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// --- Category Mapping: explicit source→target category rules ---
// Admin endpoints for managing category mapping rules and scanning price files.

// HandleCategoryMappingsList handles GET /admin/category-mappings (admin).
func (h *Handlers) HandleCategoryMappingsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	mappings, err := h.categoryMappingRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if mappings == nil {
		mappings = []model.CategoryMapping{}
	}
	httpres.WriteJSON(w, http.StatusOK, mappings)
}

// HandleCategoryMappingCreate handles POST /admin/category-mappings (admin).
func (h *Handlers) HandleCategoryMappingCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var m model.CategoryMapping
	if !httpres.ReadJSON(w, r, &m) {
		return
	}
	if err := h.categoryMappingRepo.Create(&m); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	created, _ := h.categoryMappingRepo.Get(m.ID)
	httpres.WriteJSON(w, http.StatusCreated, created)
}

// HandleCategoryMappingGet handles GET /admin/category-mappings/{id} (admin).
func (h *Handlers) HandleCategoryMappingGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "mapping_id")
	if !ok {
		return
	}
	m, err := h.categoryMappingRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, m)
}

// HandleCategoryMappingUpdate handles PATCH /admin/category-mappings/{id} (admin).
func (h *Handlers) HandleCategoryMappingUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "mapping_id")
	if !ok {
		return
	}
	var body model.CategoryMapping
	if !httpres.ReadJSON(w, r, &body) {
		return
	}
	if err := h.categoryMappingRepo.Update(id, func(m *model.CategoryMapping) {
		m.SourceCode = body.SourceCode
		m.TargetCategoryID = body.TargetCategoryID
		m.CompanyID = body.CompanyID
	}); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.categoryMappingRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, updated)
}

// HandleCategoryMappingDelete handles DELETE /admin/category-mappings/{id} (admin).
func (h *Handlers) HandleCategoryMappingDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "mapping_id")
	if !ok {
		return
	}
	if err := h.categoryMappingRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// HandleCategoryMappingsScan handles POST /admin/category-mappings/scan (admin).
// Scans all products and suggests category mappings based on ShopCategory distribution.
func (h *Handlers) HandleCategoryMappingsScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	suggestions, err := h.categoryMappingRepo.ScanPriceFiles(h.productRepo)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Convert map to slice for easier frontend handling
	var result []db.ScanSuggestion
	for _, s := range suggestions {
		result = append(result, *s)
	}
	httpres.WriteJSON(w, http.StatusOK, result)
}

// HandleCategoryMappingsExport handles GET /admin/category-mappings/export (admin).
func (h *Handlers) HandleCategoryMappingsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	mappings, err := h.categoryMappingRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="category-mappings.json"`)
	httpres.WriteJSON(w, http.StatusOK, mappings)
}

// HandleCategoryMappingsImport handles POST /admin/category-mappings/import (admin).
func (h *Handlers) HandleCategoryMappingsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var mappings []model.CategoryMapping
	if !httpres.ReadJSON(w, r, &mappings) {
		return
	}
	imported := 0
	for _, m := range mappings {
		m.ID = 0 // always create new
		if err := h.categoryMappingRepo.Create(&m); err != nil {
			fmt.Printf("[IMPORT-MAPPINGS] WARN: create mapping %s: %v\n", m.SourceCode, err)
			continue
		}
		imported++
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]int{"imported": imported})
}

// HandleCategoryMappingsApplyScan handles POST /admin/category-mappings/apply-scan (admin).
// Takes scan suggestions and creates mappings for all of them at once.
func (h *Handlers) HandleCategoryMappingsApplyScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var suggestions []db.ScanSuggestion
	if !httpres.ReadJSON(w, r, &suggestions) {
		return
	}
	applied := 0
	for _, s := range suggestions {
		mapping := &model.CategoryMapping{
			SourceCode:       s.SourceCode,
			TargetCategoryID: s.TopCategoryID,
		}
		if err := h.categoryMappingRepo.Create(mapping); err != nil {
			fmt.Printf("[APPLY-SCAN] WARN: create mapping %s: %v\n", s.SourceCode, err)
			continue
		}
		applied++
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]int{"applied": applied})
}
