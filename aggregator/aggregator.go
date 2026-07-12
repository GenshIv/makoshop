package aggregator

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
)

// Memory pool для ShardResult (sync.Pool)
var shardResultPool = sync.Pool{
	New: func() interface{} {
		return &ShardResult{
			Records: make([]Record, 0, 64),
			IDs:     make([]int32, 0, 64),
		}
	},
}

// GetShardResult получает ShardResult из пула
func GetShardResult() *ShardResult {
	result := shardResultPool.Get().(*ShardResult)
	// Очищаем предыдущие данные
	result.Records = result.Records[:0]
	result.IDs = result.IDs[:0]
	result.Error = nil
	result.HasMore = false
	result.LastKey = nil
	return result
}

// PutShardResult возвращает ShardResult в пул для повторного использования
func PutShardResult(result *ShardResult) {
	if result == nil {
		return
	}
	// Очистка происходит только при получении из пула - стандартный паттерн Go
	shardResultPool.Put(result)
}

// Search выполняет агрегированный поиск по всем шардам с использованием scatter-gather паттерна
func (a *Aggregator) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 100 // default limit
	}

	// Получаем список всех шардов
	allShards := a.selector.SelectAll()
	if len(allShards) == 0 {
		return &SearchResult{
			Records:    []Record{},
			HasMore:    false,
			Total:      0,
			ShardStats: make(map[int64]uint64),
		}, nil
	}

	// Фильтруем шарды на основе битовых масок (оптимизация)
	filteredShards := a.filter.FilterShards(opts.Terms, allShards)
	if len(filteredShards) == 0 {
		return &SearchResult{
			Records:    []Record{},
			HasMore:    false,
			Total:      0,
			ShardStats: make(map[int64]uint64),
		}, nil
	}

	// Создаем контекст с таймаутом если не установлен
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel func()
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	// ALLOCATE: Pre-allocate fixed-size buffers for all shards (LOCK-FREE!)
	buffers := make([]*ShardQueryBuffer, len(filteredShards))
	for i := range buffers {
		buffers[i] = GetShardQueryBuffer()
	}
	defer func() {
		// Return all buffers to pool
		for _, buf := range buffers {
			PutShardQueryBuffer(buf)
		}
	}()

	// SCATTER: Распределяем запросы по шардам параллельно (LOCK-FREE!)
	a.scatterToShardsWithBuffers(ctx, filteredShards, opts, buffers)

	// GATHER: Собираем результаты из буферов (LOCK-FREE!)
	return a.gatherResultsFromBuffers(ctx, filteredShards, buffers, opts.Limit, opts.LastKey, opts.LastShardID)
}

// scatterToShard отправляет запрос на конкретный шард (Stage 1 - полные Records)
func (a *Aggregator) scatterToShard(ctx context.Context, shardID int64, opts SearchOptions, resultsCh chan<- *ShardResult) {
	client, ok := a.registry.Get(shardID)
	if !ok {
		result := GetShardResult()
		result.ShardID = shardID
		result.Error = errors.New("shard not found")
		resultsCh <- result
		return
	}

	// Получаем ShardQuery из пула (оптимизация)
	shardQuery := GetShardQuery()
	defer PutShardQuery(shardQuery) // Возвращаем в пул после использования

	// Заполняем запрос для шарда с увеличенным лимитом (для сортировки)
	shardQuery.Terms = opts.Terms
	shardQuery.MinScore = opts.MinScore
	shardQuery.Limit = opts.Limit * 2 // Запрашиваем больше для корректной сортировки
	shardQuery.LastKey = opts.LastKey
	shardQuery.LastShardID = opts.LastShardID // Передаем курсор шарда

	// Stage 1: используем SearchRecord для получения полных Records
	response, err := client.SearchRecord(ctx, shardQuery)
	result := GetShardResult()
	result.ShardID = shardID

	if err != nil {
		result.Error = err
		resultsCh <- result
		return
	}

	// Копируем записи в результат и устанавливаем ShardID для детерминизма сортировки
	for _, rec := range response.Records {
		rec.ShardID = shardID // Устанавливаем ShardID как tie-breaker
		result.Records = append(result.Records, rec)
	}
	// result.HasMore и LastKey пока не используются в Stage 1

	resultsCh <- result
}

