package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	//"github.com/GenshIv/makoshop/internal/catalogizer"
	//"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/slug"
)

func (h *Handlers) HandleCategoriesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	cats, err := h.categoryRepo.ListAll()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": cats,
	})
}

// HandleCategoriesTree returns the category tree.
// GET /categories/tree
// GET /categories/tree?child_of={id}
// HEAD /categories/tree — returns headers only, body is not sent.
// Uses precomputed JSON from turbo: zero Unmarshal/Marshal on hot path.

func (h *Handlers) HandleCategoriesTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Support HEAD requests: run full logic but don't send body.
	headOnly := r.Method == http.MethodHead

	childOf := r.URL.Query().Get("child_of")
	if childOf != "" {
		parentID, err := strconv.ParseInt(childOf, 10, 64)
		if err != nil || parentID <= 0 {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid child_of parameter")
			return
		}
		data, err := h.categoryRepo.GetTreeByParentJSON(parentID)
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if headOnly {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	data, err := h.categoryRepo.GetTreeJSON()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if headOnly {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// HandleRebuildCategoryTrees rebuilds all precomputed category tree JSONs.
// POST /admin/rebuild-category-trees

func (h *Handlers) HandleRebuildCategoryTrees(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	h.categoryRepo.RebuildTrees()
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "rebuilt"})
}

// HandleDebugCategoryCounts returns counts for debugging category indexes.
// GET /admin/debug-category-counts

func (h *Handlers) HandleDebugCategoryCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	all, errAll := h.categoryRepo.ListAll()
	activeCount := 0
	rootCount := 0
	for _, c := range all {
		if c.IsActive {
			activeCount++
		}
		if c.ParentID == nil {
			rootCount++
		}
	}

	tree, _ := h.categoryRepo.GetTree()

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"all":          len(all),
		"list_all_err": errAll,
		"active":       activeCount,
		"roots":        rootCount,
		"tree_nodes":   len(tree),
	})
}

func (h *Handlers) HandleCategoryGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	cat, err := h.categoryRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "category not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, cat)
}

// HandleCategoryTreePath returns the full path from root to category in one request.
// GET /categories/tree_path/{id}
// Returns: []db.CategoryTreeNode (full data with children)

func (h *Handlers) HandleCategoryTreePath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	path, err := h.categoryRepo.GetTreePathFull(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "category path not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, path)
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateCategoryRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	nameEn := req.NameEn
	if nameEn == "" && req.NameRu != "" {
		nameEn = req.NameRu // fallback for slug if name_en not set
	}

	catSlug := req.Slug
	if catSlug == "" {
		catSlug = slug.SlugFromNameEn(nameEn)
	}

	cat := &model.Category{
		ParentID:  req.ParentID,
		NameRu:    req.NameRu,
		NameUa:    req.NameUa,
		NamePl:    req.NamePl,
		NameEn:    nameEn,
		Slug:      catSlug,
		Desc:      req.Description,
		IsActive:  req.IsActive,
		SortOrder: req.SortOrder,
	}

	if err := h.categoryRepo.Create(cat); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, cat)
}

func (h *Handlers) HandleCategoryUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	// Read raw JSON to detect which fields are present
	var raw map[string]interface{}
	if !httpres.ReadJSON(w, r, &raw) {
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
			c.Slug = slug.SlugFromNameEn(c.NameEn)
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	cat, _ := h.categoryRepo.Get(id)
	// Rebuild catalogizer tokens for this category
	if cat != nil {
		_ = h.catalogizer.BuildTokensForCategory(cat)
	}
	httpres.WriteJSON(w, http.StatusOK, cat)
}

func (h *Handlers) HandleCategoryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "category_id")
	if !ok {
		return
	}

	if err := h.categoryRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	catIDStr := parts[len(parts)-2]
	catID, err := strconv.ParseInt(catIDStr, 10, 64)
	if err != nil || catID <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid category id")
		return
	}

	// Verify category exists
	_, err = h.categoryRepo.Get(catID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "category not found")
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
	}
}

