package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GenshIv/makoshop/internal/attrs"
	"github.com/GenshIv/makoshop/internal/model"
)

// Turbo index keys for attrdefs:
// turbo_attrdef_list            -> JSON array of codes
// turbo_attrdef_code:{code}     -> docID
// turbo_attrdef_cats:{code}     -> [categoryIDs]
// turbo_attrdef_cat_codes:{catID} -> [codes] (reverse: cat -> codes, O(1))
// turbo_attr_values_cat:{code}:{catID} -> [hashes]

const (
	turboKeyAttrDefList     = "attrdef_list"
	turboKeyAttrDefCode     = "attrdef_code:"
	turboKeyAttrDefCats     = "attrdef_cats:"
	turboKeyAttrDefCatCodes = "attrdef_cat_codes:" // catID -> [codes]
	turboKeyAttrValuesCat   = "attr_values_cat:"
	turboKeyAttrDefKey      = "attrdef_key:" // raw key from HTML -> code
)

type AttrDefRepo struct {
	store *Store

	// keyCache maps a normalized raw key (lowercase) to its AttrDef.
	// It is the first line of defense against duplicate AttrDef documents:
	// within a process, a key is resolved at most once against the DB.
	mu       sync.Mutex
	keyCache map[string]*model.AttrDef

	// pendingListCodes accumulates codes of AttrDefs created since the last
	// FlushList. makodb is append-only: rewriting the whole attrdef_list on
	// every single creation (O(n) each) bloats the DB by ~1GB per import.
	// Instead we buffer the codes in memory and write the list ONCE per batch
	// via FlushList (a deduplicated set-union).
	pendingListCodes map[string]bool
}

func NewAttrDefRepo(store *Store) *AttrDefRepo {
	return &AttrDefRepo{
		store:            store,
		keyCache:         make(map[string]*model.AttrDef),
		pendingListCodes: make(map[string]bool),
	}
}

// addPendingListCode buffers a newly created code for a later batched list
// write. Cheap (in-memory), no DB access.
func (r *AttrDefRepo) addPendingListCode(code string) {
	if code == "" {
		return
	}
	r.mu.Lock()
	r.pendingListCodes[code] = true
	r.mu.Unlock()
}

// FlushList writes all buffered codes to the registry list in ONE deduplicated
// set-union write, then clears the buffer. Call once per import batch (before
// commit) so the list is updated a constant number of times, not once per
// created AttrDef.
func (r *AttrDefRepo) FlushList() error {
	r.mu.Lock()
	pending := r.pendingListCodes
	r.pendingListCodes = make(map[string]bool)
	r.mu.Unlock()

	if len(pending) == 0 {
		return nil
	}
	return r.unionAttrDefList(pending)
}

// HealList ensures every AttrDef document is present in the registry list.
// It probes all documents (id 1..nextID) and adds any missing codes to the
// list in ONE batched write. This repairs "doc not in list" orphans (e.g.
// docs created by GetByCode's on-the-fly fallback before the buffering fix).
func (r *AttrDefRepo) HealList() (int, error) {
	maxID := r.currentNextID("attrdef")
	if maxID <= 0 {
		return 0, nil
	}

	codes := make(map[string]bool)
	for id := int64(1); id <= maxID; id++ {
		data, err := r.store.DocGet(fmt.Sprintf("attrdef:%d", id))
		if err != nil || len(data) == 0 {
			continue
		}
		var d struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(data, &d) != nil || d.Code == "" {
			continue
		}
		codes[d.Code] = true
	}

	if len(codes) == 0 {
		return 0, nil
	}
	if err := r.unionAttrDefList(codes); err != nil {
		return 0, err
	}
	return len(codes), nil
}

// currentNextID reads the current ID counter for an entity without incrementing.
func (r *AttrDefRepo) currentNextID(entityType string) int64 {
	data, _ := r.store.DB().TurboRawRead(fmt.Sprintf("state:next_id:%s", entityType))
	if len(data) == 0 {
		return 0
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return id
}

// cacheGet returns the cached AttrDef for a raw key, if present.
func (r *AttrDefRepo) cacheGet(rawKey string) (*model.AttrDef, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ad, ok := r.keyCache[rawKey]
	return ad, ok
}

// cacheSet stores an AttrDef for a raw key.
func (r *AttrDefRepo) cacheSet(rawKey string, ad *model.AttrDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keyCache[rawKey] = ad
}

// ---------- CRUD ----------

func (r *AttrDefRepo) GetByCode(code string) (*model.AttrDef, error) {
	key := turboKeyAttrDefCode + code
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, ErrKeyNotFound
	}

	var docID uint64
	_, _ = fmt.Sscanf(string(data), "%d", &docID)

	ad, err := r.Get(int64(docID))
	if err == nil {
		return ad, nil
	}

	// Document doesn't exist (e.g. created by BatchUpsertCodes).
	// Create a default AttrDef on the fly.
	cats, _ := r.GetCategories(code)
	if cats == nil {
		cats = []int64{}
	}

	ad = &model.AttrDef{
		ID:           int64(docID),
		Code:         code,
		NameRu:       "",
		Categories:   cats,
		Type:         "string",
		IsActive:     true,
		IsFilterable: true,
		IsSortable:   false,
		SortOrder:    0,
		CreatedAt:    time.Now().Unix(),
	}

	buf, _ := json.Marshal(ad)
	_ = r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), buf)

	// Register the code for a batched list write so the doc is not left out of
	// the registry list (which would make it a "doc not in list" orphan).
	r.addPendingListCode(code)

	return ad, nil
}

