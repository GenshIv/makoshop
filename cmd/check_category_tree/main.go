package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/GenshIv/makodb/v2"
)

func main() {
	dbPath := "makoshop_db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	storeDB, err := makodb.OpenSharded(dbPath, 16, 100*1024*1024*1024, 5_000_000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer storeDB.Close()

	// Collect all categories
	cats := make(map[int64]*CategoryData)
	count := 0
	storeDB.ForEach(func(key string, value []byte) error {
		if len(key) >= 9 && key[:9] == "category:" {
			idStr := key[9:]
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return nil
			}
			var c CategoryData
			if err := json.Unmarshal(value, &c); err != nil {
				return nil
			}
			cats[id] = &c
			count++
		}
		return nil
	})

	fmt.Printf("Total categories: %d\n", count)

	// Find roots
	var roots []*CategoryData
	for _, c := range cats {
		if c.ParentID == nil {
			roots = append(roots, c)
		}
	}
	fmt.Printf("Root categories: %d\n", len(roots))

	// Validate parent references
	var missingParents []int64
	for _, c := range cats {
		if c.ParentID != nil {
			if _, ok := cats[*c.ParentID]; !ok {
				missingParents = append(missingParents, *c.ParentID)
			}
		}
	}
	if len(missingParents) > 0 {
		fmt.Printf("ERROR: %d categories reference missing parents: %v\n", len(missingParents), missingParents[:min(len(missingParents), 20)])
	} else {
		fmt.Println("OK: all parent references valid")
	}

	// Check turbo category indexes
	fmt.Println("\n=== Turbo category indexes ===")
	var catIndexKeys []string
	storeDB.ForEach(func(key string, value []byte) error {
		if len(key) >= 4 && key[:4] == "cat:" {
			catIndexKeys = append(catIndexKeys, key)
		}
		return nil
	})

	for _, key := range catIndexKeys {
		data, _ := storeDB.TurboRawRead(key)
		if data == nil || len(data) == 0 {
			continue
		}
		tokens := makodb.TurboUnsafeReadTokens(data)
		catID := key[4:]
		cat, ok := cats[mustParseInt64(catID)]
		if !ok {
			fmt.Printf("  %s: %d items (NO CATEGORY ENTITY)\n", key, len(tokens))
		} else {
			fmt.Printf("  %s: %d items (%s, parent=%v)\n", key, len(tokens), cat.Name, cat.ParentID)
		}
	}

	// Show tree for first root
	if len(roots) > 0 {
		fmt.Println("\n=== Category tree (first root) ===")
		printTree(storeDB, cats, roots[0], 0)
	}

	// Check specific category path (e.g., ID 11 = Мобильные телефоны)
	if c, ok := cats[11]; ok {
		fmt.Printf("\n=== Category 11 path ===\n")
		printPath(cats, c)
	}
}

type CategoryData struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id,omitempty"`
	Name     string `json:"name"`
}

func printTree(storeDB *makodb.ShardedDB, cats map[int64]*CategoryData, root *CategoryData, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	// Count products in this category index
	key := "cat:" + strconv.FormatInt(root.ID, 10)
	data, _ := storeDB.TurboRawRead(key)
	count := 0
	if data != nil && len(data) > 0 {
		tokens := makodb.TurboUnsafeReadTokens(data)
		count = len(tokens)
	}
	fmt.Printf("%s%d: %s (%d products)\n", indent, root.ID, root.Name, count)

	// Find children
	for _, c := range cats {
		if c.ParentID != nil && *c.ParentID == root.ID {
			printTree(storeDB, cats, c, depth+1)
		}
	}
}

func printPath(cats map[int64]*CategoryData, cat *CategoryData) {
	var path []*CategoryData
	current := cat
	for current != nil {
		path = append(path, current)
		if current.ParentID == nil {
			break
		}
		current = cats[*current.ParentID]
	}
	// Reverse
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	for _, c := range path {
		fmt.Printf("  %d: %s\n", c.ID, c.Name)
	}
}

func mustParseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
