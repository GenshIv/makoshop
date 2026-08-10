package api

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/attrs"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/idxbuild"
	"github.com/GenshIv/makoshop/internal/model"
)

// ImagePlaceholder is a placeholder image used when product has no images.
const ImagePlaceholder = "https://via.placeholder.com/400x400?text=No+Image"

// ImportPricesResult holds the result of a price import operation.
type ImportPricesResult struct {
	Status           string `json:"status"`
	Categories       int    `json:"categories"`
	ProductsImported int    `json:"products_imported"`
	ProductsSkipped  int    `json:"products_skipped"`
	Brands           int    `json:"brands"`
}

// HandleAdminImportPrices imports products from CSV files in _tmp/prices
// or from normalized JSONL files in _tmp/normalized.
// POST /admin/import-prices
// Query params:
//   - source=csv|normalized|multi   data source (default: csv)
//     csv       - single company from _tmp/prices/*.csv
//     normalized - from _tmp/normalized/ (single company)
//     multi     - multi-company from _tmp/prices/{company_name}/*.csv
//   - limit=N                 max products to import (per company in multi mode)
//   - company=NAME            company name for csv/normalized mode (default: "Magazilla Import")
//   - no_attrs=1              skip attribute parsing (csv mode only)
//   - workers=N               parallel workers (default: 8)
//   - use_existing_cats=1     use existing categories by name (default: 1)
func (h *Handlers) HandleAdminImportPrices(w http.ResponseWriter, r *http.Request) {
	fmt.Println("[IMPORT] HandleAdminImportPrices called, method=", r.Method)
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	source := r.URL.Query().Get("source")
	fmt.Println("[IMPORT] source=", source)
	if source == "" {
		source = "csv"
	}

	if source == "multi" {
		h.importMultiCompany(w, r)
		return
	}

	if source == "normalized" {
		h.importNormalized(w, r)
		return
	}

	// CSV import (existing logic - single company)
	inputDir := "_tmp/prices"
	limit := 0
	companyName := "Magazilla Import"
	noAttrs := false
	noCats := false
	workers := 8

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("company"); v != "" {
		companyName = v
	}
	if r.URL.Query().Get("no_attrs") == "1" {
		noAttrs = true
	}
	if r.URL.Query().Get("no_cats") == "1" {
		noCats = true
	}
	if v := r.URL.Query().Get("workers"); v != "" {
		workers, _ = strconv.Atoi(v)
	}
	if workers < 1 {
		workers = 1
	}

	company, err := ensureCompany(h.companyRepo, h.userRepo, companyName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("ensure company: %v", err))
		return
	}

	csvFiles, err := filepath.Glob(filepath.Join(inputDir, "*.csv"))
	if err != nil {
		csvFiles, _ = walkCSVFiles(inputDir)
	}
	sort.Strings(csvFiles)

	if len(csvFiles) == 0 {
		writeJSON(w, http.StatusOK, ImportPricesResult{Status: "no_files"})
		return
	}

	// Build pathToID if no_cats=1 (use existing categories)
	var pathToID map[string]int64
	if noCats {
		fmt.Println("[IMPORT-CSV] no_cats=1: building category path map from existing categories...")
		pathToID, err = h.categoryRepo.BuildPathMap()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("build path map: %v", err))
			return
		}
		fmt.Printf("[IMPORT-CSV] Found %d existing category paths\n", len(pathToID))
	}

	type fileResult struct {
		file       string
		imported   int64
		skipped    int64
		categories int
		err        error
	}

	resultsCh := make(chan fileResult, len(csvFiles))

	var totalImported atomic.Int64
	var wg sync.WaitGroup
	fileSem := make(chan struct{}, workers)

	for _, csvFile := range csvFiles {
		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			fileSem <- struct{}{}
			defer func() { <-fileSem }()

			res, err := streamImportCSVFile(
				file,
				h.categoryRepo,
				h.productRepo,
				company.ID,
				limit,
				noAttrs,
				noCats,
				pathToID,
				&totalImported,
			)
			resultsCh <- fileResult{
				file:       file,
				imported:   res.imported,
				skipped:    res.skipped,
				categories: res.categories,
				err:        err,
			}
		}(csvFile)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var finalImported int64
	var finalSkipped int64
	var finalCategories int

	for res := range resultsCh {
		if res.err != nil {
			fmt.Printf("WARN: import file %s error: %v\n", res.file, res.err)
			continue
		}
		finalImported += res.imported
		finalSkipped += res.skipped
		finalCategories += res.categories
	}

	if finalImported == 0 && finalSkipped == 0 {
		writeJSON(w, http.StatusOK, ImportPricesResult{Status: "no_products"})
		return
	}

	writeJSON(w, http.StatusOK, ImportPricesResult{
		Status:           "completed",
		Categories:       finalCategories,
		ProductsImported: int(finalImported),
		ProductsSkipped:  int(finalSkipped),
		Brands:           0,
	})
}

