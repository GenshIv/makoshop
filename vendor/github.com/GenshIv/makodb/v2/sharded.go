package makodb

// ShardedDB design notes:
//
// - Each shard lives on its own CPU/core: keys are hashed to a shard,
//   and all hot-path operations (Get/Put/Delete) are lock-free and shard-local.
// - No goroutines in the critical path: hot paths are already parallelized
//   by shard distribution; adding goroutines only adds latency and risk.
// - Monitoring helpers (ShardUsages/ActiveUsage) run sequentially on purpose.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// ShardedDB coordinates multiple DB shards to allow lock-free parallel execution.
type ShardedDB struct {
	dirPath   string
	numShards int
	shards    []*DB
	isClosed  bool
	compact   CompactState // compaction state (compact_shard.go)
	lock      *dbLock      // exclusive lock file to prevent concurrent access

	// Transaction support
	activeTxn *Transaction // Active transaction (nil if none)
	nextTxnID uint64       // Next transaction ID
}

// OpenSharded opens a sharded database inside a directory.
// dirPath is the path to the database directory.
// numShards is the number of database shards to create.
// maxTotalSize is the maximum total size allowed across all shards.
// numBucketsPerShard is the number of hash table slots per shard.
// scipvacuum skips vacuum/compaction on startup if true.
func OpenSharded(dirPath string, numShards int, maxTotalSize uint64, numBucketsPerShard uint64, scipvacuum bool) (*ShardedDB, error) {
	if numShards <= 0 {
		return nil, fmt.Errorf("makodb: numShards must be greater than 0")
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("makodb: failed to create DB directory: %w", err)
	}

	// Acquire exclusive lock to prevent concurrent access from another process.
	lock, err := acquireDBLock(dirPath)
	if err != nil {
		return nil, err
	}

	maxSizePerShard := maxTotalSize / uint64(numShards)

	shards := make([]*DB, numShards)
	for i := 0; i < numShards; i++ {
		shardPath := filepath.Join(dirPath, fmt.Sprintf("shard_%d.db", i))
		db, err := openDB(shardPath, maxSizePerShard, numBucketsPerShard, scipvacuum)
		if err != nil {
			// Clean up opened shards on error
			for j := 0; j < i; j++ {
				shards[j].close()
			}
			releaseDBLock(lock)
			return nil, fmt.Errorf("makodb: failed to open shard %d: %w", i, err)
		}
		shards[i] = db
	}

	return &ShardedDB{
		dirPath:   dirPath,
		numShards: numShards,
		shards:    shards,
		lock:      lock,
	}, nil
}

func (s *ShardedDB) IsClosed() bool {
	return s.isClosed
}

// Close closes all shards in the database and releases the lock file.
func (s *ShardedDB) Close() error {
	s.isClosed = true

	// Abort any active transaction
	if s.activeTxn != nil {
		_ = s.activeTxn.Abort()
	}

	var errs []error
	for i := range s.shards {
		shard := (*DB)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[i]))))
		if shard == nil {
			continue
		}

		// Release the owner's reference and wait for all readers to finish.
		shard.closeRefDecr()

		// Spin until refCount reaches 0 (all readers done).
		for atomic.LoadUint64(&shard.refCount) > 0 {
			runtime.Gosched()
		}

		// Now safe to close: no readers left.
		if !atomic.CompareAndSwapUint32(&shard.closeOnce, 0, 1) {
			// Already closed by closeRefDecr.
			continue
		}
		if err := shard.close(); err != nil {
			errs = append(errs, err)
		}
	}

	// Release the exclusive lock file.
	releaseDBLock(s.lock)
	s.lock = nil

	if len(errs) > 0 {
		return fmt.Errorf("makodb: errors closing shards: %v", errs)
	}
	return nil
}

// Sync flushes all pending writes to disk across all shards.
// Call this after important writes to ensure data persistence.
func (s *ShardedDB) Sync() error {
	if s.isClosed {
		return fmt.Errorf("makodb: database is closed")
	}

	var errs []error
	for i := range s.shards {
		shard := (*DB)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[i]))))
		if shard == nil {
			continue
		}

		shard.incRef()
		if err := shard.Sync(); err != nil {
			errs = append(errs, err)
		}
		shard.closeRefDecr()
	}

	if len(errs) > 0 {
		return fmt.Errorf("makodb: errors syncing shards: %v", errs)
	}
	return nil
}

