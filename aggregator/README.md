"# Aggregator для makodb

Агрегатор запросов к шардированной базе данных с реализацией паттерна scatter-gather, keyset pagination и оптимизацией через битовые маски.

## Архитектура

```
┌─────────────────────────────────────────────────────────────┐
│                      Aggregator                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │ ShardRegistry│  │ShardSelector│  │ ShardFilter      │   │
│  │              │  │             │  │ (Bitmap Cache)   │   │
│  └──────────────┘  └──────────────┘  └──────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         ▼                    ▼                     ▼
    ┌─────────┐          ┌─────────┐           ┌─────────┐
    │ Shard 0 │          │ Shard N │           │ Shard M │
    │100k rec │          │100k rec │           │100k rec │
    └─────────┘          └─────────┘           └─────────┘
```

## Компоненты

### 1. Aggregator (`aggregator.go`)

Основной компонент, реализующий scatter-gather паттерн:

- **SCATTER**: Параллельный опрос всех шардов через goroutines + `sync.WaitGroup`
- **GATHER**: Сбор результатов через канал, сортировка и агрегация

```go
// SCATTER: Распределяем запросы по шардам параллельно
for _, shardID := range filteredShards {
    wg.Add(1)
    go func(id int64) {
        defer wg.Done()
        a.scatterToShard(ctx, id, opts, resultsCh)
    }(shardID)
}

// GATHER: Собираем результаты со всех шардов
return a.gatherResults(ctx, resultsCh, opts.Limit, opts.LastKey)
```

### 2. ShardClient (`shard_client.go`)

Интерфейс для работы с отдельными шардами:

- `ShardClient` - интерфейс клиента шарда
- `MockShardClient` - мок клиент для тестирования и бенчмарков
- `ShardRegistry` - реестр всех шардов
- `ShardSelector` - селектор шардов на основе битовых масок

**Оптимизации:**
- Бинарный поиск (O(log N)) для определения startIdx при keyset pagination
- Возврат копий данных без использования пула в MockShardClient

### 3. ShardBitmapCache (`bitmap_cache.go`)

Кэш битовых масок для оптимизации запросов:

```go
// Термин "test" есть на шардах 0, 2, 4 -> маска = 0b10101 = 21
cache.UpdateTermShardMask("test", 0, true)
cache.UpdateTermShardMask("test", 2, true)  
cache.UpdateTermShardMask("test", 4, true)

// Вычисление маски для запроса с несколькими терминами (AND)
mask := cache.ComputeQueryMask([]string{"test", "data"}, OpAND)
```

### 4. Memory Pools (`pool.go`)

sync.Pool для всех промежуточных аллокаций:

- `ResultPool` - пул результатов от шардов
- `BitmapPool` - пул контекстов битовых карт
- `QueryPool` - пул запросов

## API

### SearchOptions

```go
type SearchOptions struct {
    Terms    []string  // Термины для поиска
    MinScore float32   // Минимальный скор (резерв)
    Limit    int       // Лимит результатов
    LastKey  []byte    // Ключ для keyset pagination
    Operator QueryOperator // Оператор (AND/OR)
}
```

### SearchResult

```go
type SearchResult struct {
    Records    []Record        // Результаты поиска
    HasMore    bool            // Есть ли еще результаты
    NextKey    []byte          // Ключ для следующей страницы
    Total      uint64          // Общее количество записей
    ShardStats map[int64]uint64 // Статистика по шардам
}
```

### Использование

```go
// Создание агрегатора
registry := aggregator.NewShardRegistry()
// ... регистрация шардов ...
agg := aggregator.NewAggregator(registry)

// Базовый поиск
result, err := agg.Search(ctx, aggregator.SearchOptions{
    Terms: []string{"test", "data"},
    Limit: 100,
})

// Keyset pagination - вторая страница
result2, err := agg.Search(ctx, aggregator.SearchOptions{
    Terms:   []string{"test", "data"},
    Limit:   100,
    LastKey: result.NextKey, // Используем NextKey из первого запроса
})

// Поиск с курсором
cursor := &aggregator.Cursor{
    Key:       []byte("some_key"),
    Timestamp: time.Now().Unix(),
}
result, err := agg.SearchWithCursor(ctx, []string{"test"}, cursor, 100)
```

## Keyset Pagination

Реализована корректная keyset pagination с гарантией отсутствия дубликатов и пропусков записей:

1. **Фильтрация на уровне шардов**: Каждый шард возвращает только записи после `LastKey`
2. **Глобальная сортировка**: Все результаты сортируются по ключу перед применением лимита
3. **Корректный NextKey**: Берется из последней выданной записи (`allRecords[limit-1].Key`)

