package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestTurboProductSearchWordWithFilters verifies that /products/turbo style
// queries combine the word query with attribute and price filters.
// Regression test: the AND-intersection result (text/category/brand/company)
// used to be discarded, so any q= returned the whole catalog and attribute
// filters were only applied when no other set was present.
func TestTurboProductSearchWordWithFilters(t *testing.T) {
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

	productRepo := NewProductRepo(store, NewPromoCampaignRepo(store), NewPromoPlanRepo(store), NewPromoLogRepo(store))
	categoryRepo := NewCategoryRepo(store)
	search := NewTurboProductSearch(store, productRepo, categoryRepo, true)

	products := []*model.Product{
		{ID: 101, Name: "Lampa Alfa", CategoryID: 1, CompanyID: 1, Price: 100, Currency: "PLN", Status: "active",
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Philips"}}},
		{ID: 102, Name: "Lampa Beta", CategoryID: 1, CompanyID: 1, Price: 500, Currency: "PLN", Status: "active",
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Osram"}}},
		{ID: 103, Name: "Lampa Gama", CategoryID: 2, CompanyID: 1, Price: 900, Currency: "PLN", Status: "active",
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Osram"}}},
		{ID: 104, Name: "Krzeslo Delta", CategoryID: 2, CompanyID: 1, Price: 50, Currency: "PLN", Status: "active",
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Osram"}}},
	}
	// Product documents (sort indexes and the final page load both read them),
	// indexed through the transactional batch path — the same one imports use.
	for _, p := range products {
		if err := store.DocPut(KeyProduct(p.ID), MarshalProduct(*p)); err != nil {
			t.Fatalf("seed product doc %d: %v", p.ID, err)
		}
	}
	txn := NewTransaction(store)
	if err := txn.Begin(); err != nil {
		t.Fatalf("begin txn: %v", err)
	}
	if err := search.BatchIndexProductstx(txn, products); err != nil {
		t.Fatalf("batch index tx: %v", err)
	}
	if err := txn.Commit(); err != nil {
		t.Fatalf("commit txn: %v", err)
	}
	if err := search.BuildSortIndexes(); err != nil {
		t.Fatalf("build sort indexes: %v", err)
	}

	total := func(t *testing.T, params TurboListParams) int64 {
		t.Helper()
		res, err := search.ListWithTurbo(params)
		if err != nil {
			t.Fatalf("ListWithTurbo %+v: %v", params, err)
		}
		return res.Total
	}

	cases := []struct {
		name   string
		params TurboListParams
		want   int64
	}{
		{"no filters", TurboListParams{}, 4},
		{"word only", TurboListParams{Q: "lampa"}, 3},
		{"word miss", TurboListParams{Q: "zzzz"}, 0},
		{"category only", TurboListParams{CategoryID: 2}, 2},
		{"word + category", TurboListParams{Q: "lampa", CategoryID: 2}, 1},
		{"word + attr", TurboListParams{Q: "lampa", AttrFilters: map[string][]string{"Marka": {"Osram"}}}, 2},
		{"word + attr miss", TurboListParams{Q: "lampa", AttrFilters: map[string][]string{"Marka": {"Nobrand"}}}, 0},
		// Aligned price bucket (price:0_5000 covers 0..5000).
		{"word + price bucket", TurboListParams{Q: "lampa", PriceMax: 5000}, 3},
		{"word + price bucket + attr", TurboListParams{Q: "lampa", PriceMax: 5000,
			AttrFilters: map[string][]string{"Marka": {"Philips"}}}, 1},
	}
	for _, tc := range cases {
		tc.params.Page = 1
		tc.params.Limit = 10
		if got := total(t, tc.params); got != tc.want {
			t.Errorf("%s: got total=%d, want %d", tc.name, got, tc.want)
		}
	}
}
