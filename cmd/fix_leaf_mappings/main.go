package main

import (
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: fix_leaf_mappings <data_dir> [--dry-run]")
		os.Exit(1)
	}

	dataDir := os.Args[1]
	dryRun := false
	for _, arg := range os.Args[2:] {
		if arg == "--dry-run" {
			dryRun = true
		}
	}

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

	// Load all categories
	categories, err := categoryRepo.ListAll()
	if err != nil {
		fmt.Printf("Error listing categories: %v\n", err)
		os.Exit(1)
	}

	// Build category map and identify leaf categories
	catMap := make(map[int64]*model.Category)
	isLeaf := make(map[int64]bool)
	for i, cat := range categories {
		catMap[cat.ID] = &categories[i]
		children, err := categoryRepo.GetDirectChildren(cat.ID)
		if err == nil && len(children) == 0 {
			isLeaf[cat.ID] = true
		}
	}

	fixed := 0
	for _, m := range mappings {
		targetCat, ok := catMap[m.TargetCategoryID]
		if !ok {
			continue
		}

		// If already a leaf, skip
		if isLeaf[targetCat.ID] {
			continue
		}

		// Find first leaf descendant
		leafID := findFirstLeaf(categoryRepo, catMap, targetCat.ID)
		if leafID == 0 || leafID == m.TargetCategoryID {
			continue
		}

		if dryRun {
			fmt.Printf("Would update mapping %d: %s -> %d (%s)\n",
				m.ID, m.SourceCode, leafID, catMap[leafID].NameEn)
		} else {
			err = mappingRepo.Update(m.ID, func(mapping *model.CategoryMapping) {
				mapping.TargetCategoryID = leafID
			})
			if err != nil {
				fmt.Printf("Error updating mapping %d: %v\n", m.ID, err)
				continue
			}
			fixed++
		}
	}

	if dryRun {
		fmt.Println("Dry run complete. No changes made.")
	} else {
		fmt.Printf("Fixed %d mappings\n", fixed)
	}
}

// findFirstLeaf finds the first leaf descendant of a category using BFS.
func findFirstLeaf(categoryRepo *db.CategoryRepo, catMap map[int64]*model.Category, rootID int64) int64 {
	queue := []int64{rootID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		children, err := categoryRepo.GetDirectChildren(current)
		if err != nil || len(children) == 0 {
			return current // This is a leaf
		}

		for _, child := range children {
			queue = append(queue, child)
		}
	}

	return rootID
}
