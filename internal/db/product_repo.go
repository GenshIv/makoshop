package db

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type ProductRepo struct {
	store             *Store
	promoCampaignRepo *PromoCampaignRepo
	promoPlanRepo     *PromoPlanRepo
	promoLogRepo      *PromoLogRepo
	turboSearch       *TurboProductSearch

	// Single-writer channel for batch operations
	batchChan chan batchTask
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
		p.CreatedAt = time.Now()
		p.UpdatedAt = time.Now()
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
		p.CreatedAt = time.Now()
		p.UpdatedAt = time.Now()
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
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
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
	p.UpdatedAt = time.Now()

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

func (r *ProductRepo) Delete(id int64) error {
	p, err := r.Get(id)
	if err != nil {
		return err
	}
	if r.turboSearch != nil {
		_ = r.turboSearch.UnindexProduct(p)
	}
	if err := r.store.DocDelete(KeyProduct(id)); err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
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
	ID         int64                  `json:"id"`
	SKU        string                 `json:"sku"`
	Name       string                 `json:"name"`
	CategoryID int64                  `json:"category_id"`
	CompanyID  int64                  `json:"company_id"`
	Brand      string                 `json:"brand,omitempty"`
	Price      float64                `json:"price"`
	Currency   string                 `json:"currency"`
	Status     model.ProductStatus    `json:"status"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	Images     []string               `json:"images,omitempty"`
	Promoted   bool                   `json:"promoted,omitempty"` // true if shown via promotion campaign
}

// List returns paginated product list with search, filters, sorting.
// Returns: items, totalCount.
// Deprecated: use ListWithFacets for faceted search support.
func (r *ProductRepo) List(params ListParams) ([]ProductListItem, int64, error) {
	result, err := r.ListWithFacets(params)
	if err != nil {
		return nil, 0, err
	}
	return result.Items, result.Total, nil
}

// ListWithFacets returns paginated product list with optional facets.
// Now uses TurboProductSearch as primary backend.
func (r *ProductRepo) ListWithFacets(params ListParams) (*ListResult, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	if r.turboSearch != nil {
		// Map ListParams -> TurboListParams
		facetCodes := params.FacetBrandCodes
		if len(facetCodes) == 0 {
			// По умолчанию считаем фасеты для brand
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

		// Конвертируем TurboFacets -> Facets
		var facets *Facets
		if result.Facets != nil {
			facets = &Facets{
				Brands: result.Facets.Brands,
				Attrs:  result.Facets.Attrs,
			}
		}

		return &ListResult{
			Items:  result.Items,
			Total:  result.Total,
			Page:   result.Page,
			Limit:  result.Limit,
			Facets: facets,
		}, nil
	}

	// Fallback: should not happen if turbo is enabled.
	return &ListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
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
			val, ok := p.Attributes[code]
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
		for code, expectedVal := range tf.AttributeFilters {
			actualVals, ok := ctx.AttrFilters[code]
			if !ok {
				return false
			}
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
	for code, expectedVal := range tf.AttributeFilters {
		actualVal, ok := p.Attributes[code]
		if !ok {
			return false
		}
		actualStr := fmt.Sprintf("%v", actualVal)
		expectedStr := fmt.Sprintf("%v", expectedVal)
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
	contextMap := make(map[string]interface{})
	contextMap["query"] = ctx.Q
	if ctx.CategoryID != 0 {
		contextMap["category_id"] = ctx.CategoryID
	}
	if ctx.Brand != "" {
		contextMap["brand"] = ctx.Brand
	}
	if len(ctx.AttrFilters) > 0 {
		contextMap["attr_filters"] = ctx.AttrFilters
	}

	// Create a log entry for each promoted product -> campaign
	for _, campaignId := range promoted {
		log := &model.PromoLog{
			CampaignID: campaignId,
			EventType:  model.PromoEventImpression,
			Context:    contextMap,
			Cost:       0, // cost calculated later during billing
			CreatedAt:  time.Now(),
		}
		if err := r.promoLogRepo.Create(log); err != nil {
			fmt.Printf("promo log create error: %v\n", err)
		}
	}
}
