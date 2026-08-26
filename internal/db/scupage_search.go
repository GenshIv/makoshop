package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
)

// EANPageSearch — turbo-поиск по посадочным страницам (EANPage).
// Каталог и фильтрация работают по EANPage, не по товарам.
type EANPageSearch struct {
	db           *makodb.ShardedDB
	repo         *EANPageRepo
	productRepo  *ProductRepo
	categoryRepo *CategoryRepo
	enabled      bool

	// Cache for category descendants to avoid repeated expensive lookups.
	// Key: catID, Value: []int64 of descendant IDs (not including catID itself).
	descMu       sync.Mutex
	descCache    map[int64][]int64
	descCacheTTL time.Duration
}

func NewEANPageSearch(db *makodb.ShardedDB, repo *EANPageRepo, productRepo *ProductRepo, categoryRepo *CategoryRepo, enabled bool) *EANPageSearch {
	return &EANPageSearch{
		db:           db,
		repo:         repo,
		productRepo:  productRepo,
		categoryRepo: categoryRepo,
		enabled:      enabled,
		descCache:    make(map[int64][]int64),
		descCacheTTL: 5 * time.Minute,
	}
}

// ---------- key helpers ----------

func eanpageKeyBrand(brandID int64) string { return "eanpage_brand:" + strconv.FormatInt(brandID, 10) }
func eanpageKeyVendor(companyID int64) string {
	return "eanpage_vendor:" + strconv.FormatInt(companyID, 10)
}
func eanpageKeyAttr(code string, value string) string {
	return "eanpage_attr:" + code + ":" + value
}
func eanpageKeyText(token string) string { return "eanpage_text:" + token }

// eanpageKeyCategoryUnion returns the key for a category union index.
// Contains all SCU pages of this category and all descendants.
func eanpageKeyCategoryUnion(catID int64) string {
	return "eanpage_cat_union:" + strconv.FormatInt(catID, 10)
}

// Sort index keys per category: eanpage_sort:{catID}:{type}
// Global sort indexes (catID=0): eanpage_sort:0:{type}
func eanpageSortKey(catID int64, sortType string) string {
	return "eanpage_sort:" + strconv.FormatInt(catID, 10) + ":" + sortType
}

// Sort index keys per category: eanpage_sort:{catID}:{type}
// Global sort indexes (catID=0): eanpage_sort:0:{type}
func eanpageDocKey(docID int64) string {
	return "eanpage:" + strconv.FormatInt(docID, 10)
}

// NumSort price index per category: eanpage_price:{catID}
func eanpageNumSortPriceKey(catID int64) string {
	return "eanpage_price:" + strconv.FormatInt(catID, 10)
}

const (
	eanpageSortTypePriceAsc      = "price_asc"
	eanpageSortTypePriceDesc     = "price_desc"
	eanpageSortTypeCreatedAtDesc = "created_at_desc"
)

// ---------- indexing ----------