// importNormalized imports products from _tmp/normalized
// using batch indexing via idxbuild (no TurboPutIndex in import loop).
// Query params:
//   - company=NAME            company name (default: "Magazilla Import")
//   - create_cats=1           create missing categories (default: 0)
//   - limit=N                 max products to import
//   - batch=N                 batch size (default: 100000)
//   - id_offset=N             add this offset to each product ID (for multi-company imports)
func (h *Handlers) importNormalized(w http.ResponseWriter, r *http.Request) {
	inputDir := "_tmp/normalized"
	tmpDir := "_tmp"
	limit := 0
	batchSize := 100_000
	companyName := "Magazilla Import"
	createCats := r.URL.Query().Get("create_cats") == "1"
	idOffset := int64(0)

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("batch"); v != "" {
		batchSize, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("company"); v != "" {
		companyName = v
	}
	if v := r.URL.Query().Get("id_offset"); v != "" {
		idOffset, _ = strconv.ParseInt(v, 10, 64)
	}
	if batchSize < 10_000 {
		batchSize = 10_000
	}

	fmt.Println("[IMPORT-NORMALIZED] Starting batch import from", inputDir)
	fmt.Printf("[IMPORT-NORMALIZED] Config: limit=%d batchSize=%d company=%s create_cats=%v id_offset=%d\n", limit, batchSize, companyName, createCats, idOffset)
	startTime := time.Now()

	// Ensure company
	company, err := ensureCompany(h.companyRepo, h.userRepo, companyName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("ensure company: %v", err))
		return
	}
	fmt.Printf("[IMPORT-NORMALIZED] Using company: %s (ID=%d)\n", company.Name, company.ID)

	// Step 1: Import categories (or use existing)
	fmt.Println("[IMPORT-NORMALIZED] Processing categories...")
	catFile := filepath.Join(inputDir, "categories.jsonl")
	pathToID, oldIDToPath, catCount, err := h.importCategories(catFile, createCats)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("import categories: %v", err))
		return
	}
	fmt.Printf("[IMPORT-NORMALIZED] Processed %d categories\n", catCount)

	// Step 1.5: Pre-build ancestors cache for all categories (avoid lazy DB calls during import)
	fmt.Println("[IMPORT-NORMALIZED] Building category ancestors cache...")
	catAncestorsCache := h.buildCategoryAncestorsCache(pathToID)
	fmt.Printf("[IMPORT-NORMALIZED] Ancestors cache built for %d categories\n", len(catAncestorsCache))

	// Step 2: Import products with batch indexing
	productFiles, err := filepath.Glob(filepath.Join(inputDir, "products-*.jsonl"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("glob: %v", err))
		return
	}
	sort.Strings(productFiles)

	if len(productFiles) == 0 {
		writeJSON(w, http.StatusOK, ImportPricesResult{Status: "no_files"})
		return
	}

	fmt.Printf("[IMPORT-NORMALIZED] Found %d product files to import\n", len(productFiles))

	// Batch accumulator for idxbuild
	accum := idxbuild.NewBatchAccum()
	batchID := 1

	// AttrDef collection: code -> set of category IDs
	codeCats := make(map[string]map[int64]struct{})

	// Attr values collection: code -> set of value strings
	codeValues := make(map[string]map[string]struct{})

	// Attr values per category: code -> catID -> set of value strings
	codeCatValues := make(map[string]map[int64]map[string]struct{})

	// Brand collection: brandID -> name
	brands := make(map[int64]string)

	var totalImported int
	var totalSkipped int

	for _, file := range productFiles {
		if limit > 0 && totalImported >= limit {
			break
		}

		// Передаём оставшееся количество для импорта
		remaining := 0
		if limit > 0 {
			remaining = limit - totalImported
		}

		imported, skipped, err := h.importNormalizedFileBatched(
			file, remaining, batchSize, pathToID, oldIDToPath,
			&accum, &batchID, codeCats, codeValues, codeCatValues, brands,
			company.ID, company.Name, idOffset, catAncestorsCache,
		)
		if err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: file %s error: %v\n", file, err)
		}
		totalImported += imported
		totalSkipped += skipped
		fmt.Printf("[IMPORT-NORMALIZED] File %s: imported=%d skipped=%d (total: %d)\n",
			filepath.Base(file), imported, skipped, totalImported)
	}

	fmt.Printf("[IMPORT-NORMALIZED] Pre-merge: imported=%d skipped=%d\n", totalImported, totalSkipped)

	// Step 3: Merge indexes from tmp files into DB
	if totalImported > 0 {
		db := h.productRepo.TurboSearch()
		if db == nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "turbo search not initialized")
			return
		}

		fmt.Println("[IMPORT-NORMALIZED] Merging indexes...")
		mergeStart := time.Now()

		if err := idxbuild.MergeIndexes(db.DB(), tmpDir); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: merge indexes: %v\n", err)
		}
		// Sort indexes are rebuilt separately after all imports via /admin/rebuild-sort-indexes

		fmt.Printf("[IMPORT-NORMALIZED] Merge completed in %v\n", time.Since(mergeStart))

		// Batch upsert attribute definitions
		fmt.Printf("[IMPORT-NORMALIZED] Building attrdefs (%d unique codes)\n", len(codeCats))
		if err := h.attrDefRepo.BatchUpsertCodes(codeCats); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: batch upsert attrdefs: %v\n", err)
		}

		// Batch write attribute value references (global + per-category)
		fmt.Printf("[IMPORT-NORMALIZED] Building attr value refs\n")
		if err := h.attrDefRepo.BatchWriteAttrValues(codeValues, codeCatValues); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: batch write attr values: %v\n", err)
		}

		// Batch write brand indexes
		if len(brands) > 0 {
			fmt.Printf("[IMPORT-NORMALIZED] Building brand indexes (%d brands)\n", len(brands))
			if err := h.batchWriteBrands(h.store, brands); err != nil {
				fmt.Printf("[IMPORT-NORMALIZED] WARN: batch write brands: %v\n", err)
			}
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[IMPORT-NORMALIZED] Completed: imported=%d skipped=%d time=%v (%.0f products/sec)\n",
		totalImported, totalSkipped, elapsed, float64(totalImported)/elapsed.Seconds())

	if totalImported == 0 && totalSkipped == 0 {
		writeJSON(w, http.StatusOK, ImportPricesResult{Status: "no_products"})
		return
	}

	writeJSON(w, http.StatusOK, ImportPricesResult{
		Status:           "completed",
		Categories:       0, // categories not imported in this mode
		ProductsImported: totalImported,
		ProductsSkipped:  totalSkipped,
		Brands:           0,
	})
}

// importCategories imports categories from a JSONL file.
// If createCats is false, only maps existing categories (no new ones created).
func (h *Handlers) importCategories(file string, createCats bool) (map[string]int64, map[int64]string, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, nil, 0, err
	}
	defer f.Close()

	type catRow struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	}

	// path -> dbCategoryID
	pathToID := make(map[string]int64)
	// oldID -> path (для маппинга из продуктов)
	oldIDToPath := make(map[int64]string)
	// name|parentPath -> dbCategoryID (для быстрого поиска существующих)
	keyToID := make(map[string]int64)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var row catRow
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}

		if row.Path == "" || row.Name == "" {
			continue
		}

		// Сохраняем маппинг oldID -> path
		oldIDToPath[row.ID] = row.Path

		// Определяем родительский path
		var parentPath string
		parts := strings.Split(row.Path, " -> ")
		if len(parts) > 1 {
			parentPath = strings.Join(parts[:len(parts)-1], " -> ")
		}

		// Ищем parentID
		var dbParentID *int64
		if parentPath != "" {
			if pid, ok := pathToID[parentPath]; ok {
				dbParentID = &pid
			}
		}

		// Ключ для поиска существующей категории
		key := row.Name + "|" + parentPath
		if id, ok := keyToID[key]; ok {
			pathToID[row.Path] = id
			continue
		}

		// Пробуем найти в БД по имени и родителю
		existingID := findCategoryByNameAndParent(h.categoryRepo, row.Name, dbParentID)
		if existingID != 0 {
			pathToID[row.Path] = existingID
			keyToID[key] = existingID
			continue
		}

		if createCats {
			// Создаём новую
			c := &model.Category{
				Name:     row.Name,
				Slug:     toSlugTranslit(row.Name),
				ParentID: dbParentID,
				IsActive: true,
			}
			if err := h.categoryRepo.Create(c); err != nil {
				fmt.Printf("[IMPORT-NORMALIZED] WARN: failed to create category %s: %v\n", row.Name, err)
				continue
			}

			pathToID[row.Path] = c.ID
			keyToID[key] = c.ID
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, 0, err
	}

	return pathToID, oldIDToPath, len(pathToID), nil
}

