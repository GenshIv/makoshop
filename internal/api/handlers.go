package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
)

type Handlers struct {
	store             *db.Store
	categoryRepo      *db.CategoryRepo
	attrDefRepo       *db.AttrDefRepo
	productRepo       *db.ProductRepo
	turboSearch       *db.TurboProductSearch
	scuPageSearch     *db.SCUPageSearch
	landingRepo       *db.LandingRepo
	scuPageRepo       *db.SCUPageRepo
	companyRepo       *db.CompanyRepo
	userRepo          *db.UserRepo
	cartRepo          *db.CartRepo
	orderRepo         *db.OrderRepo
	paymentRepo       *db.PaymentRepo
	reviewRepo        *db.ReviewRepo
	productImportRepo *db.ProductImportRepo
	promoPlanRepo     *db.PromoPlanRepo
	promoCampaignRepo *db.PromoCampaignRepo
	promoLogRepo      *db.PromoLogRepo
}

func NewHandlers(store *db.Store) *Handlers {
	promoCampaignRepo := db.NewPromoCampaignRepo(store)
	promoPlanRepo := db.NewPromoPlanRepo(store)
	promoLogRepo := db.NewPromoLogRepo(store)
	productRepo := db.NewProductRepo(store, promoCampaignRepo, promoPlanRepo, promoLogRepo)
	categoryRepo := db.NewCategoryRepo(store)
	attrDefRepo := db.NewAttrDefRepo(store)

	// Turbo search: enabled by default. Can be disabled via env flag if needed.
	turboEnabled := true
	turboSearch := db.NewTurboProductSearch(store.DB(), productRepo, categoryRepo, turboEnabled)
	productRepo.SetTurboSearch(turboSearch)

	landingRepo := db.NewLandingRepo(store)
	turboSearch.SetLandingRepo(landingRepo)

	scuPageRepo := db.NewSCUPageRepo(store)
	turboSearch.SetSCUPageRepo(scuPageRepo)

	// SCUPage search (catalog works on SCU pages)
	scuPageSearch := db.NewSCUPageSearch(store.DB(), scuPageRepo, productRepo, categoryRepo, turboEnabled)
	productRepo.SetSCUPageSearch(scuPageSearch)

	return &Handlers{
		store:             store,
		categoryRepo:      categoryRepo,
		attrDefRepo:       attrDefRepo,
		companyRepo:       db.NewCompanyRepo(store),
		userRepo:          db.NewUserRepo(store),
		cartRepo:          db.NewCartRepo(store),
		orderRepo:         db.NewOrderRepo(store),
		paymentRepo:       db.NewPaymentRepo(store),
		reviewRepo:        db.NewReviewRepo(store),
		productImportRepo: db.NewProductImportRepo(store, productRepo),
		promoPlanRepo:     promoPlanRepo,
		promoCampaignRepo: promoCampaignRepo,
		promoLogRepo:      promoLogRepo,
		productRepo:       productRepo,
		turboSearch:       turboSearch,
		scuPageSearch:     scuPageSearch,
		landingRepo:       landingRepo,
		scuPageRepo:       scuPageRepo,
	}
}

// TurboSearch returns the attached TurboProductSearch.
func (h *Handlers) TurboSearch() *db.TurboProductSearch {
	return h.turboSearch
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return false
	}
	return true
}

// HandleTurboProducts handles GET /products/turbo (turbo-index based search).
func (h *Handlers) HandleTurboProducts(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": result.Items,
		"total": result.Total,
		"page":  result.Page,
		"limit": result.Limit,
	})
}

func parseID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	idStr := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing id")
		return 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return 0, false
	}
	return id, true
}

func parseQueryInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func parseQueryInt(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def, err
	}
	return v, nil
}

func parseQueryFloat(s string) *float64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseQueryFloatPtr(s string) *float64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// --- Categories ---

func (h *Handlers) HandleCategoriesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	cats, err := h.categoryRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Filter by parent_id if provided
	if parentIDStr := r.URL.Query().Get("parent_id"); parentIDStr != "" {
		var parentID int64
		_, err := fmt.Sscanf(parentIDStr, "%d", &parentID)
		if err == nil {
			var filtered []model.Category
			for _, c := range cats {
				if c.ParentID != nil && *c.ParentID == parentID {
					filtered = append(filtered, c)
				}
			}
			cats = filtered
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": cats,
	})
}

// HandleCategoriesTree returns the category tree.
// GET /categories/tree
// GET /categories/tree?child_of={id}
func (h *Handlers) HandleCategoriesTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	childOf := r.URL.Query().Get("child_of")
	if childOf != "" {
		parentID, err := strconv.ParseInt(childOf, 10, 64)
		if err != nil || parentID <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid child_of parameter")
			return
		}
		tree, err := h.categoryRepo.GetTreeByParent(parentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tree)
		return
	}

	tree, err := h.categoryRepo.GetTree()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (h *Handlers) HandleCategoryGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	cat, err := h.categoryRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "category not found")
		return
	}

	writeJSON(w, http.StatusOK, cat)
}

type CreateCategoryRequest struct {
	ParentID    *int64 `json:"parent_id,omitempty"`
	ParentIDSet bool   `json:"-"` // tracks if parent_id was explicitly sent
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	IsActive    bool   `json:"is_active"`
	SortOrder   int    `json:"sort_order"`
}

func (h *Handlers) HandleCategoryCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateCategoryRequest
	if !readJSON(w, r, &req) {
		return
	}

	cat := &model.Category{
		ParentID:  req.ParentID,
		Name:      req.Name,
		Slug:      req.Slug,
		Desc:      req.Description,
		IsActive:  req.IsActive,
		SortOrder: req.SortOrder,
	}

	if err := h.categoryRepo.Create(cat); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, cat)
}

