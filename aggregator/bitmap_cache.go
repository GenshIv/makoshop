package aggregator

import (
	"sync"
)

// ShardBitmapCache - кэш битовых масок для шардов
type ShardBitmapCache struct {
	// termShardMap - mapping от термина к битовой маске шардов, где он встречается
	termShardMap map[string]uint64
	// shardDocCount - количество документов на каждом шарде
	shardDocCount map[int64]uint64
	mu            sync.RWMutex
}

// NewShardBitmapCache создает новый кэш битовых масок
func NewShardBitmapCache() *ShardBitmapCache {
	return &ShardBitmapCache{
		termShardMap:  make(map[string]uint64),
		shardDocCount: make(map[int64]uint64),
	}
}

// UpdateTermShardMask обновляет битовую маску для термина
func (c *ShardBitmapCache) UpdateTermShardMask(term string, shardID int64, exists bool) {
	if shardID < 0 || shardID >= 64 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	mask := uint64(1 << uint(shardID))
	currentMask := c.termShardMap[term]

	if exists {
		// Добавляем шард в маску
		c.termShardMap[term] = currentMask | mask
	} else {
		// Удаляем шард из маски
		c.termShardMap[term] = currentMask &^ mask
	}
}

// GetTermShardMask возвращает битовую маску шардов для термина
func (c *ShardBitmapCache) GetTermShardMask(term string) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.termShardMap[term]
}

// UpdateShardDocCount обновляет количество документов на шарде
func (c *ShardBitmapCache) UpdateShardDocCount(shardID int64, count uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shardDocCount[shardID] = count
}

// GetShardDocCount возвращает количество документов на шарде
func (c *ShardBitmapCache) GetShardDocCount(shardID int64) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.shardDocCount[shardID]
}

// ComputeQueryMask вычисляет итоговую маску шардов для запроса
// Для AND - пересечение масок, для OR - объединение
func (c *ShardBitmapCache) ComputeQueryMask(terms []string, op QueryOperator) uint64 {
	if len(terms) == 0 {
		return 0 // Нет шардов для опроса
	}

	var result uint64
	first := true

	for _, term := range terms {
		mask := c.GetTermShardMask(term)
		switch op {
		case QueryOpAND:
			if first {
				result = mask
				first = false
			} else {
				result &= mask
			}
		case QueryOpOR:
			result |= mask
		}
	}

	return result
}

// ShardFilter - фильтр для оптимизации запросов к шардам
type ShardFilter struct {
	cache *ShardBitmapCache
}

// NewShardFilter создает новый фильтр шардов
func NewShardFilter(cache *ShardBitmapCache) *ShardFilter {
	return &ShardFilter{cache: cache}
}

// FilterShards фильтрует список шардов на основе запроса
func (f *ShardFilter) FilterShards(terms []string, allShards []int64) []int64 {
	if len(terms) == 0 {
		return allShards // Без терминов - опрашиваем все шарды
	}

	// Вычисляем маску для AND (пересечение)
	mask := f.cache.ComputeQueryMask(terms, QueryOpAND)

	// Если маска пустая - нет шардов с данными
	if mask == 0 {
		return nil
	}

	// Фильтруем список шардов по маске
	var filtered []int64
	for _, shardID := range allShards {
		if shardID >= 0 && shardID < 64 {
			if mask&(1<<uint(shardID)) != 0 {
				filtered = append(filtered, shardID)
			}
		}
	}

	return filtered
}

// EstimateResultCount оценивает количество результатов на шарде
func (f *ShardFilter) EstimateResultCount(shardID int64, terms []string) uint64 {
	if shardID < 0 || shardID >= 64 {
		return 0
	}

	docCount := f.cache.GetShardDocCount(shardID)
	if docCount == 0 {
		return 0
	}

	// Простая оценка: пересечение документов по терминам
	var estimated uint64 = docCount
	for _, term := range terms {
		mask := f.cache.GetTermShardMask(term)
		if mask&(1<<uint(shardID)) != 0 {
			// Термин есть на шарде - оцениваем как 50% документов
			estimated = estimated / 2
		} else {
			// Термина нет на шарде
			return 0
		}
	}

	return estimated
}
