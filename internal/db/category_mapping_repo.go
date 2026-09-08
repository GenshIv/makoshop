package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyCategoryMappingList = "cat_mapping_list:"
)

// CategoryMappingRepo stores explicit source→target category mappings.
type CategoryMappingRepo struct {
	store *Store

	// In-memory cache for fast lookups during bulk import.
	cacheMu      sync.RWMutex
	cache        map[string]*model.CategoryMapping           // global rules: source_code -> mapping
	companyCache map[int64]map[string]*model.CategoryMapping // company-specific: company_id -> (source_code -> mapping)
	cacheLoaded  bool
}

func NewCategoryMappingRepo(store *Store) *CategoryMappingRepo {
	return &CategoryMappingRepo{store: store}
}

// LoadCache loads all mappings into in-memory maps for O(1) lookups.
// Call this once before bulk import operations.
func (r *CategoryMappingRepo) LoadCache() error {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	mappings, err := r.List()
	if err != nil {
		return fmt.Errorf("list mappings for cache: %w", err)
	}

	r.cache = make(map[string]*model.CategoryMapping)
	r.companyCache = make(map[int64]map[string]*model.CategoryMapping)

	for _, m := range mappings {
		if m.CompanyID != nil {
			if r.companyCache[*m.CompanyID] == nil {
				r.companyCache[*m.CompanyID] = make(map[string]*model.CategoryMapping)
			}
			r.companyCache[*m.CompanyID][m.SourceCode] = &m
		} else {
			r.cache[m.SourceCode] = &m
		}
	}

	r.cacheLoaded = true
	return nil
}

// InvalidateCache clears the in-memory cache (call after Create/Update/Delete).
func (r *CategoryMappingRepo) InvalidateCache() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.cache = nil
	r.companyCache = nil
	r.cacheLoaded = false
}

// Create adds a new mapping rule.
func (r *CategoryMappingRepo) Create(m *model.CategoryMapping) error {
	if m.SourceCode == "" || m.TargetCategoryID <= 0 {
		return fmt.Errorf("source_code and target_category_id are required")
	}
	id, err := r.store.NextID("category_mapping")
	if err != nil {
		return fmt.Errorf("next_id category_mapping: %w", err)
	}
	m.ID = id
	now := time.Now().Unix()
	m.CreatedAt = now
	m.UpdatedAt = now

	key := KeyCategoryMapping(id)
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal category_mapping: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("save category_mapping: %w", err)
	}
	if _, err := r.store.db.TurboPutIndexString(turboKeyCategoryMappingList, key); err != nil {
		_ = r.store.DocDelete(key)
		return fmt.Errorf("turbo index cat_mapping_list: %w", err)
	}
	return nil
}

// Get returns a mapping by ID.
func (r *CategoryMappingRepo) Get(id int64) (*model.CategoryMapping, error) {
	data, err := r.store.DocGet(KeyCategoryMapping(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("category mapping %d not found", id)
		}
		return nil, fmt.Errorf("get category_mapping: %w", err)
	}
	var m model.CategoryMapping
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal category_mapping: %w", err)
	}
	return &m, nil
}

// List returns all mapping rules.
func (r *CategoryMappingRepo) List() ([]model.CategoryMapping, error) {
	tokens, err := r.store.db.TurboGetIndexTokens(turboKeyCategoryMappingList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}
	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get category_mappings: %w", err)
	}
	mappings := make([]model.CategoryMapping, 0, len(docs))
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		var m model.CategoryMapping
		if err := json.Unmarshal(doc, &m); err != nil {
			continue
		}
		mappings = append(mappings, m)
	}
	return mappings, nil
}

// Update modifies an existing mapping rule.
func (r *CategoryMappingRepo) Update(id int64, updater func(*model.CategoryMapping)) error {
	m, err := r.Get(id)
	if err != nil {
		return err
	}
	updater(m)
	m.UpdatedAt = time.Now().Unix()

	key := KeyCategoryMapping(id)
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal category_mapping: %w", err)
	}
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("update category_mapping: %w", err)
	}
	return nil
}

