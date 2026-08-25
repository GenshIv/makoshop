package makodb

// DB design notes (hot path, cache-sensitive):
//
// - Get/ForEach are fully lock-free: only atomic loads on mmap data.
// - Put/Delete/resize/Shrink/close use RobustShmMutex (one write lock per shard).
// - No sync.Mutex/RWMutex anywhere; all locks are atomic in shared memory.
// - mmap is fixed-size at Open time; resize only truncates the file.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/GenshIv/intHache"
)

// key128 is a 128-bit hash key represented as two uint64 values
type key128 = intHache.Key128

var (
	ErrKeyNotFound    = errors.New("makodb: key not found")
	ErrOutOfSpace     = errors.New("makodb: out of database space")
	ErrCorrupt        = errors.New("makodb: database file corrupt or invalid magic")
	ErrStopIteration  = errors.New("makodb: stop iteration")   // used to break out of ForEach early
	ErrBufferTooSmall = errors.New("makodb: buffer too small") // used by GetInto when value doesn't fit
)

const (
	dbMagic      = "MAKODB\x00\x00"
	dbVersion    = uint32(1)
	headerOffset = uintptr(0)
)

// dbHeader is mapped at the very beginning of the memory-mapped file.
type dbHeader struct {
	Magic      [8]byte
	Version    uint32
	LockState  uint32 // State for ShmMutex at offset 12
	MaxSize    uint64
	FreeOffset uint64 // Offset where we can append new data/records
	NumBuckets uint64 // Number of buckets in the hash table
}

const headerSize = uint64(unsafe.Sizeof(dbHeader{}))

// bucket represents a hash table slot or a chained collision node in mmap.
// Key length is always 16 bytes (key128), so KeyLen is not stored.
type bucket struct {
	Hash       key128
	KeyOffset  uint64
	ValOffset  uint64
	ValLen     uint32
	NextOffset uint64 // Offset of next bucket in the chain (0 if none)
}

const bucketSize = uint64(unsafe.Sizeof(bucket{}))

// DB represents the memory-mapped JSON document database.
type DB struct {
	file     *os.File
	mapped   []byte // fixed-size slice for entire mmap region
	header   *dbHeader
	mutex    *RobustShmMutex
	isClosed bool

	// refCount is used for safe shard replacement during compaction.
	// Incremented when a reader acquires the shard, decremented when done.
	// Shard is closed only when refCount reaches 0.
	refCount uint64

	// closeOnce is an atomic flag to ensure Close() is called exactly once.
	// 0 = not closed, 1 = closed. Checked before decrementing refCount.
	closeOnce uint32
}

// Open opens an existing DB file or creates a new one.
// path is the database file path.
// maxSize is the maximum size the database file can grow to.
// numBuckets is the number of hash table slots (should be set on creation).
func Open(path string, maxSize uint64, numBuckets uint64) (*DB, error) {
	return openDB(path, maxSize, numBuckets, false)
}

// openDB opens a DB with optional vacuum skip.
func openDB(path string, maxSize uint64, numBuckets uint64, skipVacuum bool) (*DB, error) {
	if numBuckets == 0 {
		numBuckets = 65536 // Default bucket count
	}

	// Calculate minimum required size to hold header + hash table
	minRequiredSize := headerSize + numBuckets*bucketSize
	if maxSize < minRequiredSize {
		return nil, fmt.Errorf("makodb: maxSize (%d) must be at least %d to fit header and index", maxSize, minRequiredSize)
	}

	if !skipVacuum {
		// Attempt transparent vacuum/compaction on startup if database exists
		_ = vacuumFile(path, maxSize, numBuckets)
	}

	// Open or create file, truncate to maxSize
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	// Truncate to maxSize to ensure we have the full region
	if err := f.Truncate(int64(maxSize)); err != nil {
		f.Close()
		return nil, err
	}

	// mmap the entire maxSize region
	mapped, err := mmapSyscall(f, int(maxSize))
	if err != nil {
		f.Close()
		return nil, err
	}

	header := (*dbHeader)(unsafe.Pointer(&mapped[0]))
	mutex := NewRobustShmMutex(mapped, 12) // LockState is at offset 12

	// Safely initialize the DB if it is brand new
	if string(header.Magic[:8]) != dbMagic {
		mutex.Lock()
		if string(header.Magic[:8]) != dbMagic {
			copy(header.Magic[:8], dbMagic)
			header.Version = dbVersion
			header.MaxSize = maxSize
			header.NumBuckets = numBuckets
			// Free area starts directly after the hash table buckets
			header.FreeOffset = minRequiredSize
		}
		mutex.Unlock()
	} else {
		// Sync MaxSize with the open parameters of the existing database
		mutex.Lock()
		header.MaxSize = maxSize
		mutex.Unlock()
	}

	// Basic validation
	if header.Version != dbVersion {
		_ = munmapSyscall(mapped)
		f.Close()
		return nil, ErrCorrupt
	}

	db := &DB{
		file:     f,
		mapped:   mapped,
		header:   header,
		mutex:    mutex,
		refCount: 1, // Initial reference for the owner (ShardedDB)
	}

	// Apply memory mapping advice:
	// - MADV_RANDOM on the whole region: access pattern is random.
	// - MADV_DONTNEED on the tail [FreeOffset, MaxSize): release unused pages.
	_ = madviseRandomSyscall(mapped)
	db.applyTailDontNeedLocked()

	return db, nil
}

