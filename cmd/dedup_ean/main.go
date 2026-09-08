package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: dedup_ean <data_dir> [--dry-run]")
		os.Exit(1)
	}

	dataDir := os.Args[1]
	dryRun := false
	for _, arg := range os.Args[2:] {
		if arg == "--dry-run" {
			dryRun = true
		}
	}

	fmt.Printf("Data directory: %s\n", dataDir)
	if dryRun {
		fmt.Println("Mode: DRY RUN (no changes will be made)")
	} else {
		fmt.Println("Mode: LIVE (changes will be made)")
	}

	// Create database config
	cfg := config.DatabaseConfig{
		Path:               dataDir,
		NumShards:          16,
		MaxTotalSize:       40 * 1024 * 1024 * 1024, // 40 GB
		NumBucketsPerShard: 256,
	}

	// Open database
	store, err := db.NewStore(cfg)
	if err != nil {
		fmt.Printf("Error opening store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	eanPageRepo := db.NewEANPageRepo(store)
	productRepo := db.NewProductRepo(store, nil, nil, nil)
	turboSearch := db.NewTurboProductSearch(store, productRepo, nil, true)
	productRepo.SetTurboSearch(turboSearch)

	// List all EAN pages
	fmt.Println("\nLoading all EAN pages...")
	allPages, err := eanPageRepo.List()
	if err != nil {
		fmt.Printf("Error listing EAN pages: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Total EAN pages: %d\n", len(allPages))

	// Group by GTIN
	fmt.Println("\nGrouping by GTIN...")
	gtinGroups := make(map[string][]*model.EANPage)
	for i, page := range allPages {
		gtin := extractGTIN(page.Attributes)
		if gtin != "" {
			gtinGroups[gtin] = append(gtinGroups[gtin], &allPages[i])
		}
	}

	fmt.Printf("EAN pages with GTIN: %d\n", countPagesWithGTIN(allPages))
	fmt.Printf("Unique GTINs: %d\n", len(gtinGroups))

	// Find duplicates
	duplicateGroups := make(map[string][]*model.EANPage)
	for gtin, pages := range gtinGroups {
		if len(pages) > 1 {
			duplicateGroups[gtin] = pages
		}
	}

	fmt.Printf("Duplicate GTIN groups: %d\n", len(duplicateGroups))

	if len(duplicateGroups) == 0 {
		fmt.Println("\nNo duplicates found!")
		return
	}

	// Process each duplicate group
	totalDeleted := 0
	for gtin, pages := range duplicateGroups {
		fmt.Printf("\nGTIN %s: %d duplicates\n", gtin, len(pages))

		// Keep the first page (earliest created), delete the rest
		canonical := pages[0]
		duplicates := pages[1:]

		fmt.Printf("  Keeping: EAN=%s Slug=%s\n", canonical.EAN, canonical.Slug)

		for _, dup := range duplicates {
			fmt.Printf("  Deleting: EAN=%s Slug=%s\n", dup.EAN, dup.Slug)

			if !dryRun {
				// Get products for this duplicate page
				products, err := turboSearch.GetProductsByEAN(dup.EAN)
				if err != nil {
					fmt.Printf("    Error getting products: %v\n", err)
					continue
				}

				// Delete products
				for _, p := range products {
					err := productRepo.DeleteProductByID(p.ID)
					if err != nil {
						fmt.Printf("    Error deleting product %d: %v\n", p.ID, err)
					} else {
						totalDeleted++
					}
				}

				// Delete the duplicate EAN page
				err = eanPageRepo.Delete(dup.EAN)
				if err != nil {
					fmt.Printf("    Error deleting EAN page: %v\n", err)
				} else {
					totalDeleted++
				}
			} else {
				// Dry run: just count
				products, _ := turboSearch.GetProductsByEAN(dup.EAN)
				totalDeleted += len(products) + 1 // products + page
			}
		}
	}

	fmt.Printf("\nTotal deleted: %d (products + EAN pages)\n", totalDeleted)
	if dryRun {
		fmt.Println("Dry run complete. No changes were made.")
	} else {
		fmt.Println("Deduplication complete!")
	}
}

func extractGTIN(attrs []model.KeyValue) string {
	for _, attr := range attrs {
		key := strings.ToLower(attr.Key)
		if key == "gtin" || key == "ean" || key == "attr_225693" {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func countPagesWithGTIN(pages []model.EANPage) int {
	count := 0
	for _, page := range pages {
		if extractGTIN(page.Attributes) != "" {
			count++
		}
	}
	return count
}
