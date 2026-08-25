# Анализ Sharded mmap-based JSON DB

## Архитектура
- **ShardedDB**: 1024 шарды (numShards=1024), каждый шард — отдельный DB (mmap)
- **DB**: mmap файл, header (Magic, Version, LockState@12, MaxSize, FreeOffset, NumBuckets), buckets (Hash, KeyOffset, KeyLen, ValOffset, ValLen, NextOffset)
- **Hash**: FNV-1a, chaining для collision
- **Atomic**: NextOffset, KeyOffset, ValOffset, ValLen — atomic для lock-free read

## Зависимости
- github.com/GenshIv/hft-ipc v1.2.4 (shm.OpenOrCreateMmap, RobustShmMutex)
- github.com/edsrzf/mmap-go
- github.com/GenshIv/silentjson/v2

## Мемory
- silentjson/v2 для JSON parsing (Query)
- hft-ipc для shared memory mutex (RobustShmMutex)

## Механика
- Resize при переполнении (MaxSize * 2)
- vacuumFile для compaction
- Shrink при Close

## Потенциальные проблемы
1. **Race conditions**: Atomic NextOffset, KeyOffset, ValLen не защищает против concurrent read/write
2. **Memory leaks**: freeList не используется для compaction, всегда Resize (MaxSize * 2)
3. **No validation**: Нет проверки Magic/Version при открытии
4. **No migration**: Нет версии migration
5. **No compression**: vacuumFile не реализову (или не вызову)
6. **Lock contention**: RobustShmMutex на каждый shard может быть bottleneck при high concurrency
7. **No indexing**: Нет secondary index для range queries или filters
8. **No transaction**: Нет ACID или rollback

## Оптимизации
1. Добавить freeList для compaction
2. Добавить validation Magic/Version
3. Реализовать vacuumFile
4. Добавить secondary index
5. Добавить transaction
