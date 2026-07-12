package aggregator

import "fmt"

// ============================================================================
// Custom Errors - кастомные ошибки для агрегатора
// ============================================================================

// ErrShardNotFound - ошибка когда шард не найден в реестре
type ErrShardNotFound struct {
	ID int64
}

func (e ErrShardNotFound) Error() string {
	return fmt.Sprintf("shard %d not found in registry", e.ID)
}

// ErrNoActiveShards - ошибка когда нет активных шардов
type ErrNoActiveShards struct{}

func (e ErrNoActiveShards) Error() string {
	return "no active shards available"
}

// ErrPartialResults - ошибка частичных результатов (не все шарды ответили)
type ErrPartialResults struct {
	TotalShards  int
	Responded    int
	FailedErrors []error
}

func (e ErrPartialResults) Error() string {
	return fmt.Sprintf("partial results: %d/%d shards responded", e.Responded, e.TotalShards)
}

// ErrQueryTimeout - ошибка тайм-аута запроса
type ErrQueryTimeout struct {
	Duration int64 // в миллисекундах
}

func (e ErrQueryTimeout) Error() string {
	return fmt.Sprintf("query timeout after %d ms", e.Duration)
}
