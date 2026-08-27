# Attribute Filters - Quick Start Guide

## Problem Solved

Attribute filters in the catalog were not working due to inconsistent data formats between indexing and retrieval.

## Solution Implemented

### Core Fix

Ensured consistent use of **raw values** across all attribute-related indexes:

| Index | Format | Purpose |
|-------|--------|---------|
| `scupage_attr:{code}:{value}` | Turbo index, raw values | Filtering |
| `attr_values_cat:{code}:{catID}` | JSON map `{value: true}` | UI options |
| `attr_label:{code}:{value}` | Raw value | Display |
| `attrdef_code:{code}` | Numeric ID | Attribute lookup |

## Quick Deployment

### Step 1: Build and Deploy

```bash
cd /home/ihar/IdeaProjects/makoshop
go build ./cmd/server
# Deploy the binary
```

### Step 2: Rebuild Indexes

```bash
curl -X POST http://localhost:8080/admin/rebuild-attrdef-indexes
```

This will:
- Rebuild `attrdef_cat_codes` indexes
- Rebuild `attr_values_cat` and `attr_label` from all EAN pages
- Invalidate the cache

### Step 3: Verify

Open the catalog page in the browser:
- Attribute filters should be visible
- Clicking on filter options should work
- Products should be filtered correctly

## API Endpoints

### Get Category Attributes
```bash
GET /shop/{category_id}
```

Response includes:
```json
{
  "items": [...],
  "category_attrs": [
    {
      "code": "color",
      "options": ["red", "blue", "green"],
      "name_ru": "Цвет",
      "is_filterable": true
    }
  ]
}
```

### Apply Attribute Filter
```bash
GET /shop/{category_id}?attr.color=red,blue
```

### Rebuild Indexes
```bash
POST /admin/rebuild-attrdef-indexes
```

## Key Changes

### 1. `GetAttrValuesForCategory`
- **Before:** Returned nil (broken)
- **After:** Reads JSON map and returns sorted values

### 2. `BatchWriteAttrValues`
- **Before:** Used hex hashes (inconsistent)
- **After:** Uses raw values (consistent)

### 3. `BatchUpsertCodes`
- **Before:** Used string hashes for IDs (broken)
- **After:** Uses real numeric IDs (correct)

### 4. `RebuildAttrValuesFromSCUPages` - NEW
- Rebuilds all attribute value indexes from EAN pages
- Called by the admin rebuild endpoint

## Troubleshooting

### Filters not showing?
1. Check if category has attributes with values
2. Call rebuild endpoint
3. Clear browser cache
4. Check API response for `category_attrs`

### Filters not working?
1. Check if `scupage_attr` indexes exist
2. Verify attribute codes match in query and index
3. Check server logs for errors

### Performance issues?
1. Attribute indexes are built during `IndexSCUPageBatch`
2. Rebuilding is O(n) where n = number of EAN pages
3. Consider incremental updates for large datasets

## Documentation

- [Detailed Fix Documentation](ATTR_FILTERS_FIX.md)
- [Changes Summary](CHANGES_SUMMARY.md)
- [Test Script](test_attr_filters.sh)

## Support

For issues or questions, check:
1. Server logs
2. API responses
3. Database indexes
