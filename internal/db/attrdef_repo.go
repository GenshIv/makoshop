package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyAttrDefList   = "attrdef_list"
	turboKeyAttrDefCode   = "attrdef_code:"    // prefix + code -> docID
	turboKeyAttrDefCats   = "attrdef_cats:"    // prefix + code -> [categoryIDs]
	turboKeyAttrValuesCat = "attr_values_cat:" // prefix + code + ":" + catID -> [hashes]
)

type AttrDefRepo struct {
	store *Store
}

func NewAttrDefRepo(store *Store) *AttrDefRepo {
	return &AttrDefRepo{store: store}
}

// GetByCode returns attrdef by code. If document doesn't exist (e.g. created by BatchUpsertCodes),
// it creates a default AttrDef on the fly.
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

	// Document doesn't exist (e.g. created by BatchUpsertCodes with hash as pseudo-ID).
	// Create a default AttrDef.
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

	// Persist the document so future calls work
	buf, _ := json.Marshal(ad)
	_ = r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), buf)

	return ad, nil
}

// Get returns attrdef by ID.
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

// GetAttrValuesForCategory returns all values for an attribute code in a specific category.
// Uses attr_values_cat:<code>:<catID> index.
// If catID is 0, returns values from all categories (merged).
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

	// Read attrdef_cats:<code> to get all category IDs
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

// hashesToStrings converts hash tokens to value strings using attr_label index.
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

// GetCodesForCategory returns all attribute codes used in a category.
// Uses reverse lookup from attrdef_cats indexes.
func (r *AttrDefRepo) GetCodesForCategory(catID int64) ([]string, error) {
	// Read all attrdef codes
	data, err := r.store.DB().TurboRawRead(turboKeyAttrDefList)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	var codes []string
	if err := json.Unmarshal(data, &codes); err != nil {
		return nil, nil
	}

	var result []string
	for _, code := range codes {
		cats, err := r.GetCategories(code)
		if err != nil {
			continue
		}
		for _, c := range cats {
			if c == catID {
				result = append(result, code)
				break
			}
		}
	}

	return result, nil
}

// GetCodesForCategoryTree returns attribute codes for a category considering its subtree.
// Logic:
// - If category has direct attributes (products in this category), return them.
// - If category has no direct attributes but has children, return codes present in ALL direct children.
// - If no children, return codes for this category.
func (r *AttrDefRepo) GetCodesForCategoryTree(catID int64, categoryRepo *CategoryRepo) ([]string, error) {
	// Get codes for this category
	codes, err := r.GetCodesForCategory(catID)
	if err != nil {
		return nil, err
	}
	if len(codes) > 0 {
		return codes, nil
	}

	// No direct codes — check children
	children, err := r.getDirectChildren(catID, categoryRepo)
	if err != nil {
		return nil, err
	}
	if len(children) == 0 {
		return []string{}, nil
	}

	// Intersect codes of all direct children
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

		// Intersect
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

// getDirectChildren returns immediate children of catID.
func (r *AttrDefRepo) getDirectChildren(catID int64, categoryRepo *CategoryRepo) ([]int64, error) {
	all, err := categoryRepo.ListAll()
	if err != nil {
		return nil, err
	}

	var children []int64
	for _, c := range all {
		if c.ParentID != nil && *c.ParentID == catID {
			children = append(children, c.ID)
		}
	}
	return children, nil
}

// getSubtreeCategories returns all category IDs in the subtree rooted at catID.
func (r *AttrDefRepo) getSubtreeCategories(catID int64, categoryRepo *CategoryRepo) ([]int64, error) {
	var result []int64
	result = append(result, catID)

	// Get all categories and build children map
	all, err := categoryRepo.ListAll()
	if err != nil {
		return nil, err
	}

	children := make(map[int64][]int64)
	for _, c := range all {
		if c.ParentID != nil {
			children[*c.ParentID] = append(children[*c.ParentID], c.ID)
		}
	}

	// BFS
	queue := []int64{catID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, child := range children[current] {
			result = append(result, child)
			queue = append(queue, child)
		}
	}

	return result, nil
}

// UpsertCode adds a code to the attrdef system and links it to a category.
// If code already exists, just adds category if not present.
func (r *AttrDefRepo) UpsertCode(code string, catID int64) error {
	if code == "" || catID == 0 {
		return nil
	}

	// Check if code already exists
	existing, _ := r.GetByCode(code)

	if existing != nil {
		// Add category if not present
		cats, _ := r.GetCategories(code)
		for _, c := range cats {
			if c == catID {
				return nil
			}
		}
		// Append category
		cats = append(cats, catID)
		buf := makodb.TurboBinaryNew(Uint64SliceFromInt64(cats))
		return r.store.DB().TurboRawWrite(turboKeyAttrDefCats+code, buf)
	}

	// Create new attrdef
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

	// Save doc
	data, _ := json.Marshal(ad)
	if err := r.store.DocPut(fmt.Sprintf("attrdef:%d", id), data); err != nil {
		return err
	}

	// Index: code -> docID
	if err := r.store.DB().TurboRawWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", id))); err != nil {
		return err
	}

	// Index: code -> categories
	catBuf := makodb.TurboBinaryNew([]uint64{uint64(catID)})
	if err := r.store.DB().TurboRawWrite(turboKeyAttrDefCats+code, catBuf); err != nil {
		return err
	}

	// Add to attrdef_list
	if err := r.addToAttrDefList(code); err != nil {
		fmt.Printf("WARN: failed to add attrdef %s to list: %v\n", code, err)
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
	return r.store.DB().TurboRawWrite(turboKeyAttrDefList, buf)
}

func Uint64SliceFromInt64(in []int64) []uint64 {
	out := make([]uint64, len(in))
	for i, v := range in {
		out[i] = uint64(v)
	}
	return out
}

// BatchUpsertCodes batch-inserts all attr codes with their category links.
// Uses direct turbo writes (no per-code DB round-trips).
func (r *AttrDefRepo) BatchUpsertCodes(codeCats map[string]map[int64]struct{}) error {
	if len(codeCats) == 0 {
		return nil
	}

	var allCodes []string
	for code := range codeCats {
		allCodes = append(allCodes, code)
	}

	// Write attrdef_list
	listBuf, _ := json.Marshal(allCodes)
	if err := r.store.DB().TurboRawWrite(turboKeyAttrDefList, listBuf); err != nil {
		return fmt.Errorf("write attrdef_list: %w", err)
	}

	// Write each code -> categories and code -> docID
	for code, catSet := range codeCats {
		cats := make([]int64, 0, len(catSet))
		for c := range catSet {
			cats = append(cats, c)
		}

		// attrdef_cats:<code> -> [categoryIDs]
		catBuf := makodb.TurboBinaryNew(Uint64SliceFromInt64(cats))
		if err := r.store.DB().TurboRawWrite(turboKeyAttrDefCats+code, catBuf); err != nil {
			fmt.Printf("WARN: write attrdef_cats %s: %v\n", code, err)
		}

		// attrdef_code:<code> -> docID (use hash as pseudo-ID)
		h := uint64(0)
		for i := 0; i < len(code); i++ {
			h ^= uint64(code[i])
			h *= 1099511628211
		}
		if err := r.store.DB().TurboRawWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", h))); err != nil {
			fmt.Printf("WARN: write attrdef_code %s: %v\n", code, err)
		}
	}

	return nil
}

