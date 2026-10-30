package main

import (
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: list_categories <data_dir>")
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

	categoryRepo := db.NewCategoryRepo(store)

	cats, err := categoryRepo.ListAll()
	if err != nil {
		fmt.Printf("Error listing categories: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Total categories: %d\n", len(cats))
	for i, cat := range cats {
		if i >= 20 {
			break
		}
		parentStr := "nil"
		if cat.ParentID != nil {
			parentStr = fmt.Sprintf("%d", *cat.ParentID)
		}
		fmt.Printf("  %d: %s (parent=%s, slug=%s)\n", cat.ID, cat.NameEn, parentStr, cat.Slug)
	}
}
