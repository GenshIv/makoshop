package router

import (
	"net/http"
	"os"
	"strings"
)

// cachedAssetServer serves dist assets with long-lived immutable caching.
// Build artifacts are content-hashed in their filenames (e.g. index-AbC123.js),
// so browsers can cache them aggressively without risk of stale content.
func cachedAssetServer(dir http.Dir) http.Handler {
	fs := http.FileServer(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fs.ServeHTTP(w, r)
	})
}

// enhanceHomepageHTML injects SEO tags (canonical, OG, JSON-LD) plus Polish
// lang/title/description into the SPA index.html for the "/" route. The built
// index.html is a thin shell without these; crawlers only see this markup for
// the homepage, so we enrich it at serve time rather than rebuilding the SPA.
func enhanceHomepageHTML(content []byte, siteURL string) []byte {
	s := string(content)

	// Language: Polish storefront.
	s = strings.Replace(s, `<html lang="en">`, `<html lang="pl">`, 1)

	const title = "wszyst.pl — Katalog produktów online | Najlepsze ceny od sprawdzonych sprzedawców"
	const desc = "wszyst.pl to online katalog z tysiącami produktów. Porównuj ceny, czytaj opinie i kupuj od zweryfikowanych sprzedawców."

	// Replace <title>...</title>.
	if i := strings.Index(s, "<title>"); i >= 0 {
		if j := strings.Index(s[i:], "</title>"); j >= 0 {
			s = s[:i] + "<title>" + title + "</title>" + s[i+j+len("</title>"):]
		}
	}

	// Replace description meta content.
	const descTag = `<meta name="description" content="`
	if i := strings.Index(s, descTag); i >= 0 {
		start := i + len(descTag)
		if j := strings.Index(s[start:], `" />`); j >= 0 {
			s = s[:start] + desc + s[start+j:]
		}
	}

	jsonLD := `{
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "Organization",
      "@id": "` + siteURL + `#org",
      "name": "wszyst.pl",
      "url": "` + siteURL + `/"
    },
    {
      "@type": "WebSite",
      "@id": "` + siteURL + `#website",
      "url": "` + siteURL + `/",
      "name": "wszyst.pl",
      "publisher": {"@id": "` + siteURL + `#org"},
      "inLanguage": "pl"
    }
  ]
}`

	inject := `<link rel="canonical" href="` + siteURL + `/" />
    <meta property="og:type" content="website" />
    <meta property="og:site_name" content="wszyst.pl" />
    <meta property="og:title" content="` + title + `" />
    <meta property="og:description" content="` + desc + `" />
    <meta property="og:url" content="` + siteURL + `/" />
    <script type="application/ld+json">` + jsonLD + `</script>`

	s = strings.Replace(s, "</head>", inject+"\n  </head>", 1)

	return []byte(s)
}

