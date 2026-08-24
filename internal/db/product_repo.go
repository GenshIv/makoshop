package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
)

type ProductRepo struct {
	store             *Store
	promoCampaignRepo *PromoCampaignRepo
	promoPlanRepo     *PromoPlanRepo
	promoLogRepo      *PromoLogRepo
	turboSearch       *TurboProductSearch
	scuPageSearch     *SCUPageSearch

	// Single-writer channel for batch operations
	batchChan chan batchTask
}

// Store returns the underlying Store for direct access (emergency use only).
func (r *ProductRepo) Store() *Store {
	return r.store
}

type batchTask struct {
	products []*model.Product
	done     chan batchResult
}

type batchResult struct {
	Created int
	Skipped int
	Errors  []string
}

func NewProductRepo(store *Store, promoCampaignRepo *PromoCampaignRepo, promoPlanRepo *PromoPlanRepo, promoLogRepo *PromoLogRepo) *ProductRepo {
	r := &ProductRepo{
		store:             store,
		promoCampaignRepo: promoCampaignRepo,
		promoPlanRepo:     promoPlanRepo,
		promoLogRepo:      promoLogRepo,
		batchChan:         make(chan batchTask, 64),
	}
	// Start single writer goroutine
	go r.batchWriter()
	return r
}

// SetTurboSearch attaches a TurboProductSearch instance to this repo.
// Call this after creating both ProductRepo and TurboProductSearch to avoid circular deps.
func (r *ProductRepo) SetTurboSearch(t *TurboProductSearch) {
	r.turboSearch = t
}

// SetSCUPageSearch attaches a SCUPageSearch instance to this repo.
func (r *ProductRepo) SetSCUPageSearch(s *SCUPageSearch) {
	r.scuPageSearch = s
}

// TurboSearch returns the attached TurboProductSearch (may be nil).
func (r *ProductRepo) TurboSearch() *TurboProductSearch {
	return r.turboSearch
}

// batchWriter is the single goroutine that performs all batch DB writes.
func (r *ProductRepo) batchWriter() {
	for task := range r.batchChan {
		result := r.doCreateBatch(task.products)
		task.done <- result
	}
}

// doCreateBatch performs the actual batch creation (called only from batchWriter).
func (r *ProductRepo) doCreateBatch(products []*model.Product) batchResult {
	var result batchResult

	// Step 1: Assign IDs
	for _, p := range products {
		id, err := r.store.NextID("product")
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("next_id: %v", err))
			result.Skipped++
			continue
		}
		p.ID = id
		p.CreatedAt = time.Now().Unix()
		p.UpdatedAt = time.Now().Unix()
		if p.Status == "" {
			p.Status = model.ProductStatusDraft
		}
		if p.Currency == "" {
			p.Currency = "RUB"
		}
	}

	// Step 2: Write all products
	var createdProducts []*model.Product
	for _, p := range products {
		if p.ID == 0 {
			continue
		}
		data := MarshalProduct(*p)
		if err := r.store.DocPut(KeyProduct(p.ID), data); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("save product %d: %v", p.ID, err))
			result.Skipped++
			continue
		}
		createdProducts = append(createdProducts, p)
		result.Created++
	}

	// Step 3: Turbo indexes (batch) — only turbo, no other indexes
	if r.turboSearch != nil && len(createdProducts) > 0 {
		if err := r.turboSearch.IndexProductBatch(createdProducts); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("turbo index batch: %v", err))
		}
	}

	return result
}

// CreateBatch creates products in a batch using the single-writer goroutine.
func (r *ProductRepo) CreateBatch(products []*model.Product) batchResult {
	if len(products) == 0 {
		return batchResult{}
	}
	done := make(chan batchResult, 1)
	r.batchChan <- batchTask{products: products, done: done}
	return <-done
}