```go
// Пример использования pagination
ctx := context.Background()
var lastKey []byte

for page := 0; page < totalPages; page++ {
    opts := aggregator.SearchOptions{
        Terms:   terms,
        Limit:   pageSize,
        LastKey: lastKey,
    }
    
    result, _ := agg.Search(ctx, opts)
    
    // Обработка результатов...
    
    lastKey = result.NextKey
    if !result.HasMore {
        break
    }
}
```

## Производительность

Бенчмарки выполнены на AMD Ryzen 9 7950X3D с конфигурацией:
- **10 шардов** × **100,000 записей** = **1,000,000 записей всего**

### Результаты бенчмарков

| Бенчмарк | Время/оп | Аллокации/оп | Память/оп |
|----------|----------|--------------|-----------|
| `BenchmarkAggregator_Search` | ~420 µs | 187 | ~1.2 MB |
| `BenchmarkAggregator_Search_KeysetPagination` | ~406 µs | 187 | ~1.2 MB |
| `BenchmarkAggregator_Search_Parallelism` | ~150 µs | 1,238 | ~1.3 MB |

### Масштабируемость по количеству шардов

| Шарды | Время/оп | Аллокации/оп |
|-------|----------|--------------|
| 1 | ~55 µs | 43 |
| 5 | ~200 µs | 105 |
| 10 | ~420 µs | 187 |
| 20 | ~885 µs | 341 |

### Параллельное выполнение

```
BenchmarkAggregator_Search_Parallelism-32    7603 ops    
    148347 ns/op    1279323 B/op    1238 allocs/op
```

При параллельном выполнении (через `b.RunParallel`) достигается ~3x ускорение благодаря эффективному использованию всех ядер CPU.

## Тестирование

### Запуск тестов

```bash
# Все тесты
go test -v ./aggregator/... -run '^Test' -count=1

# С проверкой на data races
go test -race ./aggregator/... -run '^Test' -count=1

# Бенчмарки
go test -bench=. ./aggregator/... -benchmem -count=3

# Конкретный тест
go test -v ./aggregator/... -run TestAggregator_KeysetPagination
```

### Покрытие тестами

- ✅ `TestAggregator_Search` - базовый поиск
- ✅ `TestAggregator_KeysetPagination` - keyset pagination
- ✅ `TestAggregator_ScatterGatherParallelism` - параллельное выполнение
- ✅ `TestAggregator_ContextCancellation` - отмена контекста
- ✅ `TestAggregator_EmptyShards` - пустые шарды
- ✅ `TestAggregator_PoolReuse` - повторное использование пулов
- ✅ `TestShardFilter_BitmapOptimization` - оптимизация битовыми масками
- ✅ `TestCompareKeys` - сравнение ключей
- ✅ `TestConcurrentSearch` - конкурентные запросы

## Структура файлов

```
aggregator/
├── aggregator.go       # Основной агрегатор с scatter-gather
├── shard_client.go     # Клиенты шардов и реестр
├── bitmap_cache.go     # Кэш битовых масок для оптимизации
├── pool.go             # sync.Pool для аллокаций
├── aggregator_test.go  # Тесты и бенчмарки
└── README.md           # Эта документация
```

## Особенности реализации

### 1. Отсутствие data races

- Все общие состояния защищены `sync.RWMutex`
- Каналы используются для безопасной передачи данных между горутинами
- MockShardClient возвращает копии данных, а не ссылки на пул

### 2. Бинарный поиск в MockShardClient

```go
// O(log N) вместо O(N)
startIdx = sort.Search(len(m.records), func(i int) bool {
    return compareKeys(m.records[i].Key, query.LastKey) > 0
})
```

### 3. Корректная фильтрация в gatherResults

```go
// Фильтрация по LastKey для корректной keyset pagination
for _, rec := range result.Records {
    if len(lastKey) == 0 || compareKeys(rec.Key, lastKey) > 0 {
        allRecords = append(allRecords, rec)
    }
}
```

### 4. NextKey из последней выданной записи

```go
if len(allRecords) > limit {
    finalRecords = allRecords[:limit]
    // NextKey берется из allRecords[limit-1], а не allRecords[limit]!
    nextKey = make([]byte, len(finalRecords[len(finalRecords)-1].Key))
    copy(nextKey, finalRecords[len(finalRecords)-1].Key)
    hasMore = true
}
```

## Лицензия

Часть проекта makoshop.

</contents>