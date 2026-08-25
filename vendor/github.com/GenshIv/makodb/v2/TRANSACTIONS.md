# Transaction Support in makodb

makodb provides transaction support for atomic batch operations on indexes. Transactions use in-memory mmap storage for high performance and can handle millions of operations efficiently.

## Overview

Transactions allow you to:
- Batch multiple index updates atomically
- Avoid repeated writes to the main index
- Handle large-scale index modifications efficiently
- Roll back changes if needed

## Basic Usage

### Starting a Transaction

```go
package main

import (
    "fmt"
    "github.com/GenshIv/makodb/v2"
)

func main() {
    // Open database
    db, err := makodb.OpenSharded("/path/to/db", 4, 64*1024*1024, 1024, true)
    if err != nil {
        panic(err)
    }
    defer db.Close()

    // Begin transaction
    txn, err := db.BeginTransaction()
    if err != nil {
        panic(err)
    }

    // ... perform operations ...

    // Commit or abort
    if err := txn.Commit(); err != nil {
        panic(err)
    }
}
```

### Adding Documents to Indexes

```go
// Begin transaction
txn, _ := db.BeginTransaction()

// Add document to index (auto-allocates space if needed)
err := txn.SetTokenDoc("category:electronics", "doc123", []byte("Electronics category data"))

// Add another document to the same index
err = txn.SetTokenDoc("category:electronics", "doc456", []byte("More electronics data"))

// Add to a different index
err = txn.SetTokenDoc("brand:samsung", "doc789", []byte("Samsung products"))

// Commit all changes atomically
err = txn.Commit()
```

### Replacing Documents

```go
// Begin transaction
txn, _ := db.BeginTransaction()

// Set initial value
err := txn.SetTokenDoc("status:active", "doc123", []byte("active"))

// Replace with new value (same offset, new content)
err = txn.SetTokenDoc("status:active", "doc123", []byte("inactive"))

// Commit
err = txn.Commit()
```

### Removing Documents

```go
// Begin transaction
txn, _ := db.BeginTransaction()

// Add some documents
err := txn.SetTokenDoc("tag:important", "doc1", []byte("data1"))
err = txn.SetTokenDoc("tag:important", "doc2", []byte("data2"))

// Remove one document
err = txn.RemoveTokenDoc("tag:important", "doc1")

// Commit
err = txn.Commit()
```

## Reading with Transaction Awareness

When a transaction is active, reads automatically include transaction changes:

```go
// Main database has doc1 with value "old"
db.PutByKey("doc1", []byte("old"))

// Begin transaction
txn, _ := db.BeginTransaction()

// Modify doc1 in transaction
err := txn.SetTokenDoc("index:name", "doc1", []byte("new"))

// Read will return "new" (from transaction)
value, _ := db.GetTokenDoc("index:name", "doc1")
fmt.Println(string(value)) // "new"

// Abort transaction - changes are discarded
err = txn.Abort()

// Read will return "old" again (from main database)
value, _ = db.GetTokenDoc("index:name", "doc1")
fmt.Println(string(value)) // "old"
```

## Pre-allocating Index Space

You can pre-allocate space for indexes to optimize performance:

```go
// Begin transaction
txn, _ := db.BeginTransaction()

// Pre-allocate 10MB for a large index
err := txn.AllocateIndexSpace("large:index", 10*1024*1024)

// Pre-allocate 2MB for a small index
err = txn.AllocateIndexSpace("small:index", 2*1024*1024)

// Use the indexes
for i := 0; i < 100000; i++ {
    docID := makodb.HashDocID(fmt.Sprintf("doc%d", i))
    err = txn.SetTokenDoc("large:index", docID, []byte("data"))
}

// Commit
err = txn.Commit()
```

If you don't pre-allocate, indexes default to 5MB and auto-expand as needed.

## Large-Scale Operations

Transactions are optimized for handling millions of operations:

```go
// Begin transaction
txn, _ := db.BeginTransaction()

// Pre-allocate large space
err := txn.AllocateIndexSpace("massive:index", 100*1024*1024) // 100MB

// Process millions of documents
for i := 0; i < 5000000; i++ {
    err = txn.SetTokenDoc("massive:index", fmt.Sprintf("doc%d", i), []byte(fmt.Sprintf("value%d", i)))
    if err != nil {
        // Handle error
        break
    }
}

// Commit all changes at once
err = txn.Commit()
```

## Error Handling

```go
// Begin transaction
txn, err := db.BeginTransaction()
if err != nil {
    // Handle error
    return err
}

// Perform operations
err = txn.SetTokenDoc("index", docID, []byte("data"))
if err != nil {
    // Abort on error
    _ = txn.Abort()
    return err
}

// Commit
if err := txn.Commit(); err != nil {
    // Handle commit error
    return err
}
```

## Transaction Lifecycle

1. **Begin**: `db.BeginTransaction()` - Creates transaction with 50MB mmap
2. **Allocate**: `txn.AllocateIndexSpace(token, size)` - Optional pre-allocation
3. **Modify**: `txn.SetTokenDoc()` / `txn.RemoveTokenDoc()` - Modify indexes
4. **Read**: `db.GetTokenDoc()` - Reads include transaction changes
5. **End**: `txn.Commit()` or `txn.Abort()` - Finalize transaction

## Performance Characteristics

- **Memory**: 50MB per transaction (configurable)
- **Default index size**: 5MB per token (configurable)
- **Auto-expansion**: Indexes double in size when needed
- **I/O**: No disk I/O until commit
- **Throughput**: Millions of operations per second possible

## Best Practices

1. **Pre-allocate space** for large transactions to avoid expansion overhead
2. **Batch operations** - Use transactions for bulk updates
3. **Keep transactions short-lived** - Commit or abort promptly
4. **Handle errors** - Always check for errors and abort on failure
5. **Monitor memory** - Large transactions consume significant RAM

## Limitations

- Only one active transaction per database instance
- Transaction data is lost on crash (no WAL persistence yet)
- Commit applies all changes at once (no partial commits)
