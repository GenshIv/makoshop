package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/GenshIv/makoshop/internal/catalogizer"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/metrics"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/tokenizer"
	"github.com/GenshIv/silentjson/v2"
)

func (h *Handlers) HandleAdminAttrDefsList(w http.ResponseWriter, r *http.Request) {
	defs, err := h.attrDefRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if defs == nil {
		defs = []model.AttrDef{}
	}
	httpres.WriteJSON(w, http.StatusOK, defs)
}

// GET /admin/attrdefs/{code} — get attribute definition by code

func (h *Handlers) HandleAdminAttrDefGet(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}
	code := parts[3]

	ad, err := h.attrDefRepo.GetByCode(code)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "attribute not found")
		return
	}
	httpres.WriteJSON(w, http.StatusOK, ad)
}

// POST /admin/attrdefs — create attribute definition

func (h *Handlers) HandleAdminAttrDefCreate(w http.ResponseWriter, r *http.Request) {
	var req model.AttrDef
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
		return
	}
	if req.Code == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}

	if err := h.attrDefRepo.Create(req.Code, &req); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, req)
}

// PATCH /admin/attrdefs/{code} — update attribute definition

func (h *Handlers) HandleAdminAttrDefUpdate(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}
	code := parts[3]

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json")
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Return updated attrdef
	ad, _ := h.attrDefRepo.GetByCode(code)
	httpres.WriteJSON(w, http.StatusOK, ad)
}

// DELETE /admin/attrdefs/{code} — delete attribute definition

func (h *Handlers) HandleAdminAttrDefDelete(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "code is required")
		return
	}
	code := parts[3]

	if err := h.attrDefRepo.Delete(code); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"message": "deleted", "code": code})
}

// ================= SCUPage Admin handlers =================

// HandleAdminSCUPageList returns a paginated list of SCU pages.
// GET /admin/scupages?page=1&limit=50&q=search

func (h *Handlers) HandleAdminSCUPageList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	sp, err := h.scuPageRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, sp)
}

// HandleAdminSCUPageUpdate updates a SCU page.
// PATCH /admin/scupages/{id}
// Body: any subset of SCUPage fields

func (h *Handlers) HandleAdminSCUPageUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	var updates map[string]interface{}
	if !httpres.ReadJSON(w, r, &updates) {
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Reindex SCU page
	sp, _ := h.scuPageRepo.Get(id)
	if sp != nil {
		_ = h.scuPageSearch.UnindexSCUPage(sp)
		_ = h.scuPageSearch.IndexSCUPage(sp)
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// HandleAdminSCUPageDelete deletes a SCU page.
// DELETE /admin/scupages/{id}

func (h *Handlers) HandleAdminSCUPageDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid id")
		return
	}

	sp, err := h.scuPageRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	// Unindex
	_ = h.scuPageSearch.UnindexSCUPage(sp)

	// Delete
	if err := h.scuPageRepo.Delete(id); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GET /admin/db/shards — fast shard usage stats (based on FreeOffset)

func (h *Handlers) HandleAdminDBShards(w http.ResponseWriter, r *http.Request) {
	usages := h.store.DB().ShardUsages()
	httpres.WriteJSON(w, http.StatusOK, usages)
}

// GET /admin/db/shards/active — precise shard usage stats (full scan, slow)

func (h *Handlers) HandleAdminDBShardsActive(w http.ResponseWriter, r *http.Request) {
	usages := h.store.DB().ActiveUsage()
	httpres.WriteJSON(w, http.StatusOK, usages)
}

// POST /admin/db/compact — compact all shards (slow, admin only)

func (h *Handlers) HandleAdminDBCompact(w http.ResponseWriter, r *http.Request) {
	db := h.store.DB()
	if err := db.CompactAllShards(1000); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "COMPACT_ERROR", err.Error())
		return
	}
	httpres.WriteJSON(w, http.StatusOK, map[string]string{"message": "compact completed successfully"})
}

// ================= Stats handlers =================

// HandleAdminStats returns aggregated request metrics from ./_tmp/metrics.
// GET /admin/stats?refresh=1 — force refresh cache

func (h *Handlers) HandleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	const cacheTTL = 30 * time.Second

	refresh := r.URL.Query().Get("refresh") == "1"

	h.statsCacheMu.Lock()
	defer h.statsCacheMu.Unlock()

	now := time.Now()
	if !refresh && h.statsCache != nil && now.Sub(h.statsCacheAt) < cacheTTL {
		httpres.WriteJSON(w, http.StatusOK, h.statsCache)
		return
	}

	stats, err := metrics.ParseMetricsStats("./_tmp/metrics")
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "STATS_ERROR", err.Error())
		return
	}

	h.statsCache = stats
	h.statsCacheAt = now

	httpres.WriteJSON(w, http.StatusOK, stats)
}

