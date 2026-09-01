# Система брендирования сайта (украшения под разные случаи) — оценка и план работ

Дата: 2025-09-01
Статус: черновик плана (к реализации)

## 1. Контекст: что уже есть в коде

Стек (проверено по репозиторию):

- **Backend**: Go, модуль `github.com/GenshIv/makoshop`. Хранение — документная БД (makodb: `DocPut/DocGet`, `NextID`), turbo-индексы. Паттерны: модели в `internal/model/models.go`, ключи в `internal/db/keys.go`, репозитории `internal/db/*_repo.go`, хендлеры `internal/api/*.go`, единая таблица маршрутов `internal/api/router/router.go`.
- **Frontend**: Vue 3 SPA (Tailwind v4, Pinia, vue-router, vue-i18n: ru/ua/en/pl, axios-клиент `frontend/src/api/index.js`). В проде один Go-сервер раздаёт и API, и SPA (дискриминация по `Accept: text/html`).
- **Структура страницы** (`frontend/src/App.vue`): sticky-хедер → `<main>` (router-view) → футер. Ширина контента — `max-w-app` = 1568px.
- **Баннер главной**: `CatalogView.vue`, блок hero (`showHero`, ~строки 1241–1255) — показывается только в «чистом» состоянии главной (без поиска/категории/EAN-страницы).
- **Баннер категории**: `CatalogView.vue`, блок «Current category header» (~строки 1406–1457) — заголовок категории + изображение справа.
- **Админка**: маршруты `/admin/*`, карточки-ссылки на всех разделах в `AdminDashboardView.vue`, примеры страниц настроек — `AdminSettingsView.vue`, `AdminDeliveryTimesView.vue` и т.п.
- **Загрузка картинок**: `POST /admin/upload-image` → `data/uploads/categories/{filename}`, ресайз до `categoryImageMaxDim = 400` px (`internal/api/image_resize.go`), отдача по `/uploads/...`.
- **i18n**: 4 языка, админские строки в секции `admin.*` в `frontend/src/i18n/{ru,ua,en,pl}.json`.
- Системы украшений/баннеров страницы **нет** (есть только cookie-banner). Всё строится с нуля, но полностью по существующим паттернам.

## 2. Дизайн системы

### 2.1 Слоты (места на странице)

| Слот (id) | Место | Ориентировочный размер |
|---|---|---|
| `header_fullwidth` | На всю ширину, прямо под хедером | до 1920×200 |
| `home_banner` | Баннер главной страницы (место hero) | до 1500×450 |
| `category_banner` | Баннер категории (шапка страницы категории) | до 1500×350 |
| `footer_fullwidth` | На всю ширину, прямо над футером | до 1920×200 |
| `side_left_top` | Левая колонка, верх | до 300×300 |
| `side_left_bottom` | Левая колонка, низ | до 300×300 |
| `side_right_top` | Правая колонка, верх | до 300×300 |
| `side_right_bottom` | Правая колонка, низ | до 300×300 |

Всего **8 слотов**. Боковые слоты видны только на широких экранах (≥1600px — контент 1568px оставляет по ~170px по бокам на 1920px-экране); на узких — скрыты, чтобы не ломать вёрстку. Верхние боковые картинки — «над» нижними полнширинными зонами, нижние — «под» (по вертикали — в верхней и нижней трети экрана, см. 2.6).

### 2.2 Модель данных

Три сущности (все — документы в makodb, как `Category`, `LandingPage`):

```go
// Слот — место размещения элемента брендирования.
type BrandSlot string
const (
    SlotHeaderFullwidth BrandSlot = "header_fullwidth"
    SlotHomeBanner      BrandSlot = "home_banner"
    SlotCategoryBanner  BrandSlot = "category_banner"
    SlotFooterFullwidth BrandSlot = "footer_fullwidth"
    SlotSideLeftTop     BrandSlot = "side_left_top"
    SlotSideLeftBottom  BrandSlot = "side_left_bottom"
    SlotSideRightTop    BrandSlot = "side_right_top"
    SlotSideRightBottom BrandSlot = "side_right_bottom"
)

// BrandElement — один элемент бренда в конкретном слоте.
type BrandElement struct {
    Slot         BrandSlot `json:"slot"`
    ImageURL     string    `json:"image_url"`                       // светлая тема
    ImageDarkURL string    `json:"image_dark_url,omitempty"`        // тёмная тема (опц.)
    LinkURL      string    `json:"link_url,omitempty"`              // опц. ссылка по клику
    AltText      string    `json:"alt_text,omitempty"`
    // Список regex-паттернов страниц, где показывать (JS-синтаксис,
    // сопоставление с route.path). Пустой список = показывать везде.
    // Несколько паттернов = ИЛИ (показ, если совпал хотя бы один).
    PagePatterns []string `json:"page_patterns,omitempty"`
}

// BrandSet — именованный набор брендирования («Новый год 2025», «Летняя распродажа»).
// Базовая единица управления: включается/выключается целиком.
type BrandSet struct {
    ID          int64          `json:"id"`
    Name        string         `json:"name"`
    Description string         `json:"description,omitempty"`
    Enabled     bool           `json:"enabled"`
    Priority    int            `json:"priority"` // при конфликте — выше побеждает
    Elements    []BrandElement `json:"elements"` // по одному на интересующие слоты
    CreatedAt   int64          `json:"created_at"`
    UpdatedAt   int64          `json:"updated_at"`
}

// BrandCategoryTheme — переопределение картинки для раздела (категории).
// «Для разных разделов могут быть свои картинки в любое место».
type BrandCategoryTheme struct {
    ID           int64     `json:"id"`
    CategoryID   int64     `json:"category_id"`
    Slot         BrandSlot `json:"slot"`
    ImageURL     string    `json:"image_url"`
    ImageDarkURL string    `json:"image_dark_url,omitempty"`
    LinkURL      string    `json:"link_url,omitempty"`
    CreatedAt    int64     `json:"created_at"`
    UpdatedAt    int64     `json:"updated_at"`
}
```

