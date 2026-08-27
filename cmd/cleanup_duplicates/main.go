package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	token := os.Getenv("TOKEN")
	if token == "" {
		fmt.Println("Usage: TOKEN=<jwt> ./cleanup_duplicates")
		os.Exit(1)
	}

	client := &http.Client{}
	baseURL := "http://localhost:9090"

	// Get all EAN pages
	req, _ := http.NewRequest("GET", baseURL+"/admin/eanpages?limit=1000", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Items []struct {
			ID   int64  `json:"id"`
			EAN  string `json:"ean"`
			Slug string `json:"slug"`
		} `json:"items"`
	}
	json.Unmarshal(body, &result)

	fmt.Printf("Total EAN pages: %d\n", len(result.Items))

	// Group by EAN
	byEAN := make(map[string][]int64)
	for _, item := range result.Items {
		if item.EAN != "" {
			byEAN[item.EAN] = append(byEAN[item.EAN], item.ID)
		}
	}

	// Find duplicates
	duplicates := 0
	var toDelete []int64
	for ean, ids := range byEAN {
		if len(ids) > 1 {
			duplicates++
			// Keep the first one, delete the rest
			for _, id := range ids[1:] {
				toDelete = append(toDelete, id)
				fmt.Printf("  Duplicate EAN %s: keeping %d, deleting %d\n", ean, ids[0], id)
			}
		}
	}

	fmt.Printf("\nFound %d duplicate EANs, %d pages to delete\n", duplicates, len(toDelete))

	if len(toDelete) == 0 {
		return
	}

	// Delete duplicates
	for _, id := range toDelete {
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/admin/eanpages/%d", baseURL, id), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("  Error deleting %d: %v\n", id, err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("  Deleted EAN page %d\n", id)
	}

	fmt.Println("\nDone!")
}