// ================= Catalogizer handlers =================

// HandleAdminCatalogizerTrain rebuilds token indexes from category anchor_keywords.
// POST /admin/catalogizer/train

func (h *Handlers) HandleAdminCatalogizerTrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	fmt.Println("[CATALOGIZER-TRAIN] Rebuilding token indexes from anchor_keywords...")

	if err := h.catalogizer.RebuildAllCategoryTokens(); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "completed",
		"message": "Token indexes rebuilt from anchor_keywords",
	})
}

// HandleAdminCatalogizerCoverage returns coverage statistics.
// GET /admin/catalogizer/coverage

func (h *Handlers) HandleAdminCatalogizerCoverage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	categories, err := h.categoryRepo.ListAll()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"total_categories": total,
		"with_keywords":    withKeywords,
		"empty":            empty,
		"active":           active,
		"few_tokens":       fewTokens,
		"many_tokens":      manyTokens,
	})
}

type GetIDOnly struct {
	ID int64 `json:"id"`
}

var reqIdOnly = silentjson.BuildRegistry(reflect.TypeOf(GetIDOnly{}))

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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	for _, item := range result.Items {
		it := new(GetIDOnly)
		err := silentjson.ParseObject(item, reqIdOnly, unsafe.Pointer(it))
		if err != nil {

		}
		productIDs = append(productIDs, it.ID)
	}

	fmt.Printf("[CATALOGIZE] Processing %d products (apply=%v)...\n", len(productIDs), body.Apply)

	results, err := h.catalogizer.BatchCatalogize(productIDs, body.Apply)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	fmt.Printf("[CATALOGIZE] Done. %d products matched categories.\n", len(results))

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 || parts[4] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "product id is required")
		return
	}
	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid product id")
		return
	}

	type req struct {
		Apply bool `json:"apply"`
	}
	var body req
	_ = json.NewDecoder(r.Body).Decode(&body)

	p, err := h.productRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	result, err := h.catalogizer.CatalogizeProduct(p)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if result != nil && body.Apply {
		if err := h.catalogizer.ApplyCategory(p, result); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	type req struct {
		Name string `json:"name"`
	}
	var body req
	if !httpres.ReadJSON(w, r, &body) {
		return
	}

	if body.Name == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "name is required")
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"name":        body.Name,
		"tokens":      tokenWords,
		"matches":     matches,
		"match_count": len(matches),
	})
}

func (h *Handlers) HandleAdminSCUPageCatalogizeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	fmt.Printf("[SCUPAGE-CATALOGIZE-ALL] Total SCU pages to process: %d\n", len(all))

	// Get catalogizer interface
	catz := h.catalogizer

	catalogized := 0
	var results []map[string]interface{}

	catCache := map[int64][]string{}
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

		newUrl := h.scuPageRepo.ComputeSeoURL(sp.Slug, sp.CategoryID, catCache)

		if (newCatID > 0 && newCatID != sp.CategoryID) || newUrl != sp.SeoURL {
			sp.SeoURL = newUrl
			if body.Apply {
				if err := h.scuPageRepo.Update(sp.ID, func(s *model.SCUPage) {
					s.CategoryID = newCatID
					s.SeoURL = newUrl
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"processed": len(all),
		"rebuilt":   rebuilt,
	})
}

func (h *Handlers) HandleAdminSCUPageRebuildToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 || parts[4] == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "scupage id is required")
		return
	}

	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid scupage id")
		return
	}

	sp, err := h.scuPageRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	catz := h.catalogizer
	fullText := tokenizer.BuildSCUTokensFullText(sp.Title, sp.Description, sp.Content, sp.Attributes)
	if err := catz.BuildSCUTokens(sp.ID, fullText); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if err := h.scuPageRepo.RecalculateProductCounts(); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Product counts recalculated",
	})
}

// POST /admin/scupages/recalculate-min-prices
// Recalculates MinPrice for all SCU pages based on actual product prices.
// Also rebuilds sort indexes to ensure price filters work correctly.

func (h *Handlers) HandleAdminSCUPageRecalculateMinPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	if err := h.scuPageRepo.RecalculateMinPrices(h.productRepo); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Rebuild sort indexes to reflect updated prices
	if h.scuPageSearch != nil {
		if err := h.scuPageSearch.BuildSortIndexes(); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("min prices recalculated but failed to rebuild sort indexes: %v", err))
			return
		}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Min prices recalculated and sort indexes rebuilt",
	})
}

// --- Payment Methods CRUD ---
