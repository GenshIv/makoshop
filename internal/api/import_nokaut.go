package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
}

// pricesDir is the root directory for company price files.
const pricesDir = "prices"

// HandleAdminImportNokaut imports offers from Nokaut XML price files.
// It is idempotent: existing offers (EAN + name + company) are updated in place,
// never duplicated.
//
// POST /admin/import-nokaut
// Query params:
//   - company=ID_OR_NAME   import only this company (default: all companies with ImportFolder set)
//   - limit=N              max offers to import per company (0 = unlimited)
func (h *Handlers) HandleAdminImportNokaut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	companyParam := r.URL.Query().Get("company")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}

	startTime := time.Now()
	fmt.Printf("[IMPORT-NOKAUT] Starting (company=%q limit=%d)\n", companyParam, limit)

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
		// All companies that have an ImportFolder configured
		for _, c := range all {
			if c.ImportFolder != "" {
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
		fmt.Printf("[IMPORT-NOKAUT] Importing company: %s (ID=%d, folder=%q)\n", company.Name, company.ID, company.ImportFolder)

		result := h.importNokautCompany(company, limit)
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
func (h *Handlers) importNokautCompany(company *model.Company, limit int) NokautImportResult {
	cfg := company.PriceSource
	applyPriceSourceDefaults(&cfg)

	currency := cfg.Currency
	if currency == "" {
		currency = company.Settings.Currency
	}
	if currency == "" {
		currency = "PLN"
	}

	// Find XML files
	// Clean the ImportFolder path: remove leading/trailing slashes and "prices/" prefix
	importFolder := company.ImportFolder
	importFolder = strings.TrimPrefix(importFolder, "/")
	importFolder = strings.TrimSuffix(importFolder, "/")
	importFolder = strings.TrimPrefix(importFolder, "prices/")

	dir := filepath.Join(pricesDir, importFolder)
	files, err := filepath.Glob(filepath.Join(dir, "*.xml"))
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

	parser := pricesrc.NewNokautParser()

	var result NokautImportResult
	result.Files = len(files)

	var allProducts []*model.Product

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
		fmt.Printf("[IMPORT-NOKAUT] Parsing %s...\n", filepath.Base(file))

		var fileProducts []*model.Product
		var fileParsed int
		var fileSkipped int

		_, err = parser.Parse(f, func(offer pricesrc.Offer) error {
			if limit > 0 && result.OffersParsed >= limit {
				return fmt.Errorf("limit reached")
			}
			result.OffersParsed++
			fileParsed++

			p := mapOfferToProduct(offer, cfg, company.ID, currency)
			if p == nil {
				fileSkipped++
				result.ProductsSkipped++
				return nil
			}

			id, isNew, err := h.productRepo.GetOrCreateByEAN(p, pricesrc.NormalizeName(p.Name))
			if err != nil {
				fmt.Printf("[IMPORT-NOKAUT] WARN: GetOrCreateByEAN %s: %v\n", p.Name, err)
				fileSkipped++
				result.ProductsSkipped++
				return nil
			}

			if isNew {
				result.ProductsCreated++
			} else {
				result.ProductsUpdated++
			}

			// For EANPage upsert we need the final product state.
			// New products already have all fields; existing ones may have
			// merged attributes, so fetch them.
			var final *model.Product
			if isNew {
				final = p
			} else {
				if prod, err := h.productRepo.Get(id); err == nil {
					final = prod
				} else {
					final = p
				}
			}
			fileProducts = append(fileProducts, final)
			return nil
		})

		f.Close()

		if err != nil && err.Error() != "limit reached" {
			fmt.Printf("[IMPORT-NOKAUT] WARN: parse %s: %v (parsed %d before error)\n", filepath.Base(file), err, fileParsed)
		}

		allProducts = append(allProducts, fileProducts...)
		fmt.Printf("[IMPORT-NOKAUT]   %s: parsed=%d skipped=%d in %v\n",
			filepath.Base(file), fileParsed, fileSkipped, time.Since(fileStart))
	}

	// ============================================
	// Phase 2: Batch upsert EAN pages + index
	// ============================================
	fmt.Printf("[IMPORT-NOKAUT] Phase 2: Upserting EAN pages for %d products...\n", len(allProducts))
	phase2Start := time.Now()

	if err := h.eanPageRepo.LoadCatalogizerCache(); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] WARN: load catalogizer cache: %v\n", err)
	}
	h.eanPageRepo.BatchUpsertFromProducts(allProducts)

	if h.eanPageSearch != nil {
		allPages, _ := h.eanPageRepo.ListAll()
		pagePtrs := make([]*model.EANPage, len(allPages))
		for i := range allPages {
			pagePtrs[i] = &allPages[i]
		}
		if err := h.eanPageSearch.IndexEANPageBatch(pagePtrs); err != nil {
			fmt.Printf("[IMPORT-NOKAUT] WARN: index EAN pages: %v\n", err)
		}
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-NOKAUT] WARN: build EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-NOKAUT] Phase 2: EAN pages done in %v\n", time.Since(phase2Start))

	// ============================================
	// Phase 3: Recalculate EAN page counts + min prices
	// ============================================
	fmt.Println("[IMPORT-NOKAUT] Phase 3: Recalculating EAN page counts and min prices...")
	phase3Start := time.Now()
	if err := h.eanPageRepo.RecalculateProductCounts(); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] WARN: recalculate product counts: %v\n", err)
	}
	if err := h.eanPageRepo.RecalculateMinPrices(h.productRepo); err != nil {
		fmt.Printf("[IMPORT-NOKAUT] WARN: recalculate min prices: %v\n", err)
	}
	if h.eanPageSearch != nil {
		if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-NOKAUT] WARN: rebuild EAN page sort indexes: %v\n", err)
		}
	}
	fmt.Printf("[IMPORT-NOKAUT] Phase 3: done in %v\n", time.Since(phase3Start))

	// ============================================
	// Phase 4: Build product sort indexes
	// ============================================
	fmt.Println("[IMPORT-NOKAUT] Phase 4: Building product sort indexes...")
	if h.turboSearch != nil {
		if err := h.turboSearch.BuildSortIndexes(); err != nil {
			fmt.Printf("[IMPORT-NOKAUT] WARN: build product sort indexes: %v\n", err)
		}
	}

	// ============================================
	// Phase 5: Rebuild category trees
	// ============================================
	fmt.Println("[IMPORT-NOKAUT] Phase 5: Rebuilding category trees...")
	h.categoryRepo.RebuildTrees()

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
func mapOfferToProduct(offer pricesrc.Offer, cfg model.PriceSourceConfig, companyID int64, currency string) *model.Product {
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

	// Attributes: extra fields from config + product_url + shop_category
	var attrs []model.KeyValue
	for _, af := range cfg.AttrFields {
		if val := strings.TrimSpace(offer.Props[af.Field]); val != "" {
			attrs = append(attrs, model.KeyValue{Key: af.Code, Value: val})
		}
	}
	if productURL != "" {
		attrs = append(attrs, model.KeyValue{Key: "product_url", Value: productURL})
	}
	if shopCategory != "" {
		attrs = append(attrs, model.KeyValue{Key: "shop_category", Value: shopCategory})
	}

	// Parse attributes from HTML description using configured rules
	if len(cfg.HTMLAttrRules) > 0 && offer.Description != "" {
		htmlParser := pricesrc.NewHTMLAttrParser(cfg.HTMLAttrRules)
		htmlAttrs := htmlParser.Parse(offer.Description)
		for code, value := range htmlAttrs {
			attrs = append(attrs, model.KeyValue{Key: code, Value: value})
		}
	}

	// Clean HTML from description (strip tags, keep text)
	description := pricesrc.StripHTMLEntities(strings.TrimSpace(offer.Description))
	if len(description) > 2000 {
		description = description[:2000]
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
		Attributes:    attrs,
		Images:        images,
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
