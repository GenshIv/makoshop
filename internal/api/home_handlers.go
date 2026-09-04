package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/silentjson/v2"
)

// Home offers: random product sections grouped by root category for the
// storefront home page.

// homeOffersCacheTTL caps how often the random payload is rebuilt. The
// homepage is the hottest page, so a short micro-cache absorbs request bursts
// while the selection still rotates on a human timescale (?fresh=1 bypasses it).
const homeOffersCacheTTL = 60 * time.Second

const (
	homeOffersDefaultSections   = 8
	homeOffersMaxSections       = 16
	homeOffersDefaultPerSection = 12
	homeOffersMaxPerSection     = 20
)

var (
	homeOffersMu       sync.Mutex
	homeOffersPayload  []byte
	homeOffersCachedAt time.Time
)

// HomeOfferCategory is the category header of a home offers section.
type HomeOfferCategory struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	URL           string `json:"url"`
	NameRu        string `json:"name_ru,omitempty"`
	NameUa        string `json:"name_ua,omitempty"`
	NamePl        string `json:"name_pl,omitempty"`
	NameEn        string `json:"name_en,omitempty"`
	ImageLightURL string `json:"image_light_url,omitempty"`
	ImageDarkURL  string `json:"image_dark_url,omitempty"`
}

// HomeOfferSection is one home page carousel: a category plus random items.
type HomeOfferSection struct {
	Category HomeOfferCategory       `json:"category"`
	Items    []silentjson.RawMessage `json:"items"`
	Total    int                     `json:"total"` // pages in category subtree
}

// homeOffersResp is the wire shape of GET /home/offers.
type homeOffersResp struct {
	Sections    []HomeOfferSection `json:"sections"`
	GeneratedAt int64              `json:"generated_at"`
}

// Items are raw EANPage JSON, so the response must go through silentjson
// (encoding/json would base64-encode RawMessage instead of embedding it).
var homeOffersRespReg = silentjson.BuildRegistry(reflect.TypeOf(homeOffersResp{}))

// HandleHomeOffers serves GET /home/offers.
func (h *Handlers) HandleHomeOffers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	headOnly := r.Method == http.MethodHead
	fresh := r.URL.Query().Get("fresh") == "1"

	homeOffersMu.Lock()
	defer homeOffersMu.Unlock()

	if !fresh && homeOffersPayload != nil && time.Since(homeOffersCachedAt) < homeOffersCacheTTL {
		writeHomeOffers(w, headOnly, homeOffersPayload)
		return
	}

	sectionsLimit := parseHomeOffersInt(r.URL.Query().Get("sections"), homeOffersDefaultSections, homeOffersMaxSections)
	perSection := parseHomeOffersInt(r.URL.Query().Get("per_section"), homeOffersDefaultPerSection, homeOffersMaxPerSection)

	// Admin-managed overrides from global_settings: an ordered category list
	// (order = display order) and/or the carousel size.
	homeOffersIDs, homeOffersPerSection := h.loadHomeOffersSettings()
	if homeOffersPerSection > 0 {
		perSection = homeOffersPerSection
	}

	tree, err := h.categoryRepo.GetTree()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	roots := tree
	if len(homeOffersIDs) > 0 {
		byID := make(map[int64]db.CategoryTreeNode, len(tree))
		for _, node := range tree {
			byID[node.ID] = node
		}
		roots = roots[:0]
		for _, id := range homeOffersIDs {
			if node, ok := byID[id]; ok {
				roots = append(roots, node)
			}
		}
		if len(roots) > homeOffersMaxSections {
			roots = roots[:homeOffersMaxSections]
		}
	} else if len(roots) > sectionsLimit {
		roots = roots[:sectionsLimit]
	}

	payload, err := h.buildHomeOffers(roots, perSection)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	homeOffersPayload = payload
	homeOffersCachedAt = time.Now()
	writeHomeOffers(w, headOnly, payload)
}

// loadHomeOffersSettings reads the admin-managed home offers config from the
// global_settings doc. Zero values mean "not configured" (defaults apply).
func (h *Handlers) loadHomeOffersSettings() ([]int64, int) {
	val, err := h.Store().DocGet("global_settings")
	if err != nil || len(val) == 0 {
		return nil, 0
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(val, &settings); err != nil {
		return nil, 0
	}
	raw, ok := settings["home_offers"].(map[string]interface{})
	if !ok {
		return nil, 0
	}
	ids, perSection, err := normalizeHomeOffers(raw)
	if err != nil {
		return nil, 0
	}
	return ids, perSection
}

// buildHomeOffers assembles random sections for the given root categories.
// Section order follows the input order (admin-configured or sort_order);
// sections without EAN pages are dropped. Only the items inside a section
// are randomized.
func (h *Handlers) buildHomeOffers(roots []db.CategoryTreeNode, perSection int) ([]byte, error) {
	sections := make([]HomeOfferSection, 0, len(roots))
	for _, node := range roots {
		if h.eanPageSearch == nil {
			break
		}
		items, total, err := h.eanPageSearch.RandomByCategory(node.ID, perSection)
		if err != nil {
			continue
		}
		if len(items) == 0 {
			continue
		}
		sections = append(sections, HomeOfferSection{
			Category: HomeOfferCategory{
				ID:            node.ID,
				Slug:          node.Slug,
				URL:           "/shop/" + node.Slug,
				NameRu:        node.NameRu,
				NameUa:        node.NameUa,
				NamePl:        node.NamePl,
				NameEn:        node.NameEn,
				ImageLightURL: node.ImageLightURL,
				ImageDarkURL:  node.ImageDarkURL,
			},
			Items: items,
			Total: total,
		})
	}

	// Pseudo-section: random products from the whole catalog (global catID=0
	// index holds every EAN page, categorized or not). Appended last.
	if h.eanPageSearch != nil {
		if items, total, err := h.eanPageSearch.RandomByCategory(0, perSection); err == nil && len(items) > 0 {
			sections = append(sections, HomeOfferSection{
				Category: HomeOfferCategory{
					Slug:   "",
					URL:    "/shop",
					NameRu: "Случайные товары",
					NameUa: "Випадкові товари",
					NamePl: "Losowe produkty",
					NameEn: "Random products",
				},
				Items: items,
				Total: total,
			})
		}
	}

	resp := homeOffersResp{
		Sections:    sections,
		GeneratedAt: time.Now().UnixMilli(),
	}
	return silentjson.Marshal(&resp, homeOffersRespReg, nil), nil
}

func writeHomeOffers(w http.ResponseWriter, headOnly bool, payload []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.WriteHeader(http.StatusOK)
	if !headOnly {
		_, _ = w.Write(payload)
	}
}

func parseHomeOffersInt(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
		if n > max {
			return max
		}
	}
	if n < 1 {
		return def
	}
	return n
}
