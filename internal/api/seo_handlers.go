package api

import (
	"encoding/json"
	"net/http"
	"time"

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

// --- Admin: SEO export/import ---

// HandleAdminSEOExport handles GET /admin/seo/export.
func (h *Handlers) HandleAdminSEOExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	s, err := h.seoRepo.GetSettings()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	payload := map[string]interface{}{
		"exported_at":  time.Now().UTC().Format(time.RFC3339),
		"seo_settings": s,
	}

	httpres.WriteJSON(w, http.StatusOK, payload)
}

// HandleAdminSEOImport handles POST /admin/seo/import.
func (h *Handlers) HandleAdminSEOImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var payload struct {
		SEOSettings model.SEOSettings `json:"seo_settings"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}

	if err := model.ValidateSEOSettings(&payload.SEOSettings); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	payload.SEOSettings.UpdatedAt = time.Now().Unix()
	if err := h.seoRepo.SaveSettings(&payload.SEOSettings); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "imported"})
}
