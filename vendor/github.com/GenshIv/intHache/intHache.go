// Package hash64 provides a high-performance 64-bit non-cryptographic hashing engine.
// It supports two operational modes: Stateless standalone execution (Mode 2)
// and Stateful sharded pipeline execution with map rotation (Mode 1).
package intHache

import (
	"encoding/binary"
	"math/bits"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
	NumShards          = 256
	ShardMask          = NumShards - 1
)

// ============================================================================
// MODE 2: Standalone Stateless Functions (Zero-Memory Footprint)
// ============================================================================

type Key128 [2]uint64

func Sum128(data []byte) Key128 {
	const (
		seed1 uint64 = 0x243f6a8885a308d3
		seed2 uint64 = 0x13198a2e03707344

		c1 uint64 = 0x9e3779b185ebca87
		c2 uint64 = 0xc2b2ae3d27d4eb4f
	)

	h1 := seed1
	h2 := seed2

	n := len(data)

	// 16 bytes per iteration.
	for len(data) >= 16 {
		a := binary.LittleEndian.Uint64(data)
		b := binary.LittleEndian.Uint64(data[8:])

		// First stream.
		h1 ^= a
		h1 *= c1
		h1 = bits.RotateLeft64(h1, 27)

		h1 ^= b
		h1 *= c2
		h1 = bits.RotateLeft64(h1, 31)

		// Second stream deliberately mixes in the blocks
		// in the opposite order and uses different constants.
		h2 ^= b
		h2 *= c2
		h2 = bits.RotateLeft64(h2, 29)

		h2 ^= a
		h2 *= c1
		h2 = bits.RotateLeft64(h2, 33)

		data = data[16:]
	}

	// 8-byte tail.
	if len(data) >= 8 {
		v := binary.LittleEndian.Uint64(data)

		h1 ^= v
		h1 *= c1
		h1 = bits.RotateLeft64(h1, 27)

		h2 ^= v
		h2 *= c2
		h2 = bits.RotateLeft64(h2, 31)

		data = data[8:]
	}

	// Remaining 0..7 bytes.
	if len(data) != 0 {
		var v uint64

		for i, b := range data {
			v |= uint64(b) << (8 * i)
		}

		h1 ^= v
		h1 *= c2
		h1 = bits.RotateLeft64(h1, 31)

		h2 ^= v
		h2 *= c1
		h2 = bits.RotateLeft64(h2, 27)
	}

	// Length is important, especially for inputs with common prefixes.
	h1 ^= uint64(n) * c1
	h2 ^= uint64(n) * c2

	// Final avalanche #1.
	h1 ^= h1 >> 33
	h1 *= 0xff51afd7ed558ccd
	h1 ^= h1 >> 33
	h1 *= 0xc4ceb9fe1a85ec53
	h1 ^= h1 >> 33

	// Final avalanche #2, deliberately different.
	h2 ^= h2 >> 29
	h2 *= 0x165667919e3779f9
	h2 ^= h2 >> 32
	h2 *= 0x85ebca77c2b2ae63
	h2 ^= h2 >> 29

	// Cross-mix the two states once at the end.
	h1 ^= bits.RotateLeft64(h2, 17)
	h2 ^= bits.RotateLeft64(h1, 41)

	return Key128{h1, h2}
}

// Sum computes the int64 hash of a byte slice using block-wise binary mixing.
// It runs completely on the stack and holds zero persistent memory.
func Sum(data []byte) int64 {
	ln := len(data)
	if ln == 0 {
		return 0
	}

	hash := fnvOffset64
	idx := 0

	// Process 8-byte blocks simultaneously
	for idx+8 <= ln {
		block := *(*uint64)(unsafe.Pointer(&data[idx]))
		hash ^= block
		hash *= fnvPrime64
		hash = (hash << 13) | (hash >> 51) // ROL 13 rotation
		idx += 8
	}

	// Process remaining bytes
	for idx < ln {
		hash ^= uint64(data[idx])
		hash *= fnvPrime64
		idx++
	}

	// Finalizer avalanche mix
	hash ^= hash >> 33
	hash *= 0xff51afd7ed558ccd
	hash ^= hash >> 33

	return int64(hash)
}

