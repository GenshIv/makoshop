package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

// SCUPageSearch — turbo-поиск по посадочным страницам (SCUPage).
// Каталог и фильтрация работают по SCUPage, не по товарам.
type SCUPageSearch struct {
	db           *makodb.ShardedDB
	repo         *SCUPageRepo
	productRepo  *ProductRepo
	categoryRepo *CategoryRepo
	enabled      bool
}

func NewSCUPageSearch(db *makodb.ShardedDB, repo *SCUPageRepo, productRepo *ProductRepo, categoryRepo *CategoryRepo, enabled bool) *SCUPageSearch {
	return &SCUPageSearch{
		db:           db,
		repo:         repo,
		productRepo:  productRepo,
		categoryRepo: categoryRepo,
		enabled:      enabled,
	}
}

// ---------- key helpers ----------

func scupageKeyCategory(catID int64) string { return "scupage_cat:" + strconv.FormatInt(catID, 10) }
func scupageKeyBrand(brandID int64) string  { return "scupage_brand:" + strconv.FormatInt(brandID, 10) }
func scupageKeyVendor(companyID int64) string {
	return "scupage_vendor:" + strconv.FormatInt(companyID, 10)
}
func scupageKeyAttr(code string, value string) string {
	h := Fnv64(value)
	return "scupage_attr:" + code + ":" + strconv.FormatUint(h, 16)
}
func scupageKeyText(token string) string { return "scupage_text:" + token }

const (
	scupageSortPriceAsc      = "scupage_sort:price_asc"
	scupageSortPriceDesc     = "scupage_sort:price_desc"
	scupageSortCreatedAtDesc = "scupage_sort:created_at_desc"
	scupageNumSortPrice      = "scupage_price"
)

// ---------- indexing ----------

// IndexSCUPage indexes a single SCU page into turbo indexes.
func (s *SCUPageSearch) IndexSCUPage(sp *model.SCUPage) error {
	if !s.enabled {
		return nil
	}
	docID := uint64(sp.ID)

	// Category index + ancestors
	if sp.CategoryID != 0 {
		ancestors, err := s.getCategoryAncestors(sp.CategoryID)
		if err != nil {
			ancestors = []int64{sp.CategoryID}
		}
		for _, cid := range ancestors {
			if _, err := s.db.TurboPutIndex(scupageKeyCategory(cid), docID); err != nil {
				return fmt.Errorf("turbo scupage category index: %w", err)
			}
		}
	}

	// Brand index
	if sp.BrandID != 0 {
		if _, err := s.db.TurboPutIndex(scupageKeyBrand(sp.BrandID), docID); err != nil {
			return fmt.Errorf("turbo scupage brand index: %w", err)
		}
	}

	// Attributes index (from merged SCUPage attributes)
	for code, val := range sp.Attributes {
		if valStr, ok := val.(string); ok && valStr != "" {
			if _, err := s.db.TurboPutIndex(scupageKeyAttr(code, valStr), docID); err != nil {
				return fmt.Errorf("turbo scupage attr index: %w", err)
			}
		}
	}

	// Text index (title + description)
	for _, tok := range tokenizeSCUPage(sp) {
		if _, err := s.db.TurboPutIndex(scupageKeyText(tok), docID); err != nil {
			return fmt.Errorf("turbo scupage text index: %w", err)
		}
	}

	// Sort indexes are built once by BuildSortIndexes(), not per-page.
	return nil
}

// IndexSCUPageBatch indexes many SCU pages using batch turbo writes.
// ~10x faster than calling IndexSCUPage in a loop.
func (s *SCUPageSearch) IndexSCUPageBatch(pages []*model.SCUPage) error {
	if !s.enabled || len(pages) == 0 {
		return nil
	}

	// Collect all indexes in memory
	indexes := make(map[string][]uint64)

	for _, sp := range pages {
		docID := uint64(sp.ID)

		// Category index + ancestors
		if sp.CategoryID != 0 {
			ancestors, err := s.getCategoryAncestors(sp.CategoryID)
			if err != nil {
				ancestors = []int64{sp.CategoryID}
			}
			for _, cid := range ancestors {
				indexes[scupageKeyCategory(cid)] = append(indexes[scupageKeyCategory(cid)], docID)
			}
		}

		// Brand index
		if sp.BrandID != 0 {
			indexes[scupageKeyBrand(sp.BrandID)] = append(indexes[scupageKeyBrand(sp.BrandID)], docID)
		}

		// Attributes index
		for code, val := range sp.Attributes {
			if valStr, ok := val.(string); ok && valStr != "" {
				indexes[scupageKeyAttr(code, valStr)] = append(indexes[scupageKeyAttr(code, valStr)], docID)
			}
		}

		// Text index
		for _, tok := range tokenizeSCUPage(sp) {
			indexes[scupageKeyText(tok)] = append(indexes[scupageKeyText(tok)], docID)
		}
	}

	// Write all indexes in batch
	for key, docIDs := range indexes {
		if len(docIDs) == 0 {
			continue
		}
		if _, err := s.db.TurboPutBatchIndex(key, docIDs); err != nil {
			fmt.Printf("WARN: scupage batch index %s: %v\n", key, err)
		}
	}

	return nil
}