func (h *Handlers) HandleCategoryUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	var req CreateCategoryRequest
	if !readJSON(w, r, &req) {
		return
	}

	if err := h.categoryRepo.Update(id, func(c *model.Category) {
		if req.ParentID != nil {
			c.ParentID = req.ParentID
		}
		if req.Name != "" {
			c.Name = req.Name
		}
		if req.Slug != "" {
			c.Slug = req.Slug
		}
		if req.Description != "" {
			c.Desc = req.Description
		}
		if req.SortOrder != 0 {
			c.SortOrder = req.SortOrder
		}
		c.IsActive = req.IsActive
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	cat, _ := h.categoryRepo.Get(id)
	writeJSON(w, http.StatusOK, cat)
}

func (h *Handlers) HandleCategoryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	if err := h.categoryRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Category Attributes ---

// HandleCategoryAttributes handles GET, POST, DELETE for /admin/categories/{id}/attributes
func (h *Handlers) HandleCategoryAttributes(w http.ResponseWriter, r *http.Request) {
	// Parse category ID from path: /admin/categories/{id}/attributes
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 || parts[len(parts)-1] != "attributes" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	catIDStr := parts[len(parts)-2]
	catID, err := strconv.ParseInt(catIDStr, 10, 64)
	if err != nil || catID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid category id")
		return
	}

	// Verify category exists
	_, err = h.categoryRepo.Get(catID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "category not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGetCategoryAttributes(w, r, catID)
	case http.MethodPost:
		h.handleAddCategoryAttribute(w, r, catID)
	case http.MethodDelete:
		h.handleRemoveCategoryAttribute(w, r, catID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
	}
}

// GET /admin/categories/{id}/attributes
func (h *Handlers) handleGetCategoryAttributes(w http.ResponseWriter, r *http.Request, catID int64) {
	// Get attribute codes for this category
	codes, err := h.attrDefRepo.GetCodesForCategoryTree(catID, h.categoryRepo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	type attrInfo struct {
		Code   string   `json:"code"`
		Values []string `json:"values"`
	}

	var attrs []attrInfo
	for _, code := range codes {
		values, _ := h.attrDefRepo.GetAttrValuesForCategory(code, catID)
		if len(values) == 0 {
			continue
		}
		attrs = append(attrs, attrInfo{Code: code, Values: values})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"category_id": catID,
		"attributes":  attrs,
	})
}

// POST /admin/categories/{id}/attributes — add attribute code to category
// Body: { "code": "attribute-code" }
func (h *Handlers) handleAddCategoryAttribute(w http.ResponseWriter, r *http.Request, catID int64) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}

	if err := h.attrDefRepo.UpsertCode(req.Code, catID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "attribute added",
		"code":    req.Code,
	})
}

// DELETE /admin/categories/{id}/attributes — remove attribute code from category
// Query: ?code=attribute-code
func (h *Handlers) handleRemoveCategoryAttribute(w http.ResponseWriter, r *http.Request, catID int64) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "code query param is required")
		return
	}

	// Remove category from attribute's category list
	cats, err := h.attrDefRepo.GetCategories(code)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "attribute not found")
		return
	}

	var newCats []int64
	for _, c := range cats {
		if c != catID {
			newCats = append(newCats, c)
		}
	}

	if len(newCats) == 0 {
		// No more categories use this attribute — clear it
		_ = h.store.DB().TurboRawWrite("attrdef_cats:"+code, []byte{})
	} else {
		buf := makodb.TurboBinaryNew(db.Uint64SliceFromInt64(newCats))
		_ = h.store.DB().TurboRawWrite("attrdef_cats:"+code, buf)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "attribute removed",
		"code":    code,
	})
}

// --- Brands ---

// GET /brands — returns all brands from brand_list turbo index.
// Optional: ?category_id=N to filter brands by category.
func (h *Handlers) HandleBrandsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if h.turboSearch == nil {
		writeError(w, http.StatusServiceUnavailable, "TURBO_DISABLED", "turbo search is disabled")
		return
	}

	catRaw := r.URL.Query().Get("category_id")
	var catID int64
	if catRaw != "" {
		catID, _ = strconv.ParseInt(catRaw, 10, 64)
	}

	brands, err := h.turboSearch.GetBrands(catID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, brands)
}

// --- Attribute Values (turbo-based) ---

// GET /attributes/{code}/values — returns all values for an attribute code from turbo index.
// Used by frontend to build filter UI.
func (h *Handlers) HandleAttributeValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /attributes/{code}/values
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[len(parts)-1] != "values" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	code := parts[len(parts)-2]
	if code == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "attribute code required")
		return
	}

	// Без category context возвращаем значения из первой найденной категории
	// (этот endpoint устарел, лучше использовать /admin/categories/{id}/attributes)
	values, err := h.attrDefRepo.GetAttrValuesForCategory(code, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"code":   code,
		"values": values,
	})
}

// --- Products ---

type CreateProductRequest struct {
	SKU         string                 `json:"sku"`
	SCU         string                 `json:"scu,omitempty"` // Standard Catalog Unit — links to landing page
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	CategoryID  int64                  `json:"category_id"`
	CompanyID   int64                  `json:"company_id"`
	Brand       string                 `json:"brand,omitempty"`
	Price       float64                `json:"price"`
	Currency    string                 `json:"currency"`
	StockQty    int64                  `json:"stock_qty"`
	Status      string                 `json:"status"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Images      []string               `json:"images,omitempty"`
	SEO         model.ProductSEO       `json:"seo,omitempty"`
}

func (h *Handlers) HandleProductsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if result.Items == nil {
		result.Items = []db.ProductListItem{}
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) HandleProductGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "product_id")
	if !ok {
		return
	}

	p, err := h.productRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	// No redirect — return product directly.
	// SCU pages are accessed via /shop/{tree}/{slug}, not via product redirect.
	writeJSON(w, http.StatusOK, p)
}

func (h *Handlers) HandleProductCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateProductRequest
	if !readJSON(w, r, &req) {
		return
	}

	p := &model.Product{
		SKU:         req.SKU,
		SCU:         req.SCU,
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
				writeError(w, http.StatusBadRequest, "NO_COMPANY", "seller must have a company; provide company_id or create one")
				return
			}
			p.CompanyID = companyID
		} else {
			// Verify that the provided company_id belongs to this seller.
			companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
			if err != nil || p.CompanyID != companyID {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "seller can only create products for their own company")
				return
			}
		}
	}

	// Product must belong to a company.
	if p.CompanyID == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "company_id is required for products")
		return
	}

	if err := h.productRepo.Create(p); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

func (h *Handlers) HandleProductUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "product_id")
	if !ok {
		return
	}

	var req CreateProductRequest
	if !readJSON(w, r, &req) {
		return
	}

	// Get product to check ownership
	p, err := h.productRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && ctxUser.Role == model.RoleSeller {
		// Seller can only update their own products (by company_id).
		companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || p.CompanyID != companyID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "seller can only update their own products")
			return
		}
	}

	if err := h.productRepo.Update(id, func(p *model.Product) {
		if req.SKU != "" {
			p.SKU = req.SKU
		}
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
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	p, _ = h.productRepo.Get(id)
	writeJSON(w, http.StatusOK, p)
}

func (h *Handlers) HandleProductDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "product_id")
	if !ok {
		return
	}

	// Get product to check ownership
	p, err := h.productRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && ctxUser.Role == model.RoleSeller {
		// Seller can only delete their own products (by company_id).
		companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || p.CompanyID != companyID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "seller can only delete their own products")
			return
		}
	}

	if err := h.productRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ================= Cart handlers =================

// HandleCartCreate creates a new cart.
// POST /cart
// Body: {"user_id": <optional>, "session_id": "<optional>"}
func (h *Handlers) HandleCartCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		UserID    *int64 `json:"user_id,omitempty"`
		SessionID string `json:"session_id,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	// If user is authenticated, use their ID (override request)
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && req.UserID == nil {
		req.UserID = &ctxUser.ID
	}

	cart, err := h.cartRepo.Create(req.UserID, req.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, cart)
}

