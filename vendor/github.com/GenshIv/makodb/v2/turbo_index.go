package makodb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Turbo index stores document IDs as raw key128 tokens in a contiguous stream.
// No separators, no metadata, no string keys — just pure key128 values.
//
// Layout in memory (stored as value under hashed key):
// [count: uint64][tokens: key128 x count]
//
// All keys are hashed to key128 and converted to string format "<high>_<low>"
//
// All turbo indexes are guaranteed to be sorted in ascending order.
// Sorting is done via MSB-first Radix Sort (base-256) on key128.
//
// Operations are optimized for:
// - Atomic 16-byte reads/writes
// - Direct key128 comparison (no string parsing)
// - Cache-friendly sequential access
// - Fast merge-style intersection on sorted data
// - Binary search on raw bytes
//
// Trade-offs:
// - You cannot reverse-lookup what a key128 means without external mapping
// - No built-in ordering or metadata
// - Best for high-performance filtering and intersection
//
// No mutexes, no channels, no goroutines — only DB Get/Put/Delete + atomic ops.

var (
	ErrTurboCorrupt = errors.New("makodb: turbo index data corrupt")
)

const (
	// Key sizes for binary representation
	key128BinarySize = 16 // Size of key128 in binary form (2 * 8 bytes)

	// Header and entry sizes
	turboHeaderSize          = 8  // size of count field in bytes
	turboIndexEntrySize      = 16 // key128 docID size in bytes
	turboNumSortEntrySize    = 24 // value(8) + docID(16)
	turboNumSortRevEntrySize = 24 // docID(16) + value(8)
	turboSortPosEntrySize    = 24 // docID(16) + position(8)
)

// hashToken converts a string token to key128 for use as database key.
// This is used internally to convert user-provided string tokens to internal key128 representation.
// Uses turboKey to add "turbo_idx:" prefix for consistency with TurboPutIndex.
func hashToken(token string) key128 {
	return turboKey(token)
}

// hashDocID converts a string document ID to key128 using hash function.
// This is used internally to convert user-provided string IDs to internal key128 representation.
// Matches turboKey format: documents and indexes both use "turbo_idx:" prefix internally.
func hashDocID(docID string) key128 {
	return turboKey(docID)
}

// hashDocIDSlice converts a slice of string document IDs to key128 slice.
func hashDocIDSlice(docIDs []string) []key128 {
	if len(docIDs) == 0 {
		return nil
	}
	result := make([]key128, len(docIDs))
	for i, docID := range docIDs {
		result[i] = hashDocID(docID)
	}
	return result
}

// hashTokenSlice converts a slice of string tokens to key128 slice.
func hashTokenSlice(tokens []string) []key128 {
	if len(tokens) == 0 {
		return nil
	}
	result := make([]key128, len(tokens))
	for i, token := range tokens {
		result[i] = hashToken(token)
	}
	return result
}

// toKey128 converts an any value to key128.
// Supports: string, uint64, key128, intHache.Key128, []byte (16 bytes)
func toKey128(v any) key128 {
	switch val := v.(type) {
	case string:
		return hashDocID(val)
	case uint64:
		return hashKey(fmt.Sprintf("doc:%d", val))
	case key128:
		return val
	case []byte:
		if len(val) == 16 {
			return binaryTokey128(val)
		}
		return key128{}
	default:
		// Try to convert to string
		if s, ok := val.(fmt.Stringer); ok {
			return hashDocID(s.String())
		}
		return key128{}
	}
}

// toKey128Slice converts a slice of any values to key128 slice.
func toKey128Slice(v any) []key128 {
	if v == nil {
		return nil
	}
	switch vals := v.(type) {
	case []any:
		result := make([]key128, len(vals))
		for i, val := range vals {
			result[i] = toKey128(val)
		}
		return result
	case []string:
		return hashDocIDSlice(vals)
	case []key128:
		return vals
	case []uint64:
		result := make([]key128, len(vals))
		for i, val := range vals {
			result[i] = toKey128(val)
		}
		return result
	default:
		// Try reflection for slices
		return nil
	}
}

// key128ToAnySlice converts a slice of key128 to []any.
// This is used to return internal key128 values as any to public APIs.
func key128ToAnySlice(tokens []key128) []any {
	if tokens == nil {
		return nil
	}
	result := make([]any, len(tokens))
	for i, t := range tokens {
		result[i] = t
	}
	return result
}

// key128ToBinary converts a key128 to its binary representation.
// Returns a 16-byte slice: first 8 bytes are high uint64, next 8 bytes are low uint64.
func key128ToBinary(k key128) []byte {
	buf := make([]byte, key128BinarySize)
	binary.LittleEndian.PutUint64(buf[0:8], k[0])
	binary.LittleEndian.PutUint64(buf[8:16], k[1])
	return buf
}

// binaryTokey128 converts a 16-byte binary representation to key128.
// Returns zero key128 if the slice is not exactly 16 bytes.
func binaryTokey128(b []byte) key128 {
	var k key128
	if len(b) != key128BinarySize {
		return k
	}
	k[0] = binary.LittleEndian.Uint64(b[0:8])
	k[1] = binary.LittleEndian.Uint64(b[8:16])
	return k
}

// ---- Turbo key helpers (all use hashed key128 as database keys) ----

// turboKey builds a turbo index key from a token by hashing it.
// The token string is hashed with a type prefix to key128.
func turboKey(token string) key128 {
	return hashKey("turbo_idx:" + token)
}

// turboSortKey builds a turbo sort index key from a name by hashing it.
func turboSortKey(name string) key128 {
	return hashKey("turbo_sort:" + name)
}

// turboSortPosKey builds a turbo sort position index key from a name by hashing it.
func turboSortPosKey(name string) key128 {
	return hashKey("turbo_sort_pos:" + name)
}

// turboDocKey converts a key128 document ID to a key128 key.
// The key128 should already contain any prefix inside the hash.
func turboDocKey(docID key128) key128 {
	return docID
}

// turboNumSortKey builds a turbo numeric sort index key from a name by hashing it.
func turboNumSortKey(name string) key128 {
	return hashKey("turbo_numsort:" + name)
}

// turboNumSortRevKey builds a turbo numeric sort reverse index key from a name by hashing it.
func turboNumSortRevKey(name string) key128 {
	return hashKey("turbo_numsort_rev:" + name)
}

// ---- Top-N intersection helpers ----

// TurboIndexIntersectionResult holds the intersection count for a single turbo index.
type TurboIndexIntersectionResult struct {
	// Key is the original index key this result refers to.
	Key key128
	// Count is the number of common tokens with the query index.
	Count int
}

// TurboTopNByIntersection finds the indexes (from candidateKeys) with the largest
// overlap with the query index (queryKey).
//
// Requirements:
// - queryKey and each key in candidateKeys must point to a valid, sorted turbo index.
// - All turbo indexes are assumed sorted in ascending key128 order.
//
// Behavior:
// - Reads query index once.
// - For each candidate index: merge-style intersection O(|T| + |C_i|).
// - Returns results sorted by Count descending, then by Key ascending for stability.
// - If limit > 0, only the top `limit` results are returned.
//
// No goroutines, no mutexes, no hash sets — only sequential scans and comparisons.
func (s *ShardedDB) turboTopNByIntersection(queryKey key128, candidateKeys []key128, limit int) ([]TurboIndexIntersectionResult, error) {
	if len(candidateKeys) == 0 {
		return nil, nil
	}

	// 1) Read query tokens once.
	qData, err := s.turboGet(queryKey)
	if err != nil {
		return nil, err
	}
	queryTokens := turboIndexTokensSlice(qData)
	if len(queryTokens) == 0 {
		return nil, nil
	}

	// 2) For each candidate index, read its tokens and intersect.
	n := len(candidateKeys)
	res := make([]TurboIndexIntersectionResult, 0, n)

	for _, cKey := range candidateKeys {
		cData, err := s.turboGet(cKey)
		if err != nil {
			// Missing index treated as empty.
			continue
		}
		cTokens := turboIndexTokensSlice(cData)
		if len(cTokens) == 0 {
			continue
		}

		// Merge-style intersection of two sorted key128 slices.
		i, j, cnt := 0, 0, 0
		qLen, cLen := len(queryTokens), len(cTokens)

		for i < qLen && j < cLen {
			q := queryTokens[i]
			c := cTokens[j]

			if q == c {
				cnt++
				i++
				j++
			} else if bytesCompareKey128(q, c) < 0 {
				i++
			} else {
				j++
			}
		}

		if cnt > 0 {
			res = append(res, TurboIndexIntersectionResult{Key: cKey, Count: cnt})
		}
	}

	// 3) Sort results by Count desc, then Key asc for stability.
	sort.Slice(res, func(a, b int) bool {
		if res[a].Count != res[b].Count {
			return res[a].Count > res[b].Count
		}
		return bytesCompareKey128(res[a].Key, res[b].Key) < 0
	})

	// 4) Apply limit if requested.
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}

	return res, nil
}

// turboIndexTokensSlice returns the token slice from a turbo index buffer.
// Turbo indexes are already sorted; no additional sorting is performed.
func turboIndexTokensSlice(data []byte) []key128 {
	if len(data) < int(turboHeaderSize) {
		return nil
	}
	count := int(binary.LittleEndian.Uint64(data))
	if count == 0 {
		return nil
	}
	data = data[turboHeaderSize:]
	if len(data) < count*turboIndexEntrySize {
		return nil
	}
	tokens := make([]key128, count)
	for i := 0; i < count; i++ {
		offset := i * turboIndexEntrySize
		tokens[i][0] = binary.LittleEndian.Uint64(data[offset:])
		tokens[i][1] = binary.LittleEndian.Uint64(data[offset+8:])
	}
	return tokens
}

// ---- Turbo DB operations (isolated from standard Put/Get/Delete) ----

// turboGet reads a value from the turbo layer (copies data).
func (s *ShardedDB) turboGet(key key128) ([]byte, error) {
	return s.getKey128(key)
}

// turboGetZeroAlloc reads a value from the turbo layer without copying.
// Returns a direct view into the memory-mapped file.
// WARNING: The returned slice is valid only until any write operation on the same shard.
// Safe for read-only paths (search, pagination) where no writes occur between Get and use.
func (s *ShardedDB) turboGetZeroAlloc(key key128) ([]byte, error) {
	return s.getKey128ZeroAlloc(key)
}

// turboPut writes a value to the turbo layer.
func (s *ShardedDB) turboPut(key key128, value []byte) error {
	return s.putKey128(key, value)
}

// turboDelete removes a value from the turbo layer.
func (s *ShardedDB) turboDelete(key key128) error {
	return s.deleteKey128(key)
}

// TurboIndexHeader is the header of a turbo index entry.
type TurboIndexHeader struct {
	Count uint64 // number of tokens stored
}

// TurboPutIndex adds a key128 token to a turbo index.
// Returns true if the token was newly added, false if it already existed.
// The index is guaranteed to remain sorted in ascending order.
// Uses in-place insertion into sorted data — no full re-sort needed.
func (s *ShardedDB) turboPutIndex(token key128, docID key128) (bool, error) {
	key := token

	// Read current index data
	val, err := s.turboGet(key)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return false, err
	}

	// Empty index: just write single token
	if len(val) == 0 {
		buf := make([]byte, turboHeaderSize+turboIndexEntrySize)
		binary.LittleEndian.PutUint64(buf, 1)
		binary.LittleEndian.PutUint64(buf[turboHeaderSize:], docID[0])
		binary.LittleEndian.PutUint64(buf[turboHeaderSize+8:], docID[1])
		return true, s.turboPut(key, buf)
	}

	// Parse header
	if len(val) < int(turboHeaderSize) {
		return false, ErrTurboCorrupt
	}
	count := binary.LittleEndian.Uint64(val)
	tokenData := val[turboHeaderSize:]

	// Verify data integrity
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return false, ErrTurboCorrupt
	}

	// Binary search for insertion point
	pos := turboBinarySearchInsertionPoint(tokenData, count, docID)

	// Check if already exists
	if pos >= 0 {
		return false, nil
	}

	// pos is -insertionPoint-1
	insertPos := -pos - 1

	// Create new buffer with one extra token
	newCount := count + 1
	newBuf := make([]byte, turboHeaderSize+newCount*turboIndexEntrySize)
	binary.LittleEndian.PutUint64(newBuf, newCount)

	// Copy tokens before insertion point
	copy(newBuf[turboHeaderSize:], tokenData[:insertPos*turboIndexEntrySize])

	// Write new token
	binary.LittleEndian.PutUint64(newBuf[turboHeaderSize+uint64(insertPos)*turboIndexEntrySize:], docID[0])
	binary.LittleEndian.PutUint64(newBuf[turboHeaderSize+uint64(insertPos)*turboIndexEntrySize+8:], docID[1])

	// Copy tokens after insertion point
	copy(newBuf[turboHeaderSize+uint64(insertPos+1)*turboIndexEntrySize:], tokenData[insertPos*turboIndexEntrySize:])

	return true, s.turboPut(key, newBuf)
}

// TurboDeleteIndex removes a key128 token from a turbo index.
// Returns true if the token was found and removed, false if not found.
// Uses binary search for O(log n) lookup in sorted data.
func (s *ShardedDB) turboDeleteIndex(token key128, docID key128) (bool, error) {
	key := token

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}

	if len(val) == 0 {
		return false, nil
	}

	// Parse header
	if len(val) < int(turboHeaderSize) {
		return false, ErrTurboCorrupt
	}
	count := binary.LittleEndian.Uint64(val)
	tokenData := val[turboHeaderSize:]

	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return false, ErrTurboCorrupt
	}

	// Binary search for the token
	pos := turboBinarySearchInsertionPoint(tokenData, count, docID)

	// Not found
	if pos < 0 {
		return false, nil
	}

	// Last token: just shrink
	if pos == int(count)-1 {
		if count == 1 {
			return true, s.turboDelete(key)
		}
		newCount := count - 1
		newBuf := make([]byte, turboHeaderSize+newCount*turboIndexEntrySize)
		binary.LittleEndian.PutUint64(newBuf, newCount)
		copy(newBuf[turboHeaderSize:], tokenData[:newCount*turboIndexEntrySize])
		return true, s.turboPut(key, newBuf)
	}

	// Middle token: shift remaining tokens left
	newCount := count - 1
	newBuf := make([]byte, turboHeaderSize+newCount*turboIndexEntrySize)
	binary.LittleEndian.PutUint64(newBuf, newCount)

	// Copy tokens before deleted position
	copy(newBuf[turboHeaderSize:], tokenData[:pos*turboIndexEntrySize])

	// Copy tokens after deleted position (shift left by 16 bytes)
	copy(newBuf[turboHeaderSize+uint64(pos)*turboIndexEntrySize:], tokenData[(pos+1)*turboIndexEntrySize:])

	return true, s.turboPut(key, newBuf)
}

// TurboGetIndexTokens returns all key128 tokens for a given index token.
func (s *ShardedDB) turboGetIndexTokens(token string) ([]key128, error) {
	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if len(val) == 0 {
		return nil, nil
	}

	tokens := turboReadTokens(val)
	if tokens == nil {
		return nil, ErrTurboCorrupt
	}

	return tokens, nil
}

// turboContainsIndex checks if a key128 token exists in a turbo index.
// Uses zero-allocation binary scan on raw turbo index data.
func (s *ShardedDB) turboContainsIndex(token key128, docID key128) (bool, error) {
	val, err := s.turboGetZeroAlloc(token)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}

	if len(val) == 0 {
		return false, nil
	}

	// Zero-allocation: scan raw bytes directly
	return TurboBinaryContains(val, docID), nil
}

// TurboCountIndex returns the number of tokens in a turbo index.
func (s *ShardedDB) turboCountIndex(token string) (uint64, error) {
	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return 0, nil
		}
		return 0, err
	}

	if len(val) < int(turboHeaderSize) {
		return 0, ErrTurboCorrupt
	}

	return binary.LittleEndian.Uint64(val), nil
}

// TurboIntersectIndexResults performs multi-index intersection on turbo indexes.
// Returns key128 tokens that are present in ALL specified index conditions.
// Uses merge-style intersection on sorted data — O(n+m) per pair, no nested loops.
func (s *ShardedDB) turboIntersectIndexResults(conditions []TurboIndexCondition) ([]key128, error) {
	if len(conditions) == 0 {
		return nil, nil
	}

	// Get tokens for the first condition
	baseTokens, err := s.turboFilterIndexTokens(conditions[0])
	if err != nil {
		return nil, err
	}
	if len(baseTokens) == 0 {
		return nil, nil
	}

	// Intersect with each subsequent condition using merge-style (sorted)
	current := baseTokens
	for i := 1; i < len(conditions); i++ {
		condTokens, err := s.turboFilterIndexTokens(conditions[i])
		if err != nil {
			return nil, err
		}
		if len(condTokens) == 0 {
			return nil, nil
		}

		// Merge-style intersection on sorted arrays
		current = turboSortedIntersect(current, condTokens)
		if len(current) == 0 {
			return nil, nil
		}
	}

	return current, nil
}

// TurboIndexCondition represents a single turbo index filter for multi-index intersection.
type TurboIndexCondition struct {
	// Index is the index token to query (e.g., "tag:admin", "mako").
	// The method uses turbo indexes internally (prefix "turbo_idx:" added automatically).
	Index string
	// Include specifies an optional whitelist of document IDs as key128.
	// If nil or empty, all tokens for the index are used.
	Include []key128
	// Exclude specifies an optional blacklist of document IDs as key128.
	Exclude []key128
}

// turboFilterIndexTokens retrieves and filters tokens for a single TurboIndexCondition.
func (s *ShardedDB) turboFilterIndexTokens(cond TurboIndexCondition) ([]key128, error) {
	tokens, err := s.turboGetIndexTokens(cond.Index)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	// Apply Include filter using merge-style intersection (both sorted).
	// Sort Include once if needed, then intersect with tokens.
	if len(cond.Include) > 0 {
		include := make([]key128, len(cond.Include))
		copy(include, cond.Include)
		RadixSortKey128(include)

		tokens = turboSortedIntersect(tokens, include)
		if len(tokens) == 0 {
			return nil, nil
		}
	}

	// Apply Exclude filter using merge-style diff (both sorted).
	if len(cond.Exclude) > 0 {
		exclude := make([]key128, len(cond.Exclude))
		copy(exclude, cond.Exclude)
		RadixSortKey128(exclude)

		tokens = turboSortedDiff(tokens, exclude)
	}

	return tokens, nil
}

// TurboSearch performs an AND search across multiple turbo index tokens.
// Returns key128 tokens that are present in ALL specified index tokens.
func (s *ShardedDB) turboSearch(indexTokens []string) ([]key128, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	conditions := make([]TurboIndexCondition, len(indexTokens))
	for i, t := range indexTokens {
		conditions[i] = TurboIndexCondition{Index: t}
	}

	return s.turboIntersectIndexResults(conditions)
}

// TurboPutBatchIndex adds multiple key128 tokens to a turbo index in a single operation.
// Returns the number of newly added tokens.
// The index is guaranteed to remain sorted in ascending order.
func (s *ShardedDB) turboPutBatchIndex(token string, docIDs []key128) (int, error) {
	if len(docIDs) == 0 {
		return 0, nil
	}

	key := turboKey(token)

	// Read current index data
	val, err := s.turboGet(key)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return 0, err
	}

	var existingTokens []key128

	if len(val) > 0 {
		existingTokens = turboReadTokens(val)
		if existingTokens == nil {
			return 0, ErrTurboCorrupt
		}
	}

	// Sort docIDs for merge-style diff
	sortedDocIDs := make([]key128, len(docIDs))
	copy(sortedDocIDs, docIDs)
	RadixSortKey128(sortedDocIDs)

	// Use merge-style to find new tokens (docIDs - existingTokens)
	if len(existingTokens) > 0 {
		// Both are sorted, use merge-style diff
		newTokens := turboSortedDiff(sortedDocIDs, existingTokens)
		added := len(newTokens)
		if added == 0 {
			return 0, nil
		}

		// Merge and write
		allTokens := turboSortedUnion(existingTokens, newTokens)
		buf := turboSerializeKey128(allTokens)
		return added, s.turboPut(key, buf)
	} else {
		// No existing tokens, all are new
		buf := turboSerializeKey128(sortedDocIDs)
		return len(sortedDocIDs), s.turboPut(key, buf)
	}
}

// TurboDeleteBatchIndex removes multiple key128 tokens from a turbo index in a single operation.
// Returns the number of tokens that were actually removed.
func (s *ShardedDB) turboDeleteBatchIndex(token string, docIDs []key128) (int, error) {
	if len(docIDs) == 0 {
		return 0, nil
	}

	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return 0, nil
		}
		return 0, err
	}

	if len(val) == 0 {
		return 0, nil
	}

	tokens := turboReadTokens(val)
	if tokens == nil {
		return 0, ErrTurboCorrupt
	}

	// Use merge-style diff: tokens are already sorted, sort docIDs
	sortedDocIDs := make([]key128, len(docIDs))
	copy(sortedDocIDs, docIDs)
	RadixSortKey128(sortedDocIDs)

	// Merge-style: collect tokens that are NOT in docIDs
	remaining := turboSortedDiff(tokens, sortedDocIDs)
	removed := len(tokens) - len(remaining)

	if removed == 0 {
		return 0, nil
	}

	if len(remaining) == 0 {
		return removed, s.turboDelete(key)
	}

	buf := turboSerializeKey128(remaining)
	return removed, s.turboPut(key, buf)
}

// turboGetIndexTokensFiltered retrieves tokens from a turbo index with optional filtering,
// ordering, and pagination.
func (s *ShardedDB) turboGetIndexTokensFiltered(token string, include []key128, exclude []key128, reverse bool, limit, offset int) ([]key128, error) {
	// Get the initial list of tokens (already sorted from turbo index).
	tokens, err := s.turboGetIndexTokens(token)
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		return nil, nil
	}

	// Apply include filter using merge-style intersection (both sorted).
	// Sort include once if needed, then intersect with tokens.
	var filteredTokens []key128
	if include != nil && len(include) > 0 {
		includeSorted := make([]key128, len(include))
		copy(includeSorted, include)
		RadixSortKey128(includeSorted)

		filteredTokens = turboSortedIntersect(tokens, includeSorted)
		if len(filteredTokens) == 0 {
			return nil, nil
		}
	} else {
		filteredTokens = tokens
	}

	// Apply exclude filter using merge-style diff (both sorted).
	if exclude != nil && len(exclude) > 0 {
		excludeSorted := make([]key128, len(exclude))
		copy(excludeSorted, exclude)
		RadixSortKey128(excludeSorted)

		filteredTokens = turboSortedDiff(filteredTokens, excludeSorted)
		if len(filteredTokens) == 0 {
			return nil, nil
		}
	}

	// Apply reverse order if requested
	if reverse {
		for i, j := 0, len(filteredTokens)-1; i < j; i, j = i+1, j-1 {
			filteredTokens[i], filteredTokens[j] = filteredTokens[j], filteredTokens[i]
		}
	}

	// Handle pagination (limit and offset)
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filteredTokens) {
		return nil, nil
	}

	end := offset + limit
	if end > len(filteredTokens) || limit <= 0 {
		end = len(filteredTokens)
	}

	return filteredTokens[offset:end], nil
}

