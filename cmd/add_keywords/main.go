package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type Category struct {
	ID             int      `json:"id"`
	ParentID       int      `json:"parent_id"`
	NameRU         string   `json:"name_ru"`
	NameUA         string   `json:"name_ua"`
	NamePL         string   `json:"name_pl"`
	NameEN         string   `json:"name_en"`
	Slug           string   `json:"slug"`
	IsActive       bool     `json:"is_active"`
	SortOrder      int      `json:"sort_order"`
	AnchorKeywords []string `json:"anchor_keywords"`
}

type ExportFile struct {
	Categories []Category `json:"categories"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: add_keywords <token> <export_file>")
		os.Exit(1)
	}

	token := os.Args[1]
	filePath := os.Args[2]
	baseURL := "http://localhost:9090"

	// Read export file
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var export ExportFile
	if err := json.Unmarshal(data, &export); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Processing %d categories...\n", len(export.Categories))

	success := 0
	failed := 0

	for _, cat := range export.Categories {
		// Skip if category is not active
		if !cat.IsActive {
			continue
		}

		// Collect keywords from all language variants
		keywords := make([]string, 0)
		seen := make(map[string]bool)

		// Add keywords from English name
		if cat.NameEN != "" {
			for _, kw := range extractKeywords(cat.NameEN) {
				if !seen[kw] {
					keywords = append(keywords, kw)
					seen[kw] = true
				}
			}
		}

		// Add keywords from Polish name
		if cat.NamePL != "" {
			for _, kw := range extractKeywords(cat.NamePL) {
				if !seen[kw] {
					keywords = append(keywords, kw)
					seen[kw] = true
				}
			}
		}

		// Add keywords from Russian name
		if cat.NameRU != "" {
			for _, kw := range extractKeywords(cat.NameRU) {
				if !seen[kw] {
					keywords = append(keywords, kw)
					seen[kw] = true
				}
			}
		}

		if len(keywords) == 0 {
			continue
		}

		// Update category
		displayName := cat.NameEN
		if displayName == "" {
			displayName = cat.NameRU
		}
		if displayName == "" {
			displayName = cat.NamePL
		}

		if err := updateCategory(baseURL, token, cat.ID, keywords); err != nil {
			fmt.Printf("ERROR: cat %d (%s): %v\n", cat.ID, displayName, err)
			failed++
		} else {
			fmt.Printf("OK: cat %d (%s) -> %v\n", cat.ID, displayName, keywords)
			success++
		}
	}

	fmt.Printf("\nDone! Success: %d, Failed: %d\n", success, failed)
}

func extractKeywords(name string) []string {
	// Split by spaces and clean up
	words := strings.Fields(name)
	keywords := make([]string, 0, len(words))

	for _, word := range words {
		// Keep letters (including Polish diacritics and Cyrillic) and digits,
		// replace other characters with spaces.
		var b strings.Builder
		for _, r := range word {
			if isLetterOrDigit(r) {
				b.WriteRune(r)
			} else {
				b.WriteRune(' ')
			}
		}
		for _, w := range strings.Fields(b.String()) {
			if w != "" {
				keywords = append(keywords, w)
			}
		}
	}

	return keywords
}

// isLetterOrDigit returns true for letters (Latin, Polish diacritics, Cyrillic) and digits.
func isLetterOrDigit(r rune) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	// Latin letters (including Polish diacritics: ą ć ę ł ń ó ś ź ż and uppercase)
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
		return true
	}
	// Polish diacritics
	switch r {
	case 'ą', 'ć', 'ę', 'ł', 'ń', 'ó', 'ś', 'ź', 'ż',
		'Ą', 'Ć', 'Ę', 'Ł', 'Ń', 'Ó', 'Ś', 'Ź', 'Ż':
		return true
	}
	// Cyrillic (Russian/Ukrainian)
	if (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') ||
		(r >= 'ё' && r <= 'ї') || (r >= 'Ё' && r <= 'Ї') {
		return true
	}
	return false
}

func updateCategory(baseURL, token string, catID int, keywords []string) error {
	// Prepare request body
	body := map[string]interface{}{
		"anchor_keywords": keywords,
	}
	bodyJSON, _ := json.Marshal(body)

	// Create request
	url := fmt.Sprintf("%s/admin/categories/%d", baseURL, catID)
	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
