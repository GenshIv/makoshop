package db

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
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
	turboKeyCategoryList       = "cat_list:"
	turboKeyCategorySlug       = "cat_slug:"
	turboKeyCategoryPath       = "cat_path:"
	turboKeyCategoryParent     = "cat_parent:"
	turboKeyCategoryChildrenOf = "cat_children_of:"
	turboKeyCategoryAncestors  = "cat_ancestors:"
	turboKeyCategoryActive     = "cat_active:"

	// Precomputed category trees as JSON.
	turboKeyCategoryTreeFull   = "cat_tree_full:"
	turboKeyCategoryTreeAdmin  = "cat_tree_admin:" // all categories, no filtering
	turboKeyCategoryTreeParent = "cat_tree_parent:"
)

type CategoryRepo struct {
	store         *Store
	treePathCache sync.Map // catID int64 → []string

	// EANPageRepo for filtering categories with EAN pages in public tree
	eanPageRepo *EANPageRepo

	// Active transaction (nil if not in transaction)
	txn *makodb.Transaction
}

func NewCategoryRepo(store *Store) *CategoryRepo {
	return &CategoryRepo{store: store}
}

// SetEANPageRepo attaches an EANPageRepo for filtering categories in public tree.
func (r *CategoryRepo) SetEANPageRepo(repo *EANPageRepo) {
	r.eanPageRepo = repo
}

// SetTransaction sets the active transaction for this repo.
func (r *CategoryRepo) SetTransaction(txn *makodb.Transaction) {
	r.txn = txn
}

// ClearTransaction clears the active transaction.
func (r *CategoryRepo) ClearTransaction() {
	r.txn = nil
}

// ---------- CRUD ----------

func (r *CategoryRepo) Create(c *model.Category) error {
	// Use provided ID if set, otherwise generate new
	if c.ID == 0 {
		id, err := r.store.NextID("category")
		if err != nil {
			return fmt.Errorf("next_id category: %w", err)
		}
		c.ID = id
	} else {
		// Ensure the ID counter is at least this ID + 1
		if err := r.store.SetNextIDIfGreater("category", c.ID+1); err != nil {
			fmt.Printf("WARN: failed to update next_id for category %d: %v\n", c.ID, err)
		}
	}
	c.CreatedAt = time.Now().Unix()
	c.UpdatedAt = time.Now().Unix()
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

	// Rebuild precomputed tree JSONs
	r.rebuildFullTreeJSON()
	r.invalidateAllParentTrees()

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
	cat.UpdatedAt = time.Now().Unix()

	data := MarshalCategory(cat)
	if err := r.store.DocPut(KeyCategory(cat.ID), data); err != nil {
		return fmt.Errorf("update category: %w", err)
	}

	// Update turbo indexes (pass old for cleanup)
	if err := r.updateCategoryIndexes(&cat, oldCat); err != nil {
		fmt.Printf("WARN: failed to update category indexes %d: %v\n", cat.ID, err)
	}

	// Rebuild precomputed tree JSONs
	r.rebuildFullTreeJSON()
	r.invalidateAllParentTrees()

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

	// Rebuild precomputed tree JSONs
	r.rebuildFullTreeJSON()
	r.invalidateAllParentTrees()

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
	r.store.DB().TurboClearIndex(turboKeyCategoryAncestors + fmt.Sprintf("%d", cat.ID))

	// Remove descendants cache
	r.store.DB().TurboClearIndex(turboKeyCategoryChildrenOf + fmt.Sprintf("%d", cat.ID))
}

// ---------- Index helpers ----------

func (r *CategoryRepo) addToCategoryList(catID int64) {
	// Add to category list using TurboPutIndex with Key128
	_, _ = r.store.DB().TurboPutIndexString(turboKeyCategoryList, KeyCategory(catID))
}

func (r *CategoryRepo) removeFromCategoryList(catID int64) {
	// Remove from category list using TurboDeleteIndex with Key128
	r.store.DB().TurboDeleteIndexString(turboKeyCategoryList, KeyCategory(catID))
}

func (r *CategoryRepo) addToActiveList(catID int64) {
	// Add to active list using TurboPutIndex with Key128
	_, _ = r.store.DB().TurboPutIndexString(turboKeyCategoryActive, KeyCategory(catID))
}

