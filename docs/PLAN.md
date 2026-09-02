# Makoshop — План разработки

## 1. Ключевые требования

- Огромный каталог товаров, много категорий, у каждой — свой набор параметров.
- Гибкий поиск, фильтрация по множеству параметров и их пересечениям.
- Сортировки по разным полям, включая пользовательские параметры.
- Авторизация пользователей.
- Админка: управление товарами, категориями, пользователями, монетизацией.
- Корзина и оформление заказов.
- Монетизация: платное продвижение выборок товаров/компаний под заданные фильтры (не только по цене).

Это классический B2B/B2C-маркетплейс с очень гибкой структурой товаров и рекламными слотами в результатах поиска.

## 2. Концепция хранения на базе makodb

Исходя из демо-кода makodb:

- Это sharded key-value хранилище с:
  - быстрым Get/Put/Delete,
  - токенизируемым полнотекстовым поиском (Search, Index),
  - возможностью хранить произвольные индексы как отдельные key-value записи.
- В демо используются:
  - документы: tx:<id>,
  - инвертированные индексы: idx:<token>, idx:item_type:<val>, idx:country:<val>,
  - сортировочные индексы: sort:<field> — массивы ID, отсортированных по полю.

Для makoshop будем использовать ту же философию:

- Все сущности — как JSON-документы с предсказуемыми ключами.
- Индексы (фильтры, сортировки, поиск, админка) — как отдельные записи в makodb.
- Сложную логику (транзакции, авторизация, бизнес-правила) выносим в приложение (Go-сервис), makodb — как хранилище + индексы.

## 3. Сущности (Domain Model)

### 3.1. Основные

1. User — пользователь системы
   - id
   - email / phone
   - password_hash
   - role: admin, seller, buyer, moderator
   - status: active, blocked, pending
   - created_at, updated_at
   - profile: name, company_name, contact_info, address

2. Company — компания-продавец (опционально отдельная от User, но полезно)
   - id
   - name
   - legal_info (INN, ОГРН и т.п.)
   - status: verified, pending, blocked
   - settings: currency, vat_enabled, default_payment_terms
   - owner_user_id
   - created_at, updated_at

3. Category — категория товаров
   - id
   - parent_id (для иерархии)
   - name
   - slug
   - description
   - is_active
   - sort_order
   - created_at, updated_at

4. AttributeDefinition — шаблон параметра категории
   - id
   - category_id
   - name (например: “Мощность”, “Цвет”, “Материал”)
   - code (например: “power”, “color”, “material”)
   - type: string, int, float, bool, enum, multi_enum, date, range
   - options (для enum/multi_enum)
   - is_required
   - is_filterable (участвует в фильтрации на портале)
   - is_sortable (можно сортировать по нему)
   - is_searchable (участвует в полнотекстовом поиске)
   - sort_order
   - created_at, updated_at

5. Product — товар
   - id
   - ean
   - name
   - description
   - category_id
   - company_id
   - brand (опционально отдельная сущность)
   - price (базовая цена)
   - currency
   - stock_qty (если нужно; для B2B иногда заменяется на “по запросу”)
   - status: draft, active, hidden, archived
   - attributes: map[attribute_code]value (JSON-объект)
   - images: [url]
   - seo: title, description, keywords
   - created_at, updated_at

6. ProductPriceRule (опционально, если нужна сложная ценовая политика)
   - id
   - product_id
   - condition: min_qty, customer_group, region, period
   - price
   - currency
   - valid_from, valid_to

7. Session / Token — сессии / JWT-токены (если хранить в makodb)
   - id / token_hash
   - user_id
   - expires_at
   - created_at

8. Cart — корзина
   - id
   - user_id (или session_id для гостей)
   - items: [{product_id, qty, price_at_add}]
   - created_at, updated_at

9. CartItem — элемент корзины (можно встроить в Cart, но логически отдельная)
   - cart_id
   - product_id
   - qty
   - price_snapshot

