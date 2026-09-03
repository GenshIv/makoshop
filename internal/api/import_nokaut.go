package api

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	attrsPkg "github.com/GenshIv/makoshop/internal/attrs"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/pricesrc"
)

// NokautImportResult holds the result of a Nokaut price import operation.
type NokautImportResult struct {
	Status          string `json:"status"`
	Company         string `json:"company,omitempty"`
	Files           int    `json:"files"`
	OffersParsed    int    `json:"offers_parsed"`
	ProductsCreated int    `json:"products_created"`
	ProductsUpdated int    `json:"products_updated"`
	ProductsSkipped int    `json:"products_skipped"`
	ProductsDeleted int    `json:"products_deleted"`
}

// pricesDir is the root directory for company price files.
// Set via SetPricesDir (called from main.go) or defaults to "prices".
var pricesDir = "prices"

// SetPricesDir sets the directory where price files are stored.
func SetPricesDir(dir string) {
	if dir != "" {
		pricesDir = dir
	}
}

// downloadPriceFile downloads the company's price file from company.ImportURL
// and saves it to prices/<company>.<ext>. The local copy is kept for debugging
// and can be re-imported manually. Returns the saved file path.
// Always deletes the old file first to ensure a fresh download.
func downloadPriceFile(company *model.Company) (string, error) {
	// Determine file extension from the URL path (robust to query/matrix params).
	ext := priceFileExt(company.ImportURL, ".xml")

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
		fmt.Printf("[IMPORT-NOKAUT] WARN: remove old file %s: %v\n", destPath, err)
	} else {
		fmt.Printf("[IMPORT-NOKAUT] Removed old file %s\n", destPath)
	}

	// Large price files (e.g. 400MB+) can take many minutes to download on a
	// slow link. Use a generous overall cap so a legitimate slow download
	// completes instead of being aborted, while still bounding a truly stuck
	// transfer. ResponseHeaderTimeout fails fast if the server never starts
	// responding.
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

// sanitizeFileName converts a company name into a safe file name fragment
// (lowercase latin letters, digits, underscores).
func sanitizeFileName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// priceFileExt derives a safe file extension from a price URL path. It strips
// query strings and matrix parameters (? and ;) before taking the suffix after
// the last dot, so "products.json;page=1;pageSize=1000" yields ".json" instead
// of the too-long ".json;page=1;...". Falls back to defExt when the URL has no
// usable extension.
func priceFileExt(rawURL, defExt string) string {
	if rawURL == "" {
		return defExt
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return defExt
	}
	p := u.Path
	if i := strings.IndexAny(p, "?;"); i != -1 {
		p = p[:i]
	}
	if e := filepath.Ext(p); e != "" && len(e) <= 10 {
		return e
	}
	return defExt
}

// detectPriceFormat peeks at the start of a downloaded price file and returns
// "json" or "nokaut" (XML) based on the first meaningful byte, or "" when the
// content is unrecognized. This is the ground truth for which parser to use and
// lets us recover when a company's saved PriceSource.Format disagrees with the
// actual file.
func detectPriceFormat(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	if n == 0 {
		return ""
	}

	// Skip leading whitespace and a UTF-8 BOM to find the first real byte.
	i := 0
	for i < n {
		c := buf[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if i == 0 && c == 0xEF && n >= 3 && buf[1] == 0xBB && buf[2] == 0xBF {
			i = 3
			continue
		}
		break
	}
	if i >= n {
		return ""
	}

	switch buf[i] {
	case '{', '[':
		return "json"
	case '<':
		return "nokaut"
	}
	return ""
}

// HandleAdminImportNokaut imports offers from Nokaut XML price files.
// It is idempotent: existing offers (EAN + name + company) are updated in place,
// never duplicated.
//
// POST /admin/import-nokaut
// Query params:
//   - company=ID_OR_NAME   import only this company (default: all companies with ImportURL or ImportFolder set)
//   - limit=N              max offers to import per company (0 = unlimited)
func (h *Handlers) HandleAdminImportNokaut(w http.ResponseWriter, r *http.Request) {
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

	companyParam := r.URL.Query().Get("company")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}

	startTime := time.Now()
	fmt.Printf("[IMPORT-NOKAUT] Starting (company=%q limit=%d) from %s\n", companyParam, limit, r.RemoteAddr)

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
		httpres.WriteJSON(w, http.StatusOK, NokautImportResult{Status: "no_companies"})
		return
	}

	// Track live progress for this run.
	h.importProgress.Begin(len(companies))
	defer h.importProgress.Finish()

	// Import each company
	for i := range companies {
		company := &companies[i]
		fmt.Printf("[IMPORT-NOKAUT] Importing company: %s (ID=%d, url=%q, folder=%q)\n", company.Name, company.ID, company.ImportURL, company.ImportFolder)

		h.importProgress.SetCompany(i+1, company.Name, "nokaut")
		result := NokautImportResult{}
		result = h.importNokautCompany(company, limit, "")

		state := companyResultState(CompanyImportResult{Status: result.Status})
		errMsg := ""
		if state == CompanyStateFailed {
			errMsg = result.Status
		}
		h.importProgress.CompanyDone(i+1, state, errMsg)

		fmt.Printf("[IMPORT-NOKAUT] Company %s: parsed=%d created=%d updated=%d skipped=%d\n",
			company.Name, result.OffersParsed, result.ProductsCreated, result.ProductsUpdated, result.ProductsSkipped)

		// Write progress for single-company imports
		if len(companies) == 1 {
			result.Company = company.Name
			httpres.WriteJSON(w, http.StatusOK, result)
		}
	}

	// Global recalculation: run ONCE after all companies (not per company).
	if err := h.runGlobalRecalculation(); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] WARN: global recalculation: %v\n", err)
	}

	fmt.Printf("[IMPORT-NOKAUT] Completed in %v\n", time.Since(startTime))
}

