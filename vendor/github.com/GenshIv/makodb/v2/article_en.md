# Why Document-Oriented Databases Are Fast (and Where Relational Databases Start to Slow Down)

When I tell people that my embedded JSON database reads a document in 16 nanoseconds, they usually don't believe me. "JSON is just text, it's slow." "Document-oriented databases are only good for prototyping; you need PostgreSQL for serious workloads." "You can't build anything fast without B-trees."

I've heard this a hundred times. And every single time, I felt like opening `perf stat` to show what is actually happening under the hood.

In this article, we'll break down *why* document-oriented databases can be significantly faster than relational ones for certain queries, *where exactly* relational databases start to degrade, and *how* to win this battle at the hardware level.

No marketing. Just diagrams, pointers, and CPU cache lines.

---

## Part 1. How a Relational Database Stores Your Row

Let's start with what happens when you execute the query `SELECT * FROM orders WHERE id = 42` in PostgreSQL.

PostgreSQL stores data in 8 KB pages. Each page is a block on disk that contains a header, an array of line pointers (tuple pointers), and the tuples themselves, which grow from the end of the page towards the pointers:

```
PostgreSQL Page (8 KB)
┌─────────────────────────────────────────────────────┐
│ Page Header (24 bytes)                              │
├─────────────────────────────────────────────────────┤
│ Item Pointer 1 → offset 8140, len 68                │
│ Item Pointer 2 → offset 8072, len 68                │
│ Item Pointer 3 → offset 8004, len 68                │
│ ...                                                 │
│ (pointers grow DOWNWARD →)                           │
├─────────────────────────────────────────────────────┤
│                                                     │
│                  [free space]                       │
│                                                     │
├─────────────────────────────────────────────────────┤
│ ← Tuple 3: | t_xmin | t_xmax | t_cid | t_ctid |   │
│            | null bitmap | id=44 | name=... | ...   │
│ ← Tuple 2: | t_xmin | t_xmax | t_cid | t_ctid |   │
│            | null bitmap | id=43 | name=... | ...   │
│ ← Tuple 1: | t_xmin | t_xmax | t_cid | t_ctid |   │
│            | null bitmap | id=42 | name=... | ...   │
└─────────────────────────────────────────────────────┘
```

Pay attention to the header of each tuple. Before you even see your actual data (`id=42, name=...`), PostgreSQL stores **23 bytes of metadata**:
- `t_xmin` (4 bytes) — ID of the transaction that created the row
- `t_xmax` (4 bytes) — ID of the transaction that deleted the row
- `t_cid` (4 bytes) — command identifier within the transaction
- `t_ctid` (6 bytes) — physical address of the newest version of the row
- `t_infomask`, `t_infomask2` (4 bytes) — visibility flags, HOT-update flags
- `t_hoff` (1 byte) — offset to the user data

That's **23 bytes of overhead per row** before your data even begins. And these 23 bytes aren't for you—they are for the MVCC (Multi-Version Concurrency Control) mechanism so that different transactions can see different versions of the same row concurrently.

Now add a NULL bitmap (1 bit per column in the table) and alignment padding. If your row has 10 columns, the null bitmap takes another 2 bytes. Total: **25+ bytes of overhead** on a row you don't even control.

### Query Path: How Many Hops?

When `SELECT * FROM orders WHERE id = 42` is executed, the following happens:

```
Client              PostgreSQL                    Disk / Page Cache
  │                     │                              │
  │── TCP Packet ─────→ │                              │
  │                     │── parse SQL ──→ AST          │
  │                     │── plan query ──→ B-tree scan │
  │                     │                              │
  │                     │── 1. Find B-tree root ──────→│ (index page)
  │                     │                              │
  │                     │── 2. Traverse B-tree ───────→│ (2-3 pages)
  │                     │                              │
  │                     │── 3. Get ctid ──────────────→│ (leaf page)
  │                     │                              │
  │                     │── 4. Read heap page ────────→│ (data page)
  │                     │                              │
  │                     │── 5. Check visibility        │
  │                     │      (MVCC snapshot check)    │
  │                     │                              │
  │                     │── 6. Serialize to wire ─────→│
  │←── TCP Response ────│                              │
```

