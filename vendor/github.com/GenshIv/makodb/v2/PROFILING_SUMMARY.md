# Профилирование аллокаций MakoDB — Итоговый отчёт

## Выполненные работы

### 1. Анализ аллокаций

Запущены бенчмарки с heap profiler:
- `go test -bench=. -benchmem` — базовые метрики
- `go test -memprofile=mem.prof` — детальный heap профиль
- `go tool pprof -top -alloc_space` — топ аллокаций по размеру
- `go tool pprof -top -alloc_objects` — топ аллокаций по количеству

### 2. Ключевые находки

**Горячие точки аллокаций:**
1. `DB.Get` — 50 B/op, 1 alloc/op (копирование значения из mmap)
2. `TurboPutIndex` — 226 KB/op, 5 allocs/op (read-parse-sort-serialize-write)
3. `TurboIntersect` — 61 KB/op, 22 allocs/op (map-based intersection)
4. `TurboBinaryIntersectRaw` — 112 KB/op, 30 allocs/op (map-based)
5. `TurboContainsIndex` — 81 KB/op, 1 alloc/op (read-all-then-search)
6. `TurboSortIndexIntersectWithCandidatesFromDB` — ~50 MB/op (read full index)

### 3. Реализованные оптимизации

#### P0: Merge-style intersection (вместо map) — ГОТОВО

Заменены map-based операции на merge-style для sorted data:

| Функция | Изменение |
|---------|-----------|
| `TurboBinaryIntersectRaw` | map → merge-style |
| `TurboBinaryUnionRaw` | map → merge-style |
| `TurboBinaryDiff` | map → merge-style |
| `turboIntersectIndexResults` | nested loops → merge-style |
| `turboBulkIntersect` | map → merge-style |

Добавлены helper функции:
- `turboSortedIntersect(a, b []uint64) []uint64`
- `turboSortedUnion(a, b []uint64) []uint64`
- `turboBinarySearchInsertionPoint(tokenData []byte, count uint64, target uint64) int`

#### P1: Zero-allocation API — ГОТОВО

Добавлены zero-alloc варианты для hot path операций:

#### P2: Дополнительные оптимизации — ГОТОВО

**TurboPutIndex — in-place insert в sorted index:**
- Старый: read → parse → append → RadixSort → serialize → write (226 KB/op, 5 allocs)
- Новый: read → binary search → in-place insert → write (213 KB/op, 2 allocs)
- Убраны 3 аллокации (parse, sort, serialize), размер тот же (чтение всего индекса неизбежно)

**TurboDeleteIndex — binary search вместо linear scan:**
- Старый: read → parse → linear search → swap-delete → serialize → write
- Новый: read → binary search → in-place delete → write
- O(log n) вместо O(n) для поиска

**TurboIntersectToBitmap — merge-style вместо map:**
- Старый: 1 MB/op, 97 allocs
- Новый: 389 KB/op, 6 allocs — **94% меньше allocs!**

**TurboUnionToBitmap — merge-style вместо map:**
- Старый: 2.2 MB/op, 166 allocs
- Новый: 761 KB/op, 6 allocs — **96% меньше allocs!**

**ForEachZeroAlloc — zero-alloc iteration:**
- Старый (ForEach): 31.6μs/op, 64 KB, 2000 allocs (1000 records)
- Новый (ForEachZeroAlloc): 2.6μs/op, 0 B, 0 allocs — **12x быстрее, 100% меньше allocs!**

| DB | ShardedDB | Описание |
|----|-----------|----------|
| `GetZeroAlloc(key) ([]byte, error)` | ✅ | Direct view into mmap, 0 alloc |
| `GetInto(key, buf []byte) (int, error)` | ✅ | Copy into caller's buffer, 0 alloc |
| `QueryZeroAlloc(key, target) error` | ✅ | Zero-alloc JSON parsing |

### 4. Результаты

#### Turbo операции

