package aggregator

import (
	"crypto/rand"
	"fmt"
)

// ============================================================================
// Test Helpers - вспомогательные функции для тестов и бенчмарков
// ============================================================================

// generateRandomRecords генерирует случайные записи для тестирования
func generateRandomRecords(count int, avgKeyLen, avgValueLen int) []Record {
	records := make([]Record, count)
	
	for i := 0; i < count; i++ {
		keyLen := avgKeyLen + (i % 10) // Вариация длины ключа ±5 байт
		valueLen := avgValueLen + (i % 20) // Вариация длины значения ±10 байт
		
		key := make([]byte, keyLen)
		value := make([]byte, valueLen)
		
		// Заполняем случайными данными
		rand.Read(key)
		rand.Read(value)
		
		records[i] = Record{
			ID:        int32(i),
			Key:       key,
			Value:     value,
			Score:     float32(1.0 - float32(i)/float32(count)), // Score убывает с индексом
			ShardID:   int64(i % 10), // Распределяем по 10 шардам
			Timestamp: int64(i * 1000),
		}
	}
	
	return records
}

// generateRandomRecordsWithKeys генерирует записи с читаемыми ключами (для отладки)
func generateRandomRecordsWithKeys(count int, prefix string) []Record {
	records := make([]Record, count)
	
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s-%d", prefix, i)
		value := fmt.Sprintf("value-for-key-%d", i)
		
		records[i] = Record{
			ID:        int32(i),
			Key:       []byte(key),
			Value:     []byte(value),
			Score:     float32(1.0 - float32(i)/float32(count)),
			ShardID:   int64(i % 10),
			Timestamp: int64(i * 1000),
		}
	}
	
	return records
}

// generateRandomIDs генерирует случайные IDs для тестирования
func generateRandomIDs(count int) []int32 {
	ids := make([]int32, count)
	for i := 0; i < count; i++ {
		ids[i] = int32(i)
	}
	return ids
}

// generateShardResults генерирует результаты от нескольких шардов для тестирования k-way merge
func generateShardResults(shardCount, recordsPerShard int) []*ShardResult {
	results := make([]*ShardResult, shardCount)
	
	for i := 0; i < shardCount; i++ {
		result := GetShardResult()
		result.ShardID = int64(i)
		
		// Генерируем записи для этого шарда с ключами в диапазоне [shardID*range, (shardID+1)*range)
		for j := 0; j < recordsPerShard; j++ {
			key := fmt.Sprintf("key-shard-%d-record-%d", i, j)
			
			result.Records = append(result.Records, Record{
				ID:        int32(i*recordsPerShard + j),
				Key:       []byte(key),
				Value:     []byte(fmt.Sprintf("value-%d", j)),
				Score:     float32(1.0 - float32(j)/float32(recordsPerShard)),
				ShardID:   int64(i),
				Timestamp: int64((i*recordsPerShard + j) * 1000),
			})
		}
		
		results[i] = result
	}
	
	return results
}

// generateSortedRecords генерирует уже отсортированные записи (для тестирования edge cases)
func generateSortedRecords(count int, ascending bool) []Record {
	records := make([]Record, count)
	
	for i := 0; i < count; i++ {
		keyIdx := i
		if !ascending {
			keyIdx = count - 1 - i
		}
		
		records[i] = Record{
			ID:        int32(i),
			Key:       []byte(fmt.Sprintf("key-%08d", keyIdx)), // Zero-padded для корректной сортировки строк
			Value:     []byte(fmt.Sprintf("value-%d", i)),
			Score:     float32(1.0 - float32(i)/float32(count)),
			ShardID:   int64(i % 10),
			Timestamp: int64(i * 1000),
		}
	}
	
	return records
}

