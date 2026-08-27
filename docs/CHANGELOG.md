# Makoshop — Changelog

## 2025-08-26 — Импорт прайсов Nokaut + система управления ценами

### Импорт прайсов Nokaut (XML)

- Новый эндпоинт `POST /admin/import-nokaut` — импорт офферов из Nokaut XML-файлов в `prices/{ImportFolder}/*.xml`.
- Идемпотентный импорт: уникальность = **EAN + нормализованное имя + компания**. Повторный импорт обновляет существующие товары, не создавая дубликаты.
- Стриминг-парсер (`internal/pricesrc/nokaut.go`) — эффективен для файлов до 200 МБ / 120 000+ офферов.
- Конфигурируемая маппинг-функция полей через `PriceSourceConfig` (разные компании предоставляют атрибуты по-разному):
  - `ean_field`, `previous_price_field`, `image_field`, `product_url_field`, `brand_field`, `shop_category_field`
  - `availability_map` — маппинг сырых значений наличия → `in_stock`/`out_of_stock`
  - `attr_fields` — дополнительные атрибуты из XML-пропёрти
- Валидация EAN: извлекает чистый цифровой код (8/12/13/14 или 6–20 цифр).
- Разбор цен с польской запятой: `139,9`, `1.234,56` → корректный float.

### Система управления ценами в админке

- `PATCH /admin/companies/{id}` — поддержка новых полей: `import_folder`, `price_source`, `desc_ru/ua/pl/en`, `hero_image`, `is_visible`.
- `GET /admin/companies/{id}/export` — экспорт конфигурации компании в JSON.
- `POST /admin/companies/import` — импорт конфигурации компании из JSON.
- `GET /admin/price-sources` — список конфигураций источников цен по всем компаниям.
- Админ-UI: модальное окно «Prices» в `AdminCompaniesView.vue` — настройка импорта, запуск импорта, экспорт.

### Лендинг компании

- `GET /company/{slug}` — публичная страница компании с мультиязычным описанием, hero-изображением и live-статистикой (кол-во товаров, образцы).
- Фронтенд: `CompanyView.vue` (роут `/company/:slug`).

### Оранжевая цена (скидка)

- Если `previous_price > price` — текущая цена показывается **оранжевым**, старая — с зачёркиванием.
- Применено в `ProductCard.vue` (grid + list) и на страницах EAN.

### Модели

- `Product`: `EAN` → `EAN`, добавлено `PreviousPrice`.
- `Company`: добавлены `ImportFolder`, `PriceSource`, `DescRu/Ua/Pl/En`, `HeroImage`, `IsVisible`.
- Новые типы: `PriceSourceConfig`, `AttrFieldMap`.

## 2025-08-06 — Монетизация: продвигаемые товары в результатах поиска

### Встраивание промо-товаров в каталог

- При запросе `/products` теперь учитываются активные промо-кампании.
- Логика:
  - Кампания активируется только если `TargetFilters` совпадают с текущим контекстом поиска (category_ids, attribute_filters).
  - Продвигаемые товары поднимаются в начало результатов (`target_position=top`).
  - В ответе у продвигаемых товаров флаг `promoted: true`.
- Поддержка:
  - Продвижение конкретных товаров через `product_ids` в кампании.
  - Продвижение всех товаров компании, соответствующих `TargetFilters` (если `product_ids` пустой).
- Логирование:
  - При каждом показе продвигаемого товара создаётся запись в `PromoLog` (event_type=impression).
  - CampaignID привязан к логу.

### API

- `GET /products` — в ответе у товаров появился флаг `promoted`.
- `POST /admin/promo/campaigns` — создание промо-кампаний админом.
- Модель `PromoCampaign` дополнена полем `product_ids`.

### Пример

- Кампания: `target_filters.category_ids=[3]`, `product_ids=[23]`, `target_position=top`
- Запрос: `GET /products?category_id=3`
- Результат: Samsung S24 (id=23) первый в списке с `promoted=true`.

## 2025-08-06 — Фильтрация по атрибутам: OR внутри / AND между

### Исправление фильтрации по атрибутам

