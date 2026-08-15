//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type InitialData struct {
	Items []struct {
		Slug string `json:"slug"`
	} `json:"items"`
	Total int `json:"total"`
}

func main() {
	baseURL := "http://localhost:9090"

	// Extract categories from tree
	fmt.Println("Fetching categories...")
	categories := fetchCategories(baseURL)
	if err := saveList("categories.txt", categories); err != nil {
		fmt.Println("Error saving categories:", err)
		os.Exit(1)
	}
	fmt.Printf("Saved %d categories\n", len(categories))

	// Extract slugs from a few category pages
	fmt.Println("Fetching slugs...")
	slugs := fetchSlugs(baseURL, categories)
	if err := saveList("slugs.txt", slugs); err != nil {
		fmt.Println("Error saving slugs:", err)
		os.Exit(1)
	}
	fmt.Printf("Saved %d slugs\n", len(slugs))
}

func fetchCategories(baseURL string) []string {
	resp, err := http.Get(baseURL + "/categories/tree")
	if err != nil {
		fmt.Println("Error fetching categories:", err)
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tree []Category
	if err := json.Unmarshal(body, &tree); err != nil {
		fmt.Println("Error parsing categories:", err)
		return nil
	}

	var categories []string
	var walk func([]Category)
	walk = func(nodes []Category) {
		for _, c := range nodes {
			if c.IsActive && c.Slug != "" {
				categories = append(categories, c.Slug)
			}
			if len(c.Children) > 0 {
				walk(c.Children)
			}
		}
	}
	walk(tree)
	return categories
}

type Category struct {
	ID       int        `json:"id"`
	Name     string     `json:"name"`
	Slug     string     `json:"slug"`
	IsActive bool       `json:"is_active"`
	Children []Category `json:"children"`
}

func fetchSlugs(baseURL string, categories []string) []string {
	var slugs []string
	seen := make(map[string]bool)

	// Limit to first 20 categories to avoid too many requests
	limit := 20
	if len(categories) < limit {
		limit = len(categories)
	}

	htmlRe := regexp.MustCompile(`<script>window\.__INITIAL_DATA__=(.+?)</script>`)

	for _, cat := range categories[:limit] {
		resp, err := http.Get(baseURL + "/shop/" + cat + "?limit=50")
		if err != nil {
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		matches := htmlRe.FindSubmatch(body)
		if len(matches) < 2 {
			continue
		}

		var data InitialData
		if err := json.Unmarshal(matches[1], &data); err != nil {
			continue
		}

		for _, item := range data.Items {
			slug := strings.TrimSpace(item.Slug)
			if slug != "" && !seen[slug] {
				seen[slug] = true
				// Full path: category/slug
				fullPath := cat + "/" + slug
				slugs = append(slugs, fullPath)
			}
		}
	}

	return slugs
}

func saveList(filename string, items []string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, item := range items {
		if _, err := w.WriteString(item + "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}
