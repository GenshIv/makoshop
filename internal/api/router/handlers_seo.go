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