// Shrink truncates all shard files to their actual used space.
func (s *ShardedDB) Shrink() error {
	for i := range s.shards {
		shard := (*DB)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[i]))))
		shard.incRef()
		if err := shard.Shrink(); err != nil {
			shard.closeRefDecr()
			return err
		}
		shard.closeRefDecr()
	}
	return nil
}

// getShardKey128 returns the DB shard corresponding to the key128 key.
// Uses atomic load to safely handle shard replacement during compaction.
func (s *ShardedDB) getShardKey128(key key128) *DB {
	shardIdx := key[0] % uint64(s.numShards)
	return (*DB)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[shardIdx]))))
}

// getShardIndexKey128 returns the index of the shard that the given key128 key hashes to.
func (s *ShardedDB) getShardIndexKey128(key key128) int {
	return int(key[0] % uint64(s.numShards))
}

// ============================================================================
// key128-based internal methods for ShardedDB
// All internal operations should use these methods instead of the string-based ones.
// ============================================================================

// putKey128 writes or updates a key-value pair using key128 as the key.
// This is the internal version - public Put(string, []byte) should NOT call this directly.
func (s *ShardedDB) putKey128(key key128, value []byte) error {
	shard := s.getShardKey128(key)
	if shard == nil {
		return fmt.Errorf("makodb: shard is nil")
	}
	// Check if shard is closed before incrementing refCount.
	if atomic.LoadUint32(&shard.closeOnce) != 0 {
		return fmt.Errorf("makodb: shard is closed")
	}
	shard.incRef()
	defer shard.closeRefDecr()
	return shard.putKey128(key, value)
}

// PutByKey128 writes or updates a key-value pair using key128 as the key.
// This is the direct counterpart to MultiGetByDocIDs.
// PutByKey stores a value by a key of any type.
// The key is internally converted to key128.
func (s *ShardedDB) PutByKey(key any, value []byte) error {
	return s.putKey128(toKey128(key), value)
}

// PutByKey128 stores a value by a key128 key (internal type).
// This is a legacy method for compatibility; prefer PutByKey for new code.
func (s *ShardedDB) PutByKey128(key any, value []byte) error {
	return s.putKey128(toKey128(key), value)
}

// getKey128 reads the value for a key128 key from the corresponding shard.
// This is the internal version - public Get(string) should NOT call this directly.
func (s *ShardedDB) getKey128(key key128) ([]byte, error) {
	shardIdx := s.getShardIndexKey128(key)

	// If compaction is active for this shard, check compact shard first
	if compact, compactIdx := s.getCompactShard(); compact != nil && compactIdx == shardIdx {
		compact.incRef()
		if val, err := compact.getKey128(key); err == nil {
			compact.closeRefDecr()
			return val, nil
		}
		compact.closeRefDecr()
	}

	shard := s.getShardKey128(key)
	if shard == nil {
		return nil, fmt.Errorf("makodb: shard is nil")
	}
	// Check if shard is closed before incrementing refCount.
	if atomic.LoadUint32(&shard.closeOnce) != 0 {
		return nil, fmt.Errorf("makodb: shard is closed")
	}
	shard.incRef()
	defer shard.closeRefDecr()
	return shard.getKey128(key)
}

// getKey128ZeroAlloc reads the value for a key128 key without allocating memory.
// This is the internal version - public GetZeroAlloc(string) should NOT call this directly.
func (s *ShardedDB) getKey128ZeroAlloc(key key128) ([]byte, error) {
	shardIdx := s.getShardIndexKey128(key)

	// If compaction is active for this shard, check compact shard first
	if compact, compactIdx := s.getCompactShard(); compact != nil && compactIdx == shardIdx {
		compact.incRef()
		if val, err := compact.getKey128ZeroAlloc(key); err == nil {
			compact.closeRefDecr()
			return val, nil
		}
		compact.closeRefDecr()
	}

	shard := s.getShardKey128(key)
	if shard == nil {
		return nil, fmt.Errorf("makodb: shard is nil")
	}
	// Check if shard is closed before incrementing refCount.
	if atomic.LoadUint32(&shard.closeOnce) != 0 {
		return nil, fmt.Errorf("makodb: shard is closed")
	}
	shard.incRef()
	defer shard.closeRefDecr()
	return shard.getKey128ZeroAlloc(key)
}

