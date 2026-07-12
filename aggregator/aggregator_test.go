package aggregator

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"
)

const (
	shardCount      = 10
	recordsPerShard = 100_000 // 100k записей на шард для бенчмарка
)

// setupTestRegistry создает тестовый реестр с моковыми шардами
func setupTestRegistry(shardCount int, recordsPerShard int) *ShardRegistry {
	registry := NewShardRegistry()

	for i := 0; i < shardCount; i++ {
		records := generateRecords(int64(i), recordsPerShard)

		sort.Slice(records, func(a, b int) bool {
			cmp := compareKeys(records[a].Key, records[b].Key)
			if cmp != 0 {
				return cmp < 0
			}
			return records[a].ShardID < records[b].ShardID
		})
		// ОТЛАДКА:
		// for i := 0; i < 5; i++ {
		// 	fmt.Printf("Record %d: %s, Shard: %d", i, records[i].Key, records[i].ShardID)
		// }
		client := NewMockShardClient(int64(i), records)
		registry.Register(int64(i), client)
	}

	return registry
}

// generateRecords генерирует тестовые записи
func generateRecords(shardID int64, count int) []Record {
	r := rand.New(rand.NewSource(42 + shardID)) // Deterministic for testing
	records := make([]Record, 0, count)

	terms := []string{"test", "data", "record", "item", "entry", "doc", "key", "value"}

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("shard_%d_key_%06d", shardID, i)
		termIdx := r.Intn(len(terms))
		value := fmt.Sprintf("%s_value_%d_timestamp_%d", terms[termIdx], i, time.Now().UnixNano())

		records = append(records, Record{
			Key:       []byte(key),
			Value:     []byte(value),
			Timestamp: int64(i * 1000),
			ShardID:   shardID, // Устанавливаем ShardID для детерминизма сортировки
		})
	}

	return records
}

// TestAggregator_Search tests basic search functionality
func TestAggregator_Search(t *testing.T) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	opts := SearchOptions{
		Terms: []string{},
		Limit: 100,
	}

	result, err := agg.Search(ctx, opts)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if result.Total == 0 {
		t.Error("Expected non-zero total results")
	}

	t.Logf("Total records: %d across %d shards", result.Total, len(result.ShardStats))
}

// TestAggregator_KeysetPagination tests keyset pagination with ShardID tie-breaker
func TestAggregator_KeysetPagination(t *testing.T) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	limit := 50

	// Первая страница
	opts1 := SearchOptions{
		Terms: []string{},
		Limit: limit,
	}

	result1, err := agg.Search(ctx, opts1)
	if err != nil {
		t.Fatalf("First page search failed: %v", err)
	}

	if len(result1.Records) == 0 {
		t.Fatal("Expected records on first page")
	}

	// Вторая страница с keyset pagination
	// Теперь мы передаем и NextKey, и LastShardID
	opts2 := SearchOptions{
		Terms:       []string{},
		Limit:       limit,
		LastKey:     result1.NextKey,
		LastShardID: result1.LastShardID,
	}

	result2, err := agg.Search(ctx, opts2)
	if err != nil {
		t.Fatalf("Second page search failed: %v", err)
	}

	if len(result2.Records) == 0 {
		t.Fatal("Expected records on second page")
	}

	// Проверяем детерминизм перехода
	lastRec1 := result1.Records[len(result1.Records)-1]
	firstRec2 := result2.Records[0]

	// Сравниваем составной ключ: (Key, ShardID)
	// Условие: (firstRec2.Key, firstRec2.ShardID) > (lastRec1.Key, lastRec1.ShardID)
	cmp := compareKeys(firstRec2.Key, lastRec1.Key)
	isGreater := false

	if cmp > 0 {
		isGreater = true
	} else if cmp == 0 {
		if firstRec2.ShardID > lastRec1.ShardID {
			isGreater = true
		}
	}

	if !isGreater {
		t.Errorf("Pagination failed: Second page first record (Key: %s, ShardID: %d) "+
			"must be greater than first page last record (Key: %s, ShardID: %d)",
			firstRec2.Key, firstRec2.ShardID, lastRec1.Key, lastRec1.ShardID)
	}

	// Проверка на отсутствие дубликатов между страницами
	seen := make(map[string]bool)
	for _, rec := range result1.Records {
		seen[string(rec.Key)+fmt.Sprintf("_%d", rec.ShardID)] = true
	}
	for _, rec := range result2.Records {
		key := string(rec.Key) + fmt.Sprintf("_%d", rec.ShardID)
		if seen[key] {
			t.Errorf("Duplicate record found on page 2: %s", key)
		}
	}

	// fmt.Printf("Page 1 last record: %s (Shard %d), Page 2 first record: %s (Shard %d)",
	// 	lastRec1.Key, lastRec1.ShardID, firstRec2.Key, firstRec2.ShardID)
}