- **Проблема**: при указании нескольких значений одного атрибута (например, `attr.color=red&attr.color=blue`) они фильтровались как AND (должен быть и красный, и синий одновременно), вместо OR (красный ИЛИ синий).
- **Решение**:
  - Внутри одного атрибута — **OR**: все значения объединяются в union (товар подходит, если совпадает хотя бы одно значение).
  - Между разными атрибутами — **AND**: пересечение наборов (товар должен подходить по всем указанным атрибутам).
  - Пример: `attr.color=red,blue&attr.size=L` → (color=red ИЛИ color=blue) И (size=L).
- Поддержка двух форматов запроса:
  - Повторяющиеся параметры: `attr.color=red&attr.color=blue`
  - Через запятую: `attr.color=red,blue`
- Добавлен helper `getIDIndex()` для чтения списков ID из индексов.
- Исправлен вызов отсутствовавшего метода `GetIndex` на `getIDIndex(r.store, key)`.

### API (GET /products)

- `attr.<code>` — теперь корректно поддерживает множественные значения с логикой OR внутри атрибута.
- `attr.<code>_min`, `attr.<code>_max` — диапазоны для числовых атрибутов (без изменений).

## 2025-08-06 — Оптимизация и кэширование v1.2

### L1 кэш (in-memory)

- Новый пакет `internal/cache` — SimpleCache (LRU-like с TTL).
- Интеграция с `Store`:
  - `Store.cache` — кэш до 10 000 записей.
  - Методы: `CacheGet`, `CacheSet`, `CacheDelete`, `CacheClear`.
- Кэширование в репозиториях:
  - **ProductRepo.Get**: кэш 5 мин, инвалидация при Update/Delete.
  - **CategoryRepo.Get**: кэш 10 мин, инвалидация при Update/Delete.
- Уменьшает нагрузку на makodb для горячих данных (товары, категории).

### Оптимизация индексов (замена ForEach-сканов)

- Введены глобальные индексы для всех сущностей:
  - `idx:all:users`, `idx:all:products`, `idx:all:orders`, `idx:all:payments`, `idx:all:promo_campaigns`
- Индексы создаются при Create, удаляются при Delete.
- Оптимизированы методы:
  - **UserRepo.GetAllUsers** — использует `idx:all:users` (fallback: scan)
  - **ProductRepo.GetAllProducts** — использует `idx:all:products` (fallback: scan)
  - **ProductRepo.List** — при отсутствии фильтров использует `idx:all:products`
  - **OrderRepo.GetAllOrders** — использует `idx:all:orders` (fallback: scan)
  - **PromoCampaignRepo.GetAllCampaigns** — использует `idx:all:promo_campaigns` (fallback: scan)
  - **PromoCampaignRepo.GetActiveCampaigns** — использует `idx:all:promo_campaigns`
  - **PaymentRepo.CleanupTimedOutPayments** — использует `idx:all:payments` (fallback: scan)
- Все сканы теперь имеют fallback — обратно совместимо с существующей БД.

### Профилирование

- Добавлены pprof-эндпоинты для отладки и анализа производительности:
  - `/debug/pprof/` — индекс
  - `/debug/pprof/profile?seconds=30` — CPU profile
  - `/debug/pprof/heap` — heap profile
  - `/debug/pprof/goroutine` — goroutines
  - `/debug/pprof/block`, `/debug/pprof/mutex` — contention
  - `/debug/pprof/trace` — execution trace
- Доступны во время разработки для анализа узких мест.

### Следующие шаги (по PLAN.md этап 8)

- L2 кэш (опционально: отдельный Redis или маппинг через makodb).
- Масштабирование makodb: репликация, если потребуется.

## 2025-08-06 — Добиваем админку v1.1

### Admin API (дополнено по API_DESIGN.md)

- **PATCH /admin/companies/{id}/verify** — верификация/блокировка компании (admin).
  - Body: `{"status": "verified" | "blocked"}`.
  - HandleAdminCompanyVerify в AuthHandlers.

- **GET /admin/analytics/overview** — общие метрики платформы (admin).
  - Возвращает: total_users, total_companies, total_products, total_orders, total_revenue, promo_revenue.
  - Реализовано через scan всех сущностей.

- **GET /admin/analytics/products** — популярные товары по заказам (admin).
  - Params: `limit` (default 10, max 100), `sort` (orders|revenue|views, default: orders).
  - Считает заказы и выручку по каждому товару из всех заказов (кроме cancelled/refunded).
  - views пока = 0 (требуется отдельный трекер просмотров).