func vacuumFile(path string, maxSize uint64, numBuckets uint64) error {
	// If file doesn't exist, nothing to vacuum
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	// Open the existing database safely
	oldDB, err := openDB(path, maxSize, numBuckets, true)
	if err != nil {
		return err
	}

	// Create temporary file path
	tempPath := path + ".vacuum_temp"
	os.Remove(tempPath)

	// Open temporary database
	tempDB, err := openDB(tempPath, maxSize, numBuckets, true)
	if err != nil {
		oldDB.close()
		return err
	}

	// Copy all active records from old to temp
	// Read 16-byte key128 keys and use putKey128 to preserve turbo indexes
	err = oldDB.ForEachZeroAlloc(func(keyBytes []byte, value []byte) error {
		if len(keyBytes) != 16 {
			return nil // Skip invalid keys
		}
		// keyBytes is already in the correct format (16 bytes, LittleEndian)
		// We can directly create key128 from it
		var k key128
		k[0] = binary.LittleEndian.Uint64(keyBytes[0:8])
		k[1] = binary.LittleEndian.Uint64(keyBytes[8:16])
		return tempDB.putKey128(k, value)
	})

	// Sync temp DB to ensure all data is written before closing
	if err := tempDB.Sync(); err != nil {
		oldDB.close()
		tempDB.close()
		os.Remove(tempPath)
		return err
	}

	// Close both databases to release OS file locks
	oldDB.close()
	tempDB.close()

	if err != nil {
		os.Remove(tempPath)
		return err
	}

	// Replace old file with compacted temp file.
	// If this fails (e.g. file locked by another process), we fallback to using the old file as is.
	if err := os.Remove(path); err != nil {
		os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}

	return nil
}

func (db *DB) IsClosed() bool {
	return db.isClosed
}

// Close closes the database. For external callers, this safely decrements
// the reference count and closes the DB when no other users remain.
func (db *DB) Close() error {
	if db.closeRefDecr() {
		return nil
	}
	// If refCount didn't reach 0, the DB is still in use.
	// Wait briefly for other users to finish.
	for i := 0; i < 1000 && atomic.LoadUint64(&db.refCount) > 0; i++ {
		runtime.Gosched()
	}
	// Try to close again.
	if db.closeRefDecr() {
		return nil
	}
	// If still in use, return error instead of force-closing.
	return fmt.Errorf("makodb: database is still in use")
}

// close closes the database resources. Private: only called after closeOnce is set
// and refCount has reached 0. Acquires write lock to ensure exclusive access.
func (db *DB) close() error {
	db.mutex.Lock()

	var errs []error
	usedSpace := db.header.FreeOffset

	// Sync mmap to ensure all data is written to disk BEFORE unmapping
	if err := msyncSyscall(db.mapped); err != nil {
		errs = append(errs, fmt.Errorf("failed to sync mmap: %w", err))
	}

	// Sync file to ensure all metadata is written
	if err := db.file.Sync(); err != nil {
		errs = append(errs, fmt.Errorf("failed to sync file: %w", err))
	}

	// Release RobustShmMutex BEFORE unmapping, since it lives in mmap.
	db.mutex.Unlock()

	if err := munmapSyscall(db.mapped); err != nil {
		errs = append(errs, fmt.Errorf("failed to unmap: %w", err))
	}

	if usedSpace > 0 {
		if err := db.file.Truncate(int64(usedSpace)); err != nil {
			errs = append(errs, fmt.Errorf("failed to shrink file on close: %w", err))
		}
	}

	if err := db.file.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close file: %w", err))
	}

	db.isClosed = true

	if len(errs) > 0 {
		return fmt.Errorf("makodb: errors during close: %v", errs)
	}
	return nil
}