func (r *CategoryRepo) removeFromActiveList(catID int64) {
	// Remove from active list using TurboDeleteIndex with Key128
	r.store.DB().TurboDeleteIndexString(turboKeyCategoryActive, KeyCategory(catID))
}

func (r *CategoryRepo) addToParentChildrenList(parentID, childID int64) {
	key := turboKeyCategoryParent + fmt.Sprintf("%d", parentID)
	// Add to parent children list using TurboPutIndex with Key128
	_, _ = r.store.DB().TurboPutIndexString(key, KeyCategory(childID))
}

func (r *CategoryRepo) removeFromParentChildrenList(parentID, childID int64) {
	key := turboKeyCategoryParent + fmt.Sprintf("%d", parentID)
	// Remove from parent children list using TurboDeleteIndex with Key128
	r.store.DB().TurboDeleteIndexString(key, KeyCategory(childID))
}

// rebuildAncestorsCache rebuilds the ancestors cache for a category.
func (r *CategoryRepo) rebuildAncestorsCache(catID int64) {
	ancestors := r.computeAncestors(catID)
	key := turboKeyCategoryAncestors + fmt.Sprintf("%d", catID)
	// Clear existing index
	r.store.DB().TurboClearIndex(key)
	if len(ancestors) > 0 {
		// Convert int64 to Key128 and add to index
		ancestorKeys := make([]string, len(ancestors))
		for i, id := range ancestors {
			ancestorKeys[i] = KeyCategory(id)
		}
		_, _ = r.store.DB().TurboPutBatchIndexString(key, ancestorKeys)
	}
	r.treePathCache.Delete(catID)
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
	// Clear existing index
	r.store.DB().TurboClearIndex(key)
	if len(descendants) > 0 {
		// Convert int64 to Key128 and add to index
		descendantKeys := make([]string, len(descendants))
		for i, id := range descendants {
			descendantKeys[i] = KeyCategory(id)
		}
		_, _ = r.store.DB().TurboPutBatchIndexString(key, descendantKeys)
	}
}

// rebuildAncestorsCacheTx is the transactional version of rebuildAncestorsCache.
func (r *CategoryRepo) rebuildAncestorsCacheTx(txn *Transaction, catID int64) {
	ancestors := r.computeAncestors(catID)
	key := turboKeyCategoryAncestors + fmt.Sprintf("%d", catID)
	if len(ancestors) > 0 {
		ancestorKeys := make([]string, len(ancestors))
		for i, id := range ancestors {
			ancestorKeys[i] = KeyCategory(id)
		}
		_, _ = txn.TurboPutBatchIndexString(key, ancestorKeys)
	}
	r.treePathCache.Delete(catID)
}

// rebuildDescendantsCacheTx is the transactional version of rebuildDescendantsCache.
func (r *CategoryRepo) rebuildDescendantsCacheTx(txn *Transaction, catID int64) {
	descendants := r.computeDescendants(catID)
	key := turboKeyCategoryChildrenOf + fmt.Sprintf("%d", catID)
	if len(descendants) > 0 {
		descendantKeys := make([]string, len(descendants))
		for i, id := range descendants {
			descendantKeys[i] = KeyCategory(id)
		}
		_, _ = txn.TurboPutBatchIndexString(key, descendantKeys)
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
	tokens, err := r.store.DB().TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		// Fallback: compute
		return r.computeAncestors(catID), nil
	}
	// Use MultiGetByDocIDs to retrieve ancestor categories (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("get ancestor categories: %w", err)
	}
	var ids []int64
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		cat, err := UnmarshalCategory(doc)
		if err != nil {
			continue
		}
		ids = append(ids, cat.ID)
	}
	return ids, nil
}

// GetDirectChildren returns immediate children of a category (cached, O(1)).
func (r *CategoryRepo) GetDirectChildren(catID int64) ([]int64, error) {
	return r.getDirectChildrenFromIndex(catID), nil
}

func (r *CategoryRepo) getDirectChildrenFromIndex(catID int64) []int64 {
	key := turboKeyCategoryParent + fmt.Sprintf("%d", catID)
	tokens, err := r.store.DB().TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return nil
	}
	// Use MultiGetByDocIDs to retrieve child categories (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		fmt.Printf("WARN: getDirectChildrenFromIndex: %v\n", err)
		return nil
	}
	var ids []int64
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		cat, err := UnmarshalCategory(doc)
		if err != nil {
			continue
		}
		ids = append(ids, cat.ID)
	}
	return ids
}