- **GET /admin/analytics/search-queries** — популярные поисковые запросы (admin).
  - Пока возвращает stub: пустой список + note.
  - Требуется реализация логирования поисковых запросов.

- **GET /admin/promo/campaigns** — список всех промо-кампаний (admin).
  - Опциональный фильтр: `?status=active`.

- **PATCH /admin/promo/campaigns/{id}** — обновление кампании (admin).
  - Можно менять: status, target_filters, target_position, budget_total, start_at, end_at.

- **POST /admin/products/import** — массовый импорт товаров из JSON (admin).
  - Content-Type: multipart/form-data, поле `file`.
  - JSON: массив объектов Product (sku, name, price, category_id, attributes, ...).
  - Query param: `company_id` (обязателен).
  - Возвращает: `{"import_id": 1, "status": "processing"}`.

- **GET /admin/products/import/{id}** — статус импорта.
  - Возвращает: id, status (processing/completed/failed), total_lines, imported_count, skipped_count, errors[].

### Инфраструктура

- `ProductImportRepo` — новый репозиторий для управления импортом.
- `UserRepo.GetAllUsers()` — для аналитики.
- `ProductRepo.GetAllProducts()` — для аналитики.
- `PromoCampaignRepo.ListAll()` — алиас для GetAllCampaigns.
- Маршруты в main.go обновлены.

## 2025-08-06 — Отзывы, Faceted Search, Timeout Cleanup v1.0

### Отзывы (Reviews)

- Новый модуль отзывов по API_DESIGN.md:
  - **POST /products/{id}/reviews** — создание отзыва (auth required, buyer only).
    - Валидация: rating 1-5, только один отзыв на продукт от пользователя.
    - Ошибка ALREADY_REVIEWED (409) при повторной попытке.
  - **GET /products/{id}/reviews** — публичный список отзывов товара (пагинация).
  - **GET /reviews?user_id=...** — список своих отзывов (auth required).
- Хранение:
  - `review:<id>` — документ отзыва.
  - `idx:review:product:<product_id>` — список review_id по продукту.
  - `idx:review:user:<user_id>` — список review_id по пользователю.
- ReviewRepo: Create, Get, ListByProduct, ListByUser, Delete.
- Handlers обновлены: добавлен userRepo для проверки роли buyer.

### Faceted Search в каталоге

- Улучшение GET /products:
  - Ответ теперь включает `facets` (опционально):
    - `facets.brands`: { "brand_name": count, ... }
    - `facets.attrs`: { "attr_code": { "value": count, ... }, ... }
  - Для вычисления facet по атрибуту: добавить query param `attr_facet.<code>=1`.
    - Пример: `GET /products?attr_facet.color=1&attr_facet.size=1`
  - Facets вычисляются по отфильтрованному набору товаров (до сортировки и пагинации).
- ProductRepo:
  - Новый метод `ListWithFacets(params) (*ListResult, error)`.
  - Старый метод `List` теперь обёртка над `ListWithFacets` (backward compatible).
  - Типы: `ListResult`, `Facets`, `FacetCount`, обновлён `ListParams`.

### Timeout Cleanup для платежей

- Реализация POST /admin/payments/timeout-cleanup:
  - Сканирует все pending платежи старше `max_pending_minutes` (default: 30).
  - Для просроченных платежей:
    - payment.status → failed
    - order.status → cancelled (если order.status == new)
  - Возвращает детальный результат:
    - `checked_payments`, `timed_out_payments`, `cancelled_orders`, `details[]`.
- PaymentRepo:
  - Новый метод `CleanupTimedOutPayments(maxPendingMinutes int) (*TimeoutCleanupResult, error)`.
  - Хелперы: `GetOrderByID`, `UpdateOrderStatus` (для работы с заказами из PaymentRepo).

### Инфраструктура

- keys.go: добавлены `IndexKeyReviewProduct`, `IndexKeyReviewUser`.
- main.go: маршруты для `/products/{id}/reviews`, `/reviews`.
- Handlers: рефакторинг NewHandlers для переиспользования PromoCampaignRepo/PromoPlanRepo.

## 2025-08-06 — Промо-эффекты в поиске v0.9

- Промо-буст в каталоге:
  - Товары компаний с активными position-кампаниями поднимаются выше в результатах поиска GET /products.
  - Boost применяется до сортировки (promoted → non-promoted, затем sort).
  - Поддерживается через PromoPlanTypePosition.