// GetOrCreateByKey finds an AttrDef by raw key (e.g. "Moc", "Power") or creates one.
// Used by HTMLAttrKeyParser to map raw keys from HTML to attribute codes.
//
// Resolution order (single entry point for all import paths):
//  1. in-process cache (raw key, lowercase)
//  2. alias index attrdef_key:{rawkey_lower} -> code -> doc
//  3. validated code from attrs.CodeFromKey:
//     a. alias index attrdef_key:{code} -> code -> doc (normalized alias)
//     b. exact code match attrdef_code:{code} -> doc
//     c. create new AttrDef (NextID) + indexes atomically
//
// Invariant: exactly one AttrDef document per code. A document is created
// only after both the cache and the attrdef_code index confirmed absence.
func (r *AttrDefRepo) GetOrCreateByKey(rawKey string) (*model.AttrDef, error) {
	if rawKey == "" {
		return nil, ErrKeyNotFound
	}

	key := strings.TrimSpace(rawKey)
	keyLower := strings.ToLower(key)

	// 1. In-process cache.
	if ad, ok := r.cacheGet(keyLower); ok {
		return ad, nil
	}

	// 2. Alias index: raw key -> code.
	if code, ok := r.lookupKeyAlias(keyLower); ok {
		if ad, err := r.GetByCode(code); err == nil {
			r.cacheSet(keyLower, ad)
			return ad, nil
		}
	}

	// 3. Validate and derive the canonical code.
	code, ok := attrs.CodeFromKey(key)
	if !ok {
		return nil, ErrInvalidAttrKey
	}

	// 3a. Normalized alias: the canonical code may already be aliased to
	// an existing (older) code.
	if mapped, ok := r.lookupKeyAlias(code); ok {
		if ad, err := r.GetByCode(mapped); err == nil {
			r.cacheSet(keyLower, ad)
			return ad, nil
		}
	}

	// 3b. Exact code match.
	if ad, err := r.GetByCode(code); err == nil {
		// Register the raw key as an alias (deduplicated) and cache.
		_ = r.store.DB().TurboRawWrite(turboKeyAttrDefKey+keyLower, []byte(ad.Code))
		r.addKeyToDef(ad, key)
		r.cacheSet(keyLower, ad)
		return ad, nil
	}

	// 3c. Create new AttrDef.
	ad, err := r.createAttrDef(code, key)
	if err != nil {
		return nil, err
	}
	r.cacheSet(keyLower, ad)
	return ad, nil
}

// lookupKeyAlias reads the alias index attrdef_key:{key} -> code.
func (r *AttrDefRepo) lookupKeyAlias(key string) (string, bool) {
	data, err := r.store.DB().TurboRawRead(turboKeyAttrDefKey + key)
	if err != nil || len(data) == 0 {
		return "", false
	}
	code := strings.TrimSpace(string(data))
	if code == "" {
		return "", false
	}
	return code, true
}

// addKeyToDef appends a raw key to an existing AttrDef's Keys (deduplicated)
// and persists the document.
func (r *AttrDefRepo) addKeyToDef(ad *model.AttrDef, key string) {
	for _, k := range ad.Keys {
		if strings.EqualFold(k, key) {
			return // already present (dedup)
		}
	}
	ad.Keys = append(ad.Keys, key)
	buf, _ := json.Marshal(ad)
	_ = r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), buf)
}

// createAttrDef creates a new AttrDef document for a validated code and
// writes all its indexes. The doc + attrdef_code + alias indexes are written
// as one logical operation; a crash between writes can only leave an
// orphaned document (never read) or a missing alias (recreated on next
// resolve) — never a second document for the same code.
func (r *AttrDefRepo) createAttrDef(code, rawKey string) (*model.AttrDef, error) {
	// Final guard: re-check the code index (single-writer assumption holds
	// for imports, but this makes the invariant explicit).
	if data, err := r.store.DB().TurboRawRead(turboKeyAttrDefCode + code); err == nil && len(data) > 0 {
		var docID int64
		_, _ = fmt.Sscanf(string(data), "%d", &docID)
		if ad, err := r.Get(int64(docID)); err == nil {
			return ad, nil
		}
	}

	id, err := r.store.NextID("attrdef")
	if err != nil {
		return nil, err
	}

	ad := &model.AttrDef{
		ID:           id,
		Code:         code,
		NameRu:       rawKey,
		Categories:   []int64{},
		Type:         model.AttrTypeString,
		IsActive:     true,
		IsFilterable: true,
		IsSortable:   false,
		SortOrder:    0,
		Keys:         []string{rawKey},
		CreatedAt:    time.Now().Unix(),
	}

	buf, _ := json.Marshal(ad)
	if err := r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), buf); err != nil {
		return nil, err
	}

	// Code index (raw key -> docID).
	if err := r.store.TurboWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", ad.ID))); err != nil {
		return nil, err
	}

	// Alias indexes: the raw key and the canonical code both resolve to it.
	keyLower := strings.ToLower(rawKey)
	_ = r.store.TurboWrite(turboKeyAttrDefKey+keyLower, []byte(code))
	if keyLower != code {
		_ = r.store.TurboWrite(turboKeyAttrDefKey+code, []byte(code))
	}

	// Registry list: buffer for a single batched write (FlushList). Writing
	// the whole list per creation would bloat the append-only DB.
	r.addPendingListCode(code)

	return ad, nil
}

