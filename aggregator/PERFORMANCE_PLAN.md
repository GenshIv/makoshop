# План оптимизации Aggregator для High-Load

## Цель: Достичь производительности ~1B records/sec (как в референсе)

---

## Этап 0: Исследование ✅ ЗАВЕРШЁНО

### Выводы анализа makodb + demoserver:

#### Архитектура makodb:
```
┌─────────────────────────────────────────────┐
│ Header (64 bytes)                           │ ← offset 0
├─────────────────────────────────────────────┤
│ Hash Table Buckets (numBuckets × 32 bytes) │ ← headerSize  
├─────────────────────────────────────────────┤
│ Free Area (append-only data storage)        │ ← FreeOffset
└─────────────────────────────────────────────┘
```

#### Ключевые паттерны референса:

1. **Pre-built Sort Indexes**: Индексы сортировки создаются при импорте
   ```go
   "sort:total_revenue" → [123, 456, 789, ...] // IDs sorted by revenue
   ```

2. **Inverted Indexes**: Для фильтрации по категориям
   ```go
   "idx:item_type:Fruits" → [1, 5, 23, 45, ...] // IDs с этим item_type
   ```

3. **K-Way Merge на лету**: 
   - Берёт pre-sorted index из DB
   - Фильтрует по candidateMap (интерсекция инвертированных индексов)
   - Делает merge с recent transactions из памяти

4. **Zero-copy через unsafe.Slice**:
   ```go
   func serializeIDs(ids []int32) []byte {
       return unsafe.Slice((*byte)(unsafe.Pointer(&ids[0])), len(ids)*4)
   }
   ```

5. **Lock-Free Reads**: makodb использует только RLock для чтения

---

## Этап 1: K-Way Merge вместо Gather-All-Then-Sort

### Проблема текущей реализации:
```go
// ❌ O(N log N) - собираем ВСЁ и сортируем
for result := range resultsCh {
    allRecords = append(allRecords, result.Records...)
}
sort.Slice(allRecords)  // Сортировка 1M записей!
```

### Решение (как в референсе):

#### Вариант A: Использовать pre-built indexes makodb
```go
// 1. Получить pre-sorted index из makodb
sortedIDs, _ := db.Get("sort:" + sortBy)  // []int32 отсортированных ID

// 2. Фильтровать через candidateMap (интерсекция инвертированных индексов)
var filteredIDs []int32
for _, id := range sortedIDs {
    if candidateMap[id] {  // O(1) проверка
        filteredIDs = append(filteredIDs, id)
    }
}

// 3. Пагинация и fetch
pageIDs := filteredIDs[offset : offset+limit]
results, _ := db.MultiGet(pageIDs)  // Batch read
```

#### Вариант B: K-Way Merge для cross-shard сортировки
```go
// Для случаев когда нет pre-built index
type ShardIterator struct {
    shardID   int64
    sortedIDs []int32  // Pre-sorted IDs из шарда
    idx       int
}

func (s *ShardIterator) Next() (*Record, bool) {
    if s.idx >= len(s.sortedIDs) {
        return nil, false
    }
    id := s.sortedIDs[s.idx]
    s.idx++
    return shard.GetRecordByID(id)  // O(1) через hash table
}

// K-Way merge через min-heap
heap := make(ShardHeap, 0, shardCount)
for _, shard := range shards {
    heap.Push(shard.GetIterator())
}

for i := 0; i < limit && heap.Len() > 0; i++ {
    rec := heap.Pop().Next()  // Минимальный ключ
    result = append(result, rec)
}
```

### Подзадачи:
1. [ ] Добавить API для получения pre-sorted IDs из шардов
2. [ ] Реализовать ShardIterator интерфейс
3. [ ] Интегрировать k-way merge в gatherResults

---

## Этап 2: Zero-Copy и Memory-Mapped Доступ

### Проблема:
```go
// ❌ Копирование данных через границы процессов/памяти
response.Records = append(response.Records, rec)
allRecords = append(allRecords, result.Records...)
```

### Решение (как в референсе):

1. **Использовать unsafe.Slice для сериализации**:
   ```go
   func serializeIDs(ids []int32) []byte {
       return unsafe.Slice((*byte)(unsafe.Pointer(&ids[0])), len(ids)*4)
   }
   
   func deserializeIDs(b []byte) []int32 {
       return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), len(b)/4)
   }
   ```

2. **Работать с ID вместо полных Record**:
   - Сортировать только ID (4 bytes vs 100+ bytes)
   - Fetch полные записи только после сортировки и пагинации

3. **Использовать mmap напрямую** для доступа к данным шардов

---

## Этап 3: Индексированный Доступ на Уровне Шардов

### Решение (как в референсе):

1. **Добавить pre-built indexes в makodb**:
   ```go
   // При импорте данных создавать индексы:
   db.Put("sort:id", serializeIDs(sortedByID))
   db.Put("sort:date", serializeIDs(sortedByDate))
   db.Put("idx:item_type:Fruits", serializeIDs(idsWithFruits))
   ```

