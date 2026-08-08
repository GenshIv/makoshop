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
//   - source=csv|normalized   data source (default: csv)
//   - limit=N                 max products to import
//   - company=NAME            company name (default: "Magazilla Import")
//   - no_attrs=1              skip attribute parsing (csv mode only)
//   - workers=N               parallel workers (default: 8)
func (h *Handlers) HandleAdminImportPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	source := r.URL.Query().Get("source")
	if source == "" {
		source = "csv"
	}

	if source == "normalized" {
		h.importNormalized(w, r)
		return
	}

	// CSV import (existing logic)
	inputDir := "_tmp/prices"
	limit := 0
	companyName := "Magazilla Import"
	noAttrs := false
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

// importNormalized imports categories and products from _tmp/normalized
// using batch indexing via idxbuild (no TurboPutIndex in import loop).
func (h *Handlers) importNormalized(w http.ResponseWriter, r *http.Request) {
	inputDir := "_tmp/normalized"
	tmpDir := "_tmp"
	limit := 0
	batchSize := 100_000

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("batch"); v != "" {
		batchSize, _ = strconv.Atoi(v)
	}
	if batchSize < 10_000 {
		batchSize = 10_000
	}

	fmt.Println("[IMPORT-NORMALIZED] Starting batch import from", inputDir)
	fmt.Printf("[IMPORT-NORMALIZED] Config: limit=%d batchSize=%d\n", limit, batchSize)
	startTime := time.Now()

	// Step 1: Import categories
	fmt.Println("[IMPORT-NORMALIZED] Importing categories...")
	catFile := filepath.Join(inputDir, "categories.jsonl")
	pathToID, oldIDToPath, catCount, err := h.importCategories(catFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("import categories: %v", err))
		return
	}
	fmt.Printf("[IMPORT-NORMALIZED] Imported %d categories\n", catCount)

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

	// Global SKU set for deduplication
	seenSKUs := make(map[string]struct{})

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
			file, remaining, batchSize, pathToID, oldIDToPath, seenSKUs,
			&accum, &batchID, codeCats, codeValues, codeCatValues, brands,
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
		if err := idxbuild.MergeSortIndexes(db.DB(), tmpDir); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: merge sort indexes: %v\n", err)
		}
		if err := idxbuild.CleanupTmp(tmpDir); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: cleanup tmp: %v\n", err)
		}

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
			if err := h.batchWriteBrands(db.DB(), brands); err != nil {
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
		Categories:       catCount,
		ProductsImported: totalImported,
		ProductsSkipped:  totalSkipped,
		Brands:           0,
	})
}