// UnindexSCUPage removes a SCU page from all turbo indexes.
func (s *SCUPageSearch) UnindexSCUPage(sp *model.SCUPage) error {
	if !s.enabled {
		return nil
	}
	docID := uint64(sp.ID)

	if sp.CategoryID != 0 {
		ancestors, err := s.getCategoryAncestors(sp.CategoryID)
		if err != nil {
			ancestors = []int64{sp.CategoryID}
		}
		for _, cid := range ancestors {
			s.db.TurboDeleteIndex(scupageKeyCategory(cid), docID)
		}
	}

	if sp.BrandID != 0 {
		s.db.TurboDeleteIndex(scupageKeyBrand(sp.BrandID), docID)
	}

	for code, val := range sp.Attributes {
		if valStr, ok := val.(string); ok && valStr != "" {
			s.db.TurboDeleteIndex(scupageKeyAttr(code, valStr), docID)
		}
	}

	for _, tok := range tokenizeSCUPage(sp) {
		s.db.TurboDeleteIndex(scupageKeyText(tok), docID)
	}

	return nil
}

// BuildSortIndexes rebuilds all sort indexes for SCU pages.
func (s *SCUPageSearch) BuildSortIndexes() error {
	if !s.enabled {
		return nil
	}

	start := time.Now()
	fmt.Println("[SCUPAGE] Building sort indexes...")

	all, err := s.repo.List()
	if err != nil {
		return fmt.Errorf("list scupages: %w", err)
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

	for _, sp := range all {
		docID := uint64(sp.ID)
		pricesAsc = append(pricesAsc, priced{docID: docID, price: sp.MinPrice})
		pricesDesc = append(pricesDesc, priced{docID: docID, price: sp.MinPrice})
		createdDesc = append(createdDesc, timed{docID: docID, ts: sp.CreatedAt.UnixNano()})
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

	writeSortIndex := func(name string, docIDs []uint64) error {
		if err := s.db.TurboPutSortIndex(name, docIDs); err != nil {
			return fmt.Errorf("turbo put sort index %s: %w", name, err)
		}
		return nil
	}

	if err := writeSortIndex(scupageSortPriceAsc, sortPricesAsc()); err != nil {
		return err
	}
	if err := writeSortIndex(scupageSortPriceDesc, sortPricesDesc()); err != nil {
		return err
	}
	if err := writeSortIndex(scupageSortCreatedAtDesc, sortCreatedDesc()); err != nil {
		return err
	}

	// Build numSort index for price range filtering
	// Store price * 100 as uint64 (kopecks) to preserve precision
	pricePairs := make([]makodb.TurboNumSortPair, len(all))
	for i, sp := range all {
		pricePairs[i] = makodb.TurboNumSortPair{
			Value: uint64(sp.MinPrice * 100),
			DocID: uint64(sp.ID),
		}
	}
	_, _ = s.db.TurboPutNumSortBatch(scupageNumSortPrice, pricePairs)

	fmt.Printf("[SCUPAGE] Sort indexes built: %d pages, %v\n", len(all), time.Since(start))
	return nil
}

// ---------- search ----------

type SCUPageListParams struct {
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

type SCUPageListResult struct {
	Items []json.RawMessage `json:"items"` // raw SCUPage JSON
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

// ListWithTurbo returns paginated SCU pages with filters and sorting.
func (s *SCUPageSearch) ListWithTurbo(params SCUPageListParams) (*SCUPageListResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("scupage search is disabled")
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

	// 1) Build category filter (category + all descendants via union)
	var candidates []uint64

	if params.CategoryID != 0 {
		catIDs, err := s.getCategoryWithDescendants(params.CategoryID)
		if err != nil {
			return nil, fmt.Errorf("get category descendants: %w", err)
		}
		if len(catIDs) == 0 {
			catIDs = []int64{params.CategoryID}
		}
		catTokens := make([]string, len(catIDs))
		for i, cid := range catIDs {
			catTokens[i] = scupageKeyCategory(cid)
		}
		catResult, err := s.db.TurboBulkUnion(catTokens)
		if err != nil {
			return nil, fmt.Errorf("turbo union categories: %w", err)
		}
		candidates = catResult
		if len(candidates) == 0 {
			return &SCUPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
	}

	// 2) AND-индексы (vendor, text search)
	var andTokens []string

	if params.CompanyID != 0 {
		andTokens = append(andTokens, scupageKeyVendor(params.CompanyID))
	}

	if params.Q != "" {
		tokens := tokenizeQuerySCUPage(params.Q)
		for _, tok := range tokens {
			andTokens = append(andTokens, scupageKeyText(tok))
		}
	}

	if len(andTokens) > 0 {
		andResult, err := s.db.TurboBulkIntersect(andTokens)
		if err != nil {
			return nil, fmt.Errorf("turbo intersect: %w", err)
		}
		if len(andResult) == 0 {
			return &SCUPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		if candidates == nil {
			candidates = andResult
		} else {
			candidates = intersectSorted(candidates, andResult)
		}
		if len(candidates) == 0 {
			return &SCUPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
	}

	// 3) OR-атрибуты
	for code, values := range params.AttrFilters {
		if len(values) == 0 {
			continue
		}
		attrTokens := make([]string, 0, len(values))
		for _, v := range values {
			attrTokens = append(attrTokens, scupageKeyAttr(code, v))
		}
		attrIDs, err := s.db.TurboBulkUnion(attrTokens)
		if err != nil {
			return &SCUPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		if len(attrIDs) == 0 {
			return &SCUPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
		sort.Slice(attrIDs, func(i, j int) bool {
			return attrIDs[i] < attrIDs[j]
		})
		if candidates == nil {
			candidates = attrIDs
		} else {
			candidates = intersectSorted(candidates, attrIDs)
		}
		if len(candidates) == 0 {
			return &SCUPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
		}
	}

	// 3b) Price range filter via numSort index (stored as price * 100)
	if len(candidates) > 0 && (params.PriceMin > 0 || params.PriceMax > 0) {
		minVal := uint64(params.PriceMin * 100)
		maxVal := uint64(params.PriceMax * 100)
		if params.PriceMax == 0 {
			maxVal = ^uint64(0) // no upper bound
		}
		priceResult, err := s.db.TurboGetNumSortRangeIntersectCandidates(
			scupageNumSortPrice,
			minVal,
			maxVal,
			candidates,
			0,
			len(candidates), // get all matching docIDs
		)
		if err != nil {
			// Fallback: continue without price filter
		} else {
			filtered := make([]uint64, len(priceResult.Pairs))
			for i, p := range priceResult.Pairs {
				filtered[i] = p.DocID
			}
			if len(filtered) == 0 {
				return &SCUPageListResult{Items: nil, Total: 0, Page: params.Page, Limit: params.Limit}, nil
			}
			candidates = filtered
		}
	}

	// 4) Сортировка + пагинация + загрузка документов
	var sortKey string
	switch params.Sort {
	case "price", "price_asc":
		sortKey = scupageSortPriceAsc
	case "price_desc":
		sortKey = scupageSortPriceDesc
	case "created_at":
		sortKey = scupageSortCreatedAtDesc
	default:
		sortKey = scupageSortPriceAsc
	}

	// Use candidates if we have them, otherwise use full sort index
	var sortCandidates []uint64
	if len(candidates) > 0 {
		sortCandidates = candidates
	}

	res, err := s.db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
		Name:       sortKey,
		Candidates: sortCandidates,
		Page:       params.Page - 1,
		PageSize:   params.Limit,
		Desc:       false,
		DocPrefix:  "turbo_idx:scupage:",
	})

	if err != nil {
		return nil, fmt.Errorf("turbo sort page with docs: %w", err)
	}

	// 5) Return raw JSON documents directly (no unmarshal/marshal)
	items := make([]json.RawMessage, 0, len(res.Docs))
	for _, doc := range res.Docs {
		if doc == nil || len(doc) == 0 {
			continue
		}
		items = append(items, json.RawMessage(doc))
	}

	return &SCUPageListResult{
		Items: items,
		Total: int64(res.Total),
		Page:  params.Page,
		Limit: params.Limit,
	}, nil
}

// getCategoryWithDescendants returns the given category ID and all its descendants.
// Uses cached descendants index for O(1) lookup.
func (s *SCUPageSearch) getCategoryWithDescendants(catID int64) ([]int64, error) {
	descendants, err := s.categoryRepo.GetDescendants(catID)
	if err != nil {
		return nil, err
	}
	result := make([]int64, 0, len(descendants)+1)
	result = append(result, catID)
	result = append(result, descendants...)
	return result, nil
}

// ---------- helpers ----------

func (s *SCUPageSearch) getCategoryAncestors(catID int64) ([]int64, error) {
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

func tokenizeSCUPage(sp *model.SCUPage) []string {
	return tokenizeQuerySCUPage(sp.Title + " " + sp.Description)
}

func tokenizeQuerySCUPage(text string) []string {
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