2. **Использовать seek-iterator паттерн**:
   ```go
   type ShardIterator interface {
       Seek(key []byte) bool    // Перейти к ключу (O(log N) через бинарный поиск в sorted IDs)
       Next() (*Record, bool)   // Следующая запись (O(1))
       Close() error
   }
   ```

---

## Этап 4: Оптимизация Keyset Pagination

### Решение (как в референсе):

1. **Комбинированный курсор**: `(key, shardID)` для полной уникальности
2. **Предварительная фильтрация на уровне шардов** через бинарный поиск в sorted IDs
3. **Кэширование позиции итератора между запросами** (для cursor-based pagination)

---

## Этап 5: Параллелизм и Конкурентность

### Текущее состояние:
```go
// ✅ Scatter-gather уже реализован
for _, shardID := range filteredShards {
    go func(id int64) { a.scatterToShard(ctx, id, opts, resultsCh) }(shardID)
}
```

### Улучшения (как в референсе):

1. **Worker pool** для ограничения concurrency
2. **Pipeline pattern**: scatter → merge → format как конвейер
3. **Batch processing**: группировка запросов через `MultiGet`

---

## Этап 6: Профилирование и Тонкая Настройка

### Инструменты:
```bash
# CPU профиль
go test -cpuprofile=cpu.pprof -bench=. ./aggregator/...

# Memory профиль  
go test -memprofile=mem.pprof -bench=. ./aggregator/...

# Block/contention профиль
go test -blockprofile=block.pprof -race ./aggregator/...
```

### Метрики успеха:
| Метрика | Текущее | Цель | Статус |
|---------|---------|------|--------|
| Throughput | ~331K records/sec | 1B records/sec | ❌ ~3000x разрыв |
| Время (100M записей) | ~5 мин | 0.1 сек | ❌ ~30,000x разрыв |

---

## Архитектурная Диаграмма (Плановая)

```
┌─────────────────────────────────────────────────────────────┐
│                      Aggregator                              │
│                                                             │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                    Scatter Phase                         ││
│  │                                                         ││
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐││
│  │  │ Get Pre- │  │ Get Pre- │  │ Get Pre- │  │ Get Pre- │││
│  │  │ Sorted   │  │ Sorted   │  │ Sorted   │  │ Sorted   │││
│  │  │ IDs      │  │ IDs      │  │ IDs      │  │ IDs      │││
│  │  │ Shard #0 │  │ Shard #1 │  │ Shard #N-1│Shard #N   │││
│  │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘││
│  │       │             │             │             │      ││
│  │       └─────────────┴─────────────┴─────────────┘      ││
│  │                          ▼                              ││
│  │              ┌───────────────────────┐                 ││
│  │              │   Filter by Candidate │ ← O(N) pass     ││
│  │              │         Map           │   через sorted   ││
│  │              │    (intersection)     │      IDs        ││
│  │              └───────────┬───────────┘                 ││
│  │                          ▼                             ││
│  │              ┌───────────────────────┐                ││
│  │              │   Apply Pagination    │ ← O(1) slice   ││
│  │              │   offset:limit        │                ││
│  │              └───────────┬───────────┘                ││
│  │                          ▼                             ││
│  │              ┌───────────────────────┐                ││
│  │              │    MultiGet(pageIDs)  │ ← Batch read   ││
│  │              │     (lock-free!)      │                ││
│  │              └───────────────────────┘                ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         ▼                    ▼                     ▼
    ┌─────────┐          ┌─────────┐           ┌─────────┐
    │Shard #0 │          │Shard #1 │           │ Shard #N│
    │  mmap   │          │  mmap   │           │  mmap   │
    │Indexed  │          │Indexed  │           │Indexed  │
    │         │          │         │           │         │
    │ sort:id │          │ sort:id │           │ sort:id │
    │ idx:cat │          │ idx:cat │           │ idx:cat │
    └─────────┘          └─────────┘           └─────────┘
```

---

## Ключевые отличия от текущей реализации:

| Текущее | Референс (makodb) |
|---------|-------------------|
| Сортировка в памяти O(N log N) | Pre-sorted indexes + фильтрация O(N) |
| Сбор всех Record в память | Работа с ID, fetch только page |
| Копирование данных | Zero-copy через unsafe.Slice |
| Lock-free только на уровне шардов | Полностью lock-free reads |

---

## Следующие Шаги:

1. **Реализовать pre-built indexes** для makodb (сортировка при импорте)
2. **Изменить Aggregator** на использование sorted IDs вместо gather-all-then-sort
3. **Добавить unsafe.Slice сериализацию** для zero-copy передачи ID
4. **Профилирование** для выявления оставшихся узких мест

---

## Текущая Производительность vs Цель

| Метрика | Текущее | Цель | Разрыв |
|---------|---------|------|--------|
| Throughput | ~331K records/sec | 1B records/sec | **~3000x** ❌ |
| Время (100M записей) | ~5 мин | 0.1 сек | **~30,000x** ❌ |

---

Дата создания: 2025
Статус: Планирование (Этап 0 завершён)