// HandleCartGet returns a cart by ID.
// GET /cart/{id}
func (h *Handlers) HandleCartGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	cartID := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")
	if cartID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing cart id")
		return
	}

	cart, err := h.cartRepo.Get(cartID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "cart not found")
		return
	}

	writeJSON(w, http.StatusOK, cart)
}

// HandleCartMe returns the current user's cart.
// GET /cart/me (requires auth)
func (h *Handlers) HandleCartMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	cart, err := h.cartRepo.GetUserCart(ctxUser.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "cart not found")
		return
	}

	writeJSON(w, http.StatusOK, cart)
}

// HandleCartItemAdd adds a product to the cart.
// POST /cart/{id}/items
// Body: {"product_id": 123, "qty": 2}
func (h *Handlers) HandleCartItemAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /cart/{id}/items
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "cart", "{id}", "items"]
	if len(parts) < 4 || parts[2] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing cart id")
		return
	}
	cartID := parts[2]

	var req struct {
		ProductID int64 `json:"product_id"`
		Qty       int   `json:"qty"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	if req.ProductID == 0 || req.Qty <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "product_id and qty > 0 required")
		return
	}

	// Get product to fetch price and name
	p, err := h.productRepo.Get(req.ProductID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	cart, err := h.cartRepo.AddItem(cartID, req.ProductID, p.Name, req.Qty, p.Price)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, cart)
}

// HandleCartItemUpdate updates quantity of an item in the cart.
// PATCH /cart/{id}/items/{product_id}
// Body: {"qty": 5} (qty=0 removes the item)
func (h *Handlers) HandleCartItemUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /cart/{cart_id}/items/{product_id}
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "cart", "{cart_id}", "items", "{product_id}"]
	if len(parts) < 5 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}

	cartID := parts[2]
	productIDStr := parts[4]

	productID, err := strconv.ParseInt(productIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	var req struct {
		Qty int `json:"qty"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	cart, err := h.cartRepo.UpdateItem(cartID, productID, req.Qty)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, cart)
}

// HandleCartDelete deletes a cart.
// DELETE /cart/{id}
func (h *Handlers) HandleCartDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	cartID := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")
	if cartID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing cart id")
		return
	}

	if err := h.cartRepo.Delete(cartID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ================= Order handlers =================

// HandleOrderCreate creates a new order.
// POST /orders
// Body (two modes):
//  1. From cart: {"cart_id": "...", "shipping_info": {...}, "comment": "..."}
//  2. Manual:    {"user_id": 1, "items": [...], "shipping_info": {...}, "comment": "..."}
//
// user_id can be omitted if user is authenticated.
// If cart_id is provided, items are taken from the cart and the cart is cleared.
func (h *Handlers) HandleOrderCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		UserID       int64              `json:"user_id"`
		CartID       string             `json:"cart_id"`
		Items        []model.OrderItem  `json:"items"`
		ShippingInfo model.ShippingInfo `json:"shipping_info"`
		Comment      string             `json:"comment,omitempty"`
		Currency     string             `json:"currency,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	// If user is authenticated, use their ID (override request)
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && req.UserID == 0 {
		req.UserID = ctxUser.ID
	}

	var items []model.OrderItem
	var cartID string

	if req.CartID != "" {
		// Mode 1: create order from cart
		cartID = req.CartID
		if req.UserID == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required when creating order from cart")
			return
		}

		cart, err := h.cartRepo.Get(cartID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cart not found")
			return
		}

		if len(cart.Items) == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "cart is empty")
			return
		}

		// Build order items from cart items
		items = make([]model.OrderItem, 0, len(cart.Items))
		for _, ci := range cart.Items {
			items = append(items, model.OrderItem{
				ProductID: ci.ProductID,
				Qty:       ci.Qty,
				Price:     ci.Price,
			})
		}
	} else {
		// Mode 2: manual items
		if req.UserID == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required")
			return
		}
		if len(req.Items) == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "at least one item is required")
			return
		}
		items = req.Items
	}

	// Validate and enrich items: check product exists, is active, has enough stock
	var totalAmount float64
	for i := range items {
		item := &items[i]
		if item.Qty <= 0 || item.Price <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid item qty or price")
			return
		}

		p, err := h.productRepo.Get(item.ProductID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("product %d not found", item.ProductID))
			return
		}
		if p.Status != model.ProductStatusActive {
			writeError(w, http.StatusBadRequest, "PRODUCT_NOT_AVAILABLE", fmt.Sprintf("product %d is not active", item.ProductID))
			return
		}
		if p.StockQty < int64(item.Qty) {
			writeError(w, http.StatusBadRequest, "INSUFFICIENT_STOCK", fmt.Sprintf("product %d has only %d in stock", item.ProductID, p.StockQty))
			return
		}

		item.CompanyID = p.CompanyID
		item.Total = item.Price * float64(item.Qty)
		totalAmount += item.Total
	}

	currency := req.Currency
	if currency == "" {
		currency = "RUB"
	}

	order := &model.Order{
		UserID:        req.UserID,
		Items:         items,
		TotalAmount:   totalAmount,
		Currency:      currency,
		ShippingInfo:  req.ShippingInfo,
		Comment:       req.Comment,
		Status:        model.OrderStatusNew,
		PaymentStatus: model.PaymentStatusPending,
	}

	if err := h.orderRepo.Create(order); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Clear cart if order was created from it
	if cartID != "" {
		_ = h.cartRepo.Delete(cartID)
	}

	writeJSON(w, http.StatusCreated, order)
}

// HandleOrderGet returns an order by ID.
// GET /orders/{id}
// Access: order owner or admin.
func (h *Handlers) HandleOrderGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "order_id")
	if !ok {
		return
	}

	order, err := h.orderRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "order not found")
		return
	}

	// Access control: only owner or admin can view
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || (order.UserID != ctxUser.ID && ctxUser.Role != model.RoleAdmin) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only view your own orders")
		return
	}

	writeJSON(w, http.StatusOK, order)
}

// HandleOrderUserList returns orders for a user.
// GET /orders?user_id=123
// If user is authenticated and user_id is not provided, uses current user.
func (h *Handlers) HandleOrderUserList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	userIDStr := r.URL.Query().Get("user_id")
	var userID int64

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if hasUser && userIDStr == "" {
		userID = ctxUser.ID
	} else if userIDStr != "" {
		var err error
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil || userID <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid user_id")
			return
		}
	} else {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "user_id is required (or authenticate)")
		return
	}

	orders, err := h.orderRepo.GetUserOrders(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if orders == nil {
		orders = []model.Order{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": orders,
	})
}

// HandleOrderUpdateStatus updates order status.
// PATCH /orders/{id}/status
// Body: {"status": "confirmed"}
// Access rules:
//   - admin: can change any order to any status.
//   - seller: can change only orders that contain their products (by company_id).
//   - buyer (order owner): can only cancel an order in "new" status.
func (h *Handlers) HandleOrderUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /orders/{id}/status
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "orders", "{id}", "status"]
	if len(parts) < 4 || parts[2] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing order id")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid order id")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "status is required")
		return
	}

	order, err := h.orderRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "order not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	newStatus := model.OrderStatus(req.Status)

	// Admin: full access
	if ctxUser.Role == model.RoleAdmin {
		if err := h.orderRepo.UpdateStatus(id, newStatus); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		order, _ = h.orderRepo.Get(id)
		writeJSON(w, http.StatusOK, order)
		return
	}

	// Buyer: can only cancel their own order in "new" status
	if ctxUser.Role == model.RoleBuyer {
		if order.UserID != ctxUser.ID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only manage your own orders")
			return
		}
		if newStatus != model.OrderStatusCancelled {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "buyers can only cancel orders")
			return
		}
		if order.Status != model.OrderStatusNew {
			writeError(w, http.StatusBadRequest, "INVALID_STATE", "only orders in 'new' status can be cancelled by buyer")
			return
		}
		if err := h.orderRepo.UpdateStatus(id, newStatus); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		order, _ = h.orderRepo.Get(id)
		writeJSON(w, http.StatusOK, order)
		return
	}

	// Seller: can manage orders containing their products
	if ctxUser.Role == model.RoleSeller {
		companyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "NO_COMPANY", "seller must have a company")
			return
		}

		// Check that at least one item in the order belongs to this seller's company
		hasCompanyItem := false
		for _, item := range order.Items {
			if item.CompanyID == companyID {
				hasCompanyItem = true
				break
			}
		}
		if !hasCompanyItem {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only manage orders with your products")
			return
		}

		if err := h.orderRepo.UpdateStatus(id, newStatus); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		order, _ = h.orderRepo.Get(id)
		writeJSON(w, http.StatusOK, order)
		return
	}

	writeError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
}

// ================= Payment handlers =================

// HandlePaymentCreate creates a payment for an order (stub).
// POST /payments?order_id=123
// Body: {"method": "card"}
func (h *Handlers) HandlePaymentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	orderIDStr := r.URL.Query().Get("order_id")
	if orderIDStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "order_id is required")
		return
	}

	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil || orderID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid order_id")
		return
	}

	// Get order to fetch amount
	order, err := h.orderRepo.Get(orderID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "order not found")
		return
	}

	var req struct {
		Method string `json:"method"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	method := model.PaymentMethod(req.Method)
	if method == "" {
		method = model.PaymentMethodCard
	}

	// Stub: generate a fake payment URL
	payment := &model.Payment{
		OrderID:    orderID,
		Amount:     order.TotalAmount,
		Currency:   order.Currency,
		Method:     method,
		Status:     model.PaymentStatusPending,
		PaymentURL: fmt.Sprintf("https://payment.example.com/pay/%d", orderID),
	}

	if err := h.paymentRepo.Create(payment); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, payment)
}

