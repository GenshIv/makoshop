# Price Import & Attribute Indexes Verification

## Question

Will attribute indexes be correctly recalculated when prices are imported?

## Answer

**YES** - Attribute indexes are correctly recalculated during price import.

## Verification

### Import Methods

All three price import methods call `IndexSCUPageBatch`:

1. **`importCSV`** (line 227)
   - Phase 3: Batch upsert EAN pages + index
   - Calls `h.scuPageSearch.IndexSCUPageBatch(scuPtrs)`

2. **`importNormalized`** (line 456)
   - Phase 3: Batch index products + EAN pages
   - Calls `h.scuPageSearch.IndexSCUPageBatch(scuPtrs)`

3. **`importMulti`** (line 2068)
   - Phase 3: Batch upsert EAN pages + index
   - Calls `h.scuPageSearch.IndexSCUPageBatch(scuPtrs)`

### IndexSCUPageBatch Function

The `IndexSCUPageBatch` function (eanpage_search.go:182) correctly writes all attribute indexes:

#### 1. Filtering Index (line 221)
```go
indexes[eanpageKeyAttr(kv.Key, valStr)] = append(indexes[eanpageKeyAttr(kv.Key, valStr)], docID)
```
- **Key format:** `eanpage_attr:{code}:{value}`
- **Purpose:** Turbo index for filtering
- **Values:** Raw attribute values

#### 2. UI Options Index (lines 255-263)
```go
key := "attr_values_cat:" + code + ":" + strconv.FormatInt(catID, 10)
valuesSet := make(map[string]struct{})
for val := range values {
    valuesSet[val] = struct{}{}
}
buf, _ := json.Marshal(valuesSet)
_ = s.db.TurboRawWrite(key, buf)
```
- **Key format:** `attr_values_cat:{code}:{catID}`
- **Purpose:** JSON map for UI options
- **Values:** Raw attribute values as JSON map `{value: true}`

#### 3. Label Index (lines 266-268)
```go
labelKey := "attr_label:" + code + ":" + val
_ = s.db.TurboRawWrite(labelKey, []byte(val))
```
- **Key format:** `attr_label:{code}:{value}`
- **Purpose:** Display labels
- **Values:** Raw attribute values

#### 4. Category Codes Index (lines 273+)
```go
key := "attrdef_cat_codes:" + strconv.FormatInt(catID, 10)
// ... updates with attribute codes
```
- **Key format:** `attrdef_cat_codes:{catID}`
- **Purpose:** Which attribute codes exist for this category
- **Values:** JSON array of attribute codes

### Data Flow During Import

```
Price Import
├── Phase 1: Parse products
├── Phase 2: Upsert EAN pages from products
├── Phase 3: IndexSCUPageBatch
│   ├── Writes eanpage_attr:{code}:{value} (filtering)
│   ├── Writes attr_values_cat:{code}:{catID} (UI options)
│   ├── Writes attr_label:{code}:{value} (display)
│   └── Updates attrdef_cat_codes:{catID} (category codes)
└── Phase 4+: Build sort indexes, recalculate counts/prices
```

### Retrieval Flow

```
Get Category Attributes
├── GetCodesForCategoryTree
│   └── Reads attrdef_cat_codes:{catID}
├── GetAttrValuesForCategory (FIXED)
│   └── Reads attr_values_cat:{code}:{catID} as JSON map
└── Returns AttrItem[] with Options populated
```

## Conclusion

✅ **Attribute indexes are correctly recalculated during price import**

The fix ensures:
1. Consistent raw values across all attribute indexes
2. Proper JSON map format for `attr_values_cat`
3. Correct reading in `GetAttrValuesForCategory`
4. All import paths use the same indexing logic

No additional changes needed for price imports - they already work correctly with the fixed attribute index system.
