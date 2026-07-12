package aggregator

import (
	"bytes"
	"sync"
)

// ============================================================================
// MinHeap - кэш-оптимизированная минимальная куча для K-Way Merge
// БЕЗ interface{} - все аллокации на стеке или в pre-allocated буфере!
// ============================================================================

// MinHeap - бинарная минимальная куча для HeapItem
// Хранит элементы в фиксированном массиве (без heap.Interface!)
type MinHeap struct {
	items []HeapItem // Фиксированный слайс, пред-аллоцированный
	size  int        // Текущий размер кучи
}

// NewMinHeap создаёт новую минимальную кучу с заданной ёмкостью
func NewMinHeap(capacity int) *MinHeap {
	return &MinHeap{
		items: make([]HeapItem, 0, capacity), // Пред-аллокация!
		size:  0,
	}
}

// Reset сбрасывает кучу без освобождения памяти (для переиспользования)
func (h *MinHeap) Reset() {
	h.size = 0
}

// Len возвращает текущее количество элементов
func (h *MinHeap) Len() int {
	return h.size
}

// Capacity возвращает ёмкость внутреннего буфера
func (h *MinHeap) Capacity() int {
	return cap(h.items)
}

// IsEmpty проверяет, пуста ли куча
func (h *MinHeap) IsEmpty() bool {
	return h.size == 0
}

// Less сравнивает два элемента по Score (убывание), затем по Key (возрастание)
// ВНИМАНИЕ: Это инвертированная логика для MIN-кучи!
func (h *MinHeap) Less(i, j int) bool {
	a := &h.items[i]
	b := &h.items[j]

	// Сначала по Score (убывание - больше score = меньше в куче)
	if a.Score != b.Score {
		return a.Score > b.Score // Инвертируем для MIN-кучи!
	}

	// Затем по Key (возрастание)
	return bytes.Compare(a.Key, b.Key) < 0
}

// Swap меняет местами два элемента
func (h *MinHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
}

// Push добавляет элемент в кучу (O(log n))
// HOT PATH: Zero-allocation через pre-allocated буфер!
func (h *MinHeap) Push(item HeapItem) {
	// ВНИМАНИЕ: Если capacity исчерпан, запись игнорируется (не allocation!)
	// Это соответствует Закону 3 — zero-allocation в hot path.
	if h.size >= cap(h.items) {
		return // ❌ Allocation forbidden — просто игнорируем переполнение
	}

	h.items[h.size] = item // Direct write без append()!
	h.size++
	h.siftUp(h.size - 1)
}

// Pop удаляет и возвращает корневой элемент (O(log n))
func (h *MinHeap) Pop() HeapItem {
	if h.size == 0 {
		return HeapItem{}
	}

	root := h.items[0]
	h.size--

	if h.size > 0 {
		h.items[0] = h.items[h.size]
		h.siftDown(0)
	}

	return root
}

// Peek возвращает корневой элемент без удаления (O(1))
func (h *MinHeap) Peek() HeapItem {
	if h.size == 0 {
		return HeapItem{}
	}
	return h.items[0]
}

// siftUp поднимает элемент вверх по куче (для Push)
func (h *MinHeap) siftUp(idx int) {
	for idx > 0 {
		parent := (idx - 1) / 2
		if !h.Less(idx, parent) {
			break
		}
		h.Swap(idx, parent)
		idx = parent
	}
}

// siftDown опускает элемент вниз по куче (для Pop)
func (h *MinHeap) siftDown(idx int) {
	for {
		left := 2*idx + 1
		right := 2*idx + 2
		smallest := idx

		if left < h.size && h.Less(left, smallest) {
			smallest = left
		}
		if right < h.size && h.Less(right, smallest) {
			smallest = right
		}

		if smallest == idx {
			break
		}

		h.Swap(idx, smallest)
		idx = smallest
	}
}

// ============================================================================
// HeapItem - элемент кучи с полным набором полей для сравнения
// ============================================================================

type HeapItem struct {
	ID      int32   // ID записи
	ShardID int64   // ID шарда
	Key     []byte  // Ключ (для сравнения)
	Score   float32 // Score (первичный ключ сортировки)
	Idx     int     // Индекс в слайсе shard IDs
}

// ============================================================================
// Pool для MinHeap - переиспользование куч между запросами
// ============================================================================

var MinHeapPool = new(sync.Pool)

// GetMinHeap получает MinHeap из пула с указанной ёмкостью
func GetMinHeap(capacity int) *MinHeap {
	obj := MinHeapPool.Get()
	if obj == nil {
		return NewMinHeap(capacity)
	}
	h := obj.(*MinHeap)

	// Расширяем буфер если нужно
	if cap(h.items) < capacity {
		newCap := capacity
		if newCap < 16 {
			newCap = 16 // Минимальная ёмкость
		}
		h.items = make([]HeapItem, 0, newCap)
	} else {
		h.Reset()
	}

	return h
}

// PutMinHeap возвращает MinHeap в пул
func PutMinHeap(h *MinHeap) {
	// Trim до разумного размера чтобы не держать слишком много памяти
	if cap(h.items) > 1024 {
		h.items = h.items[:0] // Сбрасываем длину, ёмкость сохраняется
	} else {
		h.Reset()
	}
	MinHeapPool.Put(h)
}