// HandlePaymentConfirm confirms a payment (stub).
// POST /payments/{id}/confirm
// Body: {}
func (h *Handlers) HandlePaymentConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /payments/{id}/confirm
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "payments", "{id}", "confirm"]
	if len(parts) < 4 || parts[2] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing payment id")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid payment id")
		return
	}

	// Update payment status
	if err := h.paymentRepo.UpdateStatus(id, model.PaymentStatusPaid); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Also update order payment_status
	payment, _ := h.paymentRepo.Get(id)
	if payment != nil {
		_ = h.orderRepo.Update(payment.OrderID, func(o *model.Order) {
			o.PaymentStatus = model.PaymentStatusPaid
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "confirmed",
	})
}

// HandlePaymentGet returns a payment by ID.
// GET /payments/{id}
func (h *Handlers) HandlePaymentGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "payment_id")
	if !ok {
		return
	}

	payment, err := h.paymentRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "payment not found")
		return
	}

	writeJSON(w, http.StatusOK, payment)
}

// HandlePaymentWebhook handles payment webhooks from external providers.
// POST /payments/webhook/{provider}
// Body: provider-specific payload (stub: expects {"payment_id": 123, "status": "paid"|"failed"})
// Signature verification: header X-Webhook-Signature (simple HMAC-SHA256 stub).
func (h *Handlers) HandlePaymentWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /payments/webhook/{provider}
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "payments", "webhook", "{provider}"]
	if len(parts) < 4 || parts[2] != "webhook" || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid webhook path")
		return
	}
	provider := parts[3]

	// Stub signature verification (in production: verify HMAC)
	signature := r.Header.Get("X-Webhook-Signature")
	if signature == "" {
		writeError(w, http.StatusForbidden, "INVALID_SIGNATURE", "missing X-Webhook-Signature")
		return
	}
	// TODO: real HMAC verification per provider
	_ = provider

	var payload struct {
		PaymentID int64  `json:"payment_id"`
		Status    string `json:"status"`
	}
	if !readJSON(w, r, &payload) {
		return
	}

	if payload.PaymentID == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "payment_id is required")
		return
	}

	payment, err := h.paymentRepo.Get(payload.PaymentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "payment not found")
		return
	}

	// Idempotency: ignore if already in final state
	if payment.Status == model.PaymentStatusRefunded {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_refunded"})
		return
	}

	var newStatus model.PaymentStatus
	switch strings.ToLower(payload.Status) {
	case "paid", "succeeded", "success":
		newStatus = model.PaymentStatusPaid
	case "failed", "declined", "error":
		newStatus = model.PaymentStatusFailed
	default:
		writeError(w, http.StatusBadRequest, "INVALID_STATUS", "unknown payment status from provider")
		return
	}

	// Idempotency: if already in this status, no-op
	if payment.Status == newStatus {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_processed"})
		return
	}

	// Update payment
	if err := h.paymentRepo.UpdateStatus(payload.PaymentID, newStatus); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Update order payment_status
	_ = h.orderRepo.Update(payment.OrderID, func(o *model.Order) {
		o.PaymentStatus = newStatus
		if newStatus == model.PaymentStatusFailed {
			o.Status = model.OrderStatusCancelled
		}
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "payment_status": string(newStatus)})
}