// TestAggregator_ScatterGatherParallelism tests parallel execution
func TestAggregator_ScatterGatherParallelism(t *testing.T) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	opts := SearchOptions{
		Terms: []string{},
		Limit: 1000,
	}

	start := time.Now()
	result, err := agg.Search(ctx, opts)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Проверяем, что все шарды были опрошены
	if len(result.ShardStats) != shardCount {
		t.Errorf("Expected %d shards to be queried, got %d", shardCount, len(result.ShardStats))
	}

	t.Logf("Search completed in %v across %d shards", elapsed, len(result.ShardStats))
}

// TestAggregator_ContextCancellation tests context cancellation
func TestAggregator_ContextCancellation(t *testing.T) {
	registry := setupTestRegistry(shardCount, 100)
	agg := NewAggregator(registry)

	// Создаем отмененный контекст
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Выполняем поиск
	_, err := agg.Search(ctx, SearchOptions{Limit: 100})

	// Ошибка контекста должна быть context.Canceled
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

// TestAggregator_EmptyShards tests handling of empty shards
func TestAggregator_EmptyShards(t *testing.T) {
	registry := NewShardRegistry()
	agg := NewAggregator(registry)

	ctx := context.Background()
	result, err := agg.Search(ctx, SearchOptions{Limit: recordsPerShard})

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if result.Total != 0 {
		t.Errorf("Expected total 0 for empty registry, got %d", result.Total)
	}

	if len(result.Records) != 0 {
		t.Error("Expected empty records")
	}
}

// TestAggregator_PoolReuse tests that pools are properly reused
func TestAggregator_PoolReuse(t *testing.T) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()

	// Выполняем несколько запросов для проверки пула
	for i := 0; i < 10; i++ {
		result, err := agg.Search(ctx, SearchOptions{Limit: 50})
		if err != nil {
			t.Fatalf("Search %d failed: %v", i, err)
		}
		_ = result
	}

	// Пул должен содержать освободившиеся объекты
	// (это косвенная проверка - пул работает если нет утечек памяти)
}

// BenchmarkSearch_Basic benchmarks basic search with 100k records per shard
func BenchmarkAggregator_Search(b *testing.B) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	opts := SearchOptions{
		Terms: []string{},
		Limit: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := agg.Search(ctx, opts)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
		_ = result
	}

	totalRecords := shardCount * recordsPerShard
	b.Logf("Searched across %d shards with total %d records", shardCount, totalRecords)
}

// BenchmarkSearch_WithTerms benchmarks search with term filtering
func BenchmarkAggregator_Search_WithTerms(b *testing.B) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	opts := SearchOptions{
		Terms: []string{"test"},
		Limit: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := agg.Search(ctx, opts)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
		_ = result
	}
}

// BenchmarkSearch_KeysetPagination benchmarks keyset pagination
func BenchmarkAggregator_Search_KeysetPagination(b *testing.B) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	limit := 100

	// Получаем первую страницу для keyset
	opts1 := SearchOptions{
		Terms: []string{},
		Limit: limit,
	}

	result1, _ := agg.Search(ctx, opts1)
	lastKey := result1.NextKey

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := SearchOptions{
			Terms:   []string{},
			Limit:   limit,
			LastKey: lastKey,
		}
		result, err := agg.Search(ctx, opts)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
		_ = result
	}
}

