package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/GenshIv/makodb/v2"
)

func main() {
	db, err := makodb.OpenSharded("makoshop_db", 16, 40*1024*1024*1024, 5_000_000, false)
	if err != nil {
		fmt.Printf("Open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Check union indexes (eanpage_cat_union:{catID})
	// These contain all SCU pages of the category + all descendants.
	for _, id := range []int64{1, 2, 10, 65} {
		key := fmt.Sprintf("eanpage_cat_union:%d", id)
		data, _ := db.TurboRawRead(key)
		if len(data) > 0 {
			count := binary.LittleEndian.Uint64(data)
			fmt.Printf("Union %s: %d docIDs\n", key, count)
		} else {
			fmt.Printf("Union %s: missing/empty\n", key)
		}
	}
}
