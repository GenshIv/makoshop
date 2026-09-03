package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

// handlers_admin.go holds the per-route handler methods for /admin/* routes.
// Each method wraps one route's logic (method checks, path dispatch, auth
// delegation) so registerRoutes stays a clean, readable route table.

func (d *Deps) maintence(w http.ResponseWriter, r *http.Request) {
	type resp struct {
		Enabled        bool `json:"enabled"`
		AutoDisable    bool `json:"auto_disable"`
		PreviousEnable bool `json:"previous_enabled,omitempty"`
	}
	type req struct {
		Enable      bool `json:"enable"`
		AutoDisable bool `json:"auto_disable"`
	}

	if r.Method == http.MethodGet {
		httpres.WriteJSON(w, http.StatusOK, resp{
			Enabled:     maintenanceEnabled,
			AutoDisable: maintenanceAutoDisable,
		})
		return
	}

	if r.Method == http.MethodPost {
		var body req
		_ = json.NewDecoder(r.Body).Decode(&body)

		prev := maintenanceEnabled
		maintenanceEnabled = body.Enable
		if body.Enable {
			maintenanceAutoDisable = body.AutoDisable
		} else {
			maintenanceAutoDisable = false
		}

		fmt.Printf("[MAINTENANCE] mode changed: enabled=%v auto_disable=%v (prev=%v)\n",
			maintenanceEnabled, maintenanceAutoDisable, prev)

		httpres.WriteJSON(w, http.StatusOK, resp{
			Enabled:        maintenanceEnabled,
			AutoDisable:    maintenanceAutoDisable,
			PreviousEnable: prev,
		})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Admin users endpoints (requires admin role) ---

// GET /admin/users
func (d *Deps) adminUsers(w http.ResponseWriter, r *http.Request) {
	d.AuthHandlers.HandleAdminUsersList(w, r)
}

// GET/PATCH /admin/users/{id}
func (d *Deps) adminUser(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.AuthHandlers.HandleAdminUserGet(w, r)
	case http.MethodPatch:
		d.AuthHandlers.HandleAdminUserUpdate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Admin companies endpoints ---

// GET/POST /admin/companies
func (d *Deps) adminCompanies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.AuthHandlers.HandleAdminCompaniesList(w, r)
	case http.MethodPost:
		d.AuthHandlers.HandleAdminCompanyCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /admin/companies/create-test — create test companies
func (d *Deps) adminCompaniesCreateTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.AuthHandlers.HandleAdminCreateTestCompanies(w, r)
}

// GET /admin/companies/{id}/settings — public (company with full settings);
// everything else under /admin/companies/{id} requires admin role
func (d *Deps) adminCompany(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/settings") && r.Method == http.MethodGet {
		d.Handlers.HandleCompanyGetWithSettings(w, r)
		return
	}

	// Everything else under /admin/companies/{id} requires admin role
	d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(path, "/verify") {
			if r.Method == http.MethodPatch {
				d.AuthHandlers.HandleAdminCompanyVerify(w, r)
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasSuffix(path, "/export") {
			if r.Method == http.MethodGet {
				d.AuthHandlers.HandleAdminCompanyExport(w, r)
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasSuffix(path, "/field-map/seed") {
			if r.Method == http.MethodPost {
				d.AuthHandlers.HandleAdminCompanyFieldMapSeed(w, r)
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.Method {
		case http.MethodGet:
			d.AuthHandlers.HandleAdminCompanyGet(w, r)
		case http.MethodPatch:
			d.AuthHandlers.HandleAdminCompanyUpdate(w, r)
		case http.MethodDelete:
			d.AuthHandlers.HandleAdminCompanyDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin).ServeHTTP(w, r)
}

// POST /admin/companies/import — import company config from JSON
func (d *Deps) adminCompaniesImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.AuthHandlers.HandleAdminCompanyImport(w, r)
}

// GET /admin/companies/export-all — export all companies as JSON
func (d *Deps) adminCompaniesExportAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.AuthHandlers.HandleAdminCompaniesExportAll(w, r)
}

// POST /admin/companies/import-all — import all companies from JSON
func (d *Deps) adminCompaniesImportAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.AuthHandlers.HandleAdminCompaniesImportAll(w, r)
}

// GET /admin/price-sources — list price source configs
func (d *Deps) adminPriceSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.AuthHandlers.HandleAdminPriceSourcesList(w, r)
}

// --- Admin Analytics ---
func (d *Deps) adminAnalyticsOrders(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAnalyticsOrders(w, r)
}

func (d *Deps) adminAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAnalyticsOverview(w, r)
}

func (d *Deps) adminAnalyticsProducts(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAnalyticsProducts(w, r)
}

func (d *Deps) adminAnalyticsSearchQueries(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAnalyticsSearchQueries(w, r)
}

// --- Admin Promo campaigns ---

// GET/POST /admin/promo/campaigns
func (d *Deps) adminPromoCampaigns(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleAdminPromoCampaignsList(w, r)
		return
	}
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminPromoCampaignCreate(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// PATCH /admin/promo/campaigns/{id}
func (d *Deps) adminPromoCampaign(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch {
		d.Handlers.HandleAdminPromoCampaignUpdate(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Admin Product Import ---

// POST /admin/import-prices — import from CSV files in _tmp/prices
func (d *Deps) adminImportPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminImportPrices(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/import-nokaut — import from Nokaut XML price files in prices/
func (d *Deps) adminImportNokaut(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminImportNokaut(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// GET /admin/field-map/default — confirmed default Allegro field map
func (d *Deps) adminFieldMapDefault(w http.ResponseWriter, r *http.Request) {
	d.AuthHandlers.HandleAdminFieldMapDefault(w, r)
}

// POST /admin/import-unified — batch import: each company's price file is
// parsed using the method stored in its PriceSource.Format (saved in DB).
func (d *Deps) adminImportUnified(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminImportUnified(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// GET /admin/import-progress — live progress of the current batch price import
func (d *Deps) adminImportProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleAdminImportProgress(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/products/import
func (d *Deps) adminProductsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminProductsImport(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Admin Rebuild endpoints ---

// POST /admin/rebuild-sort-indexes — rebuild all sort indexes from products
func (d *Deps) adminRebuildSortIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildSortIndexes(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-eanpages — rebuild all EAN pages from products
func (d *Deps) adminRebuildEANPages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildEANPages(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-category-trees — rebuild precomputed category tree JSONs
func (d *Deps) adminRebuildCategoryTrees(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleRebuildCategoryTrees(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// GET /admin/debug-category-counts — debug info about categories
func (d *Deps) adminDebugCategoryCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleDebugCategoryCounts(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-eanpage-indexes — index all EAN pages into EANPageSearch
func (d *Deps) adminRebuildEANPageIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildEANPageIndexes(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-eanpage-sort-indexes — rebuild sort indexes for EAN pages
func (d *Deps) adminRebuildEANPageSortIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildEANPageSortIndexes(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-product-counts — recalculate ProductCount for all EAN pages
func (d *Deps) adminRebuildProductCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildProductCounts(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-category-slugs — rebuild slugs for all categories
func (d *Deps) adminRebuildCategorySlugs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildCategorySlugs(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-category-indexes — rebuild all category turbo indexes
func (d *Deps) adminRebuildCategoryIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildCategoryIndexes(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/rebuild-attrdef-indexes — rebuild attrdef cat_codes indexes
func (d *Deps) adminRebuildAttrDefIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.Handlers.HandleAdminRebuildAttrDefIndexes(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// POST /admin/change-password — change current user's password (any authenticated user)
func (d *Deps) adminChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.AuthHandlers.HandleChangePassword(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// GET /admin/products/import/{id} — get import status
func (d *Deps) adminProductsImportStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleAdminImportStatus(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Global settings ---

// GET /admin/settings (public) — get global settings; PATCH (admin) — update
func (d *Deps) adminSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleGlobalSettingsGet(w, r)
		return
	}
	// PATCH only for admin
	if r.Method == http.MethodPatch {
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleGlobalSettingsUpdate(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Company settings: Delivery Times ---

// GET /admin/delivery-times (public); POST (admin)
func (d *Deps) adminDeliveryTimes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleDeliveryTimesList(w, r)
		return
	}
	// POST only for admin
	if r.Method == http.MethodPost {
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleDeliveryTimeCreate(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// GET /admin/delivery-times/{id} (public); PATCH/DELETE (admin)
func (d *Deps) adminDeliveryTime(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleDeliveryTimeGet(w, r)
	case http.MethodPatch:
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleDeliveryTimeUpdate(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
	case http.MethodDelete:
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleDeliveryTimeDelete(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Company settings: Delivery Methods ---

// GET /admin/delivery-methods (public); POST (admin)
func (d *Deps) adminDeliveryMethods(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleDeliveryMethodsList(w, r)
		return
	}
	// POST only for admin
	if r.Method == http.MethodPost {
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleDeliveryMethodCreate(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// GET /admin/delivery-methods/{id} (public); PATCH/DELETE (admin)
func (d *Deps) adminDeliveryMethod(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleDeliveryMethodGet(w, r)
	case http.MethodPatch:
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleDeliveryMethodUpdate(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
	case http.MethodDelete:
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleDeliveryMethodDelete(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Company settings: Installment Plans ---

// GET /admin/installment-plans (public); POST (admin)
func (d *Deps) adminInstallmentPlans(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		d.Handlers.HandleInstallmentPlansList(w, r)
		return
	}
	// POST only for admin
	if r.Method == http.MethodPost {
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleInstallmentPlanCreate(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// GET /admin/installment-plans/{id} (public); PATCH/DELETE (admin)
func (d *Deps) adminInstallmentPlan(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleInstallmentPlanGet(w, r)
	case http.MethodPatch:
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleInstallmentPlanUpdate(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
	case http.MethodDelete:
		d.JWT.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleInstallmentPlanDelete(w, r)
		}), model.RoleAdmin).ServeHTTP(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /admin/promo-plans (admin)
func (d *Deps) adminPromoPlans(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminPromoPlanCreate(w, r)
}

// PATCH /admin/promo-plans/{id} (admin)
func (d *Deps) adminPromoPlan(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminPromoPlanUpdate(w, r)
}

// GET/POST /admin/reviews
func (d *Deps) adminReviews(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleAdminReviewsList(w, r)
	case http.MethodPost:
		d.Handlers.HandleAdminReviewsBulkActions(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET/PATCH/DELETE /admin/reviews/{id}
func (d *Deps) adminReview(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[4] == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	_ = parts[4]

	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleAdminReviewGet(w, r)
	case http.MethodPatch:
		d.Handlers.HandleAdminReviewUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleAdminReviewDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /admin/reviews/stats
func (d *Deps) adminReviewsStats(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminReviewStats(w, r)
}

// POST /admin/reviews/recalculate
func (d *Deps) adminReviewsRecalculate(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminReviewsRecalculate(w, r)
}

// --- Admin comment endpoints ---

// GET/POST /admin/comments
func (d *Deps) adminComments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleAdminCommentsList(w, r)
	case http.MethodPost:
		d.Handlers.HandleAdminCommentsBulkActions(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// PATCH/DELETE /admin/comments/{id}
func (d *Deps) adminComment(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[4] == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		d.Handlers.HandleAdminCommentUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleAdminCommentDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /admin/comments/stats
func (d *Deps) adminCommentsStats(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminCommentStats(w, r)
}

// GET /admin/votes/stats
func (d *Deps) adminVotesStats(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminVoteStats(w, r)
}

// GET/POST /admin/categories (export/tree/import query params handled inline)
func (d *Deps) adminCategories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// GET /admin/categories/export
		if r.URL.Query().Get("export") == "1" {
			d.Handlers.HandleAdminCategoriesExport(w, r)
			return
		}
		// GET /admin/categories/tree — full tree for admin (all categories)
		if r.URL.Query().Get("tree") == "1" {
			d.Handlers.HandleAdminCategoriesTree(w, r)
			return
		}
		d.Handlers.HandleCategoriesList(w, r)
	case http.MethodPost:
		// POST /admin/categories/import
		if r.URL.Query().Get("import") == "1" {
			d.Handlers.HandleAdminCategoriesImport(w, r)
			return
		}
		d.Handlers.HandleCategoryCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /admin/categories/{id} and /admin/categories/reorder
func (d *Deps) adminCategory(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// /admin/categories/reorder — bulk drag-and-drop reorder
	if strings.HasSuffix(path, "/reorder") {
		d.Handlers.HandleAdminCategoriesReorder(w, r)
		return
	}

	// /admin/categories/{id}/attributes
	if strings.HasSuffix(path, "/attributes") {
		d.Handlers.HandleCategoryAttributes(w, r)
		return
	}

	// /admin/categories/{id}
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleCategoryGet(w, r)
	case http.MethodPatch:
		d.Handlers.HandleCategoryUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleCategoryDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Admin upload endpoints ---

// POST /admin/upload-image — upload category image
func (d *Deps) adminUploadImage(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleUploadImage(w, r)
}

// DELETE /admin/upload-image/{filename} — delete uploaded image
func (d *Deps) adminDeleteImage(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleDeleteImage(w, r)
}

// --- Admin landing pages ---

// GET/POST /admin/landings
func (d *Deps) adminLandings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleLandingPagesList(w, r)
	case http.MethodPost:
		d.Handlers.HandleLandingPageCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET/PUT/DELETE /admin/landings/{id}
func (d *Deps) adminLanding(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleLandingPageGet(w, r)
	case http.MethodPut:
		d.Handlers.HandleLandingPageUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleLandingPageDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Admin AttrDef management ---

// GET/POST /admin/attrdefs
func (d *Deps) adminAttrDefs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleAdminAttrDefsList(w, r)
	case http.MethodPost:
		d.Handlers.HandleAdminAttrDefCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET/PATCH/DELETE /admin/attrdefs/{code}
func (d *Deps) adminAttrDef(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleAdminAttrDefGet(w, r)
	case http.MethodPatch:
		d.Handlers.HandleAdminAttrDefUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleAdminAttrDefDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- EANPage Admin ---

// GET /admin/eanpages
func (d *Deps) adminEANPages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminEANPageList(w, r)
}

// GET/PATCH/DELETE /admin/eanpages/{id} plus special rebuild/recalculate routes
func (d *Deps) adminEANPage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// POST /admin/eanpages/catalogize-all
	if path == "/admin/eanpages/catalogize-all" && r.Method == http.MethodPost {
		d.Handlers.HandleAdminEANPageCatalogizeAll(w, r)
		return
	}

	// POST /admin/eanpages/rebuild-tokens
	if path == "/admin/eanpages/rebuild-tokens" && r.Method == http.MethodPost {
		d.Handlers.HandleAdminEANPageRebuildTokens(w, r)
		return
	}

	// POST /admin/eanpages/rebuild-tokens/{id}
	if strings.HasPrefix(path, "/admin/eanpages/rebuild-tokens/") && r.Method == http.MethodPost {
		d.Handlers.HandleAdminEANPageRebuildToken(w, r)
		return
	}

	// POST /admin/eanpages/recalculate-product-counts
	if path == "/admin/eanpages/recalculate-product-counts" && r.Method == http.MethodPost {
		d.Handlers.HandleAdminEANPageRecalculateCounts(w, r)
		return
	}

	// POST /admin/eanpages/recalculate-min-prices
	if path == "/admin/eanpages/recalculate-min-prices" && r.Method == http.MethodPost {
		d.Handlers.HandleAdminEANPageRecalculateMinPrices(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleAdminEANPageGet(w, r)
	case http.MethodPatch:
		d.Handlers.HandleAdminEANPageUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleAdminEANPageDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Products ---

// POST /admin/products/reindex — rebuild all product indexes (admin only)
func (d *Deps) adminProductsReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminProductsReindex(w, r)
}

// POST /admin/products/delete-all — delete all products (admin only, destructive)
func (d *Deps) adminProductsDeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminProductsDeleteAll(w, r)
}

// POST /admin/delete-all — delete all eanpages, products, attributes (admin only, destructive)
func (d *Deps) adminDeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminDeleteAll(w, r)
}

// --- Admin: DB shard usage ---

// GET /admin/db/shards — fast stats
func (d *Deps) adminDBShards(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminDBShards(w, r)
}

// GET /admin/db/shards/active — precise stats (slow)
func (d *Deps) adminDBShardsActive(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminDBShardsActive(w, r)
}

// POST /admin/db/compact — compact all shards (slow, admin only)
func (d *Deps) adminDBCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminDBCompact(w, r)
}

// GET /admin/stats — aggregated request metrics
func (d *Deps) adminStats(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleAdminStats(w, r)
}

// --- Visit Statistics ---

// GET /admin/stats/visits/summary — visit statistics summary
func (d *Deps) adminStatsVisitsSummary(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleStatsSummary(w, r)
}

// GET /admin/stats/visits/referrers — visit statistics by referrer
func (d *Deps) adminStatsVisitsReferrers(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleStatsReferrers(w, r)
}

// GET /admin/stats/visits/paths — visit statistics by path
func (d *Deps) adminStatsVisitsPaths(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleStatsPaths(w, r)
}

// POST /admin/stats/visits/toggle — enable/disable visit stats
func (d *Deps) adminStatsVisitsToggle(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleStatsToggle(w, r)
}

// GET /admin/stats/visits/status — visit stats status
func (d *Deps) adminStatsVisitsStatus(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleStatsStatus(w, r)
}

// GET /admin/stats/visits/useragents — visit stats by user agent
func (d *Deps) adminStatsVisitsUserAgents(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleStatsUserAgents(w, r)
}

// GET/POST /admin/stats/visits/excluded-ips — get/update excluded IPs
func (d *Deps) adminStatsVisitsExcludedIPs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleStatsGetExcludedIPs(w, r)
	case http.MethodPost:
		d.Handlers.HandleStatsUpdateExcludedIPs(w, r)
	default:
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
	}
}

// GET /admin/debug/turbo-key?key=... — read raw turbo key (TEMP)
func (d *Deps) adminDebugTurboKey(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	data, err := d.Handlers.Store().DB().TurboRawRead(key)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return
	}
	fmt.Fprintf(w, "key=%s len=%d data=%s\n", key, len(data), string(data))
}

// --- Catalogizer ---

// POST /admin/catalogizer/train — train token index from normalized files
func (d *Deps) adminCatalogizerTrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminCatalogizerTrain(w, r)
}

// POST /admin/catalogizer/test — test catalogization on a product name
func (d *Deps) adminCatalogizerTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminCatalogizerTest(w, r)
}

// GET /admin/catalogizer/coverage — coverage statistics
func (d *Deps) adminCatalogizerCoverage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminCatalogizerCoverage(w, r)
}

// POST /admin/eanpages/rebuild-attr-code-indexes — rebuild attr_code indexes
func (d *Deps) adminEANPagesRebuildAttrCodeIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminRebuildAttrCodeIndexes(w, r)
}

// POST /admin/catalogize — run auto-catalogization
func (d *Deps) adminCatalogize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminCatalogize(w, r)
}

// POST /admin/catalogize/product/{id} — catalogize single product
func (d *Deps) adminCatalogizeProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAdminCatalogizeSingle(w, r)
}
