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

// TurboProductSearch — строго по turbo_index_guide.md.
type TurboProductSearch struct {
	store        *Store
	repo         *ProductRepo
	categoryRepo *CategoryRepo
	landingRepo  *LandingRepo
	eanPageRepo  *EANPageRepo
	mu           sync.RWMutex
	enabled      bool

	// Active transaction (nil if not in transaction)
	txn *makodb.Transaction
}

func NewTurboProductSearch(store *Store, repo *ProductRepo, categoryRepo *CategoryRepo, enabled bool) *TurboProductSearch {
	return &TurboProductSearch{
		store:        store,
		repo:         repo,
		categoryRepo: categoryRepo,
		enabled:      enabled,
	}
}

// SetLandingRepo attaches a LandingRepo for EAN/landing page management.
func (t *TurboProductSearch) SetLandingRepo(lr *LandingRepo) {
	t.landingRepo = lr
}

// SetTransaction sets the active transaction for this search.
func (t *TurboProductSearch) SetTransaction(txn *makodb.Transaction) {
	t.txn = txn
}

// ClearTransaction clears the active transaction.
func (t *TurboProductSearch) ClearTransaction() {
	t.txn = nil
}

// SetEANPageRepo attaches a EANPageRepo for SEO page management.
func (t *TurboProductSearch) SetEANPageRepo(sr *EANPageRepo) {
	t.eanPageRepo = sr
}

// DB returns the underlying ShardedDB for direct turbo operations (reads only).
func (t *TurboProductSearch) DB() *makodb.ShardedDB {
	return t.store.db
}

// ---------- key helpers (по доке) ----------

func turboKeyBrand(brandID int64) string    { return "brand:" + strconv.FormatInt(brandID, 10) }
func turboKeyVendor(companyID int64) string { return "vendor:" + strconv.FormatInt(companyID, 10) }
func turboKeyCategory(catID int64) string   { return "cat:" + strconv.FormatInt(catID, 10) }
func turboKeyAttr(code string, value string) string {
	return "attr:" + code + ":" + value
}
func turboKeyAttrLabel(code string, value string) string {
	return "attr_label:" + code + ":" + value
}
func turboKeyText(token string) string { return "text:" + token }

// Справочники
var turboKeyBrandList = "brand_list:"
var turboKeyBrandNamePrefix = "brand_name:"

const (
	turboSortPriceAsc      = "sort:price_asc"
	turboSortPriceDesc     = "sort:price_desc"
	turboSortCreatedAtDesc = "sort:created_at_desc"
	turboKeyPrice          = "price:"   // price:<productID> -> float64 bytes
	turboKeyCreatedAt      = "created:" // created:<productID> -> int64 bytes
)

// ---------- indexing ----------

// IndexProduct — для одиночных изменений.
func (t *TurboProductSearch) IndexProduct(p *model.Product) error {
	if !t.enabled {
		return nil
	}
	docID := KeyProduct(p.ID)

	// product_list index
	if _, err := t.store.db.TurboPutIndexString(TurboKeyProductList, docID); err != nil {
		return fmt.Errorf("turbo product_list index: %w", err)
	}

	if p.BrandID != 0 {
		if _, err := t.store.db.TurboPutIndexString(turboKeyBrand(p.BrandID), docID); err != nil {
			return fmt.Errorf("turbo brand index: %w", err)
		}
		// NOTE: ensureBrandInRef убран — делается в IndexProductBatch (один write для всех)
	}

	if p.CompanyID != 0 {
		if _, err := t.store.db.TurboPutIndexString(turboKeyVendor(p.CompanyID), docID); err != nil {
			return fmt.Errorf("turbo vendor index: %w", err)
		}
	}

	if p.CategoryID != 0 {
		ancestors, err := t.GetCategoryAncestors(p.CategoryID)
		if err != nil {
			ancestors = []int64{p.CategoryID}
		}
		for _, cid := range ancestors {
			if _, err := t.store.db.TurboPutIndexString(turboKeyCategory(cid), docID); err != nil {
				return fmt.Errorf("turbo category index: %w", err)
			}
		}
	}

	for _, kv := range p.Attributes {
		valStr := kv.Value
		if valStr != "" {
			// Skip attribute values longer than 40 runes
			if model.IsAttrValueTooLong(valStr) {
				continue
			}
			if _, err := t.store.db.TurboPutIndexString(turboKeyAttr(kv.Key, valStr), docID); err != nil {
				return fmt.Errorf("turbo attr index: %w", err)
			}
			// NOTE: ensureAttrValueInRef / ensureAttrValueInCatRef убраны —
			// делаются в IndexProductBatch (batch write)
		}
	}

	for _, tok := range tokenizeProduct(p) {
		if _, err := t.store.db.TurboPutIndexString(turboKeyText(tok), docID); err != nil {
			return fmt.Errorf("turbo text index: %w", err)
		}
	}

	// Диапазоны цен (по доке, вариант 1)
	t.indexPriceRanges(p.Price, docID)

	// EAN index: links product to landing page.
	// For products without EAN, use the name-based key so they attach to "nm:" pages.
	if eanKey := ProductEANIndexKey(p); eanKey != "" {
		if _, err := t.store.db.TurboPutIndexString("ean:"+eanKey, docID); err != nil {
			return fmt.Errorf("turbo ean index: %w", err)
		}
	}

	// Landing page + EANPage linking only for real EANs.
	if p.EAN != "" {
		// Update landing page product list
		if t.landingRepo != nil {
			_, _ = t.landingRepo.UpsertByEAN(p.EAN, func(lp *model.LandingPage) {
				if lp.Title == p.EAN {
					lp.Title = p.Name
				}
				if lp.Description == "" {
					lp.Description = p.Description
				}
			})
			if lp, err := t.landingRepo.GetByEAN(p.EAN); err == nil {
				_ = t.landingRepo.AddProduct(lp.ID, p.ID)
			}
		}
		// Link to EANPage (SEO page)
		if t.eanPageRepo != nil {
			_ = t.eanPageRepo.LinkProductByEAN(p.EAN, p)
		}
	}

	return nil
}