// IndexEANPage indexes a single SCU page into turbo indexes.
// Collects all indexes in memory, then writes in batch (no repeated writes to same key).
func (s *EANPageSearch) IndexEANPage(sp *model.EANPage) error {
	if !s.enabled {
		return nil
	}
	docID := KeyEANPage(sp.ID)

	// Collect all indexes in memory
	indexes := make(map[string][]string)

	// Category union index for all ancestors.
	// Union index already contains this category + all descendants.
	if sp.CategoryID != 0 {
		ancestors, err := s.getCategoryAncestors(sp.CategoryID)
		if err != nil {
			ancestors = []int64{sp.CategoryID}
		}
		for _, cid := range ancestors {
			indexes[eanpageKeyCategoryUnion(cid)] = append(indexes[eanpageKeyCategoryUnion(cid)], docID)
		}
	}

	// Brand index
	if sp.BrandID != 0 {
		indexes[eanpageKeyBrand(sp.BrandID)] = append(indexes[eanpageKeyBrand(sp.BrandID)], docID)
	}

	// Attributes index
	for _, kv := range sp.Attributes {
		valStr := kv.Value
		if valStr != "" {
			indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docID)
			// Attr values per category for filter UI: own category only (no ancestors)
			if sp.CategoryID != 0 {
				labelKey := "attr_label:" + kv.Key + ":" + valStr
				_ = s.db.TurboRawWrite(labelKey, []byte(valStr))
				// Store values in a JSON set for this category+code combination
				key := "attr_values_cat:" + kv.Key + ":" + strconv.FormatInt(sp.CategoryID, 10)
				data, _ := s.db.TurboRawRead(key)
				var valuesSet map[string]struct{}
				if data != nil && len(data) > 0 {
					json.Unmarshal(data, &valuesSet)
				}
				if valuesSet == nil {
					valuesSet = make(map[string]struct{})
				}
				if _, exists := valuesSet[valStr]; !exists {
					valuesSet[valStr] = struct{}{}
					buf, _ := json.Marshal(valuesSet)
					_ = s.db.TurboRawWrite(key, buf)
				}
				// Update attrdef_cat_codes:{catID}
				catCodesKey := "attrdef_cat_codes:" + strconv.FormatInt(sp.CategoryID, 10)
				catCodesData, _ := s.db.TurboRawRead(catCodesKey)
				var codes []string
				if catCodesData != nil && len(catCodesData) > 0 {
					json.Unmarshal(catCodesData, &codes)
				}
				found := false
				for _, c := range codes {
					if c == kv.Key {
						found = true
						break
					}
				}
				if !found {
					codes = append(codes, kv.Key)
					buf, _ := json.Marshal(codes)
					_ = s.db.TurboRawWrite(catCodesKey, buf)
				}
			}
		}
	}

	// Text index
	for _, tok := range tokenizeEANPage(sp) {
		indexes[eanpageKeyText(tok)] = append(indexes[eanpageKeyText(tok)], docID)
	}

	// Write all indexes in batch (one write per key)
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := s.db.TurboPutBatchIndexString(key, docIDs); err != nil {
			return fmt.Errorf("turbo eanpage batch index %s: %w", key, err)
		}
	}

	return nil
}

