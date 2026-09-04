package db

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

// collectTreeIDs walks a parsed category tree and collects all node IDs.
func collectTreeIDs(nodes []CategoryTreeNode, ids map[int64]struct{}) {
	for i := range nodes {
		ids[nodes[i].ID] = struct{}{}
		collectChildIDs(nodes[i].Children, ids)
	}
}

func collectChildIDs(children []*CategoryTreeNode, ids map[int64]struct{}) {
	for _, c := range children {
		if c == nil {
			continue
		}
		ids[c.ID] = struct{}{}
		collectChildIDs(c.Children, ids)
	}
}

// TestPublicTreeFilteredBySortIndexes verifies that the public category tree
// filter uses catalog sort-index counts (no EAN page documents are loaded):
// categories without pages, and categories whose pages have no offers
// (ProductCount == 0), are excluded from the public tree but kept in the
// admin tree.
func TestPublicTreeFilteredBySortIndexes(t *testing.T) {
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
	eanPageRepo := NewEANPageRepo(store)
	eanPageRepo.CategoryRepo = categoryRepo
	search := NewEANPageSearch(store.DB(), eanPageRepo, nil, categoryRepo, true)

	// 1 root (no direct pages, has a child with pages) -> stays via subtree
	// 2 child of 1 with pages -> stays
	// 3 standalone category with pages -> stays
	// 4 standalone category without pages -> public tree only
	// 5 standalone category with a single zero-offer page -> public tree only
	if err := categoryRepo.Create(&model.Category{ID: 1, NameEn: "root", Slug: "root", IsActive: true}); err != nil {
		t.Fatalf("create cat 1: %v", err)
	}
	parentID := int64(1)
	if err := categoryRepo.Create(&model.Category{ID: 2, NameEn: "child", Slug: "child", IsActive: true, ParentID: &parentID}); err != nil {
		t.Fatalf("create cat 2: %v", err)
	}
	if err := categoryRepo.Create(&model.Category{ID: 3, NameEn: "withpages", Slug: "withpages", IsActive: true}); err != nil {
		t.Fatalf("create cat 3: %v", err)
	}
	if err := categoryRepo.Create(&model.Category{ID: 4, NameEn: "nopages", Slug: "nopages", IsActive: true}); err != nil {
		t.Fatalf("create cat 4: %v", err)
	}
	if err := categoryRepo.Create(&model.Category{ID: 5, NameEn: "zerooffers", Slug: "zerooffers", IsActive: true}); err != nil {
		t.Fatalf("create cat 5: %v", err)
	}

	mkPage := func(id int64, catID int64, count int) {
		p := &model.EANPage{
			EAN:          fmt.Sprintf("590%08d", id),
			Slug:         fmt.Sprintf("product-%d", id),
			Title:        fmt.Sprintf("Product %d", id),
			CategoryID:   catID,
			Currency:     "PLN",
			MinPrice:     float64(id),
			ProductCount: count,
			IsActive:     true,
		}
		if err := eanPageRepo.Create(p); err != nil {
			t.Fatalf("create eanpage %d: %v", id, err)
		}
	}
	mkPage(1, 2, 1)
	mkPage(2, 2, 1)
	mkPage(3, 3, 1)
	mkPage(4, 5, 0) // zero offers: excluded from catalog sort indexes

	if err := search.BuildSortIndexes(); err != nil {
		t.Fatalf("build sort indexes: %v", err)
	}
	if err := store.DB().Sync(); err != nil {
		t.Fatalf("sync store: %v", err)
	}

	categoryRepo.RebuildTrees()

	// Public tree: 1 (via child), 2, 3 stay; 4 and 5 are filtered out.
	publicJSON, err := categoryRepo.GetTreeJSON()
	if err != nil {
		t.Fatalf("get public tree: %v", err)
	}
	var publicNodes []CategoryTreeNode
	if err := json.Unmarshal(publicJSON, &publicNodes); err != nil {
		t.Fatalf("parse public tree: %v", err)
	}
	publicIDs := map[int64]struct{}{}
	collectTreeIDs(publicNodes, publicIDs)
	for _, want := range []int64{1, 2, 3} {
		if _, ok := publicIDs[want]; !ok {
			t.Fatalf("public tree missing category %d (has: %v)", want, publicIDs)
		}
	}
	for _, unwanted := range []int64{4, 5} {
		if _, ok := publicIDs[unwanted]; ok {
			t.Fatalf("public tree must not contain category %d (has: %v)", unwanted, publicIDs)
		}
	}

	// Admin tree: all active categories regardless of pages.
	adminJSON, err := categoryRepo.GetAdminTreeJSON()
	if err != nil {
		t.Fatalf("get admin tree: %v", err)
	}
	var adminNodes []CategoryTreeNode
	if err := json.Unmarshal(adminJSON, &adminNodes); err != nil {
		t.Fatalf("parse admin tree: %v", err)
	}
	adminIDs := map[int64]struct{}{}
	collectTreeIDs(adminNodes, adminIDs)
	for _, want := range []int64{1, 2, 3, 4, 5} {
		if _, ok := adminIDs[want]; !ok {
			t.Fatalf("admin tree missing category %d (has: %v)", want, adminIDs)
		}
	}
}
