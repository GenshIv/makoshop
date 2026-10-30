package main

import (
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: rebuild_category_tree <data_dir>")
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

	fmt.Println("Rebuilding category tree...")
	categoryRepo.RebuildTrees()
	fmt.Println("Done!")
}
