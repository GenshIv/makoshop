package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// HandleCompanyLanding returns a company's landing page data (public).
// GET /company/{slug}
// Returns company info, multilang description, hero image, and live stats.
func (h *Handlers) HandleCompanyLanding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract slug from path: /company/{slug}
	path := r.URL.Path
	prefix := "/company/"
	if !strings.HasPrefix(path, prefix) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	slug := strings.TrimPrefix(path, prefix)
	if slug == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing slug")
		return
	}

	company, err := h.companyRepo.GetBySlug(slug)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	if !company.IsVisible {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	// Live stats: total products + sample (cheapest first)
	stats := map[string]interface{}{}
	var sampleProducts []model.Product
	if h.turboSearch != nil {
		result, err := h.turboSearch.ListWithTurbo(db.TurboListParams{
			CompanyID: company.ID,
			Sort:      "price_asc",
			Page:      1,
			Limit:     12,
		})
		if err == nil && result != nil {
			stats["product_count"] = result.Total
			// Decode sample products from raw JSON
			for _, raw := range result.Items {
				var p model.Product
				if err := json.Unmarshal(raw, &p); err == nil {
					sampleProducts = append(sampleProducts, p)
				}
			}
		}
	}
	if sampleProducts == nil {
		sampleProducts = []model.Product{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"company": map[string]interface{}{
			"id":          company.ID,
			"name":        company.Name,
			"name_ru":     company.NameRu,
			"name_ua":     company.NameUa,
			"name_pl":     company.NamePl,
			"name_en":     company.NameEn,
			"slug":        company.Slug,
			"logo_url":    company.LogoURL,
			"website_url": company.WebsiteURL,
			"desc_ru":     company.DescRu,
			"desc_ua":     company.DescUa,
			"desc_pl":     company.DescPl,
			"desc_en":     company.DescEn,
			"hero_image":  company.HeroImage,
			"currency":    company.Settings.Currency,
		},
		"stats":    stats,
		"products": sampleProducts,
	})
}
