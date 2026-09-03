package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
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
func downloadJSONPriceFile(company *model.Company, noDownload bool) (string, error) {
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

	f, err := os.Open(destPath)

	if noDownload && err == nil && f.Close() == nil {
		return destPath, nil
	}
	// Delete old file first to ensure fresh download
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) && !noDownload {
		fmt.Printf("[IMPORT-JSON] WARN: remove old file %s: %v\n", destPath, err)
	} else {
		fmt.Printf("[IMPORT-JSON] Removed old file %s\n", destPath)
		return destPath, nil
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

	f, err = os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}

	return destPath, nil
}

// allegroDefaultMaxItems caps an Allegro feed download at 1000 items (10 pages
// of 100). Allegro feeds are paginated and can hold tens of thousands of
// products; the user wants a bounded, memory-safe download.
const allegroDefaultMaxItems = 1000

// isAllegroFeed reports whether the company's price source is configured as an
// Allegro feed (paginated JSON with coded "fields"). Such feeds are downloaded
// page-by-page (capped) into a file first, then processed from the file.
func isAllegroFeed(company *model.Company) bool {
	return strings.EqualFold(strings.TrimSpace(company.PriceSource.Format), "allegro")
}

// isAllegroURL reports whether the import URL points at an Allegro feed.
// Allegro feeds use the same paginated JSON format as Tradedoubler but are
// hosted on allegro.pl domains or contain "allegro" in the path.
func isAllegroURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "allegro") {
		return true
	}
	path := strings.ToLower(u.Path)
	if strings.Contains(path, "allegro") {
		return true
	}
	return false
}

// allegroPage is the top-level shape of one Allegro feed page. It carries the
// coded product fields and a productHeader with the total hit count (used to
// know when the feed is exhausted). It also tolerates the Tradedoubler "meta"
// block so the same walker can stop on either signal.
type allegroPage struct {
	ProductHeader map[string]interface{} `json:"productHeader"`
	Products      []JsonProductFileItem  `json:"products"`
	Meta          TradedoublerMeta       `json:"meta"`
}

