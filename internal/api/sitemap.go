package api

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
)

const (
	sitemapSCUPageSize = 50000                   // max URLs per sitemap file (XML spec safe limit)
	sitemapBaseURL     = "http://localhost:5173" // base URL for sitemap (change in production)
)

// ---------- robots.txt ----------

// HandleRobotsTXT serves dynamic robots.txt
func (h *Handlers) HandleRobotsTXT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Build robots.txt content
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("User-agent: *\n"))
	sb.WriteString(fmt.Sprintf("Allow: /\n"))
	sb.WriteString(fmt.Sprintf("\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /admin/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /api/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /auth/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /cart/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /orders/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /companies/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /promo/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /payments/\n"))
	sb.WriteString(fmt.Sprintf("Disallow: /landing/\n"))
	sb.WriteString(fmt.Sprintf("\n"))
	sb.WriteString(fmt.Sprintf("Sitemap: %s/sitemap.xml\n", sitemapBaseURL))

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}

// ---------- sitemap.xml (index) ----------

// SitemapIndex represents the root sitemap index
type SitemapIndex struct {
	XMLName  xml.Name     `xml:"sitemapindex"`
	Xmlns    string       `xml:"xmlns,attr"`
	Sitemaps []SitemapRef `xml:"sitemap"`
}

