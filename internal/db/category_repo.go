package db

import (
	"fmt"
	"sort"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

const turboKeyCategoryList = "cat_list"

type CategoryRepo struct {
	store *Store
}

func NewCategoryRepo(store *Store) *CategoryRepo {
	return &CategoryRepo{store: store}
}

func (r *CategoryRepo) Create(c *model.Category) error {
	id, err := r.store.NextID("category")
	if err != nil {
		return fmt.Errorf("next_id category: %w", err)
	}
	c.ID = id
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	if c.IsActive == false {
		c.IsActive = true
	}

	data := MarshalCategory(*c)
	if err := r.store.DocPut(KeyCategory(c.ID), data); err != nil {
		return fmt.Errorf("save category: %w", err)
	}

	// Add to category list index
	if err := r.addToCategoryList(c.ID); err != nil {
		fmt.Printf("WARN: failed to add category %d to list index: %v\n", c.ID, err)
	}

	return nil
}

// addToCategoryList adds a category ID to the cat_list turbo index.
func (r *CategoryRepo) addToCategoryList(catID int64) error {
	data, _ := r.store.DB().TurboRawRead(turboKeyCategoryList)
	var ids []uint64
	if data != nil && len(data) > 0 {
		ids = makodb.TurboUnsafeReadTokens(data)
	}
	for _, id := range ids {
		if id == uint64(catID) {
			return nil // already present
		}
	}
	ids = append(ids, uint64(catID))
	buf := makodb.TurboBinaryNew(ids)
	return r.store.DB().TurboRawWrite(turboKeyCategoryList, buf)
}

func (r *CategoryRepo) Get(id int64) (*model.Category, error) {
	data, err := r.store.DocGet(KeyCategory(id))
	if err != nil {
		return nil, fmt.Errorf("get category %d: %w", id, err)
	}
	return UnmarshalCategory(data)
}

func (r *CategoryRepo) Update(id int64, updater func(*model.Category)) error {
	cat, err := r.Get(id)
	if err != nil {
		return err
	}
	updater(cat)
	cat.UpdatedAt = time.Now()

	data := MarshalCategory(*cat)
	if err := r.store.DocPut(KeyCategory(cat.ID), data); err != nil {
		return fmt.Errorf("update category: %w", err)
	}

	return nil
}

func (r *CategoryRepo) Delete(id int64) error {
	if err := r.store.DocDelete(KeyCategory(id)); err != nil {
		return fmt.Errorf("delete category: %w", err)
	}

	return nil
}

// GetByNameAndParent finds a category by name and parent ID.
// Returns nil if not found.
func (r *CategoryRepo) GetByNameAndParent(name string, parentID int64) (*model.Category, error) {
	cats, err := r.ListAll()
	if err != nil {
		return nil, err
	}
	for _, cat := range cats {
		if cat.Name == name && cat.ParentID != nil && *cat.ParentID == parentID {
			return &cat, nil
		}
	}
	return nil, nil
}

// ListAll returns all categories using the cat_list turbo index.
func (r *CategoryRepo) ListAll() ([]model.Category, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyCategoryList)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.Category
	for _, id := range ids {
		cat, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *cat)
	}
	return result, nil
}

// RebuildCategoryList rebuilds the cat_list index by scanning all category IDs.
// Use this after upgrading to the new category list index.
// This is an emergency method that reads documents directly (allowed by rules).
func (r *CategoryRepo) RebuildCategoryList() error {
	// Read next_id to know the range of possible category IDs
	nextIDData, _ := r.store.DB().TurboRawRead("state:next_id:category")
	if len(nextIDData) == 0 {
		return nil
	}

	var nextID int64
	_, _ = fmt.Sscanf(string(nextIDData), "%d", &nextID)

	var ids []uint64
	for id := int64(1); id < nextID; id++ {
		_, err := r.Get(id)
		if err == nil {
			ids = append(ids, uint64(id))
		}
	}

	buf := makodb.TurboBinaryNew(ids)
	if err := r.store.DB().TurboRawWrite(turboKeyCategoryList, buf); err != nil {
		return fmt.Errorf("write cat_list: %w", err)
	}

	fmt.Printf("[CATEGORY] Rebuilt cat_list: %d categories\n", len(ids))
	return nil
}

// CategoryTreeNode is a category with its children for tree representation.
type CategoryTreeNode struct {
	ID        int64               `json:"id"`
	ParentID  *int64              `json:"parent_id,omitempty"`
	Name      string              `json:"name"`
	Slug      string              `json:"slug"`
	Desc      string              `json:"description,omitempty"`
	IsActive  bool                `json:"is_active"`
	SortOrder int                 `json:"sort_order"`
	Children  []*CategoryTreeNode `json:"children,omitempty"`
}

// GetTree returns the full category tree (only active categories).
func (r *CategoryRepo) GetTree() ([]CategoryTreeNode, error) {
	categories, err := r.ListAll()
	if err != nil {
		return nil, err
	}
	// Filter only active categories
	var active []model.Category
	for _, c := range categories {
		if c.IsActive {
			active = append(active, c)
		}
	}
	return r.buildTree(active, nil)
}