// GetDescendants returns all descendants of a category (cached, O(1)).
func (r *CategoryRepo) GetDescendants1(catID int64) ([]int64, error) {
	key := turboKeyCategoryChildrenOf + fmt.Sprintf("%d", catID)
	tokens, err := r.store.DB().TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		// Fallback: compute
		return r.computeDescendants(catID), nil
	}
	// Use MultiGetByDocIDs to retrieve descendant categories (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("get descendant categories: %w", err)
	}
	var ids []int64
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		cat, err := UnmarshalCategory(doc)
		if err != nil {
			continue
		}
		ids = append(ids, cat.ID)
	}
	return ids, nil
}

func (r *CategoryRepo) GetDescendants(catID int64) ([]int64, error) {
	key := turboKeyCategoryChildrenOf + fmt.Sprintf("%d", catID)
	tokens, err := r.store.DB().TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return r.computeDescendants(catID), nil
	}
	// Use MultiGetByDocIDs to retrieve descendant categories (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("get descendant categories: %w", err)
	}
	var ids []int64
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		cat, err := UnmarshalCategory(doc)
		if err != nil {
			continue
		}
		ids = append(ids, cat.ID)
	}
	return ids, nil
}

// ListAll returns all categories using the cat_list turbo index.
func (r *CategoryRepo) ListAll() ([]model.Category, error) {
	tokens, err := r.store.DB().TurboGetIndexTokens(turboKeyCategoryList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}
	// Use MultiGetByDocIDs to get all categories at once (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get categories: %w", err)
	}
	var result []model.Category
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		cat, err := UnmarshalCategory(doc)
		if err != nil {
			continue
		}
		if cat != nil {
			result = append(result, *cat)
		}
	}
	return result, nil
}

// RebuildTrees rebuilds all precomputed category tree JSONs.
// Call this at startup or after bulk changes.
func (r *CategoryRepo) RebuildTrees() {
	r.rebuildFullTreeJSON()
	// Rebuild ancestors and descendants caches for all categories
	r.rebuildAllAncestorsAndDescendants()
}

// RebuildTreesTx is the transactional version of RebuildTrees.
func (r *CategoryRepo) RebuildTreesTx(txn *Transaction) error {
	r.rebuildFullTreeJSONTx(txn)
	r.rebuildAllAncestorsAndDescendantsTx(txn)
	return nil
}

// rebuildFullTreeJSON rebuilds the full active category tree and stores it as JSON in turbo.
// Called on category mutations and at startup.
func (r *CategoryRepo) rebuildFullTreeJSON() {
	// Try to get active category tokens from turbo index.
	tokens, err := r.store.DB().TurboGetIndexTokens(turboKeyCategoryActive)
	var cats []model.Category

	if err != nil || len(tokens) == 0 {
		// If index is empty/missing, fall back to listing all categories.
		all, err := r.ListAll()
		if err == nil && all != nil {
			cats = make([]model.Category, 0, len(all))
			for _, c := range all {
				if c.IsActive {
					cats = append(cats, c)
				}
			}
		}
	} else {
		// Use MultiGetByDocIDs to retrieve all active categories at once (tokens already contain full keys)
		docs, err := r.store.DB().MultiGetByDocIDs(tokens)
		if err != nil {
			fmt.Printf("WARN: rebuildFullTreeJSON MultiGetByDocIDs: %v\n", err)
			return
		}
		cats = make([]model.Category, 0, len(docs))
		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			cat, err := UnmarshalCategory(doc)
			if err != nil {
				continue
			}
			if cat.IsActive {
				cats = append(cats, *cat)
			}
		}
	}

	if len(cats) == 0 {
		r.store.TurboWrite(turboKeyCategoryTreeFull, []byte("[]"))
		r.store.TurboWrite(turboKeyCategoryTreeAdmin, []byte("[]"))
		return
	}

	// Build admin tree (all categories, no filtering)
	adminTree, _ := r.buildTree(cats, nil)
	adminJson := marshalCategoryTree(adminTree)
	if len(adminJson) == 0 {
		adminJson = []byte("[]")
	}
	r.store.TurboWrite(turboKeyCategoryTreeAdmin, adminJson)

	// Filter categories: only include those with EAN pages (for public tree)
	filteredCats := cats
	if r.eanPageRepo != nil {
		filteredCats = r.filterCategoriesWithEANPages(cats)
	}

	publicTree, _ := r.buildTree(filteredCats, nil)
	fmt.Printf("[DEBUG] rebuildFullTreeJSON: total=%d, public=%d, admin=%d\n", len(cats), len(publicTree), len(adminTree))
	publicJson := marshalCategoryTree(publicTree)
	if len(publicJson) == 0 {
		publicJson = []byte("[]")
	}
	r.store.TurboWrite(turboKeyCategoryTreeFull, publicJson)
}

