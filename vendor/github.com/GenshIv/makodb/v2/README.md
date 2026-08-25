# MakoDB 🚀

> *"I'm sorry that I long ago coined the term "objects" for this topic because it gets many people to focus on the lesser idea. The big idea is "messaging" — that is what the kernal of Smalltalk is all about... The key in making great and growable systems is much more to design how its modules communicate rather than what their internal properties and behaviors should be."* — **Alan Kay**

**MakoDB** is a serverless, ultra-fast, memory-mapped (mmap) NoSQL database in Go, designed for querying JSON documents with sub-microsecond latency.

---

## 💡 How It Works
Unlike client-server databases (like Redis or PostgreSQL) that require TCP/Unix socket overhead and thread context-switches, **MakoDB is daemonless**:
1. The database consists of standard OS files (shards) mapped directly into the virtual address space of your application via `mmap`.
2. To the processor, the database is just a contiguous chunk of RAM. Key-value reads happen in nanoseconds (directly from the OS Page Cache), bypassing context-switches.
3. Multiple processes can map and access the same database files simultaneously and safely.

---

## 🔒 Crash-Resistant Smart Locks (RobustShmMutex)
In shared-memory architectures, if a process holding a write lock crashes (due to panic or `kill -9`), the lock remains permanently locked, causing deadlocks.

**MakoDB solves this with `RobustShmMutex`**:
1. The lock stores the **Process ID (PID)** of the owner in shared memory.
2. When another writer encounters a locked mutex, it queries the OS (using `OpenProcess` on Windows or signal `0` on Unix) to check if the owner PID is alive.
3. If the process has crashed, the lock is automatically cleared (stolen) by the new writer, keeping the database active.

---

## 🛠️ Getting Started

```go
package main

import (
	"log"
	"github.com/GenshIv/makodb/v2"
)

func main() {
	// Open/create a sharded database: path, shards, max size, index buckets per shard
	db, err := makodb.OpenSharded("mydb.db", 16, 15*1024*1024*1024, 6250000)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()
}
```

---

## 📝 CRUD Operations

### 1. Put (Create / Update)
```go
key := "user:101"
jsonDoc := []byte(`{"name":"Mako","age":25,"city":"Ocean","role":"admin"}`)

err := db.Put(key, jsonDoc)
```

### 2. Get (Read) - Lock-Free
```go
val, err := db.Get("user:101")
if err != nil {
    // Check errors.Is(err, makodb.ErrKeyNotFound)
}
log.Printf("Document: %s", string(val))
```

### 3. MultiGet (Batch Read)
```go
results, err := db.MultiGet([]string{"user:101", "user:102"})
```

### 4. Delete
```go
err := db.Delete("user:101")
```

### 5. Schema Queries via `silentjson`
Uses the zero-allocation parser `silentjson` to project and extract fields directly from memory without deserializing the whole document:

```go
type UserAgeQuery struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

var result UserAgeQuery
err := db.Query("user:101", &result)
```

---

## 🔎 Full-Text Search (Inverted Index)
MakoDB supports indexing document text and performing fast multi-term intersection searches (AND queries).

```go
docID := "doc:456"
bodyText := "Mako is an extremely fast memory-mapped JSON database"

_ = db.Put(docID, []byte(`{"id":"doc:456","body":"`+bodyText+`"}`))
_ = db.Index(docID, bodyText)

// Search returns a list of matching document IDs
matches, _ := db.Search("mako database") // Returns ["doc:456"]
```

---

## 🔍 Multi-Index Intersection
Find documents that exist across multiple independent indexes (e.g., tags, categories, roles) using `IntersectIndexResults`.

First, build your indexes using `Index(token, docList)`:

```go
// tagadmin -> doc1, doc2
_ = db.Index("tagadmin", "doc1,doc2")

// roleactive -> doc1, doc2, doc3
_ = db.Index("roleactive", "doc1,doc2,doc3")

// depteng -> doc1, doc2
_ = db.Index("depteng", "doc1,doc2")
```

Then intersect them:

