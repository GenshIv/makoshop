package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// CompanyImportResult holds the result of importing a single company's price
// file using its configured format.
type CompanyImportResult struct {
	Company          string  `json:"company"`
	Format           string  `json:"format"`
	Status           string  `json:"status"`
	Files            int     `json:"files,omitempty"`
	OffersParsed     int     `json:"offers_parsed"`
	ProductsCreated  int     `json:"products_created"`
	ProductsUpdated  int     `json:"products_updated"`
	ProductsSkipped  int     `json:"products_skipped"`
	ProductsDeleted  int     `json:"products_deleted,omitempty"`
	AffectedEANPages []int64 `json:"-"` // EAN page IDs affected by this import (not serialized)
}

// UnifiedImportResult holds the result of a batch import across one or more
// companies, where each company's price file is parsed using the method stored
// in its PriceSource.Format (or an explicit source override).
type UnifiedImportResult struct {
	Status          string                `json:"status"`
	Companies       []CompanyImportResult `json:"companies"`
	OffersParsed    int                   `json:"offers_parsed"`
	ProductsCreated int                   `json:"products_created"`
	ProductsUpdated int                   `json:"products_updated"`
	ProductsSkipped int                   `json:"products_skipped"`
	ProductsDeleted int                   `json:"products_deleted"`
}

// resolveImportCompanies resolves the target companies for an import.
//   - If companiesParam (comma-separated IDs) is non-empty, resolve exactly
//     those companies, in the given order (used by the UI checkbox selection).
//   - Else if companyParam is non-empty, resolve a single company by ID or name.
//   - Otherwise, return all companies that have an ImportURL or ImportFolder.
func (h *Handlers) resolveImportCompanies(companyParam, companiesParam string) ([]model.Company, error) {
	all, err := h.companyRepo.List()
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	byID := make(map[int64]model.Company, len(all))
	for _, c := range all {
		byID[c.ID] = c
	}

	var companies []model.Company

	// Explicit list of company IDs (checkbox selection).
	if companiesParam != "" {
		for _, part := range strings.Split(companiesParam, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			cid, parseErr := strconv.ParseInt(part, 10, 64)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid company id: %s", part)
			}
			c, ok := byID[cid]
			if !ok {
				return nil, fmt.Errorf("company not found: %s", part)
			}
			companies = append(companies, c)
		}
		if len(companies) == 0 {
			return nil, fmt.Errorf("no companies selected")
		}
		return companies, nil
	}

	if companyParam != "" {
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
			return nil, fmt.Errorf("company not found: %s", companyParam)
		}
		return companies, nil
	}

	// All companies that have an import source configured.
	for _, c := range all {
		if c.ImportURL != "" || c.ImportFolder != "" {
			companies = append(companies, c)
		}
	}
	return companies, nil
}

// effectiveImportFormat returns the import method to use for a company. An
// explicit override (the source query param) wins; otherwise the company's
// saved PriceSource.Format is used; the default is "nokaut".
func effectiveImportFormat(company *model.Company, override string) string {
	format := strings.ToLower(strings.TrimSpace(override))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(company.PriceSource.Format))
	}
	if format == "" {
		format = "nokaut"
	}
	return format
}

