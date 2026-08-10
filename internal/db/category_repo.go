package db

import (
	"fmt"
	"sort"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

// Turbo index keys for categories:
// turbo_cat_list       -> [catIDs] (all categories)
// turbo_cat_slug:{slug} -> catID (slug -> id, O(1))
// turbo_cat_path:{path_hash} -> catID (tokenized path hash -> id, O(1))
// turbo_cat_parent:{parentID} -> [childIDs] (parent -> children, O(1))
// turbo_cat_children_of:{catID} -> [descendantIDs] (all descendants, cached)
// turbo_cat_ancestors:{catID} -> [ancestorIDs from root] (cached path)
// turbo_cat_active     -> [activeCatIDs] (only active categories)

const (
	turboKeyCategoryList       = "cat_list"
	turboKeyCategorySlug       = "cat_slug:"
	turboKeyCategoryPath       = "cat_path:"
	turboKeyCategoryParent     = "cat_parent:"
	turboKeyCategoryChildrenOf = "cat_children_of:"
	turboKeyCategoryAncestors  = "cat_ancestors:"
	turboKeyCategoryActive     = "cat_active"
)

type CategoryRepo struct {
	store *Store
}

func NewCategoryRepo(store *Store) *CategoryRepo {
	return &CategoryRepo{store: store}
}

// ---------- CRUD ----------

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

	// Update turbo indexes
	if err := r.updateCategoryIndexes(c, nil); err != nil {
		fmt.Printf("WARN: failed to update category indexes %d: %v\n", c.ID, err)
	}

	return nil
}

func (r *CategoryRepo) Get(id int64) (*model.Category, error) {
	data, err := r.store.DocGet(KeyCategory(id))
	if err != nil {
		return nil, fmt.Errorf("get category %d: %w", id, err)
	}
	return UnmarshalCategory(data)
}

func (r *CategoryRepo) Update(id int64, updater func(*model.Category)) error {
	oldCat, err := r.Get(id)
	if err != nil {
		return err
	}

	cat := *oldCat
	updater(&cat)
	cat.UpdatedAt = time.Now()

	data := MarshalCategory(cat)
	if err := r.store.DocPut(KeyCategory(cat.ID), data); err != nil {
		return fmt.Errorf("update category: %w", err)
	}

	// Update turbo indexes (pass old for cleanup)
	if err := r.updateCategoryIndexes(&cat, oldCat); err != nil {
		fmt.Printf("WARN: failed to update category indexes %d: %v\n", cat.ID, err)
	}

	return nil
}

func (r *CategoryRepo) Delete(id int64) error {
	cat, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove from indexes first
	r.removeCategoryFromIndexes(cat)

	// Delete document
	if err := r.store.DocDelete(KeyCategory(id)); err != nil {
		return fmt.Errorf("delete category: %w", err)
	}

	return nil
}

// ---------- Turbo index management ----------