// Sync flushes all pending writes to disk.
// Call this after important writes to ensure data persistence.
func (db *DB) Sync() error {
	if db.isClosed {
		return fmt.Errorf("makodb: database is closed")
	}

	// Sync the mmap region
	if err := msyncSyscall(db.mapped); err != nil {
		return fmt.Errorf("makodb: failed to sync mmap: %w", err)
	}

	// Sync the file
	if err := db.file.Sync(); err != nil {
		return fmt.Errorf("makodb: failed to sync file: %w", err)
	}

	return nil
}

// closeRefDecr decrements refCount and closes the DB if it reaches 0.
func (db *DB) closeRefDecr() bool {
	// If already closed, do nothing.
	if atomic.LoadUint32(&db.closeOnce) != 0 {
		return false
	}

	// Decrement refCount.
	newRef := atomic.AddUint64(&db.refCount, ^uint64(0))
	if newRef != 0 {
		return false
	}

	// We are the one who brought refCount to 0.
	// Try to become the closer.
	if !atomic.CompareAndSwapUint32(&db.closeOnce, 0, 1) {
		return false
	}

	_ = db.close()
	return true
}

// incRef increments the reference count for safe concurrent access.
func (db *DB) incRef() {
	atomic.AddUint64(&db.refCount, 1)
}

// resize grows the database by updating MaxSize in header.
// The file is truncated to newSize (must be <= original maxSize).
// No Unmap/Remap: mapping stays the same, so mutex.Lock() always sees valid memory.
// Caller must hold write lock (mutex.Lock()).
func (db *DB) resize(newSize uint64) error {
	if newSize > uint64(len(db.mapped)) {
		return ErrOutOfSpace
	}

	if err := db.file.Truncate(int64(newSize)); err != nil {
		return fmt.Errorf("makodb: failed to truncate file: %w", err)
	}

	db.header.MaxSize = newSize

	// Release memory for the newly unused tail.
	db.applyTailDontNeedLocked()

	return nil
}

// Shrink truncates the database file to its actual used space.
// No Unmap/Remap: mapping stays the same, so mutex.Lock() always sees valid memory.
// Caller must hold write lock.
func (db *DB) Shrink() error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	usedSpace := db.header.FreeOffset
	if usedSpace == 0 {
		return nil
	}

	// Truncate file to used space; mapping remains valid.
	if err := db.file.Truncate(int64(usedSpace)); err != nil {
		return fmt.Errorf("makodb: failed to truncate file: %w", err)
	}

	// Update MaxSize in header; db.mapped slice header does not change.
	db.header.MaxSize = usedSpace

	// Release memory for pages beyond the new used space.
	db.applyTailDontNeedLocked()

	return nil
}

// applyTailDontNeedLocked releases memory for the unused tail of the mapping.
// Caller must hold the write lock.
func (db *DB) applyTailDontNeedLocked() {
	freeOffset := atomic.LoadUint64(&db.header.FreeOffset)
	maxSize := atomic.LoadUint64(&db.header.MaxSize)
	if freeOffset >= maxSize || freeOffset >= uint64(len(db.mapped)) {
		return
	}
	tail := db.mapped[freeOffset:maxSize]
	_ = madviseDontNeedSyscall(tail)
}

// hashKey calculates hash of a key using intHache.
func hashKey(key string) key128 {
	return intHache.SumString128(key)
}

// hashKeyBytes computes hash of a key ([]byte). Used internally for zero-allocation lookups.
func hashKeyBytes(key []byte) key128 {
	return intHache.Sum128(key)
}

// ============================================================================
// key128-based internal methods for DB
// All internal operations should use these methods instead of the string-based ones.
// ============================================================================