// HandlePaymentRefund refunds a payment.
// POST /payments/{id}/refund
// Body: {} (full refund; partial refund support can be added later)
// Access: admin only.
func (h *Handlers) HandlePaymentRefund(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /payments/{id}/refund
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "payments", "{id}", "refund"]
	if len(parts) < 4 || parts[2] == "" || parts[3] != "refund" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid payment id")
		return
	}

	// Check auth: admin only
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	payment, err := h.paymentRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "payment not found")
		return
	}

	if payment.Status != model.PaymentStatusPaid {
		writeError(w, http.StatusBadRequest, "INVALID_STATE", "only paid payments can be refunded")
		return
	}

	// Update payment to refunded
	if err := h.paymentRepo.UpdateStatus(id, model.PaymentStatusRefunded); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Update order
	_ = h.orderRepo.Update(payment.OrderID, func(o *model.Order) {
		o.PaymentStatus = model.PaymentStatusRefunded
		o.Status = model.OrderStatusRefunded
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "refunded"})
}

// HandlePaymentTimeoutCleanup cancels orders with pending payments older than a threshold.
// POST /admin/payments/timeout-cleanup
// Body: {"max_pending_minutes": 30}
// Access: admin only.
func (h *Handlers) HandlePaymentTimeoutCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	var req struct {
		MaxPendingMinutes int `json:"max_pending_minutes"`
	}
	if !readJSON(w, r, &req) {
		req.MaxPendingMinutes = 30 // default
	}
	if req.MaxPendingMinutes <= 0 {
		req.MaxPendingMinutes = 30
	}

	result, err := h.paymentRepo.CleanupTimedOutPayments(req.MaxPendingMinutes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":              "ok",
		"max_pending_minutes": req.MaxPendingMinutes,
		"result":              result,
	})
}

// HandleCompanyOrders returns orders containing products from a specific company.
// GET /companies/{companyId}/orders?status=...&page=...&limit=...
// Access: seller (own company) or admin.
func (h *Handlers) HandleCompanyOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /companies/{companyId}/orders
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "companies", "{companyId}", "orders"]
	if len(parts) < 4 || parts[2] == "" || parts[3] != "orders" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}

	companyID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || companyID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
		return
	}

	// Check company exists
	company, err := h.companyRepo.Get(companyID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	// Access control: seller (own company) or admin
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	if ctxUser.Role == model.RoleAdmin {
		// Admin: full access
	} else if ctxUser.Role == model.RoleSeller {
		// Seller: only own company
		ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || companyID != ownerCompanyID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only view orders for your own company")
			return
		}
	} else {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
		return
	}

	// Get orders
	orders, err := h.orderRepo.GetOrdersByCompanyID(companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Filter by status if provided
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	if statusFilter != "" {
		var filtered []model.Order
		for _, o := range orders {
			if string(o.Status) == statusFilter {
				filtered = append(filtered, o)
			}
		}
		orders = filtered
	}

	if orders == nil {
		orders = []model.Order{}
	}

	// Pagination
	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}

	total := len(orders)
	start := (page - 1) * limit
	end := start + limit
	if start > total {
		orders = []model.Order{}
	} else if end > total {
		orders = orders[start:]
	} else {
		orders = orders[start:end]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"company_id": companyID,
		"company":    company.Name,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"items":      orders,
	})
}

// HandleAnalyticsOrders returns aggregate order statistics.
// GET /admin/analytics/orders
// Access: admin only.
func (h *Handlers) HandleAnalyticsOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	orders, err := h.orderRepo.GetAllOrders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Calculate aggregates
	totalOrders := len(orders)
	totalRevenue := 0.0
	statusCounts := make(map[string]int)
	paymentStatusCounts := make(map[string]int)

	for _, o := range orders {
		statusCounts[string(o.Status)]++
		paymentStatusCounts[string(o.PaymentStatus)]++
		totalRevenue += o.TotalAmount
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_orders":      totalOrders,
		"total_revenue":     totalRevenue,
		"by_status":         statusCounts,
		"by_payment_status": paymentStatusCounts,
	})
}

// HandleAnalyticsOverview returns general platform metrics.
// GET /admin/analytics/overview
// Access: admin only.
func (h *Handlers) HandleAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Count users
	users, err := h.userRepo.GetAllUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Count companies
	companies, err := h.companyRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Count products
	products, err := h.productRepo.GetAllProducts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Orders and revenue
	orders, err := h.orderRepo.GetAllOrders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	totalOrders := len(orders)
	totalRevenue := 0.0
	for _, o := range orders {
		if o.Status != model.OrderStatusCancelled && o.Status != model.OrderStatusRefunded {
			totalRevenue += o.TotalAmount
		}
	}

	// Promo revenue (budget_used across all campaigns)
	promoCampaigns, err := h.promoCampaignRepo.ListAll()
	if err != nil {
		promoCampaigns = []model.PromoCampaign{}
	}

	var promoRevenue float64
	for _, c := range promoCampaigns {
		promoRevenue += c.BudgetUsed
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_users":     len(users),
		"total_companies": len(companies),
		"total_products":  len(products),
		"total_orders":    totalOrders,
		"total_revenue":   totalRevenue,
		"promo_revenue":   promoRevenue,
	})
}

// HandleAnalyticsProducts returns popular products.
// GET /admin/analytics/products?from=...&to=...&limit=10&sort=orders
// Access: admin only.
// sort: views|orders|revenue (default: orders)
func (h *Handlers) HandleAnalyticsProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 10)
	if limit > 100 {
		limit = 100
	}

	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "orders"
	}

	// Collect product stats from orders
	type ProductStats struct {
		ProductID int64
		Name      string
		Orders    int
		Revenue   float64
	}

	statsMap := make(map[int64]*ProductStats)

	orders, err := h.orderRepo.GetAllOrders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	for _, o := range orders {
		if o.Status == model.OrderStatusCancelled || o.Status == model.OrderStatusRefunded {
			continue
		}

		for _, item := range o.Items {
			if _, ok := statsMap[item.ProductID]; !ok {
				statsMap[item.ProductID] = &ProductStats{ProductID: item.ProductID}
			}
			s := statsMap[item.ProductID]
			s.Orders += item.Qty
			s.Revenue += item.Total
		}
	}

	// Enrich with product names
	for id, s := range statsMap {
		p, err := h.productRepo.Get(id)
		if err == nil {
			s.Name = p.Name
		}
	}

	// Convert to slice
	var stats []*ProductStats
	for _, s := range statsMap {
		stats = append(stats, s)
	}

	// Sort
	switch sortBy {
	case "orders":
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].Orders > stats[j].Orders
		})
	case "revenue":
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].Revenue > stats[j].Revenue
		})
	case "views":
		// Views tracking not implemented yet; fallback to orders
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].Orders > stats[j].Orders
		})
	}

	// Limit
	if len(stats) > limit {
		stats = stats[:limit]
	}

	result := make([]map[string]interface{}, 0, len(stats))
	for _, s := range stats {
		result = append(result, map[string]interface{}{
			"product_id": s.ProductID,
			"name":       s.Name,
			"orders":     s.Orders,
			"revenue":    s.Revenue,
			"views":      0, // TODO: implement views tracking
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": result,
	})
}