// TurboClearIndex removes all tokens from a turbo index.
func (s *ShardedDB) turboClearIndex(token string) error {
	return s.turboDelete(turboKey(token))
}

// TurboListTokens returns all tokens for a given index, sorted in ascending order.
func (s *ShardedDB) turboListTokens(token string) ([]key128, error) {
	tokens, err := s.turboGetIndexTokens(token)
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		return nil, nil
	}

	sort.Slice(tokens, func(i, j int) bool {
		return bytesCompareKey128(tokens[i], tokens[j]) < 0
	})

	return tokens, nil
}

// TurboIntersectAll returns the intersection of all tokens across multiple index tokens.
// Equivalent to TurboSearch but with a clearer name for bulk operations.
func (s *ShardedDB) turboIntersectAll(indexTokens []string) ([]key128, error) {
	return s.turboSearch(indexTokens)
}

// TurboUnionAll returns the union of all tokens across multiple index tokens.
// Each token appears only once in the result.
func (s *ShardedDB) turboUnionAll(indexTokens []string) ([]key128, error) {
	return s.turboBulkUnionSorted(indexTokens)
}

// TurboDiff returns tokens that are in the first index but not in any of the others.
func (s *ShardedDB) turboDiff(baseToken string, excludeTokens []string) ([]key128, error) {
	if len(excludeTokens) == 0 {
		return s.turboGetIndexTokens(baseToken)
	}

	baseTokens, err := s.turboGetIndexTokens(baseToken)
	if err != nil {
		return nil, err
	}
	if baseTokens == nil {
		return nil, nil
	}

	// Get union of all exclude tokens using merge-style
	excludeUnion, err := s.turboBulkUnionSorted(excludeTokens)
	if err != nil {
		return nil, err
	}

	// Use merge-style diff: baseTokens - excludeUnion
	if len(excludeUnion) == 0 {
		return baseTokens, nil
	}

	return turboSortedDiff(baseTokens, excludeUnion), nil
}

// TurboIndexStats returns statistics about a turbo index.
type TurboIndexStats struct {
	Token     string
	Count     uint64
	SizeBytes uint64
}

// TurboIndexStats returns statistics about a turbo index.
func (s *ShardedDB) turboIndexStats(token string) (*TurboIndexStats, error) {
	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return &TurboIndexStats{Token: token, Count: 0, SizeBytes: 0}, nil
		}
		return nil, err
	}

	if len(val) < int(turboHeaderSize) {
		return nil, ErrTurboCorrupt
	}

	count := binary.LittleEndian.Uint64(val)

	return &TurboIndexStats{
		Token:     token,
		Count:     count,
		SizeBytes: uint64(len(val)),
	}, nil
}

// TurboCompactIndex removes duplicate tokens from a turbo index and compacts it.
// Returns the number of duplicates removed.
func (s *ShardedDB) turboCompactIndex(token string) (int, error) {
	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return 0, nil
		}
		return 0, err
	}

	if len(val) < int(turboHeaderSize) {
		return 0, ErrTurboCorrupt
	}

	count := binary.LittleEndian.Uint64(val)
	if count == 0 {
		return 0, nil
	}

	tokens := turboReadTokens(val)
	if tokens == nil {
		return 0, ErrTurboCorrupt
	}

	// Sort tokens first (they might not be sorted if written via TurboRawWrite)
	RadixSortKey128(tokens)

	// Deduplicate sorted tokens in O(n) time
	if len(tokens) == 0 {
		return 0, nil
	}
	unique := make([]key128, 0, len(tokens))
	unique = append(unique, tokens[0])
	for i := 1; i < len(tokens); i++ {
		if tokens[i] != tokens[i-1] {
			unique = append(unique, tokens[i])
		}
	}

	removed := len(tokens) - len(unique)
	if removed == 0 {
		return 0, nil
	}

	if len(unique) == 0 {
		return removed, s.turboDelete(key)
	}

	buf := turboSerializeKey128(unique)
	return removed, s.turboPut(key, buf)
}

// TurboBulkIntersect performs intersection on multiple turbo indexes.
// Reads all index data and intersects them efficiently using merge-style.
func (s *ShardedDB) turboBulkIntersect(indexTokens []string) ([]key128, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	// Read all index data
	type indexData struct {
		tokens []key128
		err    error
	}

	results := make([]indexData, len(indexTokens))

	for i, token := range indexTokens {
		tokens, err := s.turboGetIndexTokens(token)
		results[i] = indexData{tokens: tokens, err: err}
	}

	// Check for errors
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		if r.tokens == nil || len(r.tokens) == 0 {
			return nil, nil // Empty index means empty intersection
		}
	}

	// Start with the smallest set for efficiency
	smallestIdx := 0
	smallestLen := len(results[0].tokens)
	for i := 1; i < len(results); i++ {
		if len(results[i].tokens) < smallestLen {
			smallestLen = len(results[i].tokens)
			smallestIdx = i
		}
	}

	current := results[smallestIdx].tokens

	// Intersect with all other sets using merge-style (sorted)
	for i := 0; i < len(results); i++ {
		if i == smallestIdx {
			continue
		}

		other := results[i].tokens
		if len(other) == 0 {
			return nil, nil
		}

		// Merge-style intersection on sorted arrays
		current = turboSortedIntersect(current, other)
		if len(current) == 0 {
			return nil, nil
		}
	}

	return current, nil
}

// TurboIntersectSets performs AND intersection on multiple key128 sets.
// Returns key128 tokens that are present in ALL provided sets.
// Uses merge-style intersection on sorted data — O(n log n) for sorting + O(total) for intersection.
// All input sets are assumed to contain unique elements (no duplicates within a set).
func TurboIntersectSetsAny(sets ...any) []key128 {
	if len(sets) == 0 {
		return nil
	}

	// Check for empty sets
	for _, s := range sets {
		if len(s.([]key128)) == 0 {
			return nil
		}
	}

	if len(sets) == 1 {
		// Return a copy of the single set
		result := make([]key128, len(sets[0].([]key128)))
		r, ok := sets[0].([]key128)
		if !ok {
			return nil
		}
		copy(result, r)
		return result
	}

	// Sort all sets for merge-style intersection
	sortedSets := make([][]key128, len(sets))
	for i, s := range sets {
		sortedSets[i] = make([]key128, len(s.([]key128)))
		copy(sortedSets[i], s.([]key128))
		RadixSortKey128(sortedSets[i])
	}

	// Start with the smallest set for efficiency
	smallestIdx := 0
	smallestLen := len(sortedSets[0])
	for i := 1; i < len(sortedSets); i++ {
		if len(sortedSets[i]) < smallestLen {
			smallestLen = len(sortedSets[i])
			smallestIdx = i
		}
	}

	current := sortedSets[smallestIdx]

	// Intersect with all other sets using merge-style
	for i := 0; i < len(sortedSets); i++ {
		if i == smallestIdx {
			continue
		}

		other := sortedSets[i]
		if len(other) == 0 {
			return nil
		}

		current = turboSortedIntersect(current, other)
		if len(current) == 0 {
			return nil
		}
	}

	return current
}

// TurboIntersectSets performs AND intersection on multiple key128 sets.
// Returns key128 tokens that are present in ALL provided sets.
// Uses merge-style intersection on sorted data — O(n log n) for sorting + O(total) for intersection.
// All input sets are assumed to contain unique elements (no duplicates within a set).
func TurboIntersectSets(sets [][]key128) []key128 {
	if len(sets) == 0 {
		return nil
	}

	// Check for empty sets
	for _, s := range sets {
		if len(s) == 0 {
			return nil
		}
	}

	if len(sets) == 1 {
		// Return a copy of the single set
		result := make([]key128, len(sets[0]))
		copy(result, sets[0])
		return result
	}

	// Sort all sets for merge-style intersection
	sortedSets := make([][]key128, len(sets))
	for i, s := range sets {
		sortedSets[i] = make([]key128, len(s))
		copy(sortedSets[i], s)
		RadixSortKey128(sortedSets[i])
	}

	// Start with the smallest set for efficiency
	smallestIdx := 0
	smallestLen := len(sortedSets[0])
	for i := 1; i < len(sortedSets); i++ {
		if len(sortedSets[i]) < smallestLen {
			smallestLen = len(sortedSets[i])
			smallestIdx = i
		}
	}

	current := sortedSets[smallestIdx]

	// Intersect with all other sets using merge-style
	for i := 0; i < len(sortedSets); i++ {
		if i == smallestIdx {
			continue
		}

		other := sortedSets[i]
		if len(other) == 0 {
			return nil
		}

		current = turboSortedIntersect(current, other)
		if len(current) == 0 {
			return nil
		}
	}

	return current
}

// TurboUnionSets performs OR union on multiple key128 sets.
// Returns all unique key128 tokens from all provided sets.
// Uses merge-style union on sorted data — O(n log n) for sorting + O(total) for union.
func TurboUnionSets(sets [][]key128) []key128 {
	if len(sets) == 0 {
		return nil
	}

	// Collect all tokens
	allTokens := make([]key128, 0)
	for _, s := range sets {
		allTokens = append(allTokens, s...)
	}

	if len(allTokens) == 0 {
		return nil
	}

	// Sort and deduplicate
	RadixSortKey128(allTokens)

	// Deduplicate sorted tokens in O(n) time
	unique := make([]key128, 0, len(allTokens))
	unique = append(unique, allTokens[0])
	for i := 1; i < len(allTokens); i++ {
		if allTokens[i] != allTokens[i-1] {
			unique = append(unique, allTokens[i])
		}
	}

	return unique
}

// turboBulkIntersectRaw intersects multiple indexes and returns raw turbo bitmap.
// Reads raw data, uses TurboBinaryIntersectRaw, no []uint64 allocations.
func (s *ShardedDB) turboBulkIntersectRaw(indexTokens []string) ([]byte, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	// Read raw data for all indexes (zero-alloc: safe because TurboBinaryIntersectRaw
	// creates a new buffer and does not mutate inputs).
	dataSets := make([][]byte, 0, len(indexTokens))
	for _, token := range indexTokens {
		key := turboKey(token)
		data, err := s.turboGetZeroAlloc(key)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil, nil // missing index = empty intersection
			}
			return nil, err
		}
		if len(data) == 0 {
			return nil, nil
		}
		dataSets = append(dataSets, data)
	}

	if len(dataSets) == 0 {
		return nil, nil
	}

	// TurboBinaryIntersectRaw already picks the smallest set internally.
	result := TurboBinaryIntersectRaw(dataSets)
	return result, nil
}

// TurboBulkUnion performs union on multiple turbo indexes.
// Reads all index data and unions them efficiently.
func (s *ShardedDB) turboBulkUnion(indexTokens []string) ([]key128, error) {
	return s.turboBulkUnionSorted(indexTokens)
}

// turboBulkUnionSorted performs union on multiple turbo indexes using merge-style union.
// Much faster than map-based union for large sorted indexes.
// Reads raw data of all indexes and unions them using TurboBinaryUnionRaw.
func (s *ShardedDB) turboBulkUnionSorted(indexTokens []string) ([]key128, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	// Read raw data for all indexes
	dataSets := make([][]byte, 0, len(indexTokens))
	for _, token := range indexTokens {
		key := turboKey(token)
		data, err := s.turboGet(key)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				continue // skip missing indexes
			}
			return nil, err
		}
		if len(data) == 0 {
			continue
		}
		dataSets = append(dataSets, data)
	}

	if len(dataSets) == 0 {
		return nil, nil
	}

	// Merge-style union on sorted data
	unioned := TurboBinaryUnionRaw(dataSets)
	if unioned == nil || len(unioned) == 0 {
		return nil, nil
	}

	return TurboUnsafeReadTokens(unioned), nil
}

// turboBulkUnionSortedRaw is like turboBulkUnionSorted, but returns raw bitmap.
func (s *ShardedDB) turboBulkUnionSortedRaw(indexTokens []string) ([]byte, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	// Read raw data for all indexes (zero-alloc: safe because TurboBinaryUnionRaw
	// creates a new buffer and does not mutate inputs).
	dataSets := make([][]byte, 0, len(indexTokens))
	for _, token := range indexTokens {
		key := turboKey(token)
		data, err := s.turboGetZeroAlloc(key)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				continue // skip missing indexes
			}
			return nil, err
		}
		if len(data) == 0 {
			continue
		}
		dataSets = append(dataSets, data)
	}

	if len(dataSets) == 0 {
		return nil, nil
	}

	// Merge-style union on sorted data, return raw result.
	return TurboBinaryUnionRaw(dataSets), nil
}

// TurboBulkUnionByKeys performs union on multiple turbo index keys using batch reads per shard.
// Much faster than calling TurboRawRead/TurboGet for each key individually.
// Keys can be strings (token names) or key128 hashed turbo index keys.
func (s *ShardedDB) TurboBulkUnionByKeys(keys []any) ([]any, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	// Convert keys to key128
	key128Keys := toKey128Slice(keys)

	// Group keys by shard index
	type shardKeys struct {
		shard *DB
		keys  []key128
	}
	shardMap := make(map[int]*shardKeys)
	for _, key := range key128Keys {
		shardIdx := int(key[0] % uint64(s.numShards))
		entry, ok := shardMap[shardIdx]
		if !ok {
			entry = &shardKeys{shard: s.getShardKey128(key)}
			shardMap[shardIdx] = entry
		}
		entry.keys = append(entry.keys, key)
	}

	// For each shard, read all keys and union
	var shardResults [][]byte
	for _, entry := range shardMap {
		shard := entry.shard
		if shard == nil {
			continue
		}
		shard.incRef()

		// Collect raw data for all keys in this shard
		dataSets := make([][]byte, 0, len(entry.keys))
		for _, key := range entry.keys {
			data := s.turboGetFromShardDirectKey128(shard, key)
			if len(data) == 0 {
				continue
			}
			dataSets = append(dataSets, data)
		}
		shard.closeRefDecr()

		if len(dataSets) == 0 {
			continue
		}

		// Merge-style union for this shard
		unioned := TurboBinaryUnionRaw(dataSets)
		if unioned != nil && len(unioned) > 0 {
			shardResults = append(shardResults, unioned)
		}
	}

	if len(shardResults) == 0 {
		return nil, nil
	}

	// Final union across shards
	final := TurboBinaryUnionRaw(shardResults)
	if final == nil || len(final) == 0 {
		return nil, nil
	}

	return key128ToAnySlice(TurboUnsafeReadTokens(final)), nil
}

// turboGetFromShardDirectKey128 reads a value from a shard's mmap directly, without high-level Get overhead.
// Returns raw value bytes or nil if not found.
func (s *ShardedDB) turboGetFromShardDirectKey128(shard *DB, key key128) []byte {
	bucketIdx := key[0] % shard.header.NumBuckets
	rootBucketOffset := headerSize + bucketIdx*bucketSize

	b := (*bucket)(unsafe.Pointer(&shard.mapped[rootBucketOffset]))
	if b.KeyOffset == 0 {
		return nil
	}

	curr := b
	for {
		if curr.Hash == key {
			vOffset := atomic.LoadUint64(&curr.ValOffset)
			vLen := atomic.LoadUint32(&curr.ValLen)
			// Return direct view into mmap — caller must not hold it long.
			return shard.mapped[vOffset : vOffset+uint64(vLen)]
		}

		next := atomic.LoadUint64(&curr.NextOffset)
		if next == 0 {
			break
		}

		// Validate next offset to prevent out-of-bounds access
		if next >= uint64(len(shard.mapped)) {
			// Corrupted NextOffset, stop iteration
			break
		}
		curr = (*bucket)(unsafe.Pointer(&shard.mapped[next]))
	}

	return nil
}

// TurboContainsAll checks if all specified docIDs are present in the given index.
func (s *ShardedDB) turboContainsAll(token string, docIDs []key128) (bool, error) {
	tokens, err := s.turboGetIndexTokens(token)
	if err != nil {
		return false, err
	}

	if tokens == nil {
		return len(docIDs) == 0, nil
	}

	if len(docIDs) == 0 {
		return true, nil
	}

	// Use merge-style: check if all docIDs exist in tokens
	// Both tokens and docIDs need to be sorted
	sortedDocIDs := make([]key128, len(docIDs))
	copy(sortedDocIDs, docIDs)
	RadixSortKey128(sortedDocIDs)

	// Merge-style: walk through both sorted lists
	i, j := 0, 0
	for i < len(sortedDocIDs) && j < len(tokens) {
		cmp := bytesCompareKey128(sortedDocIDs[i], tokens[j])
		if cmp < 0 {
			// docID not found in tokens
			return false, nil
		} else if cmp == 0 {
			// Found match, move to next docID
			i++
			j++
		} else {
			// token < docID, move to next token
			j++
		}
	}

	// If we've checked all docIDs, they all exist
	return i == len(sortedDocIDs), nil
}

// TurboContainsAny checks if any of the specified docIDs are present in the given index.
func (s *ShardedDB) turboContainsAny(token string, docIDs []key128) (bool, error) {
	tokens, err := s.turboGetIndexTokens(token)
	if err != nil {
		return false, err
	}

	if tokens == nil || len(docIDs) == 0 {
		return false, nil
	}

	// Use merge-style: check if any docID exists in tokens
	// Both tokens and docIDs need to be sorted
	sortedDocIDs := make([]key128, len(docIDs))
	copy(sortedDocIDs, docIDs)
	RadixSortKey128(sortedDocIDs)

	// Merge-style: walk through both sorted lists
	i, j := 0, 0
	for i < len(sortedDocIDs) && j < len(tokens) {
		cmp := bytesCompareKey128(sortedDocIDs[i], tokens[j])
		if cmp < 0 {
			// docID < token, move to next docID
			i++
		} else if cmp == 0 {
			// Found match
			return true, nil
		} else {
			// token < docID, move to next token
			j++
		}
	}

	return false, nil
}

// TurboMergeIndexes merges multiple source indexes into a single destination index.
// Duplicate tokens are preserved only once.
func (s *ShardedDB) turboMergeIndexes(destToken string, srcTokens []string) error {
	if len(srcTokens) == 0 {
		return nil
	}

	// Use merge-style union of all source indexes
	if len(srcTokens) == 0 {
		return s.turboDelete(turboKey(destToken))
	}

	tokens, err := s.turboBulkUnionSorted(srcTokens)
	if err != nil {
		return err
	}

	if len(tokens) == 0 {
		return s.turboDelete(turboKey(destToken))
	}

	// Write to destination
	key := turboKey(destToken)
	buf := turboSerializeKey128(tokens)
	return s.turboPut(key, buf)
}

// TurboSplitIndex splits a turbo index into multiple indexes based on a predicate.
// Tokens for which predicate returns true go to trueToken, others to falseToken.
func (s *ShardedDB) turboSplitIndex(srcToken string, trueToken string, falseToken string, predicate func(key128) bool) error {
	tokens, err := s.turboGetIndexTokens(srcToken)
	if err != nil {
		return err
	}

	if tokens == nil {
		return nil
	}

	trueTokens := make([]key128, 0)
	falseTokens := make([]key128, 0)

	for _, t := range tokens {
		if predicate(t) {
			trueTokens = append(trueTokens, t)
		} else {
			falseTokens = append(falseTokens, t)
		}
	}

	// Sort both slices for turbo index format
	RadixSortKey128(trueTokens)
	RadixSortKey128(falseTokens)

	// Write true tokens
	if len(trueTokens) > 0 {
		key := turboKey(trueToken)
		buf := turboSerializeKey128(trueTokens)
		if err := s.turboPut(key, buf); err != nil {
			return err
		}
	} else {
		_ = s.turboDelete(turboKey(trueToken)) // ignore if not exists
	}

	// Write false tokens
	if len(falseTokens) > 0 {
		key := turboKey(falseToken)
		buf := turboSerializeKey128(falseTokens)
		if err := s.turboPut(key, buf); err != nil {
			return err
		}
	} else {
		_ = s.turboDelete(turboKey(falseToken)) // ignore if not exists
	}

	return nil
}

// TurboCopyIndex copies a turbo index from one token to another.
func (s *ShardedDB) turboCopyIndex(srcToken string, destToken string) error {
	tokens, err := s.turboGetIndexTokens(srcToken)
	if err != nil {
		return err
	}

	if tokens == nil {
		return s.turboDelete(turboKey(destToken))
	}

	key := turboKey(destToken)
	buf := turboSerializeKey128(tokens)
	return s.turboPut(key, buf)
}

// TurboRawRead performs a raw read of turbo index data for advanced use cases.
// Returns the raw byte slice stored under the index key.
func (s *ShardedDB) turboRawRead(token string) ([]byte, error) {
	key := turboKey(token)
	return s.turboGet(key)
}

// TurboRawWrite performs a raw write of turbo index data for advanced use cases.
// The caller is responsible for ensuring the data format is correct.
func (s *ShardedDB) turboRawWrite(token string, data []byte) error {
	key := turboKey(token)
	return s.turboPut(key, data)
}

// TurboUnsafeReadTokens reads tokens directly from the raw data without validation.
// Use only when you are certain the data is valid turbo index format.
func TurboUnsafeReadTokens(data []byte) []key128 {
	if len(data) < int(turboHeaderSize) {
		return nil
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return nil
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return nil
	}

	tokens := make([]key128, count)
	for i := uint64(0); i < count; i++ {
		tokens[i][0] = binary.LittleEndian.Uint64(tokenData[i*turboIndexEntrySize:])
		tokens[i][1] = binary.LittleEndian.Uint64(tokenData[i*turboIndexEntrySize+8:])
	}

	return tokens
}

// TurboUnsafeContains checks if a token exists in raw turbo index data without validation.
// Use only when you are certain the data is valid turbo index format.
func TurboUnsafeContains(data []byte, docID key128) bool {
	if len(data) < int(turboHeaderSize) {
		return false
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return false
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return false
	}

	// Direct key128 scan
	for i := uint64(0); i < count; i++ {
		offset := i * turboIndexEntrySize
		t := (*[2]uint64)(unsafe.Pointer(&tokenData[offset]))
		if t[0] == docID[0] && t[1] == docID[1] {
			return true
		}
	}

	return false
}

// TurboUnsafeContainsKey128 checks if a key128 token exists in raw turbo index data without validation.
// Use only when you are certain the data is valid turbo index format.
func TurboUnsafeContainsKey128(data []byte, docID key128) bool {
	if len(data) < int(turboHeaderSize) {
		return false
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return false
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return false
	}

	// Direct key128 scan
	for i := uint64(0); i < count; i++ {
		offset := i * turboIndexEntrySize
		var t key128
		t[0] = binary.LittleEndian.Uint64(tokenData[offset:])
		t[1] = binary.LittleEndian.Uint64(tokenData[offset+8:])
		if t == docID {
			return true
		}
	}

	return false
}

// TurboUnsafeIntersect performs intersection on raw turbo index data without validation.
// Use only when you are certain all data is valid turbo index format.
func TurboUnsafeIntersect(dataSets [][]byte) []key128 {
	if len(dataSets) == 0 {
		return nil
	}

	// Start with the first set
	current := TurboUnsafeReadTokens(dataSets[0])
	if current == nil {
		return nil
	}

	// Intersect with each subsequent set using merge-style
	for i := 1; i < len(dataSets); i++ {
		other := TurboUnsafeReadTokens(dataSets[i])
		if other == nil {
			return nil
		}

		// Merge-style intersection
		current = turboSortedIntersect(current, other)
		if len(current) == 0 {
			return nil
		}
	}

	return current
}