// BatchWriteAttrValues batch-writes attr value references.
// codeValues: global code -> values
// codeCatValues: code -> catID -> values (for per-category filtering)
func (r *AttrDefRepo) BatchWriteAttrValues(codeValues map[string]map[string]struct{}, codeCatValues map[string]map[int64]map[string]struct{}) error {
	if len(codeValues) == 0 {
		return nil
	}

	for code, valSet := range codeValues {
		if len(valSet) == 0 {
			continue
		}

		// Collect hashes and write labels
		var hashes []uint64
		for val := range valSet {
			h := fnv64(val)
			hashes = append(hashes, h)

			// Write label
			hexH := fmt.Sprintf("%x", h)
			labelKey := "attr_label:" + code + ":" + hexH
			if err := r.store.DB().TurboRawWrite(labelKey, []byte(val)); err != nil {
				fmt.Printf("WARN: write attr_label %s: %v\n", labelKey, err)
			}
		}

		// Write per-category attr_values_cat:<code>:<catID> -> [hashes]
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
				if err := r.store.DB().TurboRawWrite(catKey, catBuf); err != nil {
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

// List returns all AttrDef entries.
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

// Update updates an AttrDef by code.
func (r *AttrDefRepo) Update(code string, updater func(*model.AttrDef)) error {
	ad, err := r.GetByCode(code)
	if err != nil {
		return err
	}

	updater(ad)

	data, err := json.Marshal(ad)
	if err != nil {
		return err
	}

	return r.store.DocPut(fmt.Sprintf("attrdef:%d", ad.ID), data)
}

// Delete removes an AttrDef by code.
func (r *AttrDefRepo) Delete(code string) error {
	ad, err := r.GetByCode(code)
	if err != nil {
		return err
	}

	// Remove from doc store
	_ = r.store.DocDelete(fmt.Sprintf("attrdef:%d", ad.ID))

	// Remove indexes
	_ = r.store.DB().TurboRawWrite(turboKeyAttrDefCode+code, []byte{})
	_ = r.store.DB().TurboRawWrite(turboKeyAttrDefCats+code, []byte{})

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
		_ = r.store.DB().TurboRawWrite(turboKeyAttrDefList, buf)
	} else {
		_ = r.store.DB().TurboRawWrite(turboKeyAttrDefList, []byte{})
	}

	return nil
}

// Create creates a new AttrDef.
func (r *AttrDefRepo) Create(code string, ad *model.AttrDef) error {
	if code == "" {
		return fmt.Errorf("code is required")
	}

	// Check if exists
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
	if err := r.store.DB().TurboRawWrite(turboKeyAttrDefCode+code, []byte(fmt.Sprintf("%d", id))); err != nil {
		return err
	}

	// Index: code -> categories
	if len(ad.Categories) > 0 {
		catBuf := makodb.TurboBinaryNew(Uint64SliceFromInt64(ad.Categories))
		if err := r.store.DB().TurboRawWrite(turboKeyAttrDefCats+code, catBuf); err != nil {
			return err
		}
	}

	// Add to list
	return r.addToAttrDefList(code)
}