// ============================================================================
// Stage 2: scatterToShardIDs - отправляет запрос только на IDs (оптимизация)
// ============================================================================

// scatterToShardIDs отправляет запрос на конкретный шард и получает только IDs
func (a *Aggregator) scatterToShardIDs(ctx context.Context, shardID int64, opts SearchOptions, resultsCh chan<- *ShardResult) {
	client, ok := a.registry.Get(shardID)
	if !ok {
		result := GetShardResult()
		result.ShardID = shardID
		result.Error = errors.New("shard not found")
		resultsCh <- result
		return
	}

	// Получаем ShardQuery из пула (оптимизация)
	shardQuery := GetShardQuery()
	defer PutShardQuery(shardQuery) // Возвращаем в пул после использования

	// Заполняем запрос для шарда с увеличенным лимитом (для сортировки)
	shardQuery.Terms = opts.Terms
	shardQuery.MinScore = opts.MinScore
	shardQuery.Limit = opts.Limit * 2 // Запрашиваем больше для корректной сортировки
	shardQuery.LastKey = opts.LastKey
	shardQuery.LastShardID = opts.LastShardID // Передаем курсор шарда

	// Stage 2: используем SearchID для получения только IDs (оптимизация!)
	response, err := client.SearchID(ctx, shardQuery)
	result := GetShardResult()
	result.ShardID = shardID

	if err != nil {
		result.Error = err
		resultsCh <- result
		return
	}

	// Копируем только IDs в результат (без Records!)
	for _, id := range response.IDs {
		result.IDs = append(result.IDs, id)
	}
	// result.HasMore и LastKey пока не используются в Stage 2

	resultsCh <- result
}

// ============================================================================
// OPTIMIZED: Scatter-Gather с Shared Memory Buffers (БЕЗ БЛОКИРУЮЩИХ КАНАЛОВ!)
// ============================================================================

// scatterToShardsWithBuffers отправляет запросы на все шарды и пишет результаты в буферы (БЕЗ КАНАЛОВ!)
func (a *Aggregator) scatterToShardsWithBuffers(ctx context.Context, shardIDs []int64, opts SearchOptions, buffers []*ShardQueryBuffer) {
	var wg sync.WaitGroup

	for i, shardID := range shardIDs {
		client, ok := a.registry.Get(shardID)
		if !ok {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				// Пишем ошибку в буфер (БЕЗ БЛОКИРОВОК!)
				buffers[idx].SetResult(0, fmt.Errorf("shard %d not found", shardID))
			}(i)
			continue
		}

		wg.Add(1)
		go func(idx int, id int64) {
			defer wg.Done()

			// Получаем ShardQuery из пула (оптимизация)
			shardQuery := GetShardQuery()
			defer PutShardQuery(shardQuery) // Возвращаем в пул после использования

			// Заполняем запрос для шарда с увеличенным лимитом (для сортировки)
			shardQuery.Terms = opts.Terms
			shardQuery.MinScore = opts.MinScore
			shardQuery.Limit = opts.Limit * 2 // Запрашиваем больше для корректной сортировки
			shardQuery.LastKey = opts.LastKey
			shardQuery.LastShardID = opts.LastShardID // Передаем курсор шарда

			// Выполняем запрос к шарду (SearchRecord возвращает полные Record)
			response, err := client.SearchRecord(ctx, shardQuery)

			// Пишем результат в буфер (БЕЗ БЛОКИРОВОК!)
			if err != nil {
				buffers[idx].SetResult(0, err)
			} else {
				// FIXED-SIZE COPY: копируем напрямую без append()!
				copyCount := len(response.Records)
				if copyCount > ShardBufferCapacity {
					copyCount = ShardBufferCapacity // Truncate to capacity - no expansion!
				}

				// Direct assignment into pre-allocated buffer (zero-allocation!)
				buffers[idx].Records = buffers[idx].Records[:copyCount]
				for j := 0; j < copyCount; j++ {
					rec := response.Records[j]
					rec.ShardID = id // Устанавливаем ShardID как tie-breaker
					buffers[idx].Records[j] = rec
				}

				buffers[idx].SetResult(copyCount, nil)
			}
		}(i, shardID)
	}

	// Ждём завершения всех запросов (только один раз!)
	wg.Wait()
}