// IndexEANPageBatch indexes many SCU pages using batch turbo writes.
// ~10x faster than calling IndexEANPage in a loop.
func (s *EANPageSearch) IndexEANPageBatch(pages []*model.EANPage) error {
	if !s.enabled || len(pages) == 0 {
		return nil
	}

	// Collect all indexes in memory
	indexes := make(map[string][]string)
	// Attr values per category for filter UI: code -> {catID -> {value -> value}}
	attrCatRef := make(map[string]map[int64]map[string]string)
	// Track which attribute codes are used per category (for turbo_attrdef_cat_codes)
	catCodes := make(map[int64]map[string]struct{})

	for _, sp := range pages {
		docID := KeyEANPage(sp.ID)

		// Category union index for all ancestors.
		if sp.CategoryID != 0 {
			ancestors, err := s.getCategoryAncestors(sp.CategoryID)
			if err != nil {
				ancestors = []int64{sp.CategoryID}
			}
			for _, cid := range ancestors {
				indexes[eanpageKeyCategoryUnion(cid)] = append(indexes[eanpageKeyCategoryUnion(cid)], docID)
			}
		}

		// Brand index
		if sp.BrandID != 0 {
			indexes[eanpageKeyBrand(sp.BrandID)] = append(indexes[eanpageKeyBrand(sp.BrandID)], docID)
		}

		// Attributes index
		for _, kv := range sp.Attributes {
			valStr := kv.Value
			if valStr != "" {
				indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docID)
				// Attr values per category for filter UI: own category only (no ancestors)
				if sp.CategoryID != 0 {
					if attrCatRef[kv.Key] == nil {
						attrCatRef[kv.Key] = make(map[int64]map[string]string)
					}
					if attrCatRef[kv.Key][sp.CategoryID] == nil {
						attrCatRef[kv.Key][sp.CategoryID] = make(map[string]string)
					}
					attrCatRef[kv.Key][sp.CategoryID][valStr] = valStr
					// Track code for this category
					if catCodes[sp.CategoryID] == nil {
						catCodes[sp.CategoryID] = make(map[string]struct{})
					}
					catCodes[sp.CategoryID][kv.Key] = struct{}{}
				}
			}
		}

		// Text index
		for _, tok := range tokenizeEANPage(sp) {
			indexes[eanpageKeyText(tok)] = append(indexes[eanpageKeyText(tok)], docID)
		}
	}

	// Write all indexes in batch
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := s.db.TurboPutBatchIndexString(key, docIDs); err != nil {
			fmt.Printf("WARN: eanpage batch index %s: %v\n", key, err)
		}
	}

	// Write attr values per category indexes for filter UI
	for code, catMap := range attrCatRef {
		for catID, values := range catMap {
			key := "attr_values_cat:" + code + ":" + strconv.FormatInt(catID, 10)
			// Store all values as a JSON set
			valuesSet := make(map[string]struct{})
			for val := range values {
				valuesSet[val] = struct{}{}
			}
			if len(valuesSet) > 0 {
				buf, _ := json.Marshal(valuesSet)
				_ = s.db.TurboRawWrite(key, buf)
			}
			// Write labels
			for val := range values {
				labelKey := "attr_label:" + code + ":" + val
				_ = s.db.TurboRawWrite(labelKey, []byte(val))
			}
		}
	}

	// Update attrdef_cat_codes:{catID} with codes used by SCU pages
	for catID, codes := range catCodes {
		key := "attrdef_cat_codes:" + strconv.FormatInt(catID, 10)
		// Read existing codes
		data, _ := s.db.TurboRawRead(key)
		var existing []string
		if data != nil && len(data) > 0 {
			json.Unmarshal(data, &existing)
		}
		existingSet := make(map[string]struct{}, len(existing))
		for _, c := range existing {
			existingSet[c] = struct{}{}
		}
		// Add new codes
		for c := range codes {
			if _, ok := existingSet[c]; !ok {
				existing = append(existing, c)
				existingSet[c] = struct{}{}
			}
		}
		buf, _ := json.Marshal(existing)
		_ = s.db.TurboRawWrite(key, buf)
	}

	return nil
}

// UnindexEANPage removes a SCU page from all turbo indexes.
func (s *EANPageSearch) UnindexEANPage(sp *model.EANPage) error {
	if !s.enabled {
		return nil
	}
	docID := KeyEANPage(sp.ID)

	if sp.CategoryID != 0 {
		ancestors, err := s.getCategoryAncestors(sp.CategoryID)
		if err != nil {
			ancestors = []int64{sp.CategoryID}
		}
		for _, cid := range ancestors {
			s.db.TurboDeleteIndexString(eanpageKeyCategoryUnion(cid), docID)
		}
	}

	if sp.BrandID != 0 {
		s.db.TurboDeleteIndexString(eanpageKeyBrand(sp.BrandID), docID)
	}

	for _, kv := range sp.Attributes {
		valStr := kv.Value
		if valStr != "" {
			s.db.TurboDeleteIndexString(eanpageKeyAttr(kv.Key, valStr), docID)
		}
	}

	for _, tok := range tokenizeEANPage(sp) {
		s.db.TurboDeleteIndexString(eanpageKeyText(tok), docID)
	}

	return nil
}

