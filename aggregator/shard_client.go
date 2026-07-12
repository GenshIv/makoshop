package aggregator

import (
	"bytes"
	"context"
	"sort"
)

// ============================================================================
// MockShardClient - оптимизированный мок с pre-sorted indexes
// IMMUTABLE после создания - данные не меняются, lock-free чтение!
// ============================================================================

type MockShardClient struct {
	shardID int64
	records []Record

	// Pre-sorted indexes (как в makodb) - создаются при импорте
	sortedIDsByKey []int32 // IDs отсортированные по Key

	// НЕТ mu sync.RWMutex - данные immutable после создания!
}

// NewMockShardClient создает новый мок клиент с данными
// Данные IMMUTABLE после вызова этого конструктора
func NewMockShardClient(shardID int64, records []Record) *MockShardClient {
	client := &MockShardClient{
		shardID: shardID,
		records: records,
	}
	// Создаём pre-sorted indexes (как в makodb при импорте)
	// После этого вызова данные НЕ МЕНЯЮТСЯ - гарантируем Immutability
	client.buildSortedIndexes()
	return client
}

// buildSortedIndexes создаёт pre-sorted indexes для быстрого доступа
// Вызывается ТОЛЬКО ОДИН РАЗ в конструкторе
func (m *MockShardClient) buildSortedIndexes() {
	n := len(m.records)
	if n == 0 {
		m.sortedIDsByKey = []int32{}
		return
	}

	// Создаём массив ID для сортировки по Key
	ids := make([]int32, n)
	for i := range ids {
		ids[i] = int32(i)
	}
	sort.Slice(ids, func(i, j int) bool {
		return bytes.Compare(m.records[ids[i]].Key, m.records[ids[j]].Key) < 0
	})
	m.sortedIDsByKey = ids
}

// SearchID - быстрый поиск, возвращает только IDs (zero-copy!)
// LOCK-FREE: данные immutable после создания
func (m *MockShardClient) SearchID(ctx context.Context, query *ShardQuery) (*ShardIDResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// НЕТ mu.RLock() - данные immutable!

	// Бинарный поиск startIdx для O(log N)
	startIdx := 0
	if len(query.LastKey) > 0 {
		startIdx = sort.Search(len(m.records), func(i int) bool {
			return bytes.Compare(m.records[i].Key, query.LastKey) > 0
		})
	}

	// Собираем только IDs (zero-copy!) - используем пул!
	resultIDs := GetMergeBuffer(query.Limit + 1)
	count := 0
	for i := startIdx; i < len(m.records) && count < query.Limit+1; i++ {
		rec := m.records[i]
		if matchesTerms(rec, query.Terms) {
			resultIDs = append(resultIDs, int32(i)) // Только ID!
			count++
		}
	}

	resp := GetShardIDResponse()
	resp.ShardID = m.shardID
	resp.Total = uint64(len(m.records))
	resp.IDs = resultIDs // Используем буфер из пула

	return resp, nil
}

// SearchRecord - полный поиск с Record (для обратной совместимости)
// LOCK-FREE: данные immutable после создания
func (m *MockShardClient) SearchRecord(ctx context.Context, query *ShardQuery) (*ShardRecordResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// НЕТ mu.RLock() - данные immutable!

	// Бинарный поиск startIdx для O(log N)
	startIdx := 0
	if len(query.LastKey) > 0 {
		startIdx = sort.Search(len(m.records), func(i int) bool {
			return bytes.Compare(m.records[i].Key, query.LastKey) > 0
		})
	}

	// Собираем полные Record (для обратной совместимости)
	resultRecords := make([]Record, 0, query.Limit+1)
	count := 0
	for i := startIdx; i < len(m.records) && count < query.Limit+1; i++ {
		rec := m.records[i]
		if matchesTerms(rec, query.Terms) {
			resultRecords = append(resultRecords, rec)
			count++
		}
	}

	resp := GetShardRecordResponse()
	resp.ShardID = m.shardID
	resp.Total = uint64(len(m.records))
	resp.Records = resultRecords

	return resp, nil
}

// GetSortedIDs возвращает pre-sorted IDs для указанного поля сортировки
// LOCK-FREE: данные immutable после создания
func (m *MockShardClient) GetSortedIDs(ctx context.Context, sortBy string, limit int) ([]int32, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// НЕТ mu.RLock() - данные immutable!

	var sortedIDs []int32
	switch sortBy {
	case "id":
		// По ID - просто последовательность 0..N-1
		sortedIDs = make([]int32, len(m.records))
		for i := range sortedIDs {
			sortedIDs[i] = int32(i)
		}
	case "key", "date", "": // По умолчанию по Key
		sortedIDs = m.sortedIDsByKey
	default:
		// Для других полей - динамическая сортировка (можно кэшировать)
		n := len(m.records)
		sortedIDs = make([]int32, n)
		for i := range sortedIDs {
			sortedIDs[i] = int32(i)
		}
		sort.Slice(sortedIDs, func(i, j int) bool {
			return bytes.Compare(m.records[sortedIDs[i]].Key, m.records[sortedIDs[j]].Key) < 0
		})
	}

	// Пагинация
	if limit > 0 && len(sortedIDs) > limit {
		sortedIDs = sortedIDs[:limit]
	}

	return sortedIDs, nil
}

// MultiGet - batch read нескольких записей по ID (lock-free!)
// LOCK-FREE: данные immutable после создания
func (m *MockShardClient) MultiGet(ctx context.Context, ids []int32) (*ShardRecordResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// НЕТ mu.RLock() - данные immutable!

	records := make([]Record, 0, len(ids))
	for _, id := range ids {
		if id >= 0 && int(id) < len(m.records) {
			rec := m.records[id]
			records = append(records, rec)
		}
	}
	resp := GetShardRecordResponse()
	resp.ShardID = m.shardID
	resp.Total = uint64(len(records))
	resp.Records = records
	return resp, nil
}

// Close закрывает клиент
func (m *MockShardClient) Close() error {
	return nil
}

// ============================================================================
// Утилиты
// ============================================================================

// matchesTerms проверяет соответствие терминам запроса (zero-copy)
func matchesTerms(rec Record, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	for _, term := range terms {
		if !bytes.Contains(rec.Key, []byte(term)) && !bytes.Contains(rec.Value, []byte(term)) {
			return false
		}
	}
	return true
}

// ============================================================================
// ShardRegistry и ShardSelector - реализации (типы определены в types.go)
// ============================================================================

func (s *ShardSelector) SelectByMask(mask uint64) []int64 {
	var shards []int64
	for i := 0; i < 64; i++ {
		if mask&(1<<uint(i)) != 0 {
			shards = append(shards, int64(i))
		}
	}
	return shards
}

func (s *ShardSelector) SelectAll() []int64 {
	clients := s.registry.GetAll()
	shards := make([]int64, 0, len(clients))
	for shardID := range clients {
		shards = append(shards, shardID)
	}
	return shards
}