// findCategoryByNameAndParent finds an existing category by name and parent ID.
func findCategoryByNameAndParent(repo *db.CategoryRepo, name string, parentID *int64) int64 {
	all, err := repo.ListAll()
	if err != nil {
		return 0
	}
	for _, c := range all {
		if c.Name == name {
			if parentID == nil && c.ParentID == nil {
				return c.ID
			}
			if parentID != nil && c.ParentID != nil && *c.ParentID == *parentID {
				return c.ID
			}
		}
	}
	return 0
}

// buildCategoryAncestorsCache pre-builds a cache of category ancestors for all category IDs.
// pathToID: category path -> dbCategoryID (used to get all category IDs)
// Returns: map[catID][]ancestorIDs (includes the category itself)
func (h *Handlers) buildCategoryAncestorsCache(pathToID map[string]int64) map[int64][]int64 {
	cache := make(map[int64][]int64)

	// Collect all unique category IDs
	catIDs := make(map[int64]struct{}, len(pathToID))
	for _, id := range pathToID {
		catIDs[id] = struct{}{}
	}

	// For each category, walk up the tree using cached results
	var buildAncestors func(catID int64) []int64
	buildAncestors = func(catID int64) []int64 {
		if result, ok := cache[catID]; ok {
			return result
		}

		cat, err := h.categoryRepo.Get(catID)
		if err != nil || cat == nil {
			cache[catID] = []int64{catID}
			return cache[catID]
		}

		if cat.ParentID == nil {
			cache[catID] = []int64{catID}
			return cache[catID]
		}

		// Build ancestors for parent first
		parentAncestors := buildAncestors(*cat.ParentID)

		// This category's ancestors = itself + parent's ancestors
		ancestors := make([]int64, 0, len(parentAncestors)+1)
		ancestors = append(ancestors, catID)
		ancestors = append(ancestors, parentAncestors...)
		cache[catID] = ancestors
		return ancestors
	}

	for catID := range catIDs {
		buildAncestors(catID)
	}

	return cache
}

func (h *Handlers) importBrands(file string) (map[int64]int64, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	type brandRow struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}

	brandIDMap := make(map[int64]int64)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var row brandRow
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}

		// Just map old ID to new ID (brands are stored in product.Brand string)
		brandIDMap[row.ID] = row.ID
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	return brandIDMap, len(brandIDMap), nil
}

