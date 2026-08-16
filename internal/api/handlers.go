package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/catalogizer"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/metrics"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/tokenizer"
)

// slugFromNameEn generates a URL-safe slug from English category name.
func slugFromNameEn(name string) string {
	s := strings.ToLower(name)
	// Replace non-alnum / spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	s = b.String()
	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	// Trim leading/trailing hyphens
	return strings.Trim(s, "-")
}

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
	catalogizer       *catalogizer.Catalogizer

	// Company settings repos
	paymentMethodRepo   *db.PaymentMethodRepo
	deliveryTimeRepo    *db.DeliveryTimeRepo
	installmentPlanRepo *db.InstallmentPlanRepo

	// Stats cache
	statsCacheMu sync.Mutex
	statsCache   *metrics.Stats
	statsCacheAt time.Time
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
	turboSearch := db.NewTurboProductSearch(store, productRepo, categoryRepo, turboEnabled)
	productRepo.SetTurboSearch(turboSearch)

	landingRepo := db.NewLandingRepo(store)
	turboSearch.SetLandingRepo(landingRepo)

	scuPageRepo := db.NewSCUPageRepo(store)
	scuPageRepo.SetCategoryRepo(categoryRepo)
	scuPageRepo.EnableCatalogizeNew(false) // disabled: categories come from price files
	turboSearch.SetSCUPageRepo(scuPageRepo)

	// SCUPage search (catalog works on SCU pages)
	scuPageSearch := db.NewSCUPageSearch(store.DB(), scuPageRepo, productRepo, categoryRepo, turboEnabled)
	productRepo.SetSCUPageSearch(scuPageSearch)

	// Catalogizer
	catz := catalogizer.New(store, categoryRepo, productRepo)
	scuPageRepo.Catalogizer = catz // for TurboTopNByIntersection catalogization

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
		catalogizer:       catz,
	}
}

// Store returns the underlying store.
func (h *Handlers) Store() *db.Store {
	return h.store
}

// TurboSearch returns the attached TurboProductSearch.
func (h *Handlers) TurboSearch() *db.TurboProductSearch {
	return h.turboSearch
}

// SetCompanySettingsRepos attaches company settings repositories.
func (h *Handlers) SetCompanySettingsRepos(
	companyRepo *db.CompanyRepo,
	paymentMethodRepo *db.PaymentMethodRepo,
	deliveryTimeRepo *db.DeliveryTimeRepo,
	installmentPlanRepo *db.InstallmentPlanRepo,
) {
	h.companyRepo = companyRepo
	h.paymentMethodRepo = paymentMethodRepo
	h.deliveryTimeRepo = deliveryTimeRepo
	h.installmentPlanRepo = installmentPlanRepo
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
	if cats == nil {
		cats = []model.Category{}
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

// HandleCategoryTreePath returns the full path from root to category in one request.
// GET /categories/tree_path/{id}
// Returns: [{id, slug, name_ru, name_ua, name_pl, name_en}, ...]
func (h *Handlers) HandleCategoryTreePath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	// Verify category exists
	_, err := h.categoryRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "category not found")
		return
	}

	ancestors, err := h.categoryRepo.GetAncestors(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	type catPathItem struct {
		ID     int64  `json:"id"`
		Slug   string `json:"slug"`
		NameRu string `json:"name_ru"`
		NameUa string `json:"name_ua"`
		NamePl string `json:"name_pl"`
		NameEn string `json:"name_en"`
	}

	var path []catPathItem
	for _, aid := range ancestors {
		cat, err := h.categoryRepo.Get(aid)
		if err != nil {
			continue
		}
		path = append(path, catPathItem{
			ID:     cat.ID,
			Slug:   cat.Slug,
			NameRu: cat.NameRu,
			NameUa: cat.NameUa,
			NamePl: cat.NamePl,
			NameEn: cat.NameEn,
		})
	}

	// Add current category
	cat, _ := h.categoryRepo.Get(id)
	if cat != nil {
		path = append(path, catPathItem{
			ID:     cat.ID,
			Slug:   cat.Slug,
			NameRu: cat.NameRu,
			NameUa: cat.NameUa,
			NamePl: cat.NamePl,
			NameEn: cat.NameEn,
		})
	}

	writeJSON(w, http.StatusOK, path)
}