// TurboAtomicGetCount atomically reads the count from a turbo index.
// This is a lock-free read operation.
func (s *ShardedDB) turboAtomicGetCount(token string) (uint64, error) {
	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return 0, nil
		}
		return 0, err
	}

	if len(val) < int(turboHeaderSize) {
		return 0, ErrTurboCorrupt
	}

	// Read count (first 8 bytes)
	count := binary.LittleEndian.Uint64(val)
	return count, nil
}

// TurboAtomicContains performs a lock-free check if a token exists in a turbo index.
// This is a lock-free read operation.
func (s *ShardedDB) turboAtomicContains(token string, docID key128) (bool, error) {
	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}

	return TurboUnsafeContainsKey128(val, docID), nil
}

// TurboAtomicGetTokens performs a lock-free read of all tokens from a turbo index.
// This is a lock-free read operation.
func (s *ShardedDB) turboAtomicGetTokens(token string) ([]key128, error) {
	key := turboKey(token)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return TurboUnsafeReadTokens(val), nil
}

// ---- internal helpers ----

// turboReadTokens reads tokens from turbo index data with validation.
func turboReadTokens(data []byte) []key128 {
	if len(data) < int(turboHeaderSize) {
		return nil
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return nil
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return nil
	}

	tokens := make([]key128, count)
	for i := uint64(0); i < count; i++ {
		tokens[i][0] = binary.LittleEndian.Uint64(tokenData[i*turboIndexEntrySize:])
		tokens[i][1] = binary.LittleEndian.Uint64(tokenData[i*turboIndexEntrySize+8:])
	}

	return tokens
}

// turboContains checks if a token exists in turbo index data.
func turboContains(data []byte, docID uint64) bool {
	if len(data) < int(turboHeaderSize) {
		return false
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return false
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*8 {
		return false
	}

	// Direct uint64 scan — each iteration reads 8 bytes and compares
	for i := uint64(0); i < count; i++ {
		t := binary.LittleEndian.Uint64(tokenData[i*8:])
		if t == docID {
			return true
		}
	}

	return false
}

// turboSerialize serializes tokens to turbo index format.
func turboSerialize(tokens []uint64) []byte {
	if len(tokens) == 0 {
		return nil
	}

	buf := make([]byte, turboHeaderSize+uint64(len(tokens))*8)
	binary.LittleEndian.PutUint64(buf, uint64(len(tokens)))
	for i, t := range tokens {
		binary.LittleEndian.PutUint64(buf[turboHeaderSize+uint64(i)*8:], t)
	}
	return buf
}

// turboSerializeKey128 serializes key128 tokens to turbo index format.
func turboSerializeKey128(tokens []key128) []byte {
	if len(tokens) == 0 {
		return nil
	}

	buf := make([]byte, turboHeaderSize+uint64(len(tokens))*turboIndexEntrySize)
	binary.LittleEndian.PutUint64(buf, uint64(len(tokens)))
	for i, t := range tokens {
		offset := turboHeaderSize + uint64(i)*turboIndexEntrySize
		binary.LittleEndian.PutUint64(buf[offset:], t[0])
		binary.LittleEndian.PutUint64(buf[offset+8:], t[1])
	}
	return buf
}

// ---- fast binary helpers (length / 8-byte aligned) ----

// TurboBinaryContains checks if a token exists in turbo index data.
// Operates directly on raw bytes: scans tokens as key128 at offset += 16.
// No allocations, no parsing beyond header.
func TurboBinaryContains(data []byte, docID key128) bool {
	if len(data) < int(turboHeaderSize) {
		return false
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return false
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return false
	}

	// Scan tokens directly, 16 bytes at a time
	for i := uint64(0); i < count; i++ {
		offset := i * turboIndexEntrySize
		var t key128
		t[0] = binary.LittleEndian.Uint64(tokenData[offset:])
		t[1] = binary.LittleEndian.Uint64(tokenData[offset+8:])
		if t == docID {
			return true
		}
	}

	return false
}

// TurboBinaryFindGE returns the first token >= target in turbo index data.
// Assumes tokens are sorted in ascending order.
// Returns (token, index) or (key128{}, -1) if not found.
// Operates directly on raw bytes, no allocations.
func TurboBinaryFindGE(data []byte, target key128) (key128, int) {
	if len(data) < int(turboHeaderSize) {
		return key128{}, -1
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return key128{}, -1
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return key128{}, -1
	}

	// Binary search over key128 tokens
	lo, hi := 0, int(count)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * turboIndexEntrySize
		t := (*key128)(unsafe.Pointer(&tokenData[offset]))
		cmp := bytesCompareKey128(*t, target)
		if cmp >= 0 {
			if mid == 0 || bytesCompareKey128(*(*key128)(unsafe.Pointer(&tokenData[(mid-1)*turboIndexEntrySize])), target) < 0 {
				return *t, mid
			}
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}

	return key128{}, -1
}

// TurboBinaryFindLE returns the last token <= target in turbo index data.
// Assumes tokens are sorted in ascending order.
// Returns (token, index) or (key128{}, -1) if not found.
// Operates directly on raw bytes, no allocations.
func TurboBinaryFindLE(data []byte, target key128) (key128, int) {
	if len(data) < int(turboHeaderSize) {
		return key128{}, -1
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return key128{}, -1
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return key128{}, -1
	}

	// Binary search over key128 tokens
	lo, hi := 0, int(count)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * turboIndexEntrySize
		t := (*key128)(unsafe.Pointer(&tokenData[offset]))
		cmp := bytesCompareKey128(*t, target)
		if cmp <= 0 {
			if mid == int(count)-1 || bytesCompareKey128(*(*key128)(unsafe.Pointer(&tokenData[(mid+1)*turboIndexEntrySize])), target) > 0 {
				return *t, mid
			}
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	return key128{}, -1
}

// TurboBinaryFindExact returns the index of the exact token in turbo index data.
// Assumes tokens are sorted in ascending order.
// Returns index or -1 if not found.
// Operates directly on raw bytes, no allocations.
func TurboBinaryFindExact(data []byte, target key128) int {
	if len(data) < int(turboHeaderSize) {
		return -1
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return -1
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return -1
	}

	// Binary search over key128 tokens
	lo, hi := 0, int(count)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * turboIndexEntrySize
		t := (*key128)(unsafe.Pointer(&tokenData[offset]))
		cmp := bytesCompareKey128(*t, target)
		if cmp == 0 {
			return mid
		}
		if cmp < 0 {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	return -1
}

// TurboBinaryIntersectRaw performs intersection on multiple raw turbo index data sets.
// Returns a new turbo index buffer with the intersected tokens.
// Uses zero-copy merge-style intersection on sorted data — no []uint64 allocations.
func TurboBinaryIntersectRaw(dataSets [][]byte) []byte {
	if len(dataSets) == 0 {
		return nil
	}

	// Start with first set as current (raw bytes)
	current := dataSets[0]
	if len(current) < int(turboHeaderSize) {
		return nil
	}
	if TurboBinaryCount(current) == 0 {
		return nil
	}

	// Intersect with each subsequent set using zero-copy merge-style
	for i := 1; i < len(dataSets); i++ {
		other := dataSets[i]
		if len(other) < int(turboHeaderSize) {
			return nil
		}
		if TurboBinaryCount(other) == 0 {
			return nil
		}

		// Zero-copy merge-style intersection on raw bytes
		current = turboBinaryIntersectTwoRaw(current, other)
		if current == nil {
			return nil
		}
	}

	return current
}

// turboBinaryIntersectTwoRaw performs merge-style intersection on two raw turbo buffers.
// Both must be sorted. Returns a new turbo buffer with the result.
// No allocations — reads key128 directly from raw bytes.
func turboBinaryIntersectTwoRaw(a, b []byte) []byte {
	countA := TurboBinaryCount(a)
	countB := TurboBinaryCount(b)
	if countA == 0 || countB == 0 {
		return nil
	}

	tokenDataA := a[turboHeaderSize:]
	tokenDataB := b[turboHeaderSize:]

	// Pre-allocate result buffer with max possible size
	maxResult := countA
	if countB < maxResult {
		maxResult = countB
	}
	buf := make([]byte, turboHeaderSize+maxResult*turboIndexEntrySize)

	// Map raw bytes to []key128 without allocation/copying
	sliceA := (*[1 << 29]key128)(unsafe.Pointer(&tokenDataA[0]))[:countA:countA]
	sliceB := (*[1 << 29]key128)(unsafe.Pointer(&tokenDataB[0]))[:countB:countB]
	sliceRes := (*[1 << 29]key128)(unsafe.Pointer(&buf[turboHeaderSize]))[:maxResult:maxResult]

	out := turboIntersectKey128(sliceA, sliceB, sliceRes)

	if out == 0 {
		return nil
	}

	binary.LittleEndian.PutUint64(buf, out)
	return buf[:turboHeaderSize+out*turboIndexEntrySize]
}

// TurboBinaryUnionRaw performs union on multiple raw turbo index data sets.
// Returns a new turbo index buffer with the unioned tokens.
// Uses zero-copy merge-style union on sorted data — no []uint64 allocations.
func TurboBinaryUnionRaw(dataSets [][]byte) []byte {
	if len(dataSets) == 0 {
		return nil
	}

	// Start with first set as current (raw bytes)
	current := dataSets[0]
	if len(current) < int(turboHeaderSize) {
		return nil
	}
	if TurboBinaryCount(current) == 0 {
		return nil
	}

	// Merge with each subsequent set using zero-copy merge-style
	for i := 1; i < len(dataSets); i++ {
		other := dataSets[i]
		if len(other) < int(turboHeaderSize) {
			continue
		}
		if TurboBinaryCount(other) == 0 {
			continue
		}

		current = turboBinaryUnionTwoRaw(current, other)
	}

	return current
}

// turboBinaryUnionTwoRaw performs merge-style union on two raw turbo buffers.
// Both must be sorted. Returns a new turbo buffer with the result.
// No allocations — reads key128 directly from raw bytes.
func turboBinaryUnionTwoRaw(a, b []byte) []byte {
	countA := TurboBinaryCount(a)
	countB := TurboBinaryCount(b)
	if countA == 0 {
		return b
	}
	if countB == 0 {
		return a
	}

	tokenDataA := a[turboHeaderSize:]
	tokenDataB := b[turboHeaderSize:]

	// Pre-allocate result buffer with max possible size
	buf := make([]byte, turboHeaderSize+(countA+countB)*turboIndexEntrySize)

	// Map raw bytes to []key128 without allocation/copying
	sliceA := (*[1 << 29]key128)(unsafe.Pointer(&tokenDataA[0]))[:countA:countA]
	sliceB := (*[1 << 29]key128)(unsafe.Pointer(&tokenDataB[0]))[:countB:countB]
	sliceRes := (*[1 << 29]key128)(unsafe.Pointer(&buf[turboHeaderSize]))[: countA+countB : countA+countB]

	out := turboUnionKey128(sliceA, sliceB, sliceRes)

	if out == 0 {
		return nil
	}

	binary.LittleEndian.PutUint64(buf, out)
	return buf[:turboHeaderSize+out*turboIndexEntrySize]
}

// TurboBinaryCount returns the count of tokens in turbo index data.
// Reads only the header (first 8 bytes).
func TurboBinaryCount(data []byte) uint64 {
	if len(data) < int(turboHeaderSize) {
		return 0
	}
	return binary.LittleEndian.Uint64(data)
}

// TurboBinaryLen returns the number of 8-byte tokens in turbo index data.
// Equivalent to TurboBinaryCount but as int.
func TurboBinaryLen(data []byte) int {
	count := TurboBinaryCount(data)
	if count > 1<<30 {
		return -1 // overflow protection
	}
	return int(count)
}

// TurboBinaryGetToken returns the token at the given index in turbo index data.
// index is 0-based position in the token array.
// Returns (token, ok) where ok is false if index is out of bounds.
func TurboBinaryGetToken(data []byte, index int) (key128, bool) {
	if len(data) < int(turboHeaderSize) {
		return key128{}, false
	}

	count := binary.LittleEndian.Uint64(data)
	if index < 0 || uint64(index) >= count {
		return key128{}, false
	}

	tokenData := data[turboHeaderSize:]
	offset := uint64(index) * turboIndexEntrySize
	if offset+turboIndexEntrySize > uint64(len(tokenData)) {
		return key128{}, false
	}

	return key128{
		binary.LittleEndian.Uint64(tokenData[offset:]),
		binary.LittleEndian.Uint64(tokenData[offset+8:]),
	}, true
}

// TurboBinarySlice returns a slice of tokens from turbo index data.
// start is inclusive, end is exclusive.
// Returns nil if indices are invalid.
func TurboBinarySlice(data []byte, start, end int) []key128 {
	if len(data) < int(turboHeaderSize) {
		return nil
	}

	count := binary.LittleEndian.Uint64(data)
	if start < 0 || end < start || uint64(end) > count {
		return nil
	}

	tokenData := data[turboHeaderSize:]
	lenSlice := end - start
	tokens := make([]key128, lenSlice)

	for i := 0; i < lenSlice; i++ {
		idx := start + i
		offset := uint64(idx) * turboIndexEntrySize
		tokens[i][0] = binary.LittleEndian.Uint64(tokenData[offset:])
		tokens[i][1] = binary.LittleEndian.Uint64(tokenData[offset+8:])
	}

	return tokens
}

// TurboBinaryScan scans all tokens in turbo index data and applies a function.
// Returns false if the function returns false (early exit).
// Operates directly on raw bytes, no allocations.
func TurboBinaryScan(data []byte, fn func(key128, int) bool) bool {
	if len(data) < int(turboHeaderSize) {
		return true
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return true
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboIndexEntrySize {
		return false
	}

	for i := uint64(0); i < count; i++ {
		offset := i * turboIndexEntrySize
		t := (*key128)(unsafe.Pointer(&tokenData[offset]))
		if !fn(*t, int(i)) {
			return false
		}
	}

	return true
}

// TurboBinaryFilter filters tokens in turbo index data by a predicate.
// Returns a new turbo index buffer with only tokens for which fn returns true.
func TurboBinaryFilter(data []byte, fn func(key128, int) bool) []byte {
	tokens := TurboUnsafeReadTokens(data)
	if tokens == nil {
		return nil
	}

	var filtered []key128
	for i, t := range tokens {
		if fn(t, i) {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	return turboSerializeKey128(filtered)
}

// TurboBinaryMap maps each token in turbo index data using a function.
// Returns a new turbo index buffer with mapped tokens.
func TurboBinaryMap(data []byte, fn func(key128, int) key128) []byte {
	tokens := TurboUnsafeReadTokens(data)
	if tokens == nil {
		return nil
	}

	mapped := make([]key128, len(tokens))
	for i, t := range tokens {
		mapped[i] = fn(t, i)
	}

	return turboSerializeKey128(mapped)
}

// TurboBinarySort sorts tokens in turbo index data in ascending order.
// Returns a new turbo index buffer with sorted tokens.
// Uses Radix Sort for maximum performance on key128.
func TurboBinarySort(data []byte) []byte {
	tokens := TurboUnsafeReadTokens(data)
	if tokens == nil {
		return nil
	}

	RadixSortKey128(tokens)
	return turboSerializeKey128(tokens)
}

// TurboBinaryDedup removes duplicate tokens from turbo index data.
// Returns a new turbo index buffer with unique tokens (preserves order of first occurrence).
func TurboBinaryDedup(data []byte) []byte {
	tokens := TurboUnsafeReadTokens(data)
	if tokens == nil {
		return nil
	}

	// Sort tokens first (they might not be sorted)
	RadixSortKey128(tokens)

	// Deduplicate sorted tokens in O(n) time
	if len(tokens) == 0 {
		return nil
	}
	unique := make([]key128, 0, len(tokens))
	unique = append(unique, tokens[0])
	for i := 1; i < len(tokens); i++ {
		if tokens[i] != tokens[i-1] {
			unique = append(unique, tokens[i])
		}
	}

	if len(unique) == 0 {
		return nil
	}

	return turboSerializeKey128(unique)
}

// TurboBinaryDiff returns tokens that are in the first index but not in the second.
// Returns a new turbo index buffer with the difference.
// Uses merge-style diff on sorted data — no map allocations.
func TurboBinaryDiff(data1 []byte, data2 []byte) []byte {
	count1 := TurboBinaryCount(data1)
	count2 := TurboBinaryCount(data2)
	if count1 == 0 {
		return nil
	}
	if count2 == 0 {
		return data1
	}

	tokenData1 := data1[turboHeaderSize:]
	tokenData2 := data2[turboHeaderSize:]

	// Pre-allocate result buffer
	buf := make([]byte, turboHeaderSize+count1*turboIndexEntrySize)

	// Map raw bytes to []key128 without allocation/copying
	slice1 := (*[1 << 29]key128)(unsafe.Pointer(&tokenData1[0]))[:count1:count1]
	slice2 := (*[1 << 29]key128)(unsafe.Pointer(&tokenData2[0]))[:count2:count2]
	sliceRes := (*[1 << 29]key128)(unsafe.Pointer(&buf[turboHeaderSize]))[:count1:count1]

	out := turboDiffKey128(slice1, slice2, sliceRes)

	if out == 0 {
		return nil
	}

	binary.LittleEndian.PutUint64(buf, out)
	return buf[:turboHeaderSize+out*turboIndexEntrySize]
}

// TurboBinaryMerge merges two turbo index data sets.
// Returns a new turbo index buffer with all tokens from both sets (no dedup).
func TurboBinaryMerge(data1 []byte, data2 []byte) []byte {
	tokens1 := TurboUnsafeReadTokens(data1)
	tokens2 := TurboUnsafeReadTokens(data2)

	if tokens1 == nil && tokens2 == nil {
		return nil
	}

	if tokens1 == nil {
		return turboSerializeKey128(tokens2)
	}

	if tokens2 == nil {
		return turboSerializeKey128(tokens1)
	}

	merged := make([]key128, len(tokens1)+len(tokens2))
	copy(merged, tokens1)
	copy(merged[len(tokens1):], tokens2)

	return turboSerializeKey128(merged)
}

// TurboBinaryValidate validates turbo index data format.
// Returns true if the data is valid turbo index format.
func TurboBinaryValidate(data []byte) bool {
	if len(data) < int(turboHeaderSize) {
		return false
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return true // empty index is valid
	}

	tokenData := data[turboHeaderSize:]
	return uint64(len(tokenData)) >= count*turboIndexEntrySize
}

// TurboBinaryHeaderSize returns the size of the turbo index header.
func TurboBinaryHeaderSize() int {
	return int(turboHeaderSize)
}

// TurboBinarySize returns the expected size of a turbo index buffer for the given count.
func TurboBinarySize(count int) int {
	if count <= 0 {
		return 0
	}
	return int(turboHeaderSize) + count*turboIndexEntrySize
}

// TurboBinaryNew creates a new turbo index buffer from tokens.
func TurboBinaryNew(tokens []key128) []byte {
	return turboSerializeKey128(tokens)
}

// TurboBinaryNewEmpty creates an empty turbo index buffer.
func TurboBinaryNewEmpty() []byte {
	buf := make([]byte, turboHeaderSize)
	binary.LittleEndian.PutUint64(buf, 0)
	return buf
}

// TurboBinaryAppend appends a token to a turbo index buffer.
// Returns a new turbo index buffer with the appended token.
func TurboBinaryAppend(data []byte, token key128) []byte {
	tokens := TurboUnsafeReadTokens(data)
	if tokens == nil {
		tokens = make([]key128, 0)
	}

	tokens = append(tokens, token)
	return turboSerializeKey128(tokens)
}

// TurboBinaryAppendBatch appends multiple tokens to a turbo index buffer.
// Returns a new turbo index buffer with the appended tokens.
func TurboBinaryAppendBatch(data []byte, tokens []key128) []byte {
	if len(tokens) == 0 {
		return data
	}

	existing := TurboUnsafeReadTokens(data)
	if existing == nil {
		return turboSerializeKey128(tokens)
	}

	merged := make([]key128, len(existing)+len(tokens))
	copy(merged, existing)
	copy(merged[len(existing):], tokens)

	return turboSerializeKey128(merged)
}

// TurboBinaryRemove removes the first occurrence of a token from a turbo index buffer.
// Returns a new turbo index buffer without the token, or the original if not found.
func TurboBinaryRemove(data []byte, token key128) []byte {
	tokens := TurboUnsafeReadTokens(data)
	if tokens == nil {
		return nil
	}

	// Find and remove
	for i, t := range tokens {
		if t == token {
			// Swap with last
			last := len(tokens) - 1
			tokens[i] = tokens[last]
			tokens = tokens[:last]
			if len(tokens) == 0 {
				return nil
			}
			return turboSerializeKey128(tokens)
		}
	}

	return data
}

// TurboBinaryRemoveAll removes all occurrences of tokens from a turbo index buffer.
// Returns a new turbo index buffer without the specified tokens.
func TurboBinaryRemoveAll(data []byte, tokens []key128) []byte {
	if len(tokens) == 0 {
		return data
	}

	existing := TurboUnsafeReadTokens(data)
	if existing == nil {
		return nil
	}

	// Sort tokens for merge-style diff
	sortedTokens := make([]key128, len(tokens))
	copy(sortedTokens, tokens)
	RadixSortKey128(sortedTokens)

	// Merge-style diff
	result := turboSortedDiff(existing, sortedTokens)

	if len(result) == 0 {
		return nil
	}

	return turboSerializeKey128(result)
}

// ---- roaring-style bitmap operations on turbo indexes ----

// TurboCardinality returns the number of elements in turbo index data.
// Equivalent to TurboBinaryCount, named for roaring-style API.
func TurboCardinality(data []byte) uint64 {
	return TurboBinaryCount(data)
}

// TurboIntersectCount returns the number of elements in the intersection
// of multiple turbo index data sets without building the result.
// Operates directly on raw bytes, minimal allocations.
// For sorted indexes, uses efficient merge-style intersection.
func TurboIntersectCount(dataSets [][]byte) uint64 {
	if len(dataSets) == 0 {
		return 0
	}

	// Check for empty sets
	for i := 0; i < len(dataSets); i++ {
		if TurboBinaryCount(dataSets[i]) == 0 {
			return 0
		}
	}

	if len(dataSets) == 1 {
		return TurboBinaryCount(dataSets[0])
	}

	// For two sets, use optimized merge-style intersection if sorted
	if len(dataSets) == 2 {
		return turboIntersectCountTwoSorted(dataSets[0], dataSets[1])
	}

	// For more than two sets, iteratively intersect
	// Start with the smallest set
	smallestIdx := 0
	smallestCount := TurboBinaryCount(dataSets[0])
	for i := 1; i < len(dataSets); i++ {
		c := TurboBinaryCount(dataSets[i])
		if c < smallestCount {
			smallestCount = c
			smallestIdx = i
		}
	}

	// Build result as a sorted, deduplicated slice from the smallest set
	base := TurboUnsafeReadTokens(dataSets[smallestIdx])
	if base == nil {
		return 0
	}

	// Sort base for efficient merge intersection
	RadixSortKey128(base)

	// Intersect with each other set using merge-style
	for i := 0; i < len(dataSets); i++ {
		if i == smallestIdx {
			continue
		}

		other := TurboUnsafeReadTokens(dataSets[i])
		if other == nil {
			return 0
		}

		// Sort other for merge intersection
		RadixSortKey128(other)

		// Merge-style intersection
		var next []key128
		i1, i2 := 0, 0
		for i1 < len(base) && i2 < len(other) {
			if base[i1] == other[i2] {
				next = append(next, base[i1])
				i1++
				i2++
			} else if bytesCompareKey128(base[i1], other[i2]) < 0 {
				i1++
			} else {
				i2++
			}
		}

		base = next
		if len(base) == 0 {
			return 0
		}
	}

	return uint64(len(base))
}

// turboIntersectCountTwoSorted performs efficient merge-style intersection
// count for two sorted turbo index data sets.
func turboIntersectCountTwoSorted(a, b []byte) uint64 {
	countA := TurboBinaryCount(a)
	countB := TurboBinaryCount(b)
	if countA == 0 || countB == 0 {
		return 0
	}

	// Read tokens as key128
	tokensA := TurboUnsafeReadTokens(a)
	tokensB := TurboUnsafeReadTokens(b)
	if tokensA == nil || tokensB == nil {
		return 0
	}

	// Merge-style intersection count
	var count uint64
	i, j := 0, 0
	for i < len(tokensA) && j < len(tokensB) {
		if tokensA[i] == tokensB[j] {
			count++
			i++
			j++
		} else if bytesCompareKey128(tokensA[i], tokensB[j]) < 0 {
			i++
		} else {
			j++
		}
	}
	return count
}

// TurboIntersectCountSorted returns the number of elements in the intersection
// of multiple SORTED turbo index data sets without building the result.
// Assumes all data sets are sorted in ascending order.
// For two sets, uses zero-allocation merge-style intersection.
// For more than two sets, iteratively intersects using sorted merge.
func TurboIntersectCountSorted(dataSets [][]byte) uint64 {
	if len(dataSets) == 0 {
		return 0
	}

	// Check for empty sets
	for i := 0; i < len(dataSets); i++ {
		if TurboBinaryCount(dataSets[i]) == 0 {
			return 0
		}
	}

	if len(dataSets) == 1 {
		return TurboBinaryCount(dataSets[0])
	}

	if len(dataSets) == 2 {
		return turboIntersectCountTwoSorted(dataSets[0], dataSets[1])
	}

	// For more than two sorted sets, iteratively intersect
	// Start with the smallest set
	smallestIdx := 0
	smallestCount := TurboBinaryCount(dataSets[0])
	for i := 1; i < len(dataSets); i++ {
		c := TurboBinaryCount(dataSets[i])
		if c < smallestCount {
			smallestCount = c
			smallestIdx = i
		}
	}

	base := TurboUnsafeReadTokens(dataSets[smallestIdx])
	if base == nil {
		return 0
	}

	// Intersect with each other sorted set using merge-style
	for i := 0; i < len(dataSets); i++ {
		if i == smallestIdx {
			continue
		}

		other := TurboUnsafeReadTokens(dataSets[i])
		if other == nil {
			return 0
		}

		// Merge-style intersection (both are sorted)
		var next []key128
		i1, i2 := 0, 0
		for i1 < len(base) && i2 < len(other) {
			if base[i1] == other[i2] {
				next = append(next, base[i1])
				i1++
				i2++
			} else if bytesCompareKey128(base[i1], other[i2]) < 0 {
				i1++
			} else {
				i2++
			}
		}

		base = next
		if len(base) == 0 {
			return 0
		}
	}

	return uint64(len(base))
}

// TurboIntersectToBitmapSorted returns a new turbo index buffer representing
// the intersection of multiple SORTED turbo index data sets.
// Assumes all data sets are sorted in ascending order.
// Uses efficient merge-style intersection.
func TurboIntersectToBitmapSorted(dataSets [][]byte) []byte {
	if len(dataSets) == 0 {
		return nil
	}

	// Check for empty sets
	for i := 0; i < len(dataSets); i++ {
		if TurboBinaryCount(dataSets[i]) == 0 {
			return nil
		}
	}

	if len(dataSets) == 1 {
		return dataSets[0]
	}

	// Start with the smallest set
	smallestIdx := 0
	smallestCount := TurboBinaryCount(dataSets[0])
	for i := 1; i < len(dataSets); i++ {
		c := TurboBinaryCount(dataSets[i])
		if c < smallestCount {
			smallestCount = c
			smallestIdx = i
		}
	}

	base := TurboUnsafeReadTokens(dataSets[smallestIdx])
	if base == nil {
		return nil
	}

	// Intersect with each other sorted set using merge-style
	for i := 0; i < len(dataSets); i++ {
		if i == smallestIdx {
			continue
		}

		other := TurboUnsafeReadTokens(dataSets[i])
		if other == nil {
			return nil
		}

		// Merge-style intersection (both are sorted)
		var next []key128
		i1, i2 := 0, 0
		for i1 < len(base) && i2 < len(other) {
			if base[i1] == other[i2] {
				next = append(next, base[i1])
				i1++
				i2++
			} else if bytesCompareKey128(base[i1], other[i2]) < 0 {
				i1++
			} else {
				i2++
			}
		}

		base = next
		if len(base) == 0 {
			return nil
		}
	}

	return turboSerializeKey128(base)
}

// TurboAndSorted performs intersection of two SORTED turbo index data sets.
// Uses efficient merge-style intersection.
func TurboAndSorted(a, b []byte) []byte {
	return TurboIntersectToBitmapSorted([][]byte{a, b})
}

// TurboIntersectCountSortedFromDB returns the number of elements in the intersection
// of multiple SORTED turbo indexes by name, without building the result.
func (s *ShardedDB) turboIntersectCountSortedFromDB(indexTokens []string) (uint64, error) {
	if len(indexTokens) == 0 {
		return 0, nil
	}

	// Read all raw index data
	dataSets := make([][]byte, len(indexTokens))
	for i, token := range indexTokens {
		data, err := s.turboRawRead(token)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return 0, nil
			}
			return 0, err
		}
		dataSets[i] = data
	}

	return TurboIntersectCountSorted(dataSets), nil
}

// TurboIntersectToBitmapSortedFromDB returns a new turbo index buffer representing
// the intersection of multiple SORTED turbo indexes by name.
func (s *ShardedDB) turboIntersectToBitmapSortedFromDB(indexTokens []string) ([]byte, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	// Read all raw index data
	dataSets := make([][]byte, len(indexTokens))
	for i, token := range indexTokens {
		data, err := s.turboRawRead(token)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil, nil
			}
			return nil, err
		}
		dataSets[i] = data
	}

	return TurboIntersectToBitmapSorted(dataSets), nil
}

// TurboIntersectToBitmap returns a new turbo index buffer representing
// the intersection of multiple turbo index data sets.
// Uses merge-style intersection on sorted data — no map allocations.
func TurboIntersectToBitmap(dataSets [][]byte) []byte {
	if len(dataSets) == 0 {
		return nil
	}

	// Check for empty sets
	for i := 0; i < len(dataSets); i++ {
		if TurboBinaryCount(dataSets[i]) == 0 {
			return nil
		}
	}

	if len(dataSets) == 1 {
		return dataSets[0]
	}

	// Start with the smallest set
	smallestIdx := 0
	smallestCount := TurboBinaryCount(dataSets[0])
	for i := 1; i < len(dataSets); i++ {
		c := TurboBinaryCount(dataSets[i])
		if c < smallestCount {
			smallestCount = c
			smallestIdx = i
		}
	}

	base := TurboUnsafeReadTokens(dataSets[smallestIdx])
	if base == nil {
		return nil
	}

	// Intersect with each other set using merge-style (sorted)
	for i := 0; i < len(dataSets); i++ {
		if i == smallestIdx {
			continue
		}

		other := TurboUnsafeReadTokens(dataSets[i])
		if other == nil {
			return nil
		}

		base = turboSortedIntersect(base, other)
		if len(base) == 0 {
			return nil
		}
	}

	return turboSerializeKey128(base)
}

// TurboUnionToBitmap returns a new turbo index buffer representing
// the union of multiple turbo index data sets.
// Uses merge-style union on sorted data — no map allocations.
func TurboUnionToBitmap(dataSets [][]byte) []byte {
	if len(dataSets) == 0 {
		return nil
	}

	// Start with first non-empty set
	result := TurboUnsafeReadTokens(dataSets[0])
	if result == nil {
		return nil
	}

	// Merge with each subsequent set using merge-style (sorted)
	for i := 1; i < len(dataSets); i++ {
		other := TurboUnsafeReadTokens(dataSets[i])
		if other == nil {
			continue
		}
		result = turboSortedUnion(result, other)
	}

	if len(result) == 0 {
		return nil
	}

	return turboSerializeKey128(result)
}

// TurboDiffToBitmap returns a new turbo index buffer representing
// the difference: data1 - data2.
// Similar to roaring.AndNot(a, b) but on raw turbo buffers.
func TurboDiffToBitmap(data1 []byte, data2 []byte) []byte {
	return TurboBinaryDiff(data1, data2)
}

// TurboAnd performs intersection of two turbo index data sets.
// Alias for TurboIntersectToBitmap with exactly two inputs.
func TurboAnd(a []byte, b []byte) []byte {
	return TurboIntersectToBitmap([][]byte{a, b})
}

// TurboOr performs union of two turbo index data sets.
// Alias for TurboUnionToBitmap with exactly two inputs.
func TurboOr(a []byte, b []byte) []byte {
	return TurboUnionToBitmap([][]byte{a, b})
}

// TurboAndNot performs difference of two turbo index data sets (a - b).
// Alias for TurboDiffToBitmap.
func TurboAndNot(a []byte, b []byte) []byte {
	return TurboDiffToBitmap(a, b)
}

// TurboIntersectCountFromDB returns the number of elements in the intersection
// of multiple turbo indexes by name, without building the result.
func (s *ShardedDB) turboIntersectCountFromDB(indexTokens []string) (uint64, error) {
	if len(indexTokens) == 0 {
		return 0, nil
	}

	// Read all raw index data
	dataSets := make([][]byte, len(indexTokens))
	for i, token := range indexTokens {
		data, err := s.turboRawRead(token)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return 0, nil // empty index => empty intersection
			}
			return 0, err
		}
		dataSets[i] = data
	}

	return TurboIntersectCount(dataSets), nil
}