// CreateBatchWithIdxBuild creates products and returns them for external batch indexing via idxbuild.
// This method does NOT call turboSearch.IndexProductBatch — caller is responsible for indexing.
// Use this for large imports where indexes are built in a separate merge step.
func (r *ProductRepo) CreateBatchWithIdxBuild(products []*model.Product) ([]*model.Product, int) {
	if len(products) == 0 {
		return nil, 0
	}

	// Step 1: Assign IDs
	for _, p := range products {
		id, err := r.store.NextID("product")
		if err != nil {
			fmt.Printf("WARN: next_id product: %v\n", err)
			continue
		}
		p.ID = id
		p.CreatedAt = time.Now().Unix()
		p.UpdatedAt = time.Now().Unix()
		if p.Status == "" {
			p.Status = model.ProductStatusDraft
		}
		if p.Currency == "" {
			p.Currency = "RUB"
		}
	}

	// Step 2: Write all products
	var createdProducts []*model.Product
	for _, p := range products {
		if p.ID == 0 {
			continue
		}
		data := MarshalProduct(*p)
		if err := r.store.DocPut(KeyProduct(p.ID), data); err != nil {
			fmt.Printf("WARN: save product %d: %v\n", p.ID, err)
			continue
		}
		createdProducts = append(createdProducts, p)
	}

	// Step 3: NO indexing here — caller uses idxbuild.BatchAccum

	return createdProducts, len(createdProducts)
}

// CreateBatchWithIdxBuildAndOffset is like CreateBatchWithIdxBuild but adds idOffset to each product ID.
// Used for multi-company imports to ensure unique IDs across companies.
func (r *ProductRepo) CreateBatchWithIdxBuildAndOffset(products []*model.Product, idOffset int64) ([]*model.Product, int) {
	if len(products) == 0 {
		return nil, 0
	}

	// Step 1: Assign IDs with offset
	for _, p := range products {
		id, err := r.store.NextID("product")
		if err != nil {
			fmt.Printf("WARN: next_id product: %v\n", err)
			continue
		}
		p.ID = id + idOffset
		p.CreatedAt = time.Now().Unix()
		p.UpdatedAt = time.Now().Unix()
		if p.Status == "" {
			p.Status = model.ProductStatusDraft
		}
		if p.Currency == "" {
			p.Currency = "RUB"
		}
	}

	// Step 2: Write all products
	var createdProducts []*model.Product
	for _, p := range products {
		if p.ID == 0 {
			continue
		}
		data := MarshalProduct(*p)
		if err := r.store.DocPut(KeyProduct(p.ID), data); err != nil {
			fmt.Printf("WARN: save product %d: %v\n", p.ID, err)
			continue
		}
		createdProducts = append(createdProducts, p)
	}

	// Step 3: NO indexing here — caller uses idxbuild.BatchAccum

	return createdProducts, len(createdProducts)
}

func (r *ProductRepo) Create(p *model.Product) error {
	id, err := r.store.NextID("product")
	if err != nil {
		return fmt.Errorf("next_id product: %w", err)
	}
	p.ID = id
	p.CreatedAt = time.Now().Unix()
	p.UpdatedAt = time.Now().Unix()
	if p.Status == "" {
		p.Status = model.ProductStatusDraft
	}
	if p.Currency == "" {
		p.Currency = "RUB"
	}

	data := MarshalProduct(*p)
	if err := r.store.DocPut(KeyProduct(p.ID), data); err != nil {
		return fmt.Errorf("save product: %w", err)
	}

	// Turbo index only
	if r.turboSearch != nil {
		if err := r.turboSearch.IndexProduct(p); err != nil {
			fmt.Printf("WARN: turbo indexProduct error for product %d: %v\n", p.ID, err)
		}
	}
	return nil
}

func (r *ProductRepo) Get(id int64) (*model.Product, error) {
	data, err := r.store.DocGet(KeyProduct(id))
	if err != nil {
		return nil, fmt.Errorf("get product %d: %w", id, err)
	}
	return UnmarshalProduct(data)
}

func (r *ProductRepo) Update(id int64, updater func(*model.Product)) error {
	p, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove old turbo indexes
	if r.turboSearch != nil {
		_ = r.turboSearch.UnindexProduct(p)
	}

	updater(p)
	p.UpdatedAt = time.Now().Unix()

	data := MarshalProduct(*p)
	if err := r.store.DocPut(KeyProduct(p.ID), data); err != nil {
		return fmt.Errorf("update product: %w", err)
	}

	// Rebuild turbo indexes
	if r.turboSearch != nil {
		if err := r.turboSearch.IndexProduct(p); err != nil {
			fmt.Printf("WARN: turbo indexProduct error for product %d (update): %v\n", p.ID, err)
		}
	}
	return nil
}

// TurboKeyProductList — глобальный индекс всех product ID для быстрого перебора и удаления.
const TurboKeyProductList = "product_list"

// productUniqueKeyPrefix — префикс для индекса уникальности продукта (SKU+Company+Attributes).
const productUniqueKeyPrefix = "product_unique:"