// importNokautCompany imports all offers for a single company from its price folder.
// Uses application-level transactions for EAN page operations to ensure atomicity.
//
// NOTE: Product creation/update (GetOrCreateByEAN) happens during parsing phase,
// before the transaction starts. For full atomicity, this would need to be moved
// into the transaction as well.
func (h *Handlers) importNokautCompany(company *model.Company, limit int, explicitFile string) NokautImportResult {
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
		if company.ImportURL != "" {
			// Download the price file from the URL and save it to prices/<company>.<ext>
			// The local copy is kept for debugging and re-imports.
			destPath, err := downloadPriceFile(company)
			if err != nil {
				fmt.Printf("[IMPORT-NOKAUT] WARN: download price from %s: %v\n", company.ImportURL, err)
				return NokautImportResult{Status: "download_error"}
			}
			files = []string{destPath}
			fmt.Printf("[IMPORT-NOKAUT] Downloaded price file to %s\n", destPath)
		} else {
			// Legacy: read XML files from the company's import folder
			// Clean the ImportFolder path: remove leading/trailing slashes and "prices/" prefix
			importFolder := company.ImportFolder
			importFolder = strings.TrimPrefix(importFolder, "/")
			importFolder = strings.TrimSuffix(importFolder, "/")
			importFolder = strings.TrimPrefix(importFolder, "prices/")

			dir := filepath.Join(pricesDir, importFolder)
			var err error
			files, err = filepath.Glob(filepath.Join(dir, "*.xml"))
			if err != nil {
				fmt.Printf("[IMPORT-NOKAUT] WARN: glob %s: %v\n", dir, err)
				return NokautImportResult{Status: "no_files"}
			}
			sort.Strings(files)
			if len(files) == 0 {
				fmt.Printf("[IMPORT-NOKAUT] WARN: no XML files in %s\n", dir)
				return NokautImportResult{Status: "no_files"}
			}
			fmt.Printf("[IMPORT-NOKAUT] Found %d XML files in %s\n", len(files), dir)
		}
	}

	// Pre-load attribute definitions to avoid repeated DB hits
	attrDefCache := make(map[string]*model.AttrDef)
	newAttrKeys := make(map[string]struct{}) // unique keys not in cache
	if h.attrDefRepo != nil {
		if attrDefs, err := h.attrDefRepo.List(); err == nil {
			for i := range attrDefs {
				// Cache by each key in the Keys array
				for _, key := range attrDefs[i].Keys {
					attrDefCache[key] = &attrDefs[i]
				}
			}
			fmt.Printf("[IMPORT-NOKAUT] Pre-loaded %d attribute definitions (%d keys)\n", len(attrDefs), len(attrDefCache))
		}
	}

	parser := pricesrc.NewNokautParser()

	var result NokautImportResult
	result.Files = len(files)

	var allProducts []*model.Product
	var allParsedProducts []*model.Product
	var allParsedNames []string

	h.importProgress.SetStep(StepParse)
	for _, file := range files {
		if limit > 0 && result.OffersParsed >= limit {
			break
		}

		f, err := os.Open(file)
		if err != nil {
			fmt.Printf("[IMPORT-NOKAUT] WARN: open %s: %v\n", file, err)
			continue
		}

		fileStart := time.Now()
		fmt.Printf("[IMPORT-NOKAUT] Parsing %s (file %d/%d)...\n", filepath.Base(file), len(files), len(files))

		var fileParsed int
		var fileSkipped int

		// First pass: parse and collect all products
		var parsedProducts []*model.Product
		var parsedNames []string

		_, err = parser.Parse(f, func(offer pricesrc.Offer) error {
			if limit > 0 && result.OffersParsed >= limit {
				return fmt.Errorf("limit reached")
			}
			result.OffersParsed++
			fileParsed++
			h.importProgress.AddParsed(1)

			p := mapOfferToProduct(offer, cfg, company.ID, currency, h.attrDefRepo, attrDefCache, newAttrKeys)
			if p == nil {
				fileSkipped++
				result.ProductsSkipped++
				return nil
			}

			parsedProducts = append(parsedProducts, p)
			parsedNames = append(parsedNames, pricesrc.NormalizeName(p.Name))
			return nil
		})

		// Collect all parsed products for batch processing
		if len(parsedProducts) > 0 {
			allParsedProducts = append(allParsedProducts, parsedProducts...)
			allParsedNames = append(allParsedNames, parsedNames...)
		}

		f.Close()

		if err != nil && err.Error() != "limit reached" {
			fmt.Printf("[IMPORT-NOKAUT] WARN: parse %s: %v (parsed %d before error)\n", filepath.Base(file), err, fileParsed)
		}

		fmt.Printf("[IMPORT-NOKAUT]   %s: parsed=%d skipped=%d in %v\n",
			filepath.Base(file), fileParsed, fileSkipped, time.Since(fileStart))
	}

	// ============================================
	// Phase 0: Batch create new attribute definitions (OUTSIDE TRANSACTION)
	// ============================================
	// Creates AttrDef for all new attribute keys in one batch to avoid vacuum.
	h.importProgress.SetStep(StepAttrDefs)
	if len(newAttrKeys) > 0 && h.attrDefRepo != nil {
		fmt.Printf("[IMPORT-NOKAUT] Phase 0: Creating %d new attribute definitions (batch)...\n", len(newAttrKeys))
		phase0Start := time.Now()

		// Collect all new keys
		keys := make([]string, 0, len(newAttrKeys))
		for key := range newAttrKeys {
			keys = append(keys, key)
		}

		// Batch create all AttrDef
		created, err := h.attrDefRepo.BatchGetOrCreateByKeys(keys)
		if err != nil {
			fmt.Printf("[IMPORT-NOKAUT] WARN: batch create attribute definitions: %v\n", err)
		} else {
			// Update cache with newly created AttrDef
			for key, ad := range created {
				attrDefCache[key] = ad
			}
			fmt.Printf("[IMPORT-NOKAUT] Phase 0: created %d new attribute definitions in %v\n", len(created), time.Since(phase0Start))
		}
	}

	// ============================================
	// Phase 1: Batch create/update products (IN TRANSACTION)
	// ============================================
	fmt.Printf("[IMPORT-NOKAUT] Phase 1: Creating/updating %d products (transactional)...\n", len(allParsedProducts))
	h.importProgress.SetStep(StepProducts)
	phase1Start := time.Now()

	// Create application-level transaction BEFORE any writes
	txn := db.NewTransaction(h.store)
	if err := txn.Begin(); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] ERROR: failed to begin transaction: %v\n", err)
		result.Status = "error_transaction"
		return result
	}
	defer func() {
		if !txn.IsFinished() {
			_ = txn.Abort()
		}
	}()

	if len(allParsedProducts) > 0 {
		// Process in batches of 1000 to avoid filling up the database
		const batchSize = 100_000
		for i := 0; i < len(allParsedProducts); i += batchSize {
			end := i + batchSize
			if end > len(allParsedProducts) {
				end = len(allParsedProducts)
			}

			batchProducts := allParsedProducts[i:end]
			batchNames := allParsedNames[i:end]

			fmt.Printf("[IMPORT-NOKAUT] Phase 1: Processing batch %d-%d (%d products)...\n", i, end, len(batchProducts))

			ids, isNewMap, err := h.productRepo.BatchGetOrCreateByEANTx(txn, batchProducts, batchNames)
			if err != nil {
				fmt.Printf("[IMPORT-NOKAUT] WARN: BatchGetOrCreateByEANTx: %v\n", err)
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

				// For EANPage upsert we need the final product state.
				// New products already have all fields; existing ones may have
				// merged attributes, so fetch them.
				var final *model.Product
				if isNewMap[j] {
					final = p
				} else {
					if prod, err := h.productRepo.Get(ids[j]); err == nil {
						final = prod
					} else {
						final = p
					}
				}
				allProducts = append(allProducts, final)
			}
		}
	}
	fmt.Printf("[IMPORT-NOKAUT] Phase 1: done in %v\n", time.Since(phase1Start))

	// ============================================
	// Phase 1.6: Delete stale products not in this import (IN TRANSACTION)
	// ============================================
	// Removes products for this company that are no longer in the price file.
	// Runs before indexing so the vendor index still reflects pre-import state.
	h.importProgress.SetStep(StepCleanup)
	if h.productRepo != nil {
		fmt.Printf("[IMPORT-NOKAUT] Phase 1.6: Cleaning up stale products for company %d...\n", company.ID)
		deleted := h.productRepo.CleanupStaleProductsTx(txn, company.ID, allParsedProducts, pricesrc.NormalizeName)
		result.ProductsDeleted = deleted
		h.importProgress.SetDeleted(deleted)
		fmt.Printf("[IMPORT-NOKAUT] Phase 1.6: deleted %d stale products\n", deleted)
	}

	// ============================================
	// Phase 1.5: Batch index all products (IN TRANSACTION)
	// ============================================
	h.importProgress.SetStep(StepIndex)
	if h.turboSearch != nil && len(allProducts) > 0 {
		// Process in batches of 1000 to avoid filling up the database
		const batchSize = 100_000
		for i := 0; i < len(allProducts); i += batchSize {
			end := i + batchSize
			if end > len(allProducts) {
				end = len(allProducts)
			}

			batchProducts := allProducts[i:end]
			fmt.Printf("[IMPORT-NOKAUT] Phase 1.5: Indexing batch %d-%d (%d products)...\n", i, end, len(batchProducts))

			if err := h.turboSearch.BatchIndexProductstx(txn, batchProducts); err != nil {
				fmt.Printf("[IMPORT-NOKAUT] WARN: batch index products: %v\n", err)
			}
		}
	}

	// return NokautImportResult{}
	// ============================================
	// Phase 2: Batch upsert EAN pages + index (IN TRANSACTION)
	// ============================================
	fmt.Printf("[IMPORT-NOKAUT] Phase 2: Upserting EAN pages for %d products (transactional)...\n", len(allProducts))
	h.importProgress.SetStep(StepEANPages)
	phase2Start := time.Now()

	if err := h.eanPageRepo.LoadCatalogizerCache(); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] WARN: load catalogizer cache: %v\n", err)
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
			fmt.Printf("[IMPORT-NOKAUT] ERROR: index EAN pages failed: %v\n", err)
			_ = txn.Abort()
			result.Status = "error_index"
			return result
		}
	}
	fmt.Printf("[IMPORT-NOKAUT] Phase 2: EAN pages done in %v\n", time.Since(phase2Start))

	// NOTE: The global (company-independent) recalculations — EAN page product
	// counts, min prices, product/EAN-page sort indexes, category trees, and
	// delivery methods — are no longer run here per company. They run ONCE
	// after all companies have imported (runGlobalRecalculation in
	// import_unified.go), which saves time and avoids re-doing the same global
	// work for every company.

	// Flush any attribute codes created on-the-fly (HTML parser) to the
	// registry list in ONE batched write. This covers the case where
	// newAttrKeys was empty (so Phase 0 / BatchGetOrCreateByKeys did not run)
	// but the description parser still created new AttrDefs.
	if h.attrDefRepo != nil {
		if err := h.attrDefRepo.FlushList(); err != nil {
			fmt.Printf("[IMPORT-NOKAUT] WARN: FlushList failed: %v\n", err)
		}
	}

	// Commit transaction (per-company data: products, indexes, EAN pages).
	h.importProgress.SetStep(StepCommit)
	if err := txn.Commit(); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] ERROR: commit transaction failed: %v\n", err)
		result.Status = "error_commit"
		return result
	}

	fmt.Println("[IMPORT-NOKAUT] Transaction committed successfully")

	result.Status = "completed"
	return result
}

