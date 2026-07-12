package aggregator

import (
	"bytes"
	"context"
)

// ============================================================================
// QueryOperator - операторы для комбинирования терминов поиска
// ============================================================================

type QueryOperator int

const (
	defaultLimit = 100   // Дефолтный лимит результатов
	maxLimit     = 10000 // Максимальный лимит

	QueryOpAND QueryOperator = iota // Все термины должны присутствовать (по умолчанию)
	QueryOpOR                       // Достаточно одного термина
)

// ============================================================================
// Основные типы данных - выровнены под 64-байтовые кэш-линии
// ============================================================================

// Record - запись из базы данных (zero-copy projection на mmap)
// Размер: 64 байта = ровно одна кэш-линия!
// Layout (cache-line aligned):
//   [0..3]    ID int32           (4 bytes)
//   [4..7]    _ padding          (4 bytes - выравнивание Key под 8b)
//   [8..23]   Key header []byte  (16 bytes - pointer, len, cap)
//   [24..39]  Value header []byte (16 bytes)
//   [40..43]  Score float32      (4 bytes)
//   [44..51]  ShardID int64      (8 bytes) - tie-breaker для детерминизма
//   [52..59]  Timestamp int64    (8 bytes) - временная метка
//   [60..63]  _ padding          (4 bytes - выравнивание до 64b)
// Итого: 64 байта = ровно одна кэш-линия!

type Record struct {
	ID        int32   // ID записи (0-3)
	_         [4]byte // Padding для выравнивания Key под 8b (4-7)
	Key       []byte  // Ключ (projection, не owned) (8-23)
	Value     []byte  // Значение (projection, не owned) (24-39)
	Score     float32 // Релевантность (40-43)
	ShardID   int64   // ID шарда (tie-breaker для детерминизма сортировки) (44-51)
	Timestamp int64   // Временная метка записи (52-59)
	_         [4]byte // Padding до 64 байт для cache-line alignment (60-63)
}

// Примечание: Поле Date удалено для строгого выравнивания под 64 байта.
// Дата хранится как часть Value (JSON) или в отдельной структуре при необходимости.

// SearchOptions - опции поиска
type SearchOptions struct {
	Terms       []string // Термины для поиска
	MinScore    float32  // Минимальный score
	Limit       int      // Лимит результатов
	LastKey     []byte   // Keyset pagination cursor (ключ последней записи)
	LastShardID int64    // Shard ID последней записи (для детерминизма)
	Operator    QueryOperator
}

// SearchResult - результат поиска агрегатора
type SearchResult struct {
	Records     []Record         // Найденные записи
	HasMore     bool             // Есть ли ещё результаты
	NextKey     []byte           // Cursor для следующей страницы (keyset pagination)
	Total       uint64           // Общее количество совпадений
	ShardStats  map[int64]uint64 // Статистика по шардам
	LastShardID int64            // Shard ID последней записи
}

// Cursor - курсор для пагинации
type Cursor struct {
	Key     []byte
	ShardID int64
}

// ============================================================================
// Кэш-оптимизированные типы ответов от шардов
// ============================================================================

// ShardIDResponse - ответ только с IDs для Этапа 2 (zero-copy!)
// Размер хедера: 32 байта = пол-кэш-линии!
// Layout:
//   [0..7]    shardID int64      (8 bytes)
//   [8..15]   total uint64       (8 bytes)
//   [16..31]  ids header []int32 (16 bytes - pointer, len, cap)
// Итого: 32 байта хедер + данные в отдельной аллокации

type ShardIDResponse struct {
	ShardID int64   // ID шарда
	Total   uint64  // Общее количество записей на шарде
	IDs     []int32 // IDs (header = 16 bytes, данные отдельно)
}

// ShardRecordResponse - ответ с полными Record (для обратной совместимости)
// Размер хедера: 32 байта = пол-кэш-линии!
// Layout:
//   [0..7]    shardID int64      (8 bytes)
//   [8..15]   total uint64       (8 bytes)
//   [16..31]  records header []Record (16 bytes)

type ShardRecordResponse struct {
	ShardID int64    // ID шарда
	Total   uint64   // Общее количество записей на шарде
	Records []Record // Полные записи
}

// Heap Types - типы для heap операций (отдельно от MinHeap)
// ============================================================================

// idHeapElement - элемент для idHeap (K-Way Merge с стандартной библиотекой heap)
type idHeapElement struct {
	ID      int32   // ID записи
	ShardID int64   // ID шарда
	Key     []byte  // Ключ (для сравнения)
	Score   float32 // Score (первичный ключ сортировки)
	Idx     int     // Индекс в слайсе shard IDs
}

// idHeap - слайс элементов для heap операций из стандартной библиотеки
// Реализует интерфейс container/heap.Interface
type idHeap []idHeapElement

// ============================================================================
// Heap Interface Implementation for idHeap (container/heap)
// ============================================================================

// Len возвращает количество элементов в куче
func (h idHeap) Len() int {
	return len(h)
}

// Less сравнивает два элемента: сначала по Score (убывание), затем по Key (возрастание)
func (h idHeap) Less(i, j int) bool {
	a := &h[i]
	b := &h[j]

	// Сначала по Score (убывание - больше score = меньше в куче для min-heap)
	if a.Score != b.Score {
		return a.Score > b.Score // Инвертируем для MIN-кучи!
	}

	// Затем по Key (возрастание)
	return bytes.Compare(a.Key, b.Key) < 0
}

