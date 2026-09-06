package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestEANPageSearchWordWithFilters verifies that a word query combines
// correctly with price and attribute filters. Regression test: the price
// intersection used to be computed in the wrong binary format, silently
// zeroed the candidate set and the query then returned EVERYTHING; a
// no-match price range must return an empty result, not the whole catalog.
func TestEANPageSearchWordWithFilters(t *testing.T) {
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
	categoryRepo := NewCategoryRepo(store)
	productRepo := NewProductRepo(store, NewPromoCampaignRepo(store), NewPromoPlanRepo(store), NewPromoLogRepo(store))
	search := NewEANPageSearch(store.DB(), repo, productRepo, categoryRepo, true)

	pages := []*model.EANPage{
		{EAN: "111", Slug: "lampa-alfa", Title: "Lampa Alfa", Currency: "PLN",
			MinPrice: 100, ProductCount: 1, IsActive: true,
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Philips"}}},
		{EAN: "222", Slug: "lampa-beta", Title: "Lampa Beta", Currency: "PLN",
			MinPrice: 500, ProductCount: 1, IsActive: true,
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Osram"}}},
		{EAN: "333", Slug: "lampa-gama", Title: "Lampa Gama", Currency: "PLN",
			MinPrice: 900, ProductCount: 1, IsActive: true,
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Osram"}}},
		{EAN: "444", Slug: "krzeslo-delta", Title: "Krzeslo Delta", Currency: "PLN",
			MinPrice: 50, ProductCount: 1, IsActive: true,
			Attributes: []model.KeyValue{{Key: "Marka", Value: "Osram"}}},
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

	total := func(t *testing.T, params EANPageListParams) int64 {
		t.Helper()
		res, err := search.ListWithTurbo(params)
		if err != nil {
			t.Fatalf("ListWithTurbo %+v: %v", params, err)
		}
		return res.Total
	}

	cases := []struct {
		name   string
		params EANPageListParams
		want   int64
	}{
		{"no filters", EANPageListParams{}, 4},
		{"word only", EANPageListParams{Q: "lampa"}, 3},
		{"word miss", EANPageListParams{Q: "zzzz"}, 0},
		{"price max only", EANPageListParams{PriceMax: 200}, 2},
		{"word + price max", EANPageListParams{Q: "lampa", PriceMax: 200}, 1},
		{"word + price min", EANPageListParams{Q: "lampa", PriceMin: 600}, 1},
		{"word + price range", EANPageListParams{Q: "lampa", PriceMin: 200, PriceMax: 600}, 1},
		{"word + attr", EANPageListParams{Q: "lampa", AttrFilters: map[string][]string{"Marka": {"Osram"}}}, 2},
		{"word + attr miss", EANPageListParams{Q: "lampa", AttrFilters: map[string][]string{"Marka": {"Nobrand"}}}, 0},
		{"word + price + attr", EANPageListParams{Q: "lampa", PriceMin: 200, PriceMax: 600,
			AttrFilters: map[string][]string{"Marka": {"Osram"}}}, 1},
		// Regression: price range matching nothing under a word query used to
		// return the WHOLE catalog instead of an empty result.
		{"word + price miss", EANPageListParams{Q: "lampa", PriceMin: 950}, 0},
		{"word + price + attr miss", EANPageListParams{Q: "lampa", PriceMin: 950,
			AttrFilters: map[string][]string{"Marka": {"Osram"}}}, 0},
		{"word + price + created sort", EANPageListParams{Q: "lampa", PriceMax: 600, Sort: "created_at"}, 2},
	}
	for _, tc := range cases {
		tc.params.Page = 1
		tc.params.Limit = 10
		if got := total(t, tc.params); got != tc.want {
			t.Errorf("%s: got total=%d, want %d", tc.name, got, tc.want)
		}
	}
}