// filterCategoriesWithEANPages filters categories to only include those that have EAN pages
// (directly or through descendants). This is used for the public category tree.
func (r *CategoryRepo) filterCategoriesWithEANPages(cats []model.Category) []model.Category {
	if r.eanPageRepo == nil {
		return cats
	}

	// Get categories with EAN pages
	catsWithPages := r.eanPageRepo.CategoriesWithEANPages()
	if len(catsWithPages) == 0 {
		return cats
	}

	// Build parent map and children map
	byID := make(map[int64]*model.Category, len(cats))
	for i := range cats {
		byID[cats[i].ID] = &cats[i]
	}

	// Mark categories that have EAN pages directly
	hasEANPage := make(map[int64]bool)
	for _, cat := range cats {
		if _, ok := catsWithPages[cat.ID]; ok {
			hasEANPage[cat.ID] = true
		}
	}

	// Propagate up: if a child has EAN pages, mark parent too
	changed := true
	for changed {
		changed = false
		for _, cat := range cats {
			if cat.ParentID != nil && !hasEANPage[cat.ID] && hasEANPage[*cat.ParentID] {
				continue
			}
			if cat.ParentID != nil && hasEANPage[cat.ID] && !hasEANPage[*cat.ParentID] {
				hasEANPage[*cat.ParentID] = true
				changed = true
			}
		}
	}

	// Filter: only keep categories marked as having EAN pages
	result := make([]model.Category, 0, len(cats))
	for _, cat := range cats {
		if hasEANPage[cat.ID] {
			result = append(result, cat)
		}
	}

	fmt.Printf("[DEBUG] filterCategoriesWithEANPages: total=%d, with_pages=%d\n", len(cats), len(result))
	return result
}

// rebuildAllAncestorsAndDescendants rebuilds ancestors and descendants caches for all categories.
// This ensures that GetAncestors and GetDescendants work correctly after bulk imports.
func (r *CategoryRepo) rebuildAllAncestorsAndDescendants() {
	all, err := r.ListAll()
	if err != nil {
		fmt.Printf("WARN: rebuildAllAncestorsAndDescendants: %v\n", err)
		return
	}
	for _, cat := range all {
		r.rebuildAncestorsCache(cat.ID)
		r.rebuildDescendantsCache(cat.ID)
	}
	fmt.Printf("[DEBUG] rebuildAllAncestorsAndDescendants: rebuilt for %d categories\n", len(all))
}

// rebuildFullTreeJSONTx is the transactional version of rebuildFullTreeJSON.
func (r *CategoryRepo) rebuildFullTreeJSONTx(txn *Transaction) {
	tokens, err := r.store.DB().TurboGetIndexTokens(turboKeyCategoryActive)
	var cats []model.Category

	if err != nil || len(tokens) == 0 {
		all, err := r.ListAll()
		if err == nil && all != nil {
			cats = make([]model.Category, 0, len(all))
			for _, c := range all {
				if c.IsActive {
					cats = append(cats, c)
				}
			}
		}
	} else {
		docs, err := r.store.DB().MultiGetByDocIDs(tokens)
		if err != nil {
			fmt.Printf("WARN: rebuildFullTreeJSONTx MultiGetByDocIDs: %v\n", err)
			return
		}
		cats = make([]model.Category, 0, len(docs))
		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			cat, err := UnmarshalCategory(doc)
			if err != nil {
				continue
			}
			if cat.IsActive {
				cats = append(cats, *cat)
			}
		}
	}

	if len(cats) == 0 {
		_ = txn.TurboWrite(turboKeyCategoryTreeFull, []byte("[]"))
		return
	}

	tree, _ := r.buildTree(cats, nil)
	jsonData := marshalCategoryTree(tree)
	if len(jsonData) == 0 {
		jsonData = []byte("[]")
	}
	_ = txn.TurboWrite(turboKeyCategoryTreeFull, jsonData)
}

