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

	"github.com/GenshIv/makoshop/internal/attrs"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/idxbuild"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/slug"
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

// HandleAdminImportPrices imports products from CSV files in _tmp/prices,
// normalized JSONL files in _tmp/normalized, JSON price files, or Nokaut XML
// price files.
// POST /admin/import-prices
// Query params:
//   - source=csv|normalized|multi|json|nokaut   data source (default: csv)
//     csv       - single company from _tmp/prices/*.csv
//     normalized - from _tmp/normalized/ (single company)
//     multi     - multi-company from _tmp/prices/{company_name}/*.csv
//     json      - JSON price file (productHeader + products array)
//     nokaut    - Nokaut XML price file from company ImportURL (alias: xml)
//   - limit=N                 max products to import (per company in multi mode)
//   - company=NAME            company name for csv/normalized mode (default: "Magazilla Import")
//   - no_attrs=1              skip attribute parsing (csv mode only)
//   - workers=N               parallel workers (default: 8)
//   - use_existing_cats=1     use existing categories by name (default: 1)
func (h *Handlers) HandleAdminImportPrices(w http.ResponseWriter, r *http.Request) {
	fmt.Println("[IMPORT] HandleAdminImportPrices called, method=", r.Method)
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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

	if source == "json" {
		noDownload := r.URL.Query().Get("no_download") == "1"
		companyParam := r.URL.Query().Get("company")
		limit := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, _ = strconv.Atoi(v)
		}
		h.HandleAdminImportJSON(w, r, companyParam, limit, noDownload)
		return
	}

	if source == "nokaut" || source == "xml" {
		// Nokaut XML price files (same handler as POST /admin/import-nokaut)
		h.HandleAdminImportNokaut(w, r)
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
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("ensure company: %v", err))
		return
	}

	csvFiles, err := filepath.Glob(filepath.Join(inputDir, "*.csv"))
	if err != nil {
		csvFiles, _ = walkCSVFiles(inputDir)
	}
	sort.Strings(csvFiles)

	if len(csvFiles) == 0 {
		httpres.WriteJSON(w, http.StatusOK, ImportPricesResult{Status: "no_files"})
		return
	}

	// Build pathToID if no_cats=1 (use existing categories)
	var pathToID map[string]int64
	if noCats {
		fmt.Println("[IMPORT-CSV] no_cats=1: building category path map from existing categories...")
		pathToID, err = h.categoryRepo.BuildPathMap()
		if err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("build path map: %v", err))
			return
		}
		fmt.Printf("[IMPORT-CSV] Found %d existing category paths\n", len(pathToID))
	}

	type fileResult struct {
		file       string
		imported   int64
		skipped    int64
		categories int
		products   []*model.Product
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
				products:   res.products,
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
	var allProducts []*model.Product

	for res := range resultsCh {
		if res.err != nil {
			fmt.Printf("WARN: import file %s error: %v\n", res.file, res.err)
			continue
		}
		finalImported += res.imported
		finalSkipped += res.skipped
		finalCategories += res.categories
		allProducts = append(allProducts, res.products...)
	}

	if finalImported == 0 && finalSkipped == 0 {
		httpres.WriteJSON(w, http.StatusOK, ImportPricesResult{Status: "no_products"})
		return
	}

	// ============================================
	// Phase 2: Batch index products
	// ============================================
	fmt.Printf("[IMPORT-CSV] Phase 2: Indexing %d products...\n", len(allProducts))
	phase2Start := time.Now()
	if h.productRepo.TurboSearch() != nil && len(allProducts) > 0 {
		if err := h.productRepo.TurboSearch().IndexProductBatch(allProducts); err != nil {
			fmt.Printf("[IMPORT-CSV] WARN: index products: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-CSV] Phase 2: Products indexed in %v\n", time.Since(phase2Start))

	// ============================================
	// Phase 3: Batch upsert EAN pages + index
	// ============================================
	fmt.Println("[IMPORT-CSV] Phase 3: EAN pages...")
	phase3Start := time.Now()
	if err := h.eanPageRepo.LoadCatalogizerCache(); err != nil {
		fmt.Printf("[IMPORT-CSV] WARN: load catalogizer cache: %v\n", err)
	}
	h.eanPageRepo.BatchUpsertFromProducts(allProducts)
	if h.eanPageSearch != nil {
		allSCUs, _ := h.eanPageRepo.ListAll()
		eanPtrs := make([]*model.EANPage, len(allSCUs))
		for i := range allSCUs {
			eanPtrs[i] = &allSCUs[i]
		}
		if err := h.eanPageSearch.IndexEANPageBatch(eanPtrs); err != nil {
			fmt.Printf("[IMPORT-CSV] WARN: index EAN pages: %v\n", err)
		}
		// Build sort indexes for EAN pages (required for catalog/EAN pages to show products)
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-CSV] WARN: build EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-CSV] Phase 3: EAN pages done in %v\n", time.Since(phase3Start))

	// ============================================
	// Phase 4: Rebuild sort indexes (batch)
	// ============================================
	fmt.Println("[IMPORT-CSV] Phase 4: Rebuilding sort indexes...")
	phase4Start := time.Now()
	if h.productRepo.TurboSearch() != nil {
		if err := h.productRepo.TurboSearch().BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-CSV] WARN: rebuild sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-CSV] Phase 4: Sort indexes rebuilt in %v\n", time.Since(phase4Start))

	// Phase 5: Recalculate EAN page product counts
	fmt.Println("[IMPORT-CSV] Phase 5: Recalculating EAN page product counts...")
	phase5Start := time.Now()
	if err := h.eanPageRepo.RecalculateProductCounts(); err != nil {
		fmt.Printf("[IMPORT-CSV] WARN: recalculate product counts: %v\n", err)
	}
	fmt.Printf("[IMPORT-CSV] Phase 5: Product counts recalculated in %v\n", time.Since(phase5Start))

	// Phase 6: Recalculate EAN page min prices
	fmt.Println("[IMPORT-CSV] Phase 6: Recalculating EAN page min prices...")
	phase6Start := time.Now()
	if err := h.eanPageRepo.RecalculateMinPrices(h.productRepo); err != nil {
		fmt.Printf("[IMPORT-CSV] WARN: recalculate min prices: %v\n", err)
	}
	fmt.Printf("[IMPORT-CSV] Phase 6: Min prices recalculated in %v\n", time.Since(phase6Start))

	// Recalculate delivery_method attributes on EAN pages (from products' companies)
	fmt.Println("[IMPORT-CSV] Recalculating delivery method attributes...")
	dmStart := time.Now()
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.RecalculateDeliveryMethods(h.companyRepo, h.deliveryMethodRepo); err != nil {
			fmt.Printf("[IMPORT-CSV] WARN: recalculate delivery methods: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-CSV] Delivery method attributes recalculated in %v\n", time.Since(dmStart))

	// Phase 7: Rebuild EAN page sort indexes (to reflect updated min prices)
	fmt.Println("[IMPORT-CSV] Phase 7: Rebuilding EAN page sort indexes...")
	phase7Start := time.Now()
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-CSV] WARN: rebuild EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-CSV] Phase 7: EAN page sort indexes rebuilt in %v\n", time.Since(phase7Start))

	// Phase 8: Rebuild category tree (required for categories to appear in lists)
	fmt.Println("[IMPORT-CSV] Phase 8: Rebuilding category trees...")
	phase8Start := time.Now()
	h.categoryRepo.RebuildTrees()
	fmt.Printf("[IMPORT-CSV] Phase 8: Category trees rebuilt in %v\n", time.Since(phase8Start))

	httpres.WriteJSON(w, http.StatusOK, ImportPricesResult{
		Status:           "completed",
		Categories:       finalCategories,
		ProductsImported: int(finalImported),
		ProductsSkipped:  int(finalSkipped),
		Brands:           0,
	})
}

