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
// Supports ?source= filter to find mappings for a specific source code.
func (h *Handlers) HandleCategoryMappingsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	sourceFilter := r.URL.Query().Get("source")
	var mappings []model.CategoryMapping
	var err error

	if sourceFilter != "" {
		// Filter by source code
		mapping, err := h.categoryMappingRepo.FindBySourceCode(sourceFilter, nil)
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if mapping != nil {
			mappings = []model.CategoryMapping{*mapping}
		} else {
			mappings = []model.CategoryMapping{}
		}
	} else {
		mappings, err = h.categoryMappingRepo.List()
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if mappings == nil {
			mappings = []model.CategoryMapping{}
		}
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
	if body.SourceCode == "" || body.TargetCategoryID == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "source_code and target_category_id required")
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
// Exports in import-compatible format (source_code + target_category_id only).
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

	// Export only source_code and target_category_id for clean import
	export := make([]map[string]interface{}, 0, len(mappings))
	for _, m := range mappings {
		export = append(export, map[string]interface{}{
			"source_code":        m.SourceCode,
			"target_category_id": m.TargetCategoryID,
		})
	}

	w.Header().Set("Content-Disposition", `attachment; filename="category-mappings.json"`)
	httpres.WriteJSON(w, http.StatusOK, export)
}

// HandleCategoryMappingsImport handles POST /admin/category-mappings/import (admin).
// Accepts both standard mapping format and scan result format (top_category_id).
func (h *Handlers) HandleCategoryMappingsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Read raw JSON to handle both formats
	var raw []map[string]interface{}
	if !httpres.ReadJSON(w, r, &raw) {
		return
	}

	// Load existing mappings to avoid duplicates
	existing, err := h.categoryMappingRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	existingCodes := make(map[string]bool)
	for _, m := range existing {
		existingCodes[m.SourceCode] = true
	}

	imported := 0
	skipped := 0
	var errors []string
	for _, item := range raw {
		sourceCode, _ := item["source_code"].(string)
		if sourceCode == "" {
			errors = append(errors, "missing source_code")
			continue
		}

		// Handle both target_category_id and top_category_id (scan format)
		var targetID int64
		if v, ok := item["target_category_id"]; ok {
			targetID = int64(v.(float64))
		} else if v, ok := item["top_category_id"]; ok {
			targetID = int64(v.(float64))
		}
		if targetID == 0 {
			errors = append(errors, fmt.Sprintf("missing target_category_id for %s", sourceCode))
			continue
		}

		if existingCodes[sourceCode] {
			skipped++
			continue
		}

		mapping := &model.CategoryMapping{
			SourceCode:       sourceCode,
			TargetCategoryID: targetID,
		}
		if err := h.categoryMappingRepo.Create(mapping); err != nil {
			errors = append(errors, fmt.Sprintf("create %s: %v", sourceCode, err))
			continue
		}
		imported++
	}

	result := map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
	}
	if len(errors) > 0 {
		result["errors"] = errors
	}
	httpres.WriteJSON(w, http.StatusOK, result)
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

// HandleCategoryMappingsClearAll handles DELETE /admin/category-mappings/clear-all (admin).
// Deletes all category mappings.
func (h *Handlers) HandleCategoryMappingsClearAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	mappings, err := h.categoryMappingRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	deleted := 0
	for _, m := range mappings {
		if err := h.categoryMappingRepo.Delete(m.ID); err == nil {
			deleted++
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]int{"deleted": deleted})
}