// importCompanyByFormat imports a single company's price file using the method
// resolved from its saved PriceSource.Format (or an explicit override). It
// dispatches to the per-company import implementation for the format.
//
// When the company has a price URL, the file is downloaded once and its real
// format is detected from the content. If the detected format disagrees with
// the saved one, the detected format wins (the file content is the ground
// truth) and the saved format is corrected so future imports and the UI agree.
// This is what makes the one-click auto-import resilient to a wrong saved
// format (e.g. a JSON file that was saved with format "nokaut").
func (h *Handlers) importCompanyByFormat(company *model.Company, override string, limit int, noDownload bool) CompanyImportResult {
	format := effectiveImportFormat(company, override)
	if format == "xml" {
		format = "nokaut"
	}
	cr := CompanyImportResult{Company: company.Name, Format: format}

	explicitFile := ""
	// Paginated APIs (Tradedoubler) are walked page by page inside the JSON
	// importer, so there is no single file to pre-download for format
	// detection. Skip it to avoid a wasteful (and possibly failing) request.
	// Allegro feeds are the same: a plain single-file GET of the feed URL would
	// capture only the FIRST page (e.g. 100 of 5772 products). Let
	// importJSONCompany own the download so it can walk every page (capped, or
	// the full feed in single-file mode per the company's DisablePagination).
	isAllegro := isAllegroFeed(company) || isAllegroURL(company.ImportURL)
	if company.ImportURL != "" && !noDownload && !isTradedoublerURL(company.ImportURL) && !isAllegro {
		// Download once and let the content choose the parser.
		if path, err := downloadPriceFile(company); err == nil {
			explicitFile = path
			if detected := detectPriceFormat(path); detected != "" && detected != format {
				fmt.Printf("[IMPORT] %s: saved format %q but file is %q; using detected format\n", company.Name, format, detected)
				format = detected
				company.PriceSource.Format = detected
				h.correctCompanyFormat(company, detected)
			}
		} else {
			fmt.Printf("[IMPORT] %s: download failed: %v\n", company.Name, err)
		}
	}

	cr.Format = format
	switch format {
	case "json", "allegro":
		// "allegro" is a JSON feed with coded fields; it is downloaded page by
		// page (capped) into a file inside importJSONCompany, then parsed with
		// the company's field map.
		r := h.importJSONCompany(company, limit, noDownload, explicitFile)
		cr.Status = r.Status
		cr.Files = r.Files
		cr.OffersParsed = r.OffersParsed
		cr.ProductsCreated = r.ProductsCreated
		cr.ProductsUpdated = r.ProductsUpdated
		cr.ProductsSkipped = r.ProductsSkipped
		cr.ProductsDeleted = r.ProductsDeleted
		cr.AffectedEANPages = r.AffectedEANPages
	case "nokaut", "xml":
		r := h.importNokautCompany(company, limit, explicitFile)
		cr.Status = r.Status
		cr.Files = r.Files
		cr.OffersParsed = r.OffersParsed
		cr.ProductsCreated = r.ProductsCreated
		cr.ProductsUpdated = r.ProductsUpdated
		cr.ProductsSkipped = r.ProductsSkipped
		cr.ProductsDeleted = r.ProductsDeleted
		cr.AffectedEANPages = r.AffectedEANPages
	default:
		// Folder-based formats (csv/normalized/multi) are handled by their
		// dedicated endpoints; report them so the caller can act.
		cr.Status = "use_dedicated_endpoint"
	}
	return cr
}

// correctCompanyFormat persists a corrected PriceSource.Format for a company.
// A failure here is non-fatal: the next import will simply detect again.
func (h *Handlers) correctCompanyFormat(company *model.Company, format string) {
	if h.companyRepo == nil {
		return
	}
	if err := h.companyRepo.Update(company.ID, func(c *model.Company) {
		c.PriceSource.Format = format
	}); err != nil {
		fmt.Printf("[IMPORT] %s: failed to persist corrected format %q: %v\n", company.Name, format, err)
	}
}