// BuildSortIndexes rebuilds all sort indexes for SCU pages per category.
// Each category has its own sort indexes: eanpage_sort:{catID}:{type}
// and numSort price index: eanpage_price:{catID}
func (s *EANPageSearch) BuildSortIndexes() error {
	if !s.enabled {
		return nil
	}

	start := time.Now().Unix()
	fmt.Println("[SCUPAGE] Building sort indexes per category...")

	all, err := s.repo.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	// Group by category ancestors (each ancestor gets its own sort index)
	type priced struct {
		docID string
		price float64
	}
	type timed struct {
		docID string
		ts    int64
	}

	// Maps: catID -> list of entries
	catPricesAsc := make(map[int64][]priced)
	catPricesDesc := make(map[int64][]priced)
	catCreatedDesc := make(map[int64][]timed)
	catPricePairs := make(map[int64][]makodb.TurboNumSortPair)

	for _, sp := range all {
		docIDKey := KeyEANPage(sp.ID)
		priceVal := uint64(sp.MinPrice * 100)

		scCreated := sp.CreatedAt
		// Add to global (catID=0) index
		catPricesAsc[0] = append(catPricesAsc[0], priced{docID: docIDKey, price: sp.MinPrice})
		catPricesDesc[0] = append(catPricesDesc[0], priced{docID: docIDKey, price: sp.MinPrice})
		catCreatedDesc[0] = append(catCreatedDesc[0], timed{docID: docIDKey, ts: int64(scCreated)})
		catPricePairs[0] = append(catPricePairs[0], makodb.TurboNumSortPair{Value: priceVal, DocID: docIDKey})

		// Add to all ancestor categories
		if sp.CategoryID != 0 {
			ancestors, err := s.getCategoryAncestors(sp.CategoryID)
			if err != nil {
				ancestors = []int64{sp.CategoryID}
			}
			for _, cid := range ancestors {
				catPricesAsc[cid] = append(catPricesAsc[cid], priced{docID: docIDKey, price: sp.MinPrice})
				catPricesDesc[cid] = append(catPricesDesc[cid], priced{docID: docIDKey, price: sp.MinPrice})
				catCreatedDesc[cid] = append(catCreatedDesc[cid], timed{docID: docIDKey, ts: int64(scCreated)})
				catPricePairs[cid] = append(catPricePairs[cid], makodb.TurboNumSortPair{Value: priceVal, DocID: docIDKey})
			}
		}
	}

	// Build sort indexes for each category
	for catID, entries := range catPricesAsc {
		if len(entries) == 0 {
			continue
		}

		// Price asc
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].price != entries[j].price {
				return entries[i].price < entries[j].price
			}
			return entries[i].docID < entries[j].docID
		})
		docIDsAsc := make([]string, len(entries))
		for i, e := range entries {
			docIDsAsc[i] = e.docID
		}
		if err := s.db.TurboPutSortIndexString(eanpageSortKey(catID, eanpageSortTypePriceAsc), docIDsAsc); err != nil {
			fmt.Printf("WARN: sort index %s: %v\n", eanpageSortKey(catID, eanpageSortTypePriceAsc), err)
		}

		// Price desc
		entriesDesc := catPricesDesc[catID]
		sort.Slice(entriesDesc, func(i, j int) bool {
			if entriesDesc[i].price != entriesDesc[j].price {
				return entriesDesc[i].price > entriesDesc[j].price
			}
			return entriesDesc[i].docID < entriesDesc[j].docID
		})
		docIDsDesc := make([]string, len(entriesDesc))
		for i, e := range entriesDesc {
			docIDsDesc[i] = e.docID
		}
		if err := s.db.TurboPutSortIndexString(eanpageSortKey(catID, eanpageSortTypePriceDesc), docIDsDesc); err != nil {
			fmt.Printf("WARN: sort index %s: %v\n", eanpageSortKey(catID, eanpageSortTypePriceDesc), err)
		}

		// Created at desc
		entriesTime := catCreatedDesc[catID]
		sort.Slice(entriesTime, func(i, j int) bool {
			if entriesTime[i].ts != entriesTime[j].ts {
				return entriesTime[i].ts > entriesTime[j].ts
			}
			return entriesTime[i].docID < entriesTime[j].docID
		})
		docIDsTime := make([]string, len(entriesTime))
		for i, e := range entriesTime {
			docIDsTime[i] = e.docID
		}
		if err := s.db.TurboPutSortIndexString(eanpageSortKey(catID, eanpageSortTypeCreatedAtDesc), docIDsTime); err != nil {
			fmt.Printf("WARN: sort index %s: %v\n", eanpageSortKey(catID, eanpageSortTypeCreatedAtDesc), err)
		}

		// NumSort price index
		if pairs, ok := catPricePairs[catID]; ok && len(pairs) > 0 {
			_, _ = s.db.TurboPutNumSortBatch(eanpageNumSortPriceKey(catID), pairs)
		}
	}

	fmt.Printf("[SCUPAGE] Sort indexes built: %d pages, %d categories, %v\n", len(all), len(catPricesAsc), time.Since(time.Unix(start, 0)))
	return nil
}

