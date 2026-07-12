package aggregator

import (
	"sync"
)

// ============================================================================
// Object Pools - уменьшение аллокаций через переиспользование объектов
// ============================================================================

var (
	// Pool для ShardQuery объектов
	shardQueryPool = sync.Pool{
		New: func() interface{} {
			return &ShardQuery{
				Terms: make([]string, 0, 8), // Предварительная ёмкость для терминов
				Limit: defaultLimit,
			}
		},
	}

	// Pool для ShardIDResponse объектов (без данных)
	shardIDResponsePool = sync.Pool{
		New: func() interface{} {
			return &ShardIDResponse{
				IDs: make([]int32, 0, defaultLimit+1), // Предварительная ёмкость для IDs
			}
		},
	}

	// Pool для ShardRecordResponse объектов (без данных)
	shardRecordResponsePool = sync.Pool{
		New: func() interface{} {
			return &ShardRecordResponse{
				Records: make([]Record, 0, defaultLimit+1), // Предварительная ёмкость для Record
			}
		},
	}

	// Pool для min-heap в KWayMerge (размер до 64 шардов)
	minHeapPool = sync.Pool{
		New: func() interface{} {
			return make(idHeap, 0, 64) // Максимум 64 шарда
		},
	}

	// Pool для временных буферов при merge (размер до 10K записей)
	mergeBufferPool = sync.Pool{
		New: func() interface{} {
			return make([]int32, 0, 10000) // Буфер для IDs при merge
		},
	}
)

// ============================================================================
// ShardQuery Pool Helpers
// ============================================================================

// GetShardQuery извлекает ShardQuery из пула или создаёт новый
func GetShardQuery() *ShardQuery {
	query := shardQueryPool.Get().(*ShardQuery)
	// Сбрасываем состояние для повторного использования
	query.ShardID = 0
	query.Terms = query.Terms[:0] // Очищаем, но сохраняем ёмкость
	query.LastKey = query.LastKey[:0]
	query.Limit = defaultLimit
	query.SortBy = ""
	return query
}

// PutShardQuery возвращает ShardQuery в пул
func PutShardQuery(query *ShardQuery) {
	if query == nil {
		return
	}
	// Очистка происходит только в GetShardQuery - стандартный паттерн Go
	shardQueryPool.Put(query)
}

// ============================================================================
// ShardIDResponse Pool Helpers
// ============================================================================

// GetShardIDResponse извлекает ShardIDResponse из пула или создаёт новый
func GetShardIDResponse() *ShardIDResponse {
	resp := shardIDResponsePool.Get().(*ShardIDResponse)
	// Сбрасываем состояние для повторного использования
	resp.ShardID = 0
	resp.Total = 0
	resp.IDs = resp.IDs[:0] // Очищаем, но сохраняем ёмкость
	return resp
}

// PutShardIDResponse возвращает ShardIDResponse в пул
func PutShardIDResponse(resp *ShardIDResponse) {
	if resp == nil {
		return
	}
	// Очистка происходит только в GetShardIDResponse - стандартный паттерн Go
	shardIDResponsePool.Put(resp)
}

// ============================================================================
// ShardRecordResponse Pool Helpers
// ============================================================================

// GetShardRecordResponse извлекает ShardRecordResponse из пула или создаёт новый
func GetShardRecordResponse() *ShardRecordResponse {
	resp := shardRecordResponsePool.Get().(*ShardRecordResponse)
	// Сбрасываем состояние для повторного использования
	resp.ShardID = 0
	resp.Total = 0
	resp.Records = resp.Records[:0] // Очищаем, но сохраняем ёмкость
	return resp
}

// PutShardRecordResponse возвращает ShardRecordResponse в пул
func PutShardRecordResponse(resp *ShardRecordResponse) {
	if resp == nil {
		return
	}
	// Очистка происходит только в GetShardRecordResponse - стандартный паттерн Go
	shardRecordResponsePool.Put(resp)
}

