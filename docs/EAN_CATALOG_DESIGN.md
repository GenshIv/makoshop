# MakoShop / wszyst.pl — EAN-based Catalog & Price Import Design

Дата: 2025-08-26
Статус: черновик для реализации

## 1. Контекст

Маркетплейс-каталог: несколько компаний-поставщиков присылают прайсы
(Nokaut-формат XML). Каталог агрегирует предложения по **EAN** (европейский
штрихкод) и показывает лучшие цены от разных компаний.

Прайсы лежат в `./prices/{company_folder}/{uuid}.xml`.

## 2. Формат прайсов (Nokaut)

```xml
<nokaut>
  <offers>
    <offer>
      <id>1051</id>
      <name>...</name>
      <description>...</description>
      <url>https://...</url>
      <image>https://...</image>
      <price>799</price>
      <category>Biuro i firma</category>
      <shopcategory>Fotele Gamingowe</shopcategory>
      <producer>Domator24</producer>
      <property name="ProductUrl">https://...</property>
      <property name="EAN">5902560337471</property>
      <property name="ImageOriginalUrl">https://...</property>
      <property name="Producent">domator24</property>
      <property name="ShopProductId">1051</property>
      <property name="ShopProductCategory">Fotele Gamingowe</property>
      <property name="PreviousPrice">799</property>
      <property name="Material">nylon</property>  <!-- не у всех -->
      <availability>in stock</availability>
      <shipping>0</shipping>
    </offer>
  </offers>
</nokaut>
```

Особенности (из анализа `prices-report.json`):
- `EAN` — 13 цифр (EAN-13), иногда 12 (UPC-A), 14, 15, 16, 20. Бывает
  несколько через `;` (берём первый).
- Цена: `799` или `139,9` (польская запятая).
- `PreviousPrice` — старая цена. Если `> price` → скидка (оранжевая цена).
- `availability` — разные коды у разных компаний:
  - `in stock` / `out of stock`
  - `in_stock` / `out_of_stock`
  - `1` (в наличии), `4`, `0`
  - `3`, `7`, `1`, `99`, `14`
- Доп. атрибуты: `Material` и т.п. (не у всех).
- 6 компаний с пустыми `<offers>` — игнорируем.

## 3. Модель данных

### Company (расширение)

```go
type Company struct {
    // ... существующие поля ...

    // --- Прайс-импорт (task 1, 3, 7) ---
    ImportFolder    string            // имя папки в ./prices/ (task 3)
    PriceSource     PriceSourceConfig // конфиг парсинга (task 7)

    // --- Посадочная страница (task 4) ---
    DescRu          string
    DescUa          string
    DescPl          string
    DescEn          string
    HeroImage       string            // картинка на лендинге
    IsVisible       bool              // показывать страницу компании
}

// PriceSourceConfig — как парсить прайс конкретной компании.
type PriceSourceConfig struct {
    Format             string            // "nokaut" (пока единственный)
    Currency           string            // "PLN" (по умолчанию)
    EANField           string            // имя property для EAN (default "EAN")
    PreviousPriceField string            // default "PreviousPrice"
    ImageField         string            // default "ImageOriginalUrl" (fallback <image>)
    ProductURLField    string            // default "ProductUrl" (fallback <url>)
    BrandField         string            // default "Producent" (fallback <producer>)
    ShopCategoryField  string            // default "ShopProductCategory"
    AvailabilityMap    map[string]string // raw -> "in_stock"|"out_of_stock"
    AttrFields         []AttrFieldMap    // доп. атрибуты: XML-поле -> код каталога
}

type AttrFieldMap struct {
    Field string // имя property в XML (например "Material")
    Code  string // код атрибута в каталоге (например "material")
}
```

### Product (оффер компании)

```go
type Product struct {
    // ... существующие поля ...
    EAN             string  // было EAN — европейский штрихкод (task 2)
    PreviousPrice   float64 // старая цена (task 6)
    // Уникальность: (EAN, NormalizedName, CompanyID)  (task 3, 5)
}
```

### EANPage (was SCUPage)

SEO-страница на продукт по EAN. Агрегирует офферы всех компаний с этим EAN.
- Ключ: `EAN` (если нет EAN — `NormalizedName`).
- Title — из имени первого/основного оффера.
- MinPrice — минимальная цена среди офферов.

## 4. Уникальность и импорт (task 3, 5)

При импорте прайса компании C:
1. Для каждого offer:
   - `ean` = из поля `EANField` (или "" если нет).
   - `name` = NormalizedName(name) — lowercase, trim, collapse spaces.
   - Ключ оффера: `(ean, name, C.ID)`.
2. Если оффер существует → **UPDATE** (цена, previous_price, stock, images,
   availability). Не создаём дубликат.
3. Если нет → **CREATE**.
4. Для каждого оффера → upsert EANPage по `ean` (или name), добавить ссылку.

Индексы:
- `product_ean:{ean}:{company_id}` -> product_id (уникальность оффера)
- `eanpage_ean:{ean}` -> eanpage_id
- `ean:{ean}` -> list of product_id (для EANPage -> products)
- `eanpage_slug:{slug}` -> eanpage_id

## 5. Конфигурация в админке (task 1, 3, 7)

- Раздел «Компании»:
  - Имя компании (Name) — используется для уникальности.
  - Папка импорта (ImportFolder) — где лежит прайс.
  - Конфиг парсинга (PriceSourceConfig) — поля, маппинг availability,
    доп. атрибуты.
- Импорт/экспорт компаний:
  - Экспорт: JSON со всеми настройками компании.
  - Импорт: JSON -> создаёт/обновляет компанию.
- Запуск импорта прайсов:
  - POST /admin/import-prices?company=ID — импортировать прайс компании.
  - POST /admin/import-prices-all — все компании.

## 6. Посадочные страницы компаний (task 4)

- URL: `/company/{slug}`
- SSR для SEO/ботов.
- Показывает:
  - Название, логотип/hero-картинка.
  - Описание на языке страницы (DescRu/En/Pl/Ua).
  - Текущие параметры: кол-во товаров, мин. цена, категории.
  - Список товаров компании.

## 7. Оранжевая цена (task 6)

В карточке товара (ProductCard / EANPageView):
- Если `PreviousPrice > Price` → цена оранжевым (accent),
  старая цена зачёркнутая.

## 8. План миграции

1. Переименовать EAN -> EAN (backend + frontend).
2. Расширить модели (PreviousPrice, PriceSourceConfig, landing-поля).
3. Новый импорт Nokaut XML (config-driven, upsert).
4. Admin API.
5. Лендинг компании.
6. Frontend: admin UI + оранжевая цена.
7. Пересобрать и проверить.

Старый импорт (CSV/JSONL) удалить после проверки нового.