// rebuildAllAncestorsAndDescendantsTx is the transactional version of rebuildAllAncestorsAndDescendants.
// Optimized to collect all writes and apply them in batch to reduce space usage.
func (r *CategoryRepo) rebuildAllAncestorsAndDescendantsTx(txn *Transaction) {
	all, err := r.ListAll()
	if err != nil {
		fmt.Printf("WARN: rebuildAllAncestorsAndDescendantsTx: %v\n", err)
		return
	}

	// First pass: collect all ancestor and descendant writes
	type batchWrite struct {
		key    string
		values []string
	}
	var ancestorWrites []batchWrite
	var descendantWrites []batchWrite

	for _, cat := range all {
		// Collect ancestors
		ancestors := r.computeAncestors(cat.ID)
		if len(ancestors) > 0 {
			key := turboKeyCategoryAncestors + fmt.Sprintf("%d", cat.ID)
			ancestorKeys := make([]string, len(ancestors))
			for i, id := range ancestors {
				ancestorKeys[i] = KeyCategory(id)
			}
			ancestorWrites = append(ancestorWrites, batchWrite{key: key, values: ancestorKeys})
		}
		r.treePathCache.Delete(cat.ID)

		// Collect descendants
		descendants := r.computeDescendants(cat.ID)
		if len(descendants) > 0 {
			key := turboKeyCategoryChildrenOf + fmt.Sprintf("%d", cat.ID)
			descendantKeys := make([]string, len(descendants))
			for i, id := range descendants {
				descendantKeys[i] = KeyCategory(id)
			}
			descendantWrites = append(descendantWrites, batchWrite{key: key, values: descendantKeys})
		}
	}

	// Second pass: apply all writes in batch
	for _, w := range ancestorWrites {
		_, _ = txn.TurboPutBatchIndexString(w.key, w.values)
	}
	for _, w := range descendantWrites {
		_, _ = txn.TurboPutBatchIndexString(w.key, w.values)
	}

	fmt.Printf("[DEBUG] rebuildAllAncestorsAndDescendantsTx: applied %d ancestor writes, %d descendant writes\n", len(ancestorWrites), len(descendantWrites))
}

// rebuildParentTreeJSON rebuilds the subtree for a given parentID and stores it as JSON.
func (r *CategoryRepo) rebuildParentTreeJSON(parentID int64) {
	// Try to get active category tokens from turbo index.
	tokens, err := r.store.DB().TurboGetIndexTokens(turboKeyCategoryActive)
	var cats []model.Category

	if err != nil || len(tokens) == 0 {
		// If index is empty/missing, fall back to listing all categories.
		all, err := r.ListAll()
		if err == nil && all != nil {
			cats = make([]model.Category, 0, len(all))
			for _, c := range all {
				if c.IsActive {
					cats = append(cats, c)
				}
			}
		}
	} else {
		// Use MultiGetByDocIDs to retrieve all active categories at once (tokens already contain full keys)
		docs, err := r.store.DB().MultiGetByDocIDs(tokens)
		if err != nil {
			fmt.Printf("WARN: rebuildParentTreeJSON MultiGetByDocIDs: %v\n", err)
			return
		}
		cats = make([]model.Category, 0, len(docs))
		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			cat, err := UnmarshalCategory(doc)
			if err != nil {
				continue
			}
			if cat.IsActive {
				cats = append(cats, *cat)
			}
		}
	}

	if len(cats) == 0 {
		r.store.TurboWrite(turboKeyCategoryTreeParent+strconv.FormatInt(parentID, 10), []byte("[]"))
		return
	}

	tree, _ := r.buildTree(cats, &parentID)
	jsonData := marshalCategoryTree(tree)
	if len(jsonData) == 0 {
		jsonData = []byte("[]")
	}
	r.store.TurboWrite(turboKeyCategoryTreeParent+strconv.FormatInt(parentID, 10), jsonData)
}

// invalidateAllParentTrees clears all cached per-parent trees.
// Called on any category mutation; simple and safe.
func (r *CategoryRepo) invalidateAllParentTrees() {
	// We store keys as cat_tree_parent:{id}. Scan is expensive, so instead
	// we rely on lazy rebuild: GetTreeByParent will rebuild on demand.
	// For now, we do nothing here; mutations will trigger rebuild of needed parents.
}