// gatherResultsFromBuffers собирает результаты из буферов БЕЗ БЛОКИРУЮЩИХ КАНАЛОВ!
// ОПТИМИЗИРОВАНО: K-Way Merge через sorted IDs вместо gather-all-then-sort!
func (a *Aggregator) gatherResultsFromBuffers(ctx context.Context, shardIDs []int64, buffers []*ShardQueryBuffer, limit int, lastKey []byte, lastShardID int64) (*SearchResult, error) {
	var total uint64
	shardStats := make(map[int64]uint64)

	// 1. Собираем результаты из буферов (БЕЗ БЛОКИРОВОК!)
	type shardData struct {
		shardID int64
		records RecordSlice
	}

	shardResults := make([]*shardData, 0, len(buffers))

	for i, buf := range buffers {
		// Spin-wait без блокировки - просто ждём флаг готовности
		for !buf.Ready.Load() {
			runtime.Gosched() // Yield без блокировки
		}

		count, err := buf.GetResult()
		if err != nil {
			continue
		}

		// Records уже в буфере (не копируем!)
		records := buf.Records[:count]

		total += uint64(count)
		shardStats[shardIDs[i]] = uint64(count)

		// Создаем sorted slice для этого шарда - FIXED-SIZE COPY!
		slice := GetRecordSlice()
		for j := 0; j < count; j++ {
			rec := records[j]
			rec.ShardID = shardIDs[i] // Устанавливаем ShardID как tie-breaker
			slice.Append(rec)
		}
		sort.Sort(slice) // Сортируем внутри шарда (меньше данных чем глобально)

		shardResults = append(shardResults, &shardData{
			shardID: shardIDs[i],
			records: *slice,
		})
	}

	// 2. K-way merge через min-heap (ИСПОЛЬЗУЕМ ПУЛ!)
	finalRecords := GetFinalResult(limit)
	defer PutFinalResult(finalRecords) // Возвращаем в пул после использования

	if len(shardResults) == 1 {
		// Оптимизация: если один шард, просто берём из него
		slice := shardResults[0].records.Slice()
		for _, rec := range slice {
			// Применяем фильтрацию по lastKey и lastShardID (составной ключ)
			if len(lastKey) > 0 {
				cmp := compareKeys(rec.Key, lastKey)
				// Пропускаем если: key < lastKey ИЛИ (key == lastKey AND shardID <= lastShardID)
				if cmp < 0 || (cmp == 0 && rec.ShardID <= lastShardID) {
					continue
				}
			}
			finalRecords = append(finalRecords, rec)
			if len(finalRecords) >= limit {
				break
			}
		}
	} else if len(shardResults) > 1 {
		// K-way merge через heap (ОПТИМИЗИРОВАНО - работаем с индексами, не копиями!)
		type shardIterator struct {
			slice    []Record // Ссылка на слайс шарда
			index    int      // Текущий индекс в слайсе
			shardIdx int      // Индекс шарда в shardResults
		}

		// Инициализируем heap первыми элементами каждого шарда (РАБОТАЕМ С ИНДЕКСАМИ!)
		heap := make([]shardIterator, 0, len(shardResults))

		for i, sd := range shardResults {
			slice := sd.records.Slice()
			if len(slice) > 0 {
				heap = append(heap, shardIterator{
					slice:    slice,
					index:    0,
					shardIdx: i,
				})
			}
		}

		// Простой k-way merge без container/heap (для производительности)
		for len(heap) > 0 && len(finalRecords) < limit {
			// Находим минимальный элемент (РАБОТАЕМ С ИНДЕКСАМИ, НЕ КОПИРУЕМ!)
			minIdx := 0
			for i := 1; i < len(heap); i++ {
				recI := heap[i].slice[heap[i].index]
				recMin := heap[minIdx].slice[heap[minIdx].index]
				cmp := compareKeys(recI.Key, recMin.Key)
				if cmp < 0 || (cmp == 0 && recI.ShardID < recMin.ShardID) {
					minIdx = i
				}
			}

			// Получаем ссылку на минимальный элемент (НЕ КОПИРУЕМ!)
			rec := heap[minIdx].slice[heap[minIdx].index]

			// Применяем фильтрацию по lastKey и lastShardID (составной ключ)
			if len(lastKey) > 0 {
				cmp := compareKeys(rec.Key, lastKey)
				// Пропускаем если: key < lastKey ИЛИ (key == lastKey AND shardID <= lastShardID)
				if cmp < 0 || (cmp == 0 && rec.ShardID <= lastShardID) {
					heap[minIdx].index++
					if heap[minIdx].index >= len(heap[minIdx].slice) {
						heap = append(heap[:minIdx], heap[minIdx+1:]...)
					}
					continue
				}
			}

			finalRecords = append(finalRecords, rec)

			// Заменяем минимальный элемент следующим из того же шарда
			heap[minIdx].index++
			if heap[minIdx].index >= len(heap[minIdx].slice) {
				heap = append(heap[:minIdx], heap[minIdx+1:]...)
			}
		}
	}

	// 3. Освобождаем memory pool
	for _, sd := range shardResults {
		sd.records.Release()
	}

	// 4. Формируем NextKey для следующего шага
	var nextKey []byte
	var lastID int64
	var hasMore bool

	if len(finalRecords) > 0 {
		lastRec := finalRecords[len(finalRecords)-1]
		nextKey = lastRec.Key
		lastID = lastRec.ShardID
		hasMore = (len(finalRecords) == limit)
	}

	return &SearchResult{
		Records:     finalRecords,
		HasMore:     hasMore,
		NextKey:     nextKey,
		LastShardID: lastID,
		Total:       total,
		ShardStats:  shardStats,
	}, nil
}

