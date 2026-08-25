# Why JSON is secretly a high-performance DB format (when you strip away the abstraction layers)

We are used to thinking of JSON as a "slow" human-readable serialization format. We use it for REST APIs, configs, and web apps, but when it comes to high-performance databases, we immediately reach for Protocol Buffers, FlatBuffers, or custom binary layouts. 

But what if we looked at JSON not from a human perspective, but from a machine's perspective? 

If you strip away the heavy abstraction layers, standard JSON is highly dense, transparent, and phenomenally fast to process. In fact, you can build a database engine directly on top of JSON documents that runs reads in **16 nanoseconds** and searches in **under 0.5 milliseconds** for over a million records.

Here is how we did it with **MakoDB** (a serverless, memory-mapped NoSQL Key-Value database written in Go).

---

## 📊 The Benchmarks (Hardware: AMD Ryzen 9 7950X3D)

Since MakoDB operates directly on raw JSON documents, here is the latency and throughput under parallel load (32 threads):

| Operation | Latency (ns/op) | Throughput | Allocations | Description |
| :--- | :--- | :--- | :--- | :--- |
| **Get** | **16.78 ns** | **~60,000,000 ops/sec** | **0 B/op** | Parallel Lock-Free Reads |
| **Query** | **27.97 ns** | **~35,000,000 ops/sec** | **0 B/op** | Zero-alloc JSON fields projection |
| **Put** | **484.50 ns** | **~2,060,000 ops/sec** | **0 B/op** | Parallel Writes (16 Shards) |
| **Search** | **71.88 μs** | **~14,000 ops/sec** | **600 KB/op** | AND Multi-term Search (1000 hits) |

---

## MakoDB Code Examples: How to use it

MakoDB is a serverless daemonless library. You import it, open a database file, and start querying. Here are the core methods:

### 1. Initialization
```go
// Open/create a sharded database: path, shards, max size, index buckets per shard
db, err := makodb.OpenSharded("mydb.db", 16, 15*1024*1024*1024, 6250000)
if err != nil {
    log.Fatalf("Failed to open DB: %v", err)
}
defer db.Close()
```

### 2. Writing (Put) and Lock-Free Reading (Get)
```go
// 1. Put (Create / Update)
key := "user:101"
jsonDoc := []byte(`{"name":"Mako","age":25,"city":"Ocean","role":"admin"}`)
err := db.Put(key, jsonDoc)

// 2. Get (Lock-Free Read)
val, err := db.Get("user:101")
log.Printf("Document: %s", string(val))
```

### 3. Schema Projection & Querying
MakoDB uses the zero-allocation parser `silentjson` to project and extract fields directly from memory without deserializing the whole document. This runs at **35,000,000 queries per second**:
```go
type UserAgeQuery struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

var result UserAgeQuery
// Projects only specified fields directly out of raw JSON bytes
err := db.Query("user:101", &result)
```

### 4. Full-Text Search (Inverted Index)
MakoDB supports indexing document text and performing fast multi-term intersection searches (AND queries) in microseconds:
```go
docID := "doc:456"
bodyText := "Mako is an extremely fast memory-mapped JSON database"

_ = db.Put(docID, []byte(`{"id":"doc:456","body":"`+bodyText+`"}`))
_ = db.Index(docID, bodyText)

// Search returns matching document IDs
matches, _ := db.Search("mako database") // Returns ["doc:456"]
```

---

## The Architecture: Direct mmap & Zero Abstractions

No magic, just mechanical sympathy. MakoDB maps database files directly into the virtual address space of your application via `mmap`.
To the processor, the database is just a contiguous chunk of RAM. Key-value reads happen in nanoseconds (directly from the OS Page Cache), bypassing thread context-switches and network overhead.

Because the storage engine is daemonless and stores everything as raw JSON bytes, multiple processes (whether they are in **Go, C++, Python, or PHP**) can map and access the same database files simultaneously and safely.