// applyPriceSourceDefaults fills in default field mappings for a PriceSourceConfig.
func applyPriceSourceDefaults(cfg *model.PriceSourceConfig) {
	if cfg.EANField == "" {
		cfg.EANField = "EAN"
	}
	if cfg.PreviousPriceField == "" {
		cfg.PreviousPriceField = "PreviousPrice"
	}
	if cfg.ImageField == "" {
		cfg.ImageField = "ImageOriginalUrl"
	}
	if cfg.ProductURLField == "" {
		cfg.ProductURLField = "ProductUrl"
	}
	if cfg.BrandField == "" {
		cfg.BrandField = "Producent"
	}
	if cfg.ShopCategoryField == "" {
		cfg.ShopCategoryField = "ShopProductCategory"
	}
}

// mapOfferToProduct maps a parsed Nokaut offer to a model.Product using the
// company's PriceSourceConfig. Returns nil if the offer has no usable data.
func mapOfferToProduct(offer pricesrc.Offer, cfg model.PriceSourceConfig, companyID int64, currency string, attrDefRepo interface{}, attrDefCache map[string]*model.AttrDef, newAttrKeys map[string]struct{}) *model.Product {
	name := strings.TrimSpace(offer.Name)
	if name == "" {
		return nil
	}

	price := pricesrc.ParsePrice(offer.Price)
	if price <= 0 {
		return nil
	}

	// EAN: from configured property field
	ean := pricesrc.ExtractEAN(offer.Props[cfg.EANField])

	// Previous price: from configured property field
	previousPrice := pricesrc.ParsePrice(offer.Props[cfg.PreviousPriceField])

	// Image: from configured property field, fallback to <image>
	image := strings.TrimSpace(offer.Props[cfg.ImageField])
	if image == "" {
		image = strings.TrimSpace(offer.Image)
	}
	var images []string
	if image != "" {
		images = []string{image}
	}

	// Product URL: from configured property field, fallback to <url>
	productURL := strings.TrimSpace(offer.Props[cfg.ProductURLField])
	if productURL == "" {
		productURL = strings.TrimSpace(offer.URL)
	}

	// Brand: from configured property field, fallback to <producer>
	brand := strings.TrimSpace(offer.Props[cfg.BrandField])
	if brand == "" {
		brand = strings.TrimSpace(offer.Producer)
	}

	// Shop category: from configured property field
	shopCategory := strings.TrimSpace(offer.Props[cfg.ShopCategoryField])

	// Availability → status + stock
	status, stockQty := mapAvailability(offer.Availability, cfg.AvailabilityMap)

	// Attributes: extra fields from config only
	var attrs []model.KeyValue

	// Brand is a first-class attribute: it must land in Attributes so the
	// catalog can filter by it (attr.brand=<value>). The AttrDef for the
	// "brand" code is created in Phase 0 via newAttrKeys.
	if brand != "" {
		if nv := attrsPkg.NormalizeValue(brand); attrsPkg.ValidValue(nv) {
			attrs = append(attrs, model.KeyValue{Key: "brand", Value: nv})
			newAttrKeys["brand"] = struct{}{}
		}
	}

	for _, af := range cfg.AttrFields {
		if val := strings.TrimSpace(offer.Props[af.Field]); val != "" {
			// Normalize, split and validate the value (deduplicated).
			for _, v := range attrsPkg.SplitValues(html.UnescapeString(val)) {
				attrs = append(attrs, model.KeyValue{Key: af.Code, Value: v})
			}
		}
	}
	// product_url, purchase_url, and shop_category are now separate fields on Product, not attributes
	// Partner/affiliate purchase URL: from <url> field (e.g. webep1.com link).
	// This is the link the "go to purchase" button should use.
	purchaseURL := strings.TrimSpace(offer.URL)

	// Step 1: Clean HTML first — remove scripts, inline styles, unescape entities
	rawDescription := strings.TrimSpace(offer.Description)
	description := pricesrc.CleanHTMLDescription(rawDescription)

	// Step 2: Parse attributes from cleaned HTML description
	if description != "" && attrDefRepo != nil {
		if repo, ok := attrDefRepo.(*db.AttrDefRepo); ok {
			// Try HTML attribute parser first
			keyParser := pricesrc.NewHTMLAttrKeyParser(repo)
			htmlAttrs := keyParser.Parse(description)
			for code, values := range htmlAttrs {
				for _, value := range values {
					// Skip attribute values longer than 40 runes
					if len([]rune(value)) <= 40 {
						attrs = append(attrs, model.KeyValue{Key: code, Value: value})
					}
				}
			}

			// If no attributes found, try specific parsers based on description format
			if len(htmlAttrs) == 0 {
				var parsedAttrs map[string][]string

				// Try HDWR parser first (for "Specyfikacja urządzenia" format)
				if strings.Contains(description, "Specyfikacja urządzenia") {
					hdwrParser := pricesrc.NewHDWRAttrParser()
					parsedAttrs = hdwrParser.Parse(description)
				}

				// Fallback to Zabudowa parser (plain text format)
				if len(parsedAttrs) == 0 {
					zabudowaParser := pricesrc.NewZabudowaAttrParser()
					parsedAttrs = zabudowaParser.Parse(description)
				}

				// Limit to max 15 attributes to prevent bloat
				count := 0
				for key, values := range parsedAttrs {
					// Check cache first to avoid DB hit
					var ad *model.AttrDef
					if cached, exists := attrDefCache[key]; exists {
						ad = cached
					} else {
						// Collect new keys for batch creation later
						newAttrKeys[key] = struct{}{}
						continue
					}

					// Add all values for this key (normalized + validated).
					for _, value := range values {
						if count >= 15 {
							break
						}
						nv := attrsPkg.NormalizeValue(value)
						if attrsPkg.ValidValue(nv) {
							attrs = append(attrs, model.KeyValue{Key: ad.Code, Value: nv})
						}
						count++
					}
				}
			}
		}
	}

	// Step 3: Remove attribute section from description if attributes were found (for plain text only)
	if len(attrs) > 0 && !strings.Contains(description, "<") {
		description = removeAttributeSection(description)
	}

	// Step 4: Wrap description in <pre> tags for Zabudowa AGD (plain text format)
	if strings.Contains(description, "\n") && !strings.Contains(description, "<pre") {
		description = "<pre>" + description + "</pre>"
		// Use larger limit for pre-formatted content
		description = pricesrc.TruncateHTML(description, 5000)

	} else {
		// Step 5: Truncate description safely
		description = pricesrc.TruncateHTML(description, 3000)
	}

	// Final pass: drop duplicate (code, value) pairs coming from different
	// sources (brand + XML fields + HTML + fallback parsers).
	attrs = dedupeAttrPairs(attrs)

	return &model.Product{
		EAN:           ean,
		Name:          name,
		Description:   description,
		CompanyID:     companyID,
		Brand:         brand,
		Price:         price,
		PreviousPrice: previousPrice,
		Currency:      currency,
		StockQty:      stockQty,
		Status:        status,
		ProductURL:    productURL,
		PurchaseURL:   purchaseURL,
		Attributes:    attrs,
		Images:        images,
		ShopCategory:  shopCategory,
	}
}

