# Система отзывов и рейтингов — Дизайн

## Текущее состояние

### Бэкенд (Go)
- `model.Review` — базовая модель: ID, ProductID, UserID, Rating (1-5), Comment, CreatedAt
- `ReviewRepo` — CRUD: Create, Get, ListByProduct, ListByUser, Delete
- API: `POST /products/{id}/reviews` (создание), `GET /products/{id}/reviews` (список)
- **Нет:** админ-эндпоинтов, модерации, агрегации рейтингов, блокировки пользователей

### Фронтенд (Vue.js)
- `ProductView.vue` — отображение отзывов, форма добавления
- `EANPageView.vue` — сравнение товаров
- `ProductCard.vue` — отображение рейтинга (если есть)
- `useSeo.js` — поддерживает JSON-LD (только что добавлено)
- **Нет:** админ-страницы отзывов, управления рейтингами

---

## Цель

Полноценная система отзывов и рейтингов с:
1. **Агрегацией рейтингов** — avg_rating/review_count на уровне EANPage (не продукта, т.к. каталог работает по EANPage)
2. **Модерацией** — одобрение/отклонение отзывов администратором
3. **Админ-панелью** — просмотр, фильтрация, модерация, удаление отзывов
4. **Seller-панелью** — просмотр отзывов на свои товары
5. **JSON-LD** — aggregateRating + review для Google Rich Results

---

## 1. Модель данных

### 1.1 Расширение Review

```go
type ReviewStatus string

const (
    ReviewStatusPending  ReviewStatus = "pending"   // новое, ждёт модерации
    ReviewStatusApproved ReviewStatus = "approved"  // одобрено
    ReviewStatusRejected ReviewStatus = "rejected"  // отклонено
    ReviewStatusHidden   ReviewStatus = "hidden"    // скрыто (спам/жалобы)
)

type Review struct {
    ID           int64        `json:"id"`
    ProductID    int64        `json:"product_id"`
    EAN          string       `json:"ean,omitempty"` // computed: product's EAN
    EANPageID    int64        `json:"ean_page_id,omitempty"` // computed: EAN page ID
    UserID       int64        `json:"user_id"`
    Rating       int          `json:"rating"`
    Comment      string       `json:"comment,omitempty"`
    Status       ReviewStatus `json:"status"`
    IsFeatured   bool         `json:"is_featured"` // отмеченный админом отзыв
    Verified     bool         `json:"verified"`    // подтверждённая покупка (future)
    CreatedAt    int64        `json:"created_at"`
    UpdatedAt    int64        `json:"updated_at"`
}
```

### 1.2 Расширение Product

Добавляем поля для кэширования рейтинга (обновляются при создании/изменении отзыва):

```go
type Product struct {
    // ... existing fields ...
    AvgRating     float64 `json:"avg_rating,omitempty"`     // средний рейтинг (вычисляется)
    ReviewCount   int     `json:"review_count,omitempty"`   // количество отзывов
}
```

### 1.3 Новая модель: RatingSummary (опционально)

Для агрегации по EANPage можно создать отдельную сущность:

```go
type RatingSummary struct {
    EANPageID   int64            `json:"ean_page_id"`
    AvgRating   float64          `json:"avg_rating"`
    ReviewCount int              `json:"review_count"`
    RatingBreakdown map[int]int `json:"rating_breakdown"` // {5: 10, 4: 5, ...}
    UpdatedAt   int64            `json:"updated_at"`
}
```

**Решение:** Начнём без отдельной модели — будем вычислять on-the-fly через SQL/индекс.

---

## 2. Бэкенд API

### 2.1 Агрегация рейтингов (обязательно)

**GET /admin/reviews/stats** — статистика по отзывам
```json
{
    "total_reviews": 1234,
    "pending": 56,
    "approved": 1100,
    "rejected": 78,
    "avg_rating": 4.2,
    "rating_breakdown": { "5": 600, "4": 300, "3": 200, "2": 100, "1": 34 }
}
```

**POST /admin/reviews/recalculate** — пересчитать avg_rating для всех EANPage
```json
{ "status": "completed", "eanpages_updated": 123 }
```

### 2.2 Админ-эндпоинты для отзывов

**GET /admin/reviews** — список отзывов с фильтрацией
```
GET /admin/reviews?page=1&limit=50&status=approved&product_id=123&user_id=456&e=123456789
```
Response:
```json
{
    "items": [
        {
            "id": 1,
            "product_id": 123,
            "ean": "123456789",
            "user_id": 456,
            "user_name": "John D.",
            "rating": 5,
            "comment": "Great product!",
            "status": "approved",
            "is_featured": false,
            "created_at": 1234567890,
            "updated_at": 1234567890
        }
    ],
    "total": 100,
    "page": 1,
    "limit": 50
}
```

**GET /admin/reviews/{id}** — получить отзыв
```
GET /admin/reviews/123
```

**PATCH /admin/reviews/{id}** — обновить отзыв (модерация)
```
PATCH /admin/reviews/123
Body: { "status": "approved", "is_featured": true }
```

**DELETE /admin/reviews/{id}** — удалить отзыв
```
DELETE /admin/reviews/123
```

**POST /admin/reviews/bulk-actions** — массовые действия
```
POST /admin/reviews/bulk-actions
Body: { "action": "approve", "ids": [1, 2, 3] }
```

### 2.3 Seller-эндпоинты

**GET /seller/reviews** — отзывы на товары продавца
```
GET /seller/reviews?product_id=123
```

