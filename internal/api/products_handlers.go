package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
)

func (h *Handlers) HandleTurboProducts(w http.ResponseWriter, r *http.Request) {
	// Support HEAD requests: run full logic but don't send body.
	headOnly := r.Method == http.MethodHead

	params := ParseTurboSearchParams(r)

	result, err := h.turboSearch.ListWithTurbo(db.TurboListParams{
		Q:           params.Q,
		CategoryID:  params.CategoryID,
		CompanyID:   params.CompanyID,
		BrandID:     params.BrandID,
		AttrFilters: params.AttrFilters,
		Sort:        params.Sort,
		Page:        params.Page,
		Limit:       params.Limit,
	})
	if err != nil {
		fmt.Printf("ERROR turbo search: %v\n", err)
		http.Error(w, "turbo search error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := turboListResp{
		Items: result.Items,
		Total: result.Total,
		Page:  result.Page,
		Limit: result.Limit,
	}
	buf := marshalWithPool(&resp, turboListRespReg)
	w.Header().Set("Content-Type", "application/json")
	if !headOnly {
		w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	}
	w.WriteHeader(http.StatusOK)
	if !headOnly {
		_, _ = w.Write(buf)
	}
}

type turboListResp struct {
	Items []silentjson.RawMessage `json:"items"`
	Total int64                   `json:"total"`
	Page  int                     `json:"page"`
	Limit int                     `json:"limit"`
}

var turboListRespReg = silentjson.BuildRegistry(reflect.TypeOf(turboListResp{}))

func (h *Handlers) HandleBrandsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if h.turboSearch == nil {
		httpres.WriteError(w, http.StatusServiceUnavailable, "TURBO_DISABLED", "turbo search is disabled")
		return
	}

	catRaw := r.URL.Query().Get("category_id")
	var catID int64
	if catRaw != "" {
		catID, _ = strconv.ParseInt(catRaw, 10, 64)
	}

	brands, err := h.turboSearch.GetBrands(catID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, brands)
}

// --- Attribute Values (turbo-based) ---

// GET /attributes/{code}/values — returns all values for an attribute code from turbo index.
// Used by frontend to build filter UI.

func (h *Handlers) HandleAttributeValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /attributes/{code}/values
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[len(parts)-1] != "values" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	code := parts[len(parts)-2]
	if code == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "attribute code required")
		return
	}

	// Без category context возвращаем значения из первой найденной категории
	// (этот endpoint устарел, лучше использовать /admin/categories/{id}/attributes)
	values, err := h.attrDefRepo.GetAttrValuesForCategory(code, 0)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"code":   code,
		"values": values,
	})
}

// --- Products ---

type CreateProductRequest struct {
	EAN         string           `json:"ean,omitempty"` // Standard Catalog Unit — links to landing page
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	CategoryID  int64            `json:"category_id"`
	CompanyID   int64            `json:"company_id"`
	Brand       string           `json:"brand,omitempty"`
	Price       float64          `json:"price"`
	Currency    string           `json:"currency"`
	StockQty    int64            `json:"stock_qty"`
	Status      string           `json:"status"`
	Attributes  []model.KeyValue `json:"attributes,omitempty"`
	Images      []string         `json:"images,omitempty"`
	SEO         model.ProductSEO `json:"seo,omitempty"`
}