10. Order — заказ
    - id
    - user_id
    - status: new, confirmed, processing, shipped, delivered, cancelled, refunded
    - items: [{product_id, company_id, qty, price}]
    - total_amount
    - currency
    - payment_status
    - shipping_info
    - created_at, updated_at

11. OrderItem
    - order_id
    - product_id
    - company_id
    - qty
    - price
    - total

12. Payment
    - id
    - order_id
    - amount
    - currency
    - method: card, invoice, bank_transfer, etc.
    - status: pending, paid, failed, refunded
    - external_payment_id
    - created_at

13. Review (опционально, но часто нужно)
    - id
    - product_id
    - user_id
    - rating
    - comment
    - created_at

### 3.2. Монетизация и продвижение

14. PromotionPlan — тарифный план продвижения
    - id
    - name (например: “Базовое продвижение”, “Премиум”, “Топ-3 в категории”)
    - type: position, highlight, banner, filter_boost
    - duration_days
    - price
    - currency
    - description
    - constraints (например: max_positions, applicable_categories)

15. PromotionCampaign — активная кампания продвижения
    - id
    - company_id
    - promotion_plan_id
    - status: active, paused, expired, cancelled
    - target_filters: JSON
      - category_ids
      - attribute_filters (например: {power:[1000,5000], brand:["X"]})
    - target_position: top, top_3, sidebar, inline_N
    - budget_used, budget_total
    - start_at, end_at
    - created_at

16. PromotionLog — лог показов/кликов (для аналитики и биллинга)
    - id
    - campaign_id
    - event_type: impression, click, conversion
    - context: JSON (search_query, filters, page_url)
    - cost
    - created_at

## 4. Структура ключей в makodb

Примеры ключей (предлагаемая конвенция):

- Документы
  - user:<id>
  - company:<id>
  - category:<id>
  - attrdef:<id>
  - product:<id>
  - cart:<id>
  - order:<id>
  - payment:<id>
  - review:<id>
  - promo_plan:<id>
  - promo_campaign:<id>
  - promo_log:<id>

- Индексы поиска и фильтрации
  - idx:attr:<category_id>:<attr_code>:<value> → список product_id
  - idx:brand:<brand> → product_id[]
  - idx:company:<company_id> → product_id[]
  - idx:category:<category_id> → product_id[]
  - idx:search:<token> → product_id[] (токены из name/description/attributes для полнотекстового поиска)

- Сортировочные индексы
  - sort:product:price → product_id[] (сортировка по цене)
  - sort:product:created_at → product_id[]
  - sort:product:attr:<category_id>:<attr_code> → product_id[] (для числовых/enum атрибутов)

- Монетизация и продвижение
  - promo:active → campaign_id[] (быстрый доступ к активным кампаниям)
  - promo:target:category:<category_id> → campaign_id[]
  - promo:target:filter:<hash> → campaign_id[] (для быстрого поиска по набору фильтров)

- Состояние и метаданные
  - state:next_id:<entity> → int64 (или использовать автоинкремент в приложении)
  - state:config → JSON с общими настройками
  - auth:session:<token_hash> → session data
  - auth:user:email:<email> → user_id
  - auth:user:phone:<phone> → user_id

##  5. Архитектура приложения makoshop

Рекомендую:

- Backend: Go-микро-сервис (или монолит на Go), работающий поверх makodb.
- API: REST + (опционально) GraphQL для сложных запросов к каталогу.
- Frontend:
  - Публичный портал: SPA (React/Vue) или SSR (Next.js/Nuxt).
  - Админка: отдельный SPA или внутри того же фронтенда с ролями.
- Авторизация: JWT (stateless), с возможностью blacklisting через makodb при необходимости.
- Индексация:
  - При создании/обновлении товара:
    - сохранить документ product:<id>;
    - обновить инвертированные индексы по атрибутам, бренду, категории, компании;
    - обновить сортировочные индексы (price, created_at и т.п.).

## 6. Поиск, фильтрация и сортировка

### 6.1. Полнотекстовый поиск
- При индексации товара:
  - токенизируем name, description, текстовые атрибуты;
  - для каждого токена: idx:search:<token> → добавляем product_id.