// GET /admin/categories/{id}/attributes

func (h *Handlers) handleGetCategoryAttributes(w http.ResponseWriter, r *http.Request, catID int64) {
	// Get attribute codes for this category
	codes, err := h.attrDefRepo.GetCodesForCategoryTree(catID, h.categoryRepo)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if req.Code == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}

	if err := h.attrDefRepo.UpsertCode(req.Code, catID); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "attribute added",
		"code":    req.Code,
	})
}

// DELETE /admin/categories/{id}/attributes — remove attribute code from category
// Query: ?code=attribute-code

func (h *Handlers) handleRemoveCategoryAttribute(w http.ResponseWriter, r *http.Request, catID int64) {
	code := r.URL.Query().Get("code")
	if code == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "code query param is required")
		return
	}

	// Remove category from attribute's category list
	cats, err := h.attrDefRepo.GetCategories(code)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "attribute not found")
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
		//buf := makodb.TurboBinaryNew(db.Uint64SliceFromInt64(newCats))
		//_ = h.store.TurboWrite("attrdef_cats:"+code, buf)
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "attribute removed",
		"code":    code,
	})
}

// --- Brands ---

// GET /brands — returns all brands from brand_list turbo index.
// Optional: ?category_id=N to filter brands by category.

func (h *Handlers) HandleAdminCategoriesExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	categories, err := h.categoryRepo.ListAll()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if len(req.Categories) == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "no categories provided")
		return
	}

	// Map: oldID -> newID (for relinking later)
	oldIDToNewID := make(map[int64]int64)
	// Map: name -> newID (for finding existing by name only)
	slugToID := make(map[string]int64)

	// Build existing categories map (use name_en as primary key; fallback to name_ru)
	existing, _ := h.categoryRepo.ListAll()
	for _, cat := range existing {
		name := cat.Slug
		slugToID[name] = cat.ID
	}

	created := 0
	updated := 0

	// First pass: create/update categories WITHOUT setting parent_id (to handle any order)
	for _, ic := range req.Categories {
		slugString := ic.Slug

		// Find existing category by name_en only
		if existingID, ok := slugToID[slugString]; ok {
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
					c.Slug = slug.SlugFromNameEn(c.NameEn)
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
			// Create new (without parent for now)
			catSlug := ic.Slug
			if catSlug == "" {
				catSlug = slug.SlugFromNameEn(ic.NameEn)
			}
			cat := &model.Category{
				ID:             ic.ID, // Use ID from import file
				NameRu:         ic.NameRu,
				NameUa:         ic.NameUa,
				NamePl:         ic.NamePl,
				NameEn:         ic.NameEn,
				Slug:           catSlug,
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
				ParentID:       ic.ParentID,
			}
			if ic.ParentID != nil && *ic.ParentID != 0 {
				cat.ParentID = ic.ParentID
			}
			if err := h.categoryRepo.Create(cat); err != nil {
				fmt.Printf("WARN: create category %s: %v\n", slugString, err)
				continue
			}
			created++

			// Map old ID to new ID (they should be the same now)
			if ic.ID != 0 && cat.ID != ic.ID {
				oldIDToNewID[ic.ID] = cat.ID
			}
			slugToID[slugString] = cat.ID
		}
	}

	// Second pass: set parent_id based on mappings
	for _, ic := range req.Categories {
		if ic.ParentID != nil && ic.ID != 0 {
			// Find the category we just created/updated
			if catID, ok := oldIDToNewID[ic.ID]; ok {
				// Find the parent's new ID
				if parentID, ok := oldIDToNewID[*ic.ParentID]; ok && (ic.ParentID == nil || (*ic.ParentID != parentID && parentID > 0)) {
					h.categoryRepo.Update(catID, func(c *model.Category) {
						c.ParentID = &parentID
					})
				}
			}
		}
	}

	// Second pass: import attributes
	for _, ic := range req.Categories {
		newID, ok := oldIDToNewID[ic.ID]
		if newID == 0 && ok && newID != ic.ID {
			// Try to find by name
			nameKey := ic.Slug
			if nameKey != "" {
				newID = slugToID[nameKey]
			}
		}
		if newID == 0 || newID == ic.ID {
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "completed",
		"created":    created,
		"updated":    updated,
		"total":      created + updated,
		"old_id_map": oldIDToNewID,
		"message":    "Categories imported. Indexes and catalogizer rebuilt.",
	})
}

