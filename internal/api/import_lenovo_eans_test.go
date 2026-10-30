package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	makodb "github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestHandleAdminImportLenovoEANs verifies the zero-offer contract of
// POST /admin/import-lenovo-eans end-to-end on a temp store:
//   - every catalog variant becomes an EAN page with MinPrice=0 and
//     ProductCount=0 (no offers attached);
//   - rows without a valid EAN (and models without an EAN column) create
//     nothing;
//   - such pages never surface in category catalog listings (all sorts,
//     price ranges, home random picks) — they sit below the catalog
//     min-price floor in the per-category numSort index;
//   - they stay reachable via search, admin listing (IncludeNoOffer) and
//     direct slug lookup;
//   - a re-import upserts instead of duplicating, and never touches
//     pre-existing pages that have real offers.
func TestHandleAdminImportLenovoEANs(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}
	store, err := db.NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	// The import triggers shard compaction; after a compaction ShardedDB.Close
	// can spin forever waiting for a leaked shard refCount (makodb quirk,
	// harmless for the server which never closes the store). Guard the close
	// so the test cannot hang on teardown.
	defer func() {
		done := make(chan struct{})
		go func() {
			_ = store.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Log("WARN: store.Close() timed out after compaction; skipping")
		}
	}()

	h := NewHandlers(store)

	// Categories used by getLenovoCategoryID: 20 = Laptopy, 21 = Tablety.
	for _, cat := range []model.Category{
		{ID: 20, NameEn: "Laptops", NameRu: "Ноутбуки", Slug: "laptops", IsActive: true},
		{ID: 21, NameEn: "Tablets", NameRu: "Планшеты", Slug: "tablets", IsActive: true},
	} {
		if err := h.categoryRepo.Create(&cat); err != nil {
			t.Fatalf("create category %d: %v", cat.ID, err)
		}
	}

	// A pre-existing page with real offers in the same category: the import
	// must not touch it, and catalog listings must keep showing only it.
	const pricedEAN = "5900000000001"
	if err := h.eanPageRepo.Create(&model.EANPage{
		EAN: pricedEAN, Slug: "priced-laptop", Title: "Priced Laptop", CategoryID: 20,
		Currency: "PLN", MinPrice: 100, ProductCount: 2, IsActive: true,
	}); err != nil {
		t.Fatalf("seed priced page: %v", err)
	}

	// Minimal PSREF-like catalog: detailedSpecs.rows/cols drive the import.
	catalog := map[string]any{
		"metadata": map[string]any{"source": "test"},
		"models": []map[string]any{
			{
				"category":        "Notebooks",
				"product_name":    "ThinkPad T14 Gen 5",
				"photos":          []map[string]any{{"src": "https://img.example/t14.png"}},
				"highlight_array": []string{"Military-grade durability"},
				"detailedSpecs": map[string]any{
					"cols": []string{"Model", "EAN/UCC-13", "Processor", "Memory", "Storage"},
					"rows": []any{
						[]any{"21KB", "0196864123456", "Intel Core Ultra 7 165H, 16MB", "16GB LPDDR5x", "512GB SSD"},
						[]any{"21KC", "0196864654321", "Intel Core Ultra 5 125U, 12MB", "32GB LPDDR5x", "1TB SSD"},
						[]any{"21KD", "12345", "Intel Core Ultra 7 165H, 16MB", "16GB LPDDR5x", "512GB SSD"}, // EAN too short → skipped
					},
				},
			},
			{
				"category":     "Tablets",
				"product_name": "IdeaTab Pro",
				"photos":       []map[string]any{{"src": "https://img.example/ideatab.png"}},
				"detailedSpecs": map[string]any{
					"cols": []string{"Model", "EAN", "Processor", "Memory"},
					"rows": []any{
						[]any{"TB370FU", "0198765432109", "MediaTek Kompanio 1300T", "8GB"},
					},
				},
			},
			{
				// No EAN column at all → the whole model is skipped.
				"category":     "Notebooks",
				"product_name": "ThinkBook 14 G7",
				"detailedSpecs": map[string]any{
					"cols": []string{"Model", "Processor"},
					"rows": []any{
						[]any{"G7-IRM", "Intel Core i5-1335U"},
					},
				},
			},
		},
	}
	catalogPath := filepath.Join(tmpDir, "lenovo-psref-catalog.json")
	raw, _ := json.Marshal(catalog)
	if err := os.WriteFile(catalogPath, raw, 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	postImport := func() {
		t.Helper()
		var rec *httptest.ResponseRecorder
		// The deferred shard compaction after a finished import briefly holds
		// the import lock — retry on 409 until it drains.
		deadline := time.Now().Add(30 * time.Second)
		for {
			req := httptest.NewRequest(http.MethodPost, "/admin/import-lenovo-eans?file="+catalogPath, nil)
			rec = httptest.NewRecorder()
			h.HandleAdminImportLenovo(rec, req)
			if rec.Code == http.StatusAccepted {
				break
			}
			if rec.Code != http.StatusConflict || time.Now().After(deadline) {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			time.Sleep(50 * time.Millisecond)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != "started" {
			t.Fatalf("unexpected response body: %s", rec.Body.String())
		}
		// The import runs in a background goroutine; wait for completion.
		waitDeadline := time.Now().Add(60 * time.Second)
		for {
			snap := h.importProgress.Snapshot()
			if snap.Status == "failed" {
				t.Fatalf("import failed")
			}
			if snap.Status == "completed" {
				return
			}
			if time.Now().After(waitDeadline) {
				t.Fatalf("import did not finish in time, status = %s", snap.Status)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// GET must be rejected before anything runs.
	rec := httptest.NewRecorder()
	h.HandleAdminImportLenovo(rec, httptest.NewRequest(http.MethodGet, "/admin/import-lenovo-eans", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}

	postImport()
	if err := store.DB().Sync(); err != nil {
		t.Fatalf("sync store: %v", err)
	}

	newCat20 := []string{"0196864123456", "0196864654321"}
	newCat21 := []string{"0198765432109"}

	// --- Created pages carry no offers: zero price, zero product count. ---
	for _, ean := range append(append([]string{}, newCat20...), newCat21...) {
		p, err := h.eanPageRepo.Get(ean)
		if err != nil || p == nil {
			t.Fatalf("page %s not created: %v", ean, err)
		}
		if p.MinPrice != 0 || p.ProductCount != 0 {
			t.Fatalf("page %s = min_price %v / product_count %d, want 0/0", ean, p.MinPrice, p.ProductCount)
		}
		if !p.IsActive || p.Brand != "Lenovo" || p.Currency != "PLN" {
			t.Fatalf("page %s wrongly populated: %+v", ean, p)
		}
		if p.Slug == "" || p.Title == "" || p.Description == "" || len(p.Images) == 0 {
			t.Fatalf("page %s missing content: %+v", ean, p)
		}
	}
	if p, _ := h.eanPageRepo.Get("0196864123456"); p.CategoryID != 20 {
		t.Fatalf("laptop page category = %d, want 20", p.CategoryID)
	}
	if p, _ := h.eanPageRepo.Get("0198765432109"); p.CategoryID != 21 {
		t.Fatalf("tablet page category = %d, want 21", p.CategoryID)
	}

	// Rows/models without a usable EAN create nothing.
	for _, ean := range []string{"12345", "0190000000001"} {
		if p, err := h.eanPageRepo.Get(ean); err == nil && p != nil {
			t.Fatalf("page %s must not be created", ean)
		}
	}

	// Direct URL lookup works (slug index written by the import).
	if p, _ := h.eanPageRepo.GetBySlug("x"); p != nil {
		t.Fatalf("unexpected slug hit")
	}
	created, _ := h.eanPageRepo.Get("0196864123456")
	if p, err := h.eanPageRepo.GetBySlug(created.Slug); err != nil || p == nil || p.EAN != created.EAN {
		t.Fatalf("slug lookup for %q failed: %v", created.Slug, err)
	}

	// --- Category catalog listings must not surface no-offer pages. ---
	eansOf := func(t *testing.T, params db.EANPageListParams) []string {
		t.Helper()
		params.Page = 1
		params.Limit = 50
		res, err := h.eanPageSearch.ListWithTurbo(params)
		if err != nil {
			t.Fatalf("ListWithTurbo %+v: %v", params, err)
		}
		eans := make([]string, 0, len(res.Items))
		for _, raw := range res.Items {
			var doc model.EANPage
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("doc is not valid EANPage JSON: %v", err)
			}
			eans = append(eans, doc.EAN)
		}
		if int64(len(eans)) != res.Total {
			t.Fatalf("items %d != total %d for %+v", len(eans), res.Total, params)
		}
		return eans
	}

	for _, params := range []db.EANPageListParams{
		{CategoryID: 20},                     // default price_asc
		{CategoryID: 20, Sort: "price_desc"}, //
		{CategoryID: 20, Sort: "created_at"}, //
		{CategoryID: 20, PriceMax: 500},      // price range filter
	} {
		got := eansOf(t, params)
		if len(got) != 1 || got[0] != pricedEAN {
			t.Fatalf("catalog cat20 %+v: got %v, want [%s]", params, got, pricedEAN)
		}
	}
	if got := eansOf(t, db.EANPageListParams{CategoryID: 21}); len(got) != 0 {
		t.Fatalf("catalog cat21 (only no-offer pages): got %v, want empty", got)
	}

	// Home random picks read the same floored numSort range.
	items, total, err := h.eanPageSearch.RandomByCategory(20, 12)
	if err != nil {
		t.Fatalf("RandomByCategory: %v", err)
	}
	if total != 1 {
		t.Fatalf("RandomByCategory total = %d, want 1 (priced only)", total)
	}
	for _, raw := range items {
		var doc model.EANPage
		_ = json.Unmarshal(raw, &doc)
		if doc.MinPrice < db.EANPageMinCatalogPrice {
			t.Fatalf("RandomByCategory returned zero-price page %s", doc.EAN)
		}
	}

	// Directly: in the per-category numSort price index the no-offer pages sit
	// at value 0 — the catalog floor range (>= 1 cent) must not contain them,
	// while the full range does.
	floorRes, err := store.DB().TurboGetNumSortRangeWithDocs(makodb.TurboGetNumSortRangeWithDocsParams{
		Name: "eanpage_price:20", MinValue: 1, MaxValue: ^uint64(0), DocPrefix: "eanpage:", PageSize: 50,
	})
	if err != nil {
		t.Fatalf("numSort floor range: %v", err)
	}
	if floorRes.Total != 1 {
		t.Fatalf("numSort floor range total = %d, want 1", floorRes.Total)
	}
	fullRes, err := store.DB().TurboGetNumSortRangeWithDocs(makodb.TurboGetNumSortRangeWithDocsParams{
		Name: "eanpage_price:20", MinValue: 0, MaxValue: ^uint64(0), DocPrefix: "eanpage:", PageSize: 50,
	})
	if err != nil {
		t.Fatalf("numSort full range: %v", err)
	}
	if fullRes.Total != 3 {
		t.Fatalf("numSort full range total = %d, want 3 (floor page included)", fullRes.Total)
	}

	// --- Still reachable: search and the admin override see everything. ---
	if got := eansOf(t, db.EANPageListParams{Q: "thinkpad"}); len(got) != 2 {
		t.Fatalf("search 'thinkpad': got %v, want both no-offer thinkpad pages", got)
	}
	if got := eansOf(t, db.EANPageListParams{CategoryID: 20, IncludeNoOffer: true}); len(got) != 3 {
		t.Fatalf("admin listing cat20: got %v, want 3 pages", got)
	}
	if got := eansOf(t, db.EANPageListParams{CategoryID: 21, IncludeNoOffer: true}); len(got) != 1 {
		t.Fatalf("admin listing cat21: got %v, want 1 page", got)
	}

	// --- Re-import upserts: no duplicates, priced page untouched. ---
	postImport()
	if err := store.DB().Sync(); err != nil {
		t.Fatalf("sync store after re-import: %v", err)
	}
	if got := eansOf(t, db.EANPageListParams{CategoryID: 20, IncludeNoOffer: true}); len(got) != 3 {
		t.Fatalf("after re-import admin listing cat20: got %v (%d), want 3 pages", got, len(got))
	}
	priced, err := h.eanPageRepo.Get(pricedEAN)
	if err != nil {
		t.Fatalf("get priced page after re-import: %v", err)
	}
	if priced.MinPrice != 100 || priced.ProductCount != 2 || priced.Title != "Priced Laptop" {
		t.Fatalf("re-import modified the priced page: %+v", priced)
	}
	again, err := h.eanPageRepo.Get("0196864123456")
	if err != nil || again == nil {
		t.Fatalf("get lenovo page after re-import: %v", err)
	}
	if again.ProductCount != 0 || again.MinPrice != 0 {
		t.Fatalf("re-import attached offers to lenovo page: %+v", again)
	}

	fmt.Println("lenovo eans import contract verified")
}