// putKey128 writes or updates a key-value pair using key128 as the key.
// This is the internal version - public Put(string, []byte) should NOT call this directly.
// This method stores the key128 as a 16-byte binary key.
func (db *DB) putKey128(key key128, value []byte) error {
	valLen := uint32(len(value))

	db.mutex.Lock()

	// Check if DB is closed.
	if atomic.LoadUint32(&db.closeOnce) != 0 {
		db.mutex.Unlock()
		return fmt.Errorf("makodb: database is closed")
	}

	// 1. Check if the key already exists to update it
	bucketIdx := key[0] % db.header.NumBuckets
	rootBucketOffset := headerSize + bucketIdx*bucketSize
	b := (*bucket)(unsafe.Pointer(&db.mapped[rootBucketOffset]))

	var existingBucket *bucket
	if b.KeyOffset != 0 {
		curr := b
		for {
			if curr.Hash == key {
				existingBucket = curr
				break
			}
			if curr.NextOffset == 0 {
				break
			}

			// Validate NextOffset to prevent out-of-bounds access
			if curr.NextOffset >= uint64(len(db.mapped)) {
				// Corrupted NextOffset, stop search
				break
			}
			curr = (*bucket)(unsafe.Pointer(&db.mapped[curr.NextOffset]))
		}
	}

	// 2. If it exists, try in-place update or append new value
	if existingBucket != nil {
		oldValLen := atomic.LoadUint32(&existingBucket.ValLen)
		oldValOffset := atomic.LoadUint64(&existingBucket.ValOffset)

		// Check if we can do in-place update
		// Case 1: Same size - always can overwrite in place
		if uint32(len(value)) == oldValLen {
			// Same size - can overwrite in place
			copy(db.mapped[oldValOffset:oldValOffset+uint64(len(value))], value)
			// ValOffset and ValLen remain the same
			db.mutex.Unlock()
			return nil
		}

		// Case 2: Last record - can overwrite in place only if same size
		// (Different size requires new allocation to avoid race conditions)

		// Case 3: Not last record and different size - need to allocate new space
		required := uint64(valLen)
		if db.header.FreeOffset+required > db.header.MaxSize {
			newSize := db.header.MaxSize * 2
			if newSize < db.header.FreeOffset+required {
				newSize = db.header.FreeOffset + required
			}
			if newSize > uint64(len(db.mapped)) {
				db.mutex.Unlock()
				return ErrOutOfSpace
			}
			if err := db.resize(newSize); err != nil {
				db.mutex.Unlock()
				return err
			}
		}

		// Write new value
		newValOffset := db.header.FreeOffset
		copy(db.mapped[newValOffset:], value)
		atomic.StoreUint64(&existingBucket.ValOffset, newValOffset)
		atomic.StoreUint32(&existingBucket.ValLen, valLen)

		db.header.FreeOffset += required
		db.mutex.Unlock()
		return nil
	}

	// 3. Create new entry
	// Check if we need an overflow bucket
	needNewNode := b.KeyOffset != 0
	required := 16 + uint64(valLen) // key128 is always 16 bytes
	if needNewNode {
		required += bucketSize
	}

	if db.header.FreeOffset+required > db.header.MaxSize {
		newSize := db.header.MaxSize * 2
		if newSize < db.header.FreeOffset+required {
			newSize = db.header.FreeOffset + required
		}
		if newSize > uint64(len(db.mapped)) {
			db.mutex.Unlock()
			return ErrOutOfSpace
		}
		if err := db.resize(newSize); err != nil {
			db.mutex.Unlock()
			return err
		}
	}

	// Write key bytes (16 bytes for key128)
	var keyOffset uint64
	if needNewNode {
		// Overflow bucket comes first, then key/value data
		keyOffset = db.header.FreeOffset + bucketSize
	} else {
		keyOffset = db.header.FreeOffset
	}
	keyBytes := db.mapped[keyOffset : keyOffset+16]
	binary.LittleEndian.PutUint64(keyBytes[0:8], key[0])
	binary.LittleEndian.PutUint64(keyBytes[8:16], key[1])

	// Write value bytes
	valOffset := keyOffset + 16
	copy(db.mapped[valOffset:], value)

	// Find insertion point in bucket chain
	if !needNewNode {
		// First entry in bucket - initialize the root bucket directly
		b.Hash = key
		atomic.StoreUint64(&b.KeyOffset, keyOffset)
		atomic.StoreUint64(&b.ValOffset, valOffset)
		atomic.StoreUint32(&b.ValLen, valLen)
		atomic.StoreUint64(&b.NextOffset, 0)
	} else {
		// Append to chain - create overflow bucket
		curr := b
		for {
			next := atomic.LoadUint64(&curr.NextOffset)
			if next == 0 {
				break
			}

			// Validate NextOffset to prevent out-of-bounds access
			if next >= uint64(len(db.mapped)) {
				// Corrupted NextOffset, stop search
				break
			}
			curr = (*bucket)(unsafe.Pointer(&db.mapped[next]))
		}

		// Create new overflow bucket at the beginning of the free space
		newBucketOffset := db.header.FreeOffset
		newBucket := (*bucket)(unsafe.Pointer(&db.mapped[newBucketOffset]))
		newBucket.Hash = key
		atomic.StoreUint64(&newBucket.KeyOffset, keyOffset)
		atomic.StoreUint64(&newBucket.ValOffset, valOffset)
		atomic.StoreUint32(&newBucket.ValLen, valLen)
		atomic.StoreUint64(&newBucket.NextOffset, 0)
		atomic.StoreUint64(&curr.NextOffset, newBucketOffset)
	}

	db.header.FreeOffset += required
	db.mutex.Unlock()
	return nil
}