// attrsHash возвращает стабильный хеш от map[string]interface{} атрибутов.
// Используется для определения уникальности модификации продукта.
func attrsHash(attrs map[string]interface{}) string {
	if len(attrs) == 0 {
		return ""
	}
	// Сортируем ключи для стабильности
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(fmt.Sprintf("%v", attrs[k]))
		b.WriteString(";")
	}
	return fmt.Sprintf("%x", Fnv64(b.String()))
}

// kvGet returns the value for the given key from a KeyValue slice, or ("", false) if not found.
func kvGet(attrs []model.KeyValue, key string) (string, bool) {
	for _, kv := range attrs {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return "", false
}

// productUniqueKey строит ключ уникальности: SKU + CompanyID + option.
// option — модификатор товара (цвет и т.п.), последняя колонка в прайсе.
func productUniqueKey(sku string, companyID int64, attrs []model.KeyValue) string {
	if v, ok := kvGet(attrs, "option"); ok && v != "" {
		return fmt.Sprintf("%s:%d:%s", sku, companyID, v)
	}
	return fmt.Sprintf("%s:%d", sku, companyID)
}

// GetOrCreateByKey находит продукт по уникальному ключу (SKU+Company+Attrs) или создаёт новый.
// Если продукт найден — обновляет цену и возвращает существующий ID.
// Если не найден — создаёт новый и возвращает его ID.
func (r *ProductRepo) GetOrCreateByKey(p *model.Product) (int64, bool, error) {
	if p.SKU == "" || p.CompanyID == 0 {
		// Без SKU/Company создаём как обычно
		if err := r.Create(p); err != nil {
			return 0, false, err
		}
		return p.ID, true, nil
	}

	key := productUniqueKey(p.SKU, p.CompanyID, p.Attributes)
	keyPath := productUniqueKeyPrefix + key

	// Проверяем, есть ли уже такой продукт
	data, err := r.store.db.TurboRawRead(keyPath)
	if err == nil && len(data) > 0 {
		var existingID int64
		_, _ = fmt.Sscanf(string(data), "%d", &existingID)

		// Продукт найден — обновляем цену
		existing, err := r.Get(existingID)
		if err == nil {
			// Обновляем цену, если изменилась
			if existing.Price != p.Price {
				existing.Price = p.Price
				existing.UpdatedAt = time.Now().Unix()
				_ = r.store.DocPut(KeyProduct(existingID), MarshalProduct(*existing))
			}
			return existingID, false, nil
		}
	}

	// Продукта нет — создаём новый
	if err := r.Create(p); err != nil {
		return 0, false, err
	}

	// Записываем уникальный ключ
	_ = r.store.TurboWrite(keyPath, []byte(fmt.Sprintf("%d", p.ID)))

	return p.ID, true, nil
}

func (r *ProductRepo) Delete(id int64) error {
	p, err := r.Get(id)
	if err != nil {
		return err
	}
	if r.turboSearch != nil {
		_ = r.turboSearch.UnindexProduct(p)
	}
	// Remove from product_list index
	if p.ID != 0 {
		_, _ = r.store.db.TurboDeleteIndexString(TurboKeyProductList, KeyProduct(p.ID))
	}
	if err := r.store.DocDelete(KeyProduct(id)); err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	return nil
}

// DeleteProductByID deletes a product by ID, removing it from all indexes and its SCUPage.
// Returns error if product not found.
func (r *ProductRepo) DeleteProductByID(id int64) error {
	p, err := r.Get(id)
	if err != nil {
		return fmt.Errorf("product %d not found: %w", id, err)
	}

	// Unindex from turbo search
	if r.turboSearch != nil {
		_ = r.turboSearch.UnindexProduct(p)
	}

	// Remove from product_list
	_, _ = r.store.db.TurboDeleteIndexString(TurboKeyProductList, KeyProduct(id))

	// Remove from SCUPage if linked
	if r.scuPageSearch != nil && p.SCU != "" {
		if sp, err := r.scuPageSearch.repo.GetBySCU(p.SCU); err == nil {
			_ = r.scuPageSearch.repo.RemoveProduct(sp.ID, id)
		}
	}

	// Delete document
	if err := r.store.DocDelete(KeyProduct(id)); err != nil {
		return fmt.Errorf("delete product %d: %w", id, err)
	}
	return nil
}

// DeleteAllProducts removes all products and related indexes.
// This is a destructive operation intended for admin use only.
func (r *ProductRepo) DeleteAllProducts() error {
	// Collect all product IDs from product_list
	data, err := r.store.db.TurboRawRead(TurboKeyProductList)
	if err != nil || len(data) == 0 {
		// No products to delete
		return nil
	}

	tokens, err := r.store.db.TurboGetIndexTokens(TurboKeyProductList)
	if err != nil {
		return fmt.Errorf("get product list tokens: %w", err)
	}
	if len(tokens) == 0 {
		return nil
	}

	fmt.Printf("[DELETE-ALL] Deleting %d products from product_list...\n", len(tokens))

	// Get all products at once
	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return fmt.Errorf("multi get products: %w", err)
	}

	// Delete each product
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		p, err := UnmarshalProduct(doc)
		if err != nil {
			continue
		}
		id := p.ID
		// Unindex from turbo search
		if r.turboSearch != nil {
			_ = r.turboSearch.UnindexProduct(p)
		}
		// Delete document
		_ = r.store.DocDelete(KeyProduct(id))
	}

	// Clear product_list
	_ = r.store.TurboWrite(TurboKeyProductList, []byte{})

	// Now delete all SCU pages (they become empty without products)
	if r.scuPageSearch != nil {
		_ = r.deleteAllSCUPages()
	}

	fmt.Println("[DELETE-ALL] All products and SCU pages deleted.")
	return nil
}

