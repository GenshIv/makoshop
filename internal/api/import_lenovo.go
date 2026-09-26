package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

const lenovoCatalogPath = "/home/ihar/IdeaProjects/makoshop/lenovo-psref-catalog.json"

// LenovoCatalog represents the PSREF catalog structure
type LenovoCatalog struct {
	Metadata   map[string]interface{}   `json:"metadata"`
	Categories []map[string]interface{} `json:"categories"`
	Models     []LenovoModel            `json:"models"`
}

// LenovoModel represents a single product model
type LenovoModel struct {
	Category      string                 `json:"category"`
	Subcategory   string                 `json:"subcategory"`
	CategoryID    int64                  `json:"categoryId"`
	SubcategoryID int64                  `json:"subcategoryId"`
	ProductID     float64                `json:"productid"`
	ProductKey    string                 `json:"product_key"`
	ProductName   string                 `json:"product_name"`
	IsNew         bool                   `json:"is_new"`
	ModelCount    int                    `json:"product_model_count"`
	Photos        []LenovoPhoto          `json:"photos"`
	Highlights    []string               `json:"highlight_array"`
	Cols          []string               `json:"cols"`
	Rows          [][]interface{}        `json:"rows"`
	DetailedSpecs map[string]interface{} `json:"detailedSpecs"`
}

// LenovoPhoto represents a product photo
type LenovoPhoto struct {
	Src string `json:"src"`
}

// HandleAdminImportLenovo imports/upserts EAN pages from Lenovo PSREF catalog.
// POST /admin/import-lenovo-eans?file=/path/to/catalog.json
// If file param is not provided, uses default path.
func (h *Handlers) HandleAdminImportLenovo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Prevent concurrent imports
	if !h.importMu.TryLock() {
		httpres.WriteError(w, http.StatusConflict, "IMPORT_IN_PROGRESS", "Another import is already in progress")
		return
	}

	fmt.Printf("[IMPORT-LENOVO] Starting from %s\n", r.RemoteAddr)

	// Determine catalog file path
	catalogPath := lenovoCatalogPath
	if customPath := r.URL.Query().Get("file"); customPath != "" {
		catalogPath = customPath
	}

	// Load catalog
	catalog, err := loadLenovoCatalogFrom(catalogPath)
	if err != nil {
		h.importMu.Unlock()
		httpres.WriteError(w, http.StatusInternalServerError, "LOAD_ERROR", err.Error())
		return
	}

	fmt.Printf("[IMPORT-LENOVO] Loaded %d models\n", len(catalog.Models))

	// Start progress tracking
	h.importProgress.Begin(len(catalog.Models))

	// Run import in background
	go func() {
		defer h.importMu.Unlock()
		defer h.importProgress.Finish()

		// Disable auto-vacuum during import
		if h.store != nil {
			h.store.DB().SetAutoVacuum(false)
			defer func() {
				h.store.DB().SetAutoVacuum(true)
				h.compactInBackground()
			}()
		}

		startTime := time.Now()
		totalUpserted := 0
		totalErrors := 0
		newEANPageKeys := make([]string, 0)
		pagesToIndex := make([]*model.EANPage, 0)
		allSlugMap := make(map[string]string)

		for i, modelData := range catalog.Models {
			// Check shard health every 10 models and compact if needed
			h.checkAndCompactShards()

			// Update progress - treat each model as a "company"
			h.importProgress.SetCompany(i+1, modelData.ProductName, "lenovo-psref")

			// Process each variant (row) of this model
			upserted, errors, newKeys, pages, slugMap := h.processLenovoModel(&modelData)
			totalUpserted += upserted
			totalErrors += errors
			newEANPageKeys = append(newEANPageKeys, newKeys...)
			pagesToIndex = append(pagesToIndex, pages...)
			for ean, slug := range slugMap {
				allSlugMap[ean] = slug
			}

			// Mark company as completed
			h.importProgress.CompanyDone(i+1, "completed", "")
		}

		// Batch-add all new EAN pages to the list index (avoids 140K individual writes)
		if len(newEANPageKeys) > 0 {
			fmt.Printf("[IMPORT-LENOVO] Adding %d new EAN pages to list index...\n", len(newEANPageKeys))
			if _, err := h.store.DB().TurboPutBatchIndexString("eanpage_list", newEANPageKeys); err != nil {
				fmt.Printf("[IMPORT-LENOVO] WARN: batch add to eanpage_list failed: %v\n", err)
			} else {
				fmt.Printf("[IMPORT-LENOVO] List index updated with %d new pages\n", len(newEANPageKeys))
			}
		}

		// Write slug indexes for all new/updated pages (required for URL lookup)
		if len(allSlugMap) > 0 {
			fmt.Printf("[IMPORT-LENOVO] Writing slug indexes for %d pages...\n", len(allSlugMap))
			start := time.Now()
			slugErrors := 0
			for ean, slug := range allSlugMap {
				slugKey := "eanpage_slug:" + slug
				if err := h.store.TurboWrite(slugKey, []byte("eanpage:"+ean)); err != nil {
					slugErrors++
				}
			}
			if slugErrors > 0 {
				fmt.Printf("[IMPORT-LENOVO] WARN: %d slug index writes failed\n", slugErrors)
			} else {
				fmt.Printf("[IMPORT-LENOVO] Slug indexes written in %v\n", time.Since(start))
			}
		}

		// Index all new/updated pages for search (like price import does)
		if len(pagesToIndex) > 0 && h.eanPageSearch != nil {
			fmt.Printf("[IMPORT-LENOVO] Indexing %d EAN pages for search...\n", len(pagesToIndex))
			start := time.Now()
			if err := h.eanPageSearch.IndexEANPageBatch(pagesToIndex); err != nil {
				fmt.Printf("[IMPORT-LENOVO] WARN: batch indexing failed: %v\n", err)
			} else {
				fmt.Printf("[IMPORT-LENOVO] Search indexing completed in %v\n", time.Since(start))
			}
		}

		// Rebuild indexes after all imports (like price import does)
		if h.eanPageSearch != nil {
			start := time.Now()
			if err := h.eanPageSearch.BuildSortIndexes(); err != nil {
				fmt.Printf("[IMPORT-LENOVO] WARN: build EAN page sort indexes failed: %v\n", err)
			} else {
				fmt.Printf("[IMPORT-LENOVO] EAN sort indexes rebuilt in %v\n", time.Since(start))
			}
		}

		duration := time.Since(startTime)
		fmt.Printf("[IMPORT-LENOVO] Completed in %s: %d EAN pages upserted, %d errors\n",
			duration, totalUpserted, totalErrors)
	}()

	httpres.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"status": "started",
	})
}

