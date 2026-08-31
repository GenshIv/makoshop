package router

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"
)

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

		// Allow auth endpoints (login, register, etc.) so users can still access the app
		if strings.HasPrefix(r.URL.Path, "/auth") {
			next.ServeHTTP(w, r)
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
		// Google Analytics domains are permitted so gtag.js can load (script-src)
		// and page-view data can be sent (connect-src). Kept to specific Google
		// hosts rather than a broad https: to stay restrictive.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline' https://www.googletagmanager.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self' https://*.google-analytics.com https://analytics.google.com https://*.googletagmanager.com https://*.doubleclick.net")
		next.ServeHTTP(w, r)
	})
}

// compressiblePrefixes are Content-Type prefixes worth gzipping. Binary assets
// (images, webp) are excluded since gzip would not shrink them meaningfully.
var compressiblePrefixes = []string{
	"text/",
	"application/json",
	"application/javascript",
	"application/xml",
	"image/svg+xml",
}

func isCompressible(contentType string) bool {
	for _, p := range compressiblePrefixes {
		if strings.HasPrefix(contentType, p) {
			return true
		}
	}
	return false
}

// gzipResponseWriter decides whether to compress at WriteHeader time (when the
// Content-Type is already known) and then streams gzip directly. This avoids
// buffering large responses (e.g. multi-MB sitemaps) in memory.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	started bool
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.started {
		return
	}
	g.started = true
	if isCompressible(g.Header().Get("Content-Type")) {
		h := g.Header()
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		// Compressed length differs from the original; drop it.
		h.Del("Content-Length")
		g.gz = gzip.NewWriter(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.started {
		// Some handlers write without an explicit WriteHeader call.
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// gzipMiddleware compresses responses with gzip when the client supports it.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		next.ServeHTTP(gw, r)
		if gw.gz != nil {
			gw.gz.Close()
		}
	})
}