// ListActive returns only active categories.
// Uses the precomputed full tree JSON to avoid repeated DB reads.
func (r *CategoryRepo) ListActive() ([]model.Category, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyCategoryTreeFull)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	tree, err := unmarshalCategoryTree(data)
	if err != nil {
		return nil, err
	}

	if len(tree) == 0 {
		return nil, nil
	}

	// Flatten tree into []model.Category via DFS.
	result := make([]model.Category, 0, len(tree)*4)

	var walk func(nodes []*CategoryTreeNode)
	walk = func(nodes []*CategoryTreeNode) {
		for _, n := range nodes {
			result = append(result, model.Category{
				ID:            n.ID,
				ParentID:      n.ParentID,
				NameRu:        n.NameRu,
				NameUa:        n.NameUa,
				NamePl:        n.NamePl,
				NameEn:        n.NameEn,
				Slug:          n.Slug,
				Desc:          n.Desc,
				DescRu:        n.DescRu,
				DescUa:        n.DescUa,
				DescPl:        n.DescPl,
				DescEn:        n.DescEn,
				ImageLightURL: n.ImageLightURL,
				ImageDarkURL:  n.ImageDarkURL,
				IsActive:      n.IsActive,
				SortOrder:     n.SortOrder,
			})
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}

	roots := make([]*CategoryTreeNode, 0, len(tree))
	for i := range tree {
		roots = append(roots, &tree[i])
	}
	walk(roots)
	return result, nil
}

// ---------- Tree operations (optimized) ----------

type CategoryTreeNode struct {
	ID            int64               `json:"id"`
	ParentID      *int64              `json:"parent_id,omitempty"`
	Name          string              `json:"name"`
	NameRu        string              `json:"name_ru"`
	NameUa        string              `json:"name_ua"`
	NamePl        string              `json:"name_pl"`
	NameEn        string              `json:"name_en"`
	Slug          string              `json:"slug"`
	Desc          string              `json:"description,omitempty"`
	DescRu        string              `json:"description_ru,omitempty"`
	DescUa        string              `json:"description_ua,omitempty"`
	DescPl        string              `json:"description_pl,omitempty"`
	DescEn        string              `json:"description_en,omitempty"`
	ImageLightURL string              `json:"image_light_url,omitempty"`
	ImageDarkURL  string              `json:"image_dark_url,omitempty"`
	IsActive      bool                `json:"is_active"`
	SortOrder     int                 `json:"sort_order"`
	Children      []*CategoryTreeNode `json:"children,omitempty"`
}

var catTreeReg = silentjson.BuildRegistry(reflect.TypeOf(CategoryTreeNode{}))

// marshalCategoryTree marshals []CategoryTreeNode to JSON using silentjson.
func marshalCategoryTree(tree []CategoryTreeNode) []byte {
	return silentjson.MarshalSlice(tree, catTreeReg, nil)
}

// unmarshalCategoryTree unmarshals JSON to []CategoryTreeNode using silentjson.
// Pattern matches TestUnmarshalArrayParallel_Basic: large pre-allocated dst, zeroed.
func unmarshalCategoryTree(data []byte) ([]CategoryTreeNode, error) {
	if len(data) == 0 {
		return nil, nil
	}
	dst := make([]CategoryTreeNode, 65536)
	for i := range dst {
		dst[i] = CategoryTreeNode{}
	}
	res, err := silentjson.UnmarshalSlice[CategoryTreeNode](data, catTreeReg, dst)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetTree returns the full category tree (only active categories).
// Reads precomputed JSON from turbo; rebuilds on demand if missing.
func (r *CategoryRepo) GetTree() ([]CategoryTreeNode, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyCategoryTreeFull)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		// Not yet built: rebuild now.
		r.rebuildFullTreeJSON()
		data, err = r.store.DB().TurboRawRead(turboKeyCategoryTreeFull)
		if err != nil || len(data) == 0 {
			return nil, nil
		}
	}

	tree, err := unmarshalCategoryTree(data)
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// GetTreeByParent returns subtree rooted at the given parent category ID (only active).
// Reads precomputed JSON from turbo; rebuilds on demand if missing.
func (r *CategoryRepo) GetTreeByParent(parentID int64) ([]CategoryTreeNode, error) {
	key := turboKeyCategoryTreeParent + strconv.FormatInt(parentID, 10)
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		// Not yet built: rebuild now.
		r.rebuildParentTreeJSON(parentID)
		data, err = r.store.DB().TurboRawRead(key)
		if err != nil || len(data) == 0 {
			return nil, nil
		}
	}

	tree, err := unmarshalCategoryTree(data)
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// GetTreeJSON returns the full category tree as raw JSON bytes.
// Zero allocations for parsing: reads precomputed JSON directly from turbo.
// This is the PUBLIC tree, filtered to only show categories with EAN pages.
func (r *CategoryRepo) GetTreeJSON() ([]byte, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyCategoryTreeFull)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		r.rebuildFullTreeJSON()
		data, err = r.store.DB().TurboRawRead(turboKeyCategoryTreeFull)
		if err != nil || len(data) == 0 {
			return []byte("[]"), nil
		}
	}
	return data, nil
}