// runGlobalRecalculation runs the company-independent (global) recalculations
// exactly once, after all per-company imports have committed. These steps do
// not depend on any single company, so running them per company (as the import
// used to) was wasteful and re-did the same global work N times. Consolidating
// them into one pass after the batch saves time and removes redundant work.
//
// If affectedEANPages is provided (non-empty), only those EAN pages are
// recalculated for product counts and min prices. Otherwise, all pages are
// recalculated (full rebuild).
//
// Steps (all non-transactional, operating on the committed data):
//   - EAN page product counts (incremental if affectedEANPages provided)
//   - EAN page min prices (incremental if affectedEANPages provided)
//   - category trees
//   - (sort indexes temporarily disabled — slow, needs optimization)
func (h *Handlers) runGlobalRecalculation(affectedEANPages []int64) error {
	if h.eanPageRepo != nil {
		if len(affectedEANPages) > 0 {
			fmt.Printf("[IMPORT] Incremental recalculation for %d affected EAN pages\n", len(affectedEANPages))
			if err := h.eanPageRepo.RecalculateProductCountsForPages(affectedEANPages); err != nil {
				return fmt.Errorf("recalculate product counts: %w", err)
			}
			if err := h.eanPageRepo.RecalculateMinPricesForPages(affectedEANPages, h.productRepo); err != nil {
				return fmt.Errorf("recalculate min prices: %w", err)
			}
		} else {
			fmt.Println("[IMPORT] Full recalculation for all EAN pages")
			if err := h.eanPageRepo.RecalculateProductCounts(); err != nil {
				return fmt.Errorf("recalculate product counts: %w", err)
			}
			if err := h.eanPageRepo.RecalculateMinPrices(h.productRepo); err != nil {
				return fmt.Errorf("recalculate min prices: %w", err)
			}
		}
	}
	// Sort index rebuilding disabled temporarily — slow and needs optimization.
	// Will be re-enabled after proper incremental approach is implemented.
	// if h.turboSearch != nil {
	// 	if err := h.turboSearch.BuildSortIndexes(); err != nil {
	// 		return fmt.Errorf("build product sort indexes: %w", err)
	// 	}
	// }
	if h.categoryRepo != nil {
		h.categoryRepo.RebuildTrees()
	}
	// if h.eanPageSearch != nil {
	// 	if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
	// 		return fmt.Errorf("build EAN page sort indexes: %w", err)
	// 	}
	// 	if err := h.eanPageSearch.RecalculateDeliveryMethods(h.companyRepo, h.deliveryMethodRepo); err != nil {
	// 		fmt.Printf("[IMPORT] WARN: recalculate delivery methods: %v\n", err)
	// 	}
	// }
	return nil
}

