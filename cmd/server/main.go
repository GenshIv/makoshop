package main

import (
	"embed"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
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

func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	// Формируем новый URL с протоколом https
	target := "https://" + r.Host + r.RequestURI
	http.Redirect(w, r, target, http.StatusMovedPermanently)
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
