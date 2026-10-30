package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

type CategoryInput struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
}

type CategoryImportFile struct {
	Categories []CategoryInput `json:"categories"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: import_categories <data_dir> <categories_json>")
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

	categoryRepo := db.NewCategoryRepo(store)

	// Read JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var importFile CategoryImportFile
	if err := json.Unmarshal(data, &importFile); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Importing %d categories...\n", len(importFile.Categories))

	imported := 0
	for _, input := range importFile.Categories {
		c := &model.Category{
			ID:        input.ID,
			ParentID:  input.ParentID,
			NameRu:    input.Name,
			NameEn:    input.Name,
			Slug:      input.Slug,
			IsActive:  input.IsActive,
			SortOrder: input.SortOrder,
		}

		if err := categoryRepo.Create(c); err != nil {
			fmt.Printf("Error creating category %d (%s): %v\n", input.ID, input.Name, err)
			continue
		}
		imported++
	}

	fmt.Printf("Imported %d categories\n", imported)

	// Rebuild tree
	fmt.Println("Rebuilding category tree...")
	categoryRepo.RebuildTrees()
	fmt.Println("Done!")
}