// BatchIndexProducts indexes multiple products in batch for better performance.
// This reduces space bloat by batching index writes instead of doing them one by one.
func (t *TurboProductSearch) BatchIndexProducts(products []*model.Product) error {
	if !t.enabled || len(products) == 0 {
		return nil
	}

	fmt.Printf("[TURBO] Batch indexing %d products...\n", len(products))
	start := time.Now()

	// Collect all EAN keys and their docIDs
	eanIndex := make(map[string][]string) // eanKey -> []docID

	for _, p := range products {
		docID := KeyProduct(p.ID)

		// Add to product_list index
		if _, err := t.store.db.TurboPutIndexString(TurboKeyProductList, docID); err != nil {
			fmt.Printf("WARN: turbo index product_list: %v\n", err)
		}

		// Collect EAN index entries (name-based key for products without EAN)
		if eanKey := ProductEANIndexKey(p); eanKey != "" {
			indexKey := "ean:" + eanKey
			eanIndex[indexKey] = append(eanIndex[indexKey], docID)
		}

		// Index price ranges
		t.indexPriceRanges(p.Price, docID)
	}

	// Build map from EAN to products for efficient lookup
	eanToProducts := make(map[string][]*model.Product)
	for _, p := range products {
		if p.EAN != "" {
			eanToProducts[p.EAN] = append(eanToProducts[p.EAN], p)
		}
	}

	// Batch write all EAN indexes
	for eanKey, docIDs := range eanIndex {
		if _, err := t.store.db.TurboPutBatchIndexString(eanKey, docIDs); err != nil {
			fmt.Printf("WARN: turbo batch ean index %s: %v\n", eanKey, err)
		}
	}

	// Create landing pages in batch (only once per unique EAN)
	if t.landingRepo != nil {
		var eans []string
		for ean := range eanToProducts {
			eans = append(eans, ean)
		}

		t.landingRepo.BatchUpsertByEANs(eans, func(ean string) (string, string) {
			if prods, ok := eanToProducts[ean]; ok && len(prods) > 0 {
				return prods[0].Name, prods[0].Description
			}
			return ean, ""
		})
	}

	// Link products to landing pages and EAN pages (batch)
	if t.landingRepo != nil {
		landingToProducts := make(map[int64][]int64)

		for ean, prods := range eanToProducts {
			if lp, err := t.landingRepo.GetByEAN(ean); err == nil {
				for _, p := range prods {
					landingToProducts[lp.ID] = append(landingToProducts[lp.ID], p.ID)
				}
			}
		}

		_ = t.landingRepo.BatchAddProducts(landingToProducts)
	}

	if t.eanPageRepo != nil {
		_ = t.eanPageRepo.BatchLinkProductsByEAN(eanToProducts)
	}

	fmt.Printf("[TURBO] Batch indexed %d products in %v\n", len(products), time.Since(start))
	return nil
}

// BatchIndexProductstx indexes multiple products in batch (transactional version).
// All writes are buffered in the transaction and applied atomically on Commit.
// Uses batch operations to minimize vacuum by writing each index only once.
func (t *TurboProductSearch) BatchIndexProductstx(txn *Transaction, products []*model.Product) error {
	if !t.enabled || len(products) == 0 {
		return nil
	}

	fmt.Printf("[TURBO] Batch indexing %d products (transactional)...\n", len(products))
	start := time.Now()

	// Collect all index entries in memory to minimize writes
	// This prevents vacuum by writing each index only once with all new docIDs
	indexes := make(map[string][]string) // indexKey -> []docID

	// Collect all EAN keys and their docIDs
	eanIndex := make(map[string][]string) // eanKey -> []docID
	// Collect vendor (company) index entries for cleanup support
	vendorIndex := make(map[string][]string) // vendorKey -> []docID

	// Collect attribute values per category for filter indexes
	attrCatRef := make(map[string]map[int64]map[string]struct{}) // code -> {catID -> {value}}

	for _, p := range products {
		docID := KeyProduct(p.ID)

		// Add to product_list index (in transaction)
		if _, err := txn.TurboPutIndexString(TurboKeyProductList, docID); err != nil {
			fmt.Printf("WARN: turbo index product_list: %v\n", err)
		}

		// Collect brand index entries
		if p.BrandID != 0 {
			brandKey := turboKeyBrand(p.BrandID)
			indexes[brandKey] = append(indexes[brandKey], docID)
		}

		// Collect category index entries (with ancestors)
		if p.CategoryID != 0 {
			ancestors, err := t.GetCategoryAncestors(p.CategoryID)
			if err != nil {
				ancestors = []int64{p.CategoryID}
			}
			for _, cid := range ancestors {
				catKey := turboKeyCategory(cid)
				indexes[catKey] = append(indexes[catKey], docID)
			}
		}

		// Collect attribute index entries
		for _, kv := range p.Attributes {
			valStr := kv.Value
			if valStr != "" {
				// Skip attribute values longer than 40 runes
				if model.IsAttrValueTooLong(valStr) {
					continue
				}
				attrKey := turboKeyAttr(kv.Key, valStr)
				indexes[attrKey] = append(indexes[attrKey], docID)

				// Collect for category filter indexes
				if p.CategoryID != 0 {
					if attrCatRef[kv.Key] == nil {
						attrCatRef[kv.Key] = make(map[int64]map[string]struct{})
					}
					if attrCatRef[kv.Key][p.CategoryID] == nil {
						attrCatRef[kv.Key][p.CategoryID] = make(map[string]struct{})
					}
					attrCatRef[kv.Key][p.CategoryID][valStr] = struct{}{}
				}
			}
		}

		// Collect text index entries
		for _, tok := range tokenizeProduct(p) {
			textKey := turboKeyText(tok)
			indexes[textKey] = append(indexes[textKey], docID)
		}

		// Collect EAN index entries (name-based key for products without EAN)
		if eanKey := ProductEANIndexKey(p); eanKey != "" {
			indexKey := "ean:" + eanKey
			eanIndex[indexKey] = append(eanIndex[indexKey], docID)
		}

		// Collect vendor (company) index entries
		if p.CompanyID != 0 {
			vendorKey := turboKeyVendor(p.CompanyID)
			vendorIndex[vendorKey] = append(vendorIndex[vendorKey], docID)
		}

		// Index price ranges (in transaction)
		t.indexPriceRangesTx(txn, p.Price, docID)
	}

	// no vacuum ^
	// Build map from EAN to products for efficient lookup
	eanToProducts := make(map[string][]*model.Product)
	for _, p := range products {
		if p.EAN != "" {
			eanToProducts[p.EAN] = append(eanToProducts[p.EAN], p)
		}
	}

	// Batch write all indexes (in transaction) - this minimizes vacuum
	for key, docIDs := range indexes {
		if len(docIDs) > 0 {
			if _, err := txn.TurboPutBatchIndexString(key, docIDs); err != nil {
				fmt.Printf("WARN: turbo batch index %s: %v\n", key, err)
			}
		}
	}

	// Batch write all EAN indexes (in transaction)
	for eanKey, docIDs := range eanIndex {
		if _, err := txn.TurboPutBatchIndexString(eanKey, docIDs); err != nil {
			fmt.Printf("WARN: turbo batch ean index %s: %v\n", eanKey, err)
		}
	}

	// Batch write all vendor (company) indexes (in transaction)
	for vendorKey, docIDs := range vendorIndex {
		if _, err := txn.TurboPutBatchIndexString(vendorKey, docIDs); err != nil {
			fmt.Printf("WARN: turbo batch vendor index %s: %v\n", vendorKey, err)
		}
	}

	// Write attribute value indexes per category (for filter UI)
	for code, catMap := range attrCatRef {
		for catID, values := range catMap {
			key := "attr_values_cat:" + code + ":" + strconv.FormatInt(catID, 10)
			// Read existing values to merge (reads are not part of transaction)
			existingData, _ := t.store.db.TurboRawRead(key)
			var existing map[string]bool
			if len(existingData) > 0 {
				json.Unmarshal(existingData, &existing)
			}
			if existing == nil {
				existing = make(map[string]bool)
			}
			// Merge new values
			for val := range values {
				existing[val] = true
			}
			// Write back (buffered in transaction)
			buf, _ := json.Marshal(existing)
			if err := txn.TurboWrite(key, buf); err != nil {
				fmt.Printf("WARN: turbo batch attr_values_cat %s: %v\n", key, err)
			}
		}
	}

	// Create landing pages in batch (only once per unique EAN)
	if t.landingRepo != nil {
		var eans []string
		for ean := range eanToProducts {
			eans = append(eans, ean)
		}

		t.landingRepo.BatchUpsertByEANs(eans, func(ean string) (string, string) {
			if prods, ok := eanToProducts[ean]; ok && len(prods) > 0 {
				return prods[0].Name, prods[0].Description
			}
			return ean, ""
		})
	}

	// Link products to landing pages and EAN pages (batch)
	if t.landingRepo != nil {
		landingToProducts := make(map[int64][]int64)

		for ean, prods := range eanToProducts {
			if lp, err := t.landingRepo.GetByEAN(ean); err == nil {
				for _, p := range prods {
					landingToProducts[lp.ID] = append(landingToProducts[lp.ID], p.ID)
				}
			}
		}

		_ = t.landingRepo.BatchAddProducts(landingToProducts)
	}

	if t.eanPageRepo != nil {
		_ = t.eanPageRepo.BatchLinkProductsByEAN(eanToProducts)
	}

	fmt.Printf("[TURBO] Batch indexed %d products (transactional) in %v\n", len(products), time.Since(start))
	return nil
}