type CreateCategoryRequest struct {
	ParentID       *int64   `json:"parent_id,omitempty"`
	ParentIDSet    bool     `json:"-"` // tracks if parent_id was explicitly sent
	NameRu         string   `json:"name_ru"`
	NameUa         string   `json:"name_ua"`
	NamePl         string   `json:"name_pl"`
	NameEn         string   `json:"name_en"`
	Slug           string   `json:"slug"`
	Description    string   `json:"description,omitempty"`
	DescriptionRu  string   `json:"description_ru,omitempty"`
	DescriptionUa  string   `json:"description_ua,omitempty"`
	DescriptionPl  string   `json:"description_pl,omitempty"`
	DescriptionEn  string   `json:"description_en,omitempty"`
	ImageLightURL  string   `json:"image_light_url,omitempty"`
	ImageDarkURL   string   `json:"image_dark_url,omitempty"`
	IsActive       bool     `json:"is_active"`
	IsActivePtr    *bool    `json:"-"` // pointer to detect if is_active was explicitly sent in PATCH
	SortOrder      int      `json:"sort_order"`
	AnchorKeywords []string `json:"anchor_keywords,omitempty"` // keywords for auto-catalogization
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

	nameEn := req.NameEn
	if nameEn == "" && req.NameRu != "" {
		nameEn = req.NameRu // fallback for slug if name_en not set
	}

	slug := req.Slug
	if slug == "" {
		slug = slugFromNameEn(nameEn)
	}

	cat := &model.Category{
		ParentID:  req.ParentID,
		NameRu:    req.NameRu,
		NameUa:    req.NameUa,
		NamePl:    req.NamePl,
		NameEn:    nameEn,
		Slug:      slug,
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

	// Read raw JSON to detect which fields are present
	var raw map[string]interface{}
	if !readJSON(w, r, &raw) {
		return
	}

	// Parse into struct
	req := CreateCategoryRequest{}
	if v, ok := raw["parent_id"]; ok && v != nil {
		if f, ok := v.(float64); ok {
			p := int64(f)
			req.ParentID = &p
		}
	}
	if v, ok := raw["name_ru"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.NameRu = s
		}
	}
	if v, ok := raw["name_ua"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.NameUa = s
		}
	}
	if v, ok := raw["name_pl"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.NamePl = s
		}
	}
	if v, ok := raw["name_en"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.NameEn = s
		}
	}
	if v, ok := raw["slug"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.Slug = s
		}
	}
	if v, ok := raw["description"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.Description = s
		}
	}
	if v, ok := raw["description_ru"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.DescriptionRu = s
		}
	}
	if v, ok := raw["description_ua"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.DescriptionUa = s
		}
	}
	if v, ok := raw["description_pl"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.DescriptionPl = s
		}
	}
	if v, ok := raw["description_en"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.DescriptionEn = s
		}
	}
	if v, ok := raw["image_light_url"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.ImageLightURL = s
		}
	}
	if v, ok := raw["image_dark_url"]; ok && v != nil {
		if s, ok := v.(string); ok {
			req.ImageDarkURL = s
		}
	}
	if v, ok := raw["is_active"]; ok && v != nil {
		if b, ok := v.(bool); ok {
			req.IsActive = b
			req.IsActivePtr = &b
		}
	}
	if v, ok := raw["sort_order"]; ok && v != nil {
		if f, ok := v.(float64); ok {
			req.SortOrder = int(f)
		}
	}
	if v, ok := raw["anchor_keywords"]; ok && v != nil {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					req.AnchorKeywords = append(req.AnchorKeywords, s)
				}
			}
		}
	}

	if err := h.categoryRepo.Update(id, func(c *model.Category) {
		if req.ParentID != nil {
			c.ParentID = req.ParentID
		}
		if req.NameRu != "" {
			c.NameRu = req.NameRu
		}
		if req.NameUa != "" {
			c.NameUa = req.NameUa
		}
		if req.NamePl != "" {
			c.NamePl = req.NamePl
		}
		if req.NameEn != "" {
			c.NameEn = req.NameEn
		}

		// If slug explicitly provided, use it; otherwise regenerate from name_en
		if req.Slug != "" {
			c.Slug = req.Slug
		} else if c.NameEn != "" && c.Slug == "" {
			c.Slug = slugFromNameEn(c.NameEn)
		}

		if req.Description != "" {
			c.Desc = req.Description
		}
		if req.DescriptionRu != "" {
			c.DescRu = req.DescriptionRu
		}
		if req.DescriptionUa != "" {
			c.DescUa = req.DescriptionUa
		}
		if req.DescriptionPl != "" {
			c.DescPl = req.DescriptionPl
		}
		if req.DescriptionEn != "" {
			c.DescEn = req.DescriptionEn
		}
		if req.ImageLightURL != "" {
			c.ImageLightURL = req.ImageLightURL
		}
		if req.ImageDarkURL != "" {
			c.ImageDarkURL = req.ImageDarkURL
		}
		if req.SortOrder != 0 {
			c.SortOrder = req.SortOrder
		}
		// Only update IsActive if explicitly provided in PATCH
		if req.IsActivePtr != nil {
			c.IsActive = *req.IsActivePtr
		}
		if req.AnchorKeywords != nil {
			c.AnchorKeywords = req.AnchorKeywords
		}
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	cat, _ := h.categoryRepo.Get(id)
	// Rebuild catalogizer tokens for this category
	if cat != nil {
		_ = h.catalogizer.BuildTokensForCategory(cat)
	}
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
		Code    string   `json:"code"`
		NameRu  string   `json:"name_ru,omitempty"`
		NameUa  string   `json:"name_ua,omitempty"`
		NamePl  string   `json:"name_pl,omitempty"`
		NameEn  string   `json:"name_en,omitempty"`
		Type    string   `json:"type,omitempty"`
		Options []string `json:"options,omitempty"`
		Values  []string `json:"values,omitempty"`
	}

	var attrs []attrInfo
	for _, code := range codes {
		def, _ := h.attrDefRepo.GetByCode(code)
		values, _ := h.attrDefRepo.GetAttrValuesForCategory(code, catID)

		info := attrInfo{
			Code:   code,
			Values: values,
		}
		if def != nil {
			info.NameRu = def.NameRu
			info.NameUa = def.NameUa
			info.NamePl = def.NamePl
			info.NameEn = def.NameEn
			info.Type = string(def.Type)
		}
		attrs = append(attrs, info)
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
		_ = h.store.TurboWrite("attrdef_cats:"+code, []byte{})
	} else {
		buf := makodb.TurboBinaryNew(db.Uint64SliceFromInt64(newCats))
		_ = h.store.TurboWrite("attrdef_cats:"+code, buf)
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
	SKU         string           `json:"sku"`
	SCU         string           `json:"scu,omitempty"` // Standard Catalog Unit — links to landing page
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
// If user has no cart, creates one automatically.
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
		// No cart yet — create one
		cart, err = h.cartRepo.CreateForUser(ctxUser.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create cart")
			return
		}
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
	return
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
		Name         string           `json:"name"`
		Type         string           `json:"type"`
		DurationDays int              `json:"duration_days"`
		Price        float64          `json:"price"`
		Currency     string           `json:"currency"`
		Description  string           `json:"description,omitempty"`
		Constraints  []model.KeyValue `json:"constraints,omitempty"`
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
		Name         string           `json:"name,omitempty"`
		Type         string           `json:"type,omitempty"`
		DurationDays int              `json:"duration_days"`
		Price        float64          `json:"price"`
		Currency     string           `json:"currency,omitempty"`
		Description  string           `json:"description,omitempty"`
		Constraints  []model.KeyValue `json:"constraints,omitempty"`
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

	startAt := time.Now().Unix()
	endAt := startAt + int64(plan.DurationDays)*86400
	if req.StartAt != "" {
		if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
			startAt = t.Unix()
		}
	}
	if req.EndAt != "" {
		if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
			endAt = t.Unix()
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
				camp.StartAt = t.Unix()
			}
		}
		if req.EndAt != "" {
			if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
				camp.EndAt = t.Unix()
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

	startAt := time.Now().Unix()
	endAt := startAt + int64(plan.DurationDays)*86400
	if req.StartAt != "" {
		if t, e := time.Parse(time.RFC3339, req.StartAt); e == nil {
			startAt = t.Unix()
		}
	}
	if req.EndAt != "" {
		if t, e := time.Parse(time.RFC3339, req.EndAt); e == nil {
			endAt = t.Unix()
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
		CampaignID int64            `json:"campaign_id"`
		EventType  string           `json:"event_type"`
		Context    []model.KeyValue `json:"context,omitempty"`
		Cost       float64          `json:"cost"`
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

// HandleAdminProductsDeleteAll deletes all products and related indexes.
// POST /admin/products/delete-all
// WARNING: This is a destructive operation.
func (h *Handlers) HandleAdminProductsDeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, hasUser := auth.ContextUserFrom(r)
	if !hasUser || ctxUser.Role != model.RoleAdmin {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
		return
	}

	// Optional: require confirmation in body
	type req struct {
		Confirm bool `json:"confirm"`
	}
	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !body.Confirm {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "set confirm=true in body")
		return
	}

	if err := h.productRepo.DeleteAllProducts(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "all products deleted"})
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
		if v, ok := updates["name_ru"].(string); ok {
			ad.NameRu = v
		}
		if v, ok := updates["name_ua"].(string); ok {
			ad.NameUa = v
		}
		if v, ok := updates["name_pl"].(string); ok {
			ad.NamePl = v
		}
		if v, ok := updates["name_en"].(string); ok {
			ad.NameEn = v
		}
		// Legacy: "name" -> NameRu
		if v, ok := updates["name"].(string); ok {
			ad.NameRu = v
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

// ================= SCUPage Admin handlers =================

// HandleAdminSCUPageList returns a paginated list of SCU pages.
// GET /admin/scupages?page=1&limit=50&q=search
func (h *Handlers) HandleAdminSCUPageList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	q := r.URL.Query().Get("q")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	page := 1
	limit := 50
	if pageStr != "" {
		page, _ = strconv.Atoi(pageStr)
	}
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}

	// Use SCUPageSearch for listing with optional search
	params := db.SCUPageListParams{
		Q:     q,
		Page:  page,
		Limit: limit,
	}

	result, err := h.scuPageSearch.ListWithTurbo(params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Convert raw JSON items to readable format
	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, raw := range result.Items {
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		items = append(items, m)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"total": result.Total,
		"page":  page,
		"limit": limit,
	})
}