type SitemapRef struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// HandleSitemapIndex serves sitemap index file
func (h *Handlers) HandleSitemapIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	now := time.Now().UTC().Format(time.RFC3339)

	// Count SCU pages to determine how many sitemap files we need
	scupageCount, err := h.getSCUPageCount()
	if err != nil {
		http.Error(w, "failed to count SCU pages: "+err.Error(), http.StatusInternalServerError)
		return
	}

	numSCUSitemaps := (scupageCount + sitemapSCUPageSize - 1) / sitemapSCUPageSize
	if numSCUSitemaps < 1 {
		numSCUSitemaps = 1 // always have at least one
	}

	sitemaps := make([]SitemapRef, 0, 2+numSCUSitemaps)

	// Categories sitemap
	sitemaps = append(sitemaps, SitemapRef{
		Loc:     sitemapBaseURL + "/sitemap-categories.xml",
		LastMod: now,
	})

	// SCUPage sitemaps
	for i := 0; i < numSCUSitemaps; i++ {
		sitemaps = append(sitemaps, SitemapRef{
			Loc:     fmt.Sprintf("%s/sitemap-scupage-%d.xml", sitemapBaseURL, i),
			LastMod: now,
		})
	}

	index := SitemapIndex{
		Xmlns:    "http://www.sitemaps.org/schemas/sitemap/0.9",
		Sitemaps: sitemaps,
	}

	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(index); err != nil {
		http.Error(w, "failed to encode sitemap index: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// ---------- sitemap-categories.xml ----------

// Sitemap represents a standard sitemap file
type Sitemap struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []SitemapURL `xml:"url"`
}

type SitemapURL struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

// HandleSitemapCategories serves categories sitemap
func (h *Handlers) HandleSitemapCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	categories, err := h.categoryRepo.ListAll()
	if err != nil {
		http.Error(w, "failed to list categories: "+err.Error(), http.StatusInternalServerError)
		return
	}

	urls := make([]SitemapURL, 0, len(categories))
	for _, cat := range categories {
		if !cat.IsActive {
			continue
		}

		// Build category path: /shop/{slug1}/{slug2}/...
		treePath, err := h.categoryRepo.GetTreePath(cat.ID)
		if err != nil {
			continue
		}

		loc := "/shop/" + strings.Join(treePath, "/")

		urls = append(urls, SitemapURL{
			Loc:        sitemapBaseURL + loc,
			LastMod:    time.Unix(cat.UpdatedAt, 0).UTC().Format(time.RFC3339),
			ChangeFreq: "weekly",
			Priority:   "0.7",
		})
	}

	sitemap := Sitemap{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(sitemap); err != nil {
		http.Error(w, "failed to encode sitemap: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// ---------- sitemap-scupage-{N}.xml ----------

// HandleSitemapSCUPage serves a paginated SCUPage sitemap
func (h *Handlers) HandleSitemapSCUPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract page number from path: /sitemap-scupage-{N}.xml
	path := r.URL.Path
	var pageNum int
	var err error

	// Handle /sitemap-scupage (no page number) -> default to page 0
	if path == "/sitemap-scupage" || path == "/sitemap-scupage/" {
		pageNum = 0
	} else {
		// Remove prefix /sitemap-scupage- and suffix .xml
		pageStr := strings.TrimPrefix(path, "/sitemap-scupage-")
		pageStr = strings.TrimSuffix(pageStr, ".xml")

		pageNum, err = strconv.Atoi(pageStr)
		if err != nil || pageNum < 0 {
			http.Error(w, "invalid sitemap page number", http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	// Get all SCUPage IDs from turbo index
	scupageIDs, err := h.getAllSCUPageIDs()
	if err != nil {
		http.Error(w, "failed to read SCU page index: "+err.Error(), http.StatusInternalServerError)
		return
	}

	total := len(scupageIDs)
	if total == 0 {
		// Return empty sitemap
		sitemap := Sitemap{
			Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
			URLs:  []SitemapURL{},
		}
		w.WriteHeader(http.StatusOK)
		enc := xml.NewEncoder(w)
		enc.Indent("", "  ")
		_ = enc.Encode(sitemap)
		return
	}

	// Calculate offset and limit
	offset := pageNum * sitemapSCUPageSize
	limit := sitemapSCUPageSize

	if offset >= total {
		// Page out of range, return empty sitemap
		sitemap := Sitemap{
			Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
			URLs:  []SitemapURL{},
		}
		w.WriteHeader(http.StatusOK)
		enc := xml.NewEncoder(w)
		enc.Indent("", "  ")
		_ = enc.Encode(sitemap)
		return
	}

	end := offset + limit
	if end > total {
		end = total
	}

	pageIDs := scupageIDs[offset:end]

	// Build URLs from SCUPage documents (streaming to avoid loading all into memory)
	urls := make([]SitemapURL, 0, len(pageIDs))

	for _, id := range pageIDs {
		sp, err := h.scuPageRepo.Get(int64(id))
		if err != nil || !sp.IsActive {
			continue
		}

		// Build canonical SEO URL: /shop/{breadcrumbs}/{slug}
		loc := h.buildSCUPageURL(sp)

		urls = append(urls, SitemapURL{
			Loc:        sitemapBaseURL + loc,
			LastMod:    time.Unix(sp.UpdatedAt, 0).UTC().Format(time.RFC3339),
			ChangeFreq: "daily",
			Priority:   "0.8",
		})
	}

	sitemap := Sitemap{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(sitemap); err != nil {
		http.Error(w, "failed to encode sitemap: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// ---------- helpers ----------

// getSCUPageCount returns the total number of SCU pages from turbo index
func (h *Handlers) getSCUPageCount() (int, error) {
	data, err := h.scuPageRepo.Store.DB().TurboRawRead(db.TurboKeySCUPageList)
	if err != nil || len(data) == 0 {
		return 0, nil
	}
	ids := makodb.TurboUnsafeReadTokens(data)
	return len(ids), nil
}

// getAllSCUPageIDs returns all SCUPage IDs from turbo index as sorted slice
func (h *Handlers) getAllSCUPageIDs() ([]uint64, error) {
	tokens128, err := h.scuPageRepo.Store.DB().TurboGetIndexTokens(db.TurboKeySCUPageList)
	if err != nil || len(tokens128) == 0 {
		return nil, nil
	}
	// Convert Key128 tokens to uint64 IDs
	// Assuming token[1] contains the numeric ID
	ids := make([]uint64, len(tokens128))
	for i, token := range tokens128 {
		ids[i] = token[1]
	}
	return ids, nil
}

// buildSCUPageURL builds canonical SEO URL for a SCU page
func (h *Handlers) buildSCUPageURL(sp *model.SCUPage) string {
	if sp.CategoryID != 0 {
		treePath, err := h.categoryRepo.GetTreePath(sp.CategoryID)
		if err == nil && len(treePath) > 0 {
			return "/shop/" + strings.Join(treePath, "/") + "/" + sp.Slug
		}
	}
	return "/shop/" + sp.Slug
}
