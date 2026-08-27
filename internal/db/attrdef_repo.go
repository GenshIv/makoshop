package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
}

func NewAttrDefRepo(store *Store) *AttrDefRepo {
	return &AttrDefRepo{store: store}
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

	return ad, nil
}

// GetOrCreateByKey finds an AttrDef by raw key (e.g. "Moc", "Power") or creates one.
// Used by HTMLAttrKeyParser to map raw keys from HTML to attribute codes.
func (r *AttrDefRepo) GetOrCreateByKey(rawKey string) (*model.AttrDef, error) {
	if rawKey == "" {
		return nil, ErrKeyNotFound
	}

	// Normalize key
	key := strings.TrimSpace(rawKey)
	keyLower := strings.ToLower(key)

	// Check if key is already mapped to a code
	keyIndex := turboKeyAttrDefKey + keyLower
	codeData, err := r.store.DB().TurboRawRead(keyIndex)
	if err == nil && len(codeData) > 0 {
		code := string(codeData)
		return r.GetByCode(code)
	}

	// Generate code from key
	code := r.generateCodeFromKey(key)

	// Check if code already exists
	existing, err := r.GetByCode(code)
	if err == nil {
		// Code exists, add key to it
		existing.Keys = append(existing.Keys, key)
		buf, _ := json.Marshal(existing)
		r.store.DocPut(fmt.Sprintf("attrdef:%d", existing.ID), buf)
		// Update key index
		r.store.DB().TurboRawWrite(keyIndex, []byte(code))
		return existing, nil
	}

	// Create new AttrDef with temp ID (will be assigned by DocPut)
	ad := &model.AttrDef{
		ID:           time.Now().UnixNano(), // temp unique ID
		Code:         code,
		NameRu:       key,
		Categories:   []int64{},
		Type:         "string",
		IsActive:     true,
		IsFilterable: true,
		IsSortable:   false,
		SortOrder:    0,
		Keys:         []string{key},
		CreatedAt:    time.Now().Unix(),
	}

	// Save
	docKey := fmt.Sprintf("attrdef:%d", ad.ID)
	buf, _ := json.Marshal(ad)
	if err := r.store.DocPut(docKey, buf); err != nil {
		return nil, err
	}

	// Update indexes
	r.store.DB().TurboRawWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", ad.ID)))
	r.store.DB().TurboRawWrite(keyIndex, []byte(code))

	// Add to list
	listData, _ := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	var codes []string
	if len(listData) > 0 {
		json.Unmarshal(listData, &codes)
	}
	codes = append(codes, code)
	listBytes, _ := json.Marshal(codes)
	r.store.DB().TurboRawWrite(turboKeyAttrDefList, listBytes)

	return ad, nil
}

// generateCodeFromKey creates a normalized code from a raw key.
func (r *AttrDefRepo) generateCodeFromKey(key string) string {
	code := strings.ToLower(key)
	code = strings.ReplaceAll(code, " ", "_")
	code = strings.ReplaceAll(code, "-", "_")
	// Remove non-alphanumeric chars (except underscore)
	var result []rune
	for _, ch := range code {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			result = append(result, ch)
		}
	}
	return string(result)
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
		return codes, nil
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

	return firstCodes, nil
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
	key := turboKeyAttrDefCatCodes + fmt.Sprintf("%d", catID)
	data, _ := r.store.DB().TurboRawRead(key)
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

	// Write attr_values_cat and attr_label indexes
	for code, catMap := range attrValues {
		// Collect all unique values for this code
		allValues := make(map[string]struct{})
		for _, valMap := range catMap {
			for val := range valMap {
				allValues[val] = struct{}{}
			}
		}

		// Write labels
		for val := range allValues {
			labelKey := "attr_label:" + code + ":" + val
			if err := r.store.TurboWrite(labelKey, []byte(val)); err != nil {
				fmt.Printf("WARN: write attr_label %s: %v\n", labelKey, err)
			}
		}

		// Write attr_values_cat per category
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
		}

		// Update attrdef_cat_codes for each category
		for catID := range catMap {
			r.addToCatCodes(catID, code)
		}

		// Ensure code is in attrdef_list
		_ = r.addToAttrDefList(code)
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
