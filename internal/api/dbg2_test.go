package api

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

func TestDebugDupPages(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}
	store, _ := db.NewStore(cfg)
	defer store.Close()
	h := NewHandlers(store)

	if err := h.productRepo.Create(&model.Product{EAN: "111", Name: "x 111", CompanyID: 1, Price: 100, Currency: "PLN", Status: model.ProductStatusActive}); err != nil {
		t.Fatalf("product: %v", err)
	}
	pages, _ := h.eanPageRepo.List()
	fmt.Printf("pages after product create: %d\n", len(pages))
	for i := range pages {
		fmt.Printf("  page id=%d ean=%q count=%d cat=%d\n", pages[i].ID, pages[i].EAN, pages[i].ProductCount, pages[i].CategoryID)
	}
}
