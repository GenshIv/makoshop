// ean-translates processes raw eanpage data from _raw_sorted/page_N.json files,
// sends each item to a local llama.cpp OpenAI-compatible API for description
// generation and attribute extraction, and saves results as structured JSON.
//
// Features:
//   - Processes pages sequentially, items within a page concurrently
//   - Dedup by SHA256 hash of (ean + title + description)
//   - Resumable: tracks progress in .progress.json, skips already-processed items
//   - N concurrent API requests (configurable via -concurrency)
//   - JSON validation of API responses
//   - Continues after errors (logs skipped items, processes next)
//
// Usage:
//
//	go run ./cmd/ean-translates [-dir=ean_translates/_raw_sorted] [-out=ean_translates/descriptions]
//	   [-api=http://localhost:1234/v1/chat/completions] [-concurrency=5] [-page=0]
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- raw input types ----------

type RawItem struct {
	EAN         string         `json:"ean"`
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Content     string         `json:"content"`
	Images      []string       `json:"images"`
	CategoryID  int64          `json:"category_id"`
	Brand       string         `json:"brand"`
	BrandID     int64          `json:"brand_id"`
	IsActive    bool           `json:"is_active"`
	MinPrice    float64        `json:"min_price"`
	Currency    string         `json:"currency"`
	Attributes  []KeyValue     `json:"attributes"`
	ProductCount int          `json:"product_count"`
	LikeCount   int            `json:"like_count"`
	DislikeCount int           `json:"dislike_count"`
	CreatedAt   int64          `json:"created_at"`
	UpdatedAt   int64          `json:"updated_at"`
	SEOURL      string         `json:"seo_url"`
	Keywords    string         `json:"keywords"`
}

type RawPage struct {
	Total int       `json:"total"`
	Items []RawItem `json:"items"`
}

type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ---------- output types ----------

type ProcessedItem struct {
	EAN            string            `json:"ean"`
	URL            string            `json:"url"`
	SEOURL         string            `json:"seo_url"`
	Title          string            `json:"title"`
	Keywords       []string          `json:"keywords"`
	SourceAttrs    []KeyValue        `json:"source_attributes"`
	ExtractedFields map[string]string `json:"extracted_fields"`
	Descriptions   map[string]string `json:"descriptions"`
	GeneratedAt    string            `json:"generated_at"`
}

// ---------- progress tracking ----------

type Progress struct {
	LastPage       int       `json:"last_page"`
	TotalProcessed int       `json:"total_processed"`
	CurrentItem    string    `json:"current_item,omitempty"` // ean of item being processed
	LastHash       string    `json:"last_hash,omitempty"`
	Errors         int       `json:"errors"`
	UpdatedAt      string    `json:"updated_at"`
}

func loadProgress(path string) Progress {
	p := Progress{}
	data, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return Progress{}
	}
	return p
}

func saveProgress(path string, p Progress) {
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0644)
}

// ---------- dedup hash ----------

