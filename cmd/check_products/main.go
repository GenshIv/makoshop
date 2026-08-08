package main

import (
	"fmt"
	"strings"

	"github.com/GenshIv/makodb/v2"
)

func main() {
	dbConn, err := makodb.OpenSharded("/home/ihar/IdeaProjects/makoshop/makoshop_db", 16, 6710886400, 1000)
	if err != nil {
		fmt.Printf("OpenSharded: %v\n", err)
		return
	}
	defer dbConn.Close()

	// Find all keys containing "product"
	fmt.Println("=== Keys containing 'product' ===")
	count := 0
	dbConn.ForEach(func(key string, value []byte) error {
		if strings.Contains(key, "product") {
			fmt.Printf("  %s (%d bytes)\n", key, len(value))
			count++
			if count >= 50 {
				return fmt.Errorf("limit")
			}
		}
		return nil
	})
	fmt.Printf("Total: %d keys\n", count)
}
