package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/GenshIv/makoshop/internal/api"
	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/i18n"
	"github.com/GenshIv/makoshop/internal/metrics"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/internal/stats"
	"github.com/GenshIv/makoshop/pkg/config"
)

//go:embed i18n/*.json
var i18nFS embed.FS

// Maintenance mode: blocks non-admin traffic.
var (
	maintenanceEnabled     bool
	maintenanceAutoDisable bool
)

// maintenanceMiddleware blocks requests during maintenance except admin endpoints.
// If auto_disable is set, it disables maintenance after the current request completes.
func maintenanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Remember if we need to auto-disable after this request
		shouldAutoDisable := maintenanceEnabled && maintenanceAutoDisable

		if !maintenanceEnabled {
			next.ServeHTTP(w, r)
			return
		}

		// Allow health checks
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow admin endpoints (paths starting with /admin)
		if strings.HasPrefix(r.URL.Path, "/admin") {
			next.ServeHTTP(w, r)
			// Auto-disable after admin request if needed
			if shouldAutoDisable {
				maintenanceEnabled = false
				maintenanceAutoDisable = false
				fmt.Println("[MAINTENANCE] auto-disabled after request")
			}
			return
		}

		// For HTML clients: show maintenance page
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<title>Техническое обслуживание</title>
<style>
body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f5f5;color:#333}
.box{max-width:480px;text-align:center;padding:32px;background:#fff;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,0.08)}
h1{font-size:24px;margin-bottom:12px}
p{font-size:16px;color:#555}
</style>
</head>
<body>
<div class="box">
<h1>Техническое обслуживание</h1>
<p>Сайт временно недоступен. Пожалуйста, попробуйте через несколько минут.</p>
</div>
</body>
</html>`))
			// Auto-disable after this response if needed
			if shouldAutoDisable {
				maintenanceEnabled = false
				maintenanceAutoDisable = false
				fmt.Println("[MAINTENANCE] auto-disabled after request")
			}
			return
		}

		// For API/bots: 503
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"maintenance_mode","message":"Service temporarily unavailable. Try again later."}`))
		// Auto-disable after this response if needed
		if shouldAutoDisable {
			maintenanceEnabled = false
			maintenanceAutoDisable = false
			fmt.Println("[MAINTENANCE] auto-disabled after request")
		}
	})
}

// paymentsDisabledHandler blocks all payment-related endpoints.
// Payments are temporarily disabled: no payment providers are integrated yet,
// so every payment route returns 503 for all users (including admins).
var paymentsDisabledHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":{"code":"PAYMENTS_DISABLED","message":"Payments are temporarily unavailable"}}`))
})

// securityHeadersMiddleware adds baseline security headers to every response.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-XSS-Protection", "0")
		// CSP: allow self resources and inline styles/scripts used by the SPA.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

// bootstrapSuperAdmin creates a superadmin if no admins exist.
func bootstrapSuperAdmin(userRepo *db.UserRepo) {
	users, _, err := userRepo.List(db.ListUsersParams{})
	if err != nil {
		fmt.Printf("WARN: failed to list users during bootstrap: %v\n", err)
		return
	}

	// Check if any admin exists
	for _, u := range users {
		if u.Role == model.RoleAdmin {
			return // admins already exist
		}
	}

	// Generate random password (16 chars)
	password := generateRandomPassword(16)

	// Create superadmin
	superadmin := &model.User{
		Email:        "admin@mako.com",
		PasswordHash: "", // will be set by userRepo.Create
		Role:         model.RoleAdmin,
		Status:       model.UserStatusActive,
		IsFirstLogin: true,
	}

	if err := userRepo.Create(superadmin, password); err != nil {
		fmt.Printf("WARN: failed to create superadmin: %v\n", err)
		return
	}

	fmt.Println("========================================")
	fmt.Println("SUPERADMIN CREATED (no admins existed)")
	fmt.Printf("Email:    admin@mako.com\n")
	fmt.Printf("Password: %s\n", password)
	fmt.Println("Please change the password after first login.")
	fmt.Println("========================================")
}

// generateRandomPassword creates a random alphanumeric password.
func generateRandomPassword(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	rand.Seed(time.Now().UnixNano())
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// wantsHTML returns true if the request is from a browser navigation
// (wants HTML) rather than an API call (wants JSON).
func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}

// serveSPA serves the frontend index.html for SPA routes.
func serveSPA(w http.ResponseWriter, r *http.Request) {
	index, err := os.ReadFile("frontend/dist/index.html")
	if err != nil {
		// Fallback to src/index.html in dev mode
		index, err = os.ReadFile("frontend/index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html")
	// HEAD requests: send headers only, no body
	if r.Method != http.MethodHead {
		w.Write(index)
	}
}

// spaAware wraps a handler to serve the SPA if the request wants HTML.
func spaAware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wantsHTML(r) {
			serveSPA(w, r)
			return
		}
		next(w, r)
	}
}

