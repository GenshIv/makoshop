package aggregator

import (
	"sort"
	"testing"
)

// BenchmarkMergeStrategies сравнивает разные стратегии merge:
// 1. Naive - собрать всё и отсортировать (O(N log N) где N = total records)
// 2. K-way merge - O(K * L + L log K) где K = shards, L = limit
func BenchmarkMergeStrategies(b *testing.B) {
	const (
		numShards       = 10
		recordsPerShard = 10000
		limit           = 100
	)

	// Генерируем тестовые данные
	shardResults := make([]*ShardResult, numShards)
	for i := 0; i < numShards; i++ {
		records := make([]Record, recordsPerShard)
		for j := 0; j < recordsPerShard; j++ {
			// Создаём ключи которые будут перемешаны между шардами
			keyVal := (j * numShards) + i // Interleaved keys для worst-case scenario
			records[j] = Record{
				Key:       []byte(string(rune('A'+keyVal%26)) + string(rune('0'+keyVal/26%10))),
				Value:     []byte("value"),
				Timestamp: int64(keyVal),
				ShardID:   int64(i),
			}
		}
		shardResults[i] = &ShardResult{
			ShardID: int64(i),
			Records: records,
		}
	}

	b.Run("Naive_CollectAndSort", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Собираем ВСЁ в один слайс
			allRecords := make([]Record, 0, numShards*recordsPerShard)
			for _, sr := range shardResults {
				allRecords = append(allRecords, sr.Records...)
			}

			// Сортируем ВЕСЬ набор данных (дорого!)
			sort.Slice(allRecords, func(i, j int) bool {
				cmp := compareKeys(allRecords[i].Key, allRecords[j].Key)
				if cmp != 0 {
					return cmp < 0
				}
				return allRecords[i].ShardID < allRecords[j].ShardID
			})

			// Берём только первые limit записей
			_ = allRecords[:limit]
		}
	})

	b.Run("KWayMerge_AllEmpty", func(b *testing.B) {
		// Тест когда все шарды возвращают пустые результаты (worst case edge!)
		allEmptyResults := make([]*ShardResult, numShards)
		for i := 0; i < numShards; i++ {
			allEmptyResults[i] = &ShardResult{
				ShardID: int64(i),
				Records: nil, // nil slice!
			}
		}

		for i := 0; i < b.N; i++ {
			merger := NewKWayMerger(allEmptyResults)
			
			if merger.HasNext() {
				b.Fatal("Expected HasNext() to return false for all empty shards")
			}
			
			rec := merger.Next()
			if rec != nil {
				b.Fatal("Expected Next() to return nil for all empty shards")
			}
		}
	})

	b.Run("KWayMerge_MixedEmpty", func(b *testing.B) {
		// Тест с частично пустыми шардами (realistic scenario!)
		mixedShardResults := make([]*ShardResult, numShards)
		for i := 0; i < numShards; i++ {
			if i%2 == 0 {
				mixedShardResults[i] = &ShardResult{
					ShardID: int64(i),
					Records: generateRandomRecords(recordsPerShard, int64(i)),
				}
			} else {
				mixedShardResults[i] = &ShardResult{
					ShardID: int64(i),
					Records: []Record{}, // Пустой!
				}
			}
		}

		for i := 0; i < b.N; i++ {
			merger := NewKWayMerger(mixedShardResults)
			
			limit := 100
			result := make([]Record, 0, limit)
			
			for j := 0; j < limit && merger.HasNext(); j++ {
				if rec := merger.Next(); rec != nil {
					result = append(result, *rec)
				} else {
					break // Early exit if Next() returns nil
				}
			}
		}
	})

	b.Run("KWayMerge_SingleShard", func(b *testing.B) {
		// Тест с одним шардом (оптимизация!)
		singleShardResults := []*ShardResult{
			{
				ShardID: 0,
				Records: generateRandomRecords(recordsPerShard, 0),
			},
		}

		for i := 0; i < b.N; i++ {
			merger := NewKWayMerger(singleShardResults)
			
			limit := 100
			result := make([]Record, 0, limit)
			
			for j := 0; j < limit && merger.HasNext(); j++ {
				if rec := merger.Next(); rec != nil {
					result = append(result, *rec)
				} else {
					break // Early exit if Next() returns nil
				}
			}
		}
	})

	b.Run("KWayMerge_EmptyShards", func(b *testing.B) {
		// Тест с пустыми шардами (edge case!)
		emptyShardResults := make([]*ShardResult, numShards)
		for i := 0; i < numShards; i++ {
			emptyShardResults[i] = &ShardResult{
				ShardID: int64(i),
				Records: []Record{}, // Пустой слайс!
			}
		}

		for i := 0; i < b.N; i++ {
			merger := NewKWayMerger(emptyShardResults)
			
			limit := 100
			result := make([]Record, 0, limit)
			
			for j := 0; j < limit && merger.HasNext(); j++ {
				if rec := merger.Next(); rec != nil {
					result = append(result, *rec)
				} else {
					break // Early exit if Next() returns nil
				}
			}
			
			// Должно быть пустой результат
			if len(result) != 0 {
				b.Fatalf("Expected empty result, got %d records", len(result))
			}
		}
	})

	b.Run("KWayMerge", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Создаём merger
			merger := NewKWayMerger(shardResults)

			// Берём только limit записей (эффективно!)
			count := 0
			for count < limit && merger.HasNext() {
				_ = merger.Next()
				count++
			}
		}
	})

	b.Run("KWayMerge_NoAlloc", func(b *testing.B) {
		// Используем pre-allocated slices для ещё большей эффективности
		shardResultsPre := make([]*ShardResult, numShards)
		for i := 0; i < numShards; i++ {
			records := make([]Record, recordsPerShard)
			for j := 0; j < recordsPerShard; j++ {
				keyVal := (j * numShards) + i
				records[j] = Record{
					Key:       []byte(string(rune('A'+keyVal%26)) + string(rune('0'+keyVal/26%10))),
					Value:     []byte("value"),
					Timestamp: int64(keyVal),
					ShardID:   int64(i),
				}
			}
			shardResultsPre[i] = &ShardResult{
				ShardID: int64(i),
				Records: records,
			}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			merger := NewKWayMerger(shardResultsPre)
			count := 0
			for count < limit && merger.HasNext() {
				_ = merger.Next()
				count++
			}
		}
	})
}