// deleteKey128 removes a key-value pair using key128 as the key.
// This is the internal version - public Delete(string) should NOT call this directly.
func (s *ShardedDB) deleteKey128(key key128) error {
	shard := s.getShardKey128(key)
	if shard == nil {
		return fmt.Errorf("makodb: shard is nil")
	}
	// Check if shard is closed before incrementing refCount.
	if atomic.LoadUint32(&shard.closeOnce) != 0 {
		return fmt.Errorf("makodb: shard is closed")
	}
	shard.incRef()
	defer shard.closeRefDecr()
	return shard.deleteKey128(key)
}

// MultiGetByDocIDs retrieves values for multiple documents by their key128 docIDs.
// MultiGetByDocIDs retrieves values for multiple documents by their key128.
// Uses the key128 directly as a 16-byte binary key.
// MultiGetByDocIDs retrieves values for multiple documents by their docIDs.
// DocIDs can be of any type (string, uint64, etc.) and are internally converted to key128.
func (s *ShardedDB) MultiGetByDocIDs(docIDs []any) ([][]byte, error) {
	// Convert []any to []key128
	keys := make([]key128, len(docIDs))
	for i, docID := range docIDs {
		keys[i] = toKey128(docID)
	}
	return s.MultiGetByKey128(keys)
}

// MultiGetByKeys retrieves values for multiple keys.
// Keys can be of any type (string, uint64, etc.) and are internally converted to key128.
func (s *ShardedDB) MultiGetByKeys(keys []any) ([][]byte, error) {
	// Convert []any to []key128
	key128Keys := make([]key128, len(keys))
	for i, key := range keys {
		key128Keys[i] = toKey128(key)
	}
	return s.MultiGetByKey128(key128Keys)
}

// MultiGetByKey128 retrieves values for multiple documents by their key128.
// Keys can be of any type (string, uint64, key128, etc.) and are internally converted to key128.
// This is a legacy method for compatibility; prefer MultiGetByKeys for new code.
func (s *ShardedDB) MultiGetByKey128(keys ...any) ([][]byte, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	// Build list of (originalIndex, key) pairs, handling slices
	type keyEntry struct {
		idx int
		key key128
	}
	allEntries := make([]keyEntry, 0)
	for _, k := range keys {
		switch keyVal := k.(type) {
		case key128:
			allEntries = append(allEntries, keyEntry{len(allEntries), keyVal})
		case []any:
			for _, item := range keyVal {
				allEntries = append(allEntries, keyEntry{len(allEntries), toKey128(item)})
			}
		case []key128:
			for _, key := range keyVal {
				allEntries = append(allEntries, keyEntry{len(allEntries), key})
			}
		default:
			allEntries = append(allEntries, keyEntry{len(allEntries), toKey128(k)})
		}
	}

	results := make([][]byte, len(allEntries))

	// Group keys by shard index.
	type shardKeys struct {
		shard *DB
		pairs []struct {
			idx int
			key key128
		}
	}

	shardMap := make(map[int]*shardKeys)

	for _, entry := range allEntries {
		shardIdx := s.getShardIndexKey128(entry.key)
		ent, ok := shardMap[shardIdx]
		if !ok {
			ent = &shardKeys{shard: s.getShardKey128(entry.key)}
			shardMap[shardIdx] = ent
		}
		ent.pairs = append(ent.pairs, struct {
			idx int
			key key128
		}{entry.idx, entry.key})
	}

	// Process each shard once.
	for _, entry := range shardMap {
		entry.shard.incRef()
		for _, p := range entry.pairs {
			val, err := entry.shard.getKey128(p.key)
			if err != nil {
				if !errors.Is(err, ErrKeyNotFound) {
					entry.shard.closeRefDecr()
					return nil, err
				}
				continue
			}
			results[p.idx] = val
		}
		entry.shard.closeRefDecr()
	}

	return results, nil
}

