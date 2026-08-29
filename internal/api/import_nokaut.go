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
const pricesDir = "prices"

// downloadPriceFile downloads the company's price file from company.ImportURL
// and saves it to prices/<company>.<ext>. The local copy is kept for debugging
// and can be re-imported manually. Returns the saved file path.
func downloadPriceFile(company *model.Company) (string, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(company.ImportURL)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %s", resp.Status)
	}

	// Determine file extension from the URL path (ignore query params).
	ext := ".xml"
	if u, uerr := url.Parse(company.ImportURL); uerr == nil {
		if e := filepath.Ext(u.Path); e != "" && len(e) <= 10 {
			ext = e
		}
	}

	name := sanitizeFileName(company.Name)
	if name == "" {
		name = fmt.Sprintf("company_%d", company.ID)
	}
	destPath := filepath.Join(pricesDir, name+ext)

	if err := os.MkdirAll(pricesDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
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

	// Import each company
	for i := range companies {
		company := &companies[i]
		fmt.Printf("[IMPORT-NOKAUT] Importing company: %s (ID=%d, url=%q, folder=%q)\n", company.Name, company.ID, company.ImportURL, company.ImportFolder)

		result := NokautImportResult{}
		result = h.importNokautCompany(company, limit)

		fmt.Printf("[IMPORT-NOKAUT] Company %s: parsed=%d created=%d updated=%d skipped=%d\n",
			company.Name, result.OffersParsed, result.ProductsCreated, result.ProductsUpdated, result.ProductsSkipped)

		// Write progress for single-company imports
		if len(companies) == 1 {
			result.Company = company.Name
			httpres.WriteJSON(w, http.StatusOK, result)
		}
	}

	fmt.Printf("[IMPORT-NOKAUT] Completed in %v\n", time.Since(startTime))
}

// importNokautCompany imports all offers for a single company from its price folder.
// Uses application-level transactions for EAN page operations to ensure atomicity.
//
// NOTE: Product creation/update (GetOrCreateByEAN) happens during parsing phase,
// before the transaction starts. For full atomicity, this would need to be moved
// into the transaction as well.
func (h *Handlers) importNokautCompany(company *model.Company, limit int) NokautImportResult {
	cfg := company.PriceSource
	applyPriceSourceDefaults(&cfg)

	// Always use settings.currency as the primary currency source
	currency := company.Settings.Currency
	if currency == "" {
		currency = "PLN"
	}

	var files []string

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
				} else {
					result.ProductsUpdated++
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
	if len(allParsedProducts) > 0 && h.productRepo != nil {
		fmt.Printf("[IMPORT-NOKAUT] Phase 1.6: Cleaning up stale products for company %d...\n", company.ID)
		deleted := h.productRepo.CleanupStaleProductsTx(txn, company.ID, allParsedProducts, pricesrc.NormalizeName)
		result.ProductsDeleted = deleted
		fmt.Printf("[IMPORT-NOKAUT] Phase 1.6: deleted %d stale products\n", deleted)
	}

	// ============================================
	// Phase 1.5: Batch index all products (IN TRANSACTION)
	// ============================================

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

	// ============================================
	// Phase 3: Recalculate EAN page counts + min prices (IN TRANSACTION)
	// ============================================
	fmt.Println("[IMPORT-NOKAUT] Phase 3: Recalculating EAN page counts and min prices (transactional)...")
	phase3Start := time.Now()
	if err := h.eanPageRepo.RecalculateProductCountsTx(txn); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] ERROR: recalculate product counts failed: %v\n", err)
		_ = txn.Abort()
		result.Status = "error_counts"
		return result
	}
	if err := h.eanPageRepo.RecalculateMinPricesTx(txn, h.productRepo); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] ERROR: recalculate min prices failed: %v\n", err)
		_ = txn.Abort()
		result.Status = "error_prices"
		return result
	}
	fmt.Printf("[IMPORT-NOKAUT] Phase 3: done in %v\n", time.Since(phase3Start))

	// ============================================
	// Phase 4: Build product sort indexes (IN TRANSACTION)
	// ============================================
	fmt.Println("[IMPORT-NOKAUT] Phase 4: Building product sort indexes (transactional)...")
	if h.turboSearch != nil {
		if err := h.turboSearch.BuildSortIndexesTx(txn); err != nil {
			fmt.Printf("[IMPORT-NOKAUT] ERROR: build product sort indexes failed: %v\n", err)
			_ = txn.Abort()
			result.Status = "error_sort_indexes"
			return result
		}
	}

	// ============================================
	// Phase 5: Rebuild category trees (IN TRANSACTION)
	// ============================================

	fmt.Println("[IMPORT-NOKAUT] Phase 5: Rebuilding category trees (transactional)...")
	if err := h.categoryRepo.RebuildTreesTx(txn); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] ERROR: rebuild category trees failed: %v\n", err)
		_ = txn.Abort()
		result.Status = "error_trees"
		return result
	}

	// Commit transaction
	if err := txn.Commit(); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] ERROR: commit transaction failed: %v\n", err)
		result.Status = "error_commit"
		return result
	}

	fmt.Println("[IMPORT-NOKAUT] Transaction committed successfully")

	// Rebuild category trees with fully committed data: pages created inside the
	// transaction were not visible to the transactional rebuild (Phase 5).
	h.categoryRepo.RebuildTrees()

	// ============================================
	// Phase 6: Build EAN page sort indexes
	// ============================================
	fmt.Println("[IMPORT-NOKAUT] Phase 6: Building EAN page sort indexes...")
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-NOKAUT] ERROR: build EAN page sort indexes failed: %v\n", err)
			result.Status = "error_eanpage_sort_indexes"
			return result
		}
	}

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
	for _, af := range cfg.AttrFields {
		if val := strings.TrimSpace(offer.Props[af.Field]); val != "" {
			attrs = append(attrs, model.KeyValue{Key: af.Code, Value: html.UnescapeString(val)})
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
					attrs = append(attrs, model.KeyValue{Key: code, Value: value})
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

					// Add all values for this key
					for _, value := range values {
						if count >= 15 {
							break
						}
						attrs = append(attrs, model.KeyValue{Key: ad.Code, Value: value})
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