- При поиске:
  - db.Search(query) → пересечение/объединение по токенам;
  - ранжируем по релевантности (частота токенов, позиция, бусты от промо).

### 6.2. Фильтрация по атрибутам и пересечениям
- Для каждого filter:
  - читаем idx:attr:<category_id>:<attr_code>:<value> → product_id[];
- Пересечение списков (как в демо makodb: intersectSlices) → candidate product_ids.
- Для диапазонов (range):
  - используем сортировочный индекс sort:product:attr:<category_id>:<attr_code> и бинарный поиск по границам.

### 6.3. Сортировка
- Для стандартных полей: используем sort:product:<field>.
- Для атрибутов: sort:product:attr:<category_id>:<attr_code>.
- Алгоритм:
  - берём отфильтрованный набор candidate_ids (или их подмножество);
  - если их мало (< N) — сортируем в памяти;
  - если много — используем merge по сортировочному индексу (как в демо: streaming merge).

### 6.4. Продвижение (монетизация)
- При каждом поиске/фильтрации:
  - строим filter_context (категория, выбранные атрибуты, ключевые слова);
  - ищем кампании:
    - promo:target:category:<category_id>;
    - promo:target:filter:<hash(filter_context)>;
  - проверяем совпадение target_filters кампании с текущим контекстом;
  - вставляем продвигаемые товары в результат согласно target_position и правилам приоритета.
- Логирование:
  - для каждого показа/клика продвигаемого товара записываем promo_log:<id>;
  - обновляем budget_used в promo_campaign:<id>.

## 7. Админка

Функционал админки:

- Управление пользователями и компаниями (роль, статус, верификация).
- Управление категориями и атрибутами:
  - CRUD категорий;
  - привязка AttributeDefinition к категориям;
  - настройка filterable/sortable/searchable.
- Управление товарами:
  - просмотр, редактирование, модерация;
  - массовые операции (CSV-импорт/экспорт).
- Монетизация:
  - управление PromotionPlan;
  - просмотр и управление PromotionCampaign;
  - аналитика: показы, клики, расходы.
- Аналитика портала:
  - популярные запросы, товары, категории;
  - конверсии, заказы.

## 8. Корзина и заказы

- Корзина:
  - хранится в cart:<id>;
  - при добавлении товара: сохраняем snapshot цены;
  - поддержка гостевых корзин (по session_id), слияние при логине.
- Оформление заказа:
  - создание order:<id> из корзины;
  - перевод статусов: new → confirmed → ...;
  - интеграция с платёжными системами (Payment).

## 9. План разработки (по этапам)

### Этап 0: Подготовка
- Инициализировать проект makoshop (Go-сервис + структура).
- Подключить makodb как библиотеку.
- Определить конвенции ключей, сериализацию (JSON через silentjson или аналог).
- Настроить базовую конфигурацию (shards, размеры, пути).

### Этап 1: Базовая модель данных и CRUD
- Реализовать сущности:
  - User, Company, Category, AttributeDefinition, Product.
- REST API:
  - CRUD для категорий и атрибутов;
  - CRUD для товаров (с привязкой к категории и атрибутам);
  - CRUD для компаний и пользователей (пока без полной авторизации).
- Индексация товаров:
  - инвертированные индексы по атрибутам, категории, бренду;
  - базовые сортировочные индексы (price, created_at).

### Этап 2: Поиск, фильтрация, сортировка
- Полнотекстовый поиск по name/description/attributes.
- Фильтрация по:
  - категории;
  - атрибутам (точное совпадение, enum, multi_enum);
  - диапазонам (числовые атрибуты, цена).
- Сортировка по:
  - цене;
  - дате;
  - числовым/enum атрибутам.
- Пагинация и производительность:
  - оптимизировать пересечения индексов;
  - использовать сортировочные индексы для больших наборов.

### Этап 3: Авторизация и роли
- Регистрация и вход (email/password).
- JWT-токены, middleware проверки.
- Роли: admin, seller, buyer.
- Ограничение доступа:
  - seller управляет только своими товарами;
  - admin — всем.