// ============================================================================
// MinHeap Pool Helpers (для KWayMerge)
// ============================================================================

// GetIDHeap извлекает idHeap из пула или создаёт новый (для container/heap)
func GetIDHeap() idHeap {
	heap := minHeapPool.Get().(idHeap)
	heap = heap[:0] // Очищаем, но сохраняем ёмкость
	return heap
}

// PutIDHeap возвращает idHeap в пул (для container/heap)
func PutIDHeap(heap idHeap) {
	if cap(heap) <= 64 { // Не храним слишком большие heaps в пуле
		minHeapPool.Put(heap)
	}
}

// ============================================================================
// Merge Buffer Pool Helpers
// ============================================================================

// GetMergeBuffer извлекает буфер для merge операций
func GetMergeBuffer(capacity int) []int32 {
	if capacity > 10000 {
		// Для больших запросов создаём новый слайс
		return make([]int32, 0, capacity)
	}
	buf := mergeBufferPool.Get().([]int32)
	if cap(buf) < capacity {
		// Если ёмкость недостаточна, создаём новый
		return make([]int32, 0, capacity)
	}
	return buf[:0] // Возвращаем очищенный буфер
}

// PutMergeBuffer возвращает буфер в пул
func PutMergeBuffer(buf []int32) {
	if cap(buf) <= 10000 { // Не храним слишком большие буферы в пуле
		mergeBufferPool.Put(buf)
	}
}

// ============================================================================
// Record Pool - для переиспользования Record объектов (опционально)
// ============================================================================

var recordPool = sync.Pool{
	New: func() interface{} {
		return &Record{
			Key:   make([]byte, 0, 64),  // Средняя длина ключа ~32 байта
			Value: make([]byte, 0, 256), // Среднее значение ~128 байт
		}
	},
}

// GetRecord извлекает Record из пула или создаёт новый
func GetRecord() *Record {
	rec := recordPool.Get().(*Record)
	rec.ID = 0
	rec.ShardID = 0
	rec.Key = rec.Key[:0]
	rec.Value = rec.Value[:0]
	// Поле Date удалено для cache-line alignment (64 bytes).
	// Дата хранится как часть Value (JSON) или извлекается при необходимости.
	return rec
}

// PutRecord возвращает Record в пул
func PutRecord(rec *Record) {
	if rec == nil {
		return
	}
	// Очистка происходит только в GetRecord - стандартный паттерн Go
	recordPool.Put(rec)
}

// ============================================================================
// Batch Release Helpers - для массового освобождения ресурсов
// ============================================================================

// ReleaseAllShardResponses освобождает все ShardIDResponse из слайса
func ReleaseAllShardResponses(responses []*ShardIDResponse) {
	for _, resp := range responses {
		PutShardIDResponse(resp)
	}
}

// ReleaseAllRecordResponses освобождает все ShardRecordResponse из слайса
func ReleaseAllRecordResponses(responses []*ShardRecordResponse) {
	for _, resp := range responses {
		PutShardRecordResponse(resp)
	}
}

// ReleaseAllRecords освобождает все Record из слайса
func ReleaseAllRecords(records []Record) {
	// Record - это struct, а не pointer, поэтому не нужно освобождать каждый
	// Если бы мы использовали *Record, то:
	// for _, rec := range records {
	//     PutRecord(rec)
	// }
}

// ============================================================================
// Cleanup - для тестирования и shutdown
// ============================================================================

// ClearPools очищает все пулы (используется только в тестах!)
func ClearPools() {
	shardQueryPool.New = func() interface{} { return &ShardQuery{} }
	shardIDResponsePool.New = func() interface{} { return &ShardIDResponse{} }
	shardRecordResponsePool.New = func() interface{} { return &ShardRecordResponse{} }
	minHeapPool.New = func() interface{} { return make(idHeap, 0, 64) }
	mergeBufferPool.New = func() interface{} { return make([]int32, 0, 10000) }
	recordPool.New = func() interface{} { return &Record{} }
}
