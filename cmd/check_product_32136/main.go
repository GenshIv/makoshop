package main

import (
	"fmt"

	"github.com/GenshIv/makodb/v2"
)

func main() {
	dbConn, err := makodb.OpenSharded("/home/ihar/IdeaProjects/makoshop/makoshop_db", 16, 6710886400, 1000)
	if err != nil {
		fmt.Printf("OpenSharded: %v\n", err)
		return
	}
	defer dbConn.Close()

	// Check product 32136 with different prefixes
	for _, key := range []string{
		"product:32136",
		"turbo_idx:product:32136",
		"turbo_doc:product:32136",
		"turbo_udx:product:32136",
	} {
		data, err := dbConn.Get(key)
		if err != nil {
			fmt.Printf("Get(%s): %v\n", key, err)
		} else {
			fmt.Printf("Get(%s): %d bytes\n", key, len(data))
			fmt.Printf("  Content: %s\n", string(data[:min(len(data), 200)]))
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