// HandleAnalyticsSearchQueries returns popular search queries.
// GET /admin/analytics/search-queries?from=...&to=...&limit=20
// Access: admin only.
// NOTE: Currently returns stub data (search query logging not yet implemented).
func (h *Handlers) HandleAnalyticsSearchQueries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// TODO: implement search query logging and aggregation
	// For now, return empty list with a note
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": []interface{}{},
		"note":  "search query logging not yet implemented",
	})
}

// ================= Promo handlers =================

// HandlePromoPlansList returns all promo plans (public).
// GET /promo/plans
func (h *Handlers) HandlePromoPlansList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	plans, err := h.promoPlanRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if plans == nil {
		plans = []model.PromoPlan{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": plans,
		"total": len(plans),
	})
}

// HandleAdminPromoPlanCreate creates a promo plan (admin only).
// POST /admin/promo-plans
func (h *Handlers) HandleAdminPromoPlanCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	var req struct {
		Name         string                 `json:"name"`
		Type         string                 `json:"type"`
		DurationDays int                    `json:"duration_days"`
		Price        float64                `json:"price"`
		Currency     string                 `json:"currency"`
		Description  string                 `json:"description,omitempty"`
		Constraints  map[string]interface{} `json:"constraints,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Type == "" || req.DurationDays <= 0 || req.Price <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "name, type, duration_days > 0, price > 0 required")
		return
	}

	p := &model.PromoPlan{
		Name:         req.Name,
		Type:         model.PromoPlanType(req.Type),
		DurationDays: req.DurationDays,
		Price:        req.Price,
		Currency:     req.Currency,
		Description:  req.Description,
		Constraints:  req.Constraints,
	}
	if p.Currency == "" {
		p.Currency = "RUB"
	}

	if err := h.promoPlanRepo.Create(p); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

// HandleAdminPromoPlanUpdate updates a promo plan (admin only).
// PATCH /admin/promo-plans/{id}
func (h *Handlers) HandleAdminPromoPlanUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	id, ok := parseID(w, r, "promo_plan_id")
	if !ok {
		return
	}

	var req struct {
		Name         string                 `json:"name,omitempty"`
		Type         string                 `json:"type,omitempty"`
		DurationDays int                    `json:"duration_days"`
		Price        float64                `json:"price"`
		Currency     string                 `json:"currency,omitempty"`
		Description  string                 `json:"description,omitempty"`
		Constraints  map[string]interface{} `json:"constraints,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	if err := h.promoPlanRepo.Update(id, func(p *model.PromoPlan) {
		if req.Name != "" {
			p.Name = req.Name
		}
		if req.Type != "" {
			p.Type = model.PromoPlanType(req.Type)
		}
		if req.DurationDays > 0 {
			p.DurationDays = req.DurationDays
		}
		if req.Price > 0 {
			p.Price = req.Price
		}
		if req.Currency != "" {
			p.Currency = req.Currency
		}
		if req.Description != "" {
			p.Description = req.Description
		}
		if req.Constraints != nil {
			p.Constraints = req.Constraints
		}
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	p, _ := h.promoPlanRepo.Get(id)
	writeJSON(w, http.StatusOK, p)
}

// HandleAdminPromoCampaignCreate creates a campaign (admin only).
// POST /admin/promo/campaigns
func (h *Handlers) HandleAdminPromoCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	var req struct {
		CompanyID      int64               `json:"company_id"`
		PromoPlanID    int64               `json:"promo_plan_id"`
		Status         string              `json:"status"`
		TargetFilters  model.TargetFilters `json:"target_filters"`
		TargetPosition string              `json:"target_position"`
		ProductIDs     []int64             `json:"product_ids,omitempty"`
		BudgetTotal    float64             `json:"budget_total"`
		StartAt        string              `json:"start_at,omitempty"`
		EndAt          string              `json:"end_at,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.CompanyID <= 0 || req.PromoPlanID <= 0 || req.BudgetTotal <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "company_id > 0, promo_plan_id > 0, budget_total > 0 required")
		return
	}

	plan, err := h.promoPlanRepo.Get(req.PromoPlanID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "promo_plan not found")
		return
	}

	startAt := time.Now()
	endAt := startAt.AddDate(0, 0, plan.DurationDays)
	if req.StartAt != "" {
		if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
			startAt = t
		}
	}
	if req.EndAt != "" {
		if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
			endAt = t
		}
	}

	status := model.PromoCampaignStatusActive
	if req.Status != "" {
		status = model.PromoCampaignStatus(req.Status)
	}

	c := &model.PromoCampaign{
		CompanyID:      req.CompanyID,
		PromoPlanID:    req.PromoPlanID,
		Status:         status,
		TargetFilters:  req.TargetFilters,
		TargetPosition: req.TargetPosition,
		ProductIDs:     req.ProductIDs,
		BudgetTotal:    req.BudgetTotal,
		BudgetUsed:     0,
		StartAt:        startAt,
		EndAt:          endAt,
	}

	if err := h.promoCampaignRepo.Create(c); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, c)
}

// HandleAdminPromoCampaignsList returns all campaigns (admin only).
// GET /admin/promo/campaigns?status=...
func (h *Handlers) HandleAdminPromoCampaignsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	campaigns, err := h.promoCampaignRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if campaigns == nil {
		campaigns = []model.PromoCampaign{}
	}

	// Filter by status if provided
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	if statusFilter != "" {
		var filtered []model.PromoCampaign
		for _, c := range campaigns {
			if string(c.Status) == statusFilter {
				filtered = append(filtered, c)
			}
		}
		campaigns = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": campaigns,
		"total": len(campaigns),
	})
}

// HandleAdminPromoCampaignUpdate updates a campaign (admin only).
// PATCH /admin/promo/campaigns/{id}
func (h *Handlers) HandleAdminPromoCampaignUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	id, ok := parseID(w, r, "promo_campaign_id")
	if !ok {
		return
	}

	c, err := h.promoCampaignRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "campaign not found")
		return
	}

	var req struct {
		Status         string               `json:"status,omitempty"`
		TargetFilters  *model.TargetFilters `json:"target_filters,omitempty"`
		TargetPosition string               `json:"target_position,omitempty"`
		BudgetTotal    float64              `json:"budget_total"`
		StartAt        string               `json:"start_at,omitempty"`
		EndAt          string               `json:"end_at,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	if err := h.promoCampaignRepo.Update(id, func(camp *model.PromoCampaign) {
		if req.Status != "" {
			camp.Status = model.PromoCampaignStatus(req.Status)
		}
		if req.TargetFilters != nil {
			camp.TargetFilters = *req.TargetFilters
		}
		if req.TargetPosition != "" {
			camp.TargetPosition = req.TargetPosition
		}
		if req.BudgetTotal > 0 {
			camp.BudgetTotal = req.BudgetTotal
		}
		if req.StartAt != "" {
			if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
				camp.StartAt = t
			}
		}
		if req.EndAt != "" {
			if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
				camp.EndAt = t
			}
		}
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ = h.promoCampaignRepo.Get(id)
	writeJSON(w, http.StatusOK, c)
}

