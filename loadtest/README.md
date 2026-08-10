# Load Test

Простой нагрузочный тест для MakoShop бэкенда.

## Подготовка

1. Сгенерировать sample-файлы (slugs.txt, categories.txt):
```bash
cd loadtest
go run generate_samples.go
```

## Запуск

```bash
# Базовый тест (50 concurrent, 30 сек)
go run load.go

# Высокая нагрузка
go run load.go -c 200 -d 30s

# Максимальная нагрузка
go run load.go -c 500 -d 60s
```

## Параметры

| Параметр | Описание | По умолчанию |
|----------|----------|-------------|
| `-url` | Base URL сервера | http://localhost:9090 |
| `-c` | Конкурентность (goroutines) | 50 |
| `-d` | Длительность теста | 30s |

## Тестируемые эндпоинты

| Эндпоинт | Вес | Описание |
|----------|-----|----------|
| GET /shop | 35% | Корневой каталог |
| GET /shop/{category} | 20% | Каталог категории |
| GET /shop/{slug} | 25% | Страница товара (SCUPage) |
| GET /products | 8% | Поиск товаров |
| GET /products/turbo | 5% | Turbo-поиск |
| GET /categories/tree | 7% | Дерево категорий |

## Результаты (пример)

### 50 concurrent, 20s
```
Total: 51467 requests, 0 errors, 2567 req/s
GET /shop                  avg=16ms
GET /shop/{category}       avg=52ms
GET /shop/{slug}           avg=10ms
```

### 150 concurrent, 20s
```
Total: 58340 requests, 0 errors, 2901 req/s
GET /shop                  avg=43ms
GET /shop/{category}       avg=128ms
GET /shop/{slug}           avg=29ms
```

### 300 concurrent, 20s
```
Total: 60210 requests, 0 errors, 2982 req/s
GET /shop                  avg=87ms
GET /shop/{category}       avg=230ms
GET /shop/{slug}           avg=64ms
```

## Наблюдения

- `/shop/{category}` — самый медленный эндпоинт (сборка HTML каталога с атрибутами)
- `/shop` и `/shop/{slug}` — очень быстрые (SSR с INITIAL_DATA)
- `/products/turbo` — самый быстрый (Turbo-индексы)
- При 300 concurrent latency растёт, но ошибок нет
- Общий throughput ~3000 req/s на одном ядре
