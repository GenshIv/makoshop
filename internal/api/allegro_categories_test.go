package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAllegroShopCategory(t *testing.T) {
	dir := t.TempDir()
	// The resolver loads docs/allegro_categories.json from the working
	// directory — run from a temp dir with a fixture.
	fixture := `{
  "165": {"name": "Smartfony", "alias": "smartfony-165", "path": "Elektronika"},
  "491": {"name": "Laptopy", "alias": "laptopy-491", "path": "Elektronika > Komputery"}
}`
	docsDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "allegro_categories.json"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(old) }()

	// Numeric ID with a path -> full path with leaf appended.
	if got := resolveAllegroShopCategory("165"); got != "Elektronika > Smartfony" {
		t.Fatalf("165 = %q", got)
	}
	if got := resolveAllegroShopCategory("491"); got != "Elektronika > Komputery > Laptopy" {
		t.Fatalf("491 = %q", got)
	}
	// Already a name (Nokaut-style) — returned as-is.
	if got := resolveAllegroShopCategory("Laptopy i akcesoria"); got != "Laptopy i akcesoria" {
		t.Fatalf("name = %q", got)
	}
	// Unknown ID -> empty (caller falls back).
	if got := resolveAllegroShopCategory("999999"); got != "" {
		t.Fatalf("unknown = %q, want empty", got)
	}
}
