package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/GenshIv/makoshop/internal/attrs"
)

func main() {
	// Read HTML from stdin
	var html strings.Builder
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		html.WriteString(scanner.Text())
		html.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		os.Exit(1)
	}

	htmlStr := html.String()
	if htmlStr == "" {
		fmt.Println("{}")
		return
	}

	// Parse attributes
	parsed := attrs.ParseTable(htmlStr)

	// Output as JSON map (code -> [values]) to preserve all values
	data, err := json.Marshal(parsed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}
