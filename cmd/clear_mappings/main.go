package main

import (
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: clear_mappings <data_dir>")
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

	// List all mappings and delete them
	mappings, err := mappingRepo.List()
	if err != nil {
		fmt.Printf("Error listing mappings: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Deleting %d mappings...\n", len(mappings))
	for _, m := range mappings {
		err = mappingRepo.Delete(m.ID)
		if err != nil {
			fmt.Printf("Error deleting mapping %d: %v\n", m.ID, err)
		}
	}

	remaining, _ := mappingRepo.List()
	fmt.Printf("Remaining mappings: %d\n", len(remaining))
}
