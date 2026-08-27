// Command add_type_keywords adds product-type keywords to category anchor_keywords.
//
// Usage:
//
//	add_type_keywords <token> <mapping.json>
//
// The mapping.json file maps category IDs (as strings) to arrays of keywords:
//
//	{
//	  "39": ["blender", "mikser", "czajnik"],
//	  "201": ["klocki", "maskotka", "pluszak"]
//	}
//
// For each category, the keywords are MERGED with existing anchor_keywords
// (no duplicates). Then POST /admin/catalogizer/train is called to rebuild
// the token indexes.
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

const baseURL = "http://localhost:9090"

type Category struct {
	ID             int      `json:"id"`
	AnchorKeywords []string `json:"anchor_keywords"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: add_type_keywords <token> <mapping.json>")
		os.Exit(1)
	}

	token := os.Args[1]
	mappingPath := os.Args[2]

	// Read mapping file
	data, err := os.ReadFile(mappingPath)
	if err != nil {
		fmt.Printf("Error reading mapping file: %v\n", err)
		os.Exit(1)
	}

	var mapping map[string][]string
	if err := json.Unmarshal(data, &mapping); err != nil {
		fmt.Printf("Error parsing mapping JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Processing %d categories...\n", len(mapping))

	success := 0
	failed := 0
	skipped := 0

	// Sort category IDs for deterministic processing
	ids := make([]int, 0, len(mapping))
	for idStr := range mapping {
		var id int
		if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil {
			ids = append(ids, id)
		}
	}

	for _, catID := range ids {
		newKeywords := mapping[fmt.Sprintf("%d", catID)]
		if len(newKeywords) == 0 {
			continue
		}

		// Fetch current category to get existing keywords
		current, err := getCategory(token, catID)
		if err != nil {
			fmt.Printf("ERROR: cat %d: fetch: %v\n", catID, err)
			failed++
			continue
		}

		// Merge keywords (preserve existing, add new)
		merged := mergeKeywords(current.AnchorKeywords, newKeywords)

		// If no change, skip
		if sameKeywords(current.AnchorKeywords, merged) {
			skipped++
			continue
		}

		// Update category
		if err := updateCategory(token, catID, merged); err != nil {
			fmt.Printf("ERROR: cat %d: %v\n", catID, err)
			failed++
		} else {
			fmt.Printf("OK: cat %d: +%d keywords (total %d)\n", catID, len(newKeywords), len(merged))
			success++
		}
	}

	fmt.Printf("\nDone! Success: %d, Failed: %d, Skipped: %d\n", success, failed, skipped)

	// Rebuild catalogizer token indexes
	if success > 0 {
		fmt.Println("\nRebuilding catalogizer token indexes...")
		if err := trainCatalogizer(token); err != nil {
			fmt.Printf("WARN: catalogizer train: %v\n", err)
		} else {
			fmt.Println("Catalogizer token indexes rebuilt.")
		}
	}
}

func mergeKeywords(existing, new []string) []string {
	seen := make(map[string]bool)
	merged := make([]string, 0, len(existing)+len(new))

	for _, kw := range existing {
		kw = strings.TrimSpace(kw)
		if kw == "" || seen[kw] {
			continue
		}
		seen[kw] = true
		merged = append(merged, kw)
	}

	for _, kw := range new {
		kw = strings.TrimSpace(kw)
		if kw == "" || seen[kw] {
			continue
		}
		seen[kw] = true
		merged = append(merged, kw)
	}

	return merged
}

func sameKeywords(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	setA := make(map[string]bool)
	for _, kw := range a {
		setA[kw] = true
	}
	for _, kw := range b {
		if !setA[kw] {
			return false
		}
	}
	return true
}

func getCategory(token string, catID int) (*Category, error) {
	url := fmt.Sprintf("%s/admin/categories/%d", baseURL, catID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var cat Category
	if err := json.NewDecoder(resp.Body).Decode(&cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

func updateCategory(token string, catID int, keywords []string) error {
	body := map[string]interface{}{
		"anchor_keywords": keywords,
	}
	bodyJSON, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/admin/categories/%d", baseURL, catID)
	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func trainCatalogizer(token string) error {
	url := baseURL + "/admin/catalogizer/train"
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
