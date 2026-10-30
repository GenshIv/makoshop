package main

import (
	"embed"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/GenshIv/makoshop/internal/api"
	"github.com/GenshIv/makoshop/internal/api/router"
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
		Email:        "info@wszyst.pl",
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
	fmt.Printf("Email:    info@wszyst.pl\n")
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

// applyMemoryLimit applies the GOMEMLIMIT environment variable (if set) as the
// runtime soft memory limit. Accepted formats: plain bytes ("5368709120"),
// "512MiB"/"6GiB", or short "512M"/"6G" (binary multiples, case-insensitive).
func applyMemoryLimit() {
	v := strings.TrimSpace(os.Getenv("GOMEMLIMIT"))
	if v == "" {
		return
	}
	n, err := parseMemoryLimit(v)
	if err != nil || n <= 0 {
		log.Printf("WARN: ignoring invalid GOMEMLIMIT %q: %v", v, err)
		return
	}
	debug.SetMemoryLimit(n)
	log.Printf("GOMEMLIMIT applied: soft memory limit set to %s (%d bytes)", v, n)
}

// parseMemoryLimit parses a byte count with an optional binary suffix
// (KiB/MiB/GiB or K/M/G).
func parseMemoryLimit(s string) (int64, error) {
	units := []struct {
		suffix string
		mult   int64
	}{
		{"gib", 1 << 30}, {"mib", 1 << 20}, {"kib", 1 << 10},
		{"g", 1 << 30}, {"m", 1 << 20}, {"k", 1 << 10},
	}
	ls := strings.ToLower(strings.TrimSpace(s))
	for _, u := range units {
		if strings.HasSuffix(ls, u.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(ls, u.suffix))
			num, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("bad number in %q", s)
			}
			return num * u.mult, nil
		}
	}
	n, err := strconv.ParseInt(ls, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a valid size")
	}
	return n, nil
}

func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	// Формируем новый URL с протоколом https
	target := "https://" + r.Host + r.RequestURI
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// ipToHostRedirect checks if request comes from an IP address and redirects to
// the configured hostname (from SiteURL). Returns true if redirect was performed.
func ipToHostRedirect(siteURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if siteURL == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Parse expected host from SiteURL
			expectedHost := parseHostFromURL(siteURL)
			if expectedHost == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Get request host (strip port)
			requestHost := r.Host
			if idx := strings.LastIndex(requestHost, ":"); idx > 0 {
				requestHost = requestHost[:idx]
			}

			// If request comes from IP address, redirect to hostname
			if isIPAddress(requestHost) {
				target := siteURL + r.RequestURI
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseHostFromURL(url string) string {
	s := strings.TrimPrefix(url, "http://")
	s = strings.TrimPrefix(s, "https://")
	if idx := strings.Index(s, "/"); idx > 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "?"); idx > 0 {
		s = s[:idx]
	}
	return s
}

func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

func main() {
	// Automatically load .env (if present) so the server picks up its
	// configuration without the operator exporting variables manually.
	config.LoadEnv()

	// Apply the soft memory limit AFTER .env loading: the Go runtime reads
	// GOMEMLIMIT only at process start, before main runs, so a value from .env
	// would be silently ignored. The limit makes the GC push back (run harder)
	// as the heap approaches it, instead of letting RSS grow until the kernel
	// OOM killer fires during large price-file imports.
	applyMemoryLimit()

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
	deliveryMethodRepo := db.NewDeliveryMethodRepo(store)
	installmentPlanRepo := db.NewInstallmentPlanRepo(store)

	// Set the prices directory for import operations (XML/JSON).
	api.SetPricesDir(cfg.Import.PricesDir)

	h := api.NewHandlers(store)
	// Set the canonical site base URL (for sitemaps, robots.txt, canonicals).
	h.SetSiteURL(cfg.Server.SiteURL)
	// Load production frontend asset tags so browser SSR pages reference the
	// built bundles (not the dev /src/main.js) on deep-link navigation.
	api.LoadBrowserAssetTags()
	// Attach company settings repos to handlers
	h.SetCompanySettingsRepos(companyRepo, paymentMethodRepo, deliveryTimeRepo, deliveryMethodRepo, installmentPlanRepo)
	authHandlers := api.NewAuthHandlers(userRepo, companyRepo, cartRepo, jwtMiddleware, cfg.Auth.JWTSecret)
	// Attach turboSearch to authHandlers for company products endpoint
	authHandlers.SetTurboSearch(h.TurboSearch())
	// Attach productRepo to authHandlers for company deletion (price cleanup)
	authHandlers.SetProductRepo(h.ProductRepo())

	// Build the router: the full route table and the middleware chain (security
	// headers, maintenance, stats, metrics, gzip) live in internal/api/router.
	rt, err := router.New(router.Deps{
		Handlers:     h,
		AuthHandlers: authHandlers,
		JWT:          jwtMiddleware,
		SiteURL:      cfg.Server.SiteURL,
	})
	if err != nil {
		log.Fatalf("failed to build router: %v", err)
	}
	defer rt.Close()

	handler := rt.Handler()

	// Apply IP-to-host redirect middleware to the main handler (HTTPS listener)
	handler = ipToHostRedirect(cfg.Server.SiteURL)(handler)

	// Periodic cache rebuild every 15 minutes (background, fire-and-forget)
	cacheTicker := time.NewTicker(15 * time.Minute)
	go func() {
		for range cacheTicker.C {
			log.Println("Periodic cache rebuild starting...")
			if err := h.InvalidateAndReloadCatAttrs(); err != nil {
				log.Printf("Periodic cache rebuild error: %v", err)
			} else {
				log.Println("Periodic cache rebuild complete")
			}
		}
	}()

	// Preload the category path map (background, fire-and-forget).
	// The former EAN page "preload" (eanRepo.List()) is deliberately GONE:
	// List() materializes a copy of every page document (getKey128Copy) and
	// unmarshals all of them just to discard the result — gigabytes of
	// transient garbage at every startup, racing with the first import after
	// a restart (heap profiles caught it at ~2.5GB resident).
	go func() {
		catRepo := db.NewCategoryRepo(store)

		if _, err := catRepo.BuildPathMap(); err != nil {
			log.Printf("Category repo preload error: %v", err)
		} else {
			log.Println("Category repo preload complete")
		}
	}()

	// Preload category attributes and tree paths (background, fire-and-forget)
	go func() {
		log.Println("Preloading category attributes and tree paths...")
		if err := h.LoadAllTreePaths(); err != nil {
			log.Printf("Category tree paths preload error: %v", err)
		} else {
			log.Println("Category tree paths preload complete")
		}

		if err := h.LoadAllCategoryAttrs(); err != nil {
			log.Printf("Category attrs preload error: %v", err)
		} else {
			log.Printf("Category attrs preload complete: %d categories", h.CatAttrsCount())
		}
	}()

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

		// Apply IP-to-host redirect before HTTPS redirect on port 80
		port80Handler = ipToHostRedirect(cfg.Server.SiteURL)(port80Handler)

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