// loadLenovoCatalogFrom loads the PSREF catalog from a specified file path
func loadLenovoCatalogFrom(path string) (*LenovoCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read catalog from %s: %w", path, err)
	}

	var catalog LenovoCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("failed to parse catalog: %w", err)
	}

	return &catalog, nil
}

// processLenovoModel processes all variants of a model and upserts EAN pages
func (h *Handlers) processLenovoModel(modelData *LenovoModel) (int, int, []string, []*model.EANPage, map[string]string) {
	upserted := 0
	errors := 0
	newKeys := make([]string, 0)
	pages := make([]*model.EANPage, 0)
	slugMap := make(map[string]string) // ean -> slug

	// Get detailed specs
	details := modelData.DetailedSpecs
	if details == nil {
		return 0, 0, newKeys, pages, slugMap
	}

	rows, ok := details["rows"].([]interface{})
	if !ok {
		return 0, 0, newKeys, pages, slugMap
	}

	cols, ok := details["cols"].([]interface{})
	if !ok {
		return 0, 0, newKeys, pages, slugMap
	}

	// Find EAN column index
	eanIdx := -1
	for i, col := range cols {
		colStr, _ := col.(string)
		if strings.Contains(colStr, "EAN") || strings.Contains(colStr, "UPC") || strings.Contains(colStr, "JAN") {
			eanIdx = i
			break
		}
	}

	if eanIdx == -1 {
		return 0, 0, newKeys, pages, slugMap
	}

	// Process each variant row
	for _, row := range rows {
		rowArr, ok := row.([]interface{})
		if !ok || len(rowArr) <= eanIdx {
			continue
		}

		// Extract EAN
		eanRaw, _ := rowArr[eanIdx].(string)
		ean := extractEAN(eanRaw)
		if ean == "" {
			continue
		}

		// Build specs map
		specs := make(map[string]string)
		for i, col := range cols {
			colStr, _ := col.(string)
			if i < len(rowArr) && rowArr[i] != nil {
				val, _ := rowArr[i].(string)
				specs[colStr] = val
			}
		}

		// Generate content
		title := generateLenovoTitle(modelData.ProductName, specs)
		description := generateLenovoDescription(modelData.ProductName, specs, modelData.Highlights)
		attributes := specsToAttributes(specs)
		images := getPhotos(modelData.Photos)

		// Determine category
		categoryID := getLenovoCategoryID(modelData.Category)

		// Upsert EAN page (no per-page indexing to avoid vacuum bloat)
		page, created, err := h.upsertLenovoEANPage(ean, title, description, attributes, images, categoryID, specs)
		if err != nil {
			fmt.Printf("[IMPORT-LENOVO] Error upserting %s: %v\n", ean, err)
			errors++
		} else {
			upserted++
			if created {
				newKeys = append(newKeys, "eanpage:"+ean)
			}
			pages = append(pages, page)
			slugMap[ean] = page.Slug
		}
	}

	return upserted, errors, newKeys, pages, slugMap
}