// staticRouteContent returns the server-rendered HTML body for a client-side
// route that would otherwise be an empty shell to non-JS renderers (crawlers,
// accessibility scanners). Vue replaces this markup on mount, so it only ever
// shows when JavaScript is unavailable — but it keeps these pages from being
// flagged as "blank". Content mirrors the corresponding Vue view (Polish).
func staticRouteContent(path string) (title, desc, body string) {
	switch path {
	case "/login":
		title = "Logowanie — wszyst.pl"
		desc = "Zaloguj się na swoje konto w wszyst.pl."
		body = `<div class="min-h-[60vh] flex items-center justify-center px-4">
      <div class="w-full max-w-md bg-surface rounded-xl border border-line shadow-sm p-6 sm:p-8">
        <h1 class="text-2xl font-bold mb-6 text-center text-ink">Logowanie</h1>
        <form class="space-y-4" method="post" action="/auth/login">
          <div>
            <label class="block text-sm text-ink-2 mb-1" for="login-email">Email</label>
            <input id="login-email" name="email" type="email" autocomplete="email" required class="w-full px-3 py-2 border border-line rounded-lg bg-surface-2/50" />
          </div>
          <div>
            <label class="block text-sm text-ink-2 mb-1" for="login-password">Hasło</label>
            <input id="login-password" name="password" type="password" autocomplete="current-password" required class="w-full px-3 py-2 border border-line rounded-lg bg-surface-2/50" />
          </div>
          <button type="submit" class="w-full btn btn-primary">Logowanie</button>
        </form>
        <p class="mt-4 text-center text-sm text-ink-2">Nie masz konta? <a href="/register" class="text-accent hover:underline">Zarejestruj się</a></p>
      </div>
    </div>`
	case "/register":
		title = "Rejestracja — wszyst.pl"
		desc = "Utwórz nowe konto w wszyst.pl."
		body = `<div class="min-h-[60vh] flex items-center justify-center px-4">
      <div class="w-full max-w-md bg-surface rounded-xl border border-line shadow-sm p-6 sm:p-8">
        <h1 class="text-2xl font-bold mb-6 text-center text-ink">Rejestracja</h1>
        <form class="space-y-4" method="post" action="/auth/register">
          <div>
            <label class="block text-sm text-ink-2 mb-1" for="reg-name">Imię</label>
            <input id="reg-name" name="name" type="text" autocomplete="given-name" required class="w-full px-3 py-2 border border-line rounded-lg bg-surface-2/50" />
          </div>
          <div>
            <label class="block text-sm text-ink-2 mb-1" for="reg-email">Email</label>
            <input id="reg-email" name="email" type="email" autocomplete="email" required class="w-full px-3 py-2 border border-line rounded-lg bg-surface-2/50" />
          </div>
          <div>
            <label class="block text-sm text-ink-2 mb-1" for="reg-password">Hasło</label>
            <input id="reg-password" name="password" type="password" autocomplete="new-password" minlength="6" required class="w-full px-3 py-2 border border-line rounded-lg bg-surface-2/50" />
          </div>
          <div>
            <label class="block text-sm text-ink-2 mb-1" for="reg-role">Rola</label>
            <select id="reg-role" name="role" class="w-full px-3 py-2 border border-line rounded-lg bg-surface-2/50">
              <option value="buyer">Kupujący</option>
              <option value="seller">Sprzedawca</option>
            </select>
          </div>
          <button type="submit" class="w-full btn btn-primary">Zarejestruj się</button>
        </form>
        <p class="mt-4 text-center text-sm text-ink-2">Masz już konto? <a href="/login" class="text-accent hover:underline">Zaloguj się</a></p>
      </div>
    </div>`
	case "/privacy-policy":
		title = "Polityka prywatności — wszyst.pl"
		desc = "Polityka prywatności wszyst.pl — jak przetwarzamy Twoje dane osobowe."
		body = `<div class="max-w-app mx-auto px-4 py-8">
      <h1 class="text-2xl font-bold mb-6">Polityka prywatności</h1>
      <p class="text-ink-2 mb-6">Szanujemy Twoją prywatność i zobowiązujemy się do ochrony Twoich danych osobowych.</p>
      <section class="mb-6">
        <h2 class="text-lg font-semibold mb-2">Zbierane dane</h2>
        <p class="text-ink-2">Możemy zbierać imię, email, telefon, adres dostawy oraz dane o zamówieniach.</p>
      </section>
      <section class="mb-6">
        <h2 class="text-lg font-semibold mb-2">Cookies</h2>
        <p class="text-ink-2">Używamy cookies do poprawy funkcjonalności strony i analityki.</p>
      </section>
      <section class="mb-6">
        <h2 class="text-lg font-semibold mb-2">Używanie danych</h2>
        <p class="text-ink-2">Twoje dane są używane do realizacji zamówień, komunikacji z Tobą i poprawy naszych usług.</p>
      </section>
      <section class="mb-6">
        <h2 class="text-lg font-semibold mb-2">Twoje prawa</h2>
        <p class="text-ink-2">Możesz żądać dostępu, korekty lub usunięcia swoich danych.</p>
      </section>
      <section class="mb-6">
        <h2 class="text-lg font-semibold mb-2">Kontakt</h2>
        <p class="text-ink-2">W sprawach prywatności skontaktuj się z nami: privacy@wszyst.pl</p>
      </section>
    </div>`
	default:
		return "", "", ""
	}
	return title, desc, body
}

// enhanceStaticRouteHTML enriches the SPA shell for a client-side route with a
// real <title>, meta description, canonical link and server-rendered body
// content so non-JS renderers see a non-blank page. Vue replaces #app on mount.
func enhanceStaticRouteHTML(content []byte, siteURL string, path string) []byte {
	title, desc, body := staticRouteContent(path)
	if title == "" {
		return content
	}

	s := string(content)

	// Language: Polish storefront.
	s = strings.Replace(s, `<html lang="en">`, `<html lang="pl">`, 1)

	// Replace <title>...</title>.
	if i := strings.Index(s, "<title>"); i >= 0 {
		if j := strings.Index(s[i:], "</title>"); j >= 0 {
			s = s[:i] + "<title>" + title + "</title>" + s[i+j+len("</title>"):]
		}
	}

	// Replace description meta content.
	const descTag = `<meta name="description" content="`
	if i := strings.Index(s, descTag); i >= 0 {
		start := i + len(descTag)
		if j := strings.Index(s[start:], `" />`); j >= 0 {
			s = s[:start] + desc + s[start+j:]
		}
	}

	// Inject canonical link.
	s = strings.Replace(s, "</head>", `<link rel="canonical" href="`+siteURL+path+`" />`+"\n  </head>", 1)

	// Replace the empty #app shell with server-rendered content.
	s = strings.Replace(s, `<div id="app"></div>`, `<div id="app">`+body+`</div>`, 1)

	return []byte(s)
}

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