// BatchGetOrCreateByKeys creates AttrDef for all keys that don't exist yet.
// Returns a map of key -> created AttrDef (only newly created ones).
// Updates attrdef_list once at the end to avoid vacuum.
func (r *AttrDefRepo) BatchGetOrCreateByKeys(keys []string) (map[string]*model.AttrDef, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	resolved := make(map[string]*model.AttrDef)

	for _, rawKey := range keys {
		if rawKey == "" {
			continue
		}
		key := strings.TrimSpace(rawKey)

		ad, err := r.GetOrCreateByKey(key)
		if err != nil {
			// Invalid key (a value, a sentence…) — skip, not an error for the batch.
			if errors.Is(err, ErrInvalidAttrKey) {
				continue
			}
			return nil, err
		}
		resolved[key] = ad
	}

	// Flush all buffered codes (this batch + any created on-the-fly earlier)
	// to the registry list in ONE deduplicated set-union write.
	if err := r.FlushList(); err != nil {
		return nil, err
	}

	return resolved, nil
}

// isInAttrDefList reports whether a code is already present in the registry
// list (attrdef_list).
func (r *AttrDefRepo) isInAttrDefList(code string) (bool, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	if err != nil || len(data) == 0 {
		return false, nil
	}
	var codes []string
	if err := json.Unmarshal(data, &codes); err != nil {
		return false, err
	}
	for _, c := range codes {
		if c == code {
			return true, nil
		}
	}
	return false, nil
}

// unionAttrDefList adds codes to the registry list as a deduplicated set:
// the list is read, pre-existing duplicates dropped (self-healing), missing
// codes appended (sorted for stability), and the whole set written back in
// one write. Never produces duplicates.
func (r *AttrDefRepo) unionAttrDefList(codes map[string]bool) error {
	data, _ := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	var list []string
	if len(data) > 0 {
		if err := json.Unmarshal(data, &list); err != nil {
			list = nil
		}
	}

	existing := make(map[string]bool, len(list))
	var deduped []string
	for _, c := range list {
		if c == "" || existing[c] {
			continue
		}
		existing[c] = true
		deduped = append(deduped, c)
	}

	added := make([]string, 0, len(codes))
	for c := range codes {
		if !existing[c] {
			existing[c] = true
			added = append(added, c)
		}
	}
	sort.Strings(added)
	list = append(deduped, added...)

	buf, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return r.store.TurboWrite(turboKeyAttrDefList, buf)
}

func (r *AttrDefRepo) Get(id int64) (*model.AttrDef, error) {
	data, err := r.store.DocGet(fmt.Sprintf("attrdef:%d", id))
	if err != nil {
		return nil, err
	}

	// Unmarshal into map first to detect legacy "name" field
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// Migrate legacy "name" -> "name_ru"
	if name, ok := raw["name"].(string); ok && name != "" {
		if _, hasRu := raw["name_ru"]; !hasRu || raw["name_ru"] == "" {
			raw["name_ru"] = name
		}
		delete(raw, "name")
	}

	updatedData, _ := json.Marshal(raw)
	var ad model.AttrDef
	if err := json.Unmarshal(updatedData, &ad); err != nil {
		return nil, err
	}

	// Persist migrated doc if changed
	if string(updatedData) != string(data) {
		_ = r.store.DocPut(fmt.Sprintf("attrdef:%d", id), updatedData)
	}

	return &ad, nil
}

// ---------- Index-based lookups (O(1)) ----------

// GetCategories returns all category IDs where this attribute code is used.
func (r *AttrDefRepo) GetCategories(code string) ([]int64, error) {
	key := turboKeyAttrDefCats + code
	tokens, err := r.store.DB().TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}
	// Use MultiGetByDocIDs to retrieve category documents (tokens already contain full keys)
	docs, err := r.store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get categories: %w", err)
	}
	var cats []int64
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		cat, err := UnmarshalCategory(doc)
		if err != nil {
			continue
		}
		cats = append(cats, cat.ID)
	}
	return cats, nil
}

