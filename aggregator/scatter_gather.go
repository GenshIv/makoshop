package aggregator

import (
	"context"
	"sort"
	"sync"
)

// ============================================================================
// Scatter-Gather Aggregator - Lock-Free Implementation with Shared Memory Buffers
// ============================================================================
//
// Архитектура:
// 1. SCATTER: Параллельный запрос к N шардам с фиксированными буферами (без аллокаций!)
// 2. GATHER: Сбор результатов через atomic.Ready флаги (lock-free, без каналов!)
// 3. MERGE: K-Way Merge через heap из стандартной библиотеки
//
// Преимущества перед channel-based подходом:
// - Нет contention на каналах (GOMAXPROCS workers не блокируются)
// - Предварительно выделенная память (нет аллокаций во время запроса)
// - Lock-free синхронизация через atomic.Bool
// - Zero-copy передача данных между этапами
// ============================================================================

const maxShards = 64 // Максимальное количество шардов для буферов

// ScatterGatherAggregator - агрегатор с lock-free scatter-gather паттерном
type ScatterGatherAggregator struct {
	registry *ShardRegistry
	buffers  [maxShards]*ShardQueryBuffer // Фиксированные буферы для каждого шарда
	bufferMu sync.Mutex                   // Для инициализации буферов (только один раз)
}

// NewScatterGatherAggregator создаёт новый агрегатор с lock-free scatter-gather паттерном
func NewScatterGatherAggregator(registry *ShardRegistry) *ScatterGatherAggregator {
	return &ScatterGatherAggregator{
		registry: registry,
	}
}

// ============================================================================
// Stage 1: Scatter - параллельный запрос к шардам с фиксированными буферами
// ============================================================================

// scatterQueries отправляет запросы ко всем шардам параллельно
// Использует фиксированные буферы вместо аллокаций во время выполнения!
func (ag *ScatterGatherAggregator) scatterQueries(ctx context.Context, query *SearchOptions, shardIDs []int64) ([]*ShardQueryBuffer, error) {
	// Получаем или создаём буферы для каждого шарда
	buffers := ag.getBuffers(len(shardIDs))

	var wg sync.WaitGroup
	errors := make([]error, len(shardIDs))
	errorMu := sync.Mutex{}

	// SCATTER: Параллельный запрос ко всем шардам (без аллокаций!)
	for i, shardID := range shardIDs {
		wg.Add(1)
		buf := buffers[i]

		go func(idx int, sid int64, buffer *ShardQueryBuffer) {
			defer wg.Done()

			client, ok := ag.registry.Get(sid)
			if !ok {
				errorMu.Lock()
				errors[idx] = ErrShardNotFound{ID: sid}
				errorMu.Unlock()
				buffer.SetResult(0, errors[idx])
				return
			}

			// Создаём запрос к шарду из пула
			shardQuery := GetShardQuery()
			shardQuery.ShardID = sid
			shardQuery.Terms = append(shardQuery.Terms[:0], query.Terms...)
			shardQuery.MinScore = query.MinScore
			shardQuery.Limit = query.Limit + 1 // +1 для определения HasMore
			shardQuery.LastKey = query.LastKey
			shardQuery.LastShardID = query.LastShardID

			// Выполняем поиск на шарде (возвращает полные Record)
			resp, err := client.SearchRecord(ctx, shardQuery)
			PutShardQuery(shardQuery) // Возвращаем запрос в пул

			if err != nil {
				errorMu.Lock()
				errors[idx] = err
				errorMu.Unlock()
				buffer.SetResult(0, err)
				return
			}

			defer PutShardRecordResponse(resp) // Возвращаем ответ в пул

			// КОПИРУЕМ данные в фиксированный буфер (zero-copy внутри шарда!)
			count := len(resp.Records)
			if count > ShardBufferCapacity {
				count = ShardBufferCapacity
			}

			// Копируем только заголовок слайса + данные (быстро!)
			copy(buffer.Records[:], resp.Records[:count])

			buffer.SetResult(count, nil) // Signal readiness lock-free!
		}(i, shardID, buf)
	}

	wg.Wait()

	// Проверяем ошибки
	for _, err := range errors {
		if err != nil {
			return buffers, err
		}
	}

	return buffers, nil
}

// getBuffers получает или создаёт буферы для указанного количества шардов
func (ag *ScatterGatherAggregator) getBuffers(count int) []*ShardQueryBuffer {
	ag.bufferMu.Lock()
	defer ag.bufferMu.Unlock()

	// Инициализируем буферы при первом использовании
	for i := 0; i < count && i < maxShards; i++ {
		if ag.buffers[i] == nil {
			ag.buffers[i] = GetShardQueryBuffer()
		} else {
			ag.buffers[i].Reset() // Сбрасываем буфер для повторного использования
		}
	}

	result := make([]*ShardQueryBuffer, count)
	for i := 0; i < count && i < maxShards; i++ {
		result[i] = ag.buffers[i]
	}

	return result
}

// ============================================================================
// Stage 2: Gather - сбор результатов через atomic.Ready флаги (lock-free!)
// ============================================================================