// BenchmarkMergeStrategies_Memory показывает разницу в аллокациях памяти
func BenchmarkMergeStrategies_Memory(b *testing.B) {
	const (
		numShards       = 10
		recordsPerShard = 10000
		limit           = 100
	)

	shardResults := make([]*ShardResult, numShards)
	for i := 0; i < numShards; i++ {
		records := make([]Record, recordsPerShard)
		for j := 0; j < recordsPerShard; j++ {
			keyVal := (j * numShards) + i
			records[j] = Record{
				Key:       []byte(string(rune('A'+keyVal%26)) + string(rune('0'+keyVal/26%10))),
				Value:     []byte("value"),
				Timestamp: int64(keyVal),
				ShardID:   int64(i),
			}
		}
		shardResults[i] = &ShardResult{
			ShardID: int64(i),
			Records: records,
		}
	}

	b.Run("Naive_CollectAndSort", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			allRecords := make([]Record, 0, numShards*recordsPerShard)
			for _, sr := range shardResults {
				allRecords = append(allRecords, sr.Records...)
			}

			sort.Slice(allRecords, func(i, j int) bool {
				cmp := compareKeys(allRecords[i].Key, allRecords[j].Key)
				if cmp != 0 {
					return cmp < 0
				}
				return allRecords[i].ShardID < allRecords[j].ShardID
			})

			_ = allRecords[:limit]
		}
	})

	b.Run("KWayMerge", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			merger := NewKWayMerger(shardResults)
			count := 0
			for count < limit && merger.HasNext() {
				_ = merger.Next()
				count++
			}
		}
	})
}
