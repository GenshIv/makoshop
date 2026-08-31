package router

import (
	"log"
	"net/http"
	"time"

	"github.com/GenshIv/makoshop/internal/api"
	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/metrics"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/stats"
)

// Deps holds the dependencies needed to build the HTTP route table and
// middleware chain. The router is framework-agnostic: it only needs the
// handler instances, the JWT middleware, and the canonical site URL.
type Deps struct {
	Handlers     *api.Handlers
	AuthHandlers *api.AuthHandlers
	JWT          *auth.JWTMiddleware
	SiteURL      string
}

// Router is the fully-built HTTP handler: the route table wrapped with the
// middleware chain (security headers, maintenance, stats, metrics, gzip).
type Router struct {
	handler http.Handler
	closeFn func()
}

// New builds the complete HTTP handler for the server. It registers all
// routes and wraps them with the middleware chain. Callers should invoke
// Close (typically via defer) to release the metrics writer.
func New(d Deps) (*Router, error) {
	deps := &d
	mux := http.NewServeMux()
	registerRoutes(mux, deps)

	var handler http.Handler = mux

	// Security headers (outermost, applies to all responses)
	handler = securityHeadersMiddleware(handler)

	// Maintenance mode middleware
	handler = maintenanceMiddleware(handler)

	// Stats middleware (visit tracking)
	if statsCollector := d.Handlers.StatsCollector(); statsCollector != nil {
		// Start the stats collector
		statsCollector.Start()
		// Add stats middleware
		handler = stats.StatsMiddleware(statsCollector)(handler)
	}

	var closeFn func()
	metricsWriter, err := metrics.NewWriter("./_tmp/metrics", 1000, 2*time.Second, 50*1024*1024)
	if err != nil {
		log.Printf("WARN: metrics writer init failed: %v", err)
	} else {
		closeFn = metricsWriter.Close
		handler = metrics.Middleware(metricsWriter)(handler)
	}

	// Gzip compression (outermost: compresses final bytes sent to the client).
	handler = gzipMiddleware(handler)

	return &Router{handler: handler, closeFn: closeFn}, nil
}

// Handler returns the fully-wrapped HTTP handler to serve.
func (rt *Router) Handler() http.Handler { return rt.handler }

// Close releases resources held by the router (the metrics writer).
func (rt *Router) Close() error {
	if rt.closeFn != nil {
		rt.closeFn()
	}
	return nil
}

