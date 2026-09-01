package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/pricesrc"
)

// JSONImportResult holds the result of a JSON price import operation.
type JSONImportResult struct {
	Status          string `json:"status"`
	Company         string `json:"company,omitempty"`
	Files           int    `json:"files"`
	OffersParsed    int    `json:"offers_parsed"`
	ProductsCreated int    `json:"products_created"`
	ProductsUpdated int    `json:"products_updated"`
	ProductsSkipped int    `json:"products_skipped"`
	ProductsDeleted int    `json:"products_deleted"`
}

// downloadJSONPriceFile downloads the company's JSON price file from company.ImportURL
// and saves it to prices/<company>.json. Returns the saved file path.
// Always deletes the old file first to ensure a fresh download.
func downloadJSONPriceFile(company *model.Company) (string, error) {
	// Determine file extension from URL (robust to query/matrix params), default .json
	ext := priceFileExt(company.ImportURL, ".json")

	name := sanitizeFileName(company.Name)
	if name == "" {
		name = fmt.Sprintf("company_%d", company.ID)
	}
	destPath := filepath.Join(pricesDir, name+ext)

	if err := os.MkdirAll(pricesDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	// Delete old file first to ensure fresh download
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("[IMPORT-JSON] WARN: remove old file %s: %v\n", destPath, err)
	} else {
		fmt.Printf("[IMPORT-JSON] Removed old file %s\n", destPath)
	}

	// Large price files can take many minutes to download on a slow link. Use a
	// generous overall cap so a legitimate slow download completes instead of
	// being aborted, while still bounding a truly stuck transfer.
	// ResponseHeaderTimeout fails fast if the server never starts responding.
	client := &http.Client{
		Timeout:   15 * time.Minute,
		Transport: &http.Transport{ResponseHeaderTimeout: 60 * time.Second},
	}
	resp, err := client.Get(company.ImportURL)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %s", resp.Status)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}

	return destPath, nil
}

// HandleAdminImportJSON imports offers from JSON price files.
// It is idempotent: existing products are updated in place, never duplicated.
//
// Called from /admin/import-prices?source=json or directly from /admin/import-json
// Query params:
//   - company=ID_OR_NAME   import only this company (default: all companies with ImportURL or ImportFolder set)
//   - limit=N              max offers to import per company (0 = unlimited)
//   - no_download=1        skip download, use local file from ImportFolder
func (h *Handlers) HandleAdminImportJSON(w http.ResponseWriter, r *http.Request, companyParam string, limit int, noDownload bool) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Prevent concurrent imports
	if !h.importMu.TryLock() {
		httpres.WriteError(w, http.StatusConflict, "IMPORT_IN_PROGRESS", "Another import is already in progress")
		return
	}
	defer h.importMu.Unlock()

	startTime := time.Now()
	fmt.Printf("[IMPORT-JSON] Starting (company=%q limit=%d no_download=%t) from %s\n", companyParam, limit, noDownload, r.RemoteAddr)

	// Resolve target companies
	var companies []model.Company
	all, err := h.companyRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("list companies: %v", err))
		return
	}

	if companyParam != "" {
		// Resolve by ID or by name
		cid, parseErr := strconv.ParseInt(companyParam, 10, 64)
		for _, c := range all {
			if parseErr == nil && c.ID == cid {
				companies = append(companies, c)
				break
			}
			if strings.EqualFold(c.Name, companyParam) {
				companies = append(companies, c)
				break
			}
		}
		if len(companies) == 0 {
			httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("company not found: %s", companyParam))
			return
		}
	} else {
		// All companies that have an ImportURL or ImportFolder configured
		for _, c := range all {
			if c.ImportURL != "" || c.ImportFolder != "" {
				companies = append(companies, c)
			}
		}
	}

	if len(companies) == 0 {
		httpres.WriteJSON(w, http.StatusOK, JSONImportResult{Status: "no_companies"})
		return
	}

	// Track live progress for this run.
	h.importProgress.Begin(len(companies))
	defer h.importProgress.Finish()

	// Import each company
	for i := range companies {
		company := &companies[i]
		fmt.Printf("[IMPORT-JSON] Importing company: %s (ID=%d, url=%q, folder=%q)\n", company.Name, company.ID, company.ImportURL, company.ImportFolder)

		h.importProgress.SetCompany(i+1, company.Name, "json")
		result := JSONImportResult{}
		result = h.importJSONCompany(company, limit, noDownload, "")

		state := companyResultState(CompanyImportResult{Status: result.Status})
		errMsg := ""
		if state == CompanyStateFailed {
			errMsg = result.Status
		}
		h.importProgress.CompanyDone(i+1, state, errMsg)

		fmt.Printf("[IMPORT-JSON] Company %s: parsed=%d created=%d updated=%d skipped=%d\n",
			company.Name, result.OffersParsed, result.ProductsCreated, result.ProductsUpdated, result.ProductsSkipped)

		// Write progress for single-company imports
		if len(companies) == 1 {
			result.Company = company.Name
			httpres.WriteJSON(w, http.StatusOK, result)
		}
	}

	// Global recalculation: run ONCE after all companies (not per company).
	if err := h.runGlobalRecalculation(); err != nil {
		fmt.Printf("[IMPORT-JSON] WARN: global recalculation: %v\n", err)
	}

	fmt.Printf("[IMPORT-JSON] Completed in %v\n", time.Since(startTime))
}