```go
conditions := []makodb.IndexCondition{
    {Index: "tagadmin"},
    {Index: "roleactive"},
    {Index: "depteng"},
}

result, err := db.IntersectIndexResults(conditions)
// result -> ["doc1", "doc2"] (documents present in ALL three indexes)
```

You can also apply per-condition filters:

```go
conditions := []makodb.IndexCondition{
    {Index: "tagadmin", Include: []string{"doc1"}},
    {Index: "roleactive", Exclude: []string{"doc3"}},
}

result, _ := db.IntersectIndexResults(conditions)
```

---

## 📊 Sort Indexes (On-Demand)
Build sort indexes attached to existing index tokens using `BuildSortIndex` and `BuildNumericSortIndex`. These match the existing `GetIDsByIndex(token + ":sort")` strategy.

Example:

```go
type Transaction struct {
    ID          int     `json:"id"`
    TotalProfit float64 `json:"total_profit"`
}

reg := silentjson.BuildRegistry(reflect.TypeOf(Transaction{}))

// Build a numeric sort index under "idx:profit:sort"
makodb.BuildNumericSortIndex(db, "tx:", "profit", func(tx Transaction) float64 {
    return tx.TotalProfit
}, reg)

// Later: use it with GetIDsByIndexFiltered
sortedIDs, _ := db.GetIDsByIndexFiltered("profit", nil, reverse, limit, offset)
```

String sort index:

```go
makodb.BuildSortIndex(db, "tx:", "by_name", func(tx Transaction) string {
    return tx.CustomerName
}, reg)
```

---

## ⚡ Transactions for Batch Operations

MakoDB provides transaction support for atomic batch operations on indexes, optimized for handling millions of updates efficiently.

```go
// Begin transaction
txn, err := db.BeginTransaction()
if err != nil {
    log.Fatal(err)
}

// Add documents to indexes
docID := makodb.HashDocID("doc123")
txn.SetTokenDoc("category:electronics", docID, []byte("Electronics"))
txn.SetTokenDoc("brand:samsung", docID, []byte("Samsung"))

// Commit all changes atomically
txn.Commit()
```

**Features:**
- ✅ In-memory mmap storage (no disk I/O until commit)
- ✅ Millions of operations per second
- ✅ Automatic index expansion
- ✅ Atomic commit/abort
- ✅ Configurable memory allocation

📖 [Full Transaction Documentation](TRANSACTIONS.md)

---

## 🌐 Multi-Language Support
Since the database files are standard memory-mapped files, modules in other programming languages can read from MakoDB directly.
See the [Examples](examples/README.md) directory:
* 🐍 [Python Example](examples/python/README.md)
* 🦀 [Rust Example](examples/rust/README.md)
* 🖥️ [C++ Example](examples/cpp/README.md)

---

## 📊 Benchmarks (AMD Ryzen 9 7950X3D)

*Measured on a database under parallel load (32 threads):*

| Operation | Latency (ns/op) | Throughput | Allocations | Description |
| :--- | :--- | :--- | :--- | :--- |
| **Get** | **16.78 ns** | **~60,000,000 ops/sec** | **0 B/op** | Parallel Lock-Free Reads |
| **Query** | **27.97 ns** | **~35,000,000 ops/sec** | **0 B/op** | Zero-alloc JSON Querying |
| **Put** | **484.50 ns** | **~2,060,000 ops/sec** | **0 B/op** | Parallel Write (16 Shards) |
| **Search** | **71.88 μs** | **~14,000 ops/sec** | **600 KB/op** | AND Multi-term Search (1000 hits) |

---

## 🌐 Running MakoDB as an HTTP Server

By default, MakoDB is an in-process daemonless library. However, if you need to access it over the network or integrate it with languages that cannot access the filesystem directly, you can run the built-in HTTP server:

```bash
# Start the MakoDB HTTP REST API server
go run cmd/makoserver/main.go
```

Once running, the server exposes the following REST endpoints on port `8080`:

* **GET** `/get?key=xxx` — Retrieve a JSON document.
* **POST** `/put?key=xxx` (with JSON body) — Write or update a document.
* **DELETE** `/delete?key=xxx` — Delete a document.
* **GET** `/search?q=xxx` — Perform a multi-term search query.
