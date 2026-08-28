# Бенчмарки и профилирование makoshop

## Результаты (200 concurrent, 30s)

| Endpoint | Req/s | Avg | P95 | P99 | Max |
|----------|-------|-----|-----|-----|-----|
| GET /shop | 897 | 60ms | 161ms | 231ms | 467ms |
| **GET /shop/{category}** | **604** | **161ms** | **444ms** | **722ms** | **1.8s** |
| GET /shop/{slug} | 749 | 42ms | 144ms | 205ms | 383ms |
| GET /products | 236 | 32ms | 104ms | 168ms | 327ms |
| GET /products/turbo | 205 | 15ms | 79ms | 126ms | 226ms |
| GET /categories/tree | 298 | 21ms | 88ms | 148ms | 286ms |

**Total: ~2989 req/s, 0 errors**

## 🔴 Критические узкие места (по CPU профилю)

### 1. JSON десериализация — 43% CPU

```
encoding/json.Unmarshal            381s  (61.57%)
├── encoding/json.(*decodeState).value    270s  (43.62%)
├── encoding/json.(*decodeState).object   235s  (37.88%)
├── encoding/json.checkValid             104s  (16.85%)
└── encoding/json.(*decodeState).literalStore 122s (19.76%)
```

**Где:**
- `GetCodesForCategoryTree` → `GetCodesForCategory` → `GetCategories` → `ListAll` → JSON parse
- `findCategoryByPath` → `ListAll` → JSON parse  
- `loadProductInfos` → `ProductRepo.Get` → JSON parse
- `CategoryRepo.Get` → JSON parse

**Проблема:** Каждый запрос делает множество DB.Get() с полным JSON parse документов.

### 2. loadProductInfos — 32% CPU

```
loadProductInfos         204s  (32.89%)
└── ProductRepo.Get      203s  (32.71%)
    └── UnmarshalProduct 187s  (30.20%)
```

**Где:** `ListWithTurbo` → для КАЖДОГО SCUPage в результате загружает все связанные продукты.

**Проблема:** N+1 запрос. Если на странице 50 SCUPage по 5-10 продуктов = 250-500 DB.Get() на запрос.

### 3. findCategoryByPath — 12% CPU

```
findCategoryByPath           74s  (11.96%)
└── CategoryRepo.ListAll    104s  (16.81%)
```

**Где:** `HandleSCUPageByPath` → для КАЖДОГО запроса с категорией загружает ВСЕ категории.

**Проблема:** Нет кэша slug→id. Каждый запрос делает полный ListAll + линейный поиск.

### 4. GetCodesForCategoryTree — 20% CPU

```
GetCodesForCategoryTree    128s  (20.63%)
├── GetCodesForCategory    125s  (20.18%)
│   └── GetCategories       75s  (12.02%)
└── Get                    128s  (20.67%)
```

**Где:** `handleSCUPageCatalog` → для КАЖДОГО запроса каталога загружает атрибуты.

**Проблема:** Дублирующие чтения, нет кэша.

### 5. getCategoryWithDescendants — скрытая проблема

```go
// eanpage_search.go — вызывается для КАЖДОГО запроса с категорией
func (s *SCUPageSearch) getCategoryWithDescendants(catID int64) ([]int64, error) {
    tree, err := s.categoryRepo.GetTree()  // ← полное дерево на каждый запрос!
```

## 🟡 Средние проблемы

### 6. getCategoryAncestors — N+1

```go
// eanpage_search.go — для каждого SCUPage ищет всех родителей
func (s *SCUPageSearch) getCategoryAncestors(catID int64) ([]int64, error) {
    for current != 0 {
        cat, err := s.categoryRepo.Get(current)  // ← DB.Get на каждый уровень
```

### 7. Память

```
inuse_space: 27MB
├── buildTurboSortPosCache  14MB (50%)
├── mallocgc                 7MB (24%)
├── bufio.NewReader          3MB (9%)
```

TurboSortPosCache потребляет много памяти (кэш позиций в сортировочных индексах).

## 📊 Итоговая картина CPU