// HandleAdminCategoriesReorder handles bulk reordering of categories (drag-and-drop).
// POST /admin/categories/reorder
// Body: { "items": [ { "id": 5, "parent_id": 2, "sort_order": 0 }, ... ] }
// Validates no cycles, applies all updates, rebuilds trees.

func (h *Handlers) HandleAdminCategoriesReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req struct {
		Items []struct {
			ID        int64  `json:"id"`
			ParentID  *int64 `json:"parent_id"`
			SortOrder int    `json:"sort_order"`
		} `json:"items"`
	}

	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if len(req.Items) == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "no items provided")
		return
	}

	// Build map of current categories for validation
	allCats, err := h.categoryRepo.ListAll()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	catByID := make(map[int64]model.Category)
	for _, c := range allCats {
		catByID[c.ID] = c
	}

	// Build proposed parent map: id -> new parent_id (or nil for root)
	proposedParent := make(map[int64]*int64)
	for _, item := range req.Items {
		if _, exists := catByID[item.ID]; !exists {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("category %d not found", item.ID))
			return
		}
		proposedParent[item.ID] = item.ParentID
	}

	// Validate: no cycles allowed
	// A category cannot be moved into its own subtree
	for _, item := range req.Items {
		if item.ParentID == nil {
			continue
		}

		// Check if parent exists
		if _, exists := catByID[*item.ParentID]; !exists {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("parent category %d not found", *item.ParentID))
			return
		}

		// Check if parent is the category itself
		if *item.ParentID == item.ID {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "cannot set category as its own parent")
			return
		}

		// Check if parent is in the category's subtree (would create cycle)
		// We need to check if currentParent is a descendant of item.ID
		if isDescendant(catByID, *item.ParentID, item.ID) {
			httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "cycle detected: cannot move category into its own subtree")
			return
		}

		// Also check for cycles in the proposed changes
		// Walk up from proposed parent using proposed parents to detect cycles
		currentParent := *item.ParentID
		for currentParent != 0 {
			// Check if this parent is being moved to become a child of the current category
			if proposedParent[currentParent] != nil && *proposedParent[currentParent] == item.ID {
				httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "cycle detected: cannot move category into its own subtree")
				return
			}
			// Move up using proposed parent if available, otherwise current parent
			if proposedParent[currentParent] != nil {
				currentParent = *proposedParent[currentParent]
			} else if cat, exists := catByID[currentParent]; exists {
				if cat.ParentID != nil {
					currentParent = *cat.ParentID
				} else {
					break
				}
			} else {
				break
			}
		}
	}

	// Apply all updates
	updated := 0
	for _, item := range req.Items {
		err := h.categoryRepo.Update(item.ID, func(c *model.Category) {
			c.ParentID = item.ParentID
			c.SortOrder = item.SortOrder
		})
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("failed to update category %d: %v", item.ID, err))
			return
		}
		updated++
	}

	// Rebuild trees and indexes
	h.categoryRepo.RebuildTrees()

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"updated": updated,
	})
}

// isDescendant checks if catID is a descendant of ancestorID (or equal).
func isDescendant(catByID map[int64]model.Category, catID, ancestorID int64) bool {
	current := catID
	for current != 0 {
		if current == ancestorID {
			return true
		}
		cat, exists := catByID[current]
		if !exists || cat.ParentID == nil {
			return false
		}
		current = *cat.ParentID
	}
	return false
}

// HandleAdminEANPageCatalogizeAll re-catalogizes all EAN pages using TurboTopNByIntersection.
// POST /admin/eanpages/catalogize-all
// Body: { "apply": true }