Ключи (`internal/db/keys.go`):

- `brand_set:{id}` — документ набора.
- `brand_cat_theme:{category_id}:{slot}` — документ переопределения (уникальность на пару «категория+слот», upsert).
- `branding:version` — счётчик версий (bump при каждом изменении) — для свежести кэша на клиенте.

Элементы вложены в набор (один документ на набор): набор — атомарная единица редактирования и переключения, лишних сущностей и транзакций нет.

### 2.3 Логика показа (resolution)

Решается **на клиенте** (SPA: «страница» — это клиентский роут, сервер её не знает), данные берутся одним запросом.

Для текущего `route.path` и слота:

1. **Переопределение раздела** — если страница находится в категории (путь `/shop/...`), и для этой категории (или её предка) задан `BrandCategoryTheme` по слоту → показываем его. *Высший приоритет.*
2. **Точное совпадение regex** — среди включённых наборов берём элементы слота, у которых непустой `PagePatterns` и хотя бы один паттерн совпал с путём; выбираем из **набора с максимальным приоритетом**.
3. **По умолчанию** — среди включённых наборов элемент слота с пустым `PagePatterns`; снова — максимальный приоритет.
4. Иначе слот пуст.

Специфичность важнее приоритета: точное совпадение в наборе с низким приоритетом побеждает «везде»-элемент набора с высоким.

Правила regex:

- Синтаксис JS (ECMAScript), т.к. маппинг выполняется в браузере.
- Сопоставление — с `route.path` без query-строки (примеры: `^/$` — только главная; `^/shop/telefony` — раздел и всё под ним; `^/products/` — страницы товаров).
- Валидация в админке перед сохранением: `try { new RegExp(p) } catch` → ошибка с подсказкой.
- Сервер дополнительно ограничивает длину паттерна (≤200 символов) и количество (≤10 на элемент) — защита от мусора.

### 2.4 API

Публичный (без токена):

- `GET /branding/active` →
  ```json
  {
    "version": 42,
    "sets": [ { "id": 1, "name": "...", "enabled": true, "priority": 10, "elements": [ ... ] } ],
    "category_overrides": [ { "category_id": 7, "slot": "category_banner", "image_url": "/uploads/...", ... } ]
  }
  ```
  Маленький payload (несколько КБ), возвращается только включённые наборы. Кэшируется на клиенте; повторный запрос при смене версии/тилле.

Админские (роль `admin`, паттерн `spaAwareHandler` как у `/admin/settings`):

- `GET  /admin/branding/sets` — список всех наборов (включая выключенные).
- `POST /admin/branding/sets` — создать.
- `PATCH /admin/branding/sets/{id}` — обновить (в т.ч. мгновенный toggle `enabled`, priority, элементы).
- `DELETE /admin/branding/sets/{id}` — удалить.
- `GET  /admin/branding/category-overrides` — список переопределений.
- `POST /admin/branding/category-overrides` — upsert по (category_id, slot).
- `DELETE /admin/branding/category-overrides/{id}` — удалить.
- `POST /admin/branding/preview` (опц., удобно для чекбокса «проверить regex») — серверная проверка совпадения паттерна с путём.

Картинки: переиспользуем `POST /admin/upload-image` / `DELETE /admin/upload-image/{filename}`; добавляем подкаталог `branding` и **большой maxDim для широких баннеров** (сейчас жёстко 400px — для fullwidth-слотов мало). Решение: параметр/подкаталог определяет целевой размер (fullwidth ≤1920, home/category ≤1600, side ≤400).