// MultiGetByDocIDsWithPrefix retrieves values for multiple documents by their docIDs.
// The prefix parameter is ignored. The docIDs should already contain any prefix inside the hash.
// Uses direct key128 access for optimal performance.
func (s *ShardedDB) MultiGetByDocIDsWithPrefix(docIDs []any, prefix string) ([][]byte, error) {
	// Convert []any to []key128
	key128DocIDs := make([]key128, len(docIDs))
	for i, docID := range docIDs {
		key128DocIDs[i] = toKey128(docID)
	}
	_ = prefix // Prefix must be inside the hash, not added after
	if len(key128DocIDs) == 0 {
		return nil, nil
	}

	results := make([][]byte, len(key128DocIDs))

	// Group docIDs by shard index.
	type shardDocIDs struct {
		shard *DB
		pairs []struct {
			idx   int
			docID key128
		}
	}

	shardMap := make(map[int]*shardDocIDs)

	for i, docID := range key128DocIDs {
		shardIdx := s.getShardIndexKey128(docID)
		entry, ok := shardMap[shardIdx]
		if !ok {
			entry = &shardDocIDs{shard: s.getShardKey128(docID)}
			shardMap[shardIdx] = entry
		}
		entry.pairs = append(entry.pairs, struct {
			idx   int
			docID key128
		}{i, docID})
	}

	// Process each shard once.
	for _, entry := range shardMap {
		entry.shard.incRef()
		for _, p := range entry.pairs {
			// Use getKey128ZeroAlloc to avoid allocation.
			val, err := entry.shard.getKey128ZeroAlloc(p.docID)
			if err != nil {
				if !errors.Is(err, ErrKeyNotFound) {
					entry.shard.closeRefDecr()
					return nil, err
				}
				continue
			}
			results[p.idx] = val
		}
		entry.shard.closeRefDecr()
	}

	return results, nil
}

// ForEachZeroAlloc iterates over all active key-value pairs across all shards
// without allocating memory for keys or values.
func (s *ShardedDB) ForEachZeroAlloc(cb func(key []byte, value []byte) error) error {
	for i := range s.shards {
		shard := (*DB)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[i]))))
		shard.incRef()
		if err := shard.ForEachZeroAlloc(cb); err != nil {
			shard.closeRefDecr()
			return err
		}
		shard.closeRefDecr()
	}
	return nil
}

// ShardUsage holds usage statistics for a single shard.
type ShardUsage struct {
	ShardIndex      int
	FilePath        string
	FileSize        uint64
	FreeOffset      uint64
	MaxSize         uint64
	ActiveKeys      uint64
	ActiveDataBytes uint64
}

// ShardUsages returns fast usage statistics for all shards based on FreeOffset.
func (s *ShardedDB) ShardUsages() []ShardUsage {
	usages := make([]ShardUsage, len(s.shards))

	for i := range s.shards {
		shard := (*DB)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[i]))))
		if shard == nil {
			continue
		}

		shard.incRef()

		if atomic.LoadUint32(&shard.closeOnce) != 0 {
			shard.closeRefDecr()
			continue
		}

		freeOffset := atomic.LoadUint64(&shard.header.FreeOffset)
		maxSize := atomic.LoadUint64(&shard.header.MaxSize)

		var fileSize uint64
		info, err := os.Stat(shard.file.Name())
		if err == nil {
			fileSize = uint64(info.Size())
		}

		usages[i] = ShardUsage{
			ShardIndex: i,
			FilePath:   shard.file.Name(),
			FileSize:   fileSize,
			FreeOffset: freeOffset,
			MaxSize:    maxSize,
		}

		shard.closeRefDecr()
	}

	return usages
}

// ActiveUsage returns precise usage statistics by scanning all active records.
func (s *ShardedDB) ActiveUsage() []ShardUsage {
	usages := make([]ShardUsage, len(s.shards))

	for i := range s.shards {
		shard := (*DB)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[i]))))
		if shard == nil {
			continue
		}

		shard.incRef()

		if atomic.LoadUint32(&shard.closeOnce) != 0 {
			shard.closeRefDecr()
			continue
		}

		var keys uint64
		var dataBytes uint64

		_ = shard.ForEachZeroAlloc(func(key []byte, value []byte) error {
			keys++
			dataBytes += uint64(len(key)) + uint64(len(value))
			return nil
		})

		freeOffset := atomic.LoadUint64(&shard.header.FreeOffset)
		maxSize := atomic.LoadUint64(&shard.header.MaxSize)

		var fileSize uint64
		info, err := os.Stat(shard.file.Name())
		if err == nil {
			fileSize = uint64(info.Size())
		}

		usages[i] = ShardUsage{
			ShardIndex:      i,
			FilePath:        shard.file.Name(),
			FileSize:        fileSize,
			FreeOffset:      freeOffset,
			MaxSize:         maxSize,
			ActiveKeys:      keys,
			ActiveDataBytes: dataBytes,
		}

		shard.closeRefDecr()
	}

	return usages
}
