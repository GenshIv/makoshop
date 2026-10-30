package main

import (
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: check_leaf_mappings <data_dir>")
		os.Exit(1)
	}

	dataDir := os.Args[1]

	cfg := config.DatabaseConfig{
		Path:               dataDir,
		NumShards:          16,
		MaxTotalSize:       40 * 1024 * 1024 * 1024,
		NumBucketsPerShard: 256,
	}

	store, err := db.NewStore(cfg)
	if err != nil {
		fmt.Printf("Error opening store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	mappingRepo := db.NewCategoryMappingRepo(store)
	categoryRepo := db.NewCategoryRepo(store)

	// Load all mappings
	mappings, err := mappingRepo.List()
	if err != nil {
		fmt.Printf("Error listing mappings: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Total mappings: %d\n", len(mappings))

	// Load all categories (not just public tree)
	categories, err := categoryRepo.ListAll()
	if err != nil {
		fmt.Printf("Error listing categories: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Total categories: %d\n", len(categories))

	// Build map of category ID to category
	type CatInfo struct {
		ID       int64
		NameEn   string
	}
	catMap := make(map[int64]*CatInfo)
	for _, cat := range categories {
		name := cat.NameEn
		if name == "" {
			name = cat.NameRu
		}
		catMap[cat.ID] = &CatInfo{ID: cat.ID, NameEn: name}
	}

	// Check each mapping
	nonLeafCount := 0
	notFoundCount := 0
	for _, m := range mappings {
		cat, ok := catMap[m.TargetCategoryID]
		if !ok {
			notFoundCount++
			continue
		}

		// Check if category has children
		children, err := categoryRepo.GetDirectChildren(cat.ID)
		if err != nil {
			fmt.Printf("Error getting children for %d: %v\n", cat.ID, err)
			continue
		}

		if len(children) > 0 {
			nonLeafCount++
			fmt.Printf("\nMapping %d (source: %s) -> non-leaf category %d (%s)\n",
				m.ID, m.SourceCode, m.TargetCategoryID, cat.NameEn)
			fmt.Printf("  Has %d children:\n", len(children))
			for _, childID := range children {
				childCat, ok := catMap[childID]
				if ok {
					fmt.Printf("    - %d: %s\n", childID, childCat.NameEn)
				} else {
					fmt.Printf("    - %d: (not found)\n", childID)
				}
			}
		}
	}

	fmt.Printf("\n%d mappings point to non-leaf categories\n", nonLeafCount)
	fmt.Printf("%d mappings have target not found in categories\n", notFoundCount)
}
