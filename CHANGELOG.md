# Changelog — MakoShop

## v1.4 — Багфиксы

### Исправлено

- **Фильтры в каталоге**: теперь корректно работают несколько фильтров одновременно
  - Исправлен `watch([filters], ...)` → `watch(filters, ...)` для reactive объекта
  - `attrFilters` изменён с `ref({})` на `reactive({})` для корректного отслеживания изменений вложенных свойств
  - Исправлен watch на attrFilters: `JSON.stringify` → `watch(attrFilters, ..., { deep: true })`
- **Гостевая корзина**: теперь доступна без авторизации
  - Исправлен интерцептор Axios: проверка через заголовок `X-Skip-Auth-Redirect` вместо кастомного поля config
  - При запросе `/cart/me` без токена больше не происходит редирект на логин
  - Fallback: auth cart → guest cart (localStorage `guest_cart_id`) → пустая корзина
- Убран лишний `filters.brand` в `onMounted` (поля не существовало)

### Изменено

- `api/index.js`: проверка 401 теперь использует `X-Skip-Auth-Redirect` заголовок
- `stores/cart.js`: запрос `/cart/me` отправляется с `{ headers: { 'X-Skip-Auth-Redirect': 'true' } }`
- `views/CatalogView.vue`: `attrFilters` → `reactive({})`, исправлены watch и обращения

---

## v1.3 — Этап 7: Публичный портал (frontend)

### Исправлено

- Сортировка `price_asc` теперь корректно работает (ранее поддерживался только `price_desc`)

### Добавлено

- **Vue 3 + Vite + Pinia + Vue Router** — фронтенд-приложение в директории `frontend/`

#### Публичный портал (buyer)

- Каталог с поиском, фильтрами, сортировкой, пагинацией
- Карточка товара с отзывами
- Корзина и оформление заказа
- Авторизация/регистрация с JWT
- Личный кабинет, заказы, отзывы

#### Кабинет продавца (seller)

- Dashboard: статистика (товары, заказы, выручка, кампании)
- Управление товарами: список, создание, редактирование, удаление
- Заказы: просмотр заказов по компании
- Продвижение: рекламные кампании (создание, список, статусы)

#### Админ-панель (admin)

- Dashboard: обзор (пользователи, компании, товары, заказы, выручка)
- Пользователи: список, роли, статусы
- Компании: верификация, блокировка
- Категории: создание, список
- Аналитика: обзор, заказы по статусам, популярные товары, поисковые запросы
- Промо: планы продвижения, кампании

#### Инфраструктура

- Роут-гарды с проверкой ролей (seller/admin)
- Navigation: ссылки на seller/admin панели в хедере
- Proxy: все API-запросы через vite proxy на localhost:9090
- **Tailwind CSS** — стилизация интерфейса
- **Axios** — HTTP-клиент с JWT-интерцептором

#### Страницы

- **Каталог** (`/`) — поиск `q`, фильтры (brand, price range, attrs), сортировка, пагинация, facets UI (brands/attrs с counts)
- **Карточка товара** (`/products/:id`) — полная информация, images, attrs, avg rating, кнопка «В корзину», отзывы с пагинацией, форма отправки отзыва
- **Корзина** (`/cart`) — список товаров, изменение qty, удаление, переход к оформлению
- **Оформление заказа** (`/checkout`) — форма shipping_info, создание заказа из корзины, создание платежа
- **Авторизация** (`/login`, `/register`) — вход/регистрация buyer/seller, JWT в localStorage, merge guest cart
- **Личный кабинет** (`/profile`) — просмотр/редактирование профиля (PATCH /users/me)
- **Заказы** (`/orders`, `/orders/:id`) — список заказов пользователя, детали заказа (status, items, shipping, payment)
- **Отзывы** (`/reviews`) — список своих отзывов

#### Архитектура

- `src/api/index.js` — axios instance с авторизационным интерцептором
- `src/router/index.js` — маршруты с guard для auth-required страниц
- `src/stores/auth.js` — Pinia store для авторизации
- `src/stores/cart.js` — Pinia store для корзины (create cart, add/update/remove items)
- `src/views/` — все View-компоненты
- `src/App.vue` — layout с header (search, cart badge, nav), footer
- `.env` — `VITE_API_URL` для настройки backend URL
- `vite.config.js` — proxy → `localhost:9090` для разработки

### Заметки

- Бэкенд не изменён
- Frontend полностью независим, запускается через `cd frontend && npm run dev` (порт 5173)
- Бэкенд: порт 9090 (по умолчанию в config)

---

## v1.2 — Оптимизация

- L1 cache SimpleCache
- Глобальные индексы `idx:all:*` для users/products/orders/payments/promo_campaigns
- Замена ForEach-сканов на индексы
- pprof endpoints

## v1.1 — Админка API

- Verify company, analytics overview/products/search-queries
- Admin promo campaigns, mass import products

## v1.0 — Отзывы, Faceted Search, Timeout Cleanup

- Reviews CRUD
- Faceted search (brands/attrs counts)
- Реальный timeout cleanup

## v0.9 — Промо-эффекты в поиске

- Position boost для товаров компаний с активными кампаниями

## v0.8 — Промо и продвижение

- Plans, campaigns, logs, budget_used

## v0.7 — Seller по заказам и аналитика

- /companies/{id}/orders, /admin/analytics/orders

## v0.6 — Платежи: webhook, refund, timeout-cleanup

## v0.5 — Улучшение заказов

- Order from cart, валидации stock/active, access control по ролям

## v0.4 — Корзина, заказы, платежи

- Cart/Order/Payment CRUD, слияние корзин при логине

## v0.3 — Авторизация/роли

- JWT, admin/seller/buyer, ownership товаров

## v0.2 — Каталог

- GET /products (поиск q, фильтры, сортировка, пагинация)

## v0.1 — Ядро

- Категории, атрибуты, товары, makodb
