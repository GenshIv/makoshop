package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestCleanupStaleProductsTxImportKeys verifies the importKeys contract of
// CleanupStaleProductsTx: the caller passes a plain string slice of
// ProductEANKey values (unsorted, duplicates allowed — the function sorts and
// dedupes in place, membership is a binary search), and only products whose
// key is in the slice survive the cleanup.
func TestCleanupStaleProductsTxImportKeys(t *testing.T) {
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

	repo := NewProductRepo(store, nil, nil, nil)

	const companyID = int64(7)
	seed := []*model.Product{
		{EAN: "100", Name: "Keep Me", CompanyID: companyID, Price: 10, Currency: "PLN", Status: model.ProductStatusActive},
		{EAN: "200", Name: "Keep Me Too", CompanyID: companyID, Price: 20, Currency: "PLN", Status: model.ProductStatusActive},
		{EAN: "300", Name: "Stale Product", CompanyID: companyID, Price: 30, Currency: "PLN", Status: model.ProductStatusActive},
		// Another company's product: never touched by this company's cleanup.
		{EAN: "400", Name: "Other Company", CompanyID: 99, Price: 40, Currency: "PLN", Status: model.ProductStatusActive},
	}
	for _, p := range seed {
		if err := repo.Create(p); err != nil {
			t.Fatalf("seed product %q: %v", p.Name, err)
		}
	}

	// Seed the company price document like the importers do: cleanup uses it
	// as the source of truth for the company's existing products.
	seedDoc := &CompanyPricesDoc{Prices: make(map[int64]float64)}
	for _, p := range seed {
		if p.CompanyID == companyID {
			seedDoc.Prices[p.ID] = p.Price
		}
	}
	if err := repo.SaveCompanyPrices(companyID, seedDoc); err != nil {
		t.Fatalf("save company prices: %v", err)
	}

	priceDoc, err := repo.LoadCompanyPrices(companyID)
	if err != nil {
		t.Fatalf("load company prices: %v", err)
	}
	if priceDoc == nil {
		priceDoc = &CompanyPricesDoc{Prices: make(map[int64]float64)}
	}

	// Import keys as the importer appends them per batch: raw names here
	// (identity normalization, like the JSON import). "Stale Product" is not
	// in the list; duplicates and unsorted order must be handled.
	importKeys := []string{
		ProductEANKey("200", "Keep Me Too", companyID),
		ProductEANKey("100", "Keep Me", companyID),
		ProductEANKey("100", "Keep Me", companyID), // duplicate
	}

	txn := NewTransaction(store)
	if err := txn.Begin(); err != nil {
		t.Fatalf("begin txn: %v", err)
	}
	deleted := repo.CleanupStaleProductsTx(txn, companyID, importKeys, func(s string) string { return s }, priceDoc)
	if err := txn.Commit(); err != nil {
		t.Fatalf("commit txn: %v", err)
	}

	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the stale product)", deleted)
	}
	if _, err := repo.Get(seed[2].ID); err == nil {
		t.Error("stale product still exists after cleanup")
	}
	if _, err := repo.Get(seed[0].ID); err != nil {
		t.Errorf("kept product missing after cleanup: %v", err)
	}
	if _, err := repo.Get(seed[3].ID); err != nil {
		t.Errorf("other company's product must not be deleted: %v", err)
	}
	if _, ok := priceDoc.Prices[seed[2].ID]; ok {
		t.Error("stale product's price not removed from priceDoc")
	}
}
