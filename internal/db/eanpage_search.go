package db

import (
	"encoding/json"
	"fmt"
	"math/rand"
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
	db                 *makodb.ShardedDB
	repo               *EANPageRepo
	productRepo        *ProductRepo
	categoryRepo       *CategoryRepo
	companyRepo        *CompanyRepo
	deliveryMethodRepo *DeliveryMethodRepo
	enabled            bool

	// Cache for category descendants to avoid repeated expensive lookups.
	// Key: catID, Value: []int64 of descendant IDs (not including catID itself).
	descMu       sync.Mutex
	descCache    map[int64][]int64
	descCacheTTL time.Duration

	// Active transaction (nil if not in transaction)
	txn *makodb.Transaction
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

// SetCompanyDeliveryRepos attaches company and delivery method repositories
// required by RecalculateDeliveryMethods (used inside RebuildAllIndexes).
func (s *EANPageSearch) SetCompanyDeliveryRepos(companyRepo *CompanyRepo, deliveryMethodRepo *DeliveryMethodRepo) {
	s.companyRepo = companyRepo
	s.deliveryMethodRepo = deliveryMethodRepo
}

// SetTransaction sets the active transaction for this search.
func (s *EANPageSearch) SetTransaction(txn *makodb.Transaction) {
	s.txn = txn
}

// ClearTransaction clears the active transaction.
func (s *EANPageSearch) ClearTransaction() {
	s.txn = nil
}

// ---------- key helpers ----------

func eanpageKeyBrand(brandID int64) string { return "eanpage_brand:" + strconv.FormatInt(brandID, 10) }
func eanpageKeyVendor(companyID int64) string {
	return "eanpage_vendor:" + strconv.FormatInt(companyID, 10)
}
func eanpageKeyAttr(code string, value string) string {
	return "eanpage_attr:" + code + ":" + value
}
func eanpageKeyAttrCode(code string) string {
	return "eanpage_attr_code:" + code
}
func eanpageKeyText(token string) string { return "eanpage_text:" + token }

// eanpageKeyCategoryUnion returns the key for a category union index.
// Contains all EAN pages of this category and all descendants.
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

// IndexEANPage indexes a single EAN page into turbo indexes.
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

	// Attributes index + attr_code index
	attrCodesSeen := make(map[string]struct{})
	for _, kv := range sp.Attributes {
		valStr := kv.Value
		if valStr != "" {
			// Skip attribute values longer than 40 runes
			if model.IsAttrValueTooLong(valStr) {
				continue
			}
			indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docID)
			// Track unique attribute codes for this EAN page
			if _, ok := attrCodesSeen[kv.Key]; !ok {
				attrCodesSeen[kv.Key] = struct{}{}
				indexes[eanpageKeyAttrCode(kv.Key)] = append(indexes[eanpageKeyAttrCode(kv.Key)], docID)
			}
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

// IndexEANPageBatchTx is the transactional version of IndexEANPageBatch.
func (s *EANPageSearch) IndexEANPageBatchTx(txn *Transaction, pages []*model.EANPage) error {
	if !s.enabled || len(pages) == 0 {
		return nil
	}

	// Collect all indexes in memory
	indexes := make(map[string][]string)
	attrCatRef := make(map[string]map[int64]map[string]string)
	catCodes := make(map[int64]map[string]struct{})

	for _, sp := range pages {
		docID := KeyEANPage(sp.ID)

		if sp.CategoryID != 0 {
			ancestors, err := s.getCategoryAncestors(sp.CategoryID)
			if err != nil {
				ancestors = []int64{sp.CategoryID}
			}
			for _, cid := range ancestors {
				indexes[eanpageKeyCategoryUnion(cid)] = append(indexes[eanpageKeyCategoryUnion(cid)], docID)
			}
		}

		if sp.BrandID != 0 {
			indexes[eanpageKeyBrand(sp.BrandID)] = append(indexes[eanpageKeyBrand(sp.BrandID)], docID)
		}

		// Track unique attribute codes per EAN page
		attrCodesSeen := make(map[string]struct{})
		for _, kv := range sp.Attributes {
			valStr := kv.Value
			if valStr != "" {
				// Skip attribute values longer than 40 runes
				if model.IsAttrValueTooLong(valStr) {
					continue
				}
				indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docID)
				// Track unique attribute codes for this EAN page
				if _, ok := attrCodesSeen[kv.Key]; !ok {
					attrCodesSeen[kv.Key] = struct{}{}
					indexes[eanpageKeyAttrCode(kv.Key)] = append(indexes[eanpageKeyAttrCode(kv.Key)], docID)
				}
				if sp.CategoryID != 0 {
					if attrCatRef[kv.Key] == nil {
						attrCatRef[kv.Key] = make(map[int64]map[string]string)
					}
					if attrCatRef[kv.Key][sp.CategoryID] == nil {
						attrCatRef[kv.Key][sp.CategoryID] = make(map[string]string)
					}
					attrCatRef[kv.Key][sp.CategoryID][valStr] = valStr
					if catCodes[sp.CategoryID] == nil {
						catCodes[sp.CategoryID] = make(map[string]struct{})
					}
					catCodes[sp.CategoryID][kv.Key] = struct{}{}
				}
			}
		}

		for _, tok := range tokenizeEANPage(sp) {
			indexes[eanpageKeyText(tok)] = append(indexes[eanpageKeyText(tok)], docID)
		}
	}

	// Write all indexes in batch (buffered in transaction)
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := txn.TurboPutBatchIndexString(key, docIDs); err != nil {
			fmt.Printf("WARN: eanpage batch index %s: %v\n", key, err)
		}
	}

	// Write attr values per category indexes for filter UI (buffered in transaction).
	// Merge with existing values (read from the committed state) so a partial batch
	// does not erase values contributed by pages outside the batch.
	for code, catMap := range attrCatRef {
		for catID, values := range catMap {
			key := "attr_values_cat:" + code + ":" + strconv.FormatInt(catID, 10)
			valuesSet := make(map[string]struct{}, len(values))
			if data, _ := s.db.TurboRawRead(key); len(data) > 0 {
				var existing map[string]interface{}
				if json.Unmarshal(data, &existing) == nil {
					for v := range existing {
						valuesSet[v] = struct{}{}
					}
				}
			}
			for val := range values {
				valuesSet[val] = struct{}{}
			}
			if len(valuesSet) > 0 {
				buf, _ := json.Marshal(valuesSet)
				_ = txn.TurboWrite(key, buf)
			}
			// Write labels
			for val := range values {
				labelKey := "attr_label:" + code + ":" + val
				_ = txn.TurboWrite(labelKey, []byte(val))
			}
		}
	}

	// Update attrdef_cat_codes:{catID} with codes used by EAN pages (buffered in transaction)
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
		_ = txn.TurboWrite(key, buf)
	}

	return nil
}

