package db

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestEANPageCatalogMinPriceFloor verifies the permanent catalog filter:
// plain catalog browsing must not list pages whose min_price is below
// EANPageMinCatalogPrice (no offers or unusable offers), while search and the
// IncludeNoOffer override keep them visible.
func TestEANPageCatalogMinPriceFloor(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	categoryRepo := NewCategoryRepo(store)
	repo := NewEANPageRepo(store)
	repo.CategoryRepo = categoryRepo
	search := NewEANPageSearch(store.DB(), repo, nil, categoryRepo, true)

	if err := categoryRepo.Create(&model.Category{ID: 1, NameEn: "root", Slug: "root", IsActive: true}); err != nil {
		t.Fatalf("create category: %v", err)
	}

	pages := []*model.EANPage{
		// Priced normally — must be visible everywhere.
		{EAN: "111", Slug: "lampa-alfa", Title: "Lampa Alfa", CategoryID: 1, Currency: "PLN",
			MinPrice: 100, ProductCount: 2, IsActive: true},
		// Has offers but their price is below the floor — catalog must skip it.
		{EAN: "222", Slug: "lampa-beta", Title: "Lampa Beta", CategoryID: 1, Currency: "PLN",
			MinPrice: 0.005, ProductCount: 3, IsActive: true},
		// No offers at all — catalog must skip it too.
		{EAN: "333", Slug: "lampa-gama", Title: "Lampa Gama", CategoryID: 1, Currency: "PLN",
			MinPrice: 0, ProductCount: 0, IsActive: true},
	}
	for _, p := range pages {
		if err := repo.Create(p); err != nil {
			t.Fatalf("create page %s: %v", p.EAN, err)
		}
	}
	if err := search.IndexEANPageBatch(pages); err != nil {
		t.Fatalf("index pages: %v", err)
	}
	if err := search.BuildSortIndexes(); err != nil {
		t.Fatalf("build sort indexes: %v", err)
	}
	if err := store.DB().Sync(); err != nil {
		t.Fatalf("sync store: %v", err)
	}

	eansOf := func(t *testing.T, params EANPageListParams) []string {
		t.Helper()
		params.Page = 1
		params.Limit = 10
		res, err := search.ListWithTurbo(params)
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

	// Catalog browsing (default price_asc, created_at, price range): only the
	// priced page is listed.
	if got := eansOf(t, EANPageListParams{}); len(got) != 1 || got[0] != "111" {
		t.Fatalf("catalog default: got %v, want [111]", got)
	}
	if got := eansOf(t, EANPageListParams{Sort: "created_at"}); len(got) != 1 || got[0] != "111" {
		t.Fatalf("catalog created_at: got %v, want [111]", got)
	}
	if got := eansOf(t, EANPageListParams{Sort: "price_desc"}); len(got) != 1 || got[0] != "111" {
		t.Fatalf("catalog price_desc: got %v, want [111]", got)
	}
	if got := eansOf(t, EANPageListParams{PriceMax: 500}); len(got) != 1 || got[0] != "111" {
		t.Fatalf("catalog price range: got %v, want [111]", got)
	}

	// Search by name: all three pages must stay findable.
	if got := eansOf(t, EANPageListParams{Q: "lampa"}); len(got) != 3 {
		t.Fatalf("search by name: got %v, want all 3 pages", got)
	}
	// Admin override: the full set is listed without the floor.
	if got := eansOf(t, EANPageListParams{IncludeNoOffer: true}); len(got) != 3 {
		t.Fatalf("admin listing: got %v, want all 3 pages", got)
	}

	// Home sections (random picks) must not surface zero-price pages either.
	items, total, err := search.RandomByCategory(1, 12)
	if err != nil {
		t.Fatalf("RandomByCategory: %v", err)
	}
	if total != 1 {
		t.Fatalf("RandomByCategory total = %d, want 1", total)
	}
	for _, raw := range items {
		var doc model.EANPage
		_ = json.Unmarshal(raw, &doc)
		if doc.MinPrice < EANPageMinCatalogPrice {
			t.Fatalf("RandomByCategory returned zero-price page %s", doc.EAN)
		}
	}
}
