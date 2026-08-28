# Transactional Price Import Implementation

## Overview

This document describes the implementation of transactional support for the Nokaut price import.

## What Was Implemented

### 1. Application-Level Transaction Manager

Created a new `Transaction` type in `internal/db/transaction.go` that:
- Buffers all writes in memory
- Applies them atomically on `Commit()`
- Discards them on `Abort()`

The transaction supports:
- Document writes (`DocPut`)
- Turbo index writes (`TurboWrite`)
- Turbo batch index writes (`TurboPutBatchIndexString`)
- Turbo sort index writes (`TurboPutSortIndexString`)

### 2. Transactional Repository Methods

Added transactional versions of key methods:

#### EANPageRepo
- `BatchUpsertFromProductsTx(txn, products)` - buffers EAN page upserts
- `CreateNoListIndexTx(txn, page)` - creates EAN page in transaction
- `RecalculateProductCountsTx(txn)` - recalculates counts in transaction
- `RecalculateMinPricesTx(txn, productRepo)` - recalculates prices in transaction

#### EANPageSearch
- `IndexEANPageBatchTx(txn, pages)` - indexes EAN pages in transaction

#### TurboProductSearch
- `BuildSortIndexesTx(txn)` - builds sort indexes in transaction

#### CategoryRepo
- `RebuildTreesTx(txn)` - rebuilds category trees in transaction

### 3. Updated Import Flow

The `importNokautCompany` function now:
1. Parses all XML files and creates/updates products
2. Creates a transaction
3. Performs all EAN page operations within the transaction
4. Commits the transaction on success
5. Aborts the transaction on error

## Current Limitations

### Product Creation Outside Transaction

The `GetOrCreateByEAN` method is called during the parsing phase, which is before the transaction starts. This means:
- Products are written to the database before the transaction begins
- If the transaction fails, the products are already in the database

### Future Improvements

To achieve full atomicity, the following changes are needed:
1. Parse all data into memory without writing to the database
2. Create the transaction
3. Apply all product and EAN page changes within the transaction
4. Commit or abort as needed

## Usage

The transactional import is used automatically when importing Nokaut prices:

```bash
# Import all companies
curl -X POST "http://localhost:8080/admin/import-nokaut"

# Import specific company
curl -X POST "http://localhost:8080/admin/import-nokaut?company=1"

# Limit offers per company
curl -X POST "http://localhost:8080/admin/import-nokaut?limit=1000"
```

## Testing

To test the transactional import:
1. Run the import with a valid price file
2. Check the logs for "Transaction committed successfully"
3. Verify that all EAN pages were created/updated correctly
4. Test error handling by introducing an error mid-import (e.g., invalid data)
5. Verify that the transaction was aborted and no partial changes remain

## Files Modified

- `internal/db/transaction.go` - New transaction manager
- `internal/db/eanpage_repo.go` - Transactional EAN page methods
- `internal/db/eanpage_search.go` - Transactional EAN page search methods
- `internal/db/turbo_search.go` - Transactional sort index methods
- `internal/db/category_repo.go` - Transactional category tree methods
- `internal/api/import_nokaut.go` - Updated import flow to use transactions