func SumLite(data []byte) uint64 {
	const (
		seed uint64 = 0x9e3779b185ebca87
		mix  uint64 = 0xc2b2ae3d27d4eb4f
	)

	h := seed
	n := len(data)

	for len(data) >= 8 {
		v := binary.LittleEndian.Uint64(data)

		v ^= v >> 30
		v *= mix
		v ^= v >> 27

		h ^= v
		h *= 0x9e3779b185ebca87

		data = data[8:]
	}

	var tail uint64
	for i, b := range data {
		tail |= uint64(b) << (8 * i)
	}

	h ^= tail
	h ^= uint64(n) * mix

	// final avalanche
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33

	return h
}

// SumString computes the int64 hash of a string natively without heap allocations.
func SumString(s string) int64 {
	if s == "" {
		return 0
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return Sum(data)
}

// SumString computes the int64 hash of a string natively without heap allocations.
func SumString128(s string) [2]uint64 {
	if s == "" {
		return [2]uint64{}
	}
	data := unsafe.Slice(unsafe.StringData(s), len(s))
	return Sum128(data)
}

// ============================================================================
// MODE 1: Stateful High-Load Pipeline Engine (Infrastructure Mode)
// ============================================================================

type RotatableShard struct {
	sync.Mutex
	current  map[int64]struct{}
	previous map[int64]struct{}
}

// Pipeline encapsulates the concurrent sharded maps, rotation state, and memory pools.
// Allocates memory only when instantiated via NewPipeline().
type Pipeline struct {
	shards        [NumShards]RotatableShard
	maxShardSize  int
	rotationCount int64
	bufferPool    sync.Pool
}

// NewPipeline instantiates Mode 1 infrastructure.
// Memory is isolated inside this instance and can be fully garbage collected.
func NewPipeline(maxShardSize int) *Pipeline {
	p := &Pipeline{
		maxShardSize: maxShardSize,
	}
	for i := 0; i < NumShards; i++ {
		p.shards[i].current = make(map[int64]struct{}, maxShardSize)
		p.shards[i].previous = make(map[int64]struct{}, 0)
	}
	p.bufferPool = sync.Pool{
		New: func() any {
			b := make([]byte, 0, 64)
			return &b
		},
	}
	return p
}

// CheckAndInsert validates hash uniqueness inside the pipeline's sharded memory state.
func (p *Pipeline) CheckAndInsert(hash int64) bool {
	shardIdx := uint64(hash) & ShardMask
	shard := &p.shards[shardIdx]

	shard.Lock()
	defer shard.Unlock()

	if _, exists := shard.current[hash]; exists {
		return false
	}
	if _, exists := shard.previous[hash]; exists {
		return false
	}

	// Trigger sliding window rotation if threshold is breached
	if len(shard.current) >= p.maxShardSize {
		shard.previous = shard.current
		shard.current = make(map[int64]struct{}, p.maxShardSize)
		atomic.AddInt64(&p.rotationCount, 1)
	}

	shard.current[hash] = struct{}{}
	return true
}

// RentBuffer acquires a reusable byte slice buffer from the internal memory pool.
func (p *Pipeline) RentBuffer() *[]byte {
	return p.bufferPool.Get().(*[]byte)
}

// ReturnBuffer releases the byte slice buffer back to the pool to prevent allocations.
func (p *Pipeline) ReturnBuffer(b *[]byte) {
	p.bufferPool.Put(b)
}

// GetRotationCount returns the total number of map rotations executed safely.
func (p *Pipeline) GetRotationCount() int64 {
	return atomic.LoadInt64(&p.rotationCount)
}

// Clear explicitly deallocates all maps immediately, helping the GC free memory instantly.
func (p *Pipeline) Clear() {
	for i := 0; i < NumShards; i++ {
		p.shards[i].Lock()
		p.shards[i].current = nil
		p.shards[i].previous = nil
		p.shards[i].Unlock()
	}
}