**Six stages**. A minimum of **4 page lookups** (B-tree root, 1-2 intermediate nodes, leaf node, heap page). And every lookup is a potential CPU cache miss.

---

## Part 2. How a Document-Oriented Database Stores Your Document

Now let's look at how a document-oriented database does this based on a hash table with memory mapping (mmap). We will look at MakoDB as an example, but the concepts apply to any similar engine.

The entire database is a single file mapped directly into the process's virtual address space via the `mmap` system call:

```
Process Virtual Memory
┌──────────────────────────────────────────────────────────┐
│ 0x00000000                                               │
│  ...                                                     │
│  Application code, stack, heap                           │
│  ...                                                     │
├──────────────────────────────────────────────────────────┤
│ 0x7F000000  ← mmap start                                 │
│  ┌────────────────────────────────────────────────────┐  │
│  │ dbHeader (48 bytes)                                │  │
│  │   Magic: "MAKODB\0\0"                              │  │
│  │   FreeOffset: 0x1A3F00  ← write new data here      │  │
│  │   NumBuckets: 5000000                              │  │
│  ├────────────────────────────────────────────────────┤  │
│  │ Hash Table (5,000,000 buckets × 48 bytes)          │  │
│  │   bucket[0]: hash=0, keyOff=0 (empty)             │  │
│  │   bucket[1]: hash=0xA3F1.., keyOff=0x1200..       │  │
│  │   bucket[2]: hash=0, keyOff=0 (empty)             │  │
│  │   ...                                             │  │
│  │   bucket[4999999]: hash=0xBB12.., keyOff=...      │  │
│  ├────────────────────────────────────────────────────┤  │
│  │ Data (keys + values, append-only)                  │  │
│  │   "tx:42"  → {"id":42,"name":"Mako","cost":12.5}  │  │
│  │   "tx:43"  → {"id":43,"name":"Ray","cost":8.1}    │  │
│  │   ...                                             │  │
│  │   ← FreeOffset                                    │  │
│  │                                                   │  │
│  │   [free space until end of file]                  │  │
│  └────────────────────────────────────────────────────┘  │
│ 0x7FFFFFFF  ← mmap end                                   │
└──────────────────────────────────────────────────────────┘
```

### Query Path: One Hop

When a request for `db.Get("tx:42")` comes in:

```
Application                         mmap (RAM / Page Cache)
  │                                     │
  │── hash("tx:42") = 0xA3F10B2C       │
  │                                     │
  │── bucketIdx = hash % 5000000       │
  │   = 1730412                         │
  │                                     │
  │── offset = 48 + 1730412 × 48      │
  │   = 83059824                        │
  │                                     │
  │── *(bucket*)&mapped[83059824] ────→ │ ← ONE pointer
  │                                     │
  │   hash matched? keyLen matched?     │
  │   key bytes matched?                │
  │                                     │
  │── val = mapped[valOffset..+valLen] │ ← ONE pointer
  │                                     │
  │   Done. JSON bytes in hand.         │
```

**Two memory lookups**. One arithmetic operation to calculate the bucket offset. One hash comparison. One read of the value at the offset. That's it.

No SQL parsing. No B-trees. No MVCC visibility checks. No serialization into a wire protocol.

This is why it takes **16 nanoseconds**.

---

## Part 3. The "Deep Window" Problem in Relational Databases

Now let's talk about something rarely mentioned in textbooks: **performance degradation when shifting the pagination window**.

Imagine you have an `orders` table with 10,000,000 rows, sorted by `total_profit`. You make the following query:

```sql
SELECT * FROM orders ORDER BY total_profit OFFSET 9999950 LIMIT 50;
```

Here is what happens in PostgreSQL:

```
B-tree Index on total_profit
┌──────────────────────────────────────────┐
│ Root                                     │
│  ├── Internal Node 1                     │
│  │   ├── Leaf Page 1 (rows 1-500)        │  ← Must traverse
│  │   ├── Leaf Page 2 (rows 501-1000)     │  ← Must traverse
│  │   ├── ...                             │  ← Must traverse
│  │   ├── Leaf Page 20 (rows 9501-10000)  │  ← Must traverse
│  │   └── ...                             │
│  ├── Internal Node 2                     │
│  │   ├── Leaf Page 21 ...                │  ← Must traverse
│  │   └── ...                             │
│  │                                       │
│  │    ... 19,998 pages skipped ...       │  ← ALL OF THIS IS READ
│  │                                       │
│  ├── Internal Node N                     │
│  │   ├── Leaf Page 19999                 │  ← Must traverse
│  │   └── Leaf Page 20000 (9999951-10M)   │  ← TARGET PAGE
│  └──                                     │
└──────────────────────────────────────────┘
```

**PostgreSQL is forced to walk all 9,999,950 rows** before it can give you the 50 you actually want. It cannot "jump" to the desired position in a B-tree because a B-tree is a tree of *ordered keys*, not a random-access array.

This is known as **O(offset + limit)** complexity. On the first page (`OFFSET 0`), the query flies. On the last page (`OFFSET 9999950`), it is **200,000 times slower**.

### How a Document-Oriented Database Handles This with a Pre-Sorted Index

In MakoDB, the `sort:total_profit` index is simply a **flat array** of 10,000,000 `int32` numbers, where each number is the ID of a document sorted in ascending order of `total_profit`:

```
sort:total_profit (continuous int32 array in mmap)
┌───────────────────────────────────────────────────────────┐
│ offset 0     4     8     12    ...                        │
│ ┌─────┬─────┬─────┬─────┬─────────────────────────────┐  │
│ │ 482 │ 119 │ 7701│ 333 │ ...                         │  │
│ └─────┴─────┴─────┴─────┴─────────────────────────────┘  │
│   ↑                                                       │
│   ID of the document with the lowest total_profit         │
│                                                           │
│                    ... 10,000,000 elements ...            │
│                                                           │
│ ┌─────────────────────────────┬─────┬─────┬─────┬─────┐  │
│ │ ...                         │ 5512│  88 │ 4401│ 1002│  │
│ └─────────────────────────────┴─────┴─────┴─────┴─────┘  │
│                                        ↑                  │
│          ID of the document with the 9,999,950th profit   │
│                                                           │
│  offset = 9999950 × 4 = 39,999,800 bytes                  │
│                                                           │
│  ids[9999950] → 4401  ← INSTANT random access             │
│  ids[9999951] → 1002                                      │
│  ...                                                      │
│  ids[9999999] → 7788                                      │
└───────────────────────────────────────────────────────────┘
```

To fetch the page `OFFSET 9999950, LIMIT 50`, we only need to:

```
1. Calculate the byte offset in the array:
   byteOffset = 9999950 × 4 = 39,999,800

2. Read 50 elements (200 bytes):
   ids = mapped[39999800 : 39999800 + 200]

3. Retrieve the document for each ID:
   doc = db.Get("tx:" + ids[i])    // 16 ns × 50 = 800 ns
```

**Total time: ~1 microsecond**, regardless of whether you request the first page or the last page. **O(1)** complexity instead of **O(offset)**.

Here is the degradation chart:

```
Response Time
     │
 10s │                                          ╱ PostgreSQL
     │                                        ╱   (B-tree scan)
  1s │                                      ╱
     │                                    ╱
100ms│                                  ╱
     │                                ╱
 10ms│                              ╱
     │                            ╱
  1ms│                          ╱
     │                        ╱
100μs│──────────────────────────────────────── MakoDB
     │                                         (array, O(1))
 10μs│
     │
  1μs│
     └──────────────────────────────────────────── OFFSET →
      0       2M       4M       6M       8M      10M
```

---

## Part 4. Why an Array is Better Than a Tree (for Certain Tasks)

"But wait," an experienced DBA will say. "A B-tree supports insertions and deletions in O(log n), while an array requires O(n). Are you going to rebuild the array on every write?"

