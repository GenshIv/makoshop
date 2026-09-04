package db

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestRandomByCategory verifies the random-window selection used by the
// home page offers endpoint: correct count, subtree coverage via ancestor
// sort indexes, small-category behavior and variation between calls.
func TestRandomByCategory(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	categoryRepo := NewCategoryRepo(store)
	repo := NewEANPageRepo(store)
	repo.CategoryRepo = categoryRepo
	search := NewEANPageSearch(store.DB(), repo, nil, categoryRepo, true)

	// Root category 1 with child 2 (95 pages live in the child so the root's
	// ancestor sort index must still cover them); small root category 3 (5 pages).
	if err := categoryRepo.Create(&model.Category{ID: 1, NameEn: "root", Slug: "root", IsActive: true}); err != nil {
		t.Fatalf("create root category: %v", err)
	}
	parentID := int64(1)
	if err := categoryRepo.Create(&model.Category{ID: 2, NameEn: "child", Slug: "child", IsActive: true, ParentID: &parentID}); err != nil {
		t.Fatalf("create child category: %v", err)
	}
	if err := categoryRepo.Create(&model.Category{ID: 3, NameEn: "small", Slug: "small", IsActive: true}); err != nil {
		t.Fatalf("create small category: %v", err)
	}

	mkPages := func(catID int64, n, idOffset int) {
		for i := 0; i < n; i++ {
			p := &model.EANPage{
				EAN:          fmt.Sprintf("590%08d", idOffset+i),
				Slug:         fmt.Sprintf("product-%d", idOffset+i),
				Title:        fmt.Sprintf("Product %d", idOffset+i),
				CategoryID:   catID,
				Currency:     "PLN",
				MinPrice:     float64(idOffset + i + 1),
				ProductCount: 1,
				IsActive:     true,
			}
			if err := repo.Create(p); err != nil {
				t.Fatalf("create eanpage %d: %v", idOffset+i, err)
			}
		}
	}
	mkPages(2, 95, 0)
	mkPages(3, 5, 100)

	if err := search.BuildSortIndexes(); err != nil {
		t.Fatalf("build sort indexes: %v", err)
	}

	// Sort-index reads load from disk, flush pending writes first
	// (in production this is done by the periodic sync ticker).
	if err := store.DB().Sync(); err != nil {
		t.Fatalf("sync store: %v", err)
	}

	// Subtree coverage: random pick from the root category returns docs of
	// its child subtree, and total covers the whole subtree.
	items, total, err := search.RandomByCategory(1, 12)
	if err != nil {
		t.Fatalf("random by root category: %v", err)
	}
	if len(items) < 1 || len(items) > 12 {
		t.Fatalf("expected 1..12 items (tail windows may be short), got %d", len(items))
	}
	if total != 95 {
		t.Fatalf("expected total 95 for root subtree, got %d", total)
	}
	for _, raw := range items {
		var doc model.EANPage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("doc is not valid EANPage JSON: %v", err)
		}
		if doc.Title == "" {
			t.Fatalf("doc with empty title in result: %s", string(raw))
		}
		if doc.CategoryID != 2 {
			t.Fatalf("unexpected category %d in root category result", doc.CategoryID)
		}
	}

	// Small category: fewer docs than the limit — return all of them.
	few, fewTotal, err := search.RandomByCategory(3, 12)
	if err != nil {
		t.Fatalf("random by small category: %v", err)
	}
	if len(few) != 5 {
		t.Fatalf("expected 5 items (whole category), got %d", len(few))
	}
	if fewTotal != 5 {
		t.Fatalf("expected total 5 for small category, got %d", fewTotal)
	}

	// Empty category: no items, no error.
	empty, emptyTotal, err := search.RandomByCategory(999, 12)
	if err != nil {
		t.Fatalf("random by empty category: %v", err)
	}
	if len(empty) != 0 || emptyTotal != 0 {
		t.Fatalf("expected 0 items/total for empty category, got %d/%d", len(empty), emptyTotal)
	}

	// Global index (catID=0): every page of every category.
	_, globalTotal, err := search.RandomByCategory(0, 12)
	if err != nil {
		t.Fatalf("random by global index: %v", err)
	}
	if globalTotal != 100 {
		t.Fatalf("expected global total 100, got %d", globalTotal)
	}

	// Variation: with 95 docs and ~8 windows, ten draws should produce at
	// least two distinct sets.
	seen := make(map[string]struct{})
	for draw := 0; draw < 10; draw++ {
		items, _, err := search.RandomByCategory(1, 12)
		if err != nil {
			t.Fatalf("random draw %d: %v", draw, err)
		}
		if len(items) < 1 || len(items) > 12 {
			t.Fatalf("draw %d: expected 1..12 items (tail windows may be short), got %d", draw, len(items))
		}
		key := ""
		for _, raw := range items {
			var doc model.EANPage
			_ = json.Unmarshal(raw, &doc)
			key += fmt.Sprintf("%d,", doc.ID)
		}
		seen[key] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("expected variation between random draws, got %d distinct sets", len(seen))
	}
}
