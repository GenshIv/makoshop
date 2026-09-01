package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/pkg/config"
)

// diag_formats opens the store and prints, for every company, the saved
// PriceSource.Format next to the actual file type inferred from ImportURL, so
// we can see where the saved format disagrees with the real data.
func main() {
	path := "makoshop_db"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	cfg := config.DatabaseConfig{
		Path:               path,
		NumShards:          16,
		MaxTotalSize:       40 * 1024 * 1024 * 1024,
		NumBucketsPerShard: 5_000_000,
	}
	store, err := db.NewStore(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewStore: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	repo := db.NewCompanyRepo(store)
	companies, err := repo.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "List: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-4s %-28s %-10s %-10s %-8s %s\n", "ID", "NAME", "SAVED_FMT", "URL_EXT", "MATCH", "IMPORT_URL")
	fmt.Println(strings.Repeat("-", 110))

	mismatch := 0
	for _, c := range companies {
		saved := c.PriceSource.Format
		if saved == "" {
			saved = "(empty->nokaut)"
		}
		urlExt := "-"
		actual := ""
		if c.ImportURL != "" {
			if u, uerr := url.Parse(c.ImportURL); uerr == nil {
				urlExt = strings.TrimPrefix(filepath.Ext(u.Path), ".")
				switch strings.ToLower(filepath.Ext(u.Path)) {
				case ".json":
					actual = "json"
				case ".xml":
					actual = "nokaut"
				}
			}
		}
		match := "?"
		if actual != "" {
			eff := strings.ToLower(strings.TrimSpace(c.PriceSource.Format))
			if eff == "" {
				eff = "nokaut"
			}
			if eff == "xml" {
				eff = "nokaut"
			}
			if eff == actual {
				match = "OK"
			} else {
				match = "MISMATCH"
				mismatch++
			}
		}
		fmt.Printf("%-4d %-28s %-10s %-10s %-8s %s\n",
			c.ID, truncate(c.Name, 28), saved, urlExt, match, truncate(c.ImportURL, 60))
	}
	fmt.Println(strings.Repeat("-", 110))
	fmt.Printf("Total companies: %d, format mismatches: %d\n", len(companies), mismatch)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