- ProductRepo теперь зависит от PromoCampaignRepo и PromoPlanRepo.
- PromoCampaignRepo.GetActiveCampaigns() — получение активных кампаний (status=active, end_at >= now).

## 2025-08-06 — Промо и продвижение v0.8

- Promo plans (тарифы продвижения):
  - GET /promo/plans — публичный список тарифов.
  - POST /admin/promo-plans — создание тарифа (admin).
  - PATCH /admin/promo-plans/{id} — обновление тарифа (admin).
- Promo campaigns (кампании):
  - POST /companies/{companyId}/promo-campaigns — seller создаёт кампанию (своя компания).
  - GET /companies/{companyId}/promo-campaigns — список кампаний компании (seller/admin).
  - PATCH /promo-campaigns/{id}/status — смена статуса (seller/admin).
  - Статусы: pending → active → expired/cancelled.
  - Автоматический расчёт end_at по duration_days плана.
- Promo logs (события):
  - POST /promo/logs — регистрация события (impression/click/conversion).
  - Обновляет campaign.budget_used при cost > 0.
  - Только для активных кампаний.
- Репозитории: PromoPlanRepo, PromoCampaignRepo, PromoLogRepo.
- Исправлен баг с ListAll: неверная длина префикса key в ForEach.

## 2025-08-06 — Seller по заказам и аналитика v0.7

- Заказы компании:
  - GET /companies/{companyId}/orders — список заказов, содержащих товары компании.
  - Фильтры: status, page, limit.
  - Доступ: seller (только своя компания) или admin.
- Аналитика заказов:
  - GET /admin/analytics/orders — агрегаты по всем заказам.
  - Возвращает: total_orders, total_revenue, by_status, by_payment_status.
  - Доступ: admin only.
- OrderRepo:
  - GetOrdersByCompanyID(companyID) — поиск заказов по компании (scan-based).
  - GetAllOrders() — для аналитики.

## 2025-08-06 — Платежи: webhook, возврат, таймауты v0.6

- Webhook от платёжных провайдеров:
  - POST /payments/webhook/{provider} — обработка уведомлений.
  - Поддерживаемые статусы: paid/succeeded → PaymentStatusPaid, failed/declined → PaymentStatusFailed.
  - При failed — заказ автоматически отменяется (order.status → cancelled).
  - Идемпотентность: повторный webhook с тем же статусом игнорируется.
  - Заглушка верификации подписи через header X-Webhook-Signature.
- Возврат средств (refund):
  - POST /payments/{id}/refund — полный возврат (admin only).
  - payment.status → refunded, order.payment_status → refunded, order.status → refunded.
  - Нельзя вернуть уже refunded или неоплаченный платёж.
- Таймауты заказов:
  - POST /admin/payments/timeout-cleanup — эндпоинт для отмены заказов с просроченными платежами.
  - Пока stub (реализация сканирования по created_at — в планах).
- /payments/{id}/refund и /payments/{id}/confirm теперь через OptionalAuth.

## 2025-08-06 — Улучшение заказов v0.5

- Создание заказа из корзины:
  - POST /orders поддерживает cart_id: `{"cart_id": "...", "shipping_info": {...}}`.
  - Items берутся из корзины, корзина после создания заказа очищается.
  - Если cart_id не указан — работает в ручном режиме (items в теле запроса).
- Валидации при создании заказа:
  - Товар должен существовать.
  - Товар должен быть в статусе active.
  - stock_qty >= qty (ошибка INSUFFICIENT_STOCK).
- Контроль доступа к заказам:
  - GET /orders/{id}: только владелец заказа или admin.
  - PATCH /orders/{id}/status:
    - admin: любой статус для любого заказа.
    - seller: может менять статус заказов, содержащих его товары (по company_id).
    - buyer: может только отменить свой заказ в статусе "new" (→ cancelled).
- Маршруты /orders/{id} и /orders/{id}/status теперь проходят через OptionalAuth middleware.

## 2025-08-06 — Корзина, заказы, платежи v0.4

- Реализованы корзина (Cart), заказы (Order) и платежи (Payment):

### Корзина (Cart)

