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
	Company         string `json:"company"`
	Format          string `json:"format"`
	Status          string `json:"status"`
	Files           int    `json:"files,omitempty"`
	OffersParsed    int    `json:"offers_parsed"`
	ProductsCreated int    `json:"products_created"`
	ProductsUpdated int    `json:"products_updated"`
	ProductsSkipped int    `json:"products_skipped"`
	ProductsDeleted int    `json:"products_deleted,omitempty"`
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
// If companyParam is non-empty it resolves a single company by ID or name;
// otherwise it returns all companies that have an ImportURL or ImportFolder
// configured. Shared by the Nokaut, JSON and unified import handlers.
func (h *Handlers) resolveImportCompanies(companyParam string) ([]model.Company, error) {
	all, err := h.companyRepo.List()
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}

	var companies []model.Company
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
func (h *Handlers) importCompanyByFormat(company *model.Company, override string, limit int, noDownload bool) CompanyImportResult {
	format := effectiveImportFormat(company, override)
	cr := CompanyImportResult{Company: company.Name, Format: format}

	switch format {
	case "json":
		r := h.importJSONCompany(company, limit, noDownload)
		cr.Status = r.Status
		cr.Files = r.Files
		cr.OffersParsed = r.OffersParsed
		cr.ProductsCreated = r.ProductsCreated
		cr.ProductsUpdated = r.ProductsUpdated
		cr.ProductsSkipped = r.ProductsSkipped
		cr.ProductsDeleted = r.ProductsDeleted
	case "nokaut", "xml":
		r := h.importNokautCompany(company, limit)
		cr.Status = r.Status
		cr.Files = r.Files
		cr.OffersParsed = r.OffersParsed
		cr.ProductsCreated = r.ProductsCreated
		cr.ProductsUpdated = r.ProductsUpdated
		cr.ProductsSkipped = r.ProductsSkipped
		cr.ProductsDeleted = r.ProductsDeleted
	default:
		// Folder-based formats (csv/normalized/multi) are handled by their
		// dedicated endpoints; report them so the caller can act.
		cr.Status = "use_dedicated_endpoint"
	}
	return cr
}

// HandleAdminImportUnified imports price files for one or all companies in a
// single call. Each company's file is parsed using the method stored in its
// PriceSource.Format (saved in the DB), so a batch of price lists can be
// imported with one command.
//
// POST /admin/import-unified
// Query params:
//   - company=ID_OR_NAME  import only this company (default: all companies with ImportURL or ImportFolder set)
//   - source=...          explicit format override for all companies (default: use each company's saved Format)
//   - limit=N             max offers to import per company (0 = unlimited)
//   - no_download=1       skip download, use local file (json/nokaut)
func (h *Handlers) HandleAdminImportUnified(w http.ResponseWriter, r *http.Request) {
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
	sourceOverride := r.URL.Query().Get("source")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	noDownload := r.URL.Query().Get("no_download") == "1"

	startTime := time.Now()
	fmt.Printf("[IMPORT-UNIFIED] Starting (company=%q source=%q limit=%d no_download=%t) from %s\n",
		companyParam, sourceOverride, limit, noDownload, r.RemoteAddr)

	companies, err := h.resolveImportCompanies(companyParam)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	if len(companies) == 0 {
		httpres.WriteJSON(w, http.StatusOK, UnifiedImportResult{Status: "no_companies"})
		return
	}

	result := UnifiedImportResult{Status: "completed"}
	for i := range companies {
		company := &companies[i]
		fmt.Printf("[IMPORT-UNIFIED] Importing company: %s (ID=%d, format=%q, url=%q, folder=%q)\n",
			company.Name, company.ID, effectiveImportFormat(company, sourceOverride), company.ImportURL, company.ImportFolder)

		cr := h.importCompanyByFormat(company, sourceOverride, limit, noDownload)
		result.Companies = append(result.Companies, cr)
		result.OffersParsed += cr.OffersParsed
		result.ProductsCreated += cr.ProductsCreated
		result.ProductsUpdated += cr.ProductsUpdated
		result.ProductsSkipped += cr.ProductsSkipped
		result.ProductsDeleted += cr.ProductsDeleted

		fmt.Printf("[IMPORT-UNIFIED] Company %s (%s): status=%s parsed=%d created=%d updated=%d skipped=%d\n",
			company.Name, cr.Format, cr.Status, cr.OffersParsed, cr.ProductsCreated, cr.ProductsUpdated, cr.ProductsSkipped)
	}

	if result.Status == "completed" && result.OffersParsed == 0 && result.ProductsCreated == 0 && result.ProductsUpdated == 0 {
		result.Status = "no_products"
	}

	fmt.Printf("[IMPORT-UNIFIED] Completed %d companies in %v (parsed=%d created=%d updated=%d)\n",
		len(companies), time.Since(startTime), result.OffersParsed, result.ProductsCreated, result.ProductsUpdated)

	httpres.WriteJSON(w, http.StatusOK, result)
}
