package aggregator

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// ============================================================================
// Constants for Fixed-Size Buffers (NO EXPANSION!)
// ============================================================================

const (
	// ShardBufferCapacity — фиксированная ёмкость буфера шарда (не расширяется!)
	ShardBufferCapacity = 4096 // Достаточно для большинства запросов

	// DefaultRecordSliceCapacity — ёмкость RecordSlice
	DefaultRecordSliceCapacity = 4096

	// DefaultFinalResultCapacity — ёмкость финального результата
	DefaultFinalResultCapacity = 1024
)

// ============================================================================
// ShardQueryBuffer Pool (Fixed-Size, Lock-Free!)
// ============================================================================

var ShardQueryBufferPool = sync.Pool{
	New: func() interface{} {
		return &ShardQueryBuffer{
			Records: make([]Record, 0, ShardBufferCapacity),
		}
	},
}

// ShardQueryBuffer — буфер с фиксированной ёмкостью для результатов шарда
// Layout: данные и atomic.Bool разделены для предотвращения false sharing!
// Cache-line alignment (64 bytes):
//   [0..23]    Records header []Record  (pointer=8, len=8, cap=8)
//   [24..31]    Error pointer           (8 bytes)
//   [32..63]    _ padding               (32 bytes — выравнивание до 64b)
//   [64..71]    Ready atomic.Bool       (в отдельной cache line!)
type ShardQueryBuffer struct {
	Records []Record    // Pre-allocated слайс (макс. ShardBufferCapacity)
	Error   error       // Ошибка запроса к шарду
	_       [32]byte    // Padding до 64 байт — изоляция Ready от данных!
	Ready   atomic.Bool // Lock-free флаг готовности в отдельной cache line!
}

// GetShardQueryBuffer получает буфер из пула
func GetShardQueryBuffer() *ShardQueryBuffer {
	buf := ShardQueryBufferPool.Get().(*ShardQueryBuffer)
	// Сбрасываем состояние для повторного использования
	buf.Records = buf.Records[:0] // Оставляем capacity, обнуляем length
	buf.Error = nil
	buf.Ready.Store(false)
	return buf
}

// PutShardQueryBuffer возвращает буфер в пул
func PutShardQueryBuffer(buf *ShardQueryBuffer) {
	if cap(buf.Records) > ShardBufferCapacity*2 {
		// Защита от разрастания capacity — создаём новый слайс
		buf.Records = make([]Record, 0, ShardBufferCapacity)
	} else {
		// Trim length перед Put — стандартный паттерн для sync.Pool!
		buf.Records = buf.Records[:0]
	}
	buf.Error = nil
	buf.Ready.Store(false)
	ShardQueryBufferPool.Put(buf)
}

// SetResult устанавливает результат БЕЗ блокировок (lock-free!)
func (buf *ShardQueryBuffer) SetResult(count int, err error) {
	buf.Error = err
	// Устанавливаем фактическое количество записей
	if count > len(buf.Records) {
		count = len(buf.Records) // Защита от переполнения
	}
	buf.Records = buf.Records[:count]
	buf.Ready.Store(true) // Атомарная установка флага — потребители увидят!
}

// WaitReady блокирующее ожидание готовности (для gather фазы)
func (buf *ShardQueryBuffer) WaitReady() {
	for !buf.Ready.Load() {
		runtime.Gosched() // Уступаем процессор другим goroutine
	}
}

// GetResult получает результат из буфера (количество записей и ошибку)
func (buf *ShardQueryBuffer) GetResult() (int, error) {
	return len(buf.Records), buf.Error
}

// Reset сбрасывает буфер для повторного использования
func (buf *ShardQueryBuffer) Reset() {
	buf.Records = buf.Records[:0]
	buf.Error = nil
	buf.Ready.Store(false)
}

// IsReady проверяет готовность без блокировки
func (buf *ShardQueryBuffer) IsReady() bool {
	return buf.Ready.Load()
}

// ============================================================================
// RecordSlice Pool (Fixed-Size)
// ============================================================================

var RecordsSlicePool = sync.Pool{
	New: func() interface{} {
		return &RecordSlice{
			Records: make([]Record, 0, DefaultRecordSliceCapacity),
		}
	},
}

// GetRecordSlice - получение RecordSlice из пула с очисткой состояния
func GetRecordSlice() *RecordSlice {
	slice := RecordsSlicePool.Get().(*RecordSlice)
	// Сбрасываем состояние для повторного использования
	if slice.Records != nil {
		slice.Records = slice.Records[:0]
	}
	return slice
}

// Release - освобождение слайса обратно в пул
func (rs *RecordSlice) Release() {
	if rs.Records != nil {
		rs.Records = rs.Records[:0] // Trim перед Put — предотвращает memory bloat!
	}
	RecordsSlicePool.Put(rs)
}

// ============================================================================
// BitmapContext Pool
// ============================================================================

var BitmapPool = sync.Pool{
	New: func() interface{} {
		return &BitmapContext{}
	},
}

// GetBitmapContext получает BitmapContext из пула с очисткой состояния
func GetBitmapContext() *BitmapContext {
	ctx := BitmapPool.Get().(*BitmapContext)
	// Сбрасываем состояние для повторного использования
	ctx.Mask = 0
	return ctx
}

// Release - освобождение контекста обратно в пул
func (bc *BitmapContext) Release() {
	bc.Mask = 0 // Сброс состояния перед Put
	BitmapPool.Put(bc)
}

// ============================================================================
// SearchQuery Pool
// ============================================================================

var QueryPool = sync.Pool{
	New: func() interface{} {
		return &SearchQuery{
			Terms: make([]string, 0, 16),
		}
	},
}

// GetSearchQuery получает SearchQuery из пула с очисткой состояния
func GetSearchQuery() *SearchQuery {
	query := QueryPool.Get().(*SearchQuery)
	// Сбрасываем состояние для повторного использования
	if query.Terms != nil {
		query.Terms = query.Terms[:0]
	}
	query.MinScore = 0
	query.Limit = 0
	query.Offset = 0
	if len(query.LastKey) > 0 {
		query.LastKey = query.LastKey[:0]
	}
	query.ShardMask = 0
	return query
}

// Release - освобождение запроса обратно в пул
func (q *SearchQuery) Release() {
	if q.Terms != nil {
		q.Terms = q.Terms[:0] // Trim перед Put
	}
	if len(q.LastKey) > 0 {
		q.LastKey = q.LastKey[:0]
	}
	q.MinScore = 0
	q.Limit = 0
	q.Offset = 0
	q.ShardMask = 0
	QueryPool.Put(q)
}

// ============================================================================
// FinalResult Pool (Fixed-Size)
// ============================================================================

var FinalResultPool = sync.Pool{
	New: func() interface{} {
		return make([]Record, 0, DefaultFinalResultCapacity)
	},
}

// GetFinalResult получает слайс для финальных результатов из пула
func GetFinalResult(capacity int) []Record {
	if capacity > DefaultFinalResultCapacity*2 {
		// Для больших запросов создаём новый слайс
		return make([]Record, 0, capacity)
	}
	result := FinalResultPool.Get().([]Record)
	if cap(result) < capacity {
		// Если ёмкость недостаточна, создаём новый
		return make([]Record, 0, capacity)
	}
	return result[:0] // Возвращаем очищенный слайс
}

// PutFinalResult возвращает слайс обратно в пул
func PutFinalResult(result []Record) {
	if cap(result) <= DefaultFinalResultCapacity*2 { // Не храним слишком большие слайсы в пуле
		result = result[:0] // Trim перед Put — предотвращает memory bloat!
		FinalResultPool.Put(result)
	}
}
