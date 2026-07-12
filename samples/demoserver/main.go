package main

import (
	"cmp"
	"embed"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/silentjson/v2"
)

//go:embed static/*
var staticFS embed.FS

// Transaction represents the structure stored in the database (fully mapped to all 14 CSV columns)
type Transaction struct {
	ID           int     `json:"id"` // Changed to int
	Date         string  `json:"date"`
	Region       string  `json:"region"`
	Country      string  `json:"country"`
	ItemType     string  `json:"item_type"`
	SalesChannel string  `json:"sales_channel"`
	Priority     string  `json:"priority"`
	ShipDate     string  `json:"ship_date"`
	UnitsSold    int     `json:"units_sold"`
	UnitPrice    float64 `json:"unit_price"`
	UnitCost     float64 `json:"unit_cost"`
	TotalRevenue float64 `json:"total_revenue"`
	TotalCost    float64 `json:"total_cost"`
	TotalProfit  float64 `json:"total_profit"`
}

type SortTemp struct {
	ID           int
	Date         string
	ShipDate     string
	Region       string
	Country      string
	ItemType     string
	SalesChannel string
	Priority     string
	UnitsSold    int
	UnitPrice    float64
	UnitCost     float64
	TotalRevenue float64
	TotalCost    float64
	TotalProfit  float64
}

var transReg = silentjson.BuildRegistry(reflect.TypeOf(Transaction{}))

// Global server state
type ServerState struct {
	mu               sync.RWMutex
	Status           string `json:"status"` // "idle", "generating", "importing", "completed", "error"
	Processed        int64  `json:"processed"`
	Total            int64  `json:"total"`
	StartTime        time.Time
	Elapsed          float64 `json:"elapsed"` // in seconds
	Speed            float64 `json:"speed"`   // rows/sec
	ErrorMsg         string  `json:"error,omitempty"`
	UniqueCategories []string
	UniqueMerchants  []string
	UniqueStatuses   []string
}

var (
	state  ServerState
	db     *makodb.ShardedDB
	dbPath = "demo_transactions_db"

	// DB configuration parameters
	dbNumShards          = 16
	dbMaxTotalSize       = uint64(128 * 1024 * 1024 * 1024) // Default to 128 GB
	dbNumBucketsPerShard = uint64(5_000_000)                // Default to 5M buckets per shard

	recentTransactions []Transaction
	recentMu           sync.RWMutex
)

func init() {
	state.Status = "idle"
}

func updateProgress(processed int64, total int64) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Processed = processed
	if total > 0 {
		state.Total = total
	}
	if !state.StartTime.IsZero() {
		elapsed := time.Since(state.StartTime).Seconds()
		state.Elapsed = elapsed
		if elapsed > 0.1 {
			state.Speed = float64(processed) / elapsed
		}
	}
}

func setStatus(status string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Status = status
	if status == "generating" || status == "importing" {
		state.StartTime = time.Now()
		state.Processed = 0
		state.Elapsed = 0
		state.Speed = 0
		state.ErrorMsg = ""
	}
}

func setError(msg string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Status = "error"
	state.ErrorMsg = msg
}

func serializeInt32(val int32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(val))
	return buf[:]
}

func deserializeInt32(b []byte) int32 {
	if len(b) < 4 {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b[:4]))
}