func (h *Handlers) HandleProductsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))

	catRaw := r.URL.Query().Get("category_id")
	categoryID, _ := parseQueryInt64(catRaw)
	companyID, _ := parseQueryInt64(r.URL.Query().Get("company_id"))

	priceMin := parseQueryFloat(r.URL.Query().Get("price_min"))
	priceMax := parseQueryFloat(r.URL.Query().Get("price_max"))

	// Parse attr.<code>, attr.<code>_min, attr.<code>_max
	attrFilters := make(map[string][]string)
	attrRanges := make(map[string]*db.PriceRange)

	for key, values := range r.URL.Query() {
		if len(values) == 0 {
			continue
		}

		if strings.HasPrefix(key, "attr.") {
			code := strings.TrimPrefix(key, "attr.")
			// Support PHP-style array params: attr.color[]=red -> attr.color
			code = strings.TrimSuffix(code, "[]")

			if strings.HasSuffix(code, "_min") {
				attrCode := strings.TrimSuffix(code, "_min")
				if f := parseQueryFloatPtr(values[0]); f != nil {
					if _, ok := attrRanges[attrCode]; !ok {
						attrRanges[attrCode] = &db.PriceRange{}
					}
					attrRanges[attrCode].Min = *f
				}
			} else if strings.HasSuffix(code, "_max") {
				attrCode := strings.TrimSuffix(code, "_max")
				if f := parseQueryFloatPtr(values[0]); f != nil {
					if _, ok := attrRanges[attrCode]; !ok {
						attrRanges[attrCode] = &db.PriceRange{}
					}
					attrRanges[attrCode].Max = *f
				}
			} else {
				// Enum filter: collect all non-empty values (OR match within same attribute code).
				// Each value is an exact match (no comma splitting — values may contain commas).
				// Repeated params supported: attr.color=red&attr.color=blue
				// Brand is now treated as a regular attribute (filtered via attr:brand:<hash> turbo-index).
				for _, v := range values {
					// Normalize: replace non-breaking spaces and trim
					part := strings.ReplaceAll(v, "\u00a0", " ")
					part = strings.TrimSpace(part)
					if part != "" {
						attrFilters[code] = append(attrFilters[code], part)
					}
				}
			}
		}
	}

	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	// Support both "limit" and "per_page" (frontend uses per_page)
	limitParam := r.URL.Query().Get("limit")
	if limitParam == "" {
		limitParam = r.URL.Query().Get("per_page")
	}
	limit, _ := parseQueryInt(limitParam, 50)

	// Parse facet attribute codes (attr_facet.<code> query params)
	var facetCodes []string
	for key := range r.URL.Query() {
		if strings.HasPrefix(key, "attr_facet.") {
			code := strings.TrimPrefix(key, "attr_facet.")
			if code != "" {
				facetCodes = append(facetCodes, code)
			}
		}
	}

	params := db.ListParams{
		Q:               q,
		CategoryID:      categoryID,
		CompanyID:       companyID,
		PriceMin:        priceMin,
		PriceMax:        priceMax,
		AttrFilters:     attrFilters,
		AttrRanges:      attrRanges,
		Sort:            sort,
		Page:            page,
		Limit:           limit,
		FacetBrandCodes: facetCodes,
	}

	result, err := h.productRepo.ListWithFacets(params)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if result.Items == nil {
		result.Items = []silentjson.RawMessage{}
	}

	writeJSONEANList(w, r, http.StatusOK, *result)
}

func (h *Handlers) HandleProductGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "product_id")
	if !ok {
		return
	}

	p, err := h.productRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	// No redirect — return product directly.
	// EAN pages are accessed via /shop/{tree}/{slug}, not via product redirect.
	httpres.WriteJSON(w, http.StatusOK, p)
}

func (h *Handlers) HandleProductCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateProductRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	p := &model.Product{
		EAN:         req.EAN,
		Name:        req.Name,
		Description: req.Description,
		CategoryID:  req.CategoryID,
		CompanyID:   req.CompanyID,
		Brand:       req.Brand,
		Price:       req.Price,
		Currency:    req.Currency,
		StockQty:    req.StockQty,
		Status:      model.ProductStatus(req.Status),
		Attributes:  req.Attributes,
		Images:      req.Images,
		SEO:         req.SEO,
	}

	// If user is authenticated as seller, bind product to their company.
	// Admin can create products for any company (via company_id in request).
	// Unauthenticated requests: use company_id from body (for tests/admin).
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && ctxUser.Role == model.RoleSeller {
		// Seller must have a company. Use existing or require company_id.
		if p.CompanyID == 0 {
			companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
			if err != nil {
				httpres.WriteError(w, http.StatusBadRequest, "NO_COMPANY", "seller must have a company; provide company_id or create one")
				return
			}
			p.CompanyID = companyID
		} else {
			// Verify that the provided company_id belongs to this seller.
			companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
			if err != nil || p.CompanyID != companyID {
				httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "seller can only create products for their own company")
				return
			}
		}
	}

	// Product must belong to a company.
	if p.CompanyID == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "company_id is required for products")
		return
	}

	if err := h.productRepo.Create(p); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, p)
}