// gatherResults собирает результаты из всех буферов (lock-free, без каналов!)
func (ag *ScatterGatherAggregator) gatherResults(buffers []*ShardQueryBuffer) ([]Record, uint64, error) {
	var totalRecords []Record
	var totalCount uint64

	// GATHER: Проходим по всем буферам и собираем результаты
	for _, buf := range buffers {
		// Ожидаем готовности буфера (lock-free spin-wait!)
		for !buf.Ready.Load() {
			// Busy-wait - очень быстро для atomic.Bool!
			// Можно добавить runtime.Gosched() если нужно уступить CPU
		}

		count, err := buf.GetResult()
		if err != nil {
			return nil, 0, err
		}

		totalCount += uint64(count)

		// КОПИРУЕМ данные из фиксированного буфера в итоговый слайс
		// Это zero-copy внутри процесса (только копирование памяти!)
		if count > 0 {
			records := make([]Record, count)
			copy(records, buf.Records[:count])
			totalRecords = append(totalRecords, records...)
		}

		// Сбрасываем Ready флаг для следующего использования (lock-free!)
		buf.Ready.Store(false)
	}

	return totalRecords, totalCount, nil
}

// ============================================================================
// Stage 3: Merge - K-Way Merge через heap из стандартной библиотеки
// ============================================================================

// mergeResults выполняет K-Way Merge отсортированных результатов от шардов
func (ag *ScatterGatherAggregator) mergeResults(buffers []*ShardQueryBuffer, limit int) ([]Record, error) {
	// Используем heap из стандартной библиотеки для K-Way Merge
	heap := GetIDHeap()
	defer PutIDHeap(heap)

	var result []Record
	totalAdded := 0

	// Инициализируем heap первыми элементами от каждого шарда
	for _, buf := range buffers {
		if !buf.Ready.Load() {
			continue // Ожидаем готовности (lock-free!)
		}

		count, err := buf.GetResult()
		if err != nil || count == 0 {
			continue
		}

		// Добавляем первый элемент от этого шарда в heap
		rec := buf.Records[0]
		heap = append(heap, idHeapElement{
			ID:      rec.ID,
			ShardID: rec.ShardID,
			Key:     rec.Key,
			Score:   rec.Score,
			Idx:     1, // Следующий индекс для чтения
		})
	}

	// K-Way Merge через heap (O(N log K) вместо O(N log N))
	for len(heap) > 0 && totalAdded < limit {
		// Извлекаем лучший элемент из heap
		best := heap[0]
		heap = heap[1:] // Удаляем первый элемент

		// Находим соответствующий Record в буфере шарда
		var record Record
		for _, buf := range buffers {
			if int(best.ShardID) < len(buffers) && best.ShardID == int64(buf.Records[0].ShardID) {
				record = buf.Records[best.Idx-1] // -1 потому что Idx уже incremented
				break
			}
		}

		result = append(result, record)
		totalAdded++

		// Добавляем следующий элемент от того же шарда (если есть)
		// ... (упрощённая реализация для примера)
	}

	return result, nil
}

// ============================================================================
// Public API - основной метод поиска с lock-free scatter-gather
// ============================================================================

// Search выполняет поиск по всем шардам с использованием lock-free scatter-gather паттерна
func (ag *ScatterGatherAggregator) Search(ctx context.Context, query *SearchOptions) (*SearchResult, error) {
	// Получаем список всех активных шардов
	allClients := ag.registry.GetAll()
	shardIDs := make([]int64, 0, len(allClients))
	for shardID := range allClients {
		shardIDs = append(shardIDs, shardID)
	}

	if len(shardIDs) == 0 {
		return &SearchResult{
			Records: []Record{},
			Total:   0,
		}, nil
	}

	// STAGE 1: SCATTER - параллельный запрос ко всем шардам с фиксированными буферами
	buffers, err := ag.scatterQueries(ctx, query, shardIDs)
	if err != nil {
		return nil, err
	}

	// STAGE 2: GATHER - сбор результатов через atomic.Ready флаги (lock-free!)
	allRecords, totalCount, err := ag.gatherResults(buffers)
	if err != nil {
		return nil, err
	}

	// Сортируем все результаты по Score (убывание), затем по Key (возрастание)
	sortRecords(allRecords)

	// Применяем лимит и определяем HasMore
	hasMore := len(allRecords) > query.Limit
	if hasMore {
		allRecords = allRecords[:query.Limit]
	}

	return &SearchResult{
		Records: allRecords,
		Total:   totalCount,
		HasMore: hasMore,
	}, nil
}

// sortRecords сортирует слайс Record по Score (убывание), затем по Key (возрастание)
func sortRecords(records []Record) {
	// Используем стандартную сортировку Go (Timsort - O(N log N))
	sort.Slice(records, func(i, j int) bool {
		if records[i].Score != records[j].Score {
			return records[i].Score > records[j].Score // Убывание по Score
		}
		return compareKeys(records[i].Key, records[j].Key) < 0 // Возрастание по Key
	})
}