// updateCategoryIndexes updates all turbo indexes for a category.
// oldCat is passed for cleanup (slug change, parent change, etc.)
func (r *CategoryRepo) updateCategoryIndexes(cat, oldCat *model.Category) error {
	// 1. Update slug index
	if oldCat != nil && oldCat.Slug != "" && oldCat.Slug != cat.Slug {
		// Remove old slug
		r.store.TurboWrite(turboKeyCategorySlug+oldCat.Slug, []byte{})
	}
	if cat.Slug != "" {
		if err := r.store.TurboWrite(turboKeyCategorySlug+cat.Slug, []byte(fmt.Sprintf("%d", cat.ID))); err != nil {
			return fmt.Errorf("write cat_slug: %w", err)
		}
	}

	// 2. Update path index (slug-based path from ancestors)
	if oldCat != nil {
		oldAncestors, _ := r.GetAncestors(oldCat.ID)
		if len(oldAncestors) > 0 {
			oldPath := r.buildPathFromAncestors(oldAncestors, oldCat.Slug)
			if oldPath != "" {
				r.store.TurboWrite(turboKeyCategoryPath+hashPath(oldPath), []byte{})
			}
		}
	}
	if cat.Slug != "" {
		ancestors, _ := r.GetAncestors(cat.ID)
		path := r.buildPathFromAncestors(ancestors, cat.Slug)
		if path != "" {
			pathHash := hashPath(path)
			if err := r.store.TurboWrite(turboKeyCategoryPath+pathHash, []byte(fmt.Sprintf("%d", cat.ID))); err != nil {
				return fmt.Errorf("write cat_path: %w", err)
			}
		}
	}

	// 3. Update parent index (children list)
	if oldCat != nil && oldCat.ParentID != nil && (*oldCat.ParentID != *cat.ParentID || cat.ParentID == nil) {
		// Remove from old parent's children list
		r.removeFromParentChildrenList(*oldCat.ParentID, cat.ID)
	}
	if cat.ParentID != nil {
		r.addToParentChildrenList(*cat.ParentID, cat.ID)
	} else if oldCat != nil && oldCat.ParentID != nil {
		// Was has parent, now root — remove from parent list
		r.removeFromParentChildrenList(*oldCat.ParentID, cat.ID)
	}

	// 4. Update ancestors cache
	r.rebuildAncestorsCache(cat.ID)

	// 5. Update descendants cache (for this cat and all ancestors)
	r.rebuildDescendantsCache(cat.ID)

	// 6. Update cat_list
	r.addToCategoryList(cat.ID)

	// 7. Update active list
	if cat.IsActive {
		r.addToActiveList(cat.ID)
	} else {
		r.removeFromActiveList(cat.ID)
	}

	return nil
}

func (r *CategoryRepo) removeCategoryFromIndexes(cat *model.Category) {
	// Remove slug
	if cat.Slug != "" {
		r.store.TurboWrite(turboKeyCategorySlug+cat.Slug, []byte{})
	}

	// Remove path
	ancestors, _ := r.GetAncestors(cat.ID)
	path := r.buildPathFromAncestors(ancestors, cat.Slug)
	if path != "" {
		r.store.TurboWrite(turboKeyCategoryPath+hashPath(path), []byte{})
	}

	// Remove from parent children
	if cat.ParentID != nil {
		r.removeFromParentChildrenList(*cat.ParentID, cat.ID)
	}

	// Remove from cat_list
	r.removeFromCategoryList(cat.ID)

	// Remove from active list
	r.removeFromActiveList(cat.ID)

	// Remove ancestors cache
	r.store.TurboWrite(turboKeyCategoryAncestors+fmt.Sprintf("%d", cat.ID), []byte{})

	// Remove descendants cache
	r.store.TurboWrite(turboKeyCategoryChildrenOf+fmt.Sprintf("%d", cat.ID), []byte{})
}

// ---------- Index helpers ----------

func (r *CategoryRepo) addToCategoryList(catID int64) {
	data, _ := r.store.DB().TurboRawRead(turboKeyCategoryList)
	var ids []uint64
	if data != nil && len(data) > 0 {
		ids = makodb.TurboUnsafeReadTokens(data)
	}
	for _, id := range ids {
		if id == uint64(catID) {
			return
		}
	}
	ids = append(ids, uint64(catID))
	buf := makodb.TurboBinaryNew(ids)
	r.store.TurboWrite(turboKeyCategoryList, buf)
}

