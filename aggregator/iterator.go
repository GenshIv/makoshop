package aggregator

import (
	"container/heap"
)

// ShardIterator - итератор для одного шарда
type ShardIterator struct {
	records []Record
	index   int
}

// NewShardIterator создаёт новый итератор для шарда
func NewShardIterator(records []Record) *ShardIterator {
	return &ShardIterator{
		records: records,
		index:   0,
	}
}

// Next возвращает следующую запись или nil если конец
func (it *ShardIterator) Next() *Record {
	if it.index >= len(it.records) {
		return nil
	}
	rec := &it.records[it.index]
	it.index++
	return rec
}

// Peek возвращает текущую запись без advancement
func (it *ShardIterator) Peek() *Record {
	if it.index >= len(it.records) {
		return nil
	}
	return &it.records[it.index]
}

// HasNext проверяет есть ли следующая запись
func (it *ShardIterator) HasNext() bool {
	return it.index < len(it.records)
}

// KWayMerger - k-way merger для сортировки результатов из нескольких шардов
type KWayMerger struct {
	iterators []*ShardIterator
	heap      mergeHeap
}

// mergeHeap реализует min-heap для Record pointers с использованием ShardID как tie-breaker
type mergeHeap []struct {
	record   *Record
	iterator int // индекс в iterators массиве
}

func (h mergeHeap) Len() int { return len(h) }

func (h mergeHeap) Less(i, j int) bool {
	// Сравниваем ключи напрямую без конвертации в строки
	cmp := compareKeys(h[i].record.Key, h[j].record.Key)
	if cmp != 0 {
		return cmp < 0
	}
	// Tie-breaker: ShardID для детерминизма при одинаковых ключах
	return h[i].record.ShardID < h[j].record.ShardID
}

func (h mergeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *mergeHeap) Push(x interface{}) {
	*h = append(*h, x.(struct {
		record   *Record
		iterator int
	}))
}

func (h *mergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// NewKWayMerger создаёт новый k-way merger
func NewKWayMerger(shardResults []*ShardResult) *KWayMerger {
	merger := &KWayMerger{
		iterators: make([]*ShardIterator, 0, len(shardResults)),
		heap:      make(mergeHeap, 0, len(shardResults)),
	}

	for _, result := range shardResults {
		if len(result.Records) > 0 {
			iterator := NewShardIterator(result.Records)
			merger.iterators = append(merger.iterators, iterator)

			// Добавляем первый элемент в heap
			if rec := iterator.Peek(); rec != nil {
				heap.Push(&merger.heap, struct {
					record   *Record
					iterator int
				}{rec, len(merger.iterators) - 1})
			}
		}
	}

	heap.Init(&merger.heap)
	return merger
}

// Next возвращает следующую запись в отсортированном порядке или nil если конец
func (m *KWayMerger) Next() *Record {
	if len(m.heap) == 0 {
		return nil
	}

	elem := heap.Pop(&m.heap).(struct {
		record   *Record
		iterator int
	})

	rec := elem.record

	// Добавляем следующую запись из того же итератора если есть
	if iterator := m.iterators[elem.iterator]; iterator.HasNext() {
		if nextRec := iterator.Next(); nextRec != nil {
			heap.Push(&m.heap, struct {
				record   *Record
				iterator int
			}{nextRec, elem.iterator})
		}
	}

	return rec
}

// HasNext проверяет есть ли следующая запись
func (m *KWayMerger) HasNext() bool {
	return len(m.heap) > 0
}
