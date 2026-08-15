package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/GenshIv/makodb/v2"
)

func main() {
	dbPath := "/home/ihar/IdeaProjects/makoshop/makoshop_db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := makodb.OpenSharded(dbPath, 16, 6710886400, 1000)
	if err != nil {
		fmt.Printf("Error opening DB: %v\n", err)
		return
	}
	defer db.Close()

	// List all keys with "sort" in name
	fmt.Println("=== Keys containing 'sort' ===")
	count := 0
	err = db.ForEach(func(key string, value []byte) error {
		if strings.Contains(key, "sort") {
			fmt.Printf("  %s (%d bytes)\n", key, len(value))
			count++
			if count > 50 {
				return fmt.Errorf("limit reached")
			}
		}
		return nil
	})
	if err != nil && err.Error() != "limit reached" {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Printf("Total: %d keys\n", count)

	// Check specific keys
	fmt.Println("\n=== Specific checks ===")
	for _, name := range []string{"price_asc", "price_desc", "created_at_desc", "sort:price_asc"} {
		fmt.Printf("\n--- %s ---\n", name)

		mainData, err := db.Get(name)
		if err != nil {
			fmt.Printf("  Get: %v\n", err)
		} else if mainData == nil {
			fmt.Printf("  Get: not found\n")
		} else {
			fmt.Printf("  Get: %d bytes, items=%d\n", len(mainData), makodb.TurboSortIndexCount(mainData))
		}
	}
}