// upsertLenovoEANPage creates or updates an EAN page
func (h *Handlers) upsertLenovoEANPage(ean, title, description string, attributes []model.KeyValue, images []string, categoryID int64, specs map[string]string) (*model.EANPage, bool, error) {
	// Generate unique slug with EAN and key specs
	slug := generateLenovoSlug(title, ean, specs)

	// Build seo_url from category path + product slug
	var seoURL string
	if treePath, err := h.categoryRepo.GetTreePath(categoryID); err == nil && len(treePath) > 0 {
		seoURL = "/shop/" + strings.Join(treePath, "/") + "/" + slug
	} else {
		seoURL = "/shop/" + slug
	}

	// Check if page exists
	existing, err := h.eanPageRepo.Get(ean)
	if err != nil || existing == nil {
		// Create new EAN page
		page := &model.EANPage{
			EAN:         ean,
			Title:       title,
			Description: description[:min(500, len(description))],
			Content:     description,
			Slug:        slug,
			Brand:       "Lenovo",
			Images:      images,
			CategoryID:  categoryID,
			IsActive:    true,
			Currency:    "PLN",
			Attributes:  attributes,
			SeoURL:      seoURL,
		}

		// Use CreateNoListIndex to avoid updating eanpage_list index 140K times
		if err := h.eanPageRepo.CreateNoListIndex(page); err != nil {
			return nil, false, fmt.Errorf("create eanpage: %w", err)
		}

		return page, true, nil
	}

	// Update existing page
	err = h.eanPageRepo.Update(ean, func(sp *model.EANPage) {
		sp.Title = title
		sp.Description = description[:min(500, len(description))]
		sp.Content = description
		sp.Slug = slug
		sp.Brand = "Lenovo"
		sp.Images = images
		sp.CategoryID = categoryID
		sp.IsActive = true
		sp.Attributes = attributes
		sp.SeoURL = seoURL
	})

	if err != nil {
		return nil, false, fmt.Errorf("update eanpage: %w", err)
	}

	// Return updated page for indexing
	updated, _ := h.eanPageRepo.Get(ean)
	return updated, false, nil
}

// extractEAN extracts a valid EAN code from raw string
func extractEAN(raw string) string {
	re := regexp.MustCompile(`(\d{12,14})`)
	match := re.FindStringSubmatch(raw)
	if match != nil {
		return match[1]
	}
	return ""
}

// getPhotos extracts all photo URLs from model photos
func getPhotos(photos []LenovoPhoto) []string {
	urls := make([]string, 0, len(photos))
	for _, p := range photos {
		if p.Src != "" {
			urls = append(urls, p.Src)
		}
	}
	return urls
}

// getLenovoCategoryID maps Lenovo category to Makoshop category ID
func getLenovoCategoryID(category string) int64 {
	switch {
	case strings.Contains(strings.ToLower(category), "laptop"):
		return 20 // Laptopy
	case strings.Contains(strings.ToLower(category), "tablet"):
		return 21 // Tablety
	case strings.Contains(strings.ToLower(category), "desktop") || strings.Contains(strings.ToLower(category), "aio"):
		return 390 // Komputery stacjonarne
	default:
		return 20 // Default to laptops
	}
}

// generateLenovoTitle creates an SEO title for the product
func generateLenovoTitle(productName string, specs map[string]string) string {
	parts := []string{productName}

	if proc := specs["Processor"]; proc != "" {
		// Shorten processor name
		shortProc := strings.Split(proc, ",")[0]
		if len(shortProc) > 40 {
			shortProc = shortProc[:40]
		}
		parts = append(parts, shortProc)
	}

	if mem := specs["Memory"]; mem != "" {
		re := regexp.MustCompile(`([0-9]+GB)`)
		match := re.FindString(mem)
		if match != "" {
			parts = append(parts, match)
		}
	}

	if storage := specs["Storage"]; storage != "" {
		re := regexp.MustCompile(`([0-9]+[GT]B)`)
		match := re.FindString(storage)
		if match != "" {
			parts = append(parts, match)
		}
	}

	return strings.Join(parts, " | ")
}