// getKey128 reads the value for a key128 key from the database.
// This is the internal version - public Get(string) should NOT call this directly.
// Returns ErrKeyNotFound if the key doesn't exist.
func (db *DB) getKey128(key key128) ([]byte, error) {
	// Retry logic for handling concurrent updates
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		bucketIdx := key[0] % db.header.NumBuckets
		rootBucketOffset := headerSize + bucketIdx*bucketSize

		b := (*bucket)(unsafe.Pointer(&db.mapped[rootBucketOffset]))
		if b.KeyOffset == 0 {
			return nil, ErrKeyNotFound
		}

		curr := b
		for {
			if curr.Hash == key {
				kOffset := atomic.LoadUint64(&curr.KeyOffset)
				vOffset := atomic.LoadUint64(&curr.ValOffset)
				vLen := atomic.LoadUint32(&curr.ValLen)

				// Validate offsets to prevent out-of-bounds access
				if kOffset >= uint64(len(db.mapped)) || vOffset >= uint64(len(db.mapped)) {
					// Invalid offsets, retry
					if attempt < maxRetries-1 {
						runtime.Gosched()
						continue
					}
					return nil, ErrKeyNotFound
				}

				// Verify key bytes match
				kBytes := db.mapped[kOffset : kOffset+16]
				var storedKey key128
				storedKey[0] = binary.LittleEndian.Uint64(kBytes[0:8])
				storedKey[1] = binary.LittleEndian.Uint64(kBytes[8:16])
				if storedKey == key {
					// Validate value length and offset
					if vOffset+uint64(vLen) > uint64(len(db.mapped)) {
						// Invalid length or offset, retry
						if attempt < maxRetries-1 {
							runtime.Gosched()
							continue
						}
						return nil, ErrKeyNotFound
					}
					return db.mapped[vOffset : vOffset+uint64(vLen)], nil
				}
			}

			next := atomic.LoadUint64(&curr.NextOffset)
			if next == 0 {
				break
			}

			// Validate next offset to prevent out-of-bounds access
			if next >= uint64(len(db.mapped)) {
				// Corrupted NextOffset, stop iteration
				break
			}
			curr = (*bucket)(unsafe.Pointer(&db.mapped[next]))
		}

		// Key not found in this attempt
		if attempt < maxRetries-1 {
			runtime.Gosched()
			continue
		}
		return nil, ErrKeyNotFound
	}

	return nil, ErrKeyNotFound
}

// getKey128ZeroAlloc reads the value for a key128 key without allocating memory.
// This is the internal version - public GetZeroAlloc(string) should NOT call this directly.
// Returns a direct view into the memory-mapped file.
// WARNING: The returned slice is valid only until any write operation on the same shard.
func (db *DB) getKey128ZeroAlloc(key key128) ([]byte, error) {
	bucketIdx := key[0] % db.header.NumBuckets
	rootBucketOffset := headerSize + bucketIdx*bucketSize

	b := (*bucket)(unsafe.Pointer(&db.mapped[rootBucketOffset]))
	if b.KeyOffset == 0 {
		return nil, ErrKeyNotFound
	}

	curr := b
	for {
		if curr.Hash == key {
			kOffset := atomic.LoadUint64(&curr.KeyOffset)
			vOffset := atomic.LoadUint64(&curr.ValOffset)
			vLen := atomic.LoadUint32(&curr.ValLen)

			// Verify key bytes match
			kBytes := db.mapped[kOffset : kOffset+16]
			var storedKey key128
			storedKey[0] = binary.LittleEndian.Uint64(kBytes[0:8])
			storedKey[1] = binary.LittleEndian.Uint64(kBytes[8:16])
			if storedKey == key {
				return db.mapped[vOffset : vOffset+uint64(vLen)], nil
			}
		}

		next := atomic.LoadUint64(&curr.NextOffset)
		if next == 0 {
			break
		}

		// Validate next offset to prevent out-of-bounds access
		if next >= uint64(len(db.mapped)) {
			// Corrupted NextOffset, stop iteration
			break
		}
		curr = (*bucket)(unsafe.Pointer(&db.mapped[next]))
	}

	return nil, ErrKeyNotFound
}

