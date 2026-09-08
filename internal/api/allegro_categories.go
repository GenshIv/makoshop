package api

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Allegro category resolver: the feed delivers only numeric category IDs
// (categories[0].name = "165"); the crawler (cmd/allegro-cats) dumps the
// public category tree into docs/allegro_categories.json as
// id -> {name, alias, path}. The import resolves an ID to its full path
// ("Elektronika > Komputery > Laptopy") so ShopCategory keywords carry real
// words for catalogization.

type allegroCatEntry struct {
	Name   string `json:"name"`
	Alias  string `json:"alias"`
	Path   string `json:"path"`
	Parent string `json:"parent,omitempty"`
}

var (
	allegroCatsOnce sync.Once
	allegroCats     map[string]allegroCatEntry
)

func loadAllegroCategories() map[string]allegroCatEntry {
	allegroCatsOnce.Do(func() {
		allegroCats = map[string]allegroCatEntry{}
		data, err := os.ReadFile("docs/allegro_categories.json")
		if err != nil {
			return // no dump yet: resolver degrades to feed-level fallback
		}
		var m map[string]allegroCatEntry
		if err := json.Unmarshal(data, &m); err != nil {
			fmt.Printf("[IMPORT-ALLEGRO] WARN: parse allegro_categories.json: %v\\n", err)
			return
		}
		// Reconstruct missing paths using parent relationships.
		reconstructPaths(m)
		allegroCats = m
		fmt.Printf("[IMPORT-ALLEGRO] category dump loaded: %d categories\\n", len(allegroCats))
	})
	return allegroCats
}

// reconstructPaths fills in missing Path fields by walking up the parent chain.
func reconstructPaths(cats map[string]allegroCatEntry) {
	visited := make(map[string]bool)
	for id, entry := range cats {
		if entry.Path != "" || entry.Parent == "" {
			continue
		}
		// Walk up to find an ancestor with a known path.
		path := reconstructPathFor(id, cats, visited)
		if path != "" {
			entry.Path = path
			cats[id] = entry
		}
	}
}

func reconstructPathFor(id string, cats map[string]allegroCatEntry, visited map[string]bool) string {
	if visited[id] {
		return "" // cycle detected
	}
	visited[id] = true
	entry := cats[id]
	if entry.Parent == "" {
		return ""
	}
	parent := cats[entry.Parent]
	if parent.Path != "" {
		return parent.Path + " > " + entry.Name
	}
	// Parent also missing path — recurse.
	parentPath := reconstructPathFor(entry.Parent, cats, visited)
	if parentPath != "" {
		return parentPath + " > " + entry.Name
	}
	return ""
}

// resolveAllegroShopCategory maps a feed category reference (a numeric ID,
// possibly with stray whitespace) to "Parent > Child > Leaf" from the dump.
// Returns "" when the reference is not resolvable (empty, non-numeric or
// unknown ID) — the caller falls back to the raw value or feed-level category.
func resolveAllegroShopCategory(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if _, err := strconv.ParseInt(ref, 10, 64); err != nil {
		return ref // already a name, not an ID
	}
	entry, ok := loadAllegroCategories()[ref]
	if !ok {
		return ""
	}
	if entry.Path != "" {
		return entry.Path + " > " + entry.Name
	}
	return entry.Name
}