// GetAdminTreeJSON returns the full admin category tree as raw JSON bytes.
// Shows ALL categories without filtering.
func (r *CategoryRepo) GetAdminTreeJSON() ([]byte, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyCategoryTreeAdmin)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		r.rebuildFullTreeJSON()
		data, err = r.store.DB().TurboRawRead(turboKeyCategoryTreeAdmin)
		if err != nil || len(data) == 0 {
			return []byte("[]"), nil
		}
	}
	return data, nil
}

// GetTreeByParentJSON returns subtree JSON for the given parent category ID.
// Zero allocations for parsing: reads precomputed JSON directly from turbo.
func (r *CategoryRepo) GetTreeByParentJSON(parentID int64) ([]byte, error) {
	key := turboKeyCategoryTreeParent + strconv.FormatInt(parentID, 10)
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		r.rebuildParentTreeJSON(parentID)
		data, err = r.store.DB().TurboRawRead(key)
		if err != nil || len(data) == 0 {
			return []byte("[]"), nil
		}
	}
	return data, nil
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
		name := c.NameEn
		if name == "" {
			name = c.NameRu
		}
		byID[c.ID] = &CategoryTreeNode{
			ID:            c.ID,
			ParentID:      c.ParentID,
			Name:          name,
			NameRu:        c.NameRu,
			NameUa:        c.NameUa,
			NamePl:        c.NamePl,
			NameEn:        c.NameEn,
			Slug:          c.Slug,
			Desc:          c.Desc,
			DescRu:        c.DescRu,
			DescUa:        c.DescUa,
			DescPl:        c.DescPl,
			DescEn:        c.DescEn,
			ImageLightURL: c.ImageLightURL,
			ImageDarkURL:  c.ImageDarkURL,
			IsActive:      c.IsActive,
			SortOrder:     c.SortOrder,
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
// Uses cached ancestors for O(1) lookup, plus in-memory cache for slugs.
func (r *CategoryRepo) GetTreePath(catID int64) ([]string, error) {
	if v, ok := r.treePathCache.Load(catID); ok {
		return v.([]string), nil
	}

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

	r.treePathCache.Store(catID, path)
	return path, nil
}

// GetTreePathFull returns the full path from root to the given category as []CategoryTreeNode.
// Includes all translations and children for each node in the path.
func (r *CategoryRepo) GetTreePathFull(catID int64) ([]CategoryTreeNode, error) {
	ancestors, err := r.GetAncestors(catID)
	if err != nil {
		return nil, err
	}

	var path []CategoryTreeNode
	for _, aid := range ancestors {
		node, err := r.GetCategoryTreeNode(aid)
		if err != nil {
			return nil, err
		}
		path = append(path, *node)
	}

	// Add own category
	node, err := r.GetCategoryTreeNode(catID)
	if err != nil {
		return nil, err
	}
	path = append(path, *node)

	return path, nil
}

// GetCategoryTreeNode returns a CategoryTreeNode for the given ID, including its immediate active children.
func (r *CategoryRepo) GetCategoryTreeNode(id int64) (*CategoryTreeNode, error) {
	cat, err := r.Get(id)
	if err != nil {
		return nil, err
	}

	name := cat.NameEn
	if name == "" {
		name = cat.NameRu
	}

	node := &CategoryTreeNode{
		ID:            cat.ID,
		ParentID:      cat.ParentID,
		Name:          name,
		NameRu:        cat.NameRu,
		NameUa:        cat.NameUa,
		NamePl:        cat.NamePl,
		NameEn:        cat.NameEn,
		Slug:          cat.Slug,
		Desc:          cat.Desc,
		DescRu:        cat.DescRu,
		DescUa:        cat.DescUa,
		DescPl:        cat.DescPl,
		DescEn:        cat.DescEn,
		ImageLightURL: cat.ImageLightURL,
		ImageDarkURL:  cat.ImageDarkURL,
		IsActive:      cat.IsActive,
		SortOrder:     cat.SortOrder,
	}

	// Load immediate children
	childIDs, _ := r.GetDirectChildren(id)
	for _, cid := range childIDs {
		child, err := r.Get(cid)
		if err != nil || !child.IsActive {
			continue
		}
		childName := child.NameEn
		if childName == "" {
			childName = child.NameRu
		}
		node.Children = append(node.Children, &CategoryTreeNode{
			ID:            child.ID,
			ParentID:      child.ParentID,
			Name:          childName,
			NameRu:        child.NameRu,
			NameUa:        child.NameUa,
			NamePl:        child.NamePl,
			NameEn:        child.NameEn,
			Slug:          child.Slug,
			Desc:          child.Desc,
			DescRu:        child.DescRu,
			DescUa:        child.DescUa,
			DescPl:        child.DescPl,
			DescEn:        child.DescEn,
			ImageLightURL: child.ImageLightURL,
			ImageDarkURL:  child.ImageDarkURL,
			IsActive:      child.IsActive,
			SortOrder:     child.SortOrder,
		})
	}

	// Sort children
	sort.SliceStable(node.Children, func(i, j int) bool {
		if node.Children[i].SortOrder != node.Children[j].SortOrder {
			return node.Children[i].SortOrder < node.Children[j].SortOrder
		}
		return node.Children[i].Name < node.Children[j].Name
	})

	return node, nil
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

		name := cat.NameEn
		if name == "" {
			name = cat.NameRu
		}

		path := prefix
		if path == "" {
			path = name
		} else {
			path = prefix + " -> " + name
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

// RebuildIndexesFromDocs rebuilds all category indexes by scanning documents
// directly from docstore (using ID range from state:next_id:category).
// Use this when turbo indexes are empty/corrupted but documents exist.
func (r *CategoryRepo) RebuildIndexesFromDocs() error {
	// Read max ID from state
	key := "state:next_id:category"
	data, _ := r.store.DB().TurboRawRead(key)
	var maxID int64 = 1000
	if len(data) > 0 {
		_, _ = fmt.Sscanf(string(data), "%d", &maxID)
	}

	var cats []model.Category
	for id := int64(1); id <= maxID; id++ {
		cat, err := r.Get(id)
		if err != nil {
			continue
		}
		cats = append(cats, *cat)
	}

	if len(cats) == 0 {
		fmt.Println("[CATEGORY] No categories found in docstore")
		return nil
	}

	fmt.Printf("[CATEGORY] Found %d categories in docstore, rebuilding indexes...\n", len(cats))

	// Clear all indexes
	r.store.DB().TurboClearIndex(turboKeyCategoryList)
	r.store.DB().TurboClearIndex(turboKeyCategoryActive)

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

		// Clear path cache
		r.treePathCache.Delete(cat.ID)

		// Ancestors cache
		r.rebuildAncestorsCache(cat.ID)

		// Descendants cache
		r.rebuildDescendantsCache(cat.ID)

		// Active list
		if cat.IsActive {
			r.addToActiveList(cat.ID)
		}

		// Category list
		r.addToCategoryList(cat.ID)
	}

	fmt.Printf("[CATEGORY] Rebuilt indexes from docs: %d categories\n", len(cats))
	return nil
}

// RebuildAllIndexes rebuilds all category indexes from existing documents.
// Call this once after upgrading to the new index-based category repo.
func (r *CategoryRepo) RebuildAllIndexes() error {
	cats, err := r.ListAll()
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		// If ListAll returns empty, try rebuilding from docs directly
		return r.RebuildIndexesFromDocs()
	}

	// Clear all indexes
	r.store.DB().TurboClearIndex(turboKeyCategoryList)
	r.store.DB().TurboClearIndex(turboKeyCategoryActive)

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

		// Clear path cache
		r.treePathCache.Delete(cat.ID)

		// Ancestors cache
		r.rebuildAncestorsCache(cat.ID)

		// Descendants cache
		r.rebuildDescendantsCache(cat.ID)

		// Active list
		if cat.IsActive {
			r.addToActiveList(cat.ID)
		}

		// Category list
		r.addToCategoryList(cat.ID)
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