func (r *CategoryRepo) removeFromCategoryList(catID int64) {
	data, _ := r.store.DB().TurboRawRead(turboKeyCategoryList)
	if len(data) == 0 {
		return
	}
	ids := makodb.TurboUnsafeReadTokens(data)
	var newIDs []uint64
	for _, id := range ids {
		if id != uint64(catID) {
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) > 0 {
		buf := makodb.TurboBinaryNew(newIDs)
		r.store.TurboWrite(turboKeyCategoryList, buf)
	} else {
		r.store.TurboWrite(turboKeyCategoryList, []byte{})
	}
}

func (r *CategoryRepo) addToActiveList(catID int64) {
	data, _ := r.store.DB().TurboRawRead(turboKeyCategoryActive)
	var ids []uint64
	if data != nil && len(data) > 0 {
		ids = makodb.TurboUnsafeReadTokens(data)
	}
	for _, id := range ids {
		if id == uint64(catID) {
			return
		}
	}
	ids = append(ids, uint64(catID))
	buf := makodb.TurboBinaryNew(ids)
	r.store.TurboWrite(turboKeyCategoryActive, buf)
}

func (r *CategoryRepo) removeFromActiveList(catID int64) {
	data, _ := r.store.DB().TurboRawRead(turboKeyCategoryActive)
	if len(data) == 0 {
		return
	}
	ids := makodb.TurboUnsafeReadTokens(data)
	var newIDs []uint64
	for _, id := range ids {
		if id != uint64(catID) {
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) > 0 {
		buf := makodb.TurboBinaryNew(newIDs)
		r.store.TurboWrite(turboKeyCategoryActive, buf)
	} else {
		r.store.TurboWrite(turboKeyCategoryActive, []byte{})
	}
}

func (r *CategoryRepo) addToParentChildrenList(parentID, childID int64) {
	key := turboKeyCategoryParent + fmt.Sprintf("%d", parentID)
	data, _ := r.store.DB().TurboRawRead(key)
	var ids []uint64
	if data != nil && len(data) > 0 {
		ids = makodb.TurboUnsafeReadTokens(data)
	}
	for _, id := range ids {
		if id == uint64(childID) {
			return
		}
	}
	ids = append(ids, uint64(childID))
	buf := makodb.TurboBinaryNew(ids)
	r.store.TurboWrite(key, buf)
}

func (r *CategoryRepo) removeFromParentChildrenList(parentID, childID int64) {
	key := turboKeyCategoryParent + fmt.Sprintf("%d", parentID)
	data, _ := r.store.DB().TurboRawRead(key)
	if len(data) == 0 {
		return
	}
	ids := makodb.TurboUnsafeReadTokens(data)
	var newIDs []uint64
	for _, id := range ids {
		if id != uint64(childID) {
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) > 0 {
		buf := makodb.TurboBinaryNew(newIDs)
		r.store.TurboWrite(key, buf)
	} else {
		r.store.TurboWrite(key, []byte{})
	}
}

// rebuildAncestorsCache rebuilds the ancestors cache for a category.
func (r *CategoryRepo) rebuildAncestorsCache(catID int64) {
	ancestors := r.computeAncestors(catID)
	key := turboKeyCategoryAncestors + fmt.Sprintf("%d", catID)
	if len(ancestors) > 0 {
		buf := makodb.TurboBinaryNew(Uint64SliceFromInt64(ancestors))
		r.store.TurboWrite(key, buf)
	} else {
		r.store.TurboWrite(key, []byte{})
	}
}

// computeAncestors computes ancestors from root to parent (not including self).
func (r *CategoryRepo) computeAncestors(catID int64) []int64 {
	var ancestors []int64
	currentID := catID
	for currentID != 0 {
		cat, err := r.Get(currentID)
		if err != nil || cat == nil || cat.ParentID == nil {
			break
		}
		ancestors = append(ancestors, *cat.ParentID)
		currentID = *cat.ParentID
	}
	// Reverse: root first
	for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
		ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
	}
	return ancestors
}

// rebuildDescendantsCache rebuilds descendants cache for a category.
func (r *CategoryRepo) rebuildDescendantsCache(catID int64) {
	descendants := r.computeDescendants(catID)
	key := turboKeyCategoryChildrenOf + fmt.Sprintf("%d", catID)
	if len(descendants) > 0 {
		buf := makodb.TurboBinaryNew(Uint64SliceFromInt64(descendants))
		r.store.TurboWrite(key, buf)
	} else {
		r.store.TurboWrite(key, []byte{})
	}
}

// computeDescendants computes all descendants of a category.
func (r *CategoryRepo) computeDescendants(catID int64) []int64 {
	var result []int64
	queue := []int64{catID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		children := r.getDirectChildrenFromIndex(current)
		for _, child := range children {
			result = append(result, child)
			queue = append(queue, child)
		}
	}
	return result
}

// buildPathFromAncestors builds a slash-separated path from ancestors and own slug.
func (r *CategoryRepo) buildPathFromAncestors(ancestors []int64, ownSlug string) string {
	var parts []string
	for _, aid := range ancestors {
		cat, err := r.Get(aid)
		if err == nil && cat.Slug != "" {
			parts = append(parts, cat.Slug)
		}
	}
	if ownSlug != "" {
		parts = append(parts, ownSlug)
	}
	return joinPath(parts)
}

func joinPath(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "/" + parts[i]
	}
	return result
}

// ---------- Public API (optimized with turbo indexes) ----------

// GetBySlug returns category by slug in O(1).
func (r *CategoryRepo) GetBySlug(slug string) (*model.Category, error) {
	if slug == "" {
		return nil, ErrKeyNotFound
	}
	key := turboKeyCategorySlug + slug
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, ErrKeyNotFound
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetByPath returns category by path of slugs in O(1) via path hash.
// pathParts: ["elektronika", "telefony"] -> hash -> catID
func (r *CategoryRepo) GetByPath(pathParts []string) (*model.Category, error) {
	if len(pathParts) == 0 {
		return nil, ErrKeyNotFound
	}
	path := joinPath(pathParts)
	pathHash := hashPath(path)
	key := turboKeyCategoryPath + pathHash
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, ErrKeyNotFound
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetAncestors returns ancestors from root to parent (cached, O(1)).
func (r *CategoryRepo) GetAncestors(catID int64) ([]int64, error) {
	key := turboKeyCategoryAncestors + fmt.Sprintf("%d", catID)
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		// Fallback: compute
		return r.computeAncestors(catID), nil
	}
	hashes := makodb.TurboUnsafeReadTokens(data)
	ids := make([]int64, len(hashes))
	for i, h := range hashes {
		ids[i] = int64(h)
	}
	return ids, nil
}

// GetDirectChildren returns immediate children of a category (cached, O(1)).
func (r *CategoryRepo) GetDirectChildren(catID int64) ([]int64, error) {
	return r.getDirectChildrenFromIndex(catID), nil
}

func (r *CategoryRepo) getDirectChildrenFromIndex(catID int64) []int64 {
	key := turboKeyCategoryParent + fmt.Sprintf("%d", catID)
	data, _ := r.store.DB().TurboRawRead(key)
	if len(data) == 0 {
		return nil
	}
	hashes := makodb.TurboUnsafeReadTokens(data)
	ids := make([]int64, len(hashes))
	for i, h := range hashes {
		ids[i] = int64(h)
	}
	return ids
}

// GetDescendants returns all descendants of a category (cached, O(1)).
func (r *CategoryRepo) GetDescendants(catID int64) ([]int64, error) {
	key := turboKeyCategoryChildrenOf + fmt.Sprintf("%d", catID)
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		// Fallback: compute
		return r.computeDescendants(catID), nil
	}
	hashes := makodb.TurboUnsafeReadTokens(data)
	ids := make([]int64, len(hashes))
	for i, h := range hashes {
		ids[i] = int64(h)
	}
	return ids, nil
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

// ListActive returns only active categories.
func (r *CategoryRepo) ListActive() ([]model.Category, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyCategoryActive)
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

// ---------- Tree operations (optimized) ----------

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
	categories, err := r.ListActive()
	if err != nil {
		return nil, err
	}
	return r.buildTree(categories, nil)
}

// GetTreeByParent returns subtree rooted at the given parent category ID (only active).
func (r *CategoryRepo) GetTreeByParent(parentID int64) ([]CategoryTreeNode, error) {
	categories, err := r.ListActive()
	if err != nil {
		return nil, err
	}
	var filtered []model.Category
	for _, c := range categories {
		if c.ID == parentID || r.isDescendantCached(&c, parentID) {
			filtered = append(filtered, c)
		}
	}
	return r.buildTree(filtered, &parentID)
}

// isDescendantCached checks if category is descendant of parentID using cached descendants.
func (r *CategoryRepo) isDescendantCached(cat *model.Category, parentID int64) bool {
	if cat.ID == parentID {
		return true
	}
	descendants, _ := r.GetDescendants(parentID)
	for _, d := range descendants {
		if d == cat.ID {
			return true
		}
	}
	return false
}

func (r *CategoryRepo) buildTree(categories []model.Category, rootParentID *int64) ([]CategoryTreeNode, error) {
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

	var roots []*CategoryTreeNode
	for _, node := range byID {
		if node.ParentID == nil {
			if rootParentID == nil {
				roots = append(roots, node)
			}
		} else if rootParentID != nil && *node.ParentID == *rootParentID {
			roots = append(roots, node)
		} else if parent, ok := byID[*node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].SortOrder != roots[j].SortOrder {
			return roots[i].SortOrder < roots[j].SortOrder
		}
		return roots[i].Name < roots[j].Name
	})

	for _, root := range roots {
		r.sortChildren(root)
	}

	result := make([]CategoryTreeNode, 0, len(roots))
	for _, root := range roots {
		result = append(result, *root)
	}

	return result, nil
}

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

