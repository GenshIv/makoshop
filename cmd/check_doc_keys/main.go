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

	// Find all keys containing "160867"
	fmt.Println("=== Keys containing '160867' ===")
	count := 0
	err = db.ForEach(func(key string, value []byte) error {
		if strings.Contains(key, "160867") {
			fmt.Printf("  %s (%d bytes)\n", key, len(value))
			count++
			if count > 20 {
				return fmt.Errorf("limit reached")
			}
		}
		return nil
	})
	if err != nil && err.Error() != "limit reached" {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Printf("Total: %d keys\n", count)

	// Check product keys format
	fmt.Println("\n=== Sample product keys ===")
	count = 0
	err = db.ForEach(func(key string, value []byte) error {
		if strings.HasPrefix(key, "product:") {
			fmt.Printf("  %s (%d bytes)\n", key, len(value))
			count++
			if count >= 10 {
				return fmt.Errorf("limit reached")
			}
		}
		return nil
	})
	if err != nil && err.Error() != "limit reached" {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Printf("Total: %d keys\n", count)
}