- **POST /cart** — создание корзины:
  - Тело: `{"user_id": <optional>, "session_id": "<optional>"}`.
  - С auth: user_id берётся из токена (переопределяет тело).
  - Возвращает cart.id (UUID-подобный).
- **GET /cart/{id}** — получение корзины по ID.
- **GET /cart/me** — корзина текущего пользователя (requires auth).
- **POST /cart/{id}/items** — добавление товара:
  - Тело: `{"product_id": 123, "qty": 2}`.
  - Цена берётся из текущего продукта (snapshot).
  - Если товар уже есть — qty суммируется.
- **PATCH /cart/{id}/items/{product_id}** — изменение количества:
  - Тело: `{"qty": 5}` (qty=0 — удаление товара).
- **DELETE /cart/{id}** — удаление корзины.
- Хранение:
  - `cart:<id>` — документ корзины.
  - `cart:user:<user_id>` → cart_id (индекс по пользователю).
  - `cart:session:<session_id>` → cart_id (индекс для гостевых корзин).

### Заказы (Order)

- **POST /orders** — создание заказа:
  - Тело: `{"user_id": <optional>, "items": [...], "shipping_info": {...}, "comment": "..."}`.
  - user_id из auth или из тела.
  - items: `{"product_id": 123, "qty": 2, "price": 1500}` (snapshot цен).
  - company_id берётся автоматически из продукта.
  - Статус: new, payment_status: pending.
- **GET /orders?user_id=...** — список заказов пользователя:
  - С auth можно без user_id (берётся из токена).
- **GET /orders/{id}** — получение заказа.
- **PATCH /orders/{id}/status** — смена статуса:
  - Тело: `{"status": "confirmed"}`.
- Хранение:
  - `order:<id>` — документ заказа.
  - `order:user:<user_id>` → order_id[] (индекс по пользователю).

### Платежи (Payment) — минимально

- **POST /payments?order_id=...** — создание платежа (заглушка):
  - Тело: `{"method": "card"}`.
  - Возвращает payment_url (фейковый).
- **POST /payments/{id}/confirm** — подтверждение оплаты:
  - Обновляет payment.status → paid.
  - Обновляет order.payment_status → paid.
- **GET /payments/{id}** — получение платежа.
- Хранение:
  - `payment:<id>` — документ платежа.
  - `payment:order:<order_id>` → payment_id (индекс по заказу).

### Слияние корзин при логине

- При POST /auth/login: если передан session_id (header X-Session-ID или query session_id) и по нему найдена гостевая корзина, выполняется CartRepo.AssignToUser:
  - Если у пользователя уже есть корзина — товары гостевой корзины сливаются (qty суммируются).
  - Если корзины нет — гостевая корзина привязывается к пользователю (user_id устанавливается, session_id очищается).

### Исправления

- **PasswordHash не сохранялся**: тег `json:"-"` в model.User приводил к тому, что bcrypt-хеш не попадал в JSON при сериализации. Исправлено через MarshalUser/UnmarshalUser с промежуточным struct, включающим password_hash.
- Тестирование на makoshop_db/ (16 шардов):
  - Cart: create, add/update/delete items, /cart/me — работает.
  - Order: create, list, get, update status — работает.
  - Payment: create, confirm, get + update order.payment_status — работает.
  - Каталог GET /products не сломан.
  - Login с bcrypt работает корректно.

## 2025-08-06 — Авторизация и роли v0.3

- Реализована авторизация и система ролей (buyer/seller/admin):
  - POST /auth/register: регистрация с email, password, role, profile.
  - POST /auth/login: вход, выдача JWT (24h, HS256, claims: sub=user_id, email, role, exp, iat).
  - Пароль: хеширование bcrypt.
  - Хранение: user:<id>, auth:user:email:<email> → user_id.
  - GET /auth/me: текущий пользователь (requires auth).
  - PATCH /users/me: обновление своего профиля (requires auth).
- Middleware:
  - RequireAuth: проверка JWT, установка ContextUser в контекст запроса.
  - RequireRole: проверка JWT + роли (например, admin-only endpoints).
  - OptionalAuth: если JWT валиден — ContextUser в контекст; если нет — продолжает без ошибки.
- Admin endpoints (role=admin):
  - GET /admin/users: список пользователей с фильтрами (role, status, q) и пагинацией.
  - GET/PATCH /admin/users/{id}: просмотр и обновление пользователя.
  - GET/POST /admin/companies: список и создание компаний.
  - GET/PATCH /admin/companies/{id}: просмотр и обновление компании.
