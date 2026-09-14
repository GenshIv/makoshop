package stats

import (
	"net/http"
	"strings"
	"time"
)

// BotUserAgents is a list of common bot user agents
var BotUserAgents = []string{
	"googlebot",
	"bingbot",
	"yandexbot",
	"facebookexternalhit",
	"twitterbot",
	"linkedinbot",
	"slackbot",
	"pinterestbot",
	"whatsapp",
	"telegrambot",
	"discordbot",
	"crawler",
	"spider",
	"bot",
	"headless",
	"phantomjs",
	"selenium",
	"puppeteer",
}

// IsBotUserAgent checks if the user agent is a bot
func IsBotUserAgent(userAgent string) bool {
	if userAgent == "" {
		return false
	}

	userAgentLower := strings.ToLower(userAgent)
	for _, bot := range BotUserAgents {
		if strings.Contains(userAgentLower, bot) {
			return true
		}
	}
	return false
}

// isCountablePath checks if a path matches any of the configured countable paths.
func isCountablePath(path string, countablePaths []string) bool {
	for _, prefix := range countablePaths {
		if prefix == "/" {
			// Root matches only exact "/"
			if path == "/" {
				return true
			}
		} else if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// StatsMiddleware creates a middleware that records visits
func StatsMiddleware(collector *StatsCollector) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if stats is enabled
			if !collector.IsEnabled() {
				next.ServeHTTP(w, r)
				return
			}

			// Only count configured paths (default: root + /shop)
			if !isCountablePath(r.URL.Path, collector.config.CountablePaths) {
				next.ServeHTTP(w, r)
				return
			}

			// Quick checks
			isBot := IsBotUserAgent(r.UserAgent())
			referrer := r.Referer()
			categoryID := extractCategoryID(r.URL.Path)
			ip := extractIP(r)

			// Record visit asynchronously
			collector.RecordVisit(VisitEvent{
				IsBot:      isBot,
				Page:       r.URL.Path,
				Referrer:   referrer,
				CategoryID: categoryID,
				Timestamp:  uint32(time.Now().Unix()),
				UserAgent:  r.UserAgent(),
				IP:         ip,
			})

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// extractIP extracts the client IP from the request
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.Split(xff, ",")[0]
	}

	// Check X-Real-IP header (for proxies)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	if r.RemoteAddr != "" {
		// Remove port if present
		if idx := strings.LastIndex(r.RemoteAddr, ":"); idx > 0 {
			return r.RemoteAddr[:idx]
		}
		return r.RemoteAddr
	}

	return ""
}

// extractCategoryID extracts category ID from URL path
// /shop/{category} or /shop/{category}/{slug}
func extractCategoryID(path string) int64 {
	// Check if path starts with /shop/
	if !strings.HasPrefix(path, "/shop/") {
		return 0
	}

	// Split path
	parts := strings.SplitN(path, "/", 4)
	if len(parts) < 3 {
		return 0
	}

	// parts[2] is the category slug or ID
	categoryPart := parts[2]
	if categoryPart == "" {
		return 0
	}

	// For now, return 0 (we would need to look up the category ID)
	// This is a placeholder - in production, we would use a cache or lookup
	return 0
}
