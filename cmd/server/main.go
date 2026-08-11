package main

import (
	"embed"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/api"
	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/i18n"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

//go:embed i18n/*.json
var i18nFS embed.FS

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

func main() {
	cfg := config.DefaultConfig()

	// Load i18n translations
	loadI18n()

	store, err := db.NewStore(cfg.Database)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// Repositories (needed for superadmin bootstrap)
	userRepo := db.NewUserRepo(store)

	// Bootstrap superadmin if no admins exist
	bootstrapSuperAdmin(userRepo)

	// Auth middleware
	jwtMiddleware := auth.NewJWTMiddleware(cfg.Auth.JWTSecret)

	// Repositories
	userRepo = db.NewUserRepo(store)
	companyRepo := db.NewCompanyRepo(store)
	cartRepo := db.NewCartRepo(store)

	h := api.NewHandlers(store)
	authHandlers := api.NewAuthHandlers(userRepo, companyRepo, cartRepo, jwtMiddleware, cfg.Auth.JWTSecret)
	// Attach turboSearch to authHandlers for company products endpoint
	authHandlers.SetTurboSearch(h.TurboSearch())

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Pprof endpoints (for profiling/debugging)
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))

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
	mux.Handle("/admin/users", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHandlers.HandleAdminUsersList(w, r)
	}), model.RoleAdmin))

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
	mux.Handle("/admin/companies", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			authHandlers.HandleAdminCompaniesList(w, r)
		case http.MethodPost:
			authHandlers.HandleAdminCompanyCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}), model.RoleAdmin))

	// POST /admin/companies/create-test — create test companies
	mux.Handle("/admin/companies/create-test", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		authHandlers.HandleAdminCreateTestCompanies(w, r)
	}), model.RoleAdmin))

	// GET/PATCH /admin/companies/{id}
	mux.Handle("/admin/companies/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// /admin/companies/{id}/verify
		if strings.HasSuffix(path, "/verify") {
			if r.Method == http.MethodPatch {
				authHandlers.HandleAdminCompanyVerify(w, r)
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
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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

	// POST /admin/rebuild-scupages — rebuild all SCU pages from products
	mux.Handle("/admin/rebuild-scupages", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildSCUPages(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-scupage-indexes — index all SCU pages into SCUPageSearch
	mux.Handle("/admin/rebuild-scupage-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildSCUPageIndexes(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

	// POST /admin/rebuild-scupage-sort-indexes — rebuild sort indexes for SCU pages
	mux.Handle("/admin/rebuild-scupage-sort-indexes", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.HandleAdminRebuildSCUPageSortIndexes(w, r)
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

	// GET /admin/products/import/{id}
	mux.Handle("/admin/products/import/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.HandleAdminImportStatus(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}), model.RoleAdmin))

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

	mux.HandleFunc("/admin/categories", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// GET /admin/categories/export
			if r.URL.Query().Get("export") == "1" {
				h.HandleAdminCategoriesExport(w, r)
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
	})

	mux.HandleFunc("/admin/categories/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

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

		// /landing/scu/{scu}
		if parts[0] == "scu" && len(parts) >= 2 {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.HandleLandingPageBySCU(w, r)
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

	// SCUPage SEO pages
	// GET /shop — root catalog
	// GET /shop/{category_tree}/{slug}
	mux.HandleFunc("/shop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleSCUPageByPath(w, r)
	})
	mux.HandleFunc("/shop/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleSCUPageByPath(w, r)
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

	// --- SCUPage Admin ---

	// GET /admin/scupages
	mux.Handle("/admin/scupages", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminSCUPageList(w, r)
	}), model.RoleAdmin))

	// GET/PATCH/DELETE /admin/scupages/{id}
	mux.Handle("/admin/scupages/", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// POST /admin/scupages/relink
		if path == "/admin/scupages/relink" && r.Method == http.MethodPost {
			h.HandleAdminSCUPageRelink(w, r)
			return
		}

		// POST /admin/scupages/catalogize-all
		if path == "/admin/scupages/catalogize-all" && r.Method == http.MethodPost {
			h.HandleAdminSCUPageCatalogizeAll(w, r)
			return
		}

		// POST /admin/scupages/rebuild-tokens
		if path == "/admin/scupages/rebuild-tokens" && r.Method == http.MethodPost {
			h.HandleAdminSCUPageRebuildTokens(w, r)
			return
		}

		// POST /admin/scupages/rebuild-tokens/{id}
		if strings.HasPrefix(path, "/admin/scupages/rebuild-tokens/") && r.Method == http.MethodPost {
			h.HandleAdminSCUPageRebuildToken(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.HandleAdminSCUPageGet(w, r)
		case http.MethodPatch:
			h.HandleAdminSCUPageUpdate(w, r)
		case http.MethodDelete:
			h.HandleAdminSCUPageDelete(w, r)
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
		case http.MethodGet:
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
		if r.Method != http.MethodGet {
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

	// --- Payment endpoints ---

	// POST /payments (create payment for order)
	mux.HandleFunc("/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandlePaymentCreate(w, r)
	})

	// /payments/{id}, /payments/{id}/confirm, /payments/{id}/refund
	mux.Handle("/payments/", jwtMiddleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 3 || parts[2] == "" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// /payments/{id}/confirm POST
		if len(parts) >= 4 && parts[3] == "confirm" && r.Method == http.MethodPost {
			h.HandlePaymentConfirm(w, r)
			return
		}

		// /payments/{id}/refund POST (admin only, checked in handler)
		if len(parts) >= 4 && parts[3] == "refund" && r.Method == http.MethodPost {
			h.HandlePaymentRefund(w, r)
			return
		}

		// /payments/{id} GET
		if len(parts) == 3 && r.Method == http.MethodGet {
			h.HandlePaymentGet(w, r)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})))

	// Webhook endpoint (no auth, signature-based)
	mux.HandleFunc("/payments/webhook/", func(w http.ResponseWriter, r *http.Request) {
		h.HandlePaymentWebhook(w, r)
	})

	// Admin: timeout cleanup
	mux.Handle("/admin/payments/timeout-cleanup", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandlePaymentTimeoutCleanup(w, r)
	}), model.RoleAdmin))

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

	// SPA fallback: for any unmatched path that wants HTML, serve index.html
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Handle /sitemap-scupage-{N}.xml (Go ServeMux doesn't match this pattern directly)
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/sitemap-scupage") {
			h.HandleSitemapSCUPage(w, r)
			return
		}

		if r.Method == http.MethodGet &&
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
			w.Write(index)
			return
		}
		http.NotFound(w, r)
	})

	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Makoshop API server starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
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