// HandleAdminSCUPageGet returns a single SCU page by ID.
// GET /admin/scupages/{id}
func (h *Handlers) HandleAdminSCUPageGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	sp, err := h.scuPageRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, sp)
}

// HandleAdminSCUPageUpdate updates a SCU page.
// PATCH /admin/scupages/{id}
// Body: any subset of SCUPage fields
func (h *Handlers) HandleAdminSCUPageUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	var updates map[string]interface{}
	if !readJSON(w, r, &updates) {
		return
	}

	// Apply updates
	updater := func(sp *model.SCUPage) {
		if v, ok := updates["title"]; ok {
			if s, ok := v.(string); ok {
				sp.Title = s
			}
		}
		if v, ok := updates["description"]; ok {
			if s, ok := v.(string); ok {
				sp.Description = s
			}
		}
		if v, ok := updates["content"]; ok {
			if s, ok := v.(string); ok {
				sp.Content = s
			}
		}
		if v, ok := updates["slug"]; ok {
			if s, ok := v.(string); ok {
				sp.Slug = s
			}
		}
		if v, ok := updates["is_active"]; ok {
			if b, ok := v.(bool); ok {
				sp.IsActive = b
			}
		}
		if v, ok := updates["category_id"]; ok {
			if f, ok := v.(float64); ok {
				sp.CategoryID = int64(f)
			}
		}
		if v, ok := updates["images"]; ok {
			if arr, ok := v.([]interface{}); ok {
				imgs := make([]string, 0, len(arr))
				for _, img := range arr {
					if s, ok := img.(string); ok {
						imgs = append(imgs, s)
					}
				}
				sp.Images = imgs
			}
		}
		if v, ok := updates["attributes"]; ok {
			if arr, ok := v.([]interface{}); ok {
				var kvs []model.KeyValue
				for _, item := range arr {
					if kv, ok := item.(map[string]interface{}); ok {
						k := fmt.Sprintf("%v", kv["key"])
						val := fmt.Sprintf("%v", kv["value"])
						kvs = append(kvs, model.KeyValue{Key: k, Value: val})
					}
				}
				sp.Attributes = kvs
			}
		}
	}

	if err := h.scuPageRepo.Update(id, updater); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Reindex SCU page
	sp, _ := h.scuPageRepo.Get(id)
	if sp != nil {
		_ = h.scuPageSearch.UnindexSCUPage(sp)
		_ = h.scuPageSearch.IndexSCUPage(sp)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// HandleAdminSCUPageDelete deletes a SCU page.
// DELETE /admin/scupages/{id}
func (h *Handlers) HandleAdminSCUPageDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	sp, err := h.scuPageRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	// Unindex
	_ = h.scuPageSearch.UnindexSCUPage(sp)

	// Delete
	if err := h.scuPageRepo.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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

// ================= Stats handlers =================

// HandleAdminStats returns aggregated request metrics from ./_tmp/metrics.
// GET /admin/stats?refresh=1 — force refresh cache
func (h *Handlers) HandleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	const cacheTTL = 30 * time.Second

	refresh := r.URL.Query().Get("refresh") == "1"

	h.statsCacheMu.Lock()
	defer h.statsCacheMu.Unlock()

	now := time.Now()
	if !refresh && h.statsCache != nil && now.Sub(h.statsCacheAt) < cacheTTL {
		writeJSON(w, http.StatusOK, h.statsCache)
		return
	}

	stats, err := metrics.ParseMetricsStats("./_tmp/metrics")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_ERROR", err.Error())
		return
	}

	h.statsCache = stats
	h.statsCacheAt = now

	writeJSON(w, http.StatusOK, stats)
}