// indexPriceRangesTx добавляет docID в индексы диапазонов цен (transactional version).
func (t *TurboProductSearch) indexPriceRangesTx(txn *Transaction, price float64, docID string) {
	ranges := priceRanges()
	for _, r := range ranges {
		if price >= r.min && price < r.max {
			txn.TurboPutIndexString(r.key, docID)
		}
	}
}

// indexPriceRanges добавляет docID в индексы диапазонов цен.
// Фиксированные диапазоны: 0-5k, 5k-10k, 10k-20k, 20k-50k, 50k-100k, 100k+
func (t *TurboProductSearch) indexPriceRanges(price float64, docID string) {
	ranges := priceRanges()
	for _, r := range ranges {
		if price >= r.min && price < r.max {
			t.store.db.TurboPutIndexString(r.key, docID)
		}
	}
}

// priceRanges возвращает список фиксированных диапазонов цен.
func priceRanges() []struct {
	min, max float64
	key      string
} {
	return []struct {
		min, max float64
		key      string
	}{
		{0, 5000, "price:0_5000"},
		{5000, 10000, "price:5000_10000"},
		{10000, 20000, "price:10000_20000"},
		{20000, 50000, "price:20000_50000"},
		{50000, 100000, "price:50000_100000"},
		{100000, 1e18, "price:100000_"},
	}
}

// priceRangeKeyForFilter возвращает ключ индекса для фильтрации по диапазону цены.
// Если диапазон не покрывается одним фиксированным индексом, возвращает "".
func (t *TurboProductSearch) priceRangeKeyForFilter(priceMin, priceMax float64) string {
	ranges := priceRanges()
	for _, r := range ranges {
		// Проверяем, полностью ли фиксированный диапазон покрывается запрошенным
		if r.min >= priceMin && r.max <= priceMax {
			return r.key
		}
	}
	// Если не нашли полный охват, используем ближайший подходящий
	// Для простоты: если только priceMin — берём диапазон от него,
	// если только priceMax — берём диапазон до него
	if priceMin > 0 && priceMax == 0 {
		for _, r := range ranges {
			if r.min >= priceMin {
				return r.key
			}
		}
	}
	if priceMin == 0 && priceMax > 0 {
		for _, r := range ranges {
			if r.max <= priceMax {
				return r.key
			}
		}
	}
	return ""
}

// ensureBrandInRef добавляет бренд в справочник, если его там нет.
func (t *TurboProductSearch) ensureBrandInRef(brandID int64, name string) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	// Проверяем, есть ли уже
	brandKey := KeyBrand(brandID)
	if ok, _ := t.store.db.TurboContainsIndexString(turboKeyBrandList, brandKey); ok {
		return
	}
	// Добавляем в список брендов
	_, _ = t.store.db.TurboPutIndexString(turboKeyBrandList, brandKey)

	// Записываем имя бренда
	key := turboKeyBrandNamePrefix + strconv.FormatInt(brandID, 10)
	label := []byte(name)
	t.store.TurboWrite(key, label)
}

// ensureAttrValueInRef записывает label значения атрибута.
// Справочник значений заполняется при batch-импорте через attr_values_cat.
func (t *TurboProductSearch) ensureAttrValueInRef(code, value string) {
	if code == "" || value == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	labelKey := turboKeyAttrLabel(code, value)
	label := []byte(value)
	t.store.TurboWrite(labelKey, label)
}

// ensureAttrValueInCatRef adds a value to turbo_attr_values_cat:{code}:{catID}.
func (t *TurboProductSearch) ensureAttrValueInCatRef(code string, catID int64, value string) {
	if code == "" || value == "" || catID == 0 {
		return
	}
	key := "turbo_attr_values_cat:" + code + ":" + strconv.FormatInt(catID, 10)
	if _, err := t.store.db.TurboPutIndexString(key, value); err != nil {
		fmt.Printf("WARN: turbo attr_values_cat index %s: %v\n", key, err)
	}
	// Also write label for this value
	labelKey := turboKeyAttrLabel(code, value)
	t.store.TurboWrite(labelKey, []byte(value))
}

