// allegro-cats builds the Allegro category dump (id -> {name, alias, path})
// from LOCALLY SAVED category pages — no HTTP, no crawling, no ban risk.
//
// Workflow: save category pages from a browser (File > Save Page As > "Web
// page, HTML only" is enough) into a directory and point the tool at it:
//
//	go run ./cmd/allegro-cats -dir saved-pages -out docs/allegro_categories.json
//
// Each page contributes:
//   - its breadcrumb path (anchors with itemprop="item"; root-section levels
//     with UUID ids enrich the path text, numeric ids become map keys);
//   - every /kategoria/ link with its numeric id (apron dropdowns — the
//     children/siblings of each level), so pages covering different branches
//     accumulate into one tree.
//
// Re-running with more pages MERGES into an existing dump (already-known ids
// keep their path; new ids and better paths are added), so you can feed the
// directory incrementally.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type catEntry struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Path  string `json:"path,omitempty"` // "Elektronika > Komputery" (root "Allegro" skipped)
}

var (
	catIDRe  = regexp.MustCompile(`data-analytics-click-custom-navigation-category-id="(\d+)"`)
	hrefIDRe = regexp.MustCompile(`-(\d+)$`)
	// Every anchor pointing at a category page (trail, aprons, grids).
	anchorRe = regexp.MustCompile(`(?s)<a\s[^>]*href="(/kategoria/[^"]+)"[^>]*>(.*?)</a>`)
	// Trail breadcrumb anchors: itemprop="item" is their reliable marker.
	trailRe = regexp.MustCompile(`(?s)<a\s[^>]*itemprop="item"[^>]*>(.*?)</a>`)
	hrefRe  = regexp.MustCompile(`href="([^"]+)"`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
)

func main() {
	dir := flag.String("dir", "saved-pages", "directory with saved Allegro category pages (*.html)")
	out := flag.String("out", "docs/allegro_categories.json", "output JSON file (merged if exists)")
	flag.Parse()

	cats := loadExisting(*out)
	before := len(cats)

	files, err := filepath.Glob(filepath.Join(*dir, "*.html"))
	if err != nil || len(files) == 0 {
		// Also accept .htm and files without extension.
		files2, _ := filepath.Glob(filepath.Join(*dir, "*.htm"))
		files = append(files, files2...)
		if len(files) == 0 {
			fmt.Printf("no *.html files in %s\n", *dir)
			os.Exit(1)
		}
	}

	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			fmt.Printf("SKIP %s: %v\n", f, err)
			continue
		}
		mergePage(cats, string(body))
		fmt.Printf("%s: done (total %d)\n", f, len(cats))
	}

	writeJSON(*out, cats)
	fmt.Printf("done: %d categories (+%d new) -> %s\n", len(cats), len(cats)-before, *out)
}

// mergePage folds one saved page into the map.
func mergePage(cats map[string]catEntry, body string) {
	path := breadcrumbs(body)

	// Path entries with a numeric id become keys; UUID levels only enrich
	// the path text.
	for i, e := range path {
		if e.id == "" || e.name == "" {
			continue
		}
		full := strings.Join(names(path[:i+1]), " > ")
		prev, ok := cats[e.id]
		if !ok {
			cats[e.id] = catEntry{Name: e.name, Alias: e.alias, Path: full}
			continue
		}
		// Prefer the longer path (a deeper page refines shallow knowledge);
		// keep the first-seen name otherwise.
		if len(full) > len(prev.Path) {
			prev.Path = full
			cats[e.id] = prev
		}
	}

	// Apron/grid links: children and siblings of every level.
	for _, l := range anchors(body) {
		if l.id == "" || l.name == "" {
			continue
		}
		if _, ok := cats[l.id]; !ok {
			cats[l.id] = catEntry{Name: l.name, Alias: l.alias}
		}
	}
}

func loadExisting(path string) map[string]catEntry {
	cats := map[string]catEntry{}
	data, err := os.ReadFile(path)
	if err != nil {
		return cats
	}
	_ = json.Unmarshal(data, &cats)
	return cats
}

type pathEntry struct{ id, name, alias string }

// breadcrumbs extracts the breadcrumb path: trail anchors (itemprop="item"),
// in document order. Apron dropdown links are excluded.
func breadcrumbs(body string) []pathEntry {
	var out []pathEntry
	for _, m := range trailRe.FindAllStringSubmatch(body, -1) {
		full, inner := m[0], m[1]
		href := ""
		if hm := hrefRe.FindStringSubmatch(full); hm != nil {
			href = hm[1]
		}
		// The id attribute lives on the <a> tag itself — numeric for real
		// categories, a UUID for root sections (path text only).
		id := ""
		if idm := catIDRe.FindStringSubmatch(full); idm != nil {
			id = idm[1]
		} else if idm := hrefIDRe.FindStringSubmatch(href); idm != nil {
			id = idm[1]
		}
		name := strings.TrimSpace(tagRe.ReplaceAllString(inner, ""))
		alias := strings.TrimPrefix(href, "/kategoria/")
		out = append(out, pathEntry{id: id, name: name, alias: alias})
	}
	// De-duplicate consecutive repeats of the same NUMERIC id, keep order.
	// Empty ids (root-section levels) must all stay — they carry path text.
	dedup := out[:0]
	for i, e := range out {
		if i > 0 && e.id != "" && out[i-1].id == e.id {
			continue
		}
		dedup = append(dedup, e)
	}
	return dedup
}

type linkEntry struct{ href, id, name, alias string }

// anchors extracts every /kategoria/ link with its numeric id (attribute or
// href suffix) — children/siblings from aprons and category grids.
func anchors(body string) []linkEntry {
	var out []linkEntry
	for _, m := range anchorRe.FindAllStringSubmatch(body, -1) {
		href, inner := m[1], m[2]
		id := ""
		if idm := catIDRe.FindStringSubmatch(m[0]); idm != nil {
			id = idm[1]
		} else if idm := hrefIDRe.FindStringSubmatch(href); idm != nil {
			id = idm[1]
		}
		name := strings.TrimSpace(tagRe.ReplaceAllString(inner, ""))
		out = append(out, linkEntry{href: href, id: id, name: name, alias: strings.TrimPrefix(href, "/kategoria/")})
	}
	return out
}

func names(es []pathEntry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		if e.name == "" || strings.EqualFold(e.name, "Allegro") {
			continue // skip the root
		}
		out = append(out, e.name)
	}
	return out
}

func writeJSON(path string, cats map[string]catEntry) {
	keys := make([]string, 0, len(cats))
	for k, v := range cats {
		if _, err := strconv.ParseInt(k, 10, 64); err != nil || v.Name == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.ParseInt(keys[i], 10, 64)
		b, _ := strconv.ParseInt(keys[j], 10, 64)
		return a < b
	})
	ordered := make(map[string]catEntry, len(keys))
	for _, k := range keys {
		ordered[k] = cats[k]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Println("mkdir:", err)
		os.Exit(1)
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		fmt.Println("marshal:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Println("write:", err)
		os.Exit(1)
	}
}
