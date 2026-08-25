# Makodb — mmap-based lock-free KV DB с turbo-индексами

## Назначение

Универсализированная база данных для высокопроизводительных read-heavy workload:

- **Lock-free reads**: `Get`, `ForEach`, `GetZeroAlloc` — без блокировок, только атомарные чтения mmap.
- **Single write lock per shard**: `Put`, `Delete`, `resize`, `Shrink` — через `RobustShmMutex` (shared memory mutex, safe on crash).
- **Turbo indexes**: отсортированные массивы uint64 (docID) для быстрого поиска, пересечений, объединений.
- **Sort indexes**: для пагинации и сортировки по любому полю (цена, дата, рейтинг).
- **Numeric sort indexes**: для range-запросов по числовым полям (цена, timestamp).

### Что быстро

- **Get/Put/Delete**: O(1) хеш-таблица в mmap, lock-free read, single writer lock.
- **Turbo intersection/union**: merge-style на отсортированных uint64, O(n+m), без map-аллокаций.
- **Raw turbo operations**: `TurboBinaryIntersectRaw`, `TurboBinaryUnionRaw` — работают с raw bytes, без `[]uint64` аллокаций.
- **Sort index pagination**: прямой срез по позиции, O(1).
- **Sort index + candidates**: merge-style intersection с position index, O(C+S).
- **Numeric sort range**: binary search + срез, O(log N).
- **Assembly Optimizations**: AMD64 SIMD/Assembly для intersection, union, diff и docID extraction (ускорение в 2-3 раза). Подробнее в [ASM Optimizations](docs/asm_optimizations.md).

### Нюансы

- **docID — это uint64**. База не знает, что это значит. Сопоставление docID ↔ ключ документа — задача приложения.
- **Turbo индексы — это просто отсортированные uint64**. Нет metadata, нет reverse lookup без внешнего маппинга.
- **DB не кэширует объекты между запросами**. Каждый запрос читает из mmap напрямую.
- **Нет goroutine/channel/sync.Mutex в hot path**. Параллелизм через шардирование.
- **mmap фиксированного размера**. Resize только через `Truncate` + обновление `MaxSize`, без Unmap/Remap.
- **Turbo индексы гарантированно отсортированы**. Вставка — binary search + in-place insertion.

---

## Базовая структура

### ShardedDB

Массив `*DB` шардов. Ключ хешируется → конкретный шард.

```go
db, err := makodb.OpenSharded("/path/to/db", 16, 6710886400, 1000)
// 16 шардов, 6.4 ГБ макс, 1000 buckets на шард
defer db.Close()
```

### DB (один шард)

- **Header**: magic, version, lock state, max size, free offset, num buckets.
- **Hash table buckets**: цепочки коллизий в mmap.
- **Data area**: ключи и значения, appends через `FreeOffset`.

---

## Базовые KV операции

### Put

```go
db.Put("user:123", []byte(`{"name":"Alice"}`))
```

Пишет или обновляет key-value. Lock на шард через `RobustShmMutex`.

### Get

```go
val, err := db.Get("user:123")
```

Lock-free read. Если ключ не найден — `ErrKeyNotFound`.

### GetZeroAlloc

```go
val, err := db.GetZeroAlloc("user:123")
```

Возвращает direct view в mmap. Не копировать данные, не держать долго.

### Delete

```go
err := db.Delete("user:123")
```

Логическое удаление (отметка в bucket). Физическая очистка — через `Shrink`.

### MultiGet

```go
vals, err := db.MultiGet([]string{"user:1", "user:2", "user:3"})
```

Batch read с группировкой по шардам.

### ForEach

```go
err := db.ForEach(func(key string, value []byte) error {
    fmt.Println(key, len(value))
    return nil
})
```

Итерация по всем активным записям (lock-free read).

---

## Turbo Indexes

Turbo index — это отсортированный массив uint64 docID, хранящийся под ключом `"turbo_idx:<token>"`.

**Layout**: `[count: uint64][docID1: uint64][docID2: uint64]...`

### Пример: индекс по тегу

```go
// Добавляем документ с ID=42 в индекс "tag:electronics"
added, _ := db.TurboPutIndex("tag:electronics", 42)

// Удаляем
removed, _ := db.TurboDeleteIndex("tag:electronics", 42)

// Проверяем наличие
exists, _ := db.TurboContainsIndex("tag:electronics", 42)

// Получаем все docID
tokens, _ := db.TurboGetIndexTokens("tag:electronics")

// Количество
count, _ := db.TurboCountIndex("tag:electronics")
```

### Batch операции

```go
// Добавить несколько docID
added, _ := db.TurboPutBatchIndex("tag:sale", []uint64{1, 2, 3, 4, 5})

// Удалить несколько
removed, _ := db.TurboDeleteBatchIndex("tag:sale", []uint64{1, 3})
```

### Intersection (AND)

