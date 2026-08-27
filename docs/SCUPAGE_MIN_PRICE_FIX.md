# Fix: SCUPage MinPrice Inconsistency

## Problem

The `MinPrice` field on SCUPage was not matching the actual minimum price among its products.

### Root Cause

1. **During import**, `MinPrice` was calculated incrementally:
   - When creating a new SCUPage: `MinPrice = first_product.Price`
   - When updating: `MinPrice = min(existing_MInPrice, new_product.Price)`

2. **Problem scenarios**:
   - **Sequential imports**: Company A imports with price 100, then Company B imports with price 50 → MinPrice = 50 ✓
   - **Price increases**: If all products increase in price, MinPrice stays at the old low value ✗
   - **Product deletion**: If the cheapest product is deleted, MinPrice doesn't update ✗
   - **Multi-company with idOffset**: Products from different companies with same EAN may have different price ranges

## Solution

Added a new method `RecalculateMinPrices()` that:
1. Reads all EAN pages
2. For each EAN page, gets all linked products via turbo index `ean:{ean}`
3. Calculates the actual minimum price from all products
4. Updates the EAN page if the price changed
5. **Rebuilds sort indexes** to ensure price filters work correctly

### Files Modified

1. **`internal/db/scupage_repo.go`**:
   - Added `RecalculateMinPrices(productRepo *ProductRepo) error` method

2. **`internal/api/import_prices.go`**:
   - CSV import: Added Phase 6 (recalculate min prices) and Phase 7 (rebuild sort indexes)
   - Normalized import: Added Phase 5 (recalculate min prices) and Phase 6 (rebuild sort indexes)
   - Multi-company import: Added Phase 5 (recalculate min prices) and Phase 6 (rebuild sort indexes)

3. **`internal/api/landing_handlers.go`**:
   - `HandleAdminRebuildSCUPages`: Added Phase 7 (recalculate min prices) and Phase 8 (rebuild sort indexes)

4. **`internal/api/handlers.go`**:
   - Added `HandleAdminSCUPageRecalculateMinPrices` handler (recalculates min prices AND rebuilds sort indexes)

5. **`cmd/server/main.go`**:
   - Registered route: `POST /admin/scupages/recalculate-min-prices`

## Usage

### Automatic (during import)
Min prices are automatically recalculated after every import operation, and sort indexes are rebuilt to ensure price filters work correctly.

### Manual (admin)
```bash
# Recalculate min prices for all EAN pages and rebuild sort indexes
curl -X POST http://localhost:8080/admin/scupages/recalculate-min-prices \
  -H "Authorization: Bearer <token>"
```

Or use the "Recalculate Min Prices" button in the admin panel (EAN Pages section).

## Performance

- **Time complexity**: O(N * M) where N = number of EAN pages, M = average products per EAN
- **Database reads**: One read per product to get its price
- **Database writes**: Only for EAN pages where MinPrice actually changed
- **Sort index rebuild**: O(N log N) where N = number of EAN pages

For large datasets (100k+ products), this may take a few seconds but ensures data consistency and correct price filtering.
