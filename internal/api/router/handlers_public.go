package router

import (
	"net/http"
	"os"
	"strings"
)

// handlers_public.go holds the per-route handler methods for public
// (non-/admin) routes. Each method wraps one route's logic (method checks,
// path dispatch, auth delegation) so registerRoutes stays a clean table.

// --- Health & maintenance ---
func (d *Deps) health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// --- Auth endpoints ---

// POST /auth/register (public)
func (d *Deps) register(w http.ResponseWriter, r *http.Request) {
	d.AuthHandlers.HandleRegister(w, r)
}

// POST /auth/login (public)
func (d *Deps) login(w http.ResponseWriter, r *http.Request) {
	d.AuthHandlers.HandleLogin(w, r)
}

// GET /auth/me (requires auth)
func (d *Deps) authMe(w http.ResponseWriter, r *http.Request) {
	d.AuthHandlers.HandleMe(w, r)
}

// PATCH /users/me (requires auth)
func (d *Deps) usersMe(w http.ResponseWriter, r *http.Request) {
	d.AuthHandlers.HandleUpdateMe(w, r)
}

// --- Promo endpoints ---

// GET /promo/plans (public)
func (d *Deps) promoPlans(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandlePromoPlansList(w, r)
}

// /companies/{id}/orders and /companies/{id}/promo-campaigns (auth)
func (d *Deps) companiesSub(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	// /companies/{id}/promo-campaigns -> parts = ["", "companies", "{id}", "promo-campaigns"]
	if len(parts) >= 4 && parts[3] == "promo-campaigns" {
		switch r.Method {
		case http.MethodGet:
			d.Handlers.HandleCompanyPromoCampaignsList(w, r)
		case http.MethodPost:
			d.Handlers.HandleCompanyPromoCampaignCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	// /companies/{id}/orders -> parts = ["", "companies", "{id}", "orders"]
	if len(parts) >= 4 && parts[3] == "orders" {
		if r.Method == http.MethodGet {
			d.Handlers.HandleCompanyOrders(w, r)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// PATCH /promo-campaigns/{id}/status (auth)
func (d *Deps) promoCampaignsStatus(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandlePromoCampaignUpdateStatus(w, r)
}

// POST /promo/logs
func (d *Deps) promoLogs(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandlePromoLogCreate(w, r)
}

// --- Review endpoints ---

// GET /reviews?user_id=... (auth required, own reviews only)
func (d *Deps) reviews(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleUserReviewsList(w, r)
}

// GET /seller/reviews (seller access)
func (d *Deps) sellerReviews(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleSellerReviews(w, r)
}

// --- Comment endpoints ---

// /comments — POST (auth) для создания, GET (public) для списка
func (d *Deps) comments(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.JWT.RequireAuth(http.HandlerFunc(d.Handlers.HandleCommentCreate)).ServeHTTP(w, r)
	} else {
		d.Handlers.HandleCommentsList(w, r)
	}
}

// --- Vote endpoints ---

// /votes — POST (auth) для голосования
func (d *Deps) votes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		d.JWT.RequireAuth(http.HandlerFunc(d.Handlers.HandleVoteCreate)).ServeHTTP(w, r)
	} else {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /votes/check (auth required)
func (d *Deps) votesCheck(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleVoteCheck(w, r)
}

// --- Categories (public and admin share same CRUD for now) ---

// GET /categories
func (d *Deps) categories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleCategoriesList(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /categories/tree
func (d *Deps) categoriesTree(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleCategoriesTree(w, r)
}

// GET /categories/{id} — public category by id (and /categories/tree_path/{id})
func (d *Deps) categoriesByPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Path

	// GET /categories/tree_path/{id} — full path from root with full category data
	if strings.Contains(path, "/tree_path/") {
		d.Handlers.HandleCategoryTreePath(w, r)
		return
	}

	d.Handlers.HandleCategoryGet(w, r)
}

// --- Brands ---

// GET /brands
func (d *Deps) brands(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleBrandsList(w, r)
}

// --- Companies (public read, admin write) ---

// Unified handler for /companies, /companies/{id}, /companies/slug/{slug}, /companies/{id}/products
func (d *Deps) companies(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/companies"), "/")

	// /companies — list
	if path == "/companies" || (len(parts) == 1 && parts[0] == "") {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		d.AuthHandlers.HandleCompaniesList(w, r)
		return
	}

	// /companies/slug/{slug}
	if len(parts) >= 2 && parts[0] == "slug" && parts[1] != "" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		d.AuthHandlers.HandleCompanyGetBySlug(w, r)
		return
	}

	// /companies/{id}/products
	if len(parts) >= 3 && parts[0] != "" && parts[1] == "products" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		d.AuthHandlers.HandleCompanyProducts(w, r)
		return
	}

	// /companies/{id}
	if len(parts) == 2 && parts[0] != "" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		d.AuthHandlers.HandleCompanyGet(w, r)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Landing pages (public) ---

// GET /landing/{slug} (and /landing/ean/{ean}, /landing/{slug}/products)
func (d *Deps) landing(w http.ResponseWriter, r *http.Request) {
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
		d.Handlers.HandleLandingPageByEAN(w, r)
		return
	}

	// /landing/{slug}/products
	if len(parts) >= 2 && parts[1] == "products" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		d.Handlers.HandleLandingPageProducts(w, r)
		return
	}

	// /landing/{slug}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		d.Handlers.HandleLandingPageBySlug(w, r)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Company landing pages (public) ---

// GET /company/{slug}
func (d *Deps) companyLanding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleCompanyLanding(w, r)
}

// --- Shop endpoints (EANPage SEO pages) ---

// GET /shop — root catalog
func (d *Deps) shop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleEANPageByPath(w, r)
}