func itemHash(item RawItem) string {
	h := sha256.New()
	h.Write([]byte(item.EAN))
	h.Write([]byte(item.Title))
	h.Write([]byte(item.Description))
	h.Write([]byte(item.Content))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func isProcessed(descDir string, item RawItem) bool {
	// Check if output file already exists
	seoPath := sanitizePath(item.SEOURL)
	outDir := filepath.Join(descDir, seoPath)
	eanFile := sanitizeFileName(item.EAN)
	outPath := filepath.Join(outDir, eanFile+".json")
	if _, err := os.Stat(outPath); err == nil {
		return true
	}
	// Also check by hash
	hashFile := filepath.Join(descDir, "..", ".hash_cache", itemHash(item)+".json")
	if _, err := os.Stat(hashFile); err == nil {
		return true
	}
	return false
}

// ---------- OpenAI-compatible API ----------

type OpenAIRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Temperature *float64   `json:"temperature,omitempty"`
	MaxTokens    *int      `json:"max_tokens,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIResponse struct {
	Choices []Choice `json:"choices"`
	Error   *APIError `json:"error,omitempty"`
}

type Choice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func callLLM(apiURL, prompt string) (string, error) {
	reqBody := OpenAIRequest{
		Model: "local",
		Messages: []ChatMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: floatPtr(0.7),
		MaxTokens:   intPtr(16000),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var openResp OpenAIResponse
	if err := json.Unmarshal(body, &openResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	if openResp.Error != nil {
		return "", fmt.Errorf("API error: %s", openResp.Error.Message)
	}

	if len(openResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	if openResp.Choices[0].FinishReason == "length" {
		return "", fmt.Errorf("response truncated (max tokens)")
	}

	return openResp.Choices[0].Message.Content, nil
}

func floatPtr(f float64) *float64  { return &f }
func intPtr(i int) *int            { return &i }

// ---------- prompt builder ----------

func buildPrompt(item RawItem) string {
	var sb strings.Builder

	sb.WriteString(`You are a product data specialist. Analyze the following product and output a JSON object.

Rules:
1. Extract ALL attributes from the title, description, and source_attributes — including technical specs hidden in text
2. Generate natural, human-written descriptions in 5 languages (pl, ua, ru, be, en) — 3-4 paragraphs each + brief spec list
3. Descriptions must be natural, not mechanical — write as if a knowledgeable salesperson wrote them
4. extracted_fields should contain ALL technical parameters found anywhere in the data
5. Keywords should be a compact array of relevant search terms
6. Output ONLY valid JSON, no markdown, no explanation

`)

	if item.Title != "" {
		sb.WriteString(fmt.Sprintf("Title: %s\n\n", item.Title))
	}

	if item.Description != "" {
		sb.WriteString(fmt.Sprintf("Description (Polish): %s\n\n", item.Description))
	}

	if item.Content != "" && item.Content != item.Description {
		sb.WriteString(fmt.Sprintf("Additional content: %s\n\n", item.Content))
	}

	if len(item.Attributes) > 0 {
		sb.WriteString("Source attributes:\n")
		for _, a := range item.Attributes {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", a.Key, a.Value))
		}
		sb.WriteString("\n")
	}

	if item.Keywords != "" {
		sb.WriteString(fmt.Sprintf("Existing keywords: %s\n\n", item.Keywords))
	}

	if item.Brand != "" {
		sb.WriteString(fmt.Sprintf("Brand: %s\n\n", item.Brand))
	}

	if item.SEOURL != "" {
		sb.WriteString(fmt.Sprintf("SEO URL: %s\n\n", item.SEOURL))
	}

	sb.WriteString(`Output format (JSON only):
{
  "extracted_fields": {
    "field1": "value1",
    "field2": "value2"
  },
  "keywords": ["kw1", "kw2", "kw3"],
  "descriptions": {
    "pl": "Polish description here...",
    "ua": "Ukrainian description here...",
    "ru": "Russian description here...",
    "be": "Belarusian description here...",
    "en": "English description here..."
  }
}

IMPORTANT: Return ONLY the JSON object, nothing else. No markdown code fences, no explanation.`)

	return sb.String()
}

func parseLLMResponse(raw string) (*ProcessedItem, error) {
	// Try to find JSON in the response (in case of markdown wrapping)
	raw = strings.TrimSpace(raw)
	// Remove markdown code fences if present
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// Find the first { and last } to extract JSON
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	jsonStr := raw[start : end+1]

	// Validate JSON
	if !json.Valid([]byte(jsonStr)) {
		return nil, fmt.Errorf("invalid JSON in response")
	}

	// Parse into a temp structure
	var tmp struct {
		ExtractedFields map[string]string `json:"extracted_fields"`
		Keywords        []string          `json:"keywords"`
		Descriptions    map[string]string `json:"descriptions"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &tmp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate required fields
	if tmp.Descriptions == nil {
		return nil, fmt.Errorf("missing descriptions in response")
	}
	for _, lang := range []string{"pl", "ua", "ru", "be", "en"} {
		if tmp.Descriptions[lang] == "" {
			return nil, fmt.Errorf("missing description for language: %s", lang)
		}
	}

	if tmp.ExtractedFields == nil {
		tmp.ExtractedFields = make(map[string]string)
	}
	if tmp.Keywords == nil {
		tmp.Keywords = []string{}
	}

	return &ProcessedItem{
		ExtractedFields: tmp.ExtractedFields,
		Keywords:        tmp.Keywords,
		Descriptions:    tmp.Descriptions,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// ---------- file helpers ----------

func sanitizePath(p string) string {
	p = strings.TrimLeft(p, "/")
	p = strings.ReplaceAll(p, `"`, "")
	p = strings.ReplaceAll(p, `\`, "_")
	return p
}

func safeShort(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, `"`, "_")
	name = strings.ReplaceAll(name, `\`, "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "_")
	// Truncate to reasonable length
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// ---------- main worker ----------

func main() {
	dir := flag.String("dir", "ean_translates/_raw_sorted", "directory with page_N.json files")
	out := flag.String("out", "ean_translates/descriptions", "output directory for processed files")
	hashCache := flag.String("hash_cache", "ean_translates/.hash_cache", "directory for hash cache")
	apiURL := flag.String("api", "http://localhost:1234/v1/chat/completions", "llama.cpp OpenAI-compatible API URL")
	concurrency := flag.Int("concurrency", 5, "number of concurrent API requests")
	page := flag.Int("page", 0, "process only this page (0 = all)")
	flag.Parse()

	progressFile := filepath.Join(*dir, "..", ".progress.json")
	progress := loadProgress(progressFile)
	fmt.Printf("Loaded progress: page=%d processed=%d errors=%d\n",
		progress.LastPage, progress.TotalProcessed, progress.Errors)

	// Ensure output directories exist
	os.MkdirAll(*out, 0755)
	os.MkdirAll(*hashCache, 0755)

	// Collect page files
	var pageFiles []string
	files, _ := filepath.Glob(filepath.Join(*dir, "page_*.json"))
	for _, f := range files {
		pageFiles = append(pageFiles, f)
	}
	sort.Strings(pageFiles)

	if len(pageFiles) == 0 {
		fmt.Println("No page_N.json files found")
		os.Exit(1)
	}

	fmt.Printf("Found %d page files\n", len(pageFiles))

	// Process each page
	for _, pageFile := range pageFiles {
		baseName := filepath.Base(pageFile)
		// Extract page number
		pageNum := 0
		fmt.Sscanf(baseName, "page_%d.json", &pageNum)

		// Skip if -page is set and doesn't match
		if *page > 0 && pageNum != *page {
			continue
		}

		// Skip already completed pages (resume logic)
		if pageNum < progress.LastPage {
			fmt.Printf("SKIP page %d (already processed)\n", pageNum)
			continue
		}

		fmt.Printf("\n=== Processing page %d (%s) ===\n", pageNum, baseName)

		// Load page data
		pageData, err := loadPage(pageFile)
		if err != nil {
			fmt.Printf("ERROR loading page %d: %v\n", pageNum, err)
			progress.Errors++
			saveProgress(progressFile, progress)
			continue
		}

		fmt.Printf("Page %d: %d items\n", pageNum, len(pageData.Items))

		// Process items with concurrency limit
		processed, skipped, errs := processPageItems(pageData, *out, *hashCache, *apiURL, *concurrency, progress.TotalProcessed)

		progress.LastPage = pageNum
		progress.TotalProcessed += processed
		progress.Errors += errs

		fmt.Printf("Page %d done: processed=%d skipped=%d errors=%d\n",
			pageNum, processed, skipped, errs)

		saveProgress(progressFile, progress)
	}

	fmt.Printf("\n=== Complete ===\n")
	fmt.Printf("Total processed: %d\n", progress.TotalProcessed)
	fmt.Printf("Total errors: %d\n", progress.Errors)
}

func loadPage(path string) (*RawPage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var page RawPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}

	return &page, nil
}

func processPageItems(page *RawPage, outDir, hashCacheDir, apiURL string, concurrency int, baseCount int) (int, int, int) {
	var (
		mu        sync.Mutex
		processed int
		skipped   int
		errors    int
	)

	// Filter out already processed items
	var items []RawItem
	for _, item := range page.Items {
		if isProcessed(outDir, item) {
			skipped++
			continue
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		return 0, skipped, 0
	}

	fmt.Printf("  Items to process: %d (skipped: %d)\n", len(items), skipped)

	// Semaphore for concurrency control
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// Sort items by EAN for deterministic processing
	sort.Slice(items, func(i, j int) bool {
		return items[i].EAN < items[j].EAN
	})

	for i := range items {
		item := &items[i]
		wg.Add(1)
		sem <- struct{}{} // acquire slot

		go func(it *RawItem) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			hash := itemHash(*it)

			// Check hash dedup
			hashFile := filepath.Join(hashCacheDir, hash+".json")
			if _, err := os.Stat(hashFile); err == nil {
				mu.Lock()
				skipped++
				mu.Unlock()
				return
			}

			// Build prompt
			prompt := buildPrompt(*it)

			// Call LLM
			raw, err := callLLM(apiURL, prompt)
			if err != nil {
				fmt.Printf("  LLM error for %s: %v\n", safeShort(it.EAN, 50), err)
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			// Parse response
			result, err := parseLLMResponse(raw)
			if err != nil {
				fmt.Printf("  Parse error for %s: %v\n", safeShort(it.EAN, 50), err)
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			// Enrich result with item data
			result.EAN = it.EAN
			result.Title = it.Title
			result.SEOURL = it.SEOURL

			// Build URL
			if it.SEOURL != "" {
				result.URL = "https://wszyst.pl" + it.SEOURL
			} else {
				result.URL = ""
			}

			// Source attributes
			result.SourceAttrs = it.Attributes

			// Keywords
			if len(result.Keywords) == 0 && it.Keywords != "" {
				result.Keywords = strings.Fields(it.Keywords)
			}

			// Save output
			seoPath := sanitizePath(it.SEOURL)
			outDir := filepath.Join(outDir, seoPath)
			os.MkdirAll(outDir, 0755)

			eanFile := sanitizeFileName(it.EAN)
			outPath := filepath.Join(outDir, eanFile+".json")

			outputData, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				fmt.Printf("  Marshal error for %s: %v\n", safeShort(it.EAN, 50), err)
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			if err := os.WriteFile(outPath, outputData, 0644); err != nil {
				fmt.Printf("  Write error for %s: %v\n", safeShort(it.EAN, 50), err)
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			// Save hash
			os.WriteFile(hashFile, []byte(hash), 0644)

			mu.Lock()
			processed++
			mu.Unlock()

			fmt.Printf("  OK %s (%d/%d)\n", safeShort(it.EAN, 50), baseCount+processed, len(items))
		}(item)
	}

	wg.Wait()
	return processed, skipped, errors
}