// GetCodesForCategory returns all attribute codes for a category in O(1).
// Uses turbo_attrdef_cat_codes:{catID} index.
func (r *AttrDefRepo) GetCodesForCategory(catID int64) ([]string, error) {
	key := turboKeyAttrDefCatCodes + fmt.Sprintf("%d", catID)
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	var codes []string
	if err := json.Unmarshal(data, &codes); err != nil {
		return nil, nil
	}
	return codes, nil
}

// GetCodesForCategoryTree returns attribute codes for a category considering its subtree.
// Uses cached indexes instead of scanning all categories.
func (r *AttrDefRepo) GetCodesForCategoryTree(catID int64, categoryRepo *CategoryRepo) ([]string, error) {
	// 1. Get direct codes for this category (O(1))
	codes, err := r.GetCodesForCategory(catID)
	if err != nil {
		return nil, err
	}
	if len(codes) > 0 {
		return filterURLAttributes(codes), nil
	}

	// 2. No direct codes — check direct children (O(1) via cached children)
	children, err := categoryRepo.GetDirectChildren(catID)
	if err != nil {
		return nil, err
	}
	if len(children) == 0 {
		return nil, nil
	}

	// 3. Intersect codes of all direct children using sorted slices (less alloc than map).
	firstCodes, err := r.GetCodesForCategory(children[0])
	if err != nil || len(firstCodes) == 0 {
		return nil, nil
	}
	sort.Strings(firstCodes)

	for _, cid := range children[1:] {
		childCodes, err := r.GetCodesForCategory(cid)
		if err != nil || len(childCodes) == 0 {
			return nil, nil
		}
		sort.Strings(childCodes)
		firstCodes = intersectSortedStrings(firstCodes, childCodes)
		if len(firstCodes) == 0 {
			return nil, nil
		}
	}

	return filterURLAttributes(firstCodes), nil
}

// filterURLAttributes filters out product_url, purchase_url, and shop_category from attribute codes.
// These are now separate fields on Product, not attributes.
func filterURLAttributes(codes []string) []string {
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		if code != "product_url" && code != "purchase_url" && code != "shop_category" {
			result = append(result, code)
		}
	}
	return result
}

