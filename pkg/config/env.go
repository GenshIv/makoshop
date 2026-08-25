package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadEnv loads environment variables from a .env file, if one is present.
//
// It looks for ".env" in the current working directory first, then in the
// directory containing the running executable. This lets the server pick up
// its configuration automatically without the operator exporting anything.
//
// Variables already present in the environment are never overwritten, so an
// explicitly exported value always wins over the .env file.
func LoadEnv() {
	candidates := []string{filepath.Join(".", ".env")}
	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), ".env"))
	}

	for _, path := range candidates {
		if loadEnvFile(path) {
			return
		}
	}
}

// loadEnvFile parses a single .env file and sets any variables it defines
// that are not already present in the environment. It returns true if the
// file was found and read.
func loadEnvFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip an optional "export " prefix (shell-style .env).
		line = strings.TrimPrefix(line, "export ")

		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		value = stripQuotes(value)

		if key == "" {
			continue
		}

		// Do not override existing environment variables.
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return true
}

// stripQuotes removes a matching pair of surrounding single or double quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