This is a completely fair point. And the answer is no, we don't rebuild it.

### The Hybrid Approach: Array + Buffer (LSM Pattern)

We split the data into two layers:

```
┌─────────────────────────────────────────────────────────┐
│ Layer 1: RAM Buffer (Hot Data)                          │
│                                                         │
│  recentTransactions []Transaction                       │
│  ┌──────┬──────┬──────┬──────┬──────┐                   │
│  │tx:N+1│tx:N+2│tx:N+3│ ...  │tx:N+k│  ← k < 50       │
│  └──────┴──────┴──────┴──────┴──────┘                   │
│  Sorted in memory by the target sorting field           │
│  Insertion: O(1) append                                 │
│                                                         │
├─────────────────────────────────────────────────────────┤
│ Layer 2: On-Disk Array (Cold Data)                      │
│                                                         │
│  sort:total_profit  int32[10,000,000]                   │
│  ┌─────┬─────┬─────┬─────┬─────────────────────┐       │
│  │ 482 │ 119 │ 7701│ 333 │ ... 10M elements    │       │
│  └─────┴─────┴─────┴─────┴─────────────────────┘       │
│  Fully sorted. Random access O(1).                      │
│                                                         │
└─────────────────────────────────────────────────────────┘

        Reading (merge)
        ════════════════

     Disk (O(1) access)        RAM (k elements)
          │                        │
          │    ┌──────────────┐    │
          └───→│  Merge-Sort  │←───┘
               │  two streams │
               └──────┬───────┘
                      │
                      ▼
               Result (50 rows)
```

During reads, we merge the two sorted streams (the disk array and the RAM buffer) on the fly using a two-pointer algorithm. This works in O(offset + limit) relative to the *disk array* (a single linear pass), but with the array being O(1), we just jump directly to the target offset.

When the RAM buffer fills up (e.g., 50 elements), we perform a **flush**:

```
Flush (merging the buffer into the disk array)
──────────────────────────────────────────────

For each element in the buffer:
  1. Binary search the insertion position in the array
     (20 comparisons for 10M elements)
  2. Shift the tail of the array by 1 position
  3. Insert the element

50 elements × 20 comparisons = 1000 Get operations
+ array rewriting (40 MB) ≈ 5 ms total
```

Yes, a flush takes 5 milliseconds. But it only happens once every 50 writes. The amortized cost of insertion: **100 microseconds**—very acceptable for read-heavy workloads.

---

## Part 5. CPU Cache Lines: Why Contiguity is Everything

Now for the best part. Let's zoom in to the processor level and see why a flat array in `mmap` is *physically* faster than a B-tree.

A modern CPU does not read from RAM one byte at a time. It reads in **cache lines of 64 bytes**. When you access a single byte, the CPU loads the entire 64-byte block around it into its L1 cache.

### B-Tree: Jumping Across Memory

```
B-Tree in Memory (simplified)
─────────────────────────────

Page A (root)                ← cache line loaded
  addr: 0x1000
  ├── key < 5000 → ptr: 0x8A000
  └── key ≥ 5000 → ptr: 0x12F000

       ↓ JUMP to 0x8A000
       ↓ (distance: 561 KB, ~8781 cache lines)

Page B (internal)            ← NEW cache line, L1/L2 miss likely
  addr: 0x8A000
  ├── key < 2500 → ptr: 0x3F2000
  └── key ≥ 2500 → ptr: 0x5A1000

       ↓ JUMP to 0x3F2000
       ↓ (distance: 3.4 MB, ~55000 cache lines)

Page C (leaf)                ← NEW cache line, L2 miss likely
  addr: 0x3F2000
  └── key=42 → ctid: (page 1881, offset 3)

       ↓ JUMP to heap page 1881
       ↓ (distance: UNPREDICTABLE)

Heap page 1881              ← NEW cache line, L3 miss likely
  addr: 0x750000
  └── tuple with row data
```