- Привязка товаров к seller:
  - POST /products: если запрос от seller (через OptionalAuth), company_id берётся из его компании (GetCompanyIDByUserID). Seller не может создать товар для чужой компании.
  - PATCH /products/{id}: обернут в OptionalAuth. Если запрос от seller — проверяется ownership по company_id (403 FORBIDDEN для чужих товаров). Без токена: разрешено для тестов/admin.
  - DELETE /products/{id}: аналогично PATCH — seller может удалять только свои товары.
- Исправлена проблема ownership-проверки:
  - Раньше PATCH/DELETE /products/{id} не проходили через middleware, ContextUserFrom(r) возвращал nil, seller мог менять/удалять чужие товары.
  - Теперь /products/{id} полностью обрабатывается через OptionalAuth, ContextUser доступен в хендлерах.
- Тестирование на сервере:
  - Seller создаёт товар → company_id его компании.
  - Seller PATCH чужой товар → 403 FORBIDDEN.
  - Seller DELETE чужой товар → 403 FORBIDDEN.
  - Seller PATCH/DELETE свой товар → успех.
  - Без токена PATCH/DELETE → работает (для тестов/admin).
  - Каталог GET /products и его индексы не сломаны.

## 2025-08-06 — Каталог v0.2

- Реализован полноценный GET /products (каталог товаров) по API_DESIGN.md:
  - Полнотекстовый поиск: параметр `q` (по name, description, текстовым атрибутам через makodb.Search/IndexMultiworld).
  - Фильтры:
    - `category_id` — по категории (idx:category:<id>).
    - `company_id` — по компании (idx:company:<id>).
    - `brand` — по бренду (idx:brand:<...>).
    - `price_min`, `price_max` — диапазон цен (пост-фильтрация).
    - `attr.<code>` — точное совпадение по строковому атрибуту (idx:gattr:<code>:<value>).
    - `attr.<code>_min`, `attr.<code>_max` — диапазон для числовых атрибутов (пост-фильтрация).
  - Сортировка (`sort`):
    - `price`, `price_desc` — по цене.
    - `created_at`, `created_at_desc` — по дате создания.
    - `attr.<code>`, `attr.<code>_desc` — по числовому атрибуту.
  - Пагинация: `page`, `limit` (def=50, max=200); ответ: page, limit, total, items.
  - Пересечение фильтров: индексы пересекаются через intersectFilterConditions.
- Индексация товаров улучшена:
  - Полнотекстовый поиск через `IndexMultiworld` (токенизация по словам).
  - Добавлены глобальные индексы атрибутов `idx:gattr:<code>:<value>` для фильтрации без category_id.
  - Сортировочные индексы: `sort:product:price`, `sort:product:created_at` (comma-separated value:id).
  - Удаление из индексов при обновлении/удалении товара (removeProductIndexes).
- Обновлены репозитории и хендлеры:
  - ProductRepo.List(params) — основной метод поиска.
  - Handlers.HandleProductsList — парсинг всех query-параметров.
- Тестирование на сервере:
  - Поиск по q, фильтрация по brand/price/attr, сортировка, пагинация — работают.
  - Комбинации (q + brand + price_range + sort) — работают.
  - Нет паник при типичных запросах.

## 2025-08-06 — Ядро v0.1

- Создана структура проекта:
  - cmd/server — HTTP-сервер API.
  - internal/db — слой доступа к makodb, ключи, репозитории.
  - internal/model — доменные модели.
  - internal/api — HTTP-хендлеры.
  - pkg/config — конфигурация.
- Подключён makodb v2.1.12 через локальный replace.
- Реализованы сущности и CRUD:
  - Category (список, создание, получение по id, обновление, удаление).
  - AttributeDefinition (создание, обновление, удаление, список по категории).
  - Product (создание, получение по id, обновление, удаление; базовая индексация по категории, компании, бренду, атрибутам и полнотекстовый поиск).
- Сериализация: стандартный encoding/json (устранены паники silentjson).
- Проверено:
  - /health — работает.
  - POST /admin/categories + GET /categories — работает.
  - POST /products + GET /products/{id} — работает.
- Порт по умолчанию: 9090 (настраивается в pkg/config).