// deleteAllSCUPages removes all SCU pages and their indexes.
func (r *ProductRepo) deleteAllSCUPages() error {
	if r.scuPageSearch == nil {
		return nil
	}

	tokens, err := r.store.db.TurboGetIndexTokens(TurboKeySCUPageList)
	if err != nil || len(tokens) == 0 {
		return nil
	}

	fmt.Printf("[DELETE-ALL] Deleting %d SCU pages...\n", len(tokens))

	// Get all SCU pages at once
	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return fmt.Errorf("multi get scupages: %w", err)
	}

	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		sp, err := UnmarshalSCUPage(doc)
		if err != nil {
			continue
		}
		id := sp.ID
		// Unindex from SCUPageSearch turbo indexes
		if err := r.scuPageSearch.UnindexSCUPage(sp); err != nil {
			fmt.Printf("[DELETE-ALL] WARN: unindex scupage %d: %v\n", id, err)
		}
		// Delete SCU page doc and its indexes
		_ = r.scuPageSearch.repo.Delete(id)
	}

	fmt.Println("[DELETE-ALL] All SCU pages deleted.")
	return nil
}

// SearchContext describes the current search/filter context for promo matching.
type SearchContext struct {
	Q           string
	CategoryID  int64
	Brand       string
	AttrFilters map[string][]string
}

// ListParams describes search/filter/sort/pagination parameters for product listing.
type ListParams struct {
	Q           string
	CategoryID  int64
	CompanyID   int64
	Brand       string
	BrandID     int64
	PriceMin    *float64
	PriceMax    *float64
	AttrFilters map[string][]string // attr.<code> -> [values] (OR match)
	AttrRanges  map[string]*PriceRange
	Sort        string
	Page        int
	Limit       int
	// FacetBrandCodes is a list of attribute codes to compute brand/facet counts for.
	// If empty, only "brand" (the product.Brand field) is computed.
	FacetBrandCodes []string
}

// ListResult holds the result of a product list query.
type ListResult struct {
	Items  []ProductListItem `json:"items"`
	Total  int64             `json:"total"`
	Page   int               `json:"page"`
	Limit  int               `json:"limit"`
	Facets *Facets           `json:"facets,omitempty"`
}

type (
	AttrItem struct {
		Code         string   `json:"code"`
		Options      []string `json:"options"`
		NameRU       string   `json:"name_ru,omitempty"`
		NameUA       string   `json:"name_ua,omitempty"`
		NamePL       string   `json:"name_pl,omitempty"`
		NameEN       string   `json:"name_en,omitempty"`
		Type         string   `json:"type,omitempty"`
		IsFilterable bool     `json:"is_filterable,omitempty"`
	}

	SCUListRespData struct {
		Items         []silentjson.RawMessage `json:"items,omitempty"`
		Total         int64                   `json:"total"`
		Page          int                     `json:"page"`
		Limit         int                     `json:"limit"`
		CategoryAttrs []AttrItem              `json:"category_attrs,omitempty"`
		CatID         int64                   `json:"category_id,omitempty"`
		TreePath      []string                `json:"tree_path,omitempty"`
		Category      model.Category          `json:"category,omitempty"`
		Subcategories silentjson.RawMessage   `json:"subcategories,omitempty"` // precomputed JSON []byte, no struct->json
		Facets        *Facets                 `json:"facets,omitempty"`
		Products      []model.Product         `json:"products,omitempty"`
		SCUPage       *model.SCUPage          `json:"scu_page,omitempty"`
		TreePathFull  []CategoryTreeNode      `json:"tree_path_full,omitempty"`
		SEOURL        string                  `json:"seo_url,omitempty"`
	}
)

