package db

import (
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// TestCategoryTreePathsCacheUpdate verifies that the treePaths cache is updated
// when categories are created, updated, or deleted. This ensures that
// FindCategoryByPath works for newly created categories without requiring
// a full reload of all tree paths.
func TestCategoryTreePathsCacheUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               tmpDir + "/test_db",
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	repo := NewCategoryRepo(store)

	// Create root category
	if err := repo.Create(&model.Category{ID: 1, NameEn: "root", Slug: "root", IsActive: true}); err != nil {
		t.Fatalf("create root: %v", err)
	}

	// Load tree paths (initial load)
	if err := repo.LoadAllTreePaths(); err != nil {
		t.Fatalf("load tree paths: %v", err)
	}

	// Verify root is in cache
	catID, err := repo.FindCategoryByPath([]string{"root"})
	if err != nil || catID != 1 {
		t.Errorf("root not found in cache: catID=%d, err=%v", catID, err)
	}

	// Create child category AFTER initial load — should be added to cache automatically
	parentID := int64(1)
	if err := repo.Create(&model.Category{ID: 2, NameEn: "child", Slug: "child", IsActive: true, ParentID: &parentID}); err != nil {
		t.Fatalf("create child: %v", err)
	}

	// Verify child is now in cache without reloading
	catID, err = repo.FindCategoryByPath([]string{"root", "child"})
	if err != nil || catID != 2 {
		t.Errorf("child not found in cache after create: catID=%d, err=%v", catID, err)
	}

	// Create grandchild
	parentID2 := int64(2)
	if err := repo.Create(&model.Category{ID: 3, NameEn: "grandchild", Slug: "phones", IsActive: true, ParentID: &parentID2}); err != nil {
		t.Fatalf("create grandchild: %v", err)
	}

	// Verify grandchild is in cache
	catID, err = repo.FindCategoryByPath([]string{"root", "child", "phones"})
	if err != nil || catID != 3 {
		t.Errorf("grandchild not found in cache after create: catID=%d, err=%v", catID, err)
	}

	// Update grandchild slug — should update cache
	repo.Update(3, func(c *model.Category) {
		c.Slug = "phones-updated"
	})

	// Old path should no longer work
	_, err = repo.FindCategoryByPath([]string{"root", "child", "phones"})
	if err == nil {
		t.Errorf("old slug still found in cache after update")
	}

	// New path should work
	catID, err = repo.FindCategoryByPath([]string{"root", "child", "phones-updated"})
	if err != nil || catID != 3 {
		t.Errorf("updated slug not found in cache: catID=%d, err=%v", catID, err)
	}

	// Delete grandchild — should remove from cache
	repo.Delete(3)
	_, err = repo.FindCategoryByPath([]string{"root", "child", "phones-updated"})
	if err == nil {
		t.Errorf("deleted category still found in cache")
	}
}