// ============================================================================
// gatherResults собирает и агрегирует результаты со всех шардов с использованием k-way merge
func (a *Aggregator) gatherResults(ctx context.Context, resultsCh <-chan *ShardResult, limit int, lastKey []byte, lastShardID int64) (*SearchResult, error) {
	var total uint64
	shardStats := make(map[int64]uint64)

	// 1. Собираем результаты из каналов в sorted slices по шардам
	type shardData struct {
		shardID int64
		records RecordSlice
	}

	shardResults := make([]*shardData, 0, 32) // ожидаемое количество шардов

	for result := range resultsCh {
		if result.Error != nil {
			continue
		}

		total += uint64(len(result.Records))
		shardStats[result.ShardID] = uint64(len(result.Records))

		// Создаем sorted slice для этого шарда
		slice := GetRecordSlice()
		for _, rec := range result.Records {
			rec.ShardID = result.ShardID // Устанавливаем ShardID как tie-breaker
			slice.Append(rec)
		}
		sort.Sort(slice) // Сортируем внутри шарда (меньше данных чем глобально)

		shardResults = append(shardResults, &shardData{
			shardID: result.ShardID,
			records: *slice,
		})
	}

	// 2. K-way merge через min-heap (ИСПОЛЬЗУЕМ ПУЛ!)
	finalRecords := GetFinalResult(limit)
	defer PutFinalResult(finalRecords) // Возвращаем в пул после использования

	if len(shardResults) == 1 {
		// Оптимизация: если один шард, просто берём из него
		slice := shardResults[0].records.Slice()
		for _, rec := range slice {
			// Применяем фильтрацию по lastKey и lastShardID (составной ключ)
			if len(lastKey) > 0 {
				cmp := compareKeys(rec.Key, lastKey)
				// Пропускаем если: key < lastKey ИЛИ (key == lastKey AND shardID <= lastShardID)
				if cmp < 0 || (cmp == 0 && rec.ShardID <= lastShardID) {
					continue
				}
			}
			finalRecords = append(finalRecords, rec)
			if len(finalRecords) >= limit {
				break
			}
		}
	} else if len(shardResults) > 1 {
		// K-way merge через heap (ОПТИМИЗИРОВАНО - работаем с индексами, не копиями!)
		type shardIterator struct {
			slice    []Record // Ссылка на слайс шарда
			index    int      // Текущий индекс в слайсе
			shardIdx int      // Индекс шарда в shardResults
		}

		// Инициализируем heap первыми элементами каждого шарда (РАБОТАЕМ С ИНДЕКСАМИ!)
		heap := make([]shardIterator, 0, len(shardResults))

		for i, sd := range shardResults {
			slice := sd.records.Slice()
			if len(slice) > 0 {
				heap = append(heap, shardIterator{
					slice:    slice,
					index:    0,
					shardIdx: i,
				})
			}
		}

		// Простой k-way merge без container/heap (для производительности)
		for len(heap) > 0 && len(finalRecords) < limit {
			// Находим минимальный элемент (РАБОТАЕМ С ИНДЕКСАМИ, НЕ КОПИРУЕМ!)
			minIdx := 0
			for i := 1; i < len(heap); i++ {
				recI := heap[i].slice[heap[i].index]
				recMin := heap[minIdx].slice[heap[minIdx].index]
				cmp := compareKeys(recI.Key, recMin.Key)
				if cmp < 0 || (cmp == 0 && recI.ShardID < recMin.ShardID) {
					minIdx = i
				}
			}

			// Получаем ссылку на минимальный элемент (НЕ КОПИРУЕМ!)
			rec := heap[minIdx].slice[heap[minIdx].index]

			// Применяем фильтрацию по lastKey и lastShardID (составной ключ)
			if len(lastKey) > 0 {
				cmp := compareKeys(rec.Key, lastKey)
				// Пропускаем если: key < lastKey ИЛИ (key == lastKey AND shardID <= lastShardID)
				if cmp < 0 || (cmp == 0 && rec.ShardID <= lastShardID) {
					heap[minIdx].index++
					if heap[minIdx].index >= len(heap[minIdx].slice) {
						heap = append(heap[:minIdx], heap[minIdx+1:]...)
					}
					continue
				}
			}

			finalRecords = append(finalRecords, rec)

			// Заменяем минимальный элемент следующим из того же шарда
			heap[minIdx].index++
			if heap[minIdx].index >= len(heap[minIdx].slice) {
				heap = append(heap[:minIdx], heap[minIdx+1:]...)
			}
		}
	}

	// 3. Освобождаем memory pool
	for _, sd := range shardResults {
		sd.records.Release()
	}

	// 4. Формируем NextKey для следующего шага
	var nextKey []byte
	var lastID int64
	var hasMore bool

	if len(finalRecords) > 0 {
		lastRec := finalRecords[len(finalRecords)-1]
		nextKey = lastRec.Key
		lastID = lastRec.ShardID
		hasMore = (len(finalRecords) == limit)
	}

	return &SearchResult{
		Records:     finalRecords,
		HasMore:     hasMore,
		NextKey:     nextKey,
		LastShardID: lastID,
		Total:       total,
		ShardStats:  shardStats,
	}, nil
}

