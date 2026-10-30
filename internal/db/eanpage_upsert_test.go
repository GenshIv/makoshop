package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestBatchUpsertFromProductsTxGroups verifies the grouped (map-free) upsert:
// all products sharing a page key are merged into ONE page — counts, min
// price, attributes and keywords — and each page is written once. This covers
// the merge bug of the old map-based version, where newly created pages
// absorbed only their first product.
func TestBatchUpsertFromProductsTxGroups(t *testing.T) {
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

	txn := NewTransaction(store)
	if err := txn.Begin(); err != nil {
		t.Fatalf("begin txn: %v", err)
	}

	products := []*model.Product{
		// Three products on one NEW page (same EAN), in shuffled order — all
		// must be merged, not just the first.
		{EAN: "777", Name: "Widget B", CompanyID: 1, Price: 30, Currency: "PLN",
			Attributes: []model.KeyValue{{Key: "color", Value: "blue"}}},
		{EAN: "777", Name: "Widget A", CompanyID: 2, Price: 10, Currency: "PLN",
			Attributes: []model.KeyValue{{Key: "color", Value: "red"}}},
		{EAN: "777", Name: "Widget C", CompanyID: 3, Price: 20, Currency: "PLN",
			Attributes: []model.KeyValue{{Key: "size", Value: "large"}}},
		// One product on another new page.
		{EAN: "888", Name: "Solo Widget", CompanyID: 1, Price: 5, Currency: "PLN"},
	}

	affected := repo.BatchUpsertFromProductsTx(txn, products, nil)
	if len(affected) != 2 {
		t.Fatalf("affected pages = %d, want 2", len(affected))
	}
	if err := txn.Commit(); err != nil {
		t.Fatalf("commit txn: %v", err)
	}

	page, err := repo.GetByEAN("777")
	if err != nil {
		t.Fatalf("get merged page: %v", err)
	}
	if page.ProductCount != 3 {
		t.Errorf("ProductCount = %d, want 3 (ALL group products merged)", page.ProductCount)
	}
	if page.MinPrice != 10 {
		t.Errorf("MinPrice = %v, want 10", page.MinPrice)
	}
	attrValues := map[string]bool{}
	for _, kv := range page.Attributes {
		attrValues[kv.Value] = true
	}
	for _, want := range []string{"red", "blue", "large"} {
		if !attrValues[want] {
			t.Errorf("attribute value %q missing from merged page (attrs: %v)", want, page.Attributes)
		}
	}

	// Second import batch touching the same page must load it once, merge on
	// top and keep previous state (count grows, min price keeps the minimum).
	txn2 := NewTransaction(store)
	if err := txn2.Begin(); err != nil {
		t.Fatalf("begin txn2: %v", err)
	}
	more := []*model.Product{
		{EAN: "777", Name: "Widget D", CompanyID: 4, Price: 7, Currency: "PLN"},
	}
	if affected := repo.BatchUpsertFromProductsTx(txn2, more, nil); len(affected) != 1 {
		t.Fatalf("second upsert affected = %d, want 1", len(affected))
	}
	if err := txn2.Commit(); err != nil {
		t.Fatalf("commit txn2: %v", err)
	}

	page, err = repo.GetByEAN("777")
	if err != nil {
		t.Fatalf("re-get merged page: %v", err)
	}
	if page.ProductCount != 4 {
		t.Errorf("ProductCount after second batch = %d, want 4", page.ProductCount)
	}
	if page.MinPrice != 7 {
		t.Errorf("MinPrice after second batch = %v, want 7", page.MinPrice)
	}

	// New pages must be listed in the eanpage_list index.
	tokens, err := store.db.TurboGetIndexTokens(TurboKeyEANPageList)
	if err != nil {
		t.Fatalf("get eanpage_list tokens: %v", err)
	}
	if len(tokens) < 2 {
		t.Errorf("eanpage_list tokens = %d, want >= 2 (new pages registered)", len(tokens))
	}
}