// intersectSortedStrings returns the intersection of two sorted string slices.
func intersectSortedStrings(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	result := make([]string, 0, minLen(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetAttrValuesForCategory returns all values for an attribute code in a specific category.
// Reads attr_values_cat:{code}:{catID} as a JSON map {value: true} (written by IndexEANPageBatch).
func (r *AttrDefRepo) GetAttrValuesForCategory(code string, catID int64) ([]string, error) {
	if catID == 0 {
		return nil, nil
	}
	key := turboKeyAttrValuesCat + code + ":" + fmt.Sprintf("%d", catID)
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	// Parse as JSON map {value: true}
	var valuesMap map[string]interface{}
	if err := json.Unmarshal(data, &valuesMap); err != nil {
		return nil, nil
	}
	if len(valuesMap) == 0 {
		return nil, nil
	}

	values := make([]string, 0, len(valuesMap))
	for val := range valuesMap {
		values = append(values, val)
	}
	sort.Strings(values)
	return values, nil
}

func (r *AttrDefRepo) hashesToStrings(code string, keys []string) ([]string, error) {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		// Convert Key128 to uint64 (assuming direct representation: Key128{0, hash})
		h := key[1]
		hexH := fmt.Sprintf("%x", h)
		labelKey := "attr_label:" + code + ":" + hexH
		labelData, _ := r.store.DB().TurboRawRead(labelKey)
		value := string(labelData)
		if value == "" {
			value = hexH
		}
		values = append(values, value)
	}
	return values, nil
}

// ---------- Index management ----------

// updateCatCodesIndex updates the cat->codes index when categories change for a code.
func (r *AttrDefRepo) updateCatCodesIndex(code string, oldCats, newCats []int64) error {
	// Remove code from old categories that are no longer in newCats
	newSet := make(map[int64]struct{}, len(newCats))
	for _, c := range newCats {
		newSet[c] = struct{}{}
	}

	for _, oldCat := range oldCats {
		if _, ok := newSet[oldCat]; !ok {
			r.removeFromCatCodes(oldCat, code)
		}
	}

	// Add code to new categories that didn't have it
	for _, newCat := range newCats {
		if _, ok := newSet[newCat]; ok {
			// Check if already present
			existing, _ := r.GetCodesForCategory(newCat)
			found := false
			for _, c := range existing {
				if c == code {
					found = true
					break
				}
			}
			if !found {
				r.addToCatCodes(newCat, code)
			}
		}
	}

	return nil
}

func (r *AttrDefRepo) addToCatCodes(catID int64, code string) error {
	return r.mergeCatCodes(catID, map[string]bool{code: true})
}

// mergeCatCodes merges a set of codes into attrdef_cat_codes:{catID} with a
// SINGLE read + a SINGLE write (deduplicated, sorted for stability). Calling
// this once per category with all its codes avoids O(n) rewrites of the same
// key, which would bloat the append-only DB.
func (r *AttrDefRepo) mergeCatCodes(catID int64, newCodes map[string]bool) error {
	key := turboKeyAttrDefCatCodes + fmt.Sprintf("%d", catID)
	data, _ := r.store.DB().TurboRawRead(key)
	existing := make(map[string]bool)
	if data != nil && len(data) > 0 {
		var codes []string
		if json.Unmarshal(data, &codes) == nil {
			for _, c := range codes {
				if c != "" {
					existing[c] = true
				}
			}
		}
	}
	for c := range newCodes {
		existing[c] = true
	}
	merged := make([]string, 0, len(existing))
	for c := range existing {
		merged = append(merged, c)
	}
	sort.Strings(merged)
	buf, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	return r.store.TurboWrite(key, buf)
}

func (r *AttrDefRepo) removeFromCatCodes(catID int64, code string) error {
	key := turboKeyAttrDefCatCodes + fmt.Sprintf("%d", catID)
	data, _ := r.store.DB().TurboRawRead(key)
	if len(data) == 0 {
		return nil
	}
	var codes []string
	if err := json.Unmarshal(data, &codes); err != nil {
		return err
	}
	var newCodes []string
	for _, c := range codes {
		if c != code {
			newCodes = append(newCodes, c)
		}
	}
	if len(newCodes) > 0 {
		buf, _ := json.Marshal(newCodes)
		return r.store.TurboWrite(key, buf)
	}
	return r.store.TurboWrite(key, []byte{})
}

// ---------- CRUD with index updates ----------

func (r *AttrDefRepo) Create(code string, ad *model.AttrDef) error {
	if code == "" {
		return fmt.Errorf("code is required")
	}

	_, err := r.GetByCode(code)
	if err == nil {
		return fmt.Errorf("attrdef %s already exists", code)
	}

	id, err := r.store.NextID("attrdef")
	if err != nil {
		return err
	}

	ad.ID = id
	ad.Code = code
	ad.CreatedAt = time.Now().Unix()
	if ad.IsActive == false {
		ad.IsActive = true
	}

	data, err := json.Marshal(ad)
	if err != nil {
		return err
	}

	if err := r.store.DocPut(fmt.Sprintf("attrdef:%d", id), data); err != nil {
		return err
	}

	// Index: code -> docID
	if err := r.store.TurboWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", id))); err != nil {
		return err
	}

	// Index: code -> categories
	if len(ad.Categories) > 0 {
		// Convert categories to Key128 IDs
		catKeys := make([]string, len(ad.Categories))
		for i, catID := range ad.Categories {
			catKeys[i] = KeyCategory(catID)
		}
		if _, err := r.store.DB().TurboPutBatchIndexString(turboKeyAttrDefCats+code, catKeys); err != nil {
			return err
		}
	}

	// Index: cat -> codes
	for _, catID := range ad.Categories {
		r.addToCatCodes(catID, code)
	}

	// Add to list
	return r.addToAttrDefList(code)
}

func (r *AttrDefRepo) Update(code string, updater func(*model.AttrDef)) error {
	ad, err := r.GetByCode(code)
	if err != nil {
		return err
	}

	oldCats := make([]int64, len(ad.Categories))
	copy(oldCats, ad.Categories)

	updater(ad)

	data, err := json.Marshal(ad)
	if err != nil {
		return err
	}

	if err := r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), data); err != nil {
		return err
	}

	// Update code -> categories
	if len(ad.Categories) > 0 {
		// Convert categories to Key128 IDs
		catKeys := make([]string, len(ad.Categories))
		for i, catID := range ad.Categories {
			catKeys[i] = KeyCategory(catID)
		}
		_, _ = r.store.DB().TurboPutBatchIndexString(turboKeyAttrDefCats+code, catKeys)
	} else {
		_, _ = r.store.DB().TurboPutBatchIndexString(turboKeyAttrDefCats+code, []string{})
	}

	// Update cat -> codes
	return r.updateCatCodesIndex(code, oldCats, ad.Categories)
}

func (r *AttrDefRepo) Delete(code string) error {
	ad, err := r.GetByCode(code)
	if err != nil {
		return err
	}

	// Remove from cat -> codes
	for _, catID := range ad.Categories {
		r.removeFromCatCodes(catID, code)
	}

	// Remove doc
	_ = r.store.DocDelete(fmt.Sprintf("attrdef:%d", ad.ID))

	// Remove indexes
	_ = r.store.TurboWrite(turboKeyAttrDefCode+code, []byte{})
	_ = r.store.TurboWrite(turboKeyAttrDefCats+code, []byte{})

	// Remove from list
	data, _ := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	var codes []string
	if data != nil && len(data) > 0 {
		json.Unmarshal(data, &codes)
	}
	var newCodes []string
	for _, c := range codes {
		if c != code {
			newCodes = append(newCodes, c)
		}
	}
	if len(newCodes) > 0 {
		buf, _ := json.Marshal(newCodes)
		_ = r.store.TurboWrite(turboKeyAttrDefList, buf)
	} else {
		_ = r.store.TurboWrite(turboKeyAttrDefList, []byte{})
	}

	return nil
}