// importNormalized imports products from _tmp/normalized using phased approach:
// Phase 0: collect brands + attr codes → save in DB once
// Phase 1: parse + batch create products (no indexes)
// Phase 2: batch upsert EAN pages
// Phase 3: batch index products + EAN pages
// Phase 4: train catalogizer
// Query params:
//   - company=NAME            company name (default: "Magazilla Import")
//   - limit=N                 max products to import
//   - id_offset=N             add this offset to each product ID (for multi-company imports)
//
// NOTE: categories are NOT processed here — handled by catalogizer only.
func (h *Handlers) importNormalized(w http.ResponseWriter, r *http.Request) {
	inputDir := "_tmp/normalized"
	limit := 0
	companyName := "Magazilla Import"
	idOffset := int64(0)

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("company"); v != "" {
		companyName = v
	}
	if v := r.URL.Query().Get("id_offset"); v != "" {
		idOffset, _ = strconv.ParseInt(v, 10, 64)
	}

	fmt.Println("[IMPORT-NORMALIZED] Starting phased import from", inputDir)
	fmt.Printf("[IMPORT-NORMALIZED] Config: limit=%d company=%s id_offset=%d\n", limit, companyName, idOffset)
	startTime := time.Now()

	// Ensure company
	company, err := ensureCompany(h.companyRepo, h.userRepo, companyName)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("ensure company: %v", err))
		return
	}
	fmt.Printf("[IMPORT-NORMALIZED] Using company: %s (ID=%d)\n", company.Name, company.ID)

	// Auto-calculate idOffset based on company.ID if not explicitly set
	if idOffset == 0 {
		idOffset = company.ID * 1_000_000_000
		fmt.Printf("[IMPORT-NORMALIZED] Auto-calculated id_offset=%d for company ID=%d\n", idOffset, company.ID)
	}

	// Find product files
	productFiles, err := filepath.Glob(filepath.Join(inputDir, "products-*.jsonl"))
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("glob: %v", err))
		return
	}
	sort.Strings(productFiles)
	if len(productFiles) == 0 {
		httpres.WriteJSON(w, http.StatusOK, ImportPricesResult{Status: "no_files"})
		return
	}
	fmt.Printf("[IMPORT-NORMALIZED] Found %d product files\n", len(productFiles))

	// ============================================
	// Phase 0: Collect brands + attr codes → save in DB once
	// ============================================
	fmt.Println("[IMPORT-NORMALIZED] Phase 0: Collecting brands and attr codes...")
	phase0Start := time.Now()

	brands := make(map[int64]string)       // brandID -> name
	attrCodes := make(map[string]struct{}) // attr code -> exists

	for _, file := range productFiles {
		if err := h.collectBrandsAndAttrs(file, brands, attrCodes); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: collect from %s: %v\n", file, err)
		}
	}

	// Save brands in DB (one shot)
	fmt.Printf("[IMPORT-NORMALIZED] Saving %d brands to DB...\n", len(brands))
	if err := h.batchWriteBrands(h.store, brands); err != nil {
		fmt.Printf("[IMPORT-NORMALIZED] WARN: save brands: %v\n", err)
	}

	// Save attr codes in DB (one shot)
	fmt.Printf("[IMPORT-NORMALIZED] Saving %d attr codes to DB...\n", len(attrCodes))
	for code := range attrCodes {
		_, _ = h.attrDefRepo.GetOrCreate(code)
	}

	fmt.Printf("[IMPORT-NORMALIZED] Phase 0: done in %v\n", time.Since(phase0Start))

	// ============================================
	// Phase 1: Parse + batch create products (no indexes)
	// ============================================
	fmt.Println("[IMPORT-NORMALIZED] Phase 1: Parsing and creating products...")
	phase1Start := time.Now()

	var allProducts []*model.Product
	var skipped int
	var imported int

	for _, file := range productFiles {
		if limit > 0 && imported >= limit {
			break
		}
		prods, skip, err := h.parseProductsFile(file, company.ID, company.Name, idOffset, limit-imported)
		if err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: parse file %s: %v\n", file, err)
			continue
		}
		allProducts = append(allProducts, prods...)
		skipped += skip
		imported += len(prods)
		fmt.Printf("[IMPORT-NORMALIZED]   %s: parsed=%d skipped=%d\n",
			filepath.Base(file), len(prods), skip)
	}
	fmt.Printf("[IMPORT-NORMALIZED] Phase 1: parsed %d products in %v\n", len(allProducts), time.Since(phase1Start))

	if len(allProducts) == 0 {
		httpres.WriteJSON(w, http.StatusOK, ImportPricesResult{Status: "no_products"})
		return
	}

	// Batch create products (no indexes)
	fmt.Println("[IMPORT-NORMALIZED] Creating products in DB (batch, no indexes)...")
	createStart := time.Now()
	createdProducts, createdCount := h.productRepo.CreateBatchWithIdxBuildAndOffset(allProducts, idOffset)
	fmt.Printf("[IMPORT-NORMALIZED] Created %d products in %v\n", createdCount, time.Since(createStart))

	if createdCount == 0 {
		httpres.WriteJSON(w, http.StatusOK, ImportPricesResult{Status: "no_products_created"})
		return
	}

	// ============================================
	// Phase 2: Batch upsert EAN pages
	// ============================================
	fmt.Println("[IMPORT-NORMALIZED] Phase 2: Batch upserting EAN pages...")
	phase2Start := time.Now()

	// Load catalogizer cache ONCE before batch upsert
	if err := h.eanPageRepo.LoadCatalogizerCache(); err != nil {
		fmt.Printf("[IMPORT-NORMALIZED] WARN: load catalogizer cache: %v (will use slow path)\n", err)
	}

	productToSCU := h.eanPageRepo.BatchUpsertFromProducts(createdProducts)
	fmt.Printf("[IMPORT-NORMALIZED] Phase 2: EAN pages processed in %v (mapped %d products)\n",
		time.Since(phase2Start), len(productToSCU))

	// ============================================
	// Phase 3: Batch index products + EAN pages
	// ============================================
	fmt.Println("[IMPORT-NORMALIZED] Phase 3: Building indexes...")
	phase3Start := time.Now()

	// Index products in batch
	if h.productRepo.TurboSearch() != nil {
		if err := h.productRepo.TurboSearch().IndexProductBatch(createdProducts); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: index products: %v\n", err)
		}
	}

	// Index EAN pages in batch
	if h.eanPageSearch != nil {
		allSCUs, _ := h.eanPageRepo.ListAll()
		eanPtrs := make([]*model.EANPage, len(allSCUs))
		for i := range allSCUs {
			eanPtrs[i] = &allSCUs[i]
		}
		if err := h.eanPageSearch.IndexEANPageBatch(eanPtrs); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: index EAN pages: %v\n", err)
		}
		// Build sort indexes for EAN pages (required for catalog/EAN pages to show products)
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: build EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-NORMALIZED] Phase 3: Indexes built in %v\n", time.Since(phase3Start))

	// Phase 4: Recalculate EAN page product counts
	fmt.Println("[IMPORT-NORMALIZED] Phase 4: Recalculating EAN page product counts...")
	phase4Start := time.Now()
	if err := h.eanPageRepo.RecalculateProductCounts(); err != nil {
		fmt.Printf("[IMPORT-NORMALIZED] WARN: recalculate product counts: %v\n", err)
	}
	fmt.Printf("[IMPORT-NORMALIZED] Phase 4: Product counts recalculated in %v\n", time.Since(phase4Start))

	// Phase 5: Recalculate EAN page min prices
	fmt.Println("[IMPORT-NORMALIZED] Phase 5: Recalculating EAN page min prices...")
	phase5Start := time.Now()
	if err := h.eanPageRepo.RecalculateMinPrices(h.productRepo); err != nil {
		fmt.Printf("[IMPORT-NORMALIZED] WARN: recalculate min prices: %v\n", err)
	}
	fmt.Printf("[IMPORT-NORMALIZED] Phase 5: Min prices recalculated in %v\n", time.Since(phase5Start))

	// Recalculate delivery_method attributes on EAN pages (from products' companies)
	fmt.Println("[IMPORT-NORMALIZED] Recalculating delivery method attributes...")
	dmStart := time.Now()
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.RecalculateDeliveryMethods(h.companyRepo, h.deliveryMethodRepo); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: recalculate delivery methods: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-NORMALIZED] Delivery method attributes recalculated in %v\n", time.Since(dmStart))

	// Phase 6: Rebuild EAN page sort indexes (to reflect updated min prices)
	fmt.Println("[IMPORT-NORMALIZED] Phase 6: Rebuilding EAN page sort indexes...")
	phase6Start := time.Now()
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-NORMALIZED] WARN: rebuild EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-NORMALIZED] Phase 6: EAN page sort indexes rebuilt in %v\n", time.Since(phase6Start))

	// Phase 7: Rebuild category tree (required for categories to appear in lists)
	fmt.Println("[IMPORT-NORMALIZED] Phase 7: Rebuilding category trees...")
	phase7Start := time.Now()
	h.categoryRepo.RebuildTrees()
	fmt.Printf("[IMPORT-NORMALIZED] Phase 7: Category trees rebuilt in %v\n", time.Since(phase7Start))

	elapsed := time.Since(startTime)
	fmt.Printf("[IMPORT-NORMALIZED] Completed: created=%d skipped=%d time=%v (%.0f products/sec)\n",
		createdCount, skipped, elapsed, float64(createdCount)/elapsed.Seconds())

	httpres.WriteJSON(w, http.StatusOK, ImportPricesResult{
		Status:           "completed",
		ProductsImported: createdCount,
		ProductsSkipped:  skipped,
	})
}