// Swap меняет местами два элемента в куче
func (h idHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Push добавляет элемент в конец слайса (вызывается container/heap)
func (h *idHeap) Push(x interface{}) {
	elem := x.(idHeapElement)
	*h = append(*h, elem)
}

// Pop удаляет последний элемент и возвращает его (вызывается container/heap)
func (h *idHeap) Pop() interface{} {
	old := *h
	n := len(old)
	elem := old[n-1]
	*h = old[0 : n-1]
	return elem
}

// ============================================================================
// Query Types - типы запросов и ответов
// ============================================================================

// ============================================================================
// Интерфейс ShardClient - клиент для работы с шардами
// ============================================================================

type ShardClient interface {
	// SearchID - быстрый поиск, возвращает только IDs (zero-copy!)
	SearchID(ctx context.Context, query *ShardQuery) (*ShardIDResponse, error)

	// SearchRecord - полный поиск с Record (для обратной совместимости)
	SearchRecord(ctx context.Context, query *ShardQuery) (*ShardRecordResponse, error)

	// GetSortedIDs - получить pre-sorted IDs для указанного поля сортировки
	// sortBy: "id", "key", "date", "score" и т.д.
	// limit: 0 = все записи, >0 = ограничить результат
	GetSortedIDs(ctx context.Context, sortBy string, limit int) ([]int32, error)

	// GetSortedIDsBytes - получить pre-sorted IDs как []byte (zero-copy через unsafe.Slice!)
	// Используется для передачи sorted IDs между шардами без аллокаций!
	GetSortedIDsBytes(ctx context.Context, sortBy string, limit int) ([]byte, error)

	// MultiGet - batch read нескольких записей по ID (lock-free!)
	MultiGet(ctx context.Context, ids []int32) (*ShardRecordResponse, error)

	Close() error
}

// ShardQuery - запрос к шарду
type ShardQuery struct {
	ShardID     int64    // ID шарда (для отладки и логирования)
	Terms       []string // Термины для поиска
	MinScore    float32  // Минимальный score
	Limit       int      // Лимит результатов
	LastKey     []byte   // Keyset pagination cursor
	LastShardID int64    // Shard ID последней записи
	SortBy      string   // Поле для сортировки (например, "date", "score")
}

// ============================================================================
// Вспомогательные структуры для агрегации
// ============================================================================

// ShardResult - результат от одного шарда (для внутреннего использования)
type ShardResult struct {
	ShardID int64    // ID шарда
	Records []Record // Полные записи (Stage 1)
	IDs     []int32  // Только IDs (Stage 2 - оптимизация)
	Error   error    // Ошибка при запросе к шарду
	HasMore bool     // Есть ли ещё данные на шарде
	LastKey []byte   // Последний ключ для пагинации
}

// RecordSlice - пул-оптимизированный слайс для Records
type RecordSlice struct {
	Records []Record
}

func (rs *RecordSlice) Len() int { return len(rs.Records) }
func (rs *RecordSlice) Less(i, j int) bool {
	if rs.Records[i].Score != rs.Records[j].Score {
		return rs.Records[i].Score > rs.Records[j].Score
	}
	return compareKeys(rs.Records[i].Key, rs.Records[j].Key) < 0
}
func (rs *RecordSlice) Swap(i, j int) {
	rs.Records[i], rs.Records[j] = rs.Records[j], rs.Records[i]
}
func (rs *RecordSlice) Append(rec Record) { rs.Records = append(rs.Records, rec) }
func (rs *RecordSlice) Slice() []Record   { return rs.Records }
func (rs *RecordSlice) SetRecords(records []Record) {
	rs.Records = records[:0]
}

// BitmapContext - контекст для bitmap операций
type BitmapContext struct {
	Mask uint64
}

// SearchQuery - структура запроса поиска (для пула)
type SearchQuery struct {
	Terms     []string
	MinScore  float32
	Limit     int
	Offset    int
	LastKey   []byte // для keyset pagination
	ShardMask uint64 // маска шардов для опроса
}

// ============================================================================
// Утилиты сравнения (zero-copy)
// ============================================================================

// compareKeys сравнивает два ключа byte-by-byte
// Возвращает: -1 если a < b, 0 если a == b, 1 если a > b
func compareKeys(a, b []byte) int {
	return bytes.Compare(a, b)
}

// ============================================================================
// ShardRegistry - реестр клиентов шардов
// ============================================================================

type ShardRegistry struct {
	clients map[int64]ShardClient
}

func NewShardRegistry() *ShardRegistry {
	return &ShardRegistry{
		clients: make(map[int64]ShardClient),
	}
}

func (r *ShardRegistry) Register(shardID int64, client ShardClient) {
	r.clients[shardID] = client
}

func (r *ShardRegistry) Get(shardID int64) (ShardClient, bool) {
	client, ok := r.clients[shardID]
	return client, ok
}

func (r *ShardRegistry) GetAll() map[int64]ShardClient {
	result := make(map[int64]ShardClient, len(r.clients))
	for k, v := range r.clients {
		result[k] = v
	}
	return result
}

func (r *ShardRegistry) Count() int {
	return len(r.clients)
}

// ============================================================================
// ShardSelector - селектор шардов для опроса
// ============================================================================

type ShardSelector struct {
	registry *ShardRegistry
}

func NewShardSelector(registry *ShardRegistry) *ShardSelector {
	return &ShardSelector{registry: registry}
}

// ============================================================================
// Aggregator - агрегатор запросов к шардированной базе данных
// ============================================================================

type Aggregator struct {
	registry    *ShardRegistry
	selector    *ShardSelector
	filter      *ShardFilter
	bitmapCache *ShardBitmapCache
}

func NewAggregator(registry *ShardRegistry) *Aggregator {
	bitmapCache := NewShardBitmapCache()
	filter := NewShardFilter(bitmapCache)
	selector := NewShardSelector(registry)
	return &Aggregator{
		registry:    registry,
		selector:    selector,
		filter:      filter,
		bitmapCache: bitmapCache,
	}
}
