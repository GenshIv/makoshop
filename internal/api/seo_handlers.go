package api

import (
	"net/http"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// --- SEO structured data (JSON-LD) settings ---
//
// Admin read/write:
//   GET /admin/seo/settings
//   PUT /admin/seo/settings
//
// The settings drive the schema.org JSON-LD blocks injected into every
// landing page (see jsonld.go and writeHTMLResponseEANList).

// HandleSEOSettingsGet handles GET /admin/seo/settings (admin).
func (h *Handlers) HandleSEOSettingsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	s, err := h.seoRepo.GetSettings()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, s)
}

// HandleSEOSettingsUpdate handles PUT /admin/seo/settings (admin). Full-replace
// semantics: the body carries the complete settings object.
func (h *Handlers) HandleSEOSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var s model.SEOSettings
	if !httpres.ReadJSON(w, r, &s) {
		return
	}
	if err := h.seoRepo.SaveSettings(&s); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	// Return the stored (normalized) settings, including the server-set
	// UpdatedAt timestamp.
	updated, err := h.seoRepo.GetSettings()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, updated)
}
