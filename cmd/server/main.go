package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/GenshIv/makoshop/internal/api"
	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	cfg := config.DefaultConfig()

	store, err := db.NewStore(cfg.Database)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// Auth middleware
	jwtMiddleware := auth.NewJWTMiddleware(cfg.Auth.JWTSecret)

	// Repositories
	userRepo := db.NewUserRepo(store)
	companyRepo := db.NewCompanyRepo(store)
	cartRepo := db.NewCartRepo(store)

	h := api.NewHandlers(store)
	authHandlers := api.NewAuthHandlers(userRepo, companyRepo, cartRepo, jwtMiddleware, cfg.Auth.JWTSecret)

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
			h.HandleCategoriesList(w, r)
		case http.MethodPost:
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

	// Attribute values (turbo-based)
	// GET /attributes/{code}/values
	mux.HandleFunc("/attributes/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAttributeValues(w, r)
	})

	// --- Products ---

	// POST /admin/products/reindex — rebuild all product indexes (admin only)
	mux.Handle("/admin/products/reindex", jwtMiddleware.RequireRole(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.HandleAdminProductsReindex(w, r)
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

	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Makoshop API server starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