// TurboIntersectToBitmapFromDB returns a new turbo index buffer representing
// the intersection of multiple turbo indexes by name.
func (s *ShardedDB) turboIntersectToBitmapFromDB(indexTokens []string) ([]byte, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	// Read all raw index data
	dataSets := make([][]byte, len(indexTokens))
	for i, token := range indexTokens {
		data, err := s.turboRawRead(token)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil, nil // empty index => empty intersection
			}
			return nil, err
		}
		dataSets[i] = data
	}

	return TurboIntersectToBitmap(dataSets), nil
}

// TurboUnionToBitmapFromDB returns a new turbo index buffer representing
// the union of multiple turbo indexes by name.
func (s *ShardedDB) turboUnionToBitmapFromDB(indexTokens []string) ([]byte, error) {
	if len(indexTokens) == 0 {
		return nil, nil
	}

	// Read all raw index data
	dataSets := make([][]byte, len(indexTokens))
	for i, token := range indexTokens {
		data, err := s.turboRawRead(token)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				continue // skip empty indexes in union
			}
			return nil, err
		}
		dataSets[i] = data
	}

	return TurboUnionToBitmap(dataSets), nil
}

// TurboAndFromDB performs intersection of two turbo indexes by name.
func (s *ShardedDB) turboAndFromDB(a, b string) ([]byte, error) {
	return s.turboIntersectToBitmapFromDB([]string{a, b})
}

// TurboOrFromDB performs union of two turbo indexes by name.
func (s *ShardedDB) turboOrFromDB(a, b string) ([]byte, error) {
	return s.turboUnionToBitmapFromDB([]string{a, b})
}

// TurboAndNotFromDB performs difference of two turbo indexes by name (a - b).
func (s *ShardedDB) turboAndNotFromDB(a, b string) ([]byte, error) {
	data1, err := s.turboRawRead(a)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}

	data2, err := s.turboRawRead(b)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return data1, nil // b is empty => a - empty = a
		}
		return nil, err
	}

	return TurboAndNot(data1, data2), nil
}

// ---- Radix Sort for uint64 (LSD-first, base-256) ----

// RadixSortUint64 sorts a slice of uint64 in ascending order using
// LSD-first radix sort (base-256). Stable and branch-predictor friendly.
// Uses a single auxiliary slice for all passes.
func RadixSortUint64(src []uint64) {
	if len(src) <= 1 {
		return
	}

	// Allocate auxiliary slice once
	tmp := make([]uint64, len(src))

	// 8 bytes per uint64, 8 passes (LSD-first: from least significant to most)
	for shift := 0; shift < 64; shift += 8 {
		// Count occurrences of each byte value (0-255)
		counts := [256]int{}
		for i := 0; i < len(src); i++ {
			byteVal := (src[i] >> shift) & 0xFF
			counts[byteVal]++
		}

		// Compute starting positions
		pos := [256]int{}
		sum := 0
		for b := 0; b < 256; b++ {
			pos[b] = sum
			sum += counts[b]
		}

		// Distribute into tmp
		for i := 0; i < len(src); i++ {
			byteVal := (src[i] >> shift) & 0xFF
			idx := pos[byteVal]
			tmp[idx] = src[i]
			pos[byteVal]++
		}

		// Copy back
		copy(src, tmp)
	}
}

// RadixSortKey128 sorts a slice of key128 in ascending order.
// Uses Go's sort for correctness; can be optimized with radix sort later.
func RadixSortKey128(src []key128) {
	if len(src) <= 1 {
		return
	}
	// Use standard sort with custom comparator for correctness
	// TODO: Optimize with proper LSD radix sort
	sort.Slice(src, func(i, j int) bool {
		return bytesCompareKey128(src[i], src[j]) < 0
	})
}

// RadixSortUint64InPlace sorts a slice of uint64 in ascending order using
// LSD-first radix sort (base-256). Uses a shared buffer to reduce allocations.
// buf is an optional pre-allocated buffer; if nil or too small, a new one is allocated.
func RadixSortUint64InPlace(src []uint64, buf []uint64) []uint64 {
	if len(src) <= 1 {
		return buf
	}

	if cap(buf) < len(src) {
		buf = make([]uint64, len(src))
	}
	tmp := buf[:len(src)]

	for shift := 0; shift < 64; shift += 8 {
		counts := [256]int{}
		for i := 0; i < len(src); i++ {
			byteVal := (src[i] >> shift) & 0xFF
			counts[byteVal]++
		}

		pos := [256]int{}
		sum := 0
		for b := 0; b < 256; b++ {
			pos[b] = sum
			sum += counts[b]
		}

		for i := 0; i < len(src); i++ {
			byteVal := (src[i] >> shift) & 0xFF
			idx := pos[byteVal]
			tmp[idx] = src[i]
			pos[byteVal]++
		}

		copy(src, tmp)
	}

	return buf
}

// TurboRadixSort sorts tokens in turbo index data in ascending order using Radix Sort.
// Returns a new turbo index buffer with sorted tokens.
func TurboRadixSort(data []byte) []byte {
	tokens := TurboUnsafeReadTokens(data)
	if tokens == nil {
		return nil
	}

	RadixSortKey128(tokens)
	return turboSerializeKey128(tokens)
}

// ---- internal helpers ----

// turboBinarySearch performs binary search on a sorted slice of uint64.
// Returns index if found, or -1 if not found.
func turboBinarySearch(tokens []uint64, target uint64) int {
	lo, hi := 0, len(tokens)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if tokens[mid] == target {
			return mid
		}
		if tokens[mid] < target {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return -1
}

// turboMergeSorted merges two sorted slices of uint64 into a new sorted slice.
// Result may contain duplicates.
func turboMergeSorted(a, b []uint64) []uint64 {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	result := make([]uint64, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] <= b[j] {
			result = append(result, a[i])
			i++
		} else {
			result = append(result, b[j])
			j++
		}
	}
	for i < len(a) {
		result = append(result, a[i])
		i++
	}
	for j < len(b) {
		result = append(result, b[j])
		j++
	}
	return result
}

// turboMergeSortedUnique merges two sorted slices of uint64 into a new sorted slice.
// Result contains no duplicates.
func turboMergeSortedUnique(a, b []uint64) []uint64 {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	result := make([]uint64, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else if a[i] > b[j] {
			result = append(result, b[j])
			j++
		} else {
			// Equal, add once
			result = append(result, a[i])
			i++
			j++
		}
	}
	for i < len(a) {
		result = append(result, a[i])
		i++
	}
	for j < len(b) {
		result = append(result, b[j])
		j++
	}
	return result
}

// turboDiffSorted computes the difference of two sorted slices of uint64 (a - b).
// Returns a new sorted slice with elements in a but not in b.
func turboDiffSorted(a, b []uint64) []uint64 {
	if len(a) == 0 {
		return nil
	}
	if len(b) == 0 {
		return a
	}

	result := make([]uint64, 0, len(a))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else if a[i] > b[j] {
			j++
		} else {
			// Equal, skip
			i++
			j++
		}
	}
	for i < len(a) {
		result = append(result, a[i])
		i++
	}
	return result
}

// ============================================================================
// TurboSortIndex — sort index for high-performance sorting
// ============================================================================
//
// Two indexes are created for each sort:
//
// 1. Main index (key: "turbo_sort:<name>"):
//    - Stores docIDs in sort order (by price, rating, etc.).
//    - Format: [count: uint64][docID1: uint64][docID2: uint64]...
//    - docIDs are NOT sorted by docID; their order IS the sort order.
//    - Used for: pagination, range queries, sorted retrieval.
//
// 2. Position index (key: "turbo_sort_pos:<name>"):
//    - Stores (docID, position) pairs sorted by docID.
//    - Format: [count: uint64][docID1: uint64][pos1: uint64][docID2: uint64][pos2: uint64]...
//    - Used for: fast intersection with candidates (merge-style by docID).
//
// Operations:
// - Build: provide sorted docIDs + build position index.
// - Intersect: merge-style with position index → (docID, pos) → sort by pos.
// - Paginate: direct slice on main index.
// - Range: slice on main index by position range.
//
// No mutexes, no channels, no goroutines — only DB Get/Put/Delete + atomic ops.

var (
	turboSortHashEmpty     = uint64(0)
	turboSortHashTombstone = key128{^uint64(0), ^uint64(0)}
)

// Pool for radix sort buffers to reduce allocations in hot path.
// Note: this pool is used within a single request's execution,
// not between requests. It reduces repeated allocations for radix sort passes.
type radixSortBuffers struct {
	tempKeys    []uint64
	tempIndices []int
	indices     []int
	keysCopy    []uint64
}

var radixBufPool = sync.Pool{
	New: func() interface{} {
		return &radixSortBuffers{}
	},
}

// turboSortPosCache is a temporary hash table key128 -> position.
// Built on-demand for sort index intersection, used once, then discarded.
// Not stored in ShardedDB; no caching, no shared state.
// Each bucket stores: key128 (16 bytes) + position (8 bytes) = 24 bytes
type turboSortPosCache struct {
	data       []byte
	mask       uint64
	bucketSize int
}

// buildTurboSortPosCache builds a hash table key128 -> position from position index data.
// Allocates a new cache each time — no pooling between requests.
func buildTurboSortPosCache(posData []byte) *turboSortPosCache {
	if len(posData) < int(turboHeaderSize) {
		return nil
	}

	count := binary.LittleEndian.Uint64(posData)
	if count == 0 {
		return nil
	}

	// Choose bucket count: power of 2, load factor ~0.5
	bucketCount := uint64(1)
	for bucketCount < count*2 {
		bucketCount <<= 1
	}

	cache := &turboSortPosCache{
		data:       make([]byte, bucketCount*24),
		mask:       bucketCount - 1,
		bucketSize: int(bucketCount),
	}

	// Insert all (docID, position) pairs
	data := posData[turboHeaderSize:]
	for i := uint64(0); i < count; i++ {
		offset := i * turboSortPosEntrySize
		if offset+turboSortPosEntrySize > uint64(len(data)) {
			break
		}

		var docID key128
		docID[0] = binary.LittleEndian.Uint64(data[offset:])
		docID[1] = binary.LittleEndian.Uint64(data[offset+8:])
		pos := binary.LittleEndian.Uint64(data[offset+16:])

		cache.insert(docID, pos)
	}

	return cache
}

// insert inserts a (docID, position) pair into the hash table.
func (c *turboSortPosCache) insert(docID key128, pos uint64) {
	var emptyKey key128
	if docID == emptyKey {
		return
	}

	// Hash the key128 to uint64 using the first part
	h := docID[0]
	idx := h & c.mask

	for {
		offset := idx * 24

		// Read stored key128
		var storedKey key128
		storedKey[0] = binary.LittleEndian.Uint64(c.data[offset : offset+8])
		storedKey[1] = binary.LittleEndian.Uint64(c.data[offset+8 : offset+16])

		// Check for empty or tombstone
		if storedKey == emptyKey {
			// Store key128 + position
			binary.LittleEndian.PutUint64(c.data[offset:], docID[0])
			binary.LittleEndian.PutUint64(c.data[offset+8:], docID[1])
			binary.LittleEndian.PutUint64(c.data[offset+16:], pos)
			return
		}

		// Check for tombstone (all FFF...)
		var tombstoneKey key128
		tombstoneKey[0] = ^uint64(0)
		tombstoneKey[1] = ^uint64(0)
		if storedKey == tombstoneKey {
			// Overwrite tombstone
			binary.LittleEndian.PutUint64(c.data[offset:], docID[0])
			binary.LittleEndian.PutUint64(c.data[offset+8:], docID[1])
			binary.LittleEndian.PutUint64(c.data[offset+16:], pos)
			return
		}

		if storedKey == docID {
			// Update existing position
			binary.LittleEndian.PutUint64(c.data[offset+16:], pos)
			return
		}

		idx = (idx + 1) & c.mask
	}
}

// lookup finds the position for a docID in the hash table.
// Returns (position, ok) where ok is false if docID is not found.
func (c *turboSortPosCache) lookup(docID key128) (uint64, bool) {
	var emptyKey key128
	if docID == emptyKey {
		return 0, false
	}

	// Hash the key128 to uint64 using the first part
	h := docID[0]
	idx := h & c.mask

	for {
		offset := idx * 24

		// Read stored key128
		var storedKey key128
		storedKey[0] = binary.LittleEndian.Uint64(c.data[offset : offset+8])
		storedKey[1] = binary.LittleEndian.Uint64(c.data[offset+8 : offset+16])

		// Check for empty
		if storedKey == emptyKey {
			return 0, false
		}

		// Check for tombstone
		var tombstoneKey key128
		tombstoneKey[0] = ^uint64(0)
		tombstoneKey[1] = ^uint64(0)
		if storedKey == tombstoneKey {
			idx = (idx + 1) & c.mask
			continue
		}

		if storedKey == docID {
			return binary.LittleEndian.Uint64(c.data[offset+16:]), true
		}

		idx = (idx + 1) & c.mask
	}
}