// spaAwareHandler wraps an http.Handler to serve the SPA if the request wants HTML.
func spaAwareHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantsHTML(r) {
			serveSPA(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	// Automatically load .env (if present) so the server picks up its
	// configuration without the operator exporting variables manually.
	config.LoadEnv()

	cfg := config.DefaultConfig()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Load i18n translations
	loadI18n()

	store, err := db.NewStore(cfg.Database)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() {
		store.DB().Sync()
		store.Close()
	}()

	// Repositories (needed for superadmin bootstrap)
	userRepo := db.NewUserRepo(store)

	ticker := time.NewTicker(10 * time.Second)

	go func() {
		for range ticker.C {
			store.DB().Sync()
		}
	}()
	// Bootstrap superadmin if no admins exist
	bootstrapSuperAdmin(userRepo)

	// Auth middleware
	jwtMiddleware := auth.NewJWTMiddleware(cfg.Auth.JWTSecret)

	// Repositories
	userRepo = db.NewUserRepo(store)
	companyRepo := db.NewCompanyRepo(store)
	cartRepo := db.NewCartRepo(store)
	paymentMethodRepo := db.NewPaymentMethodRepo(store)
	deliveryTimeRepo := db.NewDeliveryTimeRepo(store)
	installmentPlanRepo := db.NewInstallmentPlanRepo(store)

	h := api.NewHandlers(store)
	// Set the canonical site base URL (for sitemaps, robots.txt, canonicals).
	h.SetSiteURL(cfg.Server.SiteURL)
	// Load production frontend asset tags so browser SSR pages reference the
	// built bundles (not the dev /src/main.js) on deep-link navigation.
	api.LoadBrowserAssetTags()
	// Attach company settings repos to handlers
	h.SetCompanySettingsRepos(companyRepo, paymentMethodRepo, deliveryTimeRepo, installmentPlanRepo)
	authHandlers := api.NewAuthHandlers(userRepo, companyRepo, cartRepo, jwtMiddleware, cfg.Auth.JWTSecret)
	// Attach turboSearch to authHandlers for company products endpoint
	authHandlers.SetTurboSearch(h.TurboSearch())

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Maintenance mode endpoint (admin only)
	mux.Handle("/admin/maintenance", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}), model.RoleAdmin))

	// --- Auth endpoints ---

	// POST /auth/register (public)
	mux.HandleFunc("/auth/register", func(w http.ResponseWriter, r *http.Request) {
		authHandlers.HandleRegister(w, r)
	})

	// POST /auth/login (public)
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		authHandlers.HandleLogin(w, r)
	})

	// GET /auth/me (requires auth)
	mux.Handle("/auth/me", jwtMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandlers.HandleMe(w, r)
	})))

	// PATCH /users/me (requires auth)
	mux.Handle("/users/me", jwtMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandlers.HandleUpdateMe(w, r)
	})))

	// --- Admin users endpoints (requires admin role) ---

	// GET /admin/users
	mux.Handle("/admin/users", spaAwareHandler(jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandlers.HandleAdminUsersList(w, r)
	}), model.RoleAdmin)))

	// GET/PATCH /admin/users/{id}
	mux.Handle("/admin/users/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			authHandlers.HandleAdminUserGet(w, r)
		case http.MethodPatch:
			authHandlers.HandleAdminUserUpdate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin))

	// --- Admin companies endpoints ---

	// GET/POST /admin/companies
	mux.Handle("/admin/companies", spaAwareHandler(jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			authHandlers.HandleAdminCompaniesList(w, r)
		case http.MethodPost:
			authHandlers.HandleAdminCompanyCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin)))

	// POST /admin/companies/create-test — create test companies
	mux.Handle("/admin/companies/create-test", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authHandlers.HandleAdminCreateTestCompanies(w, r)
	}), model.RoleAdmin))

	// GET /admin/companies/{id}/settings — public (company with full settings)
	mux.HandleFunc("/admin/companies/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/settings") && r.Method == http.MethodGet {
			h.HandleCompanyGetWithSettings(w, r)
			return
		}

		// Everything else under /admin/companies/{id} requires admin role
		jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(path, "/verify") {
				if r.Method == http.MethodPatch {
					authHandlers.HandleAdminCompanyVerify(w, r)
					return
				}
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if strings.HasSuffix(path, "/export") {
				if r.Method == http.MethodGet {
					authHandlers.HandleAdminCompanyExport(w, r)
					return
				}
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			switch r.Method {
			case http.MethodGet:
				authHandlers.HandleAdminCompanyGet(w, r)
			case http.MethodPatch:
				authHandlers.HandleAdminCompanyUpdate(w, r)
			case http.MethodDelete:
				authHandlers.HandleAdminCompanyDelete(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}), model.RoleAdmin).ServeHTTP(w, r)
	})

	// POST /admin/companies/import — import company config from JSON
	mux.Handle("/admin/companies/import", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authHandlers.HandleAdminCompanyImport(w, r)
	}), model.RoleAdmin))

	// GET /admin/companies/export-all — export all companies as JSON
	mux.Handle("/admin/companies/export-all", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authHandlers.HandleAdminCompaniesExportAll(w, r)
	}), model.RoleAdmin))

	// POST /admin/companies/import-all — import all companies from JSON
	mux.Handle("/admin/companies/import-all", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authHandlers.HandleAdminCompaniesImportAll(w, r)
	}), model.RoleAdmin))

	// GET /admin/price-sources — list price source configs
	mux.Handle("/admin/price-sources", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authHandlers.HandleAdminPriceSourcesList(w, r)
	}), model.RoleAdmin))

	// --- Admin Analytics ---

	// GET /admin/analytics/orders
	mux.Handle("/admin/analytics/orders", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAnalyticsOrders(w, r)
	}), model.RoleAdmin))

	// GET /admin/analytics/overview
	mux.Handle("/admin/analytics/overview", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAnalyticsOverview(w, r)
	}), model.RoleAdmin))

	// GET /admin/analytics/products
	mux.Handle("/admin/analytics/products", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAnalyticsProducts(w, r)
	}), model.RoleAdmin))

	// GET /admin/analytics/search-queries
	mux.Handle("/admin/analytics/search-queries", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAnalyticsSearchQueries(w, r)
	}), model.RoleAdmin))

	// --- Admin Promo ---

	// GET /admin/promo/campaigns
	mux.Handle("/admin/promo/campaigns", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleAdminPromoCampaignsList(w, r)
			return
		}
		if r.Method == http.MethodPost {
			h.HandleAdminPromoCampaignCreate(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// PATCH /admin/promo/campaigns/{id}
	mux.Handle("/admin/promo/campaigns/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			h.HandleAdminPromoCampaignUpdate(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// --- Admin Product Import ---

	// POST /admin/import-prices — import from CSV files in _tmp/prices
	mux.Handle("/admin/import-prices", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminImportPrices(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/import-nokaut — import from Nokaut XML price files in prices/
	mux.Handle("/admin/import-nokaut", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminImportNokaut(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/products/import
	mux.Handle("/admin/products/import", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminProductsImport(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-sort-indexes — rebuild all sort indexes from products
	mux.Handle("/admin/rebuild-sort-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildSortIndexes(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-eanpages — rebuild all EAN pages from products
	mux.Handle("/admin/rebuild-eanpages", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildEANPages(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-category-trees — rebuild precomputed category tree JSONs
	mux.Handle("/admin/rebuild-category-trees", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleRebuildCategoryTrees(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// GET /admin/debug-category-counts — debug info about categories
	mux.Handle("/admin/debug-category-counts", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleDebugCategoryCounts(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-eanpage-indexes — index all EAN pages into EANPageSearch
	mux.Handle("/admin/rebuild-eanpage-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildEANPageIndexes(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-eanpage-sort-indexes — rebuild sort indexes for EAN pages
	mux.Handle("/admin/rebuild-eanpage-sort-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildEANPageSortIndexes(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-product-counts — recalculate ProductCount for all EAN pages
	mux.Handle("/admin/rebuild-product-counts", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildProductCounts(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-category-slugs — rebuild slugs for all categories
	mux.Handle("/admin/rebuild-category-slugs", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildCategorySlugs(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-category-indexes — rebuild all category turbo indexes
	mux.Handle("/admin/rebuild-category-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildCategoryIndexes(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-attrdef-indexes — rebuild attrdef cat_codes indexes
	mux.Handle("/admin/rebuild-attrdef-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildAttrDefIndexes(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/change-password — change current user's password (any authenticated user)
	mux.Handle("/admin/change-password", jwtMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			authHandlers.HandleChangePassword(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})))

	// GET /admin/products/import/{id}
	mux.Handle("/admin/products/import/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleAdminImportStatus(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// --- Global settings ---

	// GET /admin/settings (public) - get global settings including default currency
	mux.Handle("/admin/settings", spaAwareHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleGlobalSettingsGet(w, r)
			return
		}
		// PATCH only for admin
		if r.Method == http.MethodPatch {
			jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleGlobalSettingsUpdate(w, r)
			}), model.RoleAdmin).ServeHTTP(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})))

	// --- Company settings: Payment Methods (temporarily disabled) ---

	// GET /admin/payment-methods (public)
	mux.Handle("/admin/payment-methods", spaAwareHandler(paymentsDisabledHandler))

	// GET /admin/payment-methods/{id} (public), PATCH/DELETE (admin)
	mux.Handle("/admin/payment-methods/", paymentsDisabledHandler)

	// --- Company settings: Delivery Times ---

	// GET /admin/delivery-times (public)
	mux.Handle("/admin/delivery-times", spaAwareHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleDeliveryTimesList(w, r)
			return
		}
		// POST only for admin
		if r.Method == http.MethodPost {
			jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleDeliveryTimeCreate(w, r)
			}), model.RoleAdmin).ServeHTTP(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})))

	// GET /admin/delivery-times/{id} (public), PATCH/DELETE (admin)
	mux.Handle("/admin/delivery-times/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleDeliveryTimeGet(w, r)
		case http.MethodPatch:
			jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleDeliveryTimeUpdate(w, r)
			}), model.RoleAdmin).ServeHTTP(w, r)
		case http.MethodDelete:
			jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleDeliveryTimeDelete(w, r)
			}), model.RoleAdmin).ServeHTTP(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// --- Company settings: Installment Plans ---

	// GET /admin/installment-plans (public)
	mux.Handle("/admin/installment-plans", spaAwareHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleInstallmentPlansList(w, r)
			return
		}
		// POST only for admin
		if r.Method == http.MethodPost {
			jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleInstallmentPlanCreate(w, r)
			}), model.RoleAdmin).ServeHTTP(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})))

	// GET /admin/installment-plans/{id} (public), PATCH/DELETE (admin)
	mux.Handle("/admin/installment-plans/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleInstallmentPlanGet(w, r)
		case http.MethodPatch:
			jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleInstallmentPlanUpdate(w, r)
			}), model.RoleAdmin).ServeHTTP(w, r)
		case http.MethodDelete:
			jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleInstallmentPlanDelete(w, r)
			}), model.RoleAdmin).ServeHTTP(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// --- Promo endpoints ---

	// GET /promo/plans (public)
	mux.HandleFunc("/promo/plans", func(w http.ResponseWriter, r *http.Request) {
		h.HandlePromoPlansList(w, r)
	})

	// POST /admin/promo-plans (admin)
	mux.Handle("/admin/promo-plans", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminPromoPlanCreate(w, r)
	}), model.RoleAdmin))

	// PATCH /admin/promo-plans/{id} (admin)
	mux.Handle("/admin/promo-plans/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminPromoPlanUpdate(w, r)
	}), model.RoleAdmin))

	// /companies/{id}/orders and /companies/{id}/promo-campaigns
	mux.Handle("/companies/", jwtMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		// /companies/{id}/promo-campaigns -> parts = ["", "companies", "{id}", "promo-campaigns"]
		if len(parts) >= 4 && parts[3] == "promo-campaigns" {
			switch r.Method {
			case http.MethodGet:
				h.HandleCompanyPromoCampaignsList(w, r)
			case http.MethodPost:
				h.HandleCompanyPromoCampaignCreate(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		// /companies/{id}/orders -> parts = ["", "companies", "{id}", "orders"]
		if len(parts) >= 4 && parts[3] == "orders" {
			if r.Method == http.MethodGet {
				h.HandleCompanyOrders(w, r)
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	})))

	// PATCH /promo-campaigns/{id}/status
	mux.Handle("/promo-campaigns/", jwtMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandlePromoCampaignUpdateStatus(w, r)
	})))

	// POST /promo/logs
	mux.HandleFunc("/promo/logs", func(w http.ResponseWriter, r *http.Request) {
		h.HandlePromoLogCreate(w, r)
	})

	// --- Review endpoints ---

	// GET /reviews?user_id=... (auth required, own reviews only)
	mux.Handle("/reviews", jwtMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleUserReviewsList(w, r)
	})))

	// --- Categories (public and admin share same CRUD for now) ---

	mux.HandleFunc("/categories", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleCategoriesList(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// GET /categories/tree
	mux.HandleFunc("/categories/tree", func(w http.ResponseWriter, r *http.Request) {
		h.HandleCategoriesTree(w, r)
	})

	// GET /categories/{id} — public category by id
	mux.HandleFunc("/categories/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Path

		// GET /categories/tree_path/{id} — full path from root with full category data
		if strings.Contains(path, "/tree_path/") {
			h.HandleCategoryTreePath(w, r)
			return
		}

		h.HandleCategoryGet(w, r)
	})

	mux.HandleFunc("/admin/categories", spaAware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// GET /admin/categories/export
			if r.URL.Query().Get("export") == "1" {
				h.HandleAdminCategoriesExport(w, r)
				return
			}
			// GET /admin/categories/tree — full tree for admin (all categories)
			if r.URL.Query().Get("tree") == "1" {
				h.HandleAdminCategoriesTree(w, r)
				return
			}
			h.HandleCategoriesList(w, r)
		case http.MethodPost:
			// POST /admin/categories/import
			if r.URL.Query().Get("import") == "1" {
				h.HandleAdminCategoriesImport(w, r)
				return
			}
			h.HandleCategoryCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/admin/categories/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// /admin/categories/reorder — bulk drag-and-drop reorder
		if strings.HasSuffix(path, "/reorder") {
			h.HandleAdminCategoriesReorder(w, r)
			return
		}

		// /admin/categories/{id}/attributes
		if strings.HasSuffix(path, "/attributes") {
			h.HandleCategoryAttributes(w, r)
			return
		}

		// /admin/categories/{id}
		switch r.Method {
		case http.MethodGet:
			h.HandleCategoryGet(w, r)
		case http.MethodPatch:
			h.HandleCategoryUpdate(w, r)
		case http.MethodDelete:
			h.HandleCategoryDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Image upload (admin only)
	// POST /admin/upload-image — upload category image
	mux.Handle("/admin/upload-image", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleUploadImage(w, r)
	}), model.RoleAdmin))

	// DELETE /admin/upload-image/{filename} — delete uploaded image
	mux.Handle("/admin/upload-image/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleDeleteImage(w, r)
	}), model.RoleAdmin))

	// Public static file serving for uploads
	// GET /uploads/{path}
	mux.HandleFunc("/uploads/", api.HandleUploadsStatic)

	// Brands
	// GET /brands
	mux.HandleFunc("/brands", func(w http.ResponseWriter, r *http.Request) {
		h.HandleBrandsList(w, r)
	})

	// Companies (public read, admin write)
	// Unified handler for /companies, /companies/{id}, /companies/slug/{slug}, /companies/{id}/products
	mux.HandleFunc("/companies", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/companies"), "/")

		// /companies — list
		if path == "/companies" || (len(parts) == 1 && parts[0] == "") {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			authHandlers.HandleCompaniesList(w, r)
			return
		}

		// /companies/slug/{slug}
		if len(parts) >= 2 && parts[0] == "slug" && parts[1] != "" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			authHandlers.HandleCompanyGetBySlug(w, r)
			return
		}

		// /companies/{id}/products
		if len(parts) >= 3 && parts[0] != "" && parts[1] == "products" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			authHandlers.HandleCompanyProducts(w, r)
			return
		}

		// /companies/{id}
		if len(parts) == 2 && parts[0] != "" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			authHandlers.HandleCompanyGet(w, r)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// Landing pages (public)
	// GET /landing/{slug}
	mux.HandleFunc("/landing/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		parts := strings.Split(strings.TrimPrefix(path, "/landing/"), "/")

		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// /landing/ean/{ean}
		if parts[0] == "ean" && len(parts) >= 2 {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.HandleLandingPageByEAN(w, r)
			return
		}

		// /landing/{slug}/products
		if len(parts) >= 2 && parts[1] == "products" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.HandleLandingPageProducts(w, r)
			return
		}

		// /landing/{slug}
		if len(parts) == 1 {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.HandleLandingPageBySlug(w, r)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// Company landing pages (public)
	// GET /company/{slug}
	mux.HandleFunc("/company/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleCompanyLanding(w, r)
	})

	// Admin landing pages
	// GET /admin/landings
	mux.Handle("/admin/landings", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleLandingPagesList(w, r)
		case http.MethodPost:
			h.HandleLandingPageCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin))

	// /admin/landings/{id}
	mux.Handle("/admin/landings/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleLandingPageGet(w, r)
		case http.MethodPut:
			h.HandleLandingPageUpdate(w, r)
		case http.MethodDelete:
			h.HandleLandingPageDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin))

	// EANPage SEO pages
	// GET /shop — root catalog
	// GET /shop/{category_tree}/{slug}
	mux.HandleFunc("/shop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleEANPageByPath(w, r)
	})
	mux.HandleFunc("/shop/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleEANPageByPath(w, r)
	})

	// Attribute values (turbo-based)
	// GET /attributes/{code}/values
	mux.HandleFunc("/attributes/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAttributeValues(w, r)
	})

	// --- Admin AttrDef management ---

	// GET/POST /admin/attrdefs
	mux.Handle("/admin/attrdefs", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleAdminAttrDefsList(w, r)
		case http.MethodPost:
			h.HandleAdminAttrDefCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin))

	// GET/PATCH/DELETE /admin/attrdefs/{code}
	mux.Handle("/admin/attrdefs/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleAdminAttrDefGet(w, r)
		case http.MethodPatch:
			h.HandleAdminAttrDefUpdate(w, r)
		case http.MethodDelete:
			h.HandleAdminAttrDefDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin))

	// --- EANPage Admin ---

	// GET /admin/eanpages
	mux.Handle("/admin/eanpages", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminEANPageList(w, r)
	}), model.RoleAdmin))

	// GET/PATCH/DELETE /admin/eanpages/{id}
	mux.Handle("/admin/eanpages/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// POST /admin/eanpages/catalogize-all
		if path == "/admin/eanpages/catalogize-all" && r.Method == http.MethodPost {
			h.HandleAdminEANPageCatalogizeAll(w, r)
			return
		}

		// POST /admin/eanpages/rebuild-tokens
		if path == "/admin/eanpages/rebuild-tokens" && r.Method == http.MethodPost {
			h.HandleAdminEANPageRebuildTokens(w, r)
			return
		}

		// POST /admin/eanpages/rebuild-tokens/{id}
		if strings.HasPrefix(path, "/admin/eanpages/rebuild-tokens/") && r.Method == http.MethodPost {
			h.HandleAdminEANPageRebuildToken(w, r)
			return
		}

		// POST /admin/eanpages/recalculate-product-counts
		if path == "/admin/eanpages/recalculate-product-counts" && r.Method == http.MethodPost {
			h.HandleAdminEANPageRecalculateCounts(w, r)
			return
		}

		// POST /admin/eanpages/recalculate-min-prices
		if path == "/admin/eanpages/recalculate-min-prices" && r.Method == http.MethodPost {
			h.HandleAdminEANPageRecalculateMinPrices(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.HandleAdminEANPageGet(w, r)
		case http.MethodPatch:
			h.HandleAdminEANPageUpdate(w, r)
		case http.MethodDelete:
			h.HandleAdminEANPageDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin))

	// --- Products ---

	// POST /admin/products/reindex — rebuild all product indexes (admin only)
	mux.Handle("/admin/products/reindex", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminProductsReindex(w, r)
	}), model.RoleAdmin))

	// POST /admin/products/delete-all — delete all products (admin only, destructive)
	mux.Handle("/admin/products/delete-all", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminProductsDeleteAll(w, r)
	}), model.RoleAdmin))

	// GET /products (public catalog)
	mux.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.HandleProductsList(w, r)
		case http.MethodPost:
			// POST /products: works for anyone, but seller gets ownership binding.
			// Auth is optional here; if present and role=seller, company is auto-bound.
			// Use optional auth middleware to populate context.
			jwtMiddleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleProductCreate(w, r)
			})).ServeHTTP(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// GET /products/turbo (turbo-index based search)
	mux.HandleFunc("/products/turbo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleTurboProducts(w, r)
	})

	// /products/{id} and /products/{id}/reviews
	mux.Handle("/products/", jwtMiddleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		// Expected: ["", "products", "{id}", "reviews"?] or ["", "products", "{id}"]
		if len(parts) < 3 || parts[2] == "" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// /products/{id}/reviews
		if len(parts) >= 4 && parts[3] == "reviews" {
			switch r.Method {
			case http.MethodGet:
				// Public: GET /products/{id}/reviews
				h.HandleProductReviewsList(w, r)
			case http.MethodPost:
				// Auth required: POST /products/{id}/reviews (buyer only)
				// OptionalAuth already applied, handler checks auth.
				h.HandleProductReviewCreate(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		// /products/{id}
		switch r.Method {
		case http.MethodGet:
			h.HandleProductGet(w, r)
		case http.MethodPatch:
			h.HandleProductUpdate(w, r)
		case http.MethodDelete:
			h.HandleProductDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// --- Cart endpoints ---

	// POST /cart (create cart)
	mux.Handle("/cart", jwtMiddleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleCartCreate(w, r)
	})))

	// GET /cart/me (current user's cart, requires auth)
	mux.Handle("/cart/me", jwtMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleCartMe(w, r)
	})))

	// /cart/{id} and /cart/{id}/items/*
	mux.HandleFunc("/cart/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		// Expected: ["", "cart", "{id}", "items"?, "{product_id}"?]
		if len(parts) < 3 || parts[2] == "" || parts[2] == "me" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = parts[2] // cartID

		// /cart/{id}/items/{product_id} PATCH
		if len(parts) >= 5 && parts[3] == "items" && parts[4] != "" && r.Method == http.MethodPatch {
			h.HandleCartItemUpdate(w, r)
			return
		}

		// /cart/{id}/items POST
		if len(parts) >= 4 && parts[3] == "items" && r.Method == http.MethodPost {
			h.HandleCartItemAdd(w, r)
			return
		}

		// /cart/{id} GET/DELETE
		if len(parts) == 3 {
			switch r.Method {
			case http.MethodGet:
				h.HandleCartGet(w, r)
			case http.MethodDelete:
				h.HandleCartDelete(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// --- Order endpoints ---

	// POST /orders (create) and GET /orders (list user orders)
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			// Use optional auth to populate context
			jwtMiddleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleOrderCreate(w, r)
			})).ServeHTTP(w, r)
		case http.MethodGet:
			// Use optional auth to populate context
			jwtMiddleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.HandleOrderUserList(w, r)
			})).ServeHTTP(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// /orders/{id} and /orders/{id}/status
	// Use OptionalAuth so handlers can check ownership/role
	mux.Handle("/orders/", jwtMiddleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 3 || parts[2] == "" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = parts[2] // orderID

		// /orders/{id}/status PATCH
		if len(parts) >= 4 && parts[3] == "status" && r.Method == http.MethodPatch {
			h.HandleOrderUpdateStatus(w, r)
			return
		}

		// /orders/{id} GET
		if len(parts) == 3 && r.Method == http.MethodGet {
			h.HandleOrderGet(w, r)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})))

	// --- Payment endpoints (temporarily disabled) ---

	// POST /payments (create payment for order)
	mux.HandleFunc("/payments", paymentsDisabledHandler.ServeHTTP)

	// /payments/{id}, /payments/{id}/confirm, /payments/{id}/refund
	mux.Handle("/payments/", paymentsDisabledHandler)

	// Webhook endpoint (no auth, signature-based)
	mux.HandleFunc("/payments/webhook/", paymentsDisabledHandler.ServeHTTP)

	// Admin: timeout cleanup
	mux.Handle("/admin/payments/timeout-cleanup", paymentsDisabledHandler)

	// --- Admin: DB shard usage ---

	// GET /admin/db/shards — fast stats
	mux.Handle("/admin/db/shards", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminDBShards(w, r)
	}), model.RoleAdmin))

	// GET /admin/db/shards/active — precise stats (slow)
	mux.Handle("/admin/db/shards/active", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminDBShardsActive(w, r)
	}), model.RoleAdmin))

	// POST /admin/db/compact — compact all shards (slow, admin only)
	mux.Handle("/admin/db/compact", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminDBCompact(w, r)
	}), model.RoleAdmin))

	// GET /admin/stats — aggregated request metrics
	mux.Handle("/admin/stats", spaAwareHandler(jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminStats(w, r)
	}), model.RoleAdmin)))

	// --- Visit Statistics ---

	// GET /admin/stats/visits/summary — visit statistics summary
	mux.Handle("/admin/stats/visits/summary", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleStatsSummary(w, r)
	}), model.RoleAdmin))

	// GET /admin/stats/visits/referrers — visit statistics by referrer
	mux.Handle("/admin/stats/visits/referrers", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleStatsReferrers(w, r)
	}), model.RoleAdmin))

	// GET /admin/stats/visits/paths — visit statistics by path
	mux.Handle("/admin/stats/visits/paths", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleStatsPaths(w, r)
	}), model.RoleAdmin))

	// POST /admin/stats/visits/toggle — enable/disable visit stats
	mux.Handle("/admin/stats/visits/toggle", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleStatsToggle(w, r)
	}), model.RoleAdmin))

	// GET /admin/stats/visits/status — visit stats status
	mux.Handle("/admin/stats/visits/status", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleStatsStatus(w, r)
	}), model.RoleAdmin))

	// GET /admin/debug/turbo-key?key=... — read raw turbo key (TEMP)
	mux.Handle("/admin/debug/turbo-key", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		data, err := h.Store().DB().TurboRawRead(key)
		if err != nil {
			fmt.Fprintf(w, "error: %v\n", err)
			return
		}
		fmt.Fprintf(w, "key=%s len=%d data=%s\n", key, len(data), string(data))
	}), model.RoleAdmin))

	// --- Catalogizer ---

	// POST /admin/catalogizer/train — train token index from normalized files
	mux.Handle("/admin/catalogizer/train", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminCatalogizerTrain(w, r)
	}), model.RoleAdmin))

	// POST /admin/catalogizer/test — test catalogization on a product name
	mux.Handle("/admin/catalogizer/test", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminCatalogizerTest(w, r)
	}), model.RoleAdmin))

	// GET /admin/catalogizer/coverage — coverage statistics
	mux.Handle("/admin/catalogizer/coverage", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminCatalogizerCoverage(w, r)
	}), model.RoleAdmin))

	// POST /admin/eanpages/rebuild-attr-code-indexes — rebuild attr_code indexes
	mux.Handle("/admin/eanpages/rebuild-attr-code-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminRebuildAttrCodeIndexes(w, r)
	}), model.RoleAdmin))

	// POST /admin/catalogize — run auto-catalogization
	mux.Handle("/admin/catalogize", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminCatalogize(w, r)
	}), model.RoleAdmin))

	// POST /admin/catalogize/product/{id} — catalogize single product
	mux.Handle("/admin/catalogize/product/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminCatalogizeSingle(w, r)
	}), model.RoleAdmin))

	// --- SEO: robots.txt and sitemap ---

	// GET /robots.txt
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		h.HandleRobotsTXT(w, r)
	})

	// GET /sitemap.xml — sitemap index
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		h.HandleSitemapIndex(w, r)
	})

	// GET /sitemap-categories.xml
	mux.HandleFunc("/sitemap-categories.xml", func(w http.ResponseWriter, r *http.Request) {
		h.HandleSitemapCategories(w, r)
	})

	// Serve built frontend static assets (JS/CSS/images) from frontend/dist/.
	// In production the Go server serves both the SPA and the API on the same
	// origin; these routes cover the hashed build artifacts referenced by
	// dist/index.html, which would otherwise fall through to 404.
	distDir := http.Dir("frontend/dist")
	mux.Handle("/assets/", http.FileServer(distDir))
	mux.Handle("/favicon.svg", http.FileServer(distDir))
	mux.Handle("/icons.svg", http.FileServer(distDir))
	mux.Handle("/koshik.png", http.FileServer(distDir))

	// SPA fallback: for any unmatched path that wants HTML, serve index.html
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Handle /sitemap-eanpage-{N}.xml (Go ServeMux doesn't match this pattern directly)
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/sitemap-eanpage") {
			h.HandleSitemapEANPage(w, r)
			return
		}

		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			strings.Contains(r.Header.Get("Accept"), "text/html") &&
			!strings.Contains(r.URL.Path, ".") {
			// Serve frontend index.html for SPA routes
			index, err := os.ReadFile("frontend/dist/index.html")
			if err != nil {
				// Fallback to src/index.html in dev mode
				index, err = os.ReadFile("frontend/index.html")
				if err != nil {
					http.Error(w, "frontend not built", http.StatusServiceUnavailable)
					return
				}
			}
			w.Header().Set("Content-Type", "text/html")
			// HEAD requests: send headers only, no body
			if r.Method != http.MethodHead {
				w.Write(index)
			}
			return
		}
		http.NotFound(w, r)
	})

	// Metrics writer (low-overhead, async, batch to ./_tmp/metrics)
	var handler http.Handler = mux

	// Security headers (outermost, applies to all responses)
	handler = securityHeadersMiddleware(handler)

	// Maintenance mode middleware
	handler = maintenanceMiddleware(handler)

	// Stats middleware (visit tracking)
	if statsCollector := h.StatsCollector(); statsCollector != nil {
		// Start the stats collector
		statsCollector.Start()
		// Add stats middleware
		handler = stats.StatsMiddleware(statsCollector)(handler)
	}

	metricsWriter, err := metrics.NewWriter("./_tmp/metrics", 1000, 2*time.Second, 50*1024*1024)
	if err != nil {
		log.Printf("WARN: metrics writer init failed: %v", err)
	} else {
		defer metricsWriter.Close()
		handler = metrics.Middleware(metricsWriter)(handler)
	}

	// Production-hardened HTTP server with sane timeouts.
	// Note: WriteTimeout is set high to allow long-running imports to complete.
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      300 * time.Second, // 5 minutes for long imports
		IdleTimeout:       120 * time.Second,
	}

	if cfg.TLSEnabled() {
		// Port 80 handler: HTTPS redirect by default.
		var port80Handler http.Handler = httpsRedirectHandler(cfg.Server.Port)

		if cfg.AutocertEnabled() {
			// Automatic Let's Encrypt certificates via ACME.
			cm := &autocert.Manager{
				Prompt:     autocert.AcceptTOS,
				HostPolicy: autocert.HostWhitelist(cfg.Server.TLS.AutocertDomains...),
				Cache:      autocert.DirCache(cfg.Server.TLS.AutocertCache),
			}
			srv.TLSConfig = cm.TLSConfig()
			// Enable HTTP-01 challenges: serve ACME challenge responses on port 80,
			// falling back to the HTTPS redirect for all other requests. Without this,
			// autocert only supports tls-alpn-01 and fails with "no viable challenge
			// type found" when the CA does not offer that type.
			port80Handler = cm.HTTPHandler(port80Handler)
		}

		// Optional plain-HTTP listener. Serves ACME http-01 challenges (autocert)
		// and/or redirects everything else to HTTPS.
		if cfg.Server.TLS.HTTPPort != "" {
			go func() {
				redirectAddr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.TLS.HTTPPort)
				redirectSrv := &http.Server{
					Addr:              redirectAddr,
					Handler:           port80Handler,
					ReadHeaderTimeout: 5 * time.Second,
				}
				log.Printf("HTTP->HTTPS redirect listener on http://%s", redirectAddr)
				if err := redirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("redirect listener stopped: %v", err)
				}
			}()
		}

		if cfg.AutocertEnabled() {
			log.Printf("Makoshop API server starting on https://%s (autocert, domains: %v, cache: %s)",
				srv.Addr, cfg.Server.TLS.AutocertDomains, cfg.Server.TLS.AutocertCache)
			if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server failed: %v", err)
			}
			return
		}

		// Explicit TLS certificate files.
		log.Printf("Makoshop API server starting on https://%s (TLS)", srv.Addr)
		if err := srv.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
		return
	}

	log.Printf("Makoshop API server starting on http://%s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

// httpsRedirectHandler returns a handler that 301-redirects all requests to
// the HTTPS port. Used by the optional plain-HTTP listener. It preserves the
// request host (domain) but always points to the HTTPS port.
func httpsRedirectHandler(httpsPort string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract the host name without any port.
		host := r.Host
		if host == "" {
			host = r.RemoteAddr
		}
		if idx := strings.LastIndex(host, ":"); idx > 0 {
			host = host[:idx]
		}
		target := "https://" + host + ":" + httpsPort
		http.Redirect(w, r, target+r.RequestURI, http.StatusMovedPermanently)
	})
}

func loadI18n() {
	files, err := i18nFS.ReadDir("i18n")
	if err != nil {
		log.Printf("i18n dir not found, using defaults: %v", err)
		return
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(f.Name(), ".json")
		data, err := i18nFS.ReadFile(fmt.Sprintf("i18n/%s", f.Name()))
		if err != nil {
			log.Printf("i18n load %s failed: %v", lang, err)
			continue
		}
		if err := i18n.Load(lang, data); err != nil {
			log.Printf("i18n parse %s failed: %v", lang, err)
			continue
		}
		log.Printf("i18n loaded: %s", lang)
	}
	// Default language can be set via env or hardcoded
	if lang := os.Getenv("I18N_LANG"); lang != "" {
		i18n.SetCurrent(lang)
	}
}
