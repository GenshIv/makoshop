package api

import (
	"fmt"
	"net/http"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// HandleProductAssignCategory handles POST /admin/products/{ean}/assign-category.
// Admin assigns a target category to an EAN; backend finds all products with that
// EAN, collects their company categories, and creates/updates mapping rules so
// future imports of products from any of those company categories go to the target.
func (h *Handlers) HandleProductAssignCategory(w http.ResponseWriter, r *http.Request, ean string) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		TargetCategoryID int64 `json:"target_category_id"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}
	if req.TargetCategoryID == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "target_category_id required")
		return
	}

	// Find ALL products with this EAN
	products, err := h.turboSearch.GetProductsByEAN(ean)
	if err != nil || len(products) == 0 {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no products found for this EAN")
		return
	}

	// Collect unique company categories from all products
	type shopCatKey struct {
		shopCategory string
		companyID    int64
	}
	seen := make(map[shopCatKey]bool)
	var mappingsCreated []int64

	for i := range products {
		p := &products[i]
		if p.ShopCategory == "" {
			continue
		}

		key := shopCatKey{shopCategory: p.ShopCategory, companyID: p.CompanyID}
		if seen[key] {
			continue
		}
		seen[key] = true

		// Check if mapping already exists for this source
		existing, err := h.categoryMappingRepo.FindBySourceCode(p.ShopCategory, &p.CompanyID)
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		var mappingID int64
		if existing != nil {
			// Update existing mapping
			if err := h.categoryMappingRepo.Update(existing.ID, func(m *model.CategoryMapping) {
				m.TargetCategoryID = req.TargetCategoryID
			}); err != nil {
				httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
			mappingID = existing.ID
		} else {
			// Create new mapping
			newMapping := &model.CategoryMapping{
				SourceCode:       p.ShopCategory,
				TargetCategoryID: req.TargetCategoryID,
				CompanyID:        &p.CompanyID,
			}
			if err := h.categoryMappingRepo.Create(newMapping); err != nil {
				httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
			mappingID = newMapping.ID
		}
		mappingsCreated = append(mappingsCreated, mappingID)

		// Update company-product-category reference table
		cpc := &db.CompanyProductCategory{
			CompanyID:        p.CompanyID,
			ProductEAN:       ean,
			TargetCategoryID: req.TargetCategoryID,
		}
		if err := h.companyProductCategoryRepo.Upsert(cpc); err != nil {
			fmt.Printf("[WARN] failed to upsert CPC reference: %v\n", err)
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":             "ok",
		"mappings_created":   len(mappingsCreated),
		"mapping_ids":        mappingsCreated,
		"target_category_id": req.TargetCategoryID,
	})
}

// HandleProductGetCategoryMapping handles GET /admin/products/{ean}/category-mapping.
// Returns the current category mapping for a product's company category.
func (h *Handlers) HandleProductGetCategoryMapping(w http.ResponseWriter, r *http.Request, ean string) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Find a product with this EAN to get company_id and shop_category
	products, err := h.turboSearch.GetProductsByEAN(ean)
	if err != nil || len(products) == 0 {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "no products found for this EAN")
		return
	}

	p := &products[0]

	existing, err := h.categoryMappingRepo.FindBySourceCode(p.ShopCategory, &p.CompanyID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if existing != nil {
		httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"has_mapping":        true,
			"mapping_id":         existing.ID,
			"target_category_id": existing.TargetCategoryID,
		})
	} else {
		httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"has_mapping": false,
		})
	}
}