// generateLenovoDescription creates a unique Polish description
func generateLenovoDescription(productName string, specs map[string]string, highlights []string) string {
	var sb strings.Builder

	processor := specs["Processor"]
	memory := specs["Memory"]
	storage := specs["Storage"]
	display := specs["Display"]
	graphics := specs["Graphics"]
	battery := specs["Battery"]
	weight := specs["Weight"]
	wifi := specs["WLAN + Bluetooth"]

	// Opening paragraph based on product type
	switch {
	case strings.Contains(productName, "ThinkPad") || strings.Contains(productName, "ThinkCentre"):
		sb.WriteString(fmt.Sprintf("%s to profesjonalne urządzenie stworzone dla wymagających użytkowników biznesowych. Połączenie niezawodności, wydajności i eleganckiej konstrukcji sprawia, że jest idealnym towarzyszem w codziennej pracy.", productName))
	case strings.Contains(productName, "IdeaPad") || strings.Contains(productName, "IdeaCentre"):
		sb.WriteString(fmt.Sprintf("%s łączy nowoczesny design z praktyczną funkcjonalnością. To urządzenie zaprojektowano z myślą o użytkownikach, którzy cenią sobie zarówno wygląd, jak i niezawodność w codziennym użytkowaniu.", productName))
	case strings.Contains(productName, "Legion"):
		sb.WriteString(fmt.Sprintf("%s to potężna maszyna stworzona dla graczy i entuzjastów wydajności. Zaawansowane komponenty zapewniają płynną pracę nawet przy najbardziej wymagających grach i aplikacjach.", productName))
	case strings.Contains(productName, "Yoga"):
		sb.WriteString(fmt.Sprintf("%s to uniwersalne urządzenie, które dostosowuje się do Twoich potrzeb. Dzięki innowacyjnej konstrukcji możesz pracować w pozycji laptopa, tabletu lub namiotu - zawsze wygodnie i efektywnie.", productName))
	default:
		sb.WriteString(fmt.Sprintf("%s to nowoczesne urządzenie od Lenovo, które łączy w sobie zaawansowane technologie z przystępną ceną. Idealny wybór dla osób poszukujących niezawodnego sprzętu do pracy i rozrywki.", productName))
	}

	// Processor info
	if processor != "" {
		shortProc := strings.Split(processor, ",")[0]
		sb.WriteString(fmt.Sprintf("\n\nW centrum tego urządzenia znajduje się %s. Ten procesor zapewnia wystarczającą moc obliczeniową do płynnej obsługi wielu zadań jednocześnie.", shortProc))
	}

	// Memory and storage
	if memory != "" || storage != "" {
		parts := []string{}
		if memory != "" {
			parts = append(parts, fmt.Sprintf("pamięcią %s", memory))
		}
		if storage != "" {
			parts = append(parts, fmt.Sprintf("dyskiem %s", storage))
		}
		sb.WriteString(fmt.Sprintf("\n\nKonfiguracja obejmuje %s, co gwarantuje szybki dostęp do danych i płynną pracę z wieloma otwartymi aplikacjami.", strings.Join(parts, " oraz ")))
	}

	// Display
	if display != "" {
		sb.WriteString(fmt.Sprintf("\n\nEkran %s oferuje wyraźny i szczegółowy obraz. Idealny do pracy z dokumentami, oglądania filmów czy edycji zdjęć.", display))
	}

	// Graphics (only if dedicated)
	if graphics != "" && !strings.Contains(graphics, "Integrated") {
		sb.WriteString(fmt.Sprintf("\n\nKarta graficzna %s zapewnia doskonałą wydajność w grach i aplikacjach graficznych.", graphics))
	}

	// Battery and weight
	if battery != "" && weight != "" {
		sb.WriteString(fmt.Sprintf("\n\nZ baterią o pojemności %s i wagą zaledwie %s, to urządzenie jest idealne do pracy w podróży. Będziesz mógł pracować przez wiele godzin bez konieczności ładowania.", battery, weight))
	} else if battery != "" {
		sb.WriteString(fmt.Sprintf("\n\nBateria o pojemności %s zapewnia długą pracę na jednym ładowaniu.", battery))
	}

	// Connectivity
	if wifi != "" {
		sb.WriteString(fmt.Sprintf("\n\nUrządzenie wyposażono w %s, co zapewnia szybkie i stabilne połączenie z internetem oraz innymi urządzeniami bezprzewodowymi.", wifi))
	}

	// Highlights
	if len(highlights) > 0 {
		sb.WriteString("\n\nWyróżniające się cechy tego modelu to: ")
		for i, h := range highlights[:min(2, len(highlights))] {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(h)
		}
		sb.WriteString(".")
	}

	return sb.String()
}