// importNormalizedFileBatched imports products from a single file using batched inserts
// with idxbuild batch indexing (no TurboPutIndex in loop).
// pathToID: category path -> dbCategoryID
// oldIDToPath: oldCategoryID (from JSONL) -> category path
// batchAccum: pointer to batch accumulator (can be reset)
// batchID: pointer to current batch ID (incremented on flush)
// codeCats: global attr code -> category set (populated during import)
// codeValues: global attr code -> value set (populated during import)
// codeCatValues: global attr code -> catID -> value set (populated during import)
// brands: global brandID -> name (populated during import)
// companyID: the company ID to assign to all imported products
// companyName: company name (added to product name for uniqueness)
// idOffset: add this offset to each product ID (for multi-company imports)
// catAncestorsCache: pre-built cache of category ancestors (catID -> []ancestorIDs)
func (h *Handlers) importNormalizedFileBatched(
	file string,
	limit, batchSize int,
	pathToID map[string]int64,
	oldIDToPath map[int64]string,
	batchAccum **idxbuild.BatchAccum,
	batchID *int,
	codeCats map[string]map[int64]struct{},
	codeValues map[string]map[string]struct{},
	codeCatValues map[string]map[int64]map[string]struct{},
	brands map[int64]string,
	companyID int64,
	companyName string,
	idOffset int64,
	catAncestorsCache map[int64][]int64,
) (int, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)

	// Первый проход: собираем все уникальные oldCategoryID из файла
	uniqueOldCatIDs := make(map[int64]struct{})
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var row struct {
			CategoryID int64 `json:"category_id"`
		}
		if err := json.Unmarshal(line, &row); err == nil && row.CategoryID != 0 {
			uniqueOldCatIDs[row.CategoryID] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	// Строим локальную мапу oldCategoryID -> dbCategoryID для этого файла
	fileCatMap := make(map[int64]int64, len(uniqueOldCatIDs))
	for oldID := range uniqueOldCatIDs {
		if path, ok := oldIDToPath[oldID]; ok {
			if dbID, ok := pathToID[path]; ok {
				fileCatMap[oldID] = dbID
			}
		}
	}
	uniqueOldCatIDs = nil

	// Перезапускаем сканер для второго прохода
	f.Seek(0, 0)
	scanner = bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)

	var imported int
	var skipped int
	var batch []*model.Product

	type productRow struct {
		SKU         string                 `json:"sku"`
		SCU         string                 `json:"scu"`
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		CategoryID  int64                  `json:"category_id"`
		BrandID     int64                  `json:"brand_id"`
		Brand       string                 `json:"brand"`
		Price       interface{}            `json:"price"`
		StockQty    int64                  `json:"stock_qty"`
		Images      []string               `json:"images,omitempty"`
		Attributes  map[string]interface{} `json:"attributes,omitempty"`
	}

	flushBatch := func() bool {
		if len(batch) == 0 {
			return false
		}

		// Create products via CreateBatchWithIdxBuildAndOffset (no indexing)
		created, count := h.productRepo.CreateBatchWithIdxBuildAndOffset(batch, idOffset)
		imported += count

		// Check limit after flush
		if limit > 0 && imported >= limit {
			return true
		}

		// Batch create/update SCU pages from products (much faster than per-product)
		if len(created) > 0 {
			_ = h.scuPageRepo.BatchUpsertFromProducts(created)
		}

		accum := *batchAccum

		// Build indexes in accumulator
		for _, p := range created {
			docID := uint64(p.ID)

			// brand index
			if p.BrandID != 0 {
				accum.AddIndex("brand:"+strconv.FormatInt(p.BrandID, 10), docID)
				// Collect brand name
				if p.Brand != "" {
					brands[p.BrandID] = p.Brand
				}
			}

			// vendor index (company)
			if p.CompanyID != 0 {
				accum.AddIndex("vendor:"+strconv.FormatInt(p.CompanyID, 10), docID)
			}

			// category index + ancestors (pre-built cache)
			if p.CategoryID != 0 {
				ancestors := catAncestorsCache[p.CategoryID]
				if len(ancestors) == 0 {
					ancestors = []int64{p.CategoryID}
				}
				for _, cid := range ancestors {
					accum.AddIndex("cat:"+strconv.FormatInt(cid, 10), docID)
				}
			}

			// Brand as attribute "brand"
			if p.Brand != "" {
				code := "brand"
				valStr := p.Brand
				h := db.Fnv64(valStr)
				accum.AddIndex("attr:"+code+":"+strconv.FormatUint(h, 16), docID)

				// Collect for attrdef: code -> category
				if p.CategoryID != 0 {
					if codeCats[code] == nil {
						codeCats[code] = make(map[int64]struct{})
					}
					codeCats[code][p.CategoryID] = struct{}{}
				}

				// Collect for attr values ref: code -> values
				if codeValues[code] == nil {
					codeValues[code] = make(map[string]struct{})
				}
				codeValues[code][valStr] = struct{}{}

				// Collect for attr values per category: code -> catID -> values
				if p.CategoryID != 0 {
					if codeCatValues[code] == nil {
						codeCatValues[code] = make(map[int64]map[string]struct{})
					}
					if codeCatValues[code][p.CategoryID] == nil {
						codeCatValues[code][p.CategoryID] = make(map[string]struct{})
					}
					codeCatValues[code][p.CategoryID][valStr] = struct{}{}
				}
			}

			// attr index + attrdef collection
			for code, val := range p.Attributes {
				if valStr, ok := val.(string); ok && valStr != "" {
					h := db.Fnv64(valStr)
					accum.AddIndex("attr:"+code+":"+strconv.FormatUint(h, 16), docID)

					// Collect for attrdef: code -> category
					if p.CategoryID != 0 {
						if codeCats[code] == nil {
							codeCats[code] = make(map[int64]struct{})
						}
						codeCats[code][p.CategoryID] = struct{}{}
					}

					// Collect for attr values ref: code -> values
					if codeValues[code] == nil {
						codeValues[code] = make(map[string]struct{})
					}
					codeValues[code][valStr] = struct{}{}

					// Collect for attr values per category: code -> catID -> values
					if p.CategoryID != 0 {
						if codeCatValues[code] == nil {
							codeCatValues[code] = make(map[int64]map[string]struct{})
						}
						if codeCatValues[code][p.CategoryID] == nil {
							codeCatValues[code][p.CategoryID] = make(map[string]struct{})
						}
						codeCatValues[code][p.CategoryID][valStr] = struct{}{}
					}
				}
			}

			// text index
			tokens := tokenizeProduct(p)
			for _, tok := range tokens {
				accum.AddIndex("text:"+tok, docID)
			}

			// price range index
			indexPriceRanges(accum, p.Price, docID)

			// sort indexes
			accum.AddSort("sort:price_asc", idxbuild.ItemWithScore{DocID: docID, Score: p.Price})
			accum.AddSort("sort:price_desc", idxbuild.ItemWithScore{DocID: docID, Score: p.Price})
			accum.AddSort("sort:created_at_desc", idxbuild.ItemWithScore{
				DocID: docID,
				Score: float64(-p.CreatedAt.UnixNano()),
			})
		}

		// Write batch to tmp files
		if err := accum.WriteBatch("_tmp", *batchID); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED]   WARN: write batch %d: %v\n", *batchID, err)
		}
		*batchID++

		// Reset accumulator for next batch
		*batchAccum = idxbuild.NewBatchAccum()

		batch = batch[:0]
		return false
	}

	for scanner.Scan() {
		if limit > 0 && imported >= limit {
			break
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var row productRow
		if err := json.Unmarshal(line, &row); err != nil {
			skipped++
			continue
		}

		if row.SKU == "" || row.Name == "" {
			skipped++
			continue
		}

		price := parsePriceValue(row.Price)
		if price <= 0 {
			skipped++
			continue
		}

		// Map category_id: oldID -> dbID
		catID, ok := fileCatMap[row.CategoryID]
		if !ok {
			skipped++
			continue
		}

		// SCU: use explicit SCU from JSONL, or derive from SKU (base without option)
		scu := row.SCU
		if scu == "" {
			// Try to extract base SKU (before first dash/underscore)
			parts := strings.SplitN(row.SKU, "-", 2)
			scu = parts[0]
			if scu == "" {
				parts = strings.SplitN(row.SKU, "_", 2)
				scu = parts[0]
			}
		}

		// Option: append to name if SKU differs from SCU
		name := row.Name
		if scu != "" && row.SKU != scu {
			option := strings.TrimPrefix(row.SKU, scu)
			option = strings.TrimPrefix(option, "-")
			option = strings.TrimPrefix(option, "_")
			if option != "" {
				name = name + " " + formatOption(option)
			}
		}

		// Append company name suffix for uniqueness
		if companyName != "" {
			name = name + " — " + companyName
		}

		p := &model.Product{
			SKU:         row.SKU,
			SCU:         scu,
			Name:        name,
			Description: row.Description,
			CategoryID:  catID,
			CompanyID:   companyID,
			BrandID:     row.BrandID, // use brand_id from JSONL
			Brand:       row.Brand,
			Price:       price,
			Currency:    "RUB",
			StockQty:    row.StockQty,
			Status:      model.ProductStatusActive,
			Attributes:  row.Attributes,
			Images:      row.Images,
			SEO: model.ProductSEO{
				Title: fmt.Sprintf("%s — MakoShop", name),
			},
		}

		batch = append(batch, p)

		if len(batch) >= batchSize {
			if done := flushBatch(); done {
				break
			}
		}

		if imported%10000 == 0 && imported > 0 {
			fmt.Printf("[IMPORT-NORMALIZED]   %s: imported=%d skipped=%d\n",
				filepath.Base(file), imported, skipped)
		}
	}

	flushBatch()

	if err := scanner.Err(); err != nil {
		return imported, skipped, err
	}

	return imported, skipped, nil
}