// Facets holds facet counts for filtering UI.
type Facets struct {
	Brands map[string]int            `json:"brands,omitempty"` // brand name -> count
	Attrs  map[string]map[string]int `json:"attrs,omitempty"`  // attr_code -> {value -> count}
}

// FacetCount is a single facet value with its count.
type FacetCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type PriceRange struct {
	Min float64
	Max float64
}

// ProductListItem is the response shape for a product in list results.
type ProductListItem struct {
	ID         int64               `json:"id"`
	SKU        string              `json:"sku"`
	Name       string              `json:"name"`
	CategoryID int64               `json:"category_id"`
	CompanyID  int64               `json:"company_id"`
	Brand      string              `json:"brand,omitempty"`
	Price      float64             `json:"price"`
	Currency   string              `json:"currency"`
	Status     model.ProductStatus `json:"status"`
	Attributes []model.KeyValue    `json:"attributes,omitempty"`
	Images     []string            `json:"images,omitempty"`
	Promoted   bool                `json:"promoted,omitempty"` // true if shown via promotion campaign
}

// List returns paginated product list with search, filters, sorting.
// Returns: items, totalCount.
// Deprecated: use ListWithFacets for faceted search support.
func (r *ProductRepo) List(params ListParams) ([]silentjson.RawMessage, int64, error) {
	result, err := r.ListWithFacets(params)
	if err != nil {
		return nil, 0, err
	}
	return result.Items, result.Total, nil
}