// HandleAdminImportUnified imports price files for one or all companies. Each
// company's file is parsed using the method stored in its PriceSource.Format
// (saved in the DB), so a batch of price lists can be imported with one command.
//
// The import is asynchronous: the handler validates the request, starts the run
// (and its live progress), then returns 202 immediately. The actual download /
// parse / write happens in a background goroutine, and the client observes it
// by polling GET /admin/import-progress until running=false.
//
// Why async: a batch import (especially with large files such as a 400MB price
// list) can take minutes. Keeping the HTTP connection open for that whole time
// meant a browser or proxy could time out and retry the request; once the first
// run finished and released the import lock, the retry started a SECOND import
// from the beginning (the "it went again" symptom). Returning immediately keeps
// the request short and makes the run fully server-side.
//
// POST /admin/import-unified
// Query params:
//   - company=ID_OR_NAME  import only this company (default: all companies with ImportURL or ImportFolder set)
//   - source=...          explicit format override for all companies (default: use each company's saved Format)
//   - limit=N             max offers to import per company (0 = unlimited)
//   - no_download=1       skip download, use local file (json/nokaut)
//
// Response: 202 {status:"started"} while the run is in flight; 409 if another
// import is already running; 404 if the company is unknown; 200 {status:
// "no_companies"} if there is nothing to import.
func (h *Handlers) HandleAdminImportUnified(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Prevent concurrent imports. The lock is held by the background goroutine
	// for the whole run, so a second request gets 409 while the first is still
	// in flight (it does not start a duplicate run).
	if !h.importMu.TryLock() {
		httpres.WriteError(w, http.StatusConflict, "IMPORT_IN_PROGRESS", "Another import is already in progress")
		return
	}

	companyParam := r.URL.Query().Get("company")
	companiesParam := r.URL.Query().Get("companies")
	sourceOverride := r.URL.Query().Get("source")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	noDownload := r.URL.Query().Get("no_download") == "1"

	fmt.Printf("[IMPORT-UNIFIED] Starting (company=%q companies=%q source=%q limit=%d no_download=%t) from %s\n",
		companyParam, companiesParam, sourceOverride, limit, noDownload, r.RemoteAddr)

	companies, err := h.resolveImportCompanies(companyParam, companiesParam)
	if err != nil {
		// No run started yet (Begin is called after resolution), so there is
		// nothing to mark as failed in the progress tracker.
		h.importMu.Unlock()
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	if len(companies) == 0 {
		h.importMu.Unlock()
		httpres.WriteJSON(w, http.StatusOK, UnifiedImportResult{Status: "no_companies"})
		return
	}

	// Start tracking live progress for this batch run so the client can see it
	// is running immediately after the 202 response. The total includes one
	// extra slot for the final GLOBAL recalculation phase, which runs ONCE
	// after all per-company imports (not per company).
	h.importProgress.Begin(len(companies) + 1)

	// Run the import in the background. The goroutine owns the import lock for
	// the duration of the run and finalizes the progress tracker when done.
	go func() {
		defer h.importMu.Unlock()
		defer h.importProgress.Finish()

		startTime := time.Now()
		result := UnifiedImportResult{Status: "completed"}

		// Collect affected EAN pages across all companies for incremental recalculation
		affectedEANPages := make(map[int64]struct{})

		for i := range companies {
			company := &companies[i]
			format := effectiveImportFormat(company, sourceOverride)
			fmt.Printf("[IMPORT-UNIFIED] Importing company: %s (ID=%d, format=%q, url=%q, folder=%q)\n",
				company.Name, company.ID, format, company.ImportURL, company.ImportFolder)

			// Mark this company as active before importing it.
			h.importProgress.SetCompany(i+1, company.Name, format)

			cr := h.importCompanyByFormat(company, sourceOverride, limit, noDownload)
			result.Companies = append(result.Companies, cr)
			result.OffersParsed += cr.OffersParsed
			result.ProductsCreated += cr.ProductsCreated
			result.ProductsUpdated += cr.ProductsUpdated
			result.ProductsSkipped += cr.ProductsSkipped
			result.ProductsDeleted += cr.ProductsDeleted

			// Collect affected EAN pages for this company
			for _, id := range cr.AffectedEANPages {
				affectedEANPages[id] = struct{}{}
			}

			// Finalize this company's progress entry.
			state := companyResultState(cr)
			errMsg := ""
			if state == CompanyStateFailed {
				errMsg = cr.Status
			}
			h.importProgress.CompanyDone(i+1, state, errMsg)

			fmt.Printf("[IMPORT-UNIFIED] Company %s (%s): status=%s parsed=%d created=%d updated=%d skipped=%d\n",
				company.Name, cr.Format, cr.Status, cr.OffersParsed, cr.ProductsCreated, cr.ProductsUpdated, cr.ProductsSkipped)
		}

		// Convert map to slice for runGlobalRecalculation
		affectedSlice := make([]int64, 0, len(affectedEANPages))
		for id := range affectedEANPages {
			affectedSlice = append(affectedSlice, id)
		}

		// Global recalculation phase: runs ONCE after all companies have
		// imported (not per company), so the same global work (EAN page counts,
		// min prices, sort indexes, category trees, delivery methods) is not
		// repeated for every company. Tracked as the final progress entry.
		globalIdx := len(companies) + 1
		h.importProgress.SetCompany(globalIdx, "Global recalculation", "")
		h.importProgress.SetStep(StepRecalc)
		if err := h.runGlobalRecalculation(affectedSlice); err != nil {
			fmt.Printf("[IMPORT-UNIFIED] ERROR: global recalculation failed: %v\n", err)
			h.importProgress.CompanyDone(globalIdx, CompanyStateFailed, err.Error())
			h.importProgress.Fail(err.Error())
			return
		}
		h.importProgress.CompanyDone(globalIdx, CompanyStateCompleted, "")

		if result.Status == "completed" && result.OffersParsed == 0 && result.ProductsCreated == 0 && result.ProductsUpdated == 0 {
			result.Status = "no_products"
		}

		fmt.Printf("[IMPORT-UNIFIED] Completed %d companies + global recalc in %v (parsed=%d created=%d updated=%d)\n",
			len(companies), time.Since(startTime), result.OffersParsed, result.ProductsCreated, result.ProductsUpdated)
	}()

	// Return immediately: the import is now running in the background. The
	// client polls /admin/import-progress for live status and the final result.
	httpres.WriteJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"message": "Import started in the background. Poll /admin/import-progress for status.",
	})
}

// companyResultState maps a CompanyImportResult status to a progress state.
func companyResultState(cr CompanyImportResult) string {
	switch cr.Status {
	case "completed", "":
		return CompanyStateCompleted
	case "use_dedicated_endpoint", "no_files", "no_products":
		return CompanyStateSkipped
	default:
		// download_error, error_* and anything else is a failure.
		return CompanyStateFailed
	}
}