// jsonPriceFile represents the top-level JSON price file structure.
type jsonPriceFile struct {
	ProductHeader map[string]interface{} `json:"productHeader"`
	Products      []jsonProductFileItem  `json:"products"`
}

// jsonPriceEntry represents a single product in the JSON price file.
type jsonProductFileItem struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Brand        string                 `json:"brand"`
	Offers       []jsonOfferFileItem    `json:"offers"`
	Categories   []jsonCategoryFileItem `json:"categories"`
	ProductImage jsonImageFileItem      `json:"productImage"`
}

// jsonOfferFileItem represents an offer within a product.
type jsonOfferFileItem struct {
	ID           string           `json:"id"`
	ProductURL   string           `json:"productUrl"`
	PriceHistory []jsonPriceEntry `json:"priceHistory"`
}

// jsonPriceEntry represents a price entry.
type jsonPriceEntry struct {
	Date  int64     `json:"date"`
	Price jsonMoney `json:"price"`
}

// jsonMoney represents a price with currency.
type jsonMoney struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

// jsonCategoryFileItem represents a category.
type jsonCategoryFileItem struct {
	Name string `json:"name"`
}

// jsonImageFileItem represents an image.
type jsonImageFileItem struct {
	URL string `json:"url"`
}

// importJSONCompany imports all offers for a single company from its JSON price file.
//
// The import is organized in phases and uses an application-level transaction
// (see TRANSACTIONAL_IMPORT.md): all writes are buffered in memory and applied
// atomically on Commit, and every index is written in a single batch per key so
// the storage is not churned by per-product writes (no vacuum storm).
//
// Phases:
//   - Phase 0:   batch-create new attribute definitions (outside transaction)
//   - Phase 1:   batch create/update products (in transaction)
//   - Phase 1.6: delete stale products not present in the file (in transaction)
//   - Phase 1.5: batch-index products (in transaction)
//   - Phase 2:   batch upsert + index EAN pages (in transaction)
//   - Phase 3:   recalculate EAN page counts + min prices (in transaction)
//   - Phase 4:   build product sort indexes (in transaction)
//   - Phase 5:   rebuild category trees (in transaction)
//   - Commit, then post-commit: rebuild trees, EAN page sort indexes,
//     delivery method attributes
func (h *Handlers) importJSONCompany(company *model.Company, limit int, noDownload bool, explicitFile string) JSONImportResult {
	cfg := company.PriceSource
	applyPriceSourceDefaults(&cfg)

	// Always use settings.currency as the primary currency source
	currency := company.Settings.Currency
	if currency == "" {
		currency = "PLN"
	}

	var files []string

	if explicitFile != "" {
		// The caller already downloaded the file (the unified importer downloads
		// once and detects the format from the content before dispatching).
		files = []string{explicitFile}
	} else {
		h.importProgress.SetStep(StepDownload)
		if noDownload {
			// Use local file from ImportFolder without downloading
			importFolder := company.ImportFolder
			importFolder = strings.TrimPrefix(importFolder, "/")
			importFolder = strings.TrimSuffix(importFolder, "/")
			importFolder = strings.TrimPrefix(importFolder, "prices/")

			dir := filepath.Join(pricesDir, importFolder)
			var err error
			files, err = filepath.Glob(filepath.Join(dir, "*.json"))
			if err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: glob %s: %v\n", dir, err)
				return JSONImportResult{Status: "no_files"}
			}
			sort.Strings(files)
			if len(files) == 0 {
				fmt.Printf("[IMPORT-JSON] WARN: no JSON files in %s\n", dir)
				return JSONImportResult{Status: "no_files"}
			}
			fmt.Printf("[IMPORT-JSON] Using local JSON files from %s (%d files)\n", dir, len(files))
		} else if company.ImportURL != "" {
			// Download the JSON price file from the URL (default: always re-download)
			destPath, err := downloadJSONPriceFile(company)
			if err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: download JSON price from %s: %v\n", company.ImportURL, err)
				return JSONImportResult{Status: "download_error"}
			}
			files = []string{destPath}
			fmt.Printf("[IMPORT-JSON] Downloaded JSON price file to %s\n", destPath)
		} else {
			// Fallback: read JSON files from the company's import folder
			importFolder := company.ImportFolder
			importFolder = strings.TrimPrefix(importFolder, "/")
			importFolder = strings.TrimSuffix(importFolder, "/")
			importFolder = strings.TrimPrefix(importFolder, "prices/")

			dir := filepath.Join(pricesDir, importFolder)
			var err error
			files, err = filepath.Glob(filepath.Join(dir, "*.json"))
			if err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: glob %s: %v\n", dir, err)
				return JSONImportResult{Status: "no_files"}
			}
			sort.Strings(files)
			if len(files) == 0 {
				fmt.Printf("[IMPORT-JSON] WARN: no JSON files in %s\n", dir)
				return JSONImportResult{Status: "no_files"}
			}
			fmt.Printf("[IMPORT-JSON] Found %d JSON files in %s\n", len(files), dir)
		}
	}

	// Pre-load attribute definitions to avoid repeated DB hits
	attrDefCache := make(map[string]*model.AttrDef)
	newAttrKeys := make(map[string]struct{})
	if h.attrDefRepo != nil {
		if attrDefs, err := h.attrDefRepo.List(); err == nil {
			for i := range attrDefs {
				for _, key := range attrDefs[i].Keys {
					attrDefCache[key] = &attrDefs[i]
				}
			}
			fmt.Printf("[IMPORT-JSON] Pre-loaded %d attribute definitions (%d keys)\n", len(attrDefs), len(attrDefCache))
		}
	}

	var result JSONImportResult
	result.Files = len(files)

	var allParsedProducts []*model.Product
	var allParsedNames []string

	h.importProgress.SetStep(StepParse)
	for _, file := range files {
		if limit > 0 && result.OffersParsed >= limit {
			break
		}

		f, err := os.Open(file)
		if err != nil {
			fmt.Printf("[IMPORT-JSON] WARN: open %s: %v\n", file, err)
			continue
		}

		fileStart := time.Now()
		fmt.Printf("[IMPORT-JSON] Parsing %s (file %d/%d)...\n", filepath.Base(file), len(files), len(files))

		var fileParsed int
		var fileSkipped int

		// First pass: parse JSON and collect all products
		var parsedProducts []*model.Product
		var parsedNames []string

		var jsonData jsonPriceFile
		if err := json.NewDecoder(f).Decode(&jsonData); err != nil {
			fmt.Printf("[IMPORT-JSON] WARN: decode %s: %v\n", file, err)
			f.Close()
			continue
		}
		f.Close()

		fmt.Printf("[IMPORT-JSON] Loaded %d products from %s\n", len(jsonData.Products), filepath.Base(file))

		for _, jp := range jsonData.Products {
			if limit > 0 && result.OffersParsed >= limit {
				break
			}

			// Parse the JSON product into a model.Product
			prod, skip, err := parseJSONProductForImport(jp, company.ID, company.Name, currency, attrDefCache, newAttrKeys, h.attrDefRepo)
			if err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: parse product %s: %v\n", jp.Name, err)
				fileSkipped++
				result.ProductsSkipped++
				continue
			}
			if skip {
				fileSkipped++
				result.ProductsSkipped++
				continue
			}
			if prod != nil {
				parsedProducts = append(parsedProducts, prod)
				parsedNames = append(parsedNames, prod.Name)
				result.OffersParsed++
				fileParsed++
				h.importProgress.AddParsed(1)
			}
		}

		fmt.Printf("[IMPORT-JSON] Parsed %d products from %s in %v (skipped=%d)\n",
			fileParsed, filepath.Base(file), time.Since(fileStart), fileSkipped)

		allParsedProducts = append(allParsedProducts, parsedProducts...)
		allParsedNames = append(allParsedNames, parsedNames...)
	}

	// ============================================
	// Phase 0: Batch create new attribute definitions (OUTSIDE TRANSACTION)
	// ============================================
	// Creates AttrDef for all new attribute keys in one batch to avoid vacuum.
	h.importProgress.SetStep(StepAttrDefs)
	if len(newAttrKeys) > 0 && h.attrDefRepo != nil {
		fmt.Printf("[IMPORT-JSON] Phase 0: Creating %d new attribute definitions (batch)...\n", len(newAttrKeys))
		keys := make([]string, 0, len(newAttrKeys))
		for key := range newAttrKeys {
			keys = append(keys, key)
		}
		if created, err := h.attrDefRepo.BatchGetOrCreateByKeys(keys); err != nil {
			fmt.Printf("[IMPORT-JSON] WARN: batch create attribute definitions: %v\n", err)
		} else {
			for key, ad := range created {
				attrDefCache[key] = ad
			}
		}
	}

	// ============================================
	// Phase 1: Batch create/update products (IN TRANSACTION)
	// ============================================
	var allProducts []*model.Product

	if len(allParsedProducts) > 0 {
		fmt.Printf("[IMPORT-JSON] Phase 1: Creating/updating %d products (transactional)...\n", len(allParsedProducts))
		h.importProgress.SetStep(StepProducts)
		phase1Start := time.Now()

		// Create application-level transaction BEFORE any writes
		txn := db.NewTransaction(h.store)
		if err := txn.Begin(); err != nil {
			fmt.Printf("[IMPORT-JSON] ERROR: failed to begin transaction: %v\n", err)
			result.Status = "error_transaction"
			return result
		}
		defer func() {
			if !txn.IsFinished() {
				_ = txn.Abort()
			}
		}()

		// Process in batches to avoid filling up the transaction buffer
		const batchSize = 100_000
		for i := 0; i < len(allParsedProducts); i += batchSize {
			end := i + batchSize
			if end > len(allParsedProducts) {
				end = len(allParsedProducts)
			}

			batchProducts := allParsedProducts[i:end]
			batchNames := allParsedNames[i:end]

			fmt.Printf("[IMPORT-JSON] Phase 1: Processing batch %d-%d (%d products)...\n", i, end, len(batchProducts))

			ids, isNewMap, err := h.productRepo.BatchGetOrCreateByEANTx(txn, batchProducts, batchNames)
			if err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: BatchGetOrCreateByEANTx: %v\n", err)
				result.ProductsSkipped += len(batchProducts)
				continue
			}

			for j, p := range batchProducts {
				if isNewMap[j] {
					result.ProductsCreated++
					h.importProgress.AddCreated(1)
				} else {
					result.ProductsUpdated++
					h.importProgress.AddUpdated(1)
				}

				// For EAN page upsert we need the final product state.
				// New products already have all fields; existing ones may have
				// merged attributes, so fetch them.
				var final *model.Product
				if isNewMap[j] {
					final = p
				} else if prod, err := h.productRepo.Get(ids[j]); err == nil {
					final = prod
				} else {
					final = p
				}
				allProducts = append(allProducts, final)
			}
		}
		fmt.Printf("[IMPORT-JSON] Phase 1: done in %v\n", time.Since(phase1Start))

		// ============================================
		// Phase 1.6: Delete stale products not in this import (IN TRANSACTION)
		// ============================================
		// Removes products for this company that are no longer in the price file.
		h.importProgress.SetStep(StepCleanup)
		fmt.Printf("[IMPORT-JSON] Phase 1.6: Cleaning up stale products for company %d...\n", company.ID)
		deleted := h.productRepo.CleanupStaleProductsTx(txn, company.ID, allParsedProducts, identityNormalize)
		result.ProductsDeleted = deleted
		h.importProgress.SetDeleted(deleted)
		fmt.Printf("[IMPORT-JSON] Phase 1.6: deleted %d stale products\n", deleted)

		// ============================================
		// Phase 1.5: Batch index all products (IN TRANSACTION)
		// ============================================
		h.importProgress.SetStep(StepIndex)
		if h.turboSearch != nil && len(allProducts) > 0 {
			for i := 0; i < len(allProducts); i += batchSize {
				end := i + batchSize
				if end > len(allProducts) {
					end = len(allProducts)
				}

				batchProducts := allProducts[i:end]
				fmt.Printf("[IMPORT-JSON] Phase 1.5: Indexing batch %d-%d (%d products)...\n", i, end, len(batchProducts))

				if err := h.turboSearch.BatchIndexProductstx(txn, batchProducts); err != nil {
					fmt.Printf("[IMPORT-JSON] WARN: batch index products: %v\n", err)
				}
			}
		}

		// ============================================
		// Phase 2: Batch upsert EAN pages + index (IN TRANSACTION)
		// ============================================
		fmt.Printf("[IMPORT-JSON] Phase 2: Upserting EAN pages for %d products (transactional)...\n", len(allProducts))
		h.importProgress.SetStep(StepEANPages)
		phase2Start := time.Now()

		if err := h.eanPageRepo.LoadCatalogizerCache(); err != nil {
			fmt.Printf("[IMPORT-JSON] WARN: load catalogizer cache: %v\n", err)
		}

		// Perform batch upsert within transaction
		h.eanPageRepo.BatchUpsertFromProductsTx(txn, allProducts)

		if h.eanPageSearch != nil {
			allPages, _ := h.eanPageRepo.ListAll()
			pagePtrs := make([]*model.EANPage, len(allPages))
			for i := range allPages {
				pagePtrs[i] = &allPages[i]
			}
			if err := h.eanPageSearch.IndexEANPageBatchTx(txn, pagePtrs); err != nil {
				fmt.Printf("[IMPORT-JSON] ERROR: index EAN pages failed: %v\n", err)
				_ = txn.Abort()
				result.Status = "error_index"
				return result
			}
		}
		fmt.Printf("[IMPORT-JSON] Phase 2: EAN pages done in %v\n", time.Since(phase2Start))

		// NOTE: The global (company-independent) recalculations — EAN page
		// product counts, min prices, product/EAN-page sort indexes, category
		// trees, and delivery methods — are no longer run here per company.
		// They run ONCE after all companies have imported
		// (runGlobalRecalculation in import_unified.go).

		// Commit transaction (per-company data: products, indexes, EAN pages).
		h.importProgress.SetStep(StepCommit)
		if err := txn.Commit(); err != nil {
			fmt.Printf("[IMPORT-JSON] ERROR: commit transaction failed: %v\n", err)
			result.Status = "error_commit"
			return result
		}
		fmt.Println("[IMPORT-JSON] Transaction committed successfully")
	}

	result.Status = "completed"
	return result
}