// UnindexProduct — для удаления/обновления.
func (t *TurboProductSearch) UnindexProduct(p *model.Product) error {
	if !t.enabled {
		return nil
	}
	docID := KeyProduct(p.ID)

	// Remove from product_list
	t.store.db.TurboDeleteIndexString(TurboKeyProductList, docID)

	if p.BrandID != 0 {
		t.store.db.TurboDeleteIndexString(turboKeyBrand(p.BrandID), docID)
	}
	if p.CompanyID != 0 {
		t.store.db.TurboDeleteIndexString(turboKeyVendor(p.CompanyID), docID)
	}
	if p.CategoryID != 0 {
		ancestors, err := t.GetCategoryAncestors(p.CategoryID)
		if err != nil {
			ancestors = []int64{p.CategoryID}
		}
		for _, cid := range ancestors {
			t.store.db.TurboDeleteIndexString(turboKeyCategory(cid), docID)
		}
	}
	for _, kv := range p.Attributes {
		valStr := kv.Value
		if valStr != "" {
			// Skip attribute values longer than 40 runes (consistent with indexing)
			if model.IsAttrValueTooLong(valStr) {
				continue
			}
			t.store.db.TurboDeleteIndexString(turboKeyAttr(kv.Key, valStr), docID)
		}
	}
	for _, tok := range tokenizeProduct(p) {
		t.store.db.TurboDeleteIndexString(turboKeyText(tok), docID)
	}

	// Remove EAN index (name-based key for products without EAN)
	if eanKey := ProductEANIndexKey(p); eanKey != "" {
		t.store.db.TurboDeleteIndexString("ean:"+eanKey, docID)
	}

	if p.EAN != "" {
		// Remove product from landing page
		if t.landingRepo != nil {
			if lp, err := t.landingRepo.GetByEAN(p.EAN); err == nil {
				_ = t.landingRepo.RemoveProduct(lp.ID, p.ID)
			}
		}
		// Remove product from EANPage
		if t.eanPageRepo != nil {
			if sp, err := t.eanPageRepo.GetByEAN(p.EAN); err == nil {
				_ = t.eanPageRepo.RemoveProduct(sp.ID, p.ID)
			}
		}
	}

	return nil
}

// UnindexProductTx — transactional version of UnindexProduct.
// All index removals are buffered in the transaction and applied on Commit.
func (t *TurboProductSearch) UnindexProductTx(txn *Transaction, p *model.Product) error {
	if !t.enabled {
		return nil
	}
	docID := KeyProduct(p.ID)

	// Remove from product_list
	txn.TurboDeleteIndexString(TurboKeyProductList, docID)

	if p.BrandID != 0 {
		txn.TurboDeleteIndexString(turboKeyBrand(p.BrandID), docID)
	}
	if p.CompanyID != 0 {
		txn.TurboDeleteIndexString(turboKeyVendor(p.CompanyID), docID)
	}
	if p.CategoryID != 0 {
		ancestors, err := t.GetCategoryAncestors(p.CategoryID)
		if err != nil {
			ancestors = []int64{p.CategoryID}
		}
		for _, cid := range ancestors {
			txn.TurboDeleteIndexString(turboKeyCategory(cid), docID)
		}
	}
	for _, kv := range p.Attributes {
		valStr := kv.Value
		if valStr != "" {
			// Skip attribute values longer than 40 runes (consistent with indexing)
			if model.IsAttrValueTooLong(valStr) {
				continue
			}
			txn.TurboDeleteIndexString(turboKeyAttr(kv.Key, valStr), docID)
		}
	}
	for _, tok := range tokenizeProduct(p) {
		txn.TurboDeleteIndexString(turboKeyText(tok), docID)
	}

	// Remove EAN index (name-based key for products without EAN)
	if eanKey := ProductEANIndexKey(p); eanKey != "" {
		txn.TurboDeleteIndexString("ean:"+eanKey, docID)
	}

	return nil
}