// GetTreePath returns the path from root to the given category as [slug1, slug2, ...].
// Uses cached ancestors for O(1) lookup.
func (r *CategoryRepo) GetTreePath(catID int64) ([]string, error) {
	ancestors, err := r.GetAncestors(catID)
	if err != nil {
		return nil, err
	}

	var path []string
	for _, aid := range ancestors {
		cat, err := r.Get(aid)
		if err != nil {
			return nil, err
		}
		if cat.Slug != "" {
			path = append(path, cat.Slug)
		}
	}

	// Add own slug
	cat, err := r.Get(catID)
	if err != nil {
		return nil, err
	}
	if cat.Slug != "" {
		path = append(path, cat.Slug)
	}

	return path, nil
}

// ---------- Migration helpers ----------

// BuildPathMap builds a map of category paths ("Cat1 -> Cat2 -> Cat3") to category IDs.
// Used during import to find existing categories without creating new ones.
func (r *CategoryRepo) BuildPathMap() (map[string]int64, error) {
	cats, err := r.ListAll()
	if err != nil {
		return nil, err
	}

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

		for id, c := range byID {
			if c.ParentID != nil && *c.ParentID == cat.ID {
				buildPaths(id, path)
			}
		}
	}

	for id, c := range byID {
		if c.ParentID == nil {
			buildPaths(id, "")
		}
	}

	return pathMap, nil
}

