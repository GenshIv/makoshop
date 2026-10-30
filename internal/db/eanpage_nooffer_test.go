package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestEnsureEANPagesWithoutOffersTx verifies the import rule for zero-price
// offers: the EAN page is created without the product — but only when the
// page does not exist yet and the offer's description is usable. Existing
// pages are never modified.
func TestEnsureEANPagesWithoutOffersTx(t *testing.T) {
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

	// An existing page with real offers — must stay untouched.
	if err := repo.Create(&model.EANPage{
		EAN: "111", Slug: "page-111", Title: "Existing", Currency: "PLN",
		MinPrice: 42, ProductCount: 5, IsActive: true,
	}); err != nil {
		t.Fatalf("seed existing page: %v", err)
	}

	txn := NewTransaction(store)
	if err := txn.Begin(); err != nil {
		t.Fatalf("begin txn: %v", err)
	}

	offers := []*model.Product{
		// Usable: page missing → must be created without offers.
		{EAN: "222", Name: "Widget One", Description: "A perfectly normal description", Price: 0, Currency: "PLN",
			Brand: "Acme", Images: []string{"https://img/1.png"}},
		// No description → no page.
		{EAN: "333", Name: "Widget Two", Description: "", Price: 0, Currency: "PLN"},
		// Junk (numeric-only) description → no page.
		{EAN: "444", Name: "Widget Three", Description: "323282", Price: 0, Currency: "PLN"},
		// Page already exists → untouched.
		{EAN: "111", Name: "Existing Offer", Description: "Existing offer description", Price: 0, Currency: "PLN"},
		// Same key twice in one batch → one page.
		{EAN: "555", Name: "Widget Five", Description: "First variant description", Price: 0, Currency: "PLN"},
		{EAN: "555", Name: "Widget Five alt", Description: "Second variant description", Price: 0, Currency: "PLN"},
	}

	created := repo.EnsureEANPagesWithoutOffersTx(txn, offers)
	if len(created) != 2 { // 222 and 555
		t.Fatalf("created %d pages, want 2", len(created))
	}
	if err := txn.Commit(); err != nil {
		t.Fatalf("commit txn: %v", err)
	}

	gotNew, err := repo.Get("222")
	if err != nil {
		t.Fatalf("get new page: %v", err)
	}
	if gotNew.ProductCount != 0 || gotNew.MinPrice != 0 {
		t.Fatalf("new page = count %d / min %v, want 0/0 (no offers)", gotNew.ProductCount, gotNew.MinPrice)
	}
	if gotNew.Title != "Widget One" || gotNew.Description != "A perfectly normal description" || gotNew.Brand != "Acme" {
		t.Fatalf("new page data not carried over: %+v", gotNew)
	}
	if !gotNew.IsActive {
		t.Fatalf("new page must be active")
	}

	for _, ean := range []string{"333", "444"} {
		if p, err := repo.Get(ean); err == nil && p != nil {
			t.Fatalf("page %s must not be created (unusable description)", ean)
		}
	}

	gotExisting, err := repo.Get("111")
	if err != nil {
		t.Fatalf("get existing page: %v", err)
	}
	if gotExisting.ProductCount != 5 || gotExisting.MinPrice != 42 || gotExisting.Title != "Existing" {
		t.Fatalf("existing page was modified: %+v", gotExisting)
	}

	if p, err := repo.Get("555"); err != nil || p == nil {
		t.Fatalf("deduped page 555 must exist: %v", err)
	}
}

func TestOfferDescriptionUsable(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"ab", false},
		{"abc", true},
		{"323282", false},
		{" 323282 ", false},
		{"Solid description", true},
	}
	for _, tc := range cases {
		if got := offerDescriptionUsable(tc.in); got != tc.want {
			t.Errorf("offerDescriptionUsable(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
