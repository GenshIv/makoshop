# Bitmap Index Usage Guide

## Overview

The bitmap index provides high-performance product filtering and faceted search using immutable, memory-mapped bitmaps. Each attribute value has a dedicated bitmap where bit N is set if product N has that attribute value.

## Configuration

The bitmap index path is configurable via environment variable:

```bash
# Default: ./data/bitmaps (relative to project root)
export MAKOSHOP_BITMAP_INDEX_PATH=/custom/path/to/bitmaps
```

Or set it in your `.env` file:
```
MAKOSHOP_BITMAP_INDEX_PATH=./data/bitmaps
```

## Building the Index

Build the index from your product list during import or as a separate reindex step:

```go
import "github.com/GenshIv/makoshop/internal/index"

// Load products from database
products := loadAllProducts()

// Build in-memory index
builder := index.NewBitmapBuilder(products)
if err := builder.Build(); err != nil {
    log.Fatal(err)
}

// Save to disk (creates directory with manifest + bitmap files)
outputDir := "/var/makoshop/bitmaps"
if err := builder.Save(outputDir); err != nil {
    log.Fatal(err)
}
```

## Loading and Querying

Load the index at server startup and use it for filtering:

```go
// Load from disk (mmap's all bitmap files)
idx, manifest, err := index.LoadIndex("/var/makoshop/bitmaps")
if err != nil {
    log.Fatal(err)
}
defer idx.Close() // unmaps all files when done

engine := index.NewFilterEngine(idx)

// Filter products by attributes
ids, err := engine.Filter("brand=Apple&color!=red")
if err != nil {
    log.Fatal(err)
}
// ids contains bitmap indices of matching products

// Get count without enumerating
count, err := engine.FilterCount("brand=Samsung")

// Get facet counts (for filter UI)
colorCounts, err := engine.FacetCounts("color", "brand=Apple")
// Returns: {"black": 5, "silver": 3, "red": 2}
```

## Filter Query Syntax

| Syntax | Example | Description |
|--------|---------|-------------|
| Exact match | `brand=Apple` | Products with brand Apple |
| Negation | `color!=red` | Products without color red |
| Multiple (AND) | `brand=Apple&storage=128GB` | Both conditions must match |
| Empty query | `` | All products |

## Integration with /shop Category Listings

To integrate bitmap filtering into category listings:

1. **Build index on import**: Add bitmap building to your import pipeline after products are saved
2. **Load at startup**: Load the index when the server starts
3. **Use in handlers**: Replace or supplement turbo search attribute filtering with bitmap operations

Example integration in `handleEANPageCatalog`:

```go
// Parse attribute filters from query params (existing code)
attrFilters := parseAttrFilters(r.URL.Query())

// Convert to bitmap filter query
filterQuery := buildFilterQuery(attrFilters)

// Use bitmap index for fast filtering
if h.bitmapEngine != nil {
    matchingIDs, err := h.bitmapEngine.Filter(filterQuery)
    if err == nil {
        // Intersect with category results and paginate
        result = applyBitmapFilter(categoryResults, matchingIDs)
    }
}
```

## Performance Characteristics

- **Filter operations**: O(n/8) where n is number of products (byte-wise AND/OR)
- **Cardinality**: O(n/8) using hardware popcount instruction
- **Memory**: ~1 byte per 8 products per attribute value
- **Disk**: Same as memory footprint, mmap'd for zero-copy reads

For 100,000 products with 10 attribute values each:
- Memory: ~12.5 MB (100K bits = 12.5KB per bitmap × 10 values)
- Filter latency: < 1ms for typical queries

## File Structure

```
/var/makoshop/bitmaps/
├── manifest.json           # Index metadata
├── brand/
│   ├── Apple.bitmap        # Products with brand=Apple
│   ├── Samsung.bitmap      # Products with brand=Samsung
│   └── ...
├── color/
│   ├── red.bitmap
│   ├── blue.bitmap
│   └── ...
└── storage/
    ├── 128GB.bitmap
    └── 256GB.bitmap
```

## Rebuilding the Index

Rebuild the index whenever products change significantly:

```bash
# Stop server
# Rebuild index
./makoshop reindex --type=bitmap --output=/var/makoshop/bitmaps
# Start server
```

Or rebuild atomically to a temp directory and rename:

```go
tmpDir := "/var/makoshop/bitmaps.tmp"
builder.Save(tmpDir)
os.Rename(tmpDir, "/var/makoshop/bitmaps") // atomic on same filesystem
```
