package router

import (
	"net/http"
)

// handlers_seo.go holds the per-route handler methods for the SEO
// structured-data (JSON-LD) settings routes.

// GET/PUT /admin/seo/settings (admin)
func (d *Deps) adminSeoSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleSEOSettingsGet(w, r)
	case http.MethodPut:
		d.Handlers.HandleSEOSettingsUpdate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /admin/seo/export (admin)
func (d *Deps) adminSeoExport(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminSEOExport(w, r)
}

// POST /admin/seo/import (admin)
func (d *Deps) adminSeoImport(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminSEOImport(w, r)
}