// ---------- search ----------

type EANPageListParams struct {
	Q           string
	CategoryID  int64
	CompanyID   int64
	BrandID     int64
	AttrFilters map[string][]string
	PriceMin    float64
	PriceMax    float64
	Sort        string
	Page        int
	Limit       int
}

type EANPageListResult struct {
	Items []silentjson.RawMessage `json:"items"` // raw EANPage JSON
	Total int64                   `json:"total"`
	Page  int                     `json:"page"`
	Limit int                     `json:"limit"`
}

// ListWithTurbo returns paginated SCU pages with filters and sorting.
// Uses per-category sort indexes — no union index, no candidates for category filter.
func (s *EANPageSearch) ListWithTurbo(params EANPageListParams) (*EANPageListResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("eanpage search is disabled")
	}

	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	// Determine sort key based on category (per-category sort indexes)
	catID := int64(0)
	if params.CategoryID != 0 {
		catID = params.CategoryID
	}

	sortType := eanpageSortTypePriceAsc
	switch params.Sort {
	case "price", "price_asc":
		sortType = eanpageSortTypePriceAsc
	case "price_desc":
		sortType = eanpageSortTypePriceDesc
	case "created_at":
		sortType = eanpageSortTypeCreatedAtDesc
	}
	sortKey := eanpageSortKey(catID, sortType)

	// Fast path: no additional filters — use sort index directly.
	// Per-category sort index already contains only category docs.
	if params.Q == "" && params.CompanyID == 0 &&
		len(params.AttrFilters) == 0 && params.PriceMin == 0 && params.PriceMax == 0 {
		res, err := s.db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
			Name:       sortKey,
			Candidates: nil,
			Page:       params.Page - 1,
			PageSize:   params.Limit,
			Desc:       false,
		})
		if err != nil {
			return nil, fmt.Errorf("turbo sort page with docs: %w", err)
		}
		items := make([]silentjson.RawMessage, 0, len(res.Docs))
		for _, doc := range res.Docs {
			if doc != nil && len(doc) > 0 {
				items = append(items, silentjson.RawMessage(doc))
			}
		}
		return &EANPageListResult{
			Items: items,
			Total: int64(res.Total),
			Page:  params.Page,
			Limit: params.Limit,
		}, nil
	}

	// Fast path: price range + price sort — use numSort directly.
	// No intersect, no extra sorting — numSort already sorted by price.
	if params.Q == "" && params.CompanyID == 0 &&
		len(params.AttrFilters) == 0 && (params.PriceMin > 0 || params.PriceMax > 0) &&
		sortType == eanpageSortTypePriceAsc {
		minVal := uint64(params.PriceMin * 100)
		maxVal := uint64(params.PriceMax * 100)
		if params.PriceMax == 0 {
			maxVal = ^uint64(0)
		}
		res, err := s.db.TurboGetNumSortRangeWithDocs(makodb.TurboGetNumSortRangeWithDocsParams{
			Name:      eanpageNumSortPriceKey(catID),
			MinValue:  minVal,
			MaxValue:  maxVal,
			Page:      params.Page - 1,
			PageSize:  params.Limit,
			Desc:      false,
			DocPrefix: "eanpage:",
		})
		if err != nil {
			return nil, fmt.Errorf("turbo numSort range with docs: %w", err)
		}
		items := make([]silentjson.RawMessage, 0, len(res.Docs))
		for _, doc := range res.Docs {
			if doc != nil && len(doc) > 0 {
				items = append(items, silentjson.RawMessage(doc))
			}
		}
		return &EANPageListResult{
			Items: items,
			Total: int64(res.Total),
			Page:  params.Page,
			Limit: params.Limit,
		}, nil
	}

	// Fast path: price range + price_desc sort — use numSort directly (desc).
	if params.Q == "" && params.CompanyID == 0 &&
		len(params.AttrFilters) == 0 && (params.PriceMin > 0 || params.PriceMax > 0) &&
		sortType == eanpageSortTypePriceDesc {
		minVal := uint64(params.PriceMin * 100)
		maxVal := uint64(params.PriceMax * 100)
		if params.PriceMax == 0 {
			maxVal = ^uint64(0)
		}
		res, err := s.db.TurboGetNumSortRangeWithDocs(makodb.TurboGetNumSortRangeWithDocsParams{
			Name:      eanpageNumSortPriceKey(catID),
			MinValue:  minVal,
			MaxValue:  maxVal,
			Page:      params.Page - 1,
			PageSize:  params.Limit,
			Desc:      true,
			DocPrefix: "eanpage:",
		})
		if err != nil {
			return nil, fmt.Errorf("turbo numSort range with docs: %w", err)
		}
		items := make([]silentjson.RawMessage, 0, len(res.Docs))
		for _, doc := range res.Docs {
			if doc != nil && len(doc) > 0 {
				items = append(items, silentjson.RawMessage(doc))
			}
		}
		return &EANPageListResult{
			Items: items,
			Total: int64(res.Total),
			Page:  params.Page,
			Limit: params.Limit,
		}, nil
	}

	// Additional filters: build candidates from filter indexes only.
	// Category filter is implicit via per-category sort/numSort indexes.
	var candidatesRaw []byte

	// AND-индексы (vendor, text search)
	var andTokens []string

	if params.CompanyID != 0 {
		andTokens = append(andTokens, eanpageKeyVendor(params.CompanyID))
	}

	if params.Q != "" {
		tokens := tokenizeQueryEANPage(params.Q)
		for _, tok := range tokens {
			andTokens = append(andTokens, eanpageKeyText(tok))
		}
	}

	if len(andTokens) > 0 {
		andRaw, err := s.db.TurboBulkIntersectRaw(andTokens)
		if err != nil {
			return nil, fmt.Errorf("turbo intersect: %w", err)
		}
		if andRaw == nil || len(andRaw) == 0 {
			return &EANPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		candidatesRaw = andRaw
	}

	// OR-атрибуты
	for code, values := range params.AttrFilters {
		if len(values) == 0 {
			continue
		}
		attrTokens := make([]string, 0, len(values))
		for _, v := range values {
			attrTokens = append(attrTokens, eanpageKeyAttr(code, v))
		}
		attrBitmap, err := s.db.TurboBulkUnionSortedRaw(attrTokens)
		if err != nil {
			return &EANPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		if attrBitmap == nil || len(attrBitmap) == 0 {
			return &EANPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		if candidatesRaw == nil {
			candidatesRaw = attrBitmap
		} else {
			candidatesRaw = makodb.TurboBinaryIntersectRaw([][]byte{candidatesRaw, attrBitmap})
		}
		if candidatesRaw == nil || len(candidatesRaw) == 0 {
			return &EANPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
	}

	// Price range filter via per-category numSort index
	if params.PriceMin > 0 || params.PriceMax > 0 {
		minVal := uint64(params.PriceMin * 100)
		maxVal := uint64(params.PriceMax * 100)
		if params.PriceMax == 0 {
			maxVal = ^uint64(0)
		}
		if candidatesRaw != nil && len(candidatesRaw) > 0 {
			// Intersect with existing candidates
			priceRaw, err := s.db.TurboGetNumSortRangeIntersectRaw(
				eanpageNumSortPriceKey(catID),
				minVal,
				maxVal,
				candidatesRaw,
			)
			if err == nil && priceRaw != nil && len(priceRaw) > 0 {
				candidatesRaw = priceRaw
			} else {
				candidatesRaw = nil
			}
		} else {
			// No other candidates: get price range directly from numSort index
			priceRaw, err := s.db.TurboGetNumSortRangeRaw(
				eanpageNumSortPriceKey(catID),
				minVal,
				maxVal,
			)
			if err == nil && priceRaw != nil && len(priceRaw) > 0 {
				candidatesRaw = priceRaw
			}
		}
	}

	// Sort + paginate + load docs using per-category sort index
	var res makodb.TurboSortPageWithDocsResult
	var err error

	isDesc := sortType == eanpageSortTypePriceDesc

	if candidatesRaw == nil || len(candidatesRaw) == 0 {
		res, err = s.db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
			Name:       sortKey,
			Candidates: nil,
			Page:       params.Page - 1,
			PageSize:   params.Limit,
			Desc:       isDesc,
			DocPrefix:  "eanpage:",
		})
	} else {
		res, err = s.db.TurboSortIndexPageRawWithDocsFromDB(
			sortKey,
			candidatesRaw,
			params.Page-1,
			params.Limit,
			isDesc,
			"eanpage:",
		)
	}

	if err != nil {
		return nil, fmt.Errorf("turbo sort page with docs: %w", err)
	}

	items := make([]silentjson.RawMessage, 0, len(res.Docs))
	for _, doc := range res.Docs {
		if doc != nil && len(doc) > 0 {
			items = append(items, silentjson.RawMessage(doc))
		}
	}

	return &EANPageListResult{
		Items: items,
		Total: int64(res.Total),
		Page:  params.Page,
		Limit: params.Limit,
	}, nil
}

// ---------- helpers ----------

func (s *EANPageSearch) getCategoryAncestors(catID int64) ([]int64, error) {
	if catID == 0 {
		return nil, nil
	}
	var ancestors []int64
	current := catID
	for current != 0 {
		ancestors = append(ancestors, current)
		cat, err := s.categoryRepo.Get(current)
		if err != nil || cat == nil || cat.ParentID == nil {
			break
		}
		current = *cat.ParentID
	}
	return ancestors, nil
}

func tokenizeEANPage(sp *model.EANPage) []string {
	return tokenizeQueryEANPage(sp.Title + " " + sp.Description)
}

func tokenizeQueryEANPage(text string) []string {
	text = strings.ToLower(text)
	fields := strings.FieldsFunc(text, func(r rune) bool {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') || r == 'ё'
		isDigit := r >= '0' && r <= '9'
		isApostrophe := r == '\'' || r == '’'
		return !isLetter && !isDigit && !isApostrophe
	})
	var tokens []string
	seen := make(map[string]struct{})
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		tokens = append(tokens, f)
	}
	return tokens
}