// hashUint64 is a fast hash function for uint64.
func hashUint64(x uint64) uint64 {
	// MurmurHash3 finalizer style
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

// turboSortPosLookupBinarySearch looks up the position for a docID in the position index
// using binary search. Position index is sorted by docID.
// Returns (position, ok) where ok is false if docID is not found.
// Zero-allocation: reads directly from posData.
func turboSortPosLookupBinarySearch(posData []byte, docID key128) (uint64, bool) {
	if len(posData) < int(turboHeaderSize) {
		return 0, false
	}

	count := binary.LittleEndian.Uint64(posData)
	if count == 0 {
		return 0, false
	}

	data := posData[turboHeaderSize:]

	// Binary search on sorted (docID, position) pairs
	lo, hi := uint64(0), count-1

	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * turboSortPosEntrySize
		if offset+turboSortPosEntrySize > uint64(len(data)) {
			hi = mid - 1
			continue
		}

		var midDocID key128
		midDocID[0] = binary.LittleEndian.Uint64(data[offset:])
		midDocID[1] = binary.LittleEndian.Uint64(data[offset+8:])

		if midDocID == docID {
			return binary.LittleEndian.Uint64(data[offset+16:]), true
		}
		cmp := bytesCompareKey128(midDocID, docID)
		if cmp < 0 {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	return 0, false
}

// TurboSortIndexStats holds statistics about a TurboSortIndex.
type TurboSortIndexStats struct {
	Name      string
	Count     uint64
	SizeBytes uint64
}

// TurboBuildSortIndex builds a TurboSortIndex from docIDs already in sort order.
// The caller is responsible for providing docIDs in the correct sort order.
// This function also builds the position index for fast intersection.
//
// Returns the main index buffer. The position index must be stored separately.
func TurboBuildSortIndex(sortedDocIDs []key128) ([]byte, []byte) {
	if len(sortedDocIDs) == 0 {
		return nil, nil
	}

	// Main index: docIDs in sort order
	mainBuf := turboSerializeKey128(sortedDocIDs)

	// Position index: (docID, position) sorted by docID
	posBuf := turboBuildPositionIndexKey128(sortedDocIDs)

	return mainBuf, posBuf
}

// turboBuildPositionIndex builds the position index from sorted docIDs.
// Position index: (docID, position) sorted by docID.
func turboBuildPositionIndex(sortedDocIDs []uint64) []byte {
	n := len(sortedDocIDs)

	// Create (docID, position) pairs
	pairs := make([]uint64, n*2)
	for i, docID := range sortedDocIDs {
		pairs[i*2] = docID
		pairs[i*2+1] = uint64(i)
	}

	// Sort by docID using Radix Sort on pairs
	turboRadixSortDocIDPositionPairs(pairs)

	// Serialize
	buf := make([]byte, turboHeaderSize+uint64(n)*turboSortPosEntrySize)
	binary.LittleEndian.PutUint64(buf, uint64(n))

	offset := turboHeaderSize
	for i := 0; i < n*2; i++ {
		binary.LittleEndian.PutUint64(buf[offset:], pairs[i])
		offset += 8
	}

	return buf
}

// turboBuildPositionIndexKey128 builds the position index from sorted key128 docIDs.
// Position index: (docID, position) sorted by docID.
func turboBuildPositionIndexKey128(sortedDocIDs []key128) []byte {
	n := len(sortedDocIDs)

	// Create (docID, position) pairs as key128 + uint64
	// We'll create a temporary slice of struct { key128, position } for sorting
	type docIDPosPair struct {
		docID    key128
		position uint64
	}
	pairs := make([]docIDPosPair, n)
	for i, docID := range sortedDocIDs {
		pairs[i] = docIDPosPair{docID: docID, position: uint64(i)}
	}

	// Sort by docID using Radix Sort
	sort.Slice(pairs, func(i, j int) bool {
		return bytesCompareKey128(pairs[i].docID, pairs[j].docID) < 0
	})

	// Serialize
	buf := make([]byte, turboHeaderSize+uint64(n)*turboSortPosEntrySize)
	binary.LittleEndian.PutUint64(buf, uint64(n))

	offset := turboHeaderSize
	for i := 0; i < n; i++ {
		// Write key128 (16 bytes)
		binary.LittleEndian.PutUint64(buf[offset:], pairs[i].docID[0])
		binary.LittleEndian.PutUint64(buf[offset+8:], pairs[i].docID[1])
		// Write position (8 bytes)
		binary.LittleEndian.PutUint64(buf[offset+16:], pairs[i].position)
		offset += turboSortPosEntrySize
	}

	return buf
}

// turboRadixSortDocIDPositionPairs sorts (docID, position) pairs by docID.
// Input: [docID0, pos0, docID1, pos1, ...]
// Output: sorted by docID, positions follow their docIDs.
func turboRadixSortDocIDPositionPairs(pairs []uint64) {
	n := len(pairs) / 2
	if n <= 1 {
		return
	}

	// Extract docIDs
	docIDs := make([]uint64, n)
	for i := 0; i < n; i++ {
		docIDs[i] = pairs[i*2]
	}

	// Radix sort docIDs with indices
	indices := turboRadixSortIndices(docIDs)

	// Reorder pairs based on sorted indices
	sorted := make([]uint64, n*2)
	for i, idx := range indices {
		sorted[i*2] = pairs[idx*2]     // docID
		sorted[i*2+1] = pairs[idx*2+1] // position
	}
	copy(pairs, sorted)
}

// turboRadixSortIndices performs Radix Sort on keys and returns sorted indices.
func turboRadixSortIndices(keys []uint64) []int {
	n := len(keys)
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}

	if n <= 1 {
		return indices
	}

	const mask = 0xFF
	tempKeys := make([]uint64, n)
	tempIndices := make([]int, n)

	for shift := uint(0); shift < 64; shift += 8 {
		// Counting sort
		counts := [256]int{}
		for i := range keys {
			b := int((keys[i] >> shift) & mask)
			counts[b]++
		}

		// Prefix sums
		total := 0
		for i := range counts {
			c := counts[i]
			counts[i] = total
			total += c
		}

		// Place elements
		for i := range keys {
			b := int((keys[i] >> shift) & mask)
			pos := counts[b]
			tempKeys[pos] = keys[i]
			tempIndices[pos] = indices[i]
			counts[b]++
		}

		// Copy back
		copy(keys, tempKeys)
		copy(indices, tempIndices)
	}

	return indices
}

// turboRadixSortIndicesReuse performs Radix Sort using pooled buffers.
// Returns sorted indices. Caller must copy the result immediately.
func turboRadixSortIndicesReuse(keys []uint64) []int {
	n := len(keys)
	if n <= 1 {
		if n == 1 {
			// Return a small static slice for n==1
			return []int{0}
		}
		return nil
	}

	buf := radixBufPool.Get().(*radixSortBuffers)

	// Grow buffers if needed
	if cap(buf.keysCopy) < n {
		buf.keysCopy = make([]uint64, n)
	}
	if cap(buf.indices) < n {
		buf.indices = make([]int, n)
	}
	if cap(buf.tempKeys) < n {
		buf.tempKeys = make([]uint64, n)
	}
	if cap(buf.tempIndices) < n {
		buf.tempIndices = make([]int, n)
	}

	ks := buf.keysCopy[:n]
	copy(ks, keys)
	idx := buf.indices[:n]
	for i := range idx {
		idx[i] = i
	}
	tempKeys := buf.tempKeys[:n]
	tempIndices := buf.tempIndices[:n]

	const mask = 0xFF
	for shift := uint(0); shift < 64; shift += 8 {
		counts := [256]int{}
		for i := 0; i < n; i++ {
			b := int((ks[i] >> shift) & mask)
			counts[b]++
		}

		total := 0
		for i := range counts {
			c := counts[i]
			counts[i] = total
			total += c
		}

		for i := 0; i < n; i++ {
			b := int((ks[i] >> shift) & mask)
			pos := counts[b]
			tempKeys[pos] = ks[i]
			tempIndices[pos] = idx[i]
			counts[b]++
		}

		copy(ks, tempKeys)
		copy(idx, tempIndices)
	}

	// Copy result before returning buffer to pool.
	result := make([]int, n)
	copy(result, idx)

	radixBufPool.Put(buf)
	return result
}

// TurboSortIndexCount returns the number of entries in a TurboSortIndex.
func TurboSortIndexCount(data []byte) uint64 {
	if len(data) < int(turboHeaderSize) {
		return 0
	}
	return binary.LittleEndian.Uint64(data)
}

// TurboSortIndexGetDocID returns the docID at the given position in the sort order.
// This is for pagination: position 0 = first in sort order.
func TurboSortIndexGetDocID(data []byte, position int) (key128, bool) {
	if len(data) < int(turboHeaderSize) {
		return key128{}, false
	}

	count := binary.LittleEndian.Uint64(data)
	if position < 0 || uint64(position) >= count {
		return key128{}, false
	}

	data = data[turboHeaderSize:]
	offset := uint64(position) * turboIndexEntrySize
	if offset+turboIndexEntrySize > uint64(len(data)) {
		return key128{}, false
	}

	var docID key128
	docID[0] = binary.LittleEndian.Uint64(data[offset:])
	docID[1] = binary.LittleEndian.Uint64(data[offset+8:])
	return docID, true
}

// TurboSortIndexSlice returns a slice of docIDs from start to end (exclusive).
// Used for pagination and range queries.
func TurboSortIndexSlice(data []byte, start, end int) []uint64 {
	if len(data) < int(turboHeaderSize) {
		return nil
	}

	count := binary.LittleEndian.Uint64(data)
	if start < 0 || end < start || uint64(end) > count {
		return nil
	}

	length := end - start
	if length == 0 {
		return nil
	}

	result := make([]uint64, length)
	data = data[turboHeaderSize:]

	for i := 0; i < length; i++ {
		pos := start + i
		offset := uint64(pos) * 8
		result[i] = binary.LittleEndian.Uint64(data[offset:])
	}

	return result
}

// TurboSortIndexSliceKey128 returns a slice of key128 docIDs from a sort index.
func TurboSortIndexSliceKey128(data []byte, start, end int) []key128 {
	if len(data) < int(turboHeaderSize) {
		return nil
	}

	count := binary.LittleEndian.Uint64(data)
	if start < 0 || end < start || uint64(end) > count {
		return nil
	}

	length := end - start
	if length == 0 {
		return nil
	}

	result := make([]key128, length)
	data = data[turboHeaderSize:]

	for i := 0; i < length; i++ {
		pos := start + i
		offset := uint64(pos) * turboIndexEntrySize
		result[i][0] = binary.LittleEndian.Uint64(data[offset:])
		result[i][1] = binary.LittleEndian.Uint64(data[offset+8:])
	}

	return result
}

// TurboSortIndexPaginate returns a page of docIDs from the sort index.
// page is 0-based, pageSize is the number of items per page.
// Returns (docIDs, total, ok) where ok is false if page is out of bounds.
func TurboSortIndexPaginate(data []byte, page, pageSize int) ([]key128, uint64, bool) {
	if len(data) < int(turboHeaderSize) {
		return nil, 0, false
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return nil, 0, true
	}

	start := page * pageSize
	if start >= int(count) {
		return nil, count, false
	}

	end := start + pageSize
	if end > int(count) {
		end = int(count)
	}

	return TurboSortIndexSliceKey128(data, start, end), count, true
}

// ---- ShardedDB methods for TurboSortIndex ----

// TurboPutSortIndex stores a TurboSortIndex in the database.
// sortedDocIDs must be in the correct sort order.
func (s *ShardedDB) turboPutSortIndex(name string, sortedDocIDs []key128) error {
	if len(sortedDocIDs) == 0 {
		return nil
	}

	mainBuf, posBuf := TurboBuildSortIndex(sortedDocIDs)

	// Store main index
	if err := s.turboPut(turboSortKey(name), mainBuf); err != nil {
		return err
	}

	// Store position index
	if err := s.turboPut(turboSortPosKey(name), posBuf); err != nil {
		return err
	}

	return nil
}

// turboGetSortIndex retrieves the main sort index from the database.
func (s *ShardedDB) turboGetSortIndex(name string) ([]byte, error) {
	key := turboSortKey(name)
	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

// turboGetSortPositionIndex retrieves the position index from the database.
func (s *ShardedDB) turboGetSortPositionIndex(name string) ([]byte, error) {
	key := turboSortPosKey(name)
	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

// turboGetSortPositionIndexZeroAlloc retrieves the position index without allocation.
// Returns a direct view into the memory-mapped file.
func (s *ShardedDB) turboGetSortPositionIndexZeroAlloc(name string) ([]byte, error) {
	key := turboSortPosKey(name)
	val, err := s.turboGetZeroAlloc(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

// turboGetSortIndexZeroAlloc retrieves the main sort index without allocation.
// Returns a direct view into the memory-mapped file.
func (s *ShardedDB) turboGetSortIndexZeroAlloc(name string) ([]byte, error) {
	key := turboSortKey(name)
	val, err := s.turboGetZeroAlloc(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

// TurboDeleteSortIndex deletes a TurboSortIndex from the database.
func (s *ShardedDB) turboDeleteSortIndex(name string) error {
	// Delete main index
	if err := s.turboDelete(turboSortKey(name)); err != nil {
		return err
	}
	// Delete position index
	if err := s.turboDelete(turboSortPosKey(name)); err != nil {
		return err
	}

	return nil
}

// TurboSortIndexStats returns statistics about a TurboSortIndex.
func (s *ShardedDB) turboSortIndexStats(name string) (*TurboSortIndexStats, error) {
	data, err := s.turboGetSortIndex(name)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}

	count := TurboSortIndexCount(data)

	return &TurboSortIndexStats{
		Name:      name,
		Count:     count,
		SizeBytes: uint64(len(data)),
	}, nil
}

// TurboSortIndexIntersectWithCandidatesFromDB intersects candidates with a sort index.
// Returns docIDs in sort order.
// Uses zero-allocation read for position index.
func (s *ShardedDB) turboSortIndexIntersectWithCandidatesFromDB(name string, candidates []key128) ([]key128, error) {
	posData, err := s.turboGetSortPositionIndexZeroAlloc(name)
	if err != nil {
		return nil, err
	}
	if posData == nil {
		return nil, nil
	}

	cache := buildTurboSortPosCache(posData)
	if cache == nil {
		return nil, nil
	}

	return turboSortIntersectWithCache(cache, candidates), nil
}

// turboSortIntersectWithCache intersects candidates with a sort index using a cached hash table.
func turboSortIntersectWithCache(cache *turboSortPosCache, candidates []key128) []key128 {
	if len(candidates) == 0 {
		return nil
	}

	// Pre-allocate with capacity equal to candidates (max possible match)
	positions := make([]uint64, 0, len(candidates))
	resultDocIDs := make([]key128, 0, len(candidates))

	for _, docID := range candidates {
		if pos, ok := cache.lookup(docID); ok {
			positions = append(positions, pos)
			resultDocIDs = append(resultDocIDs, docID)
		}
	}

	if len(positions) == 0 {
		return nil
	}

	// Sort by position using Radix Sort with pooled buffers.
	// Copy result immediately: pooled indices are reused.
	sortedIndices := turboRadixSortIndicesReuse(positions)

	// Reorder docIDs according to sorted positions.
	// Pre-allocate result slice.
	docIDs := make([]key128, 0, len(resultDocIDs))
	for _, idx := range sortedIndices {
		if idx < 0 || idx >= len(resultDocIDs) {
			continue
		}
		docIDs = append(docIDs, resultDocIDs[idx])
	}

	return docIDs
}

// TurboSortIndexIntersectWithCandidatesRawFromDB intersects a turbo index buffer
// of candidates with a sort index. Returns docIDs in sort order.
// Builds a temporary hash table for position lookups (no caching).
func (s *ShardedDB) turboSortIndexIntersectWithCandidatesRawFromDB(name string, candidatesRaw []byte) ([]key128, error) {
	posData, err := s.turboGetSortPositionIndexZeroAlloc(name)
	if err != nil {
		return nil, err
	}
	if posData == nil {
		return nil, nil
	}

	cache := buildTurboSortPosCache(posData)
	if cache == nil {
		return nil, nil
	}

	// Read candidates into a fresh slice.
	candidateCount := binary.LittleEndian.Uint64(candidatesRaw)
	if candidateCount == 0 {
		return nil, nil
	}

	candidates := make([]key128, candidateCount)
	data := candidatesRaw[turboHeaderSize:]
	for i := uint64(0); i < candidateCount; i++ {
		offset := i * turboIndexEntrySize
		if offset+turboIndexEntrySize > uint64(len(data)) {
			break
		}
		candidates[i][0] = binary.LittleEndian.Uint64(data[offset:])
		candidates[i][1] = binary.LittleEndian.Uint64(data[offset+8:])
	}

	return turboSortIntersectWithCache(cache, candidates), nil
}

// TurboSortPageParams holds parameters for paginated sort index queries.
type TurboSortPageParams struct {
	// Name is the sort index name (e.g., "price_asc", "rating_desc").
	Name string
	// Candidates is an optional list of docIDs to intersect with the sort index.
	// If nil or empty, all docIDs from the sort index are used (pure pagination).
	Candidates any
	// Page is the 0-based page number.
	Page int
	// PageSize is the number of docIDs per page.
	PageSize int
	// Desc reverses the sort order (true = descending).
	Desc bool
}

// TurboSortPageResult holds the result of a paginated sort index query.
type TurboSortPageResult struct {
	// DocIDs is the slice of docIDs for the requested page, in sort order.
	// Returns key128 for use in further operations or document retrieval.
	DocIDs []key128
	// Total is the total number of docIDs that match the query (before pagination).
	Total uint64
}

// TurboSortIndexPageFromDB returns a page of docIDs from a TurboSortIndex.
// It intersects the sort index with the optional candidates list, sorts by position,
// and returns only the requested page slice — without loading the entire result into memory.
//
// If Candidates is nil or empty, it performs direct pagination on the sort index.
// If Desc is true, the returned page is reversed.
//
// Complexity:
//   - No candidates: O(1) — direct slice on the main index.
//   - With candidates: O(N_candidates log N_candidates) — hash lookup + Radix Sort + slice.
func (s *ShardedDB) turboSortIndexPageFromDB(params TurboSortPageParams) (TurboSortPageResult, error) {
	if params.PageSize <= 0 {
		return TurboSortPageResult{}, nil
	}

	// Fast path: no candidates → pure pagination on the main index.
	if params.Candidates == nil {
		return s.turboSortPageNoCandidates(params)
	}
	candidatesSlice := toKey128Slice(params.Candidates)
	if len(candidatesSlice) == 0 {
		return s.turboSortPageNoCandidates(params)
	}

	// General path: intersect candidates with sort index, then paginate.
	return s.turboSortPageWithCandidates(params)
}

// TurboSortIndexPageRawFromDB is like TurboSortIndexPageFromDB, but candidates
// are provided as a raw turbo index buffer (bitmap) instead of a []uint64 slice.
// This avoids an extra allocation when candidates come from TurboIntersectToBitmapSortedFromDB.
func (s *ShardedDB) turboSortIndexPageRawFromDB(name string, candidatesRaw []byte, page, pageSize int, desc bool) (TurboSortPageResult, error) {
	if pageSize <= 0 || candidatesRaw == nil {
		return TurboSortPageResult{}, nil
	}

	// Fast path: empty candidates bitmap.
	if len(candidatesRaw) < int(turboHeaderSize) {
		return TurboSortPageResult{}, nil
	}
	candidateCount := binary.LittleEndian.Uint64(candidatesRaw)
	if candidateCount == 0 {
		return TurboSortPageResult{}, nil
	}

	// Get position index for binary search / hash table lookup.
	posData, err := s.turboGetSortPositionIndexZeroAlloc(name)
	if err != nil {
		return TurboSortPageResult{}, err
	}
	if posData == nil {
		return TurboSortPageResult{}, nil
	}

	// Read candidates directly from raw bytes (no intermediate []uint64 allocation).
	candidatesData := candidatesRaw[turboHeaderSize:]

	// Hybrid strategy: choose lookup method based on sizes.
	posCount := uint64(0)
	if len(posData) >= int(turboHeaderSize) {
		posCount = binary.LittleEndian.Uint64(posData)
	}

	// Use binary search for typical cases.
	if int(candidateCount) < int(posCount/10) || posCount == 0 {
		return turboSortPageRawBinarySearch(posData, candidatesData, candidateCount, page, pageSize, desc), nil
	}

	// Fall back to hash table for very large candidate sets.
	// Decode candidates only when needed for hash table path.
	candidates := make([]key128, candidateCount)
	for i := uint64(0); i < candidateCount; i++ {
		offset := i * turboIndexEntrySize
		if offset+turboIndexEntrySize > uint64(len(candidatesData)) {
			candidateCount = i
			break
		}
		candidates[i][0] = binary.LittleEndian.Uint64(candidatesData[offset:])
		candidates[i][1] = binary.LittleEndian.Uint64(candidatesData[offset+8:])
	}

	// Convert []key128 to []any for TurboSortPageParams
	anyCandidates := key128ToAnySlice(candidates[:candidateCount])
	params := TurboSortPageParams{
		Name:       name,
		Candidates: anyCandidates,
		Page:       page,
		PageSize:   pageSize,
		Desc:       desc,
	}
	return s.turboSortPageWithCandidates(params)
}

// turboSortPageRawBinarySearch performs sort index pagination using binary search
// on the position index, reading candidates directly from raw bytes.
// No []key128 allocation for candidates — zero-copy where possible.
// Uses merge-style intersection when candidates are sorted (typical case).
func turboSortPageRawBinarySearch(posData []byte, candidatesData []byte, candidateCount uint64, page, pageSize int, desc bool) TurboSortPageResult {
	// Collect (position, docID) for candidates that exist in the sort index.
	// Position index is sorted by docID. Candidates from TurboBulkUnionSortedRaw
	// are also sorted. Use merge-style intersection instead of binary search.
	positions, resultDocIDs := turboSortIntersectRawMerge(posData, candidatesData, candidateCount)

	if len(positions) == 0 {
		return TurboSortPageResult{}
	}

	total := uint64(len(positions))

	// Check page bounds early.
	start := page * pageSize
	if start >= int(total) {
		return TurboSortPageResult{Total: total}
	}
	end := start + pageSize
	if end > int(total) {
		end = int(total)
	}

	// Sort by position using Radix Sort with pooled buffers.
	sortedIndices := turboRadixSortIndicesReuse(positions)

	// Apply descending order if requested.
	if desc {
		reverseSlice(sortedIndices)
	}

	// Extract only the requested page slice.
	pageSizeOut := end - start
	docIDs := make([]key128, 0, pageSizeOut)
	for i := 0; i < pageSizeOut; i++ {
		globalIdx := start + i
		if globalIdx < 0 || globalIdx >= len(sortedIndices) {
			continue
		}
		orderIdx := sortedIndices[globalIdx]
		if orderIdx < 0 || orderIdx >= len(resultDocIDs) {
			continue
		}
		docIDs = append(docIDs, resultDocIDs[orderIdx])
	}

	return TurboSortPageResult{
		DocIDs: docIDs,
		Total:  total,
	}
}

// turboSortIntersectRawMerge performs merge-style intersection between sorted candidates
// and the position index (sorted by docID). Both inputs are sorted uint64 arrays.
// Returns (positions, docIDs) for matching candidates.
// O(len(candidates) + len(position_index)) — much faster than binary search per candidate.
func turboSortIntersectRawMerge(posData []byte, candidatesData []byte, candidateCount uint64) ([]uint64, []key128) {
	if len(posData) < int(turboHeaderSize) || candidateCount == 0 {
		return nil, nil
	}

	posCount := binary.LittleEndian.Uint64(posData)
	if posCount == 0 {
		return nil, nil
	}

	posDataBody := posData[turboHeaderSize:]
	maxPositions := min(int(posCount), int(candidateCount))
	positions := make([]uint64, 0, maxPositions)
	resultDocIDs := make([]key128, 0, maxPositions)

	// Merge-style intersection: both sorted by docID
	candIdx := uint64(0)
	posIdx := uint64(0)

	for candIdx < candidateCount && posIdx < posCount {
		candOffset := candIdx * turboIndexEntrySize
		if candOffset+turboIndexEntrySize > uint64(len(candidatesData)) {
			break
		}
		var candDocID key128
		candDocID[0] = binary.LittleEndian.Uint64(candidatesData[candOffset:])
		candDocID[1] = binary.LittleEndian.Uint64(candidatesData[candOffset+8:])

		posOffset := posIdx * turboSortPosEntrySize
		if posOffset+turboSortPosEntrySize > uint64(len(posDataBody)) {
			break
		}
		var posDocID key128
		posDocID[0] = binary.LittleEndian.Uint64(posDataBody[posOffset:])
		posDocID[1] = binary.LittleEndian.Uint64(posDataBody[posOffset+8:])

		if candDocID == posDocID {
			pos := binary.LittleEndian.Uint64(posDataBody[posOffset+16:])
			positions = append(positions, pos)
			resultDocIDs = append(resultDocIDs, candDocID)
			candIdx++
			posIdx++
		} else if bytesCompareKey128(candDocID, posDocID) < 0 {
			candIdx++
		} else {
			posIdx++
		}
	}

	return positions, resultDocIDs
}

// turboSortPageNoCandidates handles pure pagination on the main sort index.
// Uses zero-allocation read for the main index.
func (s *ShardedDB) turboSortPageNoCandidates(params TurboSortPageParams) (TurboSortPageResult, error) {
	data, err := s.turboGetSortIndexZeroAlloc(params.Name)
	if err != nil {
		return TurboSortPageResult{}, err
	}
	if data == nil {
		return TurboSortPageResult{}, nil
	}

	count := TurboSortIndexCount(data)
	if count == 0 {
		return TurboSortPageResult{}, nil
	}

	start := params.Page * params.PageSize
	if start >= int(count) {
		return TurboSortPageResult{Total: count}, nil
	}

	var docIDs []key128

	if params.Desc {
		// For descending order, read from the end of the index.
		// Page 0 (desc) = last pageSize elements.
		descStart := int(count) - start - params.PageSize
		if descStart < 0 {
			descStart = 0
		}
		descEnd := int(count) - start
		if descEnd > int(count) {
			descEnd = int(count)
		}
		if descStart >= descEnd {
			return TurboSortPageResult{Total: count}, nil
		}
		// Read the slice and reverse it to get descending order.
		docIDs = TurboSortIndexSliceKey128(data, descStart, descEnd)
		if docIDs != nil {
			for i, j := 0, len(docIDs)-1; i < j; i, j = i+1, j-1 {
				docIDs[i], docIDs[j] = docIDs[j], docIDs[i]
			}
		}
	} else {
		end := start + params.PageSize
		if end > int(count) {
			end = int(count)
		}
		docIDs = TurboSortIndexSliceKey128(data, start, end)
	}

	if docIDs == nil {
		return TurboSortPageResult{Total: count}, nil
	}

	return TurboSortPageResult{
		DocIDs: docIDs,
		Total:  count,
	}, nil
}

// turboSortPageWithCandidates intersects candidates with the sort index,
// sorts by position, and returns only the requested page slice.
// Uses zero-allocation read for position index.
// Hybrid: uses binary search for small candidate sets, hash table for large ones.
func (s *ShardedDB) turboSortPageWithCandidates(params TurboSortPageParams) (TurboSortPageResult, error) {
	posData, err := s.turboGetSortPositionIndexZeroAlloc(params.Name)
	if err != nil {
		return TurboSortPageResult{}, err
	}
	if posData == nil {
		return TurboSortPageResult{}, nil
	}

	// Convert candidates to []key128 for internal processing
	candidatesKey128 := toKey128Slice(params.Candidates)
	if len(candidatesKey128) == 0 {
		return TurboSortPageResult{}, nil
	}

	// Hybrid strategy:
	// - Binary search: O(C * log(S)) where C=candidates, S=sort_index_size
	//   No allocation overhead, good for small/medium candidate sets.
	// - Hash table: O(S) build + O(C) lookup
	//   Good when C is very large relative to S.
	// Threshold: use binary search when C < S/10 (tuned based on profiling).
	posCount := uint64(0)
	if len(posData) >= int(turboHeaderSize) {
		posCount = binary.LittleEndian.Uint64(posData)
	}

	// Use binary search for typical cases (up to ~10% of sort index size)
	// This avoids the expensive hash table build that was 70% CPU in profiling.
	if len(candidatesKey128) < int(posCount/10) || posCount == 0 {
		return turboSortPageWithCandidatesBinarySearch(posData, candidatesKey128, params), nil
	}

	// Fall back to hash table for very large candidate sets
	cache := buildTurboSortPosCache(posData)
	if cache == nil {
		return TurboSortPageResult{}, nil
	}

	// Collect (position, docID) for candidates that exist in the sort index.
	positions := make([]uint64, 0, len(candidatesKey128))
	resultDocIDs := make([]key128, 0, len(candidatesKey128))

	for _, docID := range candidatesKey128 {
		if pos, ok := cache.lookup(docID); ok {
			positions = append(positions, pos)
			resultDocIDs = append(resultDocIDs, docID)
		}
	}

	if len(positions) == 0 {
		return TurboSortPageResult{}, nil
	}

	total := uint64(len(positions))

	// Check page bounds early.
	start := params.Page * params.PageSize
	if start >= int(total) {
		return TurboSortPageResult{Total: total}, nil
	}
	end := start + params.PageSize
	if end > int(total) {
		end = int(total)
	}

	// Sort by position using Radix Sort with pooled buffers.
	sortedIndices := turboRadixSortIndicesReuse(positions)

	// Apply descending order if requested (in-place on pooled slice is safe,
	// we only read from it below).
	if params.Desc {
		reverseSlice(sortedIndices)
	}

	// Extract only the requested page slice.
	pageSize := end - start
	docIDs := make([]key128, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		globalIdx := start + i
		if globalIdx < 0 || globalIdx >= len(sortedIndices) {
			continue
		}
		orderIdx := sortedIndices[globalIdx]
		if orderIdx < 0 || orderIdx >= len(resultDocIDs) {
			// Corrupted/stale index: skip this entry.
			continue
		}
		docIDs = append(docIDs, resultDocIDs[orderIdx])
	}

	return TurboSortPageResult{
		DocIDs: docIDs,
		Total:  total,
	}, nil
}

// turboSortPageWithCandidatesBinarySearch uses merge-style intersection on the position index
// instead of building a hash table. Much faster for typical query sizes.
// Assumes candidates are sorted (typical case from TurboBulkUnionSortedRaw).
func turboSortPageWithCandidatesBinarySearch(posData []byte, candidates []key128, params TurboSortPageParams) TurboSortPageResult {
	if len(candidates) == 0 {
		return TurboSortPageResult{}
	}

	// Use merge-style intersection: both candidates and position index are sorted by docID.
	positions, resultDocIDs := turboSortIntersectMerge(posData, candidates)

	if len(positions) == 0 {
		return TurboSortPageResult{}
	}

	total := uint64(len(positions))

	// Check page bounds early.
	start := params.Page * params.PageSize
	if start >= int(total) {
		return TurboSortPageResult{Total: total}
	}
	end := start + params.PageSize
	if end > int(total) {
		end = int(total)
	}

	// Sort by position using Radix Sort with pooled buffers.
	sortedIndices := turboRadixSortIndicesReuse(positions)

	// Apply descending order if requested (in-place on pooled slice is safe,
	// we only read from it below).
	if params.Desc {
		reverseSlice(sortedIndices)
	}

	// Extract only the requested page slice.
	pageSize := end - start
	docIDs := make([]key128, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		globalIdx := start + i
		if globalIdx < 0 || globalIdx >= len(sortedIndices) {
			continue
		}
		orderIdx := sortedIndices[globalIdx]
		if orderIdx < 0 || orderIdx >= len(resultDocIDs) {
			continue
		}
		docIDs = append(docIDs, resultDocIDs[orderIdx])
	}

	return TurboSortPageResult{
		DocIDs: docIDs,
		Total:  total,
	}
}

// turboSortIntersectMerge performs merge-style intersection between sorted candidates
// and the position index (sorted by docID).
// Returns (positions, docIDs) for matching candidates.
// O(len(candidates) + len(position_index)) — much faster than binary search per candidate.
func turboSortIntersectMerge(posData []byte, candidates []key128) ([]uint64, []key128) {
	if len(posData) < int(turboHeaderSize) || len(candidates) == 0 {
		return nil, nil
	}

	posCount := binary.LittleEndian.Uint64(posData)
	if posCount == 0 {
		return nil, nil
	}

	posDataBody := posData[turboHeaderSize:]
	maxPositions := min(int(posCount), len(candidates))
	positions := make([]uint64, 0, maxPositions)
	resultDocIDs := make([]key128, 0, maxPositions)

	// Merge-style intersection: both sorted by docID
	candIdx := 0
	posIdx := uint64(0)

	for candIdx < len(candidates) && posIdx < posCount {
		candDocID := candidates[candIdx]

		posOffset := posIdx * turboSortPosEntrySize
		if posOffset+turboSortPosEntrySize > uint64(len(posDataBody)) {
			break
		}
		posDocID := (*key128)(unsafe.Pointer(&posDataBody[posOffset]))

		cmp := bytesCompareKey128(candDocID, *posDocID)
		if cmp == 0 {
			pos := binary.LittleEndian.Uint64(posDataBody[posOffset+16:])
			positions = append(positions, pos)
			resultDocIDs = append(resultDocIDs, candDocID)
			candIdx++
			posIdx++
		} else if cmp < 0 {
			candIdx++
		} else {
			posIdx++
		}
	}

	return positions, resultDocIDs
}

// reverseSlice reverses a slice of ints in place.
func reverseSlice(s []int) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// TurboSortIndexPaginateFromDB returns a page of docIDs from a TurboSortIndex.
// Deprecated: use TurboSortIndexPageFromDB instead.
func (s *ShardedDB) turboSortIndexPaginateFromDB(name string, page, pageSize int) ([]key128, uint64, error) {
	params := TurboSortPageParams{
		Name:     name,
		Page:     page,
		PageSize: pageSize,
	}
	res, err := s.turboSortIndexPageFromDB(params)
	if err != nil {
		return nil, 0, err
	}
	return res.DocIDs, res.Total, nil
}

// TurboSortPageWithDocsParams holds parameters for paginated sort index queries
// that also fetch the actual document data.
type TurboSortPageWithDocsParams struct {
	// Name is the sort index name (e.g., "price_asc", "rating_desc").
	Name string
	// Candidates is an optional list of docIDs to intersect with the sort index.
	// If nil or empty, all docIDs from the sort index are used (pure pagination).
	// Supports: []any, []string, []key128
	Candidates any
	// Page is the 0-based page number.
	Page int
	// PageSize is the number of documents per page.
	PageSize int
	// Desc reverses the sort order (true = descending).
	Desc bool
	// DocPrefix is ignored. The docIDs should already contain any prefix
	// inside the hash.
	DocPrefix string
}

// TurboSortPageWithDocsResult holds the result of a paginated sort index query
// that includes the actual document data.
type TurboSortPageWithDocsResult struct {
	// DocIDs is the slice of docIDs for the requested page, in sort order.
	// Returns any for use in further operations.
	DocIDs []any
	// Docs is the slice of document values corresponding to DocIDs.
	// Missing documents are nil in their position.
	Docs [][]byte
	// Total is the total number of docIDs that match the query (before pagination).
	Total uint64
}

// TurboSortIndexPageWithDocsFromDB returns a page of documents from a TurboSortIndex.
// It performs intersection with candidates, sorts by position, paginates,
// and then fetches the actual document data in one call.
//
// Document keys are constructed as "doc:" + strconv.FormatUint(docID, 10).
//
// Example:
//
//	res, err := db.TurboSortIndexPageWithDocsFromDB(TurboSortPageWithDocsParams{
//	    Name:       "price_asc",
//	    Candidates: candidates,
//	    Page:       0,
//	    PageSize:   20,
//	    Desc:       false,
//	})
//	// res.DocIDs = [123, 456, ...]
//	// res.Docs   = [raw_json_123, raw_json_456, ...]
//	// res.Total  = 1500
func (s *ShardedDB) turboSortIndexPageWithDocsFromDB(params TurboSortPageWithDocsParams) (TurboSortPageWithDocsResult, error) {
	// Step 1: Get paginated docIDs.
	pageParams := TurboSortPageParams{
		Name:       params.Name,
		Candidates: params.Candidates,
		Page:       params.Page,
		PageSize:   params.PageSize,
		Desc:       params.Desc,
	}
	pageRes, err := s.turboSortIndexPageFromDB(pageParams)
	if err != nil {
		return TurboSortPageWithDocsResult{}, err
	}

	if len(pageRes.DocIDs) == 0 {
		return TurboSortPageWithDocsResult{Total: pageRes.Total}, nil
	}

	// Step 2: Fetch document data using MultiGetByKey128.
	// The docPrefix should already be part of the key128 hash.
	docs, err := s.MultiGetByKey128(pageRes.DocIDs)
	return TurboSortPageWithDocsResult{
		DocIDs: key128ToAnySlice(pageRes.DocIDs),
		Docs:   docs,
		Total:  pageRes.Total,
	}, nil
}

// turboSortIndexPageRawWithDocsFromDB is like turboSortIndexPageWithDocsFromDB,
// but candidates are provided as a raw turbo bitmap.
func (s *ShardedDB) turboSortIndexPageRawWithDocsFromDB(
	name string,
	candidatesRaw []byte,
	page, pageSize int,
	desc bool,
	docPrefix string,
) (TurboSortPageWithDocsResult, error) {
	if candidatesRaw == nil || len(candidatesRaw) == 0 {
		return TurboSortPageWithDocsResult{}, nil
	}

	// Step 1: Get paginated docIDs using raw candidates.
	pageRes, err := s.turboSortIndexPageRawFromDB(name, candidatesRaw, page, pageSize, desc)
	if err != nil {
		return TurboSortPageWithDocsResult{}, err
	}

	if len(pageRes.DocIDs) == 0 {
		return TurboSortPageWithDocsResult{Total: pageRes.Total}, nil
	}

	// Step 2: Fetch document data using MultiGetByKey128.
	// The docPrefix should already be part of the key128 hash.
	docs, err := s.MultiGetByKey128(pageRes.DocIDs)
	if err != nil {
		return TurboSortPageWithDocsResult{}, err
	}

	return TurboSortPageWithDocsResult{
		DocIDs: key128ToAnySlice(pageRes.DocIDs),
		Docs:   docs,
		Total:  pageRes.Total,
	}, nil
}

// ============================================================================
// Turbo Numeric Sort Index
//
// Stores (value, docID) pairs sorted by value (uint64).
// Layout: [count: uint64][value1: uint64][docID1: uint64][value2][docID2]...
//
// Use cases:
//   - Sort/filter by numeric fields: price, rating, score, timestamp.
//   - Range queries: get all docs where value in [min, max].
//   - Pagination by value.
//
// Operations are optimized for:
//   - Radix Sort on uint64 values.
//   - Binary search for range queries.
//   - Merge-style intersection on sorted data.
//   - Minimal allocations, no mutexes.
// ============================================================================

// TurboNumSortPair represents a (value, docID) pair for numeric sort index.
// DocID can be any type (uint64, string, etc.) and is internally converted to key128.
type TurboNumSortPair struct {
	Value uint64
	DocID any
}

// TurboNumSortStats holds statistics about a numeric sort index.
type TurboNumSortStats struct {
	Name      string
	Count     uint64
	SizeBytes uint64
}

// TurboNumSortRangeParams holds parameters for range queries on numeric sort index.
type TurboNumSortRangeParams struct {
	// MinValue is the minimum value (inclusive). Zero means no lower bound.
	MinValue uint64
	// MaxValue is the maximum value (inclusive). ^uint64(0) means no upper bound.
	MaxValue uint64
	// Page is the 0-based page number.
	Page int
	// PageSize is the number of results per page.
	PageSize int
}

// TurboNumSortRangeResult holds the result of a range query on numeric sort index.
type TurboNumSortRangeResult struct {
	// Pairs is the slice of (value, docID) pairs for the requested page, in value order.
	Pairs []TurboNumSortPair
	// Total is the total number of pairs that match the range (before pagination).
	Total uint64
}

// TurboPutNumSort adds or updates a (value, docID) pair in a numeric sort index.
// If docID already exists, its value is updated and the index is re-sorted.
// Returns true if this is a new docID, false if updated.
func (s *ShardedDB) turboPutNumSort(name string, value uint64, docID any) (bool, error) {
	added, err := s.turboPutNumSortBatch(name, []TurboNumSortPair{{Value: value, DocID: docID}})
	return added > 0, err
}

// TurboPutNumSortBatch adds or updates multiple (value, docID) pairs in a numeric sort index.
// If a docID already exists, its value is updated.
// Returns the number of new docIDs added.
func (s *ShardedDB) turboPutNumSortBatch(name string, pairs []TurboNumSortPair) (int, error) {
	if len(pairs) == 0 {
		return 0, nil
	}

	key := turboNumSortKey(name)

	// Read existing data
	val, err := s.turboGet(key)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return 0, err
	}

	// Parse existing pairs
	var existing []TurboNumSortPair
	if len(val) > 0 {
		parsed := turboReadNumSortPairs(val)
		if parsed == nil {
			return 0, ErrTurboCorrupt
		}
		existing = parsed
	}

	// Build map of existing docIDs for fast lookup/update
	existingMap := make(map[key128]*TurboNumSortPair, len(existing))
	for i := range existing {
		existingMap[existing[i].DocID.(key128)] = &existing[i]
	}

	// Process new pairs: update existing or mark as new
	newCount := 0
	for _, p := range pairs {
		pDocID := toKey128(p.DocID)
		if ep, ok := existingMap[pDocID]; ok {
			// Update existing
			ep.Value = p.Value
		} else {
			// New docID - convert to key128 for storage
			newPair := TurboNumSortPair{Value: p.Value, DocID: pDocID}
			existing = append(existing, newPair)
			existingMap[pDocID] = &existing[len(existing)-1]
			newCount++
		}
	}

	if len(existing) == 0 {
		return newCount, s.turboDelete(key)
	}

	// Sort by value using Radix Sort
	turboRadixSortNumSortPairs(existing)

	// Serialize and write main index
	buf := turboSerializeNumSortPairs(existing)
	if err := s.turboPut(key, buf); err != nil {
		return newCount, err
	}

	// Build and write reverse index (sorted by docID for O(log N) lookup)
	revBuf := turboBuildNumSortRevIndex(existing)
	if revBuf != nil {
		if err := s.turboPut(turboNumSortRevKey(name), revBuf); err != nil {
			return newCount, err
		}
	} else {
		// No pairs, delete reverse index if it exists
		_ = s.turboDelete(turboNumSortRevKey(name))
	}

	return newCount, nil
}

// TurboDeleteNumSortByDocID removes a docID from a numeric sort index.
// Returns true if the docID was found and removed.
func (s *ShardedDB) turboDeleteNumSortByDocID(name string, docID any) (bool, error) {
	key := turboNumSortKey(name)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	if len(val) == 0 {
		return false, nil
	}

	pairs := turboReadNumSortPairs(val)
	if pairs == nil {
		return false, ErrTurboCorrupt
	}

	// Find and remove (swap with last to avoid shift)
	for i, p := range pairs {
		if p.DocID == docID {
			last := len(pairs) - 1
			pairs[i] = pairs[last]
			pairs = pairs[:last]

			if len(pairs) == 0 {
				if err := s.turboDelete(key); err != nil {
					return true, err
				}
				// Also delete reverse index
				return true, s.turboDelete(turboNumSortRevKey(name))
			}

			buf := turboSerializeNumSortPairs(pairs)
			if err := s.turboPut(key, buf); err != nil {
				return true, err
			}

			// Rebuild reverse index
			revBuf := turboBuildNumSortRevIndex(pairs)
			if revBuf != nil {
				if err := s.turboPut(turboNumSortRevKey(name), revBuf); err != nil {
					return true, err
				}
			} else {
				_ = s.turboDelete(turboNumSortRevKey(name))
			}

			return true, nil
		}
	}

	return false, nil
}

// TurboDeleteNumSortByDocIDs removes multiple docIDs from a numeric sort index.
// Returns the number of docIDs that were actually removed.
func (s *ShardedDB) turboDeleteNumSortByDocIDs(name string, docIDs []any) (int, error) {
	if len(docIDs) == 0 {
		return 0, nil
	}

	key := turboNumSortKey(name)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if len(val) == 0 {
		return 0, nil
	}

	pairs := turboReadNumSortPairs(val)
	if pairs == nil {
		return 0, ErrTurboCorrupt
	}

	// Build set of docIDs to delete (as key128)
	deleteSet := make(map[key128]struct{}, len(docIDs))
	for _, docID := range docIDs {
		deleteSet[toKey128(docID)] = struct{}{}
	}

	// Filter out deleted docIDs
	remaining := make([]TurboNumSortPair, 0, len(pairs))
	removed := 0
	for _, p := range pairs {
		if _, ok := deleteSet[p.DocID.(key128)]; ok {
			removed++
		} else {
			remaining = append(remaining, p)
		}
	}

	if removed == 0 {
		return 0, nil
	}

	if len(remaining) == 0 {
		if err := s.turboDelete(key); err != nil {
			return removed, err
		}
		// Also delete reverse index
		return removed, s.turboDelete(turboNumSortRevKey(name))
	}

	buf := turboSerializeNumSortPairs(remaining)
	if err := s.turboPut(key, buf); err != nil {
		return removed, err
	}

	// Rebuild reverse index
	revBuf := turboBuildNumSortRevIndex(remaining)
	if revBuf != nil {
		if err := s.turboPut(turboNumSortRevKey(name), revBuf); err != nil {
			return removed, err
		}
	} else {
		_ = s.turboDelete(turboNumSortRevKey(name))
	}

	return removed, nil
}

// TurboGetNumSortRange returns pairs in the given value range, with pagination.
// Uses binary search for efficient range lookup.
func (s *ShardedDB) turboGetNumSortRange(name string, params TurboNumSortRangeParams) (TurboNumSortRangeResult, error) {
	key := turboNumSortKey(name)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return TurboNumSortRangeResult{}, nil
		}
		return TurboNumSortRangeResult{}, err
	}
	if len(val) == 0 {
		return TurboNumSortRangeResult{}, nil
	}

	// Find range boundaries using binary search
	startIdx := turboNumSortFindGE(val, params.MinValue)
	if startIdx < 0 {
		return TurboNumSortRangeResult{}, nil
	}

	endIdx := turboNumSortFindGT(val, params.MaxValue)
	if endIdx < 0 {
		endIdx = int(turboBinaryCount(val))
	}

	if startIdx >= endIdx {
		return TurboNumSortRangeResult{}, nil
	}

	total := uint64(endIdx - startIdx)

	// Apply pagination
	if params.PageSize <= 0 {
		params.PageSize = 100 // default
	}

	pageStart := startIdx + params.Page*params.PageSize
	pageEnd := pageStart + params.PageSize
	if pageStart >= endIdx {
		return TurboNumSortRangeResult{Total: total}, nil
	}
	if pageEnd > endIdx {
		pageEnd = endIdx
	}

	// Extract pairs
	pairs := make([]TurboNumSortPair, pageEnd-pageStart)
	for i := pageStart; i < pageEnd; i++ {
		pairs[i-pageStart] = turboGetNumSortPairAt(val, i)
	}

	return TurboNumSortRangeResult{
		Pairs: pairs,
		Total: total,
	}, nil
}

// TurboGetNumSortByDocID returns the value for a given docID in a numeric sort index.
// Uses O(log N) binary search on the reverse index (sorted by docID).
// Returns (value, ok) where ok is false if docID is not found.
func (s *ShardedDB) turboGetNumSortByDocID(name string, docID any) (uint64, bool, error) {
	// Use reverse index for O(log N) lookup
	revKey := turboNumSortRevKey(name)

	revVal, err := s.turboGet(revKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(revVal) == 0 {
		return 0, false, nil
	}

	// Binary search on reverse index
	value, found := turboNumSortRevFindByDocID(revVal, docID)
	if found {
		return value, true, nil
	}

	return 0, false, nil
}

// TurboMergeNumSort merges multiple source numeric sort indexes into a destination index.
// If a docID exists in multiple sources, the last one wins.
func (s *ShardedDB) turboMergeNumSort(dstName string, srcNames []string) error {
	if len(srcNames) == 0 {
		return nil
	}

	// Collect all pairs from sources
	allPairs := make(map[key128]uint64) // docID (key128) -> value

	for _, name := range srcNames {
		key := turboNumSortKey(name)
		val, err := s.turboGet(key)
		if err != nil {
			if !errors.Is(err, ErrKeyNotFound) {
				return err
			}
			continue
		}
		if len(val) == 0 {
			continue
		}

		pairs := turboReadNumSortPairs(val)
		if pairs != nil {
			for _, p := range pairs {
				allPairs[p.DocID.(key128)] = p.Value
			}
		}
	}

	if len(allPairs) == 0 {
		if err := s.turboDelete(turboNumSortKey(dstName)); err != nil {
			return err
		}
		return s.turboDelete(turboNumSortRevKey(dstName))
	}

	// Convert to slice
	pairs := make([]TurboNumSortPair, 0, len(allPairs))
	for docID, value := range allPairs {
		pairs = append(pairs, TurboNumSortPair{Value: value, DocID: docID})
	}

	// Sort by value
	turboRadixSortNumSortPairs(pairs)

	// Write to destination
	buf := turboSerializeNumSortPairs(pairs)
	if err := s.turboPut(turboNumSortKey(dstName), buf); err != nil {
		return err
	}

	// Build and write reverse index
	revBuf := turboBuildNumSortRevIndex(pairs)
	if revBuf != nil {
		return s.turboPut(turboNumSortRevKey(dstName), revBuf)
	}
	return nil
}

// TurboCleanNumSort removes a numeric sort index and its reverse index.
func (s *ShardedDB) turboCleanNumSort(name string) error {
	if err := s.turboDelete(turboNumSortKey(name)); err != nil {
		return err
	}
	return s.turboDelete(turboNumSortRevKey(name))
}

// TurboRebuildNumSort rebuilds a numeric sort index from raw pairs.
// Use this after bulk updates or corruption.
// Also rebuilds the reverse index.
func (s *ShardedDB) turboRebuildNumSort(name string, pairs []TurboNumSortPair) error {
	if len(pairs) == 0 {
		if err := s.turboDelete(turboNumSortKey(name)); err != nil {
			return err
		}
		return s.turboDelete(turboNumSortRevKey(name))
	}

	// Sort by value
	turboRadixSortNumSortPairs(pairs)

	// Serialize and write main index
	buf := turboSerializeNumSortPairs(pairs)
	if err := s.turboPut(turboNumSortKey(name), buf); err != nil {
		return err
	}

	// Build and write reverse index
	revBuf := turboBuildNumSortRevIndex(pairs)
	if revBuf != nil {
		return s.turboPut(turboNumSortRevKey(name), revBuf)
	}
	return nil
}

// TurboNumSortStats returns statistics about a numeric sort index.
func (s *ShardedDB) turboNumSortStats(name string) (*TurboNumSortStats, error) {
	key := turboNumSortKey(name)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return &TurboNumSortStats{Name: name, Count: 0, SizeBytes: 0}, nil
		}
		return nil, err
	}

	count := turboBinaryCount(val)

	return &TurboNumSortStats{
		Name:      name,
		Count:     count,
		SizeBytes: uint64(len(val)),
	}, nil
}

// TurboIntersectNumSortWithCandidates intersects a numeric sort index with a list of candidate docIDs.
// Returns pairs (value, docID) for candidates that exist in the index, sorted by value.
// Candidates are first sorted by docID for efficient merge-style intersection.
func (s *ShardedDB) turboIntersectNumSortWithCandidates(name string, candidates []any) ([]TurboNumSortPair, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	key := turboNumSortKey(name)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if len(val) == 0 {
		return nil, nil
	}

	// Convert candidates to key128 and sort
	sortCandidates := make([]key128, len(candidates))
	for i, c := range candidates {
		sortCandidates[i] = toKey128(c)
	}
	RadixSortKey128(sortCandidates)

	// Read index pairs (already sorted by value)
	pairs := turboReadNumSortPairs(val)
	if pairs == nil {
		return nil, ErrTurboCorrupt
	}

	// Build set of candidate docIDs for fast lookup
	candidateSet := make(map[key128]struct{}, len(sortCandidates))
	for _, docID := range sortCandidates {
		candidateSet[docID] = struct{}{}
	}

	// Filter pairs that are in candidates
	var result []TurboNumSortPair
	for _, p := range pairs {
		if _, ok := candidateSet[p.DocID.(key128)]; ok {
			result = append(result, p)
		}
	}

	return result, nil
}

// TurboGetNumSortRangeIntersectCandidates returns pairs in the given value range
// that are also in the candidates list, sorted by value.
//
// Algorithm:
//  1. Binary search for range [minValue, maxValue] in the sorted index.
//  2. Build hash set of candidates.
//  3. Scan only the range, collect pairs whose docID is in candidates.
//  4. Apply pagination.
//
// Complexity: O(log N + M + K), where:
//
//	N = index size, M = candidates count, K = range size.
func (s *ShardedDB) turboGetNumSortRangeIntersectCandidates(
	name string,
	minValue uint64,
	maxValue uint64,
	candidates []any,
	page int,
	pageSize int,
) (TurboNumSortRangeResult, error) {
	if len(candidates) == 0 {
		return TurboNumSortRangeResult{}, nil
	}

	key := turboNumSortKey(name)

	val, err := s.turboGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return TurboNumSortRangeResult{}, nil
		}
		return TurboNumSortRangeResult{}, err
	}
	if len(val) == 0 {
		return TurboNumSortRangeResult{}, nil
	}

	// Step 1: Binary search for range boundaries
	startIdx := turboNumSortFindGE(val, minValue)
	if startIdx < 0 {
		return TurboNumSortRangeResult{}, nil
	}

	endIdx := turboNumSortFindGT(val, maxValue)
	if endIdx < 0 {
		endIdx = int(turboBinaryCount(val))
	}

	if startIdx >= endIdx {
		return TurboNumSortRangeResult{}, nil
	}

	// Step 2: Build hash set of candidates (as key128)
	candidateSet := make(map[key128]struct{}, len(candidates))
	for _, docID := range candidates {
		candidateSet[toKey128(docID)] = struct{}{}
	}

	// Step 3: Scan range, collect matching pairs
	var matched []TurboNumSortPair
	for i := startIdx; i < endIdx; i++ {
		p := turboGetNumSortPairAt(val, i)
		if _, ok := candidateSet[p.DocID.(key128)]; ok {
			matched = append(matched, p)
		}
	}

	if len(matched) == 0 {
		return TurboNumSortRangeResult{}, nil
	}

	total := uint64(len(matched))

	// Step 4: Apply pagination
	if pageSize <= 0 {
		pageSize = 100
	}

	pageStart := page * pageSize
	pageEnd := pageStart + pageSize
	if pageStart >= len(matched) {
		return TurboNumSortRangeResult{Total: total}, nil
	}
	if pageEnd > len(matched) {
		pageEnd = len(matched)
	}

	return TurboNumSortRangeResult{
		Pairs: matched[pageStart:pageEnd],
		Total: total,
	}, nil
}

