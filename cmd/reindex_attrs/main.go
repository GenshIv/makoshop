package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	dbPath := flag.String("db-path", "makoshop_db", "Database path")
	flag.Parse()

	cfg := config.DefaultConfig()
	cfg.Database.Path = *dbPath

	store, err := db.NewStore(cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	productRepo := db.NewProductRepo(store, db.NewPromoCampaignRepo(store), db.NewPromoPlanRepo(store), db.NewPromoLogRepo(store))

	fmt.Println("Reindexing product attributes...")

	var totalProducts int64
	var indexed int64
	var errors int64

	// Use ProductRepo to get all products
	allProducts, err := productRepo.GetAllProducts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting products: %v\n", err)
		os.Exit(1)
	}

	totalProducts = int64(len(allProducts))
	fmt.Printf("Found %d products\n", totalProducts)

	// Reindex in parallel
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // limit concurrency

	for _, p := range allProducts {
		sem <- struct{}{}
		wg.Add(1)
		go func(prod model.Product) {
			defer wg.Done()
			defer func() { <-sem }()

			// Rebuild indexes for this product
			if err := productRepo.ReindexProduct(&prod); err != nil {
				atomic.AddInt64(&errors, 1)
				return
			}

			atomic.AddInt64(&indexed, 1)
		}(p)
	}

	wg.Wait()

	fmt.Printf("Reindex complete: %d products, %d indexed, %d errors\n", totalProducts, indexed, errors)
}