func (a *Aggregator) SearchWithCursor(ctx context.Context, terms []string, cursor *Cursor, limit int) (*SearchResult, error) {
	var lastKey []byte
	if cursor != nil && len(cursor.Key) > 0 {
		lastKey = make([]byte, len(cursor.Key))
		copy(lastKey, cursor.Key)
	}

	opts := SearchOptions{
		Terms:   terms,
		Limit:   limit,
		LastKey: lastKey,
	}

	return a.Search(ctx, opts)
}

// ============================================================================
// Stage 2: SearchIDs - поиск только IDs с последующим batch fetch
// ============================================================================

// SearchIDs выполняет поиск только IDs (Stage 2 оптимизация)
func (a *Aggregator) SearchIDs(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 100 // default limit
	}

	// Получаем список всех шардов
	allShards := a.selector.SelectAll()
	if len(allShards) == 0 {
		return &SearchResult{
			Records:    []Record{},
			HasMore:    false,
			Total:      0,
			ShardStats: make(map[int64]uint64),
		}, nil
	}

	// Фильтруем шарды на основе битовых масок (оптимизация)
	filteredShards := a.filter.FilterShards(opts.Terms, allShards)
	if len(filteredShards) == 0 {
		return &SearchResult{
			Records:    []Record{},
			HasMore:    false,
			Total:      0,
			ShardStats: make(map[int64]uint64),
		}, nil
	}

	// Создаем контекст с таймаутом если не установлен
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel func()
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	// SCATTER: Распределяем запросы по шардам параллельно (только IDs!)
	resultsCh := make(chan *ShardResult, len(filteredShards))
	var wg sync.WaitGroup

	for _, shardID := range filteredShards {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			a.scatterToShardIDs(ctx, id, opts, resultsCh) // Stage 2: только IDs
		}(shardID)
	}

	// Запускаем горутину для закрытия канала после завершения всех запросов
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// GATHER: Собираем результаты со всех шардов (Stage 2 версия)
	return a.gatherResultsIDs(ctx, resultsCh, opts.Limit, opts.LastKey, opts.LastShardID)
}

