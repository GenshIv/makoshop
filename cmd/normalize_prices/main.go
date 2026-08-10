package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GenshIv/makoshop/internal/attrs"
)

type Category struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ParentID *int64 `json:"parent_id,omitempty"`
	Path     string `json:"path"`
}

type Brand struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ProductRow struct {
	SKU         string                 `json:"sku"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	CategoryID  int64                  `json:"category_id"`
	BrandID     int64                  `json:"brand_id"`
	Brand       string                 `json:"brand"`
	Price       float64                `json:"price"`
	StockQty    int64                  `json:"stock_qty"`
	Images      []string               `json:"images,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
}

func main() {
	inputDir := flag.String("input", "_tmp/prices", "Input directory with CSV files")
	outputDir := flag.String("output", "_tmp/normalized", "Output directory for normalized data")
	workers := flag.Int("workers", 16, "Number of parallel workers")
	flag.Parse()

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output dir: %v\n", err)
		os.Exit(1)
	}

	csvFiles, err := filepath.Glob(filepath.Join(*inputDir, "*.csv"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to glob CSV files: %v\n", err)
		os.Exit(1)
	}
	sort.Strings(csvFiles)

	if len(csvFiles) == 0 {
		fmt.Println("No CSV files found")
		os.Exit(0)
	}

	// Global state for categories and brands
	var catMu sync.Mutex
	var brandMu sync.Mutex
	pathToCatID := make(map[string]int64)
	brandToID := make(map[string]int64)
	var nextCatID int64 = 1
	var nextBrandID int64 = 1

	// Results
	type fileResult struct {
		file       string
		categories int
		brands     int
		products   int
		skipped    int
		err        error
	}

	resultsCh := make(chan fileResult, len(csvFiles))
	var wg sync.WaitGroup
	sem := make(chan struct{}, *workers)

	var totalProducts int64
	var totalSkipped int64

	startTime := time.Now()

	for _, csvFile := range csvFiles {
		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := processFile(file, &catMu, &brandMu, &pathToCatID, &brandToID, &nextCatID, &nextBrandID, *outputDir)
			resultsCh <- fileResult{
				file:       file,
				categories: res.categories,
				brands:     res.brands,
				products:   res.products,
				skipped:    res.skipped,
				err:        err,
			}
			atomic.AddInt64(&totalProducts, int64(res.products))
			atomic.AddInt64(&totalSkipped, int64(res.skipped))
		}(csvFile)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect categories and brands
	var allCategories []*Category
	var allBrands []*Brand

	for res := range resultsCh {
		if res.err != nil {
			fmt.Printf("WARN: file %s error: %v\n", res.file, res.err)
			continue
		}
		fmt.Printf("File %s: cats=%d brands=%d products=%d skipped=%d\n",
			filepath.Base(res.file), res.categories, res.brands, res.products, res.skipped)
	}

	fmt.Printf("Building %d categories from pathToCatID\n", len(pathToCatID))
	// Build final category list from pathToCatID
	catMu.Lock()
	for path, id := range pathToCatID {
		parts := strings.Split(path, " -> ")
		var parentID *int64
		if len(parts) > 1 {
			parentPath := strings.Join(parts[:len(parts)-1], " -> ")
			if pid, ok := pathToCatID[parentPath]; ok {
				parentID = &pid
			}
		}
		allCategories = append(allCategories, &Category{
			ID:       id,
			Name:     parts[len(parts)-1],
			ParentID: parentID,
			Path:     path,
		})
	}
	fmt.Printf("Sorted %d categories\n", len(allCategories))
	// Sort by path for consistent order
	sort.Slice(allCategories, func(i, j int) bool {
		return allCategories[i].Path < allCategories[j].Path
	})
	catMu.Unlock()

	// Build final brand list
	brandMu.Lock()
	for name, id := range brandToID {
		allBrands = append(allBrands, &Brand{ID: id, Name: name})
	}
	brandMu.Unlock()

	fmt.Printf("Writing categories.jsonl with %d entries\n", len(allCategories))
	// Write categories.jsonl
	catFile, err := os.Create(filepath.Join(*outputDir, "categories.jsonl"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create categories file: %v\n", err)
		os.Exit(1)
	}
	for _, cat := range allCategories {
		data, _ := json.Marshal(cat)
		catFile.Write(data)
		catFile.Write([]byte("\n"))
	}
	catFile.Close()
	fmt.Printf("Wrote categories.jsonl\n")

	fmt.Printf("Writing brands.jsonl with %d entries\n", len(allBrands))
	// Write brands.jsonl
	brandFile, err := os.Create(filepath.Join(*outputDir, "brands.jsonl"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create brands file: %v\n", err)
		os.Exit(1)
	}
	for _, brand := range allBrands {
		data, _ := json.Marshal(brand)
		brandFile.Write(data)
		brandFile.Write([]byte("\n"))
	}
	brandFile.Close()
	fmt.Printf("Wrote brands.jsonl\n")

	elapsed := time.Since(startTime)
	fmt.Printf("\nTotal: products=%d skipped=%d categories=%d brands=%d\n", totalProducts, totalSkipped, len(allCategories), len(allBrands))
	fmt.Printf("Time: %v\n", elapsed)
	fmt.Printf("Output: %s\n", *outputDir)
}

type fileStats struct {
	categories int
	brands     int
	products   int
	skipped    int
}

func processFile(
	csvFile string,
	catMu, brandMu *sync.Mutex,
	pathToCatID *map[string]int64,
	brandToID *map[string]int64,
	nextCatID, nextBrandID *int64,
	outputDir string,
) (fileStats, error) {
	f, err := os.Open(csvFile)
	if err != nil {
		return fileStats{}, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return fileStats{}, fmt.Errorf("read header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	baseName := strings.TrimSuffix(filepath.Base(csvFile), ".csv")
	outFile, err := os.Create(filepath.Join(outputDir, "products-"+baseName+".jsonl"))
	if err != nil {
		return fileStats{}, fmt.Errorf("create output file: %w", err)
	}
	defer outFile.Close()

	var stats fileStats

	get := func(row []string, col string) string {
		idx, ok := colIndex[col]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			stats.skipped++
			continue
		}

		var catParts []string
		for _, col := range []string{"Категория", "Подкатегория 2", "Подкатегория 3", "Подкатегория 4"} {
			val := get(row, col)
			if val != "" {
				catParts = append(catParts, val)
			}
		}
		if len(catParts) == 0 {
			stats.skipped++
			continue
		}

		var catID int64
		for i := 0; i < len(catParts); i++ {
			path := strings.Join(catParts[:i+1], " -> ")
			catMu.Lock()
			if id, ok := (*pathToCatID)[path]; ok {
				if i == len(catParts)-1 {
					catID = id
				}
				catMu.Unlock()
				continue
			}

			newID := atomic.AddInt64(nextCatID, 1) - 1
			(*pathToCatID)[path] = newID
			stats.categories++

			if i == len(catParts)-1 {
				catID = newID
			}

			catMu.Unlock()
		}

		sku := get(row, "Артикул")
		modSku := get(row, "Артикул модификации")
		if sku == "" {
			stats.skipped++
			continue
		}
		uniqueSku := modSku
		if uniqueSku == "" {
			uniqueSku = sku
		}

		name := get(row, "Имя товара")
		if name == "" {
			stats.skipped++
			continue
		}

		price := parsePrice(get(row, "Цена"))
		if price <= 0 {
			stats.skipped++
			continue
		}

		brand := get(row, "Производитель")
		var brandID int64
		if brand != "" {
			brandMu.Lock()
			if id, ok := (*brandToID)[brand]; ok {
				brandID = id
			} else {
				newID := atomic.AddInt64(nextBrandID, 1) - 1
				(*brandToID)[brand] = newID
				brandID = newID
				stats.brands++
			}
			brandMu.Unlock()
		}

		description := get(row, "Краткое описание")
		if description == "" {
			description = get(row, "Описание")
		}
		if len(description) > 2000 {
			description = description[:2000]
		}

		images := parseImages(get(row, "Ссылки на фото (через пробел)"))

		htmlAttrs := get(row, "Характеристики (HTML/Table)")
		parsedAttrs := attrs.ParseTable(htmlAttrs)
		attrMap := make(map[string]interface{})
		for code, values := range parsedAttrs {
			if len(values) > 0 {
				attrMap[code] = values[0]
			}
		}

		stockQty := parseStockQty(get(row, "Количество"))

		product := ProductRow{
			SKU:         uniqueSku,
			Name:        name,
			Description: description,
			CategoryID:  catID,
			BrandID:     brandID,
			Brand:       brand,
			Price:       price,
			StockQty:    stockQty,
			Images:      images,
			Attributes:  attrMap,
		}

		data, err := json.Marshal(product)
		if err != nil {
			stats.skipped++
			continue
		}
		outFile.Write(data)
		outFile.Write([]byte("\n"))
		stats.products++
	}

	return stats, nil
}

func parsePrice(s string) float64 {
	// First: remove ALL whitespace (spaces, tabs, newlines, non-breaking spaces)
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\u00a0' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if s == "" {
		return 0
	}

	// Determine decimal separator
	// If both . and , present: last one is decimal separator
	lastDot := strings.LastIndex(s, ".")
	lastComma := strings.LastIndex(s, ",")

	if lastDot >= 0 && lastComma >= 0 {
		if lastDot > lastComma {
			// . is decimal separator, , is thousands separator
			s = strings.ReplaceAll(s, ",", "")
		} else {
			// , is decimal separator, . is thousands separator
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		}
	} else if lastComma >= 0 {
		// Only comma: could be decimal (1,5) or thousands (1,000)
		// If digits after comma <= 2, treat as decimal separator
		afterComma := s[lastComma+1:]
		if len(afterComma) <= 2 {
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			// Thousands separator
			s = strings.ReplaceAll(s, ",", "")
		}
	}
	// If only dot: it's decimal separator, nothing to do

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseImages(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	var images []string
	for _, p := range parts {
		if strings.HasPrefix(p, "http") && strings.Contains(p, "mzimg.com") {
			images = append(images, p)
		}
	}
	if len(images) == 0 {
		return nil
	}
	return images
}

func parseStockQty(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var buf strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '-' {
			buf.WriteRune(r)
		}
	}
	f, err := strconv.ParseFloat(buf.String(), 64)
	if err != nil {
		return 0
	}
	return int64(f)
}