### 2.4 Публичные эндпоинты

**GET /products/{id}/reviews** — список отзывов продукта (текущий, добавить status filter)
```
GET /products/{id}/reviews?status=approved
```

**GET /eanpages/{id}/reviews** — отзывы для EANPage
```
GET /eanpages/{id}/reviews?status=approved
```

**GET /eanpages/{id}/rating-summary** — агрегация рейтингов для EANPage
```json
{
    "ean_page_id": 123,
    "avg_rating": 4.2,
    "review_count": 45,
    "rating_breakdown": { "5": 30, "4": 10, "3": 3, "2": 1, "1": 1 }
}
```

### 2.5 Обновление при создании/изменении отзыва

При создании/обновлении/удалении отзыва — пересчитывать avg_rating для связанного EANPage.

---

## 3. Изменение логики создания отзыва

### 3.1 Новая логика Create

1. Проверить, не писал ли пользователь уже отзыв на ЭТУ же EANPage (не продукт!)
2. Создать отзыв со статусом `pending` (если модерация включена) или `approved`
3. Если модерация выключена — сразу `approved`
4. Пересчитать avg_rating для EANPage

### 3.2 Настройки модерации

```go
type ReviewSettings struct {
    ModerationEnabled bool `json:"moderation_enabled"`
}
```

Хранится в глобальных настройках сайта (`/admin/settings`).

---

## 4. Фронтенд

### 4.1 Админ-панель: /admin/reviews

Страница с таблицей отзывов:
- Колонки: ID, Продукт (ссылка), EAN, Пользователь, Рейтинг (звёзды), Комментарий, Статус, Дата, Действия
- Фильтры: статус, пользователь, продукт, дата
- Пагинация
- Массовые действия: одобрить все, отклонить выбранные, удалить выбранные
- Действия на отзыв: одобрить, отклонить, скрыть, отметить как избранный, удалить

### 4.2 Seller-панель: /seller/reviews

Страница с отзывами на товары продавца:
- Таблица как в админке, но только для продуктов продавца
- Может отклонять отзывы на свои товары

### 4.3 Страница товара: ProductView

- Отображать только `approved` отзывы
- Показывать badge "verified purchase" (future)
- Кнопка "Полезный отзыв" (like/dislike) — future

### 4.4 JSON-LD на EANPage

```json
{
    "@context": "https://schema.org",
    "@type": "Product",
    "name": "...",
    "aggregateRating": {
        "@type": "AggregateRating",
        "ratingValue": "4.2",
        "bestRating": "5",
        "ratingCount": 45
    },
    "review": [
        {
            "@type": "Review",
            "author": { "@type": "Person", "name": "John D." },
            "datePublished": "2024-01-01",
            "reviewBody": "Great product!",
            "reviewRating": { "@type": "Rating", "ratingValue": "5" }
        }
    ]
}
```

---

## 5. Индексация и производительность

### 5.1 Turbo-индексы для отзывов

- `review_status:approved` → список ID одобренных отзывов
- `review_product:{productID}` → отзывы по продукту (уже есть)
- `review_eanpage:{eanPageID}` → отзывы по EANPage (новое)
- `review_user:{userID}` → отзывы пользователя (уже есть)
- `review_date:{unix_ts}` → для сортировки по дате

### 5.2 Кэширование avg_rating

- Хранить в Product.AvgRating и Product.ReviewCount
- Обновлять при каждом создании/удалении/изменении отзыва
- Для EANPage — вычислять как среднее avg_rating продуктов в группе

### 5.3 Background jobs

- Пересчёт avg_rating для всех EANPage (cron/админ-кнопка)
- Очистка старых отзывов (future)

---

## 6. Приоритеты реализации

### Phase 1 (MVP)
- [x] Расширение модели Review (Status, UpdatedAt, EAN, EANPageID)
- [ ] Добавление AvgRating/ReviewCount в Product
- [ ] API: GET /admin/reviews, PATCH /admin/reviews/{id}, DELETE /admin/reviews/{id}
- [ ] API: POST /products/{id}/reviews — обновление avg_rating
- [ ] Админ-страница /admin/reviews
- [ ] Публичный API: GET /eanpages/{id}/rating-summary

### Phase 2
- [ ] Модерация: статусы pending/approved/rejected
- [ ] Настройка модерации в админке
- [ ] Seller-панель отзывов
- [ ] JSON-LD на EANPage с отзывами
- [ ] Turbo-индексация по EANPage

### Phase 3
- [ ] Verified purchase badge
- [ ] Like/dislike на отзывы
- [ ] Featured reviews
- [ ] Email уведомления о новых отзывах
- [ ] Analytics: отзывы за период, тренды

---

## 7. Совместимость с текущей архитектурой

### Паттерны, которые используем:
- `silentjson` для сериализации (как в других репозиториях)
- Turbo-индексы для поиска (как в product_repo)
- `httpres.WriteJSON` / `httpres.WriteError` (как в других handlers)
- `jwtMiddleware.RequireRole` для админ-эндпоинтов
- `auth.ContextUserFrom` для проверки пользователя

### Ключи хранения:
- `review:{id}` — документ отзыва
- `review_product:{productID}` — индекс по продукту
- `review_eanpage:{eanPageID}` — индекс по EANPage (новое)
- `review_user:{userID}` — индекс по пользователю
- `review_status:{status}` — индекс по статусу (new: approved, pending, rejected)
