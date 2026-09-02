# Makoshop — API Design

## Общие правила

- База: `https://api.makoshop.com/v1`
- Все запросы и ответы в JSON.
- Аутентификация:
  - Публичные эндпоинты: без токена.
  - Защищённые: `Authorization: Bearer <JWT>`.
- Пагинация (для списков):
  - `page` (int, default=1)
  - `limit` (int, default=50, max=200)
  - Ответ содержит: `page`, `limit`, `total`, `items`.
- Статусы ошибок: стандартные HTTP-коды + JSON:
  - `{ "error": { "code": "NOT_FOUND", "message": "..." } }`

---

## 1. Auth & Users

### 1.1. Регистрация пользователя

- POST /auth/register
- Body:
  - email (string)
  - password (string)
  - role (string): buyer | seller (default: buyer)
  - profile: { name, phone, company_name } (опционально)
- Response 201:
  - user_id
  - email
  - role
  - token (JWT)

### 1.2. Вход

- POST /auth/login
- Body:
  - email
  - password
- Response 200:
  - user_id
  - email
  - role
  - token

### 1.3. Текущий пользователь

- GET /auth/me
- Auth: required
- Response 200:
  - id
  - email
  - role
  - status
  - profile

### 1.4. Обновление профиля

- PATCH /users/me
- Auth: required
- Body: partial profile (name, phone, company_name, address и т.п.)
- Response 200: обновлённый user

### 1.5. Список пользователей (админ)

- GET /admin/users
- Auth: admin
- Params: page, limit, role, status, q (поиск по email/name)
- Response 200: paginated list of users

### 1.6. Получить пользователя (админ)

- GET /admin/users/{id}
- Auth: admin
- Response 200: user

### 1.7. Обновить пользователя (админ)

- PATCH /admin/users/{id}
- Auth: admin
- Body: role, status, profile
- Response 200: user

---

## 2. Companies

### 2.1. Создать компанию (seller)

- POST /companies
- Auth: seller
- Body:
  - name
  - legal_info: { inn, ogrn, country, city, address }
  - settings: { currency, vat_enabled }
- Response 201:
  - id
  - name
  - status: pending
  - owner_user_id

### 2.2. Получить компанию

- GET /companies/{id}
- Auth: optional (публичная базовая информация)
- Response 200: company (без чувствительных полей для гостей)

### 2.3. Список компаний

- GET /companies
- Params: page, limit, status, q
- Response 200: paginated list

### 2.4. Верификация компании (админ)

- PATCH /admin/companies/{id}/verify
- Auth: admin
- Body: status: verified | blocked
- Response 200: company

---

## 3. Categories

### 3.1. Список категорий

- GET /categories
- Params:
  - parent_id (для иерархии)
  - active_only (bool, default=true)
  - page, limit
- Response 200:
  - items: [{ id, parent_id, name, slug, is_active, sort_order }]

### 3.2. Получить категорию

- GET /categories/{id}
- Response 200: category + attributes definitions (список)

### 3.3. Создать категорию (админ)

- POST /admin/categories
- Auth: admin
- Body:
  - parent_id (nullable)
  - name
  - slug
  - description
  - is_active
  - sort_order
- Response 201: category

### 3.4. Обновить категорию (админ)

- PATCH /admin/categories/{id}
- Auth: admin
- Body: partial category
- Response 200: category

### 3.5. Удалить категорию (админ)

- DELETE /admin/categories/{id}
- Auth: admin
- Response 204

---

## 4. Attribute Definitions

### 4.1. Список атрибутов категории

- GET /categories/{categoryId}/attributes
- Response 200:
  - items: [{ id, name, code, type, options, is_required, is_filterable, is_sortable, is_searchable, sort_order }]

### 4.2. Создать атрибут (админ)

- POST /admin/categories/{categoryId}/attributes
- Auth: admin
- Body:
  - name
  - code
  - type: string|int|float|bool|enum|multi_enum|date|range
  - options (для enum/multi_enum)
  - is_required
  - is_filterable
  - is_sortable
  - is_searchable
  - sort_order