// IndexProductBatch — для импорта.
func (t *TurboProductSearch) IndexProductBatch(products []*model.Product) error {
	if !t.enabled || len(products) == 0 {
		return nil
	}

	indexes := make(map[string][]string)
	// Справочники: собираем уникальные значения для batch-записи
	brandRef := make(map[uint64]string)                        // brandID -> name
	attrRef := make(map[string]map[string]string)              // code -> {value -> value}
	attrCatRef := make(map[string]map[int64]map[string]string) // code -> {catID -> {value -> value}}

	for _, p := range products {
		docID := KeyProduct(p.ID)

		// product_list index — global index of all product IDs
		indexes[TurboKeyProductList] = append(indexes[TurboKeyProductList], docID)

		if p.BrandID != 0 {
			indexes[turboKeyBrand(p.BrandID)] = append(indexes[turboKeyBrand(p.BrandID)], docID)
			if p.Brand != "" {
				brandRef[uint64(p.BrandID)] = p.Brand
			}
		}
		if p.CompanyID != 0 {
			indexes[turboKeyVendor(p.CompanyID)] = append(indexes[turboKeyVendor(p.CompanyID)], docID)
		}
		if p.CategoryID != 0 {
			ancestors, err := t.GetCategoryAncestors(p.CategoryID)
			if err != nil {
				ancestors = []int64{p.CategoryID}
			}
			for _, cid := range ancestors {
				indexes[turboKeyCategory(cid)] = append(indexes[turboKeyCategory(cid)], docID)
			}
		}
		for _, kv := range p.Attributes {
			valStr := kv.Value
			if valStr != "" {
				// Skip attribute values longer than 40 runes
				if model.IsAttrValueTooLong(valStr) {
					continue
				}
				indexes[turboKeyAttr(kv.Key, valStr)] = append(indexes[turboKeyAttr(kv.Key, valStr)], docID)
				// Справочник значений атрибута (глобальный)
				if attrRef[kv.Key] == nil {
					attrRef[kv.Key] = make(map[string]string)
				}
				attrRef[kv.Key][valStr] = valStr
				// Справочник значений атрибута по категории
				if p.CategoryID != 0 {
					if attrCatRef[kv.Key] == nil {
						attrCatRef[kv.Key] = make(map[int64]map[string]string)
					}
					if attrCatRef[kv.Key][p.CategoryID] == nil {
						attrCatRef[kv.Key][p.CategoryID] = make(map[string]string)
					}
					attrCatRef[kv.Key][p.CategoryID][valStr] = valStr
				}
			}
		}
		for _, tok := range tokenizeProduct(p) {
			indexes[turboKeyText(tok)] = append(indexes[turboKeyText(tok)], docID)
		}

		// EAN index (name-based key for products without EAN)
		if eanKey := ProductEANIndexKey(p); eanKey != "" {
			indexKey := "ean:" + eanKey
			indexes[indexKey] = append(indexes[indexKey], docID)
		}
	}

	// Записываем индексы
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := t.store.db.TurboPutBatchIndexString(key, docIDs); err != nil {
			fmt.Printf("WARN: turbo batch index %s: %v\n", key, err)
		}
	}

	// Записываем price и created батчами
	type priceEntry struct {
		docID string
		price uint64
	}
	type createdEntry struct {
		docID string
		ts    uint64
	}
	var priceEntries []priceEntry
	var createdEntries []createdEntry
	for _, p := range products {
		docID := KeyProduct(p.ID)
		priceEntries = append(priceEntries, priceEntry{docID: docID, price: uint64(p.Price * 100)})
		createdEntries = append(createdEntries, createdEntry{docID: docID, ts: uint64(p.CreatedAt * 1e9)})
	}
	if len(priceEntries) > 0 {
		priceDocIDs := make([]string, len(priceEntries))
		for i, e := range priceEntries {
			priceDocIDs[i] = e.docID
		}
		if _, err := t.store.db.TurboPutBatchIndexString(turboKeyPrice, priceDocIDs); err != nil {
			fmt.Printf("WARN: turbo batch price index: %v\n", err)
		}
		// Also store in numeric sort index
		// Note: for Key128 docID, we use the full key for numSort as well
		pairs := make([]makodb.TurboNumSortPair, len(priceEntries))
		for i, e := range priceEntries {
			pairs[i] = makodb.TurboNumSortPair{Value: e.price, DocID: uint64(e.docID[0])}
		}
		_, _ = t.store.db.TurboPutNumSortBatch("price_num", pairs)
	}
	if len(createdEntries) > 0 {
		createdDocIDs := make([]string, len(createdEntries))
		for i, e := range createdEntries {
			createdDocIDs[i] = e.docID
		}
		if _, err := t.store.db.TurboPutBatchIndexString(turboKeyCreatedAt, createdDocIDs); err != nil {
			fmt.Printf("WARN: turbo batch created index: %v\n", err)
		}
		// Also store in numeric sort index
		pairs := make([]makodb.TurboNumSortPair, len(createdEntries))
		for i, e := range createdEntries {
			// Convert Key128 docID to uint64 for numSort - use first part of the hash
			pairs[i] = makodb.TurboNumSortPair{Value: e.ts, DocID: uint64(e.docID[0])}
		}
		_, _ = t.store.db.TurboPutNumSortBatch("created_num", pairs)
	}

	// Записываем справочник брендов через TurboPutBatchIndex
	t.mu.Lock()
	if len(brandRef) > 0 {
		brandKeys := make([]string, 0, len(brandRef))
		for bid := range brandRef {
			brandKey := KeyBrand(int64(bid))
			// Check if already exists
			if ok, _ := t.store.db.TurboContainsIndexString(turboKeyBrandList, brandKey); !ok {
				brandKeys = append(brandKeys, brandKey)
			}
		}
		if len(brandKeys) > 0 {
			_, _ = t.store.db.TurboPutBatchIndexString(turboKeyBrandList, brandKeys)
		}
	}
	// Записываем имена брендов
	for bid, name := range brandRef {
		key := turboKeyBrandNamePrefix + strconv.FormatUint(bid, 10)
		t.store.TurboWrite(key, []byte(name))
	}

	// Записываем метки атрибутов (справочник attr_values_cat заполняется при batch-импорте)
	for code, values := range attrRef {
		for _, val := range values {
			labelKey := turboKeyAttrLabel(code, val)
			t.store.TurboWrite(labelKey, []byte(val))
		}
	}

	// Записываем категориальные индексы значений атрибутов: turbo_attr_values_cat:{code}:{catID}
	for code, catMap := range attrCatRef {
		for catID, values := range catMap {
			key := "turbo_attr_values_cat:" + code + ":" + strconv.FormatInt(catID, 10)
			// Собираем уникальные значения для этой категории
			strValues := make([]string, 0, len(values))
			for val := range values {
				strValues = append(strValues, val)
			}
			if len(strValues) > 0 {
				if _, err := t.store.db.TurboPutBatchIndexString(key, strValues); err != nil {
					fmt.Printf("WARN: turbo batch attr_values_cat %s: %v\n", key, err)
				}
			}
		}
	}
	t.mu.Unlock()

	return nil
}

// ---------- sort indexes (по доке, полная перестройка) ----------