### Этап 4: Корзина и заказы
- Корзина (гостевая и привязанная к user).
- Оформление заказа:
  - создание Order из Cart;
  - базовые статусы.
- Интеграция с платёжной системой (минимальная):
  - Payment-сущность, статусы.

### Этап 5: Монетизация и продвижение
- Реализовать PromotionPlan и PromotionCampaign.
- API для:
  - создания кампании по фильтру и позиции;
  - управления бюджетом и статусом.
- Встраивание продвигаемых товаров в результаты поиска:
  - логика target_filters и target_position;
  - приоритеты и лимиты.
- Логирование промо-событий и обновление бюджета.

### Этап 6: Админка
- Web-интерфейс:
  - управление пользователями, компаниями;
  - категории и атрибуты;
  - товары и модерация;
  - промо-планы и кампании;
  - базовая аналитика.
- Интеграция с бэкендом через API.

### Этап 7: Публичный портал
- Каталог с поиском, фильтрами, сортировками.
- Карточка товара.
- Корзина и оформление заказа.
- Личный кабинет покупателя и продавца.
- SEO и производительность.

### Этап 8: Оптимизация и масштабирование
- Анализ нагрузок и узких мест.
- Оптимизация индексов (структура ключей, выборочная индексация).
- Кэширование (L1 в памяти, L2 — через makodb или отдельный кэш).
- Масштабирование makodb-инстансов (sharding, репликация, если нужно).

## 10. Статус и следующие шаги

### Выполнено (v0.2 — Каталог)

- План и API-дизайн зафиксированы в docs/PLAN.md и docs/API_DESIGN.md.
- Инициализирован Go-проект, подключён makodb v2.1.12.
- Реализованы:
  - модели User, Company, Category, AttributeDefinition, Product, Cart, Order, Payment, Review, PromoPlan, PromoCampaign, PromoLog;
  - layer db (Store, ключи, сериализация, репозитории Category/AttrDef/Product);
  - HTTP-эндпоинты для категорий, атрибутов и товаров (CRUD);
  - **полноценный каталог GET /products**:
    - полнотекстовый поиск (q),
    - фильтры: category_id, company_id, brand, price_min/max, attr.<code>, attr.<code>_min/max,
      - attr.<code> поддерживает множественные значения: OR внутри одного атрибута, AND между разными атрибутами;
      - форматы: attr.color=red&attr.color=blue или attr.color=red,blue;
    - сортировка: price, price_desc, created_at, created_at_desc, attr.<code>,
    - пагинация: page, limit.
  - Индексация товаров:
    - полнотекстовый поиск через IndexMultiworld,
    - индексы: idx:category, idx:company, idx:brand, idx:attr, idx:gattr,
    - сортировочные индексы: sort:product:price, sort:product:created_at,
    - удаление из индексов при update/delete.
- Изменения зафиксированы в docs/CHANGELOG.md.

### Следующие шаги (по приоритету)

1) Авторизация и роли: ✅ выполнено
   - /auth/register, /auth/login, JWT, middleware, привязка товаров к seller.
2) Корзина и заказы: ✅ выполнено
   - Cart + Order + базовый Payment.
3) Монетизация: ✅ выполнено
   - PromotionPlan + PromotionCampaign + встраивание продвигаемых товаров в результаты поиска.
   - Продвигаемые товары помечаются `promoted=true` и поднимаются в начало.
4) Админка и аналитика: ✅ бэкенд готов
   - эндпоинты для управления и метрик (по API_DESIGN.md).
5) Дерево категорий: ✅ выполнено
   - `GET /categories/tree` — полное дерево категорий.
   - `GET /categories/tree?child_of={id}` — поддерево от указанной категории.
   - Рекурсивная структура с `children`, сортировка по `sort_order`/`name`.
6) Публичный портал (UI):
   - доводка фронтенда: навигация по дереву категорий, каталог с промо-маркировкой, корзина, оформление заказа.
7) Оптимизация и масштабирование:
   - индексы, кэширование, производительность на больших данных.
8) Интеграционные тесты и нагрузка:
   - проверка на реальных объёмах данных.
