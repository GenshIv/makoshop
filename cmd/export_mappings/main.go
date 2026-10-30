package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: export_mappings <data_dir> <output_json>")
		os.Exit(1)
	}

	dataDir := os.Args[1]
	outputFile := os.Args[2]

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

	mappings, err := mappingRepo.List()
	if err != nil {
		fmt.Printf("Error listing mappings: %v\n", err)
		os.Exit(1)
	}

	type MappingExport struct {
		SourceCode       string `json:"source_code"`
		TargetCategoryID int64  `json:"target_category_id"`
	}

	exports := make([]MappingExport, len(mappings))
	for i, m := range mappings {
		exports[i] = MappingExport{
			SourceCode:       m.SourceCode,
			TargetCategoryID: m.TargetCategoryID,
		}
	}

	data, err := json.MarshalIndent(exports, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile(outputFile, data, 0644)
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Exported %d mappings to %s\n", len(exports), outputFile)
}
