package aggregator

import "unsafe"

// ============================================================================
// Zero-Copy Serialization Helpers (Этап 2 плана: Zero-Copy и Memory-Mapped Доступ)
// ============================================================================

// serializeIDs преобразует []int32 в []byte БЕЗ КОПИРОВАНИЯ через unsafe.Slice!
// Используется для передачи sorted IDs между шардами без аллокаций.
// HOT PATH: Zero-allocation, zero-copy projection на память int32 как byte slice.
//
// Безопасность (по протоколу использования unsafe):
// - Исходный слайс не модифицируется после сериализации
// - Десериализованный слайс используется только для чтения
// - Длина байтового слайса кратна размеру элемента (4 для int32)
func serializeIDs(ids []int32) []byte {
	if len(ids) == 0 {
		return nil
	}
	// unsafe.Slice создаёт view на память int32 как byte slice — БЕЗ КОПИРОВАНИЯ!
	// Каждый int32 занимает 4 байта, всего len(ids)*4 байт.
	return unsafe.Slice((*byte)(unsafe.Pointer(&ids[0])), len(ids)*4)
}

// deserializeIDs преобразует []byte обратно в []int32 БЕЗ КОПИРОВАНИЯ!
// Обратная операция к serializeIDs — zero-copy projection.
//
// Безопасность (по протоколу использования unsafe):
// - Исходный байтовый слайс не модифицируется после десериализации
// - Длина байтового слайса должна быть кратна 4 (размер int32)
func deserializeIDs(b []byte) []int32 {
	if len(b) == 0 {
		return nil
	}
	// unsafe.Slice создаёт view на память byte как int32 slice — БЕЗ КОПИРОВАНИЯ!
	// Количество int32 = len(b)/4 (каждый int32 = 4 байта).
	return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), len(b)/4)
}

// serializeInt64s преобразует []int64 в []byte БЕЗ КОПИРОВАНИЯ!
// Используется для сериализации ShardID и других 64-битных значений.
func serializeInt64s(ids []int64) []byte {
	if len(ids) == 0 {
		return nil
	}
	// Каждый int64 занимает 8 байт, всего len(ids)*8 байт.
	return unsafe.Slice((*byte)(unsafe.Pointer(&ids[0])), len(ids)*8)
}

// deserializeInt64s преобразует []byte обратно в []int64 БЕЗ КОПИРОВАНИЯ!
func deserializeInt64s(b []byte) []int64 {
	if len(b) == 0 {
		return nil
	}
	// Количество int64 = len(b)/8 (каждый int64 = 8 байт).
	return unsafe.Slice((*int64)(unsafe.Pointer(&b[0])), len(b)/8)
}

// ============================================================================
// Примечание по безопасности:
// Эти функции используют unsafe для zero-copy проекции памяти.
// Безопасность гарантируется при соблюдении условий:
// 1. Исходный слайс не модифицируется после сериализации
// 2. Десериализованный слайс используется только для чтения
// 3. Длина байтового слайса кратна размеру элемента (4 для int32, 8 для int64)
// ============================================================================