### 2.5 Frontend: рендеринг

Новые файлы:

- `frontend/src/stores/branding.js` (Pinia): загрузка `GET /branding/active` при старте приложения; хранение `version`; методы `refresh()` (после сохранения в админке) и `isStale()` (TTL ~60 c).
- `frontend/src/composables/useBranding.js`: `resolveSlot(slot)` — реализация логики 2.3 (regex, приоритет, переопределения категорий, текущий `route.path`); реактивно пересчитывается при смене маршрута.
- `frontend/src/components/BrandingSlot.vue`: принимает `slot`, рендерит элемент: `<a>` (если есть `link_url`) → `<img>` (light/dark как в категориях: `dark:` классы), `alt`, `loading="lazy"` для ниже-фолда слотов, фиксированная высота/aspect-ratio против CLS, `object-cover`.

Размещение:

- **App.vue**:
  - `<BrandingSlot slot="header_fullwidth" />` — между `</header>` и `<main>`.
  - `<BrandingSlot slot="footer_fullwidth" />` — между `</main>` и `<footer>`.
  - Боковые слоты — CSS-сетка/absolute-колонки по краям вьюпорта, видимы только на `min-width: 1600px`; верхняя пара — в верхней трети, нижняя — в нижней трети экрана.
- **CatalogView.vue**:
  - `home_banner` — в hero-зоне: если слот заполнен, баннер заменяет/дополняет hero (решение на этапе вёрстки: поверх hero, как фоновое изображение hero-блока, либо отдельным блоком над ним; по умолчанию — замена фона hero).
  - `category_banner` — в шапке категории: баннер над/вместо текущего блока с картинкой категории.

### 2.6 Админка: отдельная страница `/admin/branding`

`frontend/src/views/admin/AdminBrandingView.vue` (+ маршрут в `router/index.js`, карточка-ссылка в `AdminDashboardView.vue`):

1. **Список наборов** (главный вид страницы) — карточки/таблица:
   - переключатель **Вкл/Выкл** на каждой карточке — мгновенный `PATCH`, без подтверждения («включать-выключать в любой момент»);
   - имя, описание, приоритет (число), количество заполненных слотов, дата изменения;
   - кнопки: «Изменить», «Удалить» (с ConfirmDialog), «+ Новый набор».
2. **Редактор набора** (отдельный экран или модалка):
   - имя, описание, приоритет;
   - блок на каждый из 8 слотов: загрузка картинки (light + dark, с превью), ссылка (опц.), alt, **список regex-паттернов** (добавить/удалить строку, live-валидация `new RegExp`, подсказка по синтаксису, чекбокс «проверить по текущему пути» через `/admin/branding/preview`); пустой список = «показывать на всех страницах»;
   - мини-превью: где на странице находится каждый слот (схема-заглушка).
3. **Переопределения разделов** (вторая вкладка страницы):
   - выбор категории (переиспользуем `AdminCategoryTree.vue`), выбор слота, загрузка картинки (light/dark), ссылка;
   - таблица существующих переопределений с удалением.
4. **i18n**: новая секция `admin.branding.*` в 4 языках (ru/ua/en/pl).

### 2.7 Свежесть и кэш

- Сервер: `branding:version` bump'ится при каждом admin-записи; `GET /branding/active` отдаёт version.
- Клиент: загрузка при старте + TTL ~60 c + принудительный `refresh()` после сохранения в админке (пользователь сразу видит результат на своём сайте).
- Опц. (не в базовой оценке): server-side кэш активного списка в памяти на время жизни процесса.

## 3. План работ по фазам

### Фаза 0. Подготовка — ~0.5 дня
- Зафиксировать размеры слотов, брейкпоинт боковых колонок (1600px), целевые maxDim для загрузки.
- Утвердить поведение `home_banner` (замена hero vs отдельный блок).

### Фаза 1. Backend: модель и хранение — ~1 день
- `internal/model/models.go`: `BrandSlot`, `BrandElement`, `BrandSet`, `BrandCategoryTheme` (+ валидаторы: длина/число regex, допустимые слоты).
- `internal/db/keys.go`: `KeyBrandSet`, `KeyBrandCatTheme`, `KeyBrandingVersion`.
- `internal/db/branding_repo.go`: CRUD наборов, upsert/delete переопределений, `ListActive()`, `BumpVersion()/GetVersion()`.
- Юнит-тесты marshal/unmarshal и repo (на временной БД, как `transaction_test.go`).

### Фаза 2. Backend: API — ~1 день
- `internal/api/branding_handlers.go`: публичный `GET /branding/active` + админ CRUD (по списку 2.4), валидация regex на входе.
- `internal/api/router/router.go`: регистрация маршрутов (паттерн `spaAwareHandler` + `JWT.RequireRole(..., model.RoleAdmin)`).
- `internal/api/upload_handlers.go`: подкаталог `branding`, maxDim по слотам (fullwidth до 1920).
- Проверка: `go build ./cmd/server`, `go vet`, ручные curl-прогоны (создать набор → включить → `GET /branding/active`).