// IndexEANPageBatch indexes many EAN pages using batch turbo writes.
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

		// Attributes index + attr_code index
		attrCodesSeen := make(map[string]struct{})
		for _, kv := range sp.Attributes {
			valStr := kv.Value
			if valStr != "" {
				// Skip attribute values longer than 40 runes
				if model.IsAttrValueTooLong(valStr) {
					continue
				}
				indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docID)
				// Track unique attribute codes for this EAN page
				if _, ok := attrCodesSeen[kv.Key]; !ok {
					attrCodesSeen[kv.Key] = struct{}{}
					indexes[eanpageKeyAttrCode(kv.Key)] = append(indexes[eanpageKeyAttrCode(kv.Key)], docID)
				}
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

	// Write attr values per category indexes for filter UI.
	// Merge with existing values (read-modify-write) so a partial batch does not
	// erase values contributed by pages outside the batch — e.g. delivery_method
	// values written by RecalculateDeliveryMethods, or standard attribute values
	// from earlier imports.
	for code, catMap := range attrCatRef {
		for catID, values := range catMap {
			key := "attr_values_cat:" + code + ":" + strconv.FormatInt(catID, 10)
			valuesSet := make(map[string]struct{}, len(values))
			// Read existing values and merge (supplement, not overwrite).
			if data, _ := s.db.TurboRawRead(key); len(data) > 0 {
				var existing map[string]interface{}
				if json.Unmarshal(data, &existing) == nil {
					for v := range existing {
						valuesSet[v] = struct{}{}
					}
				}
			}
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

	// Update attrdef_cat_codes:{catID} with codes used by EAN pages
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

// UnindexEANPage removes a EAN page from all turbo indexes.
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

	// Delete attr_code index (unique codes only)
	attrCodesSeen := make(map[string]struct{})
	for _, kv := range sp.Attributes {
		valStr := kv.Value
		if valStr != "" {
			// Skip attribute values longer than 40 runes (consistent with indexing)
			if model.IsAttrValueTooLong(valStr) {
				continue
			}
			s.db.TurboDeleteIndexString(eanpageKeyAttr(kv.Key, valStr), docID)
			// Delete from attr_code index (once per code)
			if _, ok := attrCodesSeen[kv.Key]; !ok {
				attrCodesSeen[kv.Key] = struct{}{}
				s.db.TurboDeleteIndexString(eanpageKeyAttrCode(kv.Key), docID)
			}
		}
	}

	for _, tok := range tokenizeEANPage(sp) {
		s.db.TurboDeleteIndexString(eanpageKeyText(tok), docID)
	}

	return nil
}

// UnindexEANPage removes a EAN page from all turbo indexes.
func (s *EANPageSearch) DeleteIndexEANPage(sp *model.EANPage) error {
	if !s.enabled {
		return nil
	}

	if sp.CategoryID != 0 {
		ancestors, err := s.getCategoryAncestors(sp.CategoryID)
		if err != nil {
			ancestors = []int64{sp.CategoryID}
		}
		for _, cid := range ancestors {
			s.db.TurboRawDelete(eanpageKeyCategoryUnion(cid))
		}
	}

	if sp.BrandID != 0 {
		s.db.TurboRawDelete(eanpageKeyBrand(sp.BrandID))
	}

	// Delete attr_code index (unique codes only)
	attrCodesSeen := make(map[string]struct{})
	for _, kv := range sp.Attributes {
		valStr := kv.Value
		if valStr != "" {
			// Skip attribute values longer than 40 runes (consistent with indexing)
			if model.IsAttrValueTooLong(valStr) {
				continue
			}
			s.db.TurboRawDelete(eanpageKeyAttr(kv.Key, valStr))
			// Delete from attr_code index (once per code)
			if _, ok := attrCodesSeen[kv.Key]; !ok {
				attrCodesSeen[kv.Key] = struct{}{}
				s.db.TurboRawDelete(eanpageKeyAttrCode(kv.Key))
			}
		}
	}

	for _, tok := range tokenizeEANPage(sp) {
		s.db.TurboRawDelete(eanpageKeyText(tok))
	}

	return nil
}

// BuildSortIndexes rebuilds all sort indexes for EAN pages per category.
// Each category has its own sort indexes: eanpage_sort:{catID}:{type}
// and numSort price index: eanpage_price:{catID}
func (s *EANPageSearch) BuildSortIndexes() error {
	if !s.enabled {
		return nil
	}

	start := time.Now().Unix()
	fmt.Println("[EANPAGE] Building sort indexes per category...")

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
		// Skip EAN pages with no offers: keep them in text/search indexes
		// (findable via search) but exclude from catalog sort indexes.
		if sp.ProductCount == 0 {
			continue
		}

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

	fmt.Printf("[EANPAGE] Sort indexes built: %d pages, %d categories, %v\n", len(all), len(catPricesAsc), time.Since(time.Unix(start, 0)))
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

// ListWithTurbo returns paginated EAN pages with filters and sorting.
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

// RandomByCategory returns up to `limit` random EAN pages of a category
// (including its whole subtree — per-category sort indexes are built for
// every ancestor) plus the total number of pages in the category (for the
// UI badge). Cheap randomness: read a random window from the existing
// eanpage_sort:{catID}:price_asc index instead of a dedicated random index.
func (s *EANPageSearch) RandomByCategory(catID int64, limit int) ([]silentjson.RawMessage, int, error) {
	if !s.enabled {
		return nil, 0, fmt.Errorf("eanpage search is disabled")
	}
	if limit < 1 {
		limit = 12
	}
	if limit > 50 {
		limit = 50
	}

	// First probe: page size 1 just to learn the total.
	probe, err := s.db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
		Name: eanpageSortKey(catID, eanpageSortTypePriceAsc),
		Page: 0,
		// Minimal read: docs are fetched again in the random window below.
		PageSize: 1,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("turbo sort probe: %w", err)
	}
	total := int(probe.Total)
	if total == 0 {
		return nil, 0, nil
	}

	// Random window: a page of `limit` docs starting at page*limit. Any page
	// whose start is < total is valid; the turbo layer clips the final
	// partial window to the end of the index, so every doc can appear.
	pages := (total + limit - 1) / limit
	res, err := s.db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
		Name:     eanpageSortKey(catID, eanpageSortTypePriceAsc),
		Page:     rand.Intn(pages),
		PageSize: limit,
	})
	if err != nil {
		return nil, total, fmt.Errorf("turbo sort page with docs: %w", err)
	}

	items := make([]silentjson.RawMessage, 0, len(res.Docs))
	for _, doc := range res.Docs {
		if doc != nil && len(doc) > 0 {
			items = append(items, silentjson.RawMessage(doc))
		}
	}
	// Shuffle so items within the window are not price-ordered.
	rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	return items, total, nil
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
	return tokenizeQueryEANPage(sp.EAN + " " + sp.Title + " " + sp.Description)
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
	fmt.Println("[EANPAGE] RebuildAllIndexes: starting...")

	// Step 0: Rebuild category indexes first so ancestors/tree paths are populated
	if s.categoryRepo != nil {
		fmt.Println("[EANPAGE] RebuildAllIndexes: rebuilding category indexes...")
		if err := s.categoryRepo.RebuildAllIndexes(); err != nil {
			fmt.Printf("[EANPAGE] RebuildAllIndexes: WARN: rebuild category indexes: %v\n", err)
		}
	}

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

		// Vendor index (min company ID among products with this EAN)
		// For rebuild we approximate: use first product's company if available.
		// In current model EANPage doesn't store companyID directly;
		// vendor index is usually not critical for catalog.
		// If needed, compute from products; for now skip to avoid heavy scan.

		// Attributes index
		for _, kv := range sp.Attributes {
			valStr := kv.Value
			if valStr != "" {
				// Skip attribute values longer than 40 runes
				if model.IsAttrValueTooLong(valStr) {
					continue
				}
				indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docIDKey)
			}
		}

		// Text index
		for _, tok := range tokenizeEANPage(&sp) {
			indexes[eanpageKeyText(tok)] = append(indexes[eanpageKeyText(tok)], docIDKey)
		}

		if (i+1)%batchSize == 0 {
			flushBatch()
			fmt.Printf("[EANPAGE] RebuildAllIndexes: processed %d / %d\n", i+1, len(all))
		}
	}

	// Flush remaining
	flushBatch()

	// Step 4: Rebuild sort/numSort indexes
	if err := s.BuildSortIndexes(); err != nil {
		fmt.Printf("WARN: rebuild sort indexes: %v\n", err)
	}

	// Step 5: Recalculate delivery_method attributes (from products' companies)
	// This must happen after all EAN pages are indexed, so delivery_method
	// indexes (eanpage_attr, attr_values_cat, attrdef_cat_codes) are correct.
	if s.companyRepo != nil && s.deliveryMethodRepo != nil {
		if err := s.RecalculateDeliveryMethods(s.companyRepo, s.deliveryMethodRepo); err != nil {
			fmt.Printf("WARN: RecalculateDeliveryMethods in RebuildAllIndexes: %v\n", err)
		}
	}

	fmt.Printf("[EANPAGE] RebuildAllIndexes: done in %v\n", time.Since(time.Unix(start, 0)))
	return nil
}