// Delete removes a mapping rule.
func (r *CategoryMappingRepo) Delete(id int64) error {
	key := KeyCategoryMapping(id)
	if err := r.store.DocDelete(key); err != nil {
		return fmt.Errorf("delete category_mapping: %w", err)
	}
	_, _ = r.store.db.TurboDeleteIndexString(turboKeyCategoryMappingList, key)
	return nil
}

// FindBySourceCode looks up a mapping by source code. Returns the first match
// (company-specific rules take precedence over global). Uses in-memory cache
// if loaded for O(1) lookups during bulk import.
func (r *CategoryMappingRepo) FindBySourceCode(sourceCode string, companyID *int64) (*model.CategoryMapping, error) {
	r.cacheMu.RLock()
	if r.cacheLoaded {
		// Prefer company-specific rule
		if companyID != nil {
			if cc := r.companyCache[*companyID]; cc != nil {
				if m, ok := cc[sourceCode]; ok {
					r.cacheMu.RUnlock()
					return m, nil
				}
			}
		}
		// Fall back to global rule
		if m, ok := r.cache[sourceCode]; ok {
			r.cacheMu.RUnlock()
			return m, nil
		}
		r.cacheMu.RUnlock()
		return nil, nil
	}
	r.cacheMu.RUnlock()

	// Cache not loaded — fall back to slow List() approach.
	mappings, err := r.List()
	if err != nil {
		return nil, err
	}
	for _, m := range mappings {
		if m.SourceCode == sourceCode && m.CompanyID != nil && companyID != nil && *m.CompanyID == *companyID {
			return &m, nil
		}
	}
	for _, m := range mappings {
		if m.SourceCode == sourceCode && m.CompanyID == nil {
			return &m, nil
		}
	}
	return nil, nil
}

// ScanPriceFiles analyzes recent products and suggests category mappings.
// Returns a map of source code → suggestion (count + most common target category).
func (r *CategoryMappingRepo) ScanPriceFiles(productRepo *ProductRepo) (map[string]*ScanSuggestion, error) {
	products, err := productRepo.GetAllProducts()
	if err != nil {
		return nil, fmt.Errorf("get all products: %w", err)
	}

	sourceGroups := make(map[string]map[int64]int)         // source_code → {category_id → count}
	sourceCompanies := make(map[string]map[int64]struct{}) // source_code → set of company IDs

	for _, p := range products {
		if p.ShopCategory == "" {
			continue
		}
		if sourceGroups[p.ShopCategory] == nil {
			sourceGroups[p.ShopCategory] = make(map[int64]int)
		}
		sourceGroups[p.ShopCategory][p.CategoryID]++

		if sourceCompanies[p.ShopCategory] == nil {
			sourceCompanies[p.ShopCategory] = make(map[int64]struct{})
		}
		sourceCompanies[p.ShopCategory][p.CompanyID] = struct{}{}
	}

	suggestions := make(map[string]*ScanSuggestion)
	for sourceCode, catCounts := range sourceGroups {
		total := 0
		var topCat int64
		topCount := 0
		for catID, count := range catCounts {
			total += count
			if count > topCount {
				topCount = count
				topCat = catID
			}
		}
		confidence := float64(topCount) / float64(total) * 100.0

		// Convert company set to slice
		companies := make([]int64, 0, len(sourceCompanies[sourceCode]))
		for compID := range sourceCompanies[sourceCode] {
			companies = append(companies, compID)
		}

		suggestions[sourceCode] = &ScanSuggestion{
			SourceCode:       sourceCode,
			TotalProducts:    total,
			TopCategoryID:    topCat,
			TopCategoryCount: topCount,
			Confidence:       confidence,
			Companies:        companies,
		}
	}

	return suggestions, nil
}

// ScanSuggestion represents a suggested category mapping based on scan results.
type ScanSuggestion struct {
	SourceCode       string  `json:"source_code"`
	TotalProducts    int     `json:"total_products"`
	TopCategoryID    int64   `json:"top_category_id"`
	TopCategoryCount int     `json:"top_category_count"`
	Confidence       float64 `json:"confidence"` // percentage 0-100
	Companies        []int64 `json:"companies"`  // company IDs that use this source code
}

// Key builder
func KeyCategoryMapping(id int64) string {
	return fmt.Sprintf("cat_mapping:%d", id)
}