- Response 201: attributeDefinition

### 4.3. Обновить атрибут (админ)

- PATCH /admin/attributes/{id}
- Auth: admin
- Body: partial
- Response 200: attributeDefinition

### 4.4. Удалить атрибут (админ)

- DELETE /admin/attributes/{id}
- Auth: admin
- Response 204

---

## 5. Products

### 5.1. Каталог (поиск + фильтрация + сортировка)

- GET /products
- Params:
  - q (string): полнотекстовый поиск
  - category_id (string)
  - company_id (string)
  - brand (string)
  - price_min, price_max (float)
  - attr.<code> (string/array): фильтр по атрибуту
  - attr.<code>_min, attr.<code>_max (для range)
  - sort (string): price|price_desc|created_at|created_at_desc|attr.<code>|attr.<code>_desc
  - page, limit
- Response 200:
  - page, limit, total
  - items: [{ id, sku, name, category_id, company_id, brand, price, currency, status, attributes, images }]
  - filters: { available_attributes: [...] } (опционально, для UI)
  - promoted: [{ product_id, campaign_id, position }] (опционально, для фронтенда)

### 5.2. Получить товар

- GET /products/{id}
- Response 200: full product details

### 5.3. Создать товар (seller)

- POST /products
- Auth: seller
- Body:
  - EAN
  - name
  - description
  - category_id
  - brand
  - price
  - currency
  - stock_qty
  - attributes: { code: value, ... }
  - images: [url]
  - seo: { title, description, keywords }
- Response 201: product (status: draft или active по правилам)

### 5.4. Обновить товар (seller/admin)

- PATCH /products/{id}
- Auth: seller (только свои) или admin
- Body: partial product
- Response 200: product

### 5.5. Удалить товар (seller/admin)

- DELETE /products/{id}
- Auth: seller (свои) или admin
- Response 204

### 5.6. Список товаров компании (seller/admin)

- GET /companies/{companyId}/products
- Auth: seller (своя компания) или admin
- Params: page, limit, status, q
- Response 200: paginated products

### 5.7. Массовый импорт товаров (seller/admin)

- POST /admin/products/import
- Auth: admin (или seller в дальнейшем)
- Content-Type: multipart/form-data
- Body: file (CSV/JSON)
- Response 202:
  - import_id
  - status: processing

- GET /admin/products/import/{importId}
- Response 200:
  - status: processing|completed|failed
  - imported_count
  - errors

---

## 6. Cart

### 6.1. Получить корзину

- GET /cart
- Auth: optional (для гостей — по session/cookie; для авторизованных — по user_id)
- Response 200:
  - id
  - items: [{ product_id, product_name, qty, price, total }]
  - total_amount
  - currency

### 6.2. Добавить товар в корзину

- POST /cart/items
- Auth: optional
- Body:
  - product_id
  - qty
- Response 200: updated cart

### 6.3. Обновить количество

- PATCH /cart/items/{itemId}
- Auth: optional
- Body:
  - qty
- Response 200: updated cart item

### 6.4. Удалить из корзины

- DELETE /cart/items/{itemId}
- Auth: optional
- Response 204

### 6.5. Очистить корзину

- DELETE /cart
- Auth: optional
- Response 204

---

## 7. Orders

### 7.1. Создать заказ из корзины

- POST /orders
- Auth: required
- Body:
  - shipping_info: { name, phone, email, address, city, country, postal_code }
  - payment_method: card|invoice|bank_transfer
  - comment (optional)
- Response 201:
  - id
  - status: new
  - items
  - total_amount
  - currency
  - payment_status

### 7.2. Получить заказ

- GET /orders/{id}
- Auth: required (владелец или admin)
- Response 200: order details

### 7.3. Список заказов пользователя

