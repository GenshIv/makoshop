package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// HandleAdminCompanyExport exports a company's configuration as JSON.
// GET /admin/companies/{id}/export
func (h *AuthHandlers) HandleAdminCompanyExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /admin/companies/{id}/export
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "admin", "companies", "{id}", "export"]
	if len(parts) < 5 || parts[4] != "export" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company id")
		return
	}

	c, err := h.companyRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	// Export only the configuration fields (not IDs, not owner)
	export := map[string]interface{}{
		"name":          c.Name,
		"name_ru":       c.NameRu,
		"name_ua":       c.NameUa,
		"name_pl":       c.NamePl,
		"name_en":       c.NameEn,
		"slug":          c.Slug,
		"description":   c.Description,
		"logo_url":      c.LogoURL,
		"import_folder": c.ImportFolder,
		"price_source":  c.PriceSource,
		"desc_ru":       c.DescRu,
		"desc_ua":       c.DescUa,
		"desc_pl":       c.DescPl,
		"desc_en":       c.DescEn,
		"hero_image":    c.HeroImage,
		"is_visible":    c.IsVisible,
		"currency":      c.Settings.Currency,
		"vat_enabled":   c.Settings.VatEnabled,
	}

	httpres.WriteJSON(w, http.StatusOK, export)
}

// companyExportDTO is the structure for bulk export/import of companies.
type companyExportDTO struct {
	Name         string                   `json:"name"`
	NameRu       string                   `json:"name_ru,omitempty"`
	NameUa       string                   `json:"name_ua,omitempty"`
	NamePl       string                   `json:"name_pl,omitempty"`
	NameEn       string                   `json:"name_en,omitempty"`
	Slug         string                   `json:"slug,omitempty"`
	Description  string                   `json:"description,omitempty"`
	LogoURL      string                   `json:"logo_url,omitempty"`
	WebsiteURL   string                   `json:"website_url,omitempty"`
	ImportFolder string                   `json:"import_folder,omitempty"`
	PriceSource  *model.PriceSourceConfig `json:"price_source,omitempty"`
	DescRu       string                   `json:"desc_ru,omitempty"`
	DescUa       string                   `json:"desc_ua,omitempty"`
	DescPl       string                   `json:"desc_pl,omitempty"`
	DescEn       string                   `json:"desc_en,omitempty"`
	HeroImage    string                   `json:"hero_image,omitempty"`
	IsVisible    bool                     `json:"is_visible"`
	Currency     string                   `json:"currency,omitempty"`
	VatEnabled   bool                     `json:"vat_enabled"`
	OwnerUserID  int64                    `json:"owner_user_id,omitempty"`
}

func companyToExportDTO(c *model.Company) companyExportDTO {
	ps := c.PriceSource
	return companyExportDTO{
		Name:         c.Name,
		NameRu:       c.NameRu,
		NameUa:       c.NameUa,
		NamePl:       c.NamePl,
		NameEn:       c.NameEn,
		Slug:         c.Slug,
		Description:  c.Description,
		LogoURL:      c.LogoURL,
		WebsiteURL:   c.WebsiteURL,
		ImportFolder: c.ImportFolder,
		PriceSource:  &ps,
		DescRu:       c.DescRu,
		DescUa:       c.DescUa,
		DescPl:       c.DescPl,
		DescEn:       c.DescEn,
		HeroImage:    c.HeroImage,
		IsVisible:    c.IsVisible,
		Currency:     c.Settings.Currency,
		VatEnabled:   c.Settings.VatEnabled,
		OwnerUserID:  c.OwnerUserID,
	}
}

// HandleAdminCompaniesExportAll exports all companies as JSON.
// GET /admin/companies/export-all
func (h *AuthHandlers) HandleAdminCompaniesExportAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	companies, err := h.companyRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	items := make([]companyExportDTO, 0, len(companies))
	for i := range companies {
		items = append(items, companyToExportDTO(&companies[i]))
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"companies": items,
		"total":     len(items),
	})
}