// GetTreeByParent returns subtree rooted at the given parent category ID (only active).
func (r *CategoryRepo) GetTreeByParent(parentID int64) ([]CategoryTreeNode, error) {
	categories, err := r.ListAll()
	if err != nil {
		return nil, err
	}
	// Filter only active descendants of parentID
	var filtered []model.Category
	for _, c := range categories {
		if c.IsActive && r.isDescendantOrSelf(&c, parentID) {
			filtered = append(filtered, c)
		}
	}
	return r.buildTree(filtered, &parentID)
}

// isDescendantOrSelf checks if category is descendant of or equal to parentID.
func (r *CategoryRepo) isDescendantOrSelf(cat *model.Category, parentID int64) bool {
	if cat.ID == parentID {
		return true
	}
	if cat.ParentID == nil {
		return false
	}
	// Simple check: walk up the tree
	currentID := *cat.ParentID
	for currentID != 0 {
		if currentID == parentID {
			return true
		}
		// Find parent
		parent, err := r.Get(currentID)
		if err != nil || parent.ParentID == nil {
			break
		}
		currentID = *parent.ParentID
	}
	return false
}

// buildTree builds a tree from a flat list of categories.
func (r *CategoryRepo) buildTree(categories []model.Category, rootParentID *int64) ([]CategoryTreeNode, error) {
	// Build map by ID
	byID := make(map[int64]*CategoryTreeNode, len(categories))
	for _, c := range categories {
		byID[c.ID] = &CategoryTreeNode{
			ID:        c.ID,
			ParentID:  c.ParentID,
			Name:      c.Name,
			Slug:      c.Slug,
			Desc:      c.Desc,
			IsActive:  c.IsActive,
			SortOrder: c.SortOrder,
		}
	}

	// Link children
	var roots []*CategoryTreeNode
	for _, node := range byID {
		if node.ParentID == nil {
			// Root category (no parent)
			if rootParentID == nil {
				roots = append(roots, node)
			}
		} else if rootParentID != nil && *node.ParentID == *rootParentID {
			// Direct child of requested root
			roots = append(roots, node)
		} else if parent, ok := byID[*node.ParentID]; ok {
			// Attach to parent
			parent.Children = append(parent.Children, node)
		}
	}

	// Sort roots by sort_order, then name
	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].SortOrder != roots[j].SortOrder {
			return roots[i].SortOrder < roots[j].SortOrder
		}
		return roots[i].Name < roots[j].Name
	})

	// Sort children recursively
	for _, root := range roots {
		r.sortChildren(root)
	}

	// Convert []*CategoryTreeNode to []CategoryTreeNode
	result := make([]CategoryTreeNode, 0, len(roots))
	for _, root := range roots {
		result = append(result, *root)
	}

	return result, nil
}

// sortChildren sorts children of a node recursively.
func (r *CategoryRepo) sortChildren(node *CategoryTreeNode) {
	sort.SliceStable(node.Children, func(i, j int) bool {
		if node.Children[i].SortOrder != node.Children[j].SortOrder {
			return node.Children[i].SortOrder < node.Children[j].SortOrder
		}
		return node.Children[i].Name < node.Children[j].Name
	})
	for _, child := range node.Children {
		r.sortChildren(child)
	}
}

// BuildPathMap builds a map of category paths ("Cat1 -> Cat2 -> Cat3") to category IDs.
// Used during import to find existing categories without creating new ones.
func (r *CategoryRepo) BuildPathMap() (map[string]int64, error) {
	cats, err := r.ListAll()
	if err != nil {
		return nil, err
	}

	// Build tree to compute full paths
	byID := make(map[int64]*model.Category)
	for i := range cats {
		byID[cats[i].ID] = &cats[i]
	}

	pathMap := make(map[string]int64)

	var buildPaths func(catID int64, prefix string)
	buildPaths = func(catID int64, prefix string) {
		cat, ok := byID[catID]
		if !ok {
			return
		}

		path := prefix
		if path == "" {
			path = cat.Name
		} else {
			path = prefix + " -> " + cat.Name
		}

		pathMap[path] = cat.ID

		// Recurse children
		for id, c := range byID {
			if c.ParentID != nil && *c.ParentID == cat.ID {
				buildPaths(id, path)
			}
		}
	}

	// Start from root categories
	for id, c := range byID {
		if c.ParentID == nil {
			buildPaths(id, "")
		}
	}

	return pathMap, nil
}

// GetTreePath returns the path from root to the given category as [slug1, slug2, ...].
func (r *CategoryRepo) GetTreePath(catID int64) ([]string, error) {
	var path []string
	currentID := catID
	for currentID != 0 {
		cat, err := r.Get(currentID)
		if err != nil {
			return nil, err
		}
		path = append(path, cat.Slug)
		if cat.ParentID == nil {
			break
		}
		currentID = *cat.ParentID
	}
	// Reverse path (root first)
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}