func main() {
	// Clean up old databases if they exist (commented out to persist database and support startup sync)
	// os.RemoveAll(dbPath)

	var err error
	// Open a 2 GB sharded database with 5,000,000 buckets per shard
	db, err = makodb.OpenSharded(dbPath, dbNumShards, dbMaxTotalSize, dbNumBucketsPerShard)
	if err != nil {
		log.Fatalf("Failed to open MakoDB: %v", err)
	}

	// Startup Sync Check
	var maxID int
	if totalVal, err := db.Get("state:total"); err == nil && len(totalVal) >= 4 {
		maxID = int(deserializeInt32(totalVal))
	} else if sortedVal, err := db.Get("sort:id"); err == nil && len(sortedVal) > 0 {
		ids := deserializeIDs(sortedVal)
		if len(ids) > 0 {
			maxID = int(ids[len(ids)-1]) // Fast lookup from sorted array end without looping
		}
	}

	if maxID > 0 {
		state.mu.Lock()
		state.Total = int64(maxID)
		state.Status = "completed"
		state.mu.Unlock()

		if maxID == 100_000_000 {
			state.mu.Lock()
			state.UniqueCategories = []string{"Fruits", "Clothes", "Cosmetics", "Baby Food", "Beverages", "Office Supplies", "Household", "Snacks", "Personal Care", "Cereal"}
			state.UniqueMerchants = []string{"United States", "United Kingdom", "Germany", "France", "Japan", "China", "Canada", "Australia", "India", "South Africa"}
			state.UniqueStatuses = []string{"Online", "Offline"}
			sortStrings(state.UniqueCategories)
			sortStrings(state.UniqueMerchants)
			sortStrings(state.UniqueStatuses)
			state.mu.Unlock()
		} else {
			buildAutocompleteIndex()
		}

		// Search for any unindexed records: tx:<maxID+1>, tx:<maxID+2>... (limit to 1000 to prevent startup hangs)
		recentMu.Lock()
		for i := maxID + 1; i < maxID+1000; i++ {
			idStr := "tx:" + strconv.Itoa(i)
			var tx Transaction
			if err := db.Query(idStr, &tx); err == nil {
				recentTransactions = append(recentTransactions, tx)
			} else {
				break
			}
		}
		hasUnindexed := len(recentTransactions) > 0
		recentMu.Unlock()

		if hasUnindexed {
			log.Printf("Detected %d unindexed transactions at startup. Synchronizing index...", len(recentTransactions))
			if err := flushBufferToDisk(); err != nil {
				log.Printf("Failed to sync indexes at startup: %v", err)
			} else {
				log.Println("Startup index synchronization completed successfully!")
			}
		}
	}
	defer db.Close()

	// Static files handler using embedded assets
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to load embedded static filesystem: %v", err)
	}
	http.Handle("/", http.FileServer(http.FS(subFS)))

	// API Handlers
	http.HandleFunc("/api/progress", handleProgress)
	http.HandleFunc("/api/generate-mock", handleGenerateAndImportMock)
	http.HandleFunc("/api/import-local-sales", handleImportLocalSales)
	http.HandleFunc("/api/upload", handleUpload)
	http.HandleFunc("/api/suggest", handleSuggest)
	http.HandleFunc("/api/transactions", handleTransactions)
	http.HandleFunc("/api/transactions/create", handleCreateTransaction)
	http.HandleFunc("/api/transactions/stats", handleStats)
	http.HandleFunc("/api/transactions/flush", handleFlushTransaction)

	port := "8080"
	fmt.Printf("⚡ MakoDB 100M Transactions Demo Server is running on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func handleProgress(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func handleImportLocalSales(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	if state.Status == "generating" || state.Status == "importing" {
		state.mu.Unlock()
		http.Error(w, "Import or generation in progress", http.StatusConflict)
		return
	}
	state.mu.Unlock()

	localPath := "cmd/demoserver/Sales_Records.csv"
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		http.Error(w, "Sales_Records.csv not found in cmd/demoserver/", http.StatusNotFound)
		return
	}

	setStatus("importing")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"started"}`))

	go func() {
		err := importCSV(localPath)
		if err != nil {
			setError("Failed to import local Sales_Records.csv: " + err.Error())
			return
		}

		buildAutocompleteIndex()
		setStatus("completed")
	}()
}

func handleGenerateAndImportMock(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	if state.Status == "generating" || state.Status == "importing" {
		state.mu.Unlock()
		http.Error(w, "Import or generation in progress", http.StatusConflict)
		return
	}
	state.mu.Unlock()

	setStatus("generating")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"started"}`))

	go func() {
		err := generateAndImportMockDirect(100_000_000)
		if err != nil {
			setError("Failed to generate and import mock: " + err.Error())
			return
		}

		// Build Autocomplete Indexes
		state.mu.Lock()
		state.UniqueCategories = []string{"Fruits", "Clothes", "Cosmetics", "Baby Food", "Beverages", "Office Supplies", "Household", "Snacks", "Personal Care", "Cereal"}
		state.UniqueMerchants = []string{"United States", "United Kingdom", "Germany", "France", "Japan", "China", "Canada", "Australia", "India", "South Africa"}
		state.UniqueStatuses = []string{"Online", "Offline"}
		sortStrings(state.UniqueCategories)
		sortStrings(state.UniqueMerchants)
		sortStrings(state.UniqueStatuses)
		state.mu.Unlock()

		setStatus("completed")
	}()
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state.mu.Lock()
	if state.Status == "generating" || state.Status == "importing" {
		state.mu.Unlock()
		http.Error(w, "Import or generation in progress", http.StatusConflict)
		return
	}
	state.mu.Unlock()

	// Limit upload size to 200MB
	r.Body = http.MaxBytesReader(w, r.Body, 200*1024*1024)
	err := r.ParseMultipartForm(50 * 1024 * 1024)
	if err != nil {
		http.Error(w, "File too large or malformed multipart form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file in upload form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Save file to disk temporarily
	tempFile, err := os.CreateTemp("", "uploaded_transactions_*.csv")
	if err != nil {
		http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	_, err = io.Copy(tempFile, file)
	tempFile.Close()
	if err != nil {
		http.Error(w, "Failed to save uploaded file", http.StatusInternalServerError)
		return
	}

	setStatus("importing")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"started"}`))

	go func() {
		err := importCSV(tempPath)
		if err != nil {
			setError("Failed to import CSV: " + err.Error())
			return
		}

		buildAutocompleteIndex()
		setStatus("completed")
	}()
}

func handleSuggest(w http.ResponseWriter, r *http.Request) {
	field := r.URL.Query().Get("field")
	q := strings.ToLower(r.URL.Query().Get("q"))

	state.mu.RLock()
	defer state.mu.RUnlock()

	var source []string
	switch field {
	case "category":
		source = state.UniqueCategories
	case "merchant":
		source = state.UniqueMerchants
	case "status":
		source = state.UniqueStatuses
	default:
		http.Error(w, "Invalid field parameter", http.StatusBadRequest)
		return
	}

	var suggestions []string
	for _, val := range source {
		if strings.Contains(strings.ToLower(val), q) {
			suggestions = append(suggestions, val)
			if len(suggestions) >= 10 { // Limit to 10 suggestions
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}

// SortItem is a generic struct to hold a key and a sortable value
type SortItem[T cmp.Ordered] struct {
	Key   string // The actual key in makodb, e.g., "tx:123"
	Value T
}

type SortPairInt struct {
	ID  int32
	Val int
}

type SortPairFloat struct {
	ID  int32
	Val float64
}

type SortPairString struct {
	ID  int32
	Val string
}

func prepareDateString(dateStr string) string {
	// Ожидаем формат "M/D/YYYY"
	parts := strings.Split(dateStr, "/")
	if len(parts) != 3 {
		return ""
	}
	// Ensure month and day are two digits for consistent sorting
	month := parts[0]
	if len(month) == 1 {
		month = "0" + month
	}
	day := parts[1]
	if len(day) == 1 {
		day = "0" + day
	}

	return parts[2] + month + day // YYYYMMDD
}

// extractSortValue extracts the sortable value from a Transaction based on the sortBy field.
// It also returns the reflect.Type of the extracted value for generic sorting.
func extractSortValue(tx Transaction, sortBy string) (interface{}, reflect.Type, error) {
	switch sortBy {
	case "id":
		return tx.ID, reflect.TypeOf(0), nil
	case "date":
		return prepareDateString(tx.Date), reflect.TypeOf(""), nil
	case "region":
		return tx.Region, reflect.TypeOf(""), nil
	case "country":
		return tx.Country, reflect.TypeOf(""), nil
	case "item_type":
		return tx.ItemType, reflect.TypeOf(""), nil
	case "sales_channel":
		return tx.SalesChannel, reflect.TypeOf(""), nil
	case "priority":
		return tx.Priority, reflect.TypeOf(""), nil
	case "ship_date":
		return prepareDateString(tx.ShipDate), reflect.TypeOf(""), nil
	case "units_sold":
		return tx.UnitsSold, reflect.TypeOf(0), nil
	case "unit_price":
		return tx.UnitPrice, reflect.TypeOf(0.0), nil
	case "unit_cost":
		return tx.UnitCost, reflect.TypeOf(0.0), nil
	case "total_revenue":
		return tx.TotalRevenue, reflect.TypeOf(0.0), nil
	case "total_cost":
		return tx.TotalCost, reflect.TypeOf(0.0), nil
	case "total_profit":
		return tx.TotalProfit, reflect.TypeOf(0.0), nil
	default:
		return nil, nil, fmt.Errorf("unknown sort field: %s", sortBy)
	}
}

func intersectSlices(inputSlices [][]int32) []int32 {
	if len(inputSlices) == 0 {
		return nil
	}
	if len(inputSlices) == 1 {
		return inputSlices[0]
	}
	// Sort slices by length so we start with the smallest one
	slices.SortFunc(inputSlices, func(a, b []int32) int {
		return cmp.Compare(len(a), len(b))
	})

	counts := make(map[int32]int)
	for _, id := range inputSlices[0] {
		counts[id] = 1
	}

	for i := 1; i < len(inputSlices); i++ {
		for _, id := range inputSlices[i] {
			if counts[id] == i {
				counts[id] = i + 1
			}
		}
	}

	var result []int32
	targetCount := len(inputSlices)
	for _, id := range inputSlices[0] {
		if counts[id] == targetCount {
			result = append(result, id)
		}
	}
	return result
}

// fetchTransactionKeysAndSortValuesOnTheFly performs the first pass:
// it collects transaction keys and their corresponding sort values based on filters and sortBy field.
func fetchTransactionKeysAndSortValuesOnTheFly(category, merchant, status, sortBy string) ([]interface{}, reflect.Type, int, error) {
	var sortItems []interface{}
	var sortFieldType reflect.Type

	// Determine the initial type of the sort field. This will be confirmed/updated by extractSortValue.
	switch sortBy {
	case "id", "units_sold":
		sortFieldType = reflect.TypeOf(0) // int
	case "date", "ship_date", "region", "country", "item_type", "sales_channel", "priority":
		sortFieldType = reflect.TypeOf("") // string
	case "unit_price", "unit_cost", "total_revenue", "total_cost", "total_profit":
		sortFieldType = reflect.TypeOf(0.0) // float64
	default:
		sortBy = "id"
		sortFieldType = reflect.TypeOf(0)
	}

	collectSortItem := func(key string, tx Transaction) {
		if sortVal, sft, err := extractSortValue(tx, sortBy); err == nil {
			if sortFieldType == nil || sortFieldType.Kind() == reflect.Invalid {
				sortFieldType = sft
			}

			switch sft.Kind() {
			case reflect.Int:
				sortItems = append(sortItems, SortItem[int]{Key: key, Value: sortVal.(int)})
			case reflect.String:
				sortItems = append(sortItems, SortItem[string]{Key: key, Value: sortVal.(string)})
			case reflect.Float64:
				sortItems = append(sortItems, SortItem[float64]{Key: key, Value: sortVal.(float64)})
			}
		}
	}

	if category == "" && merchant == "" && status == "" {
		// No filters, iterate all transactions
		_ = db.ForEach(func(key string, value []byte) error {
			if strings.HasPrefix(key, "tx:") {
				var tx Transaction
				if err := silentjson.ParseObject(value, transReg, unsafe.Pointer(&tx)); err == nil {
					collectSortItem(key, tx)
				}
			}
			return nil
		})
	} else {
		// With filters, use db.Search
		var queryTokens []string
		if category != "" {
			queryTokens = append(queryTokens, makodb.Tokenize(category)...)
		}
		if merchant != "" {
			queryTokens = append(queryTokens, makodb.Tokenize(merchant)...)
		}
		if status != "" {
			queryTokens = append(queryTokens, makodb.Tokenize(status)...)
		}

		if len(queryTokens) > 0 {
			queryStr := strings.Join(queryTokens, " ")
			matches, err := db.Search(queryStr)
			if err == nil {
				for _, id := range matches {
					var tx Transaction
					if err := db.Query(id, &tx); err == nil {
						// Apply exact filters
						if (category == "" || strings.EqualFold(tx.ItemType, category)) &&
							(merchant == "" || strings.EqualFold(tx.Country, merchant)) &&
							(status == "" || strings.EqualFold(tx.SalesChannel, status)) {
							collectSortItem(id, tx)
						}
					}
				}
			} else {
				return nil, nil, 0, err
			}
		}
	}

	totalCount := len(sortItems)
	return sortItems, sortFieldType, totalCount, nil
}

// fetchTransactionKeysOnTheFly fetches transaction keys based on filters when no sorting is required.
func fetchTransactionKeysOnTheFly(category, merchant, status string) ([]string, int, error) {
	var transactionKeys []string
	var totalCount int

	if category == "" && merchant == "" && status == "" {
		state.mu.RLock()
		totalCount = int(state.Total)
		state.mu.RUnlock()

		_ = db.ForEach(func(key string, value []byte) error {
			if strings.HasPrefix(key, "tx:") {
				transactionKeys = append(transactionKeys, key)
			}
			return nil
		})
	} else {
		var queryTokens []string
		if category != "" {
			queryTokens = append(queryTokens, makodb.Tokenize(category)...)
		}
		if merchant != "" {
			queryTokens = append(queryTokens, makodb.Tokenize(merchant)...)
		}
		if status != "" {
			queryTokens = append(queryTokens, makodb.Tokenize(status)...)
		}

		if len(queryTokens) > 0 {
			queryStr := strings.Join(queryTokens, " ")
			matches, err := db.Search(queryStr)
			if err == nil {
				for _, id := range matches {
					var tx Transaction
					if err := db.Query(id, &tx); err == nil {
						if (category == "" || strings.EqualFold(tx.ItemType, category)) &&
							(merchant == "" || strings.EqualFold(tx.Country, merchant)) &&
							(status == "" || strings.EqualFold(tx.SalesChannel, status)) {
							transactionKeys = append(transactionKeys, id)
						}
					}
				}
			} else {
				return nil, 0, err
			}
		}
	}
	totalCount = len(transactionKeys)
	return transactionKeys, totalCount, nil
}

func handleTransactions(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category")) // Item Type
	merchant := strings.TrimSpace(r.URL.Query().Get("merchant")) // Country
	status := strings.TrimSpace(r.URL.Query().Get("status"))     // Sales Channel

	sortBy := r.URL.Query().Get("sort_by")
	if sortBy == "" {
		sortBy = "id"
	}
	sortOrder := r.URL.Query().Get("sort_order") // "asc" or "desc"
	mode := r.URL.Query().Get("mode")            // "on_the_fly" or "pre_sorted"
	if mode == "" {
		mode = "pre_sorted"
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	pageStr := r.URL.Query().Get("page")
	page := 1
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}

	var results []Transaction
	start := time.Now()
	totalCount := 0

	if mode == "on_the_fly" {
		if sortBy != "" {
			sortItems, sortFieldType, count, err := fetchTransactionKeysAndSortValuesOnTheFly(category, merchant, status, sortBy)
			if err != nil {
				http.Error(w, "Error fetching sort values: "+err.Error(), http.StatusInternalServerError)
				return
			}
			totalCount = count

			// Sort the collected SortItems
			sort.Slice(sortItems, func(i, j int) bool {
				var isLess bool
				switch sortFieldType.Kind() {
				case reflect.Int:
					si := sortItems[i].(SortItem[int])
					sj := sortItems[j].(SortItem[int])
					isLess = si.Value < sj.Value
				case reflect.String:
					si := sortItems[i].(SortItem[string])
					sj := sortItems[j].(SortItem[string])
					isLess = si.Value < sj.Value
				case reflect.Float64:
					si := sortItems[i].(SortItem[float64])
					sj := sortItems[j].(SortItem[float64])
					isLess = si.Value < sj.Value
				default:
					return false
				}

				if sortOrder == "desc" {
					return !isLess
				}
				return isLess
			})

			// Apply pagination to sorted sortItems
			offset := (page - 1) * limit
			if offset < 0 {
				offset = 0
			}
			if offset > totalCount {
				offset = totalCount
			}
			end := offset + limit
			if end > totalCount {
				end = totalCount
			}

			if offset > len(sortItems) {
				offset = len(sortItems)
			}
			if end > len(sortItems) {
				end = len(sortItems)
			}
			slicedSortItems := sortItems[offset:end]

			keysToFetch := make([]string, len(slicedSortItems))
			for i, item := range slicedSortItems {
				switch sortFieldType.Kind() {
				case reflect.Int:
					keysToFetch[i] = item.(SortItem[int]).Key
				case reflect.String:
					keysToFetch[i] = item.(SortItem[string]).Key
				case reflect.Float64:
					keysToFetch[i] = item.(SortItem[float64]).Key
				}
			}

			fetchedData, err := db.MultiGet(keysToFetch)
			if err == nil {
				for _, key := range keysToFetch {
					if data, ok := fetchedData[key]; ok {
						var tx Transaction
						if err := silentjson.ParseObject(data, transReg, unsafe.Pointer(&tx)); err == nil {
							results = append(results, tx)
						}
					}
				}
			}
		} else {
			// No sorting
			transactionKeys, count, err := fetchTransactionKeysOnTheFly(category, merchant, status)
			if err != nil {
				http.Error(w, "Error fetching transaction keys: "+err.Error(), http.StatusInternalServerError)
				return
			}
			totalCount = count

			offset := (page - 1) * limit
			if offset < 0 {
				offset = 0
			}
			if offset > totalCount {
				offset = totalCount
			}
			end := offset + limit
			if end > totalCount {
				end = totalCount
			}

			if offset > len(transactionKeys) {
				offset = len(transactionKeys)
			}
			if end > len(transactionKeys) {
				end = len(transactionKeys)
			}
			slicedTransactionKeys := transactionKeys[offset:end]

			fetchedData, err := db.MultiGet(slicedTransactionKeys)
			if err == nil {
				for _, key := range slicedTransactionKeys {
					if data, ok := fetchedData[key]; ok {
						var tx Transaction
						if err := silentjson.ParseObject(data, transReg, unsafe.Pointer(&tx)); err == nil {
							results = append(results, tx)
						}
					}
				}
			}
		}
	} else {
		// Pre-sorted index scanning
		var slices2 [][]int32
		if category != "" {
			val, err := db.Get("idx:item_type:" + strings.ToLower(category))
			if err == nil {
				slices2 = append(slices2, deserializeIDs(val))
			} else {
				slices2 = append(slices2, nil)
			}
		}
		if merchant != "" {
			val, err := db.Get("idx:country:" + strings.ToLower(merchant))
			if err == nil {
				slices2 = append(slices2, deserializeIDs(val))
			} else {
				slices2 = append(slices2, nil)
			}
		}
		if status != "" {
			val, err := db.Get("idx:sales_channel:" + strings.ToLower(status))
			if err == nil {
				slices2 = append(slices2, deserializeIDs(val))
			} else {
				slices2 = append(slices2, nil)
			}
		}

		sortKey := "sort:" + sortBy
		sortedIDsVal, err := db.Get(sortKey)
		if err != nil {
			sortedIDsVal, _ = db.Get("sort:id")
		}
		var sortedIDs []int32
		if len(sortedIDsVal) > 0 {
			sortedIDs = deserializeIDs(sortedIDsVal)
		}

		offset := (page - 1) * limit
		if offset < 0 {
			offset = 0
		}

		// 1. Filter recentTransactions from memory
		var filteredRecent []Transaction
		recentMu.RLock()
		for _, tx := range recentTransactions {
			if (category == "" || strings.EqualFold(tx.ItemType, category)) &&
				(merchant == "" || strings.EqualFold(tx.Country, merchant)) &&
				(status == "" || strings.EqualFold(tx.SalesChannel, status)) {
				filteredRecent = append(filteredRecent, tx)
			}
		}
		recentMu.RUnlock()

		// 2. Sort filteredRecent by sortBy and sortOrder
		if len(filteredRecent) > 0 {
			slices.SortFunc(filteredRecent, func(a, b Transaction) int {
				valA, typeA, errA := extractSortValue(a, sortBy)
				valB, _, errB := extractSortValue(b, sortBy)
				if errA != nil || errB != nil {
					return 0
				}
				var cmpResult int
				switch typeA.Kind() {
				case reflect.Int:
					cmpResult = cmp.Compare(valA.(int), valB.(int))
				case reflect.String:
					cmpResult = cmp.Compare(valA.(string), valB.(string))
				case reflect.Float64:
					cmpResult = cmp.Compare(valA.(float64), valB.(float64))
				}
				if sortOrder == "desc" {
					return -cmpResult
				}
				return cmpResult
			})
		}

		if category == "" && merchant == "" && status == "" {
			// No filters: sortedIDs and filteredRecent
			totalCount = len(sortedIDs) + len(filteredRecent)

			// Merge sortedIDs and filteredRecent up to offset+limit using two-pointer
			type dbStreamIterator struct {
				sortedIDs    []int32
				currentIndex int
				step         int
			}
			var dbIter dbStreamIterator
			dbIter.sortedIDs = sortedIDs
			if sortOrder == "desc" {
				dbIter.currentIndex = len(sortedIDs) - 1
				dbIter.step = -1
			} else {
				dbIter.currentIndex = 0
				dbIter.step = 1
			}

			dbNext := func() (int32, bool) {
				if dbIter.currentIndex < 0 || dbIter.currentIndex >= len(dbIter.sortedIDs) {
					return 0, false
				}
				id := dbIter.sortedIDs[dbIter.currentIndex]
				dbIter.currentIndex += dbIter.step
				return id, true
			}

			var mergedIDs []string
			dbHasNext := true
			var nextDbID string
			var nextDbVal interface{}
			var dbValType reflect.Type

			advanceDb := func() bool {
				for {
					id, ok := dbNext()
					if !ok {
						dbHasNext = false
						return false
					}
					docID := "tx:" + strconv.Itoa(int(id))
					var tx Transaction
					if err := db.Query(docID, &tx); err == nil {
						if val, stype, err := extractSortValue(tx, sortBy); err == nil {
							nextDbID = docID
							nextDbVal = val
							dbValType = stype
							return true
						}
					}
				}
			}

			advanceDb()

			recentIdx := 0
			for (dbHasNext || recentIdx < len(filteredRecent)) && len(mergedIDs) < offset+limit {
				chooseDb := false
				if !dbHasNext {
					chooseDb = false
				} else if recentIdx >= len(filteredRecent) {
					chooseDb = true
				} else {
					recentVal, _, _ := extractSortValue(filteredRecent[recentIdx], sortBy)
					isLess := false
					switch dbValType.Kind() {
					case reflect.Int:
						isLess = nextDbVal.(int) < recentVal.(int)
					case reflect.String:
						isLess = nextDbVal.(string) < recentVal.(string)
					case reflect.Float64:
						isLess = nextDbVal.(float64) < recentVal.(float64)
					}
					if sortOrder == "desc" {
						chooseDb = !isLess
					} else {
						chooseDb = isLess
					}
				}

				if chooseDb {
					mergedIDs = append(mergedIDs, nextDbID)
					advanceDb()
				} else {
					mergedIDs = append(mergedIDs, "tx:"+strconv.Itoa(filteredRecent[recentIdx].ID))
					recentIdx++
				}
			}

			var pageIDs []string
			if offset < len(mergedIDs) {
				endIdx := offset + limit
				if endIdx > len(mergedIDs) {
					endIdx = len(mergedIDs)
				}
				pageIDs = mergedIDs[offset:endIdx]
			}

			fetchedData, err := db.MultiGet(pageIDs)
			if err == nil {
				for _, id := range pageIDs {
					if data, ok := fetchedData[id]; ok {
						var tx Transaction
						if err := silentjson.ParseObject(data, transReg, unsafe.Pointer(&tx)); err == nil {
							results = append(results, tx)
						}
					}
				}
			}
		} else {
			candidateIDs := intersectSlices(slices2)
			totalCount = len(candidateIDs) + len(filteredRecent)

			if totalCount > 0 {
				candidateMap := make(map[int32]struct{}, len(candidateIDs))
				for _, id := range candidateIDs {
					candidateMap[id] = struct{}{}
				}

				var pageIDs []string

				// If total db candidate size is small, sort them all in memory (db + recent)
				if len(candidateIDs) <= 5000 {
					type sortItem struct {
						id  string
						val interface{}
					}
					var items []sortItem
					var valType reflect.Type

					for _, id := range candidateIDs {
						docID := "tx:" + strconv.Itoa(int(id))
						var tx Transaction
						if err := db.Query(docID, &tx); err == nil {
							if sortVal, stype, err := extractSortValue(tx, sortBy); err == nil {
								valType = stype
								items = append(items, sortItem{id: docID, val: sortVal})
							}
						}
					}

					for _, tx := range filteredRecent {
						if sortVal, stype, err := extractSortValue(tx, sortBy); err == nil {
							valType = stype
							items = append(items, sortItem{id: "tx:" + strconv.Itoa(tx.ID), val: sortVal})
						}
					}

					if len(items) > 0 {
						slices.SortFunc(items, func(a, b sortItem) int {
							var cmpResult int
							switch valType.Kind() {
							case reflect.Int:
								cmpResult = cmp.Compare(a.val.(int), b.val.(int))
							case reflect.String:
								cmpResult = cmp.Compare(a.val.(string), b.val.(string))
							case reflect.Float64:
								cmpResult = cmp.Compare(a.val.(float64), b.val.(float64))
							}
							if sortOrder == "desc" {
								return -cmpResult
							}
							return cmpResult
						})

						endIdx := offset + limit
						if offset < len(items) {
							if endIdx > len(items) {
								endIdx = len(items)
							}
							for _, item := range items[offset:endIdx] {
								pageIDs = append(pageIDs, item.id)
							}
						}
					}
				} else {
					// Large candidates: merge sortedIDs (filtered by candidateMap) and filteredRecent
					type dbStreamIterator struct {
						sortedIDs    []int32
						candidateMap map[int32]struct{}
						currentIndex int
						step         int
					}
					var dbIter dbStreamIterator
					dbIter.sortedIDs = sortedIDs
					dbIter.candidateMap = candidateMap
					if sortOrder == "desc" {
						dbIter.currentIndex = len(sortedIDs) - 1
						dbIter.step = -1
					} else {
						dbIter.currentIndex = 0
						dbIter.step = 1
					}

					dbNext := func() (int32, bool) {
						for {
							if dbIter.currentIndex < 0 || dbIter.currentIndex >= len(dbIter.sortedIDs) {
								return 0, false
							}
							id := dbIter.sortedIDs[dbIter.currentIndex]
							dbIter.currentIndex += dbIter.step
							if _, ok := dbIter.candidateMap[id]; ok {
								return id, true
							}
						}
					}

					var mergedIDs []string
					dbHasNext := true
					var nextDbID string
					var nextDbVal interface{}
					var dbValType reflect.Type

					advanceDb := func() bool {
						for {
							id, ok := dbNext()
							if !ok {
								dbHasNext = false
								return false
							}
							docID := "tx:" + strconv.Itoa(int(id))
							var tx Transaction
							if err := db.Query(docID, &tx); err == nil {
								if val, stype, err := extractSortValue(tx, sortBy); err == nil {
									nextDbID = docID
									nextDbVal = val
									dbValType = stype
									return true
								}
							}
						}
					}

					advanceDb()

					recentIdx := 0
					for (dbHasNext || recentIdx < len(filteredRecent)) && len(mergedIDs) < offset+limit {
						chooseDb := false
						if !dbHasNext {
							chooseDb = false
						} else if recentIdx >= len(filteredRecent) {
							chooseDb = true
						} else {
							recentVal, _, _ := extractSortValue(filteredRecent[recentIdx], sortBy)
							isLess := false
							switch dbValType.Kind() {
							case reflect.Int:
								isLess = nextDbVal.(int) < recentVal.(int)
							case reflect.String:
								isLess = nextDbVal.(string) < recentVal.(string)
							case reflect.Float64:
								isLess = nextDbVal.(float64) < recentVal.(float64)
							}
							if sortOrder == "desc" {
								chooseDb = !isLess
							} else {
								chooseDb = isLess
							}
						}

						if chooseDb {
							mergedIDs = append(mergedIDs, nextDbID)
							advanceDb()
						} else {
							mergedIDs = append(mergedIDs, "tx:"+strconv.Itoa(filteredRecent[recentIdx].ID))
							recentIdx++
						}
					}

					if offset < len(mergedIDs) {
						endIdx := offset + limit
						if endIdx > len(mergedIDs) {
							endIdx = len(mergedIDs)
						}
						pageIDs = mergedIDs[offset:endIdx]
					}
				}

				fetchedData, err := db.MultiGet(pageIDs)
				if err == nil {
					for _, id := range pageIDs {
						if data, ok := fetchedData[id]; ok {
							var tx Transaction
							if err := silentjson.ParseObject(data, transReg, unsafe.Pointer(&tx)); err == nil {
								results = append(results, tx)
							}
						}
					}
				}
			}
		}
	}

	latency := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"latency_ns":    latency.Nanoseconds(),
		"latency_ms":    float64(latency.Nanoseconds()) / 1000000.0,
		"results_count": totalCount,
		"page":          page,
		"limit":         limit,
		"transactions":  results,
	}
	json.NewEncoder(w).Encode(response)
}

// Helpers for CSV Generation and Import

func generateMockCSV(path string, count int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	regions := []string{"North America", "Europe", "Asia", "Central America", "Sub-Saharan Africa", "Middle East", "Australia"}
	countries := []string{"United States", "United Kingdom", "Germany", "France", "Japan", "China", "Canada", "Australia", "India", "South Africa"}
	itemTypes := []string{"Fruits", "Clothes", "Cosmetics", "Baby Food", "Beverages", "Office Supplies", "Household", "Snacks", "Personal Care", "Cereal"}
	channels := []string{"Online", "Offline"}
	priorities := []string{"H", "M", "L", "C"}

	writer := csv.NewWriter(f)
	defer writer.Flush()

	// Write header
	err = writer.Write([]string{
		"Region", "Country", "Item Type", "Sales Channel", "Order Priority",
		"Order Date", "Order ID", "Ship Date", "Units Sold", "Unit Price",
		"Unit Cost", "Total Revenue", "Total Cost", "Total Profit",
	})
	if err != nil {
		return err
	}

	rng := rand.New(rand.NewSource(42)) // Seed for deterministic mock values

	for i := 0; i < count; i++ {
		id := i + 1 // Use integer ID for sorting consistency
		orderDate := time.Now().AddDate(0, 0, -rng.Intn(365))
		dateStr := orderDate.Format("1/2/2006")
		shipDateStr := orderDate.AddDate(0, 0, rng.Intn(10)+1).Format("1/2/2006")

		region := regions[rng.Intn(len(regions))]
		country := countries[rng.Intn(len(countries))]
		itemType := itemTypes[rng.Intn(len(itemTypes))]
		channel := channels[rng.Intn(len(channels))]
		priority := priorities[rng.Intn(len(priorities))]

		unitsSold := rng.Intn(9999) + 1
		unitPrice := rng.Float64()*400 + 10.0
		unitCost := unitPrice * 0.72
		totalRevenue := float64(unitsSold) * unitPrice
		totalCost := float64(unitsSold) * unitCost
		totalProfit := totalRevenue - totalCost

		err = writer.Write([]string{
			region,
			country,
			itemType,
			channel,
			priority,
			dateStr,
			strconv.Itoa(id), // Write integer ID
			shipDateStr,
			strconv.Itoa(unitsSold),
			fmt.Sprintf("%.2f", unitPrice),
			fmt.Sprintf("%.2f", unitCost),
			fmt.Sprintf("%.2f", totalRevenue),
			fmt.Sprintf("%.2f", totalCost),
			fmt.Sprintf("%.2f", totalProfit),
		})
		if err != nil {
			return err
		}

		if i%50000 == 0 {
			updateProgress(int64(i), int64(count))
		}
	}
	updateProgress(int64(count), int64(count))
	return nil
}

// getIndex finds the index of the first matching header synonym in a map
func getIndex(m map[string]int, keys ...string) int {
	for _, k := range keys {
		if idx, ok := m[k]; ok {
			return idx
		}
	}
	return -1
}

func importCSV(path string) error {
	recentMu.Lock()
	recentTransactions = nil
	recentMu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Get total row count for progress estimation
	var totalRows int64 = 0
	// Create a new reader to count rows without affecting the main reader's position
	if _, err := f.Seek(0, 0); err != nil { // Reset file pointer to beginning
		return fmt.Errorf("failed to seek file: %w", err)
	}
	rowCounter := csv.NewReader(f)
	for {
		_, err := rowCounter.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Log error but try to continue counting if possible, or return error
			log.Printf("Error reading row for count: %v", err)
			continue
		}
		totalRows++
	}
	totalRows--                             // Deduct CSV header row
	if _, err := f.Seek(0, 0); err != nil { // Reset file pointer to beginning for actual reading
		return fmt.Errorf("failed to seek file: %w", err)
	}

	reader := csv.NewReader(f)
	header, err := reader.Read() // Read CSV header row
	if err != nil {
		return err
	}

	// Create a map of header synonym -> index
	headerMap := make(map[string]int)
	for i, h := range header {
		headerMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	// Dynamically look up column indices to support different layouts and column orders
	idIdx := getIndex(headerMap, "order id", "order_id", "id", "transaction id", "transaction_id")
	dateIdx := getIndex(headerMap, "order date", "order_date", "date")
	regionIdx := getIndex(headerMap, "region")
	countryIdx := getIndex(headerMap, "country", "merchant")
	itemTypeIdx := getIndex(headerMap, "item type", "item_type", "category")
	salesChannelIdx := getIndex(headerMap, "sales channel", "sales_channel", "status")
	priorityIdx := getIndex(headerMap, "order priority", "order_priority", "priority")
	shipDateIdx := getIndex(headerMap, "ship date", "ship_date")
	unitsSoldIdx := getIndex(headerMap, "units sold", "units_sold")
	unitPriceIdx := getIndex(headerMap, "unit price", "unit_price")
	unitCostIdx := getIndex(headerMap, "unit cost", "unit_cost")
	totalRevenueIdx := getIndex(headerMap, "total revenue", "total_revenue")
	totalCostIdx := getIndex(headerMap, "total cost", "total_cost")
	totalProfitIdx := getIndex(headerMap, "total profit", "total_profit", "amount")

	// We reopen/recreate MakoDB to ensure the database file is empty before importing
	db.Close()
	os.RemoveAll(dbPath)
	var openErr error
	db, openErr = makodb.OpenSharded(dbPath, dbNumShards, dbMaxTotalSize, dbNumBucketsPerShard)
	if openErr != nil {
		return fmt.Errorf("failed to recreate database for import: %w", openErr)
	}

	// Dynamically calculate concurrency limit based on available physical memory
	availMem := getAvailablePhysicalMemory()
	// Leave 2 GB headroom for OS and other apps
	var targetLimit uint64
	if availMem > 2*1024*1024*1024 {
		targetLimit = availMem - 2*1024*1024*1024
	} else {
		targetLimit = 1 * 1024 * 1024 * 1024 // Safe minimum of 1 GB
	}

	// Respect env variable override if set
	if limitStr := os.Getenv("MAKO_WRITE_MEM_LIMIT"); limitStr != "" {
		if limit, err := strconv.ParseUint(limitStr, 10, 64); err == nil {
			targetLimit = limit
		}
	} else if targetLimit > 10*1024*1024*1024 {
		// Cap default automatic memory limit at 10 GB
		targetLimit = 10 * 1024 * 1024 * 1024
	}

	shardSize := dbMaxTotalSize / uint64(dbNumShards)
	maxConcurrentShards := 1 // Forced to 1 to keep only 1 shard active at a time and prevent disk overload

	log.Printf("[MakoDB] Shard writing configured: available memory = %d MB, target limit = %d MB, shard size = %d MB -> using %d concurrent writer (forced sequential)",
		availMem/(1024*1024), targetLimit/(1024*1024), shardSize/(1024*1024), maxConcurrentShards)

	// In-memory index building structure to avoid database contention and O(N^2) writes
	globalIndexMap := make(map[string][]int32)

	// Shard buckets to group records in memory before writing to DB shards
	type ShardBucket struct {
		DocIDs   []string
		JSONData [][]byte
	}
	buckets := make([]ShardBucket, dbNumShards)

	// Helper to flush current buckets to DB shards in parallel
	flushBuckets := func() error {
		shardChan := make(chan int, dbNumShards)
		for shardIdx := 0; shardIdx < dbNumShards; shardIdx++ {
			shardChan <- shardIdx
		}
		close(shardChan)

		var writeWg sync.WaitGroup
		var writeErr error
		var writeErrMu sync.Mutex

		for w := 0; w < maxConcurrentShards; w++ {
			writeWg.Add(1)
			go func() {
				defer writeWg.Done()
				for shardIdx := range shardChan {
					// Check if there was already an error in another goroutine
					writeErrMu.Lock()
					hasErr := writeErr != nil
					writeErrMu.Unlock()
					if hasErr {
						return
					}

					bucket := &buckets[shardIdx]
					if len(bucket.DocIDs) == 0 {
						continue
					}
					for i, docID := range bucket.DocIDs {
						if err := db.Put(docID, bucket.JSONData[i]); err != nil {
							writeErrMu.Lock()
							if writeErr == nil {
								writeErr = err
							}
							writeErrMu.Unlock()
							return
						}
					}
					// Free bucket memory immediately to limit working set size
					*bucket = ShardBucket{}
				}
			}()
		}
		writeWg.Wait()
		return writeErr
	}

	var imported int64 = 0

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading CSV row: %v", err)
			continue
		}

		var tx Transaction

		// Safely parse ID
		if idIdx != -1 && idIdx < len(row) {
			var parseErr error
			tx.ID, parseErr = strconv.Atoi(row[idIdx])
			if parseErr != nil {
				return fmt.Errorf("failed to parse ID '%s': %w", row[idIdx], parseErr)
			}
		} else {
			return fmt.Errorf("transaction ID column not found or row too short")
		}

		// Populate fields dynamically by resolved index
		if dateIdx != -1 && dateIdx < len(row) {
			tx.Date = row[dateIdx]
		}
		if regionIdx != -1 && regionIdx < len(row) {
			tx.Region = row[regionIdx]
		}
		if countryIdx != -1 && countryIdx < len(row) {
			tx.Country = row[countryIdx]
		}
		if itemTypeIdx != -1 && itemTypeIdx < len(row) {
			tx.ItemType = row[itemTypeIdx]
		}
		if salesChannelIdx != -1 && salesChannelIdx < len(row) {
			tx.SalesChannel = row[salesChannelIdx]
		}
		if priorityIdx != -1 && priorityIdx < len(row) {
			tx.Priority = row[priorityIdx]
		}
		if shipDateIdx != -1 && shipDateIdx < len(row) {
			tx.ShipDate = row[shipDateIdx]
		}
		if unitsSoldIdx != -1 && unitsSoldIdx < len(row) {
			tx.UnitsSold, _ = strconv.Atoi(row[unitsSoldIdx])
		}
		if unitPriceIdx != -1 && unitPriceIdx < len(row) {
			tx.UnitPrice, _ = strconv.ParseFloat(row[unitPriceIdx], 64)
		}
		if unitCostIdx != -1 && unitCostIdx < len(row) {
			tx.UnitCost, _ = strconv.ParseFloat(row[unitCostIdx], 64)
		}
		if totalRevenueIdx != -1 && totalRevenueIdx < len(row) {
			tx.TotalRevenue, _ = strconv.ParseFloat(row[totalRevenueIdx], 64)
		}
		if totalCostIdx != -1 && totalCostIdx < len(row) {
			tx.TotalCost, _ = strconv.ParseFloat(row[totalCostIdx], 64)
		}
		if totalProfitIdx != -1 && totalProfitIdx < len(row) {
			tx.TotalProfit, _ = strconv.ParseFloat(row[totalProfitIdx], 64)
		}

		jsonData := silentjson.Marshal(&tx, transReg, nil)
		docID := "tx:" + strconv.Itoa(tx.ID)

		// Group records by shard
		shardIdx := db.GetShardIndex(docID)
		buckets[shardIdx].DocIDs = append(buckets[shardIdx].DocIDs, docID)
		buckets[shardIdx].JSONData = append(buckets[shardIdx].JSONData, jsonData)

		txID32 := int32(tx.ID)

		// Build in-memory search index mapping
		if tx.ItemType != "" {
			key := "item_type:" + strings.ToLower(tx.ItemType)
			globalIndexMap[key] = append(globalIndexMap[key], txID32)
		}
		if tx.Country != "" {
			key := "country:" + strings.ToLower(tx.Country)
			globalIndexMap[key] = append(globalIndexMap[key], txID32)
		}
		if tx.SalesChannel != "" {
			key := "sales_channel:" + strings.ToLower(tx.SalesChannel)
			globalIndexMap[key] = append(globalIndexMap[key], txID32)
		}

		// Build token index for on-the-fly mode
		indexText := tx.ItemType + " " + tx.Country + " " + tx.SalesChannel
		tokens := makodb.Tokenize(indexText)
		for _, token := range tokens {
			globalIndexMap[token] = append(globalIndexMap[token], txID32)
		}

		imported++
		if imported%50000 == 0 {
			updateProgress(imported, totalRows)
		}

		// Flush to shards in chunks of 5,000,000 to prevent Go heap exhaustion
		if imported%5000000 == 0 {
			if err := flushBuckets(); err != nil {
				return err
			}
		}
	}

	// Flush any remaining records at the end
	if err := flushBuckets(); err != nil {
		return err
	}

	// Write globalIndexMap to MakoDB sequentially (fast, zero lock contention)
	for token, ids := range globalIndexMap {
		indexKey := "idx:" + token
		// Deduplicate IDs before storing to avoid redundant lookups
		uniqueIDs := make(map[int32]struct{}, len(ids))
		dedupedIDs := make([]int32, 0, len(ids))
		for _, id := range ids {
			if _, exists := uniqueIDs[id]; !exists {
				uniqueIDs[id] = struct{}{}
				dedupedIDs = append(dedupedIDs, id)
			}
		}
		if err := db.Put(indexKey, serializeIDs(dedupedIDs)); err != nil {
			return err
		}
	}

	state.mu.Lock()
	state.Total = imported
	state.mu.Unlock()

	// Write pre-sorted indexes using low-memory sequential DB scans
	buildAndWriteIntIndex("sort:id", func(tx *Transaction) int { return tx.ID })
	_ = db.Put("state:total", serializeInt32(int32(imported)))
	buildAndWriteStrIndex("sort:date", func(tx *Transaction) string { return prepareDateString(tx.Date) })
	buildAndWriteStrIndex("sort:ship_date", func(tx *Transaction) string { return prepareDateString(tx.ShipDate) })
	buildAndWriteStrIndex("sort:region", func(tx *Transaction) string { return tx.Region })
	buildAndWriteStrIndex("sort:country", func(tx *Transaction) string { return tx.Country })
	buildAndWriteStrIndex("sort:item_type", func(tx *Transaction) string { return tx.ItemType })
	buildAndWriteStrIndex("sort:sales_channel", func(tx *Transaction) string { return tx.SalesChannel })
	buildAndWriteStrIndex("sort:priority", func(tx *Transaction) string { return tx.Priority })
	buildAndWriteIntIndex("sort:units_sold", func(tx *Transaction) int { return tx.UnitsSold })
	buildAndWriteFloatIndex("sort:unit_price", func(tx *Transaction) float64 { return tx.UnitPrice })
	buildAndWriteFloatIndex("sort:unit_cost", func(tx *Transaction) float64 { return tx.UnitCost })
	buildAndWriteFloatIndex("sort:total_revenue", func(tx *Transaction) float64 { return tx.TotalRevenue })
	buildAndWriteFloatIndex("sort:total_cost", func(tx *Transaction) float64 { return tx.TotalCost })
	buildAndWriteFloatIndex("sort:total_profit", func(tx *Transaction) float64 { return tx.TotalProfit })

	updateProgress(imported, totalRows)
	return nil
}

func buildAutocompleteIndex() {
	catSet := make(map[string]struct{})
	merSet := make(map[string]struct{})
	statSet := make(map[string]struct{})

	_ = db.ForEach(func(key string, value []byte) error {
		if strings.HasPrefix(key, "tx:") {
			var tx Transaction
			if err := silentjson.ParseObject(value, transReg, unsafe.Pointer(&tx)); err == nil {
				if tx.ItemType != "" {
					catSet[tx.ItemType] = struct{}{}
				}
				if tx.Country != "" {
					merSet[tx.Country] = struct{}{}
				}
				if tx.SalesChannel != "" {
					statSet[tx.SalesChannel] = struct{}{}
				}
			}
		}
		return nil
	})

	state.mu.Lock()
	defer state.mu.Unlock()

	state.UniqueCategories = make([]string, 0, len(catSet))
	for cat := range catSet {
		state.UniqueCategories = append(state.UniqueCategories, cat)
	}
	sortStrings(state.UniqueCategories)

	state.UniqueMerchants = make([]string, 0, len(merSet))
	for mer := range merSet {
		state.UniqueMerchants = append(state.UniqueMerchants, mer)
	}
	sortStrings(state.UniqueMerchants)

	state.UniqueStatuses = make([]string, 0, len(statSet))
	for stat := range statSet {
		state.UniqueStatuses = append(state.UniqueStatuses, stat)
	}
	sortStrings(state.UniqueStatuses)
}

func sortStrings(slice []string) {
	for i := 0; i < len(slice); i++ {
		for j := i + 1; j < len(slice); j++ {
			if slice[i] > slice[j] {
				slice[i], slice[j] = slice[j], slice[i]
			}
		}
	}
}

func insertSorted(ids []int32, newID int32, newVal interface{}, field string) []int32 {
	n := len(ids)
	i := sort.Search(n, func(idx int) bool {
		var tx Transaction
		docID := "tx:" + strconv.Itoa(int(ids[idx]))
		if err := db.Query(docID, &tx); err != nil {
			return false
		}
		currVal, valType, err := extractSortValue(tx, field)
		if err != nil {
			return false
		}
		var isLess bool
		switch valType.Kind() {
		case reflect.Int:
			isLess = currVal.(int) < newVal.(int)
		case reflect.String:
			isLess = currVal.(string) < newVal.(string)
		case reflect.Float64:
			isLess = currVal.(float64) < newVal.(float64)
		}
		return isLess
	})

	ids = append(ids, 0)
	copy(ids[i+1:], ids[i:])
	ids[i] = newID
	return ids
}

func flushBufferToDisk() error {
	recentMu.Lock()
	if len(recentTransactions) == 0 {
		recentMu.Unlock()
		return nil
	}
	txs := make([]Transaction, len(recentTransactions))
	copy(txs, recentTransactions)
	recentTransactions = nil
	recentMu.Unlock()

	// 1. Update Inverted Indexes
	catMap := make(map[string][]int32)
	countryMap := make(map[string][]int32)
	channelMap := make(map[string][]int32)

	for _, tx := range txs {
		txID := int32(tx.ID)
		catKey := "idx:item_type:" + strings.ToLower(tx.ItemType)
		catMap[catKey] = append(catMap[catKey], txID)

		countryKey := "idx:country:" + strings.ToLower(tx.Country)
		countryMap[countryKey] = append(countryMap[countryKey], txID)

		channelKey := "idx:sales_channel:" + strings.ToLower(tx.SalesChannel)
		channelMap[channelKey] = append(channelMap[channelKey], txID)
	}

	updateInverted := func(m map[string][]int32) error {
		for key, ids := range m {
			existingVal, err := db.Get(key)
			var newIds []int32
			if err == nil && len(existingVal) > 0 {
				newIds = append(newIds, deserializeIDs(existingVal)...)
			}
			newIds = append(newIds, ids...)
			seen := make(map[int32]struct{})
			var deduped []int32
			for _, id := range newIds {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					deduped = append(deduped, id)
				}
			}
			newVal := serializeIDs(deduped)
			if err := db.Put(key, newVal); err != nil {
				return err
			}
		}
		return nil
	}

	if err := updateInverted(catMap); err != nil {
		return err
	}
	if err := updateInverted(countryMap); err != nil {
		return err
	}
	if err := updateInverted(channelMap); err != nil {
		return err
	}

	// 2. Update Sorted Indexes
	fields := []string{"id", "date", "region", "country", "item_type", "sales_channel", "order_priority", "ship_date", "units_sold", "unit_price", "unit_cost", "total_revenue", "total_cost", "total_profit"}

	for _, field := range fields {
		key := "sort:" + field
		existingVal, err := db.Get(key)
		var ids []int32
		if err == nil && len(existingVal) > 0 {
			ids = deserializeIDs(existingVal)
		}

		for _, tx := range txs {
			txID := int32(tx.ID)
			newVal, _, err := extractSortValue(tx, field)
			if err != nil {
				continue
			}
			ids = insertSorted(ids, txID, newVal, field)
		}

		newValBytes := serializeIDs(ids)
		if err := db.Put(key, newValBytes); err != nil {
			return err
		}
	}

	return nil
}

func handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state.mu.Lock()
	state.Total++
	newID := int(state.Total)
	state.mu.Unlock()

	// Random mock transaction data
	regions := []string{"North America", "Europe", "Asia", "Central America", "Sub-Saharan Africa", "Middle East", "Australia"}
	countries := []string{"United States", "United Kingdom", "Germany", "France", "Japan", "China", "Canada", "Australia", "India", "South Africa"}
	itemTypes := []string{"Fruits", "Clothes", "Cosmetics", "Baby Food", "Beverages", "Office Supplies", "Household", "Snacks", "Personal Care", "Cereal"}
	channels := []string{"Online", "Offline"}
	priorities := []string{"H", "M", "L", "C"}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	region := regions[rng.Intn(len(regions))]
	country := countries[rng.Intn(len(countries))]
	itemType := itemTypes[rng.Intn(len(itemTypes))]
	channel := channels[rng.Intn(len(channels))]
	priority := priorities[rng.Intn(len(priorities))]

	unitsSold := rng.Intn(9999) + 1
	unitPrice := rng.Float64()*400 + 10.0
	unitCost := unitPrice * 0.72
	totalRevenue := float64(unitsSold) * unitPrice
	totalCost := float64(unitsSold) * unitCost
	totalProfit := totalRevenue - totalCost

	tx := Transaction{
		ID:           newID,
		Date:         time.Now().Format("1/2/2006"),
		Region:       region,
		Country:      country,
		ItemType:     itemType,
		SalesChannel: channel,
		Priority:     priority,
		ShipDate:     time.Now().AddDate(0, 0, rng.Intn(10)+1).Format("1/2/2006"),
		UnitsSold:    unitsSold,
		UnitPrice:    unitPrice,
		UnitCost:     unitCost,
		TotalRevenue: totalRevenue,
		TotalCost:    totalCost,
		TotalProfit:  totalProfit,
	}

	jsonData := silentjson.Marshal(&tx, transReg, nil)
	docID := "tx:" + strconv.Itoa(tx.ID)

	if err := db.Put(docID, jsonData); err != nil {
		http.Error(w, "Failed to write to database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	recentMu.Lock()
	recentTransactions = append(recentTransactions, tx)
	shouldFlush := len(recentTransactions) >= 50
	recentMu.Unlock()

	if shouldFlush {
		flushBufferToDisk()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"transaction": tx,
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	recentMu.RLock()
	bufferedCount := len(recentTransactions)
	recentMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"buffered_count": bufferedCount,
	})
}

func handleFlushTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := flushBufferToDisk(); err != nil {
		http.Error(w, "Failed to flush indexes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func serializeIDs(ids []int32) []byte {
	if len(ids) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&ids[0])), len(ids)*4)
}

func deserializeIDs(b []byte) []int32 {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), len(b)/4)
}

func buildAndWriteIntIndex(key string, valExtractor func(tx *Transaction) int) {
	log.Printf("[MakoDB] Building index %s...", key)
	pairs := make([]SortPairInt, 0, state.Total)
	_ = db.ForEach(func(docID string, value []byte) error {
		if strings.HasPrefix(docID, "tx:") {
			var tx Transaction
			if err := silentjson.ParseObject(value, transReg, unsafe.Pointer(&tx)); err == nil {
				pairs = append(pairs, SortPairInt{ID: int32(tx.ID), Val: valExtractor(&tx)})
			}
		}
		return nil
	})
	slices.SortFunc(pairs, func(a, b SortPairInt) int {
		if a.Val != b.Val {
			return cmp.Compare(a.Val, b.Val)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	ids := make([]int32, len(pairs))
	for i, p := range pairs {
		ids[i] = p.ID
	}
	_ = db.Put(key, serializeIDs(ids))
}

func buildAndWriteFloatIndex(key string, valExtractor func(tx *Transaction) float64) {
	log.Printf("[MakoDB] Building index %s...", key)
	pairs := make([]SortPairFloat, 0, state.Total)
	_ = db.ForEach(func(docID string, value []byte) error {
		if strings.HasPrefix(docID, "tx:") {
			var tx Transaction
			if err := silentjson.ParseObject(value, transReg, unsafe.Pointer(&tx)); err == nil {
				pairs = append(pairs, SortPairFloat{ID: int32(tx.ID), Val: valExtractor(&tx)})
			}
		}
		return nil
	})
	slices.SortFunc(pairs, func(a, b SortPairFloat) int {
		if a.Val != b.Val {
			return cmp.Compare(a.Val, b.Val)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	ids := make([]int32, len(pairs))
	for i, p := range pairs {
		ids[i] = p.ID
	}
	_ = db.Put(key, serializeIDs(ids))
}

func buildAndWriteStrIndex(key string, valExtractor func(tx *Transaction) string) {
	log.Printf("[MakoDB] Building index %s...", key)
	pairs := make([]SortPairString, 0, state.Total)
	_ = db.ForEach(func(docID string, value []byte) error {
		if strings.HasPrefix(docID, "tx:") {
			var tx Transaction
			if err := silentjson.ParseObject(value, transReg, unsafe.Pointer(&tx)); err == nil {
				pairs = append(pairs, SortPairString{ID: int32(tx.ID), Val: valExtractor(&tx)})
			}
		}
		return nil
	})
	slices.SortFunc(pairs, func(a, b SortPairString) int {
		if a.Val != b.Val {
			return cmp.Compare(a.Val, b.Val)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	ids := make([]int32, len(pairs))
	for i, p := range pairs {
		ids[i] = p.ID
	}
	_ = db.Put(key, serializeIDs(ids))
}

func prepareDateStringInt(dateStr string) int32 {
	parts := strings.Split(dateStr, "/")
	if len(parts) != 3 {
		return 0
	}
	month, _ := strconv.Atoi(parts[0])
	day, _ := strconv.Atoi(parts[1])
	year, _ := strconv.Atoi(parts[2])
	return int32(year*10000 + month*100 + day)
}

type WorkerResult struct {
	sortDate         []SortPairInt
	sortShipDate     []SortPairInt
	sortRegion       []SortPairInt
	sortCountry      []SortPairInt
	sortItemType     []SortPairInt
	sortSalesChannel []SortPairInt
	sortPriority     []SortPairInt
	sortUnitsSold    []SortPairInt
	sortUnitPrice    []SortPairFloat
	sortUnitCost     []SortPairFloat
	sortTotalRevenue []SortPairFloat
	sortTotalCost    []SortPairFloat
	sortTotalProfit  []SortPairFloat
}

func generateAndImportMockDirect(count int) error {
	recentMu.Lock()
	recentTransactions = nil
	recentMu.Unlock()

	// Reopen/recreate MakoDB to ensure the database file is empty before importing
	db.Close()
	os.RemoveAll(dbPath)
	var openErr error
	db, openErr = makodb.OpenSharded(dbPath, dbNumShards, dbMaxTotalSize, dbNumBucketsPerShard)
	if openErr != nil {
		return fmt.Errorf("failed to recreate database for import: %w", openErr)
	}

	availMem := getAvailablePhysicalMemory()
	shardSize := dbMaxTotalSize / uint64(dbNumShards)

	log.Printf("[MakoDB] Direct progressive generation: available memory = %d MB, shard size = %d MB -> generating shard-by-shard",
		availMem/(1024*1024), shardSize/(1024*1024))

	globalIndexMap := make(map[string][]int32)
	var processedCount int64 = 0

	// Pre-group transaction IDs by shard index to avoid looping count times per shard in parallel workers
	log.Printf("[MakoDB] Pre-routing transactions to shards...")
	shardIDs := make([][]int32, dbNumShards)
	for i := 0; i < count; i++ {
		id := int32(i + 1)
		docID := "tx:" + strconv.Itoa(int(id))
		sIdx := db.GetShardIndex(docID)
		shardIDs[sIdx] = append(shardIDs[sIdx], id)
	}
	log.Printf("[MakoDB] Pre-routing completed.")

	shardChan := make(chan int, dbNumShards)
	for shardIdx := 0; shardIdx < dbNumShards; shardIdx++ {
		shardChan <- shardIdx
	}
	close(shardChan)

	const maxConcurrentShards = 2
	var writeWg sync.WaitGroup
	var writeErr error
	var writeErrMu sync.Mutex
	var indexMu sync.Mutex

	// Sorted helper slices for category-to-integer mappings
	regions := []string{"North America", "Europe", "Asia", "Central America", "Sub-Saharan Africa", "Middle East", "Australia"}
	countries := []string{"United States", "United Kingdom", "Germany", "France", "Japan", "China", "Canada", "Australia", "India", "South Africa"}
	itemTypes := []string{"Fruits", "Clothes", "Cosmetics", "Baby Food", "Beverages", "Office Supplies", "Household", "Snacks", "Personal Care", "Cereal"}
	channels := []string{"Online", "Offline"}
	priorities := []string{"H", "M", "L", "C"}

	slices.Sort(regions)
	slices.Sort(countries)
	slices.Sort(itemTypes)
	slices.Sort(channels)
	slices.Sort(priorities)

	// Slice allocations for workers to collect in-memory sort pairs
	results := make([]WorkerResult, maxConcurrentShards)

	for w := 0; w < maxConcurrentShards; w++ {
		writeWg.Add(1)
		go func(workerID int) {
			defer writeWg.Done()
			localIndexMap := make(map[string][]int32)

			// Allocate local slices. Target capacity is count/2 + some margin
			localCap := count/2 + 100000
			localSortDate := make([]SortPairInt, 0, localCap)
			localSortShipDate := make([]SortPairInt, 0, localCap)
			localSortRegion := make([]SortPairInt, 0, localCap)
			localSortCountry := make([]SortPairInt, 0, localCap)
			localSortItemType := make([]SortPairInt, 0, localCap)
			localSortSalesChannel := make([]SortPairInt, 0, localCap)
			localSortPriority := make([]SortPairInt, 0, localCap)
			localSortUnitsSold := make([]SortPairInt, 0, localCap)
			localSortUnitPrice := make([]SortPairFloat, 0, localCap)
			localSortUnitCost := make([]SortPairFloat, 0, localCap)
			localSortTotalRevenue := make([]SortPairFloat, 0, localCap)
			localSortTotalCost := make([]SortPairFloat, 0, localCap)
			localSortTotalProfit := make([]SortPairFloat, 0, localCap)

			for shardIdx := range shardChan {
				writeErrMu.Lock()
				hasErr := writeErr != nil
				writeErrMu.Unlock()
				if hasErr {
					return
				}

				log.Printf("[MakoDB] Worker %d generating and writing to shard %d/%d...", workerID, shardIdx, dbNumShards-1)

				idsInShard := shardIDs[shardIdx]
				for _, id := range idsInShard {
					docID := "tx:" + strconv.Itoa(int(id))
					rng := rand.New(rand.NewSource(int64(id)))

					regionIdx := rng.Intn(len(regions))
					countryIdx := rng.Intn(len(countries))
					itemTypeIdx := rng.Intn(len(itemTypes))
					salesChannelIdx := rng.Intn(len(channels))
					priorityIdx := rng.Intn(len(priorities))

					var tx Transaction
					tx.ID = int(id)
					orderDate := time.Now().AddDate(0, 0, -rng.Intn(365))
					tx.Date = orderDate.Format("1/2/2006")
					tx.Region = regions[regionIdx]
					tx.Country = countries[countryIdx]
					tx.ItemType = itemTypes[itemTypeIdx]
					tx.SalesChannel = channels[salesChannelIdx]
					tx.Priority = priorities[priorityIdx]
					tx.ShipDate = orderDate.AddDate(0, 0, rng.Intn(10)+1).Format("1/2/2006")
					tx.UnitsSold = rng.Intn(9999) + 1
					tx.UnitPrice = rng.Float64()*400 + 10.0
					tx.UnitCost = tx.UnitPrice * 0.72
					tx.TotalRevenue = float64(tx.UnitsSold) * tx.UnitPrice
					tx.TotalCost = float64(tx.UnitsSold) * tx.UnitCost
					tx.TotalProfit = tx.TotalRevenue - tx.TotalCost

					jsonData := silentjson.Marshal(&tx, transReg, nil)

					if err := db.Put(docID, jsonData); err != nil {
						writeErrMu.Lock()
						if writeErr == nil {
							writeErr = err
						}
						writeErrMu.Unlock()
						return
					}

					txID32 := int32(tx.ID)

					// Write directly to local memory slices (ultra-fast!)
					localSortDate = append(localSortDate, SortPairInt{ID: txID32, Val: int(prepareDateStringInt(tx.Date))})
					localSortShipDate = append(localSortShipDate, SortPairInt{ID: txID32, Val: int(prepareDateStringInt(tx.ShipDate))})
					localSortRegion = append(localSortRegion, SortPairInt{ID: txID32, Val: regionIdx})
					localSortCountry = append(localSortCountry, SortPairInt{ID: txID32, Val: countryIdx})
					localSortItemType = append(localSortItemType, SortPairInt{ID: txID32, Val: itemTypeIdx})
					localSortSalesChannel = append(localSortSalesChannel, SortPairInt{ID: txID32, Val: salesChannelIdx})
					localSortPriority = append(localSortPriority, SortPairInt{ID: txID32, Val: priorityIdx})
					localSortUnitsSold = append(localSortUnitsSold, SortPairInt{ID: txID32, Val: tx.UnitsSold})
					localSortUnitPrice = append(localSortUnitPrice, SortPairFloat{ID: txID32, Val: tx.UnitPrice})
					localSortUnitCost = append(localSortUnitCost, SortPairFloat{ID: txID32, Val: tx.UnitCost})
					localSortTotalRevenue = append(localSortTotalRevenue, SortPairFloat{ID: txID32, Val: tx.TotalRevenue})
					localSortTotalCost = append(localSortTotalCost, SortPairFloat{ID: txID32, Val: tx.TotalCost})
					localSortTotalProfit = append(localSortTotalProfit, SortPairFloat{ID: txID32, Val: tx.TotalProfit})

					if tx.ItemType != "" {
						key := "item_type:" + strings.ToLower(tx.ItemType)
						localIndexMap[key] = append(localIndexMap[key], txID32)
					}
					if tx.Country != "" {
						key := "country:" + strings.ToLower(tx.Country)
						localIndexMap[key] = append(localIndexMap[key], txID32)
					}
					if tx.SalesChannel != "" {
						key := "sales_channel:" + strings.ToLower(tx.SalesChannel)
						localIndexMap[key] = append(localIndexMap[key], txID32)
					}

					indexText := tx.ItemType + " " + tx.Country + " " + tx.SalesChannel
					tokens := makodb.Tokenize(indexText)
					for _, token := range tokens {
						localIndexMap[token] = append(localIndexMap[token], txID32)
					}

					currProcessed := atomic.AddInt64(&processedCount, 1)
					if currProcessed%50000 == 0 {
						updateProgress(currProcessed, int64(count))
					}
				}
			}

			results[workerID] = WorkerResult{
				sortDate:         localSortDate,
				sortShipDate:     localSortShipDate,
				sortRegion:       localSortRegion,
				sortCountry:      localSortCountry,
				sortItemType:     localSortItemType,
				sortSalesChannel: localSortSalesChannel,
				sortPriority:     localSortPriority,
				sortUnitsSold:    localSortUnitsSold,
				sortUnitPrice:    localSortUnitPrice,
				sortUnitCost:     localSortUnitCost,
				sortTotalRevenue: localSortTotalRevenue,
				sortTotalCost:    localSortTotalCost,
				sortTotalProfit:  localSortTotalProfit,
			}

			// Merge local indexes safely at the end of worker run
			indexMu.Lock()
			for token, ids := range localIndexMap {
				globalIndexMap[token] = append(globalIndexMap[token], ids...)
			}
			indexMu.Unlock()
		}(w)
	}
	writeWg.Wait()

	if writeErr != nil {
		return writeErr
	}

	// Write globalIndexMap to MakoDB sequentially (fast, zero lock contention)
	for token, ids := range globalIndexMap {
		indexKey := "idx:" + token
		uniqueIDs := make(map[int32]struct{}, len(ids))
		dedupedIDs := make([]int32, 0, len(ids))
		for _, id := range ids {
			if _, exists := uniqueIDs[id]; !exists {
				uniqueIDs[id] = struct{}{}
				dedupedIDs = append(dedupedIDs, id)
			}
		}
		if err := db.Put(indexKey, serializeIDs(dedupedIDs)); err != nil {
			return err
		}
	}

	state.mu.Lock()
	state.Total = int64(count)
	state.mu.Unlock()

	// 1. Write "sort:id" sequentially (takes 0.1s)
	log.Printf("[MakoDB] Writing index sort:id...")
	ids := make([]int32, count)
	for i := 0; i < count; i++ {
		ids[i] = int32(i + 1)
	}
	if err := db.Put("sort:id", serializeIDs(ids)); err != nil {
		return err
	}

	// Save total count key for instant startup
	if err := db.Put("state:total", serializeInt32(int32(count))); err != nil {
		return err
	}

	// Helper to merge, sort, and write Int index from worker results
	sortAndWriteIntIndex := func(dbKey string, selectSlice func(r *WorkerResult) []SortPairInt) error {
		log.Printf("[MakoDB] Sorting and writing index %s...", dbKey)
		pairs := make([]SortPairInt, 0, count)
		for w := 0; w < maxConcurrentShards; w++ {
			pairs = append(pairs, selectSlice(&results[w])...)
		}
		// Free worker slice fields
		for w := 0; w < maxConcurrentShards; w++ {
			switch dbKey {
			case "sort:date":
				results[w].sortDate = nil
			case "sort:ship_date":
				results[w].sortShipDate = nil
			case "sort:region":
				results[w].sortRegion = nil
			case "sort:country":
				results[w].sortCountry = nil
			case "sort:item_type":
				results[w].sortItemType = nil
			case "sort:sales_channel":
				results[w].sortSalesChannel = nil
			case "sort:priority":
				results[w].sortPriority = nil
			case "sort:units_sold":
				results[w].sortUnitsSold = nil
			}
		}

		slices.SortFunc(pairs, func(a, b SortPairInt) int {
			if a.Val != b.Val {
				return cmp.Compare(a.Val, b.Val)
			}
			return cmp.Compare(a.ID, b.ID)
		})

		ids := make([]int32, len(pairs))
		for i, p := range pairs {
			ids[i] = p.ID
		}
		return db.Put(dbKey, serializeIDs(ids))
	}

	// Helper to merge, sort, and write Float index from worker results
	sortAndWriteFloatIndex := func(dbKey string, selectSlice func(r *WorkerResult) []SortPairFloat) error {
		log.Printf("[MakoDB] Sorting and writing index %s...", dbKey)
		pairs := make([]SortPairFloat, 0, count)
		for w := 0; w < maxConcurrentShards; w++ {
			pairs = append(pairs, selectSlice(&results[w])...)
		}
		// Free worker slice fields
		for w := 0; w < maxConcurrentShards; w++ {
			switch dbKey {
			case "sort:unit_price":
				results[w].sortUnitPrice = nil
			case "sort:unit_cost":
				results[w].sortUnitCost = nil
			case "sort:total_revenue":
				results[w].sortTotalRevenue = nil
			case "sort:total_cost":
				results[w].sortTotalCost = nil
			case "sort:total_profit":
				results[w].sortTotalProfit = nil
			}
		}

		slices.SortFunc(pairs, func(a, b SortPairFloat) int {
			if a.Val != b.Val {
				return cmp.Compare(a.Val, b.Val)
			}
			return cmp.Compare(a.ID, b.ID)
		})

		ids := make([]int32, len(pairs))
		for i, p := range pairs {
			ids[i] = p.ID
		}
		return db.Put(dbKey, serializeIDs(ids))
	}

	// 2. Sort and write other indexes in-memory sequentially
	if err := sortAndWriteIntIndex("sort:date", func(r *WorkerResult) []SortPairInt { return r.sortDate }); err != nil {
		return err
	}
	if err := sortAndWriteIntIndex("sort:ship_date", func(r *WorkerResult) []SortPairInt { return r.sortShipDate }); err != nil {
		return err
	}
	if err := sortAndWriteIntIndex("sort:region", func(r *WorkerResult) []SortPairInt { return r.sortRegion }); err != nil {
		return err
	}
	if err := sortAndWriteIntIndex("sort:country", func(r *WorkerResult) []SortPairInt { return r.sortCountry }); err != nil {
		return err
	}
	if err := sortAndWriteIntIndex("sort:item_type", func(r *WorkerResult) []SortPairInt { return r.sortItemType }); err != nil {
		return err
	}
	if err := sortAndWriteIntIndex("sort:sales_channel", func(r *WorkerResult) []SortPairInt { return r.sortSalesChannel }); err != nil {
		return err
	}
	if err := sortAndWriteIntIndex("sort:priority", func(r *WorkerResult) []SortPairInt { return r.sortPriority }); err != nil {
		return err
	}
	if err := sortAndWriteIntIndex("sort:units_sold", func(r *WorkerResult) []SortPairInt { return r.sortUnitsSold }); err != nil {
		return err
	}

	if err := sortAndWriteFloatIndex("sort:unit_price", func(r *WorkerResult) []SortPairFloat { return r.sortUnitPrice }); err != nil {
		return err
	}
	if err := sortAndWriteFloatIndex("sort:unit_cost", func(r *WorkerResult) []SortPairFloat { return r.sortUnitCost }); err != nil {
		return err
	}
	if err := sortAndWriteFloatIndex("sort:total_revenue", func(r *WorkerResult) []SortPairFloat { return r.sortTotalRevenue }); err != nil {
		return err
	}
	if err := sortAndWriteFloatIndex("sort:total_cost", func(r *WorkerResult) []SortPairFloat { return r.sortTotalCost }); err != nil {
		return err
	}
	if err := sortAndWriteFloatIndex("sort:total_profit", func(r *WorkerResult) []SortPairFloat { return r.sortTotalProfit }); err != nil {
		return err
	}

	updateProgress(int64(count), int64(count))
	return nil
}