func (h *Handlers) importCategories(file string) (map[string]int64, map[int64]string, int, error) {
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

		// Создаём новую
		c := &model.Category{
			Name:     row.Name,
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
// seenSKUs: global dedup set
// batchAccum: pointer to batch accumulator (can be reset)
// batchID: pointer to current batch ID (incremented on flush)
// codeCats: global attr code -> category set (populated during import)
// codeValues: global attr code -> value set (populated during import)
// codeCatValues: global attr code -> catID -> value set (populated during import)
// brands: global brandID -> name (populated during import)
func (h *Handlers) importNormalizedFileBatched(
	file string,
	limit, batchSize int,
	pathToID map[string]int64,
	oldIDToPath map[int64]string,
	seenSKUs map[string]struct{},
	batchAccum **idxbuild.BatchAccum,
	batchID *int,
	codeCats map[string]map[int64]struct{},
	codeValues map[string]map[string]struct{},
	codeCatValues map[string]map[int64]map[string]struct{},
	brands map[int64]string,
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
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		CategoryID  int64                  `json:"category_id"`
		BrandID     int64                  `json:"brand_id"`
		Brand       string                 `json:"brand"`
		Price       float64                `json:"price"`
		StockQty    int64                  `json:"stock_qty"`
		Images      []string               `json:"images,omitempty"`
		Attributes  map[string]interface{} `json:"attributes,omitempty"`
	}

	flushBatch := func() bool {
		if len(batch) == 0 {
			return false
		}

		// Create products via CreateBatchWithIdxBuild (no indexing)
		created, count := h.productRepo.CreateBatchWithIdxBuild(batch)
		imported += count

		// Check limit after flush
		if limit > 0 && imported >= limit {
			return true
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

			// category index + ancestors
			if p.CategoryID != 0 {
				ancestors, err := h.turboSearch.GetCategoryAncestors(p.CategoryID)
				if err != nil || len(ancestors) == 0 {
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

		if row.SKU == "" || row.Name == "" || row.Price <= 0 {
			skipped++
			continue
		}

		// Deduplicate by SKU
		if _, seen := seenSKUs[row.SKU]; seen {
			skipped++
			continue
		}
		seenSKUs[row.SKU] = struct{}{}

		// Map category_id: oldID -> dbID
		catID, ok := fileCatMap[row.CategoryID]
		if !ok {
			skipped++
			continue
		}

		p := &model.Product{
			SKU:         row.SKU,
			Name:        row.Name,
			Description: row.Description,
			CategoryID:  catID,
			BrandID:     row.BrandID, // use brand_id from JSONL
			Brand:       row.Brand,
			Price:       row.Price,
			Currency:    "RUB",
			StockQty:    row.StockQty,
			Status:      model.ProductStatusActive,
			Attributes:  row.Attributes,
			Images:      row.Images,
			SEO: model.ProductSEO{
				Title: fmt.Sprintf("%s — MakoShop", row.Name),
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
func (h *Handlers) batchWriteBrands(db *makodb.ShardedDB, brands map[int64]string) error {
	var brandIDs []uint64
	for id := range brands {
		brandIDs = append(brandIDs, uint64(id))
	}

	// Write brand_list
	buf := makodb.TurboBinaryNew(brandIDs)
	if err := db.TurboRawWrite("brand_list", buf); err != nil {
		return fmt.Errorf("write brand_list: %w", err)
	}

	// Write brand_name:<ID>
	for id, name := range brands {
		key := "brand_name:" + strconv.FormatInt(id, 10)
		if err := db.TurboRawWrite(key, []byte(name)); err != nil {
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
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		CategoryID  int64                  `json:"category_id"`
		BrandID     int64                  `json:"brand_id"`
		Brand       string                 `json:"brand"`
		Price       float64                `json:"price"`
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

		if row.SKU == "" || row.Name == "" || row.Price <= 0 {
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
			Price:       row.Price,
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
// totalImported: shared atomic counter for global limit across parallel workers.
func streamImportCSVFile(
	csvFile string,
	catRepo *db.CategoryRepo,
	prodRepo *db.ProductRepo,
	companyID int64,
	limit int,
	noAttrs bool,
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

	// pathToID is built incrementally
	pathToID := make(map[string]int64)
	// attrCodesByCatID collected for this file, then bulk-created after stream
	attrCodesByCatID := make(map[int64]map[string]struct{})

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

		// Ensure category path exists
		for i := 0; i < len(catParts); i++ {
			path := strings.Join(catParts[:i+1], " -> ")
			if _, ok := pathToID[path]; ok {
				continue
			}
			parentID := (*int64)(nil)
			if i > 0 {
				parentPath := strings.Join(catParts[:i], " -> ")
				if pid, ok := pathToID[parentPath]; ok {
					parentID = &pid
				}
			}

			// Check for existing category by name+parent
			existingID := findCategoryByNameAndParent(catRepo, catParts[i], parentID)
			if existingID != 0 {
				pathToID[path] = existingID
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
			pathToID[path] = cat.ID
		}
		if len(catParts) == 0 {
			continue
		}

		sku := get(row, "Артикул")
		modSku := get(row, "Артикул модификации")
		if sku == "" {
			atomic.AddInt64(&skipped, 1)
			continue
		}
		uniqueSku := modSku
		if uniqueSku == "" {
			uniqueSku = sku
		}

		name := get(row, "Имя товара")
		if name == "" {
			atomic.AddInt64(&skipped, 1)
			continue
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
		catPath := strings.Join(catParts, " -> ")
		catID := pathToID[catPath]

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
	if !noAttrs && attrCodesByCatID != nil {
		// AttrDefRepo больше не нужен — атрибуты индексируются через turbo index
		// при создании продукта. Справочник значений создаётся автоматически.
		_ = attrCodesByCatID
	}

	fmt.Printf("File %s: imported=%d skipped=%d categories=%d\n", csvFile, imported, skipped, len(pathToID))

	return streamFileResult{
		imported:   imported,
		skipped:    skipped,
		categories: len(pathToID),
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
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
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