// generateDuplicateKeysRecords генерирует записи с дублирующимися ключами (для тестирования tie-breaker)
func generateDuplicateKeysRecords(shardCount, recordsPerShard int) []*ShardResult {
	results := make([]*ShardResult, shardCount)
	
	for i := 0; i < shardCount; i++ {
		result := GetShardResult()
		result.ShardID = int64(i)
		
		// Все шарды имеют одинаковые ключи - тестируем tie-breaker по ShardID
		for j := 0; j < recordsPerShard; j++ {
			key := fmt.Sprintf("duplicate-key-%d", j % 10) // Только 10 уникальных ключей
			
			result.Records = append(result.Records, Record{
				ID:        int32(i*recordsPerShard + j),
				Key:       []byte(key),
				Value:     []byte(fmt.Sprintf("value-shard-%d-record-%d", i, j)),
				Score:     float32(1.0 - float32(j)/float32(recordsPerShard)),
				ShardID:   int64(i),
				Timestamp: int64((i*recordsPerShard + j) * 1000),
			})
		}
		
		results[i] = result
	}
	
	return results
}

// generateLargeRecords генерирует записи с большими ключами и значениями (для тестирования memory pressure)
func generateLargeRecords(count int, keySize, valueSize int) []Record {
	records := make([]Record, count)
	
	for i := 0; i < count; i++ {
		key := make([]byte, keySize)
		value := make([]byte, valueSize)
		
		// Заполняем предсказуемыми данными (быстрее чем rand.Read)
		for j := 0; j < keySize; j++ {
			key[j] = byte((i + j) % 256)
		}
		for j := 0; j < valueSize; j++ {
			value[j] = byte((i*2 + j) % 256)
		}
		
		records[i] = Record{
			ID:        int32(i),
			Key:       key,
			Value:     value,
			Score:     float32(1.0 - float32(i)/float32(count)),
			ShardID:   int64(i % 10),
			Timestamp: int64(i * 1000),
		}
	}
	
	return records
}

// generateRecordsWithSameScore генерирует записи с одинаковым score (для тестирования secondary sort)
func generateRecordsWithSameScore(count int, shardCount int) []*ShardResult {
	results := make([]*ShardResult, shardCount)
	
	for i := 0; i < shardCount; i++ {
		result := GetShardResult()
		result.ShardID = int64(i)
		
		recordsPerShard := count / shardCount
		
		for j := 0; j < recordsPerShard; j++ {
			key := fmt.Sprintf("key-shard-%d-record-%d", i, j)
			
			result.Records = append(result.Records, Record{
				ID:        int32(i*recordsPerShard + j),
				Key:       []byte(key),
				Value:     []byte(fmt.Sprintf("value-%d", j)),
				Score:     0.5, // Все записи имеют одинаковый score!
				ShardID:   int64(i),
				Timestamp: int64((i*recordsPerShard + j) * 1000),
			})
		}
		
		results[i] = result
	}
	
	return results
}

// generateEmptyShardResults генерирует результаты с пустыми шардами (edge case тест)
func generateEmptyShardResults(shardCount int) []*ShardResult {
	results := make([]*ShardResult, shardCount)
	
	for i := 0; i < shardCount; i++ {
		result := GetShardResult()
		result.ShardID = int64(i)
		// Records пустой слайс
		
		results[i] = result
	}
	
	return results
}

// generateMixedShardResults генерирует результаты с разным количеством записей на шарде
func generateMixedShardResults(shardCount int, baseRecords int) []*ShardResult {
	results := make([]*ShardResult, shardCount)
	
	for i := 0; i < shardCount; i++ {
		result := GetShardResult()
		result.ShardID = int64(i)
		
		// Разное количество записей на каждом шарде (экспоненциальное распределение)
		recordsOnShard := baseRecords / (i + 1)
		
		for j := 0; j < recordsOnShard; j++ {
			key := fmt.Sprintf("key-shard-%d-record-%d", i, j)
			
			result.Records = append(result.Records, Record{
				ID:        int32(i*baseRecords + j),
				Key:       []byte(key),
				Value:     []byte(fmt.Sprintf("value-%d", j)),
				Score:     float32(1.0 - float32(j)/float32(recordsOnShard)),
				ShardID:   int64(i),
				Timestamp: int64((i*baseRecords + j) * 1000),
			})
		}
		
		results[i] = result
	}
	
	return results
}