// UpsertCode adds a code to the attrdef system and links it to a category.
func (r *AttrDefRepo) UpsertCode(code string, catID int64) error {
	if code == "" || catID == 0 {
		return nil
	}

	existing, _ := r.GetByCode(code)

	if existing != nil {
		cats, _ := r.GetCategories(code)
		for _, c := range cats {
			if c == catID {
				return nil
			}
		}
		cats = append(cats, catID)
		// Convert categories to Key128 IDs
		catKeys := make([]string, len(cats))
		for i, catID := range cats {
			catKeys[i] = KeyCategory(catID)
		}
		if _, err := r.store.DB().TurboPutBatchIndexString(turboKeyAttrDefCats+code, catKeys); err != nil {
			return err
		}
		return r.addToCatCodes(catID, code)
	}

	id, err := r.store.NextID("attrdef")
	if err != nil {
		return err
	}

	ad := &model.AttrDef{
		ID:         id,
		Code:       code,
		Categories: []int64{catID},
		CreatedAt:  time.Now().Unix(),
	}

	data, _ := json.Marshal(ad)
	if err := r.store.DocPut(fmt.Sprintf("attrdef:%d", id), data); err != nil {
		return err
	}

	if err := r.store.TurboWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", id))); err != nil {
		return err
	}

	// Convert category ID to Key128
	if _, err := r.store.DB().TurboPutIndexString(turboKeyAttrDefCats+code, KeyCategory(catID)); err != nil {
		return err
	}

	r.addToCatCodes(catID, code)
	return r.addToAttrDefList(code)
}

// RemoveCategoryLink unbinds a category from an attribute code:
//  1. attrdef_cats:{code} — delete the category token (turbo set, no dupes)
//  2. attrdef_cat_codes:{catID} — remove the code (deduplicated set)
//  3. AttrDef document — keep the Categories field in sync
func (r *AttrDefRepo) RemoveCategoryLink(code string, catID int64) error {
	if code == "" || catID == 0 {
		return nil
	}

	// 1. Delete the category token from the code's category index.
	_, _ = r.store.DB().TurboDeleteIndexString(turboKeyAttrDefCats+code, KeyCategory(catID))

	// 2. Remove the code from the category's code list (reverse index).
	if err := r.removeFromCatCodes(catID, code); err != nil {
		return err
	}

	// 3. Keep the document's Categories field in sync.
	if ad, err := r.GetByCode(code); err == nil {
		var newCats []int64
		for _, c := range ad.Categories {
			if c != catID {
				newCats = append(newCats, c)
			}
		}
		ad.Categories = newCats
		if buf, err := json.Marshal(ad); err == nil {
			_ = r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), buf)
		}
	}

	return nil
}

func (r *AttrDefRepo) addToAttrDefList(code string) error {
	data, _ := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	var codes []string
	if data != nil && len(data) > 0 {
		json.Unmarshal(data, &codes)
	}
	for _, c := range codes {
		if c == code {
			return nil
		}
	}
	codes = append(codes, code)
	buf, _ := json.Marshal(codes)
	return r.store.TurboWrite(turboKeyAttrDefList, buf)
}

// BatchUpsertCodes batch-inserts all attr codes with their category links.
// Uses real numeric IDs for attrdef_code:{code} (not hashes).
func (r *AttrDefRepo) BatchUpsertCodes(codeCats map[string]map[int64]struct{}) error {
	if len(codeCats) == 0 {
		return nil
	}

	var allCodes []string
	for code := range codeCats {
		allCodes = append(allCodes, code)
	}

	listBuf, _ := json.Marshal(allCodes)
	if err := r.store.TurboWrite(turboKeyAttrDefList, listBuf); err != nil {
		return fmt.Errorf("write attrdef_list: %w", err)
	}

	for code, catSet := range codeCats {
		cats := make([]int64, 0, len(catSet))
		for c := range catSet {
			cats = append(cats, c)
		}

		// Convert categories to Key128 IDs
		catKeys := make([]string, len(cats))
		for i, catID := range cats {
			catKeys[i] = KeyCategory(catID)
		}
		if _, err := r.store.DB().TurboPutBatchIndexString(turboKeyAttrDefCats+code, catKeys); err != nil {
			fmt.Printf("WARN: write attrdef_cats %s: %v\n", code, err)
		}

		// Get or create real ID for this code
		id, err := r.store.NextID("attrdef")
		if err != nil {
			fmt.Printf("WARN: get next ID for attrdef %s: %v\n", code, err)
			continue
		}

		// Check if doc already exists, if not create default
		docKey := fmt.Sprintf("attrdef:%d", id)
		existingData, _ := r.store.DB().TurboRawRead(docKey)
		if len(existingData) == 0 {
			ad := &model.AttrDef{
				ID:           id,
				Code:         code,
				Categories:   cats,
				Type:         "string",
				IsActive:     true,
				IsFilterable: true,
				CreatedAt:    time.Now().Unix(),
			}
			data, _ := json.Marshal(ad)
			if err := r.store.DocPut(docKey, data); err != nil {
				fmt.Printf("WARN: write attrdef doc %s: %v\n", docKey, err)
			}
		}

		// Write real ID (not hash)
		if err := r.store.TurboWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", id))); err != nil {
			fmt.Printf("WARN: write attrdef_code %s: %v\n", code, err)
		}

		// Build cat->codes map in memory
		for _, catID := range cats {
			key := turboKeyAttrDefCatCodes + fmt.Sprintf("%d", catID)
			existingData, _ := r.store.DB().TurboRawRead(key)
			var existingCodes []string
			if existingData != nil && len(existingData) > 0 {
				json.Unmarshal(existingData, &existingCodes)
			}
			found := false
			for _, c := range existingCodes {
				if c == code {
					found = true
					break
				}
			}
			if !found {
				existingCodes = append(existingCodes, code)
				buf, _ := json.Marshal(existingCodes)
				r.store.TurboWrite(key, buf)
			}
		}
	}

	return nil
}