```text
+-----------------------------------------------------------------+
|                       Go BFF Application                        |
|   [ In-Memory Write Buffer ]      [ Two-Pointer Merge Engine ]  |
+-------------------------------+---------------------------------+
                                |
                                | (Zero-copy mmap / Shared Memory)
                                v
+-----------------------------------------------------------------+
|                  MakoDB Storage (Key-Value)                     |
|  Documents:                                                     |
|    - "tx:101" -> {"id":101,"country":"Germany","cost":12.5}     |
|  Indices:                                                       |
|    - "sort:cost"           -> tx:15,tx:101,tx:88...             |
|    - "idx:country:germany" -> tx:4,tx:101,tx:502...             |
+-----------------------------------------------------------------+
```

---

## Solving the Real-time Indexing Bottleneck (LSM-style)

In read-heavy setups, sorting on the fly is a bottleneck. We pre-sort the index on disk (creating lists of sorted IDs like `sort:total_profit`). 

However, if you write new transactions to the database in real-time, rebuilding a 1,000,000-record index on every write would kill performance. To solve this, we implemented a hybrid in-memory buffer (LSM-style):

1. **Write Path**: New records are saved to MakoDB and appended to an in-memory buffer `recentTransactions`. Writes complete instantly (~500 nanoseconds).
2. **Read Path**: The query engine merges the pre-sorted disk index stream and the sorted in-memory stream using a **two-pointer merge-sort** algorithm on the fly. 
3. **Lazy Comparisons**: To merge them without loading the entire database, the engine lazily queries the database *only* for comparison values of the items inside the current pagination page.

When we flush the memory buffer to disk (either manually or when the buffer reaches 50 items), we perform a binary-search insertion for each new item in the sorted index array:
* A binary search in a sorted array of 1,000,000 elements takes at most **20 comparisons**.
* For a batch of 50 new items: $50 \times 20 = 1000$ точечных `Get` запросов к MakoDB.
* Each MakoDB read takes ~500 ns. The entire index flush completes in **under 5 milliseconds** on a standard SSD.

## 🛡️ Crash Resiliency & Deadlock Safety

In serverless architectures where the database is mapped directly into application memory space, a process crash during a write is a major threat. We built two key protection layers:

### 1. Deadlock Safety (RobustShmMutex)
If a process crashes (due to panic or `kill -9`) while holding a write lock on MakoDB, standard mutexes would deadlock forever.
MakoDB uses **`RobustShmMutex`**:
- The write lock stores the **Process ID (PID)** of the owner in shared memory.
- If another process finds the lock busy, it queries the OS (using `OpenProcess` on Windows or signal `0` on Unix) to check if the owner PID is alive.
- If the owner has crashed, the lock is automatically cleared (stolen) by the new writer, keeping the database fully operational.

### 2. Zero Data Loss (Startup Auto-Recovery)
What happens if the backend crashes while carrying 49 unindexed transactions in memory?
- The transactions themselves are already safe — they were saved to MakoDB under `tx:<id>` keys instantly (in 480 ns).
- **At boot**, the server queries the highest indexed ID from the on-disk index `sort:id`.
- It then scans MakoDB for any higher unindexed keys (`tx:<maxID+1>`, `tx:<maxID+2>`, etc.).
- If found, it loads them into the memory buffer and triggers a merge-flush. The index recovers automatically on startup with zero data loss!

---

## ⚖️ Load Leveling

During massive write bursts, writing straight to disk indices would saturate disk I/O and block read threads. 

The hybrid in-memory buffer acts as a **write shock-absorber**:
- Write bursts are absorbed in RAM at memory speeds (~500 ns per document).
- Disk index writes are leveled and flushed in optimized batches of 50 items. This minimizes total SSD block writes and balances I/O spikes.

---

## Try it yourself

We packaged the entire demo application—including MakoDB, a 1M transaction mock generator, search/sort engine, and a web UI—into a **single self-contained binary** using `go:embed`.

You can download the pre-compiled binary for Windows, Linux, or macOS and benchmark MakoDB on your own hardware:

* **Demo & Releases**: [github.com/GenshIv/makodb-demo](https://github.com/GenshIv/makodb-demo)
* **High-Performance JSON parser**: [github.com/GenshIv/silentjson](https://github.com/GenshIv/silentjson)

I would love to discuss the architecture in the comments! If you feel there are any weak points, let's talk about them.
