# In-Place Updates: Anti-Vacuum Optimizations

MakoDB implements smart in-place update optimizations that reduce fragmentation and minimize the need for vacuum operations.

## Overview

Instead of always allocating new space when updating a key-value pair, MakoDB intelligently reuses existing space when possible. This reduces fragmentation, improves performance, and decreases the frequency of vacuum operations.

## How It Works

### 1. Same-Size Updates (Always In-Place)

When updating a key with a value of the same size, MakoDB always overwrites the existing data in place:

```go
// Original
db.PutByKey("key1", []byte("hello"))  // 5 bytes

// Update (same size)
db.PutByKey("key1", []byte("world"))  // 5 bytes - overwritten in place
```

**Benefits:**
- No new space allocation
- No fragmentation
- Zero overhead

### 2. Last Record Optimization

When updating the last record in a bucket chain with a smaller value, MakoDB can overwrite in place:

```go
// Insert record
db.PutByKey("key1", []byte("long_value_here"))  // 15 bytes

// Update with shorter value (last record)
db.PutByKey("key1", []byte("short"))  // 5 bytes - overwritten in place
```

**Conditions:**
- Record is last in chain (`NextOffset == 0`)
- New value end ≤ FreeOffset
- No other references to this version

### 3. Space Reuse for Growing Records

When updating a last record with a larger value, MakoDB checks if there's enough space before the FreeOffset:

```go
// Insert record
db.PutByKey("key1", []byte("short"))  // 5 bytes

// Update with longer value (if space available)
db.PutByKey("key1", []byte("much_longer_value_here"))  // 22 bytes
```

**Conditions:**
- Record is last in chain
- New value fits before FreeOffset
- No other records after it

## When In-Place Updates Are Used

### ✅ In-Place Update (No New Allocation)

1. **Same size**: New value length == Old value length
2. **Smaller value**: New value length < Old value length (last record)
3. **Fits in space**: New value end ≤ FreeOffset (last record)

### ❌ New Allocation Required

1. **Chain not empty**: Record has `NextOffset != 0`
2. **Not enough space**: New value end > FreeOffset
3. **Other references**: Other buckets reference this version

## Performance Benefits

### Reduced Fragmentation

```
Without in-place updates:
[Key1_v1][Key1_v2][Key1_v3]  ← Fragmented, wasted space

With in-place updates:
[Key1_v3]  ← Single record, no waste
```

### Faster Updates

- **Same-size updates**: O(1) - just copy bytes
- **Smaller updates**: O(1) - overwrite existing space
- **No allocation overhead**: No memory management

### Less Vacuum Needed

- Fewer orphaned records
- Less dead space to reclaim
- Lower CPU usage for maintenance

## Real-World Example

Consider a document store where you frequently update metadata:

```go
// Initial document
db.PutByKey("doc1", []byte(`{"title":"Hello","views":10}`))

// Update views (same size)
db.PutByKey("doc1", []byte(`{"title":"Hello","views":15}`))  // In-place

// Update title (smaller)
db.PutByKey("doc1", []byte(`{"title":"Hi","views":15}`))  // In-place

// Update title (larger, if space available)
db.PutByKey("doc1", []byte(`{"title":"Hello World","views":15}`))  // In-place or new
```

## Monitoring

You can monitor the effectiveness of in-place updates by tracking:

1. **FreeOffset growth**: Slower growth = more in-place updates
2. **Vacuum frequency**: Less frequent vacuum = better optimization
3. **Fragmentation ratio**: Lower ratio = less waste

## Limitations

1. **Not always possible**: Chain operations may require new allocation
2. **Space constraints**: Large updates may not fit in available space
3. **Concurrent access**: Multiple writers may affect optimization

## Best Practices

1. **Keep values similar in size**: Maximizes same-size updates
2. **Batch updates**: Group related updates together
3. **Monitor FreeOffset**: Track growth to measure effectiveness
4. **Run vacuum periodically**: Still needed for long-term maintenance

## ⚠️ Important: Index Updates

Do NOT update the same index concurrently from multiple goroutines.
This can lead to unexpected behavior.

// ❌ WRONG
go db.TurboPutIndex("my_index", doc1, val1)
go db.TurboPutIndex("my_index", doc2, val2)

// ✅ CORRECT
db.TurboPutIndex("my_index", doc1, val1)
db.TurboPutIndex("my_index", doc2, val2)

## Conclusion

In-place updates are a transparent optimization that significantly reduces fragmentation and improves performance without any code changes from the user. MakoDB automatically applies these optimizations whenever possible, making your database more efficient and scalable.