### Фаза 3. Frontend: рендеринг — ~1.5 дня
- `stores/branding.js`, `composables/useBranding.js` (логика 2.3), `components/BrandingSlot.vue`.
- Размещение в `App.vue` (4 полнширинных/боковых слота) и `CatalogView.vue` (home_banner, category_banner).
- Проверка: все 8 слотов на всех типах страниц, light/dark, мобильный/десктоп, пустые состояния, отсутствие CLS.
- (Опц.) Vitest для `useBranding` — в проекте пока нет фронтенд-тест-фреймворка; если вводим — +0.5 дня.

### Фаза 4. Админка — ~2–3 дня (самая большая часть)
- Маршрут `/admin/branding` + карточка в дашборде.
- `AdminBrandingView.vue`: список наборов с живыми toggle; редактор (8 слотов: upload light/dark, ссылка, regex-список с валидацией и preview); вкладка переопределений категорий.
- i18n: 4 языка.
- Прогон: создать набор «тест» → заполнить все слоты → включить → проверить на сайте; переключить → проверить; regex на разных путях; переопределение категории.

### Фаза 5. Тестирование и релиз — ~1 день
- Матрица ручных тестов: слот × тип страницы (главная, категория, товар, корзина, EAN) × тема × ширина экрана.
- Regex: `^/$`, `^/shop/...`, `^/products/`, несколько паттернов (ИЛИ), невалидный паттерн (отклоняется в админке).
- Приоритеты: два включённых набора с конфликтом; специфичность vs приоритет.
- Свежесть: изменение в админке видно на сайте ≤ TTL/после refresh.
- Производительность: размер payload `GET /branding/active`, lazy-load, вес картинок.
- Релиз: `vite build` + `go build ./cmd/server`, деплой, smoke-проверка.
- Документация: запись в `CHANGELOG.md`, короткая инструкция администратору (раздел в этом файле или отдельная заметка).

## 4. Оценка

| Блок | Оценка |
|---|---|
| Backend (модель + repo + API + upload) | ~2 дня |
| Frontend-рендеринг (store, composable, компонент, вставка в App/Catalog) | ~1.5 дня |
| Админка (страница, редактор, overrides, i18n ×4) | ~2–3 дня |
| Тестирование + релиз + docs | ~1 день |
| **Итого** | **~6.5–8 рабочих дней**, один разработчик |

Сложность: **средняя**. Новая инфраструктура не требуется — всё по существующим паттернам проекта (модель/ключ/репо/хендлер/маршрут; Vue-страницы админки; upload; i18n).

Риски и решения:

- **Синтаксис regex**: клиентский JS vs RE2 (Go). Маппинг на клиенте → JS-синтаксис; валидация в админке `new RegExp` + серверные лимиты длины/количества.
- **Боковые слоты на узких экранах** — скрывать (min-width 1600px), не сужать контент.
- **Размеры картинок**: текущий upload режет до 400px — для fullwidth-баннеров мало; расширяем maxDim по слотам.
- **CLS**: баннеры подгружаются асинхронно — резервируем высоту (aspect-ratio/фиксированная высота) в пустом состоянии слота.
- **Свежесть**: version + TTL + refresh после сохранения в админке.

Опции на будущее (не в оценке): расписание показа по датам, A/B-варианты, HTML-контент в слотах (не только картинка), таргетинг по пользователю/роli, несколько языковых вариантов текста на баннере.

## 5. Файлы: что добавится / изменится

Новые:

- `internal/db/branding_repo.go`
- `internal/api/branding_handlers.go`
- `frontend/src/stores/branding.js`
- `frontend/src/composables/useBranding.js`
- `frontend/src/components/BrandingSlot.vue`
- `frontend/src/views/admin/AdminBrandingView.vue`

Изменяемые:

- `internal/model/models.go` (BrandSlot, BrandElement, BrandSet, BrandCategoryTheme)
- `internal/db/keys.go` (ключи)
- `internal/api/router/router.go` (маршруты)
- `internal/api/upload_handlers.go` (подкаталог branding, maxDim по слотам)
- `frontend/src/App.vue` (слоты под хедером, над футером, боковые колонки)
- `frontend/src/views/CatalogView.vue` (home_banner, category_banner)
- `frontend/src/router/index.js` (`/admin/branding`)
- `frontend/src/views/admin/AdminDashboardView.vue` (карточка-ссылка)
- `frontend/src/i18n/{ru,ua,en,pl}.json` (`admin.branding.*`)
