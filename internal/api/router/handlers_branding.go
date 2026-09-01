package router

import (
	"net/http"
)

// handlers_branding.go holds the per-route handler methods for the branding
// (page decoration) routes. See docs/BRANDING_SYSTEM_PLAN.md.

// GET /branding/active — public: enabled sets + category overrides + version.
func (d *Deps) brandingActive(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleBrandingActive(w, r)
}

// GET/POST /admin/branding/sets (admin)
func (d *Deps) adminBrandingSets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleBrandingSetsList(w, r)
	case http.MethodPost:
		d.Handlers.HandleBrandingSetCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET/PATCH/DELETE /admin/branding/sets/{id} (admin)
func (d *Deps) adminBrandingSet(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleBrandingSetGet(w, r)
	case http.MethodPatch:
		d.Handlers.HandleBrandingSetUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleBrandingSetDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET/POST/DELETE /admin/branding/category-overrides (admin)
// DELETE takes ?category_id=&slot= query params (identity is the pair).
func (d *Deps) adminBrandingCatThemes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleBrandingCatThemesList(w, r)
	case http.MethodPost:
		d.Handlers.HandleBrandingCatThemeUpsert(w, r)
	case http.MethodDelete:
		d.Handlers.HandleBrandingCatThemeDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