// BuildSortIndexes перестраивает все sort-индексы из turbo-индексов price и created.
// Вызывается один раз после импорта или по расписанию.
// BuildSortIndexesTx is the transactional version of BuildSortIndexes.
func (t *TurboProductSearch) BuildSortIndexesTx(txn *Transaction) error {
	if !t.enabled {
		return nil
	}

	start := time.Now().Unix()
	fmt.Println("[TURBO] Building sort indexes (transactional)...")

	tokens, err := t.store.db.TurboGetIndexTokens(TurboKeyProductList)
	if err != nil || len(tokens) == 0 {
		fmt.Println("[TURBO] No products in product_list")
		return nil
	}

	docs, err := t.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return fmt.Errorf("multi get products: %w", err)
	}

	type productSort struct {
		key     string
		price   float64
		created uint64
	}

	var products []productSort
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		p, err := UnmarshalProduct(doc)
		if err != nil {
			continue
		}
		products = append(products, productSort{
			key:     KeyProduct(p.ID),
			price:   p.Price,
			created: uint64(p.CreatedAt),
		})
	}

	priceAsc := make([]productSort, len(products))
	copy(priceAsc, products)
	sort.Slice(priceAsc, func(i, j int) bool {
		if priceAsc[i].price != priceAsc[j].price {
			return priceAsc[i].price < priceAsc[j].price
		}
		if priceAsc[i].created != priceAsc[j].created {
			return priceAsc[i].created > priceAsc[j].created
		}
		return priceAsc[i].key[0] < priceAsc[j].key[0] || (priceAsc[i].key[0] == priceAsc[j].key[0] && priceAsc[i].key[1] < priceAsc[j].key[1])
	})

	priceDesc := make([]productSort, len(products))
	copy(priceDesc, products)
	sort.Slice(priceDesc, func(i, j int) bool {
		if priceDesc[i].price != priceDesc[j].price {
			return priceDesc[i].price > priceDesc[j].price
		}
		if priceDesc[i].created != priceDesc[j].created {
			return priceDesc[i].created > priceDesc[j].created
		}
		return priceDesc[i].key[0] < priceDesc[j].key[0] || (priceDesc[i].key[0] == priceDesc[j].key[0] && priceDesc[i].key[1] < priceDesc[j].key[1])
	})

	createdDesc := make([]productSort, len(products))
	copy(createdDesc, products)
	sort.Slice(createdDesc, func(i, j int) bool {
		if createdDesc[i].created != createdDesc[j].created {
			return createdDesc[i].created > createdDesc[j].created
		}
		return createdDesc[i].key[0] < createdDesc[j].key[0] || (createdDesc[i].key[0] == createdDesc[j].key[0] && createdDesc[i].key[1] < createdDesc[j].key[1])
	})

	priceAscKeys := make([]string, len(priceAsc))
	priceDescKeys := make([]string, len(priceDesc))
	createdDescKeys := make([]string, len(createdDesc))

	for i := range products {
		priceAscKeys[i] = priceAsc[i].key
		priceDescKeys[i] = priceDesc[i].key
		createdDescKeys[i] = createdDesc[i].key
	}

	// Write sort indexes (buffered in transaction)
	if err := txn.TurboPutSortIndexString(turboSortPriceAsc, priceAscKeys); err != nil {
		return fmt.Errorf("turbo put sort index %s: %w", turboSortPriceAsc, err)
	}
	if err := txn.TurboPutSortIndexString(turboSortPriceDesc, priceDescKeys); err != nil {
		return fmt.Errorf("turbo put sort index %s: %w", turboSortPriceDesc, err)
	}
	if err := txn.TurboPutSortIndexString(turboSortCreatedAtDesc, createdDescKeys); err != nil {
		return fmt.Errorf("turbo put sort index %s: %w", turboSortCreatedAtDesc, err)
	}

	fmt.Printf("[TURBO] Sort indexes built (transactional): %d products, %v\n", len(products), time.Since(time.Unix(start, 0)))
	return nil
}

func (t *TurboProductSearch) BuildSortIndexes() error {
	if !t.enabled {
		return nil
	}

	start := time.Now().Unix()
	fmt.Println("[TURBO] Building sort indexes...")

	// Read product IDs from product_list
	tokens, err := t.store.db.TurboGetIndexTokens(TurboKeyProductList)
	if err != nil || len(tokens) == 0 {
		fmt.Println("[TURBO] No products in product_list")
		return nil
	}

	// Get all product data
	docs, err := t.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return fmt.Errorf("multi get products: %w", err)
	}

	// Parse products and collect for sorting
	type productSort struct {
		key     string
		price   float64
		created uint64
	}

	var products []productSort
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		p, err := UnmarshalProduct(doc)
		if err != nil {
			continue
		}
		products = append(products, productSort{
			key:     KeyProduct(p.ID),
			price:   p.Price,
			created: uint64(p.CreatedAt),
		})
	}

	// Sort by price ascending
	priceAsc := make([]productSort, len(products))
	copy(priceAsc, products)
	sort.Slice(priceAsc, func(i, j int) bool {
		if priceAsc[i].price != priceAsc[j].price {
			return priceAsc[i].price < priceAsc[j].price
		}
		// For stability, sort by created date descending, then by key
		if priceAsc[i].created != priceAsc[j].created {
			return priceAsc[i].created > priceAsc[j].created
		}
		return priceAsc[i].key[0] < priceAsc[j].key[0] || (priceAsc[i].key[0] == priceAsc[j].key[0] && priceAsc[i].key[1] < priceAsc[j].key[1])
	})

	// Sort by price descending
	priceDesc := make([]productSort, len(products))
	copy(priceDesc, products)
	sort.Slice(priceDesc, func(i, j int) bool {
		if priceDesc[i].price != priceDesc[j].price {
			return priceDesc[i].price > priceDesc[j].price
		}
		// For stability, sort by created date descending, then by key
		if priceDesc[i].created != priceDesc[j].created {
			return priceDesc[i].created > priceDesc[j].created
		}
		return priceDesc[i].key[0] < priceDesc[j].key[0] || (priceDesc[i].key[0] == priceDesc[j].key[0] && priceDesc[i].key[1] < priceDesc[j].key[1])
	})

	// Sort by created descending
	createdDesc := make([]productSort, len(products))
	copy(createdDesc, products)
	sort.Slice(createdDesc, func(i, j int) bool {
		if createdDesc[i].created != createdDesc[j].created {
			return createdDesc[i].created > createdDesc[j].created
		}
		return createdDesc[i].key[0] < createdDesc[j].key[0] || (createdDesc[i].key[0] == createdDesc[j].key[0] && createdDesc[i].key[1] < createdDesc[j].key[1])
	})

	// Extract Key128 arrays for sort indexes
	priceAscKeys := make([]string, len(priceAsc))
	priceDescKeys := make([]string, len(priceDesc))
	createdDescKeys := make([]string, len(createdDesc))

	for i := range products {
		priceAscKeys[i] = priceAsc[i].key
		priceDescKeys[i] = priceDesc[i].key
		createdDescKeys[i] = createdDesc[i].key
	}

	// Write sort indexes
	if err := t.store.db.TurboPutSortIndexString(turboSortPriceAsc, priceAscKeys); err != nil {
		return fmt.Errorf("turbo put sort index %s: %w", turboSortPriceAsc, err)
	}
	if err := t.store.db.TurboPutSortIndexString(turboSortPriceDesc, priceDescKeys); err != nil {
		return fmt.Errorf("turbo put sort index %s: %w", turboSortPriceDesc, err)
	}
	if err := t.store.db.TurboPutSortIndexString(turboSortCreatedAtDesc, createdDescKeys); err != nil {
		return fmt.Errorf("turbo put sort index %s: %w", turboSortCreatedAtDesc, err)
	}

	fmt.Printf("[TURBO] Sort indexes built: %d products, %v\n", len(products), time.Since(time.Unix(start, 0)))
	return nil
}

// ---------- search (по доке) ----------