// batchWriteBrands writes brand_list and brand_name:<ID> indexes.
func (h *Handlers) batchWriteBrands(store *db.Store, brands map[int64]string) error {
	var brandIDs []uint64
	for id := range brands {
		brandIDs = append(brandIDs, uint64(id))
	}

	// Write brand_list
	buf := makodb.TurboBinaryNew(brandIDs)
	if err := store.TurboWrite("brand_list", buf); err != nil {
		return fmt.Errorf("write brand_list: %w", err)
	}

	// Write brand_name:<ID>
	for id, name := range brands {
		key := "brand_name:" + strconv.FormatInt(id, 10)
		if err := store.TurboWrite(key, []byte(name)); err != nil {
			fmt.Printf("WARN: write brand_name %d: %v\n", id, err)
		}
	}

	return nil
}

// indexPriceRanges adds docID to price range indexes in the accumulator.
func indexPriceRanges(accum *idxbuild.BatchAccum, price float64, docID uint64) {
	ranges := []struct {
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
	for _, r := range ranges {
		if price >= r.min && price < r.max {
			accum.AddIndex(r.key, docID)
		}
	}
}

// tokenizeProduct extracts search tokens from product name and description.
func tokenizeProduct(p *model.Product) []string {
	text := strings.ToLower(p.Name + " " + p.Description)
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

// importNormalizedFile is the legacy single-product import method (kept for compatibility).
func (h *Handlers) importNormalizedFile(file string, limit, globalImported, batchSize int, catIDMap, brandIDMap map[int64]int64) (int, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)

	var imported int
	var skipped int
	var batchCount int

	type productRow struct {
		SKU         string                 `json:"sku"`
		SCU         string                 `json:"scu"`
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		CategoryID  int64                  `json:"category_id"`
		BrandID     int64                  `json:"brand_id"`
		Brand       string                 `json:"brand"`
		Price       interface{}            `json:"price"`
		StockQty    int64                  `json:"stock_qty"`
		Images      []string               `json:"images,omitempty"`
		Attributes  map[string]interface{} `json:"attributes,omitempty"`
	}

	for scanner.Scan() {
		if limit > 0 && globalImported+imported >= limit {
			break
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var row productRow
		if err := json.Unmarshal(line, &row); err != nil {
			skipped++
			continue
		}

		if row.SKU == "" || row.Name == "" {
			skipped++
			continue
		}

		price := parsePriceValue(row.Price)
		if price <= 0 {
			skipped++
			continue
		}

		// Map category_id and brand_id from normalized IDs to DB IDs
		catID := row.CategoryID
		if newID, ok := catIDMap[row.CategoryID]; ok {
			catID = newID
		}

		brand := row.Brand
		if row.BrandID != 0 {
			if _, ok := brandIDMap[row.BrandID]; !ok {
				// Brand not found, skip product
				skipped++
				continue
			}
		}

		// Filter images: keep only mzimg.com, add placeholder if none
		images := filterImages(row.Images)

		p := &model.Product{
			SKU:         row.SKU,
			Name:        row.Name,
			Description: row.Description,
			CategoryID:  catID,
			Brand:       brand,
			Price:       price,
			Currency:    "RUB",
			StockQty:    row.StockQty,
			Status:      model.ProductStatusActive,
			Attributes:  row.Attributes,
			Images:      images,
			SEO: model.ProductSEO{
				Title: fmt.Sprintf("%s — MakoShop", row.Name),
			},
		}

		if err := h.productRepo.Create(p); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: failed to create product %s: %v\n", row.SKU, err)
			skipped++
			continue
		}

		imported++
		batchCount++

		if batchCount >= batchSize {
			fmt.Printf("[IMPORT-NORMALIZED]   Progress: %d products imported\n", imported)
			batchCount = 0
		}
	}

	if err := scanner.Err(); err != nil {
		return imported, skipped, err
	}

	return imported, skipped, nil
}

type categoryInfo struct {
	Name       string
	ParentPath string
}

type streamFileResult struct {
	imported   int64
	skipped    int64
	categories int
}

// streamImportCSVFile imports a single CSV file in a streaming fashion.
// Does not load all rows into memory; processes row by row.
// noAttrs: if true, skip attribute parsing for speed (attributes can be filled later).
// noCats: if true, do not create categories; use existing ones via pathToID.
// pathToID: pre-built map of category paths to IDs (required when noCats=true).
// totalImported: shared atomic counter for global limit across parallel workers.
func streamImportCSVFile(
	csvFile string,
	catRepo *db.CategoryRepo,
	prodRepo *db.ProductRepo,
	companyID int64,
	limit int,
	noAttrs bool,
	noCats bool,
	pathToID map[string]int64,
	totalImported *atomic.Int64,
) (streamFileResult, error) {
	f, err := os.Open(csvFile)
	if err != nil {
		return streamFileResult{}, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return streamFileResult{}, fmt.Errorf("read header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	var localPathToID map[string]int64
	if !noCats {
		// Build incrementally when creating categories
		localPathToID = make(map[string]int64)
	}

	var imported int64
	var skipped int64

	get := func(row []string, col string) string {
		idx, ok := colIndex[col]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	for {
		row, err := reader.Read()
		if err != nil {
			break
		}

		// Apply global limit
		if limit > 0 && totalImported.Load()+imported >= int64(limit) {
			break
		}

		var catParts []string
		for _, col := range []string{"Категория", "Подкатегория 2", "Подкатегория 3", "Подкатегория 4"} {
			val := get(row, col)
			if val != "" {
				catParts = append(catParts, val)
			}
		}
		if len(catParts) == 0 {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		var catID int64
		if noCats {
			// Use pre-built pathToID, no category creation
			fullPath := strings.Join(catParts, " -> ")
			var ok bool
			catID, ok = pathToID[fullPath]
			if !ok {
				atomic.AddInt64(&skipped, 1)
				continue
			}
		} else {
			// Ensure category path exists (create if needed)
			for i := 0; i < len(catParts); i++ {
				path := strings.Join(catParts[:i+1], " -> ")
				if _, ok := localPathToID[path]; ok {
					continue
				}
				parentID := (*int64)(nil)
				if i > 0 {
					parentPath := strings.Join(catParts[:i], " -> ")
					if pid, ok := localPathToID[parentPath]; ok {
						parentID = &pid
					}
				}

				// Check for existing category by name+parent
				existingID := findCategoryByNameAndParent(catRepo, catParts[i], parentID)
				if existingID != 0 {
					localPathToID[path] = existingID
					continue
				}

				cat := &model.Category{
					Name:     catParts[i],
					ParentID: parentID,
					IsActive: true,
				}
				if err := catRepo.Create(cat); err != nil {
					// If creation fails, skip this product
					atomic.AddInt64(&skipped, 1)
					catParts = nil // invalidate category
					break
				}
				localPathToID[path] = cat.ID
			}
			if len(catParts) == 0 {
				continue
			}
			catID = localPathToID[strings.Join(catParts, " -> ")]
		}

		sku := get(row, "Артикул")
		modSku := get(row, "Артикул модификации")
		if sku == "" {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		// SCU = базовый артикул (без опций)
		scu := sku

		// SKU = уникальный артикул модификации (с опцией)
		uniqueSku := modSku
		if uniqueSku == "" {
			uniqueSku = sku
		}

		// Опция: извлекаем из Артикул модификации или из колонки Опция:*
		option := ""
		if modSku != "" && sku != "" && strings.Contains(modSku, sku) {
			// Опция — часть после базового артикула, например "-черный" из "756614-черный"
			option = strings.TrimPrefix(modSku, sku)
			option = strings.TrimPrefix(option, "-")
			option = strings.TrimPrefix(option, "_")
		}
		if option == "" {
			// Пробуем колонки Опция:*
			for _, col := range row {
				if strings.HasPrefix(col, "Опция:") || strings.HasPrefix(col, "option:") {
					// Неправильно — это заголовок, пропускаем
				}
			}
			// Ищем значение опции по индексу колонки
			for i, h := range header {
				if strings.HasPrefix(h, "Опция:") || strings.HasPrefix(h, "option:") {
					if i < len(row) && row[i] != "" {
						option = row[i]
						break
					}
				}
			}
		}
		// Форматируем опцию для названия
		optionDisplay := formatOption(option)

		name := get(row, "Имя товара")
		if name == "" {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		// Добавляем опцию к названию
		if optionDisplay != "" {
			name = name + " " + optionDisplay
		}

		price := parsePriceCSV(get(row, "Цена"))
		if price <= 0 {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		brand := get(row, "Производитель")

		description := get(row, "Краткое описание")
		if description == "" {
			description = get(row, "Описание")
		}
		if len(description) > 2000 {
			description = description[:2000]
		}

		images := parseImagesCSV(get(row, "Ссылки на фото (через пробел)"))
		if len(images) == 0 {
			images = []string{ImagePlaceholder}
		}

		stockQty := parseStockQtyCSV(get(row, "Количество"))

		var attrMap map[string]interface{}
		if !noAttrs {
			htmlAttrs := get(row, "Характеристики (HTML/Table)")
			parsedAttrs := attrs.ParseTable(htmlAttrs)
			attrMap = make(map[string]interface{})
			for code, values := range parsedAttrs {
				if len(values) > 0 {
					attrMap[code] = values[0]
				}
			}
		}

		// Create product immediately
		product := &model.Product{
			SKU:         uniqueSku,
			SCU:         scu,
			Name:        name,
			Description: description,
			CategoryID:  catID,
			CompanyID:   companyID,
			Brand:       brand,
			Price:       price,
			Currency:    "RUB",
			StockQty:    stockQty,
			Status:      model.ProductStatusActive,
			Attributes:  attrMap,
			Images:      images,
			SEO: model.ProductSEO{
				Title: fmt.Sprintf("%s — MakoShop", name),
			},
		}

		if err := prodRepo.Create(product); err != nil {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		atomic.AddInt64(&imported, 1)

		if imported%5000 == 0 {
			fmt.Printf("File %s: imported=%d\n", csvFile, imported)
		}
	}

	// After streaming: create attribute definitions for categories used in this file (only if attrs parsed)
	if !noAttrs {
		// AttrDefRepo больше не нужен — атрибуты индексируются через turbo index
		// при создании продукта. Справочник значений создаётся автоматически.
	}

	var catsCount int
	if !noCats {
		catsCount = len(localPathToID)
	}
	fmt.Printf("File %s: imported=%d skipped=%d categories=%d\n", csvFile, imported, skipped, catsCount)

	return streamFileResult{
		imported:   imported,
		skipped:    skipped,
		categories: catsCount,
	}, nil
}

func parsePriceCSV(s string) float64 {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	parts := strings.Fields(s)
	if len(parts) > 0 {
		s = parts[0]
	}
	return parsePriceString(s)
}

// parsePriceString normalizes and parses a price string.
func parsePriceString(s string) float64 {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u00a0", "")
	if s == "" {
		return 0
	}

	lastDot := strings.LastIndex(s, ".")
	lastComma := strings.LastIndex(s, ",")

	if lastDot >= 0 && lastComma >= 0 {
		if lastDot > lastComma {
			s = strings.ReplaceAll(s, ",", "")
		} else {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		}
	} else if lastComma >= 0 {
		afterComma := s[lastComma+1:]
		if len(afterComma) <= 2 {
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseImagesCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	var result []string
	for _, p := range parts {
		// Only keep mzimg.com images
		if !strings.Contains(p, "mzimg.com") {
			continue
		}
		if strings.HasPrefix(p, "http") {
			result = append(result, p)
			if len(result) >= 10 {
				break
			}
		}
	}
	// Return nil if no valid images (empty array in JSON)
	if len(result) == 0 {
		return nil
	}
	return result
}

// parsePriceValue parses price from JSON value which can be string or float64.
func parsePriceValue(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		return parsePriceString(strings.TrimSpace(val))
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

func parseStockQtyCSV(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

func ensureCompany(companyRepo *db.CompanyRepo, userRepo *db.UserRepo, name string) (*model.Company, error) {
	companies, _ := companyRepo.List()
	for _, c := range companies {
		if c.Name == name {
			return &c, nil
		}
	}

	// Find or create admin user
	adminUser, err := userRepo.GetByEmail("admin@mako.com")
	if err != nil {
		adminUser = &model.User{
			Email: "admin@mako.com",
			Role:  model.RoleAdmin,
		}
		if err := userRepo.Create(adminUser, "admin123"); err != nil {
			return nil, err
		}
	}

	company := &model.Company{
		Name:        name,
		OwnerUserID: adminUser.ID,
		Settings: model.CompanySettings{
			Currency:   "RUB",
			VatEnabled: false,
		},
		Status: model.CompanyStatusVerified,
	}

	if err := companyRepo.Create(company); err != nil {
		return nil, err
	}

	return company, nil
}

func walkCSVFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".csv") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// filterImages keeps only mzimg.com images and adds a placeholder if none remain.
func filterImages(images []string) []string {
	var result []string
	for _, img := range images {
		if strings.Contains(img, "mzimg.com") {
			result = append(result, img)
		}
	}
	if len(result) == 0 {
		return []string{ImagePlaceholder}
	}
	return result
}

// importMultiCompany imports products from multiple companies.
// Structure: _tmp/prices/{company_name}/*.csv
// Each subdirectory name becomes the company name.
func (h *Handlers) importMultiCompany(w http.ResponseWriter, r *http.Request) {
	fmt.Println("[IMPORT-MULTI] Starting multi-company import...")
	startTime := time.Now()

	inputDir := "_tmp/prices"
	limit := 0
	noAttrs := r.URL.Query().Get("no_attrs") == "1"
	noCats := r.URL.Query().Get("no_cats") == "1"
	workers := 8

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("workers"); v != "" {
		workers, _ = strconv.Atoi(v)
	}
	if workers < 1 {
		workers = 1
	}

	// Build pathToID if no_cats=1 (use existing categories)
	var pathToID map[string]int64
	if noCats {
		fmt.Println("[IMPORT-MULTI] no_cats=1: building category path map from existing categories...")
		var err error
		pathToID, err = h.categoryRepo.BuildPathMap()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("build path map: %v", err))
			return
		}
		fmt.Printf("[IMPORT-MULTI] Found %d existing category paths\n", len(pathToID))
	}

	// List subdirectories (each is a company)
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("read dir: %v", err))
		return
	}

	var companyDirs []string
	for _, e := range entries {
		if e.IsDir() {
			companyDirs = append(companyDirs, e.Name())
		}
	}
	sort.Strings(companyDirs)

	if len(companyDirs) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "no_company_dirs",
			"message": "No company subdirectories found in _tmp/prices/. Create directories like _tmp/prices/{company_name}/",
		})
		return
	}

	fmt.Printf("[IMPORT-MULTI] Found %d company directories: %v\n", len(companyDirs), companyDirs)

	// Process each company
	type companyResult struct {
		Company   string `json:"company"`
		CompanyID int64  `json:"company_id"`
		Imported  int64  `json:"imported"`
		Skipped   int64  `json:"skipped"`
		Error     string `json:"error,omitempty"`
	}

	var results []companyResult
	var totalImported int64
	var totalSkipped int64

	for _, companyName := range companyDirs {
		fmt.Printf("[IMPORT-MULTI] Processing company: %s\n", companyName)

		// Ensure company exists
		company, err := ensureCompany(h.companyRepo, h.userRepo, companyName)
		if err != nil {
			results = append(results, companyResult{
				Company: companyName,
				Error:   fmt.Sprintf("ensure company: %v", err),
			})
			continue
		}

		// Get CSV files for this company
		companyDir := filepath.Join(inputDir, companyName)
		csvFiles, err := filepath.Glob(filepath.Join(companyDir, "*.csv"))
		if err != nil {
			csvFiles, _ = walkCSVFiles(companyDir)
		}
		sort.Strings(csvFiles)

		if len(csvFiles) == 0 {
			fmt.Printf("[IMPORT-MULTI]   No CSV files for %s\n", companyName)
			results = append(results, companyResult{
				Company:   companyName,
				CompanyID: company.ID,
				Imported:  0,
				Skipped:   0,
			})
			continue
		}

		fmt.Printf("[IMPORT-MULTI]   Found %d CSV files\n", len(csvFiles))

		// Import using existing stream import logic
		var companyImported int64
		var companySkipped int64

		var totalImportedForCompany atomic.Int64
		var wg sync.WaitGroup
		fileSem := make(chan struct{}, workers)

		type fileResult struct {
			file     string
			imported int64
			skipped  int64
			error    error
		}

		resultsCh := make(chan fileResult, len(csvFiles))

		for _, csvFile := range csvFiles {
			wg.Add(1)
			go func(file string) {
				defer wg.Done()
				fileSem <- struct{}{}
				defer func() { <-fileSem }()

				fileLimit := 0
				if limit > 0 {
					fileLimit = limit
				}

				res, err := streamImportCSVFile(
					file,
					h.categoryRepo,
					h.productRepo,
					company.ID,
					fileLimit,
					noAttrs,
					noCats,
					pathToID,
					&totalImportedForCompany,
				)
				resultsCh <- fileResult{
					file:     file,
					imported: res.imported,
					skipped:  res.skipped,
					error:    err,
				}
			}(csvFile)
		}

		go func() {
			wg.Wait()
			close(resultsCh)
		}()

		var fileError string
		for res := range resultsCh {
			companyImported += res.imported
			companySkipped += res.skipped
			if res.error != nil && fileError == "" {
				fileError = res.error.Error()
			}
			fmt.Printf("[IMPORT-MULTI]   %s: imported=%d skipped=%d\n",
				filepath.Base(res.file), res.imported, res.skipped)
		}

		totalImported += companyImported
		totalSkipped += companySkipped

		results = append(results, companyResult{
			Company:   companyName,
			CompanyID: company.ID,
			Imported:  companyImported,
			Skipped:   companySkipped,
			Error:     fileError,
		})

		fmt.Printf("[IMPORT-MULTI]   %s total: imported=%d skipped=%d\n",
			companyName, companyImported, companySkipped)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[IMPORT-MULTI] Complete: total_imported=%d total_skipped=%d time=%v\n",
		totalImported, totalSkipped, elapsed)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "ok",
		"total_imported": totalImported,
		"total_skipped":  totalSkipped,
		"time":           elapsed.String(),
		"companies":      results,
	})
}

// formatOption formats an option string for display in product name.
// Examples:
//
//	"черный" -> "черный"
//	"64gb" -> "64 ГБ"
//	"черный-64gb" -> "черный, 64 ГБ"
func formatOption(option string) string {
	if option == "" {
		return ""
	}
	// Replace separators with comma-space
	option = strings.ReplaceAll(option, "-", ", ")
	option = strings.ReplaceAll(option, "_", ", ")
	// Capitalize first letter of each part
	parts := strings.Split(option, ", ")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Common unit replacements
		p = strings.ReplaceAll(p, "gb", "ГБ")
		p = strings.ReplaceAll(p, "g", "г")
		p = strings.ReplaceAll(p, "mm", "мм")
		p = strings.ReplaceAll(p, "cm", "см")
		p = strings.ReplaceAll(p, "kg", "кг")
		p = strings.ReplaceAll(p, "l", "л")
		// Capitalize first letter
		if len(p) > 0 {
			runes := []rune(p)
			runes[0] = unicode.ToUpper(runes[0])
			p = string(runes)
		}
		parts[i] = p
	}
	// Remove empty parts
	var cleaned []string
	for _, p := range parts {
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, ", ")
}

// HandleAdminRebuildSortIndexes rebuilds all sort indexes from existing products.
// POST /admin/rebuild-sort-indexes
// This is an emergency operation that reads all products directly (allowed by rules).
func (h *Handlers) HandleAdminRebuildSortIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	fmt.Println("[REBUILD-SORT] Starting sort index rebuild...")
	startTime := time.Now()

	db := h.productRepo.TurboSearch()
	if db == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "turbo search not initialized")
		return
	}

	// Get all product IDs from next_id state (emergency direct read)
	nextIDData, _ := h.productRepo.Store().DB().TurboRawRead("state:next_id:product")
	var maxID int64
	if len(nextIDData) > 0 {
		_, _ = fmt.Sscanf(string(nextIDData), "%d", &maxID)
	}
	fmt.Printf("[REBUILD-SORT] Max product ID: %d\n", maxID)

	// Collect all valid product IDs
	var allIDs []uint64
	for id := int64(1); id < maxID; id++ {
		_, err := h.productRepo.Get(id)
		if err == nil {
			allIDs = append(allIDs, uint64(id))
		}
		if len(allIDs)%50000 == 0 && len(allIDs) > 0 {
			fmt.Printf("[REBUILD-SORT] Collected %d product IDs\n", len(allIDs))
		}
	}
	fmt.Printf("[REBUILD-SORT] Total products: %d\n", len(allIDs))

	if len(allIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no_products"})
		return
	}

	// Build sort items
	type sortItem struct {
		DocID uint64
		Price float64
		Time  int64
	}

	var priceAsc []sortItem
	var priceDesc []sortItem
	var timeDesc []sortItem

	// Read products in batches
	batchSize := 10000
	for i := 0; i < len(allIDs); i += batchSize {
		end := i + batchSize
		if end > len(allIDs) {
			end = len(allIDs)
		}

		for _, docID := range allIDs[i:end] {
			p, err := h.productRepo.Get(int64(docID))
			if err != nil {
				continue
			}

			item := sortItem{
				DocID: docID,
				Price: p.Price,
				Time:  p.CreatedAt.UnixNano(),
			}

			priceAsc = append(priceAsc, item)
			priceDesc = append(priceDesc, item)
			timeDesc = append(timeDesc, item)
		}

		fmt.Printf("[REBUILD-SORT] Processed %d/%d products\n", i+batchSize, len(allIDs))
	}

	// Sort price_asc
	sort.Slice(priceAsc, func(i, j int) bool {
		if priceAsc[i].Price != priceAsc[j].Price {
			return priceAsc[i].Price < priceAsc[j].Price
		}
		return priceAsc[i].DocID < priceAsc[j].DocID
	})

	// Sort price_desc
	sort.Slice(priceDesc, func(i, j int) bool {
		if priceDesc[i].Price != priceDesc[j].Price {
			return priceDesc[i].Price > priceDesc[j].Price
		}
		return priceDesc[i].DocID < priceDesc[j].DocID
	})

	// Sort created_at_desc
	sort.Slice(timeDesc, func(i, j int) bool {
		if timeDesc[i].Time != timeDesc[j].Time {
			return timeDesc[i].Time > timeDesc[j].Time
		}
		return timeDesc[i].DocID < timeDesc[j].DocID
	})

	// Write sort indexes
	writeSortIndex := func(name string, items []sortItem) error {
		docIDs := make([]uint64, len(items))
		for i, item := range items {
			docIDs[i] = item.DocID
		}
		return db.DB().TurboPutSortIndex(name, docIDs)
	}

	if err := writeSortIndex("sort:price_asc", priceAsc); err != nil {
		fmt.Printf("[REBUILD-SORT] WARN: price_asc: %v\n", err)
	}
	if err := writeSortIndex("sort:price_desc", priceDesc); err != nil {
		fmt.Printf("[REBUILD-SORT] WARN: price_desc: %v\n", err)
	}
	if err := writeSortIndex("sort:created_at_desc", timeDesc); err != nil {
		fmt.Printf("[REBUILD-SORT] WARN: created_at_desc: %v\n", err)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("[REBUILD-SORT] Completed in %v\n", elapsed)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"products": len(allIDs),
		"time":     elapsed.String(),
	})
}

// toSlugTranslit creates a URL-friendly slug from a string with Cyrillic transliteration.
func toSlugTranslit(s string) string {
	// Transliterate Cyrillic to Latin using map
	translitMap := map[rune]string{
		'А': "a", 'Б': "b", 'В': "v", 'Г': "g", 'Д': "d", 'Е': "e", 'Ё': "e", 'Ж': "zh",
		'З': "z", 'И': "i", 'Й': "y", 'К': "k", 'Л': "l", 'М': "m", 'Н': "n", 'О': "o",
		'П': "p", 'Р': "r", 'С': "s", 'Т': "t", 'У': "u", 'Ф': "f", 'Х': "kh", 'Ц': "ts",
		'Ч': "ch", 'Ш': "sh", 'Щ': "shch", 'Ъ': "", 'Ы': "y", 'Ь': "", 'Э': "e", 'Ю': "yu", 'Я': "ya",
		'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
		'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
		'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
		'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	}

	var result strings.Builder
	for _, r := range s {
		if t, ok := translitMap[r]; ok {
			result.WriteString(t)
		} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			result.WriteString("-")
		}
	}

	// Collapse multiple hyphens
	slug := result.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")

	return strings.ToLower(slug)
}
