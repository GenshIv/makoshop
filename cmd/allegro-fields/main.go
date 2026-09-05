// allegro-fields reports attribute-code coverage of Allegro price feeds
// against the field map (docs/allegro_field_map.json): which attr_XXX codes
// the feed uses, how often, which are mapped/unmapped, and how many products
// each unmapped code affects — a prioritized worklist for extending the map.
//
// The -pages mode harvests parameter names from locally saved Allegro pages
// (HTML files in a directory) and merges them into the field map. Only
// parameter/filter structures are trusted — a plain "id+name" pair usually
// belongs to a category, not a parameter, and would poison the map.
//
//	go run ./cmd/allegro-fields -pages allegro-saved-pages           # dry-run
//	go run ./cmd/allegro-fields -pages allegro-saved-pages -write    # merge
//
// Usage:
//
//	go run ./cmd/allegro-fields -feed prices/allegro___bestseller_dom_i_ogd.json
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

type fieldMapFile struct {
	Fields map[string]string `json:"fields"`
}

type feedFile struct {
	Products []struct {
		Name   string `json:"name"`
		Fields []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"fields"`
	} `json:"products"`
}

type catEntry struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
	Path  string `json:"path"`
}

type row struct {
	code   string
	hits   int // products carrying the code
	mapped bool
	name   string
}

// Harvest sources. Each is anchored on a structure that only occurs for
// parameters/filters, so category objects ({"id":"N","name":"X"}) never match.
var (
	// "parameters":[{"id":"11323","name":"Stan",... — product page params.
	paramsRe = regexp.MustCompile(`"parameters"\s*:\s*\[\s*\{\s*"id"\s*:\s*"(\d+)"\s*,\s*"name"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	// {"id":"248811","type":"MULTI","name":"Marka" — inner filter definition.
	filterDefRe = regexp.MustCompile(`\{\s*"id"\s*:\s*"(\d+)"\s*,\s*"type"\s*:\s*"[A-Z]+"\s*,\s*"name"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	// {"name":"Marka","id":"248811","filters":[ — outer filter group.
	filterGroupRe = regexp.MustCompile(`\{\s*"name"\s*:\s*"((?:[^"\\]|\\.)*)"\s*,\s*"id"\s*:\s*"(\d+)"\s*,\s*"filters"\s*:`)
	// data-slot-name="Marka" data-slot-id="248811" (either order) — sidebar HTML.
	slotNameFirstRe = regexp.MustCompile(`data-slot-name="([^"]+)"\s+data-slot-id="(\d+)"`)
	slotIdFirstRe   = regexp.MustCompile(`data-slot-id="(\d+)"\s+data-slot-name="([^"]+)"`)
)

func main() {
	feed := flag.String("feed", "", "feed JSON file")
	mapPath := flag.String("map", "docs/allegro_field_map.json", "field map JSON")
	top := flag.Int("top", 60, "show N unmapped codes")
	pagesDir := flag.String("pages", "", "harvest attribute names from saved Allegro HTML pages in DIR")
	write := flag.Bool("write", false, "with -pages: write merged map back to -map (dry-run without it)")
	plan := flag.Bool("plan", false, "with -feed: compute the minimal set of pages to save for full coverage")
	catsPath := flag.String("cats", "docs/allegro_categories.json", "category dump (id -> name/alias)")
	flag.Parse()

	if *pagesDir != "" {
		harvestPages(*pagesDir, *mapPath, *write)
		return
	}
	if *feed == "" {
		fmt.Println("usage: allegro-fields -feed <file.json> [-plan]  |  allegro-fields -pages <dir> [-write]")
		os.Exit(1)
	}

	if *plan {
		planCover(*feed, *mapPath, *catsPath)
		return
	}

	raw, err := os.ReadFile(*mapPath)
	if err != nil {
		fmt.Println("field map:", err)
		os.Exit(1)
	}
	var fm fieldMapFile
	_ = json.Unmarshal(raw, &fm)

	raw, err = os.ReadFile(*feed)
	if err != nil {
		fmt.Println("feed:", err)
		os.Exit(1)
	}
	var f feedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		fmt.Println("feed parse:", err)
		os.Exit(1)
	}

	rows := map[string]*row{}
	for _, p := range f.Products {
		seen := map[string]bool{}
		for _, fl := range p.Fields {
			n := fl.Name
			if !strings.HasPrefix(n, "attr_") || seen[n] {
				continue
			}
			seen[n] = true
			r := rows[n]
			if r == nil {
				r = &row{code: n}
				rows[n] = r
			}
			r.hits++
		}
	}

	list := make([]*row, 0, len(rows))
	for _, r := range rows {
		if name, ok := fm.Fields[r.code]; ok {
			r.mapped = true
			r.name = name
		}
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].hits > list[j].hits })

	mappedCount := 0
	coveredProducts := 0
	for _, r := range list {
		if r.mapped {
			mappedCount++
			coveredProducts += r.hits
		}
	}
	totalAttrOccurrences := 0
	for _, r := range list {
		totalAttrOccurrences += r.hits
	}

	fmt.Printf("feed: %s\n", *feed)
	fmt.Printf("products: %d, distinct attr codes: %d\n", len(f.Products), len(list))
	fmt.Printf("mapped codes: %d/%d, occurrence coverage: %d/%d (%.1f%%)\n\n",
		mappedCount, len(list), coveredProducts, totalAttrOccurrences,
		100*float64(coveredProducts)/float64(totalAttrOccurrences))

	fmt.Printf("TOP %d UNMAPPED (add to %s \"fields\"):\n", *top, *mapPath)
	shown := 0
	for _, r := range list {
		if r.mapped || shown >= *top {
			continue
		}
		shown++
		fmt.Printf("  %-14s %6d products\n", r.code, r.hits)
	}
}

