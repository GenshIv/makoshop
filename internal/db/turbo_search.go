package db

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

// TurboProductSearch — строго по turbo_index_guide.md.
type TurboProductSearch struct {
	db           *makodb.ShardedDB
	repo         *ProductRepo
	categoryRepo *CategoryRepo
	mu           sync.RWMutex
	enabled      bool
}

func NewTurboProductSearch(db *makodb.ShardedDB, repo *ProductRepo, categoryRepo *CategoryRepo, enabled bool) *TurboProductSearch {
	return &TurboProductSearch{
		db:           db,
		repo:         repo,
		categoryRepo: categoryRepo,
		enabled:      enabled,
	}
}

// DB returns the underlying ShardedDB for direct turbo operations.
func (t *TurboProductSearch) DB() *makodb.ShardedDB {
	return t.db
}

// ---------- key helpers (по доке) ----------

func turboKeyBrand(brandID int64) string    { return "brand:" + strconv.FormatInt(brandID, 10) }
func turboKeyVendor(companyID int64) string { return "vendor:" + strconv.FormatInt(companyID, 10) }
func turboKeyCategory(catID int64) string   { return "cat:" + strconv.FormatInt(catID, 10) }
func turboKeyAttr(code string, value string) string {
	h := Fnv64(value)
	return "attr:" + code + ":" + strconv.FormatUint(h, 16)
}
func turboKeyAttrLabel(code string, value string) string {
	h := Fnv64(value)
	return "attr_label:" + code + ":" + strconv.FormatUint(h, 16)
}
func turboKeyText(token string) string { return "text:" + token }

// Справочники
var turboKeyBrandList = "brand_list"
var turboKeyBrandNamePrefix = "brand_name:"

const (
	turboSortPriceAsc      = "sort:price_asc"
	turboSortPriceDesc     = "sort:price_desc"
	turboSortCreatedAtDesc = "sort:created_at_desc"
)

// ---------- indexing ----------

// IndexProduct — для одиночных изменений.
func (t *TurboProductSearch) IndexProduct(p *model.Product) error {
	if !t.enabled {
		return nil
	}
	docID := uint64(p.ID)

	if p.BrandID != 0 {
		if _, err := t.db.TurboPutIndex(turboKeyBrand(p.BrandID), docID); err != nil {
			return fmt.Errorf("turbo brand index: %w", err)
		}
		// Обновляем справочник брендов
		t.ensureBrandInRef(p.BrandID, p.Brand)
	}

	if p.CompanyID != 0 {
		if _, err := t.db.TurboPutIndex(turboKeyVendor(p.CompanyID), docID); err != nil {
			return fmt.Errorf("turbo vendor index: %w", err)
		}
	}

	if p.CategoryID != 0 {
		ancestors, err := t.GetCategoryAncestors(p.CategoryID)
		if err != nil {
			ancestors = []int64{p.CategoryID}
		}
		for _, cid := range ancestors {
			if _, err := t.db.TurboPutIndex(turboKeyCategory(cid), docID); err != nil {
				return fmt.Errorf("turbo category index: %w", err)
			}
		}
	}

	for code, val := range p.Attributes {
		if valStr, ok := val.(string); ok && valStr != "" {
			if _, err := t.db.TurboPutIndex(turboKeyAttr(code, valStr), docID); err != nil {
				return fmt.Errorf("turbo attr index: %w", err)
			}
			// Обновляем справочник значений атрибута
			t.ensureAttrValueInRef(code, valStr)
		}
	}

	for _, tok := range tokenizeProduct(p) {
		if _, err := t.db.TurboPutIndex(turboKeyText(tok), docID); err != nil {
			return fmt.Errorf("turbo text index: %w", err)
		}
	}

	// Диапазоны цен (по доке, вариант 1)
	t.indexPriceRanges(p.Price, docID)

	return nil
}