| Бенчмарк | До | После | Улучшение |
|----------|-----|-------|-----------|
| TurboIntersect | 212531 ns/op, 61 KB, 22 allocs | 12168 ns/op, 61 KB, 8 allocs | **17x быстрее**, 64% меньше allocs |
| TurboBulkIntersect | 41466 ns/op, 135 KB, 33 allocs | 11725 ns/op, 61 KB, 9 allocs | **3.5x быстрее**, 73% меньше allocs |
| TurboBinaryIntersectRaw | 38608 ns/op, 112 KB, 30 allocs | 7542 ns/op, 38 KB, 6 allocs | **5x быстрее**, 80% меньше allocs |
| TurboBinaryUnionRaw | 85506 ns/op, 163 KB, 35 allocs | 13703 ns/op, 75 KB, 6 allocs | **6x быстрее**, 83% меньше allocs |
| TurboAndNot | 269159 ns/op, 628 KB, 49 allocs | 47894 ns/op, 286 KB, 4 allocs | **5.6x быстрее**, 92% меньше allocs |
| TurboIndexIntersect | 10494788 ns/op, 475 KB, 33 allocs | 61099 ns/op, 348 KB, 8 allocs | **172x быстрее**, 76% меньше allocs |

#### Get/Query операции

| Бенчмарк | До | После | Улучшение |
|----------|-----|-------|-----------|
| Get (allocated) | 22.22 ns/op, 50 B, 1 alloc | 22.22 ns/op, 50 B, 1 alloc | (baseline) |
| GetZeroAlloc | — | 7.52 ns/op, 2 B, **0 alloc** | **3x быстрее**, **100% меньше allocs** |
| GetInto | — | 8.44 ns/op, 2 B, **0 alloc** | **2.6x быстрее**, **100% меньше allocs** |
| Query (allocated) | 46.08 ns/op, 50 B, 1 alloc | 46.08 ns/op, 50 B, 1 alloc | (baseline) |
| QueryZeroAlloc | — | 37.74 ns/op, 2 B, **0 alloc** | **1.8x быстрее**, **100% меньше allocs** |

#### P2 результаты

| Бенчмарк | До | После | Улучшение |
|----------|-----|-------|----------|
| TurboPutIndex | 226 KB/op, 5 allocs | 213 KB/op, 2 allocs | **60% меньше allocs** |
| TurboIntersectToBitmap | 1 MB/op, 97 allocs | 389 KB/op, 6 allocs | **94% меньше allocs** |
| TurboUnionToBitmap | 2.2 MB/op, 166 allocs | 761 KB/op, 6 allocs | **96% меньше allocs** |
| TurboIntersectToBitmapFromDB | 1.2 MB/op, 101 allocs | 634 KB/op, 10 allocs | **90% меньше allocs** |
| ForEach (1000 rec) | 31.6μs, 64 KB, 2000 allocs | 2.6μs, 0 B, 0 allocs | **12x быстрее, 100% меньше allocs** |

#### SIGSEGV fix

Исправлена race condition в ShardUsages/ActiveUsage:
- Старый порядок: check closeOnce → incRef → double-check (race window!)
- Новый порядок: incRef → check closeOnce (безопасно)

Убран ненадёжный `time.Sleep(1ms)` из FinishCompactShard.

### 5. Следующие шаги (P3)

1. **TurboSortIndexIntersectWithCandidatesFromDB** — пагинация (~50 MB/op)
2. **Search/Tokenize** — заменить strings.Split на manual parsing
3. **TurboContainsIndex** — использовать GetZeroAlloc вместо Get для чтения raw данных
4. **MultiGet** — оптимизировать batch операции (сейчас N отдельных Get)
5. **turboFilterIndexTokens** — использовать merge-style для include/exclude фильтров

## Как использовать zero-alloc API

```go
// Для hot path с коротким временем жизни данных:
val, err := db.GetZeroAlloc(key)
if err == nil {
    // val — view на mmap, использовать немедленно!
    process(val)
}

// Для repeated reads с known max size:
buf := make([]byte, 4096)
n, err := db.GetInto(key, buf)
if err == nil {
    value := buf[:n]
    process(value)
}

// Для JSON parsing без аллокации:
var doc MyStruct
err := db.QueryZeroAlloc(key, &doc)
```

## Тестирование

```bash
# Все тесты
go test -race -count=1 ./...

# Бенчмарки
go test -bench=. -benchmem -count=1 ./...

# Heap профиль
go test -bench=. -memprofile=mem.prof ./...
go tool pprof -top -alloc_space mem.prof

# CPU профиль
go test -bench=. -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof
```