// specsToAttributes converts specs to Polish attribute key-value pairs
func specsToAttributes(specs map[string]string) []model.KeyValue {
	attrMap := map[string]string{
		"Processor":          "Procesor",
		"Memory":             "Pamięć RAM",
		"Storage":            "Dysk twardy",
		"Graphics":           "Karta graficzna",
		"Display":            "Ekran",
		"Operating System":   "System operacyjny",
		"Weight":             "Waga",
		"Battery":            "Bateria",
		"Dimensions (WxDxH)": "Wymiary",
		"Case Color":         "Kolor obudowy",
		"WLAN + Bluetooth":   "Wi-Fi i Bluetooth",
		"Ethernet":           "Ethernet",
		"Camera":             "Kamera",
		"Speakers":           "Głośniki",
		"Keyboard":           "Klawiatura",
		"Touchpad":           "Dotykowy pad",
	}

	var attrs []model.KeyValue
	for key, value := range specs {
		if value == "" || value == "N/A" || value == "None" {
			continue
		}
		plKey, ok := attrMap[key]
		if !ok {
			plKey = key
		}
		attrs = append(attrs, model.KeyValue{Key: plKey, Value: strings.TrimSpace(value)})
	}

	return attrs
}

// checkAndCompactShards checks each shard's bloat and compacts if any exceeds threshold
func (h *Handlers) checkAndCompactShards() {
	if h.store == nil {
		return
	}

	shards := h.store.DB().ShardUsages()
	const threshold = 60.0 // percent

	for i, shard := range shards {
		if i > 0 {

		}
		if shard.FileSize > 0 {
			bloatPct := float64(shard.FreeOffset) / float64(shard.FileSize) * 100.0
			if bloatPct > threshold {
				fmt.Printf("[IMPORT-LENOVO] Shard %d bloat %.1f%% > %.0f%%, compacting...\n",
					shard.ShardIndex, bloatPct, threshold)

				// Compact synchronously and wait for completion
				if err := h.store.DB().CompactAllShards(1000); err != nil {
					fmt.Printf("[IMPORT-LENOVO] Compaction error: %v\n", err)
				} else {
					fmt.Printf("[IMPORT-LENOVO] Compaction completed\n")
				}
				return
			}
		}
	}
}

// generateLenovoSlug creates a unique URL-friendly slug with EAN and key specs
func generateLenovoSlug(productName string, ean string, specs map[string]string) string {
	// Base slug from product name (lowercase)
	base := strings.ToLower(productName)
	re := regexp.MustCompile(`[^\w\s-]`)
	base = re.ReplaceAllString(base, "")
	re2 := regexp.MustCompile(`[\s_]+`)
	base = re2.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")

	// Add key specs for uniqueness (processor, memory, disk)
	var parts []string
	parts = append(parts, base)

	if proc := specs["Processor"]; proc != "" {
		shortProc := strings.ToLower(strings.Split(proc, ",")[0])
		re3 := regexp.MustCompile(`[^\w\s-]`)
		shortProc = re3.ReplaceAllString(shortProc, "")
		re4 := regexp.MustCompile(`[\s_]+`)
		shortProc = re4.ReplaceAllString(shortProc, "-")
		if len(shortProc) > 30 {
			shortProc = shortProc[:30]
		}
		parts = append(parts, strings.Trim(shortProc, "-"))
	}

	if mem := specs["Memory"]; mem != "" {
		re5 := regexp.MustCompile(`([0-9]+GB)`)
		match := re5.FindString(mem)
		if match != "" {
			parts = append(parts, strings.ToLower(match))
		}
	}

	if storage := specs["Storage"]; storage != "" {
		re6 := regexp.MustCompile(`([0-9]+[GT]B)`)
		match := re6.FindString(storage)
		if match != "" {
			parts = append(parts, strings.ToLower(match))
		}
	}

	// Add EAN suffix for guaranteed uniqueness
	return strings.Join(parts, "-") + "-" + ean
}
