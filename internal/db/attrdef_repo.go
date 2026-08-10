package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/GenshIv/makodb/v2"
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
		Name:         "",
		Categories:   cats,
		Type:         "string",
		IsActive:     true,
		IsFilterable: true,
		IsSortable:   false,
		SortOrder:    0,
		CreatedAt:    time.Now(),
	}

	buf, _ := json.Marshal(ad)
	_ = r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), buf)

	return ad, nil
}

func (r *AttrDefRepo) Get(id int64) (*model.AttrDef, error) {
	data, err := r.store.DocGet(fmt.Sprintf("attrdef:%d", id))
	if err != nil {
		return nil, err
	}
	var ad model.AttrDef
	if err := json.Unmarshal(data, &ad); err != nil {
		return nil, err
	}
	return &ad, nil
}

// ---------- Index-based lookups (O(1)) ----------

// GetCategories returns all category IDs where this attribute code is used.
func (r *AttrDefRepo) GetCategories(code string) ([]int64, error) {
	key := turboKeyAttrDefCats + code
	data, err := r.store.DB().TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, nil
	}
	ids := makodb.TurboUnsafeReadTokens(data)
	cats := make([]int64, len(ids))
	for i, id := range ids {
		cats[i] = int64(id)
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
		return []string{}, nil
	}

	// 3. Intersect codes of all direct children (O(1) each)
	firstCodes, err := r.GetCodesForCategory(children[0])
	if err != nil {
		return nil, err
	}
	firstSet := make(map[string]struct{}, len(firstCodes))
	for _, c := range firstCodes {
		firstSet[c] = struct{}{}
	}

	for _, cid := range children[1:] {
		childCodes, err := r.GetCodesForCategory(cid)
		if err != nil {
			continue
		}
		childSet := make(map[string]struct{}, len(childCodes))
		for _, c := range childCodes {
			childSet[c] = struct{}{}
		}

		newSet := make(map[string]struct{})
		for c := range firstSet {
			if _, ok := childSet[c]; ok {
				newSet[c] = struct{}{}
			}
		}
		firstSet = newSet
	}

	result := make([]string, 0, len(firstSet))
	for c := range firstSet {
		result = append(result, c)
	}

	return result, nil
}

// GetAttrValuesForCategory returns all values for an attribute code in a specific category.
func (r *AttrDefRepo) GetAttrValuesForCategory(code string, catID int64) ([]string, error) {
	if catID != 0 {
		key := turboKeyAttrValuesCat + code + ":" + fmt.Sprintf("%d", catID)
		data, err := r.store.DB().TurboRawRead(key)
		if err != nil || len(data) == 0 {
			return nil, nil
		}

		hashes := makodb.TurboUnsafeReadTokens(data)
		return r.hashesToStrings(code, hashes)
	}

	// catID == 0: merge all categories
	var allHashes []uint64
	seen := make(map[uint64]struct{})

	catsKey := turboKeyAttrDefCats + code
	catsData, err := r.store.DB().TurboRawRead(catsKey)
	if err != nil || len(catsData) == 0 {
		return nil, nil
	}

	catIDs := makodb.TurboUnsafeReadTokens(catsData)
	for _, cid := range catIDs {
		key := turboKeyAttrValuesCat + code + ":" + fmt.Sprintf("%d", cid)
		data, err := r.store.DB().TurboRawRead(key)
		if err != nil || len(data) == 0 {
			continue
		}
		hashes := makodb.TurboUnsafeReadTokens(data)
		for _, h := range hashes {
			if _, ok := seen[h]; !ok {
				seen[h] = struct{}{}
				allHashes = append(allHashes, h)
			}
		}
	}

	return r.hashesToStrings(code, allHashes)
}

func (r *AttrDefRepo) hashesToStrings(code string, hashes []uint64) ([]string, error) {
	values := make([]string, 0, len(hashes))
	for _, h := range hashes {
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
	ad.CreatedAt = time.Now()
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
		catBuf := makodb.TurboBinaryNew(Uint64SliceFromInt64(ad.Categories))
		if err := r.store.TurboWrite(turboKeyAttrDefCats+code, catBuf); err != nil {
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
		catBuf := makodb.TurboBinaryNew(Uint64SliceFromInt64(ad.Categories))
		r.store.TurboWrite(turboKeyAttrDefCats+code, catBuf)
	} else {
		r.store.TurboWrite(turboKeyAttrDefCats+code, []byte{})
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
		buf := makodb.TurboBinaryNew(Uint64SliceFromInt64(cats))
		if err := r.store.TurboWrite(turboKeyAttrDefCats+code, buf); err != nil {
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
		CreatedAt:  time.Now(),
	}

	data, _ := json.Marshal(ad)
	if err := r.store.DocPut(fmt.Sprintf("attrdef:%d", id), data); err != nil {
		return err
	}

	if err := r.store.TurboWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", id))); err != nil {
		return err
	}

	catBuf := makodb.TurboBinaryNew([]uint64{uint64(catID)})
	if err := r.store.TurboWrite(turboKeyAttrDefCats+code, catBuf); err != nil {
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

	// Clear old cat->codes indexes
	r.store.TurboWrite(turboKeyAttrDefCatCodes+"_", []byte{}) // marker

	for code, catSet := range codeCats {
		cats := make([]int64, 0, len(catSet))
		for c := range catSet {
			cats = append(cats, c)
		}

		catBuf := makodb.TurboBinaryNew(Uint64SliceFromInt64(cats))
		if err := r.store.TurboWrite(turboKeyAttrDefCats+code, catBuf); err != nil {
			fmt.Printf("WARN: write attrdef_cats %s: %v\n", code, err)
		}

		h := uint64(0)
		for i := 0; i < len(code); i++ {
			h ^= uint64(code[i])
			h *= 1099511628211
		}
		if err := r.store.TurboWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", h))); err != nil {
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
func (r *AttrDefRepo) BatchWriteAttrValues(codeValues map[string]map[string]struct{}, codeCatValues map[string]map[int64]map[string]struct{}) error {
	if len(codeValues) == 0 {
		return nil
	}

	for code, valSet := range codeValues {
		if len(valSet) == 0 {
			continue
		}

		var hashes []uint64
		for val := range valSet {
			h := fnv64(val)
			hashes = append(hashes, h)

			hexH := fmt.Sprintf("%x", h)
			labelKey := "attr_label:" + code + ":" + hexH
			if err := r.store.TurboWrite(labelKey, []byte(val)); err != nil {
				fmt.Printf("WARN: write attr_label %s: %v\n", labelKey, err)
			}
		}

		if catVals, ok := codeCatValues[code]; ok {
			for catID, catValSet := range catVals {
				if len(catValSet) == 0 {
					continue
				}
				var catHashes []uint64
				for val := range catValSet {
					catHashes = append(catHashes, fnv64(val))
				}
				catBuf := makodb.TurboBinaryNew(catHashes)
				catKey := turboKeyAttrValuesCat + code + ":" + fmt.Sprintf("%d", catID)
				if err := r.store.TurboWrite(catKey, catBuf); err != nil {
					fmt.Printf("WARN: write attr_values_cat %s: %v\n", catKey, err)
				}
			}
		}
	}

	return nil
}

func fnv64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
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