// ListWithFacets returns paginated list with optional facets.
// Now uses SCUPageSearch as primary backend (catalog shows SCU pages, not individual products).
func (r *ProductRepo) ListWithFacets(params ListParams) (*SCUListRespData, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	// Primary: use SCUPageSearch (catalog shows SCU pages)
	if r.scuPageSearch != nil {
		scuParams := SCUPageListParams{
			Q:           params.Q,
			CategoryID:  params.CategoryID,
			CompanyID:   params.CompanyID,
			BrandID:     params.BrandID,
			AttrFilters: params.AttrFilters,
			Sort:        params.Sort,
			Page:        params.Page,
			Limit:       params.Limit,
		}

		result, err := r.scuPageSearch.ListWithTurbo(scuParams)
		if err != nil {
			return nil, fmt.Errorf("scupage list: %w", err)
		}

		// Return raw SCUPage JSON directly (front-end will parse it)
		//items := make([]ProductListItem, 0, len(result.Items))
		//for _, raw := range result.Items {
		//	var m map[string]any
		//	if err := json.Unmarshal(raw, &m); err != nil {
		//		continue
		//	}
		//	item := ProductListItem{
		//		ID:       toInt64(m["id"]),
		//		Name:     toString(m["title"]),
		//		SKU:      toString(m["scu"]),
		//		Brand:    toString(m["brand"]),
		//		Currency: toString(m["currency"]),
		//		Status:   model.ProductStatusActive,
		//	}
		//	if v, ok := m["category_id"]; ok {
		//		item.CategoryID = toInt64(v)
		//	}
		//	if v, ok := m["min_price"]; ok {
		//		item.Price = toFloat64Val(v)
		//	}
		//	if v, ok := m["attributes"]; ok {
		//		item.Attributes = toAttrKV(v)
		//	}
		//	if v, ok := m["images"]; ok {
		//		item.Images = toStringSlice(v)
		//	}
		//	// CompanyID from first product in products[]
		//	if prods, ok := m["products"]; ok {
		//		if arr, ok := prods.([]any); ok && len(arr) > 0 {
		//			if first, ok := arr[0].(map[string]any); ok {
		//				item.CompanyID = toInt64(first["company_id"])
		//			}
		//		}
		//	}
		//	items = append(items, item)
		//}

		return &SCUListRespData{
			Items: result.Items,
			Total: result.Total,
			Page:  result.Page,
			Limit: result.Limit,
		}, nil
	}

	// Fallback: use TurboProductSearch (legacy)
	if r.turboSearch != nil {
		facetCodes := params.FacetBrandCodes
		if len(facetCodes) == 0 {
			facetCodes = []string{"brand"}
		}
		priceMin := 0.0
		priceMax := 0.0
		if params.PriceMin != nil {
			priceMin = *params.PriceMin
		}
		if params.PriceMax != nil {
			priceMax = *params.PriceMax
		}

		turboParams := TurboListParams{
			Q:           params.Q,
			CategoryID:  params.CategoryID,
			CompanyID:   params.CompanyID,
			BrandID:     params.BrandID,
			PriceMin:    priceMin,
			PriceMax:    priceMax,
			AttrFilters: params.AttrFilters,
			Sort:        params.Sort,
			Page:        params.Page,
			Limit:       params.Limit,
			FacetCodes:  facetCodes,
		}

		result, err := r.turboSearch.ListWithTurbo(turboParams)
		if err != nil {
			return nil, fmt.Errorf("turbo list: %w", err)
		}

		var facets *Facets
		if result.Facets != nil {
			facets = &Facets{
				Brands: result.Facets.Brands,
				Attrs:  result.Facets.Attrs,
			}
		}

		return &SCUListRespData{
			Items:  result.Items,
			Total:  result.Total,
			Page:   result.Page,
			Limit:  result.Limit,
			Facets: facets,
		}, nil
	}

	return &SCUListRespData{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toFloat64Safe(v interface{}) float64 {
	if f, ok := toFloat64(v); ok {
		return f
	}
	return 0
}

// toInt64 extracts int64 from any JSON-decoded value.
func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case string:
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

// toString extracts string from any JSON-decoded value.
func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(val, 10)
	case int:
		return strconv.Itoa(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// toFloat64Val extracts float64 from any JSON-decoded value (no bool).
func toFloat64Val(v any) float64 {
	if f, ok := toFloat64(v); ok {
		return f
	}
	return 0
}

// toAttrMap converts a JSON-decoded value to map[string]interface{}.
func toAttrKV(v any) []model.KeyValue {
	if m, ok := v.(map[string]any); ok {
		out := make([]model.KeyValue, 0, len(m))
		for k, val := range m {
			out = append(out, model.KeyValue{Key: k, Value: fmt.Sprintf("%v", val)})
		}
		return out
	}
	return nil
}

// toStringSlice converts a JSON-decoded value to []string.
func toStringSlice(v any) []string {
	if arr, ok := v.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, val := range arr {
			out = append(out, toString(val))
		}
		return out
	}
	return nil
}

// loadProductsFromIDs loads products from a set of IDs and applies price/attr-range filters.
// Used for facet computation on the pre-attr-filter candidate set.
func (r *ProductRepo) loadProductsFromIDs(ids map[int64]struct{}, params ListParams) []model.Product {
	if len(ids) == 0 {
		return nil
	}
	var products []model.Product
	for id := range ids {
		p, err := r.Get(id)
		if err != nil {
			continue
		}
		// Price range filter
		if params.PriceMin != nil && p.Price < *params.PriceMin {
			continue
		}
		if params.PriceMax != nil && p.Price > *params.PriceMax {
			continue
		}
		// Attribute range filters
		skip := false
		for code, rng := range params.AttrRanges {
			val, ok := kvGet(p.Attributes, code)
			if !ok {
				skip = true
				break
			}
			num, ok := toFloat64(val)
			if !ok {
				skip = true
				break
			}
			if rng.Min > 0 && num < rng.Min {
				skip = true
				break
			}
			if rng.Max > 0 && num > rng.Max {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		products = append(products, *p)
	}
	return products
}

// GetAllProducts returns all products (for analytics).
// Uses turbo search as the source.
func (r *ProductRepo) GetAllProducts() ([]model.Product, error) {
	if r.turboSearch != nil {
		return r.turboSearch.GetAllProducts()
	}
	return nil, nil
}

// ReindexAll rebuilds all indexes for all products.
// Useful after schema/indexing changes or corrupted indexes.
func (r *ProductRepo) ReindexAll() error {
	products, err := r.GetAllProducts()
	if err != nil {
		return fmt.Errorf("get all products for reindex: %w", err)
	}

	for i := range products {
		if err := r.reindexProduct(&products[i]); err != nil {
			// Log but continue with next product
			fmt.Printf("reindex product %d failed: %v\n", products[i].ID, err)
		}
	}

	return nil
}

// ReindexProduct rebuilds all turbo indexes for a single product without changing its data.
func (r *ProductRepo) ReindexProduct(p *model.Product) error {
	if r.turboSearch == nil {
		return nil
	}
	// Remove old turbo indexes
	_ = r.turboSearch.UnindexProduct(p)
	// Rebuild turbo indexes
	return r.turboSearch.IndexProduct(p)
}

// reindexProduct is the internal version (kept for backward compatibility).
func (r *ProductRepo) reindexProduct(p *model.Product) error {
	return r.ReindexProduct(p)
}

// applyPromoBoost boosts products that are part of active promo campaigns.
// For position-type campaigns: promoted products are moved to the front.
// This is a simple in-memory boost applied before sorting.
// applyPromoBoost inserts promoted products into the result based on active campaigns.
// It matches campaigns against the current search context (filters) and inserts products
// according to TargetPosition (top, top_N, inline_N).
// Returns a map of promoted product IDs -> campaign IDs for logging and UI marking.
func (r *ProductRepo) applyPromoBoost(products *[]model.Product, ctx *SearchContext) map[int64]int64 {
	if len(*products) == 0 || ctx == nil {
		return nil
	}

	campaigns, err := r.promoCampaignRepo.GetActiveCampaigns()
	if err != nil || len(campaigns) == 0 {
		return nil
	}

	// Build set of candidate product IDs from current results
	candidateIDs := make(map[int64]struct{}, len(*products))
	for _, p := range *products {
		candidateIDs[p.ID] = struct{}{}
	}

	// Find matching campaigns and collect promoted products
	type promoEntry struct {
		Product    model.Product
		CampaignID int64
		Position   string
		Priority   int // higher = earlier
	}

	var promotedEntries []promoEntry

	for _, c := range campaigns {
		// Check if campaign matches current search context
		if !r.campaignMatchesContext(&c, ctx) {
			continue
		}

		// Check budget
		if c.BudgetUsed >= c.BudgetTotal && c.BudgetTotal > 0 {
			continue
		}

		// Determine which products are promoted by this campaign
		var productIDs []int64
		if len(c.ProductIDs) > 0 {
			// Specific products
			productIDs = c.ProductIDs
		} else {
			// All company products in candidate set that match target filters
			for _, p := range *products {
				if p.CompanyID == c.CompanyID && r.productMatchesTargetFilters(p, c.TargetFilters) {
					productIDs = append(productIDs, p.ID)
				}
			}
		}

		// Collect promoted products (only those in candidate set)
		for _, pid := range productIDs {
			if _, ok := candidateIDs[pid]; !ok {
				continue
			}
			// Find product in results
			for i := range *products {
				if (*products)[i].ID == pid {
					priority := 100
					if c.TargetPosition == "top" {
						priority = 1000
					} else if strings.HasPrefix(c.TargetPosition, "top_") {
						priority = 900
					}
					promotedEntries = append(promotedEntries, promoEntry{
						Product:    (*products)[i],
						CampaignID: c.ID,
						Position:   c.TargetPosition,
						Priority:   priority,
					})
					break
				}
			}
		}
	}

	if len(promotedEntries) == 0 {
		return nil
	}

	// Sort promoted entries by priority (higher first), then by campaign ID for stability
	sort.SliceStable(promotedEntries, func(i, j int) bool {
		if promotedEntries[i].Priority != promotedEntries[j].Priority {
			return promotedEntries[i].Priority > promotedEntries[j].Priority
		}
		return promotedEntries[i].CampaignID < promotedEntries[j].CampaignID
	})

	// Deduplicate promoted products (keep first occurrence)
	promotedIDs := make(map[int64]int64) // product_id -> campaign_id
	var promoted []model.Product
	for _, e := range promotedEntries {
		if _, ok := promotedIDs[e.Product.ID]; ok {
			continue
		}
		promotedIDs[e.Product.ID] = e.CampaignID
		promoted = append(promoted, e.Product)
	}

	// Separate promoted from regular products
	prods := *products
	var regular []model.Product
	for _, p := range prods {
		if _, ok := promotedIDs[p.ID]; !ok {
			regular = append(regular, p)
		}
	}

	// Build set for checking
	promotedSet := make(map[int64]struct{}, len(promotedIDs))
	for pid := range promotedIDs {
		promotedSet[pid] = struct{}{}
	}

	// Merge according to target positions
	*products = r.mergePromotedProducts(promoted, regular)

	// Log impressions for promoted products
	r.logPromoImpressions(promotedIDs, ctx)

	// Return set for UI marking
	return promotedIDs
}

// campaignMatchesContext checks if a campaign's TargetFilters match the current search context.
func (r *ProductRepo) campaignMatchesContext(c *model.PromoCampaign, ctx *SearchContext) bool {
	tf := c.TargetFilters

	// If campaign specifies category_ids, current context must match one of them.
	// If context has no category_id (0) but campaign requires one, don't match.
	if len(tf.CategoryIDs) > 0 {
		if ctx.CategoryID == 0 {
			return false
		}
		found := false
		for _, cid := range tf.CategoryIDs {
			if cid == ctx.CategoryID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// If campaign specifies attribute_filters, current context must include all of them
	if len(tf.AttributeFilters) > 0 && len(ctx.AttrFilters) > 0 {
		for _, kv := range tf.AttributeFilters {
			actualVals, ok := ctx.AttrFilters[kv.Key]
			if !ok {
				return false
			}
			expectedVal := kv.Value
			// Check if any of the actual values matches the expected value
			if !r.valueMatchesFilter(actualVals, expectedVal) {
				return false
			}
		}
	}

	return true
}

// valueMatchesFilter checks if any of the actual values matches the expected filter value.
func (r *ProductRepo) valueMatchesFilter(actualVals []string, expected interface{}) bool {
	expectedStr, ok := expected.(string)
	if !ok {
		// For non-string expected values, try to convert
		expectedStr = fmt.Sprintf("%v", expected)
	}
	for _, v := range actualVals {
		if strings.EqualFold(v, expectedStr) {
			return true
		}
	}
	return false
}

// productMatchesTargetFilters checks if a product matches the campaign's TargetFilters.
func (r *ProductRepo) productMatchesTargetFilters(p model.Product, tf model.TargetFilters) bool {
	// Check category
	if len(tf.CategoryIDs) > 0 {
		found := false
		for _, cid := range tf.CategoryIDs {
			if cid == p.CategoryID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check attribute filters
	for _, kv := range tf.AttributeFilters {
		actualVal, ok := kvGet(p.Attributes, kv.Key)
		if !ok {
			return false
		}
		actualStr := actualVal
		expectedStr := kv.Value
		if !strings.EqualFold(actualStr, expectedStr) {
			return false
		}
	}

	return true
}

// mergePromotedProducts merges promoted products into regular results based on their positions.
func (r *ProductRepo) mergePromotedProducts(promoted, regular []model.Product) []model.Product {
	if len(promoted) == 0 {
		return regular
	}
	if len(regular) == 0 {
		return promoted
	}

	// Group promoted products by position type
	var topProducts []model.Product

	for _, p := range promoted {
		topProducts = append(topProducts, p)
	}

	// Simple strategy: all promoted products go to the top
	// More sophisticated positioning can be added later based on TargetPosition
	result := make([]model.Product, 0, len(promoted)+len(regular))
	result = append(result, topProducts...)
	result = append(result, regular...)

	return result
}

// logPromoImpressions logs impression events for promoted products.
func (r *ProductRepo) logPromoImpressions(promoted map[int64]int64, ctx *SearchContext) {
	if len(promoted) == 0 || r.promoLogRepo == nil {
		return
	}

	// Build context info for logging
	var contextKV []model.KeyValue
	contextKV = append(contextKV, model.KeyValue{Key: "query", Value: ctx.Q})
	if ctx.CategoryID != 0 {
		contextKV = append(contextKV, model.KeyValue{Key: "category_id", Value: fmt.Sprintf("%d", ctx.CategoryID)})
	}
	if ctx.Brand != "" {
		contextKV = append(contextKV, model.KeyValue{Key: "brand", Value: ctx.Brand})
	}
	if len(ctx.AttrFilters) > 0 {
		// Serialize attr_filters as JSON
		attrJSON, _ := json.Marshal(ctx.AttrFilters)
		contextKV = append(contextKV, model.KeyValue{Key: "attr_filters", Value: string(attrJSON)})
	}

	// Create a log entry for each promoted product -> campaign
	for _, campaignId := range promoted {
		log := &model.PromoLog{
			CampaignID: campaignId,
			EventType:  model.PromoEventImpression,
			Context:    contextKV,
			Cost:       0, // cost calculated later during billing
			CreatedAt:  time.Now().Unix(),
		}
		if err := r.promoLogRepo.Create(log); err != nil {
			fmt.Printf("promo log create error: %v\n", err)
		}
	}
}