// RebuildAllIndexes fully rebuilds all EANPage turbo indexes.
// Strategy:
//  1. Clear all indexable keys (cat, brand, vendor, sort, numSort) upfront.
//  2. Stream all EANPage documents, accumulating indexes in memory.
//  3. Flush accumulated indexes in batches to avoid high memory usage.
//  4. Rebuild sort/numSort indexes at the end.
//
// This avoids per-document deletes (vacuum) and ensures no stale indexes remain.
func (s *EANPageSearch) RebuildAllIndexes() error {
	if !s.enabled {
		return nil
	}

	start := time.Now().Unix()
	fmt.Println("[SCUPAGE] RebuildAllIndexes: starting...")

	// Step 1: Clear all indexable keys
	if err := s.clearAllIndexes(); err != nil {
		return fmt.Errorf("clear indexes: %w", err)
	}

	// Step 2 & 3: Stream all EANPage and accumulate indexes in batches
	const batchSize = 5000
	all, err := s.repo.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	// In-memory index accumulator
	indexes := make(map[string][]string)

	flushBatch := func() {
		for key, docIDs := range indexes {
			if len(docIDs) == 0 {
				continue
			}
			if _, err := s.db.TurboPutBatchIndexString(key, docIDs); err != nil {
				fmt.Printf("WARN: eanpage batch index %s: %v\n", key, err)
			}
		}
		// Reset accumulator
		for k := range indexes {
			delete(indexes, k)
		}
	}

	for i, sp := range all {
		docIDKey := KeyEANPage(sp.ID)

		// Category union index for all ancestors.
		if sp.CategoryID != 0 {
			ancestors, err := s.getCategoryAncestors(sp.CategoryID)
			if err != nil {
				ancestors = []int64{sp.CategoryID}
			}
			for _, cid := range ancestors {
				indexes[eanpageKeyCategoryUnion(cid)] = append(indexes[eanpageKeyCategoryUnion(cid)], docIDKey)
			}
		}

		// Brand index
		if sp.BrandID != 0 {
			indexes[eanpageKeyBrand(sp.BrandID)] = append(indexes[eanpageKeyBrand(sp.BrandID)], docIDKey)
		}

		// Vendor index (min company ID among products with this SCU)
		// For rebuild we approximate: use first product's company if available.
		// In current model EANPage doesn't store companyID directly;
		// vendor index is usually not critical for catalog.
		// If needed, compute from products; for now skip to avoid heavy scan.

		// Attributes index
		for _, kv := range sp.Attributes {
			valStr := kv.Value
			if valStr != "" {
				indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docIDKey)
			}
		}

		// Text index
		for _, tok := range tokenizeEANPage(&sp) {
			indexes[eanpageKeyText(tok)] = append(indexes[eanpageKeyText(tok)], docIDKey)
		}

		if (i+1)%batchSize == 0 {
			flushBatch()
			fmt.Printf("[SCUPAGE] RebuildAllIndexes: processed %d / %d\n", i+1, len(all))
		}
	}

	// Flush remaining
	flushBatch()

	// Step 4: Rebuild sort/numSort indexes
	if err := s.BuildSortIndexes(); err != nil {
		fmt.Printf("WARN: rebuild sort indexes: %v\n", err)
	}

	fmt.Printf("[SCUPAGE] RebuildAllIndexes: done in %v\n", time.Since(time.Unix(start, 0)))
	return nil
}

