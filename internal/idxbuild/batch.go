package idxbuild

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/GenshIv/makodb/v2"
)

// BatchAccum collects indexes and sorts for a single batch of products.
type BatchAccum struct {
	// index key -> docIDs
	Indexes map[string][]uint64
	// sort key -> items (docID + score)
	Sorts map[string][]ItemWithScore
}

// ItemWithScore is a sortable entry for sort indexes.
type ItemWithScore struct {
	DocID uint64
	Score float64
}

func NewBatchAccum() *BatchAccum {
	return &BatchAccum{
		Indexes: make(map[string][]uint64),
		Sorts:   make(map[string][]ItemWithScore),
	}
}

func (a *BatchAccum) AddIndex(key string, docID uint64) {
	a.Indexes[key] = append(a.Indexes[key], docID)
}

func (a *BatchAccum) AddSort(key string, item ItemWithScore) {
	a.Sorts[key] = append(a.Sorts[key], item)
}

// WriteBatch writes all accumulated data to tmp files.
func (a *BatchAccum) WriteBatch(tmpDir string, batchID int) error {
	for key, docIDs := range a.Indexes {
		if len(docIDs) == 0 {
			continue
		}
		path := filepath.Join(tmpDir, fmt.Sprintf("idx_%s_%d.dat", safeKey(key), batchID))
		if err := writeUint64Slice(path, docIDs); err != nil {
			return fmt.Errorf("write index %s batch %d: %w", key, batchID, err)
		}
	}
	for key, items := range a.Sorts {
		if len(items) == 0 {
			continue
		}
		path := filepath.Join(tmpDir, fmt.Sprintf("sort_%s_%d.dat", safeKey(key), batchID))
		if err := writeItemWithScoreSlice(path, items); err != nil {
			return fmt.Errorf("write sort %s batch %d: %w", key, batchID, err)
		}
	}
	return nil
}

// MergeIndexes reads all idx_* files, merges by key, radix sorts, and appends to DB indexes via TurboPutBatchIndex.
func MergeIndexes(db *makodb.ShardedDB, tmpDir string) error {
	type keyFiles struct {
		files []string
		total int // total elements across all files
	}
	keyFilesMap := map[string]*keyFiles{}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("read tmp dir: %w", err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "idx_") && strings.HasSuffix(e.Name(), ".dat") {
			name := e.Name()[4 : len(e.Name())-4] // remove "idx_" and ".dat"
			lastUnderscore := strings.LastIndex(name, "_")
			safeKey := name[:lastUnderscore]
			kf, ok := keyFilesMap[safeKey]
			if !ok {
				kf = &keyFiles{}
				keyFilesMap[safeKey] = kf
			}
			kf.files = append(kf.files, filepath.Join(tmpDir, e.Name()))
		}
	}

	// Pre-count elements for each key to avoid reallocations
	for safeKey, kf := range keyFilesMap {
		for _, f := range kf.files {
			if count, err := countUint64Slice(f); err == nil {
				kf.total += count
			}
		}
		keyFilesMap[safeKey] = kf
	}

	for safeKey, kf := range keyFilesMap {
		if kf.total == 0 {
			continue
		}

		// Pre-allocate slice
		all := make([]uint64, 0, kf.total)
		for _, f := range kf.files {
			data, err := readUint64Slice(f)
			if err != nil {
				return fmt.Errorf("read index file %s: %w", f, err)
			}
			all = append(all, data...)
		}
		if len(all) == 0 {
			continue
		}
		RadixSortUint64(all)
		key := decodeSafeKey(safeKey)
		// Convert uint64 docIDs to strings for TurboPutBatchIndexString
		strAll := make([]string, len(all))
		for i, id := range all {
			strAll[i] = strconv.FormatUint(id, 10)
		}
		if _, err := db.TurboPutBatchIndexString(key, strAll); err != nil {
			return fmt.Errorf("batch add to index %s: %w", key, err)
		}
	}
	return nil
}