// identityNormalize is the name normalization used by the JSON import for offer
// uniqueness keys. It is the identity function: the JSON import keys offers by
// their raw (unnormalized) name, matching keys created by earlier imports.
func identityNormalize(s string) string { return s }

// parseJSONProductForImport converts a jsonPriceEntry to a model.Product.
func parseJSONProductForImport(jp jsonProductFileItem, companyID int64, companyName, currency string,
	attrDefCache map[string]*model.AttrDef, newAttrKeys map[string]struct{}, attrDefRepo *db.AttrDefRepo) (*model.Product, bool, error) {

	if jp.Name == "" {
		return nil, true, nil
	}

	// Extract price from the latest price history entry
	price := 0.0
	extractedCurrency := currency
	if len(jp.Offers) > 0 && len(jp.Offers[0].PriceHistory) > 0 {
		lastPrice := jp.Offers[0].PriceHistory[len(jp.Offers[0].PriceHistory)-1]
		price = pricesrc.ParsePrice(lastPrice.Price.Value)
		if lastPrice.Price.Currency != "" {
			extractedCurrency = lastPrice.Price.Currency
		}
	}

	if price <= 0 {
		return nil, true, nil
	}

	// Extract SKU from offer ID
	sku := ""
	if len(jp.Offers) > 0 {
		sku = jp.Offers[0].ID
	}

	// EAN: derive from SKU (first part before dash)
	ean := sku
	if idx := strings.Index(sku, "-"); idx > 0 {
		ean = sku[:idx]
	}

	// Build name with company suffix
	name := jp.Name
	if companyName != "" {
		name = name + " — " + companyName
	}

	// Extract image URL
	var images []string
	if jp.ProductImage.URL != "" {
		images = []string{jp.ProductImage.URL}
	}

	// Extract brand ID (use name as simple ID)
	brandID := int64(0)
	if jp.Brand != "" {
		brandID = int64(len(jp.Brand)) + 1
	}

	// Brand is a first-class attribute: it must land in Attributes so the
	// catalog can filter by it (attr.brand=<value>). The AttrDef for the
	// "brand" code is created in Phase 0 via newAttrKeys.
	var attrs []model.KeyValue
	if jp.Brand != "" {
		attrs = append(attrs, model.KeyValue{Key: "brand", Value: jp.Brand})
		newAttrKeys["brand"] = struct{}{}
	}

	p := &model.Product{
		SKU:         sku,
		EAN:         ean,
		Name:        name,
		Description: jp.Description,
		CompanyID:   companyID,
		BrandID:     brandID,
		Brand:       jp.Brand,
		Price:       price,
		Currency:    extractedCurrency,
		StockQty:    0,
		Status:      model.ProductStatusActive,
		ProductURL:  "",
		Images:      images,
		Attributes:  attrs,
		SEO: model.ProductSEO{
			Title: fmt.Sprintf("%s — MakoShop", name),
		},
	}

	if len(jp.Offers) > 0 {
		p.ProductURL = jp.Offers[0].ProductURL
	}

	return p, false, nil
}