// BatchWriteAttrValues batch-writes attr value references.
// Writes attr_values_cat:{code}:{catID} as JSON map {value: true} (consistent with IndexEANPageBatch).
// Writes attr_label:{code}:{value} with raw value (consistent with IndexEANPageBatch).
func (r *AttrDefRepo) BatchWriteAttrValues(codeValues map[string]map[string]struct{}, codeCatValues map[string]map[int64]map[string]struct{}) error {
	if len(codeValues) == 0 {
		return nil
	}

	for code, valSet := range codeValues {
		if len(valSet) == 0 {
			continue
		}

		// Write labels with raw values
		for val := range valSet {
			labelKey := "attr_label:" + code + ":" + val
			if err := r.store.TurboWrite(labelKey, []byte(val)); err != nil {
				fmt.Printf("WARN: write attr_label %s: %v\n", labelKey, err)
			}
		}

		if catVals, ok := codeCatValues[code]; ok {
			for catID, catValSet := range catVals {
				if len(catValSet) == 0 {
					continue
				}
				// Write as JSON map {value: true} (consistent with IndexEANPageBatch)
				catKey := turboKeyAttrValuesCat + code + ":" + fmt.Sprintf("%d", catID)
				valuesMap := make(map[string]bool, len(catValSet))
				for val := range catValSet {
					valuesMap[val] = true
				}
				buf, err := json.Marshal(valuesMap)
				if err != nil {
					fmt.Printf("WARN: marshal attr_values_cat %s: %v\n", catKey, err)
					continue
				}
				if err := r.store.TurboWrite(catKey, buf); err != nil {
					fmt.Printf("WARN: write attr_values_cat %s: %v\n", catKey, err)
				}
			}
		}
	}

	return nil
}

func Uint64SliceFromInt64(in []int64) []uint64 {
	out := make([]uint64, len(in))
	for i, v := range in {
		out[i] = uint64(v)
	}
	return out
}

// ---------- List ----------

func (r *AttrDefRepo) List() ([]model.AttrDef, error) {
	data, err := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	var codes []string
	if err := json.Unmarshal(data, &codes); err != nil {
		return nil, err
	}

	var result []model.AttrDef
	for _, code := range codes {
		ad, err := r.GetByCode(code)
		if err != nil {
			continue
		}
		result = append(result, *ad)
	}
	return result, nil
}

// RebuildCatCodesIndex rebuilds the cat->codes index from existing data.
func (r *AttrDefRepo) RebuildCatCodesIndex() error {
	codesData, err := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	if err != nil || len(codesData) == 0 {
		return nil
	}

	var codes []string
	if err := json.Unmarshal(codesData, &codes); err != nil {
		return err
	}

	for _, code := range codes {
		cats, err := r.GetCategories(code)
		if err != nil {
			continue
		}
		for _, catID := range cats {
			key := turboKeyAttrDefCatCodes + fmt.Sprintf("%d", catID)
			existingData, _ := r.store.DB().TurboRawRead(key)
			var existingCodes []string
			if existingData != nil && len(existingData) > 0 {
				json.Unmarshal(existingData, &existingCodes)
			}
			found := false
			for _, c := range existingCodes {
				if c == code {
					found = true
					break
				}
			}
			if !found {
				existingCodes = append(existingCodes, code)
				buf, _ := json.Marshal(existingCodes)
				r.store.TurboWrite(key, buf)
			}
		}
	}

	fmt.Printf("[ATTRDEF] Rebuilt cat_codes index: %d codes\n", len(codes))
	return nil
}

// GetOrCreate returns the AttrDef by code, or creates a default one if not found.
func (r *AttrDefRepo) GetOrCreate(code string) (*model.AttrDef, error) {
	ad, err := r.GetByCode(code)
	if err == nil {
		return ad, nil
	}

	// Create default
	ad = &model.AttrDef{
		Code:         code,
		NameRu:       toHumanAttrName(code),
		Type:         model.AttrTypeString,
		IsActive:     true,
		IsFilterable: true,
		CreatedAt:    time.Now().Unix(),
	}

	if err := r.Create(code, ad); err != nil {
		return nil, err
	}

	return ad, nil
}