// dedupeAttrPairs removes duplicate (key, value) pairs, preserving order.
// Prevents multiple records of the same attribute in product documents and,
// downstream, in EAN pages and indexes.
func dedupeAttrPairs(attrs []model.KeyValue) []model.KeyValue {
	if len(attrs) < 2 {
		return attrs
	}
	seen := make(map[string]bool, len(attrs))
	result := make([]model.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		k := kv.Key + "\x00" + kv.Value
		if seen[k] {
			continue
		}
		seen[k] = true
		result = append(result, kv)
	}
	return result
}

// mapAvailability converts a raw availability value to product status + stock qty.
func mapAvailability(raw string, availMap map[string]string) (model.ProductStatus, int64) {
	raw = strings.TrimSpace(raw)

	mapped := ""
	if availMap != nil {
		mapped = availMap[raw]
	}

	// Default heuristic if no explicit mapping
	if mapped == "" {
		if strings.Contains(strings.ToLower(raw), "out") {
			mapped = "out_of_stock"
		} else {
			mapped = "in_stock"
		}
	}

	if mapped == "in_stock" {
		return model.ProductStatusActive, 1
	}
	return model.ProductStatusHidden, 0
}

// removeAttributeSection removes the attribute section from plain text description.
// Looks for common markers like "Specyfikacja urządzenia", "Specyfikacja", "Parametry", etc.
func removeAttributeSection(description string) string {
	// Common attribute section markers
	markers := []string{
		"Specyfikacja urządzenia",
		"Specyfikacja techniczna",
		"Specyfikacja",
		"Parametry",
		"Dane techniczne",
		"Technical specifications",
		"Specifications",
	}

	for _, marker := range markers {
		idx := strings.Index(description, marker)
		if idx != -1 {
			// Keep only the text before the marker
			return strings.TrimSpace(description[:idx])
		}
	}

	return description
}