```go
// Документы с обоими тегами
result, _ := db.TurboSearch([]string{"tag:electronics", "tag:sale"})
// или
result, _ := db.TurboIntersectAll([]string{"tag:electronics", "tag:sale"})
// или с условиями
result, _ := db.TurboIntersectIndexResults([]makodb.TurboIndexCondition{
    {Index: "tag:electronics"},
    {Index: "tag:sale"},
})
```

### Union (OR)

```go
// Документы с любым из тегов
result, _ := db.TurboUnionAll([]string{"tag:electronics", "tag:clothing"})

// Для больших индексов — merge-style union (быстрее!)
result, _ := db.TurboBulkUnionSorted([]string{"tag:electronics", "tag:clothing"})
```

### Diff (AND NOT)

```go
// Документы с tag:electronics, но без tag:sold
result, _ := db.TurboDiff("tag:electronics", []string{"tag:sold"})
```

### Raw turbo operations (без []uint64 аллокаций)

```go
// Intersection → raw turbo bitmap
raw, _ := db.TurboBulkIntersectRaw([]string{"tag:electronics", "tag:sale"})

// Union → raw turbo bitmap
raw, _ := db.TurboBulkUnionSortedRaw([]string{"tag:electronics", "tag:clothing"})

// AND/OR/ANDNOT по ключам
rawAnd, _ := db.TurboAndFromDB("tag:electronics", "tag:sale")
rawOr, _ := db.TurboOrFromDB("tag:electronics", "tag:clothing")
rawNot, _ := db.TurboAndNotFromDB("tag:electronics", "tag:sold")
```

### Raw turbo helpers (package-level)

```go
// Чтение count из raw turbo bitmap
count := makodb.TurboBinaryCount(raw)

// Проверка наличия docID
exists := makodb.TurboBinaryContains(raw, 42)

// Binary search: первый >= target
token, idx := makodb.TurboBinaryFindGE(raw, 100)

// Срез
slice := makodb.TurboBinarySlice(raw, 0, 10)

// Intersection raw bitmaps
result := makodb.TurboBinaryIntersectRaw([][]byte{raw1, raw2})

// Union raw bitmaps
result := makodb.TurboBinaryUnionRaw([][]byte{raw1, raw2})

// Diff raw bitmaps
result := makodb.TurboBinaryDiff(raw1, raw2)
```

---

## Sort Indexes

Sort index — для пагинации и сортировки. Два ключа на каждый sort:

1. **Main index** (`turbo_sort:<name>`): docID в порядке сортировки.
2. **Position index** (`turbo_sort_pos:<name>`): `(docID, position)` пары, отсортированы по docID.

**Layout main**: `[count][docID1][docID2]...` (порядок = порядок сортировки)
**Layout position**: `[count][docID1][pos1][docID2][pos2]...` (отсортировано по docID)

### Build

```go
// Предположим, у нас есть docID отсортированные по цене ascending
sortedByPrice := []uint64{5, 12, 3, 42, 1} // цена 5 < 12 < 3 < 42 < 1 (пример)

err := db.TurboPutSortIndex("price_asc", sortedByPrice)
```

### Пагинация без фильтров

```go
// Страница 0, 20 документов
docIDs, total, err := db.TurboSortIndexPaginateFromDB("price_asc", 0, 20)
```

### Пагинация с фильтрами (candidates)

```go
// Кандидаты из turbo intersection
candidates, _ := db.TurboSearch([]string{"tag:electronics", "tag:sale"})

// Получаем страницу, отсортированную по цене
params := makodb.TurboSortPageParams{
    Name:       "price_asc",
    Candidates: candidates,
    Page:       0,
    PageSize:   20,
    Desc:       false,
}
result, _ := db.TurboSortIndexPageFromDB(params)
// result.DocIDs — docID в порядке цены
// result.Total — всего совпадений
```

### Raw candidates (без []uint64 аллокации)

```go
// Кандидаты как raw turbo bitmap
rawCandidates, _ := db.TurboBulkIntersectRaw([]string{"tag:electronics", "tag:sale"})

// Пагинация напрямую
result, _ := db.TurboSortIndexPageRawFromDB("price_asc", rawCandidates, 0, 20, false)
```

### С документами

```go
// Получаем docID + raw JSON документов
params := makodb.TurboSortPageWithDocsParams{
    Name:       "price_asc",
    Candidates: candidates,
    Page:       0,
    PageSize:   20,
    Desc:       false,
    DocPrefix:  "scupage:", // ключи документов: "turbo_idx:scupage:123"
}
result, _ := db.TurboSortIndexPageWithDocsFromDB(params)
// result.DocIDs — []uint64
// result.Docs   — [][]byte (JSON)
// result.Total  — uint64
```

### Stats

```go
stats, _ := db.TurboSortIndexStats("price_asc")
// stats.Count, stats.SizeBytes
```

### Delete

```go
err := db.TurboDeleteSortIndex("price_asc")
```

---

## Numeric Sort Indexes

Numeric sort index — для range-запросов по числовым полям (цена, timestamp, рейтинг).

**Layout**: `[count][value1][docID1][value2][docID2]...` (отсортировано по value)

### Add/Update

