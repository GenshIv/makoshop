# Bitmap Index for Product Filters & Facets

## Overview
Immutable mmap-backed bitmap index for high-performance product filtering and faceted search. Uses zero-allocation AND/OR/AND-NOT operations with fast cardinality (popcount) and position enumeration.

## Architecture

### Data Structures
- **Bitmap**: Immutable, mmap'd byte array representing set of product IDs
- **BitmapIndex**: Map of attribute values to bitmaps
- **FilterEngine**: Parses filter queries and executes bitmap operations

### File Layout
```
internal/index/
├── bitmap.go          # Core bitmap type + operations
├── bitmap_mmap.go     # mmap persistence (load/save)
├── bitmap_builder.go  # Index building from products
└── filter_engine.go   # Query parsing + execution
```

## Implementation Phases

### Phase 1: Core Bitmap Operations (`bitmap.go`)
- `NewBitmap(size)` - allocate and zero
- `Set(id)`, `Clear(id)`, `Get(id)` - bit manipulation
- `And(other, result)` - intersection (write to caller-provided buffer)
- `Or(other, result)` - union
- `AndNot(other, result)` - difference
- `Cardinality()` - popcount using CPU POPCNT instruction
- `Enumerate()` - iterate set bit positions

### Phase 2: mmap Persistence (`bitmap_mmap.go`)
- `WriteToDisk(path)` - serialize bitmap to file
- `LoadFromDisk(path)` - mmap file, return Bitmap struct
- File format: header (size, word count) + raw bitmap data
- Immutable after load

### Phase 3: Index Builder (`bitmap_builder.go`)
- Scan all products, build bitmaps per attribute value
- Parallel building across attributes
- `BuildIndex(products, outputPath)` - main entry point
- Atomic replace on rebuild

### Phase 4: Filter Engine (`filter_engine.go`)
- Parse filter query → bitmap operations
- Example: `brand=X AND price<100` → `And(brandBitmap[X], priceRangeBitmap[<100])`
- Return product IDs from resulting bitmap
- Cardinality for facet counts

## Key Optimizations
- **Zero-allocation**: Operations write to caller-provided buffers, no GC pressure
- **Fast cardinality**: CPU POPCNT instruction via `runtime.Popcount`
- **Position enumeration**: ffs (find first set) + bit clearing loop
- **Immutable snapshots**: Built once, loaded at startup, no vacuum interference

## Configuration
- Path configurable via `MAKOSHOP_BITMAP_INDEX_PATH` env variable
- Default: `./data/bitmaps` (relative to project root)

## Integration
- Build during import (Phase 1.5) or separate reindex command
- Load at server startup
- Use for `/api/products` filtering and facet counts
- Replace/supplement current turbo search attribute indexes