// ================= Catalogizer handlers =================

// HandleAdminCatalogizerTrain rebuilds token indexes from category anchor_keywords.
// POST /admin/catalogizer/train
func (h *Handlers) HandleAdminCatalogizerTrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	fmt.Println("[CATALOGIZER-TRAIN] Rebuilding token indexes from anchor_keywords...")

	if err := h.catalogizer.RebuildAllCategoryTokens(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "completed",
		"message": "Token indexes rebuilt from anchor_keywords",
	})
}

// HandleAdminCatalogizerCoverage returns coverage statistics.
// GET /admin/catalogizer/coverage
func (h *Handlers) HandleAdminCatalogizerCoverage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	categories, err := h.categoryRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	total := len(categories)
	withKeywords := 0
	empty := 0
	active := 0
	var fewTokens []map[string]interface{}
	var manyTokens []map[string]interface{}

	for _, cat := range categories {
		kwCount := len(cat.AnchorKeywords)
		if cat.IsActive {
			active++
		}
		if kwCount == 0 {
			empty++
		} else {
			withKeywords++
			catName := cat.NameEn
			if catName == "" {
				catName = cat.NameRu
			}
			if kwCount < 5 {
				fewTokens = append(fewTokens, map[string]interface{}{
					"id":          cat.ID,
					"name":        catName,
					"token_count": kwCount,
				})
			}
			if kwCount > 30 {
				manyTokens = append(manyTokens, map[string]interface{}{
					"id":          cat.ID,
					"name":        catName,
					"token_count": kwCount,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_categories": total,
		"with_keywords":    withKeywords,
		"empty":            empty,
		"active":           active,
		"few_tokens":       fewTokens,
		"many_tokens":      manyTokens,
	})
}

// HandleAdminCatalogize runs auto-catalogization on products.
// POST /admin/catalogize
//
//	Body: {
//	  "apply": true/false,         // if true, updates product categories (default: false)
//	  "limit": 1000,              // max products to process (default: 1000)
//	  "category_id": 123,         // optional: only products in this category
//	  "company_id": 456           // optional: only products from this company
//	}
func (h *Handlers) HandleAdminCatalogize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	type req struct {
		Apply      bool  `json:"apply"`
		Limit      int   `json:"limit"`
		CategoryID int64 `json:"category_id,omitempty"`
		CompanyID  int64 `json:"company_id,omitempty"`
	}

	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Limit <= 0 {
		body.Limit = 1000
	}
	if body.Limit > 10000 {
		body.Limit = 10000
	}

	// Collect product IDs to catalogize
	var productIDs []int64

	// Use turbo search to get products
	result, err := h.turboSearch.ListWithTurbo(db.TurboListParams{
		CategoryID: body.CategoryID,
		CompanyID:  body.CompanyID,
		Page:       1,
		Limit:      body.Limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	for _, item := range result.Items {
		productIDs = append(productIDs, item.ID)
	}

	fmt.Printf("[CATALOGIZE] Processing %d products (apply=%v)...\n", len(productIDs), body.Apply)

	results, err := h.catalogizer.BatchCatalogize(productIDs, body.Apply)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	fmt.Printf("[CATALOGIZE] Done. %d products matched categories.\n", len(results))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"processed": len(productIDs),
		"matched":   len(results),
		"apply":     body.Apply,
		"results":   results[:min(len(results), 100)], // limit response size
	})
}

// HandleAdminCatalogizeSingle catalogizes a single product by ID.
// POST /admin/catalogize/product/{id}
// Body: { "apply": true/false }
func (h *Handlers) HandleAdminCatalogizeSingle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 || parts[4] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "product id is required")
		return
	}
	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	type req struct {
		Apply bool `json:"apply"`
	}
	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)

	p, err := h.productRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	result, err := h.catalogizer.CatalogizeProduct(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if result != nil && body.Apply {
		if err := h.catalogizer.ApplyCategory(p, result); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"product_id": id,
		"result":     result,
	})
}