type TurboListParams struct {
	Q           string
	CategoryID  int64
	CompanyID   int64
	BrandID     int64
	PriceMin    float64
	PriceMax    float64
	AttrFilters map[string][]string
	Sort        string
	Page        int
	Limit       int
	// FacetCodes — список кодов атрибутов, для которых нужно посчитать фасеты.
	// Если пусто, фасеты не считаются.
	FacetCodes []string
}

type TurboListResult struct {
	Items  []silentjson.RawMessage
	Total  int64
	Page   int
	Limit  int
	Facets *TurboFacets
}

// TurboFacets holds facet counts for filtering UI.
type TurboFacets struct {
	Brands map[string]int            `json:"brands,omitempty"` // brand name -> count
	Attrs  map[string]map[string]int `json:"attrs,omitempty"`  // attr_code -> {value -> count}
}

func (t *TurboProductSearch) ListWithTurbo(params TurboListParams) (*TurboListResult, error) {
	if !t.enabled {
		return nil, fmt.Errorf("turbo search is disabled")
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

	// start := time.Now().Unix()

	// 1) AND-индексы
	var andTokens []string

	if params.CategoryID != 0 {
		andTokens = append(andTokens, turboKeyCategory(params.CategoryID))
	}
	if params.CompanyID != 0 {
		andTokens = append(andTokens, turboKeyVendor(params.CompanyID))
	}
	if params.BrandID != 0 {
		andTokens = append(andTokens, turboKeyBrand(params.BrandID))
	}

	if params.Q != "" {
		tokens := tokenizeQuery(params.Q)
		for _, tok := range tokens {
			andTokens = append(andTokens, turboKeyText(tok))
		}
	}

	// 2) Пересечение AND
	var candidates []any
	var err error
	if len(andTokens) > 0 {
		candidatesSet, err := t.store.db.TurboBulkIntersect(andTokens)
		if err != nil {
			return nil, fmt.Errorf("turbo intersect: %w", err)
		}
		// Если фильтры есть, но ничего не найдено — возвращаем пустой результат
		// (nil от BulkIntersect может означать "индекс не найден", трактуем как пустой)
		if candidates == nil {
			candidates = []any{}
		} else {
			candidates = append(candidates, candidatesSet)
		}
	}

	// 2.5) Фильтрация по диапазону цены (через price:<range> индексы)
	if params.PriceMin > 0 || params.PriceMax > 0 {
		priceRangeKey := t.priceRangeKeyForFilter(params.PriceMin, params.PriceMax)
		if priceRangeKey != "" {
			priceTokens, err := t.store.db.TurboGetIndexTokens(priceRangeKey)
			if err == nil && len(priceTokens) > 0 {
				candidates = append(candidates, priceTokens)
				if len(candidates) == 0 {
					return &TurboListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
				}
			}
		}
	}

	// 3) OR-атрибуты
	for code, values := range params.AttrFilters {
		if len(values) == 0 {
			continue
		}
		attrTokens := make([]string, 0, len(values))
		for _, v := range values {
			attrTokens = append(attrTokens, turboKeyAttr(code, v))
		}
		attrIDs, err := t.store.db.TurboBulkUnion(attrTokens)
		if err != nil {
			return &TurboListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		if len(attrIDs) == 0 {
			return &TurboListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}

		if candidates == nil {
			candidates = append(candidates, attrIDs)
		}
		if len(candidates) == 0 {
			return &TurboListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
	}

	// 4) Сортировка + пагинация + загрузка документов через TurboSortIndexPageWithDocsFromDB
	var sortKey string
	switch params.Sort {
	case "price", "price_asc":
		sortKey = turboSortPriceAsc
	case "price_desc":
		sortKey = turboSortPriceDesc
	case "created_at":
		sortKey = turboSortCreatedAtDesc
	default:
		sortKey = ""
	}

	if sortKey == "" {
		sortKey = turboSortPriceAsc // по умолчанию
	}

	condFiltered := makodb.TurboIntersectSetsAny(candidates...)
	// TurboSortIndexPageWithDocsFromDB: пересечение + сортировка + пагинация + загрузка документов
	res, err := t.store.db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
		Name:       sortKey,
		Candidates: condFiltered,    // []Key128 от фасетов (или nil для всех)
		Page:       params.Page - 1, // 0-based
		PageSize:   params.Limit,
		Desc:       false, // true = обратный порядок
		DocPrefix:  "product:",
	})

	if err != nil {
		return nil, fmt.Errorf("turbo sort page with docs: %w", err)
	}

	// Парсим документы
	items := make([]silentjson.RawMessage, 0, len(res.Docs))
	for _, doc := range res.Docs {
		if doc == nil {
			continue
		}
		// p, err := UnmarshalProduct(doc)
		if err != nil {
			continue
		}
		items = append(items, doc)
	}

	total := int64(res.Total)

	//elapsed := time.Since(start)
	// fmt.Printf("DEBUG TurboListWithTurbo: total=%d page=%d items=%d time=%v\n",
	//	total, params.Page, len(items), elapsed)

	// 7) Фасеты (только для запрошенных кодов)
	var facets *TurboFacets
	if len(params.FacetCodes) > 0 && condFiltered != nil && len(condFiltered) > 0 {
		facets = t.computeFacets(condFiltered, params)
	}

	return &TurboListResult{
		Items:  items,
		Total:  total,
		Page:   params.Page,
		Limit:  params.Limit,
		Facets: facets,
	}, nil
}

// computeFacets считает фасеты по доке:
//   - Для брендов: берёт brand_list, для каждого brandID пересекает brand:<ID> с candidates
//   - Для атрибутов: для каждого запрошенного code берёт attr_values:<code>,
//     для каждого valueHash пересекает attr:<code>:<hash> с candidates
func (t *TurboProductSearch) computeFacets(candidates any, params TurboListParams) *TurboFacets {
	facets := &TurboFacets{
		Brands: make(map[string]int),
		Attrs:  make(map[string]map[string]int),
	}

	// Атрибуты: только запрошенные коды
	for _, code := range params.FacetCodes {

		// Если есть CategoryID, берём значения только для неё
		var valueHashes []string
		if params.CategoryID != 0 {
			catKey := "attr_values_cat:" + code + ":" + strconv.FormatInt(params.CategoryID, 10)
			data, _ := t.store.db.TurboRawRead(catKey)
			if data != nil && len(data) > 0 {
				var hashesSet map[string]struct{}
				if err := json.Unmarshal(data, &hashesSet); err == nil {
					valueHashes = make([]string, 0, len(hashesSet))
					for h := range hashesSet {
						valueHashes = append(valueHashes, h)
					}
				}
			}
		}

		if len(valueHashes) == 0 {
			continue
		}

		valueCounts := make(map[string]int)
		for _, hexH := range valueHashes {
			attrKey := "attr:" + code + ":" + hexH
			idxTokens, err := t.store.db.TurboGetIndexTokens(attrKey)
			if err != nil || len(idxTokens) == 0 {
				continue
			}
			count := len(makodb.TurboIntersectSetsAny(candidates, idxTokens))
			if count > 0 {
				// Получаем значение
				labelData, _ := t.store.db.TurboRawRead("attr_label:" + code + ":" + hexH)
				value := string(labelData)
				if value == "" {
					value = hexH
				}
				valueCounts[value] = count
			}
		}

		if len(valueCounts) > 0 {
			facets.Attrs[code] = valueCounts
		}
	}

	return facets
}

// GetAttrValues returns all values for an attribute code from the turbo index.
// Used by frontend to build filter UI.

// ---------- helpers ----------

// GetCategoryAncestors returns the category ID and all its ancestor IDs.
func (t *TurboProductSearch) GetCategoryAncestors(catID int64) ([]int64, error) {
	if catID == 0 {
		return nil, nil
	}
	var ancestors []int64
	current := catID
	for current != 0 {
		ancestors = append(ancestors, current)
		cat, err := t.categoryRepo.Get(current)
		if err != nil || cat == nil || cat.ParentID == nil {
			break
		}
		current = *cat.ParentID
	}
	return ancestors, nil
}

func tokenizeProduct(p *model.Product) []string {
	return tokenizeQuery(p.Name + " " + p.Description)
}

func tokenizeQuery(text string) []string {
	text = strings.ToLower(text)
	fields := strings.FieldsFunc(text, func(r rune) bool {
		// Разделители: всё кроме букв (латиница + кириллица), цифр и апострофа
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

// GetBrands returns all brands from brand_list index.
// If catID != 0, filters to brands that have products in that category.
func (t *TurboProductSearch) GetBrands(catID int64) ([]BrandInfo, error) {
	tokens, err := t.store.db.TurboGetIndexTokens(turboKeyBrandList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}

	// Use MultiGetByDocIDs to retrieve all brand documents at once (tokens already contain full keys)
	docs, err := t.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get brands: %w", err)
	}

	var result []BrandInfo
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		b, err := UnmarshalBrand(doc)
		if err != nil {
			continue
		}

		// If category filter, check if brand has products in that category
		if catID != 0 {
			brandTokens, _ := t.store.db.TurboGetIndexTokens("brand:" + strconv.FormatInt(b.ID, 10))
			catTokens, _ := t.store.db.TurboGetIndexTokens("cat:" + strconv.FormatInt(catID, 10))
			intersection := makodb.TurboIntersectSetsAny(brandTokens, catTokens)
			if len(intersection) == 0 {
				continue
			}
		}

		result = append(result, BrandInfo{
			ID:   b.ID,
			Name: b.Name,
		})
	}

	return result, nil
}

// BrandInfo is a brand with ID and name.
type BrandInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// GetAllProducts returns all products using product_list index.
func (t *TurboProductSearch) GetAllProducts() ([]model.Product, error) {
	if !t.enabled {
		return nil, nil
	}

	// Read product IDs from product_list
	tokens, err := t.store.db.TurboGetIndexTokens(TurboKeyProductList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}

	// Use MultiGetByDocIDsWithPrefix to get all products at once
	docs, err := t.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get products: %w", err)
	}

	var result []model.Product
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		p, err := UnmarshalProduct(doc)
		if err != nil {
			continue
		}
		if err != nil {
			continue
		}
		result = append(result, *p)
	}

	return result, nil
}