// indexPriceRanges добавляет docID в индексы диапазонов цен.
// Фиксированные диапазоны: 0-5k, 5k-10k, 10k-20k, 20k-50k, 50k-100k, 100k+
func (t *TurboProductSearch) indexPriceRanges(price float64, docID uint64) {
	ranges := priceRanges()
	for _, r := range ranges {
		if price >= r.min && price < r.max {
			t.db.TurboPutIndex(r.key, docID)
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

	// Добавляем в список брендов
	brandListData, err := t.db.TurboRawRead(turboKeyBrandList)
	if err != nil {
		brandListData = nil
	}
	var brandList []uint64
	if brandListData != nil && len(brandListData) > 0 {
		brandList = makodb.TurboUnsafeReadTokens(brandListData)
	}
	// Проверяем, есть ли уже
	for _, id := range brandList {
		if id == uint64(brandID) {
			return
		}
	}
	brandList = append(brandList, uint64(brandID))
	buf := makodb.TurboBinaryNew(brandList)
	t.db.TurboRawWrite(turboKeyBrandList, buf)

	// Записываем имя бренда
	key := turboKeyBrandNamePrefix + strconv.FormatInt(brandID, 10)
	label := []byte(name)
	t.db.TurboRawWrite(key, label)
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
	t.db.TurboRawWrite(labelKey, label)
}

// UnindexProduct — для удаления/обновления.
func (t *TurboProductSearch) UnindexProduct(p *model.Product) error {
	if !t.enabled {
		return nil
	}
	docID := uint64(p.ID)

	if p.BrandID != 0 {
		t.db.TurboDeleteIndex(turboKeyBrand(p.BrandID), docID)
	}
	if p.CompanyID != 0 {
		t.db.TurboDeleteIndex(turboKeyVendor(p.CompanyID), docID)
	}
	if p.CategoryID != 0 {
		ancestors, err := t.GetCategoryAncestors(p.CategoryID)
		if err != nil {
			ancestors = []int64{p.CategoryID}
		}
		for _, cid := range ancestors {
			t.db.TurboDeleteIndex(turboKeyCategory(cid), docID)
		}
	}
	for code, val := range p.Attributes {
		if valStr, ok := val.(string); ok && valStr != "" {
			t.db.TurboDeleteIndex(turboKeyAttr(code, valStr), docID)
		}
	}
	for _, tok := range tokenizeProduct(p) {
		t.db.TurboDeleteIndex(turboKeyText(tok), docID)
	}
	return nil
}

// IndexProductBatch — для импорта.
func (t *TurboProductSearch) IndexProductBatch(products []*model.Product) error {
	if !t.enabled || len(products) == 0 {
		return nil
	}

	indexes := make(map[string][]uint64)
	// Справочники: собираем уникальные значения для batch-записи
	brandRef := make(map[uint64]string)           // brandID -> name
	attrRef := make(map[string]map[uint64]string) // code -> {hash -> value}

	for _, p := range products {
		docID := uint64(p.ID)

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
		for code, val := range p.Attributes {
			if valStr, ok := val.(string); ok && valStr != "" {
				indexes[turboKeyAttr(code, valStr)] = append(indexes[turboKeyAttr(code, valStr)], docID)
				// Справочник значений атрибута
				if attrRef[code] == nil {
					attrRef[code] = make(map[uint64]string)
				}
				h := Fnv64(valStr)
				attrRef[code][h] = valStr
			}
		}
		for _, tok := range tokenizeProduct(p) {
			indexes[turboKeyText(tok)] = append(indexes[turboKeyText(tok)], docID)
		}
	}

	// Записываем индексы
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := t.db.TurboPutBatchIndex(key, docIDs); err != nil {
			fmt.Printf("WARN: turbo batch index %s: %v\n", key, err)
		}
	}

	// Записываем справочник брендов
	t.mu.Lock()
	for bid, name := range brandRef {
		// Добавляем в brand_list
		brandListData, _ := t.db.TurboRawRead(turboKeyBrandList)
		var brandList []uint64
		if brandListData != nil && len(brandListData) > 0 {
			brandList = makodb.TurboUnsafeReadTokens(brandListData)
		}
		found := false
		for _, id := range brandList {
			if id == bid {
				found = true
				break
			}
		}
		if !found {
			brandList = append(brandList, bid)
		}
		buf := makodb.TurboBinaryNew(brandList)
		t.db.TurboRawWrite(turboKeyBrandList, buf)

		// Записываем имя
		key := turboKeyBrandNamePrefix + strconv.FormatUint(bid, 10)
		t.db.TurboRawWrite(key, []byte(name))
	}

	// Записываем метки атрибутов (справочник attr_values_cat заполняется при batch-импорте)
	for code, values := range attrRef {
		for _, val := range values {
			labelKey := turboKeyAttrLabel(code, val)
			t.db.TurboRawWrite(labelKey, []byte(val))
		}
	}
	t.mu.Unlock()

	return nil
}

// ---------- sort indexes (по доке, полная перестройка) ----------

// BuildSortIndexes перестраивает все sort-индексы из текущих продуктов.
// Вызывается один раз после импорта или по расписанию.
func (t *TurboProductSearch) BuildSortIndexes() error {
	if !t.enabled {
		return nil
	}

	start := time.Now()
	fmt.Println("[TURBO] Building sort indexes...")

	all, err := t.repo.GetAllProducts()
	if err != nil {
		return fmt.Errorf("get all products: %w", err)
	}

	type priced struct {
		docID uint64
		price float64
	}
	type timed struct {
		docID uint64
		ts    int64
	}

	pricesAsc := make([]priced, 0, len(all))
	pricesDesc := make([]priced, 0, len(all))
	createdDesc := make([]timed, 0, len(all))

	for _, p := range all {
		docID := uint64(p.ID)
		pricesAsc = append(pricesAsc, priced{docID: docID, price: p.Price})
		pricesDesc = append(pricesDesc, priced{docID: docID, price: p.Price})
		createdDesc = append(createdDesc, timed{docID: docID, ts: p.CreatedAt.UnixNano()})
	}

	sortPricesAsc := func() []uint64 {
		sort.Slice(pricesAsc, func(i, j int) bool {
			if pricesAsc[i].price != pricesAsc[j].price {
				return pricesAsc[i].price < pricesAsc[j].price
			}
			return pricesAsc[i].docID < pricesAsc[j].docID
		})
		out := make([]uint64, len(pricesAsc))
		for i, e := range pricesAsc {
			out[i] = e.docID
		}
		return out
	}

	sortPricesDesc := func() []uint64 {
		sort.Slice(pricesDesc, func(i, j int) bool {
			if pricesDesc[i].price != pricesDesc[j].price {
				return pricesDesc[i].price > pricesDesc[j].price
			}
			return pricesDesc[i].docID < pricesDesc[j].docID
		})
		out := make([]uint64, len(pricesDesc))
		for i, e := range pricesDesc {
			out[i] = e.docID
		}
		return out
	}

	sortCreatedDesc := func() []uint64 {
		sort.Slice(createdDesc, func(i, j int) bool {
			if createdDesc[i].ts != createdDesc[j].ts {
				return createdDesc[i].ts > createdDesc[j].ts
			}
			return createdDesc[i].docID < createdDesc[j].docID
		})
		out := make([]uint64, len(createdDesc))
		for i, e := range createdDesc {
			out[i] = e.docID
		}
		return out
	}

	writeSortIndex := func(key string, docIDs []uint64) error {
		buf := makodb.TurboBinaryNew(docIDs)
		if err := t.db.TurboRawWrite(key, buf); err != nil {
			return fmt.Errorf("turbo raw write %s: %w", key, err)
		}
		return nil
	}

	if err := writeSortIndex(turboSortPriceAsc, sortPricesAsc()); err != nil {
		return err
	}
	if err := writeSortIndex(turboSortPriceDesc, sortPricesDesc()); err != nil {
		return err
	}
	if err := writeSortIndex(turboSortCreatedAtDesc, sortCreatedDesc()); err != nil {
		return err
	}

	fmt.Printf("[TURBO] Sort indexes built: %d products, %v\n", len(all), time.Since(start))
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
	Items  []ProductListItem
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

	start := time.Now()

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
	var candidates []uint64
	var err error
	if len(andTokens) > 0 {
		candidates, err = t.db.TurboBulkIntersect(andTokens)
		if err != nil {
			return nil, fmt.Errorf("turbo intersect: %w", err)
		}
		// Если фильтры есть, но ничего не найдено — возвращаем пустой результат
		// (nil от BulkIntersect может означать "индекс не найден", трактуем как пустой)
		if candidates == nil {
			candidates = []uint64{}
		}
	}

	// 2.5) Фильтрация по диапазону цены (через price:<range> индексы)
	if params.PriceMin > 0 || params.PriceMax > 0 {
		priceRangeKey := t.priceRangeKeyForFilter(params.PriceMin, params.PriceMax)
		if priceRangeKey != "" {
			priceIdx, err := t.db.TurboRawRead(priceRangeKey)
			if err == nil && len(priceIdx) > 0 {
				priceTokens := makodb.TurboUnsafeReadTokens(priceIdx)
				if candidates == nil {
					candidates = priceTokens
				} else {
					candidates = intersectSorted(candidates, priceTokens)
				}
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
		attrIDs, err := t.db.TurboBulkUnion(attrTokens)
		if err != nil {
			return &TurboListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		if len(attrIDs) == 0 {
			return &TurboListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		// TurboBulkUnion returns unsorted result; sort for intersectSorted.
		sort.Slice(attrIDs, func(i, j int) bool {
			return attrIDs[i] < attrIDs[j]
		})
		if candidates == nil {
			candidates = attrIDs
		} else {
			candidates = intersectSorted(candidates, attrIDs)
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

	// TurboSortIndexPageWithDocsFromDB: пересечение + сортировка + пагинация + загрузка документов
	res, err := t.db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
		Name:       sortKey,
		Candidates: candidates,      // []uint64 от фасетов (или nil для всех)
		Page:       params.Page - 1, // 0-based
		PageSize:   params.Limit,
		Desc:       false, // true = обратный порядок
		DocPrefix:  "turbo_idx:product:",
	})

	if err != nil {
		return nil, fmt.Errorf("turbo sort page with docs: %w", err)
	}

	// Парсим документы
	items := make([]ProductListItem, 0, len(res.Docs))
	for _, doc := range res.Docs {
		if doc == nil {
			continue
		}
		p, err := UnmarshalProduct(doc)
		if err != nil {
			continue
		}
		items = append(items, ProductListItem{
			ID:         p.ID,
			SKU:        p.SKU,
			Name:       p.Name,
			CategoryID: p.CategoryID,
			CompanyID:  p.CompanyID,
			Brand:      p.Brand,
			Price:      p.Price,
			Currency:   p.Currency,
			Status:     p.Status,
			Attributes: p.Attributes,
			Images:     p.Images,
		})
	}

	total := int64(res.Total)

	elapsed := time.Since(start)
	fmt.Printf("DEBUG TurboListWithTurbo: total=%d page=%d items=%d time=%v\n",
		total, params.Page, len(items), elapsed)

	// 7) Фасеты (только для запрошенных кодов)
	var facets *TurboFacets
	if len(params.FacetCodes) > 0 && candidates != nil && len(candidates) > 0 {
		facets = t.computeFacets(candidates, params)
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
func (t *TurboProductSearch) computeFacets(candidates []uint64, params TurboListParams) *TurboFacets {
	facets := &TurboFacets{
		Brands: make(map[string]int),
		Attrs:  make(map[string]map[string]int),
	}

	// Бренды: всегда считаем, если есть хотя бы один запрошенный фасет
	brandListData, err := t.db.TurboRawRead(turboKeyBrandList)
	if err == nil && len(brandListData) > 0 {
		brandIDs := makodb.TurboUnsafeReadTokens(brandListData)
		for _, bid := range brandIDs {
			// Не показываем выбранный бренд как фасет
			if params.BrandID != 0 && int64(bid) == params.BrandID {
				continue
			}
			key := turboKeyBrand(int64(bid))
			idxData, err := t.db.TurboRawRead(key)
			if err != nil || len(idxData) == 0 {
				continue
			}
			idxTokens := makodb.TurboUnsafeReadTokens(idxData)
			count := len(intersectSorted(candidates, idxTokens))
			if count > 0 {
				// Получаем имя бренда
				nameData, _ := t.db.TurboRawRead(turboKeyBrandNamePrefix + strconv.FormatInt(int64(bid), 10))
				name := string(nameData)
				if name == "" {
					name = strconv.FormatInt(int64(bid), 10)
				}
				facets.Brands[name] = count
			}
		}
	}

	// Атрибуты: только запрошенные коды
	for _, code := range params.FacetCodes {
		// Пропускаем бренд — он уже обработан выше
		if code == "brand" {
			continue
		}

		// Если есть CategoryID, берём значения только для неё
		var valueHashes []uint64
		if params.CategoryID != 0 {
			catKey := "attr_values_cat:" + code + ":" + strconv.FormatInt(params.CategoryID, 10)
			catData, err := t.db.TurboRawRead(catKey)
			if err == nil && len(catData) > 0 {
				valueHashes = makodb.TurboUnsafeReadTokens(catData)
			}
		}

		if len(valueHashes) == 0 {
			continue
		}

		valueCounts := make(map[string]int)
		for _, h := range valueHashes {
			hexH := strconv.FormatUint(h, 16)
			attrKey := "attr:" + code + ":" + hexH
			idxData, err := t.db.TurboRawRead(attrKey)
			if err != nil || len(idxData) == 0 {
				continue
			}
			idxTokens := makodb.TurboUnsafeReadTokens(idxData)
			count := len(intersectSorted(candidates, idxTokens))
			if count > 0 {
				// Получаем значение
				labelData, _ := t.db.TurboRawRead("attr_label:" + code + ":" + hexH)
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
	data, err := t.db.TurboRawRead(turboKeyBrandList)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	brandIDs := makodb.TurboUnsafeReadTokens(data)
	var result []BrandInfo

	for _, bid := range brandIDs {
		// Get brand name
		nameKey := turboKeyBrandNamePrefix + strconv.FormatUint(bid, 10)
		nameData, _ := t.db.TurboRawRead(nameKey)
		name := string(nameData)
		if name == "" {
			name = strconv.FormatUint(bid, 10)
		}

		// If category filter, check if brand has products in that category
		if catID != 0 {
			brandIdx, err := t.db.TurboRawRead("brand:" + strconv.FormatUint(bid, 10))
			if err != nil || len(brandIdx) == 0 {
				continue
			}
			catIdx, err := t.db.TurboRawRead("cat:" + strconv.FormatInt(catID, 10))
			if err != nil || len(catIdx) == 0 {
				continue
			}
			brandTokens := makodb.TurboUnsafeReadTokens(brandIdx)
			catTokens := makodb.TurboUnsafeReadTokens(catIdx)
			intersection := intersectSorted(brandTokens, catTokens)
			if len(intersection) == 0 {
				continue
			}
		}

		result = append(result, BrandInfo{
			ID:   int64(bid),
			Name: name,
		})
	}

	return result, nil
}

// BrandInfo is a brand with ID and name.
type BrandInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// GetAllProducts returns all products using turbo sort index.
func (t *TurboProductSearch) GetAllProducts() ([]model.Product, error) {
	if !t.enabled {
		return nil, nil
	}

	// Read from price sort index (contains all products)
	sortData, err := t.db.TurboRawRead(turboSortPriceAsc)
	if err != nil || len(sortData) == 0 {
		return nil, nil
	}

	docIDs := makodb.TurboUnsafeReadTokens(sortData)
	if len(docIDs) == 0 {
		return nil, nil
	}

	var result []model.Product
	for _, docID := range docIDs {
		p, err := t.repo.Get(int64(docID))
		if err != nil {
			continue
		}
		result = append(result, *p)
	}

	return result, nil
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