// gatherResultsIDs собирает и агрегирует только IDs с использованием k-way merge
func (a *Aggregator) gatherResultsIDs(ctx context.Context, resultsCh <-chan *ShardResult, limit int, lastKey []byte, lastShardID int64) (*SearchResult, error) {
	var total uint64
	shardStats := make(map[int64]uint64)

	// 1. Собираем результаты из каналов в sorted slices по шардам (только IDs!)
	type shardData struct {
		shardID int64
		ids     []int32 // Только IDs, не Records!
	}

	shardResults := make([]*shardData, 0, 32) // ожидаемое количество шардов

	for result := range resultsCh {
		if result.Error != nil {
			continue
		}

		total += uint64(len(result.IDs))
		shardStats[result.ShardID] = uint64(len(result.IDs))

		// Создаем sorted slice для этого шарда (только IDs)
		sortedIDs := make([]int32, len(result.IDs))
		copy(sortedIDs, result.IDs)
		sort.Slice(sortedIDs, func(i, j int) bool {
			return sortedIDs[i] < sortedIDs[j]
		})

		shardResults = append(shardResults, &shardData{
			shardID: result.ShardID,
			ids:     sortedIDs,
		})
	}

	// 2. K-way merge через min-heap (только IDs!)
	finalIDs := make([]int32, 0, limit)

	if len(shardResults) == 1 {
		// Оптимизация: если один шард, просто берём из него
		slice := shardResults[0].ids
		for _, id := range slice {
			finalIDs = append(finalIDs, id)
			if len(finalIDs) == limit {
				break
			}
		}
	} else if len(shardResults) > 1 {
		// K-way merge через heap (только IDs!)
		heap := make([]struct {
			id       int32
			shardIdx int
		}, 0, len(shardResults))

		for i, sd := range shardResults {
			if len(sd.ids) > 0 {
				heap = append(heap, struct {
					id       int32
					shardIdx int
				}{sd.ids[0], i})
			}
		}

		// Простой k-way merge без container/heap (для производительности)
		for len(heap) > 0 && len(finalIDs) < limit {
			// Находим минимальный элемент
			minIdx := 0
			for i := 1; i < len(heap); i++ {
				if heap[i].id < heap[minIdx].id {
					minIdx = i
				}
			}

			finalIDs = append(finalIDs, heap[minIdx].id)

			// Заменяем минимальный элемент следующим из того же шарда
			shardIdx := heap[minIdx].shardIdx
			slice := shardResults[shardIdx].ids
			nextIdx := 1 // Мы уже взяли первый элемент

			if nextIdx < len(slice) {
				heap[minIdx] = struct {
					id       int32
					shardIdx int
				}{slice[nextIdx], shardIdx}
			} else {
				// Шард исчерпан, удаляем его из heap
				heap = append(heap[:minIdx], heap[minIdx+1:]...)
			}
		}
	}

	// 3. BATCH FETCH: Получаем полные Records по IDs через MultiGet
	var finalRecords []Record
	if len(finalIDs) > 0 {
		finalRecords = a.batchFetchRecords(ctx, finalIDs)
	}

	// 4. Формируем NextKey для следующего шага (используем последний ID как курсор)
	var nextKey []byte
	var lastID int64
	var hasMore bool

	if len(finalRecords) > 0 {
		lastRec := finalRecords[len(finalRecords)-1]
		nextKey = lastRec.Key
		lastID = lastRec.ShardID
		hasMore = (len(finalRecords) == limit)
	}

	return &SearchResult{
		Records:     finalRecords,
		HasMore:     hasMore,
		NextKey:     nextKey,
		LastShardID: lastID,
		Total:       total,
		ShardStats:  shardStats,
	}, nil
}

