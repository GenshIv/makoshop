package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

type MappingInput struct {
	SourceCode       string `json:"source_code"`
	TargetCategoryID int64  `json:"target_category_id"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: import_mappings <data_dir> <mappings_json>")
		os.Exit(1)
	}

	dataDir := os.Args[1]
	jsonFile := os.Args[2]

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

	// Read JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var inputs []MappingInput
	if err := json.Unmarshal(data, &inputs); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Importing %d mappings...\n", len(inputs))

	imported := 0
	for _, input := range inputs {
		m := &model.CategoryMapping{
			SourceCode:       input.SourceCode,
			TargetCategoryID: input.TargetCategoryID,
		}
		if err := mappingRepo.Create(m); err != nil {
			fmt.Printf("Error creating mapping %s: %v\n", input.SourceCode, err)
			continue
		}
		imported++
	}

	fmt.Printf("Imported %d mappings\n", imported)
}