// clearAllIndexes removes all indexable keys for EANPageSearch.
// This ensures no stale indexes remain after rebuild.
func (s *EANPageSearch) clearAllIndexes() error {
	fmt.Println("[EANPAGE] clearAllIndexes: clearing sort/numSort indexes...")

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

	fmt.Println("[EANPAGE] clearAllIndexes: done.")
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

// BuildAttrCodeIndexes rebuilds the eanpage_attr_code:{code} indexes from all EAN pages.
// This is needed after adding the index to existing data.
func (s *EANPageSearch) BuildAttrCodeIndexes() error {
	if !s.enabled {
		return nil
	}

	start := time.Now().Unix()
	fmt.Println("[EANPAGE] Building attr_code indexes...")

	all, err := s.repo.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	// Collect all indexes in memory
	indexes := make(map[string][]string)

	for _, sp := range all {
		docID := KeyEANPage(sp.ID)
		// Track unique attribute codes per EAN page
		attrCodesSeen := make(map[string]struct{})
		for _, kv := range sp.Attributes {
			if kv.Value != "" {
				// Skip attribute values longer than 40 runes
				if model.IsAttrValueTooLong(kv.Value) {
					continue
				}
				if _, ok := attrCodesSeen[kv.Key]; !ok {
					attrCodesSeen[kv.Key] = struct{}{}
					indexes[eanpageKeyAttrCode(kv.Key)] = append(indexes[eanpageKeyAttrCode(kv.Key)], docID)
				}
			}
		}
	}

	// Write all indexes in batch
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := s.db.TurboPutBatchIndexString(key, docIDs); err != nil {
			fmt.Printf("WARN: attr_code index %s: %v\n", key, err)
		}
	}

	elapsed := time.Since(time.Unix(start, 0))
	fmt.Printf("[EANPAGE] attr_code indexes built in %v: %d indexes, %d EAN pages scanned\n", elapsed, len(indexes), len(all))
	return nil
}