```go
// Добавить документ с ценой 1500
added, _ := db.TurboPutNumSort("price", 1500, 42)

// Batch
pairs := []makodb.TurboNumSortPair{
    {Value: 1500, DocID: 1},
    {Value: 2000, DocID: 2},
    {Value: 1800, DocID: 3},
}
added, _ := db.TurboPutNumSortBatch("price", pairs)
```

### Range query

```go
// Документы с ценой от 1000 до 3000
params := makodb.TurboNumSortRangeParams{
    MinValue: 1000,
    MaxValue: 3000,
    Page:     0,
    PageSize: 20,
}
result, _ := db.TurboGetNumSortRange("price", params)
// result.Pairs — []TurboNumSortPair (value, docID)
// result.Total — всего совпадений
```

### Range + candidates

```go
// Кандидаты из turbo search
candidates, _ := db.TurboSearch([]string{"tag:electronics"})

// Цена 1000-3000 И tag:electronics
result, _ := db.TurboGetNumSortRangeIntersectCandidates("price", 1000, 3000, candidates, 0, 20)
```

### Raw range → raw bitmap

```go
// Все docID с ценой 1000-3000 как raw turbo bitmap
raw, _ := db.TurboGetNumSortRangeRaw("price", 1000, 3000)

// Range + raw candidates → raw bitmap
rawResult, _ := db.TurboGetNumSortRangeIntersectRaw("price", 1000, 3000, rawCandidates)
```

### Range + документы

```go
params := makodb.TurboGetNumSortRangeWithDocsParams{
    Name:     "price",
    MinValue: 1000,
    MaxValue: 3000,
    Page:     0,
    PageSize: 20,
    Desc:     false,
    DocPrefix: "scupage:",
}
result, _ := db.TurboGetNumSortRangeWithDocs(params)
// result.DocIDs, result.Docs, result.Total
```

### Delete

```go
removed, _ := db.TurboDeleteNumSortByDocID("price", 42)
removed, _ := db.TurboDeleteNumSortByDocIDs("price", []uint64{1, 2, 3})
```

### Stats

```go
stats, _ := db.TurboNumSortStats("price")
```

---

## Radix Sort

```go
// Сортировка uint64 (LSD radix sort, base-256)
makodb.RadixSortUint64([]uint64{5, 2, 8, 1, 9})
```

Используется внутри turbo для сортировки индексов.

---

## Top-N по пересечению

```go
// Найти топ-5 индексов из кандидатов, которые больше всего пересекаются с query
results, _ := db.TurboTopNByIntersection("query_token", []string{"cand1", "cand2", "cand3"}, 5)
// results — []TurboIndexIntersectionResult {Key, Count}
```

---

## Utility операции

```go
// Clear index
db.TurboClearIndex("tag:old")

// Compact (удалить дубликаты)
removed, _ := db.TurboCompactIndex("tag:dirty")

// Merge indexes
db.TurboMergeIndexes("tag:all", []string{"tag:a", "tag:b", "tag:c"})

// Copy index
db.TurboCopyIndex("tag:src", "tag:dst")

// Split by predicate
db.TurboSplitIndex("tag:all", "tag:even", "tag:odd", func(docID uint64) bool {
    return docID%2 == 0
})

// Raw read/write
raw, _ := db.TurboRawRead("tag:electronics")
db.TurboRawWrite("tag:electronics", raw)

// Stats
stats, _ := db.TurboIndexStats("tag:electronics")
```

---

## Пример: поиск товаров (makoshop)

```go
// Запрос: категория=1, текст="телефон", цена 5000-50000, сортировка по цене desc

// 1. Кандидаты по категории (per-category index)
catCandidates, _ := db.TurboBulkUnionSortedRaw([]string{"cat:1"})

// 2. Кандидаты по тексту (text index)
textCandidates, _ := db.TurboGet("turbo_idx:text:телефон")

// 3. Intersection: категория И текст
candidatesRaw := makodb.TurboBinaryIntersectRaw([][]byte{catCandidates, textCandidates})

// 4. Фильтр по цене через numSort range intersect
priceFiltered, _ := db.TurboGetNumSortRangeIntersectRaw("price:1", 5000, 50000, candidatesRaw)

// 5. Пагинация с сортировкой по цене desc
result, _ := db.TurboSortIndexPageRawWithDocsFromDB(
    "sort:1:price_desc", // per-category sort index
    priceFiltered,
    0,                    // page
    60,                   // limit
    true,                 // desc
    "scupage:",           // doc prefix
)

// result.DocIDs, result.Docs, result.Total
```

---

## Performance notes

- **Turbo indexes sorted**: все операции intersection/union — merge-style O(n+m), не O(n*m).
- **Raw operations preferred**: `TurboBulkIntersectRaw`, `TurboBulkUnionSortedRaw` — без `[]uint64` аллокаций.
- **Sort index fast path**: без candidates — прямой срез O(1).
- **Sort index slow path**: с candidates — merge-style intersection с position index O(C+S).
- **NumSort range**: binary search O(log N) + срез O(K).
- **No hash table in hot path**: `buildTurboSortPosCache` убран, используется binary search / merge-style.