// HandleAdminCompaniesImportAll imports all companies from JSON.
// POST /admin/companies/import-all
// Body: {"companies": [...], "owner_user_id": N} (owner_user_id used for new companies without it)
func (h *AuthHandlers) HandleAdminCompaniesImportAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		Companies   []companyExportDTO `json:"companies"`
		OwnerUserID int64              `json:"owner_user_id,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if len(req.Companies) == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "no companies to import")
		return
	}

	companies, _ := h.companyRepo.List()
	created, updated := 0, 0

	for _, dto := range req.Companies {
		if dto.Name == "" {
			continue
		}
		// Find existing by name
		var target *model.Company
		for i := range companies {
			if strings.EqualFold(companies[i].Name, dto.Name) {
				target = &companies[i]
				break
			}
		}

		if target == nil {
			ownerID := dto.OwnerUserID
			if ownerID == 0 {
				ownerID = req.OwnerUserID
			}
			if ownerID == 0 {
				httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "owner_user_id is required for new companies")
				return
			}
			target = &model.Company{
				Name:        dto.Name,
				Slug:        dto.Slug,
				OwnerUserID: ownerID,
				Status:      model.CompanyStatusVerified,
			}
			if err := h.companyRepo.Create(target); err != nil {
				httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
			created++
		}

		if err := h.companyRepo.Update(target.ID, func(c *model.Company) {
			if dto.NameRu != "" {
				c.NameRu = dto.NameRu
			}
			if dto.NameUa != "" {
				c.NameUa = dto.NameUa
			}
			if dto.NamePl != "" {
				c.NamePl = dto.NamePl
			}
			if dto.NameEn != "" {
				c.NameEn = dto.NameEn
			}
			if dto.Description != "" {
				c.Description = dto.Description
			}
			if dto.LogoURL != "" {
				c.LogoURL = dto.LogoURL
			}
			if dto.WebsiteURL != "" {
				c.WebsiteURL = dto.WebsiteURL
			}
			if dto.ImportFolder != "" {
				c.ImportFolder = dto.ImportFolder
			}
			if dto.PriceSource != nil {
				c.PriceSource = *dto.PriceSource
			}
			if dto.DescRu != "" {
				c.DescRu = dto.DescRu
			}
			if dto.DescUa != "" {
				c.DescUa = dto.DescUa
			}
			if dto.DescPl != "" {
				c.DescPl = dto.DescPl
			}
			if dto.DescEn != "" {
				c.DescEn = dto.DescEn
			}
			if dto.HeroImage != "" {
				c.HeroImage = dto.HeroImage
			}
			c.IsVisible = dto.IsVisible
			if dto.Currency != "" {
				c.Settings.Currency = dto.Currency
			}
			c.Settings.VatEnabled = dto.VatEnabled
		}); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		updated++
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"created": created,
		"updated": updated,
	})
}

// HandleAdminCompanyImport imports a company configuration from JSON.
// POST /admin/companies/import
// Body: the export JSON (plus optional "create": true to create if not exists)
func (h *AuthHandlers) HandleAdminCompanyImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		Name         string                   `json:"name"`
		Slug         string                   `json:"slug,omitempty"`
		Description  string                   `json:"description,omitempty"`
		LogoURL      string                   `json:"logo_url,omitempty"`
		WebsiteURL   string                   `json:"website_url,omitempty"`
		ImportFolder string                   `json:"import_folder,omitempty"`
		PriceSource  *model.PriceSourceConfig `json:"price_source,omitempty"`
		DescRu       string                   `json:"desc_ru,omitempty"`
		DescUa       string                   `json:"desc_ua,omitempty"`
		DescPl       string                   `json:"desc_pl,omitempty"`
		DescEn       string                   `json:"desc_en,omitempty"`
		HeroImage    string                   `json:"hero_image,omitempty"`
		IsVisible    bool                     `json:"is_visible"`
		Currency     string                   `json:"currency,omitempty"`
		VatEnabled   bool                     `json:"vat_enabled"`
		OwnerUserID  int64                    `json:"owner_user_id,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.Name == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "name is required")
		return
	}

	// Find existing company by name
	companies, _ := h.companyRepo.List()
	var target *model.Company
	for i := range companies {
		if strings.EqualFold(companies[i].Name, req.Name) {
			target = &companies[i]
			break
		}
	}

	if target == nil {
		// Create new company
		if req.OwnerUserID == 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "owner_user_id is required for new companies")
			return
		}
		target = &model.Company{
			Name:        req.Name,
			Slug:        req.Slug,
			OwnerUserID: req.OwnerUserID,
			Status:      model.CompanyStatusVerified,
		}
		if err := h.companyRepo.Create(target); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}

	// Apply config
	if err := h.companyRepo.Update(target.ID, func(c *model.Company) {
		if req.Description != "" {
			c.Description = req.Description
		}
		if req.LogoURL != "" {
			c.LogoURL = req.LogoURL
		}
		if req.WebsiteURL != "" {
			c.WebsiteURL = req.WebsiteURL
		}
		if req.ImportFolder != "" {
			c.ImportFolder = req.ImportFolder
		}
		if req.PriceSource != nil {
			c.PriceSource = *req.PriceSource
		}
		if req.DescRu != "" {
			c.DescRu = req.DescRu
		}
		if req.DescUa != "" {
			c.DescUa = req.DescUa
		}
		if req.DescPl != "" {
			c.DescPl = req.DescPl
		}
		if req.DescEn != "" {
			c.DescEn = req.DescEn
		}
		if req.HeroImage != "" {
			c.HeroImage = req.HeroImage
		}
		c.IsVisible = req.IsVisible
		if req.Currency != "" {
			c.Settings.Currency = req.Currency
		}
		c.Settings.VatEnabled = req.VatEnabled
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ := h.companyRepo.Get(target.ID)
	httpres.WriteJSON(w, http.StatusOK, c)
}

// HandleAdminPriceSourcesList lists price source configs for all companies.
// Useful for the admin UI to manage import configurations.
// GET /admin/price-sources
func (h *AuthHandlers) HandleAdminPriceSourcesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	companies, err := h.companyRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	type priceSourceItem struct {
		CompanyID    int64                   `json:"company_id"`
		CompanyName  string                  `json:"company_name"`
		ImportFolder string                  `json:"import_folder"`
		PriceSource  model.PriceSourceConfig `json:"price_source"`
	}

	items := make([]priceSourceItem, 0, len(companies))
	for _, c := range companies {
		items = append(items, priceSourceItem{
			CompanyID:    c.ID,
			CompanyName:  c.Name,
			ImportFolder: c.ImportFolder,
			PriceSource:  c.PriceSource,
		})
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"total": len(items),
	})
}
