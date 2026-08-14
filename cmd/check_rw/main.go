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

	db, err := makodb.OpenSharded(dbPath, 16, 6710886400, 1000)
	if err != nil {
		fmt.Printf("Error opening DB: %v\n", err)
		return
	}
	defer db.Close()

	// Check if product:160867 exists via TurboRawRead
	key := "product:160867"
	data, err := db.TurboRawRead(key)
	if err != nil {
		fmt.Printf("TurboRawRead(%s): %v\n", key, err)
	} else {
		fmt.Printf("TurboRawRead(%s): %d bytes\n", key, len(data))
	}

	// Check via Get
	data2, err := db.Get(key)
	if err != nil {
		fmt.Printf("Get(%s): %v\n", key, err)
	} else {
		fmt.Printf("Get(%s): %d bytes\n", key, len(data2))
	}

	// Check via MultiGet
	keys := []string{"product:160867", "product:160897"}
	multiData, err := db.MultiGet(keys)
	if err != nil {
		fmt.Printf("MultiGet: %v\n", err)
	} else {
		for k, v := range multiData {
			fmt.Printf("MultiGet[%s]: %d bytes\n", k, len(v))
		}
	}

	// Check via MultiGetByDocIDsWithPrefix (uses TurboRawRead)
	docs, err := db.MultiGetByDocIDsWithPrefix([]uint64{160867, 160897}, "product:")
	if err != nil {
		fmt.Printf("MultiGetByDocIDsWithPrefix: %v\n", err)
	} else {
		for i, d := range docs {
			if d != nil {
				fmt.Printf("MultiGetByDocIDsWithPrefix[%d] (product:%d): %d bytes\n", i, []uint64{160867, 160897}[i], len(d))
			} else {
				fmt.Printf("MultiGetByDocIDsWithPrefix[%d] (product:%d): nil\n", i, []uint64{160867, 160897}[i])
			}
		}
	}

	// Check turbo_idx:product:160867
	turboKey := "product:160867"
	turboData, err := db.TurboRawRead(turboKey)
	if err != nil {
		fmt.Printf("TurboRawRead(%s): %v\n", turboKey, err)
	} else {
		fmt.Printf("TurboRawRead(%s): %d bytes\n", turboKey, len(turboData))
	}

	// Check if there's a mapping between turbo docID and product key
	// Maybe documents are stored under a different key format?
	fmt.Println("\n=== Searching for product data ===")
	// Try to find any key that contains product data for ID 160867
	fmt.Printf("Searching for keys with '160867':\n")
	count := 0
	err = db.ForEach(func(key string, value []byte) error {
		if len(key) > 10 && key[len(key)-6:] == "160867" {
			fmt.Printf("  %s (%d bytes)\n", key, len(value))
			count++
			if count >= 10 {
				return fmt.Errorf("limit")
			}
		}
		return nil
	})
	if err != nil && err.Error() != "limit" {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Printf("Total: %d\n", count)
}
