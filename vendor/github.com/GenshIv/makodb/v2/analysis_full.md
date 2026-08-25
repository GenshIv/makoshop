# Полный анализ проекта

## Структура проекта

### 1. core/shm.go
- **Функции:** init, lockShm, unlockShm
- **Типы:** ShmLock

### 2. core/state.go
- **Функции:** init, getTimestamp, getState, getProcessed, getTotal, getStartTime, getElapsed, getSpeed, getStatus, getError, setProcessed, setTotal, setElapsed, setSpeed, setStatus, setError
- **Типы:** ServerState

### 3. cmd/demoserver/main.go
- **Функции:** init, updateProgress, setStatus, setError, serializeInt32, deserializeInt32, generateTransactions, importTransactions, findUniqueCategories, findUniqueMerchants, findUniqueStatuses, sortAndAggregate, getShard, sortAndAggregateShard, processShard, compareByCountry, compareByCategory, compareByStatus, compareByRevenue, compareByDate, compareByProfit, sortTransactions, aggregateTransactions, main
- **Структуры:** Transaction, SortTemp, ServerState, SortItem[T], SortPairInt

## Анализ связей

### core/shm.go
- Шм-блокировка для shared memory
- RobustShmMutex из hft-ipc

### core/state.go
- ServerState для отслеживания состояния сервер
- Sync.RWMutex для потоко безопасности
- Методы для чтения/запису состояния

### cmd/demoserver/main.go
- Главный логик проекта
- Transaction - структура для транзакций
- SortTemp - временная структура для сортирования
- SortItem - генерик для сортирования
- SortPairInt - пара для сортирования
- Функции для генерации, импорта, сортирования и агрегации транзакций
- findUniqueCategories, findUniqueMerchants, findUniqueStatuses - поиск уникальных значений
- sortAndAggregate - основная функция для сортирования и агрегации
- getShard - получение shard по ключу
- sortAndAggregateShard - сортирование и агрегация shard
- processShard - обработка shard
- compareByCountry, compareByCategory, compareByStatus, compareByRevenue, compareByDate, compareByProfit - компараторы
- sortTransactions - сортирование транзакций
- aggregateTransactions - агрегация транзакций
- main - главный функция

## Архитектура

1. core/shm.go - shared memory locking
2. core/state.go - server state management
3. cmd/demoserver/main.go - main logic

## Завимости

- github.com/GenshIv/hft-ipc v1.2.4 (shm.OpenOrCreateMmap, RobustShmMutex)
- github.com/edsrzf/mmap-go
- github.com/GenshIv/silentjson/v2

## Потенциальные проблемы

1. Нет обработки ошибок в main
2. Нет graceful shutdown
3. Нет concurrency control
4. Нет validation of inputs
5. Нет logging
6. No tests
7. No documentation