// turboGetNumSortRangeIntersectRaw returns docIDs in [minValue, maxValue] that
// are also in candidatesRaw, using merge-style intersection on sorted data.
// candidatesRaw must be a sorted turbo bitmap.
// Returns a raw turbo bitmap with matching docIDs (sorted by docID).
func (s *ShardedDB) turboGetNumSortRangeIntersectRaw(
	name string,
	minValue uint64,
	maxValue uint64,
	candidatesRaw []byte,
) ([]byte, error) {
	if candidatesRaw == nil || len(candidatesRaw) == 0 {
		return nil, nil
	}

	key := turboNumSortKey(name)
	// Zero-alloc: safe because val is used only within this function
	// (binary search + extract docIDs into a new buffer). The view is not returned.
	val, err := s.turboGetZeroAlloc(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if len(val) == 0 {
		return nil, nil
	}

	// Step 1: Binary search for range boundaries
	startIdx := turboNumSortFindGE(val, minValue)
	if startIdx < 0 {
		return nil, nil
	}

	endIdx := turboNumSortFindGT(val, maxValue)
	if endIdx < 0 {
		endIdx = int(turboBinaryCount(val))
	}

	if startIdx >= endIdx {
		return nil, nil
	}

	// Step 2: Extract docIDs from range (sorted by value, not by docID).
	rangeCount := endIdx - startIdx
	rangeDocIDs := make([]uint64, rangeCount)
	rangeData := val[turboHeaderSize:]
	for i := startIdx; i < endIdx; i++ {
		offset := i * int(turboNumSortEntrySize)
		rangeDocIDs[i-startIdx] = binary.LittleEndian.Uint64(rangeData[offset+8:]) // docID is at offset+8
	}

	// Sort range docIDs using Radix Sort (faster than sort.Slice)
	RadixSortUint64(rangeDocIDs)

	// Step 3: Merge-style intersection directly on raw candidates.
	candidateCount := TurboBinaryCount(candidatesRaw)
	if candidateCount == 0 {
		return nil, nil
	}
	candidateData := candidatesRaw[turboHeaderSize:]

	// Pre-allocate result buffer
	buf := make([]byte, turboHeaderSize+uint64(rangeCount)*8)
	out := uint64(0)

	i, j := uint64(0), uint64(0)
	for i < uint64(len(rangeDocIDs)) && j < candidateCount {
		va := rangeDocIDs[i]
		vb := binary.LittleEndian.Uint64(candidateData[j*8:])

		if va == vb {
			offset := turboHeaderSize + out*8
			binary.LittleEndian.PutUint64(buf[offset:], va)
			out++
			i++
			j++
		} else if va < vb {
			i++
		} else {
			j++
		}
	}

	if out == 0 {
		return nil, nil
	}

	binary.LittleEndian.PutUint64(buf, out)
	return buf[:turboHeaderSize+out*8], nil
}

// ---- Internal helpers for numeric sort index ----

// turboReadNumSortPairs reads (value, docID) pairs from raw data.
func turboReadNumSortPairs(data []byte) []TurboNumSortPair {
	if len(data) < int(turboHeaderSize) {
		return nil
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return nil
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboNumSortEntrySize {
		return nil
	}

	pairs := make([]TurboNumSortPair, count)
	for i := uint64(0); i < count; i++ {
		offset := i * turboNumSortEntrySize
		pairs[i].Value = binary.LittleEndian.Uint64(tokenData[offset:])
		// Read docID as key128 (16 bytes)
		var docIDKey128 key128
		docIDKey128[0] = binary.LittleEndian.Uint64(tokenData[offset+8:])
		docIDKey128[1] = binary.LittleEndian.Uint64(tokenData[offset+16:])
		pairs[i].DocID = docIDKey128
	}

	return pairs
}

// turboSerializeNumSortPairs serializes (value, docID) pairs to raw format.
// Each entry: 8 bytes (value) + 16 bytes (docID as key128) = 24 bytes
func turboSerializeNumSortPairs(pairs []TurboNumSortPair) []byte {
	if len(pairs) == 0 {
		return nil
	}

	buf := make([]byte, turboHeaderSize+uint64(len(pairs))*turboNumSortEntrySize)
	binary.LittleEndian.PutUint64(buf, uint64(len(pairs)))

	offset := turboHeaderSize
	for _, p := range pairs {
		binary.LittleEndian.PutUint64(buf[offset:], p.Value)
		// Convert docID to key128 and write as 16 bytes
		docIDKey128 := toKey128(p.DocID)
		binary.LittleEndian.PutUint64(buf[offset+8:], docIDKey128[0])
		binary.LittleEndian.PutUint64(buf[offset+16:], docIDKey128[1])
		offset += turboNumSortEntrySize
	}

	return buf
}

// turboRadixSortNumSortPairs sorts (value, docID) pairs by value using Radix Sort.
func turboRadixSortNumSortPairs(pairs []TurboNumSortPair) {
	n := len(pairs)
	if n <= 1 {
		return
	}

	// Extract values
	values := make([]uint64, n)
	for i := range pairs {
		values[i] = pairs[i].Value
	}

	// Radix sort values with indices
	indices := turboRadixSortIndices(values)

	// Reorder pairs based on sorted indices
	sorted := make([]TurboNumSortPair, n)
	for i, idx := range indices {
		sorted[i] = pairs[idx]
	}
	copy(pairs, sorted)
}

// turboNumSortFindGE returns the index of the first pair with value >= target.
// Uses binary search. Returns -1 if not found.
func turboNumSortFindGE(data []byte, target uint64) int {
	if len(data) < int(turboHeaderSize) {
		return -1
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return -1
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboNumSortEntrySize {
		return -1
	}

	lo, hi := 0, int(count)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * int(turboNumSortEntrySize)
		value := binary.LittleEndian.Uint64(tokenData[offset:])
		if value >= target {
			if mid == 0 || binary.LittleEndian.Uint64(tokenData[(mid-1)*int(turboNumSortEntrySize):]) < target {
				return mid
			}
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}

	return -1
}

// turboNumSortFindGT returns the index after the last pair with value <= target.
// Uses binary search. Returns -1 if all values are <= target.
func turboNumSortFindGT(data []byte, target uint64) int {
	if len(data) < int(turboHeaderSize) {
		return -1
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return -1
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboNumSortEntrySize {
		return -1
	}

	lo, hi := 0, int(count)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * int(turboNumSortEntrySize)
		value := binary.LittleEndian.Uint64(tokenData[offset:])
		if value > target {
			if mid == 0 || binary.LittleEndian.Uint64(tokenData[(mid-1)*int(turboNumSortEntrySize):]) <= target {
				return mid
			}
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}

	return -1
}

// turboGetNumSortPairAt returns the pair at the given index.
func turboGetNumSortPairAt(data []byte, index int) TurboNumSortPair {
	tokenData := data[turboHeaderSize:]
	offset := index * int(turboNumSortEntrySize)
	// Read docID as key128 (16 bytes)
	var docIDKey128 key128
	docIDKey128[0] = binary.LittleEndian.Uint64(tokenData[offset+8:])
	docIDKey128[1] = binary.LittleEndian.Uint64(tokenData[offset+16:])
	return TurboNumSortPair{
		Value: binary.LittleEndian.Uint64(tokenData[offset:]),
		DocID: docIDKey128,
	}
}

// turboBinaryCount returns the count from turbo index data header.
func turboBinaryCount(data []byte) uint64 {
	if len(data) < int(turboHeaderSize) {
		return 0
	}
	return binary.LittleEndian.Uint64(data)
}

// ============================================================================
// TurboNumSort Reverse Index helpers
// ============================================================================

// turboBuildNumSortRevIndex builds a reverse index from (value, docID) pairs.
// The reverse index stores (docID, value) pairs sorted by docID for O(log N) lookup.
// Format: [count: uint64][docID1: uint64][value1: uint64][docID2: uint64][value2: uint64]...
func turboBuildNumSortRevIndex(pairs []TurboNumSortPair) []byte {
	n := len(pairs)
	if n == 0 {
		return nil
	}

	// Make a copy of pairs for sorting
	sortedPairs := make([]TurboNumSortPair, n)
	copy(sortedPairs, pairs)

	// Sort by docID (key128)
	sort.Slice(sortedPairs, func(i, j int) bool {
		return bytesCompareKey128(sortedPairs[i].DocID.(key128), sortedPairs[j].DocID.(key128)) < 0
	})

	// Serialize: header + (docID, value) pairs sorted by docID
	buf := make([]byte, turboHeaderSize+uint64(n)*turboNumSortRevEntrySize)
	binary.LittleEndian.PutUint64(buf, uint64(n))

	offset := turboHeaderSize
	for i := 0; i < n; i++ {
		// Write docID as key128 (16 bytes)
		docIDKey128 := sortedPairs[i].DocID.(key128)
		binary.LittleEndian.PutUint64(buf[offset:], docIDKey128[0])
		binary.LittleEndian.PutUint64(buf[offset+8:], docIDKey128[1])
		binary.LittleEndian.PutUint64(buf[offset+16:], sortedPairs[i].Value)
		offset += turboNumSortRevEntrySize
	}

	return buf
}

// turboNumSortRevFindByDocID returns the value for a given docID from reverse index data.
// Uses binary search for O(log N) lookup.
// Returns (value, found) where found is true if docID exists.
func turboNumSortRevFindByDocID(data []byte, docID any) (uint64, bool) {
	if len(data) < int(turboHeaderSize) {
		return 0, false
	}

	count := binary.LittleEndian.Uint64(data)
	if count == 0 {
		return 0, false
	}

	tokenData := data[turboHeaderSize:]
	if uint64(len(tokenData)) < count*turboNumSortRevEntrySize {
		return 0, false
	}

	// Convert docID to key128 for comparison
	targetDocID := toKey128(docID)

	// Binary search for docID in sorted (docID, value) pairs
	lo, hi := 0, int(count)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * int(turboNumSortRevEntrySize)
		// Read docID as key128 (16 bytes)
		var midDocID key128
		midDocID[0] = binary.LittleEndian.Uint64(tokenData[offset:])
		midDocID[1] = binary.LittleEndian.Uint64(tokenData[offset+8:])

		if midDocID == targetDocID {
			return binary.LittleEndian.Uint64(tokenData[offset+16:]), true
		}
		// Compare key128
		if bytesCompareKey128(midDocID, targetDocID) < 0 {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	return 0, false
}

// ============================================================================
// Public API wrappers
// ============================================================================
// All public methods are thin wrappers around private implementations.
// They handle internal key prefixes transparently.

// TurboTopNByIntersection returns top N tokens by intersection count.
func (s *ShardedDB) TurboTopNByIntersection(queryKey string, candidateKeys []string, limit int) ([]TurboIndexIntersectionResult, error) {
	// Convert string keys to key128
	queryKey128 := hashToken(queryKey)
	candidateKeys128 := make([]key128, len(candidateKeys))
	for i, key := range candidateKeys {
		candidateKeys128[i] = hashToken(key)
	}
	return s.turboTopNByIntersection(queryKey128, candidateKeys128, limit)
}

// TurboPutIndex adds a key128 token to a turbo index.
func (s *ShardedDB) TurboPutIndex(token string, docID any) (bool, error) {
	return s.turboPutIndex(turboKey(token), toKey128(docID))
}

// TurboDeleteIndex removes a token from a turbo index.
func (s *ShardedDB) TurboDeleteIndex(token string, docID any) (bool, error) {
	return s.turboDeleteIndex(turboKey(token), toKey128(docID))
}

// TurboGetIndexTokens returns all document IDs for an index.
// Use these tokens for further intersection operations or document retrieval.
// Returns nil if index doesn't exist.
func (s *ShardedDB) TurboGetIndexTokens(token string) ([]any, error) {
	tokens, err := s.turboGetIndexTokens(token)
	return key128ToAnySlice(tokens), err
}

// TurboContainsIndex checks if a token contains a docID.
func (s *ShardedDB) TurboContainsIndex(token string, docID any) (bool, error) {
	return s.turboContainsIndex(turboKey(token), toKey128(docID))
}

// TurboCountIndex returns the number of tokens in an index.
func (s *ShardedDB) TurboCountIndex(token string) (uint64, error) {
	return s.turboCountIndex(token)
}

// TurboIntersectIndexResults intersects multiple index conditions.
// Returns tokens that are present in ALL specified conditions.
// Use these tokens for further operations or document retrieval.
func (s *ShardedDB) TurboIntersectIndexResults(conditions []TurboIndexCondition) ([]any, error) {
	tokens, err := s.turboIntersectIndexResults(conditions)
	return key128ToAnySlice(tokens), err
}

// TurboSearch intersects multiple index tokens.
// Returns tokens that are present in ALL specified index tokens.
// Use these tokens for further operations or document retrieval.
func (s *ShardedDB) TurboSearch(indexTokens []string) ([]any, error) {
	tokens, err := s.turboSearch(indexTokens)
	return key128ToAnySlice(tokens), err
}

// TurboPutBatchIndex adds multiple tokens to an index.
func (s *ShardedDB) TurboPutBatchIndex(token string, docIDs []any) (int, error) {
	return s.turboPutBatchIndex(token, toKey128Slice(docIDs))
}

// TurboDeleteBatchIndex removes multiple tokens from an index.
func (s *ShardedDB) TurboDeleteBatchIndex(token string, docIDs []any) (int, error) {
	return s.turboDeleteBatchIndex(token, toKey128Slice(docIDs))
}

// TurboGetIndexTokensFiltered returns filtered document IDs for an index as key128.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboGetIndexTokensFiltered(token string, include []any, exclude []any, reverse bool, limit, offset int) ([]any, error) {
	tokens, err := s.turboGetIndexTokensFiltered(token, toKey128Slice(include), toKey128Slice(exclude), reverse, limit, offset)
	return key128ToAnySlice(tokens), err
}

// TurboClearIndex clears all tokens from an index.
func (s *ShardedDB) TurboClearIndex(token string) error {
	return s.turboClearIndex(token)
}

// TurboListTokens returns all document IDs for an index, sorted in ascending order.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboListTokens(token string) ([]any, error) {
	tokens, err := s.turboListTokens(token)
	return key128ToAnySlice(tokens), err
}

// TurboIntersectAll intersects all given index tokens.
// Returns document IDs that are present in ALL specified index tokens.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboIntersectAll(indexTokens []string) ([]any, error) {
	tokens, err := s.turboIntersectAll(indexTokens)
	return key128ToAnySlice(tokens), err
}

// TurboUnionAll unions all given index tokens.
// Returns document IDs (each appears only once).
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboUnionAll(indexTokens []string) ([]any, error) {
	tokens, err := s.turboUnionAll(indexTokens)
	return key128ToAnySlice(tokens), err
}

// TurboDiff returns document IDs in baseToken that are not in any excludeTokens.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboDiff(baseToken string, excludeTokens []string) ([]any, error) {
	tokens, err := s.turboDiff(baseToken, excludeTokens)
	return key128ToAnySlice(tokens), err
}

// TurboIndexStats returns statistics about an index.
func (s *ShardedDB) TurboIndexStats(token string) (*TurboIndexStats, error) {
	return s.turboIndexStats(token)
}

// TurboCompactIndex compacts an index.
func (s *ShardedDB) TurboCompactIndex(token string) (int, error) {
	return s.turboCompactIndex(token)
}

// TurboBulkIntersect intersects multiple indexes using bulk operations.
// Returns document IDs that are present in ALL specified indexes.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboBulkIntersect(indexTokens []string) ([]any, error) {
	tokens, err := s.turboBulkIntersect(indexTokens)
	return key128ToAnySlice(tokens), err
}