// AddCodeToCategory adds an attribute code to a category.
func (r *AttrDefRepo) AddCodeToCategory(code string, catID int64) error {
	// Get existing categories for this code
	cats, err := r.GetCategories(code)
	if err != nil {
		cats = nil
	}

	// Check if already present
	for _, c := range cats {
		if c == catID {
			return nil
		}
	}

	// Add
	cats = append(cats, catID)

	// Update attrdef
	ad, err := r.GetByCode(code)
	if err != nil {
		// Create default
		ad = &model.AttrDef{
			Code:         code,
			NameRu:       toHumanAttrName(code),
			Type:         model.AttrTypeString,
			IsActive:     true,
			IsFilterable: true,
			Categories:   cats,
			CreatedAt:    time.Now().Unix(),
		}
		return r.Create(code, ad)
	}

	return r.Update(code, func(a *model.AttrDef) {
		a.Categories = cats
	})
}

// RebuildAttrValuesFromEANPages rebuilds attr_values_cat and attr_label indexes
// from all EAN pages in the database.
func (r *AttrDefRepo) RebuildAttrValuesFromEANPages(eanPageRepo *EANPageRepo) error {
	fmt.Println("[ATTRDEF] RebuildAttrValuesFromEANPages: starting...")
	startTime := time.Now()

	// Get all EAN pages
	pages, err := eanPageRepo.List()
	if err != nil {
		return fmt.Errorf("list eanpages: %w", err)
	}

	// Accumulate attr values per code and category
	// code -> catID -> {value -> true}
	attrValues := make(map[string]map[int64]map[string]bool)

	for _, sp := range pages {
		if sp.CategoryID == 0 {
			continue
		}
		for _, kv := range sp.Attributes {
			valStr := kv.Value
			if valStr == "" {
				continue
			}
			// Skip attribute values longer than 40 runes
			if model.IsAttrValueTooLong(valStr) {
				continue
			}
			code := kv.Key
			if attrValues[code] == nil {
				attrValues[code] = make(map[int64]map[string]bool)
			}
			if attrValues[code][sp.CategoryID] == nil {
				attrValues[code][sp.CategoryID] = make(map[string]bool)
			}
			attrValues[code][sp.CategoryID][valStr] = true
		}
	}

	// Write attr_values_cat and attr_label indexes.
	// Shared keys (attrdef_cat_codes:{catID}, attrdef_list) are accumulated in
	// memory and written ONCE each, to avoid O(n) rewrites of the same key
	// (which bloats the append-only DB). Per-(code,value) and per-(code,cat)
	// keys are unique, so one write each is fine.
	catCodes := make(map[int64]map[string]bool) // catID -> set of codes
	listCodes := make(map[string]bool)          // codes for attrdef_list

	for code, catMap := range attrValues {
		listCodes[code] = true

		// Collect all unique values for this code
		allValues := make(map[string]struct{})
		for _, valMap := range catMap {
			for val := range valMap {
				allValues[val] = struct{}{}
			}
		}

		// Write labels (per (code, value) — unique keys, one write each)
		for val := range allValues {
			labelKey := "attr_label:" + code + ":" + val
			if err := r.store.TurboWrite(labelKey, []byte(val)); err != nil {
				fmt.Printf("WARN: write attr_label %s: %v\n", labelKey, err)
			}
		}

		// Write attr_values_cat per (code, category) — unique keys, one write each
		for catID, valMap := range catMap {
			key := turboKeyAttrValuesCat + code + ":" + fmt.Sprintf("%d", catID)
			buf, err := json.Marshal(valMap)
			if err != nil {
				fmt.Printf("WARN: marshal attr_values_cat %s: %v\n", key, err)
				continue
			}
			if err := r.store.TurboWrite(key, buf); err != nil {
				fmt.Printf("WARN: write attr_values_cat %s: %v\n", key, err)
			}
			// Track cat -> code for a single batched write below.
			if catCodes[catID] == nil {
				catCodes[catID] = make(map[string]bool)
			}
			catCodes[catID][code] = true
		}
	}

	// Write attrdef_cat_codes per category ONCE (merged with existing).
	for catID, codeSet := range catCodes {
		if err := r.mergeCatCodes(catID, codeSet); err != nil {
			fmt.Printf("WARN: merge cat_codes %d: %v\n", catID, err)
		}
	}

	// Ensure all codes are in attrdef_list in ONE batched write.
	if len(listCodes) > 0 {
		if err := r.unionAttrDefList(listCodes); err != nil {
			fmt.Printf("WARN: union attrdef_list: %v\n", err)
		}
	}

	fmt.Printf("[ATTRDEF] RebuildAttrValuesFromEANPages: done in %v (%d pages, %d codes)\n",
		time.Since(startTime), len(pages), len(attrValues))
	return nil
}

// toHumanAttrName converts a code like "diagonal-ekrana" to "Diagonal Ekrana".
func toHumanAttrName(code string) string {
	parts := []string{"-", "_"}
	result := code
	for _, sep := range parts {
		result = strings.ReplaceAll(result, sep, " ")
	}
	// Capitalize first letter of each word
	words := strings.Fields(result)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