func (h *Handlers) HandleProductUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "product_id")
	if !ok {
		return
	}

	var req CreateProductRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	// Get product to check ownership
	p, err := h.productRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && ctxUser.Role == model.RoleSeller {
		// Seller can only update their own products (by company_id).
		companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || p.CompanyID != companyID {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "seller can only update their own products")
			return
		}
	}

	if err := h.productRepo.Update(id, func(p *model.Product) {
		if req.Name != "" {
			p.Name = req.Name
		}
		if req.Description != "" {
			p.Description = req.Description
		}
		if req.CategoryID != 0 {
			p.CategoryID = req.CategoryID
		}
		if req.CompanyID != 0 {
			p.CompanyID = req.CompanyID
		}
		if req.Brand != "" {
			p.Brand = req.Brand
		}
		if req.Price != 0 {
			p.Price = req.Price
		}
		if req.Currency != "" {
			p.Currency = req.Currency
		}
		if req.StockQty != 0 {
			p.StockQty = req.StockQty
		}
		if req.Status != "" {
			p.Status = model.ProductStatus(req.Status)
		}
		if req.Attributes != nil {
			p.Attributes = req.Attributes
		}
		if req.Images != nil {
			p.Images = req.Images
		}
		if req.SEO.Title != "" || req.SEO.Description != "" || req.SEO.Keywords != "" {
			p.SEO = req.SEO
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	p, _ = h.productRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, p)
}

func (h *Handlers) HandleProductDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "product_id")
	if !ok {
		return
	}

	// Get product to check ownership
	p, err := h.productRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && ctxUser.Role == model.RoleSeller {
		// Seller can only delete their own products (by company_id).
		companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || p.CompanyID != companyID {
			httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "seller can only delete their own products")
			return
		}
	}

	if err := h.productRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ================= Cart handlers =================

// HandleAdminProductsDeleteAll deletes all products and related indexes.
// POST /admin/products/delete-all
// WARNING: This is a destructive operation.

func (h *Handlers) HandleAdminProductsDeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Optional: require confirmation in body
	type req struct {
		Confirm bool `json:"confirm"`
	}
	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !body.Confirm {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "set confirm=true in body")
		return
	}

	if err := h.productRepo.DeleteAllProducts(); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "all products deleted"})
}

// ================= Product Import handlers =================

// HandleAdminProductsImport starts a product import from JSON file.
// POST /admin/products/import
// Content-Type: multipart/form-data
// Body: file (JSON array of products)
// Also accepts: company_id (query param or in JSON wrapper)

func (h *Handlers) HandleAdminProductsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Get company_id from query or from seller's company
	companyIDStr := r.URL.Query().Get("company_id")
	var companyID int64

	if companyIDStr != "" {
		var err error
		companyID, err = strconv.ParseInt(companyIDStr, 10, 64)
		if err != nil || companyID <= 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
			return
		}
	} else if hasUser && ctxUser.Role == model.RoleSeller {
		var err error
		companyID, err = h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil {
			httpres.WriteError(w, http.StatusBadRequest, "NO_COMPANY", "seller must have a company")
			return
		}
	} else {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "company_id is required")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "file is required")
		return
	}
	defer file.Close()

	// Limit file size
	if header.Size > 32<<20 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "file too large (max 32MB)")
		return
	}

	job, err := h.productImportRepo.CreateImportJob(file, companyID)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"import_id": job.ID,
		"status":    job.Status,
	})
}

// HandleAdminImportStatus returns the status of an import job.
// GET /admin/products/import/{importId}

func (h *Handlers) HandleAdminImportStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		httpres.WriteError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Path: /admin/products/import/{id}
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "admin", "products", "import", "{id}"]
	if len(parts) < 5 || parts[3] != "import" || parts[4] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}

	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid import id")
		return
	}

	job, err := h.productImportRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "import job not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, job)
}

// ================= Review handlers =================

// HandleProductReviewCreate creates a review for a product.
// POST /products/{id}/reviews
// Auth: required (buyer only)
// Body: {"rating": 4, "comment": "Great product!"}
// Validation:
//   - Only buyers can review.
//   - Rating must be 1-5.
//   - User can only review a product once.