// RebuildAllIndexes rebuilds all category indexes from existing documents.
// Call this once after upgrading to the new index-based category repo.
func (r *CategoryRepo) RebuildAllIndexes() error {
	cats, err := r.ListAll()
	if err != nil {
		return err
	}

	// Clear all indexes
	r.store.TurboWrite(turboKeyCategoryList, []byte{})
	r.store.TurboWrite(turboKeyCategoryActive, []byte{})

	for _, cat := range cats {
		// Slug index
		if cat.Slug != "" {
			r.store.TurboWrite(turboKeyCategorySlug+cat.Slug, []byte(fmt.Sprintf("%d", cat.ID)))
		}

		// Path index
		ancestors := r.computeAncestors(cat.ID)
		path := r.buildPathFromAncestors(ancestors, cat.Slug)
		if path != "" {
			r.store.TurboWrite(turboKeyCategoryPath+hashPath(path), []byte(fmt.Sprintf("%d", cat.ID)))
		}

		// Parent index
		if cat.ParentID != nil {
			r.addToParentChildrenList(*cat.ParentID, cat.ID)
		}

		// Ancestors cache
		r.rebuildAncestorsCache(cat.ID)

		// Descendants cache
		r.rebuildDescendantsCache(cat.ID)

		// Active list
		if cat.IsActive {
			r.addToActiveList(cat.ID)
		}
	}

	fmt.Printf("[CATEGORY] Rebuilt all indexes: %d categories\n", len(cats))
	return nil
}

// hashPath creates a hash from a path string for use as index key.
func hashPath(path string) string {
	h := uint64(14695981039346656037)
	for i := 0; i < len(path); i++ {
		h ^= uint64(path[i])
		h *= 1099511628211
	}
	return fmt.Sprintf("%x", h)
}