// HandleCompanyPromoCampaignsList returns campaigns for a company.
// GET /companies/{companyId}/promo-campaigns
// Access: seller (own) or admin.
func (h *Handlers) HandleCompanyPromoCampaignsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /companies/{id}/promo-campaigns
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[2] == "" || parts[3] != "promo-campaigns" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	companyID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || companyID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
		return
	}

	// Access control
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	if ctxUser.Role != model.RoleAdmin {
		ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || companyID != ownerCompanyID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only view campaigns for your own company")
			return
		}
	}

	campaigns, err := h.promoCampaignRepo.ListByCompany(companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if campaigns == nil {
		campaigns = []model.PromoCampaign{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"company_id": companyID,
		"items":      campaigns,
		"total":      len(campaigns),
	})
}

// HandleCompanyPromoCampaignCreate creates a promo campaign (seller only, own company).
// POST /companies/{companyId}/promo-campaigns
func (h *Handlers) HandleCompanyPromoCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /companies/{id}/promo-campaigns
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[2] == "" || parts[3] != "promo-campaigns" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	companyID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || companyID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleSeller {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "seller access required")
		return
	}
	ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
	if err != nil || companyID != ownerCompanyID {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only create campaigns for your own company")
		return
	}

	var req struct {
		PromoPlanID    int64               `json:"promo_plan_id"`
		TargetFilters  model.TargetFilters `json:"target_filters"`
		TargetPosition string              `json:"target_position"`
		BudgetTotal    float64             `json:"budget_total"`
		StartAt        string              `json:"start_at,omitempty"`
		EndAt          string              `json:"end_at,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.PromoPlanID <= 0 || req.BudgetTotal <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "promo_plan_id > 0 and budget_total > 0 required")
		return
	}

	// Verify plan exists
	plan, err := h.promoPlanRepo.Get(req.PromoPlanID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "promo_plan not found")
		return
	}

	startAt := time.Now()
	endAt := startAt.AddDate(0, 0, plan.DurationDays)
	if req.StartAt != "" {
		if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
			startAt = t
		}
	}
	if req.EndAt != "" {
		if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
			endAt = t
		}
	}

	c := &model.PromoCampaign{
		CompanyID:      companyID,
		PromoPlanID:    req.PromoPlanID,
		Status:         model.PromoCampaignStatusPending,
		TargetFilters:  req.TargetFilters,
		TargetPosition: req.TargetPosition,
		BudgetTotal:    req.BudgetTotal,
		BudgetUsed:     0,
		StartAt:        startAt,
		EndAt:          endAt,
	}

	if err := h.promoCampaignRepo.Create(c); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, c)
}

// HandlePromoCampaignUpdateStatus updates campaign status.
// PATCH /promo-campaigns/{id}/status
// Access: seller (own company) or admin.
func (h *Handlers) HandlePromoCampaignUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// /promo-campaigns/{id}/status -> parts = ["", "promo-campaigns", "{id}", "status"]
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[1] != "promo-campaigns" || parts[3] != "status" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid campaign id")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "status is required")
		return
	}

	c, err := h.promoCampaignRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "campaign not found")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	if ctxUser.Role != model.RoleAdmin {
		ownerCompanyID, err := h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil || c.CompanyID != ownerCompanyID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only manage campaigns for your own company")
			return
		}
	}

	if err := h.promoCampaignRepo.UpdateStatus(id, model.PromoCampaignStatus(req.Status)); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ = h.promoCampaignRepo.Get(id)
	writeJSON(w, http.StatusOK, c)
}

// HandlePromoLogCreate records a promo event.
// POST /promo/logs
// Body: {"campaign_id": 1, "event_type": "impression", "context": {...}, "cost": 0.01}
func (h *Handlers) HandlePromoLogCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		CampaignID int64                  `json:"campaign_id"`
		EventType  string                 `json:"event_type"`
		Context    map[string]interface{} `json:"context,omitempty"`
		Cost       float64                `json:"cost"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.CampaignID <= 0 || req.EventType == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "campaign_id > 0 and event_type required")
		return
	}

	// Verify campaign exists and is active
	c, err := h.promoCampaignRepo.Get(req.CampaignID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "campaign not found")
		return
	}
	if c.Status != model.PromoCampaignStatusActive {
		writeError(w, http.StatusBadRequest, "INVALID_STATE", "campaign must be active")
		return
	}

	// Update budget_used
	if req.Cost > 0 {
		_ = h.promoCampaignRepo.Update(req.CampaignID, func(camp *model.PromoCampaign) {
			camp.BudgetUsed += req.Cost
		})
	}

	l := &model.PromoLog{
		CampaignID: req.CampaignID,
		EventType:  model.PromoEventType(req.EventType),
		Context:    req.Context,
		Cost:       req.Cost,
	}

	if err := h.promoLogRepo.Create(l); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, l)
}

// ================= Product Import handlers =================

// HandleAdminProductsReindex rebuilds all product indexes.
// POST /admin/products/reindex
func (h *Handlers) HandleAdminProductsReindex(w http.ResponseWriter, r *http.Request) {
	if err := h.productRepo.ReindexAll(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reindex completed"})
}

// ================= Product Import handlers =================

// HandleAdminProductsImport starts a product import from JSON file.
// POST /admin/products/import
// Content-Type: multipart/form-data
// Body: file (JSON array of products)
// Also accepts: company_id (query param or in JSON wrapper)
func (h *Handlers) HandleAdminProductsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Get company_id from query or from seller's company
	companyIDStr := r.URL.Query().Get("company_id")
	var companyID int64

	if companyIDStr != "" {
		var err error
		companyID, err = strconv.ParseInt(companyIDStr, 10, 64)
		if err != nil || companyID <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company_id")
			return
		}
	} else if hasUser && ctxUser.Role == model.RoleSeller {
		var err error
		companyID, err = h.companyRepo.GetCompanyIDByUserID(ctxUser.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "NO_COMPANY", "seller must have a company")
			return
		}
	} else {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "company_id is required")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "file is required")
		return
	}
	defer file.Close()

	// Limit file size
	if header.Size > 32<<20 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "file too large (max 32MB)")
		return
	}

	job, err := h.productImportRepo.CreateImportJob(file, companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"import_id": job.ID,
		"status":    job.Status,
	})
}

