package main

import (
	"fmt"
	"os"

	"github.com/GenshIv/makodb/v2"
)

func main() {
	dbPath := "/home/ihar/IdeaProjects/makoshop/makoshop_db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := makodb.OpenSharded(dbPath, 16, 6710886400, 1000, false)
	if err != nil {
		fmt.Printf("Error opening DB: %v\n", err)
		return
	}
	defer db.Close()

	// Get candidates for category 65
	catKey := "cat:65"
	candidates, err := db.TurboGetIndexTokens(catKey)
	if err != nil {
		fmt.Printf("Error reading category index: %v\n", err)
		return
	}
	fmt.Printf("Candidates for cat:65: %d\n", len(candidates))

	// Try TurboSortIndexPageWithDocsFromDB

	res, err := db.TurboSortIndexPageWithDocsFromDB(makodb.TurboSortPageWithDocsParams{
		Name:       "price_asc",
		Candidates: candidates, // []Key128
		Page:       0,          // 0-based
		PageSize:   5,
		Desc:       false, // true = обратный порядок
		DocPrefix:  "product:",
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Total: %d\n", res.Total)
	fmt.Printf("DocIDs: %d\n", len(res.DocIDs))
	fmt.Printf("Docs count: %d\n", len(res.Docs))
	for i, doc := range res.Docs {
		if doc != nil {
			fmt.Printf("  Doc[%d] (%d bytes): %s\n", i, len(doc), string(doc[:min(len(doc), 100)]))
		} else {
			fmt.Printf("  Doc[%d]: nil\n", i)
		}
	}

	// Check if documents exist with these docIDs as keys
	if len(res.DocIDs) > 0 {
		fmt.Printf("\nChecking document keys:\n")
		for _, docID := range res.DocIDs[:3] {
			// Convert Key128 to string for display
			key := "product:" + fmt.Sprintf("%v", docID)
			data, err := db.Get(key)
			if err != nil {
				fmt.Printf("  %s: %v\n", key, err)
			} else {
				fmt.Printf("  %s: %d bytes\n", key, len(data))
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
