package aggregator

import "sync/atomic"

// ShardResultBuffer - буфер результата от шарда без блокировок
type ShardResultBuffer struct {
	Records []Record
	Error   error
	Ready   atomic.Bool // Атомарный флаг готовности (без блокировок!)
}

// NewShardResultBuffer создаёт новый буфер для шарда
func NewShardResultBuffer(capacity int) *ShardResultBuffer {
	return &ShardResultBuffer{
		Records: make([]Record, 0, capacity),
	}
}

// SetResult устанавливает результат и флаг готовности (без блокировок!)
func (buf *ShardResultBuffer) SetResult(records []Record, err error) {
	buf.Records = records
	buf.Error = err
	buf.Ready.Store(true) // Атомарная установка флага - НЕ БЛОКИРУЕТ!
}

// GetResult получает результат и сбрасывает флаг (без блокировок!)
func (buf *ShardResultBuffer) GetResult() ([]Record, error) {
	records := buf.Records
	err := buf.Error
	// Сбрасываем состояние для повторного использования
	if buf.Records != nil {
		buf.Records = buf.Records[:0]
	}
	buf.Error = nil
	return records, err
}

// IsReady проверяет готовность буфера (без блокировок!)
func (buf *ShardResultBuffer) IsReady() bool {
	return buf.Ready.Load()
}

// Reset сбрасывает буфер для повторного использования
func (buf *ShardResultBuffer) Reset() {
	if buf.Records != nil {
		buf.Records = buf.Records[:0]
	}
	buf.Error = nil
	buf.Ready.Store(false)
}