// TurboBulkIntersectRaw intersects multiple indexes and returns raw turbo bitmap.
// Much faster than TurboBulkIntersect: reads raw data, uses TurboBinaryIntersectRaw,
// no []key128 allocations for intermediate results.
// Returns a turbo index buffer: [count: uint64][tokens: key128 x count].
func (s *ShardedDB) TurboBulkIntersectRaw(indexTokens []string) ([]byte, error) {
	return s.turboBulkIntersectRaw(indexTokens)
}

// TurboBulkUnion unions multiple indexes using bulk operations.
// Uses map-based union — OK for small indexes, slow for large ones.
// For large sorted indexes, prefer TurboBulkUnionSorted.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboBulkUnion(indexTokens []string) ([]any, error) {
	tokens, err := s.turboBulkUnion(indexTokens)
	return key128ToAnySlice(tokens), err
}

// TurboBulkUnionSorted unions multiple indexes using merge-style union on sorted data.
// Much faster than TurboBulkUnion for large indexes (e.g., 92 indexes with 240k IDs each).
// No map allocations; uses TurboBinaryUnionRaw internally.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboBulkUnionSorted(indexTokens []string) ([]any, error) {
	tokens, err := s.turboBulkUnionSorted(indexTokens)
	return key128ToAnySlice(tokens), err
}