// collectBrandsAndAttrs scans a products-*.jsonl file and collects brands + attr codes.
func (h *Handlers) collectBrandsAndAttrs(file string, brands map[int64]string, attrCodes map[string]struct{}) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)

	type productRow struct {
		BrandID    int64                  `json:"brand_id"`
		Brand      string                 `json:"brand"`
		Attributes map[string]interface{} `json:"attributes,omitempty"`
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var row productRow
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		if row.BrandID != 0 && row.Brand != "" {
			brands[row.BrandID] = row.Brand
		}
		for code := range row.Attributes {
			attrCodes[code] = struct{}{}
		}
	}
	return scanner.Err()
}

// parseProductsFile reads a products-*.jsonl file and returns product structs ready for creation.
func (h *Handlers) parseProductsFile(file string, companyID int64, companyName string, idOffset int64, limit int) ([]*model.Product, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)

	var products []*model.Product
	var skipped int

	type productRow struct {
		EAN         string           `json:"ean"`
		Name        string           `json:"name"`
		Description string           `json:"description"`
		CategoryID  int64            `json:"category_id"`
		BrandID     int64            `json:"brand_id"`
		Brand       string           `json:"brand"`
		Price       interface{}      `json:"price"`
		StockQty    int64            `json:"stock_qty"`
		Images      []string         `json:"images,omitempty"`
		Attributes  []model.KeyValue `json:"attributes,omitempty"`
	}

	for scanner.Scan() {
		if limit > 0 && len(products) >= limit {
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

		if row.EAN == "" || row.Name == "" {
			skipped++
			continue
		}

		price := parsePriceValue(row.Price)
		if price <= 0 {
			skipped++
			continue
		}

		// EAN: use explicit EAN or derive from EAN
		ean := row.EAN
		if ean == "" {
			parts := strings.SplitN(row.EAN, "-", 2)
			ean = parts[0]
			if ean == "" {
				parts = strings.SplitN(row.EAN, "_", 2)
				ean = parts[0]
			}
		}

		// Option: append to name if EAN differs from EAN
		name := row.Name
		if ean != "" && row.EAN != ean {
			option := strings.TrimPrefix(row.EAN, ean)
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
			EAN:         ean,
			Name:        name,
			Description: row.Description,
			CategoryID:  0, // handled by catalogizer
			CompanyID:   companyID,
			BrandID:     row.BrandID,
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

		products = append(products, p)
	}

	if err := scanner.Err(); err != nil {
		return products, skipped, err
	}

	return products, skipped, nil
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
			nameRu := row.Name
			nameEn := row.Name // пока дублируем, slug считаем из nameEn
			c := &model.Category{
				NameRu:   nameRu,
				NameEn:   nameEn,
				Slug:     slug.Slug(nameEn),
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
		catName := c.NameEn
		if catName == "" {
			catName = c.NameRu
		}
		if catName == name {
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
// NOTE: categories are NOT processed here — handled by catalogizer only.
func (h *Handlers) importNormalizedFileBatched(
	file string,
	limit, batchSize int,
	batchAccum **idxbuild.BatchAccum,
	batchID *int,
	codeCats map[string]map[int64]struct{},
	codeValues map[string]map[string]struct{},
	codeCatValues map[string]map[int64]map[string]struct{},
	brands map[int64]string,
	companyID int64,
	companyName string,
	idOffset int64,
) (int, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)

	var imported int
	var skipped int
	var batch []*model.Product
	var allCreated []*model.Product // collect all new products for batch EAN page upsert

	type productRow struct {
		EAN         string           `json:"ean"`
		Name        string           `json:"name"`
		Description string           `json:"description"`
		CategoryID  int64            `json:"category_id"`
		BrandID     int64            `json:"brand_id"`
		Brand       string           `json:"brand"`
		Price       interface{}      `json:"price"`
		StockQty    int64            `json:"stock_qty"`
		Images      []string         `json:"images,omitempty"`
		Attributes  []model.KeyValue `json:"attributes,omitempty"`
	}

	flushBatch := func() bool {
		if len(batch) == 0 {
			return false
		}

		var created []*model.Product

		// Use GetOrCreateByKey to avoid duplicate products (EAN+company+attrs)
		for _, p := range batch {
			id, isNew, err := h.productRepo.GetOrCreateByKey(p)
			if err != nil {
				skipped++
				continue
			}

			if isNew {
				// Get the created product for indexing
				if prod, err := h.productRepo.Get(id); err == nil {
					created = append(created, prod)
					allCreated = append(allCreated, prod) // collect for batch EAN upsert
					docID := "product:" + strconv.FormatInt(prod.ID, 10)

					// product_list
					(*batchAccum).AddIndex("product_list", docID)

					// brand
					if prod.BrandID != 0 {
						(*batchAccum).AddIndex("brand:"+strconv.FormatInt(prod.BrandID, 10), docID)
						if prod.Brand != "" {
							brands[prod.BrandID] = prod.Brand
						}
					}

					// vendor
					if prod.CompanyID != 0 {
						(*batchAccum).AddIndex("vendor:"+strconv.FormatInt(prod.CompanyID, 10), docID)
					}

					// category
					if prod.CategoryID != 0 {
						(*batchAccum).AddIndex("cat:"+strconv.FormatInt(prod.CategoryID, 10), docID)
					}

					// attributes
					for _, kv := range prod.Attributes {
						valStr := kv.Value
						if valStr != "" {
							(*batchAccum).AddIndex("attr:"+kv.Key+":"+valStr, docID)
						}
					}

					// text
					for _, tok := range tokenizeProduct(prod) {
						(*batchAccum).AddIndex("text:"+tok, docID)
					}

					// EAN index (name-based key for products without EAN)
					if eanKey := db.ProductEANIndexKey(prod); eanKey != "" {
						(*batchAccum).AddIndex("ean:"+eanKey, docID)
					}
				}
				imported++
			} else {
				// Existing product updated (price), count it
				imported++
			}

			if limit > 0 && imported >= limit {
				batch = batch[:0]
				return true
			}
		}
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

		if row.EAN == "" || row.Name == "" {
			skipped++
			continue
		}

		price := parsePriceValue(row.Price)
		if price <= 0 {
			skipped++
			continue
		}

		// Map category_id: oldID -> dbID
		// Category handled by catalogizer only
		catID := int64(0)

		// EAN: use explicit EAN from JSONL, or derive from EAN (base without option)
		ean := row.EAN
		if ean == "" {
			// Try to extract base EAN (before first dash/underscore)
			parts := strings.SplitN(row.EAN, "-", 2)
			ean = parts[0]
			if ean == "" {
				parts = strings.SplitN(row.EAN, "_", 2)
				ean = parts[0]
			}
		}

		// Option: append to name if EAN differs from EAN
		name := row.Name
		if ean != "" && row.EAN != ean {
			option := strings.TrimPrefix(row.EAN, ean)
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
			EAN:         ean,
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

	// Batch upsert EAN pages from all created products (category via catalogizer only for new EAN pages)
	if len(allCreated) > 0 {
		fmt.Printf("[IMPORT-NORMALIZED] Batch upserting EAN pages for %d products...\n", len(allCreated))
		_ = h.eanPageRepo.BatchUpsertFromProducts(allCreated)
		fmt.Printf("[IMPORT-NORMALIZED] EAN pages batch upsert done.\n")
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
	//buf := makodb.TurboBinaryNew(brandIDs)
	//if err := store.TurboWrite("brand_list", buf); err != nil {
	//	return fmt.Errorf("write brand_list: %w", err)
	//}

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
func indexPriceRanges(accum *idxbuild.BatchAccum, price float64, docID string) {
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
		EAN         string           `json:"ean"`
		Name        string           `json:"name"`
		Description string           `json:"description"`
		CategoryID  int64            `json:"category_id"`
		BrandID     int64            `json:"brand_id"`
		Brand       string           `json:"brand"`
		Price       interface{}      `json:"price"`
		StockQty    int64            `json:"stock_qty"`
		Images      []string         `json:"images,omitempty"`
		Attributes  []model.KeyValue `json:"attributes,omitempty"`
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

		if row.EAN == "" || row.Name == "" {
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
			EAN:         row.EAN,
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
			fmt.Printf("[IMPORT-NORMALIZED] WARN: failed to create product %s: %v\n", row.EAN, err)
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
	products   []*model.Product
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

	// Collect products for batch create (no immediate indexing)
	const batchSize = 1000
	var batch []*model.Product
	var allProducts []*model.Product

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		created, count := prodRepo.CreateBatchWithIdxBuild(batch)
		allProducts = append(allProducts, created...)
		imported += int64(count)
		batch = batch[:0]
		return nil
	}

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

		// Determine category ID:
		// New format: category_id column (direct ID)
		// Old format: Категория + Подкатегория N (path-based)
		var catID int64
		catIDStr := get(row, "category_id")
		if catIDStr != "" {
			// New format: direct category ID
			id, err := strconv.ParseInt(catIDStr, 10, 64)
			if err != nil || id <= 0 {
				atomic.AddInt64(&skipped, 1)
				continue
			}
			// Verify category exists
			if _, err := catRepo.Get(id); err != nil {
				atomic.AddInt64(&skipped, 1)
				continue
			}
			catID = id
		} else {
			// Old format: path-based categories
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

			if noCats {
				fullPath := strings.Join(catParts, " -> ")
				var ok bool
				catID, ok = pathToID[fullPath]
				if !ok {
					atomic.AddInt64(&skipped, 1)
					continue
				}
			} else {
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
					existingID := findCategoryByNameAndParent(catRepo, catParts[i], parentID)
					if existingID != 0 {
						localPathToID[path] = existingID
						continue
					}
					nameRu := catParts[i]
					nameEn := catParts[i]
					cat := &model.Category{
						NameRu:   nameRu,
						NameEn:   nameEn,
						Slug:     slug.Slug(nameEn),
						ParentID: parentID,
						IsActive: true,
					}
					if err := catRepo.Create(cat); err != nil {
						atomic.AddInt64(&skipped, 1)
						catParts = nil
						break
					}
					localPathToID[path] = cat.ID
				}
				if len(catParts) == 0 {
					continue
				}
				catID = localPathToID[strings.Join(catParts, " -> ")]
			}
		}

		// EAN: new format "sku" or old "Артикул"
		sku := get(row, "sku")
		if sku == "" {
			sku = get(row, "Артикул")
		}
		modSku := get(row, "sku_mod")
		if modSku == "" {
			modSku = get(row, "Артикул модификации")
		}
		if sku == "" {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		// EAN = базовый артикул (без опций)
		ean := sku

		// EAN = уникальный артикул модификации (с опцией)
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

		// Name: new format "name" or old "Имя товара"
		name := get(row, "name")
		if name == "" {
			name = get(row, "Имя товара")
		}
		if name == "" {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		// Добавляем опцию к названию
		if optionDisplay != "" {
			name = name + " " + optionDisplay
		}

		// Price: new format "price" or old "Цена"
		priceStr := get(row, "price")
		if priceStr == "" {
			priceStr = get(row, "Цена")
		}
		price := parsePriceCSV(priceStr)
		if price <= 0 {
			atomic.AddInt64(&skipped, 1)
			continue
		}

		// Brand: new format "brand" or old "Производитель"
		brand := get(row, "brand")
		if brand == "" {
			brand = get(row, "Производитель")
		}

		// Description: new format "description" or old "Краткое описание"/"Описание"
		description := get(row, "description")
		if description == "" {
			description = get(row, "Краткое описание")
		}
		if description == "" {
			description = get(row, "Описание")
		}
		if len(description) > 2000 {
			description = description[:2000]
		}

		// Images: new format "images" or old "Ссылки на фото (через пробел)"
		imagesStr := get(row, "images")
		if imagesStr == "" {
			imagesStr = get(row, "Ссылки на фото (через пробел)")
		}
		images := parseImagesCSV(imagesStr)
		if len(images) == 0 {
			images = []string{ImagePlaceholder}
		}

		// Stock: new format "stock_qty" or old "Количество"
		stockStr := get(row, "stock_qty")
		if stockStr == "" {
			stockStr = get(row, "Количество")
		}
		stockQty := parseStockQtyCSV(stockStr)

		var attrKV []model.KeyValue
		if !noAttrs {
			// New format: read attr_* columns
			for _, col := range header {
				if strings.HasPrefix(col, "attr_") {
					code := strings.TrimPrefix(col, "attr_")
					val := get(row, col)
					if val != "" {
						// Skip attribute values longer than 40 runes
						if len([]rune(val)) <= 40 {
							attrKV = append(attrKV, model.KeyValue{Key: code, Value: fmt.Sprintf("%v", parseAttrValue(code, val))})
						}
					}
				}
			}

			// Old format: parse HTML table (fallback)
			if len(attrKV) == 0 {
				htmlAttrs := get(row, "Характеристики (HTML/Table)")
				parsedAttrs := attrs.ParseTable(htmlAttrs)
				for code, values := range parsedAttrs {
					if len(values) > 0 {
						attrKV = append(attrKV, model.KeyValue{Key: code, Value: values[0]})
					}
				}
			}
		}

		// Brand is a first-class attribute: always add it so the catalog can
		// filter by it (attr.brand=<value>), even when noAttrs is set.
		// Skip if an attr_brand column already provided it.
		if brand != "" {
			hasBrand := false
			for _, kv := range attrKV {
				if kv.Key == "brand" {
					hasBrand = true
					break
				}
			}
			if !hasBrand {
				attrKV = append(attrKV, model.KeyValue{Key: "brand", Value: brand})
			}
		}

		// Add to batch
		product := &model.Product{
			EAN:         ean,
			Name:        name,
			Description: description,
			CategoryID:  catID,
			CompanyID:   companyID,
			Brand:       brand,
			Price:       price,
			Currency:    "RUB",
			StockQty:    stockQty,
			Status:      model.ProductStatusActive,
			Attributes:  attrKV,
			Images:      images,
			SEO: model.ProductSEO{
				Title: fmt.Sprintf("%s — MakoShop", name),
			},
		}
		batch = append(batch, product)

		// Flush batch when full
		if len(batch) >= batchSize {
			if err := flushBatch(); err != nil {
				return streamFileResult{}, err
			}
		}

		if imported%5000 == 0 {
			fmt.Printf("File %s: buffered=%d\n", csvFile, imported)
		}
	}

	// Flush remaining batch
	if err := flushBatch(); err != nil {
		return streamFileResult{}, err
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
		products:   allProducts,
	}, nil
}

// parseAttrValue parses attribute value string to appropriate Go type.
// Returns int for pure integers, float64 for decimals, bool for true/false, string otherwise.
func parseAttrValue(code, val string) interface{} {
	val = strings.TrimSpace(val)
	if val == "" {
		return val
	}

	// Try bool
	if strings.EqualFold(val, "true") || strings.EqualFold(val, "yes") || val == "1" {
		if strings.HasSuffix(code, "_bool") || strings.Contains(code, "has_") || code == "is_active" || code == "has_net" {
			return true
		}
	}
	if strings.EqualFold(val, "false") || strings.EqualFold(val, "no") || val == "0" {
		if strings.HasSuffix(code, "_bool") || strings.Contains(code, "has_") || code == "is_active" || code == "has_net" {
			return false
		}
	}

	// Try int
	if i, err := strconv.ParseInt(val, 10, 64); err == nil {
		return int(i)
	}

	// Try float
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return f
	}

	// Default: string
	return val
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
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("build path map: %v", err))
			return
		}
		fmt.Printf("[IMPORT-MULTI] Found %d existing category paths\n", len(pathToID))
	}

	// List subdirectories (each is a company)
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("read dir: %v", err))
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
		httpres.WriteJSON(w, http.StatusOK, map[string]string{
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

	// Collect all products across all companies for batch indexing
	var allProducts []*model.Product
	var allProductsMu sync.Mutex

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
			products []*model.Product
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
					products: res.products,
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
			// Collect products
			if len(res.products) > 0 {
				allProductsMu.Lock()
				allProducts = append(allProducts, res.products...)
				allProductsMu.Unlock()
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

	// ============================================
	// Phase 2: Batch index all products (creates ean:{ean} indexes)
	// ============================================
	fmt.Printf("[IMPORT-MULTI] Phase 2: Indexing %d products...\n", len(allProducts))
	phase2Start := time.Now()
	if h.productRepo.TurboSearch() != nil && len(allProducts) > 0 {
		if err := h.productRepo.TurboSearch().IndexProductBatch(allProducts); err != nil {
			fmt.Printf("[IMPORT-MULTI] WARN: index products: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-MULTI] Phase 2: Products indexed in %v\n", time.Since(phase2Start))

	// ============================================
	// Phase 3: Batch upsert EAN pages + index
	// ============================================
	fmt.Println("[IMPORT-MULTI] Phase 3: EAN pages...")
	phase3Start := time.Now()
	if err := h.eanPageRepo.LoadCatalogizerCache(); err != nil {
		fmt.Printf("[IMPORT-MULTI] WARN: load catalogizer cache: %v\n", err)
	}
	h.eanPageRepo.BatchUpsertFromProducts(allProducts)
	if h.eanPageSearch != nil {
		allSCUs, _ := h.eanPageRepo.ListAll()
		eanPtrs := make([]*model.EANPage, len(allSCUs))
		for i := range allSCUs {
			eanPtrs[i] = &allSCUs[i]
		}
		if err := h.eanPageSearch.IndexEANPageBatch(eanPtrs); err != nil {
			fmt.Printf("[IMPORT-MULTI] WARN: index EAN pages: %v\n", err)
		}
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-MULTI] WARN: build EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-MULTI] Phase 3: EAN pages done in %v\n", time.Since(phase3Start))

	// Phase 4: Recalculate EAN page product counts
	fmt.Println("[IMPORT-MULTI] Phase 4: Recalculating EAN page product counts...")
	phase4Start := time.Now()
	if err := h.eanPageRepo.RecalculateProductCounts(); err != nil {
		fmt.Printf("[IMPORT-MULTI] WARN: recalculate product counts: %v\n", err)
	}
	fmt.Printf("[IMPORT-MULTI] Phase 4: Product counts recalculated in %v\n", time.Since(phase4Start))

	// Phase 5: Recalculate EAN page min prices
	fmt.Println("[IMPORT-MULTI] Phase 5: Recalculating EAN page min prices...")
	phase5Start := time.Now()
	if err := h.eanPageRepo.RecalculateMinPrices(h.productRepo); err != nil {
		fmt.Printf("[IMPORT-MULTI] WARN: recalculate min prices: %v\n", err)
	}
	fmt.Printf("[IMPORT-MULTI] Phase 5: Min prices recalculated in %v\n", time.Since(phase5Start))

	// Recalculate delivery_method attributes on EAN pages (from products' companies)
	fmt.Println("[IMPORT-MULTI] Recalculating delivery method attributes...")
	dmStart := time.Now()
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.RecalculateDeliveryMethods(h.companyRepo, h.deliveryMethodRepo); err != nil {
			fmt.Printf("[IMPORT-MULTI] WARN: recalculate delivery methods: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-MULTI] Delivery method attributes recalculated in %v\n", time.Since(dmStart))

	// Phase 6: Rebuild EAN page sort indexes (to reflect updated min prices)
	fmt.Println("[IMPORT-MULTI] Phase 6: Rebuilding EAN page sort indexes...")
	phase6Start := time.Now()
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-MULTI] WARN: rebuild EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-MULTI] Phase 6: EAN page sort indexes rebuilt in %v\n", time.Since(phase6Start))

	// Phase 7: Rebuild category tree (required for categories to appear in lists)
	fmt.Println("[IMPORT-MULTI] Phase 7: Rebuilding category trees...")
	phase7Start := time.Now()
	h.categoryRepo.RebuildTrees()
	fmt.Printf("[IMPORT-MULTI] Phase 7: Category trees rebuilt in %v\n", time.Since(phase7Start))

	elapsed := time.Since(startTime)
	fmt.Printf("[IMPORT-MULTI] Complete: total_imported=%d total_skipped=%d time=%v\n",
		totalImported, totalSkipped, elapsed)

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
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
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	fmt.Println("[REBUILD-SORT] Starting sort index rebuild...")
	startTime := time.Now()

	db := h.productRepo.TurboSearch()
	if db == nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "turbo search not initialized")
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
		httpres.WriteJSON(w, http.StatusOK, map[string]string{"status": "no_products"})
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

	// Read products in batches and collect for batch reindex
	batchSize := 10000
	for i := 0; i < len(allIDs); i += batchSize {
		end := i + batchSize
		if end > len(allIDs) {
			end = len(allIDs)
		}

		var batchProducts []*model.Product
		for _, docID := range allIDs[i:end] {
			p, err := h.productRepo.Get(int64(docID))
			if err != nil {
				continue
			}

			batchProducts = append(batchProducts, p)

			item := sortItem{
				DocID: docID,
				Price: p.Price,
				Time:  p.CreatedAt * 1e9,
			}

			priceAsc = append(priceAsc, item)
			priceDesc = append(priceDesc, item)
			timeDesc = append(timeDesc, item)
		}

		// Batch reindex all products in this batch
		if len(batchProducts) > 0 {
			if err := h.productRepo.TurboSearch().IndexProductBatch(batchProducts); err != nil {
				fmt.Printf("[REBUILD-SORT] WARN: batch reindex %d products: %v\n", len(batchProducts), err)
			}
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
		docIDs := make([]string, len(items))
		for i, item := range items {
			docIDs[i] = strconv.Itoa(int(item.DocID))
		}
		return db.DB().TurboPutSortIndexString(name, docIDs)
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

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"products": len(allIDs),
		"time":     elapsed.String(),
	})
}