// batchFetchRecords получает полные Records по списку IDs через MultiGet
func (a *Aggregator) batchFetchRecords(ctx context.Context, ids []int32) []Record {
	if len(ids) == 0 {
		return nil
	}

	// Группируем IDs по шардам для эффективного batch fetch
	type shardIDs struct {
		shardID int64
		ids     []int32
	}

	shardToIDs := make(map[int64][]int32)
	for _, id := range ids {
		// Определяем шард по ID (простая хеш-функция, как в ShardSelector)
		shardID := int64(id) % 100 // TODO: использовать реальную функцию распределения
		shardToIDs[shardID] = append(shardToIDs[shardID], id)
	}

	// Параллельный batch fetch по всем шардам
	resultsCh := make(chan struct {
		records []Record
		error   error
	}, len(shardToIDs))

	var wg sync.WaitGroup
	for shardID, shardIDsList := range shardToIDs {
		wg.Add(1)
		go func(sid int64, sids []int32) {
			defer wg.Done()

			client, ok := a.registry.Get(sid)
			if !ok {
				resultsCh <- struct {
					records []Record
					error   error
				}{nil, errors.New("shard not found")}
				return
			}

			// Batch fetch через MultiGet (оптимизация!)
			response, err := client.MultiGet(ctx, sids)
			if err != nil {
				resultsCh <- struct {
					records []Record
					error   error
				}{nil, err}
				return
			}

			// Устанавливаем ShardID для детерминизма сортировки
			for i := range response.Records {
				response.Records[i].ShardID = sid
			}

			resultsCh <- struct {
				records []Record
				error   error
			}{response.Records, nil}
		}(shardID, shardIDsList)
	}

	// Запускаем горутину для закрытия канала после завершения всех запросов
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Собираем все Records
	var allRecords []Record
	for result := range resultsCh {
		if result.error == nil {
			allRecords = append(allRecords, result.records...)
		}
	}

	return allRecords
}