**4 jumps**, each over an unpredictable distance. The processor cannot predict (prefetch) the next address because it depends on the data (the key values inside the tree nodes). Each jump is a potential **cache miss**, costing:
- L1 miss → L2 hit: ~5 ns
- L2 miss → L3 hit: ~15 ns
- L3 miss → RAM: ~60-100 ns

In the worst case: **4 × 60 = 240 nanoseconds** spent just moving through memory, not including any actual calculations.

### Flat Array: Sequential Access

```
int32 Array in mmap
───────────────────

  addr: 0x7F000000                          (mmap start)
  │
  │ offset = bucketIdx × 48                 (arithmetic, 0 ns)
  │
  ▼
  0x7F000000 + 83059824 = 0x842D3F10       (bucket address)
  ┌────────────────────────────────────┐
  │ bucket: hash, keyOff, valOff, ...  │    ← 48 bytes, 1 cache line
  └─────────────┬──────────────────────┘
                │
                │ valOffset = 0x1A3400     (from bucket)
                │
                ▼
  0x7F000000 + 0x1A3400 = 0x7F1A3400      (value address)
  ┌────────────────────────────────────┐
  │ {"id":42,"name":"Mako","cost":12.5}│    ← JSON bytes
  └────────────────────────────────────┘
```

**2 memory lookups**. Both addresses are calculated arithmetically (no data dependency), so the processor *can* prefetch them. When reading the `sort:*` array, data lies continuously, and the CPU loads them in 64-byte cache lines = **16 int32 elements in a single load**.

```
Cache Line (64 bytes) → 16 int32 elements
┌─────┬─────┬─────┬─────┬─────┬─────┬─────┬─────┐
│ 482 │ 119 │ 7701│ 333 │ 8812│ 1003│ 5544│ 2271│  ← 32 bytes
├─────┼─────┼─────┼─────┼─────┼─────┼─────┼─────┤
│ 9901│  42 │ 3387│ 6610│ 7004│ 1158│ 4429│ 8890│  ← 32 bytes
└─────┴─────┴─────┴─────┴─────┴─────┴─────┴─────┘
  ↑ A single RAM access brings in 16 document IDs
```

When scanning an array sequentially, the CPU utilizes its **hardware prefetcher**—special hardware circuitry that detects the sequential read pattern and starts pulling the next cache lines *before* your code even requests them.

A B-tree cannot benefit from this optimization because the address of the next node is unpredictable.

---

## Part 6. The Problem of Deletions and How to Solve It

In an append-only store (which MakoDB is), there is a weak point: deleted records do not free up space immediately. The file only grows.

```
Append-Only File After Multiple Updates
┌────────────────────────────────────────────────────────┐
│ key1=v1 │ key2=v2 │ key1=v1' │ key3=v3 │ key2=v2' │  │
│ (old)   │ (old)   │ (new)    │ (active)│ (new)    │  │
│  ↑ garbage│  ↑ garbage│         │         │          │  │
└────────────────────────────────────────────────────────┘
```

Old versions of `key1=v1` and `key2=v2` still occupy space. Buckets point to the new offsets (`v1'`, `v2'`), but the dead bytes remain in the file.

### Vacuum: Copying Active Data to a Clean File

The classic solution is **compaction** (called VACUUM in PostgreSQL, and compaction in LSM databases):

```
Vacuum (Compaction)
═══════════════════

Old File                         New File (vacuum_temp)
┌──────────────────────┐        ┌────────────────────────┐
│ key1=v1  (dead)      │        │                        │
│ key2=v2  (dead)      │  ───→  │ key1=v1' (active)      │
│ key1=v1' (active)    │  copy  │ key2=v2' (active)      │
│ key3=v3  (active)    │  ───→  │ key3=v3  (active)      │
│ key2=v2' (active)    │        │                        │
│ [40% garbage]        │        │ [0% garbage, compact]  │
└──────────────────────┘        └────────────────────────┘
         ↓
   os.Remove(old)
   os.Rename(temp → old)
```

MakoDB executes a vacuum automatically upon database startup. The process is:
1. Open the old file as read-only.
2. Create a new temporary file.
3. Iterate over all active records (`ForEach`) and copy them to the new file.
4. Close both databases.
5. Delete the old file, rename the new one.

