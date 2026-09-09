package router

import (
	"net/http"
)

// handlers_category_mapping.go holds the per-route handler methods for the
// category mapping routes (explicit source→target category rules).

// GET/POST /admin/category-mappings (admin)
func (d *Deps) adminCategoryMappings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleCategoryMappingsList(w, r)
	case http.MethodPost:
		d.Handlers.HandleCategoryMappingCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET/PATCH/DELETE /admin/category-mappings/{id} (admin)
func (d *Deps) adminCategoryMapping(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleCategoryMappingGet(w, r)
	case http.MethodPatch:
		d.Handlers.HandleCategoryMappingUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleCategoryMappingDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /admin/category-mappings/scan (admin)
func (d *Deps) adminCategoryMappingsScan(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleCategoryMappingsScan(w, r)
}

// POST /admin/category-mappings/apply-scan (admin)
func (d *Deps) adminCategoryMappingsApplyScan(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleCategoryMappingsApplyScan(w, r)
}

// GET /admin/category-mappings/export (admin)
func (d *Deps) adminCategoryMappingsExport(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleCategoryMappingsExport(w, r)
}

// POST /admin/category-mappings/import (admin)
func (d *Deps) adminCategoryMappingsImport(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleCategoryMappingsImport(w, r)
}

// DELETE /admin/category-mappings/clear-all (admin)
func (d *Deps) adminCategoryMappingsClearAll(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleCategoryMappingsClearAll(w, r)
}
