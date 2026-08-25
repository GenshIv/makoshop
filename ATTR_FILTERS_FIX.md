# Fix for Attribute Filters in Catalog

## Problem

Attribute filters in the catalog were not working due to inconsistencies in how attribute values were stored and retrieved:

1. **`scupage_attr:{code}:{value}`** — uses raw values (correct for filtering)
2. **`attr_values_cat:{code}:{catID}`** — was written inconsistently:
   - `IndexSCUPageBatch` wrote it as JSON map `{value: true}` (raw values)
   - `BatchWriteAttrValues` wrote it as turbo index with hex hashes (wrong)
3. **`attr_label:{code}:{value}`** — was written inconsistently:
   - `IndexSCUPageBatch` wrote it with raw values (correct)
   - `BatchWriteAttrValues` wrote it with hex hashes (wrong)
4. **`GetAttrValuesForCategory`** — tried to read `attr_values_cat` as turbo index tokens and returned nil
5. **`BatchUpsertCodes`** — wrote `attrdef_code:{code}` as a hash of the string, not a real numeric ID

## Solution

### 1. Fixed `GetAttrValuesForCategory` (attrdef_repo.go)

**Before:**
- Tried to read `attr_values_cat:{code}:{catID}` as turbo index tokens
- Returned nil (commented out the actual implementation)

**After:**
- Reads `attr_values_cat:{code}:{catID}` as raw JSON using `TurboRawRead`
- Parses as JSON map `{value: true}`
- Returns sorted list of values

### 2. Fixed `BatchWriteAttrValues` (attrdef_repo.go)

**Before:**
- Wrote `attr_values_cat` as turbo index with hash keys
- Wrote `attr_label` with hex hashes

**After:**
- Writes `attr_values_cat:{code}:{catID}` as JSON map `{value: true}` (consistent with `IndexSCUPageBatch`)
- Writes `attr_label:{code}:{value}` with raw values (consistent with `IndexSCUPageBatch`)

### 3. Fixed `BatchUpsertCodes` (attrdef_repo.go)

**Before:**
- Wrote `attrdef_code:{code}` as a hash of the string (not a real ID)

**After:**
- Gets or creates a real numeric ID using `store.NextID("attrdef")`
- Creates default AttrDef document if it doesn't exist
- Writes the real numeric ID to `attrdef_code:{code}`

### 4. Added `RebuildAttrValuesFromSCUPages` (attrdef_repo.go)

New method that rebuilds `attr_values_cat` and `attr_label` indexes from all SCU pages:
- Reads all SCU pages from the database
- Accumulates attribute values per code and category
- Writes `attr_values_cat:{code}:{catID}` as JSON maps
- Writes `attr_label:{code}:{value}` with raw values
- Updates `attrdef_cat_codes` indexes

### 5. Updated `HandleAdminRebuildAttrDefIndexes` (landing_handlers.go)

**Before:**
- Only rebuilt `attrdef_cat_codes` indexes

**After:**
- Rebuilds `attrdef_cat_codes` indexes
- Rebuilds `attr_values_cat` and `attr_label` from SCU pages
- Invalidates the category attributes cache

## How It Works Now

### Indexing (when SCU pages are indexed)

`IndexSCUPageBatch` writes:
- `scupage_attr:{code}:{value}` — turbo index for filtering (raw values)
- `attr_values_cat:{code}:{catID}` — JSON map `{value: true}` for UI (raw values)
- `attr_label:{code}:{value}` — raw value for display (raw values)
- `attrdef_cat_codes:{catID}` — JSON array of codes for this category

### Retrieval (when getting category attributes)

`GetCategoryAttrs` (handlers.go):
1. Gets codes via `GetCodesForCategoryTree`
2. Gets values via `GetAttrValuesForCategory` (now reads JSON map correctly)
3. Returns `AttrItem` with `Options` populated

### Filtering (when applying attribute filters)

`handleSCUPageCatalog` (landing_handlers.go):
1. Parses `attr.{code}=value1,value2` from query
2. Passes to `SCUPageSearch.ListWithTurbo`
3. `ListWithTurbo` builds candidates using `scupage_attr:{code}:{value}` indexes
4. Intersects with sort index and returns results

## Rebuilding Indexes

To rebuild attribute indexes from existing data:

```bash
# Call the admin endpoint
curl -X POST http://localhost:8080/admin/rebuild-attrdef-indexes
```

This will:
1. Rebuild `attrdef_cat_codes` indexes
2. Rebuild `attr_values_cat` and `attr_label` from all SCU pages
3. Invalidate the cache

## Files Changed

1. `internal/db/attrdef_repo.go`
   - Fixed `GetAttrValuesForCategory`
   - Fixed `BatchWriteAttrValues`
   - Fixed `BatchUpsertCodes`
   - Added `RebuildAttrValuesFromSCUPages`
   - Removed unused `fnv64` function

2. `internal/api/landing_handlers.go`
   - Updated `HandleAdminRebuildAttrDefIndexes` to rebuild attr values

## Verification

After deploying, you can verify the fix by:

1. Calling the rebuild endpoint:
   ```bash
   curl -X POST http://localhost:8080/admin/rebuild-attrdef-indexes
   ```

2. Checking the catalog page for a category:
   - Attribute filters should now be visible in the UI
   - Clicking on filter options should apply the filter correctly
   - The `category_attrs` in the API response should have populated `options`

3. Testing with a specific filter:
   ```bash
   curl "http://localhost:8080/shop/{category}?attr.color=red,blue"
   ```
   - Should return only products with red or blue color