This happens transparently to the application. When the database is opened next, the file will be clean and compact.

---

## Part 7. But Don't Relational Databases Have Hash Indexes Too?

Yes, PostgreSQL supports hash indexes (`CREATE INDEX ... USING hash`). But there is a crucial difference:

```
PostgreSQL Hash Index              MakoDB Hash Table
─────────────────────              ─────────────────

┌─────────────────────┐            ┌─────────────────────┐
│ Hash index page     │            │ mmap region         │
│  bucket → overflow  │            │  bucket → inline    │
│  pages → heap page  │            │  data right here    │
└──────┬──────────────┘            └──────┬──────────────┘
       │                                  │
       │ 3-4 levels of indirection:        │ 1-2 levels of indirection:
       │  1. hash bucket page             │  1. bucket struct
       │  2. overflow page(s)             │  2. value bytes
       │  3. heap page                    │
       │  4. MVCC visibility check        │
       │                                  │
       ▼                                  ▼
   ~200-400 ns                        ~16 ns
```

Key difference: in PostgreSQL, a hash index points to a **ctid** (the physical address of a row in the heap file). This is an extra level of indirection. Plus the MVCC check. Plus TOAST tables overhead for large values. Plus WAL logging.

In MakoDB, the bucket directly contains the offset to the value bytes in the same file. No intermediate structures. No visibility checks.

---

## Part 8. The Cherry on Top: You Build Your Own Indexes

Everything I described above—hash tables, flat arrays, cache lines—is engine mechanics. But there is another advantage of document-oriented databases that is often overlooked. And it is probably the most powerful one.

**Your application layer can independently create its own indexes and data structures—and work with them at system-kernel speeds.**

In a relational database, you are limited to the set of indexes provided by the engine: B-tree, hash, GIN, GiST, BRIN. Each is a complex internal structure hidden behind the SQL interface. You can't look inside, you can't change the storage format, you can't optimize it for your specific task. You just write `CREATE INDEX` and hope the query planner does the right thing.

In a document-oriented KV store, the situation is fundamentally different. An index is **just another key** with a binary value. This means you can build any structures you can think of:

```
Examples of Custom Indexes in a KV Store
════════════════════════════════════════

1. Pre-sorted array of IDs (what we saw earlier):
   "sort:total_profit" → [482, 119, 7701, 333, ...]
   Format: flat int32[], O(1) pagination

2. Inverted index for full-text search:
   "idx:country:germany" → [4, 101, 502, 8841, ...]
   Format: array of document IDs containing "Germany"

3. Bitmap index for boolean filters:
   "bmp:is_vip" → 0110010011101...
   Format: bitset, AND/OR in nanoseconds

4. Histograms for analytics:
   "hist:price:0-100"   → 1482033
   "hist:price:100-200" → 892441
   Format: simple integers

5. Bloom filter for fast set membership checks:
   "bloom:emails" → [binary filter data]
   Format: custom binary structure

6. Spatial index (R-tree / geohash):
   "geo:u33dc0" → [id1, id2, id3, ...]
   Format: geohash prefix → array of coordinates
```

Notice: each of these indexes is a standard `key → value` pair in the same database. They lie right next to the documents themselves, in the same `mmap` file, in the same virtual memory space. Reading any index takes the same **16 nanoseconds** as reading a document.

### Why This Matters

In PostgreSQL, if you need a custom index (e.g., a bitmap index on a custom attribute or a spatial index with a custom grid), you have two choices:
1. Write an extension in C (`pg_extension`)—months of work, debugging in the DBMS kernel, risking a crash of the entire cluster.
2. Maintain a materialized view—extra complexity, latency in updates, duplicated data.

In a KV store, you simply do:

```go
// Build an inverted index — one line
db.Put("idx:country:germany", serializeIDs(germanDocIDs))

// Build a bitmap — one line
db.Put("bmp:is_vip", vipBitmap.Bytes())

// Read index — 16 nanoseconds
ids, _ := db.Get("idx:country:germany")
```