// harvestPages extracts parameter id→name pairs from saved Allegro pages and
// merges them into the field map. Without -write it only reports what it found.
func harvestPages(dir, mapPath string, write bool) {
	// The map carries extra metadata (source/note/special/skip); keep it intact
	// by round-tripping everything except "fields".
	raw, err := os.ReadFile(mapPath)
	if err != nil {
		fmt.Println("field map:", err)
		os.Exit(1)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		fmt.Println("field map parse:", err)
		os.Exit(1)
	}
	var fm fieldMapFile
	_ = json.Unmarshal(doc["fields"], &fm.Fields)
	if fm.Fields == nil {
		fm.Fields = map[string]string{}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("pages dir:", err)
		os.Exit(1)
	}

	names := map[string]string{}         // attr id -> harvested name
	votes := map[string]map[string]int{} // attr id -> name -> count (conflict check)
	add := func(id, name string) {
		if name == "" {
			return
		}
		if unq, err := strconv.Unquote(`"` + name + `"`); err == nil {
			name = unq
		}
		if votes[id] == nil {
			votes[id] = map[string]int{}
		}
		votes[id][name]++
		if _, ok := names[id]; !ok {
			names[id] = name
		}
	}

	files := 0
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(strings.ToLower(e.Name()), ".html") && !strings.HasSuffix(strings.ToLower(e.Name()), ".htm")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			fmt.Println("skip", e.Name(), err)
			continue
		}
		files++
		s := string(data)
		for _, m := range paramsRe.FindAllStringSubmatch(s, -1) {
			add(m[1], m[2])
		}
		for _, m := range filterDefRe.FindAllStringSubmatch(s, -1) {
			add(m[1], m[2])
		}
		for _, m := range filterGroupRe.FindAllStringSubmatch(s, -1) {
			add(m[2], m[1])
		}
		for _, m := range slotNameFirstRe.FindAllStringSubmatch(s, -1) {
			add(m[2], m[1])
		}
		for _, m := range slotIdFirstRe.FindAllStringSubmatch(s, -1) {
			add(m[1], m[2])
		}
	}

	for id, tally := range votes {
		if len(tally) > 1 {
			fmt.Printf("CONFLICT attr_%s: %v — keeping first seen %q\n", id, tally, names[id])
		}
	}

	newCodes := make([]string, 0, len(names))
	for id := range names {
		code := "attr_" + id
		if _, ok := fm.Fields[code]; !ok {
			newCodes = append(newCodes, code)
		}
	}
	sort.Strings(newCodes)

	fmt.Printf("pages: %d, harvested ids: %d, new codes: %d (existing: %d)\n",
		files, len(names), len(newCodes), len(fm.Fields)-0)
	if len(newCodes) > 0 {
		fmt.Println("\nNEW codes:")
		for _, code := range newCodes {
			fmt.Printf("  %-16s %s\n", code, names[strings.TrimPrefix(code, "attr_")])
		}
	}

	if !write {
		if len(newCodes) > 0 {
			fmt.Printf("\ndry-run: re-run with -write to merge into %s\n", mapPath)
		}
		return
	}
	for _, code := range newCodes {
		fm.Fields[code] = names[strings.TrimPrefix(code, "attr_")]
	}
	fieldsRaw, err := json.MarshalIndent(fm.Fields, "", "  ")
	if err != nil {
		fmt.Println("marshal fields:", err)
		os.Exit(1)
	}
	doc["fields"] = fieldsRaw
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Println("marshal map:", err)
		os.Exit(1)
	}
	out = append(out, '\n')
	if err := os.WriteFile(mapPath, out, 0o644); err != nil {
		fmt.Println("write map:", err)
		os.Exit(1)
	}
	fmt.Printf("\nwrote %d new codes to %s (total fields: %d)\n", len(newCodes), mapPath, len(fm.Fields))
}

