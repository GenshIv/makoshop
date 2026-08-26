package main

import (
	"fmt"
	"os"
	"time"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	cfg := config.DatabaseConfig{
		Path:               "/home/ihar/IdeaProjects/makoshop/makoshop_db",
		NumShards:          16,
		MaxTotalSize:       40 * 1024 * 1024 * 1024,
		NumBucketsPerShard: 5_000_000,
	}

	store, err := db.NewStore(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewStore: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	repo := db.NewEANPageRepo(store)
	categoryRepo := db.NewCategoryRepo(store)
	promoCampaignRepo := db.NewPromoCampaignRepo(store)
	promoPlanRepo := db.NewPromoPlanRepo(store)
	promoLogRepo := db.NewPromoLogRepo(store)
	productRepo := db.NewProductRepo(store, promoCampaignRepo, promoPlanRepo, promoLogRepo)

	search := db.NewEANPageSearch(store.DB(), repo, productRepo, categoryRepo, true)

	// Warmup
	for i := 0; i < 10; i++ {
		_, _ = search.ListWithTurbo(db.EANPageListParams{Sort: "price_asc", Page: 1, Limit: 50})
		_, _ = search.ListWithTurbo(db.EANPageListParams{PriceMin: 100, PriceMax: 10000, Sort: "price_asc", Page: 1, Limit: 50})
		_, _ = search.ListWithTurbo(db.EANPageListParams{PriceMin: 100, PriceMax: 10000, Sort: "price_desc", Page: 1, Limit: 50})
	}

	const iterations = 200

	// Fast path: no filters
	var totalFast time.Duration
	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, _ = search.ListWithTurbo(db.EANPageListParams{Sort: "price_asc", Page: 1, Limit: 50})
		totalFast += time.Since(start)
	}

	// Price range asc
	var totalPRAsc time.Duration
	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, _ = search.ListWithTurbo(db.EANPageListParams{PriceMin: 100, PriceMax: 10000, Sort: "price_asc", Page: 1, Limit: 50})
		totalPRAsc += time.Since(start)
	}

	// Price range desc
	var totalPRDesc time.Duration
	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, _ = search.ListWithTurbo(db.EANPageListParams{PriceMin: 100, PriceMax: 10000, Sort: "price_desc", Page: 1, Limit: 50})
		totalPRDesc += time.Since(start)
	}

	// Price range + text filter (slow path)
	var totalPRText time.Duration
	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, _ = search.ListWithTurbo(db.EANPageListParams{PriceMin: 100, PriceMax: 10000, Q: "телефон", Sort: "price_asc", Page: 1, Limit: 50})
		totalPRText += time.Since(start)
	}

	// Price min only (no max)
	var totalPRMin time.Duration
	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, _ = search.ListWithTurbo(db.EANPageListParams{PriceMin: 100, Sort: "price_asc", Page: 1, Limit: 50})
		totalPRMin += time.Since(start)
	}

	fmt.Printf("=== Results (%d iterations) ===\n", iterations)
	fmt.Printf("Fast path (no filters):       avg=%v\n", totalFast/iterations)
	fmt.Printf("Price range asc (100-10000):  avg=%v\n", totalPRAsc/iterations)
	fmt.Printf("Price range desc (100-10000): avg=%v\n", totalPRDesc/iterations)
	fmt.Printf("Price range + text:           avg=%v\n", totalPRText/iterations)
	fmt.Printf("Price min only (>=100):       avg=%v\n", totalPRMin/iterations)
}