// HandleAdminCatalogizerTest tests catalogization on a product name.
// POST /admin/catalogizer/test
// Body: { "name": "Product name here" }
// Returns ALL matching categories sorted by relevance.
func (h *Handlers) HandleAdminCatalogizerTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	type req struct {
		Name string `json:"name"`
	}
	var body req
	if !readJSON(w, r, &body) {
		return
	}

	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "name is required")
		return
	}

	// Get all matching categories
	matches := h.catalogizer.MatchProductToCategories(body.Name)

	// Get product tokens for display
	tokens := catalogizer.TokenizeName(body.Name)
	tokenWords := make([]string, len(tokens))
	for i, t := range tokens {
		tokenWords[i] = t.Word
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":        body.Name,
		"tokens":      tokenWords,
		"matches":     matches,
		"match_count": len(matches),
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ================= Category Import/Export handlers =================

// HandleAdminCategoriesExport exports category tree with anchor keywords and attributes.
// GET /api/admin/categories/export
func (h *Handlers) HandleAdminCategoriesExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	categories, err := h.categoryRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Build export structure
	type ExportCategory struct {
		ID             int64    `json:"id,omitempty"`
		ParentID       *int64   `json:"parent_id,omitempty"`
		NameRu         string   `json:"name_ru"`
		NameUa         string   `json:"name_ua"`
		NamePl         string   `json:"name_pl"`
		NameEn         string   `json:"name_en"`
		Slug           string   `json:"slug"`
		Description    string   `json:"description,omitempty"`
		DescriptionRu  string   `json:"description_ru,omitempty"`
		DescriptionUa  string   `json:"description_ua,omitempty"`
		DescriptionPl  string   `json:"description_pl,omitempty"`
		DescriptionEn  string   `json:"description_en,omitempty"`
		ImageLightURL  string   `json:"image_light_url,omitempty"`
		ImageDarkURL   string   `json:"image_dark_url,omitempty"`
		IsActive       bool     `json:"is_active"`
		SortOrder      int      `json:"sort_order"`
		AnchorKeywords []string `json:"anchor_keywords,omitempty"`
		Attributes     []string `json:"attributes,omitempty"` // attribute codes
	}

	var export []ExportCategory
	for _, cat := range categories {
		// Get attribute codes for this category
		attrCodes, _ := h.attrDefRepo.GetCodesForCategory(cat.ID)

		export = append(export, ExportCategory{
			ID:             cat.ID,
			ParentID:       cat.ParentID,
			NameRu:         cat.NameRu,
			NameUa:         cat.NameUa,
			NamePl:         cat.NamePl,
			NameEn:         cat.NameEn,
			Slug:           cat.Slug,
			Description:    cat.Desc,
			DescriptionRu:  cat.DescRu,
			DescriptionUa:  cat.DescUa,
			DescriptionPl:  cat.DescPl,
			DescriptionEn:  cat.DescEn,
			ImageLightURL:  cat.ImageLightURL,
			ImageDarkURL:   cat.ImageDarkURL,
			IsActive:       cat.IsActive,
			SortOrder:      cat.SortOrder,
			AnchorKeywords: cat.AnchorKeywords,
			Attributes:     attrCodes,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"categories":  export,
		"total":       len(export),
	})
}

// HandleAdminCategoriesImport imports category tree from JSON.
// POST /api/admin/categories/import
// Body: { "categories": [ { "name", "parent_id", "slug", "anchor_keywords", "attributes" } ] }
// Existing categories are matched by name+parent. New ones are created.
func (h *Handlers) HandleAdminCategoriesImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	type ImportCategory struct {
		ID             int64    `json:"id,omitempty"`
		ParentID       *int64   `json:"parent_id,omitempty"`
		NameRu         string   `json:"name_ru"`
		NameUa         string   `json:"name_ua"`
		NamePl         string   `json:"name_pl"`
		NameEn         string   `json:"name_en"`
		Slug           string   `json:"slug"`
		Description    string   `json:"description,omitempty"`
		DescriptionRu  string   `json:"description_ru,omitempty"`
		DescriptionUa  string   `json:"description_ua,omitempty"`
		DescriptionPl  string   `json:"description_pl,omitempty"`
		DescriptionEn  string   `json:"description_en,omitempty"`
		ImageLightURL  string   `json:"image_light_url,omitempty"`
		ImageDarkURL   string   `json:"image_dark_url,omitempty"`
		IsActive       bool     `json:"is_active"`
		SortOrder      int      `json:"sort_order"`
		AnchorKeywords []string `json:"anchor_keywords,omitempty"`
		Attributes     []string `json:"attributes,omitempty"`
	}

	type ImportRequest struct {
		Categories []ImportCategory `json:"categories"`
	}

	var req ImportRequest
	if !readJSON(w, r, &req) {
		return
	}

	if len(req.Categories) == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "no categories provided")
		return
	}

	// Map: oldID -> newID (for relinking later)
	oldIDToNewID := make(map[int64]int64)
	// Map: name+parent -> newID (for finding existing)
	nameParentToID := make(map[string]int64)

	// Build existing categories map (use name_en as primary key; fallback to name_ru)
	existing, _ := h.categoryRepo.ListAll()
	for _, cat := range existing {
		name := cat.NameEn
		if name == "" {
			name = cat.NameRu
		}
		key := name + "|"
		if cat.ParentID != nil {
			key += fmt.Sprintf("%d", *cat.ParentID)
		}
		nameParentToID[key] = cat.ID
	}

	created := 0
	updated := 0

	// First pass: create/update categories
	for _, ic := range req.Categories {
		nameEn := ic.NameEn
		if nameEn == "" && ic.NameRu != "" {
			nameEn = ic.NameRu
		}
		if nameEn == "" {
			continue
		}

		// Determine parent ID
		var dbParentID *int64
		if ic.ParentID != nil {
			// Map old parent ID to new
			if newParentID, ok := oldIDToNewID[*ic.ParentID]; ok {
				dbParentID = &newParentID
			}
		}

		// Find existing category by name_en+parent
		key := nameEn + "|"
		if dbParentID != nil {
			key += fmt.Sprintf("%d", *dbParentID)
		}

		if existingID, ok := nameParentToID[key]; ok {
			// Update existing
			h.categoryRepo.Update(existingID, func(c *model.Category) {
				if ic.NameRu != "" {
					c.NameRu = ic.NameRu
				}
				if ic.NameUa != "" {
					c.NameUa = ic.NameUa
				}
				if ic.NamePl != "" {
					c.NamePl = ic.NamePl
				}
				if ic.NameEn != "" {
					c.NameEn = ic.NameEn
				}
				if ic.Slug != "" {
					c.Slug = ic.Slug
				} else if c.Slug == "" {
					c.Slug = slugFromNameEn(c.NameEn)
				}
				if ic.Description != "" {
					c.Desc = ic.Description
				}
				if ic.DescriptionRu != "" {
					c.DescRu = ic.DescriptionRu
				}
				if ic.DescriptionUa != "" {
					c.DescUa = ic.DescriptionUa
				}
				if ic.DescriptionPl != "" {
					c.DescPl = ic.DescriptionPl
				}
				if ic.DescriptionEn != "" {
					c.DescEn = ic.DescriptionEn
				}
				if ic.ImageLightURL != "" {
					c.ImageLightURL = ic.ImageLightURL
				}
				if ic.ImageDarkURL != "" {
					c.ImageDarkURL = ic.ImageDarkURL
				}
				c.IsActive = ic.IsActive
				if ic.SortOrder != 0 {
					c.SortOrder = ic.SortOrder
				}
				if ic.AnchorKeywords != nil {
					c.AnchorKeywords = ic.AnchorKeywords
				}
			})
			updated++

			// Map old ID to existing new ID
			if ic.ID != 0 {
				oldIDToNewID[ic.ID] = existingID
			}
		} else {
			// Create new
			slug := ic.Slug
			if slug == "" {
				slug = slugFromNameEn(nameEn)
			}
			cat := &model.Category{
				NameRu:         ic.NameRu,
				NameUa:         ic.NameUa,
				NamePl:         ic.NamePl,
				NameEn:         nameEn,
				Slug:           slug,
				ParentID:       dbParentID,
				Desc:           ic.Description,
				DescRu:         ic.DescriptionRu,
				DescUa:         ic.DescriptionUa,
				DescPl:         ic.DescriptionPl,
				DescEn:         ic.DescriptionEn,
				ImageLightURL:  ic.ImageLightURL,
				ImageDarkURL:   ic.ImageDarkURL,
				IsActive:       ic.IsActive,
				SortOrder:      ic.SortOrder,
				AnchorKeywords: ic.AnchorKeywords,
			}
			if err := h.categoryRepo.Create(cat); err != nil {
				fmt.Printf("WARN: create category %s: %v\n", nameEn, err)
				continue
			}
			created++

			// Map old ID to new ID
			if ic.ID != 0 {
				oldIDToNewID[ic.ID] = cat.ID
			}
			nameParentToID[key] = cat.ID
		}
	}

	// Second pass: import attributes
	for _, ic := range req.Categories {
		newID := oldIDToNewID[ic.ID]
		if newID == 0 {
			// Try to find by name+parent
			nameKey := ic.NameEn
			if nameKey == "" {
				nameKey = ic.NameRu
			}
			key := nameKey + "|"
			if ic.ParentID != nil {
				if newParentID, ok := oldIDToNewID[*ic.ParentID]; ok {
					key += fmt.Sprintf("%d", newParentID)
				}
			}
			newID = nameParentToID[key]
		}
		if newID == 0 {
			continue
		}

		for _, code := range ic.Attributes {
			if code == "" {
				continue
			}
			// Ensure AttrDef exists
			_, _ = h.attrDefRepo.GetOrCreate(code)
			// Add to category
			_ = h.attrDefRepo.AddCodeToCategory(code, newID)
		}
	}

	fmt.Printf("[CATEGORY-IMPORT] Done: created=%d updated=%d\n", created, updated)

	// Rebuild category indexes after import
	fmt.Println("[CATEGORY-IMPORT] Rebuilding category indexes...")
	if err := h.categoryRepo.RebuildAllIndexes(); err != nil {
		fmt.Printf("[CATEGORY-IMPORT] WARN: rebuild indexes failed: %v\n", err)
	}

	// Rebuild catalogizer token indexes from anchor_keywords
	fmt.Println("[CATEGORY-IMPORT] Rebuilding catalogizer token indexes...")
	if err := h.catalogizer.RebuildAllCategoryTokens(); err != nil {
		fmt.Printf("[CATEGORY-IMPORT] WARN: rebuild catalogizer tokens failed: %v\n", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "completed",
		"created":    created,
		"updated":    updated,
		"total":      created + updated,
		"old_id_map": oldIDToNewID,
		"message":    "Categories imported. Indexes and catalogizer rebuilt.",
	})
}