// GetProductsByEAN returns all products with a given EAN.
func (t *TurboProductSearch) GetProductsByEAN(ean string) ([]model.Product, error) {
	if !t.enabled || ean == "" {
		return nil, nil
	}

	eanKey := "ean:" + ean
	tokens, err := t.store.db.TurboGetIndexTokens(eanKey)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}

	// Use MultiGetByDocIDsWithPrefix to get all products at once
	docs, err := t.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get products by EAN: %w", err)
	}

	var result []model.Product
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		p, err := UnmarshalProduct(doc)
		if err != nil {
			continue
		}
		result = append(result, *p)
	}

	return result, nil
}

// sortUint64 sorts a slice of uint64 using simple quicksort.
func sortUint64(a []uint64) {
	if len(a) <= 1 {
		return
	}
	quickSortUint64(a, 0, len(a)-1)
}

func quickSortUint64(a []uint64, lo, hi int) {
	for lo < hi {
		p := partitionUint64(a, lo, hi)
		if p-lo < hi-p {
			quickSortUint64(a, lo, p-1)
			lo = p + 1
		} else {
			quickSortUint64(a, p+1, hi)
			hi = p - 1
		}
	}
}

func partitionUint64(a []uint64, lo, hi int) int {
	pivot := a[lo+(hi-lo)/3]
	i, j := lo, hi
	for i <= j {
		for a[i] < pivot {
			i++
		}
		for a[j] > pivot {
			j--
		}
		if i <= j {
			a[i], a[j] = a[j], a[i]
			i++
			j--
		}
	}
	return i
}

// uniqueSortedUint64 returns a new slice with duplicates removed from a sorted slice.
func uniqueSortedUint64(a []uint64) []uint64 {
	if len(a) == 0 {
		return nil
	}
	result := make([]uint64, 0, len(a))
	result = append(result, a[0])
	for i := 1; i < len(a); i++ {
		if a[i] != a[i-1] {
			result = append(result, a[i])
		}
	}
	return result
}

// intersectSorted — оба слайса отсортированы (turbo-индексы гарантируют это).
func intersectSorted(a, b []uint64) []uint64 {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	result := make([]uint64, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

// intersectSortedKey128 — оба слайса отсортированы (turbo-индексы гарантируют это).
func intersectSortedKey128(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	result := make([]string, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i][0] < b[j][0] || (a[i][0] == b[j][0] && a[i][1] < b[j][1]) {
			i++
		} else {
			j++
		}
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// containsString checks if a string slice contains a value.
func containsString(slice []string, value string) bool {
	for _, s := range slice {
		if s == value {
			return true
		}
	}
	return false
}

// Fnv64 computes a 64-bit FNV-1a hash of the input string.
func Fnv64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