// attrCodeDeliveryMethod is the EANPage attribute key that stores the deduplicated
// set of delivery method slugs available for the products within each EAN page.
const attrCodeDeliveryMethod = "delivery_method"

// RecalculateDeliveryMethods computes the delivery_method attribute for all EAN
// pages from their products' companies and rebuilds its turbo indexes.
//
// For each EAN page it resolves: products (by EAN) -> their companies ->
// Company.DeliveryMethodIds -> deduplicated DeliveryMethod.Slug values, stores them
// as a multi-value attribute on the page, then writes the full delivery_method
// indexes in batch. The index content is built in memory as key -> docIDs and
// written under the same keys (eanpage_attr:delivery_method:{slug} and
// eanpage_attr_code:delivery_method), so makodb keeps it consistent.
//
// It also maintains the per-category filter indexes so the attribute shows up in
// category filters: attr_values_cat:delivery_method:{catID} (value set) and
// attrdef_cat_codes:{catID} (code registration for the category).
func (s *EANPageSearch) RecalculateDeliveryMethods(companyRepo *CompanyRepo, dmRepo *DeliveryMethodRepo) error {
	if !s.enabled || companyRepo == nil || dmRepo == nil {
		return nil
	}

	start := time.Now().Unix()
	fmt.Println("[EANPAGE] Recalculating delivery_method attributes...")

	// companies: id -> delivery method ids
	companies, err := companyRepo.List()
	if err != nil {
		return fmt.Errorf("list companies: %w", err)
	}
	companyDeliveryIDs := make(map[int64][]int64, len(companies))
	for _, c := range companies {
		if len(c.DeliveryMethodIds) > 0 {
			companyDeliveryIDs[c.ID] = c.DeliveryMethodIds
		}
	}

	// delivery methods: id -> slug
	dms, err := dmRepo.List()
	if err != nil {
		return fmt.Errorf("list delivery methods: %w", err)
	}
	dmSlugByID := make(map[int64]string, len(dms))
	for _, dm := range dms {
		if dm.Slug != "" {
			dmSlugByID[dm.ID] = dm.Slug
		}
	}

	all, err := s.repo.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	// Full index content built in memory: key -> docIDs.
	indexes := make(map[string][]string)
	// Category filter indexes: catID -> set of delivery method slugs.
	catSlugs := make(map[int64]map[string]struct{})

	updated := 0
	for i := range all {
		sp := &all[i]
		if sp.EAN == "" {
			continue
		}

		// Products with this EAN via turbo index.
		tokens, err := s.db.TurboGetIndexTokens("ean:" + sp.EAN)
		if err != nil || len(tokens) == 0 {
			continue
		}
		docs, err := s.db.MultiGetByDocIDs(tokens)
		if err != nil || len(docs) == 0 {
			continue
		}

		// Deduplicated delivery method slugs across all products' companies.
		slugsSet := make(map[string]struct{})
		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			p, err := UnmarshalProduct(doc)
			if err != nil {
				continue
			}
			for _, dmID := range companyDeliveryIDs[p.CompanyID] {
				if slug, ok := dmSlugByID[dmID]; ok && slug != "" {
					slugsSet[slug] = struct{}{}
				}
			}
		}

		docID := KeyEANPage(sp.ID)

		// Rewrite the delivery_method attribute only when it changed.
		if !sameStringSet(deliveryMethodSlugsOf(sp.Attributes), slugsSet) {
			sp.Attributes = setDeliveryMethodAttr(sp.Attributes, slugsSet)
			sp.UpdatedAt = time.Now().Unix()
			data := MarshalEANPage(*sp)
			if err := s.repo.Store.DocPut(docID, data); err != nil {
				fmt.Printf("WARN: update delivery_method for eanpage %d: %v\n", sp.ID, err)
				continue
			}
			updated++
		}

		// Accumulate index entries (full content per key).
		if len(slugsSet) > 0 {
			codeKey := eanpageKeyAttrCode(attrCodeDeliveryMethod)
			indexes[codeKey] = append(indexes[codeKey], docID)
			for slug := range slugsSet {
				key := eanpageKeyAttr(attrCodeDeliveryMethod, slug)
				indexes[key] = append(indexes[key], docID)
			}

			// Track values per own category for the filter UI.
			if sp.CategoryID != 0 {
				if catSlugs[sp.CategoryID] == nil {
					catSlugs[sp.CategoryID] = make(map[string]struct{})
				}
				for slug := range slugsSet {
					catSlugs[sp.CategoryID][slug] = struct{}{}
				}
			}
		}

		if (i+1)%10000 == 0 {
			fmt.Printf("[EANPAGE] RecalculateDeliveryMethods: processed %d / %d (updated %d)\n", i+1, len(all), updated)
		}
	}

	// Write all indexes in batch (one write per key with full content).
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := s.db.TurboPutBatchIndexString(key, docIDs); err != nil {
			fmt.Printf("WARN: delivery_method index %s: %v\n", key, err)
		}
	}

	// Update per-category filter indexes (same keys IndexEANPageBatch writes for
	// other attributes), so delivery_method appears in category filters.
	for catID, slugs := range catSlugs {
		if len(slugs) == 0 {
			continue
		}

		// Merge the value set into attr_values_cat:delivery_method:{catID}.
		valuesKey := turboKeyAttrValuesCat + attrCodeDeliveryMethod + ":" + strconv.FormatInt(catID, 10)
		existing, _ := s.db.TurboRawRead(valuesKey)
		valuesSet := make(map[string]struct{}, len(slugs))
		if len(existing) > 0 {
			var m map[string]interface{}
			if json.Unmarshal(existing, &m) == nil {
				for v := range m {
					valuesSet[v] = struct{}{}
				}
			}
		}
		for slug := range slugs {
			valuesSet[slug] = struct{}{}
		}
		buf, err := json.Marshal(valuesSet)
		if err != nil {
			fmt.Printf("WARN: delivery_method attr_values_cat %d: %v\n", catID, err)
			continue
		}
		if err := s.db.TurboRawWrite(valuesKey, buf); err != nil {
			fmt.Printf("WARN: delivery_method attr_values_cat %d: %v\n", catID, err)
			continue
		}

		// Register the code for this category (attrdef_cat_codes:{catID}).
		codesKey := turboKeyAttrDefCatCodes + strconv.FormatInt(catID, 10)
		data, _ := s.db.TurboRawRead(codesKey)
		var codes []string
		if len(data) > 0 {
			json.Unmarshal(data, &codes)
		}
		found := false
		for _, c := range codes {
			if c == attrCodeDeliveryMethod {
				found = true
				break
			}
		}
		if !found {
			codes = append(codes, attrCodeDeliveryMethod)
			buf, _ := json.Marshal(codes)
			if err := s.db.TurboRawWrite(codesKey, buf); err != nil {
				fmt.Printf("WARN: delivery_method attrdef_cat_codes %d: %v\n", catID, err)
			}
		}
	}

	elapsed := time.Since(time.Unix(start, 0))
	fmt.Printf("[EANPAGE] RecalculateDeliveryMethods: done in %v. Updated %d pages, %d indexes.\n", elapsed, updated, len(indexes))
	return nil
}

// deliveryMethodSlugsOf returns the set of delivery_method values stored on a page.
func deliveryMethodSlugsOf(attrs []model.KeyValue) map[string]struct{} {
	set := make(map[string]struct{})
	for _, kv := range attrs {
		if kv.Key == attrCodeDeliveryMethod && kv.Value != "" {
			set[kv.Value] = struct{}{}
		}
	}
	return set
}

// setDeliveryMethodAttr returns attrs with the delivery_method entries replaced by slugs.
func setDeliveryMethodAttr(attrs []model.KeyValue, slugs map[string]struct{}) []model.KeyValue {
	result := make([]model.KeyValue, 0, len(attrs)+len(slugs))
	for _, kv := range attrs {
		if kv.Key == attrCodeDeliveryMethod {
			continue
		}
		result = append(result, kv)
	}
	sorted := make([]string, 0, len(slugs))
	for slug := range slugs {
		sorted = append(sorted, slug)
	}
	sort.Strings(sorted)
	for _, slug := range sorted {
		result = append(result, model.KeyValue{Key: attrCodeDeliveryMethod, Value: slug})
	}
	return result
}

// sameStringSet reports whether two string sets are equal.
func sameStringSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