// HandleAdminImportStatus returns the status of an import job.
// GET /admin/products/import/{importId}
func (h *Handlers) HandleAdminImportStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Path: /admin/products/import/{id}
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "admin", "products", "import", "{id}"]
	if len(parts) < 5 || parts[3] != "import" || parts[4] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}

	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid import id")
		return
	}

	job, err := h.productImportRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "import job not found")
		return
	}

	writeJSON(w, http.StatusOK, job)
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
func (h *Handlers) HandleProductReviewCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /products/{id}/reviews
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "products", "{id}", "reviews"]
	if len(parts) < 4 || parts[2] == "" || parts[3] != "reviews" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	productID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || productID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	// Auth required
	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	// Only buyers can review
	if ctxUser.Role != model.RoleBuyer {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only buyers can write reviews")
		return
	}

	// Verify product exists
	_, err = h.productRepo.Get(productID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "product not found")
		return
	}

	var req struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	if req.Rating < 1 || req.Rating > 5 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "rating must be between 1 and 5")
		return
	}

	review := &model.Review{
		ProductID: productID,
		UserID:    ctxUser.ID,
		Rating:    req.Rating,
		Comment:   req.Comment,
	}

	if err := h.reviewRepo.Create(review); err != nil {
		if strings.Contains(err.Error(), "already reviewed") {
			writeError(w, http.StatusConflict, "ALREADY_REVIEWED", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, review)
}

// HandleProductReviewsList returns reviews for a product.
// GET /products/{id}/reviews?page=1&limit=50
// Public endpoint.
func (h *Handlers) HandleProductReviewsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /products/{id}/reviews
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[2] == "" || parts[3] != "reviews" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	productID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || productID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)

	reviews, total, err := h.reviewRepo.ListByProduct(productID, page, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if reviews == nil {
		reviews = []model.Review{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"items": reviews,
	})
}

// HandleUserReviewsList returns reviews created by a user.
// GET /reviews?user_id=123
// Auth: required (user can view their own reviews)
func (h *Handlers) HandleUserReviewsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	userIDStr := r.URL.Query().Get("user_id")
	var userID int64

	if userIDStr != "" {
		var err error
		userID, err = strconv.ParseInt(userIDStr, 10, 64)
		if err != nil || userID <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid user_id")
			return
		}
		// Users can only view their own reviews
		if userID != ctxUser.ID {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you can only view your own reviews")
			return
		}
	} else {
		userID = ctxUser.ID
	}

	page, _ := parseQueryInt(r.URL.Query().Get("page"), 1)
	limit, _ := parseQueryInt(r.URL.Query().Get("limit"), 50)

	reviews, total, err := h.reviewRepo.ListByUser(userID, page, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if reviews == nil {
		reviews = []model.Review{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"items": reviews,
	})
}

// --- Admin AttrDef handlers ---

// GET /admin/attrdefs — list all attribute definitions
func (h *Handlers) HandleAdminAttrDefsList(w http.ResponseWriter, r *http.Request) {
	defs, err := h.attrDefRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if defs == nil {
		defs = []model.AttrDef{}
	}
	writeJSON(w, http.StatusOK, defs)
}

// GET /admin/attrdefs/{code} — get attribute definition by code
func (h *Handlers) HandleAdminAttrDefGet(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}
	code := parts[3]

	ad, err := h.attrDefRepo.GetByCode(code)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "attribute not found")
		return
	}
	writeJSON(w, http.StatusOK, ad)
}

// POST /admin/attrdefs — create attribute definition
func (h *Handlers) HandleAdminAttrDefCreate(w http.ResponseWriter, r *http.Request) {
	var req model.AttrDef
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}

	if err := h.attrDefRepo.Create(req.Code, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// PATCH /admin/attrdefs/{code} — update attribute definition
func (h *Handlers) HandleAdminAttrDefUpdate(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}
	code := parts[3]

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}

	if err := h.attrDefRepo.Update(code, func(ad *model.AttrDef) {
		if v, ok := updates["name"].(string); ok {
			ad.Name = v
		}
		if v, ok := updates["type"].(string); ok && v != "" {
			ad.Type = model.AttrType(v)
		}
		if v, ok := updates["is_active"].(bool); ok {
			ad.IsActive = v
		}
		if v, ok := updates["is_filterable"].(bool); ok {
			ad.IsFilterable = v
		}
		if v, ok := updates["is_sortable"].(bool); ok {
			ad.IsSortable = v
		}
		if v, ok := updates["sort_order"].(float64); ok {
			ad.SortOrder = int(v)
		}
		if v, ok := updates["unit"].(string); ok {
			ad.Unit = v
		}
		if v, ok := updates["range_params"].([]interface{}); ok {
			params := make([]string, len(v))
			for i, p := range v {
				if s, ok := p.(string); ok {
					params[i] = s
				}
			}
			ad.RangeParams = params
		}
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Return updated attrdef
	ad, _ := h.attrDefRepo.GetByCode(code)
	writeJSON(w, http.StatusOK, ad)
}

// DELETE /admin/attrdefs/{code} — delete attribute definition
func (h *Handlers) HandleAdminAttrDefDelete(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}
	code := parts[3]

	if err := h.attrDefRepo.Delete(code); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted", "code": code})
}

// GET /admin/db/shards — fast shard usage stats (based on FreeOffset)
func (h *Handlers) HandleAdminDBShards(w http.ResponseWriter, r *http.Request) {
	usages := h.store.DB().ShardUsages()
	writeJSON(w, http.StatusOK, usages)
}

// GET /admin/db/shards/active — precise shard usage stats (full scan, slow)
func (h *Handlers) HandleAdminDBShardsActive(w http.ResponseWriter, r *http.Request) {
	usages := h.store.DB().ActiveUsage()
	writeJSON(w, http.StatusOK, usages)
}

// POST /admin/db/compact — compact all shards (slow, admin only)
func (h *Handlers) HandleAdminDBCompact(w http.ResponseWriter, r *http.Request) {
	db := h.store.DB()
	if err := db.CompactAllShards(1000); err != nil {
		writeError(w, http.StatusInternalServerError, "COMPACT_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "compact completed successfully"})
}