// HandleAdminSCUPageRelink reassigns SCU pages to categories based on anchor keywords.
// POST /api/admin/scupages/relink
// Body: { "limit": 10000, "apply": true }
func (h *Handlers) HandleAdminSCUPageRelink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	type req struct {
		Limit int  `json:"limit"`
		Apply bool `json:"apply"`
	}

	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Limit <= 0 {
		body.Limit = 10000
	}
	if body.Limit > 50000 {
		body.Limit = 50000
	}

	fmt.Printf("[SCUPAGE-RELINK] Relinking SCU pages (limit=%d, apply=%v)...\n", body.Limit, body.Apply)

	// Get all SCU pages
	all, err := h.scuPageRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if len(all) > body.Limit {
		all = all[:body.Limit]
	}

	// Get all categories with anchor keywords
	categories, err := h.categoryRepo.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	relinked := 0
	type RelinkResult struct {
		SCUPageID     int64  `json:"scupage_id"`
		SCU           string `json:"scu"`
		OldCategoryID int64  `json:"old_category_id"`
		NewCategoryID int64  `json:"new_category_id"`
	}

	var results []RelinkResult

	for i := range all {
		sp := &all[i]

		// Build text from SCU page
		text := strings.ToLower(sp.Title + " " + sp.Description + " " + sp.Content)
		for _, kv := range sp.Attributes {
			if len(kv.Value) > 0 {
				text += " " + strings.ToLower(string(kv.Value))
			}
		}

		var bestCatID int64
		var bestScore float64

		for _, cat := range categories {
			if !cat.IsActive || len(cat.AnchorKeywords) == 0 {
				continue
			}

			score := 0.0
			for _, kw := range cat.AnchorKeywords {
				kwLower := strings.ToLower(strings.TrimSpace(kw))
				if kwLower == "" {
					continue
				}
				if strings.Contains(text, " "+kwLower+" ") ||
					strings.HasPrefix(text, kwLower+" ") ||
					strings.HasSuffix(text, " "+kwLower) ||
					text == kwLower {
					score += 3.0
				} else if strings.Contains(text, kwLower) {
					score += 1.0
				}
			}

			if score > bestScore {
				bestScore = score
				bestCatID = cat.ID
			}
		}

		if bestScore > 0 && bestCatID != sp.CategoryID {
			if body.Apply {
				h.scuPageRepo.Update(sp.ID, func(s *model.SCUPage) {
					s.CategoryID = bestCatID
				})
				// Reindex
				if updated, err := h.scuPageRepo.Get(sp.ID); err == nil {
					_ = h.scuPageSearch.UnindexSCUPage(updated)
					_ = h.scuPageSearch.IndexSCUPage(updated)
				}
			}
			relinked++
			results = append(results, RelinkResult{
				SCUPageID:     sp.ID,
				SCU:           sp.SCU,
				OldCategoryID: sp.CategoryID,
				NewCategoryID: bestCatID,
			})
		}
	}

	fmt.Printf("[SCUPAGE-RELINK] Done. Relinked %d SCU pages.\n", relinked)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"processed": len(all),
		"relinked":  relinked,
		"apply":     body.Apply,
		"results":   results[:min(len(results), 100)],
	})
}