// registerRoutes registers all HTTP routes on the mux. It is the single
// source of truth for the server's route table. Each route delegates to a
// handler method on *Deps (see handlers.go).
func registerRoutes(mux *http.ServeMux, d *Deps) {
	// Health
	mux.HandleFunc("/health", d.health)

	// Maintenance mode endpoint (admin only)
	mux.Handle("/admin/maintenance", d.JWT.RequireRole(http.HandlerFunc(d.maintence), model.RoleAdmin))

	// --- Auth endpoints ---

	// POST /auth/register (public)
	mux.HandleFunc("/auth/register", d.register)

	// POST /auth/login (public)
	mux.HandleFunc("/auth/login", d.login)

	// GET /auth/me (requires auth)
	mux.Handle("/auth/me", d.JWT.RequireAuth(http.HandlerFunc(d.authMe)))

	// PATCH /users/me (requires auth)
	mux.Handle("/users/me", d.JWT.RequireAuth(http.HandlerFunc(d.usersMe)))

	// --- Admin users endpoints (requires admin role) ---

	// GET /admin/users
	mux.Handle("/admin/users", spaAwareHandler(d.JWT.RequireRole(http.HandlerFunc(d.adminUsers), model.RoleAdmin)))

	// GET/PATCH /admin/users/{id}
	mux.Handle("/admin/users/", d.JWT.RequireRole(http.HandlerFunc(d.adminUser), model.RoleAdmin))

	// --- Admin companies endpoints ---

	// GET/POST /admin/companies
	mux.Handle("/admin/companies", spaAwareHandler(d.JWT.RequireRole(http.HandlerFunc(d.adminCompanies), model.RoleAdmin)))

	// POST /admin/companies/create-test — create test companies
	mux.Handle("/admin/companies/create-test", d.JWT.RequireRole(http.HandlerFunc(d.adminCompaniesCreateTest), model.RoleAdmin))

	// GET /admin/companies/{id}/settings — public; everything else admin
	mux.HandleFunc("/admin/companies/", d.adminCompany)

	// POST /admin/companies/import — import company config from JSON
	mux.Handle("/admin/companies/import", d.JWT.RequireRole(http.HandlerFunc(d.adminCompaniesImport), model.RoleAdmin))

	// GET /admin/companies/export-all — export all companies as JSON
	mux.Handle("/admin/companies/export-all", d.JWT.RequireRole(http.HandlerFunc(d.adminCompaniesExportAll), model.RoleAdmin))

	// POST /admin/companies/import-all — import all companies from JSON
	mux.Handle("/admin/companies/import-all", d.JWT.RequireRole(http.HandlerFunc(d.adminCompaniesImportAll), model.RoleAdmin))

	// GET /admin/price-sources — list price source configs
	mux.Handle("/admin/price-sources", d.JWT.RequireRole(http.HandlerFunc(d.adminPriceSources), model.RoleAdmin))

	// --- Admin Analytics ---

	mux.Handle("/admin/analytics/orders", d.JWT.RequireRole(http.HandlerFunc(d.adminAnalyticsOrders), model.RoleAdmin))
	mux.Handle("/admin/analytics/overview", d.JWT.RequireRole(http.HandlerFunc(d.adminAnalyticsOverview), model.RoleAdmin))
	mux.Handle("/admin/analytics/products", d.JWT.RequireRole(http.HandlerFunc(d.adminAnalyticsProducts), model.RoleAdmin))
	mux.Handle("/admin/analytics/search-queries", d.JWT.RequireRole(http.HandlerFunc(d.adminAnalyticsSearchQueries), model.RoleAdmin))

	// --- Admin Promo campaigns ---

	mux.Handle("/admin/promo/campaigns", d.JWT.RequireRole(http.HandlerFunc(d.adminPromoCampaigns), model.RoleAdmin))
	mux.Handle("/admin/promo/campaigns/", d.JWT.RequireRole(http.HandlerFunc(d.adminPromoCampaign), model.RoleAdmin))

	// --- Admin Product Import ---

	mux.Handle("/admin/import-prices", d.JWT.RequireRole(http.HandlerFunc(d.adminImportPrices), model.RoleAdmin))
	mux.Handle("/admin/import-nokaut", d.JWT.RequireRole(http.HandlerFunc(d.adminImportNokaut), model.RoleAdmin))
	mux.Handle("/admin/import-unified", d.JWT.RequireRole(http.HandlerFunc(d.adminImportUnified), model.RoleAdmin))
	mux.Handle("/admin/products/import", d.JWT.RequireRole(http.HandlerFunc(d.adminProductsImport), model.RoleAdmin))

	// --- Admin Rebuild endpoints ---

	mux.Handle("/admin/rebuild-sort-indexes", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildSortIndexes), model.RoleAdmin))
	mux.Handle("/admin/rebuild-eanpages", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildEANPages), model.RoleAdmin))
	mux.Handle("/admin/rebuild-category-trees", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildCategoryTrees), model.RoleAdmin))
	mux.Handle("/admin/debug-category-counts", d.JWT.RequireRole(http.HandlerFunc(d.adminDebugCategoryCounts), model.RoleAdmin))
	mux.Handle("/admin/rebuild-eanpage-indexes", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildEANPageIndexes), model.RoleAdmin))
	mux.Handle("/admin/rebuild-eanpage-sort-indexes", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildEANPageSortIndexes), model.RoleAdmin))
	mux.Handle("/admin/rebuild-product-counts", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildProductCounts), model.RoleAdmin))
	mux.Handle("/admin/rebuild-category-slugs", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildCategorySlugs), model.RoleAdmin))
	mux.Handle("/admin/rebuild-category-indexes", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildCategoryIndexes), model.RoleAdmin))
	mux.Handle("/admin/rebuild-attrdef-indexes", d.JWT.RequireRole(http.HandlerFunc(d.adminRebuildAttrDefIndexes), model.RoleAdmin))

	// POST /admin/change-password — change current user's password (any authenticated user)
	mux.Handle("/admin/change-password", d.JWT.RequireAuth(http.HandlerFunc(d.adminChangePassword)))

	// GET /admin/products/import/{id} — get import status
	mux.Handle("/admin/products/import/", d.JWT.RequireRole(http.HandlerFunc(d.adminProductsImportStatus), model.RoleAdmin))

	// --- Global settings ---

	// GET /admin/settings (public) — get global settings; PATCH (admin) — update
	mux.Handle("/admin/settings", spaAwareHandler(http.HandlerFunc(d.adminSettings)))

	// --- Company settings: Payment Methods (temporarily disabled) ---

	// GET /admin/payment-methods (public)
	mux.Handle("/admin/payment-methods", spaAwareHandler(paymentsDisabledHandler))

	// GET /admin/payment-methods/{id} (public), PATCH/DELETE (admin)
	mux.Handle("/admin/payment-methods/", paymentsDisabledHandler)

	// --- Company settings: Delivery Times ---

	// GET /admin/delivery-times (public); POST (admin)
	mux.Handle("/admin/delivery-times", spaAwareHandler(http.HandlerFunc(d.adminDeliveryTimes)))

	// GET /admin/delivery-times/{id} (public); PATCH/DELETE (admin)
	mux.Handle("/admin/delivery-times/", http.HandlerFunc(d.adminDeliveryTime))

	// --- Company settings: Delivery Methods ---

	// GET /admin/delivery-methods (public); POST (admin)
	mux.Handle("/admin/delivery-methods", spaAwareHandler(http.HandlerFunc(d.adminDeliveryMethods)))

	// GET /admin/delivery-methods/{id} (public); PATCH/DELETE (admin)
	mux.Handle("/admin/delivery-methods/", http.HandlerFunc(d.adminDeliveryMethod))

	// --- Company settings: Installment Plans ---

	// GET /admin/installment-plans (public); POST (admin)
	mux.Handle("/admin/installment-plans", spaAwareHandler(http.HandlerFunc(d.adminInstallmentPlans)))

	// GET /admin/installment-plans/{id} (public); PATCH/DELETE (admin)
	mux.Handle("/admin/installment-plans/", http.HandlerFunc(d.adminInstallmentPlan))

	// --- Promo endpoints ---

	// GET /promo/plans (public)
	mux.HandleFunc("/promo/plans", d.promoPlans)

	// POST /admin/promo-plans (admin)
	mux.Handle("/admin/promo-plans", d.JWT.RequireRole(http.HandlerFunc(d.adminPromoPlans), model.RoleAdmin))

	// PATCH /admin/promo-plans/{id} (admin)
	mux.Handle("/admin/promo-plans/", d.JWT.RequireRole(http.HandlerFunc(d.adminPromoPlan), model.RoleAdmin))

	// /companies/{id}/orders and /companies/{id}/promo-campaigns (auth)
	mux.Handle("/companies/", d.JWT.RequireAuth(http.HandlerFunc(d.companiesSub)))

	// PATCH /promo-campaigns/{id}/status (auth)
	mux.Handle("/promo-campaigns/", d.JWT.RequireAuth(http.HandlerFunc(d.promoCampaignsStatus)))

	// POST /promo/logs
	mux.HandleFunc("/promo/logs", d.promoLogs)

	// --- Review endpoints ---

	// GET /reviews?user_id=... (auth required, own reviews only)
	mux.Handle("/reviews", d.JWT.RequireAuth(http.HandlerFunc(d.reviews)))

	// GET /seller/reviews (seller access)
	mux.Handle("/seller/reviews", d.JWT.RequireRole(http.HandlerFunc(d.sellerReviews)))

	// Admin review endpoints
	mux.Handle("/admin/reviews", d.JWT.RequireRole(http.HandlerFunc(d.adminReviews)))
	mux.Handle("/admin/reviews/", d.JWT.RequireRole(http.HandlerFunc(d.adminReview)))
	mux.Handle("/admin/reviews/stats", d.JWT.RequireRole(http.HandlerFunc(d.adminReviewsStats)))
	mux.Handle("/admin/reviews/recalculate", d.JWT.RequireRole(http.HandlerFunc(d.adminReviewsRecalculate)))

	// --- Comment endpoints ---

	// /comments — POST (auth) для создания, GET (public) для списка
	mux.Handle("/comments", http.HandlerFunc(d.comments))

	// --- Vote endpoints ---

	// /votes — POST (auth) для голосования
	mux.Handle("/votes", http.HandlerFunc(d.votes))

	// GET /votes/check (auth required)
	mux.Handle("/votes/check", d.JWT.RequireAuth(http.HandlerFunc(d.votesCheck)))

	// --- Admin comment endpoints ---

	mux.Handle("/admin/comments", d.JWT.RequireRole(http.HandlerFunc(d.adminComments)))
	mux.Handle("/admin/comments/", d.JWT.RequireRole(http.HandlerFunc(d.adminComment)))
	mux.Handle("/admin/comments/stats", d.JWT.RequireRole(http.HandlerFunc(d.adminCommentsStats)))
	mux.Handle("/admin/votes/stats", d.JWT.RequireRole(http.HandlerFunc(d.adminVotesStats)))

	// --- Categories (public and admin share same CRUD for now) ---

	mux.HandleFunc("/categories", d.categories)
	mux.HandleFunc("/categories/tree", d.categoriesTree)
	mux.HandleFunc("/categories/", d.categoriesByPath)
	mux.HandleFunc("/admin/categories", spaAware(d.adminCategories))
	mux.HandleFunc("/admin/categories/", d.adminCategory)

	// Image upload (admin only)
	mux.Handle("/admin/upload-image", d.JWT.RequireRole(http.HandlerFunc(d.adminUploadImage), model.RoleAdmin))
	mux.Handle("/admin/upload-image/", d.JWT.RequireRole(http.HandlerFunc(d.adminDeleteImage), model.RoleAdmin))

	// Public static file serving for uploads
	mux.HandleFunc("/uploads/", api.HandleUploadsStatic)

	// Brands
	mux.HandleFunc("/brands", d.brands)

	// Companies (public read, admin write)
	mux.HandleFunc("/companies", d.companies)

	// Landing pages (public)
	mux.HandleFunc("/landing/", d.landing)

	// Company landing pages (public)
	mux.HandleFunc("/company/", d.companyLanding)

	// Admin landing pages
	mux.Handle("/admin/landings", d.JWT.RequireRole(http.HandlerFunc(d.adminLandings), model.RoleAdmin))
	mux.Handle("/admin/landings/", d.JWT.RequireRole(http.HandlerFunc(d.adminLanding), model.RoleAdmin))

	// EANPage SEO pages
	mux.HandleFunc("/shop", d.shop)
	mux.HandleFunc("/shop/", d.shopSub)

	// Attribute values (turbo-based)
	mux.HandleFunc("/attributes/", d.attributeValues)

	// --- Admin AttrDef management ---

	mux.Handle("/admin/attrdefs", d.JWT.RequireRole(http.HandlerFunc(d.adminAttrDefs), model.RoleAdmin))
	mux.Handle("/admin/attrdefs/", d.JWT.RequireRole(http.HandlerFunc(d.adminAttrDef), model.RoleAdmin))

	// --- EANPage Admin ---

	mux.Handle("/admin/eanpages", d.JWT.RequireRole(http.HandlerFunc(d.adminEANPages), model.RoleAdmin))
	mux.Handle("/admin/eanpages/", d.JWT.RequireRole(http.HandlerFunc(d.adminEANPage), model.RoleAdmin))

	// --- Products ---

	mux.Handle("/admin/products/reindex", d.JWT.RequireRole(http.HandlerFunc(d.adminProductsReindex), model.RoleAdmin))
	mux.Handle("/admin/products/delete-all", d.JWT.RequireRole(http.HandlerFunc(d.adminProductsDeleteAll), model.RoleAdmin))
	mux.HandleFunc("/products", d.products)
	mux.HandleFunc("/products/turbo", d.productsTurbo)
	mux.Handle("/products/", d.JWT.OptionalAuth(http.HandlerFunc(d.product)))

	// --- Cart endpoints ---

	mux.Handle("/cart", d.JWT.OptionalAuth(http.HandlerFunc(d.cart)))
	mux.Handle("/cart/me", d.JWT.RequireAuth(http.HandlerFunc(d.cartMe)))
	mux.HandleFunc("/cart/", d.cartByPath)

	// --- Order endpoints ---

	mux.HandleFunc("/orders", d.orders)
	mux.Handle("/orders/", d.JWT.OptionalAuth(http.HandlerFunc(d.order)))

	// --- Payment endpoints (temporarily disabled) ---

	mux.HandleFunc("/payments", paymentsDisabledHandler.ServeHTTP)
	mux.Handle("/payments/", paymentsDisabledHandler)
	mux.HandleFunc("/payments/webhook/", paymentsDisabledHandler.ServeHTTP)
	mux.Handle("/admin/payments/timeout-cleanup", paymentsDisabledHandler)

	// --- Admin: DB shard usage ---

	mux.Handle("/admin/db/shards", d.JWT.RequireRole(http.HandlerFunc(d.adminDBShards), model.RoleAdmin))
	mux.Handle("/admin/db/shards/active", d.JWT.RequireRole(http.HandlerFunc(d.adminDBShardsActive), model.RoleAdmin))
	mux.Handle("/admin/db/compact", d.JWT.RequireRole(http.HandlerFunc(d.adminDBCompact), model.RoleAdmin))

	// GET /admin/stats — aggregated request metrics
	mux.Handle("/admin/stats", spaAwareHandler(d.JWT.RequireRole(http.HandlerFunc(d.adminStats), model.RoleAdmin)))

	// --- Visit Statistics ---

	mux.Handle("/admin/stats/visits/summary", d.JWT.RequireRole(http.HandlerFunc(d.adminStatsVisitsSummary), model.RoleAdmin))
	mux.Handle("/admin/stats/visits/referrers", d.JWT.RequireRole(http.HandlerFunc(d.adminStatsVisitsReferrers), model.RoleAdmin))
	mux.Handle("/admin/stats/visits/paths", d.JWT.RequireRole(http.HandlerFunc(d.adminStatsVisitsPaths), model.RoleAdmin))
	mux.Handle("/admin/stats/visits/toggle", d.JWT.RequireRole(http.HandlerFunc(d.adminStatsVisitsToggle), model.RoleAdmin))
	mux.Handle("/admin/stats/visits/status", d.JWT.RequireRole(http.HandlerFunc(d.adminStatsVisitsStatus), model.RoleAdmin))
	mux.Handle("/admin/stats/visits/useragents", d.JWT.RequireRole(http.HandlerFunc(d.adminStatsVisitsUserAgents), model.RoleAdmin))
	mux.Handle("/admin/stats/visits/excluded-ips", d.JWT.RequireRole(http.HandlerFunc(d.adminStatsVisitsExcludedIPs), model.RoleAdmin))

	// GET /admin/debug/turbo-key?key=... — read raw turbo key (TEMP)
	mux.Handle("/admin/debug/turbo-key", d.JWT.RequireRole(http.HandlerFunc(d.adminDebugTurboKey), model.RoleAdmin))

	// --- Catalogizer ---

	mux.Handle("/admin/catalogizer/train", d.JWT.RequireRole(http.HandlerFunc(d.adminCatalogizerTrain), model.RoleAdmin))
	mux.Handle("/admin/catalogizer/test", d.JWT.RequireRole(http.HandlerFunc(d.adminCatalogizerTest), model.RoleAdmin))
	mux.Handle("/admin/catalogizer/coverage", d.JWT.RequireRole(http.HandlerFunc(d.adminCatalogizerCoverage), model.RoleAdmin))
	mux.Handle("/admin/eanpages/rebuild-attr-code-indexes", d.JWT.RequireRole(http.HandlerFunc(d.adminEANPagesRebuildAttrCodeIndexes), model.RoleAdmin))
	mux.Handle("/admin/catalogize", d.JWT.RequireRole(http.HandlerFunc(d.adminCatalogize), model.RoleAdmin))
	mux.Handle("/admin/catalogize/product/", d.JWT.RequireRole(http.HandlerFunc(d.adminCatalogizeProduct), model.RoleAdmin))

	// --- SEO: robots.txt and sitemap ---

	mux.HandleFunc("/robots.txt", d.robotsTXT)
	mux.HandleFunc("/sitemap.xml", d.sitemapIndex)
	mux.HandleFunc("/sitemap-categories.xml", d.sitemapCategories)

	// Serve built frontend static assets (JS/CSS/images) from frontend/dist/.
	// In production the Go server serves both the SPA and the API on the same
	// origin; these routes cover the hashed build artifacts referenced by
	// dist/index.html, which would otherwise fall through to 404.
	distDir := http.Dir("frontend/dist")
	// Hashed build artifacts are immutable — cache them for a year.
	mux.Handle("/assets/", cachedAssetServer(distDir))
	mux.Handle("/favicon.svg", http.FileServer(distDir))
	mux.Handle("/icons.svg", http.FileServer(distDir))
	mux.Handle("/koshik.png", http.FileServer(distDir))

	// SPA fallback: for any unmatched path that wants HTML, serve index.html
	mux.HandleFunc("/", d.spaFallback)
}