// clearAllIndexes removes all indexable keys for EANPageSearch.
// This ensures no stale indexes remain after rebuild.
func (s *EANPageSearch) clearAllIndexes() error {
	fmt.Println("[SCUPAGE] clearAllIndexes: clearing sort/numSort indexes...")

	// Clear per-category sort and numSort indexes
	categories, err := s.categoryRepo.ListAll()
	if err != nil {
		fmt.Printf("WARN: list categories: %v\n", err)
		categories = nil
	}

	// Clear global (catID=0) indexes
	for _, sortType := range []string{eanpageSortTypePriceAsc, eanpageSortTypePriceDesc, eanpageSortTypeCreatedAtDesc} {
		if err := s.db.TurboClearIndex(eanpageSortKey(0, sortType)); err != nil {
			fmt.Printf("WARN: clear sort index %s: %v\n", eanpageSortKey(0, sortType), err)
		}
	}
	if _, err := s.db.TurboPutNumSortBatch(eanpageNumSortPriceKey(0), nil); err != nil {
		fmt.Printf("WARN: clear numSort %s: %v\n", eanpageNumSortPriceKey(0), err)
	}

	// Clear per-category indexes
	for _, cat := range categories {
		for _, sortType := range []string{eanpageSortTypePriceAsc, eanpageSortTypePriceDesc, eanpageSortTypeCreatedAtDesc} {
			if err := s.db.TurboClearIndex(eanpageSortKey(cat.ID, sortType)); err != nil {
				fmt.Printf("WARN: clear sort index %s: %v\n", eanpageSortKey(cat.ID, sortType), err)
			}
		}
		if _, err := s.db.TurboPutNumSortBatch(eanpageNumSortPriceKey(cat.ID), nil); err != nil {
			fmt.Printf("WARN: clear numSort %s: %v\n", eanpageNumSortPriceKey(cat.ID), err)
		}
		if err := s.db.TurboClearIndex(eanpageKeyCategoryUnion(cat.ID)); err != nil {
			fmt.Printf("WARN: clear cat union index %d: %v\n", cat.ID, err)
		}
	}

	// Brand/vendor/attr/text indexes: dynamic, overwritten during rebuild.

	fmt.Println("[SCUPAGE] clearAllIndexes: done.")
	return nil
}

// parseIDFromKey extracts int64 ID from a key like "prefix:123".
func parseIDFromKey(key, prefix string) (int64, bool) {
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		return 0, false
	}
	id, err := strconv.ParseInt(key[len(prefix):], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
