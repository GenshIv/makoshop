package aggregator

import (
	"bytes"
)

// ShardIterator - итератор для одного шарда (zero-ownership!)
type ShardIterator struct {
	records []Record // Projection на mmap, не owned!
	index   int      // Текущая позиция
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
// БЕЗ container/heap — custom implementation без interface{}!
type KWayMerger struct {
	iterators []*ShardIterator
	heap      []mergeHeapElement // Custom heap без interface{}!
	size      int                // Текущий размер heap
}

// mergeHeapElement - элемент heap (без interface{}!)
type mergeHeapElement struct {
	record   *Record
	iterator int // индекс в iterators массиве
}

// Len возвращает текущее количество элементов в heap
func (h *KWayMerger) Len() int {
	return h.size
}

// Less сравнивает два элемента: сначала по Key, затем по ShardID как tie-breaker
func (m *KWayMerger) Less(i, j int) bool {
	a := &m.heap[i]
	b := &m.heap[j]

	// Сравниваем ключи напрямую без конвертации в строки
	cmp := bytes.Compare(a.record.Key, b.record.Key)
	if cmp != 0 {
		return cmp < 0
	}
	// Tie-breaker: ShardID для детерминизма при одинаковых ключах
	return a.record.ShardID < b.record.ShardID
}

// Swap меняет местами два элемента в heap
func (m *KWayMerger) Swap(i, j int) {
	m.heap[i], m.heap[j] = m.heap[j], m.heap[i]
}

// siftUp поднимает элемент вверх по heap (для Push)
func (m *KWayMerger) siftUp(idx int) {
	for idx > 0 {
		parent := (idx - 1) / 2
		if !m.Less(idx, parent) {
			break
		}
		m.Swap(idx, parent)
		idx = parent
	}
}

// siftDown опускает элемент вниз по heap (для Pop)
func (m *KWayMerger) siftDown(idx int) {
	for {
		left := 2*idx + 1
		right := 2*idx + 2
		smallest := idx

		if left < m.size && m.Less(left, smallest) {
			smallest = left
		}
		if right < m.size && m.Less(right, smallest) {
			smallest = right
		}

		if smallest == idx {
			break
		}

		m.Swap(idx, smallest)
		idx = smallest
	}
}

// Push добавляет элемент в heap (O(log n)) — БЕЗ interface{}!
func (m *KWayMerger) Push(elem mergeHeapElement) {
	if m.size >= len(m.heap) {
		// Расширение heap (редко происходит)
		newCap := len(m.heap) * 2
		if newCap == 0 {
			newCap = 4
		}
		newHeap := make([]mergeHeapElement, len(m.heap), newCap)
		copy(newHeap, m.heap)
		m.heap = newHeap
	}
	m.heap[m.size] = elem
	m.size++
	m.siftUp(m.size - 1)
}

// Pop удаляет и возвращает корневой элемент (O(log n)) — БЕЗ interface{}!
func (m *KWayMerger) Pop() mergeHeapElement {
	if m.size == 0 {
		return mergeHeapElement{}
	}

	root := m.heap[0]
	m.size--

	if m.size > 0 {
		m.heap[0] = m.heap[m.size]
		m.siftDown(0)
	}

	return root
}

// Peek возвращает корневой элемент без удаления (O(1))
func (m *KWayMerger) Peek() mergeHeapElement {
	if m.size == 0 {
		return mergeHeapElement{}
	}
	return m.heap[0]
}

// Reset сбрасывает heap для переиспользования
func (m *KWayMerger) Reset() {
	m.size = 0
	m.iterators = m.iterators[:0]
}

// NewKWayMerger создаёт новый k-way merger
func NewKWayMerger(shardResults []*ShardResult) *KWayMerger {
	merger := &KWayMerger{
		iterators: make([]*ShardIterator, 0, len(shardResults)),
		heap:      make([]mergeHeapElement, 0, len(shardResults)),
		size:      0,
	}

	for _, result := range shardResults {
		if len(result.Records) > 0 {
			iterator := NewShardIterator(result.Records)
			merger.iterators = append(merger.iterators, iterator)

			// Добавляем первый элемент в heap (БЕЗ interface{}!)
			if rec := iterator.Peek(); rec != nil {
				merger.Push(mergeHeapElement{rec, len(merger.iterators) - 1})
			}
		}
	}

	return merger
}

// Next возвращает следующую запись в отсортированном порядке или nil если конец
func (m *KWayMerger) Next() *Record {
	if m.size == 0 {
		return nil
	}

	elem := m.Pop()
	rec := elem.record

	// Добавляем следующую запись из того же итератора если есть
	if iterator := m.iterators[elem.iterator]; iterator.HasNext() {
		if nextRec := iterator.Next(); nextRec != nil {
			m.Push(mergeHeapElement{nextRec, elem.iterator})
		}
	}

	return rec
}

// HasNext проверяет есть ли следующая запись
func (m *KWayMerger) HasNext() bool {
	return m.size > 0
}