// GET /shop/{category_tree}/{slug}
func (d *Deps) shopSub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleEANPageByPath(w, r)
}

// GET /home/offers — random category sections for the storefront home page
func (d *Deps) homeOffers(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleHomeOffers(w, r)
}

// GET /attributes/{code}/values (turbo-based)
func (d *Deps) attributeValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleAttributeValues(w, r)
}

// GET /products (public catalog); POST /products (optional auth)
func (d *Deps) products(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		d.Handlers.HandleProductsList(w, r)
	case http.MethodPost:
		// POST /products: works for anyone, but seller gets ownership binding.
		// Auth is optional here; if present and role=seller, company is auto-bound.
		// Use optional auth middleware to populate context.
		d.JWT.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleProductCreate(w, r)
		})).ServeHTTP(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /products/turbo (turbo-index based search)
func (d *Deps) productsTurbo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleTurboProducts(w, r)
}

// /products/{id} and /products/{id}/reviews
func (d *Deps) product(w http.ResponseWriter, r *http.Request) {
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
			d.Handlers.HandleProductReviewsList(w, r)
		case http.MethodPost:
			// Auth required: POST /products/{id}/reviews (buyer only)
			// OptionalAuth already applied, handler checks auth.
			d.Handlers.HandleProductReviewCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /products/{id}
	switch r.Method {
	case http.MethodGet:
		d.Handlers.HandleProductGet(w, r)
	case http.MethodPatch:
		d.Handlers.HandleProductUpdate(w, r)
	case http.MethodDelete:
		d.Handlers.HandleProductDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Cart endpoints ---

// POST /cart (create cart)
func (d *Deps) cart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d.Handlers.HandleCartCreate(w, r)
}

// GET /cart/me (current user's cart, requires auth)
func (d *Deps) cartMe(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleCartMe(w, r)
}

// /cart/{id} and /cart/{id}/items/*
func (d *Deps) cartByPath(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "cart", "{id}", "items"?, "{product_id}"?]
	if len(parts) < 3 || parts[2] == "" || parts[2] == "me" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = parts[2] // cartID

	// /cart/{id}/items/{product_id} PATCH
	if len(parts) >= 5 && parts[3] == "items" && parts[4] != "" && r.Method == http.MethodPatch {
		d.Handlers.HandleCartItemUpdate(w, r)
		return
	}

	// /cart/{id}/items POST
	if len(parts) >= 4 && parts[3] == "items" && r.Method == http.MethodPost {
		d.Handlers.HandleCartItemAdd(w, r)
		return
	}

	// /cart/{id} GET/DELETE
	if len(parts) == 3 {
		switch r.Method {
		case http.MethodGet:
			d.Handlers.HandleCartGet(w, r)
		case http.MethodDelete:
			d.Handlers.HandleCartDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- Order endpoints ---

// POST /orders (create) and GET /orders (list user orders)
func (d *Deps) orders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// Use optional auth to populate context
		d.JWT.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleOrderCreate(w, r)
		})).ServeHTTP(w, r)
	case http.MethodGet:
		// Use optional auth to populate context
		d.JWT.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.Handlers.HandleOrderUserList(w, r)
		})).ServeHTTP(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /orders/{id} and /orders/{id}/status (OptionalAuth so handlers can check ownership/role)
func (d *Deps) order(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 || parts[2] == "" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = parts[2] // orderID

	// /orders/{id}/status PATCH
	if len(parts) >= 4 && parts[3] == "status" && r.Method == http.MethodPatch {
		d.Handlers.HandleOrderUpdateStatus(w, r)
		return
	}

	// /orders/{id} GET
	if len(parts) == 3 && r.Method == http.MethodGet {
		d.Handlers.HandleOrderGet(w, r)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// --- SEO: robots.txt and sitemap ---

// GET /robots.txt
func (d *Deps) robotsTXT(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleRobotsTXT(w, r)
}

// GET /sitemap.xml — sitemap index
func (d *Deps) sitemapIndex(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleSitemapIndex(w, r)
}

// GET /sitemap-categories.xml
func (d *Deps) sitemapCategories(w http.ResponseWriter, r *http.Request) {
	d.Handlers.HandleSitemapCategories(w, r)
}

// --- SPA fallback ---

// / — SPA fallback: serve index.html for client-side routes, with SEO
// enhancement for the homepage and known static routes.
func (d *Deps) spaFallback(w http.ResponseWriter, r *http.Request) {
	// Handle /sitemap-eanpage-{N}.xml (Go ServeMux doesn't match this pattern directly)
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/sitemap-eanpage") {
		d.Handlers.HandleSitemapEANPage(w, r)
		return
	}

	// Catch-all for client-side SPA routes (/login, /register, /privacy-policy,
	// /checkout, ...). All real API routes are registered as specific mux
	// patterns and take precedence over this handler, so only unknown/SPA
	// paths reach here. Serve the index.html shell for any extension-less
	// GET/HEAD request regardless of Accept: bots and link-checkers send no or
	// minimal Accept headers, and gating on "text/html" made these pages 404.
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
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
		// Homepage: inject SEO tags (canonical, OG, JSON-LD, Polish meta).
		if r.URL.Path == "/" {
			index = enhanceHomepageHTML(index, strings.TrimRight(d.SiteURL, "/"))
		} else if _, _, body := staticRouteContent(r.URL.Path); body != "" {
			// Client-side routes that would otherwise be blank shells for
			// non-JS renderers: serve server-rendered content.
			index = enhanceStaticRouteHTML(index, strings.TrimRight(d.SiteURL, "/"), r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		// HEAD requests: send headers only, no body
		if r.Method != http.MethodHead {
			w.Write(index)
		}
		return
	}
	http.NotFound(w, r)
}