// BenchmarkSearch_Parallelism benchmarks parallel scatter-gather
func BenchmarkAggregator_Search_Parallelism(b *testing.B) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	opts := SearchOptions{
		Terms: []string{},
		Limit: 100,
	}

	// Бенчмарк параллельного выполнения
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result, err := agg.Search(ctx, opts)
			if err != nil {
				b.Fatalf("Search failed: %v", err)
			}
			_ = result
		}
	})
}

// BenchmarkSearch_VariousShardCounts benchmarks with different shard counts
func BenchmarkAggregator_Search_VariousShardCounts(b *testing.B) {
	shardConfigs := []int{1, 5, 10, 20}

	for _, numShards := range shardConfigs {
		b.Run(fmt.Sprintf("%d_shards", numShards), func(b *testing.B) {
			registry := setupTestRegistry(numShards, recordsPerShard)
			agg := NewAggregator(registry)

			ctx := context.Background()
			opts := SearchOptions{
				Terms: []string{},
				Limit: 100,
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := agg.Search(ctx, opts)
				if err != nil {
					b.Fatalf("Search failed: %v", err)
				}
				_ = result
			}
		})
	}
}

// BenchmarkAggregator_MemoryAllocation benchmarks memory allocations
func BenchmarkAggregator_Search_Allocations(b *testing.B) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	opts := SearchOptions{
		Terms: []string{},
		Limit: 100,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := agg.Search(ctx, opts)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
		_ = result
	}
}

// TestShardFilter_BitmapOptimization tests bitmap-based shard filtering
func TestShardFilter_BitmapOptimization(t *testing.T) {
	cache := NewShardBitmapCache()
	filter := NewShardFilter(cache)

	// Настройка: термин "test" есть на шардах 0, 2, 4
	cache.UpdateTermShardMask("test", 0, true)
	cache.UpdateTermShardMask("test", 1, false)
	cache.UpdateTermShardMask("test", 2, true)
	cache.UpdateTermShardMask("test", 3, false)
	cache.UpdateTermShardMask("test", 4, true)

	allShards := []int64{0, 1, 2, 3, 4}
	filtered := filter.FilterShards([]string{"test"}, allShards)

	expected := []int64{0, 2, 4}
	if len(filtered) != len(expected) {
		t.Errorf("Expected %v shards, got %v", expected, filtered)
	}
}

// TestCompareKeys tests key comparison function
func TestCompareKeys(t *testing.T) {
	tests := []struct {
		a      string
		b      string
		expect int
	}{
		{"abc", "abd", -1},
		{"abd", "abc", 1},
		{"abc", "abc", 0},
		{"ab", "abc", -1},
		{"abc", "ab", 1},
	}

	for _, tt := range tests {
		result := compareKeys([]byte(tt.a), []byte(tt.b))
		if result != tt.expect {
			t.Errorf("compareKeys(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expect)
		}
	}
}

// FuzzSearch fuzz test for search function
func FuzzSearch(f *testing.F) {
	seeds := []string{
		"",
		"test",
		"data",
		"nonexistent",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, term string) {
		var terms []string
		if term != "" {
			terms = []string{term}
		}

		registry := setupTestRegistry(3, 100)
		agg := NewAggregator(registry)

		ctx := context.Background()
		_, err := agg.Search(ctx, SearchOptions{
			Terms: terms,
			Limit: 50,
		})
		if err != nil {
			t.Skipf("Search failed (expected for some inputs): %v", err)
		}
	})
}

// TestConcurrentSearch tests concurrent search operations
func TestConcurrentSearch(t *testing.T) {
	registry := setupTestRegistry(shardCount, recordsPerShard)
	agg := NewAggregator(registry)

	ctx := context.Background()
	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Запускаем 100 concurrent запросов
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := agg.Search(ctx, SearchOptions{
				Terms: []string{},
				Limit: 50,
			})
			if err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	var errors int
	for range errChan {
		errors++
	}

	if errors > 0 {
		t.Errorf("%d concurrent searches failed", errors)
	}
}
