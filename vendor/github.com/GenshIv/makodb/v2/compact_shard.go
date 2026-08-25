package makodb

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// CompactState holds compaction state for one shard at a time.
type CompactState struct {
	shardIdx     int32 // -1 = none, >=0 = shard being compacted
	compactShard *DB   // new shard being built (set once, never changed)
	active       int32 // 0 = inactive, 1 = active (copying), 2 = done (ready to swap)
	bucketIdx    int   // current bucket index being scanned
	nextOffset   int   // offset of next bucket in chain (-1 means start from root)
}

// StartCompactShard begins compaction for the shard at the given index.
func (s *ShardedDB) StartCompactShard(shardIdx int) error {
	if shardIdx < 0 || shardIdx >= s.numShards {
		return fmt.Errorf("makodb: invalid shard index %d", shardIdx)
	}

	cs := &s.compact

	if atomic.LoadInt32(&cs.active) != 0 {
		return fmt.Errorf("makodb: compaction already in progress")
	}

	shard := s.shards[shardIdx]

	compactPath := shard.file.Name() + ".compact"
	os.Remove(compactPath)

	maxSize := shard.header.MaxSize
	numBuckets := shard.header.NumBuckets
	compactDB, err := openDB(compactPath, maxSize, numBuckets, true)
	if err != nil {
		return fmt.Errorf("makodb: failed to create compact shard: %w", err)
	}

	cs.shardIdx = int32(shardIdx)
	cs.compactShard = compactDB
	cs.bucketIdx = 0
	cs.nextOffset = -1

	atomic.StoreInt32(&cs.active, 1)

	return nil
}

// CompactShardStep performs one step of compaction.
func (s *ShardedDB) CompactShardStep(maxRecords int) (copied int, done bool, err error) {
	cs := &s.compact

	if atomic.LoadInt32(&cs.active) == 0 {
		return 0, false, fmt.Errorf("makodb: no compaction in progress")
	}

	shardIdx := int(cs.shardIdx)
	shard := s.shards[shardIdx]
	compactDB := cs.compactShard

	numBuckets := int(shard.header.NumBuckets)
	bi := cs.bucketIdx
	nextOff := cs.nextOffset

	count := 0
	for bi < numBuckets && count < maxRecords {
		if nextOff == -1 {
			nextOff = int(headerSize + uint64(bi)*bucketSize)
		}

		for nextOff != -1 && count < maxRecords {
			b := (*bucket)(unsafe.Pointer(&shard.mapped[uint64(nextOff)]))

			if b.KeyOffset == 0 {
				bi++
				nextOff = -1
				continue
			}

			kOff := atomic.LoadUint64(&b.KeyOffset)

			// Read key128 (always 16 bytes)
			var k key128
			k[0] = binary.LittleEndian.Uint64(shard.mapped[kOff : kOff+8])
			k[1] = binary.LittleEndian.Uint64(shard.mapped[kOff+8 : kOff+16])

			vOff := atomic.LoadUint64(&b.ValOffset)
			vLen := atomic.LoadUint32(&b.ValLen)
			value := shard.mapped[vOff : vOff+uint64(vLen)]

			if err := compactDB.putKey128(k, value); err != nil {
				cs.bucketIdx = bi
				cs.nextOffset = nextOff
				return count, false, err
			}
			count++

			n := atomic.LoadUint64(&b.NextOffset)
			if n == 0 {
				bi++
				nextOff = -1
			} else {
				nextOff = int(n)
			}
		}
	}

	cs.bucketIdx = bi
	cs.nextOffset = nextOff

	done = bi >= numBuckets
	if done {
		atomic.StoreInt32(&cs.active, 2)
	}

	return count, done, nil
}

// FinishCompactShard completes compaction by swapping the compact shard with the original.
func (s *ShardedDB) FinishCompactShard() error {
	cs := &s.compact

	active := atomic.LoadInt32(&cs.active)
	if active == 0 {
		return fmt.Errorf("makodb: no compaction in progress")
	}
	if active != 2 {
		return fmt.Errorf("makodb: compaction not finished")
	}

	shardIdx := int(cs.shardIdx)
	oldShard := s.shards[shardIdx]
	compactDB := cs.compactShard

	originalPath := oldShard.file.Name()
	maxSize := oldShard.header.MaxSize
	numBuckets := oldShard.header.NumBuckets

	if err := compactDB.close(); err != nil {
		return fmt.Errorf("makodb: failed to close compact shard: %w", err)
	}

	if err := os.Rename(compactDB.file.Name(), originalPath); err != nil {
		return fmt.Errorf("makodb: failed to rename compact shard: %w", err)
	}

	newShard, err := openDB(originalPath, maxSize, numBuckets, true)
	if err != nil {
		return fmt.Errorf("makodb: failed to reopen compacted shard: %w", err)
	}

	newShard.incRef()

	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&s.shards[shardIdx])), unsafe.Pointer(newShard))

	oldShard.closeRefDecr()

	for atomic.LoadUint64(&oldShard.refCount) > 0 {
		runtime.Gosched()
	}

	if atomic.CompareAndSwapUint32(&oldShard.closeOnce, 0, 1) {
		_ = oldShard.close()
	}

	newShard.closeRefDecr()

	cs.shardIdx = -1
	cs.compactShard = nil
	cs.bucketIdx = 0
	cs.nextOffset = -1
	atomic.StoreInt32(&cs.active, 0)

	return nil
}

// isCompacting returns true if compaction is active.
func (s *ShardedDB) isCompacting() bool {
	return atomic.LoadInt32(&s.compact.active) != 0
}

// getCompactShard returns the compact shard, or nil if not compacting.
func (s *ShardedDB) getCompactShard() (*DB, int) {
	cs := &s.compact
	if !s.isCompacting() {
		return nil, -1
	}
	return cs.compactShard, int(cs.shardIdx)
}

// CompactAllShards compacts all shards one by one.
func (s *ShardedDB) CompactAllShards(maxRecordsPerStep int) error {
	for shardIdx := 0; shardIdx < s.numShards; shardIdx++ {
		if err := s.StartCompactShard(shardIdx); err != nil {
			return err
		}

		for {
			_, done, err := s.CompactShardStep(maxRecordsPerStep)
			if err != nil {
				return err
			}
			if done {
				break
			}
		}

		if err := s.FinishCompactShard(); err != nil {
			return err
		}
	}
	return nil
}