No extensions. No DDL migrations. No query planner that might decide not to use your index.

### The Trade-off: More Control, More Responsibility

Of course, you pay for this freedom. A relational database automatically updates indexes on every insert, update, and delete. In a document-oriented database, **you** decide:
- When to update the index (synchronously on write? asynchronously in the background? in batches every N writes?)
- What format to store it in (flat array? compressed bitset? delta encoding?)
- How to handle concurrent updates (mutex? CAS? worker-based sharding?)

This requires **more attention** from the developer. But it yields a **significantly better result** because you design the data structure specifically for your workload, rather than adjusting to a generic B-tree that has to work "for everyone".

```
Universal Index (B-Tree)          vs     Specialized Index
────────────────────────                 ─────────────────

 ┌────────────────────┐                  ┌────────────────────┐
 │ Insert: O(log n)   │                  │ Insert: O(1)       │
 │ Search: O(log n)   │                  │ Search: O(1)       │
 │ Range:  O(log n)   │                  │ Range:  O(1)       │
 │ Overhead: ~40%     │                  │ Overhead: 0%       │
 │                    │                  │                    │
 │ Works for ANY      │                  │ Works for YOUR     │
 │ query pattern      │                  │ specific task      │
 └────────────────────┘                  └────────────────────┘

 Good choice when you                    Best choice when you
 DO NOT KNOW what queries                KNOW EXACTLY what queries
 you will run tomorrow.                  you will run tomorrow.
```

And this is the true essence of the document-oriented approach. It's not about "JSON vs tables." It's about **removing the constraints of someone else's query planner** and getting direct access to the hardware. With all the power and responsibility that brings.

---

## Conclusion: When to Use What

```
┌────────────────────────────┬────────────────────┬────────────────────┐
│ Feature                    │ Relational (PG)    │ Document (KV)      │
├────────────────────────────┼────────────────────┼────────────────────┤
│ Point read by key          │ ~200-1000 ns       │ ~16 ns             │
│ Pagination OFFSET 0        │ ~1 ms              │ ~1 µs              │
│ Pagination OFFSET 10M      │ ~2-10 sec (!)      │ ~1 µs              │
│ JOIN of 3 tables           │ Fast (native)      │ Application level  │
│ ACID Transactions          │ Full               │ Reservation*       │
│ Concurrent writes          │ MVCC               │ Mutex / Sharding   │
│ Deletions & updates        │ In-place + VACUUM  │ Append + Vacuum    │
│ Ad-hoc queries             │ SQL, any logic     │ Key-based only     │
│ Custom indexes & structures│ Extension in C     │ Simple Put/Get     │
│ Overhead per row           │ 23+ bytes          │ 0 bytes            │
│ Network overhead           │ TCP + wire proto   │ 0 (embedded)       │
└────────────────────────────┴────────────────────┴────────────────────┘
```

\* *Transactions in document-oriented databases are very easy to implement using a "reservation" pattern: you write the intention of an operation to a separate key (e.g., `txn:pending:12345`), complete the steps, and then either commit (delete the pending key) or rollback (restore the previous values from the pending record). But that is a story for another time.*

Document-oriented databases are not a silver bullet. They won't replace PostgreSQL for complex business logic with transactions and JOINs. But for tasks where:
- Read workloads heavily dominate write workloads
- Data is naturally represented as documents (JSON, BSON)
- Predictable latency is needed across any pagination depth
- There is no need for complex JOINs and database-level transactions
- You are willing to design indexes specifically for your workload instead of relying on a generic planner

...they can be **orders of magnitude** faster.

Not because "NoSQL is cooler than SQL," but because **a random-access array is physically faster than a tree of pointers** when you don't need the properties of a tree. And because **a specialized index built by you for your specific workload will always beat a generic one**, if you know what you are doing.

The CPU doesn't lie. Cache lines don't lie. `perf stat` doesn't lie.

---

*If you want to try this out yourself, MakoDB is available as an embedded library in Go. One binary, zero dependencies, zero servers. Just `go get` and you're good to go.*