// feedProduct is one product of the feed reduced to what planning needs.
type feedProduct struct {
	name  string
	ean   string
	catID string
	codes []string // unmapped attr codes
}

// planCover computes the minimal set of Allegro pages to save so that every
// attr code of the feed gets a name and every feed category lands in the dump.
//
// Two savers work together:
//   - a category listing page yields the id→name of all its sidebar filters
//     (estimated coverage: the unmapped codes carried by that category's
//     products), plus the category itself and its breadcrumb ancestors;
//   - a product page yields its exact parameters and its own breadcrumbs —
//     the only way to reach categories absent from the dump (no alias → no URL)
//     and codes that never show up as listing filters.
//
// Attribute coverage is picked greedily (classic set cover): always take the
// category page that closes the most still-unmapped codes.
func planCover(feedPath, mapPath, catsPath string) {
	raw, err := os.ReadFile(mapPath)
	if err != nil {
		fmt.Println("field map:", err)
		os.Exit(1)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		fmt.Println("field map parse:", err)
		os.Exit(1)
	}
	var fm fieldMapFile
	_ = json.Unmarshal(doc["fields"], &fm.Fields)
	ignore := map[string]bool{}
	for _, key := range []string{"special", "skip"} {
		var list []string
		_ = json.Unmarshal(doc[key], &list)
		for _, s := range list {
			ignore[s] = true
		}
	}

	raw, err = os.ReadFile(feedPath)
	if err != nil {
		fmt.Println("feed:", err)
		os.Exit(1)
	}
	var f feedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		fmt.Println("feed parse:", err)
		os.Exit(1)
	}

	var cats map[string]catEntry
	if raw, err := os.ReadFile(catsPath); err == nil {
		_ = json.Unmarshal(raw, &cats)
	}

	catAttrs := map[string]map[string]bool{} // catID -> unmapped codes
	catProds := map[string]int{}
	prods := []feedProduct{}
	for _, p := range f.Products {
		fp := feedProduct{name: p.Name}
		for _, fl := range p.Fields {
			switch {
			case fl.Name == "category_id" && fl.Value != "":
				fp.catID = fl.Value
			case fl.Name == "gtin" && fl.Value != "":
				fp.ean = fl.Value
			case strings.HasPrefix(fl.Name, "attr_"):
				if _, mapped := fm.Fields[fl.Name]; mapped || ignore[fl.Name] {
					continue
				}
				fp.codes = append(fp.codes, fl.Name)
			}
		}
		if len(fp.codes) == 0 && fp.catID == "" {
			continue
		}
		prods = append(prods, fp)
		if fp.catID != "" {
			catProds[fp.catID]++
			if catAttrs[fp.catID] == nil {
				catAttrs[fp.catID] = map[string]bool{}
			}
			for _, c := range fp.codes {
				catAttrs[fp.catID][c] = true
			}
		}
	}

	// A parent listing page shows the union of its whole subtree's filters, so
	// every ancestor of a known feed category is a save candidate too. Unknown
	// feed categories can't be placed in the tree yet — their codes form a
	// separate bucket that shrinks as saved pages grow the dump (re-plan).
	full := func(e catEntry) string {
		if e.Path != "" {
			return e.Path + " > " + e.Name
		}
		return e.Name
	}
	byFull := map[string]string{} // fullPath -> catID
	for id, e := range cats {
		if e.Name != "" {
			byFull[full(e)] = id
		}
	}

	type cand struct {
		id       string
		entry    catEntry
		feedCats int // feed categories covered (self + descendants)
		prodsN   int
		codes    map[string]bool
	}
	cands := map[string]*cand{}
	addCand := func(id string, e catEntry, prods int, codes map[string]bool) *cand {
		c := cands[id]
		if c == nil {
			c = &cand{id: id, entry: e, codes: map[string]bool{}}
			cands[id] = c
		}
		c.feedCats++
		c.prodsN += prods
		for code := range codes {
			c.codes[code] = true
		}
		return c
	}
	for catID, codes := range catAttrs {
		e, known := cats[catID]
		if !known {
			continue // unknown feed category — unattributed bucket
		}
		addCand(catID, e, catProds[catID], codes)
		// credit every resolvable ancestor with this category's codes
		f := full(e)
		for {
			idx := strings.LastIndex(f, " > ")
			if idx < 0 {
				break
			}
			f = f[:idx]
			if ancID, ok := byFull[f]; ok {
				addCand(ancID, cats[ancID], catProds[catID], codes)
			}
		}
	}

	universe := map[string]bool{} // codes attributable through the tree
	for _, c := range cands {
		for code := range c.codes {
			universe[code] = true
		}
	}
	unattributed := map[string]bool{} // codes only unknown categories carry
	for catID, codes := range catAttrs {
		if _, known := cats[catID]; known {
			continue
		}
		for code := range codes {
			if !universe[code] {
				unattributed[code] = true
			}
		}
	}

	type pick struct {
		c    *cand
		gain int
	}
	var picks []pick
	uncovered := make(map[string]bool, len(universe))
	for code := range universe {
		uncovered[code] = true
	}
	for len(uncovered) > 0 {
		var bestP *pick
		for _, c := range cands {
			dup := false
			for _, p := range picks {
				if p.c.id == c.id {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			gain := 0
			for code := range c.codes {
				if uncovered[code] {
					gain++
				}
			}
			if gain == 0 {
				continue
			}
			if bestP == nil || gain > bestP.gain ||
				(gain == bestP.gain && c.prodsN > bestP.c.prodsN) ||
				(gain == bestP.gain && c.prodsN == bestP.c.prodsN && c.id < bestP.c.id) {
				bestP = &pick{c: c, gain: gain}
			}
		}
		if bestP == nil {
			break
		}
		picks = append(picks, *bestP)
		for code := range bestP.c.codes {
			delete(uncovered, code)
		}
	}

	fmt.Printf("feed: %s\n", feedPath)
	fmt.Printf("products with unmapped codes: %d/%d, unmapped codes: %d (attributable: %d, only-unknown-cats: %d), categories in feed: %d\n\n",
		len(prods), len(f.Products), len(universe)+len(unattributed), len(universe), len(unattributed), len(catAttrs))

	fmt.Printf("SAVE LIST — category pages (greedy set cover, %d pages for %d attributable codes):\n", len(picks), len(universe))
	cov := 0
	for i, p := range picks {
		cov += p.gain
		url := ""
		if p.c.entry.Alias != "" {
			alias := strings.SplitN(p.c.entry.Alias, "?", 2)[0]
			url = "https://allegro.pl/kategoria/" + alias
		}
		fmt.Printf("%3d. %-50s cats:%-4d products:%-6d new codes:%-4d covered:%.1f%%\n",
			i+1, p.c.entry.Name+" ("+p.c.id+")", p.c.feedCats, p.c.prodsN, p.gain, 100*float64(cov)/float64(len(universe)))
		if url != "" {
			fmt.Printf("     %s\n", url)
		} else {
			fmt.Printf("     (no alias in dump)\n")
		}
	}

	// --- feed categories missing from the dump ------------------------------
	var unknown []string
	unknownProds := 0
	for catID := range catProds {
		if e, ok := cats[catID]; !ok || e.Alias == "" {
			unknown = append(unknown, catID)
			unknownProds += catProds[catID]
		}
	}
	sort.Slice(unknown, func(i, j int) bool { return catProds[unknown[i]] > catProds[unknown[j]] })
	if len(unknown) > 0 {
		fmt.Printf("\nUNKNOWN categories: %d (%d products) not in %s.\n", len(unknown), unknownProds, catsPath)
		fmt.Println("Saving the pages above also lands many of them in the dump (allegro-cats picks up child links); re-plan after each batch.")
		fmt.Println("For the rest, open one product of the category — its breadcrumbs add the category, its parameters add attr names:")
		shown := 0
		for _, catID := range unknown {
			if shown >= 30 {
				fmt.Printf("  ... and %d more\n", len(unknown)-shown)
				break
			}
			var prod *feedProduct
			bestN := -1
			for i := range prods {
				if prods[i].catID != catID {
					continue
				}
				if len(prods[i].codes) > bestN {
					bestN = len(prods[i].codes)
					prod = &prods[i]
				}
			}
			shown++
			if prod != nil {
				ean := ""
				if prod.ean != "" {
					ean = ", EAN " + prod.ean
				}
				fmt.Printf("  %s (%d products): %q%s\n", catID, catProds[catID], prod.name, ean)
			} else {
				fmt.Printf("  %s (%d products)\n", catID, catProds[catID])
			}
		}
	}

	if len(uncovered) > 0 {
		fmt.Printf("\nRESIDUAL attributable codes with no category page: %d — need product pages.\n", len(uncovered))
	} else {
		fmt.Printf("\nAll %d attributable codes are covered by the save list above.\n", len(universe))
	}

	// --- discovery hints: parent slugs ---------------------------------------
	// The save list is all leaves because the dump lacks mid-level entries.
	// Every leaf alias encodes its parent's slug (…-{name-slug}-{id}), so the
	// likely parent of many picks can be named even without its id. Saving one
	// parent page usually replaces a whole cluster of leaf pages.
	type hint struct {
		slug  string
		pages int
		codes int
	}
	hints := map[string]*hint{}
	for _, p := range picks {
		a := strings.SplitN(p.c.entry.Alias, "?", 2)[0]
		suffix := "-" + slugify(p.c.entry.Name) + "-" + p.c.id
		if !strings.HasSuffix(a, suffix) {
			continue
		}
		parent := strings.TrimSuffix(a, suffix)
		if parent == "" {
			continue
		}
		h := hints[parent]
		if h == nil {
			h = &hint{slug: parent}
			hints[parent] = h
		}
		h.pages++
		h.codes += p.gain
	}
	hlist := make([]*hint, 0, len(hints))
	for _, h := range hints {
		if h.pages >= 2 {
			hlist = append(hlist, h)
		}
	}
	sort.Slice(hlist, func(i, j int) bool {
		if hlist[i].codes != hlist[j].codes {
			return hlist[i].codes > hlist[j].codes
		}
		return hlist[i].pages > hlist[j].pages
	})
	if len(hlist) > 0 {
		fmt.Printf("\nDISCOVERY hints — probable parent pages (slug derived from child aliases).\n")
		fmt.Printf("One parent usually covers its whole cluster; try these FIRST, in the browser:\n")
		for i, h := range hlist {
			if i >= 15 {
				fmt.Printf("  ... and %d more\n", len(hlist)-15)
				break
			}
			fmt.Printf("%3d. https://allegro.pl/kategoria/%-40s replaces ~%d pages, ~%d codes\n",
				i+1, h.slug, h.pages, h.codes)
		}
		fmt.Println("  (if a slug URL doesn't resolve, paste it into the Allegro search box)")
	}

	// Codes carried only by unknown categories: product pages are the direct fix.
	type prodPick struct {
		idx  int
		gain int
	}
	var prodPicks []prodPick
	{
		tmp := make([]prodPick, 0, len(prods))
		seenName := map[string]bool{}
		for i := range prods {
			gain := 0
			for _, c := range prods[i].codes {
				if unattributed[c] || uncovered[c] {
					gain++
				}
			}
			if gain == 0 || seenName[prods[i].name] {
				continue
			}
			seenName[prods[i].name] = true
			tmp = append(tmp, prodPick{idx: i, gain: gain})
		}
		sort.Slice(tmp, func(i, j int) bool { return tmp[i].gain > tmp[j].gain })
		prodPicks = tmp
	}
	if len(prodPicks) > 0 {
		fmt.Printf("\nTOP product pages by uncovered-code richness (fallback for non-filterable params):\n")
		for i, pp := range prodPicks {
			if i >= 10 {
				fmt.Printf("  ... and %d more products\n", len(prodPicks)-10)
				break
			}
			p := prods[pp.idx]
			ean := ""
			if p.ean != "" {
				ean = ", EAN " + p.ean
			}
			fmt.Printf("%3d. %d codes  %q%s\n", i+1, pp.gain, p.name, ean)
		}
	}

	fmt.Println("\nWorkflow: save the pages into a dir, then")
	fmt.Printf("  allegro-fields -pages <dir> -write   # merge new names\n")
	fmt.Printf("  allegro-cats -dir <dir>              # merge new categories\n")
	fmt.Printf("  allegro-fields -feed %s -plan        # re-plan with updated map+dump\n", feedPath)
}

// slugify transliterates a Polish category name the way Allegro builds URL
// slugs: lowercase, diacritics folded, everything non-alphanumeric → '-'.
func slugify(name string) string {
	repl := strings.NewReplacer(
		"ą", "a", "ć", "c", "ę", "e", "ł", "l", "ń", "n",
		"ó", "o", "ś", "s", "ź", "z", "ż", "z",
	)
	s := strings.ToLower(repl.Replace(name))
	var b strings.Builder
	prevDash := true // trim leading dashes
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