// HandleAdminSCUPageCatalogizeAll re-catalogizes all SCU pages using TurboTopNByIntersection.
// POST /admin/scupages/catalogize-all
// Body: { "apply": true }
func (h *Handlers) HandleAdminSCUPageCatalogizeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	type req struct {
		Apply bool `json:"apply"`
		Force bool `json:"force"`
	}

	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)

	rebuildAllIndexes := body.Apply || body.Force
	fmt.Printf("[SCUPAGE-CATALOGIZE-ALL] Starting (apply=%v force=%v rebuildAllIndexes=%v)...\n", body.Apply, body.Force, rebuildAllIndexes)

	// Get all SCU pages
	all, err := h.scuPageRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	fmt.Printf("[SCUPAGE-CATALOGIZE-ALL] Total SCU pages to process: %d\n", len(all))

	// Get catalogizer interface
	catz := h.catalogizer

	catalogized := 0
	var results []map[string]interface{}

	for i := range all {
		sp := &all[i]

		// Build tokens for this SCU page using all available text
		//todo need remove and add to insert|update only
		fullText := tokenizer.BuildSCUTokensFullText(sp.Title, sp.Description, sp.Content, sp.Attributes)
		if err := catz.BuildSCUTokens(sp.ID, fullText); err != nil {
			fmt.Printf("WARN: build tokens for scupage %d: %v\n", sp.ID, err)
			continue
		}

		if i%20000 == 0 {
			fmt.Printf("[SCUPAGE-CATALOGIZE-ALL] Processed %d SCU pages from total %d. Catalogized %d...\n", i, len(all), catalogized)
		}

		// Catalogize using TurboTopNByIntersection
		newCatID, err := catz.CatalogizeSCUPageByIntersection(sp.ID)
		if err != nil {
			fmt.Printf("WARN: catalogize scupage %d: %v\n", sp.ID, err)
			continue
		}

		if newCatID > 0 && newCatID != sp.CategoryID {
			if body.Apply {
				if err := h.scuPageRepo.Update(sp.ID, func(s *model.SCUPage) {
					s.CategoryID = newCatID
				}); err != nil {
					fmt.Printf("WARN: update scupage %d: %v\n", sp.ID, err)
					continue
				}
			}
			catalogized++
			results = append(results, map[string]interface{}{
				"scupage_id":      sp.ID,
				"scu":             sp.SCU,
				"old_category_id": sp.CategoryID,
				"new_category_id": newCatID,
			})
		}
	}

	// Full rebuild of all SCU page indexes if Apply or Force.
	// RebuildAllIndexes:
	//   1) clears all indexable keys (cat, brand, vendor, sort, numSort)
	//   2) streams all SCUPage and rebuilds indexes in batches
	//   3) rebuilds sort/numSort indexes
	// This avoids per-document deletes (vacuum) and ensures no stale indexes.
	if rebuildAllIndexes {
		fmt.Printf("[SCUPAGE-CATALOGIZE-ALL] Starting full index rebuild...\n")
		if err := h.scuPageSearch.RebuildAllIndexes(); err != nil {
			fmt.Printf("WARN: rebuild all indexes: %v\n", err)
		}
		fmt.Printf("[SCUPAGE-CATALOGIZE-ALL] Full index rebuild done.\n")
	}

	fmt.Printf("[SCUPAGE-CATALOGIZE-ALL] Done. Catalogized %d SCU pages.\n", catalogized)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"processed":   len(all),
		"catalogized": catalogized,
		"apply":       body.Apply,
		"results":     results[:min(len(results), 100)],
	})
}

// HandleAdminSCUPageRebuildTokens rebuilds token indexes for all SCU pages.
// POST /admin/scupages/rebuild-tokens
// Body: { "limit": 0 } (0 = all)
func (h *Handlers) HandleAdminSCUPageRebuildTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	type req struct {
		Limit int `json:"limit"`
	}

	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)

	fmt.Printf("[SCUPAGE-REBUILD-TOKENS] Starting (limit=%d)...\n", body.Limit)

	all, err := h.scuPageRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if body.Limit > 0 && len(all) > body.Limit {
		all = all[:body.Limit]
	}

	catz := h.catalogizer
	rebuilt := 0

	for i := range all {
		sp := &all[i]
		fullText := tokenizer.BuildSCUTokensFullText(sp.Title, sp.Description, sp.Content, sp.Attributes)
		if err := catz.BuildSCUTokens(sp.ID, fullText); err != nil {
			continue
		}
		rebuilt++
	}

	fmt.Printf("[SCUPAGE-REBUILD-TOKENS] Done. Rebuilt %d SCU page tokens.\n", rebuilt)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"processed": len(all),
		"rebuilt":   rebuilt,
	})
}

func (h *Handlers) HandleAdminSCUPageRebuildToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 || parts[4] == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "scupage id is required")
		return
	}

	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid scupage id")
		return
	}

	sp, err := h.scuPageRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	catz := h.catalogizer
	fullText := tokenizer.BuildSCUTokensFullText(sp.Title, sp.Description, sp.Content, sp.Attributes)
	if err := catz.BuildSCUTokens(sp.ID, fullText); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "rebuilt",
		"scupage_id": sp.ID,
		"scu":        sp.SCU,
		"full_text":  fullText[:min(len(fullText), 200)],
	})
}

// POST /admin/scupages/recalculate-product-counts
// Recalculates ProductCount for all SCU pages based on actual products.
func (h *Handlers) HandleAdminSCUPageRecalculateCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if err := h.scuPageRepo.RecalculateProductCounts(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Product counts recalculated",
	})
}

