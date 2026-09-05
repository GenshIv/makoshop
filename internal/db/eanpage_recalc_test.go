package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestRecalculateCountsAndMinPricesForPages verifies the combined recalc:
// counts come from the per-EAN index documents and min prices from the
// productID -> price map. Pages WITH an index document never need product
// documents; a page WITHOUT one is backfilled from product documents once.
func TestRecalculateCountsAndMinPricesForPages(t *testing.T) {
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

	repo := NewEANPageRepo(store)

	// Page 1: stale count and price -> must be fixed via its EAN doc.
	if err := repo.Create(&model.EANPage{
		EAN: "111", Slug: "page-111", Title: "P1", Currency: "PLN",
		MinPrice: 999, ProductCount: 7, IsActive: true,
	}); err != nil {
		t.Fatalf("create page 1: %v", err)
	}
	// Page 2: already correct -> must stay untouched.
	if err := repo.Create(&model.EANPage{
		EAN: "222", Slug: "page-222", Title: "P2", Currency: "PLN",
		MinPrice: 50, ProductCount: 1, IsActive: true,
	}); err != nil {
		t.Fatalf("create page 2: %v", err)
	}
	// Page 3: no EAN doc -> backfill path via product documents.
	if err := repo.Create(&model.EANPage{
		EAN: "333", Slug: "page-333", Title: "P3", Currency: "PLN",
		MinPrice: 0, ProductCount: 0, IsActive: true,
	}); err != nil {
		t.Fatalf("create page 3: %v", err)
	}

	// EAN index documents (no product documents needed for pages 1 and 2).
	if err := SaveEANIndexDoc(nil, store, "111", &EANIndexDoc{ProductIDs: []int64{101, 102}}); err != nil {
		t.Fatalf("seed ean doc 111: %v", err)
	}
	if err := SaveEANIndexDoc(nil, store, "222", &EANIndexDoc{ProductIDs: []int64{101}}); err != nil {
		t.Fatalf("seed ean doc 222: %v", err)
	}

	// Backfill source for page 3: a real product document + ean index.
	p3 := model.Product{ID: 201, EAN: "333", Name: "P3 product", Price: 30, Currency: "PLN"}
	if err := store.DocPut(KeyProduct(201), MarshalProduct(p3)); err != nil {
		t.Fatalf("seed product doc: %v", err)
	}
	if _, err := store.DB().TurboPutIndexString("ean:333", KeyProduct(201)); err != nil {
		t.Fatalf("seed ean index: %v", err)
	}

	if err := store.DB().Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	prices := map[int64]float64{101: 50, 102: 70, 201: 30}

	if err := repo.RecalculateCountsAndMinPricesForPages([]int64{1, 2, 3}, prices); err != nil {
		t.Fatalf("recalc: %v", err)
	}

	got1, err := repo.Get(1)
	if err != nil {
		t.Fatalf("get page 1: %v", err)
	}
	if got1.ProductCount != 2 || got1.MinPrice != 50 {
		t.Fatalf("page 1 = count %d / min %v, want 2 / 50 (cheapest of 50/70)", got1.ProductCount, got1.MinPrice)
	}

	got2, err := repo.Get(2)
	if err != nil {
		t.Fatalf("get page 2: %v", err)
	}
	if got2.ProductCount != 1 || got2.MinPrice != 50 {
		t.Fatalf("page 2 drifted: count=%d min=%v, want 1/50", got2.ProductCount, got2.MinPrice)
	}

	got3, err := repo.Get(3)
	if err != nil {
		t.Fatalf("get page 3: %v", err)
	}
	if got3.ProductCount != 1 || got3.MinPrice != 30 {
		t.Fatalf("page 3 backfill = count %d / min %v, want 1 / 30", got3.ProductCount, got3.MinPrice)
	}

	// Missing price entries: min price must be left unchanged (not zeroed).
	if err := repo.RecalculateCountsAndMinPricesForPages([]int64{2}, map[int64]float64{}); err != nil {
		t.Fatalf("recalc without prices: %v", err)
	}
	got2b, _ := repo.Get(2)
	if got2b.MinPrice != 50 {
		t.Fatalf("page 2 min price = %v, want unchanged 50", got2b.MinPrice)
	}

	// Backfill persisted: the doc for 333 now exists.
	doc, err := LoadEANIndexDoc(nil, store, "333")
	if err != nil || doc == nil || len(doc.ProductIDs) != 1 || doc.ProductIDs[0] != 201 {
		t.Fatalf("backfilled ean doc 333 = %+v, err=%v", doc, err)
	}
}
