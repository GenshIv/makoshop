package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

// LenovoAPIClient wraps HTTP client for Lenovo PSREF API
type LenovoAPIClient struct {
	client  *http.Client
	baseURL string
}

func NewLenovoAPIClient() *LenovoAPIClient {
	return &LenovoAPIClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: "https://psref.lenovo.com",
	}
}

// LenovoAPIResponse represents the API response structure
type LenovoAPIResponse struct {
	Code    int            `json:"code"`
	Message string         `json:"msg"`
	Data    *LenovoAPIData `json:"data,omitempty"`
}

type LenovoAPIData struct {
	Total int        `json:"total"`
	Cols  []string   `json:"cols"`
	Rows  [][]string `json:"rows"`
}

// SearchModels searches for Lenovo models by product key
func (c *LenovoAPIClient) SearchModels(productKey string, page, pageSize int) (*LenovoAPIData, error) {
	params := url.Values{}
	params.Set("pageindex", fmt.Sprintf("%d", page))
	params.Set("pagesize", fmt.Sprintf("%d", pageSize))
	params.Set("product_key", productKey)
	params.Set("search", "")
	params.Set("search_rule", "1")
	params.Set("orderby", "")
	params.Set("In_positive_or_reverse_order", "0")
	params.Set("filter_option", "")
	params.Set("mt", "")
	params.Set("vertical", "true")

	reqURL := fmt.Sprintf("%s/api/search/DefinitionFilterAndSearch/ShowModel?%s", c.baseURL, params.Encode())

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	// Browser-like headers required by the API
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://psref.lenovo.com")
	req.Header.Set("Referer", "https://psref.lenovo.com/")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Remove UTF-8 BOM if present
	if len(body) > 0 && body[0] == 0xEF && body[1] == 0xBB && body[2] == 0xBF {
		body = body[3:]
	}

	var apiResp LenovoAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	if apiResp.Code != 1 {
		return nil, fmt.Errorf("API error: code=%d msg=%s", apiResp.Code, apiResp.Message)
	}

	return apiResp.Data, nil
}

// HandleLenovoImport handles POST /admin/import-lenovo
func (h *Handlers) HandleLenovoImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProductKey string `json:"product_key"`
		Page       int    `json:"page"`
		Pagesize   int    `json:"pagesize"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ProductKey == "" {
		http.Error(w, "product_key is required", http.StatusBadRequest)
		return
	}

	if req.Page == 0 {
		req.Page = 1
	}
	if req.Pagesize == 0 {
		req.Pagesize = 30
	}

	client := NewLenovoAPIClient()
	data, err := client.SearchModels(req.ProductKey, req.Page, req.Pagesize)
	if err != nil {
		log.Printf("Lenovo API error: %v", err)
		http.Error(w, fmt.Sprintf("API error: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Lenovo import: found %d models for %s", data.Total, req.ProductKey)

	// Convert API rows to products and upsert
	imported := 0
	for _, row := range data.Rows {
		product := h.rowToProduct(data.Cols, row, req.ProductKey)
		if product == nil {
			continue
		}

		if err := h.eanPageRepo.UpsertFromProduct(product); err != nil {
			log.Printf("Warning: failed to upsert product %s: %v", product.EAN, err)
		} else {
			imported++
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "success",
		"product_key": req.ProductKey,
		"total":       data.Total,
		"received":    len(data.Rows),
		"imported":    imported,
	})
}

// rowToProduct converts an API row to a Product model
func (h *Handlers) rowToProduct(cols []string, row []string, productKey string) *model.Product {
	// Build column index map
	colIndex := make(map[string]int)
	for i, col := range cols {
		colIndex[col] = i
	}

	getCol := func(name string) string {
		if idx, ok := colIndex[name]; ok && idx < len(row) {
			return row[idx]
		}
		return ""
	}

	modelNum := getCol("Model")
	if modelNum == "" {
		return nil
	}

	// Clean up model number (remove ** suffix)
	modelNum = strings.TrimSuffix(modelNum, "**")

	product := &model.Product{
		EAN:         fmt.Sprintf("LEN-%s", modelNum), // Use model as EAN identifier
		Name:        fmt.Sprintf("Lenovo %s (%s)", getCol("Product"), modelNum),
		Brand:       "Lenovo",
		Description: "",
		Attributes:  []model.KeyValue{},
	}

	// Map key specs to attributes
	specFields := map[string]string{
		"Processor":          "processor",
		"Graphics":           "graphics",
		"Memory":             "memory",
		"Storage":            "storage",
		"Display":            "display",
		"Operating System":   "os",
		"Battery":            "battery",
		"Power Adapter":      "power_adapter",
		"Keyboard":           "keyboard",
		"Case Material":      "case_material",
		"Color":              "color",
		"Camera":             "camera",
		"WLAN + Bluetooth":   "wireless",
		"WWAN":               "wwan",
		"Fingerprint Reader": "fingerprint",
		"TPM":                "tpm",
		"Warranty":           "warranty",
		"Machine Type":       "machine_type",
		"Region":             "region",
	}

	for apiCol, attrKey := range specFields {
		value := getCol(apiCol)
		if value != "" && value != "None" {
			product.Attributes = append(product.Attributes, model.KeyValue{
				Key:   attrKey,
				Value: truncateString(value, 40),
			})
		}
	}

	return product
}

func truncateString(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