```
net/http.(*conn).serve              573s  (92.43%)
└── HandleSCUPageByPath            541s  (87.23%)
    ├── handleSCUPageCatalog       460s  (74.20%)
    │   ├── ListWithTurbo          312s  (50.39%)
    │   │   ├── getCategoryWithDescendants  (GetTree на каждый запрос)
    │   │   ├── loadProductInfos     204s  (32.89%) ← ГЛАВНЫЙ HOTSPOT
    │   │   └── JSON deserialize     120s
    │   ├── GetCodesForCategoryTree 128s  (20.63%) ← ГЛАВНЫЙ HOTSPOT
    │   └── findCategoryByPath       74s  (11.96%) ← ГЛАВНЫЙ HOTSPOT
    └── writeSCUPageResponse        80s
```

## 💡 Рекомендации по оптимизации (по приоритету)

### Priority 1: Кэш slug→id для категорий (быстрый win)

**Где:** `internal/api/landing_handlers.go` — `findCategoryByPath`

**Что:** Добавить кэш slug→category_id при запуске (из ListAll один раз).

**Ожидаемый эффект:** -12% CPU, -74ms на /shop/{category}

```go
// В Handlers добавить:
categorySlugCache map[string]int64

// При инициализации:
cats, _ := h.categoryRepo.ListAll()
h.categorySlugCache = make(map[string]int64)
for _, c := range cats {
    h.categorySlugCache[c.Slug] = c.ID
}
```

### Priority 2: Кэш дерева категорий (быстрый win)

**Где:** `internal/db/eanpage_search.go` — `getCategoryWithDescendants`

**Что:** Кэшировать дерево категорий с TTL (1 мин).

**Ожидаемый эффект:** -15% CPU, -100ms на /shop/{category}

### Priority 3: Batch load продуктов вместо N+1 (большой win)

**Где:** `internal/db/eanpage_search.go` — `loadProductInfos`

**Что:** 
1. Собрать все productIDs со всей страницы
2. Batch Get (один вызов)
3. Вернуть мапу id→product

**Ожидаемый эффект:** -30% CPU, -150ms на /shop/{category}

### Priority 4: Кэш GetCodesForCategoryTree

**Где:** `internal/db/attrdef_repo.go`

**Что:** Кэшировать результат по catID (атрибуты редко меняются).

**Ожидаемый эффект:** -20% CPU на /shop/{category}

### Priority 5: Оптимизация JSON десериализации

**Варианты:**
- Использовать `ffjson` или `easyjson` для моделей
- Или кэшировать разпаршенные документы (SCUPage, Product) с TTL

**Ожидаемый эффект:** -15-20% CPU

### Priority 6: Кэш getCategoryAncestors

**Где:** `internal/db/eanpage_search.go`

**Что:** Кэшировать путь от категории к корню.

**Ожидаемый эффект:** -5% CPU

## 🛠️ Как запустить бенчмарки

### 1. Нагрузочный тест с профилированием сервера:

```bash
cd loadtest
go run bench_server_profile.go -c 200 -d 30s
```

Профили сохраняются в `loadtest/pprof_server/`.

### 2. Анализ профилей:

```bash
# CPU профиль (где тратится время)
go tool pprof -top -nodecount=30 loadtest/pprof_server/server_cpu.prof

# CPU профиль по кумулятивному времени
go tool pprof -top -cum -nodecount=30 loadtest/pprof_server/server_cpu.prof

# Heap профиль (память)
go tool pprof -top loadtest/pprof_server/server_heap.prof

# Goroutines
go tool pprof -top loadtest/pprof_server/server_goroutine.prof
```

### 3. Unit-бенчмарки (в коде):

```bash
cd internal/api
go test -bench=. -benchmem -count=3
```

### 4. Простой нагрузочный тест:

```bash
cd loadtest
go run bench.go -c 100 -d 30s
```

## 📈 Ожидаемые результаты после оптимизаций

| Endpoint | До | После (Priority 1-4) |
|----------|-----|---------------------|
| GET /shop/{category} | 161ms avg, 444ms p95 | ~50ms avg, ~120ms p95 |
| GET /shop | 60ms avg | ~40ms avg |
| Total req/s | ~2989 | ~4500+ |