// deleteKey128 removes a key-value pair using key128 as the key.
// This is the internal version - public Delete(string) should NOT call this directly.
func (db *DB) deleteKey128(key key128) error {
	bucketIdx := key[0] % db.header.NumBuckets
	rootBucketOffset := headerSize + bucketIdx*bucketSize

	b := (*bucket)(unsafe.Pointer(&db.mapped[rootBucketOffset]))
	if b.KeyOffset == 0 {
		return ErrKeyNotFound
	}

	db.mutex.Lock()

	curr := b
	var prev *bucket
	for {
		if curr.Hash == key {
			kOffset := atomic.LoadUint64(&curr.KeyOffset)

			// Verify key bytes match
			kBytes := db.mapped[kOffset : kOffset+16]
			var storedKey key128
			storedKey[0] = binary.LittleEndian.Uint64(kBytes[0:8])
			storedKey[1] = binary.LittleEndian.Uint64(kBytes[8:16])
			if storedKey == key {
				if prev == nil {
					// Remove first entry in bucket
					next := atomic.LoadUint64(&curr.NextOffset)
					if next == 0 {
						// No more entries in chain, clear the bucket
						atomic.StoreUint64(&b.KeyOffset, 0)
						atomic.StoreUint64(&b.ValOffset, 0)
						atomic.StoreUint32(&b.ValLen, 0)
						atomic.StoreUint64(&b.NextOffset, 0)
					} else {
						// Move the next entry to the root position
						nextBucket := (*bucket)(unsafe.Pointer(&db.mapped[next]))
						b.Hash = nextBucket.Hash
						atomic.StoreUint64(&b.KeyOffset, nextBucket.KeyOffset)
						atomic.StoreUint64(&b.ValOffset, nextBucket.ValOffset)
						atomic.StoreUint32(&b.ValLen, nextBucket.ValLen)
						atomic.StoreUint64(&b.NextOffset, nextBucket.NextOffset)
						// Clear the next bucket to prevent duplicate entries
						atomic.StoreUint64(&nextBucket.KeyOffset, 0)
						atomic.StoreUint64(&nextBucket.ValOffset, 0)
						atomic.StoreUint32(&nextBucket.ValLen, 0)
						atomic.StoreUint64(&nextBucket.NextOffset, 0)
					}
				} else {
					// Remove from middle/end of chain
					next := atomic.LoadUint64(&curr.NextOffset)
					atomic.StoreUint64(&prev.NextOffset, next)
				}
				db.mutex.Unlock()
				return nil
			}
		}

		prev = curr
		next := atomic.LoadUint64(&curr.NextOffset)
		if next == 0 {
			break
		}
		curr = (*bucket)(unsafe.Pointer(&db.mapped[next]))
	}

	db.mutex.Unlock()
	return ErrKeyNotFound
}

// ForEachZeroAlloc iterates over all active key-value pairs in the shard (lock-free)
// without allocating memory for keys or values.
func (db *DB) ForEachZeroAlloc(cb func(key []byte, value []byte) error) error {
	for i := uint64(0); i < db.header.NumBuckets; i++ {
		rootBucketOffset := headerSize + i*bucketSize
		b := (*bucket)(unsafe.Pointer(&db.mapped[rootBucketOffset]))
		if b.KeyOffset == 0 {
			continue
		}

		curr := b
		for {
			kOffset := atomic.LoadUint64(&curr.KeyOffset)
			vOffset := atomic.LoadUint64(&curr.ValOffset)
			vLen := atomic.LoadUint32(&curr.ValLen)

			if kOffset != 0 {
				kBytes := db.mapped[kOffset : kOffset+16] // key128 is always 16 bytes
				vBytes := db.mapped[vOffset : vOffset+uint64(vLen)]

				if err := cb(kBytes, vBytes); err != nil {
					return err
				}
			}

			next := atomic.LoadUint64(&curr.NextOffset)
			if next == 0 {
				break
			}
			curr = (*bucket)(unsafe.Pointer(&db.mapped[next]))
		}
	}
	return nil
}