// downloadAllegroFeed walks the paginated Allegro feed starting at the page
// encoded in the URL (default 1) and returns up to limit products. limit
// semantics: limit < 0 means unlimited (download the whole feed), limit == 0
// means allegroDefaultMaxItems, limit > 0 means exactly that many. It stops
// when a page is empty, when the productHeader.totalHits / meta.totalPages is
// reached, or when the cap is hit. The page size is taken from the URL
// (default 100, the Allegro feed default).
func downloadAllegroFeed(client *http.Client, rawURL string, limit int) ([]JsonProductFileItem, error) {
	if limit == 0 {
		limit = allegroDefaultMaxItems
	}
	page, pageSize, baseQuery, err := tradedoublerPageParams(rawURL)
	if err != nil {
		return nil, err
	}
	// Allegro feeds serve 100 items per page by default; respect an explicit
	// pageSize from the URL but never exceed the API maximum.
	if pageSize > tradedoublerMaxPageSize {
		pageSize = tradedoublerMaxPageSize
	}

	var all []JsonProductFileItem
	// seen tracks product identities already collected, so a feed (or a static
	// file that ignores the page parameter) that keeps returning the same
	// products cannot loop forever in unlimited mode: a page that adds nothing
	// new ends the walk.
	seen := make(map[string]struct{})
	current := page
	for {
		reqURL, uerr := tradedoublerPageURL(rawURL, baseQuery, current)
		if uerr != nil {
			return all, uerr
		}
		fmt.Printf("[IMPORT-ALLEGRO] Fetching page %d (%d products so far)\n", current, len(all))
		var pageData allegroPage
		if ferr := fetchJSON(client, reqURL, &pageData); ferr != nil {
			return all, fmt.Errorf("page %d: %w", current, ferr)
		}

		got := len(pageData.Products)
		if got == 0 {
			fmt.Printf("[IMPORT-ALLEGRO] Page %d empty, stopping\n", current)
			break
		}

		newOnPage := 0
		for _, p := range pageData.Products {
			id := feedProductIdentity(p)
			if id == "" {
				newOnPage++
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			newOnPage++
		}
		if newOnPage == 0 {
			fmt.Printf("[IMPORT-ALLEGRO] Page %d: no new products, stopping\n", current)
			break
		}
		all = append(all, pageData.Products...)
		fmt.Printf("[IMPORT-ALLEGRO] Page %d: %d products (total %d)\n", current, got, len(all))

		if limit > 0 && len(all) >= limit {
			all = all[:limit]
			break
		}

		// Decide whether there is a next page.
		next := false
		switch {
		case pageData.Meta.TotalPages > 0:
			next = current < pageData.Meta.TotalPages
		case pageData.Meta.TotalItems > 0:
			next = len(all) < pageData.Meta.TotalItems
		case totalHitsFromHeader(pageData.ProductHeader) > 0:
			next = len(all) < totalHitsFromHeader(pageData.ProductHeader)
		default:
			// No pagination metadata: keep going while the page was full.
			next = got >= pageSize
		}
		if !next {
			break
		}
		current++
	}
	return all, nil
}

// feedProductIdentity returns a stable identifier for a feed product, used to
// detect a page that repeats products already collected (a static file or an
// API that ignores the page parameter). Prefers the offer's sourceProductId,
// then the offer id, then the product name; returns "" when none is available.
func feedProductIdentity(p JsonProductFileItem) string {
	if len(p.Offers) > 0 {
		if sid := strings.TrimSpace(p.Offers[0].SourceProductID); sid != "" {
			return "src:" + sid
		}
		if oid := strings.TrimSpace(p.Offers[0].ID); oid != "" {
			return "id:" + oid
		}
	}
	if name := strings.TrimSpace(p.Name); name != "" {
		return "name:" + name
	}
	return ""
}

// totalHitsFromHeader extracts a numeric totalHits from an Allegro
// productHeader (the value may arrive as a JSON number or a string).
func totalHitsFromHeader(header map[string]interface{}) int {
	if header == nil {
		return 0
	}
	switch v := header["totalHits"].(type) {
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// writeFeedFile writes the downloaded feed products to destPath in the
// canonical jsonPriceFile shape so the existing file-based parser can consume
// them (two-stage: download to disk first, then process from the file).
func writeFeedFile(products []JsonProductFileItem, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// Delete any stale file first so a partial/old feed is never mixed in.
	_ = os.Remove(destPath)

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	doc := jsonPriceFile{Products: products}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
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
	Products      []JsonProductFileItem  `json:"products"`
}

// jsonPriceEntry represents a single product in the JSON price file.
type JsonProductFileItem struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Brand        string                 `json:"brand"`
	Language     string                 `json:"language"`
	Fields       []jsonFieldItem        `json:"fields"` // raw feed fields (e.g. Allegro "attr_<id>")
	Offers       []jsonOfferFileItem    `json:"offers"`
	Categories   []jsonCategoryFileItem `json:"categories"`
	ProductImage jsonImageFileItem      `json:"productImage"`
}

// jsonFieldItem is a single raw feed field: a coded parameter (e.g. Allegro
// "attr_11323") and its string value. The code is resolved to a human-readable
// attribute name via the company's FieldMap (see model.FieldMapEntry).
type jsonFieldItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// jsonOfferFileItem represents an offer within a product.
type jsonOfferFileItem struct {
	ID              string           `json:"id"`
	SourceProductID string           `json:"sourceProductId"`
	ProductURL      string           `json:"productUrl"`
	PriceHistory    []jsonPriceEntry `json:"priceHistory"`
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

	// Tradedoubler feeds are served from a paginated JSON API (not a single
	// static file). Detect it up front so we can walk every page instead of
	// downloading one file.
	isTradedoubler := isTradedoublerURL(company.ImportURL)
	if isTradedoubler {
		fmt.Printf("[IMPORT-JSON] %s: detected Tradedoubler paginated API, will walk pages\n", company.Name)
	}

	// Allegro feeds are a special case of the paginated JSON feed: they carry
	// coded "fields" (attr_<id>) that are resolved via the company's FieldMap.
	// They are downloaded page-by-page (capped at allegroDefaultMaxItems) into
	// a file FIRST, then processed from the file (two-stage, memory-safe).
	// Detect Allegro feeds by URL (host contains "allegro") OR by company config.
	isAllegro := isAllegroFeed(company) || isAllegroURL(company.ImportURL)
	if isAllegro && !company.PriceSource.DisablePagination {
		fmt.Printf("[IMPORT-JSON] %s: detected Allegro feed, will download pages (cap %d) to file\n", company.Name, allegroDefaultMaxItems)
	} else if isAllegro {
		fmt.Printf("[IMPORT-JSON] %s: detected Allegro feed, pagination disabled, will download the FULL feed into a single file (no cap)\n", company.Name)
	}

	// For Allegro feeds, merge the company's field map with the default Allegro
	// field map (docs/allegro_field_map.json). The company's map takes precedence.
	// Load the field map BEFORE the Tradedoubler path so attributes are mapped
	// correctly even when the feed uses the same URL format as Tradedoubler.
	fieldMap := company.PriceSource.FieldMap
	if isAllegro {
		defaultMap := loadAllegroFieldMap()
		if defaultMap != nil && len(defaultMap) > 0 {
			merged := make(map[string]model.FieldMapEntry)
			// Copy default map first
			for k, v := range defaultMap {
				merged[k] = v
			}
			// Override with company's map
			for k, v := range fieldMap {
				merged[k] = v
			}
			fieldMap = merged
			fmt.Printf("[IMPORT-JSON] %s: merged Allegro field map (%d default + %d company entries)\n",
				company.Name, len(defaultMap), len(company.PriceSource.FieldMap))
		}
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
			destPath, err := downloadJSONPriceFile(company, noDownload)

			files = append(files, destPath)
			if err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: glob %s: %v\n", dir, err)
				return JSONImportResult{Status: "no_files"}
			}
			// Fallback: the configured "folder" may actually be a single file
			// name (e.g. prices/allegro2.json) or the file may sit at the top level.

			fmt.Printf("[IMPORT-JSON] Using local JSON files (folder=%q, %d files): %v\n", importFolder, len(files), files)
		} else if company.ImportURL != "" {
			if isAllegro && !company.PriceSource.DisablePagination {
				// Allegro: download pages (capped) into a file, then process it.
				client := tradedoublerClient()
				prods, derr := downloadAllegroFeed(client, company.ImportURL, limit)
				if derr != nil {
					fmt.Printf("[IMPORT-JSON] WARN: download Allegro feed from %s: %v\n", company.ImportURL, derr)
					return JSONImportResult{Status: "download_error"}
				}
				destPath := filepath.Join(pricesDir, sanitizeFileName(company.Name)+".json")
				if werr := writeFeedFile(prods, destPath); werr != nil {
					fmt.Printf("[IMPORT-JSON] WARN: write Allegro feed file %s: %v\n", destPath, werr)
					return JSONImportResult{Status: "download_error"}
				}
				files = []string{destPath}
				fmt.Printf("[IMPORT-JSON] %s: downloaded %d products to %s\n", company.Name, len(prods), destPath)
			} else if isAllegro {
				// Allegro with pagination disabled: download the feed as a SINGLE
				// file by doing a plain GET of the ImportURL EXACTLY as configured.
				// The URL must NOT be rewritten (no page/pageSize params) — the
				// user's feed URL returns the full price list as-is, and modifying
				// it breaks the download. The whole response is saved to one file
				// and imported as a single file. This is the "import as one file,
				// no pagination" mode.
				destPath, derr := downloadJSONPriceFile(company, noDownload)
				if derr != nil {
					fmt.Printf("[IMPORT-JSON] WARN: download Allegro single file from %s: %v\n", company.ImportURL, derr)
					return JSONImportResult{Status: "download_error"}
				}
				files = []string{destPath}
				fmt.Printf("[IMPORT-JSON] %s: downloaded Allegro single file (no pagination, URL unchanged) to %s\n", company.Name, destPath)
			} else if isTradedoubler {
				// Paginated API: pages are fetched below (not a single file).
				fmt.Printf("[IMPORT-JSON] %s: skipping single-file download (paginated API)\n", company.Name)
			} else {
				// Download the JSON price file from the URL (default: always re-download)
				destPath, err := downloadJSONPriceFile(company, noDownload)
				if err != nil {
					fmt.Printf("[IMPORT-JSON] WARN: download JSON price from %s: %v\n", company.ImportURL, err)
					return JSONImportResult{Status: "download_error"}
				}
				files = []string{destPath}
				fmt.Printf("[IMPORT-JSON] Downloaded JSON price file to %s\n", destPath)
			}
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
	// unmappedFields collects feed field codes that have no entry in the
	// company's field map, so the import report can tell the user which codes
	// to add (manual analysis of the feed).
	unmappedFields := make(map[string]struct{})
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

	if isTradedoubler && !isAllegro {
		// --- Paginated API path: walk every page and parse the products. ---
		// (Allegro feeds are handled via the file path above: they are
		// downloaded to a file first, then parsed with field-map support.)
		client := tradedoublerClient()
		tps, derr := downloadTradedoubler(client, company.ImportURL, limit)
		if derr != nil {
			fmt.Printf("[IMPORT-JSON] %s: Tradedoubler download failed: %v\n", company.Name, derr)
			result.Status = "download_error"
			return result
		}
		result.Files = 1 // the feed is one logical source
		parsed, names, skipped := parseTradedoublerProducts(tps, company.ID, company.Slug, company.Name, currency, attrDefCache, newAttrKeys, fieldMap, limit)
		allParsedProducts = parsed
		allParsedNames = names
		result.ProductsSkipped = skipped
		result.OffersParsed = len(parsed)
		for range parsed {
			h.importProgress.AddParsed(1)
		}
		fmt.Printf("[IMPORT-JSON] %s: Tradedoubler imported %d products (skipped=%d)\n", company.Name, len(parsed), skipped)
	} else {
		for _, file := range files {
			if limit > 0 && result.OffersParsed >= limit {
				break
			}

			fileStart := time.Now()
			fmt.Printf("[IMPORT-JSON] Parsing %s (file %d/%d)...\n", filepath.Base(file), len(files), len(files))

			var fileParsed int
			var fileSkipped int

			// First pass: parse JSON and collect all products. The file may be a
			// ZIP archive (Allegro "unlimited export") — readPriceFileJSON handles
			// both plain JSON and ZIP transparently.
			var parsedProducts []*model.Product
			var parsedNames []string

			jsonData, err := readPriceFileJSON(file)
			if err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: load %s: %v\n", file, err)
				continue
			}

			fmt.Printf("[IMPORT-JSON] Loaded %d products from %s\n", len(jsonData.Products), filepath.Base(file))

			for _, jp := range jsonData.Products {
				if limit > 0 && result.OffersParsed >= limit {
					break
				}

				// Parse the JSON product into a model.Product
				prod, skip, err := parseJSONProductForImport(jp, company.ID, company.Slug, company.Name, currency, attrDefCache, newAttrKeys, h.attrDefRepo, fieldMap, unmappedFields)
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

	// Phase 0.5: Apply field-map names to the created AttrDefs (OUTSIDE TRANSACTION)
	// ============================================
	// Coded feed fields (e.g. Allegro "attr_11323") are created in Phase 0 with
	// a placeholder name (the code itself). Here we set the human-readable names
	// from the company's field map (Polish primary + translations) so the catalog
	// displays them properly. Only definitions whose names actually differ are
	// rewritten, so re-imports are a no-op.
	//
	// Also cleans up old attribute definitions that have the mapped name as their
	// code (e.g. "Stan" instead of "attr_11323"). These are remnants from previous
	// imports that used the mapped name as the attribute key.
	if h.attrDefRepo != nil && len(fieldMap) > 0 {
		fmt.Printf("[IMPORT-JSON] Phase 0.5: Applying field map names (%d entries)...\n", len(fieldMap))
		applied := 0
		skipped := 0
		notFound := 0
		cleaned := 0
		for code, entry := range fieldMap {
			if entry.Skip {
				skipped++
				continue
			}
			if entry.Name == "" {
				fmt.Printf("[IMPORT-JSON] Phase 0.5: WARN: empty name for %s\n", code)
				skipped++
				continue
			}

			// Update the attribute definition with the localized names
			ad, err := h.attrDefRepo.GetByCode(code)
			if err != nil {
				notFound++
				continue // AttrDef absent (field never seen in a product) — skip.
			}
			needsUpdate := ad.NamePl != entry.Name ||
				(entry.NameRu != "" && ad.NameRu != entry.NameRu) ||
				(entry.NameUa != "" && ad.NameUa != entry.NameUa) ||
				(entry.NameEn != "" && ad.NameEn != entry.NameEn)
			if !needsUpdate {
				skipped++
				continue
			}
			if err := h.attrDefRepo.Update(code, func(a *model.AttrDef) {
				a.NamePl = entry.Name
				if entry.NameRu != "" {
					a.NameRu = entry.NameRu
				}
				if entry.NameUa != "" {
					a.NameUa = entry.NameUa
				}
				if entry.NameEn != "" {
					a.NameEn = entry.NameEn
				}
			}); err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: update AttrDef name for %s: %v\n", code, err)
			} else {
				applied++
				fmt.Printf("[IMPORT-JSON] Phase 0.5: Updated %s -> %s\n", code, entry.Name)
			}

			// Clean up old attribute definitions with the mapped name as their code
			// (e.g. "Stan" instead of "attr_11323"). These are remnants from previous
			// imports that used the mapped name as the attribute key.
			oldAd, err := h.attrDefRepo.GetByCode(entry.Name)
			if err == nil && oldAd.Code != code {
				// Old attribute definition exists with the mapped name as its code
				// Remove it to avoid duplicate attributes
				if err := h.attrDefRepo.Delete(oldAd.Code); err != nil {
					fmt.Printf("[IMPORT-JSON] WARN: delete old AttrDef %s: %v\n", oldAd.Code, err)
				} else {
					cleaned++
					fmt.Printf("[IMPORT-JSON] Phase 0.5: Cleaned up old AttrDef %s (replaced by %s)\n", oldAd.Code, code)

					if err := h.attrDefRepo.RemoveKeyFromList(code); err != nil {
						fmt.Printf("[IMPORT-JSON] WARN: delete old AttrDef %s: %v\n", oldAd.Code, err)
					}

				}

			}
		}
		fmt.Printf("[IMPORT-JSON] Phase 0.5: Applied %d, skipped %d, not found %d, cleaned %d\n", applied, skipped, notFound, cleaned)
	}

	// ============================================
	// Phase 1: Batch create/update products (IN TRANSACTION)
	// ============================================
	var allProducts []*model.Product

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

	if len(allParsedProducts) > 0 {
		fmt.Printf("[IMPORT-JSON] Phase 1: Creating/updating %d products (transactional)...\n", len(allParsedProducts))
		h.importProgress.SetStep(StepProducts)
		phase1Start := time.Now()

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

		// Flush any attribute codes created on-the-fly (description parser) to
		// the registry list in ONE batched write (covers the case where Phase 0
		// did not run but the parser still created new AttrDefs).
		if h.attrDefRepo != nil {
			if err := h.attrDefRepo.FlushList(); err != nil {
				fmt.Printf("[IMPORT-JSON] WARN: FlushList failed: %v\n", err)
			}
		}

		// Commit transaction (per-company data: products, indexes, EAN pages).
	}

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

	// Commit transaction (per-company data: products, indexes, EAN pages).
	h.importProgress.SetStep(StepCommit)
	if err := txn.Commit(); err != nil {
		fmt.Printf("[IMPORT-JSON] ERROR: commit transaction failed: %v\n", err)
		result.Status = "error_commit"
		return result
	}
	fmt.Println("[IMPORT-JSON] Transaction committed successfully")

	// Report feed fields that had no entry in the company's field map, so the
	// user knows which codes to add (manual analysis of the feed).
	if len(unmappedFields) > 0 {
		codes := make([]string, 0, len(unmappedFields))
		for code := range unmappedFields {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		fmt.Printf("[IMPORT-JSON] %s: %d feed field(s) not in the field map (add them in the company's Field Map): %s\n",
			company.Name, len(codes), strings.Join(codes, ", "))
	}

	result.Status = "completed"
	return result
}

// identityNormalize is the name normalization used by the JSON import for offer
// uniqueness keys. It is the identity function: the JSON import keys offers by
// their raw (unnormalized) name, matching keys created by earlier imports.
func identityNormalize(s string) string { return s }

// parseJSONProductForImport converts a jsonPriceEntry to a model.Product.
func parseJSONProductForImport(jp JsonProductFileItem, companyID int64, companySlug, companyName, currency string,
	attrDefCache map[string]*model.AttrDef, newAttrKeys map[string]struct{},
	attrDefRepo *db.AttrDefRepo, fieldMap map[string]model.FieldMapEntry, unmappedFields map[string]struct{}) (*model.Product, bool, error) {

	if jp.Name == "" {
		return nil, true, nil
	}

	// Extract price from the latest price history entry
	price := 0.0
	extractedCurrency := currency
	if len(jp.Offers) > 0 && len(jp.Offers[0].PriceHistory) > 0 {
		lastPrice := jp.Offers[0].PriceHistory[len(jp.Offers[0].PriceHistory)-1]
		price = pricesrc.ParsePrice(lastPrice.Price.Value)
		// Allegro prices are in cents — divide by 100 to get PLN
		// price = price / 100.0
		if lastPrice.Price.Currency != "" {
			extractedCurrency = lastPrice.Price.Currency
		}
	}

	if price <= 0 {
		return nil, true, nil
	}

	// Parse coded feed fields (e.g. Allegro "attr_<id>") using the company's
	// field map. Each mapped field becomes a first-class attribute: the field
	// code is the stable attribute code, and the map entry supplies the
	// human-readable name (applied to the AttrDef after Phase 0). Fields that
	// are absent from the map, or marked Skip, are ignored (fallback: the
	// description-based parsing above still runs).
	var attrs []model.KeyValue
	if len(jp.Fields) > 0 {
		for _, f := range jp.Fields {
			code := strings.TrimSpace(f.Name)
			if code == "" {
				continue
			}

			entry, ok := fieldMap[code]
			if !ok {
				// Field not in the map: record it so the import report can tell
				// the user which codes to add (manual analysis).
				if unmappedFields != nil {
					unmappedFields[code] = struct{}{}
				}
				continue
			}
			if v, ok := fieldMap[code]; ok && v.Name != "" {
				code = v.Name
			}
			if entry.Skip {
				continue
			}
			// Use SplitValues to handle comma-separated option lists (e.g.,
			// connectors: "HDMI, USB, Thunderbolt"). Each part is validated
			// individually, matching the behavior of the HTML attribute parser.
			for _, value := range attrsPkg.SplitValues(f.Value) {
				attrs = append(attrs, model.KeyValue{Key: code, Value: value})
			}
			newAttrKeys[code] = struct{}{}
		}
	}

	// EAN: prefer a valid EAN from the attributes (e.g. attr_225693 -> "EAN").
	// If not found, derive a STABLE identifier from the feed's sourceProductId,
	// prefixed with the company slug. sourceProductId is unique only within a
	// company, so the prefix makes it globally unique and stable across
	// re-imports. Never use the offer id (a UUID that changes on every feed
	// fetch — it would break in-place matching, prevent stock actualization,
	// and produce duplicate EAN pages).
	ean := ""
	for _, attr := range attrs {
		if attr.Key == "attr_225693" || attr.Key == "ean" || attr.Key == "EAN" {
			ean = strings.TrimSpace(attr.Value)
			break
		}
	}
	if ean == "" && len(jp.Offers) > 0 && strings.TrimSpace(jp.Offers[0].SourceProductID) != "" {
		prefix := strings.TrimSpace(companySlug)
		if prefix == "" {
			prefix = strconv.FormatInt(companyID, 10)
		}
		ean = prefix + "_" + strings.TrimSpace(jp.Offers[0].SourceProductID)
	}

	// Build name with company suffix
	name := jp.Name
	//if companyName != "" {
	//	name = name // + " — " + companyName
	//}

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
	if jp.Brand != "" {
		if nv := attrsPkg.NormalizeValue(jp.Brand); attrsPkg.ValidValue(nv) {
			attrs = append(attrs, model.KeyValue{Key: "brand", Value: nv})
			newAttrKeys["brand"] = struct{}{}
		}
	}

	// Clean the description but do NOT parse attributes from it for JSON feeds.
	// Description-based attribute parsing creates junk attributes (e.g. "Procesor",
	// "Ukryj", "Szukaj...", "0 GHz", "00 - 4") from random text in the description.
	// JSON/Allegro feeds already provide structured attributes via the "fields"
	// array, so description parsing is unnecessary and harmful.
	description := pricesrc.CleanHTMLDescription(jp.Description)

	// Drop duplicate (code, value) pairs from different sources.
	attrs = dedupeAttrPairs(attrs)

	// Extract shop category from the first category in the feed if present.
	shopCategory := ""
	if len(jp.Categories) > 0 && strings.TrimSpace(jp.Categories[0].Name) != "" {
		shopCategory = strings.TrimSpace(jp.Categories[0].Name)
	}

	p := &model.Product{
		EAN:          ean,
		Name:         name,
		Description:  description,
		CompanyID:    companyID,
		BrandID:      brandID,
		Brand:        jp.Brand,
		Price:        price,
		Currency:     extractedCurrency,
		StockQty:     0,
		Status:       model.ProductStatusActive,
		ProductURL:   "",
		Images:       images,
		Attributes:   attrs,
		ShopCategory: shopCategory,
		SEO: model.ProductSEO{
			Title: fmt.Sprintf("%s — MakoShop", name),
		},
	}

	if len(jp.Offers) > 0 {
		p.StockQty = int64(len(jp.Offers))
		// The offer's productUrl is the affiliate/partner purchase link (for
		// Tradedoubler feeds it is a tracking URL). Store it as PurchaseURL and
		// derive the direct product link for ProductURL.
		p.PurchaseURL = strings.TrimSpace(jp.Offers[0].ProductURL)
		p.ProductURL = extractDirectProductURL(jp.Offers[0].ProductURL)
	}

	return p, false, nil
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// readPriceFileJSON loads a price file and parses it into a jsonPriceFile.
// Allegro's "unlimited export" (the no-pagination, single-file download) is
// delivered as a ZIP archive containing the full JSON; when the file is a ZIP
// the inner JSON entry is extracted transparently. Plain JSON files are parsed
// as-is.
func readPriceFileJSON(path string) (jsonPriceFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return jsonPriceFile{}, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	// Peek at the first bytes to detect a ZIP archive (magic "PK\x03\x04").
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return jsonPriceFile{}, fmt.Errorf("read header: %w", err)
	}
	if hdr[0] == 'P' && hdr[1] == 'K' && hdr[2] == 0x03 && hdr[3] == 0x04 {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return jsonPriceFile{}, fmt.Errorf("seek: %w", err)
		}
		return openZipPriceJSON(f)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return jsonPriceFile{}, fmt.Errorf("seek: %w", err)
	}
	var out jsonPriceFile
	if err := json.NewDecoder(f).Decode(&out); err != nil {
		return jsonPriceFile{}, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

// openZipPriceJSON extracts the JSON entry from a ZIP archive and parses it.
// It prefers an entry whose name ends in ".json" and falls back to the first
// entry otherwise.
func openZipPriceJSON(f *os.File) (jsonPriceFile, error) {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return jsonPriceFile{}, fmt.Errorf("seek end: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return jsonPriceFile{}, fmt.Errorf("seek start: %w", err)
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return jsonPriceFile{}, fmt.Errorf("zip open: %w", err)
	}
	var target *zip.File
	for _, entry := range zr.File {
		if strings.HasSuffix(strings.ToLower(entry.Name), ".json") {
			target = entry
			break
		}
	}
	if target == nil && len(zr.File) > 0 {
		target = zr.File[0]
	}
	if target == nil {
		return jsonPriceFile{}, fmt.Errorf("zip has no entries")
	}
	rc, err := target.Open()
	if err != nil {
		return jsonPriceFile{}, fmt.Errorf("zip entry open: %w", err)
	}
	defer rc.Close()
	fmt.Printf("[IMPORT-JSON] Extracted ZIP entry %q (%d bytes) from %s\n", target.Name, target.UncompressedSize64, f.Name())
	var out jsonPriceFile
	if err := json.NewDecoder(rc).Decode(&out); err != nil {
		return jsonPriceFile{}, fmt.Errorf("decode zip entry: %w", err)
	}
	return out, nil
}

// loadAllegroFieldMap loads the default Allegro field map from
// docs/allegro_field_map.json. Returns nil if the file cannot be read or parsed.
func loadAllegroFieldMap() map[string]model.FieldMapEntry {
	// Try to find the file relative to the working directory
	filePath := "docs/allegro_field_map.json"
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Try relative to the source root (if running from a different directory)
		filePath = "../docs/allegro_field_map.json"
		data, err = os.ReadFile(filePath)
		if err != nil {
			return nil
		}
	}

	var raw struct {
		Fields  map[string]string `json:"fields"`
		Special map[string]string `json:"special"`
		Skip    []string          `json:"skip"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	fieldMap := make(map[string]model.FieldMapEntry)
	// Load regular fields
	for code, name := range raw.Fields {
		fieldMap[code] = model.FieldMapEntry{Name: name}
	}
	// Load special fields (EAN, GTIN, category_id) — these are also first-class attributes
	if raw.Special != nil {
		for code, name := range raw.Special {
			fieldMap[code] = model.FieldMapEntry{Name: name}
		}
	}
	// Load skip list
	for _, code := range raw.Skip {
		fieldMap[code] = model.FieldMapEntry{Skip: true}
	}
	return fieldMap
}

// extractDirectProductURL returns the direct product link from an offer URL.
// Tradedoubler feeds carry a tracking URL of the form
// "https://pdt.tradedoubler.com/click?...url(<url-encoded direct link>)"; the
// direct link is extracted and decoded from the url(...) part. If the URL is
// not a tracking URL (no url(...) part), it is returned as-is (already direct).
func extractDirectProductURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	const marker = "url("
	idx := strings.LastIndex(raw, marker)
	if idx < 0 {
		return raw // not a tracking URL — already a direct link
	}
	rest := raw[idx+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return raw
	}
	encoded := rest[:end]
	if decoded, err := url.PathUnescape(encoded); err == nil && decoded != "" {
		return decoded
	}
	return encoded
}