// MergeSortIndexes reads all sort_* files, sorts items within each batch, and writes via TurboPutSortIndex.
func MergeSortIndexes(db *makodb.ShardedDB, tmpDir string) error {
	type sortKeyFiles struct {
		files []string
		total int
	}
	keyFilesMap := map[string]*sortKeyFiles{}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("read tmp dir: %w", err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sort_") && strings.HasSuffix(e.Name(), ".dat") {
			name := e.Name()[5 : len(e.Name())-4] // remove "sort_" and ".dat"
			lastUnderscore := strings.LastIndex(name, "_")
			safeKey := name[:lastUnderscore]
			kf, ok := keyFilesMap[safeKey]
			if !ok {
				kf = &sortKeyFiles{}
				keyFilesMap[safeKey] = kf
			}
			kf.files = append(kf.files, filepath.Join(tmpDir, e.Name()))
		}
	}

	// Pre-count elements
	for safeKey, kf := range keyFilesMap {
		for _, f := range kf.files {
			if count, err := countItemWithScoreSlice(f); err == nil {
				kf.total += count
			}
		}
		keyFilesMap[safeKey] = kf
	}

	for safeKey, kf := range keyFilesMap {
		if kf.total == 0 {
			continue
		}

		// Pre-allocate slice
		items := make([]ItemWithScore, 0, kf.total)
		for _, f := range kf.files {
			data, err := readItemWithScoreSlice(f)
			if err != nil {
				return fmt.Errorf("read sort file %s: %w", f, err)
			}
			items = append(items, data...)
		}
		if len(items) == 0 {
			continue
		}

		// Sort items within this batch by score (O(n log n))
		asc := strings.Contains(safeKey, "asc")
		sort.Slice(items, func(i, j int) bool {
			if asc {
				return items[i].Score < items[j].Score
			}
			return items[i].Score > items[j].Score
		})

		// Extract docIDs in sorted order
		docIDs := make([]uint64, len(items))
		for i, it := range items {
			docIDs[i] = it.DocID
		}

		indexName := decodeSafeKey(safeKey)
		// Convert uint64 docIDs to strings for TurboPutSortIndexString
		strDocIDs := make([]string, len(docIDs))
		for i, id := range docIDs {
			strDocIDs[i] = strconv.FormatUint(id, 10)
		}
		if err := db.TurboPutSortIndexString(indexName, strDocIDs); err != nil {
			return fmt.Errorf("write sort index %s: %w", indexName, err)
		}
	}
	return nil
}

// CleanupTmp removes all idx_* and sort_* files from tmpDir.
func CleanupTmp(tmpDir string) error {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "idx_") || strings.HasPrefix(e.Name(), "sort_") {
			if err := os.Remove(filepath.Join(tmpDir, e.Name())); err != nil {
				return fmt.Errorf("remove %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// ---------- I/O helpers ----------

func writeUint64Slice(path string, data []uint64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	count := uint64(len(data))
	if err := binary.Write(f, binary.LittleEndian, count); err != nil {
		return err
	}
	for _, v := range data {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	return nil
}

// countUint64Slice reads only the count header from a uint64 slice file.
func countUint64Slice(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var count uint64
	if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
		return 0, err
	}
	return int(count), nil
}

// countItemWithScoreSlice reads only the count header from an ItemWithScore slice file.
func countItemWithScoreSlice(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var count uint64
	if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
		return 0, err
	}
	return int(count), nil
}

func readUint64Slice(path string) ([]uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var count uint64
	if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	data := make([]uint64, count)
	for i := uint64(0); i < count; i++ {
		if err := binary.Read(f, binary.LittleEndian, &data[i]); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func writeItemWithScoreSlice(path string, data []ItemWithScore) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	count := uint64(len(data))
	if err := binary.Write(f, binary.LittleEndian, count); err != nil {
		return err
	}
	for _, v := range data {
		if err := binary.Write(f, binary.LittleEndian, v.DocID); err != nil {
			return err
		}
		if err := binary.Write(f, binary.LittleEndian, v.Score); err != nil {
			return err
		}
	}
	return nil
}

func readItemWithScoreSlice(path string) ([]ItemWithScore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var count uint64
	if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	data := make([]ItemWithScore, count)
	for i := uint64(0); i < count; i++ {
		if err := binary.Read(f, binary.LittleEndian, &data[i].DocID); err != nil {
			return nil, err
		}
		if err := binary.Read(f, binary.LittleEndian, &data[i].Score); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// ---------- safeKey helpers ----------
// Uses '\x01' as internal separator to avoid collisions with ':' in keys like "price:0_5000".

func safeKey(key string) string {
	key = strings.ReplaceAll(key, "\x01", "\x02") // escape existing separator
	key = strings.ReplaceAll(key, ":", "\x01")
	key = strings.ReplaceAll(key, "/", "\x01")
	key = strings.ReplaceAll(key, "\\", "\x01")
	return key
}

func decodeSafeKey(safeKey string) string {
	key := strings.ReplaceAll(safeKey, "\x02", "\x01") // restore escaped separator
	key = strings.ReplaceAll(key, "\x01", ":")
	return key
}

// ---------- Radix Sort for uint64 ----------

func RadixSortUint64(arr []uint64) {
	if len(arr) <= 1 {
		return
	}
	max := arr[0]
	for _, v := range arr {
		if v > max {
			max = v
		}
	}
	var output = make([]uint64, len(arr))
	for exp := uint64(1); max/exp > 0; exp *= 256 {
		count := [256]int{}
		for _, v := range arr {
			count[(v/exp)%256]++
		}
		for i := 1; i < 256; i++ {
			count[i] += count[i-1]
		}
		for i := len(arr) - 1; i >= 0; i-- {
			bucket := (arr[i] / exp) % 256
			output[count[bucket]-1] = arr[i]
			count[bucket]--
		}
		for i := 0; i < len(arr); i++ {
			arr[i] = output[i]
		}
	}
}