// --- Payment Methods CRUD ---

func (h *Handlers) HandlePaymentMethodsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	items, err := h.paymentMethodRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []model.CompanyPaymentMethod{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) HandlePaymentMethodCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var pm model.CompanyPaymentMethod
	if !readJSON(w, r, &pm) {
		return
	}
	if err := h.paymentMethodRepo.Create(&pm); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pm)
}

func (h *Handlers) HandlePaymentMethodGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "payment_method_id")
	if !ok {
		return
	}
	pm, err := h.paymentMethodRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pm)
}

func (h *Handlers) HandlePaymentMethodUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "payment_method_id")
	if !ok {
		return
	}
	var pm model.CompanyPaymentMethod
	if !readJSON(w, r, &pm) {
		return
	}
	if err := h.paymentMethodRepo.Update(id, func(p *model.CompanyPaymentMethod) {
		if pm.Name != "" {
			p.Name = pm.Name
		}
		if pm.Slug != "" {
			p.Slug = pm.Slug
		}
		p.IsActive = pm.IsActive
		if pm.SortOrder != 0 {
			p.SortOrder = pm.SortOrder
		}
	}); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.paymentMethodRepo.Get(id)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handlers) HandlePaymentMethodDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "payment_method_id")
	if !ok {
		return
	}
	if err := h.paymentMethodRepo.Delete(id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Delivery Times CRUD ---

func (h *Handlers) HandleDeliveryTimesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	items, err := h.deliveryTimeRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []model.DeliveryTime{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) HandleDeliveryTimeCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var dt model.DeliveryTime
	if !readJSON(w, r, &dt) {
		return
	}
	if err := h.deliveryTimeRepo.Create(&dt); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, dt)
}

func (h *Handlers) HandleDeliveryTimeGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_time_id")
	if !ok {
		return
	}
	dt, err := h.deliveryTimeRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dt)
}

func (h *Handlers) HandleDeliveryTimeUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_time_id")
	if !ok {
		return
	}
	var dt model.DeliveryTime
	if !readJSON(w, r, &dt) {
		return
	}
	if err := h.deliveryTimeRepo.Update(id, func(d *model.DeliveryTime) {
		if dt.Name != "" {
			d.Name = dt.Name
		}
		if dt.Slug != "" {
			d.Slug = dt.Slug
		}
		d.IsActive = dt.IsActive
		if dt.SortOrder != 0 {
			d.SortOrder = dt.SortOrder
		}
	}); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.deliveryTimeRepo.Get(id)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handlers) HandleDeliveryTimeDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "delivery_time_id")
	if !ok {
		return
	}
	if err := h.deliveryTimeRepo.Delete(id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Installment Plans CRUD ---

func (h *Handlers) HandleInstallmentPlansList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	items, err := h.installmentPlanRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []model.InstallmentPlan{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handlers) HandleInstallmentPlanCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	var ip model.InstallmentPlan
	if !readJSON(w, r, &ip) {
		return
	}
	if err := h.installmentPlanRepo.Create(&ip); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ip)
}

func (h *Handlers) HandleInstallmentPlanGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "installment_plan_id")
	if !ok {
		return
	}
	ip, err := h.installmentPlanRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ip)
}

func (h *Handlers) HandleInstallmentPlanUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "installment_plan_id")
	if !ok {
		return
	}
	var ip model.InstallmentPlan
	if !readJSON(w, r, &ip) {
		return
	}
	if err := h.installmentPlanRepo.Update(id, func(i *model.InstallmentPlan) {
		if ip.Name != "" {
			i.Name = ip.Name
		}
		if ip.Slug != "" {
			i.Slug = ip.Slug
		}
		i.IsActive = ip.IsActive
		if ip.SortOrder != 0 {
			i.SortOrder = ip.SortOrder
		}
	}); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updated, _ := h.installmentPlanRepo.Get(id)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handlers) HandleInstallmentPlanDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	id, ok := parseID(w, r, "installment_plan_id")
	if !ok {
		return
	}
	if err := h.installmentPlanRepo.Delete(id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Company settings: get with full details ---

func (h *Handlers) HandleCompanyGetWithSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /admin/companies/{id}/settings
	path := r.URL.Path
	// Trim trailing "/settings"
	if !strings.HasSuffix(path, "/settings") {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	prefix := strings.TrimSuffix(path, "/settings")
	// prefix is now "/admin/companies/{id}"
	parts := strings.Split(prefix, "/")
	if len(parts) < 4 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing company id")
		return
	}
	idStr := parts[3]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company id")
		return
	}

	c, err := h.companyRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	// Load settings from separate storage
	settings, _ := h.companyRepo.GetCompanySettings(id)
	if settings != nil {
		c.PaymentMethodIds = settings.PaymentMethodIds
		c.DeliveryTimeIds = settings.DeliveryTimeIds
		c.InstallmentPlanIds = settings.InstallmentPlanIds
	}

	// Load full objects for payment methods, delivery times, installment plans
	var paymentMethods []model.CompanyPaymentMethod
	var deliveryTimes []model.DeliveryTime
	var installmentPlans []model.InstallmentPlan

	if len(c.PaymentMethodIds) > 0 {
		for _, id := range c.PaymentMethodIds {
			if pm, err := h.paymentMethodRepo.Get(id); err == nil {
				paymentMethods = append(paymentMethods, *pm)
			}
		}
	}
	if len(c.DeliveryTimeIds) > 0 {
		for _, id := range c.DeliveryTimeIds {
			if dt, err := h.deliveryTimeRepo.Get(id); err == nil {
				deliveryTimes = append(deliveryTimes, *dt)
			}
		}
	}
	if len(c.InstallmentPlanIds) > 0 {
		for _, id := range c.InstallmentPlanIds {
			if ip, err := h.installmentPlanRepo.Get(id); err == nil {
				installmentPlans = append(installmentPlans, *ip)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"company":           c,
		"payment_methods":   paymentMethods,
		"delivery_times":    deliveryTimes,
		"installment_plans": installmentPlans,
	})
}