// TurboBulkUnionSortedRaw is like TurboBulkUnionSorted, but returns raw turbo bitmap.
// Avoids []key128 allocation when result is used for further raw operations.
func (s *ShardedDB) TurboBulkUnionSortedRaw(indexTokens []string) ([]byte, error) {
	return s.turboBulkUnionSortedRaw(indexTokens)
}

// TurboContainsAll checks if an index contains all given docIDs.
func (s *ShardedDB) TurboContainsAll(token string, docIDs []any) (bool, error) {
	return s.turboContainsAll(token, toKey128Slice(docIDs))
}

// TurboContainsAny checks if an index contains any of the given docIDs.
func (s *ShardedDB) TurboContainsAny(token string, docIDs []any) (bool, error) {
	return s.turboContainsAny(token, toKey128Slice(docIDs))
}

// TurboMergeIndexes merges multiple indexes into one.
func (s *ShardedDB) TurboMergeIndexes(destToken string, srcTokens []string) error {
	return s.turboMergeIndexes(destToken, srcTokens)
}

// TurboSplitIndex splits an index based on a predicate.
// The predicate receives document IDs as any.
// Tokens for which predicate returns true go to trueToken, others to falseToken.
func (s *ShardedDB) TurboSplitIndex(srcToken string, trueToken string, falseToken string, predicate func(any) bool) error {
	return s.turboSplitIndex(srcToken, trueToken, falseToken, func(k key128) bool {
		return predicate(k)
	})
}

// TurboCopyIndex copies an index.
func (s *ShardedDB) TurboCopyIndex(srcToken string, destToken string) error {
	return s.turboCopyIndex(srcToken, destToken)
}

// TurboRawRead reads raw turbo index data.
func (s *ShardedDB) TurboRawRead(token string) ([]byte, error) {
	return s.turboRawRead(token)
}

// TurboRawWrite writes raw turbo index data.
func (s *ShardedDB) TurboRawWrite(token string, data []byte) error {
	return s.turboRawWrite(token, data)
}

// TurboAtomicGetCount gets count atomically.
func (s *ShardedDB) TurboAtomicGetCount(token string) (uint64, error) {
	return s.turboAtomicGetCount(token)
}

// TurboAtomicContains checks contains atomically.
func (s *ShardedDB) TurboAtomicContains(token string, docID any) (bool, error) {
	return s.turboAtomicContains(token, toKey128(docID))
}

// TurboAtomicGetTokens gets document IDs atomically.
// Use returned tokens for further intersection operations or document retrieval.
func (s *ShardedDB) TurboAtomicGetTokens(token string) ([]any, error) {
	tokens, err := s.turboAtomicGetTokens(token)
	return key128ToAnySlice(tokens), err
}

// TurboIntersectCountSortedFromDB intersects sorted indexes and returns count.
func (s *ShardedDB) TurboIntersectCountSortedFromDB(indexTokens []string) (uint64, error) {
	return s.turboIntersectCountSortedFromDB(indexTokens)
}

// TurboIntersectToBitmapSortedFromDB intersects sorted indexes and returns bitmap.
func (s *ShardedDB) TurboIntersectToBitmapSortedFromDB(indexTokens []string) ([]byte, error) {
	return s.turboIntersectToBitmapSortedFromDB(indexTokens)
}

// TurboIntersectCountFromDB intersects indexes and returns count.
func (s *ShardedDB) TurboIntersectCountFromDB(indexTokens []string) (uint64, error) {
	return s.turboIntersectCountFromDB(indexTokens)
}

// TurboIntersectToBitmapFromDB intersects indexes and returns bitmap.
func (s *ShardedDB) TurboIntersectToBitmapFromDB(indexTokens []string) ([]byte, error) {
	return s.turboIntersectToBitmapFromDB(indexTokens)
}

// TurboUnionToBitmapFromDB unions indexes and returns bitmap.
func (s *ShardedDB) TurboUnionToBitmapFromDB(indexTokens []string) ([]byte, error) {
	return s.turboUnionToBitmapFromDB(indexTokens)
}

// TurboAndFromDB returns intersection of two indexes as bitmap.
func (s *ShardedDB) TurboAndFromDB(a, b string) ([]byte, error) {
	return s.turboAndFromDB(a, b)
}

// TurboOrFromDB returns union of two indexes as bitmap.
func (s *ShardedDB) TurboOrFromDB(a, b string) ([]byte, error) {
	return s.turboOrFromDB(a, b)
}

// TurboAndNotFromDB returns a-b as bitmap.
func (s *ShardedDB) TurboAndNotFromDB(a, b string) ([]byte, error) {
	return s.turboAndNotFromDB(a, b)
}

// TurboPutSortIndex stores a sort index.
func (s *ShardedDB) TurboPutSortIndex(name string, sortedDocIDs []any) error {
	return s.turboPutSortIndex(name, toKey128Slice(sortedDocIDs))
}

// TurboDeleteSortIndex deletes a sort index.
func (s *ShardedDB) TurboDeleteSortIndex(name string) error {
	return s.turboDeleteSortIndex(name)
}

// TurboSortIndexStats returns sort index statistics.
func (s *ShardedDB) TurboSortIndexStats(name string) (*TurboSortIndexStats, error) {
	return s.turboSortIndexStats(name)
}

// TurboSortIndexIntersectWithCandidatesFromDB intersects candidates with sort index.
func (s *ShardedDB) TurboSortIndexIntersectWithCandidatesFromDB(name string, candidates []any) ([]any, error) {
	tokens, err := s.turboSortIndexIntersectWithCandidatesFromDB(name, toKey128Slice(candidates))
	return key128ToAnySlice(tokens), err
}

// TurboSortIndexIntersectWithCandidatesRawFromDB intersects raw candidates with sort index.
func (s *ShardedDB) TurboSortIndexIntersectWithCandidatesRawFromDB(name string, candidatesRaw []byte) ([]any, error) {
	tokens, err := s.turboSortIndexIntersectWithCandidatesRawFromDB(name, candidatesRaw)
	return key128ToAnySlice(tokens), err
}

// TurboSortIndexPageFromDB returns paginated results from sort index.
func (s *ShardedDB) TurboSortIndexPageFromDB(params TurboSortPageParams) (TurboSortPageResult, error) {
	return s.turboSortIndexPageFromDB(params)
}

// TurboSortIndexPageRawFromDB returns paginated results from raw candidates.
func (s *ShardedDB) TurboSortIndexPageRawFromDB(name string, candidatesRaw []byte, page, pageSize int, desc bool) (TurboSortPageResult, error) {
	return s.turboSortIndexPageRawFromDB(name, candidatesRaw, page, pageSize, desc)
}

// TurboSortIndexPaginateFromDB returns paginated docIDs from sort index.
func (s *ShardedDB) TurboSortIndexPaginateFromDB(name string, page, pageSize int) ([]any, uint64, error) {
	data, err := s.turboGetSortIndex(name)
	if err != nil {
		return nil, 0, err
	}
	if data == nil {
		return nil, 0, nil
	}
	docIDs, total, _ := TurboSortIndexPaginate(data, page, pageSize)
	return key128ToAnySlice(docIDs), total, nil
}

// TurboSortIndexPageWithDocsFromDB returns paginated results with document data.
func (s *ShardedDB) TurboSortIndexPageWithDocsFromDB(params TurboSortPageWithDocsParams) (TurboSortPageWithDocsResult, error) {
	return s.turboSortIndexPageWithDocsFromDB(params)
}

// TurboSortIndexPageRawWithDocsFromDB is like TurboSortIndexPageWithDocsFromDB,
// but candidates are provided as a raw turbo bitmap instead of []uint64.
// This avoids []uint64 allocation when candidates come from TurboBulkIntersectRaw.
func (s *ShardedDB) TurboSortIndexPageRawWithDocsFromDB(
	name string,
	candidatesRaw []byte,
	page, pageSize int,
	desc bool,
	docPrefix string,
) (TurboSortPageWithDocsResult, error) {
	return s.turboSortIndexPageRawWithDocsFromDB(name, candidatesRaw, page, pageSize, desc, docPrefix)
}

// TurboPutNumSort adds a value to numeric sort index.
func (s *ShardedDB) TurboPutNumSort(name string, value uint64, docID any) (bool, error) {
	return s.turboPutNumSort(name, value, docID)
}

// TurboPutNumSortBatch adds multiple values to numeric sort index.
func (s *ShardedDB) TurboPutNumSortBatch(name string, pairs []TurboNumSortPair) (int, error) {
	return s.turboPutNumSortBatch(name, pairs)
}

// TurboDeleteNumSortByDocID removes a docID from numeric sort index.
func (s *ShardedDB) TurboDeleteNumSortByDocID(name string, docID any) (bool, error) {
	return s.turboDeleteNumSortByDocID(name, docID)
}

// TurboDeleteNumSortByDocIDs removes multiple docIDs from numeric sort index.
func (s *ShardedDB) TurboDeleteNumSortByDocIDs(name string, docIDs []any) (int, error) {
	return s.turboDeleteNumSortByDocIDs(name, docIDs)
}

// TurboGetNumSortRange returns values in range from numeric sort index.
func (s *ShardedDB) TurboGetNumSortRange(name string, params TurboNumSortRangeParams) (TurboNumSortRangeResult, error) {
	return s.turboGetNumSortRange(name, params)
}

// TurboGetNumSortByDocID gets value for docID from numeric sort index.
func (s *ShardedDB) TurboGetNumSortByDocID(name string, docID any) (uint64, bool, error) {
	return s.turboGetNumSortByDocID(name, docID)
}

// TurboMergeNumSort merges numeric sort indexes.
func (s *ShardedDB) TurboMergeNumSort(dstName string, srcNames []string) error {
	return s.turboMergeNumSort(dstName, srcNames)
}

// TurboCleanNumSort removes duplicates from numeric sort index.
func (s *ShardedDB) TurboCleanNumSort(name string) error {
	return s.turboCleanNumSort(name)
}

// TurboRebuildNumSort rebuilds numeric sort index.
func (s *ShardedDB) TurboRebuildNumSort(name string, pairs []TurboNumSortPair) error {
	return s.turboRebuildNumSort(name, pairs)
}

// TurboNumSortStats returns numeric sort index statistics.
func (s *ShardedDB) TurboNumSortStats(name string) (*TurboNumSortStats, error) {
	return s.turboNumSortStats(name)
}

// TurboIntersectNumSortWithCandidates intersects candidates with numeric sort index.
func (s *ShardedDB) TurboIntersectNumSortWithCandidates(name string, candidates []any) ([]TurboNumSortPair, error) {
	return s.turboIntersectNumSortWithCandidates(name, candidates)
}

// TurboGetNumSortRangeIntersectCandidates returns range intersected with candidates.
func (s *ShardedDB) TurboGetNumSortRangeIntersectCandidates(name string, minValue uint64, maxValue uint64, candidates []any, page int, pageSize int) (TurboNumSortRangeResult, error) {
	return s.turboGetNumSortRangeIntersectCandidates(name, minValue, maxValue, candidates, page, pageSize)
}

// TurboGetNumSortRangeIntersectRaw is like TurboGetNumSortRangeIntersectCandidates,
// but candidates are provided as a raw turbo bitmap.
// Returns a raw turbo bitmap with matching docIDs (sorted by docID).
func (s *ShardedDB) TurboGetNumSortRangeIntersectRaw(name string, minValue uint64, maxValue uint64, candidatesRaw []byte) ([]byte, error) {
	return s.turboGetNumSortRangeIntersectRaw(name, minValue, maxValue, candidatesRaw)
}

// TurboGetNumSortRangeRaw returns all docIDs in the given value range as a raw turbo bitmap.
// This is useful when you need to use the result as candidates for further operations.
func (s *ShardedDB) TurboGetNumSortRangeRaw(name string, minValue uint64, maxValue uint64) ([]byte, error) {
	key := turboNumSortKey(name)

	// Zero-alloc: safe because val is used only within this function
	// (binary search + extract docIDs into a new buffer). The view is not returned.
	val, err := s.turboGetZeroAlloc(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if len(val) == 0 {
		return nil, nil
	}

	// Find range boundaries using binary search
	startIdx := turboNumSortFindGE(val, minValue)
	if startIdx < 0 {
		return nil, nil
	}

	endIdx := turboNumSortFindGT(val, maxValue)
	if endIdx < 0 {
		endIdx = int(turboBinaryCount(val))
	}

	if startIdx >= endIdx {
		return nil, nil
	}

	// Extract docIDs into a raw turbo buffer (key128 format)
	count := uint64(endIdx - startIdx)
	buf := make([]byte, 8+count*16)
	binary.LittleEndian.PutUint64(buf, count)
	for i := startIdx; i < endIdx; i++ {
		pair := turboGetNumSortPairAt(val, i)
		docIDKey128 := pair.DocID.(key128)
		offset := 8 + uint64(i-startIdx)*16
		binary.LittleEndian.PutUint64(buf[offset:], docIDKey128[0])
		binary.LittleEndian.PutUint64(buf[offset+8:], docIDKey128[1])
	}
	return buf, nil
}

// TurboGetNumSortRangeWithDocsParams holds parameters for TurboGetNumSortRangeWithDocs.
type TurboGetNumSortRangeWithDocsParams struct {
	// Name is the numSort index name.
	Name string
	// MinValue is the minimum value (inclusive).
	MinValue uint64
	// MaxValue is the maximum value (inclusive). ^uint64(0) means no upper bound.
	MaxValue uint64
	// Page is the 0-based page number.
	Page int
	// PageSize is the number of results per page.
	PageSize int
	// Desc if true, results are returned in descending value order.
	Desc bool
	// DocPrefix is ignored. The key128 docIDs should already contain any prefix
	// inside the hash (e.g., hash("turbo_doc:" + "scupage:" + "123")).
	DocPrefix string
}

// TurboGetNumSortRangeWithDocs returns a page of documents filtered by value range.
// Results are sorted by value (price), paginated, and documents are fetched.
// This is optimal for price range queries with price sorting.
func (s *ShardedDB) TurboGetNumSortRangeWithDocs(params TurboGetNumSortRangeWithDocsParams) (TurboSortPageWithDocsResult, error) {
	key := turboNumSortKey(params.Name)

	// Zero-alloc: safe because val is used only within this function
	// (binary search + extract docIDs). The view is not returned to caller.
	val, err := s.turboGetZeroAlloc(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return TurboSortPageWithDocsResult{}, nil
		}
		return TurboSortPageWithDocsResult{}, err
	}
	if len(val) == 0 {
		return TurboSortPageWithDocsResult{}, nil
	}

	if params.PageSize <= 0 {
		params.PageSize = 50
	}

	// Find range boundaries
	startIdx := turboNumSortFindGE(val, params.MinValue)
	if startIdx < 0 {
		return TurboSortPageWithDocsResult{}, nil
	}

	endIdx := turboNumSortFindGT(val, params.MaxValue)
	if endIdx < 0 {
		endIdx = int(turboBinaryCount(val))
	}

	if startIdx >= endIdx {
		return TurboSortPageWithDocsResult{}, nil
	}

	total := uint64(endIdx - startIdx)

	// Apply pagination
	var pageStart, pageEnd int
	if params.Desc {
		// Descending: from end
		// pageStart is the index of the LAST element on this page (highest index)
		// pageEnd is the index of the LAST element on the PREVIOUS page (lower index)
		// Range is [pageEnd+1, pageStart] inclusive
		pageStart = endIdx - 1 - params.Page*params.PageSize
		pageEnd = pageStart - params.PageSize
		if pageStart >= endIdx {
			return TurboSortPageWithDocsResult{Total: total}, nil
		}
		if pageEnd < startIdx-1 {
			pageEnd = startIdx - 1
		}
	} else {
		// Ascending: from start
		pageStart = startIdx + params.Page*params.PageSize
		pageEnd = pageStart + params.PageSize
		if pageStart >= endIdx {
			return TurboSortPageWithDocsResult{Total: total}, nil
		}
		if pageEnd > endIdx {
			pageEnd = endIdx
		}
	}

	// Collect docIDs
	var capacity int
	if params.Desc {
		capacity = pageStart - pageEnd
	} else {
		capacity = pageEnd - pageStart
	}
	if capacity < 0 {
		capacity = 0
	}
	docIDs := make([]key128, capacity)
	if capacity > 0 {
		tokenData := val[turboHeaderSize:]
		pairPtr := unsafe.Pointer(&tokenData[0])
		if params.Desc {
			// pageStart is index in val, e.g. endIdx-1, endIdx-2...
			// _extract_docids_rev_key128 expects start of the SUB-range
			// pageEnd is index of element JUST OUTSIDE the range (smaller)
			firstIdx := pageEnd + 1
			offset := uintptr(firstIdx) * uintptr(turboNumSortEntrySize)
			srcPtr := unsafe.Pointer(uintptr(pairPtr) + offset)
			_extract_docids_rev_key128(srcPtr, unsafe.Pointer(uintptr(capacity)), unsafe.Pointer(&docIDs[0]))
		} else {
			offset := uintptr(pageStart) * uintptr(turboNumSortEntrySize)
			srcPtr := unsafe.Pointer(uintptr(pairPtr) + offset)
			_extract_docids_key128(srcPtr, unsafe.Pointer(uintptr(capacity)), unsafe.Pointer(&docIDs[0]))
		}
	}

	if len(docIDs) == 0 {
		return TurboSortPageWithDocsResult{Total: total}, nil
	}

	// Fetch documents using MultiGetByKey128.
	// The docPrefix should already be part of the key128 hash.
	docs, err := s.MultiGetByKey128(docIDs)
	if err != nil {
		return TurboSortPageWithDocsResult{}, err
	}

	return TurboSortPageWithDocsResult{
		DocIDs: key128ToAnySlice(docIDs),
		Docs:   docs,
		Total:  total,
	}, nil
}

// ---- internal helpers for sorted operations ----

// turboSortedIntersect performs merge-style intersection on two sorted uint64 slices.
// Both slices must be sorted in ascending order.
// Returns a new sorted slice with common elements.
// No map allocations — O(len(a) + len(b)) time, O(min(len(a), len(b))) space.
func turboSortedIntersect(a, b []key128) []key128 {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}

	result := make([]key128, 0, min(len(a), len(b)))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if bytesCompareKey128(a[i], b[j]) < 0 {
			i++
		} else {
			j++
		}
	}

	return result
}

// turboSortedUnion performs merge-style union on two sorted uint64 slices.
// Both slices must be sorted in ascending order.
// Returns a new sorted slice with unique elements from both.
// No map allocations — O(len(a) + len(b)) time, O(len(a) + len(b)) space.
func turboSortedUnion(a, b []key128) []key128 {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	result := make([]key128, 0, len(a)+len(b))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		cmp := bytesCompareKey128(a[i], b[j])
		if cmp < 0 {
			result = append(result, a[i])
			i++
		} else if cmp > 0 {
			result = append(result, b[j])
			j++
		} else {
			// Equal: add once
			result = append(result, a[i])
			i++
			j++
		}
	}

	// Append remaining
	for i < len(a) {
		result = append(result, a[i])
		i++
	}
	for j < len(b) {
		result = append(result, b[j])
		j++
	}

	return result
}

// turboSortedDiff performs merge-style set difference on two sorted uint64 slices.
// Both slices must be sorted in ascending order.
// Returns elements in a that are not in b, preserving order.
// No map allocations — O(len(a) + len(b)) time, O(len(a)) space.
func turboSortedDiff(a, b []key128) []key128 {
	if len(a) == 0 {
		return nil
	}
	if len(b) == 0 {
		return a
	}

	result := make([]key128, 0, len(a))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		cmp := bytesCompareKey128(a[i], b[j])
		if cmp < 0 {
			result = append(result, a[i])
			i++
		} else if cmp > 0 {
			j++
		} else {
			// Equal: skip a[i] (it's in b)
			i++
			j++
		}
	}

	// Append remaining elements from a
	for i < len(a) {
		result = append(result, a[i])
		i++
	}

	return result
}

// turboBinarySearchInsertionPoint finds the position where target should be inserted
// in a sorted turbo index buffer. Returns:
//   - index >= 0 if target already exists at that position
//   - -index-1 if target not found, index is where it should be inserted
func turboBinarySearchInsertionPoint(tokenData []byte, count uint64, target key128) int {
	lo, hi := 0, int(count)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		offset := mid * turboIndexEntrySize
		// Read key128 from tokenData
		var t key128
		t[0] = binary.LittleEndian.Uint64(tokenData[offset:])
		t[1] = binary.LittleEndian.Uint64(tokenData[offset+8:])
		if t == target {
			return mid
		} else if bytesCompareKey128(t, target) < 0 {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return -lo - 1
}

// bytesCompareKey128 compares two key128 values.
// Returns -1 if a < b, 0 if a == b, 1 if a > b
func bytesCompareKey128(a, b key128) int {
	if a[0] < b[0] {
		return -1
	}
	if a[0] > b[0] {
		return 1
	}
	if a[1] < b[1] {
		return -1
	}
	if a[1] > b[1] {
		return 1
	}
	return 0
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