- GET /orders
- Auth: required
- Params: page, limit, status
- Response 200: paginated orders

### 7.4. Список заказов компании (seller)

- GET /companies/{companyId}/orders
- Auth: seller (своя) или admin
- Params: page, limit, status
- Response 200: paginated orders

### 7.5. Обновить статус заказа (seller/admin)

- PATCH /orders/{id}/status
- Auth: seller (свои) или admin
- Body:
  - status: confirmed|processing|shipped|delivered|cancelled|refunded
- Response 200: order

---

## 8. Payments

### 8.1. Создать платёж (инициация)

- POST /orders/{orderId}/payments
- Auth: required
- Body:
  - method: card|invoice|bank_transfer
  - amount (optional, обычно = order.total_amount)
- Response 201:
  - id
  - order_id
  - status: pending
  - payment_url / invoice_data (зависит от метода)

### 8.2. Получить платёж

- GET /payments/{id}
- Auth: required (владелец заказа или admin)
- Response 200: payment

### 8.3. Webhook от платёжной системы

- POST /payments/webhook/{provider}
- Auth: provider signature
- Body: provider-specific payload
- Response 200: { ok: true }

---

## 9. Promotion & Monetization

### 9.1. Список тарифных планов продвижения

- GET /promo/plans
- Auth: optional (public catalog of plans)
- Response 200:
  - items: [{ id, name, type, duration_days, price, currency, description, constraints }]

### 9.2. Создать кампанию продвижения (seller)

- POST /promo/campaigns
- Auth: seller
- Body:
  - promotion_plan_id
  - company_id
  - target_filters:
    - category_ids: [...]
    - attribute_filters: { code: [values], code_range: [min, max], ... }
  - target_position: top|top_3|sidebar|inline_N
  - budget_total
  - start_at, end_at
- Response 201:
  - id
  - status: active|pending
  - budget_used: 0

### 9.3. Список кампаний компании (seller)

- GET /promo/campaigns
- Auth: seller
- Params: page, limit, status
- Response 200: paginated campaigns

### 9.4. Получить кампанию

- GET /promo/campaigns/{id}
- Auth: seller (своя) или admin
- Response 200: campaign

### 9.5. Обновить кампанию (seller/admin)

- PATCH /promo/campaigns/{id}
- Auth: seller (своя) или admin
- Body: status (active/paused/cancelled), budget_total, target_filters, target_position
- Response 200: campaign

### 9.6. Лог продвижения (аналитика, seller/admin)

- GET /promo/campaigns/{id}/logs
- Auth: seller (своя) или admin
- Params: page, limit, event_type, from, to
- Response 200:
  - items: [{ id, event_type, context, cost, created_at }]

### 9.7. Управление промо (админ)

- GET /admin/promo/plans
- POST /admin/promo/plans
- PATCH /admin/promo/plans/{id}
- DELETE /admin/promo/plans/{id}

- GET /admin/promo/campaigns
- PATCH /admin/promo/campaigns/{id}

---

## 10. Admin & Analytics

### 10.1. Общие метрики

- GET /admin/analytics/overview
- Auth: admin
- Params: from, to
- Response 200:
  - total_users
  - total_companies
  - total_products
  - total_orders
  - total_revenue
  - promo_revenue

### 10.2. Популярные запросы

- GET /admin/analytics/search-queries
- Auth: admin
- Params: from, to, limit
- Response 200:
  - items: [{ query, count }]

### 10.3. Популярные товары

- GET /admin/analytics/products
- Auth: admin
- Params: from, to, limit, sort (views|orders|revenue)
- Response 200:
  - items: [{ product_id, name, views, orders, revenue }]

---

## Примечания

- Все ID — строки или целые числа, однозначно идентифицирующие сущность в makodb.
- Для сложных фильтров и агрегаций в админке можно добавить отдельный endpoint с динамическими параметрами.
- В дальнейшем можно добавить GraphQL-эндпоинт для гибких запросов к каталогу.
